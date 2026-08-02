SHELL := /usr/bin/env bash

GO ?= go
GOFMT ?= gofmt
PYTHON ?= python3
SHELLCHECK ?= shellcheck
KIND ?= kind
HELM ?= helm
DOCKER ?= docker

GO_FILES := $(shell find src -type f -name '*.go' -print)
SHELL_FILES := $(shell find test/vm -type f -name '*.sh' -print)

.PHONY: build test test-race vet fmt-check lint generate generate-check \
	python-check shell-check image-build helm-lint kind-integration \
	vm-validation launcher-test check ci

build:
	$(GO) build ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt-check:
	@test -z "$$($(GOFMT) -l $(GO_FILES))"

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --config .golangci.yml; \
	else \
		echo 'golangci-lint not installed; go vet is the local lint fallback'; \
	fi

generate:
	$(GO) generate ./src

generate-check: generate
	@git diff --exit-code -- src/manifests

python-check:
	$(PYTHON) -m compileall -q test/vm

shell-check:
	@for file in $(SHELL_FILES); do bash -n "$$file"; done
	@if command -v $(SHELLCHECK) >/dev/null 2>&1; then \
		$(SHELLCHECK) $(SHELL_FILES); \
	else \
		echo 'shellcheck not installed; bash -n is the local shell-check fallback'; \
	fi

image-build:
	@test -f Dockerfile || { echo 'image build is not available until the Stage 7 Dockerfile exists' >&2; exit 2; }
	$(DOCKER) build --tag tenstorrent-dra:dev .

helm-lint:
	@test -f deployments/helm/tenstorrent-dra/Chart.yaml || { echo 'Helm chart is missing' >&2; exit 2; }
	$(HELM) lint deployments/helm/tenstorrent-dra

kind-integration:
	$(MAKE) -C test/vm kind-smoke

vm-validation:
	$(MAKE) -C test/vm vm-validate

launcher-test:
	$(MAKE) -C test/vm launcher-test

check: fmt-check test test-race lint generate-check python-check shell-check launcher-test

ci: check
