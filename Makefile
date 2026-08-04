SHELL := /usr/bin/env bash

GO ?= go
GOFMT ?= gofmt
HELM ?= helm
DOCKER ?= docker

GO_FILES := $(shell find src -type f -name '*.go' -print)

.PHONY: build test fmt-check image-build helm-lint vm-validation check

build:
	$(GO) build ./...

test:
	$(GO) test ./...

fmt-check:
	@test -z "$$($(GOFMT) -l $(GO_FILES))"

image-build:
	$(DOCKER) build --tag tenstorrent-dra:dev .

helm-lint:
	$(HELM) lint deployments/helm/tenstorrent-dra
	bash test/helm/validate.sh

vm-validation:
	$(MAKE) -C test/vm vm-validate

check: fmt-check build test helm-lint
