package test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
)

// TestBuildSnapshotNormalizesAndSortsStableDevices verifies canonical values and stable PCI ordering.
func TestBuildSnapshotNormalizesAndSortsStableDevices(t *testing.T) {
	provider := device.StaticProvider{Devices: []device.RawDevice{
		inventoryRaw("0000:00:02.0", "wormhole", "Healthy", true),
		inventoryRaw("0000:00:01.0", "bh", "ok", true),
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
	if got := snapshot.Devices[0].ChipSeries; got != "blackhole" {
		t.Fatalf("normalized chip series = %q, want blackhole", got)
	}
	if !snapshot.Devices[0].Eligible || !snapshot.Devices[1].Eligible {
		t.Fatalf("healthy known devices should be eligible: %#v", snapshot.Devices)
	}
	if snapshot.Devices[0].Provenance["pci"].Path == "" {
		t.Fatal("PCI provenance path is empty")
	}
	for _, field := range []string{"stableID", "characterDevice", "pci", "chipSeries", "memory", "compute", "health", "fabric", "node", "observedAt"} {
		provenance, ok := snapshot.Devices[0].Provenance[field]
		if !ok || provenance.Source == "" || provenance.ObservedAt.IsZero() {
			t.Fatalf("missing provenance for canonical field %q: %#v", field, snapshot.Devices[0].Provenance)
		}
	}
}

// TestFilesystemProviderRequiresAbsoluteRoots verifies unsafe relative host paths are rejected.
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

// TestPCIIdentityUsesObservedLinkState verifies PCI link metadata comes from provider observations.
func TestPCIIdentityUsesObservedLinkState(t *testing.T) {
	raw := inventoryRaw("0000:00:01.0", "wormhole", "Healthy", true)
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

// TestBuildSnapshotFailsClosedPerDevice verifies discovery and health faults affect only their device.
func TestBuildSnapshotFailsClosedPerDevice(t *testing.T) {
	badHealth := inventoryRaw("0000:00:01.0", "wormhole", "failed", true)
	missingChar := inventoryRaw("0000:00:02.0", "wormhole", "Healthy", false)
	missingIdentity := inventoryRaw("0000:00:03.0", "", "Healthy", true)
	missingIdentity.Values["pci.device"] = "0xffff"

	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{badHealth, missingChar, missingIdentity}})
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

// TestBuildSnapshotRejectsDuplicateStableIdentity verifies colliding PCI identities are all ineligible.
func TestBuildSnapshotRejectsDuplicateStableIdentity(t *testing.T) {
	first := inventoryRaw("0000:00:01.0", "wormhole", "Healthy", true)
	second := inventoryRaw("0000:00:01.0", "wormhole", "Healthy", true)
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

// TestBuildSnapshotDoesNotInventMissingCapacity verifies absent memory and compute data remain zero.
func TestBuildSnapshotDoesNotInventMissingCapacity(t *testing.T) {
	raw := inventoryRaw("0000:00:04.0", "wormhole", "Healthy", true)
	delete(raw.Values, "memory_capacity_bytes")
	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{raw}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Devices[0].Memory.TotalBytes; got != 0 {
		t.Fatalf("missing memory was synthesized as %d bytes", got)
	}
}

// TestFilesystemAndStaticProvidersHaveEquivalentCanonicalSemantics verifies provider-independent normalization.
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
	staticSnapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{inventoryRaw("0000:00:01.0", "wormhole", "Healthy", false)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := canonicalSemantics(filesystemSnapshot.Devices[0]), canonicalSemantics(staticSnapshot.Devices[0]); got != want {
		t.Fatalf("filesystem and static canonical semantics differ:\n got: %s\nwant: %s", got, want)
	}
}

// TestInventorySnapshotJSONRoundTrip verifies the canonical inventory model survives JSON encoding.
func TestInventorySnapshotJSONRoundTrip(t *testing.T) {
	snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{inventoryRaw("0000:00:01.0", "wormhole", "Healthy", true)}})
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

// TestInventoryNormalizationTable exercises supported aliases and malformed observed values.
func TestInventoryNormalizationTable(t *testing.T) {
	tests := []struct {
		name, chip, health string
		eligible           bool
		wantChip           string
	}{
		{name: "wormhole variant", chip: "WH", health: "ready", eligible: true, wantChip: "wormhole"},
		{name: "blackhole variant", chip: "bh", health: "ok", eligible: true, wantChip: "blackhole"},
		{name: "unknown chip", chip: " Mystery ", health: "healthy", wantChip: "mystery"},
		{name: "missing sysfs identity", chip: "", health: "healthy", eligible: true, wantChip: "wormhole"},
		{name: "unknown health", chip: "wormhole", health: "", eligible: true, wantChip: "wormhole"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{inventoryRaw("0000:00:01.0", testCase.chip, testCase.health, true)}})
			if err != nil {
				t.Fatal(err)
			}
			observed := snapshot.Devices[0]
			if observed.Eligible != testCase.eligible || observed.ChipSeries != testCase.wantChip {
				t.Fatalf("got eligible=%v chip=%q reason=%q", observed.Eligible, observed.ChipSeries, observed.RejectionReason)
			}
		})
	}
}

// TestInventoryDerivesMissingIdentityFromKnownPCI verifies known PCI IDs fill missing chip identity.
func TestInventoryDerivesMissingIdentityFromKnownPCI(t *testing.T) {
	tests := []struct {
		name, pciDevice, wantChip string
		eligible                  bool
	}{
		{name: "wormhole", pciDevice: "0x401e", wantChip: "wormhole", eligible: true},
		{name: "blackhole", pciDevice: "0xb140", wantChip: "blackhole", eligible: true},
		{name: "unknown device", pciDevice: "0xffff", eligible: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			raw := inventoryRaw("0000:00:01.0", "", "", true)
			raw.Values["pci.device"] = testCase.pciDevice
			snapshot, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{raw}})
			if err != nil {
				t.Fatal(err)
			}
			observed := snapshot.Devices[0]
			if observed.Eligible != testCase.eligible || observed.ChipSeries != testCase.wantChip {
				t.Fatalf("got eligible=%v chip=%q reason=%q", observed.Eligible, observed.ChipSeries, observed.RejectionReason)
			}
		})
	}
}

// FuzzBuildSnapshotNeverPanics checks arbitrary observation strings cannot crash normalization.
func FuzzBuildSnapshotNeverPanics(f *testing.F) {
	f.Add("0000:00:01.0", "wormhole", "Healthy", "0x1e52")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, bdf, chip, health, vendor string) {
		_, err := device.BuildSnapshot(context.Background(), device.StaticProvider{Devices: []device.RawDevice{{
			ID:                     bdf,
			CharacterDevicePresent: true,
			Node:                   device.Node{ID: bdf, Path: "/dev/tenstorrent/0", Major: 226, Minor: 0},
			Values: map[string]string{
				"pci.PCI_SLOT_NAME": bdf,
				"pci.vendor":        vendor,
				"architecture":      chip,
				"health":            health,
			},
		}}})
		if err != nil {
			t.Fatalf("unexpected provider error: %v", err)
		}
	})
}

// TestFilesystemProviderReadsSyntheticSysfs verifies discovery against a representative synthetic host tree.
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
	writeInventoryValue(t, filepath.Join(devicePath, "health"), "Healthy\n")
	writeInventoryValue(t, filepath.Join(pciPath, "PCI_SLOT_NAME"), "0000:00:01.0\n")
	writeInventoryValue(t, filepath.Join(pciPath, "vendor"), "0x1e52\n")
	writeInventoryValue(t, filepath.Join(pciPath, "device"), "0x401e\n")
	writeInventoryValue(t, filepath.Join(pciPath, "numa_node"), "0\n")
	if err := os.Symlink(pciPath, filepath.Join(devicePath, "device")); err != nil {
		t.Fatal(err)
	}
	iommuGroup := filepath.Join(root, "sys", "kernel", "iommu_groups", "7")
	if err := os.MkdirAll(filepath.Join(iommuGroup, "devices"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(iommuGroup, filepath.Join(pciPath, "iommu_group")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pciPath, filepath.Join(iommuGroup, "devices", "0000:00:01.0")); err != nil {
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
	if observed.ChipSeries != "wormhole" {
		t.Fatalf("identity = %s, want wormhole", observed.ChipSeries)
	}
	if observed.PCI.IOMMUGroup != 7 || observed.PCI.IOMMUGroupSize != 1 {
		t.Fatalf("IOMMU group = %d/%d, want 7/1", observed.PCI.IOMMUGroup, observed.PCI.IOMMUGroupSize)
	}
	if observed.CharacterDevicePresent {
		t.Fatal("synthetic tree without a character node should not be allocatable")
	}
	if observed.Eligible || observed.RejectionReason != "character device is missing" {
		t.Fatalf("unexpected eligibility: eligible=%v reason=%q", observed.Eligible, observed.RejectionReason)
	}
}

// TestFilesystemProviderRejectsPCIPathEscape verifies PCI symlinks cannot escape the configured root.
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

// inventoryRaw constructs a configurable raw Tenstorrent observation for normalization tests.
func inventoryRaw(bdf, chip, health string, characterDevice bool) device.RawDevice {
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
			"health":                health,
			"memory_capacity_bytes": "1234",
			"tensix_cores_total":    "72",
		},
	}
}

// writeInventoryValue creates one synthetic sysfs value and any missing parent directories.
func writeInventoryValue(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// canonicalSemantics serializes provider-independent fields for concise equality checks.
func canonicalSemantics(observed device.InventoryDevice) string {
	value := struct {
		StableID        string
		PCI             device.PCIIdentity
		ChipSeries      string
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
