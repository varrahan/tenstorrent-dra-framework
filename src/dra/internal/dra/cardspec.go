package dra

import "fmt"

const (
	DeviceCapacityASICs                      = DeviceAttributeDomain + "/asics"
	DeviceCapacityTensixCores                = DeviceAttributeDomain + "/tensix-cores"
	DeviceCapacityBigRISCV                   = DeviceAttributeDomain + "/big-riscv-cores"
	DeviceCapacitySRAMBytes                  = DeviceAttributeDomain + "/sram-bytes"
	DeviceCapacityMemoryBytes                = DeviceAttributeDomain + "/memory-bytes"
	DeviceCapacityMemorySpeedGTPerSecond     = DeviceAttributeDomain + "/memory-speed-gtps"
	DeviceCapacityMemoryBandwidthBytesPerSec = DeviceAttributeDomain + "/memory-bandwidth-bytes-per-second"
	DeviceCapacityFP8TeraFLOPS               = DeviceAttributeDomain + "/fp8-teraflops"
	DeviceCapacityFP16TeraFLOPS              = DeviceAttributeDomain + "/fp16-teraflops"
	DeviceCapacityBlockFP8TeraFLOPS          = DeviceAttributeDomain + "/blockfp8-teraflops"
	DeviceCapacityBoardPowerWatts            = DeviceAttributeDomain + "/board-power-watts"
)

// CardSpec captures compute-relevant specifications for a Tenstorrent card
// class. It intentionally ignores physical variants such as cooling, dimensions,
// and power connectors because those do not change DRA scheduling capability.
type CardSpec struct {
	ChipSeries              string `json:"chipSeries"`
	CardSeries              string `json:"cardSeries"`
	ASICCount               int    `json:"asicCount"`
	TensixCores             int    `json:"tensixCores"`
	BigRISCV                int    `json:"bigRiscv,omitempty"`
	AIClockGHz              string `json:"aiClockGHz"`
	SRAMMB                  int    `json:"sramMB"`
	MemoryGB                int    `json:"memoryGB"`
	MemoryType              string `json:"memoryType"`
	MemorySpeedGTPerSecond  int    `json:"memorySpeedGTPerSecond"`
	MemoryBandwidthGBPerSec int    `json:"memoryBandwidthGBPerSecond"`
	FP8TeraFLOPS            int    `json:"fp8TeraFLOPS,omitempty"`
	FP16TeraFLOPS           int    `json:"fp16TeraFLOPS,omitempty"`
	BlockFP8TeraFLOPS       int    `json:"blockFP8TeraFLOPS"`
	TBPWatts                int    `json:"tbpWatts"`
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
		AIClockGHz:              "1",
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
		AIClockGHz:              "1",
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
		AIClockGHz:              "1.35",
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
		AIClockGHz:              "1.35",
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

func (spec CardSpec) Attributes() map[string]DeviceAttribute {
	attributes := map[string]DeviceAttribute{
		DeviceAttributeChipSeries:           StringAttribute(spec.ChipSeries),
		DeviceAttributeCardSeries:           StringAttribute(spec.CardSeries),
		DeviceAttributeAIClockGHz:           StringAttribute(spec.AIClockGHz),
		DeviceAttributeMemoryType:           StringAttribute(spec.MemoryType),
		DeviceAttributeConnectivity:         BoolAttribute(spec.Connectivity),
		DeviceAttributeSystemInterfaceType:  StringAttribute(spec.SystemInterfaceType),
		DeviceAttributeSystemInterfaceCount: IntAttribute(spec.SystemInterfaceCount),
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
	return attributes
}

func (spec CardSpec) Capacity() map[string]DeviceCapacity {
	capacity := map[string]DeviceCapacity{
		DeviceCapacityASICs:                      CapacityValue(fmt.Sprint(spec.ASICCount)),
		DeviceCapacityTensixCores:                CapacityValue(fmt.Sprint(spec.TensixCores)),
		DeviceCapacitySRAMBytes:                  CapacityValue(fmt.Sprintf("%dM", spec.SRAMMB)),
		DeviceCapacityMemoryBytes:                CapacityValue(fmt.Sprintf("%dG", spec.MemoryGB)),
		DeviceCapacityMemorySpeedGTPerSecond:     CapacityValue(fmt.Sprint(spec.MemorySpeedGTPerSecond)),
		DeviceCapacityMemoryBandwidthBytesPerSec: CapacityValue(fmt.Sprintf("%dG", spec.MemoryBandwidthGBPerSec)),
		DeviceCapacityBlockFP8TeraFLOPS:          CapacityValue(fmt.Sprint(spec.BlockFP8TeraFLOPS)),
		DeviceCapacityBoardPowerWatts:            CapacityValue(fmt.Sprint(spec.TBPWatts)),
	}
	if spec.BigRISCV > 0 {
		capacity[DeviceCapacityBigRISCV] = CapacityValue(fmt.Sprint(spec.BigRISCV))
	}
	if spec.FP8TeraFLOPS > 0 {
		capacity[DeviceCapacityFP8TeraFLOPS] = CapacityValue(fmt.Sprint(spec.FP8TeraFLOPS))
	}
	if spec.FP16TeraFLOPS > 0 {
		capacity[DeviceCapacityFP16TeraFLOPS] = CapacityValue(fmt.Sprint(spec.FP16TeraFLOPS))
	}
	return capacity
}
