package controller

import (
	"context"
	"fmt"
	"testing"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
)

// TestControllerFreezesNewWorkAboveScaleLimit verifies overflow cannot trigger placement.
func TestControllerFreezesNewWorkAboveScaleLimit(t *testing.T) {
	workload := safeWorkload()
	object, err := ttapi.ToUnstructured(workload)
	if err != nil {
		t.Fatal(err)
	}
	object.SetAPIVersion(ttapi.SchedulingAPIVersion)
	object.SetKind(ttapi.WorkloadKind)
	informer := cache.NewSharedIndexInformer(nil, &unstructured.Unstructured{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetStore().Add(object); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxWorkloads; index++ {
		item := object.DeepCopy()
		item.SetName(fmt.Sprintf("job-%04d", index))
		if err := informer.GetStore().Add(item); err != nil {
			t.Fatal(err)
		}
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{ttapi.WorkloadGVR: "TenstorrentWorkloadList"}, object,
	)
	controller := &Controller{Dynamic: client, workloadInformer: informer}
	if err := controller.reconcileWorkloadKey(context.Background(), "default/job"); err != nil {
		t.Fatal(err)
	}
	current, err := client.Resource(ttapi.WorkloadGVR).Namespace("default").Get(context.Background(), "job", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var observed ttapi.Workload
	if err := ttapi.FromUnstructured(current, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Status.Phase != "Pending" || observed.Status.Conditions[0].Reason != "ClusterScaleExceeded" {
		t.Fatalf("unexpected overflow status: %#v", observed.Status)
	}
}

// TestDeletedWorkloadReleasesPendingReservations verifies deletion cleanup stays on the queue worker.
func TestDeletedWorkloadReleasesPendingReservations(t *testing.T) {
	informer := cache.NewSharedIndexInformer(nil, &unstructured.Unstructured{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	controller := &Controller{
		workloadInformer: informer,
		pending: map[string][]ttapi.RankAssignment{
			"default/deleted": {{Rank: "rank"}},
		},
	}
	if err := controller.reconcileWorkloadKey(context.Background(), "default/deleted"); err != nil {
		t.Fatal(err)
	}
	if len(controller.pending) != 0 {
		t.Fatalf("deleted workload retained reservations: %#v", controller.pending)
	}
}
