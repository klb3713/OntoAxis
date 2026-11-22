# Spec Delta: dataset-catalog

## ADDED Requirements

### Requirement: Dataset catalog records

系统 SHALL 在租户范围内维护数据集目录记录，支持创建、读取、列出、更新与软删除。数据集名称 MUST 在租户内唯一；格式 MUST 为 `iceberg` 或 `parquet`；RID MUST 为 `ri.ontoaxis.main.dataset.<uuid-v7>`。

#### Scenario: 创建数据集

- **WHEN** 调用方以有效租户、唯一名称、格式 `iceberg` 和存储位置创建数据集
- **THEN** 系统返回 201，包含 UUIDv7 `id`、固定格式的 `rid`、`format=iceberg` 与创建/更新时间
- **AND** 该数据集自动拥有一个名为 `main` 的分支

#### Scenario: 同租户名称重复

- **WHEN** 同一租户再次创建同名数据集
- **THEN** 系统返回 409 `already_exists`
- **AND** 不创建第二条数据集记录

#### Scenario: 非法格式

- **WHEN** 创建数据集时传入 `format=delta` 或其他不在枚举内的值
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不写入任何记录

#### Scenario: 软删除后不可见

- **WHEN** 调用方删除一个已存在的数据集，随后按相同 ID 读取
- **THEN** 删除调用返回 204
- **AND** 后续读取返回 404 `not_found`，列表不再包含该数据集

### Requirement: Branch management

系统 SHALL 管理数据集分支，分支名称 MUST 在数据集内唯一。创建数据集 MUST 自动创建 `main` 分支；分支 MAY 记录父分支、当前 head transaction 与创建来源事务。

#### Scenario: 查询默认分支

- **WHEN** 创建数据集后按数据集列出分支
- **THEN** 返回列表包含且仅包含一个 `name=main` 的分支
- **AND** 该分支的 `dataset_id` 与创建返回的数据集 ID 一致

#### Scenario: 创建子分支

- **WHEN** 调用方在 `main` 分支上创建名为 `dev` 的子分支
- **THEN** 系统返回 201，包含新分支的 UUIDv7 ID、`parent_branch_id=main.id`
- **AND** `created_from_transaction_id` 等于创建时 `main` 的 head transaction（如存在）

#### Scenario: 分支名称重复

- **WHEN** 同一数据集下再次创建同名分支
- **THEN** 系统返回 409 `already_exists`

### Requirement: Transaction state machine

系统 SHALL 管理数据集事务。事务状态 MUST 只能从 `open` 转为 `committed` 或 `aborted`；已结束事务 MUST 拒绝再次变更。提交事务 MUST 更新对应分支的 `head_transaction_id`。

#### Scenario: 开始事务

- **WHEN** 调用方在存在的分支上开始一个事务
- **THEN** 系统返回 201，事务状态为 `open`，`started_at` 非空，`committed_at` 为空

#### Scenario: 提交事务

- **WHEN** 调用方提交一个 `open` 事务
- **THEN** 系统返回 200，状态变为 `committed` 且 `committed_at` 非空
- **AND** 该事务所属分支的 `head_transaction_id` 等于该事务 ID

#### Scenario: 重复提交被拒绝

- **WHEN** 调用方对已提交或已中止的事务再次提交
- **THEN** 系统返回 409 `conflict`
- **AND** 事务状态与分支 head 保持不变

#### Scenario: 中止事务

- **WHEN** 调用方中止一个 `open` 事务
- **THEN** 系统返回 200，状态变为 `aborted`
- **AND** 分支 head 不因中止而变化

### Requirement: Dataset views

系统 SHALL 支持在数据集内创建、查询、列出与删除视图。视图名称 MUST 在数据集内唯一；视图 MAY 绑定到一个存在的分支。

#### Scenario: 创建视图

- **WHEN** 调用方在存在的数据集上创建名称唯一、查询非空的视图
- **THEN** 系统返回 201，包含 UUIDv7 `id`、`dataset_id`、`version=1`

#### Scenario: 视图名称重复

- **WHEN** 同一数据集下再次创建同名视图
- **THEN** 系统返回 409 `already_exists`

#### Scenario: 删除视图

- **WHEN** 调用方删除已存在视图，随后按 ID 查询
- **THEN** 删除返回 204，后续查询返回 404 `not_found`

### Requirement: S3 file metadata

系统 SHALL 登记、查询、列出与删除数据集关联的 S3 对象元数据。元数据 MUST 包含对象 key、大小、行数、ETag 与内容类型；本能力 MUST NOT 代理对象内容或保存访问凭据。

#### Scenario: 登记文件元数据

- **WHEN** 调用方为存在的数据集登记非空 `object_key` 与合法大小
- **THEN** 系统返回 201，包含文件元数据与 UUIDv7 `id`

#### Scenario: 非法文件元数据

- **WHEN** 登记的 `object_key` 为空，或 `size_bytes`/`row_count` 为负数
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不写入文件记录

#### Scenario: 删除文件元数据

- **WHEN** 调用方删除已存在的文件元数据记录
- **THEN** 系统返回 204
- **AND** 后续查询返回 404 `not_found`

### Requirement: Tenant-scoped API access

所有 dataset catalog API MUST 要求已认证的租户上下文。缺失租户 MUST 返回 401 `unauthorized`；跨租户读取 MUST 表现为 404 `not_found`，不得泄露其他租户资源是否存在。

#### Scenario: 缺失租户上下文

- **WHEN** 请求未携带租户解析器认可的租户信息
- **THEN** 系统返回 401 `unauthorized`
- **AND** 不访问仓储层

#### Scenario: 跨租户读取

- **WHEN** 租户 A 创建的数据集被租户 B 按 ID 读取或更新
- **THEN** 系统返回 404 `not_found`

### Requirement: Consistent list and error contract

数据集、事务、视图与文件列表 API SHALL 使用 `page` 与 `per_page` 参数，`page` 默认 1，`per_page` 默认 50 且 MUST NOT 超过 200；分支列表按数据集整体返回。所有错误响应 MUST 使用统一的 `{error:{code,message,retryable}}` 结构；未知路由 MUST 返回 404。

#### Scenario: 分页列表

- **WHEN** 调用方以 `page=2&per_page=1` 请求已有三条记录的数据集列表
- **THEN** 响应包含 1 条记录、`total=3`、`total_pages=3`、`page=2`

#### Scenario: 非法分页

- **WHEN** 调用方传入 `per_page=201`
- **THEN** 系统返回 400 `invalid_argument`
- **AND** 不执行列表查询

#### Scenario: 未知路由

- **WHEN** 调用方请求 `/api/v1/datasets/unknown/path`
- **THEN** 系统返回 404，错误码为 `not_found`
