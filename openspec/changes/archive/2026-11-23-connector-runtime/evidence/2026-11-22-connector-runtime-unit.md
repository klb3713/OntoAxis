# connector-runtime 本地验证证据（2026-11-22）

> 范围：本 change 的单元、race、构建、deploy 静态校验与门禁；不包含真实 Kafka Connect / Trino E2E。所有数据为合成数据，不使用真实客户数据或生产凭据。

## 环境

- macOS x86_64，Docker 27.0.3，Compose v2。
- Go 1.26.3（`/tmp/go/bin/go`），`GOMODCACHE=/tmp/gomod`，`GOFLAGS=-buildvcs=false`。
- 代码状态：工作区未提交；本 change 新增 `internal/modules/connectors/runtime/`、`internal/modules/ingestion/runtime/`、`deploy/connector-runtime/`，并修改 connectors/ingestion 模块。

## 命令与结果

| 命令 | 结果 |
| --- | --- |
| `go build ./...` | exit 0 |
| `go vet ./internal/modules/connectors/... ./internal/modules/ingestion/...` | exit 0 |
| `go test ./...` | 全部包通过 |
| `go test -race ./internal/modules/connectors/... ./internal/modules/ingestion/...` | 通过 |
| `gofmt -l internal/modules/connectors internal/modules/ingestion` | 无输出 |
| `docker compose -f deploy/compose/compose.yaml -f deploy/connector-runtime/compose.connector-runtime.yaml --profile connector-runtime config -q` | exit 0 |
| 追加 `-f deploy/connector-runtime/compose.trino-iceberg.yaml ... config -q` | exit 0 |
| `openspec validate connector-runtime --strict` | valid |

主要单测覆盖：

- connectors runtime：命名确定性（含 UUIDv7 同前缀回归）、配置白名单/非法表名拒绝、source/sink golden、secret 注入与脱敏、Connect 2xx/4xx/5xx/超时/非法 JSON、幂等 PUT、重试序列与上限、提交失败回收、停止/删除不存在幂等、状态归一化。
- ingestion runtime：条件领取并发唯一、租约丢失、重复提交/同 Connector 冲突、失败分类与重试耗尽、终态不可变、快照确认需两次稳定、行数不一致保持 running、cleanup_pending、取消四类路径、Watch/投影一致性、后台租户隔离。
- connectors/ingestion 既有 API/service 测试不放宽仍通过；`object:batch_upsert` 不属本 change。

## 未覆盖 / 限制

1. 未启动 `connector-runtime` profile；未连接真实 Kafka Connect、Redpanda、源 Postgres、Iceberg 或 Trino。因此任务 5.1 的 “容器 healthy”、5.2 的插件列表、5.3 的 E2E、6.1 的真实 Connect 集成、6.2 的 S2-AC-06 均未完成。
2. `SnapshotVerifier` 仅有端口与 fake；生产 verifier（Iceberg snapshot + 行数 + Trino 可读）由 `stage-2-integration` 提供，未注入时作业保持 running，不会假成功。
3. 设计 D6 的“预检有界源表计数写入 `expected_rows`”尚未实现；当前 reconciler 依赖 verifier 返回行数并与运行统计比较，证据中不声称已完成源表计数。
4. `internal/app`/`cmd`、`internal/platform/config`、`deploy/compose` 未修改；装配见 `integration-patch.md`，由 `stage-2-integration` 合并。
5. `go.mod` 依赖尚未 `go mod tidy`；由 integration 在合并装配时执行。
6. 授权层跨租户/越权 403 断言依赖 `authorization-openfga-cedar` 装配；本 change 以既有租户隔离与 fail-closed 断言覆盖，未声称 403 矩阵完成。

## 结论

单元/race/构建/deploy 静态校验通过；真实运行时与 E2E 证据缺失，任务 5.1–5.3、6.1–6.4、7.3 中仅静态部分完成。不得据此宣布 S2-AC-06 已验证或本 change 可归档。
