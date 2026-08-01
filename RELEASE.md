# Release policy

Releases use semantic versioning. A major release may remove APIs or supported
runtime behavior; a minor release adds backward-compatible features; a patch
release contains compatible fixes and security updates.

Every release must pass the roadmap gates, use a clean source tag, publish
immutable linux/amd64 and linux/arm64 images plus the Helm chart to GHCR, and
include checksums, SBOMs, SLSA provenance, keyless Cosign signatures, a
compatibility matrix, release notes, and upgrade/rollback notes.

Kubernetes v1.34 is the minimum supported minor. Version skew, CRD conversion,
persisted node-state compatibility, and rollback boundaries must be documented
before publishing an artifact.

The v1.0 release is simulator-qualified. Physical-hardware certification is a
separate post-v1 qualification program.
