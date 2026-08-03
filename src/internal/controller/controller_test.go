package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/placement"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestBuildClaimPreservesExactAllocationContract(t *testing.T) {
	workload := testWorkload("job")
	assignment := ttapi.RankAssignment{
		NodeName:  "worker-a",
		ClaimName: "job-rank-0",
		Devices: []ttapi.AssignedDevice{
			{StableID: "pci-0000:01:00.0"},
			{StableID: "pci-0000:02:00.0"},
		},
	}
	claim := buildClaim(workload, workload.Spec.Ranks[0], assignment)
	request := claim.Spec.Devices.Requests[0].Exactly
	if request.DeviceClassName != "tenstorrent" || request.Count != 2 || request.AllocationMode != "ExactCount" {
		t.Fatalf("unexpected exact request: %#v", request)
	}
	expression := request.Selectors[0].CEL.Expression
	for _, want := range []string{"worker-a", "pci-0000:01:00.0", "pci-0000:02:00.0"} {
		if !strings.Contains(expression, want) {
			t.Fatalf("selector %q does not contain %q", expression, want)
		}
	}
}

func TestBuildPodInjectsClaimAndRankWithoutMutatingTemplate(t *testing.T) {
	workload := testWorkload("job")
	workload.Spec.PodTemplate.Labels = map[string]string{"existing": "label"}
	assignment := ttapi.RankAssignment{NodeName: "worker-a", PodName: "job-rank-0", ClaimName: "job-rank-0"}
	pod, err := buildPod(workload, 0, assignment)
	if err != nil {
		t.Fatal(err)
	}
	if pod.Spec.NodeSelector[corev1.LabelHostname] != "worker-a" || pod.Labels["existing"] != "label" {
		t.Fatalf("Pod metadata was not preserved: %#v", pod)
	}
	if len(pod.Spec.ResourceClaims) != 1 || len(pod.Spec.Containers[0].Resources.Claims) != 1 {
		t.Fatalf("resource claim was not attached: %#v", pod.Spec)
	}
	if got := pod.Spec.Containers[0].Env; len(got) != 2 || got[0].Name != "TT_RANK" || got[0].Value != "0" || got[1].Name != "TT_WORLD_SIZE" || got[1].Value != "1" {
		t.Fatalf("rank environment = %#v", got)
	}
	if workload.Spec.PodTemplate.Spec.NodeSelector != nil || workload.Spec.PodTemplate.Labels["tenstorrent.com/workload"] != "" {
		t.Fatal("Pod template was mutated")
	}
}

func TestEnsureChildrenRejectsMismatchedStatus(t *testing.T) {
	workload := testWorkload("job")
	workload.Status.Assignments = []ttapi.RankAssignment{{}, {}}
	if err := (&Controller{}).ensureChildren(context.Background(), workload); err == nil {
		t.Fatal("mismatched rank assignments were accepted")
	}
}

func TestReconcileWorkloadsReservesAssignmentWithinPass(t *testing.T) {
	first := testWorkload("first")
	second := testWorkload("second")
	objects := make([]runtime.Object, 0, 2)
	for _, workload := range []*ttapi.Workload{first, second} {
		object, err := ttapi.ToUnstructured(workload)
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, object)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{ttapi.WorkloadGVR: "TenstorrentWorkloadList"},
		objects...,
	)
	controller := &Controller{Kube: kubernetesfake.NewSimpleClientset(), Dynamic: dynamicClient}
	fabric := ttapi.FabricTopologyStatus{Valid: true, Generation: "generation-a", Endpoints: []ttapi.FabricEndpoint{{
		NodeName: "worker-a", Pool: "worker-a", DeviceName: "device-a", StableID: "pci-a",
		ChipSeries: "quasar", CardSeries: "q950x", FabricID: "fabric-a", RingID: "ring-a", EndpointID: "endpoint-a",
	}}}
	if err := controller.reconcileWorkloads(context.Background(), fabric); err != nil {
		t.Fatal(err)
	}

	phaseCounts := map[string]int{}
	for _, name := range []string{"first", "second"} {
		object, err := dynamicClient.Resource(ttapi.WorkloadGVR).Namespace("default").Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var workload ttapi.Workload
		if err := ttapi.FromUnstructured(object, &workload); err != nil {
			t.Fatal(err)
		}
		phaseCounts[workload.Status.Phase]++
	}
	if phaseCounts["Assigned"] != 1 || phaseCounts["Pending"] != 1 {
		t.Fatalf("workload phases = %v, want one Assigned and one Pending", phaseCounts)
	}
}

func TestStartedAssignmentDegradesOnFabricChange(t *testing.T) {
	workload := testWorkload("job")
	workload.Status = ttapi.WorkloadStatus{
		Phase: "Assigned", FabricGeneration: "old",
		Assignments: []ttapi.RankAssignment{{
			Rank: "rank-0", NodeName: "worker-a", ClaimName: "job-rank-0", PodName: "job-rank-0",
			Devices: []ttapi.AssignedDevice{{Pool: "worker-a", Name: "device-a", StableID: "pci-a", EndpointID: "endpoint-a"}},
		}},
	}
	object, err := ttapi.ToUnstructured(workload)
	if err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), object)
	now := metav1.Now()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "job-rank-0", Namespace: "default"}, Status: corev1.PodStatus{StartTime: &now}}
	controller := &Controller{Kube: kubernetesfake.NewSimpleClientset(pod), Dynamic: dynamicClient}

	if err := controller.reconcileWorkload(context.Background(), workload, ttapi.FabricTopologyStatus{Generation: "new"}, placement.Reservations{}); err != nil {
		t.Fatal(err)
	}
	if workload.Status.Phase != "Degraded" || workload.Status.FabricGeneration != "old" || len(workload.Status.Assignments) != 1 {
		t.Fatalf("started assignment was not frozen and degraded: %#v", workload.Status)
	}
	if got := workload.Status.Conditions[0].Reason; got != "FabricChanged" {
		t.Fatalf("condition reason = %q, want FabricChanged", got)
	}
}

func TestAnyRankStartedPropagatesAPIError(t *testing.T) {
	kube := kubernetesfake.NewSimpleClientset()
	kube.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API unavailable")
	})
	workload := testWorkload("job")
	workload.Status.Assignments = []ttapi.RankAssignment{{PodName: "job-rank-0"}}
	_, err := (&Controller{Kube: kube}).anyRankStarted(context.Background(), workload)
	if err == nil || !strings.Contains(err.Error(), "API unavailable") {
		t.Fatalf("anyRankStarted error = %v", err)
	}
}

func testWorkload(name string) *ttapi.Workload {
	return &ttapi.Workload{
		TypeMeta: metav1.TypeMeta{APIVersion: ttapi.SchedulingAPIVersion, Kind: ttapi.WorkloadKind},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(name + "-uid"),
		},
		Spec: ttapi.WorkloadSpec{
			ContainerName: "worker",
			PodTemplate: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers:    []corev1.Container{{Name: "worker", Image: "example.invalid/worker"}},
				RestartPolicy: corev1.RestartPolicyNever,
			}},
			Ranks: []ttapi.WorkloadRank{{Name: "rank-0", DeviceClassName: dra.GenericDeviceClassName, Count: 1}},
		},
	}
}
