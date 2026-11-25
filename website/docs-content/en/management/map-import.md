---
title: Create from an Existing World and Replace Maps
description: Launch a new server from an existing Java Edition save, or safely swap a live server's active world with chained restore points and crash-safe rollback.
order: 50
since: v3.2.0
beta: false
featured: false
tags: ["Replace World", "Existing World", "World Import", "World & Backups", "Restore Points", "Revert World", "Retry Recovery", "level.dat", "Singleplayer Save"]
updated: 2026-11-18
aliases: []
draft: false
---

If you downloaded an adventure/skyblock map, played a long singleplayer save (a folder under `.minecraft\saves\`), or migrated an older server world, you have two ways to bring it in:

- **Option 1 — Create a new server**: Open **「新建服务器 → 用已有地图开服」** (Create Server → From Existing World). MineDeck scans the world first and pre-fills the world's original Minecraft version and loader rather than blindly upgrading it.
- **Option 2 — Replace an existing server's world**: Open **「世界与备份 → 换地图」** (World & Backups → Replace World) and click **「导入并替换当前地图」** (Import and Replace Current World).

> [!NOTE]
> **Upload and folder picker rules**: Sitting at the host PC, you can pick either an uncompressed world folder or a `.zip` archive; over Phone & LAN Remote Management, only `.zip` uploads (up to **8 GB**) are allowed. World replacement, rollback, and restore-point deletion are **human-only actions**—AI Assistant and MCP tools can view status but cannot trigger imports or touch local paths.

## What Formats Are Supported vs. Blocked {#formats}

MineDeck detects `level.dat` and standard Java world layout regardless of folder name or extra wrapper directories inside an archive (if a `.zip` contains multiple worlds, MineDeck lists them so you can pick one):

| Input Type | Supported? | MineDeck Behavior / Advice |
| --- | --- | --- |
| Complete Java Edition world folder or `.zip` (with `level.dat`, vanilla pre/post-26.1 or Bukkit/Paper 3-folder split) | **Supported** | Scanned, previewed, and imported normally |
| `.mcworld` or mobile / console / Bedrock saves | Not supported | Incompatible format; renaming the file extension does not convert Bedrock worlds |
| `.schem` / `.schematic` / `.litematic` schematics | Not supported | Paste schematics in-game with WorldEdit or Litematica |
| `.rar`, `.7z`, encrypted, or split archives | Unpack first | Extract to a regular folder or unencrypted `.zip` first |
| Resource packs, datapacks, modpacks, or `.mdpack` handover packs | Wrong entry | Use Create Server modpack/handover tabs, or drop datapacks into the world's `datapacks/` folder |
| Only `region/` without `level.dat`, or a root containing >256 worlds (like `.minecraft\saves`) | Not supported | Step inside and select the folder holding the single target `level.dat` |

## Pre-Import Scan and Version Checks {#preview}

After selecting a world, MineDeck renders a full preview showing the **last saved Minecraft version, dimensions present, bundled datapacks/resource-pack hints, detected mod loader clues, target folder to replace, disk usage**, and **suggested server settings** (difficulty, gamemode, etc., **unchecked by default** unless you opt in).

Version mismatches follow clear rules:
- **World is newer than the server** (e.g. importing a `26.3` world into a `1.21.11` server): **Blocked in red**. Create a matching-version server via "From Existing World" instead of forcing a risky downgrade.
- **World is older / snapshot / loader mismatch**: **Yellow warning requiring explicit 「我已了解」 (I Understand) acknowledgment**. Launching an older save upgrades chunks permanently so older clients can no longer open it, and missing server mods will strip modded blocks/dimensions.
- **Network shares or drives without free-space telemetry**: An extra confirmation checkbox verifies you have enough room on disk.

## Replacement Flow, Singleplayer Inventories, and Restore Points {#replace-flow}

### Crash-safe, minimal-downtime swap

- Clicking **「停服并导入」** (Stop Server and Import): Staging copy and validation run while the server stays online (and remain cancelable). When ready to swap, MineDeck cleanly asks the server to save and exit (if it fails to exit within 2 minutes, the swap aborts without touching the live world—**never force-killed**).
- Once swapped, the server remains **Stopped** (flagged as newly imported and awaiting first check), while the server core, Java, memory, port, `uid.dat`, whitelist, ops, mods/plugins, and any extra worlds created by multi-world plugins **stay completely intact**.

### Moving a singleplayer save (inventories & resource packs)

- Worlds, builds, and chests carry over intact. Player inventories follow standard server rules: with `online-mode=true` and the same official account, your inventory maps automatically; on offline-mode servers (`online-mode=false`), join once briefly to create the new offline UUID file, stop the server, and rename the old `playerdata` file following the exact filename shown in the preview (keep a backup first).
- Bundled `resources.zip` files or command-block requirements are not forced automatically by MineDeck (enable command blocks in game rules on modern MC or `server.properties` on older releases as directed by the map's README).

### Rolling back with Restore Points {#restore-points}

- Every successful replacement **moves the entire previous world (including Nether and End) into a Restore Point**, which never counts toward scheduled/manual backup retention caps and is never auto-pruned until you click **「删除恢复点」** (Delete Restore Point).
- **Step-by-step chronological rollback**: If you replace maps three times in a row, only the **most recent** restore point exposes **「恢复导入前的地图」** (Restore Previous World), which swaps back that world and reverts the settings applied by that import; the swapped-out world stays alongside until you delete it.
- Servers created via **From Existing World** have no predecessor world, so only Delete Restore Point is shown.

### Power loss or forced close mid-swap {#crash-recovery}

Every swap step writes a persistent recovery journal. If power drops during the final directory move, MineDeck detects the incomplete operation on next launch and **automatically rolls back to the pre-import world**. If antivirus or File Explorer locks a directory mid-rollback, the server stays locked from starting and displays **「重试恢复」** (Retry Recovery) once you close the locking program—other restore points stay locked until recovery finishes cleanly.
