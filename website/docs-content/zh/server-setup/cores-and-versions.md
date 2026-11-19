---
title: 服务端核心与游戏版本选择
description: 对比 MineDeck 支持的 12 种服务端核心、MC 1.12.2 至 26.x 版本支持、国内镜像策略与路径约束。
order: 20
since: v3.0.0
beta: false
featured: false
tags: ["服务端核心", "Vanilla 原版", "Paper", "Purpur", "Leaves", "Folia", "Fabric", "NeoForge", "Forge", "Mohist", "Banner", "Velocity", "Waterfall", "26.x"]
updated: 2026-11-18
aliases: []
draft: false
---

在 **「新建服务器 → 从零创建」** 里，MineDeck 将 **12 种服务端核心** 按用途分成五组显示。选对核心，后面的插件/模组兼容性和下载体验都会省心很多。

## 12 种核心分组对照表 {#core-table}

| 分组 | 核心名称 | 适用场景 | 插件 / 模组 | 下载线路 |
| --- | --- | --- | --- | --- |
| **原版** | `Vanilla 原版` | 朋友纯原版联机，严格保持原汁原味特性 | 均不支持（无详情页插件/模组标签） | 镜像优先（BMCLAPI）/ 官方 |
| **插件服** | `Paper`、`Purpur`、`Leaves`、`Folia` | 原版向性能优化、领地/经济/权限等轻量插件、生电特性或区域多线程 | 支持 **插件**（Bukkit/Spigot/Paper 生态） | 直连官方 API（无国内镜像） |
| **模组服** | `Fabric`、`NeoForge`、`Forge` | 玩大型模组、整合包或使用 AI 伙伴 / 音乐共享扩展 | 支持 **模组**（按 MC 版本与加载器匹配） | Fabric 走 Meta + BMCLAPI；Forge/NeoForge 安装器可走镜像但仍需部分官方源 |
| **混合服** | `Mohist`（Forge + 插件）、`Banner`（Fabric + 插件） | 既想加模组又想用基础 Bukkit 插件 | 标签称 **「插件和模组」**（界面列表管 `mods`） | 直连 MohistMC 官方 API（无国内镜像） |
| **群组代理** | `Velocity`、`Waterfall` | 将多台子服拼成大厅群组服、跨服转发 | 支持代理端 **插件** | 直连 PaperMC Fill v3（无国内镜像） |

> [!TIP]
> - 想玩插件服又拿不准选哪家时，通常首选 **`Paper`**；重视原版生电红石特性可看 **`Leaves`**；需要 20 人以上极高实体负载分摊才考虑 **`Folia`**（许多传统插件尚未适配 Folia 多线程）。
> - 想玩模组或体验 MineDeck 的 [AI 伙伴](../in-game/companion-mod.md) / [音乐共享](../in-game/music-sharing.md)，推荐选 **`NeoForge 1.21.1`** 或 **`Forge 1.20.1`**。
> - 目前 **暂不支持 Quilt** 核心。

## 版本范围与 `26.x` 支持 {#versions}

- **Minecraft 版本跨度**：支持从 **1.12.2 到最新正式版**（包括按新命名规范的 **`26.x`** 年份版本号）。
- **版本列表差异**：多数核心展示自身 API 实际已发布构建的版本；**`NeoForge` 例外**，选版本时会先列出 Mojang `1.20.2` 及之后的全部正式版，若某个版本尚无 NeoForge 构建，会在下一步 **选构建** 时给出提示。
- **群组代理版本号独立**：**Velocity** 与 **Waterfall** 显示的是代理端自身的版本号（如 Velocity `3.4.0`），不是 Minecraft 游戏版本号。
- **高级构建号**：勾选向导中的高级/指定构建选项即可挑历史小版本或特定构建号，默认自动选中推荐构建。

## 选核心前必须知道的约束 {#constraints}

1. **Mohist 与 Banner 路径禁含中文**：这两种混合端在新建、导入整合包和接管时，都会检查存放路径的绝对路径——**不能包含中文等非 ASCII 字符**（例如不要放在 `D:\我的世界\MineDeck` 下），否则创建或接管会直接拦下并提示原因。
2. **代理端功能差异**：**Velocity** 与 **Waterfall** 没有 Minecraft EULA，也没有 `server.properties`；其详情页只有「控制台」「插件」「定时任务」「设置 → 启动与内存」，不会显示「玩家」「世界与备份」和「扩展」。
3. 官方源与国内镜像：建服拉取清单或下载核心超时的时候，到侧栏 **「设置 → 下载与网络」** 切换下载源。三个选项是 **「自动（推荐）」**、**「仅国内镜像」**、**「仅官方源」**。排查见 [启动失败与 Java 版本不匹配](../troubleshooting/startup-and-java.md)。

想直接创建一台服务器？继续阅读：[新建服务器：从零创建](./create-server.md)。
