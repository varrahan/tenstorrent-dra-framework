# DRA Resource Model

This package contains the DRA-facing resource model.

The current code maps discovered Tenstorrent device nodes into a lightweight
internal model. The next implementation step is converting that model into
Kubernetes `resource.k8s.io/v1` objects, starting with `ResourceSlice`.

The package also carries dependency-light DeviceClass and compute-class spec
models for the supported Tenstorrent PCIe card classes. Keep selector
attributes aligned with the attributes emitted by `ResourceSliceModel`:

- `tenstorrent.com/chipSeries`
- `tenstorrent.com/cardSeries`

The supported card set is based on Tenstorrent's Wormhole and Blackhole PCIe
card specification tables, collapsed to compute-equivalent classes:

- Wormhole: n150, n300
- Blackhole: p100, p150

Do not include cooling, dimensions, or other physical variant data in this DRA
model unless it becomes a scheduling requirement.
