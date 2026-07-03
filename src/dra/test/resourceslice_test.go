package test

import (
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/device"
	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
)

func TestResourceSliceModelUsesDefaultsAndMapsDevices(t *testing.T) {
	model := dra.NewResourceSliceModel("", "node-a", []device.Node{
		{ID: "0", Path: "/dev/tenstorrent/0", Major: 241, Minor: 0},
	})

	if model.DriverName != dra.DefaultDriverName {
		t.Fatalf("DriverName = %q, want %q", model.DriverName, dra.DefaultDriverName)
	}
	if model.NodeName != "node-a" {
		t.Fatalf("NodeName = %q, want node-a", model.NodeName)
	}
	if len(model.Devices) != 1 {
		t.Fatalf("Devices length = %d, want 1", len(model.Devices))
	}

	got := model.Devices[0]
	if got.Name != "tt-0" || got.Path != "/dev/tenstorrent/0" || got.Major != 241 || got.Minor != 0 {
		t.Fatalf("device mapping = %#v", got)
	}
	if stringAttribute(t, got.Attributes[dra.DeviceAttributeDeviceID]) != "0" {
		t.Fatalf("device-id attribute = %#v, want 0", got.Attributes[dra.DeviceAttributeDeviceID])
	}
}

func TestResourceSliceModelMapsOptionalChipAttributes(t *testing.T) {
	model := dra.NewResourceSliceModel("", "node-a", []device.Node{
		{
			ID:         "0",
			Path:       "/dev/tenstorrent/0",
			Major:      241,
			Minor:      0,
			ChipSeries: "blackhole",
			CardSeries: "p150",
		},
	})

	got := model.Devices[0].Attributes
	if stringAttribute(t, got[dra.DeviceAttributeChipSeries]) != "blackhole" {
		t.Fatalf("chip series attribute = %#v, want blackhole", got[dra.DeviceAttributeChipSeries])
	}
	if stringAttribute(t, got[dra.DeviceAttributeCardSeries]) != "p150" {
		t.Fatalf("card series attribute = %#v, want p150", got[dra.DeviceAttributeCardSeries])
	}
}

func TestResourceSliceModelAddsComputeClassCapacity(t *testing.T) {
	model := dra.NewResourceSliceModel("", "node-a", []device.Node{
		{
			ID:         "0",
			Path:       "/dev/tenstorrent/0",
			Major:      241,
			Minor:      0,
			ChipSeries: "wormhole",
			CardSeries: "n300",
		},
	})

	got := model.Devices[0]
	if stringAttribute(t, got.Attributes[dra.DeviceAttributeChipSeries]) != "wormhole" {
		t.Fatalf("chip series = %#v, want wormhole", got.Attributes[dra.DeviceAttributeChipSeries])
	}
	if capacityValue(got.Capacity[dra.DeviceCapacityTensixCores]) != "128" {
		t.Fatalf("tensix capacity = %q, want 128", got.Capacity[dra.DeviceCapacityTensixCores])
	}
	if capacityValue(got.Capacity[dra.DeviceCapacityMemoryBytes]) != "24G" {
		t.Fatalf("memory capacity = %q, want 24G", got.Capacity[dra.DeviceCapacityMemoryBytes])
	}
	if capacityValue(got.Capacity[dra.DeviceCapacityMemoryBandwidthBytesPerSec]) != "576G" {
		t.Fatalf("memory bandwidth = %q, want 576G", got.Capacity[dra.DeviceCapacityMemoryBandwidthBytesPerSec])
	}
	if boolAttribute(t, got.Attributes[dra.DeviceAttributeConnectivity]) != true {
		t.Fatalf("connectivity = %#v, want true", got.Attributes[dra.DeviceAttributeConnectivity])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeWarpInterfaceCount]) != 2 {
		t.Fatalf("warp interface count = %#v, want 2", got.Attributes[dra.DeviceAttributeWarpInterfaceCount])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeWarpSpeedGbps]) != 100 {
		t.Fatalf("warp speed = %#v, want 100", got.Attributes[dra.DeviceAttributeWarpSpeedGbps])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeQSFPInterfaceCount]) != 2 {
		t.Fatalf("qsfp interface count = %#v, want 2", got.Attributes[dra.DeviceAttributeQSFPInterfaceCount])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeQSFPSpeedGbps]) != 200 {
		t.Fatalf("qsfp speed = %#v, want 200", got.Attributes[dra.DeviceAttributeQSFPSpeedGbps])
	}
	if stringAttribute(t, got.Attributes[dra.DeviceAttributeSystemInterfaceType]) != "PCIe 4.0" {
		t.Fatalf("system interface type = %#v, want PCIe 4.0", got.Attributes[dra.DeviceAttributeSystemInterfaceType])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeSystemInterfaceCount]) != 16 {
		t.Fatalf("system interface count = %#v, want 16", got.Attributes[dra.DeviceAttributeSystemInterfaceCount])
	}
}

func stringAttribute(t *testing.T, attribute dra.DeviceAttribute) string {
	t.Helper()
	if attribute.String == nil {
		t.Fatalf("attribute %#v has nil String", attribute)
	}
	return *attribute.String
}

func intAttribute(t *testing.T, attribute dra.DeviceAttribute) int64 {
	t.Helper()
	if attribute.Int == nil {
		t.Fatalf("attribute %#v has nil Int", attribute)
	}
	return *attribute.Int
}

func boolAttribute(t *testing.T, attribute dra.DeviceAttribute) bool {
	t.Helper()
	if attribute.Bool == nil {
		t.Fatalf("attribute %#v has nil Bool", attribute)
	}
	return *attribute.Bool
}

func capacityValue(capacity dra.DeviceCapacity) string {
	return capacity.Value
}
