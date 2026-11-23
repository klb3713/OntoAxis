# Proposal: Jaeger v1 → v2 迁移

## 范围归属

**本 change 不属于 Stage 2。** Stage 2 brief 的 change 清单未列入它，S2-AC-09 与 Stage 2 退出条件也不要求 Jaeger v2；该 change 登记在 `docs/stages/stage-2-change-map.md` §12「Stage 外/后续候选」，待 Stage 3 立项或独立授权后实施。其存在意义是：在 Stage 2 的依赖台账 6.1 回填 jaeger 行之前，先把 v2 的目标版本与迁移路径定清楚。

## Why

`jaegertracing/all-in-one:1.62.0` 仍能拉取，但 Jaeger v1 已于 2025 年 EOL，只在出安全问题时补丁；仓库的 `deploy/deps/policy.yaml` 给 jaeger 设的 `review_by` 是 2026-12-31，与 Helm 占位 digest 例外到期同一天。继续给 v1 补 digest 等于把不可持续版本钉死，所以现在定版本、立项迁移，而不是等例外到期再处理。

## What Changes

- 运行时镜像从 `jaegertracing/all-in-one`（v1）切换到 v2 系列镜像，固定明确 tag 与 `sha256` digest，Compose 与 Helm values 同步更新。
- v1 的启动参数/环境变量迁移到 v2 的配置方式；迁移后 OTLP 接收与 UI 查询行为保持不变。
- **BREAKING**：Jaeger 镜像名与配置入口变更，v1 专有的 flag/env 组合不再生效；沿用旧配置的部署会启动失败或采集不到 trace，需按迁移说明改写。
- 本地 `observability` profile 与 smoke 脚本的 trace 断言同步更新，保证「开启 profile 后能查到 trace」这一契约仍可验证。
- `deploy/deps/policy.yaml` 的 jaeger 条目更新 `review_by` 与版本约束，lock 与审计报告随之刷新。

不在本 change 内：

- 不调整 trace 采样策略、告警规则、dashboard 内容；
- 不引入 Tempo 或其它后端替换；
- 不做生产级 HA 拓扑、对象存储后端与保留期策略（属于生产交付演进）；
- 本次只完成规划产物；实施需负责人单独授权，且授权前不得改动部署资产。

## Capabilities

**New Capabilities**

- 无。

**Modified Capabilities**

- `observability`：本地可复现观测栈中的 Jaeger 后端版本约束与查询契约。

## Impact

- 部署资产：`deploy/compose/compose.yaml`（observability profile 的 jaeger 服务）、`deploy/helm/ontoaxis-observability/values.yaml` 与相关模板参数。
- 依赖治理：`deploy/deps/policy.yaml`、`deploy/deps/lock.yaml`、审计报告、`renovate.json` 的 p0 包名（镜像名变化后需同步）。
- 验证脚本与文档：`scripts/observability-smoke.sh` 的 trace 查询断言、`docs/workflow/DEPENDENCIES.md` 的 Jaeger 已知缺口条目、Stage 2 观测栈说明。
- 不触碰：`internal/`、`web/`、`go.mod`、`go.sum`；core 的 OTel exporter 契约不变。
- 目标版本：2026-11-23 查询 Docker Hub，`jaegertracing/jaeger` 最新的非 `latest` 稳定 tag 为 `2.23.0`（2026-11-05 推送，来源：https://hub.docker.com/v2/repositories/jaegertracing/jaeger/tags ）。该 tag 作为候选目标版本，实施时以 registry 实际返回的 digest 为准，不在规划阶段臆造 digest。
