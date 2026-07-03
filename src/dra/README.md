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

The static Kubernetes `resource.k8s.io/v1` DeviceClass manifest lives at
[`manifests/deviceclasses.yaml`](manifests/deviceclasses.yaml). It defines:

- `tenstorrent-wormhole-n150d`
- `tenstorrent-wormhole-n150s`
- `tenstorrent-wormhole-n300d`
- `tenstorrent-wormhole-n300s`
- `tenstorrent-blackhole-p100a`
- `tenstorrent-blackhole-p150a`
- `tenstorrent-blackhole-p150b`

There is no `blackhole-p300` DeviceClass. The Blackhole PCIe card
documentation lists p100a, p150a, and p150b cards.

Apply it from inside the Kubernetes v1.34+ VM validation environment:

```bash
kubectl apply -f src/dra/manifests/deviceclasses.yaml
```

These classes select devices managed by `tenstorrent.com/dra` and require
ResourceSlices to publish `tenstorrent.com/chipSeries` and
`tenstorrent.com/cardSeries` and `tenstorrent.com/cardModel` attributes.

[`manifests/resourceslices.yaml`](manifests/resourceslices.yaml) is a reference
manifest that captures the card attributes and capacities that the DRA driver
should publish from node-specific discovery. It is not live inventory for a
single VM node.
