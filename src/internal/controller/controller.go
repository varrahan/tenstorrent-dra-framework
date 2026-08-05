package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/observability"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/placement"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/topology"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
)

const fabricQueueKey = "!fabric"

const MaxWorkloads = 1000

type Controller struct {
	Kube                    kubernetes.Interface
	Dynamic                 dynamic.Interface
	TopologyTTL             time.Duration
	PlacementTimeout        time.Duration
	DisableWorkloadAppArmor bool
	Metrics                 *observability.Metrics
	Logger                  *slog.Logger
	Recorder                record.EventRecorder
	SetReady                func(bool)

	queue            workqueue.TypedRateLimitingInterface[string]
	nodeInformer     cache.SharedIndexInformer
	workloadInformer cache.SharedIndexInformer
	claimInformer    cache.SharedIndexInformer
	podInformer      cache.SharedIndexInformer
	fabric           ttapi.FabricTopologyStatus
	pending          map[string][]ttapi.RankAssignment
}

// Run processes informer events through a per-object rate-limited work queue.
func (c *Controller) Run(ctx context.Context) error {
	if c.Kube == nil || c.Dynamic == nil {
		return fmt.Errorf("Kubernetes clients are required")
	}
	if c.PlacementTimeout <= 0 {
		c.PlacementTimeout = 2 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default().With("component", "controller")
	}
	c.queue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	c.pending = map[string][]ttapi.RankAssignment{}
	dynamicFactory := dynamicinformer.NewDynamicSharedInformerFactory(c.Dynamic, 0)
	kubeFactory := informers.NewSharedInformerFactory(c.Kube, 0)
	c.nodeInformer = dynamicFactory.ForResource(ttapi.NodeTopologyGVR).Informer()
	c.workloadInformer = dynamicFactory.ForResource(ttapi.WorkloadGVR).Informer()
	c.claimInformer = kubeFactory.Resource().V1().ResourceClaims().Informer()
	c.podInformer = kubeFactory.Core().V1().Pods().Informer()
	if err := c.registerHandlers(); err != nil {
		return err
	}
	dynamicFactory.Start(ctx.Done())
	kubeFactory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), c.nodeInformer.HasSynced, c.workloadInformer.HasSynced, c.claimInformer.HasSynced, c.podInformer.HasSynced) {
		return fmt.Errorf("controller informer cache did not sync")
	}
	if c.SetReady != nil {
		c.SetReady(true)
		defer c.SetReady(false)
	}
	c.queue.Add(fabricQueueKey)
	c.enqueueAllWorkloads()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for c.processNext(ctx) {
		}
	}()
	<-ctx.Done()
	c.queue.ShutDown()
	<-done
	return nil
}

// registerHandlers maps topology, workload, claim, and Pod changes to queue keys.
func (c *Controller) registerHandlers() error {
	fabricHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { c.queue.Add(fabricQueueKey) },
		UpdateFunc: func(any, any) { c.queue.Add(fabricQueueKey) },
		DeleteFunc: func(any) { c.queue.Add(fabricQueueKey) },
	}
	if _, err := c.nodeInformer.AddEventHandler(fabricHandler); err != nil {
		return err
	}
	workloadHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueWorkload,
		UpdateFunc: func(_ any, current any) { c.enqueueWorkload(current) },
		DeleteFunc: c.enqueueWorkload,
	}
	if _, err := c.workloadInformer.AddEventHandler(workloadHandler); err != nil {
		return err
	}
	childHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueChild,
		UpdateFunc: func(_ any, current any) { c.enqueueChild(current) },
		DeleteFunc: c.enqueueChild,
	}
	if _, err := c.claimInformer.AddEventHandler(childHandler); err != nil {
		return err
	}
	_, err := c.podInformer.AddEventHandler(childHandler)
	return err
}

// enqueueWorkload adds one namespaced workload key, including delete tombstones.
func (c *Controller) enqueueWorkload(object any) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(object)
	if err == nil {
		c.queue.Add(key)
	}
}

// enqueueChild adds the owning workload key carried on a controller-created child.
func (c *Controller) enqueueChild(object any) {
	if tombstone, ok := object.(cache.DeletedFinalStateUnknown); ok {
		object = tombstone.Obj
	}
	metadata, err := meta.Accessor(object)
	if err != nil {
		return
	}
	name := metadata.GetLabels()["tenstorrent.com/workload-name"]
	if name != "" {
		c.queue.Add(metadata.GetNamespace() + "/" + name)
	}
}

// enqueueAllWorkloads schedules every cached workload after fabric changes or startup.
func (c *Controller) enqueueAllWorkloads() {
	for _, object := range c.workloadInformer.GetStore().List() {
		c.enqueueWorkload(object)
	}
}

// processNext reconciles one key and applies exponential retry only to that key.
func (c *Controller) processNext(ctx context.Context) bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)
	started := time.Now()
	kind := "workload"
	if key == fabricQueueKey {
		kind = "fabric"
	}
	reconciliationID := fmt.Sprintf("%s-%d", strings.TrimPrefix(key, "!"), started.UnixNano())
	var err error
	if key == fabricQueueKey {
		c.fabric, err = c.reconcileFabric(ctx)
		if err == nil {
			c.enqueueAllWorkloads()
			if c.TopologyTTL > 0 {
				c.queue.AddAfter(fabricQueueKey, c.TopologyTTL/2)
			}
		}
	} else {
		err = c.reconcileWorkloadKey(ctx, key)
	}
	duration := time.Since(started)
	if c.Metrics != nil {
		c.Metrics.ObserveReconcile("controller", kind, duration, err)
	}
	log := c.logger().With(
		"reconciliation_id", reconciliationID,
		"reconciliation_kind", kind,
		"key", key,
		"duration_seconds", duration.Seconds(),
	)
	if key != fabricQueueKey {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			log = log.With("workload_namespace", parts[0], "workload", parts[1])
		}
	}
	if err != nil {
		log.Error("reconciliation failed", "error", err)
		c.queue.AddRateLimited(key)
		return true
	}
	log.Info("reconciliation completed")
	c.queue.Forget(key)
	return true
}

// reconcileFabric aggregates cached node observations and publishes cluster fabric status.
func (c *Controller) reconcileFabric(ctx context.Context) (ttapi.FabricTopologyStatus, error) {
	objects := c.nodeInformer.GetStore().List()
	nodes := make([]ttapi.NodeTopology, 0, len(objects))
	for _, object := range objects {
		var node ttapi.NodeTopology
		if err := ttapi.FromUnstructured(object.(*unstructured.Unstructured), &node); err != nil {
			return ttapi.FabricTopologyStatus{}, err
		}
		nodes = append(nodes, node)
	}
	status := topology.BuildFabric(nodes, c.TopologyTTL, time.Now().UTC())
	if c.Metrics != nil {
		c.Metrics.ObserveTopology(status.Valid, len(status.Errors))
	}
	resource := c.Dynamic.Resource(ttapi.FabricTopologyGVR)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := resource.Get(ctx, "cluster", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			object := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": ttapi.TopologyAPIVersion,
				"kind":       ttapi.FabricTopologyKind,
				"metadata":   map[string]any{"name": "cluster"},
			}}
			current, err = resource.Create(ctx, object, metav1.CreateOptions{})
		}
		if err != nil {
			return err
		}
		encoded, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&status)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(current.Object["status"], encoded) {
			return nil
		}
		current.Object["status"] = encoded
		_, err = resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
		return err
	})
	if err == nil && (c.fabric.Generation != status.Generation || c.fabric.Valid != status.Valid || !reflect.DeepEqual(c.fabric.Errors, status.Errors)) {
		eventType, reason, message := corev1.EventTypeNormal, "TopologyValidated", "cluster fabric topology is valid"
		if !status.Valid {
			eventType, reason = corev1.EventTypeWarning, "TopologyInvalid"
			message = strings.Join(status.Errors, "; ")
		}
		c.emitFabricEvent(eventType, reason, message)
		c.logger().Info("topology decision",
			"topology_valid", status.Valid,
			"topology_generation", status.Generation,
			"topology_errors", len(status.Errors),
		)
	}
	return status, err
}

// reconcileWorkloadKey decodes one cached workload and reconciles it in isolation.
func (c *Controller) reconcileWorkloadKey(ctx context.Context, key string) error {
	object, found, err := c.workloadInformer.GetStore().GetByKey(key)
	if err != nil {
		return err
	}
	if !found {
		delete(c.pending, key)
		return nil
	}
	var workload ttapi.Workload
	if err := ttapi.FromUnstructured(object.(*unstructured.Unstructured), &workload); err != nil {
		return err
	}
	if len(c.workloadInformer.GetStore().List()) > MaxWorkloads {
		phase := "Pending"
		if len(workload.Status.Assignments) > 0 {
			phase = "Degraded"
		}
		setWorkloadStatus(&workload, phase, false, "ClusterScaleExceeded", fmt.Sprintf("active workload count exceeds %d", MaxWorkloads))
		return c.updateWorkloadStatus(ctx, &workload)
	}
	return c.reconcileWorkload(ctx, &workload, c.fabric, c.reservations(key))
}

// reservations returns devices consumed by claims and other active workload assignments.
func (c *Controller) reservations(exclude string) placement.Reservations {
	used := placement.Reservations{}
	for key, assignments := range c.pending {
		if key != exclude {
			used.AddAssignments(assignments)
		}
	}
	for _, object := range c.claimInformer.GetStore().List() {
		claim := object.(*resourceapi.ResourceClaim)
		if claim.Namespace+"/"+claim.Labels["tenstorrent.com/workload-name"] == exclude {
			continue
		}
		if claim.Status.Allocation == nil {
			continue
		}
		for _, result := range claim.Status.Allocation.Devices.Results {
			used.Add(result.Pool, result.Device)
		}
	}
	for _, object := range c.workloadInformer.GetStore().List() {
		item := object.(*unstructured.Unstructured)
		if item.GetNamespace()+"/"+item.GetName() == exclude {
			continue
		}
		var workload ttapi.Workload
		if ttapi.FromUnstructured(item, &workload) == nil && workload.Status.Phase != "Failed" && workload.Status.Phase != "Succeeded" {
			used.AddAssignments(workload.Status.Assignments)
		}
	}
	return used
}

// reconcileWorkload validates, assigns, observes, and cleans up one workload lifecycle.
func (c *Controller) reconcileWorkload(ctx context.Context, workload *ttapi.Workload, fabric ttapi.FabricTopologyStatus, used placement.Reservations) error {
	hash := workloadSpecHash(workload)
	key := workload.Namespace + "/" + workload.Name
	if err := validateWorkload(workload); err != nil {
		setWorkloadStatus(workload, "Failed", false, "InvalidSpec", err.Error())
		workload.Status.SpecHash = hash
		return c.updateWorkloadStatus(ctx, workload)
	}
	relevantGeneration := topology.WorkloadGeneration(fabric, workload.Spec.Topology, workload.Status.Assignments)
	if len(workload.Status.Assignments) > 0 {
		if terminalPhase(workload.Status.Phase) {
			delete(c.pending, key)
			return c.deleteChildren(ctx, workload)
		}
		phase, reason, message, started := c.observedPhase(workload)
		if assignmentsReserved(workload.Status.Assignments, used) {
			if started {
				setWorkloadStatus(workload, "Degraded", false, "DeviceClaimed", "an assigned device was consumed by another claim")
				return c.updateWorkloadStatus(ctx, workload)
			}
			delete(c.pending, key)
			if err := c.deleteChildren(ctx, workload); err != nil {
				return err
			}
			workload.Status = ttapi.WorkloadStatus{Phase: "Pending", ObservedGeneration: workload.Generation, SpecHash: hash}
			setWorkloadStatus(workload, "Pending", false, "Replanning", "an assigned device was consumed by another claim")
			return c.updateWorkloadStatus(ctx, workload)
		}
		if reason == "Unschedulable" && !started {
			delete(c.pending, key)
			if err := c.deleteChildren(ctx, workload); err != nil {
				return err
			}
			workload.Status = ttapi.WorkloadStatus{Phase: "Pending", ObservedGeneration: workload.Generation, SpecHash: hash}
			setWorkloadStatus(workload, "Pending", false, "Replanning", message)
			return c.updateWorkloadStatus(ctx, workload)
		}
		if phase == "Failed" || phase == "Succeeded" {
			delete(c.pending, key)
			setWorkloadStatus(workload, phase, phase == "Succeeded", reason, message)
			if err := c.deleteChildren(ctx, workload); err != nil {
				return err
			}
			return c.updateWorkloadStatus(ctx, workload)
		}
		changed := workload.Status.SpecHash != "" && workload.Status.SpecHash != hash
		fabricChanged := workload.Status.FabricGeneration != relevantGeneration
		if changed || fabricChanged {
			if started {
				reason, message = "FabricChanged", "relevant fabric topology changed after a rank started; assignment is frozen"
				if changed {
					reason, message = "SpecChanged", "workload spec changed after a rank started; assignment is frozen"
				}
				setWorkloadStatus(workload, "Degraded", false, reason, message)
				return c.updateWorkloadStatus(ctx, workload)
			}
			if err := c.deleteChildren(ctx, workload); err != nil {
				return err
			}
			delete(c.pending, key)
			workload.Status = ttapi.WorkloadStatus{Phase: "Pending", ObservedGeneration: workload.Generation, SpecHash: hash}
			return c.updateWorkloadStatus(ctx, workload)
		}
		setWorkloadStatus(workload, phase, phase == "Running", reason, message)
		workload.Status.SpecHash = hash
		if err := c.ensureChildren(ctx, workload); err != nil {
			return err
		}
		return c.updateWorkloadStatus(ctx, workload)
	}
	if !fabric.Valid {
		setWorkloadStatus(workload, "Pending", false, "FabricInvalid", "validated fabric topology is unavailable")
		workload.Status.SpecHash = hash
		return c.updateWorkloadStatus(ctx, workload)
	}
	placementCtx, cancel := context.WithTimeout(ctx, c.PlacementTimeout)
	defer cancel()
	placementStarted := time.Now()
	assignments, ok, err := placement.SolveContext(placementCtx, workload, fabric.Endpoints, used, placement.DefaultLimits)
	placementOutcome := "success"
	if err != nil {
		placementOutcome = "error"
	} else if !ok {
		placementOutcome = "unsatisfied"
	}
	if c.Metrics != nil {
		c.Metrics.ObservePlacement(time.Since(placementStarted), placementOutcome)
	}
	if err != nil {
		setWorkloadStatus(workload, "Pending", false, "PlacementError", err.Error())
		return c.updateWorkloadStatus(ctx, workload)
	}
	if !ok {
		setWorkloadStatus(workload, "Pending", false, "Unsatisfied", "no connected assignment satisfies all ranks")
		workload.Status.FabricGeneration, workload.Status.SpecHash = relevantGeneration, hash
		return c.updateWorkloadStatus(ctx, workload)
	}
	workload.Status.Assignments = assignments
	workload.Status.FabricGeneration = topology.WorkloadGeneration(fabric, workload.Spec.Topology, assignments)
	workload.Status.SpecHash = hash
	setWorkloadStatus(workload, "Assigned", true, "Assigned", "complete connected assignment reserved")
	if err := c.updateWorkloadStatus(ctx, workload); err != nil {
		return err
	}
	c.pending[key] = assignments
	return c.ensureChildren(ctx, workload)
}

// observedPhase derives workload lifecycle from cached rank Pod states.
func (c *Controller) observedPhase(workload *ttapi.Workload) (string, string, string, bool) {
	started, succeeded := false, 0
	for _, assignment := range workload.Status.Assignments {
		object, found, _ := c.podInformer.GetStore().GetByKey(workload.Namespace + "/" + assignment.PodName)
		if !found {
			continue
		}
		pod := object.(*corev1.Pod)
		if pod.Status.Phase == corev1.PodFailed {
			return "Failed", "RankFailed", "a rank Pod failed", started
		}
		if pod.Status.Phase == corev1.PodSucceeded {
			succeeded++
		}
		if pod.Status.StartTime != nil || pod.Status.Phase == corev1.PodRunning {
			started = true
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && condition.Reason == corev1.PodReasonUnschedulable {
				return "Assigned", "Unschedulable", condition.Message, started
			}
		}
	}
	if succeeded == len(workload.Status.Assignments) {
		return "Succeeded", "RanksSucceeded", "all rank Pods succeeded", true
	}
	if started {
		return "Running", "RanksRunning", "one or more rank Pods are running", true
	}
	return "Assigned", "ChildrenPending", "rank Pods and claims are pending", false
}

// updateWorkloadStatus retries conflicts and skips writes when status is unchanged.
func (c *Controller) updateWorkloadStatus(ctx context.Context, workload *ttapi.Workload) error {
	workload.Status.ObservedGeneration = workload.Generation
	resource := c.Dynamic.Resource(ttapi.WorkloadGVR).Namespace(workload.Namespace)
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := resource.Get(ctx, workload.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		var latest ttapi.Workload
		if err := ttapi.FromUnstructured(current, &latest); err != nil {
			return err
		}
		if terminalPhase(latest.Status.Phase) && latest.Status.Phase != workload.Status.Phase {
			return nil
		}
		encoded, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&workload.Status)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(current.Object["status"], encoded) {
			return nil
		}
		current.Object["status"] = encoded
		_, err = resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
		changed = err == nil
		return err
	})
	if err == nil && changed {
		reason, message := "StatusChanged", "workload status changed"
		if len(workload.Status.Conditions) > 0 {
			reason, message = workload.Status.Conditions[0].Reason, workload.Status.Conditions[0].Message
		}
		eventType := corev1.EventTypeNormal
		if workload.Status.Phase == "Degraded" || workload.Status.Phase == "Failed" || reason == "Unsatisfied" || reason == "FabricInvalid" || reason == "PlacementError" {
			eventType = corev1.EventTypeWarning
		}
		c.emitWorkloadEvent(workload, eventType, reason, message)
		c.logger().Info("workload decision",
			"workload_namespace", workload.Namespace,
			"workload", workload.Name,
			"workload_uid", workload.UID,
			"phase", workload.Status.Phase,
			"reason", reason,
			"assignment_count", len(workload.Status.Assignments),
		)
	}
	return err
}

// logger returns the configured structured logger, including for unit tests
// that invoke an individual reconciliation method without Run initialization.
func (c *Controller) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default().With("component", "controller")
}

// emitWorkloadEvent records a decision against the custom workload object.
func (c *Controller) emitWorkloadEvent(workload *ttapi.Workload, eventType, reason, message string) {
	if c.Recorder == nil {
		return
	}
	object := &unstructured.Unstructured{}
	object.SetAPIVersion(ttapi.SchedulingAPIVersion)
	object.SetKind(ttapi.WorkloadKind)
	object.SetNamespace(workload.Namespace)
	object.SetName(workload.Name)
	object.SetUID(workload.UID)
	c.Recorder.Event(object, eventType, reason, message)
}

// emitFabricEvent records topology validation transitions without producing an
// Event on every periodic refresh.
func (c *Controller) emitFabricEvent(eventType, reason, message string) {
	if c.Recorder == nil {
		return
	}
	object := &unstructured.Unstructured{}
	object.SetAPIVersion(ttapi.TopologyAPIVersion)
	object.SetKind(ttapi.FabricTopologyKind)
	object.SetName("cluster")
	c.Recorder.Event(object, eventType, reason, message)
}

// terminalPhase reports whether a workload has reached an irreversible final state.
func terminalPhase(phase string) bool {
	return phase == "Failed" || phase == "Succeeded"
}

// setWorkloadStatus updates phase and Ready condition while preserving transition time.
func setWorkloadStatus(workload *ttapi.Workload, phase string, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	transition := metav1.Now()
	for _, existing := range workload.Status.Conditions {
		if existing.Type == "Ready" && existing.Status == status && existing.Reason == reason {
			transition = existing.LastTransitionTime
		}
	}
	workload.Status.Phase = phase
	workload.Status.Conditions = []metav1.Condition{{
		Type: "Ready", Status: status, Reason: reason, Message: message,
		ObservedGeneration: workload.Generation, LastTransitionTime: transition,
	}}
}

// workloadSpecHash returns a short deterministic digest used to detect spec edits.
func workloadSpecHash(workload *ttapi.Workload) string {
	data, _ := json.Marshal(workload.Spec)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// assignmentsReserved reports whether another claim or workload uses an assigned device.
func assignmentsReserved(assignments []ttapi.RankAssignment, used placement.Reservations) bool {
	for _, assignment := range assignments {
		for _, item := range assignment.Devices {
			if _, found := used[placement.DeviceID{Pool: item.Pool, Name: item.Name}]; found {
				return true
			}
		}
	}
	return false
}
