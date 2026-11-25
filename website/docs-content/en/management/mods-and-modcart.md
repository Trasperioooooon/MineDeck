---
title: Mods, Plugins, and the Install Queue
description: Enable, disable, and safely remove installed mods or plugins, search Modrinth or add local .jar files, and auto-resolve dependencies via the Install Queue.
order: 40
since: v3.0.0
beta: false
featured: false
tags: ["Mods", "Plugins", "Install Queue", "Cart", "Modrinth", "Disable", "Enable", "Dependencies", "Add from PC", "mods", "plugins"]
updated: 2026-11-18
aliases: []
draft: false
---

On every **non-Vanilla** server (including proxies), starting in `v3.2.0-beta.3` the former Resources and Queue ("Cart") views are unified into a single tab labeled dynamically by server type:

- **Paper / Purpur / Leaves / Folia / Velocity / Waterfall**: labeled **「插件」** (Plugins).
- **Fabric / NeoForge / Forge**: labeled **「模组」** (Mods).
- **Mohist / Taiyitist (formerly Banner)** (hybrid): labeled **「插件和模组」** (Plugins & Mods; currently manages files in `mods/`).

## Upper Section: Manage Installed Mods or Plugins {#installed}

The top half lists every file currently inside `mods` or `plugins` (**stop the server before modifying installed files**):

- **Disable / Enable**: Toggles `.jar.disabled` vs. `.jar` cleanly without deleting files or configs—ideal for isolating mod conflicts or silencing client-only mods.
- **Delete**: Removed `.jar` files are **not permanently erased or sent straight to Recycle Bin**; MineDeck moves them safely into `.minedeck\已删除\` inside that server folder in case you need to recover them.
- **Editing mod config files**: MineDeck does not render form editors for `.toml` / `.yml` / `.json` mod configs yet. Click **「打开目录」** (Open Folder) in the header to edit manually—or, if [AI Assistant](../ai-and-mcp/ai-assistant.md) or [MCP](../ai-and-mcp/mcp-setup.md) is enabled, ask AI to prepare an edit card (note: AI will not modify `config/` if it points outside via symlink or directory junction).

### "Files Not Installed from Modpack" Panel {#pending}

If a server came from a modpack with skipped or failed downloads, the **「整合包里没装上的」** panel appears here so you can retry or install missing entries anytime. See [Import Modpacks and Existing Server Packs](../server-setup/modpack-and-serverpack.md#pending).

## Lower Section: Browse, Search, and Install Queue {#modcart}

The lower half combines Modrinth discovery, local uploads, and automatic dependency resolution (**待装清单**, formerly called "Shopping Cart"; the Create Server wizard includes the same optional step for non-Vanilla loaders):

1. **Popular list & keyword search**: Searches **Modrinth** filtered automatically to the server's loader (modded servers also filter by Minecraft version; plugin searches stay flexible across minor versions). Popular sorting loads 20 items per page (up to top 100) with category filters, automatically **filtering out items marked client-side only**.
2. **「从电脑添加」 (Add from PC)**:
   - Pick or drag-and-drop pre-downloaded `.jar` files straight into the queue.
   - **Limits**: Combined online + local queue cap is **50 items** per server, max **256 MB** per local file, staged under `data\modcart\local\` (adding local files works from the desktop or mobile browser, never via AI/MCP).
   - If a `.jar` with the same name but different contents already exists on the server, MineDeck keeps the existing file and reports it on the task log.
3. **Automatic dependency check & completion**:
   - Click check dependencies after selecting Modrinth entries to preview required libraries—or skip right to Install, which checks automatically before downloading.
   - For **local `.jar` uploads**, MineDeck hashes the file to look it up on Modrinth first so missing online dependencies can be fetched automatically; for unrecognized/custom builds, it parses jar metadata and lists missing mod IDs for you to supply. Loader mismatches show a gentle warning without hard-blocking.
4. **Verified writes**: Every Modrinth download is verified against its official hash before landing in `mods/` or `plugins/`.

> [!NOTE]
> **Wizard Draft Queue Note**
> Mods picked inside the **Create Server** wizard live in a temporary wizard draft (**completely separate from the draft queue in Toolbox → Export Modrinth Modpack**). If you step back and switch loader or MC version in the wizard, incompatible picks will note "no matching version found" when creating the server—you can re-pick them afterward in the server's **Mods / Plugins** tab.
>
> In addition, if the AI Assistant modified the active install queue during a session, MineDeck re-verifies the queue snapshot before you click Confirm on an install card so unexpected files are never slipped in.
