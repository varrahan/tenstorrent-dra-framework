package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/controller"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/topology"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

// main dispatches the requested inventory, node-driver, or controller command.
func main() {
	if len(os.Args) < 2 {
		fatal("usage: tt-dra-driver <list|node|controller>")
	}
	var err error
	switch os.Args[1] {
	case "list":
		err = runList(os.Args[2:])
	case "node":
		err = runNode(os.Args[2:])
	case "controller":
		err = runController(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
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
func clusterClients() (kubernetes.Interface, dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, err
	}
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
	cdiDir, pluginDir, registrarDir := "/var/run/cdi", "/var/lib/kubelet/plugins/dra.tenstorrent.com", kubeletplugin.KubeletRegistryDir
	set.StringVar(&nodeName, "node-name", nodeName, "Kubernetes node name")
	set.DurationVar(&interval, "interval", interval, "inventory interval")
	set.DurationVar(&inventoryGrace, "inventory-grace-period", inventoryGrace, "maximum age of a cached healthy inventory observation")
	set.StringVar(&resetMode, "reset-mode", resetMode, "device reset mode: ioctl or noop")
	set.BoolVar(&requireIOMMU, "require-iommu", requireIOMMU, "quarantine devices without an IOMMU group")
	set.StringVar(&cdiDir, "cdi-dir", cdiDir, "CDI directory")
	set.StringVar(&pluginDir, "plugin-dir", pluginDir, "kubelet plugin directory")
	set.StringVar(&registrarDir, "registrar-dir", registrarDir, "kubelet registrar directory")
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
	kube, dynamicClient, err := clusterClients()
	if err != nil {
		return err
	}
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	helper, err := kubeletplugin.Start(ctx, manager, kubeletplugin.DriverName(dra.DefaultDriverName), kubeletplugin.KubeClient(kube), kubeletplugin.NodeName(nodeName), kubeletplugin.PluginDataDirectoryPath(pluginDir), kubeletplugin.RegistrarDirectoryPath(registrarDir))
	if err != nil {
		return err
	}
	defer helper.Stop()
	node, err := kube.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		snapshot, discoverErr := device.BuildSnapshot(ctx, source)
		var safety lifecycle.Safety
		var monitorErr error
		if discoverErr != nil {
			log.Printf("inventory: %v", discoverErr)
			snapshot, safety, monitorErr = manager.InventoryFailed(discoverErr)
		} else {
			snapshot, safety, monitorErr = manager.Monitor(ctx, snapshot)
		}
		if monitorErr != nil {
			log.Printf("hardware janitor: %v", monitorErr)
		}
		if err := helper.PublishResources(ctx, dra.DriverResourcesAt(nodeName, snapshot, inventoryGrace, time.Now())); err != nil {
			log.Printf("publish resources: %v", err)
		}
		if err := topology.PublishNode(ctx, dynamicClient, nodeName, node.UID, snapshot); err != nil {
			log.Printf("publish topology: %v", err)
		}
		if err := lifecycle.UpdateNodeSafety(ctx, kube, nodeName, safety); err != nil {
			log.Printf("publish node safety: %v", err)
		}
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
	leaderElect := true
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
	set.StringVar(&leaseName, "leader-election-name", leaseName, "leader election Lease name")
	set.StringVar(&leaseNamespace, "leader-election-namespace", leaseNamespace, "leader election Lease namespace")
	if err := set.Parse(args); err != nil {
		return err
	}
	if ttl <= 0 || placementTimeout <= 0 {
		return fmt.Errorf("topology TTL and placement timeout must be positive")
	}
	kube, dynamicClient, err := clusterClients()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	reconciler := &controller.Controller{Kube: kube, Dynamic: dynamicClient, TopologyTTL: ttl, PlacementTimeout: placementTimeout}
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
				if err := reconciler.Run(leaderCtx); err != nil {
					report(err)
				}
			},
			OnStoppedLeading: func() {
				if ctx.Err() == nil {
					report(fmt.Errorf("controller leader election lost"))
				}
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

// fatal writes a command error to stderr and terminates with a failure status.
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
