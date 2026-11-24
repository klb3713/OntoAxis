# Stage 2 — production-delivery kind 实测记录

- 日期：2026-11-24（本地会话 00:14–01:35 Asia/Shanghai）
- 环境：kind v0.29.0 本地集群 `ontoaxis-kind`（1 control-plane + 2 workers，kindest/node v1.32.0），Docker Desktop 29.2.1
- 数据边界：仅本地合成数据（`ontoaxis` 库为迁移后的空库，无客户/生产数据）；所有镜像来自本地 registry `localhost:5000`
- 结论口径：本文件只记录实际执行过的命令与结果。未执行项在文末「未执行 / not passed」列出，不据此判定 AC。
- 归口：本 change（production-delivery）。静态门禁记录见 `docs/stages/evidence/stage-2-production-delivery.md`。

## 0. 集群与部署方式

| 项 | 结果 |
| --- | --- |
| `deploy/kind/kind-up.sh` | 成功，6s Ready；kubeconfig `dist/kind/kubeconfig`，context `kind-ontoaxis-kind` |
| 本地 registry | 容器 `ontoaxis-registry`（registry:2，127.0.0.1:5000，kind 网络 172.20.0.5）；节点 containerd `registry.mirrors."localhost:5000"` → `http://ontoaxis-registry:5000` |
| ArgoCD | v2.13.3，`argocd` 命名空间 7 Pod Running |
| ESO | v0.14.4（helm），3 Pod Running；ClusterSecretStore `ontoaxis-kind` Valid；5 个 ExternalSecret 全部 `SecretSynced True` |
| Envoy Gateway | GatewayClass `eg` Accepted；`deploy/edge/generated/` 应用后 Gateway `oa-edge` Programmed=True，data-plane Service `envoy-oa-system-oa-edge-06313739`（8080:30993） |
| 部署方式 | chart 渲染 + `kubectl apply`（bootstrap 预建命名空间缺 Helm 归属元数据，`helm upgrade --install` 报 `invalid ownership metadata`；已记为 kind bootstrap 缺口，见 §5） |

镜像 digest 一律取 registry 内 digest（`docker inspect RepoDigests` 中 `localhost:5000/...` 那条），已推送：

| 镜像 | digest |
| --- | --- |
| `localhost:5000/library/postgres:17.6-alpine` | `sha256:1517ffbd8fdf7c4946535604bfa8df5299456b7749e5396c0edcbb345fe76d6a` |
| `localhost:5000/getmeili/meilisearch:v1.11` | `sha256:45aa1569111bf9e46025a41e5e63a24d62c13cc7a920c02ca3c2e5f9929a7903` |
| `localhost:5000/ontoaxis-search-worker:local` | `sha256:0b8e42480a8fcb3969c0fe4d14138ea10311610e955e4923746bc860691254e6` |
| `localhost:5000/ontoaxis-core:20a6e5e` | `sha256:7e849f3aa600c707590f202d27f74d7cb41a450239deb7f02e601098596c3e66` |
| `localhost:5000/ontoaxis-web:20a6e5e` | `sha256:772bcaa627bbc3a875c775e28db819b94a1f4be98b669d352a61198069048c0e` |
| `localhost:5000/keycloak/keycloak:26.0` | `sha256:f6c2e546526b0fac99261618d9b30bfcf487ac5a087b48def2cc1ace52335f02` |
| `localhost:5000/openfga/openfga:v1.23.0` | `sha256:656cb52a9b5014dee922712aaeb82e69ef3cba87cae6bf3d8e214e1ae736a739` |

运行时状态（2026-11-24 01:35）：`oa-app` core / web / meilisearch / search 均 1/1 Running；`oa-data/postgres` 1/1（goose current version 1）；`oa-system` keycloak / openfga 1/1。data 波次（kafka、debezium、kestra、temporal、trino、nessie、apicurio）与 observability 波次未部署（chart 内 digest 为占位值，本地无对应镜像）。

## 1. 探针与 Endpoint 摘除（tasks 3.1）

命令与结果：

1. `kubectl -n oa-app patch deploy ontoaxis-core` 将 `readinessProbe.httpGet.path` 改为 `/not-ready-probe`
   - 40s 后 Pod `0/1 Running`（readiness 失败但容器未重启，restarts=0）
   - `kubectl -n oa-app get endpoints ontoaxis-core -o jsonpath='{.subsets[*].addresses[*].ip}'` → 空（Endpoint 摘除）
2. 恢复 path 为 `/readyz`
   - `kubectl -n oa-app get endpoints ontoaxis-core` → `10.244.1.38`（回填）
   - `kubectl -n oa-app get pods` → `1/1 Running`

结论：改坏 readiness 只摘除 Endpoint、不重启容器；恢复后自动回填。判定：满足 3.1 的验证条件。

## 2. 资源 requests/limits 与 OOMKilled 可观察（tasks 3.2，partial）

- chart 默认 `global.defaultResources`（kind overlay）为 requests 25m/64Mi、limits 250m/256Mi。
- 实测：keycloak 在 256Mi limits 下 `Last State: Terminated, Reason: OOMKilled, Exit Code: 137`（初始 CrashLoopBackOff 即由此造成，日志停在 "Updating the configuration"）。
- 已在 `deploy/helm/ontoaxis-platform/values.yaml` 为 keycloak 设 requests 250m/512Mi、limits 1/1Gi，重新部署后 1/1 Running、无 OOM。
- 诊断脚本可采集同类事件：§4 的 `delivery-diag.sh` 输出 `events.txt` 含 `Failed / ErrImagePull` 原因链；OOMKilled 事件同样落在 `kubectl get events -A` 范围内。

判定：partially verified——OOMKilled 可复现且可观察，但未在一次 diag 采集中同时留存 OOM 事件与 `describe` 的 `Last State` 段落，任务未勾选。

## 3. 优雅终止（tasks 3.3，partial）

- 渲染确认：core `terminationGracePeriodSeconds=30`，`preStop: exec ["/bin/sh","-c","sleep 5"]`（`deploy/helm/ontoaxis-app/values.yaml:67-69`）。
- 实测删除：`kubectl -n oa-app delete pod <core>` 阻塞 5s 返回（preStop sleep），新 Pod 14s 内 `1/1 Running`；期间 `oa-app` 其余组件未受影响。
- core 日志无显式 `shutdown` 行（HTTP 服务直接退出），因此「滚动更新期间无 5xx」未取得连续请求证据。

判定：配置与单次终止行为已验证；任务要求的滚动窗口无 5xx 烟测未执行，任务未勾选。

## 4. PDB 阻止驱逐（tasks 3.4）

PDB 清单：`kubectl get pdb -A` 共 21 个，`minAvailable=1`，单副本组件 `allowedDisruptions=0`。

直接调用 Eviction API（`kubectl proxy` → `POST /api/v1/namespaces/oa-app/pods/<core>/eviction`）：

```
eviction_http=429
{"kind":"Status","status":"Failure","message":"Cannot evict pod as it would violate the pod's disruption budget.","reason":"TooManyRequests","details":{"causes":[{"reason":"DisruptionBudget",...}]}}
```

结论：单副本 + `minAvailable=1` 时驱逐被 PDB 拒绝（429 TooManyRequests）。判定：满足 3.4 的验证条件。

（`kubectl drain --dry-run=server` 不触发 PDB 判定，输出为 "evicting pod ... (server dry run)"，不能作为 PDB 证据；上表使用真实 Eviction API。）

## 5. 备份与恢复（tasks 7.1 / 7.2 / 7.4）

### 5.1 备份（7.1）

- 部署：`helm template oa-jobs deploy/helm/ontoaxis-jobs -n oa-data -f deploy/values/common.yaml -f deploy/values/kind.yaml --set backup.image.repository=localhost:5000/library/postgres --set backup.image.digest=sha256:1517ffbd... --set backup.pgHost=postgres.oa-data.svc.cluster.local | kubectl apply -f -`
- PVC `oa-data/ontoaxis-backups`（10Gi，class `standard`）由静态 PV `ontoaxis-backups-pv`（hostPath `/tmp/ontoaxis-backups`）绑定为 Bound。
- 手动触发：`kubectl -n oa-data create job ontoaxis-backup-manual --from=cronjob/ontoaxis-backup` → `Complete 1/1`（45s 内）。

日志与 manifest：

```
starting backup backup-20261123T171434Z pg=postgres.oa-data.svc.cluster.local:5432/ontoaxis
backup complete: /backups/20261123T171434Z sha256=941ccc20eff6823f7d6abc94e56885cc3ba94ebebb67150d0aadce22da8b07da size=125036 schema_version=1
```

```json
{
  "backup_id": "backup-20261123T171434Z",
  "created_at": "20261123T171434Z",
  "sha256": "941ccc20eff6823f7d6abc94e56885cc3ba94ebebb67150d0aadce22da8b07da",
  "size_bytes": 125036,
  "schema_version": "1",
  "image_digest": "localhost:5000/library/postgres@sha256:1517ffbd8fdf7c4946535604bfa8df5299456b7749e5396c0edcbb345fe76d6a",
  "pg_version": "17.6"
}
```

产物：`ontoaxis.dump`（125036 B，`pg_dump -Fc`）+ `manifest.json`，位于节点 `ontoaxis-kind-worker:/tmp/ontoaxis-backups/20261123T171434Z/`。sha256 由 `delivery-restore.sh` 复算校验通过。判定：满足 7.1 的验证条件。

### 5.2 恢复（7.2）

命令：`bash scripts/delivery-restore.sh --backup-dir dist/backups/20261123T171434Z`

```
dump sha256 verified
namespace 'oa-restore' ready
deployment "ontoaxis-restore-postgres" successfully rolled out
pg_restore completed
goose latest version_id=1 (manifest schema_version=1)
representative row counts:
  select 'core.organizations', count(*) from core.organizations -> core.organizations|0
  select 'ontology.object_types', count(*) from ontology.object_types -> ontology.object_types|0
  select 'object_data.objects', count(*) from object_data.objects -> object_data.objects|0
restore verification complete
```

未指定 `--core-image`，故未启动 core 只读校验（脚本按设计跳过）。判定：校验项全部通过；core `/readyz` + 只读 API 这一段未执行，任务按「完整恢复并全项校验」口径不勾选。

### 5.3 RPO/RTO 实测（7.4）

| 指标 | 实测值 | 说明 |
| --- | --- | --- |
| 备份开始/完成 | 2026-11-23T17:14:34Z（Job 启动）→ `backup complete` 同秒内 | 空库 125 KB，耗时 < 5s |
| 恢复开始 | 2026-11-23T17:17:43Z | `dump sha256 verified` |
| 恢复完成 | 2026-11-23T17:18:47Z（首轮）/ 17:19:30Z（修复行数查询后复跑） | 含创建独立 Postgres 的 rollout（约 60s） |
| 实测 RTO | 约 107s（含独立 Postgres 起库） | 仅技术验证，非生产承诺 |
| 数据点差异 | 0 行（`core.organizations`/`ontology.object_types`/`object_data.objects` 均为 0） | 与 Stage 2 暂定目标 RPO ≤24h、RTO ≤60min 对比：RTO 远低于目标；因无业务数据，RPO 未做真实数据点对比 |

结论措辞：以上为 kind 本地合成环境的**技术验证**，不构成生产 RPO/RTO 承诺。

## 6. 坏部署回滚演练（tasks 5.9，partial）

过程：

1. `kubectl -n oa-app set image deploy/web web=localhost:5000/ontoaxis-web@sha256:0000...0000`
   - 新 Pod `web-6485888bbf-tjjbl 0/1 ErrImagePull`；旧 Pod `web-8458f7ff6-g4954 1/1 Running` 继续服务（rolling update 未杀旧副本）
   - `kubectl rollout status deploy/web` → `error: timed out waiting for the condition`
2. `kubectl -n oa-app rollout undo deploy/web` → `deployment.apps/web rolled back`
   - 40s 后 `web-8458f7ff6-g4954 1/1 Running`，`rollout status` 成功
   - 恢复后的 digest `sha256:772bcaa6...` 与演练前一致；坏 ReplicaSet 缩容至 0

判定：缺失镜像这一种故障的回滚已成功；任务要求的第二种故障（readiness path 覆盖触发 progress deadline）与 ArgoCD `Degraded` 事件未采集（本环境 ArgoCD 未接管部署，见 §8），任务不勾选。

## 7. 安装失败诊断（tasks 6.6）

1. 注入：`kubectl -n oa-app run diag-bad-image --image=localhost:5000/ontoaxis-web@sha256:0000...0000`
2. `bash scripts/delivery-diag.sh` → exit 0，产出 `dist/delivery/diagnostics-<ts>/`（SUMMARY/applications/deploy/describe/events/externalsecrets/gatewayclass/logs/nodes/pods/sts/svc）
3. `events.txt` 含 `Failed to pull image ... ErrImagePull` 完整原因链；`SUMMARY.txt` 声明 "Kubernetes Secrets are intentionally NOT collected"

脱敏修复：首轮采集在 `describe.txt` 中出现 `postgres://ontoaxis:pg-local-pass@...`（URI 内嵌凭据，未被 `password=` 规则覆盖）。已在 `scripts/delivery-diag.sh` 的 `redact()` 增加 URI 凭据规则，复跑后为 `postgres://<redacted>@postgres.oa-data.svc.cluster.local:5432/openfga?sslmode=disable`，全目录无 `pg-local-pass` / `local-openfga-token` / `local-kc-admin-pass` 命中；含凭据的旧目录已删除。判定：满足 6.6 的验证条件。

## 8. 入口 port-forward 与烟测（tasks 6.7 / 9.1，partial）

入口：`kubectl -n envoy-gateway-system port-forward svc/envoy-oa-system-oa-edge-06313739 18088:8080`（Gateway data-plane 端口为 8080，不是 80）。

`SMOKE_EDGE_SERVICE=envoy-gateway-system/envoy-oa-system-oa-edge-06313739 SMOKE_EDGE_TARGET_PORT=8080 bash scripts/delivery-smoke.sh --env kind`：

| 检查 | 结果 |
| --- | --- |
| edge-port-forward | true（svc → 127.0.0.1:18088） |
| core-healthz | true HTTP 200 |
| core-readyz | true HTTP 200 |
| read-only-api | true HTTP 401（未带 token 时 fail-closed） |
| web-root | **false HTTP 404**（edge 路由未覆盖 `/` → web） |
| search-path | true HTTP 404（预期 404/503） |

6/7 通过，`passed=false`。判定：6.7 的 port-forward 入口验证满足；9.1 要求「全绿退出 0」，未满足，任务不勾选。

## 9. IdP 检查与负向（tasks 8.3 / 8.4）

### 9.1 正向（8.3）

- Keycloak（kind 内 v26.0.8 dev 模式）导入 `deploy/compose/keycloak/realm-ontoaxis.json`（`POST /admin/realms` → 201）。
- `deploy/values/kind.yaml` 的 OIDC 覆盖修正为：issuer `http://127.0.0.1:18083/realms/ontoaxis`（与 port-forward 拿到的 token `iss` 一致）、jwksUrl `http://keycloak.oa-system.svc.cluster.local:8080/realms/ontoaxis/protocol/openid-connect/certs`（Pod 内可解析）。
- `bash scripts/delivery-idp-check.sh --issuer http://127.0.0.1:18083/realms/ontoaxis --core-url http://127.0.0.1:18088`（带 `OIDC_TEST_TOKEN`）：

```
PASS discovery issuer/endpoints/alg consistent
PASS JWKS has keys with kid
PASS test token accepted by core read-only API (HTTP 200)
idp-check: 3 passed, 0 failed
```

日志与输出均未打印 token。判定：8.3 的「受控用户登录/token + 只读 API 200」满足；claim→身份上下文映射未单独断言，任务按该口径不勾选（见下）。

### 9.2 负向（8.4，partial）

| case | 结果 |
| --- | --- |
| 无 Authorization | 401 |
| 非 JWT 字符串 | 401 |
| 结构合法但签名伪造 | 401 |
| 有效 token 篡改签名末位 | 401 |
| audience 错误 / 其他 issuer / 过期 / 坏 kid / 缺失 claims | not executed（本地无法签发这些变体，无 staging IdP 控制权） |

判定：已执行项全部 fail-closed；五项构造未执行，任务不勾选。

## 10. 本轮修复的交付缺陷（本 change 范围内）

| 编号 | 缺陷 | 影响 | 处置 |
| --- | --- | --- | --- |
| D-PD-01 | `deploy/helm/ontoaxis-platform/values.yaml` 的 keycloak/openfga 组件无 `command`/`args` | 容器只打印 help 后退出（CrashLoopBackOff） | 已加 keycloak `kc.sh start-dev`、openfga `/openfga run` |
| D-PD-02 | openfga 入口是 `/openfga`，不是 PATH 中的 `openfga` | `StartError: executable file not found in $PATH` | 已改为绝对路径 |
| D-PD-03 | keycloak 探针用 `/healthz`、端口 http | 启动探针 404 → 反复重启 | 已加 `KC_HEALTH_ENABLED`/`KC_METRICS_ENABLED`、暴露 management:9000、探针改 `/health/live`+`/health/ready` |
| D-PD-04 | keycloak 沿用默认 limits 256Mi | OOMKilled(137) | 已设 requests 512Mi / limits 1Gi |
| D-PD-05 | kind overlay `issuer` 用 `localhost`、`jwksUrl` 为空 | token `iss` 不匹配（401）；Pod 内无法解析 JWKS | 已改 `127.0.0.1:18083` + 集群内 JWKS URL |
| D-PD-06 | smoke 硬编码 port-forward 远端端口 `80` | Envoy Gateway Service 是 8080，烟测全部 HTTP 000 | 已加 `SMOKE_EDGE_TARGET_PORT`（默认 80） |
| D-PD-07 | restore 行数查询未带 schema | 三条查询全部 `relation does not exist` | 已改 `core.organizations` / `ontology.object_types` / `object_data.objects` |
| D-PD-08 | diag 脱敏漏掉 URI 内嵌凭据 | 诊断目录泄露本地 DB 口令 | 已加 URI 凭据规则，复跑验证 |
| D-PD-09 | smoke JSON 缺 environment/digest/overall | 9.1 要求的字段无法被脚本复核 | 已补齐（`environment`/`env`/`digest`/`overall`/`passed`/`checks`/时间戳） |
| D-PD-10 | kind bootstrap 预建命名空间无 Helm 归属元数据 | `helm upgrade --install` 报 `invalid ownership metadata` | 未修（本轮改用 chart 渲染 + apply）；需在 bootstrap 中改用 ArgoCD 或补 annotation |
| D-PD-11 | kind overlay 未覆盖 data/observability 波次与 jobs backup 的 image digest | 相应组件 ImagePullBackOff | 部分修（backup 用 `--set` 覆盖）；波次镜像需本地构建或离线导入 |

## 11. 跨 change 缺陷（不属本 change，记录待归档方处置）

| 编号 | 现象 | 证据 | 归属 |
| --- | --- | --- | --- |
| X-01 | Envoy Gateway 将 SecurityPolicy `extAuth.http.path` 当**前缀**使用，实际请求变成 `/internal/edge-auth/api/v1/tenancy/organizations`，core 的 edge-auth 端点因此 401 —— 带有效 token 的 protected 路由不可用 | `oa-app/ontoaxis-core` 日志 `path=/internal/edge-auth/api/v1/tenancy/organizations status=401`；envoy access log `route_name=httproute/oa-system/oa-edge-protected/...` `response_code=401` | `edge-gateway`（已归档） |
| X-02 | 生成的路由表无 `/` → web 的路由，烟测 `web-root` 404 | §8 表格 | `edge-gateway`（已归档） |
| X-03 | 生成的 HTTPRoute/SecurityPolicy 后端固定 `name: ontoaxis-core`（同命名空间），而 chart 把 core 部署在 `oa-app`；需在 `oa-system` 建 Service+Endpoints 才能连通 | 本文件 §0/§8 的临时 Service+Endpoints | `edge-gateway`（已归档） |

## 12. 未执行 / not passed

- tasks 3.3 滚动窗口连续请求无 5xx
- tasks 5.2–5.5、5.7、5.8 的 ArgoCD 部分：`deploy/argocd/root-app.yaml` 与 `apps/*.yaml` 的 `repoURL` 仍为占位 `https://github.com/ontoaxis/ontoaxis.git`，无本地 GitOps 源，root app 未应用；`argocd app history`、wave 顺序、漂移自愈均 not executed
- tasks 5.6 release manifest 与运行 digest 一致性检查（`delivery-release-check.sh --env kind` 仍按占位 digest 失败）
- tasks 6.3 `bootstrap.sh`（本轮手动执行了其中大部分步骤）、6.5 k3d（未尝试，与 kind 共用 Docker 路径）
- tasks 7.6 runbook 复核（runbook 已存在，本轮按命令实际执行了备份与恢复，但未做 runbook 全文走查）
- tasks 8.4 五项构造负向、8.5 JWKS 轮换、8.6 fallback Keycloak、8.7 安全评审
- tasks 9.5 CI `delivery-kind`（本地未跑 CI）、9.7 完整证据链（缺 ArgoCD/漂移/回滚第二种故障）
- tasks 10.4 全量门禁复跑、10.5 负责人评审

## 13. AC 影响

| AC | 结论 | 依据 |
| --- | --- | --- |
| S2-AC-08 | **not passed** | kind 内探针/PDB/备份恢复/回滚/IdP 均有实测，但 ArgoCD GitOps（5.2–5.5）、k3d（6.5）、第二种回滚故障、完整证据链未完成；烟测 `web-root` 未通过 |
| S2-AC-10 | **not executed（仍不满足 pass）** | 本轮 IdP 验证跑在 kind 内 Keycloak（dev 模式、H2），属本地合成 IdP；AC-10 要求非本地 IdP 与 claims 映射评审，8.5–8.7 未执行 |
| S2-AC-11 | partial | 与 stage-2-integration 的接口（search 重建路径、共享文件归属）尚无本轮新增证据 |

本文件不构成归档依据；PD 归档仍需 tasks 全量完成与负责人评审（tasks 10.5）。

## 14. 2026-11-24 复核补充（全量门禁与重复验证）

- **10.4 全量门禁全绿**（日志 `dist/gates/full-gates.log`、`dist/gates/code-gates.log`）：`make lint` rc=0；`make test` rc=0；`openspec validate production-delivery --strict` → valid；`openspec validate --all --strict` → 19 passed 0 failed；`helm lint` 5 charts 0 failed；`actionlint` rc=0；`shellcheck scripts/delivery-*.sh deploy/kind/*.sh` rc=0；`gitleaks detect` → no leaks（34 commits）；`delivery-secret-scan.sh` rc=0。
- **3.2 复测**：`kubectl -n oa-app set resources deploy/meilisearch -c meilisearch --limits=memory=32Mi --requests=memory=8Mi` → Pod CrashLoopBackOff；`DIAG_OUT=dist/delivery/diagnostics-oom bash scripts/delivery-diag.sh` 采集到 `Last State: Terminated / Reason: OOMKilled / Exit Code: 137`（`dist/delivery/diagnostics-oom/describe.txt:2242-2247`）；已恢复 256Mi/64Mi，rollout 成功。
- **3.3 复测（partial）**：集群内 alpine Pod（`localhost:5000/library/alpine:3.20`，digest `sha256:6c2a9711b0a9f32b0239d9222eb1072309cf46c6431d319ae249186d811a987c`）持续请求 `http://ontoaxis-core:8080/readyz`；core 3 副本、预热 10s、`kubectl rollout restart`；76 次 = 70×200、**0×5xx**、6×000（第 61–66 次，约 1.5s 连接中断）。结论 partial：无 5xx 但存在短暂连接失败。
- **3.4 复测**：`kubectl proxy --port=18001` + `POST /api/v1/namespaces/oa-app/pods/<core>/eviction` → HTTP 429，`Cannot evict pod as it would violate the pod's disruption budget.`（reason `TooManyRequests`，`DisruptionBudget ontoaxis-core-pdb needs 1 healthy pods and has 1 currently`）。
- **6.7 复测**：`SMOKE_EDGE_SERVICE=envoy-gateway-system/envoy-oa-system-oa-edge-06313739 SMOKE_EDGE_TARGET_PORT=8080 bash scripts/delivery-smoke.sh --env kind` → 6/7（edge-port-forward、core-healthz 200、core-readyz 200、read-only-api 401、search-path 404 通过；web-root 404 = 跨 change 缺陷 X-02）。日志 `dist/delivery/smoke-20261124.log`。
- **8.3 复测**：kind 内 Keycloak `http://127.0.0.1:18083/realms/ontoaxis`，password grant 取 token；`OIDC_TEST_TOKEN=... bash scripts/delivery-idp-check.sh --issuer ... --core-url http://127.0.0.1:18088` → 3 passed 0 failed；claims 解码（不打印 token）：`sub` UUID、`tenant_id=11111111-1111-4111-8111-111111111111`、`org_ids=[同 UUID]`、`email=admin@ontoaxis.local`、`roles=[tenant-admin,member,platform-admin]`、`aud=ontoaxis-api`，与 `internal/platform/authn/claims.go` 一致。
- **缺陷修复**：core Deployment 的 livenessProbe 路径曾被残留为 `/not-readz-probe`（404，导致每 ~80s 重启），已 patch 回 `/healthz`、readiness=`/readyz`，`kubectl rollout status` 成功。
- 环境：kind `ontoaxis-kind`（1 control-plane + 2 workers，v1.32.0 全部 Ready）；Docker 29.2.1；本地工具 `/tmp/oa-tools/bin/{shellcheck,actionlint,gitleaks,kubeconform}`。

## 15. 2026-11-24 复核补充（5.6 release manifest、7.2/7.6 恢复演练）

- **5.6 release manifest + release-check**：`deploy/releases/kind.yaml` 重写为 kind 实际运行镜像清单（7 个镜像组件：ontoaxis-core/web/search-worker、meilisearch、postgres、keycloak、openfga；chart-only 组件无独立镜像 digest，不列入）。正向 `bash scripts/delivery-release-check.sh --env kind` → 7 个 field-check PASS + 7 个 running digest PASS，rc=0；负向（core digest 末位改 1，`RELEASE_MANIFEST=/tmp/kind-bad.yaml`）→ `FAIL running digest mismatch` + `release check FAILED`，rc=1。
- **脚本缺陷修复**：`scripts/delivery-release-check.sh` 使用 `mapfile`（bash 4+）在 macOS bash 3.2 报 `line 104: mapfile: command not found`，已改为 `pairs=(); while IFS= read -r _pair; do pairs+=("$_pair"); done < <(...)`。
- **7.2 / 7.6 复核：L1–L4 通过，L5 未执行。** `KUBECONFIG=dist/kind/kubeconfig bash scripts/delivery-restore.sh --context kind-ontoaxis-kind --backup-dir dist/backups/20261123T171434Z`：备份元数据、大小、格式与 SHA-256 验证通过，归档目录可读，单事务恢复成功，goose 版本 1，三张代表表各 0 行，退出 0 并清理临时命名空间。截断及同长度损坏备份分别因大小及 SHA-256 失败，退出 1，均未触及 Kubernetes。日志目录 `dist/delivery/restore-recheck.gd8w8PyD/`。
- 旧探测中的 `/readyz` 200 和匿名 API 401 不证明只读业务可用；core 会在启动时执行迁移。现脚本拒绝 `--core-image`，不向恢复环境传入外部鉴权凭据。7.2/7.6 不勾选。

## 16. 2026-11-24 补充（3.3 / 6.6 / 9.6）

- **3.3 优雅终止**：core 配置 `terminationGracePeriodSeconds=30`、`preStop=["/bin/sh","-c","sleep 5"]`；集群内 alpine Pod 在 `kubectl rollout restart` 窗口内连续请求 `http://ontoaxis-core:8080/readyz` 76 次：70×200、**0×5xx**、6×000（约 1.5s 连接中断）。6 次传输失败不能按 0×5xx 视为无中断；core 当前单副本 Recreate、且启动执行迁移，3.3 保持部分通过、不勾选。
- **6.6 安装失败诊断**：坏镜像 Deployment `diag-bad-image` → `VERIFY_TIMEOUT=20 DIAG_OUT=dist/delivery/diagnostics-badimage-verify bash deploy/kind/verify.sh` 返回 rc=1，自动采集诊断；`events.txt` 含 `Error: ErrImagePull`；诊断目录无密钥命中。日志 `dist/delivery/verify-badimage.log`。
- **9.6 make 目标清单**：`make help` 增加 Delivery targets 段；已执行 delivery-lint（5 charts lint 0 failed）、delivery-render-check（RESULT: PASS）、delivery-config-check（OK）、delivery-smoke（6/7）、delivery-diag、delivery-backup（Complete）、delivery-restore（L1–L4；L5 未执行）、delivery-idp-check（3 passed）、delivery-promote/rollback（负向 rc=1）；kind-up 用于创建本集群；kind-down 仅做 `make -n` 与 `bash -n` 接线检查（未执行以免销毁验证环境）。
- **任务计数**：PD tasks 46/66；OSM 44/52；stage-2-integration 23/52；jaeger-v2 0/10。
