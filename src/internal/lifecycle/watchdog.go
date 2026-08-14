package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// FenceStaleAgents removes schedulable capacity for node agents whose custom
// condition heartbeat has expired. Nodes without that condition were never
// managed by this driver and are ignored.
func FenceStaleAgents(ctx context.Context, kube kubernetes.Interface, dynamicClient dynamic.Interface, driver string, ttl time.Duration, now time.Time) (int, error) {
	if ttl <= 0 {
		return 0, errors.New("node-agent TTL must be positive")
	}
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("list nodes for node-agent watchdog: %w", err)
	}
	slices, err := kube.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("list ResourceSlices for node-agent watchdog: %w", err)
	}
	fenced := 0
	var resultErr error
	for index := range nodes.Items {
		node := &nodes.Items[index]
		condition, managed := acceleratorCondition(node.Status.Conditions)
		if !managed || condition.LastHeartbeatTime.IsZero() || now.Sub(condition.LastHeartbeatTime.Time) <= ttl {
			continue
		}
		fenced++
		resultErr = errors.Join(resultErr, UpdateNodeSafety(ctx, kube, node.Name, Safety{
			Unsafe: true, Reason: "AgentUnavailable", Message: fmt.Sprintf("Tenstorrent DRA node-agent heartbeat is older than %s", ttl),
		}))
		for sliceIndex := range slices.Items {
			slice := &slices.Items[sliceIndex]
			if slice.Spec.Driver != driver || slice.Spec.NodeName == nil || *slice.Spec.NodeName != node.Name {
				continue
			}
			resultErr = errors.Join(resultErr, kube.ResourceV1().ResourceSlices().Delete(ctx, slice.Name, metav1.DeleteOptions{}))
		}
		if dynamicClient != nil {
			err := dynamicClient.Resource(ttapi.NodeTopologyGVR).Delete(ctx, node.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}
	return fenced, resultErr
}

func acceleratorCondition(conditions []corev1.NodeCondition) (corev1.NodeCondition, bool) {
	for _, condition := range conditions {
		if condition.Type == NodeConditionType {
			return condition, true
		}
	}
	return corev1.NodeCondition{}, false
}
