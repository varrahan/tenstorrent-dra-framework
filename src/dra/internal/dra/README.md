# DRA Resource Model

This package contains the DRA-facing resource builders.

The current code maps discovered Tenstorrent device nodes, supported card specs,
and example workload requests into real Kubernetes `resource.k8s.io/v1` objects.
Go source in this package is the source of truth for generated manifests under
`src/dra/manifests/`.

Keep DeviceClass selector attributes aligned with attributes emitted by
ResourceSlice device builders:

- `tenstorrent.com/chipSeries`
- `tenstorrent.com/cardSeries`

Attribute and capacity names must remain valid Kubernetes DRA
`QualifiedName` identifiers. Use camelCase identifiers after the
`tenstorrent.com/` prefix, not hyphenated names.

The supported card set is based on Tenstorrent's Wormhole and Blackhole PCIe
card specification tables, collapsed to compute-equivalent classes:

- Wormhole: n150, n300
- Blackhole: p100, p150

Do not include cooling, dimensions, or other physical variant data in this DRA
model unless it becomes a scheduling requirement.
