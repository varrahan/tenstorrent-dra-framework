package lifecycle

import (
	"context"
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
	Major  uint64 `json:"major"`
	Minor  uint64 `json:"minor"`
	CDIID  string `json:"cdiID"`
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
		byName[device.DRAName(item)] = item
	}
	owners := m.deviceOwners()
	result := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		uid := types.UID("")
		if claim != nil {
			uid = claim.UID
		}
		prepared, prepareErr := m.prepareOne(claim, byName, owners)
		if prepareErr != nil {
			result[uid] = kubeletplugin.PrepareResult{Err: prepareErr}
			continue
		}
		result[uid] = kubeletplugin.PrepareResult{Devices: prepared}
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

func (m *Manager) HandleError(_ context.Context, _ error, _ string) {
	// The caller decides whether a helper error is fatal. Keeping this callback
	// side-effect free avoids turning a recoverable kubelet reconnect into data
	// loss or premature CDI cleanup.
}

func (m *Manager) prepareOne(claim *resourceapi.ResourceClaim, byName map[string]device.InventoryDevice, owners map[string]string) ([]kubeletplugin.Device, error) {
	if claim == nil || claim.Status.Allocation == nil {
		return nil, errors.New("claim has no allocation")
	}
	if existing, ok := m.state.Claims[string(claim.UID)]; ok {
		return cdiResults(existing), nil
	}
	prepared := PreparedClaim{UID: claim.UID, Namespace: claim.Namespace, Name: claim.Name}
	seen := map[string]struct{}{}
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
		if _, duplicate := seen[allocation.Device]; duplicate {
			return nil, fmt.Errorf("device %q is allocated more than once", allocation.Device)
		}
		seen[allocation.Device] = struct{}{}
		if ownerUID, owned := owners[allocation.Device]; owned && ownerUID != string(claim.UID) {
			return nil, fmt.Errorf("device %q is already prepared for claim %s", allocation.Device, ownerUID)
		}
		id := cdiID(claim.UID, allocation.Device)
		prepared.Devices = append(prepared.Devices, ClaimDevice{
			Pool: allocation.Pool, Device: allocation.Device, Path: item.Node.Path,
			Major: item.Node.Major, Minor: item.Node.Minor, CDIID: id,
		})
	}
	if err := m.writeCDI(prepared); err != nil {
		return nil, err
	}
	m.state.Claims[string(claim.UID)] = prepared
	for _, item := range prepared.Devices {
		owners[item.Device] = string(claim.UID)
	}
	return cdiResults(prepared), nil
}

func (m *Manager) deviceOwners() map[string]string {
	owners := map[string]string{}
	for uid, claim := range m.state.Claims {
		for _, item := range claim.Devices {
			owners[item.Device] = uid
		}
	}
	return owners
}

var _ kubeletplugin.DRAPlugin = (*Manager)(nil)
