// 全局设置 →「下载与网络」（11-27 夜里第一稿；11-28 重画：作者嫌这一页太空，问模组、核心、音乐共享、软件更新这些下载源能不能都放到这里）。
// 样式在 settings-download.css，选择器都带 sd 前缀。
//
// 程序现在的样子（internal/web/ui/app.js setDownload，约 5893 行）：两段。
//   · 下载源：标题「下载源」加一句「下载 Java 与服务端核心时的线路选择」，三行单选（自动（推荐）/ 仅国内镜像 / 仅官方源，每行一句说明，选中的打勾），点了马上存，弹「下载源已切换」。
//     这一项（config.source）只管服务端核心和 Java：Mojang、Forge、NeoForge、Fabric 走 BMCLAPI（NeoForge 还有 FastMirror），Java 走清华镜像；
//     Paper、Purpur、Leaves、Mohist 没有国内镜像，永远走官方。
//   · CurseForge API Key（选填）：一句说明加「免费申请」链接；一行「API Key」，下面一句（加密保存在这台电脑上……，解不开时换成黄字「密钥无法解开……请重新填写」），
//     密码框（占位「粘贴 API Key」，已设置时「已设置（留空不修改）」，存过的 Key 从不回显）、「保存」、设置过或解不开时多一颗「清除」（危险确认框）。
//   别处和下载有关、现在没有设置入口的：模组和插件只从 Modrinth 官方下（AI 伙伴模组、音乐共享要装的 Simple Voice Chat 也是）；软件更新只走 GitHub，
//   界面上是「更新与关于」页的「更新仓库」框；用户逐个确认过的下载站点记在 config.trustedHosts（internal/app/app.go，整合包里白名单外的直链，确认一次以后不再问），
//   没有地方看、也没有地方取消；下载缓存按校验值复用，没有看占用、清理的入口。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：一栏（圆角 18 = 26 − 8，内边距 16 / 20）占满面板，表单两列（各 398 宽、列距 16），
// 照向导第三步「字 18 + 6 + 控件 40」，行距 24。这一页没有保存条：分段点了马上生效，Key 在框里保存，移除站点、清理缓存都是当场做完。
//   · 第一排：服务端核心与 Java ｜ 模组与插件。第二排：软件更新 ｜ CurseForge API Key（选填）。一道细线。第三排：信任的下载站点 ｜ 下载缓存。
//   · 三项线路都是 40 高分段（三段，同向导第三步），选中块滑过去；每段的说明进那一段的悬停，覆盖范围进名字的悬停。点了马上存，不弹提示，选中块滑过去就是结果。
//       服务端核心与 Java：自动（推荐）/ 仅国内镜像 / 仅官方源，就是程序现在那一项，三句说明照原样。
//       模组与插件：自动（推荐）/ 仅国内镜像 / 仅官方源；国内镜像是 MCIM（社区维护的 Modrinth 镜像）。
//       软件更新：自动（推荐）/ 仅官网 / 仅 GitHub；从「更新与关于」的「更新仓库」挪过来、换成选线路（11-28 作者问起；那一页不再有更新仓库的框）。
//   · CurseForge API Key（选填）：名字后面一个外链图标（免费申请，草图里不真打开网页）；说明进名字的悬停。下面一个 40 高的框（密码框，不回显存过的 Key），
//     框里右边一颗保存（28 见方、离框边 6、圆角 4，框里没字时灰着；回车也能存），设置过或解不开时再多一颗清除（垃圾桶，共用的危险确认框 KIT.confirm）。
//     存好以后框清空、占位字换成「已设置（留空不修改）」；解不开时名字那一行右边一句黄字「密钥无法解密」（会改变结果，留在页上；全文进悬停），框一圈黄边，填一个新的存下去就好了。
//   · 信任的下载站点：名字后面一个淡色的个数。下面一个列表框，一行 40 高：主机名（等宽）、右边淡色的「11-26 确认」和一颗 ×（28 见方、离框边 6、圆角 4）。
//     点 × 马上移除，不弹确认、不弹提示：那一行往右划 32 像素淡掉再收起（规范第七节「清单加减」），个数跟着减。行多了在框里滚（细滚动条，不撑高页面），
//     一条都没有时框里一句淡字「暂无信任的站点」。
//   · 下载缓存：一个 40 高的框，左边总占用（等宽）、后面淡色的分项，框里右边一颗 28 高胶囊「清理」（同「存储位置」排除项框里的「加入…」）。
//     点了是危险确认框，确认后分项淡掉、按钮转圈「清理中…」约半秒，然后总占用变「0 B」、按钮灰掉（悬停「缓存已清空」）。
// 程序还没有的（改程序时要补后端，写进清单）：
//   · 模组与插件的镜像线路（MCIM）和这一项设置；AI 伙伴模组、Simple Voice Chat、整合包里的 Modrinth 文件跟着这一项走。
//   · 软件更新三选一：官网（minedeck.cn 的 latest.json，下载站 dl.minedeck.cn）和 GitHub 都查、取较新的版本，下载依次试官网下载站、GitHub 加速站、GitHub 直连；
//     校验值以 GitHub 为准，连不上才用官网的。
//   · 信任站点的列表和移除（数据已有，缺读取和删除的接口）。
//   · 下载缓存的占用统计（按核心、Java、模组分项）和清理。
// 换分类回来：选过的线路、移除过的站点、清理过的缓存都还在（数据在这个文件里）。
// 示例数据：信任站点两条（mediafilez.forgecdn.net、edge.forgecdn.net，是 CurseForge 真有的下载域名，日期是草图编的）；缓存占用 612 + 487 + 236 MB 是草图编的。
// 草图专用：&src=mirror|official、&mod=mirror|official、&upd=site|github 一打开选的是别的线路；&cf=set 已经设置过 Key，&cf=broken 存着的 Key 解不开；
//           &trust=0 没有信任站点，&trust=many 十二条（看框里滚动，后十条主机名是真有的站点、有没有人会信任是编的）；&cache=0 缓存已空。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const setTip = (el, text) => {
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  const setAttr = (el, k, v) => { if (el.getAttribute(k) !== String(v)) el.setAttribute(k, v); };

  // 三项线路。src 同程序 setDownload 的三项和说明；mod、upd 是新加的
  const LINES = {
    src: { name: "服务端核心与 Java",
      tip: "下载服务端核心与 Java 时使用的线路。\nMojang 原版、Forge、NeoForge、Fabric 经 BMCLAPI 等国内镜像下载，Java 经清华镜像下载；Paper、Purpur、Leaves、Mohist 无国内镜像，始终从官方下载。",
      opts: [
        { v: "auto", t: "自动（推荐）", d: "国内镜像（BMCLAPI / 清华）优先，失败自动切换官方源" },
        { v: "mirror", t: "仅国内镜像", d: "仅使用 BMCLAPI 与清华镜像（Paper、Purpur、Leaves、Mohist 无国内镜像，仍从官方下载）" },
        { v: "official", t: "仅官方源", d: "仅使用 Mojang / Forge / Adoptium 等官方源（海外网络适用）" },
      ] },
    mod: { name: "模组与插件",
      tip: "下载模组与插件时使用的线路，国内镜像为 MCIM（社区维护的 Modrinth 镜像）。\n适用于「模组 / 插件」页的搜索与安装、整合包中的 Modrinth 文件、AI 伙伴模组，以及音乐共享所需的 Simple Voice Chat。",
      opts: [
        { v: "auto", t: "自动（推荐）", d: "国内镜像（MCIM）优先，失败自动切换 Modrinth 官方" },
        { v: "mirror", t: "仅国内镜像", d: "仅使用 MCIM 镜像，新上架的文件可能尚未同步" },
        { v: "official", t: "仅官方源", d: "仅使用 Modrinth 官方（海外网络适用）" },
      ] },
    upd: { name: "软件更新",
      tip: "检查与下载 MineDeck 自身更新时使用的线路。\n无论选择哪条线路，均先向 GitHub 核对校验值，无法连接时才使用官网提供的校验值。",
      opts: [
        { v: "auto", t: "自动（推荐）", d: "官网与 GitHub 均检查，取较新的版本；下载依次尝试官网下载站、GitHub 加速站、GitHub" },
        { v: "site", t: "仅官网", d: "仅从官网检查与下载" },
        { v: "github", t: "仅 GitHub", d: "仅从 GitHub 检查与下载（海外网络适用）" },
      ] },
  };
  const CF_TIP = "导入 CurseForge 格式整合包并下载其中的模组时使用，Modrinth 格式无需填写。\n密钥加密保存在本机，更换电脑或 Windows 用户后需重新填写。";
  const BROKEN_TIP = "密钥无法解密，可能已更换电脑或 Windows 用户，请重新填写";
  const TRUST_TIP = "导入整合包时，来自 Modrinth、GitHub、GitLab 以外站点的文件需逐个确认；确认过的站点记录在此，之后不再询问。\n移除后，下次遇到该站点时将重新询问。";
  const CACHE_TIP = "已下载的服务端核心、Java 与模组安装包按校验值保存在此，新建服务器时直接复用。\n清理不影响已建好的服务器，之后用到时重新下载。";
  const TRUST = [{ host: "mediafilez.forgecdn.net", at: "11-26" }, { host: "edge.forgecdn.net", at: "11-20" }];
  const TRUST_MORE = [["media.forgecdn.net", "11-20"], ["maven.minecraftforge.net", "11-14"], ["maven.neoforged.net", "11-14"], ["maven.fabricmc.net", "11-09"], ["libraries.minecraft.net", "11-09"],
    ["optifine.net", "11-02"], ["repo1.maven.org", "10-28"], ["jitpack.io", "10-28"], ["dl.cloudsmith.io", "10-19"], ["www.mediafire.com", "10-05"]].map(([host, at]) => ({ host, at }));
  const PARTS = [{ k: "core", t: "核心" }, { k: "java", t: "Java" }, { k: "mods", t: "模组" }];

  let C = null, D = null;
  function data() {
    if (D) return D;
    const q = C.QS, cf = q.get("cf"), tr = q.get("trust"), pick = g => (LINES[g].opts.some(o => o.v === q.get(g)) ? q.get(g) : "auto");
    D = { src: pick("src"), mod: pick("mod"), upd: pick("upd"), set: cf === "set", broken: cf === "broken", draft: "", saving: false,
      trust: tr === "0" ? [] : tr === "many" ? [...TRUST, ...TRUST_MORE] : [...TRUST],
      cache: q.get("cache") === "0" ? { core: 0, java: 0, mods: 0 } : { core: 612, java: 487, mods: 236 }, cleaning: false }; // 单位 MB
    return D;
  }
  const root = () => document.querySelector(".cp > .sd:not(.fade-out)");
  const holder = d => (d.set ? "已设置（留空不修改）" : "粘贴 API Key"); // 同程序 cfKeyHolder
  const fmtSize = mb => (mb >= 1024 ? (mb / 1024).toFixed(1) + " GB" : mb > 0 ? mb + " MB" : "0 B");
  const total = d => PARTS.reduce((n, p) => n + d.cache[p.k], 0);

  // ---- 画 ----
  const lineHTML = (d, g) => `<div class="sd-f"><div class="sd-lb"><span title="${esc(LINES[g].tip)}">${LINES[g].name}</span></div>`
    + `<div class="sd-seg" role="radiogroup" aria-label="${LINES[g].name}" data-g="${g}"><i class="sd-sgl"></i>${LINES[g].opts.map(o =>
      `<button type="button" role="radio" data-v="${o.v}" aria-checked="${d[g] === o.v}" class="${d[g] === o.v ? "on" : ""}" title="${esc(o.d)}">${esc(o.t)}</button>`).join("")}</div></div>`;
  const saveOff = d => !d.draft.trim() || d.saving;
  const keyHTML = d => `<div class="sd-f"><div class="sd-lb"><span title="${esc(CF_TIP)}">CurseForge API Key（选填）</span>`
    + `<button type="button" class="sd-lk" data-sd="apply" title="免费申请（console.curseforge.com）" aria-label="免费申请 CurseForge API Key"><i data-ic="share"></i></button>`
    + `<span class="sd-sp"></span><span class="sd-warn"${d.broken ? "" : " hidden"} title="${esc(BROKEN_TIP)}"><i data-ic="warn"></i>密钥无法解密</span></div>`
    + `<label class="sd-iw${d.broken ? " warn" : ""}"><input class="sd-in" type="password" autocomplete="off" spellcheck="false" placeholder="${holder(d)}" value="${esc(d.draft)}" aria-label="CurseForge API Key">`
    + `<button type="button" class="sd-ib" data-sd="save" data-icn="${d.saving ? "spinner" : "save"}" aria-label="保存 API Key" aria-disabled="${saveOff(d)}" title="${saveOff(d) ? "请先粘贴 API Key" : "保存"}"><i data-ic="${d.saving ? "spinner" : "save"}"${d.saving ? ' class="sd-spin"' : ""}></i></button>`
    + `<button type="button" class="sd-ib" data-sd="clear" title="清除" aria-label="清除 CurseForge API Key"${d.set || d.broken ? "" : " hidden"}><i data-ic="trash"></i></button></label></div>`;
  const trustRow = t => `<div class="sd-tr" role="listitem" data-host="${esc(t.host)}"><b class="sd-th">${esc(t.host)}</b><span class="sd-td">${esc(t.at)} 确认</span>`
    + `<button type="button" class="sd-ib" data-sd="untrust" title="移除，下次遇到时重新询问" aria-label="移除 ${esc(t.host)}"><i data-ic="x"></i></button></div>`;
  const trustHTML = d => `<div class="sd-f sd-tf"><div class="sd-lb"><span title="${esc(TRUST_TIP)}">信任的下载站点</span><span class="sd-n"${d.trust.length ? "" : " hidden"}>${d.trust.length}</span></div>`
    + `<div class="sd-tw${d.trust.length ? "" : " none"}"><div class="list" role="list" aria-label="信任的下载站点">${d.trust.map(trustRow).join("")}</div><span class="thumb"></span>`
    + `<span class="sd-te">暂无信任的站点</span></div></div>`;
  // 缓存框里会变的三样：总占用、分项、那颗键
  const cacheView = d => {
    const n = total(d);
    // 清理中分项先收掉：「清理中…」比「清理」宽，分项留着会被挤出省略号，反正马上就没了
    return { total: fmtSize(n), parts: d.cleaning ? "" : PARTS.filter(p => d.cache[p.k]).map(p => `${p.t} ${fmtSize(d.cache[p.k])}`).join(" · "), off: d.cleaning || !n,
      tip: !d.cleaning && !n ? "缓存已清空" : "", btn: d.cleaning ? `${C.KIT.icon("spinner", "sd-spin")}<span>清理中…</span>` : `<span>清理</span>` };
  };
  const cacheHTML = d => {
    const v = cacheView(d);
    return `<div class="sd-f"><div class="sd-lb"><span title="${esc(CACHE_TIP)}">下载缓存</span></div><div class="sd-cb"><b class="sd-ct">${v.total}</b><span class="sd-cp">${esc(v.parts)}</span>`
      + `<button type="button" class="sd-btn" data-sd="clean" aria-disabled="${v.off}"${v.tip ? ` title="${v.tip}"` : ""}>${v.btn}</button></div></div>`;
  };
  function html(ctx) {
    C = ctx; const d = data();
    return `<div class="sd"><section class="sd-col" aria-label="下载与网络"><div class="sd-grid">`
      + lineHTML(d, "src") + lineHTML(d, "mod") + lineHTML(d, "upd") + keyHTML(d)
      + `<i class="sd-hr"></i>` + trustHTML(d) + cacheHTML(d)
      + `</div></section></div>`;
  }

  // ---- 分段：选中块滑过去（同向导第三步）；这一页有三组，一组一组放 ----
  function placeSeg(box, animate) {
    const hl = box.querySelector(".sd-sgl"), on = box.querySelector("button.on"); if (!hl || !on || !box.offsetWidth) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  const placeAll = r => r.querySelectorAll(".sd-seg").forEach(box => placeSeg(box, false));

  // ---- Key：只换动了的那几样（保存键、占位字、黄字、清除键） ----
  function paintKey() {
    const r = root(); if (!r) return; const d = data();
    const inp = r.querySelector(".sd-in"), iw = r.querySelector(".sd-iw"), sv = r.querySelector('[data-sd="save"]'), cl = r.querySelector('[data-sd="clear"]'), w = r.querySelector(".sd-warn");
    if (inp.placeholder !== holder(d)) inp.placeholder = holder(d);
    if (inp.value !== d.draft) inp.value = d.draft;
    const off = saveOff(d);
    setAttr(sv, "aria-disabled", off);
    setTip(sv, d.saving ? "" : off ? "请先粘贴 API Key" : "保存");
    const ic = d.saving ? "spinner" : "save";
    if (sv.dataset.icn !== ic) { sv.dataset.icn = ic; sv.innerHTML = C.KIT.icon(ic, d.saving ? "sd-spin" : ""); }
    cl.hidden = !(d.set || d.broken); w.hidden = !d.broken; iw.classList.toggle("warn", d.broken);
  }
  // 存：草图里装作等了一下请求；只填了空白就什么都不存（同程序），保存键本来也灰着
  function saveKey() {
    const d = data(); if (saveOff(d)) return;
    d.saving = true; paintKey();
    setTimeout(() => { d.saving = false; d.set = true; d.broken = false; d.draft = ""; paintKey(); }, 450);
  }
  async function clearKey() {
    const d = data();
    const k = await C.KIT.confirm({ ic: "trash", tone: "dgr", title: "清除 CurseForge API Key？", body: "清除后，导入 CurseForge 格式的整合包时将无法自动下载模组，需要时可重新填写。",
      btns: [{ k: "cancel", label: "取消" }, { k: "ok", label: "清除", tone: "dgr" }] });
    if (k !== "ok") return;
    d.set = false; d.broken = false; paintKey();
  }

  // ---- 信任的站点：点 × 马上移除，那一行往右划 32 像素淡掉（0.18 秒）再收起；别的行不动，只改个数和空的时候那句 ----
  function untrust(row) {
    const d = data(), r = root(); if (!r || row._gone) return; row._gone = true;
    d.trust = d.trust.filter(t => t.host !== row.dataset.host);
    const tw = r.querySelector(".sd-tw"), n = r.querySelector(".sd-n");
    n.textContent = d.trust.length; n.hidden = !d.trust.length; tw.classList.toggle("none", !d.trust.length);
    const h = row.offsetHeight + "px"; row.style.pointerEvents = "none";
    row.animate([{ opacity: 1, transform: "none", height: h, easing: EASE_IN }, { opacity: 0, transform: "translateX(32px)", height: h, offset: .56, easing: EASE_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px" }], { duration: 320, fill: "forwards" }).onfinish = () => { row.remove(); tw._paint?.(); };
  }

  // ---- 下载缓存：确认了才清；草图里装作等了半秒 ----
  function paintCache(anim) {
    const r = root(); if (!r) return; const v = cacheView(data());
    const tt = r.querySelector(".sd-ct"), pt = r.querySelector(".sd-cp"), bt = r.querySelector('[data-sd="clean"]');
    if (tt.textContent !== v.total) {
      tt.textContent = v.total;
      if (anim) tt.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
    }
    if (pt.textContent !== v.parts) {
      if (anim && !v.parts) pt.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = e => { pt.textContent = ""; e.target.cancel(); };
      else pt.textContent = v.parts;
    }
    setAttr(bt, "aria-disabled", v.off); setTip(bt, v.tip);
    if (bt._h !== v.btn) { bt._h = v.btn; bt.innerHTML = v.btn; }
  }
  async function cleanCache() {
    const d = data(), n = total(d); if (d.cleaning || !n) return;
    const k = await C.KIT.confirm({ ic: "trash", tone: "dgr", title: "清理下载缓存？", body: `将删除全部下载缓存，共 ${fmtSize(n)}。<br>已建好的服务器不受影响，之后用到时重新下载。`, // 分两行写，免得「影响」折成两半
      btns: [{ k: "cancel", label: "取消" }, { k: "ok", label: "清理", tone: "dgr" }] });
    if (k !== "ok" || d.cleaning) return;
    d.cleaning = true; paintCache(true);
    setTimeout(() => { d.cleaning = false; PARTS.forEach(p => { d.cache[p.k] = 0; }); paintCache(true); }, 500);
  }

  // ---- 事件（绑在这一页的根上） ----
  function onClick(e) {
    const r = root(), d = data(); if (!r) return;
    const s = e.target.closest(".sd-seg button");
    if (s) {
      const box = s.parentElement, g = box.dataset.g;
      if (s.dataset.v === d[g]) return;
      d[g] = s.dataset.v; // 同程序：点了马上存
      box.querySelectorAll("button").forEach(b => { const on = b === s; b.classList.toggle("on", on); b.setAttribute("aria-checked", on); });
      placeSeg(box, true); return;
    }
    const b = e.target.closest("[data-sd]"); if (!b) return;
    if (b.closest(".sd-iw")) e.preventDefault(); // 框里的按钮：别让 label 把焦点挪回框里再点一次
    if (b.getAttribute("aria-disabled") === "true") return;
    switch (b.dataset.sd) {
      case "save": saveKey(); break;
      case "clear": clearKey(); break;
      case "apply": break; // 草图里不打开外部网页
      case "untrust": untrust(b.closest(".sd-tr")); break;
      case "clean": cleanCache(); break;
    }
  }
  function onInput(e) { if (!e.target.classList.contains("sd-in")) return; data().draft = e.target.value; paintKey(); }
  function onKey(e) { if (e.target.classList.contains("sd-in") && e.key === "Enter" && !e.isComposing) { e.preventDefault(); saveKey(); } }

  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    placeAll(r);
    ctx.thinBar(r.querySelector(".sd-tw"));
    r.addEventListener("click", onClick);
    r.addEventListener("input", onInput);
    r.addEventListener("keydown", onKey);
    document.fonts?.ready.then(() => { if (r.isConnected) placeAll(r); });
  }
  addEventListener("resize", () => { const r = root(); if (r) { placeAll(r); r.querySelector(".sd-tw")._paint?.(); } });

  window.SETTINGS_SECS.download = { html, mount };
})();
