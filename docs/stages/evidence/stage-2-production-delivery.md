# Stage 2 · production-delivery 证据台账

## 2026-11-24 复核（当前结论）

**S2-AC-08 未通过，S2-AC-10 未执行，S2-AC-11 部分完成。** 本轮只使用本地合成环境、隔离夹具和只读集群检查；未连接 staging、修改公网 Git 仓库、推送项目或触发远端 CI。下方 11 月 23 日记录是安装受阻时的历史快照，不代表当前 kind 状态。G1 已按负责人确认登记，见 [G1 门禁记录](stage-2-g1-gate-record.md)。

| 实测项目 | 命令/结果 | 证据及边界 |
|---|---|---|
| 本地门禁 | `make lint`、`make test`、`openspec validate production-delivery --strict`、`openspec validate --all --strict`、配置漂移、边缘生成、ShellCheck、actionlint、密钥扫描，退出 0 | 本机日志 `dist/gates/production-delivery-reviewed.6W4BYQkM/`；不是 GitHub Actions run |
| 严格渲染 | `bash scripts/delivery-render-check.sh --require-schema` 退出 0；5 chart × 4 环境渲染可重复；kind 与 kind-small 各 6 个唯一 ESO 目标，渲染的 `secretKeyRef` 均有映射；kubeconform 265 valid、14 无 schema 跳过、0 invalid/errors | 本机 `dist/gates/production-delivery-reviewed.6W4BYQkM/render.log`；新 ESO 目标未在运行集群同步 |
| 晋升和回滚 | `bash scripts/delivery-release-flow-test.sh` 47/47 通过（晋升 18、回滚 29） | 本机 `dist/gates/release-flow.zwS1PNyo/offline.log`；临时 Git 仓库和模拟集群，证明失败拒绝、成功提交范围与标记时序，不证明实际 ArgoCD 回滚 |
| 固定 GitOps 根树 | `bash deploy/kind/render-gitops-root-test.sh` 退出 0：生成八个应用，拒绝覆盖和浮动修订，固定双 SHA 与 `bootstrap.sh --check-source` 一致 | 合成 Git 对象；没有启动 Git 服务、提交项目或连接集群 |
| 独立 kind 首次部署尝试 | 2026-11-24 13:08–13:11（北京时间），用本机缓存 `registry:2`、独立 Docker bridge 和 `127.0.0.1:5500` 启动 `ontoaxis-registry-rehearsal`。`docker run --pull=never` 卡在 `created`（PID 0、未监听）；约两分半后取消客户端，随后删除本轮创建的空容器和空网络 | 未进入 `kind create cluster`，无演练 kubeconfig，未运行 ArgoCD/ESO 或波次、未获取外网镜像。原 `ontoaxis-kind` 三节点复核均为 `Ready`。这是环境启动阻塞，不是 GitOps 验收失败用例 |
| 启动脚本回归 | `bash deploy/kind/kind-up-test.sh` 6/6：已卡死 registry、启动超时、非回环绑定、registry API 不可用均在建集群前失败；新建与复用只写指定 kubeconfig | 本地模拟测试；另只读复核现有 registry 的回环绑定与 `/v2/`。不是独立集群启动证据 |
| 入口诊断与全量烟测 | 本地 kind 的 `--profile edge` 6/6，退出 0；`full` 6/12，退出 1 | 本机 `dist/delivery/production-delivery-final.Hp1q2VUe/{edge,full}/smoke-kind.json`；完整模式检查出 9 个缺失 Application、12 个工作负载缺发布 pin，以及授权 API、IdP、备份/回滚标记输入不足 |
| 数据恢复 | 隔离 Postgres 的 L1–L4 通过；截断、同长损坏均在连接 Kubernetes 前退出 1 | [kind 运行记录](stage-2-production-delivery-kind.md) §15；普通 core 启动会执行迁移，不可充当 L5 只读验证 |
| IdP | 脚本离线回归验证独立正对照、网络故障不误报、Bearer 不随重定向；未获取可据以验收的外部 IdP 样本 | 本地模拟 HTTP 不是外部 IdP；历史 HTTP 000 不算拒绝 |

**待闭环：** kind 当前无受管 Application。需要受控 Git 源、固定镜像与合成身份、应用健康等待及真实 wave 证据，才能继续漂移自愈、两种 GitOps 回滚、kind/k3d 和完整烟测。另外仍缺 L5 授权只读链、密钥轮换、外部 IdP 验证、实际 CI run 与负责人签核。定时 kind 工作流默认不启用；受控运行器准备适配器不存在时，手动执行会在建集群前失败，不产生验收成功证据。

---

## 历史快照：2026-11-23 静态门禁

> 环境：本地 macOS（darwin/x86_64）+ Docker Desktop 29.2.1；合成数据；未连接真实云账号、生产密钥或客户数据。
> 时间：2026-11-23 22:30–22:50（Asia/Shanghai）。本文件由 `production-delivery` change 维护，供 `stage-2-integration` 汇总 Stage 2 证据。
> 结论先行：静态资产与门禁全部通过；kind/k3d 集群安装在本机被 Docker Desktop 阻塞，kind 相关任务标 `not executed`，因此 **S2-AC-08 不能标 pass**，S2-AC-10 无证据。

## 1. 工具版本

| 工具 | 版本 | 来源 |
|---|---|---|
| helm | v3.16.4+g7877b45 | 官方包（华为云镜像下载） |
| kubectl | v1.34.1 | 系统已有 |
| kind | v0.29.0 | GitHub release |
| kubeconform | v0.7.0 | GitHub release |
| shellcheck | 0.10.0 | GitHub release |
| actionlint | 1.7.7 | GitHub release |
| gitleaks | 8.21.2 | GitHub release |
| kindest/node | v1.32.0（本地镜像） | Docker Hub |

## 2. 静态门禁结果（全部通过）

命令均在本仓库根目录执行，原始输出见同名日志。`exit-codes.txt` 汇总退出码。

| 检查 | 命令 | 退出码 | 日志 |
|---|---|---|---|
| Helm lint | `for c in deploy/helm/*/; do helm lint "$c"; done` | 0 | `stage-2-production-delivery/helm-lint.log` |
| 渲染确定性 + kubeconform | `bash scripts/delivery-render-check.sh` | 0（RESULT: PASS，20 文件） | `stage-2-production-delivery/render-check.log` |
| ShellCheck | `shellcheck scripts/delivery-*.sh deploy/kind/*.sh` | 0 | `stage-2-production-delivery/shellcheck.log` |
| actionlint | `actionlint` | 0 | `stage-2-production-delivery/actionlint.log` |
| 密钥扫描 | `bash scripts/delivery-secret-scan.sh`（gitleaks） | 0（no leaks found，34 commits） | `stage-2-production-delivery/secret-scan.log` |
| 配置漂移 | `make delivery-config-check` | 0（OK） | `stage-2-production-delivery/config-check.log` |
| OpenSpec 严格校验 | `openspec validate --all --strict` | 0（19 passed） | `stage-2-production-delivery/openspec-validate.log` |
| release manifest 负向 | `bash scripts/delivery-release-check.sh --env kind` | 1（占位 digest 按设计失败） | `stage-2-production-delivery/release-check-negative.log` |
| 备份 CronJob 渲染 | `helm template oa-jobs ...` | 0（`0 2 * * *`、Forbid、backoffLimit 0、retention 7） | `stage-2-production-delivery/backup-cronjob.txt` |

补充负向验证（无集群）：

- `bash scripts/delivery-promote.sh` 无参 → `--to <target-env> is required`，exit 1。
- `bash scripts/delivery-restore.sh` 无参 → `manifest not found`，exit 1。
- `bash scripts/delivery-release-check.sh --env kind` → 报告 `release check FAILED for env='kind'`，exit 1（占位 digest 被拒）。

## 3. kind / k3d 集群：未执行（环境阻塞）

执行 `bash deploy/kind/kind-up.sh`（1 control-plane + 2 workers，本地 registry）：

1. registry 容器 `ontoaxis-registry` 启动成功（`registry:2`，127.0.0.1:5000）。
2. kind 节点容器创建后长期停留在 `created`；`docker inspect` 的 `StartedAt` 为零值。
3. 手动 `docker start ontoaxis-kind-control-plane` 45 秒仍未启动（无报错输出）。
4. 同期 `docker run --rm alpine:3.20 true` 正常退出，说明 Docker daemon 可用，但无法启动 kind 节点容器。
5. 已清理 `ontoaxis-kind-*` 容器与集群元数据，registry 容器保留。

结论：本机 Docker Desktop 无法启动 kind 节点容器，`deploy/kind/bootstrap.sh`、`verify.sh`、ArgoCD/ESO/Envoy Gateway 安装、helm 部署、探针/PDB/备份恢复/回滚/IdP 验证均未执行。k3d 走同一 Docker 路径，未尝试。

`deploy/kind/kind-up.sh` 本次增加了 `KIND_NODE_IMAGE` 覆盖（用于复用本地节点镜像，避免重复拉取）；默认行为不变。

## 4. 本地静态修正（本轮）

- `.github/workflows/ci.yml`：新增 `delivery-static`（helm lint、render-check、kubeconform、shellcheck、actionlint、gitleaks、edge-generate-check、delivery-config-check）与 `delivery-kind`（workflow_dispatch/schedule）；shellcheck 范围修正为实际存在的 `scripts/delivery-*.sh` 与 `deploy/kind/*.sh`。
- `scripts/delivery-restore.sh`、`scripts/delivery-rollback.sh`、`scripts/delivery-secret-scan.sh`：修复 shellcheck 报告的 SC2015/SC2034/SC2012/SC2086。
- `.gitleaks.toml`：允许 `scripts/delivery-secrets-bootstrap.sh`（仅 `secret-key:ENV_VAR` 键名映射，无凭据值）；gitleaks 历史扫描因此归零。
- `deploy/kind/kind-up.sh`：新增可选 `KIND_NODE_IMAGE`。

## 5. Stage AC 结论

| AC | 结论 | 依据 |
|---|---|---|
| S2-AC-08（Helm+ArgoCD kind/k3d、probes/PDB/Secret、备份恢复、回滚） | **not passed** | 静态资产与门禁通过；kind 未安装，部署/探针/PDB/备份恢复/回滚均未执行 |
| S2-AC-10（非本地 Keycloak OIDC/JWKS/claims、不提交生产密钥） | **not executed** | 无 staging IdP；本地 Compose Keycloak 不构成证据；仅静态渲染与密钥扫描通过 |
| S2-AC-11（协作：证据与报告） | partial | 本文件与 CI 定义已就绪；缺 kind 运行证据与 CI run 链接 |

## 6. 未执行清单（not executed）

3.1–3.4（探针/资源/优雅终止/PDB 的 kind 注入验证）、4.6（轮换）、5.2–5.9（ArgoCD 同步/wave/漂移/晋升/回滚演练；5.6/5.7 仅有负向本地验证）、6.3–6.7（bootstrap/verify/k3d/诊断/port-forward）、7.1/7.2/7.4/7.6（备份与恢复实测、RPO/RTO、runbook 演练）、8.3–8.7（IdP 实测/JWKS 轮换/fallback/安全评审）、9.1/9.5/9.7（烟测全绿/CI run/完整证据链）、10.1 的故障演练、10.4 全量门禁中的集群项、10.5 负责人评审。

## 7. G1 前置门禁

Wave 1 三个 change（`authorization-openfga-cedar`、`pipeline-kestra-dbt`、`connector-runtime`）均已归档并有 E2E 证据，G1 的事实条件已满足；但 `docs/stages/evidence/` 尚无 G1 `pass` 记录，`production-delivery/tasks.md` 1.1 未勾选。**G1 是否登记 pass 需负责人确认，不得自动批准。**

## 8. 复现命令

```bash
export PATH=/tmp/oa-tools/bin:$PATH   # 本地 shellcheck/actionlint/gitleaks/kubeconform
for c in deploy/helm/*/; do helm lint "$c"; done
bash scripts/delivery-render-check.sh
shellcheck scripts/delivery-*.sh deploy/kind/*.sh
actionlint
bash scripts/delivery-secret-scan.sh
make delivery-config-check
openspec validate --all --strict
bash scripts/delivery-release-check.sh --env kind   # 期望 exit 1（占位 digest）
```
## 2026-11-24 补充验证（4.6 / 8.4 / 8.5）

本节追加于历史快照之后，用于记录当日后续在本地 kind/Compose 合成环境的实测；仍不构成 staging/生产验收。

### 环境修复（为使 kind 可观测）

- `oa-system/openfga` 启动报 `FATAL: database "openfga" does not exist (SQLSTATE 3D000)`：已 `CREATE DATABASE openfga`，复制 `openfga-migrate` Job 为 `openfga-migrate-manual` 执行迁移（`goose_db_version` 到 6）。
- chart 缺陷：openfga readiness/liveness 使用 `/readyz` 返回 404，已 patch 为 `/healthz`（`deploy/helm/ontoaxis-platform/values.yaml` 需后续同步修正）。
- core 启动 `load authorization config: ONTOAXIS_AUTHZ_OPENFGA_STORE_ID is required` / `authorization startup check: context deadline exceeded`：在 pod port-forward `18099`（`18082` 被 kind nginx web 占用）创建 store `01M863G4226ZA836RDQ0QBR3M6`、model `01M863G43BY21CCX08WNBMS857`（`policies/openfga/model.json`），`kubectl set env` 后 core 1/1。
- kind Keycloak 仅有 `master` realm：用 `deploy/compose/keycloak/realm-ontoaxis.json` POST `/admin/realms` 导入 `ontoaxis`（201）；客户端仅 `ontoaxis-web` 为 public，无 confidential client/secret。

### 4.6 轮换路径（通过，OIDC 部分 N/A）

- `ALTER ROLE ontoaxis PASSWORD 'pg-local-pass-r1'`；更新 `external-secrets/ontoaxis-bootstrap` 的 `database-url`、`openfga-datastore-uri`、`postgres-password`，annotate 5 个 ExternalSecret `force-sync`，均 `SecretSynced`；重启 core/openfga 后 1/1 Running。
- 口令校验走 Pod IP（`pg_hba` 对 127.0.0.1 为 trust，不能作证据）：旧口令 `FATAL: password authentication failed for user "ontoaxis"`，新口令 `select 1` 成功。
- 轮换前后镜像 digest：core `sha256:7e849f3aa600c707590f202d27f74d7cb41a450239deb7f02e601098596c3e66`、web `sha256:772bcaa627bbc3a875c775e28db819b94a1f4be98b669d352a61198069048c0e`，均未变（无需重建镜像）。
- 烟测：`--profile edge` 6/6 passed（rc=0）；readiness 的 `probes-pdb-secret`、`backup-marker` passed。`full` 仍 failed（缺 12 个 render-only 工作负载 pin、缺 rollback marker、IdP issuer 主机名不一致），不冒充全绿。
- OIDC client secret：realm 无 confidential client/secret，`OIDC_CLIENT_SECRET` 在 resource-server 校验路径未使用，记为不适用（N/A），未伪造轮换。
- 证据目录：`dist/delivery/rotation-4.6/`。

### 8.4 负向矩阵（通过）

本地 Compose Keycloak `http://localhost:18083/realms/ontoaxis`，脚本 `dist/delivery/idp-negative/run.sh` 为每项构造满足前置的 client/user：audience、missing_claims、expired、bad_signature、bad_kid、issuer 六项均 `PASS candidate ... rejected (HTTP 401)`，每项正向 control 200；日志与 token（不打印原文）见 `dist/delivery/idp-negative/`。

### 8.5 JWKS 轮换（通过）

`POST /admin/realms/ontoaxis/components` 新增 `rsa-generated`（priority 200）：kid `TRKrSk8LEH1MvVn0B0TzdRoXQhbnePv4sNKerwXOMeg` → `bjQvwcPLqZQkzUPHCt7KVGDC1GCiO9Wvni8hjtbah1c`，JWKS 同时含新旧 kid；旧 token 与新 token 访问只读 API 均 200，core 未重启。证据 `dist/delivery/idp-rotation/`。

## 2026-11-24 环境恢复与数据持久化（kind）

### 根因
- Docker Desktop 重启后 `oa-data/postgres` 的 Deployment 未声明任何 volume，数据目录在容器可写层；pod 重建（`Last State: Terminated, Exit Code: 255`）导致 `ontoaxis` schema 之外的 `openfga` 数据库与部分对象丢失，表现为 `FATAL: database "openfga" does not exist`、`relation "goose_db_version" does not exist`。
- 这是 chart 缺陷 D-PD-12：`deploy/helm/ontoaxis-platform` 的 postgres 组件没有 PVC 支持，kind 中数据不可持久、无法重复演示备份/恢复与 GitOps。

### 修复（代码）
- `deploy/helm/ontoaxis-platform/templates/deployment.yaml` 增加通用 `persistence` 渲染（`volumeMounts` 与 `volumes` 合并 configMap 与 PVC）。
- 新增 `deploy/helm/ontoaxis-platform/templates/persistence.yaml`：按组件渲染 PVC（默认名 `<component>-data`、`ReadWriteOnce`、可选 `storageClass`）。
- `deploy/helm/ontoaxis-platform/values.yaml` 的 `components.postgres` 增加：
  `persistence: {enabled: true, size: 5Gi, storageClass: standard, mountPath: /var/lib/postgresql/data}`。
- 验证：`helm lint deploy/helm/ontoaxis-platform` 通过；`helm template` 渲染出 `oa-data/postgres-data` PVC；`make delivery-lint`、`scripts/delivery-render-check.sh`（RESULT: PASS）、`scripts/delivery-config-check.sh`、`openspec validate --all --strict`（19 passed）均通过。

### 集群恢复步骤（合成数据，可重放）
1. 创建 `oa-data/postgres-data` PVC（5Gi，standard），给 postgres Deployment 挂载 `/var/lib/postgresql/data`，rollout 后 `CREATE DATABASE openfga`。
2. 用 `openfga/openfga:v1.23.0` 以 `postgres://ontoaxis:<r1>@postgres.oa-data.svc.cluster.local:5432/openfga?sslmode=disable` 执行 `migrate`（Job `openfga-migrate-2`，`migration done`）。
3. port-forward `18099` 创建 store `01M867XG777JWS77HTAWQPHR1C`、model `01M867XKJNDBKTXT2ANYYYKCXH`，`kubectl set env deploy/ontoaxis-core` 两个 ID。
4. core 重新迁移 `goose: successfully migrated database to version: 1`，日志 `authz ... result:"loaded"`，`/readyz` 返回 `{"checks":{...,"database":"ok","authorization":"ok"},"status":"ok"}`；oa-app core/web/meilisearch/search 均 1/1 Running。
5. 验证 PVC 生效：`kubectl drain ontoaxis-kind-worker` 驱逐/重排后 `postgres` 重新 Running，`pg_database` 仍含 `ontoaxis` 与 `openfga`，core `/readyz` 仍 200。

### 3.4 PDB 阻塞补证
- `kubectl drain ontoaxis-kind-worker --ignore-daemonsets --delete-emptydir-data --timeout=30s`（日志 `dist/delivery/pdb-drain/drain.log`）：`error when evicting pods/"ontoaxis-search-..." -n "oa-app": global timeout reached: 30s`，命令以「pending nodes to be drained」结束；`ontoaxis-core-pdb`/`ontoaxis-search-pdb` 的 `ALLOWED DISRUPTIONS=0`，证明低于 `minAvailable` 的驱逐被 PDB 拒绝。
- 说明：core 为 `strategy: Recreate` 且单副本，滚动窗口可用副本无法维持，3.4 的「滚动更新期间可用副本不低于 PDB」仍未满足，任务保持 partial。

## 17. 2026-11-24 GitOps 5.2/5.3/5.5 演练（独立集群 ontoaxis-kind-rehearsal）

### 环境与方法
- 集群：`kind-ontoaxis-rehearsal`（context `kind-ontoaxis-kind-rehearsal`，kubeconfig `dist/kind/rehearsal.kubeconfig`），3 节点 Ready；与既有 `ontoaxis-kind` 完全隔离，未触碰原集群。
- 组件：ArgoCD v2.13.3、External Secrets Operator v0.14.4、Envoy Gateway v1.2.6（镜像均经本地 registry `localhost:5500` 注入，节点无外网出口）。
- GitOps 源：本地 checkout `/tmp/gitops/checkout`（工作区副本，排除 `.git/dist/node_modules`），根提交 `be4f89cf664c0b80ce7f5bec1ec856b9098a5407`（8 个标准子 Application），内容提交 `724721bccdf683b0666ef5640b561efe8b5bbd37`。
- 只读 Git 服务：`argocd/ontoaxis-git`（Deployment + ClusterIP Service 8080），镜像 `localhost:5500/library/ontoaxis-git-http:v12`（alpine:3.20 + git/git-daemon/python3 调 `git-http-backend`），裸仓库由 checkout `clone --bare` 生成；集群内 `git clone http://ontoaxis-git.argocd.svc.cluster.local:8080/ontoaxis.git` 成功。
- 应用方式：`deploy/kind/bootstrap.sh --skip-argocd --skip-eso`（复用已装控制面），`ROOT_APP_ENV=kind-small`，导出合成 `ONTOAXIS_*` 密钥；AppProject + root Application 应用后由 ArgoCD 自动铺开。

### 结果（2026-11-24T10:48:48Z，`dist/delivery/gitops-5/apps-final.txt`）
| Application | Sync | Health | sync-wave | syncPolicy |
|---|---|---|---|---|
| ontoaxis-namespaces | Synced | Healthy | -10 | automated{prune,selfHeal} |
| ontoaxis-secrets | Synced | Healthy | -5 | automated{prune,selfHeal} |
| ontoaxis-platform | Synced | Healthy | 0 | automated{prune,selfHeal} |
| ontoaxis-data | Synced | Healthy | 1 | automated{prune,selfHeal} |
| ontoaxis-app | Synced | Healthy | 2 | automated{prune,selfHeal} |
| ontoaxis-edge | Synced | Healthy | 3 | automated{prune,selfHeal} |
| ontoaxis-observability | Synced | Healthy | 4 | automated{prune,selfHeal} |
| ontoaxis-jobs | Synced | Healthy | 5 | automated{prune,selfHeal} |
| ontoaxis-root | Synced | Healthy | — | automated{prune,selfHeal} |

- **5.2 通过**：app-of-apps（root + 8 子应用）全部 `Synced/Healthy`；wave 注解与 bootstrap 预检（`deploy/kind/bootstrap.sh:75-110`：8 个标准子应用、身份/命名空间/targetRevision/valueFiles 精确匹配）一致。
- **5.3 通过**：syncPolicy `automated{prune:true,selfHeal:true}`，retry `limit:5`、`backoff{5s,factor:2,maxDuration:3m}`；演练期间每次 content 提交后 ArgoCD 自动同步到新 revision，无需手工 `argocd app sync`。
- **5.5 通过（有限定说明）**：`kubectl -n oa-app scale deploy web --replicas=3` 后 45s 内 self-heal 回 `replicas=1`，日志 `dist/delivery/gitops-5/drift.log`。同轮对 `oa-data/ontoaxis-backup-script` ConfigMap 人为加 `drift-marker` 未被自动清除（Application 仍显示 Synced）——疑 ArgoCD 对 Helm 渲染 ConfigMap 的字段级比较行为差异，未查明；本轮自愈结论以 Deployment 副本漂移为证据，限制已记录。
- **5.4 未勾（partial）**：wave 顺序表正确，但空集群首次铺开时未保存「platform 未就绪 → data/app Pending 等待」的逐事件证据，按要求不勾。

### 演练中发现的产品缺陷（待修，不在本 change 实现内）
- **D-PD-13**：`deploy/helm/ontoaxis-jobs` 的备份 Pod 无 `securityContext`，在 `oa-data`（`pod-security.kubernetes.io/enforce=restricted`）下被 Job 控制器拒绝创建 Pod：`would violate PodSecurity "restricted:latest": allowPrivilegeEscalation != false ... runAsNonRoot != true ... seccompProfile ...`。需为 backup 容器补 `runAsNonRoot/runAsUser/allowPrivilegeEscalation=false/capabilities.drop=[ALL]/seccompProfile=RuntimeDefault`。
- **D-PD-14**：`oa-data/ontoaxis-backups` PVC 为 `WaitForFirstConsumer`，无消费者时长期 Pending，使 Application 的 PVC 资源 Health=Progressing；本轮用一次性合规 `pvc-binder` Pod（alpine:3.20，restricted 合规 securityContext）挂载后 Bound（`pvc-f7e7e041-7580-4098-96a1-96014a697d30`，10Gi）。应在 CronJob 首次运行前提供绑定策略或改用可延迟绑定的设计说明。
- **D-PD-15**：ArgoCD `dex-server` CrashLoop（不影响 server/repo/application-controller），因演练未使用 SSO 登录；需在交付文档注明生产应启用或移除 dex。

### 复现要点（供复核）
```bash
export KUBECONFIG=$PWD/dist/kind/rehearsal.kubeconfig
kubectl --context kind-ontoaxis-kind-rehearsal -n argocd get applications
kubectl --context kind-ontoaxis-kind-rehearsal -n oa-app get deploy web -o jsonpath='{.spec.replicas}'
```

## 18. 2026-11-24 GitOps 5.9 两次坏部署回滚演练（独立集群 ontoaxis-kind-rehearsal）

前提：演练集群由 ArgoCD v2.13.3 管理（见 §17），所有变更经 content 提交 → root 提交 → ArgoCD 自动同步。

### 演练 A：镜像 digest 缺失（ImagePullBackOff）
- 注入：content 提交 `ac70d547…` 将 `components.web.image.digest` 改为 `sha256:0000…`，ArgoCD 自动同步。
- 现象：新 ReplicaSet `web-748ffc66f` 的 Pod `ErrImagePull` → `ImagePullBackOff`，`failed to resolve reference "localhost:5500/library/ontoaxis-web@sha256:0000…"`；旧 Pod 继续 1/1 服务；`ontoaxis-app` 状态 `Synced/Progressing`（旧副本健康故非 Degraded）。
- 证据：`dist/delivery/drill-5.9a/{events.txt,pods.txt,app-status.txt,app-history.json}`。
- 回滚：提交 `delivery: rollback kind`（`331c7c7c…`），20s 内 `web` digest 恢复 `sha256:772bcaa627…`，`ontoaxis-app` `Synced/Healthy`。

### 演练 B：readiness path 覆盖
- 注入：`components.ontoaxis-core.probes.readyPath=/not-readyz`（content `64aaf847…`）。
- 现象：`Readiness probe failed: HTTP probe failed with statuscode: 404`（`kubelet`，failure 阈值 3），Pod `0/1 Running`；Deployment `Available False / MinimumReplicasUnavailable`；`ontoaxis-app` `Synced/Progressing`。
- 证据：`dist/delivery/drill-5.9b/{conditions.txt,pods.txt,probe-events.txt,app-history.json}`。
- 回滚：提交 `delivery: rollback kind`（`c5653d19…`），T0=`11:48:40Z` → `11:49:13Z`（约 33s）`ontoaxis-core` 1/1、Application Healthy。

### 限制与结论
- readiness 烟测（`scripts/delivery-smoke.sh --profile readiness`）整体 `failed`：`edge-port-forward`/`core-healthz`/`core-readyz`/`web-root`/`search-path`/`gitops`/`probes-pdb-secret` 通过，`read-only-api`、`running-images`（render-only 工作负载未 pin）、`idp-discovery`（演练集群 Keycloak 未导入 realm）失败。故 5.9 的回滚动作与健康恢复成立，但「回滚后烟测全绿」不成立，AC-08 维持 partial。
- 演练命名空间 PSA 临时从 `restricted` 降为 `baseline`，原因见 **D-PD-16**：`deploy/helm/ontoaxis-app` 的 web/search 容器无 `securityContext`，在 restricted 下 `FailedCreate`（`allowPrivilegeEscalation != false …`），导致任何新 ReplicaSet 无法创建 Pod；生产 chart 必须补 `runAsNonRoot/allowPrivilegeEscalation=false/capabilities.drop=[ALL]/seccompProfile`。
- 回滚提交未推送、未合并；演练仓库 `/tmp/gitops` 与集群均为本地临时环境。

## 19. 2026-11-24 修复 D-PD-13 / D-PD-16（restricted PodSecurity 合规）

### D-PD-13 备份 Job 缺 securityContext（已修复并实测）
- 代码：`deploy/helm/ontoaxis-jobs/templates/_backup.tpl` 改为从 values 渲染 Pod/容器安全上下文；`deploy/helm/ontoaxis-jobs/values.yaml` 的 `backup` 增加：
  - `podSecurityContext: {runAsNonRoot: true, runAsUser: 999, runAsGroup: 999, fsGroup: 999, seccompProfile: {type: RuntimeDefault}}`
  - `containerSecurityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}`
- 实测（演练集群 `oa-data`，`pod-security.kubernetes.io/enforce=restricted`）：`helm template ... --set backup.name=ontoaxis-backup-test` + `kubectl create job --from=cronjob/...`，Pod 被准入创建并 `Completed`，日志：
  `backup complete: /backups/20261124T120059Z sha256=ac7663f3175703da110edf1ed4603b557ebb1d2f76de32daad35582f32b544d2 size=125036 schema_version=1`
- 结论：D-PD-13 关闭；证据日志保留在命令输出与集群历史，临时对象 `ontoaxis-backup-test`/`backup-test-run`/`ontoaxis-backup-test-script` 已删除。

### D-PD-16 app chart 缺 securityContext（核心/搜索已修复并实测；web 残留）
- 代码：`deploy/helm/ontoaxis-app/templates/deployment.yaml` 新增按组件渲染 `podSecurityContext` 与容器 `securityContext`；`deploy/helm/ontoaxis-app/values.yaml` 的 `ontoaxis-core`（`runAsUser/Group: 100`）与 `ontoaxis-search`（`runAsUser/Group: 1000`）配置 restricted 合规字段（`runAsNonRoot: true`、`allowPrivilegeEscalation: false`、`capabilities.drop: [ALL]`、`seccompProfile: RuntimeDefault`）。
- 实测（namespace 标签临时 `enforce=restricted`）：分别删除 core/search Pod 由 Deployment 重建，均 `Running ready=true`（core Pod `ontoaxis-core-5676469d5d-svts9`、search Pod `ontoaxis-search-599ffff58b-r5lvz`），无 PodSecurity 拒绝。
- **D-PD-16b（残留，未修）**：`deploy/helm/ontoaxis-app` 的 `web` 组件仍使用 `nginx:1.27-alpine`（master 以 root 运行、监听 80），无法通过 restricted；需要改用 `nginxinc/nginx-unprivileged` 类镜像并同步把 `ports.containerPort` 改为 8080（Service 端口可保持 80），属于镜像/发布变更，需重新构建 `ontoaxis-web` 与更新 release manifest 后再开启 web 的 securityContext。在此之前 web 在 restricted 命名空间的新 ReplicaSet 会 `FailedCreate`（演练中已复现）。
- 演练命名空间恢复为 chart 期望的 `baseline`（`/tmp/gitops` 临时值），仓库 namespaces 清单仍为 `restricted`（`deploy/argocd/manifests/namespaces/namespaces.yaml`）。

### 门禁（修复后）
- `helm lint` 5 charts → 0 failed；`scripts/delivery-config-check.sh` → OK；`scripts/delivery-render-check.sh` → `RESULT: PASS`（需 PyYAML）；`openspec validate --all --strict` → 19 passed 0 failed。
- 未提交（AGENTS 规则）。

## 20. 2026-11-24 D-PD-16b web 非特权迁移（已修复并实测）

### 代码
- `web/nginx.conf`：`listen 80` → `listen 8080`（`/api/` 代理与 SPA fallback 不变）。
- `web/Dockerfile` 运行阶段：`nginx:1.27-alpine` → `nginxinc/nginx-unprivileged:1.27-alpine`（UID 101、内置 writable pid/temp 路径），`EXPOSE 8080`，HEALTHCHECK 改为 `http://127.0.0.1:8080/`。
- `deploy/helm/ontoaxis-app/values.yaml` 的 `web`：`ports.containerPort: 8080`（Service 仍 80 → 命名端口 `http`），新增
  `podSecurityContext{runAsNonRoot:true,runAsUser:101,runAsGroup:101,seccompProfile:RuntimeDefault}` 与 `securityContext{allowPrivilegeEscalation:false,capabilities.drop:[ALL]}`。
- `deploy/releases/kind.yaml` 的 `ontoaxis-web` 更新为新构建镜像 digest `sha256:b0ff694e5d1164650b63b5885b067ad3022461dacd37971df6a01471ff66467e`（commit `506a0cdb46146a2f12dd8a3ab7ca454c7819aae4`）。

### 验证（先演练集群、再原 kind 集群）
- 本地构建运行镜像（同等配置：nginx 1.27-alpine + `USER 101` + `pid /tmp/nginx.pid` + nginx.conf 8080），推送 `localhost:5000/ontoaxis-web`、`localhost:5500/library/ontoaxis-web`，manifest digest `sha256:b0ff694e…6467e`。
- 演练集群 `ontoaxis-kind-rehearsal`：namespace 标签临时 `enforce=restricted`，删除 web Pod 由 Deployment 重建 → `web-dc84d9948-wqdzm` `Running ready=true`；Pod 安全上下文实测 `{"runAsGroup":101,"runAsNonRoot":true,"runAsUser":101,"seccompProfile":{"type":"RuntimeDefault"}}`、容器 `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}`；`wget http://127.0.0.1:8080/` 返回 `<!doctype html>…<html lang="zh-CN">`。
- 原集群 `ontoaxis-kind`：`kubectl set image deploy/web web=localhost:5000/ontoaxis-web@sha256:b0ff…` + 容器端口改 8080，`rollout status` 成功，`web-8566f5fcdf-z6vp6 1/1 Running`。
- `bash scripts/delivery-release-check.sh --env kind` → `release check passed for env='kind'`，7 个组件 `PASS running digest match`（含 ontoaxis-web）。
- 门禁：`helm lint` 0 failed；`delivery-render-check` `RESULT: PASS`；`delivery-config-check` OK；`openspec validate --all --strict` 19/19。

### 说明与残留
- 仓库 Dockerfile 采用 `nginxinc/nginx-unprivileged:1.27-alpine`；本次本地验证镜像用 `nginx:1.27-alpine` + 显式 `USER 101` 等效实现（无法从外网拉 unprivileged 基础镜像），两者运行行为一致（8080 端口、非 root、writable temp/pid）。CI 构建时会使用 unprivileged 基础镜像，需在 `make delivery-images` 后把各环境 release manifest 的 web digest 换成正式构建产物。
- 至此 D-PD-13、D-PD-16（含 16b）关闭；restricted 命名空间下 app 三组件（core/search/web）与备份 Job 均可运行。
- 未提交（AGENTS 规则）。

## 21. 2026-11-24 剩余任务二次评估（5.4/5.8/6.x/8.x/9.x/10.5）

- **5.4 partial**：演练集群 9 个 Application 全部 Synced/Healthy，wave 注解 -10/-5/0/1/2/3/4/5 正确；但空集群逐事件「platform 未就绪时 data/app 等待」证据仍缺——演练节点无法从离线 registry 重建空集群（kind-loaded 镜像与 registry digest 不一致），无法诚实勾选。
- **5.8 partial**：隔离回归 29/29；真实 `--sync` 路径要求 `deploy/kind/verify.sh --wait-only` + readiness 烟测通过，当前 readiness 因以下三项失败被阻断：`read-only-api`（无有效 token）、`running-images`（kind 加载镜像 imageID 为配置摘要，与 manifest digest 不同）、`idp-discovery`（演练 issuer 端口）。脚本本身未被绕过。
- **6.3 partial**：`bootstrap.sh --skip-argocd --skip-eso --skip-envoy` 在已备集群成功应用 AppProject/root；完整空集群安装依赖 `raw.githubusercontent.com` 与 `charts.external-secrets.io`，演练节点无外网，未执行完整模式。
- **6.4 partial**：坏镜像场景 `VERIFY_TIMEOUT=20 bash deploy/kind/verify.sh` → rc=1 且自动诊断（6.6 证据）；正常环境 exit 0 依赖 readiness/full 烟测全绿，受上述三项阻断。
- **6.5 not executed**：环境无 `k3d` 二进制；`deploy/kind/k3d-*.sh` 仅静态检查。
- **8.6 partial**：演练集群 Keycloak 在 `keycloak.oa-system.svc.cluster.local:8080` 可达，realm `ontoaxis` 导入 HTTP 201，port-forward issuer 可签发并解码 `tenant_id/org_ids/roles/aud`；未搭建非 localhost TLS fallback 与证书，未在 fallback 上跑负向矩阵。
- **8.7 草稿**：`docs/stages/evidence/stage-2-idp-security-review.md` 已提交评审清单（11 项），结论待负责人签核；未签核前 AC-10 不得 pass。
- **9.1 partial**：edge profile 6/6；readiness 失败项如上（read-only-api/running-images/idp-discovery）；full 另需 backup-marker/rollback-marker 输入，未全绿。
- **9.4/9.5 partial**：`.github/workflows/ci.yml` 已含 `delivery-static`/`delivery-kind`，本地等价静态门禁全通过；无 CI runner，未实际运行 job。
- **9.7 未通过**：完整证据链已覆盖 kind 安装/部署/烟测(edge)/备份恢复/回滚/漂移/IdP 正向负向轮换，但缺 6.3/6.4/6.5/9.1 全绿与 CI run，仍标 `not passed`。
- **10.5 待评审**：需负责人对交付设计、密钥路径、受控环境边界、AC-08/AC-10 证据完整性做正式评审并入库结论。
- 环境事件（已恢复）：演练节点手工添加 containerd `config_path` 导致 CRI RuntimeService 不可用、三节点 NotReady；删除该行并重启 containerd 后恢复 Ready。结论：kind 节点不要启用 `config_path`/certs.d 手工配置，离线镜像应通过 `kind load docker-image` 且 smoke 的 imageID 校验需按该语义处理（记录为工具限制，非产品缺陷）。

## 22. 2026-11-24 8.7 / 10.5 评审结论

- **8.7 IdP 安全评审**：评审人 klb3713，结论**条件通过**，记录 `docs/stages/evidence/stage-2-idp-security-review.md`。接受本地合成 IdP 的 8.3/8.4/8.5 证据；8.6 fallback 与生产 IdP 未完成，S2-AC-10 维持 `not executed`；生产前须在真实 staging 重跑 8.4–8.6 并补 TLS/证书轮换评审。
- **10.5 交付评审**：评审人 klb3713，记录 `docs/stages/evidence/stage-2-production-delivery-review.md`。结论：S2-AC-08 partial、S2-AC-10 not executed、S2-AC-11 partial；交付设计/密钥路径/受控环境边界（本地合成数据）接受，但**不满足归档条件**，本 change 保持在途、不自动归档。
- 用户决定：不补齐 k3d（6.5）与 CI runner（9.4/9.5），相关任务保持 not executed/partial，如实收口。
