# Tenstorrent Kubernetes DRA

This repository contains a Kubernetes 1.34+ Dynamic Resource Allocation driver
for Tenstorrent nodes. Each node publishes its locally observed
`/dev/tenstorrent/<n>` devices as exclusive whole-card DRA devices. A node may
contain any number of cards and may mix Wormhole, Blackhole, and future card
series.

The driver provides three runtime paths:

- `tt-dra-driver node` discovers cards, publishes ResourceSlices, serves the
  kubelet DRA protocol, writes per-claim CDI entries, and publishes node links.
- `tt-dra-driver controller` validates the cluster fabric graph and reconciles
  connected multi-rank `TenstorrentWorkload` assignments.
- `tt-dra-driver list` prints the complete local inventory for diagnostics.

Install the Helm chart from `deployments/helm/tenstorrent-dra`. It installs the
node DaemonSet, the single-replica controller, five DeviceClasses, three CRDs,
and the required RBAC. Standard ResourceClaims can select card attributes
directly. `TenstorrentWorkload` adds deterministic connected-ring placement for
distributed rank Pods.

Inventory is collected from `tt-kmd` sysfs and PCI sysfs. The QEMU `ttsim` VM is
the validation target; do not use `tt-smi` for discovery or simulator checks.
Commands that require Docker, kind, Kubernetes, or `/dev/tenstorrent*` run
inside the VM. Host-side Go tests remain independent of hardware.

The implementation intentionally does not partition cards, reset or scrub
hardware, install scheduler plugins, or provide a release/security pipeline.
Those concerns are outside this minimal DRA component.

See [docs/README.md](docs/README.md), [docs/DRA.md](docs/DRA.md), and
[docs/VM.md](docs/VM.md) for the model, APIs, and VM workflow.
