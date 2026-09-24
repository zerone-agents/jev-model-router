# Agent CLI

将命令和输入映射为管理能力调用；通过运行实例的管理 API 完成业务读取与写入。负责 schema 发现、JSON/JSON Lines、稳定退出码及 stderr 日志，不复制管理逻辑。

配套工作流位于 `skills/jev-router/SKILL.md`。仅暴露真实实现的能力；当前没有可运行命令。进程启动通过 app 装配，不绕过管理服务更新配置。
