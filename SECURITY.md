# Security policy

## Supported versions

Only the latest release on the current major version receives security fixes.
Development snapshots are not supported deployment artifacts.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Report it privately
to the repository maintainers through the configured GitHub security advisory
channel, including affected version, reproduction steps, impact, and a safe
contact address. Remove secrets and tenant data from reports.

The maintainers will acknowledge receipt, assess severity, coordinate a fix,
and publish a security advisory after a release is available. Do not disclose
unresolved device-escape, CDI, privilege-escalation, or credential issues
publicly.

## Security expectations

Every release must pass dependency, image, Helm, license, and secret scans;
produce an SBOM and provenance; and use least-privilege RBAC, read-only root
filesystems, dropped capabilities, and restricted network access where
compatible with the selected backend.
