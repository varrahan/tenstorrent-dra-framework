# Tenstorrent Kubernetes DRA Framework

This project is a Kubernetes orchestration layer for Tenstorrent accelerator
hardware. Its design center is scale-out HPC and ML clusters where distributed
jobs need topology-aware placement, health-aware scheduling, and operational
visibility across many accelerator nodes.

The long-term direction is to move beyond legacy integer-count device plugins
and use Kubernetes Dynamic Resource Allocation (DRA) so workloads can request
hardware by attributes, topology, and health state. Instead of only asking for
"one accelerator", a workload should be able to express requirements such as a
specific device class, memory profile, health state, or accelerator placement
with direct low-latency links to peer devices. Fine-grained sub-card sharing,
such as core-group or memory-region allocation, is a later-stage capability and
must not take priority over cluster-scale placement, isolation, and telemetry.

## What This Project Entails

The repository is the foundation for a hardware-software co-design effort that
connects Tenstorrent devices, kernel driver state, Kubernetes scheduling, and
cluster observability.

The project targets the following environment:

| Area | Technology | Role |
| --- | --- | --- |
| Hardware simulation | QEMU `ttsim` | Provides a simulated Tenstorrent Wormhole device for development and testing. |
| Kernel interface | `tt-kmd` | Exposes Tenstorrent device paths and driver state to the guest system. |
| Local Kubernetes | Docker and `kind` | Runs Kubernetes nodes for development and validation. |
| Resource allocation | Kubernetes DRA | Publishes and allocates accelerator resources through `ResourceSlice` and resource claim APIs. |
| Driver implementation | Go and C/C++ | Integrates Kubernetes control-plane logic with lower-level device interfaces. |
| Telemetry | C++ metrics exporter | Exposes accelerator health and performance metrics for Prometheus-style scraping. |

Development is expected to happen inside or against the QEMU `ttsim` VM
described in [docs/VM.md](docs/VM.md), where Docker, `kind`, Kubernetes tooling,
and simulated Tenstorrent hardware are available.

## Core Features

### Dynamic Resource Allocation

The DRA driver is intended to publish Tenstorrent accelerator resources to the
Kubernetes API as structured resources instead of opaque integer counts. This
allows scheduling decisions to consider hardware attributes, device health, and
allocation constraints.

Planned DRA capabilities include:

- Publishing Tenstorrent devices through Kubernetes `ResourceSlice` objects.
- Supporting resource claims for accelerator-specific properties needed by
  distributed HPC and ML workloads.
- Allocating whole accelerators or coarse accelerator partitions as the default
  scheduling units for scale-out jobs.
- Deferring fine-grained single-card sharing until device isolation, reset,
  accounting, and runtime support are proven.
- Coordinating with kubelet so allocated devices and paths are exposed only to
  the pods that requested them.

### Topology-Aware Scheduling

Tenstorrent deployments can depend heavily on device-to-device connectivity.
This project is designed to discover and expose physical topology information so
distributed workloads can be placed on accelerators with suitable interconnects.

Planned topology capabilities include:

- Discovering local accelerator inventory and device attributes.
- Mapping Ethernet ring and scale-out links between accelerator devices.
- Publishing topology metadata for scheduler consumption.
- Supporting distributed job placement that prefers direct accelerator links
  over slower host-network paths.
- Treating multi-card and multi-node placement as a core scheduling requirement,
  not an optional optimization.

### Telemetry and Observability

The telemetry component is intended to provide continuous visibility into
accelerator state. Cluster operators and automated systems should be able to
observe health and performance characteristics without manually inspecting each
node.

Planned telemetry capabilities include:

- Scraping Tenstorrent driver and device state from sources such as
  `/sys/class/tenstorrent/` or `tt-smi`.
- Reporting thermal state, power draw, NoC congestion, and fault indicators.
- Serving metrics from a lightweight C++ exporter.
- Exposing Prometheus-compatible endpoints for monitoring and alerting.

### Tenant Isolation and Hardware Hygiene

The project includes a hardware janitor role to protect workloads from stale
device state and prevent unhealthy accelerators from accepting new work.

Planned isolation and health capabilities include:

- Resetting or scrubbing devices before allocation to a new workload.
- Preventing memory state leakage between tenants.
- Detecting accelerator hangs, OOM conditions, and unrecoverable faults.
- Tainting nodes or cordoning affected accelerator paths when hardware becomes
  unhealthy.

### QEMU-Based Development Loop

The repository supports a local development flow built around a QEMU `ttsim`
Ubuntu VM. This makes it possible to validate Kubernetes integration work
against simulated Tenstorrent hardware before requiring physical cards.

The VM workflow supports:

- Booting a simulated Tenstorrent Wormhole device with custom QEMU support.
- Accessing the guest over SSH through host port forwarding.
- Running Docker and `kind` inside the VM.
- Creating disposable Kubernetes clusters for driver and manifest testing.
- Verifying simulated hardware visibility with tools such as `lspci`, `lsmod`,
  `dmesg`, and `/dev` discovery.

## Project Phases

1. Foundation: boot the QEMU `ttsim` VM, verify `tt-kmd`, and run Kubernetes
   with `kind`.
2. DRA driver: publish Tenstorrent resources through Kubernetes DRA APIs and
   allocate whole-card or coarse-partition resources to distributed workloads.
3. Telemetry: expose accelerator health and performance metrics through a
   C++ metrics exporter.
4. Topology: discover accelerator interconnects and surface topology metadata to
   scheduling components.
5. Hardware hygiene: add reset, scrubbing, health-check, and cordon/taint flows
   for tenant isolation and reliability.

## Repository Status

This repository is currently in an early architecture and environment-validation
stage. The existing documentation and validation assets focus on booting and
accessing the QEMU `ttsim` VM. Initial source scaffolds now exist for the Go
DRA driver and C++ metrics exporter; Kubernetes API integration, topology
discovery, and hardware janitor flows will be added as the implementation is
built out.

## Source Layout

- `src/dra/`: Go implementation of the Kubernetes DRA driver.
- `src/telemetry/`: C++ Tenstorrent metrics exporter.
- `vm/`: shared VM requirements, tests, and configuration independent of
  source components.
- `test/vm/`: VM validation scripts and kind smoke-test manifests.

## Documentation

- [docs/README.md](docs/README.md): required documentation entry point with
  project-wide DRA, Kubernetes version, and `kind` device-mount constraints.
- [docs/VM.md](docs/VM.md): QEMU `ttsim` VM boot, SSH access, kind validation,
  and troubleshooting guide.
- [AGENTS.md](AGENTS.md): project architecture notes and required agent workflow
  instructions.
