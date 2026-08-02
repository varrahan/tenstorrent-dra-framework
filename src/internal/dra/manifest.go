package dra

import (
	"bytes"
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func DeviceClassManifestYAML(driverName string) ([]byte, error) {
	if err := ValidateDriverName(defaultDriverName(driverName)); err != nil {
		return nil, err
	}
	classes := NewDeviceClasses(driverName)
	if err := ValidateDeviceClasses(classes); err != nil {
		return nil, err
	}

	objects := make([]any, 0, len(classes))
	for _, class := range classes {
		objects = append(objects, class)
	}
	return manifestYAML(objects...)
}

func ReferenceResourceSliceManifestYAML(driverName, nodeName string) ([]byte, error) {
	if err := ValidateDriverName(defaultDriverName(driverName)); err != nil {
		return nil, err
	}
	slices := NewReferenceResourceSlices(driverName, nodeName)
	if err := ValidateResourceSlices(slices); err != nil {
		return nil, err
	}

	objects := make([]any, 0, len(slices))
	for _, slice := range slices {
		objects = append(objects, slice)
	}
	return manifestYAML(objects...)
}

func ReferenceResourceClaimManifestYAML() ([]byte, error) {
	claims := NewReferenceResourceClaims()
	if err := ValidateResourceClaims(claims); err != nil {
		return nil, err
	}

	objects := make([]any, 0, len(claims))
	for _, claim := range claims {
		objects = append(objects, resourceClaimManifest{
			TypeMeta:   claim.TypeMeta,
			ObjectMeta: claim.ObjectMeta,
			Spec:       claim.Spec,
		})
	}
	return manifestYAML(objects...)
}

type resourceClaimManifest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              resourceapi.ResourceClaimSpec `json:"spec"`
}

func manifestYAML(objects ...any) ([]byte, error) {
	var output bytes.Buffer

	for i, object := range objects {
		if i > 0 {
			output.WriteString("---\n")
		}

		data, err := yaml.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("marshal manifest object %d: %w", i, err)
		}
		output.Write(data)
	}

	return output.Bytes(), nil
}
