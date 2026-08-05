package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics contains the bounded-cardinality production signals exported by both
// the controller and node agent. Resource identifiers belong in logs and
// Events; metric labels are deliberately limited to component, node, operation,
// kind, and outcome.
type Metrics struct {
	registry *prometheus.Registry

	componentReady       *prometheus.GaugeVec
	inventoryLastSuccess *prometheus.GaugeVec
	inventoryAge         *prometheus.GaugeVec
	inventoryGrace       *prometheus.GaugeVec
	devices              *prometheus.GaugeVec
	claimDuration        *prometheus.HistogramVec
	claimFailures        *prometheus.CounterVec
	hardwareDuration     *prometheus.HistogramVec
	hardwareOperations   *prometheus.CounterVec
	topologyValid        *prometheus.GaugeVec
	topologyErrors       *prometheus.GaugeVec
	placementDuration    *prometheus.HistogramVec
	placementAttempts    *prometheus.CounterVec
	reconcileDuration    *prometheus.HistogramVec
	reconcileFailures    *prometheus.CounterVec
}

// NewMetrics creates an isolated registry so tests and multiple command modes
// can construct metrics without colliding in the process-global registry.
func NewMetrics() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		componentReady: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_component_ready",
			Help: "Whether a driver component is ready to serve its Kubernetes role.",
		}, []string{"component", "node"}),
		inventoryLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_inventory_last_success_timestamp_seconds",
			Help: "Unix timestamp of the latest successful hardware inventory observation.",
		}, []string{"node"}),
		inventoryAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_inventory_age_seconds",
			Help: "Age in seconds of the hardware inventory currently used for publication.",
		}, []string{"node"}),
		inventoryGrace: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_inventory_grace_period_seconds",
			Help: "Maximum inventory age accepted for capacity publication.",
		}, []string{"node"}),
		devices: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_devices",
			Help: "Current Tenstorrent device count by lifecycle state.",
		}, []string{"node", "state"}),
		claimDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tenstorrent_dra_claim_operation_duration_seconds",
			Help:    "Latency of kubelet claim prepare and unprepare operations.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"node", "operation"}),
		claimFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tenstorrent_dra_claim_operation_failures_total",
			Help: "Failed kubelet claim prepare and unprepare operations.",
		}, []string{"node", "operation"}),
		hardwareDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tenstorrent_dra_hardware_operation_duration_seconds",
			Help:    "Latency of device reset and scrub operations.",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"node", "operation", "phase"}),
		hardwareOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tenstorrent_dra_hardware_operations_total",
			Help: "Device reset and scrub outcomes.",
		}, []string{"node", "operation", "phase", "outcome"}),
		topologyValid: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_topology_valid",
			Help: "Whether the current cluster fabric topology is valid (1) or invalid (0).",
		}, []string{}),
		topologyErrors: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tenstorrent_dra_topology_errors",
			Help: "Number of validation errors in the current cluster fabric topology.",
		}, []string{}),
		placementDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tenstorrent_dra_placement_duration_seconds",
			Help:    "Latency of topology-aware placement attempts.",
			Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2},
		}, []string{"outcome"}),
		placementAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tenstorrent_dra_placement_attempts_total",
			Help: "Topology-aware placement attempts by outcome.",
		}, []string{"outcome"}),
		reconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tenstorrent_dra_reconciliation_duration_seconds",
			Help:    "Controller and node reconciliation latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"component", "kind"}),
		reconcileFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tenstorrent_dra_reconciliation_failures_total",
			Help: "Controller and node reconciliation failures.",
		}, []string{"component", "kind"}),
	}
	m.registry.MustRegister(
		m.componentReady,
		m.inventoryLastSuccess,
		m.inventoryAge,
		m.inventoryGrace,
		m.devices,
		m.claimDuration,
		m.claimFailures,
		m.hardwareDuration,
		m.hardwareOperations,
		m.topologyValid,
		m.topologyErrors,
		m.placementDuration,
		m.placementAttempts,
		m.reconcileDuration,
		m.reconcileFailures,
	)
	return m
}

// Handler returns the Prometheus exposition endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// SetComponentReady updates the component readiness signal.
func (m *Metrics) SetComponentReady(component, node string, ready bool) {
	value := 0.0
	if ready {
		value = 1
	}
	m.componentReady.WithLabelValues(component, node).Set(value)
}

// ObserveInventory updates inventory freshness and device-state gauges.
func (m *Metrics) ObserveInventory(node string, observedAt time.Time, gracePeriod time.Duration, published, allocated, quarantined int) {
	if !observedAt.IsZero() {
		m.inventoryLastSuccess.WithLabelValues(node).Set(float64(observedAt.Unix()))
		age := time.Since(observedAt).Seconds()
		if age < 0 {
			age = 0
		}
		m.inventoryAge.WithLabelValues(node).Set(age)
	}
	m.inventoryGrace.WithLabelValues(node).Set(gracePeriod.Seconds())
	m.devices.WithLabelValues(node, "published").Set(float64(published))
	m.devices.WithLabelValues(node, "allocated").Set(float64(allocated))
	m.devices.WithLabelValues(node, "quarantined").Set(float64(quarantined))
}

// ObserveClaim records one claim-level prepare or unprepare attempt.
func (m *Metrics) ObserveClaim(node, operation string, duration time.Duration, err error) {
	m.claimDuration.WithLabelValues(node, operation).Observe(duration.Seconds())
	if err != nil {
		m.claimFailures.WithLabelValues(node, operation).Inc()
	}
}

// ObserveHardware records reset and scrub latency and outcome. The driver uses
// whole-device reset as its certified scrub boundary, so callers emit both
// operation labels for the same sanitization transaction.
func (m *Metrics) ObserveHardware(node, operation, phase string, duration time.Duration, err error) {
	outcome := "success"
	if err != nil {
		outcome = "failure"
	}
	m.hardwareDuration.WithLabelValues(node, operation, phase).Observe(duration.Seconds())
	m.hardwareOperations.WithLabelValues(node, operation, phase, outcome).Inc()
}

// ObserveTopology records the current fabric validation result.
func (m *Metrics) ObserveTopology(valid bool, errors int) {
	value := 0.0
	if valid {
		value = 1
	}
	m.topologyValid.WithLabelValues().Set(value)
	m.topologyErrors.WithLabelValues().Set(float64(errors))
}

// ObservePlacement records one solver attempt.
func (m *Metrics) ObservePlacement(duration time.Duration, outcome string) {
	m.placementDuration.WithLabelValues(outcome).Observe(duration.Seconds())
	m.placementAttempts.WithLabelValues(outcome).Inc()
}

// ObserveReconcile records one bounded reconciliation unit.
func (m *Metrics) ObserveReconcile(component, kind string, duration time.Duration, err error) {
	m.reconcileDuration.WithLabelValues(component, kind).Observe(duration.Seconds())
	if err != nil {
		m.reconcileFailures.WithLabelValues(component, kind).Inc()
	}
}
