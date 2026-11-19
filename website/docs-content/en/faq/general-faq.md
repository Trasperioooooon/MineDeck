---
title: Frequently Asked Questions (FAQ)
description: Quick answers on Java management, cross-network play, core support, console diagnostics, AI costs, friend mod setup, and release channels.
order: 10
since: v3.0.0
beta: false
featured: true
tags: ["FAQ", "Java", "tunnels", "OpenFrp", "SakuraFrp", "SmartScreen", "whitelist", "backups", "AI", "companion mod", "music sharing"]
updated: 2026-11-18
aliases: []
draft: false
---

Quick answers to the most common questions when starting out or maintaining a server with MineDeck.

## Do I need to install Java beforehand? {#java}

No. On startup MineDeck scans system Java installations (`JAVA_HOME`, `PATH`, registry, common install paths). If a matching version exists, it reuses it; otherwise it automatically downloads a portable runtime into `runtimes/java-{major}/` without touching system environment variables.

## Why is whitelist off by default on new servers? {#whitelist}

Starting in `v3.2.0`, the initial wizard defaults whitelist to off so newcomers aren't locked out when testing locally. **As soon as you expose a server over public tunnels or share it broadly**, turn whitelist on in the server's **「玩家」** tab and add trusted usernames.

## How do friends on another network join? {#remote-play}

- **Same Wi-Fi / LAN**: No tunnel needed—send your friends the `LAN-IP:port` join address shown in the server header or card (allow Java through Windows Firewall on first start).
- **Different networks**: Add a tunnel under **「内网穿透」** (OpenFrp, SakuraFrp, or custom `frps`), bind it to the server with **Follow server start** enabled, start the server, and send friends the public address.

## Which versions and cores are supported? {#cores}

Minecraft **1.12.2 through current releases including `26.x`** across **12 server cores**:
- Vanilla, Paper, Purpur, Leaves, Folia
- Fabric, NeoForge, Forge (Quilt is not supported)
- Mohist, Banner (note: paths cannot contain Chinese or non-ASCII characters)
- Velocity & Waterfall proxies

## The server failed to start — where should I check first? {#crashes}

Open the **「控制台」** tab and check the top **error banner** first. MineDeck recognizes 14 common startup/connection failures offline (missing dependencies, client-only mods in server `mods/`, wrong loader, duplicate mods, incompatible mods, Mixin errors, wrong Java, unsupported JVM flags, port busy, OOM, watchdog hang, EULA, client mod mismatch, broken TOML/config). If AI is configured, click **「让 AI 分析」** for deeper log analysis.

## Do backups include mods, plugins, or configs? {#backups}

No. **World backups archive the world folder only** (never mods, plugins, or configs). To migrate or back up a full server setup, export a `.mdpack` package from the server menu.

## Will AI change files silently or cost money by itself? {#ai-cost}

AI features are completely optional—until you enter a provider and API key, MineDeck never contacts AI services or fetches pricing data. When enabled, charges depend on your chosen provider and API key (local **Ollama** runs free on your PC). Read actions execute immediately, while writes and restarts always show an approval card in **「AI 助手」** requiring your click first.

## What must friends install for AI Companions or Music Sharing? {#friends-mods}

- **AI Companions** (NeoForge `1.21.1` / Forge `1.20.1`): Friends only need the matching `minedeck-companion-*.jar` from the [Download](/download/) page (they do **not** need PlayerEngine or Architectury API unless another mod requires them; modpack servers still require matching pack mods).
- **Music Sharing** (NeoForge `1.20.5+` / Forge `1.20.1+` / Mohist `1.20.1+`): Both server and joining players install matching versions of **Simple Voice Chat**; MineDeck installs the bridge mod on the server side only. Cross-network players also need a UDP voice tunnel bound to the server.

## Stable vs Beta channels {#channels}

Use **Stable** for long-term worlds. Test **Beta** builds inside a separate clean folder with backed-up worlds. Since `v3.2.0`, switch between Stable and Latest (including betas) in **「设置 → 更新与关于」**, and stop all running servers before updating MineDeck.
