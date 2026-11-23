# Stage 2 集成缺陷登记（2026-11-23）

| id | 来源 | 分类 | 描述 | 状态 | 处置/链接 |
|---|---|---|---|---|---|
| D-01 | 2.2 edge-gateway | 跨 change 装配缺陷 | `compose.authz.yaml` 未给 `ontoaxis-core-edge` 注入 authz 环境，edge core 启动失败 | 已修复 | 增加 core-edge authz env；edge smoke 12/0；见 `../compose-smoke.md` |
| D-02 | 2.5 pipeline-kestra-dbt | 依赖缺失/环境 | Kestra profile、dbt 镜像与 v2 API 升级；真实 E2E 已完成 | 已关闭 | 证据 `openspec/changes/pipeline-kestra-dbt/evidence/2026-11-23-kestra-v2-upgrade-e2e.md`；13.x 已勾选 |
| D-03 | 2.4 connector-runtime | 代码/校验器 | 栈可起、source/sink task `RUNNING`，但 sink lag 不降、Iceberg 无 snapshot、job 不 `succeeded` | 已关闭 | 见下方「D-03 关闭」；证据 `openspec/changes/connector-runtime/evidence/2026-11-23-cdc-e2e-three-way.md` |
| D-04 | 2.7 object-search-meilisearch | 配置合并/测试 | overlay 可渲染，但 base compose 无 `search` profile（`--profile search` 报 no such service）；E2E/CI 缺失 | 待处置 | 可执行清单 `../../../stage-2-unblock-checklist.md` §2；先验证已实现的 claim/rebuild 再勾选 |
| D-05 | 2.8 production-delivery | 环境/GitOps 源 | kind 集群与 ArgoCD 已存在；`ontoaxis-root` 因 repoURL 占位报 `authentication required`，`oa-*` 无 Pod；镜像停在 `a04ab4c` | 待处置 | 可执行清单 `../../../stage-2-unblock-checklist.md` §3（GitOps 源选型 A/B/C + 镜像重建） |
| D-06 | 2.1/2.3 | 环境问题 | 本地 core 依赖 authz overlay；基础 compose 单文件启动会因缺少 authz 环境失败 | 已说明 | `compose-smoke.md`、`app-wiring.md` |
| D-07 | 2.4/2.5 跨 change | 配置冲突 | Kafka Connect 与 Kestra 的 host 端口默认值同为 `18086`，两个 profile 无法同时起栈 | 待处置 | 可执行清单 `../../../stage-2-unblock-checklist.md` §3.7；建议 Connect 改 `18089` 并补端口表 |


## D-03 补充（connector CDC，2026-11-23）
- 已把 Debezium 基础镜像切到 3.6.1（Java 21），source/sink task 可 RUNNING，Kafka topic 有 snapshot 记录。
- Kafka Connect Iceberg 1.12.0 sink 仍不提交：consumer group Stable 但 lag 不降；metadata 只有 `00000-*.json`、`snapshots=[]`。
- 已尝试静态表路由、RegexRouter SMT、动态表路由（缺 route-field 失败）、独立 control topic/group；均未解决。
- 建议：降级 Iceberg Connect 到已验证版本或改用 JDBC sink 完成本地 E2E；当前不勾选 5.3/6.2。


## D-02 补充（pipeline Kestra，2026-11-23）
- 尝试拉取 `kestra/kestra:latest` 做本地 profile 探针，镜像拉取超过 2 分钟未完成，已取消并清理容器。
- pipeline-kestra-dbt 的 compose profile / 真实 Kestra E2E 仍阻塞在镜像体积与 dbt worker 镜像构建；4.5/5.3/6.1/6.2 未完成。
- HTTP adapter 端点已对照设计（`/api/v1/flows/{ns}/{id}`、`/api/v1/namespaces/{ns}/files`、`/api/v1/executions/{ns}/{flow}`），待 Kestra 版本确认。


### D-03 追加：sink 主题前缀剥离候选修复（2026-11-23）
- `BuildSinkConfig` 增加 `RegexRouter` SMT：`transforms=stripTopicPrefix`，regex `^<topic_prefix>\.`，replacement 空，使 sink 记录 topic 变为 `public.<table>`，匹配 `iceberg.tables=public.<table>`；broker 主题与租户隔离不变。
- 实测：加 `RegexRouter` SMT 后 sink task 仍 RUNNING，但 consumer lag 不下降、Iceberg metadata 未新增 snapshot；已回退 `sink.go`/`sink_test.go` 到 baseline（单元测试通过）。真实 Debezium→Iceberg 三向行数 E2E 仍未通过，5.3/6.2 保持未勾选。

### D-02 追加：Kestra v0.24 本地 E2E 启动结论（2026-11-23）
- `deploy/compose/pipeline-kestra/Dockerfile` 的 `pip3 install --break-system-packages` 在 Ubuntu 22.04 的 pip 上不存在，已改为 `pip3 install`；镜像构建成功。
- compose healthcheck 改为 `GET /api/v1/configs`（`/api/v1/flows` 对匿名返回 401，导致容器 unhealthy）。
- Kestra 0.24.0 API 对 `/api/v1/flows` 返回 `401 WWW-Authenticate: Basic`，即使 `kestra.server.basic-auth.enabled: false`；`HTTPAdapter` 目前只发 `Authorization: Bearer`，publish 返回 503 `unavailable`。
- 需在 Kestra 侧创建 API token/用户或让适配器支持 Basic 认证；在此之前 pipeline 13.x 真实 E2E 不勾选。

### D-02 追加：Kestra Basic 认证尝试（2026-11-23）
- 已为 `HTTPAdapter` 增加 `basic:` token 前缀支持（发送 `Authorization: Basic base64(user:pass)`），并增加单测。
- 但 Kestra 0.24.0 容器在 `kestra.server.basic-auth.enabled: true` + 自定义 username/password、以及默认 `admin@kestra.io` 组合下仍返回 401；`KESTRA_SERVER_BASIC_AUTH_ENABLED=true` 同样无效。
- 结论：Kestra 0.24 OSS 镜像的认证路径未确认，需要查其实际用户/令牌初始化方式，或改用可匿名访问的旧版本/测试镜像；pipeline 13.x 真实 E2E 继续不勾选。

### D-02 追加：Kestra 0.17 探测（2026-11-23）
- `kestra/kestra:v0.17.0` 本地启动后 `/api/v1/configs` 200，`/api/v1/flows` GET 405（该版本列表接口不是 GET root），说明认证本身未阻塞旧版本。
- 0.24 的 `/api/v1/flows` 401 可能是该版本新增的 API 认证过滤器；适配器实际使用 `PUT /api/v1/flows/{namespace}/{id}`、`POST /api/v1/executions/...`，需要在 0.24 上用真实 PUT/Trigger 验证，而不是用 GET 列表判断。
- 下一步：在 0.24 容器内直接调用适配器的 PUT flow / trigger execution 端点；若仍 401，再降级到 0.17 或改 Kestra 认证配置。

### D-02 根因定位（Kestra v0.24.0，2026-11-23）

- 默认启动 `kestra/kestra:v0.24.0 server local` 时，`GET /api/v1/configs` 返回 `isBasicAuthInitialized:false`；同一实例的 `/api/v1/flows`、`PUT /api/v1/flows/{ns}/{id}`、`POST /api/v1/executions/{ns}/{flow}` 均返回 `401 WWW-Authenticate: Basic`，不是业务接口自身的问题。
- 通过 `KESTRA_SERVER_BASIC_AUTH_ENABLED=true` + `KESTRA_SERVER_BASIC_AUTH_USERNAME` + `KESTRA_SERVER_BASIC_AUTH_PASSWORD` 启动后，`isBasicAuthInitialized:true`，`curl -u` 可访问 API；仅改 `KESTRA_CONFIGURATION` 的 `kestra.server.basic-auth.enabled`/自定义用户名密码不会初始化 Basic Auth 用户，仍为 401。
- 认证通过后仍有一个适配器版本差异：Kestra 0.24 的 Flow 创建/更新是 `POST /api/v1/flows/{namespace}`（body 带 `id`、`namespace`、`revision`、`tasks`），`PUT /api/v1/flows/{namespace}/{id}` 返回 404；`POST /api/v1/executions/{namespace}/{flow}` 返回 200。现有 `HTTPAdapter.RegisterFlow` 使用 PUT，需要在适配器内按版本切换为 POST，或固定到端点兼容的版本。
- 结论：E2E 阻塞由“Basic Auth 未初始化”和“0.24 Flow upsert 端点变化”叠加造成；前者用环境变量初始化本地用户、后者用 POST 端点即可解除。真实 13.x E2E 仍需在改完后重跑，未勾选。

### D-02 关闭：升级 Kestra v2.0.12（2026-11-23）
- 用户决定升级到 `kestra/kestra:v2.0.12` 并删除旧版本兼容代码/配置。
- v2 要求 `KESTRA_SERVER_BASIC_AUTH_ENABLED=true` + username/password 初始化 Basic Auth；所有 API 改为 `/api/v1/{tenant}/...`，默认 tenant `main`；Flow upsert 为 `POST /api/v1/main/flows/{namespace}`（YAML body，PUT 405）；文件、触发、查询、取消、搜索接口按 v2 重写。
- 真实 E2E 已通过：publish 200、trigger 202、run `succeeded`、Kestra execution `SUCCESS`、Trino `memory.oa_t11111111_p01a205e4.build_orders` 可查。证据 `openspec/changes/pipeline-kestra-dbt/evidence/2026-11-23-kestra-v2-upgrade-e2e.md`。
- 13.1/0.2/0.3 已勾选；13.2–13.7（dataset 输出/血缘、失败/重试/负向、遥测）仍未覆盖。
- **更正（同日稍后）**：上述"13.2–13.7 仍未覆盖"已被同日后续执行推翻。13.2 输出/血缘、13.3 坏 SQL、13.4 停机 503→恢复重试、13.5 状态回写、13.6 负向、13.7 遥测均已通过并勾选，tasks 69/69；证据见 `docs/stages/evidence/stage-2-pipeline-kestra-dbt/`（`raw/` 下 13.x 原始响应与 metrics/trace 摘录）。D-02 保持"已关闭"，本 change 已按归档流程处理，后续集成 E2E 覆盖归 `stage-2-integration` 7.2。

### D-03 关闭（connector CDC，2026-11-23）

- 根因与修复：sink 不提交由三处叠加造成——source 侧需 `ExtractNewRecordState` 拆信封、sink 需共享 warehouse 根并按 `ds_<uuid8>_<schema>` 命名空间隔离、`iceberg.control.commit.timeout-ms=120000` 修复 rebalance 后重复写入；Trino 侧 `local.location=/` 修复 `file:/warehouse/...` 路径解析。
- 结果：`scripts/connector-runtime-e2e.sh --success-only` 7/7，源表 5 = sink `verified_rows` 5 = Trino 5，作业 `01a20826-…` / `01a20854-…` 均 `succeeded`；默认负向矩阵 18/18、`--with-outage` 10/10；集成测试 3 个真实实例用例通过。
- 证据：`openspec/changes/connector-runtime/evidence/2026-11-23-cdc-e2e-three-way.md`、`2026-11-23-negative-matrix-6-3.md`、`2026-11-23-integration-and-telemetry.md`；Stage 2 索引 `docs/stages/evidence/stage-2-connector-runtime/README.md`。
- 遗留：主键/upsert 未实现、告警未 firing、真实第二租户 E2E 不可行、`cleanup.sh` 未在矩阵调用，均已在 change 的 traceability「未覆盖与后续」登记。D-07（端口冲突）仍待处置，不随 D-03 关闭。
