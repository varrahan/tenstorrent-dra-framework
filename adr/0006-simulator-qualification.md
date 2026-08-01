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
