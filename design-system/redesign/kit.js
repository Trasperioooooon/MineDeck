// 界面重构草图的小工具，放在 <head> 里同步引入。
// 看草图：`python design-system/redesign/serve.py`（仓库根目录为根、不缓存），
// 打开 http://127.0.0.1:27395/design-system/redesign/<页面>.html。直接双击 html 读不到 app.js，图标会是空的；
// 用 `python -m http.server` 也能看，但浏览器会缓存，改了草图要强制刷新。
// 1. 照 URL 参数设外观：?scheme=nord&theme=dark&accent=auto，默认是作者常用的 Nord 深色。
// 2. 把 <i data-ic="图标名"></i> 换成程序自己的图标：现读 internal/web/ui/app.js 里的 ICON 映射，
//    所以草图和真界面用的是同一套图标；草图要用程序里还没有的，在页面里先定义 window.EXTRA_ICONS = { 名字: "<path …/>" }。
// 3. 页面自己用脚本画内容时：await KIT_READY 之后用 KIT.icon(名字) 拿图标，或者画完调 KIT.hydrate(容器) 换掉里面的 <i data-ic>。
// 4. 悬停提示：草图里写的 title 一律换成自己画的小浮层（11-26 作者定：不用浏览器自带的），样子同首页胶囊上的提示。
//    鼠标停 0.4 秒出来；刚看过一个、0.3 秒内移到下一个时马上出；按下、滚动、离开窗口就收。胶囊自己有一样的提示，不重复出。
(() => {
  const q = new URLSearchParams(location.search), r = document.documentElement;
  r.dataset.scheme = q.get("scheme") || "nord";
  r.dataset.theme = q.get("theme") || "dark";
  r.dataset.accent = q.get("accent") || "auto";
  if (q.get("corner") === "round") r.dataset.corner = "round"; // 换回普通圆弧，和连续曲率对比
  const ICON = {};
  const icon = (name, cls = "") => `<svg class="ico ${cls}" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICON[name] || ""}</svg>`;
  const hydrate = (root = document) => {
    for (const el of root.querySelectorAll("i[data-ic]")) {
      if (!ICON[el.dataset.ic]) console.warn("草图里用了没有的图标：" + el.dataset.ic);
      el.outerHTML = icon(el.dataset.ic, el.className);
    }
  };
  window.KIT = { icon, hydrate };
  // 4. 悬停提示
  let tip = null, cur = null, timer = 0, lastHide = 0, raf = 0;
  const tipEl = () => tip || (tip = Object.assign(document.createElement("div"), { className: "kit-tip" }), tip.setAttribute("role", "tooltip"), document.body.appendChild(tip), tip);
  const show = el => {
    const t = tipEl(); t.textContent = el.dataset.tip; t.classList.remove("on", "below", "side-r", "side-l");
    const r = el.getBoundingClientRect(), w = t.offsetWidth, h = t.offsetHeight, gap = 8;
    // 下拉菜单里的项：弹在菜单旁边（右边放不下就左边），竖着对齐这一项；弹在上下会盖住相邻的项（作者 11-28 指出 Java 下拉里路径盖住了上一行）
    const menu = el.closest('[role="menu"]');
    if (menu) {
      const m = menu.getBoundingClientRect(), right = m.right + gap + w <= innerWidth - 8;
      t.classList.add(right ? "side-r" : "side-l");
      t.style.left = Math.round(right ? m.right + gap : Math.max(8, m.left - gap - w)) + "px";
      t.style.top = Math.round(Math.min(Math.max(8, r.top + r.height / 2 - h / 2), innerHeight - h - 8)) + "px";
      raf = requestAnimationFrame(() => t.classList.add("on"));
      return;
    }
    const below = r.top - gap - h < 8;
    t.classList.toggle("below", below);
    t.style.left = Math.round(Math.min(Math.max(8, r.left + r.width / 2 - w / 2), innerWidth - w - 8)) + "px";
    t.style.top = Math.round(below ? r.bottom + gap : r.top - gap - h) + "px";
    raf = requestAnimationFrame(() => t.classList.add("on"));
  };
  const hide = () => { clearTimeout(timer); cancelAnimationFrame(raf); // 刚 show 还没加上 .on 的也收掉（步进打错字后马上点别处，提示原来会留在屏幕上）
    if (tip) tip.classList.remove("on"); if (cur) lastHide = performance.now(); cur = null; };
  document.addEventListener("pointerover", e => {
    const el = e.target.closest?.("[title], [data-tip]");
    if (el && el.hasAttribute("title")) { // 每次悬停都从 title 重新取，脚本后来改了 title 也跟得上
      const t = el.getAttribute("title");
      if (t) { el.dataset.tip = t; if (!el.hasAttribute("aria-label") && !el.textContent.trim()) el.setAttribute("aria-label", t); } else delete el.dataset.tip;
      el.removeAttribute("title");
    }
    if (el === cur) return;
    hide();
    if (!el || !el.dataset.tip || el.closest(".cap") || el.querySelector(":scope > .kit-num")) return; // 步进正在打字时不出「点击输入数值」
    cur = el;
    timer = setTimeout(() => { if (cur === el && el.isConnected && el.dataset.tip) show(el); }, performance.now() - lastHide < 300 ? 0 : 400);
  });
  document.addEventListener("pointerout", e => { if (!e.relatedTarget) hide(); });
  document.addEventListener("pointerdown", hide, true);
  document.addEventListener("scroll", hide, true);
  addEventListener("blur", hide);
  // 马上在某个元素上出提示（不等悬停）：步进打字打错了这类要立刻说的。下一次按下、滚动、离开窗口照常收
  const tipNow = (el, text) => { el.dataset.tip = text; hide(); cur = el; show(el); };

  // 5. 确认框（11-28 起各页共用这一个，样子同共用件页的确认框：外圆角 26、内边 8，里面一块圆角 18；图标块 40 见方；按钮是 36 高的胶囊、平分一排）。
  //    btns 从左到右 [{ k, label, tone }]，tone 是 "acc"（主键）或 "dgr"（危险）；点外面、按 Esc 算 "cancel"。
  //    默认聚焦：有危险键时落在第一个（「取消」，同共用件页），否则落在最后一个（主键）；鼠标点开时画一圈细框，一动别的就收。返回 Promise<点的那个 k>
  const confirmBox = ({ ic = "info", tone = "acc", title, body = "", btns, focus, w = 400 }) => new Promise(done => {
    const mk = document.createElement("div"); mk.className = "kit-mask enter";
    mk.innerHTML = `<div class="kit-md" role="dialog" aria-modal="true" style="--w:${w}px"><div class="kit-mc"><div class="kit-mh"><span class="kit-mi ${tone}">${icon(ic)}</span><h3>${title}</h3></div>`
      + (body ? `<div class="kit-mb">${body}</div>` : "") + `</div><div class="kit-ma">`
      + btns.map(b => `<button type="button" class="kit-b ${b.tone || ""}" data-k="${b.k}">${b.label}</button>`).join("") + `</div></div>`;
    document.body.appendChild(mk);
    const prev = document.activeElement;
    const end = k => {
      if (mk.classList.contains("leave")) return;
      mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
      prev?.focus?.({ preventScroll: true }); done(k);
    };
    mk.addEventListener("click", e => { const b = e.target.closest("[data-k]"); if (b) end(b.dataset.k); else if (e.target === mk) end("cancel"); });
    mk.addEventListener("keydown", e => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); end("cancel"); } });
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) mk.querySelectorAll(".fk").forEach(x => x.classList.remove("fk")); });
    const f = mk.querySelector(`[data-k="${focus || (btns.some(b => b.tone === "dgr") ? btns[0].k : btns[btns.length - 1].k)}"]`);
    f.classList.add("fk"); f.focus({ focusVisible: false });
  });

  // 6. 离开前拦未保存的改动（作者 11-28 定：所有要点保存的页面，改了没保存就切走、刷新、跳到别的页，都先问一句）。
  //    页面登记的那一份（DETAIL_TABS / SETTINGS_SECS 里的 { html, mount }）可以多带三个：
  //      dirty() 返回未保存的项数（0 = 干净）；save() 存下（返回 false 表示没存成，比如有框填错了，那就留在这页）；discard() 丢掉草稿、回到存过的样子。
  //    「不保存」一定要调 discard()：各页的状态按模块缓存着，不丢的话下次回来草稿还在。
  //    leave(view) 返回 Promise<能不能走>；外壳在换标签、换子页之前 await 它。
  const leave = async view => {
    const n = view?.dirty?.() || 0; if (!n) return true;
    const k = await confirmBox({ ic: "save", title: "有未保存的修改", body: `当前页面有 ${n} 项修改尚未保存。`,
      btns: [{ k: "cancel", label: "取消" }, { k: "discard", label: "不保存" }, { k: "save", label: "保存", tone: "acc" }] });
    if (k === "save") return (await view.save?.()) !== false;
    if (k === "discard") { await view.discard?.(); return true; }
    return false;
  };
  //    guardPage(getView)：外壳调一次，拦这几种离开——F5 / Ctrl+R 刷新（自己弹框）、点页面上跳到别的页的链接（底部胶囊、页头里的链接）；
  //    关窗口、标题栏刷新这种拦不到自己弹框的，交给浏览器自带的 beforeunload 兜底。左上角、左下角的草图切换条（#sk、#sk2）是草图专用的，不拦。
  //    返回 { go(href) }：外壳自己用脚本跳页（如「导出交接包」跳任务页）时走它
  const guardPage = getView => {
    let bypass = false;
    const dirty = () => (getView()?.dirty?.() || 0) > 0;
    const go = async href => { if (await leave(getView())) { bypass = true; location.href = href; } };
    addEventListener("keydown", async e => {
      if (!(e.key === "F5" || ((e.ctrlKey || e.metaKey) && /^r$/i.test(e.key))) || !dirty()) return;
      e.preventDefault(); e.stopPropagation();
      if (await leave(getView())) { bypass = true; location.reload(); }
    }, true);
    addEventListener("beforeunload", e => { if (!bypass && dirty()) { e.preventDefault(); e.returnValue = ""; } });
    document.addEventListener("click", e => {
      const a = e.target.closest?.("a[href]");
      if (!a || e.defaultPrevented || e.button || e.ctrlKey || e.metaKey || e.shiftKey || a.target === "_blank") return;
      if (a.closest("#sk, #sk2")) { bypass = true; return; }
      if (!dirty()) return;
      e.preventDefault(); e.stopPropagation(); go(a.href);
    }, true);
    return { go };
  };

  // 7. 步进中间的数字：平时是字，点进去就地打字（规范第九节「平时是字、点进去才出框」；作者 11-28 定凡是步进都这样，值不在档位上的也能直接填）。
  //    el 是步进中间写数的那个元素，页面每次重画步进以后再调一次。o：
  //      get() 现在的数；set(n) 落下新的数（页面自己重画、记未保存）；min / max 范围；int 只收整数；
  //      text(n) 可选，进框时框里写什么（默认就是数字本身，不带单位）；parse(字) 可选，自己认字（如「不限」「1.5 小时」），认不出返回 NaN；
  //      hint 可选，填错时的提示（默认「请输入 min～max 之间的整数 / 数值」）；label 读屏用的名字。
  //    回车、点别处是确定；Esc 是不改。填错了框描危险色、马上出提示，回车不走；直接点别处就照原样放回去
  const numEdit = (el, o) => {
    el._kitNum = o; if (el._kitNumOn) return; el._kitNumOn = true;
    el.classList.add("kit-numt"); el.tabIndex = 0; el.setAttribute("role", "button");
    if (!el.dataset.tip && !el.title) el.dataset.tip = "点击输入数值";
    const open = () => {
      if (el.querySelector(".kit-num")) return;
      const p = el._kitNum, old = el.innerHTML, cur = p.get(), box = el.parentElement;
      const inp = Object.assign(document.createElement("input"), { className: "kit-num", value: p.text ? p.text(cur) : String(cur), spellcheck: false, autocomplete: "off" });
      inp.setAttribute("aria-label", p.label || "数值");
      el.innerHTML = ""; el.appendChild(inp); box.classList.add("kit-num-on"); inp.focus(); inp.select();
      let over = false;
      const close = () => { over = true; box.classList.remove("kit-num-on", "kit-num-bad"); if (el.contains(inp)) el.innerHTML = old; };
      const commit = () => {
        const raw = inp.value.trim(), n = p.parse ? p.parse(raw) : (/^-?\d+(\.\d+)?/.test(raw.replace(/\s/g, "")) ? parseFloat(raw.replace(/\s/g, "")) : NaN);
        if (!Number.isFinite(n) || (p.min != null && n < p.min) || (p.max != null && n > p.max) || (p.int && !Number.isInteger(n))) {
          box.classList.add("kit-num-bad");
          tipNow(inp, p.hint || `请输入 ${p.min ?? ""}～${p.max ?? ""} 之间的${p.int ? "整数" : "数值"}`);
          return false;
        }
        close(); if (n !== cur) p.set(n); return true;
      };
      inp.addEventListener("keydown", e => {
        if (e.key === "Enter") { e.preventDefault(); if (commit()) el.focus({ preventScroll: true }); }
        else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); el.focus({ preventScroll: true }); }
      });
      inp.addEventListener("input", () => { box.classList.remove("kit-num-bad"); hide(); });
      inp.addEventListener("blur", () => { if (!over && !commit()) close(); hide(); });
    };
    el.addEventListener("click", open);
    el.addEventListener("keydown", e => { if ((e.key === "Enter" || e.key === " ") && e.target === el) { e.preventDefault(); open(); } });
  };
  Object.assign(window.KIT, { confirm: confirmBox, leave, guardPage, numEdit });

  // 8. 开关只认开关本身（作者 11-28 定；规范第九节）。草图里开关和它的名字写在一起、整块都能点：详情页和设置页是 role="switch" 的按钮，
  //    向导、导入、工具箱、联机页是 .tg，EULA 是整条 [data-eula]；里面类名是 sw 或「前缀-sw」的那一小块是开关。
  //    点在开关以外（名字、说明）就在捕获阶段拦下，页面自己的点击处理收不到；按点的位置判断（开关四周放宽 4 像素），EULA 里开关设了不接鼠标也算得准。
  //    只有开关、没有字的照旧；名字里的链接、按钮照常能点；键盘聚焦后按空格、回车照常切（键盘触发的 click 的 detail 是 0）
  const SWITCH = '[role="switch"], .tg, [data-eula]';
  const isKnob = el => [...el.classList].some(c => /^([a-z]+-)?sw$/.test(c));
  document.addEventListener("click", e => {
    if (!e.detail) return;
    const b = e.target.closest?.(SWITCH); if (!b || !b.textContent.trim()) return;
    const inner = e.target.closest("a, button, input, select, textarea"); if (inner && inner !== b && b.contains(inner)) return;
    const k = [...b.querySelectorAll("span")].find(isKnob); if (!k) return;
    const r = k.getBoundingClientRect();
    if (e.clientX >= r.left - 4 && e.clientX <= r.right + 4 && e.clientY >= r.top - 4 && e.clientY <= r.bottom + 4) return;
    e.stopImmediatePropagation(); e.preventDefault();
  }, true);
  window.KIT_READY = (async () => {
    try {
      const src = await (await fetch("../../internal/web/ui/app.js")).text();
      const a = src.indexOf("const ICON = {"), b = src.indexOf("\n};", a);
      Object.assign(ICON, new Function("return " + src.slice(a + "const ICON = ".length, b + 2))());
    } catch (e) { console.warn("没取到 app.js 里的图标", e); }
    if (document.readyState === "loading") await new Promise(ok => document.addEventListener("DOMContentLoaded", ok));
    Object.assign(ICON, window.EXTRA_ICONS || {});
    hydrate();
  })();
})();
