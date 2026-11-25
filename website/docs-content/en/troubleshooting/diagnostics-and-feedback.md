---
title: Gathering Full Error Info and Submitting Feedback
description: Copy raw error stacks from the UI, locate latest.log and crash-reports folders, and redact secrets before posting community bug reports.
order: 40
since: v3.0.0
beta: false
featured: false
tags: ["Error Info", "Details", "Copy Error", "Logs", "crash-reports", "Feedback", "Bug Report", "Redaction"]
updated: 2026-11-18
aliases: []
draft: false
---

When you hit a crash or error not immediately solved by built-in banners, sharing "what you clicked + the complete uncropped log or error stack" lets friends, AI, or maintainers pinpoint the root cause fast.

## 1. Copy Full Error Details Straight from the UI {#ui-copy}

- **Toast & error modal actions**: When an error toast pops up, click **「详情」** (Details) to read the uncropped message and use **「复制报错信息」** (Copy Error Info)—this copies full backend diagnostics and context rather than a truncated summary.
- **Background task logs**: When creating a server or importing a modpack/handover pack fails, open the task details to view installer output and download retry history (MineDeck keeps the crucial first 40 lines even on long logs).
- **Send to AI**: With an AI provider configured, click **「让 AI 分析」** (Ask AI to Analyze) beside the error to bundle context cleanly.

## 2. Locating Full Log Files on Disk {#log-files}

Click **「打开目录」** (Open Folder) in a server's header (desktop only) to jump straight to key folders:

| Entry / Path | Contents | When Useful |
| --- | --- | --- |
| **Server `<root>\logs\latest.log`** | Complete current/last run server log (uncropped, original encoding intact) | Boot failures, plugin/mod exceptions, player disconnects |
| **Server `<root>\crash-reports\`** | Timestamped `.txt` crash reports dumped by Minecraft / mods | Sudden crash during startup or world load |
| **`<MineDeck>\logs\ops\`** | Redacted audit log of UI edits, schedules, AI, and MCP confirmations | Tracing when or who triggered a start, stop, or config edit |

## 3. Redact Secrets Before Posting Reports {#feedback}

Use the website [Feedback](/feedback/) page to format environment details and sanitized templates cleanly (you can copy or download a structured report even without a GitHub account).

> [!CAUTION]
> **Double-check 3 secret categories before sharing screenshots or logs publicly**:
> 1. **API Keys** (AI provider Keys, CurseForge API Keys);
> 2. **Tunnel & Auth Credentials** (OpenFrp/SakuraFrp tokens, custom `frps` tokens, Phone Remote Admin password, `rcon.password`);
> 3. Blur desktop usernames in paths if desired, and never paste `-nogui` browser launch links containing your local session `token`.

Include these 4 essentials in every report:
1. **Steps taken** (buttons clicked, loader, and Minecraft version)
2. **Expected behavior**
3. **Actual result** (paste "Copy Error Info" text or relevant `latest.log` / `crash-reports` excerpt)
4. **System info** (e.g. Windows 11 and MineDeck version)
