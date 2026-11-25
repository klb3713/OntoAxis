# Production IdP Staging

> 状态：planning（2026-11-24 创建，Stage 3 候选，暂不归属 Stage）。本 change 尚未实施：`tasks.md` 全部未勾选。
> 来源：`production-delivery` 归档时移出的 8.6 与 S2-AC-10。见 `docs/stages/evidence/stage-2-production-delivery-archive-review.md`。

## Why

本地合成 Keycloak 的 8.3/8.4/8.5 只证明技术路径；真实 staging IdP（非 localhost、TLS、真实 claims mapper）与安全复评未完成。S2-AC-10 已移出 Stage 2，需独立 change 承接，避免在主 spec 固化未实现的 IdP 行为。

## What Changes

- 搭建非 localhost TLS fallback IdP，验证登录、token 校验、JWKS 轮换不停机。
- 接入真实 staging IdP 的 issuer/audience/JWKS/claims 映射（tenant-id、org-ids、workspace-ids、realm-roles）。
- 负向矩阵（audience/issuer/expired/bad_kid/missing_claims/bad_signature）fail-closed。
- IdP 安全评审复评与证据归档，作为 S2-AC-10 的验收依据。
- 执行 E2E-11（自 `stage-2-integration` 7.5 于 2026-11-25 移交）：非本地 OIDC 登录 → API → claims 断言，并映射 S2-AC-10 证据。
