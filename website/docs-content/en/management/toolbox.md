---
title: Toolbox — Offline Skin Packs and Modrinth Modpack Export
description: Build client skin resource packs for offline-mode servers or export standard .mrpack modpacks for friends without touching any live server files.
order: 70
since: v3.2.0
beta: false
featured: false
tags: ["Toolbox", "Offline Skin Pack", "Skins", "Resource Pack", "mrpack", "Modpack Export", "OfflinePlayer", "Secret Filter"]
updated: 2026-11-18
aliases: []
draft: false
---

The **「工具箱」** (Toolbox) sidebar view (introduced in `v3.2.0`) builds shareable companion packages for friends. It currently includes **「离线皮肤包生成」** (Offline Skin Pack Generator) and **「导出 Modrinth 整合包」** (Export Modrinth Modpack).

> [!NOTE]
> **Non-destructive & human-only**:
> Nothing in Toolbox modifies existing server files. Outputs are cached in `cache\toolbox\` (up to the 24 most recent builds) for browser download or direct local disk export (desktop only). Core Toolbox export actions are hidden from AI Assistant and MCP tools (though AI can help search mods into your export draft queue).
>
> Generated resource packs and `.mrpack` archives still need broader real-world testing across diverse third-party launchers—test locally with your own client before distributing to a group.

## 1. Offline Skin Pack Generator {#skin-pack}

On offline-mode servers (`online-mode=false`), vanilla clients assign random default Steve/Alex skins to everyone. Toolbox packages uploaded skins into a single client resource pack (placed into `.minecraft\resourcepacks\`) so everyone sees custom skins offline:

- **Two slot assignment modes**:
  - **By player name (recommended)**: Computes exact `OfflinePlayer:<name>` default skin slots across the 18 default skins (MC `1.19.3+`) or 2 default skins (`1.19.2` and earlier).
  - **Sequential replacement**: Maps uploaded skins sequentially over default slots without binding specific names.
- **Collision alerts and legacy `64×32` expansion**:
  - Alerts you cleanly on **slot collisions** when two usernames hash to the same default skin slot so you can swap or switch to sequential mode.
  - Accepts both modern `64×64` and **legacy `64×32` PNG skins**, automatically expanding `64×32` to `64×64` and clearing solid legacy hat layers to prevent black-head rendering bugs.
  - Upload limits: max **2 MB** per PNG, up to **18 skins** per pack; targets Minecraft **1.8+** formats.
- **Output format**: Emits a valid `.zip` with `pack.mcmeta` plus its SHA-1 checksum (ready for `server.properties` resource-pack enforcement if hosted on a public URL).

## 2. Export Modrinth Modpack (`.mrpack`) {#mrpack-export}

Package a matched client mod setup for friends without bundling huge server worlds:

1. **Two sources**:
   - **From an existing modded server**: Pick any Fabric, Forge, NeoForge, Mohist, or Taiyitist (formerly Banner) server to scan its `mods/` folder (blocked if `mods/` itself is a directory junction/symlink; MineDeck's `minedeck-voicebridge` server mod is excluded automatically).
   - **From the Toolbox draft queue**: Build a fresh draft list (independent from the **Create Server** wizard draft), letting you include **client-only optimization, shader, or minimap mods** while resolving dependencies automatically.
2. **Smart manifest split**:
   - Files matching official Modrinth hashes from allowed download origins go into `modrinth.index.json` (with SHA-1/SHA-512 hashes and client/server `env` flags) so launchers fetch them directly.
   - Custom `.jar` files unavailable on Modrinth (keeping original filenames) and selected config folders go into `overrides/`.
3. **Optional `config/` inclusion & secret scrubbing**:
   - `config/` is **unchecked by default**. When checked to share tuned mod settings, MineDeck scans filenames, extensions, and common secret keys to exclude likely **API Keys, database passwords, bot tokens**, and backup copies—showing **both the excluded list and every included file in the export preview** so you can double-check before finalizing.

Unsure whether `.mrpack` or `.mdpack` fits your situation? Read [Sharing with Friends: Handover vs. Modpack](../networking/share-with-friends.md).
