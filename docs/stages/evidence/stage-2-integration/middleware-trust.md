# 中间件顺序与信任边界验证（2026-11-23）

- `deploy/compose/compose.yaml:71`：direct core `ONTOAXIS_TRUST_GATEWAY: "false"`。
- `deploy/compose/compose.yaml:121`：edge core `ONTOAXIS_TRUST_GATEWAY: "true"`。
- `go test ./internal/platform/authn/...`：ok（认证中间件单测，含 direct 路径拒绝客户端内部身份头、只接受 X-Tenant-Id 的负向覆盖）。
- 结论：直连 core 不信任网关注入头；edge core 仅在受信网关注入时接受内部头。中间件顺序保持 requestID→observability→recoverer→accessLog→metrics→authn→module。
