# action-temporal CI job 补丁

> 供整合变更合并进 `.github/workflows/ci.yml`；不直接修改主 CI 文件。

```yaml
  action-temporal-e2e:
    if: github.event_name == 'workflow_dispatch' || contains(github.event.pull_request.labels.*.name, 'action-temporal-e2e')
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.3'
      - name: Run action-temporal smoke
        env:
          SKIP_START: "0"
          STOP_STACK: "1"
        run: ./scripts/actions-temporal-smoke.sh
      - name: Upload smoke logs
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: action-temporal-smoke
          path: /tmp/actions-smoke-results.json
```

说明：
- 使用 label `action-temporal-e2e` 或手动触发，避免默认 PR 流程拉起重容器 profile。
- smoke 脚本自带 compose profile 启动、readyz 等待、7 条断言与栈清理。
- 若 CI runner 不支持 Docker-in-Docker，应在专用 self-hosted runner 上启用；失败不阻塞主 CI。
