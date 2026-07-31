# Telemetry and Metrics Exporter

This repository contains the blueprints for building a Kubernetes Dynamic Resource Allocation (DRA) metrics exporter for Tenstorrent hardware (Wormhole/Blackhole clusters).

This service acts as a DaemonSet to track hardware data that is actually
reported by safe node-local sources, then exposes it through Prometheus
`/metrics` and structured device inventory endpoints. It must not synthesize
capacity from card-spec tables.

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
3. **Extract Board Info**: Parse architecture, firmware-reported card identity,
   health status, PCI identity, and power-management metadata. Preserve missing
   values as absent/null instead of filling them with SKU-derived defaults.

### Stage 3: Tensix Core Utilization (TT-Metalium Profiler)

Tensix core usage must come from workload/runtime instrumentation. Integrate
with TT-Metalium to observe active `CoreGrids`; do not infer usage or available
core counts from public card tables.

Profiler results are process-local. The node exporter must not open a device
already owned by a workload merely to inspect profiler state.

1. **Profiler Build**: Use a Tracy-enabled TT-Metalium source build. In
   v0.73.1, `./build_metal.sh` enables Tracy by default; do not pass
   `--disable-profiler`. For a manual build use `-DENABLE_TRACY=ON`. Stock
   wheels may not include the support required by device profiling.
2. **Enable Profiler**: Set `TT_METAL_DEVICE_PROFILER=1`,
   `TT_METAL_PROFILER_MID_RUN_DUMP=1`, and
   `TT_METAL_PROFILER_CPP_POST_PROCESS=1` before TTNN initializes. Set
   `TT_METAL_PROFILER_DISABLE_DUMP_TO_FILES=1` when only the in-process
   publisher results are needed.
3. **Publish From The Workload**: After a synchronized workload iteration, call
   `ttnn.ReadDeviceProfiler(device)` and publish
   `ttnn.get_latest_programs_perf_data()` through the telemetry component's
   TTNN integration.
4. **Consume Node-Locally**: Atomically publish per-device workload snapshots
   under `/var/lib/tt-device-plugin/metalium-profiler`; configure the exporter
   with `--metalium-profiler-state-root`.
5. **Report Honest Semantics**: Use `ProgramAnalysisData.core_count` and
   `num_available_cores` as a recent program core-footprint/occupancy signal.
   Do not describe it as time-weighted busy percentage, and expire stale
   samples.

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

When running against `ttsim-qemu`, many physical telemetry files may be missing.
The exporter should handle absent temperature, clock, power, memory, and core
usage data gracefully without crashing or inventing placeholder values.

The current v0.73.1 profiler source build is not safe for an end-to-end run on
this QEMU profile: source-UMD topology discovery terminates QEMU. Direct
`libttsim_wh_v1.9.3.so` gets farther but does not implement the profiler NoC
write used by mid-run dumps. Use compatible physical hardware or a compatible
TTSim version for real dynamic samples; the VM remains suitable for exporter
and snapshot-contract validation.

## Optional TT-Metalium development environment

TT-Metalium is not part of the QEMU bridge health check. For experiments that
only need its Python APIs, use an isolated environment inside the VM instead of
`tt-installer`:

```bash
/home/ubuntu/.local/bin/uv venv /home/ubuntu/.venvs/tt-metalium --python /usr/bin/python3
source /home/ubuntu/.venvs/tt-metalium/bin/activate
uv pip install ttnn==0.73.1 pydantic
python -c 'import ttnn, ttnn.profiler; print(ttnn.__file__)'
tt-run --help
```

Point `TT_METAL_RUNTIME_ROOT` at the wheel's bundled runtime artifacts and send
profiler/TTNN reports to a writable work directory. Top-level `tt_lib` imports
may require optional model dependencies such as PyTorch; those are not required
for exporter or snapshot-contract validation.

The `ttnn==0.73.1` wheel exposes profiler result APIs but is not Tracy-enabled.
Dynamic workload occupancy requires a compatible source build:

```bash
git checkout v0.73.1
./build_metal.sh
```

Tracy is enabled by default in that release; do not pass `--disable-profiler`.
The equivalent manual CMake option is `-DENABLE_TRACY=ON`.
