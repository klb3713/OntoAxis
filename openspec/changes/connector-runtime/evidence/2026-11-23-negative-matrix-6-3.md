# 6.3 失败与负向矩阵记录（2026-11-23）

## 结论

6.3 列出的场景都有断言输出或对应测试，默认矩阵 `RESULT pass=18 fail=0`。paused/error 的 HTTP 码经负责人确认按实际 403 验收：Cedar 状态策略 fail-closed 拒绝，规格只要求「被拒绝」。见文末说明。

## 场景与证据

| 场景 | 期望 | 实测/覆盖 | 位置 |
| --- | --- | --- | --- |
| Connect 不可达 | 503，不伪造作业 | 503；恢复后 REST 200 | `connector-runtime-e2e.sh --with-outage`（`RESULT pass=10 fail=0`） |
| 非法同步配置 | 400 | 400（创建期宽松、同步期严格） | 默认矩阵 |
| paused 连接器启动同步 | 拒绝 | 403（`resource_state_denied`） | 默认矩阵 |
| error 连接器启动同步 | 拒绝 | 403 | 默认矩阵 |
| 提交失败（源库主机不可达） | 作业 `failed` 且脱敏 | 作业 `failed`；作业与 run 文本不含口令/连接串 | 默认矩阵 |
| 重试耗尽 | 作业 failed | `TestDispatchRetryExhaustionMarksJobFailed` | `internal/modules/ingestion/runtime/dispatcher_test.go` |
| 取消传播 | 停运行时并置 cancelled | 200 + `cancelled`，Connect 连接器被删除 | 默认矩阵 |
| 重复启动幂等 | 同一 job | 201，返回同一 job id | 默认矩阵 |
| 跨租户读取 | 404/拒绝 | 单测：`ingestion/api/authz_test.go`（otherTenant → ErrNotFound）、`ingestion/service/cancel_test.go`（跨租户 `ErrNotFound`）；E2E：伪造 `x-ontoaxis-tenant-id` → 403 | 单测 + 默认矩阵 |

## 复跑命令

```bash
./scripts/connector-runtime-e2e.sh                # 默认矩阵（18 项）
./scripts/connector-runtime-e2e.sh --with-outage  # 追加 Connect 停机/恢复
```

## 说明：paused/error 按 403 验收（原任务文本写 409，2026-11-23 负责人确认改为 403）

- 连接器状态更新成功（200），随后 `POST /api/v1/connectors/{id}/sync` 在授权层被 Cedar 的 `platform/resource-state.cedar` 拒绝（`resource_state_denied`），HTTP 映射为 403。
- `openspec/specs/connectors/spec.md` 只要求 paused/error 的同步被拒绝，没有规定状态码；409 是 tasks 里的实现备注。
- 当前 fail-closed 策略优先于服务层冲突语义：若要严格返回 409，需要把状态检查下沉到 ingestion/connectors service 并调整 Cedar 策略，属于授权行为变更，需单独评审。
- 本记录按实际 403 归档，不把任务文本的 409 写成已实现。
