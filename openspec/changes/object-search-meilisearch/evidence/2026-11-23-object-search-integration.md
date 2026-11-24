# 证据：object-search 真实实例集成测试与交付准备

- 日期：2026-11-23（Asia/Shanghai）
- change：`object-search-meilisearch`
- 环境：本地 Docker 上的 PostgreSQL 16（`127.0.0.1:55432`，user/pass/db=`ontoaxis`）与
  Meilisearch 1.12（`127.0.0.1:17701`，master key 为本地测试 key，不写入本文件/仓库）。
  二者均为**合成测试数据**，非生产实例、非真实客户数据。
- 执行者：osm-worker（agent），由 lead 复跑确认。

## 1. 集成测试（真实 PostgreSQL + Meilisearch）

命令（在仓库根目录）：

```bash
ONTOAXIS_SEARCH_TEST_DATABASE_URL='postgres://ontoaxis:***@127.0.0.1:55432/ontoaxis?sslmode=disable' \
ONTOAXIS_SEARCH_TEST_MEILISEARCH_URL='http://127.0.0.1:17701' \
ONTOAXIS_SEARCH_TEST_API_KEY='***' \
go test -tags=integration -p 1 -count=1 -timeout 15m -v ./internal/platform/search/
```

结果：**PASS，exit code 0**。osm-worker 首跑 `ok ... 6.359s`；lead 复跑 `ok ... 6.518s`，
11 个集成用例全部通过（与既有单测同包执行）：

| 用例 | 覆盖任务 | 断言摘要 |
| --- | --- | --- |
| `TestIntegrationMigrationUpDownUp` | 1.4 | `search.outbox_attempt`/`search.index_state` 列与主键存在；执行 down 后 `search` schema 表数 0；再次 up 两次仍一致 |
| `TestIntegrationClaimConcurrent` | 4.1 | 消费者 A 事务内领取并阻塞时，消费者 B 的 `RunOnce` 15s 内返回 0（`SKIP LOCKED` 不重复/不阻塞）；A 回滚后事件可再次领取；最终文档恰好 1 条、outbox 全部 `published_at` |
| `TestIntegrationConsumerAppliesAndPublishes` | 4.2/4.3 回归 | create→update→soft-delete 后 Meili 文档数 0，outbox 全 published |
| `TestIntegrationRebuildKeysetPagination` | 5.2 | 1100 行 keyset 分页重建，`Summary.Documents=1100`，索引计数一致，分页取 id 无重复无遗漏 |
| `TestIntegrationRebuildSwapKeepsPreviousIndex` | 5.3 | staging 写入被 gate 暂停期间线上索引仍可查（3 条）；放行后 swap，索引 4 条且 verified；旧 `_r` 暂存索引保留旧 3 条可查 |
| `TestIntegrationRebuildReplaysWindowEvents` | 5.4 | gate 期间插入排序游标之前的事件，放行后 replay 生效，最终 4 条含新 id |
| `TestIntegrationRebuildVerifyDetectsDrift` | 5.5 | 一致时 `Verify` 返回 nil；删除一条索引文档后返回错误，含 `document count mismatch`、`database=2`、`index=1` |
| `TestIntegrationRebuildFailureCleansStaging` | 5.6 | 注入 staging upsert 失败：线上索引仍 3 条、新 staging 在 10s 内被删除、重试可继续 |
| `TestIntegrationRebuildObjectTypePreservesOtherTypes` | 5.1 + 缺陷修复 | 整租户重建 4 条；仅 typeA 重建后 `Summary.Documents=2`、verified，typeB 2 条保留，已删除的 typeA 文档从索引移除 |
| `TestIntegrationTenantIsolation` | 5.1/租户隔离 | 两租户索引名不同且不含原始 UUID；A 索引用 B 的 tenant filter 查询为 0；settings `filterableAttributes` 含 tenant_id/object_type_id/updated_at，`displayedAttributes` 不含 properties_text |
| `TestIntegrationRebuildSummaryStable` | 5.7 | 25 条连续重建两次，`Documents`/`IndexUID`/`Verified` 一致，两次 id 集合完全一致 |

本地未注入 `ONTOAXIS_SEARCH_TEST_*` 时该文件整体 `t.Skip`（见文件头）；CI 设置
`ONTOAXIS_SEARCH_TEST_REQUIRED=1` 使缺失变量直接 Fatal。

## 2. 实现缺陷与修复（评审重点）

`internal/platform/search/rebuild.go` 原实现中，`rebuild --object-type` 仍走
「staging 索引 → 按类型扫描 → swap 到整租户索引」，会**用单一类型的数据覆盖整租户索引**，并丢失其他类型；
`verify` 的数据库计数按类型过滤、Meili 查询只按租户过滤，必然 mismatch。

修复（2026-11-23）：

- `Rebuild` 在 `objectTypeID != nil` 时改走 `rebuildObjectType`：直接对线上索引按类型 upsert，
  先快照既有该类型文档 id、删除数据库中已不存在的 stale id，再 replay 窗口事件、记录 `index_state`、可选 verify；
- 新增 `indexedObjectIDs`（按类型分页读取线上 id，超过 `maxTotalHits` fail closed）；
- `indexTenantObjects` 增加 `seen` 参数供 type 路径去重；
- `verifyTenantCount` 按类型追加 `object_type_id` filter。

修复后单测与集成测试均通过；集成用例 `TestIntegrationRebuildObjectTypePreservesOtherTypes` 专门守护该行为。
生产重建仍建议不带 `--object-type`（见运维手册与 Helm 要求文档）。

## 3. 其他验证

| 项 | 命令 | 结果 |
| --- | --- | --- |
| 构建/静态检查 | `go build ./... && go vet ./...` | exit 0 |
| 单测（search 相关） | `go test -count=1 ./internal/platform/search/... ./cmd/ontoaxis-search/...` | PASS（search 0.386s、worker 0.305s、cmd 0.502s） |
| go.mod/go.sum | `git diff --exit-code -- go.mod go.sum` | exit 0（无改动） |
| Compose profile 配置 | `docker compose -f deploy/compose/compose.yaml -f deploy/compose/search/compose.search.yaml --profile search config -q` | exit 0；无 profile 时同一命令 exit 0（`search-worker` 依赖已标 `required: false`） |
| 主 compose 无 search profile | `docker compose -f deploy/compose/compose.yaml --profile search config --services` | 不含 `meilisearch`/`search-worker`（需 production-delivery 合并，见交接） |
| 中文评测基线 | `MEILI_URL=... MEILI_API_KEY=... ./scripts/search-quality-baseline.sh`（Meilisearch 1.11.3, 2026-11-23 22:33） | 6/6 查询 recall@5=1.00、MRR@10=1.00；汇总 recall@5=1.0000、MRR@10=1.0000；exit 0。**阈值 `pending-owner-signoff`，不构成业务合格结论** |
| CI workflow YAML | `ruby -ryaml -e 'YAML.load_file(".github/workflows/ci.yml")'` | 解析成功；`search-integration` job 含 postgres/meilisearch service containers 与 `ONTOAXIS_SEARCH_TEST_REQUIRED=1` |
| change 规格校验 | `openspec validate object-search-meilisearch --strict` | `Change 'object-search-meilisearch' is valid`，exit 0 |
| 脚本语法 | `bash -n scripts/object-search-e2e.sh`、`bash -n scripts/search-quality-baseline.sh` | 均 exit 0 |

## 4. 未执行 / 未验证项（如实记录）

| 项 | 状态 | 原因 |
| --- | --- | --- |
| `scripts/object-search-e2e.sh` 全矩阵 | **not executed** | 22:30 首次运行已构建镜像并启动 postgres/meili，但卡在 search-worker `Starting`（容器 state=created）；22:34 起 Docker 容器 create/start 全局挂起（`docker run`/`docker start` 超 60s 无响应，`docker ps`/`docker exec` 正常；宿主 load ~10–12，另有并发 compose 卡住）；22:41、22:45 两次 `docker run --rm alpine:3.20 echo` 仍挂起。脚本已通过 `bash -n` 语法检查，但**没有 PASS/FAIL 记录**，不得声称 E2E 通过 |
| CI `search-integration` job | **not executed** | 本地无 GitHub Actions runner；workflow 已配置（`.github/workflows/ci.yml`），本地同命令已通过，但 CI run 未执行、无 run 链接 |
| 7.5 `up -d` 后 healthy | **not executed** | 同上 Docker 故障；仅 `config` 通过 |
| 7.6 `helm template` / kind 部署 | **not executed** | 仅产出要求文档交 production-delivery；chart 未创建（不属本 change 写入范围） |
| 9.2 安全评审 / 9.3 change 评审 / G1 | **pending** | 需评审人与负责人签字；G1 无 `pass` 记录 |
| 评测阈值签核 | **pending** | 需负责人签核，签核前不得判定业务合格 |

## 5. 相关文件

- `internal/platform/search/integration_test.go`（`//go:build integration`）
- `internal/platform/search/rebuild.go`（缺陷修复）
- `.github/workflows/ci.yml`（`search-integration` job）
- `scripts/object-search-e2e.sh`（未执行）
- `scripts/search-quality-baseline.sh`、`fixtures/search/dataset.json`、`fixtures/search/README.md`
- `deploy/compose/search/compose.search.yaml`（search profile 片段）
- `openspec/changes/object-search-meilisearch/deployment-requirements.md`
- `docs/operations/object-search-runbook.md`
- `openspec/changes/object-search-meilisearch/security-data-governance-review.md`
- `openspec/changes/object-search-meilisearch/review-wave2-admission.md`
- `openspec/changes/object-search-meilisearch/traceability.md`