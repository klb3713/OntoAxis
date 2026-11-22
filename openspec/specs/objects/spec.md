# objects Specification

## Purpose
定义 OntoAxis 对象图的对象实例与链接行为，确保其按租户隔离、可分页过滤、软删除且返回统一错误信封，为查询、动作和前端 Object Explorer 提供稳定数据面。

## Requirements

### Requirement: 对象创建与读取

系统 SHALL 支持在有效租户下创建、读取与更新对象实例；对象 MUST 包含 `object_type_id` 与非空 `properties`，对象 ID 与时间戳由服务端生成。

#### Scenario: 创建对象

- **WHEN** 调用方在有效租户下提交合法 `object_type_id` 与非空 JSON `properties`
- **THEN** 系统返回 201，并返回 `id`、`tenant_id`、`object_type_id`、`version`、`properties` 与时间戳

#### Scenario: 拒绝空 properties

- **WHEN** 调用方创建对象时提交空 `properties`
- **THEN** 系统返回 400，错误码为 `bad_request`，且不创建对象

#### Scenario: 跨租户读取对象被拒绝

- **WHEN** 租户 B 按 ID 读取租户 A 的对象
- **THEN** 系统返回 404，且不泄漏对象内容

### Requirement: 对象列表与过滤

系统 SHALL 支持按租户列出未删除对象，并可选按 `object_type_id` 过滤；列表 MUST 返回 `items` 与 `total`，分页参数 MUST 限制在安全范围内。

#### Scenario: 按类型过滤对象

- **WHEN** 调用方以 `object_type_id` 请求对象列表
- **THEN** 系统只返回该租户、该类型且未删除的对象

#### Scenario: 分页参数归一化

- **WHEN** 调用方提交 `limit=0` 或负数 `offset`
- **THEN** 系统使用默认 limit 50、offset 0，且不返回错误

### Requirement: 对象更新与删除

系统 SHALL 支持对象属性合并更新与替换更新，并支持软删除；更新 MUST 递增 `version`，删除后默认查询 MUST NOT 返回该对象。

#### Scenario: 合并更新属性

- **WHEN** 调用方以 `replace=false` 提交部分属性
- **THEN** 系统保留未提交属性、合并新属性并递增 `version`

#### Scenario: 软删除对象

- **WHEN** 调用方删除当前租户内存在的对象
- **THEN** 系统返回 204，后续按 ID 读取返回 404，列表不再返回该对象

### Requirement: 链接管理

系统 SHALL 支持创建、列出与删除对象链接；链接 MUST 绑定 `link_type_id`、`source_id`、`target_id`，同一租户内同一链接类型、源和目标 MUST 唯一。

#### Scenario: 创建链接

- **WHEN** 调用方提交合法的 `link_type_id`、`source_id` 与 `target_id`
- **THEN** 系统返回 201，并返回链接对象

#### Scenario: 列出对象关联链接

- **WHEN** 调用方请求某对象的链接列表
- **THEN** 系统返回该对象作为源或目标且未删除的链接

#### Scenario: 重复链接冲突

- **WHEN** 调用方重复创建相同租户、链接类型、源与目标的链接
- **THEN** 系统返回冲突错误，且不创建重复链接

### Requirement: 租户边界

系统 SHALL 在所有对象与链接读写下强制使用请求的租户标识；租户 MUST 来自可信上下文或 `X-Tenant-Id`，缺失或非法 MUST 拒绝请求。

#### Scenario: 缺少租户标识

- **WHEN** 请求既无租户上下文也无合法 `X-Tenant-Id`
- **THEN** 系统返回 401，错误码为 `unauthorized`

#### Scenario: 租户标识格式非法

- **WHEN** `X-Tenant-Id` 不是合法 UUID
- **THEN** 系统返回 401，错误码为 `unauthorized`

### Requirement: 错误响应契约

系统 SHALL 对所有 `/api/v1/objects` 错误返回统一错误信封 `{"error":{"code","message","metadata","retryable","request_id"}}`，且 MUST NOT 暴露底层 SQL 或内部错误文本。

#### Scenario: 对象不存在

- **WHEN** 调用方读取当前租户内不存在的对象 ID
- **THEN** 系统返回 404，错误码为 `not_found`，响应包含 `request_id`

#### Scenario: 内部错误不泄漏细节

- **WHEN** 服务端处理请求时发生未预期错误
- **THEN** 系统返回 500，错误码为 `internal`，响应不包含 SQL 或堆栈信息
