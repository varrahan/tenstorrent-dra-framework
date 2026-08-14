#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
chart="$repo_root/deployments/helm/tenstorrent-dra"
kube_version="${KUBE_VERSION:-1.34.0}"
rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

helm lint "$chart" --kube-version "$kube_version" \
  --set resetMode=noop \
  --set requireIOMMU=false \
  --set networkPolicy.enabled=true \
  --set metrics.serviceMonitor.enabled=true \
  --set alerts.enabled=true

helm template tt-dra "$chart" --namespace tenstorrent-system --kube-version "$kube_version" \
  --set resetMode=noop \
  --set requireIOMMU=false \
  --set networkPolicy.enabled=true \
  --set metrics.serviceMonitor.enabled=true \
  --set alerts.enabled=true >"$rendered"

for expected in \
  'kind: PodDisruptionBudget' \
  'kind: PriorityClass' \
  'kind: ServiceMonitor' \
  'kind: PrometheusRule' \
  'kind: NetworkPolicy' \
  'path: /startupz' \
  'path: /readyz' \
  'path: /livez' \
  'fieldPath: metadata.uid' \
  'maxSurge: 1' \
  '-node-agent-ttl=2m' \
  'tenstorrent_dra_claim_operation_failures_total'; do
  grep -q "$expected" "$rendered"
done

if helm template unsafe "$chart" --kube-version "$kube_version" --set resetMode=noop >/dev/null 2>&1; then
  echo 'unsafe noop reset and IOMMU combination passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version "$kube_version" \
  --set controller.rollout.maxUnavailable=0 --set controller.rollout.maxSurge=0 >/dev/null 2>&1; then
  echo 'zero-availability rollout passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version "$kube_version" --set controllerReplicas=1 >/dev/null 2>&1; then
  echo 'single controller replica passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version "$kube_version" \
  --set interval=2m --set inventoryGracePeriod=60s >/dev/null 2>&1; then
  echo 'inventory grace period shorter than refresh interval passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version "$kube_version" --set priorityClass.create=false >/dev/null 2>&1; then
  echo 'missing external PriorityClass names passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version "$kube_version" \
  --set metrics.service.enabled=false \
  --set metrics.serviceMonitor.enabled=true >/dev/null 2>&1; then
  echo 'ServiceMonitor without metrics Service passed values validation' >&2
  exit 1
fi
