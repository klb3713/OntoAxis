# Tasks: connector-runtime

> 所有任务保持未勾选（`- [ ]`）；只有证据存在时才可勾选。实施前需 change 级实施授权。

## 前置条件与依赖

- 前驱 change：`runtime-observability`（指标/trace 基线）与 `edge-gateway`（入口/身份链路）已归档，Wave 1 入口条件成立；证据：`openspec list` 与 `openspec/changes/archive/`。
- 跨 change 依赖：`authorization-openfga-cedar` 尚未归档。涉及授权层跨租户/越权（403）断言的负向测试标为 blocked_by 该 change；本 change 先以既有租户隔离（未找到/无副作用）与 fail-closed 语义覆盖，不修改其目录。
- 共享文件合并依赖：`internal/app`、`internal/platform/config`（若需新增键）、`cmd/ontoaxis-core` 的装配 patch 与 `deploy/compose`、`deploy/observability` 的 profile/仪表盘片段分别由 `stage-2-integration`、`production-delivery` 合并；本 change 只提供片段与 patch 说明，不直接修改。
- 环境依赖：本地 Docker（Compose v2）；真实链路 profile 需可用镜像仓库与约 3–4 GB 可用内存。不得使用真实客户数据与生产凭据。

## 1. 契约与配置

- [x] 1.1 扩展 `connectors` service 端口与 Option：新增 `SyncRuntime`（提交/状态/停止）、`SecretResolver`、运行时命名与配置生成依赖，保持既有 `NewService`/`Store`/`ConnectorTester`/`SyncStarter` 兼容。验证：`go test ./internal/modules/connectors/...` 既有测试断言不放宽仍然通过。
- [x] 1.2 新增配置键（`ONTOAXIS_CONNECT_URL`、`ONTOAXIS_CONNECT_TIMEOUT`、`ONTOAXIS_CONNECT_RETRY_MAX`、`ONTOAXIS_CONNECT_VERIFY_MAX_ROWS`）与模块 Option 映射，默认空/禁用；准备 `internal/app`、`internal/platform/config` patch 说明。验证：单元测试覆盖"未配置 → 启动可用、启动同步返回不可用"；patch 说明通过 reviewer 检查，未直接修改共享文件。
- [x] 1.3 增量迁移：`ingestion.sync_jobs` 增加 `runtime_ref`、`attempt`、`lease_owner`、`lease_until`；`sync_runs.stats` 约定 `phase`/`runtime_ref`/`attempt`/`verified_rows`/`cleanup_pending`（JSON，不改列结构）。验证：迁移 up/down 测试通过，既有列名与状态常量不变，RLS 策略保留。

## 2. 运行时定义生成与凭据

- [x] 2.1 Debezium source 配置生成：白名单键校验、表清单正则、slot/publication 默认值、确定性 runtime 名称与 topic 前缀、跨租户命名空间。验证：golden 单测（同输入字节级一致、非法表名拒绝、两租户同名 Connector 名称不同、未知键拒绝）。
- [x] 2.2 Iceberg sink 配置生成：动态表路由、warehouse 下 Dataset 目录绑定、`dataset_id` 必填校验。验证：golden 单测 + 非法 `dataset_id`/缺失 warehouse 配置的拒绝测试。
- [x] 2.3 `SecretResolver` 默认实现（环境变量 provider）与注入/脱敏：解析、提交请求注入、失败中止。验证：单测覆盖解析成功、缺失、解析失败；断言日志、错误、生成物、指标、span 不含明文。

## 3. Kafka Connect 客户端与提交编排

- [x] 3.1 实现 Kafka Connect REST 客户端（`net/http`）：`PUT /connectors/<name>/config`（upsert）、`GET /connectors/<name>/status`、`DELETE /connectors/<name>`、`PUT .../stop`；超时、有限重试、错误分类。验证：`httptest` 单测覆盖 2xx/4xx/5xx/超时/非法 JSON/幂等重复 PUT，重试次数与退避符合设计（1s/2s/4s/8s/16s，≤5 次）。
- [x] 3.2 提交/停止/状态编排：启动同步预检 → 作业创建；暂停/软删除联动停止与回收；重复提交幂等；运行时不存在时按幂等语义处理。验证：单测 + 假 Connect 场景测试（提交成功、冲突配置、停止失败、已不存在）。
- [x] 3.3 观测接入：运行时请求指标/提交耗时/出站 span，命名与标签遵循 `runtime-observability` 基线。验证：指标命名与标签断言单测；span 属性断言（dependency/operation/result，无凭据/请求体）。

## 4. ingestion 调度与对账

- [x] 4.1 调度器：条件更新 + 租约领取 `queued` 作业，提交运行时，成功后 `running`，失败按分类重试/终止。验证：并发领取测试（两 goroutine 仅一方提交）；重复调度不重复提交；重试耗尽与永久失败路径。
- [x] 4.2 对账器：周期扫描 `running`、状态映射矩阵、同步快照成功判定（RUNNING + Iceberg snapshot + 行数比对 + 连续两次稳定）、终态不可变。验证：表驱动单测覆盖映射与终态保护；注入时钟缩短周期；行数不一致不判成功。
- [x] 4.3 取消传播：`queued` 未提交直接取消；`running` 调运行时停止并确认后取消；未确认保持 `running` 且返回错误；终态冲突。验证：四类场景单测；断言取消失败时不报告 `cancelled`。
- [x] 4.4 失败分类、有限重试与退避：可重试（5xx/超时/连接）与不可重试（4xx/配置/认证）分流；统计记录 attempts 与脱敏原因。验证：单测断言重试序列、上限、成功/失败终态唯一。
- [x] 4.5 运行状态投影与 Watch 一致性：任务/运行状态、统计、错误、开始/结束时间一致；成功时记录 `verified_rows` 与 Dataset 引用。验证：投影一致性单测与 Watch 事件次数/终态断言。
- [x] 4.6 后台租户隔离：调度、对账、取消在租户上下文执行；跨租户访问返回未找到且无副作用。验证：单测 + 集成负向（租户 A 看不到/不能取消租户 B 作业）。

## 5. 真实运行时资产

- [x] 5.1 `deploy/connector-runtime/` Compose overlay：Redpanda（KRaft）、Kafka Connect（固定镜像 digest + Debezium Postgres source + Iceberg sink 插件）、源 Postgres（`wal_level=logical`、`REPLICA IDENTITY FULL`、publication、合成数据 init SQL）、warehouse 共享卷、Trino Iceberg catalog overlay；全部 `profiles: ["connector-runtime"]`、healthcheck、端口约定（19092/18086/15434）。验证：`docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml --profile connector-runtime config` 通过；容器 healthy；`deploy/compose/compose.yaml` 未修改。
- [x] 5.2 插件与镜像可复现：版本/digest/校验和清单与构建或下载说明；启动后 `GET /connector-plugins` 含所需 source/sink。验证：重建后插件列表输出归档；README 与实际 digest 一致。
- [ ] 5.3 本地 E2E 与清理脚本：创建 Dataset（既有 API）与 Connector → 启动同步 → 轮询作业 → Trino 查询行数；含负向（Connect down、非法配置、paused、跨租户、取消）与 `scripts/cleanup.sh`。验证：脚本退出码 0；输出含 job id、runtime connector 名、源/sink/查询行数（均来自合成数据）。

## 6. 真实实例测试与证据

- [ ] 6.1 集成测试（`-tags=integration`，env 开关跳过）：真实 Connect 提交/状态/停止/幂等；错误凭据脱敏；Connect 停止后的不可用路径。验证：`go test -tags=integration ./internal/modules/connectors/... ./internal/modules/ingestion/...` 通过并记录耗时与限制。
- [ ] 6.2 真实同步 E2E（S2-AC-06，blocked_by 5.1/5.3 与 integration 装配合并）：CDC 链路端到端，Dataset 可查询；证据包括源表行数 = sink 表行数 = Trino 查询行数、dataset RID、作业状态 `succeeded`、运行时连接器名。验证：脚本输出与数据库/Trino/API 三类断言一致；若全栈装配未合并，先以模块内集成替代并在证据中标注。
- [ ] 6.3 失败与负向矩阵：Connect 不可达（503，不伪造任务）、非法配置（400）、paused/error（409）、运行时提交失败（job failed 且脱敏）、重试耗尽、取消传播、重复启动幂等、跨租户 404。验证：逐场景测试记录与断言输出；授权层 403 断言 await `authorization-openfga-cedar`。
- [ ] 6.4 可观测性证据：Prometheus 抓取的指标样例、trace 样例、dashboard/告警片段（移交 `production-delivery` 合并）。验证：抓取输出与 span JSON 归档；片段文件存在且未直接改 `deploy/observability/`。

## 7. 交付与门禁

- [x] 7.1 文档同步建议：ARCHITECTURE §7/§15.2 状态更新建议、模块 README 片段、运行说明；提交给 `stage-2-integration` 合并。验证：reviewer 确认本 change 未修改共享文件，建议清单与实际文档差异一致。
- [x] 7.2 证据文件：在 `docs/stages/evidence/stage-2-connector-runtime.md`（由 integration 归档）源文件中记录命令、时间、环境、输出摘要、限制（合成数据/本地 profile）与未覆盖项。验证：traceability 中每条 planned 证据均有对应来源；证据不得使用真实客户数据。
- [x] 7.3 门禁检查：`make build`、`make test`、`make test-race`、`make lint`、`make vet`、`openspec validate connector-runtime --strict`、变更清单（`git status` 仅含本 change 目录与负责模块目录）。验证：命令输出归档；不自动提交、不归档、不宣布 Stage 完成。
