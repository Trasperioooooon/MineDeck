---
title: Schedules, Crash Auto-Restart, and Webhooks
description: Schedule daily restarts, console commands, and auto-backups, configure backoff crash recovery, and stream 8 server events to external HTTP Webhooks.
order: 60
since: v3.0.0
beta: false
featured: false
tags: ["Schedules", "Auto-Restart", "Crash Recovery", "Auto-Backup", "Notifications & Integrations", "Webhook", "instance.crash", "player.join"]
updated: 2026-11-18
aliases: []
draft: false
---

Keep long-running servers low-maintenance by combining per-server **「定时任务」** (Schedules), **「崩溃自动重启」** (Crash Auto-Restart), and global **「通知与集成」** (Notifications & Integrations) Webhooks.

## 1. Per-Server Daily Schedules {#schedules}

Open a server's **「定时任务」** (Schedules) tab to configure daily automated rules:

- **Execution time**: Enter daily **`HH:MM`** (single digits like `4:5` normalize automatically to `04:05`).
- **Three supported action types**:
  1. **Scheduled Restart**: Ideal for overnight memory cleanup and fresh chunk GC; if the port hasn't released immediately on stop, MineDeck waits a few seconds before relaunching.
  2. **Send Console Command**: e.g. `say Server maintenance in 5 minutes` or `save-all`.
  3. **Scheduled World Backup**: Hardened in `v3.2.0-beta.5`—if the scheduled trigger hits while the server is transitional (`starting` or `stopping`), the backup **waits briefly until state settles** rather than failing outright, and logs the run in the audit log.

> [!NOTE]
> Scheduled tasks and Crash Auto-Restart settings are **not carried over** when migrating a server via [Handover Packs (`.mdpack`)](../server-setup/adopt-and-handover.md); recreate desired schedules on the destination PC.

## 2. Crash Auto-Restart {#auto-restart}

On the same **「定时任务」** tab, toggle **「崩溃自动重启」** (Crash Auto-Restart) (off by default):

- **Graduated backoff**: Relaunches an unexpectedly crashed server after **`10s` → `30s` → `60s`**; if **3 consecutive attempts fail to stay up**, auto-restart halts to avoid infinite CPU-thrashing boot loops.
- **Stable reset**: Staying alive cleanly for more than **10 minutes** resets the failure counter back to zero.
- **Never triggers on intentional stops**: Clicking **「停止」** (Stop) in the UI, typing `stop` in the console, running `/stop` in-game as OP, or clicking Start/Stop during a backoff countdown cancels auto-restart cleanly.
- **Linked tunnel continuity**: Any [tunnel set to Follow Server Start](../networking/tunnels-and-accounts.md) stays active across crash restarts.

## 3. Global Webhook Notifications {#webhook}

Under global **「设置 → 通知与集成」** (Settings → Notifications & Integrations), enter your external HTTP endpoint (such as a bot relay or status board). MineDeck sends `POST` JSON payloads on key events, retrying up to 3 times on transient network failures:

### Sample JSON Payload

```json
{
  "type": "player.join",
  "time": "2026-07-12T03:00:00+08:00",
  "data": { "instance": "My Server", "player": "Steve" }
}
```

### Supported Event Types (8 Total)

| Event `type` | Trigger |
| --- | --- |
| `instance.start` | A server finishes starting up |
| `instance.stop` | A server stops cleanly |
| `instance.crash` | A server exits abnormally |
| `player.join` | A player joins the server |
| `player.leave` | A player leaves the server |
| `backup.done` | A world backup finishes writing |
| `tunnel.up` | A tunnel comes online |
| `tunnel.down` | A tunnel stops |

> [!NOTE]
> **Webhook Token Privacy on LAN**: To avoid leaking secret query tokens or paths over LAN viewing, Phone & LAN Remote views under **「设置 → 通知与集成」** mask the path/query portion as `***` (e.g. `https://hook.example/***`). Saving without changes preserves the stored URL; editing from a remote device requires pasting the complete URL again (or editing directly on the host desktop).
