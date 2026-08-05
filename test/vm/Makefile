SHELL := /usr/bin/env bash

KIND_CLUSTER := tt-dra
KIND_CONFIG := kind/ttsim-dra.yaml
HOST_UID := $(shell id -u)
HOST_GID := $(shell id -g)

CHAOS_EVIDENCE_DIR ?=
CERTIFICATION_EVIDENCE_DIR ?=
EXPECTED_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

.PHONY: fake-hardware kind-smoke kind-chaos kind-clean vm-validate vm-chaos vm-certification

fake-hardware:
	python3 ./ttsim_fake_hardware.py --root /tmp/tt-node-a --devices wormhole,blackhole
	python3 ./ttsim_fake_hardware.py --root /tmp/tt-node-b --devices wormhole
	docker run --rm --privileged -v /tmp/tt-node-a:/node-a -v /tmp/tt-node-b:/node-b alpine:3.20 sh -ec 'mkdir -p /node-a/dev/tenstorrent /node-b/dev/tenstorrent; mknod -m 660 /node-a/dev/tenstorrent/0 c 226 0; mknod -m 660 /node-a/dev/tenstorrent/1 c 226 1; mknod -m 660 /node-b/dev/tenstorrent/0 c 226 0; chown -R "$(HOST_UID):$(HOST_GID)" /node-a /node-b'

kind-smoke:
	bash ./validate.sh $(KIND_CLUSTER) $(KIND_CONFIG)

kind-chaos:
	bash ./chaos.sh $(KIND_CLUSTER)-chaos $(KIND_CONFIG) "$(CHAOS_EVIDENCE_DIR)"

kind-clean:
	kind delete cluster --name "$(KIND_CLUSTER)" || true

vm-validate: fake-hardware kind-smoke

vm-chaos: fake-hardware kind-chaos

vm-certification:
	bash ./certify.sh "$(EXPECTED_COMMIT)" "$(CERTIFICATION_EVIDENCE_DIR)"
