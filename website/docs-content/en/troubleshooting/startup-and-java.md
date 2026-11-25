---
title: Startup Failures and Java Version Mismatches
description: Fix Java version errors, invalid JVM flags, partial legacy Forge installs, and non-ASCII folder rejections on Mohist/Taiyitist (formerly Banner).
order: 20
since: v3.0.0
beta: false
featured: false
tags: ["Startup Failed", "Wrong Java", "Switch Java", "UnsupportedClassVersionError", "runtimes", "Forge", "NeoForge", "Mohist", "Taiyitist", "Banner", "Non-ASCII Path"]
updated: 2026-11-18
aliases: []
draft: false
---

When a server exits without starting, check the persistent status banner in the server header and the top of **「控制台」** (Console)—MineDeck catches the most frequent boot failures automatically and often puts a direct action button right in the alert.

## 1. Header Says "Switch Java" or `UnsupportedClassVersionError` {#java-mismatch}

**Common log lines**:
- `java.lang.UnsupportedClassVersionError: ... compiled by a more recent version of the Java Runtime (class file version 65.0)`
- `java.lang.ClassCastException: class jdk.internal.loader.ClassLoaders$AppClassLoader cannot be cast to class java.net.URLClassLoader` (often legacy Forge `1.12.2` / `1.16.5` launched with Java 17 or 21)
- `Mod ... requires java 21 or above`

**Fix**:
1. Click **「去换 Java」** (Switch Java) on the console banner or header alert to jump straight to **「设置 → 启动与内存」**.
2. Pick the matching runtime ([Java Version Map](../server-setup/java-and-jvm.md#version-map): `1.16.5` & earlier → **Java 8**, `1.18–1.20.4` → **Java 17**, `1.20.5–1.21.x` → **Java 21**, `26.1+` → **Java 25**), or click to download a portable runtime directly on that page.
3. **Damaged or deleted portable Java**: If `runtimes\java-N\` was deleted or partially extracted, the header alerts that the selected Java is missing—select another runtime or re-download under **「设置 → 启动与内存」** (or rescan system installs in global **「设置 → Java 运行时」**).

## 2. Unsupported JVM Options (`Unrecognized VM option`) {#bad-jvm-args}

**Cause**: Copying legacy GC switches (such as Java 8's `-XX:+UseConcMarkSweepGC`) or a typo into Extra JVM Arguments under a newer Java 17 / Java 21 runtime causes the JVM to abort before Minecraft even starts.

**Fix**: Open **「设置 → 启动与内存」**, remove the unrecognized `-XX:` option named in the log, and click **「启动」** (Start).

## 3. Creation or Legacy Forge / NeoForge Installer Failures {#installer-fail}

- **Upstream timeouts**: Mojang and Fabric mirror cleanly via BMCLAPI in China, whereas **Paper, Purpur, Leaves, Folia, Mohist, Taiyitist (formerly Banner), and proxies have no China mirror**; even with Forge/NeoForge `--mirror`, upstream installers still verify select files directly. If server creation stalls or errors out, switch **Download Source** (**下载源**: `Auto` / `China Mirror` / `Official`) under **「设置 → 下载与网络」** and retry.
- **Detecting half-installed legacy Forge**: Some older Forge installers exit `0` even when library downloads fail; MineDeck catches incomplete outputs and retries up to 3 times automatically before failing cleanly—inspect **「查看日志」** (preserves the first 40 lines) or **「复制报错信息」** in the task log to see which library timed out.

## 4. Non-ASCII Path Rejection on Mohist / Taiyitist {#ascii-path}

Due to upstream path-parsing bugs in **Mohist** and **Taiyitist** (formerly Banner) hybrid servers, MineDeck rejects Chinese, Japanese, or other non-ASCII folder paths during **Create**, **Import Modpack**, and **Adopt Server** (such as `D:\我的世界\MineDeck\servers\...`).

Place `MineDeck.exe` inside an ASCII-only folder (such as `D:\Games\MineDeck\`), or change the new servers folder under global **「设置 → 存储位置」** to an ASCII path and try again.

Looking up Mixin, dependency, config syntax, or OOM keywords? See the full [Console Error Banner Index](./error-banner-index.md).
