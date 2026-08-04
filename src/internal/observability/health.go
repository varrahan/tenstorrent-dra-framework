package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Health owns the process startup, readiness, and liveness states used by
// Kubernetes probes. Accelerator health is intentionally separate and is
// represented by inventory metrics and the node condition.
type Health struct {
	component string
	node      string
	metrics   *Metrics
	started   atomic.Bool
	ready     atomic.Bool
	live      atomic.Bool
}

// NewHealth creates a live but not-yet-started component health state.
func NewHealth(component, node string, metrics *Metrics) *Health {
	health := &Health{component: component, node: node, metrics: metrics}
	health.live.Store(true)
	if metrics != nil {
		metrics.SetComponentReady(component, node, false)
	}
	return health
}

// MarkStarted marks command initialization complete.
func (h *Health) MarkStarted() { h.started.Store(true) }

// SetReady marks whether the component can currently perform its Kubernetes role.
func (h *Health) SetReady(ready bool) {
	h.ready.Store(ready)
	if h.metrics != nil {
		h.metrics.SetComponentReady(h.component, h.node, ready)
	}
}

// SetLive records an unrecoverable serving failure.
func (h *Health) SetLive(live bool) { h.live.Store(live) }

// Handler returns the health and Prometheus HTTP surface.
func (h *Health) Handler(metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/startupz", probe(h.started.Load))
	mux.HandleFunc("/readyz", probe(h.ready.Load))
	mux.HandleFunc("/livez", probe(h.live.Load))
	if metrics != nil {
		mux.Handle("/metrics", metrics)
	}
	return mux
}

// StartServer binds before returning, then serves until context cancellation.
// Binding synchronously makes an invalid or occupied address a startup failure.
func StartServer(ctx context.Context, address string, health *Health, metrics http.Handler, logger *slog.Logger) (*http.Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen on observability address %q: %w", address, err)
	}
	server := &http.Server{
		Addr:              address,
		Handler:           health.Handler(metrics),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			health.SetLive(false)
			logger.Error("observability server failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("observability server shutdown failed", "error", err)
		}
	}()
	return server, nil
}

// probe returns a small handler that evaluates state at request time.
func probe(current func() bool) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !current() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte("not ready\n"))
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	}
}
