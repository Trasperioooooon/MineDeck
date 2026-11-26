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
  const I = (n, file, id = "", x = {}) => ({ n, file, id, ...x });
  const SEED = {
    paper: [
      I("BlueMap", "bluemap-5.5-paper.jar", "bluemap"), I("ChunkyBorder", "ChunkyBorder-1.2.23.jar", "chunkyborder", { need: ["chunky", "Chunky"] }),
      I("CoreProtect", "CoreProtect-CE-23.1.jar", "coreprotect"), I("DiscordSRV", "DiscordSRV-Build-1.29.0.jar.disabled", "discordsrv", { off: true }),
      I("EssentialsX", "EssentialsX-2.21.0.jar"), I("EssentialsXChat", "EssentialsXChat-2.21.0.jar"), I("Floodgate", "floodgate-spigot.jar"),
      I("Geyser", "Geyser-Spigot.jar", "geyser"), I("LuckPerms", "LuckPerms-Bukkit-5.4.145.jar", "luckperms"), I("Multiverse-Core", "multiverse-core-4.3.14.jar", "multiverse-core"),
      I("MyCommand", "MyCommand-1.0.jar", "", { err: "压缩包损坏，读不出 plugin.yml" }), I("PlaceholderAPI", "PlaceholderAPI-2.11.6.jar", "placeholderapi"),
      I("Simple Voice Chat", "voicechat-bukkit-2.5.26.jar", "simple-voice-chat"), I("SkinsRestorer", "SkinsRestorer.jar", "skinsrestorer"), I("spark", "spark-1.10.119-bukkit.jar", "spark"),
      I("TAB", "TAB v5.0.3.jar", "tab-was-taken"), I("Vault", "Vault.jar"), I("ViaBackwards", "ViaBackwards-5.2.1.jar", "viabackwards"), I("ViaVersion", "ViaVersion-5.2.1.jar", "viaversion"),
      I("WorldEdit", "worldedit-bukkit-7.3.10.jar", "worldedit"), I("WorldGuard", "worldguard-bukkit-7.0.13-dist.jar", "worldguard"),
    ],
    forge: [
      I("AppleSkin", "appleskin-forge-mc1.20.1-2.5.1.jar", "appleskin"), I("Create", "create-1.20.1-0.5.1.j.jar"), I("Create Crafts & Additions", "createaddition-1.20.1-1.2.4e.jar"),
      I("Create Deco", "createdeco-2.0.2-1.20.1-forge.jar"), I("Create Slice & Dice", "sliceanddice-forge-3.3.0.jar"), I("Create: Big Cannons", "createbigcannons-5.5.0+mc.1.20.1-forge.jar"),
      I("Create: Enchantment Industry", "create_enchantment_industry-1.20.1-for-create-0.5.1.f-1.2.9.d.jar"), I("Create: Steam 'n' Rails", "Steam_Rails-1.6.7+forge-mc1.20.1.jar"),
      I("Curios API", "curios-forge-5.11.0+1.20.1.jar", "curios"), I("Farmer's Delight", "FarmersDelight-1.20.1-1.2.6.jar"), I("FerriteCore", "ferritecore-6.0.1-forge.jar", "ferrite-core"),
      I("GeckoLib", "geckolib-forge-1.20.1-4.4.9.jar", "geckolib"), I("Jade", "Jade-1.20.1-Forge-11.12.3.jar", "jade"), I("JourneyMap", "journeymap-1.20.1-5.10.3-forge.jar.disabled", "journeymap", { off: true }),
      I("Just Enough Items (JEI)", "jei-1.20.1-forge-15.3.0.4.jar", "jei"), I("ModernFix", "modernfix-forge-5.19.4+mc1.20.1.jar", "modernfix"), I("Mowzie's Mobs", "mowziesmobs-1.6.4.jar"),
      I("Patchouli", "Patchouli-1.20.1-84-FORGE.jar", "patchouli"), I("Sophisticated Backpacks", "sophisticatedbackpacks-1.20.1-3.21.2.1155.jar", "sophisticated-backpacks"),
      I("Sophisticated Core", "sophisticatedcore-1.20.1-1.0.4.818.jar", "sophisticated-core"), I("spark", "spark-1.10.53-forge.jar", "spark"),
    ],
    proxy: [
      I("Floodgate", "floodgate-velocity.jar"), I("Geyser", "Geyser-Velocity.jar", "geyser"), I("LibertyBans", "LibertyBans-1.1.0.jar"), I("LuckPerms", "LuckPerms-Velocity-5.4.145.jar", "luckperms"),
      I("MiniMOTD", "minimotd-velocity-2.1.4.jar"), I("SkinsRestorer", "SkinsRestorer.jar", "skinsrestorer"), I("spark", "spark-1.10.119-velocity.jar", "spark"),
      I("TAB", "TAB v5.0.3.jar", "tab-was-taken"), I("ViaVersion", "ViaVersion-5.2.1.jar", "viaversion"),
    ],
  };
  // 整合包里没装上的（同共用件页「没装上的文件」那三个的说法，换成这台 Forge 1.20.1 的文件名）
  const PENDING = [
    ["create_new_age-forge-1.20.1-1.1.2.jar", "站点不在允许列表里", "下载地址不在 MineDeck 允许的站点列表里（只允许 Modrinth、GitHub、GitLab），确认站点可信后才能下载"],
    ["createoreexcavation-1.20-1.4.3.jar", "没有 CurseForge API Key", "没有填 CurseForge API Key，不能自动下载。保留本地已有的文件，或者手动放一份"],
    ["create-pack-tweaks-1.0.jar", "整合包没给校验值", "整合包没有提供这个文件的校验值，自动下载没法核对，不能用下载替代你手里的文件"],
  ];
  const LOCAL = { paper: ["SimpleTpa-1.4.jar", "46 KB"], forge: ["create_factory-0.4b-1.20.1.jar", "1.2 MB"], proxy: ["VelocityWhitelist-1.1.jar", "38 KB"] };
  // 类别片：同向导第二步（程序叫「支持库」，作者 11-26 改叫「前置库」）
  const ACATS = {
    模组: [["optimization", "优化"], ["utility", "实用"], ["management", "管理"], ["worldgen", "世界生成"], ["adventure", "冒险"], ["technology", "科技"],
      ["magic", "魔法"], ["mobs", "生物"], ["equipment", "装备"], ["storage", "存储"], ["library", "前置库"]],
    插件: [["management", "管理"], ["utility", "实用"], ["optimization", "优化"], ["economy", "经济"], ["social", "聊天社交"], ["minigame", "小游戏"],
      ["game-mechanics", "游戏机制"], ["worldgen", "世界生成"], ["library", "前置库"]],
  };
  const PAGE = 20, CAP = 100;
  const A = {};
  const dat = () => {
    const k = C.kind;
    if (!A[k]) A[k] = { view: "inst", mods: JSON.parse(JSON.stringify(SEED[k] || [])), q: { inst: "", add: "" }, cat: "", picked: [], installing: false, shown: 0, more: false };
    return A[k];
  };
  const noun = () => nounOf(C.S.core);
  const sample = () => (window.MR_SAMPLE && window.MR_SAMPLE[noun()]) || null;
  const addonOf = id => sample()?.items.find(x => x.id === id);
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
  const STOP_WHY = "要停用、启用或删除，先停止服务器：运行中这些文件被服务器占着，改了也要重启才算数";
  const sortMods = list => list.sort((a, b) => a.n.localeCompare(b.n, "en", { sensitivity: "base" }));
  function tagsOf(m) {
    const ids = installedIds(), t = [];
    if (m.need && !ids.has(m.need[0])) t.push(`<button type="button" class="ad-tag warn" data-ad-act="need" data-ad-need="${esc(m.need[1])}" title="${esc(`${m.n} 需要 ${m.need[1]}，没装的话 ${m.n} 加载不了；点了去「添加」里找它`)}">缺前置 ${esc(m.need[1])}</button>`);
    if (m.err) t.push(`<span class="ad-tag warn" title="${esc("读不出这个文件里的信息：" + m.err)}">读不出信息</span>`);
    if (m.fresh) t.push(`<span class="ad-tag" title="服务器开着时装上的，重启以后才加载">重启后生效</span>`);
    return t.join("");
  }
  function instRow(m) {
    const dis = live(), tip = dis ? STOP_WHY : m.off ? "启用（文件名去掉 .disabled，下次启动生效）" : "停用（文件改名成 .jar.disabled，服务器不再加载它，随时能启用；下次启动生效）";
    const x = m.id && addonOf(m.id);
    return `<div class="ad-row${m.off ? " off" : ""}" data-ad-file="${esc(m.file)}">`
      + `<button type="button" class="ad-tg${m.off ? "" : " on"}" data-ad-act="tg" role="switch" aria-checked="${!m.off}" aria-label="${esc(m.n)}"${dis ? ' aria-disabled="true"' : ""} title="${esc(tip)}"><span class="ad-sw"></span></button>`
      + tile(m.n, m.id, x && x.icon) + `<div class="ad-m"><div class="ad-n"><span class="nm">${esc(m.n)}</span><span class="ad-tags" data-ad-k="tags">${tagsOf(m)}</span></div>`
      + `<div class="ad-f" data-ad-k="file">${esc(m.file)}</div></div>`
      + `<button type="button" class="ad-ib ad-x" data-ad-act="del"${dis ? ' aria-disabled="true"' : ""} title="${esc(dis ? STOP_WHY : "删除（移进服务器文件夹里的 .minedeck\\已删除，想要回来从那里挪回去）")}"><i data-ic="trash"></i></button></div>`;
  }
  const pendRows = () => C.kind === "forge" && (+C.QS.get("notice") || 0) >= 3 ? PENDING.map(([f, why, full]) =>
    `<div class="ad-row pend"><span class="ad-pw" title="整合包里的这个文件没装上"><i data-ic="warn"></i></span>${tile(f.replace(/[-_].*$/, ""), f)}`
    + `<div class="ad-m"><div class="ad-n"><span class="nm mono">${esc(f)}</span></div><div class="ad-f" title="${esc(full)}">${esc(why)}</div></div>`
    + `<button type="button" class="ad-btn" data-ad-act="pend" title="选处理方式：跳过、用本地文件代替，或者确认站点可信以后下载">处理…</button></div>`).join("") : "";
  const instMatch = m => { const q = dat().q.inst.trim().toLowerCase(); return !q || (m.n + " " + m.file).toLowerCase().includes(q); };
  function instList() {
    const D = dat(), rows = D.mods.filter(instMatch);
    if (!D.mods.length) return `<div class="ad-empty">${dirOf(C.S.core)} 文件夹里还没有${noun()}</div>`;
    return (D.q.inst.trim() ? "" : pendRows()) + (rows.length ? rows.map(instRow).join("") : `<div class="ad-empty">没有包含「${esc(D.q.inst.trim())}」的${noun()}</div>`);
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
    return `<div class="ad-mod${on ? " in" : ""}${done ? " inst" : ""}" data-ad-addon="${x.id}"${done ? ` title="已经装在服务器里了"` : ""}>${tile(x.n, x.id, x.icon, "lg")}`
      + `<div class="ad-mm"><div class="n"><span class="nm">${esc(x.n)}</span>${extLink(x)}</div><div class="d" title="${esc(x.d)}">${esc(x.d)}</div>`
      + `<div class="meta"><i data-ic="download"></i>${fmtDl(x.dl)}${done ? " · 已安装" : ftag(x) ? ` · ${ftag(x)}` : ""}</div></div>`
      + `<span class="ad-add"><i data-ic="${on || done ? "check" : "plus"}"></i></span></div>`;
  }
  function gridEnd(total) {
    const D = dat();
    return `<div class="ad-gend">${!D.cat && !D.q.add.trim() && sample().total > CAP ? `仅展示热门前 ${CAP} 项，更多内容可通过搜索或分类查找` : `已显示全部 ${total} 个。`}</div>`;
  }
  const skel = n => `<div class="ad-mod skel"><span class="ad-mt lg"></span><div class="ad-mm"><i></i><i></i><i></i></div></div>`.repeat(n);
  function gridHTML() {
    const D = dat();
    if (!sample()) return skel(6);
    const items = gridItems(), has = new Set(D.picked.map(p => p.id)), inst = installedIds();
    if (!items.length) return `<div class="ad-empty grid">未找到匹配的${noun()}，请换个关键词或分类。</div>`;
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
      const added = [...g.querySelectorAll(".ad-mod")].slice(D.shown); hy(g);
      added.forEach((c, i) => c.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: Math.min(i, 9) * 22, easing: EASE_OUT, fill: "backwards" }));
      D.shown += next.length;
      if (D.shown >= total) g.insertAdjacentHTML("beforeend", gridEnd(total));
      D.more = false; g.closest(".ad-lw")._paint?.(); moreCards();
    }, 600);
  }

  // ================= 右栏：待装清单（同向导第二步） =================
  function crowHTML(p) {
    if (p.local) return `<div class="ad-crow" data-ad-id="${esc(p.id)}">${tile(p.file, p.file)}<div class="ad-m"><div class="ad-n"><span class="nm">${esc(p.file)}</span></div><div class="ad-why">本地文件 · ${esc(p.kb)}</div></div>`
      + `<span class="ad-cx" data-ad-unpick="${esc(p.id)}" title="从清单里拿掉"><i data-ic="x"></i></span></div>`;
    const x = addonOf(p.id), by = p.by && addonOf(p.by); if (!x) return "";
    const why = by ? `${esc(by.n)} 的前置依赖` : noun() === "插件" && x.cs === "required" ? "需客户端同步安装" : noun() === "模组" && x.cs !== "required" ? "仅服务端，客户端不用装" : esc(x.d);
    return `<div class="ad-crow" data-ad-id="${x.id}">${tile(x.n, x.id, x.icon)}<div class="ad-m"><div class="ad-n"><span class="nm">${esc(x.n)}</span>${by ? `<span class="ad-ftag">前置</span>` : ""}</div><div class="ad-why">${why}</div></div>`
      + `<span class="ad-cx" data-ad-unpick="${x.id}" title="从清单里拿掉"><i data-ic="x"></i></span></div>`;
  }
  const cartEmpty = () => `<div class="ad-cempty"><b>暂未选择任何${noun()}</b>点击左侧 ＋ 添加，<br>前置依赖将自动补全。</div>`;
  const insBtn = () => { const D = dat(), n = D.picked.length;
    return D.installing ? `<button type="button" class="ad-ins" aria-disabled="true"><i data-ic="spinner" class="ad-spin"></i>正在安装</button>`
      : `<button type="button" class="ad-ins" data-ad-act="install"${n ? (live() ? ` title="服务器开着也能装，重启以后才加载"` : "") : ` aria-disabled="true" title="先把要装的放进清单"`}><i data-ic="download"></i>全部安装${n ? ` · ${n}` : ""}</button>`; };
  function cartNote() {
    const D = dat(), f = D.picked.filter(p => !p.local && addonOf(p.id)?.cs === "required").length;
    return f && noun() === "插件" ? `${icon("info")}其中 ${f} 个需客户端同步安装` : "";
  }
  function cartHTML() {
    const D = dat(), n = D.picked.length;
    return `<section class="ad-col ad-cart${n ? " has" : ""}">${(C.folders || []).length ? `<div class="ad-fd">${C.folders.map(([i, t]) => `<button type="button" title="${esc(t)}"><i data-ic="${i}"></i></button>`).join("")}</div>` : ""}`
      + `<div class="ad-chd"><span class="ad-t">待装清单</span><span class="ad-cnt">${n ? `· ${n}` : ""}</span><span class="ad-sp"></span><button type="button" class="ad-clr" data-ad-act="clear">清空</button></div>`
      + `<div class="ad-lw ad-clw"><div class="list">${cartEmpty()}${D.picked.map(crowHTML).join("")}</div><span class="thumb"></span></div>`
      + `<div class="ad-cnote">${cartNote()}</div>`
      + `<div class="ad-drop" data-ad-act="local" title="自己下载的 .jar 也能放进清单，和 Modrinth 上挑的一起装"><i data-ic="folder"></i>拖入或选择 .jar 文件</div>`
      + `<span class="ad-insslot">${insBtn()}</span></section>`;
  }

  // ================= 整页 =================
  function headHTML() {
    const D = dat(), inst = D.view === "inst";
    const ctx = inst ? "" : `<span class="ad-ctx" title="草图用的是向导第二步那份 Modrinth 样本（${noun() === "插件" ? "Paper 26.2 的插件" : "NeoForge 26.1 的模组"}），和这台的核心、版本对不上；程序按这台服务器现取">适配 ${esc(C.S.label)} ${esc(C.S.mc)} · Modrinth</span>`;
    return `<div class="ad-hd"><div class="ad-seg"><i class="ad-sgl"></i><span class="${inst ? "on" : ""}" data-ad-view="inst">已安装<span class="ad-sn" data-ad-k="sn">${D.mods.length}</span></span>`
      + `<span class="${inst ? "" : "on"}" data-ad-view="add">添加</span></div><span class="ad-ctxslot" data-ad-k="ctx">${ctx}</span><span class="ad-sp"></span>`
      + `<button type="button" class="ad-ib r4" data-ad-act="refresh" title="${inst ? `刷新已安装的${noun()}` : "刷新"}"><i data-ic="refresh"></i></button>`
      + `<label class="ad-find"><i data-ic="search"></i><input class="ad-q" value="${esc(inst ? D.q.inst : D.q.add)}" placeholder="${inst ? `搜索已安装的${noun()}` : `搜索${noun()}（建议英文名）`}" autocomplete="off" spellcheck="false"></label></div>`;
  }
  function html(ctx) {
    C = ctx; closeAsk(true); const D = dat();
    return `<div class="ad"><section class="ad-col ad-main">${headHTML()}<div class="ad-body">${D.view === "inst" ? instBody() : addBody()}</div></section>${cartHTML()}</div>`;
  }
  // 名字、文件名放不下被截断时，全文进悬停（同「玩家」的 fitTitle）
  const fitTitles = r => $$(".ad-f:not([title]), .ad-n .nm", r).forEach(el => { if (el.scrollWidth > el.clientWidth + 1) el.title = el.textContent; });
  function after(r) {
    $$(".ad-lw", r).forEach(w => C.thinBar(w));
    fitTitles(r);
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
    if (dat().view === "inst") { $$(".ad-ilw .ad-row[data-ad-file]", r).forEach(row => { const m = dat().mods.find(x => x.file === row.dataset.adFile), x = m && m.id && addonOf(m.id);
      if (x && x.icon) { const t = document.createElement("span"); t.innerHTML = tile(m.n, m.id, x.icon); row.querySelector(".ad-mt").replaceWith(t.firstElementChild); } }); return; }
    const g = $(".ad-grid", r); g.innerHTML = gridHTML(); hy(g); g.closest(".ad-lw")._paint?.();
    $(".ad-clw .list", r) && dat().picked.length && (() => { const l = $(".ad-clw .list", r); l.innerHTML = cartEmpty() + dat().picked.map(crowHTML).join(""); hy(l); })();
  }

  // ---- 换着看：已安装 ↔ 添加。分段的选中块滑过去，下面这一块往点的方向逐项换（规范第七节），搜索框换成这一边的字 ----
  function setView(v) {
    const D = dat(), r = root(); if (D.view === v || !r) return;
    const dir = v === "add" ? 1 : -1; D.view = v;
    $$(".ad-seg span[data-ad-view]", r).forEach(s => s.classList.toggle("on", s.dataset.adView === v)); placeSeg(true);
    const tmp = document.createElement("div"); tmp.innerHTML = headHTML();
    swapKeyed($(".ad-hd", r), tmp.innerHTML, dir, "X");
    const q = $(".ad-q", r); q.value = v === "inst" ? D.q.inst : D.q.add; q.placeholder = v === "inst" ? `搜索已安装的${noun()}` : `搜索${noun()}（建议英文名）`;
    const rf = $("[data-ad-act=refresh]", r); rf.title = v === "inst" ? `刷新已安装的${noun()}` : "刷新"; delete rf.dataset.tip;
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
      l.scrollTop = 0; l.innerHTML = D.view === "inst" ? instList() : gridHTML(); hy(l); a.cancel();
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
    } else {
      D.picked.push({ id, by: null });
      const inst = installedIds();
      for (const d of addonOf(id)?.deps || []) if (!D.picked.some(p => p.id === d) && !inst.has(d)) D.picked.push({ id: d, by: id });
    }
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
    const list = $(".ad-clw .list", r);
    const gone = [...list.querySelectorAll(".ad-crow:not(.ad-out)")].filter(x => !has.has(x.dataset.adId));
    if (clear && gone.length) { // 清空、装完：从上往下一行接一行往右划出，间隔从 90 毫秒起每次缩成四分之三（同向导「清空」）
      let delay = 0, gap = 90;
      gone.forEach((x, i) => {
        x.classList.add("ad-out");
        const an = x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateX(40px)" }], { duration: 220, delay, easing: EASE_IN, fill: "forwards" });
        delay += gap; gap = Math.max(16, Math.round(gap * .75));
        if (i === gone.length - 1) an.onfinish = () => { gone.forEach(y => y.remove()); list.closest(".ad-lw")._paint?.(); };
      });
    } else gone.forEach((x, i) => dropOut(x, () => list.closest(".ad-lw")._paint?.(), i * 40));
    D.picked.forEach(p => {
      if (list.querySelector(`.ad-crow[data-ad-id="${CSS.escape(p.id)}"]:not(.ad-out)`)) return;
      list.insertAdjacentHTML("beforeend", crowHTML(p)); const x = hy(list.lastElementChild);
      x.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: p.by ? 60 : 0, easing: EASE_OUT, fill: "backwards" });
      x.scrollIntoView({ block: "nearest", behavior: "smooth" });
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
  // 点「全部安装」：跳后台任务进度页 task.html?kind=cart 从头放一遍（地址里只带配色参数）；清单空着时（按钮灰着）不跳
  function toCartTask() {
    if (!dat().picked.length) return;
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
          l.insertBefore(row, next || null); growIn(row, 200);
        });
        // 装上的正好是别人缺的前置：那一行的「缺前置」标记跟着拿掉
        D.mods.forEach(m => { if (m.need) { const row = $(`.ad-row[data-ad-file="${CSS.escape(m.file)}"]`, l); if (row) swapKeyed(row, instRow(m)); } });
        l.closest(".ad-lw")._paint?.();
      } else {
        const inst = installedIds();
        $$(".ad-grid .ad-mod:not(.skel)", r).forEach(m => {
          if (!inst.has(m.dataset.adAddon) || m.classList.contains("inst")) return;
          const x = addonOf(m.dataset.adAddon), tmp = document.createElement("div"); tmp.innerHTML = cardHTML(x, new Set(), inst);
          m.replaceWith(hy(tmp.firstElementChild));
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
  // ---- 已安装：启用 / 停用、删除 ----
  function toggleMod(row) {
    const D = dat(), m = D.mods.find(x => x.file === row.dataset.adFile); if (!m) return;
    m.off = !m.off; m.file = m.off ? m.file + ".disabled" : m.file.replace(/\.disabled$/i, "");
    row.dataset.adFile = m.file; row.classList.toggle("off", m.off);
    const tg = $(".ad-tg", row); tg.classList.toggle("on", !m.off); tg.setAttribute("aria-checked", !m.off);
    tg.title = m.off ? "启用（文件名去掉 .disabled，下次启动生效）" : "停用（文件改名成 .jar.disabled，服务器不再加载它，随时能启用；下次启动生效）"; delete tg.dataset.tip;
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
      + `<div class="ad-mb"><span class="mono">${esc(m.file)}</span> 会移进服务器文件夹里的 <span class="mono">.minedeck\\已删除\\${dir}</span>，不会直接删掉；想要回来，从那里挪回 ${dir} 文件夹就行。</div></div>`
      + `<div class="ad-ma"><button type="button" class="ad-b fk" data-ad-x="no">取消</button><button type="button" class="ad-b dgr" data-ad-x="ok">删除${noun()}</button></div></div>`;
    document.body.appendChild(hy(mk));
    const no = $("[data-ad-x=no]", mk); no.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return; // 已经关了（正在淡出时又点了一下）
      if (e.target.closest("[data-ad-x=ok]")) {
        closeAsk(); D.mods = D.mods.filter(x => x !== m);
        const rr = root(), el = rr && $(`.ad-row[data-ad-file="${CSS.escape(m.file)}"]`, rr);
        if (el) dropOut(el, () => el.closest?.(".ad-lw")?._paint?.());
        if (rr) { paintCount();
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
    const a = e.target.closest("[data-ad-act]"); if (!a || a.getAttribute("aria-disabled") === "true") return;
    const act = a.dataset.adAct, row = a.closest(".ad-row");
    if (act === "tg" && row) toggleMod(row);
    else if (act === "del" && row) askDelete(row);
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
  addEventListener("resize", () => { placeSeg(false); placeChip(false); chipFade(); });

  window.DETAIL_TABS.addons = { html, mount };
})();
