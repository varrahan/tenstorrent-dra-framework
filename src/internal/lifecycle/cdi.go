package lifecycle

import (
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

const (
	cdiVersion = "0.6.0"
	cdiKind    = "tenstorrent.com/accelerator"
	cdiVendor  = "tenstorrent.com"
	cdiClass   = "accelerator"
)

type cdiSpec struct {
	CDIVersion string      `json:"cdiVersion"`
	Kind       string      `json:"kind"`
	Devices    []cdiDevice `json:"devices"`
}

type cdiDevice struct {
	Name           string   `json:"name"`
	ContainerEdits cdiEdits `json:"containerEdits"`
}

type cdiEdits struct {
	DeviceNodes []cdiNode `json:"deviceNodes"`
}

type cdiNode struct {
	Path     string  `json:"path"`
	Type     string  `json:"type"`
	Major    int64   `json:"major"`
	Minor    int64   `json:"minor"`
	FileMode *uint32 `json:"fileMode,omitempty"`
}

// writeCDI creates the per-claim CDI device specification with restricted device-node access.
func (m *Manager) writeCDI(claim PreparedClaim) error {
	if err := os.MkdirAll(m.config.CDIDir, 0o755); err != nil {
		return err
	}
	spec := cdiSpec{CDIVersion: cdiVersion, Kind: cdiKind}
	for _, item := range claim.Devices {
		mode := uint32(0o600)
		spec.Devices = append(spec.Devices, cdiDevice{
			Name: cdiName(item.CDIID),
			ContainerEdits: cdiEdits{DeviceNodes: []cdiNode{{
				Path: item.Path, Type: "c", Major: int64(item.Major), Minor: int64(item.Minor), FileMode: &mode,
			}}},
		})
	}
	return atomicJSON(filepath.Join(m.config.CDIDir, cdiFilename(claim.UID)), spec, 0o644)
}

// cdiResults converts persisted claim devices into kubelet prepare results.
func cdiResults(claim PreparedClaim) []kubeletplugin.Device {
	result := make([]kubeletplugin.Device, 0, len(claim.Devices))
	for _, item := range claim.Devices {
		result = append(result, kubeletplugin.Device{
			PoolName: item.Pool, DeviceName: item.Device, CDIDeviceIDs: []string{item.CDIID},
		})
	}
	return result
}

// cdiID builds the fully qualified CDI identifier for a claim-owned device.
func cdiID(uid types.UID, device string) string {
	return cdiVendor + "/" + cdiClass + "=" + string(uid) + "-" + strings.ReplaceAll(device, "/", "-")
}

// cdiName removes the CDI kind prefix to obtain the spec-local device name.
func cdiName(id string) string {
	return strings.TrimPrefix(id, cdiKind+"=")
}

// cdiFilename derives a filesystem-safe per-claim CDI specification name.
func cdiFilename(uid types.UID) string {
	return "claim-" + strings.ReplaceAll(string(uid), "/", "-") + ".json"
}
