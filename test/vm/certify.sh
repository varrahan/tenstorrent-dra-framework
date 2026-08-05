#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
expected_commit="${1:-$(git -C "$repo_root" rev-parse HEAD)}"
evidence_dir="${2:-$repo_root/artifacts/vm-certification-$(date -u +%Y%m%dT%H%M%SZ)}"
smoke_cluster="tt-dra"
chaos_cluster="tt-dra-chaos"

mkdir -p "$evidence_dir"

record() {
  printf '%s\t%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" |
    tee -a "$evidence_dir/results.tsv"
}

cluster_exists() {
  kind get clusters 2>/dev/null | grep -Fxq "$1"
}

archive_and_delete_cluster() {
  local cluster="$1"

  if ! cluster_exists "$cluster"; then
    return
  fi
  mkdir -p "$evidence_dir/kind-$cluster"
  kind export logs "$evidence_dir/kind-$cluster" --name "$cluster" >"$evidence_dir/kind-$cluster-export.log" 2>&1
  kind delete cluster --name "$cluster" >>"$evidence_dir/kind-$cluster-export.log" 2>&1
}

finalize() {
  local result=$?
  local cluster
  local -a evidence_files=()
  trap - EXIT
  set +e
  for cluster in "$smoke_cluster" "$chaos_cluster"; do
    archive_and_delete_cluster "$cluster"
  done
  if ((result == 0)); then
    record certification PASS
  else
    record certification FAIL
  fi
  (
    cd "$evidence_dir" || exit 1
    mapfile -d '' -t evidence_files < <(find . -type f ! -name checksums.sha256 -print0 | sort -z)
    sha256sum "${evidence_files[@]}" >checksums.sha256
  )
  exit "$result"
}
trap finalize EXIT

for command in docker kind kubectl helm go make python3 systemd-detect-virt; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'required command is unavailable: %s\n' "$command" >&2
    exit 1
  fi
done

actual_commit="$(git -C "$repo_root" rev-parse HEAD)"
if [[ "$actual_commit" != "$expected_commit" ]]; then
  printf 'candidate commit mismatch: expected %s, found %s\n' "$expected_commit" "$actual_commit" >&2
  exit 1
fi
if [[ -n "$(git -C "$repo_root" status --porcelain --untracked-files=normal)" ]]; then
  printf 'VM certification requires a clean checkout of the candidate commit\n' >&2
  exit 1
fi

virtualization="$(systemd-detect-virt 2>/dev/null || true)"
case "$virtualization" in
qemu | kvm) ;;
*)
  printf 'VM certification requires the supported QEMU ttsim VM; detected %s\n' "${virtualization:-none}" >&2
  exit 1
  ;;
esac

go_version="$(go version)"
helm_version="$(helm version --short)"
kubectl_version="$(kubectl version --client=true -o json | python3 -c 'import json,sys; print(json.load(sys.stdin)["clientVersion"]["gitVersion"])')"
if [[ "$go_version" != "go version go1.25.12 "* ]]; then
  printf 'VM certification requires Go 1.25.12; found %s\n' "$go_version" >&2
  exit 1
fi
if [[ "$helm_version" != "v4.2.3"* ]]; then
  printf 'VM certification requires Helm 4.2.3; found %s\n' "$helm_version" >&2
  exit 1
fi
if [[ ! "$kubectl_version" =~ ^v1\.([3-9][4-9]|[4-9][0-9]|[1-9][0-9]{2,})\. ]]; then
  printf 'VM certification requires kubectl 1.34 or newer; found %s\n' "$kubectl_version" >&2
  exit 1
fi

docker info >/dev/null

{
  printf 'candidate_commit\t%s\n' "$actual_commit"
  printf 'candidate_tree\t%s\n' "$(git -C "$repo_root" rev-parse 'HEAD^{tree}')"
  printf 'virtualization\t%s\n' "$virtualization"
  printf 'kernel\t%s\n' "$(uname -r)"
  printf 'go\t%s\n' "$go_version"
  printf 'docker\t%s\n' "$(docker version --format '{{.Server.Version}}')"
  printf 'kind\t%s\n' "$(kind version)"
  printf 'kubectl\t%s\n' "$kubectl_version"
  printf 'helm\t%s\n' "$helm_version"
} >"$evidence_dir/metadata.tsv"

record smoke START
set +e
make -C "$script_dir" vm-validate 2>&1 | tee "$evidence_dir/vm-validate.log"
smoke_result=${PIPESTATUS[0]}
set -e
if ((smoke_result != 0)); then
  record smoke FAIL
  exit "$smoke_result"
fi
record smoke PASS
candidate_image_metadata="$(docker run --rm tenstorrent-dra:dev version)"
if ! grep -qF -- "\"commit\":\"$actual_commit\"" <<<"$candidate_image_metadata"; then
  printf 'candidate image metadata does not identify commit %s: %s\n' "$actual_commit" "$candidate_image_metadata" >&2
  exit 1
fi
printf '%s\n' "$candidate_image_metadata" >"$evidence_dir/candidate-image-metadata.json"
docker image inspect tenstorrent-dra:dev --format '{{.Id}}' |
  tee "$evidence_dir/candidate-image-digest.txt" >/dev/null

# Avoid running two three-node kind clusters concurrently in the constrained
# certification VM. Preserve the smoke logs before releasing its resources.
archive_and_delete_cluster "$smoke_cluster"

record chaos START
set +e
make -C "$script_dir" vm-chaos CHAOS_EVIDENCE_DIR="$evidence_dir/chaos" 2>&1 |
  tee "$evidence_dir/vm-chaos.log"
chaos_result=${PIPESTATUS[0]}
set -e
if ((chaos_result != 0)); then
  record chaos FAIL
  exit "$chaos_result"
fi
record chaos PASS
