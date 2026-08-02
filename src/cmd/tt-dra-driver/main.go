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

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/dra"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/lifecycle"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

func main() {
	roots := device.DefaultRoots()
	deviceRoot := flag.String("device-root", roots.DeviceRoot, "Tenstorrent device root or device node")
	sysfsRoot := flag.String("sysfs-root", roots.TenstorrentSysfsRoot, "Tenstorrent class sysfs root")
	pciSysfsRoot := flag.String("pci-sysfs-root", roots.PCISysfsRoot, "PCI sysfs device root")
	stateDir := flag.String("state-dir", roots.StateDir, "persistent state directory")
	publish := flag.Bool("publish", false, "run as a node daemon and publish live ResourceSlices")
	nodeName := flag.String("node-name", os.Getenv("NODE_NAME"), "Kubernetes node name (required with -publish or -kubelet-plugin)")
	kubeconfig := flag.String("kubeconfig", "", "kubeconfig path; empty uses in-cluster configuration")
	interval := flag.Duration("interval", 30*time.Second, "inventory reconciliation interval")
	kubeletPlugin := flag.Bool("kubelet-plugin", false, "run the kubelet DRA gRPC/CDI lifecycle service")
	pluginDir := flag.String("plugin-dir", "/var/lib/kubelet/plugins/dra.tenstorrent.com", "kubelet DRA plugin socket directory")
	registrarDir := flag.String("registrar-dir", kubeletplugin.KubeletRegistryDir, "kubelet plugin registrar directory")
	cdiDir := flag.String("cdi-dir", "/var/run/cdi", "CDI specification directory")
	flag.Parse()

	roots.DeviceRoot = *deviceRoot
	roots.TenstorrentSysfsRoot = *sysfsRoot
	roots.PCISysfsRoot = *pciSysfsRoot
	roots.StateDir = *stateDir
	provider, err := device.NewFilesystemProvider(roots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure inventory: %v\n", err)
		os.Exit(1)
	}
	if *publish {
		if *nodeName == "" {
			fmt.Fprintln(os.Stderr, "-node-name or NODE_NAME is required with -publish")
			os.Exit(2)
		}
		if *interval <= 0 {
			fmt.Fprintln(os.Stderr, "-interval must be positive")
			os.Exit(2)
		}
		if err := runPublisher(provider, *nodeName, *kubeconfig, *interval); err != nil {
			fmt.Fprintf(os.Stderr, "publish inventory: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *kubeletPlugin {
		if *nodeName == "" {
			fmt.Fprintln(os.Stderr, "-node-name or NODE_NAME is required with -kubelet-plugin")
			os.Exit(2)
		}
		if err := runKubeletPlugin(provider, *nodeName, *kubeconfig, *stateDir, *cdiDir, *pluginDir, *registrarDir); err != nil {
			fmt.Fprintf(os.Stderr, "run kubelet plugin: %v\n", err)
			os.Exit(1)
		}
		return
	}
	snapshot, err := device.BuildSnapshot(context.Background(), provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover inventory: %v\n", err)
		os.Exit(1)
	}
	devices := make([]device.Node, 0, len(snapshot.Devices))
	for _, observed := range snapshot.Devices {
		devices = append(devices, observed.Node)
	}

	output := struct {
		DeviceRoot string                   `json:"deviceRoot"`
		Devices    []device.Node            `json:"devices"`
		Inventory  device.InventorySnapshot `json:"inventory"`
	}{
		DeviceRoot: *deviceRoot,
		Devices:    devices,
		Inventory:  snapshot,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "encode discovery output: %v\n", err)
		os.Exit(1)
	}
}

func runKubeletPlugin(provider device.Provider, nodeName, kubeconfig, stateDir, cdiDir, pluginDir, registrarDir string) error {
	config, err := kubeConfig(kubeconfig)
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: nodeName,
		Driver:   dra.DefaultDriverName,
		StateDir: stateDir,
		CDIDir:   cdiDir,
		Inventory: func(ctx context.Context) (device.InventorySnapshot, error) {
			return device.BuildSnapshot(ctx, provider)
		},
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	helper, err := kubeletplugin.Start(ctx, manager,
		kubeletplugin.DriverName(dra.DefaultDriverName),
		kubeletplugin.KubeClient(clientset),
		kubeletplugin.NodeName(nodeName),
		kubeletplugin.PluginDataDirectoryPath(pluginDir),
		kubeletplugin.RegistrarDirectoryPath(registrarDir),
	)
	if err != nil {
		return err
	}
	<-ctx.Done()
	helper.Stop()
	return nil
}

func runPublisher(provider device.Provider, nodeName, kubeconfig string, interval time.Duration) error {
	config, err := kubeConfig(kubeconfig)
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	publisher, err := dra.NewResourceSlicePublisher(clientset.ResourceV1(), dra.DefaultDriverName)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	backoff := time.Second
	for {
		snapshot, discoverErr := device.BuildSnapshot(ctx, provider)
		if discoverErr != nil {
			log.Printf("inventory reconciliation failed: %v; retrying in %s", discoverErr, backoff)
			if !wait(ctx, backoff) {
				return nil
			}
			backoff = minDuration(backoff*2, interval)
			continue
		}
		if publishErr := publisher.Publish(ctx, nodeName, snapshot); publishErr != nil {
			log.Printf("ResourceSlice reconciliation failed: %v; retrying in %s", publishErr, backoff)
			if !wait(ctx, backoff) {
				return nil
			}
			backoff = minDuration(backoff*2, interval)
			continue
		}
		backoff = time.Second
		if !wait(ctx, interval) {
			return nil
		}
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func kubeConfig(path string) (*rest.Config, error) {
	if path != "" {
		config, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, fmt.Errorf("load kubeconfig: %w", err)
		}
		config.Timeout = 10 * time.Second
		return config, nil
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("load in-cluster config (use -kubeconfig outside a Pod): %w", err)
	}
	config.Timeout = 10 * time.Second
	return config, nil
}
