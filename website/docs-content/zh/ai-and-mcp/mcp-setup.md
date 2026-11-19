---
title: MCP 接口接入外部 AI 客户端
description: 开启本机 MCP 接口，用 Claude Desktop、Cherry Studio、Cursor、VS Code 或 Codex 管理 MineDeck，且写操作均保留本机确认锁。
order: 30
since: v3.0.0
beta: false
featured: false
tags: ["MCP", "Claude Desktop", "Cherry Studio", "Cursor", "VS Code", "Codex", "stdio", "Streamable HTTP", "-mcp"]
updated: 2026-11-18
aliases: []
draft: false
---

如果你自己订阅了或习惯用桌面上的外部 AI 工具（例如 **Codex、Claude Desktop、Cherry Studio、Cursor、VS Code**），开启全局 **「设置 → AI（可选）→ MCP 接口」** 后，这些外部 AI 客户端可以直接连上同一台电脑运行着的 MineDeck 查看服务器、读日志、改配置建议——而且 **完全走外部客户端自己的模型与用量，不需要在 MineDeck 里配 API Key**。

> [!NOTE]
> **默认关闭与客户端校验覆盖说明**
> - MCP 接口 **默认关闭**；仅接收来自本机（`127.0.0.1` / `localhost` / `[::1]`）的请求，拒绝一切局域网或外网访问。
> - 目前 MCP 接口在开发与测试期间重点实机验证了 **Codex** 接入链路；针对 **Claude Desktop、Cherry Studio、Cursor 与 VS Code**，程序严格按各家官方 MCP 配置规格生成一键复制片段，**尚未在真机桌面逐一开机测遍全流程**。

## 两种连接方式怎么选 {#transports}

| 方式 | 原理 | 优点与注意事项 |
| --- | --- | --- |
| **stdio 方式**（推荐用于桌面客户端） | 客户端把 `MineDeck.exe -mcp` 当作本地子进程运行转发 | 界面无需填端口也**不用配 HTTP 令牌**；日后 MineDeck 换了本地端口或重置令牌，外部工具**不用跟着改配置**（前提：主程序 `MineDeck.exe` 须正在运行） |
| **Streamable HTTP 方式** | 外部客户端直连 `http://127.0.0.1:<端口>/mcp` 并带 `Bearer` 令牌 | 无需由客户端拉起子进程；若主程序启动时想用的默认端口被占用而切换过端口（界面会提示），或点击过 **「重置令牌」**，HTTP 客户端的配置需要跟着重新复制 |

## 一键启用并复制配置 {#setup}

1. 在电脑本机打开 **「设置 → AI（可选）→ MCP 接口」**，开启 **「开启 MCP 接口」** 开关（仅限本机界面操作，局域网设备不能看令牌也不能操作此开关）。
2. 在这一行点击 **「复制配置…」**，从菜单选出你要接入的工具查看粘贴说明并复制现成 JSON：
   - **Claude Desktop**（stdio）：粘贴进 `claude_desktop_config.json`（路径通常在 Claude 的「设置 → 开发者 → 编辑配置」），保存后完全退出并重开 Claude Desktop。
   - **Cherry Studio**（HTTP）：在「设置 → MCP 服务器 → 添加 → 从 JSON 导入」粘贴即可。
   - **Cursor**（HTTP）：打开 Cursor MCP 配置 `mcp.json` 将 `minedeck` 合并进 `mcpServers`。
   - **VS Code**（HTTP）：命令面板运行 `MCP: Open User Configuration` 将 `minedeck` 合并进 `servers`。
   - **通用 HTTP / stdio**：如使用 Codex 或其他兼容工具，可按复制菜单展示的地址、请求头或 `-mcp` 启动命令填入对应配置。
3. 接通并在外部 AI 工具问一句「MineDeck 里有哪些服务器」，页面下方 **「连过的 AI 工具」** 会每 3 秒自动刷新显示已连接的客户端名称、协议与调用工具次数。

### 配置结构示意

```json
{
  "mcpServers": {
    "minedeck": {
      "command": "D:\\Games\\MineDeck\\MineDeck.exe",
      "args": ["-mcp"]
    }
  }
}
```

## 外部 AI 的安全边界：所有改写必须回到 MineDeck 点确认 {#safety}

1. 读操作直接返回，写操作只出卡片。外部工具读取服务器列表、日志、配置时，文本按规则脱敏，只替换已知的密钥、程序目录、用户目录和几种固定格式，不保证抹掉所有敏感内容，发送前自己看一眼。修改配置、启停服务器、删除或加装模组，都只会先生成一条待确认提案，同时最多挂 10 条未处理提案。要本人到 MineDeck 桌面窗口右上角 **「AI 助手」** 顶部的确认卡片里点了才执行。
2. **局域网设备不可代确认 MCP 提案**：即使开了「手机远程管理」，外部 MCP 工具提出来的写操作也**只允许坐在这台电脑前亲自确认或拒绝**，且每步写入都带着客户端名记入 `logs\ops\` 审计文件。
3. **「仅人工」能力完全隔离**：导出整合包、换地图、接管目录、添加本地 `.jar` 或变更存储/Java/AI 路径这类能力，连工具清单都不会暴露给 MCP。
4. **令牌安全**：如怀疑 HTTP 令牌泄露，点击 **「重置令牌」** 立即作废旧令牌；换电脑或换 Windows 用户账号后遇到「令牌解不开」提示同样点重置即可（如不想继续被外部调用，关闭 **「开启 MCP 接口」** 开关会立即停止服务并作废所有未确认提案）。
