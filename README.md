# Tenstorrent Kubernetes DRA

This repository contains a Kubernetes 1.34+ Dynamic Resource Allocation driver
for Tenstorrent nodes. Each node publishes its locally observed
`/dev/tenstorrent/<n>` devices as exclusive whole-card DRA devices. A node may
contain any number of cards and may mix Wormhole or Blackhole series.

The driver provides three runtime paths:

- `tt-dra-driver node` discovers cards, publishes ResourceSlices, serves the
  kubelet DRA protocol, sanitizes devices, writes per-claim CDI entries,
  monitors health, and publishes node links.
- `tt-dra-driver controller` validates the cluster fabric graph and reconciles
  connected multi-rank `TenstorrentWorkload` assignments.
- `tt-dra-driver list` prints the complete local inventory for diagnostics.
- `tt-dra-driver cleanup` is the guarded Helm pre-delete operation; it refuses
  to stop the release while Tenstorrent allocations or workloads are active.

Install the Helm chart from `deployments/helm/tenstorrent-dra`. It installs the
node DaemonSet, a two-replica leader-elected controller, three DeviceClasses,
three CRDs, hardened rollout and disruption policy, health and metrics
endpoints, an operations dashboard, and the required RBAC. Standard ResourceClaims can select card
attributes directly. `TenstorrentWorkload` adds deterministic connected-ring
placement for distributed rank Pods.

Inventory is collected from `tt-kmd` sysfs and PCI sysfs. The QEMU `ttsim` VM is
the validation target; do not use `tt-smi` for discovery or simulator checks.
Commands that require Docker, kind, Kubernetes, or `/dev/tenstorrent*` run
inside the VM. Host-side Go tests remain independent of hardware.

The implementation intentionally does not partition cards or install scheduler
plugins. Production compatibility, recovery, and security boundaries are in
[`docs/PRODUCTION.md`](docs/PRODUCTION.md). Deployment, observability, SLOs,
alerts, incident response, and lifecycle runbooks are in
[`docs/OPERATIONS.md`](docs/OPERATIONS.md).

See [docs/README.md](docs/README.md), [docs/DRA.md](docs/DRA.md), and
[docs/VM.md](docs/VM.md) for the model, APIs, and VM workflow.
