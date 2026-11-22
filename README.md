# OntoAxis（元轴）

OntoAxis 是面向企业的 Ontology 数据平台，围绕对象类型、对象实例、数据集、Pipeline 与查询提供统一的数据建模和探索能力。

当前仓库已完成 **Stage 1：Core 一次性构建**（`exited`，2026-11-22）。九个业务模块、单进程 `ontoaxis-core`、OIDC/Keycloak、Trino、`oa-web` 与 Compose 本地闭环已落地。

**Stage 2：真实运行时与生产入口** 目前为 `draft`，等待负责人评审；未批准前不创建 change、不实施。

## 设计收益

- 自研 Go 代码目标控制在 **2–4 万行**；
- 运行中的自研后端服务只有 **1 个**（`oa-core`），另有两个自研部署单元 `oa-edge` 与 `oa-web`；
- 身份、授权、网关、CDC、数据湖、SQL、对象存储由成熟 OSS 承担；
- `proto/` 与五平面设计保持稳定，代码更少、边界更清晰、部署更可控。

## 目录

- [`ARCHITECTURE.md`](ARCHITECTURE.md)：目标架构设计与关键决策（设计真源）
- [`SPEC.md`](SPEC.md)：平台基座的模块、配置、错误与迁移约定
- [`AGENTS.md`](AGENTS.md)：协作规则、命令与安全边界
- [`CLAUDE.md`](CLAUDE.md)：Claude Code 入口
- [`docs/stages/stage-1-ontoaxis-core-brief.md`](docs/stages/stage-1-ontoaxis-core-brief.md)：Stage 1 范围与 AC
- [`docs/stages/stage-1-change-map.md`](docs/stages/stage-1-change-map.md)：Stage 1 change 拆解
- [`docs/stages/stage-2-ontoaxis-runtime-brief.md`](docs/stages/stage-2-ontoaxis-runtime-brief.md)：Stage 2 范围与 AC（draft）
- [`docs/stages/stage-2-change-map.md`](docs/stages/stage-2-change-map.md)：Stage 2 change 拆解（draft）
- [`docs/workflow/RUNBOOK.md`](docs/workflow/RUNBOOK.md)：Stage 与 OpenSpec 工作流
- [`openspec/config.yaml`](openspec/config.yaml)：OpenSpec 项目上下文与 artifact 规则

## 本地启动

```sh
docker compose -f deploy/compose/compose.yaml up -d --build
curl -fsS http://127.0.0.1:18080/healthz
curl -fsS http://127.0.0.1:18080/readyz
curl -fsS http://127.0.0.1:19090/metrics | head
open http://127.0.0.1:18082/   # oa-web；Keycloak 登录 admin / admin123
docker compose -f deploy/compose/compose.yaml down -v
```

如本机端口被占用，可通过 `ONTOAXIS_CORE_HOST_PORT` / `ONTOAXIS_METRICS_HOST_PORT` / `ONTOAXIS_POSTGRES_HOST_PORT` / `ONTOAXIS_KEYCLOAK_HOST_PORT` / `ONTOAXIS_TRINO_HOST_PORT` / `ONTOAXIS_WEB_HOST_PORT` 覆盖 Compose 宿主端口。

## 开发

```sh
go build ./...
go test ./...
go test -race ./...
gofmt -w cmd internal
```

平台基座细节见 [`SPEC.md`](SPEC.md) 与 [`docs/getting-started/local-development.md`](docs/getting-started/local-development.md)。

## 开发流程

1. 新功能、行为变化、接口/权限/数据语义变化：走 OpenSpec change（先 proposal + delta specs，再实施）。
2. 行为不变的小修：走 Fast Track，保留测试和清晰提交说明。
3. 阶段级交付：按 `docs/workflow/RUNBOOK.md` 的 Stage 流程管理范围、AC、证据与退出。
4. 所有 OpenSpec 校验使用 `openspec validate --all --strict`。
