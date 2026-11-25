---
title: Sharing with Friends — Handover Pack vs. Modpack Export
description: Compare .mdpack Handover Packs, Toolbox .mrpack Modrinth exports, and direct folder copies so you share worlds or client mods cleanly without missing saves.
order: 40
since: v3.1.0
beta: false
featured: false
tags: ["Handover Pack", "mdpack", "mrpack", "Share with Friends", "Migration", "Adopt"]
updated: 2026-11-18
aliases: []
draft: false
---

MineDeck offers three ways to hand a server or mod setup over to friends. Choose based on your exact goal:

## Comparison of the Three Sharing Methods {#comparison}

| Goal | Recommended Tool & Entry | How Your Friend Uses It | Contents & Size Limit |
| --- | --- | --- | --- |
| **Hand over the full server—world save, builds, player inventories, and settings—so a friend can take over hosting** | Server card/header **「··· → 导出交接包…」** → **`.mdpack`** | Import via **「新建服务器 → 导入交接包」** in MineDeck (`v3.1.0+`) | Includes world, configs, mods/plugins, whitelist, etc.; Slim (no core jar) or Full (with core); import cap **8 GiB** |
| **Share matching client mods so friends sync via HMCL/PCL2/Modrinth—or spin up a fresh server from the same mod list** | Sidebar **「工具箱 → 导出 Modrinth 整合包」** → **`.mrpack`** | Friends drop `.mrpack` straight into any Modrinth-compatible launcher (or import into another server) | Modrinth index + optional `overrides/`/`config/` (strips voicebridge & suspected secrets); **excludes world saves** |
| **Migrate huge servers >8 GiB or preserve every custom startup script & auxiliary directory verbatim** | Stop server, zip/copy the **entire server directory** | Extract locally and click **「新建服务器 → 接管已有服务器」** | 100% original files; **no size limit** |

> [!TIP]
> **Typical workflow for starting a modded co-op run**:
> - Keep your tuned server running in MineDeck;
> - Export a `.mrpack` from **「工具箱 → 导出 Modrinth 整合包」** (including extra client-only minimap/shader/performance mods in the Toolbox draft) so every friend installs identical client mods in one drag-and-drop instead of trading dozens of `.jar` files manually.

## Detailed Guides {#links}

- Full `.mdpack` export/import & privacy checks: [Adopt Existing Servers and Handover Packs](../server-setup/adopt-and-handover.md#handover)
- Toolbox `.mrpack` export, secret scrubbing, and offline skin packs: [Toolbox](../management/toolbox.md)
- Resolving skipped downloads after a modpack import: [Import Modpacks and Existing Server Packs](../server-setup/modpack-and-serverpack.md#pending)
