package dra

import "github.com/varrahan/tt-kind-dra/src/dra/internal/device"

const DefaultDriverName = "tenstorrent.com/dra"

// DeviceResource is the device-level data needed before building a Kubernetes
// resource.k8s.io ResourceSlice.
type DeviceResource struct {
	Name       string                     `json:"name"`
	Path       string                     `json:"path"`
	Major      uint64                     `json:"major"`
	Minor      uint64                     `json:"minor"`
	Attributes map[string]DeviceAttribute `json:"attributes"`
	Capacity   map[string]DeviceCapacity  `json:"capacity,omitempty"`
}

// DeviceAttribute mirrors the typed ResourceSlice device attribute value shape.
type DeviceAttribute struct {
	String *string `json:"string,omitempty"`
	Int    *int64  `json:"int,omitempty"`
	Bool   *bool   `json:"bool,omitempty"`
}

// DeviceCapacity mirrors the ResourceSlice capacity value shape.
type DeviceCapacity struct {
	Value string `json:"value"`
}

// ResourceSliceModel is an internal, dependency-light representation of the
// ResourceSlice data this driver will publish.
type ResourceSliceModel struct {
	DriverName string           `json:"driverName"`
	NodeName   string           `json:"nodeName"`
	Devices    []DeviceResource `json:"devices"`
}

func NewResourceSliceModel(driverName, nodeName string, nodes []device.Node) ResourceSliceModel {
	if driverName == "" {
		driverName = DefaultDriverName
	}

	devices := make([]DeviceResource, 0, len(nodes))
	for _, node := range nodes {
		name := "tt-" + node.ID
		if node.ChipSeries != "" && node.CardSeries != "" {
			name = "tt-" + node.ChipSeries + "-" + node.CardSeries + "-" + node.ID
		}
		attributes := map[string]DeviceAttribute{
			DeviceAttributeDeviceID: StringAttribute(node.ID),
			DeviceAttributePath:     StringAttribute(node.Path),
		}
		if node.ChipSeries != "" {
			attributes[DeviceAttributeChipSeries] = StringAttribute(node.ChipSeries)
		}
		if node.CardSeries != "" {
			attributes[DeviceAttributeCardSeries] = StringAttribute(node.CardSeries)
		}

		capacity := map[string]DeviceCapacity(nil)
		if spec, ok := CardSpecForClass(node.ChipSeries, node.CardSeries); ok {
			for key, value := range spec.Attributes() {
				attributes[key] = value
			}
			capacity = spec.Capacity()
		}

		devices = append(devices, DeviceResource{
			Name:       name,
			Path:       node.Path,
			Major:      node.Major,
			Minor:      node.Minor,
			Attributes: attributes,
			Capacity:   capacity,
		})
	}

	return ResourceSliceModel{
		DriverName: driverName,
		NodeName:   nodeName,
		Devices:    devices,
	}
}

func StringAttribute(value string) DeviceAttribute {
	return DeviceAttribute{String: &value}
}

func IntAttribute(value int64) DeviceAttribute {
	return DeviceAttribute{Int: &value}
}

func BoolAttribute(value bool) DeviceAttribute {
	return DeviceAttribute{Bool: &value}
}

func CapacityValue(value string) DeviceCapacity {
	return DeviceCapacity{Value: value}
}
