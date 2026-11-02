# MineDeck 伙伴模组

> 第 7 阶段第二步（正式版，模组 0.2.1；0.2.1 改了什么见「验证记录」末尾）。「协议 v1」是 MineDeck 和模组之间的约定，MineDeck 一侧照它实现；内置进 MineDeck 的 jar 在 `internal/companion/jar/`（见「打进 MineDeck 的 jar」）。

PlayerEngine 做"身体"（寻路、挖掘采集、背包、跟随、打怪），MineDeck 做"大脑"：所有 LLM 调用都在 MineDeck 里，模组只认 MineDeck 通过本机连接发来的固定动作。模组不登录 Player2、不调用 `api.player2.game`，只把 PlayerEngine 1.4.x 当库用，不改它。

伙伴是服务端的一个"像玩家一样"的实体（`LivingEntity`，不是 `ServerPlayer`）：不占玩家位，不在 Tab 列表里。一个服务器上可以同时有好几个伙伴（默认最多 4 个），各有编号、名字和主人，随存档保存，重启后自动接管。

## 目录

```
companion/
  common/            两个版本共用、只用 java.* 的代码：协议、控制端口、配置文件、参数检查
  shared/            两个版本共用、用到 Minecraft 和 PlayerEngine 的代码：伙伴管理和指令、任务、打怪、禁挖区域
  neoforge-1.21.1/   NeoForge 1.21.1 版（Gradle + ModDevGradle 2.0.148）：实体、存档记录、进服检查、事件
  forge-1.20.1/      Forge 1.20.1 版（Gradle + ModDevGradle Legacy 2.0.148）
  testctl/           模拟 MineDeck 的测试发送端（Go，和 MineDeck 同一个 module，不引用现有代码）
  update-embedded.sh 重新构建两个 jar 并更新 internal/companion/jar/（--check 只检查）
  LICENSE-NOTICE.md  许可说明（会打进 jar 的 META-INF/）
```

`common/` 和 `shared/` 不是单独的工程：两个 Gradle 工程都把它们加进自己的源码目录（`build.gradle` 里的 `srcDir`），各编一遍。所以 `shared/` 里的代码要在 1.20.1 和 1.21.1 上都能编译；两个版本写法不一样的（实体、存档、事件、进服检查、`Compat`）放在各自的工程里。

## 依赖

编译时从 Modrinth 的 Maven（`https://api.modrinth.com/maven`）取，**不打进 jar，不入库，不分发**。

|  | NeoForge 1.21.1 版 | Forge 1.20.1 版 |
|---|---|---|
| 加载器 | NeoForge 21.1.x（测的 21.1.265） | Forge 47.x（测的 47.4.28） |
| PlayerEngine | `1.21.1-1.4.x`（测的是 Modrinth 版本 `1.21.1-1.4.0-NeoForge`） | `1.20.1-1.4.x`（测的是 `1.20.1-1.4.0-Forge`） |
| Architectury API | ≥ 13.0.8（测的 13.0.11） | ≥ 9.2.14（测的 9.2.14） |
| JDK | 21 | 17 |

PlayerEngine 和 Architectury API **只装在服务端**：`mods.toml` 里这两个依赖写的是 `side = "SERVER"`，客户端不要求（见「安装」）。

PlayerEngine 没有稳定的 API：1.1.x 到 1.4.0 之间 `Character` 的构造函数多了一个参数，按旧版编的模组在新版上会直接 `NoSuchMethodError`。所以 `mods.toml` 只放行测过的 1.4.x（`[1.21.1-1.4.0,1.21.1-1.5)`），换版本要重新编译、重新测。

## 构建

```bash
cd companion/neoforge-1.21.1 && ./gradlew build     # → build/libs/minedeck-companion-neoforge-1.21.1-0.2.1.jar
cd companion/forge-1.20.1    && ./gradlew build     # → build/libs/minedeck-companion-forge-1.20.1-0.2.1.jar（已转成 SRG 名）
./gradlew --stop                                    # 用完关掉 Gradle 守护进程
```

- `gradle.properties` 里把 Gradle 限制在 `-Xmx3g`；`disableRecompilation = true`，不反编译 Minecraft，第一次构建约 2–3 分钟（主要是下载），之后十几秒。
- 想把下载的东西放到别处，设 `GRADLE_USER_HOME`。
- 版本号在两个工程的 `gradle.properties`（`mod_version`），要一起改。
- Windows 上用 `gradlew.bat build`（还没在 Windows 上跑过）。

## 打进 MineDeck 的 jar

两个加载器的 jar 入库在 `internal/companion/jar/`（Go 包 `companionjar`），用 `go:embed` 打进 MineDeck.exe，同一目录的 `manifest.json` 描述它们。MineDeck 的其他代码只 import 这个包，不改它；jar 更新时只动这个目录。

- `companionjar.Load()` 读出清单并逐项检查（每个 jar 都在，大小、sha256 对得上）；`(*Manifest).Find(加载器, MC 版本)` 找 jar；`Jar.Bytes()` 取内容（再校验一次 sha256）；`RawManifest()` 是清单原文；`FilePrefix`（`minedeck-companion-`）是装进 mods 后所有版本的文件名前缀，用来找出要替换的旧版本。
- 清单（`schema` = 1）：`mod` 是本模组的 ID、版本、许可证、控制连接的协议版本；`jars` 每项是一个 jar：`loader`（`neoforge` / `forge`）、`minecraft`、`loaderVersions`（加载器版本要求）、`testedLoader`、`java`、`file`（内置文件名）、`installName`（装进 mods 的文件名，带版本号）、`size`、`sha256`、`client`（玩家的客户端要不要装本模组：`required`）、`dependencies`。
- `dependencies` 每项是一个要另外装的模组（PlayerEngine、Architectury API）：`modId`、`versionRange`（和 jar 里 mods.toml 写的一样）、`side`（`both`：服务端和客户端都要装；`server`：只有服务端要装。现在两个都是 `server`）、`license`，以及 `modrinth`：`projectId`、`slug`、查版本用的 `loaders` 和 `gameVersions`、`versionPattern`（匹配 Modrinth 的 `version_number`）、`policy` 和 `tested`（实测过的版本：版本 ID、`version_number`、mods.toml 里的版本、文件名、大小、sha1、sha512、下载地址）。
- 版本选择规则 `policy` 现在只有 `tested-first`：先装 `tested` 那个版本，下载后核对大小和 sha512；它下载不到时，才在 Modrinth 上按 `loaders`、`gameVersions` 找 `version_type` 为 release、`version_number` 匹配 `versionPattern` 的版本，取发布最晚的一个，下载后确认 mods.toml 里的版本在 `versionRange` 之内。PlayerEngine 没有稳定的 API，不要自动追新版本。

模组源码改了以后：

```bash
./companion/update-embedded.sh          # 重新构建两个 jar，复制进 internal/companion/jar/，改 manifest.json 的版本、文件名、大小、sha256
./companion/update-embedded.sh --check  # 只检查入库的 jar 和清单是不是最新的（逐字节比较），不一致时退出码 1
go test ./internal/companion/jar        # 清单和 jar 里的 mods.toml 对得上（版本、依赖范围、side）；客户端会加载的类
                                        # 不引用 PlayerEngine、Architectury；mixin 配置登记了；jar 里没有别人的文件
```

构建是可复现的：`build.gradle` 里固定了打包的时间戳和文件顺序，同样的源码、同样大版本的 JDK 构建出的 jar 逐字节相同（入库的 jar 是用 OpenJDK 21.0.12 和 17.0.20 构建的）。依赖的版本范围和 Modrinth 信息是手工维护的，脚本不改它们，改了要和两个工程的 mods.toml、gradle.properties 一起改。

## 安装

**服务端**放 3 个 jar：伙伴模组 + PlayerEngine + Architectury API（版本见「依赖」）。

**玩家的客户端只装伙伴模组**（和服务端同一个 jar），不用装 PlayerEngine 和 Architectury API。两个加载器都在容器里用开发环境的真客户端试过（见「验证记录」），玩家用启动器装的正式客户端还要在 Windows 上再试一次，见文末「真客户端自测」。

- 客户端要有伙伴模组：伙伴是模组新加的实体类型，客户端要认识它、要能画出来（原版玩家模型、默认皮肤）。
- 客户端不用 PlayerEngine：伙伴实体分成两层，客户端只创建只管外形的 `CompanionEntity`，挂着 PlayerEngine 的 `ServerCompanion` 只在服务端创建（见「实现要点」）。客户端会加载的类都不引用 PlayerEngine 和 Architectury，`go test ./internal/companion/jar` 检查入库 jar 里的这些类。
- PlayerEngine 注册了一个会同步给客户端的实体类型 `playerengine:custom_fishing_bobber`（钓鱼的浮标）。客户端没有 PlayerEngine 时，伙伴模组注册一个同名、不画出来的占位类型，进服时的注册表核对才对得上。PlayerEngine 别的都不用同步：它没注册物品、方块，网络通道都是可选的，命令只用原版的参数类型（读源码）。
- 客户端装了 PlayerEngine 也没关系（第一步原型的装法是 3 个都装）。
- 没装伙伴模组的客户端进不了服，会看到中文提示：要装哪个文件，PlayerEngine 和 Architectury API 不用装。

| 客户端 | 在哪一步被拒 |
|---|---|
| 原版 | NeoForge：配置阶段。Forge：进入游戏的那一刻（Forge 的登录阶段放原版客户端过去） |
| 装了 NeoForge、没装伙伴模组 | 配置阶段，抢在 NeoForge 同步注册表之前（`ConfigurationInitializationMixin`；mixin 没生效时退回 NeoForge 自己的英文提示「未知的注册表键」） |
| 装了 Forge、没装伙伴模组 | 登录阶段，Forge 核对模组清单的时候（`HandshakeHandlerMixin`；mixin 没生效时，客户端在同步注册表时自己断开，是 Forge 的英文提示） |

配置文件 `<服务端目录>/config/minedeck/companion.properties` 的全部键见「协议 v1」的「配置文件」。

## 用 testctl 试

```bash
go build -o testctl ./companion/testctl          # 在仓库根目录；Windows 加 GOOS=windows
CFG=<服务端目录>/config/minedeck/companion.properties
./testctl -config $CFG welcome                                              # 握手，打印 WELCOME（模组版本、配置、现有伙伴）
./testctl -config $CFG send '{"op":"spawn","name":"小木","owner":"Steve"}'  # 刷在主人旁边（主人不在线就在出生点），回复里有编号
./testctl -config $CFG send -wait -timeout 5m '{"op":"collect","companion":1,"item":"oak_log","count":3}'  # 等到 task_finished
./testctl -config $CFG send '{"op":"attack","companion":1,"mob":"zombie","count":2,"radius":12}'
./testctl -config $CFG send '{"op":"say","companion":1,"text":"你好"}'
./testctl -config $CFG send -expect forbidden '{"op":"attack","companion":1,"mob":"villager"}'  # 期望失败，错误码对上了退出码 0
./testctl -config $CFG send '{"op":"status","inventory":true}'              # 位置、血量、当前任务、背包
./testctl -config $CFG watch -for 60s                                       # 只看：事件（加 -v 还打印 STATUS）
./testctl -config $CFG repl                                                 # 标准输入一行一条 JSON 指令，打印收到的所有帧
./testctl -config $CFG demo -item oak_log -count 3                          # 刷出 → 采集 → 看背包 → 移除，一条命令做完
```

`send` 自动补上 `id`，打印收发的每一帧（`→ REQUEST`、`← RESPONSE`、`← EVENT`）；`-raw` 原样发出去（测不合格的指令）。刚改完配置时连不上会自己重试（`-patience`，默认 10 秒）。退出码：0 正常；1 本地错误；2 模组拒绝连接；3 连接中途断开；4 指令失败、带 `-wait` 时任务的结果不是 `done`、带 `-expect` 时错误码没对上，或者 demo 没做成；5 带 `-wait` 时等任务结束超时。

出生点附近没有树时，先在服务端控制台放一棵（以伙伴为中心）：

```
execute at @e[type=minedeck_companion:companion,limit=1] run fill ~3 ~-1 ~-3 ~9 ~-1 ~3 minecraft:grass_block
execute at @e[type=minedeck_companion:companion,limit=1] run fill ~3 ~ ~-3 ~9 ~10 ~3 minecraft:air
execute at @e[type=minedeck_companion:companion,limit=1] run place feature minecraft:oak ~6 ~ ~0
```

背包也能用原版命令查：`data get entity @e[type=minedeck_companion:companion,limit=1] Inventory`。好几个伙伴时用编号对应的 UUID（`status` 里有）代替选择器；服务端控制台用管道喂命令时，选择器里的中文名字可能会乱码。

## 协议 v1

MineDeck 和伙伴模组之间的本机连接，第 7 阶段第二步定稿。MineDeck 一侧照这一节实现；模组这边连接、帧、配置文件在 `common/src/main/java/minedeck/companion/net/`，指令和事件在 `shared/src/main/java/minedeck/companion/CompanionManager.java`，`testctl/` 是照这一节写的测试发送端。

做法照音乐桥接（`bridge/voicechat/README.md` 的协议第 1 版）：模组在本机监听，MineDeck 来连，帧格式相同。不同的是指令、回复、事件都是 UTF-8 的 JSON，而且同一个连接上可以管好几个伙伴。

### 连接

- 模组在 `127.0.0.1:<port>` 上监听，只接受本机连接；端口和令牌写在配置文件里（见下面「配置文件」）。服务器开好（`Done (…)` 之后）才打开端口，关服时先发 END 再关。
- **同一时间只接受一个控制连接**，第二个连接收到 ERROR 3。
- 连接断开、重连都不影响游戏里的伙伴和它们正在做的任务。断开期间的事件丢掉不补：重连后从 WELCOME 拿最新的状态，每个伙伴的 `lastResult` 是它最近一次结束的任务的结果。WELCOME 的 `session` 变了，说明服务器重启过（任务编号从 1 重新数）。
- WELCOME 和事件之间没有缝（0.2.1 起）：WELCOME 在服务器主线程上生成，连接也在同一个主线程任务里变成「就绪」，而事件都在主线程上发。所以握手时发生的每件事（接管、刷出、任务结束……）要么已经体现在 WELCOME 里，要么作为事件跟在 WELCOME 后面发过来。0.2.0 在网络线程上读每 tick 末尾才刷新的快照，握手落在一个 tick 中间时，这个 tick 里的变化两头都没有，要等下一个 STATUS。
- MineDeck 每次连接前都重读配置文件。刚改完配置时，模组最多要 2.2 秒才读到新内容，这期间连不上、ERROR 1、ERROR 5 都算「模组还没读到」，隔 0.5 秒重试，最多 10 秒。刚断开就重连遇到 ERROR 3 时同样隔 0.5 秒重试。

### 帧

整数都是小端。

| 偏移 | 长度 | 内容 |
|---|---|---|
| 0 | 1 | 类型 |
| 1 | 1 | 保留：发送方填 0，接收方忽略 |
| 2 | 2 | 内容长度 N（0–65535） |
| 4 | N | 内容 |

| 类型 | 名字 | 方向 | 内容 |
|---|---|---|---|
| `0x01` | HELLO | MineDeck → 模组 | 协议版本 u16（MineDeck 支持的最高版本）、令牌长度 u8、令牌、客户端名字（剩下的全部，UTF-8，只用来记日志） |
| `0x02` | WELCOME | 模组 → MineDeck | 协议版本 u16（这次连接用的版本），后面是 JSON |
| `0x03` | ERROR | 模组 → MineDeck | 错误码 u16、说明（UTF-8）。发完就断开，只在握手时用 |
| `0x10` | REQUEST | MineDeck → 模组 | JSON 指令 |
| `0x11` | RESPONSE | 模组 → MineDeck | JSON 回复，每条指令正好一条 |
| `0x12` | EVENT | 模组 → MineDeck | JSON 事件 |
| `0x20` | PING | MineDeck → 模组 | 任意内容，最多 64 字节 |
| `0x21` | PONG | 模组 → MineDeck | 原样带回 PING 的内容 |
| `0x30` | STATUS | 模组 → MineDeck | JSON，每 2 秒一次 |
| `0x40` | END | 双向 | 原因（UTF-8）。发完就断开 |

不认识的帧类型两边都忽略（模组只记一次日志），不断开。

### 握手和版本协商

```
MineDeck                                              模组
  ── 连接 127.0.0.1:port ─────────────────────────────►
  ── HELLO(最高版本, 令牌, "MineDeck 2.7.0") ─────────►   连上后 5 秒内要发
  ◄──────────────────────────── WELCOME(版本, JSON) ─   或者 ERROR(错误码, 说明)，然后断开
  ── REQUEST {"id":1,"op":"spawn",...} ──────────────►
  ◄─────────── EVENT spawned / RESPONSE {"id":1,...} ─   指令引起的事件先到，回复后到
  ── PING（每 2 秒）──────────────────────────────────►
  ◄───────────────────────────────────────────── PONG ─
  ◄────────────────────────────────── STATUS（每 2 秒）─
  ◄──────────────────────────── EVENT task_finished ─   任务做完、失败、被停下
  ── END("MineDeck 退出") ────────────────────────────►   然后断开
```

- **令牌**：16–255 字节的可见 ASCII 字符（MineDeck 生成 32 位十六进制就行），和配置文件里的 `token` 逐字节相同才算对。比较是常数时间的；不对时模组等 0.3 秒再回 ERROR 1。
- **版本**：HELLO 里写 MineDeck 支持的最高版本 v。模组支持 1 到 M（这一版 M = 1）：v ≥ 1 时用 min(v, M)，写在 WELCOME 开头；v = 0 回 ERROR 2。MineDeck 不支持模组选的版本时发 END 断开，提示用户更新模组或 MineDeck。只有不兼容的改动（帧格式、已有字段的含义变了）才升版本；加指令、加字段、加事件都不升，见「兼容规则」。
- **心跳**：MineDeck 每 2 秒发一次 PING。模组 10 秒没收到任何帧就发 END（「心跳超时」）断开。模组每 2 秒发一次 STATUS，MineDeck 10 秒没收到任何帧也应断开重连。

| ERROR 错误码 | 意思 |
|---|---|
| 1 | 令牌不对 |
| 2 | 协议版本不支持 |
| 3 | 已经有一个控制连接 |
| 4 | 协议错误（第一帧不是 HELLO、HELLO 格式不对、帧太长） |
| 5 | 控制端口正在关闭（配置里刚改成 `enabled=false`，或者服务器正在关闭）；服务器主线程 4 秒都没空生成 WELCOME（服务器卡住了，0.2.1 起），同样回 5，稍后再连 |

**WELCOME 的 JSON**：

```json
{
  "protocol": 1,
  "session": "3f9c0a1b",
  "mod": "0.2.1",
  "loader": "neoforge",
  "minecraft": "1.21.1",
  "playerEngine": "1.21.1-1.4.0",
  "ops": ["spawn", "despawn", "status", "collect", "follow", "goto", "attack", "give", "stop", "say"],
  "events": ["spawned", "adopted", "removed", "died", "task_started", "task_finished", "stuck", "config_applied"],
  "features": ["nodig", "adopt"],
  "limits": {"maxCount": 2304, "maxText": 256, "maxName": 32, "maxZones": 64},
  "config": {
    "maxCompanions": 4,
    "attackDeny": ["minecraft:villager", "minecraft:wandering_trader", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:allay"],
    "zones": [{"name": "base", "dim": "minecraft:overworld", "min": [-120, -64, -80], "max": [-40, 320, 10]}],
    "problems": []
  },
  "companions": []
}
```

- `session`：每次开服随机生成。
- `ops`、`events`、`features`：这个模组支持的指令、会发的事件、可选能力。v1 的 `features` 有 `nodig`（禁挖区域）和 `adopt`（重启后接管伙伴）。
- `config`：模组现在实际在用的配置。STATUS 不带它，配置变了发 `config_applied` 事件。
  - `problems` 是**字符串数组**，每条一句中文说明，列出配置文件里写错、没生效的条目；没有时是 `[]`。
  - `zones` 的 `min`、`max` 是**整理过的**：每个坐标轴上小的放 `min`、大的放 `max`（配置文件里两个角谁先谁后都行）。
- `companions`：现有的伙伴，格式见「伙伴对象」，不带背包。

### 指令和回复

REQUEST：`{"id": 1, "op": "collect", "companion": 1, ...}`。

- `id`：整数，由 MineDeck 定，RESPONSE 里原样带回。
- `op`：动作名，清单是固定的（下表），没有「破坏任意方块」「放置任意方块」「执行任意命令」这类动作。
- `companion`：伙伴的编号。不写时：只有一个伙伴就是它；一个都没有回 `no_companion`；有好几个回 `ambiguous`。MineDeck 最好总是写上。
- 指令按收到的顺序在服务器主线程上执行，回复也按这个顺序回。可以不等回复连着发好几条。服务器卡的时候回复会晚到，MineDeck 的超时设 15 秒左右。
- 不认识的字段一律忽略。

RESPONSE：成功 `{"id": 1, "ok": true, ...}`；失败 `{"id": 1, "ok": false, "code": "no_player", "error": "玩家不在线：Steve"}`，有的错误还带别的字段（比如 `not_enough` 带 `have`）。`error` 是给人看的中文说明，MineDeck 按 `code` 判断。REQUEST 不是 JSON 对象、缺 `id` 或 `op` 时回 `bad_json`，`id` 取不到就是 `null`。

| `op` | 参数（带 * 的必填） | 做什么 | 成功时回复里有 |
|---|---|---|---|
| `spawn` | `name`（1–32 个字，默认「伙伴」，不能和现有伙伴重名，不分大小写；含 `§`、换行或别的控制字符时回 `bad_args`）、`owner`（主人的游戏名，可以不在线）、`tag`（MineDeck 自己的标记，1–64 个 `A-Z a-z 0-9 _ . : -`）；位置三选一：`near`（在线玩家名，出生在他旁边、同一维度）、`x`+`z`（+`y`，+`dim`，默认主世界，`y` 不写就放在地面上）、都不写时在主人旁边（主人不在线就在世界出生点） | 刷出一个伙伴，分配编号 | `companion`（编号）、`info`（伙伴对象） |
| `despawn` | `companion` 或 `"all": true`；`drop`（默认 `false`：背包里的东西一起消失；`true`：掉在地上） | 移除伙伴 | `removed`（编号数组） |
| `status` | `companion`（不写就是全部）、`inventory`（默认 `false`） | 查状态 | `companions`（伙伴对象数组）、`config`（同 WELCOME） |
| `collect` | `item`*、`count`（1–2304，默认 1）、`timeout` | 采集或合成 N 个某物。PlayerEngine 的 `get N` 是「再弄 N 个」，背包里已有的不算 | `task` |
| `follow` | `player`（默认主人）、`timeout`（默认 0，一直跟） | 跟着某人走，直到被停下、被新任务替换、超时或那人下线 | `task` |
| `goto` | `x`*、`z`*、`y`（不写就只管水平位置）、`timeout` | 走到某处，只在伙伴所在的维度里走 | `task` |
| `attack` | `mob`*（生物种类 ID，如 `minecraft:zombie`，原版可省 `minecraft:`）、`count`（1–64）、`duration`（1–1800 秒）、`radius`（1–48 格，默认 16）、`timeout` | 打某种生物：只打以开始时伙伴的位置为中心、`radius` 以内的；打够 `count` 只、或者打满 `duration` 秒就结束（两个都写时先到的算；都不写就是 `count` = 1）。只给 `duration` 时是「守在这里，来一只打一只」 | `task` |
| `give` | `item`*（原版物品写不写 `minecraft:` 都行）、`count`（默认 1）、`player`（默认主人）、`fetch`（默认 `false`：背包里不够就回 `not_enough`；`true`：不够就先去弄）、`timeout` | 把背包里的东西交给玩家：走到旁边朝他扔过去，扔出去的东西只有这个玩家捡得起来。交出的少于 `count` 时结果是 `incomplete`，一个都没交出去是 `failed`，`detail` 写原因（玩家不在伙伴所在的维度、背包里不够也没弄到……） | `task` |
| `stop` | `companion` 或 `"all": true` | 停下当前任务（连 PlayerEngine 的闲置动作一起停） | `stopped`（被停下的任务编号数组） |
| `say` | `text`*（去掉 `§` 格式码和控制字符后 1–256 个字）、`to`（玩家名：只发给他一个人） | 以伙伴的名字在聊天栏说一句话 | — |

- 参数先由模组检查：物品名只能是小写字母、数字和 `_ . - : /`，生物种类同样，玩家名只能是 1–20 个字母、数字、`_`、`.`，数量、坐标都是整数（坐标 ±30000000 以内，`y` 在 -2048–2048），所以拼出来的 PlayerEngine 指令里不会混进空格或 `;`（PlayerEngine 用 `;` 串联多条指令），大模型没法借参数塞进清单外的动作。
- **`timeout`**（秒）：任务最多做多久，到了就停下，结果是 `timeout`。默认：`collect` 600，`goto`、`give` 300，`attack` 300（只给 `duration` 时是 `duration` + 30），`follow` 0。0 表示不限（`follow` 以外的任务最少 5 秒），最大 86400。
- **`attack` 永远不打**：玩家、MineDeck 的伙伴、有主人的动物（驯服的狼、猫、马……）、起了名字的生物（用过命名牌的）、拴着绳子的生物，以及配置里 `attack_deny` 列出的种类（默认是村民、流浪商人、铁傀儡、雪傀儡、悦灵）。指定的种类本身在这些里面时回 `forbidden`；同种类里个别不能打的（比如起了名字的僵尸）直接跳过。唯一的例外是自卫：`attack` 任务进行中，正在打这个伙伴的敌对生物（僵尸、骷髅这类，原版的 `Enemy`）不管是什么种类、起没起名字，伙伴都会还手；玩家和伙伴无论如何都不打。
- **名字和 `say` 的安全**：名字和说的话会以 `<名字> 文本` 的样子出现在聊天栏和日志里，MineDeck 管得住自己刷出的名字，管不住 testctl 之类别的程序发来的，所以模组自己把关：`name` 里有 `§`、换行或别的控制字符就回 `bad_args`；`text` 先去掉 `§` 格式码（`§` 连同它后面的一个字符）和控制字符（换行、制表符换成空格），再去掉首尾空白，剩下的为空就回 `bad_args`，不为空就照剩下的发。
- **`say`** 的格式是 `<名字> 文本`，名字是青色的，鼠标悬停显示「MineDeck AI 伙伴」；`to` 私聊时前面加灰色的「（悄悄话）」。这句话不经过原版的聊天系统，服务端控制台里不会出现 `<名字> 文本` 这种和玩家聊天一样的行（免得被当成玩家说的话），只有模组自己记的一行 `伙伴「名字」说：文本`。
- 坐标、血量这类浮点数保留 1 位小数。

**任务**：`collect`、`follow`、`goto`、`attack`、`give` 是任务。每个伙伴同一时间只有一个任务，给它新任务会把旧任务顶掉（旧任务的结果是 `superseded`）；物品名不认识这类 `rejected` 在顶掉旧任务之前就检查出来，旧任务照常做。开始时先发 `task_started` 事件，再回 RESPONSE（带 `task`）；结束时发 `task_finished`。任务对象：

```json
{"id": 7, "companion": 1, "op": "collect", "args": {"item": "oak_log", "count": 3}, "timeout": 600, "startedAt": 1793432400000, "command": "get oak_log 3"}
```

`id` 是任务编号（这次开服以来所有伙伴共用一个计数），`args` 是整理过的参数（补上了默认值），`startedAt` 是毫秒时间戳，`command` 是交给 PlayerEngine 的指令（只用来排错，格式不保证，`attack` 没有）。

**错误码**（RESPONSE 的 `code`）：

| `code` | 意思 |
|---|---|
| `bad_json` | REQUEST 不是 JSON 对象，或者缺 `id`、`op` |
| `unknown_op` | 不认识的 `op` |
| `bad_args` | 参数缺了、类型不对、超出范围 |
| `no_companion` | 没有这个编号的伙伴（或者一个伙伴都没有） |
| `ambiguous` | 没写 `companion`，而现在有不止一个伙伴 |
| `not_ready` | 伙伴刚刷出，PlayerEngine 还没准备好（第一个 tick 之前），稍后再试 |
| `limit` | 伙伴数量到上限了（带 `limit`） |
| `name_taken` | 已经有一个同名的伙伴 |
| `no_player` | 玩家不在线（或者不写 `player` 时伙伴没有主人、主人不在线） |
| `not_enough` | `give` 时背包里不够（带 `have`） |
| `forbidden` | 不许做：打玩家、打伙伴、打 `attack_deny` 里的生物 |
| `rejected` | PlayerEngine 不接受这条指令，比如物品不存在（`error` 里是它的原话） |
| `spawn_failed` | 伙伴没能加进世界 |
| `internal` | 模组内部出错（日志里有详情） |

### 事件

EVENT 的 JSON 都有 `event`（事件名），和伙伴有关的都有 `companion`（编号）。

| `event` | 什么时候 | 其他字段 |
|---|---|---|
| `spawned` | `spawn` 刷出了伙伴 | `info`（伙伴对象） |
| `adopted` | 存档里的伙伴被加载、接管了（服务器重启后；一般在 MineDeck 连上之前就接管完了，那时只体现在 WELCOME 里；正好在握手时接管的，要么在 WELCOME 里、要么紧跟着收到这个事件，见「连接」） | `info` |
| `task_started` | 任务开始 | `task` |
| `task_finished` | 任务结束 | `task`（任务对象，不是编号）、`result`、`detail`（中文说明，可能没有）、`seconds`（用时）、`pos`（结束时伙伴的位置）；`collect` 还有 `have`（背包里共有几个）、`gained`（这次多了几个）、`want`；`attack` 还有 `killed`、`want`（只给 `duration` 时是 `null`）；`give` 还有 `given`（扔给玩家的个数）、`have`（背包里还剩几个）、`want`（0.2.1 起） |
| `stuck` | 伙伴卡住了：任务进行中 30 秒几乎没动（`follow` 时离玩家 6 格以内不算，`attack` 时范围里没有目标不算），或者反复想拆、想放禁挖区域里的方块。因为禁挖区域改走不拆不放的路以后卡住的，`reason` 也是 `nodig` | `task`（这里是任务**编号**，没有任务时为 `null`）、`reason`（`no_progress` / `nodig`）、`detail`、`pos`。同一个伙伴同一种原因，动起来之前只报一次 |
| `died` | 伙伴死了（背包里的东西掉在原地） | `uuid`、`name`、`cause`（死亡信息）、`pos`；随后还有一个 `removed` |
| `removed` | 伙伴没了 | `uuid`、`name`、`reason`：`despawn`（MineDeck 移除的）、`died`、`discarded`（被命令或别的模组清掉了）、`lost`（重启后存档里找不到了） |
| `config_applied` | 配置文件改了、重新读进来了 | `config`（同 WELCOME） |

`task_finished` 的 `result`：

| `result` | 意思 |
|---|---|
| `done` | 做完了 |
| `incomplete` | 结束了但没做够：`collect` 多出来的少于 `want`，`attack` 打到的少于 `count`（范围里 15 秒没有目标就算结束），`give` 交出的少于 `count`（0.2.1 起；0.2.0 报的是 `done`） |
| `failed` | 失败：PlayerEngine 报错、找不到路、`follow` 的玩家下线了、伙伴换了维度、`give` 一个都没交出去（0.2.1 起；0.2.0 报的是 `done`） |
| `cancelled` | 被停下：`stop`、伙伴被移除或死了 |
| `superseded` | 被新任务顶掉了 |
| `timeout` | 超过了 `timeout` |

`pos`：`{"dim": "minecraft:overworld", "x": 1.5, "y": 64.0, "z": -3.5}`。

### 伙伴对象

```json
{
  "id": 1,
  "uuid": "4e427761-5412-458d-bd14-c95ec392229b",
  "name": "小木",
  "owner": "Steve",
  "ownerUuid": "8667ba71-b85a-4004-af54-457a9734eed7",
  "tag": "persona-1",
  "dim": "minecraft:overworld",
  "x": 32.5, "y": 64.0, "z": -383.5,
  "health": 20.0, "maxHealth": 20.0,
  "state": "busy",
  "task": {"id": 7, "companion": 1, "op": "collect", "args": {"item": "oak_log", "count": 3}, "timeout": 600, "startedAt": 1793432400000, "command": "get oak_log 3"},
  "lastResult": null,
  "engineTask": "<Mine oak_log> "
}
```

- `id`：编号，刷出时由模组分配（从 1 起，用现有伙伴没用的最小的数），存进存档，重启后不变。
- `owner`、`ownerUuid`、`tag`：没有时为 `null`。`ownerUuid` 只有主人刷出伙伴时在线（或者服务器认识这个名字）才有。主人只是 MineDeck 的标记，模组只用它做 `follow`、`give` 的默认玩家和 `spawn` 的默认位置；PlayerEngine 那边伙伴始终没有主人，不会去登录 Player2。
- `state`：`starting`（PlayerEngine 还没准备好）、`idle`、`busy`（有任务）。
- `lastResult`：最近一次结束的任务，和 `task_finished` 的字段一样（没有 `event`、`companion`），多一个 `at`（结束时的毫秒时间戳）；还没做过任务时为 `null`。其中 `task` 是任务对象，**一定有 `task.id`**：MineDeck 重连后靠它认出断线期间结束了的任务（再去通知发起的玩家）。
- `engineTask`：PlayerEngine 自己的当前任务，只用来排错，格式不保证。
- 背包只在 `status` 带 `"inventory": true` 时才有：`inventory`（`[{"slot": 0, "item": "minecraft:oak_log", "count": 3}]`，0–35 是主背包、36–39 是盔甲、40 是副手）、`selectedSlot`。

STATUS：`{"companions": [伙伴对象…]}`，不带背包。

### 兼容规则

1. 两边都忽略不认识的 JSON 字段、事件名和帧类型。
2. 加新指令、新事件、新的可选参数和字段都不升协议版本。MineDeck 用新指令、新能力之前先看 WELCOME 的 `ops`、`features`。
3. 模组会忽略它不认识的参数，所以以后新加的可选参数，不写时的行为必须和旧版一样（安全的那一边）。
4. `code`、`result`、`reason`、`state` 以后可能加新值。MineDeck 把不认识的 `code` 当成一般失败，不认识的 `result` 当成 `failed`，不认识的 `state` 当成 `busy`，不认识的 `reason` 照原样显示。
5. 已有字段的类型和含义不变：编号是整数，坐标、血量是数字，时间戳是毫秒数。要改就升协议版本。

### 配置文件

`<服务端目录>/config/minedeck/companion.properties`（和音乐桥接的 `voicebridge.properties` 同一层），UTF-8，Java properties 格式（`#` 开头是注释）。文件不存在时模组生成一份端口关闭的模板。MineDeck 开服前写好；服务器运行时也可以改，**每 2 秒读一次，和上次应用的内容逐字节比较**，变了就隔 0.15 秒再读一遍，两次一样才应用（不看修改时间：同一毫秒里连写两次会漏）。MineDeck 写的时候先写临时文件再改名。

| 键 | 默认 | 说明 |
|---|---|---|
| `enabled` | `false` | 打不打开控制端口。改成 `false` 时断开当前连接、关掉端口 |
| `port` | `24480` | 1024–65535。改了会断开当前连接，换端口重新监听；端口被占时每 10 秒重试一次 |
| `token` | 随机 32 位十六进制 | 16–255 个可见 ASCII 字符。改了只影响之后的新连接 |
| `max_companions` | `4` | 同时最多几个伙伴，1–16。调低了不会移除现有的，只是不能再刷 |
| `attack_deny` | `minecraft:villager,minecraft:wandering_trader,minecraft:iron_golem,minecraft:snow_golem,minecraft:allay` | `attack` 不许打的生物种类，逗号分隔；写成空的（`attack_deny=`）表示没有额外限制。玩家、伙伴、有主人的、起了名字的、拴着的生物不管这里怎么写都不打 |
| `nodig.<名字>` | 没有 | 禁挖区域，见下 |

`enabled`、`port`、`token` 写错（端口不是数字、令牌太短）时不开端口，原因写进日志。其余的键写错只是那一项不生效（用默认值或跳过那个区域），原因写进日志和 `config.problems`；不认识的键（多半是拼错了）也列进 `problems`。

**禁挖区域**：

```properties
nodig.base=minecraft:overworld -120 -64 -80 -40 320 10
nodig.farm=minecraft:overworld 200,60,200 260,90,260
```

- `<名字>`：1–32 个字母、数字、`_`、`-`。
- 值：维度 ID，然后是两个角的方块坐标 x y z（整数，包含两端，两个角谁先谁后都行），用空格或逗号隔开。维度写完整的 ID：`minecraft:overworld`、`minecraft:the_nether`、`minecraft:the_end`，模组的维度照写。
- 最多 64 个，改了马上生效（发 `config_applied`）。
- 效果：伙伴不拆、也不放区域里的方块，包括往区域里倒水倒岩浆、点火、锄地这类「对方块用物品」的动作；对区域里的方块，除了开关门、活板门、栅栏门，不做别的右键交互（按钮、拉杆、箱子……）。可以从区域里走过。`collect` 不会去拿区域里的方块；寻路要穿过区域、得拆区域里的方块时，伙伴改走不拆不放的路（绕过去），绕不过去就报 `stuck`（`reason` 是 `nodig`）。做法见「实现要点」。

## 实现要点

- **伙伴实体分两层**。`CompanionEntity`（服务端、客户端都有）继承 `LivingEntity`，只管外形：手上和身上的装备、玩家一样的属性、挥手动画，不引用 PlayerEngine。`ServerCompanion`（只在服务端创建）继承它，实现 PlayerEngine 的四个接口 `IAutomatone`（拿到内嵌的 Automatone/Baritone）、`IInventoryProvider`（玩家式背包）、`IInteractionManagerProvider`（挖方块、用物品）、`IHungerManagerProvider`（饥饿，PlayerEngine 1.4 默认关闭）。每个 tick 推进挖掘进度、背包物品、PlayerEngine 的 `serverTick()`（任务 + 寻路，它通过 `xxa`/`zza`/`jumping` 驱动移动），再由 `LivingEntity` 自己的 `aiStep → travel` 走起来；装备直接读写 PlayerEngine 的背包（和原版 `Player` 一样）；PlayerEngine 不管捡东西，伙伴自己捡起碰到的掉落物，但不捡玩家和伙伴扔的（`give` 扔出去的东西记着是谁扔的，只让接东西的玩家捡）。实体类型注册时给的工厂在服务端（PlayerEngine 在）建 `ServerCompanion`，在客户端建 `CompanionEntity`，两者同步的实体数据完全一样。参考的是 PlayerEngine 自己的源码（LGPL），没有参考 Player2NPC（没有许可证）。
- **PlayerEngine 的控制器在伙伴第一次 tick 时才创建**：在实体进世界之前创建的话，伙伴不会动，PlayerEngine 也不去加载它脚下的区块。所以刚刷出、第一个 tick 之前的指令回 `not_ready`。
- **不碰 Player2**：创建 `PlayerEngineController` 时主人（owner）传 `null`。PlayerEngine 1.4.0 的对话层 `ConversationManager.process` 跳过没有主人的伙伴；心跳只在主人有 Player2 令牌时才发。再把这个伙伴的对话队列 `setEnabled(false)`，双保险。玩家在旁边聊天时，PlayerEngine 仍然会把消息记进日志、放进伙伴的对话队列（最多 11 条），但不会处理，不会联网。
- **`/playerengine relog`**：这条命令（要 OP）会对 `PlayerEngineController.staticAPIServices` 里的每个伙伴发起 Player2 登录（先找本机的 Player2 程序，再走 `api.player2.game` 的设备授权）。这张表只给 PlayerEngine 自己的管理命令用，伙伴创建后马上从表里拿掉，这条命令就碰不到它。
- **只用清单里的指令**：`collect`、`follow`、`goto`、`give` 拼成 PlayerEngine 的 `get`、`follow`、`goto`、`give` 指令，参数都先检查过（见协议）；`attack` 是本模组自己的任务（`AttackTask`，里面用 PlayerEngine 的 `KillEntitiesTask`，目标由本模组挑）。PlayerEngine 1.4.0 还有 `agentic`（让 Player2 的模型规划）、`build_structure`（在 Player2 上搜蓝图，1.4.0 已停用）等会联网的指令，本模组不会发，参数检查保证拼不出它们。
- **换任务**：先把旧任务停下（`PlayerEngineController.stop()`）、记成 `superseded`，再发新指令。PlayerEngine 的每种指令只有一个对象，回调存在这个对象上；让它自己替换的话，同一种指令（`goto` 换 `goto`）会把旧任务的结束报到新任务头上：新任务马上「做完了」，真正结束时却不再报。物品名先用 PlayerEngine 自己的解析（`ItemList.parseRemainder`）检查，不认识就回 `rejected`，旧任务照常做。
- **打怪**：PlayerEngine 打一下之前要等攻击冷却满，而冷却只在 `Player.tick` 里涨，所以伙伴每 tick 自己涨；`LivingEntity` 的 `doHurtTarget` 什么都不做，伙伴照 `Mob` 的写法实现（武器伤害、附魔、击退、火焰附加，两个版本的 API 不一样）。PlayerEngine 自己的自动防御（`MobDefenseChain`）关掉了：它见到附近的敌对生物就去打（起了名字的也打），打不过就跑，优先级又比 MineDeck 交的任务高。不该打的生物在 `doHurtTarget` 里再把一道关，伙伴不管为什么出手都伤不到它们（自卫的例外见协议）。玩家打不伤伙伴（直接打、射箭都不行，`hurt` 里挡掉），免得 PlayerEngine「被玩家打了就还手」去打玩家。
- **禁挖区域**（`ZoneGuard`、`ZoneInteractionManager`）分三层。动作层是兜底：PlayerEngine 拆方块走的是 `level.removeBlock`，不发拆方块事件，领地保护模组拦不住，所以本模组换掉伙伴的「交互管理器」，在拆、放区域里的方块和对它们用物品（倒水、点火、锄地……）时直接拒绝。任务层：把区域加进 PlayerEngine 的「别拆」「别放」判断（`BotBehaviour.avoidBlockBreaking / avoidBlockPlacing`），采集选目标时就跳过区域里的方块；PlayerEngine 的行为状态是个栈，弹栈会把这两个判断丢掉，所以每秒和每次派任务前都补一遍。寻路层：PlayerEngine 1.4 的寻路没有按位置禁止的接口，所以每 5 tick 看一遍当前和下一段路径要拆、要放的方块，碰到区域里的，就把这个伙伴切到「只走路」（`allowBreak`、`allowPlace` 关掉，取消当前路径让它重新规划，绕过去），离开所有区域 8 格以外、过了 3 秒再恢复。
- **伙伴记录和接管**：存档的 `data/minedeck_companion.dat`（`CompanionRegistry`）记着每个伙伴的 UUID、编号、名字、最后的位置和它强制加载的区块；主人、标记存在实体自己的 NBT 里（`MineDeck` 标签）。开服时接管已经加载的伙伴，其余的等它所在的区块加载、伙伴开始 tick 时接管（伙伴脚下的区块是强制加载的，一般开服时就加载了）。开服 30 秒后还没出现的，临时加载它最后位置的区块找一次（15 秒的加载票），10 秒后还找不到就当它丢了：发 `removed`（`reason` 是 `lost`），放开它的区块，删掉记录。
- **强制加载的区块**：伙伴脚下的 2×2 个区块要强制加载，不然玩家走远以后伙伴就停住了。PlayerEngine 的 `ChunkLoadingTracker` 会做，但它记账有错（坐标正好在区块边上时记重，有时漏放、有时漏加），伙伴被移除时也不放开。所以本模组每秒自己核对一遍：伙伴用到的都强制加载，记录里有、现在谁都用不着的放开；伙伴移除、死了、换维度时放开。开服时就已经强制加载、又不在伙伴记录里的区块当成管理员 `/forceload` 的，不动。原版的 `setChunkForced` 还有个坑：每次调用都把「要保存」设成「这一次有没有改动」，重复加、重复放一次（PlayerEngine 的记账经常这样），之前的改动就不保存了，重启后伙伴脚下的区块不再强制加载（实测存档里一直没有 `data/chunks.dat`）。所以本模组每秒和关服时把各维度的强制加载记录标成要保存。
- **关服**：PlayerEngine 的方块扫描跑在普通（非守护）线程上，扫描时要主线程替它取区块。关服时正好在扫的话，主线程不再处理这些请求，那个线程就一直等，Java 进程退不出去（实测 Forge 1.20.1 遇到过）。所以开始关服时先让每个伙伴的扫描停下（`BlockScanner.reset()`），同时替它们处理排着的区块请求，等扫描线程都结束（最多 5 秒）。
- **进服检查**（`JoinCheck`）：本模组注册一个可选的网络通道 `minedeck_companion:hello`，客户端装了本模组就有。NeoForge：配置阶段的一个配置任务检查这个通道（原版客户端和装了 NeoForge 的客户端都走这里），mixin 把它插到注册表同步之前。Forge：登录阶段核对客户端的模组清单时检查（mixin 进 `HandshakeHandler`）；原版客户端不走这一步，进入游戏时（`PlayerLoggedInEvent`）再查。通道版本固定为 `1`：客户端部分只有实体外形，不同版本的伙伴模组之间互相兼容，不按模组版本卡人。
- **配置热重读**（`Housekeeper`）：每 2 秒读一次配置文件，和上次应用的内容逐字节比较（见协议「配置文件」）。端口、令牌、开关的变化交给控制端口；关服时先停定时检查再关端口，不会刚关上又被它打开。
- **线程**：网络线程只收发；指令用 `server.execute` 交给服务器主线程执行；STATUS 读主线程每秒更新一次的快照；WELCOME 在主线程上生成（先刷新快照），连接在同一个主线程任务里置成就绪，网络线程最多等 4 秒（见「连接」）。PlayerEngine 的回调都排到下一个 tick 再处理，不在它的调用栈里改任务。
- **空闲时在水里浮上来**（`IdleFloat`，0.2.1 起）：PlayerEngine 1.4 防淹水靠 `WorldSurvivalChain`（在水里、气不够、没在寻路时按住跳），可它只在 `TaskRunner` 启用时才跑；`PlayerEngineController.stop()`（MineDeck 的 `stop`、换任务都会调）停用了 `TaskRunner`，刚建好的控制器在第一次接到任务之前也没启用，这时站在水底的伙伴没人管，淹死（读源码确认，两半联调时实测）。所以 `TaskRunner` 没在跑、也没在寻路时，伙伴照原版生物的 `FloatGoal`：水深过了 `getFluidJumpThreshold`、或者在岩浆里，就按住跳，浮上来以后松开（它正按着跳时不松）。只动「跳」，不走、不转身，不改 PlayerEngine 的状态，`stop` 的意思不变。
- **`give` 的物品名和结果**（0.2.1 起）：交给 PlayerEngine 时原版物品去掉 `minecraft:`（PlayerEngine 1.4 的 `GiveCommand` 只认目录名和 `ItemHelper.stripItemName`：原版物品是 `oak_log`，模组物品是 `create:brass_ingot`；写成 `minecraft:oak_log` 时它找不到这个物品，什么都不给就结束了）。交出去的个数在伙伴扔东西时数（`spawnAtLocation`），不看背包少了几个（`fetch` 时要先弄来再扔，背包里是先多后少）；PlayerEngine 结束了、交出的却少于 `count`，报 `incomplete` 或 `failed`。

## 已知问题和限制

- PlayerEngine 只认 1.4.x（见「依赖」）。
- 服务器重启后伙伴被接管，但正在做的任务不恢复，`lastResult` 也清空。WELCOME 的 `session` 变了，MineDeck 就知道之前的任务都没了。
- 伙伴只在 `attack` 任务里出手，做别的任务时被怪打不还手（怪物一般不主动打伙伴，它不是玩家）。伙伴会淹水（0.2.1 起空闲时会浮上来，干活时由 PlayerEngine 管）、摔伤、被烧，死了背包掉在原地，MineDeck 收到 `died` 后可以重新 `spawn`。
- `give` 是走到玩家旁边把东西扔给他：玩家背包满了，东西就留在地上（只有他捡得起来，5 分钟后消失）；`given` 是伙伴扔出去的个数，不保证玩家已经捡到。PlayerEngine 交东西实际上是「全给」或者「一个都不给」（够数了才去扔），所以 `incomplete` 很少见。
- 伙伴不捡玩家扔的东西，也不捡伙伴扔的东西（免得跟着玩家时把他扔给朋友的东西抢走）。所以玩家没法靠往地上扔东西给伙伴送材料。
- 禁挖区域只管伙伴，不管玩家；「只走路」时伙伴可能绕很远，绕不过去就报 `stuck`。
- 管理员在开服之后用 `/forceload` 加的区块，如果正好是伙伴用过的，伙伴走开后会被放开（开服时就有的不受影响）。
- 移动速度属性 0.33 是按 PlayerEngine 的输入幅度（0.3）折算的，实测 `goto` 22 格用 5–6 秒，和玩家走路差不多；没逐 tick 对比。
- 只在原版地形上试过；整合包里的模组方块（寻路会不会卡住、模组冲突）要放进好友的整合包里再试。
- PlayerEngine 1.4.0 会在服务端目录的 `playerengine/` 下写自己的文件：`server_player2.json`（Player2 相关的服务端设置）、`playerengine_settings.json`、`settings.txt`（Automatone 设置）、`configs/`、`data/`（每次启动扫一遍所有物品建的索引，只有原版物品时约 2 MB，整合包会更大）。
- 专用服务器每次启动都会打一行 ERROR：`Attempted to load class net/minecraft/client/multiplayer/MultiPlayerGameMode for invalid dist DEDICATED_SERVER`（PlayerEngine 的 mixin，不影响运行）。MineDeck 的本地排错规则「客户端模组放进了服务端」会被它误触发，要加例外。
- 有原版客户端进服时（会被拒掉之前），PlayerEngine 会打一段 `Payload playerengine:sync_server_player2 may not be sent to the client!` 的 WARN（它自己吞掉了）。
- 控制台输出的中文在非 UTF-8 的终端里是问号，看 `logs/latest.log`。Forge 1.20.1 用 JDK 17 时 `latest.log` 按系统默认编码写，Linux 的 POSIX 环境下也是问号，要加 `-Dfile.encoding=UTF-8`（Windows 中文系统上是 GBK，推断）。
- PlayerEngine 1.4.0 自带「尊重玩家建筑」（`respectStructuresEnabled`，默认开）：记下真玩家放的方块，寻路时拆它们的代价乘 500，只是更不愿意拆、不是禁止，而且只记装上 PlayerEngine 之后放的方块（读源码）。要保护的地方用禁挖区域。
- 测试用的开关：JVM 参数 `-Dminedeck.companion.debug.onlyActionGuard=true` 关掉禁挖区域的任务层和寻路层、只留动作层，用来实测「PlayerEngine 真要拆区域里的方块时也拆不掉」。正常运行时不要开。
- 「客户端只装伙伴模组」只在开发环境的客户端上试过，正式客户端见「真客户端自测」。

## 验证记录

2026-10-31 在云端容器里用真的无头专用服务器测入库的这两个 jar（0.2.0，sha256 `567b9c7e…`、`47bd5166…`；`-Xmx2G`，`online-mode=false`，每个加载器一个新世界、固定种子）。一个脚本按协议逐条发指令（testctl，以及直接发帧的 Python 小程序），游戏里的效果用原版命令核对（`data get entity`、`execute if entity`、`execute if blocks … all`、`forceload query`）；玩家用 mineflayer 机器人模拟：原版客户端；NeoForge 上只注册伙伴模组通道的客户端；Forge 上模拟 FML3 登录握手、按需要增减模组清单。测试服、日志和脚本都没有入库。

|  | NeoForge 1.21.1 | Forge 1.20.1 |
|---|---|---|
| 环境 | NeoForge 21.1.265 + Architectury 13.0.11 + PlayerEngine 1.21.1-1.4.0，JDK 21 | Forge 47.4.28 + Architectury 9.2.14 + PlayerEngine 1.20.1-1.4.0，JDK 17 |
| 开服（新世界） | 12.8 秒 | 40.5 秒 |
| 握手和帧 | 令牌不对 → 0.31 秒后 ERROR 1；版本 0 → ERROR 2；第二个连接 → ERROR 3；第一帧不是 HELLO → ERROR 4；HELLO 写版本 7 → WELCOME 版本 1；不认识的帧类型忽略、连接不断；不是 JSON、缺 `id` → `bad_json`（`id` 为 `null`）；不认识的 `op` → `unknown_op`；10.0 秒没收到帧 → END「心跳超时」 | 同 |
| 多伙伴、坏参数 | 刷出编号 1–3；第 4 个 `limit`；重名 `name_taken`；名字带 `§`、换行 → `bad_args`；`say` 只剩格式码和空白 → `bad_args`；另外 13 种坏指令（`ambiguous`、`no_companion`、`bad_args`×4、`rejected`、`no_player`×2、`forbidden`×2、不是生物的种类、`unknown_op`）错误码全对；刚刷出就发指令（同一个包里）→ `not_ready` | 同 |
| `collect oak_log 3` | `done`，12.0 秒；背包和 `data get entity` 都是 3 个原木 | `done`，10.5 秒；同 |
| `goto` 22 格（不给 `y`） | `done`，6.3 秒 | `done`，6.3 秒 |
| 换任务、停下、超时 | `goto` 换 `goto`：旧的 `superseded`，新的 2 秒后还在走，`stop` → `cancelled`（`stopped: [6]`）；`collect` 换 `collect`：旧的 `superseded`，新的照常走到 `timeout`（5 秒）；`timeout: 6` → `timeout` | 同 |
| `attack` | 3 只僵尸 + 起了名字、不动的 Bob + 半径外的一只，`count: 2` → `done`，6.4 秒，`killed: 2`；Bob 和半径外那只还在；只给 `duration: 8` → `done`，8.0 秒 | 同（6.7 秒、8.0 秒） |
| 进服检查 | 原版客户端：配置阶段被拒，收到中文提示（模组名、文件名、「PlayerEngine 和 Architectury API 只装在服务器上」）；只注册伙伴模组通道、没有 PlayerEngine 通道的客户端：进服 | 原版客户端：进入游戏时被拒，同样的中文提示；FML3 客户端没报伙伴模组：登录阶段被拒（`HandshakeHandlerMixin`）；只报伙伴模组、没有 PlayerEngine 和 Architectury 及其通道：放行 |
| `follow`、`give`、`say` | `follow`：PlayerEngine 任务是 `<Going to player Tester>`，玩家下线后 `failed`（「玩家 Tester 下线了」）；`give` 1 个原木：`done`，1.7 秒，服务端看玩家背包多了 1 个，跟着玩家的另一个伙伴没去捡；`say` 的 `§c` 和换行去掉了，私聊前面有「（悄悄话）」，控制台里没有像玩家聊天的行 | 同 |
| 配置热重读 | 上限改成 4 → `config_applied`；改端口 → 旧连接收到 END「端口改了，请重新连接」，新端口上能连 | 同 |
| 禁挖区域 | 区域（11 × 18 × 21）里有一棵树和一堵土墙，区域外一棵树（树叶换成了羊毛：树叶的 `distance` 状态会随周围的变化自己变，`clone` 出来的备份也会马上重算，逐块比较会误报）：`collect acacia_log 4` 只砍区域外的（9.3 秒）；`goto` 到墙另一边，绕过去（日志「规划的路要拆禁挖区域「base」里的方块 52, 64, -390，改走不拆不放的路」）；只剩区域里的树时 60 秒 `timeout`，中间报了 `stuck`；每一步之后区域里 4158 个方块和事先的备份逐块相同；写错的 `nodig.bad` 进了 `problems` | 同（9.0 秒，`goto` 5.7 秒） |
| 重启 | 3 个伙伴开服 1 秒内都接管了，编号、主人、标记、背包不变；强制加载的区块和关服前一样（10 个） | 同（8 个） |
| 存档里的伙伴丢了 | 不装本模组开一次服（实体被丢掉）再装回来：开服 30 秒后加载它们最后的区块找一次，10 秒后报找不到，删掉记录、放开留下的强制加载区块，`forceload query` 一个都没有了 | 同 |
| 关服 | 每次 2–4 秒自己退出；关服时正好在扫描的，日志「已停下 3 个伙伴的方块扫描，等正在扫的线程结束用了 76 毫秒」 | 每次 5–7 秒自己退出（14、19 毫秒） |
| 联网 | 4 次开服，代理没收到请求，JVM 的 `connect()` 只有 DNS（`8.8.8.8:53`） | 另外只有 Forge 自己的更新检查（`files.minecraftforge.net`） |

联网是这样查的：服务端 JVM 的 HTTP(S) 代理指到一个只记录、一律拒绝的本机代理（`-Dhttps.proxyHost` 等，`http.nonProxyHosts` 置空），同时用 `strace -e trace=connect` 记下 JVM 的每一次 `connect()`。对照：伙伴还在 `staticAPIServices` 里的旧版本，OP 打 `/playerengine relog` 时日志出现 `Starting web device flow`，代理记到两次 `CONNECT api.player2.game:443`；拿掉以后就没有了（第一步原型时测的）。

禁挖区域的动作层单独测过（NeoForge）：开 `-Dminedeck.companion.debug.onlyActionGuard=true`（只留动作层），同样的场地跑一遍。PlayerEngine 采集、走路时一次次要拆区域里的土墙（52, 64, -391），全被拦下（日志 24 行，每 10 秒最多记一行），30 秒里拦下 5 次就报 `stuck`（`reason` 是 `nodig`）；每一步之后区域里的 4158 个方块逐块不变。

**真客户端（只装伙伴模组）**：在容器里用 Xvfb + Mesa 软件渲染起了真的游戏客户端（ModDevGradle 的开发环境，`--quickPlayMultiplayer` 自动进服），类路径上没有 PlayerEngine 和 Architectury，服务端照常装 3 个：

- NeoForge 1.21.1：客户端 mods 里只放伙伴模组的发布 jar。进服顺利（服务端日志 `ClientTester joined the game`），在它面前刷出的伙伴是玩家模型、头上有名字，`item replace` 给的金头盔和铁剑看得见；聊天栏是青色名字的 `<小测> 你好，我是 MineDeck 的伙伴` 和灰色「（悄悄话）」开头的私聊；`follow` 时伙伴走到客户端旁边，客户端不掉线；客户端日志里没有一行提到 playerengine，除了连不上 Mojang 服务的报错（容器里没有那些域名）没有别的错误。
- Forge 1.20.1：1.20.1 的发布 jar 是 SRG 名，开发环境用不了，所以客户端里的伙伴模组是从同一份源码编译的（PlayerEngine、Architectury 只参与编译）。结果和 NeoForge 一样：进服、看得见伙伴和装备、聊天栏正常、跟随时不掉线、日志里没有 playerengine。

两个都是开发环境的客户端，和玩家用启动器装的正式客户端还差一步，所以「真客户端自测」里的第 1 项还要在 Windows 上做。

做法（想重做时）：另建一个只用来测的 ModDevGradle 工程，不加 PlayerEngine、Architectury 的运行时依赖，`runs { client { client(); programArguments.addAll('--quickPlayMultiplayer', '127.0.0.1:<游戏端口>', '--username', '<名字>') } }`；`run/options.txt` 里关掉首次启动的无障碍引导和多人游戏警告（`onboardAccessibility:false`、`skipMultiplayerWarning:true`）；在 `Xvfb :99 -fbdir <目录>` 里 `DISPLAY=:99 ./gradlew runClient`（Mesa 的 llvmpipe 软件渲染就够），客户端进服后用 testctl 在它面前刷伙伴，`-fbdir` 写出的帧缓冲（XWD 格式）转成 PNG 看画面。

测试中发现、已经改掉的问题：PlayerEngine 同一种指令互相替换时把旧任务的结束报到新任务头上（见「实现要点」的「换任务」）；物品名不认识时没有回 `rejected`；强制加载的区块重启后丢了（原版 `setChunkForced` 的保存标记问题）；Forge 1.20.1 关服时 PlayerEngine 的扫描线程让 Java 进程退不出去；`give` 扔下的东西被跟着玩家的伙伴捡走、落在玩家够不着的地方。

### 0.2.1（2026-10-31）

两半联调（`tools/companione2e`：MineDeck 侧的真代码对真的无头服务器，见那里的 README）发现的三个模组问题，在 0.2.1 里改掉。协议仍是 v1：只多了一个可选字段（`give` 的 `task_finished` 带 `want`），`result` 用的是已有的值；客户端会加载的类没改，进服检查的通道版本还是 `1`。

|  | 0.2.0 | 0.2.1 |
|---|---|---|
| 重启后，MineDeck 握手正好碰上接管 | WELCOME 里没有伙伴，`adopted` 也发不出去，要等下一个 STATUS（是竞态：两半联调时两个加载器每次重启都碰上，联调程序对比 0.2.0 跑了三轮，碰上两轮） | WELCOME 在主线程上生成：要么在 WELCOME 里（「现有 1 个伙伴」），要么紧跟着收到 `adopted`，两种都实测到了 |
| `give` 写 `minecraft:oak_log` | PlayerEngine 收到 `give Steve minecraft:oak_log 1`，21 毫秒就结束，什么都没给，报 `done`（`given` 0） | 交给 PlayerEngine 的是 `give Steve oak_log 1`，交出 1 个，玩家背包里多了 1 个 |
| `give` 一个都没交出去 | 报 `done` | 报 `failed`，`detail`：接东西的人在下界时「Alex 不在伙伴所在的维度」；背包里没有、也弄不到（`fetch` 要基岩）时「原来一个都没有，也没弄到」 |
| 停下来的伙伴站在 3 格深的水里 | 半分钟左右淹死 | 浮在水面上（离水底 2.5–2.7 格），65 秒血量不变、不离开水箱、没开始任务 |

做法：容器里的无头 NeoForge 21.1.265、Forge 47.4.28，入库的 0.2.1 jar（sha256 `6ea730ab…`、`06449d88…`，`update-embedded.sh --check` 通过）；联调程序 NeoForge 55 项全部通过，Forge 53 项没有失败（1 项跳过：Modrinth 上没有 Architectury 别的范围内版本）；多了上面这几项：H12、H13、W1、X1b、T0。同一个联调程序换上 0.2.0 的 jar（`-mod-jar`）在 NeoForge 上对比：55 项失败 6 项，就是这几项和模组版本号（C3），T0 是竞态、碰上时才失败（见交接说明「伙伴模组 0.2.1」）。`incomplete` 没有实测到：PlayerEngine 交东西是「全给」或者「一个都不给」（够数了才去扔），只给了一部分的情况造不出来，MineDeck 那边的通知文字由单元测试覆盖。

## 真客户端自测

容器里用开发环境的客户端试过「只装伙伴模组」（见「验证记录」）。下面这些要在 Windows 上用启动器装的正式客户端再试（服务端放 3 个 jar，用 testctl 发指令），每个加载器各试一遍：

1. **客户端只装伙伴模组**（mods 里只有 `minedeck-companion-<加载器>-<MC 版本>-0.2.1.jar`，没有 PlayerEngine、Architectury API）：
   - 游戏能启动，不报缺依赖；`logs/latest.log` 里没有 `com.player2`、`dev.architectury` 相关的 `ClassNotFoundException` / `NoClassDefFoundError`。
   - 能进服（服务端日志没有「拒绝没装伙伴模组的客户端进服」）。
   - 刷出伙伴后：看得见、是玩家模型、头上有名字；`collect`、`goto`、`follow`、`attack` 时走路、挥手、手上的工具和武器都正常显示，不掉线。
   - `say` 在聊天栏是 `<名字> 文本`，名字青色、悬停显示「MineDeck AI 伙伴」；`to` 私聊前面有灰色的「（悄悄话）」。
   - 伙伴不在 Tab 列表里。
2. **客户端装了 3 个**（第一步原型的装法）：同样能进服、看得见伙伴。
3. **装了加载器、没装伙伴模组**：进服时被拒，看到中文提示（模组名、文件名、「PlayerEngine 和 Architectury API 只装在服务器上」）。NeoForge 上如果看到的是英文的「未知的注册表键」，说明 `ConfigurationInitializationMixin` 没生效。
4. **原版客户端**：同样被拒，看到中文提示。
5. **单人游戏 / 局域网**（客户端装 3 个）：没测过，不急（MineDeck 现在只管专用服务器）。按代码，集成服务器开好后控制端口也会打开，配置文件在游戏目录的 `config/minedeck/companion.properties`。

要看的主要是正式客户端那一侧和开发环境不同的地方：模组从启动器的 mods 目录加载（Forge 1.20.1 是 SRG 名的发布 jar）、注册表核对（占位的 `playerengine:custom_fishing_bobber`）、实体的生成和渲染。容器里这几项在开发环境的客户端上都正常。
