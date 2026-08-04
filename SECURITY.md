# Security policy

## Supported versions

Only the newest released minor line receives security fixes. Before the first
stable release, only the latest `0.x` release is supported. Deploy released
image digests, retain the matching Helm chart, and subscribe to repository
security advisories.

## Reporting a vulnerability

Do not disclose suspected vulnerabilities in a public issue, discussion, log,
or pull request. Use the repository's private
[security advisory report](https://github.com/varrahan/tenstorrent-dra-framework/security/advisories/new)
and include:

- the affected release, image digest, Kubernetes/KMD/firmware versions, and
  hardware family;
- reproduction steps and the expected security boundary;
- whether device isolation, DMA/IOMMU, reset or memory sanitization, RBAC,
  workload admission, release signing, or credentials are affected;
- any evidence of exploitation and a safe way to contact the reporter.

Maintainers acknowledge credible reports within one business day and provide a
triage result within three business days. Target remediation is 7 days for a
critical issue, 30 days for high severity, and 90 days for moderate severity,
subject to coordinated-disclosure and hardware-vendor constraints. Reporters
receive status at least weekly until disclosure.

Repository administrators must keep GitHub private vulnerability reporting
enabled. If that form is unavailable, contact the repository owner privately
through GitHub before sending exploit details.

## Response process

1. Restrict the advisory to the response team, preserve evidence, and assign a
   CVSS severity and affected version range.
2. For a suspected isolation or sanitization failure, stop new accelerator
   allocation, cordon affected nodes, preserve claim/CDI/audit state, and
   follow `docs/OPERATIONS.md`. Never clear quarantine to restore capacity.
3. Revoke exposed credentials or signing authority immediately. GitHub OIDC
   release signing uses no stored private key; restrict or disable the release
   environment if its workflow authority is suspect.
4. Develop the fix privately, add a regression test, run all CI and applicable
   VM/physical certification gates, and obtain security review.
5. Publish a new patch release, image digest, SBOM, provenance, Cosign
   signature, advisory, and upgrade/rollback guidance. Immutable releases are
   never overwritten; affected packages are marked vulnerable or deprecated.
6. Coordinate CVE publication when appropriate and perform a post-incident
   review for critical or exploited issues.

Dependency alerts are triaged against reachable code and the final runtime
image. A false positive may be documented, but vulnerability scanner ignores
are not accepted without an expiry, owner, and linked security review.
