package dra

import "fmt"

const (
	DeviceAttributeDomain                 = "tenstorrent.com"
	DeviceAttributeDeviceID               = DeviceAttributeDomain + "/device-id"
	DeviceAttributePath                   = DeviceAttributeDomain + "/path"
	DeviceAttributeChipSeries             = DeviceAttributeDomain + "/chipSeries"
	DeviceAttributeCardSeries             = DeviceAttributeDomain + "/cardSeries"
	DeviceAttributeCardModel              = DeviceAttributeDomain + "/cardModel"
	DeviceAttributePartNumber             = DeviceAttributeDomain + "/partNumber"
	DeviceAttributeAIClock                = DeviceAttributeDomain + "/aiClock"
	DeviceAttributeMemoryType             = DeviceAttributeDomain + "/memoryType"
	DeviceAttributeExternalPower          = DeviceAttributeDomain + "/externalPower"
	DeviceAttributePowerSupplyRequirement = DeviceAttributeDomain + "/powerSupplyRequirement"
	DeviceAttributeConnectivity           = DeviceAttributeDomain + "/connectivity"
	DeviceAttributeInternalChipToChip     = DeviceAttributeDomain + "/internalChipToChip"
	DeviceAttributeSystemInterface        = DeviceAttributeDomain + "/systemInterface"
	DeviceAttributeCooling                = DeviceAttributeDomain + "/cooling"
	DeviceAttributeDimensions             = DeviceAttributeDomain + "/dimensions"
)

// DeviceClassVariant describes a Tenstorrent chip, card series, and card model
// pairing that can be exposed through a Kubernetes DeviceClass.
type DeviceClassVariant struct {
	ChipSeries string `json:"chipSeries"`
	CardSeries string `json:"cardSeries"`
	CardModel  string `json:"cardModel"`
}

// DeviceClassModel is a dependency-light representation of the DeviceClass
// selector data this driver can publish or install.
type DeviceClassModel struct {
	Name               string `json:"name"`
	DriverName         string `json:"driverName"`
	ChipSeries         string `json:"chipSeries"`
	CardSeries         string `json:"cardSeries"`
	CardModel          string `json:"cardModel"`
	SelectorExpression string `json:"selectorExpression"`
}

var SupportedDeviceClassVariants = DeviceClassVariantsFromCardSpecs(SupportedCardSpecs)

func DeviceClassVariantsFromCardSpecs(specs []CardSpec) []DeviceClassVariant {
	variants := make([]DeviceClassVariant, 0, len(specs))
	for _, spec := range specs {
		variants = append(variants, DeviceClassVariant{
			ChipSeries: spec.ChipSeries,
			CardSeries: spec.CardSeries,
			CardModel:  spec.CardModel,
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
		Name:               DeviceClassName(variant.ChipSeries, variant.CardModel),
		DriverName:         driverName,
		ChipSeries:         variant.ChipSeries,
		CardSeries:         variant.CardSeries,
		CardModel:          variant.CardModel,
		SelectorExpression: DeviceClassSelectorExpression(driverName, variant),
	}
}

func DeviceClassName(chipSeries, cardModel string) string {
	return fmt.Sprintf("tenstorrent-%s-%s", chipSeries, cardModel)
}

func DeviceClassSelectorExpression(driverName string, variant DeviceClassVariant) string {
	if driverName == "" {
		driverName = DefaultDriverName
	}

	return fmt.Sprintf(
		"device.driver == %q &&\n"+
			"device.attributes[%q].chipSeries == %q &&\n"+
			"device.attributes[%q].cardSeries == %q &&\n"+
			"device.attributes[%q].cardModel == %q",
		driverName,
		DeviceAttributeDomain,
		variant.ChipSeries,
		DeviceAttributeDomain,
		variant.CardSeries,
		DeviceAttributeDomain,
		variant.CardModel,
	)
}
