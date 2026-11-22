# Stage 2 action-temporal 真实 Temporal 执行证据（2026-11-23）

## 环境与启动

```bash
docker compose \
  -f deploy/compose/compose.yaml \
  -f deploy/compose/authz/compose.authz.yaml \
  -f deploy/compose/action-temporal/compose.temporal.yaml up -d --build
```

- Core：`/readyz` 11 项 `ok`（9 业务 + authorization + database）
- Actions worker：`/readyz` 10 项 `ok`（不含 authorization；worker 不做用户授权）
- Temporal：`temporalio/auto-setup:1.26.2` + namespace `ontoaxis` 初始化；UI `localhost:18088`；worker HTTP `18087`
- 说明：worker 已接入 `ObjectEffectApplier`（objects.Service + `repo.NewOutbox`），对象 update/create/delete 与测试故障注入 activity 已实现；本轮 E2E 使用 `noop` step 验证调度/状态持久化。

## 已执行

1. 通过 Keycloak password grant 获取 admin token（realm `ontoaxis`，client `ontoaxis-web`）。
2. 创建 Action Type `e2e_noop2`，definition：
   ```json
   {"execution_definition":{"steps":[{"id":"s1","activity_type":"noop","read_only":true}]}}
   ```
3. `POST /api/v1/actions/types/{id}/run` → HTTP 200
4. 轮询 `GET /api/v1/actions/executions/{id}` → HTTP 200，最终 `status=succeeded`
5. Temporal 查询确认 workflow `Completed`。

## 关键 ID

- Action Type：`01a20455-2188-786f-87f5-0362c3bf6f6c`
- Execution：`01a20455-347c-7a50-ad85-8724dc452571`
- Workflow ID：`action-execution-01a20455-347c-7a50-ad85-8724dc452571`
- Workflow Run ID：`346b4ce3-e77d-414e-90b7-9589d379e857`
- 最终执行：`{"status":"succeeded","attempt":1,"current_step_index":0,"workflow_id":"action-execution-01a20455-347c-7a50-ad85-8724dc452571","workflow_run_id":"346b4ce3-e77d-414e-90b7-9589d379e857","approval_state":"none"}`

## 边界与未执行项

- `planned`：对象写入的完整 E2E（创建对象→action 更新→生命周期 outbox）、重试耗尽、逆序补偿、审批通过/拒绝/超时、`compensation_failed` 的端到端脚本断言（任务 9.1/9.2/9.3/11.4 尚未完成）。
- `skipped`：生产 Temporal HA/持久化配置由 production-delivery 负责；本地使用 auto-setup。
- 未修改 `deploy/compose/compose.yaml`；profile 为独立 overlay，符合共享文件归属。

## 审批 E2E（2026-11-23）

- 镜像/栈：`docker compose -f deploy/compose/compose.yaml -f deploy/compose/authz/compose.authz.yaml -f deploy/compose/action-temporal/compose.temporal.yaml up -d --build`
- Action Type：`01a2045f-b2b0-78d7-a076-38d249a0dc63`（`e2e_approval`，step `noop` + `requires_approval=true`）
- Execution：`01a2045f-c02c-7d8b-b887-b2f6da2853aa`
- 首次 `status=waiting_approval`，审批任务 `c97e4065-b7a2-4617-bc6c-b69451566160` pending
- `POST /api/v1/actions/approvals/{id}/approve`（`Idempotency-Key: e2e-approve-2`）→ 200，`status=approved`
- Execution 轮询：`running` → `succeeded`
- 修复：OpenFGA `action_execution` 增加 `can_write: can_write from action_type`（approve 使用 admin 组→`can_write`），并重新生成 `policies/openfga/model.json`；授权事件 `action_execution:approve` 由 `engine_error` 变为 allow。
- 说明：审批信号路径已验证；重试/逆序补偿/补偿失败/超时路径仍未跑通。

## 校验限制（2026-11-23）

- `make license-check` 失败：`go-licenses` 无法识别 `github.com/nexus-rpc/nexus-proto-annotations/go/nexusannotations/v1` 的许可证类型（`Unknown license type`），这是 Temporal SDK 的传递依赖。
- 因此任务 11.1 的 `license-check` 子项未通过；没有通过修改 Makefile 跳过检查来掩盖。
- 需要依赖安全/法务确认该依赖许可证，或由上游提供明确 license 元数据。

## 对象副作用与补偿 E2E（2026-11-23）

- 创建对象类型 `e2e_object`（`01a20463-997b-7e22-9fb1-b6a30cd6723d`）与对象 `01a20463-a976-7d2e-8362-86d0a1b85f21`（`name=before`）。
- 创建 action type `e2e_update2`（`01a20464-54a5-75f7-9d01-f6959b152306`）：`object.update` step，properties `name=after`，compensation `name=before`。
- 执行 `01a20464-61aa-7d8c-93ee-3c0501da2a9d` → `succeeded`；对象版本 `1→2`，properties `name=after`。**对象写入副作用已真实通过 Temporal 执行。**
- 创建 `e2e_comp`（`01a20464-9286-7606-a1c2-2837b848bb6d`）：step1 `object.update`，step2 `test.permanent_failure`（`read_only=true`）。
- 执行 `01a20464-b108-732a-838c-bbb3f9292464` → `compensation_failed`；对象 `01a20464-a2a0-7e45-82a4-1e7e029f6e9c` 恢复为 `name=before`（版本 2）。
- 修复：`CompensateStep` 之前只合并 step/compensation 参数，未合并 execution 参数，导致补偿 `object.update` 缺少 `object_id`；现已合并 `execution.Parameters`。
- 说明：以上为手工真实 E2E，尚未封装为任务 9.1 要求的完整 smoke 脚本（拒绝/超时/耗尽/完整断言仍未全部覆盖）。

## Smoke 脚本真实运行（2026-11-23）

- `scripts/actions-temporal-smoke.sh` 在真实 Temporal（1.26.2）+ Postgres + authz 组合上启动 profile，等待 core/worker readyz，自动创建对象类型/对象与 7 组 Action Type。
- 结果：`{"pass":7,"fail":0}`，全部通过：
  - `object_update`：对象写入成功，版本推进。
  - `compensation`：逆序补偿后对象恢复 `before`，执行 `failed`。
  - `approval_approve`：审批通过后执行 `succeeded`。
  - `approval_reject`：审批拒绝后执行 `cancelled`。
  - `approval_timeout`：2s 审批超时后执行 `timed_out`。
  - `retry_exhaustion`：`test.transient_failure` 重试 3 次后 `failed`，`execution_steps.attempt>=3`。
  - `compensation_failed`：补偿活动永久失败时执行 `compensation_failed`。
- 脚本支持 `SKIP_START` / `STOP_STACK`，结果写入 `/tmp/actions-smoke-results.json`；任务 9.1 已勾选。

## 契约与文档检查（2026-11-23）

- `make proto-lint proto-breaking generate-check`：通过（无公共 proto 变更）。
- `pnpm check`、`pnpm test`（33 passed）、`pnpm build`：通过；Playwright mock E2E 未运行，任务 11.3 未勾选。
- `scripts/actions-temporal-smoke.sh` 重跑：7/7 通过，退出码 0（任务 11.4）。
- 文档补丁：`openspec/changes/action-temporal/docs-patches.md`（services-and-ports、runtime-topology、ARCHITECTURE §5.1/§7.3 建议，任务 10.2/10.3）。

- `openspec/changes/action-temporal/ci-patch.md`：提供 label/手动触发的 E2E CI job 补丁，不修改主 `ci.yml`（任务 9.3）。

## 决策与文档收口（2026-11-23）

- 任务 1.1：`action_execution:approve` 已新增并接入 catalog/schema；审批 API 不再复用 `action_execution:cancel`。
- 任务 1.2：worker 二进制/本地 compose profile 归 action-temporal；生产 Helm/ArgoCD 归 production-delivery；app/cmd 接线归 stage-2-integration。
- 任务 1.4：自审批未禁止（审批人需 approve 权限）；不可逆副作用需显式补偿或 `allow_without_compensation` + 审批；幂等 TTL 24h。
- 任务 2.6：`internal/modules/actions/README.md` 已记录迁移顺序/字段语义/回滚边界。
- `openspec validate action-temporal --strict` 继续 valid；action 任务 40/55。

- 任务 3.4：`internal/platform/config` 新增 `ONTOAXIS_ACTIONS_EXECUTION_MODE`、`ONTOAXIS_TEMPORAL_HOST_PORT`、`ONTOAXIS_ACTIONS_APPROVAL_TIMEOUT`、`ONTOAXIS_ACTIONS_WORKFLOW_TIMEOUT`，默认 `temporal`/空/30m/5m，校验 mode 与正超时；单测 `TestLoadActionTemporalConfig`、`TestLoadRejectsInvalidActionMode` 通过。
- 任务 3.5：依赖评审见 `openspec/changes/action-temporal/dependency-review.md`；Temporal SDK v1.51.0 固定版本，license-check 传递依赖识别失败已记录为限制。
