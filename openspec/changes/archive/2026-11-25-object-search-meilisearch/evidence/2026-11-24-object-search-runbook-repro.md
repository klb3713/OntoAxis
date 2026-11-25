# 运维手册命令复现记录（2026-11-24）

- 时间：2026-11-24T21:19:44+0800（Asia/Shanghai）
- 目的：验证 `docs/operations/object-search-runbook.md` §3 迁移、§4 重建、§5 重放、§12 本地验证的命令可直接复现，为 9.1 提供“命令可复现”证据（评审签字仍待评审人）。
- 环境：临时容器，仅绑定回环，用后删除，未触碰既有栈
  - PostgreSQL：`postgres:16-alpine`，`127.0.0.1:55432`，库/用户 `ontoaxis`
  - Meilisearch：`getmeili/meilisearch:v1.11`，`127.0.0.1:17701`，`MEILI_MASTER_KEY=local-test-master-key`
  - CLI：`go build -o /tmp/ontoaxis-search ./cmd/ontoaxis-search`

## §3 迁移

| 命令 | 退出码 | 关键输出 |
| --- | --- | --- |
| `/tmp/ontoaxis-search migrate` | 0 | `{"msg":"search schema migrated"}` |
| `/tmp/ontoaxis-search migrate`（重复） | 0 | `{"msg":"search schema migrated"}`（幂等） |
| `psql -c '\dt search.*'` | 0 | `search.index_state`、`search.outbox_attempt` 两表存在 |

## §4 重建

- 裸库（仅执行 `migrate`）直接 `rebuild --tenant <uuid> --verify`：退出码 1，报
  `ontoaxis-search: search rebuild: scan objects: ERROR: relation "object_data.objects" does not exist (SQLSTATE 42P01)`。
  根因：`ontoaxis-search migrate` 只建 `search.*`，业务表需由核心服务先迁移。已在手册 §3 增补“前置条件”。
- 带真实数据、经真实 CLI 的整租户重建：见集成测试 `TestIntegrationRebuildCLIExitAndDigest`
  （`internal/platform/search/rebuild_cli_integration_test.go:24`）：构建真实 CLI 连续执行两次
  `rebuild --tenant <uuid> --verify`，均退出 0，`verified=true`、`documents=1`、`digest` 为 64 位十六进制且两次相同；
  随后向真实 Meilisearch 注入“同数量内容漂移”，CLI 对账非零退出且不打印文档内容。该用例在 §12 集成测试中通过。

## §5 死信重放

| 命令 | 退出码 | 关键输出 |
| --- | --- | --- |
| `/tmp/ontoaxis-search replay --limit 5` | 0 | `{"msg":"search dead-letter replay complete","events":0}` |

## §12 本地可复现验证（CI 同参）

- 命令与 `.github/workflows/ci.yml` 的 `search-integration` job 一致：
  `ONTOAXIS_SEARCH_TEST_REQUIRED=1` + `ONTOAXIS_SEARCH_TEST_DATABASE_URL`/`_MEILISEARCH_URL`/`_API_KEY`，
  `go test -tags=integration -count=1 -p 1 -timeout 15m -v ./internal/platform/search/`。
- 结果：退出 0，`PASS`，`--- SKIP` 计数 0（REQUIRED=1 时不跳过），包内 60 个通过项（含子测试），
  其中顶层集成用例 12 个（迁移 up/down/up、租户隔离、重建 keyset 分页、swap 保留旧索引、窗口重放、
  `--verify` 漂移检测、失败清理暂存、单类型保留其他类型、摘要稳定、CLI 退出码与 digest 等）。
- 完整日志：`evidence/2026-11-24-search-integration-ci-local.log`。
- 说明：本地等价于 CI job 的命令已通过，但**尚无 GitHub Actions run 链接**（本机无 runner），8.2 仍需在 CI 实跑后回填链接。

## 结论

- 手册 §3/§4/§5/§12 命令可复现；§4 需先具备业务库 schema（已补前置条件说明）。
- 9.1 剩余唯一未满足项是“评审通过”（评审人签字），非命令问题。
