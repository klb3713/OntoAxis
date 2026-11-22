# Compose E2E 初始阻断与解决（2026-11-23）

## 已执行

```bash
docker compose -f deploy/compose/compose.yaml up -d --build
```

结果：postgres/keycloak/trino 正常；`ontoaxis-core` **启动失败并退出 1**。

## 关键日志

```text
goose: no migrations to run. current version: 1
{"level":"ERROR","msg":"startup failed","error":"load authorization config: ONTOAXIS_AUTHZ_OPENFGA_API_URL is required"}
```

## 结论

- `internal/app` 现按 fail-fast 要求强制 authz 配置；基础 Compose 仍未提供 OpenFGA 服务与 `ONTOAXIS_AUTHZ_OPENFGA_*` 环境，也未提供 store/model bootstrap，因此基础栈无法启动核心。
- `pipeline-kestra-dbt`、`action-temporal`、`object-search-meilisearch` 的 Compose profile 片段尚未合并，`connector-runtime` overlay 仅做静态 config 校验；真实 Stage 2 E2E 无法在当前基础 Compose 上执行。
- 这是 `stage-2-integration` 的跨 change 装配缺陷（可归入 2.9/5.x），不是各自单元实现失败；未伪称任何 E2E/AC 通过。

## 解决状态（同日）

已新增 `deploy/compose/authz/` overlay 与 bootstrap，`./scripts/smoke-foundation.sh` 已通过；详见 `compose-smoke.md`。Temporal/Kestra/Kafka Connect/Meilisearch 的真实 profile 仍未合并启动，Stage 2 全链路 E2E 仍为未通过。

## edge profile + authz 阻断（2026-11-23）

`docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml --profile edge up -d --build` 失败：

- `ontoaxis-core-edge` 启动即退出，日志：`load authorization config: ONTOAXIS_AUTHZ_OPENFGA_API_URL is required`
- 原因：`deploy/compose/authz/compose.authz.yaml` 只给 `ontoaxis-core` 注入 authz 环境，未给 `ontoaxis-core-edge` 注入；edge profile 的 core-edge 因此无法启动。
- 影响：任务 5.2 edge profile 集成暂不能通过；需 `production-delivery`/compose owner 在 authz overlay 中补齐 core-edge 的 authz 环境（或合并 edge profile 时统一注入）。
- 处置：未修改共享 `deploy/compose/` 文件，按边界记录为跨 change 缺陷。
