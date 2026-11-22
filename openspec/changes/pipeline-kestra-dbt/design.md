# Design: pipeline-kestra-dbt

## Context

- Stage 1 `pipeline-authoring` 已归档：`pipelines` 模块提供定义 CRUD、发布版本快照、运行入口、Schedule、`kestra.Adapter` 接口与内存 adapter；发布只登记内存 Flow，触发只产生 `running` 运行记录并丢弃引擎返回的执行引用（`RunResult.ExecutionID` 未持久化）；DAG 仅校验"非空、合法 JSON、JSON 对象"；没有 dbt 项目、没有输出与血缘、没有对账与重试。
- 本 change 负责目录 `internal/modules/pipelines/`；`internal/app/`、`cmd/`、`deploy/`、`web/`、`go.mod` 由其他 change/owner 合并（见 `docs/stages/stage-2-change-map.md` §5）。
- 运行时事实：模块接口无 Start/Stop 生命周期钩子（`internal/platform/module/registry.go:15`），无后台 worker 基础设施；`datasets` 模块只有 catalog 元数据（无输出提交/血缘 API）；可观测基线（`docs/architecture/observability-baseline.md`）已固定指标前缀、label 白名单与 span 命名；`go.yaml.in/yaml/v3` 已是直接依赖，REST 只需 `net/http`。
- 依赖已满足：`edge-gateway` 已归档（路由与鉴权边界不变），`observability` 基线已归档（复用而非新增平台指标），`datasets` 已归档（仅消费 `DatasetResolver`）。`connector-runtime` 与本 change 并行，不阻塞实现；connector→dataset→pipeline 的跨 change E2E 属 Wave 3 集成。

## Goals

1. Pipeline 发布 = 真实 Kestra Flow 登记；触发 = 真实 Kestra 执行；运行状态可回写、可对账、可审计。
2. DAG 到 Flow 的转换确定性、可测试、无状态；同一版本重复发布不产生第二个 Flow revision。
3. dbt SQL 转换在受控镜像中执行，模型失败真实失败，成功后输出与血缘可查。
4. 失败可重试、可补偿、可观测；任何降级都 fail-closed，绝不把内存 adapter 结果伪装为真实执行。
5. 保持既有 wire 契约：不新增路由、不改 proto、不改状态枚举；Stage 1 合法定义仍可发布。

## Non-Goals

- Schedule → Kestra trigger 投影与调度执行（Schedule 仍是本地投影，后续 change）。
- Action/Temporal 边界、第 10 个业务模块、自研 DAG 引擎/调度器/执行器。
- proto 暴露 `execution_id`、前端改动、`web/` 任何变更。
- Kestra RBAC/多租户认证、生产 TLS、Helm/kind 交付（`production-delivery`）、真实客户数据与生产环境。
- Dataset 分支/事务提交、Catalog 血缘图（`datasets` owner 后续 change）。
- 通过 `oa-edge` 暴露 Kestra UI/API。

## Decisions

### D1: 真实 Kestra adapter 用手写 REST client（net/http），不引入 SDK

**选择**：在 `kestra/` 包内新增 `HTTPAdapter`，用 `net/http` 实现 `Adapter` 接口，JSON/YAML 编解码沿用现有依赖（`encoding/json`、`go.yaml.in/yaml/v3`），不新增 Go 依赖。

**理由**：仓库没有 Kestra Go SDK 可信依赖；接入面只有 Flow upsert、namespace files、执行触发/查询、执行取消五类调用，手写 client 约几百行即可，错误映射、重试、traceparent 注入与脱敏都可完全受控；避免为一个组件引入大依赖。

**备选**：官方/社区 Go SDK（不存在稳定版本）；shell out 到 `kestra` CLI（不可测试、错误不可结构化）；直接让 core 调 Kestra 内部 API（耦合版本内部实现）。均否决。

**接口面**：Adapter 在 Stage 1 两方法之外新增执行查询与可选取消，语义仍由 spec 约束：

```go
type Adapter interface {
    RegisterFlow(ctx context.Context, flow Flow) (FlowRef, error)          // 幂等 upsert，revision = pipeline version
    PublishFiles(ctx context.Context, ns string, files []File) error       // 命名空间文件，幂等
    DeleteFiles(ctx context.Context, ns string, paths []string) error      // 发布补偿
    TriggerRun(ctx context.Context, req RunRequest) (RunResult, error)     // 持久化 ExecutionID
    GetExecution(ctx context.Context, ref ExecutionRef) (Execution, error) // 状态 + outputs
}
```

**HTTP 映射（spike 必须对钉住的 Kestra 版本逐条核对并在证据中记录，不一致只改 client 不改 spec 语义）**：

| 操作 | 方法与路径（待钉版本核对） | 要点 |
| --- | --- | --- |
| Flow upsert | `PUT /api/v1/flows/{namespace}` 或 `PUT /api/v1/flows/{namespace}/{id}` | body 为 Flow YAML/JSON；同 id 同 revision 幂等 |
| Flow 查询/删除 | `GET`/`DELETE /api/v1/flows/{namespace}/{id}` | 补偿与漂移检测 |
| 命名空间文件 | `POST /api/v1/namespaces/{namespace}/files`（multipart）、`DELETE .../files/{path}` | dbt 项目文件，内容哈希去重 |
| 触发执行 | `POST /api/v1/executions/{namespace}/{flowId}` | body 含 `labels`、inputs；返回 execution id |
| 查询执行 | `GET /api/v1/executions/{executionId}` | 状态、labels、task outputs/outputFiles |
| 取消执行 | `POST /api/v1/executions/{executionId}/kill` | 仅内部使用，不暴露 API |
| 按 labels 查执行 | 执行搜索接口（spike 核对） | 触发中断后按 run 标签接管 |

**权衡**：手写 client 需要为每个 Kestra 版本做兼容；用钉死镜像版本 + 契约测试（httptest fixture + 真实脚手架 E2E）覆盖，版本升级成为显式任务。

### D2: Authoring DAG v1 契约（宽松保存、发布期严格校验）

**选择**：`pipeline.pipelines.dag` 在 v1 下新增受支持字段，未知字段忽略（前向兼容）；`dag_version` 缺省按 `1` 解释，声明其他值在发布时返回 400。草稿创建/更新仍只要求 JSON 对象（Stage 1 行为不变），严格校验在 publish/trigger 前置执行。

```json
{
  "dag_version": 1,
  "dataset_ids": ["<dataset-uuid>"],
  "inputs": [{"dataset_id": "<dataset-uuid>"}],
  "tasks": [
    {"id": "clean_orders", "type": "dbt", "depends_on": [],
     "model": "clean_orders", "materialized": "table",
     "sql": "select * from {{ ref('raw_orders') }}"}
  ],
  "outputs": [{"dataset_id": "<dataset-uuid>", "model": "clean_orders"}]
}
```

**校验规则（发布/触发时）**：任务 `id` 非空且唯一；`depends_on` 引用存在、无环；`type` 仅接受 `dbt`（预留扩展）；`sql`/`model` 必填；`outputs[].dataset_id` 同租户存在；`outputs[].model` 必须对应已声明 dbt 任务；无 `tasks` 的 DAG 生成单个 no-op 任务（兼容 Stage 1 的 `{"dataset_ids":[]}` 等定义）。错误使用稳定码（`invalid_dag_version`、`duplicate_task_id`、`dependency_cycle`、`unknown_task_type`、`missing_dependency`、`invalid_sql_reference`）。

**备选**：把 DAG 结构化进独立表（迁移成本、破坏既有 JSON 契约）；只做 JSON Schema 校验（无法做依赖环与跨租户 relation 检查）；发布与保存同样严格（会让存量草稿保存失败，破坏兼容）。均否决。

### D3: Flow IR 与 Kestra Flow 渲染

**选择**：两层结构 —— `FlowIR`（引擎无关：namespace、flow id、revision、labels、任务拓扑）→ `render.FlowYAML`（Kestra 定义）。IR 与 YAML 都做确定性排序（任务按拓扑序、同层按 id；labels 按 key 排序），输出 sha256 内容哈希，重复渲染字节一致（golden test 断言）。

Flow 任务映射：

| DAG 任务 | Kestra 任务 | 说明 |
| --- | --- | --- |
| `dbt` | `io.kestra.plugin.scripts.shell.Commands` | 在受控镜像内 `dbt run --select <model> --project-dir ...`；失败非零退出 |
| 无任务 | `io.kestra.plugin.core.flow.Sequential` + `io.kestra.plugin.core.log.Log` | no-op 兼容 |
| 依赖图 | `io.kestra.plugin.core.flow.Dag`（钉住版本支持时） | 退化方案：按拓扑序 `Sequential`（spike 决定） |

**dbt 项目文件布局**（namespace 文件，按 `(pipeline, version, content_hash)` 幂等上传）：

```text
pipeline_<pipeline_hex>/v<version>/
  dbt_project.yml
  profiles.yml
  models/<task_id>.sql
```

Flow 任务通过环境变量指向该目录并使用 namespace 文件挂载点（spike 核对钉住版本的挂载路径）；项目文件不含任何凭据。dbt 结果：命令产出 `target/run_results.json` 并由 Kestra `outputFiles` 保存，对账时通过执行 outputs 读取（spike 核对下载端点）；若 DAG 声明了 `outputs[]` 而产物缺失，视为失败而不是静默成功。

### D4: 身份派生、namespace 与 labels（租户隔离）

**选择**：namespace = `oa_<tenant_uuid_hex32>.<pipeline_uuid_hex32>`（小写无连字符），flow id = `<pipeline_hex32>`，Kestra flow revision = 发布版本号。labels（key 排序）：`ontoaxis.tenant_id`、`ontoaxis.pipeline_id`、`ontoaxis.pipeline_version`、`ontoaxis.dag_version`、`ontoaxis.managed_by=ontoaxis-core`、`ontoaxis.environment`；执行额外带 `ontoaxis.run_id`。

**理由**：命名由 UUID 确定性派生，不可猜测、不同租户天然不冲突；Kestra 本地部署无 RBAC，跨租户安全依赖 core 侧校验（namespace 固定为本租户派生值 + 执行引用必须属于本租户运行记录 + labels 校验），不把 Kestra 当安全边界；`run_id` 标签是触发中断后接管执行、避免重复执行的锚点。

**备选**：人类可读 namespace（租户名/环境，可猜测且改名漂移）；全局单 namespace + 标签过滤（单点冲突、清理困难）。否决。

### D5: 发布编排：先引擎、后数据库、失败补偿

**流程**：加载（租户内）→ 拒绝 archived → 校验 DAG v1 → 校验 dataset 输入/输出 → 渲染 dbt 项目与 Flow YAML（内容哈希）→ (1) 上传 namespace 文件 → (2) Flow upsert（revision=nextVersion）→ (3) 单事务写版本快照 + 递增 `version` + `status=published`（`SELECT ... FOR UPDATE` 串行化并发发布）。

**补偿**：步骤 (3) 失败时，尝试把 Flow 回退到上一已发布版本（用版本快照确定性重渲染后重新 upsert）、删除新 namespace 文件；首次发布则删除 Flow 与文件。补偿失败只记录告警指标与日志，仍返回失败，绝不报告成功。

**崩溃窗口**：(a) 文件后、Flow 前 → 孤儿文件，重试同内容哈希覆盖；(b) Flow 后、DB 前 → 孤儿 revision，重试 upsert 同 revision 收敛；(c) DB 提交后 → 成功。孤儿资源可按 namespace 前缀与 labels 识别，集中清理排期到 `production-delivery` 的运维任务，不在本 change 内实现后台清扫。

**备选**：先写 DB（pending 状态）再调引擎（需要新状态枚举、破坏 wire 契约；失败留下 pending 版本，需后台修复）；分布式事务（Kestra 无事务语义）。否决。

### D6: 触发编排与执行引用

**流程**：校验 published → 校验 DAG/dataset → 生成 `run_id` → 单事务插入运行记录（`status=queued`）+ 幂等记录 → 调 `TriggerRun`（labels 含 `ontoaxis.run_id`）→ 事务更新 `status=running`、`execution_id`、`started_at` → 返回 202。

**中断恢复**：运行处于 `queued` 且 `execution_id` 为空时，后续读触发恢复：按 `ontoaxis.run_id` 标签在引擎中查找执行（adapter `FindExecutionByRun`）；找到则接管引用并转 `running`；超过 `ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT`（默认 2 分钟）仍找不到则标记 `failed`（`execution_not_accepted`）。这保证"执行已发生但响应丢失"不会重复执行。

**并发**：同一 Pipeline 允许并发运行；发布与触发不互相阻塞；幂等键并发由幂等表唯一约束串行化（D11）。

### D7: 运行状态机与映射

```text
queued ──accepted──> running ──engine terminal──> succeeded | failed | cancelled
   └────────────recovery/timeout──────────────────> failed
```

| Kestra 执行状态（spike 核对枚举） | `pipeline.runs.status` |
| --- | --- |
| CREATED / RESTARTED / RETRYING / RUNNING / PAUSED / KILLING | `running` |
| SUCCESS / WARNING | `succeeded` |
| FAILED / RETRY_FAILED | `failed` |
| KILLED / CANCELLED | `cancelled` |
| 未知或无法解析 | 保持非终态；有界重试后 `failed`（`unknown_execution_state`） |

**单调写回**：`UPDATE ... WHERE status IN ('queued','running')`（CAS），终态永不回退；终态写入同时设置 `finished_at`、脱敏 `error`；成功终态与输出/血缘同事务（D10）。未知状态 MUST NOT 映射为成功（fail-closed）。

### D8: 对账机制：读时对账（无后台 worker）

**选择**：`GetPipelineRun`/`ListPipelineRuns` 返回前，对 `status IN ('queued','running')` 且 `last_reconciled_at` 早于节流窗口（默认 2s）的运行调用 `GetExecution`，单次请求最多对账 10 条，按 `started_at` 升序；写回后更新 `last_reconciled_at`。对账是幂等可重入的。

**理由**：模块接口没有 Start/Stop 生命周期，后台常驻 worker 需要改 `internal/platform/module` 或 `internal/app` 接线（属 `stage-2-integration`）；读时对账无需新生命周期、无额外 goroutine、测试确定性强，且 Stage 2 验收路径（发布→触发→查询→失败重试）都经过读取。

**代价与缓解**：无人查询的运行（含后台触发）不会被主动推进 → 由 Schedule 延后、无人读取的运行可能长期停留 `running`。缓解：状态查询 E2E 显式读取；`ONTOAXIS_PIPELINE_RECONCILE_ON_LIST` 默认开启；后台 sweeper 作为后续 change（`stage-2-integration` 接入生命周期后）实现，已在 Open Questions 登记。

**备选**：Kestra webhook 回调 core（需要公网入口与新路由，越界）；Kestra 通知插件（部署依赖）；轮询 goroutine（见上）。否决。

### D9: dbt 项目生成与执行环境

**选择**：`pipelines/dbt` 渲染器按 `(pipeline, version, content_hash)` 生成项目；`profiles.yml` 只使用 `{{ env_var('DBT_TRINO_*') }}` 引用，具体值来自部署环境/secret；`dbt_project.yml` 固定 `profile: ontoaxis_pipeline`。目标 catalog 由 `ONTOAXIS_PIPELINE_DBT_CATALOG`（本地默认 `memory`）决定，目标 schema 由租户/Pipeline 派生（`oa_t<tenant8>_p<pipeline8>`），dbt 线程数固定 2。

**执行镜像**：`deploy/compose/` profile fragment 内构建自定义 Kestra 镜像（基线 Kestra 镜像 + 钉住版本的 `dbt-trino`），不使用 Docker socket / DinD，Kestra 任务直接在 worker 进程内执行 `dbt`，命令退出码即失败信号；镜像 Dockerfile 与钉住版本由 `production-delivery` 合并与维护。

**SQL 静态检查（纵深防御，非安全边界）**：发布时拒绝显式引用受控 catalog/schema 之外的 `catalog.schema.table` 形式关系；真正的租户隔离仍由 core 侧数据/鉴权与 Trino 权限（后续 change）保证。**不**引入 SQL 解析依赖（正则 + 结构检查足够覆盖声明语法，跳过无法静态判定的动态 SQL，交由运行时失败）。

### D10: Dataset 输出与血缘写回

**选择**：新增 `pipeline.run_inputs(run_id, dataset_id, created_at)` 与 `pipeline.run_outputs(run_id, dataset_id, model, relation_catalog, relation_schema, relation_table, created_at)`，唯一约束 `(run_id, dataset_id)`。运行进入 `succeeded` 的同一事务内写入：输入来自 DAG 声明的 `dataset_ids`/`inputs[]`，输出来自 `outputs[]` 且关系标识由配置 catalog + 派生 schema + 模型名构成。

**理由**：`datasets` 模块是本仓库 catalog 元数据（真实表/分支/提交在湖仓侧），当前没有"Pipeline 提交 dataset 版本"API；直接写 datasets 表会跨越模块边界。本 change 先做"运行→dataset 的可审计引用与血缘"，Dataset 分支/事务提交作为 `datasets` owner 的后续 change（Open Questions）。

**失败语义**：输出写回失败 → 事务回滚、运行保持非终态，对账重试；重试耗尽（默认 3 次）→ 运行 `failed`（`output_writeback_failed`），不暴露成功。dbt 成功但产物缺失且声明了输出 → 同上失败路径。

### D11: 幂等记录与并发控制

**选择**：新增 `pipeline.idempotency_keys(tenant_id uuid, operation text, key_hash text, request_hash text, response_status int, response_body jsonb, created_at timestamptz, expires_at timestamptz, PRIMARY KEY (tenant_id, operation, key_hash))`，RLS 与其他表一致。请求携带 `Idempotency-Key` 时：同键同请求体且在 24h 内 → 重放首次响应；同键异请求体 → 409；并发插入冲突 → 读已有记录按重放/409 处理。过期清理在插入时机会式删除（无后台任务）。

**为什么可选**：ARCHITECTURE §9.4.2 要求 create/command API 带 `Idempotency-Key`，但当前全仓库尚未实现该中间件，且强制要求会破坏 `web/` 与其他 in-flight change 的调用方；本 change 先实现表与语义并在发布/触发生效（携带即严格），全站强制作为跨 change 决策登记（Open Questions）。

### D12: 配置与 secret 边界

新增配置（`internal/platform/config`，patch 由 `stage-2-integration` 合并；默认值 fail-closed）：

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `ONTOAXIS_KESTRA_URL` | 空 | 空 = adapter 未配置，发布/触发 503（不再静默使用内存 adapter） |
| `ONTOAXIS_KESTRA_TIMEOUT` | `10s` | 单次 HTTP 请求超时 |
| `ONTOAXIS_KESTRA_API_TOKEN` | 空 | 可选 bearer；**只从环境注入，永不记录** |
| `ONTOAXIS_PIPELINE_NAMESPACE_PREFIX` | `oa` | namespace 前缀 |
| `ONTOAXIS_PIPELINE_DBT_CATALOG` | `memory` | dbt 目标 catalog（compose 本地） |
| `ONTOAXIS_PIPELINE_DBT_IMAGE` | 空 | 受控 Kestra 镜像引用（部署侧提供，不写入 Flow 明文凭据） |
| `ONTOAXIS_PIPELINE_RECONCILE_THROTTLE` | `2s` | 读时对账节流 |
| `ONTOAXIS_PIPELINE_RUN_RECOVERY_TIMEOUT` | `2m` | 触发中断恢复窗口 |
| `ONTOAXIS_PIPELINE_RETRY_MAX_ATTEMPTS` | `3` | 引擎调用重试上限 |
| `ONTOAXIS_PIPELINE_IDEMPOTENCY_TTL` | `24h` | 幂等窗口 |

**凭据规则**：Flow YAML 只出现 `secret()` 引用或环境变量名；dbt `profiles.yml` 只出现 `env_var()` 引用；业务代码不记录 token/DSN/Authorization；日志与 span 只带 namespace/flow/revision/execution 引用。

### D13: 迁移与兼容

新增 `internal/modules/pipelines/migrations/00002_pipeline_execution.sql`（沿用 `-- +statement` 分隔与幂等风格）：

- `pipeline.runs` 增加 `execution_id text`、`failure_code text`、`attempt_count int DEFAULT 0`、`last_reconciled_at timestamptz`；
- 新建 `pipeline.run_inputs`、`pipeline.run_outputs`、`pipeline.idempotency_keys`，均启用 ENABLE/FORCE RLS 与 `app.tenant_id` 策略；
- 索引：`runs_tenant_status_started_idx (tenant_id, status, started_at)`、`run_outputs_run_idx`、`idempotency_keys_expires_idx`；
- `migrations.go` 的 `Statements()` 从"只读 `00001_pipeline_init.sql`"改为按文件名排序读取全部 `*.sql`（仍在模块目录内）。

兼容：proto/wire 不变；旧运行记录无 `execution_id`，首次读取时按"legacy 运行"处理——超过 24h 仍非终态则标记 `failed`（`legacy_run_without_execution`），避免永久 `running` 假象；旧 DAG（无 `dag_version`/`tasks`）仍可发布（D2）。迁移可重复执行，`make test` 覆盖二次应用。

### D14: 测试策略

**单元（`go test ./internal/modules/pipelines/...`）**：
1. DAG v1 校验：合法/未知版本/重复 id/依赖环/缺失依赖/未知类型/无任务兼容，错误码稳定。
2. Flow IR/YAML golden 与确定性：同输入重复渲染字节一致、哈希稳定；YAML 不含凭据（扫描 DSN/口令/token）。
3. HTTP adapter（`httptest`）：端点与请求体、幂等 upsert、4xx/5xx/timeout 映射、重试次数与退避（注入 clock）、traceparent 注入、响应丢失后的接管路径。
4. 状态映射与单调写回：全枚举表驱动；终态不回退 CAS；未知状态不成功。
5. 对账节流/批量上限/引擎不可达保留持久化状态；执行不存在 → `execution_not_found`。
6. 幂等：重放、异体 409、并发同键只生效一次；TTL 过期后允许新执行。
7. dbt 渲染器 golden（项目文件、profiles 仅 env_var、schema 派生）与 `run_results.json` 解析（成功/失败/缺失产物）。
8. 输出写回事务性（repo 故障注入 → 不暴露成功）与重放去重；跨租户 RLS 仓储测试。

**集成（真实实例；compose profile `pipeline`，经授权启动）**：
1. 发布 → Kestra 中存在对应 namespace/flow/revision/labels。
2. 触发 → 执行创建、run 持久化 `execution_id`、查询对账后 `succeeded`。
3. dbt 模型真实落到 Trino `memory` catalog 的派生 schema；`outputs` 与血缘可查。
4. 失败 E2E：坏 SQL → 执行 FAILED → run `failed`，无输出记录。
5. 失败重试 E2E：停止 Kestra → 发布返回 503 且无版本/`status` 变化 → 恢复 Kestra → 同一请求重试成功且版本只递增一次；触发侧同样验证 503→重试→成功且不重复执行。
6. 状态回写 E2E：运行成功后查询不再依赖引擎（停 Kestra 后仍返回 `succeeded`）；引擎不可达时返回已持久化状态。
7. 负向：未知任务类型、依赖环、跨租户读取 404、缺失 dataset 400、输出 dataset 跨租户 400、幂等键异体 409、`per_page=201` 400。
8. 遥测断言：`/metrics` 出现对应出站/job 指标；span/日志不含凭据与 SQL。

**证据**：原始命令/日志/JSON 输出交 `stage-2-integration` 汇总到 `docs/stages/evidence/stage-2-pipeline-kestra-dbt/`（该目录归集成 change 所有）；未执行项一律标 `planned`。

### D15: 失败与回滚

- **发布失败**：不提交版本；补偿尽力而为；返回 503；重试收敛同 revision。
- **触发失败/中断**：运行失败或按标签接管；不做"删执行"补偿（执行是事实记录），只标记运行状态与原因。
- **对账失败**：保留已持久化状态，重试有界，不伪造终态。
- **回滚**：回退代码 + 迁移（迁移只增列/增表，回退代码可忽略新列）；若需关闭真实执行，清空 `ONTOAXIS_KESTRA_URL` 使 adapter 未配置 → 发布/触发 503（fail-closed），**不**回退为内存 adapter 冒充成功。已发布 Flow/执行按 namespace 前缀保留供排查，不做破坏性删除。

## Risks / Trade-offs

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| Kestra API/状态枚举随版本漂移 | client 与真实实例不一致 | 钉住镜像版本；spike 逐端点核对并留证据；契约测试 + 真实 E2E 双覆盖 |
| 命名空间文件挂载路径/`outputFiles` 端点随版本变化 | dbt 项目不可达、产物读不到 | spike 优先验证；退化方案把项目内联为 Flow `inputFiles`（确定性不变） |
| 读时对账不能推进无人读取的运行 | 运行可能长期 `running` | Stage 2 验收路径均显式查询；后台 sweeper 登记为后续 change |
| Kestra 本地无 RBAC，多租户隔离靠命名与 core 校验 | 若绕过 core 直接访问 Kestra 可跨租户 | Kestra 不经 `oa-edge` 暴露；namespace 由 UUID 派生；跨租户执行引用在 core 拒绝；生产 RBAC 归 deployment 后续 |
| 发布补偿崩溃窗口留下孤儿 Flow 文件/revision | 资源泄漏、难清理 | 日志+指标暴露孤儿；清理任务归 `production-delivery` |
| dbt 镜像供应链（Kestra 基线 + pip 依赖） | 构建失败/不可复现 | 钉住 Kestra 镜像 digest 与 dbt-trino 版本；镜像与 E2E 证据随 change 提供 |
| Trino `memory` catalog 非持久化 | compose 重启后表消失 | 本地证据在 teardown 前采集；持久 catalog 归 `production-delivery` 评估 |
| 发布校验收紧 | Stage 1 测试/存量 DAG 若含非法任务会 400 | 无 `tasks` 的定义仍兼容；只对显式声明的 `tasks` 严格；更新 Stage 1 fixtures 并记录迁移说明 |
| `Idempotency-Key` 只在本 change 强制（携带时） | 与 ARCHITECTURE §9.4.2 全站要求存在差距 | 表与语义先落地；全站强制登记为跨 change 决策（Open Questions） |

## Migration Plan

1. **部署侧（`production-delivery` 合并）**：新增 compose `pipeline` profile（固定版本 Kestra + dbt 镜像 + Trino `memory` catalog 挂载）、端口 18086、healthcheck 与 `docs/architecture/services-and-ports.md` 更新。
2. **接线（`stage-2-integration` 合并 patch）**：`internal/app/modules.go` 注入真实 adapter（由 `ONTOAXIS_KESTRA_URL` 决定，空则未配置）、`cmd/` 无改动；`internal/platform/config` 新增字段。
3. **代码（本 change）**：测试先行（单测→adapter 契约→集成），`00002` 迁移随启动执行；默认内存 adapter 改为未配置（fail-closed），既有单测改用显式 `WithKestraAdapter(memory)`。
4. **E2E 与证据**：`openspec validate pipeline-kestra-dbt --strict` → compose 启动 → 成功/失败/重试/负向矩阵 → 证据交集成 change。
5. **回滚**：见 D15；迁移不回滚，发布版本不删除。

## Open Questions

1. 是否在 proto `PipelineRun` 暴露 `execution_id`（需 proto/gen/web 同步，建议后续 change）。
2. 全站强制 `Idempotency-Key`（ARCHITECTURE §9.4.2）由谁落地：本 change 只做表与语义，建议 `stage-2-integration` 统一中间件。
3. Schedule → Kestra trigger 投影的归属 change（当前 Schedule 仍是本地投影，不触发真实运行）。
4. 钉住的具体 Kestra 与 dbt-trino 版本、命名空间文件 API/挂载路径、`outputFiles` 下载端点（spike 任务 0.2 输出，写入代码注释与证据）。
5. 后台对账 sweeper 的生命周期归属（`stage-2-integration` 平台接线 vs 独立 change）。
6. Pipeline 运行提交 Dataset 分支/事务（`datasets` owner change，当前只记录输出与血缘）。
7. 旧非终态运行（Stage 1 遗留）在升级后标记 `failed` 是否需 owner 确认（D13）。
8. Trino 权限/持久 catalog（`production-delivery` 与后续安全 change）。
9. Kestra 认证在本地 compose 无鉴权、kind 环境如何注入（部署侧）。
