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
