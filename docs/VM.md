# QEMU `ttsim` Validation

Run the validation from inside the QEMU Ubuntu guest. The guest must provide
Docker, kind, kubectl, Helm v4.2.3, Kubernetes v1.34+, and `tt-kmd`. The driver
reads the guest's `/dev/tenstorrent`, `/sys/class/tenstorrent`, and PCI sysfs
paths.

The validation harness creates heterogeneous synthetic device trees, mounts
separate trees into two kind workers, installs the Helm chart, and asserts exact
ResourceSlice and topology contents. It then runs a standard CDI-backed claim
and a connected two-rank `TenstorrentWorkload`, verifies per-container device
visibility, upgrades and rolls back the controller and node agents while a
claim remains prepared, and checks claim state, audit, Events, metrics, CDI,
teardown, and release uninstall cleanup. It does not use `tt-smi`.
Synthetic device nodes do not implement the `tt-kmd` reset ioctl, so the harness
uses the explicit validation-only `resetMode=noop` and `requireIOMMU=false`
overrides. Production defaults remain ioctl reset and dedicated IOMMU groups.

```bash
make -C test/vm fake-hardware
make -C test/vm vm-validate
```

Use `make -C test/vm kind-clean` to remove the disposable cluster. Synthetic
trees are validation fixtures only. The harness deliberately keeps the
production workload AppArmor profile enabled. Its cluster name, kind config,
image names, and AppArmor behavior are fixed constants rather than environment
overrides. Physical hardware certification remains a release gate in
[`TODO.md`](../TODO.md).
