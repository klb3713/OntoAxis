# dependency-governance Specification

## ADDED Requirements

### Requirement: 依赖清单与策略单一真源

仓库 SHALL 以 `deploy/deps/policy.yaml` 作为依赖分级与例外登记的唯一真源，并 SHALL 以 `deploy/deps/lock.yaml` 记录从 compose、Dockerfile、Helm values 与 Makefile 抽取出的实际引用。锁文件 MUST 由工具生成且 MUST NOT 包含时间戳等易变字段，使重复生成的结果可逐字节比较。

#### Scenario: 锁文件与仓库实际引用一致

- **WHEN** 执行 `make deps-check` 且仓库中的镜像引用自上次 `make deps-lock` 后未变更
- **THEN** 校验通过，且不产生任何文件修改

#### Scenario: 引用漂移时门禁失败

- **WHEN** 有人直接修改 compose 中的镜像 tag 而未重新生成锁文件
- **THEN** `make deps-check` 失败，并输出发生漂移的文件与组件名

### Requirement: 镜像引用必须可复现

仓库中所有容器镜像与构建工具链引用 MUST 使用明确 tag，MUST NOT 使用 `latest` 或其它等价浮动标签。每个运行时就绪组件 MUST 在 `policy.yaml` 的 `digest_required_from` 日期之后同时记录 `sha256` digest；在此日期之前缺少 digest 只产生警告。

#### Scenario: latest 被拒绝

- **WHEN** 任一 compose、Dockerfile、Helm values 或 Makefile 中出现 `:latest`
- **THEN** `make deps-check` 以非零状态退出，并列出该引用的来源文件

#### Scenario: digest 棘轮到期后强制

- **WHEN** 当前日期不早于 `digest_required_from`，且某个运行时镜像没有 digest
- **THEN** `make deps-check` 失败；在日期之前同一情况只输出警告

### Requirement: 组件分级与责任归属

`policy.yaml` SHALL 为每个组件声明 tier（`p0` 平台底座、`p1` 运行时与构建链、`p2` 应用库）、owner 与 `review_by` 日期。未在策略中声明的组件 MUST 被门禁视为未分级并失败。

#### Scenario: 新增组件必须分级

- **WHEN** 仓库新增一个未在 `policy.yaml` 中声明的镜像
- **THEN** `make deps-check` 失败，并提示补充 tier、owner 与 review_by

### Requirement: 例外登记与到期失效

确需暂时保留的违规项 SHALL 登记为带 `scope`、`expires` 与 `reason` 的例外；例外一旦过期 MUST 使门禁失败。占位 digest SHALL 由覆盖其源文件路径的例外登记，而不是逐组件加标记，因为同一组件在 compose 与 Helm 中可能同时存在真实与占位引用。

#### Scenario: 例外到期后门禁失败

- **WHEN** 某个例外的 `expires` 日期早于当前日期
- **THEN** `make deps-check` 失败，并指出过期例外 id 与其负责 owner

#### Scenario: 占位 digest 不冒充已修复

- **WHEN** 审计读取到被占位例外的 `scope` 覆盖的引用
- **THEN** 报告中该行标注覆盖它的例外 id，且不得计入「版本已固定」结论

### Requirement: 自动更新通道的合并分级

仓库 SHALL 通过 Renovate 生成依赖更新 PR，并 MUST 遵守：`patch` 级可自动合并；`minor` 级必须人工确认；`major` 级与 P0 引擎（Kestra、Temporal、Trino、Keycloak、OpenFGA、Meilisearch）的更新 MUST NOT 自动合并。自动化 MUST NOT 绕过 Stage/change 授权。

#### Scenario: major 更新不自动合并

- **WHEN** Renovate 检测到某 P0 引擎的 major 版本更新
- **THEN** 该 PR 被标记 `needs-change` 且 `automerge` 为 false

### Requirement: 依赖审计与阶段证据

仓库 SHALL 提供 `make deps-audit`，在联网环境查询上游最新版本与 EOL 信息，输出带时间戳的 markdown 报告。报告 MUST 标注 tier、当前版本、上游最新、落后程度、EOL 状态与 review_by；无法查询的组件 MUST 显式标记为未知，不得推测。

#### Scenario: 审计报告可用于阶段证据

- **WHEN** 在阶段退出前执行 `make deps-audit`
- **THEN** 生成一份包含全部已登记组件与未知项的 markdown 报告，可直接归档为证据

### Requirement: 升级归类

依赖升级 SHALL 按影响面归类：行为不变且不跨主版本的升级 MAY 走 Fast Track；跨主版本、改变对外契约或涉及 P0 运行时的升级 MUST 走 OpenSpec change，并在 proposal 中记录行为差异。

#### Scenario: 引擎跨主版本必须走 change

- **WHEN** 计划把 Kestra 或 Temporal 升级到新的主版本
- **THEN** 必须存在对应 change，且 delta spec 记录错误格式、端点或配置的行为变化