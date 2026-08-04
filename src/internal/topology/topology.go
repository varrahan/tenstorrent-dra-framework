package topology

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// PublishNode creates or updates the node's eligible accelerator topology observation.
func PublishNode(ctx context.Context, client dynamic.Interface, nodeName string, nodeUID types.UID, snapshot device.InventorySnapshot) error {
	object := &ttapi.NodeTopology{
		TypeMeta: metav1.TypeMeta{APIVersion: ttapi.TopologyAPIVersion, Kind: ttapi.NodeTopologyKind},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "Node", Name: nodeName, UID: nodeUID,
			}},
		},
		Spec: ttapi.NodeTopologySpec{NodeName: nodeName, ObservedAt: metav1.NewTime(snapshot.ObservedAt)},
	}
	for _, item := range snapshot.Devices {
		if !item.Eligible {
			continue
		}
		entry := ttapi.TopologyDevice{
			Pool: nodeName, Name: device.DRAName(item), StableID: item.StableID,
			ChipSeries: item.ChipSeries,
			FabricID:   item.Fabric.FabricID, RingID: item.Fabric.RingID, EndpointID: item.Fabric.EndpointID,
		}
		for _, link := range item.Fabric.Links {
			entry.Links = append(entry.Links, ttapi.TopologyLink{
				Name: link.Name, State: link.State, SpeedGbps: link.SpeedGbps, RemoteEndpointID: link.RemoteEndpointID,
			})
		}
		object.Spec.Devices = append(object.Spec.Devices, entry)
	}
	resource := client.Resource(ttapi.NodeTopologyGVR)
	desired, err := ttapi.ToUnstructured(object)
	if err != nil {
		return err
	}
	current, err := resource.Get(ctx, nodeName, metav1.GetOptions{})
	if err == nil {
		desired.SetResourceVersion(current.GetResourceVersion())
		_, err = resource.Update(ctx, desired, metav1.UpdateOptions{})
		return err
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = resource.Create(ctx, desired, metav1.CreateOptions{})
	return err
}

// BuildFabric validates fresh node observations and produces a deterministic cluster fabric graph.
func BuildFabric(nodes []ttapi.NodeTopology, ttl time.Duration, now time.Time) ttapi.FabricTopologyStatus {
	status := ttapi.FabricTopologyStatus{ObservedAt: metav1.NewTime(now), Valid: true}
	seen := map[string]struct{}{}
	for _, node := range nodes {
		if ttl > 0 && now.Sub(node.Spec.ObservedAt.Time) > ttl {
			status.Errors = append(status.Errors, fmt.Sprintf("node %s topology is stale", node.Spec.NodeName))
			continue
		}
		for _, item := range node.Spec.Devices {
			if item.EndpointID == "" {
				continue
			}
			if _, ok := seen[item.EndpointID]; ok {
				status.Errors = append(status.Errors, "duplicate endpoint "+item.EndpointID)
				continue
			}
			seen[item.EndpointID] = struct{}{}
			status.Endpoints = append(status.Endpoints, ttapi.FabricEndpoint{
				NodeName: node.Spec.NodeName, Pool: item.Pool, DeviceName: item.Name, StableID: item.StableID,
				ChipSeries: item.ChipSeries,
				FabricID:   item.FabricID, RingID: item.RingID, EndpointID: item.EndpointID, Links: item.Links,
			})
		}
	}
	sort.Slice(status.Endpoints, func(i, j int) bool { return status.Endpoints[i].EndpointID < status.Endpoints[j].EndpointID })
	byID := map[string]ttapi.FabricEndpoint{}
	for _, item := range status.Endpoints {
		byID[item.EndpointID] = item
	}
	for _, item := range status.Endpoints {
		for _, link := range item.Links {
			if link.State != "up" || link.RemoteEndpointID == "" {
				continue
			}
			peer, ok := byID[link.RemoteEndpointID]
			if !ok {
				status.Errors = append(status.Errors, fmt.Sprintf("endpoint %s references missing peer %s", item.EndpointID, link.RemoteEndpointID))
				continue
			}
			if peer.FabricID != item.FabricID || peer.RingID != item.RingID {
				status.Errors = append(status.Errors, fmt.Sprintf("endpoint %s crosses fabric or ring", item.EndpointID))
				continue
			}
			reciprocal := false
			for _, back := range peer.Links {
				if back.State == "up" && back.RemoteEndpointID == item.EndpointID {
					reciprocal = true
					break
				}
			}
			if !reciprocal {
				status.Errors = append(status.Errors, fmt.Sprintf("link %s to %s is not reciprocal", item.EndpointID, peer.EndpointID))
			}
		}
	}
	status.Valid = len(status.Errors) == 0
	data, _ := json.Marshal(status.Endpoints)
	sum := sha256.Sum256(data)
	status.Generation = hex.EncodeToString(sum[:8])
	reason := "Valid"
	message := "fabric graph is valid"
	conditionStatus := metav1.ConditionTrue
	if !status.Valid {
		reason = "Invalid"
		message = status.Errors[0]
		conditionStatus = metav1.ConditionFalse
	}
	status.Conditions = []metav1.Condition{{Type: "Ready", Status: conditionStatus, Reason: reason, Message: message, LastTransitionTime: metav1.NewTime(now)}}
	return status
}
