package dra

import (
	"context"
	"fmt"
	"sort"

	resourceapi "k8s.io/api/resource/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/dynamic-resource-allocation/resourceslice"
)

// ConfirmResourcePublication verifies that the API server contains exactly the
// complete desired node-local pool. PublishResources is asynchronous, so a nil
// result from that call alone does not confirm that ResourceSlices were stored.
func ConfirmResourcePublication(ctx context.Context, kube kubernetes.Interface, nodeName, driver string, desired resourceslice.DriverResources) error {
	pool, found := desired.Pools[nodeName]
	if !found {
		return fmt.Errorf("desired resources do not contain node pool %q", nodeName)
	}
	selector := fields.AndSelectors(
		fields.OneTermEqualSelector(resourceapi.ResourceSliceSelectorNodeName, nodeName),
		fields.OneTermEqualSelector(resourceapi.ResourceSliceSelectorDriver, driver),
	).String()
	list, err := kube.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{FieldSelector: selector})
	if err != nil {
		return fmt.Errorf("list published ResourceSlices: %w", err)
	}

	actual := make([]resourceapi.ResourceSlice, 0, len(list.Items))
	for _, slice := range list.Items {
		// The fake client does not apply field selectors, so retain the same
		// filter here for deterministic unit tests and defensive verification.
		if slice.Spec.Driver == driver && slice.Spec.NodeName != nil && *slice.Spec.NodeName == nodeName {
			actual = append(actual, slice)
		}
	}
	if len(actual) != len(pool.Slices) {
		return fmt.Errorf("published ResourceSlice count is %d, want %d", len(actual), len(pool.Slices))
	}

	expectedDevices := make([]resourceapi.Device, 0)
	for _, slice := range pool.Slices {
		expectedDevices = append(expectedDevices, slice.Devices...)
	}
	actualDevices := make([]resourceapi.Device, 0, len(expectedDevices))
	var generation int64
	for index, slice := range actual {
		if slice.Spec.Pool.Name != nodeName {
			return fmt.Errorf("ResourceSlice %q uses pool %q, want %q", slice.Name, slice.Spec.Pool.Name, nodeName)
		}
		if slice.Spec.Pool.ResourceSliceCount != int64(len(pool.Slices)) {
			return fmt.Errorf("ResourceSlice %q reports pool size %d, want %d", slice.Name, slice.Spec.Pool.ResourceSliceCount, len(pool.Slices))
		}
		if index == 0 {
			generation = slice.Spec.Pool.Generation
		} else if slice.Spec.Pool.Generation != generation {
			return fmt.Errorf("ResourceSlice %q has incomplete pool generation %d, want %d", slice.Name, slice.Spec.Pool.Generation, generation)
		}
		actualDevices = append(actualDevices, slice.Spec.Devices...)
	}
	sort.Slice(expectedDevices, func(left, right int) bool { return expectedDevices[left].Name < expectedDevices[right].Name })
	sort.Slice(actualDevices, func(left, right int) bool { return actualDevices[left].Name < actualDevices[right].Name })
	normalizeDeviceCollections(expectedDevices)
	normalizeDeviceCollections(actualDevices)
	if !apiequality.Semantic.DeepEqual(actualDevices, expectedDevices) {
		return fmt.Errorf("published ResourceSlice devices do not match desired inventory")
	}
	return nil
}

// normalizeDeviceCollections treats nil and empty API collections identically.
// The API server drops empty maps during serialization.
func normalizeDeviceCollections(devices []resourceapi.Device) {
	for index := range devices {
		if len(devices[index].Attributes) == 0 {
			devices[index].Attributes = nil
		}
		if len(devices[index].Capacity) == 0 {
			devices[index].Capacity = nil
		}
	}
}
