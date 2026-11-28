# 可观测性 E2E-09（任务 7.4）

- 日期：2026-11-28
- 环境：macOS 本地，孤立 Compose 项目 `oa-obs-e2e`（`COMPOSE_PROJECT_NAME=oa-obs-e2e`）
- 结论：**7.4 通过（本地 Compose，合成流量）**；证据边界见下。

## 1. 规则测试（promtool）

命令：`make observability-rules-test`

- 结果：`SUCCESS`
- 覆盖：`OntoAxisCoreDown` 正例、`OntoAxisHighErrorRate` 正/负例；新增 SLO 记录规则测试 `ontoaxis:slo_http_availability:ratio_5m`（100×200 + 10×500 → 0.9091）与 `ontoaxis:slo_readyz:ratio_5m`（=1）。
- 日志：[`observability-rules-test-7.4.log`](observability-rules-test-7.4.log)

## 2. 运行时冒烟（metrics / trace / Perses / 规则加载）

命令：`COMPOSE_PROJECT_NAME=oa-obs-e2e bash scripts/observability-smoke.sh`

- 结果：`PASS: 3 HTTP metric series, 1 trace(s), Perses dashboard imported, 12 rules loaded`
- 日志：[`observability-smoke-7.4.log`](observability-smoke-7.4.log)
- 修复：`deploy/observability/prometheus/prometheus.yaml` 的 `rule_files` 原为 `/etc/prometheus/rules/*.yaml`，但 compose 将规则挂载为 `/etc/prometheus/rules.yaml`，运行时实际加载 **0 条规则**（SLO 面板无数据、告警不生效）。已改为 `/etc/prometheus/rules.yaml`，并让 `observability-smoke.sh` 断言规则组已加载（12 条）。

## 3. SLO 面板与查询

- Perses 面板截图：[`perses-slo-panel.png`](perses-slo-panel.png)（HTTP request rate、p95 latency、Module readiness、Availability SLO (5m)=100% 可见；5xx ratio 因无错误流量显示 No data）
- Prometheus 查询快照：
  - [`prometheus-slo-availability.json`](prometheus-slo-availability.json)：`ontoaxis:slo_http_availability:ratio_5m` = `1`
  - [`prometheus-slo-readyz.json`](prometheus-slo-readyz.json)：`ontoaxis:slo_readyz:ratio_5m` = `1`
  - [`prometheus-rules-loaded.json`](prometheus-rules-loaded.json)：`ontoaxis-slo` 6 条记录规则 + `ontoaxis-alerts` 6 条告警规则

## 4. 负载基线（可复现）

命令：

```
TOKEN=$(curl -sS -X POST http://127.0.0.1:18083/realms/ontoaxis/protocol/openid-connect/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=password&client_id=ontoaxis-web&username=admin&password=admin123' | \
  python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')
TARGET_URL=http://127.0.0.1:18080/api/v1/tenancy/projects BEARER_TOKEN="$TOKEN" \
  REQUESTS=300 CONCURRENCY=20 OUT=docs/stages/evidence/stage-2-integration/observability-load-baseline.json \
  ./scripts/observability-load-baseline.sh
```

- 结果（[`observability-load-baseline.json`](observability-load-baseline.json)）：300 请求 / 20 并发，**300 成功 / 0 失败**，约 **974.7 req/s**，p50 **12.2ms**、p95 **60.9ms**、p99 **92.7ms**。

## 5. 证据边界（必须保留）

- 全部为本地 Compose + 合成数据；**不是生产容量、生产 SLO 或生产压测**。
- 告警"可触发"由 promtool 规则单元测试证明；未接入真实告警通道，未做真实故障注入。
- 5xx 面板无数据是当前无错误流量，不代表错误率永远为 0。
