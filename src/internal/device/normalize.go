package device

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	tenstorrentPCIVendor = "0x1e52"
	wormholePCIDevice    = "0x401e"
	blackholePCIDevice   = "0xb140"
)

// BuildSnapshot observes, normalizes, sorts, and validates the current device inventory.
func BuildSnapshot(ctx context.Context, provider Provider) (InventorySnapshot, error) {
	if provider == nil {
		return InventorySnapshot{}, errors.New("inventory provider is nil")
	}
	observations, err := provider.Observe(ctx)
	if err != nil {
		return InventorySnapshot{}, err
	}

	observedAt := time.Now().UTC()
	devices := make([]InventoryDevice, 0, len(observations))
	for _, observation := range observations {
		devices = append(devices, normalize(observation, observedAt))
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].StableID < devices[j].StableID })
	markDuplicateIdentities(devices)
	return InventorySnapshot{Devices: devices, ObservedAt: observedAt}, nil
}

// markDuplicateIdentities makes every device sharing a stable identity ineligible.
func markDuplicateIdentities(devices []InventoryDevice) {
	for i := 1; i < len(devices); i++ {
		if devices[i].StableID != devices[i-1].StableID {
			continue
		}
		devices[i-1].Eligible = false
		devices[i-1].RejectionReason = "duplicate stable identity"
		devices[i].Eligible = false
		devices[i].RejectionReason = "duplicate stable identity"
	}
}

// normalize converts one provider observation into the canonical inventory model.
func normalize(raw RawDevice, observedAt time.Time) InventoryDevice {
	pci := pciIdentity(raw)
	chipSeries := normalizeChip(raw.Values["architecture"])
	if chipSeries == "" {
		chipSeries = chipSeriesFromPCI(pci)
	}
	device := InventoryDevice{
		StableID:               raw.ID,
		Node:                   raw.Node,
		CharacterDevicePresent: raw.CharacterDevicePresent,
		PCI:                    pci,
		ChipSeries:             chipSeries,
		FirmwareVersion:        firstValue(raw.Values, "tt_fw_bundle_ver", "firmware_version"),
		KMDVersion:             firstValue(raw.Values, "kmd_version", "driver_version"),
		Memory: MemoryInfo{
			TotalBytes:     parseUint(raw.Values["memory_capacity_bytes"]),
			AvailableBytes: parseUint(raw.Values["memory_available_bytes"]),
		},
		Compute: ComputeInfo{TensixCores: parseUint(raw.Values["tensix_cores_total"])},
		Health:  normalizeHealth(raw.Values["health"]),
		Fault:   FaultInfo{Code: raw.Values["fault_code"]},
		Fabric: FabricInfo{
			FabricID:   firstValue(raw.Values, "fabric_id", "fabric_domain"),
			RingID:     firstValue(raw.Values, "ring_id", "fabric_ring"),
			EndpointID: firstValue(raw.Values, "fabric_endpoint", "fabric_endpoint_id"),
			Links:      raw.FabricLinks,
		},
		Provenance: map[string]Provenance{},
		ObservedAt: observedAt,
	}
	if device.PCI.BDF != "" {
		device.StableID = "pci-" + device.PCI.BDF
	}
	if device.Fault.Code != "" && device.Fault.Code != "0" {
		device.Fault.Message = "non-zero hardware fault code"
		if device.Health == HealthHealthy {
			device.Health = HealthUnhealthy
		}
	}
	device.Eligible, device.RejectionReason = eligibility(device, raw.DiscoveryError)
	device.Node.ChipSeries = device.ChipSeries
	populateProvenance(&device, raw, observedAt)
	return device
}

// populateProvenance records the source path and observation time for inventory fields.
func populateProvenance(device *InventoryDevice, raw RawDevice, observedAt time.Time) {
	add := func(field, source, path string) {
		device.Provenance[field] = Provenance{Source: source, Path: path, ObservedAt: observedAt}
	}
	add("stableID", "pci-sysfs", raw.PCIPath)
	add("characterDevice", "device-sysfs", raw.SysfsPath)
	add("pci", "pci-sysfs", raw.PCIPath)
	for _, field := range []string{"chipSeries", "firmwareVersion", "kmdVersion", "memory", "compute", "health", "fault", "fabric"} {
		add(field, "tenstorrent-sysfs", raw.SysfsPath)
	}
	if strings.TrimSpace(raw.Values["architecture"]) == "" && device.ChipSeries != "" {
		add("chipSeries", "pci-sysfs", raw.PCIPath)
	}
	add("node", "device-sysfs", raw.SysfsPath)
	add("observedAt", "observer", raw.SysfsPath)
}

// eligibility applies fail-closed hardware identity and health requirements.
func eligibility(device InventoryDevice, discoveryErr error) (bool, string) {
	switch {
	case discoveryErr != nil:
		return false, "discovery: " + discoveryErr.Error()
	case !device.CharacterDevicePresent:
		return false, "character device is missing"
	case device.PCI.Vendor != "" && device.PCI.Vendor != tenstorrentPCIVendor:
		return false, "PCI vendor is not Tenstorrent"
	case device.PCI.BDF == "" || device.PCI.Vendor == "":
		return false, "PCI identity is incomplete"
	case device.ChipSeries != "wormhole" && device.ChipSeries != "blackhole":
		return false, "chip identity is not Wormhole or Blackhole"
	case device.Health == HealthUnhealthy:
		return false, "device health is " + string(device.Health)
	default:
		return true, ""
	}
}

// pciIdentity builds normalized PCI and isolation metadata from raw sysfs values.
func pciIdentity(raw RawDevice) PCIIdentity {
	return PCIIdentity{
		BDF:             firstValue(raw.Values, "pci.uevent.PCI_SLOT_NAME", "pci.PCI_SLOT_NAME", "pci.bdf"),
		Vendor:          normalizeHex(raw.Values["pci.vendor"]),
		Device:          normalizeHex(raw.Values["pci.device"]),
		SubsystemVendor: normalizeHex(raw.Values["pci.subsystem_vendor"]),
		SubsystemDevice: normalizeHex(raw.Values["pci.subsystem_device"]),
		Revision:        normalizeHex(raw.Values["pci.revision"]),
		NUMANode:        parseOptionalInt(raw.Values["pci.numa_node"]),
		IOMMUGroup:      parseOptionalInt(raw.Values["pci.iommu_group"]),
		IOMMUGroupSize:  parseOptionalInt(raw.Values["pci.iommu_group_size"]),
		LinkState:       firstValue(raw.Values, "pci.current_link_state", "pci.link_state"),
		LinkSpeed:       raw.Values["pci.current_link_speed"],
		LinkWidth:       parseInt(raw.Values["pci.current_link_width"]),
	}
}

// firstValue returns the first non-empty value among equivalent observation keys.
func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

// normalizeChip converts chip aliases into canonical series names.
func normalizeChip(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "wormhole", "wh":
		return "wormhole"
	case "blackhole", "bh":
		return "blackhole"
	default:
		return normalized
	}
}

// chipSeriesFromPCI infers a supported chip series from a known PCI device ID.
func chipSeriesFromPCI(identity PCIIdentity) string {
	if identity.Vendor != tenstorrentPCIVendor {
		return ""
	}
	switch identity.Device {
	case wormholePCIDevice:
		return "wormhole"
	case blackholePCIDevice:
		return "blackhole"
	default:
		return ""
	}
}

// normalizeHealth maps provider-specific health strings into the canonical states.
func normalizeHealth(value string) HealthState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy", "ok", "ready", "available":
		return HealthHealthy
	case "unhealthy", "failed", "fault", "offline":
		return HealthUnhealthy
	default:
		return HealthUnknown
	}
}

// normalizeHex converts hexadecimal identifiers into a lowercase 0x-prefixed form.
func normalizeHex(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || strings.HasPrefix(value, "0x") {
		return value
	}
	if parsed, err := strconv.ParseUint(value, 16, 64); err == nil {
		return fmt.Sprintf("0x%x", parsed)
	}
	return value
}

// parseUint parses decimal or 0x-prefixed unsigned values, returning zero on failure.
func parseUint(value string) uint64 {
	value = strings.TrimSpace(value)
	base := 10
	if strings.HasPrefix(value, "0x") {
		base = 16
		value = strings.TrimPrefix(value, "0x")
	}
	parsed, _ := strconv.ParseUint(value, base, 64)
	return parsed
}

// parseInt parses a decimal integer, returning zero on failure.
func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

// parseOptionalInt distinguishes a missing integer with -1 and otherwise parses it.
func parseOptionalInt(value string) int {
	if strings.TrimSpace(value) == "" {
		return -1
	}
	return parseInt(value)
}

// parseDeviceNumbers parses a sysfs major:minor device-number pair.
func parseDeviceNumbers(value string) (uint64, uint64) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	major, _ := strconv.ParseUint(parts[0], 10, 64)
	minor, _ := strconv.ParseUint(parts[1], 10, 64)
	return major, minor
}

// DRAName returns the stable DNS-label name used in ResourceSlices and claims.
func DRAName(item InventoryDevice) string {
	if item.StableID == "" && item.Node.ChipSeries != "" {
		return "tt-" + item.Node.ChipSeries + "-" + item.Node.ID
	}
	value := strings.ToLower(item.StableID)
	if value == "" {
		value = item.Node.ID
	}
	var name strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			name.WriteRune(char)
		} else {
			name.WriteByte('-')
		}
	}
	result := strings.Trim(name.String(), "-")
	if len(result) > 59 {
		result = result[:59]
	}
	return "tt-" + result
}
