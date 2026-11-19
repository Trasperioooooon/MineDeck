---
title: 下载与安装（含 SmartScreen）
description: 选择稳定版或测试版，核对 SHA-256 校验和，以及通过 Windows SmartScreen 启动与更新 MineDeck。
order: 10
since: v3.0.0
beta: false
featured: true
tags: ["下载", "安装", "SmartScreen", "Windows 已保护你的电脑", "SHA-256", "升级", "更新渠道", "AutoMCHUB"]
updated: 2026-11-18
aliases: []
draft: false
---

MineDeck 是一个单文件绿色程序，不需要安装器。目前提供 **Windows 64 位**版本，界面使用系统内置的 WebView2 窗口运行（若系统缺少 WebView2，则自动改用默认浏览器打开）。

## 选择稳定版还是测试版 {#channel}

| 渠道 | 适用场景 | 获取与更新方式 |
| --- | --- | --- |
| **稳定版** | 日常开服、和朋友长期联机 | 在官网 [下载页](/download/) 首选「推荐：稳定版」，优先从国内镜像下载 |
| **测试版** | 提前试用尚未进稳定版的新功能 | 下载页会在有更新的测试版时单独列出；**请放进新的空文件夹运行，拿旧服测试前先备份** |

> [!TIP]
> 从 `v3.2.0` 起，你可以在程序侧栏「设置 → 更新与关于」里自由切换更新渠道：**「稳定版」** 或 **「最新版（包括测试版）」**。如果使用的是 `v3.2.0-beta.2` 或更早的测试版，需要手动下载新版本覆盖替换。

## 下载与放置位置

1. 前往官网 [下载页](/download/) 下载 `MineDeck.exe`（国内镜像文件与 GitHub Release 完全一致；镜像缺文件时页面按钮会自动切到 GitHub）。
2. 新建一个专门的空文件夹（例如 `D:\Games\MineDeck\`），把 `MineDeck.exe` 放进去。

> [!NOTE]
> 默认情况下，`servers`（服务器文件）、`backups`（世界备份）、`runtimes`（便携 Java 与隧道客户端）、`cache`、`logs` 与配置文件都会生成在 `MineDeck.exe` 所在的同一目录下，因此**不要直接丢在桌面或系统下载目录里运行**。

## 核对 SHA-256 校验和 {#verify}

下载完成后，可以在 `MineDeck.exe` 所在文件夹打开 PowerShell（空白处按住 Shift 右键 → 在终端中打开），运行下面命令并与下载页或版本详情页公布的 SHA-256 对比：

```powershell
Get-FileHash .\MineDeck.exe -Algorithm SHA256
```

输出的 `Hash` 字符串（不区分大小写）应与官网版本详情页列出的 SHA-256 完全一致。

## Windows 提示「已保护你的电脑」（SmartScreen） {#smartscreen}

因为 `MineDeck.exe` 没有商业代码签名，首次双击启动时 Windows Defender SmartScreen 可能会弹出蓝色拦截提示「Windows 已保护你的电脑」。

确认已核对过 SHA-256 且文件来自官方渠道后：

1. 在弹窗左侧点击 **「更多信息」**。
2. 弹窗下方会出现 **「仍要运行」** 按钮，点击即可正常打开 MineDeck。

> [!WARNING]
> 首次启动具体某一台 Minecraft 服务器时，Windows 防火墙也可能会弹出联网确认框。请勾选允许并点击 **「允许访问」**，否则同一局域网里的朋友将无法连入你的服务器。

## 从旧版 AutoMCHUB（v2.6.1 及更早）升级

MineDeck 原名 AutoMCHUB，自 `v3.0.0` 起正式更名为 `MineDeck.exe`。因为文件名变化，老版本程序里的自动更新无法直接找到新安装包：

1. 先完全停掉所有正在运行的服务器并退出旧版程序。
2. 手动下载最新 `MineDeck.exe`，放进原来 `AutoMCHUB.exe` 所在的文件夹中双击运行。原有的服务器列表、Java 运行时与配置都会继续沿用。
3. 确认一切正常后，再删除旧的 `AutoMCHUB.exe`。
4. 如果以前开过开机自启，旧启动项会被清理，需要时可在 **「设置 → 启动与托盘」** 重新打开。

## 日常更新注意事项

- 在程序侧栏 **「设置 → 更新与关于」** 中检查并一键更新，更新下载后会先核对官方 `SHA256SUMS`。
- **更新前请先停止所有正在运行的服务器**，再触发升级替换。

下一步建议阅读：[新建服务器：从零创建](../server-setup/create-server.md) 或查阅 [常见问题 FAQ](../faq/general-faq.md)。
