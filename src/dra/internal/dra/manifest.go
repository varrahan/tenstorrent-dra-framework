package dra

import (
	"bytes"
	"fmt"

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
