# Tasks: object-search-meilisearch

> 规划态：Wave 2 门禁 G1 在 change map 中尚无 `pass` 记录，本 change 在获得实施授权前不开始写代码。

## 1. 索引契约与事件模型

- [x] 1.1 定义索引名派生、映射版本与索引设置常量（验证：单元测试断言同租户名稳定、不同租户名不同、索引名不含原始 UUID、非法 `index_version` 被拒绝）
- [x] 1.2 实现文档投影 `display_name` 与 `properties_text`（验证：表驱动单元测试覆盖中文、嵌套深度、超长截断、敏感键排除、非标量忽略、键序确定性）
- [x] 1.3 定义对象事件契约（`objects.object.created.v1`、`updated.v1`、`deleted.v1`）与最小载荷序列化（验证：单元测试往返一致、未知字段容错、载荷不含属性值）
- [ ] 1.4 worker 自管迁移创建 `search.outbox_attempt` 与 `search.index_state`（验证：临时 PostgreSQL 上 goose up→down→up 通过，`\d search.*` 表结构断言）

## 2. Meilisearch 客户端

- [x] 2.1 实现索引创建/删除/设置应用（验证：真实 Meilisearch 集成测试重复执行设置幂等、无索引时删除不报错）
- [x] 2.2 实现文档批量 upsert/delete（验证：集成测试 500 条分批写入、主键覆盖、删除后不可见）
- [x] 2.3 实现搜索查询（`q`、`filter`、`sort`、`limit`/`offset`、`maxTotalHits` 截断）（验证：集成测试过滤组合、排序方向、窗口截断标记）
- [x] 2.4 实现任务等待与 `swap-indexes`（验证：集成测试等待到 `succeeded`、切换后查询命中新索引、旧索引仍可访问）
- [x] 2.5 实现错误映射、请求超时与 trace/请求 ID 传播（验证：单元测试错误分类；端点停机时返回 `ErrUnavailable` 而非 panic）
- [x] 2.6 实现 `health` 探测（验证：集成测试健康/不健康两种断言）

## 3. 对象写路径与 outbox 生产

- [x] 3.1 实现"对象变更 + `jobs.outbox` 同事务"辅助函数（验证：集成测试提交后事件行存在；故意回滚后无事件行）
- [x] 3.2 接入 Create/Update/Delete 三条写路径并填充事件类型、聚合、版本、操作（验证：集成测试断言事件行数与字段；404/409 路径不产生事件）
- [x] 3.3 保证事件写入失败使业务写入失败（验证：注入 outbox 写错误，断言返回 5xx 且对象行未提交）
- [x] 3.4 事件载荷携带 schema 版本与发生时间（验证：单元测试断言字段存在且格式正确）
- [x] 3.5 回归既有对象 HTTP 契约（验证：`go test ./internal/modules/objects/...` 全绿，既有 handler 测试语义未变）

## 4. 索引消费者

- [ ] 4.1 实现批量 claim（`FOR UPDATE SKIP LOCKED`）与同事务标记 `published_at`（验证：集成测试并发两消费者不重复处理；事务中断后事件可被再次领取）
- [x] 4.2 实现 read-through 应用：读当前对象行 → upsert 或 delete（验证：集成测试覆盖创建、更新、软删、软删后重建事件）
- [x] 4.3 实现重复与乱序投递收敛（验证：集成测试重复事件后单文档；先新后旧事件最终等于数据库当前态）
- [x] 4.4 未知事件类型跳过并计数（验证：集成测试插入未知事件，断言指标计数、该事件被标记且不阻塞后续）
- [x] 4.5 实现失败退避、重试上限与死信（`search.outbox_attempt`）（验证：注入失败断言 `attempts`/`next_attempt_at` 递增与 `dead_at` 置位；死信存在时后续事件仍被处理）
- [x] 4.6 实现 `replay` 子命令重放死信（验证：集成测试重放后 `published_at` 置位、文档收敛、`dead_at` 清空）
- [x] 4.7 记录索引滞后指标（消费时 outbox 年龄）（验证：集成测试构造延迟事件，断言直方图样本落入预期桶）

## 5. 全量重建与对账

- [ ] 5.1 实现按租户（可选对象类型）重建（验证：集成测试两个租户各自重建互不影响，索引设置正确）
- [ ] 5.2 实现 keyset 分批扫描（每批 500）与逐批任务等待（验证：集成测试文档数与数据库行数一致，分页无重复/遗漏）
- [ ] 5.3 实现暂存索引 → `swap-indexes` 原子切换 → 旧索引保留一版（验证：集成测试切换期间查询连续可用，旧索引仍可查询）
- [ ] 5.4 实现重建窗口事件重放（`created_at >= rebuild_started_at`）（验证：重建过程中并发写入，完成后断言文档与数据库一致）
- [ ] 5.5 实现 `--verify` 摘要对账（验证：一致时退出码 0；人为删改一条索引文档后退出码非 0 并打印差异摘要）
- [ ] 5.6 实现失败清理暂存索引（验证：注入中途失败，断言线上索引未变、暂存索引被删除）
- [ ] 5.7 验证两次重建可复现（验证：E2E 连续重建两次，摘要一致）

## 6. 搜索查询 API

- [x] 6.1 注册 `GET /api/v1/objects/search` 并实现参数解析与校验矩阵（验证：handler 单元测试覆盖 `q` 与 `object_type_id` 均缺失、`limit=0/101`、负 `offset`、非法 `sort`、>10 个类型，全部 400 且为统一错误信封）
- [x] 6.2 组装响应 `items`/`total`/`limit`/`offset`/`truncated`/`request_id`（验证：handler 单元测试断言字段与类型；响应不含 `properties` 原文）
- [x] 6.3 租户只取自 `authn.RequireTenant`，不使用 `X-Tenant-Id` 回退（验证：无认证上下文返回 401；仅带 `X-Tenant-Id` 头返回 401）
- [x] 6.4 查询始终附加 `tenant_id` 过滤（验证：集成测试跨租户查询返回空集，指标/日志不含他租户数据）
- [x] 6.5 接入平台授权守卫接缝（**阻塞**：依赖 `authorization-openfga-cedar` 落地；验证：守卫契约单元测试 + 安全评审确认；守卫缺失时搜索路由不启用，禁止允许一切替身）
- [x] 6.6 接入 `ONTOAXIS_SEARCH_ENABLED` 开关（验证：关闭时搜索路由返回 404，`objects` 既有路由不受影响）
- [x] 6.7 实现搜索降级（验证：Meilisearch 停机时搜索返回 503 `unavailable`+`retryable`，同进程对象 CRUD 与 `/readyz` 正常）

## 7. Worker 与部署衔接

- [x] 7.1 新增 `cmd/ontoaxis-search` 子命令 `consume`/`rebuild`/`replay`/`migrate`（验证：`go build ./cmd/ontoaxis-search`，`migrate` 重复执行幂等）
- [x] 7.2 实现 worker 健康/就绪/指标端点与优雅关闭（验证：E2E 断言端点；SIGTERM 后当前批次完成、无事件重复处理）
- [x] 7.3 接入 telemetry 与 trace 跨度（验证：E2E 断言 trace 有 `search.consume.*`/`search.rebuild`；span 属性无租户 UUID、属性值、查询原文、凭证）
- [x] 7.4 在 `internal/platform/config` 增加搜索配置项（验证：单元测试缺省值、非法 URL/超时/批次返回错误、`enabled` 时缺 API key 报错）
- [ ] 7.5 产出 `deploy/compose/compose.yaml` 的 `search` profile 片段并交 `production-delivery`（验证：`docker compose -f deploy/compose/compose.yaml --profile search config` 通过；`up -d` 后 meilisearch 与 worker healthy）
- [ ] 7.6 产出 Meilisearch 与 worker 的 Helm/ArgoCD 要求（Deployment/Service/PVC/Secret/Probe/资源）并交 `production-delivery`（验证：`helm template` 渲染通过 + kind 部署评审）
- [ ] 7.7 确认 `go.mod`/`go.sum` 无改动（验证：`git diff --exit-code go.mod go.sum`）

## 8. 端到端、证据与质量

- [ ] 8.1 编写 `scripts/object-search-e2e.sh`（验证：`search` profile 下通过 create→search→update→search→delete→search、重建、租户隔离、停机降级，输出 `PASS/FAIL` 矩阵）
- [ ] 8.2 集成测试接入真实 PostgreSQL + Meilisearch 且 CI 必跑（验证：CI job 绿；`ONTOAXIS_SEARCH_TEST_*` 未注入时本地 skip、CI 不 skip）
- [ ] 8.3 建立中文检索评测集与基线断言（验证：`fixtures/search/` 合成数据评测通过；阈值需负责人签核后才可以此判合格）
- [ ] 8.4 采集证据并交 `stage-2-integration` 收录（验证：`docs/stages/evidence/` 中证据文件含命令、关键输出摘要、CI/运行记录链接）
- [x] 8.5 静态与竞态检查（验证：`go build ./...`、`go vet ./...`、`go test ./...`、`go test -race ./...` 全部通过）

## 9. 文档与评审

- [ ] 9.1 编写运维手册：重建、重放、降级、索引清理、映射升级（验证：评审通过，命令可直接复现）
- [ ] 9.2 安全与数据治理评审：索引字段与敏感属性边界（验证：安全评审记录；如有更严要求则登记后续 change）
- [ ] 9.3 change 评审与 Wave 2 准入核对（验证：评审记录；G1 `pass` 记录存在后才进入实施）
- [ ] 9.4 更新 `traceability.md` 的实际证据映射（验证：每条 requirement 的 evidence 与证据文件一致，planned 不写成 actual）

## Workflow follow-up

- 开工前在 change map 记录 G1 `pass` 并取得 change 级实施授权（当前未满足）
- 与 `stage-2-integration` 协调 `internal/app/` 装配 patch 的合并窗口，与 `production-delivery` 协调 Compose/Helm 片段合并
- `authorization-openfga-cedar` 落地后补 6.5 与授权过滤证据；在此之前授权证据标记 blocked，不得用替身过关
- 若评审认定"对象写路径写 outbox"属于 `objects` 契约变更，则补 `### Modified Capabilities` 与 `specs/objects/spec.md` delta
- outbox 行清理策略、租户注销索引清理、生产 HA/备份另行开 change 或纳入 `production-delivery`
- 归档前执行 `openspec validate --all --strict`、`openspec status --change object-search-meilisearch`，核对 tasks 勾选均有证据
