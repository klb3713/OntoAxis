# Spec Delta

## Purpose

定义 OntoAxis 运行时遥测契约，使 HTTP 请求、数据库、出站依赖与后台任务具备一致的 trace、指标、SLO、告警和验证方式；所有能力必须默认可关闭、可本地复现，且不得泄漏敏感数据。

## ADDED Requirements

### Requirement: Telemetry 资源标识与传播

系统 SHALL 为每个进程和请求建立稳定的 OTel resource/context；resource MUST 包含 `service.name`、`service.version`、`deployment.environment` 与实例标识，跨进程 MUST 使用 W3C `traceparent`/`tracestate` 传播，且 MUST NOT 传播 token、cookie、请求体或用户标识。

#### Scenario: 入站请求生成 trace ID

- **WHEN** 客户端请求 `ontoaxis-core` 且传入合法 `traceparent`
- **THEN** 服务沿用该 trace ID，并把同一 trace ID 写入日志、响应 `X-Request-Id` 关联上下文和出站 span

#### Scenario: 无 traceparent 时创建根 span

- **WHEN** 客户端未传 `traceparent`
- **THEN** 服务创建新的根 span，且不把请求中的任意 header 原样写入 span attributes

#### Scenario: 禁止敏感数据进入 telemetry

- **WHEN** 请求包含 Authorization、Cookie 或请求体
- **THEN** 导出的 span/log/metric 中不出现 token、密码、完整请求体或个人数据

### Requirement: HTTP RED 指标契约

系统 SHALL 暴露统一的 HTTP RED 指标：请求总数、请求时延直方图与在途请求数；指标 MUST 使用 `ontoaxis_` 前缀、MUST 使用归一化 route template 而不是原始路径、MUST NOT 使用 tenant_id/user_id 等高基数标签。

#### Scenario: 指标按 route 聚合

- **WHEN** 请求命中 `/api/v1/objects/{id}`
- **THEN** `ontoaxis_http_requests_total` 的 `route` label 为模板 `/api/v1/objects/{id}`，而不是具体 UUID

#### Scenario: 状态码与耗时被记录

- **WHEN** 任意 HTTP 请求完成
- **THEN** `ontoaxis_http_requests_total{method,route,status}` 递增，`ontoaxis_http_request_duration_seconds` 记录对应 bucket

#### Scenario: 未匹配路由不制造高基数

- **WHEN** 请求未匹配任何注册路由
- **THEN** 指标使用固定 route label `unmatched`，且不把完整 URL 作为标签

### Requirement: Trace Span 覆盖

系统 SHALL 为入站 HTTP、模块 service 边界、PostgreSQL 查询、出站 HTTP/Trino、Kafka 消费与后台任务创建 span；span MUST 记录低基数属性，MUST NOT 记录完整 SQL、对象属性、消息体或密钥。

#### Scenario: 入站到数据库的链路可关联

- **WHEN** 请求触发一次 PostgreSQL 查询
- **THEN** 同一 trace 中存在 HTTP server span 与 DB client span，且 parent/child 关系正确

#### Scenario: 出站依赖失败被标记

- **WHEN** 出站 HTTP/Trino 调用返回错误或超时
- **THEN** 对应 span 标记 error 状态，且 metrics 记录失败分类

#### Scenario: 关闭 exporter 时仍可运行

- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` 未配置
- **THEN** 服务仍正常启动、请求成功，trace 仅保留本地 no-op 行为，不影响可用性

### Requirement: Stage 2 临时 SLO 与错误预算

系统 SHALL 定义并计算 Stage 2 临时 SLO：HTTP 可用性 99.9%/30 天、read API p95 < 200ms、write API p95 < 400ms、readiness 成功率 99.95%；SLO MUST 以 Prometheus recording rules 表达，阈值 MUST 标记为 Stage 2 暂定值。

#### Scenario: 可用性错误预算可计算

- **WHEN** 过去 30 天 5xx（排除 4xx）比例超过 0.1%
- **THEN** `ontoaxis:slo_http_availability:ratio_30d` 低于目标，burn-rate 告警可触发

#### Scenario: 延迟 SLO 可区分读写

- **WHEN** 请求按 HTTP method/route 分类
- **THEN** read 与 write 分别计算 p95，并分别对照 200ms/400ms 暂定目标

#### Scenario: 无流量时不误报

- **WHEN** 某 route 在窗口内没有请求
- **THEN** SLO 计算和告警不会把“无数据”当作失败

### Requirement: 告警与 Dashboard 基线

系统 SHALL 提供 Prometheus 告警规则与 Perses dashboard；告警 MUST 包含可用性 burn-rate、p95 延迟、实例 down、readiness 失败、DB 连接池饱和与 telemetry exporter 失败；所有规则 MUST 有 `promtool test rules` 测试。

#### Scenario: 高错误率触发告警

- **WHEN** 5xx 比例持续 5 分钟高于 1%
- **THEN** `OntoAxisHighErrorRate` 进入 pending/firing，并标注 runbook 链接

#### Scenario: 实例不可用触发告警

- **WHEN** `up{job="ontoaxis-core"} == 0` 持续 2 分钟
- **THEN** `OntoAxisCoreDown` 触发，dashboard 显示实例状态与最近错误

#### Scenario: 规则测试可离线执行

- **WHEN** 执行 `make observability-rules-test`
- **THEN** `promtool test rules` 使用固定输入验证所有告警的触发与不触发场景

### Requirement: 本地可复现观测栈

系统 SHALL 提供 opt-in 的本地 `observability` Compose profile，包含 OTel Collector、Prometheus、Alertmanager、Jaeger 与 Perses；默认 Compose 启动 MUST NOT 依赖该 profile，开启 profile 后 MUST 能采集 `ontoaxis-core` 的 metrics 与 trace。

#### Scenario: 默认启动保持轻量

- **WHEN** 执行默认 `docker compose -f deploy/compose/compose.yaml up -d`
- **THEN** 不启动 observability 组件，核心栈仍健康

#### Scenario: 打开 profile 后可查询 trace

- **WHEN** 使用 `--profile observability` 启动并执行一个 API 请求
- **THEN** Jaeger 中可查询到对应 service/operation 的 trace，Prometheus 可查询 `ontoaxis_http_requests_total`

### Requirement: 遥测验证 harness

系统 SHALL 提供可重复的遥测验证 harness：指标断言、trace 传播测试、告警规则测试和 smoke 脚本；验证结果 MUST 可作为 change/task 证据，不得只依赖人工查看 dashboard。

#### Scenario: 指标断言失败可定位

- **WHEN** 中间件未注册或 label 不符合契约
- **THEN** 单元测试失败并指出缺失/错误的指标名或 label

#### Scenario: trace 传播测试覆盖跨进程

- **WHEN** 测试注入 `traceparent` 并调用下游 stub
- **THEN** 下游收到的 `traceparent` 保持同一 trace ID，span parent 正确

#### Scenario: smoke 脚本输出可归档

- **WHEN** 执行 `scripts/observability-smoke.sh`
- **THEN** 输出 metrics 查询结果、trace 查询结果和失败项，非零退出表示验证失败
