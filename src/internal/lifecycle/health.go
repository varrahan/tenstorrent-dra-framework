package lifecycle

import (
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/device"
	drahealthv1alpha1 "k8s.io/kubelet/pkg/apis/dra-health/v1alpha1"
)

const healthStreamInterval = 10 * time.Second

// DeviceHealthSnapshot returns a complete kubelet DRA health observation for
// all devices known to this node agent.
func (m *Manager) DeviceHealthSnapshot() *drahealthv1alpha1.NodeWatchResourcesResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.lockAndReload(); err == nil {
		defer m.unlockState()
	}
	return m.deviceHealthSnapshotLocked(time.Now())
}

func (m *Manager) deviceHealthSnapshotLocked(now time.Time) *drahealthv1alpha1.NodeWatchResourcesResponse {
	response := &drahealthv1alpha1.NodeWatchResourcesResponse{}
	observed := make(map[string]device.InventoryDevice, len(m.lastSnapshot.Devices))
	for _, item := range m.lastSnapshot.Devices {
		observed[device.DRAName(item)] = item
	}
	fresh := m.inventoryFresh(m.lastSnapshot) == nil
	for name, known := range m.state.Known {
		status := drahealthv1alpha1.HealthStatus_UNKNOWN
		if item, found := observed[name]; found && fresh {
			switch {
			case item.Health == device.HealthUnknown:
				status = drahealthv1alpha1.HealthStatus_UNKNOWN
			case m.deviceUnsafeReason(item) != "":
				status = drahealthv1alpha1.HealthStatus_UNHEALTHY
			case m.state.Quarantined[name].Reason != "":
				status = drahealthv1alpha1.HealthStatus_UNHEALTHY
			default:
				status = drahealthv1alpha1.HealthStatus_HEALTHY
			}
		}
		updated := known.LastSeen.Unix()
		if updated <= 0 || known.LastSeen.After(now) {
			updated = now.Unix()
		}
		response.Devices = append(response.Devices, &drahealthv1alpha1.DeviceHealth{
			Device: &drahealthv1alpha1.DeviceIdentifier{PoolName: m.config.NodeName, DeviceName: name},
			Health: status, LastUpdatedTime: updated,
		})
	}
	return response
}

// NodeWatchResources streams complete health snapshots to kubelet. Kubelet
// treats devices omitted from a snapshot or stream timeout as Unknown.
func (m *Manager) NodeWatchResources(_ *drahealthv1alpha1.NodeWatchResourcesRequest, stream drahealthv1alpha1.DRAResourceHealth_NodeWatchResourcesServer) error {
	ticker := time.NewTicker(healthStreamInterval)
	defer ticker.Stop()
	for {
		if err := stream.Send(m.DeviceHealthSnapshot()); err != nil {
			return err
		}
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-ticker.C:
		}
	}
}

var _ drahealthv1alpha1.DRAResourceHealthServer = (*Manager)(nil)
