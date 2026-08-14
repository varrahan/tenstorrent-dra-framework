package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

// TestLifecycleManagerPrepareIsIdempotentAndIsolated verifies exclusive ownership, CDI state, and safe release.
func TestLifecycleManagerPrepareIsIdempotentAndIsolated(t *testing.T) {
	root := t.TempDir()
	snapshot := device.InventorySnapshot{ObservedAt: time.Now().UTC(), Devices: []device.InventoryDevice{{
		Node:                   device.Node{ID: "0", Path: "/dev/tenstorrent/0", ChipSeries: "wormhole", Major: 241, Minor: 0},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	}}}
	m, err := lifecycle.NewManager(lifecycle.Config{NodeName: "node-a", Driver: "dra.tenstorrent.com", StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"), Resetter: lifecycle.NoopResetter{}, Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil }})
	if err != nil {
		t.Fatal(err)
	}
	uid := types.UID("claim-uid")
	claim := &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "claim", UID: uid}, Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{{Request: "accelerator", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-wormhole-0"}}}}}}
	result, err := m.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil {
		t.Fatal(err)
	}
	if result[uid].Err != nil || len(result[uid].Devices) != 1 {
		t.Fatalf("unexpected prepare result: %#v", result[uid])
	}
	if stats := m.SnapshotStats(); stats.Allocated != 1 || stats.Quarantined != 0 {
		t.Fatalf("prepared lifecycle stats = %#v", stats)
	}
	m.HandleError(context.Background(), errors.New("recoverable kubelet reconnect"), "NodePrepareResources")
	firstID := result[uid].Devices[0].CDIDeviceIDs[0]
	repeated, err := m.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || repeated[uid].Err != nil || repeated[uid].Devices[0].CDIDeviceIDs[0] != firstID {
		t.Fatalf("prepare was not idempotent: %#v %v", repeated[uid], err)
	}
	other := *claim
	other.UID = types.UID("other-claim")
	conflict, err := m.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{&other})
	if err != nil || conflict[other.UID].Err == nil {
		t.Fatalf("duplicate device allocation was accepted: %#v %v", conflict[other.UID], err)
	}
	cdiData, err := os.ReadFile(filepath.Join(root, "cdi", "claim-claim-uid.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		CDIVersion string `json:"cdiVersion"`
		Kind       string `json:"kind"`
		Devices    []struct {
			Name           string `json:"name"`
			ContainerEdits struct {
				DeviceNodes []struct {
					Path     string  `json:"path"`
					FileMode *uint32 `json:"fileMode"`
				} `json:"deviceNodes"`
			} `json:"containerEdits"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(cdiData, &spec); err != nil || spec.CDIVersion != "0.6.0" || spec.Kind != "tenstorrent.com/accelerator" || len(spec.Devices) != 1 || len(spec.Devices[0].ContainerEdits.DeviceNodes) != 1 || spec.Devices[0].ContainerEdits.DeviceNodes[0].Path != "/dev/tenstorrent/0" || spec.Devices[0].ContainerEdits.DeviceNodes[0].FileMode == nil || *spec.Devices[0].ContainerEdits.DeviceNodes[0].FileMode != 0o600 {
		t.Fatalf("unexpected CDI spec: %s", cdiData)
	}
	cdiInfo, err := os.Stat(filepath.Join(root, "cdi", "claim-claim-uid.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cdiInfo.Mode().Perm(); got != 0o644 {
		t.Fatalf("CDI file mode = %v, want 0644", got)
	}
	stateInfo, err := os.Stat(filepath.Join(root, "state", "claims.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := stateInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file mode = %v, want 0600", got)
	}
	unprepared, err := m.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim"}, UID: uid}})
	if err != nil || unprepared[uid] != nil {
		t.Fatalf("unexpected unprepare result: %#v %v", unprepared, err)
	}
	if _, err := os.Stat(filepath.Join(root, "cdi", "claim-claim-uid.json")); !os.IsNotExist(err) {
		t.Fatalf("CDI file remains after unprepare: %v", err)
	}
	if stats := m.SnapshotStats(); stats.Allocated != 0 || stats.Quarantined != 0 {
		t.Fatalf("released lifecycle stats = %#v", stats)
	}
}

// TestPrepareRejectsNilAndDuplicateAllocations verifies malformed claims fail without exposing devices.
func TestPrepareRejectsNilAndDuplicateAllocations(t *testing.T) {
	root := t.TempDir()
	snapshot := device.InventorySnapshot{ObservedAt: time.Now().UTC(), Devices: []device.InventoryDevice{{
		Node: device.Node{
			ID: "0", Path: "/dev/tenstorrent/0", ChipSeries: "wormhole", Major: 241,
		},
		CharacterDevicePresent: true,
		Health:                 device.HealthHealthy,
		Eligible:               true,
	}}}
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	nilResult, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{nil})
	if err != nil || nilResult[types.UID("")].Err == nil {
		t.Fatalf("nil claim result = %#v, err=%v", nilResult, err)
	}

	allocation := resourceapi.DeviceRequestAllocationResult{
		Request: "accelerator", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-wormhole-0",
	}
	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "duplicate", UID: "duplicate-uid"},
		Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{allocation, allocation}},
		}},
	}
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err == nil {
		t.Fatalf("duplicate allocation result = %#v, err=%v", result[claim.UID], err)
	}
	if _, err := os.Stat(filepath.Join(root, "cdi", "claim-duplicate-uid.json")); !os.IsNotExist(err) {
		t.Fatalf("failed claim wrote a CDI file: %v", err)
	}
}

// TestLifecycleManagerAllowsUpgradeOverlap verifies two agent versions can
// coexist while serializing individual state transactions.
func TestLifecycleManagerAllowsUpgradeOverlap(t *testing.T) {
	root := t.TempDir()
	snapshot := lifecycleSnapshot()
	snapshot.Devices = append(snapshot.Devices, device.InventoryDevice{
		StableID: "uuid-lifecycle-device-2", Node: device.Node{ID: "1", Path: "/dev/tenstorrent/1", ChipSeries: "wormhole", Major: 241, Minor: 1},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	})
	config := lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
	}
	first, err := lifecycle.NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := lifecycle.NewManager(config)
	if err != nil {
		t.Fatalf("upgrade peer could not open shared state: %v", err)
	}
	defer second.Close()
	firstClaim := lifecycleClaim("upgrade-first")
	secondClaim := lifecycleClaim("upgrade-second")
	secondClaim.Status.Allocation.Devices.Results[0].Device = "tt-uuid-lifecycle-device-2"
	var group sync.WaitGroup
	group.Add(2)
	errorsSeen := make(chan error, 2)
	go func() {
		defer group.Done()
		result, err := first.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{firstClaim})
		if err != nil {
			errorsSeen <- err
		} else if result[firstClaim.UID].Err != nil {
			errorsSeen <- result[firstClaim.UID].Err
		}
	}()
	go func() {
		defer group.Done()
		result, err := second.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{secondClaim})
		if err != nil {
			errorsSeen <- err
		} else if result[secondClaim.UID].Err != nil {
			errorsSeen <- result[secondClaim.UID].Err
		}
	}()
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("overlapping transaction failed: %v", err)
	}
	if stats := first.SnapshotStats(); stats.Allocated != 2 {
		t.Fatalf("overlapping state lost an allocation: %#v", stats)
	}
}

// TestLifecycleStartupRepairsMissingCDI verifies prepared ownership survives agent restart.
func TestLifecycleStartupRepairsMissingCDI(t *testing.T) {
	root := t.TempDir()
	snapshot := lifecycleSnapshot()
	config := lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
	}
	manager, err := lifecycle.NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	claim := lifecycleClaim("restart-uid")
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err != nil {
		t.Fatalf("prepare failed: %#v %v", result[claim.UID], err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	cdiPath := filepath.Join(root, "cdi", "claim-restart-uid.json")
	if err := os.Remove(cdiPath); err != nil {
		t.Fatal(err)
	}
	restarted, err := lifecycle.NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, err := os.Stat(cdiPath); err != nil {
		t.Fatalf("startup did not repair CDI state: %v", err)
	}
}

// TestLifecycleRestartCompletesInterruptedRelease verifies release intent is safely retryable.
func TestLifecycleRestartCompletesInterruptedRelease(t *testing.T) {
	root := t.TempDir()
	snapshot := lifecycleSnapshot()
	failRelease := true
	config := lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
		Resetter: lifecycle.ResetFunc(func(context.Context, string) error {
			if failRelease {
				return errors.New("interrupted reset")
			}
			return nil
		}),
	}
	manager, err := lifecycle.NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	failRelease = false
	claim := lifecycleClaim("release-uid")
	prepared, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || prepared[claim.UID].Err != nil {
		t.Fatalf("prepare failed: %#v %v", prepared[claim.UID], err)
	}
	failRelease = true
	released, err := manager.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: claim.UID}})
	if err != nil || released[claim.UID] == nil {
		t.Fatalf("release interruption was not retained: %#v %v", released, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	failRelease = false
	restarted, err := lifecycle.NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	released, err = restarted.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: claim.UID}})
	if err != nil || released[claim.UID] != nil {
		t.Fatalf("release retry failed: %#v %v", released, err)
	}
}

// TestLifecycleRecoversLiveAllocationWithoutState verifies unknown ownership is quarantined.
func TestLifecycleRecoversLiveAllocationWithoutState(t *testing.T) {
	root := t.TempDir()
	snapshot := lifecycleSnapshot()
	claim := lifecycleClaim("recovered-uid")
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
		Allocations: func(context.Context) ([]*resourceapi.ResourceClaim, error) {
			return []*resourceapi.ResourceClaim{claim}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err == nil {
		t.Fatalf("recovered allocation was exposed as a new prepare: %#v %v", result[claim.UID], err)
	}
}

// TestRepeatedPrepareRevalidatesDeviceIdentity verifies retries cannot silently switch device nodes.
func TestRepeatedPrepareRevalidatesDeviceIdentity(t *testing.T) {
	root := t.TempDir()
	snapshot := lifecycleSnapshot()
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	claim := lifecycleClaim("identity-uid")
	prepared, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || prepared[claim.UID].Err != nil {
		t.Fatalf("prepare failed: %#v %v", prepared[claim.UID], err)
	}
	snapshot.Devices[0].Node.Path = "/dev/tenstorrent/replaced"
	snapshot.Devices[0].Node.Minor = 9
	repeated, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || repeated[claim.UID].Err == nil {
		t.Fatalf("identity-changing retry was accepted: %#v %v", repeated[claim.UID], err)
	}
}

// TestPrepareRetriesAfterResetFailure verifies intent remains safe and CDI appears only after a successful retry.
func TestPrepareRetriesAfterResetFailure(t *testing.T) {
	root := t.TempDir()
	failReset := true
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return lifecycleSnapshot(), nil },
		Resetter: lifecycle.ResetFunc(func(context.Context, string) error {
			if failReset {
				return errors.New("reset failed")
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	claim := lifecycleClaim("retry-uid")
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err == nil {
		t.Fatalf("reset failure was not reported: %#v %v", result[claim.UID], err)
	}
	if _, err := os.Stat(filepath.Join(root, "cdi", "claim-retry-uid.json")); !os.IsNotExist(err) {
		t.Fatalf("failed reset exposed a CDI device: %v", err)
	}
	failReset = false
	result, err = manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err != nil {
		t.Fatalf("safe retry failed: %#v %v", result[claim.UID], err)
	}
}

// TestLifecycleRecoversUnsupportedState verifies unknown schemas are preserved and hardware is quarantined.
func TestLifecycleRecoversUnsupportedState(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "claims.json"), []byte(`{"version":99,"claims":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: stateDir, CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return lifecycleSnapshot(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	backups, err := filepath.Glob(filepath.Join(stateDir, "claims.json.corrupt-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("corrupt state backup = %v, err=%v", backups, err)
	}
	state, err := os.ReadFile(filepath.Join(stateDir, "claims.json"))
	if err != nil || !strings.Contains(string(state), "lifecycle state was corrupt") {
		t.Fatalf("visible hardware was not quarantined: %s, err=%v", state, err)
	}
}

// TestLifecycleMigratesStateVersionTwo verifies supported persisted ownership upgrades in place.
func TestLifecycleMigratesStateVersionTwo(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":2,"claims":{"migration-uid":{"uid":"migration-uid","namespace":"default","name":"claim","devices":[{"pool":"node-a","device":"tt-uuid-lifecycle-device","stableID":"uuid-lifecycle-device","path":"/dev/tenstorrent/0","major":241,"minor":0,"cdiID":"tenstorrent.com/accelerator=migration-uid-tt-uuid-lifecycle-device"}]}}}`
	if err := os.WriteFile(filepath.Join(stateDir, "claims.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: stateDir, CDIDir: filepath.Join(root, "cdi"),
		Resetter:  lifecycle.NoopResetter{},
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return lifecycleSnapshot(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	state, err := os.ReadFile(filepath.Join(stateDir, "claims.json"))
	if err != nil || !strings.Contains(string(state), `"version": 4`) || !strings.Contains(string(state), `"phase": "Recovered"`) {
		t.Fatalf("legacy state was not migrated: %s, err=%v", state, err)
	}
}

// TestPreparePreservesRequestsAndIgnoresOtherDrivers verifies the kubelet can
// expose each device only to containers selecting its request in a mixed claim.
func TestPreparePreservesRequestsAndIgnoresOtherDrivers(t *testing.T) {
	root := t.TempDir()
	snapshot := lifecycleSnapshot()
	snapshot.Devices = append(snapshot.Devices, device.InventoryDevice{
		StableID: "uuid-lifecycle-device-2", Node: device.Node{ID: "1", Path: "/dev/tenstorrent/1", ChipSeries: "wormhole", Major: 241, Minor: 1},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	})
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com", StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter: lifecycle.NoopResetter{}, Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	claim := lifecycleClaim("mixed-uid")
	claim.Status.Allocation.Devices.Results = []resourceapi.DeviceRequestAllocationResult{
		{Request: "compile", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-uuid-lifecycle-device"},
		{Request: "network", Driver: "network.example.com", Pool: "node-a", Device: "nic-0"},
		{Request: "execute/fast", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-uuid-lifecycle-device-2"},
	}
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err != nil || len(result[claim.UID].Devices) != 2 {
		t.Fatalf("mixed prepare = %#v, err=%v", result[claim.UID], err)
	}
	if got := result[claim.UID].Devices[0].Requests; len(got) != 1 || got[0] != "compile" {
		t.Fatalf("first request association = %#v", got)
	}
	if got := result[claim.UID].Devices[1].Requests; len(got) != 1 || got[0] != "execute/fast" {
		t.Fatalf("second request association = %#v", got)
	}
}

// TestPrepareRejectsUnsupportedDriverSemantics verifies unsupported opaque
// configuration and administrative access fail closed before device reset.
func TestPrepareRejectsUnsupportedDriverSemantics(t *testing.T) {
	root := t.TempDir()
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com", StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter: lifecycle.NoopResetter{}, Inventory: func(context.Context) (device.InventorySnapshot, error) { return lifecycleSnapshot(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	configured := lifecycleClaim("configured-uid")
	configured.Status.Allocation.Devices.Config = []resourceapi.DeviceAllocationConfiguration{{
		Source:              resourceapi.AllocationConfigSourceClaim,
		DeviceConfiguration: resourceapi.DeviceConfiguration{Opaque: &resourceapi.OpaqueDeviceConfiguration{Driver: "dra.tenstorrent.com"}},
	}}
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{configured})
	if err != nil || result[configured.UID].Err == nil || !strings.Contains(result[configured.UID].Err.Error(), "configuration") {
		t.Fatalf("configured result = %#v, err=%v", result[configured.UID], err)
	}
	admin := lifecycleClaim("admin-uid")
	adminAccess := true
	admin.Status.Allocation.Devices.Results[0].AdminAccess = &adminAccess
	result, err = manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{admin})
	if err != nil || result[admin.UID].Err == nil || !strings.Contains(result[admin.UID].Err.Error(), "administrative") {
		t.Fatalf("admin result = %#v, err=%v", result[admin.UID], err)
	}
}

// TestHelperFatalErrorsAreEscalated verifies invalid publication errors stop
// the process while retryable kubelet reconnects do not.
func TestHelperFatalErrorsAreEscalated(t *testing.T) {
	root := t.TempDir()
	fatal := make(chan error, 1)
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com", StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Resetter: lifecycle.NoopResetter{}, Inventory: func(context.Context) (device.InventorySnapshot, error) { return lifecycleSnapshot(), nil },
		FatalError: func(err error, _ string) { fatal <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.HandleError(context.Background(), fmt.Errorf("temporary: %w", kubeletplugin.ErrRecoverable), "publish")
	select {
	case err := <-fatal:
		t.Fatalf("recoverable error escalated: %v", err)
	default:
	}
	manager.HandleError(context.Background(), errors.New("invalid ResourceSlice"), "publish")
	select {
	case <-fatal:
	default:
		t.Fatal("fatal helper error was not escalated")
	}
}

// lifecycleSnapshot returns one current healthy device for restart tests.
func lifecycleSnapshot() device.InventorySnapshot {
	return device.InventorySnapshot{ObservedAt: time.Now().UTC(), Devices: []device.InventoryDevice{{
		StableID: "uuid-lifecycle-device", Node: device.Node{ID: "0", Path: "/dev/tenstorrent/0", ChipSeries: "wormhole", Major: 241},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	}}}
}

// lifecycleClaim returns an exact local allocation for restart tests.
func lifecycleClaim(uid types.UID) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "claim", UID: uid},
		Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{{
			Request: "accelerator", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-uuid-lifecycle-device",
		}}}}},
	}
}
