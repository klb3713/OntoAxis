# 部署与回滚

## 镜像

```sh
core_tag="$(git rev-parse --short HEAD)"
web_tag="$(git rev-parse --short HEAD)"
docker build -t "ontoaxis-core:${core_tag}" .
docker build -f web/Dockerfile -t "ontoaxis-web:${web_tag}" .
```

镜像构建是确定性的：相同源码、相同基础镜像与相同构建参数重复执行时，`docker build` 返回相同 image id（Stage 1 已验证两次构建 ID 一致）。

## 本地 Compose

```sh
docker compose -f deploy/compose/compose.yaml up -d --build
```

| 服务 | 宿主端口 |
| --- | --- |
| web | 18082 |
| core | 18080 |
| core metrics | 19090 |
| Keycloak | 18083 |
| Trino | 18081 |
| PostgreSQL | 15433 |
| oa-edge（`edge` profile） | 18085 |

健康检查：

```sh
curl -fsS http://127.0.0.1:18080/healthz
curl -fsS http://127.0.0.1:18080/readyz
```

## 回滚

1. 回滚前先停止写入并备份数据库：`docker compose -f deploy/compose/compose.yaml exec postgres pg_dump ...`。
2. 将 `deploy/compose/compose.yaml` 中 `ontoaxis-core` / `web` 的 `image` 固定到上一个已验证 tag（不要依赖 `latest`）。
3. `docker compose -f deploy/compose/compose.yaml up -d --no-build ontoaxis-core web`。
4. 校验 `/healthz`、`/readyz` 和关键业务 smoke；迁移只向前执行，若需回滚 schema 必须使用备份恢复，不要手工改迁移历史。
5. 记录回滚 tag、数据库备份路径、时间和执行人。

## oa-edge profile

```sh
make edge-generate
docker compose -f deploy/compose/compose.yaml --profile edge up -d --build
```

该 profile 启动 `oa-edge`（Traefik）和 `ontoaxis-core-edge`；后者设置 `ONTOAXIS_TRUST_GATEWAY=true`，只接受 edge 注入的内部身份头。direct `ontoaxis-core` 保持 `ONTOAXIS_TRUST_GATEWAY=false`，继续服务 `oa-web`。

## 注意

- `ONTOAXIS_TRUST_GATEWAY=true` 仅用于受控网络边界；生产由 `oa-edge` 剥离并重写内部身份头后再转发。
- 不要在宿主机直接暴露 direct core 的 18080；本地端口映射仅用于验收。

---

# Helm delivery assets (Stage 2 — `production-delivery`)

The Helm charts and environment overlays live in [`deploy/helm/`](helm/README.md)
and [`deploy/values/`](values/). This directory is **GitOps/Helm delivery**, not a
replacement for the local Compose stack above. Full chart map and required-value
contract: [`deploy/helm/README.md`](helm/README.md).

## Charts

| Chart | Namespace(s) | Contents |
| --- | --- | --- |
| `ontoaxis-platform` | `oa-system` (creates all namespaces) | postgres, keycloak, openfga, valkey, seaweedfs + Namespace objects |
| `ontoaxis-data` | `oa-data` | kafka, debezium, apicurio, kestra, temporal, trino, nessie |
| `ontoaxis-app` | `oa-app` | ontoaxis-core, web, meilisearch, ontoaxis-search (worker) |
| `ontoaxis-observability` | `oa-observability` | otel-collector, prometheus, alertmanager, jaeger, perses |
| `ontoaxis-jobs` | `oa-data` | PostgreSQL backup CronJob (`pg_dump -Fc`), PVC, manifest.json, retention 7, daily 02:00 UTC |

Only `ontoaxis-platform` renders `Namespace` resources (`oa-system`, `oa-app`,
`oa-data`, `oa-observability`) with an optional ArgoCD `sync-wave: "-10"`
annotation. Other charts assume the namespaces already exist.

## Environment overlays (`deploy/values/`)

| File | Purpose |
| --- | --- |
| `common.yaml` | Shared defaults (image pull policy, resource envelopes, OIDC base, secret store). |
| `kind.yaml` | Single-node kind/k3d: 1 replica, low resources. |
| `kind-small.yaml` | kind/k3d with heavy components trimmed (kafka/debezium/apicurio/kestra/temporal/trino/nessie + jaeger/perses disabled). |
| `staging-like.yaml` | Rehearsal: key components 2 replicas, staging OIDC issuer/audience, non-empty placeholder image digests. |
| `prod-like.yaml` | **Render-only** production-shaped overlay. Never applied to a real cluster from here. |

Apply one overlay to any chart as:

```sh
helm template oa-app deploy/helm/ontoaxis-app \
  -f deploy/values/common.yaml -f deploy/values/kind.yaml
```

## Toolchain versions (documented target)

| Tool | Target version | Notes |
| --- | --- | --- |
| Helm | 4.3.2 | `helm lint` / `helm template` are the render gates |
| kubectl | 1.29.2 | cluster access |
| kind | 0.33.0 | local Kubernetes |
| k3d | 5.9.0 | local k3s (alternative to kind) |
| argocd CLI | 2.10+ (optional locally; not installed here) | GitOps sync / app-of-apps |
| kubeconform | 0.8.0 | schema validation in render check |
| gitleaks | 8.30.1 | secret scanning |
| External Secrets Operator (ESO) | 0.9+ | provides `ExternalSecret` + `ClusterSecretStore` |
| Envoy Gateway | v1.2.6 | edge / HTTPRoute (see `deploy/edge/generated/`) |
| ArgoCD | 2.10+ | cluster GitOps controller |
| Prometheus | v3.1.0 | metrics (chart pins `prom/prometheus`) |
| OpenTelemetry Collector | 0.120.0 | traces/metrics (`otel/opentelemetry-collector-contrib`) |
| Jaeger | 1.62.0 | trace backend/all-in-one |
| Perses | v0.50.0 | dashboards |

`scripts/delivery-version-check.sh` reports these commands and `MISSING` for any
absent tool; it exits non-zero when a required command (`helm`, `kubectl`,
`kind`, `kubeconform`) is missing.

## Controlled environment boundary

The manifests here are for **controlled environments only**:

- `kind` / `k3d` — local single-node clusters.
- `staging-like` — a rehearsal cluster configured for this repo.
- `prod-like` — **render-only**; it exists to exercise production-shaped output
  and is never applied here.

Do **not** supply real cloud credentials, production secrets, or customer data
to these charts. Secret values are never stored in values files: charts reference
secret **names/keys** only, and actual values are projected by the External
Secrets Operator from the configured `ClusterSecretStore`. Image digests in the
default/overlay values are deterministic **placeholders** and must be replaced by
the release manifest before any real deployment.

## Render / install / rollback pointers

```sh
# Static checks (no cluster needed)
./scripts/delivery-version-check.sh
./scripts/delivery-render-check.sh          # SKIPs helm-specific steps when helm is absent

# Render only
helm template oa-platform deploy/helm/ontoaxis-platform \
  -f deploy/values/common.yaml -f deploy/values/staging-like.yaml > /tmp/platform.yaml

# Install (authorized controlled cluster only)
helm upgrade --install oa-platform deploy/helm/ontoaxis-platform \
  -n oa-system --create-namespace \
  -f deploy/values/common.yaml -f deploy/values/staging-like.yaml
```

Rollback: prefer `helm rollback <release> <revision>` after confirming the target
revision and taking a database backup (the `ontoaxis-jobs` CronJob / manual Job
produces a `pg_dump -Fc` archive + `manifest.json`). Migrations are forward-only;
schema rollback must use a backup restore, never hand-edited migration history.

## Local limitations (current workspace)

- **Helm is not installed here**, so `helm lint` / `helm template` were **not**
  run locally. Charts are authored to be syntactically valid; run the render gate
  on a machine with Helm 3.x before applying.
- **kind/k3d are not exercised** in this environment for the Helm path; the
  Compose stack above remains the local run path.
- YAML values/overlays were statically validated with the Ruby YAML parser
  (PyYAML is not installed in this workspace); see `scripts/delivery-render-check.sh`.

## Make targets

- `make delivery-lint` — `helm lint` 五个 chart。
- `make delivery-render` / `make delivery-render-check` — 渲染并对 5 chart × 4 overlays 做确定性、kubeconform 与 YAML 校验。

## 生产化交付（Stage 2）

- Helm charts：`deploy/helm/ontoaxis-{platform,data,app,observability,jobs}`；环境 values：`deploy/values/`。
- 静态检查：`make delivery-lint`、`make delivery-render-check`、`make delivery-config-check`。
- 本地集群：`deploy/kind/kind-up.sh`（`KIND_REGISTRY_PORT=5055`）/`deploy/kind/kind-down.sh`；`deploy/kind/bootstrap.sh` 安装 ArgoCD/ESO/Envoy 与 app-of-apps。
- GitOps：`deploy/argocd/projects/ontoaxis.yaml` + `deploy/argocd/root-app.yaml`；应用清单 `deploy/argocd/apps/`。
- 晋升/回滚：`scripts/delivery-promote.sh --from kind --to staging-like`、`scripts/delivery-rollback.sh --env kind`；发布清单 `deploy/releases/<env>.yaml`。
- 备份/恢复：`scripts/delivery-restore.sh --backup-dir <dir>`；故障处理见 `docs/operations/delivery-runbook.md`。
- 烟测/诊断：`scripts/delivery-smoke.sh --env kind`、`scripts/delivery-diag.sh`。
