# QEMU `ttsim` Validation

Run the validation from inside the QEMU Ubuntu guest. The guest must provide
Docker, kind, Kubernetes v1.34+, and `tt-kmd`. The driver reads the guest's
`/dev/tenstorrent`, `/sys/class/tenstorrent`, and PCI sysfs paths.

The minimal validation harness creates heterogeneous synthetic device trees,
mounts separate trees into two kind workers, installs the Helm chart, and prints
node-owned ResourceSlices and node topology. It does not yet exercise a
CDI-backed claim and does not use `tt-smi`.
Synthetic device nodes do not implement the `tt-kmd` reset ioctl, so the harness
uses the explicit validation-only `resetMode=noop` and `requireIOMMU=false`
overrides. Production defaults remain ioctl reset and dedicated IOMMU groups.

```bash
make -C test/vm fake-hardware
make -C test/vm vm-validate
```

Use `make -C test/vm kind-clean` to remove the disposable cluster. Synthetic
trees are validation fixtures only. CDI end-to-end and physical hardware
certification remain release gates in `TODO.md`.
