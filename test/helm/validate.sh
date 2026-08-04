#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
chart="$repo_root/deployments/helm/tenstorrent-dra"
rendered="$(mktemp)"
trap 'rm -f "$rendered"' EXIT

helm lint "$chart" --kube-version 1.34.0 \
  --set resetMode=noop \
  --set requireIOMMU=false \
  --set networkPolicy.enabled=true \
  --set metrics.serviceMonitor.enabled=true \
  --set alerts.enabled=true

helm template tt-dra "$chart" --namespace tenstorrent-system --kube-version 1.34.0 \
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
  'tenstorrent_dra_claim_operation_failures_total'; do
  grep -q "$expected" "$rendered"
done

if helm template unsafe "$chart" --kube-version 1.34.0 --set resetMode=noop >/dev/null 2>&1; then
  echo 'unsafe noop reset and IOMMU combination passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version 1.34.0 --set controllerReplicas=1 >/dev/null 2>&1; then
  echo 'single controller replica passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version 1.34.0 \
  --set interval=2m --set inventoryGracePeriod=60s >/dev/null 2>&1; then
  echo 'inventory grace period shorter than refresh interval passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version 1.34.0 --set priorityClass.create=false >/dev/null 2>&1; then
  echo 'missing external PriorityClass names passed values validation' >&2
  exit 1
fi
if helm template unsafe "$chart" --kube-version 1.34.0 \
  --set metrics.service.enabled=false \
  --set metrics.serviceMonitor.enabled=true >/dev/null 2>&1; then
  echo 'ServiceMonitor without metrics Service passed values validation' >&2
  exit 1
fi
