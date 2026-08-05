#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
action="${1:-preflight}"
entry="${2:-}"
evidence_dir="${3:-$repo_root/artifacts/hardware-$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$evidence_dir"

finish() {
  local exit_code=$?
  trap - EXIT
  if ((exit_code == 0)); then
    printf 'PASS\n' >"$evidence_dir/status"
  else
    printf 'FAIL\n' >"$evidence_dir/status"
  fi
  printf 'hardware certification evidence: %s\n' "$evidence_dir"
  exit "$exit_code"
}
trap finish EXIT

require_command() {
  if ! command -v "$1" >/dev/null; then
    printf 'required command is unavailable: %s\n' "$1" >&2
    return 1
  fi
}

if [[ "$action" != preflight ]]; then
  printf 'usage: %s preflight <matrix-entry> [evidence-directory]\n' "$0" >&2
  exit 2
fi
if [[ -z "$entry" ]]; then
  printf 'a matrix entry from test/hardware/matrix.json is required\n' >&2
  exit 2
fi

for command in go kubectl helm find python3 uname; do
  require_command "$command"
done

uname -a | tee "$evidence_dir/uname.txt"
if grep -qi microsoft "$evidence_dir/uname.txt"; then
  printf 'WSL is not a physical Tenstorrent certification environment\n' >&2
  exit 1
fi
test -d /dev/tenstorrent
test -d /sys/class/tenstorrent
test -d /sys/bus/pci/devices
find /dev/tenstorrent -maxdepth 1 -type c -print | sort | tee "$evidence_dir/device-nodes.txt"
test -s "$evidence_dir/device-nodes.txt"

go version | tee "$evidence_dir/go-version.txt"
helm version --short | tee "$evidence_dir/helm-version.txt"
grep -q '^v4\.2\.3' "$evidence_dir/helm-version.txt"
kubectl version -o json >"$evidence_dir/kubernetes-version.json"
python3 -c 'import json, sys; value=json.load(open(sys.argv[1], encoding="utf-8"))["serverVersion"]["minor"].rstrip("+"); raise SystemExit(0 if int(value) >= 34 else 1)' "$evidence_dir/kubernetes-version.json"
kubectl get nodes -o wide >"$evidence_dir/nodes.txt"

go build -mod=readonly -trimpath -buildvcs=false -o "$evidence_dir/tt-dra-driver" ./src/cmd/tt-dra-driver
"$evidence_dir/tt-dra-driver" list >"$evidence_dir/inventory.json"
python3 "$script_dir/verify_inventory.py" "$script_dir/matrix.json" "$entry" "$evidence_dir/inventory.json" | tee "$evidence_dir/inventory-verification.json"

find /sys/class/tenstorrent -maxdepth 3 -type f -o -type l | sort >"$evidence_dir/tenstorrent-sysfs.txt"
kubectl get nodes -l tenstorrent.com/enabled=true -o yaml >"$evidence_dir/enabled-nodes.yaml"
minimum_nodes="$(python3 -c 'import json, sys; data=json.load(open(sys.argv[1], encoding="utf-8")); print(next(item["minimumNodes"] for item in data["entries"] if item["id"] == sys.argv[2]))' "$script_dir/matrix.json" "$entry")"
actual_nodes="$(kubectl get nodes -l tenstorrent.com/enabled=true -o name | wc -l)"
if ((actual_nodes < minimum_nodes)); then
  printf 'matrix entry %s requires %s enabled nodes; found %s\n' "$entry" "$minimum_nodes" "$actual_nodes" >&2
  exit 1
fi

printf '%s\n' "$entry" >"$evidence_dir/matrix-entry"
printf 'preflight passed; no physical lifecycle, reset, reboot, fault-injection, concurrency, or soak claim is made by preflight alone\n' | tee "$evidence_dir/scope.txt"
