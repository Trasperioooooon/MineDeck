// 服务器详情页 →「设置 → 全部配置」（11-27 第一稿，子代理照临时规范画的；计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 detail-settings-raw.css。
// 代理端的设置只有「启动与内存」一个子页，没有这一页（外壳不列）。
//
// 程序：internal/web/ui/app.js 的 view === "raw"（main 上约 4594 行）。一句说明「server.properties 的全部键值；已知项附中文名。」；
// 搜索框（键名 / 中文名，边打边筛，Esc 清空；空格、横线、下划线、点都不算，filterBySearch）和「共 N 项」「找到 N 项（共 N 项）」；
// 一张表：键名、中文名（props-catalog.js 里有的才有）、文本框；筛不到时「没有匹配的配置项：换个关键词，或者按 Esc 清空搜索。」；
// 吸底保存条「保存全部」：只提交改过的项（藏起来的也算），保存后「已保存 server.properties（重启服务器以后生效）」；
// 服务器开着时保存条上写「服务器运行中：保存后要重启才生效」，存过以后「已保存，重启服务器以后生效」加「立即重启」（wireLiveBar）。
// 配置文件还没生成时整页一句「配置文件暂未生成；首次启动服务器后，服务端会自动生成全部配置项。」
//
// 排法（内容面板 868 × 526，里面 852 × 510）：
//   · 最上面一排 32 高（同控制台的工具一排）：左边文件名「server.properties」和项数，右边搜索框（32 高胶囊，同控制台过滤框）。
//     程序那句说明不要了：文件名就在这一排；「已知项附中文名」一看就知道。搜索框的例子进悬停。
//   · 中间一栏（圆角 18）是全部键值，一行 40 高、圆角 10（离栏边 8）：键名（等宽）、中文名（淡色，说明和默认值进悬停）、值。
//     值是「平时是字、悬停出底色、点进去才出框」的框（规范第九节，同向导第三步的名称、介绍）：七十来行每行一个 40 高的框太重，
//     这一页又是给要手改原文的人用的，所以值照原文、不换成开关和下拉（那是「常用」的事）。空的值写淡淡的「空」。
//     改过的那一行右边一个强调色小点（同「定时任务」）。多了在栏里滚，细滚动条。
//   · 最下面一条 52 高、圆角 18 的保存条（同「定时任务」，规范里吸在面板底部的那种）：右边有改动时「N 项修改未保存」「放弃修改」，「保存」亮起来；
//     左边在服务器开着、又有改动时写「保存后要重启才生效」（警告色，页头已经写着运行中，前半句省了），存过以后换成「已保存，重启服务器以后生效」和「立即重启」。
//     这条在面板里，面板底边离窗口底 86，底部胶囊压不到它（计划书里「多字段表单的吸底保存条和底部胶囊怎么相处」，这里的答案）。
// 键名、中文名、说明、默认值都照程序的 props-catalog.js 现取（程序表单的唯一数据源），按这台服务器的 Minecraft 版本挑当时有的键，照服务端的写法按字母排；
// 值是各键的默认值，只把和别的标签对得上的几项改了：端口、最大人数同页头那台服务器；Paper、Forge 控制台里是离线模式（online-mode=false）；
// 白名单同「玩家」（Paper、Forge 开着，原版 26.2 文件里没写这一项）。草图专用：地址加 &raw=none 看配置文件还没生成的样子。
(() => {
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const setTip = (el, text) => { if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip; };

  // ---- 程序的中文目录：读 props-catalog.js 原文（它写的是 window.PROPS_CATALOG = [...]，这里给它一个自己的 window，不往全局放） ----
  let CAT = null;
  const CAT_READY = fetch("../../internal/web/ui/props-catalog.js").then(r => r.text())
    .then(src => { const w = {}; new Function("window", src)(w); CAT = w.PROPS_CATALOG || []; })
    .catch(e => { console.warn("没取到 props-catalog.js", e); CAT = []; });
  const ver = v => String(v).split(".").map(Number);
  const cmpVer = (a, b) => { const x = ver(a), y = ver(b); for (let i = 0; i < Math.max(x.length, y.length); i++) { const d = (x[i] || 0) - (y[i] || 0); if (d) return d; } return 0; };
  const inVer = (p, mc) => (!p.since || cmpVer(mc, p.since) >= 0) && (!p.removed || cmpVer(mc, p.removed) < 0);
  // 和别的标签对得上的几项（null = 文件里没有这一项）
  const OVERRIDE = { paper: { "online-mode": "false", "white-list": "true" }, forge: { "online-mode": "false", "white-list": "true" }, vanilla: { "white-list": null } };
  // 搜索同程序 filterBySearch：不分大小写，空格、横线、下划线、点都不算
  const skey = s => String(s || "").toLowerCase().replace(/[\s\-_.]+/g, "");

  // ---- 数据：每台服务器一份，换标签回来还在（改了没存的值、搜索词都在） ----
  let C = null;
  const DATA = {};
  function data() {
    const k = C.kind;
    if (!DATA[k] && CAT) {
      const vals = {};
      CAT.filter(p => inVer(p, C.S.mc)).forEach(p => { vals[p.key] = p.def; });
      Object.assign(vals, { "server-port": String(C.S.port), "max-players": String(C.S.max) }, OVERRIDE[k]);
      Object.keys(vals).forEach(x => { if (vals[x] === null) delete vals[x]; });
      DATA[k] = { keys: Object.keys(vals).sort(), base: vals, cur: { ...vals }, q: "", pending: false, saving: false, restarting: false };
    }
    return DATA[k];
  }
  const meta = key => CAT.find(p => p.key === key);
  const none = () => C.QS.get("raw") === "none"; // 草图专用：配置文件还没生成
  const dirtyKeys = D => D.keys.filter(k => D.cur[k] !== D.base[k]);
  const running = () => C.st !== "stopped";

  // ---- 画 ----
  function row(D, k) {
    const m = meta(k), tip = m ? `${m.desc}\n默认：${m.def === "" ? "空" : m.def}` : "";
    return `<div class="rw-row${D.cur[k] !== D.base[k] ? " mod" : ""}" data-k="${esc(k)}" data-search="${esc(k + " " + (m ? m.label : ""))}">`
      + `<span class="rw-k">${esc(k)}</span><span class="rw-cn"${tip ? ` title="${esc(tip)}"` : ""}>${m ? esc(m.label) : ""}</span>`
      + `<input class="rw-v" value="${esc(D.cur[k])}" placeholder="空" autocomplete="off" spellcheck="false" aria-label="${esc(k)}${m ? "（" + esc(m.label) + "）" : ""}">`
      + `<span class="rw-dot" title="未保存"></span></div>`;
  }
  const countText = (D, n) => (skey(D.q) ? `找到 ${n} 项` : `共 ${D.keys.length} 项`);
  function html(ctx) {
    C = ctx;
    const D = none() || !CAT ? null : data(), dis = !D ? " disabled" : "";
    const empty = D ? "" : none() ? `<div class="rw-empty" title="第一次启动服务器以后，服务端会生成全部配置项">配置文件还没生成</div>` : `<div class="rw-empty">正在读取…</div>`;
    const rows = D ? D.keys.map(k => row(D, k)).join("") : "";
    const n = D ? dirtyKeys(D).length : 0, on = n ? " on" : "";
    return `<div class="rw"><div class="rw-tb"><span class="rw-f">server.properties</span><span class="rw-c">${D ? countText(D, D.keys.length) : ""}</span><span class="rw-sp"></span>`
      + `<label class="rw-cs${D && D.q ? " has" : ""}${dis ? " off" : ""}" title="键名或中文名，如 rcon、视距；Esc 清空"><i data-ic="search"></i>`
      + `<input class="rw-q" placeholder="搜索配置项" value="${esc(D ? D.q : "")}" autocomplete="off" spellcheck="false" aria-label="搜索配置项"${dis}>`
      + `<button type="button" class="rw-cx" title="清空"><i data-ic="x"></i></button></label></div>`
      + `<section class="rw-box" aria-label="server.properties 的全部键值"><div class="rw-lw"><div class="list">${rows}</div><span class="thumb"></span>`
      + `<div class="rw-none">没有匹配的配置项</div>${empty}</div></section>`
      + `<div class="rw-bar"><span class="rw-note" data-mode=""></span><span class="rw-sp"></span><span class="rw-n${on}" aria-live="polite"><i></i><span>${n} 项修改未保存</span></span>`
      + `<button type="button" class="rw-discard${on}" data-rw="discard"${n ? "" : ' tabindex="-1"'}>放弃修改</button>`
      + `<button type="button" class="rw-save${on}" data-rw="save"${n ? "" : ` aria-disabled="true" title="${D ? "没有要保存的修改" : none() ? "配置文件还没生成" : "正在读取…"}"`}>保存</button></div></div>`;
  }

  // ---- 挂上去 ----
  const root = () => document.querySelector(".cp > .rw:not(.fade-out)");
  // 键名放不下被截断时，全名进悬停
  const fitKey = el => setTip(el, el.scrollWidth > el.clientWidth + 1 ? el.textContent : "");
  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    const lw = r.querySelector(".rw-lw");
    ctx.thinBar(lw);
    r.addEventListener("click", onClick);
    r.addEventListener("input", onInput);
    r.addEventListener("keydown", onKey);
    if (none()) return;
    if (!CAT) { // 目录比外壳晚到（很少见）：到了再把这一页画一次，只这一次
      CAT_READY.then(() => {
        if (!r.isConnected) return;
        const t = document.createElement("template"); t.innerHTML = html(C);
        const nr = t.content.firstElementChild; r.replaceWith(nr); C.KIT.hydrate(nr); mount(C);
      });
      return;
    }
    const D = data();
    filter(D, false); paintNote(D, true);
    r.querySelectorAll(".rw-k").forEach(fitKey);
    document.fonts?.ready.then(() => { if (r.isConnected) r.querySelectorAll(".rw-k").forEach(fitKey); });
  }

  // ---- 搜索：边打边筛（停手 0.18 秒再换）。只是把不匹配的行藏起来，改过的值都还在、保存时照样提交 ----
  // 换的样子同规范「卡片、行一批换」里没有左右之分的：旧的整块往上 5 像素淡掉（0.09 秒），新的从下面 5 像素一行一行淡进来（0.22 秒、错开 22 毫秒，只做看得见的）
  function filter(D, animate) {
    const r = root(); if (!r) return;
    const list = r.querySelector(".rw-lw .list"), lw = r.querySelector(".rw-lw"), k = skey(D.q), rows = [...list.children];
    const apply = () => {
      let n = 0;
      rows.forEach(x => { const hit = !k || skey(x.dataset.search).includes(k); x.hidden = !hit; if (hit) n++; });
      r.querySelector(".rw-c").textContent = countText(D, n);
      lw.classList.toggle("is-none", !n);
      lw._paint?.();
    };
    if (!animate) { apply(); return; }
    const tok = list._tok = (list._tok || 0) + 1;
    list.getAnimations({ subtree: true }).forEach(a => a.cancel());
    const out = list.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" });
    out.onfinish = () => {
      if (list._tok !== tok) return;
      apply(); list.scrollTop = 0; out.cancel();
      const bottom = list.getBoundingClientRect().bottom;
      rows.filter(x => !x.hidden && x.getBoundingClientRect().top < bottom).forEach((x, i) =>
        x.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
    };
  }

  // ---- 保存条：只换数字、两样淡进淡出、「保存」亮不亮；左边那句按状态逐项换 ----
  function paintBar(D) {
    const r = root(); if (!r) return;
    const n = dirtyKeys(D).length, on = n > 0, t = r.querySelector(".rw-n"), x = r.querySelector(".rw-discard"), s = r.querySelector(".rw-save");
    if (on) t.lastElementChild.textContent = `${n} 项修改未保存`; // 变回 0 时旧的数跟着淡掉
    t.classList.toggle("on", on); x.classList.toggle("on", on); x.tabIndex = on ? 0 : -1;
    if (!D.saving) {
      s.classList.toggle("on", on);
      if (on) { s.removeAttribute("aria-disabled"); setTip(s, ""); } else { s.setAttribute("aria-disabled", "true"); setTip(s, "没有要保存的修改"); }
    }
    paintNote(D);
  }
  // 左边那句：开着、有改动时「保存后要重启才生效」；存过、服务器还开着时「已保存，重启服务器以后生效」加「立即重启」（同程序 wireLiveBar）
  function noteMode(D) {
    if (D.pending && C.st === "running") return "pending";
    return running() && dirtyKeys(D).length ? "live" : "";
  }
  function noteHTML(D, mode) {
    if (mode === "live") return `<i data-ic="warn"></i><span>保存后要重启才生效</span>`;
    if (mode === "pending") return `<i data-ic="warn"></i><span>已保存，重启服务器以后生效</span>`
      + `<button type="button" class="rw-btn" data-rw="restart"${D.restarting ? " disabled" : ""}>${D.restarting ? `<i data-ic="spinner" class="rw-spin"></i>正在重启` : `<i data-ic="refresh"></i>立即重启`}</button>`;
    return "";
  }
  function paintNote(D, instant) {
    const el = root()?.querySelector(".rw-note"); if (!el) return;
    const mode = noteMode(D); if (el.dataset.mode === mode) return;
    el.dataset.mode = mode;
    const tok = el._tok = (el._tok || 0) + 1;
    const show = () => {
      if (el._tok !== tok) return;
      el.innerHTML = noteHTML(D, mode); C.KIT.hydrate(el);
      if (!instant) [...el.children].forEach((c, i) => c.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
    };
    if (instant || !el.children.length) { show(); return; }
    el.getAnimations({ subtree: true }).forEach(a => a.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN }).onfinish = show;
  }

  // ---- 做事：草图里不发请求，改这里的数据、换变了的那几处 ----
  function save(D) {
    if (D.saving || !dirtyKeys(D).length) return;
    D.saving = true;
    const s = root().querySelector(".rw-save"); s.innerHTML = `${C.KIT.icon("spinner", "rw-spin")}保存`;
    setTimeout(() => { // 草图里装作等了一下请求；只提交改过的项（同程序）
      const changed = dirtyKeys(D);
      D.saving = false; D.base = { ...D.cur };
      if (C.st === "running") D.pending = true; // 运行中不会重读 server.properties，改了哪一项都要重启
      const r = root(); if (!r) return;
      r.querySelector(".rw-save").textContent = "保存";
      changed.forEach(k => r.querySelector(`.rw-row[data-k="${CSS.escape(k)}"]`)?.classList.remove("mod"));
      paintBar(D);
    }, 450);
  }
  function discard(D) {
    const r = root(); if (!r) return;
    dirtyKeys(D).forEach(k => {
      D.cur[k] = D.base[k];
      const el = r.querySelector(`.rw-row[data-k="${CSS.escape(k)}"]`); if (!el) return;
      el.querySelector(".rw-v").value = D.base[k]; el.classList.remove("mod");
    });
    paintBar(D);
  }
  function restart(D) { // 草图里装作重启了一下：好了以后那句淡掉
    if (D.restarting) return;
    D.restarting = true; const el = root()?.querySelector(".rw-note"); if (el) { el.innerHTML = noteHTML(D, "pending"); C.KIT.hydrate(el); }
    setTimeout(() => { D.restarting = false; D.pending = false; paintNote(D); }, 1500);
  }
  function clearQ(D, inp) { D.q = ""; inp.value = ""; inp.closest(".rw-cs").classList.remove("has"); clearTimeout(qT); filter(D, true); }

  // ---- 事件（绑在这一页的根上，换标签时跟着内容一起扔掉） ----
  let qT = 0;
  function onClick(e) {
    const D = !none() && CAT ? data() : null; if (!D) return;
    if (e.target.closest(".rw-cx")) { e.preventDefault(); const inp = root().querySelector(".rw-q"); clearQ(D, inp); inp.focus(); return; }
    const b = e.target.closest("[data-rw]"); if (!b || b.getAttribute("aria-disabled") === "true" || b.disabled) return;
    if (b.dataset.rw === "save") save(D);
    else if (b.dataset.rw === "discard") discard(D);
    else if (b.dataset.rw === "restart") restart(D);
  }
  function onInput(e) {
    const D = !none() && CAT ? data() : null; if (!D) return;
    if (e.target.classList.contains("rw-q")) {
      D.q = e.target.value; e.target.closest(".rw-cs").classList.toggle("has", !!D.q);
      clearTimeout(qT); qT = setTimeout(() => filter(D, true), 180); return;
    }
    const rowEl = e.target.closest(".rw-row"); if (!rowEl || !e.target.classList.contains("rw-v")) return;
    const k = rowEl.dataset.k; D.cur[k] = e.target.value;
    rowEl.classList.toggle("mod", D.cur[k] !== D.base[k]);
    paintBar(D);
  }
  function onKey(e) {
    const D = !none() && CAT ? data() : null; if (!D) return;
    if (e.target.classList.contains("rw-q") && e.key === "Escape" && e.target.value) { e.preventDefault(); e.stopPropagation(); clearQ(D, e.target); return; }
    if (e.target.classList.contains("rw-v") && e.key === "Enter") { e.preventDefault(); e.target.blur(); } // 回车只是改完这一格；保存在下面
  }

  window.DETAIL_TABS["settings/raw"] = { html, mount };
})();
