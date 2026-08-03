package main

import (
	"strings"
	"testing"
)

func TestInventoryFlagsPreserveDefaultsAndOverrides(t *testing.T) {
	set, roots := inventoryFlags("test")
	if roots.DeviceRoot != "/dev/tenstorrent" || roots.TenstorrentSysfsRoot != "/sys/class/tenstorrent" || roots.PCISysfsRoot != "/sys/bus/pci/devices" || roots.StateDir != "/var/lib/tenstorrent-dra" {
		t.Fatalf("default roots changed: %#v", roots)
	}
	if err := set.Parse([]string{
		"--device-root=/devices",
		"--sysfs-root=/sysfs",
		"--pci-sysfs-root=/pci",
		"--state-dir=/state",
	}); err != nil {
		t.Fatal(err)
	}
	if roots.DeviceRoot != "/devices" || roots.TenstorrentSysfsRoot != "/sysfs" || roots.PCISysfsRoot != "/pci" || roots.StateDir != "/state" {
		t.Fatalf("flag overrides were not applied: %#v", roots)
	}
}

func TestProviderRejectsRelativeRoots(t *testing.T) {
	_, roots := inventoryFlags("test")
	roots.DeviceRoot = "relative"
	if _, err := provider(roots); err == nil {
		t.Fatal("relative root was accepted")
	}
}

func TestRunNodeRejectsNonPositiveIntervalBeforeClusterSetup(t *testing.T) {
	t.Setenv("NODE_NAME", "worker-a")
	err := runNode([]string{"--interval=0s"})
	if err == nil || !strings.Contains(err.Error(), "interval must be positive") {
		t.Fatalf("runNode error = %v", err)
	}
}
