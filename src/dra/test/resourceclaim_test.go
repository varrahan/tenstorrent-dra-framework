package test

import (
	"strings"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
	resourceapi "k8s.io/api/resource/v1"
)

func TestResourceClaimBuildsExactDeviceRequest(t *testing.T) {
	claim := dra.NewResourceClaim("claim-wormhole-n300", dra.ResourceClaimRequest{
		DeviceClassName: "tenstorrent-wormhole-n300",
	})

	if claim.APIVersion != "resource.k8s.io/v1" || claim.Kind != "ResourceClaim" {
		t.Fatalf("resource claim type = %s/%s, want resource.k8s.io/v1/ResourceClaim", claim.APIVersion, claim.Kind)
	}
	if claim.Name != "claim-wormhole-n300" {
		t.Fatalf("claim name = %q, want claim-wormhole-n300", claim.Name)
	}

	requests := claim.Spec.Devices.Requests
	if len(requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(requests))
	}
	got := requests[0]
	if got.Name != dra.DefaultDeviceRequestName {
		t.Fatalf("request name = %q, want %q", got.Name, dra.DefaultDeviceRequestName)
	}
	if got.Exactly == nil {
		t.Fatal("exact device request is nil")
	}
	if got.Exactly.DeviceClassName != "tenstorrent-wormhole-n300" {
		t.Fatalf("deviceClassName = %q, want tenstorrent-wormhole-n300", got.Exactly.DeviceClassName)
	}
	if got.Exactly.AllocationMode != resourceapi.DeviceAllocationModeExactCount {
		t.Fatalf("allocationMode = %q, want ExactCount", got.Exactly.AllocationMode)
	}
	if got.Exactly.Count != 1 {
		t.Fatalf("count = %d, want 1", got.Exactly.Count)
	}
	if len(got.Exactly.Selectors) != 1 || got.Exactly.Selectors[0].CEL == nil {
		t.Fatalf("selectors = %#v, want spatial CEL selector", got.Exactly.Selectors)
	}
	if got.Exactly.Selectors[0].CEL.Expression != dra.SpatialAllocationSelectorExpression() {
		t.Fatalf("spatial selector = %q, want %q", got.Exactly.Selectors[0].CEL.Expression, dra.SpatialAllocationSelectorExpression())
	}
}

func TestReferenceResourceClaimsCoverSupportedDeviceClasses(t *testing.T) {
	claims := dra.NewReferenceResourceClaims()

	if len(claims) != len(dra.SupportedDeviceClassVariants) {
		t.Fatalf("reference claim count = %d, want %d", len(claims), len(dra.SupportedDeviceClassVariants))
	}

	for i, variant := range dra.SupportedDeviceClassVariants {
		wantName := dra.ReferenceResourceClaimName(variant)
		wantClass := dra.DeviceClassName(variant.ChipSeries, variant.CardSeries)
		got := claims[i]

		if got.Name != wantName {
			t.Fatalf("claim %d name = %q, want %q", i, got.Name, wantName)
		}
		if got.Spec.Devices.Requests[0].Exactly.DeviceClassName != wantClass {
			t.Fatalf("claim %d device class = %q, want %q", i, got.Spec.Devices.Requests[0].Exactly.DeviceClassName, wantClass)
		}
		selectorText := selectorsText(got.Spec.Devices.Requests[0].Exactly.Selectors)
		if variant.ChipSeries == "wormhole" && !containsSelector(selectorText, "gddrControllersPerASIC == 6") {
			t.Fatalf("claim %d selectors = %#v, want wormhole GDDR controller selector", i, selectorText)
		}
		if variant.ChipSeries == "blackhole" {
			if !containsSelector(selectorText, "gddrControllersPerASIC == 8") {
				t.Fatalf("claim %d selectors = %#v, want blackhole GDDR controller selector", i, selectorText)
			}
			if !containsSelector(selectorText, "bigRISCVCoreCount >= 16") {
				t.Fatalf("claim %d selectors = %#v, want blackhole big RISC-V selector", i, selectorText)
			}
		}
	}
}

func selectorsText(selectors []resourceapi.DeviceSelector) []string {
	values := []string(nil)
	for _, selector := range selectors {
		if selector.CEL != nil {
			values = append(values, selector.CEL.Expression)
		}
	}
	return values
}

func containsSelector(selectors []string, fragment string) bool {
	for _, selector := range selectors {
		if strings.Contains(selector, fragment) {
			return true
		}
	}
	return false
}
