# Stage 2 退出报告（退出候选，待负责人确认）

状态：`exit candidate pending owner confirmation` —— **不是** `exited`，Stage 2 仍为 `in_progress`；本报告由 `stage-2-integration` 起草，退出需负责人确认（本 change 不自动批准、不自动归档、不自动宣布退出）。
日期：2026-11-25
依据：`docs/stages/stage-2-ontoaxis-runtime-brief.md`、`docs/stages/stage-2-change-map.md`、`docs/stages/evidence/stage-2-integration.md`

## 1. Stage 范围与波次

- Stage：Stage 2 "OntoAxis Runtime 集成"
- Wave 0（已归档）：`runtime-observability`、`edge-gateway`
- Wave 1：`authorization-openfga-cedar`、`connector-runtime`、`pipeline-kestra-dbt`、`action-temporal`
- Wave 2：`object-search-meilisearch`、`production-delivery`
- Wave 3（本 change）：`stage-2-integration` —— 准入/基线、按序合并、组合根装配、Compose 与 kind 集成、全栈 E2E、质量门、证据与报告

## 2. Change 清单与归档状态

| Change | 波次 | 归档状态 |
| --- | --- | --- |
| runtime-observability | W0 | archived（2026-11-22） |
| edge-gateway | W0 | archived |
| authorization-openfga-cedar | W1 | archived（2026-11-23） |
| connector-runtime | W1 | archived（2026-11-23） |
| pipeline-kestra-dbt | W1 | archived（2026-11-23） |
| action-temporal | W1 | archived（2026-11-23） |
| object-search-meilisearch | W2 | archived（2026-11-25；`openspec/changes/archive/2026-11-25-object-search-meilisearch`，spec 写入 `openspec/specs/object-search/spec.md`） |
| production-delivery | W2 | archived（2026-11-24） |
| stage-2-integration | W3 | **在途（本 change，未归档）** |

范围外在途 change（不属 Stage 2 退出判据，需在 1.1 记录排除理由）：`production-idp-staging`、`production-delivery-hardening`、`jaeger-v2`。

## 3. Stage AC 证据表（S2-AC-01…11）

| AC | 证据 | 状态 |
| --- | --- | --- |
| S2-AC-01 Stage 1 回归（Go/前端/proto/Compose/Playwright） | Go build/vet/test/race 通过（`stage-2-integration/regression/go-gates.log`）；前端 check/test/build 通过（`web-gates.log`）；proto/generate 因本机无 buf 未执行（CI `proto` 绿）；Playwright 未全跑 | **passed-with-caveat** |
| S2-AC-02 edge-gateway | Compose edge smoke 通过（`stage-2-integration/compose-smoke.md`）；kind 入口未执行（6.3） | **passed-with-caveat** |
| S2-AC-03 authorization | 本地 allow/403/401/503 证据（object-search 原生 HTTP）、middleware 信任（`middleware-trust.md`）；E2E-03 全矩阵未执行 | **passed-with-caveat** |
| S2-AC-04 action-temporal | 合并与集成测试（2.6）；CI smoke 7/7（`wave1-stack-matrix.md`）；E2E-07 组合链未执行 | **passed-with-caveat** |
| S2-AC-05 pipeline-kestra-dbt | profile 预检（`pipeline-profile.md`）；CI Kestra ACTIVE + Trino 200（`wave1-stack-matrix.md`）；2.5 合并验证与 E2E-06 未完成 | **not passed** |
| S2-AC-06 connector-runtime | CDC 运行（`connector-profile.md`）；CI Connect 18089 + 插件 pass=5/fail=0（`wave1-stack-matrix.md`）；E2E-05 未执行 | **passed-with-caveat** |
| S2-AC-07 object-search-meilisearch | 真实 PG16+Meili 集成 12 项、原生 HTTP 68/68、隔离 Compose E2E 73/73、CI `search-integration` 绿（run 45543404516 / 45405474349） | **passed-with-caveat**（Compose 全栈 E2E-04 见 §4 缺口） |
| S2-AC-08 production-delivery | 归属方 kind 证据（`stage-2-production-delivery-kind.md`）；本 change 6.x 未复跑 | **passed-with-caveat**（kind 复跑未完成） |
| S2-AC-09 runtime-observability | Compose observability smoke 通过（5.3）；E2E-09（trace/SLO/告警）未执行 | **passed-with-caveat** |
| S2-AC-10 生产 IdP | 已移出 Stage 2（`production-idp-staging`），本地合成 IdP 仅技术证据 | **out of scope（收缩口径）** |
| S2-AC-11 证据与报告 | 证据索引（`docs/stages/evidence/stage-2-integration.md`）、CI 全绿 [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516)、本报告 | **partial**（G2/G3 记录、终局评审未完成） |

## 4. 质量门结果

| 门 | 结果 |
| --- | --- |
| Go build/vet/test/race | 通过（`regression/go-gates.log`） |
| golangci-lint | 本机无此工具，未执行（CI `build-test` 覆盖） |
| license | `make license-check` LICENSE_OK（`regression/make-gates.log`） |
| proto lint/breaking/generate-check | 本机无 buf，未执行；CI `proto` 绿 |
| edge-generate-check | 通过（`regression/make-gates.log`） |
| 前端 check/test/build | 通过（`regression/web-gates.log`） |
| Playwright（7.6） | **未执行** |
| Compose config（全 profile） | 通过，16 服务无重复宿主端口（`d07-port-conflict.md`） |
| Compose 启动矩阵（Wave 1） | 通过：CI run [45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) 3/3（`wave1-stack-matrix.md`） |
| actionlint/shellcheck | 本机无工具，未执行 |
| OpenSpec `validate --all --strict` | 通过（全部 change），但仍有范围外在途 change，不构成退出门通过 |
| 完整 CI | [45543404516](https://github.com/klb3713/OntoAxis/actions/runs/45543404516) 11/11 全绿 |

## 5. 全栈 E2E 矩阵

| 场景 | Compose | kind |
| --- | --- | --- |
| E2E-01 登录→工作台→对象读写 | not covered | not covered |
| E2E-02 edge 矩阵 | 部分（edge smoke） | not covered |
| E2E-03 授权 allow/deny/跨租户/审计 | 部分（本地） | not covered |
| E2E-04 outbox→Meili→搜索/重建 | 隔离项目 73/73（对象搜索），全栈组合 not covered | not covered |
| E2E-05 CDC→dataset→查询 | not covered | not covered |
| E2E-06 Kestra+dbt→dataset | not covered | not covered |
| E2E-07 Temporal 重试/补偿/审批 | 组件 smoke 7/7；端到端 not covered | not covered |
| E2E-08 对象→action→pipeline→dataset→查询/搜索 | not covered | not covered |
| E2E-09 可观测（trace/RED/SLO/告警） | 部分（observability smoke） | not covered |
| E2E-10 交付链路 | not covered | 归属方证据存在，未复跑 |
| E2E-11 生产 IdP | out of scope | out of scope |
| E2E-12 安全负路径 | 部分（object-search 原生 HTTP 401/403/503） | not covered |

## 6. 证据索引与可复核入口

- 索引：`docs/stages/evidence/stage-2-integration.md`（S2-AC 映射、object-search 交接 §2、质量门 §3、未完成 §5）
- 原始目录：`docs/stages/evidence/stage-2-integration/`（`regression/`、`object-search/`、`baseline/`、`defects/`、`d07-port-conflict.md`、`security-check.md`、`wave1-stack-matrix.md` 等）
- 密钥/许可检查：`security-check.md`（gitleaks v8.24.3；唯一已跟踪命中为 `deploy/kind/bootstrap.sh:199` `condition=Available` 误报，CI secret-scan 绿）

## 7. 偏差、风险与未覆盖项

1. **G2 未登记 pass**：条件 5 的 compose 启动部分已由真实 runner 矩阵（run 45570760338）提供证据，Helm/kind 启动（6.1–6.5）未完成；G2 记录待负责人确认。
2. **D-07 端口冲突**已修复（Connect 18086→18089、search worker 端口参数化），配置级与 CI 启动均验证；全量同栈一次性启动与资源占用未记录。
3. **kind 交付层（6.x）与全栈 E2E（7.x）未复跑**：本机 docker 无法拉取部分镜像，kind 集群未完成本轮复验。
4. **工具缺口**：golangci-lint/actionlint/shellcheck/buf 本机缺失，对应门以 CI 结果替代或标未执行。
5. **证据边界**：合成夹具、本地与 CI 环境不能证明真实数据验收或生产 SLO；未接入真实告警通道、未做生产压测。
6. **业务质量阈值**：object-search 业务质量门仍 `pending-owner-signoff`（工程回归门已签核，见归档 change evidence §3）。

## 8. 文档同步清单（9.4）

- `ARCHITECTURE.md` 15.x：待同步（状态表述需 owner 同意）
- `docs/stages/stage-2-change-map.md`：状态列仍为计划值，需在 G2 登记后更新
- `README.md`、`docs/getting-started/local-development.md`：待核对命令与端口
- `docs/architecture/services-and-ports.md`：已补登 Kafka Connect 18089、Redpanda 19092、source-postgres 15434、Meilisearch 17700、search worker 18088/`ONTOAXIS_SEARCH_WORKER_PORT`

## 9. 退出建议与待负责人确认项

- 当前**不建议**宣布 Stage 2 退出：G2 未登记、6.x/7.x 未完成、S2-AC-05 未通过、E2E 矩阵多数 `not covered`。
- 待负责人确认：G2 `pass` 记录（含条件 5 口径是否接受 “compose 已启动、helm 待 6.x”）、6.x/7.x 是否必须在本 Stage 完成、是否接受以 `passed-with-caveat` 收口 S2-AC-01/02/03/04/06/07/08/09。
- 本报告不自动退出；Stage 2 brief 状态保持 `in_progress`。
