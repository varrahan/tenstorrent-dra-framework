# Dynamic Resource Allocation (DRA)

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

## How DRA is used in this repo

In this repository, DRA is the bridge between Tenstorrent hardware discovery and
workload scheduling:

- The DRA driver discovers devices exposed by `tt-kmd` on the VM/host path and
  maps them to DRA-ready models.
- The driver publishes:
  - **`DeviceClass` definitions** in
    [`src/dra/manifests/deviceclasses.yaml`](../../src/dra/manifests/deviceclasses.yaml).
  - **`ResourceSlice` inventory** in
    [`src/dra/manifests/resourceslices.yaml`](../../src/dra/manifests/resourceslices.yaml).
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
    kubectl apply -f src/dra/manifests/deviceclasses.yaml \
    -f src/dra/manifests/resourceslices.yaml
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

## References

- Kubernetes DRA guide: [Dynamic Resource Allocation][].
- Project DRA scope and initial driver behavior: [`src/dra/README.md`](../../src/dra/README.md).

[Dynamic Resource Allocation]: https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/
