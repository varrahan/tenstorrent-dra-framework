# DRA Resource Model

This package contains the DRA-facing resource builders.

The current code maps discovered Tenstorrent device nodes, supported card specs,
and example workload requests into real Kubernetes `resource.k8s.io/v1` objects.
Go source in this package is the source of truth for generated manifests under
`src/dra/manifests/`.

The resource model is oriented toward scale-out HPC and ML clusters. Prefer
attributes that help the scheduler place distributed jobs on compatible,
healthy, topology-adjacent accelerators. Do not make fine-grained single-card
multiprocess sharing the default abstraction.

Keep DeviceClass selector attributes aligned with attributes emitted by
ResourceSlice device builders:

- `tenstorrent.com/chipSeries`
- `tenstorrent.com/cardSeries`

Attribute and capacity names must remain valid Kubernetes DRA
`QualifiedName` identifiers. Use camelCase identifiers after the
`tenstorrent.com/` prefix, not hyphenated names.

Do not model Tensix cores as independently allocatable scalar capacity.
Tenstorrent workloads need spatial allocation over contiguous regions of the 2D
Tensix mesh. ResourceSlice objects should publish topology attributes such as
`tenstorrent.com/tensixTopology`,
`tenstorrent.com/tensixAllocation`, and
`tenstorrent.com/gddrControllerLayout`; ResourceClaims should select for those
capabilities. Actual placement of subregions and GDDR-local blocks belongs in
the allocator/kubelet plugin, using device-discovered topology as the source of
truth.

ResourceSlice objects also publish GDDR controller counts. Wormhole chips have
six GDDR6 controllers per ASIC, and Blackhole chips have eight per ASIC.
Blackhole big RISC-V cores are represented as a scheduling attribute
(`tenstorrent.com/bigRISCVCoreCount`), not as scalar capacity.

The supported card set is based on Tenstorrent's Wormhole and Blackhole PCIe
card specification tables, collapsed to compute-equivalent classes:

- Wormhole: n150, n300
- Blackhole: p100, p150

Do not include cooling, dimensions, or other physical variant data in this DRA
model unless it becomes a scheduling requirement.
