package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// TestEnsureChildrenRejectsNameCollisions verifies existing names require exact ownership and specs.
func TestEnsureChildrenRejectsNameCollisions(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{Rank: "rank-0", NodeName: "node-a", ClaimName: "child", PodName: "child", Devices: []ttapi.AssignedDevice{{Pool: "node-a", Name: "device", StableID: "uuid-device"}}}
	rank := workload.Spec.Ranks[0]
	foreignClaim := controller.BuildClaim(workload, rank, assignment)
	foreignClaim.OwnerReferences = nil
	controllerUnderTest := &controller.Controller{Kube: fake.NewSimpleClientset(foreignClaim)}
	if err := controllerUnderTest.EnsureClaim(context.Background(), workload, rank, assignment); err == nil {
		t.Fatal("foreign ResourceClaim collision was accepted")
	}
	foreignPod, err := controller.BuildPod(workload, 0, assignment, false)
	if err != nil {
		t.Fatal(err)
	}
	foreignPod.OwnerReferences = nil
	controllerUnderTest.Kube = fake.NewSimpleClientset(foreignPod)
	if err := controllerUnderTest.EnsurePod(context.Background(), workload, 0, assignment); err == nil {
		t.Fatal("foreign Pod collision was accepted")
	}
}

// TestEnsurePodAcceptsItsScheduledChild verifies pinned defaults keep exact-spec reconciliation stable.
func TestEnsurePodAcceptsItsScheduledChild(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{NodeName: "node-a", ClaimName: "claim", PodName: "pod"}
	controllerUnderTest := &controller.Controller{Kube: fake.NewSimpleClientset()}
	if err := controllerUnderTest.EnsurePod(context.Background(), workload, 0, assignment); err != nil {
		t.Fatal(err)
	}
	pod, err := controllerUnderTest.Kube.CoreV1().Pods(workload.Namespace).Get(context.Background(), assignment.PodName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.Spec.NodeName = assignment.NodeName
	if _, err := controllerUnderTest.Kube.CoreV1().Pods(workload.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controllerUnderTest.EnsurePod(context.Background(), workload, 0, assignment); err != nil {
		t.Fatalf("scheduled controller child was rejected: %v", err)
	}
}

// TestDeleteChildrenIsIdempotentAndReportsAPIErrors verifies one delete run is enough and failures surface with intent.
func TestDeleteChildrenIsIdempotentAndReportsAPIErrors(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{ClaimName: "claim", PodName: "pod"}
	workload.Status.Assignments = []ttapi.RankAssignment{assignment}
	controllerUnderTest := &controller.Controller{Kube: fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: assignment.PodName, Namespace: workload.Namespace}},
		controller.BuildClaim(workload, workload.Spec.Ranks[0], assignment),
	)}
	if err := controllerUnderTest.DeleteChildren(context.Background(), workload); err != nil {
		t.Fatalf("delete children: %v", err)
	}
	if err := controllerUnderTest.DeleteChildren(context.Background(), workload); err != nil {
		t.Fatalf("idempotent delete children: %v", err)
	}

	podFailure := fake.NewSimpleClientset()
	podFailure.Fake.PrependReactor("delete", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pod API unavailable")
	})
	controllerUnderTest.Kube = podFailure
	if err := controllerUnderTest.DeleteChildren(context.Background(), workload); err == nil || !strings.Contains(err.Error(), "delete Pod") {
		t.Fatalf("Pod deletion error = %v", err)
	}

	claimFailure := fake.NewSimpleClientset()
	claimFailure.Fake.PrependReactor("delete", "resourceclaims", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("claim API unavailable")
	})
	controllerUnderTest.Kube = claimFailure
	if err := controllerUnderTest.DeleteChildren(context.Background(), workload); err == nil || !strings.Contains(err.Error(), "delete ResourceClaim") {
		t.Fatalf("ResourceClaim deletion error = %v", err)
	}
}

// TestBuildPodAppliesRestrictedSecurity verifies every generated rank Pod is hardened.
func TestBuildPodAppliesRestrictedSecurity(t *testing.T) {
	workload := safeWorkload()
	assignment := ttapi.RankAssignment{NodeName: "node-a", ClaimName: "claim", PodName: "pod"}
	pod, err := controller.BuildPod(workload, 0, assignment, false)
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
	validationPod, err := controller.BuildPod(workload, 0, assignment, true)
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
			if err := controller.ValidateWorkload(workload); err == nil {
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
	controller.SetWorkloadStatus(workload, "Pending", false, "Pending", "still waiting")
	if !workload.Status.Conditions[0].LastTransitionTime.Equal(&old) || workload.Status.Conditions[0].ObservedGeneration != 7 {
		t.Fatalf("stable transition metadata changed: %#v", workload.Status.Conditions[0])
	}
	controller.SetWorkloadStatus(workload, "Failed", false, "Invalid", "failed")
	if workload.Status.Conditions[0].LastTransitionTime.Equal(&old) {
		t.Fatal("changed condition retained its old transition time")
	}
}
