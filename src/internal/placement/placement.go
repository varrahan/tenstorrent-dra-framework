package placement

import (
	"sort"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
)

type DeviceID struct {
	Pool string
	Name string
}

type Reservations map[DeviceID]struct{}

func (r Reservations) Add(pool, name string) {
	r[DeviceID{Pool: pool, Name: name}] = struct{}{}
}

func (r Reservations) AddAssignments(assignments []ttapi.RankAssignment) {
	for _, assignment := range assignments {
		r.AddAssignment(assignment)
	}
}

func (r Reservations) AddAssignment(assignment ttapi.RankAssignment) {
	for _, item := range assignment.Devices {
		r.Add(item.Pool, item.Name)
	}
}

func Solve(workload *ttapi.Workload, endpoints []ttapi.FabricEndpoint, reserved Reservations) ([]ttapi.RankAssignment, bool) {
	candidates := append([]ttapi.FabricEndpoint(nil), endpoints...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].NodeName == candidates[j].NodeName {
			return candidates[i].DeviceName < candidates[j].DeviceName
		}
		return candidates[i].NodeName < candidates[j].NodeName
	})

	selected := Reservations{}
	assignments := make([]ttapi.RankAssignment, len(workload.Spec.Ranks))
	var chooseRank func(int) bool
	chooseRank = func(index int) bool {
		if index == len(workload.Spec.Ranks) {
			return connected(assignments, candidates)
		}

		rank := workload.Spec.Ranks[index]
		count := rank.Count
		if count <= 0 {
			count = 1
		}
		byNode := candidatesByNode(workload, rank, candidates, reserved, selected)
		for _, nodeName := range sortedNodeNames(byNode) {
			choices := byNode[nodeName]
			if int64(len(choices)) < count {
				continue
			}
			if chooseCombinations(choices, int(count), func(picked []ttapi.FabricEndpoint) bool {
				assignment := makeAssignment(workload.Name, rank.Name, nodeName, picked)
				assignments[index] = assignment
				selected.AddAssignment(assignment)
				if chooseRank(index + 1) {
					return true
				}
				for _, item := range assignment.Devices {
					delete(selected, DeviceID{Pool: item.Pool, Name: item.Name})
				}
				return false
			}) {
				return true
			}
		}
		return false
	}
	return assignments, chooseRank(0)
}

func candidatesByNode(workload *ttapi.Workload, rank ttapi.WorkloadRank, endpoints []ttapi.FabricEndpoint, reserved, selected Reservations) map[string][]ttapi.FabricEndpoint {
	result := map[string][]ttapi.FabricEndpoint{}
	for _, item := range endpoints {
		id := DeviceID{Pool: item.Pool, Name: item.DeviceName}
		if _, found := reserved[id]; found {
			continue
		}
		if _, found := selected[id]; found {
			continue
		}
		if !matches(rank, item) || !matchesTopology(workload.Spec.Topology, item) {
			continue
		}
		result[item.NodeName] = append(result[item.NodeName], item)
	}
	return result
}

func matches(rank ttapi.WorkloadRank, item ttapi.FabricEndpoint) bool {
	if rank.ChipSeries != "" && item.ChipSeries != rank.ChipSeries {
		return false
	}
	if rank.CardSeries != "" && item.CardSeries != rank.CardSeries {
		return false
	}
	return dra.MatchesDeviceClass(rank.DeviceClassName, item.ChipSeries, item.CardSeries)
}

func matchesTopology(topology ttapi.WorkloadTopology, item ttapi.FabricEndpoint) bool {
	if topology.FabricID != "" && item.FabricID != topology.FabricID {
		return false
	}
	return topology.RingID == "" || item.RingID == topology.RingID
}

func sortedNodeNames(devices map[string][]ttapi.FabricEndpoint) []string {
	names := make([]string, 0, len(devices))
	for name := range devices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func chooseCombinations(items []ttapi.FabricEndpoint, count int, visit func([]ttapi.FabricEndpoint) bool) bool {
	picked := make([]ttapi.FabricEndpoint, 0, count)
	var choose func(int) bool
	choose = func(start int) bool {
		if len(picked) == count {
			return visit(picked)
		}
		remaining := count - len(picked)
		for i := start; i <= len(items)-remaining; i++ {
			picked = append(picked, items[i])
			if choose(i + 1) {
				return true
			}
			picked = picked[:len(picked)-1]
		}
		return false
	}
	return choose(0)
}

func makeAssignment(workloadName, rankName, nodeName string, devices []ttapi.FabricEndpoint) ttapi.RankAssignment {
	name := workloadName + "-" + rankName
	assignment := ttapi.RankAssignment{Rank: rankName, NodeName: nodeName, ClaimName: name, PodName: name}
	for _, item := range devices {
		assignment.Devices = append(assignment.Devices, ttapi.AssignedDevice{
			Pool:       item.Pool,
			Name:       item.DeviceName,
			StableID:   item.StableID,
			EndpointID: item.EndpointID,
		})
	}
	return assignment
}

func connected(assignments []ttapi.RankAssignment, endpoints []ttapi.FabricEndpoint) bool {
	selected := map[string]struct{}{}
	for _, assignment := range assignments {
		for _, item := range assignment.Devices {
			selected[item.EndpointID] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return false
	}

	byID := make(map[string]ttapi.FabricEndpoint, len(endpoints))
	for _, item := range endpoints {
		byID[item.EndpointID] = item
	}
	var fabricID, ringID, start string
	for id := range selected {
		item, found := byID[id]
		if !found {
			return false
		}
		if start == "" {
			start, fabricID, ringID = id, item.FabricID, item.RingID
		}
		if item.FabricID != fabricID || item.RingID != ringID {
			return false
		}
	}

	seen := map[string]struct{}{start: {}}
	queue := []string{start}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, link := range byID[id].Links {
			if link.State != "up" {
				continue
			}
			if _, wanted := selected[link.RemoteEndpointID]; !wanted {
				continue
			}
			if _, visited := seen[link.RemoteEndpointID]; visited {
				continue
			}
			seen[link.RemoteEndpointID] = struct{}{}
			queue = append(queue, link.RemoteEndpointID)
		}
	}
	return len(seen) == len(selected)
}
