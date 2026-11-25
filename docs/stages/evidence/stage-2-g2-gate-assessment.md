# Stage 2 · G2 门禁评估（2026-11-25）

- 评估人：AI 代理（未登记 pass；Gate 登记需负责人确认）
- 门禁定义：`docs/stages/stage-2-change-map.md` §4「Wave 2 — 搜索与生产交付」门 G2
- **结论：暂不能直接进入 `stage-2-integration`；G2 尚未 pass。**
  - 4 项条件已有可复核证据；
  - 1 项条件被 **D-07 端口冲突**阻断（所有 Wave 1 服务同栈启动）；
  - 另有 2 个准入缺口：G2 无 pass 记录、8 个前置 change 未全部归档（OSM 本 change 任务已 52/52，证据已由 `stage-2-integration` 收录，归档待负责人授权）。

## 1. G2 条件逐项判定

| G2 条件 | 状态 | 证据 / 缺口 |
|---|---|---|
| outbox → Meilisearch 对象搜索 E2E 通过，增量更新与全量重建可复现 | **满足** | `openspec/changes/object-search-meilisearch/tasks.md` 8.1：隔离项目复跑 `object-search e2e: 73 passed, 0 failed`（CRUD 搜索收敛、A/B 租户隔离、两次 `rebuild --verify` 文档数/digest 一致、停机降级、重启追平、index-lag 指标）；5.7 两次重建可复现（`evidence/2026-11-24-object-search-verification.md`）。 |
| Helm/ArgoCD 交付资产在本地 kind 完成部署，探针/PDB/Secret 生效 | **满足** | `production-delivery` 已归档：独立演练集群 9 个 Application `Synced/Healthy`；探针 3.1、PDB 3.4、Secret/ESO 证据见 `docs/stages/evidence/stage-2-production-delivery-kind.md`、`stage-2-production-delivery.md` §17–§21。 |
| 备份恢复与镜像回滚演练通过 | **满足** | L1–L5 恢复 `dist/delivery/restore-l5.log`；5.9a 坏 digest 与 5.9b readiness 两轮回滚（`dist/delivery/drill-5.9a|b/`）。 |
| 生产 IdP 不在 Stage 2 范围 | **满足（按收缩口径）** | 已移出至 `production-idp-staging`；本地合成 IdP 8.3/8.4/8.5 仅技术证据，见 `docs/stages/evidence/stage-2-idp-security-review.md`。 |
| 所有 Wave 1 服务的 compose/helm 配置可启动 | **不满足** | 配置可解析：`base+authz` rc=0、`base+pipeline-kestra` rc=0、`base+action-temporal` rc=0、`base+connector-runtime+trino-iceberg --profile connector-runtime` rc=0。但 **D-07**：Kafka Connect 默认宿主端口 `18086`（`deploy/connector-runtime/compose.connector-runtime.yaml:105`）与 Kestra `18086`（`deploy/compose/pipeline-kestra/compose.pipeline.yaml:29`）冲突，两个 profile 无法同栈启动（`port is already allocated`），`stage-2-integration` 5.4「同时/分组启动 Wave 1/2 运行时 profile」无法通过。 |

## 2. 准入缺口（`stage-2-integration` tasks 1.1 的硬条件）

`openspec/changes/stage-2-integration/tasks.md:7` 1.1 要求：
1. `docs/stages/stage-2-change-map.md` §4 存在 **G2 为 pass** 的记录（日期、命令、结果、确认人）——**当前不存在**（仅 G1 记录在 `docs/stages/evidence/stage-2-g1-gate-record.md`）。
2. 8 个前置 change 已归档 —— **当前 7/8**：`object-search-meilisearch` 51/52（8.4 待 `stage-2-integration` 收录证据）尚未归档。
3. `openspec list` 仅本 change 在途 —— **当前不满足**：在途还有 `object-search-meilisearch`、`jaeger-v2`，以及 Stage 3 候选骨架 `production-delivery-hardening`、`production-idp-staging`（后两者不属 Stage 2，判定时应排除；前两者需处理）。

## 3. 建议执行顺序（最短路径）

1. **修 D-07（Fast Track，行为不变）**：`ONTOAXIS_CONNECT_HOST_PORT` 默认改 `18089`，Kestra 保持 `18086`；同步更新 `docs/architecture/services-and-ports.md`（补 Connect/Meilisearch/source-postgres/Redpanda 行）、`deploy/connector-runtime/README.md`、`deploy/connector-runtime/scripts/{verify-connector-runtime,cleanup}.sh`、`internal/modules/connectors/runtime/runtime_integration_test.go` 的默认 URL、`docs/stages/stage-2-unblock-checklist.md` §3.7。
2. **验证同栈启动**：`docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml -f deploy/connector-runtime/compose.trino-iceberg.yaml -f deploy/compose/pipeline-kestra/compose.pipeline.yaml -f deploy/compose/action-temporal/compose.temporal.yaml --profile connector-runtime --profile pipeline-kestra config -q` + 分组 `up -d` 端口检查；记录到 `docs/stages/evidence/stage-2-integration/`。
3. **登记 G2 pass**：负责人确认后创建 `docs/stages/evidence/stage-2-g2-gate-record.md`，并在 change map §4 写 G2 记录（日期/命令/结果/确认人）。
4. **解除 OSM 归档前置**：`stage-2-integration` 收录 OSM 8.4 证据（`openspec/changes/object-search-meilisearch/handoff/stage-2-integration.md` 列出的文件与 CI run 45405474349 链接）后归档 OSM；或经负责人批准将 8.4 标「移交 stage-2-integration」并归档。
5. **明确 jaeger-v2 不属 Stage 2 退出判据**（0/10 规划项），在 1.1 准入核对中记录排除理由。
6. 再开始 `stage-2-integration` 的 2.5（合并 pipeline-kestra-dbt）、5.4、6.x、7.x 等未完成任务。

## 4. 结论

- **不能直接开始 stage-2-integration**：G2 条件 5 被 D-07 阻断，且 G2 未登记、OSM 未归档。
- **可以在完成上述 1–5 后立即推进**；其中 D-07 是唯一需要改仓库配置的硬阻塞，其余为门禁记录与归档顺序问题。
- 上述动作不改变 Stage 2 退出结论；Stage 2 仍为 `in_progress`，G3 与 Stage 退出仍需负责人确认。

## 5. 更新（2026-11-25）

- **D-07 已修复（配置级）**：Kafka Connect 默认 `18086→18089`，search worker 端口参数化 `ONTOAXIS_SEARCH_WORKER_PORT`（避开 Temporal UI `18088`）。全 profile `docker compose ... config -q` 通过，16 服务无重复宿主端口（`docs/stages/evidence/stage-2-integration/d07-port-conflict.md`）。
- **仍未 pass**：上述仅为配置级校验；G2 条件 5 要求“可启动”，真正的分组 `up -d` 健康矩阵未执行（本机 docker 无法拉取 Connect/Kestra/Temporal/Redpanda 镜像：`registry-1.docker.io ... EOF`）。
- **OSM 归档前置**：object-search-meilisearch 本 change 任务已 52/52，8.4 证据已由 `stage-2-integration` 收录（`docs/stages/evidence/stage-2-integration.md` §2）；归档仍需负责人单独授权。
- **G2 登记**：仍需要在负责人确认后新建 `docs/stages/evidence/stage-2-g2-gate-record.md` 并在 change map §4 记录（本轮未登记、未宣布 pass）。
