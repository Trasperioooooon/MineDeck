---
title: Java Management, Memory, and JVM Arguments
description: Learn how MineDeck selects or downloads portable Java runtimes per Minecraft version, switches Java globally or per server, tunes memory, and enforces JVM safety flags.
order: 50
since: v3.0.0
beta: false
featured: false
tags: ["Java", "JVM", "Memory", "JVM Arguments", "runtimes", "Java 8", "Java 17", "Java 21", "Java 25", "log4shell", "JAVA_HOME"]
updated: 2026-11-19
aliases: []
draft: false
---

With MineDeck you don't need to guess JDK versions before hosting. It maps each Minecraft version to its matching Java major release and fetches isolated portable runtimes whenever missing.

## Minecraft-to-Java Version Map {#version-map}

When a version manifest declares `javaVersion`, MineDeck follows it directly; otherwise it falls back to these defaults:

| Core / Minecraft Version Range | Selected Java Version |
| --- | --- |
| `1.16.5` and older (including `1.12.2`) | **Java 8** |
| `1.17.x` | **Java 16** |
| `1.18` – `1.20.4` | **Java 17** |
| `1.20.5` – `1.21.x` | **Java 21** |
| `26.1` and newer year-based releases | **Java 25** |
| **Velocity** (proxy) | Fixed **Java 21** |
| **Waterfall** (proxy) | Fixed **Java 17** |

## Auto-Detection and Portable Java Runtimes {#portable-java}

1. At launch, MineDeck scans Windows Registry keys, `JAVA_HOME`, `PATH`, and common install folders for existing `java.exe` binaries.
2. Compatible local installs are reused directly without redundant downloads.
3. If none fits, MineDeck downloads a portable runtime into `<MineDeck>\runtimes\java-<version>\` trying **Tsinghua TUNA Adoptium Mirror → Official Adoptium API → Mojang Runtime (via BMCLAPI)** in order:
   - **Self-contained**: Never alters Windows system environment variables or breaks other Java apps.
   - Even if global download source is set to "Official Only", Adoptium tiers try in this mirror-assisted order first for speed.

### Managing the Global Java List Manually {#global-java}

Open **「设置 → Java 运行时」** (Settings → Java Runtimes) (adding local paths requires desktop access) to:
- View every detected system and portable Java runtime (path + major version);
- Re-scan system Java installations;
- Add any custom `java.exe` full path after automatic version validation.

## Switching Java and Tuning Memory Per Server {#per-server}

Open a server's **「设置 → 启动与内存」** (Settings → Startup & Memory) tab:

- **Java Runtime**: Switch between existing local runtimes, downloaded portable runtimes, or trigger a fresh portable download (if a mod requests newer Java after import, the page header provides a direct shortcut here).
- **Memory Allocation (`-Xmx`)**: Adjust max heap via slider or input box against detected free system RAM (`1024 MB` floor on existing servers; `512 MB` in the creation wizard).
- **Extra JVM Arguments**: Add one-per-line flags such as Aikar's Flags, G1GC tuning, or external auth `-javaagent`.

> [!NOTE]
> To prevent global OS variables from breaking server launches or forcing heap caps, MineDeck intentionally **strips `JAVA_HOME`, `JAVA_OPTS`, `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `JDK_JAVA_OPTIONS`, and `CLASSPATH`** when launching Java and `frpc` subprocesses.
>
> In addition, for **non-proxy servers below Minecraft `1.18.1`**, MineDeck preserves Log4Shell protection flags automatically (restoring them even if deleted): `1.7+` gets `-Dlog4j2.formatMsgNoLookups=true`; `1.12–1.16.5` references Mojang's `log4j2_112-116.xml` and `1.7–1.11.2` references `log4j2_17-111.xml` (since `v3.2.2`) whenever that official file exists in the server directory. Starting in `v3.2.3`, builds whose bundled Log4j is already patched (e.g. Paper post-Dec 2021) skip Mojang XML injection; Spigot and Paper `<= 1.11.2` lose console output with Mojang XML, so MineDeck leaves unpatched setups untouched and warns on startup instead. Always restrict legacy servers to trusted players—see [the v3.2.3 release notes](https://github.com/Trasperioooooon/MineDeck/blob/v3.2.3/docs/releases/v3.2.3.md).

Hit `UnsupportedClassVersionError` or `Unrecognized VM option`? See [Startup Failures and Java Mismatches](../troubleshooting/startup-and-java.md).
