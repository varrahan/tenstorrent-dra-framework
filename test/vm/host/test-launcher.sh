#!/usr/bin/env bash

set -euo pipefail

test_root="$(mktemp -d)"
cleanup() {
  if [[ -n "${fake_pidfile:-}" && -r "$fake_pidfile" ]]; then
    kill "$(<"$fake_pidfile")" 2>/dev/null || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT

vm_root="$test_root/vm"
mkdir -p "$vm_root"
touch "$vm_root/ubuntu.qcow2" "$vm_root/seed.iso" "$vm_root/libttsim.so"
fake_qemu="$test_root/fake-qemu"
fake_log="$test_root/qemu.args"
cat >"$fake_qemu" <<'FAKE_QEMU'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"$FAKE_QEMU_LOG"
pidfile=""
while (($#)); do
  if [[ "$1" == "-pidfile" ]]; then
    pidfile="$2"
    shift 2
  else
    shift
  fi
done
: "${pidfile:?missing -pidfile}"
echo "$$" >"$pidfile"
(sleep 30) &
echo "$!" >"$pidfile"
exit 0
FAKE_QEMU
chmod +x "$fake_qemu"

fake_pidfile="$vm_root/vm.pid"
QEMU_BIN="$fake_qemu" \
TTSIM_VM_ROOT="$vm_root" \
TTSIM_LIBRARY="$vm_root/libttsim.so" \
TTSIM_SSH_PORT="23456" \
TTSIM_MONITOR_SOCKET="$test_root/monitor.sock" \
TTSIM_SERIAL_LOG="$test_root/serial.log" \
TTSIM_MEMORY="2G" \
TTSIM_SMP="2" \
TTSIM_CPUSET="0-1" \
FAKE_QEMU_LOG="$fake_log" \
  "$(dirname "$0")/launch-ttsim-qemu.sh" >/dev/null

test -s "$fake_pidfile"
grep -q -- '-cpu max' "$fake_log"
grep -q -- '-accel tcg,thread=multi' "$fake_log"
grep -q -- '-smp 2' "$fake_log"
grep -q -- 'bar4-size=32M' "$fake_log"
grep -q -- 'hostfwd=tcp:127.0.0.1:23456-:22' "$fake_log"
grep -q -- "file=$vm_root/ubuntu.qcow2" "$fake_log"
echo 'launcher regression test passed'
