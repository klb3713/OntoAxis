# Proposal: Stage 2 依赖版本治理

## Why

2026-11-23 的版本盘点暴露三件事：镜像与工具链版本散落在 compose、Dockerfile、Helm values 与 Makefile 四处，没有单一真源；`trinodb/trino:latest`、`bufbuild/buf:latest` 与 Helm 的 22 条占位镜像引用（21 个组件 + jobs 备份镜像）让「固定版本」无法举证；Jaeger v1 已 EOL 近一年才被发现。缺的是清单、门禁和升级节奏，不是某一次升级。

## What Changes

- 新增 `deploy/deps/policy.yaml`：组件 tier、owner、review_by 与带 scope/expires 的例外登记。
- 新增 `deploy/deps/lock.yaml` + `make deps-lock`：从 compose / Dockerfile / Helm values / Makefile 抽取实际 pin，生成无时间戳、可 diff 的锁文件。
- 新增 `make deps-check`：锁文件新鲜度、`latest` 禁令、策略覆盖、例外到期为阻塞级；digest 强制按 `digest_required_from` 日期棘轮收紧。
- 新增 `make deps-audit`：查询上游最新版本与 EOL，输出带时间戳 markdown 报告，作为阶段退出证据。
- 新增 `renovate.json`：patch 自动、minor 人工、major 与 P0 引擎禁止自动合并。
- 新增 `.github/workflows/deps.yml`：PR 阻塞校验 + 每月审计。
- 新增 `docs/workflow/DEPENDENCIES.md` 政策真源，并在 `AGENTS.md`、`RUNBOOK`、Stage 2 brief 建立引用。
- 修复两处破窗：Trino `latest` → `484@sha256:2704eb53…`；buf `latest` → `1.75.0`。
- 本次**不**批量升级 P0 运行时；Helm 占位 digest 回填登记为 2026-12-31 到期例外，由 `production-delivery` 执行。

## Capabilities

**New Capabilities**

- `dependency-governance`：依赖版本的清单、门禁、审计与升级通道契约。

**Modified Capabilities**

- 无。

## Impact

- 新增：`deploy/deps/`、`scripts/deps.sh`、`scripts/deps.py`、`renovate.json`、`.github/workflows/deps.yml`、`docs/workflow/DEPENDENCIES.md`
- 修改：`Makefile`（3 个 target、2 个镜像 pin）、`deploy/compose/compose.yaml`（Trino 一行；该文件 owner 为 `production-delivery`，本次按负责人 2026-11-23 指示执行并在 change map 记录偏差）、`AGENTS.md`、`docs/workflow/RUNBOOK.md`、`docs/stages/stage-2-ontoaxis-runtime-brief.md`、`docs/stages/stage-2-change-map.md`
- 不触碰：`internal/`、`web/`、`go.mod`、`go.sum`、Helm values 内容
- 工具依赖：仅 python3 标准库。本机 Go build cache 在沙箱外不可写，且 CI 自带 python3，故不引入 Go 工具与第三方包。
- 已知范围外：GitHub Actions 的 SHA pin（`uses:` 目前为 `@vN`）不在本次范围，登记为独立后续任务；私有 registry 凭据由 CI secret 提供。