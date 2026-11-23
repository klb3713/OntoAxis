# 依赖版本治理

> 适用：`deploy/` 下的镜像引用、构建工具链与固定版本工具。
> 真源：`deploy/deps/policy.yaml`（手写策略）+ `deploy/deps/lock.yaml`（生成快照）。
> 工具：`scripts/deps.py`（python3 标准库，无第三方依赖），入口 `scripts/deps.sh`。

## 1. 为什么不是「从 YAML 生成部署文件」

部署资产（compose、Helm values、Dockerfile、Makefile）保持手写，工具只做两件事：
抽取引用生成锁文件、校验策略与锁一致。

理由：生成式会把 compose 和 Helm values 变成构建产物，评审时看不到真实 diff，
也会和 `deploy/helm/*/templates/_helpers.tpl` 现有的 digest 强制校验打架。
校验式保留现有资产形态，同时拿到「版本集中可见、变更可 diff、漂移会失败」的收益。

## 2. 命令

```bash
make deps-lock     # 重新抽取并写回 deploy/deps/lock.yaml（改过任何镜像引用后必跑）
make deps-check    # 离线校验，CI 阻塞项；退出码非 0 即失败
make deps-audit    # 联网查询上游最新版本与 EOL，输出 markdown（不写文件）
```

需要存档时：

```bash
make deps-audit > openspec/changes/<change>/evidence/deps-audit-$(date +%F).md
```

CI 在 `.github/workflows/deps.yml`：每次 push/PR 跑 `deps-check`；每月 1 日额外跑
`deps-audit` 并把报告作为 artifact 保留 180 天。

## 3. 策略字段

`deploy/deps/policy.yaml` 的每个组件：

| 字段 | 含义 |
| --- | --- |
| `tier` | `p0` 平台底座 / `p1` 运行时与构建链 / `p2` 应用库与自有镜像 |
| `owner` | 负责评审升级的 change 或团队，不写个人 |
| `review_by` | 下次必须复核的日期；`deps-check` 不判过期，但审计报告会展示，逾期在 Stage 评审时追 |
| `eol_source` | endoflife.date 的 slug，用于审计报告 EOL 列 |
| `support_until` | 已知的上游支持截止 |

顶层 `digest_required_from`：digest 棘轮日期。在此之前缺 digest 只是 warning，
之后变成 error。当前为 `2027-01-31`，选择该日期而不是立即强制，是因为补齐
18 个 Helm 与 12 个 compose 的 digest 必须先定 tag，等于顺带升级，会掩盖行为变化。

### 分级与合并规则

| tier | 升级时机 | 通道 | 自动合并 |
| --- | --- | --- | --- |
| p0 | EOL 前 90 天必须升级 | OpenSpec change | 禁止 |
| p1 | 跟随 LTS | minor 可 Fast Track，major 走 change | 仅 patch/digest |
| p2 | 按 semver 或提交 | PR | 视 lockfile 类型 |

`renovate.json` 的 `platform-p0` 规则必须覆盖 policy 中所有 p0 组件，
且 `automerge` 显式为 `false`。`deps-check` 会比对两边，漂移即失败。

## 4. 升级一个组件

1. 改部署资产里的 tag（compose / Dockerfile / Makefile），Helm 改 `repository` + `digest`。
2. `make deps-lock` 重新生成锁文件，确认 diff 只有预期条目。
3. `make deps-check` 通过。
4. 跑受影响的门禁：`make test`、`make smoke-*`、`pnpm check` 等，按组件归属选择。
5. 按 tier 决定走 change 还是 Fast Track；提交说明写明旧值、新值、验证命令与剩余风险。

升级 p0 引擎（Kestra、Temporal、Trino、Kafka、Keycloak、PostgreSQL）必须走 change：
这些组件的 minor 号里出现过 breaking（Kestra 2.0.0 把错误体改成 RFC 9457 problem+json）。

## 5. 例外

格式：

```yaml
exceptions:
  - id: <短横线 id>
    scope: deploy/helm/
    owner: <change 或团队>
    expires: 2026-12-31
    reason: <为什么现在不修，以及到期后做什么>
```

规则：

- `expires` 必填；过期后 `deps-check` 直接失败，不靠人记得。
- `scope` 是源文件路径前缀，用于把例外限定到某一类资产；不带 scope 的例外视为全局，不要用。
- 例外只登记「暂时做不到」，不登记「不想做」。到期前要么修掉，要么重新评审并改日期。

当前例外：

| id | scope | 到期 | 说明 |
| --- | --- | --- | --- |
| `helm-placeholder-digests` | `deploy/helm/` | 2026-12-31 | Helm values 的 digest 是 deterministic placeholder，回填真实 digest 需先定 tag，移交 production-delivery |

## 6. 已知缺口

- **Helm 无法审计版本**：values 只有 `repository` + `digest`，没有 tag，审计只能报「无 tag」。
  例外到期前必须回填真实 digest，并在 values 里补 `tag` 供 Renovate 与审计识别。
- **GitHub Actions 仍是浮动 tag**（`@v4` 等）。`helpers:pinGitHubActionDigests` 会提 PR 钉到
  SHA，但本 change 不强制门禁。
- **自有镜像**（`ghcr.io/ontoaxis/*`）由 CI 构建，不走版本升级，审计跳过上游查询。
- **审计的网络依赖**：Docker Hub / Quay / endoflife.date 任一不可达时对应行记为 `unknown`，
  不推测、不冒充成功。
- **Jaeger**：`jaegertracing/all-in-one` 的 1.x 镜像仍在发布，但 Jaeger v1 已 EOL；
  迁移到 v2 是独立 change，不在版本号里悄悄发生。

## 7. 与 Stage/OpenSpec 的关系

- 依赖基线属于 Stage 范围的一部分：Stage brief 的 Dependency baseline 一节引用本文件。
- 月度审计报告是 Stage 退出的证据之一，存入 `docs/stages/evidence/`（由 stage-2-integration 汇总）
  或 change 自己的 `evidence/`。
- 本文件的命令变更需要同步 `AGENTS.md` 第 5 节与 `docs/workflow/RUNBOOK.md`。