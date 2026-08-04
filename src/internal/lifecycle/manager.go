package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

type Config struct {
	NodeName     string
	Driver       string
	StateDir     string
	CDIDir       string
	RequireIOMMU bool
	Inventory    func(context.Context) (device.InventorySnapshot, error)
	Resetter     Resetter
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

// NewManager validates lifecycle dependencies and restores persisted allocation state.
func NewManager(config Config) (*Manager, error) {
	if strings.TrimSpace(config.NodeName) == "" || strings.TrimSpace(config.Driver) == "" {
		return nil, errors.New("node name and driver are required")
	}
	if config.Inventory == nil {
		return nil, errors.New("inventory callback is required")
	}
	if config.Resetter == nil {
		return nil, errors.New("resetter is required")
	}
	if !filepath.IsAbs(config.StateDir) || !filepath.IsAbs(config.CDIDir) {
		return nil, errors.New("state and CDI directories must be absolute")
	}
	m := &Manager{config: config, state: persistedState{
		Version: stateVersion, Claims: map[string]PreparedClaim{},
		Quarantined: map[string]QuarantineRecord{}, Known: map[string]KnownDevice{},
	}}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// PrepareResourceClaims sanitizes, verifies, and exposes each locally allocated device.
func (m *Manager) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, err := m.config.Inventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh inventory: %w", err)
	}
	if _, _, err := m.observeLocked(ctx, snapshot, false); err != nil {
		return nil, fmt.Errorf("monitor inventory: %w", err)
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
		prepared, prepareErr := m.prepareOne(ctx, claim, byName, owners)
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

// UnprepareResourceClaims sanitizes released devices before removing their CDI and ownership state.
func (m *Manager) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, err := m.config.Inventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh inventory: %w", err)
	}
	if _, _, err := m.observeLocked(ctx, snapshot, false); err != nil {
		return nil, fmt.Errorf("monitor inventory: %w", err)
	}
	byName := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		byName[device.DRAName(item)] = item
	}
	result := make(map[types.UID]error, len(claims))
	for _, claim := range claims {
		prepared, ok := m.state.Claims[string(claim.UID)]
		if !ok {
			result[claim.UID] = nil
			continue
		}
		var sanitizeErr error
		for _, claimedDevice := range prepared.Devices {
			item, found := byName[claimedDevice.Device]
			if !found || !item.CharacterDevicePresent {
				reason := "allocated device is unavailable during post-use sanitization"
				sanitizeErr = errors.Join(sanitizeErr, fmt.Errorf("device %q: %s", claimedDevice.Device, reason))
				sanitizeErr = errors.Join(sanitizeErr, m.quarantineLocked(claimedDevice.Device, claimedDevice.Path, reason, &prepared, "postflight-sanitize"))
				continue
			}
			sanitizeErr = errors.Join(sanitizeErr, m.sanitizeLocked(ctx, "postflight-sanitize", claimedDevice.Device, item.Node.Path, &prepared))
		}
		if sanitizeErr != nil {
			result[claim.UID] = sanitizeErr
			continue
		}
		if err := m.auditLocked(AuditEvent{Action: "claim-release", Outcome: "approved"}, &prepared); err != nil {
			for _, claimedDevice := range prepared.Devices {
				m.state.Quarantined[claimedDevice.Device] = QuarantineRecord{Reason: "claim release audit failed: " + err.Error(), Since: time.Now().UTC()}
			}
			result[claim.UID] = err
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

// HandleError leaves recoverable kubelet helper errors to the caller without mutating state.
func (m *Manager) HandleError(_ context.Context, _ error, _ string) {
	// The caller decides whether a helper error is fatal. Keeping this callback
	// side-effect free avoids turning a recoverable kubelet reconnect into data
	// loss or premature CDI cleanup.
}

// prepareOne validates one allocation, performs preflight sanitization, and writes its CDI spec.
func (m *Manager) prepareOne(ctx context.Context, claim *resourceapi.ResourceClaim, byName map[string]device.InventoryDevice, owners map[string]string) ([]kubeletplugin.Device, error) {
	if claim == nil || claim.Status.Allocation == nil {
		return nil, errors.New("claim has no allocation")
	}
	if existing, ok := m.state.Claims[string(claim.UID)]; ok {
		for _, claimedDevice := range existing.Devices {
			item, found := byName[claimedDevice.Device]
			if !found || m.deviceUnsafeReason(item) != "" {
				return nil, fmt.Errorf("prepared device %q is no longer healthy", claimedDevice.Device)
			}
			if record, quarantined := m.state.Quarantined[claimedDevice.Device]; quarantined {
				return nil, fmt.Errorf("prepared device %q is quarantined: %s", claimedDevice.Device, record.Reason)
			}
		}
		return cdiResults(existing), nil
	}
	if len(claim.Status.Allocation.Devices.Results) == 0 {
		return nil, errors.New("claim allocation has no devices")
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
		if !ok || !item.CharacterDevicePresent {
			return nil, fmt.Errorf("allocated device %q is not available locally", allocation.Device)
		}
		if m.config.RequireIOMMU && (item.PCI.IOMMUGroup < 0 || item.PCI.IOMMUGroupSize != 1) {
			return nil, fmt.Errorf("allocated device %q has no dedicated IOMMU group", allocation.Device)
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
	for _, claimedDevice := range prepared.Devices {
		if err := m.sanitizeLocked(ctx, "preflight-sanitize", claimedDevice.Device, claimedDevice.Path, &prepared); err != nil {
			return nil, fmt.Errorf("sanitize device %q: %w", claimedDevice.Device, err)
		}
	}
	refreshed, err := m.config.Inventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("verify sanitized inventory: %w", err)
	}
	filtered, _, err := m.observeLocked(ctx, refreshed, false)
	if err != nil {
		return nil, fmt.Errorf("verify sanitized inventory: %w", err)
	}
	verified := make(map[string]device.InventoryDevice, len(filtered.Devices))
	for _, item := range filtered.Devices {
		verified[device.DRAName(item)] = item
	}
	for _, claimedDevice := range prepared.Devices {
		item, found := verified[claimedDevice.Device]
		if !found || !item.Eligible || item.Health != device.HealthHealthy {
			return nil, fmt.Errorf("sanitized device %q did not return healthy", claimedDevice.Device)
		}
	}
	if err := m.writeCDI(prepared); err != nil {
		return nil, err
	}
	if err := m.auditLocked(AuditEvent{Action: "claim-prepare", Outcome: "approved"}, &prepared); err != nil {
		_ = os.Remove(filepath.Join(m.config.CDIDir, cdiFilename(prepared.UID)))
		for _, claimedDevice := range prepared.Devices {
			m.state.Quarantined[claimedDevice.Device] = QuarantineRecord{Reason: "claim preparation audit failed: " + err.Error(), Since: time.Now().UTC()}
		}
		return nil, err
	}
	m.state.Claims[string(claim.UID)] = prepared
	for _, item := range prepared.Devices {
		owners[item.Device] = string(claim.UID)
	}
	return cdiResults(prepared), nil
}

// deviceOwners indexes prepared devices by the claim UID that currently owns them.
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
