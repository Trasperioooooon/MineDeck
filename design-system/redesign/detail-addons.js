// 服务器详情页 →「插件」/「模组」（标签 addons；Paper 一系和代理端叫插件，Forge 这类叫模组，原版没有这个标签）。11-27 夜里派子代理照临时规范画的第一稿，
// 计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」。样式在 detail-addons.css，选择器、data 属性都带 ad 前缀。
//
// 程序现在的样子（internal/web/ui/app.js 约 4318 行「view === "addons"」、modcart.js 的 buildPanel）：上半截「已安装的插件」
//   （「plugins 文件夹里共 20 个，1 个已停用」、刷新；每个文件一行：名字、已停用标记、文件名 · 版本、读不出信息时一行原因，「停用 / 启用」「删除」两个按钮，
//   要先停服；一段说明讲停用是改名成 .jar.disabled、删除是移进 .minedeck\已删除），中间「整合包里没装上的」（有才出），
//   下半截「添加插件」：搜索框 +「搜索」按钮、类别片、热门 / 搜索结果（每条「加入清单」，装过的标「已安装」，往后翻）、
//   右边待装清单（移除、从电脑添加…、全部安装（N）、清空）和「检查前置」，还有一块「已装的插件里缺 N 项前置」的提醒。
// 这里：内容面板里左右两栏，同向导第二步「选择预装插件」的排法（左边大约 70%、右边待装清单），左边那栏顶上一颗 32 高的分段「已安装 · 添加」换着看：
//   · 已安装：一行 44 高（同向导待装清单的行）：开关（启用 / 停用，在字的左边，同规范）、图标（Modrinth 上认得出的用它的图标，认不出的用首字母块）、
//     名字和文件名、标记（缺前置、读不出信息、装上了要重启才生效），删除悬停这一行才出来。停用的那一行淡下去、文件名跟着变成 .jar.disabled。
//     运行中开关和删除灰掉、悬停说原因（程序原话）；程序那段说明拆进开关和删除的悬停。顶上的搜索框在这里筛已装的（程序没有，几十个模组时好找）。
//     「缺前置 Chunky」点了直接去「添加」、搜好 Chunky（程序是一块提醒，让人自己去下面搜）。
//   · 添加：和向导第二步一模一样（类别片、两列卡片、往后翻、卡片上的 ＋、去 Modrinth 看介绍），多一种卡片：已经装在服务器里的标「已安装」、点不了。
//     卡片数据是 Modrinth 上真的前 100 个（modrinth-sample.js，向导第二步那份）：插件是 Paper 26.2 的，模组是 NeoForge 26.1 的，
//     所以 Forge 1.20.1、Velocity 这两台看到的卡片其实对不上版本，「适配 …」那句照程序写，悬停里注明这是草图样本。
//   · 右边待装清单：同向导（加一个就把它要的前置一起放进来、标「前置」；拿掉的往右划掉；「清空」从上往下一行接一行划掉），底下「拖入或选择 .jar」
//     和「全部安装 · 3」（40 高、圆角 10，离栏边 8 同心；清单空着时灰掉）。装完清单一行行划掉，左边已安装里多出这几行；运行中装上的标「重启后生效」。
//     程序要点「检查前置」再勾，向导第二步已经改成自动补前置，这里照向导；装之前「另外会自动装上 N 项必装前置」那个确认框也就用不着了。
//   · 栏右上角是「打开插件文件夹」（外壳给的 ctx.folders），在待装清单那一栏的角上，也就是这一页的右上角。
// 已安装的名单是编的（真实常见的插件、机械动力整合包常见的模组）。Paper 那台装了 Simple Voice Chat（联机页的语音隧道照它画的），
// Forge 那台有控制台日志里出现过的 JEI（jei-1.20.1-forge-15.3.0.4.jar）和 Create Crafts & Additions（createaddition）。
// 页头有「整合包里有文件没装上」（?notice=3 或 4，只在 Forge）时，已安装最上面列出那 3 个文件（程序这一页的「整合包里没装上的」），
// 每行「处理…」程序是打开共用件页画过的那个「没装上的文件」弹窗，草图里没接（同页头那颗「处理」）。
//
// 11-28 作者审完这一页以后加的（作者的决定写在 docs/计划/2026-11-27-界面重构-待作者审.md 这一节各条后面）：
//   · 更新（程序没有，新功能）：已安装里有新版本的那一行多一个「可更新 5.2.1 → 5.3.2」标记和一颗「更新」，栏上「全部更新 · N」（只在「已安装」那边）。
//     更新一个时和它关联的一起更新、不另外确认：它要的前置（新版本要配套的前置版本）、依赖它的（要换成配套版本），关系就是那一行的 need（同「缺前置」用的那个）。
//     更新完文件名换成新版本的，标记换成「已更新 5.3.2」，被带上的那几行是「随 ViaVersion 更新」、悬停写为什么带上它，几行一起闪一下。
//     运行中「更新」「全部更新」灰掉、悬停说原因（同开关、删除）。停用着的也能更新，.disabled 后缀留着。只有在 Modrinth 上认得出的（有 id）才可能有更新。
//     草图里就地转一下再换；程序里该是后台任务（下载、换文件），进度页 task.html 还没有这一种。新版本号是编的。
//   · 待装清单空着时只留一句淡字「暂未选择任何插件」，向导里那句「点击左侧 ＋ 添加，前置依赖将自动补全。」详情页不要了。
//   · 新建时选择的尚未安装（程序 modcart.js 的 paintDraftBox，待修清单 A31：向导里挑了、建服时没装上或切走了，草稿清单记着是这台的）：
//     待装清单最上面一块，列出那几项，「一并安装」（连同待装清单，跳进度页）和「清除」（只清这几项）。程序那句解释收进标题的悬停。
//     ?draft=1 是三项都正常；?draft=2 是程序会标出来的几种：核心不符（模组服是版本不同）、无法安装、本地文件与服务器不符。
//   · 前置检查的三种结果（程序 modcart.js 的 paintDeps，后端 internal/modcart/closure.go 的 Optional、Conflicts、Unresolved）。程序要点「检查前置」才出，
//     这里清单一变就算好、画在清单里（同自动补前置）；三种都只是提醒，不拦「全部安装」（程序也照装：能装的装上，未找到的跳过）：
//     可选前置：清单末尾「可选前置」小标题下面一行一个，行尾 ＋ 加进清单；只列清单里直接挑的那几项要的，已装的、已在清单的不列（同后端）。
//     冲突：冲突的那一行下面那句换成警告色的「与 … 冲突」「与已安装的 … 冲突」，全文进悬停；拖入框上面一句「存在 N 处冲突，安装前请确认」。
//     未找到（程序叫「查不到的前置」）：要它的那一项下面多一行，标「前置」、警告色「未找到可用版本」，不算进个数，也拿不掉；拖入框上面一句「N 项前置未找到，安装时将跳过」。
//     ?dep=opt、?dep=clash、?dep=lost 各预先往清单里放好能看到这种结果的几项；可选前置是真数据、平时挑到也会出，冲突和未找到是编的、只在带着这个参数时才算（OPT、CLASH、LOST 那几张表，哪些是真的、哪些是编的写在表上）。
(() => {
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", EASE_SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const hy = el => { window.KIT.hydrate(el); return el; };
  const icon = (n, cls) => window.KIT.icon(n, cls);
  let C = null;

  // 向导第二步那份 Modrinth 样本：外壳没引，这里自己加载一次；没到之前先出骨架卡，到了再填
  if (!window.MR_SAMPLE && !document.querySelector("script[data-ad-sample]")) {
    const s = document.createElement("script"); s.src = "modrinth-sample.js"; s.dataset.adSample = "1";
    s.onload = () => sampleArrived(); document.head.appendChild(s);
  }

  // ================= 数据 =================
  const PLUGIN_CORES = ["paper", "purpur", "leaves", "folia", "velocity", "waterfall"];
  const nounOf = core => PLUGIN_CORES.includes(core) ? "插件" : "模组";
  const dirOf = core => PLUGIN_CORES.includes(core) ? "plugins" : "mods";
  // 已安装（程序 /api/instances/{名称}/mods 的 file、name、version、disabled、err；id 是在 Modrinth 上认出来的项目，认得出才有图标、才在「添加」里标「已安装」）
  // need 是它要的前置 [id, 名字]：没装（或停用着）时标「缺前置」，更新时两头互相带着。up 是有新版本 [现在的, 新的]（程序没有；新版本号是编的），
  // 每台挑了几个，其中一组有关联：Paper 的 ViaBackwards 要 ViaVersion，Forge 的 Sophisticated Backpacks 要 Sophisticated Core（样本里 Modrinth 上真的必装前置）
  const I = (n, file, id = "", x = {}) => ({ n, file, id, ...x });
  const SEED = {
    paper: [
      I("BlueMap", "bluemap-5.5-paper.jar", "bluemap"), I("ChunkyBorder", "ChunkyBorder-1.2.23.jar", "chunkyborder", { need: ["chunky", "Chunky"] }),
      I("CoreProtect", "CoreProtect-CE-23.1.jar", "coreprotect"), I("DiscordSRV", "DiscordSRV-Build-1.29.0.jar.disabled", "discordsrv", { off: true, up: ["1.29.0", "1.30.2"] }),
      I("EssentialsX", "EssentialsX-2.21.0.jar"), I("EssentialsXChat", "EssentialsXChat-2.21.0.jar"), I("Floodgate", "floodgate-spigot.jar"),
      I("Geyser", "Geyser-Spigot.jar", "geyser"), I("LuckPerms", "LuckPerms-Bukkit-5.4.145.jar", "luckperms", { up: ["5.4.145", "5.4.151"] }), I("Multiverse-Core", "multiverse-core-4.3.14.jar", "multiverse-core"),
      I("MyCommand", "MyCommand-1.0.jar", "", { err: "压缩包损坏，无法读取 plugin.yml" }), I("PlaceholderAPI", "PlaceholderAPI-2.11.6.jar", "placeholderapi"),
      I("Simple Voice Chat", "voicechat-bukkit-2.5.26.jar", "simple-voice-chat"), I("SkinsRestorer", "SkinsRestorer.jar", "skinsrestorer"), I("spark", "spark-1.10.119-bukkit.jar", "spark"),
      I("TAB", "TAB v5.0.3.jar", "tab-was-taken"), I("Vault", "Vault.jar"), I("ViaBackwards", "ViaBackwards-5.2.1.jar", "viabackwards", { need: ["viaversion", "ViaVersion"], up: ["5.2.1", "5.3.2"] }),
      I("ViaVersion", "ViaVersion-5.2.1.jar", "viaversion", { up: ["5.2.1", "5.3.2"] }),
      I("WorldEdit", "worldedit-bukkit-7.3.10.jar", "worldedit"), I("WorldGuard", "worldguard-bukkit-7.0.13-dist.jar", "worldguard"),
    ],
    forge: [
      I("AppleSkin", "appleskin-forge-mc1.20.1-2.5.1.jar", "appleskin"), I("Create", "create-1.20.1-0.5.1.j.jar"), I("Create Crafts & Additions", "createaddition-1.20.1-1.2.4e.jar"),
      I("Create Deco", "createdeco-2.0.2-1.20.1-forge.jar"), I("Create Slice & Dice", "sliceanddice-forge-3.3.0.jar"), I("Create: Big Cannons", "createbigcannons-5.5.0+mc.1.20.1-forge.jar"),
      I("Create: Enchantment Industry", "create_enchantment_industry-1.20.1-for-create-0.5.1.f-1.2.9.d.jar"), I("Create: Steam 'n' Rails", "Steam_Rails-1.6.7+forge-mc1.20.1.jar"),
      I("Curios API", "curios-forge-5.11.0+1.20.1.jar", "curios"), I("Farmer's Delight", "FarmersDelight-1.20.1-1.2.6.jar"), I("FerriteCore", "ferritecore-6.0.1-forge.jar", "ferrite-core"),
      I("GeckoLib", "geckolib-forge-1.20.1-4.4.9.jar", "geckolib"), I("Jade", "Jade-1.20.1-Forge-11.12.3.jar", "jade", { up: ["11.12.3", "11.13.1"] }), I("JourneyMap", "journeymap-1.20.1-5.10.3-forge.jar.disabled", "journeymap", { off: true }),
      I("Just Enough Items (JEI)", "jei-1.20.1-forge-15.3.0.4.jar", "jei"), I("ModernFix", "modernfix-forge-5.19.4+mc1.20.1.jar", "modernfix"), I("Mowzie's Mobs", "mowziesmobs-1.6.4.jar"),
      I("Patchouli", "Patchouli-1.20.1-84-FORGE.jar", "patchouli"),
      I("Sophisticated Backpacks", "sophisticatedbackpacks-1.20.1-3.21.2.1155.jar", "sophisticated-backpacks", { need: ["sophisticated-core", "Sophisticated Core"], up: ["3.21.2.1155", "3.23.4.1206"] }),
      I("Sophisticated Core", "sophisticatedcore-1.20.1-1.0.4.818.jar", "sophisticated-core", { up: ["1.0.4.818", "1.2.9.901"] }), I("spark", "spark-1.10.53-forge.jar", "spark"),
    ],
    proxy: [
      I("Floodgate", "floodgate-velocity.jar"), I("Geyser", "Geyser-Velocity.jar", "geyser"), I("LibertyBans", "LibertyBans-1.1.0.jar"), I("LuckPerms", "LuckPerms-Velocity-5.4.145.jar", "luckperms", { up: ["5.4.145", "5.4.151"] }),
      I("MiniMOTD", "minimotd-velocity-2.1.4.jar"), I("SkinsRestorer", "SkinsRestorer.jar", "skinsrestorer"), I("spark", "spark-1.10.119-velocity.jar", "spark", { up: ["1.10.119", "1.10.124"] }),
      I("TAB", "TAB v5.0.3.jar", "tab-was-taken"), I("ViaVersion", "ViaVersion-5.2.1.jar", "viaversion"),
    ],
  };
  // 整合包里没装上的（同共用件页「没装上的文件」那三个的说法，换成这台 Forge 1.20.1 的文件名）
  const PENDING = [
    ["create_new_age-forge-1.20.1-1.1.2.jar", "站点不在允许列表中", "下载地址不在允许列表中（仅允许 Modrinth、GitHub、GitLab），确认站点可信后才能下载"],
    ["createoreexcavation-1.20-1.4.3.jar", "未配置 CurseForge API Key", "未配置 CurseForge API Key，无法自动下载。可保留本地已有文件，或手动放入"],
    ["create-pack-tweaks-1.0.jar", "整合包未提供校验值", "整合包未提供此文件的校验值，自动下载无法校验完整性，无法替代本地文件"],
  ];
  const LOCAL = { paper: ["SimpleTpa-1.4.jar", "46 KB"], forge: ["create_factory-0.4b-1.20.1.jar", "1.2 MB"], proxy: ["VelocityWhitelist-1.1.jar", "38 KB"] };
  // 类别片：同向导第二步（程序叫「支持库」，作者 11-26 改叫「前置库」）
  const ACATS = {
    模组: [["optimization", "优化"], ["utility", "实用"], ["management", "管理"], ["worldgen", "世界生成"], ["adventure", "冒险"], ["technology", "科技"],
      ["magic", "魔法"], ["mobs", "生物"], ["equipment", "装备"], ["storage", "存储"], ["library", "前置库"]],
    插件: [["management", "管理"], ["utility", "实用"], ["optimization", "优化"], ["economy", "经济"], ["social", "聊天社交"], ["minigame", "小游戏"],
      ["game-mechanics", "游戏机制"], ["worldgen", "世界生成"], ["library", "前置库"]],
  };
  // ---- 11-28 加的几种状态用的示例（只这一页用，没往 modrinth-sample.js 里加） ----
  // 可选前置（真的）：2026-11-28 从 Modrinth /v2/project/<slug>/version（加载器和版本同样本）最新一版的 dependencies 里取的 optional，只留在样本那 100 个里的
  const OPT = { crazycrates: ["fancyholograms", "placeholderapi", "decentholograms"], "multiverse-core": ["multiverse-inventories"], huskhomes: ["bluemap"], crazyenchantments: ["griefprevention", "worldguard"],
    artifacts: ["curios", "cloth-config"], travelersbackpack: ["curios", "jei"], "toms-storage": ["jei"], "gravestone-mod": ["jade"] };
  // 冲突（编的）：样本这 200 个在 Modrinth 上一条 incompatible 都没有，这几对只为看样子。前两对现实里确实不该同装（AuthMe 的两个分支；FAWE 是用来替换 WorldEdit 的），后两对纯属示例
  const CLASH = [["authmereloaded", "authmerereloaded"], ["fastasyncworldedit", "worldedit"], ["diagonal-fences", "macaws-fences-and-walls"], ["servercore", "modernfix"]];
  // 未找到的前置（编的）：被别的项目当前置带进来时，算「要的那个版本在 Modrinth 上已经没有了」（后端 Unresolved 的一种原因）；自己从卡片上挑它时照常能加
  const LOST = ["skript", "prickle"];
  // ?dep= 预先放进清单的几项，各能看到一种结果（Paper 装着 WorldEdit、Forge 装着 ModernFix，所以冲突那组里既有清单里两项之间的，也有和已装的）
  const DEP_DEMO = {
    opt: { 插件: ["crazycrates"], 模组: ["artifacts"] },
    clash: { 插件: ["authmereloaded", "authmerereloaded", "fastasyncworldedit"], 模组: ["diagonal-fences", "macaws-fences-and-walls", "servercore"] },
    lost: { 插件: ["skbee"], 模组: ["enchantment-descriptions"] },
  };
  // ?draft= 新建时选择的尚未安装的那几项（样本里这台没装的）
  const DRAFT = { 插件: ["chunky", "fancynpcs", "grimac"], 模组: ["lithium", "clumps", "carry-on"] };
  const PAGE = 20, CAP = 100;
  const A = {};
  const dat = () => {
    const k = C.kind;
    if (!A[k]) A[k] = { view: "inst", mods: JSON.parse(JSON.stringify(SEED[k] || [])), q: { inst: "", add: "" }, cat: "", picked: [], draft: draftSeed(), installing: false, shown: 0, more: false };
    return A[k];
  };
  const noun = () => nounOf(C.S.core);
  const sample = () => (window.MR_SAMPLE && window.MR_SAMPLE[noun()]) || null;
  const addonOf = id => sample()?.items.find(x => x.id === id);
  // ?draft=1 三项都正常；2 是程序会标出来的几种（modcart.js 的 draftItemMismatch、draftItemRow、draftLocalRow）：给别的核心挑的（模组服是给别的版本挑的）、
  // 无法安装（后端 Unresolved）、本地文件与服务器不符（后端 LocalFile.Warn）。warn 是 [行里的短句, 悬停全文]；行不能单独拿掉（fixed），编号加 draft: 免得和清单里的撞
  function draftSeed() {
    const lv = +C.QS.get("draft") || 0; if (!lv) return null;
    const rows = DRAFT[noun()].map(id => ({ id })), sv = `${C.S.label} ${C.S.mc}`;
    if (lv >= 2) {
      rows[1] = noun() === "插件" ? { id: "lithium", n: "Lithium", warn: ["为 Fabric 选择，核心不符", `此项为 Fabric 选择，与当前服务器的核心（${C.S.label}）不符，可能无法安装`] }
        : { ...rows[1], warn: ["为 1.21.1 选择，版本不同", `此项为 ${C.S.label} 1.21.1 选择，当前服务器为 ${sv}，可能无法安装`] };
      rows[2].warn = ["无法安装：无适用版本", `Modrinth 上未找到适用于 ${sv} 的版本，安装时将跳过此项`];
      const [file, kb] = LOCAL[C.kind] || LOCAL.paper;
      rows.push({ id: "local:" + file, local: true, file, kb, warn: ["适用于 Fabric，核心不符", `此文件适用于 Fabric，当前服务器为 ${C.S.label}，安装后很可能无法使用`] });
    }
    return rows.map(p => ({ ...p, k: "draft:" + p.id, fixed: true }));
  }
  const installedIds = () => new Set(dat().mods.filter(m => !m.off && m.id).map(m => m.id));
  const live = () => C.st !== "stopped";

  // ================= 小件 =================
  const fmtDl = n => n >= 1e8 ? `${+(n / 1e8).toFixed(1)} 亿` : n >= 1e4 ? `${Math.round(n / 1e4)} 万` : `${n}`;
  const HUES = [18, 42, 88, 140, 172, 200, 222, 262, 300, 330];
  // 图标块：Modrinth 认得出的用它的图标（草图不联网时取不到，退回首字母块），认不出的用首字母（同向导第二步的 tile）
  function tile(n, id, iconUrl, cls = "") {
    const key = id || n, letter = esc((n.replace(/^[^A-Za-z]*\s?/, "")[0] || n[0] || "?").toUpperCase()), hue = HUES[[...key].reduce((a, c) => a + c.charCodeAt(0), 0) % HUES.length];
    return `<span class="ad-mt${iconUrl ? " img" : ""}${cls ? " " + cls : ""}" style="--c:hsl(${hue} 32% 46%)">${letter}${iconUrl ? `<img src="${esc(iconUrl)}" alt="" loading="lazy" onerror="this.parentNode.classList.remove('img');this.remove()">` : ""}</span>`;
  }
  const swapKeyed = (box, html, dir = 1, axis = "Y") => {
    if (!box) return;
    const tmp = document.createElement("div"); tmp.innerHTML = html; hy(tmp);
    const tf = v => axis === "X" ? `translateX(${v}px)` : `translateY(${v}px)`;
    let i = 0;
    $$("[data-ad-k]", tmp).forEach(n => {
      const o = box.querySelector(`[data-ad-k="${n.dataset.adK}"]`); if (!o) return;
      if (o.innerHTML === n.innerHTML && o.className === n.className) return;
      const tok = o._tok = (o._tok || 0) + 1, d = i++ * 22;
      o.getAnimations().forEach(a => a.cancel());
      o.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, delay: d, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
        if (o._tok !== tok) return;
        o.innerHTML = n.innerHTML; o.className = n.className; delete o.dataset.tip;
        if (n.hasAttribute("title")) o.setAttribute("title", n.getAttribute("title")); else o.removeAttribute("title");
        o.getAnimations().forEach(a => a.cancel());
        o.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT, fill: "backwards" });
      };
    });
  };
  function growIn(el, delay = 0) {
    const h = el.offsetHeight + "px";
    el.animate([{ opacity: 0, transform: "translateY(5px)", height: "0px", marginBottom: "-2px" }, { opacity: 0, transform: "translateY(5px)", height: h, marginBottom: "0px", offset: .45 },
      { opacity: 1, transform: "none", height: h, marginBottom: "0px" }], { duration: 360, delay, easing: EASE_OUT, fill: "backwards" });
  }
  function dropOut(el, done, delay = 0) {
    el.classList.add("ad-out");
    const h = el.offsetHeight + "px";
    el.animate([
      { opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: "translateX(32px)", height: h, marginBottom: "0px", offset: .55, easing: EASE_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 320, delay, fill: "forwards" }).onfinish = () => { el.remove(); done?.(); };
  }
  const inView = (list, els) => { const top = list.scrollTop, bot = top + list.clientHeight; return els.filter(x => x.offsetTop < bot && x.offsetTop + x.offsetHeight > top); };
  const root = () => document.querySelector(".cp > .ad:not(.fade-out)");

  // ================= 左栏：已安装 =================
  const STOP_WHY = "请先停止服务器再启用、停用、更新或删除：运行中文件正被占用，且修改后需重启生效";
  // 11-29 作者定：条件没满足的键灰着也能点，点了拦一下（规范第九节）：开着时的启用 / 停用、更新、删除，没有待装项时的「全部安装」，
  // 标 aria-disabled 加 data-hold，点了在键上弹出原因（kit.js 第 9 条，没有能当场改的）。正在更新、正在安装的那一颗照旧灰着、点了没反应
  const HOLD_STOP = ` aria-disabled="true" data-hold="${esc(STOP_WHY)}"`;
  const sortMods = list => list.sort((a, b) => a.n.localeCompare(b.n, "en", { sensitivity: "base" }));
  // ---- 更新（程序没有，11-28 作者定要加） ----
  // m 和要跟着它一起更新的那几个，顺着 need 往两头找到头。值是 [跟着谁, 哪种关系]：pre 是那一个要的前置（新版本要配套的前置版本），dep 是依赖那一个的（要换成配套版本）；m 自己是 null
  function upGroup(m) {
    const g = new Map([[m, null]]), todo = [m];
    while (todo.length) {
      const a = todo.pop();
      dat().mods.forEach(o => {
        if (!o.up || g.has(o)) return;
        const rel = a.need && a.need[0] === o.id ? "pre" : o.need && o.need[0] === a.id ? "dep" : "";
        if (rel) { g.set(o, [a, rel]); todo.push(o); }
      });
    }
    return g;
  }
  const upList = () => dat().mods.filter(m => m.up);
  // 那一行右边的「更新」：悬停先说会带上谁、为什么（作者定的不另外确认，所以点之前就看得到）；正在更新时换成转圈
  function upBtn(m) {
    if (!m.up) return "";
    if (m.upping) return `<span class="ad-btn" aria-disabled="true"><i data-ic="spinner" class="ad-spin"></i>正在更新</span>`;
    const along = [...upGroup(m)].slice(1).map(([o, [, rel]]) => rel === "pre" ? `所需前置 ${o.n}` : `依赖它的 ${o.n}`);
    const tip = live() ? STOP_WHY : `更新至 ${m.up[1]}（旧文件移入 .minedeck\\已删除）` + (along.length ? `；${along.join("、")} 需版本配套，将一并更新` : "");
    return `<button type="button" class="ad-btn" data-ad-act="up"${live() ? HOLD_STOP : ""} title="${esc(tip)}">更新</button>`;
  }
  // 栏上的「全部更新 · N」：只在「已安装」那边、有可更新的才出；有哪一行正在更新时换成转圈，免得两头同时点
  function upAllBtn() {
    const list = upList(), n = list.length; if (!n) return "";
    if (list.some(m => m.upping)) return `<span class="ad-btn" aria-disabled="true"><i data-ic="spinner" class="ad-spin"></i>正在更新</span>`;
    return `<button type="button" class="ad-btn" data-ad-act="upall"${live() ? HOLD_STOP : ""} title="${esc(live() ? STOP_WHY : `将全部 ${n} 项更新至最新版本`)}"><i data-ic="arrowUp"></i>全部更新 · ${n}</button>`;
  }
  const refreshTip = () => `刷新已安装的${noun()}并检查更新`;
  // 更新完的标记：自己点的是「已更新」，被带上的写跟着谁、悬停写为什么带上它
  function updTag(m) {
    const { from, to, via } = m.upd, [a, rel] = via || [];
    const tip = !via ? `已由 ${from} 更新至 ${to}` : rel === "pre" ? `新版本的 ${a.n} 需要配套版本的前置 ${m.n}，已随其一并由 ${from} 更新至 ${to}` : `${m.n} 依赖 ${a.n}，需与其版本配套，已随其一并由 ${from} 更新至 ${to}`;
    return `<span class="ad-tag ok" title="${esc(tip)}">${via ? `随 ${esc(a.n)} 更新` : `已更新 ${esc(to)}`}</span>`;
  }
  function tagsOf(m) {
    const ids = installedIds(), t = [];
    if (m.need && !ids.has(m.need[0])) t.push(`<button type="button" class="ad-tag warn" data-ad-act="need" data-ad-need="${esc(m.need[1])}" title="${esc(`${m.n} 依赖前置 ${m.need[1]}，缺少时将无法加载；点击前往「添加」搜索`)}">缺前置 ${esc(m.need[1])}</button>`);
    if (m.err) t.push(`<span class="ad-tag warn" title="${esc("无法读取文件信息：" + m.err)}">无法读取信息</span>`);
    if (m.fresh) t.push(`<span class="ad-tag" title="服务器运行中安装，重启后生效">重启后生效</span>`);
    if (m.up) t.push(`<span class="ad-tag new" title="Modrinth 上有新版本">可更新 ${esc(m.up[0])} → ${esc(m.up[1])}</span>`);
    if (m.upd) t.push(updTag(m));
    return t.join("");
  }
  function instRow(m) {
    const dis = live(), tip = dis ? STOP_WHY : m.off ? "启用（移除 .disabled 后缀，下次启动生效）" : "停用（重命名为 .jar.disabled，服务器不再加载，可随时重新启用；下次启动生效）";
    const x = m.id && addonOf(m.id);
    return `<div class="ad-row${m.off ? " off" : ""}" data-ad-file="${esc(m.file)}">`
      + `<button type="button" class="ad-tg${m.off ? "" : " on"}" data-ad-act="tg" role="switch" aria-checked="${!m.off}" aria-label="${esc(m.n)}"${dis ? HOLD_STOP : ""} title="${esc(tip)}"><span class="ad-sw"></span></button>`
      + tile(m.n, m.id, x && x.icon) + `<div class="ad-m"><div class="ad-n"><span class="nm">${esc(m.n)}</span><span class="ad-tags" data-ad-k="tags">${tagsOf(m)}</span></div>`
      + `<div class="ad-f" data-ad-k="file">${esc(m.file)}</div></div><span class="ad-ups" data-ad-k="up">${upBtn(m)}</span>`
      + `<button type="button" class="ad-ib ad-x" data-ad-act="del"${dis ? HOLD_STOP : ""} title="${esc(dis ? STOP_WHY : "删除（移入服务器文件夹中的 .minedeck\\已删除，需要时可移回原文件夹）")}"><i data-ic="trash"></i></button></div>`;
  }
  const pendRows = () => C.kind === "forge" && (+C.QS.get("notice") || 0) >= 3 ? PENDING.map(([f, why, full]) =>
    `<div class="ad-row pend"><span class="ad-pw" title="该整合包文件未安装"><i data-ic="warn"></i></span>${tile(f.replace(/[-_].*$/, ""), f)}`
    + `<div class="ad-m"><div class="ad-n"><span class="nm mono">${esc(f)}</span></div><div class="ad-f" title="${esc(full)}">${esc(why)}</div></div>`
    + `<button type="button" class="ad-btn" data-ad-act="pend" title="选择处理方式：跳过、使用本地文件替代，或确认站点可信后下载">处理…</button></div>`).join("") : "";
  const instMatch = m => { const q = dat().q.inst.trim().toLowerCase(); return !q || (m.n + " " + m.file).toLowerCase().includes(q); };
  function instList() {
    const D = dat(), rows = D.mods.filter(instMatch);
    if (!D.mods.length) return `<div class="ad-empty">${dirOf(C.S.core)} 文件夹中暂无${noun()}</div>`;
    return (D.q.inst.trim() ? "" : pendRows()) + (rows.length ? rows.map(instRow).join("") : `<div class="ad-empty">未找到包含「${esc(D.q.inst.trim())}」的${noun()}</div>`);
  }
  const instBody = () => `<div class="ad-lw ad-ilw"><div class="list">${instList()}</div><span class="thumb"></span></div>`;

  // ================= 左栏：添加（同向导第二步） =================
  const LIB_ONLY = x => x.cats.length === 1 && x.cats[0] === "library";
  function gridItems() {
    const D = dat(), S = sample(); if (!S) return [];
    const q = D.q.add.trim().toLowerCase();
    return S.items.filter(x => (D.cat ? x.cats.includes(D.cat) : !LIB_ONLY(x)) && (!q || (x.n + x.d).toLowerCase().includes(q))).sort((a, b) => b.dl - a.dl);
  }
  const ftag = x => noun() === "插件" ? (x.cs === "required" ? "需客户端" : "") : (x.cs !== "required" ? "仅服务端" : "");
  function extLink(x) {
    if (!/^(mod|plugin)$/.test(x.type) || !/^[A-Za-z0-9._-]+$/.test(x.id)) return "";
    return `<a class="ad-ext" href="https://modrinth.com/${x.type}/${x.id}" target="_blank" rel="noopener noreferrer" title="在 Modrinth 查看介绍" aria-label="在 Modrinth 查看 ${esc(x.n)} 的介绍"><i data-ic="share"></i></a>`;
  }
  function cardHTML(x, has, inst) {
    const on = has.has(x.id), done = inst.has(x.id);
    return `<div class="ad-mod${on ? " in" : ""}${done ? " inst" : ""}" data-ad-addon="${x.id}"${done ? ` title="已安装在此服务器"` : ""}>${tile(x.n, x.id, x.icon, "lg")}`
      + `<div class="ad-mm"><div class="n"><span class="nm">${esc(x.n)}</span>${extLink(x)}</div><div class="d" title="${esc(x.d)}">${esc(x.d)}</div>`
      + `<div class="meta"><i data-ic="download"></i>${fmtDl(x.dl)}${done ? " · 已安装" : ftag(x) ? ` · ${ftag(x)}` : ""}</div></div>`
      + `<span class="ad-add"><i data-ic="${on || done ? "check" : "plus"}"></i></span></div>`;
  }
  function gridEnd(total) {
    const D = dat();
    return `<div class="ad-gend">${!D.cat && !D.q.add.trim() && sample().total > CAP ? `仅展示热门前 ${CAP} 项，更多内容可通过搜索或分类查找` : `已显示全部 ${total} 项`}</div>`;
  }
  const skel = n => `<div class="ad-mod skel"><span class="ad-mt lg"></span><div class="ad-mm"><i></i><i></i><i></i></div></div>`.repeat(n);
  function gridHTML() {
    const D = dat();
    if (!sample()) return skel(6);
    const items = gridItems(), has = new Set(D.picked.map(p => p.id)), inst = installedIds();
    if (!items.length) return `<div class="ad-empty grid">未找到匹配的${noun()}，请更换关键词或分类</div>`;
    D.shown = Math.min(PAGE, items.length); D.more = false;
    return items.slice(0, D.shown).map(x => cardHTML(x, has, inst)).join("") + (D.shown >= Math.min(items.length, CAP) ? gridEnd(Math.min(items.length, CAP)) : "");
  }
  const chipsHTML = () => { const D = dat(); return `<div class="ad-chips"><i class="ad-chl"></i>${[["", "全部"], ...ACATS[noun()]].map(([k, t]) => `<span class="ad-chip${k === D.cat ? " on" : ""}" data-ad-cat="${k}">${t}</span>`).join("")}</div>`; };
  const addBody = () => chipsHTML() + `<div class="ad-lw ad-glw"><div class="list ad-grid">${gridHTML()}</div><span class="thumb"></span></div>`;
  // 往后翻：滚到离底 160 就接着取下一批，先出骨架卡（同向导第二步）
  function moreCards() {
    const r = root(), g = r && $(".ad-grid", r), D = dat(); if (!g || D.more || !sample()) return;
    const items = gridItems(), total = Math.min(items.length, CAP);
    if (D.shown >= total || g.scrollTop + g.clientHeight < g.scrollHeight - 160) return;
    D.more = true;
    const key = C.kind + "|" + D.cat + "|" + D.q.add;
    g.insertAdjacentHTML("beforeend", skel(4)); g.closest(".ad-lw")._paint?.();
    setTimeout(() => {
      const g2 = root() && $(".ad-grid", root());
      if (C.kind + "|" + D.cat + "|" + D.q.add !== key || g2 !== g) { D.more = false; return; }
      g.querySelectorAll(".skel").forEach(x => x.remove());
      const has = new Set(D.picked.map(p => p.id)), inst = installedIds(), next = items.slice(D.shown, Math.min(D.shown + PAGE, total));
      g.insertAdjacentHTML("beforeend", next.map(x => cardHTML(x, has, inst)).join(""));
      const added = [...g.querySelectorAll(".ad-mod")].slice(D.shown); hy(g); fitTitles(g);
      added.forEach((c, i) => c.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: Math.min(i, 9) * 22, easing: EASE_OUT, fill: "backwards" }));
      D.shown += next.length;
      if (D.shown >= total) g.insertAdjacentHTML("beforeend", gridEnd(total));
      D.more = false; g.closest(".ad-lw")._paint?.(); moreCards();
    }, 600);
  }

  // ================= 右栏：待装清单（同向导第二步） =================
  // 清单里的冲突（后端 Plan.Conflicts）：每条 [清单里的那一项, 另一方]，另一方在清单里、或者已经装在服务器上（停用着的不算）。清单里两项互相冲突时两个方向各一条（两行都要标）
  function clashes() {
    if (C.QS.get("dep") !== "clash") return []; // 编的那几对只在 ?dep=clash 下算，平时逛的时候不冒出假的冲突
    const D = dat(), has = id => D.picked.some(p => p.id === id), inst = installedIds();
    return CLASH.flatMap(([a, b]) => [[a, b], [b, a]].filter(([x, o]) => has(x) && (has(o) || inst.has(o))));
  }
  const isLost = d => C.QS.get("dep") === "lost" && LOST.includes(d); // 同上，编的，只在 ?dep=lost 下算
  // 这一行要不要换成警告的那句：[行里的短句, 悬停全文]。草稿那块自带（p.warn）；未找到的前置；有冲突的
  function crowWarn(p, x, by) {
    if (p.warn) return p.warn;
    if (p.lost) return ["未找到可用版本", `${by.n} 所需的该版本在 Modrinth 上已不存在，安装时将跳过此前置；可前往 Modrinth 查找其他版本，下载后拖入清单`];
    const c = !p.opt && clashes().find(([a]) => a === p.id), o = c && addonOf(c[1]), m = c && !dat().picked.some(q => q.id === c[1]) && dat().mods.find(y => y.id === c[1]);
    if (m) return [`与已安装的 ${m.n} 冲突`, `${x.n} 与服务器中已安装的 ${m.file} 不兼容，同时存在可能导致服务器无法启动`];
    return c ? [`与 ${o.n} 冲突`, `${x.n} 与 ${o.n} 不兼容，同时安装可能导致服务器无法启动；请移出其中一项`] : null;
  }
  // 清单里的一行。除了挑的（连同自动补的前置、本地文件），还有：小标题（p.sub）、未找到的前置（p.lost，拿不掉）、可选前置（p.opt，行尾是 ＋）、草稿那块里的（p.fixed，拿不掉；p.n 是样本里没有的自带名字）
  function crowHTML(p) {
    const key = esc(p.k || p.id);
    if (p.sub) return `<div class="ad-csub" data-ad-id="${key}">${esc(p.sub)}</div>`;
    const x = p.local ? { n: p.file, id: p.file } : addonOf(p.id) || (p.n && p), by = p.by && addonOf(p.by); if (!x) return "";
    const w = crowWarn(p, x, by);
    const why = w ? esc(w[0]) : p.local ? `本地文件 · ${esc(p.kb)}` : by ? `${esc(by.n)} 的${p.opt ? "可选" : ""}前置` : noun() === "插件" && x.cs === "required" ? "需客户端同步安装" : noun() === "模组" && x.cs !== "required" ? "仅服务端，客户端无需安装" : esc(x.d);
    const end = p.fixed || p.lost ? "" : p.opt ? `<span class="ad-cx" data-ad-optadd="${esc(p.id)}" title="加入清单"><i data-ic="plus"></i></span>` : `<span class="ad-cx" data-ad-unpick="${key}" title="移出清单"><i data-ic="x"></i></span>`;
    return `<div class="ad-crow${p.opt ? " opt" : ""}${p.lost ? " lost" : ""}" data-ad-id="${key}">${tile(x.n, x.id, x.icon)}<div class="ad-m"><div class="ad-n"><span class="nm">${esc(x.n)}</span>${by && !p.opt ? `<span class="ad-ftag">前置</span>` : ""}</div>`
      + `<div class="ad-why${w ? " warn" : ""}" data-ad-k="why"${w ? ` title="${esc(w[1])}"` : ""}>${why}</div></div>${end}</div>`;
  }
  // 清单里要画的行，照顺序：挑的（未找到的前置跟在要它的那一项后面），末尾「可选前置」小标题和各行。可选前置只列直接挑的那几项要的，已在清单、已装的不列（同后端）
  function cartRows() {
    const D = dat(), has = new Set(D.picked.map(p => p.id)), inst = installedIds(), rows = [], opt = new Map();
    D.picked.forEach(p => {
      rows.push(p);
      if (p.local) return;
      (addonOf(p.id)?.deps || []).forEach(d => { const k = "lost:" + d; if (isLost(d) && !has.has(d) && !rows.some(r => r.k === k)) rows.push({ id: d, by: p.id, lost: true, k }); });
      if (!p.by) (OPT[p.id] || []).forEach(d => { if (addonOf(d) && !has.has(d) && !inst.has(d) && !opt.has(d)) opt.set(d, p.id); });
    });
    if (opt.size) rows.push({ k: "opt:", sub: "可选前置" }, ...[...opt].map(([id, by]) => ({ id, by, opt: true, k: "opt:" + id })));
    return rows;
  }
  // 新建时选择的尚未安装（A31）：待装清单最上面一块。程序那句解释（「建这台服务器时切到了别的页面，当时草稿清单里的内容还留着」）收进标题的悬停
  function draftHTML() {
    const d = dat().draft; if (!d) return "";
    return `<div class="ad-draft"><div class="ad-dh" title="创建当前服务器时未完成安装，所选内容仍保留在草稿清单中"><i data-ic="package"></i><span>新建时选择的 ${d.length} 项尚未安装</span></div>${d.map(crowHTML).join("")}`
      + `<div class="ad-da"><button type="button" class="ad-btn pri" data-ad-act="draftins" title="连同待装清单一并安装${live() ? "；服务器运行中也可安装，重启后生效" : ""}">一并安装</button>`
      + `<button type="button" class="ad-btn" data-ad-act="draftdrop" title="仅清除以上 ${d.length} 项，待装清单不受影响">清除</button></div></div>`;
  }
  // 空着时只留一句淡字（向导里那句「点击左侧 ＋ 添加，前置依赖将自动补全。」作者 11-28 定详情页不要）
  const cartEmpty = () => `<div class="ad-cempty">暂未选择任何${noun()}</div>`;
  const cartListHTML = () => cartEmpty() + draftHTML() + cartRows().map(crowHTML).join("");
  // 加进清单：把它要的必装前置一起放进来（已在清单、已装的、未找到的不放）
  function pick(id) {
    const D = dat(), inst = installedIds();
    D.picked.push({ id, by: null });
    for (const d of addonOf(id)?.deps || []) if (!D.picked.some(p => p.id === d) && !inst.has(d) && !isLost(d)) D.picked.push({ id: d, by: id });
  }
  // ?dep= 预先放进清单的那几项：要等样本到了才知道各自的前置，所以不在 dat() 里放，画之前、样本到了时各试一次
  function seedCart() {
    const D = dat(), ids = DEP_DEMO[C.QS.get("dep")]?.[noun()];
    if (D.seeded || !ids || !sample()) return;
    D.seeded = true; ids.forEach(pick);
  }
  const insBtn = () => { const D = dat(), n = D.picked.length;
    return D.installing ? `<button type="button" class="ad-ins" aria-disabled="true"><i data-ic="spinner" class="ad-spin"></i>正在安装</button>`
      : `<button type="button" class="ad-ins" data-ad-act="install"${n ? (live() ? ` title="服务器运行中也可安装，重启后生效"` : "") : ` aria-disabled="true" data-hold="请先添加待装项" title="请先添加待装项"`}><i data-ic="download"></i>全部安装${n ? ` · ${n}` : ""}</button>`; };
  // 拖入框上面那几句：会改变结果的才写（要客户端也装、有冲突、有前置未找到），一句一行。冲突、未找到只是提醒，不拦「全部安装」（程序也照装）
  function cartNote() {
    const D = dat(), f = D.picked.filter(p => !p.local && addonOf(p.id)?.cs === "required").length;
    const c = new Set(clashes().map(x => [...x].sort().join())).size, lost = cartRows().filter(p => p.lost).length;
    return [f && noun() === "插件" && ["info", `其中 ${f} 项需客户端同步安装`], c && ["warn", `存在 ${c} 处冲突，安装前请确认`], lost && ["warn", `${lost} 项前置未找到，安装时将跳过`]]
      .filter(Boolean).map(([ic, t]) => `<div class="ad-cnl ${ic}">${icon(ic)}${t}</div>`).join("");
  }
  function cartHTML() {
    const D = dat(), n = D.picked.length;
    return `<section class="ad-col ad-cart${n ? " has" : ""}">${(C.folders || []).length ? `<div class="ad-fd">${C.folders.map(([i, t]) => `<button type="button" title="${esc(t)}"><i data-ic="${i}"></i></button>`).join("")}</div>` : ""}`
      + `<div class="ad-chd"><span class="ad-t">待装清单</span><span class="ad-cnt">${n ? `· ${n}` : ""}</span><span class="ad-sp"></span><button type="button" class="ad-clr" data-ad-act="clear">清空</button></div>`
      + `<div class="ad-lw ad-clw"><div class="list">${cartListHTML()}</div><span class="thumb"></span></div>`
      + `<div class="ad-cnote">${cartNote()}</div>`
      + `<div class="ad-drop" data-ad-act="local" title="可将本地 .jar 文件加入清单，与 Modrinth 项目一并安装"><i data-ic="folder"></i>拖入或选择 .jar 文件</div>`
      + `<span class="ad-insslot">${insBtn()}</span></section>`;
  }

  // ================= 整页 =================
  function headHTML() {
    const D = dat(), inst = D.view === "inst";
    const ctx = inst ? "" : `<span class="ad-ctx" title="草图用的是向导第二步那份 Modrinth 样本（${noun() === "插件" ? "Paper 26.2 的插件" : "NeoForge 26.1 的模组"}），和这台的核心、版本对不上；程序按这台服务器现取">适配 ${esc(C.S.label)} ${esc(C.S.mc)} · Modrinth</span>`;
    return `<div class="ad-hd"><div class="ad-seg"><i class="ad-sgl"></i><span class="${inst ? "on" : ""}" data-ad-view="inst">已安装<span class="ad-sn" data-ad-k="sn">${D.mods.length}</span></span>`
      + `<span class="${inst ? "" : "on"}" data-ad-view="add">添加</span></div><span class="ad-ctxslot" data-ad-k="ctx">${ctx}</span><span class="ad-sp"></span>`
      + `<span class="ad-ups" data-ad-k="upall">${inst ? upAllBtn() : ""}</span>`
      + `<button type="button" class="ad-ib r4" data-ad-act="refresh" title="${inst ? refreshTip() : "刷新"}"><i data-ic="refresh"></i></button>`
      + `<label class="ad-find"><i data-ic="search"></i><input class="ad-q" value="${esc(inst ? D.q.inst : D.q.add)}" placeholder="${inst ? `搜索已安装的${noun()}` : `搜索${noun()}（建议英文名）`}" autocomplete="off" spellcheck="false"></label></div>`;
  }
  function html(ctx) {
    C = ctx; closeAsk(true); const D = dat(); seedCart();
    return `<div class="ad"><section class="ad-col ad-main">${headHTML()}<div class="ad-body">${D.view === "inst" ? instBody() : addBody()}</div></section>${cartHTML()}</div>`;
  }
  // 名字、文件名放不下被截断时，全文进悬停（同「玩家」的 fitTitle）
  const fitTitles = r => $$(".ad-f:not([title]):not([data-tip]), .ad-n .nm, .ad-mm .n .nm", r).forEach(el => {
    if (el.scrollWidth > el.clientWidth + 1) { el.title = el.textContent; delete el.dataset.tip; }
    else if (el.classList.contains("nm")) { el.removeAttribute("title"); delete el.dataset.tip; }
  });
  function after(r) {
    $$(".ad-lw", r).forEach(w => C.thinBar(w));
    fitTitles(r);
    document.fonts?.ready.then(() => r.isConnected && fitTitles(r));
    placeSeg(false); placeChip(false); chipFade();
    $(".ad-glw .list", r)?.addEventListener("scroll", moreCards, { passive: true });
    const ch = $(".ad-chips", r);
    if (ch) { ch.addEventListener("scroll", chipFade, { passive: true }); ch.addEventListener("wheel", e => { if (ch.scrollWidth > ch.clientWidth && Math.abs(e.deltaY) > Math.abs(e.deltaX)) { e.preventDefault(); ch.scrollLeft += e.deltaY; } }, { passive: false }); }
  }
  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    after(r);
    r.addEventListener("click", onClick);
    r.addEventListener("input", onInput);
    r.addEventListener("dragover", e => { const d = e.target.closest?.(".ad-drop"); if (d) { e.preventDefault(); d.classList.add("over"); } });
    r.addEventListener("dragleave", e => { const d = e.target.closest?.(".ad-drop"); if (d && !d.contains(e.relatedTarget)) d.classList.remove("over"); });
    r.addEventListener("drop", e => { const d = e.target.closest?.(".ad-drop"); if (!d) return; e.preventDefault(); d.classList.remove("over"); addLocal(e.dataTransfer?.files?.[0]); });
  }
  // 分段、类别片的选中框：单独一块，滑过去
  function slide(box, hlSel, onSel, animate) {
    if (!box) return; const hl = $(hlSel, box), on = $(onSel, box); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  const placeSeg = a => slide(root() && $(".ad-seg", root()), ".ad-sgl", "span.on", a);
  const placeChip = a => slide(root() && $(".ad-chips", root()), ".ad-chl", ".ad-chip.on", a);
  function chipFade() { const c = root() && $(".ad-chips", root()); if (!c) return; c.classList.toggle("can-l", c.scrollLeft > 2); c.classList.toggle("can-r", c.scrollLeft + c.clientWidth < c.scrollWidth - 2); }
  function sampleArrived() {
    const r = root(); if (!r) return;
    seedCart();
    // 清单、草稿那块里的行要样本到了才有名字和图标：有东西时重画一遍（两边都要，?dep=、?draft= 是在「已安装」那边打开的）
    if (dat().picked.length || dat().draft) { const l = $(".ad-clw .list", r); l.innerHTML = cartListHTML(); hy(l); fitTitles(l); cartMeta(); l.closest(".ad-lw")._paint?.(); }
    if (dat().view === "inst") { $$(".ad-ilw .ad-row[data-ad-file]", r).forEach(row => { const m = dat().mods.find(x => x.file === row.dataset.adFile), x = m && m.id && addonOf(m.id);
      if (x && x.icon) { const t = document.createElement("span"); t.innerHTML = tile(m.n, m.id, x.icon); row.querySelector(".ad-mt").replaceWith(t.firstElementChild); } }); return; }
    const g = $(".ad-grid", r); g.innerHTML = gridHTML(); hy(g); fitTitles(g); g.closest(".ad-lw")._paint?.();
  }

  // ---- 换着看：已安装 ↔ 添加。分段的选中块滑过去，下面这一块往点的方向逐项换（规范第七节），搜索框换成这一边的字 ----
  function setView(v) {
    const D = dat(), r = root(); if (D.view === v || !r) return;
    const dir = v === "add" ? 1 : -1; D.view = v;
    $$(".ad-seg span[data-ad-view]", r).forEach(s => s.classList.toggle("on", s.dataset.adView === v)); placeSeg(true);
    const tmp = document.createElement("div"); tmp.innerHTML = headHTML();
    swapKeyed($(".ad-hd", r), tmp.innerHTML, dir, "X");
    const q = $(".ad-q", r); q.value = v === "inst" ? D.q.inst : D.q.add; q.placeholder = v === "inst" ? `搜索已安装的${noun()}` : `搜索${noun()}（建议英文名）`;
    const rf = $("[data-ad-act=refresh]", r); rf.title = v === "inst" ? refreshTip() : "刷新"; delete rf.dataset.tip;
    const body = $(".ad-body", r), tok = body._tok = (body._tok || 0) + 1;
    body.getAnimations({ subtree: true }).forEach(a => a.cancel());
    body.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translateX(${-5 * dir}px)` }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
      if (body._tok !== tok) return;
      body.getAnimations().forEach(a => a.cancel());
      body.innerHTML = v === "inst" ? instBody() : addBody(); hy(body); after(r);
      const list = $(".list", body), items = [...body.querySelectorAll(".ad-chips, .ad-row, .ad-mod, .ad-empty, .ad-gend")];
      inView(list, items.filter(x => x.closest(".list"))).concat(items.filter(x => !x.closest(".list"))).forEach((c, i) =>
        c.animate([{ opacity: 0, transform: `translateX(${5 * dir}px)` }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
    };
  }
  // 一批换（筛已安装、换类别、搜索、刷新）：旧的一起往反方向挪 5 像素淡掉，新的从点的那一侧一张一张淡进来，只给看得见的做（规范第七节）
  function regrid(dir = 0) {
    const r = root(); if (!r) return;
    const D = dat(), l = D.view === "inst" ? $(".ad-ilw .list", r) : $(".ad-grid", r); if (!l) return;
    if (D.view === "add") { $$(".ad-chip", r).forEach(c => c.classList.toggle("on", c.dataset.adCat === D.cat)); placeChip(true); }
    l.getAnimations({ subtree: true }).forEach(a => a.cancel());
    const tok = l._tok = (l._tok || 0) + 1;
    const a = l.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: dir ? `translateX(${-5 * dir}px)` : "none" }], { duration: 90, easing: EASE_IN, fill: "forwards" });
    a.onfinish = () => {
      if (l._tok !== tok) return;
      l.scrollTop = 0; l.innerHTML = D.view === "inst" ? instList() : gridHTML(); hy(l); fitTitles(l); a.cancel();
      const from = dir ? `translateX(${5 * dir}px)` : "translateY(5px)";
      inView(l, [...l.children]).forEach((c, i) => c.animate([{ opacity: 0, transform: from }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
      l.closest(".ad-lw")._paint?.();
    };
  }

  // ---- 待装清单的加减（同向导 syncAddons：只动变了的那几行） ----
  function toggleAddon(id) {
    const D = dat(), at = D.picked.findIndex(p => p.id === id);
    if (at >= 0) {
      D.picked.splice(at, 1);
      D.picked = D.picked.filter(p => p.by !== id || D.picked.some(q => q.by === null && (addonOf(q.id)?.deps || []).includes(p.id)));
    } else pick(id);
    syncCart();
  }
  function addLocal(file) {
    const D = dat(), [nm, kb] = file ? [file.name, file.size ? (file.size >= 1048576 ? (file.size / 1048576).toFixed(1) + " MB" : Math.max(1, Math.round(file.size / 1024)) + " KB") : "—"] : (LOCAL[C.kind] || LOCAL.paper);
    const id = "local:" + nm;
    if (D.picked.some(p => p.id === id)) { const row = root() && $(`.ad-crow[data-ad-id="${CSS.escape(id)}"]`, root()); row?.classList.remove("ad-flash"); void row?.offsetWidth; row?.classList.add("ad-flash"); return; }
    D.picked.push({ id, by: null, local: true, file: nm, kb }); syncCart();
  }
  function syncCart(clear = false) {
    const D = dat(), r = root(); if (!r) return;
    const has = new Set(D.picked.map(p => p.id));
    $$(".ad-grid .ad-mod:not(.skel)", r).forEach(m => {
      if (m.classList.contains("inst")) return;
      const on = has.has(m.dataset.adAddon);
      if (m.classList.contains("in") !== on) { m.classList.toggle("in", on); $(".ad-add", m).innerHTML = icon(on ? "check" : "plus"); }
    });
    // 清单这一列里的行（挑的、未找到的前置、可选前置和它的小标题）都带编号，照 cartRows() 对：多出来的划掉，少的补上，还在的只换变了的那句。草稿那块里的行不归这里管（:scope >）
    const list = $(".ad-clw .list", r), want = cartRows(), keys = new Set(want.map(p => p.k || p.id));
    const rows = () => [...list.querySelectorAll(":scope > [data-ad-id]:not(.ad-out)")];
    const gone = rows().filter(x => !keys.has(x.dataset.adId));
    if (clear && gone.length) { // 清空、装完：从上往下一行接一行往右划出，间隔从 90 毫秒起每次缩成四分之三（同向导「清空」）
      let delay = 0, gap = 90;
      gone.forEach((x, i) => {
        x.classList.add("ad-out");
        const an = x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateX(40px)" }], { duration: 220, delay, easing: EASE_IN, fill: "forwards" });
        delay += gap; gap = Math.max(16, Math.round(gap * .75));
        if (i === gone.length - 1) an.onfinish = () => { gone.forEach(y => y.remove()); list.closest(".ad-lw")._paint?.(); };
      });
    } else gone.forEach((x, i) => dropOut(x, () => list.closest(".ad-lw")._paint?.(), i * 40));
    let prev = null;
    want.forEach(p => {
      const old = rows().find(x => x.dataset.adId === (p.k || p.id));
      if (old) { swapKeyed(old, crowHTML(p)); prev = old; return; } // 还在的：冲突的另一方加进来、拿走时，下面那句跟着换
      const tmp = document.createElement("div"); tmp.innerHTML = crowHTML(p); const x = tmp.firstElementChild; if (!x) return;
      list.insertBefore(hy(x), prev ? prev.nextSibling : list.querySelector(":scope > [data-ad-id]")); fitTitles(x); prev = x;
      x.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: p.by ? 60 : 0, easing: EASE_OUT, fill: "backwards" });
      if (!p.opt && !p.sub) x.scrollIntoView({ block: "nearest", behavior: "smooth" });
    });
    cartMeta(); list.closest(".ad-lw")._paint?.();
  }
  function cartMeta() {
    const D = dat(), r = root(); if (!r) return;
    const n = D.picked.length, cart = $(".ad-cart", r);
    cart.classList.toggle("has", n > 0);
    $(".ad-cnt", r).textContent = n ? `· ${n}` : "";
    const note = $(".ad-cnote", r), nh = cartNote(); if (note.innerHTML !== nh) note.innerHTML = nh;
    const slot = $(".ad-insslot", r); slot.innerHTML = insBtn(); hy(slot);
  }
  // 点「全部安装」、草稿那块的「一并安装」：跳后台任务进度页 task.html?kind=cart 从头放一遍（地址里只带配色参数）；清单空着（按钮灰着）、也没有草稿时不跳
  function toCartTask() {
    if (!dat().picked.length && !dat().draft) return;
    const q = new URLSearchParams({ kind: "cart", play: "1" });
    ["theme", "scheme", "accent"].forEach(k => C.QS.get(k) && q.set(k, C.QS.get(k)));
    location.href = "task.html?" + q;
  }
  // 全部安装：草图里假装装 1.4 秒；装完清单一行行划掉，左边已安装里多出这几行（在「已安装」那边时从下面长出来），「添加」里的卡片换成「已安装」
  // （「全部安装」改成跳进度页以后没人调它了。装好以后的这个样子没有地址参数能看到，先留着没删；定了不要就整段删掉）
  function install() {
    const D = dat(); if (D.installing || !D.picked.length) return;
    D.installing = true; cartMeta();
    setTimeout(() => {
      const items = D.picked.map(p => {
        if (p.local) return I(p.file.replace(/\.jar$/i, "").replace(/[-_]v?\d.*$/, ""), p.file, "", { fresh: live() });
        const x = addonOf(p.id), ver = `${1 + (x.dl % 5)}.${x.dl % 13}.${x.dl % 7}`;
        return I(x.n, `${x.n.replace(/[^A-Za-z0-9]+/g, "")}-${ver}.jar`, x.id, { fresh: live() });
      });
      D.mods.push(...items); sortMods(D.mods);
      D.picked = []; D.installing = false;
      const r = root(); if (!r) return;
      syncCart(true);
      paintCount();
      if (D.view === "inst") {
        const l = $(".ad-ilw .list", r);
        items.filter(instMatch).forEach(m => {
          const next = [...l.querySelectorAll(".ad-row:not(.pend)")].find(x => { const o = D.mods.find(y => y.file === x.dataset.adFile); return o && o.n.localeCompare(m.n, "en", { sensitivity: "base" }) > 0; });
          const tmp = document.createElement("div"); tmp.innerHTML = instRow(m); const row = hy(tmp.firstElementChild);
          l.insertBefore(row, next || null); fitTitles(row); growIn(row, 200);
        });
        // 装上的正好是别人缺的前置：那一行的「缺前置」标记跟着拿掉
        D.mods.forEach(m => { if (m.need) { const row = $(`.ad-row[data-ad-file="${CSS.escape(m.file)}"]`, l); if (row) swapKeyed(row, instRow(m)); } });
        l.closest(".ad-lw")._paint?.();
      } else {
        const inst = installedIds();
        $$(".ad-grid .ad-mod:not(.skel)", r).forEach(m => {
          if (!inst.has(m.dataset.adAddon) || m.classList.contains("inst")) return;
          const x = addonOf(m.dataset.adAddon), tmp = document.createElement("div"); tmp.innerHTML = cardHTML(x, new Set(), inst);
          const rep = hy(tmp.firstElementChild); m.replaceWith(rep); fitTitles(rep);
        });
      }
    }, 1400);
  }

  // 分段里「已安装」后面的个数：只换这个数，换完分段的选中块按新的宽度滑一下
  function paintCount() {
    const r = root(); if (!r) return;
    swapKeyed($(".ad-seg", r), `<span class="ad-sn" data-ad-k="sn">${dat().mods.length}</span>`, 1);
    setTimeout(() => placeSeg(true), 100);
  }
  // ---- 已安装：更新。lead 是点的那一行（连同关联的一起），不给就是「全部更新」。草图里假装下 1.2 秒，然后一行接一行换成新版本 ----
  const paintRow = (m, file = m.file) => { const row = root() && $(`.ad-ilw .ad-row[data-ad-file="${CSS.escape(file)}"]`, root()); if (row) { row.dataset.adFile = m.file; swapKeyed(row, instRow(m)); } return row; };
  const paintUpAll = () => { const r = root(); if (r) swapKeyed($(".ad-hd", r), headHTML()); };
  function runUpdate(lead) {
    const g = [...(lead ? upGroup(lead) : upList().map(m => [m, null]))].filter(([m]) => !m.upping);
    g.forEach(([m]) => { m.upping = true; paintRow(m); });
    paintUpAll();
    g.forEach(([m, via], i) => setTimeout(() => {
      const [from, to] = m.up, old = m.file;
      m.file = old.replace(from, to); delete m.up; delete m.upping; m.upd = { from, to, via };
      const row = paintRow(m, old);
      if (row) { row.classList.remove("ad-flash"); void row.offsetWidth; row.classList.add("ad-flash"); }
      if (i === g.length - 1) paintUpAll();
    }, 1200 + i * 220));
  }
  // ---- 已安装：启用 / 停用、删除 ----
  function toggleMod(row) {
    const D = dat(), m = D.mods.find(x => x.file === row.dataset.adFile); if (!m) return;
    m.off = !m.off; m.file = m.off ? m.file + ".disabled" : m.file.replace(/\.disabled$/i, "");
    row.dataset.adFile = m.file; row.classList.toggle("off", m.off);
    const tg = $(".ad-tg", row); tg.classList.toggle("on", !m.off); tg.setAttribute("aria-checked", !m.off);
    tg.title = m.off ? "启用（移除 .disabled 后缀，下次启动生效）" : "停用（重命名为 .jar.disabled，服务器不再加载，可随时重新启用；下次启动生效）"; delete tg.dataset.tip;
    swapKeyed(row, `<div class="ad-f" data-ad-k="file">${esc(m.file)}</div>`, 1);
    // 停用、启用了别人要的前置：缺前置的标记跟着变
    D.mods.forEach(o => { if (o.need && o.need[0] === m.id) { const rr = $(`.ad-row[data-ad-file="${CSS.escape(o.file)}"]`, root()); if (rr) swapKeyed(rr, instRow(o)); } });
  }
  let mask = null;
  function askDelete(row) {
    const D = dat(), m = D.mods.find(x => x.file === row.dataset.adFile); if (!m) return;
    const dir = dirOf(C.S.core);
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "ad-mask enter" });
    mk.innerHTML = `<div class="ad-md" role="dialog" aria-modal="true"><div class="ad-mc"><div class="ad-mh"><span class="ad-mi"><i data-ic="trash"></i></span><h3>删除<span class="nw">「${esc(m.n)}」？</span></h3></div>`
      + `<div class="ad-mb"><span class="mono">${esc(m.file)}</span> 将移入服务器文件夹中的 <span class="mono">.minedeck\\已删除\\${dir}</span>，不会永久删除；需要恢复时，将其移回 ${dir} 文件夹即可。</div></div>`
      + `<div class="ad-ma"><button type="button" class="ad-b fk" data-ad-x="no">取消</button><button type="button" class="ad-b dgr" data-ad-x="ok">删除${noun()}</button></div></div>`;
    document.body.appendChild(hy(mk));
    const no = $("[data-ad-x=no]", mk); no.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return; // 已经关了（正在淡出时又点了一下）
      if (e.target.closest("[data-ad-x=ok]")) {
        const along = m.up ? [...upGroup(m).keys()].slice(1) : []; // 删的这个有新版本时，原来会和它一起更新的那几行
        closeAsk(); D.mods = D.mods.filter(x => x !== m);
        const rr = root(), el = rr && $(`.ad-row[data-ad-file="${CSS.escape(m.file)}"]`, rr);
        if (el) dropOut(el, () => el.closest?.(".ad-lw")?._paint?.());
        if (rr) { paintCount();
          if (m.up) { paintUpAll(); along.forEach(o => paintRow(o)); } // 「全部更新」的个数跟着少，那几行「更新」的悬停里不再提它
          D.mods.forEach(o => { if (o.need && o.need[0] === m.id) { const x = $(`.ad-row[data-ad-file="${CSS.escape(o.file)}"]`, rr); if (x) swapKeyed(x, instRow(o)); } }); }
        return;
      }
      if (e.target.closest("[data-ad-x=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) closeAsk(); });

  // ---- 事件（绑在这一页的根上，换标签时跟着内容一起扔掉） ----
  function onClick(e) {
    const D = dat(), r = root();
    const v = e.target.closest("[data-ad-view]"); if (v) { setView(v.dataset.adView); return; }
    const cat = e.target.closest("[data-ad-cat]");
    if (cat) {
      const k = cat.dataset.adCat; if (k === D.cat) return;
      const chips = $$(".ad-chip", r), at = x => chips.findIndex(c => c.dataset.adCat === x), dir = at(k) > at(D.cat) ? 1 : -1;
      D.cat = k; regrid(dir); return;
    }
    if (e.target.closest(".ad-ext")) return; // 去 Modrinth 看介绍：新开页面，不把卡片加进清单
    const card = e.target.closest(".ad-mod:not(.skel)"); if (card) { if (!card.classList.contains("inst")) toggleAddon(card.dataset.adAddon); return; }
    const un = e.target.closest("[data-ad-unpick]"); if (un) { const id = un.dataset.adUnpick, p = D.picked.find(x => x.id === id); if (p?.local) { D.picked = D.picked.filter(x => x !== p); syncCart(); } else toggleAddon(id); return; }
    const oa = e.target.closest("[data-ad-optadd]"); if (oa) { pick(oa.dataset.adOptadd); syncCart(); return; } // 可选前置行尾的 ＋：正式加进清单，这一行从「可选前置」下面拿走
    const a = e.target.closest("[data-ad-act]"); if (!a || a.getAttribute("aria-disabled") === "true") return;
    const act = a.dataset.adAct, row = a.closest(".ad-row");
    if (act === "tg" && row) toggleMod(row);
    else if (act === "del" && row) askDelete(row);
    else if (act === "up" && row) { const m = D.mods.find(x => x.file === row.dataset.adFile); if (m) runUpdate(m); }
    else if (act === "upall") runUpdate();
    else if (act === "draftins") toCartTask();
    else if (act === "draftdrop") { D.draft = null; const b = a.closest(".ad-draft"); dropOut(b, () => $(".ad-clw", r)?._paint?.()); } // 程序的「不要了」：只清草稿那几项，没有确认框
    else if (act === "need") { // 缺前置：去「添加」里搜好它
      D.q.add = a.dataset.adNeed; D.cat = "";
      if (D.view !== "add") setView("add"); else { $(".ad-q", r).value = D.q.add; regrid(0); }
      setTimeout(() => { const q = root() && $(".ad-q", root()); if (q) q.value = D.q.add; }, 100);
    }
    else if (act === "refresh") { if (a.classList.contains("spinning")) return; a.classList.add("spinning"); setTimeout(() => { a.classList.remove("spinning"); regrid(0); }, 600); }
    else if (act === "clear") { if (!D.picked.length) return; D.picked = []; syncCart(true); }
    else if (act === "local") addLocal(null); // 草图里不真弹选文件的窗口：当作选了一个示例 jar
    else if (act === "install") toCartTask();
    else if (act === "pend") a.animate([{ opacity: 1 }, { opacity: .55 }, { opacity: 1 }], { duration: 300 }); // 程序打开「没装上的文件」弹窗（共用件页画过），草图里没接
  }
  let qT = 0;
  function onInput(e) {
    if (!e.target.classList?.contains("ad-q")) return;
    const D = dat(), v = e.target.value; D.q[D.view] = v;
    clearTimeout(qT); qT = setTimeout(() => regrid(0), 180); // 停手 0.18 秒再换（规范第七节）
  }
  addEventListener("resize", () => { placeSeg(false); placeChip(false); chipFade(); const r = root(); if (r) fitTitles(r); });

  // 草图切换条（外壳左下角第二条）：11-28 补画的几种状态
  const sk = [
    { t: "新建时选择的", k: "draft", opts: [[null, "无"], ["1", "正常", "三项都能装"], ["2", "有问题", "核心不符、无法安装、本地文件与服务器不符"]] },
    { t: "前置检查", k: "dep", opts: [[null, "无"], ["opt", "可选前置"], ["clash", "冲突"], ["lost", "未找到"]] },
  ];
  window.DETAIL_TABS.addons = { html, mount, sk };
})();
