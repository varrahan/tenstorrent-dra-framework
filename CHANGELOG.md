# Changelog

All notable changes are recorded here. This project follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and Semantic
Versioning.

## [Unreleased]

## [0.1.0] - 2026-08-04

### Added

- Kubernetes 1.34 GA DRA allocation for exclusive Tenstorrent cards.
- Fail-closed inventory, health, sanitization, claim recovery, and
  topology-aware workload placement.
- Hardened controller and node-agent Helm deployment, metrics, dashboards,
  alerts, SLOs, and operational runbooks.
- Race and risk-weighted coverage gates, static analysis, vulnerability,
  dependency-license, secret, Helm, shell, and workflow policy checks.
- Reproducible Linux AMD64 and ARM64 binaries and multi-architecture images
  with embedded version, commit, and build-time metadata.
- SPDX JSON SBOMs, GitHub artifact attestations, keyless Cosign signatures,
  immutable image tags, and versioned OCI Helm releases.

### Security

- Pinned builder and distroless runtime images by digest and pinned all GitHub
  Actions by full commit.
- The runtime image and all chart containers use UID/GID 65532. Only the node
  agent retains its documented privileged host-device boundary.

[Unreleased]: https://github.com/varrahan/tenstorrent-dra-framework/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/varrahan/tenstorrent-dra-framework/releases/tag/v0.1.0
