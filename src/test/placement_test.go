package test

import (
	"reflect"
	"testing"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/placement"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

func TestReservationsAddAssignments(t *testing.T) {
	reservations := placement.Reservations{}
	reservations.AddAssignments([]ttapi.RankAssignment{{Devices: []ttapi.AssignedDevice{{Pool: "pool", Name: "device"}}}})
	if _, found := reservations[placement.DeviceID{Pool: "pool", Name: "device"}]; !found {
		t.Fatal("assignment was not reserved")
	}
}

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

func workloadWithRanks(ranks ...ttapi.WorkloadRank) *ttapi.Workload {
	return &ttapi.Workload{Spec: ttapi.WorkloadSpec{Ranks: ranks}, ObjectMeta: metav1.ObjectMeta{Name: "job"}}
}
