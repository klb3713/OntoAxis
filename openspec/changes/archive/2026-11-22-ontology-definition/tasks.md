# Tasks

## 1. 规划与契约

- [x] 1.1 完成 `proposal.md`，明确 change 动机、范围、能力与影响；验证方式：评审文件包含 Why/What Changes/Capabilities/Impact 且与 Stage 1 brief 的 S1-AC-04 范围一致。
- [x] 1.2 完成 `specs/ontology-metadata/spec.md`，覆盖对象类型、属性、链接类型、数据集绑定、租户隔离与错误契约；验证方式：`openspec validate ontology-definition --strict` 的 spec 部分通过，所有 requirement 均有 4 个 `#` 的 WHEN/THEN scenario。
- [x] 1.3 完成 `design.md`，记录分层、租户上下文、迁移/RLS、HTTP 契约与风险；验证方式：设计评审确认每个关键决策都有备选方案与取舍说明。
- [x] 1.4 完成 `traceability.md`，建立 Stage AC → capability/requirement → scenario → 证据 → 状态映射；验证方式：逐条核对 `specs/ontology-metadata/spec.md` 的 requirement 均被映射且无空证据。

## 2. 领域与服务层

- [x] 2.1 实现 `domain` 实体、分页、校验规则与哨兵错误；验证方式：`go test ./internal/modules/ontology/domain/...` 覆盖合法/非法名称、基数、分页与错误判定。
- [x] 2.2 实现 `service.Store` 窄接口与 `service.Service` 全部 16 个操作的服务层逻辑；验证方式：`go test ./internal/modules/ontology/service/...` 使用 fake store 覆盖成功路径。
- [x] 2.3 补齐服务层拒绝路径测试（重复名称、跨租户不可见、对象类型/链接类型缺失、未绑定物化、删除被引用对象类型）；验证方式：上述测试全部通过且断言具体哨兵错误。

## 3. 持久化与迁移

- [x] 3.1 实现 `migrations` 的幂等 up 语句与 down 语句，覆盖 `ontology.object_types`、`ontology.properties`、`ontology.link_types`、索引、RLS 与 FORCE RLS；验证方式：`go test ./internal/modules/ontology/migrations/...` 校验语句顺序、幂等关键字与表名。
- [x] 3.2 实现 PostgreSQL `repo.Store`，所有语句带 `tenant_id` 并在事务内设置 `app.tenant_id`，唯一冲突与无记录翻译为领域错误；验证方式：`go build ./internal/modules/ontology/...` 与 `go vet` 通过，代码检索确认无 Project/ProjectResource 表引用。
- [x] 3.3 在 `module.Migrate` 中通过 `internal/platform/db.ExecStatements` 执行迁移；验证方式：`go test ./internal/modules/ontology/...` 编译通过；`migrations` 单测覆盖语句集合，`module_test` 覆盖 nil pool 拒绝。

## 4. HTTP API

- [x] 4.1 实现 `/api/v1/ontology` 下对象类型、属性、链接类型、数据集绑定路由与 JSON 编解码；验证方式：`go test ./internal/modules/ontology/api/...` 的 `httptest` 成功用例通过。
- [x] 4.2 实现租户上下文 fail-closed 提取与 `httperr` 错误映射；验证方式：无租户返回 401、非法输入返回 400、不存在返回 404、冲突返回 409 的测试通过。
- [x] 4.3 增加至少一个契约 fixture（`api/testdata/ontology_fixture.json`）并由测试读取校验；验证方式：fixture 驱动的契约测试通过且断言关键字段。

## 5. 模块装配

- [x] 5.1 实现 `module.go` 的 `Name`、`Dependencies`、`Migrate`、`RegisterRoutes`、`Ready`；验证方式：`go test ./internal/modules/ontology/...` 中模块测试断言名称 `ontology`、依赖 `tenancy`、路由前缀与未就绪行为。
- [x] 5.2 确认模块不读写 Project/ProjectResource；验证方式：`grep` 检索 `internal/modules/ontology` 不出现 `project`/`project_resources` 表操作，且模块依赖仅声明 `tenancy` 顺序。

## 6. 集成验证

- [x] 6.1 运行 `gofmt -l internal/modules/ontology` 与 `go vet ./internal/modules/ontology/...`，验证格式与静态检查无输出/无错误。
- [x] 6.2 运行 `go test ./internal/modules/ontology/...`，验证全部测试在无真实 PostgreSQL 环境下通过并保存测试输出作为证据。
- [x] 6.3 运行 `openspec validate --all --strict`，验证 change 结构与 delta spec 校验通过；本 change 已通过 `openspec validate ontology-definition --strict`，聚合命令因其他未完成 change 缺少 delta 仍非零退出（范围外，待集成复跑）。

## Workflow follow-up

- 由评审人确认后归档 change；本 change 不自行归档、不 commit、不宣称 Stage 1 完成。
- 由集成 change 将 `ontology` 模块加入 `cmd/ontoaxis-core/main.go` 的模块列表，并在平台 authn 中间件中调用 `api.WithTenantID` 注入租户上下文。
- 后续 datasets/worker change 以真实物化任务替换当前仅受理的 `MaterializeBinding` 行为。
