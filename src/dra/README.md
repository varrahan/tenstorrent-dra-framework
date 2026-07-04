# DRA Driver

This directory contains the Go implementation of the Tenstorrent Kubernetes
Dynamic Resource Allocation driver.

Initial scope:

- Discover Tenstorrent device nodes exposed by `tt-kmd`.
- Convert local device inventory into Kubernetes DRA resource data.
- Publish `ResourceSlice` objects for Kubernetes v1.34+ clusters.
- Provide cluster-scoped `DeviceClass` definitions for supported Tenstorrent
  chip and card series.
- Provide generated reference `ResourceClaim` manifests that distributed HPC and
  ML workloads can use to request whole-card or coarse-partition resources from
  a supported DeviceClass.

The first implementation milestone is local device discovery from
`/dev/tenstorrent`. Kubernetes API writes are intentionally kept out of the
initial discovery package so that hardware detection can be tested independently.

Go tests for this component live under [`test/`](test/) so test code stays
separate from the implementation packages.

## DeviceClasses, ResourceSlices, And ResourceClaims

Go source under [`internal/dra`](internal/dra/) is the source of truth for
Kubernetes `resource.k8s.io/v1` DeviceClass, ResourceSlice, and ResourceClaim
objects. The checked-in YAML manifests are generated artifacts. Regenerate them
after changing card specs, selectors, attributes, capacities, labels, or object
builders:

```bash
go generate ./src/dra
```

The generated DeviceClass manifest lives at
[`manifests/deviceclasses.yaml`](manifests/deviceclasses.yaml). It defines:

- `tenstorrent-wormhole-n150`
- `tenstorrent-wormhole-n300`
- `tenstorrent-blackhole-p100`
- `tenstorrent-blackhole-p150`

The classes intentionally group physical card variants that are equivalent from
a compute scheduling perspective. Wormhole d/s variants differ in cooling and
form factor, not DRA class. Blackhole p150a and p150b have equivalent compute
specs and share `tenstorrent-blackhole-p150`.

Apply it from inside the Kubernetes v1.34+ VM validation environment:

```bash
kubectl apply -f src/dra/manifests/deviceclasses.yaml
```

These classes select devices managed by `tenstorrent.com/dra` and require
ResourceSlices to publish `tenstorrent.com/chipSeries` and
`tenstorrent.com/cardSeries` attributes.

[`manifests/resourceslices.yaml`](manifests/resourceslices.yaml) is a generated
reference manifest that captures the compute-relevant attributes and capacities
that the DRA driver should publish from node-specific discovery. It is not live
inventory for a single VM node.

Scale-out cluster scheduling is the primary design goal. The DRA model should
prioritize card class, health, memory characteristics, and accelerator-to-accelerator
topology needed by distributed workloads. Fine-grained single-card sharing is
secondary and must not drive the early API shape.

Tensix cores are not exposed as an independently consumable scalar capacity.
They are modeled as a 2D mesh with contiguous-region allocation requirements.
ResourceSlice devices publish mesh and GDDR-controller locality attributes, and
ResourceClaims select only devices that advertise contiguous 2D mesh allocation
with localized GDDR controller layout.

GDDR controller topology is also published explicitly. Wormhole devices expose
six GDDR6 controllers per ASIC, while Blackhole devices expose eight per ASIC.
Blackhole devices also publish the larger RISC-V core count as a scheduling
attribute, and generated Blackhole ResourceClaims select for that capability.

[`manifests/resourceclaims.yaml`](manifests/resourceclaims.yaml) contains
generated namespaced reference claims. Each claim asks for one exact-count
device from one supported DeviceClass. These are intended for Kubernetes v1.34+
VM smoke validation and example workload manifests, not as cluster-wide policy.
