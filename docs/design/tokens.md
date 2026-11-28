# 设计 token 初稿

> 状态：draft（2026-11-25）
> 上游：[visual-direction.md](visual-direction.md)
> 本文是**提案**，尚未写入 `web/`。落码时以单独 change/Fast Track 评审，并附截图证据。

## 1. 命名与分层

三层，禁止跨层引用：

```text
primitive（原始值：色阶、字号、间距）
  └── semantic（语义：canvas / text / action / border）
        └── component（组件：button-height / table-row-height / inspector-width）
```

- 原始值只出现在 `tokens.css`；功能样式不得写裸 hex。
- 组件只消费 semantic / component token，不直接消费 primitive。
- 前缀统一 `--oa-`，避免与现有 `--bg`、`--accent` 等变量冲突。
- 过渡期保留旧变量名做别名，迁移完成后删除（见 §5）。

## 2. 完整 token 草案

```css
:root {
  /* ---------- primitive: neutral ---------- */
  --oa-neutral-0: #ffffff;
  --oa-neutral-50: #f4f5f3;
  --oa-neutral-100: #eceeeb;
  --oa-neutral-200: #e2e4e1;
  --oa-neutral-300: #cdd1cc;
  --oa-neutral-400: #9aa09c;
  --oa-neutral-500: #5f6669;
  --oa-neutral-600: #4a5153;
  --oa-neutral-900: #15181a;

  /* ---------- primitive: signal ---------- */
  --oa-teal-50: #e3f0ec;
  --oa-teal-600: #0e6b5c;
  --oa-teal-700: #0a564a;

  /* ---------- primitive: data type ---------- */
  --oa-data-string: #4a5153;
  --oa-data-number: #a15c00;
  --oa-data-boolean: #6b4c8a;
  --oa-data-time: #2f5fa8;

  /* ---------- primitive: status ---------- */
  --oa-status-success: #1f7a4d;
  --oa-status-warning: #8a5a00;
  --oa-status-danger: #b3261e;
  --oa-status-info: #245c8a;

  --oa-success-50: #e7f2ec;
  --oa-warning-50: #f7efdf;
  --oa-danger-50: #fbeceb;
  --oa-info-50: #e9f0f6;

  /* ---------- primitive: space（4px 基数） ---------- */
  --oa-space-1: 4px;
  --oa-space-2: 8px;
  --oa-space-3: 12px;
  --oa-space-4: 16px;
  --oa-space-6: 24px;
  --oa-space-8: 32px;
  --oa-space-12: 48px;
  --oa-space-16: 64px;

  /* ---------- primitive: radius / type / motion ---------- */
  --oa-radius-sm: 4px;
  --oa-radius-md: 8px;
  --oa-radius-full: 999px;

  --oa-font-sans: "IBM Plex Sans", "IBM Plex Sans SC", "PingFang SC",
    "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  --oa-font-mono: "IBM Plex Mono", "SFMono-Regular", Menlo, Consolas, monospace;

  --oa-text-xs: 12px;
  --oa-text-sm: 13px;
  --oa-text-base: 14px;
  --oa-text-md: 16px;
  --oa-text-lg: 20px;
  --oa-text-xl: 26px;
  --oa-text-2xl: 32px;

  --oa-leading-xs: 1.35;
  --oa-leading-sm: 1.35;
  --oa-leading-base: 1.5;
  --oa-leading-md: 1.3;
  --oa-leading-lg: 1.25;
  --oa-leading-xl: 1.2;
  --oa-leading-2xl: 1.1;

  --oa-weight-regular: 400;
  --oa-weight-medium: 500;
  --oa-weight-semibold: 600;

  --oa-duration-fast: 120ms;
  --oa-duration-base: 180ms;
  --oa-ease-out: cubic-bezier(0.2, 0.8, 0.2, 1);

  /* ---------- semantic: surface & text ---------- */
  --oa-color-canvas: var(--oa-neutral-50);
  --oa-color-surface: var(--oa-neutral-0);
  --oa-color-surface-sunken: var(--oa-neutral-100);
  --oa-color-text: var(--oa-neutral-900);
  --oa-color-text-secondary: var(--oa-neutral-600);
  --oa-color-text-muted: var(--oa-neutral-500);
  --oa-color-text-disabled: var(--oa-neutral-400);
  --oa-color-border: var(--oa-neutral-200);
  --oa-color-border-strong: var(--oa-neutral-300);

  /* ---------- semantic: action & selection ---------- */
  --oa-color-action: var(--oa-teal-600);
  --oa-color-action-hover: var(--oa-teal-700);
  --oa-color-action-soft: var(--oa-teal-50);
  --oa-color-on-action: var(--oa-neutral-0);
  --oa-color-focus: var(--oa-teal-600);
  --oa-color-selection: var(--oa-teal-50);

  /* ---------- semantic: feedback ---------- */
  --oa-color-success: var(--oa-status-success);
  --oa-color-success-soft: var(--oa-success-50);
  --oa-color-warning: var(--oa-status-warning);
  --oa-color-warning-soft: var(--oa-warning-50);
  --oa-color-danger: var(--oa-status-danger);
  --oa-color-danger-soft: var(--oa-danger-50);
  --oa-color-info: var(--oa-status-info);
  --oa-color-info-soft: var(--oa-info-50);

  /* ---------- semantic: data type ---------- */
  --oa-color-type-string: var(--oa-data-string);
  --oa-color-type-number: var(--oa-data-number);
  --oa-color-type-boolean: var(--oa-data-boolean);
  --oa-color-type-time: var(--oa-data-time);

  /* ---------- component ---------- */
  --oa-button-height: 36px;
  --oa-button-height-sm: 32px;
  --oa-control-radius: var(--oa-radius-sm);
  --oa-table-row-height: 40px;
  --oa-table-row-height-comfortable: 48px;
  --oa-table-header-height: 36px;
  --oa-rail-width: 224px;
  --oa-rail-width-collapsed: 64px;
  --oa-inspector-width: 360px;
  --oa-content-max: 1440px;
  --oa-page-padding: var(--oa-space-8);

  --oa-focus-ring: 2px solid var(--oa-color-focus);
  --oa-focus-offset: 2px;
  --oa-elevation-overlay: 0 12px 32px rgba(21, 24, 26, 0.12),
    0 1px 2px rgba(21, 24, 26, 0.06);
}
```

## 3. 对比度

以下为按 sRGB 相对亮度计算的近似值；落地前必须用工具（如 `axe`、`colour-contrast`）复核并记录。

| 前景 / 背景 | 比值 | 用途 | AA 正文 |
| --- | --- | --- | --- |
| `#15181A` / `#FFFFFF` | 17.8:1 | 主文本 | 通过 |
| `#15181A` / `#F4F5F3` | 16.3:1 | 主文本 | 通过 |
| `#4A5153` / `#FFFFFF` | 8.1:1 | 次要文本 | 通过 |
| `#4A5153` / `#F4F5F3` | 7.4:1 | 次要文本 | 通过 |
| `#5F6669` / `#FFFFFF` | 5.8:1 | 元信息 | 通过 |
| `#5F6669` / `#F4F5F3` | 5.3:1 | 元信息 | 通过 |
| `#0E6B5C` / `#FFFFFF` | 6.4:1 | 链接、主操作 | 通过 |
| `#FFFFFF` / `#0E6B5C` | 6.4:1 | 主按钮文字 | 通过 |
| `#A15C00` / `#FFFFFF` | 5.2:1 | 数值类型 | 通过 |
| `#6B4C8A` / `#FFFFFF` | 6.9:1 | 布尔类型 | 通过 |
| `#2F5FA8` / `#FFFFFF` | 6.3:1 | 时间类型 | 通过 |
| `#1F7A4D` / `#FFFFFF` | 5.3:1 | 成功 | 通过 |
| `#8A5A00` / `#FFFFFF` | 5.9:1 | 警告 | 通过 |
| `#B3261E` / `#FFFFFF` | 6.5:1 | 危险 | 通过 |
| `#245C8A` / `#FFFFFF` | 7.1:1 | 信息 | 通过 |

注意：

- 这些颜色用于**文字**时满足 AA；作为 6–8px 的圆点/方块时不承担文本职责，但仍需配文字标签。
- `#9AA09C` 仅用于禁用文本（AA 不适用于禁用控件），不要用于正文。
- 焦点环与相邻背景的对比度需 ≥3:1；深青在浅灰/白底上满足。

## 4. 使用规则

- 一个视图最多两种功能色；信号色不参与状态或分类编码。
- 数字与可比较的值使用 `font-variant-numeric: tabular-nums`。
- 标识符使用 `--oa-font-mono`、`--oa-text-xs`；中文正文不使用等宽。
- 状态 = 圆点 + 文本；数据类型 = 方块 + 文本；不使用彩色胶囊。
- 卡片只用 1px 边框，不投影；阴影只给模态、抽屉、浮层。
- 间距只用 `--oa-space-*`，不写 5px、10px、18px 之类的散值。
- 圆角只用 sm / md / full；嵌套容器不重复加圆角。
- 动效只用于状态反馈；`prefers-reduced-motion` 下全部关闭。
- 禁止：渐变、发光、彩色阴影、`!important`（reduced-motion 覆盖除外）、字号小于 12px、中文全大写、中文加字距。

## 5. 从现有变量迁移

| 现有变量 | 现值 | 迁移到 | 备注 |
| --- | --- | --- | --- |
| `--bg` | `#f5f6fa` | `--oa-color-canvas` | 底色略变暖绿 |
| `--surface` | `#ffffff` | `--oa-color-surface` | 直接映射 |
| `--surface-soft` | `#f8f9fc` | `--oa-color-surface-sunken` | 变深一档 |
| `--ink` | `#182033` | `--oa-color-text` | 去蓝调 |
| `--ink-soft` | `#4b556c` | `--oa-color-text-secondary` | 直接映射 |
| `--muted` | `#7b849c` | `--oa-color-text-muted` | 对比度提升 |
| `--line` | `#e4e7ef` | `--oa-color-border` | 直接映射 |
| `--line-strong` | `#d3d8e4` | `--oa-color-border-strong` | 直接映射 |
| `--accent` | `#5b5bd6` | `--oa-color-action` | 靛蓝改为深青 |
| `--accent-strong` | `#4646c6` | `--oa-color-action-hover` | 同上 |
| `--accent-soft` | `#eeeeff` | `--oa-color-action-soft` | 选中底改为浅青 |
| `--blue` | `#2563eb` | 按用途映射 | 数据集/信息需逐处审计，禁止批量替换 |
| `--green` | `#16835b` | `--oa-color-success` | 状态语义 |
| `--amber` | `#a16207` | `--oa-color-warning` | 状态语义 |
| `--violet` | `#7c3aed` | 按用途映射 | 对象类型标签需去掉胶囊后重新决定 |
| `--danger` | `#b42318` | `--oa-color-danger` | 直接映射 |
| `--danger-soft` | `#fef3f2` | `--oa-color-danger-soft` | 直接映射 |
| `--sidebar` / `--sidebar-soft` | `#141a2b` / `#1d253a` | 删除 | 壳层改浅色轴轨 |
| `--radius-sm` | 8px | `--oa-radius-sm`（4px） | 控件收紧 |
| `--radius` | 12px | `--oa-radius-md`（8px） | 面板收紧 |
| `--radius-lg` | 18px | `--oa-radius-md`（8px） | 模态统一为 8 |
| `--shadow` | 双层柔和阴影 | 删除 | 卡片不投影；浮层用 `--oa-elevation-overlay` |
| `--font-sans` | Inter 栈 | `--oa-font-sans` | 改 IBM Plex |
| `--font-mono` | 系统等宽栈 | `--oa-font-mono` | 改 IBM Plex Mono |

迁移策略：

1. 先新增 `web/src/styles/tokens.css`，不动页面。
2. 在过渡文件里把旧变量别名到新 token（`--bg: var(--oa-color-canvas)` 等），一次性换色，附改造前后截图；这一步不改 DOM，属于 Fast Track 候选。
3. 页面结构改造（hub、路由、检查器）按 [information-architecture.md](information-architecture.md) 的优先级逐步进行。
4. 全部迁移完成后删除旧变量别名与 `styles.css` 中的散值。

> 注意：`--sidebar` 不能简单别名到浅色，否则深色渐变背景会残留；壳层必须与 token 一起改。

## 6. 组件状态清单

每个组件在实现前先勾完状态，缺一不合并。

| 组件 | 必须覆盖 | 备注 |
| --- | --- | --- |
| Button | default / hover / focus-visible / active / disabled / loading | 3 种 variant：primary / secondary / ghost；危险操作用 danger |
| Link | default / hover / focus-visible | 正文内链接保留下划线；导航链接不做下划线 |
| Text field | default / hover / focus / error / disabled / readonly | label、hint、error 三者用 `aria-describedby` 关联 |
| Select / Combobox | 同上 + loading / 无匹配 | 键盘：上下、Enter、Esc |
| Checkbox / Radio | default / focus-visible / disabled / error | 点击区域 ≥ 整个 label |
| Table | 表头 / hover / selected / focus-within / loading / empty / error | 数字右对齐；标识符单行省略；行选择用链接，不绑 `tr onClick` |
| Tabs | default / hover / focus-visible / selected / overflow | 左右箭头切换，roving tabindex |
| Modal / Drawer | open / close / focus trap / Esc / 初始焦点 / 归还焦点 | 关闭不得丢表单输入 |
| Inspector | open / closed / empty / 滚动 | 1080 以下变抽屉 |
| Toast / Notice | success / error / info / dismiss | 错误不自动消失 |
| Pagination | 当前页 / 前后页禁用 / 总数 | 页码变化保留筛选 |
| Breadcrumb | 当前页不可点 / 溢出折叠 | 与浏览器历史一致 |
| Async states | loading / empty / no-match / error / forbidden / degraded / mock / partial | 见 IA §8 |
| Code block / SQL | 只读 / 可编辑 / focus / error / 横向滚动 | 是否引入 Monaco 单独决策；v1 可保留 textarea |

## 7. 目标文件结构

落码时建议（当前尚未创建）：

```text
web/src/styles/
  tokens.css          # 本文档 §2
  base.css            # reset、排版、focus-visible、reduced-motion
  components/
    button.css
    field.css
    table.css
    tabs.css
    modal.css
    inspector.css
    async-state.css
```

`web/src/styles.css` 在迁移期间保留，作为对比与回退；迁移完成后删除。

## 8. 采用完成的判定

- 功能样式中裸 hex 数量为 0（`tokens.css` 除外）。
- 所有可交互元素键盘可达，且 `:focus-visible` 可见。
- 字号无 <12px；中文无字距；数字使用 tabular。
- 1440×900 与 1280×800 的 Playwright 截图基线稳定。
- 对比度检查结果记录在案。
- 200 属性、超长中英文名、UUID、空结果下无布局溢出。

## 9. 待决

- Web 字体是否自托管、子集化；若否，回退系统字体（影响仅字体，不影响比例尺）。
- 是否引入 Radix 作为行为层；是否引入 Tailwind/shadcn（建议暂不）。
- 正文 14px 还是 13px：先在对象类型 hub 上做可读性对比测试。
- 暗色模式：v1 不做；若未来要做，只重映射 semantic 层，primitive 不动。
- 信息/时间类型共用蓝色系是否可接受；若测试中混淆，调整其中一者。
