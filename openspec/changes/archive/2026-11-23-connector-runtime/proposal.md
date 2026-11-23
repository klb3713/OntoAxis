# Proposal: connector-runtime

## Why

Stage 1 的 `connectors` 与 `ingestion` 模块已经落地 Connector CRUD、类型/配置校验、敏感配置拒绝、同步任务状态机与运行记录投影，并把测试、Schema 推断、同步启动做成可注入端口。但这些端口目前只有占位/内存实现：`StartSync` 只创建 `queued` 任务和初始运行记录，没有任何真实运行时被提交或对账，任务永远停在 `queued`。

Stage 2 S2-AC-06 要求 Connector 真实同步：Kafka Connect/Debezium CDC 至少一条真实链路，最终生成可查询 Dataset；Meltano 明确后移到 Stage 3。继续使用 fake adapter 只能证明"契约存在"，不能证明同步真实发生，也无法为 Stage 2 退出提供可复核证据。

本 change 把占位适配器替换为真实运行时：为 `postgres` 类型 Connector 生成 Kafka Connect/Debezium CDC 运行时连接器，通过 Kafka Connect REST 提交、停止与删除；ingestion 增加调度与对账，把运行时状态回写为任务/运行状态；用真实容器与合成数据完成端到端同步并验证结果可查询。

## What Changes

- 新增同步运行时预检：Connector 类型是否受支持、必要配置键是否完整、Kafka Connect 是否可达；不可用时返回明确错误，不伪造任务或运行时状态。
- 新增确定性运行时配置生成：从 Connector 配置与租户/连接器标识生成 Debezium source 与 sink 连接器定义、连接器名称与 topic 命名空间；同输入字节级稳定，跨租户隔离。
- 新增凭据边界：按 `secret_ref` 在提交时解析并注入 Kafka Connect 请求；凭据不落库、不回显、不写日志/指标/trace，解析失败不提交任何运行时连接器。
- 新增 Kafka Connect REST 客户端与任务提交/停止/回收：提交幂等、冲突可识别、运行时拒绝或超时不报告成功。
- ingestion 新增调度器与对账器：`queued` 任务提交运行时后进入 `running`；按运行时状态回写 `running`/`failed`/`succeeded`；初始快照完成才允许 `succeeded`；取消传播到运行时停止；有限重试/指数退避。
- 新增同一 Connector 并发同步约束与幂等保证；重复启动返回冲突，重复提交/重复回调不产生重复运行时连接器或非法迁移。
- 提供真实链路本地资产（`connector-runtime` Compose profile 片段）：Redpanda、Kafka Connect（Debezium source + Iceberg sink）、源 Postgres（logical replication）、Iceberg warehouse 与 Trino Iceberg catalog，配合同成数据完成 CDC → Dataset 可查询 E2E。
- 增加与 `runtime-observability` 基线一致的指标与 trace（`ontoaxis_*` 命名与标签白名单）。

**不在本 change 范围**

- Meltano：Stage 3 再评估，本 change 不新增、不配置、不依赖 Meltano。
- 真实客户数据、生产数据源或生产凭据；仅使用合成数据与本地 profile。
- 自动注册 Dataset 记录：Dataset 目录记录由 `dataset-catalog` 既有 API 预创建并绑定（见 design 开放问题）；若后续要求同步自动注册，需另开 change 评估 `ingestion` 到 `datasets` 的端口与装配。
- MySQL/S3/REST API/Kafka 类型的 CDC 运行时映射：当前仅支持 `postgres`，其他类型明确返回不支持，不做半成品映射。
- 修改 proto 契约、新增 RPC、新增第 10 个业务模块、修改公开 HTTP API 形态。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `connectors`: 增加真实同步运行时的类型/配置预检、确定性运行时配置生成、凭据解析与秘密边界、Kafka Connect 任务提交/停止/状态读取、运行时命名空间与租户隔离、运行时观测。
- `ingestion`: 增加同步任务调度与提交、幂等与并发约束、运行时状态对账、初始快照完成与终态确认、取消与运行时停止传播、重试与退避、同步运行观测。

## Impact

- 修改（本 change 所有权）：`internal/modules/connectors/`、`internal/modules/ingestion/` 的 service 端口与实现、领域校验与测试；新增运行时客户端、配置生成器、调度器/对账器与迁移（仅新增列/表，保持既有列名）。
- 新增：`deploy/connector-runtime/`（Compose profile 片段、Kafka Connect 镜像与插件清单、源 Postgres 初始化与合成数据、Trino Iceberg catalog 片段、E2E 脚本）、证据源文件。
- 不直接修改共享文件：`internal/app/`、`cmd/ontoaxis-core/`、`deploy/compose/`、`deploy/observability/`、`go.mod`。装配 patch、profile 合并、告警/仪表盘合并分别由 `stage-2-integration` 与 `production-delivery` 按 change map 共享文件规则完成；本 change 提供片段与 patch 说明。
- 依赖：`runtime-observability`（已归档）提供指标/trace 基线；`edge-gateway`（已归档）提供入口与身份链路；`authorization-openfga-cedar` 尚未归档，跨租户/越权负向测试中的授权断言列为跨 change 依赖，本 change 先以现有租户隔离与未找到语义覆盖。
- 运行时：本地新增 Redpanda、Kafka Connect、源 Postgres、Iceberg warehouse 相关容器，全部位于独立 profile，默认 `make test` 与现有 Compose 启动不依赖它们。
- 数据边界：只使用合成数据；不提交 `.env`、token、数据库口令或生产连接信息。
