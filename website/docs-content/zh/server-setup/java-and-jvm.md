---
title: Java 管理、内存与启动参数
description: MineDeck 自动匹配与便携下载 Java 的规则、全局与单服换 Java 入口、内存分配及安全 JVM 参数说明。
order: 50
since: v3.0.0
beta: false
featured: false
tags: ["Java", "JVM", "内存", "启动参数", "runtimes", "Java 8", "Java 17", "Java 21", "Java 25", "log4shell", "JAVA_HOME"]
updated: 2026-11-19
aliases: []
draft: false
---

在 MineDeck 里开服不需要自己提前研究要装哪个 JDK。程序会按 Minecraft 版本自动对齐所需的 Java 大版本，并在需要时自动准备独立的便携运行时。

## Minecraft 与 Java 大版本对照表 {#version-map}

如果目标游戏版本清单里已经明确了所需 `javaVersion`，程序优先按元数据执行；找不到该字段时按以下兜底规则选取：

| 核心 / Minecraft 版本范围 | 自动选用的 Java 版本 |
| --- | --- |
| `1.16.5` 及更早（含 `1.12.2`） | **Java 8** |
| `1.17.x` | **Java 16** |
| `1.18` ～ `1.20.4` | **Java 17** |
| `1.20.5` ～ `1.21.x` | **Java 21** |
| `26.1` 及更新年份版本 | **Java 25** |
| **Velocity**（群组代理） | 固定 **Java 21** |
| **Waterfall**（群组代理） | 固定 **Java 17** |

## 自动扫描与便携 Java 运行时 {#portable-java}

1. 每次启动时，MineDeck 会在后台扫描系统注册表、`JAVA_HOME`、`PATH` 与常见安装目录中已有的 `java.exe`。
2. 电脑上已经装了合适版本时，优先直接复用，不重复下载。
3. 找不到合适版本时，MineDeck 按顺序从 **清华大学 TUNA Adoptium 镜像 → Adoptium 官方 API → Mojang 官方运行时（经 BMCLAPI 镜像）** 下载便携版到程序目录的 `runtimes/java-<版本>/`。
   - **完全自包含**：不写入系统环境变量，不和电脑上其他项目的 Java 冲突。
   - 即使你在全局设置将下载源改为「官方」，Adoptium 的前两级线路仍会按该顺序优先尝试，加速拉取体验。

### 手动管理全局 Java 列表 {#global-java}

打开侧栏 **「设置 → Java 运行时」**（仅在本机电脑打开时允许添加路径），可以：
- 查看当前识别到的所有本机 Java 与便携 Java（含路径与主版本）；
- 点击重新扫描系统 Java；
- 手动指定本机任意一个 `java.exe` 完整路径，程序会做版本校验后加入可用列表。

## 为单台服务器切换 Java 与调整内存 {#per-server}

打开具体服务器详情页，进入 **「设置 → 启动与内存」**：

- **选择 Java**：在下拉列表切换本机已有、MineDeck 便携下载或按需新下一份其他大版本的 Java（如导入整合包后某模组提示要求更高版本 Java，页头也会出现提示并提供快捷入口直接跳到这里更换）。
- **分配内存**：通过滑块或数字框设定最大堆内存（`-Xmx`），界面会自动对照本机可用物理内存做参考（详情页下限为 `1024 MB`，新建向导起步可选 `512 MB`）。
- **额外 JVM 参数**：可按行添加你需要的高级参数（例如 Aikar's Flags、G1GC 调优或外置登录 `-javaagent`）。

> [!NOTE]
> 为避免因为系统全局预设了不兼容参数导致服务器闪退或被强制限死内存，MineDeck 启动 Java 与 `frpc` 进程时会刻意**剥离掉外部系统环境里的 `JAVA_HOME`、`JAVA_OPTS`、`JAVA_TOOL_OPTIONS`、`_JAVA_OPTIONS`、`JDK_JAVA_OPTIONS` 与 `CLASSPATH`**。
>
> 同时，对于 **Minecraft 1.18.1 以下的服务器**（代理端除外），MineDeck 会按版本保留 log4shell 防护参数，在框里误删了也会补回：1.7 起带上 `-Dlog4j2.formatMsgNoLookups=true`；1.12～1.16.5 引用 Mojang 的 `log4j2_112-116.xml`，1.7～1.11.2 引用 `log4j2_17-111.xml`（v3.2.2 起），都要服务器文件夹里有这份官方文件才会引用。v3.2.3 起，服务端自带的 log4j 已经修过的（比如 2021 年 12 月以后的 Paper 构建）不再引用 Mojang 的配置。Spigot 和 1.11.2 及以前的 Paper 套上 Mojang 的配置以后控制台没有输出，当初没套上的，打开 MineDeck 以后不会替它补，开服时会提醒。这层防护挡不住所有情况，老版本的服务器请只让信得过的人进，详见 [v3.2.3 的发布说明](https://github.com/Trasperioooooon/MineDeck/blob/v3.2.3/docs/releases/v3.2.3.md)。

若服务器启动后报 `UnsupportedClassVersionError` 或 `Unrecognized VM option`，见排查指南：[启动失败与 Java 版本不匹配](../troubleshooting/startup-and-java.md)。
