package controller

import (
	"context"
	"fmt"
	"strings"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Controller) ensureChildren(ctx context.Context, workload *ttapi.Workload) error {
	if len(workload.Status.Assignments) != len(workload.Spec.Ranks) {
		return fmt.Errorf("workload has %d assignments for %d ranks", len(workload.Status.Assignments), len(workload.Spec.Ranks))
	}
	for index, assignment := range workload.Status.Assignments {
		if err := c.ensureClaim(ctx, workload, workload.Spec.Ranks[index], assignment); err != nil {
			return err
		}
		if err := c.ensurePod(ctx, workload, index, assignment); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) ensureClaim(ctx context.Context, workload *ttapi.Workload, rank ttapi.WorkloadRank, assignment ttapi.RankAssignment) error {
	_, err := c.Kube.ResourceV1().ResourceClaims(workload.Namespace).Create(ctx, buildClaim(workload, rank, assignment), metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func buildClaim(workload *ttapi.Workload, rank ttapi.WorkloadRank, assignment ttapi.RankAssignment) *resourceapi.ResourceClaim {
	ids := make([]string, 0, len(assignment.Devices))
	for _, item := range assignment.Devices {
		ids = append(ids, fmt.Sprintf("%q", item.StableID))
	}
	expression := fmt.Sprintf(
		"device.attributes[%q].nodeName == %q && device.attributes[%q].deviceID in [%s]",
		dra.AttributeDomain, assignment.NodeName, dra.AttributeDomain, strings.Join(ids, ","),
	)
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            assignment.ClaimName,
			Namespace:       workload.Namespace,
			Labels:          map[string]string{"tenstorrent.com/workload-uid": string(workload.UID)},
			OwnerReferences: owner(workload),
		},
		Spec: resourceapi.ResourceClaimSpec{Devices: resourceapi.DeviceClaim{Requests: []resourceapi.DeviceRequest{{
			Name: "accelerator",
			Exactly: &resourceapi.ExactDeviceRequest{
				DeviceClassName: rank.DeviceClassName,
				AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
				Count:           int64(len(assignment.Devices)),
				Selectors:       []resourceapi.DeviceSelector{{CEL: &resourceapi.CELDeviceSelector{Expression: expression}}},
			},
		}}}},
	}
}

func (c *Controller) ensurePod(ctx context.Context, workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment) error {
	pod, err := buildPod(workload, rankIndex, assignment)
	if err != nil {
		return err
	}
	_, err = c.Kube.CoreV1().Pods(workload.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func buildPod(workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment) (*corev1.Pod, error) {
	pod := &corev1.Pod{
		ObjectMeta: *workload.Spec.PodTemplate.ObjectMeta.DeepCopy(),
		Spec:       *workload.Spec.PodTemplate.Spec.DeepCopy(),
	}
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
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != workload.Spec.ContainerName {
			continue
		}
		container := &pod.Spec.Containers[i]
		container.Resources.Claims = append(container.Resources.Claims, corev1.ResourceClaim{Name: "tenstorrent"})
		container.Env = append(container.Env,
			corev1.EnvVar{Name: "TT_RANK", Value: fmt.Sprint(rankIndex)},
			corev1.EnvVar{Name: "TT_WORLD_SIZE", Value: fmt.Sprint(len(workload.Spec.Ranks))},
		)
		return pod, nil
	}
	return nil, fmt.Errorf("container %q not found in Pod template", workload.Spec.ContainerName)
}

func (c *Controller) deleteChildren(ctx context.Context, workload *ttapi.Workload) error {
	for _, assignment := range workload.Status.Assignments {
		if err := c.Kube.CoreV1().Pods(workload.Namespace).Delete(ctx, assignment.PodName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete Pod %q: %w", assignment.PodName, err)
		}
		if err := c.Kube.ResourceV1().ResourceClaims(workload.Namespace).Delete(ctx, assignment.ClaimName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete ResourceClaim %q: %w", assignment.ClaimName, err)
		}
	}
	return nil
}

func owner(workload *ttapi.Workload) []metav1.OwnerReference {
	controller := true
	return []metav1.OwnerReference{{
		APIVersion: ttapi.SchedulingAPIVersion,
		Kind:       ttapi.WorkloadKind,
		Name:       workload.Name,
		UID:        workload.UID,
		Controller: &controller,
	}}
}
