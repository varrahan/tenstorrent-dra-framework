# Tenstorrent Kubernetes Orchestration: System Architecture & Agents

## Mandatory Codex Startup Instructions

Before Codex plans work, edits files, runs tests, or makes implementation decisions, it must load the project documentation context from the repository's `/docs` directory.

Codex must:

1. Recursively find every README-style file under `/docs`.
2. Read each discovered README in full before acting.
3. Treat those README files as authoritative project context, second only to this `AGENTS.md` file and explicit user instructions.
4. Prefer implementation details, architecture notes, setup steps, and constraints from `/docs` over assumptions from general Kubernetes, DRA, Tenstorrent, or QEMU knowledge.
5. Mention which `/docs` README files were read when summarizing non-trivial work.
6. If `/docs` is missing or contains no README-style files, explicitly note that before proceeding and continue with the best available repository context.

Recommended discovery command from the repository root:

```bash
find ./docs -type f \
  \( -iname 'README' -o -iname 'README.*' -o -iname '*README*' \) \
  -print | sort
```

If the environment exposes `/docs` as an absolute path instead of `./docs`, use:

```bash
find /docs -type f \
  \( -iname 'README' -o -iname 'README.*' -o -iname '*README*' \) \
  -print | sort
```

Codex should not rely on stale memory of the project. Re-read the relevant `/docs` README files at the start of each new task, and re-read any README whose surrounding files are modified during the task.

## Workspace and Runtime Assumptions

This repository is the implementation workspace. Source code, documentation,
tests, and validation scripts are edited here.

The operational target is the QEMU `ttsim` Ubuntu VM. Unless explicit user
instructions say otherwise, commands that depend on Docker, `kind`, `tt-kmd`,
`/dev/tenstorrent*`, Kubernetes DRA APIs, kernel modules, or hardware smoke
validation must be written and verified from the VM perspective.

Host-side execution is acceptable for lightweight repository checks that do not
require the VM hardware environment, such as formatting, Go unit tests, Python
syntax checks, pure Python unit tests, documentation checks, and dry-run
Makefile expansion. Do not assume the host exposes Tenstorrent device paths or a
usable Kubernetes-in-`kind` runtime.

Validation-only VM assets belong under `test/vm/`. Documentation should make it
clear when a command is expected to run inside the VM.

## System Purpose

The purpose of this system is to bridge the gap between Tenstorrent ASIC hardware and Kubernetes container orchestration through a hardware-software co-design approach.

Specifically, this system transitions the cluster from legacy, integer-based device plugins to a highly intelligent, topology-aware control plane using the Kubernetes Dynamic Resource Allocation (DRA) framework. DRA enables workloads to request specialized hardware based on device attributes rather than simple counts. By implementing this system, you ensure strict tenant isolation, optimal scheduling based on physical Ethernet ring interconnects, and deep telemetry for real-time machine learning deployment pipelines.

## Core Technology Stack

| Component | Technology | Purpose |
| :--- | :--- | :--- |
| **Host Environment** | QEMU-ttsim, Linux | Simulates the physical node and Tenstorrent ASIC hardware. Runs `tt-kmd` to expose simulated hardware paths. |
| **Container Engine** | Docker | Hosts the Kubernetes nodes running via `kind` and allows for isolated compilation and testing of driver components. |
| **Orchestration** | `kind`, Kubernetes v1.34+ | Kubernetes v1.34+ is strictly required, as the Dynamic Resource Allocation (DRA) API reached General Availability (GA). |
| **DRA Driver / Resource Allocator** | Go, C/C++ | Go is the industry standard for writing Kubernetes Custom Resource Definitions (CRDs) and operators. C/C++ is utilized for high-performance bindings to interface directly with `tt-kmd`. |
| **Telemetry Agent** | Python, FastAPI | Provides a lightweight, high-performance web framework to continuously scrape hardware states and expose them as a Prometheus-scrapeable endpoint. |
| **Project & Context Management** | Obsidian | Used to structure project architecture, track custom K8s YAML manifests, and map out hardware topology definitions. |

## Key Implementation Phases

* **Phase 1: Foundation (Kubernetes v1.34+)** Configure `kind` to mount the QEMU `/dev/tenstorrent` paths directly into the virtual nodes.
* **Phase 2: The DRA Driver (Go & C++)** The driver will publish `ResourceSlices` to the Kubernetes API server, detailing specific attributes of the Tenstorrent cards.
* **Phase 3: Telemetry & Observability (Python & FastAPI)** Deploy the FastAPI container alongside the DRA driver to monitor device health and expose metrics.

---

## Core Agents & Daemons

### 1. Resource Allocator Agent (DRA Driver)

**Role:** Handles fine-grained hardware scheduling and allocation via the Kubernetes Dynamic Resource Allocation (DRA) API.

**Responsibilities:**

* Replaces legacy integer-based K8s device plugins, requiring a deep hardware-software co-design approach to bridge the K8s control plane with the ASIC.
* Parses custom resource claims, such as allocating specific Tensix core groups or SRAM partitions instead of only passing a whole PCIe device.
* Interfaces securely with the Kubelet to manage device cgroups and paths within the containerized environments.

### 2. Telemetry & Observability Agent

**Role:** Exposes real-time hardware metrics for cluster administrators and automated scaling engines.

**Responsibilities:**

* Scrapes driver telemetry, including thermal states, power draw, and Network-on-Chip (NoC) congestion, directly from `/sys/class/tenstorrent/` or `tt-smi`.
* Exposes a Prometheus-scrapeable endpoint. Structured as a lightweight Python application utilizing FastAPI to serve metrics efficiently.
* Provides the necessary visibility to debug bottlenecks in real-time machine learning deployment pipelines.

### 3. Topology Discovery Agent

**Role:** Maps physical scale-out interconnects to influence the Kubernetes scheduler.

**Responsibilities:**

* Probes the inter-chip Ethernet links and ring topologies across the cluster's nodes.
* Maps out the physical routing infrastructure. Guarantees that distributed MPI/Buda jobs are placed on cards with direct, low-latency physical links rather than traversing the slower host network.
* Generates and dynamically updates `NodeResourceTopology` Custom Resource Definitions (CRDs).

### 4. Hardware Janitor Agent (Sanitization & Health)

**Role:** Enforces strict tenant isolation and hardware stability across pod lifecycles.

**Responsibilities:**

* **Pre-flight Scrubbing:** Triggers ASIC resets via `tt-kmd` before a new container starts, ensuring memory states are wiped and preventing data leakage between deployments.
* **Continuous Health Checks:** Identifies OOM faults or unrecoverable hardware hangs. If a device fails, it dynamically taints the K8s node or cordons the specific accelerator path, preventing new pods from being black-holed.
