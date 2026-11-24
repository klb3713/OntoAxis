# object-search 部署要求（Helm / ArgoCD）

> 状态：**要求文档（requirements），不是已实现的 chart**。
> Owner：`object-search-meilisearch` change；交付对象：`production-delivery`（`deploy/helm/`、`deploy/argocd/` 的 owner）。
> 本 change 不修改 `deploy/helm/`、`deploy/argocd/`；下列内容需由 `production-delivery` 落到对应 chart / Application 后，
> 才能勾选任务 7.6 的验收项（`helm template` 渲染 + kind 部署评审）。

## 1. 组件与命名

| 组件 | 类型 | 建议命名 | Namespace |
| --- | --- | --- | --- |
| Meilisearch | StatefulSet（单副本 + PVC） | `ontoaxis-search-meilisearch` | `oa-platform` |
| 索引消费者 | Deployment | `ontoaxis-search-worker` | `oa-platform` |
| 迁移 Job | Job（ArgoCD PreSync hook） | `ontoaxis-search-migrate` | `oa-platform` |
| 重建 Job | 手动 Job 模板（不挂 CronJob） | `ontoaxis-search-rebuild` | `oa-platform` |

约束：

- Meilisearch 使用 **单副本**（官方 OSS 无内建多节点复制）；不要用 Deployment + `Recreate` 冒充高可用。
- 不要暴露公网 Ingress；Meilisearch 仅在集群内由 core 与 worker 访问。
- 与现有 chart 一致：镜像 pin digest、`securityContext` 非 root、`automountServiceAccountToken: false`、
  ExternalSecret 注入、`resources` 必填、`topologySpreadConstraints`/PDB 与同 plane 服务一致。

## 2. Meilisearch StatefulSet

| 要求 | 值 |
| --- | --- |
| 镜像 | 与 `deploy/deps/lock.yaml` 锁定的 `getmeili/meilisearch` 版本一致（当前 `v1.11`，digest 见 lock 文件），禁止浮动 tag |
| 端口 | `7700`（HTTP API，仅集群内 Service） |
| 环境变量 | `MEILI_ENV=production`、`MEILI_NO_ANALYTICS=true`；**主密钥必须来自 Secret**（见 §5），不得写入 values 明文 |
| 数据卷 | PVC `search-data`，建议 20 GiB 起，StorageClass 由环境 values 指定；`/meili_data` |
| 探针 | `readinessProbe` / `livenessProbe`: `GET /health`（HTTP 200），initialDelay 15s / period 10s / failureThreshold 3 |
| 资源 | requests `cpu: 500m, memory: 1Gi`；limits `cpu: 2, memory: 2Gi`（按租户数与对象量调整） |
| 安全上下文 | `runAsNonRoot: true`、`allowPrivilegeEscalation: false`、`readOnlyRootFilesystem: true`（`/meili_data` 为挂载卷） |
| 更新策略 | `OnDelete`；升级版本前先备份 `search-data` 快照，索引可由重建恢复 |

## 3. worker Deployment

| 要求 | 值 |
| --- | --- |
| 镜像 | 与 core 同仓库、同 digest 的 `ontoaxis-search` 镜像（`cmd/ontoaxis-search`） |
| 命令 | `["consume"]`；`args` 不传 `--tenant` |
| 副本 | `replicas: 1` 起步；可扩到 N，`Consumer` 的 `FOR UPDATE SKIP LOCKED` 保证不重复处理；不要用 `Recreate` 策略随意中断长事务 |
| 端口 | `8081`（`/healthz`、`/readyz`），`9094`（`/metrics`） |
| 环境变量 | `ONTOAXIS_DATABASE_URL`（Secret）、`ONTOAXIS_SEARCH_ENABLED=true`、`ONTOAXIS_SEARCH_URL=http://ontoaxis-search-meilisearch:7700`、`ONTOAXIS_SEARCH_API_KEY`（Secret）、`ONTOAXIS_SEARCH_INDEX_VERSION`、`ONTOAXIS_SEARCH_POLL_INTERVAL`、`ONTOAXIS_SEARCH_CONSUMER_BATCH`、`ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS`、`ONTOAXIS_LOG_LEVEL`、`ONTOAXIS_OTEL_ENDPOINT` |
| livenessProbe | `GET /healthz`（200） |
| readinessProbe | `GET /readyz`（200；内部检查 PostgreSQL ping + Meilisearch health，任一失败返回 503） |
| 资源 | requests `cpu: 100m, memory: 256Mi`；limits `cpu: 500m, memory: 512Mi` |
| PDB | `minAvailable: 1`（单副本时等价于禁止主动驱逐） |
| ServiceMonitor | 若集群有 Prometheus Operator，为 `:9094` 建 ServiceMonitor；指标名前缀 `ontoaxis_search_` |

## 4. Service 与网络

- `Service ontoaxis-search-meilisearch`：ClusterIP，`7700`；selector 只匹配 Meilisearch Pod。
- `Service ontoaxis-search-worker`：ClusterIP，暴露 `8081`（健康）与 `9094`（指标）；不对外。
- NetworkPolicy：入站 Meilisearch 仅允许 `ontoaxis-core` 与 `ontoaxis-search-worker` 的 Pod；worker 入站仅允许监控/探针来源。
- core 的 `ONTOAXIS_SEARCH_*` 环境变量由 platform chart 注入（属 `internal/app` 装配与 platform chart 的范围），本 change 只提供变量清单，见 `integration-patch.md`。

## 5. Secret 与配置

| Secret 键 | 用途 | 来源 |
| --- | --- | --- |
| `search-master-key` | Meilisearch master key + worker/core 的 `ONTOAXIS_SEARCH_API_KEY` | ExternalSecret（集群 Secret Store） |
| `database-url` | worker 的 `ONTOAXIS_DATABASE_URL` | 复用 platform 的 DB 连接 Secret |

要求：

- Secret 不得出现在 values 文件、ConfigMap、日志或 evidence 中；`helm template` 渲染结果里只允许 `secretKeyRef`。
- Meilisearch master key 需为 ≥16 字节随机值；轮换时先滚动 worker/core（读取新值），再以新 key 重启 Meilisearch（见 runbook）。
- 索引版本通过 ConfigMap/values 显式设置，不使用 `latest` 语义。

## 6. 迁移与重建 Job

### 6.1 迁移 Job（ArgoCD PreSync）

- ArgoCD Application 中加 `PreSync` hook Job：`ontoaxis-search migrate`，`backoffLimit: 2`，`activeDeadlineSeconds: 300`。
- 前置条件：platform 的 PostgreSQL 已 Ready（同一 Sync Wave 之前）。
- 幂等：`search.Migrate` 以 `CREATE ... IF NOT EXISTS` 执行，重复运行安全；Down 不在生产使用。
- 若 PostgreSQL 不可达，Job 失败并阻塞 Sync（fail-closed），不要 `HookDeletePolicy` 之外的重试掩盖错误。

### 6.2 重建 Job（手动）

- 不建 CronJob；由运维在索引版本升级、数据漂移、映射变更后手动创建：
  `ontoaxis-search rebuild --tenant <uuid> [--object-type <uuid>] [--verify]`。
- `concurrencyPolicy` 不适用；同一租户同一时刻只允许一个重建（手工纪律，或 Job 名加租户哈希）。
- 资源高于 worker：requests `cpu: 500m, memory: 512Mi`，limits `cpu: 2, memory: 2Gi`；`activeDeadlineSeconds` 按租户规模配置。
- 生产重建**不要**带 `--object-type`（该模式直接改线上索引，只适合单类型修复；整租户重建走 staging/swap/replay）。
- 失败时 Job 非零退出；staging 索引由程序清理，运维按 runbook 核对是否有残留 `_r` 索引。

## 7. ArgoCD Application 要求

- 建议新增 `deploy/argocd/apps/25-search.yaml`（或并入 `20-platform.yaml` 的 chart），`sync-wave: "2"`：
  依赖 `00-namespaces`、`10-secrets`、`20-platform`（PostgreSQL）之后，`40-app` 之前。
- `syncPolicy.automated` 与现有 app 一致：`prune: true`、`selfHeal: true`、`ApplyOutOfSyncOnly`、retry 5 次。
- Meilisearch 的 PVC 不得被 `prune` 删除：PVC 由 StatefulSet `volumeClaimTemplates` 管理，Application 的 prune 不覆盖；
  若改用独立 PVC，加 `argocd.argoproj.io/sync-options: Prune=false`。
- 升级 Meilisearch 镜像时使用 `OnDelete` + 手动删除 Pod，确保先有备份。

## 8. 验收（由 production-delivery 执行并留证）

| 项 | 命令 | 当前状态 |
| --- | --- | --- |
| chart 渲染 | `helm template ontoaxis-search deploy/helm/ontoaxis-search --values deploy/values/kind.yaml` 退出 0 且无明文 Secret | **not executed**（chart 尚未创建，本 change 不改 helm） |
| kind 部署评审 | 在 kind 环境 `kubectl apply -k`/ArgoCD sync 后 Meilisearch、worker Pod Ready，`/readyz` 200 | **not executed** |
| 迁移 hook | `kubectl get jobs -n oa-platform` 中 PreSync Job Completed，`search.*` 表存在 | **not executed** |
| 重建演练 | 手动 Job `rebuild --tenant <uuid> --verify` 退出 0，摘要 `verified=true` | **not executed**（本地 compose 已验证同类命令，见 `evidence/`） |

## 9. 回滚

1. 关闭搜索入口：`ONTOAXIS_SEARCH_ENABLED=false`（core 返回 503，CRUD 不受影响）。
2. 索引版本回退：把 `ONTOAXIS_SEARCH_INDEX_VERSION` 改回旧版本并滚动 core/worker；旧索引在切换前不要删除。
3. worker 回滚：`kubectl rollout undo deployment/ontoaxis-search-worker`；未发布事件仍留在 `jobs.outbox`，恢复后继续消费。
4. Meilisearch 数据损坏：从 PVC 快照恢复；无快照时停 worker、删除索引、全量 `rebuild --verify` 重建（搜索可降级，不阻塞 CRUD）。

## 10. 明确不覆盖

- 本 change 不提供 chart、不改 `deploy/helm/`、`deploy/argocd/`、`deploy/compose/compose.yaml`。
- 不定义生产容量基线；资源值为起始建议，需压测后由 owner 签核。
- 不定义多租户隔离的 Kubernetes 级隔离（租户隔离由索引命名与查询 filter 保证，见 design D1）。