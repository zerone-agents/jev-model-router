# 快速开始

[English](README.md) | 简体中文

通过 Docker Compose 部署 Jev Model Router。单个容器提供 API 和管理 UI，命名卷持久化 SQLite。需要 Docker（含 Compose）和 Git。

## 启动（推荐）

默认从 Docker Hub 拉取 `zeroneai/jev-model-router:latest`，无需安装 Go、Node 或本地编译。此方式要求镜像已发布；若镜像尚不可用，请使用下方源码构建覆盖文件。

```sh
git clone https://github.com/zerone-agents/jev-model-router.git
cd jev-model-router/quickstart
cp .env.example .env
```

编辑 `.env`：分别运行两次 `openssl rand -hex 32`，为 `JEV_ROUTER_SETTINGS_TOKEN` 和 `JEV_ROUTER_INFERENCE_TOKEN` 设置**不同的**随机凭证。配置生成供应商和 TypeSafe 时填入相应 API Key。`.env` 已被 Git 和镜像构建忽略，请妥善保管。更多供应商的环境变量需要同时添加到 Compose 和 `.env`。

```sh
docker compose up -d
docker compose ps
```

打开 http://127.0.0.1:8080/dashboard/ 并输入 settings 凭证。推理 API base URL 为 `http://127.0.0.1:8080/v1`，客户端使用 inference 凭证。通过 `ROUTER_PORT` 修改宿主机端口；`ROUTER_IMAGE` 接受包含版本标签或 digest 的完整镜像名。

默认仅绑定本机。远程访问需要配置 HTTPS 反向代理并明确调整网络绑定；非 loopback 地址的管理 UI 要求 HTTPS。

## 配置模型

初始数据库没有供应商和模型。容器健康检查仅确认管理 API 可访问，不代表上游健康或路由已就绪。

```sh
docker compose exec -T router jev-router schema
docker compose exec -T router jev-router schema providers.put
docker compose exec -T router jev-router call status.get --json - <<'JSON'
{}
JSON
```

每次写入使用读取到的当前版本。供应商输入示例：

```json
{"id":"cloud","base_url":"https://api.openai.com/v1","secret_ref":"env:PROVIDER_API_KEY"}
```

保存为宿主机上的 `provider.json`，通过 stdin 传入（将 `VERSION` 替换为当前版本）：

```sh
docker compose exec -T router jev-router call providers.put \
  --json - --expected-version VERSION --idempotency-key setup-provider-1 < provider.json
```

按[配套 SKILL](../skills/jev-router/SKILL.md)创建禁用模型、测试并启用。多候选 `auto` 还需通过 `decision.put` 配置 `https://api.typesafe.ai`、`jev-1.13.0` 和 `env:TYPESAFE_API_KEY`。配置仍通过共享管理 API 保存；`.env` 不会创建模型资源。连接测试和路由检查可能调用付费服务。

容器中的 `localhost` 指容器自身，上游应使用可达的主机名，不能直接照搬宿主机 loopback 地址。修改环境密钥后执行 `docker compose up -d` 重建服务。

## 从源码构建

```sh
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

从仓库根目录构建前端和 Go 程序，使用 `jev-model-router:local`，不覆盖发布镜像的标签。

## 运维

```sh
docker compose logs -f router
docker compose pull                 # 拉取发布镜像
docker compose up -d                # 使用新镜像重建，保留数据
docker compose down                 # 停止，保留 SQLite 卷
```

`docker compose down -v` 会删除数据库卷，包括配置和记录。升级前应停止服务并备份卷；不要让多个实例共享同一 SQLite 卷。文件密钥引用和其他参数见[启动配置](../docs/configuration.md)。

## 镜像发布（维护者）

Docker image 工作流构建 Linux amd64/arm64 镜像。需配置具有 `zeroneai/jev-model-router` 推送权限的仓库 secrets：`DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。推送 `v*` 版本标签会发布版本镜像，正式版本同时更新 `latest`；手动触发会从所选 ref 发布 `latest`。PR 仅构建、不发布。首次发布需要配置凭证并明确执行发布操作，添加工作流本身不会发布镜像。
