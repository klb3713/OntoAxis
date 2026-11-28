# Stage 2 Integration · 证据索引

> 状态：`complete`（2026-11-28 Stage 2 已退出；证据索引随 `stage-2-integration` 归档）。
> 归属：`docs/stages/evidence/` 由本 change 收录（`docs/stages/stage-2-change-map.md` 行 123）。原始目录：`docs/stages/evidence/stage-2-integration/`。
> 基线：`docs/stages/evidence/stage-2-integration/baseline/baseline.md`（commit `a04ab4c`）；本轮验证基线 commit `1de309d`（主干 HEAD）。

## 1. S2-AC 证据映射

| Stage AC | 对应任务 | 证据 | 状态 |
| --- | --- | --- | --- |
| S2-AC-01 Stage 1 回归（Go/前端/proto/Compose/Playwright） | 4.1、4.2、8.1、8.2、8.3、8.6、7.1、7.6 | `stage-2-integration/regression/`（go build/vet/test/race）、`compose-smoke.md`、`fullstack-e2e-7x.md` §2/§6（E2E-01 16/16、E2E-07 7 断言、E2E-08 16/16）、`playwright-7.6.md`（17/17） | **passed-with-caveat**（仅本地 buf 缺；CI proto 绿） |
| S2-AC-02 edge-gateway | 2.2、5.2、7.3、7.7 | `compose-smoke.md`、`stage-2-edge-gateway.md`、`fullstack-e2e-7x.md` §3（E2E-02 12/12）；kind 入口 6.3 已移交 | **passed-with-caveat**（Compose 矩阵通过；kind 6.3 已移交 `delivery-runtime-acceptance`） |
| S2-AC-03 authorization | 2.3、7.3 | `middleware-trust.md`、`stage-2-idp-security-review.md`、OSM 73/73 + `e2e-identity` 12/12 | **passed-with-caveat**（allow/deny/跨租户/fail-closed 已过；审计记录未单列） |
| S2-AC-04 action-temporal | 2.6、7.2 | `stage-2-action-temporal.md`、`wave1-stack-matrix.md`（CI smoke 7/7）、`fullstack-e2e-7x.md` §4（E2E-07 `e2e-actions` 7 断言） | **passed-with-caveat**（E2E-07 组合链已过） |
| S2-AC-05 pipeline-kestra-dbt | 2.5、7.2 | `pipeline-profile.md`、`wave1-stack-matrix.md`、`stage-2-pipeline-kestra-dbt/`、`pipeline-dbt-7.2.md`（E2E-06 11/11 本地 Compose） | **passed（本地 Compose）** |
| S2-AC-06 connector-runtime | 2.4、7.2 | `connector-profile.md`、`wave1-stack-matrix.md`（CI Connect 18089 + 插件 pass=5/fail=0）、`connector-runtime-7.2.md`（E2E-05 本地 23/23） | **passed（本地 Compose）**（CI `e2e-connector` 仍 opt-in） |
| S2-AC-07 object-search-meilisearch | 2.7、7.2 | `object-search/`（见 §2）、`search-profile.md`、`fullstack-e2e-7x.md` §4（E2E-04 `e2e-search-outbox` 通过） | **passed-with-caveat**（真实集成 + 隔离 73/73 + 全栈 E2E-04 已过；业务质量阈值待 owner） |
| S2-AC-08 production-delivery | 2.8、6.1–6.5、7.7 | `stage-2-production-delivery-kind.md`、`stage-2-production-delivery.md` §17–§21 | **out of scope（2026-11-28 整条移出，由 `delivery-runtime-acceptance` 承接）** |
| S2-AC-09 runtime-observability | 1.3、5.3、7.4 | `compose-smoke.md`；`fullstack-e2e-7x.md` §4（`e2e-observability`）；`observability-7.4.md`（SLO 面板/告警规则/12 规则加载/负载基线） | **passed（本地 Compose）** |
| S2-AC-10 生产 IdP | —（7.5 已于 2026-11-25 移交 `production-idp-staging` 4.1/4.2） | 已移出 Stage 2（`production-idp-staging`），本地合成 IdP 见 `stage-2-idp-security-review.md` | **out of scope（按收缩口径）** |
| S2-AC-11 证据与报告 | 1.1、8.5、8.6、9.1–9.4、10.1–10.3 | 本索引；CI run [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516)、全栈 E2E run [45626955337](https://github.com/klb3713/OntoAxis/actions/runs/45626955337) / [45644525408](https://github.com/klb3713/OntoAxis/actions/runs/45644525408)、G3 记录 `stage-2-g3-gate-record.md` | **passed（2026-11-28 负责人确认 G3 = pass、批准退出）**；G2 已登记 pass，2026-11-25 |

## 2. object-search 交接（change 已于 2026-11-25 归档：`openspec/changes/archive/2026-11-25-object-search-meilisearch`）证据（11.1–11.4）

| 文件 | 覆盖 | 关键结果 |
| --- | --- | --- |
| `object-search/2026-11-23-object-search-integration.md` | OSM 1.4/4.1/5.1–5.7 | 真实 PG16+Meilisearch 集成、缺陷修复、`go.mod` 未改 |
| `object-search/2026-11-24-object-search-verification.md` | OSM 5.5/5.7/8.1 | 后端集成 12 项、原生 HTTP 68/68、隔离 Compose E2E 73/73、Docker Desktop 重启复跑 |
| `object-search/2026-11-24-search-integration-ci-local.log` | OSM 8.2 | CI 同参本地：exit 0、0 SKIP、60 通过项 |
| `object-search/2026-11-24-object-search-runbook-repro.md` + `.log` | OSM 9.1 | 手册 §3/§4/§5/§12 命令复现 |

- 11.2 CI run：`search-integration` [run 45405474349 / job 138484907178](https://github.com/klb3713/OntoAxis/actions/runs/45405474349/job/138484907178) success（2026-11-24）；主干最终全绿 [run 45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516)。
- 11.3 OSM 9.1/9.2/9.3 评审签字（评审人 klb3713，2026-11-24）：[review-ops-runbook.md §5](../../../openspec/changes/archive/2026-11-25-object-search-meilisearch/review-ops-runbook.md)、[security-data-governance-review.md §4/§6](../../../openspec/changes/archive/2026-11-25-object-search-meilisearch/security-data-governance-review.md)、[review-wave2-admission.md §5/§6](../../../openspec/changes/archive/2026-11-25-object-search-meilisearch/review-wave2-admission.md)。
- 11.4 边界声明：以上均为**隔离本地 + 合成数据**验证；不等同 CI 或生产验收。业务质量阈值 `pending-owner-signoff`。

## 3. 质量门与缺口（8.x/9.x）

| 项 | 命令 | 结果 | 路径 |
| --- | --- | --- | --- |
| 8.1 Go（本地） | `go build ./...`、`go vet ./...`、`go test -race -count=1 ./...` | 全部通过（exit 0） | `stage-2-integration/regression/go-gates.log` |
| 8.1 golangci-lint（CI） | 与 CI 同版本 | **通过**：CI `build-test` Lint step（[run 45600592906](https://github.com/klb3713/OntoAxis/actions/runs/45600592906)），License step 同绿 | 本行 |
| 8.1 license | `make license-check` | 通过（`LICENSE_OK`） | `stage-2-integration/regression/make-gates.log` |
| 8.2 前端 | `pnpm install --frozen-lockfile && pnpm check && pnpm test && pnpm build` | **全部通过**（`INSTALL_OK/CHECK_OK/TEST_OK/BUILD_OK`） | `stage-2-integration/regression/web-gates.log` |
| 8.3 proto（CI） | `make proto-lint`、`make proto-breaking` | **通过**：CI `proto` job 绿（[run 45600592906](https://github.com/klb3713/OntoAxis/actions/runs/45600592906)）；本机无 buf 仅影响本地复跑 | 本行 |
| 8.3 生成物 | `make edge-generate-check` | 通过（`edge route output is up to date`） | `stage-2-integration/regression/make-gates.log` |
| 8.3 生成物 | `make generate-check` | **未通过（环境）**：`make generate` 依赖 buf 镜像，镜像拉取失败 | 同上 |
| 8.4 脚本/配置（CI） | `shellcheck`（全仓脚本，error 级）、`actionlint`、全 profile `compose config -q` | **通过**：CI `delivery-static` 三步全绿（[run 45600592906](https://github.com/klb3713/OntoAxis/actions/runs/45600592906)，steps 7/8/9） | 本行 + `stage-2-integration/d07-port-conflict.md` |
| 8.6 CI | 真实 runner 完整 CI | **全绿 11/11**：[run 45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) | 本行 |
| 5.4 Wave1 启动矩阵（compose） | `.github/workflows/wave1-stack-smoke.yml` | **3/3 job 绿**：[run 45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338)（connector-runtime/pipeline-kestra/action-temporal） | `stage-2-integration/wave1-stack-matrix.md` |
| 5.4/7.x 全栈分组 E2E（CI） | `.github/workflows/fullstack-e2e.yml` | **8/8 job 绿**：[run 45626955337](https://github.com/klb3713/OntoAxis/actions/runs/45626955337)（foundation / workbench 16-0 / search-outbox 73-73 / actions 7 断言 / pipeline / identity 12-0 / observability）+ [run 45644525408](https://github.com/klb3713/OntoAxis/actions/runs/45644525408)（**E2E-08 `e2e-chain` 16-0**，对象→action→搜索→dataset→pipeline→搜索/指标）；connector 默认跳过（`vars.ENABLE_CONNECTOR_E2E=true`，Iceberg 插件扫描 >45 分钟）；详见 [`fullstack-e2e-7x.md`](stage-2-integration/fullstack-e2e-7x.md) | 本行 |
| 9.2 敏感串/许可 | gitleaks + `make license-check` | 见 `security-check.md`（1 条已跟踪误报 `bootstrap.sh:199`，CI secret-scan 绿） | `stage-2-integration/security-check.md`、`regression/gitleaks.json` |

## 4. 9.2 证据边界与安全检查

- 详见 `stage-2-integration/security-check.md`：gitleaks v8.24.3 扫描 22 条命中，12 个文件未跟踪（`deploy/kind/tls/key.pem`、`dist/delivery/**`），唯一已跟踪命中为 `deploy/kind/bootstrap.sh:199` 的 `condition=Available` 误报（CI secret-scan 绿；本地 CLI 与配置 `regexTarget` 语义差异已记录，未改配置）。
- `make license-check` 通过（`LICENSE_OK`）。
- 无 token/密码/生产连接串入库；`.env` 未提交。
- 边界：Stage 2 证据均为本地/合成数据验证；生产验收由交付环境执行，不把本地结果写成 CI/生产通过。

## 5. 未完成 / 阻塞（不得勾选）

| 任务 | 阻塞原因 |
| --- | --- |
| 1.1 | G2 已于 2026-11-25 登记 `pass`（compose 矩阵 [run 45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) + 归属方 helm/kind 证据；6.x 复跑属退出条件 4）；`openspec list` 另有 3 个范围外 change，排除理由写入退出报告 §2 |
| 2.5 | 已完成：E2E-06 本地 Compose 11/11（`pipeline-dbt-7.2.md`） |
| 5.4 | **已完成**：D-07 修复；CI 分组启动矩阵 [run 45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) 3/3；2026-11-28 本地 `oa-e2e05`（connector+search+authz，10 容器）与 `oa-e2e06`（pipeline+search）同时启动，端口/资源快照见 `wave1-stack-local.md` |
| 6.1–6.5 | **已整条移交 `delivery-runtime-acceptance`（2026-11-28）**：kind/ArgoCD 运行态验收由发布前 change 承接，不再计入 Stage 2 判据（见 change-map §12、brief S2-AC-08） |
| 7.1–7.7 | 7.1 满足（E2E-01 16/16、E2E-08 16/16 真实 runner）；7.2 满足（本地 Compose：E2E-04/07 真实 runner，E2E-05 23/23、E2E-06 11/11；CI e2e-connector 仍 opt-in）；7.3 满足（E2E-02/03/12 12/12）；7.4 满足（`observability-7.4.md`）；7.6 满足（17/17）；7.7 已移交 `delivery-runtime-acceptance`；汇总页 [`stage-2-integration/fullstack-e2e-7x.md`](stage-2-integration/fullstack-e2e-7x.md) |
| 8.1/8.2/8.3/8.4 | 8.1/8.2 CI `build-test` 全绿；8.3 buf 契约门由 CI `proto` job [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) 覆盖（buf lint/generate-check/proto-breaking）；8.4 CI 安全/负向矩阵通过 |
| 8.5 | **22 passed / 0 failed（2026-11-28）**；在途仅本 change 与 4 个范围外/新建 change |
| 9.3/9.4/9.5、10.x | 依赖上述完成与评审 |
