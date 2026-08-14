package lifecycle

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	NodeConditionType = corev1.NodeConditionType("TenstorrentAcceleratorsHealthy")
	NodeTaintKey      = "tenstorrent.com/accelerator-unhealthy"
)

// UpdateNodeSafety reconciles the accelerator safety taint and health condition on a node.
func UpdateNodeSafety(ctx context.Context, client kubernetes.Interface, nodeName string, safety Safety) error {
	nodes := client.CoreV1().Nodes()
	node, err := nodes.Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if setSafetyTaint(node, safety.Unsafe) {
		node, err = nodes.Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	setSafetyCondition(node, safety)
	_, err = nodes.UpdateStatus(ctx, node, metav1.UpdateOptions{})
	return err
}

// ClearNodeSafety removes only this driver's taint and condition during a
// guarded uninstall after all allocations have been released.
func ClearNodeSafety(ctx context.Context, client kubernetes.Interface, nodeName string) error {
	nodes := client.CoreV1().Nodes()
	node, err := nodes.Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	taints := node.Spec.Taints[:0]
	for _, taint := range node.Spec.Taints {
		if taint.Key != NodeTaintKey {
			taints = append(taints, taint)
		}
	}
	specChanged := len(taints) != len(node.Spec.Taints)
	node.Spec.Taints = taints
	if specChanged {
		node, err = nodes.Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}
	conditions := node.Status.Conditions[:0]
	for _, condition := range node.Status.Conditions {
		if condition.Type != NodeConditionType {
			conditions = append(conditions, condition)
		}
	}
	if len(conditions) == len(node.Status.Conditions) {
		return nil
	}
	node.Status.Conditions = conditions
	_, err = nodes.UpdateStatus(ctx, node, metav1.UpdateOptions{})
	return err
}

// setSafetyTaint adds or removes the NoSchedule taint and reports whether the node changed.
func setSafetyTaint(node *corev1.Node, unsafe bool) bool {
	for index, taint := range node.Spec.Taints {
		if taint.Key != NodeTaintKey {
			continue
		}
		if unsafe {
			return false
		}
		node.Spec.Taints = append(node.Spec.Taints[:index], node.Spec.Taints[index+1:]...)
		return true
	}
	if !unsafe {
		return false
	}
	node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{Key: NodeTaintKey, Value: "true", Effect: corev1.TaintEffectNoSchedule})
	return true
}

// setSafetyCondition upserts accelerator health while preserving its last transition time.
func setSafetyCondition(node *corev1.Node, safety Safety) {
	now := metav1.NewTime(time.Now().UTC())
	status := corev1.ConditionTrue
	if safety.Unsafe {
		status = corev1.ConditionFalse
	}
	condition := corev1.NodeCondition{
		Type: NodeConditionType, Status: status, Reason: safety.Reason,
		Message: safety.Message, LastHeartbeatTime: now, LastTransitionTime: now,
	}
	for index := range node.Status.Conditions {
		if node.Status.Conditions[index].Type != NodeConditionType {
			continue
		}
		if node.Status.Conditions[index].Status == status {
			condition.LastTransitionTime = node.Status.Conditions[index].LastTransitionTime
		}
		node.Status.Conditions[index] = condition
		return
	}
	node.Status.Conditions = append(node.Status.Conditions, condition)
}
