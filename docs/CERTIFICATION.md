# Failure, chaos, and physical certification

Certification is evidence for one immutable candidate commit and image digest.
Synthetic results never certify reset, DMA/IOMMU, memory-remanence, real fabric,
or physical fault behavior.

## Synthetic failure and chaos gate

Run this inside the supported `ttsim` Ubuntu VM after the normal end-to-end
suite:

```bash
make -C test/vm vm-validate
make -C test/vm vm-chaos
```

The complete exact-commit gate used by CI and release automation is:

```bash
make -C test/vm vm-certification \
  EXPECTED_COMMIT="$(git rev-parse HEAD)" \
  CERTIFICATION_EVIDENCE_DIR="$PWD/artifacts/vm-certification"
```

This combined entry point requires a clean QEMU/KVM checkout, exercises both
suites, verifies the kubelet registration and DRA sockets, records the source
tree and candidate image digest, exports kind logs, and writes checksummed
evidence. `.github/workflows/release.yml` runs it before any approval or
publication job. The required isolated runner and protected-environment setup
are defined in [`ACCEPTANCE.md`](ACCEPTANCE.md).

`vm-chaos` uses short synthetic observation intervals and the explicit
validation-only `resetMode=noop`, `requireIOMMU=false`, and AppArmor-disable
settings. The baseline `vm-validate` gate keeps the production workload
AppArmor policy enabled. The chaos gate injects inventory loss, stale health,
hot-unplug, unknown health, OOM, hang, and fabric-link loss; restarts the
controller leader, node agent, kubelet, and kind node; pauses the API server;
injects orphaned persisted state; competes a standard claim with a topology
workload; delays garbage collection; and deletes Pods, workloads, and a
namespace. Every interruption captures node claim/CDI state and rejects double
ownership or orphaned CDI access. Evidence is written under `artifacts/` and
the disposable chaos cluster is removed on success or failure.

Reset and scrub failure are injected at the lifecycle boundary by the Go race
suite because synthetic character devices cannot implement `tt-kmd` ioctls:

```bash
go test -race ./src/test -run 'Test(Janitor|Lifecycle|Prepare)'
```

## Physical matrix

[`test/hardware/matrix.json`](../test/hardware/matrix.json) defines the release
matrix. It covers Wormhole and Blackhole, single- and multi-ASIC nodes, the KMD
and kernel compatibility boundaries, a mixed whole-card node, and a real
multi-node Ethernet ring. Firmware is fixed to 19.2.x, the driver ABI to 2,
Kubernetes to 1.34 or newer, Helm to 4.2.3, and the runtime to containerd 2.x
with CDI enabled. Board product names are evidence metadata rather than device
identity; each ASIC remains one whole-card device.

Run the fail-closed preflight on every physical matrix entry from the
corresponding accelerator node:

```bash
bash test/hardware/certify.sh preflight <matrix-entry> <evidence-directory>
```

Preflight rejects WSL/simulators, missing character devices or sysfs, an
incorrect Helm/Kubernetes version, ineligible hardware, shared/missing IOMMU
groups, wrong KMD/firmware/kernel/ABI values, down links, incorrect device
counts, and too few enabled nodes. It does not by itself award certification.

## Required physical execution record

For each matrix entry, preserve the preflight directory together with the
candidate commit and image digest, board and ASIC identifiers, containerd and
Kubernetes versions, rendered Helm values, ResourceSlices and topology, Events,
driver logs, metrics, `claims.json`, CDI specs, and `audit.jsonl`. Run the chart
with its production defaults: `resetMode=ioctl` and `requireIOMMU=true`.

The signed release record must show all of these passing:

1. Discovery and exact inventory; standard and topology-aware allocation;
   per-container device visibility; successful preflight and postflight reset;
   cleanup and immediate safe reuse.
2. Controller leader loss during assignment and cleanup; API interruption and
   conflict retry; node-agent and kubelet restarts before, during, and after
   prepare/unprepare; and node reboots with allocated, prepared, releasing, and
   deliberately orphaned claims.
3. Authorized platform fault injection for reset failure, post-use scrub
   failure, health loss, hot-unplug, OOM, hang, and every physical fabric link.
   A failure must retain ownership or quarantine, withdraw new capacity, and
   never leave unowned CDI access.
4. Forced Pod, workload, and namespace deletion with delayed garbage
   collection; competing native and topology requests; saturation across every
   device; and at least 1,000 repeated allocate/use/release cycles.
5. A minimum 24-hour multi-node saturation soak. Export the SLO queries from
   `OPERATIONS.md`; sanitization must be 100%, with no double allocation,
   leaked CDI file, cross-tenant marker, stale publication beyond 90 seconds,
   or unexplained topology generation change.

Node reboot and hardware fault injection are intentionally not initiated by a
generic repository script: those operations require the lab owner's explicit
maintenance authority and platform-specific controls. The release approver
must reject evidence with missing phases, synthetic/noop settings, mutable
images, edited state/CDI files outside a declared orphan-injection case, or
failed SLOs.
