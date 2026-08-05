package test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/varrahan/tenstorrent-dra-framework/src/internal/observability"
)

// TestHealthEndpointsTrackIndependentStates verifies Kubernetes probes do not
// conflate process liveness with initialization or readiness.
func TestHealthEndpointsTrackIndependentStates(t *testing.T) {
	metrics := observability.NewMetrics()
	health := observability.NewHealth("node", "worker-a", metrics)
	handler := health.Handler(metrics.Handler())

	assertStatus(t, handler, "/livez", http.StatusOK)
	assertStatus(t, handler, "/startupz", http.StatusServiceUnavailable)
	assertStatus(t, handler, "/readyz", http.StatusServiceUnavailable)

	health.MarkStarted()
	health.SetReady(true)
	assertStatus(t, handler, "/startupz", http.StatusOK)
	assertStatus(t, handler, "/readyz", http.StatusOK)

	health.SetLive(false)
	assertStatus(t, handler, "/livez", http.StatusServiceUnavailable)
}

// TestMetricsExposeProductionSignals verifies the required operations surface
// is present in Prometheus exposition format.
func TestMetricsExposeProductionSignals(t *testing.T) {
	metrics := observability.NewMetrics()
	metrics.SetComponentReady("node", "worker-a", true)
	metrics.ObserveInventory("worker-a", time.Now().Add(-time.Second), time.Minute, 2, 1, 1)
	metrics.ObserveClaim("worker-a", "prepare", 25*time.Millisecond, errors.New("prepare failed"))
	metrics.ObserveHardware("worker-a", "reset", "preflight-sanitize", 10*time.Millisecond, nil)
	metrics.ObserveHardware("worker-a", "scrub", "preflight-sanitize", 10*time.Millisecond, nil)
	metrics.ObserveTopology(false, 2)
	metrics.ObservePlacement(5*time.Millisecond, "unsatisfied")
	metrics.ObserveReconcile("controller", "workload", 5*time.Millisecond, errors.New("conflict"))

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	for _, signal := range []string{
		"tenstorrent_dra_component_ready",
		"tenstorrent_dra_inventory_age_seconds",
		"tenstorrent_dra_inventory_grace_period_seconds",
		"tenstorrent_dra_devices",
		"tenstorrent_dra_claim_operation_duration_seconds",
		"tenstorrent_dra_claim_operation_failures_total",
		"tenstorrent_dra_hardware_operations_total",
		"tenstorrent_dra_topology_valid",
		"tenstorrent_dra_placement_duration_seconds",
		"tenstorrent_dra_reconciliation_failures_total",
	} {
		if !strings.Contains(body, signal) {
			t.Fatalf("metrics output does not contain %q:\n%s", signal, body)
		}
	}
}
