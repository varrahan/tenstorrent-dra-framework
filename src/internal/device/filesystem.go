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
