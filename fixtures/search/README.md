# 对象检索评测基线（合成数据）

本目录是为 OpenSpec change `object-search-meilisearch` 任务 **8.3** 提供的检索相关度/召回评测夹具。

> **所有文档、查询与阈值均为合成数据，不含真实客户、供应商或生产数据。**
> 阈值状态为 **`pending-owner-signoff`（待负责人签核）**；在签核前，本目录和基线脚本的输出只能作为技术基线，
> **不得**用于判定业务“合格”、不得写入验收结论、不得作为 Stage 退出证据中的业务指标。
>
> 另有一个**已签核的工程回归门** `engineering_gate`（冻结语料哈希 + 不低于基线 1.00/1.00），由
> `scripts/search-quality-baseline.sh --strict` 在 CI 执行；它只保证合成夹具不回归，**不**等同业务质量门。

## 文件

| 文件 | 用途 |
| --- | --- |
| `dataset.json` | 24 条中文合成文档（供应商/物料/设备/产品/客户/合同/批次 7 类 object type）+ 6 条查询及人工标注的 relevant id + 待签核阈值 |
| `../../scripts/search-quality-baseline.sh` | 把数据集载入一次性 Meilisearch 索引，计算 recall@5 / MRR@10 并打印逐查询结果 |

## 数据集结构

```json
{
  "name": "object-search-quality-baseline",
  "version": 1,
  "synthetic": true,
  "tenant_id": "11111111-1111-4111-8111-111111111111",
  "object_type_ids": { "supplier": "...", "...": "..." },
  "thresholds": {
    "status": "pending-owner-signoff",
    "owner": "klb3713",
    "recall_at_5": 0.9,
    "mrr_at_10": 0.8,
    "note": "初稿，需负责人签核后才可作为质量门槛"
  },
  "engineering_gate": {
    "status": "signed-off",
    "owner": "klb3713",
    "signed_at": "2026-11-24",
    "corpus_sha256": "<冻结语料 documents+queries 的 sha256>",
    "baseline": { "recall_at_5": 1.0, "mrr_at_10": 1.0 },
    "note": "工程回归门，不是业务质量门"
  },
  "documents": [ { "id": "...", "tenant_id": "...", "object_type_id": "...", "display_name": "...", "properties_text": "...", "version": 1, "created_at": "...", "updated_at": "..." } ],
  "queries": [ { "id": "q1", "q": "供应商 资质", "relevant": ["..."] } ]
}
```

`documents` 的字段与 `internal/platform/search/mapping.go` 的 `Document` 对应；脚本写入的索引设置
（`searchableAttributes`/`filterableAttributes`/`sortableAttributes`/`displayedAttributes`/
`pagination.maxTotalHits`）镜像 `mapping.SettingsJSON()`。若后者变更，必须同步本脚本并重跑。

## 运行方式

```bash
# 任意可用的 Meilisearch（例如本地 compose 的 17700 或临时测试容器）
MEILI_URL=http://127.0.0.1:17700 \
MEILI_API_KEY=local-search-master-key \
scripts/search-quality-baseline.sh

# 可选：--strict 在低于“未签核初稿阈值”时退出 3（仍不代表业务判定）
scripts/search-quality-baseline.sh --strict
```

退出码：`0` 已完成测量（默认不因质量数值失败）；`2` 基础设施/协议失败；`3` `--strict` 下低于未签核初稿阈值。

## 如何解读

- 脚本输出 `recall@5` 与 `MRR@10` 的逐查询数值和汇总值，并明确打印 `status=pending-owner-signoff`。
- `q1` 等查询的 relevant 集合由人工按字面主题标注，不是业务标注规范；数据量小（24 条），不能外推到真实语料。
- 基线不覆盖：权限过滤后的可见性、同义词/拼写纠错、长文档截断、真实中文分词边界、排序字段业务权重。
- 阈值签核后，应由负责人在本文件与 `dataset.json` 中同时更新 `status`、签核人和日期；在此之前 **不得** 勾选任何“业务合格”类结论。

## 变更记录

| 日期 | 变更 | 证据 |
| --- | --- | --- |
| 2026-11-23 | 初始合成数据集与基线脚本 | 本地临时 Meilisearch v1.11.3 运行，recall@5=1.0000、MRR@10=1.0000（技术基线，阈值未签核） |
| 2026-11-24 | 拆分工程回归门与业务质量门 | 负责人签核 `engineering_gate`（冻结语料 sha256、基线 1.00/1.00），`--strict` 改为强制工程门并接入 CI；业务阈值仍 `pending-owner-signoff` |