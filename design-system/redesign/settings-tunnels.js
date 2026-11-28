// 全局设置 →「内网穿透」（11-27 夜里第一稿，子代理照临时规范画的；计划书把全局设置各分类归在「主会话照规范先做出大概、作者整体扫一眼」）。
// 样式在 settings-tunnels.css，选择器都带 stn 前缀（挂在 body 上的菜单、确认框、提示也是：.stn-dm、.stn-mask、#stn-toasts）。
//
// 来历：程序里「内网穿透」原来是单独一页（internal/web/ui/app.js renderTunnels，约 4818 行）：上面手填的「添加隧道」表单，中间「我的隧道」列表
// （启动 / 停止、日志、编辑、清除凭据、删除，公网地址带复制、「上次」），最下面「平台账户」（OpenFrp 浏览器授权、樱花粘访问密钥；连上以后
// 概览、签到、购买、断开，概览里能把平台上的隧道导入、在平台上创建并导入）。11-26 作者定：胶囊里的「内网穿透」去掉，隧道跟着服务器走
// （新建向导第四步、详情页「联机」），设置里只放平台账户和「全部隧道」总览（作者：「放到设置里面连接账户其实就可以了，到最后也就是简单的管理隧道、账户列表这些」）。
// 第一稿是 tunnels.html 的 ③；说法、状态、平台名照 detail-net.js（联机）和 wizard.html 第四步。
//
// 这里（内容面板 868 × 658，里面 852 × 642）上下两栏，栏之间 8：
//   · 上面一栏「平台账户」（圆角 18、内边距 8）：三块并排（OpenFrp、樱花穿透、自建 frps），各 273 宽、216 高、圆角 10（18 − 8，同心），块里内边距 14 / 16。
//     每块：第一行名字（13.5 号 600）和状态胶囊（24 高）；下面跟着状态换：
//       已连接：账户名（15 号 600）和用户组标签，下面几行「流量 / 隧道 / 签到」（22 高），最下面一排 36 高胶囊：签到（外链，只打开官网）、购买（菜单，列官网入口）、断开（浅危险底，确认框）。
//               樱花账户名那一行右边一颗图标按钮「更换访问密钥」（计划书：设置里放樱花访问密钥的换、删；删就是断开）。
//       OpenFrp 没连：一块虚线框，中间一颗「浏览器授权」；点了框里换成「正在等待你在浏览器里完成授权…」和「取消等待」，草图里等 2.6 秒就算授权好了。
//       樱花没连：「樱花访问密钥」「去官网复制」、40 高的框加「连接」（同向导第四步）；空着点「连接」框标红、晃一下、光标落过去，名字后面一句红字。
//               最下面一行淡字「第一次用樱花：先把 frpc 放好」，几步做法进悬停（程序原话，只是不再用 <details> 折叠）。
//       自建 frps：服务器地址、端口（同一行，端口 88 宽）、认证令牌（选填），都是 40 高、圆角 10，最下面「保存」（改了才亮）。存过令牌以后框里占位字是
//               「已设置（留空不修改）」，框里右边一颗清除（垃圾桶，危险确认框），同「下载与网络」的 API Key。
//   · 下面一栏「全部隧道」（圆角 18、内边距 14 / 20）：标题和条数，下面一行一条，44 高、圆角 10，在栏里自己滚（细滚动条）。每行从左到右：
//       两行字（上：服务器名和「游戏 / 语音」标签；没绑定的是隧道名和「没绑定服务器」；下：平台 · 节点 · 隧道名或隧道 ID）、
//       公网地址（等宽，停着的标「上次」，带复制）、状态胶囊（已连上 / 连接中 / 没开 / 上次出错 / 凭据解不开）、最右边：
//       绑定了服务器的是一个「›」，整行点了跳到那台服务器的「联机」（detail.html?kind=…&tab=net）；没绑定的是「绑定到…」（菜单列四台服务器，
//       已经有同一种隧道的那台灰着，悬停说原因）和删除（确认框，同联机页那个：「同时删掉 X 上的这条隧道」默认开，自建 frps 的没有这个开关）。
//       绑定以后只换那一行（名字、下面那行字、最右边），右上角一条提示带「撤销」；删除是那一行往右划 32 像素淡掉再收起。
// 数据都是假的：Paper 那台（生存服 · 朋友）有一条 OpenFrp 的游戏隧道（mc_survival，#2 香港-2，frp-hk.openfrp.net:31234，同详情页页头）；
// 语音隧道还没建（和联机页对齐：那页演示的就是「新建语音隧道」，主会话 11-27 改）；另有两条没绑定的老隧道：樱花的「我的隧道」（隧道 778899，同第一稿）、
// OpenFrp 的 old_server（联机页「已有隧道」里那条，#29 香港-4，上次出了错）。账户照向导和联机页：OpenFrp 已连接（steve · VIP），樱花没连。
// 令牌、密钥一律不显示真值（框是密码框，存过的只写「已设置」），不读本机配置里的任何东西。
// 离开前拦（作者 11-28 定，外壳统一拦，这里只登记 dirty / save / discard）：这一页只有自建 frps 那三个框是先改、点「保存」才生效的（地址、端口、令牌各算一项）；
// 连接、断开、绑定、删除都是点了就做，不在里面；樱花粘了没点「连接」的访问密钥也不算（那是「连接」要用的，不是一项设置）。点一行跳去服务器的「联机」走 ctx.go，有没存的先问。
// 草图专用（地址参数，定了就删）：
//   &acc=none    两家都没连；&acc=both 樱花也连上了（steve · 普通用户）；&acc=lost OpenFrp 存的凭据在这台电脑上读不出来（按没连算，状态写「读不出来」）
//   &ro=1        在局域网里打开（手机远程管理）：账户只看状态（程序在局域网里不拉概览，账户和数换成一句「在局域网里打开时只能看连接状态」），
//                按钮、粘贴框、自建 frps 的框照样摆着、灰着，悬停说原因（程序是把按钮和框藏掉）；下面的隧道列表照常能绑定、删除
//   &frps=set    自建 frps 已经存过（frp.example.com、7000、令牌已设置）
//   &tun=none    一条隧道都没有；&tun=many 多塞十几条没绑定的，看栏里滚；&tun=lost「我的隧道」的凭据解不开；&tun=err tunnels.json 读不出来（「重试」一下就读出来了）
(() => {
  window.SETTINGS_SECS = window.SETTINGS_SECS || {};
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  // kit.js 把 title 挪进 data-tip（悬停时才挪），改悬停要两处一起看（同存储位置那页）
  const setTip = (el, text) => {
    if ((el.getAttribute("title") ?? el.dataset.tip ?? "") === (text || "")) return;
    if (text) el.title = text; else el.removeAttribute("title"); delete el.dataset.tip;
  };
  const setAttr = (el, k, v) => { if (el.getAttribute(k) !== String(v)) el.setAttribute(k, v); };

  const PLAT = { openfrp: "OpenFrp", sakura: "樱花穿透", custom: "自建 frps" };
  // 平台名拼进句子里：英文开头的（OpenFrp）和旁边的汉字之间带空格，中文的不带；挨着标点的那一边传 ""
  const platIn = (k, l = " ", r = " ") => /^[A-Za-z]/.test(PLAT[k]) ? l + PLAT[k] + r : PLAT[k];
  // ---- 程序原话（app.js renderTunnels、accounts.go），收进悬停 ----
  const TIP = {
    openfrp: "OpenFrp 部分基于 OpenFrp OPENAPI（github.com/ZGIT-Network/OPENFRP-APIDOC）。此项目由社区开发，OpenFrp 官方不负责除节点问题以外的技术支持，"
      + "MineDeck 本身的问题请到 GitHub Issues 反馈。\n签到与购买仅打开官方网页，MineDeck 不代签、不代下单。",
    sakura: "连接后，可在新建向导与各服务器的「联机」页中直接创建或选用平台已有隧道。访问密钥通过 Windows 加密保存在本机。\n签到与购买仅打开官方网页，MineDeck 不代签、不代下单。",
    frps: "有自己的 frp 服务器才用这个。填好以后，各台服务器「联机」里打开「自建 frps」就连这一台，每台只要再定一个公网端口",
    auth: "通过 OpenFrp 远程授权连接。MineDeck 不会接收或保存账号密码。",
    signOF: "OpenFrp 签到需在官方控制台完成人机验证，MineDeck 不代签。\n点击将在浏览器中打开 console.openfrp.net。",
    signSK: "樱花穿透（SakuraFrp）签到需在官网登录后完成，访问密钥无法用于签到。\n点击将在浏览器中打开 www.natfrp.com/user/。",
    buy: "仅打开官方网页，MineDeck 不代下单、不代收款。",
    getKey: "在浏览器中打开樱花穿透官网的用户信息页，复制「访问密钥」",
    lost: "已保存的凭据在本机无法解密（可能已更换电脑或 Windows 用户），请重新连接账号。",
    ro: "局域网访问时仅可查看连接状态；连接、签到、购买和自建 frps 的设置请在运行 MineDeck 的电脑上操作。",
    last: "隧道当前已停止，此处显示上次运行时获取的地址。樱花穿透与 OpenFrp 的地址通常保持不变，再次启动后将自动刷新。",
    broken: "凭据无法解密，请删除此隧道后在服务器「联机」页中重新配置。\n凭据加密绑定本机与当前 Windows 用户，更换电脑或用户后无法解密。",
    list: "此处为全部隧道总览，日常管理请前往各服务器的「联机」页；点击已绑定的隧道行可前往对应服务器。",
    loadErr: "原有隧道与凭据仍保留在配置文件中，未被修改。在成功读取前无法添加、修改或删除隧道，以免覆盖原数据；处理后此处会自动刷新，也可点击「重试」。",
    // 程序 natfrpFrpcSteps 的几步（「回到这里点启动」改成了到联机里启动）；路径用存储位置那页的程序数据文件夹
    frpc: "1. 登录樱花穿透（SakuraFrp）管理面板，进入「软件下载」。\n2. 下载 Windows amd64 版 frpc，获取 .exe 文件（如为压缩包请先解压）。\n"
      + "3. 将该文件放入 D:\\MineDeck\\runtimes\\frpc\\natfrp，命名为 frpc.exe 或 frpc_windows_amd64.exe。\n"
      + "4. 返回服务器「联机」页启动隧道。若文件无故消失，多为杀毒软件误删，可在 Windows 安全中心的「保护历史记录」中恢复。\n"
      + "OpenFrp 与自建 frps 所需的 frpc 由 MineDeck 自动下载，无需手动配置。",
  };
  // 购买：照 accounts.go PurchaseOptions 的标题（两家都只给官网页面）
  const BUY = {
    openfrp: [["增值服务 / 会员与加量", "console.openfrp.net/premium"], ["账户中心", "account.naids.com/dashboard"]],
    sakura: [["SakuraFrp 商店", "www.natfrp.com/shop"], ["隧道套餐", "www.natfrp.com/tunnel/shop"], ["用户中心", "www.natfrp.com/user/"],
      ["获取访问密钥", "www.natfrp.com/user/profile"], ["付款说明", "doc.natfrp.com/faq/payment.html"]],
  };
  // 账户概览（假的）：OpenFrp 后端给「剩余 N MiB」、已用 / 上限，不给签到状态（这里把 MiB 换算成 GiB 显示）；
  // 樱花给今日已用、剩余两个裸数（单位没核实，这里假设能换算成 GiB）、签到、连续天数和上限，不给已用几条（所以只写上限）
  const ACC = {
    openfrp: { name: "steve", group: "VIP", tier: "vip", traffic: "剩余 18.4 GiB" },
    sakura: { name: "steve", group: "普通用户", tier: "", traffic: "今日已用 0.3 GiB · 剩余 4.7 GiB", limit: 2, sign: "今日未签 · 连续 12 天" },
  };

  let C = null, D = null;
  const srv = id => (C.servers || []).find(s => s.id === id);
  function data() {
    if (D) return D;
    const q = C.QS, acc = q.get("acc") || "", tun = q.get("tun") || "", paper = srv("paper");
    const live = !!(paper && paper.running);
    const tuns = [
      { id: "t1", name: "mc_survival", plat: "openfrp", node: "#2 香港-2", kind: "game", server: "paper", addr: (paper && paper.tunnel) || "frp-hk.openfrp.net:31234", on: live },
      { id: "t3", name: "我的隧道", plat: "sakura", pid: "778899", kind: "game", server: "", addr: "sk-7.natfrp.cloud:41522", last: true, on: false, broken: tun === "lost" },
      { id: "t4", name: "old_server", plat: "openfrp", pid: "204518", node: "#29 香港-4", kind: "game", server: "", addr: "frp-hk.openfrp.net:30512", last: true, on: false,
        err: "frpc 报错：proxy name [old_server] is already in use（同一条隧道已经在别的电脑上连着）" },
    ];
    if (tun === "many") {
      const P = ["openfrp", "sakura", "custom"];
      for (let i = 1; i <= 12; i++) {
        const p = P[i % 3], n = String(i).padStart(2, "0");
        tuns.push({ id: "m" + i, name: p === "custom" ? `home_frps_${n}` : `test_${n}`, plat: p, pid: p === "custom" ? "" : String(310000 + i * 137), lp: p === "custom" ? 25570 + i : 0, kind: i % 4 ? "game" : "voice",
          server: "", addr: p === "custom" ? `frp.example.com:${25570 + i}` : p === "sakura" ? `sk-${i}.natfrp.cloud:${40100 + i * 7}` : `frp-hk.openfrp.net:${30600 + i * 11}`,
          last: p !== "custom", on: false });
      }
    }
    D = {
      ro: q.get("ro") === "1",
      openfrp: { st: acc === "none" || acc === "lost" ? "off" : "on", lost: acc === "lost", t: 0 },
      sakura: { st: acc === "both" ? "on" : "off", key: "", busy: false, bad: false, replacing: false, t: 0 },
      frps: q.get("frps") === "set" ? { host: "frp.example.com", port: "7000", tokenSet: true } : { host: "", port: "7000", tokenSet: false },
      ofUsed: 2, ofLimit: 10,
      tuns: tun === "none" ? [] : tuns,
      loadErr: tun === "err", retrying: false,
    };
    Object.assign(D.frps, { h: D.frps.host, p: D.frps.port, tk: "", saving: false, msg: "", bad: "" });
    return D;
  }
  const root = () => document.querySelector(".cp > .stn:not(.fade-out)");
  const block = k => root()?.querySelector(`.stn-ab[data-acc="${k}"]`);

  // =====================================================================================================
  // 平台账户
  // =====================================================================================================
  const tagCls = { vip: "vip", svip: "svip" };
  function pillOf(k) {
    const d = data();
    if (k === "openfrp") {
      const a = d.openfrp;
      if (a.st === "on") return ["ok", "已连接", ""];
      if (a.st === "wait") return ["warm", "授权中", ""];
      if (a.lost) return ["warn", "凭据无法解密", TIP.lost];
      return ["off", "未连接", ""];
    }
    return d.sakura.st === "on" ? ["ok", "已连接", ""] : ["off", "未连接", ""];
  }
  const pillHTML = ([c, t, tip]) => `<span class="stn-pill ${c}" data-pill${tip ? ` title="${esc(tip)}"` : ""}><i></i>${esc(t)}</span>`;
  const roAttr = () => (data().ro ? ` aria-disabled="true" title="${esc(TIP.ro)}"` : "");
  function accHead(k) {
    const name = { openfrp: "OpenFrp", sakura: "樱花穿透", frps: "自建 frps" }[k];
    return `<span class="stn-an" title="${esc(TIP[k])}">${name}</span><span class="stn-sp"></span>`
      + (k === "frps" ? `<span class="stn-fmsg" data-fmsg></span>` : pillHTML(pillOf(k)));
  }
  // 已连接：账户、几行数、底下一排按钮。在局域网里打开时程序不拉概览：账户和数换成一句，按钮照样摆着、灰着（规范第九节：灰掉、不藏）
  const ROLINE = `<div class="stn-ph"><span class="stn-phs" title="${esc(TIP.ro)}">局域网访问时仅可查看连接状态</span></div>`;
  function actsHTML(k) {
    const ro = data().ro;
    return `<div class="stn-acts">`
      + `<button type="button" class="stn-b" data-a="sign"${ro ? roAttr() : ` title="${esc(k === "openfrp" ? TIP.signOF : TIP.signSK)}"`}>签到<i data-ic="share" class="tr"></i></button>`
      + `<button type="button" class="stn-b" data-a="buy"${ro ? roAttr() : ` title="${esc(TIP.buy)}"`} aria-haspopup="menu">购买<i data-ic="chevronDown" class="tr"></i></button>`
      + `<span class="stn-sp"></span><button type="button" class="stn-b sdg" data-a="disc"${roAttr()}>断开</button></div>`;
  }
  function onBody(k) {
    const d = data(), a = ACC[k];
    if (d.ro) return ROLINE + actsHTML(k);
    const kv = (key, v, attr = "") => `<div class="stn-kv"><span class="k">${key}</span><span class="v"${attr}>${esc(v)}</span></div>`;
    const who = `<div class="stn-who"><b>${esc(a.name)}</b><span class="stn-tag ${tagCls[a.tier] || "plain"}">${esc(a.group)}</span><span class="stn-sp"></span>`
      + (k === "sakura" ? `<button type="button" class="stn-ib" data-a="replace" title="更换访问密钥" aria-label="更换樱花穿透访问密钥"><i data-ic="key"></i></button>` : "") + `</div>`;
    const rows = k === "openfrp"
      ? kv("流量", a.traffic) + kv("隧道", `${d.ofUsed} / ${d.ofLimit}`, ` data-kv="tun"`)
      : kv("流量", a.traffic) + kv("隧道", `上限 ${a.limit} 条`) + kv("签到", a.sign);
    return who + `<div class="stn-kvs">${rows}</div>` + actsHTML(k);
  }
  // OpenFrp 没连：虚线框里一颗「浏览器授权」；等授权时换成一句和「取消等待」
  function ofPh() {
    const d = data();
    if (d.ro) return `<button type="button" class="stn-b pri" data-a="auth"${roAttr()}><i data-ic="link"></i>浏览器授权</button>`;
    if (d.openfrp.st === "wait") return `<span class="stn-phs"><i data-ic="spinner" class="stn-spin"></i>等待在浏览器中完成授权…</span>`
      + `<button type="button" class="stn-b" data-a="cancelauth">取消等待</button>`;
    return `<button type="button" class="stn-b pri" data-a="auth" title="${esc(TIP.auth)}"><i data-ic="link"></i>浏览器授权</button>`;
  }
  // 樱花没连（或者在换密钥）：同向导第四步的粘贴框；在局域网里打开时框和「连接」灰着
  function skForm() {
    const d = data(), s = d.sakura, rep = s.replacing, ro = d.ro;
    return `<div class="stn-lbr"><span class="stn-lb">访问密钥</span><span class="stn-bad" data-kmsg>${s.bad ? "请粘贴访问密钥" : ""}</span><span class="stn-sp"></span>`
      + `<a class="stn-lk" data-a="getkey" title="${esc(TIP.getKey)}">去官网复制</a>`
      + (rep ? `<button type="button" class="stn-ib sm" data-a="unreplace" title="取消更换" aria-label="取消更换访问密钥"><i data-ic="x"></i></button>` : "") + `</div>`
      + `<div class="stn-kr"><div class="stn-iw${s.bad ? " bad" : ""}"><input class="stn-kin" type="password" placeholder="${rep ? "粘贴新的访问密钥" : "粘贴访问密钥"}" value="${esc(s.key)}"`
      + ` autocomplete="off" spellcheck="false" aria-label="樱花穿透访问密钥"${s.busy || ro ? " disabled" : ""}></div>`
      + `<button type="button" class="stn-kb" data-a="skconnect"${ro ? roAttr() : s.busy ? ` aria-disabled="true"` : ""}>${s.busy ? `<i data-ic="spinner" class="stn-spin"></i>连接中` : "连接"}</button></div>`
      + (rep ? "" : `<div class="stn-frpc" title="${esc(TIP.frpc)}"><i data-ic="info"></i><span>首次使用需手动放置 frpc</span></div>`);
  }
  // 自建 frps：地址、端口、令牌，底下「保存」
  const fDirtyN = f => (f.h.trim() !== f.host) + (f.p.trim() !== f.port) + (f.tk !== ""); // 改了几项（离开前拦要报数）
  const fDirty = f => fDirtyN(f) > 0;
  function frpsBody() {
    const d = data(), f = d.frps, ro = d.ro, dis = ro ? " readonly tabindex=\"-1\"" : "";
    const dirty = fDirty(f) && !ro;
    return `<div class="stn-fr"><div class="stn-iw${f.bad === "host" ? " bad" : ""}"><input class="stn-fin" data-f="h" placeholder="frps 地址" value="${esc(f.h)}" autocomplete="off" spellcheck="false" aria-label="frps 地址"${dis}></div>`
      + `<div class="stn-iw port${f.bad === "port" ? " bad" : ""}"><input class="stn-fin" data-f="p" placeholder="端口" value="${esc(f.p)}" inputmode="numeric" autocomplete="off" aria-label="frps 端口"${dis}></div></div>`
      + `<div class="stn-iw"><input class="stn-fin" data-f="tk" type="password" placeholder="${f.tokenSet ? "已设置（留空不修改）" : "认证令牌（选填）"}" value="${esc(f.tk)}" autocomplete="off" spellcheck="false" aria-label="frps 认证令牌"${dis}>`
      + `<button type="button" class="stn-ib" data-a="tclear"${ro ? roAttr() : ` title="清除已保存的认证令牌"`} aria-label="清除已保存的认证令牌"${f.tokenSet ? "" : " hidden"}><i data-ic="trash"></i></button></div>`
      + `<div class="stn-acts"><span class="stn-sp"></span><button type="button" class="stn-b sv${dirty ? " on" : ""}" data-a="fsave" aria-disabled="${!dirty || f.saving}"`
      + `${ro ? ` title="${esc(TIP.ro)}"` : dirty ? "" : ` title="暂无未保存的修改"`}>${f.saving ? `<i data-ic="spinner" class="stn-spin"></i>` : ""}保存</button></div>`;
  }
  function accBody(k) {
    const d = data();
    if (k === "frps") return frpsBody();
    if (k === "openfrp") return d.openfrp.st === "on" ? onBody(k) : `<div class="stn-ph">${ofPh()}</div>`;
    return d.sakura.st === "on" && !d.sakura.replacing ? onBody(k) : skForm();
  }
  const accBlock = k => `<div class="stn-ab${k === "frps" ? " frps" : ""}" data-acc="${k}"><div class="stn-ah">${accHead(k)}</div><div class="stn-abd">${accBody(k)}</div></div>`;

  // =====================================================================================================
  // 全部隧道
  // =====================================================================================================
  function tunState(t) {
    if (t.broken) return ["warn", "凭据无法解密", TIP.broken];
    if (t.on) return ["ok", "运行中", ""];
    if (t.wait) return ["warm", "连接中", ""];
    if (t.err) return ["bad", "上次出错", t.err];
    return ["off", "已停止", ""];
  }
  const detailHref = s => {
    const q = new URLSearchParams({ kind: s.id, tab: "net" });
    if (document.documentElement.dataset.theme === "light") q.set("theme", "light");
    return `detail.html?${q}`;
  };
  // 一行分四格：字、地址、状态、最右边；只换变了的那几格
  function cells(t) {
    const s = t.server && srv(t.server), [c, st, tip] = tunState(t);
    const meta = [PLAT[t.plat], t.node, s ? t.name : t.pid ? "隧道 " + t.pid : t.lp ? "本地端口 " + t.lp : ""].filter(Boolean).join(" · ");
    const m = `<div class="stn-r1"><span class="stn-rn">${esc(s ? s.name : t.name)}</span><span class="stn-tag plain">${t.kind === "voice" ? "语音" : "游戏"}</span>`
      + (s ? "" : `<span class="stn-nb">未绑定服务器</span>`) + `</div><div class="stn-r2">${esc(meta)}</div>`;
    const a = t.broken || !t.addr ? `<span class="stn-na">—</span>`
      : `<span class="stn-ad"${t.last && !t.on ? ` title="${esc(TIP.last)}"` : ""}>${esc(t.addr)}</span>` + (t.last && !t.on ? `<span class="stn-tag plain" title="${esc(TIP.last)}">上次</span>` : "")
        + `<button type="button" class="stn-ib" data-a="copy" data-copy="${esc(t.addr)}" title="复制" aria-label="复制 ${esc(t.addr)}"><i data-ic="copy"></i></button>`;
    const p = pillHTML([c, st, tip]);
    const x = s ? `<i data-ic="chevronRight" class="stn-go"></i>`
      : `<button type="button" class="stn-bb" data-a="bind" aria-haspopup="menu">绑定到…</button>`
        + `<button type="button" class="stn-ib" data-a="del" title="删除" aria-label="删除隧道 ${esc(t.name)}"><i data-ic="trash"></i></button>`;
    return { m, a, p, x };
  }
  function rowAttrs(t) {
    const s = t.server && srv(t.server);
    return s ? { cls: "stn-row go", role: "link", tab: "0", href: detailHref(s), tip: `前往「${s.name}」的联机页` } : { cls: "stn-row", role: "", tab: "", href: "", tip: "" };
  }
  function rowHTML(t) {
    const c = cells(t), r = rowAttrs(t);
    return `<div class="${r.cls}" data-t="${t.id}"${r.role ? ` role="link" tabindex="0" data-href="${esc(r.href)}" title="${esc(r.tip)}"` : ""}>`
      + `<div class="stn-rm">${c.m}</div><div class="stn-ra">${c.a}</div><div class="stn-rs">${c.p}</div><div class="stn-rx">${c.x}</div></div>`;
  }
  function listHTML() {
    const d = data();
    if (d.loadErr) return `<div class="stn-err"><span class="stn-ei"><i data-ic="warn"></i></span><span class="stn-et" title="${esc(TIP.loadErr)}">无法读取隧道列表</span>`
      + `<button type="button" class="stn-bb" data-a="retry"${d.retrying ? ` aria-disabled="true"` : ""}><i data-ic="${d.retrying ? "spinner" : "refresh"}"${d.retrying ? ` class="stn-spin"` : ""}></i>重试</button></div>`;
    if (!d.tuns.length) return `<div class="stn-empty">暂无隧道</div>`;
    return d.tuns.map(rowHTML).join("");
  }
  const countTxt = () => (data().loadErr || !data().tuns.length ? "" : String(data().tuns.length));

  function html(ctx) {
    C = ctx; data();
    return `<div class="stn"><section class="stn-acc" aria-label="平台账号">${["openfrp", "sakura", "frps"].map(accBlock).join("")}</section>`
      + `<section class="stn-tl" aria-label="全部隧道"><div class="stn-th"><span class="stn-tt" title="${esc(TIP.list)}">全部隧道</span><span class="stn-tc" data-count>${countTxt()}</span></div>`
      + `<div class="stn-lw"><div class="list stn-list">${listHTML()}</div><span class="thumb"></span></div></section></div>`;
  }

  // =====================================================================================================
  // 只换动了的那一处
  // =====================================================================================================
  // 一块里的几样逐项换：旧的往上 5 像素淡掉（0.09 秒，错开 22 毫秒），新的从下面 5 像素一样一样淡进来（0.22 秒）
  function swapKids(box, html, after) {
    if (!box) return;
    const tok = box._tok = (box._tok || 0) + 1, olds = [...box.children];
    olds.forEach((x, i) => {
      x.getAnimations().forEach(a => a.cancel());
      x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, delay: i * 22, easing: EASE_IN, fill: "forwards" });
    });
    setTimeout(() => {
      if (box._tok !== tok || !box.isConnected) return;
      box.innerHTML = html; C.KIT.hydrate(box); after && after();
      [...box.children].forEach((x, i) => x.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
    }, olds.length ? 90 + (olds.length - 1) * 22 : 0);
  }
  // 一格换掉（字不同才换）
  function swapCell(el, html) {
    if (!el || el._h === html) return;
    el._h = html;
    const tok = el._tok = (el._tok || 0) + 1;
    el.getAnimations().forEach(a => a.cancel());
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
      if (el._tok !== tok) return;
      el.innerHTML = html; C.KIT.hydrate(el); el.getAnimations().forEach(a => a.cancel());
      el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
    };
  }
  function paintPill(k) {
    const b = block(k); if (!b) return;
    const old = b.querySelector("[data-pill]"), want = pillOf(k);
    if (old.textContent === want[1]) { setTip(old, want[2]); return; }
    const wrap = document.createElement("span"); wrap.innerHTML = pillHTML(want); const nw = wrap.firstChild;
    old.replaceWith(nw);
    nw.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
  }
  const paintBody = k => { const b = block(k); if (b) swapKids(b.querySelector(".stn-abd"), accBody(k)); };
  const paintAcc = k => { paintPill(k); paintBody(k); };
  // OpenFrp 没连时只换虚线框里面
  function paintPh() {
    const b = block("openfrp"), ph = b?.querySelector(".stn-ph");
    if (!ph) { paintBody("openfrp"); return; }
    swapKids(ph, ofPh());
  }
  function paintKvTun() {
    const el = block("openfrp")?.querySelector('[data-kv="tun"]'); if (!el) return;
    const t = `${data().ofUsed} / ${data().ofLimit}`; if (el.textContent === t) return;
    el.textContent = t; el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
  }
  // 樱花的粘贴框：连接中、空着点了这几样只改属性和那一句，框不重画（光标、输入法不断）
  function paintSkForm() {
    const b = block("sakura"); if (!b) return; const s = data().sakura;
    const iw = b.querySelector(".stn-iw"), inp = b.querySelector(".stn-kin"), kb = b.querySelector(".stn-kb"), msg = b.querySelector("[data-kmsg]");
    if (!iw || !kb) return;
    iw.classList.toggle("bad", s.bad); inp.disabled = s.busy;
    const m = s.bad ? "请粘贴访问密钥" : ""; if (msg.textContent !== m) msg.textContent = m;
    const want = s.busy ? `${C.KIT.icon("spinner", "stn-spin")}连接中` : "连接";
    if (kb._h !== want) { kb._h = want; kb.innerHTML = want; }
    setAttr(kb, "aria-disabled", s.busy);
  }
  function paintFrps() {
    const b = block("frps"); if (!b) return; const d = data(), f = d.frps;
    const [ih, ip] = b.querySelectorAll(".stn-fr .stn-iw"), tk = b.querySelector('[data-f="tk"]'), cl = b.querySelector('[data-a="tclear"]'), sv = b.querySelector('[data-a="fsave"]');
    ih.classList.toggle("bad", f.bad === "host"); ip.classList.toggle("bad", f.bad === "port");
    const ph = f.tokenSet ? "已设置（留空不修改）" : "认证令牌（选填）"; if (tk.placeholder !== ph) tk.placeholder = ph;
    if (tk.value !== f.tk) tk.value = f.tk;
    b.querySelectorAll('[data-f="h"], [data-f="p"]').forEach(i => { const v = f[i.dataset.f]; if (i.value !== v) i.value = v; });
    cl.hidden = !f.tokenSet;
    const dirty = fDirty(f) && !d.ro;
    sv.classList.toggle("on", dirty); setAttr(sv, "aria-disabled", !dirty || f.saving); setTip(sv, d.ro ? TIP.ro : dirty ? "" : "暂无未保存的修改");
    const want = `${f.saving ? C.KIT.icon("spinner", "stn-spin") : ""}保存`; if (sv._h !== want) { sv._h = want; sv.innerHTML = want; }
    const m = b.querySelector("[data-fmsg]"); if (m.textContent !== f.msg) m.textContent = f.msg;
  }
  function paintCount() {
    const el = root()?.querySelector("[data-count]"); if (!el) return;
    const t = countTxt(); if (el.textContent === t) return;
    el.textContent = t; el.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
  }
  function paintRow(t) {
    const el = root()?.querySelector(`.stn-row[data-t="${t.id}"]`); if (!el) return;
    const c = cells(t), r = rowAttrs(t);
    el.className = r.cls;
    if (r.role) { el.setAttribute("role", "link"); el.tabIndex = 0; el.dataset.href = r.href; setTip(el, r.tip); }
    else { el.removeAttribute("role"); el.removeAttribute("tabindex"); delete el.dataset.href; setTip(el, ""); }
    swapCell(el.querySelector(".stn-rm"), c.m); swapCell(el.querySelector(".stn-ra"), c.a); swapCell(el.querySelector(".stn-rs"), c.p); swapCell(el.querySelector(".stn-rx"), c.x);
  }
  // 画出来的每一格记下它的源码（换了图标以后 innerHTML 和源码对不上），paintRow 只换源码变了的那几格
  function stamp(scope) {
    scope?.querySelectorAll(".stn-row[data-t]").forEach(el => {
      const t = data().tuns.find(x => x.id === el.dataset.t); if (!t) return; const c = cells(t);
      el.querySelector(".stn-rm")._h = c.m; el.querySelector(".stn-ra")._h = c.a; el.querySelector(".stn-rs")._h = c.p; el.querySelector(".stn-rx")._h = c.x;
    });
  }
  const repaintBar = () => root()?.querySelector(".stn-lw")?._paint?.();
  // 拿掉一行：往右划 32 像素淡掉（0.18 秒），再收起
  function dropRow(t) {
    const d = data(), i = d.tuns.indexOf(t); if (i < 0) return;
    d.tuns.splice(i, 1);
    const el = root()?.querySelector(`.stn-row[data-t="${t.id}"]`);
    paintCount();
    if (!el) return;
    el.style.pointerEvents = "none";
    el.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: "translateX(32px)" }], { duration: 180, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
      const h = el.offsetHeight, mt = parseFloat(getComputedStyle(el).marginTop) || 0;
      el.style.overflow = "hidden";
      el.animate([{ height: h + "px", marginTop: mt + "px" }, { height: "0px", marginTop: "0px" }], { duration: 180, easing: SLIDE, fill: "forwards" }).onfinish = () => {
        const list = el.parentElement; el.remove();
        if (list && !data().tuns.length && !data().loadErr) {
          list.innerHTML = listHTML();
          list.firstElementChild?.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
        }
        repaintBar();
      };
    };
  }

  // =====================================================================================================
  // 菜单（同共用件页那张：外圆角 16、细边 1 加内边距 5，项 36 高、圆角 10），挂在 body 上
  // =====================================================================================================
  let menu = null;
  function openMenu(anchor, inner, onPick) {
    if (menu && menu._a === anchor) { closeMenu(); return; }
    closeMenu(true);
    const m = Object.assign(document.createElement("div"), { className: "stn-dm enter" }); m.setAttribute("role", "menu");
    m.innerHTML = inner; C.KIT.hydrate(m); document.body.appendChild(m);
    const r = anchor.getBoundingClientRect(), w = m.offsetWidth, h = m.offsetHeight, up = r.bottom + 4 + h > innerHeight - 8;
    m.classList.toggle("up", up);
    m.style.left = Math.round(Math.min(Math.max(8, r.right - w), innerWidth - w - 8)) + "px";
    m.style.top = Math.round(up ? r.top - 4 - h : r.bottom + 4) + "px";
    anchor.classList.add("open"); anchor.setAttribute("aria-expanded", "true");
    m._a = anchor; menu = m;
    m.addEventListener("click", e => {
      const it = e.target.closest(".stn-it"); if (!it || it.getAttribute("aria-disabled") === "true") return;
      closeMenu(); onPick && onPick(it);
    });
  }
  function closeMenu(now) {
    if (!menu) return; const m = menu; menu = null;
    m._a.classList.remove("open"); m._a.setAttribute("aria-expanded", "false");
    if (now) { m.remove(); return; }
    m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90);
  }
  function bindMenu(t) {
    const items = (C.servers || []).map(s => {
      const taken = data().tuns.find(x => x !== t && x.server === s.id && x.kind === t.kind);
      const why = taken ? `该服务器已绑定${t.kind === "voice" ? "语音" : "游戏"}隧道（每台服务器最多绑定游戏、语音隧道各一条）`
        : t.kind === "voice" && !s.voice ? "该服务器未安装 Simple Voice Chat" : "";
      return `<div class="stn-it" role="menuitem" data-sid="${esc(s.id)}"${why ? ` aria-disabled="true" title="${esc(why)}"` : ""}>`
        + `<span class="stn-in">${esc(s.name)}</span><span class="stn-ip">${s.port}</span></div>`;
    }).join("");
    return `<div class="stn-mhd">绑定到</div>${items}`;
  }
  const buyMenu = k => BUY[k].map(([t, u]) => `<div class="stn-it" role="menuitem" title="${esc(u)}"><i data-ic="share"></i><span class="stn-in">${esc(t)}</span></div>`).join("");

  // =====================================================================================================
  // 确认框：同共用件页（照 detail-world.css 的 .wd-mask / 联机页的删除确认）；危险的默认落在「取消」，别的落在主键
  // =====================================================================================================
  let mask = null;
  const tglHTML = (t, on, tip) => `<button type="button" class="stn-tg${on ? " on" : ""}" role="switch" aria-checked="${on}"${tip ? ` title="${esc(tip)}"` : ""}><span class="stn-sw"></span>${esc(t)}</button>`;
  function ask({ title, body = "", ok, danger = true, icon = "trash", toggle = null, onOk }) {
    closeAsk(true); closeMenu(true);
    const st = { on: toggle ? toggle.on : false };
    const mk = mask = Object.assign(document.createElement("div"), { className: "stn-mask enter" });
    mk.innerHTML = `<div class="stn-md" role="dialog" aria-modal="true" aria-label="${esc(title)}"><div class="stn-mc">`
      + `<div class="stn-mh${danger ? " wi" : ""}">${danger ? `<span class="stn-mi"><i data-ic="${icon}"></i></span>` : ""}<h3>${esc(title)}</h3></div>`
      + (body ? `<div class="stn-mb">${body}</div>` : "") + (toggle ? `<div>${tglHTML(toggle.text, st.on, toggle.tip)}</div>` : "")
      + `</div><div class="stn-ma"><button type="button" class="stn-mbt${danger ? " fk" : ""}" data-x="no">取消</button>`
      + `<button type="button" class="stn-mbt ${danger ? "dgr" : "pri fk"}" data-x="ok">${esc(ok)}</button></div></div>`;
    C.KIT.hydrate(mk); document.body.appendChild(mk);
    const fk = mk.querySelector(".fk"); fk.focus({ focusVisible: false });
    // 默认落在的那颗画一圈细框（鼠标点开时浏览器不画），一动别的就收（同共用件页）
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) fk.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") fk.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return;
      const tg = e.target.closest(".stn-tg");
      if (tg) { st.on = !st.on; tg.classList.toggle("on", st.on); tg.setAttribute("aria-checked", st.on); return; }
      if (e.target.closest("[data-x=ok]")) { closeAsk(); onOk(st.on); return; }
      if (e.target.closest("[data-x=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }

  // =====================================================================================================
  // 右上角的提示（同共用件页、存储位置那页）：带「撤销」的停 20 秒，鼠标停在上面不走
  // =====================================================================================================
  let tbox = null;
  function toast(msg, act) {
    if (!tbox || !tbox.isConnected) { tbox = Object.assign(document.createElement("div"), { id: "stn-toasts" }); tbox.setAttribute("aria-live", "polite"); document.body.appendChild(tbox); }
    const d = document.createElement("div"); d.className = "stn-tst enter";
    d.innerHTML = `<span class="stn-ti"><i data-ic="check"></i></span><span class="stn-tm">${esc(msg)}</span><button type="button" class="stn-tx" data-tx aria-label="关闭提示"><i data-ic="x"></i></button>`
      + `<span class="stn-ta"><button type="button" class="stn-tb" data-ta aria-label="${esc(act.aria)}">${esc(act.label)}</button></span>`;
    C.KIT.hydrate(d); tbox.appendChild(d);
    let left = 20000, timer = 0, since = 0, gone = false;
    const kill = () => { if (gone) return; gone = true; clearTimeout(timer); leaveToast(d); };
    const run = () => { if (gone) return; since = Date.now(); timer = setTimeout(kill, left); };
    d.addEventListener("mouseenter", () => { clearTimeout(timer); left = Math.max(0, left - (Date.now() - since)); });
    d.addEventListener("mouseleave", () => { left = Math.max(left, 4000); run(); });
    d.addEventListener("click", e => {
      if (e.target.closest("[data-tx]")) { kill(); return; }
      if (e.target.closest("[data-ta]")) { act.run(); kill(); }
    });
    run();
    return kill;
  }
  function leaveToast(d) {
    d.classList.remove("enter"); d.classList.add("leave");
    setTimeout(() => {
      const sibs = [...tbox.children].filter(x => x !== d), before = new Map(sibs.map(x => [x, x.getBoundingClientRect().top]));
      d.remove();
      sibs.forEach(x => { const dy = before.get(x) - x.getBoundingClientRect().top; if (Math.abs(dy) > .5) x.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], { duration: 320, easing: SLIDE }); });
    }, 140);
  }

  // =====================================================================================================
  // 拦下一步：框标红、晃一下（0.36 秒，−6、5、−3 像素）、光标落过去
  // =====================================================================================================
  function shake(el) {
    if (!el) return;
    el.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
      { duration: 360, easing: "ease-out" });
  }

  // =====================================================================================================
  // 动作
  // =====================================================================================================
  // 存自建 frps（那颗「保存」和外壳的「离开前拦」共用）：填错的就地拦下，返回 false；草图里装作等了一下请求，返回 Promise，存好了是 true
  let frpsP = null;
  function saveFrps() {
    const f = data().frps, bk = block("frps"); if (f.saving) return frpsP;
    const host = f.h.trim(), port = f.p.trim(), pn = +port;
    if (!host) { f.bad = "host"; f.msg = "请填写 frps 地址"; paintFrps(); const w = bk.querySelectorAll(".stn-fr .stn-iw")[0]; shake(w); w.querySelector("input").focus(); return false; }
    if (!/^\d+$/.test(port) || pn < 1 || pn > 65535) { f.bad = "port"; f.msg = "端口需在 1–65535 之间"; paintFrps(); const w = bk.querySelectorAll(".stn-fr .stn-iw")[1]; shake(w); w.querySelector("input").focus(); return false; }
    f.bad = ""; f.msg = ""; f.saving = true; paintFrps();
    return frpsP = new Promise(ok => setTimeout(() => {
      f.saving = false; f.host = host; f.port = port; f.h = host; f.p = port; if (f.tk) { f.tokenSet = true; f.tk = ""; } paintFrps(); ok(true);
    }, 450));
  }
  // 放弃自建 frps 没存的改动：三个框换回存过的样子，拦下时标的红和那句话一起收掉
  function discardFrps() {
    if (!D) return; const f = D.frps;
    f.h = f.host; f.p = f.port; f.tk = ""; f.bad = ""; f.msg = ""; paintFrps();
  }
  function act(a, b) {
    const d = data(), k = b.closest(".stn-ab")?.dataset.acc;
    switch (a) {
      case "auth": { // 草图：不真打开浏览器，等 2.6 秒算授权好了
        const o = d.openfrp; o.st = "wait"; paintPill("openfrp"); paintPh();
        clearTimeout(o.t);
        o.t = setTimeout(() => { if (o.st !== "wait") return; o.st = "on"; o.lost = false; paintAcc("openfrp"); }, 2600);
        break;
      }
      case "cancelauth": { const o = d.openfrp; clearTimeout(o.t); o.st = "off"; paintPill("openfrp"); paintPh(); break; }
      case "sign": case "getkey": break; // 草图里不打开外部网页
      case "buy": openMenu(b, buyMenu(k), () => {}); break; // 点了哪一项都只是收起（草图里不打开网页）
      case "disc": {
        const sk = k === "sakura";
        ask({ title: `断开${platIn(k, " ", "")}？`, ok: "断开", icon: "warn",
          body: sk ? "将清除本机保存的樱花穿透访问密钥。已导入 MineDeck 的樱花穿透隧道中仍保存有该密钥，如需彻底清除请删除对应隧道，必要时可前往樱花穿透官网重置访问密钥。"
            : "将清除本机保存的平台凭据，已导入 MineDeck 的隧道不会被删除。",
          onOk: () => { d[k].st = "off"; if (sk) { d.sakura.replacing = false; d.sakura.key = ""; d.sakura.bad = false; } paintAcc(k); } });
        break;
      }
      case "replace": d.sakura.replacing = true; d.sakura.key = ""; d.sakura.bad = false; paintBody("sakura"); break;
      case "unreplace": d.sakura.replacing = false; d.sakura.key = ""; d.sakura.bad = false; paintBody("sakura"); break;
      case "skconnect": {
        const s = d.sakura; if (s.busy) return;
        if (!s.key.trim()) { s.bad = true; paintSkForm(); const bk = block("sakura"); shake(bk?.querySelector(".stn-iw")); bk?.querySelector(".stn-kin")?.focus(); return; }
        s.busy = true; s.bad = false; paintSkForm();
        clearTimeout(s.t);
        s.t = setTimeout(() => { s.busy = false; s.st = "on"; s.replacing = false; s.key = ""; paintAcc("sakura"); }, 1200);
        break;
      }
      case "fsave": saveFrps(); break;
      case "tclear":
        ask({ title: "清除已保存的认证令牌？", ok: "清除令牌",
          body: "清除后连接自建 frps 将不再携带认证令牌。若 frps 启用了令牌验证，将无法连接，届时可重新填写。",
          onOk: () => { d.frps.tokenSet = false; d.frps.tk = ""; paintFrps(); } });
        break;
      case "copy": {
        if (b.classList.contains("done")) return;
        b.classList.add("done"); b.innerHTML = C.KIT.icon("check");
        setTimeout(() => { b.classList.remove("done"); b.innerHTML = C.KIT.icon("copy"); }, 1200);
        break;
      }
      case "bind": {
        const t = d.tuns.find(x => x.id === b.closest(".stn-row").dataset.t); if (!t) return;
        openMenu(b, bindMenu(t), it => {
          const s = srv(it.dataset.sid); if (!s || !d.tuns.includes(t)) return;
          t.server = s.id; t.on = false; paintRow(t);
          toast(`已绑定到「${s.name}」`, { label: "撤销", aria: `撤销将「${t.name}」绑定到「${s.name}」`, run: () => { if (!d.tuns.includes(t)) return; t.server = ""; t.on = false; paintRow(t); } });
        });
        break;
      }
      case "del": {
        const t = d.tuns.find(x => x.id === b.closest(".stn-row").dataset.t); if (!t) return;
        const plat = t.plat !== "custom" ? PLAT[t.plat] : "";
        ask({ title: `删除${t.kind === "voice" ? "语音" : "游戏"}隧道「${t.name}」？`, ok: "删除隧道",
          toggle: plat ? { text: `同时删除${platIn(t.plat)}上的这条隧道`, on: true, tip: `关闭时仅删除 MineDeck 中的本地配置，${platIn(t.plat, "")}上的隧道将保留，之后可在服务器「联机」页的「已有隧道」中重新选用。` } : null,
          onOk: both => { if (both && t.plat === "openfrp") { d.ofUsed = Math.max(0, d.ofUsed - 1); paintKvTun(); } dropRow(t); } });
        break;
      }
      case "retry": {
        if (d.retrying) return;
        d.retrying = true; const r = root(), list = r?.querySelector(".stn-list"); if (list) { list.innerHTML = listHTML(); C.KIT.hydrate(list); }
        setTimeout(() => {
          d.retrying = false; d.loadErr = false;
          const l = root()?.querySelector(".stn-list"); if (!l) return;
          l.innerHTML = listHTML(); C.KIT.hydrate(l); stamp(l); paintCount();
          const lb = l.getBoundingClientRect();
          [...l.children].filter(c => c.getBoundingClientRect().top < lb.bottom).forEach((c, i) =>
            c.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
          repaintBar();
        }, 700);
        break;
      }
    }
  }

  // =====================================================================================================
  // 事件（绑在这一页的根上；菜单、确认框、提示挂在 body 上，各自带事件）
  // =====================================================================================================
  function onClick(e) {
    const b = e.target.closest("[data-a]");
    if (b) { if (b.getAttribute("aria-disabled") === "true") return; act(b.dataset.a, b); return; }
    const iw = e.target.closest(".stn-iw"); if (iw && e.target === iw) { iw.querySelector("input")?.focus(); return; }
    const row = e.target.closest(".stn-row.go"); if (row && row.dataset.href) C.go(row.dataset.href); // 跳到别的页走外壳的 go：有没存的改动先问
  }
  function onInput(e) {
    const d = data(), t = e.target;
    if (t.classList.contains("stn-kin")) { d.sakura.key = t.value; if (d.sakura.bad) { d.sakura.bad = false; paintSkForm(); } return; }
    const f = t.dataset.f; if (!f) return;
    d.frps[f] = t.value; if (d.frps.bad && (f === "h" && d.frps.bad === "host" || f === "p" && d.frps.bad === "port")) { d.frps.bad = ""; d.frps.msg = ""; }
    paintFrps();
  }
  function onKey(e) {
    if (e.key !== "Enter" || e.isComposing) return;
    if (e.target.classList?.contains("stn-kin")) { e.preventDefault(); block("sakura")?.querySelector('[data-a="skconnect"]')?.click(); return; }
    if (e.target.dataset?.f) { e.preventDefault(); const sv = block("frps")?.querySelector('[data-a="fsave"]'); if (sv && sv.getAttribute("aria-disabled") !== "true") sv.click(); return; }
    if (e.target.matches?.(".stn-row.go")) { e.preventDefault(); C.go(e.target.dataset.href); }
  }
  // 点到菜单和它的按钮以外就收起；Esc 收菜单、确认框；滚动、改窗口大小也收菜单（菜单是固定在窗口上的）
  document.addEventListener("pointerdown", e => { if (menu && !e.target.closest(".stn-dm") && !menu._a.contains(e.target)) closeMenu(); }, true);
  document.addEventListener("keydown", e => { if (e.key === "Escape") { if (menu) closeMenu(); else if (mask) closeAsk(); } });
  addEventListener("resize", () => closeMenu(true));

  function mount(ctx) {
    C = ctx; const r = root(); if (!r) return;
    r.addEventListener("click", onClick);
    r.addEventListener("input", onInput);
    r.addEventListener("keydown", onKey);
    const w = r.querySelector(".stn-lw");
    stamp(w); C.thinBar(w);
    w.querySelector(".stn-list").addEventListener("scroll", () => closeMenu(), { passive: true });
  }

  // 离开前拦（kit.js 第 6 条）：只有自建 frps 是要点「保存」的；在局域网里打开时改不了、正在存的那一下，都不算没存
  window.SETTINGS_SECS.tunnels = { html, mount, dirty: () => (D && !D.ro && !D.frps.saving ? fDirtyN(D.frps) : 0), save: saveFrps, discard: discardFrps };
})();
