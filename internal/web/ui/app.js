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
    ...(opts.keepalive ? { keepalive: true } : {}), // 页面刷新或关掉时发的：页面没了也送得到（见工具箱草稿的 pagehide）
  });
  let j;
  try { j = await res.json(); } catch { throw new Error(`服务异常 (HTTP ${res.status})`); }
  // needLogin 标记：让 boot() 区分「需要登录」与真正的后端故障
  if (res.status === 401) { showLogin(); throw Object.assign(new Error(j.error || "需要登录"), { needLogin: true }); }
  if (!j.ok) throw Object.assign(new Error(j.error || "未知错误"), { status: res.status, hint: j.hint, portBusy: j.portBusy, created: j.created }); // hint：后端另给的一句人话，见 errSummary；created：平台上的隧道已建好（见 createPlatTunnel）
  return opts.full ? j : j.data;
}

/* 局域网访问的密码登录遮罩 */
function showLogin() {
  if ($("#login-mask")) return;
  const d = document.createElement("div");
  d.className = "modal-mask enter";
  d.id = "login-mask";
  d.innerHTML = `<div class="modal"><div class="brand-head">${logoImg()}<h3>MineDeck 手机远程管理</h3></div>
    <div class="m-body">请输入访问密码（在主机的「设置 → 手机远程管理」里设置）</div>
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
    } catch (e) { toastErr("登录失败", e); }
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

/* 提示：标题栏下方右侧，从上方落入；离场向右淡出，其余提示平移补位。
   普通提示停 3.5 秒，点一下关闭。报错提示带「详情」（点开是只读的对话框，原文可以选中、复制）和关闭键，
   点提示本身也是看详情（见 docs/文案重写/00-报错信息.md）。
   报错提示和带按钮的提示至少停 20 秒，鼠标停在上面、或者它的「详情」开着时不走（可用性走查 R03：
   以前 8 秒就没了，点「详情」的时候提示已经不在，点中的是底下的东西）。
   在服务器详情页上，提示落在页头下面，不盖住启动、停止按钮（见 placeToasts）。
   detail：「详情」里放的原文，不给就是 msg。拿接口的报错拼提示用 toastErr，不要直接传 e.message。
   opts.actions：提示里的按钮，[{ label, aria, onClick }]，点了不关提示（onClick 自己决定）。 */
const TOAST_MS = 3500, TOAST_LINGER_MS = 20000;
function toast(msg, isErr = false, detail, opts = {}) {
  const box = $("#toasts");
  const d = document.createElement("div");
  d.className = "toast enter" + (isErr ? " error" : "");
  d.innerHTML = `<span class="t-ico">${icon(isErr ? "warn" : "check")}</span>`;
  const t = document.createElement("span");
  t.className = "t-msg";
  t.textContent = msg;
  d.appendChild(t);
  d.title = isErr ? "点击查看详情" : "点击关闭";
  box.appendChild(d);
  let gone = false;
  const kill = () => {
    if (gone) return;
    gone = true;
    clearTimeout(timer);
    if (reducedMotion()) { flipList(box, () => d.remove()); return; }
    d.classList.remove("enter"); d.classList.add("leave");
    let removed = false;
    const done = () => { if (removed) return; removed = true; if (d.isConnected) flipList(box, () => d.remove()); };
    d.addEventListener("animationend", done, { once: true });
    setTimeout(done, 400);
  };
  // 停留计时：holds 是「先别走」的理由数（鼠标在上面、详情开着），都没了才接着倒数；恢复时至少再留 4 秒，不让人刚移开就没了
  const actions = opts.actions || [];
  const linger = isErr || actions.length > 0;
  let left = linger ? TOAST_LINGER_MS : TOAST_MS, timer = 0, since = 0, holds = 0;
  const run = () => { if (gone || holds) return; since = Date.now(); timer = setTimeout(kill, left); };
  const hold = () => { holds++; if (timer) { clearTimeout(timer); timer = 0; left = Math.max(0, left - (Date.now() - since)); } };
  const release = () => { holds = Math.max(0, holds - 1); if (!holds && !gone) { left = Math.max(left, 4000); run(); } };
  // a.close：点了以后收掉这条提示（「去换 Java」这类修复按钮：人已经被带去改了，提示留着只会挡路）
  const act = a => { a.onClick(); if (a.close) kill(); };
  for (const a of actions) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "btn sm ghost";
    b.textContent = a.label;
    if (a.aria) b.setAttribute("aria-label", a.aria);
    b.onclick = ev => { ev.stopPropagation(); act(a); };
    d.appendChild(b);
  }
  if (isErr) {
    const full = detail === undefined ? msg : detail;
    // 报错提示上的按钮（修复的去处）在「详情」里也给一份（回访走查 11-16：详情里只有「复制报错信息」，看完还得回来找按钮）
    const showDetail = () => { hold(); showErrDetail(full, actions.map(a => ({ ...a, onClick: () => act(a) }))).then(release); };
    const b = document.createElement("button");
    b.type = "button";
    b.className = "btn sm ghost";
    b.textContent = "详情";
    b.setAttribute("aria-label", "查看报错详情");
    b.onclick = ev => { ev.stopPropagation(); showDetail(); };
    d.appendChild(b);
    d.onclick = showDetail;
  } else {
    d.onclick = kill;
  }
  if (linger) {
    const x = document.createElement("button");
    x.type = "button";
    x.className = "t-x";
    x.innerHTML = icon("x");
    x.setAttribute("aria-label", "关闭这条提示");
    x.onclick = ev => { ev.stopPropagation(); kill(); };
    d.appendChild(x);
    d.addEventListener("mouseenter", hold);
    d.addEventListener("mouseleave", release);
  }
  placeToasts(); // 按钮都加进去、高度定了再量（Codex 复审 beta.3 之后第二轮：以前先量，加了按钮换行变高，下半截可能出屏幕）
  run();
  d.dismiss = kill; // 调用方可以提前收掉它（交接包导出：重试成功以后，上一次的报错不再挂着）
  return d; // 调用方可以换掉点击的行为（如 mcp.js：点一下打开 AI 助手）
}
/* 提示的位置：服务器详情页的页头（启动、停止、打开文件夹）和它下面的常驻提示区（上次没能启动、Java 找不到了……带着「改端口」
   「去换 Java」这些修复按钮）在可视区里时，提示落到它们下面，不盖住这些按钮（回访走查 11-16：以前只躲页头，提示区里的按钮正好被盖住）；
   都滚出去了、或者不在详情页，回到标题栏下方。看得见比不挡按钮要紧：提示区很高（矮窗口、几条提示叠着）、放到下面会出屏幕时，
   退回页头下面；还放不下就回到标题栏下方（Codex 复审 beta.3 之后第一轮：不然整条报错在屏幕外照样倒数、自己走掉）。
   出提示、提示区变了、滚动、改窗口大小时重算 */
function placeToasts() {
  const box = $("#toasts");
  if (!box || !box.childElementCount) { if (box) box.style.top = ""; return; } // 没有提示就不量（控制台滚动时也会进来）
  const head = document.querySelector(".detail-head");
  let top = "";
  if (head) {
    box.style.top = "";
    const r = head.getBoundingClientRect(), base = parseFloat(getComputedStyle(box).top) || 0;
    const h = box.offsetHeight, fits = y => y + h <= innerHeight - 8;
    const notes = document.getElementById("d-notices");
    const nb = notes && !notes.hidden ? Math.max(r.bottom, notes.getBoundingClientRect().bottom) : r.bottom;
    if (r.top < innerHeight) {
      if (nb > base && fits(nb + 8)) top = Math.round(nb + 8) + "px";
      else if (r.bottom > base && fits(r.bottom + 8)) top = Math.round(r.bottom + 8) + "px";
    }
  }
  box.style.top = top;
}
addEventListener("resize", placeToasts);
addEventListener("hashchange", () => setTimeout(placeToasts, 0));
document.addEventListener("scroll", placeToasts, { capture: true, passive: true }); // #main 自己滚，scroll 不冒泡，用捕获接

/* 弹层叠放：每个对话框是 #modal-root 里的一层遮罩，新开的叠在上面；关掉上层回到下层，下层的输入都还在（可用性走查 R04）。
   以前「报错详情」和确认框把 #modal-root 整个换掉，开着的弹窗连同填好的内容一起没了。
   每层自己在 document 上听 Esc，只有最上面那层（正在离场的不算）才响应，不然按一次全关。 */
let modalSeq = 0;
function modalLayer(html, id = "") {
  const d = document.createElement("div");
  d.className = "modal-mask enter";
  if (id) d.id = id;
  d.innerHTML = html;
  $("#modal-root").appendChild(d);
  return d;
}
const topLayer = mask => {
  const live = [...$("#modal-root").children].filter(x => !x.dataset.leaving);
  return live[live.length - 1] === mask;
};
/* 对话框里有没有要填的东西：有的点遮罩不关，点空了就把填的丢了 */
const hasFields = mask => !!mask.querySelector(".modal input, .modal select, .modal textarea");
/* 同一个 id 已经被下面那层用了就换一个（标题的 aria-labelledby 要指得准）；测试按 #modal-title 找确认框，只有一层时照旧 */
const layerId = base => document.getElementById(base) ? `${base}-${++modalSeq}` : base;

/* 错误详情的只读对话框：原文可选中，一键复制（做法见 docs/文案重写/00-报错信息.md）。叠在已有的弹窗上面，关掉回到那个弹窗。
   actions：修复的去处（同 toast 的 opts.actions，[{ label, aria, onClick }]），排在「复制报错信息」前面；点了先关掉对话框再去。
   去处由发起请求的地方给（它知道这次是什么没做成），这里不去猜报错原文。
   返回关闭时兑现的 Promise（提示在详情开着时不走，靠它接着计时） */
function showErrDetail(text, actions = []) {
  return new Promise(resolve => {
    const prev = document.activeElement;
    const tid = layerId("errd-title");
    const mask = modalLayer(`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="${tid}">
    <h3 id="${tid}">报错信息</h3>
    <div class="m-body"><div class="err-detail" tabindex="0">${esc(String(text || ""))}</div></div>
    <div class="m-actions">${actions.map((a, k) => `<button class="btn" data-fix="${k}"${a.aria ? ` aria-label="${esc(a.aria)}"` : ""}>${esc(a.label)}</button>`).join("")}<button class="btn" data-x="copy">复制报错信息</button><button class="btn primary" data-x="close">关闭</button></div>
  </div>`);
    let closed = false;
    const close = () => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      dismiss(mask);
      if (prev && prev.focus && prev.isConnected) prev.focus();
      resolve();
    };
    const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(); }; // 同 confirmModal：Esc 只关最上面这一层
    document.addEventListener("keydown", onKey);
    mask.querySelector('[data-x="close"]').onclick = close;
    mask.querySelector('[data-x="copy"]').onclick = () => copy(String(text || ""), "已复制报错信息");
    mask.querySelectorAll("[data-fix]").forEach(b => b.onclick = () => { const a = actions[+b.dataset.fix]; close(); a.onClick(); });
    mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget) close(); });
    mask.querySelector('[data-x="close"]').focus();
  });
}
/* 剪贴板写不进去时：把原文放进可选中的框并选中，说明怎么手动复制。不提示「已复制」。
   http 的手机远程管理页上 navigator.clipboard 经常没有，execCommand 也会失败。
   prev：关掉以后焦点还给谁（copy() 在动剪贴板之前记下的，不然拿到的是临时 textarea 删掉以后的 body） */
function showCopyFallback(text, prev = document.activeElement) {
  const tid = layerId("copyfb-title");
  const mask = modalLayer(`<div class="modal" id="copy-fallback" role="dialog" aria-modal="true" aria-labelledby="${tid}">
    <h3 id="${tid}">请手动复制</h3>
    <div class="m-body">剪贴板写不进去。下面是完整原文，已经选中：电脑上按 Ctrl+C 复制；手机上长按选中的文字，再点「拷贝」。</div>
    <textarea class="copy-fallback" readonly rows="5" aria-label="要复制的完整内容"></textarea>
    <div class="m-actions"><button class="btn primary" data-x="close">关闭</button></div>
  </div>`);
  const ta = mask.querySelector("textarea");
  ta.value = String(text ?? "");
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    document.removeEventListener("keydown", onKey);
    dismiss(mask);
    if (prev && prev.focus && prev.isConnected) prev.focus();
  };
  const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(); };
  document.addEventListener("keydown", onKey);
  mask.querySelector('[data-x="close"]').onclick = close;
  mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget && !hasFields(mask)) close(); });
  ta.focus();
  ta.select();
  try { ta.setSelectionRange(0, ta.value.length); } catch {}
}
/* 写入剪贴板。成功返回 true。安全上下文优先用 clipboard API；失败或 http 页面再试 execCommand。
   execCommand 返回 false 或抛错都算失败，不能当成已经复制。
   execCommand 要把焦点挪到临时 textarea 上，用完删掉以后把焦点还给原来的元素（键盘用户点完复制键焦点不丢） */
async function writeClipboard(text) {
  const value = String(text ?? "");
  if (window.isSecureContext && navigator.clipboard && navigator.clipboard.writeText) {
    try { await navigator.clipboard.writeText(value); return true; } catch {}
  }
  const back = document.activeElement;
  let ta;
  try {
    ta = document.createElement("textarea");
    ta.className = "copy-scratch";
    ta.value = value;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "0";
    ta.style.left = "0";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    try { ta.setSelectionRange(0, value.length); } catch {}
    return document.execCommand("copy") === true;
  } catch {
    return false;
  } finally {
    if (ta) ta.remove();
    if (back && back !== document.body && back.isConnected && back.focus) back.focus({ preventScroll: true });
  }
}
/* 复制到剪贴板。成功才提示已复制，并返回 true。失败弹出手动复制框，返回 false。 */
async function copy(text, okMsg = "已复制") {
  const prev = document.activeElement; // 手动复制框关掉以后焦点还给它（比如叠在下面的「报错信息」对话框里的复制键）
  if (await writeClipboard(text)) { toast(okMsg); return true; }
  showCopyFallback(text, prev);
  return false;
}
/* 复制按钮的微交互：只有真的写进剪贴板才把图标换成对勾。1.3 秒后换回。 */
async function copyFlash(b, text, okMsg) {
  const ok = await copy(text, okMsg);
  if (!ok || !b || b.dataset.busy) return;
  b.dataset.busy = "1";
  const old = b.innerHTML;
  b.innerHTML = `<span class="pop" style="display:inline-grid">${icon("check")}</span>${b.classList.contains("icon-only") ? "" : " 已复制"}`;
  setTimeout(() => { if (!b.isConnected) return; b.innerHTML = old; playClass(b, "swap-in"); delete b.dataset.busy; }, 1300);
}
/* data-copy-exact 上的原文（命令、地址、报错）一键复制。属性里是转义后的完整字符串，不带周围的说明。 */
document.addEventListener("click", ev => {
  const b = ev.target instanceof Element ? ev.target.closest("[data-copy-exact]") : null;
  if (!b) return;
  ev.preventDefault();
  ev.stopPropagation();
  const text = b.getAttribute("data-copy-exact") || "";
  const okMsg = b.dataset.copyMsg || "已复制";
  if (b.classList.contains("btn")) copyFlash(b, text, okMsg);
  else copy(text, okMsg);
});

/* 连接地址（可用性走查 R01）：同一局域网的朋友在游戏里「添加服务器」时填的「本机局域网 IP:端口」。
   IP 取 /api/app 的 ips，按 lanIPRank 排（家用网段在前，同「手机远程管理」的默认地址）；详情页头换过 IP 的记在本机，
   卡片和页头都用换过的那个。端口用服务器列表的 connectPort（代理端是它自己配置文件里的端口，还没生成时为 0）。 */
const rankIPs = ips => (ips || []).slice().sort((a, b) => lanIPRank(a) - lanIPRank(b));
/* 上一次从 /api/app 拿到的本机 IP（排好序；null 是还没拿到过）。列表和详情页不等 /api/app，先用它画，
   不然每换一次标签，地址那一行都会先缩成只有端口再变回来 */
let knownIPs = null;
const learnIPs = a => (knownIPs = rankIPs(a && a.ips));
function pickLanIP(ips) {
  let saved = "";
  try { saved = localStorage.getItem("amh_lan_ip") || ""; } catch {}
  return ips.includes(saved) ? saved : ips[0] || "";
}
const connectPortOf = i => i.kind === "proxy" ? (i.connectPort || 0) : (i.connectPort || i.port || 0);

/* ---------- 报错的正文（规矩见 docs/文案重写/01-补完口径.md 第一、二节，补完记录见 03-报错提示补完记录.md）----------
   调用处只给一句结论 what，说这一步没做成什么，如「无法启动服务器」「保存失败」。正文按下面的顺序拼：
   1. 请求根本没发出去（浏览器给的 TypeError「Failed to fetch」）：结论 +「：连不上 MineDeck」；
   2. 后端的话是一句给人看的中文：整句接在结论后面（「无法删除服务器：请先停止服务器再删除」）。
      后端那句自己带着全角冒号时用句号接，免得一句里两个冒号（「发送失败。AI 未启用：先在…」）。
      后端另给了一句人话（e.hint，例如 AI 服务商报错的「Key 不对或已失效」）时，拿它代替原文来判断、来接：
      原文里带着状态码和服务商的英文原话，整句接不上；
   3. 其余只写结论。
   「详情」和「复制报错信息」里永远是完整原文，一个字不删。
   判断只看文字本身，不看 HTTP 状态码：同一个 400 既可能是「服务器名称需为 1~40 个字符」，也可能带着 fork/exec 的系统原文。
   后端的话要么整句接上，要么不接，不截短。
   toastErr（右上角的提示）、errBox（页面报错框）、errInline（一行字里的报错）都走 errSummary，规则只写在这一处；
   要调规则，改下面几个常量。 */

/* 「给人看的话」= 含中文，不超过 ERR_INLINE_MAX 个字，并且没有 ERR_TRACES 里的任何一种痕迹 */
const ERR_INLINE_MAX = 60; // 按字符数算，英文字母、数字、空格也各算一个。提示框正文一行放 15 个汉字上下，结论加 60 个汉字是五行
const ERR_TRACES = [
  /(?:^|[^A-Za-z0-9])[A-Za-z]:[\\/]/,      // Windows 路径：C:\Users\…、D:/MC（「http://」里的「p:/」不算）
  /[^\s\\「『"'“‘(（]\\[^\s\\」』"'”’)）]/, // 反斜杠隔开的路径：bin\java.exe、世界\region、[1.20.1]\java.exe（单独引着、前后有空格的「\」这个字符不算）
  /(?:^|[^\w.\/\\\u3400-\u9fff-])\/[\w.\u3400-\u9fff-]+\//, // 斜杠开头的路径：/usr/lib/…，冒号、括号后面的也算（「启动 / 停止」「TCP/UDP」「原版/Fabric/Forge」「启用/禁用/删除」不算）
  /[\w.\u3400-\u9fff-]+\/[^\s/]*\.[A-Za-z][A-Za-z0-9]{0,7}\b/, // 带扩展名的相对路径：mods/xxx.jar、配置/服务器.json（「1.20/1.21」这种版本号不算）
  /fork\/exec|exit status|\bi\/o\b/i,       // 外部程序没起来或退出异常（fork/exec、exit status 1）、读写超时（i/o timeout）
  /\b[a-z][a-z0-9+.-]*:\/\/[^\s」』"'”’)）>]/i, // 网址：「://」后面跟着主机名才算，中文域名也算（只提到「http://」「「https://」」这几个字的不算）
  /: /,                                     // Go 拼出来的「xxx: yyy」：给人看的中文用全角冒号
  /\b[a-z]{2,}(?: [a-z]{2,})+\b/,          // 连着两个以上的小写英文单词，就是英文报错句：connection refused、Access is denied
  // 大写开头或全大写的常见英文报错：Bad Gateway、ACCESS DENIED。只认这几句，不对所有英文词放宽大小写，免得误伤 Simple Voice Chat 这类名字
  /\b(?:bad gateway|bad request|not found|forbidden|unauthori[sz]ed|internal server error|service unavailable|gateway time-?out|too many requests|access denied|permission denied|connection (?:refused|reset|closed)|timed out|no such host|broken pipe)\b(?!\.[A-Za-z0-9])/i, // 后面紧跟扩展名的是文件名（「Forbidden.jar」），不算
  /\bEOF\b/,                                // 连接被意外断开：EOF、unexpected EOF
  /\bHTTP\s*\d{3}\b/i,                      // HTTP 状态码
  /\b0x[0-9a-f]{4,}\b/i,                    // 十六进制的系统错误码
  /[\r\n]/,                                 // 好几行的：提示里放不下
];
/* 请求根本没发出去时浏览器给的原文：WebView2 / Chrome 是「Failed to fetch」，Firefox 是「NetworkError when attempting…」，Safari 是「Load failed」。
   还得同时是 TypeError：同一个 try 里代码自己出错（读了 undefined 的属性）也是 TypeError，但原文对不上 */
const ERR_OFFLINE = /^(?:Failed to fetch|NetworkError when attempting to fetch resource|Load failed)\b/i;
/* 前端自己编的兜底词：后端没给原因时才用得上（api() 的「未知错误」，裸 fetch 的「导入失败」「上传失败」「添加失败」，登录框的「登录失败」）。
   原文正好是其中一个时只写结论，不然会拼出「无法导入整合包：导入失败」。只认整句相等，不做「X失败」这类模糊匹配。
   「需要登录」不在里面：它也是后端 401 自己说的话（internal/web/server.go），是有用的原因 */
const ERR_FALLBACKS = new Set(["未知错误", "导入失败", "上传失败", "添加失败", "登录失败"]);

function errSummary(what, e) {
  const raw = e == null ? "" : String(typeof e === "object" ? e.message || e : e);
  const msg = raw.trim();
  const typeErr = !!e && typeof e === "object" && (e instanceof TypeError || e.name === "TypeError");
  const say = (e && typeof e === "object" && typeof e.hint === "string" && e.hint.trim()) || msg; // 要接的那句：后端另给的人话优先
  let text = what;
  if (typeErr && ERR_OFFLINE.test(msg)) text = what + "：连不上 MineDeck";
  else if (say && say !== what && /[\u3400-\u9fff]/.test(say) && [...say].length <= ERR_INLINE_MAX && !ERR_TRACES.some(re => re.test(say)) && !ERR_FALLBACKS.has(say))
    text = what + (say.includes("：") ? "。" : "：") + say;
  return { text, raw: raw || what };
}
/* 原文放进复制键的 data-err-raw：转义以后原样保留；回车符写成字符引用，不然 HTML 解析时 \r\n 会被并成 \n */
const errRawAttr = raw => esc(raw).replace(/\r/g, "&#13;");
/* 报错提示：正文按 errSummary 拼，「详情」里是完整原文。opts 同 toast（actions：修复的去处，提示上和「详情」里各一份）。
   返回提示的元素（同 toast） */
function toastErr(what, e, opts) {
  const { text, raw } = errSummary(what, e);
  return toast(text, true, raw, opts);
}
/* 页面报错框（.err-box）：一句结论（拼法同 toastErr），下面「复制报错信息」，复制完整原文。返回 HTML，复制键的点击由下面的委托统一接。
   hint：下一步怎么办，纯文本，单独一行（「可在「设置 → 下载与网络」换一个下载源后重试。」）；
   actions：和复制键排在一行、放在它前面的按钮（HTML，调用方负责转义），如「重试」；id：报错框自己的 id；
   label：同一页可能同时出几个报错框时，复制键的名字带上是哪一块（「复制报错信息 前置检查」），不然读屏和无障碍测试分不开 */
function errBox(what, e, { hint = "", actions = "", id = "", label = "" } = {}) {
  const { text, raw } = errSummary(what, e);
  return `<div class="err-box"${id ? ` id="${esc(id)}"` : ""}>${esc(text)}${hint ? `<div>${esc(hint)}</div>` : ""}
    <div class="err-acts">${actions}<button type="button" class="btn sm ghost" data-err-raw="${errRawAttr(raw)}"${label ? ` aria-label="复制报错信息 ${esc(label)}"` : ""}>${icon("copy")} 复制报错信息</button></div></div>`;
}
/* 一行字里的报错（摘要行、测试结果、检查更新的结果）：警告图标 + 正文（拼法同 toastErr）+ 只有图标的「复制报错信息」。返回 HTML。
   label：同一页可能同时有几处时，复制键的名字带上是哪一处 */
function errInline(what, e, { label = "" } = {}) {
  const { text, raw } = errSummary(what, e);
  const name = esc("复制报错信息" + (label ? " " + label : ""));
  return `<span class="err-inline">${icon("warn")}<span>${esc(text)}</span><button type="button" class="btn sm ghost icon-only" data-err-raw="${errRawAttr(raw)}" aria-label="${name}" title="${name}">${icon("copy")}</button></span>`;
}
document.addEventListener("click", ev => {
  const b = ev.target instanceof Element ? ev.target.closest("[data-err-raw]") : null;
  if (b) copy(b.getAttribute("data-err-raw"), "已复制报错信息");
});

function fmtBytes(n) {
  if (n <= 0) return "";
  const u = ["B", "KB", "MB", "GB"];
  let i = 0;
  while (n >= 1024 && i < 3) { n /= 1024; i++; }
  return n.toFixed(i ? 1 : 0) + " " + u[i];
}
/* 按钮写出后果（如「保留 / 删除服务器」），不读正文也能作答；危险操作默认聚焦「取消」，误按回车不会直接执行。
   危险确认框：标题前带危险色图标瓦片（icon 指定图标），最终键为实底红。遮罩淡入、对话框上浮放大进场，离场更短 */
/* 确认框叠在已有的弹窗上面（见 modalLayer），返回的 Promise 上带着这一层（.el）：调用方要在框里找东西时从这一层找，
   下面可能还叠着别的框，直接查 #modal-root 会找到下面那个的。框里有要填的东西时点遮罩不关。 */
function confirmModal({ title, body, okText = "确定", cancelText = "取消", danger = false, extra = "", icon: ic = "warn" }) {
  let mask;
  const p = new Promise(resolve => {
    const prev = document.activeElement; // 关闭后归还焦点
    const tid = layerId("modal-title");
    mask = modalLayer(`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="${tid}">
      <h3 id="${tid}">${danger ? `<span class="m-ico">${icon(ic)}</span>` : ""}<span>${esc(title)}</span></h3><div class="m-body">${body}</div>${extra}
      <div class="m-actions">
        <button class="btn" data-x="cancel">${esc(cancelText)}</button>
        <button class="btn ${danger ? "danger solid" : "primary"}" data-x="ok">${esc(okText)}</button>
      </div></div>`);
    let closed = false;
    const close = val => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      dismiss(mask);
      if (prev && prev.focus && prev.isConnected) prev.focus();
      resolve(val);
    };
    const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(null); };
    document.addEventListener("keydown", onKey);
    mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget && !hasFields(mask)) close(null); });
    mask.querySelector('[data-x="cancel"]').onclick = () => close(null);
    mask.querySelector('[data-x="ok"]').onclick = () => {
      // 表单的值在关弹窗之前读出来交回去：开了「减少动态」时弹窗是立刻移除的，关掉以后再查 DOM 就什么也拿不到
      const checks = {};
      mask.querySelectorAll("input[type=checkbox][data-k]").forEach(c => checks[c.dataset.k] = c.checked);
      mask.querySelectorAll("select[data-k]").forEach(c => checks[c.dataset.k] = c.value);
      close(checks);
    };
    mask.querySelector(danger ? '[data-x="cancel"]' : '[data-x="ok"]').focus();
  });
  p.el = mask;
  return p;
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
  chevronUp: '<path d="M4 9.5 8 5.5l4 4"/>',
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
/* 退出 MineDeck：先确认，再请后端保存世界、停掉服务器、退出。侧栏底部和「设置 → 更新与关于」两处共用（可用性走查 R17：
   开了「点关闭按钮时缩到托盘」以后点 × 不退出，以前窗口里唯一的退出藏在「更新与关于」最底下） */
async function quitApp() {
  const r = await confirmModal({ title: "退出 MineDeck？", body: "会先保存世界，停掉正在运行的服务器，然后退出程序。", okText: "退出", danger: true, icon: "power" });
  if (!r) return;
  try { await api("/api/shutdown", { method: "POST" }); document.body.innerHTML = `<div style="padding:80px;text-align:center;color:var(--muted)">MineDeck 正在停止服务器并退出，可以关闭此页面了。</div>`; }
  catch (e) { toastErr("无法退出 MineDeck", e); }
}
(function initQuit() {
  const b = $("#sb-quit");
  if (!b) return;
  b.innerHTML = icon("power") + "<span>退出 MineDeck</span>";
  b.onclick = quitApp;
})();
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

/* 外部链接交给系统默认浏览器（只在原生窗口里）：界面上指向别的网站的 http / https 链接（EULA、申请 CurseForge Key、
   openfrp.net、AI 回答里的链接……），左键、Ctrl / Shift 点、中键、键盘回车，还有脚本里的 window.open，都拦下来交给宿主函数
   hostOpenURL，由宿主用系统默认浏览器打开（宿主那边再查一遍，只认 http / https，见 openurl_windows.go）。
   不拦的话 WebView2 会用自带的简易窗口打开，用不了自己浏览器里的密码管理器、扩展和登录状态。
   不带 target 的外部链接也拦，主窗口不会被带到别的网站去。MineDeck 自己的地址（同源）和别的协议照旧。
   只在宿主给了 hostOpenURL 时启用：局域网里的浏览器、没有 WebView2 时用浏览器打开界面，都按浏览器自己的规矩开。 */
(function initExternalLinks() {
  if (typeof window.hostOpenURL !== "function") return;
  // 交给宿主的是浏览器整理过的绝对地址（中文、空格都已转成 %XX）；不是别的网站的 http / https 地址时给空串
  const externalURL = href => {
    let u;
    try { u = new URL(href, location.href); } catch { return ""; }
    return (u.protocol === "http:" || u.protocol === "https:") && u.origin !== location.origin ? u.href : "";
  };
  const fail = e => toastErr("没能用浏览器打开链接", e);
  const openExternal = url => { try { Promise.resolve(window.hostOpenURL(url)).catch(fail); } catch (e) { fail(e); } };
  window.__mdOpenURLFailed = fail; // 宿主在后台打开浏览器，打不开（很少见）时回头调这里
  const onLink = e => {
    // click 只接左键（在链接上按回车也算左键）；auxclick 只接中键，右键不管
    if (e.defaultPrevented || e.button !== (e.type === "auxclick" ? 1 : 0)) return;
    const a = e.target instanceof Element ? e.target.closest("a[href], area[href]") : null;
    // SVG 里的 <a> 的 href 不是字符串（SVGAnimatedString），取它写着的原文，再按页面地址补全
    const href = !a ? "" : typeof a.href === "string" ? a.href : a.getAttribute("href") || "";
    const url = href ? externalURL(href) : "";
    if (!url) return;
    e.preventDefault(); // WebView2 就不会自己开窗口，也不会在主窗口里跳过去
    openExternal(url);
  };
  // 在捕获阶段接：页面里别处 stopPropagation 也挡不住
  document.addEventListener("click", onLink, true);
  document.addEventListener("auxclick", onLink, true);
  const nativeOpen = window.open;
  window.open = function (url) {
    const ext = url == null ? "" : externalURL(String(url));
    if (!ext) return nativeOpen.apply(window, arguments);
    openExternal(ext);
    return null; // 没有窗口对象可给，跟弹窗被浏览器拦下时一样
  };
})();

/* 拖进窗口的文件和链接（3.1.1 起，只在窗口版）：没地方接的，页面自己接下来、标成「不能放」。
   不接的话 WebView2 会另开一个自带的小窗把拖进来的网页或文件打开，和外部链接以前弹的是同一种窗口
   （主窗口不动：3.1.1 发版前拿正式版 3.1.0 在真窗口里对照过）。
   dragover 时 dropEffect 设成 none：浏览器按不能放处理，松手后不发 drop，也不另开窗口。
   接拖放的地方（待装清单的「从电脑添加」）自己先 preventDefault，这里不管；文字、链接拖进输入框照旧放得进去，文件不行。 */
(function guardDrops() {
  if (typeof window.hostWinClose !== "function") return;
  const intoText = e => !(e.dataTransfer && [...(e.dataTransfer.types || [])].includes("Files")) &&
    e.target instanceof Element && !!e.target.closest('input, textarea, [contenteditable]:not([contenteditable="false"])');
  const guard = e => {
    if (e.defaultPrevented || intoText(e)) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "none";
  };
  window.addEventListener("dragover", guard);
  window.addEventListener("drop", guard);
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
    <div class="m-body">退出 MineDeck 会先停止${running > 1 ? "它们" : "它"}并保存世界。${tray ? "选择最小化到托盘时，服务器继续运行，点托盘图标恢复窗口。" : "托盘图标不可用，这次只能停止服务器并退出。"}</div>
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
  const onKey = e => { if (e.key === "Escape" && topLayer(d)) close(); }; // 叠着别的弹窗时只关最上面那层
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
      } catch (e) { toastErr("没能记住这个选择", e); }
    }
    try { window.hostWinHide(); } catch (e) {}
  };
  (toTray || d.querySelector('[data-x="cancel"]')).focus();
}

/* 快捷键：1-5 切换侧栏导航；T 或 / 聚焦控制台输入（MC 聊天习惯） */
document.addEventListener("keydown", e => {
  if (e.ctrlKey || e.altKey || e.metaKey) return;
  const t = e.target;
  const typing = t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT");
  if (typing) return;
  if ($("#modal-root").childElementCount || $("#login-mask")) return; // 弹层打开时不在底下切页
  if (["1", "2", "3", "4", "5"].includes(e.key)) {
    const target = ["#/instances", "#/create", "#/tunnels", "#/toolbox", "#/settings"][+e.key - 1];
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
    else if (v > 16384) note = ` <span class="mem-warn">${icon("warn")} 分配内存过大可能加剧 Java 垃圾回收（GC）停顿，导致游戏周期性掉帧。</span>`;
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
   save() 失败时抛错（由这里统一 toast），成功后以这次提交出去的快照为基线（见 wireSaveBar）。 */
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
    try { await save(input.value); base = input.value; } catch (e) { if (!e.silent) toastErr("保存失败", e); } // silent：错误已就近显示在字段下方
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
/* 基线只认这次实际提交出去的东西（2026-11-16 Codex 复审 beta.3）：点「保存」时先拍下 sent = read()，交给 save(sent)；
   成功以后基线换成 sent，不再是请求回来时表单的样子——请求途中又改的、又加减的定时任务，后台没收到，得继续算「未保存」，
   离开时照样拦。save 自己按服务器的回话改写了表单（清空填过的 Key、回显后端补回的参数、换上 Java 以后的相对值）的，
   返回改写以后应有的基线（一个对象，键同 read()）；返回 false 表示用户取消了，表单仍算没保存。
   保存途中「保存」键一直置灰：两次保存叠在一起，回来的先后不定，基线就说不准了。
   save 的第二个参数是当时的基线 base：只想提交改过的项的（server.properties），拿它和 sent 比，和保存条数的是同一份比较 */
function wireSaveBar(root, gen, read, save) {
  const bar = $(".save-bar.sticky", root);
  const saveBtn = $('[data-sb="save"]', bar);
  let base = read(), saving = false;
  const diff = () => {
    const cur = read();
    return [...new Set([...Object.keys(base), ...Object.keys(cur)])].filter(k => (base[k] ?? "") !== (cur[k] ?? "")).length;
  };
  const paint = () => {
    const n = diff();
    const wasDirty = bar.classList.contains("dirty");
    bar.classList.toggle("dirty", n > 0);
    $(".sb-text", bar).textContent = n ? `${n} 项修改未保存` : "所有修改已保存";
    saveBtn.disabled = n === 0 || saving;
    const discard = $('[data-sb="discard"]', bar);
    discard.hidden = n === 0;
    if (n > 0 && !wasDirty) playClass(discard, "swap-in"); // 「放弃修改」出现时轻轻换场
  };
  root.addEventListener("input", paint);
  root.addEventListener("change", paint);
  $('[data-sb="discard"]', bar).onclick = () => { dirtyCheck = null; navigate(); }; // 按服务器上的当前值重绘
  saveBtn.onclick = async () => {
    if (saving) return;
    saving = true;
    saveBtn.disabled = true;
    const sent = read();
    try {
      const r = await save(sent, base);
      if (r !== false && bar.isConnected) base = r && typeof r === "object" ? r : sent; // false：用户取消了，表单仍算没保存
    } catch (e) { toastErr("保存失败", e); }
    saving = false;
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

/* 后台任务记录（服务器列表页和新建页共用，可用性走查 R13）：只把要人看一眼的摆出来——进行中的、还有没装上的文件的、
   失败或带提示还没收起的；顺利完成的、取消的、点过「收起」的折成一行「另外 N 个已结束的任务」，点开才列出来，
   不再占服务器列表的首屏（A10：5 条已完成的记录把列表挤到了第二屏）。「收起」只记在这一次打开的窗口里（sessionStorage） */
const TASK_HIDE_KEY = "md_task_hidden";
const taskHidden = (() => { try { return new Set(JSON.parse(sessionStorage.getItem(TASK_HIDE_KEY) || "[]")); } catch { return new Set(); } })();
const taskHideKey = t => t.id + "|" + t.title; // 程序重开以后任务编号从 t1 重新数：带上标题，免得把新任务当成收起过的
const taskState = t => !t.ended ? (t.cancelling ? "正在取消，清理半成品…" : t.label || "进行中")
  : t.cancelled ? "已取消" : t.error ? "失败"
  : t.pendingCount ? `已完成，${t.pendingCount} 个文件待处理`
  : t.warningCount ? `已完成，${t.warningCount} 条提示` : "已完成";
const taskNeedsLook = t => !t.ended || t.pendingCount > 0 || (!t.cancelled && (!!t.error || t.warningCount > 0) && !taskHidden.has(taskHideKey(t)));
async function refreshTaskList(slot, gen) {
  if (stale(gen) || !slot.isConnected || slot._loading) return;
  slot._loading = true;
  try {
    const list = await api("/api/tasks");
    if (stale(gen) || !slot.isConnected) return;
    const row = t => {
      const go = !t.ended ? "查看进度" : t.pendingCount ? "去处理" : "查看结果";
      const goLabel = !t.ended ? "查看进度" : t.pendingCount ? "去处理没装上的文件" : "查看结果";
      const canHide = t.ended && !t.pendingCount && taskNeedsLook(t);
      return `<div class="java-row task-row${t.cancelled ? " cancelled" : ""}" data-task="${esc(t.id)}"><div style="flex:1;min-width:0"><b>${esc(t.title)}</b><div class="sub">${esc(taskState(t))}</div></div>
        <a class="btn sm" href="#/task/${encodeURIComponent(t.id)}" aria-label="${goLabel} ${esc(t.title)} ${esc(t.id)}">${go}</a>${canHide
          ? `<button type="button" class="btn sm ghost icon-only" data-hide-task="${esc(taskHideKey(t))}" title="收起这条记录" aria-label="收起 ${esc(t.title)} ${esc(t.id)}">${icon("x")}</button>` : ""}</div>`;
    };
    const shown = list.filter(taskNeedsLook), folded = list.filter(t => !taskNeedsLook(t));
    const cnt = (f, w) => { const n = folded.filter(f).length; return n ? `${n} 个${w}` : ""; };
    const parts = [cnt(t => !t.error, "已完成"), cnt(t => t.cancelled, "已取消"), cnt(t => t.error && !t.cancelled, "失败")].filter(Boolean);
    const html = list.length ? `<h2>后台任务</h2>` + shown.map(row).join("") + (folded.length
      ? `<details class="task-fold"${slot._foldOpen ? " open" : ""}><summary>${shown.length ? "另外 " : ""}${parts.length === 1 ? parts[0] + "的任务" : `${folded.length} 个已结束的任务（${parts.join("，")}）`}</summary>
        <div class="task-fold-body">${folded.map(row).join("")}</div></details>` : "") : "";
    if (slot._html !== html) {
      slot.innerHTML = html; slot._html = html;
      const fold = slot.querySelector(".task-fold");
      if (fold) fold.ontoggle = () => { slot._foldOpen = fold.open; };
      slot.querySelectorAll("[data-hide-task]").forEach(b => b.onclick = () => {
        taskHidden.add(b.dataset.hideTask);
        try { sessionStorage.setItem(TASK_HIDE_KEY, JSON.stringify([...taskHidden])); } catch { /* 存不了就只在这一页记着 */ }
        refreshTaskList(slot, gen);
      });
    }
    slot.hidden = !list.length;
  } catch (e) {
    if (!stale(gen) && slot.isConnected && slot._html === undefined) {
      slot.hidden = false;
      slot.innerHTML = `暂时无法获取后台任务，稍后自动重试 <button class="btn sm ghost" data-copy-task-err>${icon("copy")} 复制报错信息</button>`;
      const b = slot.querySelector("[data-copy-task-err]");
      if (b) b.onclick = () => copy(String((e && e.message) || e), "已复制报错信息");
    }
  } finally { slot._loading = false; }
}

/* 未保存修改与进行中操作守卫：表单类视图登记 dirtyCheck（切页 / 切 tab / 刷新前确认），分批提交登记到 leaveChecks（只拦站内切页，不拦关窗口和刷新） */
let dirtyCheck = null;
const leaveChecks = new Set();
const registerLeaveCheck = fn => { leaveChecks.add(fn); return () => leaveChecks.delete(fn); };
let curHash = null;
let guardOpen = false;
window.addEventListener("beforeunload", e => { if (dirtyCheck && dirtyCheck() > 0) { e.preventDefault(); e.returnValue = ""; } });

async function confirmBeforeLeave(hash) {
  const prompts = [];
  for (const fn of leaveChecks) {
    const p = fn();
    if (p) prompts.push(p);
  }
  const unsaved = dirtyCheck ? dirtyCheck() : 0;
  if (unsaved > 0) prompts.push({
    title: "放弃未保存的修改？", danger: true,
    body: `当前页面还有 <b>${unsaved}</b> 项修改没有保存，离开后这些修改会丢失。`,
    okText: `放弃 ${unsaved} 项修改`, cancelText: "继续编辑",
  });
  if (!prompts.length) return true;
  if (curHash && hash !== curHash) history.replaceState(null, "", curHash); // 先退回原地址，确认后再前往
  guardOpen = true;
  try {
    for (const prompt of prompts) {
      if (!(await confirmModal(prompt))) return false;
    }
  } finally {
    guardOpen = false;
  }
  if ((location.hash || "#/instances") !== hash) history.replaceState(null, "", hash);
  return true;
}

/* 详情页的旧地址（beta.3 以前的标签编号）→ 新地址（docs/文案重写/04-标签与术语-beta3.md 第一节，可用性走查 R06）。
   AI 助手的回复、提示、书签里还写着旧的。旧的「地图」标签原来就叫 world，现在就是「世界与备份」本身，不用换：
   落到它的「备份」子页上，旁边就是「换地图」 */
const DETAIL_OLD_TABS = { backups: "world/backups", res: "addons", cart: "addons", common: "settings/common", raw: "settings/raw", jvm: "settings/jvm",
  gameai: "ext/gameai", companion: "ext/companion", music: "ext/music" };
const upgradeDetailHash = hash => {
  const m = hash.match(/^#\/inst\/([^/]+)\/([a-z]+)$/);
  return m && DETAIL_OLD_TABS[m[2]] ? `#/inst/${m[1]}/${DETAIL_OLD_TABS[m[2]]}` : hash;
};

async function navigate() {
  // 旧地址原地换成新地址：用 replaceState，不多一条历史记录，「后退」不会又回到旧地址、再被换一次
  const old = location.hash || "#/instances", hash = upgradeDetailHash(old);
  if (hash !== old) history.replaceState(null, "", hash);
  if (guardOpen) { if (curHash) history.replaceState(null, "", curHash); return; }
  if (!(await confirmBeforeLeave(hash))) return;
  dirtyCheck = null;
  leaveChecks.clear();
  appearanceView = null;
  curHash = hash;
  viewGen++;
  if (consoleES) { consoleES.close(); consoleES = null; }
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  if (prTimer) { clearTimeout(prTimer); prTimer = null; }
  document.querySelectorAll(".dropmenu").forEach(x => x._close ? x._close(true) : x.remove());
  document.querySelectorAll(".modal-mask").forEach(x => { if (x._closeOnNav) x._closeOnNav(); });
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
  if (view === "create") renderCreate(arg);
  else if (view === "inst" && arg) { const sl = arg.indexOf("/"); sl >= 0 ? renderDetail(arg.slice(0, sl), arg.slice(sl + 1)) : renderDetail(arg); }
  else if (view === "settings") renderSettings(arg);
  else if (view === "tunnels") renderTunnels();
  else if (view === "toolbox") MDToolbox.render(arg);
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

/* ---------- 视图：实例列表 ---------- */
/* 时长秒 → 2h13m / 5m / 45s */
function fmtDur(sec) {
  sec = Math.max(0, sec | 0);
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60);
  if (h) return `${h}h${m}m`;
  if (m) return `${m}m`;
  return `${sec}s`;
}

/* 「打开文件夹」菜单项：按核心类型列出可用子目录（实例卡「···」与详情页「打开目录」共用）。
   「备份」不在服务器文件夹里（在备份文件夹下面，R15），走打开任意文件夹的接口；还没备份过时那个文件夹还不存在，说清楚 */
function folderItems(info) {
  const isProxy = info.kind === "proxy";
  const hasMods = ["fabric", "forge", "neoforge", "mohist", "banner"].includes(info.core);
  const hasPlugins = ["paper", "purpur", "leaves", "folia", "mohist", "banner", "velocity", "waterfall"].includes(info.core);
  const dirs = [["", "folder", "服务器文件夹"]];
  if (hasMods) dirs.push(["mods", "package", "模组"]);
  if (hasPlugins) dirs.push(["plugins", "plug", "插件"]);
  if (!isProxy) dirs.push(["world", "globe", "世界"], ["logs", "doc", "日志"], ["crash-reports", "warn", "崩溃报告"]);
  else dirs.push(["logs", "doc", "日志"]);
  const items = dirs.map(([sub, ic, label]) => ({
    icon: ic, label,
    onClick: () => api(`/api/instances/${encodeURIComponent(info.name)}/opendir${sub ? "?sub=" + encodeURIComponent(sub) : ""}`, { method: "POST" })
      .catch(e => toastErr("无法打开文件夹", e)),
  }));
  if (!isProxy && info.backupDir) items.push({ icon: "camera", label: "备份", onClick: () => openBackupDir(info) });
  return items;
}
/* 打开这台服务器的备份文件夹（R15）：还没备份过时文件夹还不存在，不报错，说一句什么时候会有 */
function openBackupDir(info) {
  return api("/api/openpath", { method: "POST", body: { path: info.backupDir } }).catch(e => {
    // 「在这里」：改过备份存放文件夹的话，以前的备份在原来的文件夹里，不能说它从没备份过
    if (/不存在/.test(String(e && e.message))) toast(`「${info.name}」在这个文件夹里还没有备份过。第一次备份以后，备份会放在 ${info.backupDir}`);
    else toastErr("无法打开备份文件夹", e);
  });
}

/* 移出列表 / 删除服务器（可用性走查 R05、U28、U30）。
   默认只移出列表：「同时删除服务器文件夹」对所有服务器默认关，关着时主按钮叫「移出列表」，文件全留着（后端把 instance.json
   改名成 instance.json.bak，下次启动不会再冒出来），以后能用「接管已有服务器」加回来。
   打开开关才是删除：主按钮写「删除服务器和文件夹」，还要再输一遍服务器名，对上了才能点。取消键叫「取消」，默认聚焦它。
   接管来的服务器，文件夹是用户自己原来的：写出是哪个文件夹，以及移出后里面会留下哪些 MineDeck 写的文件（info.mdFiles，后端按实际在的列）。
   移出以后的提示写出文件夹在哪，带「打开文件夹」（本机才有；走现成的打开文件夹接口，它只许本机调用）。 */
async function deleteInstance(info) {
  const name = info.name;
  const okText = files => files ? "删除服务器和文件夹" : "移出列表";
  const titleOf = files => files ? `删除「${name}」和它的文件夹？` : `把「${name}」移出列表？`;
  const left = info.adopted ? [...(info.mdFiles || [])] : [];
  const leftNote = info.adopted ? `<div class="del-left">移出后，原文件夹里会留下 MineDeck 写的这些文件：<code>instance.json.bak</code>（MineDeck 的设置，以后接管时会读回来）${left.map(f => `、<code>${esc(f)}</code>`).join("")}。${
    left.includes("run.before-minedeck.bat") ? "原来的启动脚本在 <code>run.before-minedeck.bat</code> 里，想换回去就删掉 <code>run.bat</code>、把它改回这个名字。" : ""}${
    left.includes("user_jvm_args.before-minedeck.txt") ? "原来的 <code>user_jvm_args.txt</code> 在 <code>user_jvm_args.before-minedeck.txt</code> 里。" : ""}不想要的可以自己删掉。</div>` : "";
  const p = confirmModal({
    title: titleOf(false), danger: true, okText: okText(false), cancelText: "取消", icon: "trash",
    body: `只从 MineDeck 的列表里拿掉，服务器文件夹和世界都留在原处：<div class="del-dir mono">${esc(info.dir || "")}</div>以后可以用「新建服务器 → 接管已有服务器」把它加回来。已有的世界备份不受影响，仍在备份文件夹里。${leftNote}`,
    extra: `<label class="switch" style="margin-bottom:4px"><input type="checkbox" data-k="files"><span class="sw"></span>
        <span class="sw-label">同时删除服务器文件夹（含世界，无法恢复）</span></label>
      <div class="hint" style="margin:0 0 16px 48px">${info.adopted ? `这台服务器是接管进来的，文件夹是你原来的那个：${esc(info.dir)}<br>` : ""}不打开时只从列表移出，文件留在磁盘上；以后可以用「新建服务器 → 接管已有服务器」把它加回来。</div>
      <div class="del-confirm" hidden><label class="field"><span>要删除，请输入服务器名「${esc(name)}」</span>
        <input type="text" data-k="confirm-name" autocomplete="off" spellcheck="false" aria-label="输入服务器名确认删除"></label></div>`,
  });
  // 按钮上的字、标题跟着开关变；打开以后要输对名字才能点（在这一层里找，下面可能还叠着别的框）
  const m = p.el, box = m.querySelector("input[data-k=files]"), ok = m.querySelector("[data-x=ok]"), confirmBox = m.querySelector(".del-confirm"),
    typed = m.querySelector("input[data-k=confirm-name]"), title = m.querySelector("h3 > span:last-child");
  const paint = () => {
    const files = box.checked;
    ok.textContent = okText(files);
    ok.className = "btn " + (files ? "danger solid" : "primary"); // 只移出列表不删东西：不用红色的危险键
    title.textContent = titleOf(files);
    confirmBox.hidden = !files;
    ok.disabled = files && typed.value.trim() !== name;
  };
  box.addEventListener("change", () => { paint(); if (box.checked) typed.focus(); });
  typed.addEventListener("input", paint);
  typed.addEventListener("keydown", e => { if (e.key === "Enter" && !ok.disabled) ok.click(); });
  paint();
  const r = await p;
  if (!r) return false;
  // 开关开着、名字却没对上（按钮本来点不了，回车也拦了）：什么都不做，不悄悄降成「移出列表」
  const files = r.files && typed.value.trim() === name;
  if (r.files && !files) return false;
  try {
    const res = await api(`/api/instances/${encodeURIComponent(name)}?files=${files ? 1 : 0}`, { method: "DELETE" });
    if (files) toast(`已删除「${name}」和它的文件夹`);
    else {
      const dir = info.dir || "";
      const local = await api("/api/app").then(a => a.local !== false).catch(() => true); // 从局域网打开时打不开主机上的文件夹
      toast(`已把「${name}」移出列表。文件夹还在：${dir}`, false, undefined, {
        actions: dir && local ? [{ label: "打开文件夹", aria: `打开「${name}」的文件夹`,
          onClick: () => api("/api/openpath", { method: "POST", body: { path: dir } }).catch(e => toastErr("无法打开文件夹", e)) }] : [],
      });
    }
    // 服务器删了，这台的待装清单却没清掉（后端读不出、也记不下来）：删服照常算成功，另给一条带「详情」的提示，
    // 免得以后同名新建的那台冒出上一台的东西时不知道是从哪来的
    if (res && res.note) toast(`「${name}」待装清单里的东西没清掉，以后再建同名的服务器时看一眼它的待装清单`, true, res.note);
    return true;
  } catch (e) { toastErr(files ? "无法删除服务器" : "无法把服务器移出列表", e); return false; }
}

/* 运行状态：像素状态灯 + 文字（不能只靠颜色） */
const ST_TEXT = { running: "运行中", starting: "启动中…", stopping: "停止中…", stopped: "已停止" };
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
      <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="inst-q" placeholder="搜索服务器名称" aria-label="搜索服务器名称"></div>
      <select id="inst-sort" aria-label="排序方式"><option value="recent">排序：最近创建</option><option value="running">排序：运行中优先</option><option value="name">排序：名称</option></select>
    </div></div>
    <section id="dup-dirs" hidden style="margin-bottom:24px"></section>
    <section id="task-list" hidden style="margin-bottom:24px"></section>
    <section id="creating-left" hidden style="margin-bottom:24px"></section>
    <div id="inst-cards">${skeletonCards(4)}</div>`;
  const paintDuplicates = dups => {
    const box = $("#dup-dirs");
    if (!box) return;
    if (!dups.length) { box.hidden = true; box.innerHTML = ""; box._html = ""; return; }
    const html = `<div class="warn-card"><div class="wc-title">${icon("warn")} 有 ${dups.length} 个服务器文件夹因为重名没有列出来</div>
      <ul class="wc-list">${dups.map(d => `<li>文件夹 <code class="mono">${esc(d.dir)}</code> 里的服务器和列表里的「<b>${esc(d.name)}</b>」${d.keptDir ? `（<code class="mono">${esc(d.keptDir)}</code>）` : ""}重名，没有列出来。</li>`).join("")}</ul>
      <div class="wc-note">要把两台都显示出来，请改掉其中一台的名字（打开没列出来的那个文件夹里的 <code>instance.json</code>，把 <code>"name"</code> 改成别的名字后重开 MineDeck；或者先把列表里这台移出列表，再用别的名字接管回来，然后重开 MineDeck）。</div></div>`;
    box.hidden = false;
    if (box._html !== html) { box.innerHTML = html; box._html = html; }
  };
  const paintLeftovers = async () => {
    const box = $("#creating-left");
    if (!box) return;
    let data;
    try { data = await api("/api/creating-leftovers"); } catch { if (box.isConnected) box.hidden = true; return; }
    if (stale(gen) || !box.isConnected) return;
    const items = data.items || [];
    if (!items.length) { box.hidden = true; box.innerHTML = ""; return; }
    box.hidden = false;
    box.innerHTML = `<div class="warn-card"><div class="wc-title">${icon("warn")} 有 ${items.length} 个上次创建没完成的文件夹</div>
      <div class="wc-note">这里只列出创建中途停下、还没建好的文件夹。接管进来的和你自己放进来的不会出现在这里，也不会自动删除。</div>
      ${items.map(it => `<div class="row" style="margin-top:8px"><b>${esc(it.name)}</b><span class="hint">${it.hasWorld ? "含世界文件 · " : ""}位于「${esc(it.root)}」</span>
        <button type="button" class="btn sm ghost" data-drop-left="${esc(it.id)}" data-world="${it.hasWorld ? "1" : "0"}" data-name="${esc(it.name)}" data-root="${esc(it.root)}" aria-label="删除没建完的文件夹 ${esc(it.name)}（${esc(it.root)}）">删除这个文件夹</button></div>`).join("")}</div>`;
    box.querySelectorAll("[data-drop-left]").forEach(b => b.onclick = async () => {
      const ok = await confirmModal({
        title: "删除没建完的文件夹？",
        body: `<p>将删除位于「${esc(b.dataset.root)}」的「${esc(b.dataset.name)}」。${b.dataset.world === "1" ? "里面的世界文件会一起删掉，无法恢复。" : "里面的文件会一起删掉。"}已建好的服务器和其他文件夹不会受影响。</p>`,
        okText: "删除文件夹", danger: true,
      });
      if (!ok) return;
      b.disabled = true;
      try {
        await api("/api/creating-leftovers/discard", { method: "POST", body: { id: b.dataset.dropLeft, confirm: true } });
        toast("已删除没建完的文件夹");
        if (!stale(gen)) paintLeftovers();
      } catch (e) {
        toastErr("无法删除这个文件夹", e);
        if (stale(gen)) return;
        if (b.isConnected) b.disabled = false;
        paintLeftovers(); // 这一行可能已经变了（别处删掉了、开始建了）：重新取一次列表，不让人接着拿旧的这一行再点
      }
    });
  };
  paintLeftovers();
  const taskList = $("#task-list");
  let lastSig = "", curList = [], prevByName = null; // prevByName：上次渲染时各实例的数据（判定状态切换、播切换动效）
  // 连接地址用的本机局域网 IP（见 pickLanIP）。列表不等 /api/app：拿到之前、或者拿不到时，卡片上只写端口、不给复制，拿到了再重画
  let lanIP = pickLanIP(knownIPs || []);
  const appInfo = api("/api/app").catch(() => ({}));
  const cardHTML = (i, n, enter, prev) => {
    const proxy = i.kind === "proxy";
    const gb = (i.xmxMb / 1024).toFixed(1) + " GB";
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
    // 端口旁边的复制键复制整个连接地址（R01）；代理端的端口第一次启动、生成配置文件以后才知道，之前不写
    const port = connectPortOf(i), addr = lanIP && port ? `${lanIP}:${port}` : "";
    const portKV = port ? `<span>端口<b>${port}</b>${addr ? `<button type="button" class="btn sm ghost icon-only copy-addr" data-act="copy" data-addr="${esc(addr)}" title="复制连接地址 ${esc(addr)}" aria-label="复制连接地址 ${nm}">${icon("copy")}</button>` : ""}</span>` : "";
    // 上次没启动起来：常驻在卡片上，下次启动成功才消失（R03）。原因按报错的规矩写：看得懂的整句接上，带路径的只写结论、原文在悬停提示里
    // 配的 Java 找不到了（R21）：常驻一行，点了去「设置 → 启动与内存」换。这是根本原因，「上次没能启动：找不到 Java」那一行就不再写一遍
    const fail = i.javaMissing
      ? `<a class="inst-fail inst-java-missing" href="${href}/settings/jvm" title="${esc(i.javaPath || "")}" aria-label="找不到 Java，去换一个 ${nm}">${icon("warn")}<span>找不到这台服务器配的 Java，去「设置 → 启动与内存」换一个</span></a>`
      : i.lastStartError && i.status === "stopped"
      ? `<div class="inst-fail" title="${esc(i.lastStartError)}">${icon("warn")}<span>${esc(i.startPortBusy && i.startPortBusy.owner
        ? `上次没能启动：端口 ${i.startPortBusy.port} 被你的服务器「${i.startPortBusy.owner}」占着` // R08：名字可能很长，不按 60 字的规矩截成结论
        : errSummary("上次没能启动", { message: i.lastStartError }).text)}</span></div>` : "";
    return `
    <div class="card st-${i.status}${enter ? " item-enter" : ""}" style="--i:${n}" data-name="${esc(i.name)}">
      <div class="inst-head">${avatarOf(i.core)}<div class="inst-title"><a class="inst-name" href="${href}">${esc(i.name)}</a><div class="inst-sub">${proxy ? "群组代理" : "游戏服务器"}</div></div>${stPill(i.status)}</div>
      <div class="badges">${coreBadge(i.core)}<span class="badge">${proxy ? "v" : "MC "}${esc(i.mc)}</span></div>
      <div class="kv inst-config">${portKV}<span>内存<b>${gb}</b></span><span>Java<b>${i.javaMajor}</b></span></div>
      ${fail}
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
    if (!list.length) { $("#inst-cards").innerHTML = `<div class="empty" style="padding:40px 0">没有名称包含「${esc(q)}」的服务器</div>`; return; }
    $("#inst-cards").innerHTML = `<div class="cards">` + list.map((i, n) => cardHTML(i, n, enter, prev && prev.get(i.name))).join("") + `</div>`;
    animateChanges(prev);
    $("#inst-cards").querySelectorAll(".inst-meta").forEach(el => { if (el.dataset.motd) renderMotd(el, el.dataset.motd); });
    $("#inst-cards").querySelectorAll("[data-act]").forEach(b => b.onclick = async ev => {
      ev.stopPropagation();
      const name = b.closest(".card").dataset.name, act = b.dataset.act;
      const info = curList.find(x => x.name === name);
      if (act === "copy") { copyFlash(b, b.dataset.addr, "已复制连接地址：" + b.dataset.addr); return; }
      if (act === "more") {
        dropMenu(b, [
          { header: "打开文件夹" }, ...folderItems(info), { sep: true },
          ...instMoreItems(info, () => { lastSig = ""; draw(); }), // 详情页头的「···」也是这两项（R06）
        ]);
        return;
      }
      try {
        if (act === "start") { b.disabled = true; await api(`/api/instances/${encodeURIComponent(name)}/start`, { method: "POST" }); toast(`「${name}」正在启动`); }
        else if (act === "stop") {
          if (!(await confirmKick(info, "停止"))) return; // 有玩家在线先问一声（R07，同详情页头）
          b.disabled = true; await api(`/api/instances/${encodeURIComponent(name)}/stop`, { method: "POST" }); toast(`「${name}」正在停止并保存世界`);
        }
      } catch (e) { if (act === "start") toastErr("无法启动服务器", e, { actions: await startFixes(name) }); else toastErr("无法停止服务器", e); }
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
    let list, dups = [];
    try {
      const resp = await api("/api/instances", { full: true });
      list = resp.data || [];
      dups = resp.duplicates || [];
    } catch (e) {
      if (!stale(gen)) $("#inst-cards").innerHTML = errBox("无法获取服务器列表", e, { hint: "正在自动重试" });
      return;
    }
    if (stale(gen)) return;
    paintDuplicates(dups);
    curList = list;
    const running = list.filter(i => i.status === "running").length;
    const online = list.reduce((s, i) => s + (i.onlineCount || 0), 0);
    $("#inst-summary").innerHTML = list.length ? `共 ${list.length} 台${SEP}运行中 ${running} 台${online ? `${SEP}在线 ${online} 人` : ""}` : "";
    if (!list.length) {
      $("#inst-chrome").hidden = true; // 没有实例时不显示「服务器」标题与搜索/排序
      $("#inst-cards").classList.add("page-fill");
      // 一个主行动 + 一个安静的次要入口（两个同权重按钮等于没有主按钮）。
      // 主按钮直接进「从零创建」的向导（不替人选核心）；整合包、已有地图、交接包、接管这四种走次要入口去「新建服务器」页挑
      $("#inst-cards").innerHTML = `<div class="empty${first ? " sec-enter" : ""}">${MDPixel.emptySVG(240)}
        <div class="empty-title">还没有服务器</div>
        选择核心与版本后，MineDeck 将自动补全服务端文件及匹配的 Java 运行环境。<br>
        <a class="btn primary" id="empty-new" href="#/create/new">${icon("plus")} 从零创建</a><br>
        <a class="quiet-link" href="#/create">导入整合包、用已有地图开服、导入交接包或接管已有服务器 →</a></div>`;
      lastSig = "empty"; first = false; return;
    }
    $("#inst-chrome").hidden = false;
    $("#inst-cards").classList.remove("page-fill");
    // 签名：数据无实质变化则跳过重绘（消除周期性闪烁与焦点丢失，也不重播动效）；启动中按秒推进分段条，所以带上时长
    // 端口、上次启动失败的原因、连接地址的 IP 也算：崩溃自动重启、定时重启没起来时，卡片上要跟着冒出原因
    const sig = list.map(i => `${i.name}:${i.status}:${i.status === "running" ? Math.floor(i.uptimeSec / 60) + "/" + i.onlineCount : i.status === "starting" ? Math.floor(i.uptimeSec / 3) : ""}:${connectPortOf(i)}:${i.lastStartError || ""}:${i.javaMissing ? 1 : 0}`).join("|") + "#" + $("#inst-q").value + "#" + $("#inst-sort").value + "#" + lanIP;
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
  appInfo.then(app => { // 签名里带着 lanIP：IP 到了（或者变了），下一次画就会把地址和复制键补上；列表已经画出来的话马上补
    const ips = learnIPs(app);
    if (stale(gen)) return;
    lanIP = pickLanIP(ips);
    if (curList.length) draw();
  });
  await draw();
  startPoll(draw, 3000, gen);
}

/* ---------- 视图：新建向导 ---------- */
const wiz = { step: 0, core: null, mc: null, build: null, snapshots: false, root: "", skipMods: false, fill: null };

/* 「返回修改」（可用性走查 R13 / F9）：表单提交、拿到任务号时记下「任务号 → 从哪个表单来、填了什么」；任务失败或取消以后，
   任务页上点「返回修改」回到原来的表单，填过的都在（导入整合包选的文件也在：File 对象留在内存里，回来时塞回文件框）。
   只记在这一次打开的页面里，刷新以后就没了，任务页上那时只给「返回新建服务器」。点了按钮才恢复，而且只恢复一次：
   之后再进新建页还是空白的表单。 */
const formDrafts = new Map(); // 任务号 → { mode: new | import | adopt, data }
let restoreDraft = null;      // 点了「返回修改」、新建页还没画出来的那一份
let formFill = null;          // 已经交给某个表单、等它画好再填进去的那一份：{ mode, data }
const takeFill = mode => { const f = formFill; formFill = null; return f && f.mode === mode ? f.data : null; };
function backToForm(taskId) {
  const d = formDrafts.get(taskId);
  if (!d) return false;
  restoreDraft = d;
  const want = "#/create/" + d.mode;
  if ((location.hash || "") === want) navigate(); else location.hash = want;
  return true;
}
/* 文件框里塞回一个之前选过的文件（DataTransfer 在 Chromium / WebView2 里能这样用）。塞不回去就算了，人再选一次 */
function putFile(input, file) {
  if (!input || !file) return false;
  try { const dt = new DataTransfer(); dt.items.add(file); input.files = dt.files; return input.files.length === 1; } catch { return false; }
}

/* 五种方式各有自己的地址（#/create/new、import、world、transfer、adopt）：首页空状态能直接进「从零创建」，
   后退键能回到这一页。「返回」一律走 goCreateHome，不直接重画（地址不变的话，再点同一张卡片不会触发路由）。 */
const CREATE_MODES = { new: () => drawWizard(), import: () => drawImport(), transfer: () => drawTransfer(), adopt: () => drawAdopt(),
  world: () => { if (window.MDWorldImport) MDWorldImport.wizardOpen(); drawWizard(); } }; // 用已有地图开服：同一个向导，选地图的面板一上来就开着
const goCreateHome = () => { if ((location.hash || "") === "#/create") navigate(); else location.hash = "#/create"; };

async function renderCreate(mode) {
  const gen = beginCreateView();
  wiz.step = 0; wiz.core = null; wiz.mc = null; wiz.build = null; wiz.root = ""; wiz.skipMods = false; wiz.fill = null;
  if (window.MDWorldImport) MDWorldImport.wizardReset(); // 用已有地图开服：上一次选的地图不带进这一次
  // 「返回修改」：向导直接回到最后一步（基本设置），核心、版本、构建、选的地图和填过的设置都照原样；导入整合包、接管交给各自的表单去填
  const dr = restoreDraft;
  restoreDraft = null;
  formFill = null;
  if (dr && dr.mode === mode) {
    if (mode === "new") {
      Object.assign(wiz, dr.data.wiz, { fill: dr.data.fields });
      if (dr.data.map && window.MDWorldImport) MDWorldImport.wizardRestore(dr.data.map);
    } else formFill = dr;
  }
  if (mode && CREATE_MODES[mode]) { CREATE_MODES[mode](); return; }
  const card = (id, ico, name, desc) => `<div class="core-card" id="mode-${id}" data-mode="${id}" role="button" tabindex="0" aria-label="${name}" aria-describedby="mode-${id}-d"><span class="recipe-ico">${icon(ico)}</span><div><div class="cn">${name}</div><div class="cd" id="mode-${id}-d">${desc}</div></div></div>`;
  // 五张卡片两组（可用性走查 R14）：「新建」是开一台新的，「搬过来」是把别处现成的一整台服务器收进来。
  // 「用已有地图开服」原来是卡片下面的一行小字，走查员翻到页底才看到（B7）
  Main().innerHTML = pageHead("新建服务器", "选一种方式开始。", "", "", `五种方式`) + `
    <section id="task-list" hidden style="margin-bottom:24px"></section>
    <div class="recipe-groups">
      <div class="core-group">
        <div class="cg-title">新建</div>
        <div class="recipe-grid">
          ${card("new", "plusBox", "从零创建", "自选核心与版本；Java 和服务端自动下载。")}
          ${card("import", "package", "导入整合包", "Modrinth / CurseForge，或作者给的现成服务端压缩包。")}
          ${card("world", "globe", "用已有地图开服", "下载的地图、自己的单人世界、别的服务器的世界，拿来开一台新的。")}
        </div>
      </div>
      <div class="core-group">
        <div class="cg-title">搬过来</div>
        <div class="recipe-grid">
          ${card("transfer", "share", "导入交接包", "MineDeck 导出的 .mdpack 交接包；世界、配置、模组和插件在包里。")}
          ${card("adopt", "folder", "接管已有服务器", "接管本机外部服务器：选择文件夹直接关联，原文件保留在本地路径。")}
        </div>
      </div>
    </div>`;
  // 卡片可用键盘激活（回车 / 空格）
  document.querySelectorAll(".recipe-grid .core-card").forEach(c => {
    c.onclick = () => { location.hash = "#/create/" + c.dataset.mode; };
    c.onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); c.click(); } };
  });
  const taskList = $("#task-list");
  await refreshTaskList(taskList, gen);
  startPoll(() => refreshTaskList(taskList, gen), 3000, gen);
}

/* ---------- 端口默认值（可用性走查 R08）----------
   新建向导（用已有地图开服是同一个向导）、导入整合包、接管已有服务器的端口：默认取 MineDeck 里别的服务器都没用的最小端口
   （从 25565 起，GET /api/ports/suggest），换过的在端口下面说一句「25565 已被『X』使用，已改为 25566」；
   手填的端口和别的服务器撞了当场提示。只提示、不拦着提交：两台从来不同时开的服务器可以共用一个端口。
   拿不到建议（老后端、连不上）就照旧，不提示。 */
const suggestPort = () => api("/api/ports/suggest").catch(() => null);
/* input：端口框；note：它下面放提示的元素；sug：/api/ports/suggest 的结果（可以是 null）。
   base：原来的端口（新建、导入是 25565，接管是文件夹里写的）；keep：「返回修改」填回来的，不换。
   avoidOther：默认端口被别的程序占着时也换（新建、导入整合包、用已有地图开服；beta.3 之后，回访走查 11-16：以前开服才报）。
   接管传 false：原来那台没停的服务器自己就是「别的程序」，换了端口接管时会改它的 server.properties，所以只提示。
   otherTip：端口被别的程序占着时另接的一句（接管：可能是原来的服务器还开着）。
   extra(v)：另外接在提示后面的一句（接管：端口和原来不一样时会改 server.properties）。返回 { def }：默认填的端口 */
function wirePortField(input, note, sug, { base = 25565, keep = false, extra = null, avoidOther = true, otherTip = "" } = {}) {
  const used = sug && Array.isArray(sug.used) ? sug.used : [];
  const owner = p => used.find(u => u.port === p);
  let auto = null; // 默认值换过：{ from, to, by（本软件的哪台）、reserved（系统保留）或 proc（别的程序，可能是空串）}
  const o = owner(base);
  if (!keep && o && sug.port && sug.port !== base) auto = { from: base, to: sug.port, by: o.instance };
  else if (!keep && avoidOther && sug && sug.fromBusy && sug.from === base && sug.port && sug.port !== base)
    auto = { from: base, to: sug.port, proc: sug.fromProcess || "", reserved: !!sug.fromReserved };
  if (auto) input.value = auto.to;
  // 手填的端口有没有被别的程序占着或被系统保留：问后端（port.suggest 带 from，回 fromBusy、fromProcess、fromReserved），
  // 停手 300 毫秒再问，回来的不是现在框里的就不要。不用 port.check：端口空着时它会在所有网卡上试绑一次，
  // 打开表单就可能让防火墙弹窗问要不要放行（Codex 复审 beta.3 之后第一轮）。
  // 默认端口（包括自动换出来的那个）只在刚打开表单那一次免查；之后改过再填回来、以及提交表单时，都照常查（阶段 04 A04）
  let other = { v: 0, text: "", suggest: 0 }, seq = 0, timer = 0;
  const busyText = (v, { proc = "", reserved = false, ownerName = "", suggest = 0 } = {}) => {
    if (ownerName) return `端口 ${v} 已被你的服务器「${ownerName}」使用（它现在开着），两台不能同时开。` +
      (suggest && suggest !== v ? `换一个吧，比如 ${suggest}。` : "");
    if (reserved) return `端口 ${v} 在系统保留的端口范围里（Hyper-V、WSL 等保留），开服时会报错。换一个端口${suggest && suggest !== v ? `，比如 ${suggest}` : ""}。`;
    return `端口 ${v} 现在被别的程序${proc ? `（${proc}）` : ""}占着，开服时会报错。换一个，或者先关掉那个程序。${otherTip}`;
  };
  const check = (v, init = false) => {
    clearTimeout(timer);
    if (!(Number.isInteger(v) && v >= 1 && v <= 65535) || owner(v) || (init && auto && v === auto.to)) return;
    if (init && sug && v === sug.from && !sug.fromBusy && !sug.usedBy) return;
    const my = ++seq;
    timer = setTimeout(async () => {
      if (!input.isConnected) return;
      let r;
      try { r = await api(`/api/ports/suggest?from=${v}`); } catch { return; }
      if (my !== seq || !input.isConnected) return;
      other = { v, suggest: 0, text: r && r.fromBusy
        ? busyText(v, { proc: r.fromProcess || "", reserved: !!r.fromReserved, suggest: r.port || 0 }) : "" };
      paint();
    }, 300);
  };
  const paint = () => {
    const v = Number(input.value), hit = Number.isInteger(v) ? owner(v) : null;
    let text = "", warn = !!hit, usePort = 0;
    if (hit) text = `端口 ${v} 已被你的服务器「${hit.instance}」使用${hit.status && hit.status !== "stopped" ? "（它现在开着）" : ""}，两台不能同时开。` +
      (sug && sug.port && sug.port !== v ? `换一个吧，比如 ${sug.port}。` : "");
    else if (other.v === v && other.text) { text = other.text; warn = true; usePort = other.suggest || 0; }
    else if (auto && v === auto.to) text = auto.by ? `${auto.from} 已被「${auto.by}」使用，已改为 ${auto.to}。`
      : auto.reserved ? `${auto.from} 在系统保留的端口范围里，已改为 ${auto.to}。`
      : `${auto.from} 被别的程序${auto.proc ? `（${auto.proc}）` : ""}占着，已改为 ${auto.to}。`;
    const more = extra ? extra(v) : "";
    note.classList.toggle("warn", warn);
    const btn = usePort && usePort !== v ? ` <button type="button" class="btn sm" data-port-use="${usePort}" aria-label="把端口改成 ${usePort}">改成 ${usePort}</button>` : "";
    note.innerHTML = text || more ? `${icon(warn ? "warn" : "info")}<span>${esc(text + more)}</span>${btn}` : "";
    const ub = note.querySelector("[data-port-use]");
    if (ub) ub.onclick = () => {
      input.value = String(usePort);
      other = { v: 0, text: "", suggest: 0 };
      input.dispatchEvent(new Event("input", { bubbles: true }));
      input.focus();
    };
  };
  const onSubmitBusy = pb => {
    if (!pb || !pb.port || !input.isConnected) return false;
    seq++; clearTimeout(timer);
    const v = Number(pb.port);
    const sugPort = Number(pb.suggest) || 0;
    let t = busyText(v, { proc: pb.process || "", reserved: !!pb.reserved, ownerName: pb.owner || "", suggest: sugPort });
    if (!pb.owner && !pb.reserved && sugPort && sugPort !== v) {
      t = `端口 ${v} 现在被别的程序${pb.process ? `（${pb.process}）` : ""}占着，开服时会报错。换一个吧，比如 ${sugPort}，或者先关掉那个程序。`;
    }
    other = { v, text: t, suggest: sugPort };
    paint();
    input.closest(".field")?.scrollIntoView({ behavior: "smooth", block: "center" });
    input.focus({ preventScroll: true });
    return true;
  };
  input.addEventListener("input", () => { paint(); check(Number(input.value)); });
  paint();
  check(Number(input.value), true);
  return { def: auto ? auto.to : base, onSubmitBusy };
}

/* ---------- 视图：接管已有服务器 ----------
   选文件夹 → 后端只读地识别一遍（核心、版本、启动文件、原来分的内存）→ 用户确认或修改 → 后台任务登记。
   只能在本机用：要读这台电脑上的文件夹（后端 instance.adopt* 是仅本机、仅人工）。 */
/* 没有时可以下载便携版的 Java 版本（清华镜像 / Adoptium / Mojang 运行时里有的） */
const JAVA_DOWNLOADABLE = [8, 11, 16, 17, 21, 25];
async function drawAdopt() {
  const gen = beginCreateView();
  const [app, cores, javas, sug] = await Promise.all([
    api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096, config: {} })),
    api("/api/cores").catch(() => []),
    api("/api/javas").catch(() => ({ portable: [], scanned: [] })),
    suggestPort(), // 端口：识别出来的和别的服务器撞了时换一个（R08）
  ]);
  if (stale(gen)) return;
  cores.forEach(c => CORES[c.id] = c);
  const maxMem = Math.max(2048, app.ramMb - 2048);
  let scan = null; // 最近一次识别的结果；改了文件夹没重新识别就不能接管
  let fill = takeFill("adopt"); // 「返回修改」：填回文件夹、重新识别一遍（文件夹里的东西可能变了），识别完再把改过的几项盖上去
  Main().innerHTML = pageHead("接管已有服务器", "接管本机外部服务器：选择服务器所在文件夹即可直接关联。文件留在原地，世界、模组、插件与配置均不改动（端口和别的服务器撞了时，只改 server.properties 里的端口）。", "",
    `<a href="#/create">新建服务器</a>${icon("chevronRight")}<span>接管已有服务器</span>`) + `
    <div class="form-grid">
      <label class="field full"><span>服务器所在的文件夹</span>
        <div class="row" style="gap:8px">
          <input type="text" id="ad-dir" spellcheck="false" autocomplete="off" style="flex:1" aria-label="服务器所在的文件夹" placeholder="如 D:\\MyServer（包含服务端核心或启动文件的文件夹）">
          <button type="button" class="btn" id="ad-browse">${icon("folder")} 浏览…</button>
          <button type="button" class="btn primary" id="ad-scan">${icon("search")} 识别</button>
        </div>
        <div class="field-err" id="ad-dir-err"></div></label>
    </div>
    <div id="ad-form"></div>
    ${wizFoot("ad-back", "返回", `<button class="btn primary" id="ad-go" disabled>${icon("check")} 接管这台服务器</button>`)}`;
  $("#ad-back").onclick = goCreateHome;
  const dirErr = msg => { $("#ad-dir").classList.toggle("invalid", !!msg); $("#ad-dir-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  const reset = () => { scan = null; $("#ad-form").innerHTML = ""; $("#ad-go").disabled = true; };
  $("#ad-dir").addEventListener("input", () => { dirErr(""); reset(); });
  $("#ad-dir").addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); $("#ad-scan").click(); } });
  $("#ad-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { $("#ad-dir").value = r.path; dirErr(""); reset(); $("#ad-scan").click(); } }
    catch (e) { toastErr("无法选择服务器所在的文件夹", e); }
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
        <label class="field" id="ad-port-f"><span>端口</span><input type="number" id="ad-port" min="1" max="65535" value="${s.port || 25565}">
          <div class="field-err" id="ad-port-err"></div><div class="field-note" id="ad-port-note" aria-live="polite"></div></label>
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
          <div class="hint">通常选「自动」。整合包说明里指定了 Java 版本的，按它选；接管后也能在「设置 → 启动与内存」里换。</div></label>
      </div>
      <div class="eula-box" id="ad-eula-box" hidden>
        <label class="switch"><input type="checkbox" id="ad-eula"><span class="sw"></span>
          <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft 最终用户许可协议 (EULA)</a></span></label>
        <div class="field-err" id="ad-eula-err"></div>
      </div>
      <div class="field w-read ad-wl" id="ad-wl"><span>白名单</span><div class="ad-wl-v">${(() => {
        // 接管沿用原来的 server.properties，白名单不改，这里只写出来（可用性走查 R02）：开着的要先在「玩家」里加名字，朋友才进得来
        const w = s.whitelistOn;
        if (w === true) return `<b>开</b>（沿用原来的设置）：只有名单里的玩家能进。接管后可以在这台服务器的「玩家」里加名字、或者关掉。`;
        if (w === false) return `<b>关</b>（沿用原来的设置）：任何知道地址的人都能进。只想让熟人进，接管后到「玩家」里打开它。`;
        return `原来的 server.properties 里没写这一项，服务端按自己的默认值：26.3 起是开（名单是空的话谁都进不来），更早的版本是关。接管后在「玩家」里能直接开关。`;
      })()}</div></div>
      <div class="hint w-read" id="ad-what" style="margin-bottom:16px;line-height:1.8">
        ${s.managed ? "该文件夹曾由 MineDeck 管理，原有的名称、内存和定时任务配置会自动恢复。<br>" : ""}
        接管时 MineDeck 只在这个文件夹里写自己的 <code>instance.json</code> 和启动脚本 <code>run.bat</code>。原来就有 <code>run.bat</code> 的，先另存为 <code>run.before-minedeck.bat</code>。别的文件都不动；只有端口和原来的不一样时，才改 <code>server.properties</code> 里端口这一项。<br>
        接管前需先停止原来的服务器，否则启动时端口会冲突。
        <span id="ad-noworld" hidden><br>${icon("info")} 这个文件夹里没看到世界（world），启动后会生成一个新世界。世界放在别处的话，先确认选对了文件夹。</span>
      </div>`;
    renderMemoryControl($("#ad-mem-mount"), { sliderId: "ad-mem", min: 1024, max: Math.max(maxMem, 1024), value: Math.min(Math.max(s.xmxMb || 4096, 1024), maxMem), totalMb: app.ramMb, availMb: app.availRamMb,
      hint: s.xmxMb ? `原启动脚本里分配的是 ${(s.xmxMb / 1024).toFixed(1)} GB，已按它填好` : "推荐分配：模组服 4 GB 以上；无模组的小型服务器 2~4 GB。" });
    // 代理端没有 EULA，版本是它自己的版本；已经同意过 EULA 的不用再勾
    const paintKind = () => {
      const proxy = isProxyCore($("#ad-core").value);
      $("#ad-eula-box").hidden = proxy || s.eula;
      $("#ad-noworld").hidden = proxy || s.hasWorld; // 代理端本来就没有存档
      $("#ad-wl").hidden = proxy; // 代理端没有白名单这一项
      $("#ad-port-f").hidden = proxy; // 代理端的端口在它自己的配置文件里，不改 server.properties
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
    if (fill && fill.dir === s.dir) {
      const f = fill;
      $("#ad-name").value = f.name;
      if ([...$("#ad-target").options].some(o => o.value === f.target)) $("#ad-target").value = f.target;
      if ([...$("#ad-core").options].some(o => o.value === f.core)) $("#ad-core").value = f.core;
      $("#ad-mc").value = f.mc;
      if ([...$("#ad-java").options].some(o => o.value === String(f.java))) $("#ad-java").value = String(f.java);
      $("#ad-eula").checked = !!f.eula;
      if (f.port) $("#ad-port").value = f.port;
      renderMemoryControl($("#ad-mem-mount"), { sliderId: "ad-mem", min: 1024, max: Math.max(maxMem, 1024), value: f.mem, totalMb: app.ramMb, availMb: app.availRamMb,
        hint: s.xmxMb ? `原启动脚本里分配的是 ${(s.xmxMb / 1024).toFixed(1)} GB` : "推荐分配：模组服 4 GB 以上；无模组的小型服务器 2~4 GB。" });
    }
    // 端口（R08）：识别出来的和 MineDeck 里别的服务器撞了就换一个没人用的，说清会改 server.properties；没撞就沿用，什么都不改
    const was = s.port || 25565;
    // 被别的程序占着时只提示、不换：多半是原来那台服务器还开着，换了端口接管时会改它的 server.properties
    wirePortField($("#ad-port"), $("#ad-port-note"), sug, { base: was, keep: !!(fill && fill.dir === s.dir && fill.port),
      avoidOther: false, otherTip: "如果是原来的服务器还开着，接管前先把它停掉。",
      extra: v => Number.isInteger(v) && v >= 1 && v <= 65535 && v !== was ? `接管时会把 server.properties 里的端口从 ${was} 改成 ${v}（别的设置不动），朋友要用新端口连。` : "" });
    fill = null;
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
    const okMC = fieldErr($("#ad-mc"), "#ad-mc-err", $("#ad-mc").value.trim() ? "" : proxy ? "请填代理端的版本号" : "请填 Minecraft 版本，如 1.20.1");
    const pv = Number($("#ad-port").value);
    const okPort = proxy || fieldErr($("#ad-port"), "#ad-port-err", Number.isInteger(pv) && pv >= 1 && pv <= 65535 ? "" : "端口需为 1 ~ 65535 的整数");
    const needEula = !proxy && !scan.eula;
    const okEula = !needEula || $("#ad-eula").checked;
    if (needEula) { $("#ad-eula-box").classList.toggle("invalid", !okEula); $("#ad-eula-err").innerHTML = okEula ? "" : icon("warn") + " 开服前需要同意 Minecraft EULA"; }
    if (!(okName && okCore && okMC && okPort && okEula)) {
      const first = !okName ? $("#ad-name") : !okCore ? $("#ad-core") : !okMC ? $("#ad-mc") : !okPort ? $("#ad-port") : $("#ad-eula");
      first.closest(".field, .eula-box").scrollIntoView({ behavior: "smooth", block: "center" });
      first.focus({ preventScroll: true });
      return;
    }
    const target = $("#ad-target").value, picked = scan.targets.find(x => x.target === target);
    $("#ad-go").disabled = true;
    try {
      // 请求和「返回修改」的草稿在发请求之前一起读好（Codex 复审 beta.3）：等回话的时候切走了，表单就没了，回来再读会出错，
      // 接管明明开始了，界面却报失败，草稿也没留下
      const draft = { dir: scan.dir, name: $("#ad-name").value.trim(), core, mc: $("#ad-mc").value.trim(), target,
        mem: +$("#ad-mem").value, java: +$("#ad-java").value, eula: $("#ad-eula").checked, port: proxy ? "" : $("#ad-port").value };
      const r = await api("/api/adopt", {
        method: "POST",
        body: {
          dir: draft.dir, name: draft.name, core, mc: draft.mc,
          build: picked && picked.core === core ? picked.build : "",
          launchTarget: target, xmxMb: draft.mem, javaMajor: draft.java, eula: needEula,
          port: proxy ? 0 : pv, // 和文件夹里写的一样时后端什么都不改
        },
      });
      formDrafts.set(r.taskId, { mode: "adopt", data: draft }); // 先按任务号记下，再看页面还在不在
      if (!stale(gen)) location.hash = `#/task/${r.taskId}`;
      else toast("已开始接管，可在新建服务器页查看进度");
    } catch (e) { toastErr("无法接管服务器", e); if (!stale(gen)) $("#ad-go").disabled = false; }
  };
  $("#ad-dir").focus();
  if (fill) { $("#ad-dir").value = fill.dir; $("#ad-scan").click(); }
}

/* ---------- 视图：整合包导入 ---------- */
async function drawImport() {
  const gen = beginCreateView();
  const [app, sug] = await Promise.all([api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096, config: {} })), suggestPort()]);
  if (stale(gen)) return;
  const maxMem = Math.max(2048, app.ramMb - 2048);
  const defMem = Math.min(6144, maxMem);
  const fill = takeFill("import"); // 「返回修改」：上次填的（连选的文件）
  let imRoot = fill ? fill.root : "";
  const on = (k, def) => (fill ? fill[k] : def) ? " checked" : "";
  Main().innerHTML = pageHead("导入整合包", "Modrinth（.mrpack）、CurseForge（zip），或作者打包好的现成服务端。清单和服务端文件可以在最外层，也可以套在文件夹里（只有一个文件夹一层层套着的，最多认三层）。核心和版本会自动识别。", "",
    `<a href="#/create">新建服务器</a>${icon("chevronRight")}<span>导入整合包</span>`) + `
    <div class="form-grid">
      <label class="field full"><span>整合包文件</span><input type="file" id="im-file" accept=".mrpack,.zip"><div class="field-err" id="im-file-err"></div></label>
      <label class="field"><span>名称</span><input type="text" id="im-name" value="${esc(fill ? fill.name : "我的整合包服务器")}" maxlength="40"></label>
      <label class="field"><span>端口</span><input type="number" id="im-port" value="${esc(fill ? fill.port : "25565")}" min="1" max="65535">
        <div class="field-note" id="im-port-note" aria-live="polite"></div></label>
      <div class="field full" id="im-mem-mount"></div>
      <label class="field full"><span>存放位置</span>
        <div class="row" style="gap:8px">
          <input type="text" id="im-root" readonly style="flex:1;cursor:default;background:var(--surface-2)" aria-label="存放位置">
          <button type="button" class="btn" id="im-browse">${icon("folder")} 浏览…</button>
        </div></label>
      <label class="field full"><span>本地来源文件夹（可选）</span>
        <div class="row" style="gap:8px">
          <input type="text" id="im-src" style="flex:1" placeholder="整合包配套的服务器文件夹（可选）" aria-label="本地来源文件夹" value="${esc(fill ? fill.src : "")}">
          <button type="button" class="btn" id="im-src-browse" aria-label="浏览本地来源文件夹">${icon("folder")} 浏览…</button>
        </div>
        <div class="hint" style="margin-top:6px">优先使用此文件夹里的同名文件补全缺失项（跳过线上校验）；还是没装上的文件，导入完成后在任务页、或者这台服务器的详情页里逐个处理。</div></label>
      <div class="field"><label class="switch"><input type="checkbox" id="im-online"${on("online", false)}><span class="sw"></span>
        <span><span class="sw-label">正版验证</span><div class="sw-desc">默认关闭：离线账号可直接进服，但不会使用正版 UUID，也不显示正版皮肤</div></span></label></div>
      <div class="field"><label class="switch"><input type="checkbox" id="im-flight"${on("flight", true)}><span class="sw"></span>
        <span><span class="sw-label">允许飞行</span><div class="sw-desc">整合包建议开启</div></span></label></div>
      ${whitelistField("im-wl", "整合包自带 server.properties 的，白名单也以这里为准。")}
    </div>
    ${app.cfKeySet ? "" : `<div class="hint" style="margin-bottom:10px">解析 CurseForge 整合包需配置 API Key。可先前往 <a href="https://console.curseforge.com/" target="_blank">免费申请</a>，并在「设置 → 下载与网络」中完成绑定。（Modrinth 整合包不受此限制）</div>`}
    <div class="wiz-bottom">
      <div class="eula-box">
        <label class="switch"><input type="checkbox" id="im-eula"${on("eula", false)}><span class="sw"></span>
          <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft EULA</a></span></label>
        <div class="field-err" id="im-eula-err"></div>
      </div>
      ${wizFoot("im-back", "返回", `<button class="btn primary" id="im-go">${icon("bolt")} 导入并创建</button>`)}
    </div>`;
  $("#im-back").onclick = goCreateHome;
  const imPortCtl = wirePortField($("#im-port"), $("#im-port-note"), sug, { keep: !!fill }); // 默认端口避开别的服务器在用的（R08）；「返回修改」填回来的不换
  if (fill) {
    $("#im-wl").checked = !!fill.wl;
    if (!putFile($("#im-file"), fill.file) && fill.file) $("#im-file-err").innerHTML = icon("info") + " " + esc(`上次选的是「${fill.file.name}」，请再选一次`);
  }
  renderMemoryControl($("#im-mem-mount"), { sliderId: "im-mem", min: 2048, max: maxMem, value: fill ? fill.mem : defMem, totalMb: app.ramMb, availMb: app.availRamMb, hint: "推荐分配 6 GB 以上" });
  const imUpdateRoot = () => { $("#im-root").value = imRoot || app.serversRoot || "程序文件夹\\servers"; };
  imUpdateRoot();
  $("#im-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { imRoot = r.path; imUpdateRoot(); toast("已选择：" + r.path); } }
    catch (e) { toastErr("无法选择存放位置", e); }
  };
  $("#im-src-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { $("#im-src").value = r.path; toast("已选择：" + r.path); } }
    catch (e) { toastErr("无法选择本地来源文件夹", e); }
  };
  // 行内校验（同向导）：错误显示在对应字段下方，改正后即时消失
  const fileErr = msg => { $("#im-file").classList.toggle("invalid", !!msg); $("#im-file-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  const eulaErr = msg => { $(".eula-box").classList.toggle("invalid", !!msg); $("#im-eula-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  $("#im-file").addEventListener("change", () => fileErr(""));
  $("#im-eula").addEventListener("change", () => { if ($("#im-eula").checked) eulaErr(""); });
  $("#im-go").onclick = async () => {
    const f = $("#im-file").files[0];
    if (!f) { fileErr("请先选择整合包文件（.mrpack 或 .zip）"); $("#im-file").focus(); return; }
    if (f.size > 4 * 1024 ** 3) { fileErr(`该整合包大小（${(f.size / 1024 ** 3).toFixed(2)} GB）超过 4 GB 导入上限。若这是完整服务端压缩包而非标准整合包，请先解压至本地文件夹，再通过「新建服务器 → 接管已有服务器」导入。`); $("#im-file").focus(); return; }
    if (!$("#im-eula").checked) { eulaErr("导入前需要同意 Minecraft EULA"); $("#im-eula").focus(); return; }
    $("#im-go").disabled = true;
    $("#im-go").textContent = "上传中…";
    // 请求和「返回修改」的草稿在第一次等待之前一起读好（Codex 复审 beta.3）：大整合包上传要一阵子，这期间切到别的页面，
    // 表单就没了，回话以后再读会出错——任务明明在跑，界面却报「无法导入整合包」，草稿也没留下
    const mem = $("#im-mem").value;
    const draft = { file: f, name: $("#im-name").value, port: $("#im-port").value, mem: +mem,
      root: imRoot, src: $("#im-src").value, online: $("#im-online").checked, flight: $("#im-flight").checked, wl: $("#im-wl").checked, eula: $("#im-eula").checked };
    const fd = new FormData();
    fd.append("file", f);
    fd.append("name", draft.name.trim());
    fd.append("xmxMb", mem);
    fd.append("port", draft.port);
    fd.append("eula", "true");
    fd.append("onlineMode", draft.online ? "true" : "false");
    fd.append("allowFlight", draft.flight ? "true" : "false");
    fd.append("whitelist", draft.wl ? "true" : "false");
    fd.append("root", imRoot);
    fd.append("localSource", draft.src.trim());
    try {
      const res = await fetch("/api/import/modpack", { method: "POST", headers: { "X-Token": TOKEN() }, body: fd });
      const j = await res.json();
      if (!j.ok) throw Object.assign(new Error(j.error || "导入失败"), { hint: j.hint, portBusy: j.portBusy }); // hint：解析报错另给的一句人话，见 errSummary
      formDrafts.set(j.data.taskId, { mode: "import", data: draft }); // 先按任务号记下，再看页面还在不在
      const pack = j.data.pack || {};
      const pending = (pack.pending || 0) > 0 ? `，${pack.pending} 个文件待处理` : "";
      if (pack.format === "server") {
        toast(pack.core && pack.mc ? `已识别为现成服务端（${pack.core} ${pack.mc}${pack.mcFromLog ? "，版本号是从压缩包里的日志认出来的" : ""}），开始导入` : "已识别为现成服务端，核心和版本解压后再认");
      } else {
        toast(`已识别 ${pack.core} ${pack.mc}（${pack.files} 个组件）` + (pending ? "；其中" + pack.pending + " 项待处理，导入后到任务页逐个处理" : ""));
      }
      if (!stale(gen)) location.hash = `#/task/${j.data.taskId}`;
    } catch (e) {
      if (!stale(gen) && e.portBusy) imPortCtl.onSubmitBusy(e.portBusy);
      toastErr("无法导入整合包", e);
      if (!stale(gen)) { $("#im-go").disabled = false; $("#im-go").innerHTML = icon("bolt") + " 导入并创建"; }
    }
  };
}

/* ---------- 视图：实例交接包（.mdpack）----------
   别人开不下去的服务器，从卡片菜单「导出交接包…」打包发过来；这里选 .mdpack 导入，
   存档、配置、模组全保留，装完直接能启动。操作走 instance.transfer.*，导出导入都在任务页看进度。 */

/* 导出交接包：确认弹窗（隐私提醒 + 完整模式 + 导出位置）→ 后台打包（任务页看进度） */
function transferExportModal(info) {
  if ($("#tr-mask")) return; // 上一个还开着
  const d = modalLayer(`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="tr-title">
    <h3 id="tr-title">导出交接包「${esc(info.name)}」</h3>
    <div class="m-body">
      将服务器打包为 .mdpack 文件发给接手的人，对方通过「新建服务器 → 导入交接包」即可导入：世界、配置、模组、插件和白名单都在包里。
      <div class="warn-card" style="margin-top:12px">
        <div class="wc-title">${icon("warn")} 含玩家数据</div>
        <div class="wc-note">包内含世界（每个玩家的进度都在里面）、白名单和 OP 名单（不包含运行日志）。注意：配置文件中的密码会原样打包（如 server.properties 的 RCON 密码、插件配置中的数据库密码与机器人令牌），在别处复用的密码请先修改后再交接。</div>
      </div>
      <div class="field tr-modes" style="margin-top:12px" role="radiogroup" aria-label="这个包给谁"><span>这个包给谁</span>
        <label class="ob-radio"><input type="radio" name="tr-mode" value="handover" checked><span>交给下一任服主
          <small class="tr-mode-note">你不再开这台服务器，交给对方接着开。对方导入时，包用途写的是「服主交接（整台交给你）」。</small></span></label>
        <label class="ob-radio"><input type="radio" name="tr-mode" value="distributable"><span>发给朋友，照着开一台
          <small class="tr-mode-note">你自己的照样开着，对方照着开一台一样的。对方导入时，包用途写的是「发给朋友，照着开一台」。</small></span></label>
        <div class="hint">两种包里的东西完全一样，选哪个只改对方导入时看到的这一句。拿不准就用默认的。</div>
      </div>
      <label class="switch" style="margin-top:12px"><input type="checkbox" id="tr-full"><span class="sw"></span>
        <span><span class="sw-label">完整模式</span><div class="sw-desc">把服务端核心也一起打包；不勾则对方导入时自动重新下载（包更小）</div></span></label>
      <label class="field" style="margin-top:12px"><span>导出到哪个文件夹</span>
        <div class="row" style="gap:8px">
          <input type="text" id="tr-dest" spellcheck="false" autocomplete="off" style="flex:1" aria-label="导出文件夹" placeholder="默认：程序数据文件夹 \\ cache \\ transfer">
          <button type="button" class="btn" id="tr-browse">${icon("folder")} 浏览…</button>
        </div>
        <div class="field-err" id="tr-dest-err"></div>
      </label>
      <div class="hint" style="margin-top:6px">世界较大时交接包可能达数 GB；可在任务页查看打包进度，中断后可重新导出。</div>
    </div>
    <div class="m-err" id="tr-err" role="alert" hidden></div>
    <div class="m-actions">
      <button class="btn" id="tr-cancel">取消</button>
      <button class="btn primary" id="tr-go">${icon("share")} 导出交接包</button>
    </div></div>`, "tr-mask");
  // 有要填的东西：点遮罩不关（以前点中的常常是刚消失的提示底下那块遮罩，填好的路径一下就没了，可用性走查 U04）；
  // Esc 只在它是最上面那层时关（上面叠着「报错信息」时先关那个）
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    document.removeEventListener("keydown", onKey);
    dismiss(d);
  };
  const onKey = e => { if (e.key === "Escape" && topLayer(d)) close(); };
  document.addEventListener("keydown", onKey);
  const destErr = html => { $("#tr-dest").classList.toggle("invalid", !!html); $("#tr-dest-err").innerHTML = html ? icon("warn") + " " + html : ""; };
  const formErr = e => { // 别的原因：写在按钮上面，不只靠右上角那条会消失的提示；原文点「详情」看（叠在这个弹窗上面）
    const box = $("#tr-err");
    if (!e) { box.hidden = true; box.innerHTML = ""; return; }
    const { text, raw } = errSummary("无法导出交接包", e);
    box.innerHTML = `${icon("warn")}<span>${esc(text)}</span><button type="button" class="btn sm ghost" id="tr-err-more" aria-label="查看导出报错的详情">详情</button>`;
    box.hidden = false;
    $("#tr-err-more").onclick = () => showErrDetail(raw);
  };
  $("#tr-cancel").onclick = close;
  $("#tr-browse").onclick = async () => {
    try { const r = await api("/api/pickdir", { method: "POST" }); if (r.path) { $("#tr-dest").value = r.path; destErr(""); formErr(null); } }
    catch (e) { toastErr("无法选择导出文件夹", e); }
  };
  $("#tr-dest").addEventListener("input", () => { if ($("#tr-dest").classList.contains("invalid")) destErr(""); });
  /* mkdir：「新建并导出」（可用性走查 R20）。导出文件夹不在时，后端先把它建出来再导出：只建最后一级、上一级要已经在，
     只许本机（transfer.MakeExportDir、web 的 handleTransferExport）。建不了的原因（上一级不在、是个文件、名字不能用、
     从局域网打开）是给人看的整句，写在那一栏下面 */
  let errToast = null; // 上一次的「无法导出交接包」：再点一次（比如「新建并导出」）就收掉，不和成功的提示叠在一起（回访走查 11-16）
  const go = async mkdir => {
    if (errToast) { errToast.dismiss(); errToast = null; }
    const dest = $("#tr-dest").value.trim();
    if (dest && !/^[a-zA-Z]:[\\/]/.test(dest)) {
      destErr("请填完整的文件夹路径，或清空用默认位置");
      return;
    }
    $("#tr-go").disabled = true;
    const mk = $("#tr-mkdir");
    if (mk) mk.disabled = true;
    destErr(""); formErr(null);
    try {
      const r = await api(`/api/instances/${encodeURIComponent(info.name)}/transfer/export`, {
        method: "POST", body: { full: $("#tr-full").checked, mode: (d.querySelector('input[name="tr-mode"]:checked') || {}).value || "handover", dest, ...(mkdir ? { mkdir: true } : {}) },
      });
      close();
      toast(mkdir ? "已新建导出文件夹，开始导出交接包" : "开始导出交接包");
      location.hash = `#/task/${r.taskId}`;
    } catch (e) {
      errToast = toastErr(mkdir ? "无法新建导出文件夹" : "无法导出交接包", e);
      if (closed) return;
      $("#tr-go").disabled = false;
      // 导出文件夹的问题写在那一栏下面，并说清为什么不可用（internal/transfer/export.go）：
      // 找不到这个文件夹时给「新建并导出」；是个文件、打不开时让人另选；没填（用默认位置）时是默认位置建不起来
      if (mkdir) destErr(esc(e.message));
      else if (dest && e.message.includes("找不到这个文件夹")) {
        destErr(`导出文件夹不可用：找不到这个文件夹。点「新建并导出」由 MineDeck 建好它再导出（只建最后一级，上一级文件夹要已经在），或者点「浏览…」选一个已有的文件夹。
          <div class="tr-mkdir-row"><button type="button" class="btn sm" id="tr-mkdir">${icon("folder")} 新建并导出</button></div>`);
        $("#tr-mkdir").onclick = () => go(true);
      } else if (/导出(?:目录|文件夹)不可用/.test(e.message)) {
        destErr(dest ? "导出文件夹不可用：这个位置不是一个文件夹，或者打不开。点「浏览…」另选一个文件夹，原因看「详情」。"
          : "导出文件夹不可用：默认位置（程序数据文件夹里的 cache\\transfer）建不起来。点「浏览…」另选一个文件夹，原因看「详情」。");
        formErr(e);
      } else if (/导出(?:目录|文件夹)不能放在/.test(e.message)) destErr(esc(e.message));
      else formErr(e);
    }
  };
  $("#tr-go").onclick = () => go(false);
}

/* 导入交接包向导：选 .mdpack → 上传（顺便出摘要）→ 摘要展示 + 填名称 / 内存 / 端口 / EULA → 后台导入 */
async function drawTransfer() {
  const gen = beginCreateView();
  const app = await api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096, config: {} }));
  if (stale(gen)) return;
  const maxMem = Math.max(2048, app.ramMb - 2048);
  let trapRoot = "", pack = null, packFile = ""; // pack：上传后拿到的摘要；packFile：后端缓存里的包路径
  // 「返回修改」（A49，同导入整合包）：上次选的包和填的都在。上传的那份导入结束时已经删了，选回来的包要重新上传一遍，
  // 传完画出表单以后再把填过的盖上去（只盖一次）
  let fill = takeFill("transfer");
  Main().innerHTML = pageHead("导入交接包", "MineDeck 导出的 .mdpack 交接包；世界、配置、模组和插件都在包里，导入后可以启动。", "",
    `<a href="#/create">新建服务器</a>${icon("chevronRight")}<span>导入交接包</span>`) + `
    <div class="form-grid">
      <label class="field full"><span>交接包文件（.mdpack）</span><input type="file" id="tp-file" accept=".mdpack,.zip">
        <div class="hint">交接包通常有几个 GB，上传需要等待，进度显示在按钮上。扩展名被改过的包，选文件时切到「所有文件」。</div>
        <div class="field-err" id="tp-file-err"></div></label>
    </div>
    <div id="tp-form"></div>
    ${wizFoot("tp-back", "返回", `<button class="btn primary" id="tp-go" disabled>${icon("bolt")} 导入交接包</button>`)}`;
  $("#tp-back").onclick = goCreateHome;
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
      fileErr(`该交接包大小（${(f.size / 1024 ** 3).toFixed(2)} GB）超过 8 GB 导入上限。大体积服务器请直接将文件夹复制到本机，再通过「新建服务器 → 接管已有服务器」导入。`);
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
  if (fill) {
    if (putFile($("#tp-file"), fill.file)) upload();
    else if (fill.file) fileErr(`上次选的是「${fill.file.name}」，请再选一次`);
  }

  const drawForm = () => {
    const s = pack;
    const mem = Math.min(Math.max(s.xmxMb || 6144, 2048), maxMem);
    $("#tp-form").innerHTML = `
      <div class="group" style="margin:16px 0">
        <div class="wc-title" style="padding:14px 16px 4px">${icon("info")} 这个包里是什么</div>
        <div class="kv" style="padding:6px 16px 14px;margin-top:4px">
          <span>原名称</span><b>${esc(s.name)}</b>
          <span>包用途</span><b>${s.mode === "distributable" ? "发给朋友，照着开一台" : "服主交接（整台交给你）"}</b>
          <span>核心</span><b>${esc(coreName(s.core))} ${esc(s.mc)}${s.build ? `（构建 ${esc(s.build)}）` : ""}</b>
          <span>世界大小</span><b>${fmtBytes(s.worldBytes) || "0 B"}</b>
          <span>模组与插件</span><b>${s.mods} 个模组${s.plugins ? ` / ${s.plugins} 个插件` : ""}</b>
          <span>内存</span><b>${(s.xmxMb / 1024).toFixed(1)} GB（原服务器的分配）</b>
          <span>Java</span><b>Java ${s.javaMajor || "按版本自动选"}</b>
          <span>导出自</span><b>MineDeck ${esc(s.minedeckVersion || "")}，${esc(s.exportedAt || "")}</b>
        </div>
      </div>
      <div class="warn-card" id="tp-trust" style="margin:0 0 16px">
        <div class="wc-title">${icon("warn")} 导入前请确认来源</div>
        <div class="wc-note">包里的模组、插件${s.full ? "和服务端核心" : ""}是对方电脑上的程序，启动服务器时会在你的电脑上运行。只导入信得过的人给的包。</div>
      </div>
      <div class="hint" style="margin:0 0 16px">原来的 JVM 参数和定时任务不会带过来，导入完成后的日志里会列出；包里有核心时，核心的启动参数也是包里的那份。</div>
      <div class="form-grid">
        <label class="field"><span>名称</span><input type="text" id="tp-name" maxlength="40" value="${esc(s.name)}"><div class="field-err" id="tp-name-err"></div></label>
        <label class="field"><span>端口</span>
          <input type="number" id="tp-port" value="${s.port || 25565}" min="1" max="65535">
          <div class="field-err" id="tp-port-err"></div></label>
        <div class="field full" id="tp-mem-mount"></div>
        <label class="field full"><span>Java</span><select id="tp-java" aria-label="Java">
          <option value="0">${s.javaMajor ? `跟原来一样：Java ${s.javaMajor}` : "自动：按 Minecraft 版本选"}（本机没有就下载）</option>
          ${JAVA_DOWNLOADABLE.filter(m => m !== s.javaMajor).map(m => `<option value="${m}">Java ${m}</option>`).join("")}</select>
          <div class="hint">通常不用改；导入后也能在「设置 → 启动与内存」里换。</div></label>
        <label class="field full"><span>存放位置</span>
          <div class="row" style="gap:8px">
            <input type="text" id="tp-root" readonly style="flex:1;cursor:default;background:var(--surface-2)" aria-label="存放位置">
            <button type="button" class="btn" id="tp-browse">${icon("folder")} 浏览…</button>
          </div>
          <div class="hint">${s.full ? "完整模式：包里有核心，导入时不再下载。" : "瘦身包：导入时下载核心。"}</div></label>
      </div>
      <div class="eula-box">
        <label class="switch"><input type="checkbox" id="tp-eula"><span class="sw"></span>
          <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft EULA</a></span></label>
        <div class="hint" style="margin-top:4px">原服主在 MineDeck 里同意过 EULA 的，这步可以不勾。</div>
        <div class="field-err" id="tp-eula-err"></div>
      </div>`;
    renderMemoryControl($("#tp-mem-mount"), { sliderId: "tp-mem", min: 2048, max: maxMem, value: fill ? fill.mem : mem, totalMb: app.ramMb, availMb: app.availRamMb,
      hint: `${(s.xmxMb / 1024).toFixed(1)} GB 是原来的分配，可沿用或调整` });
    if (fill) {
      $("#tp-name").value = fill.name; $("#tp-port").value = fill.port; $("#tp-eula").checked = fill.eula;
      if ([...$("#tp-java").options].some(o => o.value === fill.java)) $("#tp-java").value = fill.java;
      trapRoot = fill.root;
      fill = null;
    }
    const trUpdateRoot = () => { $("#tp-root").value = trapRoot || app.serversRoot || "程序文件夹\\servers"; };
    trUpdateRoot();
    $("#tp-browse").onclick = async () => {
      try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { trapRoot = r.path; trUpdateRoot(); } }
      catch (e) { toastErr("无法选择存放位置", e); }
    };
    $("#tp-eula").addEventListener("change", () => { if ($("#tp-eula").checked) { $(".eula-box").classList.remove("invalid"); $("#tp-eula-err").innerHTML = ""; } });
    // 端口换了就查一下占用（后端 port.check）；被占了提示但不拦着——可能就是想沿用包里的端口。
    // 挂 input 事件（打字时每敲一下都会查，后面的结果追上前面的），不必等失焦
    const portErr = msg => { $("#tp-port").classList.toggle("invalid", !!msg); $("#tp-port-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
    let portCheck = 0;
    $("#tp-port").addEventListener("input", async () => {
      const v = +$("#tp-port").value;
      if (!(v >= 1 && v <= 65535)) { portErr("端口需为 1 ~ 65535 的整数"); return; }
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
    // 「返回修改」的草稿在发请求之前读好（同导入整合包）：等回话的时候切走了，表单就没了
    const draft = { file: $("#tp-file").files[0], name: $("#tp-name").value, port: $("#tp-port").value, mem: +$("#tp-mem").value,
      java: $("#tp-java").value, root: trapRoot, eula: $("#tp-eula").checked };
    $("#tp-go").disabled = true;
    $("#tp-go").textContent = "导入中…";
    try {
      const r = await api("/api/transfer/import", { method: "POST", body: {
        file: packFile, name, xmxMb: draft.mem || 0, port: +draft.port || 0, eula: draft.eula, root: trapRoot, javaMajor: +draft.java || 0,
      } });
      formDrafts.set(r.taskId, { mode: "transfer", data: draft }); // 先按任务号记下，再看页面还在不在
      if (stale(gen)) return;
      toast(`开始导入：${r.pack.core} ${r.pack.mc}（${r.pack.mods} 个模组${r.pack.plugins ? ` / ${r.pack.plugins} 个插件` : ""}）`);
      location.hash = `#/task/${r.taskId}`;
    } catch (e) {
      toastErr("无法导入交接包", e);
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
/* 「选模组」那一步的名字跟着核心走：插件端（Paper 一系、代理端）装的是插件，叫「选插件」 */
const wizModNoun = () => window.MDModCart ? MDModCart.nounOf(wiz.core) : "模组";
const wizSteps = () => wiz.core === "vanilla"
  ? ["选核心", "选版本", "选构建", "基本设置"]
  : ["选核心", "选版本", "选构建", "选" + wizModNoun(), "基本设置"];
function wizardShell(inner) {
  const names = wizSteps();
  // 步骤条：方块序号 + 像素虚线；已完成打勾且可点回，当前步实底，未到达描边；眉标写明第几步
  Main().innerHTML = pageHead("新建服务器", "选好核心与版本后，Java 与服务端会自动下载并完成配置。", "", "",
    `第 ${wiz.step + 1} 步<span class="sep">/</span>共 ${names.length} 步`) + `
    <div class="steps">${names.map((n, i) => {
      const skipped = n === "选构建" && wiz.core === "vanilla"; // 原版没有「选构建」，该步不可点回
      const cls = i === wiz.step ? "cur" : i < wiz.step && !skipped ? "done" : "";
      const tip = cls === "done" ? `title="回到「${n}」"` : skipped ? `title="原版无需选构建" tabindex="-1"` : `tabindex="-1"`;
      return (i ? `<span class="step-line${i <= wiz.step ? " done" : ""}"></span>` : "") +
        `<button type="button" class="step-pill ${cls}" data-step="${i}" ${tip}><span class="sn">${cls === "done" ? icon("check") : i + 1}</span>${n}</button>`;
    }).join("")}</div>
    <div id="wiz-body" class="sec-enter">${inner}</div>`;
  document.querySelectorAll(".step-pill.done").forEach(p => p.onclick = () => { wiz.step = +p.dataset.step; drawWizard(); });
  if (window.MDWorldImport) MDWorldImport.wizardStrip(wiz, drawWizard); // 步骤条下面的「用已有地图开服」/ 已选的地图，见 worldimport.js
}

/* 向导吸底栏：左「返回」、中间回显已选内容、右主按钮；表单步与内容列同宽（w-read） */
const wizFoot = (backId, backText, right, wide = false) =>
  `<div class="wizard-foot${wide ? "" : " w-read"}"><button class="btn" id="${backId}">${icon("arrowLeft")} ${backText}</button>
    <span class="wf-sum" id="wf-sum"></span>${right}</div>`;
/* 新建、导入整合包（以及用已有地图开服，它走同一个向导）表单上的白名单开关（可用性走查 R02）：默认关，旁边写明关着意味着什么。
   26.3 起服务端自己生成配置时默认开白名单、名单又是空的，按默认值开出来的服务器朋友进不来，界面上却一切正常；
   所以 MineDeck 不论版本都按这个开关把 white-list 明确写进 server.properties。note：接在说明后面的一句（导入整合包用） */
const whitelistField = (id, note = "") => `<div class="field full wl-field"><label class="switch"><input type="checkbox" id="${id}"><span class="sw"></span>
    <span><span class="sw-label">只允许白名单里的玩家进入</span>
    <div class="sw-desc">关着时任何知道地址的人都能进；只想让熟人进，就打开它，再到「玩家」里加名字。${note}</div></span></label></div>`;
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
    $("#wback0").onclick = goCreateHome;
    paintWizSum();
    let cores;
    try { cores = await api("/api/cores"); } catch (e) { if (!stale(gen)) $("#core-grid").innerHTML = errBox("无法获取服务端核心列表", e); return; }
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
        // 用已有地图开服时预选地图保存的那个版本；列表里没有它就不预选，让人自己挑，不悄悄换成最新版（mapVer：没在用地图是 undefined）
        const mapVer = window.MDWorldImport ? MDWorldImport.wizardVersion(all) : undefined;
        if (!all.some(v => v.id === wiz.mc)) wiz.mc = mapVer !== undefined ? mapVer
          : (all.find(v => v.latest && v.type !== "snapshot") || all.find(v => v.type !== "snapshot") || {}).id || null;
        $("#wnext").disabled = !wiz.mc;
        paintWizSum();
        drawList();
        $("#ver-list .ver-item.sel")?.scrollIntoView({ block: "nearest" });
      } catch (e) { if (!outdated()) $("#ver-list").innerHTML = errBox("无法获取版本清单", e, { hint: "可在「设置 → 下载与网络」换一个下载源后重试。" }); }
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
    /* 构建默认收起成「高级」（可用性走查 U25 / R22）：新手不用懂构建，用推荐的那个直接「下一步」；要换的点开「高级」挑。
       列表照样一进来就取：要靠它定下推荐的构建，「下一步」才能点。没取到时自动展开，原因露在外面 */
    wizardShell(`<div class="sub">${coreKind(wiz.core) === "proxy" ? "" : "MC "}${esc(wiz.mc)} 的 ${esc(coreName(wiz.core))} 服务端：默认用推荐的最新稳定构建，一般不用改，直接点「下一步」。</div>
      <div class="build-rec w-read" id="build-rec">${icon("spinner")} 正在查推荐的构建…</div>
      <details class="wiz-adv w-read" id="build-adv"><summary>高级：换一个构建</summary>
        <div class="ver-list" id="build-list">${skeletonRows(6)}</div></details>
      ${wizFoot("wback", "上一步", `<button class="btn primary" id="wnext" disabled>下一步 ${icon("arrowRight")}</button>`)}`);
    $("#wback").onclick = () => { wiz.step = 1; drawWizard(); };
    $("#wnext").onclick = () => { wiz.step = 3; drawWizard(); };
    paintWizSum();
    try {
      const builds = await api(`/api/builds?core=${wiz.core}&mc=${encodeURIComponent(wiz.mc)}`);
      if (stale(gen)) return;
      if (!builds.length) throw new Error(`${wiz.mc} 暂无 ${coreName(wiz.core)} 构建，请换个版本或核心`);
      const rec = builds.find(b => b.recommended) || builds[0];
      if (!wiz.build) wiz.build = rec.id;
      paintWizSum();
      const paintRec = () => {
        const cur = builds.find(b => b.id === wiz.build) || rec;
        $("#build-rec").innerHTML = `${icon("check")} 用${cur.id === rec.id ? "推荐的" : "你选的"}构建 <b>${esc(cur.id)}</b>${cur.label ? `<span class="hint">（${esc(cur.label)}）</span>` : ""}`;
      };
      paintRec();
      if (wiz.build !== rec.id) $("#build-adv").open = true; // 之前换过：展开，看得见选的是哪个
      $("#build-list").innerHTML = builds.slice(0, 200).map(b => `
        <div class="ver-item ${wiz.build === b.id ? "sel" : ""}" data-id="${esc(b.id)}">
          <span>${esc(b.id)}${b.recommended ? `<span class="ver-tag">推荐</span>` : ""}</span><span class="vt">${esc(b.label || "")}</span>
        </div>`).join("");
      $("#build-list").querySelectorAll(".ver-item").forEach(el => {
        el.onclick = () => {
          wiz.build = el.dataset.id;
          $("#build-list").querySelectorAll(".ver-item").forEach(x => x.classList.toggle("sel", x.dataset.id === wiz.build));
          paintWizSum(); paintRec();
        };
        el.ondblclick = () => { wiz.build = el.dataset.id; wiz.step = 3; drawWizard(); };
      });
      $("#wnext").disabled = false;
    } catch (e) {
      if (stale(gen)) return;
      $("#build-rec").innerHTML = `${icon("warn")} 没查到构建，「下一步」先点不了`;
      $("#build-adv").open = true;
      $("#build-list").innerHTML = errBox("无法获取构建列表", e, { hint: "可在「设置 → 下载与网络」换一个下载源后重试。" });
    }

  } else if (wiz.step === 3 && wiz.core !== "vanilla") {
    /* 选模组（可选步，列表第 4 项）：热门清单 + 搜索 + 草稿购物车整块在外围模块 modcart.js。
       这一步永远可以「下一步」留空跳过，创建完成后想再加可以去详情页的「待装清单」标签；热门清单加载得慢、加载失败都不挡「下一步」。
       这一步是两栏（左边逛、右边清单），用数据面的宽度，吸底栏跟着放宽（wizFoot 的 wide）。 */
    /* 「跳过这一步」（可用性走查 U25 / R22）：原来只有「下一步」，走查员要猜它就是跳过。跳过就是这次建服不装待装清单里的东西
       （wiz.skipMods）：清单留在那儿，不删；清单里已经有东西时先问一句。点「下一步」照旧建好以后装清单 */
    wizardShell(`
      <div id="wiz-mod-body"></div>
      ${wizFoot("wback", "上一步", `<button class="btn ghost" id="wskip">跳过这一步</button><button class="btn primary" id="wnext">下一步 ${icon("arrowRight")}</button>`, true)}`);
    $("#wback").onclick = () => { wiz.step = 2; drawWizard(); };
    $("#wnext").onclick = () => { wiz.skipMods = false; wiz.step = 4; drawWizard(); };
    $("#wskip").onclick = async () => {
      let draft = null;
      try { draft = await api("/api/modcart"); } catch { draft = null; }
      if (stale(gen)) return;
      const n = draft ? (draft.count || 0) + (draft.local || []).length : 0;
      if (n > 0 && !(await confirmModal({ title: "跳过这一步？", icon: "info",
        body: `待装清单里已经有 ${n} 项。跳过这一步，这台服务器建好以后不会装它们；清单留着，以后还能用。`,
        okText: "跳过，这次不装", cancelText: "取消" }))) return;
      if (stale(gen)) return;
      wiz.skipMods = true; wiz.step = 4; drawWizard();
    };
    paintWizSum();
    /* 正在往清单里添加本地文件的那几秒先按住「下一步」：文件还没加完就走，后加进去的那个不在「这一步上看到的那些」里，
       建好服务器以后后端会认为清单被人动过，整张清单都不装。加完（或者失败）立刻放开。 */
    const onBusy = busy => {
      const next = $("#wnext"), skip = $("#wskip");
      if (stale(gen) || !next) return;
      next.disabled = busy;
      if (skip) skip.disabled = busy;
      next.innerHTML = busy ? `${icon("spinner")} 正在添加文件…` : `下一步 ${icon("arrowRight")}`;
    };
    if (window.MDModCart) MDModCart.wizardPanel($("#wiz-mod-body"), { core: wiz.core, mc: wiz.mc, onBusy }, gen);
    else $("#wiz-mod-body").innerHTML = `<div class="err-box">这一步的界面没有加载出来。可以先跳过，以后在这台服务器的「插件」或「模组」页里再加。</div>`;

  } else {
    const isProxy = coreKind(wiz.core) === "proxy";
    // 端口默认值避开 MineDeck 里别的服务器在用的（R08）；代理端的端口在它自己的配置文件里，不问
    const [app, sug] = await Promise.all([api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096 })), isProxy ? null : suggestPort()]);
    if (stale(gen)) return;
    const maxMem = Math.max(2048, app.ramMb - 2048);
    const defMem = isProxy ? 1024 : Math.min(4096, maxMem);
    wizardShell(`
      <div class="form-grid">
        <label class="field"><span>名称</span><input type="text" id="f-name" value="我的${coreName(wiz.core)}服务器" maxlength="40"><div class="field-err" id="f-name-err"></div></label>
        ${isProxy ? "" : `<label class="field"><span>端口</span><input type="number" id="f-port" value="25565" min="1" max="65535"><div class="field-err" id="f-port-err"></div>
          <div class="field-note" id="f-port-note" aria-live="polite"></div></label>`}
        <div class="field full" id="mem-mount"></div>
        <label class="field full"><span>存放位置</span>
          <div class="row" style="gap:8px">
            <input type="text" id="f-root" readonly style="flex:1;cursor:default;background:var(--surface-2)" aria-label="存放位置">
            <button type="button" class="btn" id="f-browse">${icon("folder")} 浏览…</button>
          </div>
          <div class="hint" id="f-path"></div>
        </label>
        ${isProxy ? "" : `
        <label class="field full"><span>服务器介绍（MOTD）</span><input type="text" id="f-motd" value="MineDeck 开服 · 一起来玩"></label>
        <div class="field"><label class="switch"><input type="checkbox" id="f-online"><span class="sw"></span>
          <span><span class="sw-label">正版验证（online-mode）</span><div class="sw-desc">默认关闭：离线账号可直接进服，但不会使用正版 UUID，也不显示正版皮肤</div></span></label></div>
        <div class="field"><label class="switch"><input type="checkbox" id="f-flight" checked><span class="sw"></span>
          <span><span class="sw-label">允许飞行（allow-flight）</span><div class="sw-desc">默认开启：防止模组移动被误踢</div></span></label></div>
        <label class="field"><span>游戏难度</span><select id="f-diff"><option value="peaceful">和平</option><option value="easy" selected>简单</option><option value="normal">普通</option><option value="hard">困难</option></select></label>
        <label class="field"><span>默认游戏模式</span><select id="f-mode"><option value="survival" selected>生存</option><option value="creative">创造</option><option value="adventure">冒险</option><option value="spectator">旁观</option></select></label>
        ${whitelistField("f-wl")}
        <div class="hint" style="grid-column:1/-1">PVP、视距等高级参数，可在服务器创建后随时调整。</div>`}
      </div>
      <div id="wiz-world"></div>
      ${isProxy
        ? `<div class="eula-box">代理端无需签署 Mojang EULA。监听端口及转发规则将在首次启动生成配置文件后开放配置。</div>
      ${wizFoot("wback", "上一步", `<button class="btn primary" id="wcreate">${icon("bolt")} 开始创建</button>`)}`
        /* EULA 开关和吸底栏贴在一起吸底（可用性走查 U25 / R22）：以前开关在吸底栏正上方，窗口不够高时被吸底栏盖住一半 */
        : `<div class="wiz-bottom">
        <div class="eula-box">
          <label class="switch"><input type="checkbox" id="f-eula"><span class="sw"></span>
            <span class="sw-label">我已阅读并同意 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft 最终用户许可协议 (EULA)</a></span></label>
          <div class="field-err" id="f-eula-err"></div>
        </div>
        ${wizFoot("wback", "上一步", `<button class="btn primary" id="wcreate">${icon("bolt")} 开始创建</button>`)}
      </div>`}`);
    $("#wback").onclick = () => { wiz.step = wiz.core === "vanilla" ? 1 : 3; drawWizard(); }; // 原版回版本页，非 vanilla 回「选模组」
    paintWizSum();
    if (window.MDWorldImport) MDWorldImport.wizardFinal($("#wiz-world"), wiz); // 用已有地图开服：按现在的核心和版本核对这张地图
    // 行内校验：离开字段时检查，出错后随输入即时复核；提交时全部检查并聚焦第一个出错项（不再用远离字段的 toast）
    const showErr = (input, errId, msg) => { input.classList.toggle("invalid", !!msg); $(errId).innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; return !msg; };
    const checkName = () => showErr($("#f-name"), "#f-name-err", $("#f-name").value.trim() ? "" : "请给服务器起个名字");
    let portDef = 25565; // 默认填的端口（下面按 MineDeck 里别的服务器在用的换过）
    const checkPort = () => {
      if (isProxy) return true;
      const v = Number($("#f-port").value);
      return showErr($("#f-port"), "#f-port-err", Number.isInteger(v) && v >= 1 && v <= 65535 ? "" : `端口需为 1 ~ 65535 的整数，不确定就用 ${portDef}`);
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
    const fill = wiz.fill; // 「返回修改」：上次在这一步填的
    wiz.fill = null;
    renderMemoryControl($("#mem-mount"), { sliderId: "f-mem", min: 512, max: maxMem, value: fill ? fill.mem : defMem, totalMb: app.ramMb, availMb: app.availRamMb, hint: isProxy ? "推荐 1 GB；代理端只做转发，占用很低" : "推荐分配：模组服 4 GB 以上；无模组的小型服务器 2~4 GB。" });
    // 存放位置：目标路径跟随实例名，可浏览改到其它盘。文件夹名取名称里的英文字母和数字（同后端 slugify）；
    // 一个都没有（全是中文）时后端用创建那一刻的日期和时间起名，这里照实说，不再写看不懂的「server-（自动）」（可用性走查 U25）
    const clientSlug = s => { let o = ""; for (const c of (s || "")) { if (/[0-9A-Za-z_-]/.test(c)) o += c; else if (c === " " || c === ".") o += "-"; } return o.replace(/^[-_]+|[-_]+$/g, ""); };
    const stamp = () => { const d = new Date(), p = n => String(n).padStart(2, "0"); return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`; };
    const curRoot = () => wiz.root || app.serversRoot || "程序文件夹\\servers";
    const updateRoot = () => {
      $("#f-root").value = curRoot();
      const slug = clientSlug($("#f-name").value.trim());
      $("#f-path").innerHTML = slug
        ? `将创建于：<b>${esc(curRoot())}\\${esc(slug)}</b>${wiz.root ? "" : "（可点「浏览」改到其他盘）"}`
        : `将创建于：<b>${esc(curRoot())}\\server-创建时的日期和时间</b>（比如 server-${stamp()}）。名称里没有英文字母或数字，文件夹就按创建时间起名；服务器在 MineDeck 里照样叫你起的名字。`;
    };
    $("#f-name").oninput = updateRoot;
    $("#f-browse").onclick = async () => {
      try { const r = await api("/api/pickdir", { method: "POST" }); if (!stale(gen) && r.path) { wiz.root = r.path; updateRoot(); toast("已选择：" + r.path); } }
      catch (e) { toastErr("无法选择存放位置", e); }
    };
    if (fill) {
      $("#f-name").value = fill.name;
      if (!isProxy) {
        $("#f-port").value = fill.port; $("#f-motd").value = fill.motd;
        $("#f-online").checked = fill.online; $("#f-flight").checked = fill.flight; $("#f-wl").checked = fill.wl; $("#f-eula").checked = fill.eula;
        $("#f-diff").value = fill.diff; $("#f-mode").value = fill.mode;
      }
    }
    // 「返回修改」填回来的端口不换；手填的和别的服务器撞了当场提示（停着的服务器共用端口不拦，正占着或系统保留的提交时后端再拦）
    let wizPortCtl = null;
    if (!isProxy) { wizPortCtl = wirePortField($("#f-port"), $("#f-port-note"), sug, { keep: !!fill }); portDef = wizPortCtl.def; }
    updateRoot();
    $("#wcreate").onclick = async () => {
      const okName = checkName(), okPort = checkPort(), okEula = checkEula();
      if (!(okName && okPort && okEula)) {
        const first = !okName ? $("#f-name") : !okPort ? $("#f-port") : $("#f-eula");
        first.closest(".field, .eula-box").scrollIntoView({ behavior: "smooth", block: "center" });
        first.focus({ preventScroll: true });
        return;
      }
      // 用已有地图开服：核对没过（标红、没确认、还在核对）就不发请求，原因写在核对框下面；过了就带上计划编号
      const world = window.MDWorldImport ? MDWorldImport.wizardCreateFields() : null;
      if (world === false) return;
      const name = $("#f-name").value.trim();
      const createBtn = $("#wcreate");
      createBtn.disabled = true;
      /* 带地图时，请求发出去到回来这段也不能换地图、回上一步：回来时收下的是发出去的那份计划，界面上却已经换了 */
      let unlockNav = null;
      if (world) {
        const back = $("#wback"), pills = [...document.querySelectorAll(".step-pill")].map(p => [p, p.onclick, p.disabled]);
        if (back) back.disabled = true;
        pills.forEach(([p]) => { p.onclick = null; p.disabled = true; });
        MDWorldImport.wizardSubmitting(true);
        unlockNav = () => { if (back) back.disabled = false; pills.forEach(([p, fn, d]) => { p.onclick = fn; p.disabled = d; }); };
      }
      try {
        // 「返回修改」用：这一步之前选的和这一步填的，带地图的连同那张地图（见 formDrafts）。和请求一起在发请求之前读好（Codex 复审 beta.3）：
        // 等回话的时候切走了，表单没了、向导（wiz）和选的地图也可能被重新进来的新建页清掉，回来再读会出错——服务器明明在建，界面却报失败
        const redo = {
          wiz: { core: wiz.core, mc: wiz.mc, build: wiz.build, snapshots: wiz.snapshots, root: wiz.root, skipMods: wiz.skipMods, step: wiz.step },
          fields: { name, mem: +$("#f-mem").value, port: isProxy ? "" : $("#f-port").value, motd: isProxy ? "" : $("#f-motd").value,
            online: !isProxy && $("#f-online").checked, flight: !isProxy && $("#f-flight").checked, wl: !isProxy && $("#f-wl").checked,
            eula: !isProxy && $("#f-eula").checked, diff: isProxy ? "" : $("#f-diff").value, mode: isProxy ? "" : $("#f-mode").value },
          map: world && window.MDWorldImport ? MDWorldImport.draftMap() : null };
        const fv = redo.fields;
        const r = await api("/api/instances", {
          method: "POST",
          body: {
            name,
            core: redo.wiz.core, mc: redo.wiz.mc, build: redo.wiz.build || "",
            root: redo.wiz.root || "",
            xmxMb: fv.mem,
            port: isProxy ? 25565 : +fv.port,
            eula: !isProxy,
            onlineMode: fv.online,
            allowFlight: fv.flight,
            motd: fv.motd.trim(),
            difficulty: fv.diff,
            gamemode: fv.mode,
            whitelist: fv.wl, // 只允许白名单里的玩家进入：默认关，后端按它明确写 white-list
            ...(world || {}),
          },
        });
        formDrafts.set(r.taskId, { mode: "new", data: redo }); // 先按任务号记下，再看页面还在不在
        if (world) MDWorldImport.wizardCreated(world.worldPlan, r.taskId); // 请求被收下了：跟着建服任务，成了才清检测结果（见 worldimport.js）
        /* 「选模组」步挑好的内容放在草稿购物车里（不带 inst）。空车照旧直接跳部署任务页；
           有货时留在向导这一页等建服任务跑完，再把草稿车一起装上（装模组是一条 kind "mods" 的任务，跳到那页看进度）。
           「有货」两样都算：Modrinth 上挑的项目（count），和从电脑添加的本地文件（local）——只加了本地文件也要装。
           建服失败仍然跳建服任务页，失败的日志要看得见；跳转本身就是切页，startPoll 停在旧代际上自然失效。
           有货时先记下草稿清单归这台新服务器（A31），放在看页面还在不在之前：建服途中切走了也记得上；失败不挡建服。
           空车不记：记号是仅人工的写操作，记一次进一条操作记录。读草稿清单偶发失败（draft 为 null）时照样记：
           不知道是不是空车，后端在草稿清单真是空的时不记，有货却漏记的话那台的详情页就不出「新建时挑的 N 项还没装」 */
        const hasDraft = d => !!d && (d.count > 0 || (d.local || []).length > 0);
        let draft = null;
        // 原版没有「选模组」这一步，草稿车里剩下的（上次没装的）不能装进原版服；点了「跳过这一步」的这次也不装
        if (redo.wiz.core !== "vanilla" && !redo.wiz.skipMods) {
          try { draft = await api("/api/modcart"); } catch { draft = null; } // 老后端没这个接口：当作空车
          if (draft === null || hasDraft(draft)) await api("/api/modcart/draft/target", { method: "POST", body: { inst: name } }).catch(() => {});
        }
        if (stale(gen)) { toast("已开始创建，可在新建服务器页查看进度"); return; }
        if (!hasDraft(draft)) { location.hash = `#/task/${r.taskId}`; return; }
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
            /* expect / expectLocal：「选模组」那一步上画出来的那些（Modrinth 项目的 ID、本地文件的 SHA-1）。草稿车在那之后被改过
               （AI 助手加了东西），上次向导半途放弃留下的本地文件这次没在页面上列出来过，或者同名的旧购物车里还剩着东西，
               后端都会拒绝，不会把人没看过的东西装进新服务器。 */
            const seen = (window.MDModCart && MDModCart.draftSeen()) || { expect: [], expectLocal: [] };
            const r2 = await api(`/api/instances/${encodeURIComponent(name)}/modcart/install`, { method: "POST",
              body: { includeDraft: true, expect: seen.expect, expectLocal: seen.expectLocal } });
            if (stale(gen)) return;
            toast(`服务器建好了，正在安装清单里的${wizModNoun()}…`);
            location.hash = `#/task/${encodeURIComponent(r2.taskId)}`;
          } catch (e) { toastErr(`无法安装清单里的${wizModNoun()}`, e); if (!stale(gen)) location.hash = `#/task/${r.taskId}`; } // 装模组没发出去：至少让建服结果可见
        };
        say("正在等待服务器创建完成…");
        await follow();
        if (!launched && !stale(gen)) startPoll(follow, 1500, gen); // 第一次查就结束了的不用再轮询
      } catch (e) {
        if (!stale(gen) && e.portBusy && wizPortCtl) wizPortCtl.onSubmitBusy(e.portBusy);
        toastErr("无法创建服务器", e);
        if (!stale(gen) && createBtn.isConnected) createBtn.disabled = false;
        if (unlockNav && !stale(gen)) unlockNav();
        if (world && !stale(gen)) MDWorldImport.wizardCreateFailed(e, wiz); // 计划过期或对不上了就重新核对；别的原因（重名）计划还能用，改好再点
      }
    };
  }
}

/* ---------- 视图：后台任务进度 ---------- */
/* 后台任务不止「创建」一种（后端 tasks.Snapshot.kind）：接管已有服务器、换 Java、交接包、装待装清单、换地图也走这一页，说法跟着换。
   完成、失败、取消以后页头换成结果（可用性走查 R13：完成后标题还写着「正在部署服务器」）；nav 是侧栏该亮哪一项（导出交接包、换 Java 这些
   是从服务器那边发起的，亮「服务器」，不亮「新建服务器」）。cancelTitle / cancelBtn 只有能取消的几种用得到 */
const TASK_KINDS = {
  "": { title: "正在创建服务器", sub: "正在下载 Java 和服务端。", done: "创建完成", doneSub: "Java、服务端与配置均已就绪",
    doneTitle: "服务器已建好", failTitle: "服务器没有建好", cancelTitle: "已取消创建", cancelBtn: "取消创建",
    ok: "服务器创建成功", fail: "创建失败", failHint: "已下载的文件有缓存，再试一次很快。", busy: "创建中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回新建服务器", nav: "create" },
  adopt: { title: "正在接管服务器", sub: "正在检查文件夹、准备 Java。世界和配置不会动（换了端口的只改端口这一项）。", done: "接管完成", doneSub: "已经收进 MineDeck，可以在这里启动和管理了",
    doneTitle: "服务器已接管", failTitle: "服务器没有接管成",
    ok: "服务器接管成功", fail: "接管失败", failHint: "服务器文件夹里的东西没有被删掉或改坏，可以再试一次。", busy: "接管中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回新建服务器", nav: "create" },
  java: { title: "正在更换 Java", sub: "本机没有这个版本的 Java，正在下载一份（不改系统里的 Java）。", done: "Java 已换好", doneSub: "下次启动这台服务器时生效",
    doneTitle: "Java 已换好", failTitle: "Java 没有换成",
    ok: "Java 已换好，下次启动生效", fail: "更换失败", failHint: "这台服务器还在用原来的 Java，可以返回重试。", busy: "下载中，请勿关闭程序…",
    go: "完成，回到服务器", href: name => `#/inst/${encodeURIComponent(name)}/settings/jvm`, back: "#/instances", backText: "返回服务器列表", nav: "instances" },
  "transfer-out": { title: "正在导出交接包", sub: "正在把世界、配置、模组和插件打包成一个文件。服务器不会被改动。", done: "导出完成", doneSub: "交接包已生成，文件位置在这一页的日志里",
    doneTitle: "交接包已导出", failTitle: "交接包没有导出成", openDir: true,
    ok: "交接包导出成功", fail: "导出失败", failHint: "没有留下不完整的包，可以重试（包的特征见任务日志）。", busy: "打包中，请勿关闭程序…",
    go: "完成", href: () => "#/instances", back: "#/instances", backText: "返回服务器列表", nav: "instances" },
  "transfer-in": { title: "正在导入交接包", sub: "正在准备 Java、下载核心并解包。世界、配置、模组和插件都会保留。", done: "交接完成", doneSub: "世界、配置、模组和插件都带过来了，可以启动了。没带过来的东西列在下面的日志里",
    doneTitle: "交接包已导入", failTitle: "交接包没有导入成",
    ok: "交接包导入成功", fail: "导入失败", failHint: "半成品的文件夹已经清理干净，可以重试。", busy: "导入中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回新建服务器", nav: "create" },
  "server-in": { title: "正在导入服务端", sub: "正在解压服务端文件、准备 Java。世界、模组、插件和配置用压缩包里的。", done: "导入完成", doneSub: "世界、模组、插件和配置都是压缩包里的那份，可以启动了",
    doneTitle: "服务端已导入", failTitle: "服务端没有导入成", cancelTitle: "已取消导入", cancelBtn: "取消导入",
    ok: "服务端导入成功", fail: "导入失败", failHint: "半成品的文件夹已经清理干净，可以重试。", busy: "导入中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/create", backText: "返回新建服务器", nav: "create" },
  mods: { title: "正在安装待装清单", sub: "正在下载清单里的内容和它们的前置，放进服务器的 mods 或 plugins 文件夹。", done: "安装完成", doneSub: "新装的内容要重启服务器后才会生效",
    doneTitle: "待装清单已装好", failTitle: "待装清单没有装成",
    ok: "安装完成", fail: "安装失败", failHint: "可以回到这台服务器的「插件」或「模组」页，在待装清单里再装一次。已经下载的文件有缓存，重试很快。", busy: "安装中，请勿关闭程序…",
    go: "完成，进入控制台", href: name => `#/inst/${encodeURIComponent(name)}`, back: "#/instances", backText: "返回服务器列表", nav: "instances" },
  // 导入地图、换回导入前的地图（worldimport.js）。这类任务的 result 是一句给人看的话，不是服务器名：resultText 让完成后显示它；
  // 「完成」按快照里的 inst 回到那台服务器的「换地图」（A53），没有 inst 的（老版本程序的任务）回服务器列表
  world: { title: "正在更换地图", sub: "导入地图，或者换回导入前的地图。出错时会自动退回原来的世界。", done: "已完成", doneSub: "可以到这台服务器的「世界与备份 → 换地图」里查看", resultText: true,
    doneTitle: "地图已处理完", failTitle: "地图没有处理完", cancelTitle: "已取消换地图", cancelSub: "没有替换任何文件，原来的世界没动。", // 取消只在复制阶段收（换地图那一页的「取消导入」）
    ok: "地图已处理完", fail: "没有完成", failHint: "所有文件都还在。到这台服务器的「世界与备份 → 换地图」里看现在用的是哪一份。", busy: "正在处理地图，请勿关闭程序…",
    go: t => t.inst ? "完成，回到这台服务器" : "完成，回到服务器列表",
    href: (_, t) => t && t.inst ? `#/inst/${encodeURIComponent(t.inst)}/world/map` : "#/instances", back: "#/instances", backText: "返回服务器列表", nav: "instances" },
};
/* 建服、导入失败以后半成品没删干净（快照里 cleanupFailed，后端 inst.FailLeftover 记的）：失败框不接上面的 failHint
   （交接包、现成服务端那两项说的是「已经清理干净」），换这一句。只认这个字段，不按 error 里「清理半成品失败」几个字认（A48）。
   没删干净的不一定是这次建的（原因里会说），所以只说这次建了一半的在哪 */
const LEFTOVER_HINT = "文件夹没删干净：这次建了一半的会列在服务器页上方「没建完的文件夹」里，到那里处理。";
/* 侧栏亮哪一项：navigate() 进任务页时先按「新建服务器」亮着（那时还不知道是哪一类任务），拿到任务以后在这里改过来 */
function markNav(key) {
  const before = keyRects();
  let moved = false;
  document.querySelectorAll("#sidebar .nav-item").forEach(a => {
    const on = a.dataset.nav === key;
    if (on !== a.classList.contains("active")) moved = true;
    a.classList.toggle("active", on);
    if (on) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current");
  });
  if (moved) slideKeys(before, ["nav"]);
}
/* Windows 路径的上一级文件夹（交接包的完整路径 → 它所在的文件夹） */
const parentDir = p => { const s = String(p || "").replace(/[\\/]+$/, ""), k = Math.max(s.lastIndexOf("\\"), s.lastIndexOf("/")); return k > 0 ? s.slice(0, k + (s[k - 1] === ":" ? 1 : 0)) : ""; };

/* ---------- 没装上的文件（导入整合包时没能自动装上的，可用性走查 R09）----------
   按服务器查、按服务器处置（GET / POST /api/instances/{名}/import-pending）：清单存在服务器文件夹里，程序重开以后也能处理，
   跳过的也还能改成别的处理。挂在三个地方（box 放清单，out 放每一项的处置结果）：导入任务页、详情页头「处理」打开的叠层、
   「插件 / 模组」页里「整合包里没装上的」那一块。同一页只挂一份：单选组按 pnd-<编号> 起名，两份会互相抢选中。
   applyId 是「应用所选的处理」的 id（各处不同，测试和读屏按它找）。
   清单是从服务器文件夹里读来的，别人可能改过：文字一律转义，链接只画 https 的。onChange(清单) 在处置以后调：任务页拿它刷新上面的告警，
   详情页拿它刷新页头那一条。 */
const PND_ACT = { skip: "跳过", local: "用本地文件代替", trust: "信任站点并下载" };
const PENDING_BATCH = 500; // 同后端 pendingResolveMax：500 接受，501 拒绝。多出来的分批提交。
const pendingLeavePrompt = n => n > 0 ? {
  title: "还有没处理完的文件", danger: true,
  body: `还有 <b>${n}</b> 项还没提交，离开后不会继续；正在处理的这一批会照常做完。`,
  okText: "离开", cancelText: "留在这里",
} : null;
/* 分批提交没装上的文件处理方式（A33）：显示批次进度并在 leaveChecks 里登记离开确认，任务页面板和详情页叠层共用 */
async function submitPendingBatches(inst, acts, { prog, gone, onUnsubmitted }) {
  const totalBatches = Math.ceil(acts.length / PENDING_BATCH);
  let left = 0;
  const setLeft = n => { left = n; onUnsubmitted(n); };
  const unregister = registerLeaveCheck(() => gone() ? null : pendingLeavePrompt(left));
  const resolved = [];
  let lastResp = null, err = null;
  try {
    for (let i = 0; i < acts.length; i += PENDING_BATCH) {
      const batch = acts.slice(i, i + PENDING_BATCH);
      setLeft(Math.max(0, acts.length - (i + batch.length)));
      if (prog && totalBatches > 1) {
        prog.hidden = false;
        prog.textContent = `正在处理第 ${Math.floor(i / PENDING_BATCH) + 1} 批，共 ${totalBatches} 批`;
      }
      const r = await api(`/api/instances/${encodeURIComponent(inst)}/import-pending`, { method: "POST", body: { items: batch } });
      if (gone()) return { stopped: true, resolved, lastResp, err: null };
      lastResp = r;
      resolved.push(...(r.resolved || []));
    }
  } catch (e) {
    err = e;
  } finally {
    setLeft(0);
    unregister();
    if (!gone() && prog) { prog.hidden = true; prog.textContent = ""; }
  }
  return { stopped: gone(), resolved, lastResp, err };
}

function pendingPanel(box, out, inst, gen, { onChange, applyId = "t-pending-apply" } = {}) {
  let view = { pending: [], skipped: [] }, sig = "", had = false, unsubmitted = 0;
  const gone = () => stale(gen) || !box.isConnected || !!box.closest("[data-leaving]");
  const leavePrompt = () => gone() ? null : pendingLeavePrompt(unsubmitted);
  const httpsOnly = u => /^https:\/\//i.test(String(u || ""));
  const dlHost = it => {
    const u = (it.urls || []).find(httpsOnly);
    if (!u) return it.host || "";
    try { return new URL(u).hostname; } catch { return it.host || ""; }
  };
  const row = (it, skipped) => `
    <div class="${skipped ? "pnd-skipped" : "pnd-item"}" data-id="${esc(it.id)}">
      <div><b>${esc(it.path)}</b>${skipped ? ` <span class="badge">已跳过</span>` : ""}${it.host ? ` <span class="badge">${esc(it.host)}</span>` : ""}</div>
      <div class="wc-note">${esc(it.note || "")}${it.size ? `（整合包标 ${fmtBytes(it.size)}）` : ""}
        ${(it.urls || []).filter(httpsOnly).map(u => ` <a href="${esc(u)}" target="_blank" rel="noopener noreferrer">下载地址</a>`).join("")}</div>
      <div class="row pnd-acts" role="radiogroup" aria-label="没装上的文件 ${esc(it.path)} 的处理方式">
        ${(it.options || ["skip", "local"]).filter(o => PND_ACT[o]).map(o =>
          `<label class="pnd-opt"><input type="radio" name="pnd-${esc(it.id)}" value="${o}"><span>${PND_ACT[o]}</span></label>`).join("")}
      </div>
      <input type="text" class="pnd-local" placeholder="本地文件或文件夹的完整路径；填文件夹时按文件名匹配" aria-label="没装上的文件 ${esc(it.path)} 的本地文件或文件夹路径" hidden>
      <label class="pnd-ack" hidden><input type="checkbox" class="pnd-ackbox">
        <span class="wc-note">我已知晓风险，信任来自「${esc(dlHost(it) || "这个站点")}」的下载。该站点不在默认允许列表里；下载后仍按整合包里的校验值核对，对不上就不装。下载成功后按域名记住（不含端口），以后同站点不再重复确认。</span></label>
    </div>`;
  const rows = () => [...box.querySelectorAll(".pnd-item, .pnd-skipped")];
  const draw = () => {
    const { pending, skipped } = view;
    const s = JSON.stringify([pending, skipped]);
    if (s === sig) return;
    // 重画之前记下已经选了的（没处理成的那几项接着显示时原样带回去）
    const keep = {};
    for (const r of rows()) {
      const radio = r.querySelector("input[type=radio]:checked");
      keep[r.dataset.id] = { act: radio ? radio.value : "", path: (r.querySelector(".pnd-local") || {}).value || "", ack: !!(r.querySelector(".pnd-ackbox") || {}).checked };
    }
    sig = s;
    if (!pending.length && !skipped.length) {
      box.innerHTML = had ? `<div class="warn-card pnd-done"><div class="wc-title">${icon("check")} 没装上的文件都处理完了</div></div>` : "";
      return;
    }
    had = true;
    const skippedBlock = skipped.length ? `<details class="pnd-skip-list"${pending.length ? "" : " open"}><summary>已跳过的 ${skipped.length} 个（没有装上，还能改成别的处理）</summary>${skipped.map(it => row(it, true)).join("")}</details>` : "";
    box.innerHTML = `<div class="warn-card pnd-card">
      ${pending.length ? `<div class="wc-title">${icon("warn")} 没装上的文件（${pending.length} 个）</div>
      <div class="wc-note">这些文件因为校验失败、链接失效或平台限制没能自动下载。给要处理的选好方式，再点「应用所选的处理」；没选的先不动。</div>
      ${pending.some(it => (it.options || []).includes("trust")) ? `<div class="row pnd-batch">
        <button type="button" class="btn sm" id="pnd-trust-all">全选可下载项</button>
        <button type="button" class="btn sm" id="pnd-ack-all">确认这些站点的风险</button>
        <span class="hint">「全选可下载项」只选上面还没选处理方式、且可从外部站点下载的文件；已跳过的、缺校验值的、你已经选了别的处理方式的不动，也不会自动勾选风险确认。勾选确认并下载成功后才按域名记住，不会把外部站点放进默认允许列表。</span>
      </div>` : ""}
      ${pending.map(it => row(it, false)).join("")}`
      : `<div class="wc-title">${icon("info")} 跳过了 ${skipped.length} 个文件，它们没有装上</div><div class="wc-note">要的话在下面换成用本地文件代替${skipped.some(x => (x.options || []).includes("trust")) ? "或信任站点下载" : ""}；不要就不用管。</div>`}
      ${skippedBlock}
      <div class="row" style="margin-top:12px">
        <button type="button" class="btn primary pnd-apply" id="${applyId}">应用所选的处理</button>
        <span class="hint pnd-prog" aria-live="polite" hidden></span>
        <span class="hint">无法自动识别的自定义/魔改模组，可选「用本地文件代替」并指定本地文件路径。</span>
      </div>
    </div>`;
    for (const r of rows()) {
      const local = r.querySelector(".pnd-local"), ack = r.querySelector("label.pnd-ack");
      for (const radio of r.querySelectorAll("input[type=radio]")) radio.onchange = () => { local.hidden = radio.value !== "local"; ack.hidden = radio.value !== "trust"; };
      const st = keep[r.dataset.id];
      if (!st || !st.act) continue;
      const radio = r.querySelector(`input[value="${st.act}"]`);
      if (radio) { radio.checked = true; radio.dispatchEvent(new Event("change")); }
      if (st.path) local.value = st.path;
      if (st.ack) r.querySelector(".pnd-ackbox").checked = true;
    }
    box.querySelector(".pnd-apply").onclick = apply;
    const trustAll = box.querySelector("#pnd-trust-all");
    const ackAll = box.querySelector("#pnd-ack-all");
    if (trustAll) trustAll.onclick = () => {
      let n = 0;
      const sha1Ids = new Set(view.pending.filter(x => !!String(x.sha1 || "")).map(x => x.id)); // 同后端：没有校验值的不下载
      for (const r of box.querySelectorAll(".pnd-item")) {
        const radio = r.querySelector('input[type=radio][value="trust"]');
        // 没有校验值的后端不会下载；已经选了处理方式的（包括选了本地文件的）不替人改
        if (!radio || !sha1Ids.has(r.dataset.id) || r.querySelector("input[type=radio]:checked")) continue;
        radio.checked = true;
        radio.dispatchEvent(new Event("change"));
        n++;
      }
      toast(n ? `已选中 ${n} 个可下载项，请确认风险后再应用。` : "没有可信任下载的项");
    };
    if (ackAll) ackAll.onclick = () => {
      let n = 0;
      for (const r of box.querySelectorAll(".pnd-item, .pnd-skipped")) {
        const radio = r.querySelector('input[type=radio][value="trust"]:checked');
        const boxAck = r.querySelector(".pnd-ackbox");
        if (!radio || !boxAck || boxAck.closest("label").hidden) continue;
        boxAck.checked = true;
        n++;
      }
      toast(n ? `已勾选 ${n} 项风险确认，点「应用所选的处理」开始下载。` : "请先选「信任站点并下载」，再确认风险");
    };
  };
  const apply = async () => {
    const acts = [];
    let missing = "";
    const rowById = new Map(rows().map(x => [x.dataset.id, x]));
    for (const it of [...view.pending, ...view.skipped]) {
      const r = rowById.get(it.id);
      const radio = r && r.querySelector("input[type=radio]:checked");
      if (!radio) continue; // 没选的留着下一批
      const act = { id: it.id, act: radio.value };
      if (radio.value === "local") {
        act.path = (r.querySelector(".pnd-local").value || "").trim();
        if (!act.path && !missing) missing = `${it.path} 选了「用本地文件代替」但没给路径`;
      }
      if (radio.value === "trust") {
        act.url = (it.urls || []).filter(httpsOnly)[0] || "";
        act.ack = r.querySelector(".pnd-ackbox").checked;
      }
      acts.push(act);
    }
    if (missing) { toast(missing, true); return; }
    if (!acts.length) { toast("先给至少一个文件选处理方式", true); return; }
    const btn = box.querySelector(".pnd-apply"), prog = box.querySelector(".pnd-prog");
    btn.disabled = true;
    const wasOpen = view.pending.length;
    const { stopped, resolved, lastResp: batchResp, err } = await submitPendingBatches(inst, acts,
      { prog, gone, onUnsubmitted: n => { unsubmitted = n; } });
    let lastResp = batchResp;
    const syncDone = () => {
      if (!lastResp || gone()) return;
      out.innerHTML = `<div class="wc-wrap">${resolved.map(o =>
        `<div class="${o.ok ? "hint" : "err-box"}" style="margin-bottom:6px">${o.ok ? "已处理" : "处理失败"}：${esc(o.path)}${o.note ? "，" + esc(o.note) : ""}</div>`).join("")}</div>`;
      view = { pending: lastResp.pending || [], skipped: lastResp.skipped || [] };
      draw();
      if (onChange) onChange(view);
    };
    if (!stopped) {
      if (!err) {
        syncDone();
        if (wasOpen && !view.pending.length) toast("没装上的文件都处理完了");
      } else {
        // 出错的那一批可能已经在后端处理了一部分（比如连接在途中断了），手里的旧响应不准：重新要一次清单再画
        let refreshed = false;
        try {
          const v = await api(`/api/instances/${encodeURIComponent(inst)}/import-pending`);
          if (!gone()) { lastResp = v; refreshed = true; }
        } catch { /* 要不到就按手里已有的画 */ }
        if (!gone()) {
          syncDone();
          toastErr(refreshed ? "有的文件没处理完，清单已按最新情况刷新" : lastResp ? "部分文件未能处理完" : "无法处理没装上的文件", err);
        }
      }
    }
    const b = box.querySelector(".pnd-apply");
    if (!gone() && b) b.disabled = false;
  };
  const load = async () => {
    try {
      const v = await api(`/api/instances/${encodeURIComponent(inst)}/import-pending`);
      if (gone()) return;
      view = { pending: v.pending || [], skipped: v.skipped || [] };
      had = had || view.pending.length > 0 || view.skipped.length > 0;
      draw();
    } catch (e) {
      if (!gone()) box.innerHTML = errBox("拿不到这台服务器没装上的文件", e, { actions: `<button type="button" class="btn sm" id="t-pending-retry">${icon("refresh")} 重试</button>` });
      const b = box.querySelector("#t-pending-retry");
      if (b) b.onclick = load;
    }
  };
  return { load, leavePrompt };
}
/* 详情页头「这台服务器有 N 个整合包文件没装上 [处理]」打开的叠层（R09）：里面就是上面这个处理面板，处理完、跳过以后
   onChange(清单) 让页头那一条跟着变（还没处理的没了就消失）。关掉以后面板不再动（box 不在页面上了） */
function pendingModal(inst, gen, onChange, addonsLabel = "") {
  const prev = document.activeElement;
  const tid = layerId("pnd-title");
  const mask = modalLayer(`<div class="modal pnd-modal" role="dialog" aria-modal="true" aria-labelledby="${tid}">
    <h3 id="${tid}">「${esc(inst)}」里没装上的文件</h3>
    <div class="m-body"><div class="hint pnd-modal-intro">导入整合包时没能自动装上、要你来处理的文件。处理方式选好了点「应用所选的处理」；跳过的也还能再改。
      ${addonsLabel ? `跳过的以后在这台服务器的「${esc(addonsLabel)}」页里也找得到。` : ""}</div>
      <div id="d-pending-out"></div><div id="d-pending-box"><div class="sub">加载中…</div></div></div>
    <div class="m-actions"><button class="btn primary" data-x="close">关闭</button></div></div>`);
  let closed = false, confirming = false, ctl = null;
  mask._closeOnNav = () => {
    if (closed) return;
    closed = true;
    document.removeEventListener("keydown", onKey);
    dismiss(mask);
  };
  const close = async () => {
    if (closed || confirming) return;
    const prompt = ctl && ctl.leavePrompt();
    if (prompt) {
      confirming = true;
      const ok = await confirmModal(prompt);
      confirming = false;
      if (!ok || closed) return;
    }
    closed = true;
    document.removeEventListener("keydown", onKey);
    dismiss(mask);
    if (prev && prev.focus && prev.isConnected) prev.focus();
  };
  const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(); };
  document.addEventListener("keydown", onKey);
  mask.querySelector('[data-x="close"]').onclick = close;
  mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget && !hasFields(mask)) close(); }); // 选了、填了的不因为点空了丢掉
  ctl = pendingPanel(mask.querySelector("#d-pending-box"), mask.querySelector("#d-pending-out"), inst, gen, { onChange, applyId: "d-pending-apply" });
  ctl.load();
  mask.querySelector('[data-x="close"]').focus();
  return mask;
}

async function renderTaskPage(taskId) {
  const gen = viewGen;
  Main().innerHTML = pageHead("正在创建服务器", "正在下载 Java 和服务端。", "", "", "", "task-eyebrow") + `
    <div id="t-fetch-error" role="status" hidden></div>
    <div id="task-box"><div class="sub">加载中…</div></div>`;
  // 骨架只建一次，之后每轮轮询只改变化的部分：新完成的步骤对勾弹出，经验条只点亮新增的格子（逐格错峰），不整块重绘重播
  const CELLS = 30;
  // 进度和日志多久没变就提示「下载慢或者在重试」（测试报告 B-01：安装器连不上时进度条长时间不动）；测试里用 __MD_STALL_MS 调短
  const STALL_MS = window.__MD_STALL_MS || 30000;
  let ended = false; // 已结束就不再轮询 / 重复提示（首轮即结束时旧逻辑仍会挂上轮询，成功提示弹两次）
  let built = false, loading = false, label = null, lit = 0, stepState = [], lastSig = "", lastChange = Date.now();
  let panel = null; // 没装上的文件的处理面板（整合包导入完成后才有）
  let lastT = null, isLocal = false; // 「打开所在文件夹」只在本机给（后端也只认本机）：/api/app 回来以后按最后一次的任务重画底栏
  api("/api/app").then(a => { isLocal = a.local === true; if (isLocal && lastT && !stale(gen) && $("#t-foot")) paintFoot(lastT); }).catch(() => {});
  const stopPoll = () => { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } };
  const kindOf = t => Object.prototype.hasOwnProperty.call(TASK_KINDS, t.kind || "") ? TASK_KINDS[t.kind || ""] : TASK_KINDS[""]; // 不认识的 kind 按创建任务显示
  // 上面那一排：失败框、取消的说明、告警（「N 个文件待处理」处理完以后要跟着变，所以单独画，处置以后重新取一次任务再画）
  const drawMsgs = t => {
    const K = kindOf(t), draft = formDrafts.has(taskId);
    // 取消：中性的一句（不是红色的失败框），后端给的是中文那句；万一是英文的取消原因也不原样露出来（复审发现 9）
    const cancelText = /context canceled/i.test(t.error || "") ? "已取消" : (t.error || "已取消");
    $("#t-msgs").innerHTML = (t.ended && t.cancelled
      ? `<div class="task-note" role="status">${icon("info")}<span>${esc(cancelText)}。${draft ? "之前填的都还在，点「返回修改」接着改。" : ""}</span></div>`
      : t.error ? `<div class="err-box"><b>${K.fail}：</b>${esc(t.error)}<br><br>${t.cleanupFailed ? LEFTOVER_HINT : K.failHint}${draft ? "点「返回修改」回到刚才的表单，填过的都在。" : ""}
        <div style="margin-top:8px"><button class="btn sm ghost" data-copy-task-fail>${icon("copy")} 复制报错信息</button></div></div>` : "") +
      (t.warnings || []).map(w => `<div class="warn-card" data-warn="${esc(w.kind || "")}">
        <div class="wc-title">${icon("warn")} ${esc(w.title)}</div>
        ${w.note ? `<div class="wc-note">${esc(w.note)}</div>` : ""}
        ${(w.items || []).length ? `<ul class="wc-list">${w.items.map(it =>
          `<li>${/^https?:\/\//i.test(it.url || "") ? `<a href="${esc(it.url)}" target="_blank" rel="noopener">${esc(it.name)}</a>` : esc(it.name)}</li>`).join("")}</ul>` : ""}
        ${w.kind === "java-req" && t.result ? `<div class="wc-note"><a class="btn sm" href="#/inst/${encodeURIComponent(t.result)}/settings/jvm">去「设置 → 启动与内存」换 Java</a></div>` : ""}
      </div>`).join("");
    const cpf = $("#t-msgs").querySelector("[data-copy-task-fail]");
    if (cpf) cpf.onclick = () => copy(String(t.error || ""), "已复制报错信息");
  };
  const refreshMsgs = async () => {
    try { const t = await api(`/api/tasks/${encodeURIComponent(taskId)}`); if (!stale(gen) && $("#t-msgs")) drawMsgs(t); } catch { /* 下次进这一页再看 */ }
  };
  // 底下的按钮：完成去服务器；没成或取消了「返回修改」（回到原来的表单，填过的都在；这一页刷新过、记不得填了什么时回新建页）
  const paintFoot = t => {
    const K = kindOf(t), success = t.ended && !t.error, cancelled = t.ended && !!t.cancelled;
    const canEdit = t.ended && !success && formDrafts.has(taskId);
    const openDir = success && K.openDir && t.result && isLocal;
    // 导入整合包留下了没装上的文件的清单：服务器文件夹里的「导入待决清单.txt」，在处理面板上点了「跳过」的也还记在里面
    // （导入时就没装进来的那些列在上面的「没有装进来」告警里，不在这份清单里）。原版服没有插件 / 模组页，点了跳过以后
    // 详情页也不再提，给个打开服务器文件夹的按钮，导入完了也找得回来（A46）。只在本机（后端也只认本机）
    const openList = success && panel && t.result && isLocal;
    $("#t-foot").innerHTML = `
      ${success ? `<a class="btn primary" href="${K.href(t.result, t)}">${icon("check")} ${esc(typeof K.go === "function" ? K.go(t) : K.go)}</a>` : ""}
      ${openDir ? `<button type="button" class="btn" id="t-open-dir">${icon("folder")} 打开所在文件夹</button>` : ""}
      ${openList ? `<button type="button" class="btn" id="t-open-inst-dir" title="里面的「导入待决清单.txt」列着没装上的文件，处理时点了「跳过」的也在">${icon("folder")} 打开服务器文件夹</button>` : ""}
      ${canEdit ? `<button type="button" class="btn${cancelled ? " primary" : ""}" id="t-back-edit">${icon("arrowLeft")} 返回修改</button>` : ""}
      ${t.ended && !success && !canEdit ? `<a class="btn" href="${K.back}">${icon("arrowLeft")} ${K.backText}</a>` : ""}
      ${!t.ended ? `<span class="warn-text">${icon("warn")} ${t.cancelling ? "正在取消，清理半成品，请勿关闭程序…" : K.busy}</span>` : ""}
      ${!t.ended && t.canCancel ? `<button class="btn ghost" id="t-cancel">${K.cancelBtn || "取消"}</button>` : ""}`;
    const backEdit = $("#t-back-edit");
    if (backEdit) backEdit.onclick = () => { if (!backToForm(taskId)) location.hash = K.back; };
    const od = $("#t-open-dir");
    if (od) od.onclick = async () => {
      try { await api("/api/openpath", { method: "POST", body: { path: parentDir(t.result) } }); }
      catch (e) { toastErr("无法打开交接包所在的文件夹", e); }
    };
    const oid = $("#t-open-inst-dir");
    if (oid) oid.onclick = async () => {
      try { await api(`/api/instances/${encodeURIComponent(t.result)}/opendir`, { method: "POST" }); }
      catch (e) { toastErr("无法打开服务器文件夹", e); }
    };
    const cancelButton = $("#t-cancel");
    if (cancelButton) cancelButton.onclick = async () => {
      cancelButton.disabled = true;
      try {
        await api(`/api/tasks/${encodeURIComponent(taskId)}/cancel`, { method: "POST" });
        toast("正在取消，等待清理半成品");
        await draw();
      } catch (e) { toastErr("暂时无法取消", e); cancelButton.disabled = false; }
    };
  };
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
      // 每轮轮询失败都会走到这里：内容没变就不重画，免得「复制报错信息」正要点时被换掉、读屏一遍遍重念
      const html = errBox("无法刷新任务进度", e, ended ? {} : { hint: "正在自动重试" });
      if (err.dataset.html !== html) { err.innerHTML = html; err.dataset.html = html; }
      if (ended && !built) $("#task-box").innerHTML = `<a class="btn" href="#/create">返回新建服务器</a>`;
      return;
    } finally { loading = false; }
    if (stale(gen) || ended) return;
    $("#t-fetch-error").hidden = true;
    const K = kindOf(t);
    const h1 = Main().querySelector(".page-head h1"), sub = Main().querySelector(".page-head .sub");
    if (!built) {
      markNav(K.nav || "create");
      if (K !== TASK_KINDS[""]) { // 页头先按「创建」画的，知道是哪一类任务后换成它的说法
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
          <div class="wc-note">下载慢或在重试。可以检查网络或代理；网络恢复后会自动接着下载，不需要重新开始。</div></div>
        <div id="t-msgs"></div>
        <div id="t-pending-out"></div>
        <div id="t-pending"></div>
        <div class="task-log" id="task-log"></div>
        <div class="save-bar" id="t-foot"></div>`;
      built = true;
    }
    // 结束了：页头换成结果（完成 / 没成 / 已取消），不再挂着「正在……」
    const success = t.ended && !t.error, cancelled = t.ended && !!t.cancelled;
    if (t.ended) {
      if (h1) h1.textContent = success ? K.doneTitle : cancelled ? (K.cancelTitle || "已取消") : K.failTitle;
      if (sub) sub.textContent = success ? (K.resultText && t.result ? t.result : K.doneSub) : cancelled ? (K.cancelSub || "没有留下半成品。") : "原因写在下面。";
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
        : st.status === "running" ? icon("spinner") : st.status === "error" ? icon("x") : st.status === "cancelled" ? `<i class="ts-dash"></i>` : `<i class="ts-px"></i>`;
    });
    const doneN = steps.filter(s => s.status === "done").length;
    $("#task-eyebrow").innerHTML = !steps.length ? "" : cancelled ? "已取消"
      : success ? `已完成${SEP}共 ${steps.length} 步` : `第 ${Math.min(steps.length, doneN + 1)} 步<span class="sep">/</span>共 ${steps.length} 步`;
    // 经验条式进度：30 格，一格 = 当前下载项的 1/30；换了下载项就清空重来；完成时填满并整条闪一下
    const prog = $("#t-prog"), cells = prog.querySelectorAll(".xp i");
    const cur = success ? K.done : cancelled ? "" : t.label;
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
      $("#t-sub").textContent = success ? (K.resultText && t.result ? t.result : K.doneSub) : `${fmtBytes(t.done) || "0 B"}${t.total > 0 ? " / " + fmtBytes(t.total) : ""}`;
      if (success) playClass(prog.querySelector(".xp"), "flash");
    }
    drawMsgs(t);
    // 没装上的文件：整合包导入完成、任务上记着还有没处理的，就按这台服务器去取清单（跳过的也列出来，还能改成别的处理）
    if (!panel && success && t.result && (t.pending || []).length) {
      panel = pendingPanel($("#t-pending"), $("#t-pending-out"), t.result, gen, { onChange: refreshMsgs });
      panel.load();
    }
    const lg = $("#task-log"), text = (t.log || []).join("\n");
    if (lg.textContent !== text) { lg.textContent = text; lg.scrollTop = lg.scrollHeight; }
    // 下载慢 / 在重试：后端报了刚发生的超时、重试（t.net，一分钟内的），或者进度、步骤、日志 STALL_MS 没有任何变化。
    // 只在文字变了时才改，免得读屏软件每轮都念一遍
    const sig = JSON.stringify([steps.map(s => s.status), t.label, t.done, t.total, (t.log || []).length, (t.log || []).slice(-1)[0]]);
    if (sig !== lastSig) { lastSig = sig; lastChange = Date.now(); }
    const idle = Date.now() - lastChange, net = t.net && t.net.ago <= 60 ? t.net : null;
    const slowEl = $("#t-slow");
    slowEl.hidden = t.ended || t.cancelling || !(net || idle >= STALL_MS);
    if (!slowEl.hidden) {
      const sec = Math.floor(idle / 1000);
      const title = net ? `${net.host ? net.host + " " : ""}${net.what}，已重试 ${net.count} 次`
        : `已经 ${sec < 10 ? sec + " 秒" : sec < 60 ? Math.floor(sec / 10) * 10 + " 多秒" : Math.floor(sec / 60) + " 分多钟"}没有新进度`;
      if ($("#t-slow-title").textContent !== title) $("#t-slow-title").textContent = title;
    }
    lastT = t;
    paintFoot(t);
    if (t.ended) { ended = true; stopPoll(); if (success) toast(K.ok); }
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

/* 详情页头下面的常驻提示区（#d-notices，见 docs/文案重写/04-标签与术语-beta3.md 第二节）：上次启动失败、模组要求更高的 Java，
   以后还有整合包里没装上的文件、Java 路径失效……有就显示，事情解决了就消失，不靠右上角会自己消失的提示。
   每条有自己的键，各自出现、更新、消失；html 为空就拿掉这一条。内容没变不重画（轮询里反复调也不闪、不打断点击）。
   id：这一条元素的 id（老的选择器还在用的，比如 #d-java-hint）。按钮的点击用 data-* 委托或 onclick 都行，重画以后要重新接 */
function detailNotice(key, html, { id = "", kind = "warn" } = {}) {
  const box = $("#d-notices");
  if (!box) return null;
  let el = box.querySelector(`[data-notice="${key}"]`);
  if (!html) { if (el) { el.remove(); box.hidden = !box.childElementCount; placeToasts(); } return null; }
  if (!el) {
    el = document.createElement("div");
    el.dataset.notice = key;
    if (id) el.id = id;
    box.appendChild(el);
  }
  el.className = "d-notice " + kind;
  const wasHidden = box.hidden;
  box.hidden = false; // 先露出来再量：藏着的提示区 placeToasts 不算
  if (el._html !== html) { el._html = html; el.innerHTML = html; placeToasts(); } // 提示区变高变矮：右上角的提示跟着让开（见 placeToasts）
  else if (wasHidden) placeToasts();
  return el;
}

/* 开服、重启没成功时，提示和「详情」里给的修复去处（回访走查 11-16）：失败的原因后端已经记进了实例列表
   （找不到配的 Java：javaMissing；端口被占：startPortBusy），按它给「去换 Java」或「改端口」，不去猜报错原文。
   查不到或者不是这两种就不给。改端口去「设置 → 常用」里的端口那一项（同详情页提示区的「改端口」） */
async function startFixes(name) {
  let cur;
  try { cur = (await api("/api/instances")).find(i => i.name === name); } catch { return []; }
  if (!cur) return [];
  const base = `#/inst/${encodeURIComponent(name)}/`;
  const go = h => { if (location.hash === h) navigate(); else location.hash = h; };
  if (cur.javaMissing) return [{ label: "去换 Java", aria: `去「${name}」的「设置 → 启动与内存」换一个 Java`, close: true, onClick: () => go(base + "settings/jvm") }];
  if (cur.startPortBusy && cur.startPortBusy.port) return [{ label: "改端口", aria: `去「${name}」的「设置 → 常用」改端口`, close: true,
    onClick: () => { focusPropKey = "server-port"; go(base + "settings/common"); } }];
  return [];
}

/* 详情页的七个标签和子页（docs/文案重写/04-标签与术语-beta3.md 第一节，可用性走查 R06）。编号是地址里 #/inst/<名字>/<编号>/<子页> 的那两段，
   各处代码、AI 助手、提示和书签互相引用，定下来就不改。这台服务器用不上的不列：代理端没有玩家、世界与备份，原版没有插件 / 模组，
   扩展一个都没有时不出现。有子页的标签不带子页时进第一个子页（设置进「常用」，代理端进「启动与内存」） */
function detailTabs(info) {
  const proxy = info.kind === "proxy";
  const tabs = [{ id: "console", label: "控制台" }];
  if (!proxy) {
    tabs.push({ id: "players", label: "玩家" });
    tabs.push({ id: "world", label: "世界与备份",
      subs: [["backups", "备份"], ...(window.MDWorldImport && MDWorldImport.applies(info) ? [["map", "换地图"]] : [])] }); // 换地图与恢复点，见 worldimport.js
  }
  if (window.MDModCart && MDModCart.applies(info)) tabs.push({ id: "addons", label: MDModCart.tabLabel(info.core) }); // 已安装 + 热门、搜索、从电脑添加、待装清单，见 modcart.js
  tabs.push({ id: "tasks", label: "定时任务" });
  tabs.push({ id: "settings", label: "设置", subs: proxy ? [["jvm", "启动与内存"]] : [["common", "常用"], ["jvm", "启动与内存"], ["raw", "全部配置"]] });
  const ext = [];
  if (window.MDGameChat && MDGameChat.applies(info)) ext.push(["gameai", "游戏内 AI"]);    // 第四阶段，见 gamechat.js
  if (window.MDCompanion && MDCompanion.applies(info)) ext.push(["companion", "AI 伙伴"]);  // 第七阶段，见 companion.js
  if (window.MDMusic && MDMusic.applies(info.core)) ext.push(["music", "音乐共享"]);        // 第五阶段，见 music.js
  if (ext.length) tabs.push({ id: "ext", label: "扩展", subs: ext });
  return tabs;
}

/* 设置 → 常用里要一进来就滚到、点亮的那一项（「启动与内存」页顶的端口点过来时用），用过就清 */
let focusPropKey = "";

/* 停止、重启之前：有玩家在线就先问一声，写出几个人会掉线（可用性走查 R07）。没人在线直接返回 true */
async function confirmKick(info, what) {
  const n = info.onlineCount || 0;
  if (!n || info.kind === "proxy") return true;
  return !!(await confirmModal({
    title: `${what}「${info.name}」？`, danger: true, icon: what === "重启" ? "refresh" : "stop",
    okText: `${what}服务器`,
    body: `现在有 <b>${n}</b> 位玩家在线，${what === "重启" ? "重启时他们会掉线，等服务器重新开好以后再进" : "停服以后他们会掉线"}。服务器会先保存世界。`,
  }));
}
/* 重启（R07）：调现成的 POST /api/instances/{name}/restart，先保存世界并停止，停下来以后再启动。after()：请求发出以后刷新页面上的状态 */
async function restartInstance(info, after) {
  if (!(await confirmKick(info, "重启"))) return false;
  toast(`「${info.name}」正在重启：先保存世界并停止，停下来以后再启动`);
  const t = after ? setTimeout(after, 400) : 0; // 请求要等服务器停下、再起来才回：先刷一次，状态胶囊马上变成「正在停止」
  try { await api(`/api/instances/${encodeURIComponent(info.name)}/restart`, { method: "POST" }); return true; }
  catch (e) { toastErr("无法重启服务器", e, { actions: await startFixes(info.name) }); return false; }
  finally { clearTimeout(t); if (after) after(); }
}
/* 卡片「···」和详情页头「···」共用的两项：导出交接包、删除服务器（R06：详情页也能找到；卡片上的保留）。
   onDeleted：移出列表或删除以后做什么（卡片重画列表，详情页回到列表） */
function instMoreItems(info, onDeleted) {
  const busy = info.status !== "stopped";
  return [
    { icon: "share", label: "导出交接包…", disabled: busy, note: busy ? "需先停止服务器" : "",
      onClick: () => transferExportModal(info) },
    { sep: true },
    { icon: "trash", label: "删除服务器…", danger: true, disabled: busy, note: busy ? "需先停止服务器" : "",
      onClick: async () => { if (await deleteInstance(info)) onDeleted(); } },
  ];
}

const liveGamerules = new Map(); // 实例名 → { [键名]: { val, disk } }：运行中发过 /gamerule 的项，存档落盘前保持刚切的状态

async function renderDetail(name, path = "console") {
  const gen = viewGen;
  let list;
  // /api/app 只为连接地址里的本机 IP：详情页不等它（慢了也先画出来），拿到以后补上地址那一行；拿不到就说没查到 IP
  const appInfo = api("/api/app").catch(() => ({}));
  try { list = await api("/api/instances"); } catch (e) { if (!stale(gen)) Main().innerHTML = errBox("无法获取服务器信息", e); return; }
  if (stale(gen)) return; // 等待期间已切走：不再建 SSE / 定时器
  const info = list.find(i => i.name === name);
  if (!info) { Main().innerHTML = `<div class="err-box">服务器不存在</div>`; return; }
  // 标签和子页：地址里的这台服务器用不上（代理端的「常用」、原版的插件 / 模组、没有的扩展）就落到能用的地方，地址原地换掉、不多一条历史记录
  const TABS = detailTabs(info);
  let [tab, sub = ""] = String(path || "console").split("/");
  const curTab = TABS.find(t => t.id === tab) || TABS[0];
  tab = curTab.id;
  sub = curTab.subs ? (curTab.subs.find(s => s[0] === sub) || curTab.subs[0])[0] : "";
  const enc = encodeURIComponent(name), base = `#/inst/${enc}/`;
  const canon = tab + (sub ? "/" + sub : "");
  if (canon !== path) { history.replaceState(null, "", base + canon); curHash = base + canon; }
  // 分派用的页面名：两级合成一级（世界与备份的「换地图」叫 worldmap，其余子页就叫子页的编号）
  const view = tab === "world" ? (sub === "map" ? "worldmap" : "backups") : (tab === "settings" || tab === "ext") ? sub : tab;
  let ips = knownIPs; // null：还在问 /api/app、以前也没拿到过
  // 页头：大号像素头像 + 标题，标题下一行是状态胶囊与徽标，再下一行是连接地址；操作按钮靠右。页头下面是常驻提示区，再下面是标签（有子页的再一行）
  // 「设置」和侧栏的「设置」同名、去的地方不同：读屏和自动化按名字分不清，可访问名称带上服务器名（「设置 生存服」）
  const tabLink = (id, label, on, href) => `<a class="tab ${on ? "cur" : ""}" data-t="${id}" href="${href}"${id === "settings" ? ` aria-label="设置 ${esc(name)}"` : ""}${on ? ` aria-current="page"` : ""}>${label}</a>`;
  Main().innerHTML = `<nav class="crumbs" aria-label="位置"><a href="#/instances">服务器</a>${icon("chevronRight")}<span>${esc(name)}</span></nav>
    <div class="detail-head">
      ${avatarOf(info.core, "lg")}
      <div class="dh-text"><h1>${esc(name)}</h1>
        <div class="dh-line"><span id="d-status">${headStatus(info)}</span>${coreBadge(info.core)}<span class="badge">${info.kind === "proxy" ? "v" : "MC "}${esc(info.mc)}</span><span class="badge" id="d-java">Java ${info.javaMajor}</span></div>
        <div class="dh-addr" id="d-addr"></div></div>
      <span class="grow"></span>
      <div class="dh-actions">
      <button class="btn${info.status === "stopped" ? " primary" : ""}" id="d-start" aria-label="启动 ${esc(name)}" ${info.status !== "stopped" ? "disabled" : ""}>${icon("play")} 启动</button>
      <button class="btn${info.status === "running" || info.status === "starting" ? " primary" : ""}" id="d-stop" aria-label="停止 ${esc(name)}" ${info.status === "stopped" || info.status === "stopping" ? "disabled" : ""}>${icon("stop")} 停止</button>
      <button class="btn" id="d-restart" aria-label="重启 ${esc(name)}" ${info.status !== "running" ? "disabled" : ""}>${icon("refresh")} 重启</button>
      <button class="btn" id="d-dir" aria-haspopup="menu">${icon("folder")} 打开文件夹 ${icon("chevronDown")}</button>
      <button class="btn ghost icon-only" id="d-more" title="更多操作" aria-label="更多操作 ${esc(name)}" aria-haspopup="menu">${icon("more")}</button>
      </div>
    </div>
    <div class="d-notices" id="d-notices" hidden></div>
    <div class="tabs" aria-label="${esc(name)} 的标签">${TABS.map(t => tabLink(t.id, t.label, t.id === tab, base + t.id)).join("")}</div>
    ${curTab.subs ? `<div class="tabs sub-tabs" aria-label="${esc(curTab.label)} 的子页">${curTab.subs.map(([id, label]) => tabLink(id, label, id === sub, base + tab + "/" + id)).join("")}</div>` : ""}
    <div id="tab-body" class="sec-enter"></div>`;
  if (samePage) slideKeys(keysBefore, ["tab"]); // 同一实例换标签：键帽从旧标签滑到新标签
  // 连接地址（R01）：本机局域网 IP + 朋友连的端口，带复制；本机有几个 IP 时可以换（记在本机，卡片也跟着用）。
  // 停着时也显示，灰字写明开服后朋友连这个。端口、IP、运行状态变了才重画，不打断用户正在选中的文字
  let addrKey = "", lastCur = info;
  const paintAddr = cur => {
    lastCur = cur;
    const el = $("#d-addr"), port = connectPortOf(cur), ip = pickLanIP(ips || []), live = cur.status === "running";
    const key = [port, ip, live, ips === null].join("|");
    if (!el || key === addrKey) return;
    addrKey = key;
    el.classList.toggle("off", !live);
    const k = `<span class="dh-addr-k">连接地址</span>`;
    if (!port) { el.innerHTML = k + `<span class="dh-addr-note">代理端的端口写在它自己的配置文件里，第一次启动、生成配置文件以后才知道</span>`; return; }
    if (!ip) { el.innerHTML = k + `<span class="mono dh-addr-v">端口 ${port}</span>` + (ips === null ? "" : `<span class="dh-addr-note">没查到这台电脑的局域网 IP，确认电脑连着网络</span>`); return; }
    const addr = `${ip}:${port}`;
    el.innerHTML = k + `<span class="mono dh-addr-v" id="d-addr-v">${esc(addr)}</span>
      <button type="button" class="btn sm" id="d-addr-copy" aria-label="复制连接地址">${icon("copy")} 复制</button>
      ${ips.length > 1 ? `<button type="button" class="btn sm ghost" id="d-addr-ip" aria-haspopup="menu" aria-label="换一个 IP（本机有 ${ips.length} 个局域网 IP）">换一个 IP</button>` : ""}
      <span class="dh-addr-note">${live ? "同一局域网的朋友在游戏里添加服务器时填这个" : "开服后朋友连这个地址"}</span>`;
    $("#d-addr-copy").onclick = e => copyFlash(e.currentTarget, addr, "已复制连接地址：" + addr);
    const sw = $("#d-addr-ip");
    if (sw) sw.onclick = () => dropMenu(sw, [{ header: "本机的局域网 IP（家用路由器下通常是 192.168 开头的）" },
      ...ips.map(x => ({ icon: x === ip ? "check" : "globe", label: x, onClick: () => {
        try { localStorage.setItem("amh_lan_ip", x); } catch {}
        addrKey = ""; paintAddr(lastCur);
      } }))]);
  };
  // 上次没启动起来（R03）：常驻在页头下面，下次启动成功才消失。原文带路径时只写结论，原文用复制键拿。
  // 端口被占着（R08，startPortBusy）：被 MineDeck 里另一台服务器占着时点它的名字（名字可能很长，不按 60 字的规矩截成结论），
  // 给「改成 N 并启动」：一步改端口、保存、启动；被别的程序占着（或者找不到能改成的端口）给「改端口」，去「设置 → 常用」里的端口那一项
  const goPortSetting = () => {
    focusPropKey = "server-port";
    const h = base + "settings/common";
    if (location.hash === h) navigate(); else location.hash = h;
  };
  const fixPort = async (btn, n) => {
    btn.disabled = true;
    try { await api(`/api/instances/${enc}/properties`, { method: "PUT", body: { pairs: [{ key: "server-port", value: String(n) }] } }); }
    catch (e) { toastErr(`无法把端口改成 ${n}`, e); btn.disabled = false; return; }
    try { await api(`/api/instances/${enc}/start`, { method: "POST" }); toast(`端口改成了 ${n}，正在启动。朋友要用新的连接地址`); }
    catch (e) { toastErr(`端口改成了 ${n}，但还是没能启动`, e); }
    refreshHead();
  };
  const paintFail = cur => {
    if (!cur.lastStartError || cur.status !== "stopped") { detailNotice("start-fail", ""); return; }
    const pb = cur.startPortBusy && cur.startPortBusy.port ? cur.startPortBusy : null;
    const { text, raw } = pb && pb.owner ? { text: `上次没能启动：端口 ${pb.port} 被你的服务器「${pb.owner}」占着，两台不能同时开`, raw: cur.lastStartError }
      : errSummary("上次没能启动", { message: cur.lastStartError });
    const fix = !pb ? "" : pb.owner && pb.suggest
      ? `<button type="button" class="btn sm" data-port-fix="${+pb.suggest}">改成 ${+pb.suggest} 并启动</button>`
      : `<button type="button" class="btn sm" data-port-edit aria-label="改端口：去「设置 → 常用」里的端口">改端口</button>`;
    const el = detailNotice("start-fail", `${icon("warn")}<span class="dn-text">${esc(text)}</span>` + fix +
      (tab !== "console" ? `<a class="btn sm ghost" href="#/inst/${encodeURIComponent(name)}/console">看控制台</a>` : "") +
      (text.endsWith(raw) ? "" : `<button type="button" class="btn sm ghost" data-err-raw="${errRawAttr(raw)}" aria-label="复制报错信息 上次没能启动">${icon("copy")} 复制报错信息</button>`) +
      `<button type="button" class="btn sm ghost" data-start-dismiss aria-label="关闭这条启动失败提示">关闭提示</button>`,
    { id: "d-start-fail" });
    if (!el) return;
    const fb = el.querySelector("[data-port-fix]"), eb = el.querySelector("[data-port-edit]"), db = el.querySelector("[data-start-dismiss]");
    if (fb) fb.onclick = () => fixPort(fb, +fb.dataset.portFix);
    if (eb) eb.onclick = goPortSetting;
    if (db) db.onclick = async () => {
      db.disabled = true;
      try {
        await api(`/api/instances/${enc}/start-error`, { method: "DELETE" });
        cur.lastStartError = "";
        cur.startPortBusy = null;
        paintFail(cur);
        placeToasts();
      } catch (e) { toastErr("无法关闭提示", e); db.disabled = false; }
    };
  };
  // 配的 Java 找不到了（R21）：常驻在提示区，带路去「设置 → 启动与内存」换一个；换好了（javaMissing 没了）就消失
  const paintJava = cur => {
    if (!cur.javaMissing) { detailNotice("java-missing", ""); return; }
    detailNotice("java-missing", `${icon("warn")}<span class="dn-text">找不到这台服务器配的 Java${cur.javaPath ? `（<span class="mono">${esc(cur.javaPath)}</span>）` : ""}，现在启动会失败。可能是那个 Java 被卸载或挪走了。</span>` +
      (view !== "jvm" ? `<a class="btn sm" href="${base}settings/jvm">去「设置 → 启动与内存」换一个</a>` : ""), { id: "d-java-missing" });
  };
  // 整合包里没装上的文件（R09）：清单存在服务器文件夹里，程序重开以后也在。还没处理的有几个就常驻一条，「处理」打开处理面板
  //（在「插件 / 模组」页上就滚到页里「整合包里没装上的」那一块，一页只挂一份面板）；跳过的不算，都处理完、都跳过了就消失
  const paintPending = v => {
    const n = v ? (v.pending || []).length : 0;
    if (!n) { detailNotice("import-pending", ""); return; }
    const el = detailNotice("import-pending", `${icon("warn")}<span class="dn-text">这台服务器有 ${n} 个整合包文件没装上，缺了它们服务器可能启动不了，或者玩家进不去。</span>` +
      `<button type="button" class="btn sm" data-pnd-open aria-label="处理没装上的 ${n} 个整合包文件">处理</button>`, { id: "d-import-pending" });
    if (el) el.querySelector("[data-pnd-open]").onclick = () => {
      const blk = $("#ad-pending");
      if (blk && !blk.hidden) { blk.scrollIntoView({ behavior: reducedMotion() ? "auto" : "smooth", block: "start" }); $("#ad-pnd-h").focus({ preventScroll: true }); return; }
      pendingModal(name, gen, paintPending, window.MDModCart && MDModCart.applies(info) ? MDModCart.tabLabel(info.core) : "");
    };
  };
  const pendingReq = info.kind === "proxy" ? Promise.resolve(null) : api(`/api/instances/${enc}/import-pending`).catch(() => null);
  paintAddr(info);
  appInfo.then(a => { const r = learnIPs(a); if (!stale(gen)) { ips = r; paintAddr(lastCur); } });
  paintFail(info);
  paintJava(info);
  pendingReq.then(v => { if (!stale(gen)) paintPending(v); });
  placeToasts();
  // 各页跟着实时状态变的东西（保存条上「运行中要重启」的说明、「立即重启」、插件 / 模组页的停用删除能不能点……）登记在这里，轮询到状态就调
  const onStatus = [];
  $("#d-dir").onclick = () => dropMenu($("#d-dir"), folderItems(lastCur));
  $("#d-more").onclick = () => dropMenu($("#d-more"), instMoreItems(lastCur, () => { location.hash = "#/instances"; }));
  $("#d-start").onclick = async () => {
    try { $("#d-start").disabled = true; await api(`/api/instances/${enc}/start`, { method: "POST" }); toast("正在启动，首次生成世界可能需要一两分钟"); refreshHead(); }
    catch (e) { // 没起来的原因马上常驻到页头下面；提示和「详情」里也给修复的去处
      const fixes = await startFixes(name);
      toastErr("无法启动服务器", e, { actions: fixes });
      if (!stale(gen)) { $("#d-start").disabled = false; refreshHead(); }
    }
  };
  $("#d-stop").onclick = async () => {
    if (!(await confirmKick(lastCur, "停止"))) return;
    try { $("#d-stop").disabled = true; await api(`/api/instances/${enc}/stop`, { method: "POST" }); toast("正在保存世界并停止"); refreshHead(); }
    catch (e) { toastErr("无法停止服务器", e); $("#d-stop").disabled = false; }
  };
  $("#d-restart").onclick = async () => {
    $("#d-restart").disabled = true;
    if (!(await restartInstance(lastCur, refreshHead)) && !stale(gen)) refreshHead();
  };
  const refreshHead = async () => {
    try {
      const l = await api("/api/instances");
      if (stale(gen)) return;
      const cur = l.find(i => i.name === name);
      if (!cur) return;
      const was = info.status;
      info.status = cur.status; // 各 tab 读到的状态随之更新
      info.onlineCount = cur.onlineCount;
      $("#d-status").innerHTML = headStatus(cur);
      animatePill($("#d-status .st-pill"), was, cur.status); // 状态变了：胶囊变色、文字换场，进入运行中放一圈波纹
      $("#d-start").disabled = cur.status !== "stopped";
      $("#d-stop").disabled = cur.status === "stopped" || cur.status === "stopping";
      $("#d-restart").disabled = cur.status !== "running";
      $("#d-start").classList.toggle("primary", cur.status === "stopped");
      $("#d-stop").classList.toggle("primary", cur.status === "running" || cur.status === "starting");
      paintAddr(cur); // 在「设置 → 常用」里改了端口：地址跟着变
      paintFail(cur);
      paintJava(cur);
      onStatus.forEach(fn => { try { fn(cur, was); } catch {} });
    } catch {}
  };
  startPoll(refreshHead, 2500, gen);

  // 模组声明了比实例更高的 Java 要求时给提示（不阻断：1.20.1 配 Java 25 的魔改模组是允许的组合），放在页头下面的提示区。
  // 只在有模组目录的核心上查一次；vanilla / 代理端会 400，静默忽略。
  if (info.kind !== "proxy" && info.core !== "vanilla") {
    api(`/api/instances/${enc}/mods`).then(r => {
      if (stale(gen) || !r || !r.needJava || r.needJava <= (info.javaMajor || 0)) return;
      detailNotice("java-need", `<a class="warn-text" href="${base}settings/jvm">${icon("warn")} 有模组要求 Java ${r.needJava}，这台服务器现在是 Java ${info.javaMajor}，启动可能失败。可在「设置 → 启动与内存」里换 Java。</a>`, { id: "d-java-hint" });
    }).catch(() => {});
  }

  // 设置类页面的保存条（R07、R16）：「运行中：保存后要重启才生效」按实时状态写，停着时不写；保存成功、服务器在运行、
  // 改的项要重启才生效时，保存条上给「立即重启」（重启以后、或者停过一次服务器，就不用了）
  const liveBarNote = () => `<span class="warn-text sb-live" hidden>${icon("warn")} <span class="sb-live-t"></span></span>` +
    `<button type="button" class="btn sm" data-sb="restart" hidden aria-label="立即重启 ${esc(name)}">${icon("refresh")} 立即重启</button>`;
  const wireLiveBar = (root, liveText) => {
    const bar = $(".save-bar.sticky", root), live = $(".sb-live", bar), rb = $('[data-sb="restart"]', bar);
    let pending = false; // 存过要重启才生效的改动，服务器还没重启过
    const paint = () => {
      if (!bar.isConnected) return;
      const run = info.status !== "stopped";
      live.hidden = !run && !pending;
      $(".sb-live-t", live).textContent = pending ? "已保存，重启服务器以后生效" : liveText;
      rb.hidden = !(pending && info.status === "running");
    };
    rb.onclick = async () => {
      rb.disabled = true;
      if (await restartInstance(info, refreshHead)) pending = false;
      rb.disabled = false;
      paint();
    };
    // 不在运行了（停下、正在停、正在启动，比如页头点了重启）：下次起来就是新的设置，「立即重启」不用了
    onStatus.push(cur => { if (cur.status !== "running") pending = false; paint(); });
    paint();
    return { saved: needRestart => { if (needRestart && info.status === "running") pending = true; paint(); } };
  };

  const body = $("#tab-body");
  if (view === "console") {
    body.innerHTML = `
      <div class="console-bar">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="con-filter" placeholder="过滤日志（如 ERROR / 玩家名）" aria-label="过滤日志"></div>
        <label class="switch"><input type="checkbox" id="con-scroll" checked><span class="sw"></span><span class="sw-label">自动滚动</span></label>
        <span class="grow"></span>
        <span class="hint">编码</span>
        <select id="con-enc" style="width:110px" aria-label="控制台编码">
          ${["auto", "utf-8", "gbk"].map(e => `<option value="${e}" ${info.consoleEncoding === e ? "selected" : ""}>${e === "auto" ? "自动" : e.toUpperCase()}</option>`).join("")}
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
      } catch (e) { toastErr("无法切换控制台编码", e); }
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
      catch (e) { toastErr("无法发送命令", e); }
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

  } else if (view === "players") {
    const act = async (action, player) => {
      try {
        await api(`/api/instances/${encodeURIComponent(name)}/players`, { method: "POST", body: { action, player } });
        toast(({ "whitelist-add": `已把 ${player} 加入白名单`, "whitelist-remove": `已把 ${player} 移出白名单`, op: `已把 ${player} 设为管理员`, deop: `已取消 ${player} 的管理员`, kick: `已踢出 ${player}`, ban: `已封禁 ${player}`, pardon: `已解除 ${player} 的封禁` }[action] || `已执行 ${action} ${player}`) + (info.status === "stopped" ? "" : "（名单文件稍后由服务器刷新）"));
        setTimeout(draw, 800);
      } catch (e) { toastErr(({ "whitelist-add": `无法把 ${player} 加入白名单`, "whitelist-remove": `无法把 ${player} 移出白名单`, op: `无法把 ${player} 设为管理员`, deop: `无法取消 ${player} 的管理员`, kick: `无法踢出 ${player}`, ban: `无法封禁 ${player}`, pardon: `无法解除 ${player} 的封禁` }[action] || `无法执行 ${action} ${player}`), e); }
    };
    const draw = async () => {
      if (stale(gen)) return;
      let pl;
      try { pl = await api(`/api/instances/${encodeURIComponent(name)}/players`); }
      catch (e) { if (!stale(gen)) body.innerHTML = errBox("无法读取玩家列表", e); return; }
      if (stale(gen)) return;
      // 三个名单的输入框、每个人名后面的「移除」「OP」「踢」都同名：可访问名称带上名单和玩家名
      const section = (title, items, addAction, removeAction, hint, extra = "") => `
        <div class="p-sec">
          <div class="p-sec-head"><b>${title}</b>（${items.length}）
            <input type="text" data-add="${addAction}" placeholder="输入玩家名，回车添加" aria-label="添加玩家到${title}">
            ${hint ? `<span class="hint">${hint}</span>` : ""}
          </div>${extra}
          <div class="badges">${items.map(p =>
            `<span class="badge">${esc(p.name)}<button type="button" class="p-x" data-act="${removeAction}" data-p="${esc(p.name)}" title="移除" aria-label="移除 ${esc(p.name)}（${title}）">${icon("x")}</button></span>`).join("") || `<span class="sub">空</span>`}</div>
        </div>`;
      // 白名单的总开关就在名单旁边（可用性走查 R02、U11），不用再去「常用设置」里找。whitelistOn 是 server.properties 里写的
      //（按服务端的读法）；没写这一项时是 null，服务端用它自己的默认值：26.3 起开，更早关
      const wlSet = pl.whitelistOn === true || pl.whitelistOn === false;
      const wlOn = wlSet ? pl.whitelistOn : verGte(info.mc, "26.3");
      const wlSwitch = `<div class="p-wl"><label class="switch"><input type="checkbox" id="pl-wl-on"${wlOn ? " checked" : ""} aria-label="只允许白名单里的玩家进入"><span class="sw"></span>
          <span><span class="sw-label">只允许白名单里的玩家进入</span>
          <div class="sw-desc" id="pl-wl-desc">${wlOn ? "开着：只有名单里的玩家能进（管理员不受限制）。" : "关着：任何知道地址的人都能进。只想让熟人进，就打开它，再在下面加名字。"}${wlSet ? "" : "（server.properties 里还没写这一项，这是服务端的默认值）"}</div></span></label></div>`;
      body.innerHTML = `<div class="players-grid">
        <div class="p-sec">
          <div class="p-sec-head"><b>在线玩家</b>（${pl.online.length}）<button class="btn sm" id="pl-refresh">${icon("refresh")} 刷新</button></div>
          <div class="badges">${pl.online.map(n => `<span class="badge core">${esc(n)}
            <button type="button" class="p-x" data-act="op" data-p="${esc(n)}" title="设为管理员" aria-label="OP ${esc(n)}">OP</button>
            <button type="button" class="p-x" data-act="kick" data-p="${esc(n)}" title="踢出" aria-label="踢 ${esc(n)}">踢</button></span>`).join("") || `<span class="sub">暂无玩家在线</span>`}</div>
        </div>
        ${section("白名单", pl.whitelist, "whitelist-add", "whitelist-remove", "", wlSwitch)}
        ${section("管理员 OP", pl.ops, "op", "deop", "")}
        ${section("封禁名单", pl.banned, "ban", "pardon", "")}
      </div>`;
      // 拨开关：写进 server.properties；服务器开着时再发 whitelist on / off，马上生效（同游戏内 AI 一键开白名单的做法）
      $("#pl-wl-on").onchange = async () => {
        const sw = $("#pl-wl-on"), on = sw.checked;
        sw.disabled = true;
        try {
          await api(`/api/instances/${encodeURIComponent(name)}/properties`, { method: "PUT", body: { pairs: [{ key: "white-list", value: String(on) }] } });
          if (info.status === "running" || info.status === "starting")
            await api(`/api/instances/${encodeURIComponent(name)}/command`, { method: "POST", body: { cmd: on ? "whitelist on" : "whitelist off" } });
          toast(on ? "已打开白名单：只有名单里的玩家能进" : "已关闭白名单：任何知道地址的人都能进");
        } catch (e) { toastErr(on ? "无法打开白名单" : "无法关闭白名单", e); }
        if (!stale(gen)) draw();
      };
      $("#pl-refresh").onclick = draw;
      body.querySelectorAll("input[data-add]").forEach(inp => inp.onkeydown = e => {
        if (e.key === "Enter" && inp.value.trim()) { act(inp.dataset.add, inp.value.trim()); inp.value = ""; }
      });
      body.querySelectorAll(".p-x").forEach(a => a.onclick = () => act(a.dataset.act, a.dataset.p));
    };
    await draw();

  } else if (view === "backups") {
    // 世界与备份 → 备份（可用性走查 R11、R15）：页顶写当前世界从哪来；备份放在哪、留几份写在这里（份数是全局设置，到「设置 → 存储位置」改）；
    // 运行中的「还原」旁边写明会先停服（以前写「要先停服」，回访走查 11-16 读成要自己先停），确认框里直接给「停止并还原」，一次确认做完
    const appInfo = await api("/api/app").catch(() => ({ config: {} }));
    if (stale(gen)) return;
    const keep = (appInfo.config && appInfo.config.backupKeep) || 10, local = appInfo.local !== false;
    const nice = t => String(t || "").replace(/(\d{1,2}:\d\d):\d\d$/, "$1"); // 2026-11-15 20:30:05 → 2026-11-15 20:30（本来就没有秒的不动）
    // 有「换地图」的服务器：页顶还要看换地图的记录（恢复点），才说得清现在的世界是不是导入的
    const hasMap = !!(window.MDWorldImport && MDWorldImport.applies(info));
    const draw = async () => {
      if (stale(gen)) return;
      let list, cur = info, ws = null;
      try {
        [list, cur, ws] = await Promise.all([api(`/api/instances/${enc}/backups`),
          api("/api/instances").then(l => l.find(i => i.name === name) || info).catch(() => info),
          hasMap ? api(`/api/instances/${enc}/world`).catch(() => null) : null]);
      } catch (e) { if (!stale(gen)) body.innerHTML = errBox("无法读取备份列表", e); return; }
      if (stale(gen)) return;
      info.worldFrom = cur.worldFrom; info.backupDir = cur.backupDir || info.backupDir;
      const wf = cur.worldFrom, running = info.status !== "stopped";
      const label = $("#bk-label") ? $("#bk-label").value : ""; // 重画时留住正在填的标签
      // 页顶：现在的世界从哪来（回访走查 11-16）。从备份还原过的记在 worldFrom 里，换地图（导入、换回）时清掉，所以有它就是它；
      // 没有的话看换地图的记录：用地图新建的、换过地图的，以前一律写成「服务器自己的世界」。地图在存档里多半叫 world，
      // 人认得的是导入时的文件夹或压缩包名，所以写来源名。恢复点删掉以后就查不到导入过，所以不说「服务器自己的世界」，只说没有记录
      const og = !wf && ws ? MDWorldImport.origin(ws) : null;
      // 随交接包带来的（carried，A52）：还原是在原来那台电脑上做的，那份备份不在这台的列表里，照实说，不让人去列表里找
      const nowText = wf && wf.carried
        ? `当前世界：原来那台电脑上从 <b>${esc(nice(wf.backupTime) || wf.backup)}</b> 的备份还原来的${wf.label ? `（${esc(wf.label)}）` : ""}，随交接包带过来（那份备份不在这台电脑上）`
        : wf
        ? `当前世界：从 <b>${esc(nice(wf.backupTime) || wf.backup)}</b> 的备份还原${wf.label ? `（${esc(wf.label)}）` : ""}，还原于 ${esc(nice(wf.at))}`
        : og && og.kind === "import" ? `当前世界：从「<b>${esc(og.source)}</b>」导入的地图${og.at ? `，导入于 ${esc(og.at)}` : ""}`
        : og && og.kind === "returned" ? `当前世界：${og.at ? esc(og.at) + " " : ""}换过一次地图，后来换回了换之前的那张`
        : og && og.kind === "busy" ? "当前世界：换地图还没走完，或者要人处理"
        : ws ? "当前世界：没有从备份还原过，「换地图」里也没有导入记录"
        : "当前世界：没有从备份还原过";
      const mapLink = og ? `<a class="quiet-link" href="${base}world/map" id="bk-now-map">到「换地图」里看</a>` : "";
      // 改过备份存放文件夹：以前的备份还在原来的文件夹里，照常列出、能还原能删。「备份放在」写的是新文件夹，
      // 不说一声的话，人会以为列出来的就在那里，点「打开」看到空文件夹还以为备份丢了（回访走查 11-16）
      const olds = [...new Set((list || []).filter(b => b.elsewhere && b.dir).map(b => b.dir))];
      const oldNote = olds.map(d => `<div class="bk-old">${icon("folder")}<span>另有 <b>${(list || []).filter(b => b.elsewhere && b.dir === d).length}</b> 份是改存放位置以前做的，还在原来的文件夹 <span class="mono">${esc(d)}</span>，照常能还原、删除</span>` +
        (local ? `<button type="button" class="btn sm ghost" data-bk-open-old="${esc(d)}" aria-label="打开原来的备份文件夹 ${esc(d)}">${icon("folder")} 打开</button>` : "") + `</div>`).join("");
      body.innerHTML = `
        <div class="bk-now" id="bk-now">${icon("globe")}<span>${nowText}</span>${mapLink}</div>
        <div class="row" style="margin-bottom:10px;flex-wrap:wrap">
          <input type="text" id="bk-label" maxlength="20" placeholder="备份标签（可选，如：更新模组前）" aria-label="备份标签（可选）" style="max-width:240px" value="${esc(label)}">
          <button class="btn primary" id="bk-create">${icon("camera")} 立即备份世界</button>
        </div>
        <div class="bk-where" id="bk-where">
          <span>${olds.length ? "新的备份放在" : "备份放在"} <span class="mono" id="bk-dir">${esc(info.backupDir || "")}</span></span>
          ${local && info.backupDir ? `<button type="button" class="btn sm ghost" id="bk-open">${icon("folder")} 打开</button>` : ""}
          <span class="sep">·</span><span id="bk-keep">保留最近 <b>${keep}</b> 份（所有服务器共用）</span>
          <a class="quiet-link" href="#/settings/storage" id="bk-keep-link">到左下角软件「设置 → 存储位置」里改</a>
        </div>
        ${oldNote ? `<div class="bk-olds" id="bk-olds">${oldNote}</div>` : ""}
        <div class="hint" style="margin-bottom:14px">服务器运行时也能备份：先暂停自动保存，压成压缩包，再恢复自动保存。超过保留份数时删掉最旧的。移出列表或删除服务器都不会删掉备份。
          还原时服务器得停着，开着时点「还原」会先帮你保存世界、停服，还原完不会自动开服。还原前会先把当前世界自动备份一份（标签「还原前自动」），再把世界换成选的那一份；核心、${window.MDModCart && MDModCart.applies(info) ? MDModCart.tabLabel(info.core) + "和" : ""}设置都不动。定时备份在「定时任务」里加。</div>
        <div class="group has-ico bk-list">${(list || []).map(b => {
          const title = b.label || b.file, who = b.label ? `${b.label}（${nice(b.time)}）` : b.file;
          return `
          <div class="row-item"><div class="ri-head"><span class="ri-ico">${icon("package")}</span>
            <div class="ri-main"><div class="ri-title">${esc(title)}${wf && !wf.carried && wf.backup === b.file ? `<span class="tag">现在的世界</span>` : ""}${b.elsewhere ? `<span class="tag neutral" title="${esc(b.dir || "")}">在原来的文件夹</span>` : ""}</div>
              <div class="ri-key">${esc(nice(b.time))} · ${b.sizeMb.toFixed(1)} MB${b.label ? ` · <span class="mono">${esc(b.file)}</span>` : ""}</div></div>
            <div class="ri-right">${running ? `<span class="hint bk-need-stop">会先停服</span>` : ""}<button class="btn sm" data-restore="${esc(b.file)}" data-who="${esc(who)}" aria-label="还原 ${esc(b.file)}">${icon("restore")} 还原</button>
              <button class="btn sm danger" data-del="${esc(b.file)}" aria-label="删除 ${esc(b.file)}">删除</button></div></div></div>`;
        }).join("") ||
          `<div class="row-item"><div class="ri-head"><div class="ri-main"><div class="ri-sub">暂无备份</div></div></div></div>`}</div>`;
      $("#bk-create").onclick = async () => {
        $("#bk-create").disabled = true;
        try {
          const r = await api(`/api/instances/${enc}/backups`, { method: "POST", body: { label: $("#bk-label").value.trim() } });
          toast(`备份完成：${r.file}`);
          if (!stale(gen)) $("#bk-label").value = "";
        } catch (e) { toastErr("备份失败", e); }
        draw();
      };
      $("#bk-label").onkeydown = e => { if (e.key === "Enter") $("#bk-create").click(); };
      const open = $("#bk-open");
      if (open) open.onclick = () => openBackupDir(info);
      body.querySelectorAll("[data-bk-open-old]").forEach(b => b.onclick = () =>
        api("/api/openpath", { method: "POST", body: { path: b.dataset.bkOpenOld } }).catch(e => toastErr("无法打开原来的备份文件夹", e)));
      body.querySelectorAll("[data-restore]").forEach(b => b.onclick = async () => {
        const file = b.dataset.restore, live = info.status !== "stopped", n = live ? (info.onlineCount || 0) : 0;
        const r = await confirmModal({
          title: live ? "停止服务器并还原备份？" : "还原备份？", danger: true, okText: live ? "停止并还原" : "还原", icon: "restore",
          body: `将把世界还原到 <b>${esc(b.dataset.who)}</b>。<br>` +
            (live ? `服务器正在运行${n ? `，有 <b>${n}</b> 位玩家在线，停服以后他们会掉线` : ""}：会先保存世界并停止，停下来以后再还原。<br>` : "") +
            "还原前会先把当前世界自动备份一份，想反悔可以再还原回来。",
        });
        if (!r || stale(gen)) return;
        body.querySelectorAll("[data-restore]").forEach(x => { x.disabled = true; });
        if (live) { toast("正在停止服务器，停下来以后还原"); setTimeout(refreshHead, 300); }
        try {
          await api(`/api/instances/${enc}/backups/restore`, { method: "POST", body: { file, stopFirst: live } });
          toast(`还原完成：现在的世界是 ${b.dataset.who}${live ? "。服务器还停着，要玩的话点「启动」" : ""}`);
        } catch (e) { toastErr("还原失败", e); }
        refreshHead();
        draw();
      });
      body.querySelectorAll("[data-del]").forEach(b => b.onclick = async () => {
        const r = await confirmModal({ title: "删除备份？", danger: true, okText: "删除", icon: "trash", body: "删除后无法恢复：" + esc(b.dataset.del) });
        if (!r) return;
        try { await api(`/api/instances/${enc}/backups?file=${encodeURIComponent(b.dataset.del)}`, { method: "DELETE" }); }
        catch (e) { toastErr("无法删除备份", e); }
        draw();
      });
    };
    // 开服、停服以后「会先停服」跟着出现、消失（只在状态变了时重画）
    onStatus.push((cur, was) => { if ((cur.status === "stopped") !== (was === "stopped")) draw(); });
    await draw();

  } else if (view === "tasks") {
    let pol;
    try { pol = await api(`/api/instances/${encodeURIComponent(name)}/policies`); }
    catch (e) { if (!stale(gen)) body.innerHTML = errBox("无法读取定时任务", e); return; }
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
    // 每一条定时任务在保存条里算一项（可用性走查 R16）：键是「类型、时间、命令」再加上第几条一样的，加两条就是两项修改
    const schedKeys = list => { const n = {}; return list.map(s => { const k = JSON.stringify([s.type, s.at, s.args || ""]); n[k] = (n[k] || 0) + 1; return "s:" + k + "#" + n[k]; }); };
    let savedKeys = new Set(schedKeys(scheds)); // 已经保存的那些：点「添加」加进来的是草稿，标「未保存」，保存以后才算数
    const drawScheds = () => {
      const keys = schedKeys(scheds);
      $("#sched-list").innerHTML = scheds.map((s, i) => `
        <div class="java-row${savedKeys.has(keys[i]) ? "" : " draft"}"><span class="badge core">${esc(TYPE_NAMES[s.type] || s.type)}</span>
          <span class="jv">每天 ${esc(s.at)}</span>
          <span class="jp">${esc(s.args || "")}</span>
          ${savedKeys.has(keys[i]) ? "" : `<span class="tag warn">未保存</span>`}
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
      $("#sc-hint").textContent = `已设为 ${hm}。${day} ${hm} 执行第一次，之后每天该时间执行（若只需执行一次，触发后移除即可）。点「添加」并保存后生效。`;
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
      () => Object.fromEntries([["crash", String($("#pol-crash").checked)], ...schedKeys(scheds).map(k => [k, "1"])]),
      async (sent, base) => {
        // 发出去的是点「保存」这一刻的那份：请求途中又加、又删的不算已保存，照旧标「未保存」（Codex 复审 beta.3）。
        // 只发这一页改了什么：崩溃重启拨过才带；定时任务按条加、删（和保存条数的是同一份键，键里带着「第几条一样的」，
        // 两条一样的删掉一条也算得出来）。不再整张表发过去：两个窗口同时开着这一页时，另一边刚加的任务这一页没有，
        // 整张发就把它删了（Codex 复审 v3.2.0 第二轮。后端按条删找不到的不算错，所以另一边已经删掉的再删一次也没事）
        const now = scheds.map(s => ({ ...s })), keys = schedKeys(now);
        const had = Object.keys(base).filter(k => k.startsWith("s:")), have = new Set(keys);
        const req = {};
        if (sent.crash !== base.crash) req.crashRestart = sent.crash === "true";
        const add = now.filter((_, i) => !(keys[i] in base));
        const remove = had.filter(k => !have.has(k)).map(k => { const [type, at, args] = JSON.parse(k.slice(2, k.lastIndexOf("#"))); return { type, at, args }; });
        if (add.length) req.addSchedules = add;
        if (remove.length) req.removeSchedules = remove;
        if (Object.keys(req).length) await api(`/api/instances/${encodeURIComponent(name)}/policies`, { method: "PUT", body: req });
        savedKeys = new Set(keys);
        if (!stale(gen)) drawScheds();
        toast("定时任务已保存，马上生效");
      });

  } else if (view === "addons") {
    // 插件 / 模组（可用性走查 R10）：原来的「资源」和「待装清单」合成这一页。上面是服务器里已经装了的（停用、启用、删除，要先停服），
    // 下面是热门、搜索、从电脑添加、待装清单（modcart.js），装过的在热门和搜索结果里标「已安装」。
    // 名字按核心叫：插件服叫插件，模组服叫模组，Mohist、Banner 两样都说（docs/文案重写/04-标签与术语-beta3.md）
    const label = MDModCart.tabLabel(info.core), modsApi = `/api/instances/${enc}/mods`;
    body.innerHTML = `<section class="ad-installed" aria-labelledby="ad-h">
        <div class="ad-head"><h2 id="ad-h">已安装的${label}</h2><span class="hint" id="ad-count" aria-live="polite"></span><span class="grow"></span>
          <button type="button" class="btn sm ghost" id="ad-reload" aria-label="刷新已安装的${label}">${icon("refresh")} 刷新</button></div>
        <div class="warn-text ad-need-stop" id="ad-need-stop" hidden>${icon("warn")} 要停用、启用或删除，先停止服务器：运行中这些文件被服务器占着，改了也要重启才算数。</div>
        <div id="ad-list">${skeletonRows(3)}</div>
        <div class="hint ad-note" id="ad-note"></div>
      </section>
      <section class="ad-pending" id="ad-pending" hidden aria-labelledby="ad-pnd-h">
        <div class="ad-head"><h2 id="ad-pnd-h" tabindex="-1">整合包里没装上的</h2></div>
        <div class="hint">导入整合包时没能自动装上的文件。跳过的也列在这里：它们没有装上，要的话还能换成别的处理方式。</div>
        <div id="ad-pending-out"></div><div id="ad-pending-box"></div>
      </section>
      <h2 class="ad-add-h">添加${label}</h2>
      <div id="ad-cart"></div>`;
    // 整合包里没装上的（R09）：还没处理的和跳过的都在这一块，跳过的也能重新处理；一个都没有时不出现。处理以后页头那一条跟着变
    pendingReq.then(v => {
      if (stale(gen) || !v || !((v.pending || []).length + (v.skipped || []).length) || !$("#ad-pending")) return;
      $("#ad-pending").hidden = false;
      pendingPanel($("#ad-pending-box"), $("#ad-pending-out"), name, gen, { onChange: paintPending, applyId: "ad-pending-apply" }).load();
    });
    let mods = [], dir = "", pids = new Set(), ids = new Set(), pidOf = {}, repaintHits = () => {};
    // 热门和搜索里认「已安装」用的两份：启用着的文件在 Modrinth 上认出来的项目、启用着的模组 ID。停用、删除以后跟着变
    const learn = () => {
      const on = mods.filter(m => !m.disabled);
      pids = new Set(on.map(m => pidOf[m.file]).filter(Boolean));
      ids = new Set(on.filter(m => m.id).map(m => m.id.toLowerCase()));
    };
    const stopped = () => info.status === "stopped";
    const titleOf = m => m.name || m.id || m.file.replace(/\.jar(\.disabled)?$/i, "");
    const paintStop = () => {
      const on = stopped();
      $("#ad-need-stop").hidden = on || !mods.length;
      body.querySelectorAll("[data-ad-toggle], [data-ad-del]").forEach(b => { if (!b.dataset.busy) b.disabled = !on; });
    };
    const paint = () => {
      const off = mods.filter(m => m.disabled).length;
      // 标题按实际列的文件夹说：Mohist、Banner 两样都能装，但这里列的只是 mods 文件夹（plugins 里的插件用「打开文件夹」看）
      const what = dir === "plugins" ? "插件" : dir === "mods" ? "模组" : label;
      $("#ad-h").textContent = `已安装的${what}`;
      $("#ad-count").textContent = mods.length ? `${dir} 文件夹里共 ${mods.length} 个${off ? `，${off} 个已停用` : ""}` : "";
      $("#ad-note").textContent = (mods.length ? `停用是把文件改名成 .jar.disabled，服务器不再加载它，随时能启用；删除是把文件移进服务器文件夹里的 .minedeck\\已删除\\${dir}，想要回来就从那里挪回 ${dir} 文件夹。改动在下次启动时生效。` : "") +
        (label === "插件和模组" ? `${mods.length ? " " : ""}plugins 文件夹里的插件没有列在这里，可以用页头的「打开文件夹 → 插件」查看。` : "");
      $("#ad-list").innerHTML = mods.length ? `<div class="group has-ico">${mods.map(m => {
        const t = titleOf(m);
        return `<div class="row-item ad-item${m.disabled ? " off" : ""}" data-file="${esc(m.file)}"><div class="ri-head"><span class="ri-ico">${icon(info.kind === "proxy" || MDModCart.nounOf(info.core) === "插件" ? "plug" : "package")}</span>
          <div class="ri-main"><div class="ri-title"><span>${esc(t)}</span>${m.disabled ? `<span class="tag neutral">已停用</span>` : ""}</div>
            <div class="ri-sub"><span class="mono">${esc(m.file)}</span>${m.version ? ` · ${esc(m.version)}` : ""}</div>${m.err ? `<div class="ri-sub">读不出这个文件里的信息：${esc(m.err)}</div>` : ""}</div>
          <div class="ri-right">
            <button type="button" class="btn sm" data-ad-toggle="${esc(m.file)}" data-enable="${m.disabled ? 1 : 0}" aria-label="${m.disabled ? "启用" : "停用"} ${esc(m.file)}">${m.disabled ? "启用" : "停用"}</button>
            <button type="button" class="btn sm danger" data-ad-del="${esc(m.file)}" aria-label="删除 ${esc(m.file)}">删除</button>
          </div></div></div>`;
      }).join("")}</div>` : `<div class="sub" id="ad-empty">${esc(dir || "插件")} 文件夹里还没有${label}。在下面挑好放进待装清单，一起装上。</div>`;
      body.querySelectorAll("[data-ad-toggle]").forEach(b => b.onclick = () => toggleOne(b));
      body.querySelectorAll("[data-ad-del]").forEach(b => b.onclick = () => deleteOne(b));
      paintStop();
    };
    const load = async () => {
      try {
        const r = await api(modsApi);
        if (stale(gen)) return;
        mods = r.mods || []; dir = r.dir || "";
        learn();
        paint();
        repaintHits();
      } catch (e) { if (!stale(gen)) $("#ad-list").innerHTML = errBox(`无法读取已安装的${label}`, e, { label: `已安装的${label}` }); }
    };
    // 热门和搜索结果里认「已安装」：先按模组 ID 对 Modrinth 的 slug（马上就有），再按文件内容在 Modrinth 上认项目（要联网，只在进页面时查一次）
    const learnProjects = () => api(modsApi + "?projects=1").then(r => {
      if (stale(gen)) return;
      pidOf = Object.fromEntries((r.mods || []).filter(m => m.projectId).map(m => [m.file, m.projectId]));
      learn();
      repaintHits();
    }).catch(() => {});
    const toggleOne = async b => {
      const file = b.dataset.adToggle, enable = b.dataset.enable === "1";
      b.disabled = true; b.dataset.busy = "1";
      try {
        await api(`${modsApi}/toggle`, { method: "POST", body: { file, enable } });
        toast(`已${enable ? "启用" : "停用"} ${file.replace(/\.disabled$/i, "")}，下次启动生效`);
      } catch (e) { toastErr(`无法${enable ? "启用" : "停用"} ${file}`, e); }
      if (!stale(gen)) await load();
    };
    const deleteOne = async b => {
      const file = b.dataset.adDel, m = mods.find(x => x.file === file);
      const ok = await confirmModal({ title: `删除「${m ? titleOf(m) : file}」？`, danger: true, icon: "trash", okText: "删除",
        body: `会把 <span class="mono">${esc(file)}</span> 从 ${esc(dir)} 文件夹移进服务器文件夹里的 <span class="mono">.minedeck\\已删除\\${esc(dir)}</span>，不会直接删掉。想要回来，从那里挪回 ${esc(dir)} 文件夹就行。` });
      if (!ok || stale(gen)) return;
      b.disabled = true; b.dataset.busy = "1";
      try {
        const r = await api(`${modsApi}/delete`, { method: "POST", body: { file } });
        const local = await api("/api/app").then(a => a.local !== false).catch(() => true); // 从局域网打开时打不开主机上的文件夹
        toast(`已删除 ${file}：移进了服务器文件夹里的 ${r.movedTo}`, false, undefined, {
          actions: local && r.dir ? [{ label: "打开文件夹", aria: `打开放删除文件的文件夹 ${name}`,
            onClick: () => api(`/api/instances/${enc}/opendir?sub=${encodeURIComponent(r.dir)}`, { method: "POST" }).catch(e => toastErr("无法打开文件夹", e)) }] : [],
        });
      } catch (e) { toastErr(`无法删除 ${file}`, e); }
      if (!stale(gen)) await load();
    };
    $("#ad-reload").onclick = load;
    onStatus.push(paintStop); // 停服以后停用、删除马上能点；开服以后马上点不了
    await load();
    if (stale(gen)) return;
    repaintHits = await MDModCart.render($("#ad-cart"), info, gen, { installedOf: h => pids.has(h.project_id) || (!!h.slug && ids.has(String(h.slug).toLowerCase())) });
    learnProjects();

  } else if (view === "common") {
    if (info.kind === "proxy") {
      body.innerHTML = `<div class="sub">代理端的监听端口和后端服务器列表在配置文件里：Velocity 是 velocity.toml，Waterfall 是 config.yml，第一次启动后生成。用「打开文件夹」修改。</div>`;
      return;
    }
    let data;
    try { data = await api(`/api/instances/${encodeURIComponent(name)}/properties`); } catch (e) { if (!stale(gen)) body.innerHTML = errBox("无法读取服务器配置", e); return; }
    if (stale(gen)) return;
    const cur = Object.fromEntries(data.pairs.map(p => [p.key, p.value]));
    const diskRules = data.gamerules && typeof data.gamerules === "object" ? data.gamerules : {};
    const mc = info.mc, running = data.running;
    if (!running) liveGamerules.delete(name);
    const gameruleVal = p => {
      if (running) {
        const m = liveGamerules.get(name), ov = m && m[p.key];
        if (ov) {
          if ((diskRules[p.key] ?? "") === ov.disk) return ov.val;
          delete m[p.key]; // 磁盘存档已经变了，改认存档里的新值
        }
      }
      return diskRules[p.key] ?? cur[p.key] ?? (p.defSince && verGte(mc, p.defSince.v) ? p.defSince.def : p.def);
    };
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
    const gameruleOf = p => (p.gameruleSince && verGte(mc, p.gameruleSince.v)) ? p.gameruleSince.rule : p.gamerule;
    // 每个控件都用设置项的中文名当可访问名称（行里的名字和控件没有 label 关联，读屏和自动化原来读不到名字）
    const fieldHTML = p => {
      const v = cur[p.key] ?? (p.defSince && verGte(mc, p.defSince.v) ? p.defSince.def : p.def), al = `aria-label="${esc(p.label)}"`;
      if (p.key === "motd") return `<div class="motd-palette" id="motd-pal"></div>
        <input type="text" data-k="motd" id="motd-in" value="${esc(v ?? "")}" ${al} style="width:100%;margin-top:6px"><div class="motd-preview" id="motd-prev"></div>`;
      if (p.mode === "gamerule") { const gv = gameruleVal(p), gr = gameruleOf(p); return `<label class="switch" title="游戏规则：服务器运行时通过 /gamerule 即时生效"><input type="checkbox" data-gr="${gr}" data-gr-key="${p.key}" ${gv === "true" ? "checked" : ""} ${running ? "" : "disabled"} ${al}><span class="sw"></span></label>`; }
      if (p.mode === "bool") return `<label class="switch"><input type="checkbox" data-k="${p.key}" ${v === "true" ? "checked" : ""} ${al}><span class="sw"></span></label>`;
      if (p.mode === "sel") return `<select data-k="${p.key}" ${al}>${p.opts.map((o, i) => `<option value="${o}" ${v === o ? "selected" : ""}>${esc(p.optNames ? p.optNames[i] : o)}</option>`).join("")}</select>`;
      if (p.mode === "int") return `<input type="number" data-k="${p.key}" value="${esc(v ?? "")}" ${al}>`;
      return `<input type="text" data-k="${p.key}" value="${esc(v ?? "")}" ${al} style="width:200px">`;
    };
    const rowHTML = p => {
      const tags = (p.restart && p.mode !== "gamerule" ? `<span class="ri-restart">重启生效</span>` : "") + (p.mode === "gamerule" ? `<span class="gr-tag">游戏规则</span>` : "");
      // 白名单：说清 MineDeck 新建时怎么写的（R02）。26.3 起服务端自己生成配置时默认开，MineDeck 新建时按向导里的开关明确写进去
      const notice = p.key === "white-list" ? `<div class="pd">MineDeck 新建服务器时按向导里的开关写这一项，默认关${verGte(mc, "26.3") ? "（这个版本的服务端自己生成配置时默认是开）" : ""}。名单和这个开关在「玩家」里也能直接改。</div>` : "";
      const grHint = (p.mode === "gamerule" && !running) ? "（服务器启动后可切换）" : "";
      if (p.key === "motd") return `<div class="prop-item wide" data-search="${esc(p.label)} ${p.key}"><div style="flex:1">
        <div class="pl">${esc(p.label)} ${tags}</div><div class="pd">${esc(p.desc || "")} 支持中文与 § 色码。</div><div class="pk">${p.key}</div>${fieldHTML(p)}</div></div>`;
      return `<div class="prop-item" data-search="${esc(p.label)} ${p.key}"><div>
        <div class="pl">${esc(p.label)} ${tags}</div><div class="pd">${esc(p.desc || "")}${grHint}</div>${notice}<div class="pk">${p.key}</div></div>${fieldHTML(p)}</div>`;
    };
    body.innerHTML = `
      <div class="common-bar">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="cp-search" placeholder="搜索设置项（中文名 / 键名，如 视距 / rcon）" style="width:320px"></div>
        <div class="cp-chips">${GROUPS.map((g, i) => inGroup(g).length ? `<button class="cp-chip" data-g="${i}">${g}</button>` : "").join("")}</div>
      </div>
      ${data.pairs.length === 0 ? `<div class="warn-text" style="margin-bottom:12px;display:flex;align-items:flex-start;gap:7px;line-height:1.7">${icon("warn")}<span>服务器尚未首次启动：部分配置项需等首次启动后由服务端生成，当前已填的值会保留。</span></div>` : ""}
      <div id="cp-list">${GROUPS.map((g, i) => {
        const gi = inGroup(g);
        return gi.length ? `<div class="cp-group" id="cpg-${i}"><h3 class="cp-gh">${g}<span class="cp-gn">${gi.length}</span></h3>
          <div class="props-grid">${gi.map(rowHTML).join("")}</div></div>` : "";
      }).join("")}</div>
      ${saveBar("保存设置", liveBarNote())}`;
    body.querySelectorAll(".cp-chip").forEach(c => c.onclick = () => $("#cpg-" + c.dataset.g, body)?.scrollIntoView({ behavior: "smooth", block: "start" }));
    wireSearch($("#cp-search"), () => {
      const q = $("#cp-search").value;
      body.querySelectorAll(".cp-group").forEach(g => { g.style.display = filterBySearch(q, g.querySelectorAll(".prop-item")) ? "" : "none"; });
    });
    // 游戏规则开关：运行时即时发送 /gamerule（不写入 server.properties）
    onStatus.push(c => {
      if (c.status !== "running") liveGamerules.delete(name);
      body.querySelectorAll("[data-gr]").forEach(sw => { sw.disabled = c.status !== "running"; });
    }); // 游戏规则只能在运行中切：开服、停服以后跟着变
    body.querySelectorAll("[data-gr]").forEach(sw => sw.onchange = async () => {
      const rule = sw.dataset.gr, propKey = sw.dataset.grKey, val = sw.checked;
      try {
        await api(`/api/instances/${encodeURIComponent(name)}/command`, { method: "POST", body: { cmd: `gamerule ${rule} ${val}` } });
        sw.checked = val;
        if (propKey) {
          let m = liveGamerules.get(name);
          if (!m) { m = {}; liveGamerules.set(name, m); }
          m[propKey] = { val: val ? "true" : "false", disk: diskRules[propKey] ?? "" };
        }
        toast(`已应用游戏规则 ${rule} = ${val}`);
      } catch (e) { toastErr(`无法应用游戏规则 ${rule}`, e); sw.checked = !val; }
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
    const lb = wireLiveBar(body, "服务器运行中：保存后要重启才生效；游戏规则开关马上生效");
    const RESTART = Object.fromEntries((window.PROPS_CATALOG || []).map(p => [p.key, p.restart !== false]));
    let savedProps = readProps();
    wireSaveBar(body, gen, readProps, async (sent, base) => { // sent：点「保存」这一刻的 readProps()，下面发的 pairs 也是这一刻读的
      // 只提交这一页改过的项（base 是打开这一页或上一次保存时的样子）：另一个窗口刚存的项，这一页没动就不拿旧值盖回去
      // （Codex 复审 v3.2.0 第一轮。后端本来就只改给到的项、其余照原样）。两边改了同一项的，以后存的为准
      const pairs = [];
      body.querySelectorAll("[data-k]").forEach(el => {
        const k = el.dataset.k;
        if ((base[k] ?? "") === (sent[k] ?? "")) return;
        let val;
        if (el.type === "checkbox") val = el.checked ? "true" : "false";
        else val = el.value;
        if (el.type === "number" || el.type === "text" || el.tagName === "SELECT") {
          if (val === "" && !(k in cur)) return; // 未设置且为空的跳过
        }
        pairs.push({ key: k, value: String(val) });
      });
      // 改过的只有「没设置过的项清空」：文件里本来就没有这一项，不用写
      if (pairs.length) await api(`/api/instances/${encodeURIComponent(name)}/properties`, { method: "PUT", body: { pairs } });
      // 存进去的项从此算「文件里有」：原来没有的项（比如资源包链接）在同一页里填上、保存，再清空、保存时，
      // 清空的那一下也要发出去，不然上面那条「未设置且为空的跳过」会把它吞掉，提示已保存、文件里却还留着（Codex 复审 v3.2.0 第二轮）
      for (const p of pairs) cur[p.key] = p.value;
      // 改的项里有要重启才生效的（MineDeck 保存只写文件，服务端运行中不会重读：目录里的项都标着重启生效）。
      // 按发出去的那份算，不按现在的表单：请求途中又改的还没存（Codex 复审 beta.3）
      const changed = Object.keys(sent).filter(k => sent[k] !== savedProps[k]);
      savedProps = sent;
      const needRestart = changed.some(k => RESTART[k] !== false);
      toast("已保存 server.properties" + (info.status !== "stopped" && needRestart ? "（重启服务器以后生效）" : ""));
      lb.saved(needRestart);
    });
    // 从「设置 → 启动与内存」页顶的端口点过来：滚到这一项、点亮、光标放进去
    if (focusPropKey) {
      const el = body.querySelector(`[data-k="${CSS.escape(focusPropKey)}"]`), item = el && el.closest(".prop-item");
      focusPropKey = "";
      if (item) { item.scrollIntoView({ block: "center" }); item.classList.add("flash"); el.focus(); setTimeout(() => item.classList.remove("flash"), 1600); }
    }

  } else if (view === "raw") {
    let data;
    try { data = await api(`/api/instances/${encodeURIComponent(name)}/properties`); } catch (e) { if (!stale(gen)) body.innerHTML = errBox("无法读取服务器配置", e); return; }
    if (stale(gen)) return;
    if (!data.pairs.length) { body.innerHTML = `<div class="sub">配置文件暂未生成；首次启动服务器后，服务端会自动生成全部配置项。</div>`; return; }
    const LABELS = Object.fromEntries((window.PROPS_CATALOG || []).map(p => [p.key, p.label]));
    const total = data.pairs.length;
    body.innerHTML = `<div style="max-width:900px"><div class="sub">server.properties 的全部键值；已知项附中文名。</div>
      <div class="common-bar">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="raw-search" placeholder="搜索配置项（键名 / 中文名，如 rcon / 视距）" aria-label="搜索配置项" style="width:320px"></div>
        <span class="hint" id="raw-count" style="margin:0">共 ${total} 项</span>
      </div>
      <table class="raw">${data.pairs.map(p =>
        `<tr data-search="${esc(p.key)} ${esc(LABELS[p.key] || "")}"><td>${esc(p.key)}</td><td class="raw-cn">${LABELS[p.key] ? esc(LABELS[p.key]) : ""}</td><td><input type="text" data-k="${esc(p.key)}" value="${esc(p.value)}" aria-label="${esc(p.key)}"></td></tr>`).join("")}</table>
      <div class="sub" id="raw-none" hidden>没有匹配的配置项：换个关键词，或者按 Esc 清空搜索。</div>
      ${saveBar("保存全部", liveBarNote())}</div>`;
    // 搜索（同常用设置）：只是把不匹配的行藏起来，保存时照样提交所有改过的项（藏起来的也算）
    wireSearch($("#raw-search"), () => {
      const q = $("#raw-search").value, n = filterBySearch(q, body.querySelectorAll("table.raw tr"));
      $("#raw-count").textContent = searchKey(q) ? `找到 ${n} 项（共 ${total} 项）` : `共 ${total} 项`;
      $("#raw-none").hidden = n > 0;
    });
    const lb = wireLiveBar(body, "服务器运行中：保存后要重启才生效");
    wireSaveBar(body, gen,
      () => Object.fromEntries([...body.querySelectorAll("input[data-k]")].map(el => [el.dataset.k, el.value])),
      async (sent, base) => {
        // 同常用设置：只提交改过的项，另一个窗口刚存的不拿这一页的旧值盖回去（Codex 复审 v3.2.0 第一轮）
        const pairs = [...body.querySelectorAll("input[data-k]")].filter(el => (base[el.dataset.k] ?? "") !== (sent[el.dataset.k] ?? ""))
          .map(el => ({ key: el.dataset.k, value: el.value }));
        if (pairs.length) await api(`/api/instances/${encodeURIComponent(name)}/properties`, { method: "PUT", body: { pairs } });
        toast("已保存 server.properties" + (info.status !== "stopped" ? "（重启服务器以后生效）" : ""));
        lb.saved(true); // server.properties 运行中不会重读，改了哪一项都要重启
      });

  } else if (view === "worldmap" && window.MDWorldImport) { // 世界与备份 → 换地图：导入并替换当前地图、恢复点，整块在 worldimport.js
    await MDWorldImport.render(body, info, gen, refreshHead);

  } else if (view === "music" && window.MDMusic) { // 扩展 → 音乐共享（第五阶段）：整块在 music.js
    await MDMusic.render(body, info, gen, refreshHead);

  } else if (view === "gameai" && window.MDGameChat) { // 扩展 → 游戏内 AI（第四阶段）：整块在 gamechat.js
    await MDGameChat.render(body, info, gen);

  } else if (view === "companion" && window.MDCompanion) { // 扩展 → AI 伙伴（第七阶段）：整块在 companion.js
    await MDCompanion.render(body, info, gen);

  } else if (view === "jvm") {
    const appInfo = await api("/api/app").catch(() => ({ ramMb: 8192, availRamMb: 4096 }));
    if (stale(gen)) return;
    const maxMem = Math.max(2048, appInfo.ramMb - 2048), local = appInfo.local !== false;
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
    // 页顶：这台服务器的基本信息集中在一处（可用性走查 R15）。端口点了去「设置 → 常用」里的端口那一项（代理端的端口在它自己的配置文件里）
    const port = connectPortOf(info), proxy = info.kind === "proxy";
    body.innerHTML = `
      <div style="max-width:640px">
        <div class="group jm-facts" id="jm-facts">
          <div class="jm-fact"><span class="jm-k">核心</span><span class="jm-v">${esc(coreName(info.core))}${info.build ? ` <span class="hint">${esc(info.build)}</span>` : ""}</span></div>
          <div class="jm-fact"><span class="jm-k">版本</span><span class="jm-v">${proxy ? "" : "Minecraft "}${esc(info.mc)}</span></div>
          <div class="jm-fact"><span class="jm-k">端口</span><span class="jm-v">${proxy
            ? (port ? `${port} <span class="hint">（在代理端自己的配置文件里改）</span>` : `<span class="hint">第一次启动、生成配置文件以后才知道</span>`)
            : `<button type="button" class="link-btn mono" id="jm-port" aria-label="端口 ${port}，去「设置 → 常用」里改">${port}</button> <span class="hint">点了去「设置 → 常用」里改</span>`}</span></div>
          <div class="jm-fact"><span class="jm-k">Java</span><span class="jm-v" id="jm-java-now">Java ${info.javaMajor}${info.javaMissing ? ` <span class="tag warn">找不到</span>` : ""}</span></div>
          <div class="jm-fact jm-dir"><span class="jm-k">服务器文件夹</span><span class="jm-v"><span class="mono" id="jm-dir">${esc(info.dir)}</span>
            <button type="button" class="btn sm ghost" id="jm-dir-copy" aria-label="复制服务器文件夹的路径">${icon("copy")} 复制</button>
            ${local ? `<button type="button" class="btn sm ghost" id="jm-dir-open" aria-label="打开服务器文件夹">${icon("folder")} 打开</button>` : ""}</span></div>
        </div>
        <div class="field" id="jm-mem-mount"></div>
        <label class="field"><span>自定义 JVM 参数</span><div class="hint">每行一个，或用空格分隔；-Xmx、-Xms 由上面的内存自动生成，不在这里填。</div>
          <textarea id="jm-jvm" rows="5" spellcheck="false" placeholder="如：-XX:+UseG1GC">${esc((info.extraJvm || []).join("\n"))}</textarea></label>
        <div class="row" style="margin-bottom:12px;flex-wrap:wrap">
          <button class="btn sm" id="jm-aikar">${icon("bolt")} 套用 Aikar's Flags（G1GC 优化）</button>
          <button class="btn sm" id="jm-clear">清空</button>
        </div>
        <div class="hint" style="margin:-4px 0 12px">套用后将替换下方输入框中的全部现有参数。</div>
        <div class="field" id="jm-preview-box">
          <span>实际启动参数预览</span>
          <div class="hint">保存后实际写入启动脚本并用来开服的参数（老版本需保留的 log4shell 防护参数由后端统一补上）：</div>
          <div class="mono" id="jm-preview" style="padding:10px 12px;border-radius:8px;background:var(--surface-2);word-break:break-all;line-height:1.6">加载中…</div>
          <div class="warn-text" id="jm-preview-note" hidden style="margin-top:6px;line-height:1.6"></div>
        </div>
        <label class="field" id="jm-java-box"><span>Java</span>
          <select id="jm-java" aria-label="这台服务器用的 Java" aria-describedby="jm-java-hint"><option value="">加载中…</option></select>
          <div class="hint" id="jm-java-hint">新建时已按 Minecraft 版本选好（1.20.1 用 Java 17，1.21 用 21，26.1 起用 25）。整合包要求其他版本时在这里换，和上面的设置一起保存，下次启动生效。本机没有的版本，保存时会先下载一份。</div>
        </label>
        <div class="hint" style="margin-bottom:14px">自己启动时双击服务器文件夹里的 run.bat（与这里的设置是同一份）。</div>
        ${saveBar("保存", liveBarNote())}
      </div>`;
    $("#jm-dir-copy").onclick = e => copyFlash(e.currentTarget, info.dir, "已复制服务器文件夹的路径");
    if ($("#jm-dir-open")) $("#jm-dir-open").onclick = () => api(`/api/instances/${enc}/opendir`, { method: "POST" }).catch(e => toastErr("无法打开文件夹", e));
    if ($("#jm-port")) $("#jm-port").onclick = () => { focusPropKey = "server-port"; location.hash = base + "settings/common"; };
    // Java 的选择也进保存条（R16，原来旁边单独一个「更换 Java」）：列出 MineDeck 下载的、本机装的，再加上本机还没有、可以现下一份的版本。
    // 现在用的那一项在 /api/javas 里找不到（那个 Java 后来被卸载了）就单列出来，标「找不到」（R21）
    let jopts = [], jcur = -1;
    const sel = $("#jm-java");
    const javaPick = () => (jcur < 0 || sel.value === "" || +sel.value === jcur ? "" : sel.value); // 没改是空串：加载前后都不算一项修改
    const javaLabel = (o, k) => o.text + (k === jcur ? " · 现在用的" : "") + (k === jcur && o.missing ? " · 找不到" : "");
    let paintBar = () => {};
    (async () => {
      const javas = await api("/api/javas").catch(() => ({ portable: [], scanned: [] }));
      if (stale(gen) || !sel.isConnected) return;
      const norm = p => (p || "").toLowerCase().replace(/\//g, "\\");
      jopts = [
        ...javas.portable.map(j => ({ major: j.major, path: j.path || "", text: `Java ${j.major}（MineDeck 下载的）`, portable: true })),
        ...javas.scanned.map(j => ({ major: j.major, path: j.path, text: `Java ${j.major}（本机装的 ${j.version}：${j.path}）` })),
      ];
      jcur = info.javaMissing ? -1 : jopts.findIndex(o => info.javaPath && norm(o.path) === norm(info.javaPath));
      if (jcur < 0 && !info.javaMissing) jcur = jopts.findIndex(o => !o.path && o.portable && o.major === info.javaMajor && info.javaSource === "portable");
      if (jcur < 0) { jopts.unshift({ major: info.javaMajor, path: info.javaPath || "", text: `Java ${info.javaMajor}${info.javaPath ? "（" + info.javaPath + "）" : ""}`, missing: !!info.javaMissing }); jcur = 0; }
      const have = new Set(jopts.filter(o => !o.missing).map(o => o.major));
      JAVA_DOWNLOADABLE.filter(m => !have.has(m)).forEach(m => jopts.push({ major: m, path: "", text: `Java ${m}（本机没有，下载一份，约 50 MB）`, download: true }));
      sel.innerHTML = jopts.map((o, k) => `<option value="${k}"${k === jcur ? " selected" : ""}>${esc(javaLabel(o, k))}</option>`).join("");
      paintBar();
    })();
    renderMemoryControl($("#jm-mem-mount"), { sliderId: "jm-mem", min: 1024, max: maxMem, value: Math.min(info.xmxMb, maxMem), totalMb: appInfo.ramMb, availMb: appInfo.availRamMb });
    // 老版本创建时加入的 log4shell 缓解参数（后端保存时也会强制补回）：套用预设 / 清空时原样保留
    const LOG4J_FIX = ["-Dlog4j2.formatMsgNoLookups=true", "-Dlog4j.configurationFile=log4j2_112-116.xml", "-Dlog4j.configurationFile=log4j2_17-111.xml"];
    const jvmFlags = () => $("#jm-jvm").value.split(/\s+/).map(s => s.trim()).filter(Boolean);
    const keptFix = () => { const cur = jvmFlags(); return LOG4J_FIX.filter(f => cur.includes(f)); };
    let saved = { xmx: $("#jm-mem").value, jvm: jvmFlags().join(" ") }; // 内存和参数上一次存下来的样子：只换 Java 时不重写启动设置
    // 预览与保存共用同一套请求体构造：只发相对 saved 改过的字段（xmxMb 给 0、extraJvm 不传表示不改），保证预览和点保存算出来的完全一致
    const buildSettingsReq = () => {
      const xmx = $("#jm-mem").value, flags = jvmFlags();
      const xmxChanged = xmx !== saved.xmx, jvmChanged = flags.join(" ") !== saved.jvm;
      const reqBody = { xmxMb: xmxChanged ? +xmx : 0, xmsMb: 0 };
      if (jvmChanged) reqBody.extraJvm = flags;
      return { xmx, flags, xmxChanged, jvmChanged, reqBody };
    };
    // 启动参数预览（A28，A25 保存参数校验）：由后端用和保存同一套函数算好返回，前端只负责展示，不自己拼参数
    let previewSeq = 0, previewTimer = 0;
    const refreshJvmPreview = immediate => {
      clearTimeout(previewTimer);
      const run = async () => {
        if (stale(gen) || !$("#jm-preview")) return;
        const seq = ++previewSeq;
        try {
          const r = await api(`/api/instances/${enc}/settings/preview`, {
            method: "POST",
            body: buildSettingsReq().reqBody,
          });
          if (stale(gen) || seq !== previewSeq || !$("#jm-preview")) return;
          $("#jm-preview").textContent = "java " + (r.launchArgs || []).join(" ");
          const noteEl = $("#jm-preview-note");
          if (r.note) { noteEl.hidden = false; noteEl.innerHTML = icon("warn") + " <span>" + esc(r.note) + "</span>"; }
          else { noteEl.hidden = true; noteEl.innerHTML = ""; }
        } catch (e) {
          if (stale(gen) || seq !== previewSeq || !$("#jm-preview")) return;
          $("#jm-preview").textContent = e && e.message ? e.message : "暂时无法预览参数";
          $("#jm-preview-note").hidden = true;
          $("#jm-preview-note").innerHTML = "";
        }
      };
      if (immediate) run(); else previewTimer = setTimeout(run, 120);
    };
    $("#jm-jvm").addEventListener("input", () => refreshJvmPreview(false));
    $("#jm-mem-mount").addEventListener("input", () => refreshJvmPreview(false));
    $("#jm-mem-mount").addEventListener("change", () => refreshJvmPreview(true));
    refreshJvmPreview(true);
    const lb = wireLiveBar(body, "服务器运行中：保存后要重启才生效");
    paintBar = wireSaveBar(body, gen,
      () => ({ xmx: $("#jm-mem").value, jvm: jvmFlags().join(" "), java: javaPick() }), // 只比较参数本身，换行 / 空格差异不算修改
      async () => {
        // 内存、参数、Java 都按点「保存」这一刻的读：请求途中又改的不算已保存（Codex 复审 beta.3）。
        // 内存和参数各管各的，只发改过的那一样（后端 xmxMb 给 0、extraJvm 不给就是不改）：两个窗口同时开着这一页时，
        // 一边改了参数，另一边只改内存再保存，不会把参数改回打开时的样子（Codex 复审 v3.2.0 第二轮）
        const { xmx, flags, xmxChanged, jvmChanged, reqBody } = buildSettingsReq();
        const pick = javaPick();
        // 选的 Java 要先下载时，这次保存完会跳去任务页：保存途中把这一页的输入锁住，不让人在途中改了又随跳页丢掉
        // （Codex 复审 beta.3 第二、三轮。第二轮试过留在本页再记一个「正在下载」的状态，带出了下载完、下载失败以后页面不同步的问题，改成锁）
        const locked = pick !== "" && jopts[+pick].download
          ? [...body.querySelectorAll("input, textarea, select, button")].filter(el => !el.closest(".save-bar") && !el.disabled) : [];
        locked.forEach(el => { el.disabled = true; });
        try {
          const notes = [];
          if (xmxChanged || jvmChanged) {
            const saveRes = await api(`/api/instances/${enc}/settings`, { method: "PUT", body: reqBody });
            if (xmxChanged) saved.xmx = xmx;
            let back = null;
            if (jvmChanged) {
              // 回显实际保存的参数（老版本被删掉的 log4shell 缓解参数会被后端补回）；回读失败不影响已成功的保存。
              // 只在输入框里还是发出去的那份时回显：请求途中又改了参数的，不拿回读的盖掉新填的。
              // 只改内存时不取回传的参数：那时后端返回的 extraJvm 可能是另一个窗口刚存的，会被当成「补回」，也会把这一页没动的参数框换掉
              back = saveRes && Array.isArray(saveRes.extraJvm)
                ? saveRes.extraJvm
                : await api("/api/instances").then(l => l.find(i => i.name === name)?.extraJvm).catch(() => null);
              if (back && !stale(gen) && jvmFlags().join(" ") === flags.join(" ")) $("#jm-jvm").value = back.join("\n");
              saved.jvm = (back || flags).join(" ");
            }
            let msg = "已保存内存与 JVM 参数" + (back?.some(a => !flags.includes(a)) ? "（已自动补回 log4shell 漏洞缓解参数）" : "");
            if (jvmChanged && saveRes && saveRes.note) msg += "。" + saveRes.note;
            notes.push(msg);
          }
          if (pick !== "") {
            const o = jopts[+pick];
            const r = await api(`/api/instances/${enc}/java`, { method: "PUT", body: { major: o.major, path: o.download ? "" : o.path } });
            if (r && r.taskId) { // 要先下载：去任务页看进度，下好了自动换上
              // 等回话的时候人已经去了别的页：不清那一页的未保存守卫、不把人拽走，只说一声（Codex 复审 beta.3 第四轮）
              if (stale(gen)) { toast(`已开始下载 Java ${o.major}，下好了自动换上。进度在任务页`); return; }
              dirtyCheck = null; location.hash = `#/task/${r.taskId}`; return;
            }
            info.javaMajor = o.major; info.javaPath = (r && r.java) || o.path; info.javaSource = o.portable ? "portable" : "local"; info.javaMissing = false;
            jcur = +pick;
            sel.querySelectorAll("option").forEach((el, k) => { el.textContent = javaLabel(jopts[k], k); });
            if ($("#d-java")) $("#d-java").textContent = "Java " + o.major; // 页头的徽标跟着换
            $("#jm-java-now").textContent = "Java " + o.major;
            notes.push(`已换成 Java ${o.major}`);
            refreshHead(); // 「找不到 Java」的提示马上消失
          }
          toast(notes.join("；") + (info.status === "stopped" ? "，下次启动生效" : "，重启服务器后生效"));
          lb.saved(true);
          refreshJvmPreview(true);
          // 新的基线是存下的那份：内存和参数按提交的（参数带上后端补回的）；Java 是相对「现在用的」算的，换上以后就是没改（空串）
          return { xmx: saved.xmx, jvm: saved.jvm, java: "" };
        } finally {
          if (!stale(gen)) locked.forEach(el => { el.disabled = false; }); // 跳去任务页以后页面已经换了，不用管
        }
      });
    $("#jm-aikar").onclick = () => {
      const fix = keptFix();
      $("#jm-jvm").value = [...aikarsFlags(+$("#jm-mem").value), ...fix].join("\n");
      paintBar();
      refreshJvmPreview(true);
      toast(`已填入 Aikar's Flags${fix.length ? "（保留 log4shell 漏洞缓解参数）" : ""}，保存后生效`);
    };
    $("#jm-clear").onclick = () => {
      const fix = keptFix();
      $("#jm-jvm").value = fix.join("\n");
      paintBar();
      refreshJvmPreview(true);
      if (fix.length) toast("已清空（保留 log4shell 漏洞缓解参数）");
    };
  }
}

/* ---------- 视图：联机穿透 ---------- */
/* 第一次跑樱花隧道前要自己放 frpc（樱花没有稳定的下载直链，见 tunnel/providers.go 的 natfrpProvider.EnsureFrpc）：
   写成能照着做的几步（可用性走查 R18）。base 是程序数据文件夹（/api/app 的 base），拿不到时写成相对的说法 */
function natfrpFrpcSteps(base) {
  const dir = base ? `${base}\\runtimes\\frpc\\natfrp` : "程序数据文件夹里的 runtimes\\frpc\\natfrp";
  return `<details class="tun-frpc" data-frpc-steps><summary>第一次用樱花：先把 frpc 放好（只要一次）</summary><ol>
    <li>登录 <a href="https://www.natfrp.com" target="_blank" rel="noopener">樱花（SakuraFrp）</a>的管理面板，打开「软件下载」。</li>
    <li>下载 frpc 的 Windows amd64 版，得到一个 .exe 文件（下到的是压缩包就先解压）。</li>
    <li>把这个 exe 放进 <code>${esc(dir)}</code>，文件名用 frpc.exe 或 frpc_windows_amd64.exe 都行。</li>
    <li>回到这里点「启动」。放过以后又不见了的，多半是被杀毒软件删了，到 Windows 安全中心的「保护历史记录」里恢复。</li>
  </ol><div class="hint">OpenFrp 和自己的 frps 用的 frpc，MineDeck 会自己下载，不用管。</div></details>`;
}

async function renderTunnels() {
  const gen = viewGen;
  let insts = [], appInfo = null;
  [insts, appInfo] = await Promise.all([api("/api/instances").catch(() => []), api("/api/app").catch(() => null)]);
  if (stale(gen)) return;
  if (!Array.isArray(insts)) insts = [];
  const base = (appInfo && appInfo.base) || "";
  const ACC_NAMES = { openfrp: "OpenFrp", natfrp: "樱花frp" };
  const openExt = (u) => { if (u) try { window.open(u, "_blank", "noopener"); } catch {} };
  // OpenFrp 授权轮询间隔（≥5s）；测试可设 window.__MD_TUNNEL_POLL_MS 缩短
  const pollMs = () => {
    const n = +window.__MD_TUNNEL_POLL_MS;
    return Number.isFinite(n) && n > 0 ? Math.max(200, n) : 5000;
  };
  Main().innerHTML = pageHead("内网穿透",
    "把这台电脑上的服务器映射到公网，不在同一网络的朋友也能连。服务商可选 OpenFrp、樱花穿透（SakuraFrp），或自己的 frps。",
    `<button class="btn primary" id="tn-toggle" aria-expanded="false" aria-controls="tn-form">${icon("plus")} 添加隧道</button>`, "", "", "tun-summary") + `
    <div class="hint tun-lan-tip" id="tn-lan-tip" style="margin:-6px 0 14px">${icon("info")} 同一局域网的朋友不用隧道：把服务器页上的「连接地址」发给他们就能进。隧道是给不在同一网络的朋友用的。</div>
    <div class="hint tun-how" id="tn-how">加隧道有两种办法：在「添加隧道」里把服务商网站上建好的隧道 ID 和密钥填进来（自己的 frps 也走这里）；
      或者在页面下方「平台账户」里连上 OpenFrp、樱花的账户，直接在平台上新建隧道，或者把平台上已有的导入进来，不用自己抄密钥。</div>
    <div id="tn-form" class="card tun-form" hidden>
    <h3>添加隧道</h3>
    <div class="hint tun-help" id="tn-help" aria-live="polite"></div>
    <div class="form-grid tun-grid">
      <label class="field"><span>服务商</span><select id="tn-provider">
        <option value="openfrp">OpenFrp（免费）</option>
        <option value="natfrp">樱花frp SakuraFrp</option>
        <option value="custom">自定义 frps</option></select></label>
      <label class="field"><span>隧道名称</span><input type="text" id="tn-name" value="我的隧道" maxlength="30"></label>
      <label class="field" data-f="cred"><span id="tn-cred-label">用户密钥</span><input type="password" id="tn-cred" placeholder="仅保存在本机">
        <small class="hint" id="tn-cred-short" style="display:block;color:var(--warning-text)" aria-live="polite" hidden>凭据不到 6 个字节（字母和数字一个算一个，汉字一个算三个）：日志里含这串凭据的行会整行隐藏，排查问题会不方便，建议换一串更长的。</small></label>
      <label class="field" data-f="proxy"><span>隧道 ID</span><input type="text" id="tn-proxy" placeholder="如 123456"></label>
      <label class="field" data-f="saddr" hidden><span>frps 服务器地址</span><input type="text" id="tn-saddr" placeholder="1.2.3.4 或 frp.example.com"></label>
      <label class="field" data-f="sport" hidden><span>frps 端口</span><input type="number" id="tn-sport" value="7000"></label>
      <label class="field" data-f="rport" hidden><span>公网远程端口</span><input type="number" id="tn-rport" value="25566"></label>
      <label class="field" data-f="lport" hidden><span>本地端口（这台服务器的端口）</span><input type="number" id="tn-lport" value="25565"></label>
      <label class="field"><span>绑定服务器</span><select id="tn-bound"><option value="">不绑定</option>
        ${insts.map(i => `<option value="${esc(i.name)}">${esc(i.name)}（端口 ${i.port}）</option>`).join("")}</select></label>
      <div class="field"><label class="switch"><input type="checkbox" id="tn-auto"><span class="sw"></span>
        <span><span class="sw-label">跟随服务器启动</span><div class="sw-desc">绑定的服务器启动时自动拉起隧道，停服后跟着停</div></span></label></div>
    </div>
    <button class="btn primary" id="tn-add" aria-label="确认添加隧道">${icon("plus")} 添加隧道</button>
    </div>
    <section id="tn-mine" class="tun-mine" aria-labelledby="tn-mine-title">
      <h3 id="tn-mine-title">我的隧道</h3>
      <div id="tun-list">${skeletonRows(4)}</div>
    </section>
    <section id="tn-accounts" class="tun-accounts" aria-label="平台账户">
      <div class="tun-accounts-head">
        <h3>平台账户</h3>
      <p class="hint">OpenFrp 部分基于 <a href="https://github.com/ZGIT-Network/OPENFRP-APIDOC" target="_blank" rel="noopener">OpenFrp OPENAPI</a>。此项目由社区开发，OpenFrp 官方不负责除节点问题以外的技术支持，MineDeck 本身的问题请到 <a href="https://github.com/Trasperioooooon/MineDeck/issues" target="_blank" rel="noopener">GitHub Issues</a> 反馈。签到和购买都只打开官网，MineDeck 不代签、不代下单。</p>
      </div>
      <div id="tn-acc-ro" class="hint" style="margin:0 0 10px" hidden>在局域网里打开时只能看连接状态，连接、签到、创建和导入隧道请在运行 MineDeck 的这台电脑上操作。</div>
      <div id="tn-acc-cards" class="tun-accounts-grid">${skeletonRows(2)}</div>
      <div id="tn-acc-panel" hidden></div>
    </section>`;

  const PROV_NAMES = { openfrp: "OpenFrp", natfrp: "樱花frp", custom: "自定义" };
  const provSel = $("#tn-provider");
  const applyFields = () => {
    const p = provSel.value;
    const show = { cred: true, proxy: p !== "custom", saddr: p === "custom", sport: p === "custom", rport: p === "custom", lport: p === "custom" };
    document.querySelectorAll("[data-f]").forEach(el => el.hidden = !show[el.dataset.f]);
    $("#tn-cred-label").textContent = { openfrp: "用户密钥（个人中心复制）", natfrp: "访问密钥", custom: "auth token（frps 未设可留空）" }[p];
    $("#tn-help").innerHTML = {
      openfrp: `先到 <a href="https://console.openfrp.net" target="_blank">OpenFrp 控制台</a> 新建一条 TCP 隧道，本地端口填这台服务器的端口。然后把隧道 ID 填到下面，用户密钥在控制台的「个人中心」里复制。`,
      natfrp: `先到 <a href="https://www.natfrp.com" target="_blank">SakuraFrp</a> 新建一条隧道，把访问密钥和隧道 ID 复制到下面。${natfrpFrpcSteps(base)}`,
      custom: `填你自己的 frps 的地址、端口和 token。公网远程端口是朋友连进来用的端口，本地端口是这台服务器的端口。`,
    }[p];
  };
  provSel.onchange = applyFields;
  applyFields();
  /* 凭据短于 6 个字符（按 UTF-8 字节算，同后端 tunnel.minMaskLen；前后空白不算）：frpc 的日志里凡是含它的行会整行隐藏，
     不然遮掉的位置反过来会提示凭据（复审发现 10）。自定义 frps 的令牌本来可以很短，所以只提示、不挡保存；空着不提示 */
  const credShort = () => {
    const n = new TextEncoder().encode($("#tn-cred").value.trim()).length;
    $("#tn-cred-short").hidden = !(n > 0 && n < 6);
  };
  $("#tn-cred").addEventListener("input", credShort);
  if (window.MDMusic) MDMusic.tunnelForm(insts, provSel); // 第五阶段：用途（游戏 / 语音）、自定义 frps 的协议
  /* 本地端口随绑定的服务器填（可用性走查 U21、R12）：选了服务器就填它的连接端口，以前一直是 25565，第二台服务器准填错。
     只管自定义 frps 的游戏隧道（樱花、OpenFrp 的本地端口在服务商后台填；语音隧道的端口由 music.js 填 SVC 的）；
     自己动手改过端口就不再替人改。用途从语音换回游戏时 music.js 先把端口还原，这里接着按绑定的服务器填 */
  let lportTyped = false;
  $("#tn-lport").addEventListener("input", () => { lportTyped = true; });
  const fillLport = () => {
    const voice = $("#tn-purpose") && $("#tn-purpose").value === "voice";
    const i = insts.find(x => x.name === $("#tn-bound").value);
    const port = i ? connectPortOf(i) : 0;
    if (lportTyped || voice || provSel.value !== "custom" || !port) return;
    $("#tn-lport").value = port;
  };
  $("#tn-bound").addEventListener("change", fillLport);
  provSel.addEventListener("change", fillLport);
  if ($("#tn-purpose")) $("#tn-purpose").addEventListener("change", fillLport);
  /* 表单在列表上方。没有隧道时一直开着，页头的「添加隧道」收起来（一屏只留一个主按钮）；
     有隧道时由页头的按钮展开 / 收起，展开后那个按钮改成「收起」。轮询重画列表不动表单里填到一半的内容。 */
  let formOpen = false, isEmpty = false;
  /* blocked：tunnels.json 在、却没读出来（后端 tunnel.list 回 503，见 tunnel.LoadError）。读出来之前什么都存不进去，
     表单和页头的「添加隧道」都收起来，不让人填了半天才被拒 */
  let blocked = false;
  const paintForm = () => {
    const f = $("#tn-form"), tg = $("#tn-toggle"), open = !blocked && (isEmpty || formOpen);
    f.hidden = !open;
    tg.hidden = isEmpty || blocked;
    tg.setAttribute("aria-expanded", String(open));
    tg.classList.toggle("primary", !open);
    tg.innerHTML = open ? `${icon("chevronUp")} 收起` : `${icon("plus")} 添加隧道`;
  };
  $("#tn-toggle").onclick = () => {
    formOpen = !formOpen;
    paintForm();
    if (formOpen) { $("#tn-form").scrollIntoView({ behavior: "smooth", block: "nearest" }); $("#tn-name").focus({ preventScroll: true }); }
  };

  /* —— 平台账户：状态卡 / 概览 / 连接 / 签到 / 购买 / 创建导入 —— */
  let accStatus = { openfrp: false, natfrp: false, local: true };
  // local 不为 true 是局域网设备在看：账户区只读（只显示连接状态，不出按钮，也不拉概览；后端对这些接口本来就回 403）。
  // 响应里没有 local 这一项时也按只读算（同 AI 设置页的 !v.local）
  const accRO = () => !accStatus.local;
  // 「已连接」：凭据解不开（openfrpUnreadable / natfrpUnreadable）的那家按未连接算
  const accOn = provider => !!accStatus[provider] && !accStatus[provider + "Unreadable"];
  let accPanel = null; // { provider, overview }
  // 概览请求的序号：每发一次加一（收起、断开时也加一），回来时不是最新的一次、页面已经换了、或者那家已经不算连着，
  // 成功的结果丢掉不画，失败的也不报错（返回 null）；只有当前这一次的失败才往外抛
  let ovSeq = 0;
  const fetchOverview = async provider => {
    const seq = ++ovSeq;
    const current = () => seq === ovSeq && !stale(gen);
    let ov;
    try { ov = await api(`/api/tunnel-accounts/${provider}/overview`); }
    catch (e) { if (current()) throw e; return null; }
    return current() && accOn(provider) ? ov : null;
  };
  // 连接状态请求的序号：先发后到的旧响应不能盖掉后发先到的新状态
  let stSeq = 0;
  let openFrpPoll = null; // { id, timer, stop }
  const stopOpenFrpPoll = () => {
    if (!openFrpPoll) return;
    clearTimeout(openFrpPoll.timer);
    openFrpPoll.stop = true;
    openFrpPoll = null;
  };
  /* 绑定服务器的下拉。当前绑着的服务器不在列表里（已经移出列表，或者这次没取到服务器列表）时也列出来、选中它，
     写明列表里没有这台：不然下拉落到「不绑定」，编辑时只改个名字也会把绑定悄悄清掉 */
  const boundSelect = (id, val = "", k = "") =>
    `<select id="${id}"${k ? ` data-k="${k}"` : ""} aria-label="绑定服务器"><option value="">不绑定</option>${insts.map(i =>
      `<option value="${esc(i.name)}"${i.name === val ? " selected" : ""}>${esc(i.name)}（端口 ${i.port}）</option>`).join("")}${val && !insts.some(i => i.name === val)
      ? `<option value="${esc(val)}" selected>${esc(val)}（列表里没有这台）</option>` : ""}</select>`;

  const paintAccCards = () => {
    const box = $("#tn-acc-cards");
    if (!box || stale(gen)) return;
    const ro = accRO();
    const roTip = $("#tn-acc-ro");
    if (roTip) roTip.hidden = !ro;
    const card = (provider) => {
      const on = accOn(provider);
      const name = ACC_NAMES[provider];
      const polling = !ro && openFrpPoll && provider === "openfrp";
      // 本机存过凭据但解不开（多半换了电脑或 Windows 用户）：按未连接显示，多一句提示
      const lost = accStatus[provider + "Unreadable"]
        ? `<div class="hint" style="margin:0 0 8px;color:var(--warning-text)" data-acc-unreadable="${provider}">之前保存的凭据在这台电脑上读不出来（可能换了电脑或 Windows 用户），请重新连接。</div>` : "";
      let body, actions;
      if (ro) {
        // 局域网打开：只留名称和连接状态，不出输入框和任何按钮
        body = lost;
        actions = "";
      } else if (provider === "openfrp" && !on) {
        body = lost + (polling
          ? `<div class="hint">已打开授权页，正在等待你在浏览器里完成授权…</div>`
          : `<div class="hint">通过 OpenFrp 远程授权连接。MineDeck 不会接收或保存账号密码。</div>`);
        actions = polling
          ? `<button class="btn sm" data-acc-cancel-login="openfrp">取消等待</button>`
          : `<button class="btn sm primary" data-acc-connect="openfrp" aria-label="浏览器授权连接 OpenFrp">${icon("link")} 浏览器授权</button>`;
      } else if (provider === "natfrp" && !on) {
        body = lost + `<div class="hint" style="margin-bottom:8px">在樱花官网的用户信息页复制访问密钥，粘贴到这里连接。连上以后能直接在平台上建隧道、导入已有的。</div>${natfrpFrpcSteps(base)}
          <label class="field" style="margin:0">
            <input type="password" id="tn-sakura-token" placeholder="粘贴访问密钥" aria-label="SakuraFrp 访问密钥" autocomplete="off"></label>`;
        actions = `<button class="btn sm" data-acc-get-token="natfrp" title="在浏览器里打开樱花官网的用户信息页">获取访问密钥</button>
          <button class="btn sm primary" data-acc-connect="natfrp" aria-label="连接樱花frp">${icon("link")} 连接</button>`;
      } else {
        const ov = accPanel && accPanel.provider === provider ? accPanel.overview : null;
        const a = ov && ov.account;
        const signTxt = a && a.signedToday != null ? (a.signedToday ? "今日已签" : "今日未签") : "";
        body = a
          ? `<div style="font-size:var(--fs-sm);line-height:1.7">
              <b>${esc(a.name || name)}</b>${a.group ? ` · ${esc(a.group)}` : ""}
              ${a.trafficSummary ? `<div class="hint" style="margin:2px 0 0">${esc(a.trafficSummary)}</div>` : ""}
              <div class="hint" style="margin:2px 0 0">隧道 ${a.tunnelCount ?? "—"}${a.tunnelLimit ? ` / ${a.tunnelLimit}` : ""}${signTxt ? ` · ${signTxt}` : ""}${a.consecutiveDays ? ` · 连续 ${a.consecutiveDays} 天` : ""}</div>
            </div>`
          : `<div class="hint">已连接。点「概览」查看流量、签到与平台隧道。</div>`;
        actions = `
          <button class="btn sm" data-acc-overview="${provider}">${accPanel && accPanel.provider === provider ? "收起概览" : "概览"}</button>
          <button class="btn sm" data-acc-sign="${provider}">签到</button>
          <button class="btn sm" data-acc-buy="${provider}">购买</button>
          <button class="btn sm danger" data-acc-disconnect="${provider}">断开</button>`;
      }
      return `<div class="java-row" style="max-width:none;align-items:flex-start;flex-direction:column;gap:10px" data-acc="${provider}">
        <div style="display:flex;align-items:center;gap:8px;width:100%">
          <b>${esc(name)}</b>
          <span class="badge ${on ? "core" : ""}">${on ? "已连接" : (polling ? "授权中" : "未连接")}</span>
          <span class="grow"></span>
        </div>
        ${body ? `<div style="width:100%">${body}</div>` : ""}
        ${actions ? `<div style="display:flex;flex-wrap:wrap;gap:6px;width:100%">${actions}</div>` : ""}
      </div>`;
    };
    box.innerHTML = card("openfrp") + card("natfrp");
    box.querySelectorAll("[data-acc-connect]").forEach(b => b.onclick = () => connectAccount(b.dataset.accConnect, b));
    box.querySelectorAll("[data-acc-get-token]").forEach(b => b.onclick = () => openExt("https://www.natfrp.com/user/profile"));
    box.querySelectorAll("[data-acc-cancel-login]").forEach(b => b.onclick = async () => {
      const id = openFrpPoll && openFrpPoll.id;
      stopOpenFrpPoll();
      paintAccCards();
      if (!id) { toast("已取消等待授权"); return; }
      // 后端那边也作废：已经发出去的轮询就算拿到授权也不保存。等它回话再说结果
      try {
        const r = await api(`/api/tunnel-accounts/openfrp/login/${encodeURIComponent(id)}/cancel`, { method: "POST" });
        if (stale(gen)) return;
        if (r && r.cancelled === false) {
          // 这次授权已经不在等了：多半是取消前刚好授权成功，也可能过期了。以后端的连接状态为准，查不到就不下结论
          const known = await refreshAccounts("openfrp");
          if (stale(gen)) return;
          if (!known) toast("这次授权在取消前已经结束，但没能确认现在的连接状态，请稍后刷新页面看看", true);
          else toast(accOn("openfrp") ? "这次授权在取消前已经完成，OpenFrp 已连接" : "这次授权已经结束，OpenFrp 没有连上");
          return;
        }
        toast("已取消等待授权");
      } catch (e) {
        if (stale(gen)) return;
        toast("取消授权没有成功：" + e.message + "。如果之后在浏览器里完成了授权，账户仍可能连上。", true);
        await refreshAccounts();
      }
    });
    box.querySelectorAll("[data-acc-disconnect]").forEach(b => b.onclick = () => disconnectAccount(b.dataset.accDisconnect));
    box.querySelectorAll("[data-acc-overview]").forEach(b => b.onclick = () => toggleOverview(b.dataset.accOverview));
    box.querySelectorAll("[data-acc-sign]").forEach(b => b.onclick = () => signAccount(b.dataset.accSign));
    box.querySelectorAll("[data-acc-buy]").forEach(b => b.onclick = () => buyAccount(b.dataset.accBuy));
  };

  const paintAccPanel = () => {
    const el = $("#tn-acc-panel");
    if (!el || stale(gen)) return;
    if (!accPanel || !accPanel.overview) { el.hidden = true; el.innerHTML = ""; return; }
    const { provider, overview: ov } = accPanel;
    const tunnels = ov.tunnels || [];
    const nodes = (ov.nodes || []).filter(n => n.canCreate);
    el.hidden = false;
    el.innerHTML = `
      <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:8px">
        <b>${esc(ACC_NAMES[provider])} 概览</b>
        <span class="hint" style="margin:0">平台上的隧道可导入到本机；也可在可用节点上创建并导入。</span>
        <span class="grow"></span>
        <button class="btn sm primary" data-acc-create="${esc(provider)}">${icon("plus")} 创建隧道并导入</button>
      </div>
      <div style="margin-bottom:10px">
        ${tunnels.length ? tunnels.map(t => `
          <div class="java-row" style="max-width:none" data-plat-tun="${esc(t.id)}">
            <b style="min-width:90px">${esc(t.name)}</b>
            <span class="badge">${esc((t.type || "").toUpperCase() || "?")}</span>
            ${t.nodeName ? `<span class="badge">${esc(t.nodeName)}</span>` : ""}
            ${t.remote ? `<span class="jv" style="font-family:var(--mono)">${esc(t.remote)}</span>` : ""}
            <span class="jp">${t.online ? "在线" : "离线"}${t.enabled === false ? " · 已禁用" : ""}</span>
            <span class="grow"></span>
            ${t.canImport ? `<button class="btn sm" data-acc-import="${esc(provider)}" data-proxy="${esc(t.id)}" data-type="${esc(t.type || "")}" aria-label="导入 ${esc(t.name)}">导入到 MineDeck</button>` : `<span class="hint" style="margin:0">不可导入</span>`}
          </div>`).join("") : `<div class="hint">平台上还没有隧道</div>`}
      </div>
      ${nodes.length ? `<div class="hint">可创建节点：${nodes.map(n => esc(n.name)).join("、")}</div>` : `<div class="hint">当前没有可创建隧道的节点</div>`}`;
    el.querySelectorAll("[data-acc-import]").forEach(b => b.onclick = () => importPlatTunnel(b.dataset.accImport, b.dataset.proxy, b.dataset.type, b));
    const createBtn = el.querySelector("[data-acc-create]");
    if (createBtn) createBtn.onclick = () => createPlatTunnel(provider, ov);
  };

  // 返回是否拿到了当前的连接状态（失败、被后一次刷新盖过、页面已经换了都算没拿到）
  const refreshAccounts = async (preferProvider) => {
    const seq = ++stSeq;
    const ov0 = ovSeq; // 发状态请求时的概览版本：等状态的这段时间用户收起、切换或断开过，就不再按这次的意图动面板
    let st;
    try { st = await api("/api/tunnel-accounts"); }
    catch (e) { if (seq === stSeq && !stale(gen)) toast(e.message, true); return false; }
    if (stale(gen) || seq !== stSeq) return false;
    accStatus = st || { openfrp: false, natfrp: false, local: true };
    if (accRO()) {
      // 局域网打开：概览是仅本机的接口，不去拉
      stopOpenFrpPoll();
      accPanel = null;
    } else if (ovSeq !== ov0) {
      // 等状态的时候用户已经动过概览：以用户的操作为准，这次刷新不展开、不换面板，只清掉已经断开的那家
      if (accPanel && !accOn(accPanel.provider)) accPanel = null;
    } else if (preferProvider && accOn(preferProvider)) {
      try {
        const ov = await fetchOverview(preferProvider);
        if (ov) accPanel = { provider: preferProvider, overview: ov };
      } catch (e) {
        // fetchOverview 只在这次仍是最新的概览请求时才抛，抛了才清面板
        if (!stale(gen)) toast(e.message, true);
        accPanel = null;
      }
    } else if (accPanel && !accOn(accPanel.provider)) {
      accPanel = null;
    } else if (accPanel && accOn(accPanel.provider)) {
      const p = accPanel.provider;
      try {
        const ov = await fetchOverview(p);
        if (ov && accPanel && accPanel.provider === p) accPanel.overview = ov;
      } catch { /* 保留旧概览 */ }
    }
    if (stale(gen)) return false;
    paintAccCards();
    paintAccPanel();
    return seq === stSeq; // 等概览的时候又开始了一次刷新：这次拿到的不算最新
  };

  const beginOpenFrpBrowserLogin = async (btn) => {
    if (btn) btn.disabled = true;
    try {
      const login = await api("/api/tunnel-accounts/openfrp/login", { method: "POST" });
      if (stale(gen)) {
        if (login && login.id) api(`/api/tunnel-accounts/openfrp/login/${encodeURIComponent(login.id)}/cancel`, { method: "POST" }).catch(() => {});
        return;
      }
      if (!login || !login.id || !login.url) throw new Error("未返回授权链接");
      openExt(login.url);
      stopOpenFrpPoll();
      const state = { id: login.id, stop: false, timer: null };
      openFrpPoll = state;
      paintAccCards();
      toast("已打开 OpenFrp 授权页，请在浏览器完成授权");
      const tick = async () => {
        if (state.stop || stale(gen) || openFrpPoll !== state) return;
        try {
          const r = await api(`/api/tunnel-accounts/openfrp/login/${encodeURIComponent(state.id)}/poll`, { method: "POST" });
          if (state.stop || stale(gen) || openFrpPoll !== state) return;
          if (r && r.pending === false) {
            stopOpenFrpPoll();
            toast("OpenFrp 已连接");
            await refreshAccounts("openfrp");
            return;
          }
          if (state.stop || stale(gen) || openFrpPoll !== state) return;
        } catch (e) {
          if (state.stop || stale(gen)) return;
          stopOpenFrpPoll();
          paintAccCards();
          toast(e.message, true);
          return;
        }
        state.timer = setTimeout(tick, pollMs());
      };
      state.timer = setTimeout(tick, pollMs());
    } catch (e) { if (!stale(gen)) toast(e.message, true); }
    if (btn) btn.disabled = false;
  };

  const connectAccount = async (provider, btn) => {
    if (provider === "openfrp") {
      await beginOpenFrpBrowserLogin(btn);
      return;
    }
    if (provider === "natfrp") {
      const token = ($("#tn-sakura-token") || {}).value;
      if (!token || !String(token).trim()) { toast("请先粘贴 SakuraFrp 访问密钥", true); return; }
      if (btn) btn.disabled = true;
      try {
        await api("/api/tunnel-accounts/natfrp/connect", { method: "POST", body: { token: String(token).trim() } });
        if (stale(gen)) return;
        toast("樱花frp 已连接");
        await refreshAccounts("natfrp");
      } catch (e) { if (!stale(gen)) toast(e.message, true); }
      if (btn) btn.disabled = false;
    }
  };

  const disconnectAccount = async (provider) => {
    const r = await confirmModal({
      title: `断开 ${ACC_NAMES[provider]}？`,
      body: provider === "natfrp"
        ? "会清除本机保存的樱花访问密钥。已导入到 MineDeck 的樱花隧道里仍保存着同一把访问密钥，要彻底清除请删除这些隧道，必要时到樱花官网重置访问密钥。"
        : "会清除本机保存的平台凭据。已导入到 MineDeck 的隧道不会删除。",
      okText: "断开", danger: true, icon: "warn",
    });
    if (!r || stale(gen)) return;
    try {
      await api(`/api/tunnel-accounts/${provider}`, { method: "DELETE" });
      if (stale(gen)) return;
      if (provider === "openfrp") stopOpenFrpPoll();
      if (accPanel && accPanel.provider === provider) accPanel = null;
      ovSeq++; // 断开前发出的概览请求回来也不再画
      toast("已断开 " + ACC_NAMES[provider]);
      await refreshAccounts();
    } catch (e) { if (!stale(gen)) toast(e.message, true); }
  };

  const toggleOverview = async (provider) => {
    if (accPanel && accPanel.provider === provider) {
      accPanel = null;
      ovSeq++; // 收起以后，在途的概览请求回来也不再把它展开
      paintAccCards();
      paintAccPanel();
      return;
    }
    try {
      const overview = await fetchOverview(provider);
      if (!overview) return; // 期间点了别的、或者账户已经断开：这份过期了
      accPanel = { provider, overview };
      paintAccCards();
      paintAccPanel();
    } catch (e) { if (!stale(gen)) toast(e.message, true); }
  };

  // 签到：两家都不代签。后端只回官网入口（openURL）和一句说明，确认后在浏览器里打开，签到在官网完成
  const signAccount = async (provider) => {
    let r;
    try { r = await api(`/api/tunnel-accounts/${provider}/sign`, { method: "POST", body: {} }); }
    catch (e) { if (!stale(gen)) toast(e.message, true); return; }
    if (stale(gen)) return;
    const openURL = r && typeof r.openURL === "string" && /^https:\/\//i.test(r.openURL) ? r.openURL : "";
    if (!openURL) { toast((r && r.message) || "没有拿到官网的签到入口", true); return; }
    const go = await confirmModal({
      title: "去官网签到",
      body: esc(r.message || `${ACC_NAMES[provider]} 的签到要在官网完成。`) + `<div class="hint" style="margin-top:8px">将打开 ${esc(openURL)}</div>`,
      okText: "打开官网", cancelText: "取消",
    });
    if (go) openExt(openURL);
  };

  const buyAccount = async (provider) => {
    let data;
    try { data = await api(`/api/tunnel-accounts/${provider}/purchase`); }
    catch (e) { if (!stale(gen)) toast(e.message, true); return; }
    if (stale(gen)) return; // 已经切到别的页面：不在那里弹购买窗
    const opts = (data && data.options) || [];
    if (!opts.length) { toast("暂无购买入口", true); return; }
    const prev = document.activeElement;
    // 叠一层（modalLayer），不整个换掉 #modal-root：下面开着的弹窗和填的东西都还在（可用性走查 R04 留下的）
    const tid = layerId("tn-buy-title");
    const mask = modalLayer(`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="${tid}" style="max-width:440px">
      <h3 id="${tid}">${esc(ACC_NAMES[provider])} 购买 / 充值</h3>
      <div class="m-body">
        <div class="hint" style="margin-bottom:10px">只打开官方页面，MineDeck 不代下单、不代收。</div>
        <div style="display:flex;flex-direction:column;gap:8px">
          ${opts.map((o, i) => `<button class="btn" data-buy="${i}" style="justify-content:space-between">${esc(o.title)} ${icon("arrowRight")}</button>`).join("")}
        </div>
      </div>
      <div class="m-actions"><button class="btn" data-x="cancel">关闭</button></div>
    </div>`);
    const close = () => { document.removeEventListener("keydown", onKey); dismiss(mask); if (prev && prev.focus && prev.isConnected) prev.focus(); };
    const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(); }; // 只关最上面这一层
    document.addEventListener("keydown", onKey);
    mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget) close(); });
    mask.querySelector('[data-x="cancel"]').onclick = close;
    mask.querySelectorAll("[data-buy]").forEach(b => b.onclick = () => {
      const o = opts[+b.dataset.buy];
      if (o && o.url) openExt(o.url);
    });
    mask.querySelector('[data-x="cancel"]').focus();
  };

  // 用途跟着平台隧道的类型走：udp 是语音，其余是游戏
  const importPlatTunnel = async (provider, proxyId, type, btn) => {
    const r = await confirmModal({
      title: "导入到 MineDeck？",
      body: `将平台隧道 <b>${esc(proxyId)}</b> 写入本机隧道列表（凭据由账户密钥填充）。`,
      okText: "导入",
      extra: `<div class="form-grid" style="margin-top:12px">
        <label class="field full"><span>绑定服务器</span>${boundSelect("tn-imp-bound", "", "bound")}</label>
        <div class="field full"><label class="switch"><input type="checkbox" id="tn-imp-auto" data-k="auto"><span class="sw"></span>
          <span class="sw-label">跟随服务器启动</span></label></div>
      </div>`,
    });
    if (!r || stale(gen)) return;
    if (btn) btn.disabled = true;
    try {
      await api(`/api/tunnel-accounts/${provider}/import`, {
        method: "POST",
        body: {
          proxyId,
          boundInstance: r.bound || "",
          autoStart: !!r.auto,
          purpose: String(type || "").toLowerCase() === "udp" ? "voice" : "game",
        },
      });
      if (stale(gen)) return;
      toast("已导入到 MineDeck");
      await drawList();
      if (stale(gen)) return;
      await refreshAccounts(provider);
    } catch (e) {
      // 后端另给了一句人话（tunnels.json 写不进去时那句）就显示它，原文进「详情」；没有就和以前一样显示原文
      if (!stale(gen)) toast(e.hint || e.message, true, e.message);
    }
    if (btn) btn.disabled = false;
  };

  // OpenFrp 的隧道名规则（同后端）；两家的默认名都给英文：minedeck_ 加 4 位随机小写字母或数字
  const OPENFRP_NAME_RE = /^[A-Za-z0-9_-]{1,30}$/;
  const defaultPlatTunName = () => {
    const abc = "abcdefghijklmnopqrstuvwxyz0123456789";
    let s = "minedeck_";
    for (let i = 0; i < 4; i++) s += abc[Math.floor(Math.random() * abc.length)];
    return s;
  };
  const createPlatTunnel = async (provider, overview) => {
    const nodes = (overview.nodes || []).filter(n => n.canCreate);
    if (!nodes.length) { toast("没有可创建的节点", true); return; }
    const prev = document.activeElement;
    const isOpenFrp = provider === "openfrp", isSakura = provider === "natfrp";
    const nodeOpts = list => list.map(n =>
      `<option value="${esc(n.id)}">${esc(n.name)}${n.udp ? "" : "（无 UDP）"}</option>`).join("");
    // 叠一层（modalLayer），不整个换掉 #modal-root；框里的东西都从这一层里找（可用性走查 R04 留下的）
    const tid = layerId("tn-create-title");
    const mask = modalLayer(`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="${tid}" style="max-width:520px">
      <h3 id="${tid}">在 ${esc(ACC_NAMES[provider])} 创建并导入</h3>
      <div class="m-body">
        <div class="form-grid">
          <label class="field"><span>隧道名称</span><input type="text" id="tn-cr-name" value="${esc(defaultPlatTunName())}" maxlength="30" autocomplete="off" spellcheck="false">
            ${isOpenFrp ? `<small class="hint" id="tn-cr-name-hint" style="display:block">OpenFrp 的隧道名只能用英文字母、数字、下划线和连字符</small>` : ""}</label>
          <label class="field"><span>节点</span><select id="tn-cr-node">${nodeOpts(nodes)}</select></label>
          <label class="field"><span>类型</span><select id="tn-cr-type"><option value="tcp">TCP</option><option value="udp">UDP</option></select></label>
          <label class="field"><span>本地端口</span><input type="number" id="tn-cr-lport" value="25565" min="1" max="65535"></label>
          <label class="field"${isOpenFrp ? "" : " hidden"}><span>公网端口</span><input type="number" id="tn-cr-rport" value="25566" min="1" max="65535"></label>
          ${isSakura ? `<div class="field full"><label class="switch"><input type="checkbox" id="tn-cr-public"><span class="sw"></span>
            <span class="sw-label">这是公开服务器</span></label>
            <div class="hint" id="tn-cr-public-desc">公开服务器指把连接方式发到论坛、服务器列表、QQ 大群，或在 B 站、抖音、小红书等公开平台宣传招人的服务器。樱花规定公开服务器只能用公开服专用节点（名称含「多线」或带公开游戏标记），用错节点会被封号且不退款。只和熟人小范围联机可以不勾。</div></div>` : ""}
          <label class="field full"><span>绑定服务器</span>${boundSelect("tn-cr-bound")}</label>
          <div class="field full"><label class="switch"><input type="checkbox" id="tn-cr-auto"><span class="sw"></span>
            <span class="sw-label">跟随服务器启动</span></label></div>
        </div>
        <div class="field-err" id="tn-cr-err" role="alert"></div>
      </div>
      <div class="m-actions">
        <button class="btn" data-x="cancel">取消</button>
        <button class="btn primary" data-x="ok">创建并导入</button>
        <button class="btn primary" data-x="go-import" hidden>关闭，去导入</button>
      </div>
    </div>`);
    let closed = false;
    // busy：请求途中，或者平台上已经建好、本机没导入成。两种都不能再发创建：后一种再点一次，平台上就多一条（A57）
    let busy = false;
    const close = () => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      dismiss(mask);
      if (prev && prev.focus && prev.isConnected) prev.focus();
    };
    const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(); }; // 只关最上面这一层（上面叠着「报错信息」时先关那个）
    document.addEventListener("keydown", onKey);
    // 框里有要填的东西：点遮罩不关（同交接弹窗，可用性走查 U04）
    mask.querySelector('[data-x="cancel"]').onclick = close;
    const okBtn = mask.querySelector('[data-x="ok"]');
    const q = sel => mask.querySelector(sel);
    const nameIn = q("#tn-cr-name"), nodeSel = q("#tn-cr-node"), pub = q("#tn-cr-public"), errEl = q("#tn-cr-err");
    // 本地端口随绑定的服务器填（同添加表单）：TCP 才填，UDP 是语音隧道，端口是 SVC 的；自己改过就不再替人改
    let lportTyped = false;
    q("#tn-cr-lport").addEventListener("input", () => { lportTyped = true; });
    q("#tn-cr-bound").addEventListener("change", () => {
      const i = insts.find(x => x.name === q("#tn-cr-bound").value), port = i ? connectPortOf(i) : 0;
      if (!lportTyped && port && q("#tn-cr-type").value === "tcp") q("#tn-cr-lport").value = port;
    });
    // 对话框里的提示：kind 记下是哪一类（name 名称不合规 / node 没有可用节点 / server 后端报的错），改了对应的地方才清掉
    const showErr = (msg, kind = "") => { errEl.textContent = msg || ""; errEl.dataset.kind = msg ? kind : ""; };
    const showServerErr = e => { errEl.innerHTML = errInline("没能创建隧道", e, { label: "创建隧道" }); errEl.dataset.kind = "server"; };
    nameIn.oninput = () => { if (errEl.dataset.kind === "name") showErr(""); };
    // 樱花勾了「这是公开服务器」：节点只列公开服专用的（publicGameAllowed）；一个都没有就不让创建
    const paintNodes = () => {
      const list = pub && pub.checked ? nodes.filter(n => n.publicGameAllowed === true) : nodes;
      const keep = nodeSel.value;
      nodeSel.innerHTML = nodeOpts(list);
      if (list.some(n => String(n.id) === keep)) nodeSel.value = keep;
      nodeSel.disabled = !list.length;
      okBtn.disabled = busy || !list.length; // 请求途中切开关也不能把按钮放开
      if (!list.length) showErr("没有可用的公开服专用节点，公开服务器暂时不能在这里创建隧道。", "node");
      else if (errEl.dataset.kind === "node") showErr("");
    };
    if (pub) pub.onchange = paintNodes;
    // 平台上已经建好、本机没导入成：主按钮换成「关闭，去导入」。关掉以后等后面的概览拉完，平台隧道列表里同名的只有一条时
    // 滚过去、焦点放到它的「导入到 MineDeck」上；认不准就只关掉
    const goBtn = q('[data-x="go-import"]');
    const showGoImport = (name, reload) => {
      okBtn.hidden = true;
      goBtn.hidden = false;
      goBtn.focus();
      goBtn.onclick = async () => {
        close();
        await reload;
        if (stale(gen)) return;
        const rows = [...document.querySelectorAll("#tn-acc-panel [data-plat-tun]")].filter(r => (r.querySelector("b") || {}).textContent === name);
        const imp = rows.length === 1 && rows[0].querySelector("[data-acc-import]");
        if (!imp) return;
        rows[0].scrollIntoView({ block: "center" });
        imp.focus();
      };
    };
    okBtn.onclick = async () => {
      if (busy) return;
      const name = nameIn.value.trim();
      if (isOpenFrp && !OPENFRP_NAME_RE.test(name)) {
        showErr("OpenFrp 的隧道名只能用英文字母、数字、下划线和连字符，最长 30 个字符", "name");
        nameIn.focus();
        return;
      }
      if (!name) { showErr("请填写隧道名称", "name"); nameIn.focus(); return; }
      showErr("");
      const body = {
        name,
        nodeId: +nodeSel.value,
        type: q("#tn-cr-type").value,
        localPort: +q("#tn-cr-lport").value || 0,
        remotePort: isOpenFrp ? (+(q("#tn-cr-rport") || {}).value || 0) : 0, // 樱花不填公网端口，由平台分配
        boundInstance: q("#tn-cr-bound").value,
        autoStart: q("#tn-cr-auto").checked,
        purpose: q("#tn-cr-type").value === "udp" ? "voice" : "game",
      };
      if (isSakura) body.publicServer = !!(pub && pub.checked);
      busy = okBtn.disabled = true;
      if (pub) pub.disabled = true; // 切它会重画节点、改写下面那句报错（paintNodes）：请求途中和建成以后都不让动
      try {
        await api(`/api/tunnel-accounts/${provider}/tunnels`, { method: "POST", body });
        close();
        if (stale(gen)) return;
        toast("已创建并导入");
        await drawList();
        if (stale(gen)) return;
        await refreshAccounts(provider);
      } catch (e) {
        if (stale(gen)) { close(); return; }
        /* 原因写在对话框里（以前只弹一下提示），填的东西都留着；正文接后端另给的那句人话，原文在复制键里。
           后面的概览一律重新拉（后端已丢掉概览缓存）：平台上可能已经建好了，关掉对话框就能看到那一条。
           平台上没建成的，按钮恢复，改了再试（没有可用节点时仍点不了，同 paintNodes）；后端说平台上已经建好了的（created），
           按钮不再放开、换成「关闭，去导入」（A57）。等的时候对话框已经被关掉的，照旧弹提示 */
        const reload = refreshAccounts(provider);
        if (closed) { toastErr("没能创建隧道", e); return; }
        showServerErr(e);
        if (e.created) showGoImport(name, reload);
        else { busy = false; okBtn.disabled = nodeSel.disabled; if (pub) pub.disabled = false; }
      }
    };
    nameIn.focus();
  };

  // 日志展开着的隧道：列表照常刷新（以前日志一展开整个列表就停刷，点了「启动」卡片也不变，测试报告第五轮），
  // 重绘时把那个日志框摘下来再放回原位，SSE 不断。
  let logId = null;
  let shown = null; // 上次画出来的列表；没变就不重画，不打断「已复制」的动效和键盘焦点
  let drawSeq = 0;
  const closeLog = () => { logId = null; if (consoleES) { consoleES.close(); consoleES = null; } };
  const warnSpan = (text, title) => `<span class="jp" style="color:var(--danger-text);display:inline-flex;align-items:center;gap:5px"${title ? ` title="${esc(title)}"` : ""}>${icon("warn")} ${esc(text)}</span>`;
  /* 这条隧道给朋友的公网地址（可用性走查 R12：不启动也能看到、能复制）。返回 { addr, last }，没有可给的返回 null。
     - 自定义 frps：地址就是配置里的「frps 地址:公网端口」，不用等 frpc 跑起来（frpc 连上以后后端记的也是这一串）；
     - 樱花、OpenFrp：地址要 frpc 连上以后才知道。停着时给上次拿到的那个，标「上次」；运行中还没连上时不拿上次的充数，
       不然新一次运行连不上时，卡片上那个看着能用的地址会盖住这次的错（Codex 复审发现）；
     - 凭据解不开：要删了重新添加，地址给了也用不上，不给 */
  const tunAddr = t => {
    if (t.credBroken) return null;
    if (t.provider === "custom") {
      if (t.running && t.online && t.publicAddr) return { addr: t.publicAddr, last: false };
      if (!t.serverAddr || !(t.remotePort > 0)) return null;
      const host = t.serverAddr.includes(":") ? `[${t.serverAddr}]` : t.serverAddr; // IPv6 加方括号，游戏里才填得进去
      return { addr: `${host}:${t.remotePort}`, last: false };
    }
    if (!t.publicAddr) return null;
    if (t.running) return t.online ? { addr: t.publicAddr, last: false } : null;
    return { addr: t.publicAddr, last: true };
  };
  const tunRow = t => {
    // 最近一次失败原因（启动失败、跟随启动失败、frpc 报错或意外退出）直接写在卡片上，不用点开日志；
    // 跑起来拿到地址后不再显示（后端在 frpc 起来时也会清掉）。「这次连上了没有」看 online（Codex 复审发现）
    const err = !t.credBroken && t.lastError && !(t.running && t.online) ? t.lastError : "";
    const a = tunAddr(t);
    const state = t.credBroken ? warnSpan("凭据无法解开，请删除后重新添加", "凭据加密绑定当前电脑和 Windows 用户，换了就解不开")
      : err ? warnSpan(t.running ? "连接出错" : "上次出错")
      : t.running && !t.online ? `<span class="jp">连接中…</span>` : "";
    // 第二行：公网地址（带复制）和跟随状态，停着、没连上过也写清楚
    const addrHTML = a ? `<span class="tun-addr-k">公网地址</span><span class="jv tun-addr-v">${esc(a.addr)}</span>${a.last ? `<span class="tag tun-last" title="隧道停着，这是上次连上时拿到的地址。樱花、OpenFrp 的地址一般不变，启动后会刷新">上次</span>` : ""}
        <button class="btn sm" data-copy="${esc(a.addr)}" aria-label="复制 ${esc(t.name)} 的公网地址">${icon("copy")} 复制</button>`
      : t.credBroken ? "" : `<span class="tun-addr-k">公网地址</span><span class="tun-addr-none">${t.running ? "连上以后显示" : "还没拿到地址，启动后显示"}</span>`;
    const follow = t.boundInstance
      ? `<span class="tun-follow">${icon("link")} 跟随「${esc(t.boundInstance)}」启动：<b>${t.autoStart ? "开" : "关"}</b></span>`
      : `<span class="tun-follow">没绑定服务器，只能手动启动</span>`;
    return `
      <div class="java-row tun-row" data-tun="${esc(t.id)}">
        ${stPill(t.running ? "running" : "stopped")}
        <b style="min-width:110px">${esc(t.name)}</b>
        <span class="badge core">${esc(PROV_NAMES[t.provider] || t.provider)}</span>${window.MDMusic ? MDMusic.tunnelBadge(t) : ""}
        ${state}
        <span class="grow"></span>
        ${t.running ? `<button class="btn sm" data-tstop="${esc(t.id)}" aria-label="停止 ${esc(t.name)}">${icon("stop")} 停止</button>` : `<button class="btn sm primary" data-tstart="${esc(t.id)}" aria-label="启动 ${esc(t.name)}">${icon("play")} 启动</button>`}
        <button class="btn sm" data-tlog="${esc(t.id)}" aria-expanded="${logId === t.id}" aria-controls="tlog-${esc(t.id)}" aria-label="日志 ${esc(t.name)}">日志</button>
        <button class="btn sm" data-tedit="${esc(t.id)}" aria-label="编辑 ${esc(t.name)}">编辑</button>
        ${t.provider === "custom" && t.hasCredential ? `<button class="btn sm" data-tclear="${esc(t.id)}" aria-label="清除 ${esc(t.name)} 保存的凭据" title="删掉这台电脑上存着的 auth token，之后启动这条隧道不带 token。frps 没设 token 时用得上；设了的话会连不上，要删掉隧道重新添加">清除凭据</button>` : ""}
        <button class="btn sm danger" data-tdel="${esc(t.id)}" aria-label="删除 ${esc(t.name)}">删除</button>
        <div class="tun-meta">${addrHTML}${addrHTML ? `<span class="tun-sep" aria-hidden="true">·</span>` : ""}${follow}</div>
        ${err ? `<div class="tun-err">${esc(err)} <button type="button" class="btn sm ghost" data-copy-exact="${errRawAttr(err)}" data-copy-msg="已复制报错原文" aria-label="复制 ${esc(t.name)} 的报错原文">复制原文</button></div>` : ""}
      </div>
      <div class="task-log" id="tlog-${esc(t.id)}" style="display:none;height:150px;margin:-2px 0 8px"></div>`;
  };
  const drawList = async () => {
    const seq = ++drawSeq;
    let list;
    try { list = await api("/api/tunnels"); } catch (e) {
      if (stale(gen) || seq !== drawSeq) return null;
      /* tunnels.json 在、却没读出来（被别的程序占着，或者内容坏了）：后端回 503，不拿空列表冒充「没有隧道」。
         以前当成空列表，再加一条隧道就把原文件连同里面的隧道和凭据整份盖掉。正文接后端那句（原因和下一步，分两种情况），
         下面一行说清现在不能保存。后端每次被问都会再读一次，平时 4 秒一次的刷新读成了就照常画列表，不用重启。
         原因没变就不重画：不然重试键、复制键每 4 秒被换掉一次，正在点的按钮和键盘焦点都会丢 */
      if (e.status === 503) {
        const sig = "load-error\n" + e.message;
        if (shown !== sig) {
          closeLog(); shown = sig;
          $("#tun-summary").innerHTML = "";
          $("#tun-list").innerHTML = errBox("无法显示隧道", e, { id: "tun-load-err",
            hint: "原来的隧道和凭据都还在文件里，没有动过。读出来之前不能添加、修改或删除隧道，免得把它们盖掉；处理好以后这里会自动刷新。",
            actions: `<button type="button" class="btn sm" id="tun-load-retry">${icon("refresh")} 重试</button>` });
          $("#tun-load-retry").onclick = async () => {
            const b = $("#tun-load-retry");
            if (b) b.disabled = true;
            await drawList();
            // 读成了（列表画出来了），或者这次是连不上 MineDeck（换成了别的报错框）：都不用再说「还是没读出来」
            const b2 = $("#tun-load-retry"); // 原因变了会重画，按钮是新的
            if (stale(gen) || !blocked || !b2) return;
            b2.disabled = false;
            toast("隧道配置还是没读出来");
          };
        }
        blocked = true;
        paintForm();
        return null;
      }
      // 已经画着列表时偶尔刷新失败（网络抖一下）：列表和展开着的日志留着，摘要那里提示一句，下次刷新成功照常重画（独立评审发现）
      if (shown !== null && $("#tun-list [data-tun]")) { shown = null; $("#tun-summary").innerHTML = errInline("无法刷新隧道列表", e); return null; }
      closeLog(); shown = null; $("#tun-list").innerHTML = errBox("无法获取隧道列表", e);
      return null;
    }
    if (stale(gen) || seq !== drawSeq) return list; // 晚到的旧响应不画
    blocked = false; // 读出来了（或者本来就好好的）：下面照常画列表、放出表单
    const sig = JSON.stringify(list);
    if (sig === shown) return list;
    shown = sig;
    const runN = list.filter(t => t.running).length;
    $("#tun-summary").innerHTML = list.length ? `${list.length} 条隧道${SEP}运行中 ${runN}` : "";
    isEmpty = !list.length;
    paintForm();
    if (isEmpty) { closeLog(); $("#tun-list").innerHTML = `<div class="empty tun-empty">${MDPixel.emptySVG(160)}<div class="empty-title">还没有隧道</div>在上面填好表单添加第一条。启动隧道后会显示公网地址，发给朋友即可连接。</div>`; return list; }
    const logBox = logId ? $("#tlog-" + logId) : null;
    $("#tun-list").innerHTML = list.map(tunRow).join("");
    if (logBox) { const ph = $("#tlog-" + logId); if (ph) ph.replaceWith(logBox); else closeLog(); } // 那条隧道删了就关掉日志
    // 真写进剪贴板才换成对勾「已复制」（copyFlash）；写不进去弹手动复制框，按钮不变（PR #28 复审：这里以前不看结果）
    $("#tun-list").querySelectorAll("[data-copy]").forEach(b => b.onclick = () => copyFlash(b, b.dataset.copy, "已复制公网地址：" + b.dataset.copy));
    $("#tun-list").querySelectorAll("[data-tstart]").forEach(b => b.onclick = async () => {
      const id = b.dataset.tstart;
      b.disabled = true;
      try { await api(`/api/tunnels/${id}/start`, { method: "POST" }); toast("隧道启动中，公网地址稍后显示在列表"); }
      catch (e) { toastErr("无法启动隧道", e); }
      b.disabled = false; // 列表没变（比如同样的错误）时不会重画，按钮要自己恢复
      watch(id, t => !t.running || !!t.online || !!t.lastError); // 这次连上（拿到公网地址）、报错或者停下就算稳定
    });
    $("#tun-list").querySelectorAll("[data-tstop]").forEach(b => b.onclick = async () => {
      const id = b.dataset.tstop;
      b.disabled = true;
      try { await api(`/api/tunnels/${id}/stop`, { method: "POST" }); } catch (e) { toastErr("无法停止隧道", e); }
      b.disabled = false;
      watch(id, t => !t.running);
    });
    $("#tun-list").querySelectorAll("[data-tdel]").forEach(b => b.onclick = async () => {
      const r = await confirmModal({ title: "删除隧道？", danger: true, okText: "删除", icon: "trash", body: "仅删除本机配置，服务商侧隧道不受影响。" });
      if (!r) return;
      try { await api(`/api/tunnels/${b.dataset.tdel}`, { method: "DELETE" }); } catch (e) { toastErr("无法删除隧道", e); }
      drawList();
    });
    /* 自定义 frps 存过 token 以后，修改时凭据留空是「不改」，靠这个按钮明说不要了（clearCredential），之后启动不带 token。
       修改接口只改传了的项：这里只发 clearCredential，名称、绑定、跟随启动都不带（列表里的可能是几秒前的，
       另一个窗口刚改过的会被改回去，Codex 复审 v3.2.0 第二轮） */
    $("#tun-list").querySelectorAll("[data-tclear]").forEach(b => b.onclick = async () => {
      const t = list.find(x => x.id === b.dataset.tclear);
      if (!t) return;
      const r = await confirmModal({ title: `清除「${t.name}」保存的凭据？`, danger: true, okText: "清除", icon: "trash",
        body: "之后启动这条隧道不带 auth token。frps 设了 token 的话会连不上，到时删掉这条隧道重新添加、填上 token 就行。" });
      if (!r) return;
      try { await api(`/api/tunnels/${t.id}`, { method: "PUT", body: { clearCredential: true } }); toast(`已清除「${t.name}」保存的凭据`); }
      catch (e) { toastErr("无法清除凭据", e); }
      drawList();
    });
    $("#tun-list").querySelectorAll("[data-tedit]").forEach(b => b.onclick = () => {
      const t = list.find(x => x.id === b.dataset.tedit);
      if (t) editTunnel(t);
    });
    /* 日志：后端先把攒下的输出一次推过来，再接着推新的。还没有输出时写一句，不留一个空黑框（可用性走查 U21） */
    $("#tun-list").querySelectorAll("[data-tlog]").forEach(b => b.onclick = () => {
      const id = b.dataset.tlog, box = $("#tlog-" + id), was = logId === id;
      document.querySelectorAll(".task-log[id^=tlog-]").forEach(x => x.style.display = "none");
      $("#tun-list").querySelectorAll("[data-tlog]").forEach(x => x.setAttribute("aria-expanded", "false"));
      closeLog();
      if (was) return;
      logId = id;
      b.setAttribute("aria-expanded", "true");
      box.style.display = "";
      box.innerHTML = `<div class="tlog-empty">还没有日志。启动这条隧道以后，frpc 的输出会显示在这里。</div>`;
      let first = true;
      consoleES = new EventSource(`/api/tunnels/${id}/console?token=${TOKEN()}`);
      consoleES.onmessage = e => {
        if (first) { first = false; box.textContent = ""; }
        box.textContent += JSON.parse(e.data) + "\n";
        box.scrollTop = box.scrollHeight;
      };
    });
    return list;
  };
  /* 编辑隧道（可用性走查 R12）：只改名称、绑定的服务器、跟随启动三样。服务商、地址、端口、凭据不在这里改：
     换服务商或 frps 地址端口要重填凭据（后端 UpdateOpt 也这么拦），这些照旧删了重新添加。
     几道检查都在后端：只许本人操作（tunnel.update 是仅人工）、不许指向 MineDeck 的网页端口、tunnels.json 没读出来时拒绝、
     运行中不能改。被拒的原因写在弹窗里，填的东西不丢。叠一层（modalLayer），不整个换掉 #modal-root */
  const editTunnel = t => {
    const prev = document.activeElement;
    const tid = layerId("tn-ed-title");
    const mask = modalLayer(`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="${tid}" style="max-width:480px">
      <h3 id="${tid}">编辑隧道「${esc(t.name)}」</h3>
      <div class="m-body">
        <div class="form-grid">
          <label class="field full"><span>隧道名称</span><input type="text" id="tn-ed-name" value="${esc(t.name)}" maxlength="30" autocomplete="off"></label>
          <label class="field full"><span>绑定服务器</span>${boundSelect("tn-ed-bound", t.boundInstance || "")}</label>
          <div class="field full"><label class="switch"><input type="checkbox" id="tn-ed-auto"${t.autoStart ? " checked" : ""}><span class="sw"></span>
            <span><span class="sw-label">跟随服务器启动</span><div class="sw-desc">绑定的服务器启动时自动拉起隧道，停服后跟着停</div></span></label></div>
        </div>
        <div class="hint" style="margin-top:10px">服务商、地址、端口和凭据不在这里改：要换的话，删掉这条隧道，重新添加。</div>
        ${t.running ? `<div class="hint" id="tn-ed-running" style="margin-top:6px;color:var(--warning-text)">${icon("warn")} 这条隧道正在运行，先停止才能保存修改。</div>` : ""}
        <div class="field-err" id="tn-ed-err" role="alert"></div>
      </div>
      <div class="m-actions">
        <button class="btn" data-x="cancel">取消</button>
        <button class="btn primary" data-x="ok">保存</button>
      </div></div>`);
    let closed = false;
    const close = () => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      dismiss(mask);
      if (prev && prev.focus && prev.isConnected) prev.focus();
    };
    const onKey = e => { if (e.key === "Escape" && topLayer(mask)) close(); };
    document.addEventListener("keydown", onKey);
    // 有要填的东西：点遮罩不关（同交接弹窗）
    mask.querySelector('[data-x="cancel"]').onclick = close;
    const nameIn = mask.querySelector("#tn-ed-name"), errEl = mask.querySelector("#tn-ed-err"), okBtn = mask.querySelector('[data-x="ok"]');
    const showErr = html => { errEl.innerHTML = html || ""; };
    nameIn.addEventListener("input", () => showErr(""));
    nameIn.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); okBtn.click(); } });
    okBtn.onclick = async () => {
      const name = nameIn.value.trim();
      if (!name) { showErr("请填写隧道名称"); nameIn.focus(); return; }
      showErr("");
      // 只发这个窗口里改了的（修改接口只改传了的项）：弹窗是照打开那一刻的列表填的，别的窗口这期间改了跟随启动、
      // 这里只改名字的话，带上弹窗里的旧开关就把它改回去了（Codex 复审 v3.2.0 第二轮）。一样都没改就直接关掉
      const change = {}, bound = mask.querySelector("#tn-ed-bound").value, auto = mask.querySelector("#tn-ed-auto").checked;
      if (name !== t.name) change.name = name;
      if (bound !== (t.boundInstance || "")) change.boundInstance = bound;
      if (auto !== !!t.autoStart) change.autoStart = auto;
      if (!Object.keys(change).length) { close(); return; }
      okBtn.disabled = true;
      try {
        await api(`/api/tunnels/${encodeURIComponent(t.id)}`, { method: "PUT", body: change });
        close();
        if (stale(gen)) return;
        toast(`已保存「${name}」的修改`);
        drawList();
      } catch (e) {
        if (closed) return;
        okBtn.disabled = false;
        showErr(errInline("没能保存修改", e, { label: "编辑隧道" }));
      }
    };
    nameIn.focus();
    nameIn.select();
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
  await refreshAccounts();
  if (stale(gen)) return;
  await drawList();
  if (stale(gen)) return; // 等的时候已经切走：不再给（可能是另一个页面的）按钮挂事件、也不起轮询
  startPoll(drawList, 4000, gen);

  $("#tn-add").onclick = async () => {
    // 请求途中不许再点：以前连点两下会加出两条一样的隧道（A57）
    const addBtn = $("#tn-add");
    if (addBtn.disabled) return;
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
      /* 「跟随服务器启动」在这里、导入和平台上创建的对话框里都默认不勾（A62）：勾上以后每次开服都自动把服务器映射到公网，
         由人自己决定。后端没传这一项时也是不跟随（零值），AI 和 MCP 不能新建隧道 */
      autoStart: $("#tn-auto").checked,
      ...(window.MDMusic ? MDMusic.tunnelExtra() : {}), // 第五阶段：purpose、proto
    };
    addBtn.disabled = true;
    try {
      await api("/api/tunnels", { method: "POST", body });
      toast("隧道已添加");
      // 收起表单并清空密钥类字段，回到列表看新隧道
      formOpen = false;
      ["#tn-cred", "#tn-proxy", "#tn-saddr"].forEach(s => { $(s).value = ""; });
      credShort();
      await drawList();
      if (!stale(gen)) paintForm();
    } catch (e) { toastErr("无法添加隧道", e); } // 写不进磁盘时表单留着、填的东西不丢，按钮恢复，改了再试
    addBtn.disabled = false;
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
  { id: "remote", label: "手机远程管理", icon: "phone" }, // 原来叫「远程访问」：像是让朋友连进来，其实是手机管理页（可用性走查 U20）
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
          <a class="set-nav" data-sec="${s.id}" href="#/settings/${s.id}"><span class="sn-ico">${icon(s.icon)}</span>${s.label}${s.id === "about" ? `<i class="nav-dot" id="set-about-dot" hidden aria-hidden="true"></i>` : ""}</a>`).join("")}</nav>
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
  try { info = await api("/api/app"); } catch (e) { if (!stale(gen)) body.innerHTML = errBox("无法读取设置", e); return; }
  if (stale(gen)) return; // 快速连点分区时，先发后至的旧响应不得覆盖当前分区
  syncUpdateDot(info);
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
      <div class="hint" style="margin-top:20px">想换背景图？在程序所在的文件夹里新建 <code>bg</code> 文件夹并放入图片，下次打开时随机选一张作为壁纸。</div>`;
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
  body.innerHTML = settingsHead("启动与托盘", "关闭按钮的行为，以及开机是否自动启动。") +
    group(
      switchRow("monitor", "点关闭按钮时缩到托盘（不退出）", "开启后，点关闭按钮不退出程序，窗口缩到托盘，服务器继续运行；点托盘图标恢复窗口。第一次缩到托盘时会弹一条系统通知提醒一次。要退出，点左下角的「退出」，或者右键托盘图标。关闭后，点关闭按钮会退出程序并停掉服务器（有服务器在运行时先确认）。", "minimizeToTray", c.minimizeToTray) +
      switchRow("power", "开机自动启动", "登录 Windows 后自动在托盘里启动，不需要管理员权限（若移动过本程序所在文件夹，需重新开关一次以更新路径）。", "autoStart", info.autoStart),
      { ico: true }) +
    ``;
  body.querySelectorAll("input[data-cfg]").forEach(inp => inp.onchange = async () => {
    const key = inp.dataset.cfg;
    try {
      await api("/api/config", { method: "PUT", body: { [key]: inp.checked } });
      if (key === "minimizeToTray") c.minimizeToTray = inp.checked; else info.autoStart = inp.checked;
      toast(inp.checked ? "已开启" : "已关闭");
    } catch (e) { toastErr(`${inp.checked ? "无法开启" : "无法关闭"}「${key === "minimizeToTray" ? "点关闭按钮时缩到托盘" : "开机自动启动"}」`, e); inp.checked = !inp.checked; }
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
    } catch (e) { toastErr("无法切换下载源", e); }
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
    } catch (e) { toastErr("无法清除 CurseForge API Key", e); }
  };
}

/* — 存储位置 — */
function storageRow(ico, title, path, desc, pick) { // 三行都有「打开」「更改…」：名字带上是哪个目录
  return riRow({ ico, title, sub: esc(desc), key: path || "", right: `
      <button class="btn sm" data-open="${esc(path || "")}" aria-label="打开${esc(title)}">${icon("folder")} 打开</button>
      ${pick ? `<button class="btn sm" data-pick="${pick}" aria-label="更改${esc(title)}…">更改…</button>
      <button class="btn sm ghost icon-only" data-reset="${pick}" title="恢复默认文件夹" aria-label="恢复默认的${esc(title)}">${icon("refresh")}</button>` : ""}` });
}
/* 存储位置（R15、R16、R20、U29）：
   - 两个存放文件夹改了即时生效，提示里带「撤销」，点了换回改之前配置里的值（原来是默认位置就换回默认），R16；
   - 备份保留份数（全局 backupKeep）放在这里（以前在单台服务器的备份页上，可用性走查 U18、R15）。
     详情页的备份页只读显示份数、链接到这里：地址是 #/settings/storage，这一行的 id 是 set-backup-keep；
   - 服务器和备份设成同一个文件夹时提示，R20；备份那一行写「所有备份都放在这里」，U29 */
const sameDir = (a, b) => !!a && !!b && a.replace(/[\\/]+$/, "").toLowerCase() === b.replace(/[\\/]+$/, "").toLowerCase();
function setStorage(body, info) {
  const gen = viewGen;
  const cfg = info.config || (info.config = {});
  const KEY = { servers: "serversDir", backups: "backupsDir" };
  const ROOT = { servers: "serversRoot", backups: "backupsRoot" };
  const NAME = { servers: "服务器存放文件夹", backups: "备份存放文件夹" };
  let keep = cfg.backupKeep || 10;
  // 改了以后按后端的说法重画（恢复默认时要后端给出默认的位置）；取不到时用自己知道的
  const refresh = async (which, value) => {
    try {
      const fresh = await api("/api/app");
      info.serversRoot = fresh.serversRoot; info.backupsRoot = fresh.backupsRoot;
      if (fresh.config) Object.assign(cfg, fresh.config);
    } catch { cfg[KEY[which]] = value; if (value) info[ROOT[which]] = value; }
  };
  /* 改一个存放文件夹：即时生效（新建的服务器、新做的备份马上用它），提示里带「撤销」（提示至少留 20 秒）。
     撤销换回的是改之前配置里的值：原来用的默认位置就换回默认（空串），不是把默认位置的路径写进配置 */
  const change = async (which, value, okMsg) => {
    const key = KEY[which], prev = cfg[key] || "";
    await api("/api/config", { method: "PUT", body: { [key]: value } });
    await refresh(which, value);
    draw();
    let undoing = false;
    const t = toast(okMsg, false, undefined, { actions: [{ label: "撤销", aria: `撤销：换回原来的${NAME[which]}`, onClick: async () => {
      if (undoing) return;
      undoing = true;
      try {
        await api("/api/config", { method: "PUT", body: { [key]: prev } });
        await refresh(which, prev);
        draw();
        const x = t.querySelector(".t-x");
        if (x) x.click();
        toast(`已换回原来的${NAME[which]}`);
      } catch (e) { undoing = false; toastErr(`没能换回原来的${NAME[which]}`, e); }
    } }] });
  };
  const draw = () => {
    if (stale(gen)) return;
    const same = sameDir(info.serversRoot, info.backupsRoot);
    body.innerHTML = settingsHead("存储位置", "服务器和备份放在哪个文件夹。改了马上生效，只对之后新建的服务器和新做的备份起作用，已经有的不会搬过去，照常能用。") +
      group(
        storageRow("server", "服务器存放文件夹", info.serversRoot, "新的服务器会建在这个文件夹里，一台一个子文件夹。中文名字不会直接用作文件夹名。", "servers") +
        storageRow("package", "备份存放文件夹", info.backupsRoot, "所有备份都放在这里，每台服务器一个子文件夹：开着、停着时做的，手动的、定时的都是。", "backups") +
        storageRow("folder", "程序数据文件夹", info.base, "程序、下载的 Java 和缓存都在这里。整个文件夹可以一起复制走。", ""),
        { ico: true }) +
      (same ? `<div class="warn-card" id="st-same" role="status"><div class="wc-title">${icon("warn")} 服务器和备份放在了同一个文件夹</div>
        <div class="wc-note">这样也能用，但备份会和服务器文件夹混在一起，不好找；硬盘坏了，服务器和备份会一起没了。建议给备份另选一个文件夹，最好在另一块硬盘上。</div></div>` : "") +
      `<div class="hint">更改存放文件夹只能在这台电脑上操作；用手机远程管理时，需要到主机上更改。</div>` +
      settingsHead("备份保留份数", "") +
      group(riRow({ ico: "clock", title: "每台服务器保留几份备份", attrs: `id="set-backup-keep"`,
        sub: "再做一份新的备份时，超出这个数的最旧那几份会删掉（「还原前自动」留的那份也算在里面）。所有服务器共用这一个数，改了下次备份时生效。",
        field: `<input type="number" id="set-bk-keep" min="1" max="1000" value="${keep}" style="max-width:120px" aria-label="每台服务器保留几份备份"><span class="ri-unit">份</span><button class="btn" id="set-bk-keep-save" aria-label="保存备份保留份数">保存</button>` }), { ico: true });
    body.querySelectorAll("[data-open]").forEach(b => b.onclick = () =>
      api("/api/openpath", { method: "POST", body: { path: b.dataset.open } }).catch(e => toastErr("无法打开文件夹", e)));
    body.querySelectorAll("[data-pick]").forEach(b => b.onclick = async () => {
      const which = b.dataset.pick;
      try {
        const r = await api("/api/pickdir", { method: "POST" });
        if (!r.path) return;
        await change(which, r.path, `已更改${NAME[which]}，马上生效`);
      } catch (e) { toastErr(which === "servers" ? "无法更改服务器存放文件夹" : "无法更改备份存放文件夹", e); }
    });
    body.querySelectorAll("[data-reset]").forEach(b => b.onclick = async () => {
      const which = b.dataset.reset;
      try { await change(which, "", `已恢复默认的${NAME[which]}`); }
      catch (e) { toastErr(which === "servers" ? "无法恢复默认的服务器存放文件夹" : "无法恢复默认的备份存放文件夹", e); }
    });
    bindSaveInput($("#set-bk-keep"), $("#set-bk-keep-save"), async v => {
      const n = Math.max(1, Math.min(1000, Math.round(+v) || 10));
      await api("/api/config", { method: "PUT", body: { backupKeep: n } });
      keep = cfg.backupKeep = n;
      if (!stale(gen)) $("#set-bk-keep").value = n;
      toast(`已设置每台服务器保留最近 ${n} 份备份（下次备份时生效）`);
    }, gen);
  };
  draw();
}

/* — Java 运行时 — */
async function setJava(body, info) {
  const gen = viewGen;
  const head = settingsHead("Java 运行时", "新建服务器时优先用本机已装的 Java；没有合适的，则使用 MineDeck 下载过的或自动新下一份。");
  body.innerHTML = head + `<div class="sub">加载中…</div>`;
  let javas = { portable: [], scanned: [], incomplete: [] };
  try { javas = await api("/api/javas"); } catch {}
  if (stale(gen)) return;
  const isLocal = !info || info.local !== false;
  const portList = javas.portable || [];
  const incList = (javas.incomplete || []).filter(m => !portList.some(j => j.major === m));
  const portRows = [
    ...portList.map(j => riRow({ ico: "package", title: `便携 Java ${j.major}`,
      sub: "MineDeck 单独下载的运行时。卸载只删这一套，不动系统里的 Java；有服务器正在用或还配着它时会拦下。",
      right: isLocal ? `<button type="button" class="btn sm ghost" data-rm-java="${j.major}" aria-label="卸载便携 Java ${j.major}">卸载</button>` : "" })),
    ...incList.map(m => riRow({ ico: "package", title: `便携 Java ${m}（残留）`,
      sub: "上次下载或卸载没走完留下的文件夹，不能直接用于开服，可点右侧清理。",
      right: isLocal ? `<button type="button" class="btn sm ghost" data-rm-java="${m}" data-inc="1" aria-label="清理残留的便携 Java ${m}">清理</button>` : "" })),
  ];
  body.innerHTML = head +
    group(
      (portRows.length ? portRows.join("")
        : riRow({ ico: "package", title: "便携运行时", sub: "还没有 MineDeck 下载的 Java。新建服务器缺少合适的 Java 时会自动下载，不影响系统里已有的 Java。" })) +
      ((javas.scanned || []).length ? javas.scanned.map(j => riRow({ ico: "coffee", titleHTML: `Java ${j.major} <span class="badge">${esc(j.version)}</span>`, key: j.path })).join("")
        : riRow({ ico: "coffee", title: "本机已安装的 Java", sub: "尚未发现，点下方「重新扫描」或手动添加" })),
      { ico: true }) +
    group(riRow({ ico: "plus", title: "手动添加", sub: "粘贴 java.exe 或 JDK 文件夹的路径",
      field: `<input type="text" id="java-path" placeholder="如 C:\\Program Files\\Java\\jdk-21\\bin\\java.exe" aria-label="Java 路径"><button class="btn" id="java-add">添加</button>` }), { ico: true }) +
    `<div class="row" style="margin-top:12px"><button class="btn" id="java-scan">${icon("refresh")} 重新扫描本机 Java</button></div>`;
  body.querySelectorAll("[data-rm-java]").forEach(b => b.onclick = async () => {
    const major = Number(b.dataset.rmJava), isInc = b.dataset.inc === "1";
    const ok = await confirmModal({
      title: isInc ? `清理便携 Java ${major} 残留？` : `卸载便携 Java ${major}？`,
      body: isInc
        ? `<p>将删除没装完或没删干净的便携 Java ${major} 文件夹，系统里安装的 Java 不会受影响。</p>`
        : `<p>只删除 MineDeck 自己下载的 Java ${major}，系统里安装的 Java 不会受影响。如果有服务器正在用或还配着它，会直接拦下并说明是哪一台。</p>`,
      okText: isInc ? "清理" : "卸载", danger: true,
    });
    if (!ok) return;
    b.disabled = true;
    try {
      await api("/api/javas/remove", { method: "POST", body: { major, confirm: true } });
      toast(isInc ? `已清理便携 Java ${major} 残留` : `已卸载便携 Java ${major}`);
      if (!stale(gen)) setJava(body, info);
    } catch (e) { toastErr(isInc ? "无法清理便携 Java 残留" : "无法卸载便携 Java", e); if (!stale(gen) && b.isConnected) b.disabled = false; }
  });
  $("#java-scan").onclick = async () => {
    $("#java-scan").disabled = true; $("#java-scan").textContent = "扫描中…（约数秒）";
    try { const list = await api("/api/javas/scan", { method: "POST" }); toast(`扫描完成，发现 ${list.length} 个 Java`); if (!stale(gen)) setJava(body, info); }
    catch (e) { toastErr("无法扫描本机 Java", e); if (!stale(gen)) { $("#java-scan").disabled = false; $("#java-scan").innerHTML = icon("refresh") + " 重新扫描本机 Java"; } }
  };
  $("#java-add").onclick = async () => {
    const p = $("#java-path").value.trim(); if (!p) return;
    try { const j = await api("/api/javas/add", { method: "POST", body: { path: p } }); toast(`已添加 Java ${j.major}（${j.version}）`); if (!stale(gen)) setJava(body, info); }
    catch (e) { toastErr("无法添加这个 Java", e); }
  };
}

/* — 手机远程管理（原来叫「远程访问」）— */
/* 访问地址排序：家用局域网段优先，让默认那张二维码就是手机最可能连得上的地址
   （172.16/12 多为 WSL / Hyper-V 虚拟网卡；169.254 是没拿到 DHCP 的自动地址，垫底）。服务器的连接地址也按它排（rankIPs） */
const lanIPRank = ip => /^192\.168\./.test(ip) ? 0 : /^10\./.test(ip) ? 1 : /^172\.(1[6-9]|2\d|3[01])\./.test(ip) ? 2 : /^169\.254\./.test(ip) ? 4 : 3;
function setRemote(body, info) {
  const lanPort = info.port || location.port || "27333"; // 实际监听端口（被占用时会随机化）
  const fwCmd = `netsh advfirewall firewall add rule name="MineDeck" dir=in action=allow protocol=TCP localport=${lanPort}`;
  const ips = rankIPs(info.ips);
  // 开关点了即时保存（开启前必须先有访问密码，后端也会拒绝）。监听地址只在程序启动时绑定：
  // 开启要重启程序才生效，和这次启动不一致时常驻提示；关闭即时生效（后端不再放行局域网的请求）
  const on = !!info.config.listenLan;
  const live = info.lanActive ?? on; // 这次启动是否已监听在局域网上
  // 页顶先指路：这里是用手机管 MineDeck 的，不是让朋友进游戏的（以前的名字「允许局域网访问」像是后者，可用性走查 U01、U20）
  body.innerHTML = settingsHead("手机远程管理", "用手机、平板或别的电脑的浏览器打开 MineDeck 的管理页，需要访问密码；开启后重启程序生效。") +
    `<div class="set-note" id="lan-not-game">${icon("info")}<span>让朋友进游戏的地址在服务器页上（每台服务器的页头和卡片上的「连接地址」），不在这里。<a href="#/instances">去服务器页</a></span></div>` +
    group(
      riRow({ ico: "phone", title: "开启手机远程管理", sub: "手机和这台电脑连在同一个 Wi-Fi 或路由器下，就能打开管理页",
        right: `<label class="switch"><input type="checkbox" id="lan-on" ${on ? "checked" : ""} aria-label="开启手机远程管理"><span class="sw"></span></label>` }) +
      riRow({ ico: "lock", title: "访问密码", sub: info.lanSet ? "已设置；要修改就输入新密码后保存" : "开启手机远程管理前必须先设置",
        field: `<input type="password" id="lan-pw" placeholder="${info.lanSet ? "已设置（输入新密码可修改）" : "设置访问密码（必填）"}" aria-label="访问密码"><button class="btn" id="lan-save">保存</button>`,
        after: `<div class="field-err ri-err" id="lan-pw-err"></div>` }),
      { ico: true }) +
    (on && !live ? `<div class="hint" id="lan-restart">${icon("info")} 手机远程管理已开启。重启 MineDeck 后生效。重启之后，这里会显示手机扫码用的地址。</div>` : "") +
    (!(on && live) ? "" : ips.length ? `<div class="qr-card">
      <div class="qr-box" id="qr-box" role="img"></div>
      <div class="qr-info">
        <div class="ri-title">手机扫码访问</div>
        <div class="ri-sub">用手机相机或浏览器扫码，输入访问密码即可管理；手机要和这台电脑在同一个局域网（同一 Wi-Fi 或路由器）。</div>
        <div class="qr-url-row"><div class="qr-url" id="qr-url"></div><button type="button" class="btn sm" id="qr-copy" data-copy-exact="" data-copy-msg="已复制管理页地址" aria-label="复制管理页地址">${icon("copy")} 复制</button></div>
        ${ips.length > 1 ? `<div class="qr-ips">${ips.map((ip, i) => `<button type="button" class="qr-ip" data-i="${i}">${esc(ip)}</button>`).join("")}</div>
        <div class="hint">本机有多个网络地址：扫码打不开就换一个（家用路由器下通常是 192.168.x.x）。</div>` : ""}
      </div>
    </div>` : `<div class="hint">未检测到本机局域网 IP，请确认已连接网络。</div>`) +
    `<div class="copy-block" id="lan-fw"><div class="copy-block-h"><span>还是打不开的话，用管理员身份在 CMD 里运行这条命令，放行防火墙。</span><button type="button" class="btn sm" id="lan-fw-copy" data-copy-exact="${errRawAttr(fwCmd)}" data-copy-msg="已复制防火墙命令" aria-label="复制防火墙命令">${icon("copy")} 复制命令</button></div><pre class="copy-block-pre" tabindex="0">${esc(fwCmd)}</pre></div>`;
  const lanOn = $("#lan-on"), lanPw = $("#lan-pw");
  const pwErr = msg => { lanPw.classList.toggle("invalid", !!msg); $("#lan-pw-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; };
  const effect = turnOn => turnOn ? (live ? "" : "，重启 MineDeck 后生效") : (live ? "，手机和别的设备现在起打不开管理页" : "");
  // 保存要等一个来回：这期间切去了别的分区或页面，就不再把远程访问画回 body（它可能已经不是当前视图的了）；提示照常弹
  const gen = viewGen;
  // 只记这次真发出去的：单独存密码时没带开关，页面上的开关状态照旧（不拿 undefined 当成关了）
  const saved = b => { if ("listenLan" in b) info.config.listenLan = b.listenLan; if (b.lanPassword) info.lanSet = true; if (!stale(gen)) setRemote(body, info); };
  let wantOn = false; // 没设密码时拨了开关：记下想开，填好密码点「保存」时一起开
  lanOn.onchange = async () => {
    const turnOn = lanOn.checked;
    const b = { listenLan: turnOn };
    if (turnOn && !info.lanSet) {
      if (!lanPw.value) { // 就地提示，开关拨回去
        lanOn.checked = false; wantOn = true;
        pwErr("先设置访问密码：填好后点「保存」，手机远程管理会一起开启");
        lanPw.focus();
        return;
      }
      b.lanPassword = lanPw.value; // 密码框里已经填了：和开关一起保存
    } else if (lanPw.value) b.lanPassword = lanPw.value; // 已有密码时框里填了新的：也一起保存，不然重画清空了框，用户以为新密码生效了（独立评审发现）
    lanOn.disabled = true;
    try {
      await api("/api/config", { method: "PUT", body: b });
      saved(b);
      toast((turnOn ? "已开启手机远程管理" : "已关闭手机远程管理") + effect(turnOn) + (b.lanPassword && info.lanSet ? "。新的访问密码已保存" : ""));
    } catch (e) { lanOn.checked = !turnOn; lanOn.disabled = false; toastErr(turnOn ? "无法开启手机远程管理" : "无法关闭手机远程管理", e); }
  };
  // 保存密码：只发密码。只有刚才因为没密码被拨回去的那一下（wantOn）才连开关一起开，即「密码和开关一起保存」。
  // 不拿页面上的开关状态一起发：另一个窗口刚关掉了远程管理，这一页没刷新、开关还显示开着，一改密码就会把它又打开
  // （Codex 复审 v3.2.0 第二轮。后端 listenLan 不给就是不改）
  $("#lan-save").onclick = async () => {
    if (!lanPw.value) { pwErr(info.lanSet ? "输入新密码再保存" : "请先输入访问密码"); lanPw.focus(); return; }
    const b = { lanPassword: lanPw.value };
    if (wantOn) b.listenLan = true;
    const opening = wantOn && !on;
    try {
      await api("/api/config", { method: "PUT", body: b });
      saved(b);
      toast(opening ? "访问密码已保存，已开启手机远程管理" + effect(true) : "访问密码已保存");
    } catch (e) { toastErr("无法保存访问密码", e); }
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
    const copyBtn = $("#qr-copy");
    if (copyBtn) copyBtn.setAttribute("data-copy-exact", url);
    body.querySelectorAll(".qr-ip").forEach(b => {
      const on = +b.dataset.i === i;
      b.classList.toggle("cur", on);
      b.setAttribute("aria-pressed", String(on));
    });
  };
  body.querySelectorAll(".qr-ip").forEach(b => b.onclick = () => show(+b.dataset.i));
  show(0);
}

/* — 通知与集成 —
   从局域网打开时，后端只给 Webhook 地址的开头（协议和主机），后面常带着令牌，换成了 ***（ops/catalog 的 maskWebhook）。
   原样保存表示没改；带着 *** 改了一半的后端不收，所以点进去就全选，输入就是整个换掉 */
function setNotify(body, info) {
  const masked = info.local === false && !!info.config.webhookUrl;
  body.innerHTML = settingsHead("通知与集成", "服务器启停 / 崩溃、玩家进出、备份完成、隧道上线等事件，可以推送到群机器人等外部服务。") +
    group(riRow({ ico: "bell", title: "Webhook 事件推送（选填）",
      sub: "有事件时，会向这个地址 POST 一条 JSON。留空就是关闭。" + (masked ? "从局域网打开时只显示地址的开头，后面的部分藏起来了，要改就把整个地址重新填一遍。" : ""),
      field: `<input type="text" id="wh-url" value="${esc(info.config.webhookUrl || "")}" placeholder="https://example.com/hook（留空关闭）" aria-label="Webhook 地址"><button class="btn" id="wh-save">保存</button>` }), { ico: true });
  if (masked) $("#wh-url").addEventListener("focus", e => e.target.select());
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
const aiModelCache = new Map(); // 服务商 + 协议 + 地址（自定义再加鉴权）→ { models, warning, source }；只存用已保存的 Key 拉到的
/* 和后端 ai.KeyWarning 同一条规则：前缀对不上当前地址就提醒，不拦截。认不出的前缀不提醒。
   只比主机名：Key 绑定的是域名，地址后面多一截路径不算对不上（和后端 sameHost 一致）。 */
function keyWarn(p, url, key) {
  if (!p || !p.keyHints || !key) return "";
  let best = null, bestLen = 0;
  for (const h of p.keyHints) for (const px of h.prefixes || []) {
    if (px && key.startsWith(px) && px.length > bestLen) { best = h; bestLen = px.length; }
  }
  if (!best) return "";
  const hit = (best.urls || []).some(u => sameHost(u, url));
  return hit ? "" : (best.note || "");
}
function sameHost(a, b) {
  try { return new URL(a).hostname.toLowerCase() === new URL(b).hostname.toLowerCase(); } catch { return false; }
}
/* 和后端 Preset.matchURL 一致：协议、主机、路径都一样（末尾斜杠不算）。用来认出预设里另一种协议的地址（MiMo 的 /anthropic）。 */
function sameEndpoint(a, b) {
  try {
    const x = new URL(a.trim()), y = new URL(b.trim());
    const path = u => u.pathname.replace(/\/+$/, "");
    return x.protocol === y.protocol && x.host.toLowerCase() === y.host.toLowerCase() && path(x) === path(y);
  } catch { return false; }
}
const AI_FEATS = [{ id: "assistant", name: "AI 助手" }, { id: "gamechat", name: "游戏内 AI" }, { id: "companion", name: "AI 伙伴" }]; // AI 助手（标题栏那个，原来叫「应用内助手」）：本机和局域网登录的设备共用一份额度；AI 伙伴（第七阶段）：玩家叫着伙伴的名字说话时
/* 「设置 → AI」页顶的总览（可用性走查 R19、U22）：AI 是三样东西，各自在哪儿用，先说清楚，再往下配服务商。
   叫法照 docs/文案重写/04-标签与术语-beta3.md：标题栏的「AI 助手」、服务器「扩展」里的「游戏内 AI」「AI 伙伴」。
   这里没有具体的服务器，只写到哪个标签下找，给一个去服务器列表的链接（详情页的地址是 #/inst/<名字>/ext/gameai 这种） */
const aiOverview = () => settingsHead("AI 能做的三件事", "三样用的都是下面这一个服务商和 Key，每天的额度分开算。") +
  group(
    riRow({ ico: "sparkle", title: "AI 助手", sub: "标题栏右上角的「AI 助手」：问 MineDeck 的事，它先查日志和配置再回答；要改东西时先给你一张确认卡片，你点了才执行。" }) +
    riRow({ ico: "server", title: "游戏内 AI", sub: "每台服务器的「扩展 → 游戏内 AI」：玩家在游戏聊天里叫它，它就在聊天里回话。代理端没有。" }) +
    riRow({ ico: "player", title: "AI 伙伴", sub: "NeoForge、Forge 服务器的「扩展 → AI 伙伴」：游戏里一个像玩家一样陪玩的角色，玩家叫着它的名字跟它说话、让它帮忙。" }),
    { ico: true, attrs: `id="ai-overview"` }) +
  `<div class="hint ai-overview-go">游戏内 AI 和 AI 伙伴在服务器页上打开和设置：<a href="#/instances">去服务器列表</a>，点进一台服务器，打开「扩展」。</div>`;
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
  body.innerHTML = aiOverview() + settingsHead("AI 服务商（可选）", "") + `<div class="sub">加载中…</div>`;
  let v;
  try { v = await api("/api/ai/settings"); }
  catch (e) { if (!stale(gen)) body.innerHTML = aiOverview() + settingsHead("AI 服务商（可选）", "") + errBox("无法读取 AI 设置", e); return; }
  if (stale(gen)) return;
  drawAI(body, v, gen);
}

function drawAI(body, v, gen) {
  const P = Object.fromEntries(v.presets.map(p => [p.id, p]));
  const saved = v.provider;
  const ro = !v.local; // 局域网打开：只能看
  const dis = ro ? " disabled" : "";
  const opt = (val, label, cur) => `<option value="${esc(val)}"${val === cur ? " selected" : ""}>${esc(label)}</option>`;
  body.innerHTML = aiOverview() +
    settingsHead("AI 服务商（可选）", "填好服务商、Key 和模型后才启用。未配置时 MineDeck 不连接任何 AI 服务，也不下载价格数据。") +
    (ro ? `<div class="warn-card"><div class="wc-title">${icon("lock")} AI 设置仅支持在本机修改</div><div class="wc-note">这里只能查看。Key 和接口地址只能在运行 MineDeck 的那台电脑上改。</div></div>` : "") +
    `<div class="ai-status" id="ai-status"></div>` +
    group(
      riRow({ ico: "sparkle", title: "服务商", sub: `<span id="ai-note"></span>`,
        right: `<select id="ai-preset" aria-label="服务商"${dis}>${saved.preset ? "" : opt("", "未选择", "")}${v.presets.map(p => opt(p.id, p.name, saved.preset)).join("")}</select>` }) +
      riRow({ ico: "plug", title: "协议", sub: "自定义地址用哪种接口协议", cls: "ai-proto-row",
        right: `<select id="ai-proto" aria-label="协议"${dis}>${opt("openai", "OpenAI 兼容", saved.protocol)}${opt("anthropic", "Claude（Anthropic）", saved.protocol)}</select>` }) +
      riRow({ ico: "key", title: "鉴权", sub: `<span id="ai-auth-sub"></span>`, cls: "ai-auth-row",
        right: `<select id="ai-auth" aria-label="鉴权"${dis}>${opt("", "默认", saved.auth || "")}${opt("bearer", "只发 Bearer", saved.auth || "")}${opt("api-key", "只发 api-key", saved.auth || "")}${opt("both", "都发", saved.auth || "")}</select>` }) +
      riRow({ ico: "sparkle", title: "思考回传", sub: `<span id="ai-echo-sub">收到过思考内容就原样传回。对方不认这个字段时，自动会去掉再试一次。</span>`, cls: "ai-echo-row",
        right: `<select id="ai-echo" aria-label="思考回传"${dis}>${opt("auto", "自动", saved.echo || "auto")}${opt("on", "总是回传", saved.echo || "auto")}${opt("off", "不回传", saved.echo || "auto")}</select>` }) +
      riRow({ ico: "globe", title: "接口地址", sub: `<span id="ai-url-sub"></span>`,
        field: `<input type="text" id="ai-url" spellcheck="false" autocomplete="off" value="${esc(saved.baseUrl)}" aria-label="接口地址"${dis}><span class="ai-alts" id="ai-alts"></span>` }) +
      riRow({ ico: "key", title: "API Key", sub: `<span id="ai-key-sub"></span>`,
        field: `<input type="password" id="ai-key" autocomplete="off" aria-label="API Key"${dis}><button class="btn ghost" id="ai-key-clear"${dis} hidden>清除</button>` }) +
      riRow({ ico: "package", title: "模型", sub: `<span id="ai-model-sub">可以手填。点右边的箭头：没拉过就去拉，拉过就列出全部，上面可以搜索。</span>`,
        field: `<div class="ai-combo" id="ai-combo"><input type="text" id="ai-model" role="combobox" aria-expanded="false" aria-controls="ai-model-list" aria-autocomplete="list" aria-haspopup="listbox" spellcheck="false" autocomplete="off" value="${esc(saved.model)}" aria-label="模型"${dis}><button type="button" class="ai-combo-btn" id="ai-model-btn" aria-label="展开模型列表" aria-haspopup="listbox"${dis}>${icon("chevronDown")}</button><div class="ai-combo-pop" id="ai-model-pop" hidden><input type="text" id="ai-model-q" placeholder="搜索模型" aria-label="搜索模型" aria-controls="ai-model-list" autocomplete="off"${dis}><div id="ai-model-list" role="listbox" aria-label="模型"></div><div class="ai-combo-msg" id="ai-model-msg"></div></div></div><button class="btn" id="ai-list" aria-label="重新获取模型列表" title="重新获取模型列表"${dis}>${icon("refresh")} 刷新</button>`,
        after: `<div class="field-err ri-err" id="ai-model-err"></div>` }) +
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

  const f = { preset: $("#ai-preset"), proto: $("#ai-proto"), url: $("#ai-url"), key: $("#ai-key"), model: $("#ai-model"), effort: $("#ai-effort"), auth: $("#ai-auth"), echo: $("#ai-echo") };
  const cur = () => P[f.preset.value];
  // 同一个网站：http/https、域名、端口都一样（和后端 ai.SameSite 一致：URL 的 origin 不管大小写和默认端口）
  const siteOf = u => { try { const x = new URL(u.trim()); return /^https?:$/.test(x.protocol) ? x.origin : ""; } catch { return ""; } };
  // 换了服务商，或者地址换到了别的网站：保存后已存的 Key 会清掉（不发给别的网站）；同一网站只改路径或协议，Key 照旧
  const keyGone = () => f.preset.value !== saved.preset || !siteOf(f.url.value) || siteOf(f.url.value) !== siteOf(saved.baseUrl);
  // 当前地址用的协议：自定义看下拉框；预设先看地址是不是某一条声明了另一种协议的可选地址（MiMo 的 /anthropic），和后端 matchURL 一致
  const protoNow = () => {
    const p = cur();
    if (!p) return "";
    if (p.id === "custom") return f.proto.value;
    const alt = (p.altUrls || []).find(a => a.protocol && sameEndpoint(a.url, f.url.value));
    return alt ? alt.protocol : p.protocol;
  };
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
    body.querySelector(".ai-auth-row").hidden = !custom;
    body.querySelector(".ai-echo-row").hidden = !custom;
    body.querySelector(".ai-effort-row").hidden = protoNow() !== "anthropic";
    const echoSub = { auto: "收到过思考内容就原样传回。对方不认这个字段时，去掉再试一次，之后就不再带。", on: "每次都带回思考内容。工具调用那一轮即使没收到，也补一个空的，避免思考模式 400。", off: "不传思考内容。有的思考模型在多轮工具调用时会因此 400。" };
    $("#ai-echo-sub").textContent = echoSub[f.echo.value] || echoSub.auto;
    $("#ai-auth-sub").textContent = protoNow() === "anthropic"
      ? "Key 放在哪个请求头。默认发 x-api-key，中转另带一个 Authorization: Bearer。"
      : "Key 放在哪个请求头。默认发 Authorization: Bearer；有的服务商（如小米 MiMo）还认 api-key。";
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
    const kw = keyWarn(p, f.url.value.trim(), f.key.value.trim());
    if (kw) ks += `<br><span class="warn-text">${icon("warn")} ${esc(kw)}</span>`;
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
    // 自填单价按条增删（按模型名）：不再把这一页上的整张表发回去——两个本机页面同时开着时，另一边刚加的那条这一页没有，
    // 整张发就把它删了（Codex 复审 v3.2.0 第二轮）。回来的是整份设置，列表按它重画，另一边加的也就显示出来了
    $("#ai-prices").querySelectorAll("[data-del-price]").forEach(b => b.onclick = () => {
      const c = (v.customPrices || [])[+b.dataset.delPrice];
      if (c) savePrices({ removePrices: [c.model] }, "已删除这条自填单价");
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
      savePrices({ addPrices: [item] }, "已添加自填单价"); // 同一个模型已经有的，后端换成这一份
    };
  };
  const savePrices = async (change, msg) => {
    try { v = await api("/api/ai/prices", { method: "PUT", body: change }); toast(msg); }
    catch (e) { toastErr("无法保存自填单价", e); return; }
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
    if (p.id === "custom" && saved.preset !== "custom") { f.proto.value = "openai"; f.auth.value = ""; f.echo.value = "auto"; }
    invalidateModels(false);
    paintForm();
  };
  f.url.addEventListener("input", paintForm);
  f.key.addEventListener("input", paintForm);
  f.proto.addEventListener("change", paintForm);
  f.auth.addEventListener("change", paintForm);
  f.echo.addEventListener("change", paintForm);

  const readForm = () => {
    const o = { preset: f.preset.value, proto: protoNow(), url: f.url.value.trim(), key: f.key.value.trim() ? "已填" : "", model: f.model.value.trim(), effort: protoNow() === "anthropic" ? f.effort.value : "" };
    if (cur() && cur().id === "custom") { o.auth = f.auth.value; o.echo = f.echo.value; }
    return o;
  };
  let formBase = JSON.stringify(readForm()); // 测试连接只认已保存的设置
  if (!ro) wireSaveBar(body, gen, readForm,
    async sent => { // sent：点「保存」这一刻的 readForm()，下面的 b 也是这一刻读的（确认框开着的时候表单点不到）
      const p = cur();
      if (!p) throw new Error("先选择服务商");
      const b = { preset: p.id, baseUrl: f.url.value.trim(), model: f.model.value.trim(), effort: protoNow() === "anthropic" ? f.effort.value : "" };
      if (p.id === "custom") { b.protocol = f.proto.value; b.auth = f.auth.value; b.echo = f.echo.value; }
      if (f.key.value.trim()) b.key = f.key.value.trim();
      else if (v.keySet && keyGone()) { // 会清掉已存的 Key：先说清楚，确认了才存，并且明着传空 Key（操作记录里看得出清过）
        const r = await confirmModal({ title: "保存后要重新填 Key", body: p.id !== saved.preset
          ? `换成了「${p.name}」，原来那家的 Key 不会带过去，保存后会清掉。可以先在上面填好新的 Key 再保存。`
          : "接口地址换到了另一个网站，原来的 Key 不会发给它，保存后会清掉。可以先在上面填好这个地址的 Key 再保存。",
          okText: "清掉 Key 并保存", danger: true, icon: "key" });
        if (!r) return false;
        b.key = "";
      }
      // 带上这一页拿到的设置版本：另一个窗口这期间存过 AI 设置的，后端整次不改、回 409。这一页的几项互相牵连
      // （换了服务商或网站，Key 要清掉；地址跟着服务商），只发改过的项会把这一页的地址配上另一边刚存的服务商和 Key，
      // 所以这里不合并，拦下来请人重新读取（Codex 复审 v3.2.0 第二轮）。清除 Key、停用 AI 是明说的动作，不带版本
      if (saved.rev) b.rev = saved.rev;
      try { v = await api("/api/ai/provider", { method: "PUT", body: b }); }
      catch (e) {
        if (e.status !== 409) throw e;
        const again = await confirmModal({ title: "AI 设置在别的窗口改过了", icon: "refresh", okText: "重新读取",
          body: "这一页显示的还是打开时的设置，这次没有保存。重新读取最新的设置以后再改（这一页没保存的改动会丢掉）。" });
        if (again && !stale(gen)) { dirtyCheck = null; navigate(); }
        return false;
      }
      // 请求途中又改的不动、也不算已保存（Codex 复审 beta.3）：Key 框和地址框只在还是发出去的那份时才按存好的改写
      const mid = readForm();
      if (f.key.value.trim() === (b.key || "")) f.key.value = ""; // 存好后清空（只填了空白也一样，什么都不存）
      Object.assign(saved, v.provider);
      if (f.url.value.trim() === b.baseUrl) f.url.value = saved.baseUrl;
      f.preset.querySelector('option[value=""]')?.remove(); // 配过之后不再有「未选择」，停用走下面的「停用 AI」
      // 新的基线：没动过的项按改写以后的表单（和存下的一致）；途中改过的项按发出去的那份；Key 框存好后应是空的
      const done = { ...readForm(), key: "" };
      Object.keys(sent).forEach(k => { if (k !== "key" && mid[k] !== sent[k]) done[k] = sent[k]; });
      formBase = JSON.stringify(done);
      invalidateModels(true);
      delete $("#ai-test-sub").dataset.result;
      repaint();
      window.MDAssist?.refresh(); // 标题栏的助手入口随之更新
      toast(v.enabled ? "AI 设置已保存" : `已保存。还差：${v.missing}`);
      return done;
    });

  $("#ai-key-clear").onclick = async () => {
    const r = await confirmModal({ title: "清除 AI 的 Key？", body: "清除后 AI 功能停用，需要时可以重新填写。", okText: "清除", danger: true, icon: "trash" });
    if (!r) return;
    try { v = await api("/api/ai/provider", { method: "PUT", body: { key: "" } }); invalidateModels(true); repaint(); toast("已清除 Key"); }
    catch (e) { toastErr("无法清除 Key", e); }
  };

  // 模型框：手填 + 自己的箭头。列表按「服务商 + 协议 + 地址」缓存。点箭头时没有缓存就去拉。
  const pop = $("#ai-model-pop"), listEl = $("#ai-model-list"), qEl = $("#ai-model-q"), msgEl = $("#ai-model-msg"), errEl = $("#ai-model-err");
  let modelPack = null, active = -1, modelSeq = 0, loading = false;
  const modelSubText = $("#ai-model-sub").textContent;
  // 缓存和「这次结果还算不算数」都按这个上下文：服务商、协议、地址，自定义再加鉴权方式
  const modelKey = () => [f.preset.value, protoNow(), f.url.value.trim(), cur() && cur().id === "custom" ? f.auth.value : ""].join("\n");
  const setExpanded = on => { f.model.setAttribute("aria-expanded", on ? "true" : "false"); pop.hidden = !on; };
  function closeModelPop() { setExpanded(false); active = -1; f.model.removeAttribute("aria-activedescendant"); }
  function suggestedPack(warning) {
    const p = cur();
    const ids = [];
    if (p && p.model) ids.push(p.model);
    for (const id of (p && p.models) || []) if (!ids.includes(id)) ids.push(id);
    return { models: ids.map(id => ({ id })), warning: warning || "", source: "preset" };
  }
  function showModelErr(e) { errEl.innerHTML = e ? errInline("无法获取模型列表", e, { label: "模型列表" }) : ""; }
  function renderModelList(query) {
    const all = (modelPack && modelPack.models) || [];
    const qv = (query || "").trim().toLowerCase();
    const rows = all.filter(m => !qv || m.id.toLowerCase().includes(qv) || (m.name || "").toLowerCase().includes(qv));
    if (active >= rows.length) active = rows.length - 1;
    listEl.innerHTML = rows.map((m, i) => {
      const on = m.id === f.model.value.trim();
      return `<button type="button" class="ai-opt${i === active ? " active" : ""}" role="option" id="ai-opt-${i}" aria-selected="${on ? "true" : "false"}" data-id="${esc(m.id)}">${icon("check")}<span>${esc(m.id)}${m.name && m.name !== m.id ? " · " + esc(m.name) : ""}</span></button>`;
    }).join("") || `<div class="ai-combo-msg">没有匹配的模型，可以直接在上面填写</div>`;
    listEl.querySelectorAll(".ai-opt").forEach((b, i) => {
      b.onmousedown = e => e.preventDefault();
      b.onclick = () => pickModel(b.dataset.id);
      b.onmouseenter = () => { active = i; paintActive(); };
    });
    msgEl.textContent = modelPack && modelPack.warning ? modelPack.warning : (modelPack && modelPack.source === "live" ? `共 ${all.length} 个` : "");
    paintActive();
  }
  function paintActive() {
    const opts = [...listEl.querySelectorAll(".ai-opt")];
    opts.forEach((b, i) => b.classList.toggle("active", i === active));
    // 当前项挂在有焦点的那个输入框上（弹出后焦点在搜索框），读屏软件才读得到
    const host = document.activeElement === qEl ? qEl : f.model;
    for (const el of [f.model, qEl]) if (el !== host) el.removeAttribute("aria-activedescendant");
    if (active >= 0 && opts[active]) { host.setAttribute("aria-activedescendant", opts[active].id); opts[active].scrollIntoView({ block: "nearest" }); }
    else host.removeAttribute("aria-activedescendant");
  }
  function pickModel(id) { f.model.value = id; f.model.dispatchEvent(new Event("input", { bubbles: true })); closeModelPop(); f.model.focus(); }
  function moveActive(d) {
    const n = listEl.querySelectorAll(".ai-opt").length;
    if (!n) return;
    active = active < 0 ? (d > 0 ? 0 : n - 1) : (active + d + n) % n;
    paintActive();
  }
  // 只取不存：结果是不是最新的一次、上下文变没变，由 openModelPop 判断以后再写缓存
  async function loadModels(force, key) {
    const p = cur();
    if (!p) throw new Error("先选择服务商");
    if (!force && !f.key.value.trim() && aiModelCache.has(key)) return aiModelCache.get(key);
    const b = { preset: p.id, baseUrl: f.url.value.trim() };
    if (p.id === "custom") { b.protocol = f.proto.value; b.auth = f.auth.value; b.echo = f.echo.value; }
    if (f.key.value.trim()) b.key = f.key.value.trim();
    const r = await api("/api/ai/models", { method: "POST", body: b });
    return Array.isArray(r) ? { models: r, warning: "", source: "live" } : { models: r.models || [], warning: r.warning || "", source: r.source || "live" };
  }
  async function openModelPop(force) {
    const p = cur();
    if (!p) { toast("先选择服务商", true); return; }
    setExpanded(true);
    qEl.value = "";
    msgEl.textContent = "正在获取模型列表…";
    listEl.innerHTML = "";
    showModelErr(null);
    const btn = $("#ai-model-btn");
    btn.disabled = true;
    const seq = ++modelSeq; // 箭头和「刷新」连着点、或者拉的时候改了地址、协议、鉴权、Key：只认最后一次
    const ctx = modelKey();
    modelPack = null; loading = true;
    try {
      const pack = await loadModels(force, ctx);
      if (stale(gen) || seq !== modelSeq) return; // 后面又发了一次：这次作废，也不写缓存（不然先回来的新结果会被晚到的旧结果盖掉）
      // 只缓存用已保存的 Key 拉到的列表：框里临时填的 Key 拉到的不进缓存，免得「放弃修改」以后还拿它冒充已存 Key 的列表
      if (!f.key.value.trim()) aiModelCache.set(ctx, pack);
      if (pop.hidden || modelKey() !== ctx) return;
      modelPack = pack;
      showModelErr(null);
      // 拉的时候已经在搜索框里打了字：按打的字筛，第一项高亮；没打就高亮当前填的模型
      const curId = f.model.value.trim(), typed = qEl.value;
      active = typed ? 0 : Math.max(0, (modelPack.models || []).findIndex(m => m.id === curId));
      qEl.focus();
      renderModelList(typed);
      $("#ai-model-sub").textContent = modelPack.warning || (modelPack.models.length ? `已有 ${modelPack.models.length} 个模型，也可以直接填写` : "没有拉到模型，请直接填写模型名");
    } catch (e) {
      if (stale(gen) || seq !== modelSeq || modelKey() !== ctx) return; // 作废了，或者已经换了服务商、地址：这个错误不属于现在这一行
      showModelErr(e);
      if (pop.hidden) return;
      modelPack = suggestedPack("列表没拉到，下面是预设里的建议，也可以直接填写。");
      if (modelPack.models.length) { active = 0; qEl.focus(); renderModelList(qEl.value); }
      else { msgEl.textContent = "列表没拉到，请直接填写模型名"; listEl.innerHTML = ""; }
    } finally {
      // 拉的时候弹层被关掉（点了别处、按了 Esc）也要恢复箭头，不然它一直灰着
      if (!stale(gen) && seq === modelSeq) { btn.disabled = ro; loading = false; }
    }
  }
  // 改了地址、协议、鉴权或 Key：在途的那次作废，弹层收起，箭头恢复可点；Key 变了缓存也不再可信
  const invalidateModels = keyChanged => {
    modelSeq++;
    loading = false;
    if (keyChanged) aiModelCache.clear();
    if (!pop.hidden) closeModelPop();
    modelPack = null; // 旧上下文的列表、报错和说明都不再属于这一行
    showModelErr(null);
    $("#ai-model-sub").textContent = modelSubText;
    $("#ai-model-btn").disabled = ro;
  };
  f.url.addEventListener("input", () => invalidateModels(false));
  f.proto.addEventListener("change", () => invalidateModels(false));
  f.auth.addEventListener("change", () => invalidateModels(false));
  f.key.addEventListener("input", () => invalidateModels(true));
  $("#ai-model-btn").onclick = () => { if (!pop.hidden) closeModelPop(); else openModelPop(false); };
  $("#ai-list").onclick = () => openModelPop(true);
  qEl.addEventListener("input", () => { if (loading) return; active = 0; renderModelList(qEl.value); }); // 拉的时候不拿旧数据搜
  const onComboKey = e => {
    if (pop.hidden) {
      if (e.key === "ArrowDown") { e.preventDefault(); openModelPop(false); }
      return;
    }
    if (e.key === "ArrowDown") { e.preventDefault(); moveActive(1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); moveActive(-1); }
    else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); closeModelPop(); f.model.focus(); }
    else if (e.key === "Enter" && !e.isComposing) {
      const opt = listEl.querySelectorAll(".ai-opt")[active];
      if (opt) { e.preventDefault(); pickModel(opt.dataset.id); }
    }
  };
  f.model.addEventListener("keydown", onComboKey);
  $("#ai-model-btn").addEventListener("keydown", e => { if (e.key === "Escape" && !pop.hidden) { e.preventDefault(); e.stopPropagation(); closeModelPop(); f.model.focus(); } });
  qEl.addEventListener("keydown", onComboKey);
  if (window.__aiModelAway) document.removeEventListener("pointerdown", window.__aiModelAway);
  window.__aiModelAway = e => {
    if (pop.hidden) return;
    if (!e.target.closest || !e.target.closest("#ai-combo")) closeModelPop();
  };
  document.addEventListener("pointerdown", window.__aiModelAway);

  $("#ai-test").onclick = async () => {
    if (JSON.stringify(readForm()) !== formBase) { toast("先保存服务商设置，再测试连接", true); return; }
    const btn = $("#ai-test"), sub = $("#ai-test-sub");
    btn.disabled = true;
    sub.textContent = "测试中…";
    try {
      const r = await api("/api/ai/test", { method: "POST" });
      if (stale(gen)) return;
      sub.dataset.result = "1";
      if (!r.ok) sub.innerHTML = errInline("连接失败", { message: r.error, hint: r.hint }, { label: "AI 测试连接" }) + // 这一页可能同时有 MCP 设置的报错框
        (r.warn ? `<br><span class="warn-text">${icon("warn")} ${esc(r.warn)}</span>` : ""); // Key 前缀和地址对不上：完整的说明放在下面
      else {
        const tools = { yes: "能调用工具", no: `<span class="warn-text">${icon("warn")} 这个模型不支持工具调用，助手用不了</span>`, unknown: `<span class="warn-text">${icon("warn")} 模型这次没有调用工具，可能不支持</span>` }[r.tools] || "";
        const cost = r.cost && r.cost.source === "local" ? "（本机模型不计费）" : r.cost ? `（约 ${fmtMoney(r.cost.amount, r.cost.currency)}）` : "";
        const round = r.rounds === 2 ? " · 含一轮工具调用" : "";
        sub.innerHTML = `${icon("check")} 连接正常 · 首个回应 ${r.latencyMs} ms · ${tools}${round} · 用了 ${fmtTokens((r.usage.input || 0) + (r.usage.output || 0) + (r.usage.cacheRead || 0) + (r.usage.cacheWrite || 0))} token${esc(cost)}` +
          (r.warn ? `<br><span class="warn-text">${icon("warn")} ${esc(r.warn)}</span>` : "");
        v = await api("/api/ai/settings").catch(() => v); // Claude 中转：测出来的功能开关已存进设置
      }
    } catch (e) { sub.innerHTML = errInline("无法测试连接", e, { label: "AI 测试连接" }); }
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
    catch (e) { toastErr("无法更新价格数据", e); v = await api("/api/ai/settings").catch(() => v); }
    if (!stale(gen)) paintPrices();
  };

  if (!ro) $("#ai-off").onclick = async () => {
    const r = await confirmModal({ title: "停用 AI？", body: "会清掉服务商设置和 Key。用量记录、每日额度和自填单价会留下。之后可以重新配置。", okText: "停用", danger: true, icon: "power" });
    if (!r) return;
    try { await api("/api/ai/provider", { method: "PUT", body: { preset: "" } }); aiModelCache.clear(); toast("已停用 AI"); dirtyCheck = null; navigate(); }
    catch (e) { toastErr("无法停用 AI", e); }
  };
  window.MDAssist?.settings($("#ai-assist-slot"), v, gen); // 应用内助手（assistant.js）
  window.MDMcp?.settings($("#ai-mcp-slot"), gen); // MCP 接口（mcp.js）
}

/* — 更新与关于 — */
/* 更新渠道（config.updateChannel）："" 跟随当前版本（默认，正式版跟稳定版、测试版跟最新版）；stable 只收正式版；latest 正式版和测试版都收 */
const UPDATE_CH = {
  auto:   { name: "跟随当前版本", t: "跟随当前版本（默认）", d: "正式版只收正式版，测试版连测试版一起收；以后装回正式版也会自动跟回正式版。" },
  stable: { name: "稳定版",       t: "稳定版（只收正式版）",   d: "只提示正式发布的版本。" },
  latest: { name: "最新版",       t: "最新版（包括测试版）",   d: "正式版和测试版都提示，哪个新提示哪个。测试版可能有还没发现的问题。" },
};
const UPDATE_CH_ORDER = ["auto", "stable", "latest"];
let seenStartupTag = ""; // A78：点过「查看」的启动更新版本不再亮圆点
const syncUpdateDot = info => {
  const show = !!(info && info.startupUpdate && info.startupUpdate.tag && info.startupUpdate.tag !== seenStartupTag);
  const navDot = $("#nav-settings-dot"), aboutDot = $("#set-about-dot");
  if (navDot) navDot.hidden = !show;
  if (aboutDot) aboutDot.hidden = !show;
};
const updateChName = ch => (UPDATE_CH[ch] || UPDATE_CH.stable).name;
const updateChSub = (eff, raw) => "检查更新和启动时的检查都按这里选的来。" +
  (raw ? "" : `还没选过，按正在用的版本定成「${updateChName(eff)}」。`);
/* 检查更新的结果写明按哪个渠道查的、查到的是正式版还是测试版。
   ahead：正在用的比查到的还新，不提示更新（稳定渠道上用着测试版时，说清楚等同号的正式版出来再提示） */
function updateResultText(r) {
  const head = `按「${updateChName(r.channel)}」查：`;
  if (!r.latest || !r.latest.tag) return `${head}还没有正式发布的版本，暂不更新（当前 v${r.current}）`;
  const kind = r.prerelease ? "测试版" : "正式版";
  if (r.ahead && r.channel === "stable" && r.currentPrerelease)
    return `${head}最新的正式版是 ${r.latest.tag}。你用的是测试版 v${r.current}，比它新，不会退回去；正式版 ${String(r.current).split(/[-+]/)[0]} 出来以后会提示更新。`;
  if (r.ahead) return `${head}已是最新。正在用的 v${r.current} 比查到的 ${r.latest.tag}（${kind}）还新。`;
  return `${head}已是最新（v${r.current}，${kind}）`;
}
function renderFoundUpdate(out, { tag, prerelease, channel, current }) {
  out.innerHTML = `按「${esc(updateChName(channel))}」查：发现新版本 <b style="color:var(--accent-text)">${esc(tag)}</b>（${prerelease ? "测试版" : "正式版"}），当前 v${esc(current)} `;
  const btn = document.createElement("button");
  btn.className = "btn sm primary";
  btn.innerHTML = icon("arrowUp") + " 立即更新并重启";
  btn.onclick = async () => {
    btn.disabled = true;
    try { await api("/api/update/apply", { method: "POST" }); document.body.innerHTML = `<div style="padding:80px;text-align:center;color:var(--muted)">正在更新，程序将自动重启…</div>`; }
    catch (e) { toastErr("无法更新 MineDeck", e); btn.disabled = false; }
  };
  out.appendChild(btn);
}
function setAbout(body, info) {
  let effCh = info.updateChannel === "latest" || info.updateChannel === "stable" ? info.updateChannel : (/-/.test(info.version) ? "latest" : "stable");
  let rawCh = info.config.updateChannel === "stable" || info.config.updateChannel === "latest" ? info.config.updateChannel : "";
  const selCh = () => rawCh || "auto";
  const chCheck = on => `<span class="ri-check${on ? " on" : ""}" aria-hidden="true">${icon("check")}</span>`;
  const su = info.startupUpdate && info.startupUpdate.tag && info.startupUpdate.tag !== seenStartupTag ? info.startupUpdate : null;
  body.innerHTML = `<div class="brand-head about-head">${logoImg()}<div><h2>MineDeck</h2><div class="sub">Windows 本地 Minecraft 开服工具 · v${esc(info.version)}</div></div></div>` +
    settingsHead("版本与更新", "") +
    group(
      riRow({ ico: "info", title: "当前版本", sub: "MineDeck · Windows 本地 Minecraft 开服工具", right: `<span class="badge core">v${esc(info.version)}</span>` }) +
      riRow({ ico: "arrowUp", title: "更新仓库", sub: "从该 GitHub 仓库的 Releases 检查新版本（国内加速镜像兜底）",
        field: `<input type="text" id="up-repo" value="${esc(info.config.updateRepo || "")}" placeholder="GitHub 仓库，如 yourname/MineDeck" aria-label="更新仓库">
          <button class="btn" id="up-save">保存</button><button class="btn" id="up-check">${icon("search")} 检查更新</button>` }) +
      riRow({ ico: "refresh", title: "启动时静默检查更新", sub: "程序启动后在后台按下面选的更新渠道检查一次新版本（需已填写更新仓库）",
        right: `<label class="switch"><input type="checkbox" id="up-silent" ${info.config.checkUpdateOnStart ? "checked" : ""} aria-label="启动时静默检查更新"><span class="sw"></span></label>` }),
      { ico: true }) +
    (su ? `<div class="set-note up-startup" id="up-startup" role="status">${icon("info")}<span>启动时查到新版本 <b>${esc(su.tag)}</b></span><button type="button" class="btn sm" id="up-startup-view">查看</button></div>` : "") +
    `<div class="hint" id="up-result" aria-live="polite"></div>` +
    settingsHead("更新渠道", `<span id="up-ch-sub">${esc(updateChSub(effCh, rawCh))}</span>`) +
    group(UPDATE_CH_ORDER.map(v => riRow({ title: UPDATE_CH[v].t, sub: esc(UPDATE_CH[v].d), right: chCheck(selCh() === v), cls: "choice" + (selCh() === v ? " sel" : ""),
      attrs: `role="radio" tabindex="0" aria-checked="${selCh() === v}" data-ch="${v}"` })).join(""), { attrs: `id="up-channel" role="radiogroup" aria-label="更新渠道"` }) +
    settingsHead("关于", "") +
    `<div class="hint" style="line-height:1.9">
      内网穿透基于 <b>OpenFrp OPENAPI</b> 接入（<a href="https://www.openfrp.net" target="_blank">openfrp.net</a>）。<br>
      界面字体：Geist、Geist Mono（© The Geist Project Authors）与 Noto Sans SC（© Adobe），均为 SIL Open Font License 1.1，
      子集随程序内嵌，许可原文见 <a href="fonts/OFL-Geist.txt" target="_blank">OFL-Geist.txt</a>、<a href="fonts/OFL-NotoSansSC.txt" target="_blank">OFL-NotoSansSC.txt</a>。<br>
      本软件以 GNU AGPL-3.0 开源，源码在 <a href="https://github.com/Trasperioooooon/MineDeck" target="_blank">GitHub</a>。<br>
      Minecraft 是 Mojang Studios 的商标，本工具与 Mojang / Microsoft / 网易均无隶属或授权关系。<br>
      NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT.
    </div>` +
    group(riRow({ ico: "power", title: "退出程序", sub: "先保存世界，停掉正在运行的服务器，然后退出",
      right: `<button class="btn danger" id="quit-app">${icon("power")} 停止服务器并退出</button>` }), { ico: true, cls: "danger-zone", attrs: `style="margin-top:28px"` });
  bindSaveInput($("#up-repo"), $("#up-save"), async v => {
    await api("/api/config", { method: "PUT", body: { updateRepo: v.trim() } });
    toast("更新仓库已保存");
  }, viewGen);
  $("#up-silent").onchange = async () => {
    try { await api("/api/config", { method: "PUT", body: { checkUpdateOnStart: $("#up-silent").checked } }); toast($("#up-silent").checked ? "已开启启动时检查更新" : "已关闭启动时检查更新"); }
    catch (e) { toastErr($("#up-silent").checked ? "无法开启启动时检查更新" : "无法关闭启动时检查更新", e); $("#up-silent").checked = !$("#up-silent").checked; }
  };
  const hideStartupBanner = tag => {
    if (tag) seenStartupTag = tag;
    info.startupUpdate = null;
    syncUpdateDot(info);
    const ban = $("#up-startup");
    if (ban) ban.remove();
  };
  if ($("#up-startup-view")) {
    $("#up-startup-view").onclick = () => {
      hideStartupBanner(su.tag);
      renderFoundUpdate($("#up-result"), { tag: su.tag, prerelease: !!su.prerelease, channel: su.channel || effCh, current: info.version });
    };
  }
  // 选了马上保存；上一个渠道查出来的结果（连同「立即更新并重启」）和启动提示不再作数，清掉。
  // chSeq 记改过几次渠道：改之前发出去、改之后才回来的检查结果也不要了
  let chSeq = 0;
  const pickCh = async c => {
    const v = c.dataset.ch, out = $("#up-result"), grp = $("#up-channel"), sub = $("#up-ch-sub");
    const wantRaw = v === "auto" ? "" : v;
    if (selCh() === v) {
      toast(v === "auto" ? `现在就是「跟随当前版本」（当前按「${updateChName(effCh)}」查）` : `现在就是「${updateChName(v)}」`);
      return;
    }
    try {
      await api("/api/config", { method: "PUT", body: { updateChannel: wantRaw } });
      rawCh = wantRaw;
      effCh = rawCh || (/-/.test(info.version) ? "latest" : "stable");
      info.config.updateChannel = rawCh;
      info.updateChannel = effCh;
      chSeq++;
      grp.querySelectorAll(".row-item").forEach(x => {
        const on = x === c;
        x.classList.toggle("sel", on);
        x.setAttribute("aria-checked", String(on));
        x.querySelector(".ri-check").classList.toggle("on", on);
      });
      sub.textContent = updateChSub(effCh, rawCh);
      out.textContent = "";
      hideStartupBanner(su ? su.tag : "");
      const silentOff = !$("#up-silent").checked;
      const msg = v === "auto"
        ? `已改成跟随当前版本（当前按「${updateChName(effCh)}」查）`
        : "已改成" + (v === "latest" ? "最新版：正式版和测试版都会提示" : "稳定版：只提示正式版");
      toast(msg + (silentOff ? "。要程序启动时自己检查，还得打开上面的「启动时静默检查更新」" : ""));
    } catch (e) { toastErr("无法切换更新渠道", e); }
  };
  $("#up-channel").querySelectorAll(".row-item").forEach(c => {
    c.onclick = () => pickCh(c);
    c.onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); pickCh(c); } };
  });
  $("#up-check").onclick = async () => {
    const out = $("#up-result"), seq = chSeq; // 先拿住：查的时候切走了，结果写进已经不在页面上的元素，不报错
    out.textContent = "检查中…";
    try {
      const r = await api("/api/update/check", { method: "POST" });
      if (seq !== chSeq) return; // 查的时候改了渠道：这是按旧渠道查的，不显示
      hideStartupBanner(su ? su.tag : "");
      if (r.hasUpdate) {
        renderFoundUpdate(out, { tag: r.latest.tag, prerelease: r.prerelease, channel: r.channel, current: r.current });
      } else {
        out.textContent = updateResultText(r);
      }
    } catch (e) { if (seq === chSeq) out.innerHTML = errInline("无法检查更新", e); }
  };
  $("#quit-app").onclick = quitApp; // 侧栏底部也有一个（见 quitApp）
}

/* 首次运行引导卡（用户协议与免责声明 + 选下载源 + 存放位置说明；
   同意后写 config.onboarded / tosAccepted，二者齐备才不再弹出） */
function showOnboarding(info) {
  const SRC = { auto: "自动（推荐）：国内镜像优先，失败自动切换官方源", mirror: "仅国内镜像：仅使用 BMCLAPI 与清华镜像", official: "仅官方源：仅使用官方源（海外网络适用）" };
  const cur = (info.config && info.config.source) || "auto";
  // 叠一层（见 modalLayer），不整个换掉 #modal-root
  const root = modalLayer(`<div class="modal onboard" role="dialog" aria-modal="true">
    <div class="brand-head">${logoImg()}<div><h3>欢迎使用 MineDeck</h3><div class="sub">Minecraft 开服工具</div></div></div>
    <div class="m-body">
      <div class="ob-sec" style="margin-top:0"><div class="ob-label">用户协议与免责声明</div>
        <div class="ob-terms">
          MineDeck 是开源、免费的 Minecraft Java 版开服辅助工具。它只从 Mojang、PaperMC 等官方公开渠道下载服务端，并在你的电脑上配好运行环境。<b>不包含、也不分发游戏客户端，不修改游戏本身，也不破解正版验证</b>。
          <ol>
            <li><b>非官方产品</b>：本工具为独立开源项目（由随迹提供维护支持），与 Mojang Studios、Microsoft 及网易（中国版《我的世界》运营方）均无任何隶属、合作或授权关系。NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT. "Minecraft" 及相关商标归 Mojang / Microsoft 所有。</li>
            <li><b>遵守官方条款</b>：使用本工具搭建与运行服务器，须遵守 <b>Minecraft EULA</b> 与 Microsoft 服务协议；如你使用网易代理的中国版《我的世界》账号或内容，请另行遵守网易的用户协议（两套账号体系互不相通，本工具仅面向国际 Java 版服务端）。</li>
            <li><b>正版建议</b>：建议使用正版 Mojang / Microsoft 账号并开启正版验证联机。是否使用离线模式由你自行决定并自行承担相应后果，本工具不提供任何绕过正版验证的功能。</li>
            <li><b>对外服务责任自负</b>：通过内网穿透等方式向他人开放服务器时，你是该服务器的运营者，须自行遵守所在地法律法规（含网络安全与未成年人保护相关规定），并对服务器内容与运营行为负责。</li>
            <li><b>开源免责</b>：本软件按 GNU AGPL-3.0 协议以「现状」提供，不作任何明示或默示担保。因使用或无法使用本软件导致的任何数据丢失、账号问题或其他损失，作者及贡献者不承担责任。</li>
          </ol>
        </div>
        <label class="ob-agree"><input type="checkbox" id="ob-agree"><span>我已阅读并同意上述《用户协议与免责声明》及 <a href="https://aka.ms/MinecraftEULA" target="_blank">Minecraft EULA</a></span></label>
      </div>
      <div class="ob-sec"><div class="ob-label">下载源（之后可在「设置」里随时更改）</div>
        <div id="ob-src">${Object.entries(SRC).map(([v, label]) =>
    `<label class="ob-radio"><input type="radio" name="ob-src" value="${v}" ${v === cur ? "checked" : ""}><span>${esc(label)}</span></label>`).join("")}</div>
      </div>
      <div class="ob-sec"><div class="ob-label">你的服务器将存放在</div>
        <div class="ob-path">${esc(info.serversRoot || "程序文件夹\\servers")}</div>
        <div class="hint" style="margin-top:6px">每台服务器一个子文件夹。想放到 D 盘等大容量分区，可到左下角软件「设置 → 存储位置」更改。</div>
      </div>
    </div>
    <div class="m-actions"><button class="btn primary" id="ob-go" disabled>同意并开始使用</button></div>
  </div>`);
  const agree = root.querySelector("#ob-agree"), go = root.querySelector("#ob-go");
  agree.onchange = () => { go.disabled = !agree.checked; };
  go.onclick = async () => {
    const sel = root.querySelector('input[name="ob-src"]:checked');
    try { await api("/api/config", { method: "PUT", body: { source: sel ? sel.value : cur, onboarded: true, tosAccepted: true } }); }
    catch (e) { toastErr("无法保存首次使用的设置", e); }
    dismiss(root);
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
    syncUpdateDot(info);
    const cores = await api("/api/cores").catch(() => []);
    cores.forEach(c => CORES[c.id] = c);
  } catch (e) {
    // 局域网访问（如手机扫码打开）尚未登录：api() 已弹出密码遮罩，保留它，不整页覆盖成「无法连接」；登录成功后 reload 重跑 boot
    if (e.needLogin) return;
    document.body.innerHTML = `<div style="padding:60px;text-align:center;color:var(--danger-text)">无法连接 MineDeck 后端。请通过程序窗口打开本页面。</div>`;
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
