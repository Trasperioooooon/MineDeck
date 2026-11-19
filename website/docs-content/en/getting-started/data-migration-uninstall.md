---
title: Data Locations, Migration & Uninstall
description: Understand MineDeck's portable folder layout, Windows DPAPI credential scoping when moving PCs, changing default roots, and clean removal.
order: 40
since: v3.0.0
beta: false
featured: false
tags: ["folders", "data location", "migration", "uninstall", "secrets.json", "DPAPI", "AppData", "runtimes"]
updated: 2026-11-18
aliases: []
draft: false
---

MineDeck keeps almost everything portable inside the folder holding `MineDeck.exe`. It does not scatter configuration across system directories or alter your global `JAVA_HOME`.

## Portable folder structure {#folders}

Inside your `MineDeck.exe` directory (for example `D:\Games\MineDeck\`):

| Path | Contents | Migration / deletion notes |
| --- | --- | --- |
| `servers\` | Created Minecraft server folders | Can be relocated for future instances via Settings → Storage |
| `backups\` | World backups (`.zip`) | Stored outside `servers\` so backups stay intact after deleting an instance |
| `runtimes\` | Portable Java downloads & `frpc` tunnel binaries | **Do not delete manually**, or instances configured to use portable Java will fail to start |
| `cache\` | Downloaded core installers and temporary Toolbox outputs (`cache\toolbox\`) | Safe to clear when no background tasks are running |
| `logs\` | Application logs and redacted operation audit logs (`logs\ops\`) | Helpful for diagnostics |
| `data\` | Staged local modcart uploads and MCP runtime state | Keep with the executable |
| `config.json` | Global settings, release channel, extra scan roots, adopted dirs, trusted modpack hosts | Carry along during migration |
| `secrets.json` | Encrypted API keys (CurseForge, AI providers), OpenFrp/SakuraFrp credentials, MCP token | **Encrypted via Windows DPAPI for the current PC + Windows account** |
| `tunnels.json` | Saved tunnel definitions and encrypted custom/provider credentials | Encrypted credentials also tied to the current PC + Windows account |

Two Windows system locations may also be touched:
- `%LocalAppData%\MineDeck`: WebView2 browser cache and client-side appearance state.
- `HKCU\...\Run` Windows startup entry (only created when you enable auto-start under **「设置 → 启动与托盘」**; requires no admin rights).

## Moving storage folders for future servers {#change-roots}

1. On the host desktop, open Settings → Storage (**「设置 → 存储位置」** — localhost-only; blocked from LAN browsers).
2. Pick a new **Server storage folder** or **Backup storage folder**.

> [!NOTE]
> Changing these roots applies immediately **to newly created servers and future backups only**. Existing servers and old backup `.zip` files remain untouched where they were, and MineDeck continues scanning previously used roots automatically.

## Migrating MineDeck to another drive or PC {#migration}

1. Stop all running servers and exit MineDeck cleanly.
2. Copy the entire directory containing `MineDeck.exe` (`servers\`, `backups\`, `runtimes\`, `config.json`, etc.)—or export single servers via `.mdpack` handover packages.
3. **Update Java paths if the drive/path changed**: Instances pinned to MineDeck's portable Java store absolute paths. If the target directory moved, open each affected server's **「设置 → 启动与内存」** (or click the header fix button) to re-select Java.
4. **Re-enter secrets on a new PC or Windows user**: Because `secrets.json` and `tunnels.json` use Windows DPAPI bound to your original machine and Windows user profile, copying them onto a different PC or account makes old keys unreadable. Re-enter your AI Key, CurseForge Key, tunnel credentials, and click Reset Token on MCP settings.

## Uninstalling completely {#uninstall}

1. If you enabled launch-at-login, turn it off first under **「设置 → 启动与托盘」** so the `HKCU\...\Run` entry is removed.
2. Stop all servers and quit MineDeck.
3. Delete the folder containing `MineDeck.exe`.
4. Delete `%LocalAppData%\MineDeck` if present, along with any custom server or backup folders you placed on other drives.
