package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FilesystemProvider reads tt-kmd class entries, their backing PCI devices,
// and optional character-device metadata from configurable roots.
type FilesystemProvider struct {
	Roots Roots
}

// NewFilesystemProvider validates host roots and constructs a filesystem inventory provider.
func NewFilesystemProvider(roots Roots) (FilesystemProvider, error) {
	if err := roots.validate(); err != nil {
		return FilesystemProvider{}, err
	}
	return FilesystemProvider{Roots: roots}, nil
}

// Observe discovers and stably sorts all Tenstorrent devices exposed through sysfs.
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

// observeEntry collects device-node, PCI, IOMMU, and fabric data for one sysfs entry.
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
	raw.Values["kernel_version"] = kernelVersion()

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
	if raw.CharacterDevicePresent {
		if version, abi, err := readKMDInfo(raw.Node.Path); err == nil {
			if observed := firstValue(raw.Values, "kmd_version", "driver_version"); observed != "" && observed != version {
				raw.DiscoveryError = errors.Join(raw.DiscoveryError, fmt.Errorf("tt-kmd version sources disagree: %s != %s", observed, version))
			}
			if observed := raw.Values["driver_abi_version"]; observed != "" && observed != fmt.Sprint(abi) {
				raw.DiscoveryError = errors.Join(raw.DiscoveryError, fmt.Errorf("tt-kmd ABI sources disagree: %s != %d", observed, abi))
			}
			raw.Values["kmd_version"] = version
			raw.Values["driver_abi_version"] = fmt.Sprint(abi)
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
		raw.Values["pci.iommu_group"], raw.Values["pci.iommu_group_size"] = readIOMMUGroup(raw.PCIPath)
	}
	raw.FabricLinks = readFabricLinks(filepath.Join(dataPath, "fabric_links"))
	return raw
}

// kernelVersion returns the running kernel release used by compatibility policy.
func kernelVersion() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// String identifies this provider in diagnostics.
func (p FilesystemProvider) String() string { return "filesystem" }

// readDeviceValues reads only identity and health fields, avoiding hardware counters
// whose simulator-only register paths can destabilize the guest.
func readDeviceValues(root string) (map[string]string, error) {
	return readSelectedValues(root, []string{
		"uevent", "dev", "architecture", "health", "fault_code", "device_uuid", "serial_number",
		"firmware_version", "tt_fw_bundle_ver", "kmd_version", "driver_version",
		"driver_abi_version",
		"memory_capacity_bytes", "memory_available_bytes", "tensix_cores_total",
		"fabric_id", "fabric_domain", "ring_id", "fabric_ring", "fabric_endpoint",
		"fabric_endpoint_id",
	})
}

// readSelectedValues reads the available files from an explicit sysfs allowlist.
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

// readPCIValues collects the PCI identity and link fields needed by normalization.
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

// readIOMMUGroup returns a device's group number and the number of devices in that group.
func readIOMMUGroup(root string) (string, string) {
	link := filepath.Join(root, "iommu_group")
	target, err := os.Readlink(link)
	if err != nil {
		return "", ""
	}
	group := filepath.Base(filepath.Clean(target))
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return group, ""
	}
	entries, err := os.ReadDir(filepath.Join(resolved, "devices"))
	if err != nil {
		return group, ""
	}
	return group, fmt.Sprint(len(entries))
}

// resolvePCIPath resolves a device's PCI symlink while rejecting paths outside the PCI tree.
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

// readFabricLinks gathers and stably sorts the observed hardware fabric links.
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

// valuesPath extracts one key from a newline-delimited key-value sysfs file.
func valuesPath(values map[string]string, file, key string) string {
	for _, line := range strings.Split(values[file], "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// rawSortKey prefers stable PCI identity when ordering raw device observations.
func rawSortKey(raw RawDevice) string {
	if raw.PCIPath != "" {
		return raw.PCIPath
	}
	return raw.ID
}
