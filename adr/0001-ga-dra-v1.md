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
