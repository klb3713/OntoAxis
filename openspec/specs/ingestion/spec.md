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

系统 SHALL 允许取消 `queued` 或 `running` 状态的同步任务；取消后任务状态 MUST 为 `cancelled` 并记录结束时间。

#### Scenario: 取消排队任务

- **WHEN** 租户取消一个 `queued` 任务
- **THEN** 任务状态变为 `cancelled` 并返回更新后的任务

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
