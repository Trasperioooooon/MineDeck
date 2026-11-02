/* MineDeck AI 伙伴（第七阶段，设计文档 §7、§9 第 9 条）：实例详情的「伙伴」标签。
   伙伴是服务器里一个像玩家一样的角色：PlayerEngine 做身体，MineDeck 做大脑；玩家在聊天里用「@名字 话」指挥它（经游戏内 AI）。
   这一页从上到下：安装（伙伴模组、PlayerEngine、Architectury API 的版本和状态，一键安装或更新）、好友的客户端要装什么（按清单，
   附 Modrinth 链接）、设置（开关、伙伴上限、attack 不许打的生物）、禁挖区域、伙伴列表（刷出、移除、停下）、最近的事件。
   保存语义（MASTER §11）：开关即时生效；伙伴上限、不许打的生物是单字段保存（没改时保存键置灰，回车即保存）；
   禁挖区域、刷出伙伴在弹窗里填，字段下方就近报错。
   在 app.js 之前加载；用到的 app.js 工具（api、esc、icon、ICON、toast、confirmModal、copy、dismiss、startPoll、stale、bindSaveInput、
   group、riRow、skeletonRows、fmtTokens）都在调用时才取。app.js 里只留了两处钩子：详情页的标签和分派。 */
"use strict";
window.MDCompanion = (() => {
  /* 有「伙伴」标签的核心：伙伴模组只有 NeoForge、Forge 版（具体支持哪个 MC 版本由后端按清单判断） */
  const CORES = new Set(["neoforge", "forge"]);
  const applies = info => CORES.has(info.core);

  /* 新增的图标（16 视框、细线，同 app.js 的 ICON），第一次用时并进 ICON */
  const ICONS = {
    bot: '<rect x="3" y="4.6" width="10" height="8.2" rx="2"/><path d="M8 2.2v2.4M6.1 8.3h.01M9.9 8.3h.01M6.3 10.7h3.4"/>',
    pick: '<path d="M3.2 12.8l6.4-6.4"/><path d="M5.6 3.6c2.4-1 5-.4 6.8 1.4 1.8 1.8 2.4 4.4 1.4 6.8"/>',
    users: '<circle cx="6" cy="5.6" r="2.2"/><path d="M2.4 12.8c0-2 1.6-3.6 3.6-3.6s3.6 1.6 3.6 3.6"/><circle cx="11.2" cy="6.2" r="1.8"/><path d="M10.6 9.3c1.8-.1 3.2 1.3 3.2 3.1"/>',
    heart: '<path d="M8 13.2S2.6 9.9 2.6 6.1A2.8 2.8 0 018 4.7a2.8 2.8 0 015.4 1.4C13.4 9.9 8 13.2 8 13.2z"/>',
    shield: '<path d="M8 2.2l5 1.8v3.7c0 2.9-2.1 5.2-5 6.1-2.9-.9-5-3.2-5-6.1V4z"/><path d="M5.9 8.1l1.5 1.5 2.8-3"/>',
  };
  let iconsReady = false;
  const ensureIcons = () => { if (!iconsReady) { Object.assign(ICON, ICONS); iconsReady = true; } };

  /* 三个模组的状态（同 companion.ModOK……）、连接、伙伴的状态、任务的结果（不认识的按兼容规则：state 当成 busy、result 当成 failed） */
  const MOD_STATE = { ok: ["", "已装好"], inrange: ["neutral", "能用（不是实测过的版本）"], outdated: ["warn", "要换"], missing: ["warn", "没装"] };
  const LINK = { off: ["", "没打开"], waiting: ["", "等服务器开好"], connecting: ["starting", "正在连"], live: ["running", "已连上"],
    retrying: ["starting", "断开了，正在重连"], failed: ["stopping", "连不上"] };
  const COMP_STATE = { starting: ["starting", "准备中"], idle: ["running", "空闲"], busy: ["starting", "忙"] };
  const RESULT = { done: "做完了", incomplete: "没做够", failed: "失败了", cancelled: "停下了", superseded: "被新任务顶掉了", timeout: "超时了" };
  const DIMS = [["minecraft:overworld", "主世界"], ["minecraft:the_nether", "下界"], ["minecraft:the_end", "末地"]];
  const dimName = d => (DIMS.find(x => x[0] === d) || [d, String(d || "").replace(/^minecraft:/, "")])[1];

  /* 校验，同后端：伙伴的名字（companion.CheckName）、玩家名（ValidPlayer）、禁挖区域（Zone.Check）、生物 ID（NormalizeEntityID） */
  const nameErr = n => {
    if (!n || [...n].length > 16) return "名字要 1–16 个字";
    if (/[\s@＠/／、:：,，<>"\\§]/.test(n) || /[\u0000-\u001f]/.test(n)) return "名字里不能有空格、@、/、、、冒号、逗号、< > \" \\ §：玩家要在聊天里用「@名字」叫它";
    return "";
  };
  const playerOK = n => /^[A-Za-z0-9_.]{1,20}$/.test(n);
  const zoneNameOK = n => /^[A-Za-z0-9_-]{1,32}$/.test(n);
  const dimOK = d => d.length <= 100 && /^[a-z0-9_.-]+:[a-z0-9_./-]+$/.test(d);
  const entityOK = id => id.length <= 100 && /^(?:[a-z0-9_.-]+:)?[a-z0-9_./-]+$/.test(id);
  const coordErr = (k, s) => {
    if (!/^-?\d+$/.test(s.trim())) return "坐标要是整数";
    const n = +s;
    if (k === "y") return n < -2048 || n > 2048 ? "y 要在 -2048 到 2048 之间" : "";
    return Math.abs(n) > 30000000 ? "x、z 要在 ±30000000 以内" : "";
  };

  const shortID = id => String(id ?? "").replace(/^minecraft:/, "");
  const num = x => (Number.isInteger(+x) ? String(+x) : (+x).toFixed(1));
  const when = at => {
    const d = new Date(at);
    if (isNaN(d)) return "";
    const p = n => String(n).padStart(2, "0");
    const hm = `${p(d.getHours())}:${p(d.getMinutes())}`;
    return d.toDateString() === new Date().toDateString() ? hm : `${d.getMonth() + 1} 月 ${d.getDate()} 日 ${hm}`;
  };
  const head = (title, sub, id = "") => `<div class="cp-head"${id ? ` id="${id}"` : ""}><h3>${esc(title)}</h3>${sub ? `<div class="sub">${sub}</div>` : ""}</div>`;
  const pill = (cls, text) => `<span class="st-pill ${cls}"><span class="dot ${cls}"></span><span class="st-label">${esc(text)}</span></span>`;
  const fieldErr = (box, inp, msg) => { if (box) box.innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; if (inp) inp.classList.toggle("invalid", !!msg); };
  const silent = e => Object.assign(e instanceof Error ? e : new Error(e), { silent: true });
  const swHTML = (attrs, on, label, dis) => `<label class="switch" title="${esc(label)}"><input type="checkbox" ${attrs} ${on ? "checked" : ""} aria-label="${esc(label)}"${dis ? " disabled" : ""}><span class="sw"></span></label>`;
  const mb = n => (n / 1048576).toFixed(1) + " MB";
  const sig = (box, data) => { const s = JSON.stringify(data); if (box.dataset.sig === s) return false; box.dataset.sig = s; return true; };

  /* 任务的说法（同后端 companion.ActionTitle） */
  function taskTitle(t) {
    if (!t) return "任务";
    const a = t.args || {}, n = k => (a[k] == null ? "" : num(a[k]));
    switch (t.op) {
      case "collect": return `采集 ${shortID(a.item)}${a.count != null ? " ×" + n("count") : ""}`;
      case "follow": return a.player ? `跟着 ${a.player}` : "跟着主人";
      case "goto": return a.y != null ? `去 (${n("x")}, ${n("y")}, ${n("z")})` : `去 (${n("x")}, ${n("z")})`;
      case "attack": return `打 ${shortID(a.mob)}${a.count != null ? " ×" + n("count") : ""}${a.duration != null ? `（守 ${n("duration")} 秒）` : ""}`;
      case "give": return `交出 ${shortID(a.item)}${a.count != null ? " ×" + n("count") : ""}${a.player ? " 给 " + a.player : ""}`;
    }
    return t.op || "任务";
  }
  const resultWord = r => RESULT[r] || RESULT.failed;

  /* 弹窗表单（外观同 confirmModal）：点确定时交给 check(mask)，返回 null 表示有错（错误已就近显示），弹窗留着；
     返回值就交出去。Esc、点外面、取消返回 null。setup(mask) 在弹窗画好后调用（接上弹窗里的联动）。 */
  function formModal({ title, body, okText = "确定", check, setup }) {
    return new Promise(resolve => {
      const root = $("#modal-root"), prev = document.activeElement;
      root.innerHTML = `<div class="modal-mask enter"><div class="modal cp-form" role="dialog" aria-modal="true" aria-labelledby="cp-m-title">
        <h3 id="cp-m-title">${esc(title)}</h3>${body}
        <div class="m-actions"><button class="btn" data-x="cancel">取消</button><button class="btn primary" data-x="ok">${esc(okText)}</button></div></div></div>`;
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
      const ok = () => { const v = check(mask); if (v != null) end(v); };
      const onKey = e => {
        if (e.key === "Escape") end(null);
        else if (e.key === "Enter" && e.target.tagName === "INPUT") { e.preventDefault(); ok(); }
      };
      document.addEventListener("keydown", onKey);
      mask.addEventListener("mousedown", e => { if (e.target === e.currentTarget) end(null); });
      mask.querySelector('[data-x="cancel"]').onclick = () => end(null);
      mask.querySelector('[data-x="ok"]').onclick = ok;
      if (setup) setup(mask);
      (mask.querySelector("input, select") || mask.querySelector('[data-x="ok"]')).focus();
    });
  }

  async function render(body, info, gen) {
    ensureIcons();
    const name = info.name, enc = encodeURIComponent(name), base = `/api/instances/${enc}/companion`;
    body.innerHTML = `<div class="cp">${skeletonRows(6)}</div>`;
    let v;
    try { v = await api(base); } catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    if (!v.install.supported) {
      body.innerHTML = `<div class="cp"><div class="warn-card"><div class="wc-title">${icon("warn")} 这台服务器用不了 AI 伙伴</div>
        <div class="wc-note">${esc(v.install.reason)}</div></div>
        <div class="hint">AI 伙伴是一个服务端模组（PlayerEngine 做身体，MineDeck 做大脑），每个加载器、每个 Minecraft 版本要各编一个，现在只做了上面列出的这几种。</div></div>`;
      return;
    }
    const st = { installing: false };
    body.innerHTML = `<div class="cp" id="cp">
      <div id="cp-gate"></div>
      ${head("安装", "服务端要装这三个模组：伙伴模组随 MineDeck 内置；PlayerEngine 和 Architectury API 点「安装」时才从 Modrinth 下载（实测过的那个版本，核对 sha512），不会自动换成新版本。")}
      <div id="cp-install"></div>
      ${head("好友的客户端", "伙伴是模组新加的实体：好友的客户端也要装下面标了「要装」的模组，版本和服务器的一样，放进客户端的 mods 目录。")}
      <div id="cp-client"></div>
      ${head("设置", "改了以后约 2 秒内生效，不用重启服务器。")}
      <div id="cp-set"></div>
      ${head("禁挖区域", "伙伴不拆、也不放区域里的方块（倒水、点火、锄地这类也不做），可以从区域里走过，绕不过去时会报「卡住了」。坐标是方块坐标，包含两端，两个角谁先谁后都行。")}
      <div id="cp-zones"></div>
      ${head("伙伴", "玩家在游戏里用「@名字 话」跟伙伴说话；只有在「游戏内 AI」标签里授权了的人能让它干活，活干完了会私下告诉发起的人。")}
      <div id="cp-link"></div>
      <div id="cp-list"></div>
      ${head("最近的事件", "这次开服以来伙伴模组报来的事，新的在前（只在内存里，最多 100 条）。")}
      <div id="cp-events"></div>
    </div>`;

    const refresh = async () => {
      try { const nv = await api(base); if (stale(gen)) return; v = nv; drawLive(); } catch { /* 轮询偶尔失败不打扰，下一拍再试 */ }
    };
    const put = async patch => { v = await api(base + "/settings", { method: "PUT", body: patch }); if (!stale(gen)) drawLive(); return v; };
    const post = async (sub, payload) => { const r = await api(base + "/" + sub, { method: "POST", body: payload || {} }); await refresh(); return r; };

    /* ---- 顶部提示：AI 没配置、游戏内 AI 没开、加载器版本不对 ---- */
    const drawGate = () => {
      const box = $("#cp-gate");
      if (!sig(box, [v.ai.enabled, v.gameai.enabled, v.install.loaderWarn, v.settings.enabled])) return;
      let html = "";
      if (v.install.loaderWarn) html += `<div class="warn-card"><div class="wc-title">${icon("warn")} 加载器的版本不对</div><div class="wc-note">${esc(v.install.loaderWarn)}</div></div>`;
      if (!v.ai.enabled) html += `<div class="warn-card"><div class="wc-title">${icon("warn")} AI 还没配置</div>
        <div class="wc-note">伙伴靠 AI 听懂玩家的话：先在「设置 → AI」里选好服务商、填好 Key 和模型。没配置时伙伴照样能刷出、移除，只是没人能在游戏里指挥它。</div>
        <a class="btn sm" href="#/settings/ai">${icon("gear")} 去设置</a></div>`;
      else if (v.settings.enabled && !v.gameai.enabled) html += `<div class="cp-note">${icon("info")} 玩家要在聊天里叫伙伴，还得在「游戏内 AI」标签里打开游戏内 AI。
        <a href="#/inst/${enc}/gameai">去打开</a></div>`;
      box.innerHTML = html;
    };

    /* ---- 安装：三个模组各自的版本和状态，一键安装或更新 ---- */
    const drawInstall = () => {
      const box = $("#cp-install"), iv = v.install;
      if (!sig(box, [iv, v.serverStatus, v.restart, st.installing])) return;
      const rows = iv.mods.map(m => {
        const [cls, label] = MOD_STATE[m.state] || MOD_STATE.missing;
        const lines = [];
        if (m.version) lines.push(`装的是 ${esc(m.version)}`);
        lines.push(m.kind === "companion" ? `MineDeck 内置的是 ${esc(m.want)}` : `实测过的是 ${esc(m.want)}，要求 ${esc(m.range)}`);
        const old = (m.files || []).filter(f => !f.keep).map(f => f.file);
        if (old.length) lines.push(`安装时删掉：${esc(old.join("、"))}`);
        const off = (m.files || []).filter(f => f.disabled).map(f => f.file);
        if (off.length) lines.push(`停用着的（不动）：${esc(off.join("、"))}`);
        return riRow({ ico: m.kind === "companion" ? "bot" : "package", cls: "cp-mod", attrs: `data-mod="${esc(m.id)}"`,
          titleHTML: `${esc(m.name)}<span class="tag${cls ? " " + cls : ""}">${esc(label)}</span>`, sub: lines.join(" · "), key: m.file,
          right: m.page ? `<a class="btn sm ghost" href="${esc(m.page)}" target="_blank" rel="noopener">Modrinth</a>` : "" });
      });
      const replace = iv.mods.some(m => (m.files || []).some(f => !f.keep));
      const running = v.serverStatus !== "stopped";
      let action;
      if (st.installing) action = riRow({ ico: "download", title: "正在安装…", sub: iv.download ? `正在从 Modrinth 下载约 ${mb(iv.download)}，网络慢的话要等一会儿` : "正在写进 mods 目录" });
      else if (!iv.needed) action = riRow({ ico: "check", title: v.restart ? "装好了，重启服务器后生效" : "三个都装好了", cls: "cp-ready",
        sub: v.restart ? "服务器运行中装的模组，要重启才会加载" : "以后 MineDeck 更新了伙伴模组，这里会提示「要换」" });
      else if (running && replace) action = riRow({ ico: "warn", title: "先停服再安装", sub: "服务器运行中不能替换已经装着的旧版本（文件被服务器占用）",
        right: `<button class="btn primary" id="cp-install-btn" disabled>${icon("download")} 安装</button>` });
      else action = riRow({ ico: "download", title: iv.ready ? "整理 mods 目录" : "一键安装",
        sub: (iv.download ? `要从 Modrinth 下载约 ${mb(iv.download)}` : "不用下载") + (running ? "；服务器正在运行，装好后重启才生效" : ""),
        right: `<button class="btn primary" id="cp-install-btn">${icon("download")} ${iv.mods.some(m => m.state === "outdated") ? "更新" : "安装"}</button>` });
      box.innerHTML = group(rows.join("") + action, { ico: true });
      const btn = $("#cp-install-btn");
      if (btn && !btn.disabled) btn.onclick = install;
    };
    const install = async () => {
      st.installing = true;
      drawInstall();
      try {
        const r = await api(base + "/install", { method: "POST" });
        if (stale(gen)) return;
        toast(r.installed.length ? `已安装：${r.installed.join("、")}${r.restart ? "（重启服务器后生效）" : ""}` : "都装好了，没什么要改的");
        for (const f of r.fallback || []) toast(f, true);
      } catch (e) { toast(e.message, true); }
      st.installing = false;
      await refresh();
      if (!stale(gen)) drawInstall();
    };

    /* ---- 好友的客户端：按清单列出（side 是 server 的不用装），附 Modrinth 链接 ---- */
    const drawClient = () => {
      const box = $("#cp-client");
      if (!sig(box, [v.client, v.local])) return;
      const rows = v.client.map(c => riRow({ ico: c.page ? "package" : "bot", attrs: `data-client="${esc(c.id)}"`,
        titleHTML: `${esc(c.name)} <span class="cp-ver">${esc(c.version)}</span><span class="tag${c.required ? "" : " neutral"}">${c.required ? "要装" : "不用装"}</span>`,
        sub: c.page ? `${esc(c.license || "")}${c.license ? " · " : ""}从 Modrinth 下载这个版本` : "MineDeck 内置，不在 Modrinth 上：从服务器的 mods 目录里把这个文件发给好友",
        key: c.file || "",
        right: c.page ? `<a class="btn sm" href="${esc(c.page)}" target="_blank" rel="noopener">${icon("link")} 下载页</a>`
          : v.local ? `<button class="btn sm" id="cp-mods-dir">${icon("folder")} 打开 mods 目录</button>` : "" }));
      rows.push(riRow({ ico: "users", title: "复制给好友的说明", sub: "要装哪几个、去哪里下，一段话发到群里就行。",
        right: `<button class="btn sm" id="cp-copy-guide">${icon("copy")} 复制</button>` }));
      box.innerHTML = group(rows.join(""), { ico: true });
      const dir = $("#cp-mods-dir");
      if (dir) dir.onclick = () => api(`/api/instances/${enc}/opendir?sub=mods`, { method: "POST" }).catch(e => toast(e.message, true));
      $("#cp-copy-guide").onclick = () => copy(guide(), "已复制给好友的说明");
    };
    const guide = () => {
      const need = v.client.filter(c => c.required);
      const lines = need.map((c, i) => `${i + 1}. ${c.name} ${c.version}：${c.page || "找房主要文件 " + (c.file || "")}`);
      return `服务器「${name}」开了 MineDeck 的 AI 伙伴，客户端要装这几个模组（版本和服务器的一样），放进 .minecraft 的 mods 目录：\n${lines.join("\n")}` +
        `\n进服后在聊天里说「@伙伴的名字 话」就能叫它。`;
    };

    /* ---- 设置：开关即时生效；伙伴上限、不许打的生物单字段保存（输入框只画一次，不随轮询重建） ---- */
    const drawSettings = () => {
      const s = v.settings;
      $("#cp-set").innerHTML = group(
        riRow({ ico: "power", title: "打开 AI 伙伴", sub: `<span id="cp-on-sub"></span>`, right: swHTML(`id="cp-on"`, s.enabled, "打开 AI 伙伴") }) +
        riRow({ ico: "bot", title: "伙伴上限", sub: `同时最多几个伙伴（1–${s.maxLimit}）。调低了不会移除现有的，只是不能再刷。`,
          field: `<input type="number" id="cp-max" min="1" max="${s.maxLimit}" step="1" inputmode="numeric" value="${s.maxCompanions}" aria-label="伙伴上限">` +
            `<span class="ri-unit">个</span><button class="btn" id="cp-max-save">保存</button>`,
          after: `<div class="field-err ri-err" id="cp-max-err"></div>` }) +
        riRow({ ico: "shield", title: "攻击时不许打的生物", sub: "生物 ID，逗号隔开；清空表示没有额外限制。玩家、伙伴、驯服的、起了名字的、拴着的生物不管这里怎么写都不打。",
          field: `<input type="text" id="cp-deny" value="${esc((s.attackDeny || []).join(", "))}" spellcheck="false" maxlength="2000" aria-label="不许打的生物">` +
            `<button class="btn" id="cp-deny-save">保存</button><button class="btn ghost" id="cp-deny-default">恢复默认</button>`,
          after: `<div class="field-err ri-err" id="cp-deny-err"></div>` }) +
        riRow({ ico: "coin", title: "AI 伙伴的每日额度", sub: `<span id="cp-quota"></span>`, right: v.local ? `<a class="btn sm" href="#/settings/ai">去设置</a>` : "" }),
        { ico: true }) + `<div id="cp-set-note"></div>`;
      $("#cp-on").onchange = async e => {
        const on = e.target.checked;
        try {
          await put({ enabled: on });
          toast(on ? "已打开 AI 伙伴：服务器开着的话，MineDeck 会自动连上伙伴模组" : "已关掉 AI 伙伴：伙伴还在游戏里，只是 MineDeck 指挥不了它们");
        } catch (err) { e.target.checked = !on; toast(err.message, true); }
      };
      const maxErr = msg => fieldErr($("#cp-max-err"), $("#cp-max"), msg);
      $("#cp-max").addEventListener("input", () => maxErr(""));
      bindSaveInput($("#cp-max"), $("#cp-max-save"), async raw => {
        const n = +raw;
        if (!Number.isInteger(n) || n < 1 || n > s.maxLimit) { maxErr(`请填 1 到 ${s.maxLimit} 之间的整数`); throw silent("不合法"); }
        try { await put({ maxCompanions: n }); } catch (e) { maxErr(e.message); throw silent(e); }
        toast(`伙伴上限：${n} 个`);
      }, gen);
      const denyErr = msg => fieldErr($("#cp-deny-err"), $("#cp-deny"), msg);
      $("#cp-deny").addEventListener("input", () => denyErr(""));
      bindSaveInput($("#cp-deny"), $("#cp-deny-save"), async raw => {
        const list = raw.split(/[,，\s]+/).map(x => x.trim()).filter(Boolean);
        const bad = list.find(x => !entityOK(x));
        if (bad) { denyErr(`「${bad}」不是生物 ID（小写字母、数字和 _ . - / :，如 minecraft:villager）`); throw silent("不合法"); }
        try { await put({ attackDeny: list }); } catch (e) { denyErr(e.message); throw silent(e); }
        $("#cp-deny").value = v.settings.attackDeny.join(", ");
        toast(list.length ? `不许打：${v.settings.attackDeny.map(shortID).join("、")}` : "攻击时没有额外限制了");
      }, gen);
      $("#cp-deny-default").onclick = async () => {
        try { await put({ denyDefault: true }); $("#cp-deny").value = v.settings.attackDeny.join(", "); $("#cp-deny").dispatchEvent(new Event("input")); toast("已恢复默认：不打村民、流浪商人、铁傀儡、雪傀儡、悦灵"); }
        catch (e) { toast(e.message, true); }
      };
    };
    const drawSetLive = () => {
      const s = v.settings, on = $("#cp-on"), missing = v.install.mods[0].state === "missing";
      if (document.activeElement !== on) on.checked = s.enabled;
      on.disabled = missing && !s.enabled;
      $("#cp-on-sub").textContent = missing && !s.enabled ? "先装伙伴模组，再打开。"
        : s.enabled ? `伙伴模组在 127.0.0.1:${s.port} 上开着控制端口，只有 MineDeck 能连（令牌写在配置文件里，不要外传）。`
          : "关着时伙伴模组不开控制端口，MineDeck 指挥不了伙伴。";
      const q = v.ai;
      $("#cp-quota").textContent = `玩家叫着伙伴的名字说话时记在这里：今天用了 ${fmtTokens(q.quotaUsed)} token，` + (q.quotaLimit ? `每天最多 ${fmtTokens(q.quotaLimit)}。` : "没有上限。") +
        "活干完了的通知不调用 AI，不花钱。";
      const box = $("#cp-set-note");
      if (!sig(box, [s.pending, s.problems, s.applied && s.applied.problems])) return;
      const notes = [];
      if (s.pending) notes.push(`<div class="cp-note">${icon("clock")} 改了的设置伙伴模组还没读到（每 2 秒读一次），稍等一下。</div>`);
      const probs = [...(s.problems || []), ...((s.applied && s.applied.problems) || [])];
      if (probs.length) notes.push(`<div class="warn-card"><div class="wc-title">${icon("warn")} 配置文件里有没生效的条目</div><div class="wc-note">${probs.map(esc).join("<br>")}</div></div>`);
      box.innerHTML = notes.join("");
    };

    /* ---- 禁挖区域：列表 + 弹窗编辑；改完整体替换（PUT zones） ---- */
    const corner = c => `${c[0]}, ${c[1]}, ${c[2]}`;
    const drawZones = () => {
      const box = $("#cp-zones"), zs = v.settings.zones || [];
      if (!sig(box, zs)) return;
      const rows = zs.map(z => riRow({ ico: "pick", attrs: `data-zone="${esc(z.name)}"`, title: z.name,
        sub: `${esc(dimName(z.dim))} · <span class="cp-xyz">(${corner(z.min)}) → (${corner(z.max)})</span>`,
        right: `<button class="btn sm ghost" data-zedit="${esc(z.name)}">编辑</button><button class="btn sm ghost danger" data-zdel="${esc(z.name)}">删除</button>` }));
      if (!zs.length) rows.push(riRow({ ico: "pick", title: "还没有禁挖区域", sub: "把家、农场这类不想让伙伴动的地方圈起来。按 F3 能看到自己脚下的坐标。" }));
      rows.push(riRow({ ico: "plus", title: "添加区域", sub: `最多 ${v.settings.maxZones} 个`, right: `<button class="btn sm" id="cp-zone-add">${icon("plus")} 添加</button>` }));
      box.innerHTML = group(rows.join(""), { ico: true });
      $("#cp-zone-add").onclick = () => editZone(null);
      box.querySelectorAll("[data-zedit]").forEach(b => b.onclick = () => editZone(zs.find(z => z.name === b.dataset.zedit)));
      box.querySelectorAll("[data-zdel]").forEach(b => b.onclick = () => delZone(b.dataset.zdel));
    };
    const saveZones = async (zs, msg) => {
      try { await put({ zones: zs }); toast(msg); return true; } catch (e) { toast(e.message, true); return false; }
    };
    const editZone = async z => {
      const zs = v.settings.zones || [];
      const known = DIMS.some(d => d[0] === (z ? z.dim : "minecraft:overworld"));
      const cur = z || { name: "", dim: "minecraft:overworld", min: ["", "", ""], max: ["", "", ""] };
      const xyz = (id, c) => ["x", "y", "z"].map((k, i) => `<label><span>${k}</span><input type="text" id="cz-${id}${k}" inputmode="numeric" value="${esc(c[i])}" aria-label="${id === "a" ? "角 1" : "角 2"} 的 ${k}"></label>`).join("");
      const res = await formModal({ title: z ? `编辑禁挖区域「${z.name}」` : "添加禁挖区域", okText: "保存",
        body: `<div class="m-body">两个角的方块坐标（包含两端），比如家的东北角和西南角；y 不确定的话，填 -64 到 320 就把上下都圈进去了。</div>
          <div class="cp-fields">
            <label class="cp-f"><span>名字</span><input type="text" id="cz-name" maxlength="32" spellcheck="false" value="${esc(cur.name)}" placeholder="如 base、farm"></label>
            <label class="cp-f"><span>维度</span><select id="cz-dim">${DIMS.map(([id, n]) => `<option value="${id}"${cur.dim === id ? " selected" : ""}>${n}（${id}）</option>`).join("")}
              <option value=""${known ? "" : " selected"}>其他（模组的维度）</option></select></label>
            <label class="cp-f" id="cz-dim-row"${known ? " hidden" : ""}><span>维度 ID</span><input type="text" id="cz-dim-custom" spellcheck="false" value="${known ? "" : esc(cur.dim)}" placeholder="如 twilightforest:twilight_forest"></label>
            <div class="cp-f"><span>角 1</span><div class="cp-xyzin">${xyz("a", cur.min)}</div></div>
            <div class="cp-f"><span>角 2</span><div class="cp-xyzin">${xyz("b", cur.max)}</div></div>
          </div><div class="field-err" id="cz-err"></div>`,
        setup: mask => { // 维度选「其他」时才要填维度 ID
          const sel = mask.querySelector("#cz-dim"), row = mask.querySelector("#cz-dim-row");
          sel.onchange = () => { row.hidden = sel.value !== ""; if (!row.hidden) mask.querySelector("#cz-dim-custom").focus(); };
        },
        check: mask => {
          const q = s => mask.querySelector(s);
          const err = (inp, msg) => { fieldErr(q("#cz-err"), null, msg); mask.querySelectorAll(".invalid").forEach(e => e.classList.remove("invalid")); if (inp) { inp.classList.add("invalid"); inp.focus(); } return null; };
          const n = q("#cz-name").value.trim();
          if (!zoneNameOK(n)) return err(q("#cz-name"), "名字只能是 1–32 个字母、数字、_、-");
          if (zs.some(x => x.name.toLowerCase() === n.toLowerCase() && (!z || x.name !== z.name))) return err(q("#cz-name"), `已经有一个叫「${n}」的区域`);
          const dim = q("#cz-dim").value || q("#cz-dim-custom").value.trim();
          if (!dimOK(dim)) return err(q("#cz-dim-custom"), "维度要写完整的 ID，比如 minecraft:overworld");
          const pts = [];
          for (const id of ["a", "b"]) {
            const c = [];
            for (const k of ["x", "y", "z"]) {
              const inp = q(`#cz-${id}${k}`), e = coordErr(k, inp.value);
              if (e) return err(inp, `${id === "a" ? "角 1" : "角 2"} 的 ${k}：${e}`);
              c.push(+inp.value.trim());
            }
            pts.push(c);
          }
          return { name: n, dim, min: pts[0], max: pts[1] };
        } });
      if (!res || stale(gen)) return;
      const next = zs.filter(x => !z || x.name !== z.name).concat(res);
      await saveZones(next, z ? `已保存区域「${res.name}」` : `已添加禁挖区域「${res.name}」`);
    };
    const delZone = async n => {
      const r = await confirmModal({ title: `删除禁挖区域「${n}」？`, body: "删掉以后伙伴就可以拆、放这片区域里的方块了。", okText: "删除", danger: true, icon: "trash" });
      if (!r || stale(gen)) return;
      await saveZones((v.settings.zones || []).filter(z => z.name !== n), `已删除区域「${n}」`);
    };

    /* ---- 伙伴：连接状态 + 列表（刷出、停下、移除、全部停下） ---- */
    const drawLink = () => {
      const box = $("#cp-link"), l = v.link;
      if (!sig(box, [l.state, l.mod, l.playerEngine, l.error, l.busy, v.serverStatus])) return;
      const [cls, label] = LINK[l.state] || LINK.connecting;
      let sub = "";
      switch (l.state) {
        case "off": sub = "在上面「设置」里打开 AI 伙伴。"; break;
        case "waiting": sub = v.serverStatus === "stopped" ? "开服以后（Done 之后伙伴模组才打开端口），MineDeck 会自动连上。"
          : v.serverStatus === "stopping" ? "服务器正在关闭，伙伴留在存档里，下次开服后自动连上。" : "服务器还在启动，开好以后自动连上。"; break;
        case "live": sub = `伙伴模组 ${esc(l.mod)} · PlayerEngine ${esc(l.playerEngine)}${l.rttMs ? ` · 心跳 ${num(l.rttMs)} 毫秒` : ""}`; break;
        default: sub = esc(l.error || "") + (l.busy ? "（别的程序占着伙伴模组的控制连接，比如测试工具、另一台电脑上的 MineDeck）" : "");
      }
      box.innerHTML = group(riRow({ ico: "plug", titleHTML: `<span>和伙伴模组的连接</span>${pill(cls, label)}`, sub }), { ico: true, cls: "cp-linkbox" });
    };
    const compRow = c => {
      const [cls, label] = COMP_STATE[c.state] || COMP_STATE.busy;
      const lines = [`${icon("heart")} ${num(c.health)}/${num(c.maxHealth)} · ${esc(dimName(c.dim))} <span class="cp-xyz">${num(c.x)}, ${num(c.y)}, ${num(c.z)}</span>`];
      if (c.task) lines.push(`正在${esc(taskTitle(c.task))}<span class="cp-dim">（任务 ${c.task.id}）</span>`);
      if (c.lastResult) {
        const t = c.lastResult.task;
        lines.push(`<span class="cp-dim">上次：${esc(t ? taskTitle(t) : "任务")} ${esc(resultWord(c.lastResult.result))}${c.lastResult.detail ? "（" + esc(c.lastResult.detail) + "）" : ""}${c.lastResult.at ? " · " + esc(when(c.lastResult.at)) : ""}</span>`);
      }
      return riRow({ ico: "bot", cls: "cp-comp", attrs: `data-comp="${c.id}"`,
        titleHTML: `<span>${esc(c.name)}</span>${pill(cls, label)}${c.owner ? `<span class="badge">主人 ${esc(c.owner)}</span>` : ""}`,
        sub: lines.map(l => `<p>${l}</p>`).join(""),
        right: (c.task ? `<button class="btn sm" data-cstop="${c.id}">${icon("stop")} 停下</button>` : "") +
          `<button class="btn sm ghost danger" data-cdel="${c.id}">移除</button>` });
    };
    const drawList = () => {
      const box = $("#cp-list"), live = v.link.state === "live", list = v.companions || [];
      if (!sig(box, [list, live, v.install.needed, v.settings.maxCompanions])) return;
      const rows = list.map(compRow);
      if (!list.length) rows.push(riRow({ ico: "bot", title: live ? "还没有伙伴" : "连上伙伴模组以后，伙伴会列在这里",
        sub: live ? "点「刷出伙伴」叫一个出来，给它起个好记的名字，玩家就能用「@名字」叫它。" : "伙伴存在世界里：关服、断开连接都不会让它消失。" }));
      const primary = !v.install.needed ? " primary" : ""; // 一屏只有一个实底主按钮：要安装的时候让给「安装」
      rows.push(riRow({ ico: "plus", title: `刷出伙伴（${list.length}/${v.settings.maxCompanions}）`, sub: live ? "伙伴不占玩家名额，也不在 Tab 列表里。" : "要先开服、打开 AI 伙伴，等连上伙伴模组。",
        right: `<button class="btn${primary}" id="cp-spawn"${live ? "" : " disabled"}>${icon("plus")} 刷出伙伴</button>` +
          `<button class="btn" id="cp-stopall"${live && list.some(c => c.task) ? "" : " disabled"}>${icon("stop")} 全部停下</button>` }));
      box.innerHTML = group(rows.join(""), { ico: true, cls: "cp-comps" });
      const sp = $("#cp-spawn");
      if (!sp.disabled) sp.onclick = spawn;
      const sa = $("#cp-stopall");
      if (!sa.disabled) sa.onclick = () => stopTasks({ all: true }, "所有伙伴都停下了");
      box.querySelectorAll("[data-cstop]").forEach(b => b.onclick = () => stopTasks({ companion: +b.dataset.cstop }, "已停下"));
      box.querySelectorAll("[data-cdel]").forEach(b => b.onclick = () => despawn(list.find(c => c.id === +b.dataset.cdel)));
    };
    const stopTasks = async (payload, msg) => {
      try { const r = await post("stop", payload); toast(r.stopped.length ? msg : "本来就没有在做的任务"); } catch (e) { toast(e.message, true); }
    };
    const despawn = async c => {
      if (!c) return;
      const r = await confirmModal({ title: `移除伙伴「${c.name}」？`, danger: true, okText: "移除", icon: "trash",
        body: "伙伴会从世界里消失。默认背包里的东西一起消失；想留下来，勾上下面这项。",
        extra: `<label class="switch" style="margin-bottom:16px"><input type="checkbox" data-k="drop"><span class="sw"></span><span class="sw-label">背包里的东西掉在原地</span></label>` });
      if (!r || stale(gen)) return;
      try { await post("despawn", { companion: c.id, drop: !!r.drop }); toast(`已移除伙伴「${c.name}」`); } catch (e) { toast(e.message, true); }
    };
    const spawn = async () => {
      const online = v.online || [];
      const res = await formModal({ title: "刷出伙伴", okText: "刷出",
        body: `<div class="m-body">伙伴会出生在主人旁边（主人不在线就在世界出生点），也可以指定出生在某个在线玩家旁边。</div>
          <div class="cp-fields">
            <label class="cp-f"><span>名字</span><input type="text" id="cs-name" maxlength="16" spellcheck="false" placeholder="如 小方" aria-describedby="cs-err"></label>
            <label class="cp-f"><span>主人</span><input type="text" id="cs-owner" maxlength="20" spellcheck="false" list="cs-online" placeholder="游戏名，可以不填" value="${esc(online.length === 1 ? online[0] : "")}"></label>
            <datalist id="cs-online">${online.map(n => `<option value="${esc(n)}">`).join("")}</datalist>
            <label class="cp-f"><span>出生在</span><select id="cs-near"><option value="">主人旁边</option>${online.map(n => `<option value="${esc(n)}">${esc(n)} 旁边</option>`).join("")}</select></label>
          </div><div class="field-err" id="cs-err"></div>`,
        check: mask => {
          const q = s => mask.querySelector(s);
          const err = (inp, msg) => { fieldErr(q("#cs-err"), null, msg); mask.querySelectorAll(".invalid").forEach(e => e.classList.remove("invalid")); if (inp) { inp.classList.add("invalid"); inp.focus(); } return null; };
          const n = q("#cs-name").value.trim(), owner = q("#cs-owner").value.trim(), near = q("#cs-near").value;
          const bad = nameErr(n);
          if (bad) return err(q("#cs-name"), bad);
          if ((v.companions || []).some(c => c.name.toLowerCase() === n.toLowerCase())) return err(q("#cs-name"), `已经有一个叫「${n}」的伙伴`);
          if (owner && !playerOK(owner)) return err(q("#cs-owner"), "主人的游戏名只能是 1–20 个字母、数字、_、.");
          return { name: n, owner, near };
        } });
      if (!res || stale(gen)) return;
      try {
        const c = await post("spawn", res);
        toast(`刷出了伙伴「${c.name}」：玩家在聊天里说「@${c.name} 跟着我」试试`);
      } catch (e) { toast(e.message, true); }
    };

    /* ---- 最近的事件 ---- */
    const drawEvents = () => {
      const box = $("#cp-events"), evs = (v.events || []).slice(0, 30);
      if (!sig(box, evs)) return;
      box.innerHTML = group(evs.length ? evs.map(e => riRow({ ico: e.level === "warn" ? "warn" : "info", cls: "cp-ev" + (e.level === "warn" ? " warn" : ""),
        titleHTML: `<span>${esc(e.text)}</span>`, sub: `<span class="num">${esc(when(e.t))}</span>` })).join("")
        : riRow({ ico: "info", title: "还没有事件", sub: "连上伙伴模组、刷出伙伴、伙伴开始和做完任务时，这里会记一笔。" }), { ico: true, cls: "cp-evs" });
    };

    /* 随状态变的几块（按签名比对，没变不重绘：开关、输入框不丢焦点） */
    const drawLive = () => { drawGate(); drawInstall(); drawClient(); drawSetLive(); drawZones(); drawLink(); drawList(); drawEvents(); };
    drawSettings();
    drawLive();
    startPoll(refresh, 3000, gen);
  }

  return { applies, render };
})();
