package test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func TestLifecycleManagerPrepareIsIdempotentAndIsolated(t *testing.T) {
	root := t.TempDir()
	snapshot := device.InventorySnapshot{Devices: []device.InventoryDevice{{
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
}

func TestPrepareRejectsNilAndDuplicateAllocations(t *testing.T) {
	root := t.TempDir()
	snapshot := device.InventorySnapshot{Devices: []device.InventoryDevice{{
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
