---
title: Connect External AI Clients via MCP
description: Enable the local MCP endpoint to manage MineDeck from Claude Desktop, Cherry Studio, Cursor, VS Code, or Codex while keeping local confirmation locks on all writes.
order: 30
since: v3.0.0
beta: false
featured: false
tags: ["MCP", "Claude Desktop", "Cherry Studio", "Cursor", "VS Code", "Codex", "stdio", "Streamable HTTP", "-mcp"]
updated: 2026-11-18
aliases: []
draft: false
---

If you already subscribe to or prefer desktop AI tools (such as **Codex, Claude Desktop, Cherry Studio, Cursor, or VS Code**), turning on **「设置 → AI（可选）→ MCP 接口」** (Settings → AI (Optional) → MCP Interface) lets those clients connect directly to MineDeck running on the same PC to inspect servers, read logs, and propose config edits—using **the external client's own model and quota without configuring an API Key inside MineDeck**.

> [!NOTE]
> **Disabled by Default & Client Testing Coverage**
> - The MCP interface is **disabled by default** and accepts connections **only from localhost** (`127.0.0.1` / `localhost` / `[::1]`), rejecting all LAN or Internet requests.
> - During development and testing, the MCP integration was primarily verified end-to-end against **Codex**. For **Claude Desktop, Cherry Studio, Cursor, and VS Code**, MineDeck generates one-click copy snippets strictly matching each tool's official MCP config schema, though **full desktop launches on every third-party client have not been individually verified end-to-end**.

## Choosing Between the Two Transports {#transports}

| Transport | How It Works | Pros & Notes |
| --- | --- | --- |
| **stdio** (recommended for desktop apps) | The client spawns `MineDeck.exe -mcp` as a local subprocess bridge | Requires **no port and no HTTP Bearer token**; if MineDeck switches ports or rotates tokens later, external tools **need no config update** (requires the main `MineDeck.exe` app to be running) |
| **Streamable HTTP** | External clients connect directly to `http://127.0.0.1:<port>/mcp` with a `Bearer` token | No client-spawned subprocess needed; if MineDeck binds an alternate port due to conflict (shown in the UI) or you click **「重置令牌」** (Reset Token), update your HTTP client config |

## Enable and Copy Ready-to-Use Configs {#setup}

1. On the local desktop UI, open **「设置 → AI（可选）→ MCP 接口」** and enable **「开启 MCP 接口」** (Enable MCP Interface) (LAN devices can neither view the token nor toggle this setting).
2. Click **「复制配置…」** (Copy Config…) on that row and pick your target client to inspect paste instructions and copy the generated JSON:
   - **Claude Desktop** (stdio): Paste into `claude_desktop_config.json` (accessible via Claude's Settings → Developer → Edit Config), save, and fully restart Claude Desktop.
   - **Cherry Studio** (HTTP): Use Settings → MCP Servers → Add → Import from JSON and paste.
   - **Cursor** (HTTP): Open Cursor's `mcp.json` and merge `minedeck` into `mcpServers`.
   - **VS Code** (HTTP): Run `MCP: Open User Configuration` from the Command Palette and merge `minedeck` into `servers`.
   - **Generic HTTP / stdio**: For Codex or other compatible tools, copy the endpoint URL, authorization header, or `-mcp` command arguments shown in the menu.
3. Ask your connected AI tool "Which servers are in MineDeck?"—below the toggle, **「连过的 AI 工具」** (Connected AI Tools) refreshes every 3 seconds to list connected client names, transports, and tool call counts.

### Sample Config Structure

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

## Security Boundary: Every Write Must Be Confirmed Inside MineDeck {#safety}

1. **Read-only calls return immediately; writes create confirmation cards**. Server lists, logs, and config files returned to external MCP tools pass through rule-based redaction (replacing known secrets, program directories, user directories, and fixed patterns; inspect output yourself because not every sensitive string is guaranteed to be matched). Editing configs, starting/stopping servers, or adding/removing mods only produces a pending proposal (up to 10 pending proposals queued at once), which executes only when you click Confirm at the top of the **「AI 助手」** (AI Assistant) panel inside MineDeck's desktop window.
2. **LAN devices cannot confirm MCP proposals**: Even with Phone & LAN Remote Management enabled, write proposals raised by MCP tools can **only be confirmed or rejected while sitting at the host PC**, and every executed change logs the client name into `logs\ops\` audit logs.
3. **Human-only operations remain hidden**: Operations like exporting modpacks, replacing worlds, adopting folders, adding local `.jar` files, or changing storage/Java/AI paths are excluded from the exposed MCP tool list entirely.
4. **Token management**: If you suspect the HTTP token leaked—or see a token-decryption error after switching PCs or Windows accounts—click **「重置令牌」** (Reset Token) to invalidate the old token immediately. Turning off **「开启 MCP 接口」** (Enable MCP Interface) stops the endpoint right away and cancels all pending proposals.
