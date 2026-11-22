# Traceability

> 规划阶段记录；状态为 `planned` 的条目必须在实现并产生证据后改为 `verified`，不得提前标记通过。

## Stage AC 映射

| Stage AC | 行为/能力 | Requirement / Scenario | 计划证据 | 状态 |
| --- | --- | --- | --- | --- |
| S2-AC-09 | edge/core/worker trace 贯通；关键指标、SLO 面板、告警规则、负载基线可复现 | `telemetry-resource-propagation` / 入站请求生成 trace ID；`trace-span-coverage` / 入站到数据库的链路可关联 | `go-test.log` + `jaeger-trace.json` | verified |
| S2-AC-09 | 同上 | `http-red-metrics` / 指标按 route 聚合；`http-red-metrics` / 状态码与耗时被记录 | `go-test.log` + `prometheus-http-requests.json` | verified |
| S2-AC-09 | 同上 | `stage-2-slo` / 可用性错误预算可计算；`stage-2-slo` / 延迟 SLO 可区分读写 | `rules-test.log` | verified |
| S2-AC-09 | 同上 | `alerting-dashboard` / 高错误率触发告警；`alerting-dashboard` / 实例不可用触发告警 | `rules-test.log` + `perses-dashboard.png` | verified |
| S2-AC-09 | 可复现本地观测栈 | `local-observability-stack` / 默认启动保持轻量；`local-observability-stack` / 打开 profile 后可查询 trace | `observability-smoke.log` | verified |
| S2-AC-09 | 验证 harness 可归档 | `telemetry-verification-harness` / smoke 脚本输出可归档 | `observability-smoke.log` | verified |
| S2-AC-01 | 不回归 Stage 1 | 全部场景 | `go-test.log`、`go-test-race.log`、`golangci-lint.log`；远端 CI 待提交后补录 | verified（本地） |
| S2-AC-11 | change 可归档 | 全部 requirement/scenario | `openspec validate --all --strict` + tasks 证据 | planned（待负责人评审与 CI） |

## Requirement → 验证方式

| Requirement | 验证位置 | 说明 |
| --- | --- | --- |
| Telemetry 资源标识与传播 | `internal/platform/observability/*_test.go` | 内存 exporter 断言 trace id、parent/child、敏感字段不出现 |
| HTTP RED 指标契约 | `internal/platform/httpserver/server_test.go`、`internal/platform/observability/metrics_test.go` | testutil 断言指标、label、bucket；route template 用例 |
| Trace Span 覆盖 | `internal/platform/db`、`internal/platform/observability`、`internal/platform/httpserver` | HTTP→DB 链路；出站 `traceparent` stub；no-op 启动 |
| Stage 2 临时 SLO 与错误预算 | `deploy/observability/prometheus/rules.yaml`、`rules_test.yaml` | `promtool test rules`；CR 记录暂定阈值 |
| 告警与 Dashboard 基线 | `deploy/observability/prometheus/rules_test.yaml`、`deploy/observability/perses/` | 触发/不触发用例；dashboard 导入 |
| 本地可复现观测栈 | `deploy/compose/compose.yaml`、`deploy/observability/` | profile 启动、Prometheus/Jaeger 查询 |
| 遥测验证 harness | `scripts/observability-smoke.sh`、`.github/workflows/ci.yml` | 脚本输出 + CI job |

## 未覆盖 / 延后

- 生产 APM、多区域 trace、长期日志存储：Stage 3。
- 业务级 SLI（例如 Pipeline 成功率、Action 执行成功率）：由对应 change 提供，本 change 只冻结命名/标签机制。
- 正式 SLO 阈值：Stage 2 为暂定值，Stage 3 用真实流量重新签核。

## 证据索引（实现后填写）

| 证据 | 路径/链接 | 日期 |
| --- | --- | --- |
| 单元测试日志 | `docs/stages/evidence/stage-2-runtime-observability/go-test.log` | 2026-11-22 |
| race/lint 日志 | `go-test-race.log`、`golangci-lint.log` | 2026-11-22 |
| Prometheus rule 测试 | `rules-test.log` | 2026-11-22 |
| observability smoke | `observability-smoke.log` | 2026-11-22 |
| Jaeger trace 查询 | `jaeger-trace.json` | 2026-11-22 |
| Perses dashboard | `perses-dashboard.png` | 2026-11-22 |
| CI run | https://github.com/klb3713/OntoAxis/actions/runs/45022003253 | 2026-11-22 |
