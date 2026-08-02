package test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
)

func TestBuildSnapshotNormalizesAndSortsStableDevices(t *testing.T) {
	provider := device.StaticProvider{Devices: []device.RawDevice{
		inventoryRaw("0000:00:02.0", "wormhole", "n300d", "Healthy", true),
		inventoryRaw("0000:00:01.0", "bh", "p150b", "ok", true),
	}}

	snapshot, err := device.BuildSnapshot(context.Background(), provider)
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 2 {
		t.Fatalf("device count = %d, want 2", len(snapshot.Devices))
	}
	if got := snapshot.Devices[0].StableID; got != "pci-0000:00:01.0" {
		t.Fatalf("first stable ID = %q, want PCI-derived ID", got)
	}
	if got := snapshot.Devices[0].CardSeries; got != "p150" {
		t.Fatalf("normalized card series = %q, want p150", got)
	}
	if !snapshot.Devices[0].Eligible || !snapshot.Devices[1].Eligible {
		t.Fatalf("healthy known devices should be eligible: %#v", snapshot.Devices)
	}
	if snapshot.Devices[0].Provenance["pci"].Path == "" {
		t.Fatal("PCI provenance path is empty")
	}
	for _, field := range []string{"stableID", "characterDevice", "pci", "chipSeries", "cardSeries", "memory", "compute", "health", "fabric", "node", "observedAt"} {
		provenance, ok := snapshot.Devices[0].Provenance[field]
		if !ok || provenance.Source == "" || provenance.ObservedAt.IsZero() {
			t.Fatalf("missing provenance for canonical field %q: %#v", field, snapshot.Devices[0].Provenance)
		}
	}
}

func TestFilesystemProviderRequiresAbsoluteRoots(t *testing.T) {
	_, err := device.NewFilesystemProvider(device.Roots{
		DeviceRoot:           "dev/tenstorrent",
		TenstorrentSysfsRoot: "/sys/class/tenstorrent",
		PCISysfsRoot:         "/sys/bus/pci/devices",
		StateDir:             "/var/lib/tenstorrent-dra",
	})
	if err == nil {
		t.Fatal("relative device root was accepted")
	}
}

func TestPCIIdentityUsesObservedLinkState(t *testing.T) {
	raw := inventoryRaw("0000:00:01.0", "wormhole", "n150", "Healthy", true)
	raw.Values["pci.current_link_state"] = "L0"
	raw.Values["pci.uevent.DRIVER"] = "tenstorrent"
	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{raw}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Devices[0].PCI.LinkState; got != "L0" {
		t.Fatalf("link state = %q, want L0", got)
	}
}

func TestBuildSnapshotFailsClosedPerDevice(t *testing.T) {
	badHealth := inventoryRaw("0000:00:01.0", "wormhole", "n150", "failed", true)
	missingChar := inventoryRaw("0000:00:02.0", "wormhole", "n150", "Healthy", false)
	unknownCard := inventoryRaw("0000:00:03.0", "unknown", "n999", "Healthy", true)

	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{badHealth, missingChar, unknownCard}})
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 3 {
		t.Fatalf("device count = %d, want 3", len(snapshot.Devices))
	}
	for _, observed := range snapshot.Devices {
		if observed.Eligible {
			t.Errorf("device %s unexpectedly eligible", observed.StableID)
		}
		if observed.RejectionReason == "" {
			t.Errorf("device %s has no rejection reason", observed.StableID)
		}
	}
}

func TestBuildSnapshotRejectsDuplicateStableIdentity(t *testing.T) {
	first := inventoryRaw("0000:00:01.0", "wormhole", "n150", "Healthy", true)
	second := inventoryRaw("0000:00:01.0", "wormhole", "n150", "Healthy", true)
	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	for _, observed := range snapshot.Devices {
		if observed.Eligible || observed.RejectionReason != "duplicate stable identity" {
			t.Fatalf("duplicate identity was not rejected: %#v", observed)
		}
	}
}

func TestBuildSnapshotDoesNotInventMissingCapacity(t *testing.T) {
	raw := inventoryRaw("0000:00:04.0", "wormhole", "n150", "Healthy", true)
	delete(raw.Values, "memory_capacity_bytes")
	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{raw}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Devices[0].Memory.TotalBytes; got != 0 {
		t.Fatalf("missing memory was synthesized as %d bytes", got)
	}
}

func TestFilesystemAndStaticProvidersHaveEquivalentCanonicalSemantics(t *testing.T) {
	root := t.TempDir()
	ttRoot := filepath.Join(root, "sys", "class", "tenstorrent")
	dataPath := filepath.Join(root, "sys", "devices", "tt", "0")
	pciRoot := filepath.Join(root, "sys", "bus", "pci", "devices")
	pciPath := filepath.Join(pciRoot, "0000:00:01.0")
	if err := os.MkdirAll(dataPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ttRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pciPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dataPath, filepath.Join(ttRoot, "0")); err != nil {
		t.Fatal(err)
	}
	writeInventoryValue(t, filepath.Join(dataPath, "uevent"), "DEVNAME=/dev/tenstorrent/0\n")
	writeInventoryValue(t, filepath.Join(dataPath, "dev"), "226:0\n")
	writeInventoryValue(t, filepath.Join(dataPath, "architecture"), "wormhole\n")
	writeInventoryValue(t, filepath.Join(dataPath, "board_type"), "n300d\n")
	writeInventoryValue(t, filepath.Join(dataPath, "health"), "Healthy\n")
	writeInventoryValue(t, filepath.Join(dataPath, "memory_capacity_bytes"), "1234\n")
	writeInventoryValue(t, filepath.Join(dataPath, "tensix_cores_total"), "72\n")
	if err := os.Symlink(pciPath, filepath.Join(dataPath, "device")); err != nil {
		t.Fatal(err)
	}
	writeInventoryValue(t, filepath.Join(pciPath, "PCI_SLOT_NAME"), "0000:00:01.0\n")
	writeInventoryValue(t, filepath.Join(pciPath, "vendor"), "0x1e52\n")
	writeInventoryValue(t, filepath.Join(pciPath, "device"), "0x401e\n")
	writeInventoryValue(t, filepath.Join(pciPath, "numa_node"), "0\n")

	provider, err := device.NewFilesystemProvider(device.Roots{
		DeviceRoot:           filepath.Join(root, "dev"),
		TenstorrentSysfsRoot: ttRoot,
		PCISysfsRoot:         pciRoot,
		StateDir:             filepath.Join(root, "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	filesystemSnapshot, err := device.BuildSnapshot(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	staticSnapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{inventoryRaw("0000:00:01.0", "wormhole", "n300d", "Healthy", false)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := canonicalSemantics(filesystemSnapshot.Devices[0]), canonicalSemantics(staticSnapshot.Devices[0]); got != want {
		t.Fatalf("filesystem and static canonical semantics differ:\n got: %s\nwant: %s", got, want)
	}
}

func TestInventorySnapshotJSONRoundTrip(t *testing.T) {
	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{inventoryRaw("0000:00:01.0", "wormhole", "n150", "Healthy", true)}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded device.InventorySnapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, want := canonicalSemantics(decoded.Devices[0]), canonicalSemantics(snapshot.Devices[0]); got != want {
		t.Fatalf("round-trip semantics differ:\n got: %s\nwant: %s", got, want)
	}
}

func TestInventoryNormalizationTable(t *testing.T) {
	tests := []struct {
		name, chip, card, health string
		eligible                 bool
		wantChip, wantCard       string
	}{
		{name: "wormhole variant", chip: "WH", card: "n150d", health: "ready", eligible: true, wantChip: "wormhole", wantCard: "n150"},
		{name: "blackhole variant", chip: "bh", card: "p150a", health: "ok", eligible: true, wantChip: "blackhole", wantCard: "p150"},
		{name: "unknown chip", chip: "mystery", card: "n150", health: "healthy", eligible: false, wantCard: "n150"},
		{name: "unknown health", chip: "wormhole", card: "n150", health: "", eligible: false, wantChip: "wormhole", wantCard: "n150"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{inventoryRaw("0000:00:01.0", testCase.chip, testCase.card, testCase.health, true)}})
			if err != nil {
				t.Fatal(err)
			}
			observed := snapshot.Devices[0]
			if observed.Eligible != testCase.eligible || observed.ChipSeries != testCase.wantChip || observed.CardSeries != testCase.wantCard {
				t.Fatalf("got eligible=%v chip=%q card=%q reason=%q", observed.Eligible, observed.ChipSeries, observed.CardSeries, observed.RejectionReason)
			}
		})
	}
}

func FuzzBuildSnapshotNeverPanics(f *testing.F) {
	f.Add("0000:00:01.0", "wormhole", "n150", "Healthy", "0x1e52")
	f.Add("", "", "", "", "")
	f.Fuzz(func(t *testing.T, bdf, chip, card, health, vendor string) {
		_, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{{
			ID:                     bdf,
			CharacterDevicePresent: true,
			Node:                   device.Node{ID: bdf, Path: "/dev/tenstorrent/0", Major: 226, Minor: 0},
			Values: map[string]string{
				"pci.PCI_SLOT_NAME": bdf,
				"pci.vendor":        vendor,
				"architecture":      chip,
				"board_type":        card,
				"health":            health,
			},
		}}})
		if err != nil {
			t.Fatalf("unexpected provider error: %v", err)
		}
	})
}

func TestFilesystemProviderReadsSyntheticSysfs(t *testing.T) {
	root := t.TempDir()
	deviceRoot := filepath.Join(root, "dev", "tenstorrent")
	ttRoot := filepath.Join(root, "sys", "class", "tenstorrent")
	pciRoot := filepath.Join(root, "sys", "bus", "pci", "devices")
	devicePath := filepath.Join(ttRoot, "0")
	deviceDataPath := filepath.Join(root, "sys", "devices", "tt", "0")
	pciPath := filepath.Join(pciRoot, "0000:00:01.0")
	if err := os.MkdirAll(deviceDataPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ttRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(deviceDataPath, devicePath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pciPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeInventoryValue(t, filepath.Join(devicePath, "uevent"), "DEVNAME=/dev/tenstorrent/0\n")
	writeInventoryValue(t, filepath.Join(devicePath, "dev"), "226:0\n")
	writeInventoryValue(t, filepath.Join(devicePath, "architecture"), "wormhole\n")
	writeInventoryValue(t, filepath.Join(devicePath, "board_type"), "n300\n")
	writeInventoryValue(t, filepath.Join(devicePath, "health"), "Healthy\n")
	writeInventoryValue(t, filepath.Join(pciPath, "PCI_SLOT_NAME"), "0000:00:01.0\n")
	writeInventoryValue(t, filepath.Join(pciPath, "vendor"), "0x1e52\n")
	writeInventoryValue(t, filepath.Join(pciPath, "device"), "0x401e\n")
	writeInventoryValue(t, filepath.Join(pciPath, "numa_node"), "0\n")
	if err := os.Symlink(pciPath, filepath.Join(devicePath, "device")); err != nil {
		t.Fatal(err)
	}

	provider, err := device.NewFilesystemProvider(device.Roots{
		DeviceRoot:           deviceRoot,
		TenstorrentSysfsRoot: ttRoot,
		PCISysfsRoot:         pciRoot,
		StateDir:             filepath.Join(root, "state"),
	})
	if err != nil {
		t.Fatalf("NewFilesystemProvider returned error: %v", err)
	}
	snapshot, err := device.BuildSnapshot(context.Background(), provider)
	if err != nil {
		t.Fatalf("BuildSnapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 1 {
		t.Fatalf("device count = %d, want 1", len(snapshot.Devices))
	}
	observed := snapshot.Devices[0]
	if observed.StableID != "pci-0000:00:01.0" {
		t.Fatalf("stable ID = %q", observed.StableID)
	}
	if observed.CharacterDevicePresent {
		t.Fatal("synthetic tree without a character node should not be allocatable")
	}
	if observed.Eligible || observed.RejectionReason != "character device is missing" {
		t.Fatalf("unexpected eligibility: eligible=%v reason=%q", observed.Eligible, observed.RejectionReason)
	}
}

func TestFilesystemProviderRejectsPCIPathEscape(t *testing.T) {
	root := t.TempDir()
	ttRoot := filepath.Join(root, "tt")
	pciRoot := filepath.Join(root, "pci")
	outside := filepath.Join(root, "outside")
	devicePath := filepath.Join(ttRoot, "0")
	for _, path := range []string{devicePath, pciRoot, outside} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(devicePath, "device")); err != nil {
		t.Fatal(err)
	}
	provider, err := device.NewFilesystemProvider(device.Roots{
		DeviceRoot:           filepath.Join(root, "dev"),
		TenstorrentSysfsRoot: ttRoot,
		PCISysfsRoot:         pciRoot,
		StateDir:             filepath.Join(root, "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := device.BuildSnapshot(context.Background(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Eligible {
		t.Fatalf("path escape should isolate device: %#v", snapshot.Devices)
	}
	if snapshot.Devices[0].RejectionReason == "" {
		t.Fatal("path escape has no rejection reason")
	}
}

func inventoryRaw(bdf, chip, card, health string, characterDevice bool) device.RawDevice {
	return device.RawDevice{
		ID:                     bdf,
		CharacterDevicePresent: characterDevice,
		Node:                   device.Node{ID: bdf, Path: "/dev/tenstorrent/0", Major: 226, Minor: 0},
		SysfsPath:              "/sys/class/tenstorrent/0",
		PCIPath:                "/sys/bus/pci/devices/" + bdf,
		Values: map[string]string{
			"pci.PCI_SLOT_NAME":     bdf,
			"pci.vendor":            "0x1e52",
			"pci.device":            "0x401e",
			"pci.numa_node":         "0",
			"architecture":          chip,
			"board_type":            card,
			"health":                health,
			"memory_capacity_bytes": "1234",
			"tensix_cores_total":    "72",
		},
	}
}

func writeInventoryValue(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func canonicalSemantics(observed device.InventoryDevice) string {
	value := struct {
		StableID        string
		PCI             device.PCIIdentity
		ChipSeries      string
		CardSeries      string
		FirmwareVersion string
		KMDVersion      string
		Memory          device.MemoryInfo
		Compute         device.ComputeInfo
		Health          device.HealthState
		Fault           device.FaultInfo
		Fabric          device.FabricInfo
		Eligible        bool
		RejectionReason string
	}{
		StableID:        observed.StableID,
		PCI:             observed.PCI,
		ChipSeries:      observed.ChipSeries,
		CardSeries:      observed.CardSeries,
		FirmwareVersion: observed.FirmwareVersion,
		KMDVersion:      observed.KMDVersion,
		Memory:          observed.Memory,
		Compute:         observed.Compute,
		Health:          observed.Health,
		Fault:           observed.Fault,
		Fabric:          observed.Fabric,
		Eligible:        observed.Eligible,
		RejectionReason: observed.RejectionReason,
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
