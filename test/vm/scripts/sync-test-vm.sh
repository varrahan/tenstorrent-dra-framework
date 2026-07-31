#!/usr/bin/env bash

set -euo pipefail

readonly VM_HOST="127.0.0.1"
readonly VM_PORT=2222
readonly VM_USER="ubuntu"
readonly VM_KEY="$HOME/.ssh/ttsim_vm_ed25519"
readonly VM_REPO_PATH="/home/ubuntu/tt-device-plugin"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
local_test_vm="${script_dir}/.."

ssh_options=(
  -i "$VM_KEY"
  -o BatchMode=yes
  -o StrictHostKeyChecking=accept-new
  -p "$VM_PORT"
)
scp_options=(
  -i "$VM_KEY"
  -o BatchMode=yes
  -o StrictHostKeyChecking=accept-new
  -P "$VM_PORT"
)

ssh "${ssh_options[@]}" "$VM_USER@$VM_HOST" \
  "mkdir -p '$VM_REPO_PATH/test' && rm -rf '$VM_REPO_PATH/test/vm'"
scp "${scp_options[@]}" -r "$local_test_vm" \
  "$VM_USER@$VM_HOST:$VM_REPO_PATH/test/vm"
