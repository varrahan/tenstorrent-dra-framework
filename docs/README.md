# Project Documentation

This README is the required `/docs` entry point for agents and developers.
Read it before planning implementation work, then read the linked document that
matches the task.

## Required Context

This repository is in an early environment-validation stage for a Tenstorrent
Kubernetes DRA integration. The design center is scale-out HPC and ML clusters:
distributed workloads, topology-aware multi-card placement, health-aware
scheduling, and tenant isolation. Fine-grained single-card
multiprocess execution is a later-stage capability and should not drive early
architecture decisions ahead of cluster placement, isolation, and health.
Development is expected to happen inside or against the QEMU `ttsim` Ubuntu VM.

This repository is the implementation workspace, but the operational target is
the VM. Write source code, documentation, tests, and validation scripts in this
checkout; run or document runtime validation from the VM perspective unless the
task explicitly says otherwise. Commands that depend on Docker, `kind`,
`tt-kmd`, `/dev/tenstorrent*`, Kubernetes DRA APIs, kernel modules, or hardware
smoke validation are VM commands. Host-side execution is only appropriate for
lightweight checks that do not require the VM hardware environment, such as
formatting, Go unit tests, Python syntax checks, pure Python unit tests,
documentation checks, and dry-run Makefile expansion.

The project targets Kubernetes v1.34 or newer. Do not validate DRA behavior with
an older cluster. For `kind` workflows, pin the node image to a v1.34+ image and
verify that the `resource.k8s.io` API group serves DRA resources such as
`DeviceClass`, `ResourceClaim`, and `ResourceSlice`.

The QEMU guest exposes simulated Tenstorrent hardware through `tt-kmd` device
paths. Treat the VM's discovered `/dev/tenstorrent*` paths as the source of
truth, and mount those paths explicitly into `kind` node containers before
validating driver, scheduler, or pod-level behavior. Avoid broad `/dev/tt*`
globs in validation commands because they also match normal terminal devices.

Do not use `tt-smi` for simulator validation, DRA discovery, or normal VM setup.
Collect inventory from `tt-kmd` sysfs under `/sys/class/tenstorrent`, backing
PCI sysfs, and Kubernetes DRA allocation state.

## Validation Assets

Shared VM requirements, tests, and configuration that are independent of a
specific `src/` component live under the repository's `test/vm/host/` directory.

Existing validation-only VM scripts and manifests live under the repository's
`test/vm/` directory. From inside the QEMU VM, run:

```bash
make -C test/vm vm-validate
```

Useful narrower targets are:

```bash
make -C test/vm load-tt-kmd
make -C test/vm kind-smoke
make -C test/vm kind-clean
make -C test/vm fake-hardware
```

From the QEMU host, `make -C test/vm sync-test-vm` copies these validation
assets into the guest checkout.

## Source Layout

The Go DRA driver is rooted directly at `src/`:

- `src/`: Go DRA commands, internal packages, generated manifests, and tests.
- `test/vm/host/`: shared VM launcher/verification utilities and VM requirements.

## Documents

The complete project documentation is intentionally limited to these three files:

- [DRA.md](DRA.md): DRA concepts, driver layout, resource-model constraints,
  generated manifests, and supported card specifications.
- [VM.md](VM.md): QEMU `ttsim` boot and access, `tt-kmd`, Docker/`kind`, device
  mounting, validation, and troubleshooting.
- `README.md` (this file): authoritative project-wide context and document
  routing.
