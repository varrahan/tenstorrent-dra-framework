package test

import (
	"fmt"
	"testing"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	resourceapi "k8s.io/api/resource/v1"
)

func TestDriverResourcesProjectsKnownCard(t *testing.T) {
	item := eligibleDevice("0000:01:00.0", "wormhole", "n150")
	item.Compute.TensixCores = 70
	item.Memory.TotalBytes = 1234
	item.Fabric = device.FabricInfo{FabricID: "fabric-a", RingID: "ring-a", EndpointID: "endpoint-a"}

	resources := dra.DriverResources("worker-a", device.InventorySnapshot{Devices: []device.InventoryDevice{item}})
	got := resources.Pools["worker-a"].Slices[0].Devices[0]
	assertStringAttribute(t, got, dra.AttributeDeviceID, "pci-0000:01:00.0")
	assertStringAttribute(t, got, dra.AttributeNodeName, "worker-a")
	assertStringAttribute(t, got, dra.AttributeFabricID, "fabric-a")
	assertIntAttribute(t, got, dra.AttributeTensixCoreCount, 70)
	quantity := got.Capacity[resourceapi.QualifiedName(dra.DeviceCapacityMemoryBytes)].Value
	if value := quantity.Value(); value != 1234 {
		t.Fatalf("memory capacity = %d, want 1234", value)
	}
}

func TestDriverResourcesPublishesUnknownCardWithoutProfileData(t *testing.T) {
	item := eligibleDevice("0000:01:00.0", "quasar", "q950x")
	resources := dra.DriverResources("worker-a", device.InventorySnapshot{Devices: []device.InventoryDevice{item}})
	got := resources.Pools["worker-a"].Slices[0].Devices[0]
	assertStringAttribute(t, got, dra.AttributeChipSeries, "quasar")
	assertStringAttribute(t, got, dra.AttributeCardSeries, "q950x")
	if _, ok := got.Attributes[resourceapi.QualifiedName(dra.AttributeTensixCoreCount)]; ok {
		t.Fatal("unknown card received a synthesized profile")
	}
}

func TestDriverResourcesFiltersAndChunksDevices(t *testing.T) {
	devices := make([]device.InventoryDevice, 129)
	for i := range devices {
		devices[i] = eligibleDevice(fmt.Sprintf("0000:01:%02x.0", i), "wormhole", "n150")
	}
	resources := dra.DriverResources("worker-a", device.InventorySnapshot{Devices: devices})
	slices := resources.Pools["worker-a"].Slices
	if len(slices) != 2 || len(slices[0].Devices) != 128 || len(slices[1].Devices) != 1 {
		t.Fatalf("slice sizes = %d/%d/%d, want 2/128/1", len(slices), len(slices[0].Devices), len(slices[1].Devices))
	}

	devices[0].Eligible = false
	resources = dra.DriverResources("worker-a", device.InventorySnapshot{Devices: devices[:1]})
	if got := len(resources.Pools["worker-a"].Slices[0].Devices); got != 0 {
		t.Fatalf("ineligible device count = %d, want 0", got)
	}
}

func TestMatchesDeviceClass(t *testing.T) {
	tests := []struct {
		name, class, chip, card string
		want                    bool
	}{
		{name: "generic unknown", class: dra.GenericDeviceClassName, chip: "quasar", card: "q950x", want: true},
		{name: "known exact", class: "tenstorrent-wormhole-n150", chip: "wormhole", card: "n150", want: true},
		{name: "known mismatch", class: "tenstorrent-wormhole-n150", chip: "wormhole", card: "n300"},
		{name: "invented specialized class", class: "tenstorrent-quasar-q950x", chip: "quasar", card: "q950x"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := dra.MatchesDeviceClass(testCase.class, testCase.chip, testCase.card); got != testCase.want {
				t.Fatalf("MatchesDeviceClass() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func eligibleDevice(bdf, chip, card string) device.InventoryDevice {
	return device.InventoryDevice{
		StableID:               "pci-" + bdf,
		Node:                   device.Node{Path: "/dev/tenstorrent/0"},
		CharacterDevicePresent: true,
		PCI:                    device.PCIIdentity{BDF: bdf, NUMANode: 0},
		ChipSeries:             chip,
		CardSeries:             card,
		Health:                 device.HealthHealthy,
		Eligible:               true,
	}
}

func assertStringAttribute(t *testing.T, item resourceapi.Device, name, want string) {
	t.Helper()
	attribute, ok := item.Attributes[resourceapi.QualifiedName(name)]
	if !ok || attribute.StringValue == nil || *attribute.StringValue != want {
		t.Fatalf("attribute %s = %#v, want %q", name, attribute, want)
	}
}

func assertIntAttribute(t *testing.T, item resourceapi.Device, name string, want int64) {
	t.Helper()
	attribute, ok := item.Attributes[resourceapi.QualifiedName(name)]
	if !ok || attribute.IntValue == nil || *attribute.IntValue != want {
		t.Fatalf("attribute %s = %#v, want %d", name, attribute, want)
	}
}
