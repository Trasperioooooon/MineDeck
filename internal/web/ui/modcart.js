/* MineDeck 自选模组 + 购物车：实例详情的「购物车」标签与新建向导的「选模组」步（可选）。
   流程：搜 Modrinth → 挑模组/插件放进购物车 → 检查依赖（缺的勾一下就一并进车）→ 一次装齐（后台任务，kind "mods"）。
   向导里是「草稿车」：不传 inst，建服完成后由 app.js 发起安装（includeDraft），装完跳模组安装任务页。
   在 app.js 之前加载；用到的 app.js 工具（api、esc、$、icon、ICON、toast、confirmModal、stale、skeletonRows）都在调用时才取，
   加载顺序不影响。app.js 里只留了钩子：详情页的标签和分派、TASK_KINDS 的 "mods"、向导的「选模组」步与提交链。 */
"use strict";
window.MDModCart = (() => {
  /* 向导「选模组」那一步上最近一次画出来的草稿车（项目 ID）；还没画过是 null。
     建服完成后带着草稿车一起装时，把它当成「人看到的那些」发给后端核对（见 app.js 的提交链）。 */
  let draftSeen = null;
  /* 有「购物车」标签的实例：同「资源」标签的条件——原版没有模组加载器，不需要 */
  const applies = info => !!info && info.core !== "vanilla";

  /* 新增的图标（16 视框、细线，同 app.js 的 ICON），第一次用时并进 ICON */
  const ICONS = { cart: '<path d="M1.8 2.2h1.9l1.5 7.2h7.7l1.3-5H4.2"/><circle cx="6.1" cy="12.3" r="1.15"/><circle cx="11.1" cy="12.3" r="1.15"/>' };
  let iconsReady = false;
  const ensureIcons = () => { if (!iconsReady) { Object.assign(ICON, ICONS); iconsReady = true; } };

  const KIND_CN = { mod: "模组", plugin: "插件" };
  const kindOf = k => KIND_CN[k] || (k ? k : "");
  /* 下载量：12.5M / 348k（同资源页的写法） */
  const fmtDown = n => n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : Math.round((n || 0) / 1e3) + "k";
  /* 购物车里一个条目 / 搜索结果一张卡的行首图标 */
  const tile = icon_url => icon_url
    ? `<img class="res-icon" src="${esc(icon_url)}" width="34" height="34" alt="" onerror="this.remove()">`
    : `<span class="res-noicon">${icon("package")}</span>`;

  /* ---------- 一块购物车面板 ----------
     ctx.inst 有值 = 某台实例的购物车（详情页，给安装按钮）；没有 = 向导的草稿车。
     ctx.status 为真（服务器在跑）时安装前提示要重启才生效。 */
  function buildPanel(root, ctx, gen) {
    ensureIcons();
    const inst = ctx.inst || "";
    root.innerHTML = `
      <div class="row" style="margin-bottom:12px;flex-wrap:wrap">
        <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="mc-q" placeholder="搜索模组 / 插件（英文名效果更好）" aria-label="搜索模组或插件" style="width:340px"></div>
        <button class="btn" id="mc-go">${icon("search")} 搜索</button>
        <span class="hint">已按当前核心与版本过滤兼容资源 · 数据来自 Modrinth（免费开放）</span>
      </div>
      <div id="mc-hits"><div class="sub">输入关键词搜索，把想要的模组或插件加进购物车</div></div>
      <h3 class="mc-gh">购物车 <span class="hint" id="mc-count" aria-live="polite"></span></h3>
      <div id="mc-list">${skeletonRows(2)}</div>
      ${ctx.inst ? `
      <div class="row mc-actions" id="mc-actions" hidden>
        <button class="btn primary" id="mc-install">${icon("download")} 安装全部</button>
        <button class="btn" id="mc-clear" aria-label="清空购物车">${icon("trash")} 清空</button>
      </div>` : `
      <div class="row mc-actions" id="mc-actions" hidden>
        <button class="btn" id="mc-clear" aria-label="清空购物车">${icon("trash")} 清空</button>
      </div>`}
      <h3 class="mc-gh">依赖检查</h3>
      <div class="sub mc-dep-sub">安装前可先检查一遍：购物车里模组的必装依赖会列出来，勾选后一并加入购物车，免得装完缺依赖开不了服。</div>
      <div class="row" style="margin-bottom:10px"><button class="btn" id="mc-dep-go">${icon("package")} 检查依赖</button><span class="hint" id="mc-dep-state" aria-live="polite"></span></div>
      <div id="mc-dep-box"></div>`;
    const qEl = $("#mc-q", root), hitsEl = $("#mc-hits", root), listEl = $("#mc-list", root),
      countEl = $("#mc-count", root), actionsEl = $("#mc-actions", root),
      depBtn = $("#mc-dep-go", root), depState = $("#mc-dep-state", root), depBox = $("#mc-dep-box", root);
    let cart = [],              // 购物车条目（后端的形状）
        hits = [],              // 最近一次搜索的结果
        dep = null,             // 最近一次依赖检查的结果
        searched = false,       // 搜过至少一次（没搜过时保留开头那句提示）
        searching = false,      // 正在搜（保留骨架屏，别被车的刷新盖掉）
        installing = false,     // 已经点了安装（等确认框或接口时不能再点）
        loadSeq = 0, searchReq = 0, depSeq = 0;
    const ids = () => new Set(cart.map(it => it.projectId));

    /* ---- 购物车清单 ---- */
    const paintList = () => {
      listEl.innerHTML = cart.length ? cart.map(it => `
        <div class="java-row mc-item">
          ${tile(it.icon)}
          <div style="flex:1;min-width:0"><b>${esc(it.title)}</b>
            <div class="jp" style="white-space:normal">${[kindOf(it.kind), it.license].filter(Boolean).map(esc).join(" · ")}</div></div>
          <button class="btn sm ghost mc-del" data-pid="${esc(it.projectId)}" data-t="${esc(it.title)}" aria-label="移除 ${esc(it.title)}">${icon("x")} 移除</button>
        </div>`).join("") : `<div class="sub" id="mc-empty">购物车是空的：搜索到的结果里点「加入购物车」，凑齐后一起装。</div>`;
      countEl.textContent = cart.length ? `共 ${cart.length} 项` : "";
      if (!inst) draftSeen = cart.map(it => it.projectId); // 草稿车：记下画在页面上的是哪些
      actionsEl.hidden = !cart.length;
      const insBtn = $("#mc-install", root);
      if (insBtn) { insBtn.innerHTML = icon("download") + ` 安装全部（${cart.length}）`; insBtn.disabled = installing; }
      listEl.querySelectorAll(".mc-del").forEach(b => b.onclick = async () => {
        b.disabled = true;
        try { await api("/api/modcart/remove", { method: "POST", body: { inst: inst || undefined, projectId: b.dataset.pid } }); }
        catch (e) { toast(e.message, true); if (!stale(gen) && b.isConnected) b.disabled = false; return; }
        if (stale(gen)) return;
        await reload();
      });
      paintHits(); // 车里有了/少了，搜索结果按钮的状态跟着变
      if (dep) paintDeps(); // 依赖检查的结果也跟着变：已经进车的不再算缺
    };

    /* ---- 搜索结果（最近一次搜索 + 当前车状态）---- */
    const paintHits = () => {
      if (!searched || searching) return;
      const have = ids();
      hitsEl.innerHTML = hits.length ? hits.map(h => {
        const inCart = have.has(h.project_id);
        return `
        <div class="java-row mc-hit">
          ${tile(h.icon_url)}
          <div style="flex:1;min-width:0"><b>${esc(h.title)}</b>
            <div class="jp" style="white-space:normal;max-height:32px;overflow:hidden">${esc(h.description)}</div></div>
          <span class="jv mc-license">${esc(h.license || "")}</span>
          <span class="jv mc-down">${fmtDown(h.downloads)}${icon("download")}</span>
          <button class="btn sm primary mc-add" data-pid="${esc(h.project_id)}" data-t="${esc(h.title)}" aria-label="${inCart ? "在车中" : "加入购物车"} ${esc(h.title)}"${inCart ? " disabled" : ""}>${icon(inCart ? "check" : "cart")} ${inCart ? "在车中" : "加入购物车"}</button>
        </div>`;
      }).join("") : `<div class="sub">没有找到兼容的资源，换个关键词试试</div>`;
      hitsEl.querySelectorAll(".mc-add").forEach(b => b.onclick = () => add([b.dataset.pid], [b]));
    };
    /* 把项目加进购物车；btns 是正在转圈的按钮（搜索结果里逐个换字） */
    const add = async (projectIds, btns) => {
      ++loadSeq; // 还没回来的读车请求作废：这次加入的结果更新
      btns.forEach(b => { b.disabled = true; const t = b.dataset.t; b.setAttribute("aria-label", "加入购物车中 " + t); b.innerHTML = icon("spinner") + " 加入中…"; });
      try {
        const d = await api("/api/modcart", { method: "POST", body: { inst: inst || undefined, projectIds } });
        if (stale(gen) || !root.isConnected) return;
        cart = d.items || [];
        paintList();
        /* 查不到的、只在客户端用的模组后端不放进车，在 skipped 里说明原因 */
        const skipped = d.skipped || [];
        const one = btns[0] ? btns[0].dataset.t : (cart.find(it => it.projectId === projectIds[0]) || {}).title;
        if (skipped.length) toast(skipped.map(x => `${x.title || x.projectId}：${x.reason}`).join("；"), true);
        else toast(projectIds.length > 1 ? `已加入购物车：${projectIds.length} 项依赖` : `已加入购物车：${one || "1 项"}`);
      } catch (e) {
        toast(e.message, true);
        if (!stale(gen) && root.isConnected) paintList(); // 加购失败也逐个按钮复位
      }
    };

    /* ---- 依赖检查 ---- */
    const paintDeps = () => {
      const have = ids();
      const missing = (dep.missing || []).filter(m => !have.has((m.project || {}).projectId));
      const missRows = missing.map(m => `
        <div class="java-row mc-miss">
          <label class="mc-check"><input type="checkbox" checked data-pid="${esc(m.project.projectId)}" aria-label="补齐 ${esc(m.project.title)}"></label>
          <div style="flex:1;min-width:0"><b>${esc(m.project.title)}</b>
            <div class="jp" style="white-space:normal">${esc(m.project.fileName)}${m.project.size ? " · " + fmtBytes(m.project.size) : ""}${m.dependencyOf ? " · " + esc(m.dependencyOf) + " 的依赖" : ""}</div></div>
          <span class="jv">必装</span>
        </div>`).join("");
      const optRows = (dep.optional || []).map(m => `
        <div class="java-row mc-opt">
          ${tile("")}
          <div style="flex:1;min-width:0"><b>${esc(m.project.title)}</b>
            <div class="jp" style="white-space:normal">${esc(m.project.fileName)}${m.project.size ? " · " + fmtBytes(m.project.size) : ""}${m.dependencyOf ? " · " + esc(m.dependencyOf) + " 的依赖" : ""}</div></div>
          <button class="btn sm ghost mc-opt-add" data-pid="${esc(m.project.projectId)}" data-t="${esc(m.project.title)}" aria-label="加入购物车 ${esc(m.project.title)}"${have.has(m.project.projectId) ? " disabled" : ""}>${have.has(m.project.projectId) ? icon("check") + " 在车中" : icon("cart") + " 加入购物车"}</button>
        </div>`).join("");
      /* files 是安装时要下载的全部文件（fromDep 的是自动补的必装前置）；missing 是购物车之外还需要的必装前置：
         安装时默认会一起装上，这里也可以勾选后正式放进购物车 */
      const files = dep.files || [], auto = files.filter(f => f.fromDep).length;
      depBox.innerHTML = `
        ${files.length ? `<div class="hint" style="margin-bottom:8px">安装时会下载 ${files.length} 个文件${auto ? `（其中 ${auto} 个是自动补上的必装依赖）` : ""}：${files.map(f => esc(f.fileName)).join("、")}</div>` : ""}
        ${missing.length ? `
        <div class="mc-dep-sec"><b class="mc-dep-t">缺的依赖（必装）</b>
          <div class="hint">安装时会自动一起装上；也可以勾选后加入购物车，在清单里一起管理。</div>
          <div class="row" style="margin:6px 0 10px"><button class="btn primary sm" id="mc-dep-add" aria-label="把勾选的加入购物车">${icon("cart")} 把勾选的加入购物车</button></div>
          ${missRows}
        </div>` : `<div class="hint" id="mc-dep-ok">没有缺依赖：购物车里的模组可以直接安装。</div>`}
        ${(dep.optional || []).length ? `<div class="mc-dep-sec"><b class="mc-dep-t">可选依赖（按需加入）</b>${optRows}</div>` : ""}
        ${(dep.conflicts || []).length ? `<div class="warn-card" style="margin:8px 0">
          <div class="wc-title">${icon("warn")} 有不兼容的组合</div>
          <ul class="wc-list">${dep.conflicts.map(c => `<li>${esc(c)}</li>`).join("")}</ul></div>` : ""}
        ${(dep.localHits || []).length ? `<div class="hint" style="margin:8px 0">服务器目录里已有的文件，安装时会跳过：${dep.localHits.map(esc).join("、")}</div>` : ""}
        ${(dep.unresolved || []).length ? `<div class="warn-card" style="margin:8px 0">
          <div class="wc-title">${icon("warn")} 查不到的依赖</div>
          <ul class="wc-list">${dep.unresolved.map(x => `<li>${esc(x.title)}：${esc(x.reason)}</li>`).join("")}</ul></div>` : ""}`;
      const addBtn = $("#mc-dep-add", root);
      if (addBtn) addBtn.onclick = async () => {
        const picked = [...depBox.querySelectorAll(".mc-miss input:checked")].map(i => i.dataset.pid);
        if (!picked.length) { toast("先勾上要补齐的依赖", true); return; }
        addBtn.disabled = true; // add 之后会重画整块，按钮跟着换新的
        await add(picked, []);
        if (stale(gen) || !root.isConnected) return;
        checkDeps(); // 补进车后重新核对一遍，剩余的缺失跟着更新
      };
      depBox.querySelectorAll(".mc-opt-add").forEach(b => b.onclick = () => add([b.dataset.pid], [b]));
    };
    const checkDeps = async () => {
      const seq = ++depSeq;
      const outdated = () => seq !== depSeq || stale(gen) || !root.isConnected;
      depBtn.disabled = true;
      depState.textContent = "检查中…";
      try {
        const d = await api(`/api/modcart/deps?${inst ? "inst=" + encodeURIComponent(inst) : "core=" + encodeURIComponent(ctx.core || "") + "&mc=" + encodeURIComponent(ctx.mc || "")}`);
        if (outdated()) return;
        dep = d;
        depState.textContent = "";
        paintDeps();
      } catch (e) {
        if (outdated()) return;
        depState.textContent = "";
        depBox.innerHTML = `<div class="err-box">${esc(e.message)}</div>`;
      } finally { if (!outdated()) depBtn.disabled = false; }
    };

    /* ---- 读一遍购物车 ---- */
    const reload = async () => {
      const seq = ++loadSeq;
      const outdated = () => seq !== loadSeq || stale(gen) || !root.isConnected;
      try {
        const d = await api(inst ? `/api/modcart?inst=${encodeURIComponent(inst)}` : "/api/modcart");
        if (outdated()) return;
        cart = d.items || [];
        paintList();
      } catch (e) { if (!outdated()) listEl.innerHTML = `<div class="err-box">${esc(e.message)}</div>`; }
    };

    /* ---- 搜索 ---- */
    const search = async () => {
      const req = ++searchReq;
      const outdated = () => stale(gen) || req !== searchReq || !root.isConnected;
      searching = true;
      hitsEl.innerHTML = skeletonRows(3);
      try {
        const query = Object.entries({ q: qEl.value.trim(), inst, core: ctx.core || "", mc: ctx.mc || "" })
          .filter(([, v]) => v !== "").map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join("&");
        const r = await api("/api/modcart/search?" + query);
        if (outdated()) return;
        hits = r.hits || [];
        searching = false; searched = true;
        paintHits();
      } catch (e) {
        if (outdated()) return;
        searching = false;
        hitsEl.innerHTML = `<div class="err-box">${esc(e.message)}</div>`;
      }
    };
    $("#mc-go", root).onclick = search;
    qEl.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); search(); } };

    /* ---- 清空 / 安装（详情页）---- */
    const clearBtn = $("#mc-clear", root);
    clearBtn.onclick = async () => {
      if (!cart.length || clearBtn.disabled) return;
      clearBtn.disabled = true;
      try { await api("/api/modcart/clear", { method: "POST", body: { inst: inst || undefined } }); }
      catch (e) { toast(e.message, true); if (!stale(gen) && clearBtn.isConnected) clearBtn.disabled = false; return; }
      if (stale(gen) || !root.isConnected) return;
      clearBtn.disabled = false;
      await reload();
    };
    const insBtn = $("#mc-install", root);
    if (insBtn) insBtn.onclick = async () => {
      if (installing) return;
      installing = true; insBtn.disabled = true;
      const reset = () => { installing = false; if (!stale(gen) && insBtn.isConnected) insBtn.disabled = false; };
      try {
        const have = ids(), miss = dep ? (dep.missing || []).filter(m => !have.has((m.project || {}).projectId)) : [];
        if (miss.length) { // 购物车之外的必装依赖会自动一起装：装之前说一声装了什么
          const ok = await confirmModal({
            title: `另外会自动装上 ${miss.length} 项必装依赖`,
            body: `购物车里的模组需要它们，安装时会一起下载：<br>${miss.map(m => esc(m.project.title)).join("、")}`,
            okText: "一起安装", cancelText: "先不装", icon: "package",
          });
          if (stale(gen) || !root.isConnected) return; // 等确认框时切走了
          if (!ok) { reset(); return; }
        }
        const n = cart.length;
        /* expect：现在页面上列着的这些。往车里加东西不用确认（AI 助手、别的窗口都能加），
           后端发现车里的和这里看到的不一样就不装，免得装上人没看过的东西。 */
        const r = await api(`/api/instances/${encodeURIComponent(inst)}/modcart/install`, { method: "POST", body: { withDeps: true, expect: cart.map(it => it.projectId) } });
        if (stale(gen)) return;
        const running = ctx.status && ctx.status !== "stopped";
        toast(`已开始安装购物车里的 ${n} 项` + (running ? "；服务器正在运行，装完要重启才生效" : ""));
        location.hash = `#/task/${encodeURIComponent(r.taskId)}`;
      } catch (e) {
        toast(e.message, true);
        reset();
        if (!stale(gen) && root.isConnected) { dep = null; depBox.innerHTML = ""; reload(); } // 车可能变了：重新读一遍，旧的依赖检查结果也作废
      }
    };
    depBtn.onclick = checkDeps;
    reload();
  }

  /* 实例详情的「购物车」标签：info = 实例概要（.name / .core / .mc / .status） */
  async function render(body, info, gen) {
    ensureIcons();
    body.innerHTML = `<div id="mc-local-tip"></div><div id="mc-panel"></div>`;
    buildPanel($("#mc-panel", body), { inst: info.name, core: info.core, mc: info.mc, status: info.status }, gen);
    /* 本地依赖核对（提示条，接口 8）：已装的模组缺依赖时先提醒一声，失败就静默跳过 */
    (async () => {
      try {
        const d = await api(`/api/instances/${encodeURIComponent(info.name)}/mods/deps`);
        if (stale(gen) || !body.isConnected || !d || !d.count) return;
        $("#mc-local-tip", body).innerHTML = `
          <div class="warn-card">
            <div class="wc-title">${icon("warn")} 已装的模组里缺 ${d.count} 项依赖</div>
            <ul class="wc-list">${(d.missing || []).map(m => `<li>${esc(m.id)}（${esc(m.requiredBy)} 需要）</li>`).join("")}</ul>
            <div class="wc-note">缺依赖的模组可能开不了服。购物车里也可以顺便检查一遍：点下面的「检查依赖」。</div>
          </div>`;
      } catch {}
    })();
  }

  /* 向导「选模组」步（可选）：草稿车（不带 inst），创建完成后由 app.js 一并安装 */
  async function wizardPanel(container, opts, gen) {
    ensureIcons();
    draftSeen = null; // 新的一轮向导：等清单画出来再记
    container.innerHTML = `
      <div class="sub">想现在就装模组的话，把它们放进草稿购物车：创建完成后 MineDeck 会自动一起安装。可以留空，以后再到服务器的「购物车」标签里添加。</div>
      <div id="mc-panel"></div>`;
    buildPanel($("#mc-panel", container), { core: opts && opts.core, mc: opts && opts.mc }, gen);
  }

  return { applies, render, wizardPanel, draftSeen: () => draftSeen };
})();
