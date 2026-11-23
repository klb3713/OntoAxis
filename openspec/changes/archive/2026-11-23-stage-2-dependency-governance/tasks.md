# Tasks: Stage 2 依赖版本治理

## 1. 基线与登记

- [x] 1.1 记录 2026-11-23 版本盘点基线
  - 验证：`openspec/changes/archive/2026-11-23-stage-2-dependency-governance/evidence/2026-11-23-baseline.md` 存在，含 compose/Helm/Dockerfile/Makefile 全部镜像与上游最新对比
- [x] 1.2 在 change map 登记支持型 change 与目录所有权
  - 验证：`docs/stages/stage-2-change-map.md` 出现 `stage-2-dependency-governance` 行与 `deploy/deps/` 归属

## 2. 清单与门禁

- [x] 2.1 编写 `deploy/deps/policy.yaml`
  - 验证：`python3 scripts/deps.py check` 解析通过且无未分级组件
- [x] 2.2 生成 `deploy/deps/lock.yaml`
  - 验证：`make deps-lock` 连续执行两次，第二次无 diff
- [x] 2.3 实现 `check` 规则（锁新鲜度、latest 禁令、策略覆盖、例外到期、digest 棘轮）
  - 验证：临时把某镜像改成 `:latest`，`make deps-check` 非零退出并指出文件；恢复后通过
- [x] 2.4 实现 `audit` 报告
  - 验证：`make deps-audit` 生成 markdown，覆盖 lock 中全部组件，无法查询的标 unknown

## 3. 自动化与文档

- [x] 3.1 添加 `renovate.json`
  - 验证：JSON 可解析；major 与 P0 引擎规则 `automerge: false`
- [x] 3.2 添加 `.github/workflows/deps.yml`
  - 验证：YAML 可解析；PR 跑 check、schedule 跑 audit
- [x] 3.3 添加 Makefile target `deps-lock` / `deps-check` / `deps-audit`
  - 验证：三个 target 均可执行且 help 列出
- [x] 3.4 编写 `docs/workflow/DEPENDENCIES.md` 并在 `AGENTS.md`、`RUNBOOK`、Stage 2 brief 建立引用
  - 验证：三处链接可达，brief 含依赖基线小节与退出证据要求

## 4. 破窗修复

- [x] 4.1 `trinodb/trino:latest` → `484@sha256:2704eb53…`
  - 验证：`make deps-check` 通过；`grep -rn "trino:latest" deploy/` 无结果
- [x] 4.2 `bufbuild/buf:latest` → `1.75.0`
  - 验证：`make deps-check` 通过；`grep -rn "buf:latest" Makefile` 无结果

## 5. 验证与证据

- [x] 5.1 `openspec validate --all --strict` 通过
- [x] 5.2 归档审计报告与 check 输出到 change evidence
  - 验证：`openspec/changes/archive/2026-11-23-stage-2-dependency-governance/evidence/2026-11-23-deps-audit.md` 存在

## 6. 显式移交（已转出台账）

以下 3 项不在本 change 内完成，已转入 `docs/stages/stage-2-change-map.md` §11 台账跟踪，
不再以未勾选任务形式保留（归档不豁免 Stage 2 退出条件 6）：

- 6.1 Helm 22 条占位镜像引用回填 + 补 `image.tag`（owner：`production-delivery`，期限 2026-12-31）
- 6.2 GitHub Actions `uses:` 改 commit SHA（owner：`production-delivery`，期限 2027-01-31）
- 6.3 P0 版本升级（各组件 owner，期限见 `deploy/deps/policy.yaml` 的 `review_by`）

## 7. 一致性修正（2026-11-23，评审发现）

- [x] 7.1 修正任务 6.1 的占位数量：18 → 21（platform 5 / data 7 / observability 5 / app 4；7.5 后含 jobs 备份镜像共 22 条引用），同步 proposal、design、`DEPENDENCIES.md`
  - 验证：`openspec/changes/archive/2026-11-23-stage-2-dependency-governance/evidence/2026-11-23-consistency-fixes.txt` 记录 lock 条目分组计数
- [x] 7.2 修正 `deps-check` 汇总字段语义：`placeholders=` 改名为 `exception_covered=`（实际统计的是被例外 scope 覆盖的条目数）
  - 验证：`make deps-check` 输出 `exception_covered=22`，且新增 `W_EXCEPTION_COVERED` 汇总行
- [x] 7.3 chart values 支持 `image.tag`：`extract_helm` 按组件块解析 repository/tag/digest（顺序无关），tag 写入锁文件与审计
  - 验证：`make deps-test` 覆盖 tag 在前/在后/缺 digest 三种情况；临时给 keycloak 加 `tag: "26.0"` 后锁文件出现该 tag
- [x] 7.4 占位 digest 不再冒充「已固定」：审计把例外覆盖行标为「例外覆盖（未验证）」，check 报 `W_NOTAG` / `W_EXCEPTION_COVERED`
  - 验证：`python3 scripts/deps.py check --verbose` 出现 22 条 `W_NOTAG` 与 1 条 `W_EXCEPTION_COVERED`；负向用例仍以退出码 1 拒绝 `:latest`
- [x] 7.5 消除块外镜像盲区：`extract_helm` 改为解析所有 `image:` 映射，`components:` 之外的 `backup.image` 也进 lock/审计；无组件名时按镜像名回退到 policy
  - 验证：`openspec/changes/archive/2026-11-23-stage-2-dependency-governance/evidence/2026-11-23-block-image-coverage.txt`；`make deps-check` 输出 `entries=46 exception_covered=22`；lock 仅新增 jobs 一条
