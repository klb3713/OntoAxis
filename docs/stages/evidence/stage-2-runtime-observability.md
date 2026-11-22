# runtime-observability 验证证据

日期：2026-11-22
环境：macOS + Docker Desktop，本地 Compose profile `observability`

## 1. 验证结果

| 验证项 | 命令 | 结果 | 证据 |
| --- | --- | --- | --- |
| Go 单元测试 | `go test ./...` | PASS | `go-test.log` |
| Go race | `CGO_ENABLED=1 go test -race ./...` | PASS | `go-test-race.log` |
| Lint | `golangci-lint run ./...` | 0 issues | `golangci-lint.log` |
| License | `make license-check` | EXIT=0 | `license-check.log` |
| Prometheus rules | `promtool test rules` | SUCCESS | `rules-test.log` |
| 本地 trace/metrics smoke | `scripts/observability-smoke.sh` | PASS | `observability-smoke.log` |
| Perses dashboard | 导入并打开 `/projects/ontoaxis/dashboards/ontoaxis-service-overview` | 渲染 6 个 panel | `perses-dashboard.png` |
| Prometheus series | `ontoaxis_http_requests_total` / `ontoaxis_module_ready` | 非空 | `prometheus-http-requests.json` |
| Jaeger trace | `service=ontoaxis-core&limit=1` | 1 条 trace | `jaeger-trace.json` |
| 生成/契约 | `make generate-check`、`make proto-breaking` | EXIT=0 | `generate-check.log`、`proto-breaking.log` |
| CI/YAML/lint | `actionlint`、`shellcheck scripts/*.sh` | EXIT=0 | `actionlint.log`、`shellcheck.log` |
| Web 测试 | `cd web && pnpm test` | 26/26 PASS | `web-test.log` |

## 2. 本地运行

```sh
export ONTOAXIS_OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
docker compose -f deploy/compose/compose.yaml --profile observability up -d --build
scripts/observability-smoke.sh
```

组件端口：core 18080、metrics 19090、Prometheus 19091、Alertmanager 19093、Jaeger 16686、Perses 18084、OTLP 14317/14318。

## 3. 证据边界

- Perses dashboard 通过 API 导入，Prometheus datasource 使用本地 `http://localhost:19091`；
  生产/远程部署需改成环境内 Prometheus 地址。该边界已记录在
  `docs/architecture/observability-baseline.md` 第 12 节。
- GitHub Actions 的 `observability` job 已写入 `.github/workflows/ci.yml`；本轮未提交/推送，
  远端 run 证据待提交后补录（tasks 7.2 保持未勾选）。
- 负责人评审尚未完成（tasks 8.3 保持未勾选）；本证据只代表本地实现验证通过。
- 真实生产告警路由（PagerDuty/Slack）不在本 change 范围；本地 Alertmanager 仅作为配置校验。
