# Project Documentation

This directory documents the implemented Tenstorrent DRA component. The
operational target is the QEMU `ttsim` Ubuntu VM and Kubernetes v1.34 or newer.

Use `tt-kmd` sysfs under `/sys/class/tenstorrent`, backing PCI sysfs, and the
Kubernetes DRA APIs as sources of truth. Mount discovered `/dev/tenstorrent`
paths explicitly into kind nodes. Do not use `tt-smi` for simulator discovery.

The Go implementation is rooted under `src/`. The node command publishes
ResourceSlices and node topology; the controller validates fabric topology and
creates exact claims and Pods for `TenstorrentWorkload` objects.

From inside the VM, the supported validation entry point is:

```bash
make -C test/vm vm-validate
```

Documents:

- [DRA.md](DRA.md): resource model, attributes, claims, topology, and workload APIs.
- [VM.md](VM.md): synthetic heterogeneous hardware and kind validation.
- [README.md](README.md): this project-wide routing document.
