package dra

import (
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	resourceslice "k8s.io/dynamic-resource-allocation/resourceslice"
)

const maxDevicesPerSlice = 128

func DriverResources(nodeName string, snapshot device.InventorySnapshot) resourceslice.DriverResources {
	devices := make([]resourceapi.Device, 0, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		if item.Eligible && item.CharacterDevicePresent && item.Health != device.HealthUnhealthy {
			devices = append(devices, resourceDevice(nodeName, item))
		}
	}
	slices := make([]resourceslice.Slice, 0, (len(devices)+maxDevicesPerSlice-1)/maxDevicesPerSlice)
	for len(devices) > 0 {
		count := min(len(devices), maxDevicesPerSlice)
		slices = append(slices, resourceslice.Slice{Devices: devices[:count]})
		devices = devices[count:]
	}
	if len(slices) == 0 {
		slices = append(slices, resourceslice.Slice{})
	}
	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{nodeName: {Slices: slices}}}
}

func resourceDevice(nodeName string, item device.InventoryDevice) resourceapi.Device {
	attrs := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		AttributeDeviceID:   stringAttribute(item.StableID),
		AttributeNodeName:   stringAttribute(nodeName),
		AttributeChipSeries: stringAttribute(item.ChipSeries),
		AttributeHealth:     stringAttribute(string(item.Health)),
	}
	setString(attrs, AttributePCIBDF, item.PCI.BDF)
	setString(attrs, AttributeFabricID, item.Fabric.FabricID)
	setString(attrs, AttributeRingID, item.Fabric.RingID)
	setString(attrs, AttributeEndpointID, item.Fabric.EndpointID)
	if item.PCI.NUMANode >= 0 {
		attrs[AttributeNUMANode] = intAttribute(int64(item.PCI.NUMANode))
	}
	capacities := map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{}
	if item.Compute.TensixCores > 0 {
		attrs[AttributeTensixCoreCount] = intAttribute(int64(item.Compute.TensixCores))
	}
	if item.Memory.TotalBytes > 0 {
		capacities[DeviceCapacityMemoryBytes] = quantityCapacity(resource.NewQuantity(int64(item.Memory.TotalBytes), resource.BinarySI))
	}
	return resourceapi.Device{Name: device.DRAName(item), Attributes: attrs, Capacity: capacities}
}

func stringAttribute(value string) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{StringValue: &value}
}
func intAttribute(value int64) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{IntValue: &value}
}
func setString(values map[resourceapi.QualifiedName]resourceapi.DeviceAttribute, name, value string) {
	if value != "" {
		values[resourceapi.QualifiedName(name)] = stringAttribute(value)
	}
}
func quantityCapacity(value *resource.Quantity) resourceapi.DeviceCapacity {
	return resourceapi.DeviceCapacity{Value: *value}
}
