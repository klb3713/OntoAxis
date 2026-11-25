# object-search Specification

## Purpose
让对象实例在按租户隔离的搜索索引中可被全文检索与过滤，并在对象新增、修改、删除后通过 outbox 异步收敛保持一致，同时提供可复现的全量重建与授权受控的查询面。

## Requirements

### Requirement: 租户隔离的索引与文档

每个租户的对象写入按租户派生的独立索引，索引文档 MUST 携带租户标识。搜索 MUST 只访问当前认证租户的索引，并额外带租户过滤条件；响应、日志与索引名 MUST NOT 暴露其他租户的文档或可枚举的原始租户标识。

#### Scenario: 跨租户不可见

- **WHEN** 租户 A 的对象已索引，租户 B 以自己的凭证搜索同一关键字
- **THEN** 结果 `items` 不含 A 的文档，且 `total` 为 0

#### Scenario: 租户不可由请求指定

- **WHEN** 搜索请求在 query 或请求头中携带 `tenant_id`/`X-Tenant-Id`，且与认证租户不同
- **THEN** 系统以认证租户为准，不返回任何他租户文档

#### Scenario: 缺少认证租户

- **WHEN** 请求没有可用的认证租户上下文
- **THEN** 返回 401 `unauthorized`，且不执行索引查询

#### Scenario: 索引名不含原始租户标识

- **WHEN** 查看索引名与索引管理输出
- **THEN** 只出现派生标识（可由已知租户标识复算），不含原始租户 UUID

### Requirement: 对象搜索查询契约

系统 SHALL 提供对象搜索查询接口，在当前租户内按关键字与对象类型检索未删除对象。请求 MUST 至少提供非空关键字或对象类型；响应 MUST 使用统一错误信封，并返回 `id`、`object_type_id`、`display_name`、`version`、`updated_at`、`total`、`limit`、`offset`、`truncated`，MUST NOT 返回对象属性原始值。

#### Scenario: 关键字命中

- **WHEN** 已索引对象包含关键字，请求携带该关键字
- **THEN** 结果包含该对象及其 `display_name`

#### Scenario: 缺少检索条件

- **WHEN** 请求未提供关键字且未提供对象类型
- **THEN** 返回 400 `bad_request`

#### Scenario: 响应不含属性原文

- **WHEN** 搜索命中对象
- **THEN** `items` 中不存在 `properties` 字段或对象属性原文

#### Scenario: 非法对象类型标识

- **WHEN** `object_type_id` 不是合法 UUID
- **THEN** 返回 400 `bad_request`

### Requirement: 分页与结果窗口

搜索 MUST 支持 `limit`/`offset` 分页：`limit` 缺省 20、允许 1..100；`offset` MUST ≥0 且不超过配置的检索窗口。命中总数超出窗口时 MUST 以 `truncated=true` 标记并把 `total` 作为下界（估计值），MUST NOT 在未标记的情况下静默截断。非法分页参数 MUST 返回 400。

#### Scenario: 缺省分页

- **WHEN** 请求未提供 `limit` 与 `offset`
- **THEN** 使用 `limit=20`、`offset=0`

#### Scenario: 非法分页参数

- **WHEN** `limit=0`、`limit=101` 或 `offset=-1`
- **THEN** 返回 400 `bad_request`

#### Scenario: 结果窗口截断标记

- **WHEN** 命中总数超过配置的检索窗口
- **THEN** 返回 `truncated=true` 与可分页范围提示，而非无标记截断

#### Scenario: 深分页上限

- **WHEN** `offset` 超过配置上限
- **THEN** 返回 400 `bad_request`，而非返回空集伪装成功

### Requirement: 过滤与排序

搜索 MUST 支持一个或多个对象类型过滤与 `updated_at` 时间范围过滤，并支持按 `updated_at`、`created_at` 升降序排序（白名单）；缺省按相关性排序。非法过滤值或排序字段 MUST 返回 400，MUST NOT 退化为无过滤的全量查询。

#### Scenario: 对象类型过滤

- **WHEN** 请求指定两个对象类型
- **THEN** 只返回这两类文档

#### Scenario: 时间范围过滤

- **WHEN** 请求指定更新时间上下界
- **THEN** 只返回区间内更新的文档

#### Scenario: 排序白名单

- **WHEN** 请求使用白名单外的排序字段或非法方向
- **THEN** 返回 400 `bad_request`

#### Scenario: 过滤器不得放宽租户边界

- **WHEN** 请求指定的对象类型属于其他租户
- **THEN** 不返回该类型任何文档，且不发生跨租户索引访问

### Requirement: 索引文档 schema 与映射版本

索引文档 SHALL 使用固定字段集合与显式声明的可检索、可过滤、可排序属性；索引名 MUST 携带映射版本。映射 MUST 确定（同一对象重复映射得到相同文档），只索引未删除对象，且 MUST NOT 索引敏感键或超出上限的文本。映射变更 MUST 通过新版本索引与全量重建生效，MUST NOT 原地改写映射。

#### Scenario: 映射确定性

- **WHEN** 同一对象按同一映射版本映射两次
- **THEN** 两次生成的文档逐字段相同

#### Scenario: 映射版本升级

- **WHEN** 映射版本从 v1 升级到 v2
- **THEN** 创建 v2 索引并触发重建，v1 索引在切换完成前保留可回退

#### Scenario: 敏感键不进入索引

- **WHEN** 对象属性包含口令、令牌、密钥类键
- **THEN** 索引文档不含这些键的值

#### Scenario: 软删除对象不索引

- **WHEN** 对象已被软删除
- **THEN** 索引中不存在该对象文档

### Requirement: 对象变更事件的可靠捕获

对象创建、更新与删除 MUST 与索引事件在同一数据库事务中提交；事件 MUST 包含租户、对象标识、对象类型、对象版本、操作类型与 schema 版本。事务回滚 MUST NOT 留下事件；事件记录失败 MUST 使对象写请求失败，不得先提交对象再补发事件。

#### Scenario: 提交产生事件

- **WHEN** 对象创建成功提交
- **THEN** 存在与该对象对应的创建事件

#### Scenario: 回滚不产生事件

- **WHEN** 对象写事务因其他步骤失败回滚
- **THEN** 不存在对应事件记录

#### Scenario: 事件写入失败即整体失败

- **WHEN** 事件记录写入失败
- **THEN** 对象写请求返回 500，且对象未落库

#### Scenario: 删除产生删除事件

- **WHEN** 对象被软删除
- **THEN** 产生删除事件并携带对象标识与版本

### Requirement: 异步索引与最终一致窗口

索引刷新 MUST 异步进行，对象写请求 MUST NOT 同步调用搜索服务。对象变更提交后，结果 MUST 在配置的同步窗口内（缺省 5 秒，p95）反映到搜索结果；窗口内允许短暂陈旧，但 MUST NOT 无限期陈旧。核心对象读写可用性 MUST NOT 依赖搜索服务可用性。

#### Scenario: 新增对象可检索

- **WHEN** 对象创建提交
- **THEN** 在同步窗口内可被搜索命中

#### Scenario: 更新生效

- **WHEN** 对象属性更新提交
- **THEN** 窗口内搜索结果的 `display_name` 反映新值

#### Scenario: 删除消失

- **WHEN** 对象软删除提交
- **THEN** 窗口后搜索不再返回该对象

#### Scenario: 写路径不依赖搜索服务

- **WHEN** 搜索服务不可用
- **THEN** 对象创建、更新、删除仍按既有契约成功

### Requirement: 重复与乱序投递的幂等性

同一事件被重复投递或事件乱序到达时，索引 MUST 收敛到数据库当前对象状态：同一对象 MUST 只有一份文档，MUST NOT 出现版本回退、重复文档或删除后复活。消费者 MUST 以读取当前对象行计算文档，而非仅依赖事件载荷。

#### Scenario: 重复投递

- **WHEN** 同一创建事件被消费两次
- **THEN** 索引中只有一份该对象文档且内容一致

#### Scenario: 乱序事件

- **WHEN** 较新的更新事件先于较旧事件被消费
- **THEN** 索引最终为数据库中的最新对象状态

#### Scenario: 删除后重复投递

- **WHEN** 对象删除后旧的更新事件再次投递
- **THEN** 文档保持删除状态，不复活

### Requirement: 租户级全量重建收敛

系统 SHALL 支持按租户的全量重建与回填：在暂存索引构建完成后与线上索引原子 `swap-indexes`，旧索引保留一版供回退。重建期间搜索 MUST 保持可用，期间发生的对象变更 MUST NOT 丢失；切换后增量结果与重建结果 MUST 一致且可复现。重建中途失败 MUST NOT 影响线上索引，MUST NOT 有半成品索引对外。

#### Scenario: 重建可复现

- **WHEN** 对同一租户连续执行两次全量重建
- **THEN** 两次得到的文档集合与摘要一致

#### Scenario: 重建期间写入

- **WHEN** 重建期间对象被更新
- **THEN** 切换并重放窗口内事件后索引为最新状态

#### Scenario: 重建失败不影响搜索

- **WHEN** 重建中途失败
- **THEN** 旧索引继续提供搜索，且无半成品索引对外

#### Scenario: 增量与重建对账

- **WHEN** 连续增量同步后执行全量重建
- **THEN** 两者的文档数量与摘要一致

### Requirement: 单类型修复收敛

系统 SHALL 支持按对象类型的修复式重建：在线上租户索引内原地 upsert 该类型的存活对象并删除已失效文档，MUST NOT 读写或删除其他对象类型的文档，MUST NOT 替换整个租户索引。该操作 MAY 在短时间内对该类型逐批部分可见（非原子），失败后 MUST 可幂等重跑；对超出分页窗口而无法完整枚举的类型 MUST fail-closed，不得依据不完整枚举删除文档。修复完成后 MUST 支持 `--verify` 对账该类型的文档数量与内容摘要。

#### Scenario: 单类型修复不影响其他类型

- **WHEN** 对某租户的 A 类型执行修复式重建
- **THEN** A 类型文档收敛到数据库当前状态，且 B 等其他类型文档数量与内容不变

#### Scenario: 单类型修复可重试

- **WHEN** 单类型修复中途失败
- **THEN** 其他类型搜索不受影响，重新执行同一修复命令后该类型收敛

#### Scenario: 单类型修复摘要对账

- **WHEN** 对同一类型连续执行两次修复并 `--verify`
- **THEN** 两次文档数量与内容摘要一致

### Requirement: 搜索授权过滤

搜索 API MUST 在访问索引前完成授权判定：带对象类型过滤时判定该对象类型作用域的 read 动作，未带时判定租户作用域的 read 动作。拒绝 MUST 返回 403，且不执行查询、不返回文档或计数；授权守卫不可用或超时 MUST 返回 503 并 fail-closed；MUST NOT 用空结果掩盖拒绝。

#### Scenario: 授权通过

- **WHEN** 主体对目标作用域具有 read 权限
- **THEN** 返回该租户内的检索结果

#### Scenario: 越权拒绝

- **WHEN** 主体对目标对象类型或租户无 read 权限
- **THEN** 返回 403，且响应不含任何 `items` 或 `total`

#### Scenario: 授权守卫不可用

- **WHEN** 授权判定超时或返回错误
- **THEN** 返回 503 `unavailable`，且不访问搜索索引

#### Scenario: 跨租户过滤被拒绝

- **WHEN** 主体尝试用其他租户的对象类型过滤
- **THEN** 拒绝请求且不返回任何他租户数据

### Requirement: 失败重试、死信与重放

索引消费失败 MUST 按上限与退避重试；超过上限 MUST 进入死信并保留可重放，死信 MUST NOT 阻塞其他事件；未知事件类型 MUST 安全跳过并计数。搜索服务不可用期间事件 MUST 保留在 outbox，恢复后 MUST 追平；死信保留时间 MUST 不少于 14 天。

#### Scenario: 暂时失败后追平

- **WHEN** 搜索服务短暂不可用后恢复
- **THEN** 未处理事件在恢复后全部应用，索引无丢失

#### Scenario: 超过上限进入死信

- **WHEN** 某事件连续失败达到重试上限
- **THEN** 该事件进入死信并记录错误与次数，其他事件继续处理

#### Scenario: 死信重放

- **WHEN** 修复后重放死信事件
- **THEN** 对应文档被更新，且事件离开死信

#### Scenario: 毒事件隔离

- **WHEN** 事件载荷非法或事件类型不认识
- **THEN** 安全跳过并计数，不阻塞队列

### Requirement: 搜索可观测性与健康隔离

系统 MUST 暴露索引事件结果计数、消费滞后（秒）、重建结果与耗时、搜索请求延迟与错误（复用既有 HTTP RED 指标）；trace MUST 覆盖搜索请求、搜索服务调用与索引消费批次。指标、日志与 span MUST NOT 把对象属性值、凭证或原始查询词作为标签。搜索依赖故障 MUST NOT 使核心就绪检查失败；索引 worker 的就绪检查 MUST 反映其数据库与搜索服务依赖。

#### Scenario: 消费滞后可观测

- **WHEN** outbox 出现积压
- **THEN** 消费滞后指标上升，告警规则可触发

#### Scenario: 核心健康不受搜索影响

- **WHEN** 搜索服务停止
- **THEN** 核心 `/healthz` 与 `/readyz` 仍返回 200，搜索请求返回 503

#### Scenario: trace 串联且不泄漏

- **WHEN** 执行一次搜索请求
- **THEN** trace 同时包含 HTTP 处理与搜索服务调用 span，且不含属性原文或查询词标签

#### Scenario: worker 就绪反映依赖

- **WHEN** worker 的数据库或搜索服务不可用
- **THEN** worker 就绪检查返回 503
