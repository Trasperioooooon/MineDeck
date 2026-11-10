/* MineDeck 前端（无框架 SPA） */
"use strict";

/* ---------- Token 与 API ---------- */
(function initToken() {
  const p = new URLSearchParams(location.search);
  const t = p.get("token");
  if (t) {
    localStorage.setItem("amh_token", t);
    history.replaceState(null, "", location.pathname + (location.hash || "#/instances"));
  }
})();
const TOKEN = () => localStorage.getItem("amh_token") || "";

async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || "GET",
    headers: { "X-Token": TOKEN(), ...(opts.body ? { "Content-Type": "application/json" } : {}) },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  let j;
  try { j = await res.json(); } catch { throw new Error(`服务异常 (HTTP ${res.status})`); }
  // needLogin 标记：让 boot() 区分「需要登录」与真正的后端故障
  if (res.status === 401) { showLogin(); throw Object.assign(new Error(j.error || "需要登录"), { needLogin: true }); }
  if (!j.ok) throw Object.assign(new Error(j.error || "未知错误"), { status: res.status });
  return j.data;
}

/* 局域网访问的密码登录遮罩 */
function showLogin() {
  if ($("#login-mask")) return;
  const d = document.createElement("div");
  d.className = "modal-mask enter";
  d.id = "login-mask";
  d.innerHTML = `<div class="modal"><div class="brand-head">${logoImg()}<h3>MineDeck 远程访问</h3></div>
    <div class="m-body">请输入访问密码（在主机的「设置 → 远程访问」中配置）</div>
    <input type="password" id="login-pw" style="width:100%;margin-bottom:16px" aria-label="访问密码" autofocus>
    <div class="m-actions"><button class="btn primary" id="login-go">登录</button></div></div>`;
  document.body.appendChild(d);
  const go = async () => {
    try {
      const res = await fetch("/api/login", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: d.querySelector("#login-pw").value }),
      });
      const j = await res.json();
      if (!j.ok) throw new Error(j.error || "登录失败");
      d.remove();
      location.reload();
    } catch (e) { toast(e.message, true); }
  };
  d.querySelector("#login-go").onclick = go;
  d.querySelector("#login-pw").onkeydown = e => { if (e.key === "Enter") go(); };
}

/* ---------- 工具 ---------- */
const $ = (sel, el = document) => el.querySelector(sel);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
/* ---------- 动效工具 ----------
   时长 / 缓动与 app.css 的 --dur-* / --ease-* 令牌一致；WAAPI 只动 transform / opacity。
   系统开启「减少动态」时一律不播（CSS 侧也全局关停了动画与过渡），状态直接切换。 */
const MOTION = { d2: 160, d3: 220, out: "cubic-bezier(.22,1,.36,1)" };
const reducedMotion = () => matchMedia("(prefers-reduced-motion: reduce)").matches;
/* 重播一个 CSS 动画类：先摘掉、强制回流、再加上；播完自动摘掉 */
function playClass(el, cls) {
  if (!el || reducedMotion()) return;
  el.classList.remove(cls); void el.offsetWidth; el.classList.add(cls);
  el.addEventListener("animationend", function h(e) { if (e.target === el) { el.classList.remove(cls); el.removeEventListener("animationend", h); } });
}
/* 列表 FLIP：增删元素后，其余元素从旧位置平移到新位置（提示堆叠用） */
function flipList(parent, mutate) {
  const before = new Map([...parent.children].map(e => [e, e.getBoundingClientRect().top]));
  mutate();
  if (reducedMotion()) return;
  [...parent.children].forEach(e => {
    const t = before.get(e);
    if (t == null) return;
    const dy = t - e.getBoundingClientRect().top;
    if (Math.abs(dy) > .5) e.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], { duration: MOTION.d3, easing: MOTION.out });
  });
}
/* 浮层离场：加 leave 类播离场动画（比进场短），播完移除；只移除这一个元素，期间新开的浮层不受影响 */
function dismiss(el, ms = 160) {
  if (!el || el.dataset.leaving) return;
  if (reducedMotion()) { el.remove(); return; }
  el.dataset.leaving = "1";
  el.classList.remove("enter");
  el.classList.add("leave");
  setTimeout(() => el.remove(), ms);
}
/* 键帽滑动（FLIP）：切换前记下各处当前项（侧栏 / 详情标签 / 设置导航 / 分段控件）的位置，
   重绘后让新当前项的键帽底板（::after）从旧位置滑到新位置，文字不动 */
const KEYCAPS = { nav: "#sidebar .nav-item.active", tab: ".tabs > .tab.cur", set: ".set-nav.cur", seg: ".seg > button.on" };
function keyRects() {
  const out = {};
  for (const [k, sel] of Object.entries(KEYCAPS)) { const e = document.querySelector(sel); if (e) out[k] = e.getBoundingClientRect(); }
  return out;
}
function slideKeys(before, which) {
  if (!before || reducedMotion()) return;
  for (const k of which) {
    const a = before[k], e = document.querySelector(KEYCAPS[k]);
    if (!a || !e) continue;
    const b = e.getBoundingClientRect();
    if (!b.width || !b.height) continue;
    const dx = a.left - b.left, dy = a.top - b.top, sx = a.width / b.width, sy = a.height / b.height;
    if (Math.abs(dx) < .5 && Math.abs(dy) < .5 && Math.abs(sx - 1) < .01) continue;
    e.animate([{ transform: `translate(${dx}px, ${dy}px) scale(${sx}, ${sy})` }, { transform: "none" }], { duration: MOTION.d3, easing: MOTION.out, pseudoElement: "::after" });
    if (k === "nav") e.animate([{ opacity: 0 }, { opacity: 0, offset: .5 }, { opacity: 1 }], { duration: MOTION.d3, pseudoElement: "::before" });
  }
}

/* 提示：标题栏下方右侧，从上方落入；离场向右淡出，其余提示平移补位；可点击关闭 */
function toast(msg, isErr = false) {
  const box = $("#toasts");
  const d = document.createElement("div");
  d.className = "toast enter" + (isErr ? " error" : "");
  d.innerHTML = `<span class="t-ico">${icon(isErr ? "warn" : "check")}</span>`;
  const t = document.createElement("span");
  t.textContent = msg;
  d.appendChild(t);
  d.title = "点击关闭";
  box.appendChild(d);
  let gone = false;
  const kill = () => {
    if (gone) return;
    gone = true;
    if (reducedMotion()) { flipList(box, () => d.remove()); return; }
    d.classList.remove("enter"); d.classList.add("leave");
    let removed = false;
    const done = () => { if (removed) return; removed = true; if (d.isConnected) flipList(box, () => d.remove()); };
    d.addEventListener("animationend", done, { once: true });
    setTimeout(done, 400);
  };
  d.onclick = kill;
  setTimeout(kill, isErr ? 6500 : 3500);
  return d; // 调用方可以换掉点击的行为（如 mcp.js：点一下打开 AI 助手）
}
/* 复制到剪贴板：优先 navigator.clipboard；非安全上下文（http 局域网）下其为 undefined，回退 execCommand */
async function copy(text, okMsg = "已复制") {
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) { await navigator.clipboard.writeText(text); toast(okMsg); return; }
  } catch (e) { /* 落到回退 */ }
  try {
    const ta = document.createElement("textarea");
    ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta); ta.focus(); ta.select();
    document.execCommand("copy"); ta.remove(); toast(okMsg);
  } catch (e) { toast("复制失败，请手动复制", true); }
}
function fmtBytes(n) {
  if (n <= 0) return "";
  const u = ["B", "KB", "MB", "GB"];
  let i = 0;
  while (n >= 1024 && i < 3) { n /= 1024; i++; }
  return n.toFixed(i ? 1 : 0) + " " + u[i];
}
/* 按钮写出后果（如「保留 / 删除服务器」），不读正文也能作答；危险操作默认聚焦「取消」，误按回车不会直接执行。
   危险确认框：标题前带危险色图标瓦片（icon 指定图标），最终键为实底红。遮罩淡入、对话框上浮放大进场，离场更短 */
function confirmModal({ title, body, okText = "确定", cancelText = "取消", danger = false, extra = "", icon: ic = "warn" }) {
  return new Promise(resolve => {
    const root = $("#modal-root");
    const prev = document.activeElement; // 关闭后归还焦点
    root.innerHTML = `<div class="modal-mask enter"><div class="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title">
      <h3 id="modal-title">${danger ? `<span class="m-ico">${icon(ic)}</span>` : ""}<span>${esc(title)}</span></h3><div class="m-body">${body}</div>${extra}
      <div class="m-actions">
        <button class="btn" data-x="cancel">${esc(cancelText)}</button>
        <button class="btn ${danger ? "danger solid" : "primary"}" data-x="ok">${esc(okText)}</button>
      </div></div></div>`;
    const mask = root.firstElementChild;
    let closed = false;
    const close = val => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      dismiss(mask);
      if (prev && prev.focus) prev.focus();
      resolve(val);
    };
    const onKey = e => { if (e.key === "Escape") close(null); };
    document.addEventListener("keydown", onKey);
    mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget) close(null); });
    mask.querySelector('[data-x="cancel"]').onclick = () => close(null);
    mask.querySelector('[data-x="ok"]').onclick = () => {
      const checks = {};
      mask.querySelectorAll("input[type=checkbox][data-k]").forEach(c => checks[c.dataset.k] = c.checked);
      close(checks);
    };
    mask.querySelector(danger ? '[data-x="cancel"]' : '[data-x="ok"]').focus();
  });
}

/* 核心元数据（启动时从 /api/cores 拉取） */
let CORES = {};
const coreName = id => (CORES[id] && CORES[id].name) || id;
const coreKind = id => (CORES[id] && CORES[id].kind) || "game";

/* 核心头像：像素图案（pixel.js，12 种核心各一枚）装在 squircle 瓦片里；核心色只作辨识，不铺大色块 */
const avatarOf = (id, cls = "") => MDPixel.avatar(id, cls);
/* 核心徽标：名称前一个核心色小方块，与像素头像呼应 */
const coreBadge = id => `<span class="badge core" style="--c:${MDPixel.CORE_COLORS[id] || "#7a8aa0"}">${esc(coreName(id))}</span>`;

/* 页头：可选眉标（像素小方块 + 上下文，如「共 4 台 · 运行中 1」，允许富文本）+ 标题 + 可选说明（允许富文本，
   调用方自负安全）+ 右侧操作区 + 可选面包屑。eyebrowId 给需要随数据刷新的眉标一个 id */
const pageHead = (title, sub = "", actions = "", crumbs = "", eyebrow = "", eyebrowId = "") =>
  (crumbs ? `<nav class="crumbs" aria-label="位置">${crumbs}</nav>` : "") +
  `<div class="page-head"><div class="ph-text">${eyebrow || eyebrowId ? `<div class="eyebrow"${eyebrowId ? ` id="${eyebrowId}"` : ""}>${eyebrow}</div>` : ""}<h1>${esc(title)}</h1>${sub ? `<div class="sub">${sub}</div>` : ""}</div>` +
  (actions ? `<div class="ph-actions">${actions}</div>` : "") + `</div>`;
const SEP = `<span class="sep">·</span>`;

/* 应用 Logo：与 exe / 任务栏图标同一套逐尺寸绘制的像素画（img/logo-*.png）。
   48px 版用于引导卡、关于页、登录框；按屏幕缩放挑原生网格的那一张，pixelated 保持像素锐利 */
const logoImg = (cls = "logo-lg") =>
  `<img class="${cls}" src="img/logo-48.png" srcset="img/logo-48.png 1x, img/logo-72.png 1.5x, img/logo-96.png 2x, img/logo-144.png 3x" width="48" height="48" alt="">`;

/* ---------- 内联 SVG 图标 ----------
   对齐 win-ctrls 风格：16 视框、细线 currentColor、圆角端点。icon(name) 返回字符串供模板拼接；
   颜色继承文字色（双主题自动成立），默认尺寸 1em（随字号缩放），故按钮/文本内无需另设色与尺寸。
   界面图标一律走这里，禁止 emoji（用户内容如 MOTD/控制台输出不受限）。 */
const ICON = {
  // 导航 / 主题
  server: '<rect x="2.5" y="2.5" width="11" height="4.5" rx="1.2"/><rect x="2.5" y="9" width="11" height="4.5" rx="1.2"/><path d="M5 4.75h.01M5 11.25h.01"/>',
  plusBox: '<rect x="2.5" y="2.5" width="11" height="11" rx="2.5"/><path d="M8 5.5v5M5.5 8h5"/>',
  palette: '<path d="M8 2.2a5.8 5.8 0 100 11.6c.9 0 1.3-.6 1.1-1.3-.3-.9.2-1.7 1.2-1.7h1.4a2.1 2.1 0 002.1-2.1C13.8 4.8 11.2 2.2 8 2.2z"/><path d="M5.2 7.2h.01M6.8 4.9h.01M9.6 4.9h.01M11.2 7.2h.01" stroke-width="1.8"/>',
  chevronRight: '<path d="M6.2 3.8 10.4 8l-4.2 4.2"/>',
  gear: '<path d="M6.72 3.37L6.87 1.60A6.5 6.5 0 0 1 9.13 1.60L9.28 3.37A4.8 4.8 0 0 1 10.36 3.82L11.73 2.68A6.5 6.5 0 0 1 13.32 4.27L12.18 5.64A4.8 4.8 0 0 1 12.63 6.72L14.4 6.87A6.5 6.5 0 0 1 14.4 9.13L12.63 9.28A4.8 4.8 0 0 1 12.18 10.36L13.32 11.73A6.5 6.5 0 0 1 11.73 13.32L10.36 12.18A4.8 4.8 0 0 1 9.28 12.63L9.13 14.40A6.5 6.5 0 0 1 6.87 14.40L6.72 12.63A4.8 4.8 0 0 1 5.64 12.18L4.27 13.32A6.5 6.5 0 0 1 2.68 11.73L3.82 10.36A4.8 4.8 0 0 1 3.37 9.28L1.6 9.13A6.5 6.5 0 0 1 1.6 6.87L3.37 6.72A4.8 4.8 0 0 1 3.82 5.64L2.68 4.27A6.5 6.5 0 0 1 4.27 2.68L5.64 3.82A4.8 4.8 0 0 1 6.72 3.37Z"/><circle cx="8" cy="8" r="2.1"/>',
  sun: '<circle cx="8" cy="8" r="3.1"/><path d="M8 1.6v1.7M8 12.7v1.7M14.4 8h-1.7M3.3 8H1.6M12.5 3.5l-1.2 1.2M4.7 11.3l-1.2 1.2M12.5 12.5l-1.2-1.2M4.7 4.7L3.5 3.5"/>',
  moon: '<path d="M13 9.6A5.6 5.6 0 016.4 3 5.6 5.6 0 1013 9.6z"/>',
  monitor: '<rect x="2.4" y="3" width="11.2" height="7.4" rx="1"/><path d="M6 13h4M8 10.4V13"/>',
  // 状态 / 数据
  search: '<circle cx="7" cy="7" r="4.3"/><path d="M10.3 10.3l3.2 3.2"/>',
  clock: '<circle cx="8" cy="8" r="5.8"/><path d="M8 4.7V8l2.4 1.5"/>',
  player: '<circle cx="8" cy="5.4" r="2.6"/><path d="M3.5 12.9c0-2.5 2-4.2 4.5-4.2s4.5 1.7 4.5 4.2"/>',
  // 动作
  play: '<path d="M6 4l6 4-6 4z" fill="currentColor"/>',
  stop: '<rect x="4.6" y="4.6" width="6.8" height="6.8" rx="1.3" fill="currentColor"/>',
  folder: '<path d="M2.4 12.6V4.6h3.4l1.4 1.6h6.4v6.4z"/>',
  bolt: '<path d="M9 2.2L4.2 9.2H7.3L6.6 13.8 11.8 6.6H8.4z" fill="currentColor"/>',
  // 文件夹直达菜单 / 状态
  package: '<path d="M8 1.9l5.4 2.9v6.4L8 14.1 2.6 11.2V4.8z"/><path d="M2.7 4.9L8 7.7l5.3-2.8M8 7.7v6.3"/>',
  plug: '<path d="M6 2.2v2.6M10 2.2v2.6M4.6 4.8h6.8v2.4a3.4 3.4 0 01-6.8 0zM8 10.6v3.2"/>',
  globe: '<circle cx="8" cy="8" r="5.9"/><path d="M2.1 8h11.8M8 2.1c1.7 1.7 2.5 3.8 2.5 5.9S9.7 12.2 8 13.9C6.3 12.2 5.5 10.1 5.5 8S6.3 3.8 8 2.1z"/>',
  doc: '<path d="M4.2 2.3h4.6l3 3v8.4H4.2z"/><path d="M8.6 2.4v3h3M6.2 8.6h3.6M6.2 10.9h3.6"/>',
  warn: '<path d="M8 2.5L14.3 13.2H1.7z"/><path d="M8 6.4v3.2M8 11.4v.01"/>',
  arrowLeft: '<path d="M12.5 8H4M7 4.5L3.5 8 7 11.5"/>',
  arrowRight: '<path d="M3.5 8H12M9 4.5L12.5 8 9 11.5"/>',
  check: '<path d="M3.4 8.4l3 3 6.2-6.6"/>',
  x: '<path d="M4 4l8 8M12 4l-8 8"/>',
  plus: '<path d="M8 3v10M3 8h10"/>',
  copy: '<rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 5.5V4A1.5 1.5 0 009 2.5H4A1.5 1.5 0 002.5 4v5A1.5 1.5 0 004 10.5h1.5"/>',
  download: '<path d="M8 2.5v7.5M4.7 6.8L8 10l3.3-3.2M3 13h10"/>',
  refresh: '<path d="M13 8a5 5 0 1 1-1.46-3.54"/><path d="M12.3 1.9v2.8H9.5"/>',
  camera: '<path d="M2.6 5.6h2.1l1-1.5h4.6l1 1.5h2.1v7.4h-10.8z"/><circle cx="8" cy="9" r="2.2"/>',
  restore: '<path d="M7.5 4.6 3 8l4.5 3.4zM13 4.6 8.5 8l4.5 3.4z" fill="currentColor"/>',
  save: '<path d="M3.4 2.6h7l3 3v7.8h-10z"/><path d="M5.4 2.6v3.2h4.4V2.6M5.4 13.4V9.2h5.2v4.2"/>',
  coffee: '<path d="M3 5.6h8.5v2.9a3.4 3.4 0 01-3.4 3.4H6.4A3.4 3.4 0 013 8.5z"/><path d="M11.5 6.3h1.2a1.6 1.6 0 010 3.1h-1.2"/><path d="M5.4 2.6v1.4M8 2.6v1.4"/>',
  phone: '<rect x="4.5" y="2.5" width="7" height="11" rx="1.4"/><path d="M7 11.4h2"/>',
  bell: '<path d="M4.6 7a3.4 3.4 0 016.8 0c0 2.4 1 3.4 1.3 3.9H3.3c.3-.5 1.3-1.5 1.3-3.9z"/><path d="M6.7 12.9a1.4 1.4 0 002.6 0"/>',
  info: '<circle cx="8" cy="8" r="5.8"/><path d="M8 7.3v3.5M8 5.2v.01"/>',
  power: '<path d="M8 2.2v5.6"/><path d="M5.1 4.7a4.6 4.6 0 105.8 0"/>',
  arrowUp: '<path d="M8 13V3.4M4.4 7 8 3.4 11.6 7"/>',
  lock: '<rect x="3.5" y="7" width="9" height="6.5" rx="1.4"/><path d="M5.5 7V5a2.5 2.5 0 015 0v2"/>',
  chevronDown: '<path d="M4 6.5 8 10.5l4-4"/>',
  link: '<path d="M6.6 9.4 9.4 6.6"/><path d="M7 4.6 8.2 3.4a2.6 2.6 0 013.7 3.7L10.7 8.3"/><path d="M9 11.4 7.8 12.6a2.6 2.6 0 01-3.7-3.7L5.3 7.7"/>',
  more: '<circle cx="3.5" cy="8" r="1.15" fill="currentColor" stroke="none"/><circle cx="8" cy="8" r="1.15" fill="currentColor" stroke="none"/><circle cx="12.5" cy="8" r="1.15" fill="currentColor" stroke="none"/>',
  share: '<path d="M7 4.4H3.4a1 1 0 00-1 1v7a1 1 0 001 1h7a1 1 0 001-1V8.8"/><path d="M9.7 2.2h4.1v4.1"/><path d="M13.6 2.4 6.7 9.3"/>',
  trash: '<path d="M2.8 4.4h10.4M6.3 4.4V2.8h3.4v1.6M4.3 4.4l.6 8.8h6.2l.6-8.8"/>',
  // AI：四角星 + 小星；Key：钥匙；费用：带 ¥ 的钱币
  sparkle: '<path d="M7 2.4l1.25 3.35L11.6 7l-3.35 1.25L7 11.6 5.75 8.25 2.4 7l3.35-1.25z"/><path d="M12.2 10.2l.5 1.3 1.3.5-1.3.5-.5 1.3-.5-1.3-1.3-.5 1.3-.5z"/>',
  key: '<circle cx="5.3" cy="10.7" r="2.7"/><path d="M7.3 8.7l5.2-5.2M10.4 5.6l1.7 1.7M11.9 4.1l1.5 1.5"/>',
  coin: '<circle cx="8" cy="8" r="5.8"/><path d="M5.9 5.2 8 7.9l2.1-2.7M8 7.9v3.4M6.2 8.9h3.6M6.2 10.3h3.6"/>',
  // 任务步骤状态
  spinner: '<path d="M8 2.2a5.8 5.8 0 105.8 5.8"/>',
  pending: '<circle cx="8" cy="8" r="2.2"/>',
};
const icon = (name, cls = "") => `<svg class="ico${cls ? " " + cls : ""}" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICON[name] || ""}</svg>`;

/* ---------- 外观：主题模式 / 配色方案 / 强调色 ----------
   index.html 已在样式生效前把三项偏好落到 <html> 的 data 属性；这里负责切换与持久化。
   偏好存 localStorage（每台设备各自记忆，局域网手机可与主机不同）；配色取值见 themes.css。 */
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
const MODE_LABEL = { auto: "跟随系统", light: "浅色", dark: "深色" };
const MODE_ICON = { auto: "monitor", light: "sun", dark: "moon" };
let appearanceView = null; // 「设置 → 外观」打开时登记的重绘函数，外观变化（含侧栏快捷切换）后同步选中态

/* 冷启动不白闪：原生窗口里把当前标题栏底色（--chrome）上报给后端存进 config.json（chromeColor），
   下次启动时宿主在创建 WebView2 之前把它设成默认底色，页面画出来之前窗口就是这个颜色（见 webviewbg_windows.go）。
   只在原生窗口里上报（局域网手机各有各的外观，不能覆盖主机的）；值没变不重复写，连点外观选项时合并成一次。 */
const chromeReport = { last: null, timer: 0 }; // last：后端当前存的值（boot 读到配置后才开始上报）
function hexColor(v) {
  v = String(v || "").trim().toLowerCase();
  let m = v.match(/^#([0-9a-f]{3})$/);
  if (m) return "#" + m[1].split("").map(c => c + c).join("");
  if (/^#[0-9a-f]{6}$/.test(v)) return v;
  m = v.match(/^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)/);
  return m ? "#" + m.slice(1, 4).map(n => Math.min(255, +n).toString(16).padStart(2, "0")).join("") : "";
}
function reportChrome() {
  if (typeof window.hostWinClose !== "function" || chromeReport.last === null) return;
  clearTimeout(chromeReport.timer);
  chromeReport.timer = setTimeout(() => {
    const c = hexColor(getComputedStyle(document.documentElement).getPropertyValue("--chrome"));
    if (!c || c === chromeReport.last) return;
    const prev = chromeReport.last;
    chromeReport.last = c;
    api("/api/config", { method: "PUT", body: { chromeColor: c } }).catch(() => { chromeReport.last = prev; });
  }, 400);
}
const Appearance = {
  get() {
    const d = document.documentElement.dataset;
    return { mode: d.themePref || "auto", scheme: d.scheme || "craft", accent: d.accent || "auto" };
  },
  set(patch) {
    const a = { ...this.get(), ...patch };
    const root = document.documentElement;
    root.dataset.themePref = a.mode;
    root.dataset.theme = a.mode === "auto" ? (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark") : a.mode;
    // 换外观时整页一次性换色：先暂停过渡，两帧后恢复（否则按钮、卡片各按自己的过渡时长慢慢变色，参差不齐）
    root.classList.add("no-transition");
    requestAnimationFrame(() => requestAnimationFrame(() => root.classList.remove("no-transition")));
    root.dataset.scheme = a.scheme;
    root.dataset.accent = a.accent;
    try { localStorage.setItem("amh_theme", a.mode); localStorage.setItem("amh_scheme", a.scheme); localStorage.setItem("amh_accent", a.accent); } catch {}
    paintThemeBtn();
    // 卡片上的 MOTD 按明暗取色，切换后重绘
    document.querySelectorAll(".inst-meta[data-motd]").forEach(el => { if (el.dataset.motd) renderMotd(el, el.dataset.motd); });
    if (appearanceView) appearanceView();
    reportChrome();
  },
};
function paintThemeBtn() {
  const btn = $("#theme-btn");
  const m = Appearance.get().mode;
  btn.innerHTML = icon(MODE_ICON[m]) + `<span>${MODE_LABEL[m]}</span>`;
  btn.title = `外观：${MODE_LABEL[m]}（点击切换；配色方案见「设置 → 外观」）`;
}
(function initTheme() {
  const NEXT = { auto: "light", light: "dark", dark: "auto" };
  $("#theme-btn").onclick = () => Appearance.set({ mode: NEXT[Appearance.get().mode] || "auto" });
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => { if (Appearance.get().mode === "auto") Appearance.set({}); });
  paintThemeBtn();
})();

/* 无边框宿主：系统标题栏已去除，由前端顶部标题栏代替。窗口按钮 + 拖动经 WebView2 的 Bind 桥接驱动。
   仅当宿主注入了 hostWin* 函数时启用（浏览器回退时保留系统窗口，什么都不做）。 */
(function initHostChrome() {
  if (typeof window.hostWinClose !== "function") return;
  document.body.classList.add("has-host-chrome");
  const call = fn => { try { window[fn](); } catch (e) {} };
  const maxBtn = $("#wc-max");
  const boxMax = `<svg viewBox="0 0 12 12" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1"><rect x="2" y="2" width="8" height="8"/></svg>`;
  const boxRestore = `<svg viewBox="0 0 12 12" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1"><rect x="2.5" y="3.5" width="6" height="6"/><path d="M4.5 3.5V1.5h6v6H8"/></svg>`;
  const refreshMax = async () => { try { maxBtn.innerHTML = (await window.hostWinIsMax()) ? boxRestore : boxMax; } catch (e) {} };
  $("#wc-min").onclick = () => call("hostWinMin");
  $("#wc-close").onclick = () => call("hostWinClose");
  maxBtn.onclick = () => { call("hostWinMaxToggle"); setTimeout(refreshMax, 80); };
  refreshMax();
  // 标题栏空白处按下 → 发起原生拖动；双击 → 最大化/还原。交互元素（按钮/链接/输入/控制键）除外。
  // 第一下按下就进了系统的拖动循环，松开键被它收走，网页收不到 dblclick；所以双击在 mousedown 里自己认：
  // 离上一下不超过系统双击间隔、屏幕位置几乎没动（拖动过窗口就不算），第二下改成最大化/还原，不再发起拖动。
  const bar = $("#titlebar");
  const onChrome = e => e.target.closest("button, a, input, select, .win-ctrls");
  let dblMs = 500, lastDown = null; // 系统双击间隔（宿主告知，取不到按 Windows 默认 500ms）；上一次按下 { t, x, y }
  if (typeof window.hostDblClickTime === "function") Promise.resolve(window.hostDblClickTime()).then(v => { if (v > 0) dblMs = v; }).catch(() => {});
  bar.addEventListener("mousedown", e => {
    if (e.button !== 0 || onChrome(e)) return;
    e.preventDefault();
    const d = { t: e.timeStamp, x: e.screenX, y: e.screenY };
    if (lastDown && d.t - lastDown.t <= dblMs && Math.abs(d.x - lastDown.x) <= 4 && Math.abs(d.y - lastDown.y) <= 4) {
      lastDown = null; // 第三下重新算起
      call("hostWinMaxToggle"); setTimeout(refreshMax, 80);
      return;
    }
    lastDown = d;
    call("hostWinDrag");
  });
  // 关窗确认：点 ×、Alt+F4、任务栏「关闭窗口」都先到宿主（WM_CLOSE）；没开「最小化到托盘」又有服务器在运行时，
  // 宿主请这里问一声（见 tray_windows.go 的 onClose）。确认框弹出来之后再回一声 hostCloseAck：宿主据此知道页面没失联
  // （弹框出错或页面失联时不回，宿主等满 3 秒后的下一次关窗直接退出，不把窗口困住）
  window.__mdCloseAsk = opts => { closeAskModal(opts || {}); call("hostCloseAck"); };
})();

/* 关窗确认框：tray = 托盘图标在不在（不在就只能停服退出），running = 在运行的服务器台数。
   三选一：最小化到托盘（可顺带记住，以后关窗都这样）/ 停止服务器并退出 / 取消。默认聚焦不丢数据的「最小化到托盘」。
   追加进 #modal-root 而不是整个替换：别的对话框开着时不顶掉它（那边的 Promise 会一直等不到结果），
   同时照样算「弹层打开」（按 1–4 不在底下切页、Esc 不连带关掉 AI 助手面板）。 */
function closeAskModal({ tray = true, running = 0 }) {
  // 焦点落在不丢数据的按钮上：有托盘就是「最小化到托盘」，没有就是「取消」，不落到「退出」上
  const focusSafe = m => (m.querySelector('[data-x="tray"]') || m.querySelector('[data-x="cancel"]')).focus();
  const old = $("#close-ask");
  if (old && !old.dataset.leaving) { focusSafe(old); return; } // 已经开着，不叠第二个
  const prev = document.activeElement;
  const d = document.createElement("div");
  d.className = "modal-mask enter";
  d.id = "close-ask";
  const title = running > 0 ? `有 ${running} 台服务器正在运行` : "有服务器正在运行";
  d.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-labelledby="close-ask-title">
    <h3 id="close-ask-title"><span>${esc(title)}</span></h3>
    <div class="m-body">退出 MineDeck 会先停止${running > 1 ? "它们" : "它"}（保存世界）。${tray ? "最小化到托盘则服务器继续运行，点托盘图标即可恢复窗口。" : "托盘图标不可用，这次只能停止服务器并退出。"}</div>
    ${tray ? `<label class="switch" style="margin-bottom:18px"><input type="checkbox" data-k="remember"><span class="sw"></span>
      <span class="sw-label">以后关闭窗口都最小化到托盘（可在「设置 → 启动与托盘」修改）</span></label>` : ""}
    <div class="m-actions">
      <button class="btn" data-x="cancel">取消</button>
      <button class="btn danger" data-x="quit">停止服务器并退出</button>
      ${tray ? `<button class="btn primary" data-x="tray">最小化到托盘</button>` : ""}
    </div></div>`;
  $("#modal-root").appendChild(d);
  focusSafe(d); // 不然焦点还停在底下页面的按钮上，按回车会点到它
  const close = () => {
    document.removeEventListener("keydown", onKey);
    dismiss(d);
    if (prev && prev.focus) prev.focus();
  };
  const onKey = e => { if (e.key === "Escape") close(); };
  document.addEventListener("keydown", onKey);
  d.addEventListener("mousedown", e => { if (e.target === e.currentTarget) close(); });
  d.querySelector('[data-x="cancel"]').onclick = close;
  d.querySelector('[data-x="quit"]').onclick = () => { close(); try { window.hostWinQuit(); } catch (e) {} };
  const toTray = d.querySelector('[data-x="tray"]');
  if (toTray) toTray.onclick = async () => {
    const remember = d.querySelector('input[data-k="remember"]').checked;
    close();
    if (remember) {
      try {
        await api("/api/config", { method: "PUT", body: { minimizeToTray: true } });
        const sw = document.querySelector('input[data-cfg="minimizeToTray"]'); // 设置页正开着时让开关跟上
        if (sw) sw.checked = true;
      } catch (e) { toast("没能记住这个选择：" + e.message, true); }
    }
    try { window.hostWinHide(); } catch (e) {}
  };
  (toTray || d.querySelector('[data-x="cancel"]')).focus();
}

/* 快捷键：1-4 切换侧栏导航；T 或 / 聚焦控制台输入（MC 聊天习惯） */
document.addEventListener("keydown", e => {
  if (e.ctrlKey || e.altKey || e.metaKey) return;
  const t = e.target;
  const typing = t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT");
  if (typing) return;
  if ($("#modal-root").childElementCount || $("#login-mask")) return; // 弹层打开时不在底下切页
  if (["1", "2", "3", "4"].includes(e.key)) {
    const target = ["#/instances", "#/create", "#/tunnels", "#/settings"][+e.key - 1];
    if ((location.hash || "#/instances") === target) navigate(); else location.hash = target;
  } else if (e.key === "/" || e.key === "t" || e.key === "T") {
    const cmd = $("#cmd-in");
    if (cmd) { e.preventDefault(); cmd.focus(); }
  }
});

/* Minecraft § 色码表 */
const MC_COLORS = { 0:"#000000",1:"#0000AA",2:"#00AA00",3:"#00AAAA",4:"#AA0000",5:"#AA00AA",6:"#FFAA00",7:"#AAAAAA",8:"#555555",9:"#5555FF",a:"#55FF55",b:"#55FFFF",c:"#FF5555",d:"#FF55FF",e:"#FFFF55",f:"#FFFFFF" };

/* 将带 § 色码的 MOTD 渲染进容器（textContent 安全构建）。
   浅色主题下把游戏原亮色压暗以保证对比度（深色容器如 motd-preview 不受影响，
   因为其背景恒为深色，调用时机在页面主题下仍成立——按容器背景取色）。 */
function renderMotd(el, text) {
  const onDark = el.classList.contains("motd-preview") ||
    document.documentElement.dataset.theme !== "light";
  const dim = hex => {
    if (onDark) return hex;
    const n = parseInt(hex.slice(1), 16);
    const f = c => Math.round(c * 0.55);
    return "#" + [(n >> 16) & 255, (n >> 8) & 255, n & 255]
      .map(c => f(c).toString(16).padStart(2, "0")).join("");
  };
  const baseColor = onDark ? "#AAAAAA" : "#595959";
  el.innerHTML = "";
  let style = { color: baseColor, bold: false, italic: false, underline: false, strike: false };
  let span = null;
  const newSpan = () => {
    span = document.createElement("span");
    span.style.color = style.color;
    span.style.fontWeight = style.bold ? "700" : "400";
    span.style.fontStyle = style.italic ? "italic" : "normal";
    span.style.textDecoration = (style.underline ? "underline " : "") + (style.strike ? "line-through" : "");
    el.appendChild(span);
  };
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if ((ch === "§" || ch === "&") && i + 1 < text.length && ch === "§") {
      const c = text[i + 1].toLowerCase();
      i++;
      if (MC_COLORS[c] !== undefined) { style = { color: dim(MC_COLORS[c]), bold: false, italic: false, underline: false, strike: false }; span = null; }
      else if (c === "l") { style.bold = true; span = null; }
      else if (c === "o") { style.italic = true; span = null; }
      else if (c === "n") { style.underline = true; span = null; }
      else if (c === "m") { style.strike = true; span = null; }
      else if (c === "r") { style = { color: baseColor, bold: false, italic: false, underline: false, strike: false }; span = null; }
      continue;
    }
    if (!span) newSpan();
    span.textContent += ch;
  }
  if (!el.childNodes.length) { const d = document.createElement("span"); d.style.color = "#666"; d.textContent = "(预览)"; el.appendChild(d); }
}

/* ---------- 版本比较（支持 1.x.y 与年份版号 26.x 混排） ----------
   逐段解析为整数元组比较：年份版号首段 26 天然大于 1.x 的 1，故 26.x 恒大于 1.x；
   对 1.21-pre1 之类后缀取前导整数（parseInt 容错）。供 properties 版本感知等复用。 */
function verParts(v) { return String(v).split(".").map(s => parseInt(s, 10) || 0); }
function verCmp(a, b) { // a<b → -1 ; a==b → 0 ; a>b → 1
  const pa = verParts(a), pb = verParts(b), n = Math.max(pa.length, pb.length);
  for (let i = 0; i < n; i++) {
    const x = pa[i] || 0, y = pb[i] || 0;
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}
const verGte = (mc, ref) => verCmp(mc, ref) >= 0;
const verLt = (mc, ref) => verCmp(mc, ref) < 0;

/* ---------- 行式控件（HMCL 三件套：普通行 / 可展开摘要行 / 行内嵌编辑器） ----------
   rowControl(opts) → HTMLElement，设置页与常用设置共用。
   opts: { title, desc?, key?, needsRestart?, control?（普通行右侧控件 Node）,
           summary?（可展开行收起时摘要 string|Node）, editor?((row)=>Node 惰性构建), open? } */
function rowControl(opts) {
  const row = document.createElement("div");
  row.className = "row-item" + (opts.editor ? " expandable" : "");
  if (opts.key) row.dataset.k = opts.key;
  const head = document.createElement("div");
  head.className = "ri-head";
  const main = document.createElement("div");
  main.className = "ri-main";
  const title = document.createElement("div");
  title.className = "ri-title";
  title.textContent = opts.title;
  if (opts.needsRestart) {
    const tag = document.createElement("span");
    tag.className = "ri-restart"; tag.textContent = "重启生效";
    title.appendChild(tag);
  }
  main.appendChild(title);
  if (opts.desc) {
    const sub = document.createElement("div");
    sub.className = "ri-sub"; sub.textContent = opts.desc;
    main.appendChild(sub);
  }
  if (opts.key) {
    const kk = document.createElement("div");
    kk.className = "ri-key"; kk.textContent = opts.key;
    main.appendChild(kk);
  }
  head.appendChild(main);
  const right = document.createElement("div");
  right.className = "ri-right";
  if (opts.editor) {
    const sum = document.createElement("span");
    sum.className = "ri-summary";
    if (opts.summary instanceof Node) sum.appendChild(opts.summary);
    else sum.textContent = opts.summary ?? "";
    const chev = document.createElement("span");
    chev.className = "ri-chev"; chev.innerHTML = icon("chevronDown");
    right.append(sum, chev);
  } else if (opts.control) {
    right.appendChild(opts.control);
  }
  head.appendChild(right);
  row.appendChild(head);
  if (opts.editor) {
    const ed = document.createElement("div");
    ed.className = "ri-editor";
    row.appendChild(ed);
    let built = false;
    head.addEventListener("click", () => {
      const open = row.classList.toggle("open");
      if (open && !built) { ed.appendChild(opts.editor(row)); built = true; }
    });
    if (opts.open) { row.classList.add("open"); ed.appendChild(opts.editor(row)); built = true; }
  }
  return row;
}

/* 内存分配控件（滑条 + 精确输入框 + 实时物理内存状态条）：向导 / 导入 / JVM 三处共用。
   在 mount 内渲染一个 id=sliderId 的 range，提交端仍按原 id 读取其 value，故调用方无需改提交逻辑。
   o: { sliderId, min, max, value, totalMb, availMb, hint? } */
function renderMemoryControl(mount, o) {
  const min = o.min, max = Math.max(o.min, o.max);
  const totalMb = o.totalMb || 0;
  let availMb = o.availMb;
  if (!availMb || availMb <= 0) availMb = totalMb;      // 后端拿不到可用内存(返回0)时退化为总量
  const usedMb = Math.max(0, totalMb - availMb);
  const g = mb => (mb / 1024).toFixed(1);
  mount.innerHTML = `
    <div class="mem-head"><span>最大内存 -Xmx</span><b class="mem-gb"></b>
      <span class="mem-mb"><input type="number" class="mem-num" min="${min}" max="${max}" step="256" aria-label="最大内存（MB）"> MB</span></div>
    <input type="range" id="${o.sliderId}" class="mem-range" min="${min}" max="${max}" step="512" aria-label="最大内存">
    <div class="mem-xp" aria-hidden="true"></div>
    <div class="mem-stat"></div>
    ${o.hint ? `<div class="hint">${esc(o.hint)}</div>` : ""}`;
  const range = mount.querySelector(".mem-range");
  const num = mount.querySelector(".mem-num");
  const gb = mount.querySelector(".mem-gb");
  const bar = mount.querySelector(".mem-xp");
  const stat = mount.querySelector(".mem-stat");
  const clamp = v => Math.min(max, Math.max(min, Math.round(+v || min)));
  // 经验条式分段：1 格 = cellMb（默认 512 MB，内存很大时放大到最多约 48 格）；其他程序已用 / 本服分配 / 超出可用
  const cellMb = Math.max(512, Math.ceil((totalMb || 8192) / 48 / 512) * 512);
  const cells = Math.max(8, Math.round((totalMb || 8192) / cellMb));
  bar.innerHTML = "<i></i>".repeat(cells);
  bar.title = `1 格 = ${cellMb} MB`;
  const paint = v => {
    gb.textContent = g(v) + " GB";
    const usedN = totalMb ? Math.min(cells, Math.round(usedMb / cellMb)) : 0;
    const allocN = Math.max(1, Math.round(v / cellMb));
    const availN = Math.max(0, cells - usedN);
    const okN = Math.min(allocN, Math.round(Math.min(availMb, v) / cellMb));
    [...bar.children].forEach((c, i) => {
      c.className = i < usedN ? "used" : i < usedN + okN ? "alloc" : i < usedN + Math.min(allocN, availN) ? "over" : "";
    });
    const over = v > availMb;
    let note = "";
    if (over) note = ` <span class="mem-warn">${icon("warn")} 超过当前可用 ${g(availMb)} GB，可能会卡，或者内存分配失败</span>`;
    else if (v > 16384) note = ` <span class="mem-warn">${icon("warn")} 内存设得太大时，清理内存会让游戏顿一下</span>`;
    stat.innerHTML = totalMb
      ? `其他程序已用 ${g(usedMb)} GB · 本服分配 <b>${g(v)} GB</b> / 共 ${g(totalMb)} GB · 1 格 = ${cellMb} MB${note}`
      : `本服分配 <b>${g(v)} GB</b>`;
  };
  const sync = v => { v = clamp(v); range.value = v; num.value = v; paint(v); };
  range.oninput = () => sync(range.value);
  num.oninput = () => { range.value = clamp(num.value); paint(+range.value); };
  num.onchange = () => sync(num.value);
  sync(o.value);
}

/* ---------- 未保存修改：快照比对 + 吸底保存条 ----------
   read() 返回当前表单的 {键: 值字符串} 快照，与基线逐键比对得出未保存项数。
   saveBar() 产出吸底保存条；wireSaveBar() 绑定状态刷新 / 保存 / 放弃修改，并登记切页守卫 dirtyCheck。
   save() 失败时抛错（由这里统一 toast），成功后以新快照为基线。 */
/* 把一个「未保存项数」来源并入当前视图的切页守卫（切页时 navigate 会清空） */
function addDirtySource(gen, fn) {
  if (stale(gen)) return;
  const prev = dirtyCheck;
  dirtyCheck = prev ? () => prev() + fn() : fn;
}

/* 单个输入框 +「保存」按钮：值没变时按钮置灰，回车即保存；未保存的改动同样受切页守卫保护 */
function bindSaveInput(input, btn, save, gen) {
  let base = input.value;
  const paint = () => { btn.disabled = input.value === base; };
  const go = async () => {
    if (input.value === base) return;
    btn.disabled = true;
    try { await save(input.value); base = input.value; } catch (e) { if (!e.silent) toast(e.message, true); } // silent：错误已就近显示在字段下方
    paint();
  };
  input.addEventListener("input", paint);
  input.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); go(); } });
  btn.onclick = go;
  addDirtySource(gen, () => (input.isConnected && input.value !== base ? 1 : 0));
  paint();
}

function saveBar(saveText, note = "") {
  return `<div class="save-bar sticky">
    <span class="sb-state"><span class="dot" aria-hidden="true"></span><span class="sb-text">所有修改已保存</span></span>${note}
    <span class="grow"></span>
    <button class="btn ghost" data-sb="discard" hidden>放弃修改</button>
    <button class="btn primary" data-sb="save" disabled>${icon("save")} ${esc(saveText)}</button>
  </div>`;
}
function wireSaveBar(root, gen, read, save) {
  const bar = $(".save-bar.sticky", root);
  const saveBtn = $('[data-sb="save"]', bar);
  let base = read();
  const diff = () => {
    const cur = read();
    return [...new Set([...Object.keys(base), ...Object.keys(cur)])].filter(k => (base[k] ?? "") !== (cur[k] ?? "")).length;
  };
  const paint = () => {
    const n = diff();
    const wasDirty = bar.classList.contains("dirty");
    bar.classList.toggle("dirty", n > 0);
    $(".sb-text", bar).textContent = n ? `${n} 项修改未保存` : "所有修改已保存";
    saveBtn.disabled = n === 0;
    const discard = $('[data-sb="discard"]', bar);
    discard.hidden = n === 0;
    if (n > 0 && !wasDirty) playClass(discard, "swap-in"); // 「放弃修改」出现时轻轻换场
  };
  root.addEventListener("input", paint);
  root.addEventListener("change", paint);
  $('[data-sb="discard"]', bar).onclick = () => { dirtyCheck = null; navigate(); }; // 按服务器上的当前值重绘
  saveBtn.onclick = async () => {
    saveBtn.disabled = true;
    try { if ((await save()) !== false && bar.isConnected) base = read(); } catch (e) { toast(e.message, true); } // save 返回 false：用户取消了，表单仍算没保存
    if (bar.isConnected) paint(); // 保存途中已切走：视图已换，不再读写旧表单
  };
  addDirtySource(gen, () => (bar.isConnected ? diff() : 0));
  paint();
  return paint; // 程序化改值（不触发 input 事件）后由调用方手动刷新
}

/* 下拉菜单（fixed 定位，点外部 / Esc 关闭，再点触发按钮收起；下方放不下时向上展开）。
   进场自锚点方向轻展开（160ms），离场更快更短（120ms）。
   items: {label, icon, onClick, danger?, disabled?, note?} | {sep: true} | {header: "…"} */
function dropMenu(anchor, items) {
  const open = document.querySelector(".dropmenu:not(.leave)");
  if (open && open._anchor === anchor) { open._close(); return; }
  document.querySelectorAll(".dropmenu:not(.leave)").forEach(x => x._close ? x._close() : x.remove());
  const m = document.createElement("div");
  m.className = "dropmenu enter";
  m.setAttribute("role", "menu");
  m._anchor = anchor;
  items.forEach(it => {
    if (it.sep) { m.insertAdjacentHTML("beforeend", `<div class="dm-sep"></div>`); return; }
    if (it.header) { m.insertAdjacentHTML("beforeend", `<div class="dm-label">${esc(it.header)}</div>`); return; }
    const b = document.createElement("button");
    b.type = "button";
    b.setAttribute("role", "menuitem");
    if (it.danger) b.className = "danger";
    b.disabled = !!it.disabled;
    b.innerHTML = `${icon(it.icon || "folder")}<span>${esc(it.label)}</span>`;
    b.onclick = e => { e.stopPropagation(); close(); it.onClick(); };
    m.appendChild(b);
    if (it.note) m.insertAdjacentHTML("beforeend", `<div class="dm-note">${esc(it.note)}</div>`);
  });
  document.body.appendChild(m);
  const r = anchor.getBoundingClientRect();
  const left = Math.max(8, Math.min(r.right - m.offsetWidth, innerWidth - m.offsetWidth - 8));
  m.style.left = left + "px";
  const h = m.offsetHeight;
  const up = r.bottom + 4 + h > innerHeight - 8;
  m.style.top = (up ? Math.max(8, r.top - h - 4) : r.bottom + 4) + "px";
  m.style.setProperty("--origin", `${up ? "bottom" : "top"} ${left + m.offsetWidth / 2 < r.left + r.width / 2 ? "left" : "right"}`);
  const onDoc = e => { if (!m.contains(e.target) && !anchor.contains(e.target)) close(); };
  const onKey = e => { if (e.key === "Escape") { close(); anchor.focus(); } };
  function close(instant) {
    document.removeEventListener("click", onDoc); document.removeEventListener("keydown", onKey);
    if (instant) m.remove(); else dismiss(m, 120);
  }
  m._close = close;
  setTimeout(() => { document.addEventListener("click", onDoc); document.addEventListener("keydown", onKey); }, 0);
}

/* server.properties 目录已数据化至 props-catalog.js（window.PROPS_CATALOG，约 60 项，含版本/gamerule 元数据） */

/* ---------- 路由 ---------- */
const Main = () => $("#main");
let consoleES = null; // 当前 SSE 连接
let pollTimer = null;
let prTimer = null;   // 控制台在线玩家延迟刷新定时器（离开视图时清理）
let lastAnimKey = null; // 页级入场动画的身份键（跨页判定用）
let keysBefore = null;  // 切页前各处键帽的位置（详情标签 / 设置导航在异步重绘后据此滑动）
let samePage = false;   // 本次切换是否只是页内（同一实例换标签、设置换分区）

/* 视图代号：每次切页 +1。异步渲染在 await 之后先核对代号，过期即放弃（不再挂定时器、不再登记守卫），
   否则快速切页时旧视图的轮询会在 await 返回后才挂上，覆盖新视图的 pollTimer 并永远清不掉。 */
let viewGen = 0;
const stale = gen => gen !== viewGen;
function startPoll(fn, ms, gen) {
  if (stale(gen)) return;
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(() => { if (!stale(gen)) fn(); }, ms);
}

// 向导与导入页也会在同一路由内切换，异步响应必须归属于具体的那一次页面。
function beginCreateView() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  return ++viewGen;
}

async function refreshTaskList(slot, gen) {
  if (stale(gen) || !slot.isConnected || slot._loading) return;
  slot._loading = true;
  try {
    const list = await api("/api/tasks");
    if (stale(gen) || !slot.isConnected) return;
    const html = list.length ? `<h2>部署与下载任务</h2>` + list.map(t => {
      const state = !t.ended ? (t.label || "进行中") : t.error ? "失败"
        : t.pendingCount ? `已完成，${t.pendingCount} 个文件待处理`
        : t.warningCount ? "已完成，有提示" : "已完成";
      return `<div class="java-row"><div style="flex:1;min-width:0"><b>${esc(t.title)}</b><div class="sub">${esc(state)}</div></div>
        <a class="btn sm" href="#/task/${encodeURIComponent(t.id)}" aria-label="${t.ended ? (t.pendingCount ? "去处理待决文件" : "查看结果") : "查看进度"} ${esc(t.title)} ${esc(t.id)}">${t.ended ? (t.pendingCount ? "去处理" : "查看结果") : "查看进度"}</a></div>`;
    }).join("") : "";
    if (slot._html !== html) { slot.innerHTML = html; slot._html = html; }
    slot.hidden = !list.length;
  } catch (e) {
    if (!stale(gen) && slot.isConnected && slot._html === undefined) {
      slot.hidden = false;
      slot.textContent = "暂时无法获取后台任务，稍后自动重试：" + e.message;
    }
  } finally { slot._loading = false; }
}

/* 未保存修改守卫：表单类视图登记 dirtyCheck（返回未保存项数），切页 / 切 tab / 刷新前先确认 */
let dirtyCheck = null;
let curHash = null;
let guardOpen = false;
window.addEventListener("beforeunload", e => { if (dirtyCheck && dirtyCheck() > 0) { e.preventDefault(); e.returnValue = ""; } });

async function navigate() {
  const hash = location.hash || "#/instances";
  if (guardOpen) { if (curHash) history.replaceState(null, "", curHash); return; }
  const unsaved = dirtyCheck ? dirtyCheck() : 0;
  if (unsaved > 0) {
    if (curHash && hash !== curHash) history.replaceState(null, "", curHash); // 先退回原地址，确认放弃后再前往
    guardOpen = true;
    const ok = await confirmModal({
      title: "放弃未保存的修改？", danger: true,
      body: `当前页面还有 <b>${unsaved}</b> 项修改没有保存，离开后这些修改会丢失。`,
      okText: `放弃 ${unsaved} 项修改`, cancelText: "继续编辑",
    });
    guardOpen = false;
    if (!ok) return;
    if ((location.hash || "#/instances") !== hash) history.replaceState(null, "", hash);
  }
  dirtyCheck = null;
  appearanceView = null;
  curHash = hash;
  viewGen++;
  if (consoleES) { consoleES.close(); consoleES = null; }
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  if (prTimer) { clearTimeout(prTimer); prTimer = null; }
  document.querySelectorAll(".dropmenu").forEach(x => x._close ? x._close(true) : x.remove());
  keysBefore = keyRects();
  const m = hash.match(/^#\/([a-z]+)(?:\/(.+))?$/);
  const view = m ? m[1] : "instances";
  const arg = m && m[2] ? decodeURIComponent(m[2]) : null;
  // 侧栏高亮：详情归「服务器」、部署任务归「新建服务器」，进入子页面时仍知道自己在哪个区
  const navKey = view === "inst" ? "instances" : view === "task" ? "create" : view;
  document.querySelectorAll("#sidebar .nav-item").forEach(a => {
    const on = a.dataset.nav === navKey;
    a.classList.toggle("active", on);
    if (on) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current");
  });
  slideKeys(keysBefore, ["nav"]); // 侧栏键帽从旧位置滑到新位置
  const mainEl = Main();
  // 页级入场动画只在真正跨页时重播：设置分区切换走 set-body 局部动效、详情 tab 走 tab-body 局部动效
  const animKey = view === "inst" && arg ? "inst/" + arg.split("/")[0] : view === "task" ? "task/" + arg : view;
  samePage = animKey === lastAnimKey;
  if (!samePage) playClass(mainEl, "view-enter"); // 重触发跨页入场（淡入 + 上移 8px，320ms）
  lastAnimKey = animKey;
  if (view === "create") renderCreate();
  else if (view === "inst" && arg) { const sl = arg.indexOf("/"); sl >= 0 ? renderDetail(arg.slice(0, sl), arg.slice(sl + 1)) : renderDetail(arg); }
  else if (view === "settings") renderSettings(arg);
  else if (view === "tunnels") renderTunnels();
  else if (view === "task" && arg) renderTaskPage(arg);
  else renderInstances();
}
window.addEventListener("hashchange", navigate);
/* 点指向当前地址的站内链接（侧栏当前项、面包屑等）：hash 不变不会触发 hashchange，
   手动重绘回到该页起点（如从向导中途回到「新建服务器」入口） */
document.addEventListener("click", e => {
  const a = e.target.closest('a[href^="#/"]');
  if (a && !e.defaultPrevented && a.getAttribute("href") === (location.hash || "#/instances")) { e.preventDefault(); navigate(); }
});

/* 一键开服：Paper 最新正式版 + 推荐设置（空状态入口） */
let quickBusy = false; // 防重入：代理慢时接口耗时较长，避免用户以为「没反应」而重复点击创建出多台
async function quickStart() {
  const gen = viewGen;
  if (quickBusy) { toast("正在创建，请稍候…"); return; }
  const ok = await confirmModal({ title: "一键开服", okText: "同意并开始", body: "将创建一台 <b>Paper 最新正式版</b> 服务器，采用推荐设置（离线模式、允许飞行、默认端口 25565）。<br>继续即表示同意 <a href='https://aka.ms/MinecraftEULA' target='_blank'>Minecraft EULA</a>。" });
  if (!ok) return;
  quickBusy = true;
  try {
    const app = await api("/api/app");
    const vers = await api("/api/mcversions?core=paper&snapshots=0");
    if (!vers || !vers.length) throw new Error("暂时获取不到 Paper 版本，请改用向导手动创建");
    const mc = (vers.find(v => v.latest) || vers[0]).id;
    const builds = await api(`/api/builds?core=paper&mc=${encodeURIComponent(mc)}`);
    if (!builds || !builds.length) throw new Error("暂时获取不到 Paper 构建，请改用向导手动创建");
    const build = (builds.find(b => b.recommended) || builds[0]).id;
    const mem = Math.max(2048, Math.min(4096, (app.ramMb || 8192) - 2048));
    const r = await api("/api/instances", { method: "POST", body: {
      name: "我的服务器", core: "paper", mc, build, root: "",
      xmxMb: mem, port: 25565, eula: true, onlineMode: false, allowFlight: true,
      difficulty: "easy", gamemode: "survival", motd: "MineDeck 开服 · 一起来玩",
    } });
    toast(`正在部署 Paper ${mc}`);
    if (!stale(gen)) location.hash = `#/task/${r.taskId}`;
  } catch (e) { toast(e.message, true); }
  finally { quickBusy = false; }
}

/* ---------- 视图：实例列表 ---------- */
/* 时长秒 → 2h13m / 5m / 45s */
function fmtDur(sec) {
  sec = Math.max(0, sec | 0);
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60);
  if (h) return `${h}h${m}m`;
  if (m) return `${m}m`;
  return `${sec}s`;
}

/* 「打开文件夹」菜单项：按核心类型列出可用子目录（实例卡「···」与详情页「打开目录」共用） */
function folderItems(info) {
  const isProxy = info.kind === "proxy";
  const hasMods = ["fabric", "forge", "neoforge", "mohist", "banner"].includes(info.core);
  const hasPlugins = ["paper", "purpur", "leaves", "folia", "mohist", "banner", "velocity", "waterfall"].includes(info.core);
  const dirs = [["", "folder", "实例目录"]];
  if (hasMods) dirs.push(["mods", "package", "模组 mods"]);
  if (hasPlugins) dirs.push(["plugins", "plug", "插件 plugins"]);
  if (!isProxy) dirs.push(["world", "globe", "世界存档"], ["logs", "doc", "日志 logs"], ["crash-reports", "warn", "崩溃报告"]);
  else dirs.push(["logs", "doc", "日志 logs"]);
  return dirs.map(([sub, ic, label]) => ({
    icon: ic, label,
    onClick: () => api(`/api/instances/${encodeURIComponent(info.name)}/opendir${sub ? "?sub=" + encodeURIComponent(sub) : ""}`, { method: "POST" })
      .catch(e => toast(e.message, true)),
  }));
}

/* 删除实例：标题点名、按钮写明后果、默认聚焦「保留」。
   不删文件 = 只移出列表：文件全留着（后端把 instance.json 改名留底，下次启动不会再冒出来），以后能用「接管已有服务器」加回来。
   接管进来的服务器，文件夹是用户自己原来的：默认不勾「同时删除文件夹」，并写出是哪个文件夹。 */
async function deleteInstance(info) {
  const name = info.name;
  const okText = files => files ? "删除服务器" : "移出列表";
  const p = confirmModal({
    title: `删除服务器「${name}」？`, danger: true, okText: okText(!info.adopted), cancelText: "保留", icon: "trash",
    body: "已有的世界备份不受影响，仍保存在备份目录中。",
    extra: `<label class="switch" style="margin-bottom:4px"><input type="checkbox" data-k="files"${info.adopted ? "" : " checked"}><span class="sw"></span>
        <span class="sw-label">同时删除服务器文件夹（含世界存档，无法恢复）</span></label>
      <div class="hint" style="margin:0 0 16px 48px">${info.adopted ? `这台服务器是接管进来的，文件夹是你原来的那个：${esc(info.dir)}<br>` : ""}不勾选就只是移出列表，文件原样留在磁盘上。以后想加回来，用「新建服务器 → 接管已有服务器」选这个文件夹。</div>`,
  });
  // 按钮上的字跟着勾选变：不删文件时写「移出列表」，不让人以为点了会删东西
  const box = $("#modal-root input[data-k=files]"), ok = $("#modal-root [data-x=ok]");
  if (box && ok) box.addEventListener("change", () => { ok.textContent = okText(box.checked); });
  const r = await p;
  if (!r) return false;
  try {
    await api(`/api/instances/${encodeURIComponent(name)}?files=${r.files ? 1 : 0}`, { method: "DELETE" });
    toast(r.files ? `已删除「${name}」` : `已把「${name}」移出列表，文件还在原处`);
    return true;
  } catch (e) { toast(e.message, true); return false; }
}

/* 运行状态：像素状态灯 + 文字（不能只靠颜色） */
const ST_TEXT = { running: "运行中", starting: "启动中…", stopping: "正在停止…", stopped: "已停止" };
const stPill = (st, meta = "") => `<span class="st-pill ${st}"><span class="dot ${st}"></span><span class="st-label">${ST_TEXT[st] || esc(st)}</span>${meta}</span>`;
/* 状态切换的动效：胶囊从旧颜色过渡到新颜色（220ms）、文字上下换场、进入「运行中」时状态灯放一圈像素波纹。
   pill 须是刚按新状态渲染好的胶囊；from 为旧状态 */
function animatePill(pill, from, to) {
  if (!pill || !from || from === to || reducedMotion()) return;
  const dot = pill.querySelector(".dot"), label = pill.querySelector(".st-label");
  pill.classList.replace(to, from); dot.classList.replace(to, from); // 先退回旧颜色，回流后再切，颜色过渡才看得见
  void pill.offsetWidth;
  pill.classList.replace(from, to); dot.classList.replace(from, to);
  const old = document.createElement("span");
  old.className = "st-label out";
  old.textContent = ST_TEXT[from] || from;
  pill.insertBefore(old, label);
  old.addEventListener("animationend", () => old.remove(), { once: true });
  setTimeout(() => old.remove(), 400);
  label.classList.add("in");
  if (to === "running") playClass(dot, "ping");
}

/* 经验条：n 格，前 on 格点亮；head = 最前面那一格闪烁（进行中）。--n 供「忙碌」态逐格错峰 */
const xpBar = (n, on, cls = "", head = false) =>
  `<div class="xp${cls ? " " + cls : ""}" aria-hidden="true">${Array.from({ length: n }, (_, i) =>
    `<i class="${i < on ? "on" : ""}${head && i === on - 1 ? " head" : ""}" style="--n:${i}"></i>`).join("")}</div>`;

/* 实例卡的状态行：运行中 = 在线人数经验条（每格 1 人，人数上限大于 20 时每格多人）+ 运行时长；
   启动中 = 按已用时间推进的分段条（每 3 秒一格），最前一格闪烁；正在停止 = 亮格扫过的忙碌条；已停止不显示 */
function statRow(i) {
  if (i.status === "running" && i.kind !== "proxy") {
    const max = Math.max(1, i.maxPlayers || 20), per = max <= 20 ? 1 : Math.ceil(max / 20), n = Math.ceil(max / per);
    const on = Math.min(n, Math.ceil((i.onlineCount || 0) / per));
    return `<div class="xp-row" title="在线人数（每格 ${per} 人）">${icon("player")}<b>${i.onlineCount}/${i.maxPlayers}</b>${xpBar(n, on, "success")}<span class="num">${icon("clock")} ${fmtDur(i.uptimeSec)}</span></div>`;
  }
  if (i.status === "running") return `<div class="xp-row">${icon("clock")}<span class="num">已运行 ${fmtDur(i.uptimeSec)}</span></div>`;
  if (i.status === "starting") {
    const on = Math.min(19, 1 + Math.floor((i.uptimeSec || 0) / 3));
    return `<div class="xp-row" title="正在启动">${icon("clock")}<b>启动中</b>${xpBar(20, on, "warn run", true)}<span class="num">${fmtDur(i.uptimeSec)}</span></div>`;
  }
  if (i.status === "stopping") return `<div class="xp-row" title="正在保存世界并停止">${icon("clock")}<b>保存世界</b>${xpBar(20, 0, "warn busy")}</div>`;
  return "";
}

/* 列表骨架占位：首屏加载态（reduced-motion 下由全局关停微光，静态显示凹陷块）。 */
const skeletonCards = (n = 6) => `<div class="cards sk-cards">` + Array.from({ length: n }, () => `
  <div class="sk-card" aria-hidden="true">
    <div class="sk-head"><span class="sk av"></span><div><span class="sk" style="width:55%"></span><span class="sk" style="width:30%;height:10px"></span></div></div>
    <div class="sk-badges"><span class="sk sk-chip"></span><span class="sk sk-chip" style="width:76px"></span></div>
    <div class="sk" style="width:64%;height:10px"></div>
    <div class="sk-actions"><span class="sk sk-btn"></span><span class="sk sk-btn" style="width:52px"></span></div>
  </div>`).join("") + `</div>`;

/* 紧凑单列表（版本/构建）行骨架 */
const skeletonRows = (n = 7) => Array.from({ length: n }, () =>
  `<div class="sk-row"><span class="sk" style="width:38%;height:13px"></span><span class="sk" style="width:52px;height:12px"></span></div>`).join("");

async function renderInstances() {
  const gen = viewGen;
  // 标题与搜索/排序工具栏包进 #inst-chrome：没有实例时整块隐藏（空态自身即整页主体），
  // 常驻 DOM 只切换 hidden，轮询重绘不重建输入框、不丢焦点
  Main().innerHTML = `<div id="inst-chrome" hidden>` +
    pageHead("服务器", "", `<a class="btn primary" href="#/create">${icon("plus")} 新建服务器</a>`, "", "", "inst-summary") + `
    <div class="toolbar">
      <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="inst-q" placeholder="搜索服务器名称" aria-label="搜索服务器名称" style="width:280px"></div>
      <select id="inst-sort" style="width:180px" aria-label="排序方式"><option value="recent">排序：最近创建</option><option value="running">排序：运行中优先</option><option value="name">排序：名称</option></select>
    </div></div>
    <section id="task-list" hidden style="margin-bottom:24px"></section>
    <div id="inst-cards">${skeletonCards(4)}</div>`;
  const taskList = $("#task-list");
  let lastSig = "", curList = [], prevByName = null; // prevByName：上次渲染时各实例的数据（判定状态切换、播切换动效）
  const cardHTML = (i, n, enter, prev) => {
    const proxy = i.kind === "proxy";
    const gb = (i.xmxMb / 1024).toFixed(1) + "G";
    const href = `#/inst/${encodeURIComponent(i.name)}`;
    const changed = prev && prev.status !== i.status;
    const stat = statRow(i), prevStat = changed ? statRow(prev) : "";
    // 状态行展开 / 收起（grid 0fr ↔ 1fr）：状态切换时先按旧样子渲染（无 → 有：先收起；有 → 无：先展开旧内容），渲染后再切
    const collapse = stat
      ? `<div class="collapse${changed && !prevStat ? " closed" : ""}" data-stat><div>${stat}</div></div>`
      : `<div class="collapse${changed && prevStat ? "" : " closed"}" data-stat${changed && prevStat ? " data-close" : ""}><div>${changed ? prevStat : ""}</div></div>`;
    // 删除、打开文件夹收进「···」：危险操作不与「启动」同排同尺寸，多一步才够得着；「管理」「···」是第三级按钮。
    // 每张卡片都有同名的「启动」「管理」「···」：可访问名称带上服务器名（「启动 生存服」），读屏、自动化按名字找得到是哪一台
    const nm = esc(i.name);
    return `
    <div class="card st-${i.status}${enter ? " item-enter" : ""}" style="--i:${n}" data-name="${esc(i.name)}">
      <div class="inst-head">${avatarOf(i.core)}<div class="inst-title"><a class="inst-name" href="${href}">${esc(i.name)}</a><div class="inst-sub">${proxy ? "群组代理" : "游戏服务器"}</div></div>${stPill(i.status)}</div>
      <div class="badges">${coreBadge(i.core)}<span class="badge">${proxy ? "v" : "MC "}${esc(i.mc)}</span>${proxy ? `<span class="badge">代理端</span>` : ""}</div>
      <div class="kv inst-config">${proxy ? "" : `<span>端口<b>:${i.port}</b></span>`}<span>内存<b>${gb}</b></span><span>Java<b>${i.javaMajor}</b></span></div>
      ${collapse}
      <div class="inst-meta" data-motd="${esc(i.motd || "")}"></div>
      <div class="inst-actions">
        ${i.status === "stopped" ? `<button class="btn sm primary" data-act="start" aria-label="启动 ${nm}">${icon("play")} 启动</button>`
          : `<button class="btn sm" data-act="stop" aria-label="停止 ${nm}" ${i.status === "stopping" ? "disabled" : ""}>${icon("stop")} 停止</button>`}
        <a class="btn sm ghost" href="${href}" aria-label="管理 ${nm}">管理</a>
        <span class="grow"></span>
        <button class="btn sm ghost icon-only" data-act="more" title="更多操作" aria-label="更多操作 ${nm}" aria-haspopup="menu">${icon("more")}</button>
      </div>
    </div>`;
  };
  // 状态切换的卡片：胶囊变色 + 文字换场 + 波纹、在线人数格逐个点亮、操作按钮换场、状态行展开 / 收起
  const animateChanges = prev => {
    if (!prev || reducedMotion()) return;
    $("#inst-cards").querySelectorAll(".card").forEach(card => {
      const i = curList.find(x => x.name === card.dataset.name), p = prev.get(card.dataset.name);
      if (!i || !p || p.status === i.status) return;
      animatePill(card.querySelector(".st-pill"), p.status, i.status);
      if (i.status === "running") card.querySelectorAll(".xp i.on").forEach((c, n) => { c.style.animationDelay = (120 + n * 40) + "ms"; c.classList.add("lit"); });
      playClass(card.querySelector('[data-act="start"], [data-act="stop"]'), "swap-in");
      const box = card.querySelector("[data-stat]");
      void box.offsetHeight;
      if (box.hasAttribute("data-close")) box.classList.add("closed"); else box.classList.remove("closed");
    });
  };
  const applyView = (enter = false) => {
    const q = ($("#inst-q").value || "").trim().toLowerCase();
    const sort = $("#inst-sort").value;
    let list = curList.filter(i => !q || i.name.toLowerCase().includes(q));
    if (sort === "name") list = list.slice().sort((a, b) => a.name.localeCompare(b.name));
    else if (sort === "running") list = list.slice().sort((a, b) => (b.status === "running") - (a.status === "running"));
    const prev = prevByName;
    prevByName = new Map(curList.map(i => [i.name, i]));
    if (!list.length) { $("#inst-cards").innerHTML = `<div class="empty" style="padding:40px 0">没有匹配「${esc(q)}」的服务器</div>`; return; }
    $("#inst-cards").innerHTML = `<div class="cards">` + list.map((i, n) => cardHTML(i, n, enter, prev && prev.get(i.name))).join("") + `</div>`;
    animateChanges(prev);
    $("#inst-cards").querySelectorAll(".inst-meta").forEach(el => { if (el.dataset.motd) renderMotd(el, el.dataset.motd); });
    $("#inst-cards").querySelectorAll("[data-act]").forEach(b => b.onclick = async ev => {
      ev.stopPropagation();
      const name = b.closest(".card").dataset.name, act = b.dataset.act;
      const info = curList.find(x => x.name === name);
      if (act === "more") {
        const busy = info.status !== "stopped";
        dropMenu(b, [
          { header: "打开文件夹" }, ...folderItems(info), { sep: true },
          { icon: "share", label: "交接给其他电脑…", disabled: busy, note: busy ? "需先停止服务器" : "",
            onClick: () => transferExportModal(info) },
          { sep: true },
          { icon: "trash", label: "删除服务器…", danger: true, disabled: busy, note: busy ? "需先停止服务器" : "",
            onClick: async () => { if (await deleteInstance(info)) { lastSig = ""; draw(); } } },
        ]);
        return;
      }
      try {
        if (act === "start") { b.disabled = true; await api(`/api/instances/${encodeURIComponent(name)}/start`, { method: "POST" }); toast(`「${name}」正在启动`); }
        else if (act === "stop") { b.disabled = true; await api(`/api/instances/${encodeURIComponent(name)}/stop`, { method: "POST" }); toast(`「${name}」正在停止并保存世界`); }
      } catch (e) { toast(e.message, true); }
      lastSig = ""; draw();
    });
    // 整卡可点：点卡片空白处进入详情（卡内按钮 / 链接各自处理）
    $("#inst-cards").querySelectorAll(".card").forEach(c => c.onclick = e => {
      if (e.target.closest("button, a")) return;
      location.hash = `#/inst/${encodeURIComponent(c.dataset.name)}`;
    });
  };
  let first = true;
  const draw = async () => {
    refreshTaskList(taskList, gen);
    let list;
    try { list = await api("/api/instances"); } catch (e) { if (!stale(gen)) $("#inst-cards").innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    curList = list;
    const running = list.filter(i => i.status === "running").length;
    const online = list.reduce((s, i) => s + (i.onlineCount || 0), 0);
    $("#inst-summary").innerHTML = list.length ? `共 ${list.length} 台${SEP}运行中 ${running}${online ? `${SEP}在线 ${online} 人` : ""}` : "";
    if (!list.length) {
      $("#inst-chrome").hidden = true; // 没有实例时不显示「服务器」标题与搜索/排序
      $("#inst-cards").classList.add("page-fill");
      // 一个主行动 + 一个安静的次要入口（两个同权重按钮等于没有主按钮）
      $("#inst-cards").innerHTML = `<div class="empty${first ? " sec-enter" : ""}">${MDPixel.emptySVG(240)}
        <div class="empty-title">还没有服务器</div>
        一键创建一台推荐配置的服务器，Java 与服务端会自动下载。<br>
        <button class="btn primary" id="quick-start">${icon("bolt")} 一键开服（Paper 最新正式版）</button><br>
        <a class="quiet-link" href="#/create">或使用向导，自选核心与版本 →</a></div>`;
      $("#quick-start").onclick = quickStart; lastSig = "empty"; first = false; return;
    }
    $("#inst-chrome").hidden = false;
    $("#inst-cards").classList.remove("page-fill");
    // 签名：数据无实质变化则跳过重绘（消除周期性闪烁与焦点丢失，也不重播动效）；启动中按秒推进分段条，所以带上时长
    const sig = list.map(i => `${i.name}:${i.status}:${i.status === "running" ? Math.floor(i.uptimeSec / 60) + "/" + i.onlineCount : i.status === "starting" ? Math.floor(i.uptimeSec / 3) : ""}`).join("|") + "#" + $("#inst-q").value + "#" + $("#inst-sort").value;
    if (sig === lastSig) return;
    lastSig = sig;
    // 首屏：骨架淡出（160ms）后卡片逐项入场（错峰 28ms）；之后的重绘不再播入场
    const sk = first && !reducedMotion() ? $("#inst-cards .sk-cards") : null;
    const enter = first;
    first = false;
    if (sk) {
      sk.classList.add("fade-out");
      setTimeout(() => { if (!stale(gen) && sk.isConnected) applyView(enter); }, MOTION.d2 - 40);
    } else applyView(enter);
  };
  $("#inst-q").oninput = () => { lastSig = ""; draw(); };
  $("#inst-sort").onchange = () => { lastSig = ""; draw(); };
  await draw();
  startPoll(draw, 3000, gen);
}

/* ---------- 视图：新建向导 ---------- */
const wiz = { step: 0, core: null, mc: null, build: null, snapshots: false, root: "" };

async function renderCreate() {
  const gen = beginCreateView();
  wiz.step = 0; wiz.core = null; wiz.mc = null; wiz.build = null; wiz.root = "";
  Main().innerHTML = pageHead("新建服务器", "选择一种方式开始", "", "", `四种方式${SEP}Java 与服务端自动下载`) + `
    <section id="task-list" hidden style="margin-bottom:24px"></section>
    <div class="recipe-grid">
      <div class="core-card" id="mode-new" role="button" tabindex="0" aria-label="从零创建" aria-describedby="mode-new-d"><span class="recipe-ico">${icon("plusBox")}</span><div><div class="cn">从零创建</div><div class="cd" id="mode-new-d">选择核心与版本，自动下载 Java 与服务端</div></div></div>
      <div class="core-card" id="mode-import" role="button" tabindex="0" aria-label="导入整合包" aria-describedby="mode-import-d"><span class="recipe-ico">${icon("package")}</span><div><div class="cn">导入整合包</div><div class="cd" id="mode-import-d">支持 Modrinth / CurseForge 整合包，自动识别版本</div></div></div>
      <div class="core-card" id="mode-transfer" role="button" tabindex="0" aria-label="导入交接包" aria-describedby="mode-transfer-d"><span class="recipe-ico">${icon("share")}</span><div><div class="cn">导入交接包</div><div class="cd" id="mode-transfer-d">别人用 MineDeck 导出的 .mdpack：存档、配置、模组全保留，装完直接能启动</div></div></div>
      <div class="core-card full" id="mode-adopt" role="button" tabindex="0" aria-label="接管已有服务器" aria-describedby="mode-adopt-d"><span class="recipe-ico">${icon("folder")}</span><div><div class="cn">接管已有服务器</div><div class="cd" id="mode-adopt-d">以前自己开的服务器，选它的文件夹就能收进来统一管理。文件留在原地，存档和配置都不动</div></div></div>
    </div>`;
  $("#mode-new").onclick = () => drawWizard();
  $("#mode-import").onclick = () => drawImport();
  $("#mode-transfer").onclick = () => drawTransfer();
  $("#mode-adopt").onclick = () => drawAdopt();
  // 卡片可用键盘激活（回车 / 空格）
  ["#mode-new", "#mode-import", "#mode-transfer", "#mode-adopt"].forEach(s => { $(s).onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); $(s).click(); } }; });
  const taskList = $("#task-list");
  await refreshTaskList(taskList, gen);
  startPoll(() => refreshTaskList(taskList, gen), 3000, gen);
}

/* ---------- 视图：接管已有服务器 ----------
   选文件夹 → 后端只读地识别一遍（核心、版本、启动文件、原来分的内存）→ 用户确认或修改 → 后台任务登记。
   只能在本机用：要读这台电脑上的文件夹（后端 instance.adopt* 是仅本机、仅人工）。 */
/* 没有时可以下载便携版的 Java 版本（清华镜像 / Adoptium / Mojang 运行时里有的） */
const JAVA_DOWNLOADABLE = [8, 11, 16, 17, 21, 25];
async function drawAdopt() {
  const gen = beginCreateView();
  const [app, cores, javas] = await Promise.all([
    api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096, config: {} })),
    api("/api/cores").catch(() => []),
    api("/api/javas").catch(() => ({ portable: [], scanned: [] })),
  ]);
  if (stale(gen)) return;
  cores.forEach(c => CORES[c.id] = c);
  const maxMem = Math.max(2048, app.ramMb - 2048);
  let scan = null; // 最近一次识别的结果；改了文件夹没重新识别就不能接管
  Main().innerHTML = pageHead("接管已有服务器", "以前自己开的服务器，选它所在的文件夹，就能收进 MineDeck 统一管理。文件留在原地，存档、模组、插件和配置都不动。", "",
    `<a href="#/create">新建服务器</a>${icon("chevronRight")}<span>接管已有服务器</span>`) + `
    <div class="form-grid">
      <label class="field full"><span>服务器所在的文件夹</span>
        <div class="row" style="gap:8px">
          <input type="text" id="ad-dir" spellcheck="false" autocomplete="off" style="flex:1" aria-label="服务器所在的文件夹" placeholder="如 D:\\MyServer：里面有服务端的 jar，或者 libraries 文件夹">
          <button type="button" class="btn" id="ad-browse">${icon("folder")} 浏览…</button>
          <button type="button" class="btn primary" id="ad-scan">${icon("search")} 识别</button>
        </div>
        <div class="field-err" id="ad-dir-err"></div></label>
    </div>
    <div id="ad-form"></div>
    ${wizFoot("ad-back", "返回", `<button class="btn primary" id="ad-go" disabled>${icon("check")} 接管这台服务器</button>`)}`;
  $("#ad-back").onclick = () => renderCreate();
  const dirErr = msg => { $("#ad-dir").classList.toggle("invalid", !!msg); $("#ad-dir-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  const reset = () => { scan = null; $("#ad-form").innerHTML = ""; $("#ad-go").disabled = true; };
  $("#ad-dir").addEventListener("input", () => { dirErr(""); reset(); });
  $("#ad-dir").addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); $("#ad-scan").click(); } });
  $("#ad-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { $("#ad-dir").value = r.path; dirErr(""); reset(); $("#ad-scan").click(); } }
    catch (e) { toast(e.message, true); }
  };

  const isProxyCore = id => coreKind(id) === "proxy";
  const drawForm = () => {
    const s = scan;
    const known = new Set([...javas.portable, ...javas.scanned].map(j => j.major));
    const majors = [...new Set([...JAVA_DOWNLOADABLE, ...known])].sort((a, b) => a - b);
    $("#ad-form").innerHTML = `
      ${s.notes.length ? `<div class="warn-card w-read" id="ad-notes"><div class="wc-title">${icon("info")} 识别时注意到</div><ul class="wc-list">${s.notes.map(n => `<li>${esc(n)}</li>`).join("")}</ul></div>` : ""}
      <div class="form-grid">
        <label class="field"><span>名称</span><input type="text" id="ad-name" maxlength="40" value="${esc(s.name)}"><div class="field-err" id="ad-name-err"></div></label>
        <label class="field"><span>服务端类型</span><select id="ad-core">
          <option value=""${s.core ? "" : " selected"} disabled>请选择…</option>
          ${cores.map(c => `<option value="${esc(c.id)}"${c.id === s.core ? " selected" : ""}>${esc(c.name)}</option>`).join("")}</select>
          <div class="field-err" id="ad-core-err"></div></label>
        <label class="field"><span id="ad-mc-label">Minecraft 版本</span><input type="text" id="ad-mc" maxlength="40" spellcheck="false" value="${esc(s.mc)}" placeholder="如 1.20.1"><div class="field-err" id="ad-mc-err"></div></label>
        <label class="field"><span>启动文件</span><select id="ad-target">${s.targets.map(t =>
          `<option value="${esc(t.target)}"${t.target === s.launchTarget ? " selected" : ""}>${esc(t.label)}</option>`).join("")}</select></label>
        <div class="field full" id="ad-mem-mount"></div>
        <label class="field full"><span>Java</span><select id="ad-java" aria-label="Java">
          <option value="0">自动：按 Minecraft 版本选，本机没有就下载</option>
          ${majors.map(m => `<option value="${m}">Java ${m}（${known.has(m) ? "本机已有" : "要下载，约 50 MB"}）</option>`).join("")}</select>
          <div class="hint">一般选「自动」。整合包说明里写了要用哪个 Java 的，就按它说的选。接管后也能在「内存 / 启动」里换。</div></label>
      </div>
      <div class="eula-box" id="ad-eula-box" hidden>
        <label class="switch"><input type="checkbox" id="ad-eula"><span class="sw"></span>
          <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft 最终用户许可协议 (EULA)</a></span></label>
        <div class="field-err" id="ad-eula-err"></div>
      </div>
      <div class="hint w-read" id="ad-what" style="margin-bottom:16px;line-height:1.8">
        ${s.managed ? "这个文件夹以前由 MineDeck 管过，原来的名称、内存、定时任务会带回来。<br>" : ""}
        接管时 MineDeck 只在这个文件夹里写自己的 <code>instance.json</code> 和启动脚本 <code>run.bat</code>。原来就有 <code>run.bat</code> 的，先另存为 <code>run.before-minedeck.bat</code>。别的文件都不动。<br>
        原来的服务器还开着的话，请先把它关掉再接管，不然启动时端口会冲突。
        <span id="ad-noworld" hidden><br>${icon("info")} 这个文件夹里没看到存档（world），启动后会生成一个新世界。存档放在别处的话，先确认选对了文件夹。</span>
      </div>`;
    renderMemoryControl($("#ad-mem-mount"), { sliderId: "ad-mem", min: 1024, max: Math.max(maxMem, 1024), value: Math.min(Math.max(s.xmxMb || 4096, 1024), maxMem), totalMb: app.ramMb, availMb: app.availRamMb,
      hint: s.xmxMb ? `原来的启动脚本里分的是 ${(s.xmxMb / 1024).toFixed(1)} GB，已经照着填好` : "模组服建议 4GB 以上。没有模组的小队，2~4GB 足够。" });
    // 代理端没有 EULA，版本是它自己的版本；已经同意过 EULA 的不用再勾
    const paintKind = () => {
      const proxy = isProxyCore($("#ad-core").value);
      $("#ad-eula-box").hidden = proxy || s.eula;
      $("#ad-noworld").hidden = proxy || s.hasWorld; // 代理端本来就没有存档
      $("#ad-mc-label").textContent = proxy ? "代理端版本" : "Minecraft 版本";
    };
    $("#ad-core").onchange = () => { $("#ad-core").classList.remove("invalid"); $("#ad-core-err").innerHTML = ""; paintKind(); };
    // 换了启动文件：核心和版本跟着换成那个文件认出来的
    $("#ad-target").onchange = () => {
      const t = s.targets.find(x => x.target === $("#ad-target").value);
      if (!t) return;
      if (t.core) $("#ad-core").value = t.core;
      if (t.mc) $("#ad-mc").value = t.mc;
      paintKind();
    };
    $("#ad-eula").addEventListener("change", () => { if ($("#ad-eula").checked) { $("#ad-eula-box").classList.remove("invalid"); $("#ad-eula-err").innerHTML = ""; } });
    paintKind();
    $("#ad-go").disabled = false;
  };

  $("#ad-scan").onclick = async () => {
    const dir = $("#ad-dir").value.trim();
    if (!dir) { dirErr("请先选择或填写服务器所在的文件夹"); $("#ad-dir").focus(); return; }
    reset();
    const btn = $("#ad-scan");
    btn.disabled = true; btn.textContent = "识别中…";
    try {
      const found = await api("/api/adopt/scan", { method: "POST", body: { dir } });
      if (stale(gen)) return; // 等待期间已离开
      scan = found;
      dirErr("");
      $("#ad-dir").value = scan.dir;
      drawForm();
      $("#ad-form").scrollIntoView({ behavior: "smooth", block: "nearest" });
    } catch (e) { if (!stale(gen)) dirErr(e.message); }
    finally { if (!stale(gen) && btn.isConnected) { btn.disabled = false; btn.innerHTML = icon("search") + " 识别"; } }
  };

  $("#ad-go").onclick = async () => {
    if (!scan) return;
    const fieldErr = (el, errId, msg) => { el.classList.toggle("invalid", !!msg); $(errId).innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; return !msg; };
    const core = $("#ad-core").value, proxy = isProxyCore(core);
    const okName = fieldErr($("#ad-name"), "#ad-name-err", $("#ad-name").value.trim() ? "" : "请给服务器起个名字");
    const okCore = fieldErr($("#ad-core"), "#ad-core-err", core ? "" : "请选一个最接近的类型：插件服选 Paper，模组服按加载器选");
    const okMC = fieldErr($("#ad-mc"), "#ad-mc-err", $("#ad-mc").value.trim() ? "" : proxy ? "请填代理端的版本，不确定可以填 1" : "请填 Minecraft 版本，如 1.20.1");
    const needEula = !proxy && !scan.eula;
    const okEula = !needEula || $("#ad-eula").checked;
    if (needEula) { $("#ad-eula-box").classList.toggle("invalid", !okEula); $("#ad-eula-err").innerHTML = okEula ? "" : icon("warn") + " 开服前需要同意 Minecraft EULA"; }
    if (!(okName && okCore && okMC && okEula)) {
      const first = !okName ? $("#ad-name") : !okCore ? $("#ad-core") : !okMC ? $("#ad-mc") : $("#ad-eula");
      first.closest(".field, .eula-box").scrollIntoView({ behavior: "smooth", block: "center" });
      first.focus({ preventScroll: true });
      return;
    }
    const target = $("#ad-target").value, picked = scan.targets.find(x => x.target === target);
    $("#ad-go").disabled = true;
    try {
      const r = await api("/api/adopt", {
        method: "POST",
        body: {
          dir: scan.dir, name: $("#ad-name").value.trim(), core, mc: $("#ad-mc").value.trim(),
          build: picked && picked.core === core ? picked.build : "",
          launchTarget: target, xmxMb: +$("#ad-mem").value, javaMajor: +$("#ad-java").value, eula: needEula,
        },
      });
      if (!stale(gen)) location.hash = `#/task/${r.taskId}`;
      else toast("已开始接管，可在新建服务器页查看进度");
    } catch (e) { toast(e.message, true); if (!stale(gen)) $("#ad-go").disabled = false; }
  };
  $("#ad-dir").focus();
}

/* ---------- 视图：整合包导入 ---------- */
async function drawImport() {
  const gen = beginCreateView();
  const app = await api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096, config: {} }));
  if (stale(gen)) return;
  const maxMem = Math.max(2048, app.ramMb - 2048);
  const defMem = Math.min(6144, maxMem);
  let imRoot = "";
  Main().innerHTML = pageHead("导入整合包", "支持 Modrinth (.mrpack) 与 CurseForge (zip)。核心、MC 版本与加载器将从整合包自动识别。", "",
    `<a href="#/create">新建服务器</a>${icon("chevronRight")}<span>导入整合包</span>`) + `
    <div class="form-grid">
      <label class="field full"><span>整合包文件</span><input type="file" id="im-file" accept=".mrpack,.zip"><div class="field-err" id="im-file-err"></div></label>
      <label class="field"><span>实例名称</span><input type="text" id="im-name" value="我的整合包服务器" maxlength="40"></label>
      <label class="field"><span>端口</span><input type="number" id="im-port" value="25565" min="1" max="65535"></label>
      <div class="field full" id="im-mem-mount"></div>
      <label class="field full"><span>存放位置</span>
        <div class="row" style="gap:8px">
          <input type="text" id="im-root" readonly style="flex:1;cursor:default;background:var(--surface-2)" aria-label="存放位置">
          <button type="button" class="btn" id="im-browse">${icon("folder")} 浏览…</button>
        </div></label>
      <label class="field full"><span>本地来源目录（可选）</span>
        <div class="row" style="gap:8px">
          <input type="text" id="im-src" style="flex:1" placeholder="还留着的老服务器目录，魔改模组按原样保留会用到" aria-label="本地来源目录">
          <button type="button" class="btn" id="im-src-browse" aria-label="浏览本地来源目录">${icon("folder")} 浏览…</button>
        </div>
        <div class="hint" style="margin-top:6px">没法自动下载的文件（魔改模组的自定义直链、缺校验值等），只要这个目录里已有对应文件，就按原样保留、不再要求线上匹配；也可以导入后在任务面板里逐个处理。</div></label>
      <div class="field"><label class="switch"><input type="checkbox" id="im-online"><span class="sw"></span>
        <span><span class="sw-label">正版验证</span><div class="sw-desc">默认关闭</div></span></label></div>
      <div class="field"><label class="switch"><input type="checkbox" id="im-flight" checked><span class="sw"></span>
        <span><span class="sw-label">允许飞行</span><div class="sw-desc">整合包强烈建议开启</div></span></label></div>
    </div>
    ${app.cfKeySet ? "" : `<div class="hint" style="margin-bottom:10px">提示：CurseForge 格式整合包下载模组需要 API Key（<a href="https://console.curseforge.com/" target="_blank">免费申请</a>后在「设置 → 下载与网络」填写）。Modrinth 格式不用填。</div>`}
    <div class="eula-box">
      <label class="switch"><input type="checkbox" id="im-eula"><span class="sw"></span>
        <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft EULA</a></span></label>
      <div class="field-err" id="im-eula-err"></div>
    </div>
    ${wizFoot("im-back", "返回", `<button class="btn primary" id="im-go">${icon("bolt")} 导入并部署</button>`)}`;
  $("#im-back").onclick = () => renderCreate(); // 直接重绘（hash 相同不会触发路由，不能用 location.hash）
  renderMemoryControl($("#im-mem-mount"), { sliderId: "im-mem", min: 2048, max: maxMem, value: defMem, totalMb: app.ramMb, availMb: app.availRamMb, hint: "整合包服建议 6GB 以上" });
  const imUpdateRoot = () => { $("#im-root").value = imRoot || app.serversRoot || "程序目录\\servers"; };
  imUpdateRoot();
  $("#im-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { imRoot = r.path; imUpdateRoot(); toast("已选择：" + r.path); } }
    catch (e) { toast(e.message, true); }
  };
  $("#im-src-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { $("#im-src").value = r.path; toast("已选择：" + r.path); } }
    catch (e) { toast(e.message, true); }
  };
  // 行内校验（同向导）：错误显示在对应字段下方，改正后即时消失
  const fileErr = msg => { $("#im-file").classList.toggle("invalid", !!msg); $("#im-file-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  const eulaErr = msg => { $(".eula-box").classList.toggle("invalid", !!msg); $("#im-eula-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  $("#im-file").addEventListener("change", () => fileErr(""));
  $("#im-eula").addEventListener("change", () => { if ($("#im-eula").checked) eulaErr(""); });
  $("#im-go").onclick = async () => {
    const f = $("#im-file").files[0];
    if (!f) { fileErr("请先选择整合包文件（.mrpack 或 .zip）"); $("#im-file").focus(); return; }
    if (f.size > 4 * 1024 ** 3) { fileErr(`这个整合包有 ${(f.size / 1024 ** 3).toFixed(2)} GB，超过了 4 GB 的上限，导不进来。`); $("#im-file").focus(); return; }
    if (!$("#im-eula").checked) { eulaErr("导入前需要同意 Minecraft EULA"); $("#im-eula").focus(); return; }
    $("#im-go").disabled = true;
    $("#im-go").textContent = "上传中…";
    const fd = new FormData();
    fd.append("file", f);
    fd.append("name", $("#im-name").value.trim());
    fd.append("xmxMb", $("#im-mem").value);
    fd.append("port", $("#im-port").value);
    fd.append("eula", "true");
    fd.append("onlineMode", $("#im-online").checked ? "true" : "false");
    fd.append("allowFlight", $("#im-flight").checked ? "true" : "false");
    fd.append("root", imRoot);
    fd.append("localSource", $("#im-src").value.trim());
    try {
      const res = await fetch("/api/import/modpack", { method: "POST", headers: { "X-Token": TOKEN() }, body: fd });
      const j = await res.json();
      if (!j.ok) throw new Error(j.error || "导入失败");
      const pending = (j.data.pack.pending || 0) > 0 ? `，${j.data.pack.pending} 个文件待处理` : "";
      toast(`已识别：${j.data.pack.core} ${j.data.pack.mc}（${j.data.pack.files} 个文件${pending}）` + (pending ? "，完成后再逐个处理" : ""));
      if (!stale(gen)) location.hash = `#/task/${j.data.taskId}`;
    } catch (e) {
      toast(e.message, true);
      if (!stale(gen)) { $("#im-go").disabled = false; $("#im-go").innerHTML = icon("bolt") + " 导入并部署"; }
    }
  };
}

/* ---------- 视图：实例交接包（.mdpack）----------
   别人开不下去的服务器，从卡片菜单「交接给其他电脑」打包发过来；这里选 .mdpack 导入，
   存档、配置、模组全保留，装完直接能启动。操作走 instance.transfer.*，导出导入都在任务页看进度。 */

/* 交接给其他电脑：确认弹窗（隐私提醒 + 完整模式 + 导出位置）→ 后台打包（任务页看进度） */
function transferExportModal(info) {
  if ($("#tr-mask")) return; // 上一个还开着
  const d = document.createElement("div");
  d.className = "modal-mask enter";
  d.id = "tr-mask";
  d.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-labelledby="tr-title">
    <h3 id="tr-title">交接「${esc(info.name)}」</h3>
    <div class="m-body">
      把这台服务器打包成一个 .mdpack 文件，发给接手的人（微信 / 网盘都行）。对方在「新建服务器」页选「导入交接包」，
      存档、配置、模组和白名单全保留，装完直接能启动。
      <div class="warn-card" style="margin-top:12px">
        <div class="wc-title">${icon("warn")} 含玩家数据</div>
        <div class="wc-note">包里有玩家的存档（playerdata）、白名单和 OP 名单。请先征得服务器成员的同意再交接。
          日志（logs）不会打进包，但它可能留有玩家 IP，必要时先在服务器目录里处理好。
          配置文件里写着的密码也会原样交出去，比如 server.properties 里的 RCON 密码、插件配置里的数据库密码和机器人令牌：在别处也用着的，先改掉再交接。</div>
      </div>
      <div class="field" style="margin-top:12px" role="radiogroup" aria-label="这个包给谁"><span>这个包给谁</span>
        <label class="ob-radio"><input type="radio" name="tr-mode" value="handover" checked><span>交给下一任服主，由他接着开</span></label>
        <label class="ob-radio"><input type="radio" name="tr-mode" value="distributable"><span>发给朋友，让他照着开一台</span></label>
        <div class="hint">包里的东西一样，只是对方导入时看到的说法不同。</div>
      </div>
      <label class="switch" style="margin-top:12px"><input type="checkbox" id="tr-full"><span class="sw"></span>
        <span><span class="sw-label">完整模式</span><div class="sw-desc">把服务端核心也一起打包；不勾则对方导入时自动重新下载（包更小）</div></span></label>
      <label class="field" style="margin-top:12px"><span>导出到哪个文件夹</span>
        <div class="row" style="gap:8px">
          <input type="text" id="tr-dest" spellcheck="false" autocomplete="off" style="flex:1" aria-label="导出文件夹" placeholder="默认：数据目录 \\ cache \\ transfer">
          <button type="button" class="btn" id="tr-browse">${icon("folder")} 浏览…</button>
        </div>
        <div class="field-err" id="tr-dest-err"></div>
      </label>
      <div class="hint" style="margin-top:6px">世界存档大的话包会有几个 GB，打包进度在任务页看，中断了随时可以重来。</div>
    </div>
    <div class="m-actions">
      <button class="btn" id="tr-cancel">取消</button>
      <button class="btn primary" id="tr-go">${icon("share")} 开始打包</button>
    </div></div>`;
  $("#modal-root").appendChild(d);
  const close = () => dismiss(d);
  d.addEventListener("click", e => { if (e.target === d) close(); });
  document.addEventListener("keydown", function escClose(e) { if (e.key === "Escape" && $("#tr-mask")) { close(); document.removeEventListener("keydown", escClose); } });
  $("#tr-cancel").onclick = close;
  $("#tr-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (r.path) { $("#tr-dest").value = r.path; $("#tr-dest").classList.remove("invalid"); $("#tr-dest-err").innerHTML = ""; } }
    catch (e) { toast(e.message, true); }
  };
  $("#tr-go").onclick = async () => {
    const dest = $("#tr-dest").value.trim();
    if (dest && !/^[a-zA-Z]:[\\/]/.test(dest)) {
      $("#tr-dest").classList.add("invalid");
      $("#tr-dest-err").innerHTML = icon("warn") + " 请填完整的文件夹路径，或清空用默认位置";
      return;
    }
    $("#tr-go").disabled = true;
    try {
      const r = await api(`/api/instances/${encodeURIComponent(info.name)}/transfer/export`, {
        method: "POST", body: { full: $("#tr-full").checked, mode: (document.querySelector('input[name="tr-mode"]:checked') || {}).value || "handover", dest },
      });
      close();
      toast("开始打包交接包");
      location.hash = `#/task/${r.taskId}`;
    } catch (e) {
      toast(e.message, true);
      $("#tr-go").disabled = false;
      if (/导出目录不可用/.test(e.message)) { $("#tr-dest").classList.add("invalid"); $("#tr-dest-err").innerHTML = icon("warn") + " " + esc(e.message); }
    }
  };
}

/* 导入交接包向导：选 .mdpack → 上传（顺便出摘要）→ 摘要展示 + 填名称 / 内存 / 端口 / EULA → 后台导入 */
async function drawTransfer() {
  const gen = beginCreateView();
  const app = await api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096, config: {} }));
  if (stale(gen)) return;
  const maxMem = Math.max(2048, app.ramMb - 2048);
  let trapRoot = "", pack = null, packFile = ""; // pack：上传后拿到的摘要；packFile：后端缓存里的包路径
  Main().innerHTML = pageHead("导入交接包", "别人用 MineDeck 导出的 .mdpack 交接包：存档、配置、模组全保留，装完直接能启动。", "",
    `<a href="#/create">新建服务器</a>${icon("chevronRight")}<span>导入交接包</span>`) + `
    <div class="form-grid">
      <label class="field full"><span>交接包文件（.mdpack）</span><input type="file" id="tp-file" accept=".mdpack,.zip">
        <div class="hint">交接包动辄几个 GB：上传要等一会儿，进度看按钮上的字。被改过扩展名的包选「所有文件」再选。</div>
        <div class="field-err" id="tp-file-err"></div></label>
    </div>
    <div id="tp-form"></div>
    ${wizFoot("tp-back", "返回", `<button class="btn primary" id="tp-go" disabled>${icon("bolt")} 导入交接包</button>`)}`;
  $("#tp-back").onclick = () => renderCreate();
  const fileErr = msg => { $("#tp-file").classList.toggle("invalid", !!msg); $("#tp-file-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  $("#tp-file").addEventListener("change", () => fileErr(""));

  let upSeq = 0; // 每选一次文件加一：前一次上传回来得晚，不能把后选的那个顶掉
  const upload = async () => {
    const seq = ++upSeq;
    const f = $("#tp-file").files[0];
    // 换了文件，上一个包的摘要就不算数了，哪怕这次选的文件有问题：不清掉的话，「导入」点下去导的是上一个包
    pack = null; packFile = "";
    $("#tp-form").innerHTML = "";
    $("#tp-go").disabled = true;
    if (!f) { fileErr("请先选择交接包文件（.mdpack）"); $("#tp-file").focus(); return; }
    // 上限是 8 GB（后端同一个数）。超了就不传：几个 GB 传完才被拒，等了半天只得到一句报错
    if (f.size > 8 * 1024 ** 3) {
      fileErr(`这个交接包有 ${(f.size / 1024 ** 3).toFixed(2)} GB，超过了 8 GB 的上限，导不进来。存档这么大的服务器，把整个服务器文件夹拷到这台电脑上，再回「新建服务器」页选「接管已有服务器」。`);
      return;
    }
    $("#tp-form").innerHTML = `<div class="sub" style="margin:8px 0 16px">上传中…</div>`;
    const fd = new FormData();
    fd.append("file", f);
    try {
      const res = await fetch("/api/transfer/upload", { method: "POST", headers: { "X-Token": TOKEN() }, body: fd });
      const j = await res.json();
      if (!j.ok) throw new Error(j.error || "上传失败");
      if (stale(gen) || seq !== upSeq) return;
      pack = j.data; packFile = j.data.file;
      drawForm();
    } catch (e) {
      if (stale(gen) || seq !== upSeq) return;
      fileErr(e.message);
      $("#tp-form").innerHTML = "";
    }
  };
  $("#tp-file").addEventListener("change", upload);

  const drawForm = () => {
    const s = pack;
    const mem = Math.min(Math.max(s.xmxMb || 6144, 2048), maxMem);
    $("#tp-form").innerHTML = `
      <div class="group" style="margin:16px 0">
        <div class="wc-title" style="padding:14px 16px 4px">${icon("info")} 这个包里是什么</div>
        <div class="kv" style="padding:6px 16px 14px;margin-top:4px">
          <span>原来叫</span><b>${esc(s.name)}</b>
          <span>这个包是</span><b>${s.mode === "distributable" ? "对方发给朋友的一份（照着开一台）" : "服主交接（整台交给你接着开）"}</b>
          <span>核心</span><b>${esc(coreName(s.core))} ${esc(s.mc)}${s.build ? `（构建 ${esc(s.build)}）` : ""}</b>
          <span>世界大小</span><b>${fmtBytes(s.worldBytes) || "0 B"}</b>
          <span>模组 / 插件</span><b>${s.mods} 个模组${s.plugins ? ` / ${s.plugins} 个插件` : ""}</b>
          <span>内存</span><b>${(s.xmxMb / 1024).toFixed(1)} GB（照原样填好）</b>
          <span>JAVA</span><b>Java ${s.javaMajor || "按版本自动选"}</b>
          <span>导出自</span><b>MineDeck ${esc(s.minedeckVersion || "")}，${esc(s.exportedAt || "")}</b>
        </div>
      </div>
      <div class="warn-card" id="tp-trust" style="margin:0 0 16px">
        <div class="wc-title">${icon("warn")} 导入前请确认来源</div>
        <div class="wc-note">交接包里的模组、插件${s.full ? "和服务端核心" : ""}是对方电脑上带过来的程序，启动服务器时会在你的电脑上运行。只导入你信得过的人给的包。
          原来的 JVM 参数和定时任务不会带过来，导入完成后的日志里会列出来，需要的话自己再加。${s.full ? "这个包带着核心：核心自己的启动参数文件也是包里的那份。" : ""}</div>
      </div>
      <div class="form-grid">
        <label class="field"><span>接手后的名称</span><input type="text" id="tp-name" maxlength="40" value="${esc(s.name)}"><div class="field-err" id="tp-name-err"></div></label>
        <label class="field"><span>端口（默认沿用包里的 ${s.port || 25565}）</span>
          <input type="number" id="tp-port" value="${s.port || 25565}" min="1" max="65535">
          <div class="field-err" id="tp-port-err"></div></label>
        <div class="field full" id="tp-mem-mount"></div>
        <label class="field full"><span>Java</span><select id="tp-java" aria-label="Java">
          <option value="0">${s.javaMajor ? `跟原来一样：Java ${s.javaMajor}` : "自动：按 Minecraft 版本选"}（本机没有就下载）</option>
          ${JAVA_DOWNLOADABLE.filter(m => m !== s.javaMajor).map(m => `<option value="${m}">Java ${m}</option>`).join("")}</select>
          <div class="hint">一般不用改。原来那台用的 Java 通常就是对的；导入后也能在「内存 / 启动」里换。</div></label>
        <label class="field full"><span>存放位置</span>
          <div class="row" style="gap:8px">
            <input type="text" id="tp-root" readonly style="flex:1;cursor:default;background:var(--surface-2)" aria-label="存放位置">
            <button type="button" class="btn" id="tp-browse">${icon("folder")} 浏览…</button>
          </div>
          <div class="hint">存放位置 ${s.full ? "· 完整模式（包里带核心，导入不再下载）" : "· 瘦身包（导入时自动下载核心）"}</div></label>
      </div>
      <div class="eula-box">
        <label class="switch"><input type="checkbox" id="tp-eula"><span class="sw"></span>
          <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft EULA</a></span></label>
        <div class="hint" style="margin-top:4px">原服主在 MineDeck 里同意过 EULA 的，这步可以不勾。</div>
        <div class="field-err" id="tp-eula-err"></div>
      </div>`;
    renderMemoryControl($("#tp-mem-mount"), { sliderId: "tp-mem", min: 2048, max: maxMem, value: mem, totalMb: app.ramMb, availMb: app.availRamMb,
      hint: `${(s.xmxMb / 1024).toFixed(1)} GB 是原来的分配，可以照用或调整` });
    const trUpdateRoot = () => { $("#tp-root").value = trapRoot || app.serversRoot || "程序目录\\servers"; };
    trUpdateRoot();
    $("#tp-browse").onclick = async () => {
      try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { trapRoot = r.path; trUpdateRoot(); } }
      catch (e) { toast(e.message, true); }
    };
    $("#tp-eula").addEventListener("change", () => { if ($("#tp-eula").checked) { $(".eula-box").classList.remove("invalid"); $("#tp-eula-err").innerHTML = ""; } });
    // 端口换了就查一下占用（后端 port.check）；被占了提示但不拦着——可能就是想沿用包里的端口。
    // 挂 input 事件（打字时每敲一下都会查，后面的结果追上前面的），不必等失焦
    const portErr = msg => { $("#tp-port").classList.toggle("invalid", !!msg); $("#tp-port-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
    let portCheck = 0;
    $("#tp-port").addEventListener("input", async () => {
      const v = +$("#tp-port").value;
      if (!(v >= 1 && v <= 65535)) { portErr("端口要在 1~65535 之间"); return; }
      if (v === (pack.port || 25565)) { portErr(""); return; }
      const my = ++portCheck;
      try {
        const p = await api(`/api/ports/${v}`);
        if (stale(gen) || my !== portCheck) return;
        if (p.tcp && p.tcp.free === false) {
          const who = (p.tcp.owners || []).map(o => o.instance || o.process).filter(Boolean)[0] || "";
          portErr(who ? `端口 ${v} 正被「${who}」占用，启动会失败` : `端口 ${v} 正被占用`);
          return;
        }
        if ((p.instances || []).some(x => x.instance && x.instance !== $("#tp-name").value.trim())) {
          portErr(`端口 ${v} 也是列表里某台服务器要用的`);
          return;
        }
        portErr("");
      } catch (e) { /* 查不成不拦着 */ }
    });
    $("#tp-go").disabled = false;
  };

  $("#tp-go").onclick = async () => {
    if (!packFile) { fileErr("交接包还没上传完，或上传失败了"); return; }
    const name = $("#tp-name") ? $("#tp-name").value.trim() : "";
    if (!name) { $("#tp-name").classList.add("invalid"); $("#tp-name-err").innerHTML = icon("warn") + " 请给这台服务器起个名字"; $("#tp-name").focus(); return; }
    $("#tp-go").disabled = true;
    $("#tp-go").textContent = "导入中…";
    try {
      const r = await api("/api/transfer/import", { method: "POST", body: {
        file: packFile, name, xmxMb: $("#tp-mem") ? +$("#tp-mem").value : 0,
        port: +$("#tp-port").value || 0, eula: $("#tp-eula").checked, root: trapRoot,
        javaMajor: $("#tp-java") ? +$("#tp-java").value || 0 : 0,
      } });
      if (stale(gen)) return;
      toast(`开始导入：${r.pack.core} ${r.pack.mc}（${r.pack.mods} 个模组）`);
      location.hash = `#/task/${r.taskId}`;
    } catch (e) {
      toast(e.message, true);
      if (stale(gen)) return;
      if (/EULA/.test(e.message)) { $(".eula-box").classList.add("invalid"); $("#tp-eula-err").innerHTML = icon("warn") + " " + esc(e.message); $("#tp-eula").focus(); }
      $("#tp-go").disabled = false;
      $("#tp-go").innerHTML = icon("bolt") + " 导入交接包";
    }
  };
}

/* 向导的步骤序列：非 vanilla 在「选择构建」与「基本设置」之间多一步「选模组」（可选，草稿购物车在外围模块 modcart.js）；
   原版没有「选模组」，也没有构建，第 3 步照旧按「跳过」画。wiz.step 始终是这份列表的下标，
   原版的编号与原来完全一致（0 核心 / 1 版本 / 2 跳过的构建 / 3 设置），非 vanilla 各步顺延一位。 */
const wizSteps = () => wiz.core === "vanilla"
  ? ["选择核心", "选择版本", "选择构建", "基本设置"]
  : ["选择核心", "选择版本", "选择构建", "选模组", "基本设置"];
function wizardShell(inner) {
  const names = wizSteps();
  // 步骤条：方块序号 + 像素虚线；已完成打勾且可点回，当前步实底，未到达描边；眉标写明第几步
  Main().innerHTML = pageHead("新建服务器", "选好核心与版本后，Java 与服务端会自动下载并完成配置。", "", "",
    `第 ${wiz.step + 1} 步<span class="sep">/</span>共 ${names.length} 步`) + `
    <div class="steps">${names.map((n, i) => {
      const skipped = n === "选择构建" && wiz.core === "vanilla"; // 原版没有「选择构建」，该步不可点回
      const cls = i === wiz.step ? "cur" : i < wiz.step && !skipped ? "done" : "";
      const tip = cls === "done" ? `title="回到「${n}」"` : skipped ? `title="原版无需选择构建" tabindex="-1"` : `tabindex="-1"`;
      return (i ? `<span class="step-line${i <= wiz.step ? " done" : ""}"></span>` : "") +
        `<button type="button" class="step-pill ${cls}" data-step="${i}" ${tip}><span class="sn">${cls === "done" ? icon("check") : i + 1}</span>${n}</button>`;
    }).join("")}</div>
    <div id="wiz-body" class="sec-enter">${inner}</div>`;
  document.querySelectorAll(".step-pill.done").forEach(p => p.onclick = () => { wiz.step = +p.dataset.step; drawWizard(); });
}

/* 向导吸底栏：左「返回」、中间回显已选内容、右主按钮；表单步与内容列同宽（w-read） */
const wizFoot = (backId, backText, right, wide = false) =>
  `<div class="wizard-foot${wide ? "" : " w-read"}"><button class="btn" id="${backId}">${icon("arrowLeft")} ${backText}</button>
    <span class="wf-sum" id="wf-sum"></span>${right}</div>`;
function paintWizSum() {
  const el = $("#wf-sum");
  if (!el) return;
  const parts = [];
  if (wiz.core) parts.push(`<b>${esc(coreName(wiz.core))}</b>`);
  if (wiz.step >= 1 && wiz.mc) parts.push(esc(wiz.mc));
  if (wiz.step >= 2 && wiz.build) parts.push("构建 " + esc(wiz.build));
  el.innerHTML = parts.length ? "已选：" + parts.join(" · ") : "";
}

async function drawWizard() {
  const gen = beginCreateView();
  if (wiz.step === 0) {
    wizardShell(`<div id="core-grid">${skeletonRows(6)}</div>
      ${wizFoot("wback0", "返回", `<button class="btn primary" id="wnext" disabled>下一步 ${icon("arrowRight")}</button>`, true)}`);
    $("#wback0").onclick = () => renderCreate();
    paintWizSum();
    let cores;
    try { cores = await api("/api/cores"); } catch (e) { if (!stale(gen)) $("#core-grid").innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    cores.forEach(c => CORES[c.id] = c);
    const CORE_GROUPS = [
      { title: "原版与轻量", ids: ["vanilla"] },
      { title: "插件服（Bukkit / Spigot 系）", ids: ["paper", "purpur", "leaves", "folia"] },
      { title: "模组服（加载器）", ids: ["fabric", "forge", "neoforge"] },
      { title: "混合服（模组 + 插件）", ids: ["mohist", "banner"] },
      { title: "群组代理", ids: ["velocity", "waterfall"] },
    ];
    const REC = "paper";
    // 一组单选：读屏读得出「单选按钮，已选中」，键盘 Tab 进来、方向键换核心（见下面的 onkeydown）
    const coreCard = c => `
      <div class="core-card ${wiz.core === c.id ? "sel" : ""}" data-id="${c.id}" role="radio" aria-checked="${wiz.core === c.id}" tabindex="-1"
        aria-label="${esc(c.name)}${c.id === REC ? "（新手推荐）" : ""}" aria-describedby="cd-${c.id}">
        ${avatarOf(c.id)}
        <div><div class="cn">${esc(c.name)} <span class="badge">${esc(c.tag)}</span>${c.id === REC ? `<span class="rec-badge">新手推荐</span>` : ""}</div>
        <div class="cd" id="cd-${c.id}">${esc(c.desc)}</div></div>
        <span class="check" aria-hidden="true">${icon("check")}</span>
      </div>`;
    const byId = Object.fromEntries(cores.map(c => [c.id, c]));
    const used = new Set();
    let coreHtml = CORE_GROUPS.map(g => {
      const items = g.ids.map(id => byId[id]).filter(Boolean);
      items.forEach(c => used.add(c.id));
      return items.length ? `<div class="core-group"><div class="cg-title">${g.title}</div><div class="core-grid">${items.map(coreCard).join("")}</div></div>` : "";
    }).join("");
    const rest = cores.filter(c => !used.has(c.id));
    if (rest.length) coreHtml += `<div class="core-group"><div class="cg-title">其它</div><div class="core-grid">${rest.map(coreCard).join("")}</div></div>`;
    $("#core-grid").innerHTML = coreHtml;
    $("#core-grid").setAttribute("role", "radiogroup");
    $("#core-grid").setAttribute("aria-label", "服务端核心");
    // 只有一张卡在 Tab 顺序里（选中的那张，没选就第一张）；方向键在全部卡片间移动并选中，空格选中，回车 = 选中并下一步（同双击）
    const cards = [...$("#core-grid").querySelectorAll(".core-card")];
    const pick = c => {
      wiz.core = c.dataset.id;
      cards.forEach(x => { const on = x === c; x.classList.toggle("sel", on); x.setAttribute("aria-checked", String(on)); x.tabIndex = on ? 0 : -1; });
      $("#wnext").disabled = false;
      paintWizSum();
    };
    if (cards.length) (cards.find(x => x.dataset.id === wiz.core) || cards[0]).tabIndex = 0;
    cards.forEach((c, i) => {
      c.onclick = () => pick(c);
      c.ondblclick = () => { wiz.core = c.dataset.id; wiz.step = 1; drawWizard(); }; // 双击 = 选中并下一步
      c.onkeydown = e => {
        if (e.key === " " || e.key === "Enter") {
          e.preventDefault();
          if (e.key === "Enter") { wiz.core = c.dataset.id; wiz.step = 1; drawWizard(); } else pick(c);
          return;
        }
        const d = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
        if (!d) return;
        e.preventDefault();
        const n = cards[(i + d + cards.length) % cards.length];
        pick(n); n.focus();
      };
    });
    $("#wnext").onclick = () => { wiz.step = 1; drawWizard(); };
    if (wiz.core) $("#wnext").disabled = false;

  } else if (wiz.step === 1) {
    wizardShell(`
      <div class="row" style="margin-bottom:12px">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="ver-search" placeholder="搜索版本号，如 1.20.1 / 26.2" style="width:280px"></div>
        ${wiz.core === "vanilla" ? `<label class="switch"><input type="checkbox" id="snap-toggle" ${wiz.snapshots ? "checked" : ""}><span class="sw"></span><span class="sw-label">显示快照版</span></label>` : ""}
      </div>
      <div class="ver-list" id="ver-list">${skeletonRows(8)}</div>
      ${wizFoot("wback", "上一步", `<button class="btn primary" id="wnext" disabled>下一步 ${icon("arrowRight")}</button>`)}`);
    $("#wback").onclick = () => { wiz.step = 0; drawWizard(); };
    $("#wnext").onclick = () => { wiz.step = 2; wiz.build = null; drawWizard(); };
    const snapEl = $("#snap-toggle");
    if (snapEl) snapEl.onchange = () => { wiz.snapshots = snapEl.checked; loadVersions(); };
    let all = [];
    const drawList = () => {
      const q = $("#ver-search").value.trim();
      const items = all.filter(v => !q || v.id.includes(q)).slice(0, 300);
      $("#ver-list").innerHTML = items.length ? items.map(v => {
        // 发布日期副行：仅版本表取自 Mojang 清单的核心带 releaseTime（ISO 8601，取日期部分），其余核心不显示
        const date = /^\d{4}-\d{2}-\d{2}/.test(v.releaseTime || "") ? v.releaseTime.slice(0, 10) : "";
        return `
        <div class="ver-item ${wiz.mc === v.id ? "sel" : ""}" data-id="${esc(v.id)}">
          <span>${esc(v.id)}${v.latest ? `<span class="ver-tag">最新</span>` : ""}${date ? `<span class="ver-date">${esc(date)}</span>` : ""}</span><span class="vt">${v.type === "snapshot" ? "快照" : "正式版"}</span>
        </div>`;
      }).join("") : `<div class="ver-item">没有匹配的版本</div>`;
      $("#ver-list").querySelectorAll(".ver-item[data-id]").forEach(el => el.onclick = () => {
        wiz.mc = el.dataset.id;
        $("#ver-list").querySelectorAll(".ver-item").forEach(x => x.classList.toggle("sel", x.dataset.id === wiz.mc));
        $("#wnext").disabled = false;
      });
    };
    let versionReq = 0;
    const loadVersions = async () => {
      const req = ++versionReq;
      const outdated = () => stale(gen) || req !== versionReq;
      $("#ver-list").innerHTML = skeletonRows(8);
      $("#wnext").disabled = true;
      try {
        const versions = await api(`/api/mcversions?core=${wiz.core}&snapshots=${wiz.snapshots ? 1 : 0}`);
        if (outdated()) return;
        all = versions;
        // 合理默认：没选过（或换核心后原选择不在列表里）就预选最新正式版，直接「下一步」即可
        if (!all.some(v => v.id === wiz.mc)) wiz.mc = (all.find(v => v.latest && v.type !== "snapshot") || all.find(v => v.type !== "snapshot") || {}).id || null;
        $("#wnext").disabled = !wiz.mc;
        paintWizSum();
        drawList();
        $("#ver-list .ver-item.sel")?.scrollIntoView({ block: "nearest" });
      } catch (e) { if (!outdated()) $("#ver-list").innerHTML = `<div class="err-box">${esc(e.message)}<br>可在「设置 → 下载与网络」切换下载源后重试。</div>`; }
    };
    $("#ver-search").oninput = drawList;
    // 委托在列表容器上（drawList 每次重建条目）：单击后刷新底栏回显，双击 = 选中并下一步
    $("#ver-list").addEventListener("click", paintWizSum);
    $("#ver-list").addEventListener("dblclick", e => {
      const it = e.target.closest(".ver-item[data-id]");
      if (it) { wiz.mc = it.dataset.id; wiz.step = 2; wiz.build = null; drawWizard(); }
    });
    await loadVersions();

  } else if (wiz.step === 2) {
    if (wiz.core === "vanilla") { wiz.step = 3; wiz.build = ""; drawWizard(); return; }
    wizardShell(`<div class="sub">为 ${coreKind(wiz.core) === "proxy" ? "" : "MC "}${esc(wiz.mc)} 选择 ${coreName(wiz.core)} 构建（默认推荐最新稳定）</div>
      <div class="ver-list" id="build-list">${skeletonRows(6)}</div>
      ${wizFoot("wback", "上一步", `<button class="btn primary" id="wnext" disabled>下一步 ${icon("arrowRight")}</button>`)}`);
    $("#wback").onclick = () => { wiz.step = 1; drawWizard(); };
    $("#wnext").onclick = () => { wiz.step = 3; drawWizard(); };
    paintWizSum();
    try {
      const builds = await api(`/api/builds?core=${wiz.core}&mc=${encodeURIComponent(wiz.mc)}`);
      if (stale(gen)) return;
      if (!builds.length) throw new Error(`${wiz.mc} 暂无 ${coreName(wiz.core)} 构建，请换个版本或核心`);
      if (!wiz.build) wiz.build = (builds.find(b => b.recommended) || builds[0]).id;
      paintWizSum();
      $("#build-list").innerHTML = builds.slice(0, 200).map(b => `
        <div class="ver-item ${wiz.build === b.id ? "sel" : ""}" data-id="${esc(b.id)}">
          <span>${esc(b.id)}${b.recommended ? `<span class="ver-tag">推荐</span>` : ""}</span><span class="vt">${esc(b.label || "")}</span>
        </div>`).join("");
      $("#build-list").querySelectorAll(".ver-item").forEach(el => {
        el.onclick = () => {
          wiz.build = el.dataset.id;
          $("#build-list").querySelectorAll(".ver-item").forEach(x => x.classList.toggle("sel", x.dataset.id === wiz.build));
          paintWizSum();
        };
        el.ondblclick = () => { wiz.build = el.dataset.id; wiz.step = 3; drawWizard(); };
      });
      $("#wnext").disabled = false;
    } catch (e) { if (!stale(gen)) $("#build-list").innerHTML = `<div class="err-box">${esc(e.message)}</div>`; }

  } else if (wiz.step === 3 && wiz.core !== "vanilla") {
    /* 选模组（可选步，列表第 4 项）：搜索 + 草稿购物车整块在外围模块 modcart.js。
       这一步永远可以「下一步」留空跳过，创建完成后想再加可以去详情页的「购物车」标签。 */
    wizardShell(`
      <div id="wiz-mod-body"></div>
      ${wizFoot("wback", "上一步", `<button class="btn primary" id="wnext">下一步 ${icon("arrowRight")}</button>`)}`);
    $("#wback").onclick = () => { wiz.step = 2; drawWizard(); };
    $("#wnext").onclick = () => { wiz.step = 4; drawWizard(); };
    paintWizSum();
    if (window.MDModCart) MDModCart.wizardPanel($("#wiz-mod-body"), { core: wiz.core, mc: wiz.mc }, gen);
    else $("#wiz-mod-body").innerHTML = `<div class="err-box">模组购物车模块没有加载出来。可以先跳过这一步，之后在服务器的「购物车」标签里添加。</div>`;

  } else {
    const isProxy = coreKind(wiz.core) === "proxy";
    const app = await api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096 }));
    if (stale(gen)) return;
    const maxMem = Math.max(2048, app.ramMb - 2048);
    const defMem = isProxy ? 1024 : Math.min(4096, maxMem);
    wizardShell(`
      <div class="form-grid">
        <label class="field"><span>实例名称</span><input type="text" id="f-name" value="我的${coreName(wiz.core)}服务器" maxlength="40"><div class="field-err" id="f-name-err"></div></label>
        ${isProxy ? "" : `<label class="field"><span>端口（默认 25565）</span><input type="number" id="f-port" value="25565" min="1" max="65535"><div class="field-err" id="f-port-err"></div></label>`}
        <div class="field full" id="mem-mount"></div>
        <label class="field full"><span>存放位置</span>
          <div class="row" style="gap:8px">
            <input type="text" id="f-root" readonly style="flex:1;cursor:default;background:var(--surface-2)" aria-label="存放位置">
            <button type="button" class="btn" id="f-browse">${icon("folder")} 浏览…</button>
          </div>
          <div class="hint" id="f-path"></div>
        </label>
        ${isProxy ? "" : `
        <label class="field full"><span>服务器介绍 MOTD（支持中文与 § 色码）</span><input type="text" id="f-motd" value="MineDeck 开服 · 一起来玩"></label>
        <div class="field"><label class="switch"><input type="checkbox" id="f-online"><span class="sw"></span>
          <span><span class="sw-label">正版验证（online-mode）</span><div class="sw-desc">默认关闭：离线账户可直接进服</div></span></label></div>
        <div class="field"><label class="switch"><input type="checkbox" id="f-flight" checked><span class="sw"></span>
          <span><span class="sw-label">允许飞行（allow-flight）</span><div class="sw-desc">默认开启：防止模组移动被误踢</div></span></label></div>
        <label class="field"><span>游戏难度</span><select id="f-diff"><option value="peaceful">和平</option><option value="easy" selected>简单</option><option value="normal">普通</option><option value="hard">困难</option></select></label>
        <label class="field"><span>默认游戏模式</span><select id="f-mode"><option value="survival" selected>生存</option><option value="creative">创造</option><option value="adventure">冒险</option><option value="spectator">旁观</option></select></label>
        <div class="hint" style="grid-column:1/-1">更多几十项设置（PVP、视距、白名单、命令方块…）可在创建后的「常用设置」中按分组修改。</div>`}
      </div>
      ${isProxy
        ? `<div class="eula-box">代理端不跑 Minecraft 游戏本身，不用同意 EULA。端口这些，等第一次启动之后，在这台服务器目录里的配置文件改。</div>`
        : `<div class="eula-box">
        <label class="switch"><input type="checkbox" id="f-eula"><span class="sw"></span>
          <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft 最终用户许可协议 (EULA)</a></span></label>
        <div class="field-err" id="f-eula-err"></div>
      </div>`}
      ${wizFoot("wback", "上一步", `<button class="btn primary" id="wcreate">${icon("bolt")} 开始部署</button>`)}`);
    $("#wback").onclick = () => { wiz.step = wiz.core === "vanilla" ? 1 : 3; drawWizard(); }; // 原版回版本页，非 vanilla 回「选模组」
    paintWizSum();
    // 行内校验：离开字段时检查，出错后随输入即时复核；提交时全部检查并聚焦第一个出错项（不再用远离字段的 toast）
    const showErr = (input, errId, msg) => { input.classList.toggle("invalid", !!msg); $(errId).innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; return !msg; };
    const checkName = () => showErr($("#f-name"), "#f-name-err", $("#f-name").value.trim() ? "" : "请给服务器起个名字");
    const checkPort = () => {
      if (isProxy) return true;
      const v = Number($("#f-port").value);
      return showErr($("#f-port"), "#f-port-err", Number.isInteger(v) && v >= 1 && v <= 65535 ? "" : "端口需为 1 ~ 65535 的整数，不确定就用 25565");
    };
    const checkEula = () => {
      if (isProxy) return true;
      const ok = $("#f-eula").checked;
      $(".eula-box").classList.toggle("invalid", !ok);
      $("#f-eula-err").innerHTML = ok ? "" : icon("warn") + " 开服前需要同意 Minecraft EULA";
      return ok;
    };
    $("#f-name").addEventListener("blur", checkName);
    $("#f-name").addEventListener("input", () => { if ($("#f-name").classList.contains("invalid")) checkName(); });
    if (!isProxy) {
      $("#f-port").addEventListener("blur", checkPort);
      $("#f-port").addEventListener("input", () => { if ($("#f-port").classList.contains("invalid")) checkPort(); });
      $("#f-eula").addEventListener("change", () => { if ($(".eula-box").classList.contains("invalid")) checkEula(); });
    }
    renderMemoryControl($("#mem-mount"), { sliderId: "f-mem", min: 512, max: maxMem, value: defMem, totalMb: app.ramMb, availMb: app.availRamMb, hint: isProxy ? "代理端很省内存，1GB 通常足够" : "模组服建议 4GB 以上。没有模组的小队，2~4GB 足够。" });
    // 存放位置：目标路径跟随实例名，可浏览改到其它盘（中文名会自动生成英文目录）
    const clientSlug = s => { let o = ""; for (const c of (s || "")) { if (/[0-9A-Za-z_-]/.test(c)) o += c; else if (c === " " || c === ".") o += "-"; } o = o.replace(/^[-_]+|[-_]+$/g, ""); return o || "server-（自动）"; };
    const curRoot = () => wiz.root || app.serversRoot || "程序目录\\servers";
    const updateRoot = () => {
      $("#f-root").value = curRoot();
      $("#f-path").innerHTML = `将创建于：<b>${esc(curRoot())}\\${esc(clientSlug($("#f-name").value.trim()))}</b>${wiz.root ? "" : "（可点「浏览」改到其它盘）"}`;
    };
    $("#f-name").oninput = updateRoot;
    $("#f-browse").onclick = async () => {
      try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { wiz.root = r.path; updateRoot(); toast("已选择：" + r.path); } }
      catch (e) { toast(e.message, true); }
    };
    updateRoot();
    $("#wcreate").onclick = async () => {
      const okName = checkName(), okPort = checkPort(), okEula = checkEula();
      if (!(okName && okPort && okEula)) {
        const first = !okName ? $("#f-name") : !okPort ? $("#f-port") : $("#f-eula");
        first.closest(".field, .eula-box").scrollIntoView({ behavior: "smooth", block: "center" });
        first.focus({ preventScroll: true });
        return;
      }
      const name = $("#f-name").value.trim();
      const createBtn = $("#wcreate");
      createBtn.disabled = true;
      try {
        const r = await api("/api/instances", {
          method: "POST",
          body: {
            name,
            core: wiz.core, mc: wiz.mc, build: wiz.build || "",
            root: wiz.root || "",
            xmxMb: +$("#f-mem").value,
            port: isProxy ? 25565 : +$("#f-port").value,
            eula: !isProxy,
            onlineMode: isProxy ? false : $("#f-online").checked,
            allowFlight: isProxy ? false : $("#f-flight").checked,
            motd: isProxy ? "" : $("#f-motd").value.trim(),
            difficulty: isProxy ? "" : $("#f-diff").value,
            gamemode: isProxy ? "" : $("#f-mode").value,
          },
        });
        if (stale(gen)) { toast("已开始部署，可在新建服务器页查看进度"); return; }
        /* 「选模组」步挑好的内容放在草稿购物车里（不带 inst）。空车照旧直接跳部署任务页；
           有货时留在向导这一页等建服任务跑完，再把草稿车一起装上（装模组是一条 kind "mods" 的任务，跳到那页看进度）。
           建服失败仍然跳建服任务页，失败的日志要看得见；跳转本身就是切页，startPoll 停在旧代际上自然失效。 */
        let draft = null;
        if (wiz.core !== "vanilla") { // 原版没有「选模组」这一步，草稿车里剩下的（上次没装的）不能装进原版服
          try { draft = await api("/api/modcart"); } catch { draft = null; } // 老后端没这个接口：当作空车
          if (stale(gen)) return;
        }
        if (!draft || !(draft.count > 0)) { location.hash = `#/task/${r.taskId}`; return; }
        /* 等建服的这段时间不能回上一步：回去会重画向导（换代号），这里就不再接着装，服务器却已经在建了 */
        const back = $("#wback");
        if (back) back.disabled = true;
        document.querySelectorAll(".step-pill").forEach(p => { p.disabled = true; p.onclick = null; });
        const say = s => { if (createBtn.isConnected) createBtn.innerHTML = icon("spinner") + " " + esc(s); };
        let launched = false;
        const follow = async () => {
          if (launched || stale(gen)) return;
          let t;
          try { t = await api(`/api/tasks/${encodeURIComponent(r.taskId)}`); } catch { return; } // 查询偶发失败：等下一轮轮询再说
          if (stale(gen) || launched) return;
          if (!t || !t.ended) { if (t && t.label) say(t.label); return; }
          launched = true;
          if (t.error) { location.hash = `#/task/${r.taskId}`; return; } // 建服失败：保持失败可见性
          try {
            /* expect：「选模组」那一步上画出来的那些。草稿车在那之后被改过（AI 助手加了东西），或者同名的旧购物车里还剩着东西，
               后端都会拒绝，不会把人没看过的东西装进新服务器。 */
            const seen = (window.MDModCart && MDModCart.draftSeen()) || [];
            const r2 = await api(`/api/instances/${encodeURIComponent(name)}/modcart/install`, { method: "POST", body: { includeDraft: true, expect: seen } });
            if (stale(gen)) return;
            toast("服务器创建完成，正在安装购物车里的模组…");
            location.hash = `#/task/${encodeURIComponent(r2.taskId)}`;
          } catch (e) { toast(e.message, true); if (!stale(gen)) location.hash = `#/task/${r.taskId}`; } // 装模组没发出去：至少让建服结果可见
        };
        say("正在等待服务器创建完成…");
        await follow();
        if (!launched && !stale(gen)) startPoll(follow, 1500, gen); // 第一次查就结束了的不用再轮询
      } catch (e) { toast(e.message, true); if (!stale(gen) && createBtn.isConnected) createBtn.disabled = false; }
    };
  }
}

/* ---------- 视图：创建任务进度 ---------- */
/* 后台任务不止「部署」一种（后端 tasks.Snapshot.kind）：接管已有服务器、换 Java 也走这一页，说法跟着换 */
const TASK_KINDS = {
  "": { title: "正在部署服务器", sub: "正在下载 Java 和服务端。这一步不用再改配置。", done: "部署完成", doneSub: "Java、服务端与配置均已就绪",
    ok: "服务器创建成功", fail: "创建失败", failHint: "可返回重试（已下载的文件有缓存，重试很快）。", busy: "部署中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回重试" },
  adopt: { title: "正在接管服务器", sub: "正在检查文件夹、准备 Java。存档和配置不会动。", done: "接管完成", doneSub: "已经收进 MineDeck，可以在这里启动和管理了",
    ok: "服务器接管成功", fail: "接管失败", failHint: "服务器文件夹里的东西没有被删掉或改坏，可以返回重试。", busy: "接管中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回重试" },
  java: { title: "正在更换 Java", sub: "本机没有这个版本的 Java，正在下载一份（不改系统里的 Java）。", done: "Java 已换好", doneSub: "下次启动这台服务器时生效",
    ok: "Java 已换好，下次启动生效", fail: "更换失败", failHint: "这台服务器还在用原来的 Java，可以返回重试。", busy: "下载中，请勿关闭程序…",
    go: "完成，回到服务器", href: name => `#/inst/${encodeURIComponent(name)}/jvm`, back: "#/instances", backText: "返回服务器列表" },
  "transfer-out": { title: "正在导出交接包", sub: "正在把世界、配置和模组打包成一个文件。服务器不会被改动。", done: "导出完成", doneSub: "交接包已生成，文件位置在这一页的日志里",
    ok: "交接包导出成功", fail: "导出失败", failHint: "没有留下不完整的包，可以重试（包的特征见任务日志）。", busy: "打包中，请勿关闭程序…",
    go: "完成", href: () => "#/instances", back: "#/instances", backText: "返回服务器列表" },
  "transfer-in": { title: "正在导入交接包", sub: "正在准备 Java、下载核心并解包。存档、配置和模组都会保留。", done: "交接完成", doneSub: "存档、配置和模组都带过来了，可以启动了。没带过来的东西列在下面的日志里",
    ok: "交接包导入成功", fail: "导入失败", failHint: "半成品的目录已经清理干净，可以重试。", busy: "导入中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回重试" },
  mods: { title: "正在安装模组", sub: "正在下载购物车里的模组和依赖，放进对应的文件夹。", done: "模组安装完成", doneSub: "新装的内容要重启服务器后才会生效",
    ok: "模组安装完成", fail: "安装失败", failHint: "可以回到这台服务器的「购物车」标签里重试；已下载的文件有缓存，重试很快。", busy: "安装中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/instances", backText: "返回服务器列表" },
};
async function renderTaskPage(taskId) {
  const gen = viewGen;
  Main().innerHTML = pageHead("正在部署服务器", "正在下载 Java 和服务端。这一步不用再改配置。", "", "", "", "task-eyebrow") + `
    <div id="t-fetch-error" class="err-box" role="status" hidden></div>
    <div id="task-box"><div class="sub">加载中…</div></div>`;
  // 骨架只建一次，之后每轮轮询只改变化的部分：新完成的步骤对勾弹出，经验条只点亮新增的格子（逐格错峰），不整块重绘重播
  const CELLS = 30;
  // 进度和日志多久没变就提示「下载慢或者在重试」（测试报告 B-01：安装器连不上时进度条长时间不动）；测试里用 __MD_STALL_MS 调短
  const STALL_MS = window.__MD_STALL_MS || 30000;
  let ended = false; // 已结束就不再轮询 / 重复提示（首轮即结束时旧逻辑仍会挂上轮询，成功提示弹两次）
  let built = false, loading = false, label = null, lit = 0, stepState = [], lastSig = "", lastChange = Date.now();
  const stopPoll = () => { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } };
  const draw = async () => {
    if (stale(gen) || ended || loading) return;
    loading = true;
    let t;
    try { t = await api(`/api/tasks/${encodeURIComponent(taskId)}`); }
    catch (e) {
      if (stale(gen) || ended) return;
      if (e.status === 404) { ended = true; stopPoll(); }
      const err = $("#t-fetch-error");
      err.hidden = false;
      err.textContent = ended ? e.message : "进度刷新失败，正在自动重试：" + e.message;
      if (ended && !built) $("#task-box").innerHTML = `<a class="btn" href="#/create">返回新建服务器</a>`;
      return;
    } finally { loading = false; }
    if (stale(gen) || ended) return;
    $("#t-fetch-error").hidden = true;
    const K = Object.prototype.hasOwnProperty.call(TASK_KINDS, t.kind || "") ? TASK_KINDS[t.kind || ""] : TASK_KINDS[""]; // 不认识的 kind 按部署任务显示
    if (!built) {
      if (K !== TASK_KINDS[""]) { // 页头先按「部署」画的，知道是哪一类任务后换成它的说法
        const h1 = Main().querySelector(".page-head h1"), sub = Main().querySelector(".page-head .sub");
        if (h1) h1.textContent = K.title;
        if (sub) sub.textContent = K.sub;
      }
      $("#task-box").innerHTML = `
        <div class="task-steps" id="t-steps"></div>
        <div class="progress-wrap" id="t-prog" hidden>
          <div class="progress-head"><b id="t-label"></b><span class="grow"></span><span class="xp-level num" id="t-pct"></span></div>
          ${xpBar(CELLS, 0, "lg")}
          <div class="progress-text" id="t-sub"></div>
        </div>
        <div class="warn-card task-slow" id="t-slow" role="status" hidden><div class="wc-title">${icon("clock")} <span id="t-slow-title"></span></div>
          <div class="wc-note">下载慢，或者在重试：可以检查网络或代理。在重试的话，网络恢复后会自己接着下，不用重新部署。</div></div>
        <div id="t-msgs"></div>
        <div id="t-pending-out"></div>
        <div id="t-pending"></div>
        <div class="task-log" id="task-log"></div>
        <div class="save-bar" id="t-foot"></div>`;
      built = true;
    }
    // 步骤：图标随状态换，新完成的对勾弹出（首轮直接画）
    const steps = t.steps || [];
    const stepsEl = $("#t-steps");
    if (stepsEl.children.length !== steps.length) {
      stepsEl.innerHTML = steps.map(() => `<div class="task-step"><span class="ts-ico"></span><span></span></div>`).join("");
      stepState = [];
    }
    steps.forEach((st, k) => {
      const row = stepsEl.children[k];
      row.lastElementChild.textContent = st.name;
      if (stepState[k] === st.status) return;
      const changed = stepState[k] !== undefined;
      stepState[k] = st.status;
      row.className = "task-step " + st.status;
      row.firstElementChild.innerHTML = st.status === "done" ? `<span class="${changed ? "pop" : ""}">${icon("check")}</span>`
        : st.status === "running" ? icon("spinner") : st.status === "error" ? icon("x") : `<i class="ts-px"></i>`;
    });
    const doneN = steps.filter(s => s.status === "done").length;
    $("#task-eyebrow").innerHTML = steps.length
      ? (t.ended && !t.error ? `已完成${SEP}共 ${steps.length} 步` : `第 ${Math.min(steps.length, doneN + 1)} 步<span class="sep">/</span>共 ${steps.length} 步`) : "";
    // 经验条式进度：30 格，一格 = 当前下载项的 1/30；换了下载项就清空重来；部署完成时填满并整条闪一下
    const success = t.ended && !t.error;
    const prog = $("#t-prog"), cells = prog.querySelectorAll(".xp i");
    const cur = success ? K.done : t.label;
    prog.hidden = !cur;
    if (cur) {
      if (cur !== label) { label = cur; lit = 0; cells.forEach(c => { c.className = ""; c.style.animationDelay = ""; }); }
      const pct = success ? 100 : t.total > 0 ? Math.min(100, t.done / t.total * 100) : 0;
      const n = Math.floor(pct / 100 * CELLS + 1e-6);
      for (let k = lit; k < n; k++) {
        cells[k].className = reducedMotion() ? "on" : "on lit";
        cells[k].style.animationDelay = Math.min(k - lit, 12) * 24 + "ms";
      }
      lit = Math.max(lit, n);
      $("#t-label").textContent = cur;
      $("#t-pct").textContent = Math.round(pct) + "%";
      $("#t-sub").textContent = success ? K.doneSub : `${fmtBytes(t.done) || "0 B"}${t.total > 0 ? " / " + fmtBytes(t.total) : ""}`;
      if (success) playClass(prog.querySelector(".xp"), "flash");
    }
    $("#t-msgs").innerHTML = (t.error ? `<div class="err-box"><b>${K.fail}：</b>${esc(t.error)}<br><br>${K.failHint}</div>` : "") +
      (t.warnings || []).map(w => `<div class="warn-card">
        <div class="wc-title">${icon("warn")} ${esc(w.title)}</div>
        ${w.note ? `<div class="wc-note">${esc(w.note)}</div>` : ""}
        ${(w.items || []).length ? `<ul class="wc-list">${w.items.map(it =>
          `<li>${/^https?:\/\//i.test(it.url || "") ? `<a href="${esc(it.url)}" target="_blank" rel="noopener">${esc(it.name)}</a>` : esc(it.name)}</li>`).join("")}</ul>` : ""}
        ${w.kind === "java-req" && t.result ? `<div class="wc-note"><a class="btn sm" href="#/inst/${encodeURIComponent(t.result)}/jvm">去「内存 / 启动」换 Java</a></div>` : ""}
      </div>`).join("");
    drawPending(t.pending || []);
    const lg = $("#task-log"), text = (t.log || []).join("\n");
    if (lg.textContent !== text) { lg.textContent = text; lg.scrollTop = lg.scrollHeight; }
    // 下载慢 / 在重试：后端报了刚发生的超时、重试（t.net，一分钟内的），或者进度、步骤、日志 STALL_MS 没有任何变化。
    // 只在文字变了时才改，免得读屏软件每轮都念一遍
    const sig = JSON.stringify([steps.map(s => s.status), t.label, t.done, t.total, (t.log || []).length, (t.log || []).slice(-1)[0]]);
    if (sig !== lastSig) { lastSig = sig; lastChange = Date.now(); }
    const idle = Date.now() - lastChange, net = t.net && t.net.ago <= 60 ? t.net : null;
    const slowEl = $("#t-slow");
    slowEl.hidden = t.ended || !(net || idle >= STALL_MS);
    if (!slowEl.hidden) {
      const sec = Math.floor(idle / 1000);
      const title = net ? `${net.host ? net.host + " " : ""}${net.what}，已经 ${net.count} 次`
        : `已经 ${sec < 10 ? sec + " 秒" : sec < 60 ? Math.floor(sec / 10) * 10 + " 多秒" : Math.floor(sec / 60) + " 分多钟"}没有新进度`;
      if ($("#t-slow-title").textContent !== title) $("#t-slow-title").textContent = title;
    }
    $("#t-foot").innerHTML = `
      ${success ? `<a class="btn primary" href="${K.href(t.result)}">${icon("check")} ${K.go}</a>` : ""}
      ${t.ended && t.error ? `<a class="btn" href="${K.back}">${icon("arrowLeft")} ${K.backText}</a>` : ""}
      ${!t.ended ? `<span class="warn-text">${icon("warn")} ${K.busy}</span>` : ""}`;
    if (t.ended) { ended = true; stopPoll(); if (!t.error) toast(K.ok); }
  };
  // ---- 待决文件面板（整合包导入完成后逐项处理；任务已结束，不再轮询，应用后自己重画） ----
  let pendingItems = [];
  let pendingSig = "";
  let hadPending = false; // 之前画到过待决项（只有从「有待决」清到零才显示收尾卡片，从来没有待决的任务不显示）
  const PND_ACT = { skip: "跳过", local: "用本地文件代替", trust: "信任站点并下载" };
  const drawPending = (items, refresh) => {
    const box = $("#t-pending");
    if (!box) return;
    // 任务页停轮询之前不会再重画（避免盖掉用户正在选的选项）；应用处理后强制重画
    const sig = JSON.stringify(items || []);
    if (sig === pendingSig && !refresh) return;
    pendingSig = sig;
    pendingItems = items || [];
    if (!pendingItems.length) {
      box.innerHTML = hadPending ? `<div class="warn-card"><div class="wc-title">${icon("check")} 待决清单已全部处理完</div></div>` : "";
      hadPending = false;
      return;
    }
    hadPending = true;
    box.innerHTML = `<div class="warn-card">
      <div class="wc-title">${icon("warn")} 待决文件（${pendingItems.length} 个）</div>
      <div class="wc-note">这些都是没法自动下载的文件（魔改模组的自定义直链、缺失校验值、下载内容对不上、CurseForge 解析不到）。逐个选一种处理方式后应用；没选的先留着。</div>
      ${pendingItems.map(it => `
      <div class="pnd-item" data-id="${esc(it.id)}" style="border-top:1px solid var(--line);margin-top:10px;padding-top:10px">
        <div><b>${esc(it.path)}</b>${it.host ? ` <span class="badge">${esc(it.host)}</span>` : ""}</div>
        <div class="wc-note">${esc(it.note || "")}${it.size ? `（整合包标 ${fmtBytes(it.size)}）` : ""}
          ${(it.urls || []).map(u => ` <a href="${esc(u)}" target="_blank" rel="noopener">下载地址</a>`).join("")}</div>
        <div class="row" style="flex-wrap:wrap;margin-top:4px" role="radiogroup" aria-label="待决文件 ${esc(it.path)} 的处理方式">
          ${(it.options || ["skip", "local"]).filter(o => PND_ACT[o]).map(o =>
            `<label style="display:inline-flex;gap:5px;align-items:center;margin-right:14px"><input type="radio" name="pnd-${esc(it.id)}" value="${o}"><span>${PND_ACT[o]}</span></label>`).join("")}
        </div>
        <input type="text" class="pnd-local" style="width:100%;margin-top:6px" placeholder="选择「用本地文件代替」时填写：本地文件或目录的完整路径（目录按文件名匹配）" aria-label="待决文件 ${esc(it.path)} 的本地文件或目录路径" hidden>
        <label class="pnd-ack" style="display:inline-flex;gap:6px;align-items:flex-start;margin-top:6px" hidden><input type="checkbox" class="pnd-ackbox">
          <span class="wc-note">我已了解风险：这个文件没有可信来源的校验背书，站点内容也可能中途被换；只在知道 ${it.host ? `「${esc(it.host)}」` : "这个站点"}是什么时才信任它。确认按域名记忆（这个域名的不同端口也一并信任），以后同域名不用再确认。</span></label>
      </div>`).join("")}
      <div class="row" style="margin-top:12px">
        <button type="button" class="btn primary" id="t-pending-apply">应用所选的处理</button>
        <span class="hint">没法自动判断的魔改模组，推荐选「用本地文件代替」指向自己那份</span>
      </div>
    </div>`;
    wirePending();
  };
  const wirePending = () => {
    for (const row of document.querySelectorAll("#t-pending .pnd-item")) {
      for (const radio of row.querySelectorAll("input[type=radio]")) {
        radio.onchange = () => {
          row.querySelector(".pnd-local").hidden = true;
          row.querySelector("label.pnd-ack").hidden = true;
          if (radio.value === "local") row.querySelector(".pnd-local").hidden = false;
          if (radio.value === "trust") row.querySelector("label.pnd-ack").hidden = false;
        };
      }
    }
    const apply = $("#t-pending-apply");
    if (!apply) return;
    apply.onclick = async () => {
      const acts = [];
      let missing = "";
      for (const it of pendingItems) {
        const radio = document.querySelector(`#t-pending input[name="pnd-${CSS.escape(it.id)}"]:checked`);
        if (!radio) continue; // 没选的留着下一批
        const row = document.querySelector(`#t-pending .pnd-item[data-id="${CSS.escape(it.id)}"]`);
        const act = { id: it.id, act: radio.value };
        if (radio.value === "local") {
          act.path = (row.querySelector(".pnd-local").value || "").trim();
          if (!act.path && !missing) missing = `${it.path} 选了「用本地文件代替」但没给路径`;
        }
        if (radio.value === "trust") {
          act.url = (it.urls || [])[0] || "";
          act.ack = row.querySelector(".pnd-ackbox").checked;
        }
        acts.push(act);
      }
      if (missing) { toast(missing, true); return; }
      if (!acts.length) { toast("先给至少一个文件选处理方式", true); return; }
      apply.disabled = true;
      try {
        const r = await api(`/api/import/pending/${encodeURIComponent(taskId)}`, { method: "POST", body: { items: acts } });
        if (stale(gen)) return;
        const out = $("#t-pending-out");
        if (out) out.innerHTML = `<div class="wc-wrap">${(r.resolved || []).map(o =>
          `<div class="${o.ok ? "hint" : "err-box"}" style="margin-bottom:6px">${o.ok ? "已处理" : "没处理成"}：${esc(o.path)}${o.note ? "——" + esc(o.note) : ""}</div>`).join("")}</div>`;
        if (!(r.pending || []).length) {
          toast("待决清单已全部处理完");
          drawPending(r.pending || [], true);
        } else if (JSON.stringify(r.pending || []) !== pendingSig) {
          // 清单变了才重画；没选的、没处理成的那些项，把用户已经选的选项、路径和确认勾原样带回去
          const keep = {};
          for (const it of r.pending || []) {
            const row = document.querySelector(`#t-pending .pnd-item[data-id="${CSS.escape(it.id)}"]`);
            if (!row) continue;
            const radio = row.querySelector("input[type=radio]:checked");
            keep[it.id] = {
              act: radio ? radio.value : "",
              path: (row.querySelector(".pnd-local") || {}).value || "",
              ack: !!(row.querySelector(".pnd-ackbox") || { checked: false }).checked,
            };
          }
          drawPending(r.pending || [], true);
          for (const it of r.pending || []) {
            const st = keep[it.id];
            if (!st || !st.act) continue;
            const row = document.querySelector(`#t-pending .pnd-item[data-id="${CSS.escape(it.id)}"]`);
            if (!row) continue;
            const radio = row.querySelector(`input[value="${st.act}"]`);
            if (radio) { radio.checked = true; radio.dispatchEvent(new Event("change")); }
            if (st.path && row.querySelector(".pnd-local")) row.querySelector(".pnd-local").value = st.path;
            if (st.ack && row.querySelector(".pnd-ackbox")) row.querySelector(".pnd-ackbox").checked = true;
          }
        }
      } catch (e) {
        if (!stale(gen)) toast(e.message, true);
      } finally {
        if (!stale(gen) && $("#t-pending-apply")) $("#t-pending-apply").disabled = false;
      }
    };
  };
  try { await draw(); } catch (e) { console.warn("首次任务渲染失败，转由轮询接管：", e); }
  if (!ended) startPoll(draw, 800, gen);
}

/* 设置项搜索（常用设置、全部配置共用）：按每一项的 data-search（中文名 + 键名）过滤，不分大小写，
   空格、横线、下划线、点都不算（「view distance」「viewdistance」都能找到 view-distance）。返回命中的项数 */
const searchKey = s => String(s || "").toLowerCase().replace(/[\s\-_.]+/g, "");
function filterBySearch(q, items) {
  const k = searchKey(q);
  let n = 0;
  items.forEach(it => {
    const hit = !k || searchKey(it.dataset.search).includes(k);
    it.style.display = hit ? "" : "none";
    if (hit) n++;
  });
  return n;
}
/* 搜索框：边输边过滤，Esc 清空 */
function wireSearch(input, apply) {
  input.oninput = apply;
  input.onkeydown = e => {
    if (e.key === "Escape" && input.value) { e.preventDefault(); e.stopPropagation(); input.value = ""; apply(); }
  };
}

/* ---------- 视图：实例详情 ---------- */
/* 详情页头的状态胶囊：像素状态灯 + 文字，运行中再带时长与在线人数 */
function headStatus(i) {
  const meta = i.status === "running"
    ? `<span class="st-meta">· ${fmtDur(i.uptimeSec)}${i.kind !== "proxy" ? ` · ${i.onlineCount}/${i.maxPlayers} 人` : ""}</span>` : "";
  return stPill(i.status, meta);
}

async function renderDetail(name, tab = "console") {
  const gen = viewGen;
  let list;
  try { list = await api("/api/instances"); } catch (e) { if (!stale(gen)) Main().innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
  if (stale(gen)) return; // 等待期间已切走：不再建 SSE / 定时器
  const info = list.find(i => i.name === name);
  if (!info) { Main().innerHTML = `<div class="err-box">实例不存在</div>`; return; }
  // 页头：大号像素头像 + 标题，标题下一行是状态胶囊与徽标；操作按钮靠右
  Main().innerHTML = `<nav class="crumbs" aria-label="位置"><a href="#/instances">服务器</a>${icon("chevronRight")}<span>${esc(name)}</span></nav>
    <div class="detail-head">
      ${avatarOf(info.core, "lg")}
      <div class="dh-text"><h1>${esc(name)}</h1>
        <div class="dh-line"><span id="d-status">${headStatus(info)}</span>${coreBadge(info.core)}<span class="badge">${info.kind === "proxy" ? "v" : "MC "}${esc(info.mc)}</span><span class="badge" id="d-java">Java ${info.javaMajor}</span></div></div>
      <span class="grow"></span>
      <button class="btn primary" id="d-start" aria-label="启动 ${esc(name)}" ${info.status !== "stopped" ? "disabled" : ""}>${icon("play")} 启动</button>
      <button class="btn" id="d-stop" aria-label="停止 ${esc(name)}" ${info.status === "stopped" || info.status === "stopping" ? "disabled" : ""}>${icon("stop")} 停止</button>
      <button class="btn" id="d-dir" aria-haspopup="menu">${icon("folder")} 打开目录 ${icon("chevronDown")}</button>
    </div>
    <div class="tabs">${(() => {
      const isProxy = info.kind === "proxy";
      const tabs = [["console", "控制台"]];
      if (!isProxy) tabs.push(["players", "玩家"], ["backups", "备份"]);
      tabs.push(["tasks", "定时任务"]);
      if (info.core !== "vanilla") tabs.push(["res", "资源"]);
      if (window.MDModCart && MDModCart.applies(info)) tabs.push(["cart", "购物车"]); // 自选模组 + 购物车，见 modcart.js
      if (window.MDMusic && MDMusic.applies(info.core)) tabs.push(["music", "音乐共享"]); // 第五阶段，见 music.js
      if (window.MDGameChat && MDGameChat.applies(info)) tabs.push(["gameai", "游戏内 AI"]); // 第四阶段，见 gamechat.js
      if (window.MDCompanion && MDCompanion.applies(info)) tabs.push(["companion", "伙伴"]); // 第七阶段，见 companion.js
      if (!isProxy) tabs.push(["common", "常用设置"], ["raw", "全部配置"]);
      tabs.push(["jvm", "内存 / 启动"]);
      const base = "#/inst/" + encodeURIComponent(name) + "/";
      return tabs.map(([id, label]) => `<a class="tab ${tab === id ? "cur" : ""}" data-t="${id}" href="${base}${id}"${tab === id ? ` aria-current="page"` : ""}>${label}</a>`).join("");
    })()}</div>
    <div id="tab-body" class="sec-enter"></div>`;
  if (samePage) slideKeys(keysBefore, ["tab"]); // 同一实例换标签：键帽从旧标签滑到新标签
  $("#d-dir").onclick = () => dropMenu($("#d-dir"), folderItems(info));
  $("#d-start").onclick = async () => {
    try { $("#d-start").disabled = true; await api(`/api/instances/${encodeURIComponent(name)}/start`, { method: "POST" }); toast("正在启动，首次生成世界可能需要一两分钟"); refreshHead(); }
    catch (e) { toast(e.message, true); $("#d-start").disabled = false; }
  };
  $("#d-stop").onclick = async () => {
    try { $("#d-stop").disabled = true; await api(`/api/instances/${encodeURIComponent(name)}/stop`, { method: "POST" }); toast("正在保存世界并停止"); refreshHead(); }
    catch (e) { toast(e.message, true); $("#d-stop").disabled = false; }
  };
  const refreshHead = async () => {
    try {
      const l = await api("/api/instances");
      if (stale(gen)) return;
      const cur = l.find(i => i.name === name);
      if (!cur) return;
      const was = info.status;
      info.status = cur.status; // 各 tab 读到的状态随之更新
      $("#d-status").innerHTML = headStatus(cur);
      animatePill($("#d-status .st-pill"), was, cur.status); // 状态变了：胶囊变色、文字换场，进入运行中放一圈波纹
      $("#d-start").disabled = cur.status !== "stopped";
      $("#d-stop").disabled = cur.status === "stopped" || cur.status === "stopping";
    } catch {}
  };
  startPoll(refreshHead, 2500, gen);

  // 模组声明了比实例更高的 Java 要求时给提示（不阻断：1.20.1 配 Java 25 的魔改模组是允许的组合）。
  // 只在有模组目录的核心上查一次；vanilla / 代理端会 400，静默忽略。
  if (info.kind !== "proxy" && info.core !== "vanilla") {
    api(`/api/instances/${encodeURIComponent(name)}/mods`).then(r => {
      if (stale(gen) || !r || !r.needJava || r.needJava <= (info.javaMajor || 0)) return;
      const host = document.querySelector(".dh-text");
      if (!host || $("#d-java-hint")) return;
      const hint = document.createElement("div");
      hint.id = "d-java-hint";
      hint.className = "dh-line";
      hint.style.marginTop = "2px";
      hint.innerHTML = `<a class="warn-text" href="#/inst/${encodeURIComponent(name)}/jvm">${icon("warn")} 有模组要求 Java ${r.needJava}，这台服务器现在是 Java ${info.javaMajor}，启动可能失败。到「内存 / 启动」换一下（也可以先不换）</a>`;
      host.appendChild(hint);
    }).catch(() => {});
  }

  const body = $("#tab-body");
  if (tab === "console") {
    body.innerHTML = `
      <div class="console-bar">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="con-filter" placeholder="过滤日志（如 ERROR / 玩家名）" style="width:280px"></div>
        <label class="switch"><input type="checkbox" id="con-scroll" checked><span class="sw"></span><span class="sw-label">自动滚动</span></label>
        <span class="grow"></span>
        <span class="hint">编码</span>
        <select id="con-enc" style="width:110px" aria-label="控制台编码">
          ${["auto", "utf-8", "gbk"].map(e => `<option value="${e}" ${info.consoleEncoding === e ? "selected" : ""}>${e}</option>`).join("")}
        </select>
        <button class="btn sm ghost" id="con-fs-dn" title="缩小字号">A-</button>
        <button class="btn sm ghost" id="con-fs-up" title="放大字号">A+</button>
        <button class="btn sm ghost icon-only" id="con-copy" title="复制全部" aria-label="复制全部">${icon("copy")}</button>
        <button class="btn sm ghost icon-only" id="con-export" title="导出为 txt" aria-label="导出为 txt">${icon("download")}</button>
      </div>
      <div class="diag-slot" id="diag-slot"></div>
      <div class="console" id="console"></div>
      <div class="console-input">
        <div class="cmd-suggest" id="cmd-suggest"></div>
        <input type="text" id="cmd-in" autocomplete="off" placeholder="如 op 玩家名 / say 大家好" aria-label="服务器命令" aria-describedby="cmd-keys">
        <button class="btn primary" id="cmd-send">发送</button>
      </div>
      <div class="hint console-keys" id="cmd-keys"><span><code class="kbd">回车</code> 发送</span><span><code class="kbd">Tab</code> 补全</span><span><code class="kbd">↑</code><code class="kbd">↓</code> 翻历史</span><span><code class="kbd">Esc</code> 关掉补全</span><span>在别处按 <code class="kbd">T</code> 或 <code class="kbd">/</code> 跳到这里</span></div>`;
    const con = $("#console");
    const filterEl = $("#con-filter");
    // 命令补全用：在线玩家名（进出游戏时刷新）
    let onlinePlayers = [];
    const refreshPlayers = () => api(`/api/instances/${encodeURIComponent(name)}/players`)
      .then(pl => { onlinePlayers = pl.online || []; }).catch(() => {});
    const schedulePlayerRefresh = () => { clearTimeout(prTimer); prTimer = setTimeout(refreshPlayers, 600); };
    refreshPlayers();
    // 字号（localStorage 记忆）+ 复制 / 导出
    let conFS = +(localStorage.getItem("amh_con_fs") || 12.5);
    const applyFS = () => { con.style.fontSize = conFS + "px"; };
    applyFS();
    $("#con-fs-dn").onclick = () => { conFS = Math.max(9, conFS - 1); localStorage.setItem("amh_con_fs", conFS); applyFS(); };
    $("#con-fs-up").onclick = () => { conFS = Math.min(22, conFS + 1); localStorage.setItem("amh_con_fs", conFS); applyFS(); };
    const conText = () => [...con.childNodes].map(d => d.textContent).join("\n");
    $("#con-copy").onclick = () => copy(conText(), "已复制控制台内容");
    $("#con-export").onclick = () => {
      const a = document.createElement("a");
      a.href = URL.createObjectURL(new Blob([conText()], { type: "text/plain;charset=utf-8" }));
      a.download = `${name}-console.txt`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 1000);
    };
    const classify = line => {
      if (line.startsWith(">") || line.startsWith("[MineDeck]")) return "cmd-echo";
      if (/ERROR|SEVERE|FATAL|Exception|^\tat |^Caused by/.test(line)) return "lv-err";
      if (/WARN/.test(line)) return "lv-warn";
      return "";
    };
    const push = line => {
      const d = document.createElement("div");
      d.textContent = line;
      const cls = classify(line);
      if (cls) d.className = cls;
      const q = filterEl.value.trim().toLowerCase();
      if (q && !line.toLowerCase().includes(q)) d.style.display = "none";
      con.appendChild(d);
      while (con.childNodes.length > 3000) con.removeChild(con.firstChild);
      if ($("#con-scroll").checked) con.scrollTop = con.scrollHeight;
      if (/ (joined|left) the game/.test(line)) schedulePlayerRefresh();
      window.MDAssist?.consoleLine(name, line); // 报错横幅（assistant.js）：出现报错、退出时重新跑本地规则
    };
    filterEl.oninput = () => {
      const q = filterEl.value.trim().toLowerCase();
      con.childNodes.forEach(d => d.style.display = (!q || d.textContent.toLowerCase().includes(q)) ? "" : "none");
    };
    $("#con-enc").onchange = async () => {
      try {
        await api(`/api/instances/${encodeURIComponent(name)}/settings`, { method: "PUT", body: { xmxMb: 0, xmsMb: 0, consoleEncoding: $("#con-enc").value } });
        toast("编码已切换，对之后的新输出生效");
      } catch (e) { toast(e.message, true); }
    };
    consoleES = new EventSource(`/api/instances/${encodeURIComponent(name)}/console?token=${TOKEN()}`);
    consoleES.onmessage = e => push(JSON.parse(e.data));
    consoleES.onerror = () => push("[MineDeck] 控制台连接中断，切换页面可重连");
    window.MDAssist?.banner($("#diag-slot"), name, gen);
    const histKey = "amh_hist_" + name;
    let hist = [];
    try { hist = JSON.parse(localStorage.getItem(histKey)) || []; } catch {}
    let histIdx = -1, draft = "";
    const send = async () => {
      const v = $("#cmd-in").value.trim();
      if (!v) return;
      $("#cmd-in").value = "";
      closeSugg();
      if (hist[0] !== v) { hist.unshift(v); hist = hist.slice(0, 50); localStorage.setItem(histKey, JSON.stringify(hist)); }
      histIdx = -1;
      try { await api(`/api/instances/${encodeURIComponent(name)}/command`, { method: "POST", body: { cmd: v } }); }
      catch (e) { toast(e.message, true); }
    };
    $("#cmd-send").onclick = send;

    /* ---- 命令补全（常用命令 + 在线玩家名，Tab / 点击补全） ---- */
    const COMMON_CMDS = [
      "say ", "tell ", "op ", "deop ", "kick ", "ban ", "ban-ip ", "pardon ", "tp ", "give ",
      "gamemode ", "defaultgamemode ", "difficulty ", "time set day", "time set night",
      "weather clear", "weather rain", "whitelist ", "gamerule ", "kill ", "xp ", "effect ",
      "title ", "setworldspawn", "list", "save-all", "save-off", "save-on", "seed", "reload", "stop",
    ];
    const PLAYER_CMDS = new Set(["op", "deop", "kick", "ban", "ban-ip", "pardon", "tp", "kill", "give", "gamemode", "xp", "effect", "tell", "title"]);
    const WL_SUB = ["add", "remove", "on", "off", "list", "reload"];
    const suggEl = $("#cmd-suggest");
    let sugg = [], suggIdx = -1;
    const closeSugg = () => { sugg = []; suggIdx = -1; suggEl.style.display = "none"; suggEl.innerHTML = ""; };
    const computeSugg = val => {
      if (!val) return [];
      const words = val.split(/\s+/).filter(Boolean);
      const trailingSpace = /\s$/.test(val);
      const first = (words[0] || "").toLowerCase();
      const lastSpace = val.lastIndexOf(" ");
      const head = val.slice(0, lastSpace + 1);
      const token = val.slice(lastSpace + 1).toLowerCase();
      const players = t => onlinePlayers.filter(n => n.toLowerCase().startsWith(t)).slice(0, 8).map(n => ({ label: n, insert: head + n }));
      if (words.length <= 1 && !trailingSpace) {
        const p = val.toLowerCase();
        return COMMON_CMDS.filter(c => c.toLowerCase().startsWith(p) && c.trim().toLowerCase() !== p)
          .slice(0, 8).map(c => ({ label: c.trim(), insert: c }));
      }
      if (first === "whitelist") {
        if (words.length === 1 || (words.length === 2 && !trailingSpace)) {
          return WL_SUB.filter(s => s.startsWith(token)).map(s => ({ label: s, insert: head + s + " " }));
        }
        if (words[1] === "add" || words[1] === "remove") return players(token);
        return [];
      }
      if (PLAYER_CMDS.has(first)) return players(token);
      return [];
    };
    const renderSugg = () => {
      if (!sugg.length) { closeSugg(); return; }
      suggEl.innerHTML = sugg.map((s, i) => `<div class="cs-item${i === suggIdx ? " sel" : ""}" data-i="${i}">${esc(s.label)}</div>`).join("");
      suggEl.style.display = "block";
      suggEl.querySelectorAll(".cs-item").forEach(el => { el.onmousedown = ev => { ev.preventDefault(); applySugg(+el.dataset.i); }; });
    };
    const applySugg = i => {
      const s = sugg[i]; if (!s) return;
      const inp = $("#cmd-in");
      inp.value = s.insert; inp.focus();
      updateSugg();
    };
    const updateSugg = () => { sugg = computeSugg($("#cmd-in").value); suggIdx = -1; renderSugg(); };
    $("#cmd-in").oninput = updateSugg;
    $("#cmd-in").onblur = () => setTimeout(closeSugg, 120);
    $("#cmd-in").onkeydown = e => {
      const open = suggEl.style.display === "block" && sugg.length;
      if (open && e.key === "Tab") { e.preventDefault(); applySugg(suggIdx < 0 ? 0 : suggIdx); return; }
      if (open && e.key === "ArrowDown") { e.preventDefault(); suggIdx = (suggIdx + 1) % sugg.length; renderSugg(); return; }
      if (open && e.key === "ArrowUp") { e.preventDefault(); suggIdx = (suggIdx - 1 + sugg.length) % sugg.length; renderSugg(); return; }
      if (open && e.key === "Escape") { e.preventDefault(); closeSugg(); return; }
      if (open && e.key === "Enter" && suggIdx >= 0) { e.preventDefault(); applySugg(suggIdx); return; }
      if (e.key === "Enter") { send(); return; }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        if (histIdx < hist.length - 1) { if (histIdx === -1) draft = $("#cmd-in").value; histIdx++; $("#cmd-in").value = hist[histIdx]; }
      } else if (e.key === "ArrowDown") {
        e.preventDefault();
        if (histIdx > -1) { histIdx--; $("#cmd-in").value = histIdx === -1 ? draft : hist[histIdx]; }
      }
    };

  } else if (tab === "players") {
    const act = async (action, player) => {
      try {
        await api(`/api/instances/${encodeURIComponent(name)}/players`, { method: "POST", body: { action, player } });
        toast(`已执行 ${action} ${player}` + (info.status === "stopped" ? "" : "（名单文件稍后由服务器刷新）"));
        setTimeout(draw, 800);
      } catch (e) { toast(e.message, true); }
    };
    const draw = async () => {
      if (stale(gen)) return;
      let pl;
      try { pl = await api(`/api/instances/${encodeURIComponent(name)}/players`); }
      catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
      if (stale(gen)) return;
      // 三个名单的输入框、每个人名后面的「移除」「OP」「踢」都同名：可访问名称带上名单和玩家名
      const section = (title, items, addAction, removeAction, hint) => `
        <div class="p-sec">
          <div class="p-sec-head"><b>${title}</b>（${items.length}）
            <input type="text" data-add="${addAction}" placeholder="输入玩家名，回车添加" aria-label="添加玩家到${title}">
            ${hint ? `<span class="hint">${hint}</span>` : ""}
          </div>
          <div class="badges">${items.map(p =>
            `<span class="badge">${esc(p.name)}<button type="button" class="p-x" data-act="${removeAction}" data-p="${esc(p.name)}" title="移除" aria-label="移除 ${esc(p.name)}（${title}）">${icon("x")}</button></span>`).join("") || `<span class="sub">空</span>`}</div>
        </div>`;
      body.innerHTML = `<div class="players-grid">
        <div class="p-sec">
          <div class="p-sec-head"><b>在线玩家</b>（${pl.online.length}）<button class="btn sm" id="pl-refresh">${icon("refresh")} 刷新</button></div>
          <div class="badges">${pl.online.map(n => `<span class="badge core">${esc(n)}
            <button type="button" class="p-x" data-act="op" data-p="${esc(n)}" title="设为管理员" aria-label="OP ${esc(n)}">OP</button>
            <button type="button" class="p-x" data-act="kick" data-p="${esc(n)}" title="踢出" aria-label="踢 ${esc(n)}">踢</button></span>`).join("") || `<span class="sub">暂无玩家在线</span>`}</div>
        </div>
        ${section("白名单", pl.whitelist, "whitelist-add", "whitelist-remove", "要在「常用设置」里打开白名单（white-list）才生效。")}
        ${section("管理员 OP", pl.ops, "op", "deop", "")}
        ${section("封禁名单", pl.banned, "ban", "pardon", "")}
      </div>`;
      $("#pl-refresh").onclick = draw;
      body.querySelectorAll("input[data-add]").forEach(inp => inp.onkeydown = e => {
        if (e.key === "Enter" && inp.value.trim()) { act(inp.dataset.add, inp.value.trim()); inp.value = ""; }
      });
      body.querySelectorAll(".p-x").forEach(a => a.onclick = () => act(a.dataset.act, a.dataset.p));
    };
    await draw();

  } else if (tab === "backups") {
    const appInfo = await api("/api/app").catch(() => ({ config: {} }));
    if (stale(gen)) return;
    let keep = (appInfo.config && appInfo.config.backupKeep) || 10;
    const draw = async () => {
      if (stale(gen)) return;
      let list;
      try { list = await api(`/api/instances/${encodeURIComponent(name)}/backups`); }
      catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
      if (stale(gen)) return;
      body.innerHTML = `
        <div class="row" style="margin-bottom:10px;flex-wrap:wrap">
          <input type="text" id="bk-label" placeholder="备份标签（可选，如：打龙前）" style="max-width:240px">
          <button class="btn primary" id="bk-create">${icon("camera")} 立即备份世界</button>
          <span class="grow"></span>
          <label class="hint" style="display:flex;align-items:center;gap:6px;margin:0">保留最近 <input type="number" id="bk-keep" min="1" max="1000" value="${keep}" style="width:72px"> 份</label>
          <button class="btn sm" id="bk-keep-save">保存</button>
        </div>
        <div class="hint" style="margin-bottom:14px">服务器开着的时候也会自动备份：先暂停自动存档，再压成压缩包，然后恢复自动存档。份数超过上面的保留份数，会删掉最旧的。删掉服务器不会删掉已经做好的备份。保留份数是全局的，每台服务器都用这一份。</div>
        <div class="group has-ico bk-list">${(list || []).map(b => `
          <div class="row-item"><div class="ri-head"><span class="ri-ico">${icon("package")}</span>
            <div class="ri-main"><div class="ri-title">${esc(b.file)}</div><div class="ri-key">${b.sizeMb.toFixed(1)} MB · ${esc(b.time)}</div></div>
            <div class="ri-right"><button class="btn sm" data-restore="${esc(b.file)}" aria-label="还原 ${esc(b.file)}">${icon("restore")} 还原</button>
              <button class="btn sm danger" data-del="${esc(b.file)}" aria-label="删除 ${esc(b.file)}">删除</button></div></div></div>`).join("") ||
          `<div class="row-item"><div class="ri-head"><div class="ri-main"><div class="ri-sub">暂无备份</div></div></div></div>`}</div>`;
      $("#bk-create").onclick = async () => {
        $("#bk-create").disabled = true;
        try {
          const r = await api(`/api/instances/${encodeURIComponent(name)}/backups`, { method: "POST", body: { label: $("#bk-label").value.trim() } });
          toast(`备份完成：${r.file}`);
        } catch (e) { toast(e.message, true); }
        draw();
      };
      bindSaveInput($("#bk-keep"), $("#bk-keep-save"), async v => {
        const n = Math.max(1, Math.min(1000, Math.round(+v) || 10));
        await api("/api/config", { method: "PUT", body: { backupKeep: n } });
        keep = n; if (!stale(gen)) $("#bk-keep").value = n;
        toast(`已设置保留最近 ${n} 份（下次备份时生效）`);
      }, gen);
      body.querySelectorAll("[data-restore]").forEach(b => b.onclick = async () => {
        const r = await confirmModal({
          title: "还原备份？", danger: true, okText: "还原", icon: "restore",
          body: `将把世界回滚到 <b>${esc(b.dataset.restore)}</b>。<br>服务器必须是停止状态。当前世界会先自动备份一份。`,
        });
        if (!r) return;
        try { await api(`/api/instances/${encodeURIComponent(name)}/backups/restore`, { method: "POST", body: { file: b.dataset.restore } }); toast("还原完成"); }
        catch (e) { toast(e.message, true); }
        draw();
      });
      body.querySelectorAll("[data-del]").forEach(b => b.onclick = async () => {
        const r = await confirmModal({ title: "删除备份？", danger: true, okText: "删除", icon: "trash", body: esc(b.dataset.del) });
        if (!r) return;
        try { await api(`/api/instances/${encodeURIComponent(name)}/backups?file=${encodeURIComponent(b.dataset.del)}`, { method: "DELETE" }); }
        catch (e) { toast(e.message, true); }
        draw();
      });
    };
    await draw();

  } else if (tab === "tasks") {
    let pol;
    try { pol = await api(`/api/instances/${encodeURIComponent(name)}/policies`); }
    catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    let scheds = pol.schedules || [];
    const TYPE_NAMES = { restart: "定时重启", command: "定时命令", backup: "定时备份" };
    // 增删定时任务、切换崩溃重启都只是改了草稿：吸底保存条显示未保存项数，离开前会被拦下确认
    body.innerHTML = `<div style="max-width:640px">
      <div class="prop-item" style="margin-bottom:18px">
        <div><div class="pl">崩溃自动重启</div><div class="pd">异常退出后，会隔 10 秒、30 秒、60 秒再启动。连续崩溃 3 次就不再自动启动，并给出提示。</div></div>
        <label class="switch"><input type="checkbox" id="pol-crash" ${pol.crashRestart ? "checked" : ""} aria-label="崩溃自动重启"><span class="sw"></span></label>
      </div>
      <h3 style="margin-bottom:8px">每日定时任务</h3>
      <div id="sched-list"></div>
      <div class="row" style="margin:12px 0 4px;flex-wrap:wrap">
        <select id="sc-type" style="width:130px" aria-label="任务类型"><option value="restart">定时重启</option><option value="command">定时命令</option><option value="backup">定时备份</option></select>
        <input type="text" id="sc-at" class="num" value="04:00" inputmode="numeric" maxlength="5" placeholder="时:分" aria-label="每天几点执行（时:分）" aria-describedby="sc-hint" style="width:88px">
        <input type="text" id="sc-args" placeholder="命令内容（仅定时命令需要），如 say 大家好" style="flex:1;min-width:200px">
        <button class="btn" id="sc-add">${icon("plus")} 添加</button>
      </div>
      <div class="sc-quick" role="group" aria-label="快捷时间">
        <button type="button" class="btn sm" data-later="5">5 分钟后</button><button type="button" class="btn sm" data-later="10">10 分钟后</button><button type="button" class="btn sm" data-later="30">30 分钟后</button>
        <span class="hint" id="sc-hint">每天到这个时间执行一次（24 小时制，如 04:00、21:30）。</span>
      </div>
      <div class="field-err" id="sc-err"></div>
      ${saveBar("保存策略")}</div>`;
    let paintBar = () => {};
    const drawScheds = () => {
      $("#sched-list").innerHTML = scheds.map((s, i) => `
        <div class="java-row"><span class="badge core">${esc(TYPE_NAMES[s.type] || s.type)}</span>
          <span class="jv">每天 ${esc(s.at)}</span>
          <span class="jp">${esc(s.args || "")}</span>
          <span class="grow"></span>
          <button class="btn sm danger" data-i="${i}" aria-label="移除 ${esc(TYPE_NAMES[s.type] || s.type)} 每天 ${esc(s.at)}${s.args ? " " + esc(s.args) : ""}">移除</button></div>`).join("") ||
        `<div class="sub">暂无定时任务</div>`;
      $("#sched-list").querySelectorAll("[data-i]").forEach(b => b.onclick = () => { scheds.splice(+b.dataset.i, 1); drawScheds(); paintBar(); });
    };
    drawScheds();
    const scErr = (msg, badTime = false) => { $("#sc-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; $("#sc-at").classList.toggle("invalid", badTime); };
    // 时间：「时:分」文本框（原来的 input[type=time] 两个小格太难点、太难改，见测试报告观察 1）。
    // 认 4:00、04:00、0400、21：30 这几种写法，一律补齐成 HH:MM——后端按 HH:MM 逐字比对当前时刻，9:05 不补零就永远不会触发
    const pad2 = n => String(n).padStart(2, "0");
    const parseHM = v => {
      const m = String(v).trim().match(/^(\d{1,2})[:：.](\d{1,2})$/) || String(v).trim().match(/^(\d{1,2})(\d{2})$/);
      if (!m || +m[1] > 23 || +m[2] > 59) return "";
      return pad2(+m[1]) + ":" + pad2(+m[2]);
    };
    const HINT = $("#sc-hint").textContent;
    $("#sc-at").addEventListener("input", () => { $("#sc-hint").textContent = HINT; if ($("#sc-at").classList.contains("invalid")) scErr(""); });
    $("#sc-at").addEventListener("blur", () => { const hm = parseHM($("#sc-at").value); if (hm) $("#sc-at").value = hm; });
    // 快捷项：填成几分钟后的时刻（只是填时间，还要点「添加」、再保存策略）
    body.querySelectorAll("[data-later]").forEach(b => b.onclick = () => {
      const d = new Date(Date.now() + b.dataset.later * 60000), hm = pad2(d.getHours()) + ":" + pad2(d.getMinutes());
      const day = d.toDateString() === new Date().toDateString() ? "今天" : "明天";
      $("#sc-at").value = hm;
      scErr("");
      $("#sc-hint").textContent = `已填成 ${hm}。${day} ${hm} 会执行第一次，以后每天这个时间都执行。只想执行一次的话，执行完把它移除。点「添加」，再保存，才会生效。`;
    });
    ["#sc-at", "#sc-args"].forEach(s => $(s).addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); $("#sc-add").click(); } }));
    $("#sc-add").onclick = () => {
      const s = { type: $("#sc-type").value, at: parseHM($("#sc-at").value), args: $("#sc-args").value.trim() };
      if (!s.at) { scErr("时间写成 时:分，如 04:00 或 21:30（24 小时制）", true); $("#sc-at").focus(); return; }
      $("#sc-at").value = s.at;
      if (s.type === "command" && !s.args) { scErr("定时命令需要填写命令内容，如 say 大家好"); $("#sc-args").focus(); return; }
      scErr("");
      scheds.push(s);
      $("#sc-args").value = "";
      drawScheds();
      paintBar();
    };
    paintBar = wireSaveBar(body, gen,
      () => ({ crash: String($("#pol-crash").checked), schedules: JSON.stringify(scheds) }),
      async () => {
        await api(`/api/instances/${encodeURIComponent(name)}/policies`, { method: "PUT", body: { crashRestart: $("#pol-crash").checked, schedules: scheds } });
        toast("策略已保存并即时生效");
      });

  } else if (tab === "res") {
    body.innerHTML = `
      <div class="row" style="margin-bottom:12px;flex-wrap:wrap">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="res-q" placeholder="搜索 Modrinth 模组/插件（英文名效果更好）" style="width:340px"></div>
        <button class="btn" id="res-go">${icon("search")} 搜索</button>
        <span class="hint">已按当前核心与版本过滤兼容资源 · 数据来自 Modrinth（免费开放）</span>
      </div>
      <div id="res-list"><div class="sub">输入关键词搜索，或直接点搜索看热门资源</div></div>`;
    let searchReq = 0;
    const search = async () => {
      const req = ++searchReq;
      const outdated = () => stale(gen) || req !== searchReq;
      $("#res-list").textContent = "搜索中…";
      try {
        const r = await api(`/api/instances/${encodeURIComponent(name)}/resources/search?q=${encodeURIComponent($("#res-q").value.trim())}`);
        if (outdated()) return;
        $("#res-list").innerHTML = (r.hits || []).map(h => `
          <div class="java-row" style="max-width:880px">
            ${h.icon_url ? `<img class="res-icon" src="${esc(h.icon_url)}" width="34" height="34" alt="" onerror="this.remove()">` : `<span class="res-noicon">${icon("package")}</span>`}
            <div style="flex:1;min-width:0"><b>${esc(h.title)}</b>
              <div class="jp" style="white-space:normal;max-height:32px;overflow:hidden">${esc(h.description)}</div></div>
            <span class="jv" style="color:var(--muted);display:inline-flex;align-items:center;gap:4px">${h.downloads >= 1e6 ? (h.downloads / 1e6).toFixed(1) + "M" : Math.round(h.downloads / 1e3) + "k"}${icon("download")}</span>
            <button class="btn sm primary" data-rid="${esc(h.project_id)}" data-rt="${esc(h.title)}" aria-label="安装 ${esc(h.title)}">${icon("download")} 安装</button>
          </div>`).join("") || `<div class="sub">没有找到兼容 ${esc(info.mc)} 的资源，换个关键词试试</div>`;
        // 20 个结果的按钮都叫「安装」、模组名又相近，名字带上模组名（测试报告观察 9）；按钮上的字变了，名字跟着变
        $("#res-list").querySelectorAll("[data-rid]").forEach(b => b.onclick = async () => {
          const label = s => b.setAttribute("aria-label", s + " " + b.dataset.rt);
          b.disabled = true;
          b.textContent = "安装中…"; label("安装中…");
          try {
            const res = await api(`/api/instances/${encodeURIComponent(name)}/resources/install`, { method: "POST", body: { projectId: b.dataset.rid } });
            toast(`已安装 ${b.dataset.rt}，文件是 ${res.file}。重启服务器后生效`);
            b.innerHTML = icon("check") + " 已安装"; label("已安装");
          } catch (e) { toast(e.message, true); b.disabled = false; b.innerHTML = icon("download") + " 安装"; label("安装"); }
        });
      } catch (e) { if (!outdated()) $("#res-list").innerHTML = `<div class="err-box">${esc(e.message)}</div>`; }
    };
    $("#res-go").onclick = search;
    $("#res-q").onkeydown = e => { if (e.key === "Enter") search(); };

  } else if (tab === "common") {
    if (info.kind === "proxy") {
      body.innerHTML = `<div class="sub">代理端的监听端口、后端服务器列表，在这台服务器目录里的配置文件中。Velocity 是 velocity.toml。Waterfall 是 config.yml。第一次启动之后才会生成。用「打开目录」去改。</div>`;
      return;
    }
    let data;
    try { data = await api(`/api/instances/${encodeURIComponent(name)}/properties`); } catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    const cur = Object.fromEntries(data.pairs.map(p => [p.key, p.value]));
    const mc = info.mc, running = data.running;
    const GROUPS = ["基础与玩法", "世界生成", "玩家与权限", "性能", "网络与远程管理", "资源包", "安全与杂项"];
    // 按实例版本计算适用项与呈现模式（gamerule 迁移项在新版转为 /gamerule 控件）
    const items = [];
    for (const p of (window.PROPS_CATALOG || [])) {
      if (p.since && verLt(mc, p.since)) continue;                 // 尚未引入
      let mode;
      if (p.type === "gamerule-bool") mode = (p.removed && verGte(mc, p.removed)) ? "gamerule" : "bool";
      else { if (p.removed && verGte(mc, p.removed)) continue; mode = p.type; } // 该版本已移除的属性 → 隐藏
      if (p.key === "level-type" && verLt(mc, "1.16")) mode = "str";  // 旧版世界类型写法不同，转自由文本
      items.push({ ...p, mode });
    }
    const inGroup = g => items.filter(p => p.group === g);
    // 每个控件都用设置项的中文名当可访问名称（行里的名字和控件没有 label 关联，读屏和自动化原来读不到名字）
    const fieldHTML = p => {
      const v = cur[p.key] ?? p.def, al = `aria-label="${esc(p.label)}"`;
      if (p.key === "motd") return `<div class="motd-palette" id="motd-pal"></div>
        <input type="text" data-k="motd" id="motd-in" value="${esc(v ?? "")}" ${al} style="width:100%;margin-top:6px"><div class="motd-preview" id="motd-prev"></div>`;
      if (p.mode === "gamerule") return `<label class="switch" title="游戏规则：服务器运行时通过 /gamerule 即时生效"><input type="checkbox" data-gr="${p.gamerule}" ${v === "true" ? "checked" : ""} ${running ? "" : "disabled"} ${al}><span class="sw"></span></label>`;
      if (p.mode === "bool") return `<label class="switch"><input type="checkbox" data-k="${p.key}" ${v === "true" ? "checked" : ""} ${al}><span class="sw"></span></label>`;
      if (p.mode === "sel") return `<select data-k="${p.key}" ${al}>${p.opts.map((o, i) => `<option value="${o}" ${v === o ? "selected" : ""}>${esc(p.optNames ? p.optNames[i] : o)}</option>`).join("")}</select>`;
      if (p.mode === "int") return `<input type="number" data-k="${p.key}" value="${esc(v ?? "")}" ${al}>`;
      return `<input type="text" data-k="${p.key}" value="${esc(v ?? "")}" ${al} style="width:200px">`;
    };
    const rowHTML = p => {
      const tags = (p.restart ? `<span class="ri-restart">重启生效</span>` : "") + (p.mode === "gamerule" ? `<span class="gr-tag">游戏规则</span>` : "");
      const notice = (p.key === "white-list" && verGte(mc, "26.3")) ? `<div class="pd" style="color:var(--warning-text)">注意：此版本起白名单默认开启</div>` : "";
      const grHint = (p.mode === "gamerule" && !running) ? "（服务器启动后可切换）" : "";
      if (p.key === "motd") return `<div class="prop-item wide" data-search="${esc(p.label)} ${p.key}"><div style="flex:1">
        <div class="pl">${esc(p.label)} ${tags}</div><div class="pd">${esc(p.desc || "")}</div><div class="pk">${p.key}</div>${fieldHTML(p)}</div></div>`;
      return `<div class="prop-item" data-search="${esc(p.label)} ${p.key}"><div>
        <div class="pl">${esc(p.label)} ${tags}</div><div class="pd">${esc(p.desc || "")}${grHint}</div>${notice}<div class="pk">${p.key}</div></div>${fieldHTML(p)}</div>`;
    };
    body.innerHTML = `
      <div class="common-bar">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="cp-search" placeholder="搜索设置项（中文名 / 键名，如 视距 / rcon）" style="width:320px"></div>
        <div class="cp-chips">${GROUPS.map((g, i) => inGroup(g).length ? `<button class="cp-chip" data-g="${i}">${g}</button>` : "").join("")}</div>
      </div>
      ${data.pairs.length === 0 ? `<div class="warn-text" style="margin-bottom:12px;display:flex;align-items:flex-start;gap:7px;line-height:1.7">${icon("warn")}<span>服务器还没第一次启动。有些项要等第一次启动之后，由服务端补上。现在填的值会留下。</span></div>` : ""}
      <div id="cp-list">${GROUPS.map((g, i) => {
        const gi = inGroup(g);
        return gi.length ? `<div class="cp-group" id="cpg-${i}"><h3 class="cp-gh">${g}<span class="cp-gn">${gi.length}</span></h3>
          <div class="props-grid">${gi.map(rowHTML).join("")}</div></div>` : "";
      }).join("")}</div>
      ${saveBar("保存设置", running ? `<span class="warn-text">${icon("warn")} 服务器开着。保存后要重启才生效。游戏规则保存后马上生效。</span>` : "")}`;
    body.querySelectorAll(".cp-chip").forEach(c => c.onclick = () => $("#cpg-" + c.dataset.g, body)?.scrollIntoView({ behavior: "smooth", block: "start" }));
    wireSearch($("#cp-search"), () => {
      const q = $("#cp-search").value;
      body.querySelectorAll(".cp-group").forEach(g => { g.style.display = filterBySearch(q, g.querySelectorAll(".prop-item")) ? "" : "none"; });
    });
    // 游戏规则开关：运行时即时发送 /gamerule（不写入 server.properties）
    body.querySelectorAll("[data-gr]").forEach(sw => sw.onchange = async () => {
      const rule = sw.dataset.gr, val = sw.checked;
      try { await api(`/api/instances/${encodeURIComponent(name)}/command`, { method: "POST", body: { cmd: `gamerule ${rule} ${val}` } }); toast(`已设置 gamerule ${rule} ${val}`); }
      catch (e) { toast(e.message, true); sw.checked = !val; }
    });
    // MOTD 编辑器：色码调色板 + 实时预览
    const motdIn = $("#motd-in", body);
    if (motdIn) {
      const pal = $("#motd-pal", body);
      const prev = $("#motd-prev", body);
      const styles = [{ c: "l", t: "粗体" }, { c: "o", t: "斜体" }, { c: "n", t: "下划线" }, { c: "m", t: "删除线" }, { c: "r", t: "重置" }];
      [..."0123456789abcdef"].forEach(c => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "pal-btn";
        b.style.background = MC_COLORS[c];
        b.title = "§" + c;
        b.onclick = () => insertCode(c);
        pal.appendChild(b);
      });
      styles.forEach(({ c, t }) => {
        const b = document.createElement("button");
        b.type = "button";
        b.className = "pal-btn txt";
        b.textContent = t;
        b.title = "§" + c;
        b.onclick = () => insertCode(c);
        pal.appendChild(b);
      });
      function insertCode(c) {
        const pos = motdIn.selectionStart ?? motdIn.value.length;
        motdIn.value = motdIn.value.slice(0, pos) + "§" + c + motdIn.value.slice(pos);
        motdIn.focus();
        motdIn.selectionStart = motdIn.selectionEnd = pos + 2;
        motdIn.dispatchEvent(new Event("input", { bubbles: true })); // 刷新预览，并让保存条感知到改动
      }
      motdIn.oninput = () => renderMotd(prev, motdIn.value);
      renderMotd(prev, motdIn.value);
    }
    const readProps = () => Object.fromEntries([...body.querySelectorAll("[data-k]")].map(el =>
      [el.dataset.k, el.type === "checkbox" ? (el.checked ? "true" : "false") : el.value]));
    wireSaveBar(body, gen, readProps, async () => {
      const pairs = [];
      body.querySelectorAll("[data-k]").forEach(el => {
        let val;
        if (el.type === "checkbox") val = el.checked ? "true" : "false";
        else val = el.value;
        if (el.type === "number" || el.type === "text" || el.tagName === "SELECT") {
          if (val === "" && !(el.dataset.k in cur)) return; // 未设置且为空的跳过
        }
        pairs.push({ key: el.dataset.k, value: String(val) });
      });
      await api(`/api/instances/${encodeURIComponent(name)}/properties`, { method: "PUT", body: { pairs } });
      toast("已保存 server.properties" + (data.running ? "（重启后生效）" : ""));
    });

  } else if (tab === "raw") {
    let data;
    try { data = await api(`/api/instances/${encodeURIComponent(name)}/properties`); } catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    if (!data.pairs.length) { body.innerHTML = `<div class="sub">配置文件为空 —— 首次启动服务器后，服务端会自动生成全部配置项。</div>`; return; }
    const LABELS = Object.fromEntries((window.PROPS_CATALOG || []).map(p => [p.key, p.label]));
    const total = data.pairs.length;
    body.innerHTML = `<div style="max-width:900px"><div class="sub">server.properties 全部键值（高级）。已知项附中文名。</div>
      <div class="common-bar">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="raw-search" placeholder="搜索配置项（键名 / 中文名，如 rcon / 视距）" aria-label="搜索配置项" style="width:320px"></div>
        <span class="hint" id="raw-count" style="margin:0">共 ${total} 项</span>
      </div>
      <table class="raw">${data.pairs.map(p =>
        `<tr data-search="${esc(p.key)} ${esc(LABELS[p.key] || "")}"><td>${esc(p.key)}</td><td class="raw-cn">${LABELS[p.key] ? esc(LABELS[p.key]) : ""}</td><td><input type="text" data-k="${esc(p.key)}" value="${esc(p.value)}" aria-label="${esc(p.key)}"></td></tr>`).join("")}</table>
      <div class="sub" id="raw-none" hidden>没有匹配的配置项：换个关键词，或者按 Esc 清空搜索。</div>
      ${saveBar("保存全部", data.running ? `<span class="warn-text">${icon("warn")} 运行中：保存后需重启生效</span>` : "")}</div>`;
    // 搜索（同常用设置）：只是把不匹配的行藏起来，保存时照样提交全部键值
    wireSearch($("#raw-search"), () => {
      const q = $("#raw-search").value, n = filterBySearch(q, body.querySelectorAll("table.raw tr"));
      $("#raw-count").textContent = searchKey(q) ? `找到 ${n} 项（共 ${total} 项）` : `共 ${total} 项`;
      $("#raw-none").hidden = n > 0;
    });
    wireSaveBar(body, gen,
      () => Object.fromEntries([...body.querySelectorAll("input[data-k]")].map(el => [el.dataset.k, el.value])),
      async () => {
        const pairs = [...body.querySelectorAll("input[data-k]")].map(el => ({ key: el.dataset.k, value: el.value }));
        await api(`/api/instances/${encodeURIComponent(name)}/properties`, { method: "PUT", body: { pairs } });
        toast("已保存 server.properties" + (data.running ? "（重启后生效）" : ""));
      });

  } else if (tab === "cart" && window.MDModCart) { // 自选模组 + 购物车：整块在 modcart.js
    await MDModCart.render(body, info, gen);

  } else if (tab === "music" && window.MDMusic) { // 音乐共享（第五阶段）：整块在 music.js
    await MDMusic.render(body, info, gen, refreshHead);

  } else if (tab === "gameai" && window.MDGameChat) { // 游戏内 AI（第四阶段）：整块在 gamechat.js
    await MDGameChat.render(body, info, gen);

  } else if (tab === "companion" && window.MDCompanion) { // AI 伙伴（第七阶段）：整块在 companion.js
    await MDCompanion.render(body, info, gen);

  } else if (tab === "jvm") {
    const appInfo = await api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096 }));
    if (stale(gen)) return;
    const maxMem = Math.max(2048, appInfo.ramMb - 2048);
    const aikarsFlags = xmxMb => {
      const big = xmxMb >= 12288;
      return ["-XX:+UseG1GC", "-XX:+ParallelRefProcEnabled", "-XX:MaxGCPauseMillis=200",
        "-XX:+UnlockExperimentalVMOptions", "-XX:+DisableExplicitGC", "-XX:+AlwaysPreTouch",
        big ? "-XX:G1NewSizePercent=40" : "-XX:G1NewSizePercent=30",
        big ? "-XX:G1MaxNewSizePercent=50" : "-XX:G1MaxNewSizePercent=40",
        big ? "-XX:G1HeapRegionSize=16M" : "-XX:G1HeapRegionSize=8M",
        big ? "-XX:G1ReservePercent=15" : "-XX:G1ReservePercent=20",
        "-XX:G1HeapWastePercent=5", "-XX:G1MixedGCCountTarget=4",
        big ? "-XX:InitiatingHeapOccupancyPercent=20" : "-XX:InitiatingHeapOccupancyPercent=15",
        "-XX:G1MixedGCLiveThresholdPercent=90", "-XX:G1RSetUpdatingPauseTimePercent=5",
        "-XX:SurvivorRatio=32", "-XX:+PerfDisableSharedMem", "-XX:MaxTenuringThreshold=1",
        "-Dusing.aikars.flags=https://mcflags.emc.gs", "-Daikars.new.flags=true"];
    };
    body.innerHTML = `
      <div style="max-width:640px">
        <div class="field" id="jm-mem-mount"></div>
        <label class="field"><span>自定义 JVM 参数。每行一个，或用空格分隔。-Xmx 和 -Xms 由上面的内存自动生成，不用在这里填。</span>
          <textarea id="jm-jvm" rows="5" spellcheck="false" placeholder="如：-XX:+UseG1GC">${esc((info.extraJvm || []).join("\n"))}</textarea></label>
        <div class="row" style="margin-bottom:12px;flex-wrap:wrap">
          <button class="btn sm" id="jm-aikar">${icon("bolt")} 套用 Aikar's Flags（G1GC 优化）</button>
          <button class="btn sm" id="jm-clear">清空</button>
        </div>
        <label class="field" id="jm-java-box"><span>Java</span>
          <div class="row" style="gap:8px">
            <select id="jm-java" style="flex:1;min-width:0" aria-label="这台服务器用的 Java" aria-describedby="jm-java-hint"><option>加载中…</option></select>
            <button type="button" class="btn" id="jm-java-set" disabled>更换 Java</button>
          </div>
          <div class="hint" id="jm-java-hint">新建时已经按 Minecraft 版本选好了（1.20.1 是 Java 17，1.21 是 21，26.1 起是 25），一般不用动。整合包说明里要求别的版本时在这里换，换了下次启动生效；起不来就换回原来的。</div>
        </label>
        <div class="hint" style="margin-bottom:14px">服务器目录：${esc(info.dir)}<br>要自己启动：双击这个目录里的 run.bat（和这里的设置是同一份）</div>
        ${saveBar("保存", info.status === "stopped" ? "" : `<span class="warn-text">${icon("warn")} 运行中：保存后需重启生效</span>`)}
      </div>`;
    // 换 Java：列出 MineDeck 下载的、本机装的，再加上本机还没有、可以现下一份的版本。和上面的保存条无关，点「更换」立即生效
    (async () => {
      const javas = await api("/api/javas").catch(() => ({ portable: [], scanned: [] }));
      if (stale(gen) || !$("#jm-java")) return;
      const norm = p => (p || "").toLowerCase().replace(/\//g, "\\");
      const opts = [
        ...javas.portable.map(j => ({ major: j.major, path: j.path || "", text: `Java ${j.major}（MineDeck 下载的）`, portable: true })),
        ...javas.scanned.map(j => ({ major: j.major, path: j.path, text: `Java ${j.major}（本机装的 ${j.version}：${j.path}）` })),
      ];
      // 现在用的这一个：按路径认；/api/javas 里没有它（比如那个 Java 后来被卸载了）就单列一项
      let cur = opts.findIndex(o => info.javaPath && norm(o.path) === norm(info.javaPath));
      if (cur < 0) cur = opts.findIndex(o => !o.path && o.portable && o.major === info.javaMajor && info.javaSource === "portable");
      if (cur < 0) { opts.unshift({ major: info.javaMajor, path: info.javaPath || "", text: `Java ${info.javaMajor}${info.javaPath ? "（" + info.javaPath + "）" : ""}` }); cur = 0; }
      const have = new Set(opts.map(o => o.major));
      JAVA_DOWNLOADABLE.filter(m => !have.has(m)).forEach(m => opts.push({ major: m, path: "", text: `Java ${m}（本机没有，下载一份，约 50 MB）`, download: true }));
      const sel = $("#jm-java"), btn = $("#jm-java-set");
      sel.innerHTML = opts.map((o, k) => `<option value="${k}"${k === cur ? " selected" : ""}>${esc(o.text)}${k === cur ? " · 现在用的" : ""}</option>`).join("");
      sel.onchange = () => { btn.disabled = +sel.value === cur; };
      btn.onclick = async () => {
        const o = opts[+sel.value];
        if (!o || +sel.value === cur) return;
        btn.disabled = true;
        try {
          const r = await api(`/api/instances/${encodeURIComponent(name)}/java`, { method: "PUT", body: { major: o.major, path: o.download ? "" : o.path } });
          if (r && r.taskId) { location.hash = `#/task/${r.taskId}`; return; } // 要先下载：去任务页看进度
          toast(`已换成 Java ${o.major}` + (info.status === "stopped" ? "，下次启动生效" : "，重启服务器后生效"));
          info.javaMajor = o.major; info.javaPath = (r && r.java) || o.path; info.javaSource = o.portable ? "portable" : "local";
          cur = +sel.value;
          sel.querySelectorAll("option").forEach((el, k) => { el.textContent = opts[k].text + (k === cur ? " · 现在用的" : ""); });
          if ($("#d-java")) $("#d-java").textContent = "Java " + o.major; // 页头的徽标跟着换
        } catch (e) { toast(e.message, true); btn.disabled = false; }
      };
    })();
    renderMemoryControl($("#jm-mem-mount"), { sliderId: "jm-mem", min: 1024, max: maxMem, value: Math.min(info.xmxMb, maxMem), totalMb: appInfo.ramMb, availMb: appInfo.availRamMb });
    // 老版本创建时加入的 log4shell 缓解参数（后端保存时也会强制补回）：套用预设 / 清空时原样保留
    const LOG4J_FIX = ["-Dlog4j2.formatMsgNoLookups=true", "-Dlog4j.configurationFile=log4j2_112-116.xml"];
    const jvmFlags = () => $("#jm-jvm").value.split(/\s+/).map(s => s.trim()).filter(Boolean);
    const keptFix = () => { const cur = jvmFlags(); return LOG4J_FIX.filter(f => cur.includes(f)); };
    const paintBar = wireSaveBar(body, gen,
      () => ({ xmx: $("#jm-mem").value, jvm: jvmFlags().join(" ") }), // 只比较参数本身，换行 / 空格差异不算修改
      async () => {
        const flags = jvmFlags();
        await api(`/api/instances/${encodeURIComponent(name)}/settings`, { method: "PUT", body: { xmxMb: +$("#jm-mem").value, xmsMb: 0, extraJvm: flags } });
        // 回显实际保存的参数（老版本被删掉的 log4shell 缓解参数会被后端补回）；回读失败不影响已成功的保存。
        // 回显在 wireSaveBar 重取基线之前完成，补回的参数不会被算成「未保存的修改」
        const saved = await api("/api/instances").then(l => l.find(i => i.name === name)?.extraJvm).catch(() => null);
        if (saved && !stale(gen)) $("#jm-jvm").value = saved.join("\n");
        toast("已保存内存与 JVM 参数" + (saved?.some(a => !flags.includes(a)) ? "（已自动补回 log4shell 漏洞缓解参数）" : ""));
      });
    $("#jm-aikar").onclick = () => {
      const fix = keptFix();
      $("#jm-jvm").value = [...aikarsFlags(+$("#jm-mem").value), ...fix].join("\n");
      paintBar();
      toast(`已填入 Aikar's Flags${fix.length ? "（保留 log4shell 漏洞缓解参数）" : ""}，保存后生效`);
    };
    $("#jm-clear").onclick = () => {
      const fix = keptFix();
      $("#jm-jvm").value = fix.join("\n");
      paintBar();
      if (fix.length) toast("已清空（保留 log4shell 漏洞缓解参数）");
    };
  }
}

/* ---------- 视图：联机穿透 ---------- */
async function renderTunnels() {
  const gen = viewGen;
  let insts = [];
  try { insts = await api("/api/instances"); } catch {}
  if (stale(gen)) return;
  Main().innerHTML = pageHead("内网穿透",
    "把这台电脑上的服务器映射到公网，不在同一个网络的朋友也能连。可以用 OpenFrp、樱花穿透（SakuraFrp），也可以填自己的 frps。",
    `<button class="btn primary" id="tn-toggle" aria-expanded="false" aria-controls="tn-form">${icon("plus")} 添加隧道</button>`, "", "", "tun-summary") + `
    <div id="tun-list">${skeletonRows(4)}</div>
    <div id="tn-form" class="card" hidden style="margin-top:16px;max-width:var(--w-read)">
    <h3 style="margin-bottom:8px">添加隧道</h3>
    <div class="hint" style="margin-bottom:14px;line-height:1.8">
      <b>OpenFrp</b>：到 <a href="https://console.openfrp.net" target="_blank">OpenFrp 控制台</a> 创建 TCP 隧道，本地端口填这台服务器的端口。再到「个人中心」复制用户密钥。<br>
      <b>樱花frp</b>：到 <a href="https://www.natfrp.com" target="_blank">SakuraFrp</a> 创建隧道。复制访问密钥和隧道 ID。第一次要按提示把 frpc 放好。<br>
      <b>自定义</b>：填自己的 frps 地址、端口和 token。
    </div>
    <div class="form-grid">
      <label class="field"><span>服务商</span><select id="tn-provider">
        <option value="openfrp">OpenFrp（免费公益）</option>
        <option value="natfrp">樱花frp SakuraFrp</option>
        <option value="custom">自定义 frps</option></select></label>
      <label class="field"><span>隧道名称</span><input type="text" id="tn-name" value="我的隧道" maxlength="30"></label>
      <label class="field" data-f="cred"><span id="tn-cred-label">用户密钥（个人中心复制）</span><input type="password" id="tn-cred" placeholder="仅保存在本机"></label>
      <label class="field" data-f="proxy"><span>隧道 ID</span><input type="text" id="tn-proxy" placeholder="如 123456"></label>
      <label class="field" data-f="saddr" hidden><span>frps 服务器地址</span><input type="text" id="tn-saddr" placeholder="1.2.3.4 或 frp.example.com"></label>
      <label class="field" data-f="sport" hidden><span>frps 端口</span><input type="number" id="tn-sport" value="7000"></label>
      <label class="field" data-f="rport" hidden><span>公网远程端口</span><input type="number" id="tn-rport" value="25566"></label>
      <label class="field" data-f="lport" hidden><span>本地端口（MC 服务器端口）</span><input type="number" id="tn-lport" value="25565"></label>
      <label class="field"><span>绑定实例</span><select id="tn-bound"><option value="">不绑定</option>
        ${insts.map(i => `<option value="${esc(i.name)}">${esc(i.name)}（端口 ${i.port}）</option>`).join("")}</select></label>
      <div class="field"><label class="switch"><input type="checkbox" id="tn-auto" checked><span class="sw"></span>
        <span><span class="sw-label">跟随实例启动</span><div class="sw-desc">绑定的服务器启动时自动拉起隧道，停服后跟着停</div></span></label></div>
    </div>
    <button class="btn primary" id="tn-add" aria-label="确认添加隧道">${icon("plus")} 添加隧道</button>
    </div>`;

  const PROV_NAMES = { openfrp: "OpenFrp", natfrp: "樱花frp", custom: "自定义" };
  const provSel = $("#tn-provider");
  const applyFields = () => {
    const p = provSel.value;
    const show = { cred: true, proxy: p !== "custom", saddr: p === "custom", sport: p === "custom", rport: p === "custom", lport: p === "custom" };
    document.querySelectorAll("[data-f]").forEach(el => el.hidden = !show[el.dataset.f]);
    $("#tn-cred-label").textContent = { openfrp: "用户密钥（个人中心复制）", natfrp: "访问密钥", custom: "auth token（frps 未设可留空）" }[p];
  };
  provSel.onchange = applyFields;
  applyFields();
  if (window.MDMusic) MDMusic.tunnelForm(insts, provSel); // 第五阶段：用途（游戏 / 语音）、自定义 frps 的协议
  $("#tn-toggle").onclick = () => {
    const f = $("#tn-form");
    f.hidden = !f.hidden;
    $("#tn-toggle").setAttribute("aria-expanded", String(!f.hidden));
    if (!f.hidden) { f.scrollIntoView({ behavior: "smooth", block: "nearest" }); $("#tn-name").focus({ preventScroll: true }); }
  };

  // 日志展开着的隧道：列表照常刷新（以前日志一展开整个列表就停刷，点了「启动」卡片也不变，测试报告第五轮），
  // 重绘时把那个日志框摘下来再放回原位，SSE 不断。
  let logId = null;
  let shown = null; // 上次画出来的列表；没变就不重画，不打断「已复制」的动效和键盘焦点
  let drawSeq = 0;
  const closeLog = () => { logId = null; if (consoleES) { consoleES.close(); consoleES = null; } };
  const warnSpan = (text, title) => `<span class="jp" style="color:var(--danger-text);display:inline-flex;align-items:center;gap:5px"${title ? ` title="${esc(title)}"` : ""}>${icon("warn")} ${esc(text)}</span>`;
  const tunRow = t => {
    // 最近一次失败原因（启动失败、跟随启动失败、frpc 报错或意外退出）直接写在卡片上，不用点开日志；
    // 停着时排在上次的公网地址前面，跑起来拿到地址后不再显示（后端在 frpc 起来时也会清掉）。
    // 公网地址跨次保存着，「这次连上了没有」看 online：新一次运行还没连上就报错，不能让上次的地址盖住这次的错（Codex 复审发现）
    const err = !t.credBroken && t.lastError && !(t.running && t.online) ? t.lastError : "";
    const addr = t.publicAddr && (!t.running || t.online) ? t.publicAddr : ""; // 运行中没连上时不拿上次的地址充数
    const state = t.credBroken ? warnSpan("凭据无法解开，请删除后重新添加", "凭据加密绑定当前电脑和 Windows 用户，换了就解不开")
      : err ? warnSpan(t.running ? "连接出错" : "上次出错")
      : addr ? `<span class="jv" style="color:var(--accent-text);font-family:var(--mono)">${esc(addr)}</span>
          <button class="btn sm" data-copy="${esc(addr)}" aria-label="复制 ${esc(t.name)} 的公网地址">${icon("copy")} 复制</button>`
      : `<span class="jp">${t.running ? "连接中…" : "尚未启动"}</span>`;
    return `
      <div class="java-row" style="max-width:980px" data-tun="${esc(t.id)}">
        ${stPill(t.running ? "running" : "stopped")}
        <b style="min-width:110px">${esc(t.name)}</b>
        <span class="badge core">${esc(PROV_NAMES[t.provider] || t.provider)}</span>${window.MDMusic ? MDMusic.tunnelBadge(t) : ""}
        ${t.boundInstance ? `<span class="badge">${icon("link")} ${esc(t.boundInstance)}${t.autoStart ? " · 跟随" : ""}</span>` : ""}
        ${state}
        <span class="grow"></span>
        ${t.running ? `<button class="btn sm" data-tstop="${esc(t.id)}" aria-label="停止 ${esc(t.name)}">${icon("stop")} 停止</button>` : `<button class="btn sm primary" data-tstart="${esc(t.id)}" aria-label="启动 ${esc(t.name)}">${icon("play")} 启动</button>`}
        <button class="btn sm" data-tlog="${esc(t.id)}" aria-expanded="${logId === t.id}" aria-controls="tlog-${esc(t.id)}" aria-label="日志 ${esc(t.name)}">日志</button>
        <button class="btn sm danger" data-tdel="${esc(t.id)}" aria-label="删除 ${esc(t.name)}">删除</button>
        ${err ? `<div class="tun-err" style="flex-basis:100%;font-size:var(--fs-xs);color:var(--danger-text);line-height:1.6;white-space:pre-wrap;overflow-wrap:anywhere">${esc(err)}</div>` : ""}
      </div>
      <div class="task-log" id="tlog-${esc(t.id)}" style="display:none;height:150px;max-width:980px;margin:-2px 0 8px"></div>`;
  };
  const drawList = async () => {
    const seq = ++drawSeq;
    let list;
    try { list = await api("/api/tunnels"); } catch (e) {
      if (stale(gen) || seq !== drawSeq) return null;
      // 已经画着列表时偶尔刷新失败（网络抖一下）：列表和展开着的日志留着，摘要那里提示一句，下次刷新成功照常重画（独立评审发现）
      if (shown !== null && $("#tun-list [data-tun]")) { shown = null; $("#tun-summary").innerHTML = `<span style="color:var(--danger-text)">${icon("warn")} 刷新失败：${esc(e.message)}</span>`; return null; }
      closeLog(); shown = null; $("#tun-list").innerHTML = `<div class="err-box">${esc(e.message)}</div>`;
      return null;
    }
    if (stale(gen) || seq !== drawSeq) return list; // 晚到的旧响应不画
    const sig = JSON.stringify(list);
    if (sig === shown) return list;
    shown = sig;
    const runN = list.filter(t => t.running).length;
    $("#tun-summary").innerHTML = list.length ? `${list.length} 条隧道${SEP}运行中 ${runN}` : "";
    if (!list.length) { closeLog(); $("#tun-list").innerHTML = `<div class="empty">${MDPixel.emptySVG(200)}<div class="empty-title">还没有隧道</div>点右上角「添加隧道」，开服后就能把本地服务器映射到公网</div>`; return list; }
    const logBox = logId ? $("#tlog-" + logId) : null;
    $("#tun-list").innerHTML = list.map(tunRow).join("");
    if (logBox) { const ph = $("#tlog-" + logId); if (ph) ph.replaceWith(logBox); else closeLog(); } // 那条隧道删了就关掉日志
    $("#tun-list").querySelectorAll("[data-copy]").forEach(b => b.onclick = async () => {
      await copy(b.dataset.copy, "已复制公网地址：" + b.dataset.copy);
      // 微交互：图标换成弹出的对勾、文字改「已复制」，1.3 秒后换回
      if (b.dataset.busy) return;
      b.dataset.busy = "1";
      const old = b.innerHTML;
      b.innerHTML = `<span class="pop" style="display:inline-grid">${icon("check")}</span> 已复制`;
      setTimeout(() => { b.innerHTML = old; playClass(b, "swap-in"); delete b.dataset.busy; }, 1300);
    });
    $("#tun-list").querySelectorAll("[data-tstart]").forEach(b => b.onclick = async () => {
      const id = b.dataset.tstart;
      b.disabled = true;
      try { await api(`/api/tunnels/${id}/start`, { method: "POST" }); toast("隧道启动中，公网地址稍后显示在列表"); }
      catch (e) { toast(e.message, true); }
      b.disabled = false; // 列表没变（比如同样的错误）时不会重画，按钮要自己恢复
      watch(id, t => !t.running || !!t.online || !!t.lastError); // 这次连上（拿到公网地址）、报错或者停下就算稳定
    });
    $("#tun-list").querySelectorAll("[data-tstop]").forEach(b => b.onclick = async () => {
      const id = b.dataset.tstop;
      b.disabled = true;
      try { await api(`/api/tunnels/${id}/stop`, { method: "POST" }); } catch (e) { toast(e.message, true); }
      b.disabled = false;
      watch(id, t => !t.running);
    });
    $("#tun-list").querySelectorAll("[data-tdel]").forEach(b => b.onclick = async () => {
      const r = await confirmModal({ title: "删除隧道？", danger: true, okText: "删除", icon: "trash", body: "仅删除本机配置，服务商侧隧道不受影响。" });
      if (!r) return;
      try { await api(`/api/tunnels/${b.dataset.tdel}`, { method: "DELETE" }); } catch (e) { toast(e.message, true); }
      drawList();
    });
    $("#tun-list").querySelectorAll("[data-tlog]").forEach(b => b.onclick = () => {
      const id = b.dataset.tlog, box = $("#tlog-" + id), was = logId === id;
      document.querySelectorAll(".task-log[id^=tlog-]").forEach(x => x.style.display = "none");
      $("#tun-list").querySelectorAll("[data-tlog]").forEach(x => x.setAttribute("aria-expanded", "false"));
      closeLog();
      if (was) return;
      logId = id;
      b.setAttribute("aria-expanded", "true");
      box.style.display = "";
      box.textContent = "";
      consoleES = new EventSource(`/api/tunnels/${id}/console?token=${TOKEN()}`);
      consoleES.onmessage = e => {
        box.textContent += JSON.parse(e.data) + "\n";
        box.scrollTop = box.scrollHeight;
      };
    });
    return list;
  };
  // 启动、停止之后短间隔刷新：马上刷一次，之后每秒一次，直到这条隧道稳定下来（最多 30 秒）；平时 4 秒一次
  const pend = new Map(); // 隧道ID → { settled: 判断稳定了没有, until: 截止时间 }
  let burstT = null;
  const burst = async () => {
    burstT = null;
    if (stale(gen)) return;
    const list = await drawList();
    if (stale(gen)) return;
    const now = Date.now();
    for (const [id, w] of pend) {
      const t = list && list.find(x => x.id === id);
      if (now > w.until || (list && (!t || w.settled(t)))) pend.delete(id);
    }
    if (pend.size && !burstT) burstT = setTimeout(burst, 1000);
  };
  const watch = (id, settled) => {
    pend.set(id, { settled, until: Date.now() + 30000 });
    if (burstT) clearTimeout(burstT);
    burstT = setTimeout(burst, 0);
  };
  await drawList();
  startPoll(drawList, 4000, gen);

  $("#tn-add").onclick = async () => {
    const p = provSel.value, custom = p === "custom";
    // frps 地址和端口只有自定义 frps 用：樱花、OpenFrp 的端口在服务商后台填，以前把隐藏着的默认值（25565 / 25566 / 7000）
    // 也发过去存进了 tunnels.json，看着像真的端口（测试报告第五轮）
    const body = {
      provider: p,
      name: $("#tn-name").value.trim(),
      credential: $("#tn-cred").value.trim(),
      proxyId: $("#tn-proxy").value.trim(),
      serverAddr: custom ? $("#tn-saddr").value.trim() : "",
      serverPort: custom ? +$("#tn-sport").value || 0 : 0,
      remotePort: custom ? +$("#tn-rport").value || 0 : 0,
      localPort: custom ? +$("#tn-lport").value || 25565 : 0,
      boundInstance: $("#tn-bound").value,
      autoStart: $("#tn-auto").checked,
      ...(window.MDMusic ? MDMusic.tunnelExtra() : {}), // 第五阶段：purpose、proto
    };
    try {
      await api("/api/tunnels", { method: "POST", body });
      toast("隧道已添加");
      // 收起表单并清空密钥类字段，回到列表看新隧道
      $("#tn-form").hidden = true;
      $("#tn-toggle").setAttribute("aria-expanded", "false");
      ["#tn-cred", "#tn-proxy", "#tn-saddr"].forEach(s => { $(s).value = ""; });
      drawList();
    } catch (e) { toast(e.message, true); }
  };
}

/* ---------- 视图：设置 ----------
   左侧分区导航（当前项是凸起键帽，切分区时键帽滑动）+ 右侧 iOS 式分组列表：一组一张卡，
   行与行之间是缩进的发丝线；行首可带强调色图标瓦片，右侧放开关 / 按钮 / 摘要。 */
const SETTINGS_SECTIONS = [
  { id: "appearance", label: "外观", icon: "palette" },
  { id: "download", label: "下载与网络", icon: "globe" },
  { id: "storage", label: "存储位置", icon: "folder" },
  { id: "java", label: "Java 运行时", icon: "coffee" },
  { id: "remote", label: "远程访问", icon: "phone" },
  { id: "notify", label: "通知与集成", icon: "bell" },
  { id: "ai", label: "AI（可选）", icon: "sparkle" },
  { id: "startup", label: "启动与托盘", icon: "monitor" },
  { id: "about", label: "更新与关于", icon: "info" },
];

async function renderSettings(section) {
  const gen = viewGen;
  if (!SETTINGS_SECTIONS.some(s => s.id === section)) section = "appearance";
  // 壳（标题 + 左导航）常驻：切分区只换右侧 body 并播局部入场动效，不整页重建、导航不闪不跳。
  // #set-body 仅存在于设置视图内，可据此判断壳是否已挂载。
  // 不套独立居中容器（旧 .settings-page 令标题与其他页错位、导航随内容横跳），直接落在全站内容列上
  if (!$("#set-body")) {
    Main().innerHTML = pageHead("设置", "", "", "", "", "set-eyebrow") + `
      <div class="settings-layout">
        <nav class="settings-nav" aria-label="设置分区">${SETTINGS_SECTIONS.map(s => `
          <a class="set-nav" data-sec="${s.id}" href="#/settings/${s.id}"><span class="sn-ico">${icon(s.icon)}</span>${s.label}</a>`).join("")}</nav>
        <div class="settings-body" id="set-body"><div class="sub">加载中…</div></div>
      </div>`;
  }
  $("#set-eyebrow").innerHTML = `设置<span class="sep">/</span>${esc(SETTINGS_SECTIONS.find(s => s.id === section).label)}`;
  document.querySelectorAll(".set-nav").forEach(a => {
    const on = a.dataset.sec === section;
    a.classList.toggle("cur", on);
    if (on) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current");
  });
  if (samePage) slideKeys(keysBefore, ["set"]); // 换分区：导航键帽从旧分区滑到新分区
  const body = $("#set-body");
  let info;
  try { info = await api("/api/app"); } catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
  if (stale(gen)) return; // 快速连点分区时，先发后至的旧响应不得覆盖当前分区
  ({ appearance: setAppearance, download: setDownload, storage: setStorage, java: setJava, remote: setRemote, notify: setNotify, ai: setAI, startup: setStartup, about: setAbout }[section])(body, info);
  playClass(body, "sec-enter"); // 只动被替换的分区内容（上移 4px 淡入）
}

/* 分区标题；sub 允许富文本（调用方自负安全，标题走 esc） */
function settingsHead(t, sub) {
  return `<div class="set-sec-head"><h2>${esc(t)}</h2>${sub ? `<div class="sub">${sub}</div>` : ""}</div>`;
}
/* 分组：一组一张卡；ico = 行首带图标瓦片（发丝线随之缩进到文字起点） */
const group = (rows, { ico = false, cls = "", attrs = "" } = {}) =>
  `<div class="group${ico ? " has-ico" : ""}${cls ? " " + cls : ""}"${attrs ? " " + attrs : ""}>${rows}</div>`;
/* 分组里的一行：可选图标瓦片 + 标题 / 说明（sub 允许富文本）/ 键名或路径 + 右侧控件；after 放在行内最下面（如字段的报错） */
function riRow({ ico = "", title = "", titleHTML = "", sub = "", key = "", right = "", cls = "", attrs = "", field = "", after = "" }) {
  return `<div class="row-item${cls ? " " + cls : ""}"${attrs ? " " + attrs : ""}><div class="ri-head">${ico ? `<span class="ri-ico">${icon(ico)}</span>` : ""}
    <div class="ri-main"><div class="ri-title">${titleHTML || esc(title)}</div>${sub ? `<div class="ri-sub">${sub}</div>` : ""}${key ? `<div class="ri-key">${esc(key)}</div>` : ""}</div>
    ${right ? `<div class="ri-right">${right}</div>` : ""}</div>${field ? `<div class="ri-field">${field}</div>` : ""}${after}</div>`;
}
/* 行式开关（标题 + 说明 + 右侧开关），data-cfg 携带配置键 */
const switchRow = (ico, title, desc, key, on) => riRow({ ico, title, sub: esc(desc),
  right: `<label class="switch" title="${esc(title)}"><input type="checkbox" data-cfg="${key}" ${on ? "checked" : ""} aria-label="${esc(title)}"><span class="sw"></span></label>` });

/* — 外观 — 模式 / 配色方案 / 强调色，点选即时生效（只存在本机）。
   方案卡的预览区带上该方案的 data-scheme/data-theme/data-accent，直接用 themes.css 的令牌渲染缩略界面。 */
function setAppearance(body) {
  let refocus = null; // 重绘后把焦点还给刚点的那一项（键盘操作不丢位置）
  const draw = () => {
    const a = Appearance.get();
    const theme = document.documentElement.dataset.theme;
    body.innerHTML = settingsHead("主题模式", "「跟随系统」会随 Windows 的浅色 / 深色设置自动切换。") +
      `<div class="seg" role="radiogroup" aria-label="主题模式">${["auto", "light", "dark"].map(m =>
        `<button type="button" role="radio" aria-checked="${a.mode === m}" class="${a.mode === m ? "on" : ""}" data-mode="${m}">${icon(MODE_ICON[m])}${MODE_LABEL[m]}</button>`).join("")}</div>` +
      settingsHead("配色方案", "点一下，整个界面都会换。") +
      `<div class="scheme-grid">${SCHEMES.map(s => `
        <button type="button" class="scheme-card ${a.scheme === s.id ? "on" : ""}" data-scheme-pick="${s.id}" aria-pressed="${a.scheme === s.id}">
          <div class="sc-preview" data-scheme="${s.id}" data-theme="${theme}" data-accent="${a.accent}" aria-hidden="true">
            <div class="sc-side"><i></i><i></i><i></i></div>
            <div class="sc-main"><div class="sc-card"><i></i><i></i><b></b></div></div>
          </div>
          <div class="sc-meta"><div><div class="sc-name">${esc(s.name)}</div><div class="sc-desc">${esc(s.desc)}</div></div>${a.scheme === s.id ? icon("check") : ""}</div>
        </button>`).join("")}</div>` +
      settingsHead("强调色", "按钮、选中项与链接使用的颜色；默认跟随配色方案。") +
      `<div class="accent-row" role="radiogroup" aria-label="强调色">
        <button type="button" role="radio" aria-checked="${a.accent === "auto"}" class="acc-swatch acc-auto ${a.accent === "auto" ? "on" : ""}" data-accent-pick="auto">跟随方案</button>
        ${ACCENTS.map(c => `<button type="button" role="radio" aria-checked="${a.accent === c.id}" class="acc-swatch ${a.accent === c.id ? "on" : ""}" style="--sw:${c.sw}" data-accent-pick="${c.id}" title="${c.name}" aria-label="${c.name}">${a.accent === c.id ? icon("check") : ""}</button>`).join("")}
      </div>
      <div class="hint" style="margin-top:20px">想换背景图？在程序所在目录新建 <code>bg</code> 文件夹并放入图片，下次打开时随机选一张作为壁纸。</div>`;
    body.querySelectorAll("[data-mode]").forEach(b => b.onclick = () => {
      refocus = `[data-mode="${b.dataset.mode}"]`;
      const before = keyRects();
      Appearance.set({ mode: b.dataset.mode });
      slideKeys(before, ["seg"]); // 分段控件的键帽滑到新选项
    });
    body.querySelectorAll("[data-scheme-pick]").forEach(b => b.onclick = () => { refocus = `[data-scheme-pick="${b.dataset.schemePick}"]`; Appearance.set({ scheme: b.dataset.schemePick }); });
    body.querySelectorAll("[data-accent-pick]").forEach(b => b.onclick = () => { refocus = `[data-accent-pick="${b.dataset.accentPick}"]`; Appearance.set({ accent: b.dataset.accentPick }); });
    if (refocus) { body.querySelector(refocus)?.focus(); refocus = null; }
  };
  appearanceView = draw;
  draw();
}

/* — 启动与托盘 — */
function setStartup(body, info) {
  const c = info.config || {};
  body.innerHTML = settingsHead("启动与托盘", "这里管两件事：点窗口关闭按钮时做什么，以及开机要不要自己打开。程序开着的时候，托盘里有图标。左键点一下，恢复窗口。右键可以「打开面板」或「全部停止并退出」。程序退出后，图标会消失。") +
    group(
      switchRow("monitor", "最小化到系统托盘", "打开之后，点窗口的关闭按钮不会退出，会收到托盘里，服务器继续运行。点托盘图标可以恢复窗口。关掉这个开关时，点关闭按钮会退出程序，并停掉服务器。有服务器在运行时会先问你，也可以改成最小化到托盘。", "minimizeToTray", c.minimizeToTray) +
      switchRow("power", "开机自动启动", "登录 Windows 后，MineDeck 会在后台（托盘）自己打开。不用管理员权限。程序换了文件夹以后，把这个开关关一次再开，路径就会改过来。", "autoStart", info.autoStart),
      { ico: true }) +
    `<div class="hint">开机自启时直接进托盘，不先弹出窗口。上面的「最小化到系统托盘」也开着的话，之后点关闭按钮也会收回托盘，服务器不会停。</div>`;
  body.querySelectorAll("input[data-cfg]").forEach(inp => inp.onchange = async () => {
    const key = inp.dataset.cfg;
    try {
      await api("/api/config", { method: "PUT", body: { [key]: inp.checked } });
      if (key === "minimizeToTray") c.minimizeToTray = inp.checked; else info.autoStart = inp.checked;
      toast(inp.checked ? "已开启" : "已关闭");
    } catch (e) { toast(e.message, true); inp.checked = !inp.checked; }
  });
}

/* CurseForge API Key 只写不读（后端只告诉 cfKeySet）：已设置时输入框留空、占位写「已设置」，旁边给「清除」；
   存着的 Key 解不开（换了电脑或 Windows 用户）时提示重填，也可以直接清除 */
const cfKeySub = k => k.broken ? `<span class="warn-text">${icon("warn")} 密钥无法解开，可能换了电脑或 Windows 用户，请重新填写</span>`
  : "加密保存在这台电脑上。换电脑或换 Windows 用户后需要重填";
const cfKeyHolder = k => k.set ? "已设置（留空不修改）" : "粘贴 API Key";

/* — 下载与网络 — 下载源是一组单选行（选中行右侧打勾，点选即时生效）；API Key 是单字段保存 */
function setDownload(body, info) {
  const src = info.config.source || "auto";
  const cf = { set: !!info.cfKeySet, broken: !!info.cfKeyBroken };
  const opts = [
    { v: "auto", t: "自动（推荐）", d: "国内镜像（BMCLAPI / 清华）优先，失败自动切换官方源" },
    { v: "mirror", t: "仅国内镜像", d: "只用 BMCLAPI 和清华镜像。Paper 和 Purpur 没有国内镜像，这两项仍从官方下载。" },
    { v: "official", t: "仅官方源", d: "只用 Mojang / Forge / Adoptium 等官方源（海外网络适用）" },
  ];
  const check = on => `<span class="ri-check${on ? " on" : ""}" aria-hidden="true">${icon("check")}</span>`;
  body.innerHTML = settingsHead("下载源", "下载 Java 与服务端核心时的线路选择。") +
    group(opts.map(o => riRow({ title: o.t, sub: esc(o.d), right: check(src === o.v), cls: "choice" + (src === o.v ? " sel" : ""),
      attrs: `role="radio" tabindex="0" aria-checked="${src === o.v}" data-v="${o.v}"` })).join(""), { attrs: `id="src-cards" role="radiogroup" aria-label="下载源"` }) +
    settingsHead("CurseForge API Key（选填）", `导入 CurseForge 格式整合包、要下载里面的模组时用。<a href="https://console.curseforge.com/" target="_blank">免费申请</a>。Modrinth 格式不用填。`) +
    group(riRow({ ico: "package", title: "API Key", sub: `<span id="cf-sub">${cfKeySub(cf)}</span>`,
      field: `<input type="password" id="cf-key" autocomplete="off" placeholder="${cfKeyHolder(cf)}" aria-label="CurseForge API Key"><button class="btn" id="cf-save">保存</button><button class="btn ghost" id="cf-clear"${cf.set || cf.broken ? "" : " hidden"}>清除</button>` }), { ico: true });
  const pick = async c => {
    if (c.classList.contains("sel")) return;
    try {
      await api("/api/config", { method: "PUT", body: { source: c.dataset.v } });
      $("#src-cards").querySelectorAll(".row-item").forEach(x => {
        const on = x === c;
        x.classList.toggle("sel", on);
        x.setAttribute("aria-checked", String(on));
        x.querySelector(".ri-check").classList.toggle("on", on);
      });
      toast("下载源已切换");
    } catch (e) { toast(e.message, true); }
  };
  $("#src-cards").querySelectorAll(".row-item").forEach(c => {
    c.onclick = () => pick(c);
    c.onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); pick(c); } };
  });
  const paintCF = () => {
    $("#cf-key").placeholder = cfKeyHolder(cf);
    $("#cf-sub").innerHTML = cfKeySub(cf);
    $("#cf-clear").hidden = !(cf.set || cf.broken);
  };
  bindSaveInput($("#cf-key"), $("#cf-save"), async v => {
    const k = v.trim();
    if (k) {
      await api("/api/config", { method: "PUT", body: { cfApiKey: k } });
      Object.assign(cf, { set: true, broken: false });
      toast("CurseForge API Key 已保存");
    }
    $("#cf-key").value = ""; // 存好后清空输入框（只填了空白也一样，什么都不存；清除走「清除」按钮）
    paintCF();
  }, viewGen);
  $("#cf-clear").onclick = async () => {
    const r = await confirmModal({ title: "清除 CurseForge API Key？", body: "清除后，导入 CurseForge 格式的整合包时无法自动下载模组。需要时可以重新填写。", okText: "清除", danger: true, icon: "trash" });
    if (!r) return;
    try {
      await api("/api/config", { method: "PUT", body: { cfApiKey: "" } });
      Object.assign(cf, { set: false, broken: false });
      paintCF();
      toast("已清除 CurseForge API Key");
    } catch (e) { toast(e.message, true); }
  };
}

/* — 存储位置 — */
function storageRow(ico, title, path, desc, pick) { // 三行都有「打开」「更改…」：名字带上是哪个目录
  return riRow({ ico, title, sub: esc(desc), key: path || "", right: `
      <button class="btn sm" data-open="${esc(path || "")}" aria-label="打开${esc(title)}">${icon("folder")} 打开</button>
      ${pick ? `<button class="btn sm" data-pick="${pick}" aria-label="更改${esc(title)}…">更改…</button>
      <button class="btn sm ghost icon-only" data-reset="${pick}" title="恢复默认目录" aria-label="恢复默认的${esc(title)}">${icon("refresh")}</button>` : ""}` });
}
function setStorage(body, info) {
  const gen = viewGen;
  const draw = () => {
    if (stale(gen)) return;
    body.innerHTML = settingsHead("存储位置", "服务器和备份放在哪个文件夹。改了之后，只对新建的服务器和备份生效。已经有的不会自动搬过去。") +
      group(
        storageRow("server", "服务器存放目录", info.serversRoot, "新的服务器会建在这个目录里，一台一个子文件夹。中文名字不会直接用作文件夹名。", "servers") +
        storageRow("package", "备份存放目录", info.backupsRoot, "服务器开着时做的备份，放在这里。", "backups") +
        storageRow("folder", "程序数据目录", info.base, "程序、下载的 Java 和缓存都在这里。整个文件夹可以一起复制走。", ""),
        { ico: true }) +
      `<div class="hint">建议放到空间大的盘，比如 D 盘，免得占满系统盘。点「更改…」会打开系统的文件夹选择框。这只在这台电脑上能用。用手机远程管理时，请在这台电脑上改。</div>`;
    body.querySelectorAll("[data-open]").forEach(b => b.onclick = () =>
      api("/api/openpath", { method: "POST", body: { path: b.dataset.open } }).catch(e => toast(e.message, true)));
    body.querySelectorAll("[data-pick]").forEach(b => b.onclick = async () => {
      try {
        const r = await api("/api/pickdir", { method: "POST" });
        if (!r.path) return;
        const key = b.dataset.pick === "servers" ? "serversDir" : "backupsDir";
        await api("/api/config", { method: "PUT", body: { [key]: r.path } });
        if (b.dataset.pick === "servers") info.serversRoot = r.path; else info.backupsRoot = r.path;
        toast("已更新目录（对新建生效）");
        draw();
      } catch (e) { toast(e.message, true); }
    });
    body.querySelectorAll("[data-reset]").forEach(b => b.onclick = async () => {
      try {
        const key = b.dataset.reset === "servers" ? "serversDir" : "backupsDir";
        await api("/api/config", { method: "PUT", body: { [key]: "" } });
        const fresh = await api("/api/app");
        info.serversRoot = fresh.serversRoot; info.backupsRoot = fresh.backupsRoot;
        toast("已恢复默认目录");
        draw();
      } catch (e) { toast(e.message, true); }
    });
  };
  draw();
}

/* — Java 运行时 — */
async function setJava(body, info) {
  const gen = viewGen;
  const head = settingsHead("Java 运行时", "新建服务器时，先用这台电脑上已经装好的 Java。没有合适的，就用 MineDeck 下载过的。还没有，就再下载一个。");
  body.innerHTML = head + `<div class="sub">加载中…</div>`;
  let javas = { portable: [], scanned: [] };
  try { javas = await api("/api/javas"); } catch {}
  if (stale(gen)) return;
  body.innerHTML = head +
    group(
      riRow({ ico: "package", title: "便携运行时", sub: "MineDeck 自己下载的 Java，单独放着，不改你系统里已经装的 Java。",
        right: javas.portable.length ? `<span class="badges">${javas.portable.map(j => `<span class="badge core">Java ${j.major}</span>`).join("")}</span>` : `<span class="ri-summary">暂无</span>` }) +
      (javas.scanned.length ? javas.scanned.map(j => riRow({ ico: "coffee", titleHTML: `Java ${j.major} <span class="badge">${esc(j.version)}</span>`, key: j.path })).join("")
        : riRow({ ico: "coffee", title: "本机已安装的 Java", sub: "尚未发现，点下方「重新扫描」或手动添加" })),
      { ico: true }) +
    group(riRow({ ico: "plus", title: "手动添加", sub: "粘贴 java.exe 或 JDK 目录的路径",
      field: `<input type="text" id="java-path" placeholder="如 C:\\Program Files\\Java\\jdk-21\\bin\\java.exe" aria-label="Java 路径"><button class="btn" id="java-add">添加</button>` }), { ico: true }) +
    `<div class="row" style="margin-top:12px"><button class="btn" id="java-scan">${icon("refresh")} 重新扫描本机 Java</button></div>`;
  $("#java-scan").onclick = async () => {
    $("#java-scan").disabled = true; $("#java-scan").textContent = "扫描中…（约数秒）";
    try { const list = await api("/api/javas/scan", { method: "POST" }); toast(`扫描完成，发现 ${list.length} 个 Java`); if (!stale(gen)) setJava(body, info); }
    catch (e) { toast(e.message, true); if (!stale(gen)) { $("#java-scan").disabled = false; $("#java-scan").innerHTML = icon("refresh") + " 重新扫描本机 Java"; } }
  };
  $("#java-add").onclick = async () => {
    const p = $("#java-path").value.trim(); if (!p) return;
    try { const j = await api("/api/javas/add", { method: "POST", body: { path: p } }); toast(`已添加 Java ${j.major}（${j.version}）`); if (!stale(gen)) setJava(body, info); }
    catch (e) { toast(e.message, true); }
  };
}

/* — 远程访问 — */
/* 访问地址排序：家用局域网段优先，让默认那张二维码就是手机最可能连得上的地址
   （172.16/12 多为 WSL / Hyper-V 虚拟网卡；169.254 是没拿到 DHCP 的自动地址，垫底） */
const lanIPRank = ip => /^192\.168\./.test(ip) ? 0 : /^10\./.test(ip) ? 1 : /^172\.(1[6-9]|2\d|3[01])\./.test(ip) ? 2 : /^169\.254\./.test(ip) ? 4 : 3;
function setRemote(body, info) {
  const lanPort = info.port || location.port || "27333"; // 实际监听端口（被占用时会随机化）
  const ips = (info.ips || []).slice().sort((a, b) => lanIPRank(a) - lanIPRank(b));
  // 开关点了即时保存（开启前必须先有访问密码，后端也会拒绝）。监听地址只在程序启动时绑定：
  // 开启要重启程序才生效，和这次启动不一致时常驻提示；关闭即时生效（后端不再放行局域网的请求）
  const on = !!info.config.listenLan;
  const live = info.lanActive ?? on; // 这次启动是否已监听在局域网上
  body.innerHTML = settingsHead("远程访问（手机 / 平板管理）", "开启后本机 IP 可从局域网访问管理界面（密码保护，开启后需重启程序生效）。") +
    group(
      riRow({ ico: "phone", title: "允许局域网访问", sub: "手机与本机连在同一 Wi-Fi / 路由器下即可打开管理界面",
        right: `<label class="switch"><input type="checkbox" id="lan-on" ${on ? "checked" : ""} aria-label="允许局域网访问"><span class="sw"></span></label>` }) +
      riRow({ ico: "lock", title: "访问密码", sub: info.lanSet ? "已设置；要修改就输入新密码后保存" : "开启局域网访问前必须先设置",
        field: `<input type="password" id="lan-pw" placeholder="${info.lanSet ? "已设置（输入新密码可修改）" : "设置访问密码（必填）"}" aria-label="访问密码"><button class="btn" id="lan-save">保存</button>`,
        after: `<div class="field-err ri-err" id="lan-pw-err"></div>` }),
      { ico: true }) +
    (on && !live ? `<div class="hint" id="lan-restart">${icon("info")} 局域网访问已开启。重启 MineDeck 后生效。重启之后，这里会显示手机扫码用的地址。</div>` : "") +
    (!(on && live) ? "" : ips.length ? `<div class="qr-card">
      <div class="qr-box" id="qr-box" role="img"></div>
      <div class="qr-info">
        <div class="ri-title">手机扫码访问</div>
        <div class="ri-sub">用手机相机或浏览器扫这个码。打开后输入访问密码就能管理。手机要和这台电脑连在同一个局域网（同一个 Wi-Fi 或路由器）。</div>
        <div class="qr-url" id="qr-url"></div>
        ${ips.length > 1 ? `<div class="qr-ips">${ips.map((ip, i) => `<button type="button" class="qr-ip" data-i="${i}">${esc(ip)}</button>`).join("")}</div>
        <div class="hint">本机有多个网络地址：扫码打不开就换一个（家用路由器下通常是 192.168.x.x）。</div>` : ""}
      </div>
    </div>` : `<div class="hint">未检测到本机局域网 IP，请确认已连接网络。</div>`) +
    `<div class="hint">还是打不开的话，用管理员身份运行一次下面这条命令，放行防火墙：<code>netsh advfirewall firewall add rule name="MineDeck" dir=in action=allow protocol=TCP localport=${esc(String(lanPort))}</code></div>`;
  const lanOn = $("#lan-on"), lanPw = $("#lan-pw");
  const pwErr = msg => { lanPw.classList.toggle("invalid", !!msg); $("#lan-pw-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  const effect = turnOn => turnOn ? (live ? "" : "，重启 MineDeck 后生效") : (live ? "，局域网里的设备现在起打不开管理界面" : "");
  // 保存要等一个来回：这期间切去了别的分区或页面，就不再把远程访问画回 body（它可能已经不是当前视图的了）；提示照常弹
  const gen = viewGen;
  const saved = b => { info.config.listenLan = b.listenLan; if (b.lanPassword) info.lanSet = true; if (!stale(gen)) setRemote(body, info); };
  let wantOn = false; // 没设密码时拨了开关：记下想开，填好密码点「保存」时一起开
  lanOn.onchange = async () => {
    const turnOn = lanOn.checked;
    const b = { listenLan: turnOn };
    if (turnOn && !info.lanSet) {
      if (!lanPw.value) { // 就地提示，开关拨回去
        lanOn.checked = false; wantOn = true;
        pwErr("先设置访问密码：填好后点「保存」，局域网访问会一起开启");
        lanPw.focus();
        return;
      }
      b.lanPassword = lanPw.value; // 密码框里已经填了：和开关一起保存
    } else if (lanPw.value) b.lanPassword = lanPw.value; // 已有密码时框里填了新的：也一起保存，不然重画清空了框，用户以为新密码生效了（独立评审发现）
    lanOn.disabled = true;
    try {
      await api("/api/config", { method: "PUT", body: b });
      saved(b);
      toast((turnOn ? "已开启局域网访问" : "已关闭局域网访问") + effect(turnOn) + (b.lanPassword && info.lanSet ? "。新的访问密码已保存" : ""));
    } catch (e) { lanOn.checked = !turnOn; lanOn.disabled = false; toast(e.message, true); }
  };
  // 保存密码：开关开着（或刚才因为没密码被拨回去）时一起开启，即「密码和开关一起保存」
  $("#lan-save").onclick = async () => {
    if (!lanPw.value) { pwErr(info.lanSet ? "输入新密码再保存" : "请先输入访问密码"); lanPw.focus(); return; }
    const b = { listenLan: lanOn.checked || wantOn, lanPassword: lanPw.value };
    const opening = b.listenLan && !on;
    try {
      await api("/api/config", { method: "PUT", body: b });
      saved(b);
      toast(opening ? "访问密码已保存，已开启局域网访问" + effect(true) : "访问密码已保存");
    } catch (e) { toast(e.message, true); }
  };
  lanPw.oninput = () => pwErr("");
  lanPw.onkeydown = e => { if (e.key === "Enter") $("#lan-save").click(); };
  if (!(on && live) || !ips.length) return;
  // 二维码纯前端生成（qrcode.js），多地址时点选切换
  const show = i => {
    const url = `http://${ips[i]}:${lanPort}`;
    $("#qr-box").innerHTML = QR.svg(url);
    $("#qr-box").setAttribute("aria-label", "访问地址二维码：" + url);
    $("#qr-url").textContent = url;
    body.querySelectorAll(".qr-ip").forEach(b => {
      const on = +b.dataset.i === i;
      b.classList.toggle("cur", on);
      b.setAttribute("aria-pressed", String(on));
    });
  };
  body.querySelectorAll(".qr-ip").forEach(b => b.onclick = () => show(+b.dataset.i));
  show(0);
}

/* — 通知与集成 — */
function setNotify(body, info) {
  body.innerHTML = settingsHead("通知与集成", "服务器启停 / 崩溃、玩家进出、备份完成、隧道上线等事件，可以推送到群机器人等外部服务。") +
    group(riRow({ ico: "bell", title: "Webhook 事件推送（选填）", sub: "有事件时，会向这个地址 POST 一条 JSON。留空就是关闭。",
      field: `<input type="text" id="wh-url" value="${esc(info.config.webhookUrl || "")}" placeholder="https://example.com/hook（留空关闭）" aria-label="Webhook 地址"><button class="btn" id="wh-save">保存</button>` }), { ico: true });
  bindSaveInput($("#wh-url"), $("#wh-save"), async v => {
    await api("/api/config", { method: "PUT", body: { webhookUrl: v.trim() } });
    toast(v.trim() ? "Webhook 已保存，即时生效" : "已关闭 Webhook 推送");
  }, viewGen);
}

/* — AI（可选）—— 设计文档 §4、§9
   没配置时只有这个分区，别处没有任何 AI 入口；没配置时后端不连接任何 AI 服务、也不下载价格数据。
   预设只是方便填写的模板：第一次打开时不预选、不推荐任何一家。
   服务商设置是多字段表单（吸底保存条）：Key 只写不读，已设置时输入框留空、占位写「已设置」；改了服务商、协议或地址，
   保存后旧 Key 自动清掉，要重新填写。每日额度是单字段保存（bindSaveInput）。局域网打开时只能看（修改仅本机）。 */
const AI_EFFORTS = [["", "默认（模型自己定）"], ["low", "低：最快、最省"], ["medium", "中"], ["high", "高"], ["xhigh", "很高"], ["max", "最高：最慢、最贵"]];
const AI_FEATS = [{ id: "assistant", name: "应用内助手" }, { id: "gamechat", name: "游戏内 AI" }, { id: "companion", name: "AI 伙伴" }]; // 应用内助手：本机和局域网登录的设备共用一份额度；AI 伙伴（第七阶段）：玩家叫着伙伴的名字说话时
const fmtTokens = n => { n = +n || 0; return n >= 10000 ? (Math.round(n / 1000) / 10).toString() + " 万" : n.toLocaleString("en-US"); };
const fmtMoney = (amount, cur) => (cur === "CNY" ? "¥" : "$") + (amount > 0 && amount < 0.01 ? amount.toFixed(4) : amount.toFixed(2));
/* 一段时间的费用合计：按币种分开（不做汇率换算），如「约 $0.12 + ¥0.30」；没有金额时为空 */
const fmtCost = cost => { const parts = ["USD", "CNY"].filter(c => cost && cost[c] > 0).map(c => fmtMoney(cost[c], c)); return parts.length ? "约 " + parts.join(" + ") : ""; };
function aiUsageLine(t, m) {
  t = t || {}; m = m || {};
  const cost = c => (fmtCost(c.cost) ? `（${fmtCost(c.cost)}）` : "");
  let line = `今天已用 ${fmtTokens(t.tokens)} token${cost(t)} · 本月 ${fmtTokens(m.tokens)} token${cost(m)}`;
  if (m.estimated) line += ` · 其中 ${m.estimated} 次按字数估算`;
  if (m.unpriced) line += ` · ${m.unpriced} 次查不到价格（只记 token）`;
  return esc(line);
}

async function setAI(body) {
  const gen = viewGen;
  body.innerHTML = settingsHead("AI 服务商（可选）", "") + `<div class="sub">加载中…</div>`;
  let v;
  try { v = await api("/api/ai/settings"); }
  catch (e) { if (!stale(gen)) body.innerHTML = settingsHead("AI 服务商（可选）", "") + `<div class="err-box">${esc(e.message)}</div>`; return; }
  if (stale(gen)) return;
  drawAI(body, v, gen);
}

function drawAI(body, v, gen) {
  const P = Object.fromEntries(v.presets.map(p => [p.id, p]));
  const saved = v.provider;
  const ro = !v.local; // 局域网打开：只能看
  const dis = ro ? " disabled" : "";
  const opt = (val, label, cur) => `<option value="${esc(val)}"${val === cur ? " selected" : ""}>${esc(label)}</option>`;
  body.innerHTML =
    settingsHead("AI 服务商（可选）", "选好服务商、填好 Key 和模型之后才启用 AI。没配置时，MineDeck 不连接任何 AI 服务，也不下载价格数据。下面的预设只是方便填写的模板，不代表推荐。") +
    (ro ? `<div class="warn-card"><div class="wc-title">${icon("lock")} AI 设置仅支持在本机修改</div><div class="wc-note">这里只能查看。Key 和接口地址只能在运行 MineDeck 的那台电脑上改。</div></div>` : "") +
    `<div class="ai-status" id="ai-status"></div>` +
    group(
      riRow({ ico: "sparkle", title: "服务商", sub: `<span id="ai-note"></span>`,
        right: `<select id="ai-preset" aria-label="服务商"${dis}>${saved.preset ? "" : opt("", "未选择", "")}${v.presets.map(p => opt(p.id, p.name, saved.preset)).join("")}</select>` }) +
      riRow({ ico: "plug", title: "协议", sub: "自定义地址用哪种接口协议", cls: "ai-proto-row",
        right: `<select id="ai-proto" aria-label="协议"${dis}>${opt("openai", "OpenAI 兼容", saved.protocol)}${opt("anthropic", "Claude（Anthropic）", saved.protocol)}</select>` }) +
      riRow({ ico: "globe", title: "接口地址", sub: `<span id="ai-url-sub"></span>`,
        field: `<input type="text" id="ai-url" spellcheck="false" autocomplete="off" value="${esc(saved.baseUrl)}" aria-label="接口地址"${dis}><span class="ai-alts" id="ai-alts"></span>` }) +
      riRow({ ico: "key", title: "API Key", sub: `<span id="ai-key-sub"></span>`,
        field: `<input type="password" id="ai-key" autocomplete="off" aria-label="API Key"${dis}><button class="btn ghost" id="ai-key-clear"${dis} hidden>清除</button>` }) +
      riRow({ ico: "package", title: "模型", sub: `<span id="ai-model-sub">从模型列表里选，也可以直接填写模型名</span>`,
        field: `<input type="text" id="ai-model" list="ai-models" spellcheck="false" autocomplete="off" value="${esc(saved.model)}" aria-label="模型"${dis}><datalist id="ai-models"></datalist>` +
          `<button class="btn" id="ai-list"${dis}>${icon("refresh")} 获取模型列表</button>` }) +
      riRow({ ico: "bolt", title: "思考深度", sub: "只对 Claude 有效：越深越慢、越贵（claude-opus-5-5 默认是「中」）", cls: "ai-effort-row",
        right: `<select id="ai-effort" aria-label="思考深度"${dis}>${AI_EFFORTS.map(([val, label]) => opt(val, label, saved.effort)).join("")}</select>` }),
      { ico: true, attrs: `id="ai-form"` }) +
    (ro ? "" : saveBar("保存")) +
    group(riRow({ ico: "monitor", title: "测试连接", sub: `<span id="ai-test-sub"></span>`, right: `<button class="btn" id="ai-test"${dis}>${icon("bolt")} 测试连接</button>` }), { ico: true }) +
    settingsHead("每日额度", "按功能分别设每天最多用多少 token（输入、输出、缓存都算），用完后这项功能暂停到第二天 0 点。留空或填 0 表示不限。") +
    group(AI_FEATS.map(f => riRow({ ico: "clock", title: f.name, sub: `<span id="q-sub-${f.id}">${aiUsageLine(v.today[f.id], v.month[f.id])}</span>`,
      field: `<input type="number" id="q-${f.id}" min="0" step="any" inputmode="decimal" placeholder="不限" value="${v.quota[f.id] ? v.quota[f.id] / 10000 : ""}" aria-label="${esc(f.name)}每日额度（万 token）"${dis}>` +
        `<span class="ri-unit">万 token / 天</span><button class="btn" id="q-save-${f.id}" aria-label="保存${esc(f.name)}每日额度"${dis}>保存</button>`,
      after: `<div class="field-err ri-err" id="q-err-${f.id}"></div>` })).join(""), { ico: true }) +
    settingsHead("费用估算", "只是估算：按公开价格或自填单价，不做汇率换算（人民币和美元分开合计）。查不到价格时只显示 token 用量。") +
    group(
      riRow({ ico: "doc", title: "价格来源", sub: `<span id="ai-price-src"></span>`, right: `<button class="btn sm" id="ai-price-refresh"${dis}>${icon("refresh")} 刷新</button>` }) +
      riRow({ ico: "coin", title: "当前模型的单价", sub: `<span id="ai-model-price"></span>`, cls: "ai-mp-row" }),
      { ico: true }) +
    `<div class="group has-ico" id="ai-prices"></div>` +
    `<div id="ai-assist-slot"></div>` +
    `<div id="ai-mcp-slot"></div>` +
    (ro ? "" : group(riRow({ ico: "power", title: "停用 AI", sub: "清掉服务商设置和 Key。用量记录、额度和自填单价会留下。",
      right: `<button class="btn danger" id="ai-off">停用</button>` }), { ico: true, cls: "danger-zone", attrs: `id="ai-off-zone" style="margin-top:28px"` })) +
    `<div class="hint" style="margin-top:20px;line-height:1.8">隐私：用 AI 时，提问和查到的结果（服务器信息、配置、日志片段等）会发给你选的那家服务商。各家保留数据的规则不一样。在意这一点，可以用本机的 Ollama。
      Key 加密保存在这台电脑上，不会发到这个界面，也不会写进日志和操作记录。下载价格只是去取一份公开的价格表（经 GitHub 加速镜像），不发送任何用户数据。</div>`;

  const f = { preset: $("#ai-preset"), proto: $("#ai-proto"), url: $("#ai-url"), key: $("#ai-key"), model: $("#ai-model"), effort: $("#ai-effort") };
  const cur = () => P[f.preset.value];
  // 同一个网站：http/https、域名、端口都一样（和后端 ai.SameSite 一致：URL 的 origin 不管大小写和默认端口）
  const siteOf = u => { try { const x = new URL(u.trim()); return /^https?:$/.test(x.protocol) ? x.origin : ""; } catch { return ""; } };
  // 换了服务商，或者地址换到了别的网站：保存后已存的 Key 会清掉（不发给别的网站）；同一网站只改路径或协议，Key 照旧
  const keyGone = () => f.preset.value !== saved.preset || !siteOf(f.url.value) || siteOf(f.url.value) !== siteOf(saved.baseUrl);
  const protoNow = () => (cur() ? (cur().id === "custom" ? f.proto.value : cur().protocol) : "");
  const paintStatus = () => {
    const p = P[saved.preset];
    $("#ai-status").innerHTML = v.enabled
      ? `<span class="tag">已启用</span><span>${esc(p ? p.name : "")} · <code>${esc(saved.model)}</code></span>`
      : `<span class="tag warn">未启用</span><span>${v.missing ? "还差：" + esc(v.missing) : ""}</span>`;
    const off = $("#ai-off-zone");
    if (off) off.hidden = !saved.preset;
  };
  const paintForm = () => {
    const p = cur();
    const custom = p && p.id === "custom";
    body.querySelector(".ai-proto-row").hidden = !custom;
    body.querySelector(".ai-effort-row").hidden = protoNow() !== "anthropic";
    $("#ai-note").innerHTML = p ? esc(p.note) : "先选一家；只是填写模板，不代表推荐";
    $("#ai-alts").innerHTML = p && p.altUrls ? p.altUrls.map(a => `<button class="btn sm ghost" type="button" data-alt="${esc(a.url)}"${dis}>换成${esc(a.label)}</button>`).join("") : "";
    $("#ai-alts").querySelectorAll("[data-alt]").forEach(b => b.onclick = () => { f.url.value = b.dataset.alt; f.url.dispatchEvent(new Event("input", { bubbles: true })); });
    $("#ai-url-sub").textContent = p && p.id !== "custom" ? `预设地址：${p.baseUrl}` : "任意 OpenAI 兼容的地址，或者走 Claude 协议的中转";
    const noKey = p && p.noKey;
    const keepKey = v.keySet && !keyGone();
    f.key.disabled = ro || noKey;
    f.key.placeholder = noKey ? "不用 Key" : keepKey ? (ro ? "已设置" : "已设置（留空不修改）") : (ro ? "未设置" : "粘贴 API Key");
    $("#ai-key-clear").hidden = ro || !(v.keySet || v.keyBroken) || keyGone();
    let ks;
    if (noKey) ks = "本机模型不用 Key";
    else if (v.keyBroken && !keyGone()) ks = `<span class="warn-text">${icon("warn")} 密钥无法解开，可能换了电脑或 Windows 用户，请重新填写</span>`;
    else if (v.keySet && keyGone()) ks = `<span class="warn-text">${icon("warn")} 换了服务商或接口地址的网站：保存后原来的 Key 会清掉（不会发给新地址），要重新填写</span>`;
    else ks = "加密保存在这台电脑上。换电脑或换 Windows 用户后要重填";
    if (custom && !noKey) ks += "。注意：中转方能看到全部对话和 Key";
    $("#ai-key-sub").innerHTML = ks;
  };
  const paintPrices = () => {
    const ps = v.prices || {};
    let src;
    if (!v.priceNeeded) src = v.enabled ? "本机模型不花钱，不下载价格数据" : "配了要付费的服务商之后，才会下载公开价格（只配了 Ollama 就不下载）";
    else if (ps.fetching) src = "正在下载公开价格…";
    else src = ps.updated ? `按公开价格估算 · ${esc(ps.updated)} 更新 · ${ps.count} 个模型（7 天自动刷新一次）` : "还没取到公开价格";
    if (v.priceNeeded && ps.error) src += `<br><span class="warn-text">${icon("warn")} ${esc(ps.error)}</span>`;
    $("#ai-price-src").innerHTML = src;
    $("#ai-price-refresh").disabled = ro || !v.priceNeeded || ps.fetching;
    const mp = v.modelPrice;
    body.querySelector(".ai-mp-row").hidden = !mp;
    if (mp) {
      const unit = c => (c === "CNY" ? "¥" : "$");
      const num = x => (Math.round(x * 10000) / 10000).toString();
      let t;
      if (mp.local) t = "本机模型，不计费";
      else if (!mp.found) t = `查不到 <code>${esc(saved.model)}</code> 的公开价格，只显示 token 用量；可以在下面自填单价`;
      else {
        const u = unit(mp.currency);
        t = esc(`${mp.source === "custom" ? "自填：" : "公开价格："}输入 ${u}${num(mp.input)} · 输出 ${u}${num(mp.output)}` +
          (mp.cacheRead != null ? ` · 缓存命中 ${u}${num(mp.cacheRead)}` : "") + (mp.cacheWrite != null ? ` · 缓存写入 ${u}${num(mp.cacheWrite)}` : "") + "（每百万 token）");
        if (mp.tiered) t += "；按输入长度分档，这里是最低一档";
        if (mp.note) t += `<br>${esc(mp.note)}`;
      }
      $("#ai-model-price").innerHTML = t;
    }
    // 自填单价：一条一行，可删除；最后一行添加（每百万 token；自填的优先于公开价格）
    const unitPrice = (x, c) => (c === "CNY" ? "¥" : "$") + (Math.round(x * 10000) / 10000).toString();
    const rows = (v.customPrices || []).map((c, i) => riRow({ ico: "coin", titleHTML: `<code>${esc(c.model)}</code>`,
      sub: esc(`输入 ${unitPrice(c.input, c.currency)} · 输出 ${unitPrice(c.output, c.currency)}` +
        (c.cacheRead != null ? ` · 缓存命中 ${unitPrice(c.cacheRead, c.currency)}` : "") + "（每百万 token）"),
      right: `<button class="btn sm ghost icon-only" data-del-price="${i}" title="删除" aria-label="删除 ${esc(c.model)} 的自填单价"${dis}>${icon("trash")}</button>` })).join("");
    $("#ai-prices").innerHTML = rows + riRow({ ico: "plus", title: "自填单价", sub: "给某个模型填每百万 token 的单价（元或美元），优先于公开价格，也能补上查不到的模型",
      field: `<div class="ai-price-add"><input type="text" id="cp-model" placeholder="模型名" spellcheck="false" aria-label="模型名" value="${esc(saved.model)}"${dis}>
        <select id="cp-cur" aria-label="币种"${dis}><option value="CNY">元（¥）</option><option value="USD">美元（$）</option></select>
        <input type="number" id="cp-in" min="0" step="any" placeholder="输入" aria-label="输入单价"${dis}>
        <input type="number" id="cp-out" min="0" step="any" placeholder="输出" aria-label="输出单价"${dis}>
        <input type="number" id="cp-cache" min="0" step="any" placeholder="缓存（选填）" aria-label="缓存命中单价"${dis}>
        <button class="btn" id="cp-add"${dis}>${icon("plus")} 添加</button></div>`, after: `<div class="field-err ri-err" id="cp-err"></div>` });
    $("#ai-prices").querySelectorAll("[data-del-price]").forEach(b => b.onclick = () => {
      const list = (v.customPrices || []).filter((_, i) => i !== +b.dataset.delPrice);
      savePrices(list, "已删除这条自填单价");
    });
    $("#cp-add").onclick = () => {
      const model = $("#cp-model").value.trim(), inp = $("#cp-in").value, out = $("#cp-out").value, cache = $("#cp-cache").value;
      const bad = x => x === "" || !(+x >= 0);
      const err = msg => { $("#cp-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
      if (!model) { err("请填写模型名"); $("#cp-model").focus(); return; }
      if (bad(inp) || bad(out)) { err("输入和输出单价都要填（不小于 0 的数）"); (bad(inp) ? $("#cp-in") : $("#cp-out")).focus(); return; }
      if (cache !== "" && !(+cache >= 0)) { err("缓存命中单价要是不小于 0 的数"); $("#cp-cache").focus(); return; }
      err("");
      const item = { model, currency: $("#cp-cur").value, input: +inp, output: +out };
      if (cache !== "") item.cacheRead = +cache;
      savePrices([...(v.customPrices || []).filter(c => c.model !== model), item], "已添加自填单价");
    };
  };
  const savePrices = async (list, msg) => {
    try { v = await api("/api/ai/prices", { method: "PUT", body: { prices: list } }); toast(msg); }
    catch (e) { toast(e.message, true); return; }
    if (!stale(gen)) paintPrices();
  };
  const paintTest = () => {
    const btn = $("#ai-test");
    btn.disabled = ro || !v.enabled;
    if (!$("#ai-test-sub").dataset.result) $("#ai-test-sub").textContent = v.enabled ? "按保存的设置发一个很小的请求，看延迟和能不能调用工具（花一点点 token，不计入额度）" : "先把服务商、Key 和模型填好并保存";
  };
  const repaint = () => { paintStatus(); paintForm(); paintPrices(); paintTest(); };
  repaint();

  // 选预设：填好协议、地址和建议模型（都可以改）；Kimi、智谱等没有建议模型的，从模型列表里选
  f.preset.onchange = () => {
    const p = cur();
    if (!p) return;
    f.url.value = p.id === "custom" ? (saved.preset === "custom" ? saved.baseUrl : "") : p.baseUrl;
    f.model.value = p.id === saved.preset ? saved.model : (p.model || "");
    if (p.id === "custom" && saved.preset !== "custom") f.proto.value = "openai";
    $("#ai-models").innerHTML = "";
    paintForm();
  };
  f.url.addEventListener("input", paintForm);
  f.proto.addEventListener("change", paintForm);

  const readForm = () => ({ preset: f.preset.value, proto: protoNow(), url: f.url.value.trim(), key: f.key.value.trim() ? "已填" : "", model: f.model.value.trim(), effort: protoNow() === "anthropic" ? f.effort.value : "" });
  let formBase = JSON.stringify(readForm()); // 测试连接只认已保存的设置
  if (!ro) wireSaveBar(body, gen, readForm,
    async () => {
      const p = cur();
      if (!p) throw new Error("先选择服务商");
      const b = { preset: p.id, baseUrl: f.url.value.trim(), model: f.model.value.trim(), effort: protoNow() === "anthropic" ? f.effort.value : "" };
      if (p.id === "custom") b.protocol = f.proto.value;
      if (f.key.value.trim()) b.key = f.key.value.trim();
      else if (v.keySet && keyGone()) { // 会清掉已存的 Key：先说清楚，确认了才存，并且明着传空 Key（操作记录里看得出清过）
        const r = await confirmModal({ title: "保存后要重新填 Key", body: p.id !== saved.preset
          ? `换成了「${p.name}」，原来那家的 Key 不会带过去，保存后会清掉。可以先在上面填好新的 Key 再保存。`
          : "接口地址换到了另一个网站，原来的 Key 不会发给它，保存后会清掉。可以先在上面填好这个地址的 Key 再保存。",
          okText: "清掉 Key 并保存", danger: true, icon: "key" });
        if (!r) return false;
        b.key = "";
      }
      v = await api("/api/ai/provider", { method: "PUT", body: b });
      f.key.value = ""; // 存好后清空（只填了空白也一样，什么都不存）
      Object.assign(saved, v.provider);
      f.url.value = saved.baseUrl;
      f.preset.querySelector('option[value=""]')?.remove(); // 配过之后不再有「未选择」，停用走下面的「停用 AI」
      formBase = JSON.stringify(readForm());
      delete $("#ai-test-sub").dataset.result;
      repaint();
      window.MDAssist?.refresh(); // 标题栏的助手入口随之更新
      toast(v.enabled ? "AI 设置已保存" : `已保存。还差：${v.missing}`);
    });

  $("#ai-key-clear").onclick = async () => {
    const r = await confirmModal({ title: "清除 AI 的 Key？", body: "清除后 AI 功能停用，需要时可以重新填写。", okText: "清除", danger: true, icon: "trash" });
    if (!r) return;
    try { v = await api("/api/ai/provider", { method: "PUT", body: { key: "" } }); repaint(); toast("已清除 Key"); }
    catch (e) { toast(e.message, true); }
  };

  // 获取模型列表：用表单里当前的服务商和地址（还没保存也行）；Key 不填时，只有地址没改才用已存的 Key
  $("#ai-list").onclick = async () => {
    const p = cur();
    if (!p) { toast("先选择服务商", true); return; }
    const btn = $("#ai-list");
    btn.disabled = true;
    try {
      const b = { preset: p.id, baseUrl: f.url.value.trim() };
      if (p.id === "custom") b.protocol = f.proto.value;
      if (f.key.value.trim()) b.key = f.key.value.trim();
      const ms = await api("/api/ai/models", { method: "POST", body: b });
      if (stale(gen)) return;
      $("#ai-models").innerHTML = ms.map(m => `<option value="${esc(m.id)}">${esc(m.name || "")}</option>`).join("");
      $("#ai-model-sub").textContent = ms.length ? `取到 ${ms.length} 个模型：点输入框选一个，也可以直接填写` : "服务商没有返回模型，请直接填写模型名";
      if (ms.length) { f.model.focus(); toast(`取到 ${ms.length} 个模型`); }
    } catch (e) { toast(e.message, true); }
    btn.disabled = ro;
  };

  $("#ai-test").onclick = async () => {
    if (JSON.stringify(readForm()) !== formBase) { toast("先保存服务商设置，再测试连接", true); return; }
    const btn = $("#ai-test"), sub = $("#ai-test-sub");
    btn.disabled = true;
    sub.textContent = "测试中…";
    try {
      const r = await api("/api/ai/test", { method: "POST" });
      if (stale(gen)) return;
      sub.dataset.result = "1";
      if (!r.ok) sub.innerHTML = `<span class="warn-text">${icon("warn")} 连接失败：${esc(r.error)}</span>`;
      else {
        const tools = { yes: "能调用工具", no: `<span class="warn-text">${icon("warn")} 这个模型不支持工具调用，助手用不了</span>`, unknown: `<span class="warn-text">${icon("warn")} 模型这次没有调用工具，可能不支持</span>` }[r.tools] || "";
        const cost = r.cost && r.cost.source === "local" ? "（本机模型不计费）" : r.cost ? `（约 ${fmtMoney(r.cost.amount, r.cost.currency)}）` : "";
        sub.innerHTML = `${icon("check")} 连接正常 · 首个回应 ${r.latencyMs} ms · ${tools} · 用了 ${fmtTokens((r.usage.input || 0) + (r.usage.output || 0) + (r.usage.cacheRead || 0) + (r.usage.cacheWrite || 0))} token${esc(cost)}`;
        v = await api("/api/ai/settings").catch(() => v); // Claude 中转：测出来的功能开关已存进设置
      }
    } catch (e) { sub.innerHTML = `<span class="warn-text">${icon("warn")} ${esc(e.message)}</span>`; }
    btn.disabled = ro || !v.enabled;
  };

  // 每日额度：单字段保存（万 token），回车即保存；不合法时就近提示，不弹提示框
  AI_FEATS.forEach(feat => {
    const inp = $(`#q-${feat.id}`), errBox = $(`#q-err-${feat.id}`);
    const err = msg => { errBox.innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; inp.classList.toggle("invalid", !!msg); };
    inp.addEventListener("input", () => err(""));
    bindSaveInput(inp, $(`#q-save-${feat.id}`), async val => {
      const n = val.trim() === "" ? 0 : +val;
      if (!(n >= 0) || n > 1e8) { err("请填不小于 0 的数（单位：万 token）"); throw Object.assign(new Error("额度不对"), { silent: true }); }
      v = await api("/api/ai/quota", { method: "PUT", body: { [feat.id]: Math.round(n * 10000) } });
      toast(n ? `${feat.name}每天最多 ${fmtTokens(Math.round(n * 10000))} token` : `${feat.name}不限额度`);
    }, gen);
  });

  $("#ai-price-refresh").onclick = async () => {
    $("#ai-price-refresh").disabled = true;
    $("#ai-price-src").textContent = "正在下载公开价格…";
    try { v = await api("/api/ai/prices/refresh", { method: "POST" }); toast("价格数据已更新"); }
    catch (e) { toast(e.message, true); v = await api("/api/ai/settings").catch(() => v); }
    if (!stale(gen)) paintPrices();
  };

  if (!ro) $("#ai-off").onclick = async () => {
    const r = await confirmModal({ title: "停用 AI？", body: "会清掉服务商设置和 Key。用量记录、每日额度和自填单价会留下。之后可以重新配置。", okText: "停用", danger: true, icon: "power" });
    if (!r) return;
    try { await api("/api/ai/provider", { method: "PUT", body: { preset: "" } }); toast("已停用 AI"); dirtyCheck = null; navigate(); }
    catch (e) { toast(e.message, true); }
  };
  window.MDAssist?.settings($("#ai-assist-slot"), v, gen); // 应用内助手（assistant.js）
  window.MDMcp?.settings($("#ai-mcp-slot"), gen); // MCP 接口（mcp.js）
}

/* — 更新与关于 — */
function setAbout(body, info) {
  body.innerHTML = `<div class="brand-head about-head">${logoImg()}<div><h2>MineDeck</h2><div class="sub">Windows 本地 Minecraft 开服工具 · v${esc(info.version)}</div></div></div>` +
    settingsHead("版本与更新", "") +
    group(
      riRow({ ico: "info", title: "当前版本", sub: "MineDeck · Windows 本地 Minecraft 开服工具", right: `<span class="badge core">v${esc(info.version)}</span>` }) +
      riRow({ ico: "arrowUp", title: "更新仓库", sub: "从该 GitHub 仓库的 Releases 检查新版本（国内加速镜像兜底）",
        field: `<input type="text" id="up-repo" value="${esc(info.config.updateRepo || "")}" placeholder="GitHub 仓库，如 yourname/MineDeck" aria-label="更新仓库">
          <button class="btn" id="up-save">保存</button><button class="btn" id="up-check">${icon("search")} 检查更新</button>` }) +
      riRow({ ico: "refresh", title: "启动时静默检查更新", sub: "程序启动后在后台检查一次新版本（需已填写更新仓库）",
        right: `<label class="switch"><input type="checkbox" id="up-silent" ${info.config.checkUpdateOnStart ? "checked" : ""} aria-label="启动时静默检查更新"><span class="sw"></span></label>` }),
      { ico: true }) +
    `<div class="hint" id="up-result" aria-live="polite"></div>` +
    settingsHead("关于", "") +
    `<div class="hint" style="line-height:1.9">
      内网穿透基于 <b>OpenFrp OPENAPI</b> 接入（<a href="https://www.openfrp.net" target="_blank">openfrp.net</a>）。<br>
      界面字体：Geist、Geist Mono（© The Geist Project Authors）与 Noto Sans SC（© Adobe），均为 SIL Open Font License 1.1，
      子集随程序内嵌，许可原文见 <a href="fonts/OFL-Geist.txt" target="_blank">OFL-Geist.txt</a>、<a href="fonts/OFL-NotoSansSC.txt" target="_blank">OFL-NotoSansSC.txt</a>。<br>
      本软件为开源项目，遵循仓库 LICENSE（MIT）。<br>
      Minecraft 是 Mojang Studios 的商标，本工具与 Mojang / Microsoft / 网易均无隶属或授权关系。<br>
      NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT.
    </div>` +
    group(riRow({ ico: "power", title: "退出程序", sub: "先保存世界，停掉正在运行的服务器，然后退出",
      right: `<button class="btn danger" id="quit-app">${icon("power")} 退出 MineDeck</button>` }), { ico: true, cls: "danger-zone", attrs: `style="margin-top:28px"` });
  bindSaveInput($("#up-repo"), $("#up-save"), async v => {
    await api("/api/config", { method: "PUT", body: { updateRepo: v.trim() } });
    toast("更新仓库已保存");
  }, viewGen);
  $("#up-silent").onchange = async () => {
    try { await api("/api/config", { method: "PUT", body: { checkUpdateOnStart: $("#up-silent").checked } }); toast($("#up-silent").checked ? "已开启启动时检查更新" : "已关闭启动时检查更新"); }
    catch (e) { toast(e.message, true); $("#up-silent").checked = !$("#up-silent").checked; }
  };
  $("#up-check").onclick = async () => {
    $("#up-result").textContent = "检查中…";
    try {
      const r = await api("/api/update/check", { method: "POST" });
      if (r.hasUpdate) {
        $("#up-result").innerHTML = `发现新版本 <b style="color:var(--accent-text)">${esc(r.latest.tag)}</b>（当前 v${esc(r.current)}） `;
        const btn = document.createElement("button");
        btn.className = "btn sm primary";
        btn.innerHTML = icon("arrowUp") + " 立即更新并重启";
        btn.onclick = async () => {
          btn.disabled = true;
          try { await api("/api/update/apply", { method: "POST" }); document.body.innerHTML = `<div style="padding:80px;text-align:center;color:var(--muted)">正在更新，程序将自动重启…</div>`; }
          catch (e) { toast(e.message, true); btn.disabled = false; }
        };
        $("#up-result").appendChild(btn);
      } else {
        $("#up-result").textContent = `已是最新（v${r.current}）`;
      }
    } catch (e) { $("#up-result").textContent = e.message; }
  };
  $("#quit-app").onclick = async () => {
    const r = await confirmModal({ title: "退出 MineDeck？", body: "会先保存世界，停掉正在运行的服务器，然后退出程序。", okText: "退出", danger: true, icon: "power" });
    if (!r) return;
    try { await api("/api/shutdown", { method: "POST" }); document.body.innerHTML = `<div style="padding:80px;text-align:center;color:var(--muted)">MineDeck 正在停止服务器并退出，可以关闭此页面了。</div>`; }
    catch (e) { toast(e.message, true); }
  };
}

/* 首次运行引导卡（用户协议与免责声明 + 选下载源 + 存放位置说明；
   同意后写 config.onboarded / tosAccepted，二者齐备才不再弹出） */
function showOnboarding(info) {
  const root = $("#modal-root");
  const SRC = { auto: "自动（推荐）— 国内镜像优先，失败自动回退官方源", mirror: "镜像优先 — 国内下载更快", official: "仅官方源 — 网络直连国外" };
  const cur = (info.config && info.config.source) || "auto";
  root.innerHTML = `<div class="modal-mask enter"><div class="modal onboard" role="dialog" aria-modal="true">
    <div class="brand-head">${logoImg()}<div><h3>欢迎使用 MineDeck</h3><div class="sub">Minecraft 开服工具</div></div></div>
    <div class="m-body">
      <div class="ob-sec" style="margin-top:0"><div class="ob-label">用户协议与免责声明</div>
        <div class="ob-terms">
          MineDeck 是开源、非营利的 Minecraft Java 版开服辅助工具。它只从 Mojang、PaperMC 等官方公开渠道下载服务端，并在你的电脑上配好运行环境。<b>不包含、也不分发游戏客户端，不修改游戏本身，也不破解正版验证</b>。
          <ol>
            <li><b>非官方产品</b>：本工具由社区独立开发，与 Mojang Studios、Microsoft 及网易（中国版《我的世界》运营方）均无任何隶属、合作或授权关系。NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT. "Minecraft" 及相关商标归 Mojang / Microsoft 所有。</li>
            <li><b>遵守官方条款</b>：使用本工具搭建与运行服务器，须遵守 <b>Minecraft EULA</b> 与 Microsoft 服务协议；如你使用网易代理的中国版《我的世界》账号或内容，请另行遵守网易的用户协议（两套账号体系互不相通，本工具仅面向国际 Java 版服务端）。</li>
            <li><b>正版建议</b>：建议使用正版 Mojang / Microsoft 账号并开启正版验证联机。是否使用离线模式由你自行决定并自行承担相应后果，本工具不提供任何绕过正版验证的功能。</li>
            <li><b>对外服务责任自负</b>：通过内网穿透等方式向他人开放服务器时，你是该服务器的运营者，须自行遵守所在地法律法规（含网络安全与未成年人保护相关规定），并对服务器内容与运营行为负责。</li>
            <li><b>开源免责</b>：本软件基于 MIT 协议按「现状」提供，不作任何明示或默示担保。因使用或无法使用本软件导致的任何数据丢失、账号问题或其他损失，作者及贡献者不承担责任。</li>
          </ol>
        </div>
        <label class="ob-agree"><input type="checkbox" id="ob-agree"><span>我已阅读并同意上述《用户协议与免责声明》及 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft EULA</a></span></label>
      </div>
      <div class="ob-sec"><div class="ob-label">下载源（之后可在「设置」里随时更改）</div>
        <div id="ob-src">${Object.entries(SRC).map(([v, label]) =>
    `<label class="ob-radio"><input type="radio" name="ob-src" value="${v}" ${v === cur ? "checked" : ""}><span>${esc(label)}</span></label>`).join("")}</div>
      </div>
      <div class="ob-sec"><div class="ob-label">你的服务器将存放在</div>
        <div class="ob-path">${esc(info.serversRoot || "程序目录\\servers")}</div>
        <div class="hint" style="margin-top:6px">每台服务器一个子文件夹。想放到 D 盘等大容量分区，可到「设置 → 存储位置」更改。</div>
      </div>
    </div>
    <div class="m-actions"><button class="btn primary" id="ob-go" disabled>同意并开始使用</button></div>
  </div></div>`;
  const agree = root.querySelector("#ob-agree"), go = root.querySelector("#ob-go");
  agree.onchange = () => { go.disabled = !agree.checked; };
  go.onclick = async () => {
    const sel = root.querySelector('input[name="ob-src"]:checked');
    try { await api("/api/config", { method: "PUT", body: { source: sel ? sel.value : cur, onboarded: true, tosAccepted: true } }); }
    catch (e) { toast(e.message, true); }
    dismiss(root.firstElementChild);
  };
}

/* 壁纸个性化：程序旁 bg/ 目录有图则随机取一张作低透明度铺底（双主题各自蒙版） */
async function applyBackground() {
  try {
    const bg = await api("/api/bg");
    if (!bg.images || !bg.images.length) return;
    const pick = bg.images[Math.floor(Math.random() * bg.images.length)];
    document.documentElement.style.setProperty("--bg-image", `url("/bg/${encodeURIComponent(pick)}")`);
    document.body.classList.add("has-bg");
  } catch {}
}

/* ---------- 启动 ---------- */
(async function boot() {
  let info;
  try {
    info = await api("/api/app");
    $("#app-ver").textContent = info.version;
    const cores = await api("/api/cores").catch(() => []);
    cores.forEach(c => CORES[c.id] = c);
  } catch (e) {
    // 局域网访问（如手机扫码打开）尚未登录：api() 已弹出密码遮罩，保留它，不整页覆盖成「无法连接」；登录成功后 reload 重跑 boot
    if (e.needLogin) return;
    document.body.innerHTML = `<div style="padding:60px;text-align:center;color:var(--danger-text)">无法连接 MineDeck 后端：${esc(e.message)}<br><br>请通过程序窗口打开本页面。</div>`;
    return;
  }
  navigate();
  window.MDAssist?.init(); // 应用内 AI 助手的入口（assistant.js）
  window.MDMcp?.init(); // MCP 接口：外部 AI 提出、等你确认的操作（mcp.js）
  document.dispatchEvent(new Event("md-ready")); // 启动画面（若在播）据此在方块拼完后淡出
  chromeReport.last = (info.config && info.config.chromeColor) || "";
  reportChrome();
  applyBackground();
  // 协议为新增项：老用户虽已 onboarded，但未同意过协议（tosAccepted）仍需补一次
  if (!info.config || !info.config.onboarded || !info.config.tosAccepted) showOnboarding(info);
})();
