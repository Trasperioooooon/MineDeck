---
title: CLI Flags & Headless Operation
description: Run MineDeck without a window (-nogui), minimized to system tray (-minimized), on a custom port (-port), or as a stdio MCP bridge (-mcp).
order: 50
since: v3.0.0
beta: false
featured: false
tags: ["CLI", "flags", "nogui", "minimized", "port", "mcp", "run.bat", "headless"]
updated: 2026-11-18
aliases: []
draft: false
---

For ordinary desktop hosting you just double-click `MineDeck.exe`. When running headless servers, auto-starting minimized, pinning the panel port, or wiring stdio MCP clients, pass command-line flags.

## Supported flags {#flags}

| Flag | Behavior | Common use case |
| --- | --- | --- |
| `-nogui` | Starts backend services and web panel without opening a WebView2 window | Running headless on a dedicated Windows host / using LAN mobile remote |
| `-minimized` | Launches directly into the Windows system tray without stealing focus | Added automatically by "Start with Windows" |
| `-port <n>` | Sets preferred local web UI port (default `27333`; picks a random port if busy) | Keeping bookmarks / local proxy ports predictable |
| `-mcp` | Starts a stdio JSON-RPC bridge forwarding to an already-running `MineDeck.exe` in the same folder | Subprocess transport for clients such as Claude Desktop |

## Using `-nogui` & finding the session token {#nogui}

Because release builds of `MineDeck.exe` target the Windows GUI subsystem, console logs are not printed back into interactive PowerShell/CMD windows unless stderr is redirected to a file:

```powershell
.\MineDeck.exe -nogui 2> minedeck-out.log
```

Open `minedeck-out.log` after a few seconds to locate the line `MineDeck 已启动: http://127.0.0.1:27333/?token=...` containing the single-run localhost URL and session `token`.

> [!WARNING]
> Treat the localhost `token` printed in `-nogui` logs as a private per-boot host credential. For phones or other computers on your LAN, enable password-protected **「手机远程管理」** instead of sharing host tokens.

## System tray auto-start (`-minimized`) {#minimized}

Enabling auto-start under **「设置 → 启动与托盘」** writes a `HKCU\...\Run` entry executing `"<your-folder>\MineDeck.exe" -minimized`. Right-clicking the tray icon lets you reopen the window or stop all servers and exit cleanly. Legacy `AutoMCHUB` startup entries are cleaned up automatically.

## Using `-mcp` as a stdio bridge {#mcp-bridge}

External AI tools supporting stdio MCP can invoke `MineDeck.exe -mcp` directly:

- Requires the main `MineDeck.exe` from the **same folder** to already be running with **MCP interface** turned on;
- Reads `data\mcp\runtime.json` next to `MineDeck.exe` to locate the live HTTP `/mcp` port, so it keeps working even if MineDeck changes ports or you reset the HTTP MCP token;
- Write actions requested over MCP remain queued as confirmation cards inside the host MineDeck window.

## Per-server `run.bat` scripts {#run-bat}

Inside every created, imported, or adopted server directory, MineDeck generates a `run.bat` matching that instance's **「设置 → 启动与内存」** configuration. If you launch a server directly with `run.bat` outside MineDeck, avoid triggering Map Import or Backup Restore on that instance inside MineDeck at the same time to prevent file lock conflicts.
