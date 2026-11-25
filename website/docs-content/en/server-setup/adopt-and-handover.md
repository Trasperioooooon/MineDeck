---
title: Adopt Existing Servers and Handover Packs
description: Manage existing local server folders in-place with Adopt Server, or bundle an entire server into a .mdpack Handover Pack for the next host or a friend.
order: 40
since: v3.1.0
beta: false
featured: false
tags: ["Adopt Server", "Remove from List", "Delete Server Folder", "Handover Pack", "mdpack", "Export Handover", "Import Handover", "Slim Mode", "Full Mode", "Migrate"]
updated: 2026-11-18
aliases: []
draft: false
---

Whether you want to bring an existing manually launched server directory under MineDeck's UI, or package a running playthrough for someone else to take over, use **「接管已有服务器」** (Adopt Existing Server) and **「交接包（`.mdpack`）」** (Handover Packs).

## 1. Adopt an Existing Server (In-Place) {#adopt}

Entry: Sidebar **「新建服务器 → 接管已有服务器」** (Create Server → Adopt Existing Server) (requires browsing local host folders; not available from mobile LAN browsers).

1. Select or paste the server's **exact instance folder** (containing the server jar, `run.bat`, or `libraries/`).
2. Click **「识别」** (Detect). MineDeck performs a read-only scan and reports the detected loader, Minecraft version, available launch scripts, existing `server.properties` port/memory values, and required Java version.
3. Confirm the server name, port, and EULA checkbox (not needed if `eula.txt` is already accepted), then submit to adopt.

### Protection and Backup Behavior During Adoption {#adopt-notes}

- **Stays in place**: Files are never moved or renamed out of their directory, and only **that single chosen folder** is registered (sibling directories are untouched).
- **Automatic backups of existing launch files**: Before saving `instance.json`, any existing `run.bat` is backed up as `run.before-minedeck.bat`, and Forge/NeoForge `user_jvm_args.txt` is backed up first too. Common non-memory JVM flags are imported automatically, while **flags containing spaces or custom flags from `run.bat` (such as authlib `-javaagent:...`) are intentionally skipped**—the summary alerts you so you can re-add them under **「设置 → 启动与内存」** afterward.
- **High-risk directory guard and port conflicts**: MineDeck blocks adopting entire drive roots, MineDeck's own program/runtime folders, the global backup folder, or already-registered folders. If the detected port collides with another MineDeck server, change it on the confirmation step (updates that server's `server-port`).
- **Rollback on failure**: If adoption fails partway through, newly written metadata files are rolled back cleanly.
- **Removable-drive tolerance**: Adopted servers on external drives hide temporarily if launched while disconnected and reappear automatically once the drive is reattached and MineDeck restarts.

## 2. "Remove from List" vs. "Delete Server Folder" {#remove-vs-delete}

Clicking **「··· → 删除」** (Delete) on a server card or header defaults to keeping **「同时删除服务器文件夹（含世界，无法恢复）」** (Also delete server folder including world — unrecoverable) **unchecked**:

- **Unchecked (default, primary button 「移出列表」 / Remove from List)**: Removes the server from MineDeck's list while leaving the entire folder intact and renaming `instance.json` to `instance.json.bak`—re-adopting that folder later restores all settings.
- **Manually checked**: Requires a second confirmation typing the exact server name before wiping the server directory on disk (world backups stored externally under Settings → Storage Locations `backups\` are kept untouched).

## 3. Exporting and Importing Handover Packs (`.mdpack`) {#handover}

Package a server's **world save, player inventories, mods/plugins, whitelist, and configs** into one `.mdpack` handover archive (imports supported up to **8 GiB**):

### Export steps {#export}

1. Stop the server completely (while packing or importing, the server cannot start or be deleted).
2. Click **「··· → 导出交接包…」** (Export Handover Pack…) on the server card or header.
3. Choose the intended audience and bundle mode:
   - **Purpose**: **「交给下一任服主」** (Pass to next host) or **「发给朋友照开一台」** (Share a copy for a friend to run)—identical pack contents, adjusted import UI instructions.
   - **Mode**: **Slim Mode (default)** skips the core server jar for a smaller file so the recipient downloads the core during import; **Full Mode** bundles the server jar and `libraries/` too, saving setup time on slow or restricted networks.
4. Pick a save destination, or click **「新建并导出」** (Create Folder & Export) in the path dialog.

### Import steps (recipient) {#import}

On another PC running MineDeck (**v3.1.0 or newer**), open **「新建服务器 → 导入交接包」**, select the `.mdpack` (do not unpack it manually or send it via chat image/video compression), verify the original name, loader, version, and world size shown on the summary page, set your local server name and port, and click **「导入交接包」** (Import Handover Pack).

### What Is Included vs. Excluded {#handover-rules}

| Included in `.mdpack` | Excluded (Handle or Re-Enable Manually) |
| --- | --- |
| World save (including multi-world plugin worlds), `eula.txt` | **Logs & crash reports** (stripped automatically) |
| `server.properties` and core configs (`paper.yml`, `spigot.yml`, etc.) | **Extra JVM arguments** (re-enter flags like `-javaagent` under Startup & Memory) |
| `mods/`, `plugins/`, `config/`, `kubejs/`, `scripts/`, server icon | **Schedules & Crash Auto-Restart toggles** |
| Whitelist, OP list, ban list (including banned IPs), proxy forwarding secrets | **RCON is automatically turned off** for security (re-enable manually if needed) |
| — | **In-Game AI, AI Companions, & Music Sharing toggles** (re-enable on recipient PC) |

> [!CAUTION]
> **Two-Way Security & Privacy Reminder**
> - **For the sender**: Config files (including RCON, database, or bot plugin keys) and banned IPs are packed as-is—**rotate any passwords reused elsewhere before exporting**, and notify players beforehand.
> - **For the receiver**: In Full Mode, server cores and mods/plugins come straight from the sender's PC and execute on yours when launched—**only import `.mdpack` archives from people you trust**.
> - If the server exceeds **8 GiB**, transfer the whole directory via drive or split archive and adopt it in-place with **Adopt Existing Server** in Section 1. See also [Sharing with Friends](../networking/share-with-friends.md).
