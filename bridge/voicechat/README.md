# MineDeck 音乐共享桥接模组（原型）

MineDeck 在主机上采集音乐，通过本机 TCP 把 PCM 发给这个模组；模组用 Simple Voice Chat（SVC）的 Opus 编码器编码，经静态音频通道推给所有装了 SVC 的玩家。模组还注册一个音量分类「MineDeck 音乐共享」，每位玩家可以在 SVC 的「调整音量」界面里单独调小或静音。

- **一个 jar 通吃**：同一个 `minedeck-voicebridge.jar` 同时给 NeoForge 1.21.1 和 Forge 1.20.1 用。
- **只装在服务端**：客户端只需要和平时一样装 SVC。
- **不引用 Minecraft 的类**：只依赖 `java.*` 和 SVC 的插件 API，字节码是 Java 8（class 版本 52）。
- **不包含 SVC 的任何文件**：SVC 的许可是"保留所有权利"，它的 API 只在编译时引用。

MineDeck 已经接入（第五阶段）：jar 打进 exe（`internal/bridge/jar/`），在实例的「音乐共享」页一键装进 `mods/`；
Go 这一侧的客户端在 `internal/bridge`。协议、模组逻辑都已在 devtest（假 SVC）里跑通，**还没有在真的 Minecraft
服务端和客户端上测过**，见文末「验证情况」。

## 目录

```
bridge/voicechat/
  src/main/java/minedeck/voicebridge/   模组源码（Java 8）
  src/main/resources/                   META-INF/mods.toml（Forge）、META-INF/neoforge.mods.toml（NeoForge）、pack.mcmeta
  src/fallback/                         备用方案：javafml + 两个空入口类（编译用的 @Mod 注解桩不打包）
  build.sh / build.ps1                  构建脚本（javac --release 8 + jar，不用 Gradle）
  update-embedded.sh                    更新 / 检查打进 MineDeck exe 的 jar（internal/bridge/jar/）
  api-from-source.sh                    从 SVC 官方源码编译 API（只在下载不了时用，只用于本地编译）
  testsend/                             测试发送端（Go，协议用 MineDeck 的 internal/bridge）：发测试音或 WAV
  devtest/                              不装 Minecraft 的自动测试：假 SVC + TOML 校验
```

## 构建

需要 JDK 17 或更新（用 `--release 8` 出 Java 8 字节码）。

```bash
./build.sh                 # 输出 build/minedeck-voicebridge.jar
./build.sh --fallback      # 另外输出 build/minedeck-voicebridge-javafml.jar（备用方案）
```

Windows：`.\build.ps1`、`.\build.ps1 -Fallback`（步骤和产物与 build.sh 相同；这个脚本还没在 Windows 上实际跑过）。

**SVC API 从哪来**（只在编译时用，不打进 jar，不入库）：

1. `--api <jar>` 或环境变量 `SVC_API_JAR` 指定；
2. 否则用缓存目录（默认 `.cache/`，可用 `MINEDECK_CACHE` 改）里的 `voicechat-api-2.6.27.jar`；
3. 没有就从 `https://maven.maxhenkel.de/repository/public/de/maxhenkel/voicechat/voicechat-api/2.6.27/` 下载并校验 sha1；
4. 下载不了时运行 `./api-from-source.sh`：从 `github.com/henkelmax/simple-voice-chat` 取 1.21.1 分支 2026-10-25 的提交（API 2.6.27，和 1.20.1 分支的 `api/` 目录逐字节相同），用 `javac --release 8` 编出一份放进缓存目录。

构建脚本最后会检查：两份 toml 和 `pack.mcmeta` 都在；jar 里没有 `de/maxhenkel/` 下的文件；每个 class 的版本号都是 52；`jdeps` 显示依赖只有 `java.*` 和 `de.maxhenkel.voicechat.api.*`；`BridgePlugin` 带 `@ForgeVoicechatPlugin`。jar 里的时间戳固定、文件按顺序打包、清单里的 `Created-By` 自己写（不让 jar 填 JDK 的版本号），同样的源码和 JDK 构建出的 jar 逐字节相同。

### 打进 MineDeck 的 jar

MineDeck 不在运行时下载这个模组：`build.sh --fallback` 构建出的两个 jar 原样入库在 `internal/bridge/jar/`，
用 `go:embed` 打进 exe，`internal/bridge/jar.go` 里登记了版本号和两个 jar 的 sha256。安装时先核对 sha256，
写进 `mods/` 后再读回来核对一次。

- 改了模组源码以后运行 `./update-embedded.sh`：重新构建，把两个 jar 复制进 `internal/bridge/jar/`，并改好 `jar.go` 里的版本号和 sha256。
- `./update-embedded.sh --check` 只检查：重新构建，和入库的 jar 逐字节比较、和 `jar.go` 登记的 sha256 比较，不一样时退出码 1。
- 用 JDK 21 构建（入库的是 OpenJDK 21.0.12 构建的）。SVC API 不管是从 Maven 下载的还是 `api-from-source.sh` 编的，编出的 class 都一样（只用来编译，不进 jar）。
- 当前入库：版本 0.2.0，`minedeck-voicebridge.jar` 的 sha256 是 `11a4e360f1ab879421e17c2f4fad921d5e5b7ef20d0f691c45d5c5f57e891e69`，`minedeck-voicebridge-javafml.jar` 是 `b1af78b47de389abf0e7311138eacd192a1267bb407e84d74621967107e28db3`。

装进实例时文件名带版本号：`minedeck-voicebridge-0.2.0.jar`（备用加载方式是 `minedeck-voicebridge-javafml-0.2.0.jar`），
`mods/` 里别的版本（包括原型阶段不带版本号的 `minedeck-voicebridge.jar`）会被删掉：同一个模组装两份，加载器会报重复模组。

## 安装

把 jar 放进服务端的 `mods/`，服务端还要装 SVC。不用装在客户端（误装了也不影响进服）。

- NeoForge 读 `META-INF/neoforge.mods.toml`，Forge 读 `META-INF/mods.toml`，互不干扰。
- 两份 toml 都用 `lowcodefml`（不需要入口类的加载方式）：插件类由 SVC 自己扫描 `@ForgeVoicechatPlugin` 注解找到。
- 依赖：必须有 SVC（`voicechat`），**不限版本**，`versionRange="[0,)"`。不能写成 `[2.6,)`：SVC 在 Forge / NeoForge 上的版本号是 `1.20.1-2.6.27`、`1.21.1-2.6.27` 这种格式，按 Maven 规则比 `2.6` 小，会被判定版本不够。也不能省略或写空字符串：Forge 1.20.1 把这两种都当成"什么版本都不匹配"。
- 启动时检测 SVC 实际提供的 API：SVC 2.6.0 起用一个多目标静态通道；更老的 SVC 自动改用"每位听众一个静态通道"的旧接口（devtest 里用 2.5.0 的 API 测过）。

## 配置文件

路径：`<实例目录>/config/minedeck/voicebridge.properties`（相对服务端的工作目录，和 SVC 自己的 `config/voicechat/` 同一层；模组启动时会在日志里打出绝对路径）。

文件不存在时，模组生成一份**端口关闭**的模板（令牌随机）。模组每 2 秒读一次文件，内容和上次应用的不一样就重新应用，**不用重启服务器**（逐字节比较内容，不看修改时间和大小；发现变化后隔 0.15 秒再读一遍，两次一样才应用，免得读到别的程序写了一半的文件）：

| 键 | 默认 | 说明 |
|---|---|---|
| `enabled` | `false` | 是否在 127.0.0.1 上打开音乐共享端口 |
| `port` | `24470` | 端口，1024–65535。改了会关掉旧端口、断开当前广播，再开新端口 |
| `token` | 随机 32 位十六进制 | 共享令牌，至少 16 个字符；发送端握手时要带上。改了只影响之后的新连接 |
| `exclude` | 空 | 不推送的玩家，填名字或 UUID，多个用逗号隔开（中英文逗号都行）。房主就在开服的电脑上玩、本来就听得到原声时，把自己填进来，免得听两遍（回声） |
| `selftest` | `false` | 自测模式：没有广播连接时持续推送测试音（C 大调琶音）。有广播连接时让位给广播，断开后自动接着放 |
| `bypass_group_isolation` | `true` | 处在"隔离"类型语音群组里的玩家也能听到音乐（SVC 2.6 起才有这个设置） |

按名字排除：模组用反射从 SVC 给的玩家对象里找"返回 `GameProfile` 的无参方法"取名字（Forge 1.20.1 正式环境的方法名是混淆过的，所以按返回类型找，不按名字找）。取不到名字时只能按 UUID 排除，日志里每位玩家连上语音时都会打出名字和 UUID。

**MineDeck 的写法**（`internal/bridge/config.go`）

- 装桥接模组、开服前、开始广播时各检查一次：打开共享（`enabled=true`），端口从 24470 起找一个现在空着、别的实例也没用的（不用系统随机给的：Windows 的动态端口随时会被别的出站连接占走），令牌每个实例随机生成 16 字节的十六进制，只写进这个文件，不放进命令行或日志。已有的只改这几行，其余原样保留。
- 先写临时文件再改名替换（原子替换），模组不会读到写了一半的文件。
- 用 UTF-8 写，一行一个 `key=value`。值里不要有反斜杠（Java 的 properties 格式会把它当转义符）。
- 「我在游戏里的名字」写进 `exclude`，2 秒内生效。刚改过配置时，客户端等 2.5 秒（模组重读一次的时间）再连，免得碰上旧端口、旧令牌。

## 协议（第 1 版）

模组在 `127.0.0.1:<port>` 上监听（只监听本机，Windows 防火墙不会弹窗），MineDeck 来连。**同一时间只接受一个广播连接**。

**帧格式**：每帧 4 字节帧头加内容。**所有整数都是小端**（和 WASAPI、WAV 的 PCM 一致，发送端不用转换字节序）；字符串都是 UTF-8。

| 偏移 | 长度 | 内容 |
|---|---|---|
| 0 | 1 | 类型 |
| 1 | 1 | 保留：发送方填 0，接收方忽略 |
| 2 | 2 | 内容长度 N，最大 4096 |
| 4 | N | 内容 |

**帧类型**

| 类型 | 名字 | 方向 | 内容 |
|---|---|---|---|
| `0x01` | HELLO | MineDeck → 模组 | 协议版本 2 字节（=1）、令牌长度 1 字节、令牌、广播者名字（剩下的全部，可以为空） |
| `0x02` | WELCOME | 模组 → MineDeck | 协议版本 2、采样率 4（48000）、每帧采样数 2（960）、声道数 1（1）、当前听众数 2、模组版本（剩下的全部） |
| `0x03` | ERROR | 模组 → MineDeck | 错误码 2、说明（剩下的全部）。发完就断开 |
| `0x10` | PCM | MineDeck → 模组 | 序号 4（从 0 开始每帧加 1）、960 个 16 位有符号采样（48 kHz 单声道，20 毫秒），共 1924 字节 |
| `0x20` | PING | MineDeck → 模组 | 任意内容（建议 8 字节时间戳），模组原样回 PONG |
| `0x21` | PONG | 模组 → MineDeck | PING 的内容原样带回，可以算往返时间 |
| `0x30` | STATUS | 模组 → MineDeck | 每 2 秒一次：听众数 2、缓冲帧数 2、音源 1（0 无 / 1 广播 / 2 自测音）、收到 4、编码 4、发出 4、丢弃 4、欠载 4（计数从这次连接开始） |
| `0x40` | END | 双向 | 原因（剩下的全部，可以为空）。发完就断开。模组在关服、配置关闭共享、改端口时会发 |

**错误码**：1 令牌不对；2 协议版本不支持；3 已经有一个广播连接；4 协议错误（帧不合规矩，比如 PCM 长度不对、第一帧不是 HELLO）；5 音乐共享已关闭。

**流程**

```
MineDeck                                   模组
  ── 连接 127.0.0.1:port ─────────────────►
  ── HELLO(1, 令牌, "房主") ───────────────►   连上后 5 秒内要发
  ◄──────────────────────────── WELCOME ───   或者 ERROR，然后断开
  ── PCM #0、#1、#2…（每 20 毫秒一帧）─────►   模组先攒 3 帧（60 毫秒）再播
  ── PING（每秒一次）──────────────────────►
  ◄─────────────────────────────── PONG ───
  ◄──────────────────── STATUS（每 2 秒）───
  ── END("用户停止") ──────────────────────►   然后断开
```

- **心跳**：模组 5 秒没收到任何帧（PCM 或 PING）就断开。MineDeck 每秒至少发一帧；5 秒没收到模组的任何帧（STATUS 每 2 秒一次）就当作断线。
- **节奏**：按实时速度发，每 20 毫秒一帧，不要一次性灌。模组按自己的时钟每 20 毫秒播一帧：缓冲最多 10 帧，多了丢最旧的；连续 1 秒多于 5 帧就丢一帧，把两边时钟的偏差追平（延迟保持在 100 毫秒左右）；取空了就跳过，重新攒 3 帧。
- **暂停**：没声音时可以只发 PING、不发 PCM。模组 100 毫秒收不到新帧就让 SVC 收尾（客户端结束这一段声音），重新有数据时自动接上。
- **断线重连**：连接断了就隔 1～2 秒重连，**每次重连前重新读一遍配置文件**（端口、令牌可能刚改过：模组改端口时会给旧连接发 END「端口改了，请重新连接」）。收到 ERROR 3（忙）也可以稍后重试：旧连接要么已经关闭，要么最多 5 秒后心跳超时。ERROR 2 说明版本不对，不要重试；ERROR 1（令牌不对）、5（共享已关闭）可能只是模组还没读到新配置，等一小会儿（MineDeck 等 10 秒）还是这样再放弃。
- **对方不读数据**：模组发给发送端的帧（WELCOME、ERROR、PONG、STATUS、END）都由这个连接自己的发送线程去写，别的线程只往它的发送队列里放，从不阻塞。发送端一直不读数据、socket 写满时，一次写入卡住超过 5 秒（或者回 PONG 时 5 秒都排不进队列），模组就断开它。
- **没有听众时**：照常编码（日志里能看到"Opus 编码正常"），只是不发送。
- **以后**：第 2 版打算加 `0x11 OPUS`（浏览器直接给的 Opus 包，原样转发）和 STATUS 里的听众明细。

协议的 Go 实现在 MineDeck 的 `internal/bridge`（`proto.go` 编解码，`client.go` 是带握手、心跳、断线重连的客户端），testsend 也用它；Java 实现见 `src/main/java/minedeck/voicebridge/Protocol.java`。

**线程**：模组的音频线程（MineDeck-VoiceBridge-Audio）每 20 毫秒取一帧、编码、交给 SVC，只做音频，不碰 socket 和文件；
每 2 秒一次的定时检查（重读配置、端口被占时重试、发 STATUS、每 30 秒记一次统计）在 MineDeck-VoiceBridge-Housekeeping
线程里；每个连接有一个读线程和一个发送线程。语音服务器停止时先停定时检查线程，再关端口；端口关掉以后，
这一轮语音服务器的对象不会再打开端口（定时检查刚读完配置、语音服务器就停了，两边撞在一起时也一样）。

## 日志

模组的日志和 SVC 的一起进 `logs/latest.log`（Windows 控制台中文乱码时看这个文件）。正常启动时能看到：

```
[voicechat] Loading plugins
[voicechat] Loaded 1 plugin(s)
[voicechat] Registering events for 'minedeck_voicebridge'
[minedeck_voicebridge] MineDeck 音乐共享桥接 0.2.0 已被 SVC 加载，等待语音服务器启动
[minedeck_voicebridge] 语音服务器已启动，初始化音乐共享（推送方式：多目标静态通道（SVC 2.6 及以上））
[minedeck_voicebridge] 已注册音量分类「MineDeck 音乐共享」（id=minedeck_music）……
[minedeck_voicebridge] 音乐共享端口已打开：127.0.0.1:24470（只接受本机连接，同一时间只接受一个广播连接）
```

前三行是 SVC 自己打的（`Loaded N plugin(s)` 的 N 是服务端所有 SVC 插件的总数）。广播时还有"广播连接已建立 / 开始推送音频 / 最近 30 秒：收到…帧，编码…帧" 这些行。

## 自测（不用 MineDeck）

**方法一：自测模式**（只要这个 jar 和 SVC）

1. jar 放进服务端 `mods/`，启动一次服务器，生成 `config/minedeck/voicebridge.properties`。
2. 把里面的 `selftest=false` 改成 `selftest=true`，保存（不用重启，约 2 秒生效）。
3. 用装了 SVC 的客户端进服，等语音连上，就能听到循环的琶音。
4. 按 V 打开语音菜单，点「调整音量」，找到带音符图标的「MineDeck 音乐共享」，拉到 0（显示"已静音"）应该只静音音乐，不影响别人说话。
5. 测完把 `selftest` 改回 `false`。

**方法二：testsend**（验证协议和整条链路）

```bash
go build -o testsend ./bridge/voicechat/testsend        # 在仓库根目录；Windows 加 GOOS=windows 编成 testsend.exe
# 配置文件里先设 enabled=true
testsend -config <实例目录>/config/minedeck/voicebridge.properties                  # 发 30 秒 440 Hz
testsend -config ... -tone 0 -duration 0                                           # 一直发琶音，Ctrl+C 停
testsend -config ... -wav 歌.wav -loop                                              # 发 WAV（会转成 48 kHz 单声道）
testsend -config ... -duration 0 -reconnect                                        # 断线自动重连（可以重启服务器试试）
testsend -config ... -client -duration 0                                           # 用 MineDeck 自己的客户端（bridge.Client）发
```

退出码：0 正常；1 本地错误；2 模组拒绝（会打出错误码）；3 连接中途断开。`-stall`、`-speed` 是测试心跳超时和时钟偏差用的。

## devtest（不装 Minecraft 的自动测试）

```bash
SVC_API_JAR=<voicechat-api-2.6.27.jar> ./devtest/run.sh
SVC_OLD_API_JARS="<旧版 API jar> ..." ./devtest/run.sh     # 另外用旧版 API 跑一遍，测兼容模式
```

1. 构建主 jar 和备用 jar，做上面那些检查。
2. 用加载器同款的库检查两份 toml：Forge 1.20.1 带的 night-config 3.6.4、NeoForge 1.21.1 带的 3.8.3，版本范围用两边都带的 maven-artifact 3.8.5；校验规则照两边加载器的源码（必填字段、modId 格式、Forge 必须写 `mandatory`、`lowcodefml` 的版本、SVC 的版本号能不能满足依赖等）。
3. 假 SVC：像 FML 那样解析 class 文件找 `@ForgeVoicechatPlugin`，像 SVC 那样 `Class.forName` + 无参构造创建插件，再模拟语音服务器启动、三位玩家连上语音（其中一位按名字排除），然后用 testsend 依次测：端口被别的程序占着时每 10 秒重试、令牌错误、正常广播 3 秒（每位听众收到 150 帧、每帧只编码一次）、同时第二个连接被拒绝、发送端被强杀后重连、心跳超时（约 5 秒断开）、协议错误、玩家断开、改配置热重载（排除名单、自测模式、开关端口）、自测音和广播互相让位、发送端快 2% 时缓冲不会一直变长、配置文件改了但修改时间和大小都没变时照样重读（13a）、发送端只发不收时定时检查不被拖住、模组主动断开它（13b）、停服时给正在广播的连接发 END 并关掉端口和线程、停服之后才应用的配置不会把端口重新打开（14）、语音服务器重启后照常工作、MineDeck 自己的客户端（`testsend -client`）推音频并在端口和令牌改了以后重读配置接着连（16）。

## 为什么一个 jar 两边都能用（读源码确认的）

- **NeoForge 1.21.1**（FancyModLoader 4.0.x）：`JarModsDotTomlModFileReader` 只看 `META-INF/neoforge.mods.toml`，有它就当 NeoForge 模组读；"只有 `mods.toml` 的是 Forge 模组" 的判断（`IncompatibleModReason`）只在没有任何读取器认领这个 jar 时才做。`lowcodefml` 是内置的语言加载器。
- **Forge 1.20.1**：只读 `META-INF/mods.toml`，不认识 `neoforge.mods.toml`，也就不管它。`lowcodefml` 由 `lowcodelanguage` 提供。依赖必须写 `mandatory`（缺了直接报错）。没有 `pack.mcmeta` 的模组会被记一条"资源包无效"的警告，所以 jar 里带了一个（`pack_format` 15，`supported_formats` 15–48，两边都认）。
- **两边的注解扫描**：所有模组文件（不管哪种加载方式）都会被扫描，类上的注解都会记下来（不管保留策略）；`ModList.getAllScanData()` 包括 lowcodefml 模组。
- **SVC**（1.20.1 / 1.21.1 分支）：`ForgeCommonCompatibilityManager` / `NeoForgeCommonCompatibilityManager` 的 `loadPlugins()` 遍历 `getAllScanData()` 里的 `@ForgeVoicechatPlugin`，`Class.forName` 后用无参构造函数创建。

**备用方案**：如果真机上 lowcodefml 版找不到插件，换 `build.sh --fallback` 出的 `minedeck-voicebridge-javafml.jar`：两份 toml 改成 `javafml`，jar 里多两个空入口类，分别带 Forge 和 NeoForge 的 `@Mod("minedeck_voicebridge")`（每个加载器只认自己那个注解，另一个类不会被加载）。编译用的 `@Mod` 注解是自己写的桩，不打包。

## 许可

本模组的代码按仓库的 MIT 许可。SVC 的许可是"保留所有权利"：仓库和 jar 里都不包含 SVC 的任何文件；SVC API 只在编译时引用（构建时下载到被 git 忽略的缓存目录，或者用 `api-from-source.sh` 在本地从官方源码编译）。

## 验证情况

第五阶段核实了代码审查指出的两个隐患，在 devtest 里用改之前的模组都能复现，改完后都通过：

- **音频线程上的阻塞式 socket 写入**：属实。原来每 2 秒的定时检查跑在音频线程上，给广播连接发 STATUS 要拿这个连接的写锁；发送端只发不收时，连接线程回 PONG 卡在 socket 写入上（Java 的阻塞 socket 没有写超时）、一直占着写锁，音频线程跟着卡住：所有人的音乐、自测音和配置重读都停了，而且这个连接永远不会被断开（13b 原来两项失败）。改成：定时检查挪到单独的线程；每个连接有自己的发送线程和发送队列，别的线程只往队列里放；一次写入卡住 5 秒就断开。
- **配置热重读的竞态**：属实，有两处。一是按「修改时间 × 31 + 大小」判断文件变没变，同一毫秒里连写两次（大小一样）、修改时间只精确到 2 秒的盘、或者两个值凑巧相抵时会漏掉改动（13a 原来失败）；改成逐字节比较内容，发现变化后隔 0.15 秒再读一遍、两次一样才应用。二是定时检查刚读完配置、还没应用时语音服务器停了，停服之后才应用的配置会把端口重新打开，留下一个没人管的监听端口，SVC 重启语音服务器后新连接会连到这个死掉的旧对象上、听众收不到声音（14、15 原来失败）；改成停服时先停定时检查线程，关掉的 ControlServer 不再打开端口。Go 这一侧：原来的 testsend 只在启动时读一次配置，模组改了端口或令牌后重连永远连不上；MineDeck 的客户端每次连接前都重读配置，刚改过配置时先等模组重读，令牌不对 / 共享已关闭在 10 秒内当作「模组还没读到新配置」继续重试（`internal/bridge` 的测试和 devtest 16 覆盖）。

已做：

- 构建：主 jar、备用 jar 都通过上面的检查；同样的输入两次构建 sha256 相同。
- TOML：两份 toml 用两边加载器同款的 night-config 和 maven-artifact 解析、校验通过。
- devtest：用 SVC API 2.6.27（多目标）跑完整流程全部通过（74 项）；主 jar 和备用 jar 都跑了；另外用 2.6.0 开发中途的快照（还没有 `addTarget`）和 2.5.0 的 API 跑，自动改用旧版兼容方式，也全部通过（71 项，少的 3 项只对多目标方式）。
- testsend 的 Windows 版（`GOOS=windows` 编译）在 wine 里跑同一套 devtest：73 项通过，只有最后一项（像按 Ctrl+C 那样让 `-client` 退出）不算数——devtest 用 SIGTERM 通知它，wine 收到 SIGTERM 直接结束了进程（退出码 143）。Go 单元测试在 wine 里也通过。

没做到（需要在真机上测，步骤见自测说明）：

- 在真的 NeoForge 1.21.1、Forge 1.20.1 服务端上加载（开发环境的网络策略挡住了 Mojang、NeoForged、Forge、BMCLAPI、Modrinth、CurseForge 的下载，起不了服务端）。
- 真的 SVC Opus 编码器的输出、客户端实际听到的声音、音量分类的界面和静音效果。
- `build.ps1` 还没在 Windows 上跑过。
