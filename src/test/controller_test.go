package test

import (
	"context"
	"strings"
	"testing"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/controller"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

// TestControllerEmitsTopologyAndWorkloadEvents verifies scheduling decisions
// are visible without parsing controller logs.
func TestControllerEmitsTopologyAndWorkloadEvents(t *testing.T) {
	dynamicClient := controllerDynamicClient(t, controllerNodeTopology(), controllerWorkload("events"))
	kube := kubernetesfake.NewSimpleClientset()
	recorder := record.NewFakeRecorder(20)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (&controller.Controller{Kube: kube, Dynamic: dynamicClient, TopologyTTL: time.Minute, Recorder: recorder}).Run(ctx)
	}()
	waitFor(t, func() bool { return workloadPhase(t, dynamicClient, "events") == "Assigned" })

	want := map[string]bool{"TopologyValidated": false, "Assigned": false}
	deadline := time.After(2 * time.Second)
	for !(want["TopologyValidated"] && want["Assigned"]) {
		select {
		case event := <-recorder.Events:
			for reason := range want {
				if strings.Contains(event, reason) {
					want[reason] = true
				}
			}
		case <-deadline:
			cancel()
			t.Fatalf("events observed = %v", want)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestControllerCreatesExactChildrenAndReservesWithinPass verifies distinct claims and Pods are created for concurrent workloads.
func TestControllerCreatesExactChildrenAndReservesWithinPass(t *testing.T) {
	dynamicClient := controllerDynamicClient(t, controllerNodeTopology(), controllerWorkload("first"), controllerWorkload("second"))
	kube := kubernetesfake.NewSimpleClientset()
	cancel, done := runController(kube, dynamicClient)
	defer cancel()

	waitFor(t, func() bool {
		first := workloadPhase(t, dynamicClient, "first")
		second := workloadPhase(t, dynamicClient, "second")
		return (first == "Assigned" && second == "Pending") || (first == "Pending" && second == "Assigned")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	phases := map[string]int{}
	for _, name := range []string{"first", "second"} {
		phases[workloadPhase(t, dynamicClient, name)]++
	}
	if phases["Assigned"] != 1 || phases["Pending"] != 1 {
		t.Fatalf("workload phases = %v, want one Assigned and one Pending", phases)
	}
	claims, err := kube.ResourceV1().ResourceClaims("default").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(claims.Items) != 1 {
		t.Fatalf("claims = %#v, err=%v", claims, err)
	}
	request := claims.Items[0].Spec.Devices.Requests[0].Exactly
	if request == nil || request.DeviceClassName != dra.GenericDeviceClassName || request.Count != 1 || len(request.Selectors) != 1 {
		t.Fatalf("exact claim request = %#v", request)
	}
	pods, err := kube.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(pods.Items) != 1 {
		t.Fatalf("Pods = %#v, err=%v", pods, err)
	}
	pod := pods.Items[0]
	if pod.Spec.NodeSelector[corev1.LabelHostname] != "worker-a" || len(pod.Spec.ResourceClaims) != 1 {
		t.Fatalf("Pod allocation wiring = %#v", pod.Spec)
	}
	if env := pod.Spec.Containers[0].Env; len(env) != 2 || env[0].Name != "TT_RANK" || env[1].Name != "TT_WORLD_SIZE" {
		t.Fatalf("rank environment = %#v", env)
	}
}

// TestControllerFreezesStartedAssignmentAfterFabricChange verifies running ranks are not moved after topology changes.
func TestControllerFreezesStartedAssignmentAfterFabricChange(t *testing.T) {
	workload := controllerWorkload("job")
	workload.Status = ttapi.WorkloadStatus{
		Phase: "Assigned", FabricGeneration: "old",
		Assignments: []ttapi.RankAssignment{{
			Rank: "rank-0", NodeName: "worker-a", ClaimName: "job-rank-0", PodName: "job-rank-0",
			Devices: []ttapi.AssignedDevice{{Pool: "worker-a", Name: "device-a", StableID: "pci-a", EndpointID: "endpoint-a"}},
		}},
	}
	dynamicClient := controllerDynamicClient(t, controllerNodeTopology(), workload)
	now := metav1.Now()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "job-rank-0", Namespace: "default"}, Status: corev1.PodStatus{StartTime: &now}}
	kube := kubernetesfake.NewSimpleClientset(pod)
	cancel, done := runController(kube, dynamicClient)
	defer cancel()

	waitFor(t, func() bool { return workloadPhase(t, dynamicClient, "job") == "Degraded" })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	object, err := dynamicClient.Resource(ttapi.WorkloadGVR).Namespace("default").Get(context.Background(), "job", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var observed ttapi.Workload
	if err := ttapi.FromUnstructured(object, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Status.FabricGeneration != "old" || len(observed.Status.Assignments) != 1 || observed.Status.Conditions[0].Reason != "FabricChanged" {
		t.Fatalf("started assignment was not frozen: %#v", observed.Status)
	}
}

// TestControllerTracksRunningAndSucceededCleanup verifies terminal Pods release child resources.
func TestControllerTracksRunningAndSucceededCleanup(t *testing.T) {
	dynamicClient := controllerDynamicClient(t, controllerNodeTopology(), controllerWorkload("lifecycle"))
	kube := kubernetesfake.NewSimpleClientset()
	cancel, done := runController(kube, dynamicClient)
	defer cancel()
	waitFor(t, func() bool { return workloadPhase(t, dynamicClient, "lifecycle") == "Assigned" })
	pods, err := kube.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if err != nil || len(pods.Items) != 1 {
		t.Fatalf("rank Pod was not created: %#v %v", pods, err)
	}
	pod := pods.Items[0].DeepCopy()
	now := metav1.Now()
	pod.Status.Phase, pod.Status.StartTime = corev1.PodRunning, &now
	if _, err := kube.CoreV1().Pods("default").UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return workloadPhase(t, dynamicClient, "lifecycle") == "Running" })
	pod, err = kube.CoreV1().Pods("default").Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.Status.Phase = corev1.PodSucceeded
	if _, err := kube.CoreV1().Pods("default").UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return workloadPhase(t, dynamicClient, "lifecycle") == "Succeeded" })
	waitFor(t, func() bool {
		pods, _ := kube.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
		claims, _ := kube.ResourceV1().ResourceClaims("default").List(context.Background(), metav1.ListOptions{})
		return len(pods.Items) == 0 && len(claims.Items) == 0
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestControllerRejectsInvalidWorkloadWithoutChildren verifies unsafe templates fail closed.
func TestControllerRejectsInvalidWorkloadWithoutChildren(t *testing.T) {
	workload := controllerWorkload("invalid")
	workload.Spec.ContainerName = "missing"
	dynamicClient := controllerDynamicClient(t, controllerNodeTopology(), workload)
	kube := kubernetesfake.NewSimpleClientset()
	cancel, done := runController(kube, dynamicClient)
	defer cancel()
	waitFor(t, func() bool { return workloadPhase(t, dynamicClient, "invalid") == "Failed" })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	pods, _ := kube.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	claims, _ := kube.ResourceV1().ResourceClaims("default").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 0 || len(claims.Items) != 0 {
		t.Fatalf("invalid workload created children: pods=%d claims=%d", len(pods.Items), len(claims.Items))
	}
}

// controllerDynamicClient builds a fake dynamic client with the project's custom resource list kinds.
func controllerDynamicClient(t *testing.T, objects ...any) *dynamicfake.FakeDynamicClient {
	t.Helper()
	runtimeObjects := make([]runtime.Object, 0, len(objects))
	for _, value := range objects {
		object, err := ttapi.ToUnstructured(value)
		if err != nil {
			t.Fatal(err)
		}
		runtimeObjects = append(runtimeObjects, object)
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			ttapi.NodeTopologyGVR: "TenstorrentNodeTopologyList",
			ttapi.WorkloadGVR:     "TenstorrentWorkloadList",
		},
		runtimeObjects...,
	)
}

// runController starts a fast test reconciliation loop and returns cancellation and completion handles.
func runController(kube *kubernetesfake.Clientset, dynamicClient *dynamicfake.FakeDynamicClient) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- (&controller.Controller{Kube: kube, Dynamic: dynamicClient, TopologyTTL: time.Minute}).Run(ctx)
	}()
	return cancel, done
}

// workloadPhase reads and decodes the current phase of a fake workload resource.
func workloadPhase(t *testing.T, client *dynamicfake.FakeDynamicClient, name string) string {
	t.Helper()
	object, err := client.Resource(ttapi.WorkloadGVR).Namespace("default").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var workload ttapi.Workload
	if err := ttapi.FromUnstructured(object, &workload); err != nil {
		t.Fatal(err)
	}
	return workload.Status.Phase
}

// waitFor polls a test condition until it succeeds or the short deadline expires.
func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for controller reconciliation")
}

// controllerNodeTopology returns the connected two-device fabric used by controller tests.
func controllerNodeTopology() *ttapi.NodeTopology {
	return &ttapi.NodeTopology{
		TypeMeta:   metav1.TypeMeta{APIVersion: ttapi.TopologyAPIVersion, Kind: ttapi.NodeTopologyKind},
		ObjectMeta: metav1.ObjectMeta{Name: "worker-a"},
		Spec: ttapi.NodeTopologySpec{
			NodeName: "worker-a", ObservedAt: metav1.Now(),
			Devices: []ttapi.TopologyDevice{{
				Pool: "worker-a", Name: "device-a", StableID: "pci-a", ChipSeries: "wormhole",
				FabricID: "fabric-a", RingID: "ring-a", EndpointID: "endpoint-a",
			}},
		},
	}
}

// controllerWorkload returns a minimal single-rank workload with stable test metadata.
func controllerWorkload(name string) *ttapi.Workload {
	return &ttapi.Workload{
		TypeMeta: metav1.TypeMeta{APIVersion: ttapi.SchedulingAPIVersion, Kind: ttapi.WorkloadKind},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "default", UID: types.UID(name + "-uid"),
		},
		Spec: ttapi.WorkloadSpec{
			ContainerName: "worker",
			PodTemplate: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "worker", Image: "example.invalid/worker"}}, RestartPolicy: corev1.RestartPolicyNever,
			}},
			Ranks: []ttapi.WorkloadRank{{Name: "rank-0", DeviceClassName: dra.GenericDeviceClassName, Count: 1}},
		},
	}
}
