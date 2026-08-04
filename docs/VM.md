# QEMU `ttsim` Validation

Run the validation from inside the QEMU Ubuntu guest. The guest must provide
Docker, kind, Kubernetes v1.34+, and `tt-kmd`. The driver reads the guest's
`/dev/tenstorrent`, `/sys/class/tenstorrent`, and PCI sysfs paths.

The validation harness creates heterogeneous synthetic device trees, mounts
separate trees into two kind workers, installs the Helm chart, and asserts exact
ResourceSlice and topology contents. It then runs a standard CDI-backed claim
and a connected two-rank `TenstorrentWorkload`, verifies per-container device
visibility, and checks claim state, audit, CDI, and teardown cleanup. It does not
use `tt-smi`.
Synthetic device nodes do not implement the `tt-kmd` reset ioctl, so the harness
uses the explicit validation-only `resetMode=noop` and `requireIOMMU=false`
overrides. Production defaults remain ioctl reset and dedicated IOMMU groups.

```bash
make -C test/vm fake-hardware
make -C test/vm vm-validate
```

The production workload AppArmor profile remains enabled by default. On a
nested development host whose kernel has no AppArmor support, use the explicit
synthetic-only override:

```bash
SYNTHETIC_DISABLE_WORKLOAD_APPARMOR=true make -C test/vm vm-validate
```

Use `make -C test/vm kind-clean` to remove the disposable cluster. Synthetic
trees and the AppArmor override are validation fixtures only. Physical hardware
certification remains a release gate in `TODO.md`.
