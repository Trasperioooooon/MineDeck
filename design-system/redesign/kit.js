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
  let tip = null, cur = null, timer = 0, lastHide = 0;
  const tipEl = () => tip || (tip = Object.assign(document.createElement("div"), { className: "kit-tip" }), tip.setAttribute("role", "tooltip"), document.body.appendChild(tip), tip);
  const show = el => {
    const t = tipEl(); t.textContent = el.dataset.tip; t.classList.remove("on", "below");
    const r = el.getBoundingClientRect(), w = t.offsetWidth, h = t.offsetHeight, gap = 8;
    const below = r.top - gap - h < 8;
    t.classList.toggle("below", below);
    t.style.left = Math.round(Math.min(Math.max(8, r.left + r.width / 2 - w / 2), innerWidth - w - 8)) + "px";
    t.style.top = Math.round(below ? r.bottom + gap : r.top - gap - h) + "px";
    requestAnimationFrame(() => t.classList.add("on"));
  };
  const hide = () => { clearTimeout(timer); if (tip) tip.classList.remove("on"); if (cur) lastHide = performance.now(); cur = null; };
  document.addEventListener("pointerover", e => {
    const el = e.target.closest?.("[title], [data-tip]");
    if (el && el.hasAttribute("title")) { // 每次悬停都从 title 重新取，脚本后来改了 title 也跟得上
      const t = el.getAttribute("title");
      if (t) { el.dataset.tip = t; if (!el.hasAttribute("aria-label") && !el.textContent.trim()) el.setAttribute("aria-label", t); } else delete el.dataset.tip;
      el.removeAttribute("title");
    }
    if (el === cur) return;
    hide();
    if (!el || !el.dataset.tip || el.closest(".cap")) return;
    cur = el;
    timer = setTimeout(() => { if (cur === el && el.isConnected && el.dataset.tip) show(el); }, performance.now() - lastHide < 300 ? 0 : 400);
  });
  document.addEventListener("pointerout", e => { if (!e.relatedTarget) hide(); });
  document.addEventListener("pointerdown", hide, true);
  document.addEventListener("scroll", hide, true);
  addEventListener("blur", hide);
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
