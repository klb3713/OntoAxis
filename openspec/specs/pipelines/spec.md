# pipelines Specification

## Purpose
在 Stage 1 中提供 Pipeline 定义、发布版本、运行入口与 Schedule 的最小可用垂直切片，并用可替换 adapter 隔离 Kestra 执行引擎，为后续 Query、Ontology 与集成 change 提供稳定、可测试的 pipelines 服务边界。

## Requirements

### Requirement: Pipeline definition management

系统 SHALL 在租户范围内维护 Pipeline 定义，支持创建、读取、列出、更新与软删除。Pipeline 名称 MUST 在租户内唯一；`pipeline_type` MUST 为 `batch`、`faster` 或 `incremental`；`dag` MUST 是合法 JSON 对象。列表 MUST 默认按 `created_at DESC` 排序且过滤 `deleted_at IS NULL`。

#### Scenario: 创建 Pipeline

- **WHEN** 调用方以有效租户、唯一名称、合法类型与 JSON 对象定义创建 Pipeline
- **THEN** 系统返回 201，包含 UUIDv7 `id`、`version=0`、`status=draft` 与创建/更新时间
- **AND** 该 Pipeline 可按 ID 读取并出现在同租户列表中

#### Scenario: 同租户名称重复

- **WHEN** 同一租户再次创建同名 Pipeline
- **THEN** 系统返回 409 `already_exists`
- **AND** 不创建第二条 Pipeline 记录

#### Scenario: 非法类型或定义

- **WHEN** 创建或更新 Pipeline 时传入不在枚举内的 `pipeline_type`，或 `dag` 不是 JSON 对象
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不写入任何记录

#### Scenario: 软删除后不可见

- **WHEN** 调用方删除已存在 Pipeline，随后按相同 ID 读取
- **THEN** 删除返回 204
- **AND** 后续读取返回 404 `not_found`，列表不再包含该 Pipeline

### Requirement: Pipeline publishing and immutable versions

系统 SHALL 在发布 Pipeline 时把当前 `dag` 快照写入 `pipeline.versions`，递增 Pipeline 的 `version`，将 `status` 置为 `published`，并调用 Kestra adapter 登记 Flow。已发布版本 MUST 不可修改；每次重新发布 MUST 生成新的递增版本号。`archived` 状态的 Pipeline MUST 拒绝发布。

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

### Requirement: Pipeline run entry

系统 SHALL 提供受租户保护的 Pipeline 运行入口。只有 `status=published` 的 Pipeline 可以触发运行；触发 MUST 创建一条 `pipeline.runs` 记录并通过 Kestra adapter 执行，`trigger_type` MUST 为 `manual`。adapter 返回失败时，运行记录 MUST 标记为 `failed` 且不得伪造成功。

#### Scenario: 触发已发布 Pipeline

- **WHEN** 调用方触发一个已发布 Pipeline 的运行
- **THEN** 系统返回 202，运行记录 `status=running`、`version` 等于当前发布版本、`started_at` 非空
- **AND** Kestra adapter 收到一次 TriggerRun 请求

#### Scenario: 草稿 Pipeline 拒绝触发

- **WHEN** 调用方触发一个 `draft` 或 `archived` Pipeline 的运行
- **THEN** 系统返回 409 `conflict`
- **AND** 不创建运行记录，不调用 Kestra adapter

#### Scenario: adapter 失败

- **WHEN** Kestra adapter 在触发时返回错误
- **THEN** 系统返回 503 `unavailable`，并存在一条 `status=failed` 的运行记录
- **AND** 运行记录包含非敏感错误说明，不保存 adapter 的原始凭证或堆栈

### Requirement: Pipeline run query

系统 SHALL 支持按租户查询单次运行与某 Pipeline 的运行列表。运行状态 MUST 为 `queued`、`running`、`succeeded`、`failed` 或 `cancelled`；列表 MUST 按 `started_at DESC` 排序并支持 `page`/`per_page` 分页。

#### Scenario: 查询运行列表

- **WHEN** 调用方以 `page=1&per_page=20` 查询已存在 Pipeline 的运行
- **THEN** 系统返回 200，包含 `runs` 数组与 `page` 分页对象
- **AND** 排序为 `started_at DESC`

#### Scenario: 跨租户读取运行

- **WHEN** 租户 B 按 ID 读取租户 A 的 Pipeline 或运行
- **THEN** 系统返回 404 `not_found`

### Requirement: Schedule management

系统 SHALL 在租户范围内管理 Pipeline Schedule，支持创建、读取、列出、更新、删除、暂停与恢复。`cron` MUST 为合法 5 字段 cron 表达式；`timezone` MUST 为合法 IANA 时区；`(tenant_id, pipeline_id, cron)` MUST 唯一。暂停 MUST 置 `enabled=false` 并清空 `next_run_at`；恢复 MUST 置 `enabled=true` 并计算下一次运行时间。

#### Scenario: 创建 Schedule

- **WHEN** 调用方为已存在 Pipeline 创建 `cron="0 * * * *"`、`timezone=Asia/Shanghai` 的 Schedule
- **THEN** 系统返回 201，`enabled=true` 且 `next_run_at` 非空
- **AND** 同一租户下同一 Pipeline 的相同 cron 不得重复创建

#### Scenario: 非法 cron 或时区

- **WHEN** 调用方传入字段数不足、越界或无法解析的 cron，或非法时区
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不写入 Schedule

#### Scenario: 暂停与恢复

- **WHEN** 调用方暂停一个启用的 Schedule，随后恢复
- **THEN** 暂停后 `enabled=false`、`next_run_at` 为空
- **AND** 恢复后 `enabled=true`、`next_run_at` 重新计算且非空

#### Scenario: 删除 Schedule

- **WHEN** 调用方删除已存在的 Schedule
- **THEN** 删除返回 204，后续按 ID 查询返回 404 `not_found`

### Requirement: Kestra adapter boundary

系统 SHALL 通过 `kestra.Adapter` 接口隔离执行引擎：发布 Pipeline 时调用 `RegisterFlow`，触发运行时调用 `TriggerRun`。模块 MUST NOT 自行实现 DAG 引擎、cron 调度器或执行器，也 MUST NOT 在 Stage 1 连接真实 Kestra 实例。未注入 adapter 时服务 MUST fail-closed 返回 `unavailable`。

#### Scenario: 默认内存 adapter

- **WHEN** 模块以默认配置构造并发布、触发一个 Pipeline
- **THEN** 所有 Flow 登记与运行请求只记录在内存 adapter 中
- **AND** 不发起任何网络请求

#### Scenario: adapter 未配置

- **WHEN** 服务未注入 Kestra adapter 时发布或触发 Pipeline
- **THEN** 系统返回 503 `unavailable`
- **AND** 不创建版本/运行记录，不伪造执行成功

### Requirement: Dataset dependency validation

pipelines 模块 MUST 声明依赖 `datasets`。当 Pipeline 的 `dag` 在 `inputs[].dataset_id` 或顶层 `dataset_ids` 中引用 dataset 时，发布与触发 MUST 通过 `service.DatasetResolver` 校验其存在；resolver 未注入时 MUST fail-closed 返回 `unavailable`，dataset 不存在时 MUST 返回 400 `invalid_argument`。

#### Scenario: 引用不存在 dataset

- **WHEN** Pipeline 定义引用一个 resolver 判定不存在的 dataset 并尝试发布
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不创建版本记录

#### Scenario: resolver 未注入

- **WHEN** Pipeline 定义引用 dataset 且服务未注入 `DatasetResolver`
- **THEN** 系统返回 503 `unavailable`
- **AND** 不调用 Kestra adapter

### Requirement: Tenant-scoped API access

所有 pipelines API MUST 要求已认证的租户上下文。缺失或非法租户 MUST 返回 401 `unauthorized`；tenant resolver 未配置 MUST 返回 503 `unavailable`；跨租户读取 MUST 表现为 404 `not_found`，不得泄露其他租户资源是否存在。

#### Scenario: 缺失租户上下文

- **WHEN** 请求未携带 tenant resolver 认可的租户信息
- **THEN** 系统返回 401 `unauthorized`
- **AND** 不访问仓储层

#### Scenario: 跨租户读取 Pipeline

- **WHEN** 租户 A 创建的 Pipeline 被租户 B 按 ID 读取或更新
- **THEN** 系统返回 404 `not_found`

### Requirement: Consistent list and error contract

列表 API SHALL 使用 `page` 与 `per_page` 参数，`page` 默认 1，`per_page` 默认 50 且 MUST NOT 超过 200。所有错误响应 MUST 使用统一的 `{error:{code,message,retryable}}` 结构；未知路由 MUST 返回 404 `not_found`。

#### Scenario: 分页列表

- **WHEN** 调用方以 `page=2&per_page=1` 请求已有三条 Pipeline 的列表
- **THEN** 响应包含 1 条记录、`total=3`、`total_pages=3`、`page=2`

#### Scenario: 非法分页

- **WHEN** 调用方传入 `per_page=201`
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不执行列表查询
