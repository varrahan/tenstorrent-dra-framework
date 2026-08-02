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
		Node:                   device.Node{ID: "0", Path: "/dev/tenstorrent/0", ChipSeries: "wormhole", CardSeries: "n150", Major: 241, Minor: 0},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	}}}
	m, err := lifecycle.NewManager(lifecycle.Config{NodeName: "node-a", Driver: "dra.tenstorrent.com", StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"), Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil }})
	if err != nil {
		t.Fatal(err)
	}
	uid := types.UID("claim-uid")
	claim := &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "claim", UID: uid}, Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{{Request: "accelerator", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-wormhole-n150-0"}}}}}}
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
		Devices []struct {
			ContainerEdits struct {
				DeviceNodes []struct {
					Path string `json:"path"`
				} `json:"deviceNodes"`
			} `json:"containerEdits"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(cdiData, &spec); err != nil || len(spec.Devices) != 1 || len(spec.Devices[0].ContainerEdits.DeviceNodes) != 1 || spec.Devices[0].ContainerEdits.DeviceNodes[0].Path != "/dev/tenstorrent/0" {
		t.Fatalf("unexpected CDI spec: %s", cdiData)
	}
	unprepared, err := m.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim"}, UID: uid}})
	if err != nil || unprepared[uid] != nil {
		t.Fatalf("unexpected unprepare result: %#v %v", unprepared, err)
	}
	if _, err := os.Stat(filepath.Join(root, "cdi", "claim-claim-uid.json")); !os.IsNotExist(err) {
		t.Fatalf("CDI file remains after unprepare: %v", err)
	}
}
