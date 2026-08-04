# Runtime environment variables

This document is the complete environment-variable contract for the installed
`tt-dra-driver` program and the workload containers it creates. The supported
Helm chart supplies the driver variables automatically; operators normally do
not set them by hand. There is no `.env` file and application configuration is
otherwise expressed through command flags and Helm values.

Build metadata, CI runner state, credentials, Docker build arguments, and local
tool selection are not runtime configuration and are therefore outside this
contract.

## Driver process variables

| Variable | Commands | Requirement | Source and behavior |
|---|---|---|---|
| `NODE_NAME` | `node` | Required unless `-node-name` is supplied | The node DaemonSet obtains `spec.nodeName` through the Kubernetes Downward API. It identifies the node whose devices, topology, health, claims, CDI state, and Kubernetes Events the process manages. Startup fails with `node name is required` when both the variable and flag are empty. An explicit `-node-name` flag takes precedence. |
| `POD_NAME` | `controller` | Optional | The controller Deployment obtains `metadata.name` through the Downward API. The value is the leader-election identity and is included in logs and Kubernetes Events. When empty, the process uses its operating-system hostname. Each controller replica must have a distinct identity. |
| `POD_NAMESPACE` | `controller` | Optional | The controller Deployment obtains `metadata.namespace` through the Downward API. It initializes the namespace for the leader-election Lease, logs, and leader Events. When empty, it defaults to `default`; `-leader-election-namespace` takes precedence. The Helm chart always passes the release namespace explicitly through that flag. |
| `KUBERNETES_SERVICE_HOST` | `node`, `controller`, `cleanup` | Required by in-cluster client setup | Kubernetes injects the API Service host. `client-go` combines it with `KUBERNETES_SERVICE_PORT` to construct the API-server URL. The program intentionally supports only in-cluster Kubernetes authentication, so these commands fail before startup if either variable is absent. Do not hard-code or override it. |
| `KUBERNETES_SERVICE_PORT` | `node`, `controller`, `cleanup` | Required by in-cluster client setup | Kubernetes injects the API Service port. `client-go` combines it with `KUBERNETES_SERVICE_HOST`. Do not hard-code or override it. |

The `version` and `list` commands do not require Kubernetes or any environment
variables. In-cluster API authentication also requires the Pod's mounted
ServiceAccount token and cluster CA files, but those are files rather than
environment variables.

## Managed workload variables

The controller injects these variables into the container selected by a
`TenstorrentWorkload` object's `spec.containerName`:

| Variable | Value and purpose |
|---|---|
| `TT_RANK` | Zero-based integer rank of this Pod in the workload's planned rank order. Distributed applications use it to select rank-specific behavior. |
| `TT_WORLD_SIZE` | Total number of ranks in the `TenstorrentWorkload`. Every rank receives the same value. |

These two variables are controller-owned. A workload is rejected if its Pod
template sets either name, preventing user values from disagreeing with the
topology assignment.

## Helm wiring

The chart wires `NODE_NAME`, `POD_NAME`, and `POD_NAMESPACE` with Downward API
`fieldRef` entries. Kubernetes supplies its API Service variables. No Secret or
ConfigMap is used for runtime environment configuration.

To inspect the rendered wiring without installing it:

```bash
helm template tt-dra deployments/helm/tenstorrent-dra
```

Do not add environment-variable configuration for a value that is invariant.
Keep invariant policy in source, chart, or build constants; reserve environment
variables for per-Pod, per-node, per-workload, or per-cluster identity.

See [`RUN.md`](RUN.md) for deployment commands and
[`PRODUCTION.md`](PRODUCTION.md) for the security and compatibility boundary.
