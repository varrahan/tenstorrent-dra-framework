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
	if err := m.inventoryFresh(snapshot); err != nil {
		return m.inventoryFailedLocked(err)
	}
	filtered, safety, monitorErr := m.observeLocked(ctx, snapshot, true)
	m.lastSnapshot = snapshot
	if err := m.persist(); err != nil {
		return filtered, safety, errors.Join(monitorErr, err)
	}
	return filtered, safety, monitorErr
}

// InventoryFailed quarantines all known devices when discovery cannot prove current health.
func (m *Manager) InventoryFailed(err error) (device.InventorySnapshot, Safety, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inventoryFailedLocked(err)
}

// inventoryFailedLocked reuses only a still-fresh observation, then fences all known capacity.
func (m *Manager) inventoryFailedLocked(err error) (device.InventorySnapshot, Safety, error) {
	reason := "inventory unavailable"
	if err != nil {
		reason += ": " + err.Error()
	}
	if m.inventoryFresh(m.lastSnapshot) == nil {
		filtered := m.filterLocked(m.lastSnapshot)
		safety := safetyFor(filtered, m.state.Known, m.state.Quarantined)
		safety.Reason = "InventoryDegraded"
		safety.Message = reason + "; using last fresh observation"
		return filtered, safety, m.auditLocked(AuditEvent{Action: "inventory", Outcome: "degraded", Reason: reason}, nil)
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

// inventoryFresh accepts only non-future observations within the configured grace period.
func (m *Manager) inventoryFresh(snapshot device.InventorySnapshot) error {
	if snapshot.ObservedAt.IsZero() {
		return errors.New("inventory observation time is missing")
	}
	age := time.Since(snapshot.ObservedAt)
	if age < -5*time.Second {
		return errors.New("inventory observation time is in the future")
	}
	if age > m.config.MaxInventoryAge {
		return fmt.Errorf("inventory observation is stale by %s", age.Round(time.Second))
	}
	return nil
}

// observeLocked reconciles observations with known devices and quarantine state while holding the manager lock.
func (m *Manager) observeLocked(ctx context.Context, snapshot device.InventorySnapshot, recoverIdle bool) (device.InventorySnapshot, Safety, error) {
	observed := make(map[string]device.InventoryDevice, len(snapshot.Devices))
	owners := m.deviceOwners()
	var resultErr error
	for _, item := range snapshot.Devices {
		name := device.DRAName(item)
		observed[name] = item
		_, known := m.state.Known[name]
		if !known && recoverIdle {
			resultErr = errors.Join(resultErr, m.quarantineLocked(name, item.Node.Path, "new device requires sanitization", nil, "hotplug"))
		}
		for priorName, prior := range m.state.Known {
			if priorName != name && prior.Path == item.Node.Path {
				resultErr = errors.Join(resultErr, m.quarantineLocked(name, item.Node.Path, "hardware identity changed on device path", nil, "hotplug"))
			}
		}
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
	case !item.Eligible && item.RejectionReason != "":
		return item.RejectionReason
	case !item.Eligible:
		return "device is ineligible"
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
	started := time.Now()
	err := m.config.Resetter.Reset(ctx, path)
	if m.config.Metrics != nil {
		duration := time.Since(started)
		m.config.Metrics.ObserveHardware(m.config.NodeName, "reset", action, duration, err)
		m.config.Metrics.ObserveHardware(m.config.NodeName, "scrub", action, duration, err)
	}
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
	if err := file.Close(); err != nil {
		return err
	}
	m.config.Logger.Info("lifecycle decision",
		"action", event.Action,
		"outcome", event.Outcome,
		"device", event.Device,
		"device_path", event.Path,
		"claim_uid", event.ClaimUID,
		"claim_namespace", event.Namespace,
		"claim", event.ClaimName,
		"reason", event.Reason,
	)
	if m.config.EventSink != nil {
		m.config.EventSink(event, claim)
	}
	return nil
}
