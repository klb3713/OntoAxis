# observability Delta

## MODIFIED Requirements

### Requirement: 本地可复现观测栈

系统 SHALL 提供 opt-in 的本地 `observability` Compose profile，包含 OTel Collector、Prometheus、Alertmanager、Jaeger 与 Perses；Jaeger MUST 使用受依赖策略约束的 v2 版本，且 MUST 以明确 tag 与 `sha256` digest 引用，MUST NOT 使用 `latest` 或浮动 tag。默认 Compose 启动 MUST NOT 依赖该 profile；开启 profile 后 MUST 能查询 `ontoaxis-core` 的 metrics 与 trace。trace 后端的查询入口 MUST 保持稳定：Jaeger UI 仍通过 16686 端口提供查询，OTLP/gRPC 与 OTLP/HTTP 接收端口 MUST 保持 4317 与 4318，使 core 的 exporter 配置无需因后端版本变化而改写。

#### Scenario: 默认启动保持轻量

- **WHEN** 执行默认 `docker compose -f deploy/compose/compose.yaml up -d`
- **THEN** 不启动 observability 组件，核心栈仍健康

#### Scenario: 打开 profile 后可查询 trace

- **WHEN** 使用 `--profile observability` 启动并执行一个 API 请求
- **THEN** Jaeger 中可查询到对应 service/operation 的 trace，Prometheus 可查询 `ontoaxis_http_requests_total`

#### Scenario: v1 配置不再生效时显式失败

- **WHEN** 以 v1 专有的启动参数或环境变量启动 v2 后端
- **THEN** 后端启动失败或健康检查不就绪，并在日志中指出无法识别的配置项，而不是静默降级为「无 trace」

#### Scenario: 版本与 digest 受策略约束

- **WHEN** 执行 `make deps-check` 且 Jaeger 引用为 `latest`、浮动 tag 或缺少 digest（棘轮日期之后）
- **THEN** 校验失败并指出违反项；`deploy/deps/lock.yaml` 中的 Jaeger 条目与实际引用一致

#### Scenario: 后端不可用时业务不受影响

- **WHEN** Jaeger 未启动或不可达
- **THEN** `ontoaxis-core` 仍正常处理请求，trace 导出失败只影响可观测性，不影响业务可用性
