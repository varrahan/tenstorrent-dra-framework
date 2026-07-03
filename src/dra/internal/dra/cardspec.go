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
	AIClock                 string `json:"aiClock"`
	SRAMMB                  int    `json:"sramMB"`
	MemoryGB                int    `json:"memoryGB"`
	MemoryType              string `json:"memoryType"`
	MemorySpeedGTPerSecond  int    `json:"memorySpeedGTPerSecond"`
	MemoryBandwidthGBPerSec int    `json:"memoryBandwidthGBPerSecond"`
	FP8TeraFLOPS            int    `json:"fp8TeraFLOPS,omitempty"`
	FP16TeraFLOPS           int    `json:"fp16TeraFLOPS,omitempty"`
	BlockFP8TeraFLOPS       int    `json:"blockFP8TeraFLOPS"`
	TBPWatts                int    `json:"tbpWatts"`
	Connectivity            string `json:"connectivity"`
	InternalChipToChip      string `json:"internalChipToChip,omitempty"`
	SystemInterface         string `json:"systemInterface"`
}

var SupportedCardSpecs = []CardSpec{
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n150",
		ASICCount:               1,
		TensixCores:             72,
		AIClock:                 "1 GHz",
		SRAMMB:                  108,
		MemoryGB:                12,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  12,
		MemoryBandwidthGBPerSec: 288,
		FP8TeraFLOPS:            262,
		FP16TeraFLOPS:           74,
		BlockFP8TeraFLOPS:       148,
		TBPWatts:                160,
		Connectivity:            "2x Warp 100; 2x QSFP-DD 200G active",
		SystemInterface:         "PCIe 4.0 x16",
	},
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n300",
		ASICCount:               2,
		TensixCores:             128,
		AIClock:                 "1 GHz",
		SRAMMB:                  192,
		MemoryGB:                24,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  12,
		MemoryBandwidthGBPerSec: 576,
		FP8TeraFLOPS:            466,
		FP16TeraFLOPS:           131,
		BlockFP8TeraFLOPS:       262,
		TBPWatts:                300,
		Connectivity:            "2x Warp 100; 2x QSFP-DD 200G active",
		InternalChipToChip:      "200G",
		SystemInterface:         "PCIe 4.0 x16",
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p100",
		ASICCount:               1,
		TensixCores:             120,
		BigRISCV:                16,
		AIClock:                 "Up to 1.35 GHz",
		SRAMMB:                  180,
		MemoryGB:                28,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  16,
		MemoryBandwidthGBPerSec: 448,
		BlockFP8TeraFLOPS:       664,
		TBPWatts:                300,
		Connectivity:            "none",
		SystemInterface:         "PCIe 5.0 x16",
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p150",
		ASICCount:               1,
		TensixCores:             120,
		BigRISCV:                16,
		AIClock:                 "Up to 1.35 GHz",
		SRAMMB:                  180,
		MemoryGB:                32,
		MemoryType:              "GDDR6",
		MemorySpeedGTPerSecond:  16,
		MemoryBandwidthGBPerSec: 512,
		BlockFP8TeraFLOPS:       664,
		TBPWatts:                300,
		Connectivity:            "4x QSFP-DD 800G",
		SystemInterface:         "PCIe 5.0 x16",
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

func (spec CardSpec) Attributes() map[string]string {
	attributes := map[string]string{
		DeviceAttributeChipSeries:      spec.ChipSeries,
		DeviceAttributeCardSeries:      spec.CardSeries,
		DeviceAttributeAIClock:         spec.AIClock,
		DeviceAttributeMemoryType:      spec.MemoryType,
		DeviceAttributeConnectivity:    spec.Connectivity,
		DeviceAttributeSystemInterface: spec.SystemInterface,
	}
	if spec.InternalChipToChip != "" {
		attributes[DeviceAttributeInternalChipToChip] = spec.InternalChipToChip
	}
	return attributes
}

func (spec CardSpec) Capacity() map[string]string {
	capacity := map[string]string{
		DeviceCapacityASICs:                      fmt.Sprint(spec.ASICCount),
		DeviceCapacityTensixCores:                fmt.Sprint(spec.TensixCores),
		DeviceCapacitySRAMBytes:                  fmt.Sprintf("%dM", spec.SRAMMB),
		DeviceCapacityMemoryBytes:                fmt.Sprintf("%dG", spec.MemoryGB),
		DeviceCapacityMemorySpeedGTPerSecond:     fmt.Sprint(spec.MemorySpeedGTPerSecond),
		DeviceCapacityMemoryBandwidthBytesPerSec: fmt.Sprintf("%dG", spec.MemoryBandwidthGBPerSec),
		DeviceCapacityBlockFP8TeraFLOPS:          fmt.Sprint(spec.BlockFP8TeraFLOPS),
		DeviceCapacityBoardPowerWatts:            fmt.Sprint(spec.TBPWatts),
	}
	if spec.BigRISCV > 0 {
		capacity[DeviceCapacityBigRISCV] = fmt.Sprint(spec.BigRISCV)
	}
	if spec.FP8TeraFLOPS > 0 {
		capacity[DeviceCapacityFP8TeraFLOPS] = fmt.Sprint(spec.FP8TeraFLOPS)
	}
	if spec.FP16TeraFLOPS > 0 {
		capacity[DeviceCapacityFP16TeraFLOPS] = fmt.Sprint(spec.FP16TeraFLOPS)
	}
	return capacity
}
