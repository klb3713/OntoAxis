# Design: Stage 2 依赖版本治理

## 背景与约束

- 仓库现状：版本散落在 `deploy/compose/**`、`deploy/**/Dockerfile`、`deploy/helm/*/values*.yaml`、`Makefile`；Helm values 只允许 `repository + digest`（`_helpers.tpl` 已强制），compose 使用 `repo:tag`。
- 目录所有权（`docs/stages/stage-2-change-map.md` §5）：`deploy/compose/`、`deploy/helm/` 归 `production-delivery`；`docs/stages/evidence/` 归 `stage-2-integration`。本 change 新增 `deploy/deps/` 并只越界修改 Trino 一行。
- 运行环境：本机 Go build cache 位于沙箱外且不可写，无法用 Go 工具做本地验证；CI 与开发机均有 python3。

## 决策

### D1：校验式锁文件，而不是从清单生成部署文件

**选择**：部署文件保持手写；`deps.lock.yaml` 由工具抽取生成，`deps-check` 校验锁与仓库一致。

**替代**：从 `policy.yaml` 渲染 compose / Helm values。

**理由**：渲染会把 compose 变成构建产物，改端口、加健康检查都要改模板，且与本仓库手写部署资产的习惯冲突；锁文件只解决「可 diff、可审计、能发现漂移」，实现成本低、可回退。

### D2：policy 与 lock 分离

`policy.yaml` 手写、变化少（tier/owner/review_by/例外）；`lock.yaml` 生成、随镜像变动（image/tag/digest）。两者职责不同，合并会导致每次镜像变动都要人工改策略文件。

### D3：python3 标准库实现，不引入新依赖

**替代**：Go 工具（仓库已有 `yaml.v3`）、PyYAML、yq。

**理由**：Go 构建缓存不可写导致本地无法验证工具自身；PyYAML 需要新增依赖；yq 需要额外安装。实现只做行级抽取与受限格式解析，标准库足够。代价是 `policy.yaml` 只支持受限子集（两层嵌套、标量、例外列表），由 `deps-check` 对解析失败显式报错，避免静默错读。

### D4：digest 用日期棘轮，而不是立即全量强制

**选择**：`digest_required_from: 2027-01-31`；在此之前缺 digest 只警告。

**理由**：给 21 个 Helm 组件（另有 jobs 备份镜像 1 条）和 14 个 compose 镜像（另有 4 处 Dockerfile 与 3 处 Makefile）补 digest，需要先逐个决定 tag（等于顺带做一轮版本升级），会掩盖行为变化，也与 `production-delivery` 的在途工作冲突。用日期棘轮把「可复现」变成有期限的强制项，而不是长期例外。

### D5：Helm 占位 digest 登记为到期例外

**选择**：占位引用不逐组件打标记（同一组件在 compose 与 Helm 可能一份真实一份占位），而是由带 `scope: deploy/helm/` 与 `expires: 2026-12-31` 的例外覆盖；审计报告为覆盖行标注例外 id，不并入「已固定」。

**替代**：改 chart 结构，让生产 values 缺失 digest 时渲染失败。

**理由**：`_helpers.tpl` 已有 `required` 与格式校验，结构改动属于 `production-delivery` 的目录；本 change 只提供检测与到期机制，回填由其执行。

### D6：Renovate 而不是 Dependabot

**理由**：Renovate 支持 docker-compose、Dockerfile、Helm 自定义 manager、digest pinning 与分组；Dependabot 对 Helm values 的 `repository + digest` 拆分写法无原生支持。代价是托管版不执行 `postUpgradeTasks`，锁文件需在 PR 分支手工跑一次 `make deps-lock`，已写入操作文档。

### D7：GitHub Actions 的 SHA pin 不在本次范围

`uses:` 目前为 `@vN`，改成 commit SHA 会产生大量机械 diff，且与依赖版本主题不同。登记为独立后续项（`review_by: 2027-01-31`）。

## 数据流

1. 开发改镜像 → `make deps-lock` 更新锁文件 → `make deps-check` 本地通过 → CI `deps` job 复核。
2. Renovate 开 PR（patch 自动合并、minor 人工、major 与 P0 禁止自动合并）→ CI 跑 `deps-check`；锁文件陈旧时按文档执行 `make deps-lock`。
3. 每月 1 号 `deps` workflow 跑 `deps-audit`，产物作为 stage 证据与 EOL 预警。
4. 阶段退出前把 `deps-audit` 输出归档到 `openspec/changes/<change>/evidence/`，由 `stage-2-integration` 合并进 `docs/stages/evidence/`。

## 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| 受限 YAML 子集解析错读 | 门禁放过违规项 | 解析失败即非零退出；策略文件结构固定并在文档中示例 |
| registry API 限流或不可达 | 审计报告大量 unknown | 单组件超时 8s；unknown 显式标注；审计不阻塞 PR |
| 私有 registry 凭据 | digest 无法解析 | 凭据只走 CI secret，不写入仓库；文档给 `hostRules` 示例 |
| Renovate 托管版不跑 postUpgradeTasks | PR 因锁文件陈旧变红 | 文档给出单命令修复；大版本本来就要人工 |
| 例外长期不处理 | 门禁形同虚设 | 例外必须带 `expires`；到期即失败，CI 每月审计提醒 |

## 迁移与回滚

- 迁移：新增文件与 3 个 Makefile target，不改运行时行为；仅 Trino 一行变更（`latest` → `484@digest`），当前 `latest` 解析结果不可考，固定到 484 属可复现性修复。
- 回滚：删除 `deploy/deps/`、`scripts/deps.*`、`renovate.json`、`.github/workflows/deps.yml`，撤销 Makefile 3 个 target 与 compose 一行即可；无数据迁移、无 schema 变更。