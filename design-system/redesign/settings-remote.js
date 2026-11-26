// 全局设置 →「手机远程管理」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 settings-remote.css。
//
// 程序现在的样子（internal/web/ui/app.js 约 6105 行 setRemote）：页顶标题和一句说明（「用手机、平板或别的电脑的浏览器打开 MineDeck 的管理页，需要访问密码；开启后重启程序生效。」），
// 一条提示「让朋友进游戏的地址在服务器页上……去服务器页」；一组两行：「开启手机远程管理」开关（说明「手机和这台电脑连在同一个 Wi-Fi 或路由器下，就能打开管理页」），
// 「访问密码」（说明随有没有设过变；密码框 +「保存」，下面一行报错）；开着没重启时一句「手机远程管理已开启。重启 MineDeck 后生效。……」；
// 开着、生效时一张卡：左边二维码（qrcode.js 现画），右边「手机扫码访问」、说明、地址 +「复制」、有几个地址时一排地址按钮和一句「扫码打不开就换一个」；
// 没有局域网 IP 时一句「未检测到本机局域网 IP，请确认已连接网络。」；最后是防火墙命令（「还是打不开的话，用管理员身份在 CMD 里运行这条命令，放行防火墙。」+「复制命令」）。
// 开关点了即时保存；开启前必须先有访问密码（后端也拒绝：「请先设置访问密码」）：没设密码、框里也没字时拨开关，开关拨回去，密码框下面报
// 「先设置访问密码：填好后点「保存」，手机远程管理会一起开启」，光标落过去，记下「想开」，之后点「保存」时密码和开关一起存；框里有字时拨开关，密码跟着一起存。
// 「保存」只存密码（只有刚才被拦下的那一下才连开关一起开）；空着点「保存」报「请先输入访问密码」（设过的报「输入新密码再保存」）。
// 开启要重启程序才生效（监听地址只在启动时绑定），关闭即时生效。
//
// 这里（内容面板里两栏，左 360 宽；栏的内边距、字和块的间距同设置里「外观」「下载与网络」「存储位置」）：
//   · 左栏：最上面是总开关（开关在字的左边，字只写「开启手机远程管理」，同「外观」的开关）；程序页顶的说明、开关下面那句、「让朋友进游戏的地址不在这里」都进它的悬停。
//     下面「访问密码」一格：框里右边一颗保存（同详情页「扩展」单字段的框）。框里有字时保存亮起来，空着也能点、点了就地拦下（同程序）。
//     密码框的占位字照程序随状态换（「设置访问密码（必填）」「已设置（输入新密码可修改）」），那一行说明进「访问密码」的悬停。
//     拦下来照规范第七节「反馈」：框标红、晃一下、光标落过去，保存的位置换成一句短的红字（程序的整句进悬停）。存好了保存图标换成勾 1.2 秒，不弹提示。
//     最下面「放行防火墙」：命令原样摆着（等宽、折行），右上角复制（规范第九节「值后面一个复制图标」）；程序那句「还是打不开的话……」进悬停。
//   · 右栏「手机扫码访问」：正中二维码（用程序的 qrcode.js 现编，真能扫；码区恒白），下面一行地址（24 号等宽）、复制、换地址（▾，点开一张菜单列本机的几个地址，
//     「扫码打不开就换一个」那句进悬停）。程序是地址下面一排按钮，地址又写一遍；这里只在地址那一处写 IP（规范第一节「一个值只说一次」）。
//     没开、开了还没重启、没有局域网 IP 时，码是一块糊掉的淡码（看得出这里是放码的地方，扫不出来），下面一行换成会改变结果的那句：
//     「重启 MineDeck 后生效」（整句进悬停）、「未检测到本机局域网 IP，请确认已连接网络。」；关着时下面那行空着（开关已经说了关着）。
//   · 换状态只换变了的那几样：开关滑过去、码清楚 / 糊掉（0.32 秒）、码下面那一行旧的淡掉新的淡进来；换地址时码和地址两样淡出淡进。
// 示例数据：程序默认端口 27333；本机三个地址照 lanIPRank 排（192.168.1.5 同向导、联机页的局域网 IP；172.24.160.1 是 WSL 虚拟网卡；26.18.4.77 是 Radmin VPN，同向导的虚拟局域网）。
// 草图专用（草图里没法重启 MineDeck）：&lan=restart 开着、还没重启；&lan=live 开着、生效（二维码）；&lan=noip 生效但没有局域网 IP；&lan=off 设过密码、关着。
// 不带参数是第一次来的样子：没设密码、关着（任务单要看「没设密码就拨开关会怎样」）。
// 程序里有、这里没画的：手机那一头打开的管理页（不在这一轮）；读取失败。
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const E_IN = "cubic-bezier(.4, 0, 1, 1)", E_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s);
  const hy = el => { window.KIT.hydrate(el); return el; };
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: SLIDE });
  const setTip = (el, text) => { // 悬停字没变就不动（kit.js 悬停时会把 title 挪进 data-tip，两处都看）
    if (!el || (el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  let C = null; // 外壳最近一次给的 ctx

  // ---- 二维码：现取程序的 qrcode.js 编码（它写的是 window.QR = …，给它一个自己的 window，不往全局放），自己画成 SVG，颜色走 .sr-qbg / .sr-qfg ----
  let QR = null;
  const QR_READY = fetch("../../internal/web/ui/qrcode.js").then(r => r.text())
    .then(src => { const w = {}; new Function("window", src)(w); QR = w.QR || null; })
    .catch(e => console.warn("没取到 qrcode.js", e));
  const DUMMY = "MineDeck"; // 没生效时那块糊掉的码（不是管理页地址）
  function qrSVG(text) {
    if (!QR) return "";
    const q = QR.encode(text), n = q.size + 8; // 四周 4 模块静区，同程序 QR.svg
    let d = "";
    q.modules.forEach((row, y) => {
      for (let x = 0; x < q.size; x++) {
        if (!row[x]) continue;
        let w = 1; while (x + w < q.size && row[x + w]) w++;
        d += `M${x + 4} ${y + 4}h${w}v1h-${w}z`; x += w - 1;
      }
    });
    return `<svg viewBox="0 0 ${n} ${n}" shape-rendering="crispEdges" aria-hidden="true"><path class="sr-qbg" d="M0 0h${n}v${n}H0z"/><path class="sr-qfg" d="${d}"/></svg>`;
  }

  // ================= 数据（换分类回来还在，没存的密码也在） =================
  const PORT = 27333; // 程序默认的监听端口（被占用时会随机化，草图不管）
  const IPS = ["192.168.1.5", "172.24.160.1", "26.18.4.77"]; // 照程序 lanIPRank 排：家用局域网段在前，172.16/12（多为 WSL / Hyper-V 虚拟网卡）在后，其他再后
  const FW = `netsh advfirewall firewall add rule name="MineDeck" dir=in action=allow protocol=TCP localport=${PORT}`;
  const TIP_ON = "用手机、平板或别的电脑的浏览器打开 MineDeck 的管理页，需要访问密码；开启后重启程序生效。\n"
    + "手机和这台电脑连在同一个 Wi-Fi 或路由器下，就能打开管理页。\n让朋友进游戏的地址在服务器页上（每台服务器的页头和卡片上的「连接地址」），不在这里。";
  const TIP_QR = "用手机相机或浏览器扫码，输入访问密码即可管理；手机要和这台电脑在同一个局域网（同一 Wi-Fi 或路由器）。";
  const TIP_IPS = "本机有多个网络地址：扫码打不开就换一个（家用路由器下通常是 192.168.x.x）。";
  const TIP_RESTART = "手机远程管理已开启。重启 MineDeck 后生效。重启之后，这里会显示手机扫码用的地址。";
  const TIP_FW = "还是打不开的话，用管理员身份在 CMD 里运行这条命令，放行防火墙。";
  const NEED_PW = "先设置访问密码：填好后点「保存」，手机远程管理会一起开启";
  let S = null;
  function seed(ctx) {
    if (S) return;
    // on：设置里开着（config.listenLan）；live：这次启动已经监听在局域网上（lanActive，只有重启才变）；set：设过访问密码；want：没设密码时拨了开关，存密码时一起开
    S = { on: false, live: false, set: false, want: false, ips: IPS.slice(), ip: 0, draft: "" };
    const m = ctx.QS.get("lan");
    if (m === "off") Object.assign(S, { set: true });
    if (m === "restart") Object.assign(S, { on: true, set: true });
    if (m === "live") Object.assign(S, { on: true, live: true, set: true });
    if (m === "noip") Object.assign(S, { on: true, live: true, set: true, ips: [] });
  }
  const url = () => `http://${S.ips[S.ip]}:${PORT}`;
  const state = () => !S.on ? "off" : !S.live ? "restart" : S.ips.length ? "live" : "noip";
  const pwTip = () => S.set ? "已设置；要修改就输入新密码后保存" : "开启手机远程管理前必须先设置";
  const pwPh = () => S.set ? "已设置（输入新密码可修改）" : "设置访问密码（必填）";
  const tooLong = v => new TextEncoder().encode(v).length > 72; // 后端 bcrypt 的上限

  // ================= 画 =================
  function underHTML(st) {
    if (st === "live") return `<div class="sr-url"><span class="sr-u">${esc(url())}</span><button type="button" class="sr-ib" data-sr-act="copy" title="复制"><i data-ic="copy"></i></button>`
      + (S.ips.length > 1 ? `<button type="button" class="sr-ib" data-sr-act="ips" title="${esc(TIP_IPS)}" aria-label="换一个地址" aria-haspopup="menu" aria-expanded="false"><i data-ic="chevronDown" class="sr-chev"></i></button>` : "")
      + `</div>`;
    if (st === "restart") return `<div class="sr-note"><i data-ic="info"></i><span title="${esc(TIP_RESTART)}">重启 MineDeck 后生效</span></div>`;
    if (st === "noip") return `<div class="sr-note"><i data-ic="warn"></i><span>未检测到本机局域网 IP，请确认已连接网络。</span></div>`;
    return "";
  }
  const qrLabel = st => st === "live" ? "访问地址二维码：" + url() : "二维码（还没生效）";
  function html(ctx) {
    seed(ctx); C = ctx;
    const st = state(), live = st === "live", qu = live ? url() : DUMMY;
    const left = `<section class="sr-col sr-set" aria-label="手机远程管理">`
      + `<button type="button" class="sr-tg${S.on ? " on" : ""}" role="switch" aria-checked="${S.on}" data-sr-act="toggle" title="${esc(TIP_ON)}">`
      + `<span class="sr-sw"></span><span class="sr-tl">开启手机远程管理</span></button>`
      + `<div class="sr-fld"><span class="sr-lb"><span data-sr-k="pwtip" title="${esc(pwTip())}">访问密码</span></span>`
      + `<label class="sr-in${S.draft ? " dirty" : ""}"><input type="password" value="${esc(S.draft)}" placeholder="${esc(pwPh())}" autocomplete="new-password" spellcheck="false" aria-label="访问密码">`
      + `<span class="sr-msg"></span><button type="button" class="sr-ib sr-save" data-sr-act="save" title="保存"><i data-ic="save"></i></button></label></div>`
      + `<div class="sr-fld sr-fw"><span class="sr-lb"><span title="${esc(TIP_FW)}">放行防火墙</span></span>`
      + `<div class="sr-cmd">${esc(FW)}<button type="button" class="sr-ib" data-sr-act="fw" title="复制"><i data-ic="copy"></i></button></div></div></section>`;
    const right = `<section class="sr-col sr-scan" aria-label="手机扫码访问"><span class="sr-lb"><span title="${esc(TIP_QR)}">手机扫码访问</span></span>`
      + `<div class="sr-qrw"><div class="sr-qr${live ? "" : " dim"}" role="img" aria-label="${esc(qrLabel(st))}" data-u="${esc(qu)}">${qrSVG(qu)}</div>`
      + `<div class="sr-under" data-st="${st}">${underHTML(st)}</div></div></section>`;
    return `<div class="sr">${left}${right}</div>`;
  }

  // ================= 挂上去 =================
  const root = () => document.querySelector(".cp > .sr:not(.fade-out)");
  function mount(ctx) {
    C = ctx;
    const r = root(); if (!r) return;
    r.addEventListener("click", onClick);
    r.addEventListener("keydown", onKey);
    r.addEventListener("input", onInput);
    // qrcode.js 还没取到时码是空的：取到以后补上
    QR_READY.then(() => { const q = r.isConnected && $(".sr-qr", r); if (q && !q.firstElementChild) q.innerHTML = qrSVG(q.dataset.u); });
  }

  // ================= 只换变了的那一处 =================
  // 一样东西换成新的：旧的往上 5 像素淡掉（0.09 秒），新的从下面 5 像素淡进来（0.22 秒）；连着换时上一次作废（规范第七节「逐项换」）
  function swapIn(el, html, after) {
    if (!el) return;
    const tok = el._srTok = (el._srTok || 0) + 1, had = !!el.innerHTML.trim();
    el.getAnimations().forEach(a => a.cancel());
    const show = () => {
      if (el._srTok !== tok) return;
      el.innerHTML = html; hy(el); after?.();
      el.getAnimations().forEach(a => a.cancel());
      if (html) el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: E_OUT });
    };
    if (!had) { show(); return; }
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: E_IN, fill: "forwards" }).onfinish = show;
  }
  // 码换内容：只淡出淡进（不挪），0.09 / 0.22 秒
  function swapQR(q, text) {
    if (q.dataset.u === text) return;
    q.dataset.u = text;
    const tok = q._srTok = (q._srTok || 0) + 1, svg = q.firstElementChild;
    if (!svg) { q.innerHTML = qrSVG(text); return; }
    svg.getAnimations().forEach(a => a.cancel());
    svg.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: E_IN, fill: "forwards" }).onfinish = () => {
      if (q._srTok !== tok) return;
      q.innerHTML = qrSVG(text);
      q.firstElementChild?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 220, easing: E_OUT });
    };
  }
  function paintLeft() {
    const r = root(); if (!r) return;
    const tg = $(".sr-tg", r); tg.classList.toggle("on", S.on); tg.setAttribute("aria-checked", String(S.on));
    const box = $(".sr-in", r), inp = $("input", box);
    if (inp.placeholder !== pwPh()) inp.placeholder = pwPh();
    setTip($('[data-sr-k="pwtip"]', r), pwTip());
    box.classList.toggle("dirty", !!inp.value);
  }
  function paintRight() {
    const r = root(); if (!r) return;
    const st = state(), live = st === "live", q = $(".sr-qr", r), under = $(".sr-under", r);
    if (live) {
      // 糊着的时候换成真码不用淡出淡进（还糊着，看不出来）；本来就清楚的（换地址）才淡出淡进
      if (q.classList.contains("dim")) { q.dataset.u = url(); q.innerHTML = qrSVG(url()); } else swapQR(q, url());
    }
    q.classList.toggle("dim", !live); q.setAttribute("aria-label", qrLabel(st));
    if (!live) closeMenu();
    if (under.dataset.st !== st) { under.dataset.st = st; swapIn(under, underHTML(st)); }
    else if (live) { const u = $(".sr-u", under); if (u && u.textContent !== url()) swapIn(u, esc(url())); }
  }
  // 存好了 / 复制了：图标换成勾 1.2 秒（规范第七节）
  function tick(b) {
    if (!b) return; untick(b);
    b._srOld = b.innerHTML; b.classList.add("done"); b.innerHTML = window.KIT.icon("check");
    b._srT = setTimeout(() => untick(b), 1200);
  }
  function untick(b) {
    if (!b?._srT) return;
    clearTimeout(b._srT); b._srT = 0; b.classList.remove("done"); b.innerHTML = b._srOld;
  }
  // 拦下来：框标红、晃一下、光标落过去，保存的位置换成一句短的红字（整句进悬停）
  function bad(box, short, full) {
    box.classList.add("bad");
    const m = $(".sr-msg", box); m.textContent = short; setTip(m, full || "");
    shake(box); $("input", box)?.focus();
  }
  // 草图里不发请求：改这里的数据、换变了的那几处（程序 PUT /api/config）
  function takePw(box) { // 框里的密码存进去：框清空，保存图标打勾
    const inp = $("input", box);
    S.set = true; S.draft = ""; inp.value = ""; box.classList.remove("bad", "dirty");
    tick($(".sr-save", box));
  }
  function toggle() {
    const r = root(), box = $(".sr-in", r), v = $("input", box).value, turnOn = !S.on;
    if (turnOn && !S.set && !v) { S.want = true; bad(box, "先设置访问密码", NEED_PW); return; } // 开关不动，等存密码时一起开
    if (v) { // 框里有字：和开关一起存（没设过密码时必须带；设过的也带上，不然框清空了，人以为新密码生效了）
      if (tooLong(v)) { bad(box, "最长 72 字节", "密码不可用（最长 72 字节）"); return; }
      takePw(box);
    }
    S.on = turnOn; S.want = false;
    paintLeft(); paintRight();
  }
  function savePw() {
    const r = root(), box = $(".sr-in", r), v = $("input", box).value;
    if (!v) { bad(box, S.set ? "输入新密码再保存" : "请先输入访问密码"); return; }
    if (tooLong(v)) { bad(box, "最长 72 字节", "密码不可用（最长 72 字节）"); return; }
    takePw(box);
    if (S.want) { S.on = true; S.want = false; } // 只有刚才被拦下的那一下才连开关一起开；不拿页面上开关的样子一起发
    paintLeft(); paintRight();
  }
  function copy(b, text) {
    try { navigator.clipboard?.writeText(text).catch(() => {}); } catch { /* 草图里复制不了也照样打勾 */ }
    tick(b);
  }
  function pickIP(i) {
    if (i === S.ip) return;
    S.ip = i; paintRight();
  }

  // ---- 换地址的菜单：同共用件页的下拉菜单，挂在 body 上 ----
  let dm = null;
  function openMenu(btn) {
    if (dm && dm._btn === btn) { closeMenu(); return; }
    closeMenu(true);
    const m = dm = Object.assign(document.createElement("div"), { className: "sr-dm enter" });
    m.setAttribute("role", "menu");
    m.innerHTML = S.ips.map((ip, i) => `<div class="sr-it${i === S.ip ? " here" : ""}" role="menuitemradio" aria-checked="${i === S.ip}" data-i="${i}"><span>${esc(ip)}</span>`
      + (i === S.ip ? `<i data-ic="check"></i>` : "") + `</div>`).join("");
    document.body.appendChild(hy(m));
    const b = btn.getBoundingClientRect();
    m.style.left = Math.round(Math.max(8, b.right + 5 - m.offsetWidth)) + "px"; // 右边和 ▾ 对齐（▾ 在地址最右，往左展开不出栏）
    m.style.top = Math.round(b.bottom + 6) + "px";
    m._btn = btn; btn.classList.add("open"); btn.setAttribute("aria-expanded", "true");
    m.addEventListener("click", e => {
      const it = e.target.closest(".sr-it"); if (!it || it.classList.contains("here")) return;
      closeMenu(); pickIP(+it.dataset.i);
    });
  }
  function closeMenu(now) {
    if (!dm) return; const m = dm; dm = null;
    m._btn?.classList.remove("open"); m._btn?.setAttribute("aria-expanded", "false");
    if (now) { m.remove(); return; }
    m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90);
  }
  document.addEventListener("pointerdown", e => { if (dm && !e.target.closest?.(".sr-dm") && !e.target.closest?.('[data-sr-act="ips"]')) closeMenu(); }, true);
  document.addEventListener("keydown", e => { if (e.key === "Escape" && dm) closeMenu(); });
  document.addEventListener("scroll", () => closeMenu(true), true);

  // ================= 事件（绑在这一页的根上，换分类时跟着内容一起扔掉） =================
  function onClick(e) {
    const b = e.target.closest("[data-sr-act]"); if (!b) return;
    switch (b.dataset.srAct) {
      case "toggle": toggle(); break;
      case "save": e.preventDefault(); savePw(); break;
      case "copy": copy(b, url()); break;
      case "fw": copy(b, FW); break;
      case "ips": openMenu(b); break;
    }
  }
  function onKey(e) {
    if (e.key !== "Enter" || !e.target.closest?.(".sr-in")) return;
    e.preventDefault(); savePw();
  }
  function onInput(e) {
    const box = e.target.closest?.(".sr-in"); if (!box) return;
    S.draft = e.target.value;
    box.classList.remove("bad"); box.classList.toggle("dirty", !!e.target.value);
    if (e.target.value) untick($(".sr-save", box)); // 勾还没收、又打了字：马上换回保存
  }

  window.SETTINGS_SECS.remote = { html, mount };
})();
