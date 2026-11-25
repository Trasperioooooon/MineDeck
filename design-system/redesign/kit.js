// 界面重构草图的小工具，放在 <head> 里同步引入。
// 1. 照 URL 参数设外观：?scheme=nord&theme=dark&accent=auto，默认是作者常用的 Nord 深色。
// 2. 把 <i data-ic="图标名"></i> 换成程序自己的图标：现读 internal/web/ui/app.js 里的 ICON 映射，
//    所以草图和真界面用的是同一套图标；草图要用程序里还没有的，在页面里先定义 window.EXTRA_ICONS = { 名字: "<path …/>" }。
(async () => {
  const q = new URLSearchParams(location.search), r = document.documentElement;
  r.dataset.scheme = q.get("scheme") || "nord";
  r.dataset.theme = q.get("theme") || "dark";
  r.dataset.accent = q.get("accent") || "auto";
  let ICON = {};
  try {
    const src = await (await fetch("../../internal/web/ui/app.js")).text();
    const a = src.indexOf("const ICON = {"), b = src.indexOf("\n};", a);
    ICON = new Function("return " + src.slice(a + "const ICON = ".length, b + 2))();
  } catch (e) { console.warn("没取到 app.js 里的图标", e); }
  if (document.readyState === "loading") await new Promise(ok => document.addEventListener("DOMContentLoaded", ok));
  Object.assign(ICON, window.EXTRA_ICONS || {});
  for (const el of document.querySelectorAll("i[data-ic]")) {
    const name = el.dataset.ic;
    if (!ICON[name]) console.warn("草图里用了没有的图标：" + name);
    el.outerHTML = `<svg class="ico ${el.className}" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICON[name] || ""}</svg>`;
  }
})();
