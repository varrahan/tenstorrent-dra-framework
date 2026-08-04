package dra

const (
	DefaultDriverName        = "dra.tenstorrent.com"
	GenericDeviceClassName   = "tenstorrent"
	WormholeDeviceClassName  = GenericDeviceClassName + "-wormhole"
	BlackholeDeviceClassName = GenericDeviceClassName + "-blackhole"
	AttributeDomain          = "tenstorrent.com"

	AttributeDeviceID         = AttributeDomain + "/deviceID"
	AttributeNodeName         = AttributeDomain + "/nodeName"
	AttributeChipSeries       = AttributeDomain + "/chipSeries"
	AttributeHealth           = AttributeDomain + "/health"
	AttributePCIBDF           = AttributeDomain + "/pciBDF"
	AttributeNUMANode         = AttributeDomain + "/numaNode"
	AttributeFabricID         = AttributeDomain + "/fabricID"
	AttributeRingID           = AttributeDomain + "/ringID"
	AttributeEndpointID       = AttributeDomain + "/endpointID"
	AttributeTensixCoreCount  = AttributeDomain + "/tensixCoreCount"
	DeviceCapacityMemoryBytes = AttributeDomain + "/memoryBytes"
)

func MatchesDeviceClass(name, chipSeries string) bool {
	switch name {
	case GenericDeviceClassName:
		return chipSeries == "wormhole" || chipSeries == "blackhole"
	case WormholeDeviceClassName:
		return chipSeries == "wormhole"
	case BlackholeDeviceClassName:
		return chipSeries == "blackhole"
	default:
		return false
	}
}
