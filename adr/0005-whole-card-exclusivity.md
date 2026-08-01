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
