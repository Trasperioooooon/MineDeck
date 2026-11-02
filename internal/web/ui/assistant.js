/* MineDeck 应用内 AI 助手（第三阶段）：标题栏入口、助手面板（会话列表、对话、确认卡片、发送预览）、
   控制台的报错横幅，以及「设置 → AI」里的助手设置。
   app.js 只在几处调这里：启动后 MDAssist.init()；控制台标签里 banner() / consoleLine()；设置 → AI 里 settings()。
   外部 AI（MCP 接口，mcp.js）提出的操作也在面板顶上用同一种确认卡片显示：mcp.js 有变化时调 ext()。
   用到的 app.js 工具（api、esc、icon、toast、dismiss、riRow……）都在调用时才取，所以本文件先于 app.js 加载。
   AI 的回复用自己写的 Markdown 小渲染器显示：先整体转义 HTML，再认几种常用写法；链接只放行 http(s)，不加载任何外部库。 */
"use strict";
window.MDAssist = (() => {
  const A = {
    st: null,     // GET /api/assistant：enabled / allowed / local / lan / missing / preset / model / quota
    panel: null,  // 打开时的面板元素
    view: "chat", // chat / list
    sid: "",      // 当前会话
    sess: null,   // 当前会话（服务端的 View，随事件更新）
    es: null,     // 当前会话的事件流（面板关上也保持：后台提出的确认卡片照样能提醒）
    attach: false, // 下一条消息附上预览过的排错资料
    retry: "",    // 服务商出错正在重试时的提示
    timer: 0,     // 确认卡片的倒计时
  };
  const enc = encodeURIComponent;
  const sp = id => "/api/assistant/sessions/" + enc(id);
  const LAST = "amh_ai_sid"; // 上次打开的会话（每台设备各自记）
  const store = {
    get: () => { try { return localStorage.getItem(LAST) || ""; } catch { return ""; } },
    set: v => { try { v ? localStorage.setItem(LAST, v) : localStorage.removeItem(LAST); } catch {} },
  };
  const q = s => A.panel && A.panel.querySelector(s);

  /* ---------- 入口 ---------- */

  function init() {
    if (document.getElementById("ai-btn")) return;
    const b = document.createElement("button");
    b.id = "ai-btn"; b.type = "button"; b.className = "tb-ai"; b.hidden = true;
    b.title = "AI 助手"; b.setAttribute("aria-label", "AI 助手"); b.setAttribute("aria-expanded", "false");
    b.innerHTML = `${icon("sparkle")}<span>AI 助手</span><i class="tb-dot" hidden></i>`;
    b.onclick = () => (A.panel ? close() : open());
    document.getElementById("win-ctrls").before(b);
    document.addEventListener("keydown", e => {
      // Esc 关面板；弹窗、菜单开着时先让它们处理（它们的监听在后面登记，这里先判断）
      if (e.key !== "Escape" || !A.panel || $("#modal-root").childElementCount || document.querySelector(".dropmenu:not(.leave)")) return;
      close();
    });
    refresh();
  }

  /* 重新取助手的状态（AI 设置改了、开关变了之后调） */
  async function refresh() {
    await status();
    if (A.panel && (!A.st || !A.st.allowed)) gate();
    else if (A.panel && A.view === "gate") open(""); // 刚配好 AI（或打开了开关）：面板直接可用
    return A.st;
  }

  async function status() {
    try { A.st = await api("/api/assistant"); } catch { A.st = null; }
    paintBtn();
  }

  function paintBtn() {
    const b = document.getElementById("ai-btn");
    if (!b) return;
    // 局域网打开时：主机关了「局域网可用 AI 助手」或者还没配置 AI，就没有入口
    b.hidden = !A.st || (!A.st.local && !A.st.allowed);
    b.classList.toggle("on", !!A.panel);
    b.setAttribute("aria-expanded", A.panel ? "true" : "false");
    const n = extList().length; // 外部 AI 等你确认的操作
    b.querySelector(".tb-dot").hidden = !!A.panel || !((A.sess && A.sess.waiting) || n);
    b.title = n ? `AI 助手（外部 AI 有 ${n} 个操作等你确认）` : "AI 助手";
    b.setAttribute("aria-label", b.title);
  }

  /* ---------- 面板 ---------- */

  function mount() {
    const p = document.createElement("aside");
    p.id = "ai-panel";
    p.className = "ai-panel" + (reducedMotion() ? "" : " enter");
    p.setAttribute("aria-label", "AI 助手");
    p.innerHTML = `<header class="ap-head">
        <button class="btn ghost sm icon-only" id="ap-back" title="会话记录" aria-label="会话记录">${icon("clock")}</button>
        <div class="ap-title"><b id="ap-title">AI 助手</b><span class="ap-sub" id="ap-sub"></span></div>
        <button class="btn ghost sm" id="ap-new" aria-haspopup="menu">${icon("plus")}<span>新会话</span></button>
        <button class="btn ghost sm icon-only" id="ap-close" title="关闭（Esc）" aria-label="关闭 AI 助手">${icon("x")}</button>
      </header>
      <div class="ap-ext" id="ap-ext" hidden></div>
      <div class="ap-body" id="ap-body" role="log"></div>
      <div class="ap-foot" id="ap-foot" hidden></div>`;
    document.body.appendChild(p);
    A.panel = p;
    p.addEventListener("animationend", () => p.classList.remove("enter"), { once: true });
    q("#ap-close").onclick = close;
    q("#ap-back").onclick = () => (A.view === "list" && A.sess ? backToChat() : list());
    q("#ap-new").onclick = e => newMenu(e.currentTarget);
    clearInterval(A.timer);
    A.timer = setInterval(tick, 1000);
    paintExt();
    paintBtn();
  }

  /* ---------- 外部 AI（MCP 接口）提出的操作：面板顶上的确认卡片（只在本机有，数据来自 mcp.js 的事件流） ---------- */

  const extList = () => (window.MDMcp ? MDMcp.pending() : []);

  // 按编号增删卡片，不整块重画：别的卡片上勾了的框、写了一半的拒绝理由都留着
  function paintExt() {
    if (!A.panel) return;
    const box = q("#ap-ext"), list = extList();
    box.hidden = !list.length;
    if (!list.length) { box.innerHTML = ""; return; }
    if (!box.querySelector(".ax-head")) box.innerHTML = `<div class="ax-head">${icon("plug")}<b>外部 AI 的请求</b><span class="tag" id="ax-n"></span>
      <span class="grow"></span><span class="ax-note">这台电脑上经 MCP 连进来的 AI 工具</span></div><div class="ax-list" id="ax-list"></div>`;
    q("#ax-n").textContent = String(list.length);
    const ids = new Set(list.map(p => p.id));
    box.querySelectorAll(".ax-item").forEach(el => { if (!ids.has(el.dataset.ext)) el.remove(); });
    const wrap = q("#ax-list");
    for (const p of list) {
      if (wrap.querySelector(`.ax-item[data-ext="${CSS.escape(p.id)}"]`)) continue;
      const t = document.createElement("div");
      t.className = "ax-item" + (reducedMotion() ? "" : " item-enter");
      t.dataset.ext = p.id;
      t.innerHTML = `<div class="ax-from">${icon("sparkle")}<span>${esc(p.actor)} · ${esc(when(p.created))}</span></div>` + cardHTML({ status: "pending", proposal: p });
      wrap.appendChild(t);
      wire(t);
    }
  }

  function ext() { paintExt(); paintBtn(); }

  async function open(id) {
    if (!A.panel) mount();
    if (!A.st || id === undefined) await status(); // 从入口按钮打开：先取最新的状态
    if (!A.panel) return; // 等待期间被关掉了
    if (!A.st || !A.st.allowed) { gate(); return; }
    const want = id || A.sid || store.get();
    if (want) show(want); else list();
  }

  function close() {
    if (!A.panel) return;
    const p = A.panel;
    A.panel = null;
    clearInterval(A.timer);
    dismiss(p, 160);
    paintBtn();
    const b = document.getElementById("ai-btn");
    if (b && !b.hidden) b.focus();
  }

  function head(title, sub) {
    q("#ap-title").textContent = title;
    q("#ap-sub").textContent = sub || "";
    q("#ap-back").title = A.view === "list" && A.sess ? "回到会话" : "会话记录";
    q("#ap-back").setAttribute("aria-label", q("#ap-back").title);
    q("#ap-back").innerHTML = icon(A.view === "list" && A.sess ? "arrowLeft" : "clock");
  }

  /* 不能用助手：没配置 AI、局域网被关掉，或者连不上 */
  function gate() {
    if (!A.panel) return;
    const st = A.st;
    A.view = "gate";
    head("AI 助手");
    q("#ap-foot").hidden = true;
    q("#ap-new").hidden = true;
    q("#ap-back").hidden = true;
    let title, text, action = "";
    if (!st) { title = "连不上 MineDeck"; text = "刷新页面再试一次。"; }
    else if (!st.enabled && st.local) {
      title = "AI 还没配置";
      text = "AI 是可选功能：在「设置 → AI」里选好服务商、填好 Key 和模型之后就能用。不配置也不影响其他功能，控制台的报错横幅照样会显示本地规则的分析。";
      action = `<button class="btn primary" data-x="setup">${icon("gear")} 去设置</button>`;
    } else if (!st.enabled) { title = "主机还没配置 AI"; text = "要在运行 MineDeck 的那台电脑上，到「设置 → AI」里配置好才能用。"; }
    else { title = "主机关闭了「局域网可用 AI 助手」"; text = "局域网登录的设备现在不能用助手，可以在主机的「设置 → AI」里打开。"; }
    q("#ap-body").innerHTML = `<div class="ap-empty ap-gate"><span class="ae-ico">${icon(st && st.enabled ? "lock" : "sparkle")}</span><h4>${esc(title)}</h4><p>${esc(text)}</p>${action}</div>`;
    const go = q('[data-x="setup"]');
    if (go) go.onclick = () => { close(); location.hash = "#/settings/ai"; };
  }

  /* ---------- 会话列表 ---------- */

  async function list() {
    if (!A.panel) return;
    A.view = "list";
    q("#ap-new").hidden = false; q("#ap-back").hidden = false;
    head("会话记录", "本机最多保存最近 20 个会话");
    q("#ap-foot").hidden = true;
    const body = q("#ap-body");
    body.innerHTML = `<div class="ap-loading">${icon("spinner")} 加载中…</div>`;
    let l;
    try { l = await api("/api/assistant/sessions"); }
    catch (e) { if (A.view === "list" && A.panel) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (A.view !== "list" || !A.panel) return;
    if (!l.length) {
      body.innerHTML = `<div class="ap-empty"><span class="ae-ico">${icon("sparkle")}</span><h4>还没有会话</h4>
        <p>问问 AI：服务器为什么起不来、怎么让朋友连进来、某个设置是什么意思。要改东西时，它会先给你一张确认卡片。</p>
        <button class="btn primary" data-x="new">${icon("plus")} 开始新会话</button></div>`;
      body.querySelector('[data-x="new"]').onclick = e => newMenu(e.currentTarget);
      return;
    }
    body.innerHTML = `<div class="ap-list">${l.map(s => `<div class="ap-row${s.id === A.sid ? " cur" : ""}">
        <button class="ar-open" data-sid="${esc(s.id)}"><span class="ar-title">${esc(s.title)}</span>
          <span class="ar-meta">${[s.inst ? esc(s.inst) : "", esc(when(s.updated)), s.origin === "lan" ? "局域网 · " + esc(s.actor) : ""].filter(Boolean).join(SEP)}
            ${s.waiting ? `<span class="tag">待确认</span>` : s.busy ? `<span class="tag neutral">回复中</span>` : ""}</span></button>
        <button class="btn ghost sm icon-only" data-del="${esc(s.id)}" data-title="${esc(s.title)}" title="删除" aria-label="删除会话「${esc(s.title)}」">${icon("trash")}</button>
      </div>`).join("")}</div>
      <div class="ap-list-foot"><span class="hint">删掉的会话找不回来</span><span class="grow"></span><button class="btn sm danger" data-x="clear">${icon("trash")} 清空全部</button></div>`;
    body.querySelectorAll("[data-sid]").forEach(b => b.onclick = () => show(b.dataset.sid));
    body.querySelectorAll("[data-del]").forEach(b => b.onclick = async () => {
      const ok = await confirmModal({ title: `删除会话「${b.dataset.title}」？`, body: "删掉之后找不回来；还没处理的确认卡片一起作废。", okText: "删除", danger: true, icon: "trash" });
      if (!ok) return;
      try { await api(sp(b.dataset.del), { method: "DELETE" }); } catch (e) { toast(e.message, true); return; }
      if (b.dataset.del === A.sid) forget();
      list();
    });
    body.querySelector('[data-x="clear"]').onclick = clearAll;
  }

  async function clearAll() {
    const local = A.st && A.st.local;
    const ok = await confirmModal({ title: "清空全部会话？", danger: true, icon: "trash", okText: "清空",
      body: local ? "会删掉这台电脑上保存的全部助手会话（包括局域网设备开的），删掉之后找不回来。" : "会删掉局域网设备开的全部会话，删掉之后找不回来。" });
    if (!ok) return false;
    try {
      const r = await api("/api/assistant/sessions", { method: "DELETE" });
      forget();
      toast(r.deleted ? `已清空 ${r.deleted} 个会话` : "没有要清空的会话");
    } catch (e) { toast(e.message, true); return false; }
    if (A.panel && A.view !== "gate") list();
    return true;
  }

  function forget() {
    if (A.es) { A.es.close(); A.es = null; }
    A.sid = ""; A.sess = null; A.attach = false;
    store.set("");
    paintBtn();
  }

  function backToChat() { A.view = "chat"; render(); }

  /* 新会话：先选关联哪台服务器（AI 默认查它），当前打开的那台排在最前 */
  async function newMenu(anchor) {
    let insts = [];
    try { insts = await api("/api/instances"); } catch {}
    const m = (location.hash || "").match(/^#\/inst\/([^/]+)/);
    const cur = m ? decodeURIComponent(m[1]) : "";
    const names = insts.map(i => i.name).sort((a, b) => (b === cur) - (a === cur));
    dropMenu(anchor, [
      { header: "新会话关联哪台服务器" },
      ...names.map(n => ({ label: n + (n === cur ? "（当前）" : ""), icon: "server", onClick: () => create(n) })),
      ...(names.length ? [{ sep: true }] : []),
      { label: "不关联服务器", icon: "sparkle", onClick: () => create("") },
    ]);
  }

  async function create(inst) {
    try {
      const v = await api("/api/assistant/sessions", { method: "POST", body: { inst } });
      if (!A.panel) mount();
      show(v.id, v);
    } catch (e) { toast(e.message, true); }
  }

  /* ---------- 对话 ---------- */

  function show(id, first) {
    if (!A.panel) return;
    A.view = "chat";
    q("#ap-new").hidden = false; q("#ap-back").hidden = false;
    if (id !== A.sid || !A.es) {
      if (A.es) { A.es.close(); A.es = null; }
      if (id !== A.sid) { const ta = q("#ap-in"); if (ta) { ta.value = ""; autosize(ta); } }
      A.sid = id; A.sess = first || null; A.attach = false; A.retry = "";
      store.set(id);
      connect(id);
    }
    render();
  }

  /* 订阅会话的事件：先来一份完整的会话，再来增量。断线时浏览器自己重连，重连后再给一份完整的 */
  function connect(id) {
    const es = new EventSource(sp(id) + "/events?token=" + enc(TOKEN()));
    A.es = es;
    es.onmessage = e => {
      if (A.es !== es) return;
      let ev;
      try { ev = JSON.parse(e.data); } catch { return; }
      onEvent(ev);
    };
    es.onerror = () => {
      if (A.es !== es || es.readyState !== EventSource.CLOSED) return;
      // 连不上了（会话被删掉、被挤出最近 20 个，或者没有权限）：回到会话列表
      forget();
      if (A.panel && A.view === "chat") { toast("这个会话已经不在了", true); list(); }
    };
  }

  function onEvent(ev) {
    if (ev.type === "session") {
      A.sess = ev.session;
      A.sess.items = A.sess.items || [];
      if (A.panel && A.view === "chat") render();
      paintBtn();
      return;
    }
    const s = A.sess;
    if (!s) return;
    const find = id => s.items.find(x => x.id === id);
    switch (ev.type) {
      case "item": {
        const k = s.items.findIndex(x => x.id === ev.item.id);
        if (k >= 0) s.items[k] = ev.item; else s.items.push(ev.item);
        pend.delete(ev.item.id);
        A.retry = "";
        paintItem(ev.item, k < 0);
        break;
      }
      case "delta": {
        const it = find(ev.id);
        if (!it) return;
        it.text = (it.text || "") + ev.text;
        it._tool = "";
        A.retry = "";
        later(it);
        break;
      }
      case "tool_start": { const it = find(ev.id); if (it) { it._tool = ev.name; later(it); } break; }
      case "thinking": { const it = find(ev.id); if (it && !it._thinking) { it._thinking = true; later(it); } break; }
      case "remove": {
        s.items = s.items.filter(x => x.id !== ev.id);
        const el = itemEl(ev.id);
        if (el) el.remove();
        break;
      }
      case "retry": A.retry = `服务商出错，正在重试（第 ${ev.attempt} 次）：${ev.err || ""}`; paintFoot(); break;
      case "state":
        for (const k of ["busy", "waiting", "needReply", "stale", "long", "attach", "title", "quota"]) if (k in ev) s[k] = ev[k];
        if (!s.busy) A.retry = "";
        if (!s.attach) A.attach = false;
        if (A.panel && A.view === "chat") {
          paintHead();
          paintFoot();
          // 回复结束：去掉「正在回复」的提示
          const last = s.items[s.items.length - 1];
          if (!s.busy && last && last.kind === "assistant") paintItem(last, false);
        }
        paintBtn();
        break;
    }
  }

  /* 流式文字：一帧最多重画一次（按编号取最新的那一条：中间可能已经被完整的一条替换了） */
  const pend = new Set();
  let raf = 0;
  function later(it) {
    pend.add(it.id);
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      const ids = [...pend];
      pend.clear();
      for (const id of ids) { const cur = A.sess && A.sess.items.find(x => x.id === id); if (cur) paintItem(cur, false); }
    });
  }

  function render() {
    if (!A.panel || A.view !== "chat") return;
    const body = q("#ap-body");
    if (!A.sess) {
      head("AI 助手");
      body.innerHTML = `<div class="ap-loading">${icon("spinner")} 正在打开会话…</div>`;
      q("#ap-foot").hidden = true;
      return;
    }
    paintHead();
    body.innerHTML = A.sess.items.length ? A.sess.items.map(itemHTML).join("") : startHTML();
    wire(body);
    paintFoot();
    body.scrollTop = body.scrollHeight;
  }

  function paintHead() {
    const s = A.sess;
    if (!A.panel || !s) return;
    head(s.title || "新会话", [s.inst ? "关联：" + s.inst : "不关联服务器", s.origin === "lan" ? "局域网 · " + s.actor : "", s.model].filter(Boolean).join(" · "));
  }

  const itemEl = id => A.panel && A.panel.querySelector(`[data-item="${CSS.escape(id)}"]`);

  function paintItem(it, fresh) {
    if (!A.panel || A.view !== "chat" || !A.sess) return;
    const body = q("#ap-body");
    const stick = body.scrollHeight - body.scrollTop - body.clientHeight < 48;
    const old = itemEl(it.id);
    const t = document.createElement("div");
    t.innerHTML = itemHTML(it);
    const n = t.firstElementChild;
    if (old) {
      // 保留展开的详情和没写完的拒绝理由
      const open = [...old.querySelectorAll("details[open]")].map(d => d.dataset.k);
      open.forEach(k => { const d = n.querySelector(`details[data-k="${CSS.escape(k)}"]`); if (d) d.open = true; });
      old.querySelectorAll(".pc-reason:not([hidden]) input").forEach(inp => {
        const card = inp.closest("[data-pid]");
        const nc = card && n.querySelector(`[data-pid="${CSS.escape(card.dataset.pid)}"] .pc-reason`);
        if (nc) { nc.hidden = false; nc.querySelector("input").value = inp.value; }
      });
      old.replaceWith(n);
    } else {
      const start = body.querySelector(".ap-start");
      if (start) start.remove();
      body.appendChild(n);
      if (fresh && !reducedMotion()) n.classList.add("item-enter");
    }
    wire(n);
    if (stick) body.scrollTop = body.scrollHeight;
  }

  function startHTML() {
    const s = A.sess;
    const ideas = s.inst ? ["服务器为什么起不来？", "帮我看看最近的报错", "朋友连不进来怎么办？", "内存设多少合适？"]
      : ["怎么开一个装模组的服务器？", "Paper 和 Fabric 有什么区别？", "怎么让朋友连进我的服务器？"];
    return `<div class="ap-empty ap-start"><span class="ae-ico">${icon("sparkle")}</span>
      <h4>${s.inst ? `有什么关于「${esc(s.inst)}」的问题？` : "有什么想问的？"}</h4>
      <p>AI 会先查日志、配置再回答；要改东西时先给你一张确认卡片，你确认了才执行。</p>
      <div class="ap-ideas">${ideas.map(t => `<button class="btn sm" data-idea="${esc(t)}">${esc(t)}</button>`).join("")}</div></div>`;
  }

  const TOOL_ST = {
    running: ["spinner", "执行中"], done: ["check", "完成"], error: ["warn", "出错"], skipped: ["info", "没有执行"],
    pending: ["pending", "等你确认"], confirmed: ["check", "已确认，已执行"], failed: ["warn", "已确认，但执行失败"],
    rejected: ["x", "已拒绝"], expired: ["clock", "已过期"], invalid: ["warn", "已作废"], canceled: ["x", "没处理就发了新消息，已作废"],
  };

  function itemHTML(it) {
    const id = esc(it.id);
    if (it.kind === "user") {
      const chips = [];
      if (it.attach) chips.push(`<button class="ap-chip" data-sent="${id}">${icon("doc")} 附上了 ${it.attach} 字的排错资料</button>`);
      if (it.masked) chips.push(`<button class="ap-chip" data-sent="${id}">${icon("lock")} 隐私信息已替换</button>`);
      return `<div class="ap-msg user${it.withdrawn ? " withdrawn" : ""}" data-item="${id}">
        <div class="ap-bubble">${esc(it.text)}</div>${chips.length ? `<div class="ap-chips">${chips.join("")}</div>` : ""}
        ${it.withdrawn ? `<div class="ap-withdrawn">没得到回复，这句已从对话里撤回 · <button class="ap-link" data-reuse="${id}">放回输入框</button></div>` : ""}</div>`;
    }
    if (it.kind === "notice") {
      const lv = ["warn", "error"].includes(it.level) ? it.level : "info";
      return `<div class="ap-note ${lv}" data-item="${id}">${icon(lv === "info" ? "info" : "warn")}<span>${esc(it.text)}</span></div>`;
    }
    const tools = (it.tools || []).map(toolHTML).join("");
    const last = A.sess && A.sess.items[A.sess.items.length - 1] === it;
    let typing = "";
    if (it._tool && A.sess.busy) typing = `正在调用 ${esc(toolTitle(it._tool))}…`;
    else if (last && A.sess.busy && !it.text && !tools) typing = it._thinking ? "思考中…" : "正在回复…";
    return `<div class="ap-msg assistant${it.partial ? " partial" : ""}" data-item="${id}">
      ${it.text ? `<div class="md">${md(it.text)}</div>` : ""}
      ${tools ? `<div class="ap-tools">${tools}</div>` : ""}
      ${typing ? `<div class="ap-typing">${icon("spinner")}<span>${typing}</span></div>` : ""}
      ${it.partial ? `<div class="ap-note">${icon("info")}<span>没说完就停了</span></div>` : ""}
      ${usageHTML(it)}</div>`;
  }

  // 工具名（instance_props_set）换成操作的标题：同一会话里已经出现过的直接用
  function toolTitle(name) {
    for (const it of (A.sess && A.sess.items) || []) for (const t of it.tools || []) if (t.name === name && t.title) return t.title;
    return name;
  }

  function toolHTML(t) {
    if (t.proposal) return cardHTML(t);
    const [ic, label] = TOOL_ST[t.status] || ["info", t.status];
    return `<details class="ap-tool ${esc(t.status)}" data-k="t-${esc(t.id)}">
      <summary>${icon(ic)}<span class="at-name">${esc(t.title || t.name)}</span><span class="at-st">${esc(label)}</span></summary>
      <div class="at-body">${argsHTML(t.args)}${t.error ? `<div class="at-err">${esc(t.error)}</div>` : ""}
        ${t.status === "done" ? `<button class="btn sm ghost" data-sent-tool="${esc(t.id)}">${icon("doc")} 看看交还给 AI 的内容</button>` : ""}</div></details>`;
  }

  function argsHTML(args) {
    const keys = args && typeof args === "object" ? Object.keys(args) : [];
    if (!keys.length) return "";
    const val = v => { const s = typeof v === "string" ? v : JSON.stringify(v, null, 1); return s.length > 600 ? s.slice(0, 600) + "…（共 " + s.length + " 字）" : s; };
    return `<pre class="ap-kv">${keys.map(k => `<b>${esc(k)}</b>: ${esc(val(args[k]))}`).join("\n")}</pre>`;
  }

  /* 确认卡片：做什么、改哪些值；危险操作多一步（勾选后才能点实底红的按钮）；EULA 这类同意只能由人勾 */
  function cardHTML(t) {
    const p = t.proposal, plan = p.plan || {};
    const danger = p.level === "D";
    const pending = t.status === "pending" && !p.outcome;
    const changes = (plan.changes || []).map(c => `<dt>${esc(c.what)}</dt><dd>${c.before ? `<del>${esc(c.before)}</del> → ` : ""}<ins>${esc(c.after)}</ins></dd>`).join("");
    let foot;
    if (pending) {
      const checks = [];
      if (p.consent) checks.push(`<label class="pc-check"><input type="checkbox" data-k="consent"><span>${esc(p.consent.text)}${/^https?:\/\//.test(p.consent.url || "") ? `（<a href="${esc(p.consent.url)}" target="_blank" rel="noopener noreferrer">条款原文</a>）` : ""}</span></label>`);
      if (danger) checks.push(`<label class="pc-check"><input type="checkbox" data-k="ack"><span>我看过上面的改动，确认执行这个危险操作</span></label>`);
      foot = `${checks.join("")}
        <div class="pc-reason" hidden><input type="text" maxlength="300" placeholder="告诉 AI 为什么不行（可以不填）" aria-label="拒绝的理由"><button class="btn sm" data-x="no">拒绝</button></div>
        <div class="pc-actions"><button class="btn sm ghost" data-x="reject">拒绝…</button><button class="btn sm ${danger ? "danger solid" : "primary"}" data-x="ok"${checks.length ? " disabled" : ""}>${danger ? "确认执行" : "确认"}</button></div>`;
    } else {
      const st = p.outcome === "done" ? "confirmed" : p.outcome || t.status;
      const [ic, label] = TOOL_ST[st] || ["info", st];
      const extra = [p.by ? `处理人：${p.by}` : "", p.reason ? `理由：${p.reason}` : "", p.error || ""].filter(Boolean).join("；");
      foot = `<div class="pc-out ${esc(st)}">${icon(ic)}<span>${esc(label)}${extra ? `（${esc(extra)}）` : ""}</span></div>`;
    }
    return `<div class="ap-card${danger ? " danger" : ""}${pending ? " pending" : ""}" data-pid="${esc(p.id)}">
      <div class="pc-head"><span class="pc-ico">${icon(danger ? "warn" : "bolt")}</span><b>${esc(p.title)}</b>
        <span class="tag${danger ? " danger" : ""}">${danger ? "危险操作" : "写操作"}</span><span class="grow"></span>
        ${pending ? `<span class="pc-exp" data-exp="${esc(p.expires)}"></span>` : ""}</div>
      ${plan.summary ? `<div class="pc-sum">${esc(plan.summary)}</div>` : ""}
      ${changes ? `<dl class="pc-changes">${changes}</dl>` : ""}
      ${plan.warning ? `<div class="pc-warn">${icon("warn")}<span>${esc(plan.warning)}</span></div>` : ""}
      ${plan.diff ? `<pre class="pc-diff">${plan.diff.split("\n").map(l => `<span class="${l.startsWith("+ ") ? "add" : l.startsWith("- ") ? "del" : l.startsWith("@@") ? "hunk" : ""}">${esc(l)}</span>`).join("\n")}</pre>` : ""}
      <details class="pc-args" data-k="a-${esc(p.id)}"><summary>具体参数</summary>${argsHTML(p.args) || `<div class="hint">没有参数</div>`}</details>
      ${foot}</div>`;
  }

  function usageHTML(it) {
    const u = it.usage;
    if (!u) return "";
    const input = (u.input || 0) + (u.cacheRead || 0) + (u.cacheWrite || 0);
    let s = `输入 ${fmtTokens(input)}${u.cacheRead ? `（缓存命中 ${fmtTokens(u.cacheRead)}）` : ""} · 输出 ${fmtTokens(u.output)} token`;
    const c = it.cost;
    if (c && c.source === "local") s += " · 本机模型不计费";
    else if (c && c.amount > 0) s += ` · 约 ${fmtMoney(c.amount, c.currency)}`;
    if (u.estimated) s += "（按字数估算）";
    return `<div class="ap-usage">${esc(s)}</div>`;
  }

  /* 给一条消息（或整个对话区）挂上事件 */
  function wire(root) {
    root.querySelectorAll("[data-sent]").forEach(b => b.onclick = () => sentModal(b.dataset.sent, ""));
    root.querySelectorAll("[data-sent-tool]").forEach(b => b.onclick = () => sentModal(b.closest("[data-item]").dataset.item, b.dataset.sentTool));
    root.querySelectorAll("[data-reuse]").forEach(b => b.onclick = () => {
      const it = A.sess.items.find(x => x.id === b.dataset.reuse);
      const ta = q("#ap-in");
      if (it && ta) { ta.value = it.text; autosize(ta); ta.focus(); }
    });
    root.querySelectorAll("[data-idea]").forEach(b => b.onclick = () => { const ta = q("#ap-in"); if (ta) { ta.value = b.dataset.idea; send(); } });
    root.querySelectorAll(".ap-card.pending").forEach(card => {
      const ok = card.querySelector('[data-x="ok"]');
      const boxes = [...card.querySelectorAll(".pc-check input")];
      const paint = () => { ok.disabled = boxes.some(c => !c.checked); };
      boxes.forEach(c => c.onchange = paint);
      ok.onclick = () => decide(card, true);
      const reason = card.querySelector(".pc-reason");
      card.querySelector('[data-x="reject"]').onclick = () => { reason.hidden = !reason.hidden; if (!reason.hidden) reason.querySelector("input").focus(); };
      card.querySelector('[data-x="no"]').onclick = () => decide(card, false);
      reason.querySelector("input").onkeydown = e => {
        if (e.key === "Enter" && !e.isComposing) { e.preventDefault(); decide(card, false); }
        if (e.key === "Escape") { e.stopPropagation(); reason.hidden = true; }
      };
    });
    tick();
  }

  async function decide(card, ok) {
    const pid = card.dataset.pid;
    const body = ok
      ? { ack: !!(card.querySelector('[data-k="ack"]') || {}).checked, consent: !!(card.querySelector('[data-k="consent"]') || {}).checked }
      : { reason: card.querySelector(".pc-reason input").value.trim() };
    const btns = [...card.querySelectorAll("button")];
    btns.forEach(b => b.disabled = true);
    const okBtn = card.querySelector('[data-x="ok"]');
    if (ok) okBtn.textContent = "执行中…";
    const ext = !!card.closest("#ap-ext");
    try {
      await api(`/api/ops/proposals/${enc(pid)}/${ok ? "confirm" : "reject"}`, { method: "POST", body });
      // 外部 AI 的：卡片拿掉，告诉一声（外部 AI 用 proposal_status 查得到结果）
      if (ext) { toast(ok ? "已确认，已经执行；外部 AI 查得到结果" : "已拒绝，外部 AI 查得到你的理由"); window.MDMcp?.drop(pid); }
    } catch (e) {
      toast(e.message, true);
      if (card.isConnected) { btns.forEach(b => b.disabled = false); okBtn.textContent = card.classList.contains("danger") ? "确认执行" : "确认"; okBtn.disabled = [...card.querySelectorAll(".pc-check input")].some(c => !c.checked); }
    }
    // 助手的：结果经事件流回来（卡片变成「已确认」「已拒绝」，AI 接着回复）
  }

  /* 确认卡片的有效期（5 分钟）。只用来提示：过没过期以服务端为准（本机时钟可能不准） */
  function tick() {
    if (!A.panel) return;
    A.panel.querySelectorAll("[data-exp]").forEach(el => {
      const left = Math.round((Date.parse(el.dataset.exp) - Date.now()) / 1000);
      if (!(left > -30 && left < 400)) { el.textContent = "5 分钟内有效"; return; }
      el.textContent = left <= 0 ? "可能已过期" : left >= 60 ? `${Math.ceil(left / 60)} 分钟内有效` : `还剩 ${left} 秒`;
    });
  }

  /* 实际发给 AI 的内容（脱敏、附上排错资料之后的样子），或者工具交还给 AI 的内容 */
  async function sentModal(item, tool) {
    let r;
    try { r = await api(`${sp(A.sid)}/sent?item=${enc(item)}${tool ? "&tool=" + enc(tool) : ""}`); } catch (e) { toast(e.message, true); return; }
    const res = await sheet({ title: tool ? "交还给 AI 的内容" : "实际发给 AI 的内容", cls: "ap-pv",
      body: `<div class="m-body">${tool ? "工具的结果脱敏、截断之后交还给 AI，外面加了「以下是数据」的标记。" : "路径里的 Windows 用户名、局域网 IP、Key 和令牌发送前已经替换掉。"}</div><pre class="ap-pre ap-sent">${esc(r.text)}</pre>`,
      buttons: [{ x: "copy", html: `${icon("copy")} 复制` }, { x: "close", text: "关闭", cls: "primary" }] });
    if (res && res.x === "copy") copy(r.text);
  }

  /* 弹窗（外观同 app.js 的 confirmModal）：点了哪个按钮就返回 {x, mask}；Esc、点外面返回 null */
  function sheet({ title, body, buttons, cls = "" }) {
    return new Promise(resolve => {
      const root = $("#modal-root");
      const prev = document.activeElement;
      root.innerHTML = `<div class="modal-mask enter"><div class="modal ${cls}" role="dialog" aria-modal="true" aria-labelledby="ap-m-title">
        <h3 id="ap-m-title">${esc(title)}</h3>${body}
        <div class="m-actions">${buttons.map(b => `<button class="btn ${b.cls || ""}" data-x="${b.x}">${b.html || esc(b.text)}</button>`).join("")}</div></div></div>`;
      const mask = root.firstElementChild;
      let done = false;
      const end = v => {
        if (done) return;
        done = true;
        document.removeEventListener("keydown", onKey);
        dismiss(mask);
        if (prev && prev.focus) prev.focus();
        resolve(v);
      };
      const onKey = e => { if (e.key === "Escape") end(null); };
      document.addEventListener("keydown", onKey);
      mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget) end(null); });
      mask.querySelectorAll(".m-actions [data-x]").forEach(b => b.onclick = () => end({ x: b.dataset.x, mask }));
      mask.querySelector(".m-actions .btn:last-child").focus();
    });
  }

  /* ---------- 输入框 ---------- */

  function paintFoot() {
    if (!A.panel || A.view !== "chat") return;
    const f = q("#ap-foot"), s = A.sess;
    if (!s) { f.hidden = true; return; }
    f.hidden = false;
    if (!f.querySelector("#ap-in")) {
      f.innerHTML = `<div class="ap-state" id="ap-state"></div>
        <div class="ap-attach" id="ap-attach" hidden>${icon("doc")}<span></span><span class="grow"></span>
          <button class="btn sm ghost" data-x="drop" title="这次不附上">不附上</button></div>
        <div class="ap-compose"><textarea id="ap-in" rows="1" placeholder="问问 AI…（回车发送，Shift + 回车换行）" aria-label="发给 AI 的消息"></textarea>
          <button class="btn primary" id="ap-send">发送</button><button class="btn" id="ap-stop" hidden>${icon("stop")} 停止</button></div>
        <div class="ap-meta" id="ap-meta"></div>`;
      const ta = f.querySelector("#ap-in");
      ta.oninput = () => autosize(ta);
      ta.onkeydown = e => { if (e.key === "Enter" && !e.shiftKey && !e.isComposing && e.keyCode !== 229) { e.preventDefault(); send(); } };
      f.querySelector("#ap-send").onclick = send;
      f.querySelector("#ap-stop").onclick = () => api(sp(A.sid) + "/stop", { method: "POST" }).catch(e => toast(e.message, true));
      f.querySelector('[data-x="drop"]').onclick = () => { A.attach = false; paintFoot(); };
    }
    const qt = s.quota || {}, over = qt.limit > 0 && qt.used >= qt.limit;
    const st = [];
    if (A.retry) st.push(`<span class="warn-text">${icon("refresh")} ${esc(A.retry)}</span>`);
    if (s.stale) st.push(`<span class="warn-text">${icon("warn")} ${esc(s.stale)}</span><button class="btn sm" data-x="fresh">${icon("plus")} 开新会话</button>`);
    else if (over) st.push(`<span class="warn-text">${icon("warn")} 今天的额度用完了，明天 0 点恢复；可以在「设置 → AI」里调高</span>`);
    else if (s.waiting && !s.busy) st.push(`<span>${icon("pending")} 上面有确认卡片等你处理；直接发新消息的话，没处理的卡片会作废</span>`);
    else if (s.needReply && !s.busy) st.push(`<span>上一次的回复没完成</span><button class="btn sm" data-x="cont">${icon("refresh")} 继续回复</button>`);
    if (s.long && !s.stale) st.push(`<span>会话已经很长了，每次发送都更贵，建议开个新会话</span>`);
    const box = f.querySelector("#ap-state");
    box.innerHTML = st.join("");
    const fresh = box.querySelector('[data-x="fresh"]');
    if (fresh) fresh.onclick = e => newMenu(e.currentTarget);
    const cont = box.querySelector('[data-x="cont"]');
    if (cont) cont.onclick = () => api(sp(A.sid) + "/continue", { method: "POST" }).catch(e => toast(e.message, true));
    const at = f.querySelector("#ap-attach");
    at.hidden = !(A.attach && s.attach > 0);
    at.querySelector("span").textContent = `发送时附上排错资料（约 ${s.attach} 字）`;
    const ta = f.querySelector("#ap-in");
    ta.disabled = !!s.stale || over;
    f.querySelector("#ap-send").hidden = !!s.busy;
    f.querySelector("#ap-send").classList.toggle("primary", !s.waiting); // 有确认卡片时，卡片上的「确认」才是主按钮
    f.querySelector("#ap-send").disabled = !!s.stale || over;
    f.querySelector("#ap-stop").hidden = !s.busy;
    const used = qt.limit > 0 ? `今天已用 ${fmtTokens(qt.used)} / ${fmtTokens(qt.limit)} token` : `今天已用 ${fmtTokens(qt.used)} token（不限额度）`;
    f.querySelector("#ap-meta").innerHTML = `<span class="${qt.limit > 0 && qt.used >= qt.limit * 0.8 ? "warn" : ""}">${esc(used)}</span><span class="grow"></span><span>改东西前都会先问你</span>`;
  }

  function autosize(ta) {
    ta.style.height = "auto";
    ta.style.height = Math.min(ta.scrollHeight + 2, 160) + "px";
  }

  async function send() {
    const s = A.sess;
    const ta = q("#ap-in");
    if (!s || !ta || s.busy) return;
    const text = ta.value.trim();
    const attach = A.attach && s.attach > 0;
    if (!text && !attach) return;
    const btn = q("#ap-send");
    btn.disabled = true;
    try {
      await api(sp(s.id) + "/messages", { method: "POST", body: { text, attach } });
      ta.value = "";
      autosize(ta);
      A.attach = false;
    } catch (e) { toast(e.message, true); }
    btn.disabled = false;
    paintFoot();
  }

  /* ---------- 让 AI 分析（报错横幅上的按钮） ---------- */

  async function analyze(name, btn) {
    const st = await refresh();
    if (!st || !st.allowed) { toast(st && !st.enabled ? "AI 还没配置" : "现在不能用 AI 助手", true); return; }
    if (btn) btn.disabled = true;
    try {
      let v;
      try { v = await api("/api/assistant/sessions", { method: "POST", body: { inst: name } }); } catch (e) { toast(e.message, true); return; }
      let pv;
      try { pv = await api(sp(v.id) + "/context", { method: "POST" }); }
      catch (e) { toast(e.message, true); api(sp(v.id), { method: "DELETE" }).catch(() => {}); return; }
      const r = await previewModal(pv, st);
      if (!r) { api(sp(v.id), { method: "DELETE" }).catch(() => {}); return; } // 不发：刚开的空会话删掉
      if (!A.panel) mount();
      show(v.id, v);
      try { await api(sp(v.id) + "/messages", { method: "POST", body: { text: r.text, attach: true } }); }
      catch (e) { toast(e.message, true); A.attach = true; paintFoot(); }
    } finally { if (btn) btn.disabled = false; }
  }

  /* 发送预览：将发送约 N 字，可以展开看全文；提问可以改。确认后才发 */
  async function previewModal(pv, st) {
    const data = pv.text.startsWith(pv.lead) ? pv.text.slice(pv.lead.length).replace(/^\n+/, "") : pv.text;
    const who = [st.preset, st.model].filter(Boolean).join(" · ");
    const r = await sheet({ title: "发给 AI 之前，先看一眼", cls: "ap-pv",
      body: `<div class="m-body">将发送约 <b>${pv.chars.toLocaleString("en-US")}</b> 字（约 ${fmtTokens(pv.tokens)} token）给 ${esc(who || "AI 服务商")}，算在今天的额度里。
          路径里的 Windows 用户名、局域网 IP、Key 和令牌已经替换掉；玩家名保留，排错时常常用得到。</div>
        <label class="ap-pv-q"><span>想问 AI 的话</span><textarea rows="2" maxlength="20000">${esc(pv.lead)}</textarea></label>
        <details class="ap-pv-more"><summary>展开查看要发送的排错资料</summary><pre class="ap-pre">${esc(data)}</pre></details>`,
      buttons: [{ x: "cancel", text: "取消" }, { x: "ok", html: `${icon("sparkle")} 发送`, cls: "primary" }] });
    return r && r.x === "ok" ? { text: r.mask.querySelector("textarea").value.trim() } : null;
  }

  /* ---------- 控制台的报错横幅（不依赖 AI：本地规则的结论，没配置 AI 时只是没有「让 AI 分析」） ---------- */

  const B = { slot: null, name: "", gen: 0, timer: 0, seq: 0 };
  // 这些行出现后重新跑一次本地规则（防抖）；新的一次启动先把横幅收起
  const TRIGGER = /^\[MineDeck\] (启动:|服务器进程已退出|检测到服务器异常退出|服务器已正常关闭)|ERROR|FATAL|SEVERE|Exception|FAILED TO BIND|Address already in use|EULA|OutOfMemoryError|Could not reserve|insufficient memory|UnsupportedClassVersionError|Unrecognized VM option|single server tick|failed to connect|incompatible voice chat/;

  function banner(slot, name, gen) {
    clearTimeout(B.timer);
    Object.assign(B, { slot, name, gen });
    diag();
  }

  function consoleLine(name, line) {
    if (name !== B.name || !B.slot || !B.slot.isConnected) return;
    if (line.startsWith("[MineDeck] 启动:")) B.slot.innerHTML = "";
    if (!TRIGGER.test(line)) return;
    clearTimeout(B.timer);
    B.timer = setTimeout(diag, 900);
  }

  async function diag() {
    const { slot, name, gen } = B;
    if (!slot || !slot.isConnected) return;
    const seq = ++B.seq;
    let rep;
    try { rep = await api(`/api/instances/${enc(name)}/diag`); } catch { return; }
    if (!A.st) await refresh();
    if (stale(gen) || seq !== B.seq || !slot.isConnected) return;
    paintBanner(slot, name, rep);
  }

  const dismissKey = name => "md_diag_x_" + name;
  function paintBanner(slot, name, rep) {
    const fs = rep.findings || [];
    if (!fs.length && !rep.crashed) { slot.innerHTML = ""; return; }
    const sig = fs.map(f => f.rule).join(",") + "|" + (rep.exit || "") + "|" + (rep.lines || 0);
    try { if (sessionStorage.getItem(dismissKey(name)) === sig) { slot.innerHTML = ""; return; } } catch {}
    const top = fs[0] || { title: "服务器异常退出", summary: rep.exit || "没有认出原因，可以看看上面的日志。" };
    const err = rep.crashed || fs.some(f => f.severity === "error");
    const wasOpen = !!slot.querySelector(".db-more[open]");
    const ai = A.st && A.st.allowed;
    slot.innerHTML = `<div class="diag-banner ${err ? "err" : "warn"}" role="status" data-sig="${esc(sig)}">
      <div class="db-head"><span class="db-ico">${icon("warn")}</span>
        <div class="db-text"><b>${esc(top.title)}</b><span>${esc(top.summary)}</span></div>
        ${ai ? `<button class="btn sm" data-x="ai">${icon("sparkle")} 让 AI 分析</button>` : ""}
        <button class="btn sm ghost icon-only" data-x="close" title="不再提示这一次的问题" aria-label="关闭报错提示">${icon("x")}</button></div>
      <details class="db-more"${wasOpen ? " open" : ""}><summary>${fs.length > 1 ? `一共认出 ${fs.length} 个问题 · ` : ""}看详情和怎么修</summary>
        ${fs.map(f => `<div class="db-f"><div class="db-ft"><b>${esc(f.title)}</b>${f.severity === "warn" ? `<span class="tag warn">提醒</span>` : ""}</div>
          <div>${esc(f.summary)}</div>
          ${(f.details || []).length ? `<ul>${f.details.map(d => `<li>${esc(d)}</li>`).join("")}</ul>` : ""}
          ${(f.fixes || []).length ? `<div class="db-fix">怎么修</div><ol>${f.fixes.map(d => `<li>${esc(d)}</li>`).join("")}</ol>` : ""}
          ${(f.evidence || []).length ? `<pre class="db-ev">${esc(f.evidence.join("\n"))}</pre>` : ""}</div>`).join("")}
        <div class="hint">以上是 MineDeck 本地规则的分析，没有联网。</div></details></div>`;
    slot.querySelector('[data-x="close"]').onclick = () => {
      try { sessionStorage.setItem(dismissKey(name), sig); } catch {}
      slot.innerHTML = "";
    };
    const b = slot.querySelector('[data-x="ai"]');
    if (b) b.onclick = () => analyze(name, b);
  }

  /* ---------- 设置 → AI 里的「应用内助手」 ---------- */

  function settings(slot, v, gen) {
    if (!slot) return;
    const ro = !v.local;
    slot.innerHTML = settingsHead("应用内助手", "在标题栏右上角打开。AI 先查日志和配置再回答；要改东西时先给你一张确认卡片，你确认了才执行。") +
      group(
        riRow({ ico: "phone", title: "局域网可用 AI 助手",
          sub: esc("局域网登录的设备（如手机）也能用助手，和本机共用每日额度；确认卡片由那台设备上的人自己确认。关掉后局域网打开的界面里没有助手入口。"),
          right: `<label class="switch" title="局域网可用 AI 助手"><input type="checkbox" id="ai-lan"${v.lanAssistant ? " checked" : ""}${ro ? " disabled" : ""} aria-label="局域网可用 AI 助手"><span class="sw"></span></label>` }) +
        riRow({ ico: "clock", title: "会话记录",
          sub: esc(ro ? "这里只能看；局域网设备开的会话可以在助手面板的「会话记录」里删除" : "本机保存最近 20 个会话（data/ai/sessions），删掉之后找不回来"),
          right: `<button class="btn danger" id="ai-sess-clear"${ro ? " disabled" : ""}>清空</button>` }),
        { ico: true });
    const lan = slot.querySelector("#ai-lan");
    lan.onchange = async () => {
      try {
        await api("/api/ai/assistant", { method: "PUT", body: { lan: lan.checked } });
        toast(lan.checked ? "局域网登录的设备可以用 AI 助手了" : "已关闭：局域网登录的设备不能用 AI 助手");
      } catch (e) { toast(e.message, true); if (!stale(gen)) lan.checked = !lan.checked; }
      refresh();
    };
    slot.querySelector("#ai-sess-clear").onclick = clearAll;
    refresh();
  }

  /* ---------- Markdown（只认常用写法；先整体转义 HTML，链接只放行 http(s)） ---------- */

  function md(src) { return mdLines(esc(src).replace(/\r\n?/g, "\n").split("\n")); }

  const isFence = s => /^\s*```/.test(s);
  const isHr = s => /^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(s);
  const heading = s => s.match(/^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
  const listItem = s => s.match(/^(\s*)([-*+•]|\d{1,3}[.)、])\s+(.*)$/);
  const isQuote = s => /^\s{0,3}&gt;/.test(s);
  const isSep = s => s.includes("|") && /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(s);
  const isTable = (L, i) => L[i].includes("|") && i + 1 < L.length && isSep(L[i + 1]);

  function mdLines(L) {
    const out = [];
    let i = 0;
    while (i < L.length) {
      const s = L[i];
      if (!s.trim()) { i++; continue; }
      let m;
      if (isFence(s)) {
        const buf = [];
        for (i++; i < L.length && !isFence(L[i]); i++) buf.push(L[i]);
        i++;
        out.push(`<pre class="md-pre"><code>${buf.join("\n")}</code></pre>`);
      } else if ((m = heading(s))) {
        out.push(`<h4 class="md-h">${inline(m[2])}</h4>`);
        i++;
      } else if (isHr(s)) {
        out.push("<hr>");
        i++;
      } else if (isQuote(s)) {
        const buf = [];
        for (; i < L.length && isQuote(L[i]); i++) buf.push(L[i].replace(/^\s{0,3}&gt;\s?/, ""));
        out.push(`<blockquote>${mdLines(buf)}</blockquote>`);
      } else if (isTable(L, i)) {
        const cells = r => r.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map(c => c.trim());
        const hd = cells(s);
        const rows = [];
        for (i += 2; i < L.length && L[i].includes("|") && L[i].trim(); i++) rows.push(cells(L[i]));
        out.push(`<div class="md-table"><table><thead><tr>${hd.map(c => `<th>${inline(c)}</th>`).join("")}</tr></thead><tbody>` +
          rows.map(r => `<tr>${hd.map((_, k) => `<td>${inline(r[k] || "")}</td>`).join("")}</tr>`).join("") + `</tbody></table></div>`);
      } else if (listItem(s)) {
        const items = [];
        while (i < L.length) {
          const t = L[i], li = listItem(t);
          if (li) { items.push({ ind: li[1].replace(/\t/g, "    ").length, ord: /\d/.test(li[2]), n: parseInt(li[2], 10), text: [li[3]] }); i++; continue; }
          if (!t.trim()) { // 空行之后还是列表（或缩进的续行）就接着算
            const nx = L[i + 1];
            if (nx !== undefined && (listItem(nx) || /^\s{2,}\S/.test(nx)) && !isFence(nx)) { i++; continue; }
            break;
          }
          if (/^\s{2,}\S/.test(t) && !isFence(t)) { items[items.length - 1].text.push(t.trim()); i++; continue; }
          break;
        }
        out.push(listHTML(items));
      } else {
        const buf = [];
        for (; i < L.length && L[i].trim() && !isFence(L[i]) && !heading(L[i]) && !isHr(L[i]) && !isQuote(L[i]) && !listItem(L[i]) && !isTable(L, i); i++) buf.push(L[i].trim());
        out.push(`<p>${buf.map(inline).join("<br>")}</p>`);
      }
    }
    return out.join("");
  }

  // 按缩进嵌套的列表
  function listHTML(items) {
    let html = "";
    const stack = [];
    for (const it of items) {
      while (stack.length && it.ind < stack[stack.length - 1].ind) html += `</li></${stack.pop().tag}>`;
      const top = stack[stack.length - 1];
      if (!top || it.ind > top.ind) {
        const tag = it.ord ? "ol" : "ul";
        html += `<${tag}${it.ord && it.n > 1 ? ` start="${it.n}"` : ""}>`;
        stack.push({ ind: it.ind, tag });
      } else html += "</li>";
      html += `<li>${it.text.map(inline).join("<br>")}`;
    }
    while (stack.length) html += `</li></${stack.pop().tag}>`;
    return html;
  }

  // 行内：`代码`、**加粗**、~~删除线~~、[链接](http…)。代码里的内容原样显示
  function inline(s) {
    return s.split(/(`[^`]+`)/).map((seg, k) => k % 2 ? `<code>${seg.slice(1, -1)}</code>` : seg
      .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_, t, u) => `<a href="${u}" target="_blank" rel="noopener noreferrer">${t}</a>`)
      .replace(/\*\*(?=\S)(.+?)\*\*/g, "<b>$1</b>")
      .replace(/__(?=\S)(.+?)__/g, "<b>$1</b>")
      .replace(/~~(?=\S)(.+?)~~/g, "<s>$1</s>")).join("");
  }

  function when(t) {
    const d = new Date(t);
    if (isNaN(d)) return "";
    const p = n => String(n).padStart(2, "0");
    const hm = `${p(d.getHours())}:${p(d.getMinutes())}`;
    return d.toDateString() === new Date().toDateString() ? hm : `${p(d.getMonth() + 1)}-${p(d.getDate())} ${hm}`;
  }

  return { init, refresh, open, close, banner, consoleLine, settings, analyze, md, ext };
})();
