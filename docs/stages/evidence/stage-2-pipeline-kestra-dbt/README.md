# stage-2 pipeline-kestra-dbt 证据交接

日期：2026-11-23
来源 change：`openspec/changes/pipeline-kestra-dbt/`（69/69）
执行人：stage-2-integration / pipeline-kestra-dbt

## 环境

- macOS x86_64，Docker 27.0.3，Compose 2.28.1
- Kestra `kestra/kestra:v2.0.12@sha256:b521aff2847161c283450d3c224523b42ea97447d86bde9519576c393d914b76`
- dbt-core 1.12.7 + dbt-trino 1.8.0
- Trino `memory` catalog；Postgres 15433；core 18080；metrics 19090；Kestra host 18086（容器内 8080）
- 启动命令：

```bash
KESTRA_BASIC_AUTH_USERNAME=admin@kestra.io \
KESTRA_BASIC_AUTH_PASSWORD=<local-only> \
docker compose -f deploy/compose/compose.yaml \
  -f deploy/compose/authz/compose.authz.yaml \
  -f deploy/compose/pipeline-kestra/compose.pipeline.yaml up -d --build
```

## 文件

| 文件 | 内容 |
| --- | --- |
| `2026-11-23-kestra-v2-upgrade-e2e.md` | v2 升级、13.2–13.7 全量 E2E 结论与命令 |
| `2026-11-23-pipeline-publish-compensation-unit.md` | 5.3/12.3/12.4 单测与补偿证据 |
| `raw/13.2-run.json` | 输出/血缘 pipeline 触发响应 |
| `raw/13.3-bad-run.json` | 坏 SQL 触发响应与失败态 |
| `raw/13.4-publish-503.json` | Kestra 停机 publish 503 |
| `raw/13.4-publish-retry.json` | 恢复后 publish 重试 200 |
| `raw/13.4-trigger-503.json` | Kestra 停机 trigger 503 |
| `raw/13.4-trigger-retry.json` | 恢复后 trigger 重试 202 |
| `raw/13.5-nonterminal-status.json` | 非终态 run 持久化状态 |
| `raw/13.6-*.json` | 负向 HTTP 状态与错误体 |
| `raw/2026-11-23-metrics-trace.txt` | `/metrics` 与 Jaeger trace 摘录 |
| `raw/kestra-7T6svmCkTcKdh6mvZErDus-run_results.run.json` | dbt run 模型级结果 |
| `raw/kestra-7T6svmCkTcKdh6mvZErDus-run_results.test.json` | dbt test 产物（args.which=test） |
| `raw/kestra-7T6svmCkTcKdh6mvZErDus-run_results.json` | Kestra 最后生成的 dbt 产物副本 |

## 结果摘要

- 成功路径：pipeline `01a205e4-d2a6-741a-a357-fe02457b29db`（v7）与输出 pipeline `01a205f5-7a43-7da5-8470-65a5e81c979e` 均 `succeeded`；Trino 表 `memory.oa_t11111111_p01a205f*` 可查；dbt 任务实际执行 `dbt run` + `dbt test`，run_results 对账 `total=1 success=1`。
- 输出/血缘：`pipeline.run_outputs` 有 dataset/model/relation 三元组。
- 失败路径：坏 SQL run `failed`、无 run_outputs、错误脱敏。
- 重试：Kestra 停机 503、恢复后版本只 +1，trigger 只产生 1 个 execution。
- 状态回写：停机后查询已落库 `succeeded`/`running`，不伪造。
- 负向：跨租户 404、非法 DAG/缺失 dataset 400、幂等异体 409。
- 遥测：指标/trace 无 tenant/凭据 label。

## 未覆盖

- dataset 输出的 API 查询面（当前经 `pipeline.run_outputs` DB + Trino 验证）。
- 生产级并发/大表/多租户 HTTP 隔离。
- 13.8 仅完成证据移交；归档/Stage 退出需负责人确认。
