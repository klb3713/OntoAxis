# Tasks: Stage 2 依赖版本治理

## 1. 基线与登记

- [x] 1.1 记录 2026-11-23 版本盘点基线
  - 验证：`openspec/changes/stage-2-dependency-governance/evidence/2026-11-23-baseline.md` 存在，含 compose/Helm/Dockerfile/Makefile 全部镜像与上游最新对比
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
  - 验证：`openspec/changes/stage-2-dependency-governance/evidence/2026-11-23-deps-audit.md` 存在

## 6. 未在本 change 内完成（显式移交）

- [ ] 6.1 Helm 18 个占位 digest 回填（owner：`production-delivery`，期限 2026-12-31）
- [ ] 6.2 GitHub Actions `uses:` 改 commit SHA（`review_by: 2027-01-31`）
- [ ] 6.3 P0 版本升级（Jaeger v2、Temporal 1.31、Keycloak 26.x、Meilisearch、Argo CD 3 等）按 change 单独排期