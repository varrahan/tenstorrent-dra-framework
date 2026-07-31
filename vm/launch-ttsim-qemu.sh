#!/usr/bin/env bash

set -euo pipefail

qemu_bin="${QEMU_BIN:-$HOME/.local/bin/qemu-system-x86_64}"
vm_root="${TTSIM_VM_ROOT:-$HOME/sim/ttsim-qemu}"
simulator="${TTSIM_LIBRARY:-$HOME/sim/libttsim_wh.so}"
ssh_port="${TTSIM_SSH_PORT:-2222}"
pidfile="$vm_root/vm.pid"
monitor_socket="${TTSIM_MONITOR_SOCKET:-/tmp/ttsim-mon.sock}"
serial_log="${TTSIM_SERIAL_LOG:-/tmp/ttsim-qemu-serial.log}"

for required in "$qemu_bin" "$vm_root/ubuntu.qcow2" \
  "$vm_root/seed.iso" "$simulator"; do
  if [[ ! -r "$required" ]]; then
    echo "missing required VM asset: $required" >&2
    exit 1
  fi
done

if [[ -r "$pidfile" ]] && kill -0 "$(<"$pidfile")" 2>/dev/null; then
  echo "ttsim QEMU bridge is already running with PID $(<"$pidfile")" >&2
  exit 1
fi

if ss -H -ltn "sport = :$ssh_port" | grep -q .; then
  echo "TCP port $ssh_port is already in use" >&2
  exit 1
fi

rm -f "$monitor_socket" "$serial_log" "$pidfile"

"$qemu_bin" \
  -m 8G -smp 4 \
  -cpu max \
  -drive "file=$vm_root/ubuntu.qcow2,if=virtio" \
  -drive "file=$vm_root/seed.iso,if=virtio,format=raw,readonly=on" \
  -device "ttsim,lib=$simulator,bar4-size=32M" \
  -netdev "user,id=net0,hostfwd=tcp:127.0.0.1:$ssh_port-:22" \
  -device virtio-net-pci,netdev=net0 \
  -serial "file:$serial_log" \
  -chardev "socket,id=mon,path=$monitor_socket,server=on,wait=off" \
  -mon chardev=mon,mode=readline \
  -display none -daemonize \
  -pidfile "$pidfile"

echo "ttsim QEMU bridge started with PID $(<"$pidfile")"
echo "SSH: ssh -p $ssh_port ubuntu@127.0.0.1"
echo "Serial log: $serial_log"
