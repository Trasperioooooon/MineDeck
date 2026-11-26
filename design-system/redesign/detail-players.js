// 服务器详情页 →「玩家」（11-27 第一稿，子代理照临时规范画的；计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」）。样式在 detail-players.css。
//
// 程序现在的玩家页（internal/web/ui/app.js 约 4051 行起）有四块：在线玩家（刷新；每人「OP」「踢」）、白名单（总开关「只允许白名单里的玩家进入」、
// 输入框回车加、每人 ×）、管理员 OP（输入框加、× 取消）、封禁名单（输入框封、× 解封）。后端照 internal/inst/players.go：服务器开着时走控制台命令，
// 停着（启动中也算没开）时直接改 whitelist.json / ops.json / banned-players.json；只有踢人要求服务器运行中（players.go 129 行「踢出玩家需要服务器运行中」）。
// 这里四块都画上，功能一样，排法照临时规范重来：
//   · 内容面板里并排四栏（顺序同程序），每栏一样的骨架：顶上名字和人数，中间名单（多了在栏里滚，细滚动条），底下一个输入框加名字。
//     四块一眼都看得见，不用切；在线玩家没有输入框，名单从上到下占满。
//   · 白名单的总开关就是白名单这一栏的名字：开关在字的左边、字只写「白名单」（同向导第四步「谁能进」，作者 11-26 定过的样子），
//     程序那两句说明（开着只有名单里的能进、管理员不受限制；关着谁都能进）和「server.properties 里还没写这一项」进悬停。
//     关着时名单淡到 0.7（规范第六节），照样能加能删——程序也是这样，名字先存着，打开就生效。
//   · 在线玩家那一栏不写人数：页头「在线人数」已经写着，点它就是来这一页（一个值只说一次）。另外三栏写人数，名单长了数不过来；
//     名单空了不写 0，名单正中写着程序的「空」。
//   · 每人「OP」改成一颗王冠：是管理员时填实、强调色，点了取消；不是时空心淡色，点了设为管理员（程序只能设、不显示是不是）；「踢」换成图标。
//     名单里的 × 平时藏着、悬停那一行才出来（同向导的待装清单），在线玩家那两颗一直在。图标按钮的字都进悬停（规范第十节）。
//   · 加名字：回车或点框里的 ＋。名字照程序的规矩（mclog.ValidName：1~16 位字母、数字、下划线），不合法时框标红、晃一下（0.36 秒），
//     ＋ 的位置换成红字「玩家名不合法」；已经在名单里的不重复加，那一行闪一下。新加的从下面淡进来、名单滚到底；拿掉的往右划 32 像素淡掉再收起（规范第七节）。
//   · 程序每做一件事在右上角出一条提示（「已把 X 加入白名单」，开着时还带「名单文件稍后由服务器刷新」）；这里名单自己就变了，不再另出提示。
//   · 停着、启动中时没有在线的人，在线那一栏是程序的原话「暂无玩家在线」；踢人只出现在在线的人那一行，所以没有要灰掉的踢人键（写进了汇报，等作者定）。
//     别的（加、删、设管理员、封、解封、白名单开关）照程序，三种状态都能用。
//   · 踢掉、封掉一个在线的人，页头的在线人数、控制台补全里的在线玩家跟着变（外壳的 ctx.setOnline）。
// 白名单、管理员、封禁名单里的名字是编的；在线的人用外壳给的 ctx.online（和页头、控制台日志对得上），都在白名单里。
// 外壳的接口见 detail.html 的 content()：html(ctx) 返回内容面板里的那一块，mount(ctx) 在换上去、图标换好以后跑；换标签回来会再调一次，数据留在这个文件里不丢。
(() => {
  // 程序图标里没有的（16 格、1.3 粗，同 MASTER 的画法）：管理员的王冠（是管理员时填实，同页头星标的 star / starOn）、踢出（门框加往外的箭头）
  const CROWN = "M2.6 5.3l2.8 2.6L8 3.6l2.6 4.3 2.8-2.6-1.1 6.2H3.7z";
  Object.assign(window.EXTRA_ICONS = window.EXTRA_ICONS || {}, {
    crown: `<path d="${CROWN}"/><path d="M3.7 13.4h8.6"/>`, crownOn: `<path d="${CROWN}" fill="currentColor"/><path d="M3.7 13.4h8.6"/>`,
    kick: '<path d="M9.3 2.8H4.1a1.3 1.3 0 00-1.3 1.3v7.8a1.3 1.3 0 001.3 1.3h5.2"/><path d="M10.9 5.4L13.5 8l-2.6 2.6M13.3 8H6.6"/>',
  });
  window.DETAIL_TABS = window.DETAIL_TABS || {};

  const NAME_OK = /^[A-Za-z0-9_]{1,16}$/; // 同 mclog.ValidName
  const PH = "输入玩家名，回车添加"; // 程序的原话
  // 三份名单（编的）。原版 26.2 早于 26.3，server.properties 里没写 white-list 时服务端默认关（players.go DefaultWhitelistOn），所以原版画成关着、没写过
  const SAMPLE = {
    paper: { wlOn: true, wlSet: true,
      wl: ["Notch_CN", "Steve", "Alex", "xiaoming_2008", "LittleFox", "RedstoneGuo", "Wang_Lei", "Kiki", "BlockBuster99", "moonlight_cat", "Dawei", "Chen_Yu", "NightOwl", "Pumpkin_Pie", "xX_DragonSlayerX"],
      ops: ["Notch_CN", "Steve"], ban: ["Griefer_X", "xX_TNT_Xx", "spam_bot_42"] },
    forge: { wlOn: true, wlSet: true,
      wl: ["Steve", "Alex", "GearHead", "Create_Fan", "BrassTank", "Wang_Lei", "Kiki", "Dawei", "Mech_Lin", "Cogwheel_77", "Andesite_Ann", "Zhou_Mo"],
      ops: ["Steve"], ban: [] },
    vanilla: { wlOn: false, wlSet: false, wl: ["Steve", "Alex", "SnapshotTester"], ops: ["Steve"], ban: ["Griefer_X"] },
  };
  // 三份名单各自的字：栏名、＋ 和 × 的悬停（照 internal/ops/catalog/players.go 的动作名）、读屏名称（照程序提示的说法）、程序里的动作名
  const L = {
    wl: { title: "白名单", add: "加入白名单", rm: "移出白名单", addAct: "whitelist-add", rmAct: "whitelist-remove", rmLabel: n => `把 ${n} 移出白名单` },
    ops: { title: "管理员 OP", add: "设为管理员", rm: "取消管理员", addAct: "op", rmAct: "deop", rmLabel: n => `取消 ${n} 的管理员` },
    ban: { title: "封禁名单", add: "封禁玩家", rm: "解除封禁", addAct: "ban", rmAct: "pardon", rmLabel: n => `解除 ${n} 的封禁` },
  };
  const WL_TIP = "开着只有名单里的玩家能进（管理员不受限制）；关着时知道地址的人都能进";
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const same = (a, b) => a.toLowerCase() === b.toLowerCase(); // 名单按名字不分大小写比（同 players.go 的 EqualFold）
  const has = (arr, n) => arr.some(x => same(x, n));

  let P = null, C = null; // P：这一页的数据（换标签回来还在）；C：外壳最近一次给的 ctx
  function seed(ctx) {
    if (P && P.kind === ctx.kind) return;
    const d = SAMPLE[ctx.kind] || SAMPLE.paper;
    P = { kind: ctx.kind, online: [...ctx.online].sort(), wl: [...d.wl], ops: [...d.ops], ban: [...d.ban], wlOn: d.wlOn, wlSet: d.wlSet }; // 在线按名字排，同 GetPlayers
  }
  const wlTip = () => WL_TIP + (P.wlSet ? "" : "\nserver.properties 里还没写这一项，这是服务端的默认值");
  const running = () => C.st === "running";

  // ---- 画 ----
  function onlineRow(n) {
    const op = has(P.ops, n), run = running();
    return `<div class="pl-row" data-pl-name="${esc(n)}"><span class="pl-nm">${esc(n)}</span>`
      + `<button type="button" class="pl-ib pl-op${op ? " on" : ""}" title="${op ? "取消管理员" : "设为管理员"}" aria-pressed="${op}" aria-label="${esc(n)} 是管理员"><i data-ic="${op ? "crownOn" : "crown"}"></i></button>`
      + `<button type="button" class="pl-ib pl-kick"${run ? "" : ' aria-disabled="true"'} title="${run ? "踢出玩家" : "踢出玩家需要服务器运行中"}" aria-label="踢出 ${esc(n)}"><i data-ic="kick"></i></button></div>`;
  }
  const listRow = (k, n) => `<div class="pl-row" data-pl-name="${esc(n)}"><span class="pl-nm">${esc(n)}</span>`
    + `<button type="button" class="pl-ib pl-x" data-pl-rm="${k}" title="${L[k].rm}" aria-label="${esc(L[k].rmLabel(n))}"><i data-ic="x"></i></button></div>`;
  const rowOf = (k, n) => k === "online" ? onlineRow(n) : listRow(k, n);
  const listBox = (k, none) => `<div class="pl-lw${P[k].length ? "" : " is-none"}" data-pl-lw="${k}"><div class="list">${P[k].map(n => rowOf(k, n)).join("")}</div>`
    + `<span class="thumb"></span><div class="pl-none">${none}</div></div>`;
  const addBox = k => `<label class="pl-add" data-pl-add="${k}"><input maxlength="16" placeholder="${PH}" autocomplete="off" spellcheck="false" aria-label="添加玩家到${L[k].title}">`
    + `<span class="pl-msg" title="1~16 位字母、数字或下划线">玩家名不合法</span><button type="button" class="pl-ib pl-plus" title="${L[k].add}"><i data-ic="plus"></i></button></label>`;
  function html(ctx) {
    seed(ctx); C = ctx;
    const online = `<section class="pl-col" data-pl-col="online"><div class="pl-hd"><span class="pl-t">在线玩家</span>`
      + `<button type="button" class="pl-ib pl-rf" title="刷新"><i data-ic="refresh"></i></button></div>${listBox("online", "暂无玩家在线")}</section>`;
    const wl = `<section class="pl-col${P.wlOn ? "" : " wl-off"}" data-pl-col="wl"><div class="pl-hd">`
      + `<button type="button" class="pl-tg${P.wlOn ? " on" : ""}" role="switch" aria-checked="${P.wlOn}" title="${esc(wlTip())}"><span class="pl-sw"></span><span class="pl-t">白名单</span></button>`
      + `<span class="pl-n">${num("wl")}</span></div>${listBox("wl", "空")}${addBox("wl")}</section>`;
    const list = k => `<section class="pl-col" data-pl-col="${k}"><div class="pl-hd"><span class="pl-t">${L[k].title}</span><span class="pl-n">${num(k)}</span></div>`
      + `${listBox(k, "空")}${addBox(k)}</section>`;
    return `<div class="pl">${online}${wl}${list("ops")}${list("ban")}</div>`;
  }

  // ---- 挂上去 ----
  const root = () => document.querySelector(".cp > .pl:not(.fade-out)") || document.querySelector(".pl");
  const lwOf = k => root()?.querySelector(`[data-pl-lw="${k}"]`);
  const rowEl = (k, n) => [...(lwOf(k)?.querySelectorAll(".pl-row:not(.pl-out)") || [])].find(r => same(r.dataset.plName, n));
  // 名字放不下被截断时，全名进悬停
  const fitTitle = el => { if (el.scrollWidth > el.clientWidth + 1) el.title = el.textContent; else el.removeAttribute("title"); };
  function mount(ctx) {
    C = ctx;
    const r = root(); if (!r) return;
    r.querySelectorAll(".pl-lw").forEach(w => ctx.thinBar(w));
    r.querySelectorAll(".pl-nm").forEach(fitTitle);
    document.fonts?.ready.then(() => r.isConnected && r.querySelectorAll(".pl-nm").forEach(fitTitle));
    r.addEventListener("click", onClick);
    r.addEventListener("keydown", onKey);
    r.addEventListener("input", onInput);
  }

  // ---- 只换变了的那一处 ----
  const num = k => P[k].length || ""; // 空的时候不写 0：名单正中已经写着「空」（一个值只说一次）
  const count = k => { const c = root()?.querySelector(`[data-pl-col="${k}"] .pl-n`); if (c) c.textContent = num(k); };
  // 踢掉、封掉在线的人以后告诉外壳：页头的在线人数、控制台的补全跟着变（外壳只换数字，不重画页头）
  const headCount = () => C.setOnline(P.online);
  // 新加的一行：从下面 5 像素淡进来（0.22 秒），名单滚到底让它露出来
  function addRow(k, n) {
    const w = lwOf(k); if (!w) return;
    const list = w.querySelector(".list");
    list.insertAdjacentHTML("beforeend", rowOf(k, n));
    const row = list.lastElementChild; KIT.hydrate(row); fitTitle(row.querySelector(".pl-nm"));
    w.classList.remove("is-none");
    row.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT, fill: "backwards" });
    w._paint?.();
    list.scrollTo({ top: list.scrollHeight, behavior: "smooth" });
  }
  // 拿掉一行：往右划 32 像素淡掉，再收起，下面的行顺着补上来（同向导待装清单）；最后一行没了，正中那句淡字再淡进来
  function dropRow(k, n) {
    const w = lwOf(k), row = rowEl(k, n); if (!row) return;
    row.classList.add("pl-out");
    const h = row.offsetHeight + "px";
    row.animate([
      { opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: "translateX(32px)", height: h, marginBottom: "0px", offset: .55, easing: EASE_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 320, fill: "forwards" }).onfinish = () => {
      row.remove(); w._paint?.();
      if (!w.querySelector(".pl-row")) w.classList.add("is-none");
    };
  }
  // 已经在名单里：那一行露出来、闪一下
  function flash(k, n) {
    const row = rowEl(k, n); if (!row) return;
    row.scrollIntoView({ block: "nearest", behavior: "smooth" });
    row.classList.remove("pl-flash"); void row.offsetWidth; row.classList.add("pl-flash");
  }
  // 在线的人那一行的王冠跟着管理员名单变
  function syncOp(n) {
    const b = rowEl("online", n)?.querySelector(".pl-op"); if (!b) return;
    const op = has(P.ops, n);
    b.classList.toggle("on", op); b.setAttribute("aria-pressed", op); b.title = op ? "取消管理员" : "设为管理员"; delete b.dataset.tip;
    b.innerHTML = KIT.icon(op ? "crownOn" : "crown");
  }
  function leave(n) {
    if (!has(P.online, n)) return;
    P.online = P.online.filter(x => !same(x, n)); dropRow("online", n); headCount();
  }
  // 照程序的七个动作（players.go）：草图里不发请求，直接改这里的数据、换变了的那几处
  function act(action, n) {
    const put = k => { if (has(P[k], n)) { flash(k, n); return false; } P[k].push(n); addRow(k, n); count(k); return true; };
    const pull = k => { P[k] = P[k].filter(x => !same(x, n)); dropRow(k, n); count(k); };
    switch (action) {
      case "whitelist-add": put("wl"); break;
      case "whitelist-remove": pull("wl"); break;
      case "op": if (put("ops")) syncOp(n); break;
      case "deop": pull("ops"); syncOp(n); break;
      case "ban": if (put("ban") && running()) leave(n); break; // 封了在线的人，服务器会把他踢下线
      case "pardon": pull("ban"); break;
      case "kick": if (running()) leave(n); break;
    }
  }
  const shake = el => el.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
    { duration: 360, easing: "cubic-bezier(.3, .7, .2, 1)" });
  function submit(box) {
    const inp = box.querySelector("input"), v = inp.value.trim(), k = box.dataset.plAdd;
    inp.focus();
    if (!v) return;
    if (!NAME_OK.test(v)) { box.classList.add("bad"); shake(box); return; }
    inp.value = ""; box.classList.remove("has");
    act(L[k].addAct, v);
  }

  // ---- 事件（绑在这一页的根上，换标签时跟着内容一起扔掉） ----
  function onClick(e) {
    const b = e.target.closest("button"); if (!b) return;
    if (b.getAttribute("aria-disabled") === "true") return;
    if (b.classList.contains("pl-plus")) { e.preventDefault(); submit(b.closest(".pl-add")); return; }
    if (b.classList.contains("pl-tg")) {
      P.wlOn = !P.wlOn; P.wlSet = true; // 程序把 white-list 写进 server.properties，开着时再发 whitelist on / off
      b.classList.toggle("on", P.wlOn); b.setAttribute("aria-checked", P.wlOn); b.title = wlTip(); delete b.dataset.tip;
      b.closest(".pl-col").classList.toggle("wl-off", !P.wlOn);
      return;
    }
    if (b.classList.contains("pl-rf")) { // 刷新：图标转 0.6 秒（规范第七节）；草图里没有新数据
      if (b.classList.contains("spinning")) return;
      b.classList.add("spinning"); setTimeout(() => b.classList.remove("spinning"), 600); return;
    }
    const row = b.closest(".pl-row"); if (!row || row.classList.contains("pl-out")) return;
    const n = row.dataset.plName;
    if (b.classList.contains("pl-x")) act(L[b.dataset.plRm].rmAct, n);
    else if (b.classList.contains("pl-op")) act(has(P.ops, n) ? "deop" : "op", n);
    else if (b.classList.contains("pl-kick")) act("kick", n);
  }
  function onKey(e) {
    const box = e.target.closest?.(".pl-add"); if (!box || e.target.tagName !== "INPUT") return;
    if (e.key === "Enter") { e.preventDefault(); submit(box); }
  }
  function onInput(e) {
    const box = e.target.closest?.(".pl-add"); if (!box) return;
    box.classList.remove("bad"); box.classList.toggle("has", !!e.target.value.trim());
  }

  window.DETAIL_TABS.players = { html, mount };
})();
