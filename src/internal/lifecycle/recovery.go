package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	"golang.org/x/sys/unix"
)

// acquireLock prevents multiple node agents from sharing lifecycle and CDI state.
func (m *Manager) acquireLock() error {
	if err := os.MkdirAll(m.config.StateDir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(m.config.StateDir, "agent.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return fmt.Errorf("lock lifecycle state: %w", err)
	}
	m.lockFile = file
	return nil
}

// Close releases the host-level lifecycle lock held by this manager.
func (m *Manager) Close() error {
	if m.lockFile == nil {
		return nil
	}
	err := errors.Join(unix.Flock(int(m.lockFile.Fd()), unix.LOCK_UN), m.lockFile.Close())
	m.lockFile = nil
	return err
}

// recoverCorruptState preserves the bad file and quarantines all currently visible hardware.
func (m *Manager) recoverCorruptState(loadErr error) error {
	path := filepath.Join(m.config.StateDir, "claims.json")
	backup := path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
	if err := os.Rename(path, backup); err != nil {
		return errors.Join(loadErr, fmt.Errorf("preserve corrupt state: %w", err))
	}
	m.state = persistedState{Version: stateVersion, Claims: map[string]PreparedClaim{}, Quarantined: map[string]QuarantineRecord{}, Known: map[string]KnownDevice{}}
	snapshot, err := m.config.Inventory(context.Background())
	if err != nil {
		return errors.Join(loadErr, fmt.Errorf("inventory for state recovery: %w", err))
	}
	for _, item := range snapshot.Devices {
		name := device.DRAName(item)
		m.state.Known[name] = KnownDevice{Path: item.Node.Path, LastSeen: snapshot.ObservedAt}
		m.state.Quarantined[name] = QuarantineRecord{Reason: "lifecycle state was corrupt", Since: time.Now().UTC()}
	}
	return errors.Join(m.auditLocked(AuditEvent{Action: "state-recovery", Outcome: "quarantined", Reason: loadErr.Error()}, nil), m.persist())
}

// reconcileStartup aligns persisted ownership, live allocations, CDI files, and inventory.
func (m *Manager) reconcileStartup(ctx context.Context) error {
	snapshot, err := m.config.Inventory(ctx)
	if err != nil {
		return fmt.Errorf("startup inventory: %w", err)
	}
	if err := m.inventoryFresh(snapshot); err != nil {
		return err
	}
	byName := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	for _, item := range snapshot.Devices {
		byName[device.DRAName(item)] = item
	}
	allocated, err := m.localAllocations(ctx, byName)
	if err != nil {
		return err
	}
	var reconcileErr error
	for uid, claim := range allocated {
		if _, found := m.state.Claims[uid]; !found {
			claim.Phase = ClaimRecovered
			m.state.Claims[uid] = claim
		}
	}
	for uid, claim := range m.state.Claims {
		if m.config.Allocations != nil {
			if _, found := allocated[uid]; !found {
				claim.Phase = ClaimReleasing
				m.state.Claims[uid] = claim
			}
		}
		for index := range claim.Devices {
			claimed := &claim.Devices[index]
			item, found := byName[claimed.Device]
			reason := ""
			switch {
			case !found:
				reason = "persisted device is not present"
			case claimed.StableID != "" && claimed.StableID != item.StableID:
				reason = "persisted hardware identity changed"
			case claimed.Path != item.Node.Path || claimed.Major != item.Node.Major || claimed.Minor != item.Node.Minor:
				reason = "persisted device node identity changed"
			case m.deviceUnsafeReason(item) != "":
				reason = m.deviceUnsafeReason(item)
			}
			if found && claimed.StableID == "" {
				claimed.StableID = item.StableID
			}
			if claim.Phase != ClaimPrepared && reason == "" {
				reason = "interrupted " + strings.ToLower(string(claim.Phase)) + " transition"
			}
			if reason != "" {
				reconcileErr = errors.Join(reconcileErr, m.quarantineLocked(claimed.Device, claimed.Path, reason, &claim, "startup-recovery"))
			}
		}
		m.state.Claims[uid] = claim
		complete := len(claim.Devices) > 0
		for _, claimed := range claim.Devices {
			complete = complete && claimed.Path != ""
		}
		if complete {
			if err := m.writeCDI(claim); err != nil {
				for _, claimed := range claim.Devices {
					reconcileErr = errors.Join(reconcileErr, m.quarantineLocked(claimed.Device, claimed.Path, "CDI repair failed: "+err.Error(), &claim, "startup-recovery"))
				}
			}
		}
	}
	if err := m.removeOrphanCDI(); err != nil {
		return err
	}
	m.lastSnapshot = snapshot
	return errors.Join(reconcileErr, m.persist())
}

// localAllocations converts API allocations for this driver and node into recoverable ownership records.
func (m *Manager) localAllocations(ctx context.Context, byName map[string]device.InventoryDevice) (map[string]PreparedClaim, error) {
	result := map[string]PreparedClaim{}
	if m.config.Allocations == nil {
		return result, nil
	}
	claims, err := m.config.Allocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list startup allocations: %w", err)
	}
	for _, claim := range claims {
		if claim == nil || claim.Status.Allocation == nil {
			continue
		}
		prepared := PreparedClaim{UID: claim.UID, Namespace: claim.Namespace, Name: claim.Name, Phase: ClaimRecovered}
		for _, allocation := range claim.Status.Allocation.Devices.Results {
			if allocation.Driver != m.config.Driver || allocation.Pool != m.config.NodeName {
				continue
			}
			claimed := ClaimDevice{Pool: allocation.Pool, Device: allocation.Device, CDIID: cdiID(claim.UID, allocation.Device)}
			if item, found := byName[allocation.Device]; found {
				claimed.StableID, claimed.Path = item.StableID, item.Node.Path
				claimed.Major, claimed.Minor = item.Node.Major, item.Node.Minor
			}
			prepared.Devices = append(prepared.Devices, claimed)
		}
		if len(prepared.Devices) > 0 {
			result[string(claim.UID)] = prepared
		}
	}
	return result, nil
}

// removeOrphanCDI deletes driver-owned CDI files that have no persisted or live claim.
func (m *Manager) removeOrphanCDI() error {
	entries, err := os.ReadDir(m.config.CDIDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	expected := map[string]struct{}{}
	for _, claim := range m.state.Claims {
		expected[cdiFilename(claim.UID)] = struct{}{}
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "claim-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if _, found := expected[entry.Name()]; found {
			continue
		}
		if err := os.Remove(filepath.Join(m.config.CDIDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
