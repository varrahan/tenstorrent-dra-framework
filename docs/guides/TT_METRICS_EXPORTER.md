# Tenstorrent DRA Metrics Exporter

This repository contains the blueprints for building a Kubernetes Dynamic Resource Allocation (DRA) metrics exporter for Tenstorrent hardware (Wormhole/Blackhole clusters).

This service acts as a DaemonSet to track real-time memory usage, Tensix core utilization, and Ethernet mesh topology, exposing them via a Prometheus `/metrics` endpoint.

---

## Implementation Guide

The development process is split into five distinct stages, transitioning from simulator environment setup to final Kubernetes deployment.

### Stage 1: Environment & Simulator Setup

Before writing code, configure the QEMU simulator (`ttsim`) to expose a multi-chip mesh network. This is required to test DRA topology scheduling.

1. **Configure multi-chip profile**: On your host, launch `ttsim` using a multi-device profile (e.g., `wh_x2` or `wh_x8`).
2. **Attach to QEMU**: Boot your VM ensuring the simulated PCIe devices are attached.
3. **Verify KMD Binding**: Inside the guest VM, verify that `tt-kmd` successfully binds and creates endpoints at `/sys/class/tenstorrent/0`, `/sys/class/tenstorrent/1`, etc.

### Stage 2: Device Discovery & Memory Telemetry (C++ sysfs)

Bypass `pyluwen` to build a lean, uniform C++ binary. Begin by reading the host operating system's sysfs endpoints.

1. **Iterate Devices**: Write C++ logic using `<filesystem>` to scan `/sys/class/tenstorrent/` and count available devices.
2. **Extract Memory Data**: Open and read `/sys/class/tenstorrent/N/memory_usage` stream.
3. **Extract Board Info**: Parse architecture (Wormhole/Blackhole) and health status metrics, noting that simulator thermal/power data may be dummy values.

### Stage 3: Tensix Core Utilization (TT-Metalium Profiler)

Tensix cores use static spatial mapping. You must integrate with TT-Metalium to intercept active `CoreGrids`.

1. **Link Library**: Link your C++ application against `libtt_metal.so`.
2. **Enable Profiler**: Ensure the environment variable `TT_METAL_DEVICE_PROFILER=1` is active.
3. **Poll Active Programs**: Periodically call `tt::tt_metal::detail::ReadDeviceProfilerResults(device_ptr)`.
4. **Calculate Usage**: Parse the returned `CoreRangeSet` objects to calculate the delta between total chip cores and actively reserved cores.

### Stage 4: Prometheus Exporter Packaging

Expose the gathered metrics to Kubernetes via a lightweight embedded HTTP server.

1. **Integrate `prometheus-cpp`**: Include the official Prometheus C++ client.
2. **Register Gauges**: Create Prometheus gauges for `tt_memory_used_bytes`, `tt_memory_total_bytes`, `tt_tensix_cores_used`, and `tt_tensix_cores_available`.
3. **Polling Loop**: Start a background thread that executes the Stage 2 and Stage 3 logic at a set interval (e.g., every 5 seconds) to update the gauge values.
4. **Serve `/metrics`**: Bind the embedded CivetWeb/Crow server to port `9400` to serve the exposition format.

### Stage 5: Kubernetes Deployment

Deploy the exporter as a cluster-wide service.

1. **Containerize**: Write a `Dockerfile` using a minimal base image (e.g., Ubuntu or distroless), compiling the static C++ binary.
2. **DaemonSet Manifest**: Create a `daemonset.yaml` that mounts the necessary host paths (`/sys/class/tenstorrent`, `/dev/tenstorrent/`) into the container.
3. **Configure ServiceMonitor**: If using the Prometheus Operator, deploy a `ServiceMonitor` to automatically scrape port `9400` on all node exporter pods.

---

## Simulator Caveats

When running against `ttsim-qemu`, physical telemetry (temperature, clock speed, power draw) will likely yield static placeholders. Ensure your exporter handles these edge cases gracefully without crashing.
