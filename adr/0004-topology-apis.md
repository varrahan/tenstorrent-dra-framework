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
