package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/topology"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Controller struct {
	Kube        kubernetes.Interface
	Dynamic     dynamic.Interface
	Interval    time.Duration
	TopologyTTL time.Duration
}

func (c *Controller) Run(ctx context.Context) error {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	for {
		if err := c.reconcile(ctx); err != nil {
			fmt.Printf("controller reconciliation: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(c.Interval):
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) error {
	fabric, err := c.reconcileFabric(ctx)
	if err != nil {
		return err
	}
	return c.reconcileWorkloads(ctx, fabric)
}

func (c *Controller) reconcileFabric(ctx context.Context) (ttapi.FabricTopologyStatus, error) {
	list, err := c.Dynamic.Resource(ttapi.NodeTopologyGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ttapi.FabricTopologyStatus{}, err
	}
	nodes := make([]ttapi.NodeTopology, 0, len(list.Items))
	for i := range list.Items {
		var node ttapi.NodeTopology
		if err := ttapi.FromUnstructured(&list.Items[i], &node); err == nil {
			nodes = append(nodes, node)
		}
	}
	status := topology.BuildFabric(nodes, c.TopologyTTL, time.Now().UTC())
	resource := c.Dynamic.Resource(ttapi.FabricTopologyGVR)
	current, err := resource.Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		object, _ := ttapi.ToUnstructured(&ttapi.FabricTopology{TypeMeta: metav1.TypeMeta{APIVersion: "topology.tenstorrent.com/v1alpha1", Kind: "TenstorrentFabricTopology"}, ObjectMeta: metav1.ObjectMeta{Name: "cluster"}})
		current, err = resource.Create(ctx, object, metav1.CreateOptions{})
	}
	if err != nil {
		return status, err
	}
	statusMap, _ := ttapi.ToUnstructured(&ttapi.FabricTopology{Status: status})
	current.Object["status"] = statusMap.Object["status"]
	_, err = resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	return status, err
}

func (c *Controller) reconcileWorkloads(ctx context.Context, fabric ttapi.FabricTopologyStatus) error {
	list, err := c.Dynamic.Resource(ttapi.WorkloadGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	claims, err := c.Kube.ResourceV1().ResourceClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, claim := range claims.Items {
		if claim.Status.Allocation == nil {
			continue
		}
		for _, result := range claim.Status.Allocation.Devices.Results {
			used[result.Pool+"/"+result.Device] = true
		}
	}
	for i := range list.Items {
		var workload ttapi.Workload
		if err := ttapi.FromUnstructured(&list.Items[i], &workload); err != nil {
			continue
		}
		for _, assignment := range workload.Status.Assignments {
			for _, item := range assignment.Devices {
				used[item.Pool+"/"+item.Name] = true
			}
		}
	}
	for i := range list.Items {
		var workload ttapi.Workload
		if err := ttapi.FromUnstructured(&list.Items[i], &workload); err != nil {
			continue
		}
		if err := c.reconcileWorkload(ctx, &list.Items[i], &workload, fabric, used); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileWorkload(ctx context.Context, raw interface{ GetNamespace() string }, workload *ttapi.Workload, fabric ttapi.FabricTopologyStatus, used map[string]bool) error {
	if len(workload.Status.Assignments) > 0 {
		if workload.Status.FabricGeneration != fabric.Generation && !c.anyRankStarted(ctx, workload) {
			if err := c.deleteChildren(ctx, workload); err != nil {
				return err
			}
			workload.Status = ttapi.WorkloadStatus{Phase: "Pending"}
			return c.updateWorkloadStatus(ctx, workload)
		}
		return c.ensureChildren(ctx, workload)
	}
	if !fabric.Valid {
		workload.Status = ttapi.WorkloadStatus{Phase: "Pending", Conditions: condition(false, "FabricInvalid", "validated fabric topology is unavailable")}
		return c.updateWorkloadStatus(ctx, workload)
	}
	assignments, ok := solve(workload, fabric.Endpoints, used)
	if !ok {
		workload.Status = ttapi.WorkloadStatus{Phase: "Pending", FabricGeneration: fabric.Generation, Conditions: condition(false, "Unsatisfied", "no connected assignment satisfies all ranks")}
		return c.updateWorkloadStatus(ctx, workload)
	}
	workload.Status = ttapi.WorkloadStatus{Phase: "Assigned", FabricGeneration: fabric.Generation, Assignments: assignments, Conditions: condition(true, "Assigned", "complete connected assignment reserved")}
	if err := c.updateWorkloadStatus(ctx, workload); err != nil {
		return err
	}
	return c.ensureChildren(ctx, workload)
}

func solve(workload *ttapi.Workload, endpoints []ttapi.FabricEndpoint, used map[string]bool) ([]ttapi.RankAssignment, bool) {
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].NodeName == endpoints[j].NodeName {
			return endpoints[i].DeviceName < endpoints[j].DeviceName
		}
		return endpoints[i].NodeName < endpoints[j].NodeName
	})
	selected := map[string]bool{}
	assignments := make([]ttapi.RankAssignment, len(workload.Spec.Ranks))
	var choose func(int) bool
	choose = func(index int) bool {
		if index == len(workload.Spec.Ranks) {
			return connected(assignments, endpoints)
		}
		rank := workload.Spec.Ranks[index]
		count := rank.Count
		if count == 0 {
			count = 1
		}
		nodes := map[string][]ttapi.FabricEndpoint{}
		for _, item := range endpoints {
			if used[item.Pool+"/"+item.DeviceName] || selected[item.EndpointID] || !matches(rank, item) {
				continue
			}
			if workload.Spec.Topology.FabricID != "" && item.FabricID != workload.Spec.Topology.FabricID {
				continue
			}
			if workload.Spec.Topology.RingID != "" && item.RingID != workload.Spec.Topology.RingID {
				continue
			}
			nodes[item.NodeName] = append(nodes[item.NodeName], item)
		}
		names := make([]string, 0, len(nodes))
		for name := range nodes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			choices := nodes[name]
			if int64(len(choices)) < count {
				continue
			}
			picked := choices[:count]
			assignment := ttapi.RankAssignment{Rank: rank.Name, NodeName: name, ClaimName: workload.Name + "-" + rank.Name, PodName: workload.Name + "-" + rank.Name}
			for _, item := range picked {
				selected[item.EndpointID] = true
				assignment.Devices = append(assignment.Devices, ttapi.AssignedDevice{Pool: item.Pool, Name: item.DeviceName, StableID: item.StableID, EndpointID: item.EndpointID})
			}
			assignments[index] = assignment
			if choose(index + 1) {
				return true
			}
			for _, item := range picked {
				delete(selected, item.EndpointID)
			}
		}
		return false
	}
	return assignments, choose(0)
}

func matches(rank ttapi.WorkloadRank, item ttapi.FabricEndpoint) bool {
	if rank.ChipSeries != "" && item.ChipSeries != rank.ChipSeries {
		return false
	}
	if rank.CardSeries != "" && item.CardSeries != rank.CardSeries {
		return false
	}
	switch rank.DeviceClassName {
	case "tenstorrent":
		return true
	case "tenstorrent-wormhole-n150":
		return item.ChipSeries == "wormhole" && item.CardSeries == "n150"
	case "tenstorrent-wormhole-n300":
		return item.ChipSeries == "wormhole" && item.CardSeries == "n300"
	case "tenstorrent-blackhole-p100":
		return item.ChipSeries == "blackhole" && item.CardSeries == "p100"
	case "tenstorrent-blackhole-p150":
		return item.ChipSeries == "blackhole" && item.CardSeries == "p150"
	}
	return false
}

func connected(assignments []ttapi.RankAssignment, endpoints []ttapi.FabricEndpoint) bool {
	ids := map[string]bool{}
	var fabricID, ringID string
	for _, assignment := range assignments {
		for _, item := range assignment.Devices {
			ids[item.EndpointID] = true
		}
	}
	if len(ids) == 0 {
		return false
	}
	byID := map[string]ttapi.FabricEndpoint{}
	for _, item := range endpoints {
		byID[item.EndpointID] = item
	}
	for id := range ids {
		item := byID[id]
		if fabricID == "" {
			fabricID, ringID = item.FabricID, item.RingID
		}
		if item.FabricID != fabricID || item.RingID != ringID {
			return false
		}
	}
	queue := []string{}
	for id := range ids {
		queue = append(queue, id)
		break
	}
	seen := map[string]bool{queue[0]: true}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, link := range byID[id].Links {
			if link.State == "up" && ids[link.RemoteEndpointID] && !seen[link.RemoteEndpointID] {
				seen[link.RemoteEndpointID] = true
				queue = append(queue, link.RemoteEndpointID)
			}
		}
	}
	return len(seen) == len(ids)
}

func (c *Controller) ensureChildren(ctx context.Context, workload *ttapi.Workload) error {
	for index, assignment := range workload.Status.Assignments {
		rank := workload.Spec.Ranks[index]
		if err := c.ensureClaim(ctx, workload, rank, assignment); err != nil {
			return err
		}
		if err := c.ensurePod(ctx, workload, index, assignment); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) ensureClaim(ctx context.Context, workload *ttapi.Workload, rank ttapi.WorkloadRank, assignment ttapi.RankAssignment) error {
	ids := make([]string, 0, len(assignment.Devices))
	for _, item := range assignment.Devices {
		ids = append(ids, fmt.Sprintf("%q", item.StableID))
	}
	expression := fmt.Sprintf("device.attributes[%q].nodeName == %q && device.attributes[%q].deviceID in [%s]", dra.AttributeDomain, assignment.NodeName, dra.AttributeDomain, strings.Join(ids, ","))
	count := int64(len(assignment.Devices))
	claim := &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Name: assignment.ClaimName, Namespace: workload.Namespace, Labels: map[string]string{"tenstorrent.com/workload-uid": string(workload.UID)}, OwnerReferences: owner(workload)}, Spec: resourceapi.ResourceClaimSpec{Devices: resourceapi.DeviceClaim{Requests: []resourceapi.DeviceRequest{{Name: "accelerator", Exactly: &resourceapi.ExactDeviceRequest{DeviceClassName: rank.DeviceClassName, AllocationMode: resourceapi.DeviceAllocationModeExactCount, Count: count, Selectors: []resourceapi.DeviceSelector{{CEL: &resourceapi.CELDeviceSelector{Expression: expression}}}}}}}}}
	_, err := c.Kube.ResourceV1().ResourceClaims(workload.Namespace).Create(ctx, claim, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (c *Controller) ensurePod(ctx context.Context, workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment) error {
	pod := &corev1.Pod{ObjectMeta: *workload.Spec.PodTemplate.ObjectMeta.DeepCopy(), Spec: *workload.Spec.PodTemplate.Spec.DeepCopy()}
	pod.Name, pod.Namespace, pod.OwnerReferences = assignment.PodName, workload.Namespace, owner(workload)
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels["tenstorrent.com/workload"] = workload.Name
	if pod.Spec.NodeSelector == nil {
		pod.Spec.NodeSelector = map[string]string{}
	}
	pod.Spec.NodeSelector[corev1.LabelHostname] = assignment.NodeName
	claimName := assignment.ClaimName
	pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, corev1.PodResourceClaim{Name: "tenstorrent", ResourceClaimName: &claimName})
	found := false
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != workload.Spec.ContainerName {
			continue
		}
		found = true
		pod.Spec.Containers[i].Resources.Claims = append(pod.Spec.Containers[i].Resources.Claims, corev1.ResourceClaim{Name: "tenstorrent"})
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{Name: "TT_RANK", Value: fmt.Sprint(rankIndex)}, corev1.EnvVar{Name: "TT_WORLD_SIZE", Value: fmt.Sprint(len(workload.Spec.Ranks))})
	}
	if !found {
		return fmt.Errorf("container %q not found in Pod template", workload.Spec.ContainerName)
	}
	_, err := c.Kube.CoreV1().Pods(workload.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func (c *Controller) updateWorkloadStatus(ctx context.Context, workload *ttapi.Workload) error {
	resource := c.Dynamic.Resource(ttapi.WorkloadGVR).Namespace(workload.Namespace)
	current, err := resource.Get(ctx, workload.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	status, _ := ttapi.ToUnstructured(&ttapi.Workload{Status: workload.Status})
	current.Object["status"] = status.Object["status"]
	_, err = resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	return err
}
func (c *Controller) anyRankStarted(ctx context.Context, workload *ttapi.Workload) bool {
	for _, assignment := range workload.Status.Assignments {
		pod, err := c.Kube.CoreV1().Pods(workload.Namespace).Get(ctx, assignment.PodName, metav1.GetOptions{})
		if err == nil && pod.Status.StartTime != nil {
			return true
		}
	}
	return false
}
func (c *Controller) deleteChildren(ctx context.Context, workload *ttapi.Workload) error {
	for _, assignment := range workload.Status.Assignments {
		_ = c.Kube.CoreV1().Pods(workload.Namespace).Delete(ctx, assignment.PodName, metav1.DeleteOptions{})
		_ = c.Kube.ResourceV1().ResourceClaims(workload.Namespace).Delete(ctx, assignment.ClaimName, metav1.DeleteOptions{})
	}
	return nil
}
func owner(workload *ttapi.Workload) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{APIVersion: "scheduling.tenstorrent.com/v1alpha1", Kind: "TenstorrentWorkload", Name: workload.Name, UID: workload.UID, Controller: &yes}}
}
func condition(ok bool, reason, message string) []metav1.Condition {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}
	return []metav1.Condition{{Type: "Ready", Status: status, Reason: reason, Message: message, LastTransitionTime: metav1.Now()}}
}

var _ = types.UID("")
