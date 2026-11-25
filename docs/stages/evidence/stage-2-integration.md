# Stage 2 Integration · 证据索引

> 状态：`in-progress`（2026-11-25 建立；逐项按“已有证据 / 待补 / 阻塞”标注，未通过项不写成通过）。
> 归属：`docs/stages/evidence/` 由本 change 收录（`docs/stages/stage-2-change-map.md` 行 123）。原始目录：`docs/stages/evidence/stage-2-integration/`。
> 基线：`docs/stages/evidence/stage-2-integration/baseline/baseline.md`（commit `a04ab4c`）；本轮验证基线 commit `1de309d`（主干 HEAD）。

## 1. S2-AC 证据映射

| Stage AC | 对应任务 | 证据 | 状态 |
| --- | --- | --- | --- |
| S2-AC-01 Stage 1 回归（Go/前端/proto/Compose/Playwright） | 4.1、4.2、8.1、8.2、8.3、8.6、7.1、7.6 | `stage-2-integration/regression/`（go build/vet/test/race）、`stage-2-integration/compose-smoke.md`；前端与 proto 见 §3 缺口 | **partial**（Go 通过；前端/proto/Playwright 未全跑） |
| S2-AC-02 edge-gateway | 2.2、5.2、7.3、7.7 | `stage-2-integration/compose-smoke.md`、`stage-2-edge-gateway.md`、`stage-2-edge-gateway/`；kind 入口见 §3 缺口 | **partial**（Compose edge smoke 通过；kind 未执行） |
| S2-AC-03 authorization | 2.3、7.3 | `stage-2-integration/middleware-trust.md`、`stage-2-idp-security-review.md`；allow/deny 集成见 object-search 原生 HTTP 证据 | **partial**（本地 allow/403/401/503 有；E2E-03 全矩阵未执行） |
| S2-AC-04 action-temporal | 2.6、7.2 | `stage-2-action-temporal.md`、`stage-2-integration/wave1-stack-matrix.md`（CI smoke 7/7）；组合数据链 E2E-07 见 §3 缺口 | **partial** |
| S2-AC-05 pipeline-kestra-dbt | 2.5、7.2 | `stage-2-integration/pipeline-profile.md`、`stage-2-integration/wave1-stack-matrix.md`（CI Kestra ACTIVE + Trino 200）、`stage-2-pipeline-kestra-dbt/` | **partial**（profile 预检与 CI 启动已过；2.5 合并验证与 E2E-06 未完成） |
| S2-AC-06 connector-runtime | 2.4、7.2 | `stage-2-integration/connector-profile.md`、`stage-2-integration/wave1-stack-matrix.md`（CI Connect 18089 + 插件 pass=5/fail=0）、`stage-2-connector-runtime/` | **partial**（CDC 运行；E2E-05 未执行） |
| S2-AC-07 object-search-meilisearch | 2.7、7.2 | `stage-2-integration/object-search/`（见 §2）、`stage-2-integration/search-profile.md` | **partial**（真实集成 + 隔离 E2E 73/73；Compose 全栈 E2E-04 见 §3） |
| S2-AC-08 production-delivery | 2.8、6.1–6.5、7.7 | `stage-2-production-delivery-kind.md`、`stage-2-production-delivery.md` §17–§21 | **partial**（归属方 kind 证据已存在；本 change 6.x 未复跑） |
| S2-AC-09 runtime-observability | 1.3、5.3、7.4 | `stage-2-integration/compose-smoke.md`；观测 smoke 见归档证据 | **partial**（E2E-09 未执行） |
| S2-AC-10 生产 IdP | —（7.5 已于 2026-11-25 移交 `production-idp-staging` 4.1/4.2） | 已移出 Stage 2（`production-idp-staging`），本地合成 IdP 见 `stage-2-idp-security-review.md` | **out of scope（按收缩口径）** |
| S2-AC-11 证据与报告 | 1.1、8.5、8.6、9.1–9.4、10.1–10.3 | 本索引；CI run [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) | **partial**（G3 与终局评审未完成；G2 已登记 pass，2026-11-25） |

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
| 5.4/7.x 全栈分组 E2E（CI） | `.github/workflows/fullstack-e2e.yml` | **4/4 job 绿**：[run 45607781575](https://github.com/klb3713/OntoAxis/actions/runs/45607781575)（foundation/search-outbox/actions/pipeline）；connector 默认跳过（`vars.ENABLE_CONNECTOR_E2E=true` 开启，Iceberg 插件扫描 >45 分钟） | 本行 |
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
| 2.5 | pipeline-kestra-dbt 合并验证未完成（E2E-06） |
| 5.4 | 配置级已过（D-07 修复）；分组 `up -d` 启动矩阵已在 CI 真实 runner 通过（run 45570760338，见 `wave1-stack-matrix.md`）；全量同栈一次性启动未执行 |
| 6.1–6.5 | kind 交付层复跑未执行：集群节点 Ready 但 `oa-data`/`oa-observability` 多数 `ImagePullBackOff`（13）、ArgoCD 无 Application，见 `stage-2-integration/kind-cluster-state.md` |
| 7.1–7.7 | 部分覆盖：`fullstack-e2e` run 45607781575 的 foundation/search-outbox/actions/pipeline 四组在真实 runner 通过（E2E-04/07 与 Kestra+Trino 启动）；E2E-01/02/03/05/06/08/09/12 的完整断言与 `e2e-connector`（默认关闭）未合成一页证据；7.7 kind 受镜像/ArgoCD 阻塞（`kind-cluster-state.md`） |
| 8.1/8.2/8.3/8.4 部分 | 工具缺失或未跑（见 §3） |
| 8.5 | 仍有在途 change |
| 9.3/9.4/9.5、10.x | 依赖上述完成与评审 |
