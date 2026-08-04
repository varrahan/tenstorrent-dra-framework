package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	tttopology "github.com/varrahan/tenstorrent-dra-framework/src/internal/topology"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestBuildFabricValidReciprocalGraph verifies reciprocal links produce a valid deterministic graph.
func TestBuildFabricValidReciprocalGraph(t *testing.T) {
	now := time.Now().UTC()
	nodes := []ttapi.NodeTopology{
		nodeTopology("worker-b", now, topologyDevice("b", "endpoint-b", "endpoint-a")),
		nodeTopology("worker-a", now, topologyDevice("a", "endpoint-a", "endpoint-b")),
	}
	status := tttopology.BuildFabric(nodes, time.Minute, now)
	if !status.Valid || len(status.Errors) != 0 || len(status.Endpoints) != 2 {
		t.Fatalf("valid graph rejected: %#v", status)
	}
	if status.Endpoints[0].EndpointID != "endpoint-a" || status.Generation == "" {
		t.Fatalf("graph was not normalized deterministically: %#v", status)
	}
}

// TestBuildFabricRejectsInvalidGraphs verifies stale, duplicate, missing, and nonreciprocal topology is rejected.
func TestBuildFabricRejectsInvalidGraphs(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name      string
		nodes     []ttapi.NodeTopology
		wantError string
	}{
		{name: "stale", nodes: []ttapi.NodeTopology{nodeTopology("worker-a", now.Add(-2*time.Minute), topologyDevice("a", "endpoint-a", ""))}, wantError: "stale"},
		{name: "duplicate", nodes: []ttapi.NodeTopology{
			nodeTopology("worker-a", now, topologyDevice("a", "endpoint-a", "")),
			nodeTopology("worker-b", now, topologyDevice("b", "endpoint-a", "")),
		}, wantError: "duplicate endpoint"},
		{name: "missing peer", nodes: []ttapi.NodeTopology{nodeTopology("worker-a", now, topologyDevice("a", "endpoint-a", "missing"))}, wantError: "missing peer"},
		{name: "asymmetric", nodes: []ttapi.NodeTopology{
			nodeTopology("worker-a", now, topologyDevice("a", "endpoint-a", "endpoint-b")),
			nodeTopology("worker-b", now, topologyDevice("b", "endpoint-b", "")),
		}, wantError: "not reciprocal"},
		{name: "cross ring", nodes: []ttapi.NodeTopology{
			nodeTopology("worker-a", now, topologyDevice("a", "endpoint-a", "endpoint-b")),
			nodeTopology("worker-b", now, func() ttapi.TopologyDevice {
				item := topologyDevice("b", "endpoint-b", "endpoint-a")
				item.RingID = "ring-b"
				return item
			}()),
		}, wantError: "crosses fabric or ring"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			status := tttopology.BuildFabric(testCase.nodes, time.Minute, now)
			if status.Valid || len(status.Errors) == 0 || !strings.Contains(strings.Join(status.Errors, "\n"), testCase.wantError) {
				t.Fatalf("status = %#v, want error containing %q", status, testCase.wantError)
			}
		})
	}
}

// TestPublishNodeCreatesAndUpdatesTopology verifies node topology publication supports create and update.
func TestPublishNodeCreatesAndUpdatesTopology(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	snapshot := device.InventorySnapshot{ObservedAt: time.Now().UTC(), Devices: []device.InventoryDevice{{
		StableID: "pci-a", Eligible: true, ChipSeries: "blackhole",
		Fabric: device.FabricInfo{FabricID: "fabric-a", RingID: "ring-a", EndpointID: "endpoint-a"},
	}}}
	if err := tttopology.PublishNode(context.Background(), client, "worker-a", types.UID("node-uid"), snapshot); err != nil {
		t.Fatal(err)
	}
	object, err := client.Resource(ttapi.NodeTopologyGVR).Get(context.Background(), "worker-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var topology ttapi.NodeTopology
	if err := ttapi.FromUnstructured(object, &topology); err != nil {
		t.Fatal(err)
	}
	if len(topology.Spec.Devices) != 1 || topology.Spec.Devices[0].ChipSeries != "blackhole" {
		t.Fatalf("published topology = %#v", topology.Spec)
	}

	snapshot.Devices = nil
	if err := tttopology.PublishNode(context.Background(), client, "worker-a", types.UID("node-uid"), snapshot); err != nil {
		t.Fatal(err)
	}
	object, err = client.Resource(ttapi.NodeTopologyGVR).Get(context.Background(), "worker-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ttapi.FromUnstructured(object, &topology); err != nil {
		t.Fatal(err)
	}
	if len(topology.Spec.Devices) != 0 {
		t.Fatalf("updated topology still has devices: %#v", topology.Spec.Devices)
	}
}

// TestPublishNodePropagatesGetError verifies unexpected API lookup failures reach the caller.
func TestPublishNodePropagatesGetError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("get", ttapi.NodeTopologyGVR.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API unavailable")
	})
	err := tttopology.PublishNode(context.Background(), client, "worker-a", "node-uid", device.InventorySnapshot{})
	if err == nil || !strings.Contains(err.Error(), "API unavailable") {
		t.Fatalf("PublishNode error = %v", err)
	}
}

// nodeTopology constructs one timestamped node observation for fabric tests.
func nodeTopology(name string, observedAt time.Time, devices ...ttapi.TopologyDevice) ttapi.NodeTopology {
	return ttapi.NodeTopology{Spec: ttapi.NodeTopologySpec{NodeName: name, ObservedAt: metav1.NewTime(observedAt), Devices: devices}}
}

// topologyDevice constructs one endpoint with an optional reciprocal-link target.
func topologyDevice(name, endpointID, remoteID string) ttapi.TopologyDevice {
	item := ttapi.TopologyDevice{
		Pool: name, Name: name, StableID: "pci-" + name, ChipSeries: "wormhole",
		FabricID: "fabric-a", RingID: "ring-a", EndpointID: endpointID,
	}
	if remoteID != "" {
		item.Links = []ttapi.TopologyLink{{Name: "eth0", State: "up", RemoteEndpointID: remoteID}}
	}
	return item
}
