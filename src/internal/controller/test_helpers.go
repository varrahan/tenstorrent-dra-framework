package controller

import (
	"context"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/client-go/tools/cache"
)

// BuildClaim exposes child claim construction for src/test white-box coverage.
func BuildClaim(workload *ttapi.Workload, rank ttapi.WorkloadRank, assignment ttapi.RankAssignment) *resourceapi.ResourceClaim {
	return buildClaim(workload, rank, assignment)
}

// BuildPod exposes child Pod construction for src/test white-box coverage.
func BuildPod(workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment, disableAppArmor bool) (*corev1.Pod, error) {
	return buildPod(workload, rankIndex, assignment, disableAppArmor)
}

// EnsureChildren mirrors ensureChildren for src/test white-box validation.
func (c *Controller) EnsureChildren(ctx context.Context, workload *ttapi.Workload) error {
	return c.ensureChildren(ctx, workload)
}

// EnsureClaim mirrors ensureClaim for src/test white-box validation.
func (c *Controller) EnsureClaim(ctx context.Context, workload *ttapi.Workload, rank ttapi.WorkloadRank, assignment ttapi.RankAssignment) error {
	return c.ensureClaim(ctx, workload, rank, assignment)
}

// EnsurePod mirrors ensurePod for src/test white-box validation.
func (c *Controller) EnsurePod(ctx context.Context, workload *ttapi.Workload, rankIndex int, assignment ttapi.RankAssignment) error {
	return c.ensurePod(ctx, workload, rankIndex, assignment)
}

// DeleteChildren mirrors deleteChildren for src/test white-box validation.
func (c *Controller) DeleteChildren(ctx context.Context, workload *ttapi.Workload) error {
	return c.deleteChildren(ctx, workload)
}

// ReconcileWorkloadKey mirrors reconcileWorkloadKey for src/test white-box validation.
func (c *Controller) ReconcileWorkloadKey(ctx context.Context, key string) error {
	return c.reconcileWorkloadKey(ctx, key)
}

// ValidateWorkload mirrors validateWorkload for src/test white-box validation.
func ValidateWorkload(workload *ttapi.Workload) error {
	return validateWorkload(workload)
}

// SetWorkloadStatus mirrors setWorkloadStatus for src/test white-box validation.
func SetWorkloadStatus(workload *ttapi.Workload, phase string, ready bool, reason, message string) {
	setWorkloadStatus(workload, phase, ready, reason, message)
}

// Pending exposes currently pending assignments for src/test assertions.
func (c *Controller) Pending() map[string][]ttapi.RankAssignment {
	return c.pending
}

// SetWorkloadInformer sets workload informer state for test setup.
func (c *Controller) SetWorkloadInformer(informer cache.SharedIndexInformer) {
	c.workloadInformer = informer
}

// SetPending sets pending assignments map for test setup.
func (c *Controller) SetPending(assignments map[string][]ttapi.RankAssignment) {
	c.pending = assignments
}
