package controller

import (
	"context"
	"fmt"
	"strings"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
)

// ensureChildren creates the ResourceClaim and Pod required for every assigned rank.
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

// ensureClaim idempotently creates the exact-device ResourceClaim for one rank.
func (c *Controller) ensureClaim(ctx context.Context, workload *ttapi.Workload, rank ttapi.WorkloadRank, assignment ttapi.RankAssignment) error {
	desired := buildClaim(workload, rank, assignment)
	claims := c.Kube.ResourceV1().ResourceClaims(workload.Namespace)
	_, err := claims.Create(ctx, desired, metav1.CreateOptions{})
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := claims.Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !ownedBy(existing.OwnerReferences, workload.UID) || !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		return fmt.Errorf("ResourceClaim %q collides with a different owner or spec", desired.Name)
	}
	return nil
}

// buildClaim selects the assignment's exact stable device IDs on its chosen node.
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
			Name:      assignment.ClaimName,
			Namespace: workload.Namespace,
			Labels: map[string]string{
				"tenstorrent.com/workload-uid":  string(workload.UID),
				"tenstorrent.com/workload-name": workload.Name,
			},
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

// ensurePod idempotently creates the node-pinned Pod for one assigned rank.
func (c *Controller) ensurePod(ctx context.Context, workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment) error {
	pod, err := buildPod(workload, rankIndex, assignment)
	if err != nil {
		return err
	}
	kubescheme.Scheme.Default(pod)
	pods := c.Kube.CoreV1().Pods(workload.Namespace)
	_, err = pods.Create(ctx, pod, metav1.CreateOptions{})
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	existing, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if existing.Spec.NodeName != "" && existing.Spec.NodeName != assignment.NodeName {
		return fmt.Errorf("Pod %q is scheduled on unexpected node %q", pod.Name, existing.Spec.NodeName)
	}
	actual := existing.Spec.DeepCopy()
	actual.NodeName = pod.Spec.NodeName
	if !ownedBy(existing.OwnerReferences, workload.UID) || !apiequality.Semantic.DeepEqual(*actual, pod.Spec) {
		return fmt.Errorf("Pod %q collides with a different owner or spec", pod.Name)
	}
	return nil
}

// buildPod injects node placement, the DRA claim, and distributed rank environment variables.
func buildPod(workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment) (*corev1.Pod, error) {
	templateMeta := workload.Spec.PodTemplate.ObjectMeta.DeepCopy()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: templateMeta.Labels, Annotations: templateMeta.Annotations}, Spec: *workload.Spec.PodTemplate.Spec.DeepCopy()}
	pod.Name, pod.Namespace, pod.OwnerReferences = assignment.PodName, workload.Namespace, owner(workload)
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels["tenstorrent.com/workload"] = workload.Name
	pod.Labels["tenstorrent.com/workload-name"] = workload.Name
	pod.Labels["tenstorrent.com/workload-uid"] = string(workload.UID)
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
		hardenPod(pod)
		return pod, nil
	}
	return nil, fmt.Errorf("container %q not found in Pod template", workload.Spec.ContainerName)
}

// hardenPod applies the production baseline to every controller-created container.
func hardenPod(pod *corev1.Pod) {
	trueValue, falseValue := true, false
	pod.Spec.AutomountServiceAccountToken = &falseValue
	if pod.Spec.SecurityContext == nil {
		pod.Spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	pod.Spec.SecurityContext.RunAsNonRoot = &trueValue
	pod.Spec.SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	pod.Spec.SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}
	containers := []*[]corev1.Container{&pod.Spec.InitContainers, &pod.Spec.Containers}
	for _, group := range containers {
		for index := range *group {
			container := &(*group)[index]
			if container.SecurityContext == nil {
				container.SecurityContext = &corev1.SecurityContext{}
			}
			container.SecurityContext.Privileged = &falseValue
			container.SecurityContext.AllowPrivilegeEscalation = &falseValue
			container.SecurityContext.ReadOnlyRootFilesystem = &trueValue
			container.SecurityContext.RunAsNonRoot = &trueValue
			container.SecurityContext.Capabilities = &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}
		}
	}
}

// deleteChildren removes unstarted Pods and claims before an assignment is recomputed.
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

// owner returns the controlling owner reference used by workload child resources.
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

// ownedBy reports whether an object has the expected controlling workload UID.
func ownedBy(references []metav1.OwnerReference, uid types.UID) bool {
	for _, reference := range references {
		if reference.Controller != nil && *reference.Controller && reference.UID == uid {
			return true
		}
	}
	return false
}
