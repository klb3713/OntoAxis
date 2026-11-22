# actions Specification

## Purpose
定义 OntoAxis Stage 1 的动作编排最小契约：Action Type 元数据、校验/预演、执行记录与可替换 adapter 边界，确保真实动作执行可在 Stage 2 接入而不改变 API 形状。

## Requirements

### Requirement: Action Type 管理

系统 SHALL 支持租户内 Action Type 的创建、读取、更新、列表与软删除；Action Type MUST 名称唯一、状态限定为 `active/draft/disabled`，删除后默认查询 MUST NOT 返回。

#### Scenario: 创建 Action Type

- **WHEN** 调用方在有效租户下提交名称与可选参数定义
- **THEN** 系统返回 201，并返回带 `id`、`tenant_id`、`name`、`status` 与时间戳的 Action Type

#### Scenario: 拒绝空名称

- **WHEN** 调用方创建 Action Type 时名称为空
- **THEN** 系统返回 400，错误码为 `bad_request`，且不创建对象

#### Scenario: 重复名称冲突

- **WHEN** 调用方在同一租户创建同名未删除 Action Type
- **THEN** 系统返回 409，错误码为 `conflict`

### Requirement: 校验与预演

系统 SHALL 提供 Action 校验与预演；校验 MUST 检查 Action Type 存在、状态为 `active`、执行定义有效，并检查 `parameters.required` 中声明的必填参数与类型；预演 MUST 返回校验结果、执行模式（`temporal` 或 `noop`）、是否需要人工审批、步骤摘要与预计超时，且 MUST NOT 产生外部副作用或创建执行记录。预演返回的执行模式 MUST 与实际提交时使用的一致，MUST NOT 把未配置的真实引擎报告为可用。

#### Scenario: 缺少必填参数

- **WHEN** 调用方对要求 `reason` 的 Action Type 提交空参数
- **THEN** 系统返回 `valid=false` 与 `missing required parameter "reason"`

#### Scenario: 预演无副作用

- **WHEN** 调用方预演一个合法 Action
- **THEN** 系统返回执行模式、参数与审批要求，且不产生外部副作用、不创建执行记录

#### Scenario: 预演有效动作

- **WHEN** 对有效 Action Type 与合法参数发起预演
- **THEN** 返回 `valid=true`、执行模式、审批要求与步骤摘要，且执行记录数量不变

#### Scenario: 预演拒绝非法参数

- **WHEN** 缺少必填参数或参数类型不符
- **THEN** 返回校验失败与字段级错误，不产生任何副作用

#### Scenario: 预演如实报告执行模式

- **WHEN** 部署未配置真实引擎而以显式 no-op 模式运行
- **THEN** 预演报告 `noop`，不得报告为真实执行模式

### Requirement: 执行入口与 Adapter

系统 SHALL 支持创建 Action Execution；Run MUST 在参数校验通过后创建状态 `queued` 的执行记录，把当前执行定义快照写入执行记录，并通过可替换的 Adapter 提交外部工作流引擎执行；Adapter MUST NOT 在请求线程同步执行业务步骤。提交失败时系统 MUST 将执行投影为 `failed`（原因 `engine_unavailable`）并返回可重试的 `engine_unavailable` 错误，MUST NOT 返回成功或伪造 `running` 状态。no-op adapter MUST 仅在显式配置的降级模式下保留用于本地无外部依赖测试，执行详情与预演 MUST 如实标注该模式；不得静默从真实引擎回落到 no-op。

#### Scenario: 运行合法 Action

- **WHEN** 调用方运行合法 Action Type 与参数
- **THEN** 系统返回 200 与状态为 `queued` 的 Execution，并由适配器提交真实工作流

#### Scenario: 运行非法 Action Type

- **WHEN** 调用方对不存在或非 active 的 Action Type 运行
- **THEN** 系统拒绝并返回 404 或 400

#### Scenario: 成功提交执行

- **WHEN** 参数与授权校验通过且外部工作流引擎可用
- **THEN** 系统创建 `queued` 执行并成功提交 workflow，返回执行标识与状态

#### Scenario: 引擎不可用失败关闭

- **WHEN** 外部工作流引擎不可达或拒绝请求
- **THEN** 系统返回可重试的 `engine_unavailable` 错误，执行被标记为 `failed`，不被报告为成功

#### Scenario: 显式 no-op 降级

- **WHEN** 部署显式配置 no-op 模式并提交执行
- **THEN** 执行详情与预演标注 `noop`，不出现真实引擎执行证据

### Requirement: 执行记录与状态操作

系统 SHALL 支持按租户与可选 Action Type 列出执行记录、按 ID 查询、取消与重试。读取 MUST 返回执行状态、步骤进度、尝试次数、审批状态与工作流引用；取消 MUST 拒绝 `succeeded`、`failed`、`cancelled`、`timed_out`、`compensation_failed` 等终态执行，允许 `queued`、`running`、`waiting_approval`；重试 MUST 只允许 `failed`、`timed_out`、`compensation_failed` 与 `cancelled`，并 MUST 创建新的 `queued` 执行、重新执行校验与授权，MUST NOT 修改原执行；非法状态操作 MUST 返回 `conflict`。等待审批或补偿中的执行被取消时 MUST 走补偿路径。

#### Scenario: 取消已取消执行

- **WHEN** 调用方取消状态为 `cancelled` 的 Execution
- **THEN** 系统返回 400 或 409 冲突，且状态不变

#### Scenario: 重试失败执行

- **WHEN** 调用方重试状态为 `failed` 的 Execution
- **THEN** 系统创建一个新的 `queued` Execution，原执行保持终态不变

#### Scenario: 取消待审批执行

- **WHEN** 对 `waiting_approval` 执行发起取消
- **THEN** 系统取消审批等待并补偿已完成步骤，最终状态为 `cancelled`

#### Scenario: 取消成功执行被拒绝

- **WHEN** 对 `succeeded` 执行发起取消
- **THEN** 系统返回 `conflict` 且执行不变

#### Scenario: 执行详情包含进度

- **WHEN** 查询运行中执行
- **THEN** 返回状态、步骤进度、尝试次数、审批状态与工作流引用

### Requirement: 租户边界与错误契约

系统 SHALL 在所有 `/api/v1/actions` 读写下使用请求租户；租户 MUST 来自可信上下文或 `X-Tenant-Id`，缺失或非法时 MUST 返回统一错误信封中的 `unauthorized`。跨租户资源访问 MUST 返回 `not_found`，不得泄露存在性。授权拒绝 MUST 返回 `permission_denied`（含稳定原因码），授权或审计不可用 MUST 返回 `service_unavailable`，且上述情况 MUST NOT 产生副作用。workflow、activity、审批任务与幂等记录 MUST 携带并校验租户，MUST NOT 跨租户读写。错误信封 MUST 包含 `code`、`message`、`metadata`、`retryable` 与 `request_id`，MUST NOT 暴露底层 SQL、引擎错误或内部堆栈。

#### Scenario: 缺少租户

- **WHEN** 请求无租户上下文与合法 `X-Tenant-Id`
- **THEN** 系统返回 401，错误码为 `unauthorized`

#### Scenario: 资源不存在

- **WHEN** 调用方读取当前租户内不存在的 Action Type/Execution，或使用租户 A 上下文访问租户 B 的资源
- **THEN** 系统返回 404，错误码为 `not_found`，响应包含 `request_id`

#### Scenario: 授权拒绝的错误契约

- **WHEN** 授权守卫拒绝某 actions 操作
- **THEN** 返回 `permission_denied` 与稳定原因码，`retryable=false`，无副作用

#### Scenario: 内部错误脱敏

- **WHEN** 数据库或工作流引擎返回内部错误
- **THEN** 错误信封只包含稳定错误码与 `request_id`，不包含原始错误文本或堆栈

### Requirement: Action Type 执行定义

系统 SHALL 允许 Action Type 声明可校验的执行定义，用于描述真实执行所需的步骤、活动类型、重试策略、审批要求与超时；创建与更新 MUST 校验定义的完整性、活动类型已知性、步骤数量上限、重试与超时取值范围以及审批配置合法性，非法定义 MUST 返回 `invalid_definition` 且不写入任何记录。系统 MUST 在执行提交时把当前执行定义快照到执行记录，之后对 Action Type 定义的修改 MUST NOT 影响已提交的执行。仅状态为 `active` 的 Action Type MUST 被 Run 接受；`draft` 或 `disabled` MUST 被拒绝。

#### Scenario: 创建带有效执行定义的 Action Type

- **WHEN** 租户调用创建 Action Type 并提交步骤、重试、审批与超时都在允许范围内的执行定义
- **THEN** 系统返回创建成功的 Action Type，定义可被后续预演与执行读取

#### Scenario: 拒绝非法执行定义

- **WHEN** 请求中的执行定义引用了未知活动类型、步骤数超上限，或重试/超时取值越界
- **THEN** 系统返回 `invalid_definition` 错误，且不创建或更新 Action Type

#### Scenario: 定义变更不影响在途执行

- **WHEN** 某执行已提交后再修改其 Action Type 的执行定义
- **THEN** 该执行继续使用提交时快照的定义，新的执行才使用新定义

#### Scenario: 非 active 类型不可执行

- **WHEN** 对 `draft` 或 `disabled` 状态的 Action Type 发起 Run
- **THEN** 系统拒绝请求且不创建执行记录

### Requirement: 执行状态与终态定义

系统 SHALL 将执行状态限定为 `queued`、`running`、`waiting_approval`、`compensating`、`succeeded`、`failed`、`cancelled`、`timed_out`、`compensation_failed`；其中 `succeeded`、`failed`、`cancelled`、`timed_out`、`compensation_failed` MUST 为终态且不可再变更。

#### Scenario: 终态不可变更

- **WHEN** 对已 `succeeded` 或 `failed` 的执行发起任何状态操作
- **THEN** 系统拒绝操作且不修改终态与时间戳

### Requirement: 状态转换与并发控制

系统 SHALL 遵循以下状态转换：`queued` 只能进入 `running` 或 `cancelled`；`running` 可进入 `waiting_approval`、`compensating`、`succeeded`、`failed`、`timed_out`；`waiting_approval` 可进入 `running`、`compensating`、`cancelled`、`timed_out`；`compensating` 可进入 `failed`、`cancelled`、`timed_out`、`compensation_failed`。并发或冲突的状态转换 MUST 通过条件更新只允许一个成功者，其余 MUST 被拒绝并记录，客户端 MUST NOT 观察到状态倒退。

#### Scenario: 成功路径的状态推进

- **WHEN** 一个无需审批的执行提交并由 worker 开始执行且所有步骤成功
- **THEN** 状态依次出现 `queued`、`running`、`succeeded`，且 `succeeded` 之后不再变化

#### Scenario: 并发取消与完成竞争

- **WHEN** 取消请求与执行完成几乎同时到达
- **THEN** 只有一方生效，另一方收到冲突错误，最终状态稳定在单一终态

### Requirement: 幂等与重复提交

系统 SHALL 要求 `POST` 创建与命令接口携带 `Idempotency-Key`；同一租户同一 key 的相同请求 MUST 返回首次结果，且 MUST NOT 重复创建执行或重复触发 workflow；同一 key 携带不同请求体 MUST 返回 `already_exists` 冲突；缺少该头的创建/命令请求 MUST 返回 `idempotency_key_required`。执行、workflow 与 activity MUST 以执行标识与步骤序号构成幂等键，activity 至少一次执行语义下的重试 MUST NOT 重复产生业务副作用；重复启动同一执行 MUST 被识别为同一执行，而不是新建执行。

#### Scenario: 相同 key 重复提交返回同一执行

- **WHEN** 客户端使用相同 `Idempotency-Key` 与相同请求体重复提交 Run
- **THEN** 系统返回首次创建的执行标识，执行与 workflow 均不重复创建

#### Scenario: 相同 key 不同请求体冲突

- **WHEN** 客户端使用相同 `Idempotency-Key` 但请求体不同再次提交
- **THEN** 系统返回 `already_exists` 冲突且不产生第二个执行

#### Scenario: activity 重试不重复副作用

- **WHEN** activity 执行成功但响应丢失，引擎按重试策略再次调度同一 activity
- **THEN** 幂等键使该步骤的副作用只应用一次，步骤记录保持一条

#### Scenario: 缺少幂等键被拒绝

- **WHEN** 创建或命令请求缺少 `Idempotency-Key`
- **THEN** 系统返回 `idempotency_key_required` 且不产生副作用

### Requirement: 失败重试与退避

系统 SHALL 为每个执行步骤应用有上限的重试与退避策略；默认最多 3 次尝试、初值 1 秒、倍率 2、最大间隔 30 秒，执行定义可在允许范围内覆盖。可重试错误（引擎不可用、网络失败、上游 5xx、并发冲突）MUST 按退避重试；不可重试错误（参数非法、授权拒绝、资源不存在、审批拒绝）MUST 立即失败且不重试。重试次数、最后一次尝试时间与错误摘要 MUST 在执行详情中对客户端可见；重试耗尽 MUST 进入失败路径并触发补偿。系统 MUST NOT 将仍在重试的执行报告为成功。

#### Scenario: 瞬时失败后重试成功

- **WHEN** 步骤第一次执行遇到可重试错误、第二次成功
- **THEN** 执行继续推进并在详情中显示尝试次数为 2

#### Scenario: 重试耗尽进入失败与补偿

- **WHEN** 某步骤持续返回可重试错误并耗尽重试上限
- **THEN** 执行进入失败路径并补偿此前已成功步骤，状态最终为 `failed`

#### Scenario: 不可重试错误立即失败

- **WHEN** 某步骤返回参数非法或授权拒绝等不可重试错误
- **THEN** 系统立即终止重试并进入补偿或失败，不进行额外尝试

### Requirement: 补偿

系统 SHALL 在某步骤于先前步骤已产生副作用后失败、被取消或超时时，按已完成步骤的逆序执行补偿。补偿 MUST 幂等、可重试且带独立超时；补偿完成后执行 MUST 进入 `failed`、`cancelled` 或 `timed_out` 中与起因对应的终态；补偿自身重试耗尽 MUST 进入 `compensation_failed`，保留不可回滚副作用清单，且 MUST NOT 报告为成功。补偿结果与残留副作用 MUST 可查询并写入审计。

#### Scenario: 后续步骤失败触发逆序补偿

- **WHEN** 步骤一已产生副作用、步骤二永久失败
- **THEN** 系统补偿步骤一，最终状态为 `failed`，副作用清单显示已回滚

#### Scenario: 补偿失败进入独立终态

- **WHEN** 补偿操作自身持续失败并耗尽重试
- **THEN** 执行进入 `compensation_failed`，保留残留副作用记录且不标记为成功

#### Scenario: 运行中取消触发补偿

- **WHEN** 执行在步骤已产生副作用后收到取消
- **THEN** 系统停止调度新步骤、补偿已完成步骤，最终状态为 `cancelled`

### Requirement: 人工审批

系统 SHALL 在执行定义声明审批时，于对应步骤前暂停执行并使其进入 `waiting_approval`，同时创建租户内审批任务，包含执行摘要、请求时间、决定期限与状态。审批决定 MUST 经授权守卫，MUST 幂等——同一任务重复决定只生效一次；已决定或已过期的任务 MUST 拒绝再次决定。批准 MUST 使执行继续；拒绝 MUST 补偿后进入 `cancelled` 并记录 `approval_rejected`；超过期限 MUST 进入 `timed_out` 并记录 `approval_timeout`，随后补偿。审批任务列表与查询 MUST 按租户过滤并支持按状态筛选。

#### Scenario: 审批通过后继续执行

- **WHEN** 审批人在期限内批准待审批任务
- **THEN** 执行离开 `waiting_approval` 并按定义继续，最终成功或按后续结果终结

#### Scenario: 审批拒绝触发补偿

- **WHEN** 审批人拒绝待审批任务
- **THEN** 系统补偿此前已产生的副作用，执行最终为 `cancelled` 且原因可查询

#### Scenario: 重复审批决定只生效一次

- **WHEN** 同一审批任务被提交两次相同或不同的决定
- **THEN** 只有第一次生效，后续请求被拒绝且不改变任务或执行状态

#### Scenario: 审批超时

- **WHEN** 审批任务超过决定期限仍未有决定
- **THEN** 执行进入 `timed_out` 并按补偿规则处理，任务状态为过期

#### Scenario: 越权审批被拒绝

- **WHEN** 无审批权限的调用方尝试决定审批任务
- **THEN** 系统返回授权拒绝，任务与执行状态均不改变

### Requirement: 取消与超时

系统 SHALL 支持对 `queued`、`running`、`waiting_approval` 状态的执行发起取消；`queued` 取消 MUST 阻止执行启动并直接进入 `cancelled`；`running` 或 `waiting_approval` 取消 MUST 停止调度新步骤、等待或触发补偿，并按补偿结果进入 `cancelled` 或 `compensation_failed`；对终态执行取消 MUST 返回冲突。执行超时 MUST 由执行定义或引擎超时策略限定，超时 MUST 停止执行、进行补偿并进入 `timed_out`；若补偿失败则进入 `compensation_failed`。取消与超时结果 MUST 可查询并写入审计。

#### Scenario: 取消排队中的执行

- **WHEN** 对尚未开始的 `queued` 执行发起取消
- **THEN** 执行直接进入 `cancelled`，workflow 不再产生业务步骤

#### Scenario: 取消运行中的执行

- **WHEN** 对已产生副作用的 `running` 执行发起取消
- **THEN** 系统停止后续步骤并补偿，最终为 `cancelled`

#### Scenario: 执行超时

- **WHEN** 执行在限定时间内未完成
- **THEN** 系统进入 `timed_out` 并按补偿规则处理

#### Scenario: 取消终态执行被拒绝

- **WHEN** 对已处于 `succeeded` 或 `failed` 的执行发起取消
- **THEN** 系统返回冲突错误且不改变执行

### Requirement: 授权守卫

系统 SHALL 在所有 `actions` 读写端点（含执行读取、取消、重试、审批列表与审批决定）执行统一授权判定；拒绝 MUST 返回授权错误且不产生副作用、不启动或停止 workflow、不修改审批任务。授权不可用或超时 MUST fail-closed 返回 `service_unavailable`，MUST NOT 放行。授权判定 MUST 使用请求租户与资源事实（Action Type、执行及其祖先关系）；跨租户资源 MUST 先返回未找到，再谈授权。内部 workflow 与 activity MUST 仅在服务身份与执行所属租户绑定的范围内执行，MUST NOT 借内部调用绕过租户或授权边界。

#### Scenario: Run 被授权拒绝

- **WHEN** 调用方对某 Action Type 无执行权限却发起 Run
- **THEN** 系统返回授权拒绝，且不创建执行、不提交 workflow

#### Scenario: 审批决定被授权拒绝

- **WHEN** 调用方对目标审批任务无决定权限
- **THEN** 系统返回授权拒绝，任务仍为待审批

#### Scenario: 授权服务不可用

- **WHEN** 授权引擎超时或不可用
- **THEN** 系统返回 `service_unavailable` 且不产生任何副作用

#### Scenario: 跨租户执行读取被拒绝

- **WHEN** 调用方以自己的租户上下文读取另一租户的执行标识
- **THEN** 系统返回未找到，不泄露资源存在性

### Requirement: 执行审计事件

系统 SHALL 为执行生命周期写入 append-only 审计事件，至少覆盖提交、开始、步骤重试、审批请求、审批决定、补偿开始与结束、终态、取消请求与引擎失败；事件 MUST 使用平台审计公共信封与 Sink，包含事件类型、发生时间、租户、请求标识、行为主体、执行与 Action Type 标识、结果与原因码。API 命令侧的审计写入失败 MUST 阻止副作用并返回 `audit_unavailable`；worker 侧审计失败 MUST 重试并显式记录，MUST NOT 静默丢弃或伪造事件。事件 MUST NOT 包含 token、完整请求体、敏感参数值或原始错误堆栈。

#### Scenario: 提交时先审计后执行

- **WHEN** 调用方提交 Run 且授权通过
- **THEN** 在提交 workflow 前写入提交审计事件，事件可被检索

#### Scenario: 审计不可用阻止副作用

- **WHEN** 审计 Sink 写入失败
- **THEN** 系统返回 `audit_unavailable`，不创建执行或提交 workflow

#### Scenario: 脱敏

- **WHEN** 执行参数包含凭据类字段并产生审计事件
- **THEN** 事件中只出现脱敏标记，不出现原始敏感值

### Requirement: 执行可观测性

系统 SHALL 暴露 Action 执行的指标、trace 与日志：至少包括提交/终态计数、执行时延、activity 重试次数、审批等待时长与在飞执行数；指标标签 MUST 符合可观测基线白名单，MUST NOT 使用租户、用户、执行标识等无界高基数标签。worker MUST 提供 `healthz`、`readyz` 与 `metrics`，`readyz` MUST 反映 Temporal 连接与命名空间可用性。workflow 与 activity MUST 传播 trace 上下文并产生按模块与操作命名的 span，取消、重试、补偿与审批等待 MUST 在 trace 或日志中可定位。

#### Scenario: 指标可断言

- **WHEN** 完成一次成功执行与一次失败执行
- **THEN** `/metrics` 中可断言对应计数与时延指标，且标签不含租户或执行标识

#### Scenario: worker 就绪语义

- **WHEN** Temporal 不可达或命名空间不可用
- **THEN** worker 的 `readyz` 返回失败状态，恢复后重新就绪

#### Scenario: trace 贯通

- **WHEN** 执行从 API 提交推进到 activity
- **THEN** trace 中可看到 API、workflow 与 activity 的父子 span 关系

### Requirement: 审批界面

系统 SHALL 提供最小审批界面：审批人 MUST 能看到待审批任务列表（Action Type、执行摘要、请求时间与决定期限），MUST 能批准或拒绝并填写备注；界面 MUST 只通过受授权 API 读写，MUST NOT 在客户端伪造成功或绕过租户边界；决定完成后 MUST 以服务端最终状态刷新，失败 MUST 显示明确错误。界面 MUST 支持 `en`/`zh`，空态、加载态、无权限与错误态 MUST 有明确反馈。

#### Scenario: 查看待审批列表

- **WHEN** 审批人打开审批页且存在待审批任务
- **THEN** 页面展示任务摘要、请求时间与期限，并只显示当前租户数据

#### Scenario: 批准后以服务端状态刷新

- **WHEN** 审批人在页面批准任务
- **THEN** 页面用服务端返回的最终状态更新列表，不出现乐观成功

#### Scenario: 决定失败不伪造成功

- **WHEN** 审批决定因授权或服务错误失败
- **THEN** 页面显示错误并保持任务待审批，不显示已批准

#### Scenario: 无权限与空态

- **WHEN** 当前用户无审批权限或没有待审批任务
- **THEN** 页面分别展示无权限提示或空态，不产生越权请求

### Requirement: 真实实例端到端验收

本变更 SHALL 提供在真实 Temporal 实例与 PostgreSQL 上的端到端验证：至少一个 workflow 在真实实例执行，覆盖 activity 失败后的重试、后续步骤失败后的补偿以及人工审批通过分支；MUST 覆盖审批拒绝与重试耗尽两条失败路径，并验证默认执行模式已替换 Stage 1 的 no-op adapter。验证 MUST 可复现，记录命令、环境、执行标识与结果；未实际执行或环境不可用 MUST 标记为未通过或 `planned`，MUST NOT 以 mock、fake 或本地单元测试结果冒充真实实例证据。

#### Scenario: 真实实例覆盖重试、补偿与审批

- **WHEN** 在真实 Temporal 与 PostgreSQL 上运行 E2E 场景
- **THEN** 观察到 activity 重试、后续失败触发的逆序补偿与审批通过分支，并产出可复核记录

#### Scenario: 真实实例覆盖拒绝与耗尽路径

- **WHEN** 运行审批拒绝与 activity 重试耗尽两条负向场景
- **THEN** 执行分别进入 `cancelled` 与 `failed`，补偿与审计证据可查

#### Scenario: 未执行不冒充通过

- **WHEN** Temporal 或数据库环境不可用导致 E2E 未运行
- **THEN** 证据明确标记未通过或 `planned`，不得写成已验证
