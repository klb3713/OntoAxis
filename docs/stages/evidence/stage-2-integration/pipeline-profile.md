# pipeline-kestra-dbt 本地 profile 预检（2026-11-23）

## 文件
- `deploy/compose/pipeline-kestra/Dockerfile`：`kestra/kestra:v0.24.0` + `dbt-trino==1.8.0`。
- `deploy/compose/pipeline-kestra/compose.pipeline.yaml`：Kestra `server local`（memory repository/queue、local storage），端口 18086；向 core 注入 `ONTOAXIS_KESTRA_URL=http://kestra:8080`、`ONTOAXIS_PIPELINE_DBT_CATALOG=memory`、`ONTOAXIS_PIPELINE_DBT_IMAGE=ontoaxis/pipeline-dbt:local`、`ONTOAXIS_ENVIRONMENT=local`。

## 已验证
- `docker compose -f deploy/compose/compose.yaml -f deploy/compose/pipeline-kestra/compose.pipeline.yaml config -q` 通过。

## 未验证（阻塞）
- Kestra 镜像体积大，`kestra/kestra:latest` 拉取超过 2 分钟未完成；本 profile 使用固定 `v0.24.0`，尚未实际 build/pull/启动。
- dbt 镜像尚未构建；Kestra 版本与 HTTP adapter 端点/`io.kestra.plugin.scripts.shell.Commands` 兼容性未确认。
- 4.5 仅完成静态 compose 片段，5.3/6.1/6.2 真实 E2E 仍未通过。
