package test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/placement"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSolveFindsConnectedCombinationAndDoesNotMutateInput verifies deterministic connected placement preserves inputs.
func TestSolveFindsConnectedCombinationAndDoesNotMutateInput(t *testing.T) {
	endpoints := []ttapi.FabricEndpoint{
		endpoint("worker-a", "b", "e2"),
		endpoint("worker-a", "c", "e3"),
		endpoint("worker-a", "a", "e1"),
	}
	endpoints[2].Links = []ttapi.TopologyLink{{State: "up", RemoteEndpointID: "e3"}}
	endpoints[1].Links = []ttapi.TopologyLink{{State: "up", RemoteEndpointID: "e1"}}
	original := append([]ttapi.FabricEndpoint(nil), endpoints...)
	workload := workloadWithRanks(ttapi.WorkloadRank{Name: "rank-0", DeviceClassName: dra.GenericDeviceClassName, Count: 2})

	assignments, ok := placement.Solve(workload, endpoints, placement.Reservations{})
	if !ok {
		t.Fatal("connected alternative combination was not found")
	}
	got := []string{assignments[0].Devices[0].Name, assignments[0].Devices[1].Name}
	if !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("selected devices = %v, want [a c]", got)
	}
	if !reflect.DeepEqual(endpoints, original) {
		t.Fatal("Solve mutated its endpoint input")
	}
}

// TestSolveHonorsReservationsClassesAndTopology verifies every placement constraint is enforced.
func TestSolveHonorsReservationsClassesAndTopology(t *testing.T) {
	endpoints := []ttapi.FabricEndpoint{
		endpoint("worker-a", "a", "e1"),
		endpoint("worker-a", "b", "e2"),
		endpoint("worker-b", "c", "e3"),
	}
	for i := range endpoints {
		endpoints[i].Links = []ttapi.TopologyLink{{State: "up", RemoteEndpointID: endpoints[(i+1)%len(endpoints)].EndpointID}}
	}
	endpoints[2].ChipSeries = "blackhole"
	reserved := placement.Reservations{}
	reserved.Add("worker-a", "a")
	workload := workloadWithRanks(ttapi.WorkloadRank{Name: "rank-0", DeviceClassName: dra.WormholeDeviceClassName})
	workload.Spec.Topology = ttapi.WorkloadTopology{FabricID: "fabric-a", RingID: "ring-a"}

	assignments, ok := placement.Solve(workload, endpoints, reserved)
	if !ok || assignments[0].Devices[0].Name != "b" {
		t.Fatalf("assignment = %#v, ok=%v; want unreserved Wormhole device b", assignments, ok)
	}
	reserved.Add("worker-a", "b")
	if _, ok := placement.Solve(workload, endpoints, reserved); ok {
		t.Fatal("solver ignored reservations or specialized DeviceClass")
	}
}

// TestReservationsAddAssignments verifies assignment devices are indexed as reserved.
func TestReservationsAddAssignments(t *testing.T) {
	reservations := placement.Reservations{}
	reservations.AddAssignments([]ttapi.RankAssignment{{Devices: []ttapi.AssignedDevice{{Pool: "pool", Name: "device"}}}})
	if _, found := reservations[placement.DeviceID{Pool: "pool", Name: "device"}]; !found {
		t.Fatal("assignment was not reserved")
	}
}

// TestChildNameIsBoundedAndUIDScoped verifies generated child names are safe and collision resistant.
func TestChildNameIsBoundedAndUIDScoped(t *testing.T) {
	first := placement.ChildName(strings.Repeat("Long_Name", 20), "workload-a", "rank")
	second := placement.ChildName(strings.Repeat("Long_Name", 20), "workload-b", "rank")
	if len(first) > 63 || first == second || strings.HasPrefix(first, "-") || strings.HasSuffix(first, "-") {
		t.Fatalf("unsafe child names: %q %q", first, second)
	}
	if empty := placement.ChildName("___", "workload", "rank"); strings.HasPrefix(empty, "-") {
		t.Fatalf("empty prefix produced an invalid name: %q", empty)
	}
}

// TestSolveContextEnforcesLimitsAndCancellation verifies bounded placement fails promptly.
func TestSolveContextEnforcesLimitsAndCancellation(t *testing.T) {
	workload := workloadWithRanks(ttapi.WorkloadRank{Name: "rank-0", DeviceClassName: dra.GenericDeviceClassName, Count: 1})
	limits := placement.Limits{MaxRanks: 1, MaxEndpoints: 1, MaxDevices: 1}
	if _, _, err := placement.SolveContext(context.Background(), workload, []ttapi.FabricEndpoint{endpoint("a", "a", "a"), endpoint("b", "b", "b")}, nil, limits); err == nil {
		t.Fatal("oversized placement request was accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := placement.SolveContext(canceled, workload, []ttapi.FabricEndpoint{endpoint("a", "a", "a")}, nil, limits); err == nil {
		t.Fatal("canceled placement request did not stop")
	}
}

// TestSolveSupportsMaximumRanks verifies the declared 64-rank boundary succeeds.
func TestSolveSupportsMaximumRanks(t *testing.T) {
	const count = 64
	endpoints := make([]ttapi.FabricEndpoint, count)
	ranks := make([]ttapi.WorkloadRank, count)
	for index := range endpoints {
		id := fmt.Sprintf("endpoint-%02d", index)
		endpoints[index] = endpoint(fmt.Sprintf("node-%02d", index), fmt.Sprintf("device-%02d", index), id)
		ranks[index] = ttapi.WorkloadRank{Name: fmt.Sprintf("rank-%02d", index), DeviceClassName: dra.GenericDeviceClassName, Count: 1}
		if index > 0 {
			endpoints[index].Links = append(endpoints[index].Links, ttapi.TopologyLink{State: "up", RemoteEndpointID: endpoints[index-1].EndpointID})
			endpoints[index-1].Links = append(endpoints[index-1].Links, ttapi.TopologyLink{State: "up", RemoteEndpointID: id})
		}
	}
	assignments, ok, err := placement.SolveContext(context.Background(), workloadWithRanks(ranks...), endpoints, nil, placement.DefaultLimits)
	if err != nil || !ok || len(assignments) != count {
		t.Fatalf("maximum-rank placement failed: assignments=%d ok=%v err=%v", len(assignments), ok, err)
	}
}

// TestSolveSupportsMaximumDevices verifies the declared 128-device boundary succeeds.
func TestSolveSupportsMaximumDevices(t *testing.T) {
	const count = 128
	endpoints := make([]ttapi.FabricEndpoint, count)
	for index := range endpoints {
		id := fmt.Sprintf("endpoint-%03d", index)
		endpoints[index] = endpoint("node", fmt.Sprintf("device-%03d", index), id)
		if index > 0 {
			endpoints[index].Links = append(endpoints[index].Links, ttapi.TopologyLink{State: "up", RemoteEndpointID: endpoints[index-1].EndpointID})
			endpoints[index-1].Links = append(endpoints[index-1].Links, ttapi.TopologyLink{State: "up", RemoteEndpointID: id})
		}
	}
	workload := workloadWithRanks(ttapi.WorkloadRank{Name: "rank", DeviceClassName: dra.GenericDeviceClassName, Count: count})
	assignments, ok, err := placement.SolveContext(context.Background(), workload, endpoints, nil, placement.DefaultLimits)
	if err != nil || !ok || len(assignments[0].Devices) != count {
		t.Fatalf("maximum-device placement failed: ok=%v err=%v", ok, err)
	}
}

// BenchmarkSolveMaximumRanks measures the supported 64-rank placement boundary.
func BenchmarkSolveMaximumRanks(b *testing.B) {
	const count = 64
	endpoints := make([]ttapi.FabricEndpoint, count)
	ranks := make([]ttapi.WorkloadRank, count)
	for index := range endpoints {
		id := fmt.Sprintf("endpoint-%02d", index)
		endpoints[index] = endpoint(fmt.Sprintf("node-%02d", index), fmt.Sprintf("device-%02d", index), id)
		ranks[index] = ttapi.WorkloadRank{Name: fmt.Sprintf("rank-%02d", index), DeviceClassName: dra.GenericDeviceClassName, Count: 1}
		if index > 0 {
			endpoints[index].Links = append(endpoints[index].Links, ttapi.TopologyLink{State: "up", RemoteEndpointID: endpoints[index-1].EndpointID})
			endpoints[index-1].Links = append(endpoints[index-1].Links, ttapi.TopologyLink{State: "up", RemoteEndpointID: id})
		}
	}
	workload := workloadWithRanks(ranks...)
	b.ResetTimer()
	for range b.N {
		if _, ok, err := placement.SolveContext(context.Background(), workload, endpoints, nil, placement.DefaultLimits); err != nil || !ok {
			b.Fatalf("placement failed: ok=%v err=%v", ok, err)
		}
	}
}

// endpoint constructs a healthy Wormhole fabric endpoint for placement tests.
func endpoint(node, name, id string) ttapi.FabricEndpoint {
	return ttapi.FabricEndpoint{
		NodeName:   node,
		Pool:       node,
		DeviceName: name,
		StableID:   "pci-" + name,
		ChipSeries: "wormhole",
		FabricID:   "fabric-a",
		RingID:     "ring-a",
		EndpointID: id,
	}
}

// workloadWithRanks constructs a workload around the supplied placement ranks.
func workloadWithRanks(ranks ...ttapi.WorkloadRank) *ttapi.Workload {
	return &ttapi.Workload{Spec: ttapi.WorkloadSpec{Ranks: ranks}, ObjectMeta: metav1.ObjectMeta{Name: "job"}}
}
