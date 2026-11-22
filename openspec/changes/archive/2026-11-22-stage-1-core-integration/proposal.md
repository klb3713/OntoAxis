# Stage 1 Core Integration

## Why

九个业务模块和平台基座已分别实现，但只有装配到同一进程、运行在同一 PostgreSQL 上并以真实 HTTP 请求贯通，才能证明 Stage 1 交付的是可运行的核心，而不是九个独立代码目录。

## What Changes

- 在 `cmd/ontoaxis-core` 中按依赖顺序装配九个模块，并在 `internal/app` 建立唯一的模块工厂。
- 在业务路由前接入 `authn.EdgeAuthenticator`，把可信网关身份注入模块租户上下文；健康与指标端点保持公开。
- 在本地 Compose 中运行 core + PostgreSQL（以及外部查询/前端依赖），让一次 `docker compose up` 可启动核心闭环。
- 增加集成级 smoke 测试、模块顺序测试和 Stage 1 证据记录。
- 不改变任何模块的领域行为或 API 契约。

## Impact

- Affected code: `cmd/ontoaxis-core`, `internal/app`, `internal/platform/httpserver`, `deploy/compose`, `Makefile`, `openspec/changes/stage-1-core-integration`.
- No database schema changes beyond the modules' own migrations.
- No new external service is required for the minimal Compose loop; Trino and web remain optional local profile services.
