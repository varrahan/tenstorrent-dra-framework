package dra

import "github.com/varrahan/tt-kind-dra/src/dra/internal/device"

const DefaultDriverName = "tenstorrent.com/dra"

// DeviceResource is the device-level data needed before building a Kubernetes
// resource.k8s.io ResourceSlice.
type DeviceResource struct {
	Name       string            `json:"name"`
	Path       string            `json:"path"`
	Major      uint64            `json:"major"`
	Minor      uint64            `json:"minor"`
	Attributes map[string]string `json:"attributes"`
	Capacity   map[string]string `json:"capacity,omitempty"`
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
		attributes := map[string]string{
			DeviceAttributeDeviceID: node.ID,
			DeviceAttributePath:     node.Path,
		}
		if node.ChipSeries != "" {
			attributes[DeviceAttributeChipSeries] = node.ChipSeries
		}
		if node.CardSeries != "" {
			attributes[DeviceAttributeCardSeries] = node.CardSeries
		}
		if node.CardModel != "" {
			attributes[DeviceAttributeCardModel] = node.CardModel
		}

		capacity := map[string]string(nil)
		if spec, ok := CardSpecForModel(node.CardModel); ok {
			for key, value := range spec.Attributes() {
				attributes[key] = value
			}
			capacity = spec.Capacity()
		}

		devices = append(devices, DeviceResource{
			Name:       "tt-" + node.ID,
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
