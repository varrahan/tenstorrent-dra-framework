package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"github.com/varrahan/tenstorrent-dra-framework/src/internal/observability"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
)

type Config struct {
	NodeName        string
	Driver          string
	StateDir        string
	CDIDir          string
	RequireIOMMU    bool
	MaxInventoryAge time.Duration
	Inventory       func(context.Context) (device.InventorySnapshot, error)
	Allocations     func(context.Context) ([]*resourceapi.ResourceClaim, error)
	Resetter        Resetter
	Metrics         *observability.Metrics
	Logger          *slog.Logger
	EventSink       func(AuditEvent, *PreparedClaim)
}

type Manager struct {
	config       Config
	mu           sync.Mutex
	state        persistedState
	lastSnapshot device.InventorySnapshot
	lockFile     *os.File
}

type ClaimPhase string

const (
	ClaimPreparing ClaimPhase = "Preparing"
	ClaimPrepared  ClaimPhase = "Prepared"
	ClaimReleasing ClaimPhase = "Releasing"
	ClaimRecovered ClaimPhase = "Recovered"
)

type PreparedClaim struct {
	UID       types.UID     `json:"uid"`
	Namespace string        `json:"namespace"`
	Name      string        `json:"name"`
	Phase     ClaimPhase    `json:"phase"`
	Devices   []ClaimDevice `json:"devices"`
}

type ClaimDevice struct {
	Pool     string `json:"pool"`
	Device   string `json:"device"`
	StableID string `json:"stableID"`
	Path     string `json:"path"`
	Major    uint64 `json:"major"`
	Minor    uint64 `json:"minor"`
	CDIID    string `json:"cdiID"`
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
	if config.MaxInventoryAge < 0 {
		return nil, errors.New("maximum inventory age must not be negative")
	}
	if config.MaxInventoryAge == 0 {
		config.MaxInventoryAge = 2 * time.Minute
	}
	if config.Logger == nil {
		config.Logger = slog.Default().With("component", "node", "node", config.NodeName)
	}
	m := &Manager{config: config, state: persistedState{
		Version: stateVersion, Claims: map[string]PreparedClaim{},
		Quarantined: map[string]QuarantineRecord{}, Known: map[string]KnownDevice{},
	}}
	if err := m.acquireLock(); err != nil {
		return nil, err
	}
	if err := m.load(); err != nil {
		if recoverErr := m.recoverCorruptState(err); recoverErr != nil {
			m.Close()
			return nil, recoverErr
		}
	}
	if config.Allocations != nil || len(m.state.Claims) > 0 {
		if err := m.reconcileStartup(context.Background()); err != nil {
			m.Close()
			return nil, err
		}
	}
	return m, nil
}

// PrepareResourceClaims sanitizes, verifies, and exposes each locally allocated device.
func (m *Manager) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	refreshStarted := time.Now()
	snapshot, err := m.config.Inventory(ctx)
	if err != nil {
		m.observeClaim("prepare", refreshStarted, err)
		return nil, fmt.Errorf("refresh inventory: %w", err)
	}
	if err := m.inventoryFresh(snapshot); err != nil {
		m.observeClaim("prepare", refreshStarted, err)
		return nil, err
	}
	if _, _, err := m.observeLocked(ctx, snapshot, false); err != nil {
		m.observeClaim("prepare", refreshStarted, err)
		return nil, fmt.Errorf("monitor inventory: %w", err)
	}
	byName := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		byName[device.DRAName(item)] = item
	}
	owners := m.deviceOwners()
	result := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		started := time.Now()
		uid := types.UID("")
		if claim != nil {
			uid = claim.UID
		}
		prepared, prepareErr := m.prepareOne(ctx, claim, byName, owners)
		m.observeClaim("prepare", started, prepareErr)
		if prepareErr != nil {
			result[uid] = kubeletplugin.PrepareResult{Err: prepareErr}
			continue
		}
		result[uid] = kubeletplugin.PrepareResult{Devices: prepared}
	}
	if err := m.persist(); err != nil {
		m.observeClaim("prepare", time.Now(), err)
		return nil, err
	}
	return result, nil
}

// UnprepareResourceClaims sanitizes released devices before removing their CDI and ownership state.
func (m *Manager) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	refreshStarted := time.Now()
	snapshot, err := m.config.Inventory(ctx)
	if err != nil {
		m.observeClaim("unprepare", refreshStarted, err)
		return nil, fmt.Errorf("refresh inventory: %w", err)
	}
	if err := m.inventoryFresh(snapshot); err != nil {
		m.observeClaim("unprepare", refreshStarted, err)
		return nil, err
	}
	if _, _, err := m.observeLocked(ctx, snapshot, false); err != nil {
		m.observeClaim("unprepare", refreshStarted, err)
		return nil, fmt.Errorf("monitor inventory: %w", err)
	}
	byName := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		byName[device.DRAName(item)] = item
	}
	result := make(map[types.UID]error, len(claims))
	for _, claim := range claims {
		started := time.Now()
		prepared, ok := m.state.Claims[string(claim.UID)]
		if !ok {
			result[claim.UID] = nil
			m.observeClaim("unprepare", started, nil)
			continue
		}
		if prepared.Phase != ClaimReleasing {
			prepared.Phase = ClaimReleasing
			m.state.Claims[string(claim.UID)] = prepared
			if err := m.persist(); err != nil {
				result[claim.UID] = fmt.Errorf("persist release intent: %w", err)
				m.observeClaim("unprepare", started, result[claim.UID])
				continue
			}
			if err := m.auditLocked(AuditEvent{Action: "claim-release-intent", Outcome: "recorded"}, &prepared); err != nil {
				result[claim.UID] = err
				m.observeClaim("unprepare", started, err)
				continue
			}
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
			_ = m.persist()
			result[claim.UID] = sanitizeErr
			m.observeClaim("unprepare", started, sanitizeErr)
			continue
		}
		if err := m.auditLocked(AuditEvent{Action: "claim-release", Outcome: "approved"}, &prepared); err != nil {
			for _, claimedDevice := range prepared.Devices {
				m.state.Quarantined[claimedDevice.Device] = QuarantineRecord{Reason: "claim release audit failed: " + err.Error(), Since: time.Now().UTC()}
			}
			result[claim.UID] = err
			m.observeClaim("unprepare", started, err)
			continue
		}
		if err := os.Remove(filepath.Join(m.config.CDIDir, cdiFilename(prepared.UID))); err != nil && !os.IsNotExist(err) {
			result[claim.UID] = err
			m.observeClaim("unprepare", started, err)
			continue
		}
		delete(m.state.Claims, string(claim.UID))
		if err := m.persist(); err != nil {
			m.state.Claims[string(claim.UID)] = prepared
			result[claim.UID] = fmt.Errorf("commit released claim: %w", err)
			m.observeClaim("unprepare", started, result[claim.UID])
			continue
		}
		result[claim.UID] = nil
		m.observeClaim("unprepare", started, nil)
	}
	if err := m.persist(); err != nil {
		m.observeClaim("unprepare", time.Now(), err)
		return nil, err
	}
	return result, nil
}

// Stats is a point-in-time lifecycle summary used for bounded-cardinality
// device gauges.
type Stats struct {
	Allocated   int
	Quarantined int
}

// SnapshotStats returns current allocated and quarantined device counts.
func (m *Manager) SnapshotStats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats := Stats{Quarantined: len(m.state.Quarantined)}
	for _, claim := range m.state.Claims {
		stats.Allocated += len(claim.Devices)
	}
	return stats
}

// observeClaim records one claim operation when metrics are configured.
func (m *Manager) observeClaim(operation string, started time.Time, err error) {
	if m.config.Metrics != nil {
		m.config.Metrics.ObserveClaim(m.config.NodeName, operation, time.Since(started), err)
	}
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
	prepared, err := m.claimFromAllocation(claim, byName, owners)
	if err != nil {
		return nil, err
	}
	existing, found := m.state.Claims[string(claim.UID)]
	if found {
		if !sameClaim(existing, prepared) {
			return nil, errors.New("repeated prepare does not match persisted allocation")
		}
		if existing.Phase == ClaimReleasing || existing.Phase == ClaimRecovered {
			return nil, fmt.Errorf("claim is in %s recovery", existing.Phase)
		}
		if existing.Phase == ClaimPrepared {
			for _, claimed := range existing.Devices {
				if record, quarantined := m.state.Quarantined[claimed.Device]; quarantined {
					return nil, fmt.Errorf("prepared device %q is quarantined: %s", claimed.Device, record.Reason)
				}
			}
			if err := m.writeCDI(existing); err != nil {
				return nil, fmt.Errorf("repair CDI: %w", err)
			}
			return cdiResults(existing), nil
		}
		prepared = existing
	} else {
		prepared.Phase = ClaimPreparing
		m.state.Claims[string(claim.UID)] = prepared
		if err := m.persist(); err != nil {
			delete(m.state.Claims, string(claim.UID))
			return nil, fmt.Errorf("persist prepare intent: %w", err)
		}
		if err := m.auditLocked(AuditEvent{Action: "claim-prepare-intent", Outcome: "recorded"}, &prepared); err != nil {
			return nil, err
		}
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
	if err := m.inventoryFresh(refreshed); err != nil {
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
		if !found || !item.Eligible || item.Health != device.HealthHealthy || item.StableID != claimedDevice.StableID {
			reason := "device did not return with the same healthy identity"
			_ = m.quarantineLocked(claimedDevice.Device, claimedDevice.Path, reason, &prepared, "preflight-verify")
			return nil, fmt.Errorf("sanitized device %q %s", claimedDevice.Device, reason)
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
	prepared.Phase = ClaimPrepared
	m.state.Claims[string(claim.UID)] = prepared
	if err := m.persist(); err != nil {
		prepared.Phase = ClaimPreparing
		m.state.Claims[string(claim.UID)] = prepared
		_ = os.Remove(filepath.Join(m.config.CDIDir, cdiFilename(prepared.UID)))
		return nil, fmt.Errorf("commit prepared claim: %w", err)
	}
	for _, item := range prepared.Devices {
		owners[item.Device] = string(claim.UID)
	}
	return cdiResults(prepared), nil
}

// claimFromAllocation validates a local allocation and captures its exact current identity.
func (m *Manager) claimFromAllocation(claim *resourceapi.ResourceClaim, byName map[string]device.InventoryDevice, owners map[string]string) (PreparedClaim, error) {
	if len(claim.Status.Allocation.Devices.Results) == 0 {
		return PreparedClaim{}, errors.New("claim allocation has no devices")
	}
	prepared := PreparedClaim{UID: claim.UID, Namespace: claim.Namespace, Name: claim.Name, Phase: ClaimPreparing}
	seen := map[string]struct{}{}
	for _, allocation := range claim.Status.Allocation.Devices.Results {
		if allocation.Driver != m.config.Driver {
			return PreparedClaim{}, fmt.Errorf("allocation driver %q does not match %q", allocation.Driver, m.config.Driver)
		}
		if allocation.Pool != m.config.NodeName {
			return PreparedClaim{}, fmt.Errorf("allocation pool %q is not local node %q", allocation.Pool, m.config.NodeName)
		}
		item, ok := byName[allocation.Device]
		if !ok || !item.Eligible || m.deviceUnsafeReason(item) != "" {
			return PreparedClaim{}, fmt.Errorf("allocated device %q is not healthy locally", allocation.Device)
		}
		if m.config.RequireIOMMU && (item.PCI.IOMMUGroup < 0 || item.PCI.IOMMUGroupSize != 1) {
			return PreparedClaim{}, fmt.Errorf("allocated device %q has no dedicated IOMMU group", allocation.Device)
		}
		if _, duplicate := seen[allocation.Device]; duplicate {
			return PreparedClaim{}, fmt.Errorf("device %q is allocated more than once", allocation.Device)
		}
		seen[allocation.Device] = struct{}{}
		if ownerUID, owned := owners[allocation.Device]; owned && ownerUID != string(claim.UID) {
			return PreparedClaim{}, fmt.Errorf("device %q is already prepared for claim %s", allocation.Device, ownerUID)
		}
		id := cdiID(claim.UID, allocation.Device)
		prepared.Devices = append(prepared.Devices, ClaimDevice{
			Pool: allocation.Pool, Device: allocation.Device, StableID: item.StableID, Path: item.Node.Path,
			Major: item.Node.Major, Minor: item.Node.Minor, CDIID: id,
		})
	}
	return prepared, nil
}

// sameClaim compares immutable claim identity and exact device allocation details.
func sameClaim(left, right PreparedClaim) bool {
	return left.UID == right.UID && left.Namespace == right.Namespace && left.Name == right.Name && reflect.DeepEqual(left.Devices, right.Devices)
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
