# Proposal: pipeline-kestra-dbt

## Why

Stage 2 要求 Pipeline 通过真实 Kestra + dbt 执行（S2-AC-05）：发布 Flow、运行、状态回写、失败重试与 Dataset 输出均可验证。

当前实现（Stage 1 `pipeline-authoring`）只提供定义、版本与运行入口的垂直切片：`publish` 仅向内存 adapter 登记，`trigger` 只产生一条 `running` 记录且丢弃引擎返回的执行引用，运行状态不会回写，没有 dbt 项目、没有 Dataset 输出与血缘、没有重试与补偿。因此无法回答"这个 Pipeline 是否真的在 Kestra 上执行过、失败后是否可重试、输出是否落到声明的 Dataset"。继续在这些假设上叠加 Stage 2 后续能力，会把"内存 adapter 返回即成功"固化为事实来源。

本 change 把 `pipelines` 从"可替换 adapter 的最小切片"推进为"真实执行闭环"：确定性 Flow 生成、真实 Kestra 发布/触发、执行状态对账回写、dbt SQL 转换、Dataset 输出与血缘、失败重试与负向路径，同时保留 Stage 1 已归档的 API 契约（仅行为收紧，不破坏字段与状态码语义）。

## What Changes

- **Authoring DAG v1 契约**：在 `pipeline.pipelines.dag` 之上定义可校验的版本化定义（`dag_version` 缺省按 v1 解释，保持 Stage 1 存量定义可发布），校验任务标识唯一、依赖存在且无环、任务类型受支持；发布阶段校验，草稿保存仍宽松。
- **Flow IR 与 Flow 生成**：确定性地把 DAG 转换为执行引擎无关的 Flow IR，再渲染为 Kestra Flow 定义（namespace、flow id、revision、labels、tasks），相同定义生成字节一致的结果；无任务定义生成单 no-op 任务并保持向后兼容。
- **真实 Kestra adapter**：实现连接真实 Kestra 的 adapter（Flow 登记 upsert、命名空间文件、执行触发、执行查询），未配置或不可达时 fail-closed；内存 adapter 退化为仅测试/显式本地模式的实现，不再是静默默认。
- **发布路径**：Flow 与项目文件先写引擎、再提交版本快照，失败补偿且不留下部分版本；幂等 upsert 同一 revision。
- **触发路径**：持久化引擎执行引用；把 run 标识写入执行 labels，触发重试按标签接管已有执行，避免重复执行。
- **状态回写与对账**：读取运行前对非终态运行与 Kestra 对账，按固定映射写回状态（终态单调不回退）；引擎不可达返回已持久化状态，不伪造终态；执行不存在时有界重试后失败。
- **dbt 项目与模型映射**：按 (Pipeline, 版本, 内容哈希) 生成确定性 dbt 项目（`dbt_project.yml`、`profiles.yml`、`models/*.sql`），项目文件不含凭据；Flow 在受控镜像中执行 `dbt run`/`dbt test`，失败传播为非零退出。
- **Dataset 输出与血缘**：DAG 声明 `outputs[]`；发布/触发校验输出 dataset 存在且同租户；运行成功与输出、输入血缘在同一事务内写回，重放不重复。
- **重试、退避与补偿**：只对可重试失败（连接、超时、5xx、429）做有界指数退避（带抖动）；4xx 校验错误不重试；发布失败补偿引擎侧未提交资源；重试耗尽不伪造成功。
- **幂等**：发布/触发支持 `Idempotency-Key`（携带即生效：24h 重放首次结果，同键异体返回 409，并发同键只成功一次）。
- **可观测与失败信息**：出站 Kestra 调用、对账任务复用 `ontoaxis` 基线指标与 span；调用方错误使用统一错误结构，持久化错误经过脱敏，不泄漏凭据、DSN、SQL 或引擎堆栈。
- **不修改**：proto/wire 契约、`cmd/`、`internal/app/`、`deploy/`、`web/`、`go.mod`/`go.sum`（接线与 compose 由 owner 合并，见 Impact）。
- **不新建**：第 10 个业务模块、自研 DAG 引擎/调度器/执行器。

## Capabilities

### New Capabilities

无。本 change 不新增 capability，全部行为落在既有 `pipelines` 能力上。

### Modified Capabilities

- `pipelines`: 发布、运行入口、运行查询、Kestra adapter 边界与 Dataset 依赖校验的行为收紧（MODIFIED），并新增 DAG→Flow 生成、namespace/labels 租户隔离、发布/触发引擎语义、幂等、状态对账回写、dbt 项目与执行、Dataset 输出与血缘、重试/补偿、失败路径与遥测等要求（ADDED）。

## Impact

- **代码（本 change 负责目录）**：`internal/modules/pipelines/` —— `domain`（DAG v1 解析/校验、状态机、身份派生）、`kestra`（真实 HTTP adapter、Flow IR/YAML 渲染、重试）、`service`（发布/触发/对账/幂等/输出写回）、`repo`（执行引用、输出、血缘、幂等记录）、`migrations`（新增 `00002_*` 迁移，需把 `Statements()` 从单文件改为按序读取）、`api`（仅幂等头处理与错误映射，不新增路由）。
- **共享文件（只提交 patch，不直接修改）**：
  - `internal/app/modules.go` 与 `cmd/ontoaxis-core/` 接线、`internal/platform/config/` 新增 `ONTOAXIS_KESTRA_*`/dbt 配置字段 → 交 `stage-2-integration` 合并；
  - `deploy/compose/`（Kestra 服务、Trino `memory` catalog、dbt 镜像构建）→ 交 `production-delivery` 合并（compose profile fragment + Dockerfile）；
  - `docs/architecture/services-and-ports.md`（新增 Kestra 端口 18086）→ 随 patch 提交给文档 owner。
- **依赖**：`datasets` 能力（Stage 1 已归档，`DatasetResolver` 已存在）仅需消费，无需新接口；`observability` 基线（已归档）复用指标/span 约定，不新增平台指标；`edge-gateway`（已归档）路由不变，Kestra 不经过 `oa-edge` 暴露；`connector-runtime` 不阻塞本 change，只影响 Wave 3 的 connector→dataset→pipeline 集成 E2E。
- **数据**：新增 `pipeline.runs.execution_id` 等列与 `run_inputs`/`run_outputs`/`idempotency_keys` 表，全部在 `pipeline` schema 内，沿用既有 RLS 策略模式；迁移幂等、可重入。
- **安全**：默认 fail-closed；Flow 与 dbt 项目只引用 secret 名称/环境变量，不落凭据；错误与日志脱敏；跨租户 namespace/执行/运行一律 404 或 403。
- **验证**：单元测试 + 真实实例集成测试（compose `pipeline` profile：Kestra + Trino memory catalog + dbt 镜像）；证据按 `docs/stages/evidence/` 约定交给 `stage-2-integration` 汇总；`openspec validate pipeline-kestra-dbt --strict` 通过前不归档。
