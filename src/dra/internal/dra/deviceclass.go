package dra

import "fmt"

const (
	DeviceAttributeDomain                 = "tenstorrent.com"
	DeviceAttributeDeviceID               = DeviceAttributeDomain + "/device-id"
	DeviceAttributePath                   = DeviceAttributeDomain + "/path"
	DeviceAttributeChipSeries             = DeviceAttributeDomain + "/chipSeries"
	DeviceAttributeCardSeries             = DeviceAttributeDomain + "/cardSeries"
	DeviceAttributeAIClockGHz             = DeviceAttributeDomain + "/aiClockGHz"
	DeviceAttributeAIClockMHz             = DeviceAttributeDomain + "/aiClockMHz"
	DeviceAttributeMemoryType             = DeviceAttributeDomain + "/memoryType"
	DeviceAttributeConnectivity           = DeviceAttributeDomain + "/connectivity"
	DeviceAttributeWarpInterfaceCount     = DeviceAttributeDomain + "/warpInterfaceCount"
	DeviceAttributeWarpSpeedGbps          = DeviceAttributeDomain + "/warpSpeedGbps"
	DeviceAttributeQSFPInterfaceCount     = DeviceAttributeDomain + "/qsfpInterfaceCount"
	DeviceAttributeQSFPSpeedGbps          = DeviceAttributeDomain + "/qsfpSpeedGbps"
	DeviceAttributeSystemInterfaceType    = DeviceAttributeDomain + "/systemInterfaceType"
	DeviceAttributeSystemInterfaceCount   = DeviceAttributeDomain + "/systemInterfaceCount"
	DeviceAttributeInternalChipToChipGbps  = DeviceAttributeDomain + "/internalChipToChipGbps"
)

// DeviceClassVariant describes a compute-equivalent Tenstorrent chip and card
// series pairing that can be exposed through a Kubernetes DeviceClass.
type DeviceClassVariant struct {
	ChipSeries string `json:"chipSeries"`
	CardSeries string `json:"cardSeries"`
}

// DeviceClassModel is a dependency-light representation of the DeviceClass
// selector data this driver can publish or install.
type DeviceClassModel struct {
	Name               string `json:"name"`
	DriverName         string `json:"driverName"`
	ChipSeries         string `json:"chipSeries"`
	CardSeries         string `json:"cardSeries"`
	SelectorExpression string `json:"selectorExpression"`
}

var SupportedDeviceClassVariants = DeviceClassVariantsFromCardSpecs(SupportedCardSpecs)

func DeviceClassVariantsFromCardSpecs(specs []CardSpec) []DeviceClassVariant {
	variants := make([]DeviceClassVariant, 0, len(specs))
	for _, spec := range specs {
		variants = append(variants, DeviceClassVariant{
			ChipSeries: spec.ChipSeries,
			CardSeries: spec.CardSeries,
		})
	}
	return variants
}

func NewDeviceClassModels(driverName string) []DeviceClassModel {
	models := make([]DeviceClassModel, 0, len(SupportedDeviceClassVariants))
	for _, variant := range SupportedDeviceClassVariants {
		models = append(models, NewDeviceClassModel(driverName, variant))
	}
	return models
}

func NewDeviceClassModel(driverName string, variant DeviceClassVariant) DeviceClassModel {
	if driverName == "" {
		driverName = DefaultDriverName
	}

	return DeviceClassModel{
		Name:               DeviceClassName(variant.ChipSeries, variant.CardSeries),
		DriverName:         driverName,
		ChipSeries:         variant.ChipSeries,
		CardSeries:         variant.CardSeries,
		SelectorExpression: DeviceClassSelectorExpression(driverName, variant),
	}
}

func DeviceClassName(chipSeries, cardSeries string) string {
	return fmt.Sprintf("tenstorrent-%s-%s", chipSeries, cardSeries)
}

func DeviceClassSelectorExpression(driverName string, variant DeviceClassVariant) string {
	if driverName == "" {
		driverName = DefaultDriverName
	}

	return fmt.Sprintf(
		"device.driver == %q &&\n"+
			"device.attributes[%q].chipSeries == %q &&\n"+
			"device.attributes[%q].cardSeries == %q",
		driverName,
		DeviceAttributeDomain,
		variant.ChipSeries,
		DeviceAttributeDomain,
		variant.CardSeries,
	)
}
