#!/usr/bin/env bash

set -euo pipefail

cluster="${SPLIT_CLUSTER:-ttsim-split}"
node_image="${SPLIT_NODE_IMAGE:-kindest/node:v1.34.0}"
# Keep the advertised kubeadm endpoint at 6443.  A different host port can be
# selected explicitly, but it must remain reachable at the same port from the
# QEMU user-network gateway.
api_port="${SPLIT_API_PORT:-6443}"

command -v kind >/dev/null 2>&1 || { echo 'kind is required on the host' >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo 'kubectl is required on the host' >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo 'docker is required on the host' >&2; exit 1; }
kind get clusters | grep --color=never -qx "$cluster" && {
  echo "kind cluster already exists: $cluster" >&2
  exit 1
}

config="$(mktemp)"
trap 'rm -f "$config"' EXIT
cat >"$config" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: "0.0.0.0"
  apiServerPort: ${api_port}
nodes:
- role: control-plane
  image: ${node_image}
EOF

kind create cluster --name "$cluster" --config "$config" --wait 5m
kubectl --context "kind-${cluster}" wait --for=condition=Ready node --all --timeout=5m
join_command="$(docker exec "${cluster}-control-plane" kubeadm token create --print-join-command --kubeconfig /etc/kubernetes/admin.conf)"

cat <<EOF
split control plane is ready:
  cluster: ${cluster}
  host API endpoint: https://127.0.0.1:${api_port}
  QEMU user-network gateway endpoint: https://10.0.2.2:${api_port}
  context: kind-${cluster}

Worker join command (run inside the VM after installing kubelet/kubeadm):
  ${join_command}

Validation-only CNI fallback (copy before starting kubelet if kindnet cannot
route across QEMU user networking):
  scp -P 2222 test/vm/host/split-worker-cni.conflist ubuntu@127.0.0.1:/tmp/10-ttsim.conflist
  ssh -p 2222 ubuntu@127.0.0.1 'sudo install -m 0644 /tmp/10-ttsim.conflist /etc/cni/net.d/10-ttsim.conflist'

The VM must join this cluster as a real kubelet/containerd worker. Mounting a
guest character device into a host kind container is not possible; do not use
this control plane for device injection until the VM worker has joined.
EOF
