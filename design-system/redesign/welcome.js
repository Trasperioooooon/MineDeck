// 第一次打开：引导和用户协议（11-27 夜里第一稿）。页面结构、草图参数、和程序不一样的地方写在 welcome.html 开头的注释里；样式在 welcome.css（.wl- 前缀）。
// 做法：状态都在 S 里；拨开关、换下载源、点灰着的主键只改变了的那一处（开关的类、主键的 aria-disabled 和悬停、选中块的位置），不重写面板；
// 只有「分两步」换步时整页按向导的换步动画换一遍（往走的方向挪 16 像素淡掉 0.14 秒，新的一步从另一边进来 0.26 秒）。
(() => {
  const QS = new URLSearchParams(location.search);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  // 改悬停提示：kit.js 悬停时把 title 挪进 data-tip，所以两处都要看、都要清
  const setTip = (el, text) => {
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title");
    delete el.dataset.tip;
  };
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";

  // ---- 程序原文（app.js showOnboarding 约 6913–6923 行），一字不改，只把引言包进 <p>、去掉缩进 ----
  const TERMS = `<p>MineDeck 是开源、免费的 Minecraft Java 版开服辅助工具。它只从 Mojang、PaperMC 等官方公开渠道下载服务端，并在你的电脑上配好运行环境。<b>不包含、也不分发游戏客户端，不修改游戏本身，也不破解正版验证</b>。</p>`
    + `<ol>`
    + `<li><b>非官方产品</b>：本工具为独立开源项目（由随迹提供维护支持），与 Mojang Studios、Microsoft 及网易（中国版《我的世界》运营方）均无任何隶属、合作或授权关系。NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT. "Minecraft" 及相关商标归 Mojang / Microsoft 所有。</li>`
    + `<li><b>遵守官方条款</b>：使用本工具搭建与运行服务器，须遵守 <b>Minecraft EULA</b> 与 Microsoft 服务协议；如你使用网易代理的中国版《我的世界》账号或内容，请另行遵守网易的用户协议（两套账号体系互不相通，本工具仅面向国际 Java 版服务端）。</li>`
    + `<li><b>正版建议</b>：建议使用正版 Mojang / Microsoft 账号并开启正版验证联机。是否使用离线模式由你自行决定并自行承担相应后果，本工具不提供任何绕过正版验证的功能。</li>`
    + `<li><b>对外服务责任自负</b>：通过内网穿透等方式向他人开放服务器时，你是该服务器的运营者，须自行遵守所在地法律法规（含网络安全与未成年人保护相关规定），并对服务器内容与运营行为负责。</li>`
    + `<li><b>开源免责</b>：本软件按 GNU AGPL-3.0 协议以「现状」提供，不作任何明示或默示担保。因使用或无法使用本软件导致的任何数据丢失、账号问题或其他损失，作者及贡献者不承担责任。</li>`
    + `</ol>`;
  // 同意那句（原文，链接照程序指向 aka.ms/MinecraftEULA；草图里点了不打开网页）
  const AGREE = `我已阅读并同意上述《用户协议与免责声明》及 <a href="https://aka.ms/MinecraftEULA" target="_blank" rel="noopener noreferrer" title="在浏览器里打开 aka.ms/MinecraftEULA">Minecraft EULA</a>`;
  // 下载源：三段的字和说明同「设置 → 下载与网络」（settings-download.js，照程序 setDownload）；程序引导卡里那三句说明是另一种写法，见返回的清单
  const OPTS = [
    { v: "auto", t: "自动（推荐）", d: "国内镜像（BMCLAPI / 清华）优先，失败自动切换官方源" },
    { v: "mirror", t: "仅国内镜像", d: "只用 BMCLAPI 和清华镜像。Paper 和 Purpur 没有国内镜像，这两项仍从官方下载。" },
    { v: "official", t: "仅官方源", d: "只用 Mojang / Forge / Adoptium 等官方源（海外网络适用）" },
  ];
  const SRC_TIP = "下载 Java 与服务端核心时的线路选择\n之后可在「设置」里随时更改";
  // 存放位置：示例路径放在 C 盘（「设置 → 存储位置」草图用的是 D:\MineDeck，可那句说明说「想放到 D 盘……」，放 D 盘读着别扭）；
  // 说明是程序原话，「左下角软件」那个位置已经没有了，改指底部导航
  const PATH = { hd: "C:\\MineDeck\\", tl: "servers" };
  const PATH_TIP = "每台服务器一个子文件夹。想放到 D 盘等大容量分区，可到底部导航的「设置 → 存储位置」更改。";
  // 保存失败时「详情」里的原文：编的（程序里是后端写 config.json 时报的错）
  const RAW = "open C:\\MineDeck\\config.json: Access is denied.";

  // ---- 状态 ----
  const S = {
    flow: QS.get("flow") === "steps" ? "steps" : "one",
    old: QS.get("user") === "old",
    agree: QS.get("agree") === "1",
    src: OPTS.some(o => o.v === QS.get("src")) ? QS.get("src") : "auto",
    fail: QS.get("save") === "fail",
    step: 0, saving: false, leaving: false,
  };
  // 有哪几步：老用户只补协议；新用户一页放完（all），或者分两步（协议 terms → 下载源和存放位置 setup）
  const STEPS = () => (S.old ? ["terms"] : S.flow === "steps" ? ["terms", "setup"] : ["all"]);
  if (QS.get("step") === "2" && STEPS().length > 1) { S.step = 1; S.agree = true; }
  const kind = () => STEPS()[S.step];
  const isLast = () => S.step === STEPS().length - 1;
  const needAgree = () => kind() !== "setup" && !S.agree;

  // ---- 画 ----
  const headHTML = k => k === "setup"
    ? `<div class="wl-head"><h1 class="wl-q">确认下载源与存放位置</h1></div>`
    : `<div class="wl-head"><img class="wl-logo" src="../../internal/web/ui/img/logo-16.png" alt=""><div class="wl-ht"><h1>欢迎使用 MineDeck</h1><p>Minecraft 开服工具</p></div></div>`;
  const termsHTML = () => `<div class="wl-col wl-terms"><h2 class="wl-h">用户协议与免责声明</h2>`
    + `<div class="wl-lw"><div class="list wl-tx" tabindex="0" aria-label="用户协议与免责声明">${TERMS}</div><i class="thumb"></i></div></div>`;
  const segHTML = () => `<div class="wl-seg" role="radiogroup" aria-label="下载源"><span class="wl-sgl"></span>${OPTS.map(o =>
    `<button type="button" role="radio" data-src="${o.v}" aria-checked="${S.src === o.v}" class="${S.src === o.v ? "on" : ""}" title="${esc(o.d)}">${esc(o.t)}</button>`).join("")}</div>`;
  const fieldsHTML = () => `<div class="wl-col wl-fields">`
    + `<div class="wl-f"><div class="wl-lb"><span title="${esc(SRC_TIP)}">下载源</span></div>${segHTML()}</div>`
    + `<div class="wl-f" title="${esc(PATH_TIP)}"><div class="wl-lb"><span>服务器存放文件夹</span></div>`
    + `<div class="wl-pb"><i data-ic="folder"></i><span class="wl-pt"><span class="hd">${esc(PATH.hd)}</span><span class="tl">${esc(PATH.tl)}</span></span></div></div>`
    + `</div>`;
  const agreeHTML = () => `<div class="wl-agree${S.agree ? " on" : ""}" role="switch" tabindex="0" aria-checked="${S.agree}">`
    + `<span class="wl-sw"></span><span class="wl-at">${AGREE}</span><span class="wl-msg">开始使用前需要同意</span></div>`;
  function pageHTML() {
    const k = kind();
    const body = k === "all" ? termsHTML() + fieldsHTML() + agreeHTML() : k === "terms" ? termsHTML() + agreeHTML() : fieldsHTML();
    return headHTML(k) + `<section class="wl-pnl${k === "setup" ? " fit" : ""}" aria-label="${k === "setup" ? "下载源与存放位置" : "用户协议与免责声明"}">${body}</section>`;
  }
  function mountPage() {
    const p = $("#wl-page");
    p.innerHTML = pageHTML(); KIT.hydrate(p);
    placeSeg(false);
    const lw = $(".wl-lw", p); if (lw) thinBar(lw);
  }

  // 分段的选中块：滑到选中的那一段（animate 为 false 时直接放过去）
  function placeSeg(animate) {
    const hl = $(".wl-sgl"), on = $(".wl-seg button.on"); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }

  // 细滚动条（同设置外壳的 thinBar）：系统的藏掉，右边 4 像素的细条，移上去或在滚时才出来、能拖；上下还有没露出来的那一头淡出 28
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
    paint();
  }

  // ---- 按整像素走的补间（同向导：小点、主键变宽，收尾不抖） ----
  function bezier(x1, y1, x2, y2) {
    const cx = 3 * x1, bx = 3 * (x2 - x1) - cx, ax = 1 - cx - bx, cy = 3 * y1, by = 3 * (y2 - y1) - cy, ay = 1 - cy - by;
    const X = t => ((ax * t + bx) * t + cx) * t, Y = t => ((ay * t + by) * t + cy) * t, dX = t => (3 * ax * t + 2 * bx) * t + cx;
    return x => { let t = x; for (let i = 0; i < 8; i++) { const d = dX(t); if (Math.abs(d) < 1e-6) break; t -= (X(t) - x) / d; } return Y(Math.min(1, Math.max(0, t))); };
  }
  const E_SLIDE = bezier(.3, .7, .2, 1);
  const tweens = {};
  function tweenPx(key, dur, ease, frame, done) {
    const id = tweens[key] = (tweens[key] || 0) + 1, t0 = performance.now();
    const tick = now => { if (tweens[key] !== id) return; const p = Math.min(1, (now - t0) / dur); frame(ease(p)); if (p < 1) requestAnimationFrame(tick); else done?.(); };
    requestAnimationFrame(tick);
  }

  // ---- 底下的按钮 ----
  // 主键的字：一页放完和老用户「同意并开始使用」（程序原话）；分两步时第一步「下一步」、第二步「开始使用」（同意已经在第一步拨过了）
  const NEXT = () => !isLast() ? { k: "next", html: `下一步<i data-ic="chevronRight"></i>`, arrow: true }
    : S.flow === "steps" && !S.old ? { k: "go", html: "开始使用" } : { k: "agree", html: "同意并开始使用" };
  // 字换了时（下一步 ↔ 开始使用）按钮宽度按整像素平滑变，旧字 0.09 秒淡掉、新字淡进来（同向导「下一步」变「创建服务器」）
  let navTok = 0;
  function navLabel() {
    const b = $("#wl-next"), n = NEXT();
    if (b.dataset.label === n.k) return;
    const first = !b.dataset.label, tok = ++navTok;
    b.dataset.label = n.k;
    const swap = () => {
      if (tok !== navTok) return;
      const w0 = b.offsetWidth;
      b.style.width = ""; b.classList.toggle("arrow", !!n.arrow); b.innerHTML = `<span class="lbl">${n.html}</span>`; KIT.hydrate(b);
      if (first) return;
      const w1 = b.offsetWidth;
      b.querySelector(".lbl").animate([{ opacity: 0 }, { opacity: 1 }], { duration: 220, delay: 60, easing: EASE_OUT, fill: "backwards" });
      if (w0 === w1) return;
      b.style.width = w0 + "px";
      tweenPx("nextw", 320, E_SLIDE, e => { b.style.width = Math.round(w0 + (w1 - w0) * e) + "px"; }, () => { if (tok === navTok) b.style.width = ""; });
    };
    const old = b.querySelector(".lbl");
    if (first || !old) return swap();
    old.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = swap;
  }
  // 主键灰不灰、悬停说什么：只改这两样
  function paintNext() {
    const b = $("#wl-next"), off = needAgree();
    if (b.getAttribute("aria-disabled") !== String(off)) b.setAttribute("aria-disabled", off);
    setTip(b, off ? "先同意用户协议与免责声明" : "");
    navLabel();
  }
  function paintNav() {
    $("#wl-back").classList.toggle("off", S.step === 0); // 「上一步」只在分两步的第二步有，从下面浮上来
    paintNext();
  }
  function initDots() {
    const n = STEPS().length;
    $("#wl-dots").innerHTML = n > 1 ? Array.from({ length: n }, (_, i) => `<i class="${i === S.step ? "on" : ""}"></i>`).join("") : "";
  }
  // 换步时小点：宽的那个 22 → 8、下一个 8 → 22，按整像素走（同向导）
  function stepDots() {
    const live = $$("#wl-dots i"), from = live.find(x => x.classList.contains("on")), to = live[S.step];
    live.forEach((x, i) => x.classList.toggle("on", i === S.step));
    if (!from || !to || from === to) { live.forEach(x => { x.style.width = ""; }); return; }
    const a = parseFloat(from.style.width) || 22, b = parseFloat(to.style.width) || 8;
    tweenPx("dots", 320, E_SLIDE, e => {
      to.style.width = (b + Math.round((22 - b) * e)) + "px"; from.style.width = (a - Math.round((a - 8) * e)) + "px";
    }, () => live.forEach(x => { x.style.width = ""; }));
  }

  // ---- 换步（只有分两步有）：同向导 goStep ----
  let stepping = false;
  function goStep(n) {
    if (stepping || n === S.step || n < 0 || n >= STEPS().length) return;
    stepping = true;
    const p = $("#wl-page"), dir = n > S.step ? 1 : -1;
    S.step = n;
    paintNav(); stepDots();
    const out = p.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translateX(${-16 * dir}px)` }], { duration: 140, easing: EASE_IN, fill: "forwards" });
    out.onfinish = () => {
      mountPage(); out.cancel();
      p.animate([{ opacity: 0, transform: `translateX(${16 * dir}px)` }, { opacity: 1, transform: "none" }], { duration: 260, easing: EASE_OUT }).onfinish = () => { stepping = false; };
    };
  }

  // ---- 同意 ----
  function toggleAgree() {
    const bar = $(".wl-agree"); if (!bar) return;
    S.agree = !S.agree;
    bar.classList.toggle("on", S.agree); bar.setAttribute("aria-checked", S.agree);
    if (S.agree) bar.classList.remove("bad");
    paintNext();
  }
  // 没同意就点了灰着的主键：同意那条描红、晃一下（0.36 秒，−6、5、−3），右边出一句，焦点落过去（同向导第三步 EULA）
  function block() {
    const bar = $(".wl-agree"); if (!bar) return;
    bar.classList.add("bad");
    bar.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
      { duration: 360, easing: SLIDE });
    bar.focus({ preventScroll: true });
  }

  // ---- 保存、进首页 ----
  // 落到哪：新用户空首页，老用户有服务器的首页；enter=1 让首页的胶囊从下面浮上来。主题、配色这几个参数跟着带过去
  const homeURL = () => {
    const q = new URLSearchParams();
    ["theme", "scheme", "accent", "corner"].forEach(k => { if (QS.get(k)) q.set(k, QS.get(k)); });
    if (!S.old) q.set("empty", "1");
    q.set("enter", "1");
    return "home.html?" + q;
  };
  function leavePage() {
    S.leaving = true;
    document.body.classList.add("wl-hide");
    $("#wl-page").animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 140, easing: EASE_IN, fill: "forwards" });
  }
  // 点了以后：主键里转一下（草图装作等了一下请求），写 source、onboarded、tosAccepted
  function save() {
    S.saving = true;
    const b = $("#wl-next");
    b.style.width = b.offsetWidth + "px"; b.classList.add("busy"); b.setAttribute("aria-busy", "true");
    b.innerHTML = KIT.icon("spinner", "wl-spin");
    setTimeout(() => {
      if (!S.fail) { leavePage(); setTimeout(() => { location.href = homeURL(); }, 320); return; }
      // 写失败：照程序，弹报错、引导照样收起（下次打开还会再弹）。草图里首页叠一层进来，提示留在它上面
      toast();
      leavePage();
      setTimeout(() => {
        const f = Object.assign(document.createElement("iframe"), { className: "wl-home", src: homeURL(), title: "首页" });
        f.addEventListener("load", () => requestAnimationFrame(() => f.classList.add("on")));
        document.body.appendChild(f);
      }, 320);
    }, 450);
  }

  // ---- 右上角的报错提示：同共用件页（报错的停 20 秒，鼠标停在上面、「详情」开着时不走，移开以后至少再留 4 秒；点一下看详情，× 关掉） ----
  function toast() {
    const box = $("#wl-toasts"), d = document.createElement("div");
    d.className = "wl-tst enter"; d.title = "点击查看详情";
    d.innerHTML = `<span class="ti"><i data-ic="warn"></i></span><span class="tm">无法保存首次使用的设置</span>`
      + `<button type="button" class="wl-ib" data-tx aria-label="关闭这条提示"><i data-ic="x"></i></button>`
      + `<span class="ta"><button type="button" class="wl-sb" data-td>详情</button></span>`;
    KIT.hydrate(d); box.appendChild(d);
    let left = 20000, timer = 0, since = 0, holds = 0, gone = false;
    const kill = () => { if (gone) return; gone = true; clearTimeout(timer); d.classList.remove("enter"); d.classList.add("leave"); setTimeout(() => d.remove(), 140); };
    const run = () => { if (gone || holds) return; since = Date.now(); timer = setTimeout(kill, left); };
    const hold = () => { holds++; if (timer) { clearTimeout(timer); timer = 0; left = Math.max(0, left - (Date.now() - since)); } };
    const release = () => { holds = Math.max(0, holds - 1); if (!holds && !gone) { left = Math.max(left, 4000); run(); } };
    d.addEventListener("click", e => {
      if (e.target.closest("[data-tx]")) { kill(); return; }
      if (mask) return;
      hold(); openErr(release);
    });
    d.addEventListener("mouseenter", hold); d.addEventListener("mouseleave", release);
    run();
  }
  // 报错信息（照 .wd-mask 那一套）：原文、右上一个复制图标（复制后换成勾 1.2 秒），底下「关闭」（默认落在它上面，画一圈细框）
  let mask = null;
  function openErr(onClose) {
    const mk = mask = document.createElement("div");
    mk.className = "wl-mask enter"; mk._done = onClose;
    mk.innerHTML = `<div class="wl-md" role="dialog" aria-modal="true" aria-label="报错信息"><div class="wl-mc">`
      + `<h3><span>报错信息</span><button type="button" class="wl-ib" data-copy title="复制报错信息" aria-label="复制报错信息"><i data-ic="copy"></i></button></h3>`
      + `<div class="wl-raw" tabindex="0">${esc(RAW)}</div></div>`
      + `<div class="wl-ma"><button type="button" class="wl-mb fk" data-close>关闭</button></div></div>`;
    KIT.hydrate(mk); document.body.appendChild(mk);
    const close = mk.querySelector("[data-close]"); close.focus({ focusVisible: false });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) close.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") close.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return;
      const cp = e.target.closest("[data-copy]");
      if (cp) {
        try { navigator.clipboard?.writeText(RAW).catch(() => {}); } catch {}
        cp.innerHTML = KIT.icon("check"); setTip(cp, "已复制");
        clearTimeout(cp._t); cp._t = setTimeout(() => { cp.innerHTML = KIT.icon("copy"); setTip(cp, "复制报错信息"); }, 1200);
        return;
      }
      if (e.target.closest("[data-close]") || e.target === mk) shut();
    });
  }
  function shut() {
    if (!mask) return;
    const mk = mask; mask = null;
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
    mk._done?.();
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) shut(); });

  // ---- 事件 ----
  const page = $("#wl-page");
  page.addEventListener("click", e => {
    if (S.saving || S.leaving) return;
    if (e.target.closest(".wl-agree a")) { e.preventDefault(); return; } // 草图里不打开外部网页；点链接也不拨开关（同向导）
    if (e.target.closest(".wl-agree")) { toggleAgree(); return; }
    const s = e.target.closest("[data-src]");
    if (s && s.dataset.src !== S.src) {
      S.src = s.dataset.src; // 草图里记在内存里，点主键时和同意一起写
      $$(".wl-seg [data-src]").forEach(b => { const on = b === s; b.classList.toggle("on", on); b.setAttribute("aria-checked", on); });
      placeSeg(true);
    }
  });
  page.addEventListener("keydown", e => {
    if (e.target.classList?.contains("wl-agree") && (e.key === " " || e.key === "Enter") && !S.saving && !S.leaving) { e.preventDefault(); toggleAgree(); }
  });
  $("#wl-next").addEventListener("click", () => {
    if (S.saving || S.leaving || stepping) return;
    if (needAgree()) { block(); return; }
    if (!isLast()) { goStep(S.step + 1); return; }
    save();
  });
  $("#wl-back").addEventListener("click", () => { if (!S.saving && !S.leaving) goStep(S.step - 1); });
  addEventListener("resize", () => { placeSeg(false); $(".wl-lw")?._paint?.(); });

  // ---- 草图切换条 ----
  function skBar() {
    const link = (k, v, label, on, title = "") => {
      const q = new URLSearchParams(location.search);
      if (v === null) q.delete(k); else q.set(k, v);
      if (k === "flow" || k === "user") q.delete("step");
      const s = q.toString();
      return `<a href="${s ? "?" + s : location.pathname}" class="${on ? "on" : ""}"${title ? ` title="${esc(title)}"` : ""}>${label}</a>`;
    };
    const th = document.documentElement.dataset.theme;
    $("#sk").innerHTML = `<b>草图</b><b>主题</b>${link("theme", null, "深", th === "dark")}${link("theme", "light", "浅", th === "light")}<i></i>`
      + `<b>排法</b>${link("flow", null, "一页", S.flow === "one", "主会话推荐这种：下载源、存放位置都有默认值，没什么要选的，不值得多点一下")}`
      + `${link("flow", "steps", "分两步", S.flow === "steps", "① 协议和同意 → ② 下载源和存放位置。老用户两种一样，只有协议")}<i></i>`
      + `<b>用户</b>${link("user", null, "新用户", !S.old)}${link("user", "old", "老用户", S.old, "已经用过、没同意过协议的老用户：只补协议这一步")}<i></i>`
      + `<b>保存</b>${link("save", null, "成功", !S.fail)}${link("save", "fail", "失败", S.fail, "写配置失败：照程序，弹报错、照样进首页，下次打开再弹")}`;
  }

  KIT_READY.then(() => {
    skBar(); mountPage(); initDots(); paintNav();
    // 进场：底下的按钮从下面浮上来（同向导）
    requestAnimationFrame(() => requestAnimationFrame(() => document.body.classList.remove("wl-hide")));
    document.fonts?.ready.then(() => { placeSeg(false); $(".wl-lw")?._paint?.(); });
  });
})();
