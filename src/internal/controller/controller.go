package controller

import (
	"context"
	"fmt"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/placement"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/topology"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

type Controller struct {
	Kube        kubernetes.Interface
	Dynamic     dynamic.Interface
	Interval    time.Duration
	TopologyTTL time.Duration
}

// Run continuously reconciles cluster fabric state and Tenstorrent workloads.
func (c *Controller) Run(ctx context.Context) error {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		if err := c.reconcile(ctx); err != nil {
			fmt.Printf("controller reconciliation: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// reconcile updates the fabric topology before reconciling workloads against it.
func (c *Controller) reconcile(ctx context.Context) error {
	fabric, err := c.reconcileFabric(ctx)
	if err != nil {
		return err
	}
	return c.reconcileWorkloads(ctx, fabric)
}

// reconcileFabric aggregates node topology objects and publishes cluster fabric status.
func (c *Controller) reconcileFabric(ctx context.Context) (ttapi.FabricTopologyStatus, error) {
	list, err := c.Dynamic.Resource(ttapi.NodeTopologyGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ttapi.FabricTopologyStatus{}, err
	}
	nodes := make([]ttapi.NodeTopology, 0, len(list.Items))
	for i := range list.Items {
		var node ttapi.NodeTopology
		if err := ttapi.FromUnstructured(&list.Items[i], &node); err != nil {
			return ttapi.FabricTopologyStatus{}, fmt.Errorf("decode node topology %q: %w", list.Items[i].GetName(), err)
		}
		nodes = append(nodes, node)
	}
	status := topology.BuildFabric(nodes, c.TopologyTTL, time.Now().UTC())
	resource := c.Dynamic.Resource(ttapi.FabricTopologyGVR)
	current, err := resource.Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		object, convertErr := ttapi.ToUnstructured(&ttapi.FabricTopology{TypeMeta: metav1.TypeMeta{APIVersion: ttapi.TopologyAPIVersion, Kind: ttapi.FabricTopologyKind}, ObjectMeta: metav1.ObjectMeta{Name: "cluster"}})
		if convertErr != nil {
			return status, convertErr
		}
		current, err = resource.Create(ctx, object, metav1.CreateOptions{})
	}
	if err != nil {
		return status, err
	}
	statusMap, err := ttapi.ToUnstructured(&ttapi.FabricTopology{Status: status})
	if err != nil {
		return status, err
	}
	current.Object["status"] = statusMap.Object["status"]
	_, err = resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	return status, err
}

// reconcileWorkloads collects existing reservations and reconciles each workload in one pass.
func (c *Controller) reconcileWorkloads(ctx context.Context, fabric ttapi.FabricTopologyStatus) error {
	list, err := c.Dynamic.Resource(ttapi.WorkloadGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	workloads := make([]ttapi.Workload, 0, len(list.Items))
	for i := range list.Items {
		var workload ttapi.Workload
		if err := ttapi.FromUnstructured(&list.Items[i], &workload); err != nil {
			return fmt.Errorf("decode workload %q: %w", list.Items[i].GetName(), err)
		}
		workloads = append(workloads, workload)
	}
	claims, err := c.Kube.ResourceV1().ResourceClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	used := placement.Reservations{}
	for _, claim := range claims.Items {
		if claim.Status.Allocation == nil {
			continue
		}
		for _, result := range claim.Status.Allocation.Devices.Results {
			used.Add(result.Pool, result.Device)
		}
	}
	for i := range workloads {
		used.AddAssignments(workloads[i].Status.Assignments)
	}
	for i := range workloads {
		if err := c.reconcileWorkload(ctx, &workloads[i], fabric, used); err != nil {
			return err
		}
	}
	return nil
}

// reconcileWorkload preserves safe assignments or computes and materializes a new one.
func (c *Controller) reconcileWorkload(ctx context.Context, workload *ttapi.Workload, fabric ttapi.FabricTopologyStatus, used placement.Reservations) error {
	if len(workload.Status.Assignments) > 0 {
		started, err := c.anyRankStarted(ctx, workload)
		if err != nil {
			return err
		}
		if workload.Status.FabricGeneration != fabric.Generation {
			if started {
				workload.Status.Phase = "Degraded"
				workload.Status.Conditions = condition(false, "FabricChanged", "fabric topology changed after a rank started; assignment is frozen")
				if err := c.updateWorkloadStatus(ctx, workload); err != nil {
					return err
				}
				return c.ensureChildren(ctx, workload)
			}
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
	assignments, ok := placement.Solve(workload, fabric.Endpoints, used)
	if !ok {
		workload.Status = ttapi.WorkloadStatus{Phase: "Pending", FabricGeneration: fabric.Generation, Conditions: condition(false, "Unsatisfied", "no connected assignment satisfies all ranks")}
		return c.updateWorkloadStatus(ctx, workload)
	}
	workload.Status = ttapi.WorkloadStatus{Phase: "Assigned", FabricGeneration: fabric.Generation, Assignments: assignments, Conditions: condition(true, "Assigned", "complete connected assignment reserved")}
	if err := c.updateWorkloadStatus(ctx, workload); err != nil {
		return err
	}
	used.AddAssignments(assignments)
	return c.ensureChildren(ctx, workload)
}

// updateWorkloadStatus writes only the latest status subresource for a workload.
func (c *Controller) updateWorkloadStatus(ctx context.Context, workload *ttapi.Workload) error {
	resource := c.Dynamic.Resource(ttapi.WorkloadGVR).Namespace(workload.Namespace)
	current, err := resource.Get(ctx, workload.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	status, err := ttapi.ToUnstructured(&ttapi.Workload{Status: workload.Status})
	if err != nil {
		return err
	}
	current.Object["status"] = status.Object["status"]
	_, err = resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	return err
}

// anyRankStarted reports whether Kubernetes has started any assigned rank Pod.
func (c *Controller) anyRankStarted(ctx context.Context, workload *ttapi.Workload) (bool, error) {
	for _, assignment := range workload.Status.Assignments {
		pod, err := c.Kube.CoreV1().Pods(workload.Namespace).Get(ctx, assignment.PodName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if pod.Status.StartTime != nil {
			return true, nil
		}
	}
	return false, nil
}

// condition builds the workload's single Ready condition for a reconciliation outcome.
func condition(ok bool, reason, message string) []metav1.Condition {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}
	return []metav1.Condition{{Type: "Ready", Status: status, Reason: reason, Message: message, LastTransitionTime: metav1.Now()}}
}
