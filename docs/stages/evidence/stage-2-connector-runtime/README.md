# connector-runtime Stage 2 证据索引（2026-11-23）

> 本目录是 `connector-runtime` 在 Stage 2 的证据入口，供归档评审与 integration 复跑使用。
> 运行环境：本地 Docker Compose `connector-runtime` profile + 合成数据（源表 `public.customers` 5 行），**非生产、非真实客户数据**。
> 详细原始记录保留在 `openspec/changes/connector-runtime/evidence/`；本文件只做索引与限制说明，不替代原始证据。

## 关键结论

| 项 | 结果 | 证据 |
| --- | --- | --- |
| 真实 CDC 链路 | Kafka Connect/Debezium → Iceberg sink → Trino；源表 5 = sink `verified_rows` 5 = Trino 5 | `openspec/changes/connector-runtime/evidence/2026-11-23-cdc-e2e-three-way.md` |
| 作业终态 | `01a20826-192c-7945-8dca-b887e58f5d74`、`01a20854-1756-7732-889a-2198c226a600` 均 `succeeded` | 同上 |
| 负向矩阵 | 默认 18/18；`--with-outage` 10/10；paused/error 为 403（Cedar `resource_state_denied`） | `2026-11-23-negative-matrix-6-3.md`、`2026-11-23-cancel-and-negative-matrix.md` |
| 集成测试 | 3 个真实 Connect 实例用例通过 | `2026-11-23-integration-and-telemetry.md` |
| 遥测 | Prometheus 指标序列与 Jaeger span 已抓取归档 | `2026-11-23-connector-metrics.txt`、`2026-11-23-jaeger-trace-sample.json` |
| 门禁 | build/vet/lint/test/test-race/deps-check/`openspec validate --all --strict` 通过 | `2026-11-23-connector-runtime-archive-review.md` |

## 复跑命令

```bash
# 起栈（执行前按 RUNBOOK §7.1 判级；本地 profile，L0/L1）
docker compose -f deploy/compose/compose.yaml \
  -f deploy/connector-runtime/compose.connector-runtime.yaml \
  --profile connector-runtime up -d --build

# 成功链路 + 三方行数
./scripts/connector-runtime-e2e.sh --success-only

# 默认负向矩阵（18 项）
./scripts/connector-runtime-e2e.sh

# 追加 Connect 停机/恢复（10 项）
./scripts/connector-runtime-e2e.sh --with-outage

# 集成测试
go test -tags=integration ./internal/modules/connectors/... ./internal/modules/ingestion/...
```

## 证据可用性说明

- `2026-11-23-cdc-e2e-three-way.md` 正文引用的 `2026-11-23-success-e2e-5-3.log` 未提交到仓库：`*.log` 默认被忽略，只有 `docs/stages/evidence/**/*.log` 例外允许。需要原始日志时按上面的命令重跑生成，关键断言（job id、dataset 命名空间、5=5=5）已写入该证据正文。
- 本目录不复制这些原始文件，避免出现两份可能不同步的副本。

## 限制与未覆盖

- 主键/upsert 未实现：当前 append 模式，无 identifier；重启/重放语义未验证。
- 告警规则未做本地 firing 验证，只验证了指标存在与查询。
- 真实第二租户 E2E 不可行（本地单租户栈）；跨租户用「伪造 `x-ontoaxis-tenant-id` → 403」与单测覆盖。
- `scripts/cleanup.sh` 未在 E2E 矩阵中调用（销毁性 teardown），矩阵用幂等复跑与独立清理替代。
- Connect 持久化连接器配置仍含密码，凭据外部化属 Stage 3。
