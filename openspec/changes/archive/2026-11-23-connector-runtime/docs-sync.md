# 文档同步建议（connector-runtime）

> 本 change 不修改共享文档；以下建议移交 `stage-2-integration` / `production-delivery`。

1. `ARCHITECTURE.md` §7 运行时拓扑：补充 Kafka Connect 作为 connectors/ingestion 的运行时依赖，标注 `connector-runtime` profile 为本地/测试用，production 由部署层提供。
2. `ARCHITECTURE.md` §15.2 状态：新增 `connector-runtime` 条目，说明真实 CDC 链路由该 change 引入，当前证据为单元/静态校验，E2E 待 integration/production 合并。
3. connectors/ingestion 模块 README 或 `docs/reference/`：补充 `ONTOAXIS_CONNECT_*`、`ONTOAXIS_ICEBERG_*`、`ONTOAXIS_SECRET_*` 配置与 “空 URL 即禁用、StartSync 不可用” 语义；说明凭据只在提交时解析、不得落库/日志。
4. 运行说明：`deploy/connector-runtime/README.md` 作为本地运行入口；生产接入不得复用本地默认口令与无认证 Connect。
5. 迁移说明：`runtime_ref/attempt/lease_owner/lease_until` 为 additive，回滚时仅停止 profile/配置，不自动回滚数据；清理脚本见 `scripts/cleanup.sh`。
