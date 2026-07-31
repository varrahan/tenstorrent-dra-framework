#!/usr/bin/env bash

set -euo pipefail

vm_root="${TTSIM_VM_ROOT:-$HOME/sim/ttsim-qemu}"
ssh_port="${TTSIM_SSH_PORT:-2222}"
ssh_key="${TTSIM_SSH_KEY:-$HOME/.ssh/ttsim_vm_ed25519}"
pidfile="$vm_root/vm.pid"

if [[ ! -r "$pidfile" ]] || ! kill -0 "$(<"$pidfile")" 2>/dev/null; then
  echo "ttsim QEMU bridge is not running" >&2
  exit 1
fi

command_line="$(tr '\0' ' ' < "/proc/$(<"$pidfile")/cmdline")"
if grep -Eq -- '-enable-kvm|-accel[[:space:]]+kvm|-cpu[[:space:]]+host' \
  <<<"$command_line"; then
  echo "unsupported KVM/host-CPU option found in QEMU command line" >&2
  exit 1
fi
grep -q -- '-cpu max' <<<"$command_line"
grep -q -- 'bar4-size=32M' <<<"$command_line"

ssh_options=(
  -i "$ssh_key"
  -p "$ssh_port"
  -o BatchMode=yes
  -o ConnectTimeout=5
)

ssh "${ssh_options[@]}" ubuntu@127.0.0.1 'bash -s' <<'GUEST'
set -euo pipefail

lspci -nnk -s 00:03.0 | grep -q '1e52:401e'
lspci -nnk -s 00:03.0 | grep -q 'Kernel driver in use: tenstorrent'
test "$(modinfo /home/ubuntu/tt-kmd/tenstorrent.ko | awk '/^version:/{print $2}')" = "2.3.0"
test -c /dev/tenstorrent/0

resource=/sys/bus/pci/devices/0000:00:03.0/resource
test -r "$resource"
python3 - "$resource" <<'PYTHON'
from pathlib import Path
import sys

resources = []
for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines():
    start, end, _flags = (int(value, 16) for value in line.split())
    resources.append(0 if end < start else end - start + 1)

expected = {0: 512 * 1024 * 1024, 2: 1 * 1024 * 1024, 4: 32 * 1024 * 1024}
for bar, size in expected.items():
    if resources[bar] != size:
        raise SystemExit(f"BAR{bar}: expected {size}, got {resources[bar]}")
PYTHON

echo "guest bridge verification passed"
GUEST

echo "host launch verification passed"
