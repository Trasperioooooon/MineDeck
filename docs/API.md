# MineDeck 开放 API

MineDeck 的 GUI 与后端通过本地 HTTP API 通信，该 API 同时对外开放，可用任何语言编写扩展（这替代了传统"插件系统"）。

## 认证

- 本机：请求头 `X-Token: <token>`（token 在启动 URL 的 `?token=` 参数中，每次启动随机）
- 局域网模式：`POST /api/login {"password": "..."}` 换取会话 Cookie
- SSE 端点可用查询参数 `?token=` 传递
- **仅本机（loopback）端点**：`/api/pickdir`、`/api/openpath`、`/api/instances/{name}/opendir` 会调起系统对话框 / 资源管理器，`/api/javas/add` 会执行请求中给出的主机路径（`java -version` 探测），均只接受来自本机的请求；局域网会话调用返回 403
- **主机路径仅本机可指定**：局域网会话不能决定文件写到主机哪里——`POST /api/instances` 与 `POST /api/import/modpack` 的 `root` 会被忽略（实例一律建在默认存放根下）；`PUT /api/config` 只要带了 `serversDir` / `backupsDir`（含置空恢复默认）即整次返回 403、不做任何修改，不带这两个键的其余配置仍可远程修改
- **AI 设置的修改类接口仅本机**：`/api/ai/*` 里除 `GET /api/ai/settings` 外都只接受本机请求（含 Key 和接口地址）；局域网会话只能看，调用返回 403
- **免 token 端点**：`GET /bg/{name}` 提供程序旁 `bg/` 目录内的壁纸图片（供前端 CSS `url()` 引用，仅受 Host 校验约束）

## 端点总览

### 应用
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/app` | 版本、内存（`ramMb`/`availRamMb`）、实际监听端口 `port`、`serversRoot`/`backupsRoot`、`autoStart`（开机自启真值，查注册表）、`config`、本机 IP；`lanSet`（是否已设访问密码）、`cfKeySet`（是否已设 CurseForge API Key）、`cfKeyBroken`（已存的 Key 解不开，多半是换了电脑或 Windows 用户，需要重填） |
| PUT | `/api/config` | 局部更新配置（见下方「配置键」；仅传要改的键） |
| POST | `/api/pickdir` | 弹系统文件夹选择框，返回 `{path}`（取消为空）。**仅本机** |
| POST | `/api/openpath` | 在资源管理器打开 `{path}` 目录。**仅本机** |
| GET | `/api/bg` | 列出 `bg/` 目录内壁纸文件名 `{images:[...]}` |
| GET | `/bg/{name}` | 提供单张壁纸图片（免 token，`filepath.Base` 防穿越） |
| POST | `/api/shutdown` | 优雅退出（自动停服） |
| POST | `/api/update/check` · `/api/update/apply` | 检查/应用更新 |
| GET | `/api/cores` | 12 种核心元数据（id/name/tag/desc/kind） |
| GET | `/api/mcversions?core=&snapshots=` | 版本列表；每项 `{id, type, latest, releaseTime?}`，`releaseTime` 为 Mojang 清单的发布时间（ISO 8601），仅 Vanilla / NeoForge 等版本表取自官方清单的核心提供，其余核心省略 |
| GET | `/api/builds?core=&mc=` | 构建列表 |
| GET | `/api/javas` · POST `/api/javas/scan` · POST `/api/javas/add` | Java 管理（`add` **仅本机**） |

**配置键**（`PUT /api/config`，均为可选、只更新所传字段）：
`source`（auto/mirror/official）、`cfApiKey`（**只写**：用 Windows DPAPI 加密存进程序目录的 `secrets.json`，任何响应都不下发——`config` 里的 `cfApiKey` 永远为空；是否已设置看 `GET /api/app` 的 `cfKeySet`；传空串清除）、`webhookUrl`、`updateRepo`、`checkUpdateOnStart`、`serversDir`（实例存放根，空=默认；**仅本机**；改动时旧值并入 `roots`，旧根里的实例照常扫描）、`backupsDir`（备份根；**仅本机**）、`backupKeep`（每实例保留份数 1–1000）、`onboarded`（首启引导完成标记）、`tosAccepted`（用户协议与免责声明已同意标记）、`minimizeToTray`（关窗最小化到托盘）、`autoStart`（开机自启，直写 `HKCU\...\Run`）、`listenLan`、`lanPassword`（明文，服务端存为 bcrypt 哈希；任何响应都不下发该哈希，是否已设密码看 `GET /api/app` 的 `lanSet`）、`chromeColor`（界面标题栏底色，只收 `#rrggbb`，存为小写，其它写法整次返回 400；空串清除。原生窗口里的前端在外观变化时上报，下次启动时宿主把它设为 WebView2 的默认底色，避免冷启动白闪；没有值时按系统浅 / 深色取默认方案的标题栏色）。

### 实例
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/instances` | 实例列表；每项含 `name/core/kind/mc/build/javaMajor/xmxMb/xmsMb/port/status/motd/dir/createdAt/consoleEncoding/onlineCount/uptimeSec/maxPlayers/extraJvm` |
| POST | `/api/instances` | 创建（返回 taskId，轮询 `/api/tasks/{id}`）；body 可含 `difficulty/gamemode/root` 等（`root` 仅本机生效） |
| POST | `/api/import/modpack` | multipart 导入整合包（file + name/xmxMb/port/eula/onlineMode/allowFlight/root；`root` 仅本机生效） |
| POST | `/api/instances/{name}/start` \| `/stop` \| `/kill` | 进程控制 |
| POST | `/api/instances/{name}/command` | `{"cmd":"say hi"}` |
| POST | `/api/instances/{name}/opendir?sub=` | 在资源管理器打开实例目录或其白名单子目录（`sub` ∈ mods/plugins/world/logs/crash-reports），**仅本机** |
| GET | `/api/instances/{name}/console` | SSE 控制台流（先回放后实时） |
| GET/PUT | `/api/instances/{name}/properties` | server.properties 读写 |
| PUT | `/api/instances/{name}/settings` | `{xmxMb, xmsMb, consoleEncoding, extraJvm}`（`extraJvm` 为字符串数组，一行一参，传入即整体替换；MC 1.18.1 以下实例的 log4shell 缓解参数始终保留，缺失时自动补回） |
| GET/PUT | `/api/instances/{name}/policies` | 崩溃重启 + 定时任务 |
| GET/POST | `/api/instances/{name}/backups`；POST `.../backups/restore`；DELETE `.../backups?file=` | 备份（保留份数由全局 `backupKeep` 控制，跨备份根合计）。新备份写当前备份根；列表 / 还原 / 删除也查内置根与改目录前用过的旧根（config.json `backupRoots`），同名文件视为同一份；`file` 只取文件名，须为 `.zip` |
| GET/POST | `/api/instances/{name}/players` | 玩家名单与操作 `{action, player}` |
| GET | `/api/instances/{name}/resources/search?q=` | Modrinth 兼容资源搜索 |
| POST | `/api/instances/{name}/resources/install` | `{projectId}` 安装到 mods/plugins |
| DELETE | `/api/instances/{name}?files=1` | 删除实例 |

`GET /api/tasks/{id}` 快照除进度/日志外，还含 `warnings[]`（结构化告警，如 CurseForge 未解析模组清单 `{kind,title,note,items:[{name,url}]}`），供导入完成页醒目呈现。

### 穿透
| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST | `/api/tunnels` | 列表（每项含运行态 `lastError`——frpc 最近一条失败原因，成功后清空；`credBroken`——凭据解不开，多半是换了电脑或 Windows 用户，这条隧道不能启动，要重填凭据）/ 新增 |
| PUT/DELETE | `/api/tunnels/{id}` | 修改 / 删除 |
| POST | `/api/tunnels/{id}/start` \| `/stop` | 启停 frpc |
| GET | `/api/tunnels/{id}/console` | SSE frpc 日志 |

凭据 `credential` 只写不读：用 Windows DPAPI 加密后存进 `tunnels.json`（`"dpapi:…"`），任何响应都不下发；`PUT` 时留空表示不修改。旧版的明文凭据在启动时自动加密写回。自定义 frps 的 token 不再写进 frpc 的配置文件，而是经环境变量交给 frpc 进程（配置里只有 `{{ .Envs.… }}` 模板）。OpenFrp 和樱花的 frpc 仍然只能从命令行参数接收凭据。

### 音乐共享（第五阶段）

把主机这台电脑上一个程序（Win11）或整机（只能录整机时）的声音，经服务器上的 Simple Voice Chat（SVC）播给玩家（设计文档 §8）。只有 NeoForge 1.20.5 以上、Forge 1.20.1 以上（Mohist 也算，还没实测）的实例能用；服务器上要有 SVC 和 MineDeck 内置的桥接模组。开始广播、列音源、确认整机录音都**仅本机**（录的是主机的声音），局域网会话调用返回 403；局域网会话可以看状态、停止广播、改设置。这几个接口都经过操作层：开始、停止广播和设置类是「仅人工」，AI 永远调不了；看状态是读操作；装桥接模组、改 voice_host 是写操作，AI 发起的要人确认。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/instances/{name}/music` | 「音乐共享」页的全部内容，只读。字段：<br>• `supported`、`reason`、`note`：核心和版本支不支持、为什么。<br>• `serverStatus`：实例状态。<br>• `svc`：`{installed, file?, version?, loader?, disabled?}`，按 jar 里的元数据找模组 ID `voicechat`，不看文件名。<br>• `bridge`：`{installed, file?, variant?, current, others?, version, config?, loaded?}`。`variant` 是加载方式 `main`（默认）或 `javafml`（备用）；`others` 是别的版本，安装时会删掉；`config` 是实例 `config/minedeck/voicebridge.properties` 里的 `{enabled, port, exclude, selftest, bypassGroupIsolation}`，令牌不下发；`loaded` 只在服务器运行中才有：SVC 加载了它为 true，SVC 加载完插件却没有它为 false。<br>• `voice`：SVC 服务端配置 `{found, port, voiceHost, path}`，`port` 为 -1 表示和游戏同一个端口；`voicePort`：这次开服 SVC 实际监听的 UDP 端口（从控制台认出来的）。<br>• `remote`：远程好友的语音 `{tunnels, addr?, state, managed}`。`tunnels` 是绑定到这个实例的语音隧道；`state` 为 `none`（没有语音隧道）、`waiting`（隧道还没拿到公网地址）、`pending`（下次开服前写进 voice_host，或 SVC 还没生成配置）、`same`（voice_host 已是隧道地址）、`differ`（voice_host 是别的值，不自动覆盖）；`managed` 表示 voice_host 是 MineDeck 写的。<br>• `players`：`{known, voice, excluded, noVoice, incompatible}`，从这次开服以后的控制台认出谁连上了语音、谁在排除名单里、谁在线却没连上语音、谁的 SVC 版本不兼容（`{name, client, server}`）；`known` 为 false 表示中途才开始看控制台，可能漏人。<br>• `capability`：`{os, mode, warn, warning}`，这台电脑能怎么录：`mode` 为 `process`（只录一个程序）、`system`（只能录整机）或空（录不了）；`warn` 为 true 时开始前要确认 `warning` 这段提醒。<br>• `systemConfirmed`：这台电脑确认过整机录音的提醒。<br>• `settings`：`{exclude, volume, mode?, exe?}`，后两项是上次用的采集方式和程序。<br>• `broadcast`：正在给这个实例广播时才有，字段见下。`elsewhere`：正在给别的实例广播时，那个实例的名字。`last`：上一次广播怎么结束的 `{reason, at, error}`。<br>• `restart`：服务器运行中改的、要重启才生效的项（「桥接模组」「voice_host」）。<br>• `guide`：可以直接发给好友的说明。`local`：请求是否来自本机。 |
| POST | `/api/instances/{name}/music/start` | 开始广播：`{mode, pid?, exe?, device?}`。`mode` 为 `process` 时要给 `pid`（`exe` 只用来显示，下次默认选它）；为 `system` 时 `device` 可选（播放设备 ID，不填用上次选的，没选过用系统默认）。返回 `broadcast`。服务器要在运行、装了桥接模组（装好后重启过）和 SVC，否则返回 400。同一时间只能有一个广播，已经在广播时返回 409；整机录音还没确认过时也返回 409，先调 `/api/music/confirm-system`。**仅本机** |
| POST | `/api/instances/{name}/music/stop` | 停止广播：通知桥接模组收尾、停掉采集。没在给这个实例广播时返回 400 |
| PUT | `/api/instances/{name}/music/settings` | `{exclude?, volume?}`，只改传了的。`exclude` 是「我在游戏里的名字」：几个名字用逗号隔开，写进桥接模组的配置，2 秒内生效，这些人不再收到音乐（自己已经在听原声）；名字里不能有 `\ = # ! :`，每个最多 36 个字。`volume` 是音量 0–200（%），正在广播时立刻生效 |
| POST | `/api/instances/{name}/music/install` | 把内置的桥接模组装进实例的 mods：`{variant?}`，`main`（默认）或 `javafml`（备用加载方式）。删掉别的版本，写好它的配置（打开共享、端口、令牌）。返回 `{installed, file, variant, current, version, removed?, restart}`。没有 SVC 时返回 400（桥接模组离不开它，装了服务器起不来）；服务器运行中要替换已经装着的版本返回 409（文件被占用，先停服）；运行中新装的 `restart` 为 true |
| PUT | `/api/instances/{name}/music/voicehost` | 改 SVC 配置里的 `voice_host`：`{fromTunnel: true}` 用语音隧道的公网地址（以后隧道地址变了也跟着改），或 `{value}` 手动填（MineDeck 不会自动覆盖；空串清掉）。只改这一行，改前把原文件备份成 `.bak`。返回 `{before, after, changed, restart}`。SVC 的配置还没生成（没开过服）时返回 400 |
| GET | `/api/music/sources` | 这台电脑上正在用声音的程序 `sources: [{pid, exe, name?, playing, peak, devices?}]`（`peak` 是当前电平 0–1；游戏本身的 java 不列出来）、播放设备 `devices: [{id, name, default}]`，以及 `capability`、`systemConfirmed`、`device`（上次选的播放设备）、`error?`。**仅本机** |
| POST | `/api/music/confirm-system` | 记下「这台电脑确认过：整机录音会把游戏声音、语音聊天一起播出去」，每台电脑一次。**仅本机** |

`broadcast` 的字段：`{inst, phase, since, mode, source, volume, listeners, buffered, rttMs, sent, dropped, silent, modDropped, underruns, lastError?, busy?, warning?, input?}`。`phase` 为 `connecting`（连接中）、`live`（正在播）、`retrying`（断线重连中）；`listeners` 是桥接模组报的听众数，还没连上时为 -1；`busy` 表示模组那边已经有别的广播连着；`sent` / `dropped` / `silent` 是发给模组的、没连上或发不及丢掉的、静音没发的帧数，`modDropped` / `underruns` 是模组缓冲满了丢掉的帧数和缓冲取空的次数。

- **自动做的事**：开服前（手动、定时重启、崩溃重启都算）补好桥接模组的配置；语音隧道拿到公网地址时（`tunnel.up`）和开服前，把地址写进 voice_host——只在 voice_host 为空、或者还是 MineDeck 上次写的值时写，用户自己填的不覆盖。服务器停止、音乐软件退出、连不上桥接模组、退出 MineDeck 时广播自己停下，并记一条操作记录：`op` 为 `music.stop`，`src` 为 `schedule`，`actor` 为「自动停止」，`args` 里有 `reason`；出错停下的记为失败。
- **存放**：整机录音的确认、播放设备、每个实例的音量和上次的选择存在 `<程序目录>/data/music/settings.json`；排除名单、端口和令牌在实例的 `config/minedeck/voicebridge.properties`。
- **隧道的新字段**（`/api/tunnels`）：`purpose` 为 `game`（游戏，默认，老数据没有这一项也按游戏算）或 `voice`（SVC 的 UDP 隧道，公网地址会写进绑定实例的 voice_host）；自定义 frps 的 `proto` 为 `tcp`（默认）或 `udp`，樱花和 OpenFrp 的协议在服务商后台定，传了也会清掉。Webhook 和事件流里的 `tunnel.up` 多了隧道编号 `id`。

### AI（可选）

AI 是可选功能。没配置时（`enabled` 为 false）MineDeck 不连接任何 AI 服务，不起后台任务，也不下载价格数据。下表标了「联网」的三个接口才会对外连接，而且都要人在设置页里点了才发：取模型列表、测试连接、刷新价格。AI 的设置和 Key 只能由人来改：它们在操作层里是「仅人工」的操作，以后的 AI 功能看不到也调不了。修改类的接口都**仅本机**，局域网会话只能看。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ai/settings` | 返回当前设置和状态，不联网，局域网会话也能看。字段：<br>• `presets`：服务商预设。<br>• `provider`：服务商设置 `preset/protocol/baseUrl/model/maxOutput/effort/eager/fallback`，不含 Key。<br>• `keySet` / `keyBroken`：Key 是否已设置、是否解不开。<br>• `enabled`：是否已启用；`missing`：还差什么，如「填写 Key」。<br>• `quota`：每日额度。<br>• `today` / `month`：今天和本月的用量。<br>• `priceNeeded`：是否配置了要花钱的服务商。<br>• `prices`：价格数据的状态。<br>• `modelPrice`：当前模型的单价。<br>• `customPrices`：自填单价。<br>• `local`：请求是否来自本机。 |
| PUT | `/api/ai/provider` | 改服务商设置，只改传了的字段：<br>• `preset`：传空串表示停用 AI，同时清掉 Key。<br>• `protocol`：`openai` / `anthropic`，只有自定义时可选。<br>• `baseUrl`、`model`、`maxOutput`。<br>• `effort`：Claude 的思考深度 `low/medium/high/xhigh/max`；空串用模型默认。<br>• `key`：**只写**；空串清除。<br>换了服务商，或者接口地址换到了别的网站（http/https、域名、端口有一样不同），旧 Key 自动清掉，要在同一次请求里给新 Key，或者之后重填；同一个网站只改路径或协议，Key 照旧。返回内容同 `GET /api/ai/settings`。**仅本机** |
| POST | `/api/ai/models` | 取模型列表（**联网**），返回 `[{id, name?, maxOutput?}]`。<br>body `{preset, protocol?, baseUrl?, key?}` 可以是还没保存的服务商和地址。不给 `key` 时，只有服务商、协议和地址都和已保存的一样，才用已存的 Key。<br>服务商那边出错时返回 502。**仅本机** |
| POST | `/api/ai/test` | 测试连接（**联网**）：按已保存的设置发一个最小的流式请求，带一个测试用的工具。返回字段：<br>• `ok`、`latencyMs`（到收到第一个事件）、`totalMs`、`model`。<br>• `tools`：`yes` 能调用工具，`no` 服务商说不支持，`unknown` 模型这次没调用。<br>• `reply`、`usage`、`stop`。<br>• `cost`：估算的费用 `{amount, currency, source}`。`source` 是 `litellm`、`custom` 或 `local`（本机模型，不计费）；查不到价格时没有这一项。<br>• `eager`、`fallback`：Claude 才有，见下文。<br>连不上时仍返回 200，`ok` 为 false，`error` 写明原因。没启用时返回 400。花的 token 不计入每日额度。**仅本机** |
| PUT | `/api/ai/quota` | 每日额度：`{assistant, gamechat, companion}`，只改传了的。单位 token，输入、输出、缓存命中、缓存写入都算，0 表示不限。`companion` 是 AI 伙伴（第七阶段）：玩家在聊天里叫着伙伴的名字说话时记在它名下。**仅本机** |
| PUT | `/api/ai/prices` | 整体替换自填单价：`{prices: [{model, currency, input, output, cacheRead?, cacheWrite?}]}`。`currency` 是 `CNY` 或 `USD`，单价按每百万 token 填。最多 100 条，模型不能重复。**仅本机** |
| POST | `/api/ai/prices/refresh` | 马上重新下载公开价格（**联网**）。没配置 AI 或只配了本机模型时返回 400，下载失败返回 502。**仅本机** |

- **Key**：用 Windows DPAPI 加密，存进程序目录的 `secrets.json`，键名是 `ai.key`。任何响应都不返回 Key，操作记录里写成 `***`。`keyBroken` 为 true 表示已存的 Key 解不开，多半是换了电脑或 Windows 用户，需要重填。自定义预设可以不填 Key（有的中转和本机服务不要 Key）。
- **Claude 的两个开关**：`provider.eager` 和 `provider.fallback` 只读，表示流式时工具参数是否边生成边推（`eager_input_streaming`），以及是否开启服务端回退。官方地址默认都开。自定义地址走 Claude 协议时，两项默认都关，测试连接时试一下，能用才打开，结果存进设置。请求被服务商以「不认识的参数」拒绝时也会自动关掉重发。
- **用量**：`today` / `month` 按功能分开给，功能有 `assistant`、`gamechat`、`companion`。每项的字段是 `{requests, input, output, cacheRead, cacheWrite, reasoning, tokens, estimated?, unpriced?, cost?}`：
  - `input` 不含缓存命中和缓存写入；`reasoning` 已经算在 `output` 里。
  - `tokens` 是额度按的数：输入 + 输出 + 缓存命中 + 缓存写入。
  - `estimated` 是其中按字数估算用量的次数（回复中途断开，拿不到用量）。
  - `unpriced` 是查不到价格、只记了 token 的次数。
  - `cost` 是 `{币种: 金额}`，都是估算。人民币和美元分开合计，不做汇率换算。

  按本地时间的自然日和自然月统计。记录存在 `<程序目录>/data/ai/usage.json`，保留 400 天。某项功能当天的 `tokens` 达到额度后，这项功能暂停到第二天 0 点。
- **价格**：公开价格来自 LiteLLM 的 `model_prices_and_context_window.json`（MIT 许可）。下载时整份下载，依次试 ghfast.top、gh-proxy.com，最后直连 GitHub；下载的内容当作不可信数据严格校验，再缓存到 `<程序目录>/data/ai/prices.json`，7 天刷新一次。只有配置了要花钱的服务商才去取。
  - `prices` 的字段是 `{source, updated?, count, fetching?, error?}`。
  - `modelPrice` 的字段是 `{found, local?, source, currency, input, output, cacheRead?, cacheWrite?, tiered?, note?}`，单价按每百万 token。`source` 是 `litellm` 或 `custom`；`tiered` 表示按输入长度分档，显示的是最低一档；国内四家的 `note` 提示「按国际站美元价估算」。查不到价格时 `found` 为 false，只显示 token。
  - 自填单价优先于公开价格，也能补上查不到的模型。

### 操作记录
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ops/log?inst=&src=&op=&limit=` | 操作记录，新的在前；可按实例、来源、操作（前缀匹配，如 `instance.players.`）过滤；`limit` 默认 100、最多 1000。局域网会话也能看 |

除登录、两个 SSE 控制台流和壁纸外，上面的接口都经过程序内部的操作层（`internal/ops`）：先判断权限（调用来源 × 风险等级 × 是否仅本机），再按参数说明校验，最后执行。写操作、危险操作和仅人工的操作（读操作不记）每执行一次，就在程序目录的 `logs/ops/<年-月>.jsonl` 追加一行，按月一个文件，保留 12 个月、总共不超过 20 MB：

```json
{"t":"2026-11-02T20:15:03+08:00","op":"instance.props.set","src":"ui","actor":"本机",
 "inst":"生存服","args":{"pairs":[{"key":"max-players","value":"10"}]},"ok":true,"ms":12}
```

- `src` 是来源：`ui`（本机界面）、`api`（本机的其他程序）、`lan`（局域网设备，`actor` 是它的地址）、`schedule`（定时任务和崩溃自动重启，`actor` 写明是哪一种）；`assistant`（应用内 AI 助手）、`gamechat`（游戏内 AI，`actor` 是玩家名，见「游戏内 AI（第四阶段）」）、`companion`（留给以后的 AI 伙伴）。AI 发起的写操作要人确认后才执行，记录里带 `proposal`（提案编号）和 `approvedBy`（确认的人）。
- 失败的操作也记，`ok` 为 false，`err` 是错误信息。
- Key、密码、令牌、凭据、Webhook 地址，以及 server.properties 里像密码的项（如 `rcon.password`），在 `args` 里一律写成 `***`（清空时保留空串）。

### 应用内 AI 助手（第三阶段）

对话本身不改任何东西，所以会话接口不走操作层；AI 调用的工具都经过操作层：读操作直接执行，写操作和危险操作生成提案，人确认后才执行（见下一节）。局域网会话要主机开着「局域网可用 AI 助手」（默认开）才能用，而且只看得见、只能处理局域网开的会话；本机都看得见。看、删会话不要求 AI 配好（停用 AI 后也能清空历史）；开会话、发消息、生成发送预览要求 AI 配好，没配好返回 400。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/assistant` | 状态：`enabled`（AI 配好了）、`allowed`（这个请求能用助手）、`local`、`lan`（「局域网可用 AI 助手」开关）、`missing`、`preset`、`model`、`quota {used, limit}`（助手今天的用量和每日额度，单位 token，`limit` 为 0 表示不限） |
| GET | `/api/assistant/sessions` | 会话列表，最近动过的在前：`[{id, title, inst, updated, origin: local/lan, actor, busy?, waiting?}]` |
| POST | `/api/assistant/sessions` | 开新会话 `{inst}`（可以不关联实例），返回会话。系统提示和工具清单此时定下，之后不变 |
| DELETE | `/api/assistant/sessions` | 清空：本机清空全部，局域网只清空局域网开的。返回 `{deleted}` |
| GET / DELETE | `/api/assistant/sessions/{id}` | 取会话 / 删掉（正在进行的回复停掉，没处理的提案作废） |
| POST | `/api/assistant/sessions/{id}/messages` | 发消息 `{text, attach}`：在后台开始回复，立刻返回，结果经事件推送。<br>• `attach` 为 true 时附上之前预览过的排错资料（30 分钟内有效，过期返回 409）；只附资料时 `text` 可以空。<br>• 正在回复时返回 409，额度用完返回 429，消息超过 2 万字返回 400。<br>• 有确认卡片没处理就发新消息时，没处理的提案作废，结果一起交还给 AI |
| POST | `/api/assistant/sessions/{id}/context` | 生成发送预览（「让 AI 分析」）：按关联的实例打包排错资料（实例信息、本地规则的结论、本次启动以来精简过的控制台、本次启动之后的崩溃报告和 hs_err 摘要、相关模组，约 2 万 token 以内），脱敏后返回 `{lead, text, chars, tokens}`。`lead` 是默认的提问，`text` 是将要发送的全文。会话没关联实例时返回 400 |
| POST | `/api/assistant/sessions/{id}/stop` | 停止正在进行的回复（已经执行的工具不撤回） |
| POST | `/api/assistant/sessions/{id}/continue` | 上次的回复没完成（停止、出错、空回复），或者确认卡片都处理完了：接着回复。没有要继续的返回 409 |
| GET | `/api/assistant/sessions/{id}/events` | SSE（可用 `?token=`）：先推 `{"type":"session","session":…}`（完整的会话），之后是增量：<br>• `item`：新增或更新一条（按 `id` 替换）；<br>• `delta`：回复的文字片段 `{id, text}`；`tool_start`、`thinking`：正在调用工具、正在思考；<br>• `state`：`busy/waiting/needReply/stale/long/attach/title/quota`；<br>• `retry`：服务商出错，正在重试；`remove`：撤掉一条（没有内容的空回复）。<br>客户端跟不上时连接被断开，重连后重新拿一份完整的 |
| GET | `/api/assistant/sessions/{id}/sent?item=&tool=` | 实际发给 AI 的内容 `{text}`：用户的话（脱敏后、带上排错资料的），或者某次工具调用交还给 AI 的内容 |
| PUT | `/api/ai/assistant` | 「局域网可用 AI 助手」开关 `{lan}`，返回同 `GET /api/ai/settings`。**仅本机**，仅人工 |

- **会话**：`{id, title, inst, created, updated, origin, actor, model, preset, items[], busy, waiting, needReply, stale?, long?, attach?, quota}`。`waiting` 表示有确认卡片在等；`stale` 不为空表示 AI 设置改过（换了服务商、协议或地址，或者 Claude 换了模型），这个会话不能接着用；`long` 表示会话很长了，建议开新会话；`attach` 是打包好、等着发送的排错资料的字数。`GET /api/ai/settings` 多了 `lanAssistant` 字段。
- **会话里的一条**（`items[]`）：`{id, kind: user/assistant/notice, t, text, attach?, masked?, withdrawn?, tools?, usage?, cost?, model?, partial?, level?}`。
  - `masked`：用户的话里的隐私信息发送前被替换了。`withdrawn`：这句没得到回复，已从对话里撤回。
  - `partial`：回复没说完就停了（不在历史里）。`notice` 是提示（已停止、请求失败、空回复等），`level` 为 info / warn / error。
  - `tools[]`：`{id, name, op, title, level: R/W/D, args, status, error?, proposal?}`。`status` 是 running、done、error、skipped、pending（等确认）、confirmed、failed、rejected、expired、invalid、canceled（没处理就发了新消息）。`proposal` 是确认卡片要显示的内容，处理完后多了 `outcome`、`reason`、`by`、`error`。
- **发给 AI 之前一律脱敏**：程序目录、用户目录、路径里的 Windows 用户名、计算机名、局域网 IP（含本机各网卡的地址）换成 `<程序目录>`、`<用户目录>`、`<用户名>`、`<计算机名>`、`<局域网IP>`；Key、访问密码哈希、MineDeck 令牌、长得像 Key 的长串、配置里像密码的值写成 `***`；玩家名保留。工具结果外面加「以下是数据」的边界标记，超过 16 KB 截断并注明。新建实例时要人勾的 `eula` 和主机路径 `root` 不在 AI 能填的参数里。
- **会话保存**：存在 `<程序目录>/data/ai/sessions/<id>.json`，只留最近 20 个；重启后读回，接着聊时发给 AI 的历史逐字节不变。读回时还在等确认的提案按过期处理。
- 一轮最多调 8 次工具。空回复（Claude 空的 `end_turn`、OpenAI 兼容只给了思考内容）不进历史，提示换个说法再问。

### 提案：AI 提出的写操作（第三阶段）

AI 调用写操作或危险操作时，操作层不执行，而是生成提案，5 分钟内有效。提案里存着规范化后的参数和预览 `plan`：一句话说明 `summary`、改动清单 `changes [{what, before, after}]`、危险操作的警告 `warning`、文本文件的逐行对比 `diff`。人确认后按存下的参数原样执行；执行前先检查前置条件（预览时记下的文件指纹、实例状态等），变了就作废。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/ops/proposals?session=` | 还在等确认的提案，只列这个请求能处理的：`[{id, op, title, level, args, plan, consent?, caller, created, expires}]` |
| POST | `/api/ops/proposals/{id}/confirm` | 确认并执行，返回执行结果。body `{ack, consent}`：<br>• 危险操作要 `ack: true`（第二次确认），缺了返回 400；<br>• 带 `consent` 的提案（如新建实例的 EULA）要 `consent: true`，同意只能由人在确认时给出；<br>• 提案不存在或已处理返回 404，过期 410，前置条件变了 409（提案作废）。<br>只有人能确认；「仅本机」的操作只能在本机确认 |
| POST | `/api/ops/proposals/{id}/reject` | 拒绝 `{reason}`，理由可以不填，会交还给 AI |

- 局域网会话只能处理局域网开的助手会话里的提案，而且主机要开着「局域网可用 AI 助手」；本机会话的提案只能在本机处理（403）。
- 操作记录里，提案的每个节点各记一条，用 `event` 区分：`propose`（提出）、`reject`（拒绝，`approvedBy` 是拒绝的人，`reason` 是理由）、`void`（过期或作废，`reason` 写明原因）。确认后执行的那条没有 `event`，带 `proposal`（提案编号）和 `approvedBy`（确认的人）。

### 排错和修复（第三阶段）

AI 助手用的读写能力，网页也能直接调。人调用时，写操作直接执行，不出提案。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/instances/{name}/diag` | 本地规则排错，不联网：检查最近一次启动以来的控制台，返回 `{status, findings[], crashed, exit, lines}`。<br>每个 finding 是 `{rule, title, summary, details[], fixes[], evidence[], mods[], files[], severity: error/warn}`。`rule` 是 13 类之一：`missing-deps`、`client-only`、`wrong-loader`、`duplicate-mods`、`incompatible-mods`、`mixin`、`java-version`、`port-in-use`、`out-of-memory`、`watchdog`、`eula`、`client-mismatch`、`broken-config`；另有 `jvm-option`（JVM 参数不被支持，归在 Java 一类）和 `crash`（异常退出但没认出原因）。<br>界面的报错横幅据此显示，没配置 AI 也照样有 |
| GET | `/api/instances/{name}/console/tail?lines=&level=&q=&since=` | 控制台最近的输出（内存里最多 3000 行）。`lines` 默认 100；`level` 为 `warn` / `error` 时只看这一级及更严重的；`q` 是关键字；`since=start` 只看最近一次启动以来的 |
| GET | `/api/instances/{name}/logs?file=&lines=&q=` | 读 `logs/latest.log`（`file=debug` 时读 `debug.log`）末尾 `lines` 行，默认 200。给了 `q` 就改成搜索，最多 50 处，每处带前后 2 行。GBK 编码的日志自动转码 |
| GET | `/api/instances/{name}/crash-reports?file=&full=` | 不给 `file` 时列出 `crash-reports/` 里的崩溃报告和 `hs_err_pid*.log`：`{reports: [{name, kind: crash/jvm, size, modified, thisStart}]}`，新的在前。给了 `file` 返回摘要（开头、Description、第一个异常、加载器标出的可疑模组、Java 版本和内存）；`full=1` 返回原文的前 16 KB |
| GET | `/api/instances/{name}/mods?q=&detail=` | 解析 mods 目录（插件服是 plugins）里的 jar：`{dir, count, disabled, mods[]}`。给了 `q` 或 `detail=1` 时，每项带上加载器、依赖、mixin 配置等 |
| POST | `/api/instances/{name}/mods/toggle` | `{file, enable}`：把 jar 改名成 `.jar.disabled` 来禁用，改回来就是启用；不删文件，重启后生效 |
| GET | `/api/instances/{name}/config?path=&start=&lines=` | 读 `config/`、`defaultconfigs/`、`<世界>/serverconfig/`、`plugins/<插件>/` 下的配置文件，1 MB 以内，按行分页。`path` 为空时列出能读的目录，是目录时列出里面的文件 |
| PUT | `/api/instances/{name}/config` | 改配置文件 `{path, mode, edits?, content?}`：<br>• `edit`（默认）：`edits: [{find, replace}]` 逐处替换，原文要一字不差且只出现一次；<br>• `replace`：用 `content` 整体替换，文件不存在时新建；<br>• `reset`：备份后删掉，让模组重新生成默认配置；<br>• `undo`：恢复成上一次的备份。<br>改之前自动备份到 `<实例>/.minedeck/config-backups/`，每个文件留 20 份。读和比对按 UTF-8、`\n` 换行，写回时保持原文件的编码（UTF-8、带 BOM 的 UTF-8 或 GBK）和换行。内容里有脱敏占位符（`***`、`<用户名>` 等）时拒绝写入 |
| PUT | `/api/instances/{name}/java` | `{major, path?}`：给实例换 Java，下次启动生效。本机或 MineDeck 已经有这个版本就直接换，返回 `{java}`；没有就在后台下载便携版，返回 `{taskId}`（进度见 `/api/tasks/{id}`） |
| POST | `/api/instances/{name}/restart` | 重启：先保存世界并停止（最多等 40 秒），再启动。服务器没在运行时返回 400。危险操作 |
| GET | `/api/ports/{port}?proto=` | 端口占用：`{port, tcp?, udp?, instances?}`。每个协议是 `{free, owners?: [{pid, process, instance?, minedeck?}], note?}`；`instances` 是配置了这个端口的实例。`proto` 为 `tcp` / `udp`，空串两种都查 |

### 游戏内 AI（第四阶段）

玩家在游戏聊天里用「@AI 问题」提问，MineDeck 从控制台认出这句聊天，调 AI，再用 tellraw 回复（设计文档 §6）。服务器上不用装插件或模组；群组代理端没有游戏聊天，没有这个功能。设置按实例存，默认关。改设置、一键开启白名单都**仅本机**，局域网会话只能看设置和最近的问答、做识别测试，改的话返回 403。这几个接口都经过操作层，都是「仅人工」的操作，AI 看不到也调不了；看设置、识别测试、看最近的问答不记操作记录。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/instances/{name}/gameai` | 「游戏内 AI」标签的全部内容，只读，局域网会话也能看。字段：<br>• `supported`：为 false 表示群组代理端。<br>• `settings`：当前设置，字段见下面的 `PUT`。<br>• `defaults`：限流和额度的默认值 `{cooldown, perPlayer, perServer, budget}`。<br>• `templates`：10 个命令模板 `[{id, title, usage, limit, default}]`。`usage` 里的 `<自己>` 由程序换成提问的人；`default` 是默认开不开（`gamemode`、`give`、`difficulty`、`seed` 默认关）。<br>• `ai`：`{enabled, missing?, provider?, model?, quotaUsed, quotaLimit}`。`provider` 是隐私提示里服务商的说法；`quotaUsed`、`quotaLimit` 是「设置 → AI」里游戏内 AI 的每日额度，所有实例合计，`quotaLimit` 为 0 表示不限。<br>• `today`：这个实例今天的 `{count, tokens, players}`，`players` 是名字（小写）→ 次数。<br>• `players`：可以授权的人 `[{name, online, whitelist, op, seen, allowed}]`：在线的、白名单里的、管理员、最近来过的（`seen`）、已经授权的，同名不分大小写只列一次，在线的排前面。<br>• `whitelistOn`：server.properties 的 `white-list` 是否为 true。`running`：服务器在运行。`local`：请求是否来自本机。 |
| PUT | `/api/instances/{name}/gameai` | 改设置，只改传了的字段，返回内容同 `GET`。不合规时整次不改，返回 400。<br>• `enabled`：打开游戏内 AI。<br>• `prefix`：触发前缀，默认 `@AI`。1–16 个字，不能有空白、`/ ／ 、`、冒号、逗号和 `< > " \ §`。<br>• `aliases`：别名，整体替换，最多 8 个，规则同前缀。<br>• `allowed`：能让 AI 执行命令的玩家，整体替换，最多 200 个；名字是 1–16 个可打印的 ASCII 字符，不含空格和 `< > " \`，不以 `@` 开头。<br>• `templates`：`{模板编号: 开关}`，只改列出的；没有这个模板返回 400。<br>• `cooldown`（每人冷却，秒，0–3600）、`perPlayer`（每人每天几次）、`perServer`（全服每天几次）、`budget`（全服每天多少 token）：0 表示不限。<br>• `looseNames`：放宽名字规则；`anyThread`：不看线程名；`pattern`：自定义正则，要有命名分组 `name` 和 `msg`，对整行日志匹配，最多 500 个字，空串表示用内置的规则。<br>**仅本机** |
| POST | `/api/instances/{name}/gameai/test` | 识别测试：`{line, …}`。`line` 是一行服务器日志，必填，最多 4000 字；还可以带上 `PUT` 的任意字段（比如还没保存的 `prefix`、`aliases`、`pattern`），只用于这一次，不保存。返回 `{format, thread?, chat, player?, message?, ask?, reason?, online, authorized, result}`：<br>• `format` 是 `forge`、`vanilla`、`paper` 或 `custom`（自定义正则），空串表示认不出日志格式；`chat` 表示认出是一句聊天。<br>• `ask` 是解析出的 `{prefix, opts, private, noExec, question, action, unknown?, example?}`。`opts` 是 `私`、`问`、`额度`、`帮助`；`action` 为 `ask`（提问）、`help`、`quota`、`unknown`（未知选项）、`nospace`（选项后面没空格）、`misplaced`（选项写在了问题后面）。<br>• 没触发时 `reason` 写明原因；`online`、`authorized` 是这个人现在在不在线、能不能让 AI 执行命令；`result` 是一句话说明会怎么处理。<br>不发任何东西，局域网会话也能用 |
| GET | `/api/instances/{name}/gameai/log?limit=` | 最近的问答和执行记录，新的在前，`limit` 1–100，默认 50。每条是 `{t, player, kind, opts?, question?, reply?, private?, exec?, commands?, usage?, cost?, cut?, err?, note?}`：<br>• `kind`：`ask`（回答了）、`failed`（没答上，`err` 是详情）、`help`、`quota`、`unknown`、`nospace`、`misplaced`、`cooldown`、`limit`（次数或额度用完）、`busy`（排队满了）、`silenced`（刷屏，开始静默）、`disabled`（主机没配置 AI）。<br>• `reply` 是完整的回答或系统提示（游戏里太长截掉了时 `cut` 为 true）；`private` 表示只发给了提问的人；`exec` 表示这次给了 AI 执行命令的工具。<br>• `commands`：`[{command, run?, ok, reason?, output?}]`。`command` 是模型给的原文，`run` 是实际执行的，`reason` 是没执行的原因，`output` 是执行后 1 秒内的控制台输出。<br>• `usage`、`cost` 的写法同应用内助手。<br>局域网会话也能看 |
| POST | `/api/instances/{name}/gameai/whitelist` | 一键开启白名单：`{players}`。先把这些人加进白名单，已经在里面的跳过；名字要是 1–16 位字母、数字、下划线，否则返回 400。再开启白名单：<br>• 服务器运行中：发 `whitelist add <名字>`，把 server.properties 的 `white-list` 改成 true，再发 `whitelist on`；<br>• 服务器停着：直接改 `whitelist.json` 和 server.properties。<br>返回内容同 `GET`。**仅本机** |

- **怎么认聊天**：只看打开了游戏内 AI 的实例。认三种日志格式：Forge / NeoForge、原版 / Fabric / Quilt、Paper 系。聊天要由服务器主线程或异步聊天线程打出，Forge 格式还要是 `minecraft/MinecraftServer` 或 `minecraft/DedicatedServer` 记录的；`anyThread` 打开后不看线程名。说话的人要在线，名字默认只认 1–16 位字母、数字、下划线；`looseNames` 打开后放宽成 1–16 个可打印的 ASCII 字符，以 `@` 开头、含 `"`、`\` 或中文的名字照样不认。MineDeck 自己写进控制台的行（`> ` 和 `[MineDeck]` 开头）不看。
- **提问选项**：写在前缀之后、问题之前。`/私`（`/p`）只回提问的人；`/问`（`/ask`）这一次不给 AI 执行命令的工具；`/额度`（`/quota`）和 `/帮助`（`/h`、`/help`）不调用 AI。标记也认全角的 `／` 和顿号 `、`，英文别名不分大小写，前缀也认全角的 `＠`。
- **回复**：`tellraw @a {"text":"[AI→名字] ","color":"aqua","extra":[{"text":"回答","color":"white"}]}`。用了 `/私` 的回答和所有系统提示，目标是提问的人，开头是 `[AI] `；系统提示的正文是灰色。文本里的非 ASCII 字符一律写成 `\uXXXX`，整条命令只有 ASCII，不受服务器读标准输入的编码影响。每条最多 180 个字，最多 3 条，更长的截掉，提示到 MineDeck 面板看完整的回答。MineDeck 的控制台里另写一行看得懂的回显，比如 `[MineDeck] 游戏内 AI 回复（全服可见）：[AI→Steve] …`。每次开服后受理第一个提问之前，先向全服发一次「提问会发给哪家服务商」。
- **执行命令**：只有 `allowed` 里的人、而且没带 `/问` 时，请求里才有 `run_command` 工具。每条命令执行前，按最新的设置再查一次授权，再按打开了的命令模板和写死的禁止清单校验：整条只能是 ASCII，不许用目标选择器，「自己」由程序换成提问的人。通过了才写进服务器的标准输入，控制台回显 `[MineDeck] 游戏内 AI 替 名字 执行：/命令`。一个问题最多执行 3 条命令、调用 6 次工具、请求 4 次 AI，2 分钟没答完按超时处理。AI 能读的只有 `game.info`：核心、Minecraft 版本、加载器版本、模组和插件的数量、在线玩家、人数上限、服务器的本地时间。它是操作层里唯一 `InGame` 的读操作；游戏内调用别的操作一律 403，写操作也不生成提案。
- **限流和额度**：每人冷却、每人每天的次数、全服每天的次数、全服每天的 token（`budget`）这四项按实例算；另有「设置 → AI」里游戏内 AI 的每日额度（`PUT /api/ai/quota` 的 `gamechat`），所有实例合计。受理一个提问就算一次，并开始冷却，没答上也算。不调用 AI 的回复不算次数，但算进防刷屏：同一个人 1 分钟内触发超过 5 次，静默 10 分钟。同一实例同一时间只回答 1 个问题，最多排队 3 个。按本地日期计，过了 0 点清零。
- **操作记录**：来源 `gamechat`，`actor` 是玩家名。每个问题一条 `gameai.ask`，`args` 是 `{chars, opts, exec, result, usage, commands}`：问题的字数、用了哪些选项、给没给执行工具、结果（同 `kind`）、这次用的 token、命令的条数，不记问题原文。AI 要执行的每条命令一条 `gameai.command`，`args` 是 `{command, run}`；没执行的也记，记为失败，`err` 是原因。不调用 AI 的回复不写操作记录，只进最近的问答。
- **存放**：设置在实例目录的 `gameai.json`。当天的次数和 token、最近来过的名字、最近 100 条问答在同一目录的 `gameai-state.json`，没打开游戏内 AI 时不写。这两个文件跟着实例走，删实例时一起删掉。

### AI 伙伴（第七阶段）

伙伴是服务器里一个像玩家一样的角色（不占玩家名额，不在 Tab 列表里）：MineDeck 内置的伙伴模组加上 PlayerEngine 做身体，大脑在 MineDeck（设计文档 §7.2）。MineDeck 和伙伴模组之间的本机连接是协议 v1（`companion/README.md` 的「协议 v1」）。只有清单里有 jar 的加载器和 MC 版本能用，现在是 NeoForge 1.21.1 和 Forge 1.20.1。这几个接口都经过操作层：看状态是读操作；装模组、改设置、刷出、移除、全部停下是写操作，人在界面上点了就做，AI 助手只能提出、人确认后才做；游戏内（游戏内 AI、伙伴自己）一个都调不了。都不是「仅本机」，局域网会话也能用（打开 mods 目录除外，那是另一个接口）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/instances/{name}/companion` | 「伙伴」标签的全部内容，只读，不联网。字段：<br>• `inst`、`serverStatus`、`online`（在线的玩家）。<br>• `install`：`{supported, reason?, loader, mc, loaderWarn?, java, mods, ready, needed, download}`。`reason` 是不支持的原因（按清单列出支持的组合）；`loaderWarn` 是加载器版本不在伙伴模组要求的范围里；`mods` 三项依次是伙伴模组和两个依赖，每项 `{id, name, kind, state, version?, want, range?, file, size, files, license, page?}`：`kind` 为 `companion`（内置）或 `dependency`（要从 Modrinth 下载）；`state` 为 `ok`（装着要的版本：伙伴模组和内置的逐字节相同，依赖是实测过的版本）、`inrange`（依赖的版本在范围内，但不是实测过的，不自动换）、`outdated`（伙伴模组是别的版本，或依赖的版本不在范围内）、`missing`；`want` 是要装的版本，`range` 是依赖的版本范围；`files` 是 mods 里相关的文件 `[{file, version?, disabled?, keep}]`，`keep` 为 false 的安装时会删掉；`page` 是依赖在 Modrinth 上实测版本的网页。`ready`：三个都能用；`needed`：有要装或要换的；`download`：装的话要下载多少字节。<br>• `client`：好友的客户端要装什么，按清单来 `[{id, name, version, file?, required, page?, license?}]`：伙伴模组看清单的 `client`，依赖看 `side`（`both` 才要装）。伙伴模组没有 `page`（不在 Modrinth 上，从服务器的 mods 里拷）。<br>• `settings`：配置文件 `config/minedeck/companion.properties` 里的 `{exists, enabled, port, maxCompanions, attackDeny, attackDenyDefault, zones, problems?}`（令牌不下发），加上 `applied`（模组实际在用的，连着时才有，同协议里的 `config`）、`pending`（文件改了、模组还没读到）、`defaultDeny`、`maxLimit`（16）、`maxZones`（64）。`zones` 是 `[{name, dim, min, max}]`，两个角照配置文件里写的顺序。<br>• `link`：和伙伴模组的连接 `{state, since?, addr?, error?, busy?, mod?, playerEngine?, session?, protocol?, ops?, features?, rttMs?}`。`state` 为 `off`（没打开）、`waiting`（服务器没开、还没开好，或者正在关：关服时模组先发 END「服务器正在关闭」，MineDeck 不再重连，等下次开服）、`connecting`、`live`、`retrying`（断开了，正在重连）、`failed`（协议版本对不上这类重试也没用的错误，等关服或改设置）；`busy` 表示别的程序占着控制连接。<br>• `companions`：现有的伙伴，格式同协议的伙伴对象（不带背包）；不认识的 `state` 按 `busy`、不认识的 `result` 按 `failed` 给。<br>• `events`：这次开服以来最近的事（最多 100 条，新的在前）`[{t, event, companion?, name?, level, text}]`，`event` 是模组的事件名，另有 `connected`、`disconnected`；`level` 为 `info` 或 `warn`；`text` 是中文的一句话。<br>• `restart`：服务器运行中装了模组，重启后生效。<br>• `ai`：`{enabled, missing?, quotaUsed, quotaLimit}`，后两项是「设置 → AI」里 AI 伙伴今天的用量和每日额度。<br>• `gameai`：`{enabled, prefix, allowed}`，实例的游戏内 AI 开没开（玩家要靠它在聊天里指挥伙伴）、触发前缀、能指挥伙伴的玩家。`local`：请求是否来自本机。 |
| POST | `/api/instances/{name}/companion/install` | 装伙伴模组和两个依赖（**联网**，只有这个接口会访问 Modrinth）。伙伴模组从 exe 里写出；PlayerEngine、Architectury API 先下清单里实测过的版本（`cdn.modrinth.com`），核对大小和 sha512；只有它回 404 / 410 时，才查 `api.modrinth.com` 找 release、`version_number` 匹配清单规则、发布最晚的那个，核对 Modrinth 给的 sha512，再确认 jar 里 mods.toml 的版本在范围内。已经装着、版本在范围内的不动。每装好一个，马上删掉它的旧版本（停用的 `.jar.disabled` 不动）。返回 `{installed, removed, fallback?, restart, install}`：`fallback` 是换成了别的版本的说明。不支持的实例返回 400；服务器运行中要替换旧版本返回 409（文件被占用，先停服）；下载失败、校验对不上返回 502 |
| PUT | `/api/instances/{name}/companion/settings` | 改设置，只改传了的字段，返回内容同 `GET`。<br>• `enabled`：打开时写好 `enabled=true`、端口（从 24480 起选一个空着、别的实例的伙伴模组和音乐桥接都没用的）和令牌（32 位十六进制），服务器开着就去连；伙伴模组没装时返回 400。关掉时先给模组发 END 再改 `enabled=false`（伙伴还在游戏里，只是指挥不了）。<br>• `maxCompanions`：1–16。<br>• `attackDeny`：`attack` 不许打的生物 ID，整体替换，原版的可以省 `minecraft:`，空数组表示没有额外限制；`denyDefault: true` 恢复默认（删掉这一行）。<br>• `zones`：禁挖区域，整体替换 `[{name, dim, min: [x, y, z], max: [x, y, z]}]`：名字 1–32 个字母、数字、`_`、`-`，不能重名；维度写完整的 ID；x、z 在 ±30000000 以内，y 在 -2048–2048；最多 64 个。<br>只改配置文件里相关的行，注释和别的键原样保留，先写临时文件再改名；模组约 2 秒内读到，不用重启。不合规时整次不改，返回 400 |
| POST | `/api/instances/{name}/companion/spawn` | 刷出伙伴：`{name, owner?, near?}`。`name` 1–16 个字，不能有空白、`@`、选项标记（`/ ／ 、`）、冒号、逗号和 `< > " \ §`（玩家要在聊天里用「@名字」叫它），不能和游戏内 AI 以 `@` 开头的前缀、别名重名；`owner` 是主人的游戏名；`near` 是出生在这个在线玩家旁边，不填在主人旁边（主人不在线就在世界出生点）。返回伙伴对象。没连着伙伴模组返回 409；模组拒绝（重名、到上限、玩家不在线……）返回 400，`error` 是模组的说明 |
| POST | `/api/instances/{name}/companion/despawn` | 移除伙伴：`{companion}`（编号）或 `{all: true}`；`drop: true` 时背包里的东西掉在原地，否则一起消失。返回 `{removed}`（编号）。没连着返回 409 |
| POST | `/api/instances/{name}/companion/stop` | 停下伙伴的任务：`{companion}` 或 `{all: true}`。返回 `{stopped}`（任务编号）。没连着返回 409 |

- **自动连接**：实例开服（手动、定时重启、崩溃重启）而且打开了伙伴时，等服务器开好（`Done` 之后模组才打开端口）再连。开服前补好配置（端口和别的实例撞了就换一个，令牌坏了重新生成）。关服、崩溃时断开（给模组发 END，原因「服务器已停止」），清掉这次开服的状态；退出 MineDeck 时同样发 END。重连照协议 v1：每次连接前重读配置；配置刚改过或服务器刚开好的 10 秒内，连不上、令牌不对、端口正在关闭隔 0.5 秒重试；刚断开就遇到「已经有一个控制连接」同样快速重试；之后放慢，服务器开着就一直试。模组选了 MineDeck 不支持的协议版本时不再重试（`link.state` 为 `failed`），提示更新。
- **玩家怎么指挥伙伴**：经游戏内 AI（见上一节）。实例打开了游戏内 AI 和伙伴、连着伙伴模组时：
  - 有执行权限、又没带 `/问` 的人提问，AI 多 8 个工具 `companion_follow`、`companion_collect`、`companion_goto`、`companion_attack`、`companion_give`、`companion_stop`、`companion_say`、`companion_status`（只列模组支持的）。没有权限的人请求里根本没有这些工具。每个动作执行前按最新的设置再查一次授权；参数只放行协议表里的那几个；「跟着我」「给我」不写玩家时补成提问的人；`say` 的文字去掉 `§`、换行和控制字符。一个问题最多让伙伴做 4 件事（查状态不算）。
  - 「@伙伴名 话」等同于对这个伙伴说的 `@AI 话`：回复以伙伴的名义发出（`[小方→Steve] `，用了 `/私` 时是 `[小方] `），用量记在 AI 伙伴名下（`/api/ai/quota` 的 `companion`），也受游戏内 AI 的冷却和次数限制。伙伴的名字和前缀撞了时前缀优先。识别测试也认伙伴的名字（多一个 `companion` 字段）。
  - 任务做完、没做够、失败或超时时，按模板私下告诉发起的玩家，开头是伙伴的名字，比如 `[小方] 采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 5 个，用时 10 秒。`；被停下、被新任务顶掉的不通知。不调用 AI、不算次数、不扣额度；他不在线时只记一笔。断开期间结束的任务，重连后从伙伴的 `lastResult` 认出来照样通知。
- **操作记录**：伙伴的每个动作一条 `companion.action`，`src` 为 `companion`，`actor` 是发起的玩家，`args` 是 `{op, args, companion?, task?}`（实际发给模组的参数）；模组拒绝的记为失败。叫着伙伴的名字问的问题，`gameai.ask` 的 `args` 多了 `companion`（名字）和 `actions`（动作的条数）。最近的问答（`/gameai/log`）里多了 `companion`、`actions: [{op, companion?, title, ok, task?, reason?}]`，`kind` 为 `task` 的是任务通知。
- **存放**：伙伴的设置在实例的 `config/minedeck/companion.properties`（模组的配置文件，令牌也在里面，不下发）；伙伴存在世界存档里；最近的事件只在内存里。

所有响应统一为 `{"ok": true, "data": ...}` 或 `{"ok": false, "error": "..."}`。

### MCP 接口（外部 AI 工具）

让同一台电脑上的其他 AI 工具（Claude Desktop、Cherry Studio、Cursor、VS Code 等）经 MCP 调用操作层，默认关闭。设计见 `docs/AI与音乐共享功能设计.md` §15。

**设置接口**（都是仅人工的操作；修改类的**仅本机**，局域网会话只能看，拿不到令牌）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/mcp` | 状态，不含令牌：`enabled`、`local`（请求是否来自本机）、`url`（`http://127.0.0.1:<端口>/mcp`）、`port`、`portChanged`（想用的端口被占用，这次换了一个：HTTP 方式的配置要跟着改）、`tokenSet`、`tokenBroken`（解不开，要重置）、`clients`（本次启动以来连过的 AI 工具，最近的在前：`{name, title?, version?, transport: http/stdio, protocol, first, last, calls}`）、`pending`（等确认的外部 AI 提案数）、`tools`（给外部 AI 的工具数）、`versions`（支持的协议版本） |
| PUT | `/api/mcp` | `{enabled}`：打开或关闭。第一次打开（或令牌解不开）时生成令牌，写运行时文件；关闭时删掉运行时文件，没处理的外部 AI 提案作废。返回同 GET。**仅本机** |
| POST | `/api/mcp/token` | 重置令牌：HTTP 方式的客户端要换新令牌，stdio 方式不用改。返回同 GET。**仅本机** |
| GET | `/api/mcp/config` | 复制配置用：`{url, token, exe, snippets: [{id, name, transport, where, text}]}`。`id` 有 `claude`（stdio）、`cherry`、`cursor`、`vscode`（HTTP）、`http`、`stdio`（通用）；`where` 写明粘贴到哪儿。没打开时返回 400。**仅本机**，每次调用记一条操作记录 |
| GET | `/api/mcp/events` | SSE（可用 `?token=`）：先推 `{"type":"pending","items":[…]}`，之后等确认的外部 AI 提案每次有变化（新提案、处理完、过期、关掉 MCP）再推一份完整的。每项是 `{id, op, title, level: W/D, args, plan, consent?, actor, created, expires}`。**仅本机** |

**MCP 端点**：`POST /mcp`（Streamable HTTP）。

- 不认网页的令牌和局域网会话，要带 `Authorization: Bearer <MCP 令牌>`；只收本机的连接；`Host` 要是 127.0.0.1、localhost 或 [::1]；带了 `Origin` 的要是本机的网页。依次检查，不过就回 JSON-RPC 错误（不带 id）：
  - 405：不是 POST（GET、DELETE 也是 405，不提供事件流、不用 DELETE 结束会话）；
  - 403、-31003：不是本机的连接，或 Host、Origin 不对（没开局域网访问时，Host 不对会先被 MineDeck 的通用检查拒绝，回纯文本的 403）；
  - 403、-31001：MCP 接口没打开；
  - 401、-31002：令牌不对或没带（带 `WWW-Authenticate: Bearer`），令牌解不开时提示重置。
- 协议：2026-07-28（无状态：每个请求的 `_meta` 带 `io.modelcontextprotocol/protocolVersion` 和 `clientCapabilities`，有 `server/discover`），同时认 initialize 握手的 2025-11-25、2025-06-18、2025-03-26、2024-11-05。
  - 2026-07-28 的请求要带和请求体一致的 `MCP-Protocol-Version`、`Mcp-Method`，tools/call 还要 `Mcp-Name`（不是 ASCII 的写成 `=?base64?…?=`）。对不上回 400、-32020；`_meta` 缺字段回 400、-32602；不支持的版本回 400、-32022（`data: {supported, requested}`）；未知方法和 2026-07-28 去掉了的方法（initialize、ping 等）回 404、-32601。
  - 旧版：`initialize` 谈版本（要的不认识就回 2025-11-25），回 `Mcp-Session-Id`；之后的请求带上它和 `MCP-Protocol-Version`。认不出的会话照常处理，不回 404。也收 2025-03-26 的批量请求。
- 方法：`server/discover`（2026-07-28）、`initialize`、`ping`（旧版）、`tools/list`、`tools/call`。能力只有 `tools`，`listChanged` 为 false。回复一律是 JSON（不开 SSE 流）。
- 工具：和应用内助手给 AI 的一样（仅人工的不给，`app.update.check`、`java.scan`、`ops.log`、`game.info` 也不给），工具名把点换成下划线；外加 `proposal_status`（`{id?, wait?}`：查提案的处理结果，不填 id 列出最近一小时的，`wait` 最多等 30 秒）。
  - 读操作直接执行，结果脱敏、加数据边界、超过 16 KB 截断。
  - 写操作和危险操作只生成提案（同「提案：AI 提出的写操作」一节），结果是文字说明：提案编号、要做什么、要用户在 MineDeck 的界面上确认。同时等确认的 MCP 提案最多 10 个。
  - 参数不对、执行失败按工具的错误（`isError: true`）交回；没有这个工具回 -32602。
- 外部 AI 的提案只能在本机确认或拒绝（`/api/ops/proposals/{id}/confirm|reject`，局域网会话 403，`GET /api/ops/proposals` 也不列给局域网）。操作记录里 `src` 是 `mcp`，`actor` 是客户端在 `clientInfo` 里报的名字（清理过、最长 40 个字，不知道就写「MCP 客户端」），确认后执行的那条带 `approvedBy`。

**stdio 桥**：`MineDeck.exe -mcp`。MCP 客户端把它当子进程启动，标准输入输出一行一条 JSON-RPC 消息，转给同一目录下运行着的 MineDeck 的 `/mcp`。

- 端口从 `<程序目录>/data/mcp/runtime.json` 读（`{v, pid, port, exe, version, started}`，打开着 MCP 时由运行着的 MineDeck 写，关掉或退出时删掉）。桥会核对里面的进程还在、是同一目录下的程序。
- 令牌由桥自己从 `secrets.json` 用 DPAPI 解出来，所以 stdio 的配置里不用填令牌。
- 桥转发时带 `X-MineDeck-Bridge`（版本号）和 `X-MineDeck-Client`（客户端在 initialize 里报的名字，URL 编码）：前者让「连过的 AI 工具」显示 stdio，后者在 MineDeck 重启、认不出旧会话时用来记名字。都是自报的，只用来显示和记录。
- MineDeck 没运行时回 -31000，令牌读不出来时回 -31002，MCP 没打开时转回 -31001。都带原来请求的 id，同时写到标准错误。

## Webhook 事件

在全局设置配置 URL 后，以下事件将以 JSON POST 推送（失败重试 3 次）：

```json
{ "type": "player.join", "time": "2026-07-12T03:00:00+08:00",
  "data": { "instance": "我的服务器", "player": "Steve" } }
```

事件类型：`instance.start` / `instance.stop` / `instance.crash`、`player.join` / `player.leave`、`backup.done`、`tunnel.up` / `tunnel.down`。
