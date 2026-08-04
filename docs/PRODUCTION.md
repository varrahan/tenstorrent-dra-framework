# Production contract

This document defines the production boundary enforced by the driver. Anything
outside these limits fails closed and is not published as allocatable capacity.

## Supported hardware and software

| Surface | Supported production range |
| --- | --- |
| ASIC | Wormhole (`0x401e`) and Blackhole (`0xb140`) |
| `tt-kmd` | Stable 2.5.x through 2.10.x releases |
| `tt-kmd` ioctl ABI | 2 |
| Firmware | 19.2.x |
| Linux kernel | 5.4 through 6.18 |
| Kubernetes | 1.34 or newer, DRA `resource.k8s.io/v1` |

The public `GET_DRIVER_INFO` ioctl supplies the KMD and ABI versions when the
device supports it. The simulator may supply `kmd_version` and
`driver_abi_version` sysfs fixtures instead. Firmware, explicit health, and the
hardware UUID must be readable from the configured `tt-kmd` sysfs surface.
Missing, malformed, unknown, or unsupported values make only that device
ineligible.

The resetter requests a nonblocking exclusive device open, so a device already
in use fails closed instead of stalling lifecycle reconciliation. It completes
reset on one file descriptor when supported and reopens only when an older KMD
invalidates the reset-issuing descriptor before `POST_RESET`. Pre-release and
testing KMD builds are rejected.

`device_uuid` is the production identity. `serial_number` is accepted as an
equivalent platform-provided UUID. PCI BDF remains diagnostic metadata and does
not name a DRA device, so PCI renumbering does not change claim identity. A UUID
change on an existing device path is a hot replacement: the old identity stays
quarantined and the new identity must pass reset, scrub, and a fresh healthy
observation before publication.

## Inventory freshness

Every published device has an explicit `Healthy` observation. `Unknown` is
never eligible. The node agent refreshes every 30 seconds by default and may
reuse the last healthy observation for at most the configured 60-second
`inventoryGracePeriod`. Once that bounded grace expires, it publishes an empty
pool and fences all known capacity. Agent restart has no in-memory grace and
therefore immediately requires a new observation. DeviceClass selectors repeat
the `Healthy` requirement, and prepare applies the same eligibility policy.

## Claim-state recovery

State schema version 3 records each claim as `Preparing`, `Prepared`,
`Releasing`, or `Recovered`. Prepare persists exclusive ownership before reset
or CDI creation and commits `Prepared` only after reset, identity verification,
CDI write, and audit success. Unprepare persists `Releasing` before post-use
reset and removes ownership only after scrub, audit, and CDI deletion succeed.
State and CDI files use atomic rename and directory sync; audit records use a
synced append-only log.

`agent.lock` uses an exclusive host `flock`, so only one node agent can mutate a
state/CDI directory. At startup the agent reconciles current inventory, live
local ResourceClaim allocations, persisted claims, and driver-owned CDI files:

- missing CDI for a valid prepared claim is regenerated exactly;
- interrupted, missing, changed, or unknown ownership is retained and
  quarantined;
- an allocation with no state becomes `Recovered` and cannot be newly prepared;
- orphaned driver CDI files are removed after allocation reconciliation;
- corrupt state is preserved as `claims.json.corrupt-<timestamp>`, all visible
  devices are quarantined, and recovery requires sanitization;
- version 1 and 2 state migrates in place to version 3; unknown future versions
  use the corrupt-state recovery path.

These rules make node-agent and kubelet retries idempotent. A restart can leave
a device safely owned or quarantined, never unowned while CDI access is being
returned. Operators recover a `Recovered` or interrupted release through the
normal kubelet unprepare path; they must not edit `claims.json` or CDI files.

## Authorization and workload threat model

The node agent and controller use separate ServiceAccounts and ClusterRoles.
The node role cannot create Pods, workloads, or claims. It reads claims for
startup reconciliation, owns ResourceSlices and node topology, and only uses
the configured node name in node API calls. Kubernetes RBAC cannot express a
dynamic “this DaemonSet Pod's node name” resource restriction, so code-level
node-name binding is the additional boundary.

The controller must watch workloads and create children across namespaces, so
those permissions are cluster-scoped. A fail-closed ValidatingAdmissionPolicy
requires the requesting principal to already have `create` permission on Pods
in the workload namespace. A TenstorrentWorkload therefore cannot grant Pod or
ServiceAccount authority the requester did not already have.

The CRD and controller jointly enforce unique DNS-safe ranks, supported
DeviceClasses, 1–64 ranks, 1–128 devices per rank, 128 total devices, and at
most 16 containers. The target container must exist. Templates cannot select a
ServiceAccount, mount a token, use host namespaces or hostPath, set node
placement or DRA claims, inject controller rank variables, use ephemeral
containers, or request privilege. Child names contain a workload-UID hash and
fit the DNS label limit. Existing child names are accepted only when controller
owner UID and desired spec match exactly.

Controller-created Pods use `restartPolicy: Never`, no service-account token,
RuntimeDefault seccomp and AppArmor, non-root execution, no privilege
escalation, a read-only root filesystem, and all capabilities dropped. Workload
images must write through mounted writable volumes when needed.

The runtime environment surface is deliberately narrow: node, Pod, namespace,
and Kubernetes API Service identity are injected by Kubernetes, while rank
identity is controller-generated for managed workloads. The authoritative list
and override rules are in [`ENV.md`](ENV.md).

Every image process uses UID and GID 65532. Controller and cleanup containers
run without privilege escalation or Linux capabilities. The privileged,
non-root node DaemonSet is the sole Pod Security exception; privileged mode
still grants the capabilities required to cross root-owned host-path and
device permission boundaries. It needs
read/write access to `/dev/tenstorrent`, read-only access to the configured
Tenstorrent class, PCI, and backing device sysfs roots, and write access to the
state, CDI, kubelet plugin, and registrar directories. No whole `/sys` mount is
used. Cluster operators must label the node-agent namespace for privileged Pod
Security and apply local SELinux/AppArmor rules only to those eight paths. Workload Pods need
no device hostPath, privileged, SELinux, or AppArmor exception because CDI and
the container runtime inject only allocated device nodes.

## Controller lifecycle and limits

Dynamic and typed informers feed a per-object exponentially rate-limited queue;
API conflicts are retried. Fabric expiry is evaluated from the informer cache.
Two controller replicas use a Lease, with only the leader reconciling.
Per-workload failures do not block other keys.

Workload phases are `Pending`, `Assigned`, `Running`, `Degraded`, `Failed`, and
`Succeeded`. Status includes `observedGeneration` and preserves condition
transition time. Spec becomes immutable after assignment. Unstarted assignments
are replanned after relevant fabric changes, unschedulable Pods, or competing
ordinary claims. Started assignments are frozen and degraded. Failed and
succeeded workloads delete children and release reservations; deletion relies
on owner-reference garbage collection.

Placement is canceled with its request context, limited to two seconds by
default, and bounded to 256 nodes, 256 fabrics, 2,048 endpoints, 1,000 active
workloads, 64 ranks, and 128 devices per workload. Fabric generations hash only
the selected or requested fabric and ring, so unrelated fabrics do not disturb
a workload. Boundary tests and a 64-rank benchmark enforce these declared
limits.

## API compatibility and upgrades

The production APIs are `topology.tenstorrent.com/v1` and
`scheduling.tenstorrent.com/v1`. Within a chart major version, existing fields
retain meaning and new optional fields may be added. Removing or changing a
field requires a new API version, a conversion webhook, both versions served
during migration, and a storage-version migration before the old version is
disabled.

The identical pre-release `v1alpha1` schema remains served with Kubernetes'
schema-compatible `None` conversion while `v1` is the only storage version.
After applying the CRD, rewrite existing alpha objects through `v1`, verify the
CRD `status.storedVersions` contains only `v1`, and only then remove alpha from a
future chart. No field-changing conversion is attempted without a webhook.

Helm does not upgrade files from `crds/`. Upgrade CRDs first, wait for them to be
established, then upgrade the release:

```bash
kubectl apply --server-side -f deployments/helm/tenstorrent-dra/crds/topology.yaml
kubectl wait --for=condition=Established \
  crd/tenstorrentworkloads.scheduling.tenstorrent.com --timeout=120s
helm upgrade --install tt-dra deployments/helm/tenstorrent-dra
```

Before any future storage version is introduced, back up all three custom
resources, deploy conversion support, verify every object reads through both
versions, migrate stored objects, and inspect `status.storedVersions`. Rollback
must keep the conversion service and old served version until no object remains
stored in the new version.

See [`examples/standard-claim.yaml`](../examples/standard-claim.yaml) and
[`examples/topology-workload.yaml`](../examples/topology-workload.yaml) for the
supported request paths.
