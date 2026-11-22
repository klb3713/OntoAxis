# tenancy Specification

## Purpose
定义 OntoAxis 租户域的组织、工作区、成员关系、项目与项目资源绑定行为，确保所有租户级数据在 API 边界内被正确隔离并可被后续业务模块引用。

## Requirements

### Requirement: 组织管理

系统 SHALL 支持租户内组织（Organization）的创建、读取、更新、列表与删除；组织 MUST 带有租户归属、唯一 slug 与审计时间，删除 MUST 为软删除且默认查询不返回已删除组织。

#### Scenario: 创建组织

- **WHEN** 调用方在有效租户下提交组织名称与可选 slug
- **THEN** 系统返回 201，并返回带 `id`、`tenant_id`、`name`、`slug` 与时间戳的组织对象

#### Scenario: 按租户隔离组织列表

- **WHEN** 租户 A 创建组织后，调用方以租户 B 请求组织列表
- **THEN** 响应中的组织列表不包含租户 A 的组织

#### Scenario: 拒绝无效组织输入

- **WHEN** 调用方创建组织时名称为空
- **THEN** 系统返回 400 且错误码为 `bad_request`，并且不创建组织

### Requirement: 工作区管理

系统 SHALL 支持在组织下创建、读取、更新、列表与删除工作区（Workspace）；工作区 MUST 属于同一租户内的组织，且同一组织内 slug MUST 唯一。

#### Scenario: 创建组织下的工作区

- **WHEN** 调用方在既有组织下提交工作区名称
- **THEN** 系统返回 201，并返回带 `organization_id` 与 `tenant_id` 的工作区对象

#### Scenario: 拒绝跨租户组织绑定

- **WHEN** 调用方尝试用其他租户的组织创建本租户工作区
- **THEN** 系统返回 404 且不创建工作区

### Requirement: 成员关系管理

系统 SHALL 支持为组织或工作区添加、列出与移除成员关系（Membership）；成员关系 MUST 声明主体类型、主体标识、角色与状态，主体类型与状态 MUST 使用允许值。

#### Scenario: 添加成员关系

- **WHEN** 调用方为组织添加主体类型为 `user`、角色为 `member` 的成员关系
- **THEN** 系统返回 201，成员关系状态默认为 `active`

#### Scenario: 拒绝非法主体类型

- **WHEN** 调用方提交不在 `user`、`group`、`service` 内的主体类型
- **THEN** 系统返回 400，错误码为 `bad_request`

#### Scenario: 移除成员关系

- **WHEN** 调用方移除当前租户内存在的成员关系
- **THEN** 系统返回 204，后续列表不再返回该成员关系

### Requirement: 项目管理

系统 SHALL 支持项目（Project）的创建、读取、更新、列表与删除；项目 MUST 同时绑定同一租户内的组织与工作区，项目名称 MUST 在组织内唯一（未删除范围内）。

#### Scenario: 创建项目

- **WHEN** 调用方提交组织、工作区、名称与可选描述创建项目
- **THEN** 系统返回 201，并返回带 `organization_id`、`workspace_id` 与 `tenant_id` 的项目对象

#### Scenario: 更新项目

- **WHEN** 调用方对当前租户内的项目提交新的展示名或描述
- **THEN** 系统返回更新后的项目对象，未提交字段保持原值

#### Scenario: 跨租户读取项目被拒绝

- **WHEN** 租户 B 请求租户 A 的项目 ID
- **THEN** 系统返回 404 且不泄漏项目内容

### Requirement: 项目资源绑定

系统 SHALL 支持把资源绑定到项目并列出、解绑项目资源（ProjectResource）；资源类型 MUST 限定为 `object_type`、`dataset`、`pipeline`、`dashboard`，同一项目内同一资源 MUST 只绑定一次。

#### Scenario: 绑定项目资源

- **WHEN** 调用方为当前租户内的项目添加类型为 `dataset` 的资源
- **THEN** 系统返回 201，并返回带 `project_id`、`resource_type` 与 `resource_id` 的绑定对象

#### Scenario: 重复绑定冲突

- **WHEN** 调用方对同一项目重复添加相同类型与标识的资源
- **THEN** 系统返回 409，错误码为 `conflict`

#### Scenario: 解绑项目资源

- **WHEN** 调用方删除当前租户内项目下的资源绑定
- **THEN** 系统返回 204，后续列表不再包含该绑定

### Requirement: 租户边界

系统 SHALL 在所有租户级读写下强制使用请求的租户标识；租户标识 MUST 来自可信上下文或 `X-Tenant-Id`，缺失或格式非法 MUST 拒绝请求，两者同时存在但不一致 MUST 返回 403。

#### Scenario: 缺少租户标识

- **WHEN** 请求既没有租户上下文也没有 `X-Tenant-Id`
- **THEN** 系统返回 401 且错误码为 `unauthorized`

#### Scenario: 租户标识格式非法

- **WHEN** 请求的 `X-Tenant-Id` 不是合法 UUID
- **THEN** 系统返回 400 且错误码为 `bad_request`

#### Scenario: 上下文与请求头冲突

- **WHEN** 请求上下文中的租户与 `X-Tenant-Id` 不一致
- **THEN** 系统返回 403 且错误码为 `forbidden`

### Requirement: 错误响应契约

系统 SHALL 对所有 `/api/v1/tenancy` 错误返回统一错误信封 `{"error":{"code","message","metadata","retryable","request_id"}}`，且 MUST NOT 暴露底层 SQL 或内部错误文本。

#### Scenario: 资源不存在

- **WHEN** 调用方读取当前租户内不存在的资源 ID
- **THEN** 系统返回 404，错误码为 `not_found`，响应包含 `request_id`

#### Scenario: 内部错误不泄漏细节

- **WHEN** 服务端处理请求时发生未预期错误
- **THEN** 系统返回 500，错误码为 `internal`，响应不包含 SQL 或堆栈信息
