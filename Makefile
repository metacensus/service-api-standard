.PHONY: help build run test test-race test-adapter test-artifact test-integration \
        fmt fmt-check vet tidy-check golangci vuln lint check hooks docker-build \
        release release-major release-minor release-patch latest

.DEFAULT_GOAL := help

IMAGE     ?= metacensus/service-api-standard
IMAGE_TAG ?= dev

# Read out of go.mod so no copy can drift from what CI's setup-go uses.
GOTOOLCHAIN_PIN ?= $(shell awk '/^toolchain /{t=$$2} /^go /{if (g == "") g = "go" $$2} END{print (t != "" ? t : g)}' go.mod)
ifeq ($(GOTOOLCHAIN_PIN),)
$(error could not read the Go toolchain from go.mod; refusing to run unpinned)
endif
# A go.work above the checkout would lift the pins; ignore it.
export GOTOOLCHAIN := $(GOTOOLCHAIN_PIN)
export GOWORK := off

# Tools run at a pinned version rather than whatever is installed: a linter
# built with an older Go refuses a newer module, and CI must agree with a laptop.
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK   := golang.org/x/vuln/cmd/govulncheck@v1.8.0

help:
	@awk -F' — ' '/^## /{ sub(/^## /, ""); printf "  make %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

## build — compile the service to ./bin/service
build:
	go build -trimpath -o bin/service ./cmd/service

## run — run the service (needs DATABASE_URL)
run:
	go run ./cmd/service

## test — unit tests
test:
	go test ./...

## test-race — unit tests under the race detector
test-race:
	go test -race -count=1 ./...

## test-adapter — the Postgres adapter suite against a container (needs Docker)
test-adapter:
	go test -tags=integration -race -count=1 -timeout 15m ./...

## test-artifact — the built image against Postgres (needs Docker; SERVICE_IMAGE skips the build)
test-artifact:
	@image='$(SERVICE_IMAGE)'; \
	if [ -z "$$image" ]; then \
		$(MAKE) --no-print-directory docker-build; \
		image='$(IMAGE):$(IMAGE_TAG)'; \
	fi; \
	SERVICE_IMAGE="$$image" go test -tags=artifact -count=1 -timeout 15m ./integration/...

## test-integration — the adapter suite, then the artifact suite
test-integration: test-adapter test-artifact

## fmt — rewrite with gofmt
fmt:
	gofmt -w .

## fmt-check — fail if gofmt would rewrite anything
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt would rewrite:"; echo "$$unformatted"; exit 1; \
	fi

## vet — go vet, integration- and artifact-tagged code included
vet:
	go vet ./...
	go vet -tags=integration ./...
	go vet -tags=artifact ./...

## tidy-check — fail if go mod tidy would change go.mod or go.sum
tidy-check:
	go mod tidy -diff

## golangci — golangci-lint, from .golangci.yml
golangci:
	go run $(GOLANGCI_LINT) run ./...

## lint — formatting, vet, module freshness and golangci-lint
lint: fmt-check vet tidy-check golangci

## vuln — govulncheck over the service's dependencies
vuln:
	go run $(GOVULNCHECK) ./...

## check — lint, race tests and both integration suites
check: lint test-race test-integration

## hooks — opt in to the pre-commit hook; unset core.hooksPath to opt out
hooks:
	git config core.hooksPath scripts/hooks
	@echo "core.hooksPath set to scripts/hooks"

## docker-build — build the image for this machine's arch
docker-build:
	docker build -t $(IMAGE):$(IMAGE_TAG) .

VERSION ?=
TYPE    ?= patch

## release — tag and push the next version (TYPE=patch|minor|major, or VERSION=1.2.3)
release: scripts/version.sh
	@set -e; \
	VERSION=$$(./scripts/version.sh "$(VERSION)" "$(TYPE)"); \
	if [ -z "$$VERSION" ]; then \
		echo "Error: version.sh produced no version; refusing to tag"; \
		exit 1; \
	fi; \
	TAG="v$$VERSION"; \
	MSG="Release $$VERSION"; \
	if git rev-parse "$$TAG" >/dev/null 2>&1; then \
		echo "Error: Tag $$TAG already exists"; \
		exit 1; \
	fi; \
	if git ls-remote --exit-code --tags origin "refs/tags/$$TAG" >/dev/null 2>&1; then \
		echo "Error: Tag $$TAG already exists on origin"; \
		exit 1; \
	fi; \
	git tag -a "$$TAG" -m "$$MSG" && \
	git push origin "$$TAG" && \
	echo "Released: $$TAG"

## release-major — release, bumping the major
release-major:
	@$(MAKE) release TYPE=major

## release-minor — release, bumping the minor
release-minor:
	@$(MAKE) release TYPE=minor

## release-patch — release, bumping the patch
release-patch:
	@$(MAKE) release TYPE=patch

## latest — print the most recent version tag
latest:
	@bash -c '. scripts/version.sh && get_latest_version'
