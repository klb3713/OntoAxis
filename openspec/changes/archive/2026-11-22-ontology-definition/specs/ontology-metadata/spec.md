# Spec Delta

## Purpose

提供企业级 Ontology 元数据的类型定义与数据集绑定管理，使对象类型、属性与链接类型可被上层对象、查询与动作模块稳定引用，并在多租户环境下保持严格的租户隔离与职责边界。

## ADDED Requirements

### Requirement: Object Type Creation

系统 SHALL 允许在指定租户下创建对象类型，记录名称、显示名、描述与主键属性，并返回包含服务端生成标识和时间戳的完整对象类型。

#### Scenario: Create a valid object type
- **WHEN** 调用方以合法名称、显示名与主键属性创建对象类型
- **THEN** 系统返回创建成功的对象类型，包含唯一标识、`active` 状态、创建时间与更新时间

#### Scenario: Reject duplicate object type name in the same tenant
- **WHEN** 同一租户下已存在同名对象类型时再次创建
- **THEN** 系统拒绝创建并返回 `already_exists` 冲突错误，已有对象类型不被修改

#### Scenario: Allow the same object type name in different tenants
- **WHEN** 两个不同租户分别创建同名对象类型
- **THEN** 系统为两个租户分别创建独立的对象类型，二者互不可见

### Requirement: Object Type Retrieval and Listing

系统 SHALL 支持按标识获取单个对象类型以及分页列出当前租户的对象类型；查询结果 MUST 仅包含未删除的记录。

#### Scenario: Get an existing object type
- **WHEN** 调用方请求当前租户下存在的对象类型标识
- **THEN** 系统返回该对象类型及其属性列表

#### Scenario: Get a missing object type
- **WHEN** 调用方请求不存在或属于其他租户的对象类型标识
- **THEN** 系统返回 `not_found` 错误，不泄露资源是否存在

#### Scenario: List object types with pagination
- **WHEN** 调用方以页码和每页数量请求对象类型列表
- **THEN** 系统返回当前页记录、总数、页码与每页数量，且不包含已软删除记录

### Requirement: Object Type Update and Deletion

系统 SHALL 允许更新对象类型的显示名与描述，并 SHALL 以软删除方式删除对象类型；仍被链接类型引用的对象类型 MUST NOT 被删除。

#### Scenario: Update mutable object type fields
- **WHEN** 调用方提交新的显示名和描述
- **THEN** 系统持久化变更并返回更新后的对象类型与新的更新时间

#### Scenario: Delete an unreferenced object type
- **WHEN** 调用方删除当前租户下存在且未被链接类型引用的对象类型
- **THEN** 系统软删除该对象类型及其属性，后续查询不再返回

#### Scenario: Reject deletion of a referenced object type
- **WHEN** 调用方删除仍被任一链接类型作为源类型或目标类型引用的对象类型
- **THEN** 系统返回冲突错误并保持对象类型及其属性不变

### Requirement: Property Management

系统 SHALL 允许为同一租户下已存在的对象类型创建属性，并支持按对象类型列出、更新和软删除属性；属性名称在同一对象类型内 MUST 唯一。

#### Scenario: Create a property on an existing object type
- **WHEN** 调用方为当前租户下存在的对象类型提交合法属性定义
- **THEN** 系统返回创建的属性，包含对象类型标识、名称、类型、必填标记与唯一约束标记

#### Scenario: Reject property creation for a missing object type
- **WHEN** 调用方为不存在或属于其他租户的对象类型创建属性
- **THEN** 系统返回 `not_found` 错误且不创建属性

#### Scenario: Reject duplicate property name within an object type
- **WHEN** 同一对象类型下已存在同名属性时再次创建
- **THEN** 系统返回 `already_exists` 冲突错误

#### Scenario: Update and delete a property
- **WHEN** 调用方更新属性的显示名或必填标记，随后删除该属性
- **THEN** 系统先返回更新后的属性，再软删除该属性且后续列表不再包含它

### Requirement: Link Type Management

系统 SHALL 允许创建、列出、更新和软删除链接类型；链接类型 MUST 引用同一租户下已存在的源对象类型与目标对象类型，且基数 MUST 为受支持的值之一。

#### Scenario: Create a valid link type
- **WHEN** 调用方以存在的源/目标对象类型和合法基数创建链接类型
- **THEN** 系统返回创建的链接类型，包含源类型标识、目标类型标识与基数

#### Scenario: Reject link type with missing endpoint
- **WHEN** 调用方引用不存在或属于其他租户的源对象类型或目标对象类型
- **THEN** 系统返回 `not_found` 错误且不创建链接类型

#### Scenario: Reject unsupported cardinality
- **WHEN** 调用方提交不在 `one_to_one`、`one_to_many`、`many_to_many` 范围内的基数
- **THEN** 系统返回 `invalid_argument` 错误

#### Scenario: Update and delete a link type
- **WHEN** 调用方更新链接类型的显示名，随后删除该链接类型
- **THEN** 系统先返回更新后的链接类型，再软删除且后续列表不再包含它

### Requirement: Dataset Binding Lifecycle

系统 SHALL 允许将当前租户下的对象类型绑定到一个数据集，并允许解除绑定；绑定信息 MUST 与对象类型一同返回。

#### Scenario: Bind a dataset to an object type
- **WHEN** 调用方为存在的对象类型提交数据集标识与同步模式
- **THEN** 系统保存绑定并返回带有数据集标识与同步模式的对象类型

#### Scenario: Unbind a dataset from an object type
- **WHEN** 调用方解除存在绑定的对象类型的绑定
- **THEN** 系统清除绑定信息并返回未绑定数据集的对象类型

#### Scenario: Reject binding for a missing object type
- **WHEN** 调用方为不存在或属于其他租户的对象类型绑定数据集
- **THEN** 系统返回 `not_found` 错误且不保存绑定

### Requirement: Binding Materialization Acceptance

系统 SHALL 接受对象类型数据集绑定的物化请求并返回唯一操作标识；物化请求 MUST NOT 在请求内执行真实数据同步。

#### Scenario: Accept materialization for a bound object type
- **WHEN** 调用方对已绑定数据集的对象类型请求物化
- **THEN** 系统返回非空且唯一的操作标识，用于后续异步处理追踪

#### Scenario: Reject materialization without binding
- **WHEN** 调用方对未绑定数据集的对象类型请求物化
- **THEN** 系统返回冲突错误且不产生操作标识

### Requirement: Tenant Isolation

系统 SHALL 将所有 Ontology 元数据的读写限制在请求所属租户内；任何跨租户访问 MUST 表现为资源不存在。

#### Scenario: Cross-tenant read is hidden
- **WHEN** 租户 A 请求租户 B 的对象类型、属性或链接类型标识
- **THEN** 系统返回 `not_found`，且不返回租户 B 的任何数据

#### Scenario: Cross-tenant mutation is rejected
- **WHEN** 租户 A 尝试更新或删除租户 B 的资源
- **THEN** 系统返回 `not_found`，租户 B 的数据保持不变

#### Scenario: Missing tenant context is rejected
- **WHEN** 请求未携带可解析的租户上下文
- **THEN** 系统返回 `unauthorized` 错误且不访问任何元数据

### Requirement: Validation and Error Responses

系统 SHALL 校验所有输入，并以统一错误响应结构返回失败原因；非法 JSON、缺失必填字段、非法名称或非法分页参数 MUST 被拒绝。

#### Scenario: Reject malformed request body
- **WHEN** 请求体不是合法 JSON 或包含未知字段类型
- **THEN** 系统返回 `bad_request` 错误，响应包含稳定的错误码与消息

#### Scenario: Reject invalid object type name
- **WHEN** 调用方提交不符合命名规则的名称
- **THEN** 系统返回 `invalid_argument` 错误且不写入数据

#### Scenario: Reject invalid pagination
- **WHEN** 调用方提交小于 1 的页码或超出允许范围的每页数量
- **THEN** 系统返回 `invalid_argument` 错误

### Requirement: Metadata Persistence Boundary

系统 SHALL 将对象类型、属性与链接类型持久化在 `ontology` schema 的 `object_types`、`properties`、`link_types` 表中，并通过可重复执行的迁移创建这些表；模块 MUST NOT 读取或写入 Project/ProjectResource 表。

#### Scenario: Apply ontology migration repeatedly
- **WHEN** 迁移在已存在 `ontology` 表的数据库上再次执行
- **THEN** 迁移成功完成且不丢失、不重复已有数据

#### Scenario: Reject project resource management through ontology
- **WHEN** 调用方尝试通过 Ontology 接口管理项目或项目资源
- **THEN** 系统不提供相应行为，项目与项目资源仍由 tenancy 模块管理

### Requirement: Module Registration Contract

系统 SHALL 以统一模块接口暴露 Ontology 能力，声明对 `tenancy` 的依赖，在 `/api/v1/ontology` 下注册路由，并在数据库不可用时报告未就绪。

#### Scenario: Register ontology module
- **WHEN** 应用按模块接口注册 Ontology 模块
- **THEN** 模块报告名称 `ontology` 与依赖 `tenancy`，且路由挂载在 `/api/v1/ontology`

#### Scenario: Report readiness without database
- **WHEN** 数据库连接池未初始化或不可用
- **THEN** 模块的就绪检查返回错误，不报告就绪

#### Scenario: Unknown ontology route remains not found
- **WHEN** 调用方请求 `/api/v1/ontology` 下未定义的路由
- **THEN** 系统返回 `not_found` 错误而非默认成功响应
