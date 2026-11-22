# OntoAxis（元轴）

OntoAxis 是面向企业的 Ontology 数据平台，围绕对象类型、对象实例、数据集、Pipeline 与查询提供统一的数据建模和探索能力。主要用户为数据平台团队/数据工程师、业务分析师和数据治理/合规人员。

当前仓库处于**治理初始化阶段**：只有架构设计与 Stage/OpenSpec 治理文件，尚未包含可运行的业务代码、构建配置或基础设施。`ARCHITECTURE.md` 描述的是目标设计，不代表已实现。

## 目录

- [`ARCHITECTURE.md`](ARCHITECTURE.md)：目标架构与实施方案（设计真源）
- [`AGENTS.md`](AGENTS.md)：协作规则、命令与安全边界
- [`CLAUDE.md`](CLAUDE.md)：Claude Code 入口
- [`doc/workflow/RUNBOOK.md`](doc/workflow/RUNBOOK.md)：Stage 与 OpenSpec 工作流
- [`openspec/config.yaml`](openspec/config.yaml)：OpenSpec 项目上下文与 artifact 规则
- `openspec/specs/`：已归档行为契约（当前为空）；`openspec/changes/`：在途变更（当前为 `stage-1-platform-foundation`）

## 开发流程

1. 新功能、行为变化、接口/权限/数据语义变化：走 OpenSpec change（先 proposal + delta specs，再实施）。
2. 行为不变的小修：走 Fast Track，保留测试和清晰提交说明。
3. 阶段级交付：按 `doc/workflow/RUNBOOK.md` 的 Stage 流程管理范围、AC、证据与退出。
4. 所有 OpenSpec 校验使用 `openspec validate --all --strict`。

当前已有 **Stage 1 批准版**（[`doc/stages/stage-1-ontoaxis-core-brief.md`](doc/stages/stage-1-ontoaxis-core-brief.md) + [`doc/stages/stage-1-change-map.md`](doc/stages/stage-1-change-map.md)）；Stage 已通过评审；首个 OpenSpec change `stage-1-platform-foundation` 已完成规划，等待变更评审。
