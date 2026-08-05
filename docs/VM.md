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
The kind workers also receive the guest's `/sys/kernel/security` mount so
kubelet can verify and enforce the production AppArmor profile. The guest must
have AppArmor enabled; an empty or unavailable securityfs fails the baseline
workload gate closed. Before installing the chart, the harness extracts the
node-native `apparmor_parser` from Debian's package into each disposable worker
so containerd can load its RuntimeDefault profile without executing package
service scripts against the guest kernel.

```bash
make -C test/vm fake-hardware
make -C test/vm vm-validate
make -C test/vm vm-chaos
```

Use `make -C test/vm vm-certification` from a clean checkout to run both suites
as the exact-commit release gate and produce checksummed evidence.

Use `make -C test/vm kind-clean` to remove the disposable cluster. Synthetic
trees are validation fixtures only. The harness deliberately keeps the
production workload AppArmor profile enabled in `vm-validate`. The separate
chaos suite disables AppArmor explicitly so it can isolate lifecycle and
control-plane interruption behavior, captures evidence under `artifacts/`, and
always removes its disposable cluster. Cluster names, kind config, and image
names are fixed validation constants rather than runtime overrides. The
physical matrix, preflight, evidence rules, and remaining lab-owned release
gates are in [`CERTIFICATION.md`](CERTIFICATION.md).
