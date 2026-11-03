/* MineDeck 音乐共享（第五阶段，设计文档 §8、§9 第 5 条）：实例详情的「音乐共享」标签，隧道表单的「用途」和「协议」。
   在 app.js 之前加载；用到的 app.js 工具（api、esc、icon、toast、confirmModal、copy、startPoll、stale、bindSaveInput、
   riRow、group、xpBar、skeletonRows、fmtDur）都在调用时才取，加载顺序不影响。app.js 里只留了几处钩子：
   详情页的标签和分派、隧道表单与列表。 */
"use strict";
window.MDMusic = (() => {
  /* 有「音乐共享」标签的核心：桥接模组只有 Forge 系的元数据（NeoForge 1.20.5+、Forge 1.20.1+，版本由后端判断） */
  const CORES = new Set(["neoforge", "forge", "mohist"]);
  const applies = core => CORES.has(core);

  /* 新增的图标（16 视框、细线，同 app.js 的 ICON），第一次用时并进 ICON */
  const ICONS = {
    music: '<path d="M6 12V3.7l7-1.5v8.3"/><circle cx="4.3" cy="12" r="1.8"/><circle cx="11.3" cy="10.5" r="1.8"/>',
    headphones: '<path d="M2.8 10.2V8.3a5.2 5.2 0 0110.4 0v1.9"/><rect x="2.3" y="9.4" width="2.8" height="4.1" rx="1"/><rect x="10.9" y="9.4" width="2.8" height="4.1" rx="1"/>',
    volume: '<path d="M2.6 6.1h2.5L8.4 3.3v9.4L5.1 9.9H2.6z"/><path d="M10.7 5.7a3.1 3.1 0 010 4.6M12.5 4a5.5 5.5 0 010 8"/>',
  };
  let iconsReady = false;
  const ensureIcons = () => { if (!iconsReady) { Object.assign(ICON, ICONS); iconsReady = true; } };

  /* 「我在游戏里的名字」的检查，同后端 bridge.NormalizeExclude：逗号（或分号、空格）隔开，每个最多 36 个字，不能有 \ = # ! : */
  function checkNames(s) {
    for (const n of s.split(/[,，;；\s]+/).filter(Boolean)) {
      if ([...n].length > 36) return `「${n.slice(0, 12)}…」太长了：每个名字最多 36 个字`;
      if (/[\\=#!:\u0000-\u001f]/.test(n)) return `「${n}」里有不能用的字符（\\ = # ! :）`;
    }
    return "";
  }
  const when = at => {
    const d = new Date(at);
    if (isNaN(d)) return "";
    const p = n => String(n).padStart(2, "0");
    const hm = `${p(d.getHours())}:${p(d.getMinutes())}`;
    return d.toDateString() === new Date().toDateString() ? hm : `${d.getMonth() + 1} 月 ${d.getDate()} 日 ${hm}`;
  };
  const pill = (cls, text) => `<span class="st-pill ${cls}"><span class="dot ${cls}"></span><span class="st-label">${esc(text)}</span></span>`;
  const okIco = (ok, name) => `<span class="ri-ico ${ok ? "mu-ok" : "mu-bad"}">${icon(ok ? "check" : name || "warn")}</span>`;
  const names = list => `<div class="mu-names">${list.map(n => `<span class="badge">${esc(n)}</span>`).join("")}</div>`;
  /* 一行（同 app.js 的 riRow，只是行首瓦片可以换成状态瓦片） */
  const row = ({ tile, title = "", titleHTML = "", sub = "", key = "", right = "", cls = "", attrs = "", field = "", after = "" }) =>
    `<div class="row-item${cls ? " " + cls : ""}"${attrs ? " " + attrs : ""}><div class="ri-head">${tile || ""}
      <div class="ri-main"><div class="ri-title">${titleHTML || esc(title)}</div>${sub ? `<div class="ri-sub">${sub}</div>` : ""}${key ? `<div class="ri-key">${esc(key)}</div>` : ""}</div>
      ${right ? `<div class="ri-right">${right}</div>` : ""}</div>${field ? `<div class="ri-field">${field}</div>` : ""}${after}</div>`;
  const tile = name => `<span class="ri-ico">${icon(name)}</span>`;
  const meterCells = peak => Math.min(10, Math.ceil(Math.sqrt(Math.max(0, +peak || 0)) * 10));

  /* ---------- 实例详情的「音乐共享」标签 ----------
     上：广播（选音源 → 开始；正在播时显示音源、听众数、停止）；中：检查清单（核心、SVC、桥接模组、语音端口、远程语音）；
     下：听众（谁连上了语音、谁没装 SVC、谁版本不兼容，复制给好友的说明）、设置（我在游戏里的名字、音量）、高级选项。
     轮询：每秒一拍——在选音源时刷新电平，每 2 拍刷新状态，每 3 拍刷新页头。各块按签名比对，没变不重绘，输入框不丢焦点。 */
  async function render(body, info, gen, refreshHead) {
    ensureIcons();
    const name = info.name, enc = encodeURIComponent(name), base = `/api/instances/${enc}/music`;
    body.innerHTML = `<div class="mu">${skeletonRows(6)}</div>`;
    let st;
    try { st = await api(base); } catch (e) { if (!stale(gen)) body.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; return; }
    if (stale(gen)) return;
    if (!st.supported) {
      body.innerHTML = `<div class="mu"><div class="warn-card"><div class="wc-title">${icon("warn")} 这台服务器用不了音乐共享</div>
        <div class="wc-note">${esc(st.reason)}</div></div>
        <div class="hint">音乐共享靠 Simple Voice Chat（简单语音聊天）和 MineDeck 的桥接模组把音乐推给玩家，现在只支持 NeoForge 1.20.5 以上、Forge 1.20.1 以上的服务器。</div></div>`;
      return;
    }
    const v = { st, src: null, sel: null, mode: "", device: null, busy: false, advOpen: false, srcLoading: false, vhOpen: false };
    body.innerHTML = `<div class="mu" id="mu">
      <div id="mu-cast"></div>
      <div class="mu-head"><h3>检查清单</h3><div class="sub">都打勾了才能广播；装了 SVC 的玩家都能听到，各自在 SVC 里能单独静音。</div></div>
      <div id="mu-restart"></div>
      <div id="mu-check"></div>
      <div class="mu-head"><h3>听众</h3></div>
      <div id="mu-players"></div>
      <div class="mu-head"><h3>设置</h3></div>
      <div id="mu-set"></div>
      <div id="mu-adv-box" style="margin-top:12px"></div>
    </div>`;

    /* ---- 状态和阻碍 ---- */
    const restartHas = what => (v.st.restart || []).includes(what);
    const curMode = () => {
      const cap = v.st.capability || {};
      if (cap.mode !== "process") return cap.mode || "";
      return v.mode || (v.st.settings.mode === "system" ? "system" : "process");
    };
    const blockers = () => {
      const s = v.st, out = [];
      if (!s.svc.installed) out.push("先装 Simple Voice Chat");
      else if (restartHas("Simple Voice Chat")) out.push("装好 Simple Voice Chat 后要重启服务器");
      if (!s.bridge.file) out.push(s.bridge.installed ? "先更新桥接模组" : "先装桥接模组");
      else if (restartHas("桥接模组")) out.push("装好桥接模组后要重启服务器");
      if (s.serverStatus !== "running") out.push(s.serverStatus === "starting" ? "服务器还在启动，等它启动完成" : "先开服");
      if (!curMode()) out.push("这台电脑录不了声音");
      return out;
    };

    /* ---- 广播 ---- */
    const drawCast = () => {
      const s = v.st, box = $("#mu-cast"), b = s.broadcast;
      const shape = b ? "live" : s.elsewhere ? "else:" + s.elsewhere : !s.local ? "remote" : `idle:${curMode()}:${s.capability.warn ? 1 : 0}:${s.systemConfirmed ? 1 : 0}`;
      if (box.dataset.shape !== shape) {
        box.dataset.shape = shape;
        box.innerHTML = b ? liveHTML() : s.elsewhere ? elsewhereHTML(s.elsewhere) : !s.local ? remoteHTML() : idleHTML();
        wireCast();
      }
      if (b) paintLive(); else if (!s.elsewhere && s.local) { paintStart(); drawSources(); }
      const last = $("#mu-last");
      if (last) {
        const l = !b && s.last;
        last.hidden = !l;
        if (l) { last.className = "mu-last" + (l.error ? " err" : ""); last.textContent = `上次广播${when(l.at) ? "在 " + when(l.at) + " " : ""}停了：${l.reason}`; }
      }
    };
    const castHead = sub => `<div class="mu-head"><h3>广播</h3>${sub ? `<div class="sub">${sub}</div>` : ""}</div>`;
    const liveHTML = () => castHead("") + group(
      row({ tile: tile("music"), titleHTML: `<span id="mu-live-title">正在广播</span><span id="mu-live-pill"></span>`, sub: `<span id="mu-live-src"></span>`,
        right: `<button class="btn" id="mu-stop">${icon("stop")} 停止广播</button>` }) +
      row({ tile: tile("headphones"), titleHTML: `<span id="mu-live-lis"></span>`, sub: `<span id="mu-live-net" class="num"></span>` }),
      { ico: true, cls: "mu-live" }) + `<div id="mu-live-warn"></div>`;
    const elsewhereHTML = other => castHead("") + group(row({ tile: tile("music"), title: `正在给「${other}」广播`,
      sub: "同一时间只能有一个广播：先在那边停掉，再回来开始。", right: `<a class="btn sm" href="#/inst/${encodeURIComponent(other)}/music">去看看</a>` }), { ico: true }) +
      `<div class="mu-last" id="mu-last" hidden></div>`;
    const remoteHTML = () => castHead("") + group(row({ tile: tile("music"), title: "开始广播只能在主机上操作",
      sub: "音乐录的是主机这台电脑的声音：到主机上的 MineDeck 里开始。这里可以看状态、停止广播。" }), { ico: true }) +
      `<div class="mu-last" id="mu-last" hidden></div>`;
    const idleHTML = () => {
      const s = v.st, cap = s.capability, m = curMode();
      if (!m) return castHead("") + `<div class="err-box">这台电脑录不了声音（${esc(cap.os || "系统不支持")}）：音乐共享要 Windows 10 2004 或更新的系统。</div>` +
        `<div class="mu-last" id="mu-last" hidden></div>`;
      const start = `<div class="mu-actions"><button class="btn primary" id="mu-start">${icon("play")} ${m === "system" ? "开始广播（录整机声音）" : "开始广播"}</button>
        <span class="mu-why" id="mu-why"></span><span class="grow"></span>${m === "process" ? `<button class="btn sm ghost" id="mu-src-refresh">${icon("refresh")} 刷新列表</button>` : ""}</div>
        <div class="mu-last" id="mu-last" hidden></div>`;
      if (m === "process") return castHead("选一个正在放歌的程序：只录它的声音，游戏声音和语音聊天都不会被录进去。") +
        `<div class="group has-ico" id="mu-src" role="radiogroup" aria-label="音源">${row({ title: "正在找出声的程序…" })}</div>` + start;
      return castHead(cap.warn ? "" : "整机录音：录这台电脑放出来的全部声音。") +
        `<div class="warn-card"><div class="wc-title">${icon("warn")} ${cap.warn ? "这台电脑只能录整机的声音" : "整机录音会把所有声音一起播出去"}</div>
          <div class="wc-note">游戏声音、语音聊天（好友说的话）也会一起播出去，好友会听到自己说话的回声。想少点回声：主机别在这台电脑上进游戏，或者把游戏和语音的音量调低。${s.systemConfirmed ? "" : "开始前会再确认一次（每台电脑一次）。"}</div></div>` + start;
    };
    const wireCast = () => {
      const stop = $("#mu-stop");
      if (stop) stop.onclick = async () => {
        stop.disabled = true;
        try { await api(base + "/stop", { method: "POST" }); toast("已停止广播"); } catch (e) { toast(e.message, true); }
        await loadStatus();
      };
      const startBtn = $("#mu-start");
      if (startBtn) startBtn.onclick = startCast;
      const rf = $("#mu-src-refresh");
      if (rf) rf.onclick = () => loadSources(true);
    };
    const paintLive = () => {
      const b = v.st.broadcast;
      const ph = b.phase === "live" ? pill("running", "正在播") : pill("starting", b.phase === "connecting" ? "连接中…" : "重连中…");
      $("#mu-live-pill").innerHTML = " " + ph;
      const secs = Math.max(0, (Date.now() - new Date(b.since).getTime()) / 1000);
      $("#mu-live-src").textContent = `本机 · ${b.source} · 已播 ${fmtDur(secs)}`;
      $("#mu-live-lis").textContent = b.listeners >= 0 ? (b.listeners ? `${b.listeners} 人在听` : "还没有人在听") : "听众数：连上桥接模组后显示";
      $("#mu-live-net").textContent = b.phase === "live"
        ? `缓冲 ${b.buffered} 帧 · 往返 ${Math.round(b.rttMs)} ms · 已发 ${b.sent} 帧${b.dropped ? ` · 丢了 ${b.dropped} 帧` : ""}${b.underruns ? ` · 断续 ${b.underruns} 次` : ""}`
        : (b.busy ? "桥接模组那边已经有别的广播连着：等它停下再连" : b.lastError || "正在连接桥接模组…");
      $("#mu-live-warn").innerHTML = b.mode === "system" ? `<div class="warn-text mu-warn">${icon("warn")} 正在录整机声音：游戏声音、语音聊天也会一起播出去</div>` : "";
    };
    const paintStart = () => {
      const btn = $("#mu-start");
      if (!btn) return;
      const why = blockers();
      const needSel = curMode() === "process" && !v.sel;
      btn.disabled = v.busy || why.length > 0 || needSel;
      $("#mu-why").textContent = why[0] || (needSel ? "先在上面选一个正在出声的程序" : "");
    };

    /* ---- 音源（按程序录时）：列表的成员变了才重绘，电平原地更新 ---- */
    const drawSources = () => {
      const box = $("#mu-src");
      if (!box) return;
      if (!v.src) return;
      const list = [...(v.src.sources || [])].sort((a, b) => a.exe.localeCompare(b.exe) || a.pid - b.pid);
      if (v.sel && !list.some(x => x.pid === v.sel.pid)) v.sel = null;
      if (!v.sel) {
        const lastExe = (v.st.settings.exe || "").toLowerCase();
        const pick = list.find(x => lastExe && x.exe.toLowerCase() === lastExe) || list.find(x => x.playing);
        if (pick) v.sel = { pid: pick.pid, exe: pick.exe };
      }
      const sig = list.map(x => x.pid + ":" + x.exe).join("|") + "#" + (v.sel ? v.sel.pid : "") + "#" + (v.src.error || "");
      if (box.dataset.sig !== sig) {
        box.dataset.sig = sig;
        const focused = document.activeElement && box.contains(document.activeElement) ? document.activeElement.dataset.pid : null;
        box.innerHTML = list.length ? list.map(x => {
          const on = !!v.sel && v.sel.pid === x.pid;
          return row({ tile: tile("music"), title: x.name && x.name !== x.exe ? `${x.name}（${x.exe}）` : x.exe,
            sub: `<span data-playing>${x.playing ? "正在出声" : "没有声音"}</span>${(x.devices || []).length ? " · " + esc(x.devices.join("、")) : ""}`,
            right: `<span class="mu-meter" title="当前电平">${xpBar(10, meterCells(x.peak), "success")}</span><span class="ri-check${on ? " on" : ""}" aria-hidden="true">${icon("check")}</span>`,
            cls: "choice" + (on ? " sel" : ""), attrs: `role="radio" tabindex="0" aria-checked="${on}" data-pid="${x.pid}" data-exe="${esc(x.exe)}"` });
        }).join("") : row({ title: v.src.error ? "列不出正在出声的程序" : "没有程序在出声",
          sub: esc(v.src.error || "先在音乐软件里放一首歌，再点「刷新列表」。游戏本身（java）不列出来，录进去会有回声。") });
        box.querySelectorAll("[data-pid]").forEach(r => {
          const pick = () => { v.sel = { pid: +r.dataset.pid, exe: r.dataset.exe }; drawSources(); paintStart(); $(`#mu-src [data-pid="${r.dataset.pid}"]`)?.focus(); };
          r.onclick = pick;
          r.onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); pick(); } };
        });
        if (focused) $(`#mu-src [data-pid="${focused}"]`)?.focus();
      }
      for (const x of list) {
        const r = box.querySelector(`[data-pid="${x.pid}"]`);
        if (!r) continue;
        const n = meterCells(x.peak);
        r.querySelectorAll(".mu-meter .xp i").forEach((c, k) => c.classList.toggle("on", k < n));
        const pl = r.querySelector("[data-playing]");
        if (pl) pl.textContent = x.playing ? "正在出声" : "没有声音";
      }
      paintStart();
    };
    const sourcesWanted = () => v.st.local && !v.st.broadcast && !v.st.elsewhere;
    const loadSources = async force => {
      if (v.srcLoading || (!force && !sourcesWanted())) return;
      v.srcLoading = true;
      try {
        const s = await api("/api/music/sources");
        if (stale(gen)) return;
        v.src = s;
        if (v.device === null) v.device = s.device || "";
        drawSources();
        drawAdv();
      } catch (e) {
        if (!stale(gen)) { v.src = { sources: [], devices: [], error: e.message }; drawSources(); }
      } finally { v.srcLoading = false; }
    };

    /* 开始广播：整机录音（Win10，或者 Win11 上在高级选项里改成整机）先确认一次提醒，确认过就记住 */
    const startCast = async () => {
      const m = curMode();
      if (m === "system" && !v.st.systemConfirmed) {
        const ok = await confirmModal({
          title: v.st.capability.warn ? "这台电脑只能录整机的声音" : "改用整机录音？", okText: "仍然录整机", cancelText: "先不录", danger: true,
          body: `${esc(v.st.capability.warning)}<br><br>想少点回声：主机别在这台电脑上进游戏，或者把游戏和语音的音量调低。<br>这台电脑确认一次就记住，以后不再问。`,
        });
        if (!ok || stale(gen)) return;
        try { await api("/api/music/confirm-system", { method: "POST" }); v.st.systemConfirmed = true; }
        catch (e) { toast(e.message, true); return; }
      }
      const req = m === "process" ? { mode: "process", pid: v.sel.pid, exe: v.sel.exe } : { mode: "system", device: v.device || "" };
      v.busy = true; paintStart();
      try {
        await api(base + "/start", { method: "POST", body: req });
        toast(m === "process" ? `开始广播 ${v.sel.exe} 的声音` : "开始广播整机的声音");
      } catch (e) { toast(e.message, true); }
      v.busy = false;
      await loadStatus();
    };

    /* ---- 检查清单 ---- */
    const drawCheck = () => {
      const s = v.st;
      const sig = JSON.stringify([s.serverStatus, s.svc, s.bridge, s.voice, s.voicePort, s.remote, s.restart, s.note, s.local, v.vhOpen]);
      const box = $("#mu-check");
      const typing = $("#mu-vh") && ($("#mu-vh") === document.activeElement || $("#mu-vh").value !== $("#mu-vh").defaultValue);
      $("#mu-restart").innerHTML = (s.restart || []).length ? `<div class="warn-card"><div class="wc-title">${icon("warn")} 重启服务器后生效</div>
        <div class="wc-note">${esc(s.restart.join("、"))} 是服务器运行中装上或改过的：重启服务器以后才生效。</div></div>` : "";
      if (box.dataset.sig === sig || typing) return; // 正在手动填 voice_host：先不重绘，免得把填了一半的冲掉
      box.dataset.sig = sig;
      const coreLabel = { neoforge: "NeoForge", forge: "Forge", mohist: "Mohist" }[info.core] || info.core;
      // SVC：没装时直接给安装按钮（测试报告观察 13：原来只给「去资源页搜索」，资源页 20 个同名的「安装」容易装错）
      const svc = s.svc;
      const svcRow = svc.installed
        ? row({ tile: okIco(true), titleHTML: `Simple Voice Chat${restartHas("Simple Voice Chat") ? `<span class="tag warn">重启生效</span>` : ""}`,
          sub: `已装 ${esc(svc.version || "（版本未知）")}。玩家的客户端也要装 SVC，版本和服务器一样最稳妥。`, key: svc.file })
        : row({ tile: okIco(false), title: "Simple Voice Chat：mods 里没有",
          sub: `音乐靠它推给玩家。点「安装」从 Modrinth 下载 ${info.core === "neoforge" ? "NeoForge" : "Forge"} ${esc(info.mc)} 版里最新的正式版放进 mods（要联网）` +
            (s.serverStatus !== "stopped" ? "；服务器在运行，装好后重启才生效。" : "。") + "玩家的客户端也要装同一个版本。" +
            ((svc.disabled || []).length ? `<br>有被停用的：${esc(svc.disabled.join("、"))}（不会动它）` : ""),
          right: `<button class="btn sm" id="mu-svc-install" aria-label="安装 Simple Voice Chat">${icon("download")} 安装</button>` });
      // 桥接模组
      const br = s.bridge;
      const variantName = br.variant === "javafml" ? "（备用加载方式）" : "";
      let brRow;
      if (!br.installed) {
        brRow = row({ tile: okIco(false), title: "MineDeck 桥接模组：还没装",
          sub: svc.installed ? `MineDeck 内置的 ${esc(br.version)} 版，装进 mods 就行，不用联网。` : "先装好 Simple Voice Chat：桥接模组离不开它，没有的话服务器会起不来。",
          right: `<button class="btn sm" data-install="main" aria-label="安装桥接模组"${svc.installed ? "" : " disabled"}>${icon("download")} 安装</button>` });
      } else if (!br.file || (br.others || []).length) {
        brRow = row({ tile: okIco(false), title: "MineDeck 桥接模组：要更新",
          sub: `mods 里有别的版本（${esc((br.others || []).join("、"))}），换成内置的 ${esc(br.version)} 版。${s.serverStatus !== "stopped" ? "服务器运行中换不了（文件被占用），先停服。" : ""}`,
          right: `<button class="btn sm" data-install="${br.variant || "main"}" aria-label="更新桥接模组"${s.serverStatus !== "stopped" ? " disabled" : ""}>${icon("download")} 更新</button>` });
      } else if (br.loaded === false) {
        brRow = row({ tile: okIco(false), title: "MineDeck 桥接模组：SVC 没加载它",
          sub: `这次开服 SVC 加载完插件了，却没有桥接模组。可以换${br.variant === "javafml" ? "回默认" : "成备用"}加载方式，停服后安装、再开服试试。`,
          right: `<button class="btn sm" data-install="${br.variant === "javafml" ? "main" : "javafml"}"${s.serverStatus !== "stopped" ? " disabled" : ""}>换${br.variant === "javafml" ? "回默认" : "成备用"}加载方式</button>` });
      } else {
        brRow = row({ tile: okIco(true), titleHTML: `MineDeck 桥接模组${restartHas("桥接模组") ? `<span class="tag warn">重启生效</span>` : ""}`,
          sub: `已装 ${esc(br.version)} 版${variantName}${br.loaded ? "，SVC 已加载它" : ""}。`, key: br.file });
      }
      // 语音端口
      const vc = s.voice;
      const portText = vc.port === -1 ? "和游戏同一个端口" : `UDP ${vc.port}`;
      const portRow = vc.found
        ? row({ tile: tile("plug"), title: `语音端口：${portText}`,
          sub: `${s.voicePort && s.voicePort !== vc.port ? `这次开服 SVC 实际用的是 UDP ${s.voicePort}。` : ""}局域网里的好友：在 Windows 防火墙放行这个 UDP 端口就能连上语音。`, key: vc.path })
        : row({ tile: tile("plug"), title: "语音端口：还不知道", sub: "SVC 第一次开服后才生成配置（默认 UDP 24454），开一次服再来看。", key: vc.path });
      // 远程语音
      const rm = s.remote, t0 = (rm.tunnels || [])[0];
      const vh = vc.voiceHost;
      const vhKey = `voice_host = ${vh || "（空）"}`;
      const manual = `<button class="btn sm ghost" id="mu-vh-toggle" aria-expanded="${v.vhOpen}">手动填写</button>`;
      let rmRow;
      const mismatch = (rm.tunnels || []).filter(t => t.localPort && vc.found && vc.port > 0 && t.localPort !== vc.port)
        .map(t => `隧道「${esc(t.name)}」的本地端口 ${t.localPort} 和 SVC 的端口 ${vc.port} 对不上，核对一下。`).join("");
      switch (rm.state) {
        case "none":
          rmRow = { tile: tile("globe"), title: "远程好友的语音：没有语音隧道",
            sub: (vh ? `voice_host 已经填了 ${esc(vh)}。` : "") + "远程好友要听到声音：在「内网穿透」添加一条 UDP 隧道，用途选「语音」、绑定这台服务器。只有局域网好友就不用管。",
            right: `<a class="btn sm" href="#/tunnels">去添加</a>${manual}` };
          break;
        case "waiting":
          rmRow = { tile: okIco(false, "clock"), title: `远程好友的语音：隧道「${t0.name}」还没拿到公网地址`,
            sub: esc(t0.lastError || (t0.running ? "隧道连接中…" : "启动隧道后，MineDeck 会把公网地址自动写进 voice_host。")) + (t0.autoStart ? "" : " 勾上「跟随实例启动」，开服时就会拉起它。"),
            right: manual };
          break;
        case "idle": // 隧道停着，地址是上次的；勾了跟随启动的，下次开服会跟着拉起来，voice_host 不清
          rmRow = { tile: okIco(false), title: "远程好友的语音：语音隧道没开",
            sub: (rm.tunnels || []).some(t => t.autoStart)
              ? `隧道停着时谁都连不上 ${esc(rm.addr)} 这个地址的语音。启动隧道就恢复；下次开服时它也会跟着启动，voice_host 照旧。`
              : rm.managed
              ? `voice_host 还是隧道上次的地址 ${esc(rm.addr)}：隧道停着时谁都连不上这个地址的语音（本机玩家也一样）。启动隧道就恢复；不开的话，下次开服前 MineDeck 会把它清回空。`
              : `隧道上次的公网地址是 ${esc(rm.addr)}。启动隧道，或者勾上「跟随实例启动」，MineDeck 会把地址写进 voice_host。`,
            right: `<a class="btn sm" href="#/tunnels">去启动</a>${manual}` };
          break;
        case "pending":
          rmRow = { tile: okIco(true), title: `远程好友的语音：${rm.addr}`,
            sub: vc.found ? "下次开服前自动写进 voice_host。" : "SVC 的配置还没生成：开一次服，MineDeck 就会把这个地址写进 voice_host。", right: manual };
          break;
        case "same":
          rmRow = { tile: okIco(true), title: `远程好友的语音：${rm.addr}`, sub: "voice_host 已经是语音隧道的公网地址，远程好友连服后就能听到。", right: manual };
          break;
        default: // differ
          rmRow = { tile: okIco(false), title: "远程好友的语音：voice_host 和隧道地址不一样",
            sub: `voice_host 现在是 ${esc(vh)}，语音隧道的公网地址是 ${esc(rm.addr)}。是你自己填的就留着；想用隧道的地址就点「改用隧道地址」。`,
            right: `<button class="btn sm" id="mu-vh-tunnel">改用隧道地址</button>${manual}` };
      }
      rmRow.key = vhKey;
      if (mismatch) rmRow.sub += `<br><span class="warn-text">${icon("warn")} ${mismatch}</span>`;
      rmRow.cls = v.vhOpen ? "open" : "";
      rmRow.after = `<div class="ri-editor"><div class="ri-field" style="padding:0">
          <input type="text" id="mu-vh" value="${esc(vh)}" placeholder="如 frp-abc.example.com:34567；清空表示不填" aria-label="voice_host"><button class="btn" id="mu-vh-save" aria-label="保存 voice_host">保存</button></div>
        <div class="field-err" id="mu-vh-err"></div>
        <div class="hint">填了以后所有人都走这个地址连语音（局域网的人也会绕到穿透节点）。手动填的 MineDeck 不会自动覆盖。${s.serverStatus !== "stopped" ? "SVC 只在启动时读，重启服务器后生效。" : ""}</div></div>`;
      box.innerHTML = group(
        row({ tile: okIco(true), title: `核心：${coreLabel} ${info.mc}`, sub: esc(s.note || "支持音乐共享。") }) +
        svcRow + brRow + portRow + row(rmRow), { ico: true });
      // 交互
      const svcBtn = $("#mu-svc-install");
      if (svcBtn) svcBtn.onclick = async () => {
        svcBtn.disabled = true;
        svcBtn.textContent = "下载中…";
        try {
          const r = await api(base + "/svc", { method: "POST" });
          toast(`已装好 Simple Voice Chat ${r.version || r.name}` + (r.restart ? "：重启服务器后生效" : "") + (r.type ? `（Modrinth 上没有正式版，装的是 ${r.type} 版）` : ""));
        } catch (e) { toast(e.message, true); }
        box.dataset.sig = ""; // 失败时也重画，按钮恢复
        await loadStatus();
      };
      box.querySelectorAll("[data-install]").forEach(b => b.onclick = async () => {
        b.disabled = true;
        try {
          const r = await api(base + "/install", { method: "POST", body: { variant: b.dataset.install } });
          toast((r.restart ? "已装好桥接模组：重启服务器后生效" : "已装好桥接模组") + ((r.removed || []).length ? `（删掉了 ${r.removed.join("、")}）` : ""));
        } catch (e) { toast(e.message, true); }
        await loadStatus();
      });
      const tb = $("#mu-vh-tunnel");
      if (tb) tb.onclick = async () => {
        tb.disabled = true;
        try { const r = await api(base + "/voicehost", { method: "PUT", body: { fromTunnel: true } }); toast(vhMsg(r)); }
        catch (e) { toast(e.message, true); }
        await loadStatus();
      };
      $("#mu-vh-toggle").onclick = () => {
        v.vhOpen = !v.vhOpen;
        const r = $("#mu-vh").closest(".row-item");
        r.classList.toggle("open", v.vhOpen);
        $("#mu-vh-toggle").setAttribute("aria-expanded", String(v.vhOpen));
        box.dataset.sig = ""; // 下次轮询按新的展开状态重算
        if (v.vhOpen) $("#mu-vh").focus();
      };
      const vhErr = msg => { $("#mu-vh-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; $("#mu-vh").classList.toggle("invalid", !!msg); };
      bindSaveInput($("#mu-vh"), $("#mu-vh-save"), async val => {
        vhErr("");
        try { const r = await api(base + "/voicehost", { method: "PUT", body: { value: val.trim() } }); toast(vhMsg(r)); }
        catch (e) { vhErr(e.message); throw Object.assign(e, { silent: true }); }
        $("#mu-vh").defaultValue = $("#mu-vh").value; // 已保存：不再算「填了一半」
        box.dataset.sig = "";
        loadStatus();
      }, gen);
    };
    const vhMsg = r => !r.changed ? "voice_host 没有变" : `已写进 voice_host=${r.after || "（空）"}${r.restart ? "：重启服务器后生效" : ""}`;

    /* ---- 听众 ---- */
    const drawPlayers = () => {
      const s = v.st, p = s.players, box = $("#mu-players");
      const sig = JSON.stringify([s.serverStatus, p, s.guide]);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      const rows = [];
      if (s.serverStatus !== "running") rows.push(row({ tile: tile("headphones"), title: "开服后这里会显示谁连上了语音", sub: "连上语音的人都能听到音乐；没连上的多半是没装 SVC，或者版本不对。" }));
      else {
        rows.push(row({ tile: tile("headphones"), title: p.voice.length ? `会听到音乐：${p.voice.length} 人` : "还没有人连上语音",
          sub: (p.voice.length ? names(p.voice) : "") + (p.known ? "" : `<div class="hint">MineDeck 是开服以后才开始看控制台的，可能漏掉了一些人。</div>`) }));
        if (p.excluded.length) rows.push(row({ tile: tile("player"), title: "在「我在游戏里的名字」里：不再发给他", sub: names(p.excluded) }));
        if (p.noVoice.length) rows.push(row({ tile: okIco(false), title: `在线但没连上语音：${p.noVoice.length} 人`, sub: "多半没装 Simple Voice Chat，把下面的说明发给他们。" + names(p.noVoice) }));
        if (p.incompatible.length) rows.push(row({ tile: okIco(false), title: `SVC 版本不兼容：${p.incompatible.length} 人`,
          sub: "要装和服务器一样版本的 SVC。" + `<div class="mu-names">${p.incompatible.map(x => `<span class="badge">${esc(x.name)}（客户端 ${x.client} / 服务端 ${x.server}）</span>`).join("")}</div>` }));
      }
      rows.push(row({ tile: tile("copy"), title: "发给好友的说明", sub: `<span class="mu-guide">${esc(s.guide)}</span>`, right: `<button class="btn sm" id="mu-guide-copy" aria-label="复制发给好友的说明">${icon("copy")} 复制</button>` }));
      box.innerHTML = group(rows.join(""), { ico: true });
      $("#mu-guide-copy").onclick = () => copy(v.st.guide, "已复制给好友的说明");
    };

    /* ---- 设置：只画一次（输入框不随轮询重建） ---- */
    const drawSettings = () => {
      const s = v.st;
      $("#mu-set").innerHTML = group(
        row({ tile: tile("player"), title: "我在游戏里的名字（可以不填）",
          sub: "填了就不把音乐再发给你：你在这台电脑上已经直接听到原声了，SVC 再发一份会听到两遍。几个名字用逗号隔开。",
          field: `<input type="text" id="mu-ex" value="${esc(s.settings.exclude)}" maxlength="400" placeholder="如 Steve" aria-label="我在游戏里的名字"><button class="btn" id="mu-ex-save" aria-label="保存我在游戏里的名字">保存</button>`,
          after: `<div class="field-err ri-err" id="mu-ex-err"></div>` }) +
        row({ tile: tile("volume"), title: "音量", sub: "100% 为原样；正在广播时立刻生效。",
          right: `<input type="range" class="mu-vol" id="mu-vol" min="0" max="200" step="5" value="${s.settings.volume}" aria-label="音量"><span class="mu-vol-v" id="mu-vol-v">${s.settings.volume}%</span>` }),
        { ico: true });
      const exErr = msg => { $("#mu-ex-err").innerHTML = msg ? icon("warn") + " " + esc(msg) : ""; $("#mu-ex").classList.toggle("invalid", !!msg); };
      $("#mu-ex").addEventListener("input", () => { if ($("#mu-ex").classList.contains("invalid")) exErr(checkNames($("#mu-ex").value)); });
      bindSaveInput($("#mu-ex"), $("#mu-ex-save"), async val => {
        const bad = checkNames(val);
        exErr(bad);
        if (bad) throw Object.assign(new Error(bad), { silent: true });
        try { await api(base + "/settings", { method: "PUT", body: { exclude: val.trim() } }); }
        catch (e) { exErr(e.message); throw Object.assign(e, { silent: true }); }
        toast(val.trim() ? `已保存：不再把音乐发给 ${val.trim()}（2 秒内生效）` : "已清空：音乐发给所有连上语音的人");
      }, gen);
      const vol = $("#mu-vol");
      vol.oninput = () => { $("#mu-vol-v").textContent = vol.value + "%"; };
      vol.onchange = async () => {
        try { await api(base + "/settings", { method: "PUT", body: { volume: +vol.value } }); }
        catch (e) { toast(e.message, true); }
      };
    };

    /* ---- 高级选项：采集方式（Win11）、整机录音的播放设备、桥接模组的加载方式 ---- */
    const drawAdv = () => {
      const s = v.st, cap = s.capability, m = curMode(), box = $("#mu-adv-box");
      const devs = (v.src && v.src.devices) || [];
      const sig = JSON.stringify([cap.mode, m, devs, v.device, s.bridge.variant, s.bridge.installed, s.serverStatus, s.local, v.advOpen, !!s.broadcast]);
      if (box.dataset.sig === sig) return;
      box.dataset.sig = sig;
      // 折叠行的名字固定是标题，副标题放进描述（同游戏内 AI 的「高级识别」）
      const rows = [row({ tile: tile("gear"), title: "高级选项", sub: `<span id="mu-adv-desc">${s.local ? "采集方式、整机录音用哪个播放设备、桥接模组的加载方式。" : "桥接模组的加载方式。"}</span>`,
        right: `<span class="ri-chev">${icon("chevronDown")}</span>`, cls: "expandable" + (v.advOpen ? " open" : ""),
        attrs: `id="mu-adv-head" role="button" tabindex="0" aria-expanded="${v.advOpen}" aria-label="高级选项" aria-describedby="mu-adv-desc"` })];
      if (cap.mode === "process" && s.local) rows.push(row({ tile: tile("music"), title: "采集方式", cls: "mu-adv-row",
        sub: "默认只录一个程序；改成整机录音会把游戏声音、语音聊天一起录进去（有回声）。",
        right: `<div class="seg" role="radiogroup" aria-label="采集方式">${[["process", "只录一个程序"], ["system", "整机录音"]].map(([k, l]) =>
          `<button type="button" role="radio" aria-checked="${m === k}" class="${m === k ? "on" : ""}" data-mode="${k}"${s.broadcast ? " disabled" : ""}>${l}</button>`).join("")}</div>` }));
      else if (cap.mode === "system" && s.local) rows.push(row({ tile: tile("music"), title: "采集方式：整机录音", cls: "mu-adv-row", sub: `这台电脑（${esc(cap.os)}）只能录整机的声音；Windows 11 才能只录一个程序。` }));
      if (m === "system" && s.local) {
        const def = devs.find(d => d.default);
        rows.push(row({ tile: tile("volume"), title: "播放设备", cls: "mu-adv-row",
          sub: "录哪个设备放出来的声音。在 Windows「应用音量和设备首选项」里把音乐软件改到第二个设备（比如虚拟声卡），再在这里选它，就能避开游戏声音和回声。",
          right: `<select id="mu-dev" aria-label="播放设备"${s.broadcast ? " disabled" : ""}><option value="">系统默认${def ? `（${esc(def.name)}）` : ""}</option>${devs.map(d =>
            `<option value="${esc(d.id)}"${d.id === v.device ? " selected" : ""}>${esc(d.name)}</option>`).join("")}</select>` }));
      }
      if (s.bridge.installed) {
        const alt = s.bridge.variant === "javafml" ? "main" : "javafml";
        rows.push(row({ tile: tile("package"), title: `桥接模组的加载方式：${s.bridge.variant === "javafml" ? "备用" : "默认"}`, cls: "mu-adv-row",
          sub: "默认的在服务器上没被 SVC 加载时，换成备用的试试（停服后安装）。",
          right: `<button class="btn sm" data-install-adv="${alt}"${s.serverStatus !== "stopped" ? ` disabled title="先停服"` : ""}>换成${alt === "main" ? "默认" : "备用"}的</button>` }));
      }
      box.innerHTML = group(rows.join(""), { ico: true, cls: "mu-adv" + (v.advOpen ? " open" : "") });
      const head = $("#mu-adv-head"), toggle = () => {
        v.advOpen = !v.advOpen;
        box.dataset.sig = "";
        drawAdv();
        $("#mu-adv-head").focus();
      };
      head.onclick = toggle;
      head.onkeydown = e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle(); } };
      box.querySelectorAll("[data-mode]").forEach(b => b.onclick = () => {
        v.mode = b.dataset.mode;
        drawAll();
        if (v.mode === "process") loadSources(true);
      });
      const dev = $("#mu-dev");
      if (dev) dev.onchange = () => { v.device = dev.value; };
      box.querySelectorAll("[data-install-adv]").forEach(b => b.onclick = async () => {
        b.disabled = true;
        try {
          const r = await api(base + "/install", { method: "POST", body: { variant: b.dataset.installAdv } });
          toast(`已换成${b.dataset.installAdv === "main" ? "默认" : "备用"}加载方式：${r.file}，开服后看 SVC 有没有加载它`);
        } catch (e) { toast(e.message, true); }
        await loadStatus();
      });
    };

    const drawAll = () => { drawCast(); drawCheck(); drawPlayers(); drawAdv(); };
    const loadStatus = async () => {
      try {
        const s = await api(base);
        if (stale(gen)) return;
        v.st = s;
        drawAll();
      } catch (e) { /* 轮询偶尔失败不打扰，下一拍再试 */ }
    };

    drawSettings();
    drawAll();
    if (v.st.local) loadSources(true);
    let tick = 0;
    startPoll(() => {
      tick++;
      if (tick % 3 === 0 && refreshHead) refreshHead();
      if (curMode() === "process" && sourcesWanted()) loadSources();
      if (tick % 2 === 0) loadStatus();
    }, 1000, gen);
  }

  /* ---------- 隧道表单：「用途」和自定义 frps 的「协议」（§8.8） ----------
     樱花、OpenFrp 的隧道是 TCP 还是 UDP 由服务商后台定，MineDeck 看不出来，只能让用户标；选了「语音」就写出要在后台怎么建，
     SVC 的端口从绑定实例的配置里读。自定义 frps 选「语音」时协议默认 UDP、本地端口填 SVC 的端口。 */
  function tunnelForm(insts, provSel) {
    ensureIcons();
    const nameField = $("#tn-name") && $("#tn-name").closest("label");
    const sportField = $("#tn-sport") && $("#tn-sport").closest("label");
    if (!nameField || !sportField) return;
    nameField.insertAdjacentHTML("afterend", `<label class="field"><span>用途</span><select id="tn-purpose">
      <option value="game">游戏（玩家连服用）</option><option value="voice">语音（Simple Voice Chat 的 UDP 隧道）</option></select></label>`);
    sportField.insertAdjacentHTML("afterend", `<label class="field" id="tn-proto-f" hidden><span>协议</span><select id="tn-proto">
      <option value="tcp">TCP（游戏）</option><option value="udp">UDP（语音）</option></select></label>`);
    $("#tn-form .form-grid").insertAdjacentHTML("afterend", `<div class="mu-tip" id="tn-voice-tip" hidden></div>`);
    const purpose = $("#tn-purpose"), proto = $("#tn-proto"), bound = $("#tn-bound");
    let svcPort = 24454, portFrom = "";
    const lportLabel = $("#tn-lport") && $("#tn-lport").closest("label").querySelector("span");
    const paint = () => {
      const p = provSel.value, voice = purpose.value === "voice";
      $("#tn-proto-f").hidden = p !== "custom";
      if (lportLabel) lportLabel.textContent = voice ? "本地端口（SVC 的语音端口）" : "本地端口（MC 服务器端口）";
      const tip = $("#tn-voice-tip");
      tip.hidden = !voice;
      if (!voice) return;
      const port = `<b>${svcPort}</b>${portFrom ? `（${esc(portFrom)} 的 SVC 端口）` : "（SVC 默认端口；以实例 config/voicechat/voicechat-server.properties 里的 port 为准）"}`;
      const steps = {
        natfrp: [`在樱花后台新建隧道：类型选 <b>UDP</b>，本地 IP 填 127.0.0.1，本地端口填 ${port}。节点可以和游戏隧道选同一个。`,
          "复制隧道 ID 填在上面；访问密钥和游戏隧道是同一个。", "绑定同一台服务器，勾上「跟随实例启动」。拿到公网地址后，MineDeck 会自动把它写进这台服务器的 voice_host。"],
        openfrp: [`在 OpenFrp 控制台新建 <b>UDP</b> 隧道，本地端口填 ${port}（OpenFrp 的 UDP 隧道还没实测过）。`, "复制隧道 ID 填在上面。",
          "绑定同一台服务器，勾上「跟随实例启动」；拿到公网地址后自动写进 voice_host。"],
        custom: [`协议选 <b>UDP</b>，本地端口填 ${port}，公网远程端口填 frps 上给语音开的 UDP 端口（frps 要放行它）。`,
          "绑定同一台服务器，勾上「跟随实例启动」；公网地址就是「frps 地址:远程端口」，会自动写进 voice_host。"],
      }[p] || [];
      tip.innerHTML = `<div class="mu-tip-title">${icon("headphones")} 语音隧道怎么建</div><ol>${steps.map(s => `<li>${s}</li>`).join("")}</ol>`;
    };
    const readPort = async () => {
      svcPort = 24454; portFrom = "";
      const name = bound.value, i = insts.find(x => x.name === name);
      if (i && applies(i.core)) {
        try {
          const st = await api(`/api/instances/${encodeURIComponent(name)}/music`);
          if (st.voice && st.voice.port > 0) { svcPort = st.voice.port; portFrom = name; }
        } catch {}
      }
      if (purpose.value === "voice" && provSel.value === "custom") $("#tn-lport").value = svcPort;
      paint();
    };
    purpose.onchange = () => {
      if (purpose.value === "voice") { proto.value = "udp"; readPort(); return; }
      proto.value = "tcp";
      if (provSel.value === "custom" && +$("#tn-lport").value === svcPort) $("#tn-lport").value = 25565; // 只还原我们填进去的
      paint();
    };
    provSel.addEventListener("change", () => { if (purpose.value === "voice") readPort(); else paint(); });
    bound.addEventListener("change", () => { if (purpose.value === "voice") readPort(); });
    paint();
  }
  /* 添加隧道时并进请求体的字段 */
  function tunnelExtra() {
    const p = $("#tn-purpose");
    if (!p) return {};
    const out = { purpose: p.value };
    if ($("#tn-provider").value === "custom") out.proto = $("#tn-proto").value;
    return out;
  }
  /* 隧道列表里的标记：语音隧道、自定义 frps 的 UDP */
  function tunnelBadge(t) {
    ensureIcons();
    return (t.purpose === "voice" ? `<span class="badge">${icon("headphones")} 语音</span>` : "") +
      (t.provider === "custom" && t.proto === "udp" ? `<span class="badge">UDP</span>` : "");
  }

  return { applies, render, tunnelForm, tunnelExtra, tunnelBadge };
})();
