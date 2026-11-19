---
title: 启动参数与无窗口运行
description: 使用 -nogui、-minimized、-port 和 -mcp 命令行参数控制 MineDeck 运行方式，以及手动脚本 run.bat 的用法。
order: 50
since: v3.0.0
beta: false
featured: false
tags: ["命令行", "CLI", "nogui", "minimized", "port", "mcp", "后台运行", "run.bat", "token"]
updated: 2026-11-18
aliases: []
draft: false
---

日常使用只需直接双击 `MineDeck.exe`，但在需要挂机后台运行、更改本地面板端口或接入外部 MCP 客户端时，可以使用命令行参数启动。

## 支持的命令行参数 {#flags}

| 参数 | 作用说明 | 适用场景 |
| --- | --- | --- |
| `-nogui` | 只启动本地后台与 Web 服务，不打开桌面窗口 | 长期挂服、配合手机远程管理或仅通过浏览器访问面板 |
| `-minimized` | 启动后直接最小化进系统托盘，不抢占前台窗口 | 开机自启时自动携带，或挂到托盘静默待命 |
| `-port <端口>` | 指定本地网页面板监听端口（默认 `27333`） | 若默认端口与本机工具冲突时固定端口；若指定端口依然被占则自动换用系统随机端口 |
| `-mcp` | 不开窗口、不初始化实例，直接作为外部 AI 工具的 stdio 桥接子进程 | Claude Desktop 等只用标准输入输出（stdio）的 MCP 工具启动 |

## 使用 `-nogui` 无窗口模式 {#nogui}

因为正式发布版 `MineDeck.exe` 编译为 Windows 窗口程序，直接在命令行（PowerShell / CMD）里执行 `MineDeck.exe -nogui` 时**标准输出不会打印回当前控制台**，因此需要把标准错误日志重定向到文本文件中来查看本次随机生成的一性登录 `token` 与监听端口：

```powershell
.\MineDeck.exe -nogui 2> minedeck-out.log
```

等待几秒后查看 `minedeck-out.log`，里面会出现类似 `MineDeck 已启动: http://127.0.0.1:27333/?token=...` 的本机完整访问地址。

> [!WARNING]
> **权限与令牌提示**：该 URL 中的 `token` **仅对当前这一次运行有效**，并且只用于标识运行在本机的控制台会话；如果你开启了 **「手机远程管理」**，同一局域网下的其他设备请通过设定的访问密码登录（不要把本机 URL 和 `token` 直接转发给他人）。

## 开机自启与 `-minimized` {#minimized}

在 **「设置 → 启动与托盘」** 中打开 **「开机自动启动」** 时，MineDeck 会在当前用户的 Windows 启动项写入：

```text
"<你的目录>\MineDeck.exe" -minimized
```

这样每次开机登录桌面后，MineDeck 会自动缩在右下角系统托盘，不会弹出主窗口打扰操作；托盘右键菜单提供 **「打开 MineDeck」** 与 **「全部停止并退出」**。

## 使用 `-mcp` 接入外部工具 {#mcp-bridge}

支持标准输入输出（stdio）的外部 AI 客户端（例如 Claude Desktop）会在本地直接拉起 `MineDeck.exe -mcp` 作为子进程：

- 它**前提要求同一目录下的 `MineDeck.exe` 主程序已经在运行**，且在全局设置中打开了 **「MCP 接口」**；
- 桥接进程启动时会读取程序目录 `data\mcp\runtime.json`，确认进程与目录匹配后把消息转发给正在运行的本机端口；
- 因为不需要写死网页端口和 HTTP 令牌，即便以后重置了 MCP 令牌或重启更换了本地端口，stdio 客户端也无需重新修改配置文件。

详细接法见 [MCP 接口接入外部 AI 客户端](../ai-and-mcp/mcp-setup.md)。

## 服务器文件夹里的 `run.bat` {#run-bat}

每台创建、接管或导入完成的服务器目录内，MineDeck 都会维护一份与当前 **「设置 → 启动与内存」** 完全等效的启动脚本 `run.bat`。

- 如果你临时脱离 MineDeck 想快速拉起某一台单服调试，双击其目录下的 `run.bat` 即可用相同 Java 与 JVM 参数启动；
- 在外面手动运行着同一台服务器期间，**不要同时再回 MineDeck 触发「换地图」或「备份还原」**，防止世界文件夹出现文件锁冲突。
