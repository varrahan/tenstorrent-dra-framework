package dra

import (
	"fmt"

	"github.com/varrahan/tt-kind-dra/src/dra/internal/device"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const DefaultDriverName = "tenstorrent.com/dra"

const (
	DefaultPoolGeneration int64 = 1
	ReferenceNodeName           = "ttsim-node"
)

// NewResourceSliceForNodes converts discovered local device nodes into a typed
// Kubernetes resource.k8s.io/v1 ResourceSlice.
func NewResourceSliceForNodes(driverName, sliceName, nodeName, poolName string, nodes []device.Node) resourceapi.ResourceSlice {
	driverName = defaultDriverName(driverName)

	devices := make([]resourceapi.Device, 0, len(nodes))
	for _, node := range nodes {
		devices = append(devices, newDeviceResource(node))
	}

	return NewResourceSlice(driverName, sliceName, nodeName, poolName, DefaultPoolGeneration, devices)
}

// NewResourceSlice builds the typed Kubernetes object the DRA driver publishes.
func NewResourceSlice(driverName, sliceName, nodeName, poolName string, generation int64, devices []resourceapi.Device) resourceapi.ResourceSlice {
	driverName = defaultDriverName(driverName)

	return resourceapi.ResourceSlice{
		TypeMeta: metav1.TypeMeta{
			APIVersion: resourceapi.SchemeGroupVersion.String(),
			Kind:       "ResourceSlice",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   sliceName,
			Labels: resourceSliceLabels("", ""),
		},
		Spec: resourceapi.ResourceSliceSpec{
			Driver:   driverName,
			NodeName: &nodeName,
			Pool: resourceapi.ResourcePool{
				Name:               poolName,
				Generation:         generation,
				ResourceSliceCount: 1,
			},
			Devices: devices,
		},
	}
}

func NewReferenceResourceSlices(driverName, nodeName string) []resourceapi.ResourceSlice {
	if nodeName == "" {
		nodeName = ReferenceNodeName
	}

	slices := make([]resourceapi.ResourceSlice, 0, len(SupportedCardSpecs))
	for _, spec := range SupportedCardSpecs {
		slice := NewResourceSlice(
			driverName,
			ReferenceResourceSliceName(spec),
			nodeName,
			ReferenceResourcePoolName(nodeName, spec),
			DefaultPoolGeneration,
			[]resourceapi.Device{NewReferenceDevice(spec)},
		)
		slice.Labels = resourceSliceLabels(spec.ChipSeries, spec.CardSeries)
		slices = append(slices, slice)
	}
	return slices
}

func NewReferenceDevice(spec CardSpec) resourceapi.Device {
	return resourceapi.Device{
		Name:       "tt-" + spec.ChipSeries + "-" + spec.CardSeries + "-0",
		Attributes: spec.Attributes(),
		Capacity:   spec.Capacity(),
	}
}

func ReferenceResourceSliceName(spec CardSpec) string {
	return "ttsim-" + spec.ChipSeries + "-" + spec.CardSeries
}

func ReferenceResourcePoolName(nodeName string, spec CardSpec) string {
	return nodeName + "/" + spec.ChipSeries + "-" + spec.CardSeries
}

func defaultDriverName(driverName string) string {
	if driverName != "" {
		return driverName
	}
	return DefaultDriverName
}

func newDeviceResource(node device.Node) resourceapi.Device {
	attributes := nodeAttributes(node)
	capacity := map[resourceapi.QualifiedName]resourceapi.DeviceCapacity(nil)

	if spec, ok := CardSpecForClass(node.ChipSeries, node.CardSeries); ok {
		for key, value := range spec.Attributes() {
			attributes[key] = value
		}
		capacity = spec.Capacity()
	}

	return resourceapi.Device{
		Name:       deviceResourceName(node),
		Attributes: attributes,
		Capacity:   capacity,
	}
}

func deviceResourceName(node device.Node) string {
	if node.ChipSeries != "" && node.CardSeries != "" {
		return "tt-" + node.ChipSeries + "-" + node.CardSeries + "-" + node.ID
	}
	return "tt-" + node.ID
}

func nodeAttributes(node device.Node) map[resourceapi.QualifiedName]resourceapi.DeviceAttribute {
	attributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		DeviceAttributeDeviceID: StringAttribute(node.ID),
		DeviceAttributePath:     StringAttribute(node.Path),
		DeviceAttributeMajor:    IntAttribute(int64(node.Major)),
		DeviceAttributeMinor:    IntAttribute(int64(node.Minor)),
	}
	if node.ChipSeries != "" {
		attributes[DeviceAttributeChipSeries] = StringAttribute(node.ChipSeries)
	}
	if node.CardSeries != "" {
		attributes[DeviceAttributeCardSeries] = StringAttribute(node.CardSeries)
	}
	return attributes
}

func StringAttribute(value string) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{StringValue: &value}
}

func IntAttribute(value int64) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{IntValue: &value}
}

func BoolAttribute(value bool) resourceapi.DeviceAttribute {
	return resourceapi.DeviceAttribute{BoolValue: &value}
}

func CapacityValue(value int64) resourceapi.DeviceCapacity {
	return CapacityValueFromString(value, "")
}

func CapacityValueFromString(value int64, suffix string) resourceapi.DeviceCapacity {
	return resourceapi.DeviceCapacity{Value: resource.MustParse(fmt.Sprintf("%d%s", value, suffix))}
}

func resourceSliceLabels(chipSeries, cardSeries string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":      "tt-dra-driver",
		"app.kubernetes.io/component": "dra-resource-slice",
	}
	if chipSeries != "" {
		labels["tenstorrent.com/chip-series"] = chipSeries
	}
	if cardSeries != "" {
		labels["tenstorrent.com/card-series"] = cardSeries
	}
	return labels
}
