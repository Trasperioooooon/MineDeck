# MineDeck · 我的世界（Minecraft）一键开服工具

[![Release](https://img.shields.io/github/v/release/Trasperioooooon/MineDeck?color=6fd24b)](https://github.com/Trasperioooooon/MineDeck/releases/latest) [![Downloads](https://img.shields.io/github/downloads/Trasperioooooon/MineDeck/total?color=6fd24b)](https://github.com/Trasperioooooon/MineDeck/releases) [![License](https://img.shields.io/github/license/Trasperioooooon/MineDeck?color=6fd24b)](LICENSE) ![Platform](https://img.shields.io/badge/platform-Windows-2b6cb0) ![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)

Windows 上的**我的世界（Minecraft Java 版）一键开服工具**。一个文件 `MineDeck.exe`，大约 13 MB。它会下载 Java 和服务端，用窗口管理服务器。整合包可以直接导入。樱花穿透和 OpenFrp 已经做在里面，不在同一个网络的朋友也能连进来。也可以接上你自己的 API Key。接上以后可以用开服助手、游戏内问答、会跟随和砍树打怪的 AI 伙伴，还有音乐共享，把房主正在放的音乐传给服务器里的人。

选好版本，点创建，后面会自动做完：按版本下载 Java，下载服务端，运行安装器，同意 EULA，写出配置和启动脚本，然后可以在窗口里管理服务器。下载 Java 和服务端时优先用国内镜像（BMCLAPI、清华 TUNA），镜像不可用就改用官方源。

> 不在同一个网络时，可以在「内网穿透」页添加樱花穿透、OpenFrp，或填你自己的 frps。隧道可以跟着服务器一起启动。公网地址可以复制给朋友。

<p align="center"><img src="docs/images/01-servers.png" width="860" alt="MineDeck 主界面：我的世界服务器列表，显示每台服务器的核心、版本、运行状态、在线人数和启动按钮"></p>

---

## ✨ 功能一览

| 能力 | 说明 |
|---|---|
| **十二种服务端核心** | 原版。插件服是 Paper、Purpur、Leaves、Folia。模组服是 Fabric、NeoForge、Forge。模组和插件都能用的是 Mohist、Banner。群组代理是 Velocity、Waterfall。 |
| **本机 Java 扫描** | 会在这台电脑上找已经装好的 Java，能用就直接用，不再下载 |
| **控制台** | 可以在 GBK 和 UTF-8 之间切换，老整合包出现乱码时用这个。警告和错误会标上颜色。日志可以过滤，命令有历史记录。常用命令和在线玩家名可以用 Tab 补全。 |
| **MOTD 彩色编辑器** | 编辑服务器介绍时可以选颜色，旁边能看到效果。中文和表情符号会按游戏要求的格式自动处理。 |
| **版本范围** | 支持 1.12.2 到目前的最新版，包括 26.x 这种按年份编号的版本。版本列表从官方清单读取，以后的新版本也会出现在列表里。 |
| **Java 不用自己装** | 按游戏版本选择对应的 Java，常见的是 8、16、17、21、25。电脑里已经有合适的就直接用，不再下载。没有的话会下载一份，只给 MineDeck 用，不改系统设置，也不影响你已经装好的 Java。 |
| **下载与重试** | 下载时按这个顺序尝试：先是国内镜像 BMCLAPI 和清华 TUNA，然后是 Adoptium 和官方源，最后是 Mojang 提供的 Java。某个源失败就换下一个，最多重试 3 轮。下完会校验文件是否完整。 |
| **安装器加固** | 安装 Forge 或 NeoForge 时，会自动使用 BMCLAPI 镜像，并事先放好原版服务端，免得安装器再去连 Mojang。失败会自动重试 3 次。 |
| **图形界面** | 用窗口管理服务器。电脑没有 WebView2 时，会改用浏览器打开。界面里有创建向导、实时控制台、命令输入和配置。配色有 7 套，默认是「方块」（暖沙色底、草绿色），另外有 Fluent、GitHub、石墨、Nord、Catppuccin、Solarized。每套都可以浅色、深色或跟随系统，还可以另选 8 种强调色。字体放在程序里，换一台电脑看起来也一样。图标是像素风的「体素机柜」，12 种核心各有头像。刚打开时不会先闪一下白屏。高分辨率屏幕上画面保持清晰。 |
| **系统托盘与自启** | 可以设成关掉窗口时缩到系统托盘，服务器继续运行。托盘右键有「打开面板」和「全部停止并退出」。也可以开机自动启动，启动后直接进托盘，不弹出窗口。这几项都在「设置 → 启动与托盘」里开关。 |
| **存储位置自选** | 服务器和备份可以改存到别的盘，用系统的选择文件夹窗口来改。可以设多个存放位置，打开时都会认到。中文服务器名不会直接拿来当文件夹名。名字里没有英文或数字时，文件夹会叫 server- 加上创建时间。 |
| **日常使用** | 第一次打开会有引导。控制台里输入命令可以补全，常用命令和在线玩家名按 Tab 都能出。备份可以设置保留多少份。把图片放进程序旁边的 `bg` 文件夹，就能当背景。 |
| **整合包导入** | 把 Modrinth 整合包（.mrpack）拖进来，就能建好服务器。核心、版本和加载器会自动识别，只给客户端用的文件会跳过。CurseForge 的 zip 也可以导入，需要自己填 API Key。 |
| **内网穿透** | 可以接 OpenFrp、樱花穿透，也可以填你自己的 frps。穿透客户端 frpc 会自动下载，并跟着服务器启动。公网地址可以在页面上复制。 |
| **备份、重启和定时任务** | 可以备份和还原世界，服务器开着的时候也能备份。崩溃后会自动再启动，先等 10 秒，再等 30 秒，再等 60 秒，连续 3 次仍起不来就停止。正常运行超过 10 分钟后，这个计数会清零。还可以按天定时重启、执行命令或备份。 |
| **玩家管理** | 可以看在线玩家，踢出或设为 OP。白名单、管理员和封禁可以在界面里改。服务器关着的时候，改的是名单文件本身。离线模式的 UUID 会自动算好。 |
| **资源浏览器** | 实例内直接搜索安装 Modrinth 模组/插件（按核心与版本自动过滤兼容项） |
| **远程管理** | 可以打开局域网访问，用密码登录，手机上也能管服务器。另外有 REST API，也可以用 Webhook 把事件推送给别的程序（见 docs/API.md）。 |
| **自动更新** | 可以检查 GitHub 上的新版本。国内访问不稳时会改用加速镜像。更新时会下载新版本并重新启动。 |
| **全部服务器配置** | `server.properties` 里大约 60 项，分成 7 组。可以搜索。每项有中文说明、取值范围，以及要不要重启。当前游戏版本用不到的项会隐藏。Minecraft 1.21.9 及以后，已经改到游戏规则的项，在界面里改完马上生效。新服务器默认关闭正版验证，默认允许飞行。 |
| **进程托管** | 可以启动和停止服务器。停止时会先发送 `stop`，30 秒还没退出就强制结束。启动前会检查端口是否被占用。关掉 MineDeck 时，服务器会一起停，避免 Java 进程留在后台。 |
| **AI 接入（可选）** | 填自己的 API Key：OpenAI 兼容和 Claude 两种协议，内置 DeepSeek / 通义千问 / Kimi / 智谱 / OpenAI / Claude / Ollama（本机）七个预设和自定义地址。MineDeck 会按公开价格估算每次大概花多少钱。每个功能都能设每天的用量上限，按 token 算。Key 加密保存在这台电脑上，除了你选的服务商，不会发给别人。不配置 AI，就不会连接任何 AI 服务。 |
| **应用内 AI 助手** | 标题栏里的「AI 助手」会先看日志、配置和崩溃报告再回答。改配置、换 Java 或重启服务器，会先给出确认卡片，你点了才执行。发给 AI 的内容会先去掉本机路径、局域网地址和密钥，你随时可以看实际发出去的原文。控制台顶部的提示条能认出常见的开服失败原因，例如缺前置模组、把客户端模组放进了服务器、端口被占、Java 版本不对。这一步不需要 AI。 |
| **游戏内 AI** | 玩家在聊天里说「@AI」再加上问题，回复默认所有人都能看见。`/私` 只回复提问的人。`/问` 只回答，不执行命令。`/额度` 看今天还能问几次。`/帮助` 看怎么用。你在面板里勾选的玩家，可以让 AI 执行白名单里的命令。每条命令的参数都会按允许的写法核对。op、execute 这类命令不会执行。白名单也可以在面板里打开。 |
| **音乐共享** | 把房主电脑上音乐软件的声音，经 Simple Voice Chat 放给服务器里的人听。每个人可以在语音设置里单独调小或静音。在 Windows 11 上，可以只录你选的那个音乐程序，不会录进游戏声音和语音。桥接模组已经带在 MineDeck 里，在音乐共享页安装即可。 |
| **AI 伙伴** | 可以在 NeoForge 1.21.1 或 Forge 1.20.1 的服务器里放 AI 伙伴。聊天里说「@伙伴名 跟着我」「砍 10 个橡木原木」「打附近的僵尸」，它就会去做。可以设置不让它挖的区域，也可以指定不许打的生物。好友那边只需要装一个伙伴模组。 |
| **MCP 接口（可选）** | 可以让 Claude Desktop、Cherry Studio、Cursor、VS Code 这些支持 MCP 的工具连上 MineDeck。查状态、看日志可以直接做。改配置、重启只会先提出来，你在 MineDeck 里确认后才执行。这个接口默认关着，只接受本机的连接，而且要带令牌。 |
| **启动时的保护** | 较老的版本会自动打开 log4shell 的官方缓解。启动服务器时会清掉 `JAVA_HOME` 这类环境变量，避免用到别的 Java。下载的文件会做完整性校验。本机接口带一组随机令牌，用来防止别的程序随便调用。 |

## 📸 截图

| **AI 开服助手**：先查模组清单再找原因，要改东西先给确认卡片 | **报错横幅**：不用 AI 也能指出少装前置、客户端模组进了服务端 |
|---|---|
| <img src="docs/images/03-assistant.png" alt="AI 助手面板：AI 读了模组清单，找出小地图模组是客户端模组，提出禁用它的确认卡片"> | <img src="docs/images/02-console-banner.png" alt="控制台顶部的报错横幅：缺少前置模组 geckolib，客户端模组放进了服务端，附修复办法和「让 AI 分析」按钮"> |
| **游戏内 AI**：聊天里 @AI 提问，授权的玩家能让它执行白名单里的命令 | **AI 伙伴**：刷出伙伴、看它在干什么，禁挖区域保护你的建筑 |
| <img src="docs/images/04-gameai-log.png" alt="游戏内 AI 的问答和执行记录：执行了 weather clear，拒绝了 op"> | <img src="docs/images/05-companion.png" alt="伙伴标签：已连上伙伴模组，小方正在采集橡木原木，阿土空闲，两个禁挖区域"> |
| **音乐共享**：选一个音乐软件，经 Simple Voice Chat 放给全服听 | **接入 AI**：七个预设，填 Key、测试连接、设每日额度 |
| <img src="docs/images/06-music.png" alt="音乐共享标签：选网易云音乐作音源，检查清单全部通过"> | <img src="docs/images/07-settings-ai.png" alt="设置里的 AI 分区：已启用 DeepSeek，测试连接正常、能调用工具"> |

## 🚀 使用

1. 把 `MineDeck.exe` 放进一个**单独的文件夹**。服务器、下载的 Java 和缓存都在这个文件夹里。只有界面上的偏好设置放在 `%LocalAppData%\MineDeck`。
2. 双击运行，在窗口中点「新建服务器」→ 选核心 → 选版本 → 选构建 → 勾选 EULA → 创建。
3. 创建完成后点「▶ 启动」，等控制台出现 `Done (...)!` 即开服成功。
4. 同一个局域网里的朋友，用 `你的内网IP:端口` 连接。不在同一个网络的朋友，用「内网穿透」页的隧道，连同一个端口。

常见操作：
- **发命令**：实例 → 控制台 → 底部输入框（`op 玩家名`、`whitelist add 玩家名`、`say 内容`…）
- **改配置**：打开实例，进「常用设置」或「全部配置」。有的项要重启才生效，界面上会标出来。
- **调内存**：实例 → 内存/启动 → 滑块
- **手动开服**：每个实例目录内有与 GUI 等效的 `run.bat`
- **命令行模式**：`MineDeck.exe -nogui -port 27333`（仅后台服务，浏览器访问日志中的 URL）

## ❓ 常见问题

### 要自己装 Java 吗？

不用自己装。电脑里已经有合适的 Java，MineDeck 会直接用。没有的话，它会按游戏版本下载一份对应的 Java，只给 MineDeck 自己用，不改系统设置，也不影响你装的其他软件。

### 朋友不在同一个网络，怎么连进来？

在「内网穿透」页添加一条隧道，樱花穿透、OpenFrp 或你自己的 frps 都可以。把隧道绑定到这台服务器，并打开「跟随实例启动」。开服后把公网地址发给朋友。同一个局域网的朋友不用隧道，直接连 `你的内网IP:端口`。

### 支持哪些版本和核心？

支持 MC 1.12.2 到最新版，包括 26.x 起的年份版本号。核心有 12 种：原版、Paper、Purpur、Leaves、Folia、Fabric、NeoForge、Forge、Mohist、Banner、Velocity、Waterfall。Modrinth（`.mrpack`）和 CurseForge（zip）的整合包可以直接导入。

### 服务器起不来、报错看不懂怎么办？

开服失败时，先看控制台顶部的提示条。缺前置模组、把客户端模组放进了服务器、端口被占、Java 版本不对，这些常见原因它都能直接认出来，并告诉你怎么改。接了 AI 的话，还可以点「让 AI 分析」，或者在 AI 助手里直接问。

### AI 功能要花钱吗？会不会偷偷联网？

AI 是可选功能。不配置 AI，MineDeck 就不会连接任何 AI 服务。要用的话，填入你自己的 API Key，费用由你选的服务商直接收取。MineDeck 会估算每次大概花多少钱，每个功能都能设每天的用量上限。Key 加密保存在这台电脑上，除了你选的服务商，不会发给别人。

### AI 会不会乱改我的服务器？

AI 不会擅自改你的服务器。看日志、读配置这类只读的事，它可以直接做。改配置、换 Java、重启服务器之前，它会先弹出一张确认卡片，你点了确认才会执行。每一步都会记在本机的操作记录里（`logs\ops` 文件夹）。游戏内 AI 默认只回答问题。只有你勾选的玩家，才能让它执行命令，而且只能执行白名单里的命令。`op`、`execute` 这类命令不会执行。从 MCP 连进来的外部 AI 工具也一样。看状态、读日志可以直接做。要改服务器，得你在 MineDeck 里确认以后才会执行。

### AI 伙伴和音乐共享，好友要装什么？

- AI 伙伴：好友的客户端只装伙伴模组，发布页有下载。
- 音乐共享：服务器和好友都装同一版本的 Simple Voice Chat，桥接模组由 MineDeck 装进服务器。

### 数据存在哪？怎么卸载？

数据都在 exe 所在的文件夹里，只有界面偏好存在 `%LocalAppData%\MineDeck`。卸载就是删掉这两个文件夹。

### Windows 提示「已保护你的电脑」？

exe 没有数字签名，点「更多信息 → 仍要运行」即可。源码公开（MIT），也可以自己构建（见下文）。

## 🔧 从源码构建

需要 Go 1.25+（Windows）：

```powershell
$env:GOPROXY = 'https://goproxy.cn,direct'   # 国内加速
go build -trimpath -ldflags="-s -w -H windowsgui" -o MineDeck.exe .
```

调试版（带控制台日志）：`go build -o minedeck-cli.exe .` 后运行 `minedeck-cli.exe -nogui`。

界面字体子集（`internal/web/ui/fonts/`）和 exe 图标资源（`rsrc_windows_amd64.syso`）都已入库，构建时不需要 Python 或其他工具。改了界面文案要检查字体子集（见 [`tools/fonts/README.md`](tools/fonts/README.md)），换了 `assets/icon.ico` 要重新生成 syso（命令见 `design-system/MASTER.md` §7）。

## 🏗️ 架构

```
main.go / tray_windows.go / dpi_windows.go / webviewbg_windows.go
            入口：HTTP 服务 + WebView2 窗口/浏览器回退 + 系统托盘（关窗最小化）+ DPI 感知 + 冷启动底色 + 优雅退出
internal/
├── app/       基础目录与全局配置（下载源策略、内存探测、多存储根）
├── autostart/ 开机自启（读写 HKCU\...\Run，每用户免管理员）
├── dl/        多源故障转移下载器（重试/看门狗/SHA-1/256/MD5/原子落盘）
├── mcsrc/     12 核心版本源注册表：Mojang 清单(BMCLAPI 镜像)、Forge、NeoForge、
│              Fabric Meta、Paper Fill v3、Purpur、Mohist、FastMirror 聚合…
├── java/      便携 Java 三级下载链 + 本机 Java 扫描复用
├── inst/      实例：创建流水线、安装器托管、run.bat 生成、server.properties
│              保序读写、进程状态机、控制台环形缓冲 + 订阅、备份/策略/玩家
├── modpack/   整合包解析（Modrinth .mrpack / CurseForge zip）
├── tunnel/    内网穿透（OpenFrp / 樱花frp / 自定义 frps，frpc 托管；游戏 TCP / 语音 UDP）
├── ops/       操作层：所有界面和 AI 发起的操作统一经这里（参数说明、风险等级、提案与确认、操作记录）；具体操作在 ops/catalog
├── secret/    DPAPI 加密保存密钥（CurseForge Key、穿透凭据、AI Key）
├── mclog/     服务器日志解析（进服、退服、聊天，防伪造）
├── ai/        AI 接入层：OpenAI 兼容 / Claude 协议（自己发 HTTP、解析 SSE）、预设、价格、记账与额度
├── assistant/ 应用内 AI 助手（会话、工具映射、提案流程、上下文打包与脱敏）
├── diag/ modinfo/  报错规则（报错横幅的数据来源）、jar 里的模组信息解析
├── chatai/    游戏内 AI（认聊天、提问选项、命令白名单、tellraw 回复）
├── music/ musicshare/ bridge/   音乐共享：WASAPI 采集、广播与检查清单、SVC 桥接模组的协议客户端和内置 jar
├── companion/ AI 伙伴：伙伴模组的协议客户端、安装（Modrinth，sha512 校验）、管理器和内置 jar
├── tasks/     后台任务进度 + 结构化告警（供前端轮询）
├── events/ webhook/ update/   事件总线、Webhook 推送、GitHub 自动更新
├── web/       REST API + SSE 控制台流 + go:embed 前端（ui/ 多配色 SPA，配色令牌见 themes.css，内嵌字体在 ui/fonts/）
└── themes/    配色方案对比度测试（go test ./internal/themes/，校验 themes.css）
tools/ui-dev/  前端仿真后端 + 交互回归测试 + 截图（Node + Playwright，不需要 Windows，见其 README）
tools/fonts/   内嵌字体的子集生成脚本（Python + fontTools，手动运行，产物入库）
tools/promo/   README 截图和社交预览图的生成脚本（在仿真后端上拍真实界面，用法见脚本开头）
bridge/voicechat/  音乐共享的 SVC 桥接模组源码（javac 构建，产物入库在 internal/bridge/jar，见其 README）
companion/     AI 伙伴模组源码（NeoForge 1.21.1 / Forge 1.20.1，Gradle 构建，产物入库在 internal/companion/jar，见其 README）
assets/        应用图标 icon.ico（像素画逐尺寸绘制）；界面设计规范见 design-system/MASTER.md
```

关键设计决策见 `docs/MC自动化开服技术实现方案与版本映射表.md`（含实测验证过的版本映射与下载源矩阵）。

## 📋 版本 ↔ Java 对照（自动处理，无需记忆）

| MC 版本 | Java | 服务端可选核心 |
|---|---|---|
| 1.12 – 1.16.5 | 8 | Vanilla / Paper / Purpur(1.14+) / Forge / Fabric(1.14+) |
| 1.17.x | 16 | 同上 |
| 1.18 – 1.20.4 | 17 | 同上 |
| 1.20.5 – 1.21.x | 21 | + NeoForge(1.20.2+) |
| 26.1+（年份版本号） | 25 | Vanilla / Paper / Purpur / Fabric / NeoForge |

*实际以这个游戏版本要求的 Java 大版本为准。上表只是连不上版本列表时的对照。*

## 🔤 第三方字体与许可

界面内嵌三款开源字体（经 `go:embed` 打包进 exe，运行时不联网下载），均为 [SIL Open Font License 1.1](https://openfontlicense.org/)：

| 字体 | 用途 | 版权 | 来源 |
|---|---|---|---|
| **Geist** | 西文正文、数字 | Copyright 2024 The Geist Project Authors | Vercel 官方 npm 包 `geist@1.8.2`（[vercel/geist-font](https://github.com/vercel/geist-font)） |
| **Geist Mono** | 控制台、路径、端口等等宽文字 | 同上 | 同上 |
| **Noto Sans SC** | 中文 | © 2014-2021 Adobe，保留字体名「Source」 | [google/fonts](https://github.com/google/fonts) 的 `ofl/notosanssc`（与思源黑体同源） |

- 三款字体都是子集：西文保留拉丁字符与常用符号，中文保留 GB2312 全部 6763 个汉字、GB2312 符号区与界面用到的全部字符；字重轴限制在 400–600。子集之外的生僻字由系统字体（微软雅黑等）补上。
- 子集化属于 OFL 允许的修改。子集保留了字体内的版权与许可信息，许可原文随字体放在 [`internal/web/ui/fonts/`](internal/web/ui/fonts/)（`OFL-Geist.txt`、`OFL-NotoSansSC.txt`）。字体不单独出售；修改后的字体名不含保留字体名「Source」。
- 子集由 [`tools/fonts/build_fonts.py`](tools/fonts/) 从固定版本的源文件生成，来源、校验和与字符集构成见 [`tools/fonts/README.md`](tools/fonts/README.md)。

## ⚠️ 已知边界

- Paper 和 Purpur 没有国内镜像，一般直接连官方地址就能下载。Forge 和 NeoForge 安装时，有一部分依赖库仍然要连官方源。程序会为安装器使用镜像并自动重试。官方源连不上时，这一步还是可能失败。
- 首次启动某实例时 Windows 防火墙可能弹窗，请选择「允许访问」，否则局域网玩家无法连接。
- 删掉 `runtimes` 文件夹以后，已经建好的服务器会启动不了。重新创建实例，缺少的 Java 会自动补上。

## English

MineDeck is a Minecraft (Java Edition) server tool for Windows. It is one file, `MineDeck.exe`, about 13 MB. It downloads Java and the server software and manages servers in a window. Modpacks can be imported directly. SakuraFrp and OpenFrp are built in, so friends who are not on the same network can join. You can also add your own API key.

Optional AI features use your own API key (OpenAI-compatible or Claude protocol; presets for DeepSeek, Qwen, Kimi, Zhipu, OpenAI, Claude and Ollama):

- an in-app assistant that reads logs, configs and crash reports before it answers. Changing a config, switching Java or restarting shows a confirmation card and runs only after you confirm. Paths on this PC, LAN addresses and keys are removed before the text is sent, and you can see exactly what was sent;
- in-game AI: players say `@AI` and then a question. Replies are visible to everyone by default. `/额度` shows how many questions are left today. Players you tick can have it run allowlisted commands. `op` and `execute` are not run;
- AI companions on NeoForge 1.21.1 and Forge 1.20.1. In chat you can tell one to follow, chop wood or fight nearby mobs. Friends only install one companion mod;
- music sharing sends the host's music player to everyone on the server through Simple Voice Chat. On Windows 11 it can record only the program you pick, without game sound or voice chat;
- an MCP interface, off by default. It accepts connections only from this PC, and it requires a token. Claude Desktop, Cherry Studio, Cursor and VS Code can read status and logs directly. Changes to the config or a restart run only after you confirm them in MineDeck.

The interface is Chinese only for now. MIT licensed.
