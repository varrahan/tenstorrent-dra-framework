package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/dynamic-resource-allocation/resourceslice"
)

func TestConfirmResourcePublicationRequiresExactCompletePool(t *testing.T) {
	nodeName := "node-a"
	snapshot := device.InventorySnapshot{ObservedAt: time.Now().UTC(), Devices: []device.InventoryDevice{{
		StableID: "uuid-published", Node: device.Node{ID: "0", Path: "/dev/tenstorrent/0", ChipSeries: "wormhole", Major: 241},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	}, {
		StableID: "uuid-published-second", Node: device.Node{ID: "1", Path: "/dev/tenstorrent/1", ChipSeries: "blackhole", Major: 241, Minor: 1},
		CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	}}}
	desired := dra.DriverResourcesAt(nodeName, snapshot, time.Minute, time.Now())
	node := nodeName
	stored := &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "published"},
		Spec: resourceapi.ResourceSliceSpec{
			Driver: dra.DefaultDriverName, NodeName: &node,
			Pool:    resourceapi.ResourcePool{Name: nodeName, Generation: 3, ResourceSliceCount: 1},
			Devices: desired.Pools[nodeName].Slices[0].Devices,
		},
	}
	unrelatedNode := "node-b"
	unrelated := &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated"},
		Spec: resourceapi.ResourceSliceSpec{
			Driver: dra.DefaultDriverName, NodeName: &unrelatedNode,
			Pool: resourceapi.ResourcePool{Name: unrelatedNode, Generation: 1, ResourceSliceCount: 1},
		},
	}
	client := fake.NewSimpleClientset(stored, unrelated)
	if err := dra.ConfirmResourcePublication(context.Background(), client, nodeName, dra.DefaultDriverName, desired); err != nil {
		t.Fatalf("exact publication was not confirmed: %v", err)
	}

	stored.Spec.Devices = nil
	client = fake.NewSimpleClientset(stored)
	err := dra.ConfirmResourcePublication(context.Background(), client, nodeName, dra.DefaultDriverName, desired)
	if err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("stale publication was accepted: %v", err)
	}
}

func TestConfirmResourcePublicationRejectsWrongPoolAndIncompleteSlices(t *testing.T) {
	nodeName := "node-a"
	desired := dra.DriverResourcesAt(nodeName, device.InventorySnapshot{ObservedAt: time.Now().UTC()}, time.Minute, time.Now())
	node := nodeName
	stored := &resourceapi.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "wrong-pool"},
		Spec: resourceapi.ResourceSliceSpec{
			Driver: dra.DefaultDriverName, NodeName: &node,
			Pool: resourceapi.ResourcePool{Name: "another-pool", Generation: 1, ResourceSliceCount: 2},
		},
	}
	err := dra.ConfirmResourcePublication(context.Background(), fake.NewSimpleClientset(stored), nodeName, dra.DefaultDriverName, desired)
	if err == nil || !strings.Contains(err.Error(), "uses pool") {
		t.Fatalf("wrong publication pool was accepted: %v", err)
	}
}

func TestConfirmResourcePublicationRejectsMissingDesiredPoolAndListFailure(t *testing.T) {
	empty := resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{}}
	if err := dra.ConfirmResourcePublication(context.Background(), fake.NewSimpleClientset(), "node-a", dra.DefaultDriverName, empty); err == nil || !strings.Contains(err.Error(), "do not contain") {
		t.Fatalf("missing desired pool was accepted: %v", err)
	}

	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "resourceslices", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API unavailable")
	})
	desired := dra.DriverResourcesAt("node-a", device.InventorySnapshot{ObservedAt: time.Now().UTC()}, time.Minute, time.Now())
	if err := dra.ConfirmResourcePublication(context.Background(), client, "node-a", dra.DefaultDriverName, desired); err == nil || !strings.Contains(err.Error(), "API unavailable") {
		t.Fatalf("ResourceSlice list failure was hidden: %v", err)
	}
}

func TestConfirmResourcePublicationRejectsCountAndGenerationMismatch(t *testing.T) {
	nodeName := "node-a"
	node := nodeName
	desired := resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{nodeName: {
		Slices: []resourceslice.Slice{{}, {}},
	}}}
	one := &resourceapi.ResourceSlice{ObjectMeta: metav1.ObjectMeta{Name: "one"}, Spec: resourceapi.ResourceSliceSpec{
		Driver: dra.DefaultDriverName, NodeName: &node,
		Pool: resourceapi.ResourcePool{Name: nodeName, Generation: 1, ResourceSliceCount: 2},
	}}
	if err := dra.ConfirmResourcePublication(context.Background(), fake.NewSimpleClientset(one), nodeName, dra.DefaultDriverName, desired); err == nil || !strings.Contains(err.Error(), "count") {
		t.Fatalf("incomplete slice count was accepted: %v", err)
	}

	two := one.DeepCopy()
	two.Name = "two"
	two.Spec.Pool.Generation = 2
	if err := dra.ConfirmResourcePublication(context.Background(), fake.NewSimpleClientset(one, two), nodeName, dra.DefaultDriverName, desired); err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatalf("mixed pool generation was accepted: %v", err)
	}

	two.Spec.Pool.Generation = 1
	one.Spec.Pool.ResourceSliceCount = 3
	if err := dra.ConfirmResourcePublication(context.Background(), fake.NewSimpleClientset(one, two), nodeName, dra.DefaultDriverName, desired); err == nil || !strings.Contains(err.Error(), "reports pool size") {
		t.Fatalf("wrong declared pool size was accepted: %v", err)
	}
}
