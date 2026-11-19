---
title: Download & Install (SmartScreen)
description: Pick stable vs beta builds, verify SHA-256 checksums, get past Windows SmartScreen, and upgrade safely.
order: 10
since: v3.0.0
beta: false
featured: true
tags: ["download", "install", "SmartScreen", "SHA-256", "upgrade", "AutoMCHUB", "release channel"]
updated: 2026-11-18
aliases: []
draft: false
---

MineDeck ships as a single portable Windows executable with no installer. It runs on **64-bit Windows**, using the built-in WebView2 window (or your default browser if WebView2 is absent). Note that the MineDeck desktop interface itself is in Chinese.

## Stable vs Beta releases {#channel}

| Channel | Best for | Where to get & update |
| --- | --- | --- |
| **Stable** | Everyday hosting with friends | Choose the recommended stable build on the [Download](/download/) page (uses a China mirror first, with GitHub as fallback). |
| **Beta** | Trying upcoming features early | Listed separately on the Download page when newer than stable. **Run betas from a clean folder and back up existing servers first.** |

> [!TIP]
> Starting in `v3.2.0`, you can pick your update channel in Settings → Updates and about (**「设置 → 更新与关于」**): **Stable** or **Latest (including betas)**. Earlier `v3.2.0-beta.2` and older test builds must be replaced manually.

## Where to place `MineDeck.exe`

1. Download `MineDeck.exe` from the [Download](/download/) page.
2. Put it inside its own dedicated empty folder (for example `D:\Games\MineDeck\`).

> [!NOTE]
> By default, MineDeck creates `servers`, `backups`, `runtimes` (portable Java and tunnel clients), `cache`, `logs`, and configuration JSON files next to `MineDeck.exe`. Avoid launching it directly from your Desktop or Downloads folder.

## Verify the SHA-256 checksum {#verify}

Open PowerShell in the folder containing `MineDeck.exe` and run:

```powershell
Get-FileHash .\MineDeck.exe -Algorithm SHA256
```

Compare the output `Hash` against the SHA-256 shown on the Download page or the corresponding release page under Changelog.

## Windows SmartScreen ("Windows protected your PC") {#smartscreen}

Because `MineDeck.exe` is not code-signed, Windows Defender SmartScreen may block the very first run. After checking the SHA-256 hash:

1. Click **More info** in the SmartScreen prompt.
2. Click **Run anyway** to launch MineDeck.

> [!WARNING]
> When starting a Minecraft server for the first time, Windows Firewall may also ask for permission. Choose **Allow access** so friends on your LAN can join.

## Upgrading from AutoMCHUB (v2.6.1 or earlier)

MineDeck was previously named AutoMCHUB before `v3.0.0`. Because the executable name changed, older `AutoMCHUB.exe` auto-updaters cannot find `MineDeck.exe`:

1. Stop all running servers and quit the old program.
2. Place the newly downloaded `MineDeck.exe` into the same folder as your existing `AutoMCHUB.exe` and run it. Your servers, portable Java, and settings carry over automatically.
3. Once verified, you may delete `AutoMCHUB.exe`. If start-with-Windows was enabled before, re-enable it under Settings → Startup & tray (**「设置 → 启动与托盘」**).

## Updating inside MineDeck

- Check for updates under Settings → Updates and about (**「设置 → 更新与关于」**); downloaded updates are verified against official `SHA256SUMS` before replacing the executable.
- **Stop all running Minecraft servers before applying an update.**
