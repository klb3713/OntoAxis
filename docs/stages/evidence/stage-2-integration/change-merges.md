# Stage 2 change 合并记录（2026-11-23）

| task | change | 复跑/证据 | 结果 |
|---|---|---|---|
| 2.1 | runtime-observability | `scripts/observability-smoke.sh` + `compose-smoke.md` | PASS（1 metric series, 1 trace, Perses dashboard） |
| 2.2 | edge-gateway | `make edge-generate-check` + `EDGE_TEST_FAIL_CLOSED=1 scripts/edge-smoke.sh` + `compose-smoke.md` | PASS（12/0，401/403/404/405/429/5xx） |
| 2.3 | authorization-openfga-cedar | `go test -tags=integration -run TestAuthorizerWithRealOpenFGA ./internal/platform/authz/` + authz evidence | PASS（真实 OpenFGA allow/deny/跨租户） |
| 2.4 | connector-runtime | 提交 `8f3e533`；`docs/stages/evidence/stage-2-connector-runtime/README.md` + `openspec/changes/connector-runtime/evidence/2026-11-23-cdc-e2e-three-way.md` | PASS（CDC 三方行数 5=5=5，job `succeeded`；负向 18/18） |
| 2.6 | action-temporal | `scripts/actions-temporal-smoke.sh` 7/7 + `pnpm check/test/build` | PASS（对象写入/补偿/审批/超时/重试耗尽） |

待合并：
- 2.5 pipeline-kestra-dbt：需 Kestra/dbt profile 合并与 E2E。
- 2.7 object-search-meilisearch：需 Meilisearch profile/worker 合并与索引一致性 E2E。
- 2.8 production-delivery：静态 chart/render 检查通过；kind/集群安装未执行。
- 2.9 跨 change 缺陷清单：已登记 edge core authz env 装配缺陷并修复（见 `compose-smoke.md`）。
