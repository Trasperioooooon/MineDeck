---
title: World Backups, Restore, and Storage Paths
description: Create manual or live hot backups, restore worlds safely with automatic safety snapshots, and customize global backup folders and retention limits.
order: 30
since: v3.0.0
beta: false
featured: false
tags: ["Backups", "Restore", "Hot Backup", "backups", "Storage Paths", "Retention", "World & Backups", "level-name"]
updated: 2026-11-18
aliases: []
draft: false
---

Open a server's **「世界与备份 → 备份」** (World & Backups → Backups) tab (proxies exclude this tab) to snapshot the current world anytime, restore historical backups, or prune old archives.

> [!IMPORTANT]
> **Key boundary: world backups archive only the world folder, NOT `mods`, `plugins`, or `server.properties`.**
> If you plan to upgrade versions, overhaul your mod list, or migrate an entire server setup, world backups alone will not restore mods and configs—export a [Handover Pack (`.mdpack`)](../server-setup/adopt-and-handover.md) as well.

## Instant and Live Hot Backups {#backup}

- **Click 「立即备份」 (Backup Now) anytime**: Create `.zip` world backups whether the server is **stopped** or **running (hot backup)**.
- **Hot-backup safety guard**: If the server restarts mid-backup (manual restart, crash auto-restart, or stop-and-start), the in-flight backup is discarded as invalid so mismatched region files never form a corrupted archive.
- **Per-disk I/O serialization**: Backups, world copies, and archive extractions targeting the same physical drive queue sequentially to prevent disk thrashing, while tasks on separate drives run in parallel.

## Restoring a World Safely {#restore}

When griefing, redstone mishaps, or experimental builds break a world, choose an entry in the backup list and click **「还原」** (Restore):

1. If the server is running, MineDeck prompts you and gracefully saves and stops the server before starting restoration. **Once restored, the server stays stopped so you can inspect everything before clicking 「启动」 (Start).**
2. **Automatic pre-restore safety backup**: Before overwriting the active world, MineDeck **automatically creates one extra backup of your current world**, protecting you from picking the wrong timestamp by mistake.
3. **Scoped to the default world directory**: Even if `level-name` in `server.properties` was manually edited to point outside the server folder, backup and restore operate only on the default `world` path so unrelated directories cannot be wiped.

## Global Retention Limits and Custom Storage Paths {#storage}

Manage both settings under sidebar **「设置 → 存储位置」** (Settings → Storage Locations) (**host desktop only; LAN devices cannot alter local disk paths**):

- **Backups kept per server**: Defaults to **10** and can be tuned from **1 to 1000**. When exceeded, the oldest `.zip` is pruned automatically as new backups finish (**note: this is a global setting**, and [restore points](./map-import.md#restore-points) generated when replacing worlds do **not** count toward this cap—delete those manually).
- **Custom 「备份存放文件夹」 (Backup Folder) and 「服务器存放文件夹」 (Servers Folder)**:
  - Takes effect immediately, applying **only to newly created servers and newly generated backups**.
  - Existing server directories and `.zip` archives in old locations **are not moved**; MineDeck tracks previously used root paths (`roots` / `backupRoots`) so existing servers and backups remain listed, startable, and restorable seamlessly.
