package dra

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DeviceAttributeDomain               = "tenstorrent.com"
	DeviceAttributeDeviceID             = DeviceAttributeDomain + "/deviceID"
	DeviceAttributePath                 = DeviceAttributeDomain + "/path"
	DeviceAttributeMajor                = DeviceAttributeDomain + "/major"
	DeviceAttributeMinor                = DeviceAttributeDomain + "/minor"
	DeviceAttributeChipSeries           = DeviceAttributeDomain + "/chipSeries"
	DeviceAttributeCardSeries           = DeviceAttributeDomain + "/cardSeries"
	DeviceAttributeAIClockMHz           = DeviceAttributeDomain + "/aiClockMHz"
	DeviceAttributeMemoryType           = DeviceAttributeDomain + "/memoryType"
	DeviceAttributeConnectivity         = DeviceAttributeDomain + "/connectivity"
	DeviceAttributeWarpInterfaceCount   = DeviceAttributeDomain + "/warpInterfaceCount"
	DeviceAttributeWarpSpeedGbps        = DeviceAttributeDomain + "/warpSpeedGbps"
	DeviceAttributeQSFPInterfaceCount   = DeviceAttributeDomain + "/qsfpInterfaceCount"
	DeviceAttributeQSFPSpeedGbps        = DeviceAttributeDomain + "/qsfpSpeedGbps"
	DeviceAttributeSystemInterfaceType  = DeviceAttributeDomain + "/systemInterfaceType"
	DeviceAttributeSystemInterfaceCount = DeviceAttributeDomain + "/systemInterfaceCount"
)

// DeviceClassVariant describes a compute-equivalent Tenstorrent chip and card
// series pairing that can be exposed through a Kubernetes DeviceClass.
type DeviceClassVariant struct {
	ChipSeries string `json:"chipSeries"`
	CardSeries string `json:"cardSeries"`
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

func NewDeviceClasses(driverName string) []resourceapi.DeviceClass {
	classes := make([]resourceapi.DeviceClass, 0, len(SupportedDeviceClassVariants))
	for _, variant := range SupportedDeviceClassVariants {
		classes = append(classes, NewDeviceClass(driverName, variant))
	}
	return classes
}

func NewDeviceClass(driverName string, variant DeviceClassVariant) resourceapi.DeviceClass {
	driverName = defaultDriverName(driverName)

	return resourceapi.DeviceClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: resourceapi.SchemeGroupVersion.String(),
			Kind:       "DeviceClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   DeviceClassName(variant.ChipSeries, variant.CardSeries),
			Labels: deviceClassLabels(variant),
		},
		Spec: resourceapi.DeviceClassSpec{
			Selectors: []resourceapi.DeviceSelector{
				{
					CEL: &resourceapi.CELDeviceSelector{
						Expression: DeviceClassSelectorExpression(driverName, variant),
					},
				},
			},
		},
	}
}

func DeviceClassName(chipSeries, cardSeries string) string {
	return fmt.Sprintf("tenstorrent-%s-%s", chipSeries, cardSeries)
}

func DeviceClassSelectorExpression(driverName string, variant DeviceClassVariant) string {
	driverName = defaultDriverName(driverName)

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

func deviceClassLabels(variant DeviceClassVariant) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "tt-dra-driver",
		"app.kubernetes.io/component": "dra-device-class",
		"tenstorrent.com/chip-series": variant.ChipSeries,
		"tenstorrent.com/card-series": variant.CardSeries,
	}
}
