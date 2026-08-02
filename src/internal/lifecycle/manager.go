package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

const (
	stateVersion = 1
	cdiVersion   = "0.6.0"
	cdiKind      = "tenstorrent.com/accelerator"
	cdiVendor    = "tenstorrent.com"
	cdiClass     = "accelerator"
)

type Config struct {
	NodeName  string
	Driver    string
	StateDir  string
	CDIDir    string
	Inventory func(context.Context) (device.InventorySnapshot, error)
}

type Manager struct {
	config Config
	mu     sync.Mutex
	state  persistedState
}

type persistedState struct {
	Version int                      `json:"version"`
	Claims  map[string]PreparedClaim `json:"claims"`
}

type PreparedClaim struct {
	UID       types.UID     `json:"uid"`
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Devices   []ClaimDevice `json:"devices"`
}

type ClaimDevice struct {
	Pool   string `json:"pool"`
	Device string `json:"device"`
	Path   string `json:"path"`
	CDIID  string `json:"cdiID"`
}

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

func NewManager(config Config) (*Manager, error) {
	if strings.TrimSpace(config.NodeName) == "" || strings.TrimSpace(config.Driver) == "" {
		return nil, errors.New("node name and driver are required")
	}
	if config.Inventory == nil {
		return nil, errors.New("inventory callback is required")
	}
	if !filepath.IsAbs(config.StateDir) || !filepath.IsAbs(config.CDIDir) {
		return nil, errors.New("state and CDI directories must be absolute")
	}
	m := &Manager{config: config, state: persistedState{Version: stateVersion, Claims: map[string]PreparedClaim{}}}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, err := m.config.Inventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh inventory: %w", err)
	}
	byName := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		byName[deviceName(item.Node)] = item
	}
	result := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		prepared, prepareErr := m.prepareOne(claim, byName)
		if prepareErr != nil {
			result[claim.UID] = kubeletplugin.PrepareResult{Err: prepareErr}
			continue
		}
		result[claim.UID] = kubeletplugin.PrepareResult{Devices: prepared}
	}
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) UnprepareResourceClaims(_ context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[types.UID]error, len(claims))
	for _, claim := range claims {
		prepared, ok := m.state.Claims[string(claim.UID)]
		if !ok {
			result[claim.UID] = nil
			continue
		}
		if err := os.Remove(filepath.Join(m.config.CDIDir, cdiFilename(prepared.UID))); err != nil && !os.IsNotExist(err) {
			result[claim.UID] = err
			continue
		}
		delete(m.state.Claims, string(claim.UID))
		result[claim.UID] = nil
	}
	if err := m.persist(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) HandleError(_ context.Context, err error, msg string) {
	// The caller decides whether a helper error is fatal. Keeping this callback
	// side-effect free avoids turning a recoverable kubelet reconnect into data
	// loss or premature CDI cleanup.
	_ = fmt.Sprintf("%s: %v", msg, err)
}

func (m *Manager) prepareOne(claim *resourceapi.ResourceClaim, byName map[string]device.InventoryDevice) ([]kubeletplugin.Device, error) {
	if claim == nil || claim.Status.Allocation == nil {
		return nil, errors.New("claim has no allocation")
	}
	if existing, ok := m.state.Claims[string(claim.UID)]; ok {
		return cdiResults(existing), nil
	}
	prepared := PreparedClaim{UID: claim.UID, Namespace: claim.Namespace, Name: claim.Name}
	for _, allocation := range claim.Status.Allocation.Devices.Results {
		if allocation.Driver != m.config.Driver {
			return nil, fmt.Errorf("allocation driver %q does not match %q", allocation.Driver, m.config.Driver)
		}
		if allocation.Pool != m.config.NodeName {
			return nil, fmt.Errorf("allocation pool %q is not local node %q", allocation.Pool, m.config.NodeName)
		}
		item, ok := byName[allocation.Device]
		if !ok || !item.Eligible || !item.CharacterDevicePresent || item.Health != device.HealthHealthy {
			return nil, fmt.Errorf("allocated device %q is not healthy and available locally", allocation.Device)
		}
		for otherUID, other := range m.state.Claims {
			if otherUID == string(claim.UID) {
				continue
			}
			for _, owned := range other.Devices {
				if owned.Device == allocation.Device {
					return nil, fmt.Errorf("device %q is already prepared for claim %s", allocation.Device, otherUID)
				}
			}
		}
		id := cdiID(claim.UID, allocation.Device)
		prepared.Devices = append(prepared.Devices, ClaimDevice{Pool: allocation.Pool, Device: allocation.Device, Path: item.Node.Path, CDIID: id})
	}
	if err := m.writeCDI(prepared); err != nil {
		return nil, err
	}
	m.state.Claims[string(claim.UID)] = prepared
	return cdiResults(prepared), nil
}

func (m *Manager) writeCDI(claim PreparedClaim) error {
	if err := os.MkdirAll(m.config.CDIDir, 0o755); err != nil {
		return err
	}
	spec := cdiSpec{CDIVersion: cdiVersion, Kind: cdiKind}
	for _, item := range claim.Devices {
		spec.Devices = append(spec.Devices, cdiDevice{Name: cdiName(item.CDIID), ContainerEdits: cdiEdits{DeviceNodes: []cdiNode{{Path: item.Path, Type: "c", FileMode: ptr(uint32(0o600))}}}})
	}
	return atomicJSON(filepath.Join(m.config.CDIDir, cdiFilename(claim.UID)), spec, 0o644)
}

func (m *Manager) load() error {
	data, err := os.ReadFile(filepath.Join(m.config.StateDir, "claims.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &m.state); err != nil {
		return fmt.Errorf("decode claim state: %w", err)
	}
	if m.state.Version != stateVersion || m.state.Claims == nil {
		return fmt.Errorf("unsupported claim state version %d", m.state.Version)
	}
	return nil
}

func (m *Manager) persist() error {
	if err := os.MkdirAll(m.config.StateDir, 0o755); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(m.config.StateDir, "claims.json"), m.state, 0o600)
}

func atomicJSON(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func cdiResults(claim PreparedClaim) []kubeletplugin.Device {
	result := make([]kubeletplugin.Device, 0, len(claim.Devices))
	for _, item := range claim.Devices {
		result = append(result, kubeletplugin.Device{PoolName: item.Pool, DeviceName: item.Device, CDIDeviceIDs: []string{item.CDIID}})
	}
	return result
}

func deviceName(node device.Node) string {
	if node.ChipSeries != "" && node.CardSeries != "" {
		return "tt-" + node.ChipSeries + "-" + node.CardSeries + "-" + node.ID
	}
	return "tt-" + node.ID
}

func cdiID(uid types.UID, device string) string {
	return cdiVendor + "/" + cdiClass + "=" + string(uid) + "-" + strings.ReplaceAll(device, "/", "-")
}

func cdiName(id string) string { return strings.ReplaceAll(strings.ReplaceAll(id, "/", "-"), "=", "-") }
func cdiFilename(uid types.UID) string {
	return "claim-" + strings.ReplaceAll(string(uid), "/", "-") + ".json"
}
func ptr[T any](value T) *T { return &value }

var _ kubeletplugin.DRAPlugin = (*Manager)(nil)
