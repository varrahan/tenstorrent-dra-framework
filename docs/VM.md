# QEMU `ttsim` Validation

Run the validation from inside the QEMU Ubuntu guest. The guest must provide
Docker, kind, Kubernetes v1.34+, and `tt-kmd`. The driver reads the guest's
`/dev/tenstorrent`, `/sys/class/tenstorrent`, and PCI sysfs paths.

The minimal validation harness creates heterogeneous synthetic device trees,
mounts separate trees into two kind workers, installs the Helm chart, and checks
node-owned ResourceSlices and CDI-backed claims. It does not use `tt-smi`.

```bash
make -C test/vm fake-hardware
make -C test/vm vm-validate
```

Use `make -C test/vm kind-clean` to remove the disposable cluster. Synthetic
trees are validation fixtures only; physical hardware certification is outside
this repository's scope.
