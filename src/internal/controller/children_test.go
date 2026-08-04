package controller

import (
	"context"
	"testing"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// TestEnsureChildrenRejectsNameCollisions verifies existing names require exact ownership and specs.
func TestEnsureChildrenRejectsNameCollisions(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{Rank: "rank-0", NodeName: "node-a", ClaimName: "child", PodName: "child", Devices: []ttapi.AssignedDevice{{Pool: "node-a", Name: "device", StableID: "uuid-device"}}}
	rank := workload.Spec.Ranks[0]
	foreignClaim := buildClaim(workload, rank, assignment)
	foreignClaim.OwnerReferences = nil
	controller := &Controller{Kube: fake.NewSimpleClientset(foreignClaim)}
	if err := controller.ensureClaim(context.Background(), workload, rank, assignment); err == nil {
		t.Fatal("foreign ResourceClaim collision was accepted")
	}
	foreignPod, err := buildPod(workload, 0, assignment, false)
	if err != nil {
		t.Fatal(err)
	}
	foreignPod.OwnerReferences = nil
	controller.Kube = fake.NewSimpleClientset(foreignPod)
	if err := controller.ensurePod(context.Background(), workload, 0, assignment); err == nil {
		t.Fatal("foreign Pod collision was accepted")
	}
}

// TestEnsurePodAcceptsItsScheduledChild verifies pinned defaults keep exact-spec reconciliation stable.
func TestEnsurePodAcceptsItsScheduledChild(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{NodeName: "node-a", ClaimName: "claim", PodName: "pod"}
	controller := &Controller{Kube: fake.NewSimpleClientset()}
	if err := controller.ensurePod(context.Background(), workload, 0, assignment); err != nil {
		t.Fatal(err)
	}
	pod, err := controller.Kube.CoreV1().Pods(workload.Namespace).Get(context.Background(), assignment.PodName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.Spec.NodeName = assignment.NodeName
	if _, err := controller.Kube.CoreV1().Pods(workload.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.ensurePod(context.Background(), workload, 0, assignment); err != nil {
		t.Fatalf("scheduled controller child was rejected: %v", err)
	}
}

// TestBuildPodAppliesRestrictedSecurity verifies every generated rank Pod is hardened.
func TestBuildPodAppliesRestrictedSecurity(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{NodeName: "node-a", ClaimName: "claim", PodName: "pod"}
	pod, err := buildPod(workload, 0, assignment, false)
	if err != nil {
		t.Fatal(err)
	}
	container := pod.Spec.Containers[0]
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken ||
		pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.SeccompProfile == nil || pod.Spec.SecurityContext.AppArmorProfile == nil ||
		container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil ||
		*container.SecurityContext.AllowPrivilegeEscalation || container.SecurityContext.ReadOnlyRootFilesystem == nil ||
		!*container.SecurityContext.ReadOnlyRootFilesystem || len(container.SecurityContext.Capabilities.Drop) != 1 {
		t.Fatalf("Pod security baseline is incomplete: %#v", pod.Spec)
	}
	validationPod, err := buildPod(workload, 0, assignment, true)
	if err != nil || validationPod.Spec.SecurityContext.AppArmorProfile != nil || validationPod.Spec.SecurityContext.SeccompProfile == nil {
		t.Fatalf("synthetic AppArmor override changed the remaining security baseline: %#v, %v", validationPod.Spec, err)
	}
}

// TestValidateWorkloadRejectsUnsafeTemplates verifies admission defense remains fail-closed in the controller.
func TestValidateWorkloadRejectsUnsafeTemplates(t *testing.T) {
	tests := map[string]func(*ttapi.Workload){
		"missing target":  func(workload *ttapi.Workload) { workload.Spec.ContainerName = "missing" },
		"host namespace":  func(workload *ttapi.Workload) { workload.Spec.PodTemplate.Spec.HostPID = true },
		"service account": func(workload *ttapi.Workload) { workload.Spec.PodTemplate.Spec.ServiceAccountName = "admin" },
		"host path": func(workload *ttapi.Workload) {
			workload.Spec.PodTemplate.Spec.Volumes = []corev1.Volume{{VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		},
		"reserved environment": func(workload *ttapi.Workload) {
			workload.Spec.PodTemplate.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "TT_RANK", Value: "9"}}
		},
		"init target": func(workload *ttapi.Workload) {
			workload.Spec.ContainerName = "setup"
			workload.Spec.PodTemplate.Spec.InitContainers = []corev1.Container{{Name: "setup", Image: "example.invalid/setup"}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			workload := safeWorkload()
			mutate(workload)
			if err := validateWorkload(workload); err == nil {
				t.Fatal("unsafe workload was accepted")
			}
		})
	}
}

// TestSetWorkloadStatusPreservesTransitionTime verifies stable conditions do not look like new transitions.
func TestSetWorkloadStatusPreservesTransitionTime(t *testing.T) {
	workload := safeWorkload()
	workload.Generation = 7
	old := metav1.NewTime(time.Unix(1, 0).UTC())
	workload.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Pending", LastTransitionTime: old}}
	setWorkloadStatus(workload, "Pending", false, "Pending", "still waiting")
	if !workload.Status.Conditions[0].LastTransitionTime.Equal(&old) || workload.Status.Conditions[0].ObservedGeneration != 7 {
		t.Fatalf("stable transition metadata changed: %#v", workload.Status.Conditions[0])
	}
	setWorkloadStatus(workload, "Failed", false, "Invalid", "failed")
	if workload.Status.Conditions[0].LastTransitionTime.Equal(&old) {
		t.Fatal("changed condition retained its old transition time")
	}
}

// safeWorkload returns a minimal workload accepted by the production validator.
func safeWorkload() *ttapi.Workload {
	return &ttapi.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "default", UID: types.UID("job-uid")},
		Spec: ttapi.WorkloadSpec{
			ContainerName: "worker",
			PodTemplate: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "worker", Image: "example.invalid/worker"}},
			}},
			Ranks: []ttapi.WorkloadRank{{Name: "rank-0", DeviceClassName: dra.GenericDeviceClassName, Count: 1}},
		},
	}
}
