// 全局设置 →「更新与关于」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 settings-about.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 6796 行 setAbout）：页顶 Logo、「MineDeck」「Windows 本地 Minecraft 开服工具 · v3.2.9」；
// 「版本与更新」一组：当前版本（徽章 v3.2.9）、更新仓库（框 +「保存」+「检查更新」）、启动时静默检查更新（开关）；启动时查到新版本时一条「启动时查到新版本 vX [查看]」；
// 检查结果一行（已是最新 / 发现新版本 vX（正式版）+「立即更新并重启」）；「更新渠道」三行单选（跟随当前版本（默认）/ 稳定版 / 最新版，选了马上存，清掉上一次的结果）；
// 「关于」一段字（OpenFrp、字体许可、AGPL、Mojang 声明）；最底下「退出程序」+「停止服务器并退出」（确认框）。侧栏底部还有版本号和「退出 MineDeck」（U04：以后不要了，挪到这里）。
// 「立即更新并重启」在后端（ops/catalog/appops.go app.update.apply）是一步做完：有服务器开着就拒绝（「服务器「…」仍在运行，请先停止所有服务器再更新」），
// 再查一次、下载、校验、替换、重启，界面整页换成「正在更新，程序将自动重启…」；没有下载进度，也没有「下载好了等重启」这一步。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：左右两栏（圆角 18 = 26 − 8），左栏占其余、右栏 340 宽。
//   · 左栏：Logo 48、名字和一句「Windows 本地 Minecraft 开服工具」；下面一块 64 高、圆角 10 的版本块——版本号只在这里写一次（程序页顶、「当前版本」两处都写了），
//     下面一行是状态，右边是要做的那一步：检查更新 → 检查中 → 已是最新 / 发现新版本 vX（正式版）→ 下载更新 → 正在下载（块底一条细进度条和 MB 数）→
//     vX 已下载，重启后完成更新 →「重启并更新」。换状态只换那一行字和右边那颗键（逐项换），进度只改数和条。
//     再下面：更新渠道（40 高三段，各自的说明进悬停）、更新仓库（等宽框，框里一颗保存，同「通知与集成」）、启动时静默检查更新（开关在字的左边）。
//     最下面一颗「退出 MineDeck」（危险色浅底红字，同首页大卡的停止），点了是确认框，确认了整窗口一层「正在停止服务器并退出」（同共用件页的整页状态）。
//   · 右栏「关于」：程序那段字一条一块，原文照留（字体许可、AGPL、Mojang 声明是义务，不收进悬停）；外链只放图标（规范第十节），地址在悬停里。
// 任务单要的、程序没有的两种状态（要改后端，写进清单）：「正在下载」「下载好了等重启」。有服务器开着时点「重启并更新」：程序是拒绝，
// 这里改成一个确认框列出要停的服务器，确认了先停服再更新（同「退出」的做法）；要不要这样等作者定。左边分类上的小点留到装好新版本才去掉（程序是点过「查看」就去掉）。
// 示例数据：现在 v3.2.9 正式版（同 internal/app/app.go）；新版本 v3.3.0、38.6 MB 是草图编的；更新仓库是程序默认的 Trasperioooooon/MineDeck；静默检查开着；渠道没选过（跟随当前版本）。
// 草图专用：外壳的 &update=1 是启动时查到新版本；&up=downloading 一打开就在下载，&up=ready 一打开就是下载好了等重启。外链在草图里不跳转（悬停看地址），两份许可原文能打开。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }], { duration: 360, easing: SLIDE });
  const rise = (el, i = 0) => el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" });
  const setTip = (el, t) => { if (!el || (el.getAttribute("title") ?? el.dataset.tip ?? "") === (t || "")) return; if (t) el.title = t; else el.removeAttribute("title"); delete el.dataset.tip; };

  const CH = { // 同程序 UPDATE_CH
    auto: { l: "跟随当前版本", t: "跟随当前版本（默认）：当前为正式版时仅接收正式版更新，当前为测试版时同时接收测试版；切回正式版后自动恢复仅接收正式版。" },
    stable: { l: "稳定版", t: "稳定版（仅正式版）：仅提示正式发布版本。" },
    latest: { l: "最新版", t: "最新版（含测试版）：同时接收正式版与测试版更新，优先提示最新版本。测试版可能存在未知问题。" },
  };
  const NEW = "3.3.0", SIZE = 38.6; // 草图编的
  const REPO = "Trasperioooooon/MineDeck"; // 程序默认（internal/app/app.go DefaultUpdateRepo）
  let A = null, C = null;
  const effCh = () => A.ch || (/-/.test(A.ver) ? "latest" : "stable"); // 没选过：按正在用的版本定（同程序）
  const chName = c => ({ stable: "稳定版", latest: "最新版" })[c];
  function seed(ctx) {
    if (A) return;
    const up = ctx.QS.get("up"), has = ctx.update || up === "downloading" || up === "ready";
    A = { ver: "3.2.9", has, st: !has ? "idle" : up === "downloading" ? "downloading" : up === "ready" ? "ready" : "found",
      got: up === "downloading" ? 9.8 : 0, ch: "", silent: true, repo: REPO, draft: null };
    if (A.st === "downloading") runDl();
  }

  // ---- 版本块：一行状态、右边那一步 ----
  function stateHTML() {
    const head = `当前渠道（${chName(effCh())}）：`;
    switch (A.st) {
      case "checking": return `<span class="sab-sx"><i data-ic="spinner" class="sab-spin"></i>正在检查…</span>`;
      case "latest": return `<span class="sab-sx ok" title="${esc(`${head}已是最新版本（v${A.ver}，正式版）`)}"><i data-ic="check"></i>已是最新</span>`;
      case "found": return `<span class="sab-sx new" title="${esc(`${head}发现新版本 v${NEW}（正式版），当前版本 v${A.ver}`)}">发现新版本 v${NEW}（正式版）</span>`;
      case "downloading": return `<span class="sab-sx">正在下载 v${NEW}</span><span class="sab-mb"><b class="sab-got"></b> / ${SIZE} MB</span>`;
      case "ready": return `<span class="sab-sx new" title="下载好了，也校验过了；重启 MineDeck 时换上新版本">v${NEW} 已下载，重启后完成更新</span>`;
      default: return `<span class="sab-sx">正式版</span>`;
    }
  }
  function actHTML() {
    if (A.st === "found") return `<button type="button" class="sab-btn pri" data-sab="dl"><i data-ic="download"></i>下载更新</button>`;
    if (A.st === "ready") return `<button type="button" class="sab-btn pri" data-sab="restart"><i data-ic="refresh"></i>重启并更新</button>`;
    if (A.st === "downloading") return "";
    const busy = A.st === "checking";
    return `<button type="button" class="sab-btn" data-sab="check"${busy ? " disabled" : ""}><i data-ic="refresh"${busy ? ' class="sab-spin"' : ""}></i>检查更新</button>`;
  }
  // 逐项换：旧的往上 5 像素淡掉（0.09 秒），新的从下面 5 像素淡进来（0.22 秒、错开 22 毫秒）；连着变时上一次作废
  function swap(el, html, anim) {
    if (el._h === html) return; el._h = html;
    const tok = el._tok = (el._tok || 0) + 1;
    const show = () => { if (el._tok !== tok) return; el.getAnimations().forEach(x => x.cancel()); el.innerHTML = html; C.KIT.hydrate(el); paintGot(); if (anim) [...el.children].forEach(rise); };
    if (!anim || !el.children.length) { show(); return; }
    el.getAnimations({ subtree: true }).forEach(x => x.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = show;
  }
  function paintGot() {
    const r = root(); if (!r) return;
    const g = r.querySelector(".sab-got"); if (g) g.textContent = A.got.toFixed(1);
    const bar = r.querySelector(".sab-bar"); bar.classList.toggle("on", A.st === "downloading"); bar.firstElementChild.style.width = (A.got / SIZE * 100).toFixed(2) + "%";
  }
  function paintUp(anim = true) {
    const r = root(); if (!r) return;
    const v = r.querySelector(".sab-v"), t = "v" + A.ver; if (v.textContent !== t) v.textContent = t;
    swap(r.querySelector(".sab-s"), stateHTML(), anim); swap(r.querySelector(".sab-act"), actHTML(), anim); paintGot();
  }
  function runDl() {
    clearInterval(A.timer);
    A.timer = setInterval(() => {
      A.got = Math.min(SIZE, A.got + 1.3); paintGot();
      if (A.got >= SIZE) { clearInterval(A.timer); A.st = "ready"; paintUp(); }
    }, 120);
  }
  function check() {
    if (A.st === "checking") return;
    A.st = "checking"; paintUp();
    setTimeout(() => { A.st = A.has ? "found" : "latest"; paintUp(); }, 800); // 装作等了一下请求
  }

  // ---- 确认框（同共用件页的危险确认框）、整窗口一层（同共用件页的整页状态）：挂在 body 上 ----
  let mask = null;
  function ask({ ic, title, body, ok, onOk }) {
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "sab-mask enter" });
    mk.innerHTML = `<div class="sab-md" role="dialog" aria-modal="true"><div class="sab-mc"><div class="sab-mh"><span class="sab-mi"><i data-ic="${ic}"></i></span><h3>${title}</h3></div>`
      + `<div class="sab-mbd">${body}</div></div><div class="sab-ma"><button type="button" class="sab-b fk" data-x="no">取消</button><button type="button" class="sab-b dgr" data-x="ok">${ok}</button></div></div>`;
    document.body.appendChild(mk); C.KIT.hydrate(mk);
    const no = mk.querySelector("[data-x=no]"); no.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
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
  // 草图里装作等了一会儿就回来（真程序这时已经退出或重启了）
  function cover(t1, t2, spin, then) {
    const ov = Object.assign(document.createElement("div"), { className: "sab-ov enter" });
    ov.innerHTML = `<img src="../../internal/web/ui/img/logo-32.png" alt=""><div class="sab-o1">${t1}</div><div class="sab-o2">${spin ? `<i data-ic="spinner" class="sab-spin"></i>` : ""}${t2}</div>`;
    document.body.appendChild(ov); C.KIT.hydrate(ov);
    setTimeout(() => { then?.(); ov.classList.remove("enter"); ov.classList.add("leave"); setTimeout(() => ov.remove(), 220); }, 2200);
  }
  function restart() {
    const run = C.servers.filter(s => s.running);
    const go = () => cover("正在更新", "程序将自动重启", true, () => {
      A.ver = NEW; A.has = false; A.st = "latest"; paintUp(false);
      document.querySelector('#snav [data-sec="about"] .dot')?.remove(); // 装好了：左边分类上的小点去掉
    });
    if (!run.length) { go(); return; }
    ask({ ic: "arrowUp", title: "停止服务器并更新？", ok: "停止服务器并更新", onOk: go,
      body: `更新前需先停止正在运行的 ${run.length} 台服务器：${run.map(s => `<b>${esc(s.name)}</b>`).join("、")}。将先保存存档，随后重启 MineDeck 并更新至 v${NEW}。` });
  }
  const quit = () => ask({ ic: "power", title: "退出 MineDeck？", ok: "退出", body: "将先保存存档并停止正在运行的服务器，随后退出程序。",
    onOk: () => cover("正在停止服务器并退出", "可以关闭此页面", false) });

  // ---- 画 ----
  const segHTML = () => `<div class="sab-seg" role="radiogroup" aria-label="更新渠道"><i class="sab-sgl"></i>${["auto", "stable", "latest"].map(v => {
    const on = (A.ch || "auto") === v;
    return `<button type="button" role="radio" data-v="${v}" aria-checked="${on}" class="${on ? "on" : ""}" title="${esc(CH[v].t)}">${CH[v].l}</button>`;
  }).join("")}</div>`;
  const chTip = () => "手动检查与启动时检查均按所选渠道执行。" + (A.ch ? "" : `当前未指定渠道，已按当前版本自动设为「${chName(effCh())}」。`);
  const item = (body, link) => `<div class="sab-it"><p>${body}</p>${link ? `<button type="button" class="sab-lk" title="${link}" aria-label="打开 ${link}"><i data-ic="share"></i></button>` : ""}</div>`;
  function html(ctx) {
    C = ctx; seed(ctx);
    const dirty = A.draft !== null && A.draft !== A.repo;
    return `<div class="sab">`
      + `<section class="sab-col sab-main" aria-label="版本与更新">`
      + `<div class="sab-brand"><img src="../../internal/web/ui/img/logo-48.png" alt=""><div><div class="sab-name">MineDeck</div><div class="sab-tag">Windows 本地 Minecraft 开服工具</div></div></div>`
      + `<div class="sab-up"><div class="sab-txt"><b class="sab-v">v${A.ver}</b><div class="sab-s"></div></div><div class="sab-act"></div><span class="sab-bar"><i></i></span></div>`
      + `<div class="sab-f"><div class="sab-lb"><span class="sab-cht" title="${esc(chTip())}">更新渠道</span></div>${segHTML()}</div>`
      + `<div class="sab-f"><div class="sab-lb"><span title="从该 GitHub 仓库的 Releases 检查新版本（国内加速镜像兜底）">更新仓库</span></div>`
      + `<label class="sab-in${dirty ? " dirty" : ""}"><input value="${esc(A.draft ?? A.repo)}" placeholder="GitHub 仓库，如 yourname/MineDeck" spellcheck="false" autocomplete="off" aria-label="更新仓库">`
      + `<button type="button" class="sab-ib" data-sab="repo" title="保存"${dirty ? "" : " disabled"}><i data-ic="save"></i></button></label></div>`
      + `<button type="button" class="sab-swr${A.silent ? " on" : ""}" data-sab="silent" role="switch" aria-checked="${A.silent}" title="程序启动后将在后台按所选更新渠道检查一次新版本（需先填写更新仓库）">`
      + `<span class="sab-sw"></span><span>启动时静默检查更新</span></button>`
      + `<button type="button" class="sab-btn dgr sab-quit" data-sab="quit" title="先保存存档并停止正在运行的服务器，随后退出程序"><i data-ic="power"></i>退出 MineDeck</button>`
      + `</section>`
      + `<section class="sab-col sab-ab" aria-label="关于"><div class="sab-lb"><span>关于</span></div><div class="sab-lw"><div class="list">`
      + item("内网穿透基于 <b>OpenFrp OPENAPI</b> 接入", "https://www.openfrp.net")
      + item(`界面字体：Geist、Geist Mono（© The Geist Project Authors）与 Noto Sans SC（© Adobe），均为 SIL Open Font License 1.1，子集随程序内嵌，许可原文见 `
        + `<a class="sab-tx" href="../../internal/web/ui/fonts/OFL-Geist.txt" target="_blank">OFL-Geist.txt</a>、<a class="sab-tx" href="../../internal/web/ui/fonts/OFL-NotoSansSC.txt" target="_blank">OFL-NotoSansSC.txt</a>。`)
      + item("本软件以 GNU AGPL-3.0 开源，源码在 GitHub。", "https://github.com/Trasperioooooon/MineDeck")
      + `<div class="sab-it sab-dis"><p>Minecraft 是 Mojang Studios 的商标，本工具与 Mojang / Microsoft / 网易均无隶属或授权关系。</p>`
      + `<p class="en">NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT.</p></div>`
      + `</div><span class="thumb"></span></div></section></div>`;
  }
  const root = () => document.querySelector(".cp > .sab:not(.fade-out)");

  function placeSeg(box, animate) {
    const hl = box.querySelector(".sab-sgl"), on = box.querySelector("button.on"); if (!hl || !on || !box.offsetWidth) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function pickCh(b) {
    if (b.classList.contains("on")) return;
    const box = b.parentElement, v = b.dataset.v;
    box.querySelectorAll("button").forEach(x => { const on = x === b; x.classList.toggle("on", on); x.setAttribute("aria-checked", on); });
    placeSeg(box, true);
    A.ch = v === "auto" ? "" : v;
    setTip(root().querySelector(".sab-cht"), chTip());
    if (A.st === "latest" || A.st === "found") { A.st = "idle"; paintUp(); } // 同程序：换了渠道，上一次查出来的结果不再作数；在下载的、下载好的不动
  }
  function saveRepo() {
    const r = root(), box = r.querySelector(".sab-in"), inp = box.querySelector("input"), b = box.querySelector(".sab-ib");
    if (A.draft === null || A.draft === A.repo) return;
    A.repo = inp.value.trim(); A.draft = null; if (inp.value !== A.repo) inp.value = A.repo;
    box.classList.remove("dirty"); b.disabled = true;
    if (!b._done) { // 存好了：保存图标换成勾 1.2 秒（同「通知与集成」）
      b._done = true; const old = b.innerHTML; b.classList.add("done"); b.innerHTML = C.KIT.icon("check");
      setTimeout(() => { b.classList.remove("done"); b.innerHTML = old; b._done = false; }, 1200);
    }
  }
  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    ctx.thinBar(r.querySelector(".sab-lw"));
    placeSeg(r.querySelector(".sab-seg"), false);
    document.fonts?.ready.then(() => { if (r.isConnected) placeSeg(r.querySelector(".sab-seg"), false); });
    paintUp(false);
    r.addEventListener("click", e => {
      const sb = e.target.closest(".sab-seg button"); if (sb) { pickCh(sb); return; }
      const b = e.target.closest("[data-sab]"); if (!b || b.disabled) return;
      switch (b.dataset.sab) {
        case "check": check(); break;
        case "dl": A.st = "downloading"; A.got = 0; paintUp(); runDl(); break;
        case "restart": restart(); break;
        case "repo": saveRepo(); break;
        case "silent": A.silent = !A.silent; b.classList.toggle("on", A.silent); b.setAttribute("aria-checked", A.silent); break;
        case "quit": quit(); break;
      }
    });
    r.addEventListener("input", e => {
      if (!e.target.closest(".sab-in")) return;
      A.draft = e.target.value; const box = e.target.closest(".sab-in"), dirty = A.draft !== A.repo;
      box.classList.toggle("dirty", dirty); const b = box.querySelector(".sab-ib"); if (!b._done) b.disabled = !dirty;
    });
    r.addEventListener("keydown", e => { if (e.key === "Enter" && e.target.closest?.(".sab-in")) { e.preventDefault(); saveRepo(); } });
  }
  addEventListener("resize", () => { const r = root(); if (r) placeSeg(r.querySelector(".sab-seg"), false); });

  window.SETTINGS_SECS.about = { html, mount };
})();
