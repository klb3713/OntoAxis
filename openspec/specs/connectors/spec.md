# connectors Specification

## Purpose
让租户能够登记、维护和验证外部数据源连接器，并为下游同步任务提供稳定、可审计的连接器定义与状态视图。

## Requirements

### Requirement: Connector 创建

系统 SHALL 允许租户创建 Connector，创建成功时返回 `active` 状态、服务端生成的 UUIDv7 标识与创建/更新时间；同一租户内未删除的 Connector 名称 MUST 唯一。

#### Scenario: 成功创建连接器

- **WHEN** 租户提交合法的名称、受支持的类型与非敏感配置
- **THEN** 系统返回 `active` 状态的 Connector，并包含服务端生成的标识与时间戳

#### Scenario: 同租户重名被拒绝

- **WHEN** 租户创建与现有未删除 Connector 同名的 Connector
- **THEN** 系统返回冲突错误，且不创建重复记录

#### Scenario: 不同租户可使用相同名称

- **WHEN** 两个不同租户分别创建同名 Connector
- **THEN** 两个租户各自创建成功且彼此不可见

### Requirement: Connector 类型与配置校验

系统 SHALL 只接受 `postgres`、`mysql`、`s3`、`rest_api`、`kafka` 五种 Connector 类型，并 MUST 拒绝缺少名称、名称格式非法、类型未知或配置不是合法 JSON 对象的请求。

#### Scenario: 未知类型被拒绝

- **WHEN** 创建请求携带不在受支持集合内的类型
- **THEN** 系统返回参数错误，且不创建 Connector

#### Scenario: 配置不是对象被拒绝

- **WHEN** 创建或更新请求的配置字段不是 JSON 对象
- **THEN** 系统返回参数错误，且不修改任何记录

### Requirement: 敏感配置不得写入 Connector

系统 MUST 拒绝在 Connector 配置中出现密码、密钥、令牌、凭证等敏感字段名；敏感值只能通过独立的 `secret_ref` 引用保存，且任何响应 MUST NOT 回显 `secret_ref`。

#### Scenario: 配置包含敏感字段被拒绝

- **WHEN** 创建或更新请求的配置包含 `password`、`token`、`api_key`、`secret` 等敏感键
- **THEN** 系统返回参数错误，且不保存该配置

#### Scenario: 响应不回显秘密引用

- **WHEN** 客户端查询一个已配置 `secret_ref` 的 Connector
- **THEN** 响应不包含 `secret_ref` 字段或其取值

### Requirement: Connector 查询与列表

系统 SHALL 提供按标识查询与分页列表能力；列表 MUST 支持 `page`、`per_page` 参数（默认第 1 页、每页 20 条，最大 100 条），默认按创建时间倒序，并 MUST 过滤已软删除的记录。

#### Scenario: 分页列表返回稳定结构

- **WHEN** 客户端请求 Connector 列表并指定页码与每页数量
- **THEN** 系统返回当前页数据以及包含页码、每页数量、总数和总页数的分页信息

#### Scenario: 软删除记录不出现在列表

- **WHEN** 某个 Connector 已被删除后客户端请求列表
- **THEN** 列表中不包含该 Connector

### Requirement: Connector 更新与软删除

系统 SHALL 允许更新 Connector 的名称、非敏感配置、标签与状态；删除 MUST 为软删除，删除后按标识查询 MUST 返回未找到。

#### Scenario: 更新状态生效

- **WHEN** 租户把 Connector 状态从 `active` 更新为 `paused`
- **THEN** 后续查询返回 `paused`，且更新时间晚于创建时间

#### Scenario: 删除后不可见

- **WHEN** 租户删除一个 Connector 后再次按标识查询
- **THEN** 系统返回未找到

#### Scenario: 非法状态被拒绝

- **WHEN** 更新请求携带不在 `active`、`paused`、`error` 内的状态
- **THEN** 系统返回参数错误，且原状态保持不变

### Requirement: 租户隔离

系统 MUST 对所有 Connector 操作要求有效租户上下文；缺失或非法租户标识的请求 MUST 被拒绝，跨租户访问 MUST 表现为未找到且不得泄露记录是否存在。

#### Scenario: 缺少租户标识被拒绝

- **WHEN** 请求未携带租户标识
- **THEN** 系统返回参数错误，且不执行任何数据操作

#### Scenario: 跨租户查询不可见

- **WHEN** 租户 A 使用租户 B 的 Connector 标识发起查询
- **THEN** 系统返回未找到，且响应不包含租户 B 的数据

### Requirement: Connector 测试与 Schema 推断

系统 SHALL 提供 Connector 连通性测试与 Schema 推断能力；测试结果 MUST 包含是否成功与说明信息，推断结果 MUST 为结构化 Schema。适配器不可用时系统 MUST 返回不可用或未实现错误，不得返回伪造的成功结果。

#### Scenario: 测试适配器成功

- **WHEN** 已注入的测试适配器对目标 Connector 返回成功
- **THEN** 系统返回成功标志与说明信息

#### Scenario: 测试适配器不可用

- **WHEN** 未配置测试适配器或适配器返回不可用
- **THEN** 系统返回不可用错误，且不返回成功标志

#### Scenario: 推断 Schema

- **WHEN** 已注入的 Schema 适配器返回字段定义
- **THEN** 系统以结构化对象返回推断出的 Schema

### Requirement: 启动同步

系统 SHALL 允许为 `active` 状态的 Connector 启动同步并返回同步任务标识；`paused` 或 `error` 状态的 Connector MUST 被拒绝，适配器不可用时 MUST 返回不可用错误。

#### Scenario: 活跃连接器启动同步

- **WHEN** 租户对 `active` 状态的 Connector 发起启动同步
- **THEN** 系统创建同步任务并返回任务标识

#### Scenario: 暂停连接器拒绝同步

- **WHEN** 租户对 `paused` 状态的 Connector 发起启动同步
- **THEN** 系统返回冲突错误，且不创建同步任务

#### Scenario: 同步适配器不可用

- **WHEN** 未配置同步适配器
- **THEN** 系统返回不可用错误，且不伪造任务标识
