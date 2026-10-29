# 快速开始

[English](README.md) | 简体中文

通过 Docker Compose 部署 Jev Model Router。单个容器提供 API 和管理 UI，命名卷持久化 SQLite。需要 Docker（含 Compose）和 Git。

## 启动（推荐）

默认从 Docker Hub 拉取 `zeroneai/jev-model-router:latest`，无需安装 Go、Node 或本地编译。要固定本文对应版本，在 `.env` 设置 `ROUTER_IMAGE=zeroneai/jev-model-router:0.1.27`。

```sh
git clone https://github.com/zerone-agents/jev-model-router.git
cd jev-model-router/quickstart
cp .env.example .env
```

编辑 `.env`：分别运行三次 `openssl rand -hex 32`，设置不同的 `JEV_ROUTER_SETTINGS_TOKEN`、`JEV_ROUTER_INFERENCE_TOKEN` 和 `JEV_ROUTER_ENCRYPTION_KEY`。前两项用于客户端认证，第三项用于加密托管模型凭证，必须与 SQLite 分开备份。执行 `chmod 600 .env`。该文件已被 Git 和镜像构建忽略，请妥善保管。生成与决策模型的 API Key 推荐启动后通过 CLI/API 配置，不再默认填入 `.env`。
```sh
docker compose up -d
docker compose ps
```

打开 http://127.0.0.1:8080/dashboard/ 并输入 settings 凭证。推理 API base URL 为 `http://127.0.0.1:8080/v1`，客户端使用 inference 凭证。通过 `ROUTER_PORT` 修改宿主机端口；`ROUTER_IMAGE` 接受包含版本标签或 digest 的完整镜像名。

默认仅绑定本机。远程访问需要配置 HTTPS 反向代理并明确调整网络绑定；非 loopback 地址的管理 UI 要求 HTTPS。

## 远程管理 API Key

先分别查询 `jev-router schema providers.put` 和 `jev-router schema decision.put`，确认均支持只写的 `api_key`。旧发布镜像可能仅支持供应商密钥托管；若 `decision.put` 尚无此字段，请升级到包含该功能的版本，或使用下方源码构建方式。不要假定 `latest` 已包含尚未发布的改动。

启动时配置加密主密钥后，通过私有 JSON 文件或 stdin 添加、轮换生成与决策 API Key，`api_key` 和 `secret_ref` 二选一。后续托管密钥变更无需修改 Compose 或重启。现有 `env:`/`file:` 引用继续支持；Compose 保留可选 `PROVIDER_API_KEY` 和 `TYPESAFE_API_KEY` 映射，仅用于兼容旧部署。更多外部环境变量须同时写入 `.env` 并显式映射到 Compose；已有引用迁移完成前请保留其变量。

默认请用户自行在命令行配置密钥。只有用户明确确认环境安全、无泄漏风险，并明确将 API Key 提供给 Agent 用于配置时，Agent 才协助操作。不要提交密钥或分享含密钥的 Compose 渲染结果；加密主密钥应单独备份，丢失后无法恢复托管密钥。
详见[远程 CLI 操作](../docs/cli-installation.zh-CN.md#托管供应商密钥)与[加密及恢复说明](../docs/configuration.md#managed-provider-credentials)。远程提交密钥要求 HTTPS，也可经 SSH 隧道使用回环 HTTP。反向代理终止 TLS 后应通过私有网络转发至容器。接口不返回明文密钥。旧密文版本会保留供在途请求使用；删除供应商不会清除历史密文及备份。尚未实现主密钥轮换，直接改值会导致存在托管记录的实例启动失败。

## 配置模型

初始数据库没有供应商和模型。容器健康检查仅确认管理 API 可访问，不代表上游健康或路由已就绪。

```sh
docker compose exec -T router jev-router schema
docker compose exec -T router jev-router schema providers.put
docker compose exec -T router jev-router schema decision.put
docker compose exec -T router jev-router call status.get --json - <<'JSON'
{}
JSON
```

每次写入使用读取到的当前版本。供应商输入示例：

```json
{"id":"cloud","base_url":"https://api.openai.com/v1","api_key":"YOUR_PROVIDER_API_KEY"}
```

请自行在终端创建私有 `provider.json`（例如先执行 `umask 077`），在本地替换示例占位值，再通过 stdin 传入。真实密钥不要放进命令参数、Shell 历史或聊天中；将 `VERSION` 替换为当前配置版本：

```sh
docker compose exec -T router jev-router call providers.put \
  --json - --expected-version VERSION --idempotency-key setup-provider-1 < provider.json
```

按[配套 SKILL](../skills/jev-router/SKILL.md)创建禁用模型、测试并启用。多候选 `auto` 还需配置 `decision.put`。先发现 schema，再创建私有 `decision.json`，包含 `base_url`、`model` 和只写的 `api_key`；第三方 Jev 兼容服务按文档填写 `path`。官网使用 `https://api.typesafe.ai` 和 `jev-1.13.0`。重新读取版本后，用 `docker compose exec -T router jev-router call decision.put --json - --expected-version VERSION --idempotency-key setup-decision-1 < decision.json` 提交。确认成功后删除本地凭证文件。配置仍通过共享管理 API 保存；`.env` 不会创建模型资源。连接测试和路由检查可能调用付费服务。

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

Release 工作流构建 Linux amd64/arm64 镜像。必填 Actions Variable `DOCKERHUB_IMAGE` 设为 `zeroneai/jev-model-router`（namespace/repository，不带 registry host 或标签）。需配置具有 `zeroneai/jev-model-router` 推送权限的仓库 secrets：`DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。推送 `v*` 版本标签会发布版本镜像，正式版本同时更新 `latest`；手动触发会从所选 ref 发布 `latest`。PR 仅运行本地容器冒烟测试，跳过镜像发布任务。首次发布需要配置凭证并明确执行发布操作，添加工作流本身不会发布镜像。

## 华为云 SWR 镜像

Docker Hub 访问困难时，在 `.env` 中将 `ROUTER_IMAGE` 改为已发布的 SWR 镜像，再运行 `docker compose pull && docker compose up -d`。目标示例（实际可用性取决于维护者配置的地址和首次发布）：

```dotenv
ROUTER_IMAGE=swr.cn-east-3.myhuaweicloud.com/zerone/jev-model-router:latest
```

维护者沿用 Agent Hub 的仓库变量及 secret 名称：

| 类型 | 名称 | 值 |
| --- | --- | --- |
| Variable | `DOCKERHUB_IMAGE` | Docker Hub namespace/repository，如 `zeroneai/jev-model-router` |
| Variable | `REGISTRY_HOST` | SWR 域名，如 `swr.cn-east-3.myhuaweicloud.com` |
| Variable | `REGISTRY_IMAGE` | 不带标签的完整镜像路径，如 `swr.cn-east-3.myhuaweicloud.com/zerone/jev-model-router` |
| Secret | `DOCKER_REGISTRY_USER` | SWR 登录用户名 |
| Secret | `DOCKER_REGISTRY_PASSWORD` | SWR 登录密码 |

配置完整后，Release 将同一次构建及相同版本/latest 标签推送到 Docker Hub 和 SWR。两项 SWR 变量均未配置时只发布 Docker Hub；配置不完整或地址不匹配会报错。任一仓库认证或推送失败都会导致发布失败，宣布可用前需核实两个仓库。PR 构建不登录、不发布。私有 SWR 仓库要求用户认证；无需登录的 Quickstart 应使用公开镜像。

### Dashboard 会话

远程管理页面需在 `.env` 配置 `JEV_ROUTER_DASHBOARD_ORIGIN=https://router.example.com` 并重建容器。反向代理负责 TLS、保留原始 Host，后端保持私网访问。未配置 Origin 时，浏览器登录仅允许回环 HTTP。登录有效期固定为 7 天，刷新和同凭证重启后保留；注销撤销当前请求会话。CLI 仍使用 Bearer。容量、凭证轮换及备份恢复见[会话部署说明](../docs/configuration.md#dashboard-sessions)。

## 推理协议

OpenAI 客户端使用 Router `/v1` base URL；Anthropic SDK 使用 Router 根地址。`/v1/chat/completions` 与 `/v1/messages` 均可在各自兼容边界内接入 OpenAI 兼容与原生 Anthropic 上游。见[调用示例](../README.zh-CN.md#调用推理接口)、[原生 Messages 兼容范围](../docs/compatibility.md#native-messages-clients-with-native-anthropic-upstream-51)和[原生 Anthropic/BigModel 配置](../docs/configuration.md#native-anthropic-generation-upstream)。
