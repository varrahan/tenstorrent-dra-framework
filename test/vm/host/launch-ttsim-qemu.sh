#!/usr/bin/env bash

set -euo pipefail

readonly QEMU_BIN="$HOME/.local/bin/qemu-system-x86_64"
readonly TTSIM_VM_ROOT="$HOME/sim/ttsim-qemu"
readonly TTSIM_LIBRARY="$HOME/sim/libttsim_wh.so"
readonly TTSIM_SSH_PORT="2222"
readonly TTSIM_MONITOR_SOCKET="/tmp/ttsim-mon.sock"
readonly TTSIM_SERIAL_LOG="/tmp/ttsim-qemu-serial.log"

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

"$QEMU_BIN" \
  -m 8G -smp 4 \
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

echo "ttsim QEMU bridge started with PID $(<"$pidfile")"
echo "SSH: ssh -p $TTSIM_SSH_PORT ubuntu@127.0.0.1"
echo "Serial log: $TTSIM_SERIAL_LOG"
