# Wave 1 分组启动矩阵（G2 条件 5 · compose 部分）

- 日期：2026-11-25
- 环境：GitHub Actions `ubuntu-latest` runner（真实 runner，可拉取镜像）
- 分支/提交：`stage2-int/d07-osm-evidence` @ `c8b82cc`
- 工作流：`.github/workflows/wave1-stack-smoke.yml`（3 个并行 job）
- run：[45570760338](https://github.com/klb3713/OntoAxis/actions/runs/45570760338) — 结论 **success（3/3 job 绿）**

| Wave 1 服务 | job | job id | 结果 | 关键证据 |
| --- | --- | --- | --- | --- |
| connector-runtime（Kafka Connect/Debezium） | connector-runtime | 139056529082 | success | `PASS Kafka Connect REST 可达: http://127.0.0.1:18089`；`PASS 插件存在: io.debezium.connector.postgresql.PostgresConnector`；`PASS 插件存在: org.apache.iceberg.connect.IcebergSinkConnector`；`PASS fallback JDBC sink 可用: io.debezium.connector.jdbc.JdbcSinkConnector`；`PASS GET /connectors 成功`；`SUMMARY pass=5 fail=0 skip=2`（skip：source-postgres 未起、未传 `--submit-smoke`） |
| pipeline-kestra-dbt（Kestra） | pipeline-kestra | 139056529129 | success | Kestra `/api/v1/configs`：`{"state":"ACTIVE","coordinator":true,"starting":false,"uptime":"23.01s"}`；Trino `/v1/info` 200 |
| action-temporal（Temporal） | action-temporal | 139056528825 | success | actions smoke：`{"pass":7,"fail":0}`（object_update、compensation、approval_approve、approval_reject、approval_timeout、retry_exhaustion、compensation_failed） |

## 说明与边界

- 本轮验证的是 **compose 栈真实 `up -d` 后各服务健康/接口可用**，用于解除 D-07 端口冲突后的启动阻塞；端口分配见 `d07-port-conflict.md`（Kafka Connect 18089、Kestra 18086、Temporal UI 18088、search worker 默认 18088/同栈用 18091）。
- 为缩短运行时间并避免 core 的 fail-closed 依赖阻塞 Wave 1 服务本身，workflow 只启动各组件所需的最小依赖集，并直接轮询组件自身接口（不依赖 compose healthcheck）：connector job 轮询 `http://127.0.0.1:18089/connectors`；pipeline job 轮询 Kestra HTTP 与 Trino `/v1/info`；temporal job 复用既有 smoke（`SKIP_START=1`，Temporal/核心服务已由 job 步骤拉起）。
- **未覆盖**：Helm/kind 的“可启动”（属 6.x 任务）；source-postgres CDC 写链路（`--submit-smoke` 未启用）；Wave 2 组件（Meilisearch/OpenFGA）另见 `search-profile.md`、`compose-smoke.md`。
- 结论：**G2 条件 5 的 compose 启动部分已具备真实 runner 证据**；G2 是否登记 `pass` 仍由负责人确认（本文件不构成门禁批准）。
