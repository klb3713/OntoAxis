# Spec Delta: pipelines

## ADDED Requirements

### Requirement: Authoring DAG contract to Flow generation

系统 SHALL 把发布版本中的作者 DAG 按版本化契约确定性地转换为执行引擎无关的 Flow IR，并渲染为 Kestra Flow 定义。转换 MUST 校验任务标识唯一、依赖引用存在且无环、任务类型受支持、`dag_version` 受支持（缺省按 v1 解释）与必填字段完整。校验失败 MUST 返回 400 `invalid_argument` 与稳定错误码，且不登记 Flow、不推进版本。相同 `dag` 与 `dag_version` MUST 产生字节一致的 Flow IR 与 Flow 定义。无任务定义 MUST 生成单个 no-op 任务以保持兼容。

#### Scenario: 合法 DAG 生成确定性 Flow 定义

- **WHEN** 调用方发布一个包含 dbt 任务与依赖关系的合法 DAG
- **THEN** 系统生成含 namespace、flow id、revision 与 tasks 的 Flow 定义，且重复生成字节一致
- **AND** 只向执行引擎登记一次该 revision

#### Scenario: 未知任务类型被拒绝

- **WHEN** DAG 中任务的 `type` 不在受支持集合内
- **THEN** 系统返回 400 `invalid_argument`，错误码稳定
- **AND** 不登记 Flow，Pipeline 版本与状态保持不变

#### Scenario: 依赖环或缺失依赖被拒绝

- **WHEN** DAG 的 `depends_on` 构成环或引用不存在的任务标识
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不写入版本记录，不调用执行引擎

#### Scenario: 重复任务标识被拒绝

- **WHEN** DAG 中两个任务使用同一 `id`
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不产生 Flow 定义

#### Scenario: 不支持的 DAG 版本

- **WHEN** DAG 声明不受支持的 `dag_version`
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 未声明 `dag_version` 的 Stage 1 定义仍可发布为 v1

### Requirement: Flow namespace and labels for tenant isolation

系统 SHALL 为每个（租户, Pipeline）派生稳定且互不冲突的 Kestra namespace 与 flow 标识，并把租户与版本元数据写入 Flow labels。namespace MUST 由租户标识与 Pipeline 标识确定性派生，不同租户 MUST NOT 共享 namespace；labels MUST 至少包含租户、Pipeline、发布版本、DAG 版本与托管方标记，且 MUST NOT 包含凭据、令牌或个人信息。跨租户的 Flow 登记、执行触发与执行查询 MUST 返回 404 或 403，MUST NOT 返回其他租户资源内容。

#### Scenario: 不同租户的 Flow 相互隔离

- **WHEN** 租户 A 与租户 B 各自发布同名 Pipeline
- **THEN** 两者落在不同 namespace，flow 标识与 labels 均可区分
- **AND** 租户 B 读取或触发租户 A 的 Flow 返回 404

#### Scenario: labels 携带版本元数据

- **WHEN** 发布带租户、Pipeline、发布版本与 DAG 版本的 Pipeline
- **THEN** Flow labels 包含对应键值，且不包含凭据或个人信息

#### Scenario: 跨租户执行查询被拒绝

- **WHEN** 租户 B 以租户 A 的执行引用查询运行状态
- **THEN** 系统返回 404，且不暴露执行内容

### Requirement: Flow publish and execution trigger

系统 SHALL 通过执行引擎 REST API 以幂等 upsert 语义登记（namespace, flow id, revision）并触发执行。发布重试 MUST 覆盖同一 revision，MUST NOT 创建第二个 Flow；触发 MUST 把租户、Pipeline 版本与 run 标识写入执行 labels 并持久化执行引用，重试 MUST 复用已存在的执行。引擎返回 4xx 定义错误时 MUST 映射为 400 `invalid_argument`；引擎不可达、超时或 5xx 时 MUST 返回 503 `unavailable` 并保持持久化状态一致。

#### Scenario: 同一 revision 幂等 upsert

- **WHEN** 发布同一定义并因瞬态错误重试登记
- **THEN** 引擎中仍只有一个 Flow，revision 不变
- **AND** 版本快照只在登记成功后提交

#### Scenario: 触发创建执行并持久化引用

- **WHEN** 触发已发布 Pipeline 的运行
- **THEN** 引擎执行以 run 标识打标，运行记录持久化执行引用
- **AND** 系统返回 202 且运行状态为 `running`

#### Scenario: 触发重试接管已有执行

- **WHEN** 触发响应丢失后以同一运行重试
- **THEN** 系统按 run 标识找到已有执行，不创建第二次执行

#### Scenario: 引擎校验错误映射

- **WHEN** 引擎拒绝 Flow 定义并返回 4xx
- **THEN** 系统返回 400 `invalid_argument`，运行记录失败且错误信息脱敏

#### Scenario: 引擎超时或 5xx

- **WHEN** 引擎超时或返回 5xx
- **THEN** 系统返回 503 `unavailable`，已持久化状态保持
- **AND** 不报告发布或触发成功

### Requirement: Publish and trigger idempotency

发布与触发 API SHALL 在请求携带 `Idempotency-Key` 时提供幂等重放：同一租户 24 小时内以相同键与相同请求体重试 MUST 返回首次结果，MUST NOT 产生第二次 Flow 登记或执行；相同键但请求体不同 MUST 返回 409 `conflict`；并发使用同一键 MUST 只有一个请求生效。缺少该头时 MUST 保持既有同步语义，不得把缺少该头解释为成功重放。

#### Scenario: 相同键重放首次结果

- **WHEN** 同一租户以相同键和相同请求体重复调用发布或触发
- **THEN** 第二次返回首次结果，引擎调用次数不增加

#### Scenario: 相同键不同请求体

- **WHEN** 同一租户以相同键提交不同请求体
- **THEN** 系统返回 409 `conflict`
- **AND** 不创建版本或运行记录

#### Scenario: 并发同键只生效一次

- **WHEN** 两个并发请求使用同一键
- **THEN** 至多一个请求执行引擎调用，另一个得到首次结果或 409

#### Scenario: 缺少幂等键

- **WHEN** 请求未携带 `Idempotency-Key`
- **THEN** 每次调用按既有语义处理，不返回历史结果

### Requirement: Run status reconciliation and writeback

系统 SHALL 在返回非终态运行前与实际执行引擎对账并写回状态。状态映射 MUST 为：非终态执行映射 `running`，成功（含告警）映射 `succeeded`，失败与重试耗尽映射 `failed`，取消或终止映射 `cancelled`；未知或无法解析状态 MUST NOT 映射为成功。运行状态 MUST 单调，终态 MUST NOT 回退。引擎不可达时 MUST 返回已持久化状态；执行不存在时 MUST 在有界重试后标记 `failed` 并记录可审计原因。

#### Scenario: 成功执行回写

- **WHEN** 查询一个引擎中已成功完成的运行
- **THEN** 运行状态为 `succeeded`、`finished_at` 非空且错误信息为空
- **AND** 输出与血缘在同一次写回中记录

#### Scenario: 失败执行回写

- **WHEN** 引擎执行因 dbt 任务失败而终止
- **THEN** 运行状态为 `failed`，错误信息脱敏且可审计

#### Scenario: 未知状态不伪造成功

- **WHEN** 引擎返回无法映射的非成功状态
- **THEN** 运行状态保持非终态或按失败收尾，MUST NOT 变为 `succeeded`

#### Scenario: 执行不存在

- **WHEN** 引擎在有限重试后仍返回执行不存在
- **THEN** 运行状态标记 `failed`，原因为 `execution_not_found`

#### Scenario: 终态不回退

- **WHEN** 已持久化 `succeeded` 的运行再次对账且引擎返回瞬态错误
- **THEN** 运行状态保持 `succeeded`

#### Scenario: 引擎不可达

- **WHEN** 查询运行状态时引擎不可达
- **THEN** 系统返回 200 与已持久化状态，不伪造终态

### Requirement: dbt project and model execution

类型为 `dbt` 的 DAG 任务 SHALL 映射为 dbt 模型与确定性 dbt 项目：系统按（Pipeline, 发布版本, 内容哈希）生成 `dbt_project.yml`、`profiles.yml` 与 `models/*.sql`，项目文件 MUST NOT 包含任何凭据，模型 schema MUST 由租户与 Pipeline 派生并在版本间稳定。Flow MUST 在受控镜像中执行 `dbt run` 与 `dbt test`；dbt 非零退出 MUST 使运行失败并传播脱敏原因；成功后 MUST 采集模型级结果用于输出与遥测。

#### Scenario: 项目确定性生成

- **WHEN** 同一 DAG 版本被发布两次
- **THEN** 生成的 dbt 项目文件字节一致，内容哈希稳定
- **AND** 模型文件名与 SQL 与任务声明对应

#### Scenario: 项目不含凭据

- **WHEN** 渲染 dbt 项目与 Flow 定义
- **THEN** 只出现凭据的环境变量或 secret 引用，不出现口令、DSN 或令牌

#### Scenario: 模型 schema 隔离

- **WHEN** 发布或运行一个 dbt 任务
- **THEN** 目标 schema 由租户与 Pipeline 派生，模型写入受控 catalog
- **AND** 显式引用控制范围外 catalog 或 schema 的 SQL 在发布时被拒绝

#### Scenario: dbt 执行失败

- **WHEN** dbt 编译或运行返回非零退出
- **THEN** 运行标记 `failed`，错误信息脱敏且不含完整 SQL 或堆栈

#### Scenario: dbt 测试失败

- **WHEN** `dbt test` 失败
- **THEN** 运行标记 `failed`，不写入数据集输出

### Requirement: Dataset outputs and lineage

系统 SHALL 把声明的 Dataset 输出与输入/输出血缘写回。DAG 的 `outputs[]` MUST 引用当前租户已存在的 dataset；输出写回 MUST 与终态 `succeeded` 写入在同一事务内完成，写回失败 MUST NOT 暴露成功。输出 MUST 记录 dataset、模型与受控关系标识；同一运行重放 MUST NOT 产生重复输出或重复血缘记录。运行结束后输入与输出 MUST 都可按运行查询与审计。

#### Scenario: 成功运行记录输出与血缘

- **WHEN** 一次运行成功完成并声明了两个输出 dataset
- **THEN** 存在两条输出记录，关系标识由受控 schema 派生
- **AND** 输入与输出 dataset 关系可审计

#### Scenario: 输出写回原子性

- **WHEN** 终态写回时输出记录写入失败
- **THEN** 运行不表现为 `succeeded`，重试后完成或按失败收尾

#### Scenario: 重放不重复输出

- **WHEN** 同一运行的写回被重放
- **THEN** 每个（运行, dataset）只有一条输出记录

#### Scenario: 输出 dataset 不存在

- **WHEN** 发布或触发声明了 resolver 判定不存在的输出 dataset
- **THEN** 系统返回 400 `invalid_argument`，不创建版本或运行

### Requirement: Retry, backoff and compensation

对执行引擎的发布、触发与查询 SHALL 仅对可重试失败（连接错误、超时、5xx、429）做有界重试，使用指数退避与抖动；4xx 校验错误 MUST NOT 重试。重试耗尽后 MUST 记录尝试次数并以 `unavailable` 或运行失败收尾，MUST NOT 伪造成功。发布失败 MUST 尝试补偿未提交的引擎侧资源；触发中断 MUST 按 run 标签接管已创建执行，找不到执行时 MUST 在有界重试后失败。

#### Scenario: 可重试失败后退避重试

- **WHEN** 引擎连续两次连接中断后恢复
- **THEN** 发布或触发最终成功，尝试次数被记录

#### Scenario: 校验错误不重试

- **WHEN** 引擎返回 400 定义错误
- **THEN** 系统立即失败，只调用一次引擎

#### Scenario: 重试有界

- **WHEN** 引擎持续返回 503
- **THEN** 系统在有限次数后返回 503，运行记录失败
- **AND** 不发生无限重试或重复执行

#### Scenario: 发布失败补偿

- **WHEN** Flow 已登记但项目文件上传中途失败
- **THEN** 系统清理未提交的引擎侧资源并报告失败
- **AND** 不提交版本快照

#### Scenario: 触发中断接管

- **WHEN** 运行处于 `queued` 且未持久化执行引用
- **THEN** 系统按 run 标签接管已创建的执行
- **AND** 在有界重试后仍找不到执行时把运行标记 `failed`

### Requirement: Execution telemetry and failure sanitization

Pipeline 发布、触发与对账 SHALL 产出符合 `ontoaxis` 基线的遥测：出站执行引擎调用 MUST 记录依赖耗时与结果，对账 MUST 记录任务结果，span MUST 携带 namespace、flow、revision 与执行引用但 MUST NOT 携带凭据、DSN、SQL 文本或完整请求体。面向调用方的错误 MUST 使用统一错误结构并区分 `invalid_argument`、`unavailable` 与 `conflict`；持久化错误 MUST 经过脱敏，MUST NOT 包含引擎原始堆栈。

#### Scenario: 出站调用遥测

- **WHEN** 发布或触发访问执行引擎
- **THEN** 记录依赖耗时与结果指标，span 不包含凭据、SQL 或请求体

#### Scenario: 对账任务遥测

- **WHEN** 对账任务执行
- **THEN** 记录任务结果计数

#### Scenario: 错误结构一致

- **WHEN** 引擎返回 400 或 503
- **THEN** 调用方收到统一 `{error:{code,message,retryable}}` 结构，可重试语义正确

#### Scenario: 错误与日志脱敏

- **WHEN** 引擎错误包含连接串或堆栈
- **THEN** 持久化错误与日志中不出现凭据、DSN 或堆栈原文

## MODIFIED Requirements

### Requirement: Pipeline publishing and immutable versions

系统 SHALL 在发布 Pipeline 时把当前 `dag` 快照写入 `pipeline.versions`，递增 Pipeline 的 `version`，将 `status` 置为 `published`，并调用 Kestra adapter 登记 Flow。发布 MUST 先校验 authoring DAG v1 契约并生成与该版本绑定的 Flow 定义；只有 Flow 登记成功后才提交版本快照与状态变更，登记失败 MUST 保持原 `version`、`status` 与既有版本不变并返回 `unavailable`。已发布版本 MUST 不可修改；每次重新发布 MUST 生成新的递增版本号。`archived` 状态的 Pipeline MUST 拒绝发布。

#### Scenario: 首次发布

- **WHEN** 调用方发布一个 `draft` 状态的 Pipeline
- **THEN** 系统返回 200，`version=1`、`status=published`，并存在 `version=1` 的版本快照
- **AND** Kestra adapter 收到一次 Flow 登记请求

#### Scenario: 重新发布递增版本

- **WHEN** 调用方修改定义后再次发布同一个 Pipeline
- **THEN** 系统返回 `version=2`，`pipeline.versions` 同时保留版本 1 和版本 2
- **AND** 版本 1 的 `dag` 不因再次发布而变化

#### Scenario: 归档拒绝发布

- **WHEN** 调用方发布一个 `status=archived` 的 Pipeline
- **THEN** 系统返回 409 `conflict`
- **AND** 不新增版本记录，不调用 Kestra adapter

#### Scenario: 修改已发布定义回到草稿

- **WHEN** 调用方修改一个 `status=published` Pipeline 的 `dag`
- **THEN** 该 Pipeline 的 `status` 变为 `draft`，`version` 保持当前已发布版本号
- **AND** 再次触发运行返回 409 `conflict`，直到发布新版本
- **AND** 已发布版本的 `dag` 快照保持不变

#### Scenario: 发布时 DAG 校验失败

- **WHEN** 调用方发布一个不满足 authoring DAG v1 契约的定义
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不新增版本记录，不调用 Kestra adapter，`version` 与 `status` 不变

#### Scenario: Flow 登记失败

- **WHEN** Kestra adapter 在发布过程中登记 Flow 失败
- **THEN** 系统返回 503 `unavailable`
- **AND** 不新增版本记录，`version` 与 `status` 保持不变
- **AND** 重试成功后版本只递增一次，不产生重复版本

### Requirement: Pipeline run entry

系统 SHALL 提供受租户保护的 Pipeline 运行入口。只有 `status=published` 的 Pipeline 可以触发运行；触发 MUST 创建一条 `pipeline.runs` 记录并通过 Kestra adapter 执行，`trigger_type` MUST 为 `manual`，并 MUST 持久化执行引擎返回的执行引用。adapter 返回失败时，运行记录 MUST 标记为 `failed` 且不得伪造成功；同一运行触发的重试 MUST 复用已存在的执行，MUST NOT 创建第二次执行。

#### Scenario: 触发已发布 Pipeline

- **WHEN** 调用方触发一个已发布 Pipeline 的运行
- **THEN** 系统返回 202，运行记录 `status=running`、`version` 等于当前发布版本、`started_at` 非空
- **AND** Kestra adapter 收到一次 TriggerRun 请求，运行记录持久化执行引用

#### Scenario: 草稿 Pipeline 拒绝触发

- **WHEN** 调用方触发一个 `draft` 或 `archived` Pipeline 的运行
- **THEN** 系统返回 409 `conflict`
- **AND** 不创建运行记录，不调用 Kestra adapter

#### Scenario: adapter 失败

- **WHEN** Kestra adapter 在触发时返回错误
- **THEN** 系统返回 503 `unavailable`，并存在一条 `status=failed` 的运行记录
- **AND** 运行记录包含非敏感错误说明，不保存 adapter 的原始凭证或堆栈

#### Scenario: 触发中途失败后恢复

- **WHEN** 触发时执行已创建但响应丢失
- **THEN** 同一运行的重试复用该执行，不创建第二次执行
- **AND** 运行最终进入 `running`，执行引用被持久化

### Requirement: Pipeline run query

系统 SHALL 支持按租户查询单次运行与某 Pipeline 的运行列表。非终态运行 MUST 在返回前与执行引擎对账并写回状态；终态 `succeeded`、`failed` 或 `cancelled` MUST NOT 回退；引擎不可达时 MUST 返回已持久化状态。运行状态 MUST 为 `queued`、`running`、`succeeded`、`failed` 或 `cancelled`；列表 MUST 按 `started_at DESC` 排序并支持 `page`/`per_page` 分页。

#### Scenario: 查询运行列表

- **WHEN** 调用方以 `page=1&per_page=20` 查询已存在 Pipeline 的运行
- **THEN** 系统返回 200，包含 `runs` 数组与 `page` 分页对象
- **AND** 排序为 `started_at DESC`

#### Scenario: 跨租户读取运行

- **WHEN** 租户 B 按 ID 读取租户 A 的 Pipeline 或运行
- **THEN** 系统返回 404 `not_found`

#### Scenario: 查询时对账写回

- **WHEN** 查询一个非终态运行且引擎执行已成功完成
- **THEN** 系统返回的运行状态为 `succeeded`，`finished_at` 非空
- **AND** 该状态被持久化，后续查询不再依赖引擎

#### Scenario: 引擎不可达时返回持久化状态

- **WHEN** 查询运行状态时执行引擎不可达
- **THEN** 系统返回 200 与已持久化状态
- **AND** 不把运行标记为成功或伪造终态

#### Scenario: 执行不存在

- **WHEN** 引擎在有限重试后仍返回执行不存在
- **THEN** 运行状态标记为 `failed`，原因为 `execution_not_found`

### Requirement: Kestra adapter boundary

系统 SHALL 通过 `kestra.Adapter` 接口隔离执行引擎：发布 Pipeline 时调用 `RegisterFlow`，触发运行时调用 `TriggerRun`，对账运行状态时调用执行查询方法。真实部署 MUST 使用连接真实 Kestra 的 adapter；内存 adapter MUST 只用于测试或显式本地模式，MUST NOT 作为静默默认。模块 MUST NOT 自行实现 DAG 引擎、cron 调度器或执行器。未注入 adapter 或 adapter 未配置时服务 MUST fail-closed 返回 `unavailable`。

#### Scenario: 默认内存 adapter

- **WHEN** 测试或显式本地模式下模块以内存 adapter 构造并发布、触发一个 Pipeline
- **THEN** 所有 Flow 登记与运行请求只记录在内存 adapter 中
- **AND** 不发起任何网络请求

#### Scenario: adapter 未配置

- **WHEN** 服务未注入 Kestra adapter 或 adapter 未配置时发布或触发 Pipeline
- **THEN** 系统返回 503 `unavailable`
- **AND** 不创建版本/运行记录，不伪造执行成功

#### Scenario: 真实 adapter 访问

- **WHEN** 部署配置了执行引擎地址
- **THEN** 发布、触发与对账通过 HTTP 访问该地址并注入 traceparent
- **AND** 凭据不出现在 Flow、日志或错误响应中

#### Scenario: 引擎不可达 fail-closed

- **WHEN** 真实 adapter 访问引擎失败
- **THEN** 系统返回 503 `unavailable`
- **AND** 不返回成功，也不留下部分版本或运行成功记录

### Requirement: Dataset dependency validation

pipelines 模块 MUST 声明依赖 `datasets`。当 Pipeline 的 `dag` 在 `inputs[].dataset_id`、顶层 `dataset_ids` 或 `outputs[].dataset_id` 中引用 dataset 时，发布与触发 MUST 通过 `service.DatasetResolver` 校验其存在且属于当前租户；resolver 未注入时 MUST fail-closed 返回 `unavailable`，dataset 不存在时 MUST 返回 400 `invalid_argument`。

#### Scenario: 引用不存在 dataset

- **WHEN** Pipeline 定义引用一个 resolver 判定不存在的 dataset 并尝试发布
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不创建版本记录

#### Scenario: resolver 未注入

- **WHEN** Pipeline 定义引用 dataset 且服务未注入 `DatasetResolver`
- **THEN** 系统返回 503 `unavailable`
- **AND** 不调用 Kestra adapter

#### Scenario: 输出 dataset 不存在

- **WHEN** Pipeline 的 `outputs[]` 引用 resolver 判定不存在的 dataset 并尝试发布
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不创建版本记录，不调用 Kestra adapter

#### Scenario: 跨租户 dataset 引用

- **WHEN** Pipeline 输出引用一个属于其他租户的 dataset
- **THEN** resolver 判定其不存在，系统返回 400 `invalid_argument`
- **AND** 不创建版本或运行记录
