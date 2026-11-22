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

系统 SHALL 提供 Action 校验与预演；校验 MUST 检查 Action Type 存在、状态为 `active`、并检查 `parameters.required` 中声明的必填参数；预演 MUST 返回校验结果与 adapter 标识但不产生副作用。

#### Scenario: 缺少必填参数

- **WHEN** 调用方对要求 `reason` 的 Action Type 提交空参数
- **THEN** 系统返回 `valid=false` 与 `missing required parameter "reason"`

#### Scenario: 预演无副作用

- **WHEN** 调用方预演一个合法 Action
- **THEN** 系统返回 `adapter:"noop"` 与参数，且不创建执行记录

### Requirement: 执行入口与 Adapter

系统 SHALL 支持创建 Action Execution；Run MUST 在参数校验通过后创建状态 `queued` 的执行记录，并调用可替换的 `Adapter.Start`；Stage 1 adapter MUST 为无副作用的 Noop adapter。

#### Scenario: 运行合法 Action

- **WHEN** 调用方运行合法 Action Type 与参数
- **THEN** 系统返回 200 与状态为 `queued` 的 Execution

#### Scenario: 运行非法 Action Type

- **WHEN** 调用方对不存在或非 active 的 Action Type 运行
- **THEN** 系统拒绝并返回 404 或 400

### Requirement: 执行记录与状态操作

系统 SHALL 支持按租户与可选 Action Type 列出执行记录、按 ID 查询、取消与重试；取消 MUST 拒绝已经成功或已取消的执行；重试 MUST 只允许失败或已取消执行。

#### Scenario: 取消已取消执行

- **WHEN** 调用方取消状态为 `cancelled` 的 Execution
- **THEN** 系统返回 400，且状态不变

#### Scenario: 重试失败执行

- **WHEN** 调用方重试状态为 `failed` 的 Execution
- **THEN** 系统创建一个新的 `queued` Execution

### Requirement: 租户边界与错误契约

系统 SHALL 在所有 `/api/v1/actions` 读写下使用请求租户；租户 MUST 来自可信上下文或 `X-Tenant-Id`，缺失/非法 MUST 返回统一错误信封中的 `unauthorized`。

#### Scenario: 缺少租户

- **WHEN** 请求无租户上下文与合法 `X-Tenant-Id`
- **THEN** 系统返回 401，错误码为 `unauthorized`

#### Scenario: 资源不存在

- **WHEN** 调用方读取当前租户内不存在的 Action Type/Execution
- **THEN** 系统返回 404，错误码为 `not_found`，响应包含 `request_id`
