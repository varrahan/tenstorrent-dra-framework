package dra

import (
	"bytes"
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func DeviceClassManifestYAML(driverName string) ([]byte, error) {
	objects := make([]any, 0, len(SupportedDeviceClassVariants))
	for _, class := range NewDeviceClasses(driverName) {
		objects = append(objects, class)
	}
	return manifestYAML(objects...)
}

func ReferenceResourceSliceManifestYAML(driverName, nodeName string) ([]byte, error) {
	slices := NewReferenceResourceSlices(driverName, nodeName)
	objects := make([]any, 0, len(slices))
	for _, slice := range slices {
		objects = append(objects, slice)
	}
	return manifestYAML(objects...)
}

func ReferenceResourceClaimManifestYAML() ([]byte, error) {
	claims := NewReferenceResourceClaims()
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
