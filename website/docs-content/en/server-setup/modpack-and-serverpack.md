---
title: Import Modpacks and Ready-Made Server Packs
description: Import Modrinth (.mrpack), CurseForge (.zip), or pre-built Server Pack archives, and resolve skipped or unverified mods under Files Not Installed.
order: 30
since: v3.1.0
beta: false
featured: false
tags: ["Modpack", "mrpack", "CurseForge", "Server Pack", "Pre-built Server", "Pending Mods", "Files Not Installed", "CurseForge Key", "trustedHosts"]
updated: 2026-11-18
aliases: []
draft: false
---

Under sidebar **「新建服务器 → 导入整合包」** (Create Server → Import Modpack), MineDeck accepts three archive types (max **4 GiB**):

- **Modrinth Modpacks (`.mrpack`)**: Detects loader and Minecraft version automatically, downloads server files, and **automatically skips client-only mods**.
- **CurseForge Modpacks (`.zip`)**: Handles `manifest.json` at the root or nested one folder deep (e.g. `PackName/manifest.json`), routing items into `mods/`, `resourcepacks/`, `shaderpacks/`, and `datapacks/`.
- **Ready-Made Server Packs (`.zip`)**: Archives without a modpack manifest that already contain `mods/`, `plugins/`, `server.properties`, or a server jar (also supports one outer wrapper folder).

> [!WARNING]
> **Client Modpack ≠ Server Pack**
> Many CurseForge `.zip` downloads are **client packs** packed with minimaps, shader menus, and HUD mods. MineDeck does not pre-strip those during import; if startup crashes, the console [Error Banner](../troubleshooting/error-banner-index.md#client-only) identifies which files failed so you can click **「停用」** (Disable) on the **Mods** tab. For big modpacks, always prefer the author's dedicated **Server Pack** download when available.

## Import Flow and CurseForge API Key {#import-flow}

1. Open **「新建服务器 → 导入整合包」** and pick your `.mrpack` or `.zip`.
2. Fill **Server Name**, **Port**, **Memory**, and **Location**; if you pre-downloaded extra files locally, point **「本地来源文件夹」** (Local Source Folder) under Advanced options to match files locally first.
3. Accept Minecraft's EULA and start importing, tracking downloads and extraction in background Tasks.

### CurseForge API Key Notes {#cf-key}

Because CurseForge requires an API Key for programmatic downloads:
- If you import CurseForge packs often, enter your free CurseForge API Key under global **「设置 → 下载与网络 → CurseForge API Key（选填）」** (encrypted locally with Windows DPAPI).
- **Imports still work without a Key**: unresolved entries won't fail the entire server setup—instead they land cleanly in **「没装上的文件」** (Files Not Installed, alongside `未解析模组清单.txt` inside the instance directory).
- **Modrinth `.mrpack` needs no API Key**.

## Ready-Made Server Pack Boundaries {#serverpack}

When importing a pre-built server archive (`.zip`):
- **No core re-download**: Extracts the archive directly, updates only `server-port` and any required `log4shell` flags, and preserves packed `online-mode` and `allow-flight` settings.
- **Prerequisite**: The archive must **already contain the server core jar and installed `libraries/`** (or core jar for Fabric/Paper/Vanilla).
- If your "Server Pack" contains only an installer or `start.bat` / `run.sh` that downloads libraries on first run, MineDeck rejects direct import—unpack it elsewhere, run the author's setup script once until the server boots cleanly, and then use [Adopt Existing Server](./adopt-and-handover.md).
- Custom JVM flags inside packed launch scripts or `user_jvm_args.txt` are not imported automatically; re-add them afterward under **「设置 → 启动与内存」**.

## Handling "Files Not Installed" Item by Item {#pending}

Entries blocked by **(1) missing CurseForge Key, (2) untrusted download domain, (3) missing hash / non-HTTPS link, or (4) hash mismatch** are persisted in `.minedeck/import-pending.json` (plus readable `导入待决清单.txt`) under **「没装上的文件」** (Files Not Installed).

> [!NOTE]
> This list persists across restarts. Whenever items remain pending, a persistent alert stays in the server header and lets you resolve each entry from **Tasks**, the **header notice**, or **「模组 / 插件 → 整合包里没装上的」** using three choices:
>
> 1. **Supply a local file**: Pick an individual manually downloaded `.jar` or point to a folder containing multiple files for filename-matched copy (copied files are noted as unverified in task logs).
> 2. **Trust host & download**: For `https` URLs with verified hashes, read the security notice and approve downloads from that host (`config.json` saves the domain in `trustedHosts`; **there is no UI revoke button yet**, so stop MineDeck and edit `config.json` manually if you ever want to remove a trusted host).
> 3. **Skip**: Dismiss for now; skipped entries remain re-installable via local files inside the **Mods / Plugins** tab (**note**: Vanilla servers have no Mods/Plugins tab, so skipping on Vanilla cannot be re-opened in-UI later).

Additional rules:

- **Target protection**: Pending items can never overwrite world saves or `server.properties`.
- **Clean registration**: Cached downloads survive retries on network errors, and the server only enters your active list once initial setup finishes cleanly.
- **Datapacks**: Datapacks extracted from CurseForge manifests drop into `datapacks/` without forcibly changing existing world datapack enable states.
- **Windows filename rules**: Windows reserved device names (`CON`, `COM1`, etc.), trailing dots/spaces, and case-collision duplicates are filtered or handled safely and noted in task logs.
