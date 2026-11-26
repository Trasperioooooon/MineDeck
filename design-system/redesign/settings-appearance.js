// 全局设置 →「外观」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。
// 样式在 settings-appearance.css，选择器、图标名都带 sa 前缀。
//
// 程序现在的样子（internal/web/ui/app.js setAppearance，约 5830 行；Appearance 约 636 行）：三段标题加一句说明——主题模式（跟随系统 / 浅色 / 深色的分段）、
// 配色方案（七张卡，预览区用那个方案自己的令牌画缩略界面，下面名字和一句说明）、强调色（「跟随方案」加八个色块，选中的打勾）；
// 最底下一句「想换背景图？在程序所在的文件夹里新建 bg 文件夹并放入图片，下次打开时随机选一张作为壁纸」。点了马上生效，存在本机，没有保存键。
// 侧栏底部还有一颗主题切换（U04：以后不要了，只留这里）。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：左右两栏（圆角 18 = 26 − 8，内边距 16 / 20），都从上往下排，照向导第三步「字 18 + 6 + 控件」，块和块隔 24：
//   · 左栏（852 − 8 − 300 = 544 宽）：
//       主题模式：40 高分段（图标加字），选中块滑过去；点了调 ctx.setTheme，整页跟着换。「跟随系统」先按系统的明暗算出深浅再传，系统换了跟着换。
//       配色方案：七张卡，一排四张，卡上半是那个方案的缩略界面（照重构以后的样子画：页面底、一块面板、底部胶囊，用那个方案自己的令牌，跟着现在的深浅和强调色），
//         下面是名字；选中框是单独的一块，在格子里横着、竖着滑过去（卡片本身没有底色，不用叠三层）。说明（「暖沙纸底 + 草绿（默认）」这些）进悬停。
//         点了整页换方案（同程序「点一下，整个界面都会换」），地址里的 scheme 跟着改，刷新也还是它。
//       强调色：「跟随方案」胶囊加八个 28 的圆（颜色照程序 ACCENTS），选中的那一个外面一圈强调色细环（离它 2、环宽 2，和胶囊同心），滑过去；程序是色块里打勾，这里只有环。
//         点了整页换强调色，地址里的 accent 跟着改。
//   · 右栏（300 宽）：
//       背景图（U08）：一块 16:10 的虚线占位框，里面图标和「选择背景图…」，整块点了就是选图（草图里不弹选文件的窗口，假装选了一张）；
//         选了以后框里是那张图（草图里是 CSS 画的一张替身，不是真图），右上角一个 × 移除（28 见方、圆角 4，离框边 6，同框里的按钮），再点图是换一张；文件名进悬停。
//         换的时候框里的东西逐项换（旧的往上 5 像素淡掉 0.09 秒，新的从下面 5 像素淡进来 0.22 秒）。草图里不把图铺到窗口上（没有真图）。
//       下面三个开关（36 × 20，在字的左边，说明进悬停）：「首页的爱心跳动」「首页显示『点击赞赏』标签」放在一起（11-26 作者要的，都默认开），
//         一道细线，「在底部导航显示 AI 助手」（11-26 作者：AI 改成是否呈现的选项、在设置里；关了调 ctx.setCapAI(false)，胶囊里那颗没了）。
// 都是点了马上生效，同程序，没有保存条，也不弹提示（变的就是整页的样子）。换外观的那一下整页先停掉过渡、两帧后恢复（同程序 Appearance.set 的 no-transition），
// 免得各处按自己的过渡时长慢慢变色；滑动的选中块不停。
// 换分类回来，选的都还在（数据在这个文件里）；主题、方案、强调色、胶囊里的 AI 助手本来就挂在外壳上。
// 草图专用：&bg=1 一打开就是选过背景图的样子。
(() => {
  // 程序图标里没有「图片」：照 16 格、1.3 粗画的一张（框、太阳、山）
  Object.assign(window.EXTRA_ICONS = window.EXTRA_ICONS || {}, {
    saImage: '<rect x="2.2" y="3" width="11.6" height="10" rx="1.8"/><circle cx="10.4" cy="6.2" r="1.1"/><path d="M2.6 11.4l3.3-3.3 2.5 2.4 1.5-1.4 3.4 3.1"/>',
  });
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const setTip = (el, text) => { // kit.js 悬停时把 title 挪进 data-tip，两处都看
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  const rise = (el, i = 0) => el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" });

  // 同程序（app.js 约 594 行）
  const SCHEMES = [
    { id: "craft", name: "方块", desc: "暖沙纸底 + 草绿（默认）" },
    { id: "fluent", name: "Fluent", desc: "Windows 11 风格" },
    { id: "github", name: "GitHub", desc: "Primer 设计系统" },
    { id: "graphite", name: "石墨", desc: "中性灰阶" },
    { id: "nord", name: "Nord", desc: "北极冷色调" },
    { id: "catppuccin", name: "Catppuccin", desc: "柔和粉彩" },
    { id: "solarized", name: "Solarized", desc: "经典护眼配色" },
  ];
  const ACCENTS = [
    { id: "blue", name: "蓝", sw: "#2563eb" }, { id: "indigo", name: "靛蓝", sw: "#4f46e5" },
    { id: "violet", name: "紫", sw: "#7c3aed" }, { id: "teal", name: "青", sw: "#0f766e" },
    { id: "green", name: "绿", sw: "#15803d" }, { id: "orange", name: "橙", sw: "#c2410c" },
    { id: "rose", name: "玫红", sw: "#e11d48" }, { id: "graphite", name: "石墨", sw: "#3f3f46" },
  ];
  const MODES = [
    { v: "auto", t: "跟随系统", ic: "monitor", tip: "随 Windows 的浅色 / 深色设置自动切换" },
    { v: "light", t: "浅色", ic: "sun" },
    { v: "dark", t: "深色", ic: "moon" },
  ];
  const TOGGLES = [
    { k: "heart", t: "首页的爱心跳动", tip: "首页字标右边那颗像素心每 4.2 秒跳一下（悬停时不跳）；关掉以后心还在，只是不跳" },
    { k: "label", t: "首页显示「点击赞赏」标签", tip: "心右上方那个「点击赞赏」标签；点标签上的 × 就等于关掉这里，想要回来在这里打开" },
    { k: "ai", t: "在底部导航显示 AI 助手", tip: "底部导航里 AI 助手那一颗；关掉以后不显示，在这里打开" },
  ];
  const BG_NAME = "雪山.jpg"; // 草图里假装选的那一张

  const html0 = document.documentElement;
  const sysLight = () => matchMedia("(prefers-color-scheme: light)").matches;
  let C = null, A = null;
  function data() {
    if (A) return A;
    const d = html0.dataset;
    A = { mode: d.theme === "light" ? "light" : "dark", scheme: d.scheme || "nord", accent: d.accent || "auto", bg: C.QS.get("bg") === "1", heart: true, label: true, ai: true };
    return A;
  }
  const root = () => document.querySelector(".cp > .sa:not(.fade-out)");

  // ---- 改外壳：主题、方案、强调色挂在 <html> 上 ----
  // 整页一次换色：先停掉过渡，两帧后恢复（同程序 Appearance.set）；滑动的选中块（.sa-glide）不停
  function swapNow() { html0.classList.add("sa-swap"); requestAnimationFrame(() => requestAnimationFrame(() => html0.classList.remove("sa-swap"))); }
  const themeOf = m => (m === "auto" ? (sysLight() ? "light" : "dark") : m);
  function setQ(k, v, def) { const q = new URLSearchParams(location.search); if (v === def) q.delete(k); else q.set(k, v); history.replaceState(null, "", "?" + q); }
  function paintPreviews(k, v) { root()?.querySelectorAll(".sa-pv").forEach(p => { if (p.dataset[k] !== v) p.dataset[k] = v; }); }
  function applyTheme() {
    const t = themeOf(A.mode);
    if (html0.dataset.theme !== t) { swapNow(); C.setTheme(t); }
    paintPreviews("theme", t);
  }
  // 方案、强调色：外壳的 setTheme 只认深浅，这两样直接改 <html> 和地址；再按原来的深浅调一次 setTheme，让左上角草图切换条的链接带上新的地址
  function applyScheme() { swapNow(); html0.dataset.scheme = A.scheme; setQ("scheme", A.scheme, "nord"); C.setTheme(html0.dataset.theme); }
  function applyAccent() { swapNow(); html0.dataset.accent = A.accent; setQ("accent", A.accent, "auto"); C.setTheme(html0.dataset.theme); paintPreviews("accent", A.accent); }
  // 「跟随系统」时系统换了明暗，跟着换（同程序）
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => { if (A && C && A.mode === "auto") applyTheme(); });

  // ---- 画 ----
  const segHTML = D => `<div class="sa-seg" role="radiogroup" aria-label="主题模式"><i class="sa-sgl sa-glide"></i>${MODES.map(m =>
    `<button type="button" role="radio" data-mode="${m.v}" aria-checked="${D.mode === m.v}" class="${D.mode === m.v ? "on" : ""}"${m.tip ? ` title="${m.tip}"` : ""}><i data-ic="${m.ic}"></i>${m.t}</button>`).join("")}</div>`;
  // 缩略界面：页面底、一块面板（两行字、一颗强调色按钮）、底部胶囊（最右一颗实心强调色，同「新建」）
  const pvHTML = (s, t, acc) => `<span class="sa-pv" data-scheme="${s.id}" data-theme="${t}" data-accent="${acc}" aria-hidden="true">`
    + `<span class="sa-pp"><span class="sa-pl"></span><span class="sa-pl s"></span><span class="sa-pb"></span></span><span class="sa-pc"><span></span><span></span><span></span></span></span>`;
  const cardsHTML = (D, t) => `<div class="sa-schemes" role="radiogroup" aria-label="配色方案"><i class="sa-shl sa-glide"></i>${SCHEMES.map(s =>
    `<button type="button" role="radio" class="sa-sc${D.scheme === s.id ? " on" : ""}" data-pick="${s.id}" aria-checked="${D.scheme === s.id}" title="${esc(s.desc)}">`
    + `${pvHTML(s, t, D.accent)}<span class="sa-sn">${esc(s.name)}</span></button>`).join("")}</div>`;
  const accHTML = D => `<div class="sa-acc" role="radiogroup" aria-label="强调色"><i class="sa-ring sa-glide"></i>`
    + `<button type="button" role="radio" class="sa-auto${D.accent === "auto" ? " on" : ""}" data-acc="auto" aria-checked="${D.accent === "auto"}">跟随方案</button>`
    + ACCENTS.map(c => `<button type="button" role="radio" class="sa-sw${D.accent === c.id ? " on" : ""}" data-acc="${c.id}" style="--sw:${c.sw}" title="${c.name}" aria-label="${c.name}" aria-checked="${D.accent === c.id}"></button>`).join("")
    + `</div>`;
  const bgInner = D => (D.bg
    ? `<span class="sa-bgi"></span><button type="button" class="sa-bgx" data-sa="bgx" title="移除背景图" aria-label="移除背景图"><i data-ic="x"></i></button>`
    : `<span class="sa-bge"><i data-ic="saImage"></i><span class="sa-bgb">选择背景图…</span></span>`);
  const bgTip = D => (D.bg ? `${BG_NAME}\n点一下换一张` : "");
  const tgHTML = D => `<div class="sa-tgs">${TOGGLES.map((g, i) => (i === 2 ? `<i class="sa-hr"></i>` : "")
    + `<button type="button" class="sa-tg${D[g.k] ? " on" : ""}" data-tg="${g.k}" role="switch" aria-checked="${D[g.k]}" title="${esc(g.tip)}"><span class="sa-swc"></span><span class="sa-tl">${esc(g.t)}</span></button>`).join("")}</div>`;

  function html(ctx) {
    C = ctx; const D = data(), t = html0.dataset.theme;
    return `<div class="sa"><section class="sa-col" aria-label="主题与配色">`
      + `<div class="sa-f"><div class="sa-lb"><span>主题模式</span></div>${segHTML(D)}</div>`
      + `<div class="sa-f"><div class="sa-lb"><span title="点一下，整个界面都会换">配色方案</span></div>${cardsHTML(D, t)}</div>`
      + `<div class="sa-f"><div class="sa-lb"><span title="按钮、选中项与链接使用的颜色；默认跟随配色方案">强调色</span></div>${accHTML(D)}</div></section>`
      + `<section class="sa-col sa-side" aria-label="背景图与首页">`
      + `<div class="sa-f"><div class="sa-lb"><span>背景图</span></div><div class="sa-bg${D.bg ? "" : " none"}" role="button" tabindex="0" data-sa="bg"`
      + ` aria-label="${D.bg ? "更换背景图…" : "选择背景图…"}"${D.bg ? ` title="${esc(bgTip(D))}"` : ""}>${bgInner(D)}</div></div>`
      + `${tgHTML(D)}</section></div>`;
  }

  // ---- 选中框滑过去：位置和宽（高）一起变，0.32 秒（规范第七节）；不动画时直接放好 ----
  function glide(hl, x, y, w, h, animate) {
    if (!hl) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translate(${x}px, ${y}px)`;
    if (w != null) hl.style.width = w + "px";
    if (h != null) hl.style.height = h + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  const placeSeg = (r, a) => { const b = r.querySelector(".sa-seg button.on"); if (b) glide(r.querySelector(".sa-sgl"), b.offsetLeft, 0, b.offsetWidth, null, a); };
  const placeCard = (r, a) => { const b = r.querySelector(".sa-sc.on"); if (b) glide(r.querySelector(".sa-shl"), b.offsetLeft, b.offsetTop, b.offsetWidth, b.offsetHeight, a); };
  const placeRing = (r, a) => { const b = r.querySelector(".sa-acc .on"); if (b) glide(r.querySelector(".sa-ring"), b.offsetLeft - 4, 0, b.offsetWidth + 8, null, a); };
  const placeAll = (r, a) => { placeSeg(r, a); placeCard(r, a); placeRing(r, a); };
  const pick = (els, on) => els.forEach(x => { const y = on(x); x.classList.toggle("on", y); x.setAttribute("aria-checked", y); });

  // 背景图：框里的东西逐项换（旧的往上淡掉，新的从下面淡进来）；连着点时上一次作废
  function swapBg(r) {
    const D = data(), box = r.querySelector(".sa-bg"), tok = box._tok = (box._tok || 0) + 1;
    const show = () => {
      if (box._tok !== tok) return;
      box.classList.toggle("none", !D.bg); box.innerHTML = bgInner(D); C.KIT.hydrate(box);
      setTip(box, bgTip(D)); box.setAttribute("aria-label", D.bg ? "更换背景图…" : "选择背景图…");
      [...box.children].forEach((k, i) => rise(k, i));
    };
    box.getAnimations({ subtree: true }).forEach(a => a.cancel());
    const kids = [...box.children]; if (!kids.length) { show(); return; }
    kids.forEach((k, i) => {
      const an = k.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" });
      if (i === kids.length - 1) an.onfinish = show;
    });
  }

  function onClick(e) {
    const D = data(), r = root(); if (!r) return;
    const m = e.target.closest("[data-mode]");
    if (m) {
      if (m.dataset.mode === D.mode) return;
      D.mode = m.dataset.mode; pick(r.querySelectorAll("[data-mode]"), x => x.dataset.mode === D.mode); placeSeg(r, true); applyTheme(); return;
    }
    const s = e.target.closest("[data-pick]");
    if (s) {
      if (s.dataset.pick === D.scheme) return;
      D.scheme = s.dataset.pick; pick(r.querySelectorAll(".sa-sc"), x => x.dataset.pick === D.scheme); placeCard(r, true); applyScheme(); return;
    }
    const a = e.target.closest("[data-acc]");
    if (a) {
      if (a.dataset.acc === D.accent) return;
      D.accent = a.dataset.acc; pick(r.querySelectorAll("[data-acc]"), x => x.dataset.acc === D.accent); placeRing(r, true); applyAccent(); return;
    }
    const tg = e.target.closest("[data-tg]");
    if (tg) {
      const k = tg.dataset.tg; D[k] = !D[k];
      tg.classList.toggle("on", D[k]); tg.setAttribute("aria-checked", D[k]);
      if (k === "ai") C.setCapAI(D.ai);
      return;
    }
    if (e.target.closest("[data-sa=bgx]")) { D.bg = false; swapBg(r); return; }
    if (e.target.closest("[data-sa=bg]")) { D.bg = true; swapBg(r); } // 草图里不弹选文件的窗口：假装选了一张（已经有图时就是换一张，样子不变）
  }
  function onKey(e) {
    if ((e.key === "Enter" || e.key === " ") && e.target.matches?.(".sa-bg")) { e.preventDefault(); e.target.click(); }
  }

  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    placeAll(r, false);
    r.addEventListener("click", onClick);
    r.addEventListener("keydown", onKey);
    document.fonts?.ready.then(() => { if (r.isConnected) placeAll(r, false); });
  }
  addEventListener("resize", () => { const r = root(); if (r) placeAll(r, false); });

  window.SETTINGS_SECS.appearance = { html, mount };
})();
