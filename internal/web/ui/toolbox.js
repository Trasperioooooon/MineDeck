/* MineDeck 工具箱（前端注册表与工具页面）：
   - 首页（#/toolbox）：以工具卡片网格列出已挂载的联机辅助工具（可扩展注册表 TOOLS）。
   - 工具一（#/toolbox/skin-pack）：离线皮肤包生成，预演离线 UUID / 默认皮肤槽位 / 撞槽解决，上传 PNG 并导出标准客户端资源包。
   - 工具二（#/toolbox/mrpack-export）：导出 Modrinth 整合包（.mrpack），支持从已有模组服务器或草稿清单配置、复用 MDModCart 挑选模组、区分在线下载项与 overrides。 */
"use strict";

window.MDToolbox = (() => {
  const MAX_SKIN_BYTES = 2 << 20;
  const MAX_SKINS = 18;
  const MOD_CORES = new Set(["fabric", "forge", "neoforge", "mohist", "banner"]);
  const MC_PRESETS = ["26.3", "26.2", "26.1", "1.21.11", "1.21.4", "1.20.4", "1.20.1", "1.19.4", "1.19.2", "1.18.2", "1.12.2", "1.8.9"];

  const TOOLS = [
    {
      id: "skin-pack",
      title: "离线皮肤包生成",
      tag: "客户端资源包",
      ico: "player",
      desc: "把离线服里朋友们的皮肤 PNG 打包成客户端资源包，按玩家名离线 UUID 命中各自默认皮肤，或者按顺序替换。",
      summary: "支持 64×64 与旧版 64×32 自动扩充 · 算好槽位、pack.mcmeta 与 SHA-1",
      render: renderSkinPack,
    },
    {
      id: "mrpack-export",
      title: "导出 Modrinth 整合包",
      tag: ".mrpack 分享包",
      ico: "package",
      desc: "把模组和配置打包成标准 .mrpack 发给朋友，可以从已有的模组服务器直接导出，也可以自己现挑模组，前置由 MineDeck 找齐。",
      summary: "支持 Modrinth CDN 与本地 overrides · 仅客户端模组自动标 server unsupported",
      render: renderMrpackExport,
    },
  ];

  const dlUrlWithToken = (basePath, id) =>
    `${basePath}/${encodeURIComponent(id)}${TOKEN() ? "?token=" + encodeURIComponent(TOKEN()) : ""}`;

  function render(toolId) {
    const gen = ++viewGen;
    if (typeof pollTimer !== "undefined" && pollTimer) { clearInterval(pollTimer); pollTimer = null; }
    if (!toolId) {
      renderHome();
      return;
    }
    const tool = TOOLS.find(t => t.id === toolId);
    if (!tool) {
      Main().innerHTML = pageHead(
        "找不到这个工具",
        "工具箱里还没有这个功能，回工具箱首页选一个试试。",
        `<a class="btn primary" href="#/toolbox">${icon("arrowLeft")} 回到工具箱</a>`,
        `<a href="#/toolbox">工具箱</a><span class="sep">/</span><span>未找到</span>`
      );
      return;
    }
    tool.render(gen);
  }

  function renderHome() {
    Main().innerHTML =
      pageHead(
        "工具箱",
        "常用联机辅助工具，生成的资源包与整合包可直接下载或按需要保存到本机目录。",
        "",
        "",
        "联机辅助",
        "tb-eyebrow"
      ) +
      `<div class="cards tb-cards" role="list">
        ${TOOLS.map(t => `
          <a href="#/toolbox/${esc(t.id)}" class="card tb-card" data-tool="${esc(t.id)}" role="listitem" aria-label="${esc(t.title)}：${esc(t.summary)}">
            <div class="tb-card-head">
              <span class="tb-card-ico">${icon(t.ico)}</span>
              <div class="tb-card-meta">
                <div class="tb-card-title">${esc(t.title)}</div>
                <span class="badge">${esc(t.tag)}</span>
              </div>
              <span class="tb-card-go" aria-hidden="true">${icon("chevronRight")}</span>
            </div>
            <p class="tb-card-desc">${esc(t.desc)}</p>
            <div class="hint tb-card-sub">${esc(t.summary)}</div>
          </a>
        `).join("")}
      </div>`;
  }

  /* ========================================================================
     工具一：离线皮肤包生成（#/toolbox/skin-pack）
     ======================================================================== */
  async function inspectSkinPNG(file) {
    if (!file) return { err: "请选择皮肤 PNG 文件" };
    if (file.size > MAX_SKIN_BYTES) {
      return { err: `图片体积 ${fmtBytes(file.size)} 超过了 2 MB 上限` };
    }
    try {
      const head = new Uint8Array(await file.slice(0, 8).arrayBuffer());
      const sig = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
      if (head.length < 8 || sig.some((b, idx) => head[idx] !== b)) {
        return { err: "文件头不是标准 PNG（请提供未经改后缀的 .png 皮肤图片）" };
      }
    } catch {
      return { err: "没能读取文件内容" };
    }
    const url = URL.createObjectURL(file);
    try {
      const dims = await new Promise((resolve, reject) => {
        const img = new Image();
        img.onload = () => resolve({ w: img.naturalWidth, h: img.naturalHeight });
        img.onerror = () => reject(new Error("decode"));
        img.src = url;
      });
      if (dims.w === 64 && dims.h === 64) {
        return { w: 64, h: 64, url, legacy: false, err: "" };
      }
      if (dims.w === 64 && dims.h === 32) {
        return { w: 64, h: 32, url, legacy: true, err: "" };
      }
      URL.revokeObjectURL(url);
      return { err: `皮肤尺寸必须是 64×64 或旧版 64×32（当前是 ${dims.w}×${dims.h}）` };
    } catch {
      URL.revokeObjectURL(url);
      return { err: "PNG 图片已损坏或无法解码" };
    }
  }

  function renderSkinPack(gen) {
    const newRow = (player = "") => ({
      id: Math.random().toString(36).slice(2, 9),
      player,
      model: "",
      slot: -1,
      keep: false,
      skip: false,
      file: null,
      fileName: "",
      thumbUrl: "",
      w: 0,
      h: 0,
      legacy: false,
      fileErr: "",
    });

    const st = {
      mc: "1.21.4",
      packName: "朋友皮肤包",
      description: "",
      mode: "player",
      destDir: "",
      local: true,
      instances: [],
      rows: [newRow("")],
      preview: null,
      previewErr: null,
      building: false,
      buildErr: null,
      result: null,
    };

    const revokeThumbs = () => {
      for (const r of st.rows) {
        if (r.thumbUrl) URL.revokeObjectURL(r.thumbUrl);
      }
    };

    Main().innerHTML =
      pageHead(
        "离线皮肤包生成",
        "为离线服玩家分配对应默认皮肤槽位并生成客户端资源包（不修改服务器本体文件）。",
        `<a class="btn" href="#/toolbox">${icon("arrowLeft")} 回到工具箱</a>`,
        `<a href="#/toolbox">工具箱</a><span class="sep">/</span><span>离线皮肤包生成</span>`,
        "客户端资源包"
      ) +
      `<div class="tb-wrap">
        <div class="group tb-sec">
          <div class="tb-sec-head"><h2>1. 目标版本与分配方式</h2><span id="sp-ver-badge" class="badge">正在计算槽位…</span></div>
          <div class="form-grid tb-grid">
            <label class="field">
              <span>Minecraft 版本</span>
              <input type="text" id="sp-mc" value="${esc(st.mc)}" list="sp-mc-list" placeholder="如 1.21.4 或 1.20.1" aria-label="目标 Minecraft 版本" maxlength="40">
              <datalist id="sp-mc-list">${MC_PRESETS.map(v => `<option value="${esc(v)}">`).join("")}</datalist>
            </label>
            <div class="field">
              <span>分配模式</span>
              <div class="seg" id="sp-mode" role="group" aria-label="皮肤槽位分配模式">
                <button type="button" class="on" data-mode="player" aria-pressed="true">按玩家名算默认槽位</button>
                <button type="button" data-mode="sequential" aria-pressed="false">按顺序替换</button>
              </div>
            </div>
            <label class="field">
              <span>材质包名称</span>
              <input type="text" id="sp-packname" value="${esc(st.packName)}" placeholder="朋友皮肤包" aria-label="材质包名称" maxlength="100">
            </label>
            <label class="field">
              <span>pack.mcmeta 描述（可选）</span>
              <input type="text" id="sp-desc" value="" placeholder="留空自动写上目标版本与日期" aria-label="材质包描述" maxlength="200">
            </label>
          </div>
          <div class="tb-import-bar" id="sp-inst-bar" hidden>
            <span class="hint">${icon("server")} 也可以从已有服务器读取在线 / 白名单 / 管理员玩家名：</span>
            <select id="sp-inst-pick" aria-label="选择要导入玩家名单的服务器"></select>
            <button type="button" class="btn sm" id="sp-import-players">${icon("player")} 读取服务器玩家</button>
          </div>
        </div>

        <div class="group tb-sec">
          <div class="tb-sec-head">
            <h2>2. 皮肤列表 <span class="hint" id="sp-row-count">1 / ${MAX_SKINS}</span></h2>
            <button type="button" class="btn sm" id="sp-add-row">${icon("plus")} 添加一条皮肤</button>
          </div>
          <div class="hint" id="sp-mode-tip" style="margin-bottom:12px">按玩家名离线 UUID 自动算出 Minecraft 客户端会加载的默认皮肤槽位；每位玩家上传一张 64×64 或旧版 64×32 PNG。</div>
          <div id="sp-rows" class="tb-skin-rows"></div>
        </div>

        <div class="group tb-sec">
          <div class="tb-sec-head"><h2>3. 槽位预演与生成</h2><span id="sp-packfmt-badge" class="badge"></span></div>
          <div id="sp-preview-area"></div>
          <div id="sp-host-row" class="tb-host-row" hidden>
            <label class="field tb-dir-field">
              <span>同步保存到本机文件夹（可选）</span>
              <div class="row">
                <input type="text" id="sp-destdir" value="" placeholder="留空只提供浏览器下载；选填本机目录可一步存盘" aria-label="同步保存皮肤包的本机文件夹路径" maxlength="400">
                <button type="button" class="btn sm" id="sp-pickdir" aria-label="浏览皮肤包保存文件夹">${icon("folder")} 浏览…</button>
              </div>
            </label>
          </div>
          <div class="row tb-act-row">
            <button type="button" class="btn primary" id="sp-build">${icon("package")} 生成皮肤资源包（.zip）</button>
            <span class="hint" id="sp-build-hint"></span>
          </div>
          <div id="sp-build-err"></div>
          <div id="sp-result"></div>
        </div>
      </div>`;

    const mcIn = $("#sp-mc"), packIn = $("#sp-packname"), descIn = $("#sp-desc");
    const modeBox = $("#sp-mode"), addBtn = $("#sp-add-row"), rowsEl = $("#sp-rows");
    const prevArea = $("#sp-preview-area"), buildBtn = $("#sp-build"), buildErrEl = $("#sp-build-err"), resultEl = $("#sp-result");
    const verBadge = $("#sp-ver-badge"), fmtBadge = $("#sp-packfmt-badge"), rowCountEl = $("#sp-row-count"), modeTipEl = $("#sp-mode-tip");
    const hostRow = $("#sp-host-row"), destIn = $("#sp-destdir"), pickDirBtn = $("#sp-pickdir");
    const instBar = $("#sp-inst-bar"), instPick = $("#sp-inst-pick"), importBtn = $("#sp-import-players");
    const instMc = new Map(); // 服务器名 → MC 版本：从哪台读名单，版本就跟着改成哪台的（回访走查 11-16：默认 1.21.4 没跟着变，差点做错版本）

    let previewTimer = null;
    let previewSeq = 0;

    function rowPreviewPayload() {
      if (st.mode === "player") {
        const origIdx = [];
        const entries = [];
        st.rows.forEach((r, idx) => {
          if (r.player.trim()) {
            origIdx.push(idx);
            entries.push({ player: r.player.trim(), model: r.model, slot: -1, keep: !!r.keep, skip: !!r.skip });
          }
        });
        return { entries, origIdx };
      }
      return {
        origIdx: st.rows.map((_, idx) => idx),
        entries: st.rows.map(r => ({
          player: r.player.trim(),
          model: r.model,
          slot: Number.isInteger(r.slot) ? r.slot : -1,
          keep: !!r.keep,
          skip: !!r.skip,
        })),
      };
    }

    function itemByRowIdx(idx) {
      if (!st.preview || !st.preview.items) return null;
      const { origIdx } = rowPreviewPayload();
      const k = origIdx.indexOf(idx);
      return k >= 0 ? st.preview.items[k] : null;
    }

    function updateRowBadges() {
      st.rows.forEach((r, idx) => {
        const el = $(`#sp-row-slot-${idx}`);
        if (!el) return;
        const item = itemByRowIdx(idx);
        if (!item) {
          el.innerHTML = st.mode === "player" && !r.player.trim() ? `<span class="hint">填写玩家名后显示分配槽位</span>` : "";
          return;
        }
        const cls = item.skipped ? "badge" : item.conflict ? "badge warn" : "badge core";
        const slotTxt = item.slot.index < 0 ? "不分槽位" : `${item.slot.id}（${item.slot.modelLabel}）`;
        const extra = item.skipped ? " · 已跳过" : item.conflict ? " · 撞槽" : "";
        const uuidTxt = item.uuid ? ` <span class="mono hint">${esc(item.uuid.slice(0, 8))}…</span>` : "";
        el.innerHTML = `<span class="${cls}">${esc(slotTxt)}${esc(extra)}</span>${uuidTxt}`;
      });
    }

    function renderRows() {
      rowCountEl.textContent = `${st.rows.length} / ${MAX_SKINS}`;
      addBtn.disabled = st.rows.length >= MAX_SKINS;
      const slots = (st.preview && st.preview.slots) || [];
      rowsEl.innerHTML = st.rows.map((r, idx) => {
        const n = idx + 1;
        const fileNote = r.fileErr
          ? `<div class="tb-err-line">${icon("warn")} <span>${esc(r.fileErr)}</span></div>`
          : r.file
            ? `<div class="hint">${esc(r.fileName)} · ${r.w}×${r.h}${r.legacy ? "（旧版 64×32：打包时自动镜像扩展为 64×64）" : ""}</div>`
            : `<div class="hint">未选择皮肤 PNG（支持 64×64 或旧版 64×32，不超过 2 MB）</div>`;
        return `
          <div class="tb-skin-row" data-idx="${idx}">
            <div class="tb-skin-thumb">
              ${r.thumbUrl ? `<img src="${esc(r.thumbUrl)}" alt="第 ${n} 条皮肤预览" width="40" height="40">` : icon("player")}
            </div>
            <div class="tb-skin-main">
              <div class="tb-skin-top">
                <span class="tb-row-num">#${n}</span>
                ${st.mode === "player"
                  ? `<input type="text" class="sp-player" data-idx="${idx}" value="${esc(r.player)}" placeholder="玩家名（区分大小写）" aria-label="第 ${n} 条玩家名" maxlength="32">`
                  : `<input type="text" class="sp-player" data-idx="${idx}" value="${esc(r.player)}" placeholder="备注玩家名（可选）" aria-label="第 ${n} 条玩家备注" maxlength="32">
                     <select class="sp-model" data-idx="${idx}" aria-label="第 ${n} 条手臂模型">
                       <option value=""${r.model === "" ? " selected" : ""}>自动手臂</option>
                       <option value="wide"${r.model === "wide" ? " selected" : ""}>经典（4px）</option>
                       <option value="slim"${r.model === "slim" ? " selected" : ""}>纤细（3px）</option>
                     </select>
                     <select class="sp-slot" data-idx="${idx}" aria-label="第 ${n} 条目标槽位">
                       <option value="-1"${r.slot < 0 ? " selected" : ""}>自动顺位分配</option>
                       ${slots.map(s => `<option value="${s.index}"${r.slot === s.index ? " selected" : ""}>#${s.index} ${esc(s.id)}（${esc(s.modelLabel)}）</option>`).join("")}
                     </select>`}
                <div class="tb-skin-btns">
                  <button type="button" class="btn sm sp-pick" data-idx="${idx}" aria-label="为第 ${n} 条选择皮肤 PNG">${icon("folder")} ${r.file ? "更换皮肤" : "选择皮肤 PNG"}</button>
                  <input type="file" class="sp-file" data-idx="${idx}" accept="image/png,.png" hidden>
                  <button type="button" class="btn sm ghost sp-del" data-idx="${idx}" aria-label="移除第 ${n} 条皮肤"${st.rows.length <= 1 ? " disabled" : ""}>${icon("trash")}</button>
                </div>
              </div>
              <div class="tb-skin-sub">
                ${fileNote}
                <div id="sp-row-slot-${idx}" class="tb-row-slot"></div>
              </div>
            </div>
          </div>`;
      }).join("");
      updateRowBadges();

      rowsEl.querySelectorAll(".sp-player").forEach(inp => {
        inp.oninput = () => {
          const i = +inp.dataset.idx;
          if (!st.rows[i]) return;
          st.rows[i].player = inp.value;
          st.rows[i].keep = false;
          st.rows[i].skip = false;
          schedulePreview(180);
        };
      });
      rowsEl.querySelectorAll(".sp-model").forEach(sel => {
        sel.onchange = () => {
          const i = +sel.dataset.idx;
          if (!st.rows[i]) return;
          st.rows[i].model = sel.value;
          schedulePreview(0);
        };
      });
      rowsEl.querySelectorAll(".sp-slot").forEach(sel => {
        sel.onchange = () => {
          const i = +sel.dataset.idx;
          if (!st.rows[i]) return;
          st.rows[i].slot = parseInt(sel.value, 10);
          st.rows[i].keep = false;
          st.rows[i].skip = false;
          schedulePreview(0);
        };
      });
      rowsEl.querySelectorAll(".sp-pick").forEach(btn => {
        btn.onclick = () => {
          const i = +btn.dataset.idx;
          const fileInput = rowsEl.querySelector(`.sp-file[data-idx="${i}"]`);
          if (fileInput) fileInput.click();
        };
      });
      rowsEl.querySelectorAll(".sp-file").forEach(fi => {
        fi.onchange = async () => {
          const i = +fi.dataset.idx;
          const file = fi.files && fi.files[0];
          const row = st.rows[i];
          if (!file || !row) return;
          if (row.thumbUrl) { URL.revokeObjectURL(row.thumbUrl); row.thumbUrl = ""; }
          row.file = null;
          row.fileName = file.name;
          row.w = 0; row.h = 0; row.legacy = false;
          const check = await inspectSkinPNG(file);
          if (stale(gen) || !st.rows[i]) { if (check.url) URL.revokeObjectURL(check.url); return; }
          if (check.err) {
            row.fileErr = check.err;
          } else {
            row.file = file;
            row.thumbUrl = check.url;
            row.w = check.w; row.h = check.h; row.legacy = check.legacy;
            row.fileErr = "";
          }
          renderRows();
          refreshBuildButton();
        };
      });
      rowsEl.querySelectorAll(".sp-del").forEach(btn => {
        btn.onclick = () => {
          const i = +btn.dataset.idx;
          if (st.rows.length <= 1 || !st.rows[i]) return;
          if (st.rows[i].thumbUrl) URL.revokeObjectURL(st.rows[i].thumbUrl);
          st.rows.splice(i, 1);
          renderRows();
          schedulePreview(0);
        };
      });
    }

    function renderPreviewBox() {
      updateRowBadges();
      if (st.previewErr) {
        verBadge.textContent = "版本需检查";
        fmtBadge.textContent = "";
        prevArea.innerHTML = errBox("无法预演皮肤槽位分配", st.previewErr, { label: "皮肤槽位预演" });
        refreshBuildButton();
        return;
      }
      const p = st.preview;
      if (!p) {
        prevArea.innerHTML = `<div class="hint">正在计算默认皮肤槽位…</div>`;
        refreshBuildButton();
        return;
      }
      verBadge.textContent = p.modernSlots ? `18 个默认槽位（1.19.3+）` : `2 个默认槽位（Steve / Alex）`;
      fmtBadge.textContent = p.minFormat
        ? `min_format ${p.minFormat} · pack_format ${p.packFormat}`
        : `pack_format ${p.packFormat}`;

      const notesHtml = (p.notes || []).map(n => `<div class="hint tb-note">${icon("info")} <span>${esc(n)}</span></div>`).join("");
      let conflictHtml = "";
      if (p.conflicts && p.conflicts.length) {
        const { origIdx } = rowPreviewPayload();
        conflictHtml = `
          <div class="warn-card" id="sp-conflicts" role="alert">
            <div class="wc-title">${icon("warn")} 发现 ${p.conflicts.length} 处默认皮肤槽位冲突</div>
            <div class="wc-note">同一个默认皮肤槽位在一个资源包里只能放 1 张贴图。请选择要保留哪一位（其余自动跳过），或切回按顺序模式手动挑不同槽位。</div>
            ${p.conflicts.map(c => `
              <div class="tb-conflict-item">
                <div class="tb-conflict-title">槽位 <code>${esc(c.slotId)}</code>（${esc(c.model === "slim" ? "纤细 3px" : "经典 4px")}）· 路径 <code>${esc(c.path)}</code></div>
                <div class="tb-conflict-btns">
                  ${(c.entries || []).map(pIdx => {
                    const rowIdx = origIdx[pIdx];
                    const row = st.rows[rowIdx] || {};
                    const label = (row.player && row.player.trim()) || `第 ${rowIdx + 1} 条`;
                    return `<button type="button" class="btn sm sp-resolve-keep" data-slot="${c.slotIndex}" data-ridx="${rowIdx}" data-entries="${esc((c.entries || []).map(e => origIdx[e]).join(","))}" aria-label="在槽位 ${esc(c.slotId)} 冲突中保留 ${esc(label)}（第 ${rowIdx + 1} 条）">${icon("check")} 只保留 ${esc(label)}（#${rowIdx + 1}）</button>`;
                  }).join("")}
                </div>
              </div>
            `).join("")}
          </div>`;
      }

      const items = p.items || [];
      const tableHtml = items.length ? `
        <div class="tb-table-wrap">
          <table class="tbl">
            <thead>
              <tr><th>行</th><th>玩家 / 状态</th><th>命中槽位</th><th>资源包内路径</th>${p.mode === "player" ? "<th>离线 UUID</th>" : ""}</tr>
            </thead>
            <tbody>
              ${items.map(it => {
                const { origIdx } = rowPreviewPayload();
                const rowIdx = origIdx[it.index] ?? it.index;
                const statusTag = it.skipped ? `<span class="badge">已跳过</span>`
                  : it.conflict ? `<span class="badge warn">撞槽</span>`
                  : `<span class="badge core">正常</span>`;
                return `<tr>
                  <td>#${rowIdx + 1}</td>
                  <td>${esc(it.player || "顺位皮肤")} ${statusTag}</td>
                  <td>${it.slot.index < 0 ? "—" : `<code>${esc(it.slot.id)}</code>（${esc(it.slot.modelLabel)}）`}</td>
                  <td class="mono">${it.slot.index < 0 ? "—" : esc(it.slot.path)}</td>
                  ${p.mode === "player" ? `<td class="mono">${esc(it.uuid || "")}</td>` : ""}
                </tr>`;
              }).join("")}
            </tbody>
          </table>
        </div>` : `<div class="hint">添加皮肤并填写信息后，这里会实时显示每个皮肤对应的贴图路径。</div>`;

      prevArea.innerHTML = notesHtml + conflictHtml + tableHtml;
      prevArea.querySelectorAll(".sp-resolve-keep").forEach(btn => {
        btn.onclick = () => {
          const keepIdx = +btn.dataset.ridx;
          const group = (btn.dataset.entries || "").split(",").map(s => +s).filter(n => !Number.isNaN(n));
          for (const i of group) {
            if (!st.rows[i]) continue;
            if (i === keepIdx) {
              st.rows[i].keep = true;
              st.rows[i].skip = false;
            } else {
              st.rows[i].keep = false;
              st.rows[i].skip = true;
            }
          }
          renderRows();
          schedulePreview(0);
        };
      });
      refreshBuildButton();
    }

    function refreshBuildButton() {
      const hint = $("#sp-build-hint");
      const active = st.rows.filter(r => !r.skip);
      const hasFileErr = active.some(r => !!r.fileErr);
      const missingFile = active.some(r => !r.file);
      const missingPlayer = st.mode === "player" && active.some(r => !r.player.trim());
      const hasConflict = !!(st.preview && st.preview.conflicts && st.preview.conflicts.length > 0);
      buildBtn.disabled = st.building || !!st.previewErr || active.length === 0 || hasFileErr || missingFile || missingPlayer || hasConflict;
      if (st.building) hint.textContent = "正在生成资源包…";
      else if (st.previewErr) hint.textContent = "请先修正 Minecraft 版本或填写内容";
      else if (hasConflict) hint.textContent = "请先在上方解决撞槽冲突后再生成";
      else if (hasFileErr) hint.textContent = "请先替换校验失败的皮肤 PNG";
      else if (missingPlayer) hint.textContent = "按玩家名模式下，每条未跳过的皮肤都要填写玩家名";
      else if (missingFile) hint.textContent = "请为每条未跳过的皮肤选择一张 PNG 图片";
      else hint.textContent = `已就绪，将打包 ${active.length} 张皮肤`;
    }

    async function runPreview() {
      const seq = ++previewSeq;
      const payload = rowPreviewPayload();
      try {
        const res = await api("/api/toolbox/skin-pack/preview", {
          method: "POST",
          body: { mc: mcIn.value.trim() || "1.21.4", mode: st.mode, entries: payload.entries },
        });
        if (stale(gen) || seq !== previewSeq) return;
        const prevSlotCount = st.preview ? st.preview.slotCount : 0;
        st.preview = res;
        st.previewErr = null;
        renderPreviewBox();
        if (st.mode === "sequential" && prevSlotCount !== res.slotCount) renderRows();
      } catch (e) {
        if (stale(gen) || seq !== previewSeq) return;
        st.preview = null;
        st.previewErr = e;
        renderPreviewBox();
      }
    }

    function schedulePreview(ms) {
      if (previewTimer) clearTimeout(previewTimer);
      if (ms <= 0) runPreview();
      else previewTimer = setTimeout(runPreview, ms);
    }

    mcIn.oninput = () => { st.mc = mcIn.value; schedulePreview(180); };
    packIn.oninput = () => { st.packName = packIn.value; };
    descIn.oninput = () => { st.description = descIn.value; };
    destIn.oninput = () => { st.destDir = destIn.value; };

    modeBox.querySelectorAll("button[data-mode]").forEach(b => {
      b.onclick = () => {
        const m = b.dataset.mode;
        if (m === st.mode) return;
        const kr = typeof keyRects === "function" ? keyRects() : null;
        st.mode = m;
        for (const r of st.rows) { r.keep = false; r.skip = false; }
        modeBox.querySelectorAll("button").forEach(x => {
          const on = x.dataset.mode === m;
          x.classList.toggle("on", on);
          x.setAttribute("aria-pressed", on ? "true" : "false");
        });
        if (typeof slideKeys === "function" && kr) slideKeys(kr, ["seg"]);
        modeTipEl.textContent = m === "player"
          ? "按玩家名离线 UUID 自动算出 Minecraft 客户端会加载的默认皮肤槽位；每位玩家上传一张 64×64 或旧版 64×32 PNG。"
          : "按顺序或手动挑槽位放进资源包里；适合给特定默认角色外观整体换皮。";
        renderRows();
        schedulePreview(0);
      };
    });

    addBtn.onclick = () => {
      if (st.rows.length >= MAX_SKINS) return;
      st.rows.push(newRow(""));
      renderRows();
      schedulePreview(0);
    };

    pickDirBtn.onclick = async () => {
      try {
        const r = await api("/api/pickdir", { method: "POST" });
        if (!stale(gen) && r && r.path) {
          destIn.value = r.path;
          st.destDir = r.path;
        }
      } catch (e) { toastErr("无法打开文件夹选择窗口", e); }
    };

    importBtn.onclick = async () => {
      const name = instPick.value;
      if (!name) return;
      importBtn.disabled = true;
      try {
        const pl = await api(`/api/instances/${encodeURIComponent(name)}/players`);
        if (stale(gen)) return;
        const names = [];
        const seen = new Set();
        const add = n => { const t = String(n || "").trim(); if (t && !seen.has(t)) { seen.add(t); names.push(t); } };
        (pl.online || []).forEach(add);
        (pl.whitelist || []).forEach(x => add(x.name || x));
        (pl.ops || []).forEach(x => add(x.name || x));
        if (!names.length) {
          toast("这台服务器里还没有记录到任何玩家");
          return;
        }
        // 名单读到了才改版本：读失败、没人时不动用户填的
        const existing = new Set(st.rows.map(r => r.player.trim()).filter(Boolean));
        let added = 0;
        for (const p of names) {
          if (existing.has(p)) continue;
          const emptySlot = st.rows.find(r => !r.player.trim() && !r.file);
          if (emptySlot) {
            emptySlot.player = p;
          } else if (st.rows.length < MAX_SKINS) {
            st.rows.push(newRow(p));
          } else {
            break;
          }
          existing.add(p);
          added++;
        }
        const mc = instMc.get(name) || "", mcChanged = !!mc && mcIn.value.trim() !== mc;
        if (mcChanged) { mcIn.value = st.mc = mc; }
        const verNote = mcChanged ? `，版本改成了这台的 ${mc}` : "";
        toast(added ? `已从「${name}」填入 ${added} 位玩家${verNote}` : `名单里的玩家都已经填在列表里了${verNote}`);
        renderRows();
        schedulePreview(0);
      } catch (e) {
        toastErr("没能读取服务器玩家名单", e);
      } finally {
        if (!stale(gen)) importBtn.disabled = false;
      }
    };

    buildBtn.onclick = async () => {
      st.building = true;
      st.buildErr = null;
      buildErrEl.innerHTML = "";
      refreshBuildButton();
      try {
        const fd = new FormData();
        const entries = st.rows.map(r => ({
          player: r.player.trim(),
          model: r.model,
          slot: Number.isInteger(r.slot) ? r.slot : -1,
          keep: !!r.keep,
          skip: !!r.skip,
        }));
        st.rows.forEach((r, idx) => {
          if (r.file && !r.skip) fd.append(`file_${idx}`, r.file, r.fileName || `skin_${idx}.png`);
        });
        fd.append("meta", JSON.stringify({
          mc: mcIn.value.trim() || "1.21.4",
          packName: packIn.value.trim(),
          description: descIn.value.trim(),
          mode: st.mode,
          entries,
          destDir: st.local ? destIn.value.trim() : "",
        }));
        const res = await fetch("/api/toolbox/skin-pack/build", {
          method: "POST",
          headers: { "X-Token": TOKEN() },
          body: fd,
        });
        const j = await res.json().catch(() => null);
        if (res.status === 401) { if (typeof showLogin === "function") showLogin(); throw new Error((j && j.error) || "需要登录"); }
        if (!res.ok || !j || !j.ok) {
          throw Object.assign(new Error((j && j.error) || `生成失败（HTTP ${res.status}）`), { status: res.status, hint: j && j.hint });
        }
        if (stale(gen)) return;
        const data = j.data;
        st.result = data;
        resultEl.innerHTML = `
          <div class="tb-result" id="sp-done-card">
            <div class="tb-res-title">${icon("check")} 皮肤包已生成：<strong>${esc(data.fileName)}</strong></div>
            <div class="tb-res-facts">
              <span>大小 ${esc(fmtBytes(data.size))}</span>
              <span>写入贴图 ${data.textureCount} 张</span>
              <span>pack_format ${data.packFormat}</span>
            </div>
            <div class="tb-sha-row">
              <span>resource-pack-sha1：</span>
              <code class="mono" id="sp-sha1">${esc(data.sha1)}</code>
              <button type="button" class="btn sm ghost" id="sp-copy-sha1" aria-label="复制皮肤包 SHA-1">${icon("copy")} 复制 SHA-1</button>
            </div>
            ${data.savedPath ? `<div class="hint">已同步保存到本机：<code class="mono">${esc(data.savedPath)}</code></div>` : ""}
            <div class="hint">${esc(data.notice || "")}</div>
            <div class="row" style="margin-top:12px">
              <a class="btn primary" id="sp-dl" href="${esc(dlUrlWithToken("/api/toolbox/skin-pack/download", data.downloadId))}" download="${esc(data.fileName)}">${icon("download")} 下载皮肤资源包 (.zip)</a>
            </div>
          </div>`;
        $("#sp-copy-sha1", resultEl).onclick = function () { copyFlash(this, data.sha1, "已复制 resource-pack-sha1"); };
        toast("离线皮肤包已生成");
      } catch (e) {
        if (!stale(gen)) {
          st.buildErr = e;
          buildErrEl.innerHTML = errBox("无法生成皮肤资源包", e, { label: "皮肤包生成" });
          toastErr("无法生成皮肤资源包", e);
        }
      } finally {
        if (!stale(gen)) {
          st.building = false;
          refreshBuildButton();
        }
      }
    };

    window.addEventListener("hashchange", function cleanThumbs() {
      if (stale(gen)) {
        revokeThumbs();
        window.removeEventListener("hashchange", cleanThumbs);
      }
    });

    renderRows();
    runPreview();

    // 并行读本机状态和服务器列表（不影响首屏输入）
    api("/api/app").catch(() => ({})).then(a => {
      if (stale(gen)) return;
      st.local = !a || a.local !== false;
      hostRow.hidden = !st.local;
    });
    api("/api/instances").catch(() => []).then(list => {
      if (stale(gen) || !Array.isArray(list)) return;
      const gameInsts = list.filter(i => i.kind !== "proxy");
      if (!gameInsts.length) return;
      for (const i of gameInsts) if (i.mc) instMc.set(i.name, i.mc);
      instPick.innerHTML = gameInsts.map(i => `<option value="${esc(i.name)}">${esc(i.name)}（${esc(coreName(i.core))} ${esc(i.mc)}）</option>`).join("");
      instBar.hidden = false;
    });
  }

  /* ========================================================================
     工具二：导出 Modrinth 整合包（#/toolbox/mrpack-export）
     ======================================================================== */
  function renderMrpackExport(gen) {
    const st = {
      source: "draft", // draft | instance
      inst: "",
      name: "我的整合包",
      versionId: "1.0.0",
      summary: "",
      core: "fabric",
      mc: "1.20.1",
      loaderVersion: "",
      includeConfig: false,
      withDeps: true,
      destDir: "",
      local: true,
      modInsts: [],
      loading: true,
      preview: null,
      previewErr: null,
      building: false,
      buildErr: null,
      result: null,
    };

    Main().innerHTML =
      pageHead(
        "导出 Modrinth 整合包",
        "生成标准 .mrpack 发给朋友，他们用启动器（PCL2 / HMCL / Modrinth App / Prism）一键装好同样的模组。和服务器菜单里的「导出交接包」不一样：交接包是把整台服务器交给另一台电脑接着开，这里做的是给朋友的游戏客户端用的。",
        `<a class="btn" href="#/toolbox">${icon("arrowLeft")} 回到工具箱</a>`,
        `<a href="#/toolbox">工具箱</a><span class="sep">/</span><span>导出 Modrinth 整合包</span>`,
        ".mrpack 分享包"
      ) +
      `<div class="tb-wrap">
        <div class="group tb-sec">
          <div class="tb-sec-head">
            <h2>1. 选择导出方式与整合包信息</h2>
            <div class="seg" id="mp-src" role="group" aria-label="整合包模组来源">
              <button type="button" class="on" data-src="draft" aria-pressed="true">自己挑模组</button>
              <button type="button" data-src="instance" aria-pressed="false">从已有服务器导出</button>
            </div>
          </div>
          <div id="mp-draft-msg" role="status"></div>

          <div id="mp-inst-row" class="form-grid tb-grid" hidden>
            <label class="field">
              <span>选择已有模组服务器</span>
              <select id="mp-inst" aria-label="选择要导出的模组服务器"></select>
            </label>
            <div class="field tb-chk-field">
              <label class="switch">
                <input type="checkbox" id="mp-inc-cfg" aria-label="一并打包 config 文件夹">
                <span class="sw"></span>
                <span><span class="sw-label">一并打包 config/ 配置</span><div class="sw-desc">放进 overrides/config/。看起来带密码、令牌、密钥的配置文件会尽量挑出来不打进去，但难免有认不出的，发之前请看一眼预览里列出的配置文件。</div></span>
              </label>
            </div>
          </div>
          <div id="mp-no-inst" class="hint" hidden>当前还没有模组服（Fabric / Forge / NeoForge / Mohist / Banner），可以先用「自己挑模组」配整合包。</div>

          <div class="form-grid tb-grid">
            <label class="field">
              <span>整合包名称</span>
              <input type="text" id="mp-name" value="${esc(st.name)}" placeholder="朋友联机整合包" aria-label="整合包名称" maxlength="100">
            </label>
            <label class="field">
              <span>版本号</span>
              <input type="text" id="mp-ver" value="${esc(st.versionId)}" placeholder="1.0.0" aria-label="整合包版本号" maxlength="50">
            </label>
            <label class="field tb-col-span">
              <span>简介（可选）</span>
              <input type="text" id="mp-summary" value="" placeholder="给朋友看的一句话说明" aria-label="整合包简介" maxlength="300">
            </label>
          </div>

          <div id="mp-draft-fields" class="form-grid tb-grid" style="margin-top:12px">
            <label class="field">
              <span>模组加载器</span>
              <select id="mp-core" aria-label="整合包加载器">
                <option value="fabric">Fabric</option>
                <option value="neoforge">NeoForge</option>
                <option value="forge">Forge</option>
              </select>
            </label>
            <label class="field">
              <span>Minecraft 版本</span>
              <input type="text" id="mp-mc" value="${esc(st.mc)}" list="mp-mc-list" placeholder="如 1.20.1" aria-label="整合包 Minecraft 版本" maxlength="40">
              <datalist id="mp-mc-list">${MC_PRESETS.map(v => `<option value="${esc(v)}">`).join("")}</datalist>
            </label>
            <label class="field">
              <span>加载器版本（可选）</span>
              <input type="text" id="mp-loader-ver" value="" placeholder="留空自动选官方推荐版本" aria-label="可选加载器版本" maxlength="80">
            </label>
            <div class="field tb-chk-field">
              <label class="switch">
                <input type="checkbox" id="mp-withdeps" checked aria-label="自动带上必装前置模组">
                <span class="sw"></span>
                <span><span class="sw-label">自动带出必装前置</span><div class="sw-desc">导出时一并收进列表里模组所需的必装前置，省得朋友装完启动报错。</div></span>
              </label>
            </div>
          </div>
        </div>

        <div class="group tb-sec" id="mp-cart-sec">
          <div class="tb-sec-head"><h2>2. 挑选要放进整合包的模组</h2><span class="hint">清单会自动保存</span></div>
          <div id="mp-modcart-host"></div>
        </div>

        <div class="group tb-sec">
          <div class="tb-sec-head"><h2>3. 导出清单预览与生成</h2></div>
          <div id="mp-host-row" class="tb-host-row" hidden>
            <label class="field tb-dir-field">
              <span>同步保存到本机文件夹（可选）</span>
              <div class="row">
                <input type="text" id="mp-destdir" value="" placeholder="留空只提供浏览器下载；选填本机目录可一步存盘" aria-label="同步保存整合包的本机文件夹路径" maxlength="400">
                <button type="button" class="btn sm" id="mp-pickdir" aria-label="浏览整合包保存文件夹">${icon("folder")} 浏览…</button>
              </div>
            </label>
          </div>
          <div class="row tb-act-row">
            <button type="button" class="btn" id="mp-preview-btn">${icon("search")} 预览导出清单</button>
            <button type="button" class="btn primary" id="mp-build-btn">${icon("package")} 生成 .mrpack 整合包</button>
          </div>
          <div id="mp-preview-box"></div>
          <div id="mp-build-err"></div>
          <div id="mp-result"></div>
        </div>
      </div>`;

    const srcBox = $("#mp-src"), instRow = $("#mp-inst-row"), noInstEl = $("#mp-no-inst"), instSel = $("#mp-inst");
    const incCfgEl = $("#mp-inc-cfg"), nameIn = $("#mp-name"), verIn = $("#mp-ver"), sumIn = $("#mp-summary");
    const draftFields = $("#mp-draft-fields"), coreSel = $("#mp-core"), mcIn = $("#mp-mc"), loaderIn = $("#mp-loader-ver"), depsEl = $("#mp-withdeps");
    const cartSec = $("#mp-cart-sec"), cartHost = $("#mp-modcart-host");
    const hostRow = $("#mp-host-row"), destIn = $("#mp-destdir"), pickDirBtn = $("#mp-pickdir");
    const prevBtn = $("#mp-preview-btn"), buildBtn = $("#mp-build-btn");
    const prevBox = $("#mp-preview-box"), buildErrEl = $("#mp-build-err"), resultEl = $("#mp-result");
    const draftMsg = $("#mp-draft-msg");

    let saveTimer = null;

    function currentReq() {
      return {
        source: st.source,
        inst: st.source === "instance" ? st.inst : "",
        name: nameIn.value.trim(),
        versionId: verIn.value.trim(),
        summary: sumIn.value.trim(),
        core: st.source === "draft" ? coreSel.value : "",
        mc: st.source === "draft" ? mcIn.value.trim() : "",
        loaderVersion: st.source === "draft" ? loaderIn.value.trim() : "",
        includeConfig: st.source === "instance" ? !!incCfgEl.checked : false,
        withDeps: !!depsEl.checked,
        destDir: st.local ? destIn.value.trim() : "",
      };
    }

    // 草稿读回来以前不存：那时表单里没动过的格子还是默认值，存上去会把以前存的草稿盖掉。
    // 先记下「有要存的」，读回来、把没动过的格子补上以后再存合起来的那一份；读失败就这一次不自动存（Codex 复审 beta.3 第二、三轮）。
    // 读的时候来源切换是锁着的；从「自己挑模组」切走时，排着的那次保存当场存掉：名称、版本这几格两种来源共用，
    // 切到「从已有服务器导出」以后会被改成服务器的名字，等计时到了再读就把它存进草稿了
    let draftReady = false, savePending = false;
    function scheduleSaveDraft() {
      if (st.source !== "draft") return;
      if (!draftReady) { savePending = true; return; }
      if (saveTimer) clearTimeout(saveTimer);
      saveTimer = setTimeout(saveDraftNow, 260);
    }
    function saveDraftNow() {
      if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; }
      api("/api/toolbox/mrpack/draft", {
        method: "PUT",
        body: {
          name: nameIn.value.trim(),
          versionId: verIn.value.trim(),
          summary: sumIn.value.trim(),
          core: coreSel.value,
          mc: mcIn.value.trim(),
          loaderVersion: loaderIn.value.trim(),
          includeConfig: !!incCfgEl.checked,
        },
      }).catch(() => {});
    }

    function mountDraftCart() {
      if (!window.MDModCart || st.source !== "draft") return;
      // 工具箱自己的那份清单（cart=toolbox）：和新建向导的草稿清单分开，建服时不会被装进去
      MDModCart.toolboxPanel(cartHost, { core: coreSel.value || "fabric", mc: mcIn.value.trim() || "1.20.1" }, gen);
    }

    function applySourceUI() {
      const isInst = st.source === "instance";
      draftFields.hidden = isInst;
      cartSec.hidden = isInst;
      // 读不出草稿的说明和「重新读取」只在「自己挑模组」时出现：名称、版本、简介两种来源共用，停在「从已有服务器导出」时
      // 重新读取要么不填它们（切回来一改，服务器名就被存进草稿，Codex 复审 beta.3 之后第一轮），要么把服务器名换掉，都不对
      draftMsg.hidden = isInst;
      if (isInst) {
        instRow.hidden = st.modInsts.length === 0;
        noInstEl.hidden = st.modInsts.length > 0;
        prevBtn.disabled = st.modInsts.length === 0;
        buildBtn.disabled = st.modInsts.length === 0;
      } else {
        instRow.hidden = true;
        noInstEl.hidden = true;
        prevBtn.disabled = false;
        buildBtn.disabled = false;
        mountDraftCart();
      }
    }

    function renderPreview(p) {
      const depsStr = Object.entries(p.dependencies || {}).map(([k, v]) => `${k} ${v}`).join(" · ");
      const warnGroups = [
        ...(p.warnings || []),
        ...(p.missingDeps || []).map(x => `可能缺少前置：${x}`),
        ...(p.conflicts || []).map(x => `存在潜在冲突：${x}`),
        ...(p.unresolved || []).map(x => `待处理本地文件：${x}`),
      ];
      const warnHtml = warnGroups.length ? `
        <div class="warn-card">
          <div class="wc-title">${icon("warn")} 打包提醒（${warnGroups.length}）</div>
          <ul class="wc-list">${warnGroups.map(w => `<li>${esc(w)}</li>`).join("")}</ul>
        </div>` : "";
      const ENV_CN = { required: "要装", optional: "可选", unsupported: "不装" };
      const envTag = it => {
        if (it.serverEnv === "unsupported") return `<span class="badge core">仅客户端（已标 server: unsupported）</span>`;
        if (it.clientEnv === "unsupported") return `<span class="badge">仅服务端（客户端不装）</span>`;
        if (it.serverEnv === "required" && it.clientEnv === "required") return `<span class="badge">客户端+服务端</span>`;
        const c = ENV_CN[it.clientEnv] || "要装", sv = ENV_CN[it.serverEnv] || "要装";
        return `<span class="badge" title="Modrinth 上标的：客户端${c}，服务端${sv}。启动器装整合包时，可选的会问朋友要不要装">客户端${esc(c)} · 服务端${esc(sv)}</span>`;
      };
      const filesHtml = (p.files || []).length ? `
        <h3 class="tb-subh">写入 modrinth.index.json（启动器自动从官方 CDN 下载 · ${p.files.length} 项）</h3>
        <div class="tb-table-wrap">
          <table class="tbl">
            <thead><tr><th>名称</th><th>包内路径</th><th>环境</th><th>大小</th></tr></thead>
            <tbody>
              ${p.files.map(f => `
                <tr>
                  <td>${esc(f.title || f.fileName)}${f.fromDep ? ` <span class="badge">前置依赖</span>` : ""}</td>
                  <td class="mono">${esc(f.packPath)}</td>
                  <td>${envTag(f)}</td>
                  <td>${esc(fmtBytes(f.size || 0))}</td>
                </tr>`).join("")}
            </tbody>
          </table>
        </div>` : "";
      const ovHtml = (p.overrides || []).length ? `
        <h3 class="tb-subh">直接打包进 overrides/（${p.overrides.length} 项）</h3>
        <div class="tb-table-wrap">
          <table class="tbl">
            <thead><tr><th>包内路径</th><th>原因 / 说明</th><th>大小</th></tr></thead>
            <tbody>
              ${p.overrides.map(o => `
                <tr>
                  <td class="mono">${esc(o.packPath)}</td>
                  <td>${esc(o.note || "本地文件")}</td>
                  <td>${esc(fmtBytes(o.size || 0))}</td>
                </tr>`).join("")}
            </tbody>
          </table>
        </div>` : "";

      prevBox.innerHTML = `
        <div class="tb-preview-sum">
          <span class="badge core">${esc(depsStr || (p.core + " " + p.mc))}</span>
          <span class="badge">在线下载 ${p.downloadCount} 项</span>
          <span class="badge">overrides ${p.overrideCount} 项</span>
          ${p.clientOnlyCount ? `<span class="badge">仅客户端 ${p.clientOnlyCount} 项</span>` : ""}
          ${p.configCount ? `<span class="badge">配置 ${p.configCount} 项</span>` : ""}
        </div>
        ${warnHtml}
        ${filesHtml}
        ${ovHtml}
        ${!p.downloadCount && !p.overrideCount ? `<div class="hint">清单里还没有任何模组，请先挑选模组或切换到已有服务器后再导出。</div>` : ""}`;
    }

    async function doPreview() {
      if (st.building) return;
      st.previewErr = null;
      prevBox.innerHTML = `<div class="hint">正在整理导出清单与 Modrinth 校验信息…</div>`;
      try {
        const res = await api("/api/toolbox/mrpack/preview", { method: "POST", body: currentReq() });
        if (stale(gen)) return;
        st.preview = res;
        renderPreview(res);
      } catch (e) {
        if (stale(gen)) return;
        st.previewErr = e;
        prevBox.innerHTML = errBox("无法预览整合包清单", e, { label: "整合包预览" });
      }
    }

    async function doBuild() {
      st.building = true;
      buildBtn.disabled = true;
      buildErrEl.innerHTML = "";
      try {
        const data = await api("/api/toolbox/mrpack/build", { method: "POST", body: currentReq() });
        if (stale(gen)) return;
        st.result = data;
        const depsStr = Object.entries(data.dependencies || {}).map(([k, v]) => `${k} ${v}`).join(" · ");
        resultEl.innerHTML = `
          <div class="tb-result" id="mp-done-card">
            <div class="tb-res-title">${icon("check")} Modrinth 整合包已生成：<strong>${esc(data.fileName)}</strong></div>
            <div class="tb-res-facts">
              <span>${esc(depsStr)}</span>
              <span>大小 ${esc(fmtBytes(data.size))}</span>
              <span>在线下载 ${data.downloadCount} 项</span>
              <span>overrides ${data.overrideCount} 项</span>
            </div>
            <div class="tb-sha-row">
              <span>SHA-1：</span>
              <code class="mono" id="mp-sha1">${esc(data.sha1)}</code>
              <button type="button" class="btn sm ghost" id="mp-copy-sha1" aria-label="复制整合包 SHA-1">${icon("copy")} 复制 SHA-1</button>
            </div>
            ${data.savedPath ? `<div class="hint">已同步保存到本机：<code class="mono">${esc(data.savedPath)}</code></div>` : ""}
            ${(data.warnings || []).length ? `<div class="hint">提示：${esc(data.warnings.join("；"))}</div>` : ""}
            <div class="hint">朋友拿到以后：在启动器里选「导入整合包」（Modrinth App、Prism、HMCL、PCL2 都有），选这个 .mrpack，启动器会建好对应版本和加载器、下载里面的模组。</div>
            <div class="row" style="margin-top:12px">
              <a class="btn primary" id="mp-dl" href="${esc(dlUrlWithToken("/api/toolbox/mrpack/download", data.downloadId))}" download="${esc(data.fileName)}">${icon("download")} 下载 .mrpack 整合包</a>
            </div>
          </div>`;
        $("#mp-copy-sha1", resultEl).onclick = function () { copyFlash(this, data.sha1, "已复制 .mrpack SHA-1"); };
        toast(".mrpack 整合包已生成");
      } catch (e) {
        if (!stale(gen)) {
          st.buildErr = e;
          buildErrEl.innerHTML = errBox("无法生成 .mrpack 整合包", e, { label: "整合包生成" });
          toastErr("无法生成 .mrpack 整合包", e);
        }
      } finally {
        if (!stale(gen)) {
          st.building = false;
          buildBtn.disabled = st.source === "instance" && st.modInsts.length === 0;
        }
      }
    }

    srcBox.querySelectorAll("button[data-src]").forEach(b => {
      b.onclick = () => {
        const s = b.dataset.src;
        if (s === st.source) return;
        if (st.source === "draft" && saveTimer) saveDraftNow(); // 切走之前存掉排着的（见 scheduleSaveDraft）
        const kr = typeof keyRects === "function" ? keyRects() : null;
        st.source = s;
        srcBox.querySelectorAll("button").forEach(x => {
          const on = x.dataset.src === s;
          x.classList.toggle("on", on);
          x.setAttribute("aria-pressed", on ? "true" : "false");
        });
        if (typeof slideKeys === "function" && kr) slideKeys(kr, ["seg"]);
        if (s === "instance" && st.modInsts.length) {
          const picked = st.modInsts.find(i => i.name === instSel.value) || st.modInsts[0];
          if (picked) {
            st.inst = picked.name;
            if (nameIn.value === "我的整合包" || !nameIn.value.trim()) nameIn.value = picked.name;
          }
        }
        prevBox.innerHTML = "";
        buildErrEl.innerHTML = "";
        applySourceUI();
      };
    });

    instSel.onchange = () => {
      st.inst = instSel.value;
      const picked = st.modInsts.find(i => i.name === st.inst);
      if (picked && (!nameIn.dataset.dirty || !nameIn.value.trim())) {
        nameIn.value = picked.name;
      }
      prevBox.innerHTML = "";
    };

    // 初始化（读草稿、服务器列表）还没回来时人就可能开始填：回来以后只补人没动过的字段（Codex 复审）
    const touched = new Set();
    [nameIn, verIn, sumIn, loaderIn].forEach(el => {
      el.oninput = () => {
        touched.add(el);
        if (el === nameIn) nameIn.dataset.dirty = "1";
        scheduleSaveDraft();
      };
    });
    coreSel.onchange = () => { touched.add(coreSel); st.core = coreSel.value; scheduleSaveDraft(); mountDraftCart(); };
    mcIn.oninput = () => { touched.add(mcIn); st.mc = mcIn.value; scheduleSaveDraft(); };
    mcIn.onchange = () => { touched.add(mcIn); st.mc = mcIn.value; scheduleSaveDraft(); mountDraftCart(); };
    incCfgEl.onchange = () => scheduleSaveDraft();
    destIn.oninput = () => { st.destDir = destIn.value; };
    pickDirBtn.onclick = async () => {
      try {
        const r = await api("/api/pickdir", { method: "POST" });
        if (!stale(gen) && r && r.path) { destIn.value = r.path; st.destDir = r.path; }
      } catch (e) { toastErr("无法打开文件夹选择窗口", e); }
    };
    prevBtn.onclick = doPreview;
    buildBtn.onclick = doBuild;

    // 把读回来的草稿填进表单。第一次读回来时只补人没动过的格子（force 为假）；「重新读取」时整份换成存好的（force 为真）：
    // 读失败期间改的不保留，提示条上写明了。两种时候来源切换都锁着、停在「自己挑模组」（见 applySourceUI 和下面的重新读取）
    function fillDraft(draft, force) {
      if (!draft) return;
      const fill = (el, v) => { if (force || !touched.has(el)) el.value = v; };
      if (draft.name) fill(nameIn, st.name = draft.name);
      if (draft.versionId) fill(verIn, st.versionId = draft.versionId);
      if (draft.summary != null) fill(sumIn, st.summary = draft.summary);
      if (draft.core && ["fabric", "forge", "neoforge"].includes(draft.core)) fill(coreSel, st.core = draft.core);
      if (draft.mc) fill(mcIn, st.mc = draft.mc);
      if (draft.loaderVersion != null) fill(loaderIn, st.loaderVersion = draft.loaderVersion);
    }

    // 读草稿失败（比如文件正被别的程序占着）：这一次不自动存，免得盖掉存好的那份（Codex 复审 beta.3 第三轮）。
    // 以前页面上什么都不说，人接着填、离开再回来，填的都没了也不知道（第五轮）。现在常驻一条说明，带「重新读取」：
    // 读出来了就换成存好的那份、恢复自动保存；还读不出来就换上新的原因，接着不存
    function paintDraftErr(e) {
      if (!e) { draftMsg.innerHTML = ""; return; }
      draftMsg.innerHTML = errBox("读不出上次存好的整合包信息", e, {
        hint: "这一次改的不会自动保存，免得把存好的那份盖掉。点「重新读取」：读出来以后，表单换成存好的那份，现在改的不保留。",
        actions: `<button type="button" class="btn sm" id="mp-draft-retry">${icon("refresh")} 重新读取</button>`,
        id: "mp-draft-err", label: "整合包草稿",
      });
      const retry = $("#mp-draft-retry");
      retry.onclick = async () => {
        retry.disabled = true;
        // 读的时候来源切换锁着（同第一次读）：读回来时一定还停在「自己挑模组」，整份填回去不会碰到服务器的名字
        const srcBtns = [...srcBox.querySelectorAll("button[data-src]")];
        srcBtns.forEach(b => { b.disabled = true; });
        let d;
        try { d = await api("/api/toolbox/mrpack/draft"); }
        catch (e2) {
          if (stale(gen)) return;
          srcBtns.forEach(b => { b.disabled = false; });
          paintDraftErr(e2);
          $("#mp-draft-retry").focus();
          return;
        }
        if (stale(gen)) return;
        srcBtns.forEach(b => { b.disabled = false; });
        fillDraft(d, true);
        savePending = false; // 读失败期间排着的那次保存作废：表单已经换成存好的了
        draftReady = true;
        paintDraftErr(null);
        mountDraftCart(); // 加载器、版本可能变了，清单按新的查
        toast("已读回存好的整合包信息，之后的修改会自动保存");
      };
    }

    // 读草稿设置 + 实例列表 + 本机权限。读完以前来源切换锁着（见 scheduleSaveDraft）
    let draftErr = null;
    srcBox.querySelectorAll("button[data-src]").forEach(b => { b.disabled = true; });
    Promise.all([
      api("/api/toolbox/mrpack/draft").catch(e => { draftErr = e || new Error("读不出来"); return null; }),
      api("/api/instances").catch(() => []),
      api("/api/app").catch(() => ({})),
    ]).then(([draft, insts, appInfo]) => {
      if (stale(gen)) return;
      st.loading = false;
      st.local = !appInfo || appInfo.local !== false;
      hostRow.hidden = !st.local;
      srcBox.querySelectorAll("button[data-src]").forEach(b => { b.disabled = false; });
      if (draftErr) paintDraftErr(draftErr);
      else {
        fillDraft(draft, false);
        draftReady = true;
        if (savePending) { savePending = false; scheduleSaveDraft(); }
      }
      if (Array.isArray(insts)) {
        st.modInsts = insts.filter(i => MOD_CORES.has(i.core));
        instSel.innerHTML = st.modInsts.map(i => `<option value="${esc(i.name)}">${esc(i.name)}（${esc(coreName(i.core))} ${esc(i.mc)}）</option>`).join("");
        if (st.modInsts.length) st.inst = st.modInsts[0].name;
      }
      applySourceUI();
    });
  }

  return { render, TOOLS };
})();
