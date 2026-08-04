# Production operations

This document is the production operations contract for the Tenstorrent DRA
node agent and controller. Hardware- and Kubernetes-dependent commands run
inside the QEMU `ttsim` Ubuntu VM or an equivalent supported cluster node.

## Deployment baseline

The Helm chart deploys two controller replicas with leader election, a
`PodDisruptionBudget` with `minAvailable: 1`, preferred pod anti-affinity, and a
hostname topology-spread constraint. Controller rollout uses zero unavailable
replicas and one surge replica. The node DaemonSet rolls one node at a time;
surge is deliberately disabled because two agents cannot hold the same host
state lock. Persisted ownership and CDI files remain on the host across an
agent restart.

The default resource envelope for each controller and node-agent container is
100m CPU and 128Mi memory requested, with limits of 1 CPU and 512Mi. Controller
Pods receive a lower priority than the node lifecycle agents. Both remain below
the reserved `system-cluster-critical` and `system-node-critical` priorities.
Node agents tolerate the driver's own unhealthy-accelerator taint so the
component responsible for recovery is never evicted by that taint. Controller
and node termination grace periods are 30 and 60 seconds respectively.

The chart's `values.schema.json` rejects unknown keys, relative host paths,
invalid durations and ports, fewer than two controller replicas, invalid image
digests, non-positive API limits, and the unsafe combination of `resetMode=noop`
with IOMMU enforcement. `noop` remains a synthetic-validation mode only. Use an
image digest for a release installation:

```bash
helm upgrade --install tt-dra deployments/helm/tenstorrent-dra \
  --namespace tenstorrent-system --create-namespace \
  --set image.repository=registry.example/tenstorrent-dra \
  --set image.tag='' \
  --set image.digest=sha256:<64-hex-digits> \
  --atomic --wait --timeout=10m
```

The startup probe succeeds after command initialization, readiness means the
component can perform its Kubernetes role, and liveness means its HTTP serving
loop remains alive. Accelerator health does not fail process liveness; it
withdraws capacity, changes the node condition, and raises operational alerts.
Verify the release signature and attestations before installation, and record
the resolved digest with the change ticket. The exact commands and immutable
artifact policy are in [`RELEASE.md`](RELEASE.md).

## NetworkPolicy decision

NetworkPolicies are not required for the driver isolation boundary. The
controller and node agent initiate connections to the Kubernetes API server,
and the node agent also serves kubelet over its host-mounted Unix socket. A
portable egress policy for API-server virtual IPs, control-plane endpoints, and
DNS cannot be generated safely by this chart, so API egress is deliberately
unrestricted.

The only TCP listener is the HTTP health and metrics port. If the cluster uses
default-deny ingress, enable `networkPolicy.enabled` and select the Prometheus
namespace and Pods with `networkPolicy.namespaceSelector` and
`networkPolicy.podSelector`. The supplied policy is ingress-only. Confirm that
the CNI permits kubelet health probes before enabling it:

```bash
helm upgrade tt-dra deployments/helm/tenstorrent-dra \
  --reuse-values \
  --set networkPolicy.enabled=true \
  --set networkPolicy.namespaceSelector.kubernetes\.io/metadata\.name=monitoring \
  --set networkPolicy.podSelector.app\.kubernetes\.io/name=prometheus \
  --atomic --wait
```

## Logs, Events, and metrics

Both commands emit one-line JSON logs. Common fields include `component`,
`reconciliation_id`, `reconciliation_kind`, `duration_seconds`, and `outcome`.
Node lifecycle decisions add `node`, `claim_namespace`, `claim`, `claim_uid`,
`device`, `device_path`, `action`, and `reason`. Workload decisions add
`workload_namespace`, `workload`, `workload_uid`, `phase`, and assignment count.
Do not use claim, workload, or device identifiers as metric labels; they are
available in logs and Kubernetes Events instead.

The components emit correlated Kubernetes Events for claim preparation and
release, reset-backed scrubbing, health and quarantine transitions, topology
validation and publication, workload assignment and phase changes, and leader
election. Start an incident timeline with:

```bash
kubectl get events -A --sort-by=.lastTimestamp
kubectl logs -n tenstorrent-system deployment/tt-dra-controller --since=30m
kubectl logs -n tenstorrent-system daemonset/tt-dra-node --since=30m
```

The chart creates `tt-dra-metrics` by default. `/startupz`, `/readyz`,
`/livez`, and `/metrics` are served on port 8080. Enable the `ServiceMonitor`
only when the Prometheus Operator CRDs are installed.

| Signal | Meaning |
| --- | --- |
| `tenstorrent_dra_component_ready` | Ready controller and node processes |
| `tenstorrent_dra_inventory_last_success_timestamp_seconds` | Last successful node observation |
| `tenstorrent_dra_inventory_age_seconds` | Age of inventory used for publication |
| `tenstorrent_dra_inventory_grace_period_seconds` | Configured publication freshness bound |
| `tenstorrent_dra_devices{state="published\|allocated\|quarantined"}` | Current node capacity and ownership |
| `tenstorrent_dra_claim_operation_duration_seconds` | Prepare and unprepare latency |
| `tenstorrent_dra_claim_operation_failures_total` | Prepare and unprepare failures |
| `tenstorrent_dra_hardware_operation_duration_seconds` | Reset and scrub-boundary latency by phase |
| `tenstorrent_dra_hardware_operations_total` | Reset and scrub outcomes by phase |
| `tenstorrent_dra_topology_valid`, `tenstorrent_dra_topology_errors` | Cluster fabric validity |
| `tenstorrent_dra_placement_duration_seconds`, `tenstorrent_dra_placement_attempts_total` | Placement latency and result |
| `tenstorrent_dra_reconciliation_duration_seconds`, `tenstorrent_dra_reconciliation_failures_total` | Controller and node loop behavior |

`dashboard.enabled=true` installs the `Tenstorrent DRA Operations` Grafana
dashboard ConfigMap. `alerts.enabled=true` installs Prometheus rules for every
production failure surface above and requires `monitoring.coreos.com/v1`. The
DaemonSet-unavailable rule also uses the standard `kube-state-metrics` series;
all other rules use metrics exported by this driver.

## Service-level objectives

Measure these objectives over a rolling 30-day window. Planned, declared node
maintenance is excluded only while the accelerator node is cordoned and no
claim is allocated to it.

| Objective | Target | Measurement |
| --- | --- | --- |
| Controller availability | 99.95% with at least one ready replica | `sum(tenstorrent_dra_component_ready{component="controller"}) >= 1` |
| Eligible-node agent availability | 99.9% per node | `tenstorrent_dra_component_ready{component="node"} == 1` |
| Claim lifecycle success | 99.9% of prepare/unprepare calls | Failure counter divided by operation count derived from histogram count |
| Prepare latency | p95 <= 5s, p99 <= 10s | Claim-operation histogram, `operation="prepare"` |
| Topology placement latency | p99 <= 500ms | Placement histogram |
| Inventory fault fencing | <= 90s from lost observation to empty publication | Last-success timestamp, ResourceSlice observation, and Events |
| Controller recovery | <= 60s after a replica or leader failure | Ready gauge and `BecameLeader` Event |
| Node-agent recovery | <= 5m after process or node restart | Ready gauge plus startup-recovery Events |
| Tenant sanitization | 100% success before access and before release | Reset and scrub counters; any failure keeps ownership quarantined |

Sanitization is a safety invariant, not an error-budget objective. A failed
reset or scrub must never be converted into availability by manually clearing
state or quarantine.

## Incident response

For every page:

1. Record the alert labels, first firing time, recent Events, and the relevant
   `reconciliation_id`, claim UID, workload UID, node, and device.
2. Stop new demand if safety is uncertain. Cordon the affected node; do not
   delete lifecycle state or CDI files.
3. Determine whether the device is actively owned from the ResourceClaim and
   `/var/lib/tenstorrent-dra/claims.json` on the node.
4. Follow the specific runbook below. Preserve `audit.jsonl` and the corrupt
   state backup for post-incident review.
5. Restore scheduling only after a successful sanitization and a fresh healthy
   observation have removed quarantine automatically.

### Controller unavailable or reconciliation failures

```bash
kubectl -n tenstorrent-system get pods,pdb,lease
kubectl -n tenstorrent-system describe lease tt-dra-controller
kubectl -n tenstorrent-system logs deployment/tt-dra-controller --since=30m
kubectl auth can-i --as=system:serviceaccount:tenstorrent-system:tt-dra-controller \
  update tenstorrentworkloads.scheduling.tenstorrent.com/status --all-namespaces
```

Confirm API-server reachability, lease renewals, informer list/watch errors,
memory limits, and reconciliation failure kind. A healthy standby should assume
leadership within 60 seconds. If both replicas fail identically, roll back the
release rather than repeatedly restarting them.

### Node agent unavailable or stale inventory

```bash
kubectl -n tenstorrent-system get pod -l app.kubernetes.io/component=node -o wide
kubectl describe node <node>
kubectl -n tenstorrent-system logs daemonset/tt-dra-node --since=30m
sudo find /sys/class/tenstorrent -maxdepth 2 -type f -o -type l
sudo ls -l /dev/tenstorrent
```

Check `tt-kmd`, sysfs mounts, API connectivity, the kubelet registrar path, and
the host state lock. Inventory may use its last fresh observation only within
`inventoryGracePeriod`; after that the expected result is empty capacity and an
unsafe node condition.

### Claim prepare or unprepare failure

Inspect the ResourceClaim allocation, claim Events, node log entries with its
UID, the CDI file, persisted phase, and audit trail:

```bash
kubectl -n <namespace> describe resourceclaim <claim>
sudo grep '<claim-uid>' /var/lib/tenstorrent-dra/audit.jsonl
sudo grep '<claim-uid>' /var/lib/tenstorrent-dra/claims.json
sudo test -e /var/run/cdi/claim-<claim-uid>.json
```

Let kubelet retry. A failed unprepare intentionally retains CDI and ownership
until post-use sanitization succeeds. Never delete either file to force reuse.

### Sanitization failure

Cordon the node immediately. Determine whether the failing phase was
preflight, postflight, or recovery and preserve the `tt-kmd` and kernel logs.
Do not retry through an unbounded shell loop and do not clear quarantine. If a
normal kubelet retry does not succeed after the underlying KMD or hardware issue
is fixed, drain other workloads and follow the node-replacement procedure.

### Invalid fabric topology

```bash
kubectl get tenstorrentfabrictopology cluster -o yaml
kubectl get tenstorrentnodetopologies -o yaml
kubectl get events -A --field-selector reason=TopologyInvalid
```

Resolve stale nodes, duplicate endpoint IDs, missing or asymmetric peers,
cross-ring links, and down links at their source. Running assignments remain
frozen and report degradation; unstarted topology workloads wait or replan.

### Hardware quarantine and node replacement

1. Cordon the node and identify all claims whose allocation pool is the node.
2. Allow owners to terminate normally so kubelet can unprepare and sanitize.
3. Drain non-accelerator workloads. Never force removal of an allocated card's
   persisted ownership.
4. Stop kubelet and the node agent before replacing hardware.
5. Preserve `/var/lib/tenstorrent-dra/audit.jsonl` and `claims.json`.
6. Replace the device, verify its production UUID, KMD, firmware, IOMMU group,
   and fabric links, then restart kubelet and the agent.
7. The new identity is quarantined until recovery sanitization and a subsequent
   healthy observation succeed. Only then uncordon the node.

### State recovery

On startup, inspect `state-recovery` and `startup-recovery` Events and audit
records. A corrupt state file is retained as `claims.json.corrupt-<timestamp>`;
all visible devices remain quarantined. Recover `Recovered`, `Preparing`, or
`Releasing` claims through normal kubelet unprepare after the allocation is no
longer in use. Do not edit JSON or CDI files. Escalate if the API allocation and
persisted owner disagree after the kubelet has reconciled.

### Upgrade, rollback, and uninstall

Back up all three custom-resource kinds, apply CRDs first, and wait for
establishment as described in `PRODUCTION.md`. Then:

```bash
helm upgrade tt-dra deployments/helm/tenstorrent-dra \
  --namespace tenstorrent-system --atomic --wait --timeout=10m
kubectl -n tenstorrent-system rollout status deployment/tt-dra-controller
kubectl -n tenstorrent-system rollout status daemonset/tt-dra-node
```

Observe prepared claims throughout the rollout. Their Pod UID, allocation, CDI
file, and persisted ownership must not change. If the new revision fails:

```bash
helm history tt-dra -n tenstorrent-system
helm rollback tt-dra <known-good-revision> -n tenstorrent-system \
  --wait --timeout=10m
```

The pre-delete hook refuses uninstall while a Tenstorrent allocation or active
`TenstorrentWorkload` exists. Drain all claims and workloads first. The hook
then stops controller and node Pods and removes generated ResourceSlices and
topology objects before Helm removes release-owned namespaced and cluster
resources:

```bash
helm uninstall tt-dra -n tenstorrent-system --wait --timeout=10m
```

Helm intentionally retains CRDs from the chart `crds/` directory. They are API
data definitions, not leaked release resources. Delete them manually only after
backing up and removing every custom resource, and only when no other release
uses the APIs. Host audit and empty state directories are also retained for
forensics.

### Capacity and API-server pressure

At the declared maximum, the controller supports 256 nodes, 256 fabrics, 2,048
endpoints, 1,000 active workloads, 64 ranks per workload, and 128 devices per
workload. ResourceSlices split at 128 devices. Benchmark changes at those limits
before increasing them.

With the default 30-second inventory interval, each node produces at most about
three steady-state API writes per interval: ResourceSlice publication, node
topology publication, and node safety status. Approximate node-agent write load
as `3 * node_count / interval_seconds` QPS, before retries and claim activity.
For 256 nodes this is about 25.6 writes/s. The controller maintains four
list/watch streams, writes fabric status at most every half topology TTL, and
writes workload status and children only on reconciliation changes.

Each process defaults to client QPS 20 and burst 40. Do not raise these limits
to hide API-server throttling. First inspect apiserver request latency, 429s,
watch restarts, object count, and node inventory interval. Shard clusters rather
than increasing the declared solver limits. At sustained maximum workload and
endpoint counts, start controller sizing at 500m CPU and 512Mi requested with a
1Gi memory limit, then adjust from reconciliation latency, throttling, and
working-set evidence. Increase node resources only when discovery or reset
latency demonstrates pressure; hardware operation latency is normally not CPU
bound.
