/* MineDeck 自选模组 + 待装清单：实例详情的「待装清单」标签与新建向导的「选模组 / 选插件」步（可选）。
   叫法：界面上叫「待装清单」（原来叫「购物车」）；代码里的标识符、接口 /api/modcart 和下面的注释仍然写 cart / 购物车 / 车，是同一个东西。
   「依赖」在界面上一律写成玩家更熟的「前置」。
   流程：一打开就列出当前核心和版本下最热门的（Modrinth，按下载量）→ 也可以搜、按类别筛、往后翻 → 挑模组/插件放进购物车；
   自己下载好的 jar 用「从电脑添加…」放进同一张清单 → 检查依赖（缺的勾一下就一并进车）→ 一次装齐（后台任务，kind "mods"）。
   版面：宽的时候左边逛、右边是清单和检查前置（吸顶，边逛边看得到已经选了什么）；窄了叠成一栏（见 modcart.css）。
   向导里是「草稿车」：不传 inst，建服完成后由 app.js 发起安装（includeDraft），装完跳模组安装任务页。
   在 app.js 之前加载；用到的 app.js 工具（api、esc、$、icon、ICON、toast、confirmModal、stale、skeletonRows、fmtBytes、TOKEN、showLogin）
   都在调用时才取，加载顺序不影响。app.js 里只留了钩子：详情页的标签和分派、TASK_KINDS 的 "mods"、向导的「选模组」步与提交链。 */
"use strict";
window.MDModCart = (() => {
  /* 向导「选模组」那一步上最近一次画出来的草稿车：{ expect: [项目 ID], expectLocal: [本地文件的 SHA-1] }；还没画过是 null。
     建服完成后带着草稿车一起装时，把它当成「人看到的那些」发给后端核对（见 app.js 的提交链）：
     草稿车里多出人没看过的东西（上次向导半途放弃留下的文件、AI 助手加的项目），后端不装。 */
  let draftSeen = null;
  /* 有「购物车」标签的实例：同「资源」标签的条件——原版没有模组加载器，不需要 */
  const applies = info => !!info && info.core !== "vanilla";

  /* 这种核心装的是什么：插件端（Paper 一系和两种代理端）是「插件」，其余是「模组」（同后端 resourceSpec：Mohist、Banner 在这里也按模组算） */
  const PLUGIN_CORES = new Set(["paper", "purpur", "leaves", "folia", "velocity", "waterfall"]);
  const nounOf = core => PLUGIN_CORES.has(core) ? "插件" : "模组";

  /* 类别筛选片：Modrinth 的 category slug → 给人看的叫法。后端按同一份白名单校验（internal/modcart/modrinth.go），改这里要一起改 */
  const CATS = {
    模组: [["optimization", "优化"], ["utility", "实用"], ["management", "管理"], ["worldgen", "世界生成"], ["adventure", "冒险"], ["technology", "科技"],
      ["magic", "魔法"], ["mobs", "生物"], ["equipment", "装备"], ["storage", "存储"], ["library", "支持库"]],
    插件: [["management", "管理"], ["utility", "实用"], ["optimization", "优化"], ["economy", "经济"], ["social", "聊天社交"], ["minigame", "小游戏"],
      ["game-mechanics", "游戏机制"], ["worldgen", "世界生成"], ["library", "支持库"]],
  };
  const MAX_LOCAL = 256 * 1024 * 1024; // 单个本地文件的上限，同后端 modcart.MaxLocalSize：超了就不传，免得传完才被拒

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
     ctx.status 为真（服务器在跑）时安装前提示要重启才生效。
     ctx.onBusy(忙不忙)：正在往清单里添加本地文件时通知外面（向导拿它暂时按住「下一步」：文件还没加完就走，
     后加进去的那个不在「人看到的那些」里，建好服务器以后整张清单都装不上）。 */
  function buildPanel(root, ctx, gen) {
    const inst = ctx.inst || "", noun = nounOf(ctx.core), cats = CATS[noun];
    root.innerHTML = `
      <div class="mc-wrap${inst ? "" : " in-wiz"}">
        <section class="mc-browse" aria-label="挑${noun}">
          <div class="row mc-searchbar">
            <div class="search-field">${icon("search", "sf-ico")}<input type="text" id="mc-q" placeholder="搜索${noun}（英文名效果更好）" aria-label="搜索${noun}" maxlength="100"></div>
            <button class="btn" id="mc-go">${icon("search")} 搜索</button>
          </div>
          <div class="cp-chips mc-chips" role="group" aria-label="按类别筛选">
            <button type="button" class="cp-chip" data-cat="" aria-pressed="true">全部</button>
            ${cats.map(([slug, name]) => `<button type="button" class="cp-chip" data-cat="${slug}" aria-pressed="false">${name}</button>`).join("")}
          </div>
          <div class="mc-hits-head">
            <h3 id="mc-hits-title" aria-live="polite"></h3>
            <span class="hint">按当前核心与版本过滤；数据来自 Modrinth</span>
          </div>
          <div id="mc-hits"></div>
          <div id="mc-more-row" class="mc-more-row" hidden></div>
        </section>
        <aside class="mc-side" id="mc-side" aria-label="待装清单">
          <h3 class="mc-gh">待装清单 <span class="hint" id="mc-count" aria-live="polite"></span></h3>
          <div id="mc-list">${skeletonRows(2)}</div>
          <div class="row mc-local-row">
            <button class="btn" id="mc-local-pick">${icon("folder")} 从电脑添加…</button>
            <input type="file" id="mc-file" multiple accept=".jar" hidden>
            <span class="hint">自己下载的 jar 也可以拖到这里</span>
          </div>
          <div id="mc-local-state" aria-live="polite"></div>
          <div class="row mc-actions" id="mc-actions" hidden>
            ${inst ? `<button class="btn primary" id="mc-install">${icon("download")} 全部安装</button>` : ""}
            <button class="btn" id="mc-clear" aria-label="清空待装清单">${icon("trash")} 清空</button>
          </div>
          <h3 class="mc-gh">检查前置</h3>
          <div class="sub mc-dep-sub">装之前可以先查一遍：清单里的${noun}还需要哪些前置（必装的依赖）会列出来，勾上就一并加进清单，免得装完缺前置、服务器起不来。</div>
          <div class="row" style="margin-bottom:10px"><button class="btn" id="mc-dep-go">${icon("package")} 检查前置</button><span class="hint" id="mc-dep-state" aria-live="polite"></span></div>
          <div id="mc-dep-box"></div>
        </aside>
      </div>`;
    const qEl = $("#mc-q", root), hitsEl = $("#mc-hits", root), titleEl = $("#mc-hits-title", root), moreRow = $("#mc-more-row", root),
      listEl = $("#mc-list", root), countEl = $("#mc-count", root), actionsEl = $("#mc-actions", root), sideEl = $("#mc-side", root),
      localState = $("#mc-local-state", root), pickBtn = $("#mc-local-pick", root), fileEl = $("#mc-file", root),
      depBtn = $("#mc-dep-go", root), depState = $("#mc-dep-state", root), depBox = $("#mc-dep-box", root);
    let cart = [],              // 购物车里的 Modrinth 项目（后端的形状）
        local = [],             // 购物车里的本地文件（后端的形状：sha1、fileName、size、title、warn…）
        dep = null,             // 最近一次依赖检查的结果
        installing = false,     // 已经点了安装（等确认框或接口时不能再点）
        uploading = false,      // 正在往清单里添加本地文件
        loadSeq = 0, depSeq = 0;
    /* 浏览区（热门清单 / 搜索结果）的状态 */
    let query = "", cat = "",   // 这一批结果是按什么查的（点了搜索才算数，不跟着输入框变）
        hits = [],              // 已经列出来的
        nextOffset = 0, more = false,
        hitState = "loading",   // loading | ok | error
        hitErr = "", moreErr = "", loadingMore = false,
        searchReq = 0;
    const ids = () => new Set(cart.map(it => it.projectId));
    const gone = () => stale(gen) || !root.isConnected;
    const total = () => cart.length + local.length;
    /* 请求里指明是哪辆车、按哪种核心看（后端拿核心给本地文件写「加载器不对」的提醒） */
    const target = () => ({ inst: inst || undefined, core: ctx.core || undefined, mc: ctx.mc || undefined });
    const targetQuery = () => Object.entries({ inst, core: ctx.core || "", mc: ctx.mc || "" })
      .filter(([, v]) => v !== "").map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join("&");
    const setBusy = b => { uploading = b; pickBtn.disabled = b; if (ctx.onBusy) ctx.onBusy(b); paintActions(); };

    /* ---- 购物车清单 ---- */
    const paintActions = () => {
      actionsEl.hidden = !total();
      const insBtn = $("#mc-install", root);
      if (insBtn) { insBtn.innerHTML = icon("download") + ` 全部安装（${total()}）`; insBtn.disabled = installing || uploading; }
    };
    /* 本地文件那一行下面的提示：先用清单接口给的（加载器不对、读不出信息），查过前置以后换成更准的那一份 */
    const localNotes = lf => {
      const c = dep && (dep.local || []).find(x => x.sha1 === lf.sha1);
      const warn = [], info = [];
      if (c ? c.warn : lf.warn) warn.push(c ? c.warn : lf.warn);
      if (c && c.nameTaken) warn.push("服务器里已经有同名文件，内容不一样。安装时不会覆盖它。");
      if (c && c.skip) info.push(`服务器里已经有一模一样的文件（${c.skip}），安装时跳过。`);
      if (c && c.covers) info.push(`和清单里的 ${c.covers} 是同一个，只装这一份。`);
      return warn.map(w => `<div class="warn-text mc-local-warn">${icon("warn")}<span>${esc(w)}</span></div>`).join("") +
        info.map(t => `<div class="hint mc-local-info">${esc(t)}</div>`).join("");
    };
    const paintList = () => {
      const rows = cart.map(it => `
        <div class="java-row mc-item">
          ${tile(it.icon)}
          <div style="flex:1;min-width:0"><b>${esc(it.title)}</b>
            <div class="jp" style="white-space:normal">${[kindOf(it.kind), it.license].filter(Boolean).map(esc).join(" · ")}</div></div>
          <button class="btn sm ghost mc-del" data-pid="${esc(it.projectId)}" data-t="${esc(it.title)}" aria-label="移除 ${esc(it.title)}">${icon("x")} 移除</button>
        </div>`).concat(local.map(lf => `
        <div class="java-row mc-item mc-local">
          <span class="res-noicon">${icon("doc")}</span>
          <div style="flex:1;min-width:0"><b>${esc(lf.title || lf.fileName)}</b>
            <div class="jp" style="white-space:normal;overflow-wrap:anywhere">本地文件 · ${esc(lf.fileName)}${lf.size ? " · " + fmtBytes(lf.size) : ""}</div>
            ${localNotes(lf)}</div>
          <button class="btn sm ghost mc-del-local" data-sha="${esc(lf.sha1)}" aria-label="移除本地文件 ${esc(lf.fileName)}">${icon("x")} 移除</button>
        </div>`));
      listEl.innerHTML = rows.length ? rows.join("")
        : `<div class="sub" id="mc-empty">清单是空的。在列表里点「加入清单」，或用下面的「从电脑添加…」放入自己下载的文件，挑齐后一起安装。</div>`;
      countEl.textContent = total() ? `共 ${total()} 项` : "";
      if (!inst) draftSeen = { expect: cart.map(it => it.projectId), expectLocal: local.map(lf => lf.sha1) }; // 草稿车：记下画在页面上的是哪些
      paintActions();
      listEl.querySelectorAll(".mc-del").forEach(b => b.onclick = () => removeOne(b, "/api/modcart/remove", { projectId: b.dataset.pid }, false));
      listEl.querySelectorAll(".mc-del-local").forEach(b => b.onclick = () => removeOne(b, "/api/modcart/local/remove", { sha1: b.dataset.sha }, true));
      paintHits(); // 车里有了/少了，搜索结果按钮的状态跟着变
      if (dep) paintDeps(); // 依赖检查的结果也跟着变：已经进车的不再算缺
    };
    /* 用接口回来的整张清单换掉页面上的 */
    const take = d => { cart = d.items || []; local = d.local || []; paintList(); };
    const removeOne = async (btn, url, body, isLocal) => {
      btn.disabled = true;
      ++loadSeq; // 还没回来的读车请求作废：这次移除的结果更新
      try {
        const d = await api(url, { method: "POST", body: { ...target(), ...body } });
        if (gone()) return;
        take(d);
        if (isLocal && dep) checkDeps(); // 本地文件少了一个，上次查的前置不作数了，重查一遍
      } catch (e) { toast(e.message, true); if (!gone() && btn.isConnected) btn.disabled = false; }
    };

    /* ---- 浏览区：热门清单 / 搜索结果（最近一批结果 + 当前车状态）---- */
    const catName = () => (cats.find(([slug]) => slug === cat) || [])[1] || "";
    const paintTitle = () => {
      titleEl.textContent = query ? `“${query}” 的搜索结果${cat ? " · " + catName() : ""}`
        : cat ? `${catName()}类${noun} · 按下载量` : `热门${noun} · 按下载量`;
    };
    const paintHits = () => {
      hitsEl.setAttribute("aria-busy", hitState === "loading" ? "true" : "false");
      if (hitState === "loading") { hitsEl.innerHTML = skeletonRows(5); moreRow.hidden = true; return; }
      if (hitState === "error") {
        /* 出错只在列表区说，不弹提示打扰；这一块坏了不影响别的（清单、从电脑添加、向导的下一步都照常能用） */
        hitsEl.innerHTML = `<div class="err-box" id="mc-hits-err">${noun}列表没有加载出来：${esc(hitErr)}
          <div>检查一下网络或代理，再点「重试」。${inst ? "" : "这一步可以先跳过，不影响建服务器；"}自己下载好的文件仍然可以用「从电脑添加…」放进清单。</div>
          <div class="row" style="margin-top:8px"><button class="btn sm" id="mc-retry" aria-label="重试加载${noun}列表">${icon("refresh")} 重试</button></div></div>`;
        $("#mc-retry", root).onclick = () => load(false);
        moreRow.hidden = true;
        return;
      }
      const have = ids();
      hitsEl.innerHTML = hits.length ? hits.map(h => {
        const inCart = have.has(h.project_id);
        return `
        <div class="java-row mc-hit">
          ${tile(h.icon_url)}
          <div class="mc-hit-main"><b>${esc(h.title)}</b>
            <div class="jp" title="${esc(h.description)}">${esc(h.description)}</div></div>
          <span class="jv mc-license" title="${esc(h.license || "")}">${esc(h.license || "")}</span>
          <span class="jv mc-down">${fmtDown(h.downloads)}${icon("download")}</span>
          <button class="btn sm mc-add" data-pid="${esc(h.project_id)}" data-t="${esc(h.title)}" aria-label="${inCart ? "已在清单" : "加入清单"} ${esc(h.title)}"${inCart ? " disabled" : ""}>${icon(inCart ? "check" : "plus")} ${inCart ? "已在清单" : "加入清单"}</button>
        </div>`;
      }).join("") : `<div class="sub" id="mc-hits-empty">${query || cat ? `没有找到合适的${noun}。换个关键词或类别试试。` : `这个核心和版本下，Modrinth 上暂时没有能装的${noun}。`}</div>`;
      hitsEl.querySelectorAll(".mc-add").forEach(b => b.onclick = () => add([b.dataset.pid], [b]));
      /* 往后翻：还有下一页给按钮；翻到头了（最多 5 页）说一声去哪儿找别的 */
      moreRow.hidden = !hits.length;
      moreRow.innerHTML = (moreErr ? `<div class="err-box" id="mc-more-err">后面的没有加载出来：${esc(moreErr)}。再点一次「加载更多」试试。</div>` : "") +
        (more ? `<button class="btn" id="mc-more"${loadingMore ? " disabled" : ""}>${loadingMore ? icon("spinner") + " 加载中…" : icon("chevronDown") + " 加载更多"}</button>
            <span class="hint">已列出 ${hits.length} 个</span>`
          : `<span class="hint" id="mc-more-end">${hits.length >= 100 ? `只列出最热门的 ${hits.length} 个。想找别的，用上面的搜索或类别。` : `已显示全部 ${hits.length} 个。`}</span>`);
      const moreBtn = $("#mc-more", root);
      if (moreBtn) moreBtn.onclick = () => load(true);
    };
    /* 取一页结果。append=false 是重新查（换了关键词或类别、重试、刚打开）；true 是「加载更多」，接在后面。
       切页竞态：每次请求带序号，只认最后一次；离开页面以后回来的不写页面。 */
    const load = async append => {
      const req = ++searchReq;
      const outdated = () => gone() || req !== searchReq;
      moreErr = "";
      if (append) { loadingMore = true; }
      else { hitState = "loading"; hits = []; nextOffset = 0; more = false; loadingMore = false; paintTitle(); }
      paintHits();
      try {
        const qs = Object.entries({ q: query, category: cat, offset: append ? String(nextOffset) : "", inst, core: ctx.core || "", mc: ctx.mc || "" })
          .filter(([, v]) => v !== "").map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join("&");
        const r = await api("/api/modcart/search?" + qs);
        if (outdated()) return;
        const got = r.hits || [];
        hits = append ? hits.concat(got.filter(h => !hits.some(x => x.project_id === h.project_id))) : got;
        nextOffset = (Number.isFinite(r.offset) ? r.offset : (append ? nextOffset : 0)) + 20;
        more = !!r.more;
        hitState = "ok"; loadingMore = false;
      } catch (e) {
        if (outdated()) return;
        loadingMore = false;
        if (append) moreErr = e.message; else { hitState = "error"; hitErr = e.message; }
      }
      paintHits();
    };
    const search = () => { query = qEl.value.trim(); load(false); };
    $("#mc-go", root).onclick = search;
    qEl.onkeydown = e => { if (e.key === "Enter") { e.preventDefault(); search(); } };
    root.querySelectorAll(".mc-chips .cp-chip").forEach(c => c.onclick = () => {
      if (c.dataset.cat === cat) return;
      cat = c.dataset.cat;
      root.querySelectorAll(".mc-chips .cp-chip").forEach(x => x.setAttribute("aria-pressed", String(x === c)));
      query = qEl.value.trim(); // 输入框里改了还没点搜索的关键词，换类别时一起算上，标题和结果才对得上
      load(false);
    });

    /* 把项目加进购物车；btns 是正在转圈的按钮（搜索结果里逐个换字） */
    const add = async (projectIds, btns) => {
      ++loadSeq; // 还没回来的读车请求作废：这次加入的结果更新
      btns.forEach(b => { b.disabled = true; const t = b.dataset.t; b.setAttribute("aria-label", "加入中 " + t); b.innerHTML = icon("spinner") + " 加入中…"; });
      try {
        const d = await api("/api/modcart", { method: "POST", body: { ...target(), projectIds } });
        if (gone()) return;
        take(d);
        /* 查不到的、只在客户端用的模组后端不放进车，在 skipped 里说明原因 */
        const skipped = d.skipped || [];
        const one = btns[0] ? btns[0].dataset.t : (cart.find(it => it.projectId === projectIds[0]) || {}).title;
        if (skipped.length) toast(skipped.map(x => `${x.title || x.projectId}：${x.reason}`).join("；"), true);
        else toast(projectIds.length > 1 ? `已加入清单：${projectIds.length} 项前置` : `已加入清单：${one || "1 项"}`);
      } catch (e) {
        toastErr("无法加入清单", e);
        if (!gone()) paintList(); // 加购失败也逐个按钮复位
      }
    };

    /* ---- 从电脑添加本地文件 ----
       用 <input type="file">（不用主机的文件选择框）：局域网里拿手机或别的电脑打开界面时也能用。一个一个传，
       传的时候在清单下面写「正在添加 x / n」；哪个没加上，原因就近列在清单下面（不用一串提示框）。加完自动查一遍前置。 */
    const paintLocalState = (text, fails, okText) => {
      localState.innerHTML = (text ? `<div class="hint mc-local-prog">${icon("spinner")} ${esc(text)}</div>` : "") +
        (okText ? `<div class="hint" id="mc-local-ok">${esc(okText)}</div>` : "") +
        (fails && fails.length ? `<div class="err-box" id="mc-local-err">有 ${fails.length} 个文件没有加进清单：
            <ul class="wc-list">${fails.map(([name, why]) => `<li><b>${esc(name)}</b>：${esc(why)}</li>`).join("")}</ul>
            <div class="row" style="margin-top:6px"><button class="btn sm" id="mc-local-dismiss" aria-label="关掉这条添加失败的说明">知道了</button></div></div>` : "");
      const x = $("#mc-local-dismiss", root);
      if (x) x.onclick = () => { localState.innerHTML = ""; };
    };
    const addFiles = async files => {
      const list = [...files];
      if (!list.length || uploading) return;
      setBusy(true);
      /* 人走了（切页、向导换步）就把还在传的掐掉：不然文件在人离开以后才落进清单，没人看过 */
      const abort = new AbortController();
      const watch = setInterval(() => { if (gone()) { abort.abort(); clearInterval(watch); } }, 150);
      const fails = [];
      let added = 0, dup = 0;
      try {
        for (let i = 0; i < list.length; i++) {
          const f = list[i];
          paintLocalState(`正在添加 ${i + 1} / ${list.length}：${f.name}`, fails);
          if (!/\.jar$/i.test(f.name)) { fails.push([f.name, "不是 .jar 文件。模组和插件都是 .jar 文件，压缩包要先解压。"]); continue; }
          if (f.size > MAX_LOCAL) { fails.push([f.name, `有 ${fmtBytes(f.size)}，超过了 256 MB 的上限。可以等服务器建好以后，自己把它拷进服务器的文件夹。`]); continue; }
          try {
            const fd = new FormData();
            fd.append("file", f);
            for (const [k, v] of Object.entries(target())) if (v) fd.append(k, v);
            const res = await fetch("/api/modcart/local", { method: "POST", headers: { "X-Token": TOKEN() }, body: fd, signal: abort.signal });
            let j;
            try { j = await res.json(); } catch { throw new Error(`服务异常 (HTTP ${res.status})`); }
            if (res.status === 401) { showLogin(); throw new Error(j.error || "需要登录"); }
            if (!j.ok) throw new Error(j.error || "添加失败");
            if (gone()) return;
            ++loadSeq; // 还没回来的读车请求作废
            take(j.data);
            if (j.data.duplicate) dup++; else added++;
          } catch (e) {
            if (gone()) return;
            fails.push([f.name, e.message]);
          }
        }
      } finally { clearInterval(watch); if (!gone()) setBusy(false); }
      const said = [added ? `已添加 ${added} 个文件` : "", dup ? `${dup} 个已经在清单里了，没有重复添加` : ""].filter(Boolean).join("；");
      paintLocalState("", fails, said ? said + "。" : "");
      if (added || dup) checkDeps(); // 加完自动查一遍前置
    };
    pickBtn.onclick = () => fileEl.click();
    fileEl.onchange = () => { const fs = [...fileEl.files]; fileEl.value = ""; addFiles(fs); }; // 清掉选择：同一个文件移除以后还能再选一次
    /* 把文件拖进来也行。整块面板都接（拖偏到左边的列表上也算数：不接的话浏览器会把这个 jar 当成网页打开或者下载下来），
       高亮只画在清单那一栏，告诉人文件会落到哪儿 */
    const hasFiles = e => e.dataTransfer && [...(e.dataTransfer.types || [])].includes("Files");
    let dragDepth = 0;
    root.addEventListener("dragenter", e => { if (hasFiles(e)) { e.preventDefault(); dragDepth++; sideEl.classList.add("mc-drop"); } });
    root.addEventListener("dragover", e => { if (hasFiles(e)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } });
    root.addEventListener("dragleave", e => { if (hasFiles(e) && --dragDepth <= 0) { dragDepth = 0; sideEl.classList.remove("mc-drop"); } });
    root.addEventListener("drop", e => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      dragDepth = 0; sideEl.classList.remove("mc-drop");
      addFiles(e.dataTransfer.files);
    });

    /* ---- 依赖检查 ---- */
    const depRow = (m, inner, cls) => `
        <div class="java-row ${cls}">
          ${inner}
          <div style="flex:1;min-width:0"><b>${esc(m.project.title)}</b>
            <div class="jp" style="white-space:normal">${esc(m.project.fileName)}${m.project.size ? " · " + fmtBytes(m.project.size) : ""}${m.dependencyOf ? " · " + esc(m.dependencyOf) + " 的前置" : ""}</div></div>`;
    const paintDeps = () => {
      const have = ids();
      const missing = (dep.missing || []).filter(m => !have.has((m.project || {}).projectId));
      const missRows = missing.map(m => depRow(m, `<label class="mc-check"><input type="checkbox" checked data-pid="${esc(m.project.projectId)}" aria-label="补上 ${esc(m.project.title)}"></label>`, "mc-miss") + `
          <span class="jv">必装</span>
        </div>`).join("");
      /* 可选前置的按钮名字里写明「可选前置」：同一个项目可能正好也列在左边的热门清单里，两个按钮不能同名 */
      const optRows = (dep.optional || []).map(m => depRow(m, tile(""), "mc-opt") + `
          <button class="btn sm ghost mc-opt-add" data-pid="${esc(m.project.projectId)}" data-t="${esc(m.project.title)}" aria-label="${have.has(m.project.projectId) ? "已在清单" : "加入清单"}（可选前置）${esc(m.project.title)}"${have.has(m.project.projectId) ? " disabled" : ""}>${have.has(m.project.projectId) ? icon("check") + " 已在清单" : icon("plus") + " 加入清单"}</button>
        </div>`).join("");
      /* files 是安装时要下载的全部文件（fromDep 的是自动补的必装前置）；missing 是购物车之外还需要的必装前置：
         安装时默认会一起装上，这里也可以勾选后正式放进购物车。
         本地文件：local 逐个一行（recognized = 在 Modrinth 上认出来了，它的前置已经算进 files / missing）；
         manual 是认不出来的本地文件自己写明要、哪儿都没找到的前置——只知道名字，没法代为下载。 */
      const files = dep.files || [], auto = files.filter(f => f.fromDep).length;
      const locals = dep.local || [], known = locals.filter(x => x.recognized).length, manual = dep.manual || [];
      depBox.innerHTML = `
        ${files.length ? `<div class="hint" style="margin-bottom:8px">安装时会下载 ${files.length} 个文件${auto ? `（其中 ${auto} 个是自动补上的必装前置）` : ""}：${files.map(f => esc(f.fileName)).join("、")}</div>` : ""}
        ${locals.length ? `<div class="hint" id="mc-dep-local" style="margin-bottom:8px">清单里有 ${locals.length} 个本地文件${known ? `，其中 ${known} 个在 Modrinth 上认出来了，它们的前置已经一起算进来` : ""}。${known < locals.length ? "认不出来的，按文件自己写明的前置检查。" : ""}${dep.localNote ? esc(dep.localNote) : ""}</div>` : ""}
        ${missing.length ? `
        <div class="mc-dep-sec"><b class="mc-dep-t">还缺的前置（必装）</b>
          <div class="hint">安装时会自动一起装上。也可以勾上加进清单，和别的放在一起管。</div>
          <div class="row" style="margin:6px 0 10px"><button class="btn primary sm" id="mc-dep-add" aria-label="把勾选的加入清单">${icon("plus")} 把勾选的加入清单</button></div>
          ${missRows}
        </div>` : manual.length ? "" : `<div class="hint" id="mc-dep-ok">不缺前置，清单里的可以直接装。</div>`}
        ${manual.length ? `<div class="warn-card" id="mc-dep-manual" style="margin:8px 0">
          <div class="wc-title">${icon("warn")} 有 ${manual.length} 个前置要自己去找</div>
          <ul class="wc-list">${manual.map(x => `<li><b>${esc(x.id)}</b>（${esc(x.requiredBy)} 需要）</li>`).join("")}</ul>
          <div class="wc-note">这是你添加的本地文件自己写明要的前置。我们只知道它的名字，没法替你下载：到 Modrinth 或 CurseForge 搜这个名字，下载对应的 jar，再用「从电脑添加…」放进清单。</div></div>` : ""}
        ${(dep.optional || []).length ? `<div class="mc-dep-sec"><b class="mc-dep-t">可选前置（按需加入）</b>${optRows}</div>` : ""}
        ${(dep.conflicts || []).length ? `<div class="warn-card" style="margin:8px 0">
          <div class="wc-title">${icon("warn")} 有冲突，装之前先看一眼</div>
          <ul class="wc-list">${dep.conflicts.map(c => `<li>${esc(c)}</li>`).join("")}</ul></div>` : ""}
        ${(dep.localHits || []).length ? `<div class="hint" style="margin:8px 0">服务器目录里已有的文件，安装时会跳过：${dep.localHits.map(esc).join("、")}</div>` : ""}
        ${(dep.unresolved || []).length ? `<div class="warn-card" style="margin:8px 0">
          <div class="wc-title">${icon("warn")} 查不到的前置</div>
          <ul class="wc-list">${dep.unresolved.map(x => `<li>${esc(x.title)}：${esc(x.reason)}</li>`).join("")}</ul></div>` : ""}`;
      const addBtn = $("#mc-dep-add", root);
      if (addBtn) addBtn.onclick = async () => {
        const picked = [...depBox.querySelectorAll(".mc-miss input:checked")].map(i => i.dataset.pid);
        if (!picked.length) { toast("先勾上要补的前置", true); return; }
        addBtn.disabled = true; // add 之后会重画整块，按钮跟着换新的
        await add(picked, []);
        if (gone()) return;
        checkDeps(); // 补进车后重新核对一遍，剩余的缺失跟着更新
      };
      depBox.querySelectorAll(".mc-opt-add").forEach(b => b.onclick = () => add([b.dataset.pid], [b]));
    };
    const checkDeps = async () => {
      const seq = ++depSeq;
      const outdated = () => seq !== depSeq || gone();
      depBtn.disabled = true;
      depState.textContent = "检查中…";
      try {
        const d = await api("/api/modcart/deps?" + targetQuery());
        if (outdated()) return;
        dep = d;
        depState.textContent = "";
        paintList(); // 本地文件那几行的提示换成这次查出来的（里面会接着画前置这一块）
      } catch (e) {
        if (outdated()) return;
        dep = null;
        depState.textContent = "";
        depBox.innerHTML = `<div class="err-box">前置没有查出来：${esc(e.message)}<div>检查一下网络，再点一次「检查前置」。</div></div>`;
      } finally { if (!outdated()) depBtn.disabled = false; }
    };

    /* ---- 读一遍购物车 ---- */
    const reload = async () => {
      const seq = ++loadSeq;
      const outdated = () => seq !== loadSeq || gone();
      try {
        const d = await api("/api/modcart" + (targetQuery() ? "?" + targetQuery() : ""));
        if (outdated()) return;
        take(d);
      } catch (e) { if (!outdated()) listEl.innerHTML = `<div class="err-box">待装清单没有读出来：${esc(e.message)}</div>`; }
    };

    /* ---- 清空 / 安装（详情页）---- */
    const clearBtn = $("#mc-clear", root);
    clearBtn.onclick = async () => {
      if (!total() || clearBtn.disabled) return;
      clearBtn.disabled = true;
      try { await api("/api/modcart/clear", { method: "POST", body: target() }); }
      catch (e) { toast(e.message, true); if (!gone() && clearBtn.isConnected) clearBtn.disabled = false; return; }
      if (gone()) return;
      clearBtn.disabled = false;
      if (dep) { dep = null; depBox.innerHTML = ""; } // 清单空了，上次查的前置不作数了
      await reload();
    };
    const insBtn = $("#mc-install", root);
    if (insBtn) insBtn.onclick = async () => {
      if (installing || uploading) return;
      installing = true; insBtn.disabled = true;
      const reset = () => { installing = false; if (!gone() && insBtn.isConnected) insBtn.disabled = false; };
      try {
        const have = ids(), miss = dep ? (dep.missing || []).filter(m => !have.has((m.project || {}).projectId)) : [];
        if (miss.length) { // 购物车之外的必装依赖会自动一起装：装之前说一声装了什么
          const ok = await confirmModal({
            title: `另外会自动装上 ${miss.length} 项必装前置`,
            body: `清单里的${noun}需要它们，安装时会一起下载：<br>${miss.map(m => esc(m.project.title)).join("、")}`,
            okText: "一起安装", cancelText: "先不装", icon: "package",
          });
          if (gone()) return; // 等确认框时切走了
          if (!ok) { reset(); return; }
        }
        const n = total();
        /* expect / expectLocal：现在页面上列着的这些（Modrinth 项目的 ID、本地文件的 SHA-1）。往车里加东西不用确认
           （AI 助手、别的窗口都能加），后端发现车里的和这里看到的不一样就不装，免得装上人没看过的东西。 */
        const r = await api(`/api/instances/${encodeURIComponent(inst)}/modcart/install`, { method: "POST",
          body: { withDeps: true, expect: cart.map(it => it.projectId), expectLocal: local.map(lf => lf.sha1) } });
        if (stale(gen)) return;
        const running = ctx.status && ctx.status !== "stopped";
        toast(`已开始安装清单里的 ${n} 项` + (running ? "。服务器正在运行，装完要重启才生效" : ""));
        location.hash = `#/task/${encodeURIComponent(r.taskId)}`;
      } catch (e) {
        toastErr("无法开始安装", e);
        reset();
        if (!gone()) { dep = null; depBox.innerHTML = ""; reload(); } // 车可能变了：重新读一遍，旧的依赖检查结果也作废
      }
    };
    depBtn.onclick = checkDeps;
    reload();
    load(false); // 一打开就列出最热门的。失败或很慢都只影响左边这一块
  }

  /* 实例详情的「购物车」标签：info = 实例概要（.name / .core / .mc / .status） */
  async function render(body, info, gen) {
    body.innerHTML = `<div id="mc-local-tip"></div><div id="mc-panel"></div>`;
    buildPanel($("#mc-panel", body), { inst: info.name, core: info.core, mc: info.mc, status: info.status }, gen);
    /* 本地依赖核对（提示条，接口 8）：已装的模组缺依赖时先提醒一声，失败就静默跳过 */
    (async () => {
      try {
        const d = await api(`/api/instances/${encodeURIComponent(info.name)}/mods/deps`);
        if (stale(gen) || !body.isConnected || !d || !d.count) return;
        $("#mc-local-tip", body).innerHTML = `
          <div class="warn-card">
            <div class="wc-title">${icon("warn")} 已装的模组里缺 ${d.count} 项前置</div>
            <ul class="wc-list">${(d.missing || []).map(m => `<li>${esc(m.id)}（${esc(m.requiredBy)} 需要）</li>`).join("")}</ul>
            <div class="wc-note">缺前置的模组可能让服务器起不来。可以在下面搜到它们，加进清单装上。</div>
          </div>`;
      } catch {}
    })();
  }

  /* 向导「选模组」步（可选）：草稿车（不带 inst），创建完成后由 app.js 一并安装。opts.onBusy 见 buildPanel */
  async function wizardPanel(container, opts, gen) {
    const noun = nounOf(opts && opts.core);
    draftSeen = null; // 新的一轮向导：等清单画出来再记
    container.innerHTML = `
      <div class="sub mc-wiz-sub">想一开服就带上${noun}，现在就把它们加进待装清单，服务器建好后会自动装上。也可以先跳过，以后在这台服务器的「待装清单」标签里再加。</div>
      <div id="mc-panel"></div>`;
    buildPanel($("#mc-panel", container), { core: opts && opts.core, mc: opts && opts.mc, onBusy: opts && opts.onBusy }, gen);
  }

  return { applies, render, wizardPanel, nounOf, draftSeen: () => draftSeen };
})();
