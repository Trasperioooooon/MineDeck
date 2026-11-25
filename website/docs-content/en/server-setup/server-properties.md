---
title: Server Configuration (Common & All Properties)
description: Manage 78 curated server.properties options across 7 categories, search every key in All Properties, preview colored MOTDs, and toggle MC 1.21.9+ gamerules live.
order: 60
since: v3.0.0
beta: false
featured: false
tags: ["server.properties", "Common", "All Properties", "MOTD", "Gamerules", "online-mode", "server-port", "Difficulty"]
updated: 2026-11-18
aliases: []
draft: false
---

Open a server's **「设置」** (Settings) tab (**Velocity** and **Waterfall** proxies only show **Startup & Memory**, having no `server.properties`) to adjust curated common options or search every raw key.

## 「设置 → 常用」 (Settings → Common): 7 Categories {#common}

MineDeck translates and documents **78 of the most-used `server.properties` fields** across 7 clean groups:

1. **Basics & Gameplay**: World name, gamemode, difficulty, hardcore, max players, and MOTD (with live color picker preview plus proper encoding for Chinese text and Emoji).
2. **World Generation**: Seed, generate structures, level type, view distance, and simulation distance.
3. **Players & Permissions**: Online verification (`online-mode`), whitelist toggle, default OP level, PvP, allow-flight, etc.
4. **Performance**: Entity broadcast range, compression threshold, sync chunk writes, etc.
5. **Networking & Remote Admin**: Server port (`server-port`), `server-ip` binding, Query, RCON toggles & password.
6. **Resource Pack**: Forced resource pack URL, SHA-1 hash, and prompt text.
7. **Security & Misc**: Spawn protection radius, broadcast console to OPs, etc.

> [!TIP]
> - **Auto-hides unsupported fields**: Fields unavailable in older or newer Minecraft releases are hidden automatically so you don't tweak dead keys.
> - **Restart-required badges**: Options that need a server restart are clearly marked right beside their labels.
> - **Live MC `1.21.9+` gamerule toggles**: Starting in **Minecraft `1.21.9`**, several settings formerly in `server.properties` (such as **PvP**, **Enable Command Blocks**, and **Spawn Monsters**) moved to in-game Gamerules—in Settings → Common these act as **live switches that take effect immediately while Running** (unavailable while the server is stopped).
> - **MC `26.1+` difficulty sync**: Where editing config alone does not update an existing `26.1+` world's difficulty, MineDeck dispatches a matching `difficulty` command automatically on boot.

## 「设置 → 全部配置」 (Settings → All Properties): Full Key Search {#raw}

Need an obscure plugin compatibility option or custom property missing from Common?

- Switch to **「设置 → 全部配置」** to browse every existing key-value pair in `server.properties`.
- Known keys carry descriptions too, and the search box filters quickly by **either English key name or Chinese label**.

## Troubleshooting and Safety Notes {#notes}

- **Port checks & header "Change Port"**: When redirected from a startup port conflict alert, clicking **「改端口」** (Change Port) scrolls directly to `server-port` and highlights the field.
- **Read-only file or external lock errors**: On Windows, leaving `server.properties` open in an exclusive editor or marked Read-Only causes saves (and map/backup metadata reads) to report the exact file-lock/read-only cause clearly (`v3.2.0-beta.4+` preserves file protection rather than bypassing read-only flags).
- **Leave `server-ip` blank**: Unless intentionally restricting a multi-homed PC to one local interface, keep `server-ip` empty—entering a public IP (especially behind FRP tunnels) will fail server bind on launch.
