/* MineDeck 游戏内 AI（第四阶段，设计文档 §6、§9 第 4 条）：实例详情的「游戏内 AI」标签。
   玩家在聊天里用「@AI 问题」提问，MineDeck 用 tellraw 回复（默认全服可见，问题前加 /私 只回提问的人）。这一页按 §9 的顺序：
   开关和触发前缀、能让 AI 执行命令的玩家（没开白名单时提醒，可一键开启）、命令模板、限流和额度、最近的问答和执行记录、识别测试和高级识别。
   保存语义（MASTER §11）：开关和勾选即时生效；前缀、别名、各项限流、自定义正则是单字段保存（没改时保存键置灰，回车即保存）。
   局域网打开时只能看、能做识别测试（改设置仅本机）。
   在 app.js 之前加载；用到的 app.js 工具（api、esc、icon、ICON、toast、confirmModal、startPoll、stale、bindSaveInput、group、riRow、
   skeletonRows、fmtTokens、fmtCost）都在调用时才取。app.js 里只留了两处钩子：详情页的标签和分派。 */
"use strict";
window.MDGameChat = (() => {
  /* 有「游戏内 AI」标签的实例：群组代理端没有游戏聊天 */
  const applies = info => info.kind !== "proxy";

  /* 新增的图标（16 视框、细线，同 app.js 的 ICON），第一次用时并进 ICON */
  const ICONS = {
    chat: '<path d="M2.6 3.4h10.8v7.2H7.6L4.8 13v-2.4H2.6z"/><path d="M5.4 6.3h5.2M5.4 8.3h3.4"/>',
    terminal: '<rect x="2.3" y="3" width="11.4" height="10" rx="1.6"/><path d="M5 6.4l2 1.6-2 1.6M8.4 10h2.6"/>',
    shield: '<path d="M8 2.2l5 1.8v3.7c0 2.9-2.1 5.2-5 6.1-2.9-.9-5-3.2-5-6.1V4z"/><path d="M5.9 8.1l1.5 1.5 2.8-3"/>',
  };
  let iconsReady = false;
  const ensureIcons = () => { if (!iconsReady) { Object.assign(ICON, ICONS); iconsReady = true; } };

  /* 最近的问答里每种记录的说法 */
  const KIND = { ask: "提问", help: "帮助", quota: "额度", unknown: "未知选项", nospace: "选项后没空格", misplaced: "选项放错位置",
    cooldown: "冷却中", limit: "次数或额度用完", busy: "正忙", silenced: "刷屏静默", disabled: "AI 未配置", failed: "没答上" };
  const SAMPLE = "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: [Not Secure] <Steve> @AI /私 今晚怎么打末影龙";
  const PATTERN_EXAMPLE = "^\\[\\d{2}:\\d{2}:\\d{2} INFO\\]: \\[\\w+\\] (?P<name>\\w+): (?P<msg>.*)$"; // 插件把聊天改成「[头衔] 名字: 内容」时
  const FORMAT = { forge: "Forge / NeoForge", vanilla: "原版 / Fabric / Quilt", paper: "Paper / Purpur", custom: "自定义正则", "": "认不出" };

  /* 名字的检查，同后端（chatai 的 validSender 放宽后的规则）：1–16 个可打印的 ASCII 字符，不含空格和 < >，不以 @ 开头，不含 " \ */
  const nameErr = n => {
    if (!n) return "先填玩家名";
    if ([...n].length > 16) return "玩家名最多 16 个字";
    if (n.startsWith("@")) return "以 @ 开头的名字不行（写进命令会变成目标选择器）";
    if (/["\\]/.test(n)) return "名字里不能有 \" 或 \\";
    if (/[^\x21-\x7e]|[<>]/.test(n)) return "名字只能是英文字母、数字和半角符号（不能有中文、空格和 < >）";
    return "";
  };
  const validWL = n => /^[A-Za-z0-9_]{1,16}$/.test(n); // 白名单只收正版名字的写法（同 inst.PlayerAction）
  const when = at => {
    const d = new Date(at);
    if (isNaN(d)) return "";
    const p = n => String(n).padStart(2, "0");
    const hm = `${p(d.getHours())}:${p(d.getMinutes())}`;
    return d.toDateString() === new Date().toDateString() ? hm : `${d.getMonth() + 1} 月 ${d.getDate()} 日 ${hm}`;
  };
  const head = (title, sub) => `<div class="gc-head"><h3>${esc(title)}</h3>${sub ? `<div class="sub">${sub}</div>` : ""}</div>`;
  const fieldErr = (box, inp, msg) => { box.innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; if (inp) inp.classList.toggle("invalid", !!msg); };
  const silent = e => Object.assign(e instanceof Error ? e : new Error(e), { silent: true });
  const swHTML = (attrs, on, label, dis) => `<label class="switch" title="${esc(label)}"><input type="checkbox" ${attrs} ${on ? "checked" : ""} aria-label="${esc(label)}"${dis}><span class="sw"></span></label>`;

  /* 四项限流（§6.7）：单字段保存；budget 在界面上按万 token 填 */
  const LIMITS = [
    { k: "cooldown", title: "每人冷却", unit: "秒", sub: "同一个人问完一次，隔多久才能再问" },
    { k: "perPlayer", title: "每人每天", unit: "次", sub: "每个人每天最多问几次" },
    { k: "perServer", title: "全服每天", unit: "次", sub: "这台服务器所有人每天合计最多问几次" },
    { k: "budget", title: "全服每天的 token", unit: "万 token", sub: "这台服务器每天最多用多少 token（输入、输出都算）", scale: 10000 },
  ];

  async function render(body, info, gen) {
    ensureIcons();
    const base = `/api/instances/${encodeURIComponent(info.name)}/gameai`;
    body.innerHTML = `<div class="gc">${skeletonRows(6)}</div>`;
    let v;
    try { v = await api(base); } catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    const ro = !v.local, dis = ro ? " disabled" : "";
    const st = { authName: "", advOpen: false };
    body.innerHTML = `<div class="gc" id="gc">
      <div id="gc-gate"></div>
      ${head("游戏内 AI", "玩家在聊天里用「前缀 + 问题」提问，回复默认全服都看得到；问题前加 /私 只回提问的人，加 /帮助 看全部用法。")}
      <div id="gc-main"></div>
      ${head("能让 AI 执行命令的玩家", "只有勾选的人能让 AI 执行下面打开了的命令，其他人只能提问。按名字授权：离线模式下，名字能被别人冒用。")}
      <div id="gc-warn"></div>
      <div id="gc-auth"></div>
      ${head("命令模板", "AI 只能执行打开了的这几类命令，每个参数都按模板检查，「自己」由程序换成提问的人。op、deop、ban、stop、execute 这类命令永远不允许，这里也打不开。")}
      <div id="gc-tpl"></div>
      ${head("限流和额度", "填 0 表示不限。不调用 AI 的回复（/帮助、/额度、选项写错）不算次数，但同一个人 1 分钟内触发超过 5 次会被静默 10 分钟；同一时间只回答 1 个问题，最多排队 3 个。")}
      <div id="gc-lim"></div>
      ${head("最近的问答和执行记录", "游戏里每条回复最多 3 条消息，太长的截掉了；这里是完整的回答。只保存最近 100 条，存在这台服务器的目录里。")}
      <div id="gc-log"></div>
      ${head("识别测试", "贴一行服务器日志，看 MineDeck 认出的玩家、正文和选项，以及会怎么处理。用的是上面填的前缀和下面的高级识别（没保存的也算），不会发任何东西。")}
      <div id="gc-test"></div>
      <div id="gc-adv"></div>
    </div>`;

    const put = async patch => {
      v = await api(base, { method: "PUT", body: patch });
      if (!stale(gen)) drawLive();
      return v;
    };

    /* ---- 顶部提示：AI 没配置；局域网只能看 ---- */
    const drawGate = () => {
      const box = $("#gc-gate");
      const sig = JSON.stringify([v.ai.enabled, ro]);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      let html = "";
      if (!v.ai.enabled) html += `<div class="warn-card"><div class="wc-title">${icon("warn")} AI 还没配置</div>
        <div class="wc-note">游戏内 AI 要先在「设置 → AI」里选好服务商、填好 Key 和模型。没配置时，有人 @AI 提问只会私下收到「主机还没配置 AI」。</div>
        ${ro ? "" : `<a class="btn sm" href="#/settings/ai">${icon("gear")} 去设置</a>`}</div>`;
      if (ro) html += `<div class="gc-note">${icon("lock")} 局域网打开：只能看设置和最近的问答、做识别测试；改设置要在运行 MineDeck 的那台电脑上。</div>`;
      box.innerHTML = html;
    };

    /* ---- 开关、前缀、别名（输入框只画一次，不随轮询重建）；「提问会发给」那一行随状态更新 ---- */
    const drawMain = () => {
      const s = v.settings;
      $("#gc-main").innerHTML = group(
        riRow({ ico: "sparkle", title: "打开游戏内 AI", sub: "关着时 MineDeck 不看聊天，也不会回复任何人。", right: swHTML(`id="gc-on"`, s.enabled, "打开游戏内 AI", dis) }) +
        riRow({ ico: "chat", title: "触发前缀", sub: "聊天以它开头才会触发，比如「@AI 今晚怎么打末影龙」。不分大小写，也认全角的 ＠。",
          field: `<input type="text" id="gc-prefix" value="${esc(s.prefix)}" maxlength="16" spellcheck="false" aria-label="触发前缀"${dis}><button class="btn" id="gc-prefix-save"${dis}>保存</button>`,
          after: `<div class="field-err ri-err" id="gc-prefix-err"></div>` }) +
        riRow({ ico: "chat", title: "别名（可以不填）", sub: "和前缀一样能触发，比如 @小助手；几个用逗号隔开。",
          field: `<input type="text" id="gc-alias" value="${esc((s.aliases || []).join("，"))}" maxlength="200" spellcheck="false" placeholder="如 @小助手" aria-label="别名"${dis}><button class="btn" id="gc-alias-save"${dis}>保存</button>`,
          after: `<div class="field-err ri-err" id="gc-alias-err"></div>` }) +
        riRow({ ico: "globe", titleHTML: `<span id="gc-prov"></span>`, sub: `<span id="gc-today"></span>` }),
        { ico: true });
      $("#gc-on").onchange = async e => {
        const on = e.target.checked;
        try {
          await put({ enabled: on });
          toast(on ? `已打开游戏内 AI：玩家在聊天里说「${v.settings.prefix} 问题」就能问` : "已关闭游戏内 AI");
        } catch (err) { e.target.checked = !on; toast(err.message, true); }
      };
      const pErr = msg => fieldErr($("#gc-prefix-err"), $("#gc-prefix"), msg);
      $("#gc-prefix").addEventListener("input", () => pErr(""));
      bindSaveInput($("#gc-prefix"), $("#gc-prefix-save"), async val => {
        const p = val.trim();
        if (!p) { pErr("前缀不能是空的"); throw silent("前缀是空的"); }
        try { await put({ prefix: p }); } catch (e) { pErr(e.message); throw silent(e); }
        $("#gc-prefix").value = v.settings.prefix;
        toast(`触发前缀改成了 ${v.settings.prefix}`);
      }, gen);
      const aErr = msg => fieldErr($("#gc-alias-err"), $("#gc-alias"), msg);
      $("#gc-alias").addEventListener("input", () => aErr(""));
      bindSaveInput($("#gc-alias"), $("#gc-alias-save"), async val => {
        const list = val.split(/[,，、;；\s]+/).map(x => x.trim()).filter(Boolean);
        try { await put({ aliases: list }); } catch (e) { aErr(e.message); throw silent(e); }
        $("#gc-alias").value = (v.settings.aliases || []).join("，");
        toast(list.length ? `别名：${v.settings.aliases.join("、")}` : "已清空别名");
      }, gen);
    };
    const drawStatus = () => {
      const a = v.ai, t = v.today;
      $("#gc-prov").textContent = a.enabled ? `提问会发给${/^[\x00-\x7f]/.test(a.provider) ? " " : ""}${a.provider}${a.model ? `（${a.model}）` : ""}` : "AI 还没配置";
      const used = `今天全服问了 ${t.count} 次，用了 ${fmtTokens(t.tokens)} token。`;
      $("#gc-today").textContent = a.enabled
        ? `${used}这次开服第一次有人提问时，会在游戏里告诉大家一次提问会发给谁。`
        : `${used}${a.missing ? `还差：${a.missing}。` : ""}`;
    };

    /* ---- 授权：每个人一个开关；没开白名单时提醒，可一键开启 ---- */
    const drawWarn = () => {
      const box = $("#gc-warn"), allowed = v.settings.allowed || [];
      const who = st.authName || allowed[0] || "";
      const sig = JSON.stringify([v.whitelistOn, who, allowed.length, ro]);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      box.innerHTML = !v.whitelistOn && allowed.length ? `<div class="warn-card" id="gc-wl-warn"><div class="wc-title">${icon("warn")} 这个服没开白名单</div>
        <div class="wc-note">离线模式下，任何人用 ${esc(who)} 的名字进服，都能让 AI 执行你勾选的命令。开白名单可以挡住不知道名字的陌生人（比如扫到穿透地址的人）；不开也照样能授权。</div>
        <button class="btn sm" id="gc-wl"${dis}>${icon("shield")} 一键开启白名单</button></div>` : "";
      const b = $("#gc-wl");
      if (b) b.onclick = enableWhitelist;
    };
    const enableWhitelist = async () => {
      const cand = v.players.filter(p => !p.whitelist && (p.online || p.seen || p.allowed || p.name === st.authName));
      const ok = cand.filter(p => validWL(p.name)), bad = cand.filter(p => !validWL(p.name));
      const listed = v.players.filter(p => p.whitelist).map(p => p.name);
      const extra = `<div class="gc-wl-list">${ok.map(p => `<label class="gc-wl-item"><input type="checkbox" data-k="${esc(p.name)}" checked>
          <span>${esc(p.name)}</span>${p.online ? `<span class="badge">在线</span>` : ""}${p.allowed ? `<span class="badge">已授权</span>` : ""}${!p.online && p.seen ? `<span class="badge">最近来过</span>` : ""}</label>`).join("") ||
          `<div class="sub">在线的、最近来过的、授权了的人都已经在白名单里了。</div>`}
        ${bad.length ? `<div class="hint">${icon("warn")} ${esc(bad.map(p => p.name).join("、"))} 的名字不是 1–16 位字母、数字、下划线，加不进白名单。</div>` : ""}
        ${listed.length ? `<div class="hint">已经在白名单里：${esc(listed.join("、"))}</div>` : ""}</div>`;
      const r = await confirmModal({ title: "一键开启白名单", okText: "加进白名单并开启", icon: "shield", extra,
        body: `先把勾选的人加进白名单，再开启白名单，免得把好友关在外面。${info.status === "running"
          ? "服务器正在运行：会发送 whitelist add 和 whitelist on。"
          : "服务器停着：直接改名单文件，并把 server.properties 的 white-list 改成 true。"}` });
      if (!r || stale(gen)) return;
      const players = ok.filter(p => r[p.name]).map(p => p.name);
      try {
        v = await api(base + "/whitelist", { method: "POST", body: { players } });
        if (stale(gen)) return;
        drawLive();
        toast(players.length ? `已把 ${players.join("、")} 加进白名单，并开启了白名单` : "已开启白名单");
      } catch (e) { toast(e.message, true); }
    };
    const badges = p => [p.online && "在线", p.whitelist && "白名单", p.op && "管理员", !p.online && p.seen && "最近来过"]
      .filter(Boolean).map(t => `<span class="badge">${t}</span>`).join("");
    const drawAuth = () => {
      const box = $("#gc-auth");
      const sig = JSON.stringify([v.players, ro]);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      const focused = document.activeElement && box.contains(document.activeElement) ? document.activeElement.dataset.allow : null;
      const typing = $("#gc-add-name") ? $("#gc-add-name").value : "";
      const rows = v.players.map(p => riRow({ ico: "player", titleHTML: `${esc(p.name)}<span class="gc-badges">${badges(p)}</span>`,
        right: swHTML(`data-allow="${esc(p.name)}"`, p.allowed, `允许 ${p.name} 让 AI 执行命令`, dis) }));
      if (!v.players.length) rows.push(riRow({ ico: "player", title: "还没有可以授权的玩家", sub: "服务器开着时，在线的人会列在这里；白名单、管理员和最近来过的人也会列出来。也可以在下面直接填名字。" }));
      rows.push(riRow({ ico: "plus", title: "按名字授权", sub: "列表里没有的人（比如还没进过服的好友）",
        field: `<input type="text" id="gc-add-name" maxlength="16" spellcheck="false" placeholder="玩家名，如 Steve" aria-label="按名字授权"${dis}><button class="btn" id="gc-add"${dis}>授权</button>`,
        after: `<div class="field-err ri-err" id="gc-add-err"></div>` }));
      box.innerHTML = group(rows.join(""), { ico: true });
      $("#gc-add-name").value = typing;
      box.querySelectorAll("[data-allow]").forEach(c => c.onchange = () => setAllowed(c.dataset.allow, c.checked, c));
      const addErr = msg => fieldErr($("#gc-add-err"), $("#gc-add-name"), msg);
      const add = async () => {
        const n = $("#gc-add-name").value.trim(), bad = nameErr(n);
        addErr(bad);
        if (bad) return;
        if ((v.settings.allowed || []).some(x => x.toLowerCase() === n.toLowerCase())) { addErr(`${n} 已经授权了`); return; }
        if (await setAllowed(n, true)) $("#gc-add-name").value = "";
      };
      $("#gc-add").onclick = add;
      $("#gc-add-name").onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); add(); } };
      $("#gc-add-name").addEventListener("input", () => addErr(""));
      if (focused) box.querySelector(`[data-allow="${CSS.escape(focused)}"]`)?.focus();
    };
    const setAllowed = async (name, on, box) => {
      const cur = (v.settings.allowed || []).filter(x => x.toLowerCase() !== name.toLowerCase());
      if (on) cur.push(name);
      try {
        if (on) st.authName = name;
        await put({ allowed: cur });
        toast(on ? `已允许 ${name} 让 AI 执行命令${v.whitelistOn ? "" : "（这个服没开白名单，见上面的提醒）"}` : `已收回 ${name} 的执行权限`);
        return true;
      } catch (e) { if (box) box.checked = !on; toast(e.message, true); return false; }
    };

    /* ---- 命令模板：开关即时生效 ---- */
    const drawTpl = () => {
      const box = $("#gc-tpl"), s = v.settings;
      const sig = JSON.stringify([s.templates, ro]);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      box.innerHTML = group(v.templates.map(t => riRow({ ico: "terminal",
        titleHTML: `${esc(t.title)}${t.default ? "" : `<span class="tag neutral">默认关</span>`}`, sub: esc(t.limit), key: t.usage,
        right: swHTML(`data-tpl="${esc(t.id)}"`, !!s.templates[t.id], t.title, dis) })).join(""), { ico: true });
      box.querySelectorAll("[data-tpl]").forEach(c => c.onchange = async () => {
        const t = v.templates.find(x => x.id === c.dataset.tpl), on = c.checked;
        try { await put({ templates: { [t.id]: on } }); toast(`${on ? "已打开" : "已关闭"}「${t.title}」`); }
        catch (e) { c.checked = !on; toast(e.message, true); }
      });
    };

    /* ---- 限流和额度：单字段保存 ---- */
    const drawLimits = () => {
      const s = v.settings, d = v.defaults;
      const val = l => (l.scale ? s[l.k] / l.scale : s[l.k]);
      $("#gc-lim").innerHTML = group(LIMITS.map(l => riRow({ ico: "clock", title: l.title,
        sub: `${esc(l.sub)}（默认 ${l.scale ? d[l.k] / l.scale : d[l.k]} ${esc(l.unit)}）`,
        field: `<input type="number" id="gc-l-${l.k}" min="0" step="${l.scale ? "any" : "1"}" inputmode="${l.scale ? "decimal" : "numeric"}" value="${val(l)}" aria-label="${esc(l.title)}（${esc(l.unit)}）"${dis}>` +
          `<span class="ri-unit">${esc(l.unit)}</span><button class="btn" id="gc-l-save-${l.k}"${dis}>保存</button>`,
        after: `<div class="field-err ri-err" id="gc-l-err-${l.k}"></div>` })).join("") +
        riRow({ ico: "coin", title: "游戏内 AI 的每日总额度", sub: `<span id="gc-quota"></span>`, right: ro ? "" : `<a class="btn sm" href="#/settings/ai">去设置</a>` }),
        { ico: true });
      LIMITS.forEach(l => {
        const inp = $(`#gc-l-${l.k}`), err = msg => fieldErr($(`#gc-l-err-${l.k}`), inp, msg);
        inp.addEventListener("input", () => err(""));
        bindSaveInput(inp, $(`#gc-l-save-${l.k}`), async raw => {
          const n = raw.trim() === "" ? 0 : +raw;
          if (!(n >= 0) || (!l.scale && !Number.isInteger(n))) { err(`请填不小于 0 的${l.scale ? "数" : "整数"}（单位：${l.unit}）`); throw silent("不合法"); }
          try { await put({ [l.k]: l.scale ? Math.round(n * l.scale) : n }); } catch (e) { err(e.message); throw silent(e); }
          toast(n ? `${l.title}：${n} ${l.unit}` : `${l.title}：不限`);
        }, gen);
      });
    };
    const drawQuota = () => {
      const a = v.ai, el = $("#gc-quota");
      if (el) el.textContent = `「设置 → AI」里给游戏内 AI 设的每日额度，所有服务器合计：今天用了 ${fmtTokens(a.quotaUsed)} token，` +
        (a.quotaLimit ? `上限 ${fmtTokens(a.quotaLimit)}。` : "没有上限。") + "用完了所有服务器的游戏内 AI 都停到第二天。";
    };

    /* ---- 最近的问答和执行记录 ---- */
    const recRow = r => {
      const tags = [`<span class="tag${r.kind === "failed" || r.kind === "silenced" ? " warn" : r.kind === "ask" ? "" : " neutral"}">${esc(KIND[r.kind] || r.kind)}</span>`];
      for (const o of r.opts || []) tags.push(`<span class="badge">/${esc(o)}</span>`);
      if (r.exec) tags.push(`<span class="badge">可执行命令</span>`);
      const lines = [];
      if (r.question) lines.push(`<p><b>问</b>${esc(r.question)}</p>`);
      if (r.reply) lines.push(`<p><b>${r.kind === "ask" ? "答" : "回"}</b>${esc(r.reply)}${r.private ? `<span class="gc-dim">（只发给 ${esc(r.player)}）</span>` : ""}${r.cut ? `<span class="gc-dim">（游戏里截断了）</span>` : ""}</p>`);
      for (const c of r.commands || []) {
        lines.push(`<p class="gc-cmd ${c.ok ? "ok" : "bad"}">${icon(c.ok ? "check" : "x")}<code>/${esc(c.run || c.command)}</code>${c.ok ? "" : `<span>没执行：${esc(c.reason || "")}</span>`}</p>`);
        if (c.ok && c.output) lines.push(`<pre class="gc-out">${esc(c.output)}</pre>`);
      }
      if (r.err) lines.push(`<p class="gc-err">${esc(r.err)}</p>`);
      if (r.note) lines.push(`<p class="gc-dim">${esc(r.note)}</p>`);
      if (r.usage) lines.push(`<p class="gc-dim num">用了 ${fmtTokens((r.usage.input || 0) + (r.usage.output || 0) + (r.usage.cacheRead || 0) + (r.usage.cacheWrite || 0))} token${r.cost ? " · " + fmtCost(r.cost) : ""}</p>`);
      return riRow({ ico: r.kind === "ask" || r.kind === "failed" ? "sparkle" : "chat", cls: "gc-rec",
        titleHTML: `<span>${esc(r.player)}</span><span class="gc-when num">${esc(when(r.t))}</span>${tags.join("")}`, sub: lines.join("") });
    };
    const drawLog = async () => {
      let list;
      try { list = await api(base + "/log?limit=50"); } catch (e) { if (!stale(gen) && !$("#gc-log").dataset.sig) $("#gc-log").innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
      if (stale(gen)) return;
      const box = $("#gc-log"), sig = JSON.stringify(list);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      box.innerHTML = group(list.length ? list.map(recRow).join("") :
        riRow({ ico: "chat", title: "还没有人问过", sub: `打开游戏内 AI 以后，在游戏里说「${esc(v.settings.prefix)} 你好」试试。` }), { ico: true, cls: "gc-log" });
    };

    /* ---- 识别测试：用表单上的前缀、别名和高级识别（没保存的也算） ---- */
    const drawTest = () => {
      $("#gc-test").innerHTML = group(riRow({ ico: "search", title: "一行服务器日志",
        sub: "从控制台复制一行玩家的聊天（带前面的时间和线程），或者点「填入示例」。",
        field: `<input type="text" id="gc-line" class="gc-mono" spellcheck="false" placeholder="${esc(SAMPLE)}" aria-label="一行服务器日志">` +
          `<button class="btn ghost" id="gc-sample">填入示例</button><button class="btn" id="gc-run">${icon("search")} 识别</button>`,
        after: `<div class="field-err ri-err" id="gc-test-err"></div><div id="gc-test-out"></div>` }), { ico: true });
      $("#gc-sample").onclick = () => { $("#gc-line").value = SAMPLE; $("#gc-line").focus(); };
      $("#gc-run").onclick = runTest;
      $("#gc-line").onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); runTest(); } };
    };
    const overrides = () => {
      const o = {};
      const p = $("#gc-prefix").value.trim();
      if (p) o.prefix = p;
      o.aliases = $("#gc-alias").value.split(/[,，、;；\s]+/).map(x => x.trim()).filter(Boolean);
      const pat = $("#gc-pattern");
      if (pat) o.pattern = pat.value.trim();
      return o;
    };
    const runTest = async () => {
      const line = $("#gc-line").value.trim(), err = msg => fieldErr($("#gc-test-err"), $("#gc-line"), msg);
      const out = $("#gc-test-out");
      if (!line) { err("先贴一行日志"); out.innerHTML = ""; return; }
      err("");
      let r;
      try { r = await api(base + "/test", { method: "POST", body: { line, ...overrides() } }); }
      catch (e) { err(e.message); out.innerHTML = ""; return; }
      if (stale(gen)) return;
      const a = r.ask, kv = [["日志格式", FORMAT[r.format] ?? r.format], r.thread && ["线程", r.thread]];
      if (r.chat) {
        kv.push(["玩家", `${esc(r.player)} ${r.online ? `<span class="badge">在线</span>` : `<span class="badge">不在线</span>`}${r.authorized ? `<span class="badge">能执行命令</span>` : ""}`, true]);
        kv.push(["正文", r.message]);
      }
      if (a) {
        kv.push(["前缀", a.prefix]);
        kv.push(["选项", (a.opts || []).length ? a.opts.map(o => "/" + o).join(" ") : "没有"]);
        if (a.question) kv.push(["问题", a.question]);
      }
      out.innerHTML = `<dl class="gc-kv">${kv.filter(Boolean).map(([k, val, html]) => `<dt>${esc(k)}</dt><dd>${html ? val : esc(val)}</dd>`).join("")}</dl>
        <div class="gc-result ${a ? "hit" : "miss"}">${icon(a ? "check" : "info")}<span>${esc(r.result)}</span></div>`;
    };

    /* ---- 高级识别：放宽名字、不看线程名、自定义正则（收起时只显示标题行） ---- */
    const drawAdv = () => {
      const s = v.settings, box = $("#gc-adv");
      box.innerHTML = group(
        riRow({ ico: "gear", title: "高级识别", sub: "认不出聊天时再来改：放宽名字规则、不看线程名、自定义正则。",
          right: `<span class="ri-chev">${icon("chevronDown")}</span>`, cls: "expandable" + (st.advOpen ? " open" : ""),
          attrs: `id="gc-adv-head" role="button" tabindex="0" aria-expanded="${st.advOpen}"` }) +
        riRow({ ico: "player", title: "放宽名字规则", cls: "gc-adv-row",
          sub: "默认只认 1–16 位字母、数字、下划线。离线服的名字带 - . 这类字符时打开；以 @ 开头、含 \" \\ 或中文的名字照样不认（没法安全地写进命令）。",
          right: swHTML(`id="gc-loose"`, s.looseNames, "放宽名字规则", dis) }) +
        riRow({ ico: "terminal", title: "不看线程名", cls: "gc-adv-row",
          sub: "默认只认服务器主线程打出的聊天。装了改线程名的模组、聊天认不出来时打开（仍要求是 MinecraftServer 记录的）。",
          right: swHTML(`id="gc-thread"`, s.anyThread, "不看线程名", dis) }) +
        riRow({ ico: "doc", title: "自定义正则（可以不填）", cls: "gc-adv-row",
          sub: "给装了聊天格式插件或模组的服用：用 (?P&lt;name&gt;…) 圈出名字、(?P&lt;msg&gt;…) 圈出聊天内容，对整行日志匹配。留空用内置的规则；改之前先在上面的识别测试里试试。",
          field: `<input type="text" id="gc-pattern" class="gc-mono" value="${esc(s.pattern || "")}" maxlength="500" spellcheck="false" placeholder="如 ${esc(PATTERN_EXAMPLE)}" aria-label="自定义正则"${dis}><button class="btn" id="gc-pattern-save"${dis}>保存</button>`,
          after: `<div class="field-err ri-err" id="gc-pattern-err"></div>` }),
        { ico: true, cls: "gc-adv" + (st.advOpen ? " open" : "") });
      const h = $("#gc-adv-head"), toggle = () => {
        st.advOpen = !st.advOpen;
        h.classList.toggle("open", st.advOpen);
        box.firstElementChild.classList.toggle("open", st.advOpen);
        h.setAttribute("aria-expanded", String(st.advOpen));
      };
      h.onclick = toggle;
      h.onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle(); } };
      const sw = (id, key, onMsg, offMsg) => $(id).onchange = async e => {
        const on = e.target.checked;
        try { await put({ [key]: on }); toast(on ? onMsg : offMsg); } catch (err) { e.target.checked = !on; toast(err.message, true); }
      };
      sw("#gc-loose", "looseNames", "已放宽名字规则", "名字只认字母、数字、下划线");
      sw("#gc-thread", "anyThread", "已不看线程名", "只认服务器主线程打出的聊天");
      const pErr = msg => fieldErr($("#gc-pattern-err"), $("#gc-pattern"), msg);
      $("#gc-pattern").addEventListener("input", () => pErr(""));
      bindSaveInput($("#gc-pattern"), $("#gc-pattern-save"), async val => {
        try { await put({ pattern: val.trim() }); } catch (e) { pErr(e.message); throw silent(e); }
        toast(val.trim() ? "已保存自定义正则" : "已清空自定义正则，改用内置的规则");
      }, gen);
    };

    /* 随设置和状态变的几块（按签名比对，没变不重绘：开关、输入框不丢焦点） */
    const drawLive = () => { drawGate(); drawStatus(); drawWarn(); drawAuth(); drawTpl(); drawQuota(); };

    drawMain();
    drawLimits();
    drawTest();
    drawAdv();
    drawLive();
    await drawLog();
    if (stale(gen)) return;
    startPoll(async () => {
      try {
        const nv = await api(base);
        if (stale(gen)) return;
        v = nv;
        drawLive();
      } catch { /* 轮询偶尔失败不打扰，下一拍再试 */ }
      drawLog();
    }, 5000, gen);
  }

  return { applies, render };
})();
