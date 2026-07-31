package dra

import (
	"strconv"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const DefaultDeviceRequestName = "accelerator"

type ResourceClaimRequest struct {
	Name            string
	DeviceClassName string
	Count           int64
	Selectors       []resourceapi.DeviceSelector
}

func NewResourceClaim(name string, request ResourceClaimRequest) resourceapi.ResourceClaim {
	request = defaultResourceClaimRequest(request)
	selectors := append(SpatialAllocationSelectors(), request.Selectors...)

	return resourceapi.ResourceClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: resourceapi.SchemeGroupVersion.String(),
			Kind:       "ResourceClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: resourceClaimLabels(request.DeviceClassName),
		},
		Spec: resourceapi.ResourceClaimSpec{
			Devices: resourceapi.DeviceClaim{
				Requests: []resourceapi.DeviceRequest{
					{
						Name: request.Name,
						Exactly: &resourceapi.ExactDeviceRequest{
							DeviceClassName: request.DeviceClassName,
							Selectors:       selectors,
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           request.Count,
						},
					},
				},
			},
		},
	}
}

func SpatialAllocationSelectors() []resourceapi.DeviceSelector {
	return []resourceapi.DeviceSelector{
		{
			CEL: &resourceapi.CELDeviceSelector{
				Expression: SpatialAllocationSelectorExpression(),
			},
		},
	}
}

func SpatialAllocationSelectorExpression() string {
	return `device.attributes["tenstorrent.com"].tensixTopology == "2dMesh" && ` +
		`device.attributes["tenstorrent.com"].tensixAllocation == "contiguousRegion" && ` +
		`device.attributes["tenstorrent.com"].gddrControllerLayout == "localizedControllers"`
}

func NewReferenceResourceClaims() []resourceapi.ResourceClaim {
	claims := make([]resourceapi.ResourceClaim, 0, len(SupportedDeviceClassVariants))
	for _, variant := range SupportedDeviceClassVariants {
		claims = append(claims, NewResourceClaim(
			ReferenceResourceClaimName(variant),
			ReferenceResourceClaimRequest(variant),
		))
	}
	return claims
}

func ReferenceResourceClaimRequest(variant DeviceClassVariant) ResourceClaimRequest {
	spec, _ := CardSpecForClass(variant.ChipSeries, variant.CardSeries)

	return ResourceClaimRequest{
		DeviceClassName: DeviceClassName(variant.ChipSeries, variant.CardSeries),
		Selectors:       DeviceClassVariantSelectors(spec),
	}
}

func DeviceClassVariantSelectors(spec CardSpec) []resourceapi.DeviceSelector {
	selectors := []resourceapi.DeviceSelector{
		{
			CEL: &resourceapi.CELDeviceSelector{
				Expression: GDDRControllerSelectorExpression(spec),
			},
		},
	}
	if spec.BigRISCV > 0 {
		selectors = append(selectors, resourceapi.DeviceSelector{
			CEL: &resourceapi.CELDeviceSelector{
				Expression: BigRISCVSelectorExpression(spec.BigRISCV),
			},
		})
	}
	return selectors
}

func GDDRControllerSelectorExpression(spec CardSpec) string {
	return `device.attributes["tenstorrent.com"].gddrControllersPerASIC == ` + intString(spec.GDDRControllersPerASIC)
}

func BigRISCVSelectorExpression(minimumCoreCount int64) string {
	return `device.attributes["tenstorrent.com"].bigRISCVCoreCount >= ` + intString(minimumCoreCount)
}

func intString(value int64) string {
	return strconv.FormatInt(value, 10)
}

func ReferenceResourceClaimName(variant DeviceClassVariant) string {
	return "claim-" + variant.ChipSeries + "-" + variant.CardSeries
}

func defaultResourceClaimRequest(request ResourceClaimRequest) ResourceClaimRequest {
	if request.Name == "" {
		request.Name = DefaultDeviceRequestName
	}
	if request.Count == 0 {
		request.Count = 1
	}
	return request
}

func resourceClaimLabels(deviceClassName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "tt-dra-driver",
		"app.kubernetes.io/component":  "dra-resource-claim",
		"tenstorrent.com/device-class": deviceClassName,
	}
}
