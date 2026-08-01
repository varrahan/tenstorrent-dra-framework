package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
)

func main() {
	roots := device.DefaultRoots()
	deviceRoot := flag.String("device-root", roots.DeviceRoot, "Tenstorrent device root or device node")
	sysfsRoot := flag.String("sysfs-root", roots.TenstorrentSysfsRoot, "Tenstorrent class sysfs root")
	pciSysfsRoot := flag.String("pci-sysfs-root", roots.PCISysfsRoot, "PCI sysfs device root")
	stateDir := flag.String("state-dir", roots.StateDir, "persistent state directory")
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
