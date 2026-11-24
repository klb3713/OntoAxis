# Stage 2 IdP 配置与 Claims 映射安全评审（PD 8.7）

状态：**草稿，待负责人（klb3713）签核**；评审通过前 S2-AC-10 不得标 pass。

## 评审范围
- 交付环境：本地 kind（`ontoaxis-kind`、演练 `ontoaxis-kind-rehearsal`）与本地 Compose；均为合成数据与本地合成 IdP，不涉及真实云账号、生产密钥或客户数据。
- 关联任务：8.1–8.6；关联 change：production-delivery。

## 核对清单
| # | 检查项 | 证据 | 结论 |
|---|--------|------|------|
| 1 | issuer 白名单：core 仅接受配置的 issuer，且 issuer 与 token `iss` 逐字节一致 | `deploy/values/kind.yaml` global.oidc.issuer；8.4 issuer 负向 401（`dist/delivery/idp-negative/`） | 符合（单 issuer，无动态信任） |
| 2 | audience 严格匹配：`ontoaxis-api`，错误 audience 拒绝 | 8.4 audience 负向 401 | 符合 |
| 3 | JWKS 来源固定：集群内 Keycloak Service DNS，非公网 | `ONTOAXIS_OIDC_JWKS_URL=http://keycloak.oa-system.svc.cluster.local:8080/realms/ontoaxis/protocol/openid-connect/certs` | 符合（本地环境；生产需 HTTPS） |
| 4 | 签名算法与 kid：仅 RS256；未知 kid/坏签名拒绝 | 8.4 bad_signature/bad_kid 401；8.5 轮换新旧 kid 均 200 | 符合 |
| 5 | claim 名称固定：`tenant_id`、`org_ids`、`roles`、`aud`、`sub`、`email` | 8.3 claims 解码（`dist/delivery/idp-negative/control.token` 等） | 符合 |
| 6 | 缺失映射 claims fail-closed | 8.4 missing_claims 401 | 符合 |
| 7 | 过期 token 拒绝 | 8.4 expired 401 | 符合 |
| 8 | 无生产密钥/客户数据；密钥只来自 K8s Secret/ESO | `scripts/delivery-secret-scan.sh` rc=0；repo 无 `.env.kind` | 符合 |
| 9 | token 不落日志 | `delivery-idp-check.sh` 不打印 token；诊断脱敏规则 | 符合 |
| 10 | fallback IdP 路径 | 8.6 仅完成临时证书与 in-cluster Keycloak 可达性；未完成非 localhost TLS 登录/负向 | **未满足**（partial） |
| 11 | 生产 IdP 计划与密钥轮换 | 未在真实 staging IdP 验证（本地合成） | **未满足**（not executed） |

## 结论（待签核）
- 本地合成 IdP 的正向/负向/轮换路径证据充分；**不构成生产 IdP 安全验收**。
- 建议：S2-AC-10 维持 `not executed`；生产前需在真实 staging IdP 重跑 8.4/8.5/8.6 并补 TLS/证书轮换评审。

## 评审记录
- 评审人：klb3713（项目负责人）
- 评审时间：2026-11-24
- 结论：**条件通过（conditional）**
- 条件：接受本地合成 IdP 的 8.3/8.4/8.5 证据；8.6 fallback 与生产 IdP 路径未完成，S2-AC-10 维持 `not executed`；进入生产前须在真实 staging IdP 重跑 8.4–8.6，并完成 TLS/证书轮换与轮换窗口评审。
