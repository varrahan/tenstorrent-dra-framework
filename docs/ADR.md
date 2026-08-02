# Architecture Decision Records

This document consolidates the current architecture decisions. New
architecture decisions should be added here with a unique number, status, date,
decision, and consequences.

---

# ADR 0001: GA DRA v1 baseline

- Status: Accepted
- Date: 2026-08-01

## Decision

The v1.0 compatibility baseline is Kubernetes v1.34 and the selected current
stable release, using only GA `resource.k8s.io/v1` DRA behavior. The driver
name is permanently `dra.tenstorrent.com`.

Alpha or non-baseline features such as partitionable devices, consumable
capacity, and device taints are not required for v1.0 behavior.

## Consequences

ResourceSlice publication and claim selection must work on v1.34 without
feature-gate assumptions. Newer Kubernetes behavior may be tested, but cannot
become a v1.0 requirement without a compatibility decision.

---

# ADR 0002: Kubelet DRA and CDI lifecycle

- Status: Accepted
- Date: 2026-08-01

## Decision

The node daemon implements the official kubelet DRA plugin contract. It exposes
allocated cards through per-card CDI entries and never mounts the complete
`/dev/tenstorrent` directory into workloads. Prepare and unprepare operations
are idempotent and ownership-checked.

## Consequences

containerd and CRI-O CDI paths are configuration, not hard-coded assumptions.
Claim state and CDI records require crash-safe persistence and startup
reconciliation before the daemon reports readiness.

---

# ADR 0003: Persistent node-local state

- Status: Accepted
- Date: 2026-08-01

## Decision

Prepared-claim ownership, lifecycle generation, and quarantine records are
stored as versioned JSON under a configurable node-local state directory. Files
are written to temporary siblings, fsynced where supported, atomically renamed,
and reconciled against Kubernetes claims, CDI files, and live inventory.

## Consequences

Node-local state is reconstructable rather than authoritative. Ambiguous or
corrupt records fail closed and cannot release another claim's device.

---

# ADR 0004: Topology API layers

- Status: Accepted
- Date: 2026-08-01

## Decision

Topology is represented at three layers: DRA attributes for device selection,
NodeResourceTopology for node-local PCI/NUMA zones and costs, and the
`TenstorrentNodeTopology` and `TenstorrentFabricTopology` v1alpha1 CRDs for
observed and validated cross-node fabric graphs.

## Consequences

The last valid graph remains visible for diagnosis, but stale or contradictory
observations are ineligible for new topology-required allocations. Graph
generations are immutable and must reconcile completely before scheduling uses
them.

---

# ADR 0005: Exclusive whole-card allocation

- Status: Accepted
- Date: 2026-08-01

## Decision

One physical card is the v1.0 allocation unit and may belong to only one
ResourceClaim at a time. Tensix core groups, SRAM regions, and GDDR-local
partitions are not independently allocatable.

## Consequences

The scheduler sees only healthy, whole-card devices. Any future partitioning
requires new isolation, reset, accounting, runtime, and compatibility evidence;
it cannot be introduced as an implicit optimization.

---

# ADR 0006: Simulator-qualified v1.0

- Status: Accepted
- Date: 2026-08-01

## Decision

v1.0 is released only after QEMU `ttsim`, simulated character-device injection,
synthetic multi-node topology, hard placement, failure recovery, and Helm
lifecycle qualification pass. Physical Wormhole and Blackhole certification is
explicitly outside the v1.0 gate.

## Consequences

Production code uses configurable Linux paths and versioned hardware backends;
QEMU launchers, simulator libraries, SSH ports, and fake sysfs remain under
`test/vm/`. Physical support claims must identify a later certification effort.

---

# ADR 0007: Inventory identity and provenance

## Status

Accepted

## Decision

The inventory normalizer uses the PCI BDF as the stable physical identity,
serialized as `pci-<BDF>`, and orders snapshots by that identity. A device is
eligible only when its character device, complete PCI identity, Tenstorrent
vendor, known chip/card combination, and health are all observed and valid.
Duplicate identities and uncertain observations are retained for diagnostics
but removed from eligibility.

Every canonical field carries provenance identifying the backend class, source
path when available, and observation timestamp. Scheduler projections exclude
host paths, permissions, and major/minor values; those remain node-local
runtime data.

## Consequences

- Enumeration order and restarts do not change device identity.
- Hotplug and malformed data isolate only the affected device.
- Missing live capacity is represented as missing; public card specifications
  never synthesize availability.
- Backends can be replaced by fixture or physical-KMD implementations without
  changing the canonical schema.
