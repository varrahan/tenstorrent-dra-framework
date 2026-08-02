package dra

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	resourceclient "k8s.io/client-go/kubernetes/typed/resource/v1"
)

const (
	resourceSliceChunkSize = 128
	nodeLabel              = "tenstorrent.com/node"
)

// ResourceSlicePublisher projects the healthy, locally observed inventory into
// the GA resource.k8s.io/v1 API. It deliberately accepts a typed interface so
// reconciliation is straightforward to test without a cluster.
type ResourceSlicePublisher struct {
	client resourceclient.ResourceV1Interface
	driver string
}

func NewResourceSlicePublisher(client resourceclient.ResourceV1Interface, driver string) (*ResourceSlicePublisher, error) {
	if client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	if strings.TrimSpace(driver) == "" {
		driver = DefaultDriverName
	}
	return &ResourceSlicePublisher{client: client, driver: driver}, nil
}

// Publish replaces the node's projection atomically from the API consumer's
// perspective: desired slices are upserted first, then slices no longer in the
// desired set are removed. Only eligible, healthy, character-device-backed
// cards are published.
func (p *ResourceSlicePublisher) Publish(ctx context.Context, nodeName string, snapshot device.InventorySnapshot) error {
	if strings.TrimSpace(nodeName) == "" {
		return fmt.Errorf("node name is required")
	}
	observed := make([]device.InventoryDevice, 0, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		if item.Eligible && item.CharacterDevicePresent && item.Health == device.HealthHealthy {
			observed = append(observed, item)
		}
	}

	count := (len(observed) + resourceSliceChunkSize - 1) / resourceSliceChunkSize
	if count == 0 {
		count = 1
	}
	generation := poolGeneration(observed)
	desired := make(map[string]struct{}, count)
	for index := 0; index < count; index++ {
		start := index * resourceSliceChunkSize
		end := start + resourceSliceChunkSize
		if end > len(observed) {
			end = len(observed)
		}
		name := sliceName(nodeName, index)
		desired[name] = struct{}{}
		object := NewResourceSlice(p.driver, name, nodeName, nodeName, generation, devicesForInventory(observed[start:end]))
		object.Labels[nodeLabel] = nodeName
		object.Spec.Pool.ResourceSliceCount = int64(count)
		if err := p.upsert(ctx, &object); err != nil {
			return fmt.Errorf("publish ResourceSlice %q: %w", name, err)
		}
	}

	selector := labels.Set{
		"app.kubernetes.io/name":      "tt-dra-driver",
		"app.kubernetes.io/component": "dra-resource-slice",
		nodeLabel:                     nodeName,
	}.AsSelector().String()
	list, err := p.client.ResourceSlices().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return fmt.Errorf("list stale ResourceSlices: %w", err)
	}
	for _, item := range list.Items {
		if _, ok := desired[item.Name]; ok {
			continue
		}
		if err := p.client.ResourceSlices().Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("delete stale ResourceSlice %q: %w", item.Name, err)
		}
	}
	return nil
}

func (p *ResourceSlicePublisher) upsert(ctx context.Context, desired *resourceapi.ResourceSlice) error {
	current, err := p.client.ResourceSlices().Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = p.client.ResourceSlices().Create(ctx, desired, metav1.CreateOptions{})
			return err
		}
		return err
	}
	desired.ResourceVersion = current.ResourceVersion
	_, err = p.client.ResourceSlices().Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

func devicesForInventory(items []device.InventoryDevice) []resourceapi.Device {
	result := make([]resourceapi.Device, 0, len(items))
	for _, item := range items {
		node := item.Node
		chipSeries, cardSeries := item.ChipSeries, item.CardSeries
		// Keep compatibility with callers that construct InventoryDevice from
		// the discovery Node projection rather than the normalized fields.
		if chipSeries == "" {
			chipSeries = node.ChipSeries
		}
		if cardSeries == "" {
			cardSeries = node.CardSeries
		}
		// Runtime paths and major/minor numbers are intentionally omitted from
		// live scheduler objects. They remain node-local lifecycle data.
		attributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			DeviceAttributeDeviceID: StringAttribute(node.ID),
		}
		if item.StableID != "" {
			attributes[DeviceAttributeDeviceID] = StringAttribute(item.StableID)
		}
		if chipSeries != "" {
			attributes[DeviceAttributeChipSeries] = StringAttribute(chipSeries)
		}
		if cardSeries != "" {
			attributes[DeviceAttributeCardSeries] = StringAttribute(cardSeries)
		}
		if spec, ok := CardSpecForClass(chipSeries, cardSeries); ok {
			for name, value := range spec.Attributes() {
				attributes[name] = value
			}
		}
		if item.PCI.BDF != "" {
			attributes[DeviceAttributePCIBDF] = StringAttribute(item.PCI.BDF)
		}
		if item.PCI.NUMANode >= 0 {
			attributes[DeviceAttributeNUMANode] = IntAttribute(int64(item.PCI.NUMANode))
		}
		if item.Fabric.EndpointID != "" {
			attributes[DeviceAttributeEndpointID] = StringAttribute(item.Fabric.EndpointID)
		}
		if item.Fabric.FabricID != "" {
			attributes[DeviceAttributeFabricID] = StringAttribute(item.Fabric.FabricID)
		}
		if item.Fabric.RingID != "" {
			attributes[DeviceAttributeRingID] = StringAttribute(item.Fabric.RingID)
		}
		result = append(result, resourceapi.Device{Name: deviceResourceName(node), Attributes: attributes})
	}
	return result
}

func sliceName(node string, index int) string {
	return fmt.Sprintf("tt-dra-%s-%d", strings.ToLower(strings.ReplaceAll(node, "_", "-")), index)
}

func poolGeneration(items []device.InventoryDevice) int64 {
	hash := sha256.New()
	for _, item := range items {
		node := item.Node
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%d\x00%s\x00%s\n", node.ID, node.Path, node.Major, node.Minor, item.PCI.BDF, item.Fabric.EndpointID)
	}
	value := binary.BigEndian.Uint64(hash.Sum(nil)[:8]) & 0x7fffffffffffffff
	if value == 0 {
		return 1
	}
	return int64(value)
}
