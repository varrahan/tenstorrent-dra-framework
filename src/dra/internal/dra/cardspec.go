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

// CardSpec captures the static card-level specifications published by
// Tenstorrent's Wormhole and Blackhole PCIe card documentation.
type CardSpec struct {
	ChipSeries              string `json:"chipSeries"`
	CardSeries              string `json:"cardSeries"`
	CardModel               string `json:"cardModel"`
	PartNumber              string `json:"partNumber"`
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
	ExternalPower           string `json:"externalPower"`
	PowerSupplyRequirement  string `json:"powerSupplyRequirement,omitempty"`
	Connectivity            string `json:"connectivity"`
	InternalChipToChip      string `json:"internalChipToChip,omitempty"`
	SystemInterface         string `json:"systemInterface"`
	Cooling                 string `json:"cooling"`
	Dimensions              string `json:"dimensions"`
}

var SupportedCardSpecs = []CardSpec{
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n150",
		CardModel:               "n150d",
		PartNumber:              "TC-02002",
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
		ExternalPower:           "1x 4+4-pin EPS12V",
		Connectivity:            "2x Warp 100; 2x QSFP-DD 200G active",
		SystemInterface:         "PCIe 4.0 x16",
		Cooling:                 "Active axial fan",
		Dimensions:              "52.2mm x 256mm x 111mm",
	},
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n150",
		CardModel:               "n150s",
		PartNumber:              "TC-02001",
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
		ExternalPower:           "1x 4+4-pin EPS12V",
		Connectivity:            "2x Warp 100; 2x QSFP-DD 200G active",
		SystemInterface:         "PCIe 4.0 x16",
		Cooling:                 "Passive",
		Dimensions:              "36mm x 254mm x 111mm",
	},
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n300",
		CardModel:               "n300d",
		PartNumber:              "TC-02004",
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
		ExternalPower:           "1x 4+4-pin EPS12V",
		Connectivity:            "2x Warp 100; 2x QSFP-DD 200G active",
		InternalChipToChip:      "200G",
		SystemInterface:         "PCIe 4.0 x16",
		Cooling:                 "Active axial fan",
		Dimensions:              "52.2mm x 256mm x 111mm",
	},
	{
		ChipSeries:              "wormhole",
		CardSeries:              "n300",
		CardModel:               "n300s",
		PartNumber:              "TC-02003",
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
		ExternalPower:           "1x 4+4-pin EPS12V",
		Connectivity:            "2x Warp 100; 2x QSFP-DD 200G active",
		InternalChipToChip:      "200G",
		SystemInterface:         "PCIe 4.0 x16",
		Cooling:                 "Passive",
		Dimensions:              "36mm x 254mm x 111mm",
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p100",
		CardModel:               "p100a",
		PartNumber:              "TC-03008",
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
		ExternalPower:           "1x 12+4-pin 12V-2x6",
		PowerSupplyRequirement:  "ATX 3.1 Certified or better",
		Connectivity:            "none",
		SystemInterface:         "PCIe 5.0 x16",
		Cooling:                 "Active",
		Dimensions:              "42mm x 270mm x 111mm",
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p150",
		CardModel:               "p150a",
		PartNumber:              "TC-03003",
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
		ExternalPower:           "1x 12+4-pin 12V-2x6",
		PowerSupplyRequirement:  "ATX 3.1 Certified or better",
		Connectivity:            "4x QSFP-DD 800G passive",
		SystemInterface:         "PCIe 5.0 x16",
		Cooling:                 "Active",
		Dimensions:              "42mm x 270mm x 111mm",
	},
	{
		ChipSeries:              "blackhole",
		CardSeries:              "p150",
		CardModel:               "p150b",
		PartNumber:              "TC-03002",
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
		ExternalPower:           "1x 12+4-pin 12V-2x6",
		PowerSupplyRequirement:  "ATX 3.1 Certified or better",
		Connectivity:            "4x QSFP-DD 800G passive",
		SystemInterface:         "PCIe 5.0 x16",
		Cooling:                 "Passive",
		Dimensions:              "42mm x 270mm x 111mm",
	},
}

func CardSpecForModel(cardModel string) (CardSpec, bool) {
	for _, spec := range SupportedCardSpecs {
		if spec.CardModel == cardModel {
			return spec, true
		}
	}
	return CardSpec{}, false
}

func (spec CardSpec) Attributes() map[string]string {
	attributes := map[string]string{
		DeviceAttributeChipSeries:      spec.ChipSeries,
		DeviceAttributeCardSeries:      spec.CardSeries,
		DeviceAttributeCardModel:       spec.CardModel,
		DeviceAttributePartNumber:      spec.PartNumber,
		DeviceAttributeAIClock:         spec.AIClock,
		DeviceAttributeMemoryType:      spec.MemoryType,
		DeviceAttributeExternalPower:   spec.ExternalPower,
		DeviceAttributeConnectivity:    spec.Connectivity,
		DeviceAttributeSystemInterface: spec.SystemInterface,
		DeviceAttributeCooling:         spec.Cooling,
		DeviceAttributeDimensions:      spec.Dimensions,
	}
	if spec.PowerSupplyRequirement != "" {
		attributes[DeviceAttributePowerSupplyRequirement] = spec.PowerSupplyRequirement
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
