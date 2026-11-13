/* MineDeck 地图导入：把一份现成的 Java 版世界用在服务器上（后端 internal/worldimport，设计见 docs/地图导入-设计.md）。
   两个入口共用这里的「选来源 → 检测 → 挑地图 → 核对」：
     详情页的「地图」标签：看当前世界和恢复点，「导入并替换当前地图」，换回导入前的地图，删除恢复点；
     新建向导：第一步上方的「用已有地图开服」，选好以后按地图预选核心和版本，最后一步核对，创建时带上 worldPlan / worldAck。
   来源：坐在这台电脑前的人可以选文件夹或填路径（/api/world/scan，后端只认本机）；任何设备都可以上传 ZIP（/api/world/upload）。
   不可信的文字：地图名、文件夹名、来源名、检查结果、数据包名、说明书、卡住的路径都是别人给的，一律转义或用 textContent，
   说明书只当纯文字显示，里面的网址不变成链接。
   检测结果由后端保管（上传的文件也在它那儿）：人放弃了就调 /api/world/discard 清掉。导入的请求发出去以后先留着
   （没完成时可以回去重新核对，不用再传一遍），任务成功了再清；新建服务器的请求被收下以后就清（后端把来源钉在新建任务上，用完才删文件）。
   在 app.js 之前加载；用到的 app.js 工具（api、esc、$、icon、toast、toastErr、errBox、confirmModal、stale、startPoll、fmtBytes、xpBar、
   coreName、ST_TEXT、TOKEN、showLogin、viewGen）都在调用时才取，加载顺序不影响。app.js 里只留了钩子：详情页的标签和分派、
   TASK_KINDS 的 "world"、向导的地图条、版本预选、最后一步的核对和提交。 */
"use strict";
window.MDWorldImport = (() => {
  const TAB = "world";
  const TAB_LABEL = "地图"; // 标签上的字只在这一处
  const UPLOAD_MAX = 8 * 1024 ** 3; // 同后端 worldUploadLimit
  const PROXY_CORES = new Set(["velocity", "waterfall"]);
  /* 有「地图」标签的服务器：代理端没有世界（后端也会拒绝，不只是不显示） */
  const applies = info => !!info && info.kind !== "proxy" && !PROXY_CORES.has(info.core);

  const E = encodeURIComponent;
  const post = (path, body) => api(path, { method: "POST", body: body || {} });
  const size = n => fmtBytes(n) || "0 B";
  const pad = x => String(x).padStart(2, "0");
  const when = (iso, sec = false) => {
    const d = new Date(iso);
    if (isNaN(d) || d.getFullYear() < 2000) return ""; // 读不出来的记录没有时间（后端给的是零值）
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}${sec ? ":" + pad(d.getSeconds()) : ""}`;
  };
  const clip = (s, n) => { const a = [...String(s || "")]; return a.length > n ? a.slice(0, n).join("") + "…" : a.join(""); };
  const candName = c => (c && c.meta && c.meta.levelName) || (c && c.folderName) || "没有名字的地图";

  /* ---------- 放弃检测结果 ----------
     pending：现在还握着、没用掉的那次检测。alive() 说它所在的界面还在不在；切了页（hashchange）发现不在了就清掉。
     导入、创建的请求发出去以后调 keep()：后台任务要读这份来源，不能清。 */
  let pending = null;
  let activeUpload = null; // 正在上传的那一个：{ abort, alive }
  const discard = id => { if (id) api("/api/world/discard", { method: "POST", body: { scanId: id } }).catch(() => {}); };
  const hold = (id, alive) => {
    if (pending && pending.id !== id) discard(pending.id);
    pending = { id, alive };
  };
  const drop = () => { if (pending) { discard(pending.id); pending = null; } };
  const keep = id => { if (pending && (!id || pending.id === id)) pending = null; };
  window.addEventListener("hashchange", () => setTimeout(() => {
    if (pending && !pending.alive()) drop();
    if (activeUpload && !activeUpload.alive()) activeUpload.abort();
  }, 0));

  /* 这一页发起的换地图任务（服务器名 → 任务）：切到别的标签再回来还能接着看进度。记在 sessionStorage 里，刷新页面也认得。
     后端的世界状态里没有任务编号，别的设备上发起的这里看不到，只能看到「正在换地图」的提示。 */
  const LIVE_KEY = "md_world_tasks";
  const live = (() => { try { return JSON.parse(sessionStorage.getItem(LIVE_KEY) || "{}") || {}; } catch { return {}; } })();
  const saveLive = () => { try { sessionStorage.setItem(LIVE_KEY, JSON.stringify(live)); } catch { /* 存不了就只在这一次页面里记着 */ } };
  /* 服务器名 → 最近一次发起导入时选的 { scan, cand }：导入没完成时可以回去重新核对，不用再传一遍。只在这一次页面里记着 */
  const picks = {};

  /* ---------- 给人看的说法 ---------- */
  const LAYOUT = {
    legacy: "旧版布局（26.1 以前的原版）",
    "legacy-bukkit": "旧版 Paper / Spigot 布局（主世界、下界、末地三个文件夹）",
    modern: "新版布局（26.1 起的原版）",
    "modern-paper": "新版 Paper 布局（26.1 起）",
  };
  const DIMS = { "minecraft:overworld": "主世界", "minecraft:the_nether": "下界", "minecraft:the_end": "末地" };
  const dimName = id => DIMS[id] || `<code>${esc(id)}</code>`;
  const KIND_TITLE = {
    "java-world": "没找到能导入的地图",
    "damaged-world": "这份世界缺了关键文件",
    bedrock: "这是基岩版的世界",
    structure: "这是建筑蓝图，不是完整的世界",
    "resource-pack": "这是资源包",
    datapack: "这是数据包",
    modpack: "这是整合包",
    server: "这是一份完整的服务端",
    mdpack: "这是 MineDeck 的交接包",
    archive: "这个压缩包读不了",
    unknown: "没认出这是什么",
  };
  const SET_NAME = { difficulty: "难度", gamemode: "默认游戏模式", hardcore: "极限模式" };
  const SET_VAL = {
    peaceful: "和平", easy: "简单", normal: "普通", hard: "困难",
    survival: "生存", creative: "创造", adventure: "冒险", spectator: "旁观",
    true: "开", false: "关",
  };
  const setVal = v => { const k = String(v == null ? "" : v).trim().toLowerCase(); return SET_VAL[k] || (k === "" ? "没有设" : String(v)); };
  const DIFFS = ["peaceful", "easy", "normal", "hard"], MODES = ["survival", "creative", "adventure", "spectator"];

  /* 检查结果：一句话在外面，技术细节收进「详情」 */
  const bySev = list => {
    const g = { block: [], confirm: [], warn: [], info: [] };
    for (const x of list || []) (g[x.sev] || g.info).push(x);
    return g;
  };
  const issueLi = x => `<li><span>${esc(x.msg)}</span>${x.detail
    ? `<details class="wi-detail"><summary aria-label="详情：${esc(clip(x.msg, 18))}">详情</summary><div class="wi-detail-body">${esc(x.detail)}</div></details>` : ""}</li>`;
  const issueList = (list, cls = "") => `<ul class="wi-issues${cls ? " " + cls : ""}">${list.map(issueLi).join("")}</ul>`;

  /* ---------- 上传（要进度，所以用 XHR；回来的形状和 api() 一样） ---------- */
  function upload(file, onProgress) {
    const xhr = new XMLHttpRequest();
    const done = new Promise((resolve, reject) => {
      xhr.open("POST", "/api/world/upload");
      xhr.setRequestHeader("X-Token", TOKEN());
      xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress(e.loaded, e.total); };
      xhr.onload = () => {
        let j = null;
        try { j = JSON.parse(xhr.responseText); } catch { /* 不是 JSON：下面按服务异常报 */ }
        if (xhr.status === 401) { showLogin(); reject(Object.assign(new Error((j && j.error) || "需要登录"), { needLogin: true })); return; }
        if (!j) { reject(new Error(`服务异常 (HTTP ${xhr.status})`)); return; }
        if (!j.ok) { reject(Object.assign(new Error(j.error || "上传失败"), { status: xhr.status, hint: j.hint })); return; }
        resolve(j.data);
      };
      xhr.onerror = () => reject(new TypeError("Failed to fetch"));
      xhr.onabort = () => reject(Object.assign(new Error("已取消上传"), { aborted: true }));
      const fd = new FormData();
      fd.append("file", file);
      xhr.send(fd);
    });
    return { done, abort: () => xhr.abort() };
  }

  /* ---------- 选来源、检测、挑地图 ----------
     o.local：是不是坐在运行 MineDeck 的那台电脑前（/api/app 的 local）；o.gone()：这块界面还在不在；
     o.hold(scan)：拿到检测结果了，登记「人走了就清掉」；o.onPicked(scan, cand)：选定了一张地图；
     o.quiet：同屏已经有主按钮（向导的「下一步」），这里的按钮都用普通样子。
     返回 { source(), cands() }：回到选来源、回到候选列表。 */
  function sourceUI(root, o) {
    let scan = null, busy = false;
    const q = sel => $(sel, root);
    const setBusy = (b, text) => {
      busy = b;
      root.querySelectorAll("#wi-browse, #wi-scan, #wi-upload").forEach(x => { x.disabled = b; });
      const st = q("#wi-src-state");
      if (st) st.innerHTML = b && text ? `<div class="wi-busy">${icon("spinner")} <span>${esc(text)}</span></div>` : "";
    };
    const pathErr = msg => {
      const inp = q("#wi-path"), err = q("#wi-path-err");
      if (inp) inp.classList.toggle("invalid", !!msg);
      if (err) err.innerHTML = msg ? icon("warn") + " " + esc(msg) : "";
    };

    function drawSource(prevPath = "") {
      scan = null; busy = false;
      root.innerHTML = `
        <div class="wi-src">
          ${o.local ? `
          <div class="field"><span>这台电脑上的地图</span>
            <div class="row wi-row">
              <button type="button" class="btn${o.quiet ? "" : " primary"}" id="wi-browse">${icon("folder")} 选择地图文件夹</button>
              <span class="hint">选存档文件夹（里面有 level.dat），或者它的上一层。</span>
            </div>
            <div class="row wi-row">
              <input type="text" id="wi-path" spellcheck="false" autocomplete="off" value="${esc(prevPath)}" aria-label="地图文件夹或 ZIP 文件的路径" placeholder="也可以填完整路径：文件夹，或者没解压的 ZIP，如 D:\\Maps\\SkyCity">
              <button type="button" class="btn" id="wi-scan">${icon("search")} 检测</button>
            </div>
            <div class="field-err" id="wi-path-err"></div>
          </div>` : `
          <div class="hint wi-remote">${icon("info")} 选文件夹只能在运行 MineDeck 的那台电脑上操作。在这台设备上，请把地图压成 ZIP 再上传。</div>`}
          <div class="field"><span>${o.local ? "或者上传 ZIP 压缩包" : "上传地图的 ZIP 压缩包"}</span>
            <div class="row wi-row">
              <button type="button" class="btn${o.local || o.quiet ? "" : " primary"}" id="wi-upload">${icon("arrowUp")} 上传 ZIP</button>
              <input type="file" id="wi-file" hidden>
              <span class="hint">最大 8 GB。更大的地图，请在运行 MineDeck 的那台电脑上直接选文件夹。</span>
            </div>
            <div id="wi-up" aria-live="polite"></div>
          </div>
          <div id="wi-src-state" aria-live="polite"></div>
        </div>`;
      if (o.local) {
        q("#wi-browse").onclick = async () => {
          if (busy) return;
          try {
            const r = await api("/api/pickdir", { method: "POST" });
            if (o.gone() || !r.path) return;
            q("#wi-path").value = r.path;
            scanPath(r.path);
          } catch (e) { toastErr("无法选择地图文件夹", e); }
        };
        q("#wi-scan").onclick = () => scanPath(q("#wi-path").value);
        q("#wi-path").addEventListener("input", () => pathErr(""));
        q("#wi-path").addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); scanPath(q("#wi-path").value); } });
      }
      q("#wi-upload").onclick = () => { if (!busy) q("#wi-file").click(); };
      q("#wi-file").onchange = () => { const f = q("#wi-file").files[0]; q("#wi-file").value = ""; startUpload(f); };
    }

    async function scanPath(p) {
      if (busy) return;
      p = String(p || "").trim();
      if (!p) { pathErr("先选一个文件夹，或者把完整路径填进来"); return; }
      pathErr("");
      q("#wi-up").innerHTML = "";
      setBusy(true, "正在检测这份地图…");
      try {
        const s = await post("/api/world/scan", { path: p });
        if (o.gone()) { discard(s.id); return; }
        showScan(s);
      } catch (e) {
        if (o.gone()) return;
        setBusy(false);
        q("#wi-src-state").innerHTML = errBox("没能检测这个位置", e);
      }
    }

    function startUpload(file) {
      if (busy || !file) return;
      const up = q("#wi-up");
      const ext = ((file.name || "").match(/\.([a-z0-9]+)$/i) || ["", ""])[1].toLowerCase();
      q("#wi-src-state").innerHTML = "";
      if (file.size > UPLOAD_MAX) {
        up.innerHTML = `<div class="err-box">这个压缩包有 ${esc((file.size / 1024 ** 3).toFixed(1))} GB，超过了 8 GB 的上限。这么大的地图，请在运行 MineDeck 的那台电脑上直接选择地图文件夹导入。</div>`;
        return;
      }
      if (["rar", "7z", "tar", "gz", "xz", "bz2"].includes(ext)) { // 传完才被告知读不了太浪费：这几种在上传前就说
        up.innerHTML = `<div class="err-box">读不了 ${esc(ext.toUpperCase())} 压缩包。先用解压软件解开，${o.local ? "再选解出来的地图文件夹" : "把地图文件夹重新压成 ZIP 再上传"}。</div>`;
        return;
      }
      setBusy(true, "");
      up.innerHTML = `<div class="wi-upbar">
        <div class="progress-head"><b id="wi-up-name"></b><span class="grow"></span><span class="xp-level num" id="wi-up-pct">0%</span></div>
        ${xpBar(30, 0)}
        <div class="row wi-row"><span class="progress-text" id="wi-up-text">正在上传…</span><span class="grow"></span>
          <button type="button" class="btn sm" id="wi-up-cancel">取消上传</button></div></div>`;
      q("#wi-up-name").textContent = file.name; // 文件名是别人给的：只当文字
      const cells = up.querySelectorAll(".xp i");
      const job = upload(file, (loaded, total) => {
        if (o.gone() || !up.isConnected) return;
        const pct = total > 0 ? Math.min(100, loaded / total * 100) : 0;
        const n = Math.floor(pct / 100 * cells.length + 1e-6);
        cells.forEach((c, k) => { c.className = k < n ? "on" : ""; });
        q("#wi-up-pct").textContent = Math.round(pct) + "%";
        q("#wi-up-text").textContent = loaded >= total ? "传完了，正在检测这份地图…" : `已上传 ${size(loaded)} / ${size(total)}`;
        if (loaded >= total) q("#wi-up-cancel").disabled = true;
      });
      activeUpload = { abort: job.abort, alive: () => !o.gone() };
      q("#wi-up-cancel").onclick = () => job.abort();
      job.done.then(s => {
        activeUpload = null;
        if (o.gone()) { discard(s.id); return; }
        showScan(s);
      }).catch(e => {
        activeUpload = null;
        if (o.gone()) return;
        setBusy(false);
        up.innerHTML = e && e.aborted ? `<div class="hint">已取消上传。</div>` : errBox("没能上传这份地图", e);
      });
    }

    function showScan(s) {
      scan = s; busy = false;
      o.hold(s);
      const cands = s.candidates || [];
      if (s.kind !== "java-world" || !cands.length) { drawRejected(s); return; }
      if (cands.length === 1) { o.onPicked(s, cands[0]); return; }
      drawCands();
    }

    /* 不是能导入的 Java 版世界：把原因（基岩版、蓝图、资源包、整合包、RAR…）摆在最前面，到此为止 */
    function drawRejected(s) {
      const g = bySev(s.issues);
      const main = g.block.length ? g.block : [...g.confirm, ...g.warn, ...g.info];
      const rest = g.block.length ? [...g.confirm, ...g.warn, ...g.info] : [];
      root.innerHTML = `
        <div class="wi-reject">
          <div class="err-box" id="wi-reject"><b>${esc(KIND_TITLE[s.kind] || KIND_TITLE.unknown)}，不能当地图导入。</b>
            ${main.length ? issueList(main) : ""}</div>
          <div class="hint">来源：<span>${esc((s.source && s.source.name) || "")}</span></div>
          ${rest.length ? `<div class="wi-sec"><h3>另外注意到</h3>${issueList(rest, "muted")}</div>` : ""}
          <div class="row wi-row"><button type="button" class="btn" id="wi-again">${icon("arrowLeft")} 换一个来源</button></div>
        </div>`;
      q("#wi-again").onclick = () => { drop(); drawSource(); };
    }

    /* 一个来源里有几张地图：列出来让人选，不替人挑第一个 */
    function drawCands() {
      const s = scan, g = bySev(s.issues);
      const notes = [...g.confirm, ...g.warn, ...g.info];
      const row = c => {
        const m = c.meta || {}, cg = bySev(c.issues), name = candName(c);
        const where = c.root ? `<code>${esc(c.root)}</code>` : "来源的最外层";
        const extra = (c.group || []).length ? `（连同 ${c.group.map(x => `<code>${esc(x)}</code>`).join("、")}）` : "";
        const ver = m.versionName ? `保存于 ${esc(m.versionName)}${m.snapshot ? "（快照）" : ""}` : "没有记录版本";
        const rest = [...cg.confirm, ...cg.warn, ...cg.info];
        return `<div class="row-item" data-wi-cand="${esc(c.id)}"><div class="ri-head wi-cand"><span class="ri-ico">${icon("globe")}</span>
          <div class="ri-main">
            <div class="ri-title"><span>${esc(name)}</span>${cg.block.length ? `<span class="tag warn">不能导入</span>` : ""}</div>
            <div class="ri-sub">${ver} · ${size(c.bytes)} · ${esc(LAYOUT[c.layout] || c.layout || "布局不明")}</div>
            <div class="ri-sub">位置：${where}${extra}</div>
            ${cg.block.length ? issueList(cg.block, "block") : ""}
            ${rest.length ? issueList(rest, "muted") : ""}
          </div>
          <div class="ri-right"><button type="button" class="btn" data-wi-pick="${esc(c.id)}" aria-label="用这张地图 ${esc(name)} ${esc(c.root || c.folderName || c.id)}"${cg.block.length ? " disabled" : ""}>用这张</button></div>
        </div></div>`;
      };
      root.innerHTML = `
        <div class="wi-cands">
          <div class="sub">在「<span>${esc((s.source && s.source.name) || "来源")}</span>」里找到 ${s.candidates.length} 张地图，选一张：</div>
          ${notes.length ? issueList(notes, "muted") : ""}
          <div class="group has-ico wi-group">${s.candidates.map(row).join("")}</div>
          <div class="row wi-row"><button type="button" class="btn" id="wi-again">${icon("arrowLeft")} 换一个来源</button></div>
        </div>`;
      root.querySelectorAll("[data-wi-pick]").forEach(b => b.onclick = () => {
        const c = s.candidates.find(x => x.id === b.dataset.wiPick);
        if (c) o.onPicked(s, c);
      });
      q("#wi-again").onclick = () => { drop(); drawSource(); };
    }

    drawSource();
    return { source: drawSource, cands: drawCands };
  }

  /* ---------- 核对页（导入计划）----------
     mode "replace"：替换一台服务器的地图；"create"：用这张地图新建服务器。返回 HTML，说明书按钮由 wireReadme 接。 */
  function planHTML(p, c, scan, mode) {
    // 「地图没有提供下界 / 末地」在「地图里有什么」那一块按 missing 说了，检查结果里同一件事的那一条不再重复列
    const g = bySev((p.issues || []).filter(x => !((p.missing || []).length && x.code === "dims-not-provided"))), m = c.meta || {}, name = candName(c);
    const saved = m.versionName ? `${m.versionName}${m.snapshot ? "（快照）" : ""}` : "";
    const target = `${coreName(p.core)} ${p.mc}`;
    const rel = {
      same: ["ok", "版本一致，可以直接用。"],
      older: ["warn", `地图比服务器旧。第一次启动时，游戏会把这份地图升级到 ${p.mc}，升级以后不能再拿回旧版本用。${mode === "replace" ? "导入前的世界留在恢复点里，不受影响。" : ""}`],
      newer: ["bad", `地图比服务器新，${p.mc} 读不了它，${mode === "replace" ? "不能导入。可以新建一台" + (saved ? " " + saved + " " : "同版本") + "的服务器来用这张地图。" : "不能用这个版本开服。回到「选版本」，选" + (saved ? " " + saved + " 或更新的版本。" : "和地图一样的版本。")}`],
      unknown: ["warn", "判断不了地图和服务器谁新谁旧（地图里没有记录版本，或者版本对不上清单）。要不要继续，看下面需要确认的事项。"],
    }[p.relation] || ["warn", "判断不了地图和服务器的版本关系。"];
    const dims = (c.dimensions || []).map(d =>
      `<li>${dimName(d.id)}<span class="wi-dim-n">${d.regions > 0 ? d.regions + " 个区域文件" : "还没生成过区块"}</span></li>`).join("");
    const clues = [...(m.serverBrands || []), ...(m.modMarkers || [])];
    const kv = (k, v) => `<div class="wi-kv"><span class="wi-k">${k}</span><span class="wi-v">${v}</span></div>`;
    const list = arr => arr.map(x => `<code>${esc(x)}</code>`).join("、");
    return `
      <div class="wi-plan">
        <div class="wi-card">
          <div class="wi-map"><span class="ri-ico">${icon("globe")}</span>
            <div><div class="wi-map-name" id="wi-map-name">${esc(name)}</div>
              <div class="hint">${size(c.bytes)} · ${c.files || 0} 个文件 · ${esc(LAYOUT[c.layout] || c.layout || "布局不明")} · 来源 <span>${esc((scan.source && scan.source.name) || "")}</span></div></div></div>
          <div class="wi-rel ${rel[0]}" id="wi-rel" data-rel="${esc(p.relation || "unknown")}">
            <div class="wi-rel-line"><span>地图保存于 <b>${saved ? esc(saved) : "没有记录"}</b></span>${icon("arrowRight")}<span>${mode === "create" ? "要建的服务器" : "这台服务器"} <b>${esc(target)}</b></span></div>
            <div class="wi-rel-say">${esc(rel[1])}</div>
          </div>
        </div>

        ${g.block.length ? `<div class="err-box wi-block" id="wi-block"><b>${mode === "create" ? "不能用这张地图开服：" : "现在不能导入："}</b>${issueList(g.block)}</div>` : ""}
        ${g.confirm.length ? `<div class="warn-card wi-confirm" id="wi-confirm">
          <div class="wc-title">${icon("warn")} 需要你确认</div>
          ${issueList(g.confirm)}
          <label class="ob-agree wi-check"><input type="checkbox" id="wi-ack"><span>我已了解上面需要确认的事项</span></label>
        </div>` : ""}
        ${g.warn.length ? `<div class="wi-sec" id="wi-warn"><h3>${icon("warn")} 要留意</h3>${issueList(g.warn)}</div>` : ""}

        <div class="wi-sec"><h3>地图里有什么</h3>
          ${kv("维度", dims ? `<ul class="wi-dims">${dims}</ul>` : "没有读到维度")}
          ${(p.missing || []).length ? `<div class="hint wi-missing" id="wi-missing">地图没有提供${esc(p.missing.join("、"))}，不是导丢了：服务器第一次用到时会生成全新的。</div>` : ""}
          ${kv("数据包", (c.datapacks || []).length ? list(c.datapacks) : "没有")}
          ${kv("资源包", c.resourcePack ? "带了 <code>resources.zip</code>" : "没有")}
          ${kv("核心和模组的线索", clues.length || m.wasModded
            ? `${clues.length ? list(clues) : ""}${m.wasModded ? (clues.length ? "；" : "") + "存档标记为用过模组" : ""}`
            : "存档里没有读到线索，无法确认它是不是纯原版")}
          ${kv("玩家进度", c.playerFiles > 0 ? `${c.playerFiles} 个玩家的文件` : "没有玩家文件")}
          ${(c.sourceHints || []).length ? kv("不会导入", `来源里和地图放在一起的 ${list(c.sourceHints)}`) : ""}
        </div>

        ${mode === "replace" ? `
        <div class="wi-sec"><h3>会换掉什么</h3>
          ${(p.replace || []).length
            ? `<div class="wi-p" id="wi-replace">服务器里的 ${list(p.replace)} 会整体移进恢复点，不会删。以后可以在这一页换回来。</div>`
            : `<div class="wi-p" id="wi-replace">这台服务器现在没有世界文件夹，不会换掉任何东西。</div>`}
          <div class="wi-p" id="wi-kept">保持原样：核心和版本、Java、内存、端口、模组和插件、白名单和 OP、其他各项设置。</div>
        </div>
        ${(p.settings || []).length ? `<div class="wi-sec" id="wi-settings"><h3>可以一起改的设置</h3>
          <div class="hint">地图存档里的设置和这台服务器的不一样。默认都不改；勾上的才改，换回旧地图时会一并改回去。</div>
          ${p.settings.map(s => `<label class="ob-agree wi-check"><input type="checkbox" data-wi-set="${esc(s.key)}" aria-label="改设置 ${esc(SET_NAME[s.key] || s.key)}">
            <span><b>${esc(SET_NAME[s.key] || s.key)}</b>：${esc(setVal(s.before))} ${icon("arrowRight")} ${esc(setVal(s.after))} <code>${esc(s.key)}</code>
              <span class="hint">${esc(s.why || "")}</span></span></label>`).join("")}
        </div>` : ""}` : `
        <div class="wi-sec"><h3>会怎么放</h3>
          <div class="wi-p">地图会在服务器建好之前放进去；放不进去就整台不建，不会留下一台自己生成新世界的服务器。来源始终只读。</div>
        </div>`}

        ${(c.readme || []).length ? `<div class="wi-sec" id="wi-readmes"><h3>说明书</h3>
          <div class="hint">地图作者写的文字，只当普通文字显示。里面的网址不要随手打开。</div>
          ${c.readme.map((f, k) => `<div class="wi-readme"><div class="row wi-row"><code>${esc(f)}</code><span class="grow"></span>
            <button type="button" class="btn sm" data-wi-readme="${k}" aria-label="查看说明书 ${esc(f)}">查看</button></div>
            <pre class="wi-readme-text" data-wi-readme-text="${k}" hidden></pre></div>`).join("")}
        </div>` : ""}

        ${g.info.length ? `<div class="wi-sec" id="wi-info"><h3>说明</h3>${issueList(g.info, "muted")}</div>` : ""}

        <div class="hint" id="wi-space">需要大约 ${size(p.needBytes)} 的空间（地图加一点余量），服务器所在的盘还剩 ${size(p.freeBytes)}。</div>

        ${mode === "replace" && p.mustStop ? `<label class="ob-agree wi-check wi-stop"><input type="checkbox" id="wi-stop">
          <span><b>停服并导入</b><span class="hint">服务器现在开着。勾上以后，导入时先让它自己保存并退出（不强制结束）；等不到它停下就放弃导入，一个文件都不换。</span></span></label>` : ""}
      </div>`;
  }
  /* 说明书：点了才去取，放进 <pre> 的 textContent（不可信的文字，不当 HTML，也不认链接） */
  function wireReadme(root, scan, cand, gone) {
    root.querySelectorAll("[data-wi-readme]").forEach(b => b.onclick = async () => {
      const k = b.dataset.wiReadme, pre = root.querySelector(`[data-wi-readme-text="${k}"]`), file = cand.readme[+k];
      if (!pre) return;
      if (!pre.hidden) { pre.hidden = true; b.textContent = "查看"; return; }
      if (pre.dataset.loaded) { pre.hidden = false; b.textContent = "收起"; return; }
      b.disabled = true;
      try {
        const r = await post("/api/world/readme", { scanId: scan.id, candidate: cand.id, file });
        if (gone() || !pre.isConnected) return;
        pre.textContent = String((r && r.text) || "（这份说明是空的）");
        pre.dataset.loaded = "1";
        pre.hidden = false;
        b.textContent = "收起";
      } catch (e) { toastErr("无法读取说明书", e); }
      finally { b.disabled = false; }
    });
  }

  /* ---------- 详情页的「地图」标签 ---------- */
  const POINT = {
    staging: ["neutral", "没走完", "这次导入停在复制阶段，没有动过原来的世界。"],
    staged: ["neutral", "没走完", "地图已经复制好，还没开始换，原来的世界没有动过。"],
    committing: ["warn", "正在换", "正在换地图。"],
    "rolling-back": ["warn", "正在退回", "正在退回原来的世界。"],
    "rolled-back": ["neutral", "没有生效", "这次导入没有生效，服务器用的还是原来的世界。"],
    restoring: ["warn", "正在换回", "正在换回导入前的地图。"],
    restored: ["neutral", "已换回", "已经换回导入前的地图。"],
    stuck: ["warn", "要人处理", "自动恢复没有成功。文件都还在，服务器已暂停启动。"],
    unreadable: ["warn", "记录读不出来", "MineDeck 不知道这次换地图走到了哪一步。为了不在半成品的世界上启动，服务器已暂停启动。文件都还在，处理好之前不要动这个文件夹里的东西。"],
  };
  const DELETABLE = new Set(["committed", "rolled-back", "restored"]);
  /* 一条恢复点现在是什么情况：[标签的样式, 标签, 一句话]。
     能换回的只有一条：这个世界名最近一次导入、而且导入前有旧世界的那一条（后端的 canReturn）。
     多次导入要从最近的一次往前换，乱序换回会把中间那张图困在恢复点里；导入前没有世界的（用地图新建的服务器）没有可以换回的东西。 */
  const sameLevel = (a, b) => String(a || "").toLowerCase() === String(b || "").toLowerCase();
  function pointSay(pt, st) {
    if (pt.state === "committed") {
      if (pt.levelName && !sameLevel(pt.levelName, st.levelName))
        return ["neutral", "已导入", `这次导入的是世界文件夹 ${pt.levelName}，现在的 level-name 不是它，不能自动换回。把 level-name 改回 ${pt.levelName} 就可以。`];
      const newest = (st.points || []).find(x => x.state === "committed" && sameLevel(x.levelName, pt.levelName)); // 列表是新的在前
      const noOld = "这次导入之前服务器还没有世界，没有可以换回的旧地图。";
      if (newest && newest.id !== pt.id) return ["neutral", "更早的导入", "这之后又导入过别的地图。" + (!pt.hasOld ? noOld
        : newest.hasOld ? "要先换回最近那一次，才能换回这一次之前的世界。" : "现在不能直接换回这一次之前的世界。")];
      if (pt.canReturn) return ["", "已导入", "正在用这张地图。导入前的世界留在恢复点里，可以换回。"];
      return ["", "已导入", "正在用这张地图。" + (pt.hasOld ? "现在不能换回导入前的世界。" : noOld)];
    }
    const s = POINT[pt.state] || ["neutral", "状态不明", "读不出这次导入的状态。"];
    if (pt.state === "restored" && pt.keptBytes > 0) return [s[0], s[1], s[2] + "导入的那份地图（包括之后的改动）留在旁边，确认不要了可以删掉。"];
    return s;
  }

  async function render(body, info, gen, refreshHead) {
    const name = info.name, base = `/api/instances/${E(name)}/world`;
    const gone = () => stale(gen) || !body.isConnected;
    if (!applies(info)) { // 代理端没有这个标签；直接打开地址进来的也说清楚
      body.innerHTML = `<div class="sub">代理端（Velocity / Waterfall）本身没有世界，不能导入地图。请到它后面的游戏服务器里导入。</div>`;
      return;
    }
    let app;
    try { app = await api("/api/app"); } catch { app = {}; }
    if (gone()) return;
    const local = app.local !== false;
    let st = null, sig = "", tick = null;

    /* ---- 首页：当前世界、恢复点 ---- */
    const loadHome = async force => {
      let next;
      try { next = await api(base); }
      catch (e) {
        if (gone()) return;
        if (!st || force) { tick = null; body.innerHTML = errBox("无法读取这台服务器的地图信息", e, { actions: `<button type="button" class="btn sm" id="wi-reload">${icon("refresh")} 重试</button>` }); $("#wi-reload", body).onclick = () => loadHome(true); }
        return;
      }
      if (gone()) return;
      const s = JSON.stringify(next);
      if (!force && s === sig) return; // 没变化就不重画，免得打断正在看的人
      st = next; sig = s;
      drawHome();
    };
    const busyHTML = () => {
      const stuck = (st.points || []).flatMap(p => p.stuck || []);
      return `<div class="warn-card wi-busybox" id="wi-busy" role="status"><div class="wc-title">${icon("warn")} <span>${esc(st.busy)}</span></div>
        ${stuck.length ? `<div class="wc-note">文件都还在，没有删任何东西。这些地方要先看一下：</div><ul class="wc-list wi-stuck">${stuck.map(x => `<li>${esc(x)}</li>`).join("")}</ul>` : ""}
        <div class="wc-note">正在换地图的话，等它做完就好，这一页会自己刷新。如果是上次没换完：先关掉占着世界文件夹的程序（在 MineDeck 之外手动启动的服务器、开着存档的地图工具），停止这台服务器，再点「重试恢复」。</div>
        <button type="button" class="btn" id="wi-retry">${icon("refresh")} 重试恢复</button></div>`;
    };
    const pointRow = pt => {
      const bad = pt.state === "unreadable";
      const [cls, tag, say] = pointSay(pt, st), t = when(pt.created), nm = pt.candidate || (bad ? "一次换地图的记录" : "没有名字的地图");
      const where = bad || pt.state === "stuck" ? pt.stuck || [] : []; // 要人看的地方：本机给路径，别的设备上后端只给一句话，都只当文字
      const who = `${nm} ${when(pt.created, true)}`; // 按钮的名字带上是哪一次导入
      const stopped = st.status === "stopped";
      const sizes = [pt.oldBytes > 0 ? `导入前的世界占 ${size(pt.oldBytes)}` : "", pt.keptBytes > 0 ? `留在旁边的导入地图占 ${size(pt.keptBytes)}` : ""].filter(Boolean).join(" · ");
      return `<div class="row-item" data-wi-point="${esc(pt.id)}" data-state="${esc(pt.state)}"><div class="ri-head wi-pt"><span class="ri-ico">${icon("restore")}</span>
        <div class="ri-main">
          <div class="ri-title"><span>${esc(nm)}</span><span class="tag${cls ? " " + cls : ""}">${esc(tag)}</span></div>
          <div class="ri-sub">${esc(say)}</div>
          ${pt.error ? `<div class="ri-sub">${bad ? "读不出来的原因" : "当时的原因"}：<span>${esc(pt.error)}</span></div>` : ""}
          ${where.length ? `<div class="ri-sub wi-where">在这里：${where.map(x => `<span>${esc(x)}</span>`).join("、")}</div>` : ""}
          ${bad ? (sizes ? `<div class="ri-sub">${sizes}</div>` : "")
            : `<div class="ri-sub">${t ? esc(t) + " 导入 · " : ""}来源 <span>${esc(pt.source || "不明")}</span>${sizes ? " · " + sizes : ""}</div>`}
          ${pt.canReturn && !stopped ? `<div class="ri-sub wi-need-stop">要换回，先停止服务器。</div>` : ""}
        </div>
        <div class="ri-right wi-pt-acts">
          ${pt.canReturn ? `<button type="button" class="btn sm" data-wi-restore="${esc(pt.id)}" aria-label="恢复导入前的地图 ${esc(who)}"${stopped && !st.busy ? "" : " disabled"}>${icon("restore")} 恢复导入前的地图</button>` : ""}
          ${DELETABLE.has(pt.state) ? `<button type="button" class="btn sm danger" data-wi-del="${esc(pt.id)}" aria-label="删除恢复点 ${esc(who)}"${st.busy ? " disabled" : ""}>删除恢复点</button>` : ""}
        </div></div></div>`;
    };
    const drawHome = () => {
      const pts = st.points || [];
      body.innerHTML = `
        ${st.busy ? busyHTML() : ""}
        <div class="group has-ico wi-group wi-now"><div class="row-item"><div class="ri-head"><span class="ri-ico">${icon("globe")}</span>
          <div class="ri-main"><div class="ri-title">当前地图</div>
            <div class="ri-sub">世界文件夹 <code id="wi-level">${esc(st.levelName || "world")}</code>（server.properties 的 level-name）· 服务器<span id="wi-status">${esc(ST_TEXT[st.status] || st.status || "")}</span></div></div>
          <div class="ri-right"><button type="button" class="btn primary" id="wi-start"${st.busy ? " disabled" : ""}>${icon("download")} 导入并替换当前地图</button></div>
        </div></div></div>
        <div class="hint wi-lead">把一份现成的 Java 版世界（下载的地图、单人存档、别的服务器的世界）换到这台服务器上。原来的世界不会删，整体留在恢复点里，随时可以换回。核心、版本、模组插件和各项设置都不动。</div>
        <h2 class="wi-h">恢复点</h2>
        <div class="group has-ico wi-group" id="wi-points">${pts.map(pointRow).join("") ||
          `<div class="row-item"><div class="ri-head"><div class="ri-main"><div class="ri-sub">还没有恢复点。导入地图时，原来的世界会留在这里。</div></div></div></div>`}</div>
        ${pts.length ? `<div class="hint wi-lead">恢复点是原样留下的世界文件夹，不在普通备份里，也不会被定时备份挤掉。它一直留着，直到你在这里删掉。</div>` : ""}`;
      /* 自动刷新要省着来：后端每次都要把各个恢复点的文件夹数一遍大小。平时十来秒一次，正在换地图（或卡住）时三秒一次；
         页头看到服务器状态变了（刚停下、刚启动）就马上刷一次，「恢复导入前的地图」能不能点跟着它走 */
      let n = 0;
      tick = () => { n++; if (info.status !== st.status || n % (st.busy ? 2 : 8) === 0) loadHome(false); };
      $("#wi-start", body).onclick = () => startFlow();
      const retry = $("#wi-retry", body);
      if (retry) retry.onclick = async () => {
        retry.disabled = true;
        try { await api(base + "/retry", { method: "POST" }); toast("已经恢复到之前的世界"); }
        catch (e) { toastErr("恢复没有成功", e); }
        if (!gone()) loadHome(true);
      };
      body.querySelectorAll("[data-wi-restore]").forEach(b => b.onclick = () => askRestore(pts.find(x => x.id === b.dataset.wiRestore)));
      body.querySelectorAll("[data-wi-del]").forEach(b => b.onclick = () => askDelete(pts.find(x => x.id === b.dataset.wiDel)));
    };
    const askRestore = async pt => {
      if (!pt) return;
      const r = await confirmModal({
        title: "恢复导入前的地图？", danger: true, icon: "restore", okText: "换回导入前的地图",
        body: `将把「${esc(name)}」换回 ${esc(when(pt.created))} 导入「${esc(pt.candidate || "地图")}」之前的世界。<br>` +
          "现在这张地图（包括导入以后服务器里的所有改动）会移到旁边留着，不会删。<br>服务器必须是停止状态。",
      });
      if (!r || gone()) return;
      try {
        const res = await post(base + "/restore", { id: pt.id, confirm: "restore" });
        if (gone()) return;
        live[name] = { taskId: res.taskId, kind: "restore" };
        saveLive();
        showTask();
      } catch (e) { toastErr("无法换回导入前的地图", e); if (!gone()) loadHome(true); }
    };
    const askDelete = async pt => {
      if (!pt) return;
      const what = pt.state === "committed"
        ? (pt.hasOld ? `删掉的是 ${esc(when(pt.created))} 导入「${esc(pt.candidate || "地图")}」之前的那份世界${pt.oldBytes > 0 ? "（" + size(pt.oldBytes) + "）" : ""}。删了以后就不能再换回导入前的地图。`
          : "这次导入之前服务器还没有世界，删掉的只是这次导入的记录。")
        : pt.state === "restored" ? `删掉的是换回旧地图时留在旁边的那份导入地图${pt.keptBytes > 0 ? "（" + size(pt.keptBytes) + "）" : ""}，连同这次导入的记录。`
        : "这次导入没有生效，删掉的是它留下的记录和临时文件。";
      const r = await confirmModal({
        title: "删除恢复点？", danger: true, icon: "trash", okText: "删除恢复点",
        body: `${what}<br><b>删除后无法恢复。</b>正在用的世界不受影响。`,
      });
      if (!r || gone()) return;
      try { await post(base + "/delete", { id: pt.id, confirm: "delete" }); toast("恢复点已删除"); }
      catch (e) { toastErr("无法删除恢复点", e); }
      if (!gone()) loadHome(true);
    };

    /* ---- 导入：选来源 → 挑地图 → 核对 → 确认 → 任务 ---- */
    function startFlow(again) {
      tick = null;
      body.innerHTML = `
        <div class="wi-flow">
          <div class="wi-flow-head"><h2>导入并替换「<span>${esc(name)}</span>」的地图</h2><span class="grow"></span>
            <button type="button" class="btn ghost sm" id="wi-quit">${icon("x")} 放弃导入</button></div>
          <div id="wi-stage"></div>
        </div>`;
      const stage = $("#wi-stage", body);
      $("#wi-quit", body).onclick = () => { drop(); if (activeUpload) activeUpload.abort(); loadHome(true); };
      const src = sourceUI(stage, {
        local, gone,
        hold: s => hold(s.id, () => !gone()),
        onPicked: (scan, cand) => showPlan(stage, src, scan, cand),
      });
      if (again && again.scan) { hold(again.scan.id, () => !gone()); showPlan(stage, src, again.scan, again.cand); }
    }
    async function showPlan(stage, src, scan, cand) {
      stage.innerHTML = `<div class="wi-busy">${icon("spinner")} <span>正在核对地图和这台服务器…</span></div>`;
      const many = (scan.candidates || []).length > 1;
      const back = () => { if (many) src.cands(); else { drop(); src.source(); } };
      let plan;
      try { plan = await post("/api/world/plan", { scanId: scan.id, candidate: cand.id, inst: name }); }
      catch (e) {
        if (gone()) return;
        stage.innerHTML = errBox("没能核对这张地图", e, { actions: `<button type="button" class="btn sm" id="wi-replan">${icon("refresh")} 重新核对</button>` }) +
          `<div class="row wi-row"><button type="button" class="btn" id="wi-back">${icon("arrowLeft")} ${many ? "换一张地图" : "换一个来源"}</button></div>`;
        $("#wi-replan", stage).onclick = () => showPlan(stage, src, scan, cand);
        $("#wi-back", stage).onclick = back;
        return;
      }
      if (gone()) return;
      stage.innerHTML = planHTML(plan, cand, scan, "replace") + `
        <div id="wi-go-err"></div>
        <div class="wi-acts">
          <button type="button" class="btn" id="wi-back">${icon("arrowLeft")} ${many ? "换一张地图" : "换一个来源"}</button>
          <span class="grow"></span>
          <span class="hint" id="wi-go-why" aria-live="polite"></span>
          <button type="button" class="btn primary" id="wi-go">${icon("download")} 导入并替换当前地图</button>
        </div>`;
      wireReadme(stage, scan, cand, gone);
      const go = $("#wi-go", stage), why = $("#wi-go-why", stage), ackEl = $("#wi-ack", stage), stopEl = $("#wi-stop", stage);
      const upd = () => {
        const ack = !plan.needConfirm || !!(ackEl && ackEl.checked);
        const stop = !plan.mustStop || !!(stopEl && stopEl.checked);
        go.disabled = !!plan.blocked || !ack || !stop;
        why.textContent = plan.blocked ? "有标红的问题，现在不能导入。"
          : !ack ? "先勾选「我已了解上面需要确认的事项」。"
          : !stop ? "服务器开着：勾选「停服并导入」，或者先停止服务器再重新检测。" : "";
      };
      if (ackEl) ackEl.onchange = upd;
      if (stopEl) stopEl.onchange = upd;
      upd();
      $("#wi-back", stage).onclick = back;
      go.onclick = async () => {
        const settings = [...stage.querySelectorAll("[data-wi-set]")].filter(x => x.checked).map(x => x.dataset.wiSet);
        const stopFirst = !!(stopEl && stopEl.checked);
        const repl = (plan.replace || []).map(x => `<code>${esc(x)}</code>`).join("、");
        const r = await confirmModal({
          title: "导入并替换当前地图", danger: true, icon: "download", okText: "替换地图",
          body: (repl ? `将把「${esc(name)}」现在的世界（${repl}）整体移进恢复点，换上地图「${esc(candName(cand))}」。`
              : `「${esc(name)}」现在没有世界文件夹，会直接放进地图「${esc(candName(cand))}」。`) +
            (settings.length ? `<br>同时改这些设置：${settings.map(k => esc(SET_NAME[k] || k)).join("、")}。` : "") +
            (stopFirst ? "<br>服务器会先停止（等它自己保存退出，不强制结束）。" : "") +
            "<br>核心、版本、模组插件和其他设置不变。导入以后可以在这一页换回导入前的地图。",
        });
        if (!r || gone()) return;
        go.disabled = true;
        try {
          const res = await post("/api/world/import", { planId: plan.id, confirm: "replace", acknowledge: !!(ackEl && ackEl.checked), settings, stopFirst });
          keep(scan.id); // 任务还要读这份来源：从这一刻起不能再清
          picks[name] = { scan, cand };
          live[name] = { taskId: res.taskId, kind: "import", scanId: scan.id };
          saveLive();
          if (!gone()) showTask();
        } catch (e) {
          if (gone()) return;
          $("#wi-go-err", stage).innerHTML = errBox("没能开始导入", e, { hint: "没有替换任何文件。", actions: `<button type="button" class="btn sm" id="wi-replan">${icon("refresh")} 重新核对</button>` });
          $("#wi-replan", stage).onclick = () => showPlan(stage, src, scan, cand);
          upd();
        }
      };
    }

    /* ---- 任务进度：导入（复制阶段能取消）、换回旧地图 ---- */
    function showTask() {
      const job = live[name];
      if (!job) { loadHome(true); return; }
      const isImport = job.kind === "import";
      const CELLS = 30;
      body.innerHTML = `
        <div class="wi-flow" id="wi-task">
          <div class="wi-flow-head"><h2 id="wi-task-title"></h2></div>
          <div class="task-steps" id="wi-steps"></div>
          <div class="progress-wrap" id="wi-prog" hidden>
            <div class="progress-head"><b id="wi-prog-label"></b><span class="grow"></span><span class="xp-level num" id="wi-prog-pct"></span></div>
            ${xpBar(CELLS, 0, "lg")}
            <div class="progress-text" id="wi-prog-text"></div>
          </div>
          <div id="wi-task-msg" aria-live="polite"></div>
          <details class="wi-detail wi-log"><summary>过程记录</summary><div class="task-log" id="wi-task-log"></div></details>
          <div class="wi-acts" id="wi-task-acts"></div>
        </div>`;
      $("#wi-task-title", body).textContent = isImport ? `正在把地图导入「${name}」` : `正在把「${name}」换回导入前的地图`;
      let ended = false, loading = false, cancelling = false, actsSig = "";
      const finish = () => { delete live[name]; saveLive(); };
      const poll = async () => {
        if (gone() || ended || loading) return;
        loading = true;
        let t;
        try { t = await api(`/api/tasks/${E(job.taskId)}`); }
        catch (e) {
          if (gone()) return;
          if (e.status === 404) { // 任务记录没了（程序重启过）：回首页，看磁盘上实际是什么状态
            ended = true; finish();
            if (job.scanId) discard(job.scanId);
            loadHome(true);
          }
          return;
        } finally { loading = false; }
        if (gone() || ended) return;
        const steps = t.steps || [], stepsEl = $("#wi-steps", body);
        const stepsHTML = steps.map(s => `<div class="task-step ${esc(s.status)}"><span class="ts-ico">${
          s.status === "done" ? icon("check") : s.status === "running" ? icon("spinner") : s.status === "error" ? icon("x") : `<i class="ts-px"></i>`}</span><span>${esc(s.name)}</span></div>`).join("");
        if (stepsEl.dataset.html !== stepsHTML) { stepsEl.innerHTML = stepsHTML; stepsEl.dataset.html = stepsHTML; }
        const prog = $("#wi-prog", body), showProg = !t.ended && !!t.label && t.total > 0;
        prog.hidden = !showProg;
        if (showProg) {
          const pct = Math.min(100, t.done / t.total * 100), n = Math.floor(pct / 100 * CELLS + 1e-6);
          prog.querySelectorAll(".xp i").forEach((c, k) => { c.className = k < n ? "on" : ""; });
          $("#wi-prog-label", body).textContent = t.label;
          $("#wi-prog-pct", body).textContent = Math.round(pct) + "%";
          $("#wi-prog-text", body).textContent = `${size(t.done)} / ${size(t.total)}`;
        }
        const lg = $("#wi-task-log", body), text = (t.log || []).join("\n");
        if (lg.textContent !== text) lg.textContent = text;
        // 取消只在复制到暂存区之前和期间有用：开始停服、换地图以后，后端会换完或者自己退回
        const run = steps.findIndex(s => s.status === "running");
        const canCancel = isImport && !t.ended && run >= 0 && run <= 1;
        const acts = $("#wi-task-acts", body);
        const sigNow = t.ended ? "ended" : canCancel ? (cancelling ? "cancelling" : "cancel") : isImport ? "late" : "busy";
        if (sigNow !== actsSig) {
          actsSig = sigNow;
          acts.innerHTML = t.ended ? "" : canCancel
            ? `<span class="hint">地图还在复制，原来的世界没有动。</span><span class="grow"></span><button type="button" class="btn" id="wi-cancel"${cancelling ? " disabled" : ""}>${cancelling ? "正在取消…" : "取消导入"}</button>`
            : `<span class="warn-text">${icon("warn")} ${isImport ? "已经开始换地图，不能取消了：会换完，出错就自动退回原来的世界。" : "正在换回，请不要关闭程序。"}</span>`;
          const c = $("#wi-cancel", body);
          if (c) c.onclick = async () => {
            cancelling = true; c.disabled = true; c.textContent = "正在取消…";
            try { await post("/api/world/cancel", { taskId: job.taskId }); }
            catch (e) { cancelling = false; toastErr("无法取消导入", e); actsSig = ""; }
          };
        }
        if (!t.ended) return;
        ended = true;
        finish();
        const msg = $("#wi-task-msg", body);
        if (t.error) {
          msg.innerHTML = `<div class="err-box" id="wi-task-err"><b>${isImport ? "导入没有完成" : "没有换回"}：</b><span id="wi-task-err-text"></span>
            <div class="err-acts"><button type="button" class="btn sm ghost" id="wi-task-copy">${icon("copy")} 复制报错信息</button></div></div>`;
          $("#wi-task-err-text", body).textContent = t.error;
          $("#wi-task-copy", body).onclick = () => copy(String(t.error), "已复制报错信息");
          if (job.scanId) hold(job.scanId, () => !gone()); // 这次检测还在：人可以回去重新核对，走了就清掉
        } else {
          msg.innerHTML = `<div class="wi-done" id="wi-task-done">${icon("check")} <span id="wi-task-done-text"></span></div>`;
          $("#wi-task-done-text", body).textContent = t.result || (isImport ? "地图已经换上。" : "已经换回导入前的地图。");
          if (job.scanId) discard(job.scanId); // 用完了：上传的压缩包可以清掉
        }
        const pick = t.error && job.scanId && picks[name] && picks[name].scan.id === job.scanId ? picks[name] : null;
        if (!pick) delete picks[name];
        acts.innerHTML = `${pick ? `<button type="button" class="btn" id="wi-task-again">${icon("refresh")} 重新核对这张地图</button>` : ""}<span class="grow"></span>
          <button type="button" class="btn primary" id="wi-task-ok">${icon("check")} ${t.error ? "回到地图页" : "完成"}</button>`;
        $("#wi-task-ok", body).onclick = () => { if (t.error) { drop(); delete picks[name]; } loadHome(true); };
        if (pick) $("#wi-task-again", body).onclick = () => startFlow(pick);
        if (refreshHead) refreshHead();
      };
      tick = poll;
      poll();
    }

    startPoll(() => { if (refreshHead) refreshHead(); if (tick) tick(); }, 1500, gen);
    if (live[name]) showTask(); else await loadHome(true);
  }

  /* ---------- 新建向导：用已有地图开服 ----------
     wz：向导里选定的地图。{ scan, cand, plan, planning, planErr, seq, verMissing, created, hint }。
     选定以后按地图预选核心和版本（人可以改）；走到最后一步时按当时的核心和版本生成计划，核对通过才让创建。 */
  let wz = null, wzOpen = false;
  const inWizard = () => /^#\/create\/(new|world)/.test(location.hash || "");
  const wizardMap = () => (wz ? wz.cand : null);

  /* 「新建服务器」页卡片下面的那一行入口。地址 #/create/world 进的是同一个向导，只是选地图的面板一上来就开着 */
  const homeLink = () => `<div class="hint wi-home-link">手里有现成的地图（下载的地图、单人存档、别的服务器的世界）？<a href="#/create/world" id="mode-world">用已有地图开服</a></div>`;
  function wizardOpen() { wzOpen = true; }
  /* 进新建页（renderCreate）时调：向导从头开始，上一次没用掉的检测清掉 */
  function wizardReset() {
    drop(); // 握着的那次检测（选定了的，或者候选列表还开着的）；创建请求被收下时已经清过，这里不会再清一遍
    wz = null; wzOpen = false;
    if (activeUpload) activeUpload.abort();
  }
  /* 存档里的核心线索 → 预选哪种核心。只是建议，人可以在「选核心」里改 */
  function suggestCore(c) {
    const b = ((c.meta && c.meta.serverBrands) || []).join(" ").toLowerCase();
    if (b.includes("neoforge")) return "neoforge";
    if (b.includes("forge")) return "forge";
    if (/fabric|quilt/.test(b)) return "fabric";
    if (/paper|spigot|purpur|bukkit/.test(b)) return "paper";
    return "vanilla";
  }
  /* 版本列表加载出来以后调（app.js 的 loadVersions）：没在用地图开服返回 undefined；在用就返回地图保存的版本，
     列表里没有它返回 null，由人自己选，不悄悄换成最新版 */
  function wizardVersion(all) {
    if (!wz) return undefined;
    const v = wz.cand.meta && wz.cand.meta.versionName;
    const hit = !!v && (all || []).some(x => x.id === v);
    wz.verMissing = !hit;
    paintNote();
    return hit ? v : null;
  }
  function paintNote() {
    const el = $("#wi-wiz-note");
    if (!el || !wz) return;
    const v = wz.cand.meta && wz.cand.meta.versionName;
    el.hidden = !wz.verMissing;
    el.textContent = !wz.verMissing ? ""
      : v ? `这种核心的版本列表里没有 ${v}（地图保存的版本），请自己选一个。选比地图旧的不能用；选更新的，第一次启动时游戏会升级这张地图。`
      : "地图里没有记录保存的版本，请自己选一个。最后一步会核对能不能用。";
  }
  /* 向导每一步画完以后调（app.js 的 wizardShell）：步骤条下面的那一条。
     第一步没选地图时是入口「用已有地图开服」；选了以后每一步都显示选的是哪张，可以换、可以不用 */
  function wizardStrip(wiz, redraw) {
    const host = $("#wiz-body");
    if (!host || !host.parentNode) return;
    const gen = viewGen, gone = () => stale(gen) || !inWizard();
    const el = document.createElement("div");
    el.id = "wi-wiz";
    el.className = "wi-wiz";
    host.parentNode.insertBefore(el, host);
    if (wz) {
      const c = wz.cand, m = c.meta || {};
      el.innerHTML = `<div class="wi-strip chosen" id="wi-wiz-chosen"><span class="ri-ico">${icon("globe")}</span>
        <div class="wi-strip-main"><div class="wi-strip-title">用地图「<span id="wi-wiz-name">${esc(candName(c))}</span>」开服</div>
          <div class="hint">${m.versionName ? "保存于 " + esc(m.versionName) + (m.snapshot ? "（快照）" : "") : "没有记录版本"} · ${size(c.bytes)} · ${
            wiz.step === 0 ? esc(wz.hint || "") + "核心和版本可以改，最后一步会核对这张地图能不能用。"
            : $("#wiz-world") ? "能不能用这个核心和版本开服，核对结果在下面。" : "最后一步会核对这张地图能不能用。"}</div>
          <div class="hint wi-strip-note" id="wi-wiz-note" hidden></div></div>
        <button type="button" class="btn sm" id="wi-wiz-change">换一张</button>
        <button type="button" class="btn sm ghost" id="wi-wiz-drop">不用地图</button></div>`;
      paintNote();
      $("#wi-wiz-change", el).onclick = () => { if (wz && wz.created) return; wizardReset(); wzOpen = true; wiz.step = 0; redraw(); };
      $("#wi-wiz-drop", el).onclick = () => { if (wz && wz.created) return; wizardReset(); redraw(); };
      if (wz.created) lockStrip();
      return;
    }
    if (wiz.step !== 0) { // 没选地图就往后走了：入口只在第一步，开着的选择面板收掉
      if (wzOpen) { wzOpen = false; drop(); if (activeUpload) activeUpload.abort(); }
      el.hidden = true;
      return;
    }
    if (!wzOpen) {
      el.innerHTML = `<div class="wi-strip" id="wi-wiz-offer"><span class="ri-ico">${icon("globe")}</span>
        <div class="wi-strip-main"><div class="wi-strip-title">手里有现成的地图？</div>
          <div class="hint">下载的冒险、空岛、跑酷地图，自己的单人存档，朋友发来的旧服世界，都可以直接拿来开服。</div></div>
        <button type="button" class="btn" id="wi-wiz-pick">${icon("globe")} 用已有地图开服</button></div>`;
      $("#wi-wiz-pick", el).onclick = () => { wzOpen = true; redraw(); };
      return;
    }
    el.innerHTML = `<div class="wi-panel" id="wi-wiz-panel">
      <div class="wi-flow-head"><h2>用已有地图开服</h2><span class="grow"></span>
        <button type="button" class="btn ghost sm" id="wi-wiz-cancel">${icon("x")} 不用地图了</button></div>
      <div class="sub">先选地图。选好以后会按地图预选核心和版本，其余步骤和平常一样。</div>
      <div id="wi-wiz-stage"></div></div>`;
    $("#wi-wiz-cancel", el).onclick = () => { wizardReset(); redraw(); };
    api("/api/app").catch(() => ({})).then(app => {
      if (gone() || !el.isConnected || wz) return;
      sourceUI($("#wi-wiz-stage", el), {
        local: app.local !== false, quiet: true, gone: () => gone() || !el.isConnected,
        hold: s => hold(s.id, inWizard),
        onPicked: (scan, cand) => {
          const core = suggestCore(cand), brands = ((cand.meta && cand.meta.serverBrands) || []).join("、");
          wz = { scan, cand, seq: 0, plan: null,
            hint: core === "vanilla" ? "存档里没有别的核心的线索，先预选了原版。" : `按存档里的线索（${brands}）预选了 ${coreName(core)}。` };
          wzOpen = false;
          hold(scan.id, inWizard);
          wiz.core = core;
          wiz.mc = (cand.meta && cand.meta.versionName) || null;
          wiz.build = null;
          if (cand.meta && cand.meta.snapshot) wiz.snapshots = true;
          wiz.step = 0;
          redraw();
        },
      });
    });
  }
  /* 最后一步（基本设置）画完以后调：按现在选的核心和版本生成计划，把核对结果放进 mount。
     每次进这一步都重新生成：中途改过核心或版本的话，旧计划后端不认 */
  function wizardFinal(mount, wiz) {
    if (!wz || !mount) return;
    const gen = viewGen, my = ++wz.seq, scan = wz.scan, cand = wz.cand, m = cand.meta || {};
    const gone = () => stale(gen) || !wz || my !== wz.seq || !mount.isConnected;
    wz.plan = null; wz.planErr = null; wz.planning = true;
    mount.innerHTML = `<div class="wi-panel wi-wiz-plan" id="wi-wiz-plan"><h3>核对地图</h3>
      <div id="wi-wiz-inner"><div class="wi-busy">${icon("spinner")} <span>正在核对这张地图能不能用 ${esc(coreName(wiz.core))} ${esc(wiz.mc || "")} 开服…</span></div></div>
      <div class="field-err" id="wi-wiz-err"></div></div>`;
    const inner = $("#wi-wiz-inner", mount);
    post("/api/world/plan", { scanId: scan.id, candidate: cand.id, newInst: true, core: wiz.core, mc: wiz.mc }).then(p => {
      if (gone()) return;
      wz.plan = p; wz.planning = false;
      // 地图存档里的难度、模式和上面表单里的不一样时提一句；要不要照地图的来，由人点
      const fd = $("#f-diff"), fm = $("#f-mode");
      const wantD = m.hasDifficulty ? DIFFS[m.difficulty] : "", wantM = m.hasGameType ? MODES[m.gameType] : "";
      const differs = () => (!!fd && !!wantD && fd.value !== wantD) || (!!fm && !!wantM && fm.value !== wantM);
      inner.innerHTML = planHTML(p, cand, scan, "create") + (fd && fm && (wantD || wantM) ? `
        <div class="wi-sec" id="wi-wiz-fill"><div class="row wi-row"><span class="hint">地图存档里${[wantD ? "难度是" + setVal(wantD) : "", wantM ? "默认模式是" + setVal(wantM) : ""].filter(Boolean).join("，")}。上面的设置以你选的为准。</span>
          <span class="grow"></span><button type="button" class="btn sm" id="wi-wiz-fill-go">按地图的填</button></div></div>` : "");
      wireReadme(inner, scan, cand, gone);
      const fill = $("#wi-wiz-fill-go", inner);
      if (fill) {
        fill.disabled = !differs();
        fill.onclick = () => { if (wantD) fd.value = wantD; if (wantM) fm.value = wantM; fill.disabled = true; toast("已按地图存档填好难度和默认游戏模式"); };
        [fd, fm].forEach(x => x.addEventListener("change", () => { if (fill.isConnected) fill.disabled = !differs(); }));
      }
      const ack = $("#wi-ack", inner);
      if (ack) ack.onchange = () => { $("#wi-wiz-err", mount).innerHTML = ""; };
    }).catch(e => {
      if (gone()) return;
      wz.planErr = e; wz.planning = false;
      inner.innerHTML = errBox("没能核对这张地图", e, { actions: `<button type="button" class="btn sm" id="wi-wiz-replan">${icon("refresh")} 重新核对</button>` });
      $("#wi-wiz-replan", inner).onclick = () => wizardFinal(mount, wiz);
    });
  }
  /* 点「开始部署」时调：没在用地图返回 null；核对没过返回 false（原因写在核对框下面，不发创建请求）；
     过了返回要并进创建请求的 { worldPlan, worldAck } */
  function wizardCreateFields() {
    if (!wz) return null;
    const box = $("#wi-wiz-plan"), errEl = $("#wi-wiz-err");
    const fail = msg => {
      if (errEl) errEl.innerHTML = icon("warn") + " " + esc(msg);
      if (box) box.scrollIntoView({ behavior: "smooth", block: "center" });
      return false;
    };
    if (wz.planning) return fail("还在核对地图，等一下再点");
    if (!wz.plan) return fail("地图还没有核对成功。先点上面的「重新核对」，或者换一张地图");
    if (wz.plan.blocked) return fail("这张地图不能用这个核心和版本开服，先看上面标红的原因");
    const ack = box ? $("#wi-ack", box) : null;
    if (wz.plan.needConfirm && !(ack && ack.checked)) return fail("先勾选「我已了解上面需要确认的事项」");
    if (errEl) errEl.innerHTML = "";
    return { worldPlan: wz.plan.id, worldAck: !!(ack && ack.checked) };
  }
  /* 创建请求被收下了：后端把计划和来源都钉在新建任务上（上传的文件等任务结束才删），
     这里就可以告诉它检测结果不要了，不用等它自己过期。请求还没被收下之前不能清 */
  function wizardCreated() {
    if (!wz) return;
    wz.created = true;
    drop();
    lockStrip();
  }
  /* 服务器已经在建了：「换一张」「不用地图」不能再点。待装清单里有东西时向导会留在这一页等建完再装（app.js 那时也按住了「上一步」和步骤条），
     这时重画向导，后面那一步就没人接着做了 */
  function lockStrip() {
    for (const id of ["#wi-wiz-change", "#wi-wiz-drop"]) { const b = $(id); if (b) b.disabled = true; }
  }
  /* 创建请求被拒了：是计划的问题（过期、用过了、核心版本对不上）就重新核对一遍。
     别的原因（比如重名）计划还在后端那儿，核对结果和勾过的确认都留着，改好再点就行 */
  function wizardCreateFailed(e, wiz) {
    if (!wz || wz.created) return;
    if (/计划|重新检测|过期|用过了|重新选择地图/.test(String((e && e.message) || ""))) wizardFinal($("#wiz-world"), wiz);
  }

  return { TAB, TAB_LABEL, applies, render, homeLink, wizardOpen, wizardReset, wizardMap, wizardVersion, wizardStrip, wizardFinal, wizardCreateFields, wizardCreated, wizardCreateFailed };
})();
