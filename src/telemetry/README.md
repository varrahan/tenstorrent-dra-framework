# Telemetry Service

This directory contains the Python telemetry service for Tenstorrent device
health and metrics.

Initial scope:

- Discover Tenstorrent device nodes exposed by `tt-kmd`.
- Render minimal Prometheus-compatible metrics.
- Serve health and metrics endpoints with FastAPI.

The service starts small and intentionally shares no runtime code with the Go
DRA driver. Both components may inspect the same device paths, but each owns its
own language-specific runtime and packaging.
