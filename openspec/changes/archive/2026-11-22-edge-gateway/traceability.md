# Traceability: edge-gateway

## Stage 2 AC 映射

| Stage AC | 验收要求 | 本 change 覆盖 | 证据（planned/actual） |
|---|---|---|---|
| S2-AC-02 | `oa-edge` 独立部署可用：路由与 Stage 1 一致，JWT/租户上下文透传，未认证 401、越权 403、限流与未知路由行为可验证 | `edge-gateway` 全部 5 个 ADDED requirements；`identity-edge` 既有 JWT/tenant 契约作为前置 | `scripts/edge-smoke.sh`（Traefik 11/11、fail-closed 12/12）、`scripts/edge-smoke-envoy.sh`（kind 10/10）、`docs/stages/evidence/stage-2-edge-gateway.md`、CI run 45026353151 |

## Requirement 映射

| Requirement | 场景 | 预期证据 |
|---|---|---|
| 路由表单一来源与确定性生成 | 相同输入生成相同配置；重复前缀/未知 auth 被拒绝；摘要可复核 | `tools/edgegen` 单测；`make edge-generate-check` 两次运行；生成物 diff |
| 路由匹配与负向响应 | 最长前缀优先；未知 404；错误方法 405；公开 allowlist；metrics 集群内 | `scripts/edge-smoke.sh` 与 `scripts/edge-smoke-envoy.sh` 的状态码矩阵；kind Envoy Gateway v1.2.6 运行验证 |
| 受保护路由的身份边界 | 无 JWT 401；伪造 header 清理；租户不匹配 403；公开路由不注入身份 | JWT 负向测试；Traefik 响应日志；`identity-edge` 既有 authn 测试；Envoy extAuth 结构 |
| 入口限流 | 超限 429 + Retry-After；健康路由豁免；租户独立 | `edge-smoke.sh` / `edge-smoke-envoy.sh` 限流用例；Traefik `Retry-After`、Envoy 429 证据 |
| Provider 一致性与独立部署 | health 一致；未知路由一致；未认证一致；readiness/digest 一致 | Traefik 启动/健康日志；`edge-generate-check`；kind Envoy Gateway Gateway Programmed=True 与两个 provider smoke |

## 未覆盖 / 后续

- 生产 TLS、公网 LoadBalancer、跨副本精确租户限流不在本 change；由 `production-delivery` 处理。
- `identity-edge` 的 JWT 校验与 tenant context 行为不在本 change 重定义；两种 provider 都通过 core 内部 `/internal/edge-auth` 复用该契约（Envoy 使用 SecurityPolicy extAuth）。
- 本 change 明确不做 edge 自治鉴权；若未来要让 Compose edge 不依赖 core，需另开 change 评估 sidecar 或插件。

## 归档检查

- [x] tasks 全部勾选且每项有证据（Envoy kind、CI 45026353151、负责人评审已完成）
- [x] `openspec validate --all --strict` 通过
- [x] `edge-gateway` 主 spec 归档后位于 `openspec/specs/edge-gateway/spec.md`（归档时生成）
- [x] Stage 2 change map 与证据索引已更新
