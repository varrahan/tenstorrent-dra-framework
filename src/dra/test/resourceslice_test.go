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
	if got.Attributes[dra.DeviceAttributeDeviceID] != "0" {
		t.Fatalf("device-id attribute = %q, want 0", got.Attributes[dra.DeviceAttributeDeviceID])
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
	if got[dra.DeviceAttributeChipSeries] != "blackhole" {
		t.Fatalf("chip series attribute = %q, want blackhole", got[dra.DeviceAttributeChipSeries])
	}
	if got[dra.DeviceAttributeCardSeries] != "p150" {
		t.Fatalf("card series attribute = %q, want p150", got[dra.DeviceAttributeCardSeries])
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
	if got.Attributes[dra.DeviceAttributeChipSeries] != "wormhole" {
		t.Fatalf("chip series = %q, want wormhole", got.Attributes[dra.DeviceAttributeChipSeries])
	}
	if got.Capacity[dra.DeviceCapacityTensixCores] != "128" {
		t.Fatalf("tensix capacity = %q, want 128", got.Capacity[dra.DeviceCapacityTensixCores])
	}
	if got.Capacity[dra.DeviceCapacityMemoryBytes] != "24G" {
		t.Fatalf("memory capacity = %q, want 24G", got.Capacity[dra.DeviceCapacityMemoryBytes])
	}
	if got.Capacity[dra.DeviceCapacityMemoryBandwidthBytesPerSec] != "576G" {
		t.Fatalf("memory bandwidth = %q, want 576G", got.Capacity[dra.DeviceCapacityMemoryBandwidthBytesPerSec])
	}
	if got.Attributes[dra.DeviceAttributeInternalChipToChip] != "200G" {
		t.Fatalf("internal chip-to-chip = %q, want 200G", got.Attributes[dra.DeviceAttributeInternalChipToChip])
	}
}
