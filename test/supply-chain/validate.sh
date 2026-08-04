#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

if ! head -n 1 Dockerfile | grep -Eq '^# syntax=[^@]+@sha256:[0-9a-f]{64}$'; then
  echo "Dockerfile frontend is not pinned by digest" >&2
  exit 1
fi

mapfile -t from_lines < <(grep -E '^FROM ' Dockerfile)
if ((${#from_lines[@]} != 2)); then
  echo "Dockerfile must contain exactly one build and one runtime stage" >&2
  exit 1
fi
for from_line in "${from_lines[@]}"; do
  if [[ ! "$from_line" =~ @sha256:[0-9a-f]{64}([[:space:]]|$) ]]; then
    echo "base image is not pinned by digest: $from_line" >&2
    exit 1
  fi
done
grep -Eq "^FROM --platform=\\\$BUILDPLATFORM [^ ]+@sha256:[0-9a-f]{64} AS build$" Dockerfile
grep -qx 'ARG TARGETOS' Dockerfile
grep -qx 'ARG TARGETARCH' Dockerfile

grep -qx 'USER 65532:65532' Dockerfile
grep -Eq '^SHELLCHECK_IMAGE := [^@]+@sha256:[0-9a-f]{64}$' Makefile
grep -qx 'override CGO_MODE := 0' Makefile
grep -qx 'override RELEASE_GOOS := linux' Makefile
grep -qx 'override GO_TOOLCHAIN := go1.25.12' Makefile
grep -q 'runAsUser: 65532' deployments/helm/tenstorrent-dra/templates/daemonset.yaml
grep -q 'go-version: 1.25.12' .github/workflows/ci.yml
grep -q 'version: v4.2.3' .github/workflows/ci.yml
if grep -Eq '^[[:space:]]+(GO_VERSION|HELM_VERSION):' .github/workflows/ci.yml; then
  echo "fixed CI tool versions must be literals, not environment variables" >&2
  exit 1
fi
if grep -Eq "^[[:space:]]*(tag|appVersion):[[:space:]]*[\"']?dev" \
  deployments/helm/tenstorrent-dra/Chart.yaml deployments/helm/tenstorrent-dra/values.yaml; then
  echo "production chart defaults must not use a mutable dev tag" >&2
  exit 1
fi

while IFS= read -r use; do
  if [[ ! "$use" =~ @[0-9a-f]{40}$ ]]; then
    echo "GitHub Action is not pinned to a full commit: $use" >&2
    exit 1
  fi
done < <(awk '/^[[:space:]]*-[[:space:]]+uses:/ { value=$3; sub(/[[:space:]]+#.*/, "", value); print value }' .github/workflows/*.yml)

release_workflow=.github/workflows/release.yml
for required in \
  'linux/amd64,linux/arm64' \
  'docker build --no-cache --provenance=false' \
  '--platform linux/arm64' \
  'make release release-reproducibility' \
  'ARM aarch64' \
  'cosign sign' \
  'sbom-path:' \
  'subject-checksums:' \
  'push-to-registry: true' \
  'helm push'; do
  grep -qF -- "$required" "$release_workflow"
done

temporary_directory="$(mktemp -d)"
trap 'rm -rf -- "$temporary_directory"' EXIT
test_version=0.0.0-validation
test_commit=0123456789abcdef0123456789abcdef01234567
test_date=2026-08-04T00:00:00Z
go build -mod=readonly -trimpath -buildvcs=false \
  -ldflags="-s -w -buildid= -X main.version=$test_version -X main.commit=$test_commit -X main.buildDate=$test_date" \
  -o "$temporary_directory/tt-dra-driver" ./src/cmd/tt-dra-driver
version_output="$("$temporary_directory/tt-dra-driver" version)"
grep -qF '"version":"'"$test_version"'"' <<<"$version_output"
grep -qF '"commit":"'"$test_commit"'"' <<<"$version_output"
grep -qF '"buildDate":"'"$test_date"'"' <<<"$version_output"
