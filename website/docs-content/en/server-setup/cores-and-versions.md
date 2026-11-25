---
title: Choosing Server Cores and Minecraft Versions
description: Compare all 12 supported server cores in MineDeck, Minecraft 1.12.2 through 26.x version coverage, mirror routing strategies, and path rules.
order: 20
since: v3.0.0
beta: false
featured: false
tags: ["Server Cores", "Vanilla", "Paper", "Purpur", "Leaves", "Folia", "Fabric", "NeoForge", "Forge", "Mohist", "Taiyitist", "Banner", "Velocity", "Waterfall", "26.x"]
updated: 2026-11-18
aliases: []
draft: false
---

In **「新建服务器 → 从零创建」** (Create Server → From Scratch), MineDeck organizes **12 supported server cores** into five practical groups. Choosing the right core makes mod/plugin compatibility and updates effortless later.

## Overview of the 12 Server Cores {#core-table}

| Group | Core Name | Best For | Plugins / Mods | Download Route |
| --- | --- | --- | --- | --- |
| **Vanilla** | `Vanilla 原版` | Pure vanilla gameplay with friends | None (no Mods/Plugins tab) | China mirror first (BMCLAPI) / Official |
| **Plugin Servers** | `Paper`, `Purpur`, `Leaves`, `Folia` | Vanilla-compatible performance tuning, land claim/economy plugins, redstone tweaks, or regionized multithreading | **Plugins** (Bukkit/Spigot/Paper) | Direct official API (no China mirror) |
| **Modded Servers** | `Fabric`, `NeoForge`, `Forge` | Large modpacks, mods, AI Companions, or Music Sharing | **Mods** (matched to MC version & loader) | Fabric uses Meta + BMCLAPI; Forge/NeoForge installers mirror where supported plus official checks |
| **Hybrid Servers** | `Mohist` (Forge + Plugins), `Taiyitist` (formerly Banner, Fabric + Plugins) | Mixing mods with basic Bukkit plugins | Labeled **「插件和模组」** (UI manages `mods/`) | Direct MohistMC API (no China mirror) |
| **Proxies** | `Velocity`, `Waterfall` | Linking multiple backend servers into a hub network | Proxy **Plugins** only | Direct PaperMC Fill v3 (no China mirror) |

> [!TIP]
> - Unsure which plugin core to pick? Start with **`Paper`**. Care deeply about strict vanilla redstone/technical mechanics? Check **`Leaves`**. Only pick **`Folia`** for massive entity-heavy servers (20+ spread players), as many legacy plugins do not support Folia multithreading yet.
> - Planning to use mods or MineDeck's [AI Companions](../in-game/companion-mod.md) / [Music Sharing](../in-game/music-sharing.md)? **`NeoForge 1.21.1`** or **`Forge 1.20.1`** are recommended starting points.
> - **Quilt** is not currently supported.

## Version Span and `26.x` Releases {#versions}

- **Minecraft version range**: Supports **`1.12.2` through the latest release**, including **`26.x`** year-based numbering.
- **Version listing differences**: Most cores list only Minecraft versions with published loader builds. **`NeoForge` is the exception**: step 2 lists Mojang releases from `1.20.2` onward first, and warns in the **Build** selector if a version has no NeoForge build yet.
- **Independent proxy versions**: **Velocity** and **Waterfall** use proxy software versions (e.g. Velocity `3.4.0`), not Minecraft client versions.
- **Custom build numbers**: Expand Advanced/Specific Build options in the wizard to pick an older sub-version or exact build number instead of the recommended default.

## Key Constraints Before Selecting a Core {#constraints}

1. **ASCII-only paths for Mohist & Taiyitist (formerly Banner)**: During Create, Modpack Import, and Adopt Server, MineDeck enforces **no Chinese or non-ASCII characters** anywhere in the absolute path for these hybrid cores (avoid e.g. `D:\我的世界\MineDeck`), blocking creation immediately with a clear reason instead of failing cryptically at boot.
2. **Proxy feature differences**: **Velocity** and **Waterfall** have no Minecraft EULA or `server.properties`, so their details view shows only Console, Plugins, Schedules, and Startup & Memory (no Players, World & Backups, or Extensions).
3. **Official vs. China Mirror routes**: If fetching version manifests or cores times out, switch Download Source (**下载源**: **「自动（推荐）」**, **「仅国内镜像」**, or **「仅官方源」**) under **「设置 → 下载与网络」**—see [Startup Failures & Java Mismatches](../troubleshooting/startup-and-java.md).

Ready to deploy your first server? Continue to [Create Server: From Scratch](./create-server.md).
