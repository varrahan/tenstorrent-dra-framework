package test

import (
	"context"
	"fmt"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResourceSlicePublisherFiltersAndCleansUp(t *testing.T) {
	client := fake.NewSimpleClientset()
	publisher, err := dra.NewResourceSlicePublisher(client.ResourceV1(), dra.DefaultDriverName)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := device.InventorySnapshot{Devices: []device.InventoryDevice{
		{Node: device.Node{ID: "0", Path: "/dev/tenstorrent/0"}, CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true},
		{Node: device.Node{ID: "1", Path: "/dev/tenstorrent/1"}, CharacterDevicePresent: true, Health: device.HealthUnhealthy, Eligible: true},
		{Node: device.Node{ID: "2", Path: "/dev/tenstorrent/2"}, CharacterDevicePresent: false, Health: device.HealthHealthy, Eligible: true},
	}}
	ctx := context.Background()
	if err := publisher.Publish(ctx, "node-a", snapshot); err != nil {
		t.Fatal(err)
	}
	list, err := client.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || len(list.Items[0].Spec.Devices) != 1 {
		t.Fatalf("expected one healthy device slice, got %#v", list.Items)
	}
	stale := &resourceapi.ResourceSlice{ObjectMeta: metav1.ObjectMeta{Name: "stale", Labels: map[string]string{
		"app.kubernetes.io/name": "tt-dra-driver", "app.kubernetes.io/component": "dra-resource-slice", "tenstorrent.com/node": "node-a",
	}}}
	if _, err := client.ResourceV1().ResourceSlices().Create(ctx, stale, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(ctx, "node-a", snapshot); err != nil {
		t.Fatal(err)
	}
	list, err = client.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name == "stale" {
		t.Fatalf("stale slice was not removed: %#v", list.Items)
	}
}

func TestResourceSlicePublisherPublishesAllocationAttributes(t *testing.T) {
	client := fake.NewSimpleClientset()
	publisher, err := dra.NewResourceSlicePublisher(client.ResourceV1(), dra.DefaultDriverName)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := device.InventorySnapshot{Devices: []device.InventoryDevice{{
		Node: device.Node{ID: "0", Path: "/dev/tenstorrent/0", ChipSeries: "wormhole", CardSeries: "n300"},
		PCI:  device.PCIIdentity{BDF: "0000:00:01.0", Vendor: "0x1e52"}, CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true,
	}}}
	if err := publisher.Publish(context.Background(), "node-a", snapshot); err != nil {
		t.Fatal(err)
	}
	list, err := client.ResourceV1().ResourceSlices().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attrs := list.Items[0].Spec.Devices[0].Attributes
	if _, ok := attrs[dra.DeviceAttributeTensixTopology]; !ok {
		t.Fatal("live slice is missing Tensix topology selector attribute")
	}
	if _, ok := attrs[dra.DeviceAttributeGDDRControllerLayout]; !ok {
		t.Fatal("live slice is missing GDDR layout selector attribute")
	}
}

func TestResourceSlicePublisherChunksLargeInventory(t *testing.T) {
	client := fake.NewSimpleClientset()
	publisher, err := dra.NewResourceSlicePublisher(client.ResourceV1(), dra.DefaultDriverName)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := device.InventorySnapshot{Devices: make([]device.InventoryDevice, 129)}
	for i := range snapshot.Devices {
		snapshot.Devices[i] = device.InventoryDevice{Node: device.Node{ID: fmt.Sprintf("%d", i), Path: fmt.Sprintf("/dev/tenstorrent/%d", i)}, CharacterDevicePresent: true, Health: device.HealthHealthy, Eligible: true}
	}
	if err := publisher.Publish(context.Background(), "node-a", snapshot); err != nil {
		t.Fatal(err)
	}
	list, err := client.ResourceV1().ResourceSlices().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 || len(list.Items[0].Spec.Devices)+len(list.Items[1].Spec.Devices) != 129 {
		t.Fatalf("expected two chunks totaling 129 devices, got %d slices", len(list.Items))
	}
}
