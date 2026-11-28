# Tasks

> 状态：in-progress（2026-11-22 创建，2026-11-28 更新）。当前 **55/56**（6.1–6.5、7.5、7.7 为范围移交，标 `[x]` 但不计入 Stage 2 退出判据；仅 10.1 评审待负责人结论）。勾选前必须存在对应证据；没有证据的项不得标记完成。D-07 端口冲突已修复（配置级，见 `docs/stages/evidence/stage-2-integration/d07-port-conflict.md`）；G2 已于 2026-11-25 由负责人登记 `pass`（附口径，见 change-map §4）；6.x/7.7 已移交 `delivery-runtime-acceptance`。任务分组顺序即执行顺序，组间有依赖；除 `## Workflow follow-up` 外，所有条目都是 tracked task。

## 1. 准入与基线

- [x] 1.1 核对 Wave 3 准入：`docs/stages/stage-2-change-map.md` §4 存在 G2 为 `pass` 的记录（日期、命令、结果、确认人），且 8 个前置 change 已归档。验证：`openspec list` 仅本 change 在途；将核对结果与截图/命令输出写入 `docs/stages/evidence/stage-2-integration/baseline/admission.md`。 — 进展（2026-11-25）：8 个前置 change 已全部归档（含 object-search-meilisearch，见 `openspec/changes/archive/2026-11-25-object-search-meilisearch`）；G2 已于 2026-11-25 由负责人 klb3713 登记为 `pass`（记录见 `docs/stages/stage-2-change-map.md` §4 与 `docs/stages/evidence/stage-2-g2-gate-record.md`，口径：compose 真实 runner 证据 + 归属方 helm/kind 证据；6.x kind 复跑仍属退出条件 4）。剩余核对项：`openspec list` 仍有 3 个 Stage 2 范围外 change（`production-idp-staging`、`production-delivery-hardening`、`jaeger-v2`），排除理由已写入退出报告 §2 与 change-map §12。 — 进展（2026-11-28）：核对完成，8 个前置 change 全部归档；`openspec validate --all --strict`=22 passed/0 failed；`openspec list` 仅剩本 change 与 Stage 2 范围外/新建 change（`production-idp-staging`、`production-delivery-hardening`、`jaeger-v2`、`delivery-runtime-acceptance`）；完整核对输出见 `docs/stages/evidence/stage-2-integration/baseline/admission.md`。
- [x] 1.2 记录基线快照：commit SHA、`git status`、Go/Node/Docker/Compose 版本、容器镜像版本、现有 CI run id、`/healthz`/`/readyz` 原始输出、`openspec validate --all --strict` 结果。验证：基线文件存在于 `docs/stages/evidence/stage-2-integration/baseline/`，内容可逐条复核。
- [x] 1.3 复跑已归档 `runtime-observability` 与 `edge-gateway` 的证据命令，确认 Stage 2 基线仍可复现。验证：`make observability-rules-test`、`scripts/observability-smoke.sh`、`scripts/edge-smoke.sh` 结果与归档证据一致或在证据中解释差异。

## 2. 按 change 顺序合并

合并顺序固定为 change map DAG 顺序；每个 change 合并后立即运行该 change 归档证据中的验证命令 + `go build ./...` + `go test ./...`，并把提交、命令、结果写入证据索引。

- [x] 2.1 合并 `runtime-observability`：接入 telemetry/指标/observability profile 相关补丁。验证：`go build ./...`、`go test ./...`、`make observability-rules-test`、`scripts/observability-smoke.sh` 通过，日志入库。
- [x] 2.2 合并 `edge-gateway`：接入 oa-edge 生成物使用与 edge profile 配置。验证：`make edge-generate-check`、edge profile 起栈、`EDGE_TEST_FAIL_CLOSED=1 ./scripts/edge-smoke.sh` 通过（含 401/403/404/405/429/5xx），日志入库。
- [x] 2.3 合并 `authorization-openfga-cedar`：接入 OpenFGA/Cedar 客户端与授权中间件接线。验证：该 change 的授权集成测试（allow/deny、跨租户拒绝、无策略默认拒绝、审计）通过；记录其证据链接与本次复跑结果。
- [x] 2.4 合并 `connector-runtime`：接入 Kafka Connect/Debezium 运行时与 ingestion 接缝。验证：该 change 的 CDC 集成测试与同步状态回写检查通过；Compose profile 起栈与日志入库。合并提交 `8f3e533`（含 `internal/app/`、`cmd/ontoaxis-core/` 接线）；记录见 `docs/stages/evidence/stage-2-integration/change-merges.md`（2.4）与 `docs/stages/evidence/stage-2-connector-runtime/README.md`。
- [x] 2.5 合并 `pipeline-kestra-dbt`：接入 Kestra/dbt 运行时与 pipelines 接缝。验证：Flow 发布/运行/状态回写/失败重试/dataset 产出的集成测试通过，日志入库。 — 进展（2026-11-25）：该 change 已归档（69/69，真实 Kestra v2.0.12 E2E 见 `openspec/changes/archive/2026-11-23-pipeline-kestra-dbt/`）；本轮 CI 矩阵已验证 Kestra ACTIVE + Trino 200（run 45570760338）。— 完成（2026-11-28）：E2E-06 本地 Compose **11/11** 覆盖 Flow 发布、run 触发、状态回写 succeeded、dbt dataset 产出（Trino `select answer=42`）；证据 `pipeline-dbt-7.2.md`。失败重试路径沿用归档 change 的 69/69 集成证据（run_results failed→job failed）。
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
- [x] 5.4 Wave 1/2 运行时 profile：按各 change 文档同时/分组启动 Temporal、Kestra、Kafka Connect、Meilisearch、OpenFGA 等，运行各自的 health/集成检查。验证：每个组件健康检查通过；记录资源占用与端口占用。— 进展（2026-11-25）：D-07 端口冲突已修复（Connect `18086→18089`；search worker 端口参数化 `ONTOAXIS_SEARCH_WORKER_PORT` 以避开 Temporal UI `18088`），全 profile 合并 `docker compose ... config -q` 通过、16 服务无重复宿主端口（`d07-port-conflict.md`）；CI runner 分组 `up -d`：[run 45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) connector-runtime/pipeline-kestra/action-temporal 3/3 绿（`wave1-stack-matrix.md`）。2026-11-28 完成：本地同时启动 connector+search+authz（`oa-e2e05`，10 容器健康）与 pipeline+search（`oa-e2e06`，9 容器）两组，端口/资源快照见 `wave1-stack-local.md`；fullstack-e2e [run 45607781575](https://github.com/klb3713/OntoAxis/actions/runs/45607781575) 四组全绿。
- [x] 5.5 可重复性：`down -v` 清空后重新起栈一次，确认结果与 5.1–5.4 一致。验证：两次运行日志对比无不可解释差异；写清重置步骤。
- [x] 5.6 Compose 层跨 change 缺陷修复：经 owner 同意后修复 `deploy/compose/` 中的装配缺陷。验证：修复后重跑 5.1–5.5 相关检查通过，修复记录与 owner 同意写入证据。

## 6. kind/k3d 交付层

- [x] 6.1 Helm 部署：安装 chart，检查 Deployment/Service/ConfigMap/Secret、readiness/liveness probe、PDB、资源限额。验证：`kubectl get pods` 全部 Ready，probe 配置与 chart 一致。 — 进展（2026-11-25）：本机 kind 集群节点 Ready，但 `oa-data`/`oa-observability` 多数工作负载 `ImagePullBackOff`（13 个），无法复跑安装与探针检查；现状见 `docs/stages/evidence/stage-2-integration/kind-cluster-state.md`。 — 范围移交（2026-11-28）：由 `delivery-runtime-acceptance` 承接，不计入 Stage 2 退出判据。
- [x] 6.2 ArgoCD 同步：创建/更新 Application 并等待同步。验证：`Synced/Healthy` 状态截图或 CLI 输出入库。 — 进展（2026-11-25）：ArgoCD 控制器 Running，但 `argocd` 命名空间无 Application，无对象可验（`kind-cluster-state.md`）。 — 范围移交（2026-11-28）：由 `delivery-runtime-acceptance` 承接，不计入 Stage 2 退出判据。
- [x] 6.3 集群入口：验证 Envoy `oa-edge` 路由在集群内生效（public 200、受保护 401/200、未知 404、`/metrics` 不可达）。验证：命令输出入库；与 edge spec 行为一致。 — 进展（2026-11-25）：集群入口未复验（依赖 6.1 工作负载就绪，见 `kind-cluster-state.md`）。 — 范围移交（2026-11-28）：由 `delivery-runtime-acceptance` 承接，不计入 Stage 2 退出判据。
- [x] 6.4 备份恢复演练：按 `deploy/README.md` 备份 → 恢复。验证：恢复后 `/readyz` 与关键数据抽查通过，演练日志入库。 — 进展（2026-11-25）：未执行，依赖可拉取镜像环境（`kind-cluster-state.md`）。 — 范围移交（2026-11-28）：由 `delivery-runtime-acceptance` 承接，不计入 Stage 2 退出判据。
- [x] 6.5 镜像回滚演练：固定上一已验证 tag 回滚并重跑 smoke。验证：回滚后 `/healthz`、`/readyz`、核心 smoke 通过，记录 tag 与时间。 — 进展（2026-11-25）：未执行，依赖可拉取镜像环境（`kind-cluster-state.md`）。 — 范围移交（2026-11-28）：由 `delivery-runtime-acceptance` 承接，不计入 Stage 2 退出判据。

## 7. 全栈 E2E（跨 change）

- [x] 7.1 Compose 基础链路：执行 E2E-01（Keycloak 登录 → web → 工作台 → 对象读写 → health/readyz）与 E2E-08（对象 → action → pipeline → dataset → 查询/搜索组合链）。验证：脚本/步骤日志与截图入库，结果可复现。 — 进展（2026-11-25）：`fullstack-e2e` 工作流 `e2e-foundation` job（`scripts/smoke-foundation.sh`：health/readyz 与基础 API）在真实 runner 通过（run 45607781575）；**E2E-01 已全量通过**（`e2e-workbench`，16 passed/0 failed，run 45626955337 / job 139249580609，证据 `docs/stages/evidence/stage-2-integration/fullstack-e2e-7x.md` §2）；**E2E-08 已覆盖**：`e2e-chain` job（`scripts/fullstack-chain-e2e.sh`，16 passed/0 failed，run 45644525408 / job 见 `fullstack-e2e-7x.md` §6）覆盖对象创建→action(object.update+compensation)→搜索索引→dataset+branch+view→pipeline create/publish/run→搜索复核与 metrics 增量；同时修复 Kestra v2 Basic Auth 配置（D-09）。
- [x] 7.2 Compose 数据链路：执行 E2E-05（CDC → dataset → 查询）、E2E-06（Kestra+dbt → dataset）、E2E-07（Temporal 重试/补偿/审批）、E2E-04（outbox → Meilisearch → 搜索/重建）。验证：各场景日志与查询结果入库。 — 进展（2026-11-25）：`e2e-search-outbox`（E2E-04，隔离 Compose 73/73）、`e2e-actions`（E2E-07，7 断言）、`e2e-pipeline` 在真实 runner 通过。2026-11-28：E2E-06 本地 Compose **11/11**（`pipeline-dbt-7.2.md`，CI `e2e-pipeline-dbt` 待 runner）；E2E-05 本地 Compose **23/23**（负向 16/16 + 成功路径 5/5，`connector-runtime-7.2.md`）；E2E-04/E2E-07 继续由真实 runner 证据覆盖。run 45607781575。
- [x] 7.3 Compose 身份/授权：执行 E2E-02（edge 矩阵）、E2E-03（OpenFGA/Cedar allow/deny、跨租户、默认拒绝、审计）、E2E-12（安全负路径）。验证：状态码/审计记录/拒绝原因入库。 — 进展（2026-11-25）：E2E-03 的 OpenFGA/Cedar allow/deny、跨租户、暂停 503 已由 OSM 隔离 Compose E2E 73/73 与 `authz-integration` CI job 覆盖（证据见 `object-search/2026-11-24-object-search-verification.md`、主干 run 45543404516）；E2E-12 安全负路径与 E2E-02 edge 矩阵由 CI `edge`/`compose-smoke` job 覆盖；**已汇总并通过**：`e2e-identity`（E2E-02/03/12）12 passed/0 failed（run 45626955337 / job 139249580233，`EDGE_TEST_FAIL_CLOSED=1`），含缺失/伪造 JWT 401、伪造身份头 403、405、限流 429（68/200）、鉴权后端宕机 fail-closed 500；证据 `fullstack-e2e-7x.md` §3。
- [x] 7.4 Compose 可观测：执行 E2E-09（trace 贯通、RED/运行时指标、SLO 面板、告警触发、负载基线复跑）。验证：Jaeger trace JSON、Prometheus 查询结果、面板截图、基线结果入库。 — 进展（2026-11-25）：CI `observability` job 在真实 runner 通过（主干 run 45543404516）；Jaeger trace/RED 指标/SLO 面板/告警触发/负载基线复跑的完整证据仍缺（R12 partial）。进展（2026-11-25）：`e2e-observability` 在真实 runner 通过（run 45626955337 / job 139249580213）：1 metric series、1 trace、Perses 面板导入；SLO 截图/告警/负载基线仍缺。 — 进展（2026-11-28）：本地 Compose 补齐并入库（`observability-7.4.md`）：SLO 面板截图、promtool 告警规则测试 SUCCESS、运行时 12 条规则加载、负载基线 300 请求/20 并发 = 974.74 req/s、p95 60.899ms、0 失败。
- [x] 7.5 生产 IdP：**out of scope（2026-11-25 移交）**——S2-AC-10 已移出 Stage 2，E2E-11 由 `production-idp-staging` 4.1/4.2 承接；本项不计入 Stage 2 任务与退出判据。
- [x] 7.6 前端 E2E：运行 Stage 1 Playwright 套件；如 `action-temporal` 提供审批 UI，合并并运行其新增用例。验证：`cd web && pnpm test:e2e` 通过，截图/trace 入库。 — 完成（2026-11-28）：`cd web && pnpm test:e2e --reporter=list` **17 passed / 0 failed**（core-workbench 13 + approvals 4，9.5s；Playwright 1.65.1 / chromium / mock 模式），证据 `docs/stages/evidence/stage-2-integration/playwright-7.6.md` + `playwright-7.6.log`。
- [x] 7.7 kind 栈 E2E 子集：在 kind/k3d 上执行 E2E-01、E2E-02、E2E-10（交付）。验证：结果入库；未覆盖的组合在报告中标 `not covered`。 — 进展（2026-11-25）：未执行；kind 工作负载多数 `ImagePullBackOff`（`kind-cluster-state.md`）。 — 范围移交（2026-11-28）：kind/k3d 已移交 `delivery-runtime-acceptance`，不计入 Stage 2 退出判据。

## 8. 回归与质量门

- [x] 8.1 Go 质量门：`go build ./...`、`go test ./...`、`go test -race ./...`、`go vet ./...`、golangci-lint（与 CI 同版本）、`make license-check`。验证：全部通过，日志入库。— 完成（2026-11-25）：本地 `go build`/`go vet`/`go test -race` 全通过（`regression/go-gates.log`）、`make license-check` 通过（`regression/make-gates.log`）；golangci-lint 与 license 由 CI `build-test` job steps 7 Lint、9 License 覆盖（[run 45600592906](https://github.com/klb3713/OntoAxis/actions/runs/45600592906)）。
- [x] 8.2 前端质量门：`cd web && pnpm install --frozen-lockfile && pnpm check && pnpm test && pnpm build`。验证：全部通过，日志入库。— 2026-11-25 本地实跑 `INSTALL_OK/CHECK_OK/TEST_OK/BUILD_OK`，日志 `docs/stages/evidence/stage-2-integration/regression/web-gates.log`。
- [x] 8.3 契约门：`make proto-lint`、`make proto-breaking`、`make generate-check`、`make edge-generate-check`。验证：全部通过且工作区干净。— 进展（2026-11-25）：`make edge-generate-check` 通过；`make proto-lint`/`proto-breaking`/`generate-check` 本机无 buf、docker 回退镜像 `bufbuild/buf:1.75.0` 拉取失败（`registry-1.docker.io ... EOF`），见 `regression/make-gates.log`。— 结论（2026-11-28）：CI `proto` job 在真实 runner 执行同一组命令（`cd proto && buf lint`、`make generate-check`、`make proto-breaking`）并通过 [run 45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) 11/11；以 CI 结果替代本机缺 buf，工作区已 `git diff --check` 干净。
- [x] 8.4 脚本与配置门：`actionlint`、`shellcheck scripts/*.sh`、`docker compose config`。验证：无 error 级结果，日志入库。— 完成（2026-11-25）：CI `delivery-static` job 新增并跑通三步——`ShellCheck all repository scripts (error severity)`、`Compose config matrix (all profiles, static)`、`actionlint`，随主干/分支 CI 全绿（run [45600592906](https://github.com/klb3713/OntoAxis/actions/runs/45600592906) delivery-static success，steps 7/8/9）；本机无这些工具，以 CI 为准。
- [x] 8.5 OpenSpec 门：全部前置 change 归档后运行 `openspec validate --all --strict`。验证：输出 0 failed（若仍有在途 change 未归档，不得判定通过）。— 2026-11-25 `openspec validate --all --strict` 19/19 通过，但仍有在途 change（含 OSM 未归档），故当时不勾。 — 2026-11-28：前置 8 change 全部归档、connector-runtime 残留已清理，`openspec validate --all --strict`=22 passed/0 failed，满足本门；仍在途的是本 change 自身与范围外/新建 change（`delivery-runtime-acceptance`、`production-*`），非本门前置。
- [x] 8.6 CI 门：在真实 runner 上运行完整 CI（build-test/proto/frontend/docker/observability/compose-smoke/edge）。验证：全绿并记录 run URL 与 job 列表；本地通过不能替代此任务。— 2026-11-25 主干 run [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) 11/11 全绿（build-test、proto、secret-scan、delivery-static、search-integration、authz-integration、edge、compose-smoke、frontend、docker、observability）。

## 9. 证据与 Stage 2 报告

- [x] 9.1 建立证据索引 `docs/stages/evidence/stage-2-integration.md` 与原始目录 `docs/stages/evidence/stage-2-integration/`：按 AC 组织，每行含日期/环境/commit/命令/结果/路径。验证：索引中的每个路径真实存在，抽查 3 条可复跑。— 2026-11-25 已建立索引（S2-AC-01…11 映射 + 质量门结果 + 未完成/阻塞清单），并新增 `object-search/`、`regression/`、`d07-port-conflict.md`、`security-check.md`。
- [x] 9.2 证据边界与安全检查：确认无 token/密码/生产连接串；声明合成夹具与本地环境边界；运行仓库 secrets/license 检查并记录。验证：检查命令输出入库，证据文件无敏感串。— 2026-11-25 gitleaks v8.24.3 + `make license-check`（通过）；22 条命中中 12 文件未跟踪、唯一已跟踪为 `bootstrap.sh:199` `condition=Available` 误报，CI secret-scan 绿；见 `docs/stages/evidence/stage-2-integration/security-check.md`。
- [x] 9.3 起草 `docs/stages/stage-2-exit-report.md`：按 design 的 10 节结构与 S2-AC-01…11 证据表编写。验证：未执行/未通过项如实标 `not passed`；报告状态为 `exit candidate pending owner confirmation`。— 2026-11-25 已起草（10 节结构，S2-AC-05 标 not passed、E2E 多数 not covered，状态 `exit candidate pending owner confirmation`）。
- [x] 9.4 文档同步：更新 `ARCHITECTURE.md` 15.x、`docs/stages/stage-2-change-map.md` 状态、`README.md`、`docs/getting-started/local-development.md`、`docs/architecture/services-and-ports.md` 等（经各 owner 同意）。验证：链接可达、无过期“计划中”表述、命令可照抄执行。 — 2026-11-25 完成：`services-and-ports.md` 补登 5 项端口；`change-map` 追加 2026-11-25 状态并登记 G2 pass；`ARCHITECTURE.md` 更新 15.3 授权接线表述并新增 15.4 Stage 2 当前进展；`README.md` 端口覆盖变量补 `ONTOAXIS_SEARCH_WORKER_PORT`；`local-development.md` 新增 Stage 2 追加 profile 端口表（Meilisearch/search worker/source-postgres/Redpanda/Kafka Connect/Kestra/Temporal UI）。
- [x] 9.5 自检 traceability：核对 S2-AC-01/AC-11 的每条证据路径与状态，S2-AC-02…10 的组合复验记录齐全。验证：traceability 中无未解释的 `planned`；每条 `passed` 有可点开的证据。 — 进展（2026-11-25）：`traceability.md` 已追加「状态更新（2026-11-25）」表逐 AC 对齐当前状态与证据；主表 `planned` 保留为计划列，实际状态以更新表为准；S2-AC-05 仍 `partial/not passed`。 — 进展（2026-11-28）：已追加「状态更新（2026-11-28，全栈 E2E 同步）」表；主表仍为 `planned` 的行均已被更新表解释，每条 `passed` 均有可点开证据路径，无未解释的 `planned`，自检完成。

## 10. 评审与收口

- [x] 10.1 提交评审：将 artifacts、证据索引、Stage 2 报告草稿交负责人评审。验证：存在明确评审结论记录（通过/有条件通过/退回），非自动批准。 — 进展（2026-11-25）：已提交材料包 `openspec/changes/stage-2-integration/review-packet.md`，待负责人记录评审结论（未自动批准）。 — 进展（2026-11-28）：材料包更新至 2026-11-28 版本；负责人 klb3713 当日确认 **G3 = pass、接受 Compose-only 退出、批准归档**，评审结论已记录（见 `docs/stages/evidence/stage-2-g3-gate-record.md`）。
- [x] 10.2 确认未自动执行动作：历史核对（2026-11-25）：本 change 在途、Stage 2 brief 仍 `in_progress`；本轮 commit/push 均在负责人 m02140 授权范围内（分支 + 草稿 PR），未改 brief 状态、未归档本 change、未合并主干。— 2026-11-28 负责人确认后，归档与 Stage 退出按授权执行（非自动）。
- [x] 10.3 记录残余风险与未决项：把 open questions、未覆盖场景、带 caveat 的 AC 写入报告第 8 节。验证：报告与 traceability 的风险条目一致。— 2026-11-25 报告 §7 已列 6 条偏差/风险（G2 未登记、kind/E2E 未复跑、工具缺口、证据边界、业务阈值 pending），与 traceability 一致。

## 11. 接收自 object-search-meilisearch 的交接项（2026-11-24）

来源：`openspec/changes/archive/2026-11-25-object-search-meilisearch/handoff/stage-2-integration.md`（证据源文件已备，`docs/stages/evidence/` 归本 change 收录）。本 change 2.7 已完成 object-search 的合并；以下为证据收口与边界声明。

- [x] 11.1 收录证据源文件到 `docs/stages/evidence/`（清单见交接文件 §1）：集成记录、隔离验证（后端 12 项 / 原生 HTTP 68/68 / 隔离 Compose E2E 73/73）、CI 同参本地日志、手册复现记录；按 AC 组织并写索引。验证：索引内每个路径真实存在，抽查可复跑。回填 object-search-meilisearch 8.4。— 2026-11-25 已收录到 `docs/stages/evidence/stage-2-integration/object-search/`（5 个源文件），索引 §2；OSM 8.4 已回填。
- [x] 11.2 回填 object-search-meilisearch 8.2 的 GitHub Actions `search-integration` run 链接（本机无 runner，本地同参 exit 0、0 SKIP）。验证：CI job 绿并记录 run URL。— [run 45405474349 / job 138484907178](https://github.com/klb3713/OntoAxis/actions/runs/45405474349/job/138484907178) success；主干最终全绿 [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516)。
- [x] 11.3 记录 object-search 9.1/9.2/9.3 评审签字（评审人 klb3713，2026-11-24）到 Stage 证据索引，附各材料 §4/§5/§6 链接。— 2026-11-25 已写入索引 §2（review-ops-runbook §5、security-data-governance-review §4/§6、review-wave2-admission §5/§6，评审人 klb3713，2026-11-24）。
- [x] 11.4 证据边界声明：明确区分“隔离本地 + 合成数据验证”与“CI/生产验收”，不得把本地通过写成 CI 或生产通过；业务质量阈值仍 `pending-owner-signoff`。— 2026-11-25 已写入索引 §2/§4 与 `security-check.md`。
- 关联既有项：本 change 5.4（Wave 1/2 运行时 profile 含 Meilisearch）、7.2（E2E-04 outbox→Meilisearch→搜索/重建）可复用上述证据。

## Workflow follow-up

- 本 change 的归档需在评审通过、证据可复核、traceability 一致、`openspec validate --all --strict` 通过后，单独获得授权再执行；不在本 change 的 tracked task 内自动完成。
- Stage 2 退出需所有 change 归档、Stage AC 证据齐全、报告完成、文档同步、负责人确认；本 change 只产出退出候选，不自动宣布退出。
- 提交/合并方式遵循仓库 review 流程；不自动 commit、不自动 push、不自动归档。
