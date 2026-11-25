---
title: Console, Background Tasks, and Audit Logs
description: Stream live console logs, switch UTF-8/GBK encoding, use command Tab completion, guard online players before stopping, track background tasks, and inspect redacted audit logs.
order: 10
since: v3.0.0
beta: false
featured: false
tags: ["Console", "Logs", "GBK", "UTF-8", "Tab Completion", "Background Tasks", "Audit Log", "logs/ops", "Open Folder"]
updated: 2026-11-18
aliases: []
draft: false
---

Opening any server defaults to the **「控制台」** (Console) tab—your central hub for start/stop control, live log streaming, command entry, and crash diagnostics.

## Core Console Features {#console}

- **Live logs and history replay**: Automatically loads recent history when opened and streams new lines via SSE (Server-Sent Events); `WARN` and `ERROR` lines are color-highlighted, with keyword filtering (helpful for tracking a specific player or exception) and font-size controls.
- **UTF-8 / GBK encoding toggle**: Defaults to **UTF-8**; if legacy Windows plugins or wrapper scripts print garbled Chinese text, switch the log toolbar to **GBK** (remembered per server).
- **Command bar and Tab completion**: Press Up/Down arrows to cycle command history, and press **`Tab`** to autocomplete standard vanilla Minecraft commands as well as **currently online player names** (no leading `/` needed).
- **Header shortcuts and error banners**:
  - Displays the server's LAN **Connect Address** (**连接地址**) in the header (click to copy; use **「···」** to switch across multiple NIC IPs).
  - The **「打开目录」** (Open Folder) dropdown (local desktop only) opens the instance root or jumps straight into `mods`, `plugins`, `world`, `logs`, or `crash-reports` in File Explorer.
  - When a server fails to launch or crashes, a diagnostic banner appears at the top of the console with a **「让 AI 分析」** (Ask AI to Analyze) shortcut—see the [Console Error Banner Index](../troubleshooting/error-banner-index.md).

## Graceful Shutdown, Restart Guard, and Port Checks {#safe-stop}

- **Confirmation when players are online**: For non-proxy servers, clicking **「停止」** (Stop) or **「重启」** (Restart) while players are connected opens a confirmation dialog ("N players are currently online") before sending save + stop commands, preventing rollback loss.
- **Graceful exit and process cleanup**: Stopping sends `stop` first and waits up to ~`30 seconds` before force-terminating a hung process. Exiting MineDeck stops all running servers and auto-started tunnels first (Java and `frpc` subprocesses are bound to a Windows Job Object so no orphan processes stay behind).
- **Pre-launch port probe**: Before launching Java, MineDeck attempts to bind the configured `server-ip` and `server-port`; if another app or server holds the port, startup aborts immediately with a **「改端口」** (Change Port) button or suggested free port (skipped for proxies or port `0`).

## Tracking Background Tasks {#tasks}

Long-running jobs such as server creation, Java downloads, modpack imports, and `.mdpack` handover exports/imports run as background tasks:

- **Navigate freely**: Open **Tasks** (**任务列表**) at the bottom or top right anytime to inspect step-by-step progress and logs; long installer logs preserve the first 40 lines plus recent tail lines.
- **Slow-download notice (30s) and timeout snapshots**: If a transfer stalls for 30 seconds without progress, the task card notes slow network or automatic retry behavior, and failure snapshots record the last contacted host.
- **Cancellation**: Creating servers, importing modpacks, and importing handover packs can be canceled during safe stages once staging cleanup completes (**note**: files on disk remain intact across MineDeck restarts, though completed pre-restart task summaries clear on relaunch).

## Audit Logs (`logs/ops/`) {#ops-logs}

To help co-admins and troubleshooting trace past changes, UI mutations, scheduled tasks, crash auto-restarts, confirmed **AI Assistant** actions, and confirmed **MCP client** actions are recorded in `<MineDeck>\logs\ops\`:

- Purely read-only page views and log reads are not logged.
- Any API Keys, passwords, access tokens, Webhook URLs, or sensitive `server.properties` fields (such as `rcon.password`) are automatically masked as `***` before writing to disk.
