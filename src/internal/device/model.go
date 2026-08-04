package device

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Roots describes the host paths shared by inventory and lifecycle components.
type Roots struct {
	DeviceRoot           string
	TenstorrentSysfsRoot string
	PCISysfsRoot         string
	StateDir             string
}

func DefaultRoots() Roots {
	return Roots{
		DeviceRoot:           "/dev/tenstorrent",
		TenstorrentSysfsRoot: "/sys/class/tenstorrent",
		PCISysfsRoot:         "/sys/bus/pci/devices",
		StateDir:             "/var/lib/tenstorrent-dra",
	}
}

func (r Roots) validate() error {
	paths := map[string]string{
		"device root":            r.DeviceRoot,
		"Tenstorrent sysfs root": r.TenstorrentSysfsRoot,
		"PCI sysfs root":         r.PCISysfsRoot,
		"state directory":        r.StateDir,
	}
	for name, value := range paths {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be empty", name)
		}
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be absolute: %q", name, value)
		}
	}
	return nil
}

type Provenance struct {
	Source     string    `json:"source"`
	Path       string    `json:"path,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

type HealthState string

const (
	HealthHealthy   HealthState = "Healthy"
	HealthUnhealthy HealthState = "Unhealthy"
	HealthUnknown   HealthState = "Unknown"
)

type PCIIdentity struct {
	BDF             string `json:"bdf,omitempty"`
	Vendor          string `json:"vendor,omitempty"`
	Device          string `json:"device,omitempty"`
	SubsystemVendor string `json:"subsystemVendor,omitempty"`
	SubsystemDevice string `json:"subsystemDevice,omitempty"`
	Revision        string `json:"revision,omitempty"`
	NUMANode        int    `json:"numaNode"`
	LinkState       string `json:"linkState,omitempty"`
	LinkSpeed       string `json:"linkSpeed,omitempty"`
	LinkWidth       int    `json:"linkWidth,omitempty"`
}

type MemoryInfo struct {
	TotalBytes     uint64 `json:"totalBytes,omitempty"`
	AvailableBytes uint64 `json:"availableBytes,omitempty"`
}

type ComputeInfo struct {
	TensixCores uint64 `json:"tensixCores,omitempty"`
}

type FaultInfo struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type FabricLink struct {
	Name             string `json:"name"`
	State            string `json:"state,omitempty"`
	SpeedGbps        uint64 `json:"speedGbps,omitempty"`
	RemoteBDF        string `json:"remoteBdf,omitempty"`
	RemoteEndpointID string `json:"remoteEndpointID,omitempty"`
}

type FabricInfo struct {
	FabricID   string       `json:"fabricID,omitempty"`
	RingID     string       `json:"ringID,omitempty"`
	EndpointID string       `json:"endpointID,omitempty"`
	Links      []FabricLink `json:"links,omitempty"`
}

// InventoryDevice is the scheduler-independent device model. Path and device
// numbers are local runtime data and must not become scheduling policy.
type InventoryDevice struct {
	StableID               string                `json:"stableID"`
	Node                   Node                  `json:"node"`
	CharacterDevicePresent bool                  `json:"characterDevicePresent"`
	PCI                    PCIIdentity           `json:"pci"`
	ChipSeries             string                `json:"chipSeries,omitempty"`
	FirmwareVersion        string                `json:"firmwareVersion,omitempty"`
	KMDVersion             string                `json:"kmdVersion,omitempty"`
	Memory                 MemoryInfo            `json:"memory"`
	Compute                ComputeInfo           `json:"compute"`
	Health                 HealthState           `json:"health"`
	Fault                  FaultInfo             `json:"fault,omitempty"`
	Fabric                 FabricInfo            `json:"fabric"`
	Provenance             map[string]Provenance `json:"provenance"`
	ObservedAt             time.Time             `json:"observedAt"`
	Eligible               bool                  `json:"eligible"`
	RejectionReason        string                `json:"rejectionReason,omitempty"`
}

type InventorySnapshot struct {
	Devices    []InventoryDevice `json:"devices"`
	ObservedAt time.Time         `json:"observedAt"`
}

// RawDevice is a provider-neutral observation passed to normalization.
type RawDevice struct {
	ID                     string
	Node                   Node
	CharacterDevicePresent bool
	SysfsPath              string
	PCIPath                string
	Values                 map[string]string
	FabricLinks            []FabricLink
	DiscoveryError         error
}

type Provider interface {
	Observe(context.Context) ([]RawDevice, error)
}

type StaticProvider struct {
	Devices []RawDevice
}

func (p StaticProvider) Observe(context.Context) ([]RawDevice, error) {
	return append([]RawDevice(nil), p.Devices...), nil
}
