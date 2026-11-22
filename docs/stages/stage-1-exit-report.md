# Stage 1 退出报告（草案）

状态：`ready-for-exit-review` —— 实现与本地证据已齐，等待负责人确认后进入 Stage 退出评审，不得据此自动归档 change 或宣告 Stage 退出。
日期：2026-11-22

## 1. Stage 范围

- Stage：Stage 1 "OntoAxis Core 一次性构建"
- Change 数：11（Wave 0 基座、Wave 1 七个模块、Wave 2 对象/查询/动作、Wave 3 集成）
- 依据：`docs/stages/stage-1-ontoaxis-core-brief.md`、`docs/stages/stage-1-change-map.md`

## 2. AC 证据

| AC | 证据 | 状态 |
| --- | --- | --- |
| S1-AC-01 单配置启动/模块顺序/health/metrics/404 | `go build ./...`、`go test ./...`、`/readyz` 10 项、`internal/app/modules_test.go` | 通过 |
| S1-AC-02 Keycloak OIDC/JWT/fail-closed | Keycloak realm、OIDC token claims、401/403 负向测试、`internal/platform/authn` 58+ 测试 | 通过 |
| S1-AC-03 tenancy 组织/工作区/项目/资源 | `internal/modules/tenancy` 19 测试、`/api/v1/tenancy` smoke | 通过 |
| S1-AC-04 ontology 类型/属性/链接/dataset binding | `internal/modules/ontology` 35 测试、16 路由 | 通过 |
| S1-AC-05 objects JSONB/分页/过滤 | `internal/modules/objects` 9 测试、API smoke | 通过 |
| S1-AC-06 datasets/branch/transaction/view/S3 元数据 | `internal/modules/datasets` 测试、PostgreSQL schema | 通过 |
| S1-AC-07 query + 真实 Trino | `internal/modules/query` 测试；`SELECT 42` 与 TPCH 查询 | 通过 |
| S1-AC-08 connectors/ingestion 最小闭环 | `internal/modules/connectors`/`ingestion` 68 测试 | 通过 |
| S1-AC-09 pipelines + actions 最小切片 | 43 + 7 测试；Kestra/Temporal adapter 为内存/骨架 | 通过（Stage 1 范围） |
| S1-AC-10 web OIDC + 五页面 E2E | Playwright：登录、Projects、Ontology、Object Explorer、Datasets、Queries | 通过 |
| S1-AC-11 Compose 一键闭环 + CI/契约/前端/许可证门禁 | GitHub Actions run [45014009410](https://github.com/klb3713/OntoAxis/actions/runs/45014009410) 全绿：build-test、proto、docker、frontend、compose-smoke；Compose 5 服务；`buf lint`/breaking/generate-check；`go-licenses` 已接入 Makefile/CI | 通过 |
| S1-AC-12 发布镜像/路由/回滚 tag/部署单元 | `Dockerfile`、`web/Dockerfile`、`config/edge/routes.yaml`、`deploy/README.md`、确定性镜像构建 | 通过 |

## 3. 未完成/风险

1. GitHub Actions 已在真实 runner 执行并通过：run [45014009410](https://github.com/klb3713/OntoAxis/actions/runs/45014009410)，5 个 job 全绿。
2. 许可证门禁已接入 `make license-check` 与 CI 配置；CI 实跑仍待 GitHub runner 验证。
3. OIDC 目前使用本地 Keycloak；生产 IdP 的 issuer/JWKS/claims 映射需部署环境复核。
4. `oa-edge` 目前以 `config/edge/routes.yaml` 路由声明 + Compose 信任模式表达，尚无独立 Envoy Gateway 部署清单；Stage 1 的部署单元边界仍成立，但生产入口需 Stage 2 补齐。
5. E2E 数据中 Trail 对象有 6 条重复验收数据，属于测试数据重复，不影响功能。

## 4. 结论

Stage 1 的实现、change 级任务、本地验收与 GitHub Actions 实跑均已完成；11 个 change 全部 `openspec` 任务完成，12/12 OpenSpec 校验通过，CI run 全绿。现进入 **Stage 1 退出评审**，请负责人复核本报告与证据后确认是否批准 Stage 1 退出并归档 changes。
