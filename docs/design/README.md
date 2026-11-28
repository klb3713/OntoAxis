# OntoAxis 界面设计（docs/design）

> 状态：draft（2026-11-25），待项目负责人与真实用户评审。
> 范围：`web/` 的界面语言、信息架构与设计系统；不改变后端 API、权限模型或数据语义。
> 本目录不是实现证据。未落入代码并经截图/测试验证之前，任何内容都只能表述为“提案”。

## 文档

| 文件 | 回答的问题 | 状态 |
| --- | --- | --- |
| [brief.md](brief.md) | 为谁设计、三个核心任务、原则与非目标 | draft |
| [information-architecture.md](information-architecture.md) | 导航、对象类型 hub、路由、关键流程、页面清单 | draft |
| [visual-direction.md](visual-direction.md) | 视觉母题、色彩/字体/布局语法、刻意拒绝的默认 | draft |
| [tokens.md](tokens.md) | 三层 token、对比度、迁移映射、组件状态清单 | draft |

## 阅读顺序

1. **brief**：先确认用户与任务。任何视觉争论回到这一页。
2. **IA**：确认“对象类型是导航脊柱”与路由契约。
3. **visual-direction**：确认一个可比较的方向，而不是最终皮肤。
4. **tokens**：确认命名、比例尺与迁移路径。

## 与现有真源的关系

- 产品范围与 AC：`docs/stages/stage-*-brief.md`
- 目标架构与前端技术选型：`ARCHITECTURE.md` §10
- 当前行为契约：`openspec/specs/`
- 工作流与门禁：`docs/workflow/RUNBOOK.md`

本目录只定义界面设计，不替代以上任何真源。若与 Stage brief 或主规格冲突，以它们为准，并回来修正本目录。

## 变更流程

1. 本目录内容变更走文档评审，不触代码门禁。
2. 设计定稿后，按 `docs/workflow/RUNBOOK.md` 判断走 change 还是 Fast Track；界面行为变化不得用 Fast Track 绕过评审。
3. 代码实现必须带：状态覆盖、键盘/焦点检查、Playwright 截图证据。
4. 纯 token 等价重构（截图不变）可走 Fast Track；一旦改变页面结构或交互，按 change 处理。

## 当前边界

- 本文档只覆盖已经存在的 7 个路由（项目、Ontology、对象浏览、数据集、查询、审批、登录）与它们的壳层。
- Pipeline、连接器、访问策略、图视图等规划能力只在 IA 中标注为“规划”，不视为已实现。
- 暗色模式、移动端适配不在 v1 范围；token 结构保留后续重映射的可能。

## 实施进度

| 日期 | 范围 | 状态 | 验证 |
| --- | --- | --- | --- |
| 2026-11-25 | token v0.1：新增 `web/src/styles/tokens.css`，旧变量映射到新 token，替换颜色/字体/圆角/表头与壳层配色；无 DOM、路由或交互变更 | 已落地，未提交 | `pnpm check`、`pnpm test`（35/35）、`pnpm build`、Playwright E2E（17/17）、1440/1280 截图 |

本次只做视觉 token 重构，按 Fast Track 处理。深色轴轨为过渡状态；浅色轴轨、项目切换器、对象类型 hub 与路由调整属于界面行为变化，超出 Stage 2 brief 的“`web/` 默认不新增页面”；实施前需要负责人决定新建 Stage 3，或在 Stage 2 中显式扩展范围，并按 RUNBOOK 开 OpenSpec change。

Web 字体尚未引入：当前回退到系统字体（PingFang SC / SF），IBM Plex 需自托管并子集化后才会生效，不影响本次 token 与比例尺。
