# Release and supply-chain operations

This is the release contract for binaries, the container image, and the Helm
chart. Releases are created only by `.github/workflows/release.yml` from a
`vMAJOR.MINOR.PATCH` tag in the canonical repository. The workflow token
publishes packages under `ghcr.io/varrahan`.

## Artifact policy

Each release contains reproducible Linux AMD64 and ARM64 binaries, SHA-256
checksums, a versioned Helm chart, and an SPDX JSON image SBOM for each
architecture. The container is
a distroless, non-root, multi-architecture image. Its build and runtime bases
are pinned by digest. Version, commit, and the source commit timestamp are
embedded in the binary and OCI labels; inspect them with:

```bash
tt-dra-driver version
```

The registry receives `sha-<40-character-commit>` and exact `MAJOR.MINOR.PATCH`
tags. No release workflow publishes `latest`, moving major/minor aliases, or
`dev`. The semver tag is created from the already scanned, attested, signed,
and signature-verified manifest digest. Production deployments should pin that
digest through `image.digest` even though the version tag is immutable by
release policy.

GitHub OIDC creates signed build-provenance and SBOM attestations for the image
and a signed provenance attestation over release-file checksums. Cosign adds a
separate keyless image signature bound to the release workflow identity. No
long-lived signing key is stored in repository secrets.

Release binaries are CGO-disabled Linux executables built with the pinned Go
1.25.12 toolchain. The release workflow verifies byte-for-byte AMD64 and ARM64
binary reproducibility, independently rebuilds both platform images, and
confirms that the ARM64 image contains an AArch64 executable. These fixed build
policies are constants; version, commit, source timestamp, architecture,
registry identity, and digest remain release-specific inputs.

## Preparing a release

1. Choose a SemVer version and update all three version declarations:
   `Chart.yaml` `version`, `Chart.yaml` `appVersion`, and `values.yaml`
   `image.tag`.
2. Move the relevant changelog entries from `Unreleased` to a dated
   `## [MAJOR.MINOR.PATCH]` section and update comparison links.
3. Run `go mod verify && make ci image-check` on a development machine. Image,
   kind, and Tenstorrent hardware checks run inside the supported `ttsim` VM.
4. Run `make -C test/vm vm-certification` and any physical certification gates
   for the supported matrix. Preserve the checksummed evidence with the
   release approval. The tag workflow reruns exact-commit VM certification and
   does not trust an earlier branch run.
5. Merge through protected CI, create `vMAJOR.MINOR.PATCH` at the approved
   commit, and push the tag. Do not manually upload or replace release files.

The tag workflow publishes only after `QEMU VM certification` passes and the
protected `security-release-approval`, `operations-release-approval`, and
`hardware-release-approval` environments approve. Those environments must
have required reviewers, self-review prevention, protected-tag restrictions,
and `RELEASE_APPROVAL_REQUIRED=enabled`; see
[`ACCEPTANCE.md`](ACCEPTANCE.md). Absent environment configuration fails the
approval jobs rather than silently publishing.

The release workflow rechecks version agreement, changelog presence, module
integrity, CI policy, Helm packaging, image vulnerability policy, and whether
the image/chart version already exists. It then publishes the image, chart, and
GitHub release. A partially failed run must be investigated before retrying;
never delete or overwrite a published semver tag to make a rerun pass.

## Verifying a release

Resolve and retain the image digest:

```bash
version=0.1.0
image=ghcr.io/varrahan/tenstorrent-dra
docker buildx imagetools inspect "$image:$version"
```

Verify the keyless Cosign identity. Replace the tag ref in the certificate
identity with the release being installed:

```bash
digest=sha256:<manifest-digest>
cosign verify "$image@$digest" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity="https://github.com/varrahan/tenstorrent-dra-framework/.github/workflows/release.yml@refs/tags/v$version"
```

Verify GitHub attestations and downloaded release files:

```bash
gh attestation verify "oci://$image@$digest" --repo varrahan/tenstorrent-dra-framework
gh release download "v$version" --repo varrahan/tenstorrent-dra-framework --dir "release-$version"
cd "release-$version"
sha256sum --check checksums.txt
```

Pull and inspect the exact chart version before installation:

```bash
helm pull oci://ghcr.io/varrahan/charts/tenstorrent-dra --version "$version"
helm show chart "tenstorrent-dra-$version.tgz"
```

## Upgrade and rollback

Apply CRD upgrades and backups as described in
[`PRODUCTION.md`](PRODUCTION.md), then install a verified digest with
`--atomic --wait`. Record the previous chart version and image digest before
changing either. To roll back application resources, use Helm history and
explicitly restore the prior verified digest:

```bash
helm history tt-dra -n tenstorrent-system
helm rollback tt-dra <revision> -n tenstorrent-system --wait --timeout=10m
helm upgrade tt-dra oci://ghcr.io/varrahan/charts/tenstorrent-dra \
  --version <previous-version> -n tenstorrent-system --reuse-values \
  --set image.tag='' --set image.digest=sha256:<previous-digest> \
  --atomic --wait --timeout=10m
```

Helm does not roll back CRDs. Do not remove a served or stored API version when
rolling back. The guarded pre-delete hook also refuses uninstall while a claim
or workload is active. Operational upgrade, rollback, state recovery, and node
replacement procedures remain in [`OPERATIONS.md`](OPERATIONS.md).

For a vulnerable release, follow [`SECURITY.md`](../SECURITY.md): publish a new
patch version, mark the affected release/package, and direct users to a new
verified digest. Never rebuild an existing version in place.
