# object-search profile 集成（2026-11-23）

## 启动方式
```
docker compose -f deploy/compose/compose.yaml \
  -f deploy/compose/authz/compose.authz.yaml \
  -f deploy/compose/search/compose.search.yaml up -d --build
```
新增 `deploy/compose/search/{Dockerfile,compose.search.yaml}`：Meilisearch v1.11 + `ontoaxis-search` consume worker + core search env。

## 修复的阻塞缺陷
- `search.Client.EnsureIndex` 未等待 `update_settings` 任务；已改为等待 task 成功。
- `update_settings` 方法错误使用 `PUT`（Meilisearch 返回 405），已改为 `PATCH`。
- `SettingsJSON` 含非法字段 `primaryKey`（settings 接口 400），已移除（主键在创建索引时设置）。
- consumer `ApplyEvent` 未确保索引/设置存在；已在上传/删除前调用 `EnsureIndex`。

## 验证
- `go test ./internal/platform/search/`：ok。
- 创建对象后 `GET /api/v1/objects/search?q=needle-search-xyz7` 返回该对象，`total=3`（含此前数据）。
- 更新对象后 Meilisearch 文档为 `display_name=search-upd-after, properties_text=name=search-upd-after`；删除后文档不存在。
- 全量重建：`search-worker rebuild --tenant 11111111-1111-4111-8111-111111111111 --verify` → `documents=16, verified=true`，exit 0。
- core `/readyz` 与 worker `/readyz` 均 200。

## 限制
- 本地 Meilisearch 单节点、master key 为本地开发值；生产部署由 production-delivery 负责。
- 搜索一致性依赖 outbox 异步消费，窗口约 5s。
