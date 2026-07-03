package dra

import (
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
							Selectors:       request.Selectors,
							AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
							Count:           request.Count,
						},
					},
				},
			},
		},
	}
}

func NewReferenceResourceClaims() []resourceapi.ResourceClaim {
	claims := make([]resourceapi.ResourceClaim, 0, len(SupportedDeviceClassVariants))
	for _, variant := range SupportedDeviceClassVariants {
		className := DeviceClassName(variant.ChipSeries, variant.CardSeries)
		claims = append(claims, NewResourceClaim(
			ReferenceResourceClaimName(variant),
			ResourceClaimRequest{DeviceClassName: className},
		))
	}
	return claims
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
