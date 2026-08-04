SHELL := /usr/bin/env bash

KIND_CLUSTER ?= tt-dra
KIND_CONFIG ?= kind/ttsim-dra.yaml

.PHONY: fake-hardware kind-smoke kind-clean vm-validate

fake-hardware:
	python3 ./ttsim_fake_hardware.py --root /tmp/tt-node-a --devices wormhole,blackhole
	python3 ./ttsim_fake_hardware.py --root /tmp/tt-node-b --devices wormhole
	docker run --rm --privileged -e HOST_UID=$$(id -u) -e HOST_GID=$$(id -g) -v /tmp/tt-node-a:/node-a -v /tmp/tt-node-b:/node-b alpine:3.20 sh -ec 'mkdir -p /node-a/dev/tenstorrent /node-b/dev/tenstorrent; mknod -m 660 /node-a/dev/tenstorrent/0 c 226 0; mknod -m 660 /node-a/dev/tenstorrent/1 c 226 1; mknod -m 660 /node-b/dev/tenstorrent/0 c 226 0; chown -R "$$HOST_UID:$$HOST_GID" /node-a /node-b'

kind-smoke:
	bash ./validate.sh $(KIND_CLUSTER) $(KIND_CONFIG)

kind-clean:
	kind delete cluster --name "$(KIND_CLUSTER)" || true

vm-validate: fake-hardware kind-smoke
