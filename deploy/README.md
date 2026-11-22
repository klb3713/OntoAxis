# 部署与回滚

## 镜像

```sh
core_tag="$(git rev-parse --short HEAD)"
web_tag="$(git rev-parse --short HEAD)"
docker build -t "ontoaxis-core:${core_tag}" .
docker build -f web/Dockerfile -t "ontoaxis-web:${web_tag}" web
```

镜像构建是确定性的：相同源码、相同基础镜像与相同构建参数重复执行时，`docker build` 返回相同 image id（Stage 1 已验证两次构建 ID 一致）。

## 本地 Compose

```sh
docker compose -f deploy/compose/compose.yaml up -d --build
```

| 服务 | 宿主端口 |
| --- | --- |
| web | 18082 |
| core | 18080 |
| core metrics | 19090 |
| Keycloak | 18083 |
| Trino | 18081 |
| PostgreSQL | 15433 |

健康检查：

```sh
curl -fsS http://127.0.0.1:18080/healthz
curl -fsS http://127.0.0.1:18080/readyz
```

## 回滚

1. 回滚前先停止写入并备份数据库：`docker compose -f deploy/compose/compose.yaml exec postgres pg_dump ...`。
2. 将 `deploy/compose/compose.yaml` 中 `ontoaxis-core` / `web` 的 `image` 固定到上一个已验证 tag（不要依赖 `latest`）。
3. `docker compose -f deploy/compose/compose.yaml up -d --no-build ontoaxis-core web`。
4. 校验 `/healthz`、`/readyz` 和关键业务 smoke；迁移只向前执行，若需回滚 schema 必须使用备份恢复，不要手工改迁移历史。
5. 记录回滚 tag、数据库备份路径、时间和执行人。

## 注意

- Compose 中 `ONTOAXIS_TRUST_GATEWAY=true` 仅用于本地/受控网络边界；生产由 `oa-edge` 剥离并重写内部身份头后再转发。
- 不要在宿主机直接暴露 core 的 18080；本地端口映射仅用于验收。
