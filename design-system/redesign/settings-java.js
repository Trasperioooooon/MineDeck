// 全局设置 →「Java 运行时」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 settings-java.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 6044 行 setJava）：页顶标题和一句说明（「新建服务器时优先用本机已装的 Java；没有合适的，则使用 MineDeck 下载过的或自动新下一份。」）；
// 一组里先是 MineDeck 下载的便携 Java（每行「便携 Java 21」、一句说明、右边「卸载」；没装完的是「便携 Java N（残留）」和「清理」；一个都没有时一行「便携运行时」），
// 再是扫描到的本机 Java（「Java 8」后面一个版本小标签、下面路径；一个都没有时「本机已安装的 Java · 尚未发现」）；另一组「手动添加」（路径框 +「添加」）；
// 最下面「重新扫描本机 Java」（扫描中写「扫描中…（约数秒）」）。卸载、清理先弹危险确认框；有服务器在用时后端拦下（mc.go java.remove：开着的「请先停服再卸载」，
// 配着的「请先到「设置 → 启动与内存」更换后再卸载」）。做完每件事右上角一条提示。程序这一页没有下载 Java 的入口：缺 Java 时新建服务器自动下，
// 换哪个 Java 在每台服务器的「设置 → 启动与内存」里（那里也能选本机没有的、现下一份）。
//
// 这里（内容面板里并排两栏，各 422 宽）：
//   · 左栏「MineDeck 下载的」：一行一个便携 Java，两行字（「Java 21」、路径），右边是用着它的服务器（一台一颗小胶囊，开着的前面一个成功色小方点；
//     点了去那台服务器的「设置 → 启动与内存」换 Java——这就是这一页「换 Java」的入口），再右边是卸载（垃圾桶，一直露着：行少，灰着的也要让人看见，规范第九节「不藏起来」）。
//     有服务器配着、开着的，卸载灰掉，悬停写后端那句原话（规范第九节「暂时不能用的：灰掉、悬停说原因」，不再点了确认以后才被拦）。
//     没装完的那一行标「残留」（程序那句说明进悬停），清理一直露着。
//   · 右栏「本机装的」：扫描到的和手动加的，「Java 8」后面淡淡的版本号、下面路径（路径放不下时先省前面的上级目录，Java 文件夹名和 bin\java.exe 留着）。
//     右上角贴栏角是「重新扫描本机 Java」（图标按钮；扫描中图标一直转），扫完整个列表一批换（规范第七节「刷新」：旧的一起往上淡掉，新的从下面一行一行淡进来）。
//     最下面手动添加的框（程序的「添加」换成框里的 ＋，回车也行；路径的例子进框的悬停）。不是 java.exe、或者那里的 Java 运行不了，框标红、晃一下，
//     ＋ 的位置换成一句短的红字（全文是后端 java.AddManual 的原话，进悬停）；已经在列表里的那一行闪一下。
//   · 页顶那句说明、两组各自的说明进两栏栏名的悬停；空的时候栏正中一行淡字（「还没有 MineDeck 下载的 Java」「尚未发现」）。
//   · 做完一件事不弹提示，那一处自己变（同详情页各页）：卸载、清理的那一行往右划掉，加的那一行从下面淡进来。
// 示例数据同详情页「启动与内存」（detail-settings.js 的 JAVAS）：便携 21、17、25，本机 Java 8（1.8.0_421）；四台服务器各用 21、17、25、21，
// 所以三套便携都有人用、卸载都灰着。另编了一份没装完的 Java 11（能清理、看确认框）；第一次重新扫描会多扫出一个 Temurin 17。路径都是编的。
// 草图专用：&java=spare 多一套没人用的便携 Java 16（看卸载的确认框和往右划掉）；&java=empty 两栏都是空的；
// &java=bad 便携 Java 17 检查不过（标「无法启动」，点重新下载看下载、检查、恢复；用着它的「机械动力整合包」详情页也加 &java=bad 对得上）。
// 程序里有、这里没画的：局域网打开时没有卸载、添加（程序 isLocal）；读取失败。用着它的服务器这一项程序的 /api/javas 不给，要后端补。
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
  const TIP_P = "由 MineDeck 独立下载的 Java 运行时。新建服务器缺少合适版本时会自动下载，不影响系统已安装的 Java。\n打开此页时会检查每一套能否正常启动；无法启动的可重新下载，检查通过后才替换原文件，服务器设置不用改。\n卸载仅删除对应运行时；若有服务器正在运行或配置使用该版本，则无法卸载。";
  const TIP_S = "新建服务器时优先使用本机已安装的 Java；若无合适版本，则使用 MineDeck 已下载的运行时或自动下载。";
  const TIP_INC = "上次下载或卸载未完成而残留的文件夹，无法用于开服，可点击右侧按钮清理。";
  // 检查和重新下载（作者 11-28 要画的，做法照 v3.3.0-beta.1 下载与更新整改第四节）：打开这一页时把每一套都跑一次 java -version，
  // 看能不能起来、报出来的大版本对不对；检查不过的那一行标「无法启动」，MineDeck 下载的可以重新下载（先下到旁边、检查过再换上去，同一个路径，
  // 用着它的服务器设置不用改）。本机装的检查不过时新建服务器不用它，改用 MineDeck 下载的。左栏右上角「重新检查」再跑一次。
  const JAVA_BROKEN_WHY = "运行 java -version 失败：找不到 bin\\server\\jvm.dll（文件可能被杀毒软件删除或磁盘出错）"; // 编的，同详情页「启动与内存」
  const TIP_BAD_P = `检查未通过：${JAVA_BROKEN_WHY}\n可点击右侧按钮重新下载，用着它的服务器设置不用改`;
  const TIP_BAD_S = `检查未通过：${JAVA_BROKEN_WHY}\n新建服务器时不会使用此 Java，改用 MineDeck 下载的`;
  const TIP_ADD = "粘贴 java.exe 或 JDK 文件夹的路径，如 C:\\Program Files\\Java\\jdk-21\\bin\\java.exe";
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
    };
    if (mode === "spare") D.portable = [25, 21, 17, 16];
    if (mode === "empty") { D.portable = []; D.incomplete = []; D.scanned = []; }
  }
  // &java=bad 时用坏掉那套的服务器起不来，当它停着（不然「坏了还在跑」说不通）
  const usersOf = m => (C?.servers || []).filter(s => USES[s.id] === m).map(s => s.running && D?.badUse === s.id ? { ...s, running: false } : s);
  // 卸载会被后端拦下的原因（mc.go java.remove 的原话）：有开着的先说开着的，没有再说配着的
  function blockOf(m) {
    const us = usersOf(m); if (!us.length) return "";
    const run = us.filter(s => s.running);
    return run.length ? `服务器正在使用 Java ${m} 运行（${run.map(s => s.name).join("、")}），请先停止服务器再卸载`
      : `服务器正配置使用 Java ${m}（${us.map(s => s.name).join("、")}），请先在「设置 → 启动与内存」中更换后再卸载`;
  }

  // ================= 画 =================
  // 路径：后面 keep 段（Java 文件夹名、bin、java.exe）留全，前面的上级目录放不下时先省
  function pathHTML(p, keep) {
    const parts = p.split("\\"), n = parts.length, k = Math.min(keep, n);
    const hd = parts.slice(0, n - k).join("\\"), tl = parts.slice(n - k).join("\\");
    return `<span class="sj-path">${hd ? `<span class="hd">${esc(hd)}\\</span>` : ""}<span class="tl">${esc(tl)}</span></span>`;
  }
  const detailURL = s => {
    const q = new URLSearchParams({ kind: s.id, tab: "settings", sub: "jvm" });
    if (!s.running) q.set("st", "stopped");
    if (C.QS.get("theme")) q.set("theme", C.QS.get("theme"));
    return `detail.html?${q}`;
  };
  const svHTML = (s, m) => `<a class="sj-sv" href="${detailURL(s)}" title="${esc(`「${s.name}」${s.running ? "（运行中）" : ""}正在使用 Java ${m}，点击前往「设置 → 启动与内存」更换`)}">`
    + `${s.running ? "<i></i>" : ""}<span>${esc(s.name)}</span></a>`;
  // 名字后面的检查状态：检查中一个小转圈；检查不过一个红的「无法启动」（原因进悬停）；没问题什么都不加
  const chkHTML = (k, tip) => D.checking || D.redl[k] === "check" ? `<i data-ic="spinner" class="sj-chk"></i>`
    : D.bad.has(k) && !D.redl[k] ? `<span class="sj-tg bad" title="${esc(tip)}">无法启动</span>` : "";
  function pRow(m) {
    const k = "p" + m, us = usersOf(m), broke = D.bad.has(k), rd = D.redl[k];
    const why = rd ? "正在重新下载，完成后才能卸载" : blockOf(m);
    // 重新下载：一直露着；开着的服务器正用它跑时灰掉（文件被占着换不了）。坏了的那一套不会有服务器在用它跑
    const run = broke ? [] : us.filter(s => s.running);
    const rdWhy = run.length ? `服务器正在使用 Java ${m} 运行（${run.map(s => s.name).join("、")}），请先停止服务器再重新下载` : "";
    return `<div class="sj-row" data-sj-k="${k}"><div class="sj-main"><div class="sj-l1"><span class="sj-nm">Java ${m}</span>${chkHTML(k, TIP_BAD_P)}</div>`
      + (rd ? `<span class="sj-prog">${rd === "dl" ? "正在重新下载…" : "正在检查…"}</span>` : pathHTML(exeOf(m), 3)) + `</div>`
      + (us.length ? `<div class="sj-use">${us.map(s => svHTML(s, m)).join("")}</div>` : "")
      + `<button type="button" class="sj-ib sj-rd${broke && !rd ? " hot" : ""}" data-sj-act="redl" data-m="${m}" aria-label="重新下载便携 Java ${m}"`
      + (rd || rdWhy ? ` aria-disabled="true"` : "") + (rd ? "" : ` title="${esc(rdWhy || `重新下载 Java ${m}`)}"`) + `>`
      + `<i data-ic="${rd === "dl" ? "spinner" : "download"}"${rd === "dl" ? ' class="sj-chk"' : ""}></i></button>`
      + `<button type="button" class="sj-ib sj-rm" data-sj-act="rm" data-m="${m}" aria-label="卸载便携 Java ${m}"`
      + (why ? ` aria-disabled="true" title="${esc(why)}"` : ` title="卸载"`) + `><i data-ic="trash"></i></button></div>`;
  }
  const iRow = m => `<div class="sj-row inc" data-sj-k="i${m}"><div class="sj-main"><div class="sj-l1"><span class="sj-nm">Java ${m}</span><span class="sj-tg warn" title="${esc(TIP_INC)}">残留</span></div>`
    + `${pathHTML(`${RT}\\java-${m}`, 1)}</div><button type="button" class="sj-ib sj-rm" data-sj-act="clean" data-m="${m}" title="清理" aria-label="清理残留的便携 Java ${m}"><i data-ic="trash"></i></button></div>`;
  const sRow = j => `<div class="sj-row" data-sj-k="s${esc(j.path.toLowerCase())}"><div class="sj-main"><div class="sj-l1"><span class="sj-nm">Java ${j.major}</span>`
    + `<span class="sj-ver">${esc(j.version)}</span>${chkHTML("s" + j.path.toLowerCase(), TIP_BAD_S)}</div>${pathHTML(j.path, 3)}</div></div>`;
  const pRows = () => D.portable.map(pRow).join("") + D.incomplete.filter(m => !D.portable.includes(m)).map(iRow).join("");
  const sRows = () => D.scanned.map(sRow).join("");
  const pCount = () => D.portable.length + D.incomplete.filter(m => !D.portable.includes(m)).length;
  const scanBtn = () => `<button type="button" class="sj-corner${D.scanning ? " busy" : ""}" data-sj-act="scan" title="${D.scanning ? "扫描中…（约数秒）" : "重新扫描本机 Java"}" aria-label="重新扫描本机 Java"`
    + `${D.scanning ? ' aria-disabled="true"' : ""}><i data-ic="refresh"></i></button>`;

  const checkBtn = () => `<button type="button" class="sj-corner${D.checking ? " busy" : ""}" data-sj-act="check" title="${D.checking ? "检查中…" : "重新检查"}" aria-label="重新检查 MineDeck 下载的 Java"`
    + `${D.checking ? ' aria-disabled="true"' : ""}><i data-ic="refresh"></i></button>`;

  function html(ctx) {
    seed(ctx); C = ctx;
    const left = `<section class="sj-col" aria-label="MineDeck 下载的 Java"><div class="sj-hd fdpad"><span class="sj-t" title="${esc(TIP_P)}">MineDeck 下载的</span></div>${checkBtn()}`
      + `<div class="sj-lw${pCount() ? "" : " is-none"}" data-sj-lw="p"><div class="list">${pRows()}</div><span class="thumb"></span><div class="sj-none">暂无 MineDeck 下载的 Java</div></div></section>`;
    const right = `<section class="sj-col" aria-label="本机装的 Java"><div class="sj-hd fdpad"><span class="sj-t" title="${esc(TIP_S)}">本机装的</span></div>${scanBtn()}`
      + `<div class="sj-lw${D.scanned.length ? "" : " is-none"}" data-sj-lw="s"><div class="list">${sRows()}</div><span class="thumb"></span><div class="sj-none">未检测到</div></div>`
      + `<label class="sj-add${D.draft.trim() ? " has" : ""}" title="${esc(TIP_ADD)}"><input value="${esc(D.draft)}" placeholder="粘贴 java.exe 或 JDK 文件夹的路径" spellcheck="false" autocomplete="off" aria-label="手动添加 Java 路径">`
      + `<span class="sj-msg"></span><button type="button" class="sj-ib sj-plus" data-sj-act="add" title="添加" aria-label="添加 Java"><i data-ic="plus"></i></button></label></section>`;
    return `<div class="sj">${left}${right}</div>`;
  }

  // ================= 挂上去 =================
  const root = () => document.querySelector(".cp > .sj:not(.fade-out)");
  const lwOf = k => root()?.querySelector(`[data-sj-lw="${k}"]`);
  const rowEl = k => [...(root()?.querySelectorAll(".sj-row:not(.sj-out)") || [])].find(r => r.dataset.sjK === k);
  // 路径、版本放不下被截断时，全文进悬停
  function fitTips(scope) {
    scope.querySelectorAll(".sj-path").forEach(p => {
      const cut = [...p.children].some(x => x.scrollWidth > x.clientWidth + 1);
      setTip(p, cut ? p.textContent : "");
    });
    scope.querySelectorAll(".sj-ver").forEach(v => setTip(v, v.scrollWidth > v.clientWidth + 1 ? v.textContent : ""));
  }
  function mount(ctx) {
    C = ctx;
    const r = root(); if (!r) return;
    r.querySelectorAll(".sj-lw").forEach(w => ctx.thinBar(w));
    fitTips(r); document.fonts?.ready.then(() => r.isConnected && fitTips(r));
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
    const row = hy(list.lastElementChild); fitTips(row);
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
      list.innerHTML = html; hy(list); fitTips(list);
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
    const n = t.firstElementChild; row.replaceChildren(...n.childNodes); hy(row); fitTips(row);
  }
  // 重新下载（草图：下 1.6 秒、检查 0.6 秒；好了「无法启动」拿掉、那一行闪一下）
  function redownload(m) {
    const k = "p" + m; if (D.redl[k]) return;
    D.redl[k] = "dl"; paintRow(k, pRow(m));
    setTimeout(() => { D.redl[k] = "check"; paintRow(k, pRow(m)); }, 1600);
    setTimeout(() => { delete D.redl[k]; D.bad.delete(k); paintRow(k, pRow(m)); flash(k); }, 2200);
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

  // ---- 卸载、清理：危险确认框（程序 confirmModal 的原话；标题点名是哪一套，按钮写出后果，同共用件页） ----
  function askRemove(m, inc) {
    ask({
      title: inc ? `清理便携 Java ${m} 残留？` : `卸载便携 Java ${m}？`,
      body: inc ? `将删除未完整安装或卸载残留的便携 Java ${m} 文件夹，不影响系统已安装的 Java。`
        : `仅删除 MineDeck 下载的 <b>Java ${m}</b>，不影响系统已安装的 Java。`,
      ok: inc ? "清理" : `卸载 Java ${m}`,
      onOk: () => {
        if (inc) D.incomplete = D.incomplete.filter(x => x !== m); else D.portable = D.portable.filter(x => x !== m);
        dropRow((inc ? "i" : "p") + m);
      },
    });
  }
  let mask = null;
  function ask({ title, body, ok, onOk }) {
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "sj-mask enter" });
    mk.innerHTML = `<div class="sj-md" role="dialog" aria-modal="true"><div class="sj-mc"><div class="sj-mh"><span class="sj-mi"><i data-ic="trash"></i></span><h3>${esc(title)}</h3></div>`
      + `<div class="sj-mb">${body}</div></div><div class="sj-ma"><button type="button" class="sj-b fk" data-sj-x="no">取消</button><button type="button" class="sj-b dgr" data-sj-x="ok">${esc(ok)}</button></div></div>`;
    document.body.appendChild(hy(mk));
    const no = $("[data-sj-x=no]", mk); no.focus({ focusVisible: false });
    // 默认落在「取消」：鼠标点开时浏览器不画焦点框，回车却照样按到它，所以画一圈细框，一动别的就收（同共用件页）
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return;
      if (e.target.closest("[data-sj-x=ok]")) { closeAsk(); onOk(); return; }
      if (e.target.closest("[data-sj-x=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) closeAsk(); });

  // ================= 事件（绑在这一页的根上，换分类时跟着内容一起扔掉） =================
  function onClick(e) {
    const b = e.target.closest("[data-sj-act]"); if (!b) return;
    if (b.getAttribute("aria-disabled") === "true") return;
    const m = +b.dataset.m;
    switch (b.dataset.sjAct) {
      case "scan": scan(); break;
      case "check": check(); break;
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
