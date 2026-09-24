# 可执行入口

规划中的 `jev-router` 程序入口。仅负责启动参数、命令装配和进程退出，将业务行为交给 `internal/`。

当前没有 main 程序或可执行命令。一个 Go module 同时承载服务启动与 Agent CLI，不在入口实现路由规则。
