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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

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

func inventoryFlags(name string) (*flag.FlagSet, *device.Roots) {
	roots := device.DefaultRoots()
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.StringVar(&roots.DeviceRoot, "device-root", roots.DeviceRoot, "Tenstorrent device root")
	set.StringVar(&roots.TenstorrentSysfsRoot, "sysfs-root", roots.TenstorrentSysfsRoot, "Tenstorrent sysfs root")
	set.StringVar(&roots.PCISysfsRoot, "pci-sysfs-root", roots.PCISysfsRoot, "PCI sysfs root")
	set.StringVar(&roots.StateDir, "state-dir", roots.StateDir, "claim state directory")
	return set, &roots
}
func provider(roots *device.Roots) (device.FilesystemProvider, error) {
	return device.NewFilesystemProvider(*roots)
}

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

func runNode(args []string) error {
	set, roots := inventoryFlags("node")
	nodeName := os.Getenv("NODE_NAME")
	interval, cdiDir, pluginDir, registrarDir := 30*time.Second, "/var/run/cdi", "/var/lib/kubelet/plugins/dra.tenstorrent.com", kubeletplugin.KubeletRegistryDir
	set.StringVar(&nodeName, "node-name", nodeName, "Kubernetes node name")
	set.DurationVar(&interval, "interval", interval, "inventory interval")
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
	kube, dynamicClient, err := clusterClients()
	if err != nil {
		return err
	}
	source, err := provider(roots)
	if err != nil {
		return err
	}
	manager, err := lifecycle.NewManager(lifecycle.Config{
		NodeName: nodeName,
		Driver:   dra.DefaultDriverName,
		StateDir: roots.StateDir,
		CDIDir:   cdiDir,
		Inventory: func(ctx context.Context) (device.InventorySnapshot, error) {
			return device.BuildSnapshot(ctx, source)
		},
	})
	if err != nil {
		return err
	}
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
		if discoverErr != nil {
			log.Printf("inventory: %v", discoverErr)
		} else {
			if err := helper.PublishResources(ctx, dra.DriverResources(nodeName, snapshot)); err != nil {
				log.Printf("publish resources: %v", err)
			}
			if err := topology.PublishNode(ctx, dynamicClient, nodeName, node.UID, snapshot); err != nil {
				log.Printf("publish topology: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runController(args []string) error {
	set := flag.NewFlagSet("controller", flag.ContinueOnError)
	interval, ttl := 5*time.Second, 90*time.Second
	set.DurationVar(&interval, "interval", interval, "controller interval")
	set.DurationVar(&ttl, "topology-ttl", ttl, "node topology TTL")
	if err := set.Parse(args); err != nil {
		return err
	}
	kube, dynamicClient, err := clusterClients()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return (&controller.Controller{Kube: kube, Dynamic: dynamicClient, Interval: interval, TopologyTTL: ttl}).Run(ctx)
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
