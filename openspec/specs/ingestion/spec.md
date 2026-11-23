# ingestion Specification

## Purpose
让租户能够发起、跟踪和管理连接器同步任务，并以可查询的运行记录呈现每次同步的状态、统计与错误，为数据接入提供可审计的执行视图。

## Requirements

### Requirement: 同步任务创建

系统 SHALL 为存在且为 `active` 状态的 Connector 创建同步任务，初始状态 MUST 为 `queued`，并同时创建一条初始运行记录。

#### Scenario: 为活跃连接器创建任务

- **WHEN** 租户为 `active` 状态的 Connector 提交同步任务创建请求
- **THEN** 系统返回 `queued` 状态的任务，且该任务存在一条初始运行记录

#### Scenario: 连接器不存在

- **WHEN** 租户使用不存在的 Connector 标识创建同步任务
- **THEN** 系统返回未找到，且不创建任务或运行记录

#### Scenario: 暂停连接器拒绝创建任务

- **WHEN** 租户为 `paused` 状态的 Connector 创建同步任务
- **THEN** 系统返回冲突错误，且不创建任务

### Requirement: 同步任务状态机

系统 SHALL 只允许 `queued → running → succeeded | failed` 以及 `queued | running → cancelled` 的状态迁移；终态 MUST 不可再变更，非法迁移 MUST 返回冲突错误。

#### Scenario: 任务从排队进入运行

- **WHEN** 运行器把 `queued` 任务标记为 `running`
- **THEN** 任务状态为 `running` 且记录开始时间

#### Scenario: 任务成功结束

- **WHEN** 运行器把 `running` 任务标记为 `succeeded`
- **THEN** 任务状态为 `succeeded` 且记录结束时间

#### Scenario: 终态任务不可再变更

- **WHEN** 对 `succeeded` 或 `cancelled` 的任务再次请求状态变更
- **THEN** 系统返回冲突错误，且原状态保持不变

### Requirement: 同步任务查询与列表

系统 SHALL 提供按标识查询与分页列表能力；列表 MUST 支持按 `connector_id` 过滤、`page`/`per_page` 分页（默认第 1 页、每页 20 条，最大 100 条），并按创建时间倒序返回。

#### Scenario: 按连接器过滤任务

- **WHEN** 客户端使用 `connector_id` 查询任务列表
- **THEN** 返回的任务全部属于该 Connector

#### Scenario: 分页信息完整

- **WHEN** 客户端请求任务列表
- **THEN** 响应包含当前页任务与页码、每页数量、总数、总页数

### Requirement: 同步任务取消

系统 SHALL 允许取消 `queued` 或 `running` 状态的同步任务。取消 `queued` 任务后 MUST 直接置为 `cancelled` 并记录结束时间，MUST NOT 创建运行时资源；取消 `running` 任务 MUST 先向运行时传播停止，运行时确认停止后 MUST 置为 `cancelled` 并记录结束时间。运行时停止未获确认时 MUST 返回错误并保持 `running` 供重试，MUST NOT 报告已取消。终态任务再次取消 MUST 返回冲突且不改变状态。

#### Scenario: 取消排队任务

- **WHEN** 租户取消一个 `queued` 任务
- **THEN** 任务状态变为 `cancelled` 并返回更新后的任务，运行时不出现新连接器

#### Scenario: 停止未被确认

- **WHEN** 取消 `running` 任务时运行时不可达或拒绝停止请求
- **THEN** 系统返回错误，任务保持 `running` 供重试，且不报告已取消

#### Scenario: 取消已结束任务被拒绝

- **WHEN** 租户取消一个 `succeeded` 或 `failed` 任务
- **THEN** 系统返回冲突错误，且原状态保持不变

### Requirement: 同步任务监听

系统 SHALL 提供任务状态监听能力，在任务状态变化时推送最新状态，并在任务进入终态后结束监听。

#### Scenario: 监听终态任务立即收到结果

- **WHEN** 客户端监听一个已经处于 `cancelled` 状态的任务
- **THEN** 监听立即推送该任务状态并结束

#### Scenario: 监听中的任务进入终态

- **WHEN** 客户端监听一个 `queued` 或 `running` 任务且任务随后进入终态
- **THEN** 监听推送终态更新后结束，不继续无限等待

### Requirement: 同步运行状态投影

系统 SHALL 为每个同步任务维护运行记录，运行记录 MUST 包含状态、统计信息、错误信息与开始/结束时间，且状态变化与任务状态保持一致。

#### Scenario: 任务开始运行同步更新运行记录

- **WHEN** 任务从 `queued` 变为 `running`
- **THEN** 对应运行记录状态为 `running` 并记录开始时间

#### Scenario: 任务失败记录错误

- **WHEN** 任务以失败结束并带有错误信息
- **THEN** 对应运行记录状态为 `failed`，保存错误信息与结束时间

#### Scenario: 查询任务运行记录

- **WHEN** 客户端按任务标识查询运行记录
- **THEN** 系统返回该任务的运行记录列表，按开始时间倒序排列

### Requirement: 同步任务租户隔离

系统 MUST 对所有同步任务与运行记录操作要求有效租户上下文；缺失或非法租户标识 MUST 被拒绝，跨租户访问 MUST 表现为未找到且不得泄露记录是否存在。

#### Scenario: 缺少租户标识被拒绝

- **WHEN** 请求未携带租户标识
- **THEN** 系统返回参数错误，且不执行任何数据操作

#### Scenario: 跨租户任务不可见

- **WHEN** 租户 A 使用租户 B 的任务标识查询或取消任务
- **THEN** 系统返回未找到，且不修改租户 B 的任务

### Requirement: 同步作业调度与运行时提交

系统 SHALL 调度 `queued` 同步作业并把其提交到运行时。调度 MUST 在租户上下文中读取 Connector 与配置、生成运行时连接器并提交；提交被接受后作业 MUST 进入 `running` 并记录运行时标识。提交前校验失败、Connector 状态不允许或运行时不可用 MUST 使作业进入 `failed` 并记录脱敏原因，MUST NOT 留下可被重复调度并重复提交的状态。通过既有创建接口创建的作业 MUST 自动进入同一调度流程。

#### Scenario: 排队作业被提交

- **WHEN** 存在 `queued` 作业且 Connector 为 `active`、运行时可达
- **THEN** 系统提交运行时连接器，把作业置为 `running` 并记录运行时标识

#### Scenario: 运行时不可达

- **WHEN** 调度时 Kafka Connect 不可达
- **THEN** 作业进入 `failed` 并记录脱敏的不可用原因，不提交任何运行时连接器

#### Scenario: Connector 已暂停

- **WHEN** 调度前发现 Connector 已不是 `active`
- **THEN** 作业进入 `failed`，且不创建运行时连接器

### Requirement: 调度幂等与并发约束

系统 MUST 保证每个作业最多提交一次运行时连接器：同一作业被重复调度或重复触发 MUST 不产生重复运行时资源，重复对账/回调 MUST 幂等。多实例并发调度 MUST 通过条件更新或租约机制保证独占。同一 Connector 已存在运行中作业时，新作业 MUST 被拒绝或以冲突状态结束，不得并发写同一目标。

#### Scenario: 并发调度仅一方提交

- **WHEN** 两个调度实例同时尝试处理同一 `queued` 作业
- **THEN** 仅一方完成提交，另一方按幂等语义跳过

#### Scenario: 重复触发不重复提交

- **WHEN** 同一作业被重复调度或对账回调重复到达
- **THEN** 不产生第二个运行时连接器，也不重复追加运行记录

#### Scenario: 同一 Connector 并发同步被拒绝

- **WHEN** 同一 Connector 已有 `running` 作业时又创建或调度同步
- **THEN** 新作业以冲突结束，不并发写入同一目标

### Requirement: 运行时状态对账与回写

系统 SHALL 周期对账 `running` 作业的运行时状态并回写任务与运行记录。对账映射 MUST：运行时运行中→`running`；运行时失败→`failed` 并记录脱敏错误；作业运行中但运行时暂停、停止或缺失→`failed` 并记录可复核原因；`unknown` MUST NOT 映射为运行中。终态作业 MUST NOT 被对账改写，运行记录统计 MUST 与任务状态保持一致。

#### Scenario: 运行中状态保持

- **WHEN** 对账发现运行时连接器与其任务均运行中
- **THEN** 作业保持 `running`，运行记录状态一致

#### Scenario: 运行时失败回写

- **WHEN** 对账发现运行时任务失败
- **THEN** 作业进入 `failed`，运行记录记录脱敏错误

#### Scenario: 运行时缺失

- **WHEN** 作业仍为 `running` 但对账时运行时连接器缺失
- **THEN** 作业进入 `failed`，并记录运行时缺失原因

#### Scenario: 终态不被改写

- **WHEN** 对账扫描到 `succeeded`、`failed` 或 `cancelled` 作业
- **THEN** 对账不修改其状态、统计或完成时间

### Requirement: 初始快照完成与成功确认

系统 MUST 只在运行时确认初始快照完成且目标数据可读后才把作业置为 `succeeded`；MUST NOT 仅凭运行时运行状态、定时器或重试次数判定成功。成功时 MUST 记录完成时间、行数等运行统计与目标 Dataset 引用，并 MUST 尝试停止运行时连接器。停止失败 MUST 记录为待清理项，MUST NOT 谎报运行时已停止，也 MUST NOT 撤销已完成状态。

#### Scenario: 快照完成后判定成功

- **WHEN** 运行时确认初始快照完成，且目标数据可读并具备预期行数统计
- **THEN** 作业置为 `succeeded`，运行记录含完成时间、统计与 Dataset 引用

#### Scenario: 仅运行中不判定成功

- **WHEN** 运行时连接器为运行中但尚未确认初始快照完成
- **THEN** 作业保持 `running`，不被置为 `succeeded`

#### Scenario: 完成后停止失败

- **WHEN** 作业已成功但停止运行时连接器失败
- **THEN** 作业保持 `succeeded`，同时记录待清理项，不报告运行时已停止

### Requirement: 取消与运行时停止传播

系统 SHALL 在取消 `queued` 或 `running` 作业时传播到运行时：`queued` 且未提交 MUST 直接取消且不创建运行时资源；`running` MUST 调用运行时停止并在确认后置为 `cancelled`；停止未获确认 MUST 返回错误并保持 `running`（可重试），MUST NOT 报告已取消。终态作业再次取消 MUST 返回冲突。

#### Scenario: 取消排队作业

- **WHEN** 租户取消尚未提交的 `queued` 作业
- **THEN** 作业置为 `cancelled`，运行时不出现新连接器

#### Scenario: 取消运行中作业

- **WHEN** 租户取消 `running` 作业且运行时停止被确认
- **THEN** 作业置为 `cancelled`，运行记录含完成时间与取消原因

#### Scenario: 停止未被确认

- **WHEN** 运行时不可达或拒绝停止请求
- **THEN** 系统返回错误，作业保持 `running` 供重试

#### Scenario: 终态不可再取消

- **WHEN** 租户取消已处于终态的作业
- **THEN** 系统返回冲突，且不改变状态

### Requirement: 重试、退避与失败分类

系统 MUST 对可重试失败（运行时 5xx、超时、连接失败）按有限指数退避重试，重试次数与最后失败原因 MUST 记录在运行统计；不可重试失败（参数错误、配置非法、认证失败、冲突）MUST 立即失败。重试耗尽 MUST 进入 `failed`，MUST NOT 无限重试或把耗尽计为成功；每个作业 MUST 只接受一个终态。

#### Scenario: 瞬时失败后重试成功

- **WHEN** 提交首次遇到运行时 5xx，随后重试成功
- **THEN** 作业最终进入 `running`，运行统计记录重试次数

#### Scenario: 重试耗尽

- **WHEN** 可重试失败达到重试上限
- **THEN** 作业进入 `failed`，运行统计记录尝试次数与最后失败原因

#### Scenario: 不可重试失败立即终止

- **WHEN** 提交因配置非法或认证失败被运行时拒绝
- **THEN** 作业立即进入 `failed`，不继续重试

### Requirement: 同步运行观测

系统 SHALL 产生符合 `runtime-observability` 基线的同步观测：作业计数与结果、调度/对账耗时或滞后、重试计数 MUST 使用 `ontoaxis_` 前缀、允许的单位后缀与标签白名单；MUST NOT 使用租户、作业、Connector 或运行时连接器标识作为指标标签。trace MUST 覆盖调度、提交、对账与取消路径，MUST NOT 记录凭据或请求体。

#### Scenario: 作业结果指标

- **WHEN** 作业进入终态
- **THEN** 作业计数指标按结果标签递增，标签不含高基数标识

#### Scenario: 对账链路可追踪

- **WHEN** 对账器处理一批 `running` 作业
- **THEN** 产生带操作与结果的模块 span，且不记录敏感字段

#### Scenario: 重试可观测

- **WHEN** 提交或对账发生重试
- **THEN** 重试指标与日志记录重试次数，且不含凭据

### Requirement: 后台调度与对账的租户隔离

后台调度、对账与取消传播 MUST 在对应租户上下文中读写：MUST NOT 跨租户查询或回写任务/运行记录，MUST NOT 触碰其他租户的运行时连接器。跨租户访问作业 MUST 表现为未找到，且不产生副作用。

#### Scenario: 对账只处理本租户作业

- **WHEN** 对账器处理租户 A 的作业
- **THEN** 只读写租户 A 的任务与运行记录，且只访问租户 A 的运行时命名空间

#### Scenario: 跨租户取消无副作用

- **WHEN** 租户 A 请求取消租户 B 的作业
- **THEN** 系统返回未找到，且租户 B 的作业与运行时连接器保持原状态
