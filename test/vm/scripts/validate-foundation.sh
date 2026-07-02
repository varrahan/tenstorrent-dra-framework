#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

KIND_CLUSTER="${KIND_CLUSTER:-agent-smoke}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.34.0}"
KIND_WAIT="${KIND_WAIT:-120s}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-kind-${KIND_CLUSTER}}"
TT_DEVICE_PATH="${TT_DEVICE_PATH:-/dev/tenstorrent}"
CLEANUP_KIND_CLUSTER="${CLEANUP_KIND_CLUSTER:-1}"
KIND_ONLY=0

usage() {
  cat <<'EOF'
Usage: validate-foundation.sh [--kind-only] [--keep-cluster]

Environment:
  KIND_CLUSTER          kind cluster name, default: agent-smoke
  KIND_NODE_IMAGE       kind node image, default: kindest/node:v1.34.0
  KIND_WAIT             kind readiness timeout, default: 120s
  KUBECTL_CONTEXT       kubectl context, default: kind-${KIND_CLUSTER}
  TT_DEVICE_PATH        Tenstorrent device directory or device, default: /dev/tenstorrent
  TT_KMD_MODULE         Tenstorrent kernel module path, default: /home/ubuntu/tt-kmd/tenstorrent.ko
  CLEANUP_KIND_CLUSTER  delete a cluster created by this script on success, default: 1
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --kind-only)
      KIND_ONLY=1
      ;;
    --keep-cluster)
      CLEANUP_KIND_CLUSTER=0
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '[validate] error: unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

log() {
  printf '[validate] %s\n' "$*"
}

fail() {
  printf '[validate] error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

device_check_command() {
  if [ -d "$TT_DEVICE_PATH" ]; then
    printf 'find %s -maxdepth 1 -type c -print -quit | grep -q .' "$TT_DEVICE_PATH"
  else
    printf 'test -c %s' "$TT_DEVICE_PATH"
  fi
}

host_path_type() {
  if [ -d "$TT_DEVICE_PATH" ]; then
    printf 'Directory'
  else
    printf 'CharDevice'
  fi
}

node_has_device() {
  local node_container="${KIND_CLUSTER}-control-plane"

  docker exec "$node_container" test -e "$TT_DEVICE_PATH"
  if [ -d "$TT_DEVICE_PATH" ]; then
    docker exec "$node_container" find "$TT_DEVICE_PATH" -maxdepth 1 -type c -ls
  else
    docker exec "$node_container" test -c "$TT_DEVICE_PATH"
    docker exec "$node_container" ls -l "$TT_DEVICE_PATH"
  fi
}

make_kind_config() {
  local path="$1"

  cat >"$path" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: ${KIND_NODE_IMAGE}
  extraMounts:
  - hostPath: ${TT_DEVICE_PATH}
    containerPath: ${TT_DEVICE_PATH}
    propagation: HostToContainer
EOF
}

make_smoke_manifest() {
  local path="$1"
  local check_command
  local type

  check_command="$(device_check_command)"
  type="$(host_path_type)"

  cat >"$path" <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ttsim-device-check
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: check
        image: busybox:1.36
        command: ["sh", "-c", "${check_command}"]
        securityContext:
          privileged: true
        volumeMounts:
        - name: ttsim-device
          mountPath: ${TT_DEVICE_PATH}
      volumes:
      - name: ttsim-device
        hostPath:
          path: ${TT_DEVICE_PATH}
          type: ${type}
EOF
}

run_base_checks() {
  log "checking guest OS and services"
  cat /etc/os-release
  uname -a
  systemctl is-active ssh >/dev/null || systemctl status ssh --no-pager

  log "checking Docker, kind, and kubectl"
  docker version >/dev/null || sudo docker version >/dev/null
  docker ps >/dev/null || sudo docker ps >/dev/null
  kind version
  kubectl version --client
}

run_kind_smoke() {
  local created_cluster=0
  local kind_config
  local smoke_manifest

  require_cmd docker
  require_cmd find
  require_cmd grep
  require_cmd kind
  require_cmd kubectl

  "$SCRIPT_DIR/load-tt-kmd.sh"

  test -e "$TT_DEVICE_PATH" || fail "device path does not exist: $TT_DEVICE_PATH"

  kind_config="$(mktemp)"
  smoke_manifest="$(mktemp)"
  make_kind_config "$kind_config"
  make_smoke_manifest "$smoke_manifest"

  log "using kind config generated from ${TEST_DIR}/kind/ttsim-dra.yaml defaults"
  cat "$kind_config"

  if kind get clusters | grep --color=never -qx "$KIND_CLUSTER"; then
    log "using existing kind cluster: $KIND_CLUSTER"
  else
    log "creating kind cluster: $KIND_CLUSTER"
    kind create cluster --name "$KIND_CLUSTER" --config "$kind_config" --wait "$KIND_WAIT"
    created_cluster=1
  fi

  log "checking Kubernetes API and DRA resources"
  kubectl cluster-info --context "$KUBECTL_CONTEXT"
  kubectl --context "$KUBECTL_CONTEXT" version
  kubectl --context "$KUBECTL_CONTEXT" api-resources --api-group=resource.k8s.io
  kubectl --context "$KUBECTL_CONTEXT" api-resources --api-group=resource.k8s.io \
    | grep --color=never -E '^(deviceclasses|resourceclaims|resourceslices)[[:space:]]'

  log "checking Tenstorrent device path inside kind node"
  node_has_device

  log "checking Tenstorrent device path inside a privileged pod"
  kubectl --context "$KUBECTL_CONTEXT" delete job ttsim-device-check --ignore-not-found >/dev/null
  kubectl --context "$KUBECTL_CONTEXT" apply -f "$smoke_manifest"
  kubectl --context "$KUBECTL_CONTEXT" wait --for=condition=complete job/ttsim-device-check --timeout=120s
  kubectl --context "$KUBECTL_CONTEXT" logs job/ttsim-device-check || true
  kubectl --context "$KUBECTL_CONTEXT" delete job ttsim-device-check --ignore-not-found >/dev/null

  if [ "$created_cluster" -eq 1 ] && [ "$CLEANUP_KIND_CLUSTER" = "1" ]; then
    log "deleting validation kind cluster: $KIND_CLUSTER"
    kind delete cluster --name "$KIND_CLUSTER"
  fi

  rm -f "$kind_config" "$smoke_manifest"
}

if [ "$KIND_ONLY" -eq 0 ]; then
  run_base_checks
fi

run_kind_smoke
log "ok"
