# AI 伙伴两半联调（companione2e）

MineDeck 这一半的真代码，对真的无头服务器和真的伙伴模组，从安装到游戏内指挥走一遍。第七阶段两半合并以后用它验证两边能配合起来；以后改了 `internal/companion`、`internal/chatai` 的伙伴部分，或者换了内置的伙伴模组，可以再跑一次。

- 用到的 MineDeck 代码：`internal/companion`（安装、协议 v1 客户端、管理器）、`internal/chatai`（游戏内 AI 和伙伴工具、任务通知）、`internal/ai`（AI 接入层、记账和额度）、`internal/ops`（操作记录）、`internal/diag`（排错规则）、`internal/events`（实例启停事件）。
- `inst`、`ops/catalog`、`web` 只能在 Windows 上编译，所以服务器进程由这里托管（`server.go`），照抄了它们和伙伴有关的几处：开服前回调、JVM 参数（`inst.launchArgs`）、环境变量（`procutil.CleanEnv`）、控制台解码（`inst.decodeConsole` 的 auto）、在线名单、`Done (…)!` 算开好、`instance.start/stop/crash` 事件、停服的做法，以及 `catalog` 里把伙伴、游戏内 AI 接起来的方式。改了那几处要跟着改这里。
- 玩家是 mineflayer 机器人（`bot.js`）：NeoForge 上在配置阶段声明 `minedeck_companion:hello` 通道（客户端只装伙伴模组），Forge 上模拟 FML3 登录握手。AI 是本机的假服务（`fakeai.go`，OpenAI 兼容、流式），按玩家的话调伙伴工具；没给的工具也照样调，模拟不听话的模型。
- 依赖从真的 Modrinth 下载（只在「安装」那一步）。

所有文件都带 build tag `companione2e`，`go test ./...`、`go vet ./...` 不会编译它。

## 准备

只能在 Linux 上跑（要 `java`、`node`、`iptables`，断线那一步要 root）。

1. JDK：NeoForge 1.21.1 要 21，Forge 1.20.1 要 17。
2. 装好加载器的服务端目录（只取它的 `libraries/`），用加载器的安装器装：

   ```bash
   java -jar neoforge-21.1.265-installer.jar --installServer <目录>/srv-neo
   java -jar forge-1.20.1-47.4.28-installer.jar --installServer <目录>/srv-forge
   ```

   版本要和 `main.go` 里的 `presets` 一致（启动参数文件 `libraries/…/unix_args.txt`）。
3. mineflayer：`npm install --prefix <目录>/node-bot mineflayer@4.39.0`。
4. 网络能连 `api.modrinth.com`、`cdn.modrinth.com`。

不要把实例、下载的东西放进仓库。

## 跑

在仓库根目录：

```bash
go build -tags companione2e -o <目录>/companione2e ./tools/companione2e
<目录>/companione2e -loader neo   -work <目录>/p7e2e -template <目录>/srv-neo   -node-modules <目录>/node-bot/node_modules
<目录>/companione2e -loader forge -work <目录>/p7e2e -template <目录>/srv-forge -node-modules <目录>/node-bot/node_modules \
    -java /usr/lib/jvm/java-17-openjdk-amd64/bin/java
```

- 每次都在 `<work>/<loader>/inst` 新建一个干净的实例（`libraries/` 硬链接过来，`mods` 是空的，新世界，种子 `minedeck-p7`，和平难度），跑完关服。
- 一次只起一台服务器，`-Xmx2048M`（`-xmx` 可改）。端口：NeoForge 游戏 25631、控制 25641（改端口那一步用 25642）；Forge 游戏 25632、控制 25643（25644）。`-game-port`、`-ctl-port` 可改。
- `-steps` 只跑其中几步（逗号隔开）：`install,connect,manage,chat,water,reconnect,portchange,diag,exit,stop,restart`，默认 `all`。安装每次都做；后面几步要先开服，所以 `connect` 总是跟着跑。
- `-jvm "<参数>"` 在 `inst.launchArgs` 的固定参数后面加 JVM 参数（比如复现编码问题时）。
- `-mod-jar <jar>` 装好以后用这个 jar 换掉内置的伙伴模组，只为和别的版本对比（比如修好之前的 0.2.0：`git show 733c151:internal/companion/jar/minedeck-companion-neoforge-1.21.1.jar`）；安装那几项照常按内置的查，C3 的模组版本对不上是正常的。
- 中途 Ctrl-C：照样关服、退出玩家、去掉 iptables 规则。
- 退出码：0 全部通过（SKIP 不算失败），1 有失败。

结果在 `<work>/<loader>/`：`evidence.txt`（每一项的结论和证据要点，末尾是汇总表）、`console.log`（MineDeck 看到的控制台，带时间）、`bot-Steve.log`、`bot-Alex.log`（机器人收到的消息）、`inst/logs/latest.log`（模组日志，UTF-8）、`oplog/`（操作记录）、`ai/`（用量记账）。

## 测什么

| 编号 | 内容 |
|---|---|
| I1a–I1c | 干净的实例：`PreviewInstall` 列出要写的伙伴模组和要下载的两个依赖；`Install` 从真 Modrinth 下载；伙伴模组和内置的逐字节相同（sha256），依赖的大小、sha512、`mods.toml` 版本和清单里实测过的一样 |
| I2 | 再装一次：`Noop`，什么都不改（文件修改时间不变） |
| I3 | 放一个旧版伙伴模组（原型 0.1.0，从 git 历史 `-old-rev` 取）：状态 `outdated`，安装时换掉 |
| I4–I6 | 依赖装着范围内的别的版本（从 Modrinth 另取一个，比如 Architectury 13.0.8）：两份都在时只留实测过的；只有它时不动（`inrange`）；删掉后重新装回实测过的。Modrinth 上没有别的范围内版本时跳过（Forge 1.20.1 就是这样） |
| C1–C5 | 打开伙伴（写好端口、令牌）、打开游戏内 AI（授权 Steve）；开服到 `Done` 后自动连上；状态里有模组、PlayerEngine 的版本、协议、`ops`、`features`；模组日志里有这次连接；两个机器人进服 |
| M1–M8 | 管理（界面按钮的接口）：刷出 3 个、重名被拒；停下一个（刚刷出来的伙伴第一次 tick 之前模组回 `not_ready`，服务器不忙时刷出和紧跟着的指令会落在同一个 tick 间隙里，所以这一项隔 100 毫秒重试，最多 5 秒）、全部停下；改上限（`config_applied`，超上限被拒）、改 `attack_deny`（打牛被拒）、改禁挖区域（两个角反着写，模组整理成 min/max，「还没读到」的提示消失）；移除时掉落 / 不掉落背包 |
| H1–H7 | 游戏内指挥（先在出生点上空 y=100 铺一块 29×29 的草地，下面垫一层屏障、四周围 4 格高的屏障，放好两棵树，玩家和伙伴都放上去，goto 的目标、水箱也在里面：地形每次一样，旁边没有水；屏障拆不掉，PlayerEngine 去找远处的树时也走不出去、掉不下去）：`@小木 跟着我`（以小木的名义回复，伙伴真的走过来）；`@小木 砍 3 个原木`（做完后 Steve 私下收到模板通知，Alex 收不到，不调用 AI，背包里有原木）；`@AI 让伙伴停下`（被停下的不通知）；没授权的 Alex、带 `/问` 的：请求里没有伙伴工具，模型硬调也被拦，模组没开始任务；用量分别记在 AI 伙伴、游戏内 AI 名下，次数、操作记录对得上；AI 伙伴的每日额度、每人每天的次数用完时不调用 AI |
| H8–H11 | `@小木 给我 1 个原木`（Steve 背包里多了原木，做完后通知）；`@小木 去 X Z`（走过去，到了通知）；`@小木 跟大家打个招呼`（伙伴以自己的名字说话，§ 格式码和换行去掉了）；`@小木 去打村民`（模组回 `forbidden`，AI 照实转告，没开始任务） |
| H12–H13 | give 一个都没交出去（模组 0.2.1 起报 `failed`，0.2.0 报 `done`）：`@小木 给 Alex 1 个原木` 时 Alex 在下界，Steve 收到「……失败了：交出了 0 个（要 1 个）……Alex 不在伙伴所在的维度」；`@小木 给我 1 个基岩`（fetch，背包里没有、PlayerEngine 也弄不到），收到「……原来一个都没有，也没弄到」 |
| W1 | 停下来的伙伴（PlayerEngine 停用了 TaskRunner，它的防淹水不跑）放进 3 格深的玻璃水箱，看 65 秒：活着、不掉血、浮在水面（离水底 2.5 格左右）、没离开水箱、没开始任务（模组 0.2.1 起；0.2.0 半分钟左右淹死）。看完拆掉水箱、清掉里面的掉落物；淹死了就重新刷一个小木给后面几步用（只有换成旧版对比时才会这样） |
| R1–R3 | 伙伴采集时用 iptables 丢掉控制端口的包：MineDeck 10 秒后发现、开始重连；任务在断开期间做完；恢复后自动重连，照样私下通知 Steve |
| P1 | 改配置文件里的端口再改回来：模组发 END「端口改了」，MineDeck 在新端口上重连 |
| D1 | 装了 PlayerEngine 的服正常启动后，排错规则不报「客户端模组放进了服务端」（控制台里确实有 PlayerEngine 那一行 dist 报错） |
| X1–X4 | 退出 MineDeck（`companion.ShutdownAll`）：模组收到 END；X1b 趁 MineDeck 断开，用协议客户端直接发 `give minecraft:oak_log`（绕开 MineDeck 自己去掉 `minecraft:` 的那一步）：模组交给 PlayerEngine 的是 `give Steve oak_log 1`，真的交出去了；再连上；服务器开着时在「伙伴」标签里关掉伙伴：模组收到 END、关掉端口；再打开，自动连上 |
| S1–S2 | 关服：进程自己退出；模组先发 END「服务器正在关闭」，MineDeck 当成正常关服（不报「会自动重连」，不再重连） |
| T0–T2 | 重启：接管马上就看得到（WELCOME 里「现有 1 个伙伴」，或者紧跟着收到 `adopted`；只看事件，不看会被 STATUS 补上的伙伴列表；模组 0.2.1 起。0.2.0 是竞态：握手落在接管的那个 tick 里时两头都没有，第七阶段两半联调每次重启都碰上，但不是每次都碰得上，所以这一项对 0.2.0 不一定失败）；重新连上，存档里的伙伴被接管（编号和关服前一样、主人不变），session 变了；再关服 |

聊天的节奏：游戏内 AI 防刷屏，同一个人 1 分钟内触发超过 5 次会被静默 10 分钟，所以 Steve 问满 5 次后会等一会儿（证据里有「等 N 秒」）。

## 测不到的

- `internal/web` 的 HTTP 接口和「伙伴」标签的界面：只能在 Windows 上编译，这里直接调 `companion.Service` 的接口（网页接口只是经操作层转一下，有自己的单元测试）。
- Windows 上的事：文件被服务器占着删不掉（运行中换旧版本）、DPAPI、WebView2、国内网络下 Modrinth 的速度。
- 正式的游戏客户端：机器人只是模拟了握手，看不到伙伴长什么样（见 `companion/README.md` 的「真客户端自测」）。
- 真的 AI：假 AI 只按关键词调工具，测的是 MineDeck 的权限、记账和通知，不是模型选工具的本事。
