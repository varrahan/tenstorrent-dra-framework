# DRA Resource Model

This package contains the DRA-facing resource model.

The current code maps discovered Tenstorrent device nodes into a lightweight
internal model. The next implementation step is converting that model into
Kubernetes `resource.k8s.io/v1` objects, starting with `ResourceSlice`.

The package also carries dependency-light DeviceClass and card-spec models for
the supported Tenstorrent PCIe cards. Keep selector attributes aligned with the
attributes emitted by `ResourceSliceModel`:

- `tenstorrent.com/chipSeries`
- `tenstorrent.com/cardSeries`
- `tenstorrent.com/cardModel`

The supported card set is based on Tenstorrent's Wormhole and Blackhole PCIe
card specification tables:

- Wormhole: n150d, n150s, n300d, n300s
- Blackhole: p100a, p150a, p150b
