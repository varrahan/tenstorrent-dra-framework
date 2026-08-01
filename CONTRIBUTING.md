# Contributing

## Before opening a change

Read [AGENTS.md](AGENTS.md), [docs/README.md](docs/README.md), and the relevant
document under `docs/`. Runtime checks involving Docker, kind, Kubernetes DRA,
`tt-kmd`, or `/dev/tenstorrent*` run inside the QEMU VM; host checks are limited
to repository-safe tests and static validation.

The supported baseline is Kubernetes v1.34 or newer. Do not validate DRA work
against an older cluster, and do not use `tt-smi` for discovery or simulator
validation.

## Local checks

```bash
make check
```

Run VM validation from the guest with:

```bash
make -C test/vm vm-validate
```

Generated manifests are authoritative from Go source. Run `make generate` and
commit the resulting files when source builders change.

## Change requirements

- Keep whole-card exclusivity, fail-closed behavior, and configurable Linux
  paths intact.
- Add or update tests for behavior and failure scenarios.
- Keep QEMU-specific behavior under `test/vm/`.
- Record architecture changes in `adr/` and update `ROADMAP.md` when a gate or
  public interface changes.
- Do not include secrets, tenant payloads, or hardware telemetry in logs.

Pull requests must describe the validation performed, the Kubernetes versions
used, and any VM or hardware limitations.
