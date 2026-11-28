// 服务器详情页 →「设置 → 常用」「设置 → 启动与内存」（11-27 夜里第一稿，子代理照临时规范画的；计划书把这两页归在「作者要一页一页看」）。样式在 detail-settings.css。
// 「全部配置」是另一份（detail-settings-raw.js）。代理端的设置只有「启动与内存」，外壳不列子页，键还是 settings/jvm。
//
// ======== 常用（程序 app.js view === "common"，约 4430 行） ========
// 程序：一个搜索框（中文名 / 键名）、一排分组片（点了滚到那一组）、7 组表单（props-catalog.js 按版本挑出来的 server.properties 项，
// 每项：中文名、「重启生效」标签、说明、键名、控件——开关 / 下拉 / 数字框 / 文本框）；MOTD 有 § 色码调色板和预览；26.x 起四项变成游戏规则开关
// （开着时走 /gamerule 马上生效，停着时灰掉「服务器启动后可切换」）；吸底保存条「保存设置」，只提交改过的项，开着时写「服务器运行中：保存后要重启才生效；游戏规则开关马上生效」。
// 这里程序有的项一项不少（目录同「全部配置」一样现取 props-catalog.js，按这台服务器的版本挑；值也同「全部配置」：目录的默认值，
// 端口、最大人数同页头，Paper、Forge 是离线模式、白名单开着，原版 26.2 文件里没写白名单）。排法照临时规范重来（内容面板 868 × 526，里面 852 × 510）：
//   · 最上面一排 32 高：七个分组片（28 高胶囊，选中块滑过去），跟着下面滚到哪一组走；点了滚过去。右边搜索框（32 高胶囊，同控制台过滤框），
//     边打边筛（停手 0.18 秒），中文名、键名都认，空格、横线、下划线、点都不算（同程序 filterBySearch）；筛不到时栏正中一行「没有匹配的配置项」。
//   · 中间一栏（圆角 18）是全部的项，多了在栏里滚（细滚动条）。最上面「谁能进」两块（正版验证、白名单，同向导第四步那条：开关在字的左边；两块一样宽、按字的宽度，不再铺满一行，作者 11-28 嫌白名单那块太长），
//     任务单说这两项放在「常用」里；白名单那块右边一个箭头去「玩家」（名单在那里）。下面一组一组：组名和项数，组里四列的格子
//     （字 18 + 6 + 框 40，同向导第三步；行距 16）：步进和数字框占一格，分段、下拉、文本框占两格，MOTD 占一整行；开关放在每组最后，四个一行，开关在字的左边。
//     26.x 的四个游戏规则开关在「基础与玩法」最后单独一行，标「游戏规则」（悬停说马上生效、不用保存）；停着、启动中时灰掉。
//   · 控件（规范第九节）：程序的 <select> 换成分段（四项以内）或自己画的下拉（世界类型五项）；数字：范围小的用步进加档位（视距 3~32、最大人数、出生点保护……，
//     现有值不在档位上时照样显示，一动落到相邻的档），两项权限等级用 1~4 的分段，范围大、要能填 -1 的（端口、卡死超时、压缩阈值、世界边界、连锁更新上限、发包限速）
//     用向导端口那样能打字、不带箭头的框，单位写在框里右边；不是整数、端口越界时框标红、右边一句红字，点保存时拦下（滚过去、晃一下、光标落过去）。
//   · MOTD：左边是两行的输入框（游戏的服务器列表最多显示两行；回车换行，第三行进不去；存进 server.properties 时换行要写成 \n，改程序时注意），
//     框里右上角的调色板按钮点开 16 色和 5 种样式：没选中字时把 § 码插在光标处（同程序），选中一段时只改这一段（作者 11-28 要的，做法见调色板那段）；
//     右边是两行的预览（恒深底，同程序 motd-preview）。
//   · 说明、键名、默认值都进中文名的悬停；程序每一项都挂着「重启生效」（目录里每一项都是 restart: true），这里不再每项挂，改成保存条上说一次（同「全部配置」）。
// 保存条（52 高、圆角 18，面板里最下面一条，同「全部配置」「定时任务」）：有改动时右边淡进来「N 项修改未保存」「放弃修改」，「保存」亮起来；
// 服务器开着或启动中、又有改动时左边「保存后要重启才生效」，开着时存过以后「已保存，重启服务器以后生效」加「立即重启」（同程序 wireLiveBar）。
// 游戏规则开关不进保存条。改过还没存的项，中文名后面一个强调色小点（同「全部配置」）。草图专用：&raw=none 看配置文件还没生成（程序是页顶一句警告，这里放保存条左边）。
//
// ======== 启动与内存（程序 app.js view === "jvm"，约 4641 行） ========
// 程序：页顶一块（核心和构建、版本、端口（点了去「常用」）、Java、服务器文件夹带复制和打开）；最大内存（滑条、MB 数字框、经验条、
// 「其他程序已用 · 本服分配 / 共 · 1 格 = 」一行）；自定义 JVM 参数（文本框）、「套用 Aikar's Flags（G1GC 优化）」「清空」；实际启动参数预览（后端算的）；
// Java 下拉（MineDeck 下载的、本机装的、本机没有可以现下一份的；现在用的那项找不到时单列、标「找不到」）；run.bat 那句；吸底保存条「保存」。
// 这里（内容面板里左右两栏，左 476 宽，下面同「常用」的保存条）：
//   · 左栏照向导第三步「服务器配置」那栏：内存分配（标题右边图例；格子也是滑条，0.5 GB 一格地走；右边步进 132 宽；下面快捷预设）——控件和代码照向导搬过来；
//     下面两行和步进对齐：「Java ｜ 构建」「服务器文件夹 ｜ 端口」（同向导「存储路径 ｜ 端口配置」，右边一列都是 132 宽）。
//     核心和版本页头写着，不再写；端口在「常用」里改，这里一个只读的框，点了去「常用」、光标落在端口上（同程序）；代理端的端口在它自己的配置文件里，不能点。
//     服务器文件夹一个路径框，后面复制图标（打开文件夹在页头「···」里）；run.bat 那句进悬停。
//   · 右栏是自定义 JVM 参数：一行一个（程序是一个文本框，「每行一个，或用空格分隔」），点一行就地改，悬停出 ×，最下面一个框加（空格隔开的一次加几个）；
//     右上角「Aikar's Flags」（28 高胶囊，同「定时任务」的预设：已经是这一套时 ＋ 换成勾）、实际启动参数（点开一块浮层看后端会用的整行，
//     平时不摆在页上，免得 -Xmx 和左边的步进各写一遍）、清空。参数里有双引号、或者写了 -Xmx 这类内存参数时框标红（请在左侧设置内存）。
//   · 内存（计划书挂着的问题）：上限和向导一样是「当前可用的内存按 0.5 GB 往下取整」，但「系统已占」不算这台服务器自己（开着时它自己占的那份不能再算一遍），
//     所以开着、停着画出来一样。以前建的服务器内存不是 0.5 GB 的整数倍（比如 3000 MB）时：照原样显示（步进里写「3000 MB」，格子按比例涂，把手停在准确的位置），
//     打开页面不算修改；一动才落到 0.5 GB 的档上——减号落到下面最近的一档、加号落到上面最近的一档，拖和点按落点四舍五入，预设就是预设的数；「放弃修改」回到原来的 3000 MB。
//     现有分配超过上限时也照原样显示，步进的数变黄、预设那一排右边一句「超过可用内存」（整句在悬停，程序原话），加号灰掉，减号一下落到上限。
//   · 「指定构建」（计划书挂着的问题；程序里没有，前后端都没有换构建的接口）：画了一个最小的入口——左栏「构建」下拉，只列同一个游戏版本的构建
//     （最新的标「最新」），选了算一项修改、保存后下次启动用。不放换游戏版本（那是升级世界，回不去，不该是设置里的一个下拉）。构建号是编的。
// 草图专用：&mem=3000 看不是 0.5 GB 整数倍的内存，&mem=20480 看超过上限；&ram=8|16|32|48|64|128 换本机内存（同向导右上角，默认 32）；
// &jvm=aikar 让这台服务器一开始就带着 Aikar's Flags（默认都没有自定义参数，和控制台第一行的启动命令对得上）。&notice=2 以上时现在用的 Java 找不到（同页头提示）。
//
// ======== 作者 11-28 审完以后加的两样（两页都有） ========
//   · 离开前拦未保存：两页各登记 dirty / save / discard（文件末尾，leaveHooks 包出来的），外壳在换标签、换子页、刷新、跳页前问「有未保存的修改」。
//     选「保存」时有填错的框就留在这页：「常用」是整数框、端口（cmCheck），「启动与内存」是正改着、标着红的那一行参数（jvCheck）。
//     「启动与内存」的端口点了要换去「常用」，所以它自己先问这一句，过了才记下「光标落在端口上」，被取消就什么都不留。
//   · 步进中间的数点进去就地打字（KIT.numEdit，本文件的 numEdit 包了一层）：
//     「常用」的步进框里是不带单位的整数（单位写在悬停和填错的提示里——空服暂停平时写「5 分钟」，存的是秒），范围是档位的两头，不在档位上的照收，
//     之后点加减往那个方向落到最近一档；更大的数（比如最大人数上千）去「全部配置」填。
//     内存按 GB 认，也认带「GB」「MB」的写法；小于 0.5 GB、超过上限算填错，别的数同拖动一样四舍五入到 0.5 GB 的档上。
//
// 外壳的接口见 detail.html 的 content()：html(ctx) 返回内容面板里的那一块，mount(ctx) 换上去、图标换好以后跑；数据留在这个文件里，换标签回来不丢，没存的改动也在。
(() => {
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const setTip = (el, text) => { // 悬停字没变就不动（kit.js 悬停时会把 title 挪进 data-tip，两处都看）
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  const setText = (el, t) => { if (el.textContent !== t) el.textContent = t; };
  const setAttr = (el, k, v) => { if (el.getAttribute(k) !== String(v)) el.setAttribute(k, v); };
  const setHTML = (el, html) => { if (el._h !== html) { el._h = html; el.innerHTML = html; return true; } return false; }; // 一样就不换
  const shake = el => el.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });
  const rise = (el, i = 0) => el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" });
  const skey = s => String(s || "").toLowerCase().replace(/[\s\-_.]+/g, ""); // 同程序 filterBySearch
  const range = (a, b) => Array.from({ length: b - a + 1 }, (_, i) => a + i);
  const tpl = html => { const t = document.createElement("template"); t.innerHTML = html; return t.content.firstElementChild; };

  let C = null; // 外壳最近一次给的 ctx
  const running = () => C.st !== "stopped"; // 开着或启动中：保存后要重启才生效（同程序 info.status !== "stopped"）
  const live = () => C.st === "running";    // 开着：游戏规则开关能用、存过以后能「立即重启」

  // ---- 程序的中文目录：同「全部配置」，读 props-catalog.js 原文（它写的是 window.PROPS_CATALOG = [...]，给它一个自己的 window，不往全局放） ----
  let CAT = null;
  const CAT_READY = fetch("../../internal/web/ui/props-catalog.js").then(r => r.text())
    .then(src => { const w = {}; new Function("window", src)(w); CAT = w.PROPS_CATALOG || []; })
    .catch(e => { console.warn("没取到 props-catalog.js", e); CAT = []; });
  const verParts = v => String(v).split(".").map(s => parseInt(s, 10) || 0);
  const verCmp = (a, b) => { const x = verParts(a), y = verParts(b); for (let i = 0; i < Math.max(x.length, y.length); i++) { const d = (x[i] || 0) - (y[i] || 0); if (d) return d < 0 ? -1 : 1; } return 0; };
  const verGte = (a, b) => verCmp(a, b) >= 0, verLt = (a, b) => verCmp(a, b) < 0;

  // =====================================================================================================
  // 保存条：两页一样（同「全部配置」「定时任务」）。pg 是这一页的几样：root、数据、改了几项、存、放弃
  // =====================================================================================================
  function barHTML(n) {
    const on = n ? " on" : "";
    return `<div class="st-bar"><span class="st-note" data-mode=""></span><span class="st-sp"></span><span class="st-n${on}" aria-live="polite"><i></i><span>${n} 项修改未保存</span></span>`
      + `<button type="button" class="st-discard${on}" data-st="discard"${n ? "" : ' tabindex="-1"'}>放弃修改</button>`
      + `<button type="button" class="st-save${on}" data-st="save"${n ? "" : ' aria-disabled="true" title="暂无未保存的修改"'}>保存</button></div>`;
  }
  // 只换数字、两样淡进淡出、「保存」亮不亮；保存条本身的大小不变
  function paintBar(pg) {
    const r = pg.root(); if (!r) return; const D = pg.D();
    const n = pg.count(D), on = n > 0, t = r.querySelector(".st-n"), x = r.querySelector(".st-discard"), s = r.querySelector(".st-save");
    if (on) setText(t.lastElementChild, `${n} 项修改未保存`); // 变回 0 时旧的数跟着淡掉，不先换成 0
    t.classList.toggle("on", on); x.classList.toggle("on", on); if (x.tabIndex !== (on ? 0 : -1)) x.tabIndex = on ? 0 : -1;
    if (!D.saving) {
      s.classList.toggle("on", on);
      if (on) { s.removeAttribute("aria-disabled"); setTip(s, ""); } else { s.setAttribute("aria-disabled", "true"); setTip(s, "暂无未保存的修改"); }
    }
    paintNote(pg);
  }
  // 左边那句：开着、有改动时「保存后要重启才生效」；存过、服务器还开着时「已保存，重启服务器以后生效」加「立即重启」（同程序 wireLiveBar）
  function noteMode(pg, D) {
    if (D.pending && live()) return "pending";
    if (running() && pg.count(D)) return "live";
    return pg.idleNote ? pg.idleNote(D) : "";
  }
  function noteHTML(D, mode) {
    if (mode === "live") return `<i data-ic="warn"></i><span>保存后需重启生效</span>`;
    if (mode === "nofile") return `<i data-ic="warn"></i><span title="服务器尚未首次启动，部分配置项将在首次启动后由服务端生成，当前已修改的配置会保留。">配置文件尚未生成</span>`;
    if (mode === "pending") return `<i data-ic="warn"></i><span>已保存，重启服务器后生效</span>`
      + `<button type="button" class="st-btn" data-st="restart"${D.restarting ? " disabled" : ""}>${D.restarting ? `<i data-ic="spinner" class="st-spin"></i>正在重启` : `<i data-ic="refresh"></i>立即重启`}</button>`;
    return "";
  }
  // 换的时候逐项换：旧的往上 5 像素淡掉（0.09 秒），新的从下面 5 像素一样一样淡进来（0.22 秒、错开 22 毫秒）；连着变时上一次作废
  function paintNote(pg, instant) {
    const el = pg.root()?.querySelector(".st-note"); if (!el) return; const D = pg.D();
    const mode = noteMode(pg, D); if (el.dataset.mode === mode) return;
    el.dataset.mode = mode;
    const tok = el._tok = (el._tok || 0) + 1;
    const show = () => {
      if (el._tok !== tok) return;
      el.innerHTML = noteHTML(D, mode); C.KIT.hydrate(el);
      if (!instant) [...el.children].forEach(rise);
    };
    if (instant || !el.children.length) { show(); return; }
    el.getAnimations({ subtree: true }).forEach(a => a.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN }).onfinish = show;
  }
  // 返回 Promise<存成了没有>（被拦下是 false）：外壳离开前选「保存」时要等它（kit.js 第 6 条）。D.saving 平时是 false，正在存时是那个 Promise，再调给同一个
  function save(pg) {
    const D = pg.D(); if (D.saving) return D.saving;
    if (!pg.count(D)) return Promise.resolve(true);
    if (pg.check && !pg.check(D)) return Promise.resolve(false); // 有不对的：拦下，框已经标红、晃过了
    const s = pg.root().querySelector(".st-save"); s.innerHTML = `${C.KIT.icon("spinner", "st-spin")}保存`;
    return D.saving = new Promise(done => setTimeout(() => { // 草图里装作等了一下请求
      D.saving = false; pg.commit(D);
      if (C.st === "running") D.pending = true; // 运行中不会重读：改了哪一项都要重启（同程序：目录里每一项都是 restart: true；启动与内存也是）
      const r = pg.root(); if (r) r.querySelector(".st-save").textContent = "保存";
      paintBar(pg); done(true);
    }, 450));
  }
  // 外壳离开这一页之前用的三样（kit.js 第 6 条），两页一样：项数就是保存条上的那个数；数据还没到（还没画过、目录没取回来）算干净
  const leaveHooks = pg => ({
    dirty: () => { const D = C && pg.D(); return D ? pg.count(D) : 0; },
    save: () => save(pg),
    discard: () => { const D = pg.D(); if (D) pg.discard(D); },
  });
  // 步进中间的数点进去就地打字（kit.js 第 7 条；作者 11-28 定凡是步进都这样）。两页的步进都走这里：o 同 KIT.numEdit 的，text、parse 必给。
  // 范围各自在 parse 里查（越界返回 NaN），不交给 kit 的 min / max：现有的值可以在范围外（超过上限的内存、别处填的数），
  // 点进去没改就回车不该算填错，所以框里的字没动时原样返回现在的数
  const numEdit = (el, o) => C.KIT.numEdit(el, { ...o, parse: raw => (raw === o.text(o.get()) ? o.get() : o.parse(raw)) });
  function restart(pg) { // 草图里装作重启了一下：好了以后那句淡掉
    const D = pg.D(); if (D.restarting) return;
    D.restarting = true; const el = pg.root()?.querySelector(".st-note"); if (el) { el.innerHTML = noteHTML(D, "pending"); C.KIT.hydrate(el); }
    setTimeout(() => { D.restarting = false; D.pending = false; paintNote(pg); }, 1500);
  }

  // =====================================================================================================
  // 菜单（下拉）和浮层：同共用件页那张菜单（外圆角 16、细边加内边距 5，项 36 高、圆角 10）；挂在 body 上，点外面、Esc、栏里一滚就收
  // =====================================================================================================
  let menu = null, pop = null;
  function openMenu(anchor, inner, pick, act) { // act：菜单项里另有按钮（data-act）时，点按钮走它，不算选这一项
    if (menu && menu._a === anchor) { closeMenu(); return; }
    closeMenu(); closePop();
    const m = document.createElement("div"); m.className = "st-dm enter"; m.setAttribute("role", "menu"); m.innerHTML = inner;
    document.body.appendChild(m); C.KIT.hydrate(m);
    const r = anchor.getBoundingClientRect();
    m.style.minWidth = r.width + "px"; // 和框一样宽（内容长的可以更宽），左边对齐
    const w = m.offsetWidth, h = m.offsetHeight, up = r.bottom + 4 + h > innerHeight - 8;
    m.classList.toggle("up", up);
    m.style.left = Math.round(Math.max(8, Math.min(r.left, innerWidth - w - 8))) + "px";
    m.style.top = Math.round(up ? r.top - h - 4 : r.bottom + 4) + "px";
    anchor.classList.add("open"); anchor.setAttribute("aria-expanded", "true"); m._a = anchor;
    m.addEventListener("click", e => {
      const it = e.target.closest(".st-it"), ab = e.target.closest("[data-act]");
      if (it && ab && act) { closeMenu(); act(ab.dataset.act, it.dataset.v); return; }
      if (!it || it.getAttribute("aria-disabled") === "true") return;
      closeMenu(); if (!it.classList.contains("here")) pick(it.dataset.v);
    });
    menu = m;
  }
  function closeMenu() {
    if (!menu) return; const m = menu; menu = null;
    m._a.classList.remove("open"); m._a.setAttribute("aria-expanded", "false"); m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90);
  }
  // 浮层（MOTD 的调色板、实际启动参数）：在按钮下面、右边对齐；放不下就到上面。
  // 调色板的按钮在两行简介框的右上角，从按钮下面弹会盖住第二行（正好是选中要改的字），所以它对着整个框的下沿弹
  const popRef = a => a.closest(".st-motd .st-iw") || a;
  function openPop(anchor, inner, cls, wire) {
    if (pop && pop._a === anchor) { closePop(); return; }
    closePop(); closeMenu();
    const p = document.createElement("div"); p.className = `st-pop ${cls} enter`; p.setAttribute("role", "dialog"); p.innerHTML = inner;
    document.body.appendChild(p); C.KIT.hydrate(p);
    pop = p; p._a = anchor; anchor.classList.add("open");
    wire?.(p); placePop();
  }
  function placePop() {
    if (!pop) return;
    const r = popRef(pop._a).getBoundingClientRect(), w = pop.offsetWidth, h = pop.offsetHeight, up = r.bottom + 6 + h > innerHeight - 8;
    pop.classList.toggle("up", up);
    pop.style.left = Math.round(Math.max(8, Math.min(r.right - w, innerWidth - w - 8))) + "px";
    pop.style.top = Math.round(up ? r.top - h - 6 : r.bottom + 6) + "px";
  }
  function closePop() {
    if (!pop) return; const p = pop; pop = null;
    p._a.classList.remove("open"); p.classList.remove("enter"); p.classList.add("leave"); setTimeout(() => p.remove(), 90);
  }
  // 文档级的监听只在这里绑一次（模块加载时）；换标签时点的是左边的标签栏，在菜单外面，跟着收掉
  document.addEventListener("click", e => {
    if (menu && !e.composedPath().some(n => n === menu || n === menu._a)) closeMenu();
    if (pop && !e.composedPath().some(n => n === pop || n === pop._a)) closePop();
  });
  document.addEventListener("keydown", e => { if (e.key === "Escape") { closeMenu(); closePop(); } });
  addEventListener("resize", () => {
    closeMenu(); closePop();
    const r = cmRoot(); if (r) { r.querySelectorAll(".st-seg").forEach(s => placeSeg(s, false)); placeChip(false); }
    if (jvRoot()) paintMem(jvData());
  });

  // 分段选：同向导第三步（40 高、圆角 10，选中块离外框 4、圆角 6，滑过去）
  const segHTML = (opts, on, label) => `<div class="st-seg" role="radiogroup" aria-label="${esc(label)}"><i class="st-sgl"></i>${opts.map(([v, l]) =>
    `<button type="button" role="radio" data-v="${esc(v)}" aria-checked="${v === on}" class="${v === on ? "on" : ""}">${esc(l)}</button>`).join("")}</div>`;
  function placeSeg(box, animate) {
    const hl = box.querySelector(".st-sgl"), on = box.querySelector("button.on");
    if (!hl) return;
    hl.style.opacity = on ? "" : "0"; if (!on || !box.offsetWidth) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function pickSeg(box, v, animate) {
    box.querySelectorAll("button").forEach(b => { const on = b.dataset.v === v; b.classList.toggle("on", on); b.setAttribute("aria-checked", on); });
    placeSeg(box, animate);
  }

  // =====================================================================================================
  // 常用
  // =====================================================================================================
  const GROUPS = ["基础与玩法", "世界生成", "玩家与权限", "性能", "网络与远程管理", "资源包", "安全与杂项"]; // 同程序
  const WHO = ["online-mode", "white-list"]; // 「谁能进」：放在栏的最上面，不在「玩家与权限」里重复
  const WHO_NAME = { "online-mode": "正版验证", "white-list": "白名单" }; // 同向导第四步（作者定过的字）
  // 和别的标签对得上的几项（同「全部配置」的 OVERRIDE；null = 文件里没有这一项）
  const OVERRIDE = { paper: { "online-mode": "false", "white-list": "true" }, forge: { "online-mode": "false", "white-list": "true" }, vanilla: { "white-list": null } };
  const secs = v => (v < 60 ? `${v} 秒` : v % 3600 === 0 ? `${v / 3600} 小时` : v % 60 === 0 ? `${v / 60} 分钟` : `${v} 秒`);
  // 步进的档位（范围小、一下一档的）。档位是草图定的，现有值不在档位上时照样显示，一动落到相邻的档；范围照服务端（视距、模拟距离 3~32）。
  // unit 是存进文件的那个数的单位（就地打字时填的是它，见 wireSteps；平时显示的字由 fmt 定，可以换算成别的单位）
  const STEP = {
    "player-idle-timeout": { lad: [0, 5, 10, 15, 20, 30, 45, 60, 90, 120], unit: "分钟", fmt: v => (v <= 0 ? "不踢出" : `${v} 分钟`) },
    "spawn-protection": { lad: [0, 1, 2, 4, 8, 16, 24, 32, 48, 64], unit: "格", fmt: v => (v <= 0 ? "不保护" : `${v} 格`) },
    "max-players": { lad: [1, 2, 3, 4, 5, 6, 8, 10, 12, 15, 16, 20, 24, 30, 32, 40, 50, 64, 80, 100, 128, 200, 500, 1000], unit: "人", fmt: v => `${v} 人` },
    "view-distance": { lad: range(3, 32), unit: "区块", fmt: v => `${v} 区块` },
    "simulation-distance": { lad: range(3, 32), unit: "区块", fmt: v => `${v} 区块` },
    "entity-broadcast-range-percentage": { lad: [10, 25, 50, 75, 100, 125, 150, 200, 300, 500], unit: "%", fmt: v => `${v}%` },
    "pause-when-empty-seconds": { lad: [0, 10, 30, 60, 120, 300, 600, 1800, 3600], unit: "秒", fmt: v => (v <= 0 ? "不暂停" : secs(v)) },
    "status-heartbeat-interval": { lad: [0, 5, 10, 15, 30, 60, 120, 300, 600], unit: "秒", fmt: v => (v <= 0 ? "不推送" : secs(v)) },
    "chat-spam-threshold-seconds": { lad: [1, 2, 3, 5, 10, 15, 20, 30, 60], unit: "秒", fmt: v => `${v} 秒` },
    "command-spam-threshold-seconds": { lad: [1, 2, 3, 5, 10, 15, 20, 30, 60], unit: "秒", fmt: v => `${v} 秒` },
  };
  const LEVELS = [["1", "1"], ["2", "2"], ["3", "3"], ["4", "4"]];
  const SEGS = { "op-permission-level": LEVELS, "function-permission-level": LEVELS, "text-filtering-version": [["0", "0"], ["1", "1"]] };
  // 能打字的整数框（范围大、要能填 -1 的；端口照向导的「1～65535」）
  const NUM = { "server-port": { port: 1 }, "query.port": { port: 1 }, "rcon.port": { port: 1 }, "management-server-port": { port: 0 },
    "max-world-size": { unit: "格" }, "max-tick-time": { unit: "毫秒" }, "network-compression-threshold": { unit: "字节" }, "max-build-height": { unit: "格" }, "rate-limit": { unit: "包/秒" } };
  const SANS = new Set(["motd", "resource-pack-prompt"]); // 文本框里除了这两项都是等宽字（地址、路径、键、种子……）
  function specOf(p) {
    if (p.mode === "gamerule") return { ctl: "gr" };
    if (p.mode === "bool") return { ctl: "bool" };
    if (p.key === "motd") return { ctl: "motd", span: 4 };
    if (SEGS[p.key]) return { ctl: "seg", span: 1, opts: SEGS[p.key] };
    if (p.mode === "sel") { const o = p.opts.map((v, i) => [v, p.optNames ? p.optNames[i] : v]); return { ctl: o.length <= 4 ? "seg" : "sel", span: 2, opts: o }; }
    if (p.mode === "int") return STEP[p.key] ? { ctl: "step", span: 1, ...STEP[p.key] } : { ctl: "num", span: 1, ...(NUM[p.key] || {}) };
    return { ctl: "text", span: 2, mono: !SANS.has(p.key) };
  }

  const CM = {};
  let cmFocus = ""; // 从「启动与内存」的端口点过来：滚到这一项、点亮、光标放进去（同程序 focusPropKey）
  function cmData() {
    const k = C.kind;
    if (CM[k] || !CAT) return CM[k];
    const mc = C.S.mc, items = [];
    for (const p of CAT) { // 同程序：按版本挑出适用的项，游戏规则迁移项在新版本上变成 /gamerule 开关
      if (p.since && verLt(mc, p.since)) continue;
      let mode;
      if (p.type === "gamerule-bool") mode = p.removed && verGte(mc, p.removed) ? "gamerule" : "bool";
      else { if (p.removed && verGte(mc, p.removed)) continue; mode = p.type; }
      if (p.key === "level-type" && verLt(mc, "1.16")) mode = "str";
      items.push({ ...p, mode });
    }
    items.forEach(p => { p.spec = specOf(p); });
    const def = p => (p.defSince && verGte(mc, p.defSince.v) ? p.defSince.def : p.def);
    const vals = {}, gr = {}, missing = new Set();
    items.forEach(p => { if (p.mode === "gamerule") gr[p.key] = def(p); else vals[p.key] = def(p); });
    Object.assign(vals, { "server-port": String(C.S.port), "max-players": String(C.S.max) });
    Object.entries(OVERRIDE[k] || {}).forEach(([key, v]) => { if (v === null) missing.add(key); else vals[key] = v; });
    if (C.QS.get("raw") === "none") items.forEach(p => missing.add(p.key));
    CM[k] = { items, base: { ...vals }, cur: { ...vals }, gr, missing, q: "", act: 0, pending: false, saving: false, restarting: false };
    return CM[k];
  }
  const itemOf = (D, k) => D.items.find(p => p.key === k);
  const cmDirty = D => Object.keys(D.cur).filter(k => D.cur[k] !== D.base[k]);
  const cmRoot = () => document.querySelector(".cp > .st-cm:not(.fade-out)");
  const cmList = () => cmRoot()?.querySelector(".st-lw .list");
  const fieldEl = k => cmRoot()?.querySelector(`[data-k="${CSS.escape(k)}"]`);
  const PG_CM = {
    root: cmRoot, D: () => cmData(), count: D => cmDirty(D).length,
    idleNote: () => (C.QS.get("raw") === "none" ? "nofile" : ""),
    check: D => cmCheck(D), discard: D => cmDiscard(D),
    commit: D => { D.base = { ...D.cur }; cmRoot()?.querySelectorAll(".mod").forEach(el => el.classList.remove("mod")); },
  };

  // 悬停：说明、键名（程序在每一项下面写着）；没写进文件的照程序说一句
  function tipOf(D, p) {
    const out = [p.desc + (p.key === "motd" ? " 支持中文与 § 色码，最多两行。" : "")];
    if (p.key === "white-list") out.push(`MineDeck 新建服务器时会按向导设置写入此项（默认关闭${verGte(C.S.mc, "26.3") ? "；该版本服务端自行生成配置时默认开启" : ""}）。白名单开关与名单亦可在「玩家」页管理。`);
    if (p.mode === "gamerule") out.push(live() ? "游戏规则：服务器运行时通过 /gamerule 即时生效，无需保存" : "游戏规则：服务器运行后可切换");
    if (D.missing.has(p.key) && p.mode !== "gamerule") out.push("server.properties 尚未写入此项，当前显示服务端默认值");
    out.push(p.mode === "gamerule" ? `/gamerule ${grRule(p)}` : p.key);
    return out.join("\n");
  }
  const grRule = p => (p.gameruleSince && verGte(C.S.mc, p.gameruleSince.v) ? p.gameruleSince.rule : p.gamerule);
  const optLabel = (p, v) => (p.spec.opts.find(o => o[0] === v) || [v, v || "未设置"])[1];
  const num = v => (/^-?\d+$/.test(String(v).trim()) ? +v : NaN);
  const fmtStep = (s, v) => (Number.isNaN(num(v)) ? (String(v).trim() || "未设置") : s.fmt(num(v)));
  const stepTo = (s, v, d) => { const n = num(v); if (Number.isNaN(n)) return undefined; return d > 0 ? s.lad.find(x => x > n) : [...s.lad].reverse().find(x => x < n); };
  function badOf(p, v) { // 能打字的整数框：不是整数、端口越界时标红；空着不算（同程序：没设置且为空的跳过）
    if (p.spec.ctl !== "num" || !String(v).trim()) return "";
    if (Number.isNaN(num(v))) return ["需填整数", "请输入整数，可为负数（如 -1）"];
    if (p.spec.port !== undefined && (num(v) < p.spec.port || num(v) > 65535)) return [`${p.spec.port}～65535`, `端口需为 ${p.spec.port}～65535 的整数`];
    return "";
  }

  // MOTD：§ 色码（同程序 MC_COLORS、renderMotd；预览恒深底，所以用游戏原色，不压暗）
  const MCC = { 0: "#000000", 1: "#0000AA", 2: "#00AA00", 3: "#00AAAA", 4: "#AA0000", 5: "#AA00AA", 6: "#FFAA00", 7: "#AAAAAA", 8: "#555555", 9: "#5555FF",
    a: "#55FF55", b: "#55FFFF", c: "#FF5555", d: "#FF55FF", e: "#FFFF55", f: "#FFFFFF" };
  const MCN = { 0: "黑色", 1: "深蓝", 2: "深绿", 3: "湖蓝", 4: "深红", 5: "紫色", 6: "金色", 7: "灰色", 8: "深灰", 9: "蓝色", a: "绿色", b: "天蓝", c: "红色", d: "粉红", e: "黄色", f: "白色" };
  const MSTY = [["l", "粗体"], ["o", "斜体"], ["n", "下划线"], ["m", "删除线"], ["r", "重置"]]; // 同程序的五个
  function motdHTML(text) {
    const base = () => ({ c: "#AAAAAA", b: 0, i: 0, u: 0, s: 0 });
    let st = base(), out = "", buf = "";
    const flush = () => { if (!buf) return; out += `<span style="color:${st.c}${st.b ? ";font-weight:600" : ""}${st.i ? ";font-style:italic" : ""}${st.u || st.s ? `;text-decoration:${st.u ? "underline " : ""}${st.s ? "line-through" : ""}` : ""}">${esc(buf)}</span>`; buf = ""; };
    for (let i = 0; i < text.length; i++) {
      if (text[i] === "§" && i + 1 < text.length) {
        const c = text[++i].toLowerCase(); flush();
        if (MCC[c]) st = { ...base(), c: MCC[c] }; else if (c === "l") st.b = 1; else if (c === "o") st.i = 1; else if (c === "n") st.u = 1; else if (c === "m") st.s = 1; else if (c === "r") st = base();
        continue;
      }
      buf += text[i];
    }
    flush();
    return out || `<span class="st-mpe">（预览）</span>`;
  }

  // ---- 画 ----
  function boolHTML(D, p, inWho) {
    const gr = p.mode === "gamerule", on = (gr ? D.gr[p.key] : D.cur[p.key]) === "true", off = gr && !live();
    const name = inWho ? WHO_NAME[p.key] : p.label, mod = !gr && D.cur[p.key] !== D.base[p.key];
    return `<button type="button" class="st-b${on ? " on" : ""}${mod ? " mod" : ""}" ${gr ? "data-gr" : "data-k"}="${esc(p.key)}" role="switch" aria-checked="${on}"`
      + (inWho ? "" : ` data-search="${esc(p.label + " " + p.key)}"`) + `${off ? ' aria-disabled="true"' : ""} title="${esc(tipOf(D, p))}">`
      + `<span class="st-sw"></span><span class="st-bl">${esc(name)}</span><i class="st-dot"></i></button>`;
  }
  function ctlHTML(D, p) {
    const s = p.spec, v = D.cur[p.key] ?? "", al = `aria-label="${esc(p.label)}"`;
    if (s.ctl === "motd") return `<div class="st-motd"><label class="st-iw"><textarea class="st-in" rows="2" wrap="off" placeholder="服务器简介" autocomplete="off" spellcheck="false" ${al}>${esc(v)}</textarea>`
      + `<button type="button" class="st-ib" data-st="pal" title="颜色与样式：选中文字时仅应用于所选内容，未选中时插入光标处"><i data-ic="palette"></i></button></label><div class="st-mdp" aria-hidden="true">${motdHTML(v)}</div></div>`;
    if (s.ctl === "seg") return segHTML(s.opts, v, p.label);
    if (s.ctl === "sel") return `<button type="button" class="st-sel" data-st="sel" aria-haspopup="menu" aria-expanded="false" ${al}><span class="st-selv">${esc(optLabel(p, v))}</span><i data-ic="chevronDown" class="st-chev"></i></button>`;
    if (s.ctl === "step") {
      const dn = stepTo(s, v, -1) !== undefined, up = stepTo(s, v, 1) !== undefined;
      return `<div class="st-stp"><button type="button" data-st="dec" aria-label="${esc(p.label)} 减小" aria-disabled="${!dn}" title="${dn ? "减小" : `已达最小值（${esc(s.fmt(s.lad[0]))}）`}"><i data-ic="minus"></i></button>`
        + `<b class="st-sv">${esc(fmtStep(s, v))}</b><button type="button" data-st="inc" aria-label="${esc(p.label)} 增大" aria-disabled="${!up}" title="${up ? "增大" : `已达最大值（${esc(s.fmt(s.lad[s.lad.length - 1]))}）`}"><i data-ic="plus"></i></button></div>`;
    }
    const bad = badOf(p, v);
    return `<label class="st-iw${s.ctl === "num" || s.mono ? " mono" : ""}${bad ? " bad" : ""}"><input class="st-in" value="${esc(v)}" placeholder="留空" autocomplete="off" spellcheck="false"${s.ctl === "num" ? ' inputmode="numeric"' : ""} ${al}>`
      + (s.unit ? `<span class="st-unit">${s.unit}</span>` : "") + `<span class="st-msg"${bad ? ` title="${esc(bad[1])}"` : ""}>${bad ? esc(bad[0]) : ""}</span></label>`;
  }
  const fieldHTML = (D, p) => `<div class="st-f${D.cur[p.key] !== D.base[p.key] ? " mod" : ""}" data-k="${esc(p.key)}" data-search="${esc(p.label + " " + p.key)}" style="grid-column: span ${p.spec.span}">`
    + `<div class="st-lb"><span class="st-lt" title="${esc(tipOf(D, p))}">${esc(p.label)}</span><i class="st-dot"></i></div>${ctlHTML(D, p)}</div>`;
  function whoHTML(D) {
    const it = WHO.map(k => itemOf(D, k)).filter(Boolean); if (!it.length) return "";
    return `<div class="st-who">${it.map(p => `<div class="st-wb" data-search="${esc(p.label + " " + WHO_NAME[p.key] + " " + p.key)}">${boolHTML(D, p, true)}`
      + (p.key === "white-list" ? `<button type="button" class="st-ib st-go" data-st="players" title="前往「玩家」管理白名单"><i data-ic="arrowRight"></i></button>` : "") + `</div>`).join("")}</div>`;
  }
  function groupHTML(D, g) {
    const f = g.items.filter(p => p.mode !== "bool" && p.mode !== "gamerule"), b = g.items.filter(p => p.mode === "bool"), r = g.items.filter(p => p.mode === "gamerule");
    return `<section class="st-g" data-g="${g.i}" aria-label="${g.name}"><div class="st-gh"><span>${g.name}</span><span class="st-gn">${g.items.length}</span></div>`
      + (f.length ? `<div class="st-grid">${f.map(p => fieldHTML(D, p)).join("")}</div>` : "")
      + (b.length ? `<div class="st-bgrid">${b.map(p => boolHTML(D, p)).join("")}</div>` : "")
      + (r.length ? `<div class="st-grh" title="服务器运行时通过 /gamerule 即时生效，无需保存；服务器未运行时不可切换"><i data-ic="bolt"></i>游戏规则</div><div class="st-bgrid st-grs">${r.map(p => boolHTML(D, p)).join("")}</div>` : "")
      + `</section>`;
  }
  const groupsOf = D => GROUPS.map((name, i) => ({ name, i, items: D.items.filter(p => p.group === name && !WHO.includes(p.key)) })).filter(g => g.items.length);
  function cmHTML(ctx) {
    C = ctx; const D = cmData();
    if (!D) return `<div class="st-cm"><div class="st-wait">正在读取…</div></div>`;
    const G = groupsOf(D);
    return `<div class="st-cm"><div class="st-tb"><div class="st-chips" role="tablist" aria-label="分组"><i class="st-chl"></i>`
      + G.map(g => `<button type="button" role="tab" class="st-chip${g.i === D.act ? " on" : ""}" data-st="chip" data-g="${g.i}" aria-selected="${g.i === D.act}">${g.name}</button>`).join("")
      + `</div><span class="st-sp"></span><label class="st-cs${D.q ? " has" : ""}" title="中文名或键名，如 视距、rcon；Esc 清空"><i data-ic="search"></i>`
      + `<input class="st-q" placeholder="搜索配置项" value="${esc(D.q)}" autocomplete="off" spellcheck="false" aria-label="搜索配置项"><button type="button" class="st-cx" title="清空"><i data-ic="x"></i></button></label></div>`
      + `<section class="st-box" aria-label="常用设置"><div class="st-lw"><div class="list">${whoHTML(D)}${G.map(g => groupHTML(D, g)).join("")}</div><span class="thumb"></span>`
      + `<div class="st-none">没有匹配的配置项</div></div></section>${barHTML(cmDirty(D).length)}</div>`;
  }

  // ---- 挂上去 ----
  let spyLock = 0;
  function cmMount(ctx) {
    C = ctx; const r = cmRoot(); if (!r) return;
    if (!cmData()) { // 目录比外壳晚到（很少见）：到了再把这一页画一次
      CAT_READY.then(() => { if (!r.isConnected) return; const nr = tpl(cmHTML(C)); r.replaceWith(nr); C.KIT.hydrate(nr); cmMount(C); });
      return;
    }
    const D = cmData(), list = r.querySelector(".st-lw .list");
    ctx.thinBar(r.querySelector(".st-lw"));
    r.querySelectorAll(".st-seg").forEach(s => placeSeg(s, false));
    placeChip(false);
    filter(D, false);
    paintNote(PG_CM, true);
    wireSteps(D);
    r.addEventListener("click", cmClick);
    r.addEventListener("input", cmInput);
    r.addEventListener("keydown", cmKey);
    r.addEventListener("focusout", cmBlur);
    r.addEventListener("pointerdown", e => { if (e.target.closest(".st-pop")) e.preventDefault(); });
    list.addEventListener("scroll", () => { closeMenu(); if (pop?._a && list.contains(pop._a)) closePop(); if (!spyLock) spy(); }, { passive: true });
    list.addEventListener("scrollend", () => { spyLock = 0; });
    document.fonts?.ready.then(() => { if (!r.isConnected) return; r.querySelectorAll(".st-seg").forEach(s => placeSeg(s, false)); placeChip(false); });
    if (cmFocus) { const k = cmFocus; cmFocus = ""; requestAnimationFrame(() => focusField(k)); }
    else if (D.top) list.scrollTop = D.top; // 换标签回来：停在原来滚到的地方
    list.addEventListener("scroll", () => { D.top = list.scrollTop; }, { passive: true });
  }
  function focusField(k) {
    const el = fieldEl(k), list = cmList(); if (!el || !list) return;
    spyLock = 1; clearTimeout(goGroup._t); goGroup._t = setTimeout(() => { spyLock = 0; }, 300);
    list.scrollTop = Math.max(0, el.offsetTop - list.clientHeight / 2 + el.offsetHeight / 2);
    setChip(+el.closest(".st-g").dataset.g, false); el.classList.remove("st-flash"); void el.offsetWidth; el.classList.add("st-flash");
    el.querySelector("input, textarea")?.focus({ preventScroll: true });
  }

  // ---- 分组片：选中块滑到下面正看着的那一组（滚着看时跟着走，点了滚过去） ----
  function placeChip(animate) {
    const r = cmRoot(); if (!r) return;
    const hl = r.querySelector(".st-chl"), on = r.querySelector(".st-chip.on"); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function setChip(i, animate) {
    const r = cmRoot(), D = cmData(); if (!r) return;
    if (D.act === i && r.querySelector(".st-chip.on")?.dataset.g === String(i)) return;
    D.act = i;
    r.querySelectorAll(".st-chip").forEach(c => { const on = +c.dataset.g === i; c.classList.toggle("on", on); c.setAttribute("aria-selected", on); });
    placeChip(animate);
  }
  function spy() {
    const list = cmList(); if (!list) return;
    const gs = [...list.querySelectorAll(".st-g:not([hidden])")]; if (!gs.length) return;
    let act = gs[0];
    for (const g of gs) if (g.offsetTop <= list.scrollTop + 24) act = g;
    if (list.scrollTop > 0 && list.scrollTop + list.clientHeight >= list.scrollHeight - 2) act = gs[gs.length - 1]; // 到底了：最后一组太短、滚不到顶上时也亮它
    setChip(+act.dataset.g, true);
  }
  function goGroup(i) {
    const list = cmList(), g = list?.querySelector(`.st-g[data-g="${i}"]`); if (!g || g.hidden) return;
    setChip(i, true);
    const top = i === +list.querySelector(".st-g:not([hidden])").dataset.g ? 0 : g.offsetTop; // 第一组：连同上面「谁能进」一起露出来
    if (Math.abs(list.scrollTop - top) < 2) return;
    spyLock = 1; clearTimeout(goGroup._t); goGroup._t = setTimeout(() => { spyLock = 0; }, 900); // 不支持 scrollend 时的兜底
    list.scrollTo({ top, behavior: "smooth" });
  }

  // ---- 搜索：边打边筛，不匹配的格子藏起来（改过的值都还在、保存时照样提交）；空了的组、空了的格子区一起藏，分组片灰掉 ----
  // 换的样子同「全部配置」：旧的整块往上 5 像素淡掉（0.09 秒），新的从下面 5 像素一格一格淡进来（0.22 秒、错开 22 毫秒，只做看得见的）
  function filter(D, animate) {
    const r = cmRoot(); if (!r) return;
    const list = r.querySelector(".st-lw .list"), lw = r.querySelector(".st-lw"), k = skey(D.q), cells = [...list.querySelectorAll("[data-search]")];
    const apply = () => {
      let n = 0;
      cells.forEach(x => { const hit = !k || skey(x.dataset.search).includes(k); x.hidden = !hit; if (hit) n++; });
      list.querySelectorAll(".st-grid, .st-bgrid, .st-who").forEach(b => { b.hidden = !b.querySelector("[data-search]:not([hidden])"); });
      list.querySelectorAll(".st-grh").forEach(h => { h.hidden = h.nextElementSibling.hidden; });
      list.querySelectorAll(".st-g").forEach(g => { g.hidden = !g.querySelector("[data-search]:not([hidden])"); });
      r.querySelectorAll(".st-chip").forEach(c => { const off = list.querySelector(`.st-g[data-g="${c.dataset.g}"]`).hidden; c.classList.toggle("off", off); c.tabIndex = off ? -1 : 0; });
      lw.classList.toggle("is-none", !n);
      list.querySelectorAll(".st-seg").forEach(s => placeSeg(s, false)); // 藏着时量不到位置，露出来以后再放一次
      lw._paint?.();
    };
    if (!animate) { apply(); return; }
    const tok = list._tok = (list._tok || 0) + 1;
    list.getAnimations({ subtree: true }).forEach(a => a.cancel());
    const out = list.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" });
    out.onfinish = () => {
      if (list._tok !== tok) return;
      apply(); list.scrollTop = 0; out.cancel(); spy();
      const bottom = list.getBoundingClientRect().bottom;
      cells.filter(x => !x.hidden && x.getBoundingClientRect().top < bottom).forEach((x, i) => rise(x, Math.min(i, 14)));
    };
  }
  function clearQ(D, inp) { D.q = ""; inp.value = ""; inp.closest(".st-cs").classList.remove("has"); clearTimeout(qT); filter(D, true); }

  // ---- 改值：只换动了的那一处、那一项的小点和保存条 ----
  function mark(D, k) { const el = fieldEl(k); if (el) el.classList.toggle("mod", D.cur[k] !== D.base[k]); }
  function setVal(D, k, v) { D.cur[k] = v; mark(D, k); paintBar(PG_CM); }
  function paintSwitch(el, on) { el.classList.toggle("on", on); el.setAttribute("aria-checked", on); }
  function paintStep(el, p, v) {
    const s = p.spec, sv = el.querySelector(".st-sv"), [mi, pl] = el.querySelectorAll(".st-stp button"), t = fmtStep(s, v);
    if (sv.textContent !== t) sv.textContent = t;
    const dn = stepTo(s, v, -1) !== undefined, up = stepTo(s, v, 1) !== undefined;
    setAttr(mi, "aria-disabled", !dn); setTip(mi, dn ? "减小" : `已达最小值（${s.fmt(s.lad[0])}）`);
    setAttr(pl, "aria-disabled", !up); setTip(pl, up ? "增大" : `已达最大值（${s.fmt(s.lad[s.lad.length - 1])}）`);
  }
  // 步进打字：框里是不带单位的整数（单位写在悬停和填错的提示里），范围是档位的两头，不在档位上的照收。步进只在挂上去时画一次、之后只换字，所以这里交一次就够
  function wireSteps(D) {
    D.items.filter(p => p.spec.ctl === "step").forEach(p => {
      const el = fieldEl(p.key), sv = el?.querySelector(".st-sv"); if (!sv) return;
      const s = p.spec, lo = s.lad[0], hi = s.lad[s.lad.length - 1];
      setTip(sv, `点击输入数值（单位：${s.unit}）`);
      numEdit(sv, { get: () => num(D.cur[p.key]), set: n => { setVal(D, p.key, String(n)); paintStep(el, p, D.cur[p.key]); }, label: p.label,
        text: n => (Number.isNaN(n) ? "" : String(n)), parse: raw => { const n = num(raw.replace(s.unit, "")); return n >= lo && n <= hi ? n : NaN; },
        hint: `请输入 ${lo}～${hi} 之间的整数（单位：${s.unit}）` });
    });
  }
  function paintBad(el, p, v) {
    const iw = el.querySelector(".st-iw"), m = el.querySelector(".st-msg"), bad = badOf(p, v); if (!iw || !m) return;
    iw.classList.toggle("bad", !!bad); m.textContent = bad ? bad[0] : ""; setTip(m, bad ? bad[1] : "");
  }
  // 放弃修改时把一项换回存下来的样子（只换这一项）
  function paintField(D, k) {
    const el = fieldEl(k); if (!el) return; const p = itemOf(D, k), v = D.cur[k];
    if (el.classList.contains("st-b")) paintSwitch(el, v === "true");
    else if (p.spec.ctl === "step") paintStep(el, p, v);
    else if (p.spec.ctl === "seg") pickSeg(el.querySelector(".st-seg"), v, true);
    else if (p.spec.ctl === "sel") el.querySelector(".st-selv").textContent = optLabel(p, v);
    else {
      const inp = el.querySelector(".st-in"); if (inp.value !== v) inp.value = v;
      paintBad(el, p, v); if (p.key === "motd") el.querySelector(".st-mdp").innerHTML = motdHTML(v);
    }
    mark(D, k);
  }
  // 保存前：有不对的框就拦下——滚过去、框晃一下、光标落过去（规范第七节「反馈」）
  function cmCheck(D) {
    const bad = cmDirty(D).find(k => badOf(itemOf(D, k), D.cur[k])); if (!bad) return true;
    const el = fieldEl(bad), list = cmList();
    if (D.q && el?.hidden) clearQ(D, cmRoot().querySelector(".st-q"));
    setTimeout(() => {
      const f = fieldEl(bad); if (!f) return;
      const top = f.offsetTop, h = list.clientHeight;
      if (top < list.scrollTop || top + f.offsetHeight > list.scrollTop + h) list.scrollTo({ top: Math.max(0, top - h / 2), behavior: "smooth" });
      const iw = f.querySelector(".st-iw"); shake(iw); f.querySelector("input, textarea").focus({ preventScroll: true });
    }, D.q ? 320 : 0);
    return false;
  }
  function cmDiscard(D) {
    cmDirty(D).forEach(k => { D.cur[k] = D.base[k]; paintField(D, k); });
    paintBar(PG_CM);
  }

  // ---- 事件（绑在这一页的根上，换标签时跟着内容一起扔掉） ----
  let qT = 0;
  function cmClick(e) {
    const D = cmData(); if (!D) return;
    if (e.target.closest(".st-cx")) { e.preventDefault(); const inp = cmRoot().querySelector(".st-q"); clearQ(D, inp); inp.focus(); return; }
    const sb = e.target.closest(".st-seg button");
    if (sb) {
      if (sb.classList.contains("on")) return;
      const el = sb.closest(".st-f"); pickSeg(sb.parentElement, sb.dataset.v, true); setVal(D, el.dataset.k, sb.dataset.v); return;
    }
    const sw = e.target.closest(".st-b");
    if (sw) {
      if (sw.getAttribute("aria-disabled") === "true") return;
      if (sw.hasAttribute("data-gr")) { // 游戏规则：开着时马上生效（程序发 /gamerule），不进保存条
        const k = sw.dataset.gr; D.gr[k] = D.gr[k] === "true" ? "false" : "true"; paintSwitch(sw, D.gr[k] === "true"); return;
      }
      const k = sw.dataset.k, v = D.cur[k] === "true" ? "false" : "true"; paintSwitch(sw, v === "true"); setVal(D, k, v); return;
    }
    const b = e.target.closest("[data-st]"); if (!b || b.getAttribute("aria-disabled") === "true" || b.disabled) return;
    const el = b.closest(".st-f"), p = el && itemOf(D, el.dataset.k);
    switch (b.dataset.st) {
      case "chip": if (!b.classList.contains("off")) goGroup(+b.dataset.g); break;
      case "dec": case "inc": {
        const v = stepTo(p.spec, D.cur[p.key], b.dataset.st === "inc" ? 1 : -1); if (v === undefined) return;
        setVal(D, p.key, String(v)); paintStep(el, p, D.cur[p.key]); break;
      }
      case "sel": openMenu(b, p.spec.opts.map(([v, l]) => `<div class="st-it${v === D.cur[p.key] ? " here" : ""}" role="menuitemradio" aria-checked="${v === D.cur[p.key]}" data-v="${esc(v)}"><span class="st-im">${esc(l)}</span>`
        + `<span class="st-is mono">${esc(v.replace("minecraft:", ""))}</span>${v === D.cur[p.key] ? `<i data-ic="check" class="st-ck"></i>` : ""}</div>`).join(""),
        v => { setVal(D, p.key, v); b.querySelector(".st-selv").textContent = optLabel(p, v); }); break;
      case "pal": openPop(b, palHTML(), "st-palp"); break;
      case "players": closeMenu(); closePop(); C.goTab("players"); break;
      case "discard": cmDiscard(D); break;
      case "save": save(PG_CM); break;
      case "restart": restart(PG_CM); break;
    }
  }
  function cmInput(e) {
    const D = cmData(); if (!D) return;
    if (e.target.classList.contains("st-q")) {
      D.q = e.target.value; e.target.closest(".st-cs").classList.toggle("has", !!D.q);
      clearTimeout(qT); qT = setTimeout(() => filter(D, true), 180); return;
    }
    const el = e.target.closest(".st-f"); if (!el || !e.target.classList.contains("st-in")) return;
    if (e.target.tagName === "TEXTAREA" && e.target.value.split("\n").length > 2) { // 粘贴进来超过两行：只留前两行
      const t = e.target, pos = t.selectionStart; t.value = t.value.split("\n").slice(0, 2).join("\n"); t.selectionStart = t.selectionEnd = Math.min(pos, t.value.length);
    }
    const p = itemOf(D, el.dataset.k), v = e.target.value;
    setVal(D, p.key, v); paintBad(el, p, v);
    if (p.key === "motd") el.querySelector(".st-mdp").innerHTML = motdHTML(v);
  }
  function cmKey(e) {
    const D = cmData(); if (!D) return;
    if (e.target.classList.contains("st-q") && e.key === "Escape" && e.target.value) { e.preventDefault(); e.stopPropagation(); clearQ(D, e.target); return; }
    if (e.target.classList.contains("st-in") && e.key === "Enter") {
      if (e.target.tagName === "TEXTAREA") { // 服务器简介：回车换行，最多两行（游戏的服务器列表只显示两行）；已经两行时回车不做事
        const t = e.target, rest = t.value.slice(0, t.selectionStart) + t.value.slice(t.selectionEnd);
        if (rest.includes("\n")) e.preventDefault();
        return;
      }
      e.preventDefault(); e.target.blur(); // 回车只是改完这一格；保存在下面
    }
  }
  function cmBlur(e) { // 整数框离开时去掉两头的空格
    const el = e.target.closest?.(".st-f"); if (!el || !e.target.classList.contains("st-in")) return;
    const D = cmData(), p = itemOf(D, el.dataset.k); if (p.spec.ctl !== "num") return;
    const v = e.target.value.trim(); if (v !== e.target.value) { e.target.value = v; setVal(D, p.key, v); paintBad(el, p, v); }
  }
  // 调色板：16 色、5 种样式（同程序）；按下时不抢焦点，浮层开着可以连着点。
  // 没选中字时把 § 码插在光标处（同程序）；选中了一段字时只改这一段（作者 11-28 要的；程序现在是把选中的字换成 § 码）：
  //   前面插上新的码，后面补「§r + 选中前一刻生效的颜色和样式」，让后面的字照旧；改完那一段仍是选中的，可以接着叠样式。
  //   § 码的规矩（游戏的）：颜色码会把粗体、斜体这些样式清掉，§r 全清。所以给一段换颜色时，前面已经生效的样式在新颜色码后面再补一遍，
  //   段里原有的颜色码去掉；给一段加样式时，段里每个颜色码、§r 后面也补上这个样式；「重置」把段里的码都去掉。
  const MFMT = new Set(["l", "o", "n", "m"]);
  function motdState(text, upto) { // text 前 upto 个字走完以后生效的颜色和样式
    let c = "", f = [];
    for (let i = 0; i < upto; i++) {
      if (text[i] !== "§" || i + 1 >= upto) continue;
      const k = text[++i].toLowerCase();
      if (MCC[k]) { c = k; f = []; } else if (k === "r") { c = ""; f = []; } else if (MFMT.has(k) && !f.includes(k)) f.push(k);
    }
    return { c, f };
  }
  const fmtCodes = f => f.map(k => "§" + k).join("");
  function motdApply(text, s, e, k) { // 返回 [新的字, 新选区起, 新选区止]
    if (s > 0 && text[s - 1] === "§") s--; // 别把 § 和它后面那个字拆开
    if (e > s && text[e - 1] === "§" && e < text.length) e++;
    if (s === e) return [text.slice(0, s) + "§" + k + text.slice(e), s + 2, s + 2];
    const a = motdState(text, s), b = motdState(text, e), sel = text.slice(s, e);
    let pre, mid;
    if (MCC[k]) { pre = "§" + k + fmtCodes(a.f); mid = sel.replace(/§[0-9a-fr]/gi, ""); }
    else if (k === "r") { pre = "§r"; mid = sel.replace(/§[0-9a-fk-or]/gi, ""); }
    else { pre = "§" + k; mid = sel.replace(/§[0-9a-fr]/gi, m => m + "§" + k); }
    const after = text.slice(e), post = !after || /^§[0-9a-fr]/i.test(after) ? "" : "§r" + (b.c ? "§" + b.c : "") + fmtCodes(b.f);
    return [text.slice(0, s) + pre + mid + post + after, s + pre.length, s + pre.length + mid.length];
  }
  const palHTML = () => `<div class="st-pal">${Object.keys(MCC).map(c => `<button type="button" class="st-pc" data-c="${c}" style="--c:${MCC[c]}" title="§${c} ${MCN[c]}"></button>`).join("")}</div>`
    + `<div class="st-pst">${MSTY.map(([c, t]) => `<button type="button" class="st-ps st-ps-${c}" data-c="${c}" title="§${c}">${t}</button>`).join("")}</div>`;
  document.addEventListener("pointerdown", e => { if (pop && e.target.closest(".st-palp")) e.preventDefault(); });
  document.addEventListener("click", e => {
    const b = e.target.closest(".st-palp [data-c]"); if (!b || !pop) return;
    const D = cmData(), el = fieldEl("motd"), inp = el?.querySelector(".st-in"); if (!inp) return;
    const pos = inp.selectionStart ?? inp.value.length, end = inp.selectionEnd ?? pos;
    const [t, ns, ne] = motdApply(inp.value, pos, end, b.dataset.c);
    inp.value = t; inp.focus(); inp.setSelectionRange(ns, ne);
    setVal(D, "motd", inp.value); el.querySelector(".st-mdp").innerHTML = motdHTML(inp.value);
  });

  // =====================================================================================================
  // 启动与内存
  // =====================================================================================================
  // 本机内存是假的，同向导（[总量, 其他程序已占]，单位 MB；32 是作者截图那台）。「已占」里不算这台服务器自己
  const MACHINES = { 8: [8192, 6656], 16: [16384, 7578], 32: [32563, 15565], 48: [49152, 16691], 64: [65536, 19149], 128: [131072, 23450] };
  let MEM = null;
  function machine() {
    const k = MACHINES[C.QS.get("ram")] ? +C.QS.get("ram") : 32, [total, used] = MACHINES[k], avail = total - used;
    const cell = [512, 1024, 2048, 4096].find(c => total / c <= 40) || 4096;
    return { total, used, avail, cap: Math.max(512, Math.floor(avail / 512) * 512), cell, cells: Math.ceil(total / cell), blocks: total > 32768 };
  }
  const gs = mb => { const x = Math.round(mb / 1024 * 10) / 10; return (Number.isInteger(x) ? x : x.toFixed(1)) + "G"; }; // 同向导：整数不带 .0
  const gbTxt = mb => gs(mb).replace("G", " GB");
  const memTxt = v => (v % 512 ? `${v} MB` : gbTxt(v)); // 不是 0.5 GB 整数倍的照原样写 MB
  const PRESETS = [2048, 4096, 8192, 16384], PRESETS_PROXY = [512, 1024, 2048]; // 同向导
  // Java：本机有的（MineDeck 下载的、本机装的）和能现下的，同程序 /api/javas 和 JAVA_DOWNLOADABLE；路径是编的
  const JAVAS = { portable: [21, 17, 25], scanned: [{ major: 8, version: "1.8.0_421", path: "C:\\Program Files\\Java\\jre1.8.0_421\\bin\\java.exe" }] };
  const JAVA_DOWNLOADABLE = [8, 11, 16, 17, 21, 25];
  const MISSING_JAVA = "C:\\Program Files\\Java\\jdk-21\\bin\\java.exe"; // 同页头「找不到这台服务器配的 Java」那条
  const JAVA_BROKEN_WHY = "运行 java -version 失败：找不到 bin\\server\\jvm.dll（文件可能被杀毒软件删除或磁盘出错）"; // 编的，同全局设置 Java 那页
  // 每台的启动设置：内存同控制台第一行的启动命令（Paper、原版 4096，Forge 6144）；构建号是编的（程序里没有换构建，见开头）。
  // need 是这个版本至少要的 Java（同程序：游戏版本照官方元数据，缺字段时 mcsrc.staticJavaFor 兜底——26.1 起 25、1.20.5 起 21、1.18 起 17；
  // 代理端是 mcsrc 里写死的 FixedJava，Velocity 21）。比它低的在下拉里灰掉（作者 11-28 要的：程序现在换 Java 时不拦，选了起不来）
  const JV_SEED = {
    paper: { xmx: 4096, java: 21, need: 21, build: "232", builds: ["232", "231", "230", "229", "228", "227", "226", "224"], dir: "D:\\MineDeck\\servers\\server-20261101-203512" },
    forge: { xmx: 6144, java: 17, need: 17, build: "47.3.0", builds: ["47.4.0", "47.3.12", "47.3.10", "47.3.7", "47.3.0", "47.2.32", "47.2.20", "47.2.0"], dir: "D:\\MineDeck\\servers\\create-pack" },
    vanilla: { xmx: 4096, java: 25, need: 25, build: "", builds: [], dir: "D:\\MineDeck\\servers\\server-20261120-195841" },
    proxy: { xmx: 1024, java: 21, need: 21, build: "498", builds: ["510", "509", "505", "498", "492", "487", "480"], dir: "D:\\MineDeck\\servers\\server-20261015-221007" },
  };
  // 同程序 aikarsFlags：12 GB 以上换一套
  const aikars = xmx => {
    const big = xmx >= 12288;
    return ["-XX:+UseG1GC", "-XX:+ParallelRefProcEnabled", "-XX:MaxGCPauseMillis=200", "-XX:+UnlockExperimentalVMOptions", "-XX:+DisableExplicitGC", "-XX:+AlwaysPreTouch",
      big ? "-XX:G1NewSizePercent=40" : "-XX:G1NewSizePercent=30", big ? "-XX:G1MaxNewSizePercent=50" : "-XX:G1MaxNewSizePercent=40",
      big ? "-XX:G1HeapRegionSize=16M" : "-XX:G1HeapRegionSize=8M", big ? "-XX:G1ReservePercent=15" : "-XX:G1ReservePercent=20",
      "-XX:G1HeapWastePercent=5", "-XX:G1MixedGCCountTarget=4", big ? "-XX:InitiatingHeapOccupancyPercent=20" : "-XX:InitiatingHeapOccupancyPercent=15",
      "-XX:G1MixedGCLiveThresholdPercent=90", "-XX:G1RSetUpdatingPauseTimePercent=5", "-XX:SurvivorRatio=32", "-XX:+PerfDisableSharedMem", "-XX:MaxTenuringThreshold=1",
      "-Dusing.aikars.flags=https://mcflags.emc.gs", "-Daikars.new.flags=true"];
  };
  const LOG4J_FIX = ["-Dlog4j2.formatMsgNoLookups=true", "-Dlog4j.configurationFile=log4j2_112-116.xml", "-Dlog4j.configurationFile=log4j2_17-111.xml"]; // 同程序：套用、清空时原样留着
  const MEM_FLAG = /^-(Xmx|Xms|XX:(MaxHeapSize|InitialHeapSize|MinHeapSize)=)/i; // 同后端 jvmMemoryPrefixes

  const JV = {};
  let seqId = 0;
  function jvData() {
    const k = C.kind;
    if (JV[k]) return JV[k];
    const s = JV_SEED[k] || JV_SEED.paper, q = parseInt(C.QS.get("mem"), 10);
    const xmx = q >= 512 && q <= 262144 ? q : s.xmx, missing = (+C.QS.get("notice") || 0) >= 2;
    const javas = [];
    if (missing) javas.push({ major: 21, path: MISSING_JAVA, missing: true });
    JAVAS.portable.forEach(m => javas.push({ major: m, portable: true }));
    // 草图：&java=bad 这台配的那套 MineDeck 下载的 Java 检查不过（同全局设置 Java 那页的 &java=bad；原因是编的，照 v3.3.0-beta.1 整改计划第四节的做法）
    if (C.QS.get("java") === "bad") { const o = javas.find(x => x.portable && x.major === s.java); if (o) { o.broken = true; o.why = JAVA_BROKEN_WHY; } }
    JAVAS.scanned.forEach(j => javas.push({ ...j }));
    const have = new Set(javas.filter(o => !o.missing).map(o => o.major));
    JAVA_DOWNLOADABLE.filter(m => !have.has(m)).forEach(m => javas.push({ major: m, download: true }));
    const java = missing ? 0 : javas.findIndex(o => o.portable && o.major === s.java);
    const args = (C.QS.get("jvm") === "aikar" ? aikars(xmx) : []).map(t => ({ id: ++seqId, t }));
    JV[k] = { xmx, java, build: s.build, builds: s.builds, dir: s.dir, javas, args, form: "",
      base: { xmx, java, build: s.build, args: args.map(a => a.t) }, pending: false, saving: false, restarting: false, dl: false,
      need: s.need || 8, needWho: C.S.proxy ? `${C.S.label} ${C.S.mc}` : `Minecraft ${C.S.mc}`, redl: "" }; // redl：重新下载进行到哪（"" / "dl" / "check"）
    return JV[k];
  }
  const argStr = list => list.join(" ");
  const jvArgs = D => D.args.map(a => a.t);
  const jvDirty = D => [D.xmx !== D.base.xmx, argStr(jvArgs(D)) !== argStr(D.base.args), D.java !== D.base.java, D.build !== D.base.build].filter(Boolean).length;
  const jvRoot = () => document.querySelector(".cp > .st-jv:not(.fade-out)");
  const PG_JV = {
    root: jvRoot, D: () => jvData(), count: jvDirty,
    check: () => jvCheck(), discard: D => jvDiscard(D),
    commit: D => {
      const dl = D.java !== D.base.java && D.javas[D.java].download;
      D.base = { xmx: D.xmx, java: D.java, build: D.build, args: jvArgs(D) };
      const r = jvRoot(); if (!r) return;
      r.querySelectorAll(".mod").forEach(el => el.classList.remove("mod"));
      paintArgsMod(D);
      if (dl) downloadJava(); else paintJava(D);
      paintBuild(D);
    },
  };
  // 选的 Java 本机没有：程序保存以后去任务页看下载进度、下好了自动换上。草图里跳后台任务进度页 task.html 从头放一遍（地址里只带配色参数）
  function downloadJava() {
    const q = new URLSearchParams({ kind: "java", play: "1" });
    ["theme", "scheme", "accent"].forEach(k => C.QS.get(k) && q.set(k, C.QS.get(k)));
    location.href = "task.html?" + q;
  }
  const buildName = b => (C.kind === "forge" ? b : `#${b}`);
  const javaLow = (D, o) => !o.missing && o.major < D.need; // 低于这个版本至少要的 Java：灰掉、悬停说原因
  const javaSub = (D, o, i) => (o.missing ? "未找到" : o.download ? "需下载（约 50 MB）" : o.portable ? "MineDeck 已下载" : `本机已安装 ${o.version}`)
    + (o.broken ? " · 无法启动" : "") + (i === D.base.java && !o.missing ? " · 当前使用" : "");
  const javaTip = (D, o) => o.missing ? `${o.path}\n未找到该 Java 路径（可能已被卸载或移动），当前无法启动服务器`
    : o.broken ? `Java ${o.major} 检查未通过：${o.why}\n可点开下拉重新下载，或换一个 Java`
    : javaLow(D, o) ? `${D.needWho} 需要 Java ${D.need} 及以上` : o.path || "";

  // ---- 画 ----
  function memHTML(D) {
    const M = MEM, proxy = !!C.S.proxy;
    const tip = `本机总内存 ${gbTxt(M.total)}${M.blocks ? "" : ` · 每格 ${gbTxt(M.cell)}`} · 可分配上限 ${gbTxt(M.cap)}（不含本服已占内存）\n` + (proxy ? "推荐 1 GB（代理端仅做转发，占用较低）" : ["forge", "neoforge", "fabric"].includes(C.kind) ? "模组服推荐分配 4 GB 以上" : "小型服务器推荐分配 2～4 GB");
    const body = M.blocks ? `<i class="st-bu" style="--w:${M.used}"></i><i class="st-ba" style="--w:${M.avail}"><b></b></i>` : "<i></i>".repeat(M.cells);
    return `<div class="st-fld st-mem"><span class="st-lb2"><span>内存分配</span><i class="st-dot"></i><span class="st-mleg"></span></span>`
      + `<div class="st-memx"><div class="st-mbar" tabindex="0" role="slider" aria-label="内存分配" aria-valuemin="512" aria-valuemax="${M.cap}" title="${esc(tip)}">`
      + `<div class="st-cells${M.blocks ? " blk" : ""}">${body}<span class="st-knob"></span></div></div>`
      + `<div class="st-mstp"><button type="button" data-st="m-" aria-label="减少 0.5 GB"><i data-ic="minus"></i></button><span class="st-msv"></span><button type="button" data-st="m+" aria-label="增加 0.5 GB"><i data-ic="plus"></i></button></div></div>`
      + `<div class="st-mpre">${(proxy ? PRESETS_PROXY : PRESETS).map(v => `<button type="button" class="st-mp" data-st="mp" data-v="${v}"${v > M.cap ? " disabled" : ""}>${gs(v)}</button>`).join("")}<span class="st-mlow"></span></div></div>`;
  }
  const javaFieldHTML = D => `<span class="st-selv"></span><i data-ic="chevronDown" class="st-chev"></i>`;
  function jvHTML(ctx) {
    C = ctx; MEM = machine(); const D = jvData();
    const port = C.S.proxy ? String(C.S.port) : (CM[C.kind]?.base["server-port"] ?? String(C.S.port));
    const hasBuild = D.builds.length > 0;
    const left = `<section class="st-col" aria-label="内存与 Java">${memHTML(D)}`
      + `<div class="st-r2${hasBuild ? "" : " one"}"><div class="st-fld st-javaf"><span class="st-lb2"><span title="${C.S.proxy ? "如需更换运行所用的 Java 版本，保存后于下次启动生效；本地未安装的版本将在保存时自动下载。" : "新建时已按 Minecraft 版本自动匹配（1.20.1 用 Java 17，1.21 用 Java 21，26.1 起用 Java 25）。如需更换版本，保存后于下次启动生效；本地未安装的版本将在保存时自动下载。"}">Java</span><i class="st-dot"></i></span>`
      + `<button type="button" class="st-sel st-java" data-st="java" aria-haspopup="menu" aria-expanded="false" aria-label="服务器 Java 版本"><i data-ic="coffee"></i>${javaFieldHTML(D)}</button></div>`
      + (hasBuild ? `<div class="st-fld st-buildf"><span class="st-lb2"><span title="${C.S.proxy ? "同一版本的代理端可能发布多次构建更新" : "同一游戏版本的服务端可能发布多次构建更新"}，新建时默认使用最新稳定构建。若与某个${C.kind === "forge" ? "模组" : "插件"}不兼容，可在此切换旧版构建，保存后于下次启动生效。">构建</span><i class="st-dot"></i></span>`
        + `<button type="button" class="st-sel st-build" data-st="build" aria-haspopup="menu" aria-expanded="false" aria-label="构建"><span class="st-selv"></span><i data-ic="chevronDown" class="st-chev"></i></button></div>` : "")
      + `</div><div class="st-r2"><div class="st-fld"><span class="st-lb2"><span>服务器文件夹</span></span>`
      + `<div class="st-path" title="${esc(D.dir + "\n手动启动可双击该文件夹中的 run.bat，其参数与本页配置同步")}"><i data-ic="folder"></i><b><span class="hd">${esc(D.dir.slice(0, D.dir.lastIndexOf("\\")))}</span><span class="tl">${esc(D.dir.slice(D.dir.lastIndexOf("\\")))}</span></b>`
      + `<button type="button" class="st-ib" data-st="copy" title="复制路径"><i data-ic="copy"></i></button></div></div>`
      + `<div class="st-fld"><span class="st-lb2"><span>端口</span></span>` + (C.S.proxy
        ? `<div class="st-port off" title="请在代理端配置文件中修改（Velocity 为 velocity.toml，Waterfall 为 config.yml）"><b>${esc(port)}</b></div>`
        : `<button type="button" class="st-port" data-st="port" title="在「常用」中修改端口"><b>${esc(port)}</b><i data-ic="arrowRight"></i></button>`) + `</div></div></section>`;
    const right = `<section class="st-jcol" aria-label="自定义 JVM 参数"><div class="st-jh"><span class="st-lb2"><span title="每行一项；-Xmx、-Xms 由左侧内存分配自动生成，无需在此填写">JVM 参数</span><i class="st-dot"></i></span><span class="st-sp"></span>`
      + `<button type="button" class="st-chip st-aik" data-st="aikar"><i data-ic="plus"></i>Aikar's Flags</button>`
      + `<button type="button" class="st-hb" data-st="preview" title="实际启动参数" aria-haspopup="dialog"><i data-ic="doc"></i></button>`
      + `<button type="button" class="st-hb" data-st="clear" title="清空"><i data-ic="trash"></i></button></div>`
      + `<div class="st-jlw"><div class="list" role="list">${D.args.map(a => argRow(D, a)).join("")}</div><span class="thumb"></span><div class="st-jnone">暂无自定义参数</div></div>`
      + `<label class="st-iw mono st-jadd"><input class="st-in" value="${esc(D.form)}" placeholder="如 -XX:+UseG1GC" autocomplete="off" spellcheck="false" aria-label="添加 JVM 参数">`
      + `<span class="st-msg"></span><button type="button" class="st-ib" data-st="jadd" title="添加参数（以空格分隔可一次添加多项）"><i data-ic="plus"></i></button></label></section>`;
    return `<div class="st-jv">${left}${right}${barHTML(jvDirty(D))}</div>`;
  }
  const isNewArg = (D, t) => !D.base.args.includes(t);
  const argRow = (D, a) => `<div class="st-jr${isNewArg(D, a.t) ? " mod" : ""}" role="listitem" data-id="${a.id}"><span class="st-jt" title="${esc(a.t)}">${esc(a.t)}</span>`
    + `<i class="st-dot"></i><button type="button" class="st-jx" data-st="jdel" title="删除" aria-label="删除 ${esc(a.t)}"><i data-ic="x"></i></button></div>`;

  // ---- 挂上去 ----
  function jvMount(ctx) {
    C = ctx; MEM = machine(); const r = jvRoot(); if (!r) return; const D = jvData();
    ctx.thinBar(r.querySelector(".st-jlw"));
    paintMem(D); paintJava(D); paintBuild(D); paintArgsUI(D);
    paintNote(PG_JV, true);
    numEdit(r.querySelector(".st-msv"), { get: () => D.xmx, set: n => setMem(D, n), text: memTxt, parse: memParse, label: "内存分配",
      hint: `请输入 0.5～${gbTxt(MEM.cap)} 之间的数值；单位默认为 GB，也可写作「4096 MB」` });
    r.addEventListener("click", jvClick);
    r.addEventListener("input", jvInput);
    r.addEventListener("keydown", jvKey);
    r.addEventListener("focusout", jvBlur);
    r.addEventListener("pointerdown", memDown);
    r.querySelector(".st-jlw .list").addEventListener("scroll", () => closeMenu(), { passive: true });
    document.fonts?.ready.then(() => { if (r.isConnected) paintMem(D); });
  }

  // ---- 内存：控件和代码照向导第三步（格子也是滑条；点、拖、方向键都行；0.5 GB 一格地走；步进是这一块里唯一写着数的地方） ----
  const C_USED = "color-mix(in srgb, var(--text) 30%, transparent)", C_ALLOC = "var(--accent)", C_FREE = "color-mix(in srgb, var(--text) 9%, transparent)";
  const memClamp = v => Math.min(MEM.cap, Math.max(512, Math.round((+v || 512) / 512) * 512)); // 按 0.5 GB 取整，夹在 0.5 GB 和上限之间
  function paintMem(D) {
    const r = jvRoot(), bar = r?.querySelector(".st-mbar"); if (!bar) return;
    const M = MEM, v = D.xmx, end = M.used + v;
    if (M.blocks) { // 本机超过 32 GB：两块（同向导），本服分配从可用那块的左边涂起
      const ba = bar.querySelector(".st-ba"), w = Math.round(ba.clientWidth * Math.min(1, v / M.avail));
      ba.querySelector("b").style.width = w + "px";
      bar.querySelector(".st-knob").style.left = (ba.offsetLeft + w) + "px";
    } else { // 格子：灰的系统已占、强调色本服分配、淡的剩余；一格只占一部分时按比例涂（3000 MB 这种也画得准）
      bar.querySelectorAll(".st-cells i").forEach((c, i) => {
        const a = i * M.cell, pct = x => Math.max(0, Math.min(100, (x - a) / M.cell * 100)), u = pct(M.used), e = pct(end);
        c.className = u <= 0 && e >= 100 ? "alloc" : "";
        c.style.background = `linear-gradient(to right, ${C_USED} 0 ${u}%, ${C_ALLOC} ${u}% ${e}%, ${C_FREE} ${e}% 100%)`;
      });
      const W = bar.querySelector(".st-cells").clientWidth, cw = (W - (M.cells - 1) * 2) / M.cells, t = end / M.cell, k = Math.floor(t), f = t - k;
      bar.querySelector(".st-knob").style.left = Math.round(Math.min(W, f === 0 && k > 0 ? k * (cw + 2) - 1 : k * (cw + 2) + f * cw)) + "px";
    }
    setAttr(bar, "aria-valuenow", v); setAttr(bar, "aria-valuetext", memTxt(v));
    // 步进：到 0.5 GB、到上限时那一边灰掉；超过上限、超过 16 GB 时数字变黄（照程序的两句提醒，进悬停）
    const st = r.querySelector(".st-mstp"), over = v > M.cap, big = v > 16384, [mi, pl] = st.querySelectorAll("button");
    setText(st.querySelector(".st-msv"), memTxt(v));
    mi.disabled = v <= 512; pl.disabled = v >= M.cap;
    st.classList.toggle("warn", over || big);
    const warn = (v % 512 ? `当前值 ${v} MB 非 0.5 GB 整数倍，调整时将对齐至相邻的 0.5 GB 档位\n` : "")
      + (over ? `超过当前可用内存 ${gbTxt(M.avail)}，可能导致卡顿或内存分配失败` : big ? "分配内存过大可能加剧 Java 垃圾回收（GC）停顿，导致游戏周期性掉帧。" : "");
    setTip(st, warn);
    setTip(st.querySelector(".st-msv"), warn + (warn && !warn.endsWith("\n") ? "\n" : "") + "点击输入数值（单位：GB）"); // 中间的数能点进去打字，它自己有悬停，上面那几句也带着
    setTip(pl, pl.disabled ? `已达当前可用上限 ${gbTxt(M.cap)}` : "增加 0.5 GB"); setTip(mi, over ? `降至可用上限 ${gbTxt(M.cap)}` : "减少 0.5 GB");
    // 图例：系统已占、分配（只有颜色，数在步进里）、剩余（= 可用 − 分配，和格子上淡的那一截一样长）
    setHTML(r.querySelector(".st-mleg"), `<span><i class="used"></i>系统已占 <b>${gs(M.used)}</b></span><span><i class="alloc"></i>分配</span><span><i class="free"></i>剩余 <b>${gs(Math.max(0, M.avail - v))}</b></span>`);
    r.querySelectorAll(".st-mp").forEach(b => b.classList.toggle("on", +b.dataset.v === v));
    // 预设那一排右边一句：超过上限（会改变结果，看得见）；上限比推荐的还低（同向导「可用内存不足」）
    const low = r.querySelector(".st-mlow"), want = C.S.proxy ? 1024 : 4096;
    const lowTxt = over ? "超过可用内存" : M.cap < want ? "可用内存不足" : "";
    if (low.dataset.t !== lowTxt) {
      low.dataset.t = lowTxt; low.innerHTML = lowTxt ? `${C.KIT.icon("warn")}${lowTxt}` : "";
      setTip(low, over ? `超过当前可用内存 ${gbTxt(M.avail)}，可能导致卡顿或内存分配失败\n关闭其他程序或服务器可释放更多内存；点击减号可快速降至上限 ${gbTxt(M.cap)}`
        : lowTxt ? `当前可用内存仅剩 ${gbTxt(M.avail)}，可分配上限为 ${gbTxt(M.cap)}，低于推荐值 ${gbTxt(want)}。\n关闭其他程序或服务器后重新打开本页可提升上限。` : "");
    }
    r.querySelector(".st-mem").classList.toggle("mod", v !== D.base.xmx);
  }
  function memAt(bar, x) {
    const M = MEM;
    if (M.blocks) { const a = bar.querySelector(".st-ba").getBoundingClientRect(); return memClamp((x - a.left) / a.width * M.avail); }
    const b = bar.querySelector(".st-cells").getBoundingClientRect(), cw = (b.width - (M.cells - 1) * 2) / M.cells, d = Math.max(0, x - b.left);
    const k = Math.floor(d / (cw + 2)), f = Math.min(1, (d - k * (cw + 2)) / cw);
    return memClamp((k + f) * M.cell - M.used);
  }
  // 一下：不是 0.5 GB 整数倍时，减号落到下面最近的一档、加号落到上面最近的一档；超过上限时减号一下回到上限
  const memStep = (v, d) => (d > 0 && v >= MEM.cap ? v : d < 0 && v > MEM.cap ? MEM.cap : d < 0 ? Math.max(512, Math.ceil(v / 512) * 512 - 512) : Math.min(MEM.cap, Math.floor(v / 512) * 512 + 512));
  // 步进里打的字：按 GB 认，也认带「GB」「MB」的写法（进框时写的就是步进上那几个字，3000 MB 还是「3000 MB」）。
  // 小于 0.5 GB、超过上限算填错（返回 NaN）；别的数同拖动一样四舍五入到 0.5 GB 的档上（这页的规矩：一动才落到档上）
  function memParse(raw) {
    const m = raw.replace(/\s/g, "").match(/^(\d+(?:\.\d+)?)(gb?|mb?)?$/i); if (!m) return NaN;
    const mb = +m[1] * (/^m/i.test(m[2] || "") ? 1 : 1024);
    return mb < 512 || mb > MEM.cap ? NaN : Math.round(mb / 512) * 512;
  }
  function setMem(D, v) {
    if (v === D.xmx) return;
    D.xmx = v; paintMem(D); paintAikar(D); paintPreview(D); paintBar(PG_JV);
  }
  function memDown(e) {
    const bar = e.target.closest(".st-mbar"); if (!bar || e.button !== 0) return;
    const D = jvData();
    e.preventDefault(); bar.focus({ preventScroll: true }); bar.setPointerCapture(e.pointerId); bar.classList.add("drag");
    const move = ev => setMem(D, memAt(bar, ev.clientX));
    const up = () => { bar.classList.remove("drag"); bar.removeEventListener("pointermove", move); bar.removeEventListener("pointerup", up); bar.removeEventListener("pointercancel", up); };
    move(e); bar.addEventListener("pointermove", move); bar.addEventListener("pointerup", up); bar.addEventListener("pointercancel", up);
  }

  // ---- Java、构建：自己画的下拉（框同输入框，点开是菜单） ----
  function paintJava(D) {
    const b = jvRoot()?.querySelector(".st-java"); if (!b) return;
    const o = D.javas[D.java], v = b.querySelector(".st-selv");
    const h = D.dl ? `${C.KIT.icon("spinner", "st-spin")}<span>正在下载 Java ${o.major}…</span>`
      : D.redl ? `${C.KIT.icon("spinner", "st-spin")}<span>${D.redl === "check" ? "正在检查" : "正在重新下载"} Java ${o.major}…</span>`
      : `<span>Java ${o.major}</span><span class="st-sub">${esc(javaSub(D, o, D.java).replace(" · 当前使用", ""))}</span>`;
    setHTML(v, h);
    b.classList.toggle("bad", (!!o.missing || !!o.broken || javaLow(D, o)) && !D.dl && !D.redl); setTip(b, D.redl ? "" : javaTip(D, o));
    jvRoot().querySelector(".st-javaf").classList.toggle("mod", D.java !== D.base.java);
  }
  function paintBuild(D) {
    const b = jvRoot()?.querySelector(".st-build"); if (!b) return;
    setHTML(b.querySelector(".st-selv"), `<span class="mono">${esc(buildName(D.build))}</span>${D.build === D.builds[0] ? `<span class="st-sub">最新</span>` : ""}`);
    jvRoot().querySelector(".st-buildf").classList.toggle("mod", D.build !== D.base.build);
  }
  // 坏了的那一项（检查不过）：字标红，右边一颗「重新下载」（点它马上重下，不用等保存；先下到旁边、检查过再换上去，同一个路径，服务器设置不动）
  const javaMenu = D => D.javas.map((o, i) => {
    const low = javaLow(D, o), off = o.missing || (low && i !== D.java), tip = javaTip(D, o);
    return `<div class="st-it${i === D.java ? " here" : ""}${o.missing ? " miss" : ""}${o.broken ? " broke" : ""}${low ? " low" : ""}" role="menuitemradio" aria-checked="${i === D.java}" data-v="${i}"`
      + `${off ? ' aria-disabled="true"' : ""}${tip ? ` title="${esc(tip)}"` : ""}><span class="st-im">Java ${o.major}</span><span class="st-is">${esc(javaSub(D, o, i))}</span>`
      + (o.broken ? `<button type="button" class="st-redl" data-act="redl" title="重新下载 Java ${o.major}" aria-label="重新下载 Java ${o.major}"><i data-ic="download"></i></button>`
        : o.download && !low ? `<i data-ic="download" class="st-dl"></i>` : "")
      + (i === D.java ? `<i data-ic="check" class="st-ck"></i>` : "") + `</div>`;
  }).join("");
  // 重新下载（草图：下 1.6 秒、检查 0.6 秒，好了那一项不再标红）
  function redownload(D, i) {
    const o = D.javas[i]; if (!o || D.redl) return;
    D.redl = "dl"; paintJava(D);
    setTimeout(() => { D.redl = "check"; paintJava(D); }, 1600);
    setTimeout(() => { o.broken = false; delete o.why; D.redl = ""; paintJava(D); }, 2200);
  }
  const buildMenu = D => D.builds.map((b, i) => `<div class="st-it${b === D.build ? " here" : ""}" role="menuitemradio" aria-checked="${b === D.build}" data-v="${esc(b)}">`
    + `<span class="st-im mono">${esc(buildName(b))}</span><span class="st-is">${[i === 0 ? "最新" : "", b === D.base.build ? "当前使用" : ""].filter(Boolean).join(" · ")}</span>`
    + (b === D.build ? `<i data-ic="check" class="st-ck"></i>` : "") + `</div>`).join("");

  // ---- JVM 参数：一行一个；加的、改的那一行有小点；整块的改动算保存条上的一项（同程序：参数整个比一次） ----
  const listEl = () => jvRoot()?.querySelector(".st-jlw .list");
  function paintArgsMod(D) { listEl()?.querySelectorAll(".st-jr").forEach(el => { const a = D.args.find(x => x.id === +el.dataset.id); if (a) el.classList.toggle("mod", isNewArg(D, a.t)); }); }
  function paintArgsUI(D) {
    const r = jvRoot(); if (!r) return;
    r.querySelector(".st-jh").classList.toggle("mod", argStr(jvArgs(D)) !== argStr(D.base.args));
    r.querySelector(".st-jlw").classList.toggle("is-none", !D.args.length);
    const clr = r.querySelector('[data-st="clear"]'), none = !D.args.some(a => !LOG4J_FIX.includes(a.t));
    setAttr(clr, "aria-disabled", none); setTip(clr, none ? "暂无可清空的参数" : "清空");
    paintAikar(D); r.querySelector(".st-jlw")._paint?.();
  }
  // 「Aikar's Flags」：已经是这一套（按现在的内存挑的那套）时 ＋ 换成勾、淡下去（同「定时任务」的预设）
  function paintAikar(D) {
    const b = jvRoot()?.querySelector(".st-aik"); if (!b) return;
    const same = argStr(jvArgs(D).filter(t => !LOG4J_FIX.includes(t))) === argStr(aikars(D.xmx));
    setTip(b, same ? "已套用当前内存对应的 Aikar's Flags 参数" : "套用 Aikar's Flags（G1GC 优化）\n将替换下方全部自定义参数");
    if (b.classList.contains("has") === same) return;
    b.classList.toggle("has", same); b.querySelector("svg").outerHTML = C.KIT.icon(same ? "check" : "plus");
  }
  function argsChanged(D) { paintArgsUI(D); paintPreview(D); paintBar(PG_JV); }
  // 新的一行：先撑开高度，再从下面 5 像素淡进来；拿掉一行：往右划 32 像素淡掉再收起（同「定时任务」「玩家」）
  function growRow(el, i = 0) {
    const h = el.offsetHeight + "px";
    el.animate([{ height: "0px", marginTop: "-2px", opacity: 0, transform: "translateY(5px)", easing: SLIDE },
      { height: h, marginTop: "0px", opacity: 0, transform: "translateY(5px)", offset: .4, easing: EASE_OUT },
      { height: h, marginTop: "0px", opacity: 1, transform: "none" }], { duration: 360, delay: i * 22, fill: "backwards" }).onfinish = () => jvRoot()?.querySelector(".st-jlw")._paint?.();
  }
  function dropRow(el, delay = 0, dx = 32) {
    el.classList.add("st-out"); const h = el.offsetHeight + "px";
    el.animate([{ opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: `translateX(${dx}px)`, height: h, marginBottom: "0px", offset: .55, easing: SLIDE },
      { opacity: 0, transform: `translateX(${dx}px)`, height: "0px", marginBottom: "-2px" }], { duration: 320, delay, fill: "forwards" }).onfinish = () => { el.remove(); jvRoot()?.querySelector(".st-jlw")._paint?.(); };
  }
  function addArgs(D, list) {
    const box = listEl(); const born = [];
    list.forEach(t => {
      const had = D.args.find(a => a.t === t);
      if (had) { flashRow(had.id); return; } // 已经有了：不重复加，那一行闪一下
      const a = { id: ++seqId, t }; D.args.push(a);
      const el = tpl(argRow(D, a)); box.appendChild(el); C.KIT.hydrate(el); born.push(el);
    });
    born.forEach((el, i) => growRow(el, i));
    if (born.length) setTimeout(() => box.scrollTo({ top: box.scrollHeight, behavior: "smooth" }), 60);
    argsChanged(D);
  }
  function flashRow(id) {
    const el = listEl()?.querySelector(`.st-jr[data-id="${id}"]:not(.st-out)`); if (!el) return;
    el.scrollIntoView({ block: "nearest", behavior: "smooth" }); el.classList.remove("st-flash"); void el.offsetWidth; el.classList.add("st-flash");
  }
  // 清空：从上往下一行接一行往右划 40 像素，间隔从 90 毫秒起每次缩成四分之三（规范第七节「清单加减」）；log4shell 缓解参数留着（同程序）
  function clearArgs(D) {
    const gone = D.args.filter(a => !LOG4J_FIX.includes(a.t)); if (!gone.length) return;
    D.args = D.args.filter(a => LOG4J_FIX.includes(a.t));
    let t = 0, gap = 90;
    gone.forEach(a => { const el = listEl()?.querySelector(`.st-jr[data-id="${a.id}"]`); if (el) { dropRow(el, t, 40); t += gap; gap = Math.max(12, gap * .75); } });
    argsChanged(D);
  }
  // 套用 Aikar's Flags：整份换（同程序），旧的整块往上淡掉，新的一行一行从下面淡进来（规范「一批换」）
  function applyAikar(D) {
    const keep = D.args.filter(a => LOG4J_FIX.includes(a.t)).map(a => a.t);
    D.args = [...aikars(D.xmx), ...keep].map(t => ({ id: ++seqId, t }));
    const box = listEl(), tok = box._tok = (box._tok || 0) + 1;
    box.getAnimations({ subtree: true }).forEach(a => a.cancel());
    const out = box.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" });
    out.onfinish = () => {
      if (box._tok !== tok) return;
      box.innerHTML = D.args.map(a => argRow(D, a)).join(""); C.KIT.hydrate(box); box.scrollTop = 0; out.cancel();
      const bottom = box.getBoundingClientRect().bottom;
      [...box.children].filter(x => x.getBoundingClientRect().top < bottom).forEach((x, i) => rise(x, i));
      argsChanged(D);
    };
    paintAikar(D); paintBar(PG_JV); jvRoot().querySelector(".st-jh").classList.add("mod");
  }
  // 参数对不对：不能有双引号（同后端 validateJVMArgs）；内存参数不在这里填（后端会照收，但会盖掉左边的内存，所以这里拦下）
  const argBad = t => (t.includes('"') ? ["不可含双引号", "JVM 参数不可包含双引号"] : MEM_FLAG.test(t) ? ["请在左侧设置内存", "-Xmx、-Xms 由左侧内存分配自动生成，无需在此填写"] : "");
  const splitArgs = v => v.split(/\s+/).map(s => s.trim()).filter(Boolean);
  function addFromBox(D) {
    const r = jvRoot(), iw = r.querySelector(".st-jadd"), inp = iw.querySelector("input"), list = splitArgs(inp.value);
    if (!list.length) { iw.classList.add("bad"); setMsg(iw, ["请填写参数", "请输入 JVM 参数，如 -XX:+UseG1GC"]); shake(iw); inp.focus(); return; }
    const bad = list.map(argBad).find(Boolean);
    if (bad) { iw.classList.add("bad"); setMsg(iw, bad); shake(iw); inp.focus(); return; }
    inp.value = D.form = ""; addArgs(D, list);
  }
  const setMsg = (iw, m) => { const s = iw.querySelector(".st-msg"); s.textContent = m ? m[0] : ""; setTip(s, m ? m[1] : ""); };
  // 点一行就地改：那一行变成框（同「全部配置」的值：平时是字、点进去出框）；回车或离开时存这一行，清空了就是删掉，Esc 不改
  function editRow(D, el) {
    if (el.querySelector(".st-jin")) return;
    const a = D.args.find(x => x.id === +el.dataset.id); if (!a) return;
    const inp = document.createElement("input"); inp.className = "st-jin"; inp.value = a.t; inp.spellcheck = false; inp.autocomplete = "off"; inp.setAttribute("aria-label", "编辑 JVM 参数");
    el.querySelector(".st-jt").replaceWith(inp); el.classList.add("edit"); inp.focus(); inp.select();
  }
  function commitRow(D, inp, cancel) {
    const el = inp.closest(".st-jr"); if (!el || el._done) return;
    const a = D.args.find(x => x.id === +el.dataset.id); if (!a) return;
    const list = cancel ? [a.t] : splitArgs(inp.value), bad = list.map(argBad).find(Boolean);
    if (bad) { el.classList.add("bad"); setTip(inp, bad[1]); shake(el); inp.focus(); return false; }
    el._done = true;
    if (!list.length) { D.args = D.args.filter(x => x !== a); dropRow(el); argsChanged(D); return; }
    a.t = list[0];
    const span = document.createElement("span"); span.className = "st-jt"; span.textContent = a.t; span.title = a.t;
    inp.replaceWith(span); el.classList.remove("edit", "bad"); el._done = false;
    if (list.length > 1) { // 一格里写了几个：第一个留在这一行，其余的接在它后面
      const i = D.args.indexOf(a), more = list.slice(1).filter(t => !D.args.some(x => x.t === t)).map(t => ({ id: ++seqId, t }));
      D.args.splice(i + 1, 0, ...more);
      let prev = el; more.forEach((m, j) => { const n = tpl(argRow(D, m)); prev.after(n); C.KIT.hydrate(n); growRow(n, j); prev = n; });
    }
    paintArgsMod(D); argsChanged(D);
  }

  // ---- 实际启动参数（程序是页上一直摆着的一块「实际启动参数预览」）：点开一块浮层，内容跟着改动现算（后端用同一套函数算，这里照 launchArgs 拼） ----
  function launchLine(D) {
    const xmx = D.xmx, target = C.kind === "forge" ? [`@libraries/net/minecraftforge/forge/${C.S.mc}-${D.build}/win_args.txt`] : ["-jar", "server.jar"];
    return ["java", `-Xms${Math.min(1024, xmx)}M`, `-Xmx${xmx}M`, "-Dfile.encoding=UTF-8", "-Dstdout.encoding=UTF-8", "-Dstderr.encoding=UTF-8", ...jvArgs(D), ...target, "nogui"];
  }
  // 一个参数一块，只在参数之间折行（不在「-D」的横线后面断开）；一个参数比一行还长时才在里面断
  const lineHTML = D => launchLine(D).map(t => `<span class="st-a">${esc(t)}</span>`).join(" ");
  const previewHTML = D => `<div class="st-pvh" title="保存后写入启动脚本并用于启动的参数（旧版本所需的 log4shell 防护参数由后端自动补全）">实际启动参数</div>`
    + `<div class="st-pvw"><div class="list st-pvt">${lineHTML(D)}</div><span class="thumb"></span></div>`;
  function paintPreview(D) {
    if (!pop || !pop.classList.contains("st-pvp")) return;
    if (setHTML(pop.querySelector(".st-pvt"), lineHTML(D))) { pop.querySelector(".st-pvw")._paint?.(); placePop(); }
  }

  // ---- 事件 ----
  function jvClick(e) {
    const D = jvData();
    const t = e.target.closest(".st-jt"); if (t) { editRow(D, t.closest(".st-jr")); return; }
    const b = e.target.closest("[data-st]"); if (!b || b.getAttribute("aria-disabled") === "true" || b.disabled) return;
    switch (b.dataset.st) {
      case "m-": case "m+": setMem(D, memStep(D.xmx, b.dataset.st === "m+" ? 1 : -1)); break;
      case "mp": setMem(D, +b.dataset.v); break;
      case "java": if (!D.dl && !D.redl) openMenu(b, javaMenu(D), v => { D.java = +v; paintJava(D); paintBar(PG_JV); }, (act, v) => { if (act === "redl") redownload(D, +v); }); break;
      case "build": openMenu(b, buildMenu(D), v => { D.build = v; paintBuild(D); paintPreview(D); paintBar(PG_JV); }); break;
      case "copy": { // 复制以后图标换成勾 1.2 秒（规范第七节）
        navigator.clipboard?.writeText(D.dir).catch(() => {});
        b.classList.add("done"); b.innerHTML = C.KIT.icon("check");
        clearTimeout(b._t); b._t = setTimeout(() => { b.classList.remove("done"); b.innerHTML = C.KIT.icon("copy"); }, 1200); break;
      }
      case "port": { // 去「设置 → 常用」，光标落在端口上（同程序）：点左边标签栏里的子页，外壳照常换内容。
        // 这页有没保存的改动时外壳会先问、可能被取消，取消了外壳不会告诉这里；所以自己先问这一句（同一个 KIT.leave），
        // 过了才记下 cmFocus——到外壳再查时已经干净，不会问第二遍。不然取消以后 cmFocus 留着，下次进「常用」会莫名落到端口上
        closeMenu(); closePop();
        const s = document.querySelector('#snav .subs [data-s="common"]'); if (!s) break;
        C.KIT.leave(window.DETAIL_TABS["settings/jvm"]).then(ok => { if (ok) { cmFocus = "server-port"; s.click(); } });
        break;
      }
      case "aikar": if (!b.classList.contains("has")) applyAikar(D); break;
      case "clear": clearArgs(D); break;
      case "preview": openPop(b, previewHTML(D), "st-pvp", p => C.thinBar(p.querySelector(".st-pvw"))); break;
      case "jadd": addFromBox(D); break;
      case "jdel": { const el = b.closest(".st-jr"), a = D.args.find(x => x.id === +el.dataset.id); D.args = D.args.filter(x => x !== a); dropRow(el); argsChanged(D); break; }
      case "discard": jvDiscard(D); break;
      case "save": save(PG_JV); break;
      case "restart": restart(PG_JV); break;
    }
  }
  // 保存前：有一行参数正改着、填得不对（标着红）就拦下——那一行晃一下、光标落回去（同「常用」保存时的拦法）
  function jvCheck() {
    const el = jvRoot()?.querySelector(".st-jr.bad"); if (!el) return true;
    el.scrollIntoView({ block: "nearest" }); shake(el); el.querySelector(".st-jin")?.focus({ preventScroll: true });
    return false;
  }
  function jvDiscard(D) {
    D.xmx = D.base.xmx; D.java = D.base.java; D.build = D.base.build;
    if (argStr(jvArgs(D)) !== argStr(D.base.args)) {
      D.args = D.base.args.map(t => ({ id: ++seqId, t }));
      const box = listEl(); box.innerHTML = D.args.map(a => argRow(D, a)).join(""); C.KIT.hydrate(box);
      [...box.children].forEach((x, i) => rise(x, Math.min(i, 14)));
    }
    paintMem(D); paintJava(D); paintBuild(D); paintArgsUI(D); paintPreview(D); paintBar(PG_JV);
  }
  function jvInput(e) {
    const iw = e.target.closest(".st-jadd");
    if (iw) { jvData().form = e.target.value; if (iw.classList.contains("bad")) { iw.classList.remove("bad"); setMsg(iw, ""); } return; }
    if (e.target.classList.contains("st-jin")) { const el = e.target.closest(".st-jr"); if (el.classList.contains("bad")) { el.classList.remove("bad"); setTip(e.target, ""); } }
  }
  function jvKey(e) {
    const D = jvData();
    if (e.target.closest(".st-jadd") && e.key === "Enter" && !e.isComposing) { e.preventDefault(); addFromBox(D); return; }
    if (e.target.classList.contains("st-jin")) {
      if (e.key === "Enter" && !e.isComposing) { e.preventDefault(); commitRow(D, e.target); }
      else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); commitRow(D, e.target, true); }
      return;
    }
    if (e.target.classList.contains("st-mbar")) { // 方向键一次 0.5 GB，Home 最少、End 最多（同向导）
      const map = { ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 };
      if (e.key in map) { e.preventDefault(); setMem(D, memStep(D.xmx, map[e.key])); }
      else if (e.key === "Home") { e.preventDefault(); setMem(D, 512); }
      else if (e.key === "End") { e.preventDefault(); setMem(D, MEM.cap); }
    }
  }
  function jvBlur(e) { if (e.target.classList?.contains("st-jin")) commitRow(jvData(), e.target); }

  window.DETAIL_TABS["settings/common"] = { html: cmHTML, mount: cmMount, ...leaveHooks(PG_CM) };
  window.DETAIL_TABS["settings/jvm"] = { html: jvHTML, mount: jvMount, ...leaveHooks(PG_JV) };
})();
