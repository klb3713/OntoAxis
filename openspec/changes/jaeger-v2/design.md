# Design: Jaeger v1 → v2 迁移

## 背景与约束

- 当前引用：Compose `jaegertracing/all-in-one:1.62.0`（无 digest），Helm `deploy/helm/ontoaxis-observability/values.yaml` 只有 `repository + digest`，digest 是 deterministic placeholder。
- 依赖策略：`policy.yaml` 中 jaeger 为 p0、`review_by: 2026-12-31`；`renovate.json` 的 `platform-p0` 名单包含 `jaegertracing/all-in-one`，镜像名变化后名单必须同步，否则 `E_RENOVATE` 直接失败。
- 观测契约：core 通过 OTLP 导出，本地 Jaeger UI 用于查 trace；端口 16686 / 4317 / 4318 是既有约定。
- 本 change 当前只做规划：不修改任何部署资产，实施需单独授权。
- 范围归属：**不属于 Stage 2**（brief §5 未列入、S2-AC-09 不要求），登记为 change map §12 的 Stage 外候选；Stage 2 内的 6.1 先钉 v1 真实 digest 并记录 EOL 风险。

## 决策

### D1：目标版本以 registry 事实为准

2026-11-23 查询 Docker Hub 得到 `jaegertracing/jaeger` 最新非 `latest` 稳定 tag 为 `2.23.0`，作为候选目标版本。实施时的第一步必须是解析该 tag 的真实 digest 并写入 lock，规划阶段不写死 digest。

### D2：v2 配置入口整体替换，不做 v1 兼容层

v1 依赖 `COLLECTOR_OTLP_ENABLED` 一类环境变量与命令行 flag；v2 以配置文件为中心。**选择**：按 v2 官方配置重写后端配置，不保留 v1 环境变量映射。**理由**：兼容层会让「哪些配置仍生效」变得不可判定，也掩盖迁移遗漏；v1 配置残留应在启动阶段显式失败（对应 spec 的「v1 配置不再生效时显式失败」）。

### D3：端口与存储形态保持

UI 仍为 16686，OTLP gRPC/HTTP 仍为 4317/4318，存储仍为内存（本地 profile 用途）。**理由**：core 的 exporter 配置与 smoke 脚本断言不该因后端升级而改，升级面越小越容易回滚。

### D4：镜像名变更同步依赖治理三处

`policy.yaml` 的 jaeger 条目不变（组件名仍是 jaeger），但 `renovate.json` 的 p0 名单、Helm values 的 `repository`、lock 条目必须一起改。**理由**：`deps-check` 的 `E_RENOVATE` 会比对 policy 的 p0 组件镜像与 Renovate 名单，漏改一处就红。

### D5：验证分三层

1. 静态：`helm template` 渲染出 `repo@sha256:`、kubeconform 通过、`make deps-check` 通过；
2. 行为：`--profile observability` 启动后 smoke 脚本能查到 trace，且停掉 Jaeger 时 core 仍可用；
3. 负向：沿用 v1 环境变量启动时后端不就绪，日志指出无法识别的配置项。

## 数据流

1. 开发改后端版本/配置 → `make deps-lock` 更新锁文件 → `make deps-check` 本地通过 → CI `deps` job 复核。
2. `--profile observability` 启动 → core 经 OTLP 4317 导出 → Jaeger v2 内存存储 → UI 16686 查询。
3. 迁移完成后删除 `DEPENDENCIES.md` 中「Jaeger v1 已 EOL」的已知缺口条目，并更新 policy 的 `review_by`。

## 风险与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| v2 配置 schema 与 v1 差异大 | 后端启动失败或采集不到 trace | 先在本地 profile 验证 smoke，再改 Helm；保留 v1 配置片段供回滚 |
| v2 镜像不含「all-in-one 等价」启动方式 | 需要额外组件或参数 | 实施第一步用 `--help`/配置样例确认单进程启动方式，确认不了则暂停并回报，不猜 |
| 镜像名漏改 Renovate 名单 | `deps-check` 失败 | 把 policy/lock/renovate/values 四处作为同一个任务验收 |
| UI 端口或查询行为变化 | smoke 断言失效、runbook 失效 | 端口契约写入 spec；smoke 失败即视为迁移未完成 |
| 内存存储重启丢 trace | 误判为采集故障 | runbook 标注本地 profile 非持久化，不据此判断生产可用性 |

## 迁移与回滚

- 迁移：改 4 处引用（compose、Helm values、policy/renovate 名单、lock）+ 后端配置；不涉及数据库与数据迁移。
- 回滚：单行回退镜像与配置到 v1 版本（保留在文档中的旧片段），跑同一套 smoke；因本地为内存存储，无数据兼容问题。
- 生产侧：本 change 不改变生产拓扑与存储后端；生产迁移若需要持久化与 HA，另立 change。
