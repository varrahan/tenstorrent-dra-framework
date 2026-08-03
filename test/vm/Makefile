SHELL := /usr/bin/env bash

KIND_CLUSTER ?= tt-dra
KIND_CONFIG ?= kind/ttsim-dra.yaml

.PHONY: fake-hardware kind-smoke kind-clean vm-validate

fake-hardware:
	python3 ./ttsim_fake_hardware.py --root /tmp/tt-node-a --cards wormhole:n300,blackhole:p150 --create-device-nodes
	python3 ./ttsim_fake_hardware.py --root /tmp/tt-node-b --cards wormhole:n150 --create-device-nodes

kind-smoke:
	bash ./validate.sh $(KIND_CLUSTER) $(KIND_CONFIG)

kind-clean:
	kind delete cluster --name "$(KIND_CLUSTER)" || true

vm-validate: fake-hardware kind-smoke
