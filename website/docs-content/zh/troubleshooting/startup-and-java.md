---
title: 启动失败与 Java 版本不匹配
description: 快速解决服务器启动报错、Java 不兼容或运行时损坏、老版本 Forge 安装半截以及 Mohist/Taiyitist（原 Banner）中文路径阻断。
order: 20
since: v3.0.0
beta: false
featured: false
tags: ["启动失败", "Java 版本不对", "去换 Java", "UnsupportedClassVersionError", "runtimes", "Forge", "NeoForge", "Mohist", "Taiyitist", "Banner", "中文路径"]
updated: 2026-11-18
aliases: []
draft: false
---

当服务器没能顺利开起来并退出时，建议先看一眼详情页头常驻提示区以及 **「控制台」** 顶部的黄色/红色提示卡片——MineDeck 会优先识别出最常见的几个硬伤，多数情况点一下提示卡片里的跳转按钮就能直接修复。

## 一、页头提示「去换 Java」或报错 `UnsupportedClassVersionError` {#java-mismatch}

**常见原日志线索**：
- `java.lang.UnsupportedClassVersionError: ... compiled by a more recent version of the Java Runtime (class file version 65.0)`
- `java.lang.ClassCastException: class jdk.internal.loader.ClassLoaders$AppClassLoader cannot be cast to class java.net.URLClassLoader`（常见于老版 Forge 1.12.2 / 1.16.5 错用了 Java 17 或 21）
- `Mod ... requires java 21 or above`

**修复办法**：
1. 直接点控制台报错横幅或页头的 **「去换 Java」** 按钮，跳转到该服务器的 **「设置 → 启动与内存」**。
2. 对照核心要求切回匹配版本（可参考 [Java 对应表](../server-setup/java-and-jvm.md#version-map)：1.16.5 及更早选 **Java 8**，1.18–1.20.4 选 **Java 17**，1.20.5–1.21.x 选 **Java 21**，26.1+ 选 **Java 25**）；列表里还没有时可在该页直接按需下载一份便携版。
3. **如果是「便携 Java 目录被误删或损坏」**：页头会出现「找不到指定的 Java」提示——如果你删过程序目录下的 `runtimes\java-N\`，回到 **「设置 → 启动与内存」** 重新选中一个可用 Java 或再点下载一次即可；也可以去全局 **「设置 → Java 运行时」** 点重新扫描本机环境。

## 二、JVM 启动参数不被支持 (`Unrecognized VM option`) {#bad-jvm-args}

**常见原因**：
从网上抄了老版本教程的 GC 启动参数（如 Java 8 时代的 `-XX:+UseConcMarkSweepGC`），或者多打了一个错别字/空格带进新 Java 17 / Java 21 运行环境，导致 Java 虚拟机根本来不及启服就当场退出。

**修复办法**：
打开这台服务器的 **「设置 → 启动与内存」**，清掉「额外 JVM 参数」框内报错日志里点名不识别的 `-XX:` 或参数行后重新点 **「启动」**。

## 三、创建阶段或老版本 Forge / NeoForge 安装失败 {#installer-fail}

- **官方源连通超时**：Mojang 与 Fabric 走国内镜像（BMCLAPI）很顺畅，但 **Paper、Purpur、Leaves、Folia、Mohist、Taiyitist（原 Banner）与代理端没有国内镜像**；同时安装 **Forge / NeoForge** 时即使默认启用 `--mirror`，官方安装器仍有一部分步骤要回源确认库校验文件。如果某次建服在任务页卡住或报错失败，可以到侧栏 **「设置 → 下载与网络」** 尝试切换 **下载源**（`自动` / `国内镜像` / `官方源`）并重新发起一次创建。
- **老版本 Forge「半截安装」识别**：部分旧版 Forge 安装器即便内部拉取失败也会返回成功退出码；MineDeck 会将其主动识破为未安装完成并自动最多重试 3 次，如果仍然失败可在任务页点击 **「查看日志」**（保留开头前 40 行）或 **「复制报错信息」** 看清具体卡在哪个库文件下载上。

## 四、Mohist / Taiyitist 提示路径不合法 {#ascii-path}

因为 **Mohist** 与 **Taiyitist**（原 Banner）混合核心底层路径解析问题，MineDeck 在 **新建**、**导入整合包** 和 **接管已有服务器** 阶段会硬性拦截所有带中文、日文或其他非 ASCII 字符的路径（例如 `D:\我的世界\MineDeck\servers\...`）。

请把 `MineDeck.exe` 放在纯英文文件夹（例如 `D:\Games\MineDeck\`），或者到全局 **「设置 → 存储位置」** 把新服务器存放路径改到一个不含中文字符的盘符目录后再试。

需要查其余模组依赖、Mixin、配置文件或内存溢出的报错关键词，直接去：[控制台报错横幅索引](./error-banner-index.md)。
