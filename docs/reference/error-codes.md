# 错误码与错误响应

所有外部 API 使用统一错误信封：

```json
{
  "error": {
    "code": "not_found",
    "message": "route not found",
    "metadata": {},
    "retryable": false,
    "request_id": "..."
  }
}
```

| code | HTTP | 使用场景 |
|---|---:|---|
| `bad_request` | 400 | 参数格式错误 |
| `invalid_argument` | 400 | 语义校验失败 |
| `unauthorized` | 401 | 缺少/无效 JWT |
| `forbidden` | 403 | 权限不足 |
| `not_found` | 404 | 路由或资源不存在 |
| `conflict` | 409 | 并发/状态冲突 |
| `already_exists` | 409 | 唯一键冲突 |
| `unavailable` | 503 | 依赖不可用 |
| `not_implemented` | 501 | 尚未实现的能力 |
| `internal` | 500 | 未预期错误，消息固定为 `internal server error` |

实现位置：`internal/platform/httperr/errors.go`。5xx 响应不得包含 SQL、堆栈或内部错误文本。
