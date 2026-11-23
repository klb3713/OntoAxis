SHELL := /bin/bash
GO ?= go
BUF ?= buf
BUF_DOCKER ?= docker run --rm -v "$(CURDIR)":/workspace -w /workspace/proto bufbuild/buf:1.75.0
GO_DOCKER ?= docker run --rm -v "$(CURDIR)":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.26.3

.PHONY: help build test test-race fmt vet lint license-check deps-lock deps-check deps-audit deps-test deps-resolve proto-lint proto-breaking generate generate-check edge-generate edge-generate-check docker-build compose-up compose-down smoke observability-rules-test observability-smoke

help:
	@echo "Targets: build test test-race fmt vet lint license-check deps-lock deps-check deps-audit deps-test deps-resolve proto-lint proto-breaking generate generate-check edge-generate edge-generate-check docker-build compose-up compose-down smoke observability-rules-test observability-smoke"

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
	# nexus-proto-annotations 是 Temporal 的传递依赖，模块内无许可证元数据；经负责人批准加路径例外，依赖树其余包仍检查。
	$(GO) run github.com/google/go-licenses@v1.6.0 check --ignore github.com/nexus-rpc/nexus-proto-annotations ./...

# 依赖版本治理：policy.yaml 是单一真源，lock.yaml 由 deps-lock 生成。
# check 离线且可在 CI 阻塞；audit 需要网络，结果用 `make deps-audit > report.md` 存档。
deps-lock:
	python3 scripts/deps.py lock --write

deps-check:
	python3 scripts/deps.py check

deps-audit:
	python3 scripts/deps.py audit

deps-test:
	python3 scripts/deps_test.py

deps-resolve:
	@echo "# 联网查询 tag→digest 映射（Helm 占位 digest 回填前使用）"
	python3 scripts/deps.py resolve

proto-lint:
	@if command -v $(BUF) >/dev/null 2>&1; then cd proto && $(BUF) lint; else $(BUF_DOCKER) lint; fi

proto-breaking:
	@if command -v $(BUF) >/dev/null 2>&1; then cd proto && $(BUF) breaking --against '../.git#branch=main,subdir=proto'; else $(BUF_DOCKER) breaking --against '../.git#branch=main,subdir=proto'; fi

generate:
	@if command -v $(BUF) >/dev/null 2>&1; then cd proto && $(BUF) generate; else $(BUF_DOCKER) generate; fi

generate-check:
	$(MAKE) generate
	@if [ -n "$$(git status --porcelain -- gen openapi web/src/generated | grep -v '^??')" ]; then echo "generated files changed; run make generate and commit outputs"; git status --short -- gen openapi web/src/generated; exit 1; fi

edge-generate:
	@if command -v $(GO) >/dev/null 2>&1; then $(GO) run ./tools/edgegen -config config/edge/routes.yaml -output deploy/edge/generated; else $(GO_DOCKER) go run ./tools/edgegen -config config/edge/routes.yaml -output deploy/edge/generated; fi

edge-generate-check:
	@if command -v $(GO) >/dev/null 2>&1; then $(GO) run ./tools/edgegen -config config/edge/routes.yaml -output deploy/edge/generated -check; else $(GO_DOCKER) go run ./tools/edgegen -config config/edge/routes.yaml -output deploy/edge/generated -check; fi

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

## Delivery (Helm / GitOps)
delivery-lint:
	helm lint deploy/helm/ontoaxis-platform deploy/helm/ontoaxis-data deploy/helm/ontoaxis-app deploy/helm/ontoaxis-observability deploy/helm/ontoaxis-jobs

delivery-render:
	bash scripts/delivery-render-check.sh

delivery-render-check:
	bash scripts/delivery-render-check.sh

delivery-config-check:
	bash scripts/delivery-config-check.sh

delivery-sync-observability:
	bash scripts/delivery-sync-observability.sh

DELIVERY_ENV ?= kind

delivery-smoke:
	bash scripts/delivery-smoke.sh --env $(DELIVERY_ENV)

delivery-diag:
	bash scripts/delivery-diag.sh

delivery-backup:
	kubectl -n oa-data create job --from=cronjob/ontoaxis-backup backup-manual-$$(date -u +%Y%m%dT%H%M%SZ)

delivery-restore:
	bash scripts/delivery-restore.sh --manifest $${MANIFEST:?set MANIFEST=/path/to/manifest.json}

delivery-idp-check:
	bash scripts/delivery-idp-check.sh --issuer $${OIDC_ISSUER:-http://127.0.0.1:18088/realms/ontoaxis} --core-url $${OIDC_CORE_URL:-http://127.0.0.1:18088}

delivery-promote:
	bash scripts/delivery-promote.sh --from $${FROM_ENV:?set FROM_ENV} --to $${TO_ENV:?set TO_ENV} --release $${RELEASE:?set RELEASE}

delivery-rollback:
	bash scripts/delivery-rollback.sh --env $${TARGET_ENV:?set TARGET_ENV} --release $${RELEASE:?set RELEASE}

delivery-kind-up:
	bash deploy/kind/kind-up.sh

delivery-kind-down:
	bash deploy/kind/kind-down.sh

.PHONY: delivery-lint delivery-render delivery-render-check delivery-config-check delivery-sync-observability delivery-smoke delivery-diag delivery-backup delivery-restore delivery-idp-check delivery-promote delivery-rollback delivery-kind-up delivery-kind-down
