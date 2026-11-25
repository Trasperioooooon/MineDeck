---
title: Players, Whitelist, and Bans
description: Inspect online players, toggle and edit the whitelist, operators (OPs), and ban list, and understand default-off whitelisting and offline UUIDs.
order: 20
since: v3.0.0
beta: false
featured: false
tags: ["Players", "Whitelist", "OP", "Operators", "Ban", "Kick", "UUID", "Online Mode"]
updated: 2026-11-18
aliases: []
draft: false
---

Under a server's **「玩家」** (Players) tab (excluded on Velocity and Waterfall proxies), you can manage access control visually without memorizing console commands.

## Main Sections {#features}

1. **Online Players**: View connected players and click to **Kick** (**踢出**, **only available while the server is Running**) or ban.
2. **Whitelist (**白名单**)**:
   - Includes a master **Whitelist toggle**; when enabled, only listed players can connect.
   - Add or remove players by username, or promote a whitelisted player straight to operator.
3. **Operators (OP)**: Grant or revoke server operator privileges without typing `/op` or `/deop`.
4. **Ban List**: Inspect banned usernames and unban players cleanly.

> [!TIP]
> **Seamless live and offline sync**: Whitelist, OP, and Ban changes work whether the server is **running or stopped**—MineDeck dispatches live console commands when running, or edits the JSON lists on disk directly when stopped (using online or offline UUID rules automatically).

## Why New Servers Start with Whitelist Disabled {#default-whitelist}

Starting in `v3.2.0`, newly created servers leave **「白名单」** (Whitelist) **disabled by default** for two reasons:
- Friends hosting a private session for the first time frequently lock themselves out by forgetting to whitelist their own usernames.
- However, once you share a public tunnel address broadly—or enable [In-Game AI](../in-game/in-game-ai.md)—**turn the whitelist back on immediately** (alongside `online-mode` or an auth plugin) so port scanners cannot connect uninvited.

## Online vs. Offline UUID Rules {#uuid}

- When **online authentication is disabled** (`online-mode=false`, common when friends use offline launchers), MineDeck computes deterministic offline UUIDs (`OfflinePlayer:<name>`) whenever updating whitelist or ops files.
- Because offline mode authenticates strictly by **username**, always be mindful before authorizing players in [In-Game AI Command Permissions](../in-game/in-game-ai.md#permissions) unless you run a login/auth plugin—without one, anyone can connect using another player's username.

Looking for the `online-mode` toggle itself? Check [Server Properties (Common & All Settings)](../server-setup/server-properties.md).
