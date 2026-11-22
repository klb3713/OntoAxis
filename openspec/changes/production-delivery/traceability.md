# Traceability: production-delivery

## Stage 2 AC 映射

| Stage AC | 验收要求 | 本 change 覆盖 | 证据（planned/actual） |
|---|---|---|---|
| S2-AC-08 | 以 Helm chart + ArgoCD 在 kind/k3d 完成生产化部署：probes/resources/PDB 与 Secret 注入生效，备份/恢复与镜像回滚演练通过 | 全部 12 个 ADDED requirements；重点为 Helm 渲染与摘要、ArgoCD 同步、kind/k3d 安装、探针/资源/PDB、密钥注入、备份/恢复、镜像回滚、交付烟测 | planned：`docs/stages/evidence/stage-2-production-delivery.md`（kind 安装/部署/烟测、probes/PDB/Secret 生效、备份恢复演练、回滚演练、ArgoCD Synced/Healthy）；`deploy/releases/kind.yaml`；`docs/stages/evidence/logs/production-delivery/`；CI `delivery-static` run |
| S2-AC-10 | 使用非本地 Keycloak OIDC issuer/JWKS/claims 映射完成登录与 Token 验证，且不提交生产密钥 | `预发 IdP 配置与 claims 映射`、`仓库零明文密钥`、`受控环境与安全边界` requirements；staging-like values + `scripts/delivery-idp-check.sh` | planned：staging issuer discovery/登录/claim 映射证据、负向 401 用例、JWKS 轮换记录、安全评审记录、gitleaks 与 secret scan 结果；外部 IdP 不可用时以 fallback 标记 `partial` |
| S2-AC-11（协作） | Stage 2 证据与报告完成：全部 change 归档、`openspec validate --all --strict` 通过、CI 全绿、Stage 退出获负责人确认 | 本 change 提供证据源文件、CI run 与 `docs/operations/delivery-runbook.md`（ARCHITECTURE §14.7 部署 runbook 要求），由 `stage-2-integration` 汇总 | planned：`docs/stages/evidence/stage-2-production-delivery.md`、CI `delivery-static` run 链接、validate 输出、G2 门禁索引 |

说明：S2-AC-08/AC-10 的通过结论由 Stage 2 退出评审依据本 change 证据与 G2 门禁判定；本 change 只提供契约、资产与证据，不自行宣布 AC 通过。

## Requirement 映射

| Requirement | 场景 | 预期证据 |
|---|---|---|
| 可复现的 Helm 发布单元与镜像摘要固定 | 相同 values 渲染确定；latest/缺 digest 被拒；缺必填密钥引用；未声明环境被拒；chart 校验门禁 | `scripts/delivery-render-check.sh` 两次渲染 sha256 比较；负向渲染输出；kubeconform 结果；CI `delivery-static` |
| 环境 values 与受控晋升 | digest 晋升写回；缺证据晋升被拒；晋升不触碰密钥；回退上一次晋升 | `scripts/delivery-promote.sh` 输出与提交 diff；来源 env 烟测 JSON；`git revert` + 烟测记录 |
| ArgoCD 同步与漂移收敛 | 新提交 Synced/Healthy；手工漂移 self-heal；wave 顺序；Degraded；prod-like 手动同步 | `argocd app get/wait` 输出、`argocd app history`、事件与 diff 摘要；`scripts/delivery-drift-check.sh` |
| kind/k3d 可重复安装与诊断 | 干净集群安装；幂等；镜像失败诊断；卸载清理；kind/k3d 一致 | `deploy/kind/*-up|down.sh` 日志、`verify.sh` 输出、诊断目录、两次安装对比、k3d 同检查项结果 |
| 探针、资源与 PDB | 存活/就绪分离；PDB 阻止驱逐；滚动更新遵守 PDB；资源超限可观察；优雅终止 | kind 注入故障的事件/Endpoint 变化；`kubectl drain` 输出；`kubectl rollout status`+PDB 状态；OOMKilled 事件；滚动窗口烟测 0 个 5xx |
| 密钥注入与外部密钥源 | ExternalSecret 同步；缺 key 未就绪；轮换无需重建镜像；store 不可达不回退 | ESO `status`、目标 Secret key 清单（脱敏）、负向用例输出、轮换前后 Pod digest 与烟测；`deploy/argocd/secrets/README.md` |
| 仓库零明文密钥 | 明文提交阻断；仅引用 values 通过；证据脱敏；本地密钥不入库 | gitleaks 与 `delivery-secret-scan.sh` 输出、命中样例（不含完整值）、`git status` 证据 |
| 备份与恢复演练 | 备份可恢复；损坏备份被拒；恢复后就绪；manifest 缺字段被拒；RPO/RTO 记录；未覆盖范围标记 | `manifest.json` + sha256 复算；恢复脚本输出与 L1–L5 结果；篡改/截断用例；RPO/RTO 表格；residual risk 记录 |
| 镜像摘要与回滚 | 坏部署回滚；回滚目标不可用阻断；同 schema 回滚；跨 schema 走恢复；manifest 与运行摘要一致 | `argocd app history`、rollout 事件、`delivery-rollback.sh` 输出与烟测 JSON；`delivery-release-check.sh` 漂移检查；跨 schema 流程记录 |
| 预发 IdP 配置与 claims 映射 | staging 登录与 claim 映射；audience/issuer 不匹配；过期/坏签名；JWKS 轮换；本地 Keycloak 不作为证据；配置不含密钥 | `delivery-idp-check.sh` 输出（discovery、token 验证与 claims 映射，不记录 token 原文）；负向 401 矩阵；kid 变化记录；本地环境 smoke 的 `not passed` 项 |
| 交付烟测与失败证据 | 全绿可归档；失败非零退出；未执行项 not passed；证据可复核；同一脚本跨环境 | smoke JSON、diag 目录、evidence 汇总片段、跨 env 输出对比 |
| 受控环境与安全边界 | 生产凭据被拒绝；证据声明环境；云端步骤标未执行；合成数据 | 脚本拒绝输出、evidence 环境标注、`not executed` 项清单、数据来源声明与评审记录 |

## 前置依赖与门禁

- Stage 2 Wave 1 G1 门禁尚无 `pass` 记录：实现任务在门禁记录出现前不得执行（tasks 1.1 负责记录来源）。
- 依赖已归档的 `edge-gateway`（`deploy/edge/generated/`、`deploy/edge/kind/install-envoy-gateway.sh`、`make edge-generate-check`）与 `runtime-observability`（`deploy/observability/` 配置与告警契约）；本 change 不修改其行为契约。
- 依赖 `object-search-meilisearch` change 的 Meilisearch 重建/重新索引路径，作为备份未覆盖 Meilisearch 时的恢复手段；该 change 尚未归档，证据标注依赖状态。
- 依赖 Wave 1 各服务（connector-runtime、pipeline-kestra-dbt、action-temporal、authorization-openfga-cedar 等）的 Compose/Helm 配置可启动；G2 要求全部 Wave 1 服务在 compose/helm 配置下可启动。
- 外部依赖（不阻塞设计、影响证据完整性）：staging IdP 与 claims mapper 可用性；外部 secret provider 连通性（Stage 2 不验证，标 `not executed`）；真实 LoadBalancer/TLS（Stage 3）。

## 未覆盖 / 后续

- 不覆盖真实云账号、生产密钥、生产环境或真实客户数据；不做生产切换、多区域 HA/DR、容量规划、自动扩缩容、托管服务选型。
- 不覆盖公网 TLS 证书、真实 DNS、生产 LoadBalancer；kind/k3d 烟测以 port-forward 验证入口。
- 对象存储与 Meilisearch 不在 Stage 2 备份范围（依赖重建路径）；Kafka/Kestra/Temporal/Trino/Nessie 视为可重建，不保证备份一致点。
- 跨 schema 回滚不执行 down migration，要求停机 + 备份恢复；独立迁移 Job（`PreSync` hook）需要 core 增加 migrate 子命令，属后续 change。
- web 仍按环境构建（构建期 `VITE_OIDC_AUTHORITY`），运行期配置注入留待后续 change。
- 不改变业务 API/授权判定逻辑与既有 Compose 本地开发默认路径；不替代 `edge-gateway`/`runtime-observability` 能力契约。
- Argo Rollouts/金丝雀/蓝绿发布、真实外部 secret provider 连通性验证、Stage 3 生产演练不在本 change。

## 受控环境与证据边界

- 允许环境：kind、k3d、staging-like、prod-like（仅渲染验证）。禁止读取真实云账号凭据、生产密钥、真实客户数据。
- 所有证据必须标注环境类型与 digest、时间；未执行项写 `not passed`/`not executed`，不得写成 `pass`。
- 本地 Compose Keycloak（`localhost:18083`）不产生 S2-AC-10 证据；使用 fallback 时结论标 `partial` 并说明原因。

## 归档检查

- [ ] tasks 全部勾选且每项有可复核证据（kind/k3d 安装与烟测、备份恢复、回滚、IdP 验证、CI run）
- [ ] `openspec validate production-delivery --strict` 与 `openspec validate --all --strict` 通过
- [ ] S2-AC-08/S2-AC-10 有证据且结论标注 pass/partial/not passed；未达成项有明确 residual risk
- [ ] G2 门禁记录可追溯，`docs/stages/evidence/stage-2-production-delivery.md` 由 stage-2-integration 汇总
- [ ] `deploy/README.md`、`docs/operations/delivery-runbook.md`、Stage 2 change map 与证据索引已同步
- [ ] 负责人评审通过（含安全评审），确认未连接真实云、未提交生产密钥、未使用真实客户数据
