package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const tenstorrentPCIVendor = "0x1e52"

// Roots describes the host paths used by the inventory provider. StateDir is
// part of the configuration contract even though inventory itself does not
// write state; lifecycle components use the same validated roots.
type Roots struct {
	DeviceRoot           string
	TenstorrentSysfsRoot string
	PCISysfsRoot         string
	StateDir             string
}

// DefaultRoots returns portable Linux defaults. VM-specific paths are never
// embedded in production inventory code.
func DefaultRoots() Roots {
	return Roots{
		DeviceRoot:           "/dev/tenstorrent",
		TenstorrentSysfsRoot: "/sys/class/tenstorrent",
		PCISysfsRoot:         "/sys/bus/pci/devices",
		StateDir:             "/var/lib/tenstorrent-dra",
	}
}

func (r Roots) validate() error {
	for name, value := range map[string]string{
		"device root":            r.DeviceRoot,
		"Tenstorrent sysfs root": r.TenstorrentSysfsRoot,
		"PCI sysfs root":         r.PCISysfsRoot,
		"state directory":        r.StateDir,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be empty", name)
		}
		if !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be absolute: %q", name, value)
		}
	}
	return nil
}

// Provenance identifies where a canonical field came from.
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

// InventoryDevice is the canonical scheduler-independent device model. Path
// and major/minor are local runtime data; callers must not turn Path into a
// scheduling policy.
type InventoryDevice struct {
	StableID               string                `json:"stableID"`
	Node                   Node                  `json:"node"`
	CharacterDevicePresent bool                  `json:"characterDevicePresent"`
	PCI                    PCIIdentity           `json:"pci"`
	ChipSeries             string                `json:"chipSeries,omitempty"`
	CardSeries             string                `json:"cardSeries,omitempty"`
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

// RawDevice is the provider-neutral observation passed to the normalizer.
// Providers may be backed by real sysfs, a fixture, or a future hardware API.
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

// Provider supplies observations without coupling the normalizer to a
// filesystem layout.
type Provider interface {
	Observe(context.Context) ([]RawDevice, error)
}

// StaticProvider is useful for deterministic unit and fixture tests.
type StaticProvider struct {
	Devices []RawDevice
}

func (p StaticProvider) Observe(context.Context) ([]RawDevice, error) {
	devices := make([]RawDevice, len(p.Devices))
	copy(devices, p.Devices)
	return devices, nil
}

// FilesystemProvider reads tt-kmd class entries, their backing PCI devices,
// and optional character-device metadata from configurable roots.
type FilesystemProvider struct {
	Roots Roots
}

func NewFilesystemProvider(roots Roots) (FilesystemProvider, error) {
	if err := roots.validate(); err != nil {
		return FilesystemProvider{}, err
	}
	return FilesystemProvider{Roots: roots}, nil
}

func (p FilesystemProvider) Observe(ctx context.Context) ([]RawDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p.Roots.TenstorrentSysfsRoot)
	if err != nil {
		return nil, fmt.Errorf("read Tenstorrent sysfs root %q: %w", p.Roots.TenstorrentSysfsRoot, err)
	}

	devices := make([]RawDevice, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type()&os.ModeSymlink == 0 && !entry.IsDir() {
			continue
		}
		devices = append(devices, p.observeEntry(entry.Name()))
	}
	sort.Slice(devices, func(i, j int) bool {
		left, right := rawSortKey(devices[i]), rawSortKey(devices[j])
		if left == right {
			return devices[i].ID < devices[j].ID
		}
		return left < right
	})
	return devices, nil
}

func (p FilesystemProvider) observeEntry(id string) RawDevice {
	sysfsPath := filepath.Join(p.Roots.TenstorrentSysfsRoot, id)
	raw := RawDevice{ID: id, SysfsPath: sysfsPath, Values: map[string]string{}}
	dataPath, err := filepath.EvalSymlinks(sysfsPath)
	if err != nil {
		raw.DiscoveryError = fmt.Errorf("resolve Tenstorrent sysfs entry %q: %w", sysfsPath, err)
		return raw
	}
	if values, err := readDeviceValues(dataPath); err != nil {
		raw.DiscoveryError = err
		return raw
	} else {
		raw.Values = values
	}

	raw.Node.ID = id
	raw.Node.Path = valuesPath(raw.Values, "uevent", "DEVNAME")
	if raw.Node.Path == "" {
		raw.Node.Path = filepath.Join(p.Roots.DeviceRoot, id)
	} else if !filepath.IsAbs(raw.Node.Path) {
		raw.Node.Path = filepath.Join("/dev", raw.Node.Path)
	}
	if info, err := os.Lstat(raw.Node.Path); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		if discovered, ok, discoverErr := classifyCharacterDevice(raw.Node.Path, info); discoverErr != nil {
			raw.DiscoveryError = discoverErr
		} else if ok {
			raw.Node = discovered
			raw.CharacterDevicePresent = true
		}
	}
	if dev := raw.Values["dev"]; dev != "" && raw.Node.Major == 0 && raw.Node.Minor == 0 {
		raw.Node.Major, raw.Node.Minor = parseDeviceNumbers(dev)
	}

	linkPath := filepath.Join(dataPath, "device")
	priorError := raw.DiscoveryError
	var resolveErr error
	raw.PCIPath, resolveErr = resolvePCIPath(linkPath, p.Roots.PCISysfsRoot)
	if resolveErr != nil {
		raw.DiscoveryError = resolveErr
		return raw
	}
	raw.DiscoveryError = priorError
	if raw.PCIPath != "" {
		for key, value := range readPCIValues(raw.PCIPath) {
			raw.Values["pci."+key] = value
		}
	}
	raw.FabricLinks = readFabricLinks(filepath.Join(dataPath, "fabric_links"))
	return raw
}

func (p FilesystemProvider) String() string { return "filesystem" }

func BuildSnapshot(ctx context.Context, provider Provider) (InventorySnapshot, error) {
	if provider == nil {
		return InventorySnapshot{}, errors.New("inventory provider is nil")
	}
	observations, err := provider.Observe(ctx)
	if err != nil {
		return InventorySnapshot{}, err
	}
	now := time.Now().UTC()
	devices := make([]InventoryDevice, 0, len(observations))
	for _, observation := range observations {
		devices = append(devices, normalize(observation, now))
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].StableID < devices[j].StableID })
	for i := 1; i < len(devices); i++ {
		if devices[i].StableID == devices[i-1].StableID {
			devices[i-1].Eligible = false
			devices[i-1].RejectionReason = "duplicate stable identity"
			devices[i].Eligible = false
			devices[i].RejectionReason = "duplicate stable identity"
		}
	}
	return InventorySnapshot{Devices: devices, ObservedAt: now}, nil
}

func normalize(raw RawDevice, observedAt time.Time) InventoryDevice {
	device := InventoryDevice{
		StableID:               raw.ID,
		Node:                   raw.Node,
		CharacterDevicePresent: raw.CharacterDevicePresent,
		PCI:                    pciIdentity(raw),
		ChipSeries:             normalizeChip(raw.Values["architecture"]),
		CardSeries:             normalizeCard(raw.Values["board_type"]),
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
	device.Node.CardSeries = device.CardSeries
	populateProvenance(&device, raw, observedAt)
	return device
}

func populateProvenance(device *InventoryDevice, raw RawDevice, observedAt time.Time) {
	devicePath := raw.SysfsPath
	pciPath := raw.PCIPath
	add := func(field, source, path string) {
		device.Provenance[field] = Provenance{Source: source, Path: path, ObservedAt: observedAt}
	}
	add("stableID", "pci-sysfs", pciPath)
	add("characterDevice", "device-sysfs", devicePath)
	add("pci", "pci-sysfs", pciPath)
	add("chipSeries", "tenstorrent-sysfs", devicePath)
	add("cardSeries", "tenstorrent-sysfs", devicePath)
	add("firmwareVersion", "tenstorrent-sysfs", devicePath)
	add("kmdVersion", "tenstorrent-sysfs", devicePath)
	add("memory", "tenstorrent-sysfs", devicePath)
	add("compute", "tenstorrent-sysfs", devicePath)
	add("health", "tenstorrent-sysfs", devicePath)
	add("fault", "tenstorrent-sysfs", devicePath)
	add("fabric", "tenstorrent-sysfs", devicePath)
	add("node", "device-sysfs", devicePath)
	add("observedAt", "observer", devicePath)
}

func eligibility(device InventoryDevice, discoveryErr error) (bool, string) {
	if discoveryErr != nil {
		return false, "discovery: " + discoveryErr.Error()
	}
	if !device.CharacterDevicePresent {
		return false, "character device is missing"
	}
	if device.PCI.Vendor != "" && device.PCI.Vendor != tenstorrentPCIVendor {
		return false, "PCI vendor is not Tenstorrent"
	}
	if device.PCI.BDF == "" || device.PCI.Vendor == "" {
		return false, "PCI identity is incomplete"
	}
	if device.ChipSeries == "" || device.CardSeries == "" {
		return false, "chip/card identity is incomplete"
	}
	if device.Health == HealthUnhealthy {
		return false, "device health is " + string(device.Health)
	}
	return true, ""
}

func pciIdentity(raw RawDevice) PCIIdentity {
	return PCIIdentity{
		BDF:             firstValue(raw.Values, "pci.uevent.PCI_SLOT_NAME", "pci.PCI_SLOT_NAME", "pci.bdf"),
		Vendor:          normalizeHex(raw.Values["pci.vendor"]),
		Device:          normalizeHex(raw.Values["pci.device"]),
		SubsystemVendor: normalizeHex(raw.Values["pci.subsystem_vendor"]),
		SubsystemDevice: normalizeHex(raw.Values["pci.subsystem_device"]),
		Revision:        normalizeHex(raw.Values["pci.revision"]),
		NUMANode:        parseOptionalInt(raw.Values["pci.numa_node"]),
		LinkState:       firstValue(raw.Values, "pci.current_link_state", "pci.link_state"),
		LinkSpeed:       raw.Values["pci.current_link_speed"],
		LinkWidth:       parseInt(raw.Values["pci.current_link_width"]),
	}
}

// readDeviceValues intentionally reads an allowlist. Some tt-kmd sysfs nodes
// are hardware counters; reading them recursively can enter simulator-only
// register paths and destabilize the guest. Discovery needs identity and
// health metadata, not every exported sysfs file.
func readDeviceValues(root string) (map[string]string, error) {
	return readSelectedValues(root, []string{
		"uevent", "dev", "architecture", "board_type", "health", "fault_code",
		"firmware_version", "tt_fw_bundle_ver", "kmd_version", "driver_version",
		"memory_capacity_bytes", "memory_available_bytes", "tensix_cores_total",
		"fabric_id", "fabric_domain", "ring_id", "fabric_ring", "fabric_endpoint",
		"fabric_endpoint_id",
	})
}

func readSelectedValues(root string, names []string) (map[string]string, error) {
	values := map[string]string{}
	for _, name := range names {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			continue
		}
		values[name] = strings.TrimSpace(string(data))
	}
	return values, nil
}

func readPCIValues(root string) map[string]string {
	values, _ := readSelectedValues(root, []string{
		"uevent", "PCI_SLOT_NAME", "vendor", "device", "subsystem_vendor", "subsystem_device",
		"revision", "numa_node", "current_link_state", "current_link_speed",
		"current_link_width", "link_state",
	})
	if uevent := values["uevent"]; uevent != "" {
		for _, line := range strings.Split(uevent, "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				values["uevent."+parts[0]] = strings.TrimSpace(parts[1])
			}
		}
	}
	if values["PCI_SLOT_NAME"] != "" {
		values["PCI_SLOT_NAME"] = strings.TrimSpace(values["PCI_SLOT_NAME"])
	}
	return values
}

func resolvePCIPath(linkPath, pciRoot string) (string, error) {
	info, err := os.Lstat(linkPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("PCI backing link %q is missing", linkPath)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("PCI backing path %q is not a symlink", linkPath)
	}
	target, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return "", fmt.Errorf("resolve PCI backing link %q: %w", linkPath, err)
	}
	root, err := filepath.EvalSymlinks(pciRoot)
	if err != nil {
		return "", fmt.Errorf("resolve PCI root %q: %w", pciRoot, err)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// /sys/bus/pci/devices is commonly a symlinked view of the canonical
		// /sys/devices tree. Validate the BDF through the caller's PCI root
		// before accepting the canonical target.
		candidate, candidateErr := filepath.EvalSymlinks(filepath.Join(pciRoot, filepath.Base(target)))
		if candidateErr != nil || candidate != target {
			return "", fmt.Errorf("PCI backing path %q escapes PCI root %q", target, root)
		}
	}
	return target, nil
}

func readFabricLinks(root string) []FabricLink {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	links := make([]FabricLink, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		values, err := readSelectedValues(filepath.Join(root, entry.Name()), []string{"state", "speed_gbps", "remote_bdf", "remote_endpoint_id"})
		if err != nil {
			continue
		}
		links = append(links, FabricLink{
			Name:             entry.Name(),
			State:            values["state"],
			SpeedGbps:        parseUint(values["speed_gbps"]),
			RemoteBDF:        values["remote_bdf"],
			RemoteEndpointID: values["remote_endpoint_id"],
		})
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Name < links[j].Name })
	return links
}

func valuesPath(values map[string]string, file, key string) string {
	for _, line := range strings.Split(values[file], "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func rawSortKey(raw RawDevice) string {
	if raw.PCIPath != "" {
		return raw.PCIPath
	}
	return raw.ID
}

func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func normalizeChip(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "wormhole", "wh":
		return "wormhole"
	case "blackhole", "bh":
		return "blackhole"
	default:
		return ""
	}
}

func normalizeCard(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, suffix := range []string{"a", "b", "d", "s"} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSuffix(value, suffix)
			break
		}
	}
	if value == "n150" || value == "n300" || value == "p100" || value == "p150" {
		return value
	}
	return ""
}

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

func normalizeHex(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "0x") {
		if parsed, err := strconv.ParseUint(value, 16, 64); err == nil {
			return fmt.Sprintf("0x%x", parsed)
		}
	}
	return value
}

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

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func parseOptionalInt(value string) int {
	if strings.TrimSpace(value) == "" {
		return -1
	}
	return parseInt(value)
}

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
	if item.StableID == "" && item.Node.ChipSeries != "" && item.Node.CardSeries != "" {
		return "tt-" + item.Node.ChipSeries + "-" + item.Node.CardSeries + "-" + item.Node.ID
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
