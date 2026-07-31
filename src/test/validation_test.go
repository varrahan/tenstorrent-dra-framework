package test

import (
	"strings"
	"testing"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/dra"
	resourceapi "k8s.io/api/resource/v1"
)

func TestGeneratedDRAObjectsPassSchemaAndCELValidation(t *testing.T) {
	if err := dra.ValidateDeviceClasses(dra.NewDeviceClasses("")); err != nil {
		t.Fatalf("validate DeviceClasses: %v", err)
	}
	if err := dra.ValidateResourceSlices(dra.NewReferenceResourceSlices("", dra.ReferenceNodeName)); err != nil {
		t.Fatalf("validate ResourceSlices: %v", err)
	}
	if err := dra.ValidateResourceClaims(dra.NewReferenceResourceClaims()); err != nil {
		t.Fatalf("validate ResourceClaims: %v", err)
	}
}

func TestManifestGenerationRejectsInvalidDriverName(t *testing.T) {
	for _, generate := range []struct {
		name string
		fn   func() error
	}{
		{
			name: "DeviceClass",
			fn: func() error {
				_, err := dra.DeviceClassManifestYAML("tenstorrent.com/dra")
				return err
			},
		},
		{
			name: "ResourceSlice",
			fn: func() error {
				_, err := dra.ReferenceResourceSliceManifestYAML("tenstorrent.com/dra", dra.ReferenceNodeName)
				return err
			},
		},
	} {
		t.Run(generate.name, func(t *testing.T) {
			err := generate.fn()
			if err == nil || !strings.Contains(err.Error(), "invalid driver name") {
				t.Fatalf("error = %v, want invalid driver name", err)
			}
		})
	}
}

func TestValidationRejectsMalformedOrNonBooleanCEL(t *testing.T) {
	for _, expression := range []string{"device.", `device.driver`} {
		class := dra.NewDeviceClass("", dra.SupportedDeviceClassVariants[0])
		class.Spec.Selectors[0].CEL.Expression = expression

		if err := dra.ValidateDeviceClasses([]resourceapi.DeviceClass{class}); err == nil {
			t.Fatalf("expression %q passed CEL validation", expression)
		}
	}
}

func TestValidationRejectsInvalidResourceSliceSchema(t *testing.T) {
	slice := dra.NewReferenceResourceSlices("", dra.ReferenceNodeName)[0]
	slice.Spec.Driver = "tenstorrent.com/dra"
	slice.Spec.Devices[0].Name = "INVALID_DEVICE"

	err := dra.ValidateResourceSlices([]resourceapi.ResourceSlice{slice})
	if err == nil {
		t.Fatal("invalid ResourceSlice passed schema validation")
	}
	for _, want := range []string{"spec.driver", "invalid driver name", "INVALID_DEVICE"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error %q does not contain %q", err, want)
		}
	}
}
