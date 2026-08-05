# Release acceptance controls

This document defines the approvals for one immutable release candidate. It is
an approval contract, not an approval record. A reviewer must
not approve a candidate whose commit, image digest, or certification evidence
differs from the candidate named in the release.

## Automated QEMU gate

The `QEMU VM certification` job runs on a dedicated self-hosted runner labeled
`tt-qemu-vm`. The runner must be an ephemeral, disposable instance of the
supported QEMU `ttsim` Ubuntu VM with Docker, kind, Kubernetes 1.34 or newer,
Helm 4.2.3, Go 1.25.12, and no production credentials. Pull requests from
forks do not execute repository code on this privileged runner; maintainers
must move an accepted fork change to a trusted branch before certification.

`test/vm/certify.sh` rejects a dirty checkout, a commit mismatch, and any
virtualization other than QEMU/KVM. It runs both `vm-validate` and `vm-chaos`,
checks the kubelet registration and DRA sockets, records the candidate source
tree and local image digest, retains chaos safety snapshots and kind logs, and
produces a SHA-256 evidence manifest. The tag workflow cannot enter an
approval environment or publish until this exact-commit job passes.

Repository administrators must make `QEMU VM certification` a required check
on protected trusted branches. If the runner is offline, certification stays
queued and release publication remains blocked.

## Security review

The implemented technical controls cover the required review surfaces:

- node and controller identities and RBAC are separated; the node cannot
  create workloads, Pods, or claims, and controller child creation is bounded
  by fail-closed admission and exact owner/spec checks;
- the privileged node boundary mounts only the device, selected sysfs, state,
  CDI, kubelet-plugin, and registrar paths; controller, cleanup, and generated
  workload containers use the restricted non-root baseline;
- prepare and release persist ownership around reset/scrub and CDI transitions,
  recover interrupted state, and quarantine uncertainty;
- actions, base images, toolchains, versions, release artifacts, SBOMs,
  attestations, and keyless signatures are pinned or immutable and checked by
  CI and the release workflow.

The repository's automated policy and synthetic fault evidence are engineering
inputs, not independent security approval. Before release, configure the
GitHub environment `security-release-approval` with required security
reviewers, prevent self-review, restrict deployment to protected release tags,
and set its environment variable `RELEASE_APPROVAL_REQUIRED=enabled`. The
workflow fails closed when that variable is absent. The reviewer must inspect
the RBAC, admission, privileged host/CDI boundary, supply-chain results,
synthetic certification evidence, and applicable physical isolation evidence.

## Operations acceptance

The chart supplies health endpoints, metrics, dashboards, alerts, SLOs,
capacity limits, upgrade and rollback procedures, and incident, quarantine,
state-recovery, and node-replacement runbooks. Accountable ownership is still
an organizational decision and must not be inferred from the existence of
those assets.

Before release, configure the GitHub environment
`operations-release-approval` with required operations/on-call reviewers,
prevent self-review, restrict deployment to protected release tags, and set
its environment variable `RELEASE_APPROVAL_REQUIRED=enabled`. The workflow
fails closed when that variable is absent. Approval confirms that dashboard
and alert destinations are live, paging routes have named responders and
escalation, the declared SLO queries are retained, and the upgrade, rollback,
quarantine, and recovery procedures have operational owners.

## Physical certification approval

Physical execution is separately enforced by the protected
`hardware-release-approval` environment. Configure it with required lab/release
reviewers, self-review prevention, protected-tag restrictions, and
`RELEASE_APPROVAL_REQUIRED=enabled`. Its approver must link complete evidence
for every supported matrix entry; synthetic results cannot satisfy this gate.

## Candidate approval record

Retain these fields with the release approval and the VM and physical evidence:

| Field | Required value |
| --- | --- |
| Version and tag | Exact `vMAJOR.MINOR.PATCH` |
| Candidate | Full 40-character commit and source-tree hash |
| Image | Immutable manifest digest |
| QEMU evidence | Workflow run and `vm-certification-<commit>` artifact |
| Physical evidence | Every supported matrix entry and approver decision |
| Security approval | Reviewer/team, UTC timestamp, decision, linked findings |
| Operations approval | Owner/team, UTC timestamp, paging route, decision |
| Exceptions | None, or an owner, expiry, risk statement, and release rejection |

Missing or mismatched fields reject the release. Approval never waives reset,
sanitization, ownership, CDI isolation, artifact immutability, or physical
certification requirements.
