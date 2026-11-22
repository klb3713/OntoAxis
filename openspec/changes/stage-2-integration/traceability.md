# Traceability

> 状态：planning（2026-11-22 创建）。本文件中的“计划证据”尚未产生；`状态` 列为 `planned` 的条目在 `tasks.md` 对应任务完成并写入 `docs/stages/evidence/` 之前，不得判定为通过。
> 说明：本 change 设置 `skip_specs: true`，不新增或修改 capability，因此不存在 delta requirement 映射；行为契约保持 `openspec/specs/` 既有 13 个 capability 不变。本表把 Stage AC 映射到任务与计划证据。

## Stage AC 映射

| Stage AC | 验收内容 | 对应任务 | 计划证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-01 | Stage 1 全量回归通过：9 个模块边界不变；`go test ./...`、前端 check/build/test、`buf lint`/breaking、Compose smoke、Stage 1 Playwright 均保持绿色 | 4.1、4.2、8.1、8.2、8.3、8.6、7.1、7.6 | `docs/stages/evidence/stage-2-integration/regression/`（go-test.log、go-test-race.log、golangci-lint.log、license-check.log、web-check.log、web-test.log、web-build.log、proto-lint.log、proto-breaking.log、generate-check.log、edge-generate-check.log、playwright/ 截图与 trace、compose-smoke.log、ci/run.txt）；`internal/app/modules_test.go` 9 模块顺序断言输出 | planned |
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
| S2-AC-02 | `edge-gateway`（已归档） | 2.2、5.2、7.3、7.7 | edge smoke 矩阵日志（200/401/403/404/405/429/fail-closed）、Envoy kind 输出 | planned |
| S2-AC-03 | `authorization-openfga-cedar` | 2.3、7.3 | allow/deny、跨租户拒绝、默认拒绝、审计记录 | planned |
| S2-AC-04 | `action-temporal` | 2.6、7.2 | 重试/补偿/审批/状态回写日志 | planned |
| S2-AC-05 | `pipeline-kestra-dbt` | 2.5、7.2 | Flow 发布/运行/失败重试/dataset 产出日志 | planned |
| S2-AC-06 | `connector-runtime` | 2.4、7.2 | CDC → dataset → 查询日志 | planned |
| S2-AC-07 | `object-search-meilisearch` | 2.7、7.2 | 索引一致性、全量重建日志 | planned |
| S2-AC-08 | `production-delivery` | 2.8、6.1–6.5、7.7 | Helm/ArgoCD 状态、probes/PDB/Secret、备份恢复与回滚演练日志 | planned |
| S2-AC-09 | `runtime-observability`（已归档） | 1.3、5.3、7.4 | Jaeger trace JSON、Prometheus 查询、SLO 面板截图、告警触发、负载基线 | planned |
| S2-AC-10 | `production-delivery` | 7.5 | 非本地 OIDC 登录/JWKS/claims 证据（不含密钥） | planned |

## 已知缺口与未决项

- 前置 6 个 change（`authorization-openfga-cedar`、`connector-runtime`、`pipeline-kestra-dbt`、`action-temporal`、`object-search-meilisearch`、`production-delivery`）目前均未归档、未实施（起草本 change 时多数仍只有脚手架），其 artifacts 由各自 change 推进；本 change 的依赖、任务与证据可能随其 proposal/design 定稿微调，但 S2-AC-01/AC-11 的映射与门禁不变。
- Wave 3 准入（G2 `pass`）当前未记录，本 change 处于 planning/blocked，所有状态保持 `planned`。
- 未决项见 `design.md` 的 Open Questions（审批 UI、kind/k3d 主栈、Compose 资源、生产 IdP 环境、负载基线阈值）；定稿后需回填本表对应证据路径。
- 证据边界：本地/合成环境不能证明真实数据验收或生产 SLO；未覆盖组合必须标 `not covered`，不得以“计划”充“通过”。
