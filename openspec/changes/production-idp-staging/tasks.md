# Tasks: production-idp-staging

> 计划任务（2026-11-24）。全部未实施，未勾选。

## 1. TLS fallback IdP
- [ ] 1.1 非 localhost TLS issuer 启动并可登录
- [ ] 1.2 JWKS 轮换不停机验证

## 2. 真实 staging IdP
- [ ] 2.1 接入 staging issuer/audience/JWKS
- [ ] 2.2 claims 映射断言（tenant-id/org-ids/workspace-ids/realm-roles）

## 3. 负向与安全
- [ ] 3.1 六项负向 token 全部 401/403
- [ ] 3.2 IdP 安全评审复评并签核
- [ ] 3.3 证据归档并更新 S2-AC-10 traceability
