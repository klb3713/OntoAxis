# Tasks

> 状态：planning（2026-11-22 创建）。**全部任务未开始，未勾选，未产生任何实现或证据。** 勾选前必须存在对应证据；没有证据的项不得标记完成。任务分组顺序即执行顺序，组间有依赖；除 `## Workflow follow-up` 外，所有条目都是 tracked task。

## 1. 准入与基线

- [ ] 1.1 核对 Wave 3 准入：`docs/stages/stage-2-change-map.md` §4 存在 G2 为 `pass` 的记录（日期、命令、结果、确认人），且 8 个前置 change 已归档。验证：`openspec list` 仅本 change 在途；将核对结果与截图/命令输出写入 `docs/stages/evidence/stage-2-integration/baseline/admission.md`。
- [x] 1.2 记录基线快照：commit SHA、`git status`、Go/Node/Docker/Compose 版本、容器镜像版本、现有 CI run id、`/healthz`/`/readyz` 原始输出、`openspec validate --all --strict` 结果。验证：基线文件存在于 `docs/stages/evidence/stage-2-integration/baseline/`，内容可逐条复核。
- [x] 1.3 复跑已归档 `runtime-observability` 与 `edge-gateway` 的证据命令，确认 Stage 2 基线仍可复现。验证：`make observability-rules-test`、`scripts/observability-smoke.sh`、`scripts/edge-smoke.sh` 结果与归档证据一致或在证据中解释差异。

## 2. 按 change 顺序合并

合并顺序固定为 change map DAG 顺序；每个 change 合并后立即运行该 change 归档证据中的验证命令 + `go build ./...` + `go test ./...`，并把提交、命令、结果写入证据索引。

- [x] 2.1 合并 `runtime-observability`：接入 telemetry/指标/observability profile 相关补丁。验证：`go build ./...`、`go test ./...`、`make observability-rules-test`、`scripts/observability-smoke.sh` 通过，日志入库。
- [x] 2.2 合并 `edge-gateway`：接入 oa-edge 生成物使用与 edge profile 配置。验证：`make edge-generate-check`、edge profile 起栈、`EDGE_TEST_FAIL_CLOSED=1 ./scripts/edge-smoke.sh` 通过（含 401/403/404/405/429/5xx），日志入库。
- [x] 2.3 合并 `authorization-openfga-cedar`：接入 OpenFGA/Cedar 客户端与授权中间件接线。验证：该 change 的授权集成测试（allow/deny、跨租户拒绝、无策略默认拒绝、审计）通过；记录其证据链接与本次复跑结果。
- [x] 2.4 合并 `connector-runtime`：接入 Kafka Connect/Debezium 运行时与 ingestion 接缝。验证：该 change 的 CDC 集成测试与同步状态回写检查通过；Compose profile 起栈与日志入库。合并提交 `8f3e533`（含 `internal/app/`、`cmd/ontoaxis-core/` 接线）；记录见 `docs/stages/evidence/stage-2-integration/change-merges.md`（2.4）与 `docs/stages/evidence/stage-2-connector-runtime/README.md`。
- [ ] 2.5 合并 `pipeline-kestra-dbt`：接入 Kestra/dbt 运行时与 pipelines 接缝。验证：Flow 发布/运行/状态回写/失败重试/dataset 产出的集成测试通过，日志入库。
- [x] 2.6 合并 `action-temporal`：接入 Temporal workflow/activity 与 actions 接缝。验证：重试/补偿/审批分支与状态回写集成测试通过；如含 `web/` 审批 UI，一并合并其前端测试。
- [x] 2.7 合并 `object-search-meilisearch`：接入 outbox 消费者与 Meilisearch 索引接缝。验证：对象创建/更新/删除与索引一致性、全量重建可复现的集成测试通过，日志入库。
- [x] 2.8 合并 `production-delivery`：接入 Helm/ArgoCD 交付物、备份恢复与回滚脚本的使用方式。验证：chart lint/模板渲染通过，kind/k3d 安装脚本按其文档可执行，日志入库。
- [x] 2.9 缺陷登记与处置：汇总 2.1–2.8 发现的跨 change 缺陷，逐条分类（依赖缺失/跨 change 缺陷/环境问题/安全回归）并记录处置（退回归属 change 或 owner 签核后在本 change 修复）。验证：证据索引中存在 `defects/` 清单，每条有状态与链接。

## 3. 依赖与生成物收口

- [x] 3.1 统一 `go.mod`/`go.sum`：合并各 change 依赖补丁并执行 `go mod tidy`。验证：`git status --porcelain` 无未提交依赖差异，`go build ./...`、`go mod verify` 通过。
- [x] 3.2 刷新 proto/OpenAPI/前端生成物：执行 `make generate`。验证：`make generate-check` 在干净工作区通过（无差异）。
- [x] 3.3 刷新 edge 生成物：执行 `make edge-generate`。验证：`make edge-generate-check` 通过，路由摘要记入证据。
- [x] 3.4 新增配置键与环境变量只进入示例/文档，不进入真实密钥：核对 `README.md`、`docs/getting-started/local-development.md`、`.env.example`（如存在）。验证：diff 中无 token/密码/生产连接串；新键均有说明。

## 4. 组合根与模块装配

- [x] 4.1 在 `internal/app/` 与 `cmd/ontoaxis-core/` 完成运行时 adapter 接线，保持 9 模块名称/顺序/边界不变。验证：`go build ./...`、`go test ./internal/app/...`；`internal/app/modules_test.go` 的 9 模块顺序断言仍通过。
- [x] 4.2 增加组合测试：断言语义 adapter 被注入、缺失必填配置时启动失败（fail-fast，不静默退回 no-op）。验证：`go test ./internal/app/...` 新增用例通过且覆盖正/负路径。
- [x] 4.3 验证空库启动与迁移：`docker compose ... down -v` 后重新起栈。验证：`/readyz` 返回 9 个业务模块 + authorization + database 共 11 项 `ok`，迁移日志无错误。
- [x] 4.4 验证中间件顺序与信任边界未变：direct core 保持 `ONTOAXIS_TRUST_GATEWAY=false`，edge core 为 `true`；伪造内部身份头在 direct 路径被拒绝。验证：对应负路径测试或 curl 矩阵记录入库。

## 5. Compose 集成

- [x] 5.1 静态校验与 default profile：`docker compose -f deploy/compose/compose.yaml config` 通过；起栈后 `scripts/smoke-foundation.sh` 通过。验证：命令输出与日志入库。
- [x] 5.2 edge profile 集成：起 `--profile edge` 后运行 `EDGE_TEST_FAIL_CLOSED=1 ./scripts/edge-smoke.sh`。验证：全部用例 PASS，日志入库。
- [x] 5.3 observability profile 集成：设置 `ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT` 后起 profile，运行 `make observability-rules-test` 与 `scripts/observability-smoke.sh`。验证：Prometheus 有指标、Jaeger 有 `ontoaxis-core` trace，输出入库。
- [ ] 5.4 Wave 1/2 运行时 profile：按各 change 文档同时/分组启动 Temporal、Kestra、Kafka Connect、Meilisearch、OpenFGA 等，运行各自的 health/集成检查。验证：每个组件健康检查通过；记录资源占用与端口占用。
- [x] 5.5 可重复性：`down -v` 清空后重新起栈一次，确认结果与 5.1–5.4 一致。验证：两次运行日志对比无不可解释差异；写清重置步骤。
- [x] 5.6 Compose 层跨 change 缺陷修复：经 owner 同意后修复 `deploy/compose/` 中的装配缺陷。验证：修复后重跑 5.1–5.5 相关检查通过，修复记录与 owner 同意写入证据。

## 6. kind/k3d 交付层

- [ ] 6.1 Helm 部署：安装 chart，检查 Deployment/Service/ConfigMap/Secret、readiness/liveness probe、PDB、资源限额。验证：`kubectl get pods` 全部 Ready，probe 配置与 chart 一致。
- [ ] 6.2 ArgoCD 同步：创建/更新 Application 并等待同步。验证：`Synced/Healthy` 状态截图或 CLI 输出入库。
- [ ] 6.3 集群入口：验证 Envoy `oa-edge` 路由在集群内生效（public 200、受保护 401/200、未知 404、`/metrics` 不可达）。验证：命令输出入库；与 edge spec 行为一致。
- [ ] 6.4 备份恢复演练：按 `deploy/README.md` 备份 → 恢复。验证：恢复后 `/readyz` 与关键数据抽查通过，演练日志入库。
- [ ] 6.5 镜像回滚演练：固定上一已验证 tag 回滚并重跑 smoke。验证：回滚后 `/healthz`、`/readyz`、核心 smoke 通过，记录 tag 与时间。

## 7. 全栈 E2E（跨 change）

- [ ] 7.1 Compose 基础链路：执行 E2E-01（Keycloak 登录 → web → 工作台 → 对象读写 → health/readyz）与 E2E-08（对象 → action → pipeline → dataset → 查询/搜索组合链）。验证：脚本/步骤日志与截图入库，结果可复现。
- [ ] 7.2 Compose 数据链路：执行 E2E-05（CDC → dataset → 查询）、E2E-06（Kestra+dbt → dataset）、E2E-07（Temporal 重试/补偿/审批）、E2E-04（outbox → Meilisearch → 搜索/重建）。验证：各场景日志与查询结果入库。
- [ ] 7.3 Compose 身份/授权：执行 E2E-02（edge 矩阵）、E2E-03（OpenFGA/Cedar allow/deny、跨租户、默认拒绝、审计）、E2E-12（安全负路径）。验证：状态码/审计记录/拒绝原因入库。
- [ ] 7.4 Compose 可观测：执行 E2E-09（trace 贯通、RED/运行时指标、SLO 面板、告警触发、负载基线复跑）。验证：Jaeger trace JSON、Prometheus 查询结果、面板截图、基线结果入库。
- [ ] 7.5 生产 IdP：执行 E2E-11（非本地 OIDC issuer/JWKS/claims 登录与 API 调用）。验证：登录与 API 成功，claims 映射正确，证据中无任何生产密钥。
- [ ] 7.6 前端 E2E：运行 Stage 1 Playwright 套件；如 `action-temporal` 提供审批 UI，合并并运行其新增用例。验证：`cd web && pnpm test:e2e` 通过，截图/trace 入库。
- [ ] 7.7 kind 栈 E2E 子集：在 kind/k3d 上执行 E2E-01、E2E-02、E2E-10（交付）。验证：结果入库；未覆盖的组合在报告中标 `not covered`。

## 8. 回归与质量门

- [ ] 8.1 Go 质量门：`go build ./...`、`go test ./...`、`go test -race ./...`、`go vet ./...`、golangci-lint（与 CI 同版本）、`make license-check`。验证：全部通过，日志入库。
- [ ] 8.2 前端质量门：`cd web && pnpm install --frozen-lockfile && pnpm check && pnpm test && pnpm build`。验证：全部通过，日志入库。
- [ ] 8.3 契约门：`make proto-lint`、`make proto-breaking`、`make generate-check`、`make edge-generate-check`。验证：全部通过且工作区干净。
- [ ] 8.4 脚本与配置门：`actionlint`、`shellcheck scripts/*.sh`、`docker compose config`。验证：无 error 级结果，日志入库。
- [ ] 8.5 OpenSpec 门：全部前置 change 归档后运行 `openspec validate --all --strict`。验证：输出 0 failed（若仍有在途 change 未归档，不得判定通过）。
- [ ] 8.6 CI 门：在真实 runner 上运行完整 CI（build-test/proto/frontend/docker/observability/compose-smoke/edge）。验证：全绿并记录 run URL 与 job 列表；本地通过不能替代此任务。

## 9. 证据与 Stage 2 报告

- [ ] 9.1 建立证据索引 `docs/stages/evidence/stage-2-integration.md` 与原始目录 `docs/stages/evidence/stage-2-integration/`：按 AC 组织，每行含日期/环境/commit/命令/结果/路径。验证：索引中的每个路径真实存在，抽查 3 条可复跑。
- [ ] 9.2 证据边界与安全检查：确认无 token/密码/生产连接串；声明合成夹具与本地环境边界；运行仓库 secrets/license 检查并记录。验证：检查命令输出入库，证据文件无敏感串。
- [ ] 9.3 起草 `docs/stages/stage-2-exit-report.md`：按 design 的 10 节结构与 S2-AC-01…11 证据表编写。验证：未执行/未通过项如实标 `not passed`；报告状态为 `exit candidate pending owner confirmation`。
- [ ] 9.4 文档同步：更新 `ARCHITECTURE.md` 15.x、`docs/stages/stage-2-change-map.md` 状态、`README.md`、`docs/getting-started/local-development.md`、`docs/architecture/services-and-ports.md` 等（经各 owner 同意）。验证：链接可达、无过期“计划中”表述、命令可照抄执行。
- [ ] 9.5 自检 traceability：核对 S2-AC-01/AC-11 的每条证据路径与状态，S2-AC-02…10 的组合复验记录齐全。验证：traceability 中无未解释的 `planned`；每条 `passed` 有可点开的证据。

## 10. 评审与收口

- [ ] 10.1 提交评审：将 artifacts、证据索引、Stage 2 报告草稿交负责人评审。验证：存在明确评审结论记录（通过/有条件通过/退回），非自动批准。
- [ ] 10.2 确认未自动执行动作：`openspec list` 仍显示本 change 在途、未被归档；Stage 2 brief 状态仍为 `in_progress`（未改为 exited）；未 commit/push 未经确认的内容。验证：命令输出入库。
- [ ] 10.3 记录残余风险与未决项：把 open questions、未覆盖场景、带 caveat 的 AC 写入报告第 8 节。验证：报告与 traceability 的风险条目一致。

## 11. 接收自 object-search-meilisearch 的交接项（2026-11-24）

来源：`openspec/changes/object-search-meilisearch/handoff/stage-2-integration.md`（证据源文件已备，`docs/stages/evidence/` 归本 change 收录）。本 change 2.7 已完成 object-search 的合并；以下为证据收口与边界声明。

- [ ] 11.1 收录证据源文件到 `docs/stages/evidence/`（清单见交接文件 §1）：集成记录、隔离验证（后端 12 项 / 原生 HTTP 68/68 / 隔离 Compose E2E 73/73）、CI 同参本地日志、手册复现记录；按 AC 组织并写索引。验证：索引内每个路径真实存在，抽查可复跑。回填 object-search-meilisearch 8.4。
- [ ] 11.2 回填 object-search-meilisearch 8.2 的 GitHub Actions `search-integration` run 链接（本机无 runner，本地同参 exit 0、0 SKIP）。验证：CI job 绿并记录 run URL。
- [ ] 11.3 记录 object-search 9.1/9.2/9.3 评审签字（评审人 klb3713，2026-11-24）到 Stage 证据索引，附各材料 §4/§5/§6 链接。
- [ ] 11.4 证据边界声明：明确区分“隔离本地 + 合成数据验证”与“CI/生产验收”，不得把本地通过写成 CI 或生产通过；业务质量阈值仍 `pending-owner-signoff`。
- 关联既有项：本 change 5.4（Wave 1/2 运行时 profile 含 Meilisearch）、7.2（E2E-04 outbox→Meilisearch→搜索/重建）可复用上述证据。

## Workflow follow-up

- 本 change 的归档需在评审通过、证据可复核、traceability 一致、`openspec validate --all --strict` 通过后，单独获得授权再执行；不在本 change 的 tracked task 内自动完成。
- Stage 2 退出需所有 change 归档、Stage AC 证据齐全、报告完成、文档同步、负责人确认；本 change 只产出退出候选，不自动宣布退出。
- 提交/合并方式遵循仓库 review 流程；不自动 commit、不自动 push、不自动归档。
