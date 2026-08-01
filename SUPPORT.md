# Support policy

## Scope

v1.0 is simulator-qualified on Kubernetes v1.34 and the pinned current-stable
release, with Linux amd64 and arm64 images and CDI-capable containerd or CRI-O.
QEMU `ttsim` and the documented `tt-kmd` bridge are validation environments.

Physical Linux systems with compatible KMD and sysfs layouts are architecturally
supported but are not physically certified by the simulator GA release.

## Requests

Use GitHub issues for reproducible defects, documentation errors, and feature
requests. Include release version, Kubernetes version, runtime, architecture,
backend, relevant status conditions, Events, logs, and the exact validation
command. Redact tenant data and credentials.

Use the security process in [SECURITY.md](SECURITY.md) for vulnerabilities.
