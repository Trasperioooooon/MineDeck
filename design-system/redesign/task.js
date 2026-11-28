// 后台任务 / 进度页草图的脚本（11-27 夜里第一稿）。怎么看、和程序哪里不一样都写在 task.html 开头。
// 做法：每种任务一份「过程」（ops，照 Go 里 StartStep / Logf / ProgressFn 的先后排），草图参数 st 决定停在哪一帧：
//   进行中、下载慢、连不上停在 m 标记那里；失败在 fail 标记那里出错；取消在 cancel 标记那里收下取消；完成、部分完成跑到底。
//   ?play=1 不是直接画出来，而是从第一步起照这份过程一样一样往下走，走到这个 st 的结尾（失败就在那一步出错，下载慢就在那里卡一会儿再接着走）。
// 页面骨架只建一次（build），之后每一处一个小函数（paintHead、paintSteps、paintProg……），只改变了的那一处。
(() => {
  const QS = new URLSearchParams(location.search);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const esc = s => String(s ?? "").replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const IN = "cubic-bezier(.4, 0, 1, 1)", OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)", SINK = "cubic-bezier(.4, 0, .2, 1)";
  const icon = (n, c = "") => window.KIT.icon(n, c);
  const hy = el => { window.KIT.hydrate(el); return el; };
  const wait = ms => new Promise(ok => setTimeout(ok, ms));
  const MB = 1048576, CELLS = 30;
  // 同程序 fmtBytes
  const fmtBytes = n => { if (n <= 0) return ""; const u = ["B", "KB", "MB", "GB"]; let i = 0; while (n >= 1024 && i < 3) { n /= 1024; i++; } return n.toFixed(i ? 1 : 0) + " " + u[i]; };
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });
  // 去别的草图：配色参数带过去
  const link = (page, params = {}) => { const q = new URLSearchParams(); ["theme", "scheme", "accent"].forEach(k => QS.get(k) && q.set(k, QS.get(k))); Object.entries(params).forEach(([k, v]) => q.set(k, v)); const s = q.toString(); return page + (s ? "?" + s : ""); };

  // ================= 示例数据（都是编的；名字、核心、版本和首页提示浮层、共用件页、详情页的示例对上） =================
  const SRV = {
    test: { name: "测试服", core: "paper", label: "Paper", mc: "1.21.4", detail: "paper" },              // 首页浮层 t3「创建服务器 测试服（paper 1.21.4）」
    gear: { name: "机械动力：齿轮之城", core: "forge", label: "Forge", mc: "1.20.1", detail: "forge" },     // 首页浮层 t5（进行中，下载整合包文件）
    tinker: { name: "匠魂整合", core: "neoforge", label: "NeoForge", mc: "1.21.1", detail: "forge" },      // 首页浮层 t4（3 个文件待处理），文件同 commons 的 PND
    old: { name: "怀旧服", core: "paper", label: "Paper", mc: "1.16.5", detail: "paper", dir: "D:\\MC\\怀旧服" },
    friend: { name: "生存服 · 朋友", core: "paper", label: "Paper", mc: "1.21.4", detail: "paper" },      // 详情页、设置页里的那台
    handed: { name: "朋友的生存服", core: "paper", label: "Paper", mc: "1.21.4", detail: "paper", pack: "生存服-朋友-交接包-20261127-221530.mdpack" },
    sky: { name: "空岛服", core: "purpur", label: "Purpur", mc: "1.21.4", detail: "paper", zip: "空岛服务端.zip" },
  };
  const JDK21 = "MineDeck\\runtimes\\java\\jdk-21.0.5+11\\bin\\java.exe";
  const NET_HINT = "网络请求超时，请检查网络或代理后再试", DISK_HINT = "磁盘剩余空间不足，请清理磁盘空间或更换存放位置后再试";
  // 「慢」那块黄条的悬停（作者 11-29 定：按任务换字，不下载的不再说下载）。黄条程序里是「多久没有新进度」就出（app.js 约 3479 行：步骤、进度、日志 STALL_MS 没变），
  // 不管卡在哪一步；所以按卡住的那一项换：后端报了重试（T.net）或者正在下的那一项是下载（进度名以「下载」开头），用程序原话；
  // 别的照任务种类换成不提下载的说法：导出卡在打包，换地图卡在复制到暂存区，接管卡在检查文件夹、登记（接管要下 Java 时那一项是「下载 Java 8」，照旧用原话），
  // 导入交接包卡在解包；新建、导入整合包、导入服务端在下载以外卡住（Forge 安装器跑着、写配置）用最后那句通用的。只说会怎样，不许诺程序没有的行为（不自动重试、不提网络）
  const SLOW_TIP = "下载较慢或正在重试。可检查网络或代理；网络恢复后将自动继续下载，无需重新开始。"; // 程序原话
  const SLOW_TIP_OF = {
    export: "要打包的文件较多或体积较大时，打包耗时较长，请耐心等待。",
    map: "地图文件较多或体积较大时，复制至暂存区耗时较长，请耐心等待。",
    adopt: "服务器文件夹中的文件较多时，检查与登记耗时较长，请耐心等待。",
    import: "交接包中的文件较多或体积较大时，解包耗时较长，请耐心等待。",
    "": "当前步骤耗时较长，请耐心等待。",
  };
  // 程序 LEFTOVER_HINT；「服务器页」照重构以后的叫法改成「首页」（首页提示浮层里的「没建完的文件夹」）
  const LEFTOVER_HINT = "文件夹未能自动清理：会列在首页上方「未建完的文件夹」中，可前往处理。";
  const EXPORT_PATH = "D:\\MC\\交接包\\生存服-朋友-交接包-20261127-221530.mdpack";
  const CREATE_STEPS = ["解析版本信息", "准备 Java 运行时", "下载服务端核心", "安装服务端", "写入配置文件", "生成启动脚本"]; // inst/install.go createSteps

  // 新建，说法同程序 TASK_KINDS[""]
  const T_CREATE = { title: "正在创建服务器", sub: "正在下载 Java 和服务端。", done: "服务器已建好", doneSub: "Java、服务端与配置均已就绪",
    fail: "服务器创建失败", cx: "已取消创建", cxBtn: "取消创建", failHint: "已下载的文件有缓存，重试速度较快。", go: "完成，进入控制台" };
  const ASK_CREATE = { title: s => `取消创建<span class="nw">「${esc(s.name)}」？</span>`, body: "将删除未建完的文件夹，已下载的文件会保留在缓存中。", keep: "继续创建" };
  // 导入整合包：Go 里现在和新建是同一类任务（kind 为空），程序页头只能写「正在创建服务器」；作者 11-29 定后端给它自己的种类，说法照交接包、服务端导入那两套。
  // 做完的那句（进标题的悬停）分两种：有没装上的文件时不说「均已就绪」（第二个参数是任务本身，见 headModel）
  const T_PACK = { title: "正在导入整合包", sub: "正在准备 Java、安装服务端，并下载整合包中的模组与配置。",
    done: "整合包已导入", doneSub: (s, t) => t.pending.length || t.skipped.length ? "其余文件均已安装；未装上的文件可在下方逐项处理" : "服务端、模组与配置均已就绪，可以启动",
    fail: "整合包导入失败", cx: "已取消导入", cxBtn: "取消导入", failHint: "已下载的文件有缓存，重试速度较快。", go: "完成，进入控制台" };
  const ASK_PACK = { title: s => `取消导入<span class="nw">「${esc(s.name)}」？</span>`, body: "将删除未完成导入的服务器文件夹，已下载的文件会保留在缓存中。", keep: "继续导入" };

  // Forge / NeoForge 的建服过程（下载安装器、预放原版服务端、跑官方安装器），导入整合包多两步
  const forgeOps = o => [
    { s: 0 }, { l: `MC ${o.mc} 需要 Java ${o.java}（来自官方元数据）` },
    { s: 1 }, { l: `使用本机已安装的 Java ${o.java}（${o.jv}）：MineDeck\\runtimes\\java\\${o.jdk}\\bin\\java.exe` }, { l: `Java 就绪：MineDeck\\runtimes\\java\\${o.jdk}\\bin\\java.exe` },
    { s: 2 }, { p: `下载 ${o.installer}`, b: o.insMb * MB }, { l: "预放置原版服务端（加速安装器）" }, { m: "cancel", f: .55 }, { p: "下载 server.jar", b: o.vanMb * MB },
    { s: 3 }, { l: "运行官方安装器（将从网络下载依赖库，耗时视网络情况可能需数分钟）…" },
    { l: `[安装器] JVM info: Eclipse Adoptium - ${o.jv} - ${o.jv}+11` }, { l: "[安装器] Target Directory: ." }, { l: "[安装器] Extracting main jar:" },
    { l: `[安装器] Considering library ${o.lib}` }, { l: "[安装器] Considering library net.minecraftforge:JarJarFileSystems:0.3.19" },
    { l: "……（已省略中间 236 行）……" },
    { l: "[安装器] Processor: net.minecraftforge:binarypatcher:1.1.1:fatjar" }, { l: "[安装器] \tSplitting: done" }, { l: "[安装器] The server installed successfully" },
    { l: `启动目标：args:@libraries/${o.libPath}/win_args.txt` },
    { s: 4 }, { s: 5 },
    { s: 6 }, { m: "run", f: .41 }, { m: "slow", f: .41 }, { m: "fail", f: .66 }, { p: "下载整合包文件", b: o.packMb * MB }, { l: `整合包文件下载完成（${o.files} 个）` },
    ...(o.partial ? [{ l: "⚠ 1 个文件无法自动下载（未配置 API Key），已计入「没装上的文件」，导入完成后可逐项处理" }] : []),
    { s: 7 }, { l: "白名单：关闭（任何知道地址的玩家均可加入）" }, { l: "服务器创建完成 ✔" },
  ];

  // 每种任务：说法（T，照 app.js TASK_KINDS 原话）、步骤名（照 Go 里各自的 steps）、过程（ops）和几种结尾
  const KINDS = {
    create: { label: "创建", from: "new", srv: SRV.test, cancel: true, edit: ["wizard.html"], T: T_CREATE, ask: ASK_CREATE,
      steps: CREATE_STEPS, cxText: "已取消，未创建服务器",
      ops: () => [
        { s: 0 }, { l: "MC 1.21.4 需要 Java 21（来自官方元数据）" },
        { s: 1 }, { l: "需要 Java 21，开始下载便携式运行时（不影响系统环境）" }, { l: "按 Adoptium 公布的 SHA-256 从清华镜像下载 OpenJDK21U-jdk_x64_windows_hotspot_21.0.5_11.zip" },
        { m: "cancel", f: .62 }, { p: "下载 Java 21", b: 195.6 * MB }, { l: `Java 就绪：${JDK21}` },
        { s: 2 }, { m: "run", f: .37 }, { m: "slow", f: .37 }, { m: "fail", f: .37 }, { p: "下载 paper-1.21.4-232.jar", b: 51.2 * MB },
        { s: 3 }, { l: "该核心无需安装步骤，跳过" },
        { s: 4 }, { l: "白名单：关闭（任何知道地址的玩家均可加入）" },
        { s: 5 }, { l: "服务器创建完成 ✔" },
      ],
      // 下载服务端核心走 fetchToInstance，出问题会报 NetRetry（主机名、什么问题、第几次）：「下载慢」有两种标题
      slow: { net: { host: "api.papermc.io", what: "下载失败", count: 3 }, idle: 47,
        pre: ["从 api.papermc.io 下载 paper-1.21.4-232.jar 失败（读取超时），将切换至下一个下载源或稍后重试", "从 api.papermc.io 下载 paper-1.21.4-232.jar 失败（读取超时），将切换至下一个下载源或稍后重试", "从 api.papermc.io 下载 paper-1.21.4-232.jar 失败（读取超时），将切换至下一个下载源或稍后重试"] },
      fail: { pre: ["从 api.papermc.io 下载 paper-1.21.4-232.jar 失败（读取超时），将切换至下一个下载源或稍后重试", "从 fill-data.papermc.io 下载 paper-1.21.4-232.jar 失败（HTTP 403），将切换至下一个下载源或稍后重试"],
        err: "多次尝试后仍下载失败（api.papermc.io: 读取超时；fill-data.papermc.io: HTTP 403（源站拒绝访问：可能是机器人防护、链接失效或无访问权限））", hint: NET_HINT,
        leftover: "remove MineDeck\\servers\\测试服\\cache\\paper-1.21.4-232.jar: The process cannot access the file because it is being used by another process." },
      go: s => link("detail.html", { kind: s.detail }) },

    // 日志里的话（cxText 和 forgeOps 里的「服务器创建完成 ✔」）是后端写的，照旧（同服务端导入）；页面上的说法见 T_PACK
    pack: { label: "整合包", from: "new", srv: SRV.gear, cancel: true, edit: ["import.html", { flow: "modpack" }], T: T_PACK, ask: ASK_PACK,
      cover: "导入整合包", steps: [...CREATE_STEPS, "下载整合包文件", "应用整合包覆盖"], cxText: "已取消，未创建服务器",
      ops: s => s === SRV.tinker
        ? forgeOps({ mc: "1.21.1", java: 21, jv: "21.0.5", jdk: "jdk-21.0.5+11", installer: "neoforge-21.1.77-installer.jar", insMb: 5.9, vanMb: 51.6, lib: "net.neoforged:neoforge:21.1.77",
            libPath: "net/neoforged/neoforge/21.1.77", packMb: 268.3, files: 214, partial: true })
        : forgeOps({ mc: "1.20.1", java: 17, jv: "17.0.13", jdk: "jdk-17.0.13+11", installer: "forge-1.20.1-47.3.0-installer.jar", insMb: 7.1, vanMb: 47.4, lib: "net.minecraftforge:forge:1.20.1-47.3.0",
            libPath: "net/minecraftforge/forge/1.20.1-47.3.0", packMb: 312.5, files: 186 }),
      // 整合包文件的下载不报 NetRetry（dl.Fetch 没传 OnRetry），只会是「多久没有新进度」
      slow: { idle: 47 },
      fail: { pre: [], err: "下载整合包文件失败：create-1.20.1-0.5.1.j.jar：多次尝试后仍下载失败（cdn.modrinth.com: 读取超时）", hint: NET_HINT,
        leftover: "remove MineDeck\\servers\\机械动力：齿轮之城\\mods: The process cannot access the file because it is being used by another process." },
      // 有文件没装上（首页浮层 t4）：换成匠魂整合；文件和原因同共用件页「没装上的文件」那三个
      partial: { srv: SRV.tinker,
        warnings: [
          { kind: "cf-unresolved", merged: true },
          { kind: "import-pending", merged: true },
          { kind: "import-skipped", title: "2 个文件未安装", note: "导入时已跳过这些文件，原因见各项说明。仅客户端文件无需安装到服务端；其余文件可在导入完成后手动放入服务器文件夹。",
            items: [{ name: "mods/oculus-mc1.21.1-1.8.0.jar（整合包清单标明仅客户端）", mono: true }, { name: "mods/sodiumextras-neoforge-1.0.7.jar（整合包清单标明仅客户端）", mono: true }] },
          // 程序这条告警下面有一颗「去「设置 → 启动与内存」换 Java」；模组名是编的
          { kind: "java-req", title: "有模组需要 Java 25，当前使用 Java 21",
            note: "此提示不影响本次导入。保留这些模组可能导致启动失败或功能异常；可在「设置 → 启动与内存」更换 Java（支持自动下载便携版），也可暂保持现状。",
            items: [{ name: "example-optimizer" }], act: ["gojava", "更换 Java", "前往「设置 → 启动与内存」更换 Java"] },
        ],
        pending: [
          { id: "p1", p: "mods/toughasnails-neoforge-1.21.1-10.0.0.jar", why: "未配置 CurseForge API Key", note: "「Tough As Nails」：未配置 CurseForge API Key，无法自动下载。可保留本地已有文件，或手动放入后指定路径。", opts: ["skip", "local"], url: "https://www.curseforge.com/minecraft/mc-mods/tough-as-nails" },
          { id: "p2", p: "mods/sophisticatedbackpacks-1.21.1-3.23.4.jar", why: "站点不在允许列表中", host: "mediafilez.forgecdn.net", note: "下载地址不在 MineDeck 允许列表中（仅允许 Modrinth、GitHub、GitLab），确认站点可信后才能下载。", opts: ["skip", "local", "trust"], url: "https://mediafilez.forgecdn.net/files/5/8/sophisticatedbackpacks-1.21.1-3.23.4.jar" },
          { id: "p3", p: "mods/custom-tweaks-1.0.jar", why: "整合包未提供校验值", note: "整合包未提供该文件的校验值，无法核对下载内容，不可自动下载替代本地文件。", opts: ["skip", "local"] },
        ],
        skipped: [{ id: "p0", p: "mods/jei-1.21.1-neoforge-19.21.0.247.jar", why: "整合包未提供校验值", note: "整合包未提供该文件的校验值，无法核对下载内容，不可自动下载替代本地文件。", opts: ["skip", "local"] }],
      },
      go: s => link("detail.html", { kind: s.detail, tab: "addons" }) },

    adopt: { label: "接管", from: "new", srv: SRV.old, edit: ["import.html", { flow: "adopt" }],
      T: { title: "正在接管服务器", sub: "正在检查文件夹并准备 Java。不会改动现有存档与配置（更改端口时仅更新端口项）。", done: "服务器已接管", doneSub: "已添加至 MineDeck，可直接启动和管理",
        fail: "服务器接管失败", failHint: "服务器文件夹中的原有文件未被删除或修改，可重试。", go: "完成，进入控制台" },
      steps: ["检查服务器文件夹", "准备 Java 运行时", "登记到 MineDeck"],
      ops: (s, st) => [
        { s: 0 }, { l: "服务器文件夹：D:\\MC\\怀旧服" }, { l: "启动方式：paper-1.16.5-794.jar" },
        { s: 1 }, { l: "MC 1.16.5 需要 Java 8（来自官方元数据）" }, { l: "需要 Java 8，开始下载便携式运行时（不影响系统环境）" },
        { l: "按 Adoptium 公布的 SHA-256 从清华镜像下载 OpenJDK8U-jdk_x64_windows_hotspot_8u432b06.zip" },
        { m: "run", f: .58 }, { m: "slow", f: .58 }, { m: "fail", f: .58 }, { p: "下载 Java 8", b: 102.3 * MB }, { l: "Java 就绪：MineDeck\\runtimes\\java\\jdk8u432-b06\\bin\\java.exe" },
        { s: 2 }, st === "partial" ? { l: "⚠ " + LOG4J_TITLE + "。" + LOG4J_NOTE } : { l: "已启用 log4shell 漏洞缓解（Mojang 官方方案，log4j2_112-116.xml）" },
        { l: "已写入 eula.txt（已在上一步同意 Minecraft EULA）" }, { l: "接管完成 ✔ 存档、模组、插件与配置文件均未改动" },
      ],
      slow: { idle: 47 }, // Java 的下载不报 NetRetry
      fail: { pre: ["清华镜像下载失败（读取超时），切换至 Adoptium 官方源"], err: "多次尝试后仍下载失败（github.com: 读取超时；objects.githubusercontent.com: 读取超时）", hint: NET_HINT },
      partial: { warnings: [{ kind: "custom-log4j", title: "", note: "" }] }, // 标题、说明在下面补（LOG4J_*）
      go: s => link("detail.html", { kind: s.detail }) },

    java: { label: "Java", from: "inst", srv: SRV.friend,
      T: { title: "正在更换 Java", sub: "本机未检测到该版本的 Java，正在下载便携版运行时（不影响系统 Java 环境）。", done: "Java 已更换", doneSub: "下次启动该服务器时生效",
        fail: "Java 更换失败", failHint: "服务器仍使用原有 Java，可返回重试。", go: "完成，返回服务器" },
      cover: "Java 21", steps: ["准备 Java 运行时", "应用新 Java"], showDoneSub: true,
      ops: () => [
        { s: 0 }, { l: "需要 Java 21，开始下载便携式运行时（不影响系统环境）" }, { l: "按 Adoptium 公布的 SHA-256 从清华镜像下载 OpenJDK21U-jdk_x64_windows_hotspot_21.0.5_11.zip" },
        { m: "run", f: .52 }, { m: "slow", f: .52 }, { m: "fail", f: .52 }, { p: "下载 Java 21", b: 195.6 * MB },
        { s: 1 }, { l: `已更换为 Java 21：${JDK21}（下次启动时生效）` },
      ],
      slow: { idle: 47 },
      fail: { pre: ["清华镜像下载失败（读取超时），切换至 Adoptium 官方源"], err: "多次尝试后仍下载失败（github.com: 读取超时；objects.githubusercontent.com: 读取超时）", hint: NET_HINT },
      go: s => link("detail.html", { kind: s.detail, tab: "settings", sub: "jvm" }) },

    export: { label: "导出", from: "inst", srv: SRV.friend,
      T: { title: "正在导出交接包", sub: "正在将存档、配置、模组和插件打包为单个文件，不会改动原服务器。", done: "交接包已导出", doneSub: "交接包已生成，文件路径见下方日志",
        fail: "交接包导出失败", failHint: "未保留不完整的交接包，可重试（详情见任务日志）。", go: "完成" },
      steps: ["收集清单", "打包交接包", "收尾"],
      ops: () => [
        { s: 0 }, { l: "含存档、配置、模组与名单（不包含 logs、崩溃报告与锁定文件）" }, { l: "插件 12 个" }, { l: "待打包 1834 个文件、486.2 MB" },
        { s: 1 }, { m: "run", f: .44 }, { m: "slow", f: .44 }, { m: "fail", f: .81 }, { p: "打包交接包（已打包 {n} 个文件）", b: 486.2 * MB, files: 1834 },
        { l: "交接包大小：402.7 MB（含存档 388.1 MB）" }, { l: "将此文件发送给接收方，对方在「新建服务器」页选择「交接包」导入即可" },
        { s: 2 }, { l: `交接包已生成：${EXPORT_PATH}` }, { fin: EXPORT_PATH },
      ],
      slow: { idle: 47 },
      fail: { pre: [], err: `write ${EXPORT_PATH}.part: There is not enough space on the disk.`, hint: DISK_HINT },
      go: () => link("home.html") },

    import: { label: "导入", from: "new", srv: SRV.handed, edit: ["import.html", { flow: "transfer" }],
      T: { title: "正在导入交接包", sub: "正在准备 Java、下载核心并解包。存档、配置、模组和插件均会保留。", done: "交接包已导入",
        doneSub: "存档、配置、模组和插件均已导入，可以启动。未随包导入的项目见下方日志",
        fail: "交接包导入失败", failHint: "已清理未完成的文件夹，可重试。", go: "完成，进入控制台" },
      steps: ["校验交接包", "准备 Java 运行时", "下载服务端核心", "安装服务端", "解包服务器文件", "生成启动脚本"],
      showDoneSub: () => "未随包导入的项目见下方日志", // doneSub 的后半句（前半句和标题说的一样）
      ops: () => [
        { s: 0 }, { l: "交接包：生存服 · 朋友（paper 1.21.4，2026-11-27 22:15 导出，交接）" }, { l: "包内清单：存档 388.1 MB、模组 0 个、插件 12 个" }, { l: "未包含：logs、崩溃报告" },
        { s: 1 }, { l: "原服务器使用 Java 21（读取自原 instance.json）" }, { l: `使用本机已安装的 Java 21（21.0.5）：${JDK21}` }, { l: `Java 就绪：${JDK21}` },
        { s: 2 }, { m: "run", f: .63 }, { m: "slow", f: .63 }, { p: "下载 paper-1.21.4-232.jar", b: 51.2 * MB },
        { s: 3 },
        { s: 4 }, { m: "fail", f: .7 }, { p: "解包交接包（已解压 {n} 个文件）", b: 402.7 * MB, files: 1834 }, { l: "已解压 486.2 MB" },
        { l: "原有的定时任务（2 条）与崩溃自动重启设置未随包导入，如需使用请在该服务器设置中重新配置" },
        { s: 5 }, { l: "导入完成 ✔ 存档、配置、模组与插件均已导入（未随包导入的项目见上方日志）" },
      ],
      // 下载核心经 inst.FetchArtifact → fetchToInstance，同新建，会报 NetRetry
      slow: { net: { host: "api.papermc.io", what: "下载失败", count: 2 }, idle: 47,
        pre: ["从 api.papermc.io 下载 paper-1.21.4-232.jar 失败（读取超时），将切换至下一个下载源或稍后重试", "从 api.papermc.io 下载 paper-1.21.4-232.jar 失败（读取超时），将切换至下一个下载源或稍后重试"] },
      fail: { pre: [], err: "write MineDeck\\servers\\朋友的生存服\\world\\region\\r.0.0.mca: There is not enough space on the disk.", hint: DISK_HINT },
      go: s => link("detail.html", { kind: s.detail }) },

    server: { label: "服务端", from: "new", srv: SRV.sky, cancel: true, edit: ["import.html", { flow: "modpack" }],
      T: { title: "正在导入服务端", sub: "正在解压服务端文件并准备 Java，沿用压缩包中的存档、模组、插件和配置。", done: "服务端已导入", doneSub: "已沿用压缩包中的存档、模组、插件和配置，可以直接启动",
        fail: "服务端导入失败", cx: "已取消导入", cxBtn: "取消导入", failHint: "已清理未完成的文件夹，可重试。", go: "完成，进入控制台" },
      ask: { title: s => `取消导入<span class="nw">「${esc(s.name)}」？</span>`, body: "将删除未解压完的文件夹。", keep: "继续导入" },
      steps: ["解压服务端文件", "识别核心和版本", "准备 Java 运行时", "登记到 MineDeck"], cxText: "已取消，未创建服务器",
      ops: (s, st) => [
        { s: 0 }, { l: "按现有服务端解压（文件位于压缩包顶层）" }, { l: "已解压 2416 个文件" },
        { s: 1 }, { l: "已按「接管本地服」的规则识别：purpur 1.21.4，启动文件 purpur-1.21.4-2416.jar" },
        ...(st === "partial" ? [{ l: "版本号 1.21.4 识别自压缩包中的 logs/latest.log（服务端 jar 未标注版本）" },
          { l: "已将 start.bat 中的以下 JVM 参数导入至「自定义 JVM 参数」：-XX:+UseG1GC -XX:MaxGCPauseMillis=200 -Dusing.aikars.flags=https://mcflags.emc.gs" },
          { l: "已忽略 start.bat 中的以下 JVM 参数：-javaagent:authlib-injector.jar（可使 Java 加载外部程序）" }] : []),
        { s: 2 }, { l: "使用 Java 21" }, { l: "需要 Java 21，开始下载便携式运行时（不影响系统环境）" },
        { m: "cancel", f: .3 }, { m: "run", f: .46 }, { m: "slow", f: .46 }, { m: "fail", f: .46 }, { p: "下载 Java 21", b: 195.6 * MB }, { l: `Java 就绪：${JDK21}` },
        { s: 3 }, { l: "已写入 eula.txt" }, { l: "服务端导入完成 ✔ 已沿用压缩包中的存档、模组、插件与配置，未重新下载核心" },
      ],
      slow: { idle: 47 },
      fail: { pre: ["清华镜像下载失败（读取超时），切换至 Adoptium 官方源"], err: "多次尝试后仍下载失败（github.com: 读取超时；objects.githubusercontent.com: 读取超时）", hint: NET_HINT,
        leftover: "remove MineDeck\\servers\\空岛服\\world\\session.lock: The process cannot access the file because it is being used by another process." },
      partial: { warnings: [
        { kind: "mc-from-log", title: "版本号通过日志识别", note: "服务端 jar 未标注 Minecraft 版本，1.21.4 识别自压缩包中的 logs/latest.log（上次开服版本）。版本号决定所选 Java 及是否启用 log4shell 防护；若后续更换过服务端，建议先解压并通过「新建服务器 → 接管本地服」手动指定版本。" },
        { kind: "script-jvm", title: "已导入 3 项启动脚本 JVM 参数，忽略 1 项",
          note: "已导入项可在「设置 → 启动与内存」的自定义 JVM 参数中查看：-XX:+UseG1GC -XX:MaxGCPauseMillis=200 -Dusing.aikars.flags=https://mcflags.emc.gs。若更换 Java 大版本后不兼容可前往移除。仅自动导入性能调优参数，内存沿用导入页的设置；涉及加载外部程序、执行命令、改写代理或日志配置的参数默认忽略，确认可信后可手动添加。",
          items: [{ name: "-javaagent:authlib-injector.jar（可使 Java 加载外部程序）", mono: true }] },
      ] },
      go: s => link("detail.html", { kind: s.detail }) },

    cart: { label: "清单", from: "inst", srv: SRV.friend,
      T: { title: "正在安装待装清单",
        sub: s => /^(mohist|banner|arclight|taiyitist)$/i.test(s.core)
          ? "正在下载清单中的插件与模组及其前置，将放入服务器的 plugins / mods 文件夹。"
          : /^(fabric|forge|neoforge|quilt)$/i.test(s.core)
            ? "正在下载清单中的模组及其前置，将放入服务器的 mods 文件夹。"
            : "正在下载清单中的插件及其前置，将放入服务器的 plugins 文件夹。",
        done: "待装清单已安装", doneSub: "新安装的内容需重启服务器后生效",
        fail: "待装清单安装失败",
        failHint: s => {
          const tab = /^(mohist|banner|arclight|taiyitist)$/i.test(s.core) ? "插件和模组" : /^(fabric|forge|neoforge|quilt)$/i.test(s.core) ? "模组" : "插件";
          return `可返回服务器的「${tab}」页在待装清单中重新安装。已下载的文件有缓存，重试速度较快。`;
        },
        go: "完成，进入控制台" },
      cover: "待装清单 3 项", steps: ["匹配版本与前置", "下载文件", "放入本地文件", "核对本地依赖"], showDoneSub: true,
      ops: (s, st) => [
        { s: 0 }, { l: "待装清单共 3 个 Modrinth 项目：LuckPerms、EssentialsX、Chunky" },
        { s: 1 }, { m: "fail" }, { l: "[1/3] LuckPerms：LuckPerms-Bukkit-5.4.145.jar（1.9 MiB）" }, { p: "下载 LuckPerms-Bukkit-5.4.145.jar", b: 1.9 * MB },
        { l: "[2/3] EssentialsX：EssentialsX-2.20.1.jar（3.1 MiB）" },
        ...(st === "partial" ? [{ l: "  失败：多次尝试后仍下载失败（cdn.modrinth.com: 读取超时）" }]
          : [{ m: "run", f: .58 }, { m: "slow", f: .58 }, { p: "下载 EssentialsX-2.20.1.jar", b: 3.1 * MB }]),
        { l: "[3/3] Chunky：Chunky-Bukkit-1.4.28.jar（0.4 MiB）" }, { p: "下载 Chunky-Bukkit-1.4.28.jar", b: .4 * MB },
        { s: 2 }, { s: 3 }, { l: "已读取 3 个插件的依赖声明" }, { l: "服务器正在运行：新安装的内容需重启服务器后生效" }, { l: st === "partial" ? "已安装 2 个文件" : "已安装 3 个文件" },
      ],
      slow: { idle: 47 },
      // 全部没下成才算失败（modcart.go：len(failed) == len(files)），一个没下成是「部分完成」
      fail: { pre: ["[1/3] LuckPerms：LuckPerms-Bukkit-5.4.145.jar（1.9 MiB）", "  失败：多次尝试后仍下载失败（cdn.modrinth.com: 读取超时）", "[2/3] EssentialsX：EssentialsX-2.20.1.jar（3.1 MiB）",
        "  失败：多次尝试后仍下载失败（cdn.modrinth.com: 读取超时）", "[3/3] Chunky：Chunky-Bukkit-1.4.28.jar（0.4 MiB）", "  失败：多次尝试后仍下载失败（cdn.modrinth.com: 读取超时）"],
        err: "多次尝试后仍下载失败（cdn.modrinth.com: 读取超时）", hint: NET_HINT },
      partial: { warnings: [{ kind: "download-failed", title: "1 个文件下载失败", note: "待装清单保持不变：请检查网络或代理后重试，已安装的文件会自动跳过。",
        items: [{ name: "EssentialsX-2.20.1.jar", mono: true }], act: ["retry", "重试"] }] },
      go: s => link("detail.html", { kind: s.detail }) },

    map: { label: "地图", from: "inst", srv: SRV.friend, mapName: "天空之城 SkyCity",
      T: { title: "正在更换地图", sub: "导入地图或换回导入前的地图，出错时将自动退回原有存档。", done: "地图更换完成", doneSub: "可在「世界与备份 → 换地图」中查看",
        fail: "地图更换失败", cx: "已取消更换地图", cxSub: "未替换任何文件，原有存档保持不变。", failHint: "原有文件均未丢失，可在「世界与备份 → 换地图」查看当前使用的存档。",
        go: "完成，返回服务器" },
      cover: "天空之城 SkyCity", steps: ["核对地图与服务器", "复制地图至暂存区", "停止并锁定服务器", "替换为新地图"], cxText: "已取消，未替换任何文件",
      // 结果是一句给人看的话（worldimport service.go runImport 的返回值），做完放在标题下面
      ops: () => [
        { s: 0 }, { l: "开始导入：天空之城 SkyCity（SkyCity_v2.1.zip）" }, { l: "核对通过：地图与当前服务器兼容" },
        { s: 1 }, { m: "cancel", f: .4 }, { m: "run", f: .46 }, { m: "slow", f: .46 }, { p: "复制地图", b: 1126 * MB }, { l: "地图已复制至暂存区（1.1 GB）" },
        { s: 2 }, { l: "正在停止服务器（等待服务器保存并正常退出，不会强制终止）…" }, { l: "服务器已保存并退出，已锁定当前服务器" },
        { s: 3 }, { m: "fail" }, { l: "原存档已移入恢复点：world、world_nether、world_the_end" }, { l: "新地图已替换完成" },
        { l: "导入前的存档已保留在恢复点中（2.3 GB），后续可随时在「世界与备份 → 换地图」中换回" },
        { fin: "「生存服 · 朋友」的地图存档已导入，尚未启动验证。服务器目前处于停止状态，启动后可进服确认是否为目标地图" },
      ],
      slow: { idle: 47 },
      fail: { pre: [], err: "导入中断，已退回导入前的存档：rename MineDeck\\servers\\生存服 · 朋友\\world: Access is denied.", hint: "没有读写权限，请检查文件夹权限或关闭占用它的程序" },
      go: s => link("detail.html", { kind: s.detail, tab: "world", sub: "map" }) },
  };
  // 接管时文件夹里已经有一份不是官方的 log4j 配置（inst/install.go warnAdoptedCustomLog4j、log4j_migrate.go log4jXMLConflictParts 拼出来的原话）
  var LOG4J_TITLE = "log4shell 防护未生效：此 1.16.5 服务器未应用 Mojang 官方日志配置";
  var LOG4J_NOTE = "服务器文件夹中已存在非官方的 log4j2_112-116.xml，MineDeck 未覆盖也未自动引用。如需使用官方配置，请重命名或移走该文件后重启 MineDeck；如需使用自定义配置，请在「设置 → 启动与内存」添加 -Dlog4j.configurationFile=log4j2_112-116.xml。";
  Object.assign(KINDS.adopt.partial.warnings[0], { title: LOG4J_TITLE, note: LOG4J_NOTE });
  // 并进「没装上的文件」那一块的两条告警（同一件事程序说了三遍：「N 个文件需手动补装」「N 个文件待处理」和处理面板），说明进那一格分段的悬停
  const PEND_TIP = "这些文件无法自动下载，需逐项选择处理方式：用本地文件代替、跳过，或在确认第三方站点可信后下载。已跳过的文件后续仍可在服务器详情页处理。";
  const ACT = { skip: "跳过", local: "用本地文件代替", trust: "信任站点并下载" };

  // ================= 草图参数 =================
  // 每种任务哪几种状态程序里真会出现：取消只有新建、导入整合包、导入服务端（程序里有取消键）和换地图（取消键在换地图那页，这页上只看得到结果）；
  // 部分完成只有导入整合包（没装上的文件）、接管（log4j）、导入服务端（版本号、JVM 参数）、待装清单（没下载成的）。切不出来的组合在切换条上灰掉，硬写进地址时落到「进行中」
  const CAN_CX = ["create", "pack", "server", "map"], CAN_PART = ["pack", "adopt", "server", "cart"];
  const NET_SLOW = ["create", "import"]; // 只有下载服务端核心（inst.fetchToInstance，导入交接包经 FetchArtifact 也走它）和 Forge 安装器报 NetRetry；这两种的「慢」停在下核心那里，别的卡住只会是「多久没有新进度」
  const STATES = { running: "进行中", slow: "慢", done: "完成", failed: "失败", cancelled: "取消", partial: "部分", lost: "连不上", gone: "不存在" };
  const legal = (k, s) => s === "cancelled" ? CAN_CX.includes(k) : s === "partial" ? CAN_PART.includes(k) : true;
  const KIND = Object.hasOwn(KINDS, QS.get("kind")) ? QS.get("kind") : "create";
  let ST = Object.hasOwn(STATES, QS.get("st")) ? QS.get("st") : "running";
  if (!legal(KIND, ST)) ST = "running";
  const DOCK = QS.get("dock") === "sink" ? "sink" : "float";
  const PLAY = QS.get("play") === "1", ENTER = QS.get("enter") === "1", CART = QS.get("cart") === "1";
  const DRAFT = QS.get("draft") !== "0", LOCAL = QS.get("local") !== "0", LEFT = QS.get("leftover") === "1";
  const WHY = QS.get("why") === "idle" || !NET_SLOW.includes(KIND) ? "idle" : "net";

  // ================= 任务：照 Go 的 tasks.Task 走 =================
  let T = null;          // 眼下这个任务
  let playTok = 0;       // 自动往下走的那一轮，换了就作废
  const fresh = (kindKey, srv) => { const K = KINDS[kindKey]; return { kind: kindKey, K, srv, steps: K.steps.map(n => ({ name: n, status: "pending" })), log: [], label: "", done: 0, total: 0,
    ended: false, err: "", errHint: "", cancelled: false, cancelling: false, result: "", warnings: [], pending: [], skipped: [], net: null, idle: 0, leftover: false, last: null }; };
  const opsOf = T => T.K.ops(T.srv, ST);
  function startStep(i) { T.steps.forEach((s, j) => { if (j < i && s.status === "running") s.status = "done"; }); T.steps[i].status = "running"; T.label = ""; T.done = T.total = 0; T.net = null; T.idle = 0; }
  const labelOf = (op, f) => op.p.replace("{n}", Math.round(f * (op.files || 0)));
  function applyOp(op, f = 1) {
    if ("s" in op) startStep(op.s);
    else if ("l" in op) T.log.push(op.l);
    else if ("p" in op) { T.label = labelOf(op, f); T.total = op.b; T.done = Math.round(op.b * f); T.net = null; T.idle = 0; T.last = { label: T.label, done: T.done, total: T.total }; }
  }
  function finish(result) { T.steps.forEach(s => { if (s.status !== "done") s.status = "done"; }); T.ended = true; T.result = result; }
  function fail(err, hint) { T.steps.forEach(s => { if (s.status === "running") s.status = "error"; }); T.ended = true; T.err = err; T.errHint = hint || ""; T.log.push("❌ " + err); }
  function endCancel() { T.steps.forEach(s => { if (s.status === "running") s.status = "cancelled"; }); T.ended = true; T.cancelled = true; T.cancelling = false; T.err = T.K.cxText; T.log.push(T.K.cxText); }
  function endFail() {
    const F = T.K.fail; F.pre.forEach(l => T.log.push(l));
    T.leftover = LEFT && !!F.leftover;
    fail(F.err + (T.leftover ? "；清理未完成文件失败：" + F.leftover : ""), F.hint);
  }
  function endPartial() {
    const P = T.K.partial;
    T.warnings = P.warnings.map(w => ({ ...w }));
    T.pending = (P.pending || []).map(x => ({ ...x, act: "", path: "", ack: false, err: "" }));
    T.skipped = (P.skipped || []).map(x => ({ ...x, act: "", path: "", ack: false, err: "" }));
  }
  const finOf = ops => (ops.find(o => "fin" in o) || {}).fin ?? T.srv.name;
  // 直接画出某一帧
  function snapshot(kindKey, st) {
    const K = KINDS[kindKey];
    T = fresh(kindKey, st === "partial" && K.partial?.srv || K.srv);
    const ops = opsOf(T), target = { running: "run", lost: "run", slow: "slow", failed: "fail", cancelled: "cancel" }[st];
    for (let i = 0; i < ops.length; i++) {
      const op = ops[i];
      if (op.m) {
        if (op.m !== target) continue;
        if (op.f != null) applyOp(ops.slice(i + 1).find(o => "p" in o), op.f);
        return endAt(st, ops);
      }
      if (!("fin" in op)) applyOp(op);
    }
    return endAt(st, ops);
  }
  function endAt(st, ops) {
    if (st === "slow") { const S = T.K.slow; if (WHY === "net" && S.net) { S.pre.forEach(l => T.log.push(l)); T.net = { ...S.net }; } else T.idle = S.idle; }
    else if (st === "failed") endFail();
    else if (st === "cancelled") endCancel();
    else if (st === "done") finish(finOf(ops));
    else if (st === "partial") { finish(finOf(ops)); endPartial(); }
  }

  // ================= 页面骨架：只建一次 =================
  const V = {}; // 已经画在页面上的样子（每一处比较这个，变了才动）
  const coverSub = (T) => {
    const s = T.srv, K = T.K, base = `${esc(s.label)} · <span class="mono">${esc(s.mc)}</span>`;
    if (T.kind === "adopt") return `${base} · <span class="mono">${esc(s.dir)}</span>`;
    if (T.kind === "import") return `${base} · <span class="mono">${esc(s.pack)}</span>`;
    if (T.kind === "server") return `${base} · <span class="mono">${esc(s.zip)}</span>`;
    return K.cover ? `${base} · ${esc(K.cover)}` : base;
  };
  function build() {
    const s = T.srv;
    $("#tk2-page").innerHTML = `<h1 class="tk2-q" id="tk2-q"></h1><div class="tk2-qs" id="tk2-qs"></div>`
      + `<section class="tk2-panel"><div class="tk2-cov" style="--c:${MDPixel16.CORE_COLORS[s.core]}"><span class="tk2-cava">${MDPixel16.svg(s.core, 32)}</span>`
      + `<div class="tk2-ct"><div class="tk2-cn">${esc(s.name)}</div><div class="tk2-cs">${coverSub(T)}</div></div></div>`
      + `<div class="tk2-col"><div class="tk2-steps" id="tk2-steps">${T.steps.map(x => `<div class="tk2-step"><span class="tk2-si"></span><span>${esc(x.name)}</span></div>`).join("")}</div>`
      + `<div class="tk2-main"><div class="tk2-prog" id="tk2-prog"><div class="tk2-ph"><span class="tk2-lbl"></span><span class="tk2-sp"></span><span class="tk2-bytes"></span><span class="tk2-pct"></span></div>`
      + `<div class="tk2-xp">${"<i></i>".repeat(CELLS)}</div></div>`
      + `<div class="tk2-body"><div class="tk2-fold" id="tk2-note"><div></div></div><div class="tk2-fold" id="tk2-tabs"><div></div></div>`
      + `<div class="tk2-views" id="tk2-views"><div class="tk2-view" data-v="log"><div class="tk2-lw tk2-log"><div class="list"></div><i class="thumb"></i></div></div></div></div></div></div></section>`;
    Object.keys(V).forEach(k => delete V[k]);
    V.steps = []; V.lit = 0; V.logN = 0; V.view = "log";
    thinBar($(".tk2-log"));
  }
  function buildGone() {
    $("#tk2-page").innerHTML = `<div class="tk2-gone"><span class="tk2-si2" title="后台任务记录仅在本次运行期间保留，重启 MineDeck 后将清空"><i data-ic="clock"></i></span><div class="tk2-t1">任务不存在</div></div>`;
    hy($("#tk2-page"));
  }

  // ================= 每一处 =================
  // 逐项换（规范第七节）：旧的 0.09 秒淡掉（往反方向挪 5），换上新的，0.22 秒从方向那一侧挪 5 像素淡进来；连着换时上一次没换完的作废
  function swapIn(el, html, { dx = 0, dy = 5 } = {}) {
    const tok = el._tok = (el._tok || 0) + 1;
    el.getAnimations().forEach(a => a.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translate(${-dx}px, ${-dy}px)` }], { duration: 90, easing: IN, fill: "forwards" }).onfinish = () => {
      if (el._tok !== tok) return;
      el.innerHTML = html; hy(el); el.getAnimations().forEach(a => a.cancel());
      el.animate([{ opacity: 0, transform: `translate(${dx}px, ${dy}px)` }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT });
    };
  }
  const setText = (el, t) => { if (el && el.textContent !== t) el.textContent = t; };

  // ---- 标题和下面那行字 ----
  function headParts() {
    const K = T.K, tt = !T.ended ? K.T.title : T.cancelled ? (K.T.cx || "已取消") : T.err ? K.T.fail : K.T.done;
    // 程序页头下面那一行说明（K.sub、做完的 doneSub）进标题的悬停；页面上那一行只留会改变结果、要人记住的
    const val = v => typeof v === "function" ? v(T.srv, T) : v;
    const tip = !T.ended ? val(K.T.sub) : !T.err && !T.cancelled && !doneSubShown() ? val(K.T.doneSub) : "";
    return { tt, tip };
  }
  const headModel = (p = headParts()) => `<span${p.tip ? ` title="${esc(p.tip)}"` : ""}>${esc(p.tt)}</span>`;
  const doneSubShown = () => T.kind === "export" || T.kind === "map" || !!T.K.showDoneSub;
  function subModel() {
    const K = T.K;
    if (ST === "lost") return `<span class="tk2-warn" title="无法连接 MineDeck（Failed to fetch）">${icon("warn")}无法刷新任务进度，正在自动重试</span>`
      + `<button type="button" class="tk2-ib" data-act="copylost" title="复制报错信息">${icon("copy")}</button>`;
    if (!T.ended) return esc(T.cancelling ? "正在取消并清理未完成文件，请勿关闭程序" : "请勿关闭程序");
    if (T.cancelled) return esc((K.T.cxSub || "未留下未完成文件。").replace(/。$/, ""));
    if (T.err) return "";
    if (T.kind === "export") return `<span><span class="mono">${esc(T.result)}</span><button type="button" class="tk2-ib" data-act="copypath" title="复制路径">${icon("copy")}</button>`
      + (LOCAL ? `<button type="button" class="tk2-ib" data-act="opendir" title="打开所在文件夹">${icon("folder")}</button>` : "") + `</span>`;
    if (T.kind === "map") return esc(T.result);
    if (K.showDoneSub) return esc(typeof K.showDoneSub === "function" ? K.showDoneSub() : K.T.doneSub);
    return "";
  }
  function paintHead(first) {
    const p = headParts(), h = headModel(p), s = subModel(), q = $("#tk2-q"), qs = $("#tk2-qs"), sp = $(":scope > span", q);
    if (V.h !== h) {
      // 字没变、只换了悬停（整合包「没装上的文件」都处理完了，doneSub 跟着换）：只改悬停，标题不整句换
      if (!first && V.h != null && V.tt === p.tt && sp) { if (p.tip) sp.title = p.tip; else sp.removeAttribute("title"); delete sp.dataset.tip; }
      else if (first || V.h == null) { q.innerHTML = h; hy(q); } else swapIn(q, h);
      V.h = h; V.tt = p.tt;
    }
    if (V.s !== s) { if (first || V.s == null) { qs.innerHTML = s; hy(qs); } else swapIn(qs, s); V.s = s; }
  }

  // ---- 步骤 ----
  const stepIco = s => s === "done" ? icon("check") : s === "running" ? icon("spinner", "tk2-spin") : s === "error" ? icon("x") : s === "cancelled" ? `<i class="tk2-dash"></i>` : `<i class="tk2-px"></i>`;
  function paintSteps(first) {
    $$("#tk2-steps .tk2-step").forEach((row, i) => {
      const s = T.steps[i].status; if (V.steps[i] === s) return;
      const pop = !first && V.steps[i] !== undefined; V.steps[i] = s;
      row.className = "tk2-step " + s;
      const ic = $(".tk2-si", row); ic.innerHTML = stepIco(s);
      if (pop) ic.animate([{ transform: "scale(.4)", opacity: 0 }, { transform: "scale(1.15)", opacity: 1, offset: .6 }, { transform: "none", opacity: 1 }], { duration: 260, easing: OUT });
    });
  }

  // ---- 进度：这一项是什么、多少、百分比，30 格 ----
  // 这一项的名字只在和步骤名说的不是一回事时才写（「下载 Java 21」写；「下载整合包文件」「复制地图」和步骤名重了就不写；「打包交接包（已 812 个文件）」只写括号里那半句）
  function shownLabel(label) {
    const cur = T.steps.find(s => s.status === "running" || s.status === "error" || s.status === "cancelled"), sn = cur ? cur.name : "";
    if (!label) return "";
    if (sn && sn.includes(label)) return "";
    if (sn && label.startsWith(sn)) return label.slice(sn.length).replace(/^（|）$/g, "");
    return label;
  }
  const keyOf = label => label.replace(/（[^）]*）$/, ""); // 换了下载项才清空重来（导出时括号里的文件数每个文件都变，不算换项）
  function progModel() {
    if (T.ended && !T.err && !T.cancelled) return { key: "∎", lbl: "", bytes: "", pct: "100%", n: CELLS, idle: false, done: true };
    // 这一步没在下载（StartStep 把 label 清了）：停在上一项的样子、淡下去；结束了（失败、取消）也淡下去
    const src = T.label ? T : T.last;
    if (!src) return { key: "", lbl: "", bytes: "", pct: "", n: 0, idle: true };
    const pct = src.total > 0 ? Math.min(100, src.done / src.total * 100) : 0;
    return { key: keyOf(src.label), lbl: T.label ? shownLabel(src.label) : "", bytes: `${fmtBytes(src.done) || "0 B"}${src.total > 0 ? " / " + fmtBytes(src.total) : ""}`,
      pct: Math.floor(pct) + "%", n: Math.floor(pct / 100 * CELLS + 1e-6), idle: !T.label || T.ended };
  }
  function paintProg(first) {
    const m = progModel(), el = $("#tk2-prog"), cells = $$(".tk2-xp i", el);
    el.classList.toggle("idle", m.idle);
    setText($(".tk2-lbl", el), m.lbl); setText($(".tk2-bytes", el), m.bytes); setText($(".tk2-pct", el), m.pct);
    if (m.key !== V.key) { if (V.key != null && !m.done && V.lit) { cells.forEach(c => { c.className = ""; c.style.animationDelay = ""; }); V.lit = 0; } V.key = m.key; }
    if (m.n > V.lit) { for (let k = V.lit; k < m.n; k++) { cells[k].className = first ? "on" : "on lit"; cells[k].style.animationDelay = first ? "" : Math.min(k - V.lit, 12) * 24 + "ms"; } V.lit = m.n; }
    else if (m.n < V.lit) { for (let k = m.n; k < V.lit; k++) cells[k].className = ""; V.lit = m.n; }
    if (m.done && !first && !V.flashed) { const xp = $(".tk2-xp", el); xp.classList.remove("flash"); void xp.offsetWidth; xp.classList.add("flash"); }
    V.flashed = m.done;
  }

  // ---- 提示条：下载慢、失败的原因 ----
  const slowText = () => {
    if (T.net) return `${T.net.host ? T.net.host + " " : ""}${T.net.what}，已重试 ${T.net.count} 次`;
    const sec = T.idle; return sec < 10 ? `已 ${sec} 秒无新进度` : sec < 60 ? `已超过 ${Math.floor(sec / 10) * 10} 秒无新进度` : `已超过 ${Math.floor(sec / 60)} 分钟无新进度`;
  };
  function noteModel() {
    if (!T.ended && !T.cancelling && ST !== "lost" && (T.net || T.idle >= 30)) return { key: "slow", text: slowText() };
    if (T.ended && T.err && !T.cancelled) return { key: "fail", text: T.err + T.errHint + T.leftover };
    return null;
  }
  // 悬停用哪一句：见 SLOW_TIP_OF 上面的说明
  const slowTip = () => T.net || /^下载/.test(T.label) ? SLOW_TIP : SLOW_TIP_OF[T.kind] || SLOW_TIP_OF[""];
  function noteHTML(m) {
    if (m.key === "slow") return `<div class="tk2-slow" title="${esc(slowTip())}">${icon("clock")}<span class="tk2-slt">${esc(m.text)}</span></div>`;
    // 「创建失败：」这类前缀不写（标题已经说没建好）；failHint 进右下「重试」的悬停，没删干净时换成 LEFTOVER_HINT 写在这里（要人去处理）
    return `<div class="tk2-fail"><div class="tk2-fr">${icon("warn")}${esc(T.err)}</div>${T.errHint ? `<div class="tk2-fh">${esc(T.errHint)}</div>` : ""}`
      + (T.leftover ? `<div class="tk2-flo">${icon("folder")}<span>${esc(LEFTOVER_HINT)}</span></div>` : "")
      + `<button type="button" class="tk2-ib" data-act="copyerr" title="复制报错信息">${icon("copy")}</button></div>`;
  }
  function paintNote(first) {
    const m = noteModel(), box = $("#tk2-note"), inner = box.firstElementChild;
    if (!m) { if (V.note) { box.classList.remove("open"); V.note = null; } return; }
    if (V.note && V.note.key === m.key) {
      if (m.key === "slow") {
        if (V.note.text !== m.text) { const t = $(".tk2-slt", inner); swapIn(t, esc(m.text), { dy: 3 }); }
        const sl = $(".tk2-slow", inner), tip = slowTip(); // 卡住的那一项换了（下载 ↔ 别的），悬停跟着换
        if (sl && (sl.getAttribute("title") ?? sl.dataset.tip) !== tip) { sl.title = tip; delete sl.dataset.tip; }
      }
      V.note = m; return;
    }
    inner.innerHTML = noteHTML(m); hy(inner);
    if (first) { box.style.transition = "none"; box.classList.add("open"); void box.offsetWidth; box.style.transition = ""; }
    else box.classList.add("open");
    V.note = m;
  }

  // ---- 分段：做完以后有要看的（没装上的文件、提示）才出来，和日志换着看 ----
  const shownWarnings = () => T.warnings.filter(w => !w.merged);
  function tabList() {
    if (!T.ended || T.err || T.cancelled) return [];
    const v = [];
    if (T.pending.length || T.skipped.length || V.hadPend) { V.hadPend = true; v.push(["pend", "没装上的文件", T.pending.length ? String(T.pending.length) : "", PEND_TIP]); }
    const w = shownWarnings(); if (w.length) v.push(["warn", "提示", String(w.length), ""]);
    if (v.length) v.push(["log", "日志", "", ""]);
    return v;
  }
  function paintTabs(first) {
    const list = tabList(), box = $("#tk2-tabs"), inner = box.firstElementChild, ids = list.map(x => x[0]).join();
    if (!list.length) { if (V.tabs) { box.classList.remove("open"); V.tabs = ""; showView("log", first); } return; }
    if (V.tabs !== ids) {
      if (!V.tabs || !list.some(x => x[0] === V.view)) V.view = list[0][0]; // 刚出来时停在第一格（没装上的文件 / 提示）
      inner.innerHTML = `<div><div class="tk2-seg" id="tk2-seg"><i class="tk2-sgl"></i>${list.map(([id, t, n, tip]) => `<span data-tab="${id}"${tip ? ` title="${esc(tip)}"` : ""}>${t}<span class="tk2-n">${n}</span></span>`).join("")}</div></div>`;
      V.tabs = ids;
      if (first) { box.style.transition = "none"; box.classList.add("open"); void box.offsetWidth; box.style.transition = ""; } else box.classList.add("open");
      $$("#tk2-seg > span[data-tab]").forEach(s => s.classList.toggle("on", s.dataset.tab === V.view));
      requestAnimationFrame(() => placeSeg($("#tk2-seg"), false));
      showView(V.view, first);
      return;
    }
    list.forEach(([id, , n]) => setText($(`#tk2-seg [data-tab="${id}"] .tk2-n`), n)); // 只换数
    placeSeg($("#tk2-seg"), true);
  }
  function placeSeg(el, animate) {
    if (!el) return; const hl = $(".tk2-sgl", el), on = $(":scope > span.on", el);
    if (!on) { hl.style.opacity = "0"; return; }
    if (!animate || hl.style.opacity === "0") { hl.style.transition = "none"; }
    hl.style.opacity = ""; hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; } else requestAnimationFrame(() => { hl.style.transition = ""; });
  }
  function pickTab(id) {
    if (id === V.view) return;
    const ids = V.tabs.split(","), dir = ids.indexOf(id) > ids.indexOf(V.view) ? 1 : -1;
    $$("#tk2-seg > span[data-tab]").forEach(s => s.classList.toggle("on", s.dataset.tab === id)); placeSeg($("#tk2-seg"), true);
    showView(id, false, dir);
  }
  // 换着看：旧的一块往反方向挪 5 像素淡掉（0.09 秒），新的从方向那一侧挪 5 像素淡进来（0.22 秒）
  function showView(id, instant, dir = 1) {
    ensureView(id);
    const views = $$("#tk2-views > .tk2-view"), next = views.find(v => v.dataset.v === id), cur = views.find(v => !v.hidden && v !== next);
    V.view = id;
    if (instant || !cur) { views.forEach(v => { v.hidden = v !== next; }); afterShow(next); return; }
    const tok = V.vtok = (V.vtok || 0) + 1;
    cur.getAnimations().forEach(a => a.cancel());
    cur.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translateX(${-5 * dir}px)` }], { duration: 90, easing: IN, fill: "forwards" }).onfinish = () => {
      if (V.vtok !== tok) return;
      views.forEach(v => { v.hidden = v !== next; v.getAnimations().forEach(a => a.cancel()); });
      afterShow(next);
      next.animate([{ opacity: 0, transform: `translateX(${5 * dir}px)` }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT });
    };
  }
  function afterShow(v) { $$(".tk2-lw", v).forEach(w => w._paint?.()); if (v.dataset.v === "log") { const l = $(".list", v); l.scrollTop = l.scrollHeight; $(".tk2-lw", v)._paint?.(); } $$(".tk2-seg.lg", v).forEach(s => placeSeg(s, false)); }
  function ensureView(id) {
    if ($(`#tk2-views > [data-v="${id}"]`)) return;
    const v = Object.assign(document.createElement("div"), { className: "tk2-view", hidden: true }); v.dataset.v = id;
    v.innerHTML = id === "pend" ? pendHTML() : warnHTML();
    $("#tk2-views").appendChild(hy(v));
    $$(".tk2-lw", v).forEach(thinBar);
    if (id === "pend") paintApply();
  }

  // ---- 日志：只往后加 ----
  const logCls = l => /^❌/.test(l) ? "e" : /^\s*⚠/.test(l) ? "w" : /^……（已省略/.test(l) ? "c" : "";
  function paintLog(first) {
    const w = $(".tk2-log"), l = $(".list", w);
    if (T.log.length < V.logN) { l.innerHTML = ""; V.logN = 0; } // 换了一个任务（重试）
    if (T.log.length === V.logN) return;
    const atBottom = l.scrollTop + l.clientHeight >= l.scrollHeight - 6;
    T.log.slice(V.logN).forEach(line => {
      const d = document.createElement("div"); d.textContent = line; const c = logCls(line); if (c) d.className = c;
      l.appendChild(d);
      if (!first) d.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT });
    });
    V.logN = T.log.length;
    if (first || atBottom) l.scrollTop = l.scrollHeight; // 人往上翻着看的时候不把他拽回底下
    w._paint?.();
  }

  // ---- 没装上的文件（照 commons 的 M_pending；程序 pendingPanel） ----
  const hasTrust = () => [...T.pending, ...T.skipped].some(x => x.opts.includes("trust"));
  function pbarHTML() {
    return (hasTrust() ? `<button type="button" class="tk2-b sm" data-act="trustall" title="仅选中下方尚未指定处理方式且可从外部站点下载的文件；已跳过、缺少校验值或已选其他方式的项保持不变，且不会自动勾选风险确认。">全选可下载项</button>`
      + `<button type="button" class="tk2-b sm" data-act="ackall" title="为所有已选「信任站点并下载」的项勾选风险确认。仅在下载成功后按域名记录，不会加入默认允许列表。">确认这些站点的风险</button>` : "")
      + `<span class="tk2-sp"></span>` + (LOCAL ? `<button type="button" class="tk2-ib" data-act="openinst" title="打开服务器文件夹：其中「导入待决清单.txt」列出所有未安装文件（含已跳过项）；CurseForge 无法自动下载的项另存于「未解析模组清单.txt」。">${icon("folder")}</button>` : "");
  }
  const segLg = x => `<div class="tk2-seg lg" data-pseg="${x.id}"><i class="tk2-sgl"></i>${x.opts.map(o => `<span data-v="${o}"${x.act === o ? ` class="on"` : ""}>${ACT[o]}</span>`).join("")}</div>`;
  const moreHTML = x => x.act === "local" ? `<input class="tk2-inp" data-k="lpath" placeholder="本地文件或文件夹的完整路径" title="填写文件夹路径时按文件名匹配" spellcheck="false" autocomplete="off" value="${esc(x.path)}">`
    : x.act === "trust" ? `<span class="tk2-ck${x.ack ? " on" : ""}" data-act="ack" role="checkbox" aria-checked="${x.ack}" title="${esc(`我已知晓风险，信任来自「${x.host}」的下载。该站点不在默认允许列表中，下载后仍按整合包校验值核对，校验不通过则不安装。\n下载成功后按域名（不含端口）记录，后续同站点不再重复确认。`)}"><span class="tk2-bx">${icon("check")}</span>信任 <span class="mono">${esc(x.host)}</span> 的下载</span>` : "";
  const pndHTML = (x, skipped) => `<div class="tk2-pnd${skipped ? " skipped" : ""}" data-pid="${x.id}"><div class="tk2-pr1"><span class="tk2-pp" title="${esc(x.p)}">${esc(x.p)}</span>`
    + (skipped ? `<span class="tk2-tag">已跳过</span>` : "") + (x.url ? `<button type="button" class="tk2-ib" data-act="ext" title="在浏览器中打开下载地址">${icon("share")}</button>` : "") + `</div>`
    + `<div class="tk2-pwhy" title="${esc(x.note)}">${esc(x.why)}${x.host ? ` · <span class="host">${esc(x.host)}</span>` : ""}</div>`
    + segLg(x) + `<div class="tk2-pmore">${moreHTML(x)}</div>${x.err ? `<div class="tk2-perr">${icon("warn")}<span>${esc(x.err)}</span></div>` : ""}</div>`;
  const foldHTML = () => T.skipped.length ? `<button type="button" class="tk2-pfold" data-act="pfold"><span>已跳过 ${T.skipped.length} 个</span>${icon("chevronDown")}</button>` : "";
  function pendHTML() {
    return `<div class="tk2-pv"><div class="tk2-pbar" data-k="pbar">${pbarHTML()}</div>`
      + `<div class="tk2-lw" data-k="plw"><div class="list" data-k="plist">${pendBody()}</div><i class="thumb"></i></div>`
      + `<div class="tk2-pfoot"><span class="tk2-pmsg" data-k="pmsg"></span><span class="tk2-sp"></span><button type="button" class="tk2-b pri" data-act="apply"><span class="tk2-pl">应用所选的处理</span></button></div></div>`;
  }
  // 清单空了：都处理完（没有跳过的）或者只剩跳过的（程序：「跳过了 N 个文件，它们没有装上」，说明进悬停）
  const doneLine = () => !T.pending.length ? (T.skipped.length
    ? `<div class="tk2-pdone info" title="如需补装，可在下方改为「用本地文件代替」${T.skipped.some(x => x.opts.includes("trust")) ? "或「信任站点并下载」" : ""}；无需补装则可忽略。">${icon("info")}已跳过 ${T.skipped.length} 个文件，未安装</div>`
    : `<div class="tk2-pdone">${icon("check")}没装上的文件已全部处理完成</div>`) : "";
  const pendBody = () => doneLine() + T.pending.map(x => pndHTML(x, false)).join("") + foldHTML();
  const pv = () => $('#tk2-views > [data-v="pend"]');
  const itemOf = id => T.pending.find(x => x.id === id) || T.skipped.find(x => x.id === id);
  function pndPick(seg, v) {
    const x = itemOf(seg.dataset.pseg); if (!x || x.act === v) return;
    x.act = v; x.err = "";
    $$(":scope > span", seg).forEach(s => s.classList.toggle("on", s.dataset.v === v)); placeSeg(seg, true);
    const row = seg.closest(".tk2-pnd"), more = $(".tk2-pmore", row); more.innerHTML = moreHTML(x); hy(more);
    if (more.firstElementChild) more.firstElementChild.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT });
    $(".tk2-perr", row)?.remove();
    $('[data-k="plw"]', pv())._paint?.();
  }
  function pmsg(text, soft) {
    const m = $('[data-k="pmsg"]', pv()); if (!m) return;
    m.textContent = text; m.classList.toggle("soft", !!soft); m.classList.add("show");
    clearTimeout(m._t); m._t = setTimeout(() => m.classList.remove("show"), 1800);
  }
  function trustAll() {
    let n = 0;
    $$('.tk2-pnd:not(.skipped)', pv()).forEach(row => { const x = itemOf(row.dataset.pid); if (x && !x.act && x.opts.includes("trust")) { pndPick($("[data-pseg]", row), "trust"); n++; } });
    if (!n) pmsg("没有可从外部站点下载的项", true);
  }
  function ackAll() {
    let n = 0;
    $$(".tk2-pnd", pv()).forEach(row => { const x = itemOf(row.dataset.pid); if (x && x.act === "trust" && !x.ack) { x.ack = true; const ck = $('[data-act="ack"]', row); ck?.classList.add("on"); ck?.classList.remove("bad"); ck?.setAttribute("aria-checked", "true"); n++; } });
    if (!n && !$$(".tk2-pnd", pv()).some(r => itemOf(r.dataset.pid)?.act === "trust")) pmsg("请先选择「信任站点并下载」，再确认风险", true);
  }
  let applying = false, trustFailed = false;
  // 11-29 作者定：条件没满足的键灰着也能点，点了拦一下（规范第九节）。「应用所选的处理」：还有没给路径的「用本地文件代替」、没勾风险的「信任站点并下载」，
  // 或者一个都没选时灰着，悬停说第一条原因；点了照旧由 applyPending 拦下（框或勾标红、晃一下），不标 data-hold（标了 kit.js 会截走这一下）。
  // applyWhy 只看状态、不动页面；正在处理时照旧灰着（applying）
  function applyWhy() {
    const root = pv(); if (!root) return "";
    const rows = $$(".tk2-pnd", root).filter(r => !r.classList.contains("out")), xs = rows.map(r => itemOf(r.dataset.pid)).filter(Boolean);
    if (xs.some(x => x.act === "local" && !x.path.trim())) return "请填写本地文件或文件夹的完整路径";
    if (xs.some(x => x.act === "trust" && !x.ack)) return "请先确认信任该站点的下载";
    if (!xs.some(x => x.act)) return "请至少为一个文件选择处理方式";
    return "";
  }
  function paintApply() {
    const b = pv() && $('[data-act="apply"]', pv()); if (!b || applying) return;
    const why = applyWhy(), now = b.getAttribute("title") ?? b.dataset.tip ?? "";
    if (why) b.setAttribute("aria-disabled", "true"); else b.removeAttribute("aria-disabled");
    if (now !== why) { if (why) b.title = why; else b.removeAttribute("title"); delete b.dataset.tip; }
  }
  async function applyPending() {
    if (applying) return;
    const root = pv(), rows = $$(".tk2-pnd", root).filter(r => !r.classList.contains("out"));
    // 拦下来：选了「用本地文件代替」没给路径、选了「信任站点并下载」没勾风险（框或勾标红、晃一下，同 commons；程序是右上角一条报错提示）
    for (const r of rows) {
      const x = itemOf(r.dataset.pid); if (!x) continue;
      if (x.act === "local" && !x.path.trim()) { const inp = $('[data-k="lpath"]', r); inp.classList.add("bad"); inp.focus({ preventScroll: true }); inp.scrollIntoView({ block: "nearest", behavior: "smooth" }); shake(inp); return; }
      if (x.act === "trust" && !x.ack) { const ck = $('[data-act="ack"]', r); ck.classList.add("bad"); ck.scrollIntoView({ block: "nearest", behavior: "smooth" }); shake(ck); return; }
    }
    const go = rows.filter(r => itemOf(r.dataset.pid)?.act);
    if (!go.length) { pmsg("请至少为一个文件选择处理方式"); shake($('[data-k="plw"]', root)); return; }
    applying = true;
    const btn = $('[data-act="apply"]', root); btn.setAttribute("aria-disabled", "true"); $(".tk2-pl", btn).innerHTML = `${icon("spinner", "tk2-spin")}正在处理`;
    go.forEach(r => r.classList.add("busy"));
    await wait(800);
    const done = [];
    go.forEach(r => {
      const x = itemOf(r.dataset.pid);
      r.classList.remove("busy");
      // 第一次信任下载演示「处理失败」（那一块留着，下面一行红字，还能再点一次）
      if (x.act === "trust" && !trustFailed) {
        trustFailed = true; x.err = "处理失败：多次尝试后仍下载失败（" + x.host + ": 读取超时）";
        $(".tk2-perr", r)?.remove(); r.insertAdjacentHTML("beforeend", `<div class="tk2-perr">${icon("warn")}<span>${esc(x.err)}</span></div>`); hy(r);
        r.lastElementChild.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT });
        return;
      }
      done.push([r, x]);
    });
    // 处理好的那几块往右划 32 像素淡掉再收起（规范第七节「清单加减」）；跳过的挪进最后那一行；结果记进日志（Go：处置结果记进任务日志）
    done.forEach(([r, x], i) => {
      T.pending = T.pending.filter(y => y !== x); T.skipped = T.skipped.filter(y => y !== x);
      if (x.act === "skip") T.skipped.push({ ...x, act: "", ack: false, path: "", err: "" });
      T.log.push(`已处理：${x.p}（${ACT[x.act]}）`);
      r.classList.add("out");
      const h = r.offsetHeight + "px";
      r.animate([
        { opacity: 1, transform: "none", height: h, easing: IN },
        { opacity: 0, transform: "translateX(32px)", height: h, offset: .55, easing: OUT },
        { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-8px", paddingTop: "0px", paddingBottom: "0px" },
      ], { duration: 320, delay: i * 40, fill: "forwards" }).onfinish = () => { r.remove(); afterApply(); };
    });
    if (!done.length) afterApply();
    applying = false;
    btn.removeAttribute("aria-disabled"); $(".tk2-pl", btn).textContent = "应用所选的处理";
    paintApply(); // 11-29：处理完以后剩下的那几块还够不够条件
    paintHead(false); paintLog(false); paintTabs(false); // 标题的悬停看还有没有没装上的（T_PACK 的 doneSub）
  }
  // 处理以后：最后那一行的数、上面那排批量键（没有能信任下载的了就收起）、清单空了那一句
  function afterApply() {
    const root = pv(); if (!root) return;
    const l = $('[data-k="plist"]', root), fold = $('[data-act="pfold"]', l), open = fold?.classList.contains("open");
    if (T.skipped.length) {
      if (fold) setText($("span", fold), `已跳过 ${T.skipped.length} 个`);
      else { l.insertAdjacentHTML("beforeend", foldHTML()); hy(l); }
      // 展开着的：新跳过的那一个接在后面
      if (open) T.skipped.forEach(x => { if (!$(`[data-pid="${x.id}"]`, l)) { l.insertAdjacentHTML("beforeend", pndHTML(x, true)); const el = l.lastElementChild; hy(el); placeSeg($("[data-pseg]", el), false); el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT }); } });
    } else fold?.remove();
    const want = doneLine(), have = $(".tk2-pdone", l);
    if (want && (!have || have.outerHTML.replace(/<svg[\s\S]*?<\/svg>/, "") !== want.replace(/<svg[\s\S]*?<\/svg>/, ""))) {
      have?.remove(); l.insertAdjacentHTML("afterbegin", want); const d = l.firstElementChild; hy(d);
      d.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: OUT });
    } else if (!want) have?.remove();
    const pb = $('[data-k="pbar"]', root), nb = pbarHTML(); if (pb._h !== nb) { pb.innerHTML = nb; hy(pb); pb._h = nb; }
    $('[data-act="apply"]', root).closest(".tk2-pfoot").hidden = !T.pending.length && !T.skipped.some(x => x.act);
    $('[data-k="plw"]', root)._paint?.();
    paintApply();
  }
  function pndFold(b) {
    const open = b.classList.toggle("open"), l = b.parentElement;
    if (open) T.skipped.forEach((x, i) => {
      l.insertAdjacentHTML("beforeend", pndHTML(x, true)); const el = l.lastElementChild; hy(el); placeSeg($("[data-pseg]", el), false);
      el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: OUT, fill: "backwards" });
    });
    else $$(".tk2-pnd.skipped", l).forEach(el => el.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: IN, fill: "forwards" }).onfinish = () => { el.remove(); $('[data-k="plw"]', pv())._paint?.(); });
    $('[data-act="apply"]', pv()).closest(".tk2-pfoot").hidden = false;
    setTimeout(() => { const w = $('[data-k="plw"]', pv()); w._paint?.(); if (open) $(".list", w).scrollTo({ top: $(".list", w).scrollHeight, behavior: "smooth" }); }, 30);
  }

  // ---- 提示（任务的结构化告警，程序 drawMsgs 里的 warn-card） ----
  function warnHTML() {
    return `<div class="tk2-wv"><div class="tk2-lw"><div class="list">${shownWarnings().map(w => `<div class="tk2-wb"><div class="tk2-wt" title="${esc(w.note)}">${icon("warn")}<span>${esc(w.title)}</span></div>`
      + ((w.items || []).length ? `<ul class="tk2-wi">${w.items.map(it => `<li>${it.url ? `<a href="#" data-act="ext" title="${esc(it.url)}">${esc(it.name)}${icon("share")}</a>` : it.mono ? `<span class="mono">${esc(it.name)}</span>` : esc(it.name)}</li>`).join("")}</ul>` : "")
      + (w.act ? `<div class="tk2-wa"><button type="button" class="tk2-b sm" data-act="${w.act[0]}" title="${esc(w.act[2] || "")}">${w.act[0] === "retry" ? icon("refresh") : ""}${w.act[1]}</button></div>` : "")
      + `</div>`).join("")}</div><i class="thumb"></i></div></div>`;
  }

  // ================= 底部：胶囊和左右两颗按钮 =================
  function pillSpecs() {
    const K = T?.K;
    const away = () => ({ k: "away", t: !K || K.from === "new" ? "返回主页" : "返回服务器", tip: "任务将在后台继续运行，可在首页上方查看进度" });
    if (ST === "gone") return [null, DOCK === "sink" ? away() : null];
    let L = null, R = null;
    const edit = tip => DRAFT && K.edit ? { k: "edit", t: "返回修改", back: true, tip } : null;
    if (!T.ended) {
      if (!T.cancelling && K.T.cxBtn && ST !== "lost") L = { k: "cancel", t: K.T.cxBtn };
      if (DOCK === "sink") R = away();
    } else if (T.cancelled) {
      L = edit("已填写的配置会保留，点击「返回修改」可返回表单继续调整。");
      if (T.kind === "map") R = { k: "go", t: "返回服务器", pri: true };
    } else if (T.err) {
      const failHint = typeof K.T.failHint === "function" ? K.T.failHint(T.srv) : K.T.failHint;
      L = edit("已填写的配置会保留，点击「返回修改」可返回表单继续调整。");
      if (T.kind === "map") R = { k: "go", t: "返回服务器", pri: true, tip: failHint };
      else if (DRAFT) R = { k: "retry", t: "重试", ic: "refresh", pri: true, tip: T.leftover ? "" : failHint };
    } else R = { k: "go", t: K.T.go, ic: "check", pri: true };
    if (DOCK === "sink" && !L && !R) R = away();
    return [L, R];
  }
  const pillHTML = sp => `<span class="tk2-pl">${sp.back ? icon("chevronRight") : sp.ic ? icon(sp.ic) : ""}${esc(sp.t)}</span>`;
  function makePill(sp) {
    const b = Object.assign(document.createElement("button"), { type: "button", className: "tk2-pill" + (sp.pri ? " pri" : "") + (sp.back ? " back" : "") + (sp.ic && sp.pri ? " ic" : "") });
    b.dataset.act = sp.k; if (sp.tip) b.title = sp.tip; b.innerHTML = pillHTML(sp); b._sig = JSON.stringify(sp); return b;
  }
  // 浮上来、沉下去：同向导的上一步、下一步（0.32 秒，cubic-bezier(.4, 0, .2, 1)）
  const rise = b => b.animate([{ transform: "translateY(180%)", opacity: 0 }, { transform: "none", opacity: 1 }], { duration: 320, easing: SINK });
  function sinkPill(b) { b.classList.add("gone"); b.style.pointerEvents = "none"; b.animate([{ transform: "none", opacity: 1 }, { transform: "translateY(180%)", opacity: 0 }], { duration: 320, easing: SINK, fill: "forwards" }).onfinish = () => b.remove(); }
  // 同一颗按钮换字：旧字 0.09 秒淡掉，按钮宽度按整像素平滑变，新字淡进来（同向导「下一步」变「创建服务器」）
  function morphPill(b, sp) {
    const sig = JSON.stringify(sp); b._sig = sig;
    const lbl = $(".tk2-pl", b);
    lbl.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: IN, fill: "forwards" }).onfinish = () => {
      if (b._sig !== sig) return;
      const w0 = b.offsetWidth; b.style.width = "";
      b.className = "tk2-pill" + (sp.pri ? " pri" : "") + (sp.back ? " back" : "") + (sp.ic && sp.pri ? " ic" : ""); b.dataset.act = sp.k;
      if (sp.tip) b.title = sp.tip; else { b.removeAttribute("title"); delete b.dataset.tip; }
      b.innerHTML = pillHTML(sp); const w1 = b.offsetWidth;
      $(".tk2-pl", b).animate([{ opacity: 0 }, { opacity: 1 }], { duration: 220, delay: 60, easing: OUT, fill: "backwards" });
      if (w0 === w1) return;
      const t0 = performance.now();
      const step = now => { if (b._sig !== sig) return; const p = Math.min(1, (now - t0) / 320), e = 1 - Math.pow(1 - p, 3); b.style.width = Math.round(w0 + (w1 - w0) * e) + "px"; if (p < 1) requestAnimationFrame(step); else b.style.width = ""; };
      b.style.width = w0 + "px"; requestAnimationFrame(step);
    };
  }
  function setPill(slot, sp, instant) {
    const cur = [...slot.children].find(c => !c.classList.contains("gone")) || null, sig = sp ? JSON.stringify(sp) : "";
    if ((cur?._sig || "") === sig) return;
    if (!sp) { if (cur) sinkPill(cur); return; }
    if (!cur) { const b = makePill(sp); slot.appendChild(b); if (!instant) rise(b); return; }
    // 主键和普通的换来换去（颜色不一样）：沉下去一颗、浮上来一颗；同一种只换字
    if (!!JSON.parse(cur._sig).pri !== !!sp.pri) { sinkPill(cur); const b = makePill(sp); slot.appendChild(b); rise(b); return; }
    morphPill(cur, sp);
  }
  function paintPills(instant) { const [L, R] = pillSpecs(); setPill($("#tk2-pl"), L, instant); setPill($("#tk2-pr"), R, instant); }
  function paintCap() {
    // 胶囊里亮哪一颗：详情页发起的（换 Java、导出、待装清单、换地图，程序侧栏亮「服务器」）亮主页，同详情页；新建、导入、接管不亮（「新建」本来就是唯一实心的那颗）
    const lit = T && T.K.from !== "new";
    $(".tk2-cap a.home").classList.toggle("on", !!lit);
  }

  // ================= 一整轮画 =================
  function paintAll(first, pillsInstant = first) {
    paintHead(first); paintSteps(first); paintProg(first); paintNote(first); paintTabs(first); paintLog(first); paintPills(pillsInstant); paintCap();
  }

  // ================= 确认框（照 detail-world 的 ask，危险的默认落在「取消」那一侧） =================
  let mask = null;
  function ask({ icon: ic, title, body, ok, keep, onOk, w = 400 }) {
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "tk2-mask enter" });
    mk.innerHTML = `<div class="tk2-md" role="dialog" aria-modal="true" style="--w:${w}px"><div class="tk2-mc"><div class="tk2-mh"><span class="tk2-mi">${icon(ic)}</span><h3>${title}</h3></div>`
      + `<div class="tk2-mb">${body}</div></div><div class="tk2-ma"><button type="button" class="tk2-b fk" data-x="no">${esc(keep)}</button><button type="button" class="tk2-b dgr" data-x="ok">${esc(ok)}</button></div></div>`;
    document.body.appendChild(mk);
    const no = $("[data-x=no]", mk); no.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return;
      if (e.target.closest("[data-x=ok]")) { closeAsk(); onOk(); return; }
      if (e.target.closest("[data-x=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) closeAsk(); });

  // ================= 动作 =================
  function askCancel() {
    if (!T || T.ended || T.cancelling) return;
    const K = T.K;
    ask({ icon: "stop", title: K.ask.title(T.srv), body: K.ask.body, ok: K.T.cxBtn, keep: K.ask.keep, onOk: () => {
      if (T.ended) return;
      T.cancelling = true; paintAll(false); // 程序：取消收下以后还在清理半成品，任务没结束；取消键没了，下面那行字换成「正在取消……」
      const me = T; setTimeout(() => { if (T !== me || T.ended) return; endCancel(); paintAll(false); }, 1400);
    } });
  }
  // 换页：整页往走的方向挪 16 像素淡掉（0.14 秒），新的从另一边挪进来（0.26 秒）（规范第七节「换步」；重试是一个新任务）
  function turnPage(rebuild) {
    const page = $("#tk2-page");
    page.getAnimations().forEach(a => a.cancel());
    page.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateX(-16px)" }], { duration: 140, easing: IN, fill: "forwards" }).onfinish = () => {
      rebuild(); page.getAnimations().forEach(a => a.cancel());
      page.animate([{ opacity: 0, transform: "translateX(16px)" }, { opacity: 1, transform: "none" }], { duration: 260, easing: OUT });
    };
  }
  function retry(kindKey, srv) {
    const k = kindKey || T.kind, s = srv || T.srv;
    playTok++; closeAsk(true);
    turnPage(() => { ST = "done"; T = fresh(k, s); build(); paintAll(true, false); play(); });
  }
  function nav(href) { location.href = href; }
  function flashOk(btn) { // 复制以后图标换成勾 1.2 秒（规范第七节）
    if (!btn || btn._ok) return; const old = btn.innerHTML; btn._ok = 1; btn.classList.add("ok"); btn.innerHTML = icon("check");
    setTimeout(() => { btn.innerHTML = old; btn.classList.remove("ok"); btn._ok = 0; }, 1200);
  }
  const copyText = t => { try { navigator.clipboard?.writeText(t).catch(() => {}); } catch { /* 草图里复制不了也不要紧 */ } };
  document.addEventListener("click", e => {
    const tab = e.target.closest("#tk2-seg > span[data-tab]"); if (tab) { pickTab(tab.dataset.tab); return; }
    const ps = e.target.closest("[data-pseg] > span[data-v]"); if (ps) { pndPick(ps.parentElement, ps.dataset.v); return; }
    const a = e.target.closest("[data-act]"); if (!a) return;
    const act = a.dataset.act;
    if (a.getAttribute("aria-disabled") === "true" && act !== "apply") return; // 灰着的「应用所选的处理」点了要拦一下（applyPending；正在处理时它自己不动）
    if (act === "cancel") askCancel();
    else if (act === "retry") retry();
    else if (act === "edit") { const [page, p] = T.K.edit; nav(link(page, p)); }
    else if (act === "go") nav(T.K.go(T.srv));
    else if (act === "away") nav(!T || T.K.from === "new" ? link("home.html", { notices: "1" }) : link("detail.html", { kind: T.srv.detail }));
    else if (act === "gojava") nav(link("detail.html", { kind: T.srv.detail, tab: "settings", sub: "jvm" }));
    else if (act === "copyerr") { copyText(T.err); flashOk(a); }
    else if (act === "copylost") { copyText("无法连接 MineDeck（Failed to fetch）"); flashOk(a); }
    else if (act === "copypath") { copyText(T.result); flashOk(a); }
    else if (act === "opendir" || act === "openinst" || act === "ext") { e.preventDefault(); a.animate([{ opacity: 1 }, { opacity: .4 }, { opacity: 1 }], { duration: 300 }); } // 程序里打开文件夹、外链；草图里只闪一下
    else if (act === "trustall") trustAll();
    else if (act === "ackall") ackAll();
    else if (act === "apply") applyPending();
    else if (act === "pfold") pndFold(a);
    else if (act === "ack") { const x = itemOf(a.closest(".tk2-pnd").dataset.pid); x.ack = !x.ack; a.classList.toggle("on", x.ack); a.classList.remove("bad"); a.setAttribute("aria-checked", String(x.ack)); }
  });
  document.addEventListener("input", e => {
    if (e.target.dataset.k !== "lpath") return;
    const x = itemOf(e.target.closest(".tk2-pnd").dataset.pid); x.path = e.target.value; e.target.classList.remove("bad");
  });
  // 11-29：「没装上的文件」里点了、填了以后（上面处理完），重算「应用所选的处理」灰不灰（见 applyWhy）
  ["click", "input"].forEach(t => document.addEventListener(t, e => { if (e.target.closest?.('#tk2-views > [data-v="pend"]')) paintApply(); }));

  // ================= 自动往下走（?play=1、重试） =================
  // 照这一份过程一样一样走：一步一停、日志一行一行加、下载的那一项 0 到 100% 一格一格亮；走到这个 st 的结尾。
  async function play() {
    const tok = ++playTok, ops = opsOf(T), alive = () => tok === playTok && !T.ended && !T.cancelling;
    const target = ST === "failed" ? "fail" : ST === "slow" ? "slow" : null;
    for (let i = 0; i < ops.length; i++) {
      const op = ops[i];
      if (op.m) {
        if (op.m !== target || !alive()) continue;
        if (target === "fail") { await wait(op.f != null ? 0 : 400); if (!alive()) return; if (op.f != null) await download(ops.slice(i + 1).find(o => "p" in o), op.f, alive); if (!alive()) return; endFail(); paintAll(false); return; }
        // 下载慢：卡在这里，程序的轮询每 0.8 秒一轮；net 的每次重试数加一，idle 的「已经 N 多秒」往上走
        if (op.f != null) await download(ops.slice(i + 1).find(o => "p" in o), op.f, alive, true);
        const S = T.K.slow;
        if (WHY === "net" && S.net) for (let c = 1; c <= S.net.count && alive(); c++) { T.log.push(S.pre[c - 1] || S.pre[0]); T.net = { ...S.net, count: c }; paintAll(false); await wait(1300); }
        else for (const sec of [30, 41, 52]) { if (!alive()) return; T.idle = sec; paintAll(false); await wait(1300); }
        continue;
      }
      if (!alive()) return;
      if ("fin" in op) continue;
      if ("p" in op) { await download(op, 1, alive); continue; }
      await wait("s" in op ? 380 : 240);
      if (!alive()) return;
      applyOp(op); paintAll(false);
    }
    if (!alive()) return;
    await wait(300); if (!alive()) return;
    finish(finOf(ops)); if (ST === "partial" && T.K.partial) endPartial();
    paintAll(false);
    // 新建时挑了插件 / 模组（?cart=1）：建好以后接着装待装清单（程序是留在向导等建服跑完，再跳到装清单的任务页）
    if (CART && T.kind === "create" && !T.err) { await wait(1100); if (tok !== playTok) return; const s = T.srv; turnPage(() => { T = fresh("cart", s); build(); paintAll(true, false); play(); }); }
  }
  // 一项下载：从 from 走到 to（0..1），每 0.09 秒一格；中途被取消、换任务就停
  async function download(op, to, alive, from0) {
    const start = from0 ? 0 : (T.label && keyOf(T.label) === keyOf(op.p) ? T.done / op.b : 0);
    const n = Math.max(6, Math.round(18 * (to - start)));
    for (let k = 1; k <= n; k++) {
      if (!alive()) return;
      applyOp(op, start + (to - start) * k / n); paintAll(false);
      await wait(90);
    }
  }

  // ================= 草图切换条 =================
  function skBar() {
    const q0 = () => new URLSearchParams(location.search);
    const href = mut => { const q = q0(); q.delete("play"); q.delete("enter"); mut(q); return "?" + q; };
    const a = (h, label, on, cls = "", tip = "") => `<a href="${h}" class="${on ? "on" : ""}${cls ? " " + cls : ""}"${tip ? ` title="${esc(tip)}"` : ""}>${label}</a>`;
    const th = document.documentElement.dataset.theme;
    const kinds = Object.entries(KINDS).map(([k, K]) => a(href(q => { q.set("kind", k); if (!legal(k, ST)) q.delete("st"); }), K.label, k === KIND, "",
      k === "pack" ? "导入整合包（Go 里现在和新建是同一类任务、多两步，作者 11-29 定后端给它自己的种类；有文件没装上的「部分」只在这一种）" : ""));
    const naTip = s => s === "cancelled" ? "这类任务程序里不能取消（没有取消键），切过去会落到「进行中」" : s === "partial" ? (KIND === "create" ? "从零创建不会有没装上的文件；看「整合包」" : "这类任务没有要单独处理的结果（要留意的只写在日志里）") : "";
    const sts = Object.entries(STATES).map(([s, label]) => a(href(q => q.set("st", s)), label, s === ST, legal(KIND, s) ? "" : "na", legal(KIND, s) ? "" : naTip(s)));
    $("#tk2-sk").innerHTML = `<b>草图</b><b>主题</b>${a(href(q => q.delete("theme")), "深", th === "dark")}${a(href(q => q.set("theme", "light")), "浅", th === "light")}<i></i>`
      + `<b>任务</b>${kinds.join("")}<i></i><b>状态</b>${sts.join("")}`;
    // 右上角第二条：跟着状态变的几样
    const ex = [];
    ex.push(`<b>底部</b>${a(href(q => q.delete("dock")), "浮起", DOCK === "float", "", "胶囊浮起来，左右两颗按钮（推荐）")}${a(href(q => q.set("dock", "sink")), "沉下", DOCK === "sink", "", "胶囊沉着（同向导），进行中右下多一颗「回到主页」当出路")}`);
    if (ST !== "gone" && ST !== "lost") ex.push(`<a href="${href(q => q.set("play", "1"))}" title="从第一步自动走到这个状态的结尾（失败就在出错那一步停下，慢就在那里卡一会儿再接着走）">▶ 跑一遍</a>`);
    if (KIND === "create" && ["running", "done"].includes(ST)) ex.push(`<a href="${href(q => { q.set("play", "1"); q.set("cart", "1"); q.set("st", "done"); q.set("enter", "1"); })}" title="从向导的「创建服务器」进来，第二步挑过插件：建好以后接着装待装清单">从向导进来 · 带清单</a>`);
    if (ST === "slow" && NET_SLOW.includes(KIND)) ex.push(`<b>慢</b>${a(href(q => q.delete("why")), "在重试", WHY === "net")}${a(href(q => q.set("why", "idle")), "没进度", WHY === "idle")}`);
    if (ST === "failed" && KINDS[KIND].fail.leftover) ex.push(a(href(q => LEFT ? q.delete("leftover") : q.set("leftover", "1")), "没删干净", LEFT, "", "失败以后半成品没清干净（快照里 cleanupFailed）"));
    if ((ST === "failed" && KIND !== "map") || (ST === "cancelled" && KINDS[KIND].edit)) ex.push(a(href(q => DRAFT ? q.set("draft", "0") : q.delete("draft")), "没有草稿", !DRAFT, "", "这一页刷新过，或者从首页浮层点进来的：记不得这个任务是拿什么发起的，没有「返回修改」「重试」"));
    if ((KIND === "export" && ST === "done") || (KIND === "pack" && ST === "partial")) ex.push(a(href(q => LOCAL ? q.set("local", "0") : q.delete("local")), "别的设备", !LOCAL, "", "手机远程管理、浏览器打开：没有「打开文件夹」"));
    $("#tk2-sk2").innerHTML = ex.join("<i></i>");
  }

  // 细滚动条（同 settings.html 的 thinBar）
  function thinBar(w) {
    const l = w.querySelector(".list"), t = w.querySelector(".thumb");
    const paint = () => {
      const ch = l.clientHeight, sh = l.scrollHeight, st = l.scrollTop;
      w.classList.toggle("fits", sh <= ch + 1); w.classList.toggle("can-up", st > 2); w.classList.toggle("can-down", st + ch < sh - 2);
      const h = Math.max(28, ch * ch / sh);
      t.style.height = h + "px"; t.style.top = (sh > ch ? st / (sh - ch) * (ch - h) : 0) + "px";
    };
    if (w._paint) { w._paint(); return; }
    w._paint = paint;
    let idle = 0;
    l.addEventListener("scroll", () => { paint(); w.classList.add("scrolling"); clearTimeout(idle); idle = setTimeout(() => w.classList.remove("scrolling"), 700); }, { passive: true });
    t.addEventListener("pointerdown", e => {
      e.preventDefault(); t.setPointerCapture(e.pointerId); t.classList.add("drag");
      const y0 = e.clientY, s0 = l.scrollTop, ratio = (l.scrollHeight - l.clientHeight) / (l.clientHeight - t.offsetHeight);
      const move = ev => { l.scrollTop = s0 + (ev.clientY - y0) * ratio; };
      const up = () => { t.classList.remove("drag"); t.removeEventListener("pointermove", move); t.removeEventListener("pointerup", up); };
      t.addEventListener("pointermove", move); t.addEventListener("pointerup", up);
    });
    new ResizeObserver(paint).observe(l);
    paint();
  }

  // ================= 开始 =================
  window.KIT_READY.then(() => {
    skBar();
    if (DOCK === "sink" || ENTER) document.body.classList.add("tk2-capdown");
    if (ST === "gone") { buildGone(); paintPills(true); paintCap(); }
    else {
      if (PLAY) { T = fresh(KIND, ST === "partial" && KINDS[KIND].partial?.srv || KINDS[KIND].srv); build(); paintAll(true); }
      else { snapshot(KIND, ST); build(); paintAll(true); }
    }
    // 从向导进来（?enter=1）：内容从右边挪进来（同向导换步的进场），胶囊从下面浮上来，左下的按钮浮上来
    if (ENTER) {
      $("#tk2-page").animate([{ opacity: 0, transform: "translateX(16px)" }, { opacity: 1, transform: "none" }], { duration: 260, easing: OUT });
      $$(".tk2-pill").forEach(rise);
      requestAnimationFrame(() => requestAnimationFrame(() => { if (DOCK === "float") document.body.classList.remove("tk2-capdown"); }));
    }
    if (PLAY && ST !== "gone") setTimeout(play, ENTER ? 420 : 300);
    window.__tk2 = { get T() { return T; }, V, paintAll }; // 给量尺寸的脚本用
  });
})();
