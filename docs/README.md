# Project Documentation

This README is the required `/docs` entry point for agents and developers.
Read it before planning implementation work, then read the linked document that
matches the task.

## Required Context

This repository is in an early environment-validation stage for a Tenstorrent
Kubernetes DRA integration. Development is expected to happen inside or against
the QEMU `ttsim` Ubuntu VM.

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

## Validation Assets

Validation-only VM scripts and manifests live under the repository's `test/vm/`
directory. From inside the QEMU VM, run:

```bash
make -C test/vm vm-validate
```

Useful narrower targets are:

```bash
make -C test/vm load-tt-kmd
make -C test/vm kind-smoke
make -C test/vm kind-clean
```

## Source Layout

Runtime source code is split by component and language:

- `src/dra/`: Go implementation of the Kubernetes DRA driver.
- `src/telemetry/`: Python/FastAPI telemetry service.

## Documents

- [VM.md](VM.md): Booting and accessing the QEMU `ttsim` VM, validating Docker
  and `kind`, mounting Tenstorrent device paths into `kind`, and troubleshooting
  host-to-guest access.
