# pipeline-kestra-dbt 发布补偿与故障注入单测（2026-11-23）

## 范围

本次只补 5.3、12.3、12.4 中**不依赖真实 Kestra** 的部分：

- 5.3 发布补偿：DB 提交失败、Flow 注册失败、namespace 文件上传中途失败时回退。
- 12.3 故障注入：文件上传中途失败、DB 提交失败、触发响应丢失（既有 6.2 测试）。
- 12.4 降级不伪造：未配置/不可达 503；引擎恢复后重试只成功一次。

真实 Kestra UAT/E2E（0.2、8.3、13.x）仍受 D-02 阻塞，未在本次证据中声明通过。

## 代码

- `kestra.Adapter` 新增 `DeleteFlow`；`MemoryAdapter` 与 `HTTPAdapter` 实现，HTTP 删除 404 幂等。
- `service.Repository` 新增 `GetPublishedVersion`；Memory/Postgres 实现。
- `service.PublishPipeline` 新增 `failPublish/rollbackPublish`：
  - 首次发布失败：删除新 Flow + 新文件；
  - 已有版本失败：读取上一版本快照，重建并重新登记上一版 Flow，恢复上一版 dbt 文件，删除新版本多出的路径；
  - 补偿失败：`slog.Warn("pipeline publish compensation failed", ...)`，仍返回原失败（`ErrUnavailable`）。
- `outboundOperation` 将 DELETE flow 记为 `flows.delete`。

## 测试

文件：`internal/modules/pipelines/service/publish_compensation_test.go`

| 测试 | 覆盖 |
|---|---|
| `TestPublishFailureRestoresPreviousFlowAndFiles` | DB 失败 → 回退 Flow 到 v1、版本不变、返回 `ErrUnavailable` |
| `TestFirstPublishFailureDeletesFlowAndFiles` | 首次发布 DB 失败 → 删除 revision 1 Flow + 文件 |
| `TestPublishFilesPartialFailureRestoresPreviousProject` | 文件上传中途失败 → 恢复上一版项目文件与 Flow |
| `TestPublishRecoversAfterEngineFailureOnlyOnce` | 引擎第一次失败，重试成功且版本只递增一次 |
| `TestPublishCompensationFailureIsWarnedAndStillFails` | 补偿自身失败 → 告警且仍返回失败 |
| `TestHTTPAdapterDeleteFlow` | DELETE Flow 路径与 404 幂等 |

命令与结果：

```text
go test ./internal/modules/pipelines/...        ok
go test -race ./internal/modules/pipelines/...  250 passed in 8 packages
go test ./...                                   1216 passed in 96 packages
go build ./...                                  ok
go vet ./...                                    ok
openspec validate --all --strict                19 passed, 0 failed
```

## 仍未完成

- 12.3 的“集成重试 E2E”仍依赖真实 Kestra。
- 12.4 的“集成断言”仍依赖真实 Kestra。
- 0.2/0.3/8.3/13.1–13.8 未勾选；阻塞见 `docs/stages/evidence/stage-2-integration/defects/index.md` D-02。

## 12.3/12.4 集成补充（2026-11-23）

- 12.3 故障注入：`publish_compensation_test.go` 覆盖 `PublishFiles` 部分失败、DB publish 失败、首发布/后续发布补偿；`TestPublishFailureRestoresPreviousFlowAndFiles` 断言返回 `ErrUnavailable` 且旧 Flow/版本保留。响应丢失由 `TestTriggerLostResponseAdoptsExecution`（6.2）覆盖。
- 12.4 集成：`evidence/2026-11-23-kestra-v2-upgrade-e2e.md` 13.4——Kestra 不可达 publish 503 且 version 不变；恢复后同一请求重试 version 只 +1；trigger 503 时 run 落 `failed` 且无 Kestra execution，恢复重试后新 run `succeeded` 且 execution 恰好 1 个。
