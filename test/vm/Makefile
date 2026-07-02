SHELL := /usr/bin/env bash

KIND_CLUSTER ?= agent-smoke
KIND_NODE_IMAGE ?= kindest/node:v1.34.0
KIND_WAIT ?= 120s
TT_DEVICE_PATH ?= /dev/tenstorrent
TT_KMD_MODULE ?= /home/ubuntu/tt-kmd/tenstorrent.ko
CLEANUP_KIND_CLUSTER ?= 1

.PHONY: help load-tt-kmd vm-validate kind-smoke kind-clean

help:
	@printf '%s\n' 'Validation targets:'
	@printf '  %-16s %s\n' 'load-tt-kmd' 'Load and verify the Tenstorrent kernel driver in the VM'
	@printf '  %-16s %s\n' 'vm-validate' 'Run host-in-VM foundation checks plus kind/DRA smoke validation'
	@printf '  %-16s %s\n' 'kind-smoke' 'Run only the kind/DRA/device visibility smoke validation'
	@printf '  %-16s %s\n' 'kind-clean' 'Delete the validation kind cluster'

load-tt-kmd:
	TT_DEVICE_PATH="$(TT_DEVICE_PATH)" \
	TT_KMD_MODULE="$(TT_KMD_MODULE)" \
	./scripts/load-tt-kmd.sh

vm-validate:
	KIND_CLUSTER="$(KIND_CLUSTER)" \
	KIND_NODE_IMAGE="$(KIND_NODE_IMAGE)" \
	KIND_WAIT="$(KIND_WAIT)" \
	TT_DEVICE_PATH="$(TT_DEVICE_PATH)" \
	TT_KMD_MODULE="$(TT_KMD_MODULE)" \
	CLEANUP_KIND_CLUSTER="$(CLEANUP_KIND_CLUSTER)" \
	./scripts/validate-foundation.sh

kind-smoke:
	KIND_CLUSTER="$(KIND_CLUSTER)" \
	KIND_NODE_IMAGE="$(KIND_NODE_IMAGE)" \
	KIND_WAIT="$(KIND_WAIT)" \
	TT_DEVICE_PATH="$(TT_DEVICE_PATH)" \
	TT_KMD_MODULE="$(TT_KMD_MODULE)" \
	CLEANUP_KIND_CLUSTER="$(CLEANUP_KIND_CLUSTER)" \
	./scripts/validate-foundation.sh --kind-only

kind-clean:
	kind delete cluster --name "$(KIND_CLUSTER)" || true
