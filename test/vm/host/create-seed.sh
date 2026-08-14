#!/usr/bin/env bash

set -euo pipefail

vm_root="${TTSIM_VM_ROOT:-$HOME/sim/ttsim-qemu}"
ssh_key="${TTSIM_SSH_KEY:-$HOME/.ssh/ttsim_vm_ed25519}"

if [[ ! -r "${ssh_key}.pub" ]]; then
  echo "missing VM SSH public key: ${ssh_key}.pub" >&2
  exit 1
fi

command -v genisoimage >/dev/null
mkdir -p "$vm_root"

seed_root="$(mktemp -d)"
trap 'rm -rf "$seed_root"' EXIT

public_key="$(<"${ssh_key}.pub")"
cat >"$seed_root/user-data" <<EOF
#cloud-config
hostname: ttsim
manage_etc_hosts: true
ssh_pwauth: false
users:
  - default
  - name: ubuntu
    groups: [adm, sudo]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: true
    ssh_authorized_keys:
      - $public_key
growpart:
  mode: auto
  devices: ['/']
resize_rootfs: true
EOF

cat >"$seed_root/meta-data" <<'EOF'
instance-id: ttsim-dra
local-hostname: ttsim
EOF

genisoimage -quiet -output "$vm_root/seed.iso" -volid cidata \
  -joliet -rock "$seed_root/user-data" "$seed_root/meta-data"

echo "created $vm_root/seed.iso"
