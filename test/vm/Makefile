SHELL := /usr/bin/env bash

KIND_CLUSTER ?= agent-smoke
KIND_NODE_IMAGE ?= kindest/node:v1.34.0
KIND_WAIT ?= 120s
TT_DEVICE_PATH ?= /dev/tenstorrent
TT_KMD_MODULE ?= /home/ubuntu/tt-kmd/tenstorrent.ko
CLEANUP_KIND_CLUSTER ?= 1
FAKE_SYSFS_ROOT ?= /tmp/tt-sim-sysfs
FAKE_STATE_ROOT ?= /tmp/tt-sim-state
FAKE_DEVICE_COUNT ?= 32
FAKE_INTERVAL ?= 1
FAKE_ITERATIONS ?= 0
FAKE_FIXTURE ?= normal

.PHONY: help load-tt-kmd vm-validate kind-smoke kind-clean fake-hardware sync-test-vm

help:
	@printf '%s\n' 'Validation targets:'
	@printf '  %-16s %s\n' 'load-tt-kmd' 'Load and verify the Tenstorrent kernel driver in the VM'
	@printf '  %-16s %s\n' 'vm-validate' 'Run host-in-VM foundation checks plus kind/DRA smoke validation'
	@printf '  %-16s %s\n' 'kind-smoke' 'Run only the kind/DRA/device visibility smoke validation'
	@printf '  %-16s %s\n' 'kind-clean' 'Delete the validation kind cluster'
	@printf '  %-16s %s\n' 'fake-hardware' 'Generate synthetic sysfs and workload state (runs until interrupted)'
	@printf '  %-16s %s\n' 'sync-test-vm' 'Copy test/vm into the running QEMU guest repository'

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

fake-hardware:
	python3 ./ttsim_fake_hardware.py \
		--sysfs-root "$(FAKE_SYSFS_ROOT)" \
		--state-root "$(FAKE_STATE_ROOT)" \
		--device-count "$(FAKE_DEVICE_COUNT)" \
		--fixture "$(FAKE_FIXTURE)" \
		--interval "$(FAKE_INTERVAL)" \
		--iterations "$(FAKE_ITERATIONS)" \
		--simulate-workloads

sync-test-vm:
	./scripts/sync-test-vm.sh
