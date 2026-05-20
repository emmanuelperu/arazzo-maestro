# Convenience wrapper around go build / go test and the arazzo-maestro
# CLI. The `dist` and `lint` targets glob over every `examples/*.arazzo.yaml`
# so adding a new example file requires no Makefile change.

BIN := bin/arazzo-maestro
ARAZZO_FILES := $(wildcard examples/*.arazzo.yaml)

# Version stamped into the binary. Override with `make build VERSION=0.1.0`
# (or `docker build --build-arg VERSION=…` for the image). When goreleaser
# takes over in OpenSSF Phase 2, it will set this from the git tag.
VERSION  ?= 0.0.1
LDFLAGS  := -s -w -X main.version=$(VERSION)

# Files the binary depends on. Beyond Go sources, embedded assets
# referenced via //go:embed must trigger a rebuild — otherwise `make
# dist` would silently render with a stale binary.
GO_SOURCES   := $(shell find cmd internal -name '*.go')
EMBED_ASSETS := $(shell find cmd internal -name '*.html' -o -name '*.yml' -o -name '*.json')

.DEFAULT_GOAL := help

.PHONY: help build test vet lint dist clean

help: ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[1m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

$(BIN): $(GO_SOURCES) $(EMBED_ASSETS) go.mod go.sum
	go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/arazzo-maestro

build: $(BIN) ## Build the arazzo-maestro binary.

test: ## Run go test on every package.
	go test ./...

vet: ## Run go vet on every package.
	go vet ./...

lint: $(BIN) ## Lint every examples/*.arazzo.yaml.
	@status=0; \
	for f in $(ARAZZO_FILES); do \
		echo "→ lint $$f"; \
		$(BIN) lint $$f || status=$$?; \
	done; \
	exit $$status

dist: $(BIN) ## Render every examples/*.arazzo.yaml in light + dark themes under dist/<workflow>/{light,dark}/.
	@rm -rf dist
	@for f in $(ARAZZO_FILES); do \
		name=$$(basename $$f .arazzo.yaml); \
		echo "→ view $$f → dist/$$name/{light,dark}/"; \
		$(BIN) view $$f -o dist/$$name/light --theme light; \
		$(BIN) view $$f -o dist/$$name/dark  --theme dark; \
	done

clean: ## Remove dist/ and bin/.
	rm -rf dist bin
