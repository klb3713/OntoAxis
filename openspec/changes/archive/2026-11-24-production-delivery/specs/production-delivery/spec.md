# production-delivery Specification

## Purpose

定义 OntoAxis 生产化交付的行为契约：以 Helm chart 与 ArgoCD GitOps 在 kind/k3d、staging-like、prod-like 受控环境中可复现地部署运行时组件，通过外部密钥注入、探针/资源/PDB、备份恢复、镜像摘要与回滚、预发 IdP 验证和交付烟测给出可复核证据，同时禁止真实云账号、生产密钥与真实客户数据进入交付链路。

## ADDED Requirements

### Requirement: 可复现的 Helm 发布单元与镜像摘要固定

生产化部署 SHALL 以 Helm chart 交付全部运行时组件，并按生命周期分组（platform / data / app / observability）；每次渲染 MUST 由环境 values 与按 digest 固定的镜像共同决定；渲染 MUST 确定（相同输入产生相同清单），并 MUST 拒绝 `latest`、缺少镜像 digest、缺少必填密钥引用或未声明的环境；chart MUST 通过 lint、模板渲染与清单校验后才允许部署。

#### Scenario: 相同 values 渲染确定

- **WHEN** 对同一 values 连续执行两次 `helm template`
- **THEN** 两次产物的字节内容与 digest 注释完全一致

#### Scenario: latest 或缺失 digest 被拒绝

- **WHEN** 某个组件的 image 使用 `latest` 或省略 digest
- **THEN** 渲染失败并指出该组件与缺失字段，不产生可部署清单

#### Scenario: 缺少必填密钥引用

- **WHEN** values 未声明某个要求外部密钥的组件的 secret key 引用
- **THEN** 渲染失败并列出缺失的 key 名称

#### Scenario: 未声明的环境被拒绝

- **WHEN** 使用未在 chart 中声明的环境名渲染
- **THEN** 渲染失败，不回退到默认 values 从而避免误部署

#### Scenario: chart 校验门禁

- **WHEN** CI 执行 helm lint、渲染与清单校验
- **THEN** 全部通过才允许进入部署阶段，任一失败即阻断

### Requirement: 环境 values 与受控晋升

系统 SHALL 为 kind、staging-like、prod-like 分离非敏感 values 并共享同一组 chart；晋升 MUST 以镜像 digest 为单位，MUST 要求来源环境存在通过的交付烟测与就绪证据，MUST NOT 复制密钥或环境密钥值；晋升结果 MUST 以 Git 提交表达并可回退。

#### Scenario: digest 晋升写回目标环境

- **WHEN** 执行从 kind 到 staging-like 的晋升
- **THEN** 目标环境 values 中的 digest 被更新并产生一条可评审的提交

#### Scenario: 缺证据的晋升被拒绝

- **WHEN** 来源环境没有通过的交付烟测记录
- **THEN** 晋升脚本拒绝执行并报告缺失的证据项

#### Scenario: 晋升不触碰密钥

- **WHEN** 检查晋升提交的 diff
- **THEN** 只包含 values 中的 digest、chart 版本或非敏感配置字段，不包含任何密钥值

#### Scenario: 回退上一次晋升

- **WHEN** 对晋升提交执行 revert 并同步
- **THEN** 目标环境回到上一已验证 digest，且交付烟测重新通过

### Requirement: ArgoCD 同步与漂移收敛

系统 SHALL 由 ArgoCD 以 app-of-apps 同步 chart，并按 sync wave 顺序（命名空间/CRD/ESO → platform → data → app → edge → observability → jobs）推进；kind/staging-like MUST 开启 automated prune 与 self-heal，prod-like MUST 手动同步；成功 MUST 以 Synced/Healthy 为条件，漂移 MUST 被检测并收敛，同步失败 MUST 呈现 Degraded 且不得记为成功。

#### Scenario: 新提交同步到 Synced/Healthy

- **WHEN** 向 GitOps 仓库提交新的 digest
- **THEN** 对应 Application 先变为 OutOfSync，同步后为 Synced/Healthy，且 Pod 实际镜像 digest 与 values 一致

#### Scenario: 手工漂移被收敛

- **WHEN** 在集群中手工修改 Deployment 或 ConfigMap
- **THEN** Application 变为 OutOfSync，self-heal 恢复期望状态并记录漂移事件

#### Scenario: 同步失败呈现 Degraded

- **WHEN** 部署因就绪超时或清单错误无法完成
- **THEN** Application 变为 Degraded 并保留失败事件，不产生成功证据

#### Scenario: prod-like 不自动同步

- **WHEN** prod-like 环境出现新的提交
- **THEN** Application 保持 OutOfSync 直到执行显式 sync，自动同步不生效

### Requirement: kind 可重复安装与诊断

系统 SHALL 提供 kind 的一键安装/卸载脚本：创建集群（至少 1 control-plane + 2 workers）、本地镜像仓库、安装 Envoy Gateway 与 ArgoCD、加载镜像并部署全部 chart；安装 MUST 幂等，MUST 在就绪超时后失败并输出事件/describe 诊断；MUST 复用 `deploy/edge` 与 `deploy/observability` 的既有资产，不重复维护路由与告警真源。

#### Scenario: 干净集群一键安装

- **WHEN** 在空集群上执行安装脚本
- **THEN** 所有 workload Ready，ArgoCD 应用 Synced/Healthy，交付烟测通过

#### Scenario: 重复执行安装幂等

- **WHEN** 对已安装集群再次执行安装脚本
- **THEN** 不产生破坏性变更，已就绪组件保持可用

#### Scenario: 镜像不可用给出诊断

- **WHEN** 镜像无法拉取或加载
- **THEN** 安装失败并输出 ImagePull/ErrImageLoad 事件与相关 Pod describe，不静默重试为成功

#### Scenario: 卸载清理

- **WHEN** 执行卸载脚本
- **THEN** 命名空间、ArgoCD 应用与本地 registry 被清理，主机无残留端口占用

### Requirement: 探针、资源与 PDB

系统 SHALL 为长期运行组件配置 startup/readiness/liveness 探针（HTTP 组件使用 `/healthz` 与 `/readyz`，MUST NOT 用 `/metrics` 作为存活探针），MUST 设置 requests/limits 与优雅终止（terminationGracePeriodSeconds、preStop），MUST 为关键无状态组件配置 PDB 并保证 PDB 阻止不满足条件的驱逐；就绪失败 MUST 摘除流量但不触发重启。

#### Scenario: 存活与就绪语义分离

- **WHEN** 注入存活失败与就绪失败两种故障
- **THEN** 存活失败触发重启，就绪失败仅摘除 Endpoint 且容器不重启

#### Scenario: PDB 阻止不满足条件的驱逐

- **WHEN** 对受 PDB 保护的节点执行 drain，可用副本将低于 minAvailable
- **THEN** 驱逐被拒绝或阻塞，PDB 状态与事件可复核

#### Scenario: 资源超限可观察

- **WHEN** 容器超过 limits
- **THEN** OOMKilled 或 CPU throttling 通过事件/指标可观察，不静默失败

#### Scenario: 优雅终止

- **WHEN** Pod 收到 SIGTERM
- **THEN** 在 preStop 与 terminationGracePeriodSeconds 窗口内完成在途请求后再退出

### Requirement: 密钥注入与外部密钥源

系统 SHALL 从外部密钥源注入运行时密钥（数据库 DSN、OIDC client secret、OpenFGA/Meilisearch/Trino/对象存储凭据、Keycloak bootstrap 口令、备份口令），应用 MUST 通过 Secret 引用读取；MUST NOT 使用内置默认口令；密钥缺失或非法时组件 MUST 保持未就绪并给出非敏感错误；轮换 MUST 无需重建镜像。

#### Scenario: ExternalSecret 同步为 Secret

- **WHEN** ExternalSecret 指向的外部密钥存在
- **THEN** 生成对应 K8s Secret，Pod 通过 env 或文件引用消费且不打印值

#### Scenario: 缺失 key 保持未就绪

- **WHEN** 外部密钥源缺少某个必需 key
- **THEN** 相关 Pod 保持未就绪，错误信息只包含 key 名称而不含密钥值

#### Scenario: 轮换无需重建镜像

- **WHEN** 轮换数据库口令或 OIDC client secret
- **THEN** 按 refreshInterval 同步后新值生效，启动时读取的组件滚动重启，支持热加载的组件免重启

#### Scenario: 外部密钥源不可达

- **WHEN** 外部密钥源暂时不可达
- **THEN** 组件保持未就绪并使用非敏感错误，不回退到默认口令或旧值

### Requirement: 仓库零明文密钥

系统 SHALL 保证 Git 跟踪内容中不存在明文密钥（values、chart、ArgoCD 应用、脚本、测试夹具、证据与日志）；values MUST 只引用密钥名称或键路径；CI MUST 运行密钥扫描并在命中时阻断；证据与日志 MUST 脱敏（token、口令、connection string 中的凭据）。

#### Scenario: 明文密钥提交被阻断

- **WHEN** 提交中包含明文 secret 的 values 或脚本
- **THEN** 密钥扫描失败并阻断，报告命中的文件与位置（不回显完整值）

#### Scenario: 仅引用密钥的 values 通过

- **WHEN** values 只包含 secretStoreRef、remoteRef key 等引用
- **THEN** 扫描通过，chart 渲染仍要求密钥存在

#### Scenario: 证据与日志脱敏

- **WHEN** 采集失败诊断、事件或日志作为证据
- **THEN** token、口令与带凭据的 connection string 被替换为占位符

#### Scenario: 本地密钥文件不入库

- **WHEN** 创建 `.env.kind` 或本地 secret 覆写文件
- **THEN** 它们被 gitignore 覆盖，CI 不打包或上传这些文件

### Requirement: 备份与恢复演练

系统 SHALL 对权威状态执行可校验备份（PostgreSQL `pg_dump -Fc`，附 sha256、created_at、schema 版本与镜像 digest），MUST 能在干净命名空间恢复并通过迁移版本、代表性数据量与只读 API smoke 校验；损坏或不完整备份 MUST 被拒绝；恢复 MUST NOT 手工修改迁移历史；演练 MUST 记录实测 RPO/RTO 并与 Stage 2 暂定目标对比，且不得判定为生产合格。

#### Scenario: 备份可恢复且校验通过

- **WHEN** 从最近一次备份恢复到干净的 `oa-restore` 命名空间
- **THEN** 迁移版本、代表性表行数与只读 API smoke 通过

#### Scenario: 损坏备份被拒绝

- **WHEN** 备份文件的 sha256 与 manifest 不一致
- **THEN** 恢复脚本拒绝执行并报告校验失败，不启动恢复实例

#### Scenario: 恢复后服务就绪

- **WHEN** 恢复完成且应用启动
- **THEN** `/readyz` 返回 200，只读查询返回预期数据形状

#### Scenario: manifest 缺失字段被拒绝

- **WHEN** 备份缺少 schema 版本或 created_at
- **THEN** 恢复被拒绝并报告缺失字段

#### Scenario: RPO/RTO 记录为技术验证

- **WHEN** 演练完成
- **THEN** 证据记录实测 RPO/RTO 并标注"仅技术验证，非生产承诺"

#### Scenario: 未覆盖范围显式标记

- **WHEN** 对象存储与 Meilisearch 不在 Stage 2 备份范围
- **THEN** 证据中标记 residual risk 与重建路径，不声称完整备份

### Requirement: 镜像摘要与回滚

系统 SHALL 以镜像 digest 部署，MUST 维护 release manifest 记录 digest、commit 与 chart 版本；回滚 MUST 通过 GitOps 恢复到上一已验证 digest 并完成交付烟测；若迁移不兼容，回滚 MUST 走备份恢复路径并记录，不得回写迁移历史；回滚失败 MUST 阻断并保留证据。

#### Scenario: 坏部署回滚到上一摘要

- **WHEN** 部署一个就绪失败的镜像
- **THEN** ArgoCD 应用变为 Degraded，回滚到上一已验证 digest 后恢复 Healthy 并通过交付烟测

#### Scenario: 回滚目标不可用被阻断

- **WHEN** 上一 digest 在镜像仓库中不存在
- **THEN** 回滚失败并明确阻断发布，保留失败证据

#### Scenario: 同 schema 回滚免恢复

- **WHEN** 回滚前后数据库 schema 版本一致
- **THEN** 直接回退镜像并验证数据读取正常，无需恢复备份

#### Scenario: 跨 schema 回滚走恢复路径

- **WHEN** 回滚目标需要更旧的 schema
- **THEN** 停止写入、从备份恢复到独立实例并记录，不执行 down migration 或手工改迁移历史

#### Scenario: release manifest 与运行摘要一致

- **WHEN** 检查 release manifest 与运行中 Pod 的镜像
- **THEN** 两者 digest 一致；不一致时烟测失败并报告漂移

### Requirement: 交付烟测与失败证据

系统 SHALL 提供交付烟测脚本，检查 edge 200、core ready、IdP discovery、ArgoCD 应用 Synced/Healthy、探针/PDB/Secret 生效、最近备份与回滚标记、运行镜像 digest 一致；输出 MUST 含环境、digest、时间与逐项结果，非零退出表示失败；失败 MUST 采集脱敏事件/describe/日志并作为证据；未执行项 MUST 标记 `not passed`。

#### Scenario: 注入失败后非零退出

- **WHEN** 注入坏 digest 或缺失 Secret
- **THEN** 烟测以非零退出，并采集相关事件与 describe 作为失败证据

#### Scenario: 未执行项标记 not passed

- **WHEN** 某项检查因环境不支持而未执行
- **THEN** 输出 `not passed` 并说明原因，绝不记为 pass

#### Scenario: 证据可复核

- **WHEN** 审阅交付证据
- **THEN** 能追溯到命令、环境类型、digest 与时间，且不含密钥值

#### Scenario: 同一脚本跨环境使用

- **WHEN** 分别对 kind、staging-like、prod-like 执行烟测
- **THEN** 使用同一脚本，按环境裁剪检查项并在结果中显式声明未执行项

### Requirement: 受控环境与安全边界

系统 SHALL 只在 kind/k3d、staging-like、prod-like 受控环境执行交付流程；MUST NOT 读取真实云账号凭据、生产密钥或真实客户数据；证据 MUST 声明环境类型；需要真实云资源的步骤 MUST 标记为未执行并阻塞"生产就绪"声明；任何检测到生产凭据引用的脚本 MUST 拒绝执行并报告。

#### Scenario: 生产凭据被拒绝

- **WHEN** 脚本或 values 引用了生产云账号凭据或生产环境 endpoint
- **THEN** 脚本拒绝执行并报告违规字段，不发起连接

#### Scenario: 证据声明环境类型

- **WHEN** 生成交付证据
- **THEN** 证据标注环境为 kind、k3d 或 staging-like，不包含生产环境

#### Scenario: 云端步骤标记未执行

- **WHEN** 某验证需要真实云资源（如外部 secret provider）
- **THEN** 该步骤标记为 `not executed`，不作为任何 AC 的通过证据

#### Scenario: 合成数据边界

- **WHEN** 准备交付验证数据
- **THEN** 只使用合成夹具，不出现真实客户数据，并在证据中声明
