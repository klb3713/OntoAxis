# Tasks: pipeline-kestra-dbt

依赖说明（对应 `docs/stages/stage-2-change-map.md` 的 Wave 1 门禁 G1）：

- `datasets`（已归档）：仅消费 `DatasetResolver` 校验输入/输出 dataset，不修改 datasets 模块。
- `observability`（已归档）：复用 `ontoaxis` 指标前缀、label 白名单与 span 命名，不新增平台指标定义。
- `edge-gateway`（已归档）：路由与鉴权边界不变；Kestra 不通过 `oa-edge` 暴露。
- `connector-runtime`（并行，不阻塞）：只在 Wave 3 的 connector→dataset→pipeline 集成 E2E 中汇合，本 change 的 E2E 使用合成 dataset。
- 共享文件只提交 patch：`internal/app/`、`cmd/`、`deploy/compose/`、`internal/platform/config` 由 `stage-2-integration` / `production-delivery` 合并，本 change 不直接修改。

## 0. 前置与契约确认

- [x] 0.1 核对依赖 change 状态（datasets/observability/edge-gateway 已归档）与 change map 的目录归属，记录共享文件 patch 清单（验证：变更说明与 change map §5 对照，无越界文件）。
- [x] 0.2 Spike：在授权启动的本地环境钉住 Kestra 镜像版本并逐条核对 Flow upsert、namespace files、执行触发/查询/取消、按 labels 查执行、`outputFiles` 下载端点与 dbt 挂载路径（验证：spike 记录含实际请求/响应与版本，写入 design 附录或证据目录）。
- [x] 0.3 Spike：确定 dbt 执行方式（自定义 Kestra 镜像内置 dbt-trino vs 容器任务）与钉住版本，确认不使用 Docker socket（验证：镜像 Dockerfile 草案 + 一次 `dbt --version` 输出）。
- [x] 0.4 明确 dbt 目标 catalog/schema 派生规则与 SQL 静态检查边界，写入代码注释与测试夹具（验证：规则表 + 单测通过）。

## 1. Authoring DAG v1 契约与校验

- [x] 1.1 在 `internal/modules/pipelines/domain` 定义 DAG v1 结构（`dag_version`、`dataset_ids`、`inputs[]`、`tasks[]`、`outputs[]`），未知字段忽略，缺省 `dag_version=1`（验证：解析/序列化单测）。
- [x] 1.2 实现发布期校验：任务 id 唯一、`depends_on` 存在且无环、任务类型受支持、必填字段完整、`outputs[].model` 对应已声明任务；返回稳定错误码（验证：表驱动单测覆盖全部负向用例与错误码）。
- [x] 1.3 保持草稿保存宽松（仅 JSON 对象），发布/触发前执行 v1 校验；无 `tasks` 的定义按 v1 合法处理（验证：既有 Stage 1 定义仍可发布，回归单测）。
- [x] 1.4 更新 Stage 1 fixtures 与测试：原发布/触发用例使用合法 v1 定义或显式无任务定义（验证：`go test ./internal/modules/pipelines/...` 全绿）。

## 2. Flow IR、YAML 渲染与身份派生

- [x] 2.1 定义引擎无关 `FlowIR`（namespace、flow id、revision、labels、任务拓扑）与 Kestra 渲染器，任务按拓扑序、同层按 id 排序，labels 按 key 排序（验证：golden 文件 + 重复渲染字节一致断言）。
- [x] 2.2 实现 namespace 与 flow id 派生：`oa_<tenant_hex32>.<pipeline_hex32>`、flow id `<pipeline_hex32>`、revision=发布版本（验证：不同租户/同 Pipeline 多版本单测）。
- [x] 2.3 生成 labels：`ontoaxis.tenant_id`、`pipeline_id`、`pipeline_version`、`dag_version`、`managed_by=ontoaxis-core`、`environment`，执行额外 `run_id`（验证：labels 快照测试，断言无凭据/个人信息）。
- [x] 2.4 无任务 DAG 生成 no-op Flow；依赖图在钉住版本支持时用 `Dag` 任务、否则按拓扑序退化 `Sequential`（验证：两类 golden + Kestra 结构校验单测）。
- [x] 2.5 渲染结果做 sha256 内容哈希并用于幂等上传/对账（验证：同输入哈希稳定、异输入哈希变化）。

## 3. Kestra REST client 与 adapter 实现

- [x] 3.1 实现 `HTTPAdapter`（`net/http`，无新依赖）：超时、JSON/YAML 编解码、traceparent 注入、bearer token 仅从配置注入且不记录（验证：`httptest` 断言请求头与 body，日志脱敏断言）。
- [x] 3.2 实现 Flow upsert（同 revision 幂等）、namespace 文件上传/删除（内容哈希去重）、执行触发、执行查询、按 run label 查找执行、执行取消（验证：逐端点 `httptest` 契约测试）。
- [x] 3.3 实现错误映射：4xx 定义错误 → `invalid_argument` 语义；连接错误/超时/5xx/429 → `unavailable` 可重试；错误信息脱敏（不含 DSN/token/堆栈）（验证：表驱动单测 + 脱敏扫描）。
- [x] 3.4 实现有界指数退避（带抖动、由注入 clock 控制），仅对可重试失败生效，记录尝试次数与结果指标（验证：注入 clock 的重试单测；兼容既有 `sethvargo/go-retry` 间接依赖，不新增模块）。
- [x] 3.5 将模块默认 adapter 改为"未配置 fail-closed"，内存 adapter 改为显式 `WithKestraAdapter(memory)` 仅测试/本地使用；发布/触发 503（验证：模块构造单测 + `module_test.go` 行为回归）。

## 4. 配置、迁移与接线 patch

- [x] 4.1 新增 `internal/platform/config` 字段（`ONTOAXIS_KESTRA_*`、dbt catalog/镜像、对账/恢复/重试/幂等参数）并 fail-closed 校验（验证：config 单测；patch 提交 `stage-2-integration` 合并）。
- [x] 4.2 新增 `migrations/00002_pipeline_execution.sql`：`runs` 增列（`execution_id`、`failure_code`、`attempt_count`、`last_reconciled_at`）、新表 `run_inputs`/`run_outputs`/`idempotency_keys`、索引与 RLS 策略（验证：迁移两次应用幂等；RLS 跨租户仓储测试）。
- [x] 4.3 修改 `migrations.go` 使 `Statements()` 按文件名排序读取全部 `*.sql`（验证：迁移单测与应用两次幂等）。
- [x] 4.4 提交 `internal/app/modules.go` 接线 patch：由配置构造真实 adapter，未配置则保持未配置（验证：patch 文件 + 本地启动日志显示 adapter 状态；由集成 owner 合并）。
- [x] 4.5 提交 compose `pipeline` profile fragment（固定版本 Kestra、dbt 镜像、Trino `memory` catalog、端口 18086、healthcheck）给 `production-delivery`（验证：fragment 文件 + 集成时 compose config 校验）。

## 5. 发布路径改造

- [x] 5.1 发布编排：校验 DAG v1 → 校验输入/输出 dataset → 渲染项目与 Flow（哈希）→ 上传 namespace 文件 → Flow upsert（revision=nextVersion）→ 单事务提交版本快照并递增 `version`/置 `published`（验证：service 单测断言顺序与事务边界）。
- [x] 5.2 并发发布串行化：`SELECT ... FOR UPDATE` 锁 Pipeline 行，避免重复递增版本（验证：并发发布测试只有一个成功版本推进）。
- [x] 5.3 发布补偿：DB 提交失败时回退 Flow 到上一已发布版本、删除新 namespace 文件；首次发布删除 Flow 与文件；补偿失败记录告警且仍返回失败（验证：注入 repo 故障的单测断言补偿调用与 503；见 `evidence/2026-11-23-pipeline-publish-compensation-unit.md`）。
- [x] 5.4 发布失败保持 `version`/`status` 不变、不新增版本记录（验证：失败路径单测与集成断言）。

## 6. 触发路径与执行引用

- [x] 6.1 触发编排：插入 `queued` 运行 + 幂等记录 → `TriggerRun`（labels 含 `run_id`）→ 更新 `running`/`execution_id`/`started_at` → 202（验证：service 单测断言状态与字段）。
- [x] 6.2 触发重试按 `ontoaxis.run_id` 标签接管已创建执行，避免第二次执行（验证：`httptest` 模拟响应丢失 + 重试路径单测）。
- [x] 6.3 触发中断恢复：`queued` 且无执行引用时按标签查找接管；超过 `RUN_RECOVERY_TIMEOUT` 标记 `failed`（`execution_not_accepted`）（验证：读时恢复单测 + 集成）。
- [x] 6.4 adapter 失败保持 503 + 运行 `failed`，错误信息脱敏且不保存原始堆栈（验证：失败单测与脱敏断言）。

## 7. 状态回写与对账

- [x] 7.1 实现引擎状态到运行状态的映射表，未知状态不映射成功（验证：全枚举表驱动单测）。
- [x] 7.2 实现单调写回 CAS（`WHERE status IN ('queued','running')`），终态设置 `finished_at` 与脱敏错误（验证：终态不回退并发测试）。
- [x] 7.3 实现读时对账：仅非终态、节流窗口（默认 2s）、单请求上限 10 条、按 `started_at` 升序，写回并更新 `last_reconciled_at`（验证：节流/批量上限单测）。
- [x] 7.4 引擎不可达时返回已持久化状态（200），执行 404 有界重试后标记 `failed`（`execution_not_found`）（验证：单测 + 集成）。
- [x] 7.5 旧运行兼容：无 `execution_id` 且超过 24h 的非终态运行标记 `failed`（`legacy_run_without_execution`）（验证：迁移后读取单测）。

## 8. dbt 项目生成与执行

- [x] 8.1 实现 dbt 渲染器：`dbt_project.yml`、`profiles.yml`（仅 `env_var()`）、`models/<task_id>.sql`，按 (pipeline, version, content_hash) 生成（验证：golden 文件 + 确定性断言 + 凭据扫描）。
- [x] 8.2 模型 schema 派生（`oa_t<tenant8>_p<pipeline8>`）与目标 catalog 配置；发布期拒绝控制范围外显式 `catalog.schema.table` 引用（验证：负向单测）。
- [x] 8.3 Flow 任务执行 `dbt run`/`dbt test` 并保存 `target/run_results.json`；非零退出即失败（验证：Flow golden + 集成失败路径）。
- [x] 8.4 对账解析 `run_results.json`：模型级状态、成功计数、失败原因脱敏；产物缺失且声明输出时判失败（验证：解析单测 + 集成）。
- [x] 8.5 dbt 项目文件上传/删除走 namespace files 并带内容哈希（验证：`httptest` 断言幂等与补偿删除）。

## 9. Dataset 输出与血缘

- [x] 9.1 发布/触发校验 `outputs[]` dataset 存在且同租户（复用 `DatasetResolver`，未注入时 503）（验证：负向单测覆盖不存在与跨租户）。
- [x] 9.2 实现 `run_inputs`/`run_outputs` 写回：输入来自声明引用，输出含 dataset、model、关系标识，唯一约束 (run_id, dataset_id)（验证：repo 单测 + 集成查询）。
- [x] 9.3 输出写回与 `succeeded` 终态同事务；写回失败保持非终态、有界重试后 `failed`（`output_writeback_failed`）（验证：故障注入单测 + 集成）。
- [x] 9.4 重放不产生重复输出/血缘（验证：幂等重放单测）。

## 10. 幂等

- [x] 10.1 实现 `Idempotency-Key` 解析与校验（仅发布/触发；缺省保持既有语义）（验证：handler 单测）。
- [x] 10.2 实现幂等记录：同键同体 24h 重放首次响应、同键异体 409、并发同键只生效一次（唯一约束串行化）（验证：并发测试 + 集成 409）。
- [x] 10.3 过期机会式清理，无后台任务（验证：插入时清理单测）。
- [x] 10.4 响应重放不改变引擎调用次数（验证：`httptest` 计数断言）。

## 11. 遥测与脱敏

- [x] 11.1 出站 Kestra 调用记录 `ontoaxis_outbound_request_duration_seconds{dependency="kestra",operation,result}`，span 使用 `pipelines.*` 命名并携带 namespace/flow/revision/execution 引用（验证：单测 + `/metrics` 断言）。
- [x] 11.2 对账任务记录 `ontoaxis_jobs_total{type="pipeline_run_reconcile",result}`；不新增带 tenant 的指标 label（验证：指标断言 + label 白名单检查）。
- [x] 11.3 日志/错误脱敏：不含 token、DSN、Authorization、SQL 原文、请求体（验证：日志扫描单测 + 集成日志检查）。

## 12. 失败路径与负向测试矩阵

- [x] 12.1 负向矩阵单测：未知任务类型、依赖环、重复 id、不支持 `dag_version`、跨租户读取 404、缺失 dataset 400、输出 dataset 跨租户 400、`per_page=201` 400、幂等异体 409（验证：`go test` 全绿并列出用例清单）。
- [x] 12.2 引擎失败矩阵：4xx 不重试、超时/5xx 有界重试、持续 503 收敛 503、执行不存在 `execution_not_found`、未知状态不成功（验证：`httptest` 单测矩阵）。
- [x] 12.3 发布/触发中断与补偿：文件上传中途失败、DB 提交失败、响应丢失（验证：故障注入单测 + 集成重试 E2E）。
- [x] 12.4 降级不伪造：未配置/不可达时 503 且无成功记录；引擎恢复后重试只成功一次（验证：集成断言）。

## 13. 真实实例集成 E2E 与证据

- [x] 13.1 经授权启动 compose `pipeline` profile（Kestra + Trino memory catalog + dbt 镜像），记录版本、端口、健康检查输出（验证：`docker compose ... config` + 健康检查日志，证据落盘）。
- [x] 13.2 成功路径 E2E：创建 dataset → 创建/发布 Pipeline → 触发 → 查询对账为 `succeeded` → 输出与血缘可查 → dbt 表存在于派生 schema（验证：API 响应与 Trino `SHOW TABLES` 证据）。
- [x] 13.3 失败路径 E2E：坏 SQL → 执行 FAILED → run `failed`、无输出记录、错误脱敏（验证：响应/DB/日志证据）。
- [x] 13.4 失败重试 E2E：停 Kestra → 发布 503 且无版本变化 → 恢复 → 同一请求重试成功且版本只递增一次；触发侧 503→重试→成功且不重复执行（验证：命令日志 + DB 查询证据）。
- [x] 13.5 状态回写 E2E：成功后停止 Kestra 再查询仍返回 `succeeded`；对非终态运行引擎不可达返回持久化状态（验证：两次查询对比证据）。
- [x] 13.6 负向集成用例：跨租户 404、非法 DAG 400、缺失/跨租户 dataset 400、幂等异体 409（验证：HTTP 状态与错误体证据）。
- [x] 13.7 遥测证据：抓取 `/metrics` 相关序列与一次 trace，断言无凭据/tenant label（验证：指标/日志抓取文件）。
- [x] 13.8 证据移交：原始输出与命令交 `stage-2-integration` 汇总至 `docs/stages/evidence/stage-2-pipeline-kestra-dbt/`（验证：交接清单与文件路径）。

## 14. 文档、追溯与门禁

- [x] 14.1 更新 `docs/architecture/services-and-ports.md` 的 Kestra 端口 18086（patch 提交文档 owner 合并）（验证：diff 与端口表一致性）。
- [x] 14.2 更新 traceability：把 planned 证据改为 actual 并记录未执行项（验证：traceability 与证据文件一一对应）。
- [x] 14.3 运行 `openspec validate pipeline-kestra-dbt --strict` 与 `openspec validate --all --strict`（验证：命令输出）。
- [x] 14.4 运行 `make build`、`make test`、`make test-race`、`make lint`（验证：命令输出；共享文件由 owner 合并后由集成 change 统一跑全仓门禁）。
- [x] 14.5 代码评审：确认未修改其他 change 目录、未改 proto/`web/`/`deploy/`、未提交凭据（验证：`git status`/diff 审查记录）。
