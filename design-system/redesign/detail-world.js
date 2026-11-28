// 服务器详情页 →「世界与备份」的两个子页：备份（world/backups）、换地图（world/map）。11-27 夜里派子代理照临时规范画的第一稿，
// 计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」。样式在 detail-world.css，选择器都带 .wd- 前缀。
//
// 程序现在的样子（internal/web/ui/app.js 约 4113 行「view === "backups"」；换地图整块在 worldimport.js 的 render，约 476 行起）。
//
// ---- 备份 ----
// 程序：页顶一句「当前世界：从 … 的备份还原（标签），还原于 …」（或导入的地图、换回过、随交接包带来、没有记录），
//       标签输入框 +「立即备份世界」，「备份放在 <路径> 打开 · 保留最近 10 份（所有服务器共用）到设置里改」，改过存放位置时另一行说旧文件夹，
//       一大段说明（运行中也能备份、超过份数删最旧的、还原时会先停服、还原前自动备份、定时备份在「定时任务」里加），
//       下面一行一份：标签或文件名、「现在的世界」「在原来的文件夹」标记、时间 · 大小 · 文件名，运行中写「会先停服」，「还原」「删除」两个按钮。
// 这里：内容面板里一栏（圆角 18 = 26 − 8），从上到下：
//   · 顶上一行 28 高（同「玩家」各栏）：「备份」和份数「9 / 10」（后面那个数是保留份数，所有服务器共用；说明和到哪改进悬停，一个值只说一次）；
//     右边是「当前世界」那句（只在现在的世界不是列表里某一份时才出，比如导入的地图、换回过的；从列表里某一份还原的，就在那一行标「现在的世界」，不再另说一遍），
//     再右边一颗 28 高的胶囊「每天 04:00」（定时备份，点了去「定时任务」；没有定时备份时淡着写「定时备份」），最右边贴栏角是「打开备份文件夹」（外壳给的 ctx.folders）。
//   · 中间是列表，一行 40 高、圆角 10：时间（等宽）、标签（「定时」「还原前自动」是程序自己打的，淡一点）、标记、大小；「还原」「删除」悬停那一行才出来
//     （同「玩家」名单里的 ×、向导待装清单）。文件名进时间的悬停。多了在栏里滚（细滚动条）。
//   · 最下面一行：标签输入框（40 高、圆角 10，程序的占位字）+「立即备份世界」（40 高、圆角 10，同联机页樱花那行「连接」的做法）。
//     启动中灰掉（后端这时拒绝备份：「服务器正在启动或停止，请稍后再备份」）。程序那段说明拆进各按钮的悬停。
//   · 还原：危险确认框（共用件页的样子：标题点名是哪一份、按钮写出后果、默认聚焦「取消」）。运行中、启动中按钮写「停止并还原」，带在线人数。
//     照程序修好以后的做法画：只换世界，核心、插件和服务器设置都不动（框里不写「会覆盖设置」）。
//     还原完列表顶上多一份「还原前自动」，「现在的世界」从原来那行挪到这一行（只换这两处标记），超过保留份数时最旧的一份往右划掉。
//   · 删除：危险确认框，同共用件页「删除备份「更新模组前」？」那张。删掉的正好是「现在的世界」那份时，顶上那句跟着出来（程序也是这样：worldFrom 还在）。
//   · 程序里没有下载备份（只能打开文件夹），也没有备份页自己的「自动备份」开关（自动的就是「定时任务」里的定时备份，标签「定时」），这里照程序不加。
//
// ---- 换地图 ----
// 程序：「当前地图 world（level-name）· 服务器运行中」+「导入并替换当前地图」，一段说明，「恢复点」列表（每条：名字、状态标签、一句话、时间 · 来源 · 大小、
//       「恢复导入前的地图」「删除恢复点」，要先停服）；点了导入进流程：选来源（选文件夹 / 填路径 / 上传 ZIP）→ 一个来源里有几张时挑一张 →
//       核对（版本关系、要确认的、要留意的、地图里有什么、会换掉什么、可以一起改的设置、要多少空间、运行中「停服并导入」）→ 确认框 → 任务进度（五步、复制时的经验条、能取消）。
// 这里和新建的「导入地图存档」（import.html?flow=map）用同一套：虚线框、填路径那一行、封面条、左「地图 · 3」挑一张（选中框滑过去）、右「地图里有什么」那张小表、
// 「我已了解」开关（新版本、快照要确认，不开不让往下走，描红、晃一下）。详情页里没有向导浮在窗口底部的上一步、下一步，
// 所以换成面板底下一排 36 高的胶囊（36 的一半 18 = 26 − 8，又是胶囊、又和面板下角同心，同共用件页弹窗底下那排）。
//   · 打开时：左右两栏。左「替换当前地图」：虚线框（点了选压缩包、拖进来）+ 填路径那一行（选文件夹、检测）；标题后面淡淡一个 world（level-name，说明进悬停）。
//     右「恢复点」：一条一块（圆角 10），名字和状态标签、时间 · 来源 · 大小，「恢复导入前的地图」和删除；那句解释进状态标签的悬停。停着才能点，开着灰掉、悬停说原因。
//     贴栏角是「打开世界文件夹」。程序那段说明（原来的世界不会删、留在恢复点、核心插件设置都不动）进虚线框的悬停。
//   · 选了来源：虚线框里上传进度（30 格）或「正在识别…」，认出来以后整页换步到「选择地图」：封面条（来源）+ 两栏 + 底下「上一步」「下一步」。
//   · 「核对」：封面条换成选的那张地图；左栏版本关系（保存于 → 这台服务器，一个标签说一致 / 会升级 / 读不了）和会换掉什么、不动什么、要多少空间、要留意的；
//     右栏「可以一起改的设置」（地图和服务器不一样的几项，开关默认关；都一样时没有这一栏）；
//     底下一条（52 高、圆角 18，同 EULA）：「我已了解：…」和运行中的「停服并导入」；地图比服务器新时这一条换成红字，主键灰掉。
//   · 确认框 → 任务进度：封面条下一栏，左边五步（程序 worldimport importSteps 的原话），右边复制的进度（30 格）和过程记录；复制完以前能取消。
//     做完点「完成」回到打开时那页，恢复点顶上多一条（从下面淡进来）。
// 示例数据：Paper、原版用 import.html 那个 SkyCity_v2.1.zip（三张，第三张只有 level.dat_old 不能导入）；地图存于 1.20.4，比 Paper 1.21.4、原版 26.2 旧，
// 所以要「我已了解」。Forge 那台 1.20.1 换成一个朋友的机械动力存档：第一张 1.20.1 能直接换，第二张 1.20.4 比服务器新、核对时拦下。都是编的。
// 原版那台以前导入过 SkyCity（有一条能换回的恢复点），Forge 那台换过一次又换回了，Paper 那台没换过：备份页顶上那句「当前世界」跟着这里的记录走。
// 草图里换不了页头的状态（外壳没给这个接口）：「停止并还原」「停服并导入」做完以后页头还是运行中，改程序时照真的走。
(() => {
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", EASE_SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const m = s => `<span class="mono">${esc(s)}</span>`;
  const hy = el => { window.KIT.hydrate(el); return el; };
  const icon = (n, cls) => window.KIT.icon(n, cls);
  const pad = n => String(n).padStart(2, "0");
  const size = mb => mb >= 1024 ? (mb / 1024).toFixed(mb >= 10240 ? 0 : 1) + " GB" : mb.toFixed(1) + " MB";
  let C = null; // 外壳最近一次给的 ctx

  // ================= 数据（每台服务器一份；换标签回来还在） =================
  const KEEP = 10; // 程序默认保留份数（app.DefaultBackupKeep），所有服务器共用
  // 备份文件名：时间（20060102-150405）加可选的「-标签」，同 inst/backup.go 的 parseBackupName
  const parse = f => { const x = f.match(/^(\d{4})(\d\d)(\d\d)-(\d\d)(\d\d)(\d\d)(?:\.\d+)?(?:-(.+))?\.zip$/); return x ? { time: `${x[1]}-${x[2]}-${x[3]} ${x[4]}:${x[5]}`, label: x[7] || "" } : { time: "", label: "" }; };
  const bk = (file, mb, old) => ({ file, mb, ...parse(file), old: old || "" }); // old：改存放位置以前的旧文件夹（程序的 elsewhere + dir）
  const SEED = {
    // Paper：天天 04:00 定时备份；11-22 从「开荒第一周」还原过一次（那时自动做了一份「还原前自动」）；最旧那份还在改存放位置以前的文件夹里
    paper: { dir: "D:\\MineDeck\\backups\\生存服 · 朋友", sched: "04:00", worldMb: 823, worlds: ["world", "world_nether", "world_the_end"],
      props: { difficulty: "easy", gamemode: "survival" },
      backups: [bk("20261127-040000-定时.zip", 812.4), bk("20261126-213012-更新插件前.zip", 806.0), bk("20261126-040000-定时.zip", 798.7),
        bk("20261125-040000-定时.zip", 791.2), bk("20261124-193544.zip", 785.9), bk("20261124-040000-定时.zip", 780.3),
        bk("20261122-150210-还原前自动.zip", 771.8), bk("20261120-221903-开荒第一周.zip", 640.1), bk("20261115-120400-刚建服.zip", 212.5, "E:\\旧备份\\生存服 · 朋友")],
      wf: { backup: "20261120-221903-开荒第一周.zip", at: "2026-11-22 15:02" }, points: [] },
    // Forge：没有定时备份；11-23 换过一张测试图，又换回来了（恢复点里留着那张导入的地图）
    forge: { dir: "D:\\MineDeck\\backups\\机械动力整合包", sched: "", worldMb: 2410, worlds: ["world"], props: { difficulty: "easy", gamemode: "survival" },
      backups: [bk("20261126-224012-更新模组前.zip", 2366.4), bk("20261125-193000.zip", 2291.7), bk("20261123-201533-火车站建好.zip", 2150.2), bk("20261121-180210.zip", 1662.9)],
      wf: null, points: [{ id: "f1", name: "机械动力 · 测试图", state: "restored", created: "2026-11-23 18:40", source: "create_test_world.zip", keptMb: 402.6, oldMb: 0, hasOld: true }] },
    // 原版：11-20 用 SkyCity 换了地图（导入前的世界留在恢复点里，能换回）
    vanilla: { dir: "D:\\MineDeck\\backups\\原版快照测试", sched: "", worldMb: 1204, worlds: ["world"], props: { difficulty: "easy", gamemode: "survival" },
      backups: [bk("20261126-220130.zip", 1190.6), bk("20261120-201005-换地图前.zip", 88.7)],
      wf: null, points: [{ id: "v1", name: "天空之城 SkyCity", state: "committed", created: "2026-11-20 20:14", source: "SkyCity_v2.1.zip", oldMb: 88.4, hasOld: true, canReturn: true }] },
  };
  // 换地图的来源（字段照 /api/world/scan 的候选，同 import.html 的 MAPSRC）
  const README = "天空之城 SkyCity v2.1\n作者：steve　测试：alex\n\n建议难度：普通　游戏模式：冒险\n建议人数：1～4 人\n\n【玩法】\n从出生点的告示牌开始，一路往上爬到城顶的灯塔。\n一共五层，每层有一个钥匙，拿齐五把才能开灯塔的门。\n掉下去会回到这一层的存档点，不扣东西。\n\n【注意】\n地图自带数据包 skycity_core，别删，删了机关不动。\n不要改游戏规则 commandBlockOutput，会刷屏。\n多人玩时建议关掉 PVP。\n\n【资源包】\n资源包 resources.zip 放在世界文件夹里，进服前自己装上。\n不装也能玩，只是钥匙和门的样子是原版的。\n\n【更新记录】\nv2.1　修了第三层的门卡住\nv2.0　加了第五层\nv1.0　第一版\n\n更新记录、反馈：https://example.com/skycity\n请勿二次发布。";
  const SKY = { file: "SkyCity_v2.1.zip", mb: 1331, path: "D:\\Maps\\SkyCity", cands: [
    { name: "天空之城 SkyCity", root: "SkyCity_v2.1/SkyCity", ver: "1.20.4", mb: 1126, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 186], ["下界", 12], ["末地", 4]], missing: [], dps: ["skycity_core"], rp: true, players: 0, readme: "README.txt", hints: ["安装说明.png"],
      diff: "normal", mode: "adventure", clues: [], notes: [] },
    { name: "天空之城（建造存档）", root: "SkyCity_v2.1/SkyCity_build", ver: "1.20.4", mb: 214, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 40]], missing: ["下界", "末地"], dps: [], rp: false, players: 1, readme: "", hints: [], diff: "peaceful", mode: "creative", clues: [],
      notes: [["warn", "单人存档：原玩家背包与经验能否衔接取决于登录账号", "此为单人存档，地形、建筑与容器内容均完整保留；原玩家的背包与经验能否衔接取决于进入服务器使用的账号（开启正版验证并使用同一正版账号通常可衔接）。MineDeck 不会修改玩家数据文件"]] },
    { name: "SkyCity", root: "SkyCity_v2.1/old/SkyCity", ver: "", mb: 1024, block: "只有备份用的 level.dat_old",
      blockFull: "该存档仅含备份文件 level.dat_old，缺少 level.dat。请先将 level.dat_old 复制并重命名为 level.dat，确认可在单人游戏中打开后再导入" },
  ] };
  const CREATE = { file: "朋友的机械动力存档.zip", mb: 2087, path: "D:\\Maps\\create_s2", cands: [
    { name: "机械动力生存 · 第二季", root: "create_s2/world", ver: "1.20.1", mb: 1843, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 412], ["下界", 38], ["末地", 9]], missing: [], dps: [], rp: false, players: 4, readme: "", hints: ["mods.txt"],
      diff: "normal", mode: "survival", clues: ["forge", "create", "createaddition", "railways"],
      notes: [["info", "该地图曾使用模组：缺少对应模组时相关方块与物品将消失", "检测到 Create、Create Crafts & Additions、Steam 'n' Rails 线索。若服务器未安装对应模组，相关方块与物品将在首次加载时被清除；当前服务器已全部安装"]] },
    { name: "测试世界", root: "create_s2/test", ver: "1.20.4", mb: 96, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 6]], missing: ["下界", "末地"], dps: [], rp: false, players: 1, readme: "", hints: [], diff: "peaceful", mode: "creative", clues: [], notes: [] },
  ] };
  const W = {};
  const dat = () => {
    const k = C.kind;
    if (!W[k]) {
      const s = SEED[k] || { dir: "", sched: "", worldMb: 0, worlds: ["world"], props: { difficulty: "easy", gamemode: "survival" }, backups: [], wf: null, points: [] };
      W[k] = JSON.parse(JSON.stringify(s));
      W[k].level = "world"; W[k].src = k === "forge" ? CREATE : SKY;
      W[k].flow = freshFlow();
    }
    return W[k];
  };
  const freshFlow = () => ({ screen: "home", phase: "empty", pct: 0, path: "", fromPath: false, fileName: "", cand: 0, ack: false, stop: false, sets: {}, readme: false, task: null, tok: 0 });

  // ================= 共用的小件 =================
  // 只换变了的那几样：新的 HTML 里带 data-wd-k 的，和旧的一样就不动；不一样的旧的 0.09 秒往反方向挪 5 像素淡掉，新的 0.22 秒挪进来，错开 22 毫秒（规范第七节）
  function swapKeyed(box, html, dir = 1, axis = "Y") {
    if (!box) return;
    const tmp = document.createElement("div"); tmp.innerHTML = html; hy(tmp);
    const tf = v => axis === "X" ? `translateX(${v}px)` : `translateY(${v}px)`;
    let i = 0;
    $$("[data-wd-k]", tmp).forEach(n => {
      const o = box.querySelector(`[data-wd-k="${n.dataset.wdK}"]`); if (!o) return;
      const same = o.innerHTML === n.innerHTML && o.className === n.className && (o.getAttribute("title") || o.dataset.tip || "") === (n.getAttribute("title") || "");
      if (same) return;
      const tok = o._tok = (o._tok || 0) + 1, d = i++ * 22;
      o.getAnimations().forEach(a => a.cancel());
      o.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, delay: d, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
        if (o._tok !== tok) return;
        o.innerHTML = n.innerHTML; o.className = n.className; delete o.dataset.tip;
        if (n.hasAttribute("title")) o.setAttribute("title", n.getAttribute("title")); else o.removeAttribute("title");
        if (o.classList.contains("wd-ps")) fitWdEl(o);
        o.getAnimations().forEach(a => a.cancel());
        o.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT, fill: "backwards" });
      };
    });
  }
  // 一块里的几样整批换（虚线框里的图标、一句、小字）：旧的一起淡掉，新的一样一样淡进来
  function swapItems(box, html, dir = 1, axis = "Y") {
    if (!box) return;
    const tf = v => axis === "X" ? `translateX(${v}px)` : `translateY(${v}px)`;
    const tok = box._tok = (box._tok || 0) + 1, olds = [...box.children];
    olds.forEach(x => { x.getAnimations().forEach(a => a.cancel()); x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, easing: EASE_IN, fill: "forwards" }); });
    setTimeout(() => {
      if (box._tok !== tok) return;
      box.innerHTML = html; hy(box);
      [...box.children].forEach((x, i) => x.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
    }, olds.length ? 90 : 0);
  }
  // 新加的一行：高度从 0 长出来、从下面 5 像素淡进来（清单加减，规范第七节）
  function growIn(el, delay = 0) {
    const h = el.offsetHeight + "px";
    el.animate([{ opacity: 0, transform: "translateY(5px)", height: "0px", marginBottom: "-2px" }, { opacity: 0, transform: "translateY(5px)", height: h, marginBottom: "0px", offset: .45 },
      { opacity: 1, transform: "none", height: h, marginBottom: "0px" }], { duration: 360, delay, easing: EASE_OUT, fill: "backwards" });
  }
  // 拿掉一行：往右划 32 像素淡掉，再收起（同「玩家」名单、向导待装清单）
  function dropOut(el, done) {
    el.classList.add("wd-out");
    const h = el.offsetHeight + "px";
    el.animate([
      { opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: "translateX(32px)", height: h, marginBottom: "0px", offset: .55, easing: EASE_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 320, fill: "forwards" }).onfinish = () => { el.remove(); done?.(); };
  }
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }], { duration: 360, easing: EASE_SLIDE });
  const folders = () => {
    const D = dat();
    return (C.folders || []).length ? `<div class="wd-fd">${C.folders.map(([i, t]) => `<button type="button" title="${esc(t + (C.sub === "backups" && D.dir ? "\n" + D.dir : ""))}"><i data-ic="${i}"></i></button>`).join("")}</div>` : "";
  };
  const live = () => C.st !== "stopped";
  const addonNoun = () => ["paper", "purpur", "leaves", "folia", "velocity", "waterfall"].includes(C.S.core) ? "插件" : ["mohist", "banner"].includes(C.S.core) ? "插件和模组" : C.S.core === "vanilla" ? "" : "模组";
  const addonList = () => addonNoun().replace("和", "、");
  const keptWhat = () => `核心${addonList() ? "、" + addonList() : ""}和设置保持不变`;
  const bkLabelPh = () => { const a = addonNoun(); return `备份标签（选填，如：${a === "插件" ? "更新插件前" : a === "模组" ? "更新模组前" : a ? "更新插件或模组前" : "更新版本前"}）`; };
  const fitWdEl = el => { if (el.scrollWidth > el.clientWidth + 1) { el.title = el.textContent; delete el.dataset.tip; } else { el.removeAttribute("title"); delete el.dataset.tip; } };
  const fitWdTitles = r => $$(".wd-ps", r).forEach(fitWdEl);

  // ---- 确认框：同共用件页（commons.html）的危险确认框。挂在 body 上，换标签时收掉 ----
  let mask = null;
  function ask({ icon: ic, title, body, ok, onOk, w = 380 }) {
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "wd-mask enter" });
    mk.innerHTML = `<div class="wd-md" role="dialog" aria-modal="true" style="--w:${w}px"><div class="wd-mc"><div class="wd-mh"><span class="wd-mi"><i data-ic="${ic}"></i></span><h3>${title}</h3></div>`
      + `<div class="wd-mb">${body}</div></div><div class="wd-ma"><button type="button" class="wd-b fk" data-wd-x="no">取消</button><button type="button" class="wd-b dgr" data-wd-x="ok">${ok}</button></div></div>`;
    document.body.appendChild(hy(mk));
    const no = $("[data-wd-x=no]", mk); no.focus({ focusVisible: false });
    // 默认落在「取消」：鼠标点开时浏览器不画焦点框，回车却照样按到它，所以画一圈细框，一动别的就收（同共用件页）
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return; // 已经关了（正在淡出时又点了一下）
      if (e.target.closest("[data-wd-x=ok]")) { closeAsk(); onOk(); return; }
      if (e.target.closest("[data-wd-x=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) closeAsk(); });

  // ================= 备份 =================
  const bkRoot = () => document.querySelector(".cp > .wd-bk:not(.fade-out)");
  const isSys = l => l === "定时" || l === "还原前自动";
  const bkName = b => b.label || b.time;
  // 现在的世界从哪来（app.js 约 4134 行 nowText、worldimport.js 的 origin）：从列表里某一份还原的，就在那一行标「现在的世界」，这里不出
  function nowLine() {
    const D = dat(), wf = D.wf;
    if (wf) {
      if (D.backups.some(b => b.file === wf.backup)) return "";
      const p = parse(wf.backup);
      return `<button type="button" class="wd-now" title="${esc(`还原于 ${wf.at}；该备份已不在列表中`)}"><i data-ic="map"></i><span>当前存档：从 ${esc(p.time)} 的备份还原${p.label ? `（${esc(p.label)}）` : ""}</span></button>`;
    }
    const og = origin(D);
    if (!og) return "";
    const t = og.kind === "import" ? `当前存档：从「${esc(og.source)}」导入的地图` : "当前存档：已换回导入地图前的存档";
    return `<button type="button" class="wd-now" data-wd-act="tomap" title="${esc(`${og.kind === "import" ? "导入于" : "换回于"} ${og.at}；点击前往「换地图」查看`)}"><i data-ic="map"></i><span>${t}</span></button>`;
  }
  function origin(D) {
    for (const p of D.points) {
      if (p.state === "committed") return { kind: "import", source: p.source, at: p.created };
      if (p.state === "restored") return { kind: "returned", at: p.returned || p.created };
    }
    return null;
  }
  const keepTip = () => `保留最近 ${KEEP} 份，超出后自动删除最旧的备份。份数上限所有服务器共用，可在「设置 → 存储位置」中修改`;
  const countHTML = () => `${dat().backups.length}<small> / ${KEEP}</small>`;
  function schedBtn() {
    const D = dat();
    return D.sched
      ? `<button type="button" class="wd-chip" data-wd-act="sched" title="每天 ${D.sched} 自动备份（标签「定时」）；可在「定时任务」中修改"><i data-ic="clock"></i>每天 ${D.sched}</button>`
      : `<button type="button" class="wd-chip none" data-wd-act="sched" title="尚未设置定时备份；可在「定时任务」中添加"><i data-ic="clock"></i>定时备份</button>`;
  }
  const restoreTip = () => live() ? `将先保存存档并停止服务器，再执行还原；仅替换存档，${keptWhat()}` : `仅替换存档，${keptWhat()}`;
  function tagsHTML(b) {
    const D = dat(), cur = D.wf && D.wf.backup === b.file;
    return (cur ? `<span class="wd-tag acc" title="${esc(`于 ${D.wf.at} 从此备份还原`)}">当前存档</span>` : "")
      + (b.old ? `<button type="button" class="wd-tag" data-wd-act="old" title="${esc(`更改备份位置前创建，位于原文件夹，可正常还原或删除；点击打开文件夹\n${b.old}`)}">位于原文件夹</button>` : "");
  }
  function bkRow(b) {
    return `<div class="wd-row" data-wd-file="${esc(b.file)}"><span class="wd-time" title="${esc(b.file)}">${esc(b.time)}</span>`
      + `<span class="wd-lab${isSys(b.label) ? " sys" : ""}">${esc(b.label)}</span><span class="wd-tags">${tagsHTML(b)}</span><span class="wd-sp"></span>`
      + `<button type="button" class="wd-btn wd-act" data-wd-act="restore" title="${esc(restoreTip())}"><i data-ic="restore"></i>还原</button>`
      + `<button type="button" class="wd-ib wd-act" data-wd-act="del" title="删除"><i data-ic="trash"></i></button>`
      + `<span class="wd-size">${size(b.mb)}</span></div>`;
  }
  function makeBtn(busy) {
    const starting = C.st === "starting";
    const tip = starting ? "服务器正在启动或停止，请稍后再备份" : C.st === "running" ? "服务器运行中也可备份：先暂停自动保存，打包存档后再恢复自动保存" : "";
    return busy ? `<button type="button" class="wd-go" data-wd-act="make" aria-disabled="true"><i data-ic="spinner" class="wd-spin"></i>正在备份</button>`
      : `<button type="button" class="wd-go" data-wd-act="make"${starting ? ' aria-disabled="true"' : ""}${tip ? ` title="${esc(tip)}"` : ""}><i data-ic="camera"></i>立即备份存档</button>`;
  }
  function bkHTML() {
    const D = dat();
    return `<div class="wd-bk"><section class="wd-col">${folders()}`
      + `<div class="wd-hd fdpad"><span class="wd-t">备份</span><span class="wd-n" data-wd-k="n" title="${esc(keepTip())}">${countHTML()}</span><span class="wd-sp"></span>`
      + `<span class="wd-nowslot" data-wd-k="now">${nowLine()}</span>${schedBtn()}</div>`
      + `<div class="wd-lw${D.backups.length ? "" : " is-none"}"><div class="list">${D.backups.map(bkRow).join("")}</div><span class="thumb"></span><div class="wd-none">暂无备份</div></div>`
      + `<div class="wd-mk"><input class="wd-inp" maxlength="20" placeholder="${esc(bkLabelPh())}" aria-label="备份标签（选填）" autocomplete="off" spellcheck="false">`
      + `<span class="wd-goslot">${makeBtn(false)}</span></div></section></div>`;
  }
  // 只换变了的那几处
  const bkList = () => bkRoot()?.querySelector(".wd-lw .list");
  const rowOf = file => bkList() && [...bkList().querySelectorAll(".wd-row:not(.wd-out)")].find(r => r.dataset.wdFile === file);
  function paintHead(dir = 1) {
    const r = bkRoot(); if (!r) return;
    swapKeyed($(".wd-hd", r), `<span class="wd-n" data-wd-k="n" title="${esc(keepTip())}">${countHTML()}</span><span class="wd-nowslot" data-wd-k="now">${nowLine()}</span>`, dir);
    r.querySelector(".wd-lw").classList.toggle("is-none", !dat().backups.length);
  }
  function paintTags(file) {
    const row = rowOf(file), b = dat().backups.find(x => x.file === file); if (!row || !b) return;
    const box = $(".wd-tags", row), html = tagsHTML(b);
    if (box.innerHTML === hy(Object.assign(document.createElement("span"), { innerHTML: html })).innerHTML) return;
    swapItems(box, html, 1);
  }
  function addBackup(b, spare) {
    const D = dat(); D.backups.unshift(b);
    const list = bkList();
    if (list) {
      list.insertAdjacentHTML("afterbegin", bkRow(b)); const row = hy(list.firstElementChild);
      growIn(row); list.scrollTo({ top: 0, behavior: "smooth" });
    }
    trim(spare); paintHead(1); bkRoot()?.querySelector(".wd-lw")._paint?.();
  }
  // 超过保留份数：最旧的往右划掉（程序在做完一份以后按时间删最旧的，「还原前自动」也算数；
  // 还原时正要用的那一份不删，同 inst/backup.go createBackup 的 spare）
  function trim(spare) {
    const D = dat();
    while (D.backups.length > KEEP) {
      let at = D.backups.length - 1; if (D.backups[at].file === spare) at--;
      const [b] = D.backups.splice(at, 1), row = rowOf(b.file);
      if (row) setTimeout(() => dropOut(row, () => bkRoot()?.querySelector(".wd-lw")._paint?.()), 380);
    }
  }
  // 草图里的「现在」：示例数据最新的一份是 11-27 04:00，所以从 11-27 20:30 起走（照真的流逝），不拿这台电脑的钟（早上看草图时新备份会排到 04:00 那份后面）
  const T0 = Date.now(), NOW0 = new Date(2026, 10, 27, 20, 30, 0).getTime();
  const stamp = () => { const d = new Date(NOW0 + Date.now() - T0); return { f: `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`,
    t: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}` }; };
  let making = false;
  function makeBackup() {
    const r = bkRoot(); if (!r || making || C.st === "starting") return;
    const inp = $(".wd-inp", r), label = inp.value.trim().replace(/[\\/:*?"<>|]/g, "");
    making = true; inp.disabled = true;
    const slot = $(".wd-goslot", r); slot.innerHTML = makeBtn(true); hy(slot);
    setTimeout(() => {
      making = false;
      const s = stamp(), D = dat();
      addBackup({ file: `${s.f}${label ? "-" + label : ""}.zip`, mb: D.worldMb + Math.round(Math.random() * 40) / 10, time: s.t, label, old: "" });
      const rr = bkRoot(); if (!rr) return;
      const i2 = $(".wd-inp", rr); i2.disabled = false; i2.value = "";
      const sl = $(".wd-goslot", rr); sl.innerHTML = makeBtn(false); hy(sl);
    }, 1200);
  }
  function askRestore(b) {
    const D = dat(), lv = live(), n = lv ? (C.online || []).length : 0, nm = bkName(b);
    ask({ icon: "restore", title: `还原备份<span class="nw">「${esc(nm)}」？</span>`, ok: lv ? "停止并还原" : "还原存档", w: 400,
      body: `<span class="mono" title="${esc(b.file)}">${b.label ? esc(b.time) + " · " : ""}${size(b.mb)}</span><br>`
        + (C.st === "starting" ? `服务器正在启动，将先停止再还原；还原后不会自动启动。<br>`
          : lv ? `服务器正在运行${n ? `，当前有 <b>${n}</b> 位玩家在线，停止服务器后将断开连接` : ""}：将先保存存档并停止服务器，还原后不会自动启动。<br>` : "")
        + `仅替换存档；还原前会自动备份当前存档，需要时可再次还原。`,
      onOk: () => doRestore(b) });
  }
  function doRestore(b) {
    const row = rowOf(b.file);
    if (row) { row.classList.add("busy"); const btn = $("[data-wd-act=restore]", row); btn.innerHTML = `${icon("spinner", "wd-spin")}正在还原`; btn.setAttribute("aria-disabled", "true"); }
    setTimeout(() => {
      const D = dat(), s = stamp(), prev = D.wf && D.wf.backup;
      // 还原前自动备份现在的世界（inst/backup.go RestoreBackup），再把世界换成这一份、记下 worldFrom
      addBackup({ file: `${s.f}-还原前自动.zip`, mb: D.worldMb, time: s.t, label: "还原前自动", old: "" }, b.file);
      D.wf = { backup: b.file, at: `${s.t}` }; D.worldMb = b.mb;
      if (prev) paintTags(prev);
      paintTags(b.file); paintHead(1);
      const rw = rowOf(b.file);
      if (rw) { rw.classList.remove("busy"); const btn = $("[data-wd-act=restore]", rw); btn.innerHTML = `${icon("restore")}还原`; btn.removeAttribute("aria-disabled"); }
    }, 1400);
  }
  function askDelete(b) {
    ask({ icon: "trash", title: `删除备份<span class="nw">「${esc(bkName(b))}」？</span>`, ok: "删除备份",
      body: `<span class="mono" title="${esc(b.file)}">${b.label ? esc(b.time) + " · " : ""}${size(b.mb)}</span><br>删除后<b class="dg">无法恢复</b>。`,
      onOk: () => {
        const D = dat(); D.backups = D.backups.filter(x => x.file !== b.file);
        const row = rowOf(b.file); if (row) dropOut(row, () => bkRoot()?.querySelector(".wd-lw")._paint?.());
        paintHead(1);
      } });
  }
  function bkClick(e) {
    const a = e.target.closest("[data-wd-act]"); if (!a) return;
    if (a.getAttribute("aria-disabled") === "true") return;
    const act = a.dataset.wdAct, row = a.closest(".wd-row"), b = row && dat().backups.find(x => x.file === row.dataset.wdFile);
    if (act === "make") makeBackup();
    else if (act === "restore" && b && !row.classList.contains("busy")) askRestore(b);
    else if (act === "del" && b && !row.classList.contains("busy")) askDelete(b);
    else if (act === "sched") C.goTab("tasks");
    else if (act === "tomap") document.querySelector('#snav [data-s="map"]')?.click(); // 外壳没给切子页的接口：照外壳自己的做法点左边那一行
    else if (act === "old") a.animate([{ opacity: 1 }, { opacity: .5 }, { opacity: 1 }], { duration: 300 }); // 草图里不真打开文件夹
  }
  window.DETAIL_TABS["world/backups"] = {
    html(ctx) { C = ctx; closeAsk(true); dat(); return bkHTML(); },
    mount(ctx) {
      C = ctx; const r = bkRoot(); if (!r) return;
      ctx.thinBar($(".wd-lw", r));
      r.addEventListener("click", bkClick);
      $(".wd-inp", r).addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); makeBackup(); } });
      if (making) { const sl = $(".wd-goslot", r); sl.innerHTML = makeBtn(true); hy(sl); $(".wd-inp", r).disabled = true; }
    },
  };

  // ================= 换地图 =================
  const mapRoot = () => document.querySelector(".cp > .wd-map:not(.fade-out)");
  const SET_NAME = { difficulty: "难度", gamemode: "默认游戏模式" };
  const SET_VAL = { peaceful: "和平", easy: "简单", normal: "普通", hard: "困难", survival: "生存", creative: "创造", adventure: "冒险", spectator: "旁观" };
  const DELETABLE = new Set(["committed", "rolled-back", "restored"]);
  const verKey = v => { const x = String(v).match(/^(\d+)\.(\d+)(?:\.(\d+))?(?:-snapshot-(\d+))?/); return x ? [+x[1], +x[2], +(x[3] || 0), x[4] != null ? -1000 + +x[4] : 0] : [0, 0, 0, 0]; };
  const cmpVer = (a, b) => { const x = verKey(a), y = verKey(b); for (let i = 0; i < 4; i++) if (x[i] !== y[i]) return x[i] - y[i]; return 0; };
  const cand = () => { const D = dat(); return D.src.cands[D.flow.cand]; };
  const relation = c => !c.ver ? "unknown" : cmpVer(c.ver, C.S.mc) === 0 ? "same" : cmpVer(c.ver, C.S.mc) < 0 ? "older" : "newer";
  const settingsOf = c => Object.keys(SET_NAME).filter(k => c[k === "difficulty" ? "diff" : "mode"] && c[k === "difficulty" ? "diff" : "mode"] !== dat().props[k])
    .map(k => ({ key: k, before: dat().props[k], after: c[k === "difficulty" ? "diff" : "mode"] }));

  // 恢复点一条现在是什么情况：[标签样式, 标签, 一句话]（worldimport.js pointSay，那句话进标签的悬停）
  function pointSay(pt) {
    const D = dat();
    if (pt.state === "committed") {
      const newest = D.points.find(x => x.state === "committed");
      const noOld = "导入前服务器尚无存档，无可换回的原存档";
      if (newest && newest !== pt) return ["", "更早的导入", "此后已导入其他地图。" + (!pt.hasOld ? noOld : "需先换回最近一次导入，才能换回此次之前的存档")];
      if (pt.canReturn) return ["acc", "已导入", "当前正在使用此地图。导入前的存档保留在恢复点中，可随时换回"];
      return ["acc", "已导入", "当前正在使用此地图。" + (pt.hasOld ? "当前无法换回导入前的存档" : noOld)];
    }
    return ({ restored: ["", "已换回", "已换回导入前的存档。曾导入的地图（含后续改动）仍另行保留，确认不再需要后可删除"],
      restoring: ["warn", "正在换回", "正在换回导入前的存档"], "rolled-back": ["", "没有生效", "此次导入未生效，服务器仍使用原存档"] })[pt.state]
      || ["", "状态不明", "无法读取此次导入的状态"];
  }
  function ptRow(pt) {
    const [cls, tag, say] = pointSay(pt), why = live() ? (pt.canReturn ? "请先停止服务器后再换回或删除" : "请先停止服务器后再删除恢复点") : "";
    const dis = why ? ' aria-disabled="true"' : "", busy = pt.state === "restoring";
    const sizes = [pt.oldMb > 0 ? `原存档 ${size(pt.oldMb)}` : "", pt.keptMb > 0 ? `已保留地图 ${size(pt.keptMb)}` : ""].filter(Boolean).join(" · ");
    return `<div class="wd-pt" data-wd-pt="${pt.id}"><div class="wd-pn"><span class="nm">${esc(pt.name)}</span><span class="wd-tag ${cls}" data-wd-k="tag" title="${esc(say)}">${tag}</span><span class="wd-sp"></span>`
      + `<span class="wd-pa" data-wd-k="acts">${busy ? `<span class="wd-busy">${icon("spinner", "wd-spin")}</span>`
        : (pt.canReturn ? `<button type="button" class="wd-btn" data-wd-act="ret"${dis} title="${esc(why || "换回此次导入前的存档；当前地图（含导入后的所有改动）会另行保留，不会删除")}"><i data-ic="restore"></i>换回导入前的存档</button>` : "")
        + (DELETABLE.has(pt.state) ? `<button type="button" class="wd-ib" data-wd-act="pdel"${dis} title="${esc(why || "删除恢复点")}"><i data-ic="trash"></i></button>` : "")}</span></div>`
      + `<div class="wd-ps" data-wd-k="sub">${esc(pt.created)} 导入 · ${m(pt.source)}${sizes ? " · " + sizes : ""}</div></div>`;
  }
  // ---- 打开时那页：左「替换当前地图」、右「恢复点」 ----
  const dzTip = () => `支持冒险、空岛、跑酷地图，以及单人存档或旧服存档。压缩包内嵌套多层文件夹、附带说明书或资源包时均可识别；含多张地图时可从中选择。\n原存档不会删除，将完整保留在恢复点中随时换回；核心、版本${addonList() ? "、" + addonList() : ""}和各项设置均保持不变`;
  function dzInner() {
    const F = dat().flow;
    if (F.phase === "up") return `<span class="wd-dzi"><i data-ic="arrowUp"></i></span><b class="mono">${esc(F.fileName)}</b>`
      + `<div class="wd-upr"><div class="wd-upbar">${"<i></i>".repeat(30)}</div><button type="button" class="wd-ib" data-wd-act="upx" title="取消上传"><i data-ic="x"></i></button></div>`
      + `<span class="wd-dzs"><span class="num wd-uptxt">${upText()}</span></span>`;
    if (F.phase === "scan") return `<span class="wd-dzi"><i data-ic="spinner" class="wd-spin"></i></span><b>正在识别…</b><span class="wd-dzs">${m(F.fromPath ? F.path : F.fileName)}</span>`;
    if (F.phase === "error") return `<span class="wd-dzi bad"><i data-ic="warn"></i></span><b>此文件为整合包，无法作为地图导入</b><span class="wd-dzs bad">未检测到可用存档，请更换来源</span>`;
    return `<span class="wd-dzi"><i data-ic="map"></i></span><b>拖入地图压缩包，或点击选择</b><span class="wd-dzs">.zip · 最大 8 GB</span>`;
  }
  const upText = () => { const F = dat().flow, mb = dat().src.mb; return `已上传 ${size(mb * F.pct / 100)} / ${size(mb)} · ${Math.floor(F.pct)}%`; };
  function homeHTML() {
    const D = dat(), F = D.flow, busy = F.phase === "up" || F.phase === "scan";
    return `<div class="wd-home"><section class="wd-col wd-imp"><div class="wd-hd"><span class="wd-t">替换当前存档</span>`
      + `<span class="wd-lv" title="${esc(`当前使用的存档文件夹 ${D.level}（对应 server.properties 中的 level-name）`)}">${esc(D.level)}</span></div>`
      + `<div class="wd-dz${busy ? " busy" : ""}${F.phase === "error" ? " bad" : ""}" data-wd-act="pick" tabindex="0" role="button" aria-label="拖入地图压缩包，或点击选择"${busy ? "" : ` title="${esc(dzTip())}"`}><div class="wd-dzc">${dzInner()}</div></div>`
      + `<label class="wd-prow"><input class="wd-path" spellcheck="false" autocomplete="off" value="${esc(F.fromPath ? F.path : "")}" aria-label="地图文件夹或 ZIP 文件的路径" placeholder="或输入文件夹 / ZIP 路径，如 D:\\Maps\\SkyCity" title="本机上的地图存档文件夹（含 level.dat 或其上级文件夹），或未解压的 ZIP 压缩包">`
      + `<button type="button" class="wd-ib" data-wd-act="browse" title="选择地图文件夹（含 level.dat 的存档文件夹或其上级文件夹）"><i data-ic="folder"></i></button>`
      + `<button type="button" class="wd-ib" data-wd-act="scan" title="检测"><i data-ic="search"></i></button></label></section>`
      + `<section class="wd-col wd-pts">${folders()}<div class="wd-hd fdpad"><span class="wd-t" title="导入地图时，原存档将完整保留于此，可随时换回。恢复点为原样留存的存档文件夹，独立于普通备份之外，不受备份保留份数限制，直至手动删除">恢复点</span>`
      + `<span class="wd-n" data-wd-k="pn">${D.points.length || ""}</span></div>`
      + `<div class="wd-lw${D.points.length ? "" : " is-none"}"><div class="list">${D.points.map(ptRow).join("")}</div><span class="thumb"></span><div class="wd-none">暂无恢复点</div></div></section></div>`;
  }
  // ---- 选了来源以后的三页：选择地图、核对、进度 ----
  function cover() {
    const D = dat(), F = D.flow, S = D.src, c = cand();
    const name = F.screen === "pick" ? (F.fromPath ? esc(F.path.split("\\").pop()) : m(F.fileName)) : esc(c.name);
    const sub = F.screen === "pick" ? (F.fromPath ? `文件夹 · ${m(F.path)}` : `压缩包 · ${size(S.mb)}`) : `${m(c.root)} · ${size(c.mb)} · 来自 ${F.fromPath ? m(F.path) : m(F.fileName)}`;
    return `<div class="wd-cov" style="--c:${MDPixel16.CORE_COLORS.vanilla}"><span class="wd-cava">${MDPixel16.svg("vanilla", 32)}</span>`
      + `<div class="wd-ct"><div class="wd-cn" data-wd-k="cn">${name}</div><div class="wd-cs" data-wd-k="cs">${sub}</div></div></div>`;
  }
  const footHTML = (left, right) => `<div class="wd-foot"><span class="wd-fl">${left}</span><span class="wd-sp"></span><span class="wd-fr">${right}</span></div>`;
  const backBtn = (act = "back") => `<button type="button" class="wd-nb back" data-wd-act="${act}"><i data-ic="chevronRight"></i>上一步</button>`;
  function candRow(c, i) {
    const F = dat().flow;
    return `<div class="wd-cand${i === F.cand ? " on" : ""}${c.block ? " off" : ""}" data-wd-cand="${i}"${c.block ? ` title="${esc(c.blockFull)}"` : ""}>`
      + `<span class="wd-ci"><i data-ic="map"></i></span><div class="wd-cm"><div class="n">${esc(c.name)}</div><div class="s">${m(c.root)} · ${size(c.mb)}</div></div>`
      + (c.block ? `<span class="wd-tag bad">无法导入</span>` : "") + `</div>`;
  }
  // 右栏：一张固定的小表（每张地图都是这几行，没有的写「没有」），换地图时只换变了的那几格（同 import.html 的 mapDetInner）
  function factsHTML(c) {
    const dims = c.dims.map(([n, r]) => `<span title="${r} 个区域文件">${n}</span>`).join(" · ")
      + (c.missing.length ? `<span class="f" title="地图未包含${c.missing.join("、")}（并非导入遗漏），服务器首次加载时将自动生成">${c.missing.join("、")}将自动生成</span>` : "");
      const dd = (k, v) => `<dd data-wd-k="${k}">${v}</dd>`, no = `<span class="f">无</span>`;
    const clue = c.clues.length ? `${c.clues.slice(0, 3).map(m).join("、")}${c.clues.length > 3 ? `<span class="f">等 ${c.clues.length} 个</span>` : ""}`
      : `<span title="未检测到服务端或模组线索，无法确认是否为纯原版">无（按原版处理）</span>`;
    const notes = (c.notes || []).map(([t, s, full]) => `<div class="wd-nt ${t}" title="${esc(full)}"><i data-ic="${t === "warn" ? "warn" : "info"}"></i><span>${esc(s)}</span></div>`).join("");
    return `<dl class="wd-facts"><dt>保存于</dt>${dd("ver", m(c.ver || "未记录"))}<dt>布局</dt>${dd("lay", `<span title="${esc(c.layoutFull)}">${c.layout}</span>`)}`
      + `<dt>维度</dt>${dd("dim", dims)}<dt>数据包</dt>${dd("dp", c.dps.length ? c.dps.map(m).join("、") : no)}`
      + `<dt>资源包</dt>${dd("rp", c.rp ? `${m("resources.zip")}<span class="f">不会自动下发给玩家</span>` : no)}`
      + `<dt>模组线索</dt>${dd("clue", clue)}<dt>玩家进度</dt>${dd("pl", c.players ? `${c.players} 份玩家数据` : no)}`
      + `<dt>说明书</dt>${dd("rd", c.readme ? `${m(c.readme)}<button type="button" class="wd-ib sm" data-wd-act="readme" title="查看说明书（仅以纯文本显示，请勿随意访问其中的链接）"><i data-ic="doc"></i></button>` : no)}`
      + `<dt>不会导入</dt>${dd("hint", c.hints.length ? `<span title="与地图位于同一文件夹的附带文件，不会导入">${c.hints.map(m).join("、")}</span>` : no)}</dl>`
      + `<div class="wd-notes" data-wd-k="notes">${notes}</div>`;
  }
  function pickHTML() {
    const D = dat(), S = D.src;
    return `<div class="wd-flow">${cover()}<div class="wd-scols">`
      + `<section class="wd-col"><div class="wd-hd"><span class="wd-t">地图</span><span class="wd-n">${S.cands.length}</span></div>`
      + `<div class="wd-lw"><div class="list"><i class="wd-hl"></i>${S.cands.map(candRow).join("")}</div><span class="thumb"></span></div></section>`
      + `<section class="wd-col wd-detc"><div class="wd-hd"><span class="wd-t">地图内容</span></div><div class="wd-det">${factsHTML(cand())}</div></section></div>`
      + footHTML(backBtn(), `<button type="button" class="wd-nb pri" data-wd-act="next">下一步<i data-ic="chevronRight"></i></button>`) + `</div>`;
  }
  // 核对（程序 planHTML 的 replace 那一种）
  const REL = {
    same: ["ok", "版本一致", "版本一致，可直接导入"],
    older: ["warn", "将自动升级", null],
    newer: ["bad", "无法读取", null],
    unknown: ["warn", "无法判断", "无法判断地图与服务器的版本高低（地图未记录版本或版本不在列表中）"],
  };
  const needAck = rel => rel === "older" || rel === "unknown";
  function ackText(c, rel) {
    const mc = C.S.mc;
    if (rel === "unknown") return ["地图未记录版本，无法判断是否兼容", REL.unknown[2]];
    return [`地图将升级至 ${mc}，升级后无法再以旧版本打开`, `该地图最后保存于 ${c.ver}，低于服务器的 ${mc}。首次启动时游戏会将其升级至 ${mc}，之后无法再用于旧版本；导入前的存档将保留在恢复点中，不受影响。旧版地图的机关、命令与数据包在新版本中可能失效`];
  }
  function checkHTML() {
    const D = dat(), F = D.flow, c = cand(), rel = relation(c), [rc, rt] = REL[rel], sets = rel === "newer" ? [] : settingsOf(c), S = C.S; // 读不了的地图不列可改的设置
    const relTip = rel === "newer" ? `地图版本高于服务器，${S.mc} 无法读取，无法导入。可新建一台 ${c.ver} 的服务器使用此地图` : rel === "older" ? ackText(c, rel)[1] : REL[rel][2];
    const left = `<section class="wd-col wd-chk"><div class="wd-hd"><span class="wd-t">核对</span></div>`
      + `<div class="wd-rel"><div class="wd-rv"><span class="k">地图保存于</span><span class="v mono">${esc(c.ver || "未记录")}</span></div><i data-ic="arrowRight"></i>`
      + `<div class="wd-rv"><span class="k">当前服务器</span><span class="v"><span class="mono">${esc(S.label)} ${esc(S.mc)}</span></span></div><span class="wd-sp"></span>`
      + `<span class="wd-tag ${rc}" title="${esc(relTip)}">${rt}</span></div>`
      + `<dl class="wd-facts"><dt>替换</dt><dd><span>${D.worlds.map(m).join("、")}</span><span class="f" title="将完整移入恢复点而不删除，后续可在「恢复点」中换回">移入恢复点</span></dd>`
      + `<dt>保留</dt><dd>核心和版本、Java、内存、端口、${addonList() ? addonList() + "、" : ""}白名单和管理员、其他设置</dd>`
      + `<dt>空间</dt><dd>需要约 ${size(c.mb * 1.08)}<span class="f">D 盘剩余 214 GB</span></dd></dl>`
      + `<div class="wd-notes">${(c.notes || []).map(([t, s, full]) => `<div class="wd-nt ${t}" title="${esc(full)}"><i data-ic="${t === "warn" ? "warn" : "info"}"></i><span>${esc(s)}</span></div>`).join("")}</div></section>`;
    const right = sets.length ? `<section class="wd-col wd-setc"><div class="wd-hd"><span class="wd-t" title="以下设置项与地图记录不同。默认保持不变；开启后将在导入时同步修改，换回原存档时一并恢复">可同步修改的设置</span></div>`
      + `<div class="wd-sets">${sets.map(s => `<button type="button" class="wd-set${F.sets[s.key] ? " on" : ""}" data-wd-set="${s.key}" role="switch" aria-checked="${!!F.sets[s.key]}">`
        + `<span class="wd-sw"></span><span class="nm">${SET_NAME[s.key]}</span><span class="wd-sp"></span><span class="v">${SET_VAL[s.before] || s.before}${icon("arrowRight")}<b>${SET_VAL[s.after] || s.after}</b></span></button>`).join("")}</div></section>` : "";
    // 底下一条：要确认的、要停服的；地图比服务器新时换成一句红字（不能往下走）
    let strip = "";
    if (rel === "newer") strip = `<div class="wd-strip"><span class="wd-sbad" title="${esc(relTip)}"><i data-ic="warn"></i>地图版本高于服务器，${esc(S.mc)} 无法读取，无法导入</span></div>`;
    else if (needAck(rel) || live()) {
      const [s, full] = needAck(rel) ? ackText(c, rel) : ["", ""];
      strip = `<div class="wd-strip">` + (needAck(rel) ? `<button type="button" class="wd-tg${F.ack ? " on" : ""}" data-wd-tg="ack" role="switch" aria-checked="${F.ack}" title="${esc(full)}"><span class="wd-sw"></span><span class="t">我已了解：<b>${esc(s)}</b></span></button>` : "")
        + (live() ? `<button type="button" class="wd-tg${F.stop ? " on" : ""}" data-wd-tg="stop" role="switch" aria-checked="${F.stop}" title="服务器正在运行。开启后，导入前会先通知服务器保存并正常退出（不强制终止）；若超时未能停止则中止导入，不会修改任何文件"><span class="wd-sw"></span><span class="t">停止并导入</span></button>` : "") + `</div>`;
    }
    const go = rel === "newer" ? `<button type="button" class="wd-nb pri" aria-disabled="true" title="${esc(relTip)}"><i data-ic="download"></i>导入并替换当前存档</button>`
      : `<button type="button" class="wd-nb pri" data-wd-act="go"><i data-ic="download"></i>导入并替换当前存档</button>`;
    return `<div class="wd-flow">${cover()}<div class="wd-scols chk${sets.length ? "" : " one"}">${left}${right}</div>${strip}${footHTML(backBtn(), go)}</div>`;
  }
  // 进度：五步（internal/worldimport/service.go importSteps 的原话），复制那一步有 30 格的经验条
  const STEPS = ["核对地图和服务器", "复制地图到暂存区", "停止并锁定服务器", "换上新地图", "完成"];
  function stepIco(i) {
    const T = dat().flow.task;
    if (T.cancelled && i >= T.step && i < 4) return i === T.step ? `<i class="wd-dash"></i>` : `<i class="wd-px"></i>`;
    if (i < T.step || (T.ended && !T.cancelled)) return icon("check");
    if (i === T.step && !T.ended) return icon("spinner", "wd-spin");
    return `<i class="wd-px"></i>`;
  }
  const stepCls = i => { const T = dat().flow.task; return i < T.step || (T.ended && !T.cancelled) ? "done" : i === T.step && !T.ended ? "run" : T.cancelled && i === T.step ? "cx" : ""; };
  function taskHTML() {
    const D = dat(), T = D.flow.task, c = cand();
    return `<div class="wd-flow">${cover()}<section class="wd-col wd-task"><div class="wd-steps">${STEPS.map((s, i) => `<div class="wd-step ${stepCls(i)}" data-wd-step="${i}"><span class="ic">${stepIco(i)}</span><span>${s}</span></div>`).join("")}</div>`
      + `<div class="wd-prog"><div class="wd-ph"><span class="mono wd-pt2">${size(c.mb * T.pct / 100)} / ${size(c.mb)}</span><span class="wd-sp"></span><span class="mono wd-pct">${Math.floor(T.pct)}%</span></div>`
      + `<div class="wd-xp${T.step > 1 ? " done" : ""}">${Array.from({ length: 30 }, (_, i) => `<i${i < Math.floor(T.pct / 100 * 30 + 1e-6) ? ' class="on"' : ""}></i>`).join("")}</div>`
      + `<div class="wd-lw wd-log"><div class="list">${T.log.map(l => `<div>${esc(l)}</div>`).join("")}</div><span class="thumb"></span></div>`
      + `<div class="wd-msg" data-wd-k="msg">${taskMsg()}</div></div></section>${footHTML(taskLeft(), taskRight())}</div>`;
  }
  const taskMsg = () => { const T = dat().flow.task; return T.cancelled ? `<span class="wd-mi2">${icon("info")}已取消换地图：未替换任何文件，原存档未变动。</span>` : T.ended ? `<span class="wd-ok">${icon("check")}地图已替换完成。</span>` : ""; };
  function taskLeft() {
    const T = dat().flow.task;
    if (T.cancelled) return `<button type="button" class="wd-nb" data-wd-act="again"><i data-ic="refresh"></i>重新核对地图</button>`;
    if (T.ended) return "";
    const can = T.step <= 1;
    return `<button type="button" class="wd-nb" data-wd-act="cancel"${can ? ` title="地图正在复制，原存档尚未变动"` : ` aria-disabled="true" title="已开始替换存档，无法取消；若出错将自动退回原存档"`}>取消导入</button>`;
  }
  const taskRight = () => { const T = dat().flow.task; return T.ended ? `<button type="button" class="wd-nb pri" data-wd-act="done">${T.cancelled ? "返回换地图" : `${icon("check")}完成`}</button>` : ""; };
  const mapInner = () => { const F = dat().flow; return F.screen === "pick" ? pickHTML() : F.screen === "check" ? checkHTML() : F.screen === "task" ? taskHTML() : homeHTML(); };
  function mapAfter(r) {
    $$(".wd-lw", r).forEach(w => C.thinBar(w));
    fitWdTitles(r);
    document.fonts?.ready.then(() => r.isConnected && fitWdTitles(r));
    placeHL(false);
    if (dat().flow.screen === "pick" && dat().flow.readme) openReadme(false);
  }
  // 换步：整页往走的方向挪 16 像素淡掉（0.14 秒），新的一步从另一边挪进来（0.26 秒）（规范第七节）
  let stepping = false; // 换步那一下（0.14 + 0.26 秒）里不收点击，免得连点两下照着新状态又走一步（同向导 goStep）
  function stepMap(dir) {
    const r = mapRoot(); if (!r) { stepping = false; return; }
    stepping = true;
    const tok = r._tok = (r._tok || 0) + 1;
    r.getAnimations().forEach(a => a.cancel());
    const out = r.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translateX(${-16 * dir}px)` }], { duration: 140, easing: EASE_IN, fill: "forwards" });
    out.onfinish = () => {
      if (r._tok !== tok) return;
      r.innerHTML = mapInner(); hy(r); mapAfter(r); out.cancel();
      r.animate([{ opacity: 0, transform: `translateX(${16 * dir}px)` }, { opacity: 1, transform: "none" }], { duration: 260, easing: EASE_OUT })
        .onfinish = () => { if (r._tok === tok) stepping = false; };
      const nw = dat().flow.justAdded; if (nw) { dat().flow.justAdded = ""; const row = $(`[data-wd-pt="${nw}"]`, r); if (row) growIn(row, 180); }
    };
  }
  // 候选地图的选中框：单独一块，滑过去（同向导核心、版本那两列）
  function placeHL(animate) {
    const r = mapRoot(); if (!r) return;
    const hl = $(".wd-hl", r), on = $(".wd-cand.on", r); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateY(${on.offsetTop}px)`; hl.style.height = on.offsetHeight + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  // ---- 选来源 ----
  function paintDz() {
    const r = mapRoot(); if (!r || dat().flow.screen !== "home") return;
    const F = dat().flow, dz = $(".wd-dz", r);
    const busy = F.phase === "up" || F.phase === "scan";
    dz.classList.toggle("busy", busy); dz.classList.toggle("bad", F.phase === "error");
    delete dz.dataset.tip; if (busy) dz.removeAttribute("title"); else dz.setAttribute("title", dzTip()); // 正在传、正在识别时不出那段说明
    swapItems($(".wd-dzc", r), dzInner(), 1);
  }
  function paintUp() {
    const r = mapRoot(); if (!r) return;
    const F = dat().flow, n = Math.floor(F.pct / 100 * 30 + 1e-6);
    $$(".wd-upbar i", r).forEach((c, i) => c.classList.toggle("on", i < n));
    const t = $(".wd-uptxt", r); if (t) t.textContent = upText();
  }
  function startUpload(name) {
    const D = dat(), F = D.flow; if (F.phase === "up" || F.phase === "scan") return;
    const tok = ++F.tok;
    Object.assign(F, { phase: "up", pct: 0, fileName: name || D.src.file, fromPath: false, path: "" });
    const inp = mapRoot() && $(".wd-path", mapRoot()); if (inp) inp.value = "";
    paintDz();
    const tick = () => {
      if (F.tok !== tok || F.phase !== "up") return;
      F.pct = Math.min(100, F.pct + 2.6 + Math.random() * 2.2); paintUp();
      if (F.pct < 100) setTimeout(tick, 70); else setTimeout(() => { if (F.tok === tok) scanThen(tok); }, 250);
    };
    setTimeout(tick, 320);
  }
  function startScan(path) {
    const F = dat().flow; if (F.phase === "up" || F.phase === "scan") return;
    const r = mapRoot(), row = r && $(".wd-prow", r);
    if (!path) { if (row) { row.classList.add("bad"); shake(row); $(".wd-path", r).focus(); } return; }
    if (/mrpack|整合包|modpack/i.test(path)) { F.phase = "error"; F.path = path; paintDz(); return; }
    F.tok++; Object.assign(F, { fromPath: true, path }); scanThen(F.tok);
  }
  function scanThen(tok) {
    const F = dat().flow; F.phase = "scan"; paintDz();
    setTimeout(() => {
      if (F.tok !== tok || F.phase !== "scan") return;
      F.phase = "empty"; F.screen = "pick"; F.cand = dat().src.cands.findIndex(c => !c.block); F.readme = false;
      stepMap(1);
    }, 900);
  }
  // ---- 说明书：点了在右栏里铺开（只当纯文字，同 import.html） ----
  function openReadme(animate = true) {
    const r = mapRoot(), col = r && $(".wd-detc", r); if (!col || $(".wd-rdm", col)) return;
    dat().flow.readme = true;
    col.insertAdjacentHTML("beforeend", `<div class="wd-rdm"><div class="wd-rh"><span class="mono">${esc(cand().readme)}</span><span class="wd-sp"></span><span class="f">地图作者附带说明，仅以纯文本显示</span>`
      + `<button type="button" class="wd-ib" data-wd-act="rdclose" title="收起"><i data-ic="x"></i></button></div><div class="wd-lw"><div class="list"><pre>${esc(README)}</pre></div><span class="thumb"></span></div></div>`);
    const p = hy($(".wd-rdm", col)); C.thinBar($(".wd-lw", p));
    if (animate) p.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
  }
  function closeReadme() {
    const p = mapRoot() && $(".wd-rdm", mapRoot()); dat().flow.readme = false; if (!p) return;
    p.animate([{ opacity: 1 }, { opacity: 0, transform: "translateY(5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = () => p.remove();
  }
  function pickCand(i) {
    const D = dat(), F = D.flow; if (i === F.cand || D.src.cands[i].block) return;
    const dir = i > F.cand ? 1 : -1; F.cand = i; F.ack = false; F.sets = {};
    const r = mapRoot(); $$(".wd-cand", r).forEach(x => x.classList.toggle("on", +x.dataset.wdCand === i)); placeHL(true);
    if (F.readme) closeReadme();
    swapKeyed($(".wd-det", r), factsHTML(cand()), dir);
  }
  // ---- 导入 ----
  function tryGo() {
    const D = dat(), F = D.flow, c = cand(), rel = relation(c), r = mapRoot();
    const miss = [needAck(rel) && !F.ack ? "ack" : "", live() && !F.stop ? "stop" : ""].filter(Boolean);
    if (miss.length) { // 不开不让往下走：那一条描红，开关晃一下（同向导的 EULA、import.html 的「核对地图」）
      const st = $(".wd-strip", r); st?.classList.add("bad");
      miss.forEach(k => { const t = $(`[data-wd-tg="${k}"]`, r); t?.classList.add("bad"); shake(t); });
      return;
    }
    const sets = settingsOf(c).filter(s => F.sets[s.key]);
    ask({ icon: "download", w: 420, title: `用<span class="nw">「${esc(c.name)}」</span>替换当前存档？`, ok: live() ? "停止并替换" : "替换存档",
      body: `当前存档（${D.worlds.map(m).join("、")}）将完整移入恢复点，后续可在本页换回。`
        + (sets.length ? `<br>同步修改设置：${sets.map(s => `${SET_NAME[s.key]}改为${SET_VAL[s.after]}`).join("、")}。` : "")
        + (live() ? `<br>将先通知服务器保存并正常停止（不强制终止）。` : "")
        + `<br>${keptWhat().replace("和设置保持不变", "和其他设置不变")}。`,
      onOk: startTask });
  }
  function startTask() {
    const D = dat(), F = D.flow, c = cand();
    F.task = { step: 0, pct: 0, ended: false, cancelled: false, log: [`开始导入：${c.name}（${F.fromPath ? F.path : F.fileName}）`] };
    F.screen = "task"; stepMap(1);
    const tok = ++F.tok, T = F.task;
    const at = (ms, fn) => setTimeout(() => { if (F.tok === tok && !T.cancelled) fn(); }, ms);
    at(900, () => { T.step = 1; T.log.push("核对通过：地图与当前服务器兼容"); paintTask(); copy(); });
    const copy = () => {
      if (F.tok !== tok || T.cancelled) return;
      T.pct = Math.min(100, T.pct + 3 + Math.random() * 3); paintTask(true);
      if (T.pct < 100) { setTimeout(copy, 90); return; }
      T.log.push(`已复制 ${size(c.mb)} 到暂存区`);
      T.step = 2; paintTask();
      at(900, () => { T.log.push(live() ? "服务器已保存并退出，已锁定当前服务器" : "已锁定当前服务器，暂停启动"); T.step = 3; paintTask(); });
      at(2100, () => { T.log.push(`原存档已移入恢复点：${D.worlds.join("、")}`); T.log.push("新地图已替换完成"); T.step = 4; paintTask(); });
      at(2700, () => { T.ended = true; paintTask(); });
    };
  }
  function paintTask(onlyBar) {
    const r = mapRoot(), T = dat().flow.task; if (!r || !T || dat().flow.screen !== "task") return;
    const n = Math.floor(T.pct / 100 * 30 + 1e-6);
    $$(".wd-xp i", r).forEach((c, i) => c.classList.toggle("on", i < n));
    $(".wd-pct", r).textContent = Math.floor(T.pct) + "%";
    $(".wd-pt2", r).textContent = `${size(cand().mb * T.pct / 100)} / ${size(cand().mb)}`;
    if (onlyBar) return;
    $(".wd-xp", r).classList.toggle("done", T.step > 1); // 复制完了：经验条淡下去，步骤那边已经打了勾
    $$(".wd-step", r).forEach((s, i) => { const cls = `wd-step ${stepCls(i)}`; if (s.className.trim() !== cls.trim()) { s.className = cls; $(".ic", s).innerHTML = stepIco(i); } });
    const list = $(".wd-log .list", r), have = list.children.length;
    T.log.slice(have).forEach(l => { const d = document.createElement("div"); d.textContent = l; list.appendChild(d); d.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT }); });
    list.scrollTop = list.scrollHeight; $(".wd-log", r)._paint?.();
    swapKeyed($(".wd-msg", r).parentElement, `<div class="wd-msg" data-wd-k="msg">${taskMsg()}</div>`, 1);
    const fl = $(".wd-fl", r), fr = $(".wd-fr", r);
    if (fl.innerHTML !== hy(Object.assign(document.createElement("span"), { innerHTML: taskLeft() })).innerHTML) swapItems(fl, taskLeft(), 1);
    if (fr.innerHTML !== hy(Object.assign(document.createElement("span"), { innerHTML: taskRight() })).innerHTML) swapItems(fr, taskRight(), 1);
  }
  function finishTask() {
    const D = dat(), F = D.flow, T = F.task, c = cand(); if (!T) return;
    if (!T.cancelled) {
      // 新的一条恢复点排最前；以前那条「已导入」就成了「更早的导入」，不能再直接换回（worldimport canReturn 只给最近那一次）
      D.points.forEach(p => { p.canReturn = false; });
      const s = stamp(), id = "p" + Date.now();
      D.points.unshift({ id, name: c.name, state: "committed", created: s.t, source: F.fromPath ? F.path.split("\\").pop() : F.fileName, oldMb: D.worldMb, hasOld: true, canReturn: true });
      D.wf = null; D.worldMb = c.mb; // 换地图会清掉「从备份还原的」那条记录（app.js 注释：worldFrom 换地图时清掉）
      F.justAdded = id;
    }
    Object.assign(F, freshFlow(), { justAdded: F.justAdded || "" });
    stepMap(-1);
  }
  function askReturn(pt) {
    ask({ icon: "restore", w: 400, title: `换回<span class="nw">「${esc(pt.name)}」</span>导入前的存档？`, ok: "换回导入前的存档",
      body: `当前地图（含导入后的所有改动）会另行保留，不会删除。`,
      onOk: () => {
        const D = dat(); pt.state = "restoring"; paintPoint(pt);
        setTimeout(() => {
          pt.state = "restored"; pt.canReturn = false; pt.keptMb = D.worldMb; pt.returned = stamp().t; D.worldMb = pt.oldMb || D.worldMb; pt.oldMb = 0; D.wf = null;
          paintPoint(pt);
        }, 1600);
      } });
  }
  function askDelPoint(pt) {
    const what = pt.state === "committed" ? (pt.hasOld ? `将删除 ${esc(pt.created)} 导入「${esc(pt.name)}」前的原存档${pt.oldMb ? `（${size(pt.oldMb)}）` : ""}，删除后将无法换回。` : "导入前服务器尚无存档，仅删除此次导入记录。")
      : pt.state === "restored" ? `将删除换回原存档时保留的导入地图${pt.keptMb ? `（${size(pt.keptMb)}）` : ""}及此次导入记录。` : "此次导入未生效，将删除相关记录与临时文件。";
    ask({ icon: "trash", w: 400, title: `删除恢复点<span class="nw">「${esc(pt.name)}」？</span>`, ok: "删除恢复点",
      body: `${what}<br>删除后<b class="dg">无法恢复</b>，当前使用的存档不受影响。`,
      onOk: () => {
        const D = dat(); D.points = D.points.filter(x => x !== pt);
        const r = mapRoot(), row = r && $(`[data-wd-pt="${pt.id}"]`, r);
        if (row) dropOut(row, () => { $(".wd-pts .wd-lw", r)?._paint?.(); $(".wd-pts .wd-lw", r)?.classList.toggle("is-none", !D.points.length); });
        if (r) swapKeyed($(".wd-pts .wd-hd", r), `<span class="wd-n" data-wd-k="pn">${D.points.length || ""}</span>`, 1);
      } });
  }
  function paintPoint(pt) { const r = mapRoot(), row = r && $(`[data-wd-pt="${pt.id}"]`, r); if (row) swapKeyed(row, ptRow(pt), 1); }

  function mapClick(e) {
    if (stepping) return;
    const D = dat(), F = D.flow;
    const cd = e.target.closest("[data-wd-cand]"); if (cd) { pickCand(+cd.dataset.wdCand); return; }
    const st = e.target.closest("[data-wd-set]");
    if (st) { const k = st.dataset.wdSet; F.sets[k] = !F.sets[k]; st.classList.toggle("on", F.sets[k]); st.setAttribute("aria-checked", F.sets[k]); return; }
    const tg = e.target.closest("[data-wd-tg]");
    if (tg) {
      const k = tg.dataset.wdTg; F[k] = !F[k]; tg.classList.toggle("on", F[k]); tg.setAttribute("aria-checked", F[k]);
      if (F[k]) { tg.classList.remove("bad"); const s = tg.closest(".wd-strip"); if (s && !$(".wd-tg.bad", s)) s.classList.remove("bad"); }
      return;
    }
    const a = e.target.closest("[data-wd-act]"); if (!a || a.getAttribute("aria-disabled") === "true") return;
    const act = a.dataset.wdAct, pt = a.closest("[data-wd-pt]") && D.points.find(x => x.id === a.closest("[data-wd-pt]").dataset.wdPt);
    if (act === "upx") { e.stopPropagation(); F.tok++; F.phase = "empty"; F.pct = 0; paintDz(); return; }
    if (act === "pick") { if (F.phase === "empty" || F.phase === "error") startUpload(); return; } // 草图里不真弹选文件的窗口：当作选了示例压缩包
    if (act === "browse") { const inp = $(".wd-path", mapRoot()); inp.value = D.src.path; $(".wd-prow", mapRoot()).classList.remove("bad"); startScan(D.src.path); return; }
    if (act === "scan") { startScan($(".wd-path", mapRoot()).value.trim()); return; }
    if (act === "ret" && pt) { askReturn(pt); return; }
    if (act === "pdel" && pt) { askDelPoint(pt); return; }
    if (act === "readme") { openReadme(); return; }
    if (act === "rdclose") { closeReadme(); return; }
    if (act === "back") { if (F.screen === "check") { F.screen = "pick"; F.ack = false; F.stop = false; } else { Object.assign(F, freshFlow()); } stepMap(-1); return; }
    if (act === "next") { F.screen = "check"; F.ack = false; F.stop = false; F.sets = {}; F.readme = false; stepMap(1); return; }
    if (act === "go") { tryGo(); return; }
    if (act === "cancel") { const T = F.task; if (T && !T.ended && T.step <= 1) { T.cancelled = true; T.ended = true; F.tok++; paintTask(); } return; } // 取消的结果写在下面那句里，过程记录不再重复
    if (act === "again") { F.task = null; F.screen = "check"; F.ack = false; F.stop = false; stepMap(-1); return; }
    if (act === "done") { finishTask(); return; }
  }
  window.DETAIL_TABS["world/map"] = {
    html(ctx) { C = ctx; closeAsk(true); stepping = false; dat(); return `<div class="wd-map">${mapInner()}</div>`; },
    mount(ctx) {
      C = ctx; const r = mapRoot(); if (!r) return;
      mapAfter(r);
      r.addEventListener("click", mapClick);
      r.addEventListener("keydown", e => {
        if (e.target.classList?.contains("wd-path") && e.key === "Enter") { e.preventDefault(); if (!stepping) startScan(e.target.value.trim()); }
        if (e.target.classList?.contains("wd-dz") && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); e.target.click(); }
      });
      r.addEventListener("input", e => { if (e.target.classList?.contains("wd-path")) $(".wd-prow", r).classList.remove("bad"); });
      // 拖文件进来：经过时虚线框换强调色，放下就当选了那个文件
      r.addEventListener("dragover", e => { const dz = e.target.closest?.(".wd-dz"); if (!dz || dat().flow.phase === "up" || dat().flow.phase === "scan") return; e.preventDefault(); dz.classList.add("over"); });
      r.addEventListener("dragleave", e => { const dz = e.target.closest?.(".wd-dz"); if (dz && !dz.contains(e.relatedTarget)) dz.classList.remove("over"); });
      r.addEventListener("drop", e => { const dz = e.target.closest?.(".wd-dz"); if (!dz) return; e.preventDefault(); dz.classList.remove("over"); startUpload(e.dataTransfer?.files?.[0]?.name); });
    },
  };
  addEventListener("resize", () => { placeHL(false); const r = mapRoot(); if (r) fitWdTitles(r); });
})();
