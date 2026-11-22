SHELL := /bin/bash
GO ?= go
BUF ?= buf
BUF_DOCKER ?= docker run --rm -v "$(CURDIR)":/workspace -w /workspace/proto bufbuild/buf:latest

.PHONY: help build test test-race fmt vet lint license-check proto-lint proto-breaking generate generate-check docker-build compose-up compose-down smoke observability-rules-test observability-smoke

help:
	@echo "Targets: build test test-race fmt vet lint license-check proto-lint proto-breaking generate generate-check docker-build compose-up compose-down smoke observability-rules-test observability-smoke"

build:
	$(GO) build ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

vet:
	$(GO) vet ./...

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not installed; go vet passed"; fi

license-check:
	$(GO) run github.com/google/go-licenses@v1.6.0 check ./...

proto-lint:
	@if command -v $(BUF) >/dev/null 2>&1; then cd proto && $(BUF) lint; else $(BUF_DOCKER) lint; fi

proto-breaking:
	@if command -v $(BUF) >/dev/null 2>&1; then cd proto && $(BUF) breaking --against '../.git#branch=main,subdir=proto'; else $(BUF_DOCKER) breaking --against '../.git#branch=main,subdir=proto'; fi

generate:
	@if command -v $(BUF) >/dev/null 2>&1; then cd proto && $(BUF) generate; else $(BUF_DOCKER) generate; fi

generate-check:
	$(MAKE) generate
	@if [ -n "$$(git status --porcelain -- gen openapi web/src/generated | grep -v '^??')" ]; then echo "generated files changed; run make generate and commit outputs"; git status --short -- gen openapi web/src/generated; exit 1; fi

docker-build:
	docker build -t ontoaxis-core:dev .

compose-up:
	docker compose -f deploy/compose/compose.yaml up -d --build

compose-down:
	docker compose -f deploy/compose/compose.yaml down -v

smoke:
	./scripts/smoke-foundation.sh

observability-rules-test:
	docker run --rm --entrypoint promtool -v "$(CURDIR)/deploy/observability/prometheus":/etc/prometheus prom/prometheus:v3.1.0 test rules /etc/prometheus/rules_test.yaml

observability-smoke:
	./scripts/observability-smoke.sh
