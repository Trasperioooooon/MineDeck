简体中文 | [English](README_EN.md)

<p align="center">
  <img src="internal/web/ui/img/logo-144.png" width="96" alt="MineDeck 图标">
</p>

<h1 align="center">MineDeck</h1>

<p align="center">Windows 上的我的世界（Minecraft Java 版）一键开服工具</p>

<p align="center">
  <a href="https://github.com/Trasperioooooon/MineDeck/releases/latest"><img alt="最新稳定版" src="https://img.shields.io/github/v/release/Trasperioooooon/MineDeck"></a>
  <a href="https://github.com/Trasperioooooon/MineDeck/releases"><img alt="GitHub Releases 累计下载" src="https://img.shields.io/github/downloads/Trasperioooooon/MineDeck/total"></a>
  <a href="https://github.com/Trasperioooooon/MineDeck/stargazers"><img alt="GitHub stars" src="https://img.shields.io/github/stars/Trasperioooooon/MineDeck"></a>
  <a href="LICENSE"><img alt="许可证" src="https://img.shields.io/github/license/Trasperioooooon/MineDeck"></a>
</p>

MineDeck 是 Windows 上的我的世界（Minecraft Java 版）一键开服工具。整个程序就是一个 `MineDeck.exe`，十几 MB，不用安装。它会替你下载 Java 和服务端，在一个窗口里创建和管理服务器。整合包可以直接导入。OpenFrp 和樱花穿透已经做在里面，不在同一个网络的朋友也能连进来。

选好核心和版本，勾选 EULA，点创建，后面的事它自己做完：按游戏版本准备 Java，下载服务端，运行安装器，写好配置和启动脚本。下载时优先走国内镜像（BMCLAPI、清华 TUNA），镜像不可用再换官方源。

想用 AI 的话，可以接上你自己的 API Key：开服助手会先读日志和配置再回答，玩家能在游戏聊天里向 AI 提问，服务器里还可以放会跟随、采集、打怪的 AI 伙伴。这些都是可选的，不配置就不会连接任何 AI 服务。另外有一个不靠 AI 的音乐共享，把房主电脑上正在放的音乐传给服务器里的人听。

<p align="center"><img src="docs/images/01-servers.png" width="860" alt="MineDeck 主界面：服务器列表，显示每台服务器的核心、版本、运行状态、在线人数和启动按钮"></p>

## 它能做什么

这里只讲个大概。每一项的默认值、上限和边界情况，都写在 [docs/功能清单.md](docs/功能清单.md) 里。

### 开服

- **十二种服务端核心**：原版；插件服 Paper、Purpur、Leaves、Folia；模组服 Fabric、Forge、NeoForge；模组和插件都能用的 Mohist、Banner；群组代理 Velocity、Waterfall。
- **版本**：Minecraft 1.12.2 到最新的正式版，包括 26.x 这种按年份编号的版本。版本列表从官方清单读取，以后出的新版本也会出现在列表里。
- **Java 不用自己装**：按游戏版本选对应的 Java，常见的是 8、16、17、21、25。电脑里已经有合适的就直接用；没有就下载一份，只给 MineDeck 用，不改系统设置，也不影响你已经装好的 Java。
- **下载会自己换源和重试**：一个源失败就换下一个，最多试 3 轮，下完会校验文件是否完整。安装 Forge 或 NeoForge 时让安装器走镜像，失败了自动再跑，最多跑 3 次。
- **整合包导入**：Modrinth 的 `.mrpack` 可以直接导入，核心、版本和加载器自动识别，只给客户端用的文件会跳过。CurseForge 的 zip 也能导入，填上你自己的 CurseForge API Key 以后才能自动下载里面的文件。

### 管理

- **控制台**：实时日志，警告和错误会标上颜色，可以过滤。命令有历史记录，常用命令和在线玩家名按 Tab 补全。老整合包出现乱码时，可以在 GBK 和 UTF-8 之间切换。
- **改配置不用翻文件**：「常用设置」把 `server.properties` 里的 78 项分成 7 组，每项有中文名和说明，要重启才生效的会标出来。「全部配置」列出文件里所有的键值，可以搜索。服务器介绍（MOTD）可以选颜色，旁边能看到效果。
- **玩家**：看在线玩家，踢出、封禁、设为 OP，改白名单。服务器关着的时候，改的是名单文件本身。
- **备份和定时任务**：可以备份和还原世界，服务器开着的时候也能备份，默认每台服务器保留 10 份。可以按天定时重启、执行命令或备份。崩溃后自动重启默认关着；打开以后先等 10 秒，再等 30 秒，再等 60 秒，连续 3 次仍起不来就停止。
- **窗口和托盘**：可以设成关掉窗口时缩到系统托盘，服务器继续运行。也可以开机自动启动，启动后直接进托盘。界面有 7 套配色，每套都可以浅色、深色或跟随系统。
- **存放位置**：服务器和备份可以改存到别的盘。
- **启动时的保护**：较老的游戏版本会自动加上 log4shell 的缓解参数。启动服务器时会清掉 `JAVA_HOME` 这类环境变量，免得用到别的 Java。关掉 MineDeck 时服务器会一起停，不会有 Java 进程留在后台。

### 联机

- **内网穿透**：可以接 OpenFrp、樱花穿透，也可以填你自己的 frps。隧道可以绑定到某台服务器，跟着它一起启动，公网地址在页面上复制。OpenFrp 和自建 frps 用的客户端会自动下载；樱花穿透的客户端要按提示自己下载一次。
- **局域网访问**：打开「允许局域网访问」并设一个访问密码，重启 MineDeck 以后，同一局域网里的手机和别的电脑也能打开管理页面。
- **API 和 Webhook**：有本地 HTTP API。服务器启动、停止、崩溃、玩家进出这类事件可以推送到你填的地址。见 [docs/API.md](docs/API.md)。

### 出了问题

- **报错横幅**：服务器起不来时，控制台顶部会直接指出常见的原因，比如缺前置模组、把客户端模组放进了服务器、端口被占、Java 版本不对，一共 13 类。这一步不需要 AI。

### AI（可选，用你自己的 Key）

- **接入**：支持 OpenAI 兼容和 Claude 两种协议，内置 DeepSeek、通义千问、Kimi、智谱、OpenAI、Claude、Ollama（本机）七个预设，也可以填自定义地址。Key 加密保存在这台电脑上。MineDeck 按公开价格估算费用，每个功能都可以设每天的 token 额度。
- **应用内助手**：标题栏里的「AI 助手」会先读日志、配置和崩溃报告再回答。要改配置、换 Java 或重启服务器，它会先给出一张确认卡片，你点了才执行。发给模型的内容会先替换掉 Windows 用户名、局域网地址和密钥，你随时可以看实际发出去的原文。
- **游戏内 AI**：玩家在聊天里说 `@AI` 再加上问题就能问。你在面板里勾选的玩家，可以让它执行白名单里的命令，比如改时间、改天气、传送自己。`op`、`execute` 这类命令不会执行。
- **AI 伙伴**：可以在 NeoForge 1.21.1 或 Forge 1.20.1 的服务器里放 AI 伙伴。在聊天里说「@伙伴名 跟着我」，或者让它去采集、打怪、把东西交给你，它就会去做。可以划出不让它挖的区域，也可以指定不许打的生物。好友的客户端要装一个伙伴模组。
- **MCP 接口**：默认关着。打开以后，支持 MCP 的 AI 工具可以连上 MineDeck：查状态、读日志可以直接做；改配置、重启只会先提出来，你在 MineDeck 里确认后才执行。只接受本机的连接，而且要带令牌。

### 音乐共享

把房主电脑上音乐软件的声音，经 Simple Voice Chat 放给服务器里的人听。每个人可以在语音设置里单独调小或静音。在 Windows 11 上可以只录你选的那个音乐程序，不会录进游戏声音和语音；Windows 10 只能录整机的声音。需要 NeoForge 1.20.5 及以后，或者 Forge、Mohist 1.20.1 及以后的服务器。

## 3.1.0 测试版里新增的

下面这些目前只在 v3.1.0 的测试版里，稳定版 v3.0.0 没有。

- **交接包**：把一台停着的服务器打成一个 `.mdpack` 文件，交给另一台电脑上的 MineDeck 接着开。包里有存档、配置、模组、插件和名单。配置文件是原样进包的，里面写着的密码也会一起交出去，在别处也用着的先改掉。收包的人要知道：包里的模组和插件会在你的电脑上运行，只导入信得过的人给的包。给收包人的说明见[接收者上手指南](docs/接收者上手指南.md)。
- **待装清单**：打开就列着当前核心和版本下最热门的模组或插件，可以按类别筛，也可以搜。挑中的放进待装清单，前置自动补齐，一次装好。自己下载好的 jar 也能从电脑上直接加进清单，MineDeck 会帮着查它还缺哪些前置。新建向导里也多了可选的「选模组」一步（插件端叫「选插件」）。从 Modrinth 下载的文件，下载后会校验。
- **整合包里下载不了的文件可以逐个处理**：以前只要有一个文件的下载地址不在允许的站点里，或者没有校验值，整个整合包就导不进来。现在整合包照常导入，这些文件列在任务页的「待决文件」里，可以跳过，可以用本地的文件代替，也可以看过风险说明后信任那个站点再下载。
- **接管已有服务器**：以前自己开的服务器，选它所在的文件夹，就能收进来一起管理。核心、版本、启动文件和原来分的内存会自动认出来。文件留在原地，原来的启动脚本会先留一份备份。
- **在界面上换 Java**：每台服务器的「内存 / 启动」里可以换 Java。
- 另外有两处小改动：删除服务器时选了不删文件夹的，现在是真的移出列表，下次启动不会又冒出来，以后还能再接管回来；后台任务在切页或刷新以后还能找回进度。

测试版在[发布页](https://github.com/Trasperioooooon/MineDeck/releases)上标着 Pre-release，要手动下载。用着稳定版的不会收到测试版的更新提示，测试版之间也不会自动更新，要手动换掉 `MineDeck.exe`。等 v3.1.0 正式发布，装着测试版的人可以在「设置 → 更新与关于」里直接更新过去。

试测试版时请把它放进一个单独的文件夹；要拿已有的服务器来试，先把整个文件夹备份一份。每一版已知的问题写在 [docs/releases/](docs/releases/) 里的发布说明中。

## 截图

| **AI 助手**：先查模组清单再找原因，要改东西先给确认卡片 | **报错横幅**：不用 AI 也能指出少装前置、客户端模组进了服务端 |
|---|---|
| <img src="docs/images/03-assistant.png" alt="AI 助手读了模组清单，指出客户端模组，并给出禁用该文件的确认卡片"> | <img src="docs/images/02-console-banner.png" alt="控制台顶部的报错横幅：缺少前置模组 geckolib，以及客户端模组放进了服务端"> |
| **游戏内 AI**：聊天里 @AI 提问，授权的玩家能让它执行白名单里的命令 | **AI 伙伴**：刷出伙伴、看它在干什么，禁挖区域保护你的建筑 |
| <img src="docs/images/04-gameai-log.png" alt="游戏内 AI 记录：执行了 weather clear，拒绝了 op"> | <img src="docs/images/05-companion.png" alt="伙伴页：已连接模组，一个伙伴在采集，一个空闲，以及两块禁挖区域"> |
| **音乐共享**：选一个音乐软件，经 Simple Voice Chat 放给全服听 | **接入 AI**：七个预设，填 Key、测试连接、设每日额度 |
| <img src="docs/images/06-music.png" alt="音乐共享：选择网易云音乐作为音源，检查清单已通过"> | <img src="docs/images/07-settings-ai.png" alt="设置中的 AI 页：已启用 DeepSeek，测试连接成功"> |

## 下载和安装

到 [GitHub Releases](https://github.com/Trasperioooooon/MineDeck/releases) 下载 `MineDeck.exe`。页面上标着 Latest 的是稳定版，标着 Pre-release 的是测试版。官网和国内下载镜像还在准备，上线以后会把地址补在这里。

1. 把 `MineDeck.exe` 放进一个单独的文件夹。服务器、下载的 Java、缓存和备份默认都在这个文件夹里，只有界面上的偏好设置放在 `%LocalAppData%\MineDeck`。
2. 双击运行。Windows 若提示「Windows 已保护你的电脑」，点「更多信息 → 仍要运行」。这个 exe 没有数字签名；源码是公开的，也可以自己构建。
3. 要用 AI 伙伴的话，同一个发布页上还有给好友客户端用的伙伴模组 jar。按服务器是 NeoForge 1.21.1 还是 Forge 1.20.1 选一个，放进客户端的 `mods` 文件夹。

从 v2.6.1 及以前的版本升级（那时的文件名是 `AutoMCHUB.exe`）：自动更新找不到改名后的文件，需要手动把 `MineDeck.exe` 放进原来的文件夹。旧的开机自启项会被删掉，需要的话在「设置 → 启动与托盘」里重新打开。

卸载：开过开机自启的，先在「设置 → 启动与托盘」里关掉；然后删掉 exe 所在的文件夹和 `%LocalAppData%\MineDeck`。把服务器或备份改存到了别处的，那些文件夹要另外删。

## 快速开始

1. 打开程序，在窗口里点「新建服务器」。
2. 选核心、版本、构建，勾选 EULA，点创建。
3. 创建完成后点启动。控制台出现 `Done (...)!` 时，这台服务器就开好了。
4. 同一个局域网里的朋友，用 `这台电脑的局域网 IP:端口` 连接。不在同一个网络的朋友，由你在「内网穿透」里加一条隧道，绑定这台服务器并打开「跟随实例启动」，再把公网地址发给他们。

常用的几个地方：

- 发命令：打开服务器 → 控制台 → 底部输入框。
- 改配置：打开服务器 → 常用设置，或全部配置。要重启才生效的项会标出来。
- 调内存：打开服务器 → 内存 / 启动。
- 手动开服：每台服务器的文件夹里有一份和界面等效的 `run.bat`。
- 不开窗口运行：`MineDeck.exe -nogui`，默认端口 27333，被占用时改用随机端口。管理页面的地址和令牌会打印在程序的日志输出里；发布版是窗口程序，在终端里直接运行看不到这一行，需要把标准错误输出重定向到文件。

## 常见问题

### 要自己装 Java 吗？

不用。电脑里已经有合适的 Java，MineDeck 会直接用。没有的话，它会按游戏版本下载一份，只给 MineDeck 自己用，不改系统设置，也不影响你装的其他软件。

### 朋友不在同一个网络，怎么连进来？

在「内网穿透」里添加一条隧道，OpenFrp、樱花穿透或你自己的 frps 都可以。把隧道绑定到这台服务器，并打开「跟随实例启动」。开服后把公网地址发给朋友。同一个局域网里的朋友不用隧道，直接连 `这台电脑的局域网 IP:端口`。

### 支持哪些版本和核心？

Minecraft 1.12.2 到最新的正式版，包括 26.x。核心有 12 种：原版、Paper、Purpur、Leaves、Folia、Fabric、NeoForge、Forge、Mohist、Banner、Velocity、Waterfall。Modrinth（`.mrpack`）和 CurseForge（zip）的整合包可以导入。

### 服务器起不来、报错看不懂怎么办？

先看控制台顶部的报错横幅。缺前置模组、把客户端模组放进了服务器、端口被占、Java 版本不对，这些常见原因它都能直接认出来，并告诉你怎么改。接了 AI 的话，还可以点「让 AI 分析」，或者在 AI 助手里直接问。

### AI 功能要花钱吗？会不会偷偷联网？

AI 是可选的。不配置 AI，MineDeck 就不会连接任何 AI 服务。要用的话，填入你自己的 API Key，费用由你选的服务商直接收取。MineDeck 会估算大概花了多少钱，每个功能都能设每天的用量上限。Key 加密保存在这台电脑上，只会发给你填的那个接口地址。有一点要留意：打开了局域网访问以后，局域网里登录的设备默认也能用 AI 助手，花的是你的额度，不想这样可以在设置里关掉。

### AI 会不会乱改我的服务器？

不会擅自改。看日志、读配置这类只读的事，它可以直接做。改配置、换 Java、重启服务器之前，它会先弹出一张确认卡片，你点了确认才会执行。执行过的操作记在本机的 `logs\ops` 文件夹里。游戏内 AI 默认只回答问题；只有你勾选的玩家才能让它执行命令，而且只能执行白名单里的命令，`op`、`execute` 这类不会执行。从 MCP 连进来的外部 AI 工具也一样：看状态、读日志可以直接做，要改服务器，得你在 MineDeck 里确认以后才会执行。

### AI 伙伴和音乐共享，好友要装什么？

- AI 伙伴：好友的客户端装伙伴模组，发布页有下载，按服务器的加载器和游戏版本选。
- 音乐共享：服务器和好友都装同一版本的 Simple Voice Chat。桥接模组由 MineDeck 装进服务器，好友不用装。

### 稳定版和测试版怎么选？

日常开服用稳定版。想试交接包、待装清单、接管已有服务器这些新功能，再去下测试版，并且照上面「3.1.0 测试版里新增的」一节说的，放进单独的文件夹、先备份。

### 数据存在哪？怎么搬家？

都在 exe 所在的文件夹里：`servers`、`backups`、`runtimes`（下载的 Java 和穿透客户端）、`cache`、`logs`、`data`，以及 `config.json`、`secrets.json`、`tunnels.json` 这几个文件。只有界面偏好存在 `%LocalAppData%\MineDeck`。搬家时把整个文件夹拷走，然后留意两件事。`secrets.json` 和 `tunnels.json` 里的密钥是按这台电脑上的这个 Windows 用户加密的，换了电脑或换了用户要重新填一遍。每台服务器记的是 Java 的完整路径，新位置和原来的路径不一样时，用着 MineDeck 自带 Java 的服务器会提示找不到 Java，要重新指定一次：测试版在「内存 / 启动」里选；稳定版界面上没有这个入口，接了 AI 的可以让助手换。

## 已知的限制

- 界面只有中文，程序只有 Windows 版。
- Paper、Purpur 这些核心没有国内镜像，一般直接连官方地址就能下载。安装 Forge 和 NeoForge 时，有一部分文件仍然要从官方源取，官方源连不上时这一步可能失败。
- 第一次启动某台服务器时，Windows 防火墙可能弹窗，请选「允许访问」，否则局域网里的朋友连不上。
- 不要删 `runtimes` 文件夹。里面是 MineDeck 下载的 Java 和穿透客户端，删了以后，用着这份 Java 的服务器会启动不了。测试版可以在「内存 / 启动」里重新选一个 Java；稳定版界面上没有这个入口，接了 AI 的可以让助手换，否则只能删掉那台服务器重新建。
- 备份只包含世界，不包含模组、插件和配置。
- 更新 MineDeck 之前要先停掉所有服务器。
- Windows 10 上的音乐共享只能录整机的声音。

## 参与和构建

问题和建议请开 [GitHub Issue](https://github.com/Trasperioooooon/MineDeck/issues)。不要在 Issue 里贴 API Key、MCP 令牌、穿透凭据或 CurseForge Key。

在 Windows 上从源码构建需要 Go 1.25 或更高：

```powershell
$env:GOPROXY = 'https://goproxy.cn,direct'
go build -trimpath -ldflags="-s -w -H windowsgui" -o MineDeck.exe .
```

带控制台日志的调试构建：

```powershell
go build -o minedeck-cli.exe .
.\minedeck-cli.exe -nogui
```

界面字体子集和 exe 的图标资源已经在仓库里，构建不需要 Python 或别的工具。改了界面文案要检查字体子集，见 [tools/fonts/README.md](tools/fonts/README.md)。换了 `assets/icon.ico` 要重新生成图标资源，命令在 [design-system/MASTER.md](design-system/MASTER.md) 里。

本地 HTTP API 的说明在 [docs/API.md](docs/API.md)。各版本的发布说明在 [docs/releases/](docs/releases/)。版本和下载源的对照笔记在 [docs/MC自动化开服技术实现方案与版本映射表.md](docs/MC自动化开服技术实现方案与版本映射表.md)。

## 许可证

程序本身是 [MIT License](LICENSE)。

界面内嵌的 Geist、Geist Mono、Noto Sans SC 三款字体使用 [SIL Open Font License 1.1](https://openfontlicense.org/)。打进程序的是子集，保留了字体内的版权与许可信息，许可原文在 [`internal/web/ui/fonts/`](internal/web/ui/fonts/)，生成方式见 [tools/fonts/README.md](tools/fonts/README.md)。

伙伴模组带上的第三方说明在 [companion/LICENSE-NOTICE.md](companion/LICENSE-NOTICE.md)。
