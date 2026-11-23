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

系统 SHALL 允许为 `active` 状态的 Connector 启动同步并返回同步任务标识；`paused` 或 `error` 状态的 Connector MUST 被拒绝，且 MUST NOT 创建同步任务。拒绝错误 MUST 为明确的冲突或拒绝语义：运行时预检在授权层前置时返回 `403`，未进入预检时返回冲突错误。适配器不可用时 MUST 返回不可用错误，MUST NOT 伪造任务标识。

#### Scenario: 活跃连接器启动同步

- **WHEN** 租户对 `active` 状态的 Connector 发起启动同步
- **THEN** 系统创建同步任务并返回任务标识

#### Scenario: 暂停连接器拒绝同步

- **WHEN** 租户对 `paused` 状态的 Connector 发起启动同步
- **THEN** 系统返回拒绝错误（`403` 或冲突错误），且不创建同步任务

#### Scenario: 错误状态连接器拒绝同步

- **WHEN** 租户对 `error` 状态的 Connector 发起启动同步
- **THEN** 系统返回拒绝错误（`403` 或冲突错误），且不创建同步任务

#### Scenario: 同步适配器不可用

- **WHEN** 未配置同步适配器
- **THEN** 系统返回不可用错误，且不伪造任务标识

### Requirement: 同步运行时预检与类型范围

系统 SHALL 在为 Connector 启动同步前完成运行时预检：Connector 类型 MUST 存在可用的 CDC 映射、必要配置键 MUST 完整、Kafka Connect MUST 可达。当前仅 `postgres` 类型映射为 Debezium CDC，其他类型 MUST 返回不支持错误。预检失败 MUST 返回明确错误（参数错误/不可用/冲突），MUST NOT 创建同步任务或运行时连接器，MUST NOT 返回伪造的任务标识。

#### Scenario: 受支持类型通过预检

- **WHEN** 租户对 `active` 的 `postgres` Connector 发起启动同步，配置键完整且 Kafka Connect 可达
- **THEN** 系统创建 `queued` 同步任务并返回任务标识

#### Scenario: 不支持的类型被拒绝

- **WHEN** 租户对 `mysql`、`s3`、`rest_api` 或 `kafka` 类型的 Connector 发起启动同步
- **THEN** 系统返回不支持错误，且不创建同步任务或运行时连接器

#### Scenario: 运行时不可达

- **WHEN** Kafka Connect 不可达或健康检查失败
- **THEN** 系统返回不可用错误，且不返回任务标识

#### Scenario: 必要配置键缺失

- **WHEN** 预检发现主键、连接参数或表清单等必需配置键缺失或非法
- **THEN** 系统返回参数错误，且不创建同步任务

### Requirement: 运行时连接器配置生成

系统 SHALL 从 Connector 类型、非敏感配置与租户/连接器标识生成 Kafka Connect 运行时连接器定义（Debezium source 与 sink）。生成 MUST 确定：相同输入 MUST 产生相同连接器名称与配置内容。不同租户或不同 Connector MUST 落在不同命名空间（连接器名称前缀与 topic 前缀）。生成 MUST 只使用白名单配置键，MUST NOT 包含明文凭据、`secret_ref` 值或未声明字段，并 MUST 拒绝不能安全映射为运行时标识符的输入。

#### Scenario: 相同输入生成相同定义

- **WHEN** 对同一 Connector 连续两次生成运行时连接器定义
- **THEN** 两次连接器名称、topic 前缀与配置内容完全一致

#### Scenario: 租户与连接器命名空间隔离

- **WHEN** 两个租户各有一个同名 Connector
- **THEN** 生成的运行时连接器名称与 topic 前缀互不相同

#### Scenario: 不安全标识符被拒绝

- **WHEN** 表清单或目标命名空间包含不能安全映射为运行时标识符的字符
- **THEN** 生成失败并返回参数错误，且不生成任何运行时连接器定义

### Requirement: 凭据解析与秘密边界

系统 MUST 在提交运行时任务前按 `secret_ref` 解析凭据，并只把凭据注入提交请求与运行时连接器配置。系统 MUST NOT 将凭据写入数据库、日志、错误信息、指标、trace 或 HTTP 响应，失败信息与状态回写 MUST 脱敏。`secret_ref` 缺失或无法解析时 MUST 返回不可用或参数错误并中止提交，MUST NOT 提交部分运行时连接器。

#### Scenario: 凭据仅在提交时注入

- **WHEN** Connector 携带可解析的 `secret_ref` 并成功提交
- **THEN** 提交请求包含解析后的凭据，而响应、运行记录与生成物均不包含明文凭据

#### Scenario: 凭据无法解析

- **WHEN** `secret_ref` 缺失或对应凭据不可解析
- **THEN** 系统返回不可用或参数错误，且未提交任何运行时连接器

#### Scenario: 错误信息脱敏

- **WHEN** 运行时提交因认证失败而返回错误
- **THEN** 返回给调用方的错误信息与日志不包含凭据明文或完整 DSN

### Requirement: Kafka Connect 任务提交

系统 SHALL 通过 Kafka Connect REST API 提交运行时连接器，并记录其运行时标识。对同一 Connector 的重复提交 MUST 幂等：相同配置 MUST 返回相同运行时连接器集合，不产生重复连接器或重复任务。运行时返回 4xx/5xx、超时或无法解析的响应时 MUST 返回错误，MUST NOT 报告提交成功；已存在冲突配置时 MUST 返回冲突或按幂等规则安全更新并留下可复核记录。

#### Scenario: 首次提交创建运行时连接器

- **WHEN** 调度器为 `queued` 任务提交运行时连接器
- **THEN** Kafka Connect 中存在对应的 source 与 sink 连接器，且系统记录其运行时标识

#### Scenario: 重复提交幂等

- **WHEN** 同一 Connector 的相同配置被再次提交
- **THEN** 不新增重复连接器或任务，返回相同运行时标识

#### Scenario: 运行时拒绝提交

- **WHEN** Kafka Connect 返回 4xx/5xx、超时或不可解析响应
- **THEN** 系统返回错误且不把任务报告为已提交或已运行

### Requirement: 运行时任务停止与回收

系统 SHALL 在 Connector 暂停或软删除、同步取消时停止并删除对应运行时连接器。停止与删除 MUST 以运行时确认为准；运行时拒绝或不可达时 MUST 返回错误并保留可重试状态，MUST NOT 报告已停止或已回收。运行时中不存在的连接器 MUST 视为已停止/已回收。

#### Scenario: 暂停 Connector 停止运行时

- **WHEN** 租户把 `active` Connector 更新为 `paused`
- **THEN** 系统停止并删除其运行时连接器，且后续该 Connector 不再有运行中的任务

#### Scenario: 停止未被运行时确认

- **WHEN** Kafka Connect 拒绝停止请求或不可达
- **THEN** 系统返回错误且不报告运行时已停止

#### Scenario: 运行时连接器已不存在

- **WHEN** 需要回收的运行时连接器在 Kafka Connect 中已不存在
- **THEN** 系统按幂等语义视为回收完成，不返回错误

### Requirement: 运行时状态读取与归一

系统 SHALL 从 Kafka Connect 读取运行时连接器与任务状态，并归一为可审计状态（`running`、`failed`、`paused`、`missing`、`unknown`）。状态读取 MUST 限定在对应租户的运行时命名空间。运行时不可用 MUST 返回不可用错误；归一 MUST NOT 把 `missing` 或 `unknown` 映射为运行中；错误信息 MUST 脱敏。

#### Scenario: 健康运行时归一为运行中

- **WHEN** 运行时连接器与其任务均为运行状态
- **THEN** 系统返回 `running`

#### Scenario: 任务失败归一为失败

- **WHEN** 运行时连接器存在但至少一个任务处于失败状态
- **THEN** 系统返回 `failed`，且错误信息不包含凭据或原始堆栈

#### Scenario: 运行时不可用

- **WHEN** Kafka Connect 不可达或响应非法
- **THEN** 系统返回不可用错误，且不把状态归一为 `running`

### Requirement: 运行时命名空间与租户隔离

系统 MUST 让运行时连接器名称与 topic 前缀包含租户级命名空间，使不同租户的同名 Connector MUST 不冲突。跨租户启动同步、读取运行时状态、暂停或删除 MUST 表现为未找到，且 MUST NOT 停止、修改或删除其他租户的运行时连接器。

#### Scenario: 同名 Connector 跨租户不冲突

- **WHEN** 租户 A 与租户 B 各自创建同名 Connector 并启动同步
- **THEN** 两组运行时连接器名称与 topic 前缀互不冲突

#### Scenario: 跨租户启动同步不可见

- **WHEN** 租户 A 使用租户 B 的 Connector 标识发起启动同步
- **THEN** 系统返回未找到，且不创建任务、不接触租户 B 的运行时连接器

#### Scenario: 跨租户暂停不影响他人运行时

- **WHEN** 租户 A 请求暂停租户 B 的 Connector
- **THEN** 系统返回未找到，且租户 B 的运行时连接器保持原状态

### Requirement: 同步运行时观测

系统 SHALL 为运行时预检、提交、停止与状态读取产生符合 `runtime-observability` 基线的指标与 trace：指标 MUST 使用 `ontoaxis_` 前缀与 `_total`/`_seconds` 单位后缀，标签 MUST 限于 method/route/status/module/operation/driver/result/environment 白名单，MUST NOT 使用租户、Connector 标识、topic 或运行时连接器名称作为指标标签。trace MUST 覆盖出站 Kafka Connect REST 调用并记录依赖、操作与结果，MUST NOT 记录凭据、请求体或原始异常文本。

#### Scenario: 指标命名与标签符合基线

- **WHEN** 系统完成一次运行时提交
- **THEN** 相关指标使用 `ontoaxis_` 前缀与允许的标签，且不含高基数或敏感标签

#### Scenario: 出站调用产生 trace

- **WHEN** 系统调用 Kafka Connect REST API
- **THEN** 产生带依赖名、操作与结果属性的出站 span

#### Scenario: 失败计入结果标签

- **WHEN** 运行时提交或状态读取失败
- **THEN** 对应指标以失败结果标签计数，且不记录凭据
