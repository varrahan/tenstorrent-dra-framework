package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
)

const auditFilename = "audit.jsonl"

type Safety struct {
	Unsafe  bool
	Healthy int
	Total   int
	Reason  string
	Message string
}

type AuditEvent struct {
	Time      time.Time `json:"time"`
	Action    string    `json:"action"`
	Outcome   string    `json:"outcome"`
	Device    string    `json:"device,omitempty"`
	Path      string    `json:"path,omitempty"`
	ClaimUID  string    `json:"claimUID,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	ClaimName string    `json:"claimName,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

// Monitor updates health and quarantine state, recovers idle devices, and filters unsafe capacity.
func (m *Manager) Monitor(ctx context.Context, snapshot device.InventorySnapshot) (device.InventorySnapshot, Safety, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	filtered, safety, monitorErr := m.observeLocked(ctx, snapshot, true)
	if err := m.persist(); err != nil {
		return filtered, safety, errors.Join(monitorErr, err)
	}
	return filtered, safety, monitorErr
}

// InventoryFailed quarantines all known devices when discovery cannot prove current health.
func (m *Manager) InventoryFailed(err error) (device.InventorySnapshot, Safety, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reason := "inventory unavailable"
	if err != nil {
		reason += ": " + err.Error()
	}
	var auditErr error
	for name, known := range m.state.Known {
		auditErr = errors.Join(auditErr, m.quarantineLocked(name, known.Path, reason, nil, "inventory"))
	}
	snapshot := device.InventorySnapshot{ObservedAt: time.Now().UTC()}
	safety := safetyFor(snapshot, m.state.Known, m.state.Quarantined)
	if persistErr := m.persist(); persistErr != nil {
		auditErr = errors.Join(auditErr, persistErr)
	}
	return snapshot, safety, auditErr
}

// observeLocked reconciles observations with known devices and quarantine state while holding the manager lock.
func (m *Manager) observeLocked(ctx context.Context, snapshot device.InventorySnapshot, recoverIdle bool) (device.InventorySnapshot, Safety, error) {
	observed := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	owners := m.deviceOwners()
	var resultErr error
	for _, item := range snapshot.Devices {
		name := device.DRAName(item)
		observed[name] = item
		m.state.Known[name] = KnownDevice{Path: item.Node.Path, LastSeen: snapshot.ObservedAt}
		if reason := m.deviceUnsafeReason(item); reason != "" {
			resultErr = errors.Join(resultErr, m.quarantineLocked(name, item.Node.Path, reason, m.claimForOwner(owners[name]), healthAction(owners[name])))
		}
	}
	for name, known := range m.state.Known {
		if _, found := observed[name]; !found {
			resultErr = errors.Join(resultErr, m.quarantineLocked(name, known.Path, "device is no longer observed", m.claimForOwner(owners[name]), healthAction(owners[name])))
		}
	}
	if recoverIdle {
		for name, item := range observed {
			record, quarantined := m.state.Quarantined[name]
			if !quarantined || owners[name] != "" || !item.CharacterDevicePresent {
				continue
			}
			unsafeBeforeReset := m.deviceUnsafeReason(item) != ""
			if record.AwaitingHealth && !unsafeBeforeReset {
				if err := m.auditLocked(AuditEvent{Time: time.Now().UTC(), Action: "recovery-health", Outcome: "success", Device: name, Path: item.Node.Path}, nil); err != nil {
					resultErr = errors.Join(resultErr, err)
				} else {
					delete(m.state.Quarantined, name)
				}
				continue
			}
			if err := m.sanitizeLocked(ctx, "recovery-sanitize", name, item.Node.Path, nil); err != nil {
				resultErr = errors.Join(resultErr, err)
			} else if unsafeBeforeReset {
				m.state.Quarantined[name] = QuarantineRecord{
					Reason: "awaiting healthy observation after sanitization", Since: record.Since, AwaitingHealth: true,
				}
			}
		}
	}
	filtered := m.filterLocked(snapshot)
	return filtered, safetyFor(filtered, m.state.Known, m.state.Quarantined), resultErr
}

// filterLocked returns a snapshot copy with quarantined devices marked ineligible.
func (m *Manager) filterLocked(snapshot device.InventorySnapshot) device.InventorySnapshot {
	filtered := snapshot
	filtered.Devices = append([]device.InventoryDevice(nil), snapshot.Devices...)
	for index := range filtered.Devices {
		name := device.DRAName(filtered.Devices[index])
		if record, found := m.state.Quarantined[name]; found {
			filtered.Devices[index].Eligible = false
			filtered.Devices[index].RejectionReason = "quarantined: " + record.Reason
		}
	}
	return filtered
}

// deviceUnsafeReason returns the first health, isolation, or fabric fault blocking a device.
func (m *Manager) deviceUnsafeReason(item device.InventoryDevice) string {
	switch {
	case !item.CharacterDevicePresent:
		return "character device is unavailable"
	case item.Health != device.HealthHealthy:
		return "device health is " + string(item.Health)
	case item.Fault.Code != "" && item.Fault.Code != "0":
		return "hardware fault " + item.Fault.Code
	case m.config.RequireIOMMU && item.PCI.IOMMUGroup < 0:
		return "device has no IOMMU group"
	case m.config.RequireIOMMU && item.PCI.IOMMUGroupSize != 1:
		return fmt.Sprintf("IOMMU group %d contains %d devices", item.PCI.IOMMUGroup, item.PCI.IOMMUGroupSize)
	}
	for _, link := range item.Fabric.Links {
		state := strings.ToLower(strings.TrimSpace(link.State))
		if state != "" && state != "up" {
			return fmt.Sprintf("fabric link %s is %s", link.Name, link.State)
		}
	}
	return ""
}

// safetyFor summarizes healthy advertised capacity and decides whether the node must be fenced.
func safetyFor(snapshot device.InventorySnapshot, known map[string]KnownDevice, quarantined map[string]QuarantineRecord) Safety {
	safety := Safety{Total: len(known)}
	for _, item := range snapshot.Devices {
		if _, blocked := quarantined[device.DRAName(item)]; !blocked && item.Eligible && item.Health == device.HealthHealthy {
			safety.Healthy++
		}
	}
	safety.Unsafe = safety.Healthy == 0
	if safety.Unsafe {
		safety.Reason = "NoHealthyDevices"
		safety.Message = fmt.Sprintf("0 of %d Tenstorrent devices are healthy and available", safety.Total)
	} else {
		safety.Reason = "DevicesHealthy"
		safety.Message = fmt.Sprintf("%d of %d Tenstorrent devices are healthy and available", safety.Healthy, safety.Total)
	}
	return safety
}

// healthAction labels a health audit according to whether the device has an active owner.
func healthAction(owner string) string {
	if owner != "" {
		return "active-health"
	}
	return "idle-health"
}

// claimForOwner looks up prepared claim context for health and audit events.
func (m *Manager) claimForOwner(uid string) *PreparedClaim {
	if uid == "" {
		return nil
	}
	claim, found := m.state.Claims[uid]
	if !found {
		return nil
	}
	return &claim
}

// quarantineLocked records an unsafe device and appends its transition to the audit log.
func (m *Manager) quarantineLocked(name, path, reason string, claim *PreparedClaim, action string) error {
	current, found := m.state.Quarantined[name]
	if found && current.Reason == reason {
		return nil
	}
	since := time.Now().UTC()
	if found {
		since = current.Since
	}
	m.state.Quarantined[name] = QuarantineRecord{Reason: reason, Since: since}
	return m.auditLocked(AuditEvent{
		Time: time.Now().UTC(), Action: action, Outcome: "quarantined",
		Device: name, Path: path, Reason: reason,
	}, claim)
}

// sanitizeLocked resets one device and updates quarantine state from the reset and audit outcomes.
func (m *Manager) sanitizeLocked(ctx context.Context, action, name, path string, claim *PreparedClaim) error {
	err := m.config.Resetter.Reset(ctx, path)
	event := AuditEvent{Time: time.Now().UTC(), Action: action, Outcome: "success", Device: name, Path: path}
	if err != nil {
		event.Outcome = "failure"
		event.Reason = err.Error()
	}
	auditErr := m.auditLocked(event, claim)
	if err == nil && auditErr == nil {
		delete(m.state.Quarantined, name)
		return nil
	}
	reason := "sanitization failed"
	if err != nil {
		reason += ": " + err.Error()
	} else {
		reason += ": audit record failed: " + auditErr.Error()
	}
	current, found := m.state.Quarantined[name]
	if !found {
		current.Since = time.Now().UTC()
	}
	current.Reason = reason
	m.state.Quarantined[name] = current
	return errors.Join(err, auditErr)
}

// auditLocked durably appends one lifecycle event and optional claim identity to the JSONL log.
func (m *Manager) auditLocked(event AuditEvent, claim *PreparedClaim) error {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if claim != nil {
		event.ClaimUID = string(claim.UID)
		event.Namespace = claim.Namespace
		event.ClaimName = claim.Name
	}
	if err := os.MkdirAll(m.config.StateDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(m.config.StateDir, auditFilename), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
