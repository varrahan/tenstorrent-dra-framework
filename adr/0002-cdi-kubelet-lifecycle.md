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
