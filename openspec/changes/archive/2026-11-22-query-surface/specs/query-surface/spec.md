# Spec Delta

## Purpose

定义 OntoAxis Stage 1 查询模块的最小行为：保存查询 CRUD、只读 SQL 执行/解释、对象查询，并对未配置引擎 fail-closed。

## ADDED Requirements

### Requirement: 保存查询 CRUD

系统 SHALL 支持租户内 SavedQuery 的创建、列表与删除；名称 MUST 非空且租户内唯一，SQL MUST 为只读语句。

#### Scenario: 保存合法查询

- **WHEN** 调用方在有效租户下提交名称与 `SELECT` 语句
- **THEN** 系统返回 201 和带 `id`、`name`、`sql`、`owner_id` 的 SavedQuery

#### Scenario: 拒绝写语句

- **WHEN** 调用方保存 `DELETE FROM t` 或 `CREATE TABLE`
- **THEN** 系统返回 400，错误码为 `bad_request`

#### Scenario: 重名冲突

- **WHEN** 调用方在同一租户保存同名未删除查询
- **THEN** 系统返回 409，错误码为 `conflict`

### Requirement: 执行只读 SQL

系统 SHALL 接受 `SELECT/WITH/SHOW/DESCRIBE/EXPLAIN/VALUES` 开头的 SQL，通过 Trino HTTP 执行并返回 `columns/rows/row_count`；未配置 Trino 时 MUST 返回 503。

#### Scenario: 执行 SELECT

- **WHEN** Trino 已配置且 SQL 为 `SELECT 1`
- **THEN** 系统返回 200 与列/行结果

#### Scenario: 未配置查询引擎

- **WHEN** `ONTOAXIS_TRINO_URL` 为空时执行 SQL
- **THEN** 系统返回 503，错误码为 `unavailable`

#### Scenario: 拒绝非只读 SQL

- **WHEN** 调用方执行 `UPDATE`、`INSERT` 或 `DROP`
- **THEN** 系统返回 400，且不调用 Trino

### Requirement: 解释查询

系统 SHALL 通过 Trino `EXPLAIN` 返回查询计划；输入合法性规则与执行查询一致。

#### Scenario: 解释合法 SQL

- **WHEN** 调用方解释 `SELECT * FROM t`
- **THEN** 系统返回 200 与 `plan` 字符串

### Requirement: 对象查询

系统 SHALL 提供按租户与可选 object type 查询对象实例的 API，直接复用 `objects` 模块的只读 Service；缺少 objects Service 时 MUST 返回 503。

#### Scenario: 按类型查询对象

- **WHEN** 调用方带有效 `object_type_id` 请求对象查询
- **THEN** 系统返回分页对象列表与 total

#### Scenario: 无对象服务

- **WHEN** query 模块未注入 objects Service 时查询对象
- **THEN** 系统返回 503，错误码为 `unavailable`

### Requirement: 租户边界与错误契约

系统 SHALL 从认证上下文取租户；缺失/非法租户 MUST 返回 401。所有错误 MUST 使用统一错误信封。

#### Scenario: 缺少租户

- **WHEN** 请求没有认证上下文与 `X-Tenant-Id`
- **THEN** 系统返回 401，错误码为 `unauthenticated`
