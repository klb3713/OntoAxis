# GitHub Actions 引用钉 commit SHA（台账 6.2，2026-11-23）

## 范围

`ci.yml` 与 `deps.yml` 共 20 处 `uses:` 由浮动 major tag（`@v4` 等）改为 40 位 commit SHA，
行尾保留 `# vN` 注释便于人读。行为不变，仅消除「上游移动 tag 导致构建不可复现」的风险。

## 解析方式

逐个查询 GitHub API `git/ref`，annotated tag 取 tag object 指向的 commit；`bufbuild/buf-setup-action`
没有 `v1` tag，`v1` 是移动分支（当前 HEAD 等于 `v1.50.0`），按其 commit 固定并在注释中标注实际版本。

| Action | 原引用 | commit SHA | 解析依据 |
| --- | --- | --- | --- |
| `actions/checkout` | `@v4` | `11d5960a326750d5838078e36cf38b85af677262` | tag `v4` |
| `actions/setup-go` | `@v5` | `40f1582b2485089dde7abd97c1529aa768e1baff` | tag `v5` |
| `gitleaks/gitleaks-action` | `@v2` | `ff98106e4c7b2bc287b24eaf42907196329070c7` | annotated tag `v2` → commit |
| `bufbuild/buf-setup-action` | `@v1` | `a47c93e0b1648d5651a065437926377d060baa99` | 分支 `v1`（= tag `v1.50.0`） |
| `pnpm/action-setup` | `@v4` | `b906affcce14559ad1aafd4ab0e942779e9f58b1` | annotated tag `v4` → commit |
| `actions/setup-node` | `@v4` | `49933ea5288caeca8642d1e84afbd3f7d6820020` | tag `v4` |
| `actions/setup-python` | `@v5` | `a26af69be951a213d495a4c3e4e4022e16d87065` | tag `v5` |
| `actions/upload-artifact` | `@v4` | `ea165f8d65b6e75b540449e92b4886f43607fa02` | tag `v4` |

## 验证

### 1. 仓库内无浮动 tag

```
$ grep -rn "uses:.*@v[0-9]" .github/workflows/
（无结果）
$ grep -rc "uses:.*@[0-9a-f]\{40\}" .github/workflows/*.yml
.github/workflows/ci.yml:15
.github/workflows/deps.yml:5
```

### 2. YAML 仍可解析

```
$ ruby -ryaml -e 'YAML.load_file(ARGV[0])' .github/workflows/ci.yml .github/workflows/deps.yml
ci.yml: yaml-ok
deps.yml: yaml-ok
```

### 3. 逐条回查上游

对 8 个 action 重新查询 `git/ref` 并与 pinned SHA 比对：6 个 tag 直接相等；`gitleaks@v2` 与
`pnpm@v4` 是 annotated tag（ref 指向 tag object），解引用后 commit 相等；`buf-setup@v1` 为分支，
commit 相等。结论 `ALL_MATCH=True`。

### 4. 门禁与负向用例

新增 `make deps-actions-check` 并接入 `deps.yml` 的 check job。临时放入一个
`uses: actions/checkout@v4` 的 workflow：

```
$ make deps-actions-check
.github/workflows/_tmp-floating.yml:7:      - uses: actions/checkout@v4
GitHub Actions 引用必须是 commit SHA（可保留行尾 # vN 注释），不要用浮动 tag
make: *** [deps-actions-check] Error 1
```

删除临时文件后 `actions-pinned=yes`。

## 剩余风险

- 上游可能在未来移动 `v1` 分支；本仓库已固定到具体 commit，不会被动跟随。Renovate 的
  `helpers:pinGitHubActionDigests` 会按 SHA 更新提 PR，仍走人工评审。
- 固定 SHA 后不再自动获得 action 的 patch 修复，需依赖 Renovate 提示；这是 6.2 的既定取舍。
