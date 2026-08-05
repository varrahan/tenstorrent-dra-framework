package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ttapi "github.com/varrahan/tenstorrent-dra-framework/src/internal/api"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/controller"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/observability"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/topology"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

// These values are replaced with deterministic release metadata through
// -ldflags. Defaults keep local development builds useful and explicit.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

type buildInformation struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// main dispatches the requested inventory, node-driver, or controller command.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})).With(
		"version", version,
		"commit", commit,
	)
	slog.SetDefault(logger)
	if err := runCommand(os.Args[1:], os.Stdout); err != nil {
		fatal(err.Error())
	}
}

// runCommand validates and dispatches one executable invocation.
func runCommand(args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return errors.New("usage: tt-dra-driver <version|list|node|controller|cleanup>")
	}
	var err error
	switch args[0] {
	case "version":
		err = runVersion(stdout)
	case "list":
		err = runList(args[1:])
	case "node":
		err = runNode(args[1:])
	case "controller":
		err = runController(args[1:])
	case "cleanup":
		err = runCleanup(args[1:])
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	return err
}

// runVersion emits machine-readable provenance for the running binary.
func runVersion(writer io.Writer) error {
	return json.NewEncoder(writer).Encode(buildInformation{
		Version: version, Commit: commit, BuildDate: buildDate,
	})
}

// runCleanup removes driver-generated cluster objects during Helm uninstall.
// It refuses to proceed while an allocated Tenstorrent claim or active
// TenstorrentWorkload exists.
func runCleanup(args []string) error {
	set := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	kubeAPIQPS, kubeAPIBurst := 20.0, 40
	releaseName, releaseNamespace, resourcePrefix := "", "", ""
	set.Float64Var(&kubeAPIQPS, "kube-api-qps", kubeAPIQPS, "Kubernetes client request rate")
	set.IntVar(&kubeAPIBurst, "kube-api-burst", kubeAPIBurst, "Kubernetes client burst limit")
	set.StringVar(&releaseName, "release-name", releaseName, "Helm release name")
	set.StringVar(&releaseNamespace, "release-namespace", releaseNamespace, "Helm release namespace")
	set.StringVar(&resourcePrefix, "resource-prefix", resourcePrefix, "release-safe workload resource prefix")
	if err := set.Parse(args); err != nil {
		return err
	}
	if kubeAPIQPS <= 0 || kubeAPIBurst <= 0 {
		return fmt.Errorf("Kubernetes API QPS and burst must be positive")
	}
	if releaseName == "" || releaseNamespace == "" || resourcePrefix == "" {
		return fmt.Errorf("release name, namespace, and resource prefix are required")
	}
	kube, dynamicClient, err := clusterClients(kubeAPIQPS, kubeAPIBurst)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	claims, err := kube.ResourceV1().ResourceClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list ResourceClaims: %w", err)
	}
	for index := range claims.Items {
		claim := &claims.Items[index]
		if claim.Status.Allocation == nil {
			continue
		}
		for _, result := range claim.Status.Allocation.Devices.Results {
			if result.Driver == dra.DefaultDriverName {
				return fmt.Errorf("refusing cleanup: ResourceClaim %s/%s still has a Tenstorrent allocation", claim.Namespace, claim.Name)
			}
		}
	}
	workloads, err := dynamicClient.Resource(ttapi.WorkloadGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("list TenstorrentWorkloads: %w", err)
	}
	if err == nil {
		for index := range workloads.Items {
			phase, _, _ := unstructured.NestedString(workloads.Items[index].Object, "status", "phase")
			if phase != "Succeeded" && phase != "Failed" {
				return fmt.Errorf("refusing cleanup: TenstorrentWorkload %s/%s is still active", workloads.Items[index].GetNamespace(), workloads.Items[index].GetName())
			}
		}
	}
	if err := kube.AppsV1().DaemonSets(releaseNamespace).Delete(ctx, resourcePrefix+"-node", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete node DaemonSet: %w", err)
	}
	if err := kube.AppsV1().Deployments(releaseNamespace).Delete(ctx, resourcePrefix+"-controller", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete controller Deployment: %w", err)
	}
	deadline := time.Now().Add(time.Minute)
	for {
		pods, err := kube.CoreV1().Pods(releaseNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/instance=" + releaseName,
		})
		if err != nil {
			return fmt.Errorf("wait for driver Pods: %w", err)
		}
		remaining := 0
		for index := range pods.Items {
			if pods.Items[index].Labels["job-name"] != resourcePrefix+"-cleanup" {
				remaining++
			}
		}
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %d driver Pods to terminate", remaining)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	slices, err := kube.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list ResourceSlices: %w", err)
	}
	deletedSlices := 0
	for index := range slices.Items {
		if slices.Items[index].Spec.Driver != dra.DefaultDriverName {
			continue
		}
		if err := kube.ResourceV1().ResourceSlices().Delete(ctx, slices.Items[index].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete ResourceSlice %q: %w", slices.Items[index].Name, err)
		}
		deletedSlices++
	}
	if err := dynamicClient.Resource(ttapi.NodeTopologyGVR).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete node topology objects: %w", err)
	}
	if err := dynamicClient.Resource(ttapi.FabricTopologyGVR).Delete(ctx, "cluster", metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete fabric topology: %w", err)
	}
	slog.Info("driver-generated cluster objects removed", "release", releaseName, "namespace", releaseNamespace, "resource_slices", deletedSlices)
	return nil
}

// inventoryFlags defines the shared host-path flags for inventory-backed commands.
func inventoryFlags(name string) (*flag.FlagSet, *device.Roots) {
	roots := device.DefaultRoots()
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.StringVar(&roots.DeviceRoot, "device-root", roots.DeviceRoot, "Tenstorrent device root")
	set.StringVar(&roots.TenstorrentSysfsRoot, "sysfs-root", roots.TenstorrentSysfsRoot, "Tenstorrent sysfs root")
	set.StringVar(&roots.PCISysfsRoot, "pci-sysfs-root", roots.PCISysfsRoot, "PCI sysfs root")
	set.StringVar(&roots.StateDir, "state-dir", roots.StateDir, "claim state directory")
	return set, &roots
}

// provider constructs the filesystem inventory source from parsed root flags.
func provider(roots *device.Roots) (device.FilesystemProvider, error) {
	return device.NewFilesystemProvider(*roots)
}

// clusterClients creates typed and dynamic Kubernetes clients from in-cluster configuration.
func clusterClients(qps float64, burst int) (kubernetes.Interface, dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, err
	}
	config.QPS = float32(qps)
	config.Burst = burst
	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return kube, dynamicClient, nil
}

// runList prints one normalized inventory snapshot as JSON.
func runList(args []string) error {
	set, roots := inventoryFlags("list")
	if err := set.Parse(args); err != nil {
		return err
	}
	source, err := provider(roots)
	if err != nil {
		return err
	}
	snapshot, err := device.BuildSnapshot(context.Background(), source)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

// runNode starts discovery, lifecycle enforcement, DRA publication, and health reporting for one node.
func runNode(args []string) error {
	set, roots := inventoryFlags("node")
	nodeName := os.Getenv("NODE_NAME")
	resetMode := "ioctl"
	requireIOMMU := true
	interval, inventoryGrace := 30*time.Second, 60*time.Second
	httpAddress := ":8080"
	kubeAPIQPS, kubeAPIBurst := 20.0, 40
	cdiDir, pluginDir, registrarDir := "/var/run/cdi", "/var/lib/kubelet/plugins/dra.tenstorrent.com", kubeletplugin.KubeletRegistryDir
	set.StringVar(&nodeName, "node-name", nodeName, "Kubernetes node name")
	set.DurationVar(&interval, "interval", interval, "inventory interval")
	set.DurationVar(&inventoryGrace, "inventory-grace-period", inventoryGrace, "maximum age of a cached healthy inventory observation")
	set.StringVar(&resetMode, "reset-mode", resetMode, "device reset mode: ioctl or noop")
	set.BoolVar(&requireIOMMU, "require-iommu", requireIOMMU, "quarantine devices without an IOMMU group")
	set.StringVar(&cdiDir, "cdi-dir", cdiDir, "CDI directory")
	set.StringVar(&pluginDir, "plugin-dir", pluginDir, "kubelet plugin directory")
	set.StringVar(&registrarDir, "registrar-dir", registrarDir, "kubelet registrar directory")
	set.StringVar(&httpAddress, "http-address", httpAddress, "health and metrics listen address")
	set.Float64Var(&kubeAPIQPS, "kube-api-qps", kubeAPIQPS, "Kubernetes client request rate")
	set.IntVar(&kubeAPIBurst, "kube-api-burst", kubeAPIBurst, "Kubernetes client burst limit")
	if err := set.Parse(args); err != nil {
		return err
	}
	if nodeName == "" {
		return fmt.Errorf("node name is required")
	}
	if interval <= 0 {
		return fmt.Errorf("inventory interval must be positive")
	}
	if inventoryGrace < interval {
		return fmt.Errorf("inventory grace period must be at least the inventory interval")
	}
	if kubeAPIQPS <= 0 || kubeAPIBurst <= 0 {
		return fmt.Errorf("Kubernetes API QPS and burst must be positive")
	}
	var resetter lifecycle.Resetter
	switch resetMode {
	case "ioctl":
		resetter = lifecycle.KMDResetter{}
	case "noop":
		if requireIOMMU {
			return fmt.Errorf("noop reset mode requires -require-iommu=false and is for synthetic validation only")
		}
		resetter = lifecycle.NoopResetter{}
	default:
		return fmt.Errorf("unsupported reset mode %q", resetMode)
	}
	kube, dynamicClient, err := clusterClients(kubeAPIQPS, kubeAPIBurst)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := slog.Default().With("component", "node", "node", nodeName)
	metrics := observability.NewMetrics()
	health := observability.NewHealth("node", nodeName, metrics)
	if _, err := observability.StartServer(ctx, httpAddress, health, metrics.Handler(), logger); err != nil {
		return err
	}
	recorder, broadcaster := eventRecorder(ctx, kube, "tenstorrent-dra-node", nodeName)
	defer broadcaster.Shutdown()
	source, err := provider(roots)
	if err != nil {
		return err
	}
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName:        nodeName,
		Driver:          dra.DefaultDriverName,
		StateDir:        roots.StateDir,
		CDIDir:          cdiDir,
		Resetter:        resetter,
		Metrics:         metrics,
		Logger:          logger,
		EventSink:       lifecycleEventSink(recorder, nodeName),
		RequireIOMMU:    requireIOMMU,
		MaxInventoryAge: inventoryGrace,
		Inventory: func(ctx context.Context) (device.InventorySnapshot, error) {
			return device.BuildSnapshot(ctx, source)
		},
		Allocations: func(ctx context.Context) ([]*resourceapi.ResourceClaim, error) {
			list, err := kube.ResourceV1().ResourceClaims(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			claims := make([]*resourceapi.ResourceClaim, 0, len(list.Items))
			for index := range list.Items {
				claims = append(claims, list.Items[index].DeepCopy())
			}
			return claims, nil
		},
	})
	if err != nil {
		return err
	}
	defer manager.Close()
	helper, err := kubeletplugin.Start(ctx, manager, kubeletplugin.DriverName(dra.DefaultDriverName), kubeletplugin.KubeClient(kube), kubeletplugin.NodeName(nodeName), kubeletplugin.PluginDataDirectoryPath(pluginDir), kubeletplugin.RegistrarDirectoryPath(registrarDir))
	if err != nil {
		return err
	}
	defer helper.Stop()
	node, err := kube.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	health.MarkStarted()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastSafetyReason := ""
	lastSuccessfulObservation := time.Time{}
	for {
		reconcileStarted := time.Now()
		reconciliationID := fmt.Sprintf("inventory-%s-%d", nodeName, reconcileStarted.UnixNano())
		snapshot, discoverErr := device.BuildSnapshot(ctx, source)
		var safety lifecycle.Safety
		var monitorErr error
		if discoverErr != nil {
			logger.Error("inventory discovery failed", "reconciliation_id", reconciliationID, "error", discoverErr)
			snapshot, safety, monitorErr = manager.InventoryFailed(discoverErr)
		} else {
			lastSuccessfulObservation = snapshot.ObservedAt
			snapshot, safety, monitorErr = manager.Monitor(ctx, snapshot)
		}
		if monitorErr != nil {
			logger.Error("hardware janitor reconciliation failed", "reconciliation_id", reconciliationID, "error", monitorErr)
		}
		resourcesErr := helper.PublishResources(ctx, dra.DriverResourcesAt(nodeName, snapshot, inventoryGrace, time.Now()))
		if resourcesErr != nil {
			logger.Error("resource publication failed", "reconciliation_id", reconciliationID, "error", resourcesErr)
			recorder.Eventf(node, corev1.EventTypeWarning, "ResourcePublicationFailed", "ResourceSlice publication failed: %v", resourcesErr)
		}
		topologyErr := topology.PublishNode(ctx, dynamicClient, nodeName, node.UID, snapshot)
		if topologyErr != nil {
			logger.Error("node topology publication failed", "reconciliation_id", reconciliationID, "error", topologyErr)
			recorder.Eventf(node, corev1.EventTypeWarning, "TopologyPublicationFailed", "Node topology publication failed: %v", topologyErr)
		}
		safetyErr := lifecycle.UpdateNodeSafety(ctx, kube, nodeName, safety)
		if safetyErr != nil {
			logger.Error("node safety publication failed", "reconciliation_id", reconciliationID, "error", safetyErr)
			recorder.Eventf(node, corev1.EventTypeWarning, "NodeSafetyPublicationFailed", "Node safety publication failed: %v", safetyErr)
		}
		if safety.Reason != lastSafetyReason {
			eventType := corev1.EventTypeNormal
			if safety.Unsafe {
				eventType = corev1.EventTypeWarning
			}
			recorder.Event(node, eventType, safety.Reason, safety.Message)
			lastSafetyReason = safety.Reason
		}
		published := 0
		for _, item := range snapshot.Devices {
			if item.Eligible {
				published++
			}
		}
		stats := manager.SnapshotStats()
		metrics.ObserveInventory(nodeName, lastSuccessfulObservation, inventoryGrace, published, stats.Allocated, stats.Quarantined)
		reconcileErr := errors.Join(discoverErr, monitorErr, resourcesErr, topologyErr, safetyErr)
		metrics.ObserveReconcile("node", "inventory", time.Since(reconcileStarted), reconcileErr)
		health.SetReady(discoverErr == nil && resourcesErr == nil && topologyErr == nil && safetyErr == nil)
		logger.Info("inventory reconciliation completed",
			"reconciliation_id", reconciliationID,
			"duration_seconds", time.Since(reconcileStarted).Seconds(),
			"published_devices", published,
			"allocated_devices", stats.Allocated,
			"quarantined_devices", stats.Quarantined,
			"healthy_devices", safety.Healthy,
			"total_devices", safety.Total,
			"outcome", outcome(reconcileErr),
		)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// runController starts the cluster topology and workload reconciliation loop.
func runController(args []string) error {
	set := flag.NewFlagSet("controller", flag.ContinueOnError)
	ttl, placementTimeout := 90*time.Second, 2*time.Second
	httpAddress := ":8080"
	kubeAPIQPS, kubeAPIBurst := 20.0, 40
	leaderElect := true
	disableWorkloadAppArmor := false
	leaseName, leaseNamespace := "tenstorrent-dra-controller", os.Getenv("POD_NAMESPACE")
	identity := os.Getenv("POD_NAME")
	if leaseNamespace == "" {
		leaseNamespace = "default"
	}
	if identity == "" {
		identity, _ = os.Hostname()
	}
	set.DurationVar(&ttl, "topology-ttl", ttl, "node topology TTL")
	set.DurationVar(&placementTimeout, "placement-timeout", placementTimeout, "maximum placement solve time")
	set.BoolVar(&leaderElect, "leader-elect", leaderElect, "run controller reconciliation under a Lease")
	set.BoolVar(&disableWorkloadAppArmor, "synthetic-disable-workload-apparmor", disableWorkloadAppArmor, "omit workload AppArmor profiles for synthetic validation")
	set.StringVar(&leaseName, "leader-election-name", leaseName, "leader election Lease name")
	set.StringVar(&leaseNamespace, "leader-election-namespace", leaseNamespace, "leader election Lease namespace")
	set.StringVar(&httpAddress, "http-address", httpAddress, "health and metrics listen address")
	set.Float64Var(&kubeAPIQPS, "kube-api-qps", kubeAPIQPS, "Kubernetes client request rate")
	set.IntVar(&kubeAPIBurst, "kube-api-burst", kubeAPIBurst, "Kubernetes client burst limit")
	if err := set.Parse(args); err != nil {
		return err
	}
	if ttl <= 0 || placementTimeout <= 0 {
		return fmt.Errorf("topology TTL and placement timeout must be positive")
	}
	if kubeAPIQPS <= 0 || kubeAPIBurst <= 0 {
		return fmt.Errorf("Kubernetes API QPS and burst must be positive")
	}
	kube, dynamicClient, err := clusterClients(kubeAPIQPS, kubeAPIBurst)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := slog.Default().With("component", "controller", "pod", identity, "namespace", leaseNamespace)
	metrics := observability.NewMetrics()
	health := observability.NewHealth("controller", "", metrics)
	if _, err := observability.StartServer(ctx, httpAddress, health, metrics.Handler(), logger); err != nil {
		return err
	}
	recorder, broadcaster := eventRecorder(ctx, kube, "tenstorrent-dra-controller", identity)
	defer broadcaster.Shutdown()
	health.MarkStarted()
	health.SetReady(true)
	reconciler := &controller.Controller{
		Kube: kube, Dynamic: dynamicClient, TopologyTTL: ttl, PlacementTimeout: placementTimeout,
		DisableWorkloadAppArmor: disableWorkloadAppArmor, Metrics: metrics, Logger: logger, Recorder: recorder,
	}
	if !leaderElect {
		return reconciler.Run(ctx)
	}
	result := make(chan error, 1)
	report := func(err error) {
		select {
		case result <- err:
		default:
		}
	}
	go leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{Name: leaseName, Namespace: leaseNamespace},
			Client:    kube.CoordinationV1(), LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
		},
		LeaseDuration: 15 * time.Second, RenewDeadline: 10 * time.Second, RetryPeriod: 2 * time.Second,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				logger.Info("leader election acquired", "leader_identity", identity)
				recorder.Event(podReference(identity, leaseNamespace), corev1.EventTypeNormal, "BecameLeader", "Controller acquired the leader lease")
				if err := reconciler.Run(leaderCtx); err != nil {
					health.SetReady(false)
					report(err)
				}
			},
			OnStoppedLeading: func() {
				if ctx.Err() == nil {
					health.SetReady(false)
					recorder.Event(podReference(identity, leaseNamespace), corev1.EventTypeWarning, "LeadershipLost", "Controller lost the leader lease")
					report(fmt.Errorf("controller leader election lost"))
				}
			},
			OnNewLeader: func(newIdentity string) {
				logger.Info("leader election observed", "leader_identity", newIdentity)
			},
		},
	})
	select {
	case err := <-result:
		stop()
		return err
	case <-ctx.Done():
		return nil
	}
}

// eventRecorder writes correlated core/v1 Events through the component's
// existing Kubernetes client.
func eventRecorder(ctx context.Context, kube kubernetes.Interface, component, host string) (record.EventRecorder, record.EventBroadcaster) {
	broadcaster := record.NewBroadcaster(record.WithContext(ctx))
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kube.CoreV1().Events(metav1.NamespaceAll)})
	return broadcaster.NewRecorder(kubescheme.Scheme, corev1.EventSource{Component: component, Host: host}), broadcaster
}

// lifecycleEventSink translates durable janitor audit decisions into
// Kubernetes Events on the affected claim or node.
func lifecycleEventSink(recorder record.EventRecorder, nodeName string) func(lifecycle.AuditEvent, *lifecycle.PreparedClaim) {
	return func(event lifecycle.AuditEvent, claim *lifecycle.PreparedClaim) {
		var object runtime.Object = &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
		if claim != nil {
			object = &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{
				Name: claim.Name, Namespace: claim.Namespace, UID: claim.UID,
			}}
		}
		eventType := corev1.EventTypeNormal
		if event.Outcome == "failure" || event.Outcome == "quarantined" || event.Outcome == "degraded" {
			eventType = corev1.EventTypeWarning
		}
		recorder.Eventf(object, eventType, lifecycleEventReason(event),
			"action=%s outcome=%s device=%s reason=%s", event.Action, event.Outcome, event.Device, event.Reason)
	}
}

// lifecycleEventReason maps internal action names to stable UpperCamelCase
// Kubernetes Event reasons.
func lifecycleEventReason(event lifecycle.AuditEvent) string {
	if event.Outcome == "quarantined" {
		return "DeviceQuarantined"
	}
	switch event.Action {
	case "claim-prepare-intent":
		return "ClaimPrepareStarted"
	case "claim-prepare":
		return "ClaimPrepared"
	case "claim-release-intent":
		return "ClaimReleaseStarted"
	case "claim-release":
		return "ClaimReleased"
	case "preflight-sanitize":
		return "PreflightSanitized"
	case "postflight-sanitize":
		return "PostflightSanitized"
	case "recovery-sanitize", "recovery-health":
		return "DeviceRecovered"
	case "inventory":
		return "InventoryDegraded"
	case "state-recovery", "startup-recovery":
		return "StateRecovered"
	default:
		parts := strings.FieldsFunc(event.Action, func(r rune) bool { return r == '-' || r == '_' })
		for index := range parts {
			if parts[index] != "" {
				parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
			}
		}
		return strings.Join(parts, "")
	}
}

// podReference returns a minimal object suitable for leader-election Events.
func podReference(name, namespace string) runtime.Object {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

func outcome(err error) string {
	if err != nil {
		return "failure"
	}
	return "success"
}

// fatal emits a structured command failure and terminates with a failure status.
func fatal(message string) {
	slog.Error("command failed", "error", message)
	os.Exit(1)
}
