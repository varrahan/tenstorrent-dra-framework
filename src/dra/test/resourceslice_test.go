package test

import (
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/device"
	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
	resourceapi "k8s.io/api/resource/v1"
)

func TestResourceSliceUsesDefaultsAndMapsDevices(t *testing.T) {
	slice := dra.NewResourceSliceForNodes("", "slice-a", "node-a", "node-a", []device.Node{
		{ID: "0", Path: "/dev/tenstorrent/0", Major: 241, Minor: 0},
	})

	if slice.APIVersion != "resource.k8s.io/v1" || slice.Kind != "ResourceSlice" {
		t.Fatalf("resource slice type = %s/%s, want resource.k8s.io/v1/ResourceSlice", slice.APIVersion, slice.Kind)
	}
	if slice.Spec.Driver != dra.DefaultDriverName {
		t.Fatalf("driver = %q, want %q", slice.Spec.Driver, dra.DefaultDriverName)
	}
	if slice.Spec.NodeName == nil || *slice.Spec.NodeName != "node-a" {
		t.Fatalf("node name = %#v, want node-a", slice.Spec.NodeName)
	}
	if len(slice.Spec.Devices) != 1 {
		t.Fatalf("devices length = %d, want 1", len(slice.Spec.Devices))
	}

	got := slice.Spec.Devices[0]
	if got.Name != "tt-0" {
		t.Fatalf("device mapping = %#v", got)
	}
	if stringAttribute(t, got.Attributes[dra.DeviceAttributeDeviceID]) != "0" {
		t.Fatalf("device-id attribute = %#v, want 0", got.Attributes[dra.DeviceAttributeDeviceID])
	}
	if stringAttribute(t, got.Attributes[dra.DeviceAttributePath]) != "/dev/tenstorrent/0" {
		t.Fatalf("path attribute = %#v, want /dev/tenstorrent/0", got.Attributes[dra.DeviceAttributePath])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeMajor]) != 241 {
		t.Fatalf("major attribute = %#v, want 241", got.Attributes[dra.DeviceAttributeMajor])
	}
	if intAttribute(t, got.Attributes[dra.DeviceAttributeMinor]) != 0 {
		t.Fatalf("minor attribute = %#v, want 0", got.Attributes[dra.DeviceAttributeMinor])
	}
}

func TestResourceSliceMapsOptionalChipAttributes(t *testing.T) {
	slice := dra.NewResourceSliceForNodes("", "slice-a", "node-a", "node-a", []device.Node{
		{
			ID:         "0",
			Path:       "/dev/tenstorrent/0",
			Major:      241,
			Minor:      0,
			ChipSeries: "blackhole",
			CardSeries: "p150",
		},
	})

	got := slice.Spec.Devices[0].Attributes
	if stringAttribute(t, got[dra.DeviceAttributeChipSeries]) != "blackhole" {
		t.Fatalf("chip series attribute = %#v, want blackhole", got[dra.DeviceAttributeChipSeries])
	}
	if stringAttribute(t, got[dra.DeviceAttributeCardSeries]) != "p150" {
		t.Fatalf("card series attribute = %#v, want p150", got[dra.DeviceAttributeCardSeries])
	}
}

func TestResourceSliceAddsComputeClassCapacity(t *testing.T) {
	slice := dra.NewResourceSliceForNodes("", "slice-a", "node-a", "node-a", []device.Node{
		{
			ID:         "0",
			Path:       "/dev/tenstorrent/0",
			Major:      241,
			Minor:      0,
			ChipSeries: "wormhole",
			CardSeries: "n300",
		},
	})

	got := slice.Spec.Devices[0]

	for key, want := range map[resourceapi.QualifiedName]string{
		dra.DeviceAttributeChipSeries:           "wormhole",
		dra.DeviceAttributeSystemInterfaceType:  "PCIe 4.0",
		dra.DeviceAttributeTensixTopology:       dra.TensixTopology2DMesh,
		dra.DeviceAttributeTensixAllocation:     dra.TensixAllocationContiguous,
		dra.DeviceAttributeGDDRControllerLayout: dra.GDDRControllerLayoutLocalized,
	} {
		if got := stringAttribute(t, got.Attributes[key]); got != want {
			t.Fatalf("string attribute %q = %q, want %q", key, got, want)
		}
	}

	for key, want := range map[resourceapi.QualifiedName]int64{
		dra.DeviceAttributeTensixCoreCount:        128,
		dra.DeviceAttributeGDDRControllersPerASIC: 6,
		dra.DeviceAttributeGDDRControllerCount:    12,
		dra.DeviceAttributeWarpInterfaceCount:     2,
		dra.DeviceAttributeWarpSpeedGbps:          100,
		dra.DeviceAttributeQSFPInterfaceCount:     2,
		dra.DeviceAttributeQSFPSpeedGbps:          200,
		dra.DeviceAttributeSystemInterfaceCount:   16,
	} {
		if got := intAttribute(t, got.Attributes[key]); got != want {
			t.Fatalf("int attribute %q = %d, want %d", key, got, want)
		}
	}

	for key, want := range map[resourceapi.QualifiedName]bool{
		dra.DeviceAttributeConnectivity: true,
	} {
		if got := boolAttribute(t, got.Attributes[key]); got != want {
			t.Fatalf("bool attribute %q = %t, want %t", key, got, want)
		}
	}

	for key, want := range map[resourceapi.QualifiedName]string{
		dra.DeviceCapacityMemoryBytes:                "24G",
		dra.DeviceCapacityMemoryBandwidthBytesPerSec: "576G",
	} {
		if got := capacityValue(got.Capacity[key]); got != want {
			t.Fatalf("capacity %q = %q, want %q", key, got, want)
		}
	}
	if _, ok := got.Capacity[dra.DeviceAttributeTensixCoreCount]; ok {
		t.Fatal("tensix core count must not be exposed as scalar capacity")
	}
	if _, ok := got.Capacity[dra.DeviceAttributeBigRISCVCoreCount]; ok {
		t.Fatal("big RISC-V core count must not be exposed as scalar capacity")
	}
}

func stringAttribute(t *testing.T, attribute resourceapi.DeviceAttribute) string {
	t.Helper()
	if attribute.StringValue == nil {
		t.Fatalf("attribute %#v has nil String", attribute)
	}
	return *attribute.StringValue
}

func intAttribute(t *testing.T, attribute resourceapi.DeviceAttribute) int64 {
	t.Helper()
	if attribute.IntValue == nil {
		t.Fatalf("attribute %#v has nil Int", attribute)
	}
	return *attribute.IntValue
}

func boolAttribute(t *testing.T, attribute resourceapi.DeviceAttribute) bool {
	t.Helper()
	if attribute.BoolValue == nil {
		t.Fatalf("attribute %#v has nil Bool", attribute)
	}
	return *attribute.BoolValue
}

func capacityValue(capacity resourceapi.DeviceCapacity) string {
	return capacity.Value.String()
}
