#!/usr/bin/env bash
set -euo pipefail

cluster="${1:?cluster name required}"
config="${2:?kind config required}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
readonly IMAGE_REPOSITORY="tenstorrent-dra"
readonly IMAGE_TAG="dev"
readonly E2E_IMAGE="tenstorrent-dra-e2e:dev"
readonly DISABLE_WORKLOAD_APPARMOR="false"
readonly KUBE_WAIT_TIMEOUT="${KUBE_WAIT_TIMEOUT:-600s}"
candidate_version="$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)"
candidate_commit="$(git -C "$repo_root" rev-parse HEAD 2>/dev/null || echo unknown)"
candidate_source_epoch="$(git -C "$repo_root" log -1 --format=%ct 2>/dev/null || echo 0)"
candidate_build_date="$(date -u --date="@$candidate_source_epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo 1970-01-01T00:00:00Z)"
candidate_image_args=(
  --build-arg "VERSION=$candidate_version"
  --build-arg "VCS_REF=$candidate_commit"
  --build-arg "SOURCE_DATE_EPOCH=$candidate_source_epoch"
  --build-arg "BUILD_DATE=$candidate_build_date"
)
kubectl_context="kind-$cluster"
cluster_ready=false

# kind's Debian node image does not include apparmor_parser. Mounting the
# guest's securityfs lets kubelet detect AppArmor, and this installs only the
# node-native parser binary needed by containerd to load RuntimeDefault. The
# package is extracted rather than installed so its service scripts cannot
# load unrelated profiles into the guest kernel.
install_apparmor_parser() {
  local node
  for node in "${cluster}-worker" "${cluster}-worker2"; do
    docker exec "$node" sh -ec '
      if ! apparmor_parser --version >/dev/null 2>&1; then
        apt-get update
        apt-get install --download-only -y --no-install-recommends apparmor
        archive="$(find /var/cache/apt/archives -maxdepth 1 -type f -name "apparmor_*.deb" -print -quit)"
        test -n "$archive"
        staging=/tmp/tt-apparmor-parser-package
        mkdir -p "$staging"
        dpkg-deb --extract "$archive" "$staging"
        parser="$(find "$staging" -type f -name apparmor_parser -print -quit)"
        test -n "$parser"
        install -m 0755 "$parser" /usr/sbin/apparmor_parser
      fi
      apparmor_parser --version
      systemctl restart containerd
      systemctl is-active --quiet containerd
    '
  done
}

# Image imports and the containerd restarts above can temporarily starve the
# control plane under QEMU TCG. Wait for both the API bootstrap hooks and all
# node heartbeats to recover before issuing mutating requests.
wait_cluster_recovery() {
  local attempt
  for ((attempt = 0; attempt < 120; attempt++)); do
    if kubectl --context "$kubectl_context" --request-timeout=15s get --raw=/readyz >/dev/null 2>&1 &&
      kubectl --context "$kubectl_context" --request-timeout=15s wait \
        --for=condition=Ready nodes --all --timeout=15s >/dev/null 2>&1; then
      return
    fi
    sleep 5
  done
  return 1
}

# kind returns while kube-apiserver is still completing post-start hooks. On a
# four-vCPU TCG guest, simultaneous worker and controller bootstrap can consume
# those hooks' fixed internal deadlines. Temporarily reserve CPU for the API,
# then restore the control plane before requiring normal cluster readiness.
bootstrap_tcg_cluster() {
  local control_plane="${cluster}-control-plane"
  local worker
  local attempt
  local api_ready=false
  local old_api_container
  local new_api_container
  local staging=/tmp/tt-static-pods

  for worker in "${cluster}-worker" "${cluster}-worker2"; do
    docker update --cpus 0.25 "$worker" >/dev/null
  done
  docker exec "$control_plane" sh -ec "
    mkdir -p '$staging'
    mv /etc/kubernetes/manifests/kube-controller-manager.yaml '$staging/'
    mv /etc/kubernetes/manifests/kube-scheduler.yaml '$staging/'
  "

  for ((attempt = 0; attempt < 120; attempt++)); do
    if kubectl --context "$kubectl_context" --request-timeout=15s get --raw=/readyz >/dev/null 2>&1; then
      api_ready=true
      break
    fi
    sleep 5
  done

  if [[ "$api_ready" == true ]]; then
    old_api_container="$(docker exec "$control_plane" crictl ps --quiet --name kube-apiserver | head -n 1)"
    docker exec "$control_plane" sh -ec '
      sed -i "/startupProbe:/,/volumeMounts:/ s/failureThreshold: 24/failureThreshold: 60/" \
        /etc/kubernetes/manifests/kube-apiserver.yaml
      sed -i "/livenessProbe:/,/readinessProbe:/ s/failureThreshold: 8/failureThreshold: 60/" \
        /etc/kubernetes/manifests/kube-apiserver.yaml
    '
    api_ready=false
    for ((attempt = 0; attempt < 120; attempt++)); do
      new_api_container="$(docker exec "$control_plane" crictl ps --quiet --name kube-apiserver | head -n 1)"
      if [[ -n "$new_api_container" && "$new_api_container" != "$old_api_container" ]] &&
        kubectl --context "$kubectl_context" --request-timeout=15s get --raw=/readyz >/dev/null 2>&1; then
        api_ready=true
        break
      fi
      sleep 5
    done
  fi

  if [[ "$api_ready" == true ]]; then
    kubectl --context "$kubectl_context" --request-timeout=60s delete lease \
      --namespace kube-system kube-controller-manager kube-scheduler \
      --ignore-not-found >/dev/null
  fi
  docker exec "$control_plane" sh -ec "
    mv '$staging/kube-controller-manager.yaml' /etc/kubernetes/manifests/
    mv '$staging/kube-scheduler.yaml' /etc/kubernetes/manifests/
  "
  if [[ "$api_ready" != true ]]; then
    return 1
  fi
  for worker in "${cluster}-worker" "${cluster}-worker2"; do
    docker update --cpus 0.75 "$worker" >/dev/null
  done
  wait_cluster_recovery
}

# TCG can still delay an individual mutating request after the cluster reports
# Ready. Retry Helm operations only after the API and every node recover again.
run_with_cluster_recovery() {
  local attempt
  for ((attempt = 1; attempt <= 3; attempt++)); do
    if "$@"; then
      return
    fi
    if ((attempt == 3)); then
      return 1
    fi
    echo "cluster operation failed (attempt $attempt/3); waiting for recovery" >&2
    wait_cluster_recovery
  done
}

label_node() {
  local node="$1"
  local attempt
  for ((attempt = 0; attempt < 60; attempt++)); do
    if kubectl --context "$kubectl_context" --request-timeout=20s label node "$node" \
      tenstorrent.com/enabled=true --overwrite; then
      return
    fi
    sleep 5
  done
  return 1
}

# cleanup_e2e removes only the validation-owned workloads and claims from this cluster.
cleanup_e2e() {
  if [[ "$cluster_ready" != true ]]; then
    return
  fi
  kubectl --context "$kubectl_context" delete tenstorrentworkload tt-e2e-topology --ignore-not-found --wait=true >/dev/null
  kubectl --context "$kubectl_context" delete pod -l tenstorrent.com/workload-name=tt-e2e-topology --ignore-not-found --wait=true >/dev/null
  kubectl --context "$kubectl_context" delete resourceclaim -l tenstorrent.com/workload-name=tt-e2e-topology --ignore-not-found --wait=true >/dev/null
  kubectl --context "$kubectl_context" delete pod tt-e2e-standard --ignore-not-found --wait=true >/dev/null
  kubectl --context "$kubectl_context" delete resourceclaim tt-e2e-standard --ignore-not-found --wait=true >/dev/null
  kubectl --context "$kubectl_context" delete pod tt-e2e-request-isolation --ignore-not-found --wait=true >/dev/null
  kubectl --context "$kubectl_context" delete resourceclaim tt-e2e-request-isolation --ignore-not-found --wait=true >/dev/null
}

# wait_claim_cleanup waits until kubelet removes a claim's CDI file and persisted ownership.
wait_claim_cleanup() {
  local node="$1"
  local uid="$2"
  local attempt
  for ((attempt = 0; attempt < 120; attempt++)); do
    if docker exec "$node" test ! -f "/var/run/cdi/claim-$uid.json" &&
      ! docker exec "$node" grep -q "$uid" /var/lib/tenstorrent-dra/claims.json; then
      return
    fi
    sleep 1
  done
  return 1
}

docker build --provenance=false --tag "$IMAGE_REPOSITORY:$IMAGE_TAG" "${candidate_image_args[@]}" "$repo_root"
docker build --platform linux/amd64 --provenance=false --file "$script_dir/e2e.Dockerfile" --tag "$E2E_IMAGE" "$script_dir"
kind create cluster --name "$cluster" --config "$script_dir/$config"
cluster_ready=true
trap cleanup_e2e EXIT
bootstrap_tcg_cluster
install_apparmor_parser
# The control-plane node is tainted and runs no project workloads. Avoid
# unpacking application images there so kube-apiserver retains its CPU budget.
worker_nodes="${cluster}-worker,${cluster}-worker2"
kind load docker-image "$IMAGE_REPOSITORY:$IMAGE_TAG" --name "$cluster" --nodes "$worker_nodes"
kind load docker-image "$E2E_IMAGE" --name "$cluster" --nodes "$worker_nodes"
wait_cluster_recovery
label_node "${cluster}-worker"
label_node "${cluster}-worker2"
helm_values=(
  --set image.repository="$IMAGE_REPOSITORY"
  --set image.tag="$IMAGE_TAG"
  --set image.pullPolicy=IfNotPresent
  --set sysfsRoot=/tt-sys/class/tenstorrent
  --set pciSysfsRoot=/tt-sys/bus/pci/devices
  --set sysfsDevicesRoot=/tt-sys/devices
  --set resetMode=noop
  --set requireIOMMU=false
  --set syntheticDisableWorkloadAppArmor="$DISABLE_WORKLOAD_APPARMOR"
)
run_with_cluster_recovery helm upgrade --install tt-dra "$repo_root/deployments/helm/tenstorrent-dra" \
  --kube-context "$kubectl_context" \
  "${helm_values[@]}" \
  --wait --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" rollout status deployment/tt-dra-controller --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" rollout status daemonset/tt-dra-node --timeout="$KUBE_WAIT_TIMEOUT"
for node in "${cluster}-worker" "${cluster}-worker2"; do
  docker exec "$node" sh -ec '
    test "$(find /var/lib/kubelet/plugins/dra.tenstorrent.com -maxdepth 1 -type s -name "dra-*.sock" | wc -l)" -eq 1
    test "$(find /var/lib/kubelet/plugins_registry -maxdepth 1 -type s -name "dra.tenstorrent.com-*-reg.sock" | wc -l)" -eq 1
  '
done
kubectl --context "$kubectl_context" wait --for=create "tenstorrentnodetopology/${cluster}-worker" --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" wait --for=create "tenstorrentnodetopology/${cluster}-worker2" --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" wait --for=jsonpath='{.status.valid}'=true tenstorrentfabrictopology/cluster --timeout="$KUBE_WAIT_TIMEOUT"

test "$(kubectl --context "$kubectl_context" get resourceslices -o jsonpath='{range .items[*].spec.devices[*]}{.name}{"\n"}{end}' | wc -w)" -eq 3
test "$(kubectl --context "$kubectl_context" get resourceslices -o jsonpath='{range .items[*].spec.devices[*]}{.attributes.tenstorrent\.com/health.string}{"\n"}{end}' | grep -c '^Healthy$')" -eq 3
test "$(kubectl --context "$kubectl_context" get tenstorrentnodetopologies -o name | wc -l)" -eq 2
inventory="$(kubectl --context "$kubectl_context" get resourceslices -o jsonpath='{range .items[*]}{range .spec.devices[*]}{.attributes.tenstorrent\.com/nodeName.string}{" "}{.name}{" "}{.attributes.tenstorrent\.com/chipSeries.string}{" "}{.attributes.tenstorrent\.com/health.string}{"\n"}{end}{end}' | sort)"
expected_inventory="$(printf '%s\n' "${cluster}-worker tt-uuid-tt-node-a-0 wormhole Healthy" "${cluster}-worker tt-uuid-tt-node-a-1 blackhole Healthy" "${cluster}-worker2 tt-uuid-tt-node-b-0 wormhole Healthy")"
test "$inventory" = "$expected_inventory"
test "$(kubectl --context "$kubectl_context" get tenstorrentfabrictopology cluster -o jsonpath='{.status.endpoints[*].endpointID}' | wc -w)" -eq 3
test "$(kubectl --context "$kubectl_context" get tenstorrentfabrictopology cluster -o jsonpath='{.status.errors}')" = '[]'

kubectl --context "$kubectl_context" apply -f "$script_dir/e2e-standard.yaml"
kubectl --context "$kubectl_context" wait --for=condition=Ready pod/tt-e2e-standard --timeout="$KUBE_WAIT_TIMEOUT"
standard_node="$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.spec.nodeName}')"
standard_uid="$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-standard -o jsonpath='{.metadata.uid}')"
standard_device="$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-standard -o jsonpath='{.status.allocation.devices.results[0].device}')"
standard_pod_uid="$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.metadata.uid}')"
test "$standard_device" = tt-uuid-tt-node-a-1
test "$(kubectl --context "$kubectl_context" logs tt-e2e-standard)" = devices=/dev/tenstorrent/1
docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"
docker exec "$standard_node" grep -q "$standard_uid" /var/lib/tenstorrent-dra/claims.json

# Upgrade and rollback both controller and node Pods while the prepared claim is
# running. The workload Pod, allocation, CDI file, and persisted ownership must
# remain unchanged through both node-agent restarts.
run_with_cluster_recovery helm upgrade tt-dra "$repo_root/deployments/helm/tenstorrent-dra" \
  --kube-context "$kubectl_context" \
  "${helm_values[@]}" \
  --set-string controller.podAnnotations.test-rollout=upgrade \
  --set-string node.podAnnotations.test-rollout=upgrade \
  --wait --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" rollout status deployment/tt-dra-controller --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" rollout status daemonset/tt-dra-node --timeout="$KUBE_WAIT_TIMEOUT"
test "$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.metadata.uid}')" = "$standard_pod_uid"
test "$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-standard -o jsonpath='{.status.allocation.devices.results[0].device}')" = "$standard_device"
docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"
docker exec "$standard_node" grep -q "$standard_uid" /var/lib/tenstorrent-dra/claims.json

run_with_cluster_recovery helm rollback tt-dra 1 --kube-context "$kubectl_context" --wait --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" rollout status deployment/tt-dra-controller --timeout="$KUBE_WAIT_TIMEOUT"
kubectl --context "$kubectl_context" rollout status daemonset/tt-dra-node --timeout="$KUBE_WAIT_TIMEOUT"
test "$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.metadata.uid}')" = "$standard_pod_uid"
test "$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-standard -o jsonpath='{.status.allocation.devices.results[0].device}')" = "$standard_device"
test "$(kubectl --context "$kubectl_context" get --raw /api/v1/namespaces/default/services/http:tt-dra-metrics:8080/proxy/readyz)" = ok
metrics_output="$(kubectl --context "$kubectl_context" get --raw /api/v1/namespaces/default/services/http:tt-dra-metrics:8080/proxy/metrics)"
grep -q '^tenstorrent_dra_component_ready' <<<"$metrics_output"
test -n "$(kubectl --context "$kubectl_context" get events --field-selector reason=ClaimPrepared -o name)"
node_logs="$(kubectl --context "$kubectl_context" logs daemonset/tt-dra-node)"
grep -q '"reconciliation_id"' <<<"$node_logs"

# The uninstall cleaner must fail closed before stopping either component while
# this claim is active.
kubectl --context "$kubectl_context" apply -f - <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: tt-cleanup-guard
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      serviceAccountName: tt-dra-controller
      containers:
      - name: cleanup
        image: $IMAGE_REPOSITORY:$IMAGE_TAG
        imagePullPolicy: IfNotPresent
        args: [cleanup, -release-name=tt-dra, -release-namespace=default, -resource-prefix=tt-dra]
EOF
kubectl --context "$kubectl_context" wait --for=condition=Failed job/tt-cleanup-guard --timeout="$KUBE_WAIT_TIMEOUT"
cleanup_logs="$(kubectl --context "$kubectl_context" logs job/tt-cleanup-guard)"
grep -q 'refusing cleanup' <<<"$cleanup_logs"
kubectl --context "$kubectl_context" get deployment/tt-dra-controller daemonset/tt-dra-node >/dev/null
test "$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.metadata.uid}')" = "$standard_pod_uid"
kubectl --context "$kubectl_context" delete job tt-cleanup-guard --wait=true

kubectl --context "$kubectl_context" delete pod tt-e2e-standard --wait=true
wait_claim_cleanup "$standard_node" "$standard_uid"
docker exec "$standard_node" sh -c "grep -F '\"claimUID\":\"$standard_uid\"' /var/lib/tenstorrent-dra/audit.jsonl | grep -q '\"action\":\"claim-release\"'"
kubectl --context "$kubectl_context" delete resourceclaim tt-e2e-standard --wait=true

kubectl --context "$kubectl_context" apply -f "$script_dir/e2e-request-isolation.yaml"
kubectl --context "$kubectl_context" wait --for=condition=Ready pod/tt-e2e-request-isolation --timeout="$KUBE_WAIT_TIMEOUT"
isolation_node="$(kubectl --context "$kubectl_context" get pod tt-e2e-request-isolation -o jsonpath='{.spec.nodeName}')"
isolation_uid="$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-request-isolation -o jsonpath='{.metadata.uid}')"
test "$(kubectl --context "$kubectl_context" logs tt-e2e-request-isolation -c wormhole)" = 'request=wormhole devices=/dev/tenstorrent/0'
test "$(kubectl --context "$kubectl_context" logs tt-e2e-request-isolation -c blackhole)" = 'request=blackhole devices=/dev/tenstorrent/1'
docker exec "$isolation_node" grep -q '"requests": \[' /var/lib/tenstorrent-dra/claims.json
docker exec "$isolation_node" grep -q '"wormhole"' /var/lib/tenstorrent-dra/claims.json
docker exec "$isolation_node" grep -q '"blackhole"' /var/lib/tenstorrent-dra/claims.json
kubectl --context "$kubectl_context" delete pod tt-e2e-request-isolation --wait=true
wait_claim_cleanup "$isolation_node" "$isolation_uid"
kubectl --context "$kubectl_context" delete resourceclaim tt-e2e-request-isolation --wait=true

kubectl --context "$kubectl_context" apply -f "$script_dir/e2e-topology.yaml"
kubectl --context "$kubectl_context" wait --for=jsonpath='{.status.phase}'=Running tenstorrentworkload/tt-e2e-topology --timeout="$KUBE_WAIT_TIMEOUT"
test "$(kubectl --context "$kubectl_context" get tenstorrentworkload tt-e2e-topology -o jsonpath='{range .status.assignments[*]}{.rank}{"\n"}{end}' | wc -l)" -eq 2
test "$(kubectl --context "$kubectl_context" get tenstorrentworkload tt-e2e-topology -o jsonpath='{range .status.assignments[*]}{.nodeName}{"\n"}{end}' | sort -u | wc -l)" -eq 1
test "$(kubectl --context "$kubectl_context" get tenstorrentworkload tt-e2e-topology -o jsonpath='{range .status.assignments[*].devices[*]}{.name}{"\n"}{end}' | sort | tr '\n' ' ')" = "tt-uuid-tt-node-a-0 tt-uuid-tt-node-a-1 "

topology_pods="$(kubectl --context "$kubectl_context" get pods -l tenstorrent.com/workload-name=tt-e2e-topology -o name)"
topology_claims="$(kubectl --context "$kubectl_context" get resourceclaims -l tenstorrent.com/workload-name=tt-e2e-topology -o name)"
test "$(wc -w <<<"$topology_pods")" -eq 2
test "$(wc -w <<<"$topology_claims")" -eq 2
for pod in $topology_pods; do
  kubectl --context "$kubectl_context" wait --for=condition=Ready "$pod" --timeout="$KUBE_WAIT_TIMEOUT"
  kubectl --context "$kubectl_context" logs "$pod" | grep -Eq '^rank=[01] world=2 devices=/dev/tenstorrent/[01]$'
done

topology_artifacts=()
for claim in $topology_claims; do
  claim_name="${claim##*/}"
  claim_uid="$(kubectl --context "$kubectl_context" get resourceclaim "$claim_name" -o jsonpath='{.metadata.uid}')"
  claim_node="$(kubectl --context "$kubectl_context" get resourceclaim "$claim_name" -o jsonpath='{.status.allocation.devices.results[0].pool}')"
  docker exec "$claim_node" test -f "/var/run/cdi/claim-$claim_uid.json"
  docker exec "$claim_node" grep -q "$claim_uid" /var/lib/tenstorrent-dra/claims.json
  topology_artifacts+=("$claim_node" "$claim_uid")
done

kubectl --context "$kubectl_context" delete tenstorrentworkload tt-e2e-topology --wait=true
kubectl --context "$kubectl_context" delete pod -l tenstorrent.com/workload-name=tt-e2e-topology --ignore-not-found --wait=true
kubectl --context "$kubectl_context" delete resourceclaim -l tenstorrent.com/workload-name=tt-e2e-topology --ignore-not-found --wait=true
for resource in $topology_pods $topology_claims; do
  kubectl --context "$kubectl_context" wait --for=delete "$resource" --timeout="$KUBE_WAIT_TIMEOUT"
done
set -- "${topology_artifacts[@]}"
while [[ "$#" -gt 0 ]]; do
  wait_claim_cleanup "$1" "$2"
  docker exec "$1" sh -c "grep -F '\"claimUID\":\"$2\"' /var/lib/tenstorrent-dra/audit.jsonl | grep -q '\"action\":\"claim-release\"'"
  shift 2
done

kubectl --context "$kubectl_context" get resourceslices
kubectl --context "$kubectl_context" get tenstorrentnodetopologies
kubectl --context "$kubectl_context" get tenstorrentfabrictopologies
cleanup_e2e
helm uninstall tt-dra --kube-context "$kubectl_context" --wait --timeout="$KUBE_WAIT_TIMEOUT"
test -z "$(kubectl --context "$kubectl_context" get all,serviceaccounts,configmaps,roles,rolebindings,poddisruptionbudgets,leases,networkpolicies -l app.kubernetes.io/instance=tt-dra -o name)"
test -z "$(kubectl --context "$kubectl_context" get clusterroles,clusterrolebindings,priorityclasses,deviceclasses,validatingadmissionpolicies,validatingadmissionpolicybindings -l app.kubernetes.io/instance=tt-dra -o name)"
test -z "$(kubectl --context "$kubectl_context" get resourceslices -o name)"
test -z "$(kubectl --context "$kubectl_context" get tenstorrentnodetopologies -o name)"
test -z "$(kubectl --context "$kubectl_context" get tenstorrentfabrictopologies -o name)"
trap - EXIT
