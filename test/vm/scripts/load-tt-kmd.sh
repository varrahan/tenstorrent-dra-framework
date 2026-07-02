#!/usr/bin/env bash
set -Eeuo pipefail

TT_DEVICE_PATH="${TT_DEVICE_PATH:-/dev/tenstorrent}"
TT_KMD_MODULE="${TT_KMD_MODULE:-/home/ubuntu/tt-kmd/tenstorrent.ko}"

log() {
  printf '[tt-kmd] %s\n' "$*"
}

fail() {
  printf '[tt-kmd] error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

sudo_run() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo "$@"
  fi
}

device_has_character_node() {
  if [ -d "$TT_DEVICE_PATH" ]; then
    find "$TT_DEVICE_PATH" -maxdepth 1 -type c -print -quit | grep -q .
  else
    test -c "$TT_DEVICE_PATH"
  fi
}

require_cmd find
require_cmd grep
require_cmd lspci
require_cmd lsmod
require_cmd modinfo

log "checking simulated Tenstorrent PCI device"
lspci -nn | grep --color=never -i 'tenstorrent' >/dev/null || fail "no Tenstorrent PCI device found"
lspci -nnk -d 1e52: || true

if lsmod | grep --color=never -q '^tenstorrent[[:space:]]'; then
  log "kernel module already loaded"
else
  test -r "$TT_KMD_MODULE" || fail "kernel module not readable: $TT_KMD_MODULE"
  log "loading kernel module: $TT_KMD_MODULE"
  modinfo "$TT_KMD_MODULE" | sed -n '1,20p'
  sudo_run insmod "$TT_KMD_MODULE" || {
    lsmod | grep --color=never -q '^tenstorrent[[:space:]]' || fail "failed to load tenstorrent module"
  }
fi

log "verifying kernel module and device node"
lsmod | grep --color=never -i '^tenstorrent[[:space:]]' || fail "tenstorrent module is not loaded"
lspci -nnk -d 1e52: || true

test -e "$TT_DEVICE_PATH" || fail "device path does not exist: $TT_DEVICE_PATH"
device_has_character_node || fail "no character device found at or under: $TT_DEVICE_PATH"

if [ -d "$TT_DEVICE_PATH" ]; then
  find "$TT_DEVICE_PATH" -maxdepth 1 -type c -ls
else
  ls -l "$TT_DEVICE_PATH"
fi

log "ok"
