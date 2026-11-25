---
title: Phone & LAN Remote Admin vs. Friends Joining
description: Manage MineDeck from a phone or tablet browser on your LAN while understanding local-only security boundaries, plus a pre-join checklist for friends.
order: 30
since: v3.0.0
beta: false
featured: false
tags: ["Remote Admin", "QR Code", "Access Password", "LAN Management", "Friend Checklist", "Joining", "Security Boundary"]
updated: 2026-11-18
aliases: []
draft: false
---

First, separate two commonly confused concepts: **「设置 → 手机远程管理」** (Settings → Phone Remote Management) opens the **MineDeck admin dashboard** on your phone/tablet browser—it is **not** the server address friends enter in Minecraft! To join the game, send friends your [LAN Connect Address](./lan-and-ports.md) or [Tunnel Public Address](./tunnels-and-accounts.md).

## 1. Enabling Phone & LAN Remote Management {#remote-admin}

Open **「设置 → 手机远程管理」** (formerly called "Remote Access"):

1. **Set an access password first**: Required before enabling (stored locally as a `bcrypt` hash, never plaintext).
2. Enable **「开启手机远程管理」** and **restart MineDeck once** (`0.0.0.0` binding takes effect after restart; toggling it off disables LAN access immediately and logs out active remote sessions).
3. Return to the settings page after restart to scan the **QR code** or open the LAN dashboard URL from any phone on the same router/Wi-Fi.

### Security Boundaries: What Remote Browsers Cannot Change {#remote-bounds}

Even with the password, high-impact actions affecting host filesystem paths or sensitive credentials remain **host-desktop-only** (hidden or read-only from remote browsers):

- **Blocked over LAN**: Changing global Storage Locations, registering local Java runtime paths, adopting local server folders, managing local-path pending items, saving Toolbox exports straight to host folders, picking local folders in Replace World (`.zip` uploads remain allowed), **AI provider settings & Keys**, **MCP interface toggle/token**, and **OpenFrp / SakuraFrp platform account binding & tunnel creation**. Webhook secret URL paths are also masked as `***`.
- **Allowed over LAN**: Viewing servers, starting/stopping/restarting, sending console commands, editing Common/All Properties, choosing installed Java/memory, toggling/deleting/uploading `.jar` mods, creating/restoring backups, checking bound tunnel status, and using the AI Assistant (only when **「局域网可用 AI 助手」** is left enabled).

> [!CAUTION]
> LAN dashboard access lets a logged-in user start/stop servers, add plugins, and edit JVM flags (which executes programs on the host PC). **Share the password only with trusted co-admins.** Responsive layouts and landscape/safe-area spacing are tested on common Android browsers; real-device iPhone Safari edge cases are still welcome via the [Feedback](/feedback/) page.

## 2. Pre-Join Checklist to Send Friends {#friends-checklist}

When inviting friends to play, share these 4 details upfront:

1. **Minecraft edition, version, and loader**: e.g. "Java Edition `1.21.1`, `NeoForge`" (or Vanilla—no mod loader needed).
2. **Client mods or pack needed**:
   - **Vanilla or Plugin server (Paper/Purpur/etc.)**: Friends join with matching Vanilla Java Edition.
   - **Modded server**: Friends need matching client mods (see [Toolbox `.mrpack` Export](../management/toolbox.md) or [Sharing with Friends](./share-with-friends.md)).
   - **AI Companions enabled**: Friends **must install the matching Companion Mod jar** (see [AI Companions](../in-game/companion-mod.md)).
   - **Music Sharing enabled**: Friends should install matching **Simple Voice Chat** (not the bridge mod) to hear broadcast music (see [Music Sharing](../in-game/music-sharing.md)).
3. **Server address**: Send `LAN_IP:Port` on the same LAN, or the tunnel's public address (e.g. `xxx.natfrp.cloud:12345`) over the Internet.
4. **Whitelist username**: If you enabled the whitelist, add friends' exact Minecraft usernames under **「玩家」** (case-sensitive in offline mode).
