package dra

const (
	DefaultDriverName = "dra.tenstorrent.com"
	AttributeDomain   = "tenstorrent.com"

	AttributeDeviceID               = AttributeDomain + "/deviceID"
	AttributeNodeName               = AttributeDomain + "/nodeName"
	AttributeChipSeries             = AttributeDomain + "/chipSeries"
	AttributeCardSeries             = AttributeDomain + "/cardSeries"
	AttributeHealth                 = AttributeDomain + "/health"
	AttributePCIBDF                 = AttributeDomain + "/pciBDF"
	AttributeNUMANode               = AttributeDomain + "/numaNode"
	AttributeFabricID               = AttributeDomain + "/fabricID"
	AttributeRingID                 = AttributeDomain + "/ringID"
	AttributeEndpointID             = AttributeDomain + "/endpointID"
	AttributeTensixCoreCount        = AttributeDomain + "/tensixCoreCount"
	AttributeGDDRControllerCount    = AttributeDomain + "/gddrControllerCount"
	AttributeGDDRControllersPerASIC = AttributeDomain + "/gddrControllersPerASIC"
	AttributeBigRISCVCoreCount      = AttributeDomain + "/bigRISCVCoreCount"
	AttributeAIClockMHz             = AttributeDomain + "/aiClockMHz"
	AttributeMemoryType             = AttributeDomain + "/memoryType"
	AttributeConnectivity           = AttributeDomain + "/connectivity"
	AttributeWarpInterfaceCount     = AttributeDomain + "/warpInterfaceCount"
	AttributeQSFPInterfaceCount     = AttributeDomain + "/qsfpInterfaceCount"
	AttributeSystemInterfaceType    = AttributeDomain + "/systemInterfaceType"
)

// Compatibility aliases used by the card profile projection.
const (
	DeviceAttributeDomain                 = AttributeDomain
	DeviceAttributeDeviceID               = AttributeDeviceID
	DeviceAttributeChipSeries             = AttributeChipSeries
	DeviceAttributeCardSeries             = AttributeCardSeries
	DeviceAttributeFabricID               = AttributeFabricID
	DeviceAttributeRingID                 = AttributeRingID
	DeviceAttributeEndpointID             = AttributeEndpointID
	DeviceAttributeNUMANode               = AttributeNUMANode
	DeviceAttributePCIBDF                 = AttributePCIBDF
	DeviceAttributeTensixCoreCount        = AttributeTensixCoreCount
	DeviceAttributeGDDRControllerCount    = AttributeGDDRControllerCount
	DeviceAttributeGDDRControllersPerASIC = AttributeGDDRControllersPerASIC
	DeviceAttributeBigRISCVCoreCount      = AttributeBigRISCVCoreCount
	DeviceAttributeAIClockMHz             = AttributeAIClockMHz
	DeviceAttributeMemoryType             = AttributeMemoryType
	DeviceAttributeConnectivity           = AttributeConnectivity
	DeviceAttributeWarpInterfaceCount     = AttributeWarpInterfaceCount
	DeviceAttributeWarpSpeedGbps          = AttributeDomain + "/warpSpeedGbps"
	DeviceAttributeQSFPInterfaceCount     = AttributeQSFPInterfaceCount
	DeviceAttributeQSFPSpeedGbps          = AttributeDomain + "/qsfpSpeedGbps"
	DeviceAttributeSystemInterfaceType    = AttributeSystemInterfaceType
	DeviceAttributeSystemInterfaceCount   = AttributeDomain + "/systemInterfaceCount"
	DeviceAttributeTensixTopology         = AttributeDomain + "/tensixTopology"
	DeviceAttributeTensixAllocation       = AttributeDomain + "/tensixAllocation"
	DeviceAttributeGDDRControllerLayout   = AttributeDomain + "/gddrControllerLayout"
)

const (
	TensixTopology2DMesh          = "2dMesh"
	TensixAllocationContiguous    = "contiguousRegion"
	GDDRControllerLayoutLocalized = "localizedControllers"
)
