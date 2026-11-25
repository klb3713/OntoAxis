# 交付包：object-search-meilisearch → production-delivery

- 归属：`deploy/compose/`、`deploy/helm/`、`deploy/argocd/` 由 `production-delivery` 合并（见 `docs/stages/stage-2-change-map.md` §共享文件单一所有权，行 119–120）。
- 本 change 只提供片段与要求，不直接改上述目录。
- 日期：2026-11-24
- 汇总清单：[handoff-todo.md](handoff-todo.md) A 节；已在 `openspec/changes/production-delivery/tasks.md` §11 登记（11.1–11.4）。

## 1. Compose `search` profile 片段（对应 task 7.5）

- 片段文件：`deploy/compose/search/compose.search.yaml`（已带 `profiles: ["search"]`；core 的 `depends_on.search-worker` 标 `required: false`）。
- 需要 `production-delivery` 合并到 `deploy/compose/compose.yaml` 的内容：
  - 服务 `meilisearch`（profile `search`，`getmeili/meilisearch:v1.11`，仅回环 `127.0.0.1:17700:7700`，卷 `search_data`）
  - 服务 `search-worker`（profile `search`，`deploy/compose/search/Dockerfile`，依赖 postgres healthy + meilisearch started，仅回环 `127.0.0.1:18088:8081`）
  - `ontoaxis-core` 增加 `ONTOAXIS_SEARCH_ENABLED/URL/API_KEY` 环境与 `depends_on.search-worker{required:false}`
  - 顶层卷 `search_data`
- 合并后验收命令：
  ```bash
  docker compose -f deploy/compose/compose.yaml --profile search config -q   # 期望 exit 0
  docker compose -f deploy/compose/compose.yaml --profile search up -d       # meilisearch 与 search-worker healthy
  ```
- 现状：片段与主文件叠加渲染 `config -q` 已 exit 0；隔离项目内 `up -d` 已至 meilisearch/worker healthy（证据 `evidence/2026-11-24-object-search-verification.md` §4，E2E 73/73）。**主文件合并未做**，故 7.5 不勾。

## 2. Helm/ArgoCD 要求（对应 task 7.6）

- 要求文档：`openspec/changes/object-search-meilisearch/deployment-requirements.md`
  （StatefulSet/Deployment、PVC、Secret、探针、资源、ArgoCD wave、迁移与重建 Job、回滚；验收表均 `not executed`）。
- 待 `production-delivery` 创建 chart 后执行：
  ```bash
  helm template oa-search deploy/helm/<chart> -f <values>   # 期望通过
  # kind 部署评审
  ```
- 现状：**chart 未创建**，`helm template` 与 kind 评审 `not executed`，故 7.6 不勾。

## 3. 交接验收清单

- [ ] 合并 compose 片段，`config -q` 与 `up -d` 健康
- [ ] 创建 search chart，`helm template` 通过
- [ ] kind 部署评审通过
- [ ] 回填 7.5/7.6 勾选与证据链接

## 4. 附带发现：CI `edge` job 的 `ontoaxis-core` 启动失败（与搜索无关）

- 现象：`ci` 工作流 `edge` job 在 `docker compose --profile edge up -d` 时 core 容器 `exited (1)`，导致依赖服务一并失败。
- 根因：`deploy/compose/compose.yaml` 的 base 文件不含授权依赖，edge job 原命令未叠加 `deploy/compose/authz/compose.authz.yaml`，core 因 fail-closed 校验直接退出：
  `{"level":"ERROR","msg":"startup failed","error":"load authorization config: ONTOAXIS_AUTHZ_OPENFGA_API_URL is required"}`。
- 处理（2026-11-24，已在本分支 `ci.yml` 修正，未改 `deploy/compose/`）：`edge` job 的 up/logs/down 命令统一叠加 authz overlay，并显式启动 `openfga`、`authz-init`；`docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml --profile edge config -q` 已 exit 0。
- 请 `production-delivery` 复核该 CI 变更是否与其 edge 交付口径一致（`deploy/compose/` 归属方）；若后续改用其他 authz 注入方式，请同步本 job。
