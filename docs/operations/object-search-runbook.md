# 对象检索（Meilisearch）运维手册

适用组件：`ontoaxis-search` worker（`cmd/ontoaxis-search`）、`internal/platform/search`、Meilisearch、`jobs.outbox` →
`search.*` 索引链路。对应 OpenSpec change `object-search-meilisearch` 任务 9.1。

> 所有命令均为**本地/合成数据**验证过的形态；生产执行前仍需按 `docs/workflow/RUNBOOK.md` §7.1 判级并走 L2 确认。
> 手册不包含任何真实凭据；示例中的 master key 仅用于本地 Compose。

## 1. 组件与配置

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ONTOAXIS_SEARCH_ENABLED` | `false` | 关闭时 core 搜索返回 503，CRUD 不受影响 |
| `ONTOAXIS_SEARCH_URL` | 空 | Meilisearch 基址，如 `http://meilisearch:7700` |
| `ONTOAXIS_SEARCH_API_KEY` | 空 | Meilisearch API key（生产由 Secret 注入） |
| `ONTOAXIS_SEARCH_TIMEOUT` | `1500ms` | 搜索请求超时（≤10s） |
| `ONTOAXIS_SEARCH_INDEX_VERSION` | `v1` | 索引版本，格式 `v<数字>` |
| `ONTOAXIS_SEARCH_SYNC_WINDOW` | `5s` | 重建窗口/等待 task 的时间基准（1–60s） |
| `ONTOAXIS_SEARCH_POLL_INTERVAL` | `500ms` | consumer 轮询间隔（100ms–10s） |
| `ONTOAXIS_SEARCH_CONSUMER_BATCH` | `50` | 单事务 claim 批量（1–500） |
| `ONTOAXIS_SEARCH_CONSUMER_MAX_ATTEMPTS` | `5` | 失败超过即进入 dead letter（1–20） |
| `ONTOAXIS_SEARCH_HTTP_ADDR` | `:8081` | worker `/healthz`、`/readyz` |
| `ONTOAXIS_SEARCH_METRICS_ADDR` | `:9094` | worker `/metrics` |
| `ONTOAXIS_DATABASE_URL` | 空 | worker 必填，PostgreSQL DSN |
| `ONTOAXIS_LOG_LEVEL` | `info` | `debug`/`warn`/`error` |

索引命名：`objects_<version>_<sha256(tenantUUID) 前 16 位>`；重建暂存索引：`<indexUID>_r<Unix 纳秒>`。
每个租户独立索引；所有查询强制附加 `tenant_id` filter。

## 2. 健康检查

```bash
# worker
curl -fsS http://127.0.0.1:18088/healthz   # {"status":"ok"}
curl -fsS http://127.0.0.1:18088/readyz    # {"status":"ready"}；PG 或 Meili 不可用时 503

# Meilisearch
curl -fsS http://127.0.0.1:17700/health    # {"status":"available"}

# 端到端（需要租户 token）
curl -fsS -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:18080/api/v1/objects/search?q=test&limit=1'
```

`/readyz` 依次检查 PostgreSQL ping、Meilisearch `/health`；任一失败返回 503 并标明 `dependency`。
搜索入口不可用时的预期行为：`GET /api/v1/objects/search` 返回 503 `object search is unavailable`（可重试），
对象 CRUD 仍返回 2xx（design D6）。

## 3. 迁移

```bash
# Compose（本地）
docker compose -f deploy/compose/compose.yaml -f deploy/compose/search/compose.search.yaml \
  --profile search run --rm search-worker migrate

# 裸进程
ONTOAXIS_DATABASE_URL='postgres://ontoaxis:ontoaxis@127.0.0.1:55432/ontoaxis?sslmode=disable' \
  ./ontoaxis-search migrate
```

> **前置条件（裸进程/迁移顺序）**：`ontoaxis-search migrate` 只创建 `search.index_state` 与
> `search.outbox_attempt`（可重复执行，`CREATE ... IF NOT EXISTS`）。`consume`/`rebuild` 还要读取
> `object_data.objects` 等业务表，必须先由核心服务完成业务库迁移；否则直接 `rebuild` 会报
> `search rebuild: scan objects: ERROR: relation "object_data.objects" does not exist (SQLSTATE 42P01)`。
> 用 Compose 时核心栈会先迁移业务库；裸进程场景需先执行 `ontoaxis-core` 的迁移（或先起核心栈）。

验证：

```bash
psql "$ONTOAXIS_DATABASE_URL" -c "\dt search.*"
psql "$ONTOAXIS_DATABASE_URL" -c "\d search.outbox_attempt"
psql "$ONTOAXIS_DATABASE_URL" -c "\d search.index_state"
```

迁移是幂等的（`CREATE ... IF NOT EXISTS`），重复执行安全。**不要**在生产执行 goose Down；Down 仅用于本地
`go test -tags=integration` 的 up→down→up 验证（见 `internal/platform/search/integration_test.go`）。

## 4. 重建索引

```bash
# 整租户重建 + 一致性校验（生产推荐；staging → swap → 窗口重放 → verify）
docker compose ... run --rm search-worker rebuild --tenant <tenant-uuid> --verify

# 单对象类型修复（直接改线上索引：upsert 该类型 + 删除 stale id + 重放 + verify）
docker compose ... run --rm search-worker rebuild --tenant <tenant-uuid> --object-type <type-uuid> --verify
```

判读：

- 对账成功时 CLI 退出码为 0；JSON 日志含 `"verified":true`、`digest`（64 位十六进制）、`documents`、`index_uid`、`duration`。`documents` 是重建扫描写入数，窗口内的变更可能在随后重放时才计入对账。
- `--verify` 在 PostgreSQL 只读快照上重建预期投影，并分页读取 Meilisearch 的完整文档，不使用 `estimatedTotalHits`。按文档 ID 排序后比较数量与 SHA-256 摘要。计数不同报 `document count mismatch`；数量相同但内容不同报 `document digest mismatch`，退出码非 0，不打印文档内容。
- 两个后端不共享同一快照。活跃写入期间的对账失败需先查 §8、等待消费者追平，再在稳定窗口重试；不要把并发写入时的单次失败直接判为数据丢失。
- 整租户重建期间旧索引继续可查；swap 后旧索引（暂存名 `_r*`）保留一版用于回退，确认后可删除（§7）。
- **不要**在生产用 `--object-type` 代替整租户重建：它不经过 staging/swap，只适合单类型紧急修复。
- 同一租户同一时刻只跑一个重建（`search.index_state` 只记录最后结果，并发重建会互相覆盖）。

## 5. 失败事件与死信重放

```sql
-- 待处理/重试中
SELECT id, tenant_id, event_type, created_at
FROM jobs.outbox WHERE published_at IS NULL ORDER BY created_at LIMIT 20;

-- 重试次数/下次重试/错误
SELECT * FROM search.outbox_attempt WHERE dead_at IS NULL ORDER BY next_attempt_at LIMIT 20;

-- 死信
SELECT event_id, attempts, last_error, dead_at
FROM search.outbox_attempt WHERE dead_at IS NOT NULL ORDER BY dead_at DESC LIMIT 50;
```

处理步骤：

1. 先修复根因（Meili 不可达、映射/设置错误、数据异常），否则重放会再次进入死信。
2. 重放（清 `dead_at`/`attempts`，把对应 outbox 重新置为未发布）：

   ```bash
   docker compose ... run --rm search-worker replay --limit 100
   ```

3. 观察 worker 日志与 `jobs.outbox.published_at`，确认积压下降。
4. 若死信来自未知事件类型，consumer 会把未知事件直接标记 published（不索引）；核对事件契约后再决定是否重建。

## 6. 降级与恢复

**Meilisearch 停机**

- 现象：搜索 503；worker `/readyz` 503；outbox 继续累积（CRUD 正常）。
- 处置：恢复 Meilisearch → 等 `/health` 200 → worker 在下一轮轮询继续消费；无需手工补数据。
- 若停机超过 `CONSUMER_MAX_ATTEMPTS * backoff`（默认 1s/2s/4s/8s/16s 后进入死信），按 §5 重放。

**worker 停机**

- outbox 只增不减；重启后继续消费。重建/迁移由独立 Job 执行，不依赖长期运行的 worker。

**core 侧关闭搜索**

- 设 `ONTOAXIS_SEARCH_ENABLED=false` 并滚动 core；搜索返回 503，CRUD 与 outbox 写入不受影响。
- 重新开启后，先确认 worker 与 Meili 已 Ready，再放开 core。

## 7. 索引清理

```bash
# 列出索引
curl -sS -H "Authorization: Bearer $MEILI_API_KEY" 'http://<meili>:7700/indexes?limit=1000' \
  | jq -r '.results[].uid'

# 删除某个暂存/旧版本索引（确认无流量后）
curl -sS -X DELETE -H "Authorization: Bearer $MEILI_API_KEY" \
  'http://<meili>:7700/indexes/<index-uid>'

# 查看索引状态记录
psql "$ONTOAXIS_DATABASE_URL" -c \
  "SELECT tenant_id, index_version, index_uid, last_success_at, rebuilt_at FROM search.index_state ORDER BY tenant_id;"
```

- 可删：确认重建成功且观察期结束后的 `objects_<旧版本>_*`；失败重建遗留的 `_r*`（先确认没有正在运行的重建）。
- 不可删：当前 `ONTOAXIS_SEARCH_INDEX_VERSION` 对应的索引；正在 swap/回退保护期内的上一版索引。
- `Client.DeleteIndex` 对 404 视为成功，但**不等待**删除 task；删除后若立即重建同名索引，先轮询 `/tasks` 确认删除完成。

## 8. 常见故障

| 现象 | 可能原因 | 处置 |
| --- | --- | --- |
| 搜索返回 400 `invalid_search_filter` | 索引 settings 未应用（filterable 缺字段）；consumer 未 EnsureIndex | 确认 worker 使用 `ClientApplier`（含 `EnsureIndex`）；重建该租户索引；检查 `mapping.SettingsJSON()` |
| `rebuild` 报 wait task timeout | Meili 慢/不可达；`SYNC_WINDOW` 太小 | 查 Meili `/tasks` 与 `/health`；临时调大 `ONTOAXIS_SEARCH_SYNC_WINDOW`（≤60s） |
| `--verify` 计数或摘要不一致 | 窗口内事件未重放完；索引文档陈旧；DB 与索引内容不一致 | 查 `jobs.outbox` 残留、`search.index_state.last_event_at`；修根因后重跑整租户重建 |
| `rebuild --object-type` 后其他类型不见 | 旧版本缺陷（已修复：此前 type 重建会 swap 到整租户索引） | 确认运行的是修复后镜像；对受影响租户执行整租户重建 |
| outbox 长期积压 | worker 停止/频繁失败；Meili 不可达 | 查 `/readyz`、`dead_letters_total`、`outbox_attempt.last_error` |
| 中文检索召回差 | 分词/同义词/字段权重；`properties_text` 截断 | 用 `scripts/search-quality-baseline.sh` 做技术基线；阈值需负责人签核，不得自行判定合格 |
| 删除索引后立即重建失败 | 删除 task 未完成（404 视为成功但异步） | 轮询 `/tasks` 至删除 task 成功后再建 |

## 9. 指标与告警建议

| 指标 | 含义 | 建议告警 |
| --- | --- | --- |
| `ontoaxis_search_index_events_total{result="success\|retry\|dead"}` | 事件处理结果 | `retry` 持续增长 / `dead` > 0 |
| `ontoaxis_search_index_lag_seconds` | 事件创建到处理完成的时延 | p95 > 60s 持续 10m |
| `ontoaxis_search_index_batch_duration_seconds` | 单批耗时 | p95 > 5s |
| `ontoaxis_search_dead_letters_total` | 死信累计 | 5m 内增长 > 0 |
| `ontoaxis_search_rebuild_documents_total{result}` | 重建文档数 | 重建任务失败或 0 文档 |
| `ontoaxis_search_rebuild_duration_seconds` | 重建耗时 | 超过租户规模预算 |
| worker `/readyz` | PG/Meili 健康 | 连续 3 次 503 |
| `jobs.outbox` 未发布计数（SQL） | 消费积压 | > 1000 或 10m 不下降 |

## 10. 映射/索引版本升级（mapping 变更）

1. 改 `mapping.SettingsJSON()`/`BuildDocument` 的字段或分词设置 → 必须升 `ONTOAXIS_SEARCH_INDEX_VERSION`（如 `v1`→`v2`），
   不要原地覆盖当前版本索引。
2. 滚动 worker 与 core，使新版本配置生效（worker 消费会写入 `objects_v2_*` 索引）。
3. 对每个租户执行 `rebuild --tenant <uuid> --verify` 建立 v2 索引；观察 `verified=true`。
4. 切换读取：core 的 `ONTOAXIS_SEARCH_INDEX_VERSION=v2` 滚动完成后，搜索走新索引。
5. 观察期（建议 ≥1 个业务日）后删除 v1 索引与 `search.index_state` 中对应旧版本行；保留 v2 行。
6. 回退：把版本改回 v1 并滚动 core/worker；v1 索引在删除前仍可查。重建窗口内的事件会被 replay 到当前版本索引。

## 11. 安全与数据

- Meilisearch master key 仅存于 Secret；日志、工单、evidence 不记录 key 明文。
- 索引文档不包含敏感属性：`password/passwd/secret/token/api_key/credential/private_key` 命中的整棵属性子树被排除
  （`mapping.FlattenProperties`），`properties_text` 按 4096 字节截断。
- 租户隔离依赖索引命名 + `tenant_id` filter；不要手工执行不带 filter 的跨租户搜索作为“运维便利”。
- 重建/迁移使用只读/受限账号时，确认 `jobs.outbox` 的写权限仅授予 worker。

## 12. 本地可复现验证

```bash
# 集成测试（真实 PostgreSQL + Meilisearch；未注入变量时 skip）
ONTOAXIS_SEARCH_TEST_DATABASE_URL='postgres://ontoaxis:ontoaxis@127.0.0.1:55432/ontoaxis?sslmode=disable' \
ONTOAXIS_SEARCH_TEST_MEILISEARCH_URL='http://127.0.0.1:17701' \
ONTOAXIS_SEARCH_TEST_API_KEY='local-test-master-key' \
  go test -tags=integration -count=1 -p 1 -timeout 15m -v ./internal/platform/search/

# 端到端矩阵（起独立 Compose 栈，含降级场景）
./scripts/object-search-e2e.sh

# 检索质量技术基线（阈值待签核）
MEILI_URL=http://127.0.0.1:17700 MEILI_API_KEY=local-search-master-key \
  ./scripts/search-quality-baseline.sh
```

证据记录要求：命令、时间（含时区）、退出码、关键输出摘要；未执行项写 `not executed`，skip 不得写成 pass。