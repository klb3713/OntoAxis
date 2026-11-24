# 安全与数据治理评审清单（object-search）

> 对应任务 **9.2**。§1–§3、§5 为事实清单与待决问题；§4、§6 记录 2026-11-24 评审人 **klb3713** 的结论与决策处置。
> 本文件只陈述代码事实与已验证证据；结论以 §4 评审表为准，不泛化判定“合规/合格”。

## 1. 范围

- 被索引的数据：`object_data.objects` 中未软删除的对象，字段映射见 `internal/platform/search/mapping.go`。
- 索引存储：Meilisearch，每租户一个索引 `objects_<version>_<sha256(tenantUUID)[:16]>`。
- 读取路径：`GET /api/v1/objects/search`（core），强制附加 `tenant_id` filter；授权接缝见 `internal/app/search.go`。
- 写入路径：`jobs.outbox` → `ontoaxis-search consume`；重建：`ontoaxis-search rebuild`。

## 2. 事实清单（可直接核对的实现与证据）

> 下表「评审状态」列反映 2026-11-23 初稿；2026-11-24 的评审结论与逐项处置见 §4、§6（S11/S12/S13/S15 等标「待评审」项已在 §6 给出处置）。

| # | 检查项 | 当前实现/证据 | 状态 |
| --- | --- | --- | --- |
| S1 | 索引文档字段最小化 | `mapping.Document` 仅 `id/tenant_id/object_type_id/display_name/properties_text/version/created_at/updated_at`；`displayedAttributes` 不含 `properties_text`/`tenant_id`（`SettingsJSON`） | 代码事实，单测覆盖 |
| S2 | 敏感属性排除 | `sensitiveKeyPattern = (?i)password\|passwd\|secret\|token\|api_key\|credential\|private_key`，命中的键**整棵子树**不进入 `properties_text`（`flattenMap`） | 单测 `TestFlattenProperties*` 覆盖；E2E 验证 `secret` 不入索引 |
| S3 | 属性深度限制 | `flattenMap` 在 `depth > 2` 停止（根 map 为 depth 1），更深层嵌套不索引 | 代码事实，单测覆盖 |
| S4 | 文本截断 | `properties_text` 按 UTF-8 字符边界截断到 4096 字节（`truncateUTF8`） | 单测覆盖 |
| S5 | 租户隔离（存储） | 每租户独立索引；索引名用租户 UUID 的 sha256 前 16 位，不暴露原始 UUID | 集成测试 `TestIntegrationTenantIsolation`、E2E 租户 B 检查 |
| S6 | 租户隔离（查询） | core 的 `searchBackend.Search` 总是附加 `tenant_id = "<uuid>"` filter；Meilisearch 侧 filterable 含 `tenant_id` | 集成测试 `TestIntegrationTenantIsolation`；core 侧 filter 逻辑见 `internal/app/search.go`（无独立单测） |
| S7 | 认证/授权 | 搜索路由要求 `authn.RequireTenant`，通过 OpenFGA/Cedar 守卫判定租户范围或经租户内事实校验的对象类型范围；不实现逐对象实例/字段级过滤 | R10 有处理器回归和 [真实本地 Keycloak/OpenFGA/Cedar HTTP 证据](evidence/2026-11-24-object-search-verification.md#5-原生进程-http-功能矩阵非-compose)；安全评审已通过（§6），Compose 主文件合并归 `production-delivery` |
| S8 | 软删除语义 | consumer 对 `objects.object.deleted.v1` 或读穿 `deleted_at IS NOT NULL` 时删除索引文档；重建只扫描未删除行 | 集成测试 `TestIntegrationConsumerAppliesAndPublishes`、`TestIntegrationRebuild*` |
| S9 | 密钥处理 | Meilisearch master key 经 `ONTOAXIS_SEARCH_API_KEY` 注入；客户端错误不携带响应体；日志不打印 key（`client.go`） | 代码事实；Compose 本地 key 为开发值，生产须 Secret |
| S10 | 索引数据可清除性 | 删除对象→索引文档删除；租户注销/索引删除流程**未实现**（runbook §7 提供手工删除命令） | 未实现，需后续 change |
| S11 | 备份/快照中的数据 | Meilisearch 数据卷快照包含索引文本；是否允许落盘/加密/保留期限未定义 | 待评审 |
| S12 | 敏感键覆盖范围 | 正则只覆盖英文键；中文键（如 `身份证`、`手机号`、`银行卡`）及值级 PII 不识别 | 待评审，可能需要扩展规则或字段级策略 |
| S13 | `properties_text` 的业务可见性 | 索引文本包含属性键值（排除敏感键、深度≤2）；是否含商业敏感信息未评审 | 待评审 |
| S14 | trace/日志泄露 | 请求传播 request id/traceparent；日志只记录索引 UID、事件 id、错误类别，不记录文档内容 | 代码事实，需评审确认日志采样策略 |
| S15 | 数据驻留/跨境 | 未定义 Meilisearch 部署区域与数据驻留要求 | 待评审（部署要求文档已列 Secret/PVC，不含驻留） |

## 3. 需要评审人决策的问题

1. **S12**：敏感键正则是否足够？是否要求“允许列表（仅索引显式声明的字段）”替代当前“排除列表”？若要求允许列表，需新 change 修改 `mapping` 与对象类型 schema。
2. **S13**：`properties_text` 是否允许包含全部非敏感属性？是否需要按对象类型配置可索引字段？
3. **S7/R10**：当前守卫判定租户/对象类型范围，没有逐对象实例或字段级授权。若真实环境要求对 marking、实例或字段收窄，需明确可索引对象范围并另行批准实现，不得把 R10 的作用域守卫视为逐行授权。
4. **S10**：租户注销/数据删除的索引清理是否必须与对象数据删除同事务/同 SLA？当前需手工删除索引。
5. **S11/S15**：Meilisearch PVC 快照的加密、保留期、部署区域是否有硬性要求？若有，登记到 `production-delivery` 的部署要求与后续 change。
6. **S14**：dead letter 中 `last_error` 是否可能包含 Meilisearch 响应片段？当前客户端错误不携带响应体，但需评审确认。
7. **S5**：索引名哈希截断 16 hex（64 bit）是否满足不可逆/防枚举要求？是否需要更长哈希或随机化？
8. **质量阈值分级（2026-11-24 负责人批准）**：合成夹具质量度量拆为「工程回归门」（冻结语料 + 不低于基线，`engineering_gate` 已签，接 CI `--strict`）与「业务质量门」（recall@5=0.9 / MRR@10=0.8，仍 `pending-owner-signoff`）。业务门需代表性语料与标注规范后才能签核；在此之前任何搜索质量数值不得写入验收结论。

## 4. 评审结论（2026-11-24 评审人 klb3713 确认）

| 项 | 结论 | 评审人 | 日期 | 后续 change |
| --- | --- | --- | --- | --- |
| 索引字段与敏感属性边界 | **通过**（接受 S1–S4：排除列表 `password/passwd/secret/token/api_key/credential/private_key` + 深度≤2 + 4096 字节截断；S12/S13 细化登记后续） | klb3713 | 2026-11-24 | S12/S13 见 §6 后续登记 |
| 租户/对象级隔离 | **通过**（R10 作用域守卫；逐对象实例/字段级授权缺失登记后续，不视为逐行授权） | klb3713 | 2026-11-24 | 逐对象实例/字段级授权另行评估 |
| 数据保留/删除/备份 | **通过**（S10 手工删除流程现状接受；租户注销索引清理与 S11 备份加密/保留期登记后续） | klb3713 | 2026-11-24 | 删除流程 change 登记 |
| 密钥与日志 | **通过**（S9 key 经 Secret 注入、错误不带响应体；S14 日志不记录文档内容现状接受） | klb3713 | 2026-11-24 | — |
| 质量阈值分级（工程门 / 业务门） | 工程回归门已签；业务质量门**未评审/保持 pending** | klb3713（工程门） | 2026-11-24 | 业务门待代表性语料与标注规范 |

## 5. 风险登记（本 change 内新增，2026-11-24）

| ID | 风险 | 影响 | 缓解 | 状态 |
| --- | --- | --- | --- | --- |
| R-1 | 单类型修复式重建非原子，短暂部分可见 | 该类型搜索结果在秒级窗口内可能新旧混杂 | 归为修复/收敛语义并在手册标注；执行前追平 outbox、执行后 `--verify`；失败可幂等重跑；不影响其他类型（见 `design.md` D15） | 已接受（负责人 2026-11-24） |
| R-2 | 合成语料仅 24 文档 / 6 查询 | recall/MRR 分辨率粗，无法外推真实语料；MRR@10 在 24 文档下几乎必为 1.0 | 仅作工程回归门；业务阈值延后到代表性语料（≥30 查询/类型、≥300 文档、含权限过滤与中文分词） | 已登记，业务门待评审 |
| R-3 | `properties_text` 含非敏感属性键值（S13） | 可能纳入商业敏感信息 | 按敏感键排除 + 深度/长度上限；若要求白名单投影另开 change | 待评审（S12/S13） |
| R-4 | 阈值门依赖 CI 实际运行 | 无 runner 时门禁不生效，漂移不被发现 | 8.2 待补 CI run 链接；本地 `--strict` 可复现 | 未闭环（8.2） |

> 评审已于 2026-11-24 由评审人 klb3713 完成（见 §4、§6）。仍不得据此判定业务/合规合格；业务质量门保持 pending。

## 6. 待决问题处置记录（2026-11-24，评审人 klb3713 确认：按材料建议处置并登记后续）

| 问题 | 处置结论 | 后续去向 |
| --- | --- | --- |
| S12 敏感键覆盖 | 保留现有排除列表（英文键）+ 深度/长度上限；中文键与值级 PII 的允许列表方案不作本次实现 | 登记后续安全加固 change |
| S13 `properties_text` 可见性 | 接受当前“排除敏感键、深度≤2、4096 截断”；按对象类型配置可索引字段留待评估 | 后续评估 |
| S7/R10 对象级授权 | 接受当前租户/对象类型作用域守卫；明确**不视为逐对象/字段级授权** | 逐对象实例/字段级授权另行批准 |
| S10 删除/租户注销 | 接受 §7 手工删除索引；租户注销索引清理流程 | 登记后续 change |
| S11/S15 备份与驻留 | 快照加密、保留期、部署区域暂无硬性要求 | 登记 `production-delivery` 部署要求与后续 change |
| S14 日志/dead letter | 接受现状：客户端错误不带响应体，日志不记录文档内容 | — |
| S5 索引名哈希 | 接受 16 hex（64 bit）不可逆/防枚举 | — |
| 质量阈值分级 | 工程门已签并接 CI；业务质量门保持 `pending-owner-signoff` | 代表性语料与标注规范后再签 |