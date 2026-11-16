### 工作分支 claude/pensive-pascal-r0p9mz（2026-11-02 已合入 main）

2026-11-02 用户让把这个分支合进 main，合并提交是「合并：AI 接入与音乐共享（阶段 1–5、7）、MCP 接口、v2.9.0-beta.2 和实测后的第一批修复」，合并后 main 的内容和分支一模一样。合的时候 beta.2 的第一批修复还没在 Windows 上复测（见下文「v2.9.0-beta.2 的实测和第一批修复」），版本号仍是 2.9.0-beta.2，没有打新 tag。分支上那 87 个云端会话的提交作者是 Claude（noreply 邮箱），所以不算进用户 GitHub 主页的贡献格子；用户本机提交用的是 `Atticus Sylvan <HNXWen001@outlook.com>`，这个邮箱绑定在 Trasperioooooon 账号上。两个已发布的 tag 指向那段历史，所以没有改写作者。

下面是这个分支在当时的 main 之上做的事：设计文档的阶段 1–5 和阶段 7 都已完成（阶段 3、4 的小节在下方，阶段 5、7 单独成节），阶段 6 待定。用户的安排是所有功能做完后再统一自测，所以真服、真实 Key 和 Windows 真机上的验证项都还没做。各阶段的验证清单写在各自小节的末尾，已经按测试项编号汇总进 `docs/测试计划.md`。

2026-10-31 工作分支上的版本号改成了 2.9.0-beta.1，打 tag `v2.9.0-beta.1` 发成了公开的预发布版，给用户和好友统一自测用。测试的文档有三份：`docs/测试计划.md`（完整清单，写成本机的 AI 智能体或独立测试员照着就能测）、`docs/好友测试指南.md`（给进服帮忙的好友）、`docs/releases/v2.9.0-beta.1.md`（发布说明）。反馈格式在测试计划第 5 节，GitHub 上有同样格式的 Issue 模板。测试通过后合入 main，发正式版 v2.9.0。

之后版本号改成 2.9.0-beta.2，准备发第二个预发布版：比 beta.1 多了 MCP 接口和伙伴模组 0.2.1（见下文两节），发布说明在 `docs/releases/v2.9.0-beta.2.md`；测试计划补了 J 组，`site/` 和 README 也提到了 MCP。2026-10-31 用户在 Release 页面发布了 v2.9.0-beta.2（tag 在 5af555e），附件核对通过。2026-11-01 到 11-02 用户本机做了一轮实测，结论和第一批修复见下文「v2.9.0-beta.2 的实测和第一批修复」。

- **音乐共享的 SVC 桥接模组原型**（`bridge/voicechat`）：一个 jar 同时给 NeoForge 1.21.1 和 Forge 1.20.1 用，协议和逻辑在假 SVC 里跑通，还没在真服上测，详见那里的 README。分支 phase5 已把它接入应用。
- **设计文档的第一阶段「架构调整」**（2026-10-28 完成）：
  - `internal/web/web.go` 按领域拆成 13 个文件（纯搬移，单独一个提交）。
  - 新增操作层：框架在 `internal/ops`（不依赖 Windows，Linux 上可直接测试），MineDeck 的 49 个具体操作登记在 `internal/ops/catalog`。每个操作有参数说明（JSON Schema 子集）和风险等级（读 / 写 / 危险 / 仅人工）；AI 发起的写操作只生成提案，人确认后按存下的参数原样执行（危险操作要二次确认，过期或前置条件变了就作废）；读操作以外的都记进 `logs/ops/<年-月>.jsonl`（选择文件夹、打开目录除外，失败的也记，密钥写成 `***`），新接口 `GET /api/ops/log`。网页接口改成「解析请求 → ops.Do → 写回响应」，47 个旧接口的路径、方法、返回格式和状态码不变；登录、SSE 控制台流和壁纸不走操作层，整合包上传只有存文件那一步留在网页层。定时任务和崩溃自动重启也各记一条（来源 schedule）。
  - 新增 `internal/secret`（Windows DPAPI，x/sys/windows 自带的函数，不用 cgo）：CurseForge API Key 加密存进 `secrets.json`，旧配置启动时自动迁移；接口不再下发 Key，改给 `cfKeySet` / `cfKeyBroken`。`tunnels.json` 的穿透凭据同样加密并迁移；自定义 frps 的 token 不再写进 frpc 配置文件，改走环境变量（见下文「和设计文档的出入」第 4 条）；旧版留下的带明文 token 的配置文件启动时删掉。
  - 新增 `internal/mclog`：进服、退服只对拆出前缀后的正文做锚定匹配，修掉「聊天里打一句 X joined the game 就能伪造进服」；能拆出聊天的发送者和正文，给第四阶段用。
  - 前端：导入页的提示改看 `cfKeySet`；设置页的 CurseForge Key 输入框改成只写不读（已设置时留空、占位写「已设置（留空不修改）」，加「清除」，解不开时提示重填）；隧道凭据解不开时这一行写明原因。仿真后端和测试同步。
  - release 版 exe 从 11,021,312 字节增加到 11,296,768 字节（约 10.8 MB）。
- **设计文档的第二阶段「AI 接入层」**（2026-10-28 完成）：
  - 新包 `internal/ai`：不碰 Windows API，Linux 上能直接测试；不用任何服务商的 SDK。
    - OpenAI 兼容和 Claude 两种协议都自己发 HTTP、自己解析 SSE。
    - 支持普通对话、流式和模型列表。工具调用统一成 `Tool` / `ToolCall` / `ToolResult`；用量统一成输入、输出、缓存命中、缓存写入、思考五项。
    - 会话 `Conversation` 只追加不改写。回复的原生内容原样存下（包括思考块和服务端回退留下的块），保存再读回也一字不差。
  - 7 个服务商预设加「自定义」，在 `presets.go`，只是填写模板。地址和要点照设计文档 §2.1、§4.2 填；标「摘要」的，在代码注释、界面说明和下文里都照样标着。
  - 费用估算，在 `prices.go`：
    - 运行时取 LiteLLM 的公开价格，经 `dl.GHMirrors` 下载。这是 GitHub 加速镜像列表，从 update 包收进了 dl 包，自动更新和下载 frp 也用它。
    - 下载的内容严格校验后缓存到 `data/ai/prices.json`，7 天刷新一次。
    - 预设和 LiteLLM 服务商名有一张对照表。查不到价格时只记 token。
    - 用户可以自填单价（元或美元，不做汇率换算），自填的优先。
  - 记账和额度，在 `usage.go`：`data/ai/usage.json` 按本地日期记各功能的用量和估算费用，保留 400 天。助手和游戏内 AI 各有每日 token 额度。
  - `ai.Service`（`service.go`）是对外的门面，管设置、Key、客户端、测试连接、模型列表、价格、记账和额度。
    - 设置存在 `data/ai/settings.json`，不含 Key。Key 用 DPAPI 加密，存在 `secrets.json` 的 `ai.key`。
    - 它在 `main` 里建好（`catalog.NewAI`），启动时调 `Start`。没配置 AI 时什么也不做，不联网。
  - 新增 7 个接口 `/api/ai/*`，都走操作层，登记成「仅人工」的操作；修改类的「仅本机」。详见 `docs/API.md`。
  - 设置页新增分区「AI（可选）」，仿真后端和交互测试同步（测试共 39 条）。界面新用到「¥」，所以重新生成了中文字体子集。
  - release 版 exe 从 11,296,768 字节增加到 11,601,408 字节（约 11.1 MB），多了 304,640 字节（约 297.5 KB）。

#### 第一阶段和设计文档的出入

都以代码为准，原因如下：

1. **包结构**：设计文档只列了 `ops/`。框架放在 `internal/ops`，具体操作放在子包 `internal/ops/catalog`：框架不碰 Windows API、能在 Linux 上测；`inst` 要把定时任务报给操作层，具体操作又要引用 `inst`，分开才不循环引用（`inst` 只暴露一个回调 `SetAutoHook`）。
2. **操作名**：按 §3.2 用带领域前缀的点号写法（`instance.props.set`，给 AI 的工具名是 `instance_props_set`），没用 §5.2 表里的短名（`props_set`、`player_op`）。玩家操作按风险拆成 7 个（加减白名单是写，op / deop / 踢人 / 封禁 / 解封是危险）。
3. **权限细节**：`Caller` 多了 `Local`（发起的人是否在这台电脑上，局域网打开的助手会话要用它判断「仅本机」）；`Op` 多了 `LocalMsg`（保留原来 403 的提示原文）、`InGame`（游戏内 AI 和 AI 伙伴只能调明确开放的读操作，目前一个都没开放，比设计稿的「只开放少数几个」保守）、`NoLog`（选择文件夹、打开目录不记）。`Do` 先判权限再校验参数（设计稿的顺序是先校验），这样仅本机的接口仍是先报 403。确认提案时：只认人；仅本机的操作要本机的人确认；危险操作要带 `ack`；过期报 410，前置条件变了报 409。
4. **自定义 frps 的 token**：frp 0.61.1 用 `html/template` 渲染配置文件（`pkg/config/load.go`），原样代入环境变量会把 `+ & " ' < >` 转成 HTML 实体，token 就对不上。所以环境变量 `MINEDECK_FRP_TOKEN` 里放的是逐字写成 TOML `\uXXXX` 转义的 token，配置第一行写 `auth.token = "{{ .Envs.MINEDECK_FRP_TOKEN }}"`（放在第一行，免得前面用户填的内容改变转义上下文）。用官方 0.61.1 的 frps / frpc（含 Windows 版 frpc.exe，在 wine 下）实测：token 含这些符号、中文和 emoji 都能登录。
5. **进服、退服识别**：除了设计稿的「joined the game」「left the game」，还认 PlayerList 的「X[/地址] logged in with entity id …」和「X lost connection: …」：它们写的是账号名，不受队伍前缀影响，插件关掉进服提示时也照样有；两种都出现时事件只推一次。进服退服的名字放宽到 1–16 个不含空白和 `< > [ ]` 的字符（设计稿是 `[A-Za-z0-9_]{1,16}`），因为离线模式允许 `Mr-X` 这类名字、Floodgate 的基岩版玩家带 `.` 前缀，按设计稿的写法这些人会从在线人数里消失；放宽不影响防伪造（聊天、/me、/say 的正文都带空格或以 `<` 开头）。聊天的发送者仍只认 `[A-Za-z0-9_]{1,16}`。Forge 格式下进服退服认任何 `minecraft/…` 记录器，聊天只认 MinecraftServer / DedicatedServer。聊天发送者必须在线、名字规则放宽这两条留给第四阶段；在线名单里可能出现 `@a` 这类名字，到时按 §6.1 先丢掉以 `@` 开头和含 `" \ §` 的名字。
6. **日志样本**：`internal/mclog/testdata/logs` 的五份样本是按各端 log4j2 配置的格式手写的，不是真服实录（这次在云端起不了服务端）。原版的格式和几句英文已对照 1.21.1 官方服务端 jar 里的 log4j2.xml、语言文件和日志格式串核对过。
7. **接口细节**：`/api/app` 除了 `cfKeySet` 还给 `cfKeyBroken`（设计稿只写了界面提示）；隧道列表多了 `credBroken`。壁纸的列表和图片都不走操作层（设计稿只点了「壁纸图片」）。操作记录多了 `session`、`err` 两个字段，查询多了 `op` 过滤；Webhook 地址和 server.properties 里像密码的项（`rcon.password` 等）也写成 `***`。来源 `ui` 和 `api` 靠请求头 `Sec-Fetch-Site: same-origin` 区分（浏览器和 WebView2 里的界面会带）；局域网的 `actor` 是设备地址，因为所有局域网登录共用一个会话值，没有会话编号。
8. **CurseForge Key 输入框**：设计稿 §3.5 / §14 第 14 项原定跟界面重做一起改，界面 v5 已合入，所以这次一起改了。只填空白不会被当成清除。
9. `web.go` 拆分前是 1218 行（设计稿写的是 1172 行）。「47 个接口」= 46 个 `/api/` 路由加 `GET /bg/{name}`。

#### 第二阶段和设计文档的出入

都以代码为准，原因如下：

1. **注册表没挪**：操作层的注册表仍在 `web.New` 里建。这一阶段只有设置接口要用 AI，不需要挪；`ai.Service` 在 `main` 里建好后传进 `web.New`。
2. **AI 设置全部「仅人工」，局域网能看**：7 个 `ai.*` 操作都是「仅人工」，连 `ai.settings.get` 也是，以后的 AI 功能看不到也改不了自己的设置。修改类的「仅本机」，局域网登录的设备只能看设置页，而且看不到 Key。设计稿 §10.3 只说设置「仅本机」，没说能不能看。
3. **接口不全是「未启用」**：§4.5 写的是没配置时 `/api/ai/*` 返回「未启用」。实际上设置页本身要用这些接口：
   - `GET /api/ai/settings` 照常返回，带 `enabled: false` 和 `missing`（还差什么）。
   - 测试连接和刷新价格返回 400「AI 未启用……」。
   - 取模型列表在没配置时也能用。Kimi、智谱、OpenAI 没有建议模型，要先取列表才能配完，所以只要人在设置页里点「获取模型列表」，就用表单里还没保存的地址发出请求。这是没配置时唯一可能的对外连接，而且只在人点了之后发；没有任何自动的或后台的请求。
   - Key 不会发到别的地址：不传 Key 时，只有服务商、协议和地址都和已保存的一样，才用已存的 Key。
4. **AI 是设置页的一个分区**，不是 §4.5 说的「默认折叠的区块」：v5 的设置页按分区切换，不点进去就看不到，效果相同。
5. **额度按全部 token 算**：输入、输出、缓存命中和缓存写入都算（思考已算在输出里）。设计稿没说怎么算，这样最好理解。缺点是缓存命中便宜却按全额计入，以后有需要可以改成只算输入和输出。额度在请求前检查，一次回复不会中途截断，所以当天的用量可能略微超过额度。
6. **测试连接不计入额度**，也不记账。界面上写明了「花一点点 token，不计入额度」。
7. **Claude 服务端回退的用量**：按 `usage.iterations` 逐次累加，每次尝试按各自模型的价格算，和 §4.4 一致。
8. **自定义预设可以不填 Key**：有的中转和本机服务不需要认证。其余预设除 Ollama 外都必须填 Key。
9. **逐家兼容的两处默认做法**（都是「摘要」，要实测）：
   - Kimi 的工具结果消息带 `name`。
   - DeepSeek 的回复把 `reasoning_content` 原样带回。§4.2 说思考模式下工具循环可能需要这样做。
10. **空回复不能追加进历史**：Claude 返回空的 `end_turn`，或者 OpenAI 兼容的服务只返回思考内容时，`AddResult` 会报错。Claude 不接受内容为空的 assistant 消息，所以不能硬塞。第三阶段要处理这种情况，比如提示「模型没有给出回复」，再 `UndoUser` 让用户重问。
11. **代理只认环境变量**：OpenAI 的预设要代理才连得上时，读的是 `HTTPS_PROXY` 等环境变量（`http.ProxyFromEnvironment`）。设置页里没有单独的代理设置。
12. **价格文件的地址已核实**：§4.4 标的是推断。2026-10-28 从云端打开过 `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`，共 2,967,643 字节；ghfast.top 和 gh-proxy.com 也都能转发它。但从国内访问这两个镜像还没测过。
13. **测试用的价格样本**：`internal/ai/testdata/litellm/` 里是从 LiteLLM 1.106.5 自带的价格文件中原样摘出的 27 条（32 KB），旁边附上它的 MIT 许可证。其中 3 条是解析时要跳过的：`sample_spec`、一个嵌入模型、一个画图模型。写坏的数据在测试里现场构造。

#### 第三、四阶段开始前要知道的事

- **怎么用 AI 接入层**：
  - 每次请求前先调 `svc.Begin(功能)`。没配置时报 `ai.ErrDisabled`，额度用完报 `*ai.QuotaError`（错误信息可以直接给用户看）。
  - 用 `svc.Client(true)` 按当前设置建客户端，用 `cl.NewConversation(系统提示, 工具)` 开会话，再用 `cl.Chat(ctx, conv, onEvent)` 发请求。
  - 拿到 `Result` 后，不管成没成功都要 `svc.Record(功能, res)` 记账。这一步返回估算的费用，查不到价格时为 nil。
  - 回复完整的，用 `conv.AddResult(res)` 追加进历史；有工具调用的，把全部结果按顺序用 `conv.AddToolResults` 一次交回。
  - 请求出错、被拒答或用户停止后，用 `conv.UndoUser()` 撤回那条提问。
  - 会话用 `conv.Save` / `ai.LoadConversation` 保存和读回。
- **会话的系统提示和工具清单定下就不改**。Claude 的思考块要求历史前缀一字不差，所以系统提示里不要放时间戳。服务商、协议或模型改了，就开新会话（`Chat` 发现协议对不上会报错）。
- **工具名和参数**：工具名用 `ops.ToolName`（只能是字母、数字、`_` 和 `-`，最长 64 个字符），参数说明用 `op.Params` 编成的 JSON。
  - 运行工具前先看 `res.Stop`：`max_tokens` 和 `refusal` 时不执行。
  - `ToolCall.Bad` 表示参数不是完整的 JSON 对象，可能是流式时被截断了。这时不执行，交回一个 `IsError` 的结果，并说明参数格式不对。
- **流式事件**：`EvText`、`EvReasoning`、`EvToolStart` / `EvToolDelta` / `EvToolDone`、`EvUsage`、`EvFallback`（Claude 换了模型）、`EvRetry`（正在重试）。可以直接转成给前端的 SSE。
- **额度的功能**：`ai.QuotaFeatures` 现在只有 `assistant` 和 `gamechat`。`companion` 已经能记账，但还没有额度，第七阶段再加。
- **测试连接的结果**：`TestResult.Tools` 为 `no` 时，这个模型不能调用工具，助手要提示换模型。
- **第一阶段留下的、仍然要做的事**：
  - 提案还没有 HTTP 接口。第三阶段要加列出、确认（危险操作带 `ack`）、拒绝三个接口。确认后用提出者的 `Caller` 执行，记录里 `src` 是 assistant，`approvedBy` 是确认的人。提案的生成和拒绝不写操作记录，需要的话再加。
  - 助手会话的 `Caller.Local` 要按创建会话的那个请求是否来自本机（`isLoopbackReq`）来设。
  - 注册表要和助手共用（提案存在里面），到时可以改成在 `main` 里建好再传进去。
  - 游戏内 AI 现在没有任何工具。第四阶段的 `game_info` 要设 `InGame: true`。
  - §5.1 说 EULA 必须由用户自己勾。现在 `instance.create` 照参数执行，接确认卡片时要让 `eula` 只来自人的确认。
  - 读操作的结果会原样交给 AI。`instance.props.get` 里有 `rcon.password`，`app.info` 里有程序目录（含 Windows 用户名）和局域网 IP，要按 §5.3 的脱敏规则处理后再发。
  - §10.3 的「局域网可用 AI 助手」开关（默认开）还没做，属于第三阶段。

#### 第三阶段「应用内 AI 助手」（2026-10-28 完成）

同一个工作分支。上一节「第一阶段留下的、仍然要做的事」除游戏内 AI 那条外都做完了。

- **新包 `internal/assistant`**（不碰 Windows API，Linux 上能直接测，也能加 `-race`）：
  - 会话：本机保存最近 20 个，存在 `data/ai/sessions/<id>.json`，重启后读回，接着聊时发给 AI 的历史逐字节不变。会话的 `Caller.Local` 按开会话的那个请求定，局域网开的会话只有局域网的设备看得见。
  - 工具映射：按会话的调用者从注册表取（`Registry.Available`），去掉 `app.update.check`、`java.scan`、`ops.log` 三个对排错没用的。读操作直接执行；写操作和危险操作由操作层生成提案，界面出确认卡片。
  - 提案流程：操作层新增 `OnResolve` 通知（确认后执行完、执行失败、拒绝、过期、作废），助手据此把结果交还给 AI；这一轮的卡片都处理完了就自动接着回复。没处理就发新消息时，没处理的提案经操作层拒绝作废。
  - 上下文打包（§5.3）和脱敏（`Redactor`），发送预览；回复在后台跑，经 SSE 推给界面，HTTP 请求断开不影响。
  - 空回复、拒答、停止、出错时撤回提问（`UndoUser`），界面把原话放回输入框。
- **新包 `internal/diag`**：13 类报错规则和报错横幅的数据来源，另有 `jvm-option`（JVM 参数不被支持）和兜底的 `crash`。样本在 `internal/diag/testdata`：25 份正例覆盖 13 类加 `jvm-option`，有 NeoForge、Forge、Fabric、Paper 几种格式；5 份反例是正常运行的五种核心，里面有玩家在聊天里、`/say` 里贴的报错原文。**新包 `internal/modinfo`**：解析 jar 里的 `mods.toml`、`neoforge.mods.toml`、`fabric.mod.json`、`quilt.mod.json`、`plugin.yml`（模组 ID、版本、依赖、mixin 配置、客户端或服务端），按文件大小和修改时间缓存。
- **操作层新增 12 个操作**：读 `instance.console.tail`、`instance.logs.read`、`instance.crashes`、`instance.diag`、`instance.mods.list`、`instance.config.read`、`port.check`；写 `instance.java.set`、`instance.config.write`、`instance.mods.toggle`；危险 `instance.restart`；仅人工 `ai.assistant.set`。`app.info` 多了 `os`（Windows 版本），实例列表多了 `javaSource`（便携版还是本机的）。改配置文件时，写回保持原文件的编码（GBK、带 BOM 的 UTF-8）和换行，备份存原文件的字节。
- **操作层的其他改动**：`Op.HumanParams` 是 AI 看不到、也填不了的参数（`instance.create` 的 `eula` 和 `root`）；`Op.Consent` 是只能由人在确认时给的同意（EULA，确认接口带 `consent`）；`Plan.Diff` 是文本文件的逐行对比（`ops.LineDiff`）；提案的提出、拒绝、作废也写操作记录（`event` 和 `reason` 两个新字段）。`Reject` 多了理由参数，确认多了 `ConfirmWith`。
- **ai 包**：`AddUser` 可以跟在工具结果后面（停止、作废、空回复之后用户接着说话）；新增 `NeedsReply`、`Current`、`Usage`、「局域网可用 AI 助手」开关（`LANAssistant`，默认开）。
- **注册表移到 main**：`web.New(mgr, tun, reg, asst)`，网页接口和助手共用一个注册表（提案存在里面）。新增 27 个接口，见 `docs/API.md` 的三个「第三阶段」小节。
- **界面**：新文件 `internal/web/ui/assistant.js` / `assistant.css`，`app.js` 只加了 5 处调用和 2 个占位元素。标题栏右上角「AI 助手」打开面板（不占 1–4，Esc 关闭）：会话列表、流式回复、停止和继续、每条回复的用量、额度提示、确认卡片（危险操作要多勾一次、EULA 只能人勾、拒绝可写理由）、查看实际发给 AI 的内容。控制台顶部的报错横幅不依赖 AI，配置了 AI 才多「让 AI 分析」，点了先给发送预览。「设置 → AI」多了「应用内助手」分区：局域网开关、清空会话。AI 回复用自己写的 Markdown 渲染器，先整体转义 HTML。没有新的 GB2312 以外的字，字体子集不用重新生成（`--check` 通过）。
- **测试**：Go 侧新增 assistant 包的 19 个测试（覆盖 §12 第 3 行的验收标准）、diag、modinfo、`ops/catalog/phase3_test.go`、`web/assistant_test.go`（27 个新接口逐个调一遍、本机会话的提案流程、局域网的可见范围和开关）。前端交互测试 44 条（这一阶段加了 5 条），仿真后端和测试里新增的部分各集中在一块「第三阶段」。
- release 版 exe 从 11,601,408 字节增加到 12,270,592 字节（约 11.7 MB），多了 669,184 字节（约 653.5 KB）。

#### 第三阶段和设计文档的出入

都以代码为准，原因如下：

1. **操作名**：沿用第一阶段的点号写法。`console_tail` → `instance.console.tail`，`logfile_read` → `instance.logs.read`，`crash_reports` → `instance.crashes`，`mods_list` → `instance.mods.list`，`config_read` / `config_write` → `instance.config.read` / `.write`，`port_check` → `port.check`，`java_set` → `instance.java.set`，`mod_toggle` → `instance.mods.toggle`，`instance_restart` → `instance.restart`。另加了 `instance.diag`，让 AI 也拿得到本地规则的结论。
2. **风险等级**：重启是危险操作（在线的人会掉线），换 Java、改配置、禁用模组是写操作。换 Java 时本机没有这个版本，就在后台下载便携版，返回任务编号。
3. **面板不是「临时面板」**：做成了标题栏入口的侧边浮层，手机宽度占满。§9 的各项都有，「发送内容预览」分两处：「让 AI 分析」发送前的预览，以及每条消息上「查看实际发送的内容」。
4. **横幅**：在实例的控制台标签里显示。打开控制台时跑一次本地规则；之后出现报错行、启动或退出时，防抖后再跑，不是后端推送。实例卡片上没有横幅。关掉横幅只对这一次的问题有效（换了问题或者重新启动后会再出现）。
5. **规则**：13 类之外，`jvm-option` 从 §5.3 第 7 条里单列出来，`crash` 是兜底（异常退出但没认出原因）。「好友进不来」（`client-mismatch`）是提醒级别，不算崩溃。「配置文件被改坏」§5.3 写的是原文待收集，现在按 Forge / NeoForge 的「Failed loading config file … for modid …」、Night Config 的 `ParsingException` 和「Invalid TOML」这几种写法来认，真服上的原文还要再收集。样本是按各端的日志格式手写的，不是真服实录。
6. **脱敏比 §5.3 多做了几样**：
   - 计算机名、程序目录和用户目录也换成占位符；
   - 本机各网卡的地址都换掉，哪怕是公网地址；IPv6 的 ULA、链路本地地址和 CGNAT 地址也算局域网 IP；
   - 配置里像密码的值，以及 JSON 里键名像密钥的值，写成 `***`。
   AI 照着占位符写回配置文件会被拒绝。
7. **看、删会话不要求 AI 配好**：停用 AI 之后，历史照样能看、能清空。
8. **一轮最多 8 次工具调用**：到上限后的调用不执行，交回「到上限了」的说明，再请求一次让 AI 收尾（一轮最多请求 11 次）。超过 16 KB 的工具结果，带 `lines` 的从最旧的行删起，其余的截掉后面。
9. **AI 设置改了**：换了服务商、协议或地址，或者 Claude 换了模型，旧会话不能接着用，要开新会话。OpenAI 兼容的服务换模型可以接着用。
10. **局域网开关关掉后**，局域网的设备看不到入口、会话，也处理不了提案；本机不受影响。开关打开时，局域网开的会话里的提案由那台设备上的人自己确认，主机本机也能处理。

#### 第三阶段要用真实 Key 验证的

云端只用 httptest 起的假服务测过，下面这些要在用户的电脑上用真实的 Key 走一遍：

- **至少两家**（比如 DeepSeek 和 Claude，或者再加通义）：
  - 问「服务器为什么起不来」，看 AI 先调读工具再回答，读操作不弹确认；
  - 让它把人数改成 10，确认卡片出现，确认后 server.properties 改对；
  - 拒绝时写个理由，下一次回复里应该提到它；
  - 回复中途点停止，再接着说话，不报错。
- **Claude**：
  - 停止或作废之后接着说话时，会出现两条连续的 user 消息，确认服务商接受（应该会合并）；
  - 工具循环里思考块原样带回，不报错；
  - 换了模型后，旧会话提示开新会话。
- **DeepSeek 的思考模式**：工具循环里带回 `reasoning_content` 不报错；只给思考内容、没有正文时，提示「模型没有给出回复」。
- **其他 OpenAI 兼容的服务**（Kimi、智谱、通义）：流式的工具调用分片能拼对，工具结果能交回。
- **真服排错**：NeoForge 1.21.1 和 Forge 1.20.1 上分别故意弄出几种错误，看横幅能不能认出来，「让 AI 分析」给出的提案对不对：
  - 少装前置模组；
  - 把小地图放进服务端；
  - 端口被占；
  - Java 版本不对。
  JVM 自己崩溃时，hs_err 的摘要也看一下。
- **发送预览**：在自己的电脑上看一眼，预览里没有 Windows 用户名、局域网 IP 和 Key。
- **端口占用**：能不能查到占用端口的程序名，要不要管理员权限。
- **配置文件**：GBK 编码、带 BOM、Windows 换行的配置文件改过之后，编码和换行应保持原样（测试里覆盖了），「撤销」能恢复；改完后模组或插件照常读得进去。
- **局域网手机**：开关开着能用助手；关掉后没有入口；手机上开的会话，主机上也看得见。
- `app.info` 的 Windows 版本对不对（用的是 `RtlGetVersion`）。

#### 第四阶段「游戏内聊天 AI」（2026-10-28 完成）

同一个工作分支，在第三、五阶段合并之后做的。上文「第一阶段留下的、仍然要做的事」里游戏内 AI 那条（`game_info` 要设 `InGame: true`）也做了。

- **新包 `internal/chatai`**：不碰 Windows API，Linux 上能直接测，也能加 `-race`。实例那一侧（控制台、标准输入、玩家名单）由 `Backend` 接口注入。
  - 认聊天（`recognize.go`）：用 `mclog` 拆出聊天的发送者和正文，再认触发前缀；说话的人要在线。实例可以放宽名字规则、不看线程名，或者填自定义正则（命名分组 `name` 和 `msg`）。
  - 提问选项（`ask.go`）：`/私`、`/问`、`/额度`、`/帮助` 和英文别名，也认全角的 `／`、顿号 `、` 和全角的 `＠`。
  - 命令白名单（`command.go`）：10 个模板，`gamemode`、`give`、`difficulty`、`seed` 默认关；禁止清单写死；每个参数按模板校验，「自己」由程序换成提问的人。
  - 回复（`tellraw.go`）：只用 `text`、`color`、`extra`；非 ASCII 字符写成 `\uXXXX`，整条命令只有 ASCII；每条最多 180 个字，最多 3 条。
  - 处理（`chatai.go`、`answer.go`、`prompt.go`）：
    - 服务器的每行输出经 `OnLine` 交进来。它在读输出的协程里同步调用，只做识别和入队。
    - 有人触发了，才给这个实例起两个协程：一个按顺序处理触发（防刷屏、限流、系统提示），一个按顺序回答（同一时间 1 个，最多排队 3 个）。
    - 每个问题一次新的对话，调 AI 接入层的顺序同应用内助手：`Begin(ai.FeatGameChat)` → `Client` → `Chat` → `Record` → `AddResult` / `AddToolResults`。空回复和只有空白的回复都按「模型没有给出回复」处理，私下告诉提问的人。
  - 设置和状态（`settings.go`、`state.go`、`view.go`）：设置存在实例目录的 `gameai.json`；当天的次数和 token、最近来过的名字、最近 100 条问答存在 `gameai-state.json`。写盘都在锁外。
- **mclog**：新增 `ParseWith(line, ChatOptions{LooseNames, AnyThread})`，`Parse` 的行为不变。进服、退服的识别不受这两个放宽项影响。
- **inst**：
  - `SetLineHook`：游戏内 AI 从这里拿到服务器的每行输出，在线名单按这一行更新之后才调。现在只能登记一个回调，第七阶段的 AI 伙伴如果也要看聊天，要改成能登记多个。
  - `CommandEcho`：写命令时可以指定控制台里的回显。tellraw 里的 `\u` 转义人看不懂，控制台里改写成 `[MineDeck] 游戏内 AI 回复（全服可见）：…`。
- **操作层**（`internal/ops/catalog/gamechat.go`）登记了 6 个操作：
  - 读：`game.info`，`InGame: true`，是游戏内 AI 唯一能调的操作。
  - 仅人工：`gameai.get`、`gameai.set`、`gameai.test`、`gameai.log`、`gameai.whitelist`，其中 `set` 和 `whitelist` 还「仅本机」。
  - `gameai.ask`、`gameai.command` 这两种操作记录由 chatai 直接写（`Registry.Record`），不是登记的操作。
  - 其他改动：`assistant.Redactor` 的构造抽成了 `catalog.NewRedactor`，助手和游戏内 AI 共用；`catalog.Env` 多了 `GameChat`、`Token`；`main.go` 只多传了 `Token: web.Token`，`web.New` 没改；助手的工具清单去掉了 `game.info`。
- **网页接口**：`internal/web/gamechat.go` 里 5 个接口，`server.go` 只加了一行 `s.gameChatRoutes(mux)`。详见 `docs/API.md` 的「游戏内 AI（第四阶段）」。
- **界面**：实例详情的「游戏内 AI」标签在新文件 `internal/web/ui/gamechat.js` 和 `gamechat.css` 里，app.js 只加了标签和分派两处钩子，index.html 引入这两个文件。
  - 内容按 §9 第 4 条的顺序：开关、触发前缀和别名；能让 AI 执行命令的玩家；命令模板；限流和额度；最近的问答和执行记录；识别测试；高级识别（默认收起）。
  - 授权了人、服务器却没开白名单时，提醒冒名的风险，给「一键开启白名单」。
  - 局域网打开时只能看、能做识别测试，控件禁用。
  - 界面用到的字都在现有的字体子集里，`build_fonts.py --check` 通过。
- **测试**：
  - chatai 的 28 个测试，在 Linux 上直接跑，`-race -count=5` 通过。覆盖 §12 第 4 行能在云端测的验收标准：
    - §6.2 的 20 条样例；
    - 五类核心的日志样本（在 `internal/chatai/testdata/logs/`），含 `[Not Secure]`、伪造、格式码、全角 ＠、线程或记录器不对；
    - 回复默认全服可见，`/私` 只回提问的人；
    - `/问` 时请求里没有 `run_command`；`/帮助`、`/额度`、未知选项不调用 AI、不扣次数；
    - 没授权的人，请求里没有执行命令的工具；授权的人说「把天气改晴」，执行 `weather clear`；
    - 模型输出 op、execute 等也不执行；绕过白名单的写法（命名空间、选择器、大小写、全角、超长、换行、以 `@` 开头的名字）都被拒；
    - 刷屏被静默；冷却、每人每天、全服每天、token 上限，过 0 点清零；排队满了回复「正忙」；排队时被收回授权的不执行。
  - ops/catalog：
    - 端到端测试 `TestGameChatEndToEnd`：用真的 `inst.Manager`，把测试程序自己当成 java，起一个假的服务器进程。它的控制台出现一句聊天 → 认出提问 → 假的 AI 要执行 `weather clear` → 写进标准输入 → 服务器的输出交还给 AI → 回复的 tellraw 写进标准输入。逐字节检查标准输入里只有 ASCII，也检查控制台的回显和操作记录。另外测了服务器运行中一键开启白名单时命令的先后。
    - `TestGameInfo`：游戏内调别的操作一律 403。`TestGameAIOps`：局域网改设置 403；服务器停着时，一键开启白名单直接改文件。
  - web：`TestGameChatRoutes` 把 5 个接口各调一遍，检查响应里搜不到密钥。
  - 前端交互测试共 57 条，这一阶段加了 6 条。仿真后端和测试里新增的部分各集中在一块「第四阶段」。
  - wine 下有测试的 22 个包全部通过。下文「测试和发版」里的包清单还没加上 chatai（第三、五阶段的 assistant、diag、modinfo、bridge、musicshare 也还没加）。
- release 版 exe 从 12,829,184 字节（7de1ba2，第三、五阶段合并之后）增加到 13,104,640 字节（约 12.5 MB），多了 275,456 字节（约 269 KB）。

#### 第四阶段和设计文档的出入

都以代码为准，原因如下：

1. **回复里的中文写成 `\uXXXX`**：§6.3 和 §2.3 原来说回复里不用 `\u` 这类转义。用户定了改成：JSON 文本里所有非 ASCII 的字都写成 `\uXXXX`，超出 BMP 的写成代理对，整条命令只有 ASCII。原因如下：
   - 核实过 MineDeck 写标准输入时不转码，Go 的字符串原样写出，就是 UTF-8。实例的「控制台编码」只管怎么读输出。
   - 服务器按什么编码读标准输入，取决于核心和 Java 的设置；整条命令只有 ASCII 时，在任何编码下读出来都一样。
   - 1.21.4 及以前，文本组件用 Gson 解析，认 `\uXXXX`。1.21.5 起改用 SNBT，SNBT 带引号的字符串也认 `\uXXXX`（推断，要实测）。
   - 换行、控制字符和 `§` 仍然去掉，不用 `\n`。
2. **放宽名字规则后也只认 ASCII**：§6.1 第 4 步放宽成 `[^\s<>]{1,16}`，再丢掉以 `@` 开头、含 `"`、`\`、`§` 的名字。实际放宽成 1–16 个可打印的 ASCII 字符（不含空格和 `< >`），再丢掉以 `@` 开头、含 `"`、`\` 的。中文名不认，因为名字要写进 tellraw 的目标和命令模板里的「自己」，而命令要保证整条只有 ASCII。
3. **AI 要执行的命令也只能是 ASCII**：§6.6 校验的第 1 步之外，命令里有中文或全角字符也拒绝。模板里的参数（ID、数字、坐标、名字）本来都是 ASCII。
4. **隐私提示的时机**：§10.1 说「游戏内第一次有人触发时」告诉大家一次。实际是每次开服后、受理第一个提问之前，全服发一次（灰色）。`/帮助`、`/额度` 这类不调用 AI 的不算，因为它们不会把任何东西发给服务商。重新开服后再发一次，因为在线的可能换了一批人。
5. **次数怎么算**：§6.7 没说没答上的算不算。实际上受理一个提问就算一次、开始冷却；AI 出错、超时、空回复也算，花掉的 token 照实记，免得服务商出问题时玩家一直重问。
   - 全服每天的 token 上限按实例算，键名叫 `budget`：操作记录会把键名里带 token 的值当成密钥写成 `***`。
   - 「设置 → AI」里游戏内 AI 的每日额度另外管所有实例的合计，两个都满足才问 AI。
6. **禁止清单多了几类**：设计稿的清单都在，另外加了三类：
   - 同样危险的：`place`、`fillbiome`、`loot`、`damage`、`setworldspawn`、`defaultgamemode`、`return`；
   - 会冒充别人说话的：`tellraw`、`title`、`say`、`me`、`msg`、`tell`、`w`、`teammsg`、`tm`、`bossbar`、`trigger`；
   - 各家加载器和插件端的命令：`forge`、`neoforge`、`fabric`、`bukkit`、`paper`、`plugins`、`version` 等。
   不在模板里的命令本来就不执行，这张表让拒绝的原因说清楚，也防止以后加模板时误放。模板也多认两种写法：`teleport`（同 `tp`）和旧版的 `locatebiome`。
7. **每个问题的上限**：设计稿没写。一个问题最多请求 4 次 AI、调用 6 次工具、执行 3 条命令，2 分钟没答完按超时处理。执行命令后收集控制台输出交给 AI（2026-11 起收到就返回：普通命令最多等 2 秒，locate 一类最多等 5 秒、只认它自己的反馈，见设计文档 §6.6 第 6 步），但去掉聊天行（不把别人的聊天交给 AI，§6.4）和 MineDeck 自己的行，最多 2000 字节，先脱敏。
8. **一键开启白名单**：服务器运行中时，除了照 §6.5 发 `whitelist add` 和 `whitelist on`，还先把 server.properties 的 `white-list` 改成 true，不依赖 `whitelist on` 会不会写回这个文件（§6.5 标了要实测）。先改文件再发命令，免得两边同时写。名字只收 1–16 位字母、数字、下划线（复用 `PlayerAction`），带 `-` 这类字符的名字，界面上单独列出来说明加不进去。
9. **整个设置都仅本机**：§10.3 说授权名单和命令模板仅本机。实际上开关、前缀、限流、高级识别和一键开启白名单也都仅本机；局域网能看设置和最近的问答，能做识别测试。
10. **样本的位置**：§11 写的是 `testdata/logs/{forge-1.20.1,neoforge-1.21.1,vanilla,paper,fabric}/` 这几个目录。实际在 `internal/chatai/testdata/logs/` 下，每类一个文件：`forge-1.20.1.log`、`neoforge-1.21.1.log`、`vanilla-1.21.1.log`、`paper-1.21.4.log`、`fabric-1.20.1.log`。样本是按各端的日志格式手写的，不是真服实录。
11. **工具名**：`game_info` 在操作层里叫 `game.info`（沿用点号写法），给 AI 的工具名仍是 `game_info`。`run_command` 不是操作层的操作：游戏内调用者的写操作在操作层一律 403，命令按 §6.6 走 chatai 自己的白名单，每条记成一条 `gameai.command`。
12. **「最近来过的名字」**：从进服的日志和提问里记下，最多 50 个，用来列可以授权的人。打开了游戏内 AI 才写盘；没打开时只记在内存里，重启 MineDeck 就没了。

#### 第四阶段要在真服上、用真实 Key 验证的

云端只用假的服务器进程和 httptest 起的假 AI 测过，下面这些要在用户的电脑上走一遍：

- **真服问答**：NeoForge 1.21.1 和 Forge 1.20.1（好友的整合包）各开一台，在「游戏内 AI」标签里打开开关。
  - 在游戏里说 `@AI 今晚怎么打末影龙`：回复全服都看得到，开头是 `[AI→名字]`，中文不乱码。第一次提问前，全服先收到一条「提问会发给……处理」。
  - `@AI /私 …`：只有自己看得到，换个号确认别人收不到。
  - `/帮助`、`/额度`、`@AI /tp 怎么用`、`@AI 今晚怎么打末影龙 /私`：都只私下回复，不调用 AI，「最近的问答」里没有用量。
  - 中文、全角标点、emoji（超出 BMP 的字）都显示正常。回复超过 180 个字时拆成几条，最后一条带「完整回答请在 MineDeck 面板查看」。
  - MineDeck 的控制台里看得到 `[MineDeck] 游戏内 AI 回复……` 这样的回显，看不到 `\u` 转义。
- **1.21.5 以后的版本**（手边有的话，比如 1.21.8 或更新）：tellraw 里的 `\uXXXX` 能被 SNBT 认出来，中文显示正常，不报错。这一条是推断，最要紧。
- **聊天识别**：
  - 在两台真服的控制台里核对聊天行的原文和 §6.1 的格式一致：线程名、记录器、`[Not Secure]`。把一行聊天贴进「识别测试」，应认出玩家和问题。
  - 手边有 Paper、Fabric、原版、Mohist 服的话也各试一次。装了改聊天格式的插件或模组时，试一下自定义正则。
- **授权和执行命令**：
  - 没授权时说「把天气改晴」：AI 只回答，不执行。
  - 授权以后再说：执行 `weather clear`。「最近的问答」里有这条命令和服务器的输出；`logs\ops` 里有 `gameai.ask` 和 `gameai.command`，来源是 gamechat。
  - `@AI /问 把天气改晴`：有执行权限也不执行。
  - 让它「给我 op」「用 execute 执行……」「把所有人传送过来」：都不执行，AI 用中文说明原因。
  - 打开 `gamemode` 模板后，「把我改成创造模式」能执行，关掉后不能；`give` 同理，再给一个模组物品试试。
  - 各模板的写法在 1.20.1 和 1.21.1 上都被服务器接受（`effect give`、`locate structure` / `biome`、`spawnpoint` 等），看「最近的问答」里每条命令的输出。
- **一键开启白名单**：服务器运行中、白名单没开时授权一个人，界面出现提醒。点「一键开启白名单」并确认后：在线的人都还在服里，没人被踢；`whitelist.json` 里有他们，server.properties 里是 `white-list=true`。服务器停着时再试一次，开服后白名单生效。
- **真实的 Key**：至少两家（比如 DeepSeek 和通义）各问几次。
  - 问「现在几点」「谁在线」时，AI 会调 `game_info`。
  - 执行命令后，AI 根据服务器的输出回答。
  - 用量和费用记进「设置 → AI」里游戏内 AI 那一栏，和「最近的问答」里每条的用量对得上。额度用完后提示「今天的额度用完了」。
- **限流**：1 分钟内连发 6 次 `@AI /帮助`，之后 10 分钟不再回复；冷却中再问，提示还剩几秒。
- **离线模式的名字**：名字带 `-` 的玩家（比如 `Mr-X`）默认不认，打开「放宽名字规则」后能认；中文名照样不认。
- **局域网手机**：打开「游戏内 AI」标签只能看，控件都是禁用的，识别测试能用。

