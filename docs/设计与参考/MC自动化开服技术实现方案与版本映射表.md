# MC 自动化开服技术实现方案与版本映射表

> Agent 1（联网研究员）产出 · 检索日期：2026-07-12 · 状态：待用户确认，未开始编码

---

## 0. 研究结论摘要（2026 年 7 月生态现状）

1. **Minecraft 已更换版本号体系**：自 2026 年 3 月的 **26.1（"Tiny Takeover"）** 起，Java 版改用「年份.序号」命名。当前最新正式版 **26.2**（2026-06-16 发布，已实测官方 manifest 确认），最新快照 26.3-snapshot-3。工具设计必须同时兼容 `1.x.x` 与 `26.x` 两套版本号。
2. **Java 需求出现了新分界**：26.1+ 需要 **Java 25**。完整映射见 §1。
3. **模组加载器格局**：NeoForge 已是 1.20.5+ 的事实标准并持续跟进 26.x；Fabric 对 26.2 的支持为 loader 0.19.3；Forge 仍在更新（1.21.11 对应 Forge 61.1.9），但现代版本生态已迁往 NeoForge。
4. **BMCLAPI 镜像实测**：`/forge/*`、`/neoforge/*` 端点可用且已确认返回结构；`/mc/game/version_manifest*.json` 从海外 IP 访问返回 503（国内通常正常）。**结论：必须做"镜像 ↔ 官方"双向自动故障转移，不能单源依赖。**

---

## 1. MC 版本 ↔ Java 版本映射表

### 1.1 静态兜底表（硬编码进程序）

| MC 版本范围 | 必需 Java（major） | 备注 |
|---|---|---|
| 1.8 – 1.11.2 | **8** | 老 Forge 在 Java 9+ 会崩，必须严格用 8 |
| 1.12 – 1.16.5 | **8** | 同上；Vanilla 本身可跑更高版但不冒险 |
| 1.17 – 1.17.1 | **16** | Forge 37 早期构建在 Java 17 上有兼容问题，精确匹配 16 最稳 |
| 1.18 – 1.20.4 | **17** | LTS，Temurin 全平台可得 |
| 1.20.5 – 1.21.x | **21** | NeoForge 从此代起为主流 |
| **26.1+（新版本号体系）** | **25** | 2026-03 起生效 |

### 1.2 动态权威来源（首选策略）

**不依赖硬编码表**：Mojang 每个版本的元数据 JSON 中带有 `javaVersion.majorVersion` 字段（如 26.2 → 25，1.20.4 → 17）。程序运行时直接读取该字段决定 Java 版本，**未来任何新版本自动正确**；§1.1 的表仅作离线/字段缺失时的兜底。

### 1.3 各核心对 MC 版本的支持范围（决定 UI 上给用户展示哪些选项）

| 核心 | 支持范围 | 说明 |
|---|---|---|
| Vanilla | 全版本 | manifest 里有的都能开 |
| Forge | 1.1 – 1.21.x（仍在更新） | 通过列表 API 动态判断某 MC 版本有无构建 |
| NeoForge | **1.20.2+**（含 26.x） | 1.20.1 时代与 Forge 纠缠不清，工具从 1.20.2 起才提供 NeoForge 选项 |
| Fabric | 1.14+（含 26.x） | 由 Fabric Meta API 动态取交集 |

---

## 2. 下载源矩阵（核心设计：国内镜像优先 + 官方源自动回退）

### 2.1 Vanilla 服务端

| 用途 | 国内镜像（BMCLAPI） | 官方源 |
|---|---|---|
| 版本清单 | `https://bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json` | `https://piston-meta.mojang.com/mc/game/version_manifest_v2.json` ✅已实测 |
| 版本 JSON / server.jar | 将官方 URL 的 host（`piston-meta.mojang.com` / `piston-data.mojang.com` / `launchermeta.mojang.com`）替换为 `bmclapi2.bangbang93.com` | 版本 JSON 内 `downloads.server.url` + `sha1` |

### 2.2 Forge

| 用途 | URL 模式 | 状态 |
|---|---|---|
| 构建列表 | `https://bmclapi2.bangbang93.com/forge/minecraft/{mc版本}` → JSON 数组，字段：`build` / `version` / `mcversion` / `files[{format, category, hash}]` | ✅ 已实测 |
| 安装器下载（镜像） | `https://bmclapi2.bangbang93.com/forge/download?mcversion={mc}&version={forge}&category=installer&format=jar` | 文档端点 |
| 安装器下载（官方） | `https://maven.minecraftforge.net/net/minecraftforge/forge/{mc}-{forge}/forge-{mc}-{forge}-installer.jar` | 稳定直链规律 |

### 2.3 NeoForge

| 用途 | URL 模式 | 状态 |
|---|---|---|
| 版本列表 | `https://bmclapi2.bangbang93.com/neoforge/list/{mc版本}` → JSON 数组，字段：`version` / `rawVersion` / `mcversion` / `installerPath` | ✅ 已实测（1.21.1 返回 236 条） |
| 安装器下载（镜像） | `https://bmclapi2.bangbang93.com` + `installerPath`（形如 `/maven/net/neoforged/neoforge/{v}/neoforge-{v}-installer.jar`） | ✅ 结构已确认 |
| 安装器下载（官方） | `https://maven.neoforged.net/releases/net/neoforged/neoforge/{v}/neoforge-{v}-installer.jar` | 稳定直链规律 |

### 2.4 Fabric

| 用途 | URL 模式 | 状态 |
|---|---|---|
| Loader 版本列表 | `https://meta.fabricmc.net/v2/versions/loader/{game_version}` | ✅ 端点已确认 |
| **一体化服务端 launcher** | `https://meta.fabricmc.net/v2/versions/loader/{game}/{loader}/{installer}/server/jar` → 直接可运行的小 jar | ✅ 端点已确认 |
| 国内镜像 | `https://bmclapi2.bangbang93.com/fabric-meta/v2/...`（同路径映射） | 文档端点 |

Fabric 加分项：其 launcher 首次启动才去 Mojang 下载原版 server.jar（国内慢）。我们可**预先经 BMCLAPI 下好原版 jar**，写入 `fabric-server-launcher.properties` 的 `serverJar=` 指向它，彻底规避这一步。

### 2.5 便携式 Java（Temurin/Adoptium，免安装 zip 解压即用）

| 优先级 | 来源 | URL 模式 | 备注 |
|---|---|---|---|
| P0（国内） | 清华 TUNA 镜像 | `https://mirrors.tuna.tsinghua.edu.cn/Adoptium/{ver}/jre/x64/windows/` 目录下取 zip | ✅ 已实测目录存在；**有 8/11/17/21/25，无 16** |
| P1（官方） | Adoptium API v3 | `https://api.adoptium.net/v3/binary/latest/{ver}/ga/windows/x64/jre/hotspot/normal/eclipse` | 覆盖含 16 在内的全部版本；Java 16 只能走这里 |
| P2（兜底） | Mojang 官方运行时（BMCLAPI 有镜像） | java-runtime manifest 按文件逐个下载 | 实现复杂，仅作最终兜底，第一版可不做 |

### 2.6 风险提示（已实测发现）

- BMCLAPI 为公益服务，存在偶发 503 / 限流（本次海外实测 `/mc/` 路径即 503）。**所有下载必须实现：源健康探测 → 失败自动切换另一源 → 重试（指数退避）→ SHA-1 校验**。这条会成为 Agent 4 的第一职责。

---

## 3. 编程语言选型：推荐 Go

| 维度 | **Go（推荐）** | Python + PyInstaller | C# (.NET 8) |
|---|---|---|---|
| 单文件 exe | ✅ 原生静态编译，约 8–12 MB，零依赖 | ⚠️ 30 MB+，onefile 启动慢（解压到临时目录） | ⚠️ 自包含发布 70 MB+，或要求用户装 .NET 运行时 |
| 杀软误报 | 低 | **高**（PyInstaller 是重灾区，分发给朋友时致命） | 低 |
| 并发下载/进度条 | ✅ goroutine 天生适合分块下载 | 可做但费劲 | 可做 |
| 进程管理（启动/停止服务器、转发控制台） | ✅ `os/exec` 简洁可靠 | subprocess 可用 | 可用 |
| 解压 zip / 生成 bat / JSON API | 全部标准库内置 | 依赖第三方打包 | 内置 |
| GUI 升级路径 | CLI 先行，后续可加 Fyne/Wails | tkinter 丑，PyQt 巨大 | WinForms 最强但绑死体积 |

**结论：Go + 交互式 CLI**（方向键选版本/核心的向导式界面，如 `charmbracelet/bubbletea` 或纯 stdin 极简实现），完全满足"鲁棒性优先、单文件 exe"的要求。Python 仅在"你本人想频繁改代码且不在意杀软误报"时才建议。

---

## 4. 四大核心的初始化流程差异（Agent 3 的实现依据）

| 核心 | 下载物 | 初始化动作 | 启动方式 |
|---|---|---|---|
| Vanilla | `server.jar` | 写 `eula.txt` | `java -Xms… -Xmx… -jar server.jar nogui` |
| Fabric | 一体化 launcher jar（~1 MB） | 预置原版 jar + properties 指向 | `java -jar fabric-server-…-launcher.jar nogui` |
| Forge ≤ 1.16.5 | `…-installer.jar` | 运行 `java -jar installer.jar --installServer` → 生成 `forge-{mc}-{v}.jar` | `java -jar forge-{mc}-{v}.jar nogui` |
| Forge 1.17+ / NeoForge | `…-installer.jar` | 同上，但生成 `run.bat` + `user_jvm_args.txt` + `libraries/.../win_args.txt`（@参数文件机制） | 重写 `run.bat`：用**我们便携 Java 的绝对路径** + `@user_jvm_args.txt @libraries/…/win_args.txt nogui` |

统一收尾动作：写 `eula.txt`（`eula=true`，需用户在向导中明确勾选同意 Mojang EULA）、按用户选择生成 `server.properties`（端口 / motd / max-players / online-mode）、生成最终 `run.bat` 与 `start_server.bat`。

**目录规划**（工具运行目录下自包含，不碰系统环境变量）：

```
MineDeck/
├── MineDeck.exe
├── runtimes/            # 便携 Java，按 major 版本分目录
│   ├── java-8/  java-17/  java-21/  java-25/
├── cache/               # 下载缓存（installer、server.jar，带 sha1 校验）
└── servers/
    └── <实例名>/         # 每个服务器一个实例目录
```

---

## 5. 多智能体分工预告（阶段二细化，此处仅列职责边界）

- **Agent 2（环境与下载魔术师）**：双源下载器（镜像/官方 failover、重试、SHA-1 校验、断点续传）、便携 Java 解压部署、`javaVersion.majorVersion` 解析、EULA 写入。
- **Agent 3（核心启动器专家）**：四核心差异化安装流水线（§4）、installer 子进程托管与日志透传、`run.bat` 生成与重写（绝对路径 Java）、服务器进程启停与控制台转发。
- **Agent 4（QA 与异常处理员）**：端口占用预检（启动前 `net.Listen` 试探 25565）、Java 环境污染隔离（无视 `PATH`/`JAVA_HOME`，清除 `_JAVA_OPTIONS`/`JAVA_TOOL_OPTIONS` 注入）、路径含中文/空格的检测与告警、内存自适应默认值（物理内存的一半、上限 4G，可改）、下载失败/校验失败的用户友好报错、Windows 防火墙首次放行提示。

---

## 6. 待用户确认的决策点

1. **UI 形态**：第一版做「交互式 CLI 向导」（推荐，最快落地最鲁棒），还是直接上简易 GUI？
2. **核心范围**：Vanilla / Forge / NeoForge / Fabric 已覆盖需求，是否还要 **Paper/Purpur**（插件服，非模组服，下载 API 也很规范）？
3. **online-mode 默认值**：向导中让用户选「正版验证 开/关」，默认开还是默认关（离线局域网场景常用关）？
4. 语言选型 **Go** 是否认可（或坚持 Python）？

---

## 7. 主要信息来源

- [Minecraft Wiki – Server/Requirements](https://minecraft.wiki/w/Server/Requirements)（1.17→16、1.18→17、1.20.5→21 官方口径）
- [Mojang 官方版本清单（实测）](https://piston-meta.mojang.com/mc/game/version_manifest_v2.json)（latest.release = 26.2）
- [ModReady – Java 版本需求全表](https://modready.gg/guides/minecraft-java-version-requirements)（26.1+ → Java 25，2026-03 分界）
- [BMCLAPI 文档站](https://bmclapidoc.bangbang93.com/) 及实测端点：[Forge 列表](https://bmclapi2.bangbang93.com/forge/minecraft/1.12.2)、[NeoForge 列表](https://bmclapi2.bangbang93.com/neoforge/list/1.21.1)
- [Fabric Meta API（实测端点列表）](https://meta.fabricmc.net/)、[Fabric for Minecraft 26.2 公告](https://fabricmc.net/2026/06/15/262.html)
- [NeoForge 官网 / 26.1 发布公告](https://neoforged.net/news/26.1release/)、[NeoForge Maven 直链规律](https://maven.neoforged.net/)、[NeoForge 官方服务端安装文档](https://docs.neoforged.net/user/docs/server/)
- [Forge 官方下载索引（1.21.11 仍在更新）](https://files.minecraftforge.net/net/minecraftforge/forge/index_1.21.11.html)
- [Adoptium API v3 Cookbook](https://github.com/adoptium/api.adoptium.net/blob/main/docs/cookbook.adoc)、[清华 TUNA Adoptium 镜像（实测）](https://mirrors.tuna.tsinghua.edu.cn/Adoptium/)
