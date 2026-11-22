# Proposal: dataset-catalog

## Why

Stage 1 的核心数据路径需要一个稳定的数据集目录：Query、Pipeline 以及后续的对象实例绑定都需要先知道“有哪些数据集、当前在哪个分支、最近一次事务是什么”。当前仓库只有平台骨架，没有 datasets 模块，也没有任何业务 schema 或 API。

本 change 把数据集目录建成一个可独立测试的最小垂直切片：领域模型、租户隔离的仓储、服务层、HTTP API 和幂等迁移一起交付，为后续的 Query/Pipeline 集成提供 `module.Module` 接入点。

## What Changes

- 新增 `dataset-catalog` capability，覆盖 Dataset、Branch、Transaction、View 与 S3 文件元数据。
- 新增 `internal/modules/datasets/` 模块，实现 `module.Module`，`Dependencies()` 返回 `tenancy`。
- 新增 `dataset` schema 的幂等 DDL：`datasets`、`dataset_branches`、`dataset_transactions`、`dataset_views`、`dataset_files`，并为租户表启用 RLS 与 FORCE ROW LEVEL SECURITY。
- 新增 `/api/v1/datasets` 路由族：数据集 CRUD、分支创建/查询、事务开始/提交/中止、视图创建/查询/删除、文件元数据登记/查询/删除。
- 新增统一的分页、错误映射和 fail-closed 租户解析：缺失租户返回 401，跨租户读取表现为 404。
- 测试使用内存 fake store 与 `httptest`，不依赖真实 Postgres、S3 或 Nessie。

### Out of Scope

- 真实 Iceberg/Nessie/S3 SDK 调用与数据读写；本 change 只保存 Ontology 语义元数据。
- `PreviewData`、`ListVersions` 等需要读取底层表/版次的 RPC。
- 权限策略、质量规则、保留策略、Pipeline 绑定的业务实现；schema 中保留表结构，但本 change 不提供 API。
- 认证中间件本身：租户解析器按接口注入，默认只信任 identity-edge 注入的内部头。
- 修改 `cmd/`、`internal/app/`、`internal/platform/`、`go.mod`、`go.sum`，或把模块注册进主程序。

## Impact

- 新增代码：`internal/modules/datasets/**`（domain、service、repo、api、migrations、module.go）。
- 新增规格：`openspec/changes/dataset-catalog/specs/dataset-catalog/spec.md`。
- 新增 schema：5.1 的 9 张 `dataset` 表。本 change 使用并暴露 `datasets`、`dataset_branches`、`dataset_transactions`、`dataset_views`，新增 S3 元数据表 `dataset_files`；`dataset_permissions`、`quality_rules`、`retention_policies`、`pipeline_bindings` 只建表，不提供 API。所有租户表启用 RLS 与 FORCE ROW LEVEL SECURITY。
- 依赖：仅依赖 `tenancy` 模块；复用 `internal/platform/module`、`internal/platform/db`、`internal/platform/httperr`、`internal/platform/httpserver`，不新增第三方依赖。
- 下游：Query、Pipeline 与对象实例绑定可按 `module.Module` 注册本模块；真实数据库迁移与端到端认证联调需要集成门 G1 的执行环境。
