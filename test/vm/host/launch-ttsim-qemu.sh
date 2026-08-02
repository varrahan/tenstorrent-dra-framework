#!/usr/bin/env bash

set -euo pipefail

readonly QEMU_BIN="${QEMU_BIN:-$HOME/.local/bin/qemu-system-x86_64}"
readonly TTSIM_VM_ROOT="${TTSIM_VM_ROOT:-$HOME/sim/ttsim-qemu}"
readonly TTSIM_LIBRARY="${TTSIM_LIBRARY:-$HOME/sim/libttsim_wh.so}"
readonly TTSIM_SSH_PORT="${TTSIM_SSH_PORT:-2222}"
readonly TTSIM_MONITOR_SOCKET="${TTSIM_MONITOR_SOCKET:-/tmp/ttsim-mon.sock}"
readonly TTSIM_SERIAL_LOG="${TTSIM_SERIAL_LOG:-/tmp/ttsim-qemu-serial.log}"
readonly TTSIM_MEMORY="${TTSIM_MEMORY:-8G}"
readonly TTSIM_SMP="${TTSIM_SMP:-8}"
readonly TTSIM_CPUSET="${TTSIM_CPUSET:-}"

pidfile="$TTSIM_VM_ROOT/vm.pid"

for required in "$QEMU_BIN" "$TTSIM_VM_ROOT/ubuntu.qcow2" \
  "$TTSIM_VM_ROOT/seed.iso" "$TTSIM_LIBRARY"; do
  if [[ ! -r "$required" ]]; then
    echo "missing required VM asset: $required" >&2
    exit 1
  fi
done

if [[ -r "$pidfile" ]] && kill -0 "$(<"$pidfile")" 2>/dev/null; then
  echo "ttsim QEMU bridge is already running with PID $(<"$pidfile")" >&2
  exit 1
fi

if ss -H -ltn "sport = :$TTSIM_SSH_PORT" | grep -q .; then
  echo "TCP port $TTSIM_SSH_PORT is already in use" >&2
  exit 1
fi

rm -f "$TTSIM_MONITOR_SOCKET" "$TTSIM_SERIAL_LOG" "$pidfile"

qemu_command=(
  -m "$TTSIM_MEMORY" -smp "$TTSIM_SMP" \
  -accel tcg,thread=multi \
  -cpu max \
  -drive "file=$TTSIM_VM_ROOT/ubuntu.qcow2,if=virtio" \
  -drive "file=$TTSIM_VM_ROOT/seed.iso,if=virtio,format=raw,readonly=on" \
  -device "ttsim,lib=$TTSIM_LIBRARY,bar4-size=32M" \
  -netdev "user,id=net0,hostfwd=tcp:127.0.0.1:$TTSIM_SSH_PORT-:22" \
  -device virtio-net-pci,netdev=net0 \
  -serial "file:$TTSIM_SERIAL_LOG" \
  -chardev "socket,id=mon,path=$TTSIM_MONITOR_SOCKET,server=on,wait=off" \
  -mon chardev=mon,mode=readline \
  -display none -daemonize \
  -pidfile "$pidfile"
)

if [[ -n "$TTSIM_CPUSET" ]]; then
  command -v taskset >/dev/null 2>&1 || {
    echo 'TTSIM_CPUSET requires taskset' >&2
    exit 1
  }
  taskset --cpu-list "$TTSIM_CPUSET" "$QEMU_BIN" "${qemu_command[@]}"
else
  "$QEMU_BIN" "${qemu_command[@]}"
fi

echo "ttsim QEMU bridge started with PID $(<"$pidfile")"
echo "SSH: ssh -p $TTSIM_SSH_PORT ubuntu@127.0.0.1"
echo "Serial log: $TTSIM_SERIAL_LOG"
