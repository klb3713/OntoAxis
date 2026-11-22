# 依赖评审：Temporal Go SDK

- 依赖：`go.temporal.io/sdk v1.51.0`（Apache-2.0/MIT 兼容的 Temporal Go SDK；传递依赖含 `go.temporal.io/api v1.64.3`）。
- 用途：仅 `internal/modules/actions/temporal` 与 `cmd/ontoaxis-actions-worker` 使用；core 业务模块不导入 Temporal SDK。
- 替代方案：自研工作流/重试/信号机制（成本高、可靠性低）、Cadence SDK（生态/维护较弱）。选择 Temporal 因其持久化工作流、信号审批、重试与 worker 模型满足 D1–D10。
- License 检查：`make license-check` 当前因传递依赖 `github.com/nexus-rpc/nexus-proto-annotations/go/nexusannotations/v1` 许可证识别失败；已记录为已知限制，不在本变更内伪造通过。
- 版本策略：固定 `v1.51.0`，升级需单独依赖评审；`go.mod/go.sum` 变更由 `stage-2-integration` 统一 tidy/合并。
- 验证：`go build ./...` 在本分支通过；worker 与 SDK 单测/真实 Temporal smoke 通过。
