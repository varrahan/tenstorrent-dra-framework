#!/usr/bin/env bash
set -euo pipefail

cluster="${1:?cluster name required}"
config="${2:?kind config required}"
kind create cluster --name "$cluster" --config "$config"
kubectl label node "${cluster}-worker" tenstorrent.com/enabled=true --overwrite
kubectl label node "${cluster}-worker2" tenstorrent.com/enabled=true --overwrite
helm upgrade --install tt-dra deployments/helm/tenstorrent-dra \
  --set nodeSelector.tenstorrent.com/enabled=true \
  --set sysfsRoot=/tt-sys/class/tenstorrent \
  --set pciSysfsRoot=/tt-sys/bus/pci/devices
kubectl wait --for=condition=Established crd/tenstorrentworkloads.scheduling.tenstorrent.com --timeout=120s
kubectl get resourceslices
kubectl get tenstorrentnodetopologies
