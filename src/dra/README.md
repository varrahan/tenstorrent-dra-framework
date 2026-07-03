# DRA Driver

This directory contains the Go implementation of the Tenstorrent Kubernetes
Dynamic Resource Allocation driver.

Initial scope:

- Discover Tenstorrent device nodes exposed by `tt-kmd`.
- Convert local device inventory into Kubernetes DRA resource data.
- Publish `ResourceSlice` objects for Kubernetes v1.34+ clusters.
- Provide cluster-scoped `DeviceClass` definitions for supported Tenstorrent
  chip and card series.

The first implementation milestone is local device discovery from
`/dev/tenstorrent`. Kubernetes API writes are intentionally kept out of the
initial discovery package so that hardware detection can be tested independently.

Go tests for this component live under [`test/`](test/) so test code stays
separate from the implementation packages.

## DeviceClasses And ResourceSlices

Go source under [`internal/dra`](internal/dra/) is the source of truth for
Kubernetes `resource.k8s.io/v1` DeviceClass and ResourceSlice objects. The
checked-in YAML manifests are generated artifacts. Regenerate them after
changing card specs, selectors, attributes, capacities, labels, or object
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
