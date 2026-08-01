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
