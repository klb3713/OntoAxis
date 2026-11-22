# 平台迁移约定

> 适用于 `internal/platform/db/migrations/` 与各业务模块 `internal/modules/<module>/migrations/`。

## 顺序

1. 平台迁移（`jobs.outbox`、`jobs.operations`）先执行。
2. 业务模块迁移按 `ARCHITECTURE.md` 3.4.4 固定顺序执行：
   `tenancy → connectors → ingestion → datasets → pipelines → ontology → objects → query → actions`。
3. 同一模块内的迁移按 goose 文件名时间戳顺序执行。

## 命名

`YYYYMMDDHHMMSS_<module>_<verb>_<noun>.sql`，回滚脚本同目录 `*.down.sql`。

## 规则

- 只追加，不修改已发布迁移；
- 每个迁移在单个事务中执行，失败整体回滚；
- 只允许写本模块 schema；
- 删除列/放宽约束必须有回填或兼容迁移说明；
- 应用启动时自动执行 `goose.UpContext`，不提供手工 ORM auto-migrate；
- 平台 schema `jobs` 由 foundation owner 维护，业务模块不得修改。
