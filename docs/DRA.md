# Dynamic Resource Allocation and Hardware Model

## What DRA is

Kubernetes Dynamic Resource Allocation (DRA) is the control-plane framework Kubernetes
uses for **non-CPU/GPU-style hardware resources that need rich device metadata and
lifecycle management**.

Instead of treating an accelerator only as a count (`nvidia.com/gpu: 1`), DRA lets
workloads request resources by attributes and capability requirements (for example,
chip family, memory size, interconnect topology, or capacities) and lets the
scheduler allocate exact devices on a per-request basis.

This project uses DRA to expose Tenstorrent accelerators to Kubernetes as structured
resources instead of opaque scalar values. The primary target is scale-out HPC
and ML cluster scheduling, where distributed jobs need compatible cards, healthy
devices, and low-latency topology rather than arbitrary single-card sharing.

## Key DRA terminology

- **Device driver (resource provider)**: Component that discovers hardware and
  publishes DRA objects.
- **`DeviceClass`**: A named resource class used by workloads to describe what
  kind of device they need (for example, `tenstorrent-wormhole-n150` or
  `tenstorrent-blackhole-p150`).
- **`ResourceSlice`**: The object where a driver publishes one or more concrete
  devices, including per-device attributes, capacities, and node placement.
- **`Device` (inside a ResourceSlice)**: The concrete unit (card/chip instance)
  with fields such as path, IDs, and performance/capacity characteristics.
- **`ResourceClaim`**: A request object created by a workload (Pod or controller)
  that binds to a specific `DeviceClass` and optional capacity requirements.
- **`ResourceClaimTemplate`**: A template for repeated/templated claims.
- **`status.allocation`**: The scheduler/allocator result that identifies the
  concrete selected device and consumed capacity.
- **`resource.k8s.io`**: The API group where DRA types (`DeviceClass`,
  `ResourceSlice`, `ResourceClaim`, `ResourceClaimTemplate`) are defined.

## Driver implementation

The Go driver is rooted directly under `src/`:

- [`cmd/tt-dra-driver`](../src/cmd/tt-dra-driver/): device discovery command.
- [`cmd/tt-dra-manifests`](../src/cmd/tt-dra-manifests/): manifest generator.
- [`internal/device`](../src/internal/device/): node-local device discovery.
- [`internal/dra`](../src/internal/dra/): typed DRA builders and validation.
- [`manifests`](../src/manifests/): generated reference YAML.
- [`test`](../src/test/): Go tests and generated-artifact checks.

Device discovery is intentionally independent of Kubernetes API writes. Go
source in `src/internal/dra` is authoritative; checked-in YAML is generated:

```bash
go generate ./src
```

The supported DeviceClasses are:

- `tenstorrent-wormhole-n150`
- `tenstorrent-wormhole-n300`
- `tenstorrent-blackhole-p100`
- `tenstorrent-blackhole-p150`

They select devices managed by `dra.tenstorrent.com` and require live
ResourceSlices to publish `tenstorrent.com/chipSeries` and
`tenstorrent.com/cardSeries`. The checked-in ResourceSlices and ResourceClaims
are reference manifests for validation and examples, not live inventory or
cluster policy.

## How DRA is used in this repo

In this repository, DRA is the bridge between Tenstorrent hardware discovery and
workload scheduling:

- The DRA driver discovers devices exposed by `tt-kmd` on the VM/host path and
  maps them to DRA-ready models.
- The driver publishes:
  - **`DeviceClass` definitions** in
    [`src/manifests/deviceclasses.yaml`](../src/manifests/deviceclasses.yaml).
  - **`ResourceSlice` inventory** in
    [`src/manifests/resourceslices.yaml`](../src/manifests/resourceslices.yaml).
- Tenstorrent-specific attributes used in DRA objects (chip series, card series,
  clock, memory/bandwidth, link interfaces, topology flags) are encoded as
  device attributes/capacities so the scheduler can make better placement decisions
  than integer-only counts.
- This enables use cases like topology-aware placement for multi-device AI jobs
  and HPC workloads, while still allowing the scheduler to distinguish Blackhole
  vs Wormhole characteristics.

## Why this matters here

This project needs DRA for four reasons:

1. **Scale-out placement**: Distributed HPC and ML workloads can request devices
   that satisfy topology, class, and health requirements across nodes.
2. **Hardware specificity**: Workloads can request a capability profile instead of
   just “a card,” which is critical for mixed card families.
3. **Topology and isolation intent**: Future work can extend request/selection
   logic to enforce interconnect-aware placement and tenant-safe allocation.
4. **API-native lifecycle**: Scheduling, reservation, and status flow follow the
   Kubernetes control-plane model rather than out-of-band scripts or node-side heuristics.

Fine-grained sub-card allocation, such as selecting Tensix subregions for
multiple processes on the same ASIC, is intentionally secondary. It should be
added only when the allocator, kubelet plugin, Tenstorrent runtime, and hardware
reset/scrub flows can enforce isolation and account for usage reliably.

## Minimal workflow in practical terms

1. Tenstorrent nodes expose local accelerators.
2. DRA objects (`DeviceClass` + `ResourceSlice`) are available in Kubernetes.
3. A Pod requests a resource through `spec.resourceClaims` (or a
   `ResourceClaimTemplate`).
4. The scheduler considers the request against slice/device attributes and makes
   an allocation decision.
5. The allocation details are recorded, and workload placement follows that
   contract.

## Short end-to-end example

The flow for this repository looks like:

1. Install DRA manifests:

    ```bash
    kubectl apply -f src/manifests/deviceclasses.yaml \
    -f src/manifests/resourceslices.yaml
    ```

2. Create a reusable request template for a Tenstorrent class:

    ```yaml
    apiVersion: resource.k8s.io/v1
    kind: ResourceClaimTemplate
    metadata:
      name: tenstorrent-accel-claim-template
    spec:
      spec:
        devices:
          requests:
          - name: accel
            exactly:
              deviceClassName: tenstorrent-wormhole-n300
    ```

3. Reference that template from a Pod:

    ```yaml
    apiVersion: v1
    kind: Pod
    metadata:
      name: tt-infer
    spec:
      containers:
      - name: app
        image: your-app-image
        command: ["sleep", "infinity"]
      resourceClaims:
      - name: accel
        resourceClaimTemplateName: tenstorrent-accel-claim-template
    ```

When submitted, scheduler sees the Pod request, allocates a compatible Tenstorrent
device from `ResourceSlice`, records it on `ResourceClaim.status.allocation`, and
schedules the Pod onto the matching node.

## Resource-model constraints

Scale-out scheduling is the primary design goal. Live ResourceSlices must use
observed node-local data from `tt-kmd` sysfs, backing PCI sysfs, topology
discovery, Kubernetes allocation state, or workload profiler data. They must
not synthesize available capacity from public card tables.

Tensix cores are not independently allocatable scalar capacity. They form a 2D
mesh that requires contiguous-region allocation. ResourceSlices advertise
`tenstorrent.com/tensixTopology`, `tenstorrent.com/tensixAllocation`, and
`tenstorrent.com/gddrControllerLayout`; actual subregion and GDDR-local
placement belongs in the allocator and kubelet plugin after isolation, reset,
accounting, and runtime behavior are proven.

Wormhole exposes six GDDR6 controllers per ASIC; Blackhole exposes eight.
Blackhole also publishes its Big RISC-V core count as an attribute. Attribute
and capacity names must be valid Kubernetes DRA `QualifiedName` values, using
camelCase after the `tenstorrent.com/` prefix.

## Tenstorrent PCIe card reference

Public card specifications inform DeviceClass design and workload selection,
but are not a live inventory source.

### Blackhole

| Card | PCIe | AI clock | Tensix | SRAM | Memory | Bandwidth | BLOCKFP8 | TBP | Cooling | Interconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| p100a | 5.0 ×16 | Up to 1.35 GHz | 120 | 180 MB | 28 GB GDDR6 | 448 GB/s | 664 TFLOPS | 300 W | Active | No QSFP |
| p150a | 5.0 ×16 | Up to 1.35 GHz | 120 | 180 MB | 32 GB GDDR6 | 512 GB/s | 664 TFLOPS | 300 W | Active | 4 × QSFP-DD 800G |
| p150b | 5.0 ×16 | Up to 1.35 GHz | 120 | 180 MB | 32 GB GDDR6 | 512 GB/s | 664 TFLOPS | 300 W | Passive | 4 × QSFP-DD 800G |

Blackhole has 16 SiFive x280 Big RISC-V cores per processor. The p150a and
p150b variants are compute-equivalent and share one DRA class; their cooling
differences are operational rather than scheduling capabilities.

### Wormhole

| Card | PCIe | AI clock | ASICs | Tensix | SRAM | Memory | Bandwidth | FP8 / FP16 / BLOCKFP8 | TBP | Cooling | Interconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| n150d | 4.0 ×16 | 1 GHz | 1 | 72 | 108 MB | 12 GB GDDR6 | 288 GB/s | 262 / 74 / 148 TFLOPS | 160 W | Active | 2 × QSFP-DD 200G, 2 × Warp 100 |
| n150s | 4.0 ×16 | 1 GHz | 1 | 72 | 108 MB | 12 GB GDDR6 | 288 GB/s | 262 / 74 / 148 TFLOPS | 160 W | Passive | 2 × QSFP-DD 200G, 2 × Warp 100 |
| n300d | 4.0 ×16 | 1 GHz | 2 | 128 | 192 MB | 24 GB GDDR6 | 576 GB/s | 466 / 131 / 262 TFLOPS | 300 W | Active | 2 × QSFP-DD 200G, 2 × Warp 100 |
| n300s | 4.0 ×16 | 1 GHz | 2 | 128 | 192 MB | 24 GB GDDR6 | 576 GB/s | 466 / 131 / 262 TFLOPS | 300 W | Passive | 2 × QSFP-DD 200G, 2 × Warp 100 |

The d/s variants are compute-equivalent and differ primarily in cooling. n150
is a single-ASIC card; n300 is dual-ASIC. Use n150 for lower-power capacity and
n300 when aggregate compute and memory per node matter more.

## References

- Kubernetes DRA guide: [Dynamic Resource Allocation][].
- [Tenstorrent card index][cards]
- [Blackhole documentation][blackhole documentation]
- [Wormhole documentation][wormhole documentation]

[Dynamic Resource Allocation]: https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/
[blackhole documentation]: https://docs.tenstorrent.com/aibs/blackhole/index.html
[wormhole documentation]: https://docs.tenstorrent.com/aibs/wormhole/index.html
[cards]: https://tenstorrent.com/hardware/cards
