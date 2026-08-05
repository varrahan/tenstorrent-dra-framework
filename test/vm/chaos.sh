#!/usr/bin/env bash
set -euo pipefail

cluster="${1:-tt-dra-chaos}"
config="${2:-kind/ttsim-dra.yaml}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
evidence_dir="${3:-$repo_root/artifacts/chaos-$(date -u +%Y%m%dT%H%M%SZ)}"
readonly IMAGE_REPOSITORY="tenstorrent-dra"
readonly IMAGE_TAG="dev"
readonly E2E_IMAGE="tenstorrent-dra-e2e:dev"
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
worker_a="${cluster}-worker"
worker_b="${cluster}-worker2"
control_plane="${cluster}-control-plane"
fixture_b="/tmp/tt-node-b/sys/devices/tt/0"
cluster_created=false
release_installed=false
api_paused=false
device_moved=false

mkdir -p "$evidence_dir"

record() {
  printf '%s\t%b\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" | tee -a "$evidence_dir/results.tsv"
}

device_count() {
  kubectl --context "$kubectl_context" get resourceslices -o json |
    python3 -c 'import json, sys; data=json.load(sys.stdin); node=sys.argv[1]; print(sum(len(item["spec"].get("devices") or []) for item in data["items"] if item["spec"].get("nodeName") == node))' "$1"
}

node_health_is() {
  local node="$1"
  local expected="$2"
  local actual
  actual="$(kubectl --context "$kubectl_context" get "node/$node" -o jsonpath='{.status.conditions[?(@.type=="TenstorrentAcceleratorsHealthy")].status}' 2>/dev/null)"
  [[ "$actual" == "$expected" ]]
}

device_count_is() {
  [[ "$(device_count "$1")" == "$2" ]]
}

driver_ready_on() {
  local node="$1"
  local pod
  pod="$(kubectl --context "$kubectl_context" get pods -l app.kubernetes.io/component=node --field-selector "spec.nodeName=$node" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
  [[ -n "$pod" ]] && [[ "$(kubectl --context "$kubectl_context" get "pod/$pod" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)" == "True" ]]
}

pod_ready() {
  [[ "$(kubectl --context "$kubectl_context" get "pod/$1" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)" == "True" ]]
}

claim_state_absent() {
  local node="$1"
  local uid="$2"
  ! docker exec "$node" grep -q "$uid" /var/lib/tenstorrent-dra/claims.json &&
    docker exec "$node" test ! -f "/var/run/cdi/claim-$uid.json"
}

resource_gone() {
  ! kubectl --context "$kubectl_context" get "$1" >/dev/null 2>&1
}

workload_phase_is() {
  [[ "$(kubectl --context "$kubectl_context" get tenstorrentworkload/tt-e2e-topology -o jsonpath='{.status.phase}' 2>/dev/null)" == "$1" ]]
}

leader_changed() {
  local previous="$1"
  local current
  current="$(kubectl --context "$kubectl_context" get lease tt-dra-controller -o jsonpath='{.spec.holderIdentity}' 2>/dev/null)"
  [[ -n "$current" && "$current" != "$previous" ]]
}

wait_for() {
  local description="$1"
  shift
  local attempt
  for ((attempt = 0; attempt < 120; attempt++)); do
    if "$@"; then
      return
    fi
    sleep 1
  done
  printf 'timed out waiting for %s\n' "$description" >&2
  return 1
}

snapshot_safety() {
  local name="$1"
  local destination="$evidence_dir/safety/$name"
  local node
  mkdir -p "$destination"
  for node in "$worker_a" "$worker_b"; do
    mkdir -p "$destination/$node"
    docker cp "$node:/var/lib/tenstorrent-dra/claims.json" "$destination/$node/claims.json" >/dev/null
    docker exec "$node" find /var/run/cdi -maxdepth 1 -type f -name 'claim-*.json' -print >"$destination/$node/cdi-files.txt"
  done
  python3 "$script_dir/assert_safety.py" "$destination" | tee "$destination/summary.json"
}

collect_evidence() {
  if [[ "$cluster_created" != true ]]; then
    return
  fi
  kubectl --context "$kubectl_context" get nodes,pods,resourceclaims,resourceslices -A -o yaml >"$evidence_dir/kubernetes.yaml" 2>&1 || true
  kubectl --context "$kubectl_context" get tenstorrentworkloads,tenstorrentnodetopologies,tenstorrentfabrictopologies -A -o yaml >"$evidence_dir/tenstorrent.yaml" 2>&1 || true
  kubectl --context "$kubectl_context" get events -A --sort-by=.lastTimestamp >"$evidence_dir/events.txt" 2>&1 || true
  kubectl --context "$kubectl_context" logs deployment/tt-dra-controller --all-pods=true --prefix=true >"$evidence_dir/controller.log" 2>&1 || true
  kubectl --context "$kubectl_context" logs daemonset/tt-dra-node --prefix=true >"$evidence_dir/node.log" 2>&1 || true
  snapshot_safety final >"$evidence_dir/final-safety.txt" 2>&1 || true
}

cleanup() {
  local result=$?
  trap - EXIT
  set +e
  if [[ "$api_paused" == true ]]; then
    docker unpause "$control_plane" >/dev/null
  fi
  if [[ "$device_moved" == true ]]; then
    mv /tmp/tt-node-b/tenstorrent-0.unplugged /tmp/tt-node-b/sys/class/tenstorrent/0
  fi
  collect_evidence
  kubectl --context "$kubectl_context" patch resourceclaim -A -l tenstorrent.com/workload-name=tt-e2e-topology --type=merge -p '{"metadata":{"finalizers":null}}' >/dev/null 2>&1
  kubectl --context "$kubectl_context" delete namespace tt-chaos-delete --ignore-not-found --wait=false >/dev/null 2>&1
  kubectl --context "$kubectl_context" delete tenstorrentworkload tt-e2e-topology --ignore-not-found --wait=false >/dev/null 2>&1
  kubectl --context "$kubectl_context" delete pod,resourceclaim tt-e2e-standard --ignore-not-found --wait=false >/dev/null 2>&1
  if [[ "$release_installed" == true ]]; then
    helm uninstall tt-dra --kube-context "$kubectl_context" --wait --timeout=180s >/dev/null 2>&1
  fi
  if [[ "$cluster_created" == true ]]; then
    kind delete cluster --name "$cluster" >/dev/null 2>&1
  fi
  if ((result == 0)); then
    record 'suite\tPASS'
  else
    record "suite\tFAIL\texit=$result"
  fi
  printf 'chaos evidence: %s\n' "$evidence_dir"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

go -C "$repo_root" test -race ./src/test -run '^(TestControllerRetriesWorkloadStatusConflict|TestInventoryFailureFencesKnownCapacity|TestJanitorRetainsOwnershipWhenPostflightSanitizationFails|TestPrepareRetriesAfterResetFailure)$'
record 'api-conflict-inventory-reset-scrub-failure-injection\tPASS'

record 'setup\tSTART'
docker build --provenance=false --tag "$IMAGE_REPOSITORY:$IMAGE_TAG" "${candidate_image_args[@]}" "$repo_root"
docker build --platform linux/amd64 --provenance=false --file "$script_dir/e2e.Dockerfile" --tag "$E2E_IMAGE" "$script_dir"
kind create cluster --name "$cluster" --config "$script_dir/$config"
cluster_created=true
kind load docker-image "$IMAGE_REPOSITORY:$IMAGE_TAG" --name "$cluster"
kind load docker-image "$E2E_IMAGE" --name "$cluster"
kubectl --context "$kubectl_context" label node "$worker_a" tenstorrent.com/enabled=true --overwrite
kubectl --context "$kubectl_context" label node "$worker_b" tenstorrent.com/enabled=true --overwrite
helm upgrade --install tt-dra "$repo_root/deployments/helm/tenstorrent-dra" \
  --kube-context "$kubectl_context" \
  --set image.repository="$IMAGE_REPOSITORY" \
  --set image.tag="$IMAGE_TAG" \
  --set image.pullPolicy=IfNotPresent \
  --set sysfsRoot=/tt-sys/class/tenstorrent \
  --set pciSysfsRoot=/tt-sys/bus/pci/devices \
  --set sysfsDevicesRoot=/tt-sys/devices \
  --set interval=2s \
  --set inventoryGracePeriod=6s \
  --set topologyTTL=12s \
  --set controller.minReadySeconds=0 \
  --set node.minReadySeconds=0 \
  --set resetMode=noop \
  --set requireIOMMU=false \
  --set syntheticDisableWorkloadAppArmor=true \
  --wait --timeout=180s
release_installed=true
kubectl --context "$kubectl_context" rollout status deployment/tt-dra-controller --timeout=120s
kubectl --context "$kubectl_context" rollout status daemonset/tt-dra-node --timeout=120s
wait_for 'worker A inventory' device_count_is "$worker_a" 2
wait_for 'worker B inventory' device_count_is "$worker_b" 1
snapshot_safety setup
record 'setup\tPASS'

printf 'Unknown\n' >"$fixture_b/health"
wait_for 'unknown health fencing' node_health_is "$worker_b" False
wait_for 'unknown health withdrawal' device_count_is "$worker_b" 0
snapshot_safety health-unknown
printf 'Healthy\n' >"$fixture_b/health"
wait_for 'unknown health recovery' node_health_is "$worker_b" True
wait_for 'unknown health republication' device_count_is "$worker_b" 1
record 'health-unknown\tPASS'

for fault in OOM HANG; do
  printf '%s\n' "$fault" >"$fixture_b/fault_code"
  wait_for "$fault fencing" node_health_is "$worker_b" False
  wait_for "$fault withdrawal" device_count_is "$worker_b" 0
  snapshot_safety "fault-${fault,,}"
  printf '0\n' >"$fixture_b/fault_code"
  wait_for "$fault recovery" node_health_is "$worker_b" True
  wait_for "$fault republication" device_count_is "$worker_b" 1
  record "fault-$fault\tPASS"
done

printf 'down\n' >"$fixture_b/fabric_links/link0/state"
wait_for 'fabric-link fencing' node_health_is "$worker_b" False
wait_for 'fabric-link withdrawal' device_count_is "$worker_b" 0
snapshot_safety fabric-link-down
printf 'up\n' >"$fixture_b/fabric_links/link0/state"
wait_for 'fabric-link recovery' node_health_is "$worker_b" True
wait_for 'fabric-link republication' device_count_is "$worker_b" 1
record 'fabric-link-loss\tPASS'

mv /tmp/tt-node-b/sys/class/tenstorrent/0 /tmp/tt-node-b/tenstorrent-0.unplugged
device_moved=true
wait_for 'hot-unplug fencing' node_health_is "$worker_b" False
wait_for 'hot-unplug withdrawal' device_count_is "$worker_b" 0
snapshot_safety hot-unplug
mv /tmp/tt-node-b/tenstorrent-0.unplugged /tmp/tt-node-b/sys/class/tenstorrent/0
device_moved=false
wait_for 'hot-plug recovery' node_health_is "$worker_b" True
wait_for 'hot-plug republication' device_count_is "$worker_b" 1
record 'hot-unplug\tPASS'

kubectl --context "$kubectl_context" apply -f "$script_dir/e2e-standard.yaml"
kubectl --context "$kubectl_context" wait --for=condition=Ready pod/tt-e2e-standard --timeout=120s
standard_node="$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.spec.nodeName}')"
standard_uid="$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-standard -o jsonpath='{.metadata.uid}')"
docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"
snapshot_safety claim-prepared
record 'claim-prepare\tPASS'

node_pod="$(kubectl --context "$kubectl_context" get pods -l app.kubernetes.io/component=node --field-selector "spec.nodeName=$standard_node" -o name)"
kubectl --context "$kubectl_context" delete "$node_pod" --wait=false
wait_for 'node agent restart' driver_ready_on "$standard_node"
docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"
snapshot_safety node-agent-restart
record 'node-agent-restart-prepared\tPASS'

docker exec "$standard_node" systemctl restart kubelet
wait_for 'kubelet restart node readiness' node_health_is "$standard_node" True
kubectl --context "$kubectl_context" wait --for=condition=Ready pod/tt-e2e-standard --timeout=120s
docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"
snapshot_safety kubelet-restart
record 'kubelet-restart-prepared\tPASS'

docker pause "$control_plane" >/dev/null
api_paused=true
sleep 5
docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"
docker unpause "$control_plane" >/dev/null
api_paused=false
kubectl --context "$kubectl_context" rollout status deployment/tt-dra-controller --timeout=120s
kubectl --context "$kubectl_context" rollout status daemonset/tt-dra-node --timeout=120s
snapshot_safety api-interruption
record 'kubernetes-api-interruption\tPASS'

docker restart "$standard_node" >/dev/null
kubectl --context "$kubectl_context" wait --for=condition=Ready "node/$standard_node" --timeout=120s
wait_for 'node agent after node reboot' driver_ready_on "$standard_node"
reboot_outcome=""
for ((attempt = 0; attempt < 60; attempt++)); do
  if pod_ready tt-e2e-standard && docker exec "$standard_node" test -f "/var/run/cdi/claim-$standard_uid.json"; then
    reboot_outcome=prepared
    break
  fi
  if ! pod_ready tt-e2e-standard && claim_state_absent "$standard_node" "$standard_uid"; then
    reboot_outcome=released
    break
  fi
  sleep 1
done
if [[ -z "$reboot_outcome" ]]; then
  printf 'node reboot left neither safely prepared nor safely released state\n' >&2
  exit 1
fi
snapshot_safety node-reboot
record "node-reboot-$reboot_outcome\tPASS"
if [[ "$reboot_outcome" == released ]]; then
  kubectl --context "$kubectl_context" delete pod tt-e2e-standard --force --grace-period=0 --ignore-not-found --wait=false
  kubectl --context "$kubectl_context" delete resourceclaim tt-e2e-standard --wait=true
  kubectl --context "$kubectl_context" apply -f "$script_dir/e2e-standard.yaml"
  kubectl --context "$kubectl_context" wait --for=condition=Ready pod/tt-e2e-standard --timeout=120s
  standard_node="$(kubectl --context "$kubectl_context" get pod tt-e2e-standard -o jsonpath='{.spec.nodeName}')"
  standard_uid="$(kubectl --context "$kubectl_context" get resourceclaim tt-e2e-standard -o jsonpath='{.metadata.uid}')"
fi

node_pod="$(kubectl --context "$kubectl_context" get pods -l app.kubernetes.io/component=node --field-selector "spec.nodeName=$standard_node" -o name)"
kubectl --context "$kubectl_context" label "node/$standard_node" tenstorrent.com/enabled=false --overwrite
wait_for 'node agent stop for orphan injection' resource_gone "pod/$(basename "$node_pod")"
docker cp "$standard_node:/var/lib/tenstorrent-dra/claims.json" "$evidence_dir/orphan-before.json" >/dev/null
docker exec "$standard_node" rm -f /var/lib/tenstorrent-dra/claims.json
kubectl --context "$kubectl_context" label "node/$standard_node" tenstorrent.com/enabled=true --overwrite
wait_for 'node agent orphan recovery' driver_ready_on "$standard_node"
docker exec "$standard_node" grep -q '"phase": "Recovered"' /var/lib/tenstorrent-dra/claims.json
snapshot_safety orphan-recovery
record 'orphaned-claim-recovery\tPASS'

kubectl --context "$kubectl_context" apply -f "$script_dir/e2e-topology.yaml"
wait_for 'topology workload blocked by standard claim' workload_phase_is Pending
prior_leader="$(kubectl --context "$kubectl_context" get lease tt-dra-controller -o jsonpath='{.spec.holderIdentity}')"
kubectl --context "$kubectl_context" delete "pod/$prior_leader" --wait=false
wait_for 'controller leader failover' leader_changed "$prior_leader"
snapshot_safety competing-workload
record 'competing-standard-topology-and-leader-failover\tPASS'

kubectl --context "$kubectl_context" delete pod tt-e2e-standard --force --grace-period=0 --wait=false
node_pod="$(kubectl --context "$kubectl_context" get pods -l app.kubernetes.io/component=node --field-selector "spec.nodeName=$standard_node" -o name)"
kubectl --context "$kubectl_context" delete "$node_pod" --wait=false
wait_for 'node agent restart during unprepare' driver_ready_on "$standard_node"
wait_for 'claim cleanup after forced deletion' claim_state_absent "$standard_node" "$standard_uid"
kubectl --context "$kubectl_context" delete resourceclaim tt-e2e-standard --wait=true
wait_for 'topology workload after release' workload_phase_is Running
snapshot_safety forced-delete-and-replan
record 'forced-pod-delete-unprepare-restart-replan\tPASS'

delayed_claim="$(kubectl --context "$kubectl_context" get resourceclaims -l tenstorrent.com/workload-name=tt-e2e-topology -o jsonpath='{.items[0].metadata.name}')"
kubectl --context "$kubectl_context" patch "resourceclaim/$delayed_claim" --type=merge -p '{"metadata":{"finalizers":["chaos.tenstorrent.com/gc-delay"]}}'
prior_leader="$(kubectl --context "$kubectl_context" get lease tt-dra-controller -o jsonpath='{.spec.holderIdentity}')"
kubectl --context "$kubectl_context" delete tenstorrentworkload tt-e2e-topology --wait=false
kubectl --context "$kubectl_context" delete "pod/$prior_leader" --wait=false
wait_for 'leader failover during cleanup' leader_changed "$prior_leader"
kubectl --context "$kubectl_context" get "resourceclaim/$delayed_claim" >/dev/null
snapshot_safety garbage-collection-delay
kubectl --context "$kubectl_context" patch "resourceclaim/$delayed_claim" --type=merge -p '{"metadata":{"finalizers":null}}'
wait_for 'delayed claim garbage collection' resource_gone "resourceclaim/$delayed_claim"
record 'workload-delete-gc-delay-leader-failover\tPASS'

kubectl --context "$kubectl_context" create namespace tt-chaos-delete
kubectl --context "$kubectl_context" apply -n tt-chaos-delete -f "$script_dir/e2e-standard.yaml"
kubectl --context "$kubectl_context" wait -n tt-chaos-delete --for=condition=Ready pod/tt-e2e-standard --timeout=120s
namespace_node="$(kubectl --context "$kubectl_context" get -n tt-chaos-delete pod tt-e2e-standard -o jsonpath='{.spec.nodeName}')"
namespace_uid="$(kubectl --context "$kubectl_context" get -n tt-chaos-delete resourceclaim tt-e2e-standard -o jsonpath='{.metadata.uid}')"
kubectl --context "$kubectl_context" delete namespace tt-chaos-delete --wait=false
wait_for 'namespace deletion' resource_gone namespace/tt-chaos-delete
wait_for 'namespace claim cleanup' claim_state_absent "$namespace_node" "$namespace_uid"
snapshot_safety namespace-delete
record 'namespace-deletion-cleanup\tPASS'

record 'all-chaos-cases\tPASS'
