/* MineDeck MCP 接口（设计文档 §15）：「设置 → AI（可选）」里的「MCP 接口」分组，以及外部 AI 提出、等你确认的操作。
   外部 AI 经 MCP 调写操作时只生成提案：这里订阅 /api/mcp/events（只给本机），新提案来时弹一条提示、标题栏「AI 助手」亮小方块，
   助手面板顶上列出确认卡片（卡片和应用内助手是同一个组件，在 assistant.js）。
   app.js 只在两处调这里：启动后 MDMcp.init()；设置 → AI 里 settings()。用到的 app.js 工具都在调用时才取。 */
"use strict";
window.MDMcp = (() => {
  const M = {
    st: null,       // GET /api/mcp
    es: null,       // 等确认的外部 AI 提案的事件流（本机、打开了 MCP 接口时才连）
    pending: [],    // 等确认的外部 AI 提案
    seen: new Set(), // 提示过的提案编号
    first: true,    // 连上后的第一份：已经在等的合成一条提示
  };
  const MASK = "<令牌，复制时自动带上>";

  async function init() { await refresh(); }

  /* 重新取状态，按需连上或断开事件流 */
  async function refresh() {
    try { M.st = await api("/api/mcp"); } catch { M.st = null; }
    connect();
    return M.st;
  }

  function connect() {
    const want = !!(M.st && M.st.local && M.st.enabled);
    if (!want) {
      if (M.es) { M.es.close(); M.es = null; }
      if (M.pending.length) set([]);
      return;
    }
    if (M.es) return;
    const es = new EventSource("/api/mcp/events?token=" + encodeURIComponent(TOKEN()));
    M.es = es;
    es.onmessage = e => {
      if (M.es !== es) return;
      let ev;
      try { ev = JSON.parse(e.data); } catch { return; }
      if (ev.type === "pending") set(ev.items || []);
    };
    es.onerror = () => { if (M.es === es && es.readyState === EventSource.CLOSED) M.es = null; }; // 被拒（刚关掉 MCP）：等下次 refresh 再连
  }

  function set(items) {
    const fresh = items.filter(p => !M.seen.has(p.id));
    items.forEach(p => M.seen.add(p.id));
    M.pending = items;
    if (fresh.length) {
      if (M.first && fresh.length > 1) notify(`外部 AI 有 ${fresh.length} 个操作等你确认：在右上角「AI 助手」里处理`);
      else fresh.forEach(p => notify(`外部 AI「${p.actor}」请求：${p.title}。点这里打开「AI 助手」确认`));
    }
    M.first = false;
    window.MDAssist?.ext(); // 助手面板顶上的卡片、标题栏入口的小方块
  }

  /* 提示：点一下打开助手面板（提示本身也会关掉） */
  function notify(text) {
    const el = toast(text);
    if (!el) return;
    el.classList.add("ext");
    const ico = el.querySelector(".t-ico");
    if (ico) ico.innerHTML = icon("plug");
    const kill = el.onclick;
    el.title = "打开 AI 助手确认";
    el.onclick = () => { if (kill) kill(); window.MDAssist?.open(); };
  }

  const pending = () => M.pending.slice();

  /* 确认或拒绝之后先从列表里拿掉（事件流随后也会推一份新的） */
  function drop(id) {
    M.pending = M.pending.filter(p => p.id !== id);
    window.MDAssist?.ext();
  }

  /* ---------- 设置 → AI 里的「MCP 接口」 ---------- */

  const HEAD = "让这台电脑上的其他 AI 工具（Claude Desktop、Cherry Studio、Cursor、VS Code 等）通过 MCP 用 MineDeck：查看实例、日志、配置直接执行；要改东西时，MineDeck 里会出现确认卡片，你确认了才执行。默认关闭，只接受这台电脑上的连接。";

  async function settings(slot, gen) {
    if (!slot) return;
    slot.innerHTML = settingsHead("MCP 接口", esc(HEAD)) + `<div class="sub">加载中…</div>`;
    let v;
    try { v = await api("/api/mcp"); }
    catch (e) { if (!stale(gen) && slot.isConnected) slot.innerHTML = settingsHead("MCP 接口", esc(HEAD)) + `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen) || !slot.isConnected) return;
    M.st = v;
    connect();
    draw(slot, v, gen);
  }

  function ago(t) {
    const d = new Date(t), s = Math.round((Date.now() - d) / 1000);
    if (isNaN(d)) return "";
    if (s < 60) return "刚刚";
    if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
    const p = n => String(n).padStart(2, "0");
    const hm = `${p(d.getHours())}:${p(d.getMinutes())}`;
    return d.toDateString() === new Date().toDateString() ? hm : `${p(d.getMonth() + 1)}-${p(d.getDate())} ${hm}`;
  }

  function draw(slot, v, gen) {
    const ro = !v.local;
    const sw = `<label class="switch" title="开启 MCP 接口"><input type="checkbox" id="mcp-on"${v.enabled ? " checked" : ""}${ro ? " disabled" : ""} aria-label="开启 MCP 接口"><span class="sw"></span></label>`;
    const portSub = v.portChanged
      ? `<span class="warn-text">${icon("warn")} 想用的端口被占用，这次用的是 ${esc(String(v.port))}：HTTP 方式的配置要跟着改，或者改用 stdio 方式</span>`
      : "HTTP 方式连这个地址；stdio 方式（MineDeck.exe -mcp）不受端口影响";
    const tokenSub = v.tokenBroken
      ? `<span class="warn-text">${icon("warn")} 令牌解不开，可能换了电脑或 Windows 用户：请重置</span>`
      : "已生成，加密保存在本机，复制配置时才带上；泄露了就重置（HTTP 方式的客户端要换新令牌，stdio 方式不用改）";
    let rows = riRow({ ico: "plug", title: "开启 MCP 接口",
      sub: esc(v.enabled ? `已开启：外部 AI 能用 ${v.tools} 个工具，写操作和危险操作要你在这里确认` : "关闭时外部 AI 连不上 MineDeck；打开时会生成一个访问令牌"), right: sw });
    if (v.enabled) {
      rows += riRow({ ico: "globe", title: "接口地址", sub: portSub, key: v.url,
        right: ro ? "" : `<button class="btn sm" id="mcp-url">${icon("copy")} 复制</button>` });
      rows += riRow({ ico: "key", title: "访问令牌", sub: tokenSub, right: ro ? "" : `<button class="btn" id="mcp-reset">重置令牌</button>` });
      if (!ro) rows += riRow({ ico: "doc", title: "复制配置", sub: "选你用的 AI 工具，复制后按说明粘贴进它的 MCP 配置",
        right: `<button class="btn" id="mcp-copy" aria-haspopup="menu">${icon("copy")} 复制配置…</button>` });
    }
    const clients = (v.clients || []).map(c => riRow({ ico: c.transport === "stdio" ? "monitor" : "globe",
      titleHTML: `${esc(c.title || c.name)}${c.version ? ` <span class="badge">${esc(c.version)}</span>` : ""}`,
      sub: esc([c.transport === "stdio" ? "stdio" : "HTTP", "协议 " + c.protocol, c.calls ? `调用了 ${c.calls} 次工具` : "", ago(c.last)].filter(Boolean).join(" · ")) })).join("");
    slot.innerHTML = settingsHead("MCP 接口", esc(HEAD)) +
      (ro ? `<div class="warn-card"><div class="wc-title">${icon("lock")} MCP 接口的设置仅支持在本机修改</div><div class="wc-note">这里只能查看。令牌和配置只给运行 MineDeck 的那台电脑；外部 AI 提出的操作也只能在那台电脑上确认。</div></div>` : "") +
      group(rows, { ico: true, attrs: `id="mcp-group"` }) +
      (v.enabled ? `<div class="mcp-sub-head">连过的 AI 工具（本次启动以来）</div>` + group(clients || riRow({ ico: "clock", title: "还没有 AI 工具连过",
        sub: "配置好以后，在那个 AI 工具里问一句「MineDeck 里有哪些服务器」试试" }), { ico: true, attrs: `id="mcp-clients"` }) : "") +
      `<div class="hint mcp-hint">外部 AI 看到的内容和应用内助手一样先脱敏（Windows 用户名、局域网 IP、Key 都会替换掉）；删实例、改设置这类仅人工的操作不会给它。令牌只给这台电脑上的程序用，别发给别人。</div>`;
    const on = slot.querySelector("#mcp-on");
    on.onchange = async () => {
      on.disabled = true;
      try {
        v = await api("/api/mcp", { method: "PUT", body: { enabled: on.checked } });
        toast(v.enabled ? "MCP 接口已打开" : "MCP 接口已关闭：外部 AI 连不上了，没处理的请求已作废");
      } catch (e) { toast(e.message, true); on.checked = !on.checked; on.disabled = false; return; }
      M.st = v;
      connect();
      if (!stale(gen) && slot.isConnected) draw(slot, v, gen);
    };
    const url = slot.querySelector("#mcp-url");
    if (url) url.onclick = () => copy(v.url, "已复制接口地址");
    const reset = slot.querySelector("#mcp-reset");
    if (reset) reset.onclick = async () => {
      const ok = await confirmModal({ title: "重置 MCP 访问令牌？", danger: true, icon: "key", okText: "重置",
        body: "旧令牌马上失效：用 HTTP 方式连的 AI 工具（Cherry Studio、Cursor、VS Code 等）要换上新令牌才能连上；stdio 方式（Claude Desktop）不用改。" });
      if (!ok) return;
      try { v = await api("/api/mcp/token", { method: "POST" }); toast("已重置令牌：记得更新 HTTP 方式的配置"); }
      catch (e) { toast(e.message, true); return; }
      if (!stale(gen) && slot.isConnected) draw(slot, v, gen);
    };
    const cp = slot.querySelector("#mcp-copy");
    if (cp) cp.onclick = async e => {
      const anchor = e.currentTarget;
      let cfg;
      try { cfg = await api("/api/mcp/config"); } catch (err) { toast(err.message, true); return; }
      dropMenu(anchor, [{ header: "复制给哪个 AI 工具" }, ...cfg.snippets.map(s => ({
        label: s.name + (s.transport === "stdio" ? "（stdio）" : "（HTTP）"), icon: s.transport === "stdio" ? "monitor" : "globe", onClick: () => snippet(s, cfg.token) }))]);
    };
  }

  /* 一种客户端的配置：说明粘贴到哪儿，预览里令牌是占位，复制的是完整的 */
  async function snippet(s, token) {
    const shown = token ? s.text.split(token).join(MASK) : s.text;
    const ok = await confirmModal({ title: `${s.name} 的配置`, okText: "复制", cancelText: "关闭",
      body: `${esc(s.where)}<pre class="ap-pre mcp-pre">${esc(shown)}</pre>` });
    if (ok) copy(s.text, `已复制 ${s.name} 的配置`);
  }

  return { init, refresh, settings, pending, drop };
})();
