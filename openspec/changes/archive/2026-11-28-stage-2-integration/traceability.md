# Traceability

> 状态：planning（2026-11-22 创建）。本文件中的“计划证据”尚未产生；`状态` 列为 `planned` 的条目在 `tasks.md` 对应任务完成并写入 `docs/stages/evidence/` 之前，不得判定为通过。
> 说明：本 change 设置 `skip_specs: true`，不新增或修改 capability，因此不存在 delta requirement 映射；行为契约保持 `openspec/specs/` 既有 13 个 capability 不变。本表把 Stage AC 映射到任务与计划证据。

## Stage AC 映射

| Stage AC | 验收内容 | 对应任务 | 计划证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-01 | Stage 1 全量回归通过：9 个模块边界不变；`go test ./...`、前端 check/build/test、`buf lint`/breaking、Compose smoke、Stage 1 Playwright 均保持绿色 | 4.1、4.2、8.1、8.2、8.3、8.6、7.1、7.6 | `docs/stages/evidence/stage-2-integration/regression/`（go-test.log、go-test-race.log、golangci-lint.log、license-check.log、web-check.log、web-test.log、web-build.log、proto-lint.log、proto-breaking.log、generate-check.log、edge-generate-check.log、playwright-7.6.md、playwright-7.6.log、compose-smoke.log、ci/run.txt）；`internal/app/modules_test.go` 9 模块顺序断言输出 | planned |
| S2-AC-11 | Stage 2 证据与报告完成：全部 change 归档、`openspec validate --all --strict` 通过、CI 全绿、Stage 退出获负责人确认 | 1.1、8.5、8.6、9.1–9.4、10.1–10.3 | `openspec validate --all --strict` 输出；CI run URL 与 job 列表；`docs/stages/evidence/stage-2-integration.md` 索引；`docs/stages/stage-2-exit-report.md`；评审确认记录 | planned |

### S2-AC-11 拆分核对

| 子条件 | 对应任务 | 计划证据 | 状态 |
| --- | --- | --- | --- |
| 8 个前置 change + 本 change 全部归档 | 1.1、10.1、10.2 | `openspec list` 输出、change map §4 门禁记录、归档目录清单 | planned |
| `openspec validate --all --strict` 通过 | 8.5 | 命令输出（0 failed） | planned |
| CI 全绿 | 8.6 | CI run URL、七组 job 结果 | planned |
| Stage 2 报告完成 | 9.3 | `docs/stages/stage-2-exit-report.md`（10 节结构） | planned |
| 文档同步 | 9.4 | `ARCHITECTURE.md` 15.x、change map、README、local-development、services-and-ports 的 diff 与链接检查 | planned |
| 负责人确认 | 10.1 | 明确评审结论记录（非自动批准） | planned |

## 支撑 AC 的组合复验

S2-AC-02…10 的主证据与验收归属各自 change；本 change 只做跨 change 组合复验并在报告中记录，不替代其证据。

| Stage AC | 归属 change | 本 change 组合复验任务 | 计划证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-02 | `edge-gateway`（已归档） | 2.2、5.2、7.3、7.7 | edge smoke 矩阵日志（`fullstack-e2e-7x.md` §3：200/401/403/404/405/429/fail-closed，`e2e-identity` 12/12，run 45626955337）；Envoy kind 输出（7.7 未执行） | partial（Compose 矩阵通过，kind 未跑） |
| S2-AC-03 | `authorization-openfga-cedar` | 2.3、7.3 | allow/deny、跨租户拒绝、默认拒绝、审计记录（OSM 隔离 E2E 73/73 + `e2e-identity` 12/12；`fullstack-e2e-7x.md` §3） | partial（审计记录未单列） |
| S2-AC-04 | `action-temporal` | 2.6、7.2 | E2E-07 重试/补偿/审批（`e2e-actions` 7 断言，run 45626955337）；`fullstack-e2e-7x.md` §4 | partial（E2E-07 组合链已过） |
| S2-AC-05 | `pipeline-kestra-dbt` | 2.5、7.2 | `pipeline-dbt-7.2.md`：E2E-06 本地 Compose 11/11（publish→run succeeded→Trino answer=42）；2.5 完成 | passed（本地 Compose） |
| S2-AC-06 | `connector-runtime` | 2.4、7.2 | `connector-runtime-7.2.md`：E2E-05 本地 Compose 23/23（负向 16/16 + CDC→Iceberg→Trino 5 行一致）；CI `e2e-connector` opt-in | passed（本地 Compose） |
| S2-AC-07 | `object-search-meilisearch` | 2.7、7.2 | 索引一致性、全量重建（隔离 E2E 73/73 + 全栈 E2E-04 `e2e-search-outbox`） | partial（E2E-04 全栈通过） |
| S2-AC-08 | `production-delivery` | 2.8、6.1–6.5、7.7 | **已整条移出 Stage 2（2026-11-28），由 `delivery-runtime-acceptance` 承接**；归属方 kind 证据保留 | out of scope（整条移出） |
| S2-AC-09 | `runtime-observability`（已归档） | 1.3、5.3、7.4 | `e2e-observability` + `observability-7.4.md`：trace/RED/Perses/SLO 面板/告警规则/12 规则加载/负载基线 | passed（本地 Compose） |
| S2-AC-10 | `production-idp-staging` | 4.1 | 非本地 OIDC 登录/JWKS/claims 证据（不含密钥） | out of scope（2026-11-25 任务移交 `production-idp-staging` 4.1/4.2） |

## 已知缺口与未决项

- 前置 6 个 change（`authorization-openfga-cedar`、`connector-runtime`、`pipeline-kestra-dbt`、`action-temporal`、`object-search-meilisearch`、`production-delivery`）起草时尚未归档；截至 2026-11-25 均已归档（`openspec/changes/archive/`）。
- Wave 3 准入（G2 `pass`）已于 2026-11-25 由负责人 klb3713 登记（见 change-map §4 与 `docs/stages/evidence/stage-2-g2-gate-record.md`）。
- 未决项见 `design.md` 的 Open Questions（审批 UI、kind/k3d 主栈、Compose 资源、生产 IdP 环境、负载基线阈值）；定稿后需回填本表对应证据路径。
- 证据边界：本地/合成环境不能证明真实数据验收或生产 SLO；未覆盖组合必须标 `not covered`，不得以“计划”充“通过”。

## 状态更新（2026-11-25，历史快照）

| Stage AC | 当前状态 | 说明 |
| --- | --- | --- |
| S2-AC-01 | partial | Go 全门通过、前端 check/test/build 通过、edge-gen 通过；golangci-lint/buf/Playwright 未跑（工具缺失）。2026-11-25 追加：E2E-01/02/03/07/08 组合链与 edge 矩阵在真实 runner 通过（`fullstack-e2e-7x.md` §2/§3/§6），Playwright 7.6 已于 2026-11-28 通过（17/17）；仍缺 buf 本地工具，见 `docs/stages/evidence/stage-2-integration.md` §3 |
| S2-AC-02 | partial | Compose edge smoke 通过；kind 入口未执行 |
| S2-AC-03 | partial | 本地 allow/403/401/503 证据（object-search 原生 HTTP）；E2E-03 全矩阵未执行 |
| S2-AC-04 | partial | 组合运行时已有；CI smoke 7/7（run 45570760338，`wave1-stack-matrix.md`）；E2E-07 未执行 |
| S2-AC-05 | passed（本地 Compose） | profile 预检 + CI Kestra ACTIVE/Trino 200；E2E-06 本地 11/11（`pipeline-dbt-7.2.md`） |
| S2-AC-06 | passed（本地 Compose） | E2E-05 本地 23/23（`connector-runtime-7.2.md`）；CI `e2e-connector` opt-in 备注 |
| S2-AC-07 | partial | 真实集成 + 隔离 Compose E2E 73/73 + 真实 CI run 已收录；Compose 全栈 E2E-04 未执行 |
| S2-AC-08 | out of scope | 已于 2026-11-28 整条移出，`delivery-runtime-acceptance` 承接 |
| S2-AC-09 | passed（本地 Compose） | `observability-7.4.md`：SLO 面板/告警规则/负载基线 300/20 |
| S2-AC-10 | out of scope | 移出至 `production-idp-staging`（收缩口径） |
| S2-AC-11 | partial | 索引已建立（9.1）、CI 全绿（8.6）、退出报告草稿已起草（9.3，`docs/stages/stage-2-exit-report.md`，状态 exit candidate pending owner confirmation）；G2 记录与终局评审未完成 |


## 状态更新（2026-11-28，全栈 E2E 同步）

> 依据：`docs/stages/evidence/stage-2-integration/fullstack-e2e-7x.md`；run [45626955337](https://github.com/klb3713/OntoAxis/actions/runs/45626955337)（7.1/7.3/7.4 job 绿）与 [45644525408](https://github.com/klb3713/OntoAxis/actions/runs/45644525408)（E2E-08 `e2e-chain` 16/16）。仅同步证据状态，未改 Stage 状态、未归档。

| Stage AC | 当前状态 | 说明 |
| --- | --- | --- |
| S2-AC-01 | partial（近通过） | Go/前端/proto 门通过；E2E-01 16/16、E2E-07 7 断言、E2E-08 16/16 在真实 runner 通过；Playwright 7.6 已通过（17/17）；仅本地 buf 缺 |
| S2-AC-02 | partial | Compose edge 矩阵 12/12（`e2e-identity`）；kind 6.3 未跑 |
| S2-AC-03 | partial | allow/deny/跨租户/fail-closed 已过（OSM 73/73 + `e2e-identity` 12/12）；审计记录未单列 |
| S2-AC-04 | partial（近通过） | E2E-07 `e2e-actions` 7 断言通过 |
| S2-AC-05 | passed（本地 Compose） | 2.5 完成；E2E-06 dbt 产 dataset 本地 11/11（`pipeline-dbt-7.2.md`） |
| S2-AC-06 | passed（本地 Compose） | E2E-05（CDC）本地 23/23（`connector-runtime-7.2.md`）；CI opt-in |
| S2-AC-07 | partial（近通过） | 隔离 73/73 + 全栈 E2E-04 `e2e-search-outbox` 通过 |
| S2-AC-08 | out of scope | 已于 2026-11-28 整条移出至 `delivery-runtime-acceptance` |
| S2-AC-09 | passed（本地 Compose） | `e2e-observability` + `observability-7.4.md`（SLO/告警/基线补齐） |
| S2-AC-10 | out of scope | 移出至 `production-idp-staging`（收缩口径） |
| S2-AC-11 | partial | 索引与 CI 全绿已就绪；G3 与终局评审未完成；G2 已于 2026-11-25 登记 `pass` |

自检（9.5，2026-11-28）：主表仍为 `planned` 的行均已被「状态更新（2026-11-25）」与「状态更新（2026-11-28）」两表解释；每条 `passed` 均可点开对应证据路径；无未解释的 `planned`。S2-AC-05 已 `passed（本地 Compose）`（2.5 完成、E2E-06 本地 11/11，见 `pipeline-dbt-7.2.md`）；S2-AC-06 已 `passed（本地 Compose）`（E2E-05 本地 23/23，见 `connector-runtime-7.2.md`）。
