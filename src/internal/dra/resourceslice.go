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
		AttributeCardSeries: stringAttribute(item.CardSeries),
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
	if profile, ok := CardSpecForClass(item.ChipSeries, item.CardSeries); ok {
		cores := profile.TensixCores
		if item.Compute.TensixCores > 0 {
			cores = int64(item.Compute.TensixCores)
		}
		attrs[AttributeTensixCoreCount] = intAttribute(cores)
		attrs[AttributeGDDRControllerCount] = intAttribute(profile.GDDRControllerCount())
		attrs[AttributeGDDRControllersPerASIC] = intAttribute(profile.GDDRControllersPerASIC)
		attrs[AttributeAIClockMHz] = intAttribute(profile.AIClockMHz)
		attrs[AttributeMemoryType] = stringAttribute(profile.MemoryType)
		attrs[AttributeConnectivity] = boolAttribute(profile.Connectivity)
		attrs[AttributeSystemInterfaceType] = stringAttribute(profile.SystemInterfaceType)
		if profile.BigRISCV > 0 {
			attrs[AttributeBigRISCVCoreCount] = intAttribute(profile.BigRISCV)
		}
		if profile.WarpInterfaceCount > 0 {
			attrs[AttributeWarpInterfaceCount] = intAttribute(profile.WarpInterfaceCount)
		}
		if profile.QSFPInterfaceCount > 0 {
			attrs[AttributeQSFPInterfaceCount] = intAttribute(profile.QSFPInterfaceCount)
		}
		for name, value := range profile.Capacity() {
			capacities[name] = value
		}
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
func boolAttribute(value bool) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{BoolValue: &value}
}
func setString(values map[resourceapi.QualifiedName]resourceapi.DeviceAttribute, name, value string) {
	if value != "" {
		values[resourceapi.QualifiedName(name)] = stringAttribute(value)
	}
}
func quantityCapacity(value *resource.Quantity) resourceapi.DeviceCapacity {
	return resourceapi.DeviceCapacity{Value: *value}
}

func StringAttribute(value string) resourceapi.DeviceAttribute { return stringAttribute(value) }
func IntAttribute(value int64) resourceapi.DeviceAttribute     { return intAttribute(value) }
func BoolAttribute(value bool) resourceapi.DeviceAttribute     { return boolAttribute(value) }
func CapacityValue(value int64) resourceapi.DeviceCapacity     { return CapacityValueFromString(value, "") }
func CapacityValueFromString(value int64, suffix string) resourceapi.DeviceCapacity {
	return resourceapi.DeviceCapacity{Value: resource.MustParse(resource.NewQuantity(value, resource.DecimalSI).String() + suffix)}
}
