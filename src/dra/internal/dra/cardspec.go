package dra

import (
	resourceapi "k8s.io/api/resource/v1"
)

const (
	DeviceCapacityASICs                      = DeviceAttributeDomain + "/asics"
	DeviceCapacitySRAMBytes                  = DeviceAttributeDomain + "/sramBytes"
	DeviceCapacityMemoryBytes                = DeviceAttributeDomain + "/memoryBytes"
	DeviceCapacityMemorySpeedGTPerSecond     = DeviceAttributeDomain + "/memorySpeedGTPerSecond"
	DeviceCapacityMemoryBandwidthBytesPerSec = DeviceAttributeDomain + "/memoryBandwidthBytesPerSecond"
	DeviceCapacityFP8TeraFLOPS               = DeviceAttributeDomain + "/fp8TeraFLOPS"
	DeviceCapacityFP16TeraFLOPS              = DeviceAttributeDomain + "/fp16TeraFLOPS"
	DeviceCapacityBlockFP8TeraFLOPS          = DeviceAttributeDomain + "/blockFP8TeraFLOPS"
	DeviceCapacityBoardPowerWatts            = DeviceAttributeDomain + "/boardPowerWatts"
)

// CardSpec captures compute-relevant specifications for a Tenstorrent card
// class. It intentionally ignores physical variants such as cooling, dimensions,
// and power connectors because those do not change DRA scheduling capability.
type CardSpec struct {
	ChipSeries              string `json:"chipSeries"`
	CardSeries              string `json:"cardSeries"`
	ASICCount               int64  `json:"asicCount"`
	TensixCores             int64  `json:"tensixCores"`
	BigRISCV                int64  `json:"bigRiscv,omitempty"`
	GDDRControllersPerASIC  int64  `json:"gddrControllersPerASIC"`
	AIClockMHz              int64  `json:"aiClockMHz"`
	SRAMMB                  int64  `json:"sramMB"`
	MemoryGB                int64  `json:"memoryGB"`
	MemoryType              string `json:"memoryType"`
	MemorySpeedGTPerSecond  int64  `json:"memorySpeedGTPerSecond"`
	MemoryBandwidthGBPerSec int64  `json:"memoryBandwidthGBPerSecond"`
	FP8TeraFLOPS            int64  `json:"fp8TeraFLOPS,omitempty"`
	FP16TeraFLOPS           int64  `json:"fp16TeraFLOPS,omitempty"`
	BlockFP8TeraFLOPS       int64  `json:"blockFP8TeraFLOPS"`
	TBPWatts                int64  `json:"tbpWatts"`
	Connectivity            bool   `json:"connectivity"`
	WarpInterfaceCount      int64  `json:"warpInterfaceCount,omitempty"`
	WarpSpeedGbps           int64  `json:"warpSpeedGbps,omitempty"`
	QSFPInterfaceCount      int64  `json:"qsfpInterfaceCount,omitempty"`
	QSFPSpeedGbps           int64  `json:"qsfpSpeedGbps,omitempty"`
	SystemInterfaceType     string `json:"systemInterfaceType"`
	SystemInterfaceCount    int64  `json:"systemInterfaceCount"`
}

var SupportedCardSpecs = []CardSpec{
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n150",
		ASICCount:               1,
		TensixCores:             72,
		GDDRControllersPerASIC:  6,
		AIClockMHz:              1000,
		SRAMMB:                  108,
		MemoryGB:                12,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  12,
		MemoryBandwidthGBPerSec: 288,
		FP8TeraFLOPS:            262,
		FP16TeraFLOPS:           74,
		BlockFP8TeraFLOPS:       148,
		TBPWatts:                160,
		Connectivity:            true,
		WarpInterfaceCount:      2,
		WarpSpeedGbps:           100,
		QSFPInterfaceCount:      2,
		QSFPSpeedGbps:           200,
		SystemInterfaceType:     "PCIe 4.0",
		SystemInterfaceCount:    16,
	},
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n300",
		ASICCount:               2,
		TensixCores:             128,
		GDDRControllersPerASIC:  6,
		AIClockMHz:              1000,
		SRAMMB:                  192,
		MemoryGB:                24,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  12,
		MemoryBandwidthGBPerSec: 576,
		FP8TeraFLOPS:            466,
		FP16TeraFLOPS:           131,
		BlockFP8TeraFLOPS:       262,
		TBPWatts:                300,
		Connectivity:            true,
		WarpInterfaceCount:      2,
		WarpSpeedGbps:           100,
		QSFPInterfaceCount:      2,
		QSFPSpeedGbps:           200,
		SystemInterfaceType:     "PCIe 4.0",
		SystemInterfaceCount:    16,
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p100",
		ASICCount:               1,
		TensixCores:             120,
		BigRISCV:                16,
		GDDRControllersPerASIC:  8,
		AIClockMHz:              1350,
		SRAMMB:                  180,
		MemoryGB:                28,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  16,
		MemoryBandwidthGBPerSec: 448,
		BlockFP8TeraFLOPS:       664,
		TBPWatts:                300,
		Connectivity:            false,
		SystemInterfaceType:     "PCIe 5.0",
		SystemInterfaceCount:    16,
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p150",
		ASICCount:               1,
		TensixCores:             120,
		BigRISCV:                16,
		GDDRControllersPerASIC:  8,
		AIClockMHz:              1350,
		SRAMMB:                  180,
		MemoryGB:                32,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  16,
		MemoryBandwidthGBPerSec: 512,
		BlockFP8TeraFLOPS:       664,
		TBPWatts:                300,
		Connectivity:            true,
		QSFPInterfaceCount:      4,
		QSFPSpeedGbps:           800,
		SystemInterfaceType:     "PCIe 5.0",
		SystemInterfaceCount:    16,
	},
}

func CardSpecForClass(chipSeries, cardSeries string) (CardSpec, bool) {
	for _, spec := range SupportedCardSpecs {
		if spec.ChipSeries == chipSeries && spec.CardSeries == cardSeries {
			return spec, true
		}
	}
	return CardSpec{}, false
}

func (spec CardSpec) Attributes() map[resourceapi.QualifiedName]resourceapi.DeviceAttribute {
	attributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		DeviceAttributeChipSeries:             StringAttribute(spec.ChipSeries),
		DeviceAttributeCardSeries:             StringAttribute(spec.CardSeries),
		DeviceAttributeTensixCoreCount:        IntAttribute(spec.TensixCores),
		DeviceAttributeTensixTopology:         StringAttribute(TensixTopology2DMesh),
		DeviceAttributeTensixAllocation:       StringAttribute(TensixAllocationContiguous),
		DeviceAttributeGDDRControllerLayout:   StringAttribute(GDDRControllerLayoutLocalized),
		DeviceAttributeGDDRControllerCount:    IntAttribute(spec.GDDRControllerCount()),
		DeviceAttributeGDDRControllersPerASIC: IntAttribute(spec.GDDRControllersPerASIC),
		DeviceAttributeAIClockMHz:             IntAttribute(spec.AIClockMHz),
		DeviceAttributeMemoryType:             StringAttribute(spec.MemoryType),
		DeviceAttributeConnectivity:           BoolAttribute(spec.Connectivity),
		DeviceAttributeSystemInterfaceType:    StringAttribute(spec.SystemInterfaceType),
		DeviceAttributeSystemInterfaceCount:   IntAttribute(spec.SystemInterfaceCount),
	}
	if spec.WarpInterfaceCount > 0 {
		attributes[DeviceAttributeWarpInterfaceCount] = IntAttribute(spec.WarpInterfaceCount)
	}
	if spec.WarpSpeedGbps > 0 {
		attributes[DeviceAttributeWarpSpeedGbps] = IntAttribute(spec.WarpSpeedGbps)
	}
	if spec.QSFPInterfaceCount > 0 {
		attributes[DeviceAttributeQSFPInterfaceCount] = IntAttribute(spec.QSFPInterfaceCount)
	}
	if spec.QSFPSpeedGbps > 0 {
		attributes[DeviceAttributeQSFPSpeedGbps] = IntAttribute(spec.QSFPSpeedGbps)
	}
	if spec.BigRISCV > 0 {
		attributes[DeviceAttributeBigRISCVCoreCount] = IntAttribute(spec.BigRISCV)
	}
	return attributes
}

func (spec CardSpec) GDDRControllerCount() int64 {
	return spec.ASICCount * spec.GDDRControllersPerASIC
}

func (spec CardSpec) Capacity() map[resourceapi.QualifiedName]resourceapi.DeviceCapacity {
	capacity := map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
		DeviceCapacityASICs:                      CapacityValue(spec.ASICCount),
		DeviceCapacitySRAMBytes:                  CapacityValueFromString(spec.SRAMMB, "M"),
		DeviceCapacityMemoryBytes:                CapacityValueFromString(spec.MemoryGB, "G"),
		DeviceCapacityMemorySpeedGTPerSecond:     CapacityValue(spec.MemorySpeedGTPerSecond),
		DeviceCapacityMemoryBandwidthBytesPerSec: CapacityValueFromString(spec.MemoryBandwidthGBPerSec, "G"),
		DeviceCapacityBlockFP8TeraFLOPS:          CapacityValue(spec.BlockFP8TeraFLOPS),
		DeviceCapacityBoardPowerWatts:            CapacityValue(spec.TBPWatts),
	}
	if spec.FP8TeraFLOPS > 0 {
		capacity[DeviceCapacityFP8TeraFLOPS] = CapacityValue(spec.FP8TeraFLOPS)
	}
	if spec.FP16TeraFLOPS > 0 {
		capacity[DeviceCapacityFP16TeraFLOPS] = CapacityValue(spec.FP16TeraFLOPS)
	}
	return capacity
}
