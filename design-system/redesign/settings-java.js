// 全局设置 →「Java 运行时」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 settings-java.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 6044 行 setJava）：页顶标题和一句说明（「新建服务器时优先用本机已装的 Java；没有合适的，则使用 MineDeck 下载过的或自动新下一份。」）；
// 一组里先是 MineDeck 下载的便携 Java（每行「便携 Java 21」、一句说明、右边「卸载」；没装完的是「便携 Java N（残留）」和「清理」；一个都没有时一行「便携运行时」），
// 再是扫描到的本机 Java（「Java 8」后面一个版本小标签、下面路径；一个都没有时「本机已安装的 Java · 尚未发现」）；另一组「手动添加」（路径框 +「添加」）；
// 最下面「重新扫描本机 Java」（扫描中写「扫描中…（约数秒）」）。卸载、清理先弹危险确认框；有服务器在用时后端拦下（mc.go java.remove：开着的「请先停服再卸载」，
// 配着的「请先到「设置 → 启动与内存」更换后再卸载」）。做完每件事右上角一条提示。程序这一页没有下载 Java 的入口：缺 Java 时新建服务器自动下，
// 换哪个 Java 在每台服务器的「设置 → 启动与内存」里（那里也能选本机没有的、现下一份）。
//
// 这里（内容面板里并排两栏，默认各 422 宽，中间能左右拖，见下面「作者 11-29 定的」）：
//   · 左栏「MineDeck 下载的」：一行一个便携 Java，两行字（「Java 21」、路径），右边是用着它的服务器（一台一颗小胶囊，开着的前面一个成功色小方点；
//     点了去那台服务器的「设置 → 启动与内存」换 Java——这就是这一页「换 Java」的入口），再右边是卸载（垃圾桶，一直露着：行少，灰着的也要让人看见，规范第九节「不藏起来」）。
//     有服务器配着、开着的，卸载灰掉，悬停写后端那句原话（规范第九节「暂时不能用的：灰掉、悬停说原因」，不再点了确认以后才被拦）。
//     没装完的那一行标「残留」（程序那句说明进悬停），清理一直露着。
//   · 右栏「本机装的」：扫描到的和手动加的，「Java 8」后面淡淡的版本号、下面路径（放不下时省中间，见下面 11-29 那段）。
//     右上角贴栏角是「重新扫描本机 Java」（图标按钮；扫描中图标一直转），扫完整个列表一批换（规范第七节「刷新」：旧的一起往上淡掉，新的从下面一行一行淡进来）。
//     最下面手动添加的框（程序的「添加」换成框里的 ＋，回车也行；路径的例子进框的悬停）。不是 java.exe、或者那里的 Java 运行不了，框标红、晃一下，
//     ＋ 的位置换成一句短的红字（全文是后端 java.AddManual 的原话，进悬停）；已经在列表里的那一行闪一下。
//   · 页顶那句说明、两组各自的说明进两栏栏名的悬停；空的时候栏正中一行淡字（「还没有 MineDeck 下载的 Java」「尚未发现」）。
//   · 做完一件事不弹提示，那一处自己变（同详情页各页）：卸载、清理的那一行往右划掉，加的那一行从下面淡进来。
// 作者 11-29 定的（照做）：
//   · 两栏之间能左右拖：两栏中间 8 宽的缝就是手柄（往两边各多 2 像素好抓），移上去光标变左右箭头、缝正中淡淡显出一道线，拖着、键盘聚焦时线是强调色。
//     左栏最窄 332（路径留「D:\…\java.exe」，示例里每行的胶囊——收起来的「生存服 · 朋友 +1」、&java=bad 带「无法启动」的那行——都还不省略），右栏最窄 300（手动添加框的占位字放得下）。
//     拖完记在 localStorage 的 mdSketch.javaSplit（左栏占两栏的比例），换分类回来、刷新、关了重开都用它；面板变宽变窄时按比例跟着变。
//     双击手柄回到正中（主会话加的，记下的值一并清掉）。手柄能 Tab 到（role="separator"），左右方向键一次挪 16 像素（按住 Shift 64），Home、End 到两头。
//   · 路径中间省略：盘符和最后的文件名（残留那一行是 java-N 文件夹）一直留着，从中间往外省上级目录，「Program Files」「runtimes」这类先省；
//     放得下就整条，再是「D:\MineDeck\…\java-21\bin\java.exe」「D:\…\java-21\bin\java.exe」，最窄「D:\…\java.exe」。栏宽一变（拖手柄、换窗口）重新算；完整路径一直在悬停里。
//   · 用着它的服务器：一台一颗胶囊，点了走 ctx.go 去那台的 detail.html?kind=<id>&tab=settings&sub=jvm（停着的加 st=stopped，&java=bad 时坏的那套加 java=bad，
//     主题参数照带）。路径至少留到 Java 文件夹名时胶囊才全摆开，挤不下时两台以上的收成一颗「生存服 · 朋友 +1」（开着的排前面），悬停列全，点了出一个小菜单挑去哪台；
//     再窄路径先省到只剩盘符和文件名，胶囊的名字最后才省。有服务器在用时卸载灰着，悬停说是哪几台（11-27 第一稿就有，核对过）。
//   · 下载 Java（程序这一页没有，新加的）：左栏最下面一块 40 高，和右栏的手动添加框对称；点开往上弹一个小菜单，列程序 JAVA_DOWNLOADABLE 的六个大版本（新的在上），
//     已经下好的灰着写「已下载」，正在下的灰着写「正在下载」（不重复下），有残留的写「残留」、下了就替换那一行。选一个，左栏按版本插进一行（左栏各行一律按版本新的在上，残留的也一样），
//     第二行依次「正在下载… 37%」（进度是编的，约 5 秒走完）、「正在检查…」，好了变成普通的一行、闪一下（同重新下载）。程序的 java.Ensure 有锁、同时只下一套，
//     所以连着选两个时后一个先写「等待下载…」。改程序要补：这一页调 java.Ensure 的接口（放进后台任务，带进度）。
//   · 叫法：确认框标题、读屏标签里的「便携 Java」统一成「MineDeck 下载的 Java」（「卸载 MineDeck 下载的 Java 17？」）；栏名悬停里的「Java 运行时」留着。
//   · 卸载、清理的确认框换成共用的 KIT.confirm（规范第九节：旧的改到哪页顺手换掉），这一页自己那份删了。
// 示例数据同详情页「启动与内存」（detail-settings.js 的 JAVAS）：便携 21、17、25，本机 Java 8（1.8.0_421）；四台服务器各用 21、17、25、21，
// 所以三套便携都有人用、卸载都灰着。另编了一份没装完的 Java 11（能清理、看确认框）；第一次重新扫描会多扫出一个 Temurin 17。路径都是编的。
// 草图专用：&java=spare 多一套没人用的便携 Java 16（看卸载的确认框和往右划掉）；&java=empty 两栏都是空的；
// &java=bad 便携 Java 17 检查不过（标「无法启动」，点重新下载看下载、检查、恢复；用着它的「机械动力整合包」详情页也加 &java=bad 对得上）。
// 程序里有、这里没画的：局域网打开时没有卸载、添加（程序 isLocal；新加的下载也应一样）；读取失败。用着它的服务器这一项程序的 /api/javas 不给，要后端补。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const E_IN = "cubic-bezier(.4, 0, 1, 1)", E_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const hy = el => { window.KIT.hydrate(el); return el; };
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });
  const setTip = (el, text) => { // 悬停字没变就不动（kit.js 悬停时会把 title 挪进 data-tip，两处都看）
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  let C = null; // 外壳最近一次给的 ctx

  // ================= 数据（换分类回来还在） =================
  const RT = "D:\\MineDeck\\runtimes"; // 程序：exe 所在文件夹下的 runtimes\java-N（app.RuntimesDir）；草图里程序放在 D:\MineDeck（同详情页的服务器文件夹）
  const exeOf = m => `${RT}\\java-${m}\\bin\\java.exe`;
  const USES = { paper: 21, forge: 17, vanilla: 25, proxy: 21 }; // 每台服务器配的 Java，同 detail-settings.js
  const TIP_P = "由 MineDeck 独立下载的 Java 运行时。新建服务器缺少合适版本时会自动下载，不影响系统已安装的 Java。\n打开此页时会自动检查各项运行时能否正常启动；无法启动时可重新下载，检查通过后再替换原文件，服务器设置无需修改。\n卸载仅删除对应运行时；若有服务器正在运行或配置使用该版本，则无法卸载。";
  const TIP_S = "新建服务器时优先使用本机已安装的 Java；若无合适版本，则使用 MineDeck 已下载的运行时或自动下载。";
  const TIP_INC = "上次下载或卸载未完成而残留的文件夹，无法用于开服，可点击右侧按钮清理。";
  // 检查和重新下载（作者 11-28 要画的，做法照 v3.3.0-beta.1 下载与更新整改第四节）：打开这一页时把每一套都跑一次 java -version，
  // 看能不能起来、报出来的大版本对不对；检查不过的那一行标「无法启动」，MineDeck 下载的可以重新下载（先下到旁边、检查过再换上去，同一个路径，
  // 用着它的服务器设置不用改）。本机装的检查不过时新建服务器不用它，改用 MineDeck 下载的。左栏右上角「重新检查」再跑一次。
  const JAVA_BROKEN_WHY = "运行 java -version 失败：缺少 bin\\server\\jvm.dll（文件可能已被杀毒软件删除或磁盘异常）"; // 编的，同详情页「启动与内存」
  const TIP_BAD_P = `检查未通过：${JAVA_BROKEN_WHY}\n可点击右侧按钮重新下载，使用该版本的服务器设置无需修改`;
  const TIP_BAD_S = `检查未通过：${JAVA_BROKEN_WHY}\n新建服务器时不会使用此 Java，将改用 MineDeck 下载的运行时`;
  const TIP_ADD = "粘贴 java.exe 或 JDK 文件夹的路径，如 C:\\Program Files\\Java\\jdk-21\\bin\\java.exe";
  const DL_MAJORS = [25, 21, 17, 16, 11, 8]; // 「下载 Java」菜单：程序 app.js 的 JAVA_DOWNLOADABLE（8、11、16、17、21、25），这里新的在上
  let D = null;
  function seed(ctx) {
    if (D) return;
    const mode = ctx.QS.get("java");
    D = {
      portable: [25, 21, 17], // 程序按大版本从小到大列（java.Installed）；这里新的在上
      incomplete: [11],
      scanned: [{ major: 8, version: "1.8.0_421", path: "C:\\Program Files\\Java\\jre1.8.0_421\\bin\\java.exe" }],
      found: [{ major: 17, version: "17.0.13", path: "C:\\Program Files\\Eclipse Adoptium\\jdk-17.0.13.11-hotspot\\bin\\java.exe" }], // 第一次重新扫描时多扫出来的
      scanning: false, draft: "",
      checking: false, checked: false, bad: new Set(mode === "bad" ? ["p17"] : []), badUse: mode === "bad" ? "forge" : "", redl: {}, // redl：那一行重新下载进行到哪（"dl" / "check"）
      job: {}, queue: [], active: 0, // 「下载 Java」：job[行] = { stage: "wait" | "dl" | "check", pct }；queue 排着等的大版本，active 正在下的（程序同时只下一套）
    };
    if (mode === "spare") D.portable = [25, 21, 17, 16];
    if (mode === "empty") { D.portable = []; D.incomplete = []; D.scanned = []; }
  }
  // &java=bad 时用坏掉那套的服务器起不来，当它停着（不然「坏了还在跑」说不通）。开着的排前面（胶囊收成一颗时露出来的是开着的那台）
  const usersOf = m => (C?.servers || []).filter(s => USES[s.id] === m).map(s => s.running && D?.badUse === s.id ? { ...s, running: false } : s)
    .sort((a, b) => b.running - a.running);
  // 卸载会被后端拦下的原因（mc.go java.remove 的原话）：有开着的先说开着的，没有再说配着的
  function blockOf(m) {
    const us = usersOf(m); if (!us.length) return "";
    const run = us.filter(s => s.running);
    return run.length ? `服务器正在使用 Java ${m} 运行（${run.map(s => s.name).join("、")}），请先停止服务器再卸载`
      : `服务器正配置使用 Java ${m}（${us.map(s => s.name).join("、")}），请先在「设置 → 启动与内存」中更换后再卸载`;
  }

  // ================= 画 =================
  // 路径：先放整条，挂上去以后按栏宽换成中间省略的那一档（fitRow）；完整路径一直在悬停里
  const pathHTML = p => `<span class="sj-path" data-p="${esc(p)}" title="${esc(p)}">${esc(p)}</span>`;
  // 去那台服务器的「设置 → 启动与内存」：参数照 detail.html 认的（kind 分服务器，st=stopped 停着，java=bad 那套坏了）；主题参数现从地址栏取（外观页换主题只改地址栏）
  const detailURL = (s, m) => {
    const q = new URLSearchParams({ kind: s.id, tab: "settings", sub: "jvm" }), now = new URLSearchParams(location.search);
    if (!s.running) q.set("st", "stopped");
    if (D.bad.has("p" + m) && !D.redl["p" + m]) q.set("java", "bad");
    ["theme", "scheme", "accent", "corner", "bg", "card"].forEach(k => now.get(k) && q.set(k, now.get(k)));
    return `detail.html?${q}`;
  };
  const svTip = (s, m) => `「${s.name}」${s.running ? "（运行中）" : ""}正在使用 Java ${m}，点击前往「设置 → 启动与内存」更换`;
  const svHTML = (s, m) => `<a class="sj-sv" href="${esc(detailURL(s, m))}" title="${esc(svTip(s, m))}">${s.running ? "<i></i>" : ""}<span>${esc(s.name)}</span></a>`;
  // 谁在用：一台一颗胶囊；两台以上另备一颗收起来的「第一台 +N」（挤不下时换上它，fitRow 定），悬停列全，点了出菜单挑去哪台
  const foldTip = (us, m) => `正在使用 Java ${m} 的服务器：\n${us.map(s => s.name + (s.running ? "（运行中）" : "")).join("\n")}\n点击选择服务器，前往「设置 → 启动与内存」更换`;
  const useHTML = (us, m) => !us.length ? "" : `<div class="sj-use">${us.map(s => svHTML(s, m)).join("")}`
    + (us.length > 1 ? `<button type="button" class="sj-sv sj-fold" data-sj-act="users" data-m="${m}" aria-haspopup="menu" aria-expanded="false" title="${esc(foldTip(us, m))}">`
      + `${us[0].running ? "<i></i>" : ""}<span>${esc(us[0].name)}</span><b>+${us.length - 1}</b></button>` : "") + `</div>`;
  // 名字后面的检查状态：检查中一个小转圈；检查不过一个红的「无法启动」（原因进悬停）；没问题什么都不加。正在下载的那一行不跟着一起检查（它下完自己查）
  const chkHTML = (k, tip) => {
    const j = D.job[k];
    return (j ? j.stage === "check" : D.checking || D.redl[k] === "check") ? `<i data-ic="spinner" class="sj-chk"></i>`
      : !j && D.bad.has(k) && !D.redl[k] ? `<span class="sj-tg bad" title="${esc(tip)}">无法启动</span>` : "";
  };
  const progHTML = j => `<span class="sj-prog">${j.stage === "wait" ? "等待下载…" : j.stage === "check" ? "正在检查…"
    : `正在下载… <span class="sj-pct">${Math.floor(j.pct)}%</span>`}</span>`;
  function pRow(m) {
    const k = "p" + m, us = usersOf(m), broke = D.bad.has(k), rd = D.redl[k], job = D.job[k];
    const why = job ? `${job.stage === "wait" ? "正在等待下载" : "正在下载"}，完成后才能卸载` : rd ? "正在重新下载，完成后才能卸载" : blockOf(m);
    // 重新下载：一直露着；开着的服务器正用它跑时灰掉（文件被占着换不了）。坏了的那一套不会有服务器在用它跑
    const run = broke ? [] : us.filter(s => s.running);
    const rdWhy = run.length ? `服务器正在使用 Java ${m} 运行（${run.map(s => s.name).join("、")}），请先停止服务器再重新下载` : "";
    // 正在下、正在查的那一颗不灰（在转或刚转完）；排着等的灰着、不转
    const busy = rd || (job && job.stage !== "wait"), spin = rd === "dl" || job?.stage === "dl";
    return `<div class="sj-row" data-sj-k="${k}"><div class="sj-main"><div class="sj-l1"><span class="sj-nm">Java ${m}</span>${chkHTML(k, TIP_BAD_P)}</div>`
      + (job ? progHTML(job) : rd ? `<span class="sj-prog">${rd === "dl" ? "正在重新下载…" : "正在检查…"}</span>` : pathHTML(exeOf(m))) + `</div>`
      + useHTML(us, m)
      + `<button type="button" class="sj-ib sj-rd${broke && !rd ? " hot" : ""}${busy ? " busy" : ""}" data-sj-act="redl" data-m="${m}" aria-label="重新下载 MineDeck 下载的 Java ${m}"`
      + (busy || job || rdWhy ? ` aria-disabled="true"` : "") + (busy ? "" : ` title="${esc(job ? "等待前一项下载完成后开始" : rdWhy || `重新下载 Java ${m}`)}"`) + `>`
      + `<i data-ic="${spin ? "spinner" : "download"}"${spin ? ' class="sj-chk"' : ""}></i></button>`
      + `<button type="button" class="sj-ib sj-rm" data-sj-act="rm" data-m="${m}" aria-label="卸载 MineDeck 下载的 Java ${m}"`
      + (why ? ` aria-disabled="true" title="${esc(why)}"` : ` title="卸载"`) + `><i data-ic="trash"></i></button></div>`;
  }
  const iRow = m => `<div class="sj-row inc" data-sj-k="i${m}"><div class="sj-main"><div class="sj-l1"><span class="sj-nm">Java ${m}</span><span class="sj-tg warn" title="${esc(TIP_INC)}">残留</span></div>`
    + `${pathHTML(`${RT}\\java-${m}`)}</div><button type="button" class="sj-ib sj-rm" data-sj-act="clean" data-m="${m}" title="清理" aria-label="清理 MineDeck 下载的 Java ${m} 的残留文件夹"><i data-ic="trash"></i></button></div>`;
  const sRow = j => `<div class="sj-row" data-sj-k="s${esc(j.path.toLowerCase())}"><div class="sj-main"><div class="sj-l1"><span class="sj-nm">Java ${j.major}</span>`
    + `<span class="sj-ver">${esc(j.version)}</span>${chkHTML("s" + j.path.toLowerCase(), TIP_BAD_S)}</div>${pathHTML(j.path)}</div></div>`;
  // 左栏的行一律按版本新的在上，残留的也排进去（直接画和下载时插进去用同一个顺序）
  const pList = () => [...D.portable.map(m => ({ m })), ...D.incomplete.filter(m => !D.portable.includes(m)).map(m => ({ m, inc: true }))].sort((a, b) => b.m - a.m);
  const pRows = () => pList().map(x => (x.inc ? iRow : pRow)(x.m)).join("");
  const sRows = () => D.scanned.map(sRow).join("");
  const pCount = () => pList().length;
  const scanBtn = () => `<button type="button" class="sj-corner${D.scanning ? " busy" : ""}" data-sj-act="scan" title="${D.scanning ? "扫描中…（约数秒）" : "重新扫描本机 Java"}" aria-label="重新扫描本机 Java"`
    + `${D.scanning ? ' aria-disabled="true"' : ""}><i data-ic="refresh"></i></button>`;

  const checkBtn = () => `<button type="button" class="sj-corner${D.checking ? " busy" : ""}" data-sj-act="check" title="${D.checking ? "检查中…" : "重新检查"}" aria-label="重新检查 MineDeck 下载的 Java"`
    + `${D.checking ? ' aria-disabled="true"' : ""}><i data-ic="refresh"></i></button>`;

  // 左栏最下面的「下载 Java」：40 高，和右栏的手动添加框对称（同样的底、细边、圆角 10），右边一个 ▴ 表示往上弹菜单
  const dlBtn = () => `<button type="button" class="sj-dlb" data-sj-act="dl" aria-haspopup="menu" aria-expanded="false"><i data-ic="download"></i><span>下载 Java</span>`
    + `<i data-ic="chevronUp" class="sj-chev"></i></button>`;

  function html(ctx) {
    seed(ctx); C = ctx;
    const left = `<section class="sj-col" aria-label="MineDeck 下载的 Java"><div class="sj-hd fdpad"><span class="sj-t" title="${esc(TIP_P)}">MineDeck 下载的</span></div>${checkBtn()}`
      + `<div class="sj-lw${pCount() ? "" : " is-none"}" data-sj-lw="p"><div class="list">${pRows()}</div><span class="thumb"></span><div class="sj-none">暂无 MineDeck 下载的 Java</div></div>${dlBtn()}</section>`;
    // 两栏中间的手柄（11-29 作者要的）：比例每次画都从 localStorage 现读，画出来就是记下的宽，不先闪一下正中
    const split = `<div class="sj-split" role="separator" aria-orientation="vertical" tabindex="0" aria-label="调整两栏宽度"></div>`;
    const right = `<section class="sj-col" aria-label="本机装的 Java"><div class="sj-hd fdpad"><span class="sj-t" title="${esc(TIP_S)}">本机装的</span></div>${scanBtn()}`
      + `<div class="sj-lw${D.scanned.length ? "" : " is-none"}" data-sj-lw="s"><div class="list">${sRows()}</div><span class="thumb"></span><div class="sj-none">未检测到</div></div>`
      + `<label class="sj-add${D.draft.trim() ? " has" : ""}" title="${esc(TIP_ADD)}"><input value="${esc(D.draft)}" placeholder="粘贴 java.exe 或 JDK 文件夹的路径" spellcheck="false" autocomplete="off" aria-label="手动添加 Java 路径">`
      + `<span class="sj-msg"></span><button type="button" class="sj-ib sj-plus" data-sj-act="add" title="添加" aria-label="添加 Java"><i data-ic="plus"></i></button></label></section>`;
    return `<div class="sj" style="--sj-r: ${loadSplit()}">${left}${split}${right}</div>`;
  }

  // ================= 挂上去 =================
  const root = () => document.querySelector(".cp > .sj:not(.fade-out)");
  const lwOf = k => root()?.querySelector(`[data-sj-lw="${k}"]`);
  const rowEl = k => [...(root()?.querySelectorAll(".sj-row:not(.sj-out)") || [])].find(r => r.dataset.sjK === k);

  // ================= 两栏的宽（11-29 作者要的：能左右拖，记住，下次进来不回正中） =================
  const SPLIT_KEY = "mdSketch.javaSplit", L_MIN = 332, R_MIN = 300, GAP = 8, STEP = 16; // 两个最窄同 settings-java.css 里 .sj 的 clamp
  let split = .5; // localStorage 用不了（隐私窗口之类）时记在这里，至少这次打开期间换分类回来还在
  function loadSplit() {
    try { const v = localStorage.getItem(SPLIT_KEY); split = v === null ? .5 : +v > 0 && +v < 1 ? +v : .5; } catch { /* 读不了就用记着的 */ }
    return split;
  }
  function saveSplit(r) {
    split = +r.toFixed(4);
    try { if (split === .5) localStorage.removeItem(SPLIT_KEY); else localStorage.setItem(SPLIT_KEY, String(split)); } catch { /* 存不了照样能拖 */ }
  }
  // 设左栏占两栏的比例：夹在两个最窄之间（按眼下面板的宽算；CSS 里也夹着，面板变宽变窄时照比例跟着变），同步读屏用的 aria-value*
  function setSplit(r, save) {
    const sj = root(); if (!sj) return split;
    const W = sj.clientWidth - GAP, lo = Math.min(.5, L_MIN / W), hi = Math.max(.5, 1 - R_MIN / W);
    r = Math.min(hi, Math.max(lo, r));
    sj.style.setProperty("--sj-r", String(r));
    const sp = sj.querySelector(".sj-split");
    sp.setAttribute("aria-valuenow", Math.round(r * 100)); sp.setAttribute("aria-valuemin", Math.round(lo * 100)); sp.setAttribute("aria-valuemax", Math.round(hi * 100));
    if (save) saveSplit(r); else split = r;
    return r;
  }
  const leftW = sj => sj.querySelector(".sj-col").getBoundingClientRect().width;
  // 拖：左栏按整像素跟着鼠标走，松手记下。指针捕获拿不到（脚本合成的指针）也照样能拖：移动、松开听在 window 上。
  // 鼠标拖不抢焦点（抢了以后那道线会一直亮着强调色）；键盘要调时 Tab 过来
  function splitDown(e) {
    const sp = e.currentTarget, sj = root(); if (e.button !== 0 || !sj) return;
    e.preventDefault();
    try { sp.setPointerCapture(e.pointerId); } catch { /* 合成的指针没法捕获 */ }
    const W = sj.clientWidth - GAP, l0 = leftW(sj), x0 = e.clientX;
    let r = split;
    sp.classList.add("drag"); document.documentElement.classList.add("sj-dragging");
    const move = ev => { r = setSplit(Math.round(l0 + ev.clientX - x0) / W); };
    const up = () => {
      sp.classList.remove("drag"); document.documentElement.classList.remove("sj-dragging");
      removeEventListener("pointermove", move); removeEventListener("pointerup", up); removeEventListener("pointercancel", up);
      saveSplit(r);
    };
    addEventListener("pointermove", move); addEventListener("pointerup", up); addEventListener("pointercancel", up);
  }
  // 键盘：左右方向键一次 16 像素（Shift 64），Home、End 到两头
  function splitKey(e) {
    const sj = root(); if (!sj) return;
    const W = sj.clientWidth - GAP, cur = leftW(sj), d = e.shiftKey ? STEP * 4 : STEP;
    const px = { ArrowLeft: cur - d, ArrowRight: cur + d, Home: 0, End: W }[e.key];
    if (px === undefined) return;
    e.preventDefault(); setSplit(Math.round(px) / W, true);
  }

  // ================= 路径中间省略、胶囊收起（11-29 作者要的） =================
  // 路径的几档：整条 → 从中间往外省上级目录（连着的一段，「Program Files」「runtimes」这类先省，一样时省靠后的）→ 只剩盘符和文件名。
  // 「…\bin\java.exe」前面的 Java 文件夹名先留着，上级目录省光了才连它一起省。need 是「上级目录省光、Java 文件夹名还在」那一档
  const BORING = /^(program files( \(x86\))?|programs|runtimes?|java|users|appdata|local|roaming)$/i;
  const lt = (x, y) => x[0] !== y[0] ? x[0] < y[0] : x[1] !== y[1] ? x[1] < y[1] : x[2] < y[2];
  const candCache = new Map();
  function pathCands(p) {
    if (candCache.has(p)) return candCache.get(p);
    const s = p.split("\\"), n = s.length, out = [p];
    const exe = n >= 4 && /^javaw?\.exe$/i.test(s[n - 1]) && /^bin$/i.test(s[n - 2]);
    const a = 1, b = n - (exe ? 3 : 1) - 1, c = (a + b) / 2; // 能省的上级目录是 s[a..b]
    const cut = (lo, hi) => [...s.slice(0, lo), "…", ...s.slice(hi + 1)].join("\\");
    for (let k = 1; k <= b - a + 1; k++) {
      let best = null;
      for (let lo = a; lo + k - 1 <= b; lo++) {
        const hi = lo + k - 1, key = [s.slice(lo, hi + 1).filter(x => !BORING.test(x)).length, Math.abs((lo + hi) / 2 - c), -lo];
        if (!best || lt(key, best.key)) best = { key, lo, hi };
      }
      out.push(cut(best.lo, best.hi));
    }
    const need = out.length - 1;
    if (n >= 3) out.push(cut(1, n - 2));
    const r = { list: [...new Set(out)], need };
    candCache.set(p, r); return r;
  }
  const CAP_MIN = 76; // 胶囊挤到最窄：开着的小方点、名字留两三个字、「+N」
  // 量一行里不随栏宽变的东西（画完、换了内容、字体载完时量一次）：按钮和间距占的宽、第一行（名字和标记）、胶囊全摆开和收起来的宽、路径每一档的宽
  function measureRow(row) {
    const main = row.querySelector(".sj-main"), use = row.querySelector(".sj-use"), path = row.querySelector(".sj-path"), l1 = row.querySelector(".sj-l1");
    if (use) { use.classList.remove("fold"); use.style.width = "0px"; }
    const m = { fixed: row.getBoundingClientRect().width - main.getBoundingClientRect().width };
    const gap = parseFloat(getComputedStyle(l1).columnGap) || 0;
    m.l1 = [...l1.children].reduce((w, x) => w + x.getBoundingClientRect().width + gap, -gap);
    if (use) {
      use.style.width = "max-content"; m.full = m.fold = use.getBoundingClientRect().width;
      if (use.querySelector(".sj-fold")) { use.classList.add("fold"); m.fold = use.getBoundingClientRect().width; use.classList.remove("fold"); }
    }
    if (path) { // 每一档放进一个看不见的同款 span 里量（同一套字体回退，「…」落在哪个字体上都量得准）
      const pc = pathCands(path.dataset.p), pr = path.cloneNode();
      pr.removeAttribute("title"); pr.style.cssText = "position: absolute; visibility: hidden; width: auto; overflow: visible";
      main.appendChild(pr);
      m.cands = pc.list.map(s => { pr.textContent = s; const w = pr.getBoundingClientRect().width; return { s, w, e: Math.max(w, m.l1) }; }); // e：这一档时名字那一栏要的宽（第一行不缩，比路径宽时按它算）
      pr.remove();
      m.need = m.cands[pc.need].e;
    } else { m.cands = [{ s: null, w: 0, e: m.l1 }]; m.need = m.l1; }
    return (row._m = m);
  }
  // 按眼下的宽分：路径留到 Java 文件夹名还放得下全部胶囊，就全摆开；不然胶囊收成一颗（只有一台的就让名字省略），
  // 先保胶囊最窄 CAP_MIN 挑路径能放的最长那档，剩下的给胶囊（到它不省略为止），再剩下的还给路径
  function fitRow(row, rw) {
    const m = row._m || measureRow(row), use = row.querySelector(".sj-use"), path = row.querySelector(".sj-path");
    const avail = (rw ?? row.getBoundingClientRect().width) - m.fixed, last = m.cands[m.cands.length - 1];
    let useW = 0;
    if (use) {
      let fold = false;
      if (m.full + m.need <= avail) useW = m.full;
      else {
        fold = m.fold < m.full;
        const c = m.cands.find(x => x.e <= avail - Math.min(m.fold, CAP_MIN)) || last;
        useW = Math.max(0, Math.min(m.fold, avail - c.e));
      }
      useW = Math.ceil(useW);
      use.classList.toggle("fold", fold); use.style.width = useW + "px";
    }
    if (path) {
      const room = avail - useW - 1;
      let i = m.cands.findIndex(x => x.w <= room); if (i < 0) i = m.cands.length - 1;
      if (path.textContent !== m.cands[i].s) {
        path.textContent = m.cands[i].s;
        // 量的和画出来的差一点时（字体还没载完、「…」落到别的字体上）：再降一档
        while (i < m.cands.length - 1 && path.scrollWidth > path.clientWidth) path.textContent = m.cands[++i].s;
      }
    }
  }
  // scope 里的行（或者 scope 本身就是一行）都按眼下的宽排一遍；fresh：内容或字体变了，重新量。版本号放不下被截断时全文进悬停
  function fitRows(scope, fresh) {
    const rows = scope.classList?.contains("sj-row") ? [scope] : [...scope.querySelectorAll(".sj-row")];
    if (fresh) rows.forEach(r => delete r._m);
    rows.forEach(r => r._m || measureRow(r));
    const ws = rows.map(r => r.getBoundingClientRect().width);
    rows.forEach((r, i) => fitRow(r, ws[i]));
    scope.querySelectorAll(".sj-ver").forEach(v => setTip(v, v.scrollWidth > v.clientWidth + 1 ? v.textContent : ""));
  }
  // 栏宽一变（拖手柄、换窗口）就重排那一栏；面板宽变了顺手把手柄的 aria-value* 对上
  let ro = null, fontHooked = false;
  function watch(r) {
    ro?.disconnect();
    ro = new ResizeObserver(es => es.forEach(e => { if (!e.target.isConnected) return; if (e.target === r) setSplit(split); else fitRows(e.target); }));
    ro.observe(r); r.querySelectorAll(".sj-col").forEach(c => ro.observe(c));
  }

  function mount(ctx) {
    C = ctx;
    const r = root(); if (!r) return;
    r.querySelectorAll(".sj-lw").forEach(w => ctx.thinBar(w));
    setSplit(split); fitRows(r); document.fonts?.ready.then(() => r.isConnected && fitRows(r, true));
    watch(r);
    if (!fontHooked && document.fonts) { // 字体（中文那套按字分块）后来才载完的：重新量一遍
      fontHooked = true; document.fonts.addEventListener("loadingdone", () => { const x = root(); if (x) fitRows(x, true); });
    }
    const sp = r.querySelector(".sj-split");
    sp.addEventListener("pointerdown", splitDown);
    sp.addEventListener("dblclick", () => setSplit(.5, true)); // 双击回到正中（主会话加的），记下的一并清掉
    sp.addEventListener("keydown", splitKey);
    r.addEventListener("click", onClick);
    r.addEventListener("keydown", onKey);
    r.addEventListener("input", onInput);
    if (!D.checked) check();
  }

  // ================= 只换变了的那一处 =================
  // 拿掉一行：往右划 32 像素淡掉，再收起（同「玩家」名单、「备份」）；最后一行没了，正中那句淡字再淡进来
  function dropRow(k, lw) {
    const row = rowEl(k); if (!row) return;
    row.classList.add("sj-out");
    const h = row.offsetHeight + "px";
    row.animate([
      { opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: E_IN },
      { opacity: 0, transform: "translateX(32px)", height: h, marginBottom: "0px", offset: .55, easing: E_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 320, fill: "forwards" }).onfinish = () => {
      const w = row.closest(".sj-lw"); row.remove();
      if (w) { w.classList.toggle("is-none", !w.querySelector(".sj-row")); w._paint?.(); }
    };
  }
  // 新加的一行：从下面 5 像素淡进来，列表滚到它露出来（同「玩家」加名字）
  function addRow(k, rowHTML) {
    const w = lwOf(k); if (!w) return;
    const list = w.querySelector(".list");
    list.insertAdjacentHTML("beforeend", rowHTML);
    const row = hy(list.lastElementChild); fitRows(row);
    w.classList.remove("is-none");
    row.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: E_OUT, fill: "backwards" });
    w._paint?.();
    list.scrollTo({ top: list.scrollHeight, behavior: "smooth" });
  }
  // 整个列表一批换（重新扫描完）：旧的一起往上 5 像素淡掉（0.09 秒），新的从下面 5 像素一行一行淡进来（0.22 秒、错开 22 毫秒，只给看得见的做）
  function swapList(k, html) {
    const w = lwOf(k); if (!w) return;
    const list = w.querySelector(".list"), olds = [...list.children];
    const tok = list._sjTok = (list._sjTok || 0) + 1;
    olds.forEach(x => { x.getAnimations().forEach(a => a.cancel()); x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: E_IN, fill: "forwards" }); });
    setTimeout(() => {
      if (list._sjTok !== tok || !list.isConnected) return;
      list.innerHTML = html; hy(list); fitRows(list);
      w.classList.toggle("is-none", !list.children.length); w._paint?.();
      const bottom = list.getBoundingClientRect().bottom;
      [...list.children].filter(x => x.getBoundingClientRect().top < bottom).forEach((x, i) =>
        x.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: E_OUT, fill: "backwards" }));
    }, olds.length ? 90 : 0);
  }
  function flash(k) {
    const row = rowEl(k); if (!row) return;
    row.scrollIntoView({ block: "nearest", behavior: "smooth" });
    row.classList.remove("sj-flash"); void row.offsetWidth; row.classList.add("sj-flash");
  }
  // 重新扫描：图标转到扫完（程序「扫描中…（约数秒）」），草图里装作扫了 1.4 秒；扫完整个列表一批换。中途换了分类，回来时还在转
  function scan() {
    if (D.scanning) return;
    D.scanning = true; paintScan();
    setTimeout(() => {
      D.scanning = false;
      D.found.forEach(j => { if (!D.scanned.some(x => x.path.toLowerCase() === j.path.toLowerCase())) D.scanned.push(j); });
      D.found = [];
      paintScan(); swapList("s", sRows());
    }, 1400);
  }
  // 检查：打开这一页时自动跑一次（换分类回来不再跑），左栏右上角「重新检查」再跑。草图里装作检查了 0.8 秒；只换名字后面那一小块，行不重画
  function check() {
    if (D.checking) return;
    D.checking = true; paintChecks();
    setTimeout(() => { D.checking = false; D.checked = true; paintChecks(); }, 800);
  }
  function paintChecks() {
    const b = root()?.querySelector('[data-sj-act="check"]');
    if (b) { b.classList.toggle("busy", D.checking); if (D.checking) b.setAttribute("aria-disabled", "true"); else b.removeAttribute("aria-disabled"); setTip(b, D.checking ? "检查中…" : "重新检查"); }
    D.portable.forEach(m => paintRow("p" + m, pRow(m)));
    D.scanned.forEach(j => paintRow("s" + j.path.toLowerCase(), sRow(j)));
  }
  // 换一行的内容（状态变了的那一行）：位置不动，不做动效
  function paintRow(k, rowHTML) {
    const row = rowEl(k); if (!row) return;
    const t = document.createElement("div"); t.innerHTML = rowHTML;
    const n = t.firstElementChild; row.replaceChildren(...n.childNodes); hy(row); fitRows(row, true);
  }
  const rowOf = html => { const t = document.createElement("div"); t.innerHTML = html; return hy(t.firstElementChild); };
  // 左栏按版本插进一行（下载 Java）：先撑开高度，再从下面 5 像素淡进来（同「启动与内存」加参数），列表滚到它露出来
  function insertRow(m, html) {
    const w = lwOf("p"); if (!w) return;
    const list = w.querySelector(".list"), row = rowOf(html);
    const next = [...list.querySelectorAll(".sj-row:not(.sj-out)")].find(r => +r.dataset.sjK.slice(1) < m);
    list.insertBefore(row, next || null);
    w.classList.remove("is-none"); fitRows(row);
    const h = row.offsetHeight + "px", mg = next ? "marginBottom" : "marginTop"; // 收起时把它带来的 2 像素间距也吃掉（插在最后时那道间距在它上面）
    row.animate([{ height: "0px", [mg]: "-2px", opacity: 0, transform: "translateY(5px)", easing: SLIDE },
      { height: h, [mg]: "0px", opacity: 0, transform: "translateY(5px)", offset: .4, easing: E_OUT },
      { height: h, [mg]: "0px", opacity: 1, transform: "none" }], { duration: 360, fill: "backwards" }).onfinish = () => w._paint?.();
    w._paint?.();
    setTimeout(() => row.isConnected && row.scrollIntoView({ block: "nearest", behavior: "smooth" }), 60);
  }
  // 残留那一行就地换成正在下载的这一行（位置不动，里面的字换一下、淡进来）
  function swapRow(oldK, html) {
    const old = rowEl(oldK); if (!old) return false;
    const row = rowOf(html); old.replaceWith(row); fitRows(row);
    [...row.children].forEach(x => x.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 220, easing: E_OUT }));
    return true;
  }
  // 重新下载（草图：下 1.6 秒、检查 0.6 秒；好了「无法启动」拿掉、那一行闪一下）
  function redownload(m) {
    const k = "p" + m; if (D.redl[k]) return;
    D.redl[k] = "dl"; paintRow(k, pRow(m));
    setTimeout(() => { D.redl[k] = "check"; paintRow(k, pRow(m)); }, 1600);
    setTimeout(() => { delete D.redl[k]; D.bad.delete(k); paintRow(k, pRow(m)); flash(k); }, 2200);
  }
  // 下载 Java（11-29 作者要的）：左栏按版本插进一行（有残留的就地换掉残留那一行），第二行「正在下载… N%」→「正在检查…」，好了变成普通的一行、闪一下。
  // 程序 java.Ensure 有锁、同时只下一套：前一个没下完时这一个先「等待下载…」。状态都在 D 里，中途换分类，回来时画出同一刻的样子
  function download(m) {
    const k = "p" + m; if (D.job[k] || D.portable.includes(m)) return; // 正在下的、已经有的不重复下
    const inc = D.incomplete.includes(m);
    D.incomplete = D.incomplete.filter(x => x !== m);
    D.portable = [...D.portable, m].sort((a, b) => b - a);
    D.job[k] = { stage: "wait", pct: 0 }; D.queue.push(m);
    pump(true); // 前面没在下的话马上开始（先改状态再画，这一行画出来就是「正在下载」，淡进来的动效不被重画打断）
    if (!(inc && swapRow("i" + m, pRow(m)))) insertRow(m, pRow(m));
  }
  function pump(quiet) {
    if (D.active || !D.queue.length) return;
    const m = D.active = D.queue.shift(), k = "p" + m, job = D.job[k];
    job.stage = "dl"; if (!quiet) paintRow(k, pRow(m));
    const tick = () => { // 草图编的进度：每 0.14 秒涨 1～4.5，约 5 秒到头
      job.pct = Math.min(100, job.pct + 1 + Math.random() * 3.5);
      if (job.pct < 100) { const p = rowEl(k)?.querySelector(".sj-pct"); if (p) p.textContent = Math.floor(job.pct) + "%"; setTimeout(tick, 140); return; }
      job.stage = "check"; paintRow(k, pRow(m));
      setTimeout(() => { delete D.job[k]; D.active = 0; paintRow(k, pRow(m)); flash(k); pump(); }, 600);
    };
    setTimeout(tick, 140);
  }
  function paintScan() {
    const b = root()?.querySelector('[data-sj-act="scan"]'); if (!b) return;
    b.classList.toggle("busy", D.scanning);
    if (D.scanning) b.setAttribute("aria-disabled", "true"); else b.removeAttribute("aria-disabled");
    setTip(b, D.scanning ? "扫描中…（约数秒）" : "重新扫描本机 Java");
  }

  // ---- 手动添加：照后端 java.AddManual（文件夹就找它下面的 bin\java.exe；只认 java.exe；那里的 Java 要能运行）。草图里从路径里认大版本，认不出就当运行不了 ----
  const PATCH = { 11: "11.0.25", 16: "16.0.2", 17: "17.0.13", 21: "21.0.5", 25: "25.0.1" };
  function probe(v) {
    let p = v.replace(/\//g, "\\").replace(/\\+$/, "");
    const base = p.split("\\").pop();
    if (/\.exe$/i.test(base) && !/^javaw?\.exe$/i.test(base)) return { short: "请选择 java.exe", err: "请选择 java.exe（或 Java 的安装文件夹）" };
    if (!/\.exe$/i.test(base)) p = /\\bin$/i.test(p) ? p + "\\java.exe" : p + "\\bin\\java.exe";
    const m = p.match(/(?:jdk|jre|java|temurin|zulu|openjdk)[-_ ]?(1\.)?(\d{1,2})(?!\d)/i);
    const major = m ? +m[2] : 0;
    if (!m || major < 8 || major > 30 || (m[1] && major !== 8)) return { short: "无法运行此 Java", err: "该路径下的 Java 无法运行，请确认所选 Java 完整可用" };
    return { major, version: major === 8 ? "1.8.0_" + (400 + (p.length % 60)) : PATCH[major] || `${major}.0.1`, path: p };
  }
  function bad(box, short, full) {
    box.classList.add("bad");
    const m = $(".sj-msg", box); m.textContent = short; setTip(m, full || "");
    shake(box); $("input", box)?.focus();
  }
  function submit(box) {
    const inp = $("input", box), v = inp.value.trim();
    inp.focus();
    if (!v) return;
    const r = probe(v);
    if (r.err) { bad(box, r.short, r.err); return; }
    inp.value = ""; D.draft = ""; box.classList.remove("has", "bad");
    const k = "s" + r.path.toLowerCase();
    if (D.scanned.some(j => j.path.toLowerCase() === r.path.toLowerCase())) { flash(k); return; } // 程序：已经在列表里的照样回一个，不重复加
    D.scanned.push(r); addRow("s", sRow(r));
  }

  // ---- 卸载、清理：危险确认框（程序 confirmModal 的原话；标题点名是哪一套，按钮写出后果）。共用 KIT.confirm（规范第九节），默认落在「取消」 ----
  async function askRemove(m, inc) {
    const k = await window.KIT.confirm({ ic: "trash", tone: "dgr",
      title: inc ? `清理 MineDeck 下载的 Java ${m} 的残留？` : `卸载 MineDeck 下载的 Java ${m}？`,
      body: inc ? `将删除未完整安装或卸载残留的 Java ${m} 文件夹，不影响系统已安装的 Java。` : `仅删除 MineDeck 下载的 Java ${m}，不影响系统已安装的 Java。`,
      btns: [{ k: "cancel", label: "取消" }, { k: "ok", label: inc ? "清理" : `卸载 Java ${m}`, tone: "dgr" }] });
    if (k !== "ok") return;
    if (inc) D.incomplete = D.incomplete.filter(x => x !== m); else D.portable = D.portable.filter(x => x !== m);
    dropRow((inc ? "i" : "p") + m);
  }

  // ---- 小菜单（「下载 Java」、收起来的胶囊）：同「手机远程管理」换地址那张（外圆角 16、细边 1 加内边距 5，项 36 高、圆角 10），挂在 body 上；
  //      下面放得下往下弹，放不下往上；点外面、Esc、一滚就收。方向键上下挪、回车选 ----
  let menu = null;
  function openMenu(anchor, html, pick) {
    if (menu && menu._a === anchor) { closeMenu(); return; }
    closeMenu(true);
    const m = menu = Object.assign(document.createElement("div"), { className: "sj-dm enter" });
    m.setAttribute("role", "menu"); m.innerHTML = html; document.body.appendChild(hy(m));
    const r = anchor.getBoundingClientRect(), w = m.offsetWidth, h = m.offsetHeight, up = r.bottom + 6 + h > innerHeight - 8;
    m.classList.toggle("up", up);
    m.style.left = Math.round(Math.max(8, Math.min(r.left, innerWidth - w - 8))) + "px";
    m.style.top = Math.round(up ? r.top - 6 - h : r.bottom + 6) + "px";
    m._a = anchor; anchor.classList.add("open"); anchor.setAttribute("aria-expanded", "true");
    m.addEventListener("click", e => {
      const it = e.target.closest('[role="menuitem"]'); if (!it) return;
      e.preventDefault();
      if (it.getAttribute("aria-disabled") === "true") return;
      closeMenu(); pick(it);
    });
    m.addEventListener("keydown", e => {
      const its = [...m.querySelectorAll('[role="menuitem"]:not([aria-disabled="true"])')], i = its.indexOf(document.activeElement);
      if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); its[(i + (e.key === "ArrowDown" ? 1 : its.length - 1)) % its.length]?.focus(); }
      else if (e.key === "Enter" || e.key === " ") { e.preventDefault(); document.activeElement?.closest?.('[role="menuitem"]')?.click(); }
      else if (e.key === "Tab") closeMenu();
    });
    return m;
  }
  function closeMenu(now) {
    if (!menu) return; const m = menu; menu = null;
    m._a.classList.remove("open"); m._a.setAttribute("aria-expanded", "false");
    if (now) { m.remove(); return; }
    m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90);
  }
  document.addEventListener("pointerdown", e => { if (menu && !e.target.closest?.(".sj-dm") && !menu._a.contains(e.target)) closeMenu(); }, true);
  document.addEventListener("keydown", e => { if (e.key === "Escape" && menu) { const a = menu._a; closeMenu(); a.isConnected && a.focus(); } });
  document.addEventListener("scroll", () => closeMenu(true), true);
  // 「下载 Java」菜单：已经下好的灰着写「已下载」、正在下的灰着写「正在下载」「等待下载」；有残留的写「残留」，下了就替换那一行
  const dlMenu = () => DL_MAJORS.map(m => {
    const job = D.job["p" + m], has = D.portable.includes(m), inc = !has && D.incomplete.includes(m);
    const note = job ? (job.stage === "wait" ? "等待下载" : "正在下载") : has ? "已下载" : inc ? "残留" : "";
    return `<div class="sj-it" role="menuitem" tabindex="-1" data-m="${m}"${has ? ' aria-disabled="true"' : ""}${inc ? ` title="下载后替换残留的文件夹"` : ""}>`
      + `<span class="sj-im">Java ${m}</span>${note ? `<span class="sj-is${inc ? " warn" : ""}">${note}</span>` : ""}${has ? "" : `<i data-ic="download"></i>`}</div>`;
  }).join("");
  // 收起来的胶囊点开：用着这一套的每台服务器一项，点了去那台的「设置 → 启动与内存」
  const usersMenu = m => usersOf(m).map(s => `<a class="sj-it" role="menuitem" tabindex="-1" href="${esc(detailURL(s, m))}" title="${esc(svTip(s, m))}">`
    + `<i class="sj-dot${s.running ? "" : " off"}"></i><span class="sj-im">${esc(s.name)}</span></a>`).join("");
  function openDl(b, kb) {
    const m = openMenu(b, dlMenu(), it => download(+it.dataset.m));
    if (m && kb) m.querySelector('[role="menuitem"]:not([aria-disabled="true"])')?.focus();
  }
  function openUsers(b, kb) {
    const m = openMenu(b, usersMenu(+b.dataset.m), it => C.go(it.href));
    if (m && kb) m.querySelector('[role="menuitem"]')?.focus();
  }

  // ================= 事件（绑在这一页的根上，换分类时跟着内容一起扔掉） =================
  function onClick(e) {
    // 胶囊：去那台服务器的「启动与内存」，走外壳的 ctx.go（有没保存的先问；href 在点的这一刻读，背景图参数 kit.js 已经补上）
    const a = e.target.closest("a.sj-sv");
    if (a) { if (e.button || e.ctrlKey || e.metaKey || e.shiftKey) return; e.preventDefault(); C.go(a.href); return; }
    const b = e.target.closest("[data-sj-act]"); if (!b) return;
    if (b.getAttribute("aria-disabled") === "true") return;
    const m = +b.dataset.m;
    switch (b.dataset.sjAct) {
      case "scan": scan(); break;
      case "check": check(); break;
      case "dl": openDl(b, !e.detail); break;
      case "users": openUsers(b, !e.detail); break;
      case "redl": redownload(m); break;
      case "add": e.preventDefault(); submit(b.closest(".sj-add")); break;
      case "rm": askRemove(m, false); break;
      case "clean": askRemove(m, true); break;
    }
  }
  function onKey(e) {
    const box = e.target.closest?.(".sj-add"); if (!box || e.target.tagName !== "INPUT") return;
    if (e.key === "Enter") { e.preventDefault(); submit(box); }
  }
  function onInput(e) {
    const box = e.target.closest?.(".sj-add"); if (!box) return;
    D.draft = e.target.value;
    box.classList.remove("bad"); box.classList.toggle("has", !!e.target.value.trim());
  }

  window.SETTINGS_SECS.java = { html, mount };
})();
