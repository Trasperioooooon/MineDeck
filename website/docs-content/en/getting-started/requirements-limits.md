---
title: System Requirements & Known Limits
description: Windows runtime requirements, platform limits, download mirror boundaries, and areas that still need wider real-world testing.
order: 20
since: v3.0.0
beta: false
featured: false
tags: ["requirements", "limits", "Windows", "WebView2", "mirrors", "backups", "updates", "DPAPI"]
updated: 2026-11-18
aliases: []
draft: false
---

Before hosting a long-running world with friends, review MineDeck's supported environment and intentional boundaries.

## Environment requirements {#requirements}

| Item | Requirement & behavior |
| --- | --- |
| **Operating system** | **Windows 64-bit only** (Windows 10 / 11). macOS and Linux builds are not provided. |
| **Desktop UI language** | The MineDeck application interface itself is in **Simplified Chinese**. |
| **Desktop window** | Runs inside Windows' built-in **WebView2** container; if WebView2 is absent, MineDeck falls back to opening the local panel in your default browser. |
| **Java runtime** | **Pre-installation is optional.** On startup MineDeck scans the registry, `JAVA_HOME`, `PATH`, and common folders, and downloads isolated portable Java builds into `runtimes/` when needed without altering system environment variables. |
| **Folder placement** | Portable single `.exe`; keep it inside a dedicated ASCII folder with enough disk space for server cores, worlds, and backups. |

> [!WARNING]
> **Non-ASCII path restriction**: **Mohist** and **Taiyitist** (formerly Banner) hybrid cores reject directories whose absolute path contains Chinese or other non-ASCII characters during creation, modpack import, and adoption. Using a clean path such as `D:\Games\MineDeck\` avoids this across all cores.

## Known functional limits {#limits}

- **Download mirror differences**: Mojang vanilla servers and Fabric accelerate downloads via China mirrors (`BMCLAPI`). **Paper, Purpur, Leaves, Folia, Mohist, Taiyitist (formerly Banner), Velocity, and Waterfall download directly from upstream APIs** with no domestic mirror; Forge and NeoForge installers still require partial access to official upstream metadata.
- **Backups exclude mods, plugins, and configs**: World backups only archive the world directory. To package mods, plugins, configs, and worlds together, export a `.mdpack` handover package instead.
- **Stop servers before updating**: Applying an in-app update in **「设置 → 更新与关于」** requires stopping all active servers first.
- **Port reservation limits**: The server creation wizard checks existing MineDeck instances and currently listening TCP sockets, **but cannot detect port ranges reserved by Windows Hyper-V, WSL2, or Docker Desktop** ahead of time (those trigger pre-launch bind errors when starting).
- **Windows 10 vs Windows 11 music sharing**: Windows 11 captures audio per-application (excluding the game itself to prevent echo). Windows 10 only supports system-wide loopback, which includes all system and game audio and asks for explicit confirmation.
- **LAN remote admin boundaries**: Logging in from a phone or another PC over LAN lets you start/stop servers, use the console, and upload mods/map ZIPs, but **path changes, adding local `java.exe` paths, adopting local folders, AI/MCP settings, and local disk exports remain restricted to localhost requests**.
- **Mod configuration editing**: The Mods / Plugins tab handles enable, disable, and delete actions, while visual form editing for individual mod config files is not built in (edit inside the folder, or via AI assistant / MCP proposals).
- **Proxy cores**: **Velocity** and **Waterfall** do not use `eula.txt` or `server.properties`, and omit Players, World & Backups, and Extensions tabs.

## Scenarios still awaiting wider real-world testing {#untested-notes}

Several advanced features work under unit, protocol, or mocked integration tests, while full long-session verification with diverse live clients is still ongoing:

- Live third-party **OpenFrp / SakuraFrp** account OAuth/token flows and OpenFrp UDP voice tunnels;
- Multi-player live client sessions for **AI Companions**, **Music Sharing**, and **In-game AI** `/私` privacy + MC `1.21.5+` compatibility;
- Broad third-party launcher testing on `.mrpack` packs and offline skin packs exported from **Toolbox**, post-map-swap live client joins, and **iPhone** Safari layout checks;
- Per-client hands-on testing across **Claude Desktop, Cherry Studio, Cursor, and VS Code** MCP configs (verified primarily with Codex).

Please report any discrepancies on the [Feedback](/feedback/) page.
