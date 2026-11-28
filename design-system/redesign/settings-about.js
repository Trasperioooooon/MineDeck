// 全局设置 →「关于与其他」（11-28 作者定：「启动与托盘」「通知与集成」「更新与关于」合成一页，命名为「关于与其他」；分类 id 沿用 about，图标沿用 info）。样式在 settings-about.css。
// 三页的第一稿是 11-27 夜里子代理照临时规范画的，原来各一份 settings-startup / settings-notify / settings-about，合并以后只剩这一份。
//
// 程序现在是三个分类（internal/web/ui/app.js）：
//   · 「更新与关于」（约 6796 行 setAbout）：页顶 Logo、「MineDeck」「Windows 本地 Minecraft 开服工具 · v3.2.9」；
//     「版本与更新」一组：当前版本（徽章 v3.2.9）、更新仓库（框 +「保存」+「检查更新」）、启动时静默检查更新（开关）；启动时查到新版本时一条「启动时查到新版本 vX [查看]」；
//     检查结果一行（已是最新 / 发现新版本 vX（正式版）+「立即更新并重启」）；「更新渠道」三行单选（跟随当前版本（默认）/ 稳定版 / 最新版，选了马上存，清掉上一次的结果）；
//     「关于」一段字（OpenFrp、字体许可、AGPL、Mojang 声明）；最底下「退出程序」+「停止服务器并退出」（确认框）。侧栏底部还有版本号和「退出 MineDeck」（U04：以后不要了，挪到这里）。
//     「立即更新并重启」在后端（ops/catalog/appops.go app.update.apply）是一步做完：有服务器开着就拒绝（「服务器「…」仍在运行，请先停止所有服务器再更新」），
//     再查一次、下载、校验、替换、重启，界面整页换成「正在更新，程序将自动重启…」；没有下载进度，也没有「下载好了等重启」这一步。
//   · 「启动与托盘」（约 5856 行 setStartup）：页顶标题和一句「关闭按钮的行为，以及开机是否自动启动。」，一组两行（行首图标瓦片、标题、说明，右边开关）：
//     「点关闭按钮时缩到托盘（不退出）」（config.minimizeToTray）、「开机自动启动」（info.autoStart，写注册表）。两个开关都是点了即时保存（PUT /api/config），
//     成功提示「已开启 / 已关闭」，失败提示「无法开启「…」」并把开关拨回去。
//   · 「通知与集成」（约 6213 行 setNotify）：页顶标题和一句说明（「服务器启停 / 崩溃、玩家进出、备份完成、隧道上线等事件，可以推送到群机器人等外部服务。」），
//     一组一行：「Webhook 事件推送（选填）」，说明「有事件时，会向这个地址 POST 一条 JSON。留空就是关闭。」，地址框（占位「https://example.com/hook（留空关闭）」）+「保存」。
//     保存是 bindSaveInput：改过了「保存」才能点，回车也能存；存好了提示「Webhook 已保存，即时生效」，存空的提示「已关闭 Webhook 推送」。
//     后端（ops/catalog/appops.go）不收：不是 http(s):// 开头的（「Webhook 地址需以 http(s):// 开头」）、没有主机名的（「Webhook 地址的格式不对，要写成 https://主机名/路径 这样」）。
//     从局域网打开时只给地址的开头，后面换成 ***（maskWebhook），说明里多一句「从局域网打开时只显示地址的开头……」，点进框就全选；原样存回去算没改，
//     改了一半还带着 *** 的不收（「Webhook 地址里的 *** 是藏起来的部分，要改就把整个地址重新填一遍」）。这一分类程序里就这一项：没有按事件分的开关，也没有「发一条测试」，这里照程序，不加。
//
// 这里（内容面板 868 × 658，里面 852 × 642）：版面以原来「更新与关于」为底，左右两栏（圆角 18 = 26 − 8），左栏占其余、右栏 340 宽；右栏「关于」原样。
//   · 左栏从上往下：
//     1. Logo 48、名字和一句「Windows 本地 Minecraft 开服工具」。
//     2. 版本块 64 高、圆角 10——版本号只在这里写一次（程序页顶、「当前版本」两处都写了），下面一行是状态，右边是要做的那一步：
//        检查更新 → 检查中 → 已是最新 / 发现新版本 vX（正式版）→ 下载更新 → 正在下载（块底一条细进度条和 MB 数）→ vX 已下载，重启后完成更新 →「重启并更新」。
//        换状态只换那一行字和右边那颗键（逐项换），进度只改数和条。
//     3. 更新渠道（40 高三段，各自的说明进悬停）。程序里的「更新仓库」框不在这里：更新从哪里下载挪到了「下载与网络」的「软件更新」（11-28 作者问起）。
//   · 11-29 作者：合并了也要适当区分，不能只是连在一起。左栏 Logo 下面照原来的三个分类分成三块，各带一个小标题（13.5 号、加粗、正文色，比「更新渠道」这种
//     淡字标签高一级），块和块之间一道细线：「版本与更新」（版本块、更新渠道、启动时静默检查更新）、「启动与托盘」（两个开关）、「通知与集成」（Webhook）。
//     右栏「关于」的标题用同一种小标题。下面 2～5 条说的是各块里的东西。
//     4. 一组三个开关，同一种样子（36 高一行，开关 36 × 20 在字的左边，字只写名字，说明进整行的悬停；只有开关本身能点，kit.js 统一处理）：
//        「启动时静默检查更新」，多隔 8（开关之间看着是 24，同块和块的间距），再是从「启动与托盘」搬来的「关闭窗口时最小化到托盘（不退出）」「开机自动启动」。都是点了马上拨过去（同程序：即时保存），不弹提示。
//        没写成功时开关拨回去、那一行晃一下，行尾一句短的红字，整句进悬停（规范第七节「反馈」）。托盘那一行的悬停里说退出的办法：
//        程序写「点左下角的「退出」」，侧栏没有了（计划书 U04），这里写「右键托盘图标，或在本页底部点击「退出 MineDeck」」。
//     5. 「Webhook 事件推送（选填）」，从「通知与集成」搬来：名字一行（程序页顶那句、那一行的说明进它的悬停），下面地址框占满栏宽（等宽字），
//        框里右边一颗保存（同详情页「扩展」单字段的框：没改时灰着，改过了亮起来，回车也能存，存好了换成勾 1.2 秒，不弹提示）。
//        后端不收的照规范第七节「反馈」就地拦下：框标红、晃一下、光标落过去，保存的位置换成一句短的红字，后端的原话进悬停。
//     6. 最下面一颗「退出 MineDeck」贴底（危险色浅底红字，同首页大卡的停止），点了是确认框，确认了整窗口一层「正在停止服务器并退出」（同共用件页的整页状态）。
//   · 右栏「关于」：程序那段字一条一块，原文照留（字体许可、AGPL、Mojang 声明是义务，不收进悬停）；外链只放图标（规范第十节），地址在悬停里。
// 任务单要的、程序没有的两种状态（要改后端，写进清单）：「正在下载」「下载好了等重启」。有服务器开着时点「重启并更新」：程序是拒绝，
// 这里改成一个确认框列出要停的服务器，确认了先停服再更新（同「退出」的做法）；要不要这样等作者定。左边分类上的小点留到装好新版本才去掉（程序是点过「查看」就去掉）。
// 离开前拦（作者 11-28 定，外壳统一拦，这里只登记 dirty / save / discard）：这一页只有 Webhook 地址要点保存，改了没存就离开先问；
// 选「保存」时后端不收的照样就地拦下、留在这页。更新渠道和三个开关都是点了就生效，不算未保存。
// 示例数据：现在 v3.2.9 正式版（同 internal/app/app.go）；新版本 v3.3.0、38.6 MB 是草图编的；渠道没选过（跟随当前版本）；
// 静默检查开着、最小化到托盘开着、开机自启关着（任意挑的）；Webhook 存着 https://example.com/hook（示例地址，不写真实的服务地址）。
// 草图专用：外壳的 &update=1 是启动时查到新版本；&up=downloading 一打开就在下载，&up=ready 一打开就是下载好了等重启；
// &fail=1 让「开机自动启动」打开时写注册表失败（看拦下来的样子）；&from=lan 看从局域网打开时的 Webhook（地址是 https://example.com/***，点进框全选，悬停多一句）。
// 外链在草图里不跳转（悬停看地址），两份许可原文能打开。没画的：Webhook 读取失败。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }], { duration: 360, easing: SLIDE });
  const rise = (el, i = 0) => el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" });
  const setTip = (el, t) => { if (!el || (el.getAttribute("title") ?? el.dataset.tip ?? "") === (t || "")) return; if (t) el.title = t; else el.removeAttribute("title"); delete el.dataset.tip; };

  const CH = { // 同程序 UPDATE_CH
    auto: { l: "跟随当前版本", t: "跟随当前版本（默认）：当前为正式版时仅接收正式版更新，当前为测试版时同时接收测试版；切回正式版后自动恢复仅接收正式版。" },
    stable: { l: "稳定版", t: "稳定版（仅正式版）：仅提示正式发布版本。" },
    latest: { l: "最新版", t: "最新版（含测试版）：同时接收正式版与测试版更新，优先提示最新版本。测试版可能存在未知问题。" },
  };
  const NEW = "3.3.0", SIZE = 38.6; // 草图编的
  let A = null, W = null, N = null, C = null; // A：版本和更新；W：三个开关；N：Webhook。换分类回来不丢
  const effCh = () => A.ch || (/-/.test(A.ver) ? "latest" : "stable"); // 没选过：按正在用的版本定（同程序）
  const chName = c => ({ stable: "稳定版", latest: "最新版" })[c];
  function seed(ctx) {
    if (A) return;
    const up = ctx.QS.get("up"), has = ctx.update || up === "downloading" || up === "ready";
    A = { ver: "3.2.9", has, st: !has ? "idle" : up === "downloading" ? "downloading" : up === "ready" ? "ready" : "found",
      got: up === "downloading" ? 9.8 : 0, ch: "" };
    W = { silent: true, tray: true, auto: false, fail: ctx.QS.get("fail") === "1" };
    const lan = ctx.QS.get("from") === "lan", real = "https://example.com/hook";
    N = { lan, saved: lan ? maskOf(real) : real, draft: null }; // saved：框里该有的（局域网那边就是遮掉的样子）；draft：改了还没存的
    if (A.st === "downloading") runDl();
  }

  // ---- 三个开关：点了马上拨过去（同程序：即时保存）。sep：和上一行多隔 8（上面一个管更新，下面两个管窗口和开机） ----
  const SWS = [
    { k: "silent", label: "启动时静默检查更新", tip: "程序启动后将在后台按所选更新渠道检查一次新版本" },
    { k: "tray", label: "关闭窗口时最小化到托盘（不退出）",
      tip: "开启后，点击关闭按钮将最小化至系统托盘，程序与服务器继续运行；点击托盘图标可恢复窗口（首次最小化时将发送一次系统通知）。"
        + "如需退出，可右键托盘图标，或在本页底部点击「退出 MineDeck」。关闭后，点击关闭按钮将停止服务器并退出程序（有运行中的服务器时会先确认）。" },
    { k: "auto", label: "开机自动启动",
      tip: "登录 Windows 后自动在系统托盘静默启动，无需管理员权限（若移动过程序文件夹，需重新切换此开关以更新启动路径）。" },
  ];
  const swHTML = r => `<button type="button" class="sab-swr${r.sep ? " sep" : ""}${W[r.k] ? " on" : ""}" data-sab="sw" data-k="${r.k}" role="switch" aria-checked="${W[r.k]}" title="${esc(r.tip)}">`
    + `<span class="sab-sw"></span><span class="sab-sl">${esc(r.label)}</span><span class="sab-sm"></span></button>`;
  const paintSw = el => { const on = W[el.dataset.k]; el.classList.toggle("on", on); el.setAttribute("aria-checked", on); };
  function toggleSw(el) {
    const k = el.dataset.k, want = !W[k], label = SWS.find(r => r.k === k).label;
    el.classList.remove("bad"); setTip(el.querySelector(".sab-sm"), "");
    W[k] = want; paintSw(el); // 同程序：先拨过去，写不成再拨回来
    if (W.fail && k === "auto" && want) {
      setTimeout(() => {
        W[k] = false;
        const cur = root()?.querySelector(`.sab-swr[data-k="${k}"]`); if (!cur) return;
        paintSw(cur);
        const m = cur.querySelector(".sab-sm"); m.textContent = "开启失败"; setTip(m, `无法开启「${label}」：写入开机启动项失败`);
        cur.classList.add("bad"); shake(cur);
      }, 420); // 装作等了一下请求
    }
  }

  // ---- Webhook 地址：这一页唯一要点保存的项 ----
  const MASK = "***"; // 同后端 webhookMask
  const HOOK_TIP = "支持将服务器启停 / 崩溃、玩家进出、备份完成、隧道上线等事件推送至群机器人等外部服务。\n触发事件时将向该地址 POST 一条 JSON；留空即关闭推送。";
  const HOOK_TIP_LAN = "\n通过局域网访问时仅显示地址前缀（路径与参数已隐藏），如需修改请重新填写完整地址。";
  // 同后端 maskWebhook：只留协议和主机，后面换成 ***
  function maskOf(u) { if (!u) return ""; try { const p = new URL(u); return p.protocol + "//" + p.host + "/" + MASK; } catch { return MASK; } }
  const hookDirty = () => !!N && N.draft !== null && N.draft !== N.saved;
  const hookBox = () => root()?.querySelector(".sab-in");
  function hookHTML() {
    const v = N.draft ?? N.saved, dirty = hookDirty();
    return `<div class="sab-f"><div class="sab-lb"><span title="${esc(HOOK_TIP + (N.lan ? HOOK_TIP_LAN : ""))}">Webhook 事件推送（选填）</span></div>`
      + `<label class="sab-in${dirty ? " dirty" : ""}"><input value="${esc(v)}" placeholder="https://example.com/hook（留空关闭）" spellcheck="false" autocomplete="off" aria-label="Webhook 地址">`
      + `<span class="sab-ie"></span><button type="button" class="sab-ib" data-sab="hook" title="保存"${dirty ? "" : " disabled"}><i data-ic="save"></i></button></label></div>`;
  }
  function paintHook() {
    const box = hookBox(); if (!box) return;
    const dirty = hookDirty(), b = box.querySelector(".sab-ib");
    box.classList.remove("bad"); box.classList.toggle("dirty", dirty);
    if (dirty) untick(b); // 勾还没收、又改了：马上换回保存
    b.disabled = !dirty;
  }
  // 存好了：保存图标换成勾 1.2 秒（规范第七节）
  function tick(b) {
    untick(b);
    b._old = b.innerHTML; b.classList.add("done"); b.innerHTML = C.KIT.icon("check");
    b._t = setTimeout(() => untick(b), 1200);
  }
  function untick(b) {
    if (!b._t) return;
    clearTimeout(b._t); b._t = 0; b.classList.remove("done"); b.innerHTML = b._old;
  }
  // 后端 url.Parse 以后要有主机名（webhookURLOK）。浏览器的 URL 会把 https:///x 认成主机 x，所以照 Go 的读法自己取：// 后面到第一个 / ? # 为止，去掉账号和端口
  function hostOf(u) {
    const m = u.match(/^https?:\/\/([^/?#]*)/i); if (!m) return "";
    const hp = m[1].split("@").pop();
    return hp.startsWith("[") ? hp.slice(1, hp.indexOf("]") > 0 ? hp.indexOf("]") : undefined) : hp.split(":")[0];
  }
  function hookBad(box, short, full) {
    box.classList.add("bad");
    const m = box.querySelector(".sab-ie"); m.textContent = short; setTip(m, full);
    shake(box); box.querySelector("input")?.focus();
  }
  // 照后端 appops.go 那几道：草图里不发请求，改这里的数据、换框的样子。返回存成了没有（被拦下是 false）：外壳「离开前拦」选了保存时看它
  function saveHook() {
    const box = hookBox(); if (!box) return true;
    const inp = box.querySelector("input"), u = inp.value.trim();
    if (N.draft === null || inp.value === N.saved) return true; // 没改：保存灰着，回车也不存（同 bindSaveInput）
    if (N.lan && u.includes(MASK)) { hookBad(box, "请填写完整地址", "地址中的 *** 为隐藏部分，修改时请重新填写完整地址"); return false; }
    if (u && !/^https?:\/\//.test(u)) { hookBad(box, "需以 http(s):// 开头", "Webhook 地址需以 http(s):// 开头"); return false; }
    if (u && (!hostOf(u) || /\s/.test(hostOf(u)))) { hookBad(box, "地址格式无效", "Webhook 地址格式无效，请按 https://主机名/路径 格式填写"); return false; }
    N.saved = N.lan ? maskOf(u) : u; N.draft = null; // 存空的就是关掉推送（程序提示「已关闭 Webhook 推送」，这里框空着、占位字写着「留空关闭」）
    if (inp.value !== N.saved) inp.value = N.saved;
    const b = box.querySelector(".sab-ib");
    box.classList.remove("dirty", "bad"); b.disabled = true;
    tick(b);
    return true;
  }
  // 放弃没存的地址：草稿丢掉，框里换回存过的
  function discardHook() {
    if (!N) return; N.draft = null;
    const inp = hookBox()?.querySelector("input"); if (inp) inp.value = N.saved;
    paintHook();
  }

  // ---- 版本块：一行状态、右边那一步 ----
  function stateHTML() {
    const head = `当前渠道（${chName(effCh())}）：`;
    switch (A.st) {
      case "checking": return `<span class="sab-sx"><i data-ic="spinner" class="sab-spin"></i>正在检查…</span>`;
      case "latest": return `<span class="sab-sx ok" title="${esc(`${head}已是最新版本（v${A.ver}，正式版）`)}"><i data-ic="check"></i>已是最新</span>`;
      case "found": return `<span class="sab-sx new" title="${esc(`${head}发现新版本 v${NEW}（正式版），当前版本 v${A.ver}`)}">发现新版本 v${NEW}（正式版）</span>`;
      case "downloading": return `<span class="sab-sx">正在下载 v${NEW}</span><span class="sab-mb"><b class="sab-got"></b> / ${SIZE} MB</span>`;
      case "ready": return `<span class="sab-sx new" title="已下载并通过校验，重启 MineDeck 时将应用新版本">v${NEW} 已下载，重启后完成更新</span>`;
      default: return `<span class="sab-sx">正式版</span>`;
    }
  }
  function actHTML() {
    if (A.st === "found") return `<button type="button" class="sab-btn pri" data-sab="dl"><i data-ic="download"></i>下载更新</button>`;
    if (A.st === "ready") return `<button type="button" class="sab-btn pri" data-sab="restart"><i data-ic="refresh"></i>重启并更新</button>`;
    if (A.st === "downloading") return "";
    const busy = A.st === "checking";
    return `<button type="button" class="sab-btn" data-sab="check"${busy ? " disabled" : ""}><i data-ic="refresh"${busy ? ' class="sab-spin"' : ""}></i>检查更新</button>`;
  }
  // 逐项换：旧的往上 5 像素淡掉（0.09 秒），新的从下面 5 像素淡进来（0.22 秒、错开 22 毫秒）；连着变时上一次作废
  function swap(el, html, anim) {
    if (el._h === html) return; el._h = html;
    const tok = el._tok = (el._tok || 0) + 1;
    const show = () => { if (el._tok !== tok) return; el.getAnimations().forEach(x => x.cancel()); el.innerHTML = html; C.KIT.hydrate(el); paintGot(); if (anim) [...el.children].forEach(rise); };
    if (!anim || !el.children.length) { show(); return; }
    el.getAnimations({ subtree: true }).forEach(x => x.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = show;
  }
  function paintGot() {
    const r = root(); if (!r) return;
    const g = r.querySelector(".sab-got"); if (g) g.textContent = A.got.toFixed(1);
    const bar = r.querySelector(".sab-bar"); bar.classList.toggle("on", A.st === "downloading"); bar.firstElementChild.style.width = (A.got / SIZE * 100).toFixed(2) + "%";
  }
  function paintUp(anim = true) {
    const r = root(); if (!r) return;
    const v = r.querySelector(".sab-v"), t = "v" + A.ver; if (v.textContent !== t) v.textContent = t;
    swap(r.querySelector(".sab-s"), stateHTML(), anim); swap(r.querySelector(".sab-act"), actHTML(), anim); paintGot();
  }
  function runDl() {
    clearInterval(A.timer);
    A.timer = setInterval(() => {
      A.got = Math.min(SIZE, A.got + 1.3); paintGot();
      if (A.got >= SIZE) { clearInterval(A.timer); A.st = "ready"; paintUp(); }
    }, 120);
  }
  function check() {
    if (A.st === "checking") return;
    A.st = "checking"; paintUp();
    setTimeout(() => { A.st = A.has ? "found" : "latest"; paintUp(); }, 800); // 装作等了一下请求
  }

  // ---- 确认框用共用的 KIT.confirm（规范第九节，11-28 换掉了这页自己那一套）；整窗口一层（同共用件页的整页状态）挂在 body 上 ----
  const ask = async ({ ic, title, body, ok, onOk }) => {
    const k = await C.KIT.confirm({ ic, tone: "dgr", title, body, btns: [{ k: "cancel", label: "取消" }, { k: "ok", label: ok, tone: "dgr" }] });
    if (k === "ok") onOk();
  };
  // 草图里装作等了一会儿就回来（真程序这时已经退出或重启了）
  function cover(t1, t2, spin, then) {
    const ov = Object.assign(document.createElement("div"), { className: "sab-ov enter" });
    ov.innerHTML = `<img src="../../internal/web/ui/img/logo-32.png" alt=""><div class="sab-o1">${t1}</div><div class="sab-o2">${spin ? `<i data-ic="spinner" class="sab-spin"></i>` : ""}${t2}</div>`;
    document.body.appendChild(ov); C.KIT.hydrate(ov);
    setTimeout(() => { then?.(); ov.classList.remove("enter"); ov.classList.add("leave"); setTimeout(() => ov.remove(), 220); }, 2200);
  }
  function restart() {
    const run = C.servers.filter(s => s.running);
    const go = () => cover("正在更新", "程序将自动重启", true, () => {
      A.ver = NEW; A.has = false; A.st = "latest"; paintUp(false);
      document.querySelector('#snav [data-sec="about"] .dot')?.remove(); // 装好了：左边分类上的小点去掉
    });
    if (!run.length) { go(); return; }
    ask({ ic: "arrowUp", title: "停止服务器并更新？", ok: "停止服务器并更新", onOk: go,
      body: `更新前需先停止正在运行的 ${run.length} 台服务器：${run.map(s => `<b>${esc(s.name)}</b>`).join("、")}。将先保存存档，随后重启 MineDeck 并更新至 v${NEW}。` });
  }
  const quit = () => ask({ ic: "power", title: "退出 MineDeck？", ok: "退出", body: "将先保存存档并停止正在运行的服务器，随后退出程序。",
    onOk: () => cover("正在停止服务器并退出", "可以关闭此页面", false) });

  // ---- 画 ----
  const segHTML = () => `<div class="sab-seg" role="radiogroup" aria-label="更新渠道"><i class="sab-sgl"></i>${["auto", "stable", "latest"].map(v => {
    const on = (A.ch || "auto") === v;
    return `<button type="button" role="radio" data-v="${v}" aria-checked="${on}" class="${on ? "on" : ""}" title="${esc(CH[v].t)}">${CH[v].l}</button>`;
  }).join("")}</div>`;
  const chTip = () => "手动检查与启动时检查均按所选渠道执行。" + (A.ch ? "" : `当前未指定渠道，已按当前版本自动设为「${chName(effCh())}」。`);
  const item = (body, link) => `<div class="sab-it"><p>${body}</p>${link ? `<button type="button" class="sab-lk" title="${link}" aria-label="打开 ${link}"><i data-ic="share"></i></button>` : ""}</div>`;
  function html(ctx) {
    C = ctx; seed(ctx);
    return `<div class="sab">`
      + `<section class="sab-col sab-main" aria-label="更新与其他">`
      + `<div class="sab-brand"><img src="../../internal/web/ui/img/logo-48.png" alt=""><div><div class="sab-name">MineDeck</div><div class="sab-tag">Windows 本地 Minecraft 开服工具</div></div></div>`
      // 三页合成一页以后照原来的三块分开（作者 11-29：合并了也要适当区分）：每块一个小标题，块和块之间一道细线
      + `<div class="sab-grp" role="group" aria-label="版本与更新"><div class="sab-gt">版本与更新</div>`
      + `<div class="sab-up"><div class="sab-txt"><b class="sab-v">v${A.ver}</b><div class="sab-s"></div></div><div class="sab-act"></div><span class="sab-bar"><i></i></span></div>`
      + `<div class="sab-f"><div class="sab-lb"><span class="sab-cht" title="${esc(chTip())}">更新渠道</span></div>${segHTML()}</div>`
      + `<div class="sab-sws">${SWS.slice(0, 1).map(swHTML).join("")}</div></div><i class="sab-hr"></i>`
      + `<div class="sab-grp" role="group" aria-label="启动与托盘"><div class="sab-gt">启动与托盘</div><div class="sab-sws">${SWS.slice(1).map(swHTML).join("")}</div></div><i class="sab-hr"></i>`
      + `<div class="sab-grp" role="group" aria-label="通知与集成"><div class="sab-gt">通知与集成</div>${hookHTML()}</div>`
      + `<button type="button" class="sab-btn dgr sab-quit" data-sab="quit" title="先保存存档并停止正在运行的服务器，随后退出程序"><i data-ic="power"></i>退出 MineDeck</button>`
      + `</section>`
      + `<section class="sab-col sab-ab" aria-label="关于"><div class="sab-gt">关于</div><div class="sab-lw"><div class="list">`
      + item("内网穿透基于 <b>OpenFrp OPENAPI</b> 接入", "https://www.openfrp.net")
      + item(`界面字体：Geist、Geist Mono（© The Geist Project Authors）与 Noto Sans SC（© Adobe），均为 SIL Open Font License 1.1，子集随程序内嵌，许可原文见 `
        + `<a class="sab-tx" href="../../internal/web/ui/fonts/OFL-Geist.txt" target="_blank">OFL-Geist.txt</a>、<a class="sab-tx" href="../../internal/web/ui/fonts/OFL-NotoSansSC.txt" target="_blank">OFL-NotoSansSC.txt</a>。`)
      + item("本软件以 GNU AGPL-3.0 开源，源码在 GitHub。", "https://github.com/Trasperioooooon/MineDeck")
      + `<div class="sab-it sab-dis"><p>Minecraft 是 Mojang Studios 的商标，本工具与 Mojang / Microsoft / 网易均无隶属或授权关系。</p>`
      + `<p class="en">NOT AN OFFICIAL MINECRAFT PRODUCT. NOT APPROVED BY OR ASSOCIATED WITH MOJANG OR MICROSOFT.</p></div>`
      + `</div><span class="thumb"></span></div></section></div>`;
  }
  const root = () => document.querySelector(".cp > .sab:not(.fade-out)");

  function placeSeg(box, animate) {
    const hl = box.querySelector(".sab-sgl"), on = box.querySelector("button.on"); if (!hl || !on || !box.offsetWidth) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateX(${on.offsetLeft}px)`; hl.style.width = on.offsetWidth + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  function pickCh(b) {
    if (b.classList.contains("on")) return;
    const box = b.parentElement, v = b.dataset.v;
    box.querySelectorAll("button").forEach(x => { const on = x === b; x.classList.toggle("on", on); x.setAttribute("aria-checked", on); });
    placeSeg(box, true);
    A.ch = v === "auto" ? "" : v;
    setTip(root().querySelector(".sab-cht"), chTip());
    if (A.st === "latest" || A.st === "found") { A.st = "idle"; paintUp(); } // 同程序：换了渠道，上一次查出来的结果不再作数；在下载的、下载好的不动
  }
  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    ctx.thinBar(r.querySelector(".sab-lw"));
    placeSeg(r.querySelector(".sab-seg"), false);
    document.fonts?.ready.then(() => { if (r.isConnected) placeSeg(r.querySelector(".sab-seg"), false); });
    paintUp(false);
    r.addEventListener("click", e => {
      const sb = e.target.closest(".sab-seg button"); if (sb) { pickCh(sb); return; }
      const b = e.target.closest("[data-sab]"); if (!b || b.disabled) return;
      switch (b.dataset.sab) {
        case "check": check(); break;
        case "dl": A.st = "downloading"; A.got = 0; paintUp(); runDl(); break;
        case "restart": restart(); break;
        case "sw": toggleSw(b); break;
        case "hook": e.preventDefault(); saveHook(); break;
        case "quit": quit(); break;
      }
    });
    // Webhook 地址框：回车也能存；局域网打开时点进去就全选，输入就是整个换掉（同程序）
    r.addEventListener("keydown", e => { if (e.key === "Enter" && e.target.closest?.(".sab-in")) { e.preventDefault(); saveHook(); } });
    r.addEventListener("input", e => { if (e.target.closest?.(".sab-in")) { N.draft = e.target.value; paintHook(); } });
    r.addEventListener("focusin", e => { if (N.lan && e.target.matches?.(".sab-in input")) e.target.select(); });
  }
  addEventListener("resize", () => { const r = root(); if (r) placeSeg(r.querySelector(".sab-seg"), false); });

  // 离开前拦（kit.js 第 6 条）：这一页只有 Webhook 地址一项
  window.SETTINGS_SECS.about = { html, mount, dirty: () => (hookDirty() ? 1 : 0), save: saveHook, discard: discardHook };
})();
