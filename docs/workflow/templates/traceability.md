# 变更 Traceability 模板

> 使用说明：复制本文件到 `openspec/changes/<change-name>/traceability.md` 后填写。
> Stage AC 编号只用于本 Stage 追溯，不写进长期 capability 的 requirement 名称。
> 所有状态从 `planned` 开始；只有证据存在才能改为 `verified`。

## 1. Stage AC 映射

| Stage AC | Capability / Requirement | Scenario | 证据 | 状态 |
| --- | --- | --- | --- | --- |
| S<序号>-AC-01 | 填写 capability / requirement | 填写 scenario 名 | 计划证据位置 | planned |
| S<序号>-AC-02 | 填写 capability / requirement | 填写 scenario 名 | 计划证据位置 | planned |

## 2. 未覆盖与偏差

| 项 | 原因 | 处理 | 负责人 |
| --- | --- | --- | --- |
| 填写未覆盖项 | 说明原因 | 说明补救或延后安排 | 负责人 |

## 3. 证据索引

| 证据 | 位置 | 生成方式 | 备注 |
| --- | --- | --- | --- |
| 填写证据名 | 填写文件或记录位置 | 填写生成命令 | 说明真实/合成数据边界 |

## 4. 归档检查

- [ ] 所有 requirement 至少有一个 scenario；
- [ ] 所有 Stage AC 有映射或明确偏差；
- [ ] 证据可复核，真实/合成数据分开；
- [ ] `openspec validate --all --strict` 通过；
- [ ] 主规格归档后已核对。
