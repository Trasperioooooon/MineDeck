// 全局设置 →「下载与网络」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。
// 样式在 settings-download.css，选择器都带 sd 前缀。
//
// 程序现在的样子（internal/web/ui/app.js setDownload，约 5893 行）：两段。
//   · 下载源：标题「下载源」加一句「下载 Java 与服务端核心时的线路选择」，三行单选（自动（推荐）/ 仅国内镜像 / 仅官方源，每行一句说明，选中的打勾），点了马上存，弹「下载源已切换」。
//   · CurseForge API Key（选填）：一句说明加「免费申请」链接；一行「API Key」，下面一句（加密保存在这台电脑上……，解不开时换成黄字「密钥无法解开……请重新填写」），
//     密码框（占位「粘贴 API Key」，已设置时「已设置（留空不修改）」，存过的 Key 从不回显）、「保存」、设置过或解不开时多一颗「清除」（危险确认框）。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：一栏（圆角 18 = 26 − 8，内边距 16 / 20）占满面板，两项并排在栏的最上面（两列各 398 宽、列距 16，
// 照向导第三步「字 18 + 6 + 控件 40」）；这一页只有两项，下面空着，不为填满加字。
//   · 下载源：40 高分段（三段，同向导第三步），选中块滑过去；每段的说明进那一段的悬停。点了马上存（同程序），不弹提示，选中块滑过去就是结果。
//   · CurseForge API Key（选填）：名字后面一个外链图标（免费申请，草图里不真打开网页）；说明进名字的悬停。下面一个 40 高的框（密码框，不回显存过的 Key），
//     框里右边一颗保存（28 见方、离框边 6、圆角 4，框里没字时灰着；回车也能存，同详情页「游戏内 AI」的触发前缀），设置过或解不开时再多一颗清除（垃圾桶，危险确认框）。
//     存好以后框清空、占位字换成「已设置（留空不修改）」；解不开时名字那一行右边一句黄字「密钥无法解开」（会改变结果，留在页上；全文进悬停），框一圈黄边，填一个新的存下去就好了。
// 确认框照 detail-world.css 的 .wd-mask 那一套（共用件页的危险确认框；--scrim、--elev-4 外壳里没有，深浅两套定在这里）。
// 草图专用：&cf=set 已经设置过 Key，&cf=broken 存着的 Key 解不开；&src=mirror|official 一打开选的是别的下载源。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const setTip = (el, text) => {
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };

  // 同程序 setDownload 的三项和说明
  const OPTS = [
    { v: "auto", t: "自动（推荐）", d: "国内镜像（BMCLAPI / 清华）优先，失败自动切换官方源" },
    { v: "mirror", t: "仅国内镜像", d: "只用 BMCLAPI 和清华镜像。Paper 和 Purpur 没有国内镜像，这两项仍从官方下载。" },
    { v: "official", t: "仅官方源", d: "只用 Mojang / Forge / Adoptium 等官方源（海外网络适用）" },
  ];
  const CF_TIP = "导入 CurseForge 格式整合包、要下载里面的模组时用。Modrinth 格式不用填。\n加密保存在这台电脑上。换电脑或换 Windows 用户后需要重填";
  const BROKEN_TIP = "密钥无法解开，可能换了电脑或 Windows 用户，请重新填写";

  let C = null, D = null;
  function data() {
    if (D) return D;
    const q = C.QS.get("src"), cf = C.QS.get("cf");
    D = { src: OPTS.some(o => o.v === q) ? q : "auto", set: cf === "set", broken: cf === "broken", draft: "", saving: false };
    return D;
  }
  const root = () => document.querySelector(".cp > .sd:not(.fade-out)");
  const holder = d => (d.set ? "已设置（留空不修改）" : "粘贴 API Key"); // 同程序 cfKeyHolder

  // ---- 画 ----
  const segHTML = d => `<div class="sd-seg" role="radiogroup" aria-label="下载源"><i class="sd-sgl"></i>${OPTS.map(o =>
    `<button type="button" role="radio" data-src="${o.v}" aria-checked="${d.src === o.v}" class="${d.src === o.v ? "on" : ""}" title="${esc(o.d)}">${esc(o.t)}</button>`).join("")}</div>`;
  const saveOff = d => !d.draft.trim() || d.saving;
  function html(ctx) {
    C = ctx; const d = data();
    return `<div class="sd"><section class="sd-col" aria-label="下载与网络"><div class="sd-grid">`
      + `<div class="sd-f"><div class="sd-lb"><span title="下载 Java 与服务端核心时的线路选择">下载源</span></div>${segHTML(d)}</div>`
      + `<div class="sd-f"><div class="sd-lb"><span title="${esc(CF_TIP)}">CurseForge API Key（选填）</span>`
      + `<button type="button" class="sd-lk" data-sd="apply" title="免费申请（console.curseforge.com）" aria-label="免费申请 CurseForge API Key"><i data-ic="share"></i></button>`
      + `<span class="sd-sp"></span><span class="sd-warn"${d.broken ? "" : " hidden"} title="${esc(BROKEN_TIP)}"><i data-ic="warn"></i>密钥无法解开</span></div>`
      + `<label class="sd-iw${d.broken ? " warn" : ""}"><input class="sd-in" type="password" autocomplete="off" spellcheck="false" placeholder="${holder(d)}" value="${esc(d.draft)}" aria-label="CurseForge API Key">`
      + `<button type="button" class="sd-ib" data-sd="save" data-icn="${d.saving ? "spinner" : "save"}" aria-label="保存 API Key" aria-disabled="${saveOff(d)}" title="${saveOff(d) ? "先粘贴 API Key" : "保存"}"><i data-ic="${d.saving ? "spinner" : "save"}"${d.saving ? ' class="sd-spin"' : ""}></i></button>`
      + `<button type="button" class="sd-ib" data-sd="clear" title="清除" aria-label="清除 CurseForge API Key"${d.set || d.broken ? "" : " hidden"}><i data-ic="trash"></i></button></label></div>`
      + `</div></section></div>`;
  }

  // ---- 分段：选中块滑过去（同向导第三步） ----
  function placeSeg(r, animate) {
    const hl = r.querySelector(".sd-sgl"), on = r.querySelector(".sd-seg button.on"); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }

  // ---- 只换动了的那几样：保存键、占位字、黄字、清除键 ----
  function paintKey() {
    const r = root(); if (!r) return; const d = data();
    const inp = r.querySelector(".sd-in"), iw = r.querySelector(".sd-iw"), sv = r.querySelector('[data-sd="save"]'), cl = r.querySelector('[data-sd="clear"]'), w = r.querySelector(".sd-warn");
    if (inp.placeholder !== holder(d)) inp.placeholder = holder(d);
    if (inp.value !== d.draft) inp.value = d.draft;
    const off = saveOff(d);
    if (sv.getAttribute("aria-disabled") !== String(off)) sv.setAttribute("aria-disabled", off);
    setTip(sv, d.saving ? "" : off ? "先粘贴 API Key" : "保存");
    const ic = d.saving ? "spinner" : "save";
    if (sv.dataset.icn !== ic) { sv.dataset.icn = ic; sv.innerHTML = C.KIT.icon(ic, d.saving ? "sd-spin" : ""); }
    cl.hidden = !(d.set || d.broken); w.hidden = !d.broken; iw.classList.toggle("warn", d.broken);
  }
  // 存：草图里装作等了一下请求；只填了空白就什么都不存（同程序），保存键本来也灰着
  function saveKey() {
    const d = data(); if (saveOff(d)) return;
    d.saving = true; paintKey();
    setTimeout(() => { d.saving = false; d.set = true; d.broken = false; d.draft = ""; paintKey(); }, 450);
  }

  // ---- 确认框：同共用件页的危险确认框（照 detail-world.js 的 ask）。挂在 body 上 ----
  let mask = null;
  function ask({ title, body, ok, onOk }) {
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "sd-mask enter" });
    mk.innerHTML = `<div class="sd-md" role="dialog" aria-modal="true" aria-label="${esc(title)}"><div class="sd-mc"><div class="sd-mh"><span class="sd-mi"><i data-ic="trash"></i></span><h3>${esc(title)}</h3></div>`
      + `<div class="sd-mb">${body}</div></div><div class="sd-ma"><button type="button" class="sd-b fk" data-sdx="no">取消</button><button type="button" class="sd-b dgr" data-sdx="ok">${esc(ok)}</button></div></div>`;
    C.KIT.hydrate(mk); document.body.appendChild(mk);
    const no = mk.querySelector("[data-sdx=no]"); no.focus({ focusVisible: false });
    // 默认落在「取消」：鼠标点开时浏览器不画焦点框，回车却照样按到它，所以画一圈细框，一动别的就收（同共用件页）
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return;
      if (e.target.closest("[data-sdx=ok]")) { closeAsk(); onOk(); return; }
      if (e.target.closest("[data-sdx=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) closeAsk(); });

  // ---- 事件（绑在这一页的根上） ----
  function onClick(e) {
    const r = root(), d = data(); if (!r) return;
    const s = e.target.closest("[data-src]");
    if (s) {
      if (s.dataset.src === d.src) return;
      d.src = s.dataset.src; // 同程序：点了马上存
      r.querySelectorAll("[data-src]").forEach(b => { const on = b === s; b.classList.toggle("on", on); b.setAttribute("aria-checked", on); });
      placeSeg(r, true); return;
    }
    const b = e.target.closest("[data-sd]"); if (!b) return;
    if (b.closest(".sd-iw")) e.preventDefault(); // 框里的按钮：别让 label 把焦点挪回框里再点一次
    if (b.getAttribute("aria-disabled") === "true") return;
    switch (b.dataset.sd) {
      case "save": saveKey(); break;
      case "clear": ask({ title: "清除 CurseForge API Key？", ok: "清除", onOk: () => { d.set = false; d.broken = false; paintKey(); },
        body: "清除后，导入 CurseForge 格式的整合包时无法自动下载模组。需要时可以重新填写。" }); break;
      case "apply": break; // 草图里不打开外部网页
    }
  }
  function onInput(e) { if (!e.target.classList.contains("sd-in")) return; data().draft = e.target.value; paintKey(); }
  function onKey(e) { if (e.target.classList.contains("sd-in") && e.key === "Enter" && !e.isComposing) { e.preventDefault(); saveKey(); } }

  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    placeSeg(r, false);
    r.addEventListener("click", onClick);
    r.addEventListener("input", onInput);
    r.addEventListener("keydown", onKey);
    document.fonts?.ready.then(() => { if (r.isConnected) placeSeg(r, false); });
  }
  addEventListener("resize", () => { const r = root(); if (r) placeSeg(r, false); });

  window.SETTINGS_SECS.download = { html, mount };
})();
