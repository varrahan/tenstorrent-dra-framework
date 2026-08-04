package test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func TestJanitorSanitizesBeforeAndAfterUse(t *testing.T) {
	root := t.TempDir()
	snapshot := janitorSnapshot(device.HealthHealthy)
	var resets []string
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"), RequireIOMMU: true,
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
		Resetter: lifecycle.ResetFunc(func(_ context.Context, path string) error {
			resets = append(resets, path)
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := janitorClaim("claim-uid")
	prepared, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || prepared[claim.UID].Err != nil {
		t.Fatalf("prepare = %#v, err=%v", prepared[claim.UID], err)
	}
	if len(resets) != 1 {
		t.Fatalf("preflight reset count = %d, want 1", len(resets))
	}
	unprepared, err := manager.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: claim.UID}})
	if err != nil || unprepared[claim.UID] != nil {
		t.Fatalf("unprepare = %#v, err=%v", unprepared, err)
	}
	if len(resets) != 2 {
		t.Fatalf("total reset count = %d, want 2", len(resets))
	}
	events := readAuditEvents(t, filepath.Join(root, "state", "audit.jsonl"))
	if len(events) != 4 || events[0].Action != "preflight-sanitize" || events[1].Action != "claim-prepare" || events[2].Action != "postflight-sanitize" || events[3].Action != "claim-release" {
		t.Fatalf("unexpected audit events: %#v", events)
	}
	info, err := os.Stat(filepath.Join(root, "state", "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("audit permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestJanitorRetainsOwnershipWhenPostflightSanitizationFails(t *testing.T) {
	root := t.TempDir()
	snapshot := janitorSnapshot(device.HealthHealthy)
	resetCount := 0
	failPostflight := true
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
		Resetter: lifecycle.ResetFunc(func(context.Context, string) error {
			resetCount++
			if resetCount > 1 && failPostflight {
				return errors.New("reset failed")
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := janitorClaim("claim-uid")
	prepared, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || prepared[claim.UID].Err != nil {
		t.Fatalf("prepare = %#v, err=%v", prepared[claim.UID], err)
	}
	unprepared, err := manager.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: claim.UID}})
	if err != nil || unprepared[claim.UID] == nil {
		t.Fatalf("failed postflight = %#v, err=%v", unprepared, err)
	}
	if _, err := os.Stat(filepath.Join(root, "cdi", "claim-claim-uid.json")); err != nil {
		t.Fatalf("failed sanitization released CDI ownership: %v", err)
	}
	filtered, safety, err := manager.Monitor(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Devices[0].Eligible || !safety.Unsafe {
		t.Fatalf("failed device was not fenced: %#v, %#v", filtered.Devices[0], safety)
	}
	failPostflight = false
	unprepared, err = manager.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{{UID: claim.UID}})
	if err != nil || unprepared[claim.UID] != nil {
		t.Fatalf("retry unprepare = %#v, err=%v", unprepared, err)
	}
}

func TestJanitorMonitorsAndRecoversIdleDevices(t *testing.T) {
	root := t.TempDir()
	snapshot := janitorSnapshot(device.HealthUnhealthy)
	failReset := true
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
		Resetter: lifecycle.ResetFunc(func(context.Context, string) error {
			if failReset {
				return errors.New("still unhealthy")
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	filtered, safety, err := manager.Monitor(context.Background(), snapshot)
	if err == nil || filtered.Devices[0].Eligible || !safety.Unsafe {
		t.Fatalf("unhealthy monitor = eligible=%v safety=%#v err=%v", filtered.Devices[0].Eligible, safety, err)
	}
	failReset = false
	snapshot = janitorSnapshot(device.HealthHealthy)
	filtered, safety, err = manager.Monitor(context.Background(), snapshot)
	if err != nil || !filtered.Devices[0].Eligible || safety.Unsafe {
		t.Fatalf("recovered monitor = eligible=%v safety=%#v err=%v", filtered.Devices[0].Eligible, safety, err)
	}
	snapshot.Devices[0].Fault.Code = "OOM"
	filtered, safety, err = manager.Monitor(context.Background(), snapshot)
	if filtered.Devices[0].Eligible || !safety.Unsafe {
		t.Fatalf("hardware fault was not fenced: %#v, %#v, %v", filtered.Devices[0], safety, err)
	}
	snapshot.Devices[0].Fault.Code = ""
	if _, _, err := manager.Monitor(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Devices[0].Fabric.Links = []device.FabricLink{{Name: "eth0", State: "down"}}
	filtered, safety, err = manager.Monitor(context.Background(), snapshot)
	if filtered.Devices[0].Eligible || !safety.Unsafe {
		t.Fatalf("failed fabric link was not fenced: %#v, %#v, %v", filtered.Devices[0], safety, err)
	}
}

func TestJanitorRequiresDedicatedIOMMUGroup(t *testing.T) {
	root := t.TempDir()
	snapshot := janitorSnapshot(device.HealthHealthy)
	snapshot.Devices[0].PCI.IOMMUGroupSize = 2
	resetCalled := false
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"), RequireIOMMU: true,
		Inventory: func(context.Context) (device.InventorySnapshot, error) { return snapshot, nil },
		Resetter: lifecycle.ResetFunc(func(context.Context, string) error {
			resetCalled = true
			return nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := janitorClaim("claim-uid")
	result, err := manager.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	if err != nil || result[claim.UID].Err == nil || resetCalled {
		t.Fatalf("shared IOMMU group result = %#v, reset=%v, err=%v", result[claim.UID], resetCalled, err)
	}
}

func TestInventoryFailureFencesKnownCapacity(t *testing.T) {
	root := t.TempDir()
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: "node-a", Driver: "dra.tenstorrent.com",
		StateDir: filepath.Join(root, "state"), CDIDir: filepath.Join(root, "cdi"),
		Inventory: func(context.Context) (device.InventorySnapshot, error) {
			return janitorSnapshot(device.HealthHealthy), nil
		},
		Resetter: lifecycle.NoopResetter{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, safety, err := manager.Monitor(context.Background(), janitorSnapshot(device.HealthHealthy)); err != nil || safety.Unsafe {
		t.Fatalf("healthy monitor safety=%#v err=%v", safety, err)
	}
	filtered, safety, err := manager.InventoryFailed(errors.New("sysfs unavailable"))
	if err != nil || len(filtered.Devices) != 0 || !safety.Unsafe {
		t.Fatalf("inventory failure = devices=%d safety=%#v err=%v", len(filtered.Devices), safety, err)
	}
}

func TestNodeSafetyConditionAndTaint(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	unsafe := lifecycle.Safety{Unsafe: true, Reason: "NoHealthyDevices", Message: "no healthy devices"}
	if err := lifecycle.UpdateNodeSafety(ctx, client, "node-a", unsafe); err != nil {
		t.Fatal(err)
	}
	node, err := client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(node.Spec.Taints) != 1 || node.Spec.Taints[0].Key != lifecycle.NodeTaintKey {
		t.Fatalf("unsafe taints = %#v", node.Spec.Taints)
	}
	if conditionStatus(node) != corev1.ConditionFalse {
		t.Fatalf("unsafe condition = %#v", node.Status.Conditions)
	}
	if err := lifecycle.UpdateNodeSafety(ctx, client, "node-a", lifecycle.Safety{Reason: "DevicesHealthy", Message: "healthy"}); err != nil {
		t.Fatal(err)
	}
	node, err = client.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(node.Spec.Taints) != 0 || conditionStatus(node) != corev1.ConditionTrue {
		t.Fatalf("safe node was not restored: %#v", node)
	}
}

func janitorSnapshot(health device.HealthState) device.InventorySnapshot {
	return device.InventorySnapshot{ObservedAt: metav1.Now().Time, Devices: []device.InventoryDevice{{
		StableID:               "pci-0000:00:01.0",
		Node:                   device.Node{ID: "0", Path: "/dev/tenstorrent/0", Major: 226},
		CharacterDevicePresent: true, Health: health, Eligible: health == device.HealthHealthy,
		PCI: device.PCIIdentity{BDF: "0000:00:01.0", IOMMUGroup: 7, IOMMUGroupSize: 1}, ChipSeries: "wormhole",
	}}}
}

func janitorClaim(uid types.UID) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "claim", UID: uid},
		Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{Results: []resourceapi.DeviceRequestAllocationResult{{
				Request: "accelerator", Driver: "dra.tenstorrent.com", Pool: "node-a", Device: "tt-pci-0000-00-01-0",
			}}},
		}},
	}
}

func readAuditEvents(t *testing.T, path string) []lifecycle.AuditEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []lifecycle.AuditEvent
	for _, line := range bytesLines(data) {
		var event lifecycle.AuditEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func bytesLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for index, value := range data {
		if value != '\n' {
			continue
		}
		if index > start {
			lines = append(lines, data[start:index])
		}
		start = index + 1
	}
	return lines
}

func conditionStatus(node *corev1.Node) corev1.ConditionStatus {
	for _, condition := range node.Status.Conditions {
		if condition.Type == lifecycle.NodeConditionType {
			return condition.Status
		}
	}
	return corev1.ConditionUnknown
}
