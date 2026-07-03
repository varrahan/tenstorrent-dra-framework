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

	for key, want := range map[string]string{
		dra.DeviceAttributeChipSeries:          "wormhole",
		dra.DeviceAttributeSystemInterfaceType: "PCIe 4.0",
	} {
		if got := stringAttribute(t, got.Attributes[key]); got != want {
			t.Fatalf("string attribute %q = %q, want %q", key, got, want)
		}
	}

	for key, want := range map[string]int64{
		dra.DeviceAttributeWarpInterfaceCount:   2,
		dra.DeviceAttributeWarpSpeedGbps:        100,
		dra.DeviceAttributeQSFPInterfaceCount:   2,
		dra.DeviceAttributeQSFPSpeedGbps:        200,
		dra.DeviceAttributeSystemInterfaceCount: 16,
	} {
		if got := intAttribute(t, got.Attributes[key]); got != want {
			t.Fatalf("int attribute %q = %d, want %d", key, got, want)
		}
	}

	for key, want := range map[string]bool{
		dra.DeviceAttributeConnectivity: true,
	} {
		if got := boolAttribute(t, got.Attributes[key]); got != want {
			t.Fatalf("bool attribute %q = %t, want %t", key, got, want)
		}
	}

	for key, want := range map[string]string{
		dra.DeviceCapacityTensixCores:                "128",
		dra.DeviceCapacityMemoryBytes:                "24G",
		dra.DeviceCapacityMemoryBandwidthBytesPerSec: "576G",
	} {
		if got := capacityValue(got.Capacity[key]); got != want {
			t.Fatalf("capacity %q = %q, want %q", key, got, want)
		}
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
