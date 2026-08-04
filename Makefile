SHELL := /usr/bin/env bash

GO ?= go
GOFMT ?= gofmt
HELM ?= helm
DOCKER ?= docker

BINARY := tt-dra-driver
COMMAND := ./src/cmd/tt-dra-driver
override IMAGE_REPOSITORY := tenstorrent-dra
override CGO_MODE := 0
override RELEASE_GOOS := linux
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || echo 0)
BUILD_DATE ?= $(shell date -u --date="@$(SOURCE_DATE_EPOCH)" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo 1970-01-01T00:00:00Z)

STATICCHECK_VERSION := v0.6.1
GOVULNCHECK_VERSION := v1.6.0
LICHEN_VERSION := v0.3.0
GITLEAKS_VERSION := v8.30.1
ACTIONLINT_VERSION := v1.7.11
override GO_TOOLCHAIN := go1.25.12
SHELLCHECK_IMAGE := docker.io/koalaman/shellcheck-alpine@sha256:9955be09ea7f0dbf7ae942ac1f2094355bb30d96fffba0ec09f5432207544002

GO_FILES := $(shell find src -type f -name '*.go' -print)
LDFLAGS := -s -w -buildid= -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)
BUILD_FLAGS := -mod=readonly -trimpath -buildvcs=false -ldflags="$(LDFLAGS)"

.PHONY: build test race coverage coverage-check fmt-check vet staticcheck govulncheck \
	license-check secret-scan actionlint shellcheck security image-build image-check \
	supply-chain-check helm-lint helm-package release-binaries release-reproducibility \
	release-checksums release vm-validation check ci clean

build:
	$(GO) build $(BUILD_FLAGS) ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

coverage:
	mkdir -p coverage
	$(GO) test -race -covermode=atomic -coverpkg=./src/... -coverprofile=coverage/coverage.out ./src/...

coverage-check: coverage
	bash test/coverage/check.sh coverage/coverage.out

fmt-check:
	@test -z "$$($(GOFMT) -l $(GO_FILES))"

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

govulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

license-check:
	@temporary_binary="$$(mktemp)"; \
		trap 'rm -f -- "$$temporary_binary"' EXIT; \
		CGO_ENABLED=$(CGO_MODE) $(GO) build $(BUILD_FLAGS) -o "$$temporary_binary" $(COMMAND); \
		GOTOOLCHAIN=$(GO_TOOLCHAIN) $(GO) run github.com/selesy/lichen@$(LICHEN_VERSION) --config=.lichen.yaml "$$temporary_binary"

secret-scan:
	$(GO) run github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) git --redact --no-banner .

actionlint:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

shellcheck:
	$(DOCKER) run --rm --volume "$(CURDIR):/mnt:ro" --workdir /mnt $(SHELLCHECK_IMAGE) \
		shellcheck test/coverage/check.sh test/helm/validate.sh test/supply-chain/validate.sh test/vm/validate.sh

supply-chain-check:
	bash test/supply-chain/validate.sh

security: staticcheck govulncheck license-check secret-scan

image-build:
	$(DOCKER) build --provenance=false --tag $(IMAGE_REPOSITORY):dev \
		--build-arg VERSION=$(VERSION) \
		--build-arg VCS_REF=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--build-arg SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) .

image-check: image-build
	test "$$($(DOCKER) image inspect $(IMAGE_REPOSITORY):dev --format '{{.Config.User}}')" = "65532:65532"
	$(DOCKER) run --rm $(IMAGE_REPOSITORY):dev version

helm-lint:
	$(HELM) lint deployments/helm/tenstorrent-dra
	bash test/helm/validate.sh

helm-package:
	mkdir -p dist
	$(HELM) package deployments/helm/tenstorrent-dra --destination dist

release-binaries:
	mkdir -p dist
	CGO_ENABLED=$(CGO_MODE) GOOS=$(RELEASE_GOOS) GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -o dist/$(BINARY)-linux-amd64 $(COMMAND)
	CGO_ENABLED=$(CGO_MODE) GOOS=$(RELEASE_GOOS) GOARCH=arm64 $(GO) build $(BUILD_FLAGS) -o dist/$(BINARY)-linux-arm64 $(COMMAND)

release-reproducibility:
	@temporary_directory="$$(mktemp -d)"; \
		trap 'rm -rf -- "$$temporary_directory"' EXIT; \
		for architecture in amd64 arm64; do \
			for attempt in first second; do \
				CGO_ENABLED=$(CGO_MODE) GOOS=$(RELEASE_GOOS) GOARCH="$$architecture" $(GO) build $(BUILD_FLAGS) \
					-o "$$temporary_directory/$$architecture-$$attempt" $(COMMAND); \
			done; \
		cmp "$$temporary_directory/$$architecture-first" "$$temporary_directory/$$architecture-second"; \
		done

release-checksums:
	cd dist && sha256sum $(BINARY)-linux-amd64 $(BINARY)-linux-arm64 tenstorrent-dra-$(VERSION).tgz $(notdir $(wildcard dist/*.spdx.json)) > checksums.txt

release: clean release-binaries helm-package release-checksums

vm-validation:
	$(MAKE) -C test/vm vm-validate

check: fmt-check vet build test helm-lint

ci: fmt-check vet staticcheck coverage-check govulncheck license-check secret-scan actionlint shellcheck supply-chain-check helm-lint

clean:
	rm -rf -- dist coverage
