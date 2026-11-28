// 服务器详情页 · 联机（11-27 第一稿，作者：「把玩家、联机这两页做了」）。
// 计划书要它和新建向导第四步对齐：上面同样三张卡（局域网 / 自建 frps、OpenFrp、樱花穿透），下面跟着选的那张换；数值、节点面板、虚拟局域网、
// 拼好的公网地址都照 wizard.html 第四步（那里的注释写了每样东西从平台接口哪一项来）。和向导不一样的：
//   · 卡片的选中框是「正在看哪张」，不是「选了哪种」。游戏隧道建在哪个平台，那张卡第二行写隧道的状态；打开这页先停在那张，没有隧道停在局域网。
//   · 有隧道的平台，卡片下面换成这条隧道自己的一页：公网联机地址（大字、能复制）、节点、跟随服务器启动、开停，「···」里在平台上打开、删除隧道。
//     装了 Simple Voice Chat 的服务器旁边再一块语音隧道（作者 11-26 定：一台最多游戏、语音各一条，语音那一行只在装了它的服务器上出现）。
//   · 没有隧道的平台照向导的节点面板，右上角一颗主键：新建隧道时「建隧道」，已有隧道时「用这条」；节点、隧道没选时灰着。
//     游戏隧道在别的平台上时，这颗键叫「换到 樱花穿透」：建好以后原来那条停掉、解绑，平台上的不删（悬停里说）。
//   · 「谁能进」不在这一页：白名单在「玩家」，正版验证在「设置 → 常用」（一个值只在一处改）。
//   · 11-29 作者定：自建 frps 的地址、端口、令牌只在「设置 → 内网穿透」里填，这里只留开关和公网端口。设置里存过时，开关打开后大字拼出公网地址、
//     下面一个「公网端口」框；框下面一行淡字写用的是哪个服务端，后面一颗齿轮去设置。没存过时开关灰着拨不动，框下面一句「尚未配置自建 frps」和「前往设置」。
//     「保存并连接」照旧在右下角，只管开关和公网端口：第一次存之前开着就亮，存过以后公网端口改了才亮；公网端口填错（不是 1～65535 的整数）就地拦下。
//     去设置走 ctx.go（经过外壳的离开前拦），地址带上配色参数和 frps，设置页看到的是同一个状态。
// 草图参数（左下角第二条「自建 frps」切换）：&frps=set 表示设置里存过（示例同设置页：frp.example.com、7000、令牌已设置）；不带就是没配置。
// 平台账户、节点、平台上的隧道、公网地址都是假的（节点名照两家官网抄，同向导）；Paper 那台有一条 OpenFrp 的游戏隧道，和页头的地址对得上。
(() => {
Object.assign(window.EXTRA_ICONS = window.EXTRA_ICONS || {}, {
  wifi: '<path d="M1.6 6.2a9 9 0 0112.8 0M3.8 8.6a5.8 5.8 0 018.4 0M6 11a2.7 2.7 0 014 0"/><circle cx="8" cy="13.3" r=".9" fill="currentColor" stroke="none"/>',
  voice: '<rect x="5.8" y="1.8" width="4.4" height="7.6" rx="2.2"/><path d="M3.4 7.6a4.6 4.6 0 009.2 0M8 12.2v2"/>',
});
window.DETAIL_TABS = window.DETAIL_TABS || {};
let C = null; // 外壳传来的 ctx（这台服务器的样子和几个小工具），每次 html / mount 都换成最新的
const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
const LAN_IP = "192.168.1.5";
const LAN_NETS = [["Radmin VPN", "26.18.4.77"], ["ZeroTier", "10.147.20.3"], ["Tailscale", "100.88.12.5"], ["Hamachi", "25.61.140.9"], ["蒲公英", "172.16.3.21"]];
const VIAS = [["lan", "局域网 / 自建 frps"], ["openfrp", "OpenFrp"], ["sakura", "樱花穿透"]];
const PLAT = { openfrp: "OpenFrp", sakura: "樱花穿透" };
const platIn = (k, l = " ", r = " ") => /^[A-Za-z]/.test(PLAT[k]) ? l + PLAT[k] + r : PLAT[k]; // 平台名夹在中文句子里：英文开头的（OpenFrp）和旁边的汉字之间带空格，中文的不带；挨着标点或句末的那一边传 ""（同 settings-tunnels.js）

// ---- 节点（同向导第四步，节选；标记同那里：u UDP、h HTTPS、e 扩展地址、r 实名、f 满载、o 离线） ----
const OF_USER = { name: "steve", tier: "vip", realname: true };
const TIERS = ["", "vip", "svip"], TIER_NAME = { vip: "VIP", svip: "SVIP" };
const OF_SECS = [["中国大陆", "均需实名"], ["中国香港 / 中国台湾 / 中国澳门", "免备建站"], ["海外", "不建议中国大陆使用"]];
const OF = (id, n, d, sec, tier, bw, f = "") => ({ key: "#" + id, id, n, d, sec, tier, bw, udp: f.includes("u"), https: f.includes("h"), ext: f.includes("e"),
  real: sec === 0 || f.includes("r"), full: f.includes("f"), off: f.includes("o"), host: sec === 1 ? "frp-hk.openfrp.net" : `frp-${id}.openfrp.net` });
const OF_NODES = [
  OF(6, "厦门多线", "限时开放 | 封 UDP", 0, "svip", "100 Mbps"), OF(7, "台州多线", "建站需人工过白", 0, "svip", "100 Mbps", "uhe"),
  OF(8, "镇江多线", "无防", 0, "vip", "50 Mbps", "u"), OF(9, "义乌电信家宽", "周一周五早四点重拨换 IP，注意防失联（高负载）", 0, "", "100 Mbps", "u"),
  OF(10, "芜湖联通", "[限量]", 0, "vip", "50 Mbps", "u"), OF(11, "广州移动", "[限量] 跨网优秀", 0, "svip", "100 Mbps", "uf"),
  OF(12, "绍兴多线", "大水管 BGP 全国优秀", 0, "svip", "100 Mbps", "uf"), OF(14, "温州多线", "大水管推荐", 0, "svip", "100 Mbps", "u"),
  OF(15, "九江电信", "[限量]", 0, "vip", "50 Mbps", "u"), OF(17, "宁波电信", "主地址已封禁 UDP，如需连接 UDP 隧道，连接方需使用扩展地址连接（隧道详情查看）", 0, "vip", "100 Mbps", "ue"),
  OF(18, "泉州电信", "端口限制 5w-6w", 0, "", "30 Mbps × 0.8", "uf"), OF(19, "西安电信", "西南 F3", 0, "vip", "50 Mbps", "uh"),
  OF(22, "常州电信", "已调整线路", 0, "vip", "100 Mbps", "u"), OF(24, "昆明电信", "西南 +1", 0, "vip", "50 Mbps", "u"),
  OF(28, "郴州电信", "端口限制 5w-6w", 0, "", "50 Mbps", "u"), OF(34, "上海电信", "复活赛打赢了", 0, "svip", "100 Mbps", "uh"),
  OF(36, "济南联通", "", 0, "vip", "100 Mbps", "u"), OF(45, "北京移动", "端口限制 5w-6w", 0, "", "100 Mbps", "uo"),
  OF(1, "香港-1", "极致三网优化", 1, "svip", "30 Mbps", "uhr"), OF(2, "香港-2", "尽力优化", 1, "vip", "30 Mbps", "ur"),
  OF(3, "香港-3", "尽力优化", 1, "vip", "30 Mbps", "uhrf"), OF(29, "香港-4", "无优化，大陆最好别用", 1, "", "1 Gbps", "uh"),
  OF(4, "韩国-首尔", "拔线喽", 2, "", "4 Gbps × 5", "uh"), OF(5, "韩国-春川", "移动还行", 2, "", "4 Gbps × 10", "uh"), OF(42, "日本-东京", "", 2, "", "1 Gbps", "uh"),
];
const SK_USER = { name: "steve", level: 0 };
const SK_SECS = [["普通节点（内地）", true], ["普通节点（其他）", false]];
const SK = (n, d, f = "", i = 0) => ({ key: n, n, d, vip: 0, pub: f.includes("p"), noudp: f.includes("u"), beta: f.includes("b"), inland: f.includes("i"), full: false, off: false,
  host: `sk-${i}.natfrp.cloud` });
const D_DNS = "三网 DNS 分流，请通过节点域名连接", D_GEN = "通用穿透节点", D_LT = "非联通宽带谨慎使用，如遇问题建议换三线节点", D_SEA = "只推荐海外用户使用";
const SK_NODES = [
  SK("江苏镇江多线PLUS-扩容1", "游戏专用", "pui", 1), SK("济南三线PLUS4", D_DNS, "i", 2), SK("宁波多线PLUS-扩容1", "屏蔽海外 | 游戏专用", "pui", 3), SK("合肥电信PLUS2", D_GEN, "i", 4),
  SK("济南联通PLUS-扩容1", D_LT, "i", 5), SK("长沙电信PLUS4", D_GEN, "i", 6), SK("浙江移动PLUS1", D_GEN, "i", 7), SK("福建三线PLUS1", D_DNS, "i", 8),
  SK("越南1", D_SEA, "", 9), SK("日本1", D_SEA, "", 10), SK("韩国1", D_SEA, "", 11), SK("新加坡1", D_SEA, "", 12),
];
const nodesOf = k => k === "openfrp" ? OF_NODES : SK_NODES;
const nodeBy = (k, key) => nodesOf(k).find(x => x.key === key);
// 平台上已经建好的隧道（假的，同向导）：本地端口对得上这台服务器的 TCP 隧道才能用
const PLAT_TUNS = {
  openfrp: [{ n: "mc_survival", t: "TCP", node: "#2", lp: 25565 }, { n: "old_server", t: "TCP", node: "#29", lp: 25566 }],
  sakura: [{ n: "survival_01", t: "TCP", node: "浙江移动PLUS1", lp: 25565 }, { n: "voice_01", t: "UDP", node: "浙江移动PLUS1", lp: 24454 }],
};

// ---- 这一页的状态：每台服务器一份（草图里切核心是换一台） ----
const NETS = {};
function net() {
  const k = C.kind;
  if (!NETS[k]) {
    const st = C.st, paper = k === "paper";
    NETS[k] = {
      // 自建 frps：服务端（地址、端口、令牌）从设置里读（FRPS()），这里只记开关和公网端口；saved 是上次点「保存」时的公网端口，fbad 是公网端口填错被拦着
      via: paper ? "openfrp" : "lan", vlan: 0, frpsOn: false, frps: { remote: "", saved: "", fbad: false }, frpsSaved: false,
      sakura: false, connecting: false, key: "", saveKey: true, tab: { openfrp: "new", sakura: "new" }, filter: "all", q: "", show: { full: false, off: false, perm: false },
      pick: { openfrp: "", sakura: "" }, use: { openfrp: "", sakura: "" }, swap: false, busy: false,
      // Paper 这台：一条 OpenFrp 的游戏隧道（和页头的地址对得上），装了 Simple Voice Chat、语音隧道还没建
      game: paper ? { plat: "openfrp", name: "mc_survival", node: "#2", port: 31234, follow: true, on: st !== "stopped", wait: st === "starting" } : null,
      svc: paper, voice: null,
    };
  }
  return NETS[k];
}
const PORT = () => C.S.port;
const addrOf = t => `${nodeBy(t.plat, t.node).host}:${t.port}`;

// ---- 卡片 ----
function tunState(t) { return t.wait ? ["warm", "连接中"] : t.on ? ["ok", "运行中"] : ["off", "已停止"]; }
function viaMeta(k) {
  const N = net();
  if (k === "lan") return "同一 Wi-Fi / 连接自有服务端"; // 向导里作者定过的字
  if (N.game && N.game.plat === k) { const [c, t] = tunState(N.game); return `<i class="dot ${c}"></i>游戏隧道${t}`; }
  if (k === "openfrp") return `<i class="dot ok"></i>${OF_USER.name} · ${TIER_NAME[OF_USER.tier] || "普通用户"}`;
  return N.sakura ? `<i class="dot ok"></i>${SK_USER.name}` : `<i class="dot"></i>未连接`;
}
const VIA_TIP = {
  lan: "同一局域网内可直接联机；如有自有 frps，开启「自建 frps」即可公网联机",
  openfrp: "通过 OpenFrp 实现公网联机",
  sakura: "通过樱花穿透实现公网联机",
};
function cards() {
  const v = net().via;
  return `<div class="vias">${VIAS.map((_, i) => `<i class="vslot" style="grid-column:${i + 1}"></i>`).join("")}<i class="vhl"></i>`
    + VIAS.map(([k, n], i) => `<div class="vcard${v === k ? " on" : ""}" data-via="${k}" style="grid-column:${i + 1}" title="${esc(VIA_TIP[k])}"><div class="n">${n}</div><div class="m">${viaMeta(k)}</div></div>`).join("")
    + `</div>`;
}
function paintMeta() { $$(".netv .vcard").forEach(c => { const m = $(".m", c); m.innerHTML = viaMeta(c.dataset.via); }); }

// ---- 局域网 / 自建 frps（同向导） ----
const vlanNets = () => LAN_NETS.slice(0, [0, 2, 5][net().vlan]);
const ib = (act, ic, tip, extra = "") => `<button class="nib" data-act="${act}" title="${esc(tip)}"${extra}><i data-ic="${ic}"></i></button>`;
const vlanRows = () => vlanNets().map(([n, ip]) => `<div class="netrow" title="已加入同一${/^[A-Za-z0-9]/.test(n || "") ? " " : ""}${esc(n)}${/[A-Za-z0-9]$/.test(n || "") ? " " : ""}网络的玩家可使用此地址连接"><span class="nn">${esc(n)}</span><span class="mono">${ip}:${PORT()}</span>`
  + ib("copy", "copy", "复制", ` data-copy="${ip}:${PORT()}"`) + `</div>`).join("");
const ntg = (key, t, on, tip = "", cls = "", dis = false) => `<button class="ntg${on ? " on" : ""}${cls ? " " + cls : ""}" data-ntg="${key}" role="switch" aria-checked="${on}"${dis ? " disabled" : ""}${tip ? ` title="${esc(tip)}"` : ""}><span class="sw"></span>${t}</button>`;
// 设置里存的自建 frps（11-29 作者定只在「设置 → 内网穿透」里填）：草图里 &frps=set 算存过，示例同设置页；没存过是 null
const FRPS = () => C.QS.get("frps") === "set" ? { host: "frp.example.com", port: "7000" } : null;
const FRPS_OFF = "适用于自有 frps 服务端，点击开启";
const FRPS_NONE = "需先在「设置 → 内网穿透」中填写自建 frps 的地址、端口与令牌";
const FRPS_BAD = "请输入 1～65535 之间的整数";
const remoteOk = s => { s = s.trim(); return !s || (/^\d+$/.test(s) && +s >= 1 && +s <= 65535); }; // 公网端口：空着算对（用本地端口），填了要是 1～65535 的整数
function frpsAddr() {
  const N = net(), cfg = FRPS(), h = cfg ? cfg.host : "", port = N.frps.remote.trim() || PORT();
  return (h ? `<span class="mono" title="${esc(h)}:${esc(port)}">${esc(h)}:${esc(port)}</span>` : `<span class="ph">frps 地址</span><span class="mono">:${esc(port)}</span>`)
    + ib("copy", "copy", "复制", ` data-copy="${esc(h)}:${esc(port)}"${h ? "" : " disabled"}`);
}
// 「保存并连接」/「保存」：第一次存之前开着就亮；存过以后公网端口改了才亮（同设置页的「保存」）
const frpsLit = () => { const N = net(); return !!FRPS() && N.frpsOn && (!N.frpsSaved || N.frps.remote.trim() !== N.frps.saved); };
const saveBtn = () => `<button class="nb pri" data-act="frpsave"${frpsLit() ? "" : " disabled"} title="保存配置，并跟随服务器启动">${net().frpsSaved ? "保存" : "保存并连接"}</button>`;
// 公网端口框下面一行淡字：存过时写用的是哪个服务端，后面一颗齿轮去设置；没存过时一句「尚未配置自建 frps」，后面一颗「前往设置」。开关关着时这一行不淡、照常能点
function frpsSrv() {
  const cfg = FRPS();
  if (!cfg) return `<div class="fsrv"><i data-ic="info"></i><span class="t">尚未配置自建 frps</span><button class="nb sm" data-act="tosettings"><i data-ic="gear"></i>前往设置</button></div>`;
  return `<div class="fsrv" title="在「设置 → 内网穿透」中修改"><span class="t">frps 服务端：<span class="mono">${esc(cfg.host)}:${esc(cfg.port)}</span></span>`
    + ib("tosettings", "gear", "前往「设置 → 内网穿透」") + `</div>`;
}
function lanPage() {
  const N = net(), cfg = FRPS(), on = N.frpsOn && !!cfg, fd = on ? "" : ` tabindex="-1"`;
  const lan = `<div class="lanp"><div class="lh"><i data-ic="wifi"></i><span class="t">局域网</span></div>`
    + `<div class="lbody"><span class="lb" title="同一局域网内的玩家在多人游戏中添加服务器时使用此地址">局域网联机地址</span>`
    + `<div class="bigaddr"><span class="mono">${LAN_IP}:${PORT()}</span>${ib("copy", "copy", "复制", ` data-copy="${LAN_IP}:${PORT()}"`)}</div>`
    + `<div class="vlan"><div class="vlh"><span class="lb" title="已启动并加入同一虚拟局域网（如 Radmin VPN、ZeroTier）时，使用对应地址联机；MineDeck 仅自动列出本机网卡地址">虚拟局域网</span>`
    + ib("vrefresh", "refresh", "重新检测") + `</div><div class="lwrap"><div class="list vrows" id="nvrows">${vlanRows()}</div><span class="thumb"></span></div></div>`
    + `<div class="vnone${vlanNets().length ? " gone" : ""}" id="nvnone"><i data-ic="info"></i><span>未检测到虚拟局域网，启动 Radmin、ZeroTier 等组网工具后将自动识别</span></div></div></div>`;
  const stt = N.frpsSaved && cfg ? (C.st === "running" ? `<span class="npill ok"><i></i>运行中</span>` : C.st === "starting" ? `<span class="npill warm"><i></i>连接中</span>` : `<span class="npill off"><i></i>已停止</span>`) : "";
  // 没配置时整块 .off：开关灰着、拨不动（disabled，点哪儿都不打开），悬停整块说去哪填；公网端口框下面那一行和「前往设置」不淡
  const tip = !cfg ? FRPS_NONE : on ? "" : FRPS_OFF;
  const frp = `<div class="lanp frpp${on ? " on" : ""}${cfg ? "" : " off"}"${tip ? ` title="${esc(tip)}"` : ""}>`
    + `<div class="lh">${ntg("frpsOn", "自建 frps", on, "由 MineDeck 启动 frpc 连接自有 frps，实现公网联机", "b", !cfg)}${stt}</div>`
    + `<div class="lbody"><span class="lb" title="公网玩家连接时使用此地址，由 frps 地址与公网端口组成（公网端口留空时默认与本地端口一致）">公网联机地址</span>`
    + `<div class="bigaddr" id="nfaddr">${frpsAddr()}</div>`
    + `<div class="frps"><input class="inp${N.frps.fbad ? " bad" : ""}" data-f="remote" placeholder="公网端口" value="${esc(N.frps.remote)}" inputmode="numeric" autocomplete="off" title="公网联机使用的端口，留空时默认与本地端口一致（${PORT()}）"${fd}>`
    + `<span class="fmsg">${N.frps.fbad ? FRPS_BAD : ""}</span></div>${frpsSrv()}</div>`
    + (cfg ? `<div class="lfoot">${saveBtn()}</div>` : "") + `</div>`;
  return lan + frp;
}

// ---- 隧道建好以后的那一页 ----
function nodeLine(t) {
  const x = nodeBy(t.plat, t.node);
  const tags = t.plat === "openfrp"
    ? (x.tier ? `<span class="tag ${x.tier}">${TIER_NAME[x.tier]}</span>` : "") + `<span class="nn"><span class="id">#${x.id}</span>${esc(x.n)}</span><span class="tag good">${x.bw}</span>` + (x.udp ? `<span class="tag plain">UDP</span>` : "")
    : `<span class="nn">${esc(x.n)}</span>` + (x.pub ? `<span class="tag pub">公开游戏</span>` : "");
  return `<div class="tnode" title="${esc(x.n + (x.d ? "\n" + x.d : ""))}"><span class="k">节点</span>${tags}<span class="nsp"></span>`
    + (t.kind === "voice" ? "" : `<button class="nb sm" data-act="swapnode" title="更换到其他节点：将在新节点创建隧道并连接，完成后删除平台上的原隧道">更换节点</button>`) + `</div>`;
}
function tunBlock(t) {
  const [c, s] = tunState(t), voice = t.kind === "voice";
  return `<div class="tunp${voice ? " voice" : ""}" data-tun="${voice ? "voice" : "game"}"><div class="lh"><i data-ic="${voice ? "voice" : "globe"}"></i><span class="t">${voice ? "语音隧道" : "游戏隧道"}</span>`
    + `<span class="npill ${c}" data-pill><i></i>${s}</span><span class="nsp"></span>${ib("tmenu", "more", "更多")}</div>`
    + `<div class="lbody">`
    + (voice ? `<span class="lb" title="MineDeck 已自动写入 Simple Voice Chat 的 voice_host，玩家无需手动填写">语音地址</span>`
      : `<span class="lb" title="公网玩家在多人游戏中添加服务器时使用此地址">公网联机地址</span>`)
    + `<div class="bigaddr"><span class="mono">${addrOf(t)}</span>${ib("copy", "copy", "复制", ` data-copy="${addrOf(t)}"`)}</div>${nodeLine(t)}`
    + `<div class="tkv" title="在${platIn(t.plat)}控制台中对应的隧道名称"><span class="k">隧道</span><span class="mono">${esc(t.name)}</span><span class="tag plain">${voice ? "UDP" : "TCP"}</span></div></div>`
    + `<div class="tfoot">${ntg("follow", "跟随服务器启动", t.follow, "服务器启动时自动启动隧道，停止时自动关闭")}<span class="nsp"></span>`
    + (t.on || t.wait ? `<button class="nb stop" data-act="tstop"><i data-ic="stop"></i>停止隧道</button>` : `<button class="nb go" data-act="tstart"><i data-ic="play"></i>启动隧道</button>`)
    + `</div></div>`;
}
function voiceBlock() {
  const N = net();
  if (N.voice) return tunBlock(N.voice);
  return `<div class="tunp empty voice"><div class="lh"><i data-ic="voice"></i><span class="t">语音隧道</span><span class="sub">Simple Voice Chat</span></div>`
    + `<div class="vadd"><button class="nb pri" data-act="vadd"${N.busy ? " disabled" : ""} title="${esc(`Simple Voice Chat 语音使用 UDP 协议，需单独创建隧道。MineDeck 将在游戏隧道所在节点创建，并自动写入 voice_host，玩家无需手动填写`)}">`
    + (N.busy ? `<i data-ic="spinner" class="spin"></i>创建中` : `<i data-ic="plus"></i>新建语音隧道`) + `</button></div></div>`;
}
const tunPage = () => tunBlock(net().game) + (net().svc ? voiceBlock() : "");

// ---- 节点面板（同向导第四步；右上角多一颗主键） ----
function nodeWhy(k, x) {
  if (x.off) return ["off", "离线"];
  if (x.full) return ["full", "节点已满载，暂无法创建隧道"];
  if (k === "openfrp") {
    if (TIERS.indexOf(x.tier) > TIERS.indexOf(OF_USER.tier)) return ["perm", `仅限 ${TIER_NAME[x.tier]} 用户使用`];
    if (x.real && !OF_USER.realname) return ["perm", "需先完成 OpenFrp 实名认证"];
  } else if (x.vip > SK_USER.level) return ["perm", "VIP 等级不足"];
  return null;
}
function nodeShown(k, x) {
  const N = net();
  if (N.filter === "udp" && !(k === "openfrp" ? x.udp : !x.noudp)) return false;
  if (N.filter === "pub" && !x.pub) return false;
  const why = nodeWhy(k, x);
  if (why && !N.show[why[0]]) return false;
  const q = N.q.trim().toLowerCase();
  return !q || `${x.key} ${x.n} ${x.d}`.toLowerCase().includes(q);
}
function nodeCard(k, x) {
  const N = net(), why = nodeWhy(k, x), cur = N.swap && N.game?.plat === k && N.game.node === x.key, on = !cur && N.pick[k] === x.key;
  const st = x.off ? `<span class="tag bad">离线</span>` : x.full ? `<span class="tag plain">满载</span>` : cur ? `<span class="tag plain">当前</span>` : "";
  const cls = `nd${on ? " on" : ""}${why ? " off" : ""}${cur ? " cur" : ""}`, tip = why ? why[1] : cur ? "当前正在使用此节点" : `${k === "openfrp" ? x.key + " " : ""}${x.n}${x.d ? "\n" + x.d : ""}`;
  if (k === "openfrp") {
    const tags = `<span class="tag good">${x.bw}</span>` + (x.udp ? `<span class="tag plain">UDP</span>` : "") + (x.https ? `<span class="tag plain">HTTPS</span>` : "")
      + (x.real && x.sec !== 0 ? `<span class="tag warn">实名</span>` : "") + (x.ext ? `<span class="tag warn" title="主地址受限时，需使用扩展地址连接">扩展地址</span>` : "");
    return `<div class="${cls}" data-node="${esc(x.key)}" title="${esc(tip)}"><div class="r1">${x.tier ? `<span class="tag ${x.tier}">${TIER_NAME[x.tier]}</span>` : ""}`
      + `<span class="n"><span class="id">#${x.id}</span>${esc(x.n)}</span>${st}</div><div class="d one">${esc(x.d) || "&nbsp;"}</div><div class="r3">${tags}</div></div>`;
  }
  const tags = (x.pub ? `<span class="tag pub">公开游戏</span>` : "") + (x.noudp ? `<span class="tag bad">无 UDP</span>` : "") + (x.beta ? `<span class="tag warn">BETA</span>` : "") + st;
  return `<div class="${cls}" data-node="${esc(x.key)}" title="${esc(tip)}"><div class="r1"><span class="n">${esc(x.n)}</span>${tags}</div><div class="d">${esc(x.d)}</div></div>`;
}
function nodeCards(k) {
  const secs = k === "openfrp" ? OF_SECS.map(([t, s], i) => [t, s, OF_NODES.filter(x => x.sec === i)]) : SK_SECS.map(([t, inland]) => [t, "", SK_NODES.filter(x => x.inland === inland)]);
  const out = secs.map(([t, sub, all]) => {
    const list = all.filter(x => nodeShown(k, x)); if (!list.length) return "";
    return `<div class="ngrp"><div class="ngh"><h4>${t}</h4>${sub ? `<span class="s">${sub}</span>` : ""}<span class="c">${all.filter(x => !nodeWhy(k, x)).length} 个可用</span></div>`
      + `<div class="ncards">${list.map(x => nodeCard(k, x)).join("")}</div></div>`;
  }).join("");
  return out || `<div class="nempty">没有匹配的节点</div>`;
}
function tunRows(k) {
  const N = net(), q = N.q.trim().toLowerCase(), list = PLAT_TUNS[k].filter(t => !q || (t.n + " " + t.node).toLowerCase().includes(q));
  if (!list.length) return `<div class="nempty">平台上没有匹配的隧道</div>`;
  return list.map(t => {
    const bad = t.t !== "TCP" ? "此为 UDP 隧道（通常用于语音），游戏隧道需使用 TCP 协议" : t.lp !== PORT() ? `本地端口为 ${t.lp}，与当前服务器端口 ${PORT()} 不一致；请先在平台将本地端口改为 ${PORT()}` : "";
    const x = nodeBy(k, t.node);
    return `<div class="trow${N.use[k] === t.n ? " on" : ""}${bad ? " off" : ""}" data-use="${esc(t.n)}" title="${esc(bad || "选择此隧道")}">`
      + `<span class="n">${esc(t.n)}</span><span class="tag plain">${t.t}</span><span class="k">${esc(k === "openfrp" ? `${x.key} ${x.n}` : x.n)} · 本地端口 ${t.lp}</span></div>`;
  }).join("");
}
const ckBox = (key, t) => `<span class="ck${net().show[key] ? " on" : ""}" data-ck="${key}" title="显示${t}节点（不可选）"><span class="bx"><i data-ic="check"></i></span>${t}</span>`;
function nodeBody(k) {
  const N = net(), fil = [["all", "全部"], ["udp", "支持 UDP"], ...(k === "sakura" ? [["pub", "公开游戏"]] : [])];
  const top = N.swap || N.tab[k] === "new"
    ? `<div class="nfil"><div class="nfils"><i class="nfhl"></i>${fil.map(([v, t]) => `<button class="nf${N.filter === v ? " on" : ""}" data-nfil="${v}">${t}</button>`).join("")}</div><span class="nsp"></span>`
      + ckBox("full", "满载") + ckBox("off", "离线") + ckBox("perm", "无权限") + `</div>` : "";
  return top + `<div class="lwrap nwrap"><div class="list nlist" id="nnlist">${N.swap || N.tab[k] === "new" ? nodeCards(k) : tunRows(k)}</div><span class="thumb"></span></div>`;
}
// 右上角的主键：选好节点或隧道才亮
function mainBtn(k) {
  const N = net(), newTab = N.swap || N.tab[k] === "new", ready = newTab ? !!N.pick[k] : !!N.use[k];
  const other = !N.swap && N.game && N.game.plat !== k;
  const label = N.busy ? `<i data-ic="spinner" class="spin"></i>${N.swap ? "更换中" : "创建中"}` : N.swap ? "确认更换" : other ? `切换至${platIn(k, " ", "")}` : newTab ? "新建隧道" : "使用此隧道";
  const tip = !ready ? (newTab ? "请先选择节点" : "请先选择隧道")
    : N.swap ? "将在新节点创建隧道并连接，完成后删除平台上的原隧道；公网联机地址将会变化"
    : other ? `${newTab ? "创建" : "绑定"}后将停止并解绑当前${platIn(N.game.plat)}隧道，但保留平台上的原隧道` : newTab ? `本地端口 ${PORT()}，公网端口${k === "openfrp" ? "在节点允许范围内自动分配" : "由樱花穿透自动分配"}，其余采用平台默认配置` : "";
  return `<button class="nb pri" data-act="nmain"${ready && !N.busy ? "" : " disabled"}${tip ? ` title="${esc(tip)}"` : ""}>${label}</button>`;
}
function nodePanel(k) {
  const N = net(), on = v => N.tab[k] === v ? ` class="on"` : "", useTab = !N.swap && N.tab[k] === "use";
  const head = N.swap ? ib("swapback", "arrowLeft", "返回隧道") + `<span class="nt">更换节点</span>`
    : `<div class="seg" data-seg="tab"><i class="sgl"></i><span data-v="new"${on("new")} title="由 MineDeck 在${platIn(k)}上创建新隧道">新建隧道</span><span data-v="use"${on("use")} title="从平台已创建的隧道中选择一条绑定到当前服务器">已有隧道</span></div>`;
  return `<div class="nbar">${head}<label class="nsearch"><i data-ic="search"></i><input id="nnq" placeholder="${useTab ? "搜索隧道" : "搜索节点"}" value="${esc(N.q)}" autocomplete="off" spellcheck="false"></label>`
    + ib("nrefresh", "refresh", useTab ? "刷新隧道列表" : "刷新节点列表") + `<span id="nmainslot" style="display:contents">${mainBtn(k)}</span></div><div class="nbody">${nodeBody(k)}</div>`;
}
function sakuraKey() {
  const N = net();
  return `<div class="keyb"><div class="lrow"><span class="lb">樱花访问密钥</span><a class="tlink" title="在浏览器中打开樱花穿透官网的用户信息页，复制「访问密钥」">去官网复制</a></div>`
    + `<div class="keyrow"><input class="inp" id="nskey" type="password" placeholder="粘贴访问密钥" value="${esc(N.key)}" autocomplete="off" spellcheck="false">`
    + `<button class="kbtn" data-act="connect"${N.connecting ? " disabled" : ""}>${N.connecting ? `<i data-ic="spinner" class="spin"></i>连接中` : "连接"}</button></div>`
    + ntg("saveKey", "记住密钥", N.saveKey, "使用 Windows 加密保存在本机，后续配置其他服务器时无需重复粘贴；可在「设置 → 内网穿透」中更换或删除") + `</div>`
    + `<div class="nph">请先连接账号，以加载并选择可用节点</div>`;
}
function detail() {
  const N = net(), v = N.via;
  if (v === "lan") return lanPage();
  if (v === "sakura" && !N.sakura) return sakuraKey();
  if (N.game && N.game.plat === v && !N.swap) return tunPage();
  return nodePanel(v);
}

// ---- 换东西的动效（同向导：旧的往反方向挪 5 像素淡掉 0.09 秒，新的一项一项浮进来 0.22 秒、错开 22 毫秒） ----
function swapItems(box, html, dir, axis = "X") {
  if (!box) return;
  const easeIn = "cubic-bezier(.4, 0, 1, 1)", easeOut = "cubic-bezier(.2, .7, .2, 1)", d = 5 * dir, tf = v => axis === "X" ? `translateX(${v}px)` : `translateY(${v}px)`;
  const tok = box._tok = (box._tok || 0) + 1, olds = [...box.children];
  olds.forEach((x, i) => x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-d) }], { duration: 90, delay: i * 22, easing: easeIn, fill: "forwards" }));
  setTimeout(() => {
    if (box._tok !== tok) return;
    box.innerHTML = html; C.KIT.hydrate(box); after();
    [...box.children].forEach((x, i) => x.animate([{ opacity: 0, transform: tf(d) }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: easeOut, fill: "backwards" }));
  }, 90 + Math.max(0, olds.length - 1) * 22);
}
const swapDetail = (dir, axis) => swapItems($(".netv .vdet"), detail(), dir, axis);
function regridNodes(dir = 0) {
  const l = $("#nnlist"); if (!l) return;
  const N = net(), k = N.via;
  l.getAnimations({ subtree: true }).forEach(a => a.cancel());
  const tok = l._tok = (l._tok || 0) + 1;
  const a = l.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: dir ? `translateX(${-5 * dir}px)` : "none" }], { duration: 90, easing: "cubic-bezier(.4, 0, 1, 1)", fill: "forwards" });
  a.onfinish = () => {
    if (l._tok !== tok) return;
    l.scrollTop = 0; l.innerHTML = N.swap || N.tab[k] === "new" ? nodeCards(k) : tunRows(k); C.KIT.hydrate(l); a.cancel();
    const lb = l.getBoundingClientRect(), from = dir ? `translateX(${5 * dir}px)` : "translateY(5px)";
    $$(".ngh, .nd, .trow, .nempty", l).filter(c => c.getBoundingClientRect().top < lb.bottom).forEach((c, i) =>
      c.animate([{ opacity: 0, transform: from }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: "cubic-bezier(.2, .7, .2, 1)", fill: "backwards" }));
    l.closest(".lwrap")?._paint?.();
  };
}
// 选中框滑到选中的那一项。三张卡是三等分、宽带小数，用 getBoundingClientRect（同向导）；分段和筛选用 offsetLeft（从框的内边距那里量，同向导）
function slide(box, hlSel, onSel, animate, rect) {
  if (!box) return;
  const hl = $(hlSel, box), on = $(onSel, box); if (!hl || !on) return;
  if (!animate) hl.style.transition = "none";
  const x = rect ? on.getBoundingClientRect().left - box.getBoundingClientRect().left : on.offsetLeft, w = rect ? on.getBoundingClientRect().width : on.offsetWidth;
  hl.style.transform = `translateX(${x}px)`; hl.style.width = w + "px";
  if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
}
const placeVias = a => slide($(".netv .vias"), ".vhl", ".vcard.on", a, true);
const placeSeg = a => slide($(".netv .seg"), ".sgl", "span.on", a);
const placeNfil = a => slide($(".netv .nfils"), ".nfhl", ".nf.on", a);
function after() { placeSeg(false); placeNfil(false); $$(".netv .lwrap").forEach(w => C.thinBar(w)); }
const paintMain = () => { const s = $("#nmainslot"); if (s) { s.innerHTML = mainBtn(net().via); C.KIT.hydrate(s); } };

// ---- 事件 ----
function copyBtn(b) {
  b.classList.add("done"); b.innerHTML = C.KIT.icon("check");
  setTimeout(() => { b.classList.remove("done"); b.innerHTML = C.KIT.icon("copy"); }, 1200);
}
function pickVia(k) {
  const N = net(); if (N.via === k) return;
  const order = VIAS.map(x => x[0]), dir = order.indexOf(k) > order.indexOf(N.via) ? 1 : -1;
  N.via = k; N.swap = false; N.q = "";
  $$(".netv .vcard").forEach(c => c.classList.toggle("on", c.dataset.via === k));
  placeVias(true); swapDetail(dir);
}
// 隧道的状态变了：只换那一块的状态胶囊、开停按钮和卡片第二行
function repaintTun(which) {
  const N = net(), t = which === "voice" ? N.voice : N.game, el = $(`.netv .tunp[data-tun="${which}"]`); if (!el || !t) return;
  const [c, s] = tunState(t), p = $("[data-pill]", el); p.className = `npill ${c}`; p.innerHTML = `<i></i>${s}`;
  const f = $(".tfoot > .nb", el);
  f.outerHTML = t.on || t.wait ? `<button class="nb stop" data-act="tstop"><i data-ic="stop"></i>停止隧道</button>` : `<button class="nb go" data-act="tstart"><i data-ic="play"></i>启动隧道</button>`;
  C.KIT.hydrate(el); paintMeta();
}
function connectTun(t, which) { t.on = false; t.wait = true; repaintTun(which); setTimeout(() => { t.wait = false; t.on = true; repaintTun(which); }, 1500); }
function nmain() {
  const N = net(), k = N.via; if (N.busy) return;
  N.busy = true; paintMain();
  setTimeout(() => {
    N.busy = false;
    const live = C.st !== "stopped";
    if (N.swap) { N.game.node = N.pick[k]; N.game.port = 30000 + (nodeBy(k, N.pick[k]).id || 7) * 131 % 9000; N.swap = false; }
    else {
      const use = N.tab[k] === "use" ? PLAT_TUNS[k].find(t => t.n === N.use[k]) : null;
      // 新建的隧道「跟随服务器启动」默认关（作者 11-28 定，同程序 A62：开着的话每次开服都自动映射到公网，要人自己打开）；语音隧道同
      N.game = { plat: k, name: use ? use.n : "survival_" + PORT(), node: use ? use.node : N.pick[k], port: use ? 31502 : 32000 + PORT() % 1000, follow: false, on: false, wait: false };
      N.voice = null;
    }
    N.pick[k] = ""; N.use[k] = "";
    paintMeta(); swapDetail(1);
    if (live) setTimeout(() => connectTun(N.game, "game"), 400);
  }, 1200);
}
// 自建 frps 的「保存并连接」/「保存」：公网端口填错就地拦下（框标红、晃一下、光标落过去、右边一句红字），存好了标题那一行多一颗状态（只加这一颗，别的不动），
// 按钮的字换成「保存」、灰到下次改了公网端口
const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }],
  { duration: 360, easing: "cubic-bezier(.3, .7, .2, 1)" });
function markRemote(bad) {
  net().frps.fbad = bad;
  const i = $(".netv [data-f=remote]"), m = $(".netv .frps .fmsg"); if (!i) return;
  i.classList.toggle("bad", bad); m.textContent = bad ? FRPS_BAD : "";
}
function paintSave() {
  const b = $(".netv [data-act=frpsave]"); if (!b) return;
  b.disabled = !frpsLit(); b.textContent = net().frpsSaved ? "保存" : "保存并连接";
}
function saveFrps() {
  const N = net(); if (!frpsLit()) return;
  if (!remoteOk(N.frps.remote)) { markRemote(true); const i = $(".netv [data-f=remote]"); shake(i); i?.focus(); return; }
  const first = !N.frpsSaved; N.frpsSaved = true; N.frps.saved = N.frps.remote.trim(); paintSave();
  if (first) {
    $(".netv .frpp .lh").insertAdjacentHTML("beforeend", C.st === "running" ? `<span class="npill ok"><i></i>运行中</span>` : C.st === "starting" ? `<span class="npill warm"><i></i>连接中</span>` : `<span class="npill off"><i></i>已停止</span>`);
    $(".netv .frpp .lh .npill").animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: "cubic-bezier(.2, .7, .2, 1)" });
    paintMeta();
  }
}
// 去「设置 → 内网穿透」：走外壳的 ctx.go（有没保存的先问）；带上配色参数和 frps，设置页看到的是同一个状态
function toSettings() {
  const q = new URLSearchParams({ sec: "tunnels" });
  ["theme", "scheme", "accent", "frps"].forEach(k => C.QS.get(k) && q.set(k, C.QS.get(k)));
  C.go("settings.html?" + q);
}
// 删除隧道：确认框（同共用件页的危险确认框），默认连平台上的一起删
let mask = null;
function confirmDelete(which) {
  const N = net(), t = which === "voice" ? N.voice : N.game;
  const st = { both: true };
  mask = Object.assign(document.createElement("div"), { className: "nmask enter" });
  mask.innerHTML = `<div class="nmd" role="dialog" aria-modal="true"><div class="mc"><div class="mh"><span class="mi"><i data-ic="trash"></i></span><h3>删除${which === "voice" ? "语音" : "游戏"}隧道「${esc(t.name)}」？</h3></div>`
    + (which === "game" && N.voice ? `<div class="mb">关联的语音隧道「<b>${esc(N.voice.name)}</b>」也将一并删除。</div>` : "")
    + `<div>${ntg("both", `同时删除${platIn(t.plat)}上的${which === "game" && N.voice ? "这两条" : "该"}隧道`, true, `关闭时仅移除 MineDeck 中的记录，保留${platIn(t.plat)}上的隧道，后续可在「已有隧道」中重新绑定`)}</div></div>`
    + `<div class="ma"><button class="nb" data-x="close">取消</button><button class="nb dgr" data-x="del">删除隧道</button></div></div>`;
  document.body.appendChild(mask); C.KIT.hydrate(mask);
  $("[data-x=close]", mask).focus({ focusVisible: false });
  mask.addEventListener("click", e => {
    const tg = e.target.closest(".ntg"); if (tg) { st.both = !st.both; tg.classList.toggle("on", st.both); tg.setAttribute("aria-checked", st.both); return; }
    if (e.target.closest("[data-x=del]")) { closeMask(); if (which === "voice") N.voice = null; else { N.game = null; N.voice = null; N.via = N.via; } paintMeta(); swapDetail(-1, "Y"); return; }
    if (e.target.closest("[data-x=close]") || e.target === mask) closeMask();
  });
}
function closeMask() { if (!mask) return; const m = mask; mask = null; m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 140); }
// 「···」：在平台上打开、删除（同外壳的菜单样子）
let menu = null;
function toggleMenu(btn) {
  if (menu) return closeMenu();
  const which = btn.closest(".tunp").dataset.tun, t = which === "voice" ? net().voice : net().game;
  menu = Object.assign(document.createElement("div"), { className: "dm enter" }); menu.setAttribute("role", "menu");
  menu.innerHTML = `<div class="it" role="menuitem"><i data-ic="share"></i>在${platIn(t.plat)}控制台打开</div><div class="sep"></div>`
    + `<div class="it dgr" role="menuitem" data-del="${which}"><i data-ic="trash"></i>删除隧道…</div>`;
  document.body.appendChild(menu); C.KIT.hydrate(menu);
  const r = btn.getBoundingClientRect();
  menu.style.left = Math.round(r.right - menu.offsetWidth) + "px"; menu.style.top = Math.round(r.bottom + 4) + "px";
  btn.classList.add("open"); menu._a = btn;
}
function closeMenu() { if (!menu) return; const m = menu; menu = null; m._a.classList.remove("open"); m.classList.remove("enter"); m.classList.add("leave"); setTimeout(() => m.remove(), 90); }

document.addEventListener("click", e => {
  if (menu && !e.target.closest(".dm")) { if (!e.target.closest("[data-act=tmenu]")) closeMenu(); }
  const del = e.target.closest(".dm [data-del]"); if (del && menu) { const w = del.dataset.del; closeMenu(); confirmDelete(w); return; }
  if (menu && e.target.closest(".dm .it")) { closeMenu(); return; } // 「在平台上打开」：草图里只收起菜单
  const root = e.target.closest(".netv"); if (!root || !C || C.tab !== "net") return;
  const N = net(), k = N.via;
  const via = e.target.closest("[data-via]"); if (via) { pickVia(via.dataset.via); return; }
  const a = e.target.closest("[data-act]")?.dataset.act;
  if (a === "copy") { const b = e.target.closest("[data-act]"); if (!b.disabled) copyBtn(b); return; }
  if (a === "vrefresh") {
    const b = e.target.closest(".nib"); if (b.classList.contains("spinning")) return;
    b.classList.add("spinning");
    setTimeout(() => {
      b.classList.remove("spinning"); N.vlan = Math.min(2, N.vlan + 1);
      const box = $("#nvrows"); if (!box) return;
      box.innerHTML = vlanRows(); C.KIT.hydrate(box);
      [...box.children].forEach((c, i) => c.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: "cubic-bezier(.2, .7, .2, 1)", fill: "backwards" }));
      $("#nvnone")?.classList.toggle("gone", vlanNets().length > 0); box.closest(".lwrap")._paint?.();
    }, 600);
    return;
  }
  if (a === "frpsave") { saveFrps(); return; }
  if (a === "tosettings") { toSettings(); return; }
  const fr = e.target.closest(".frpp:not(.on):not(.off)"); const tgl = e.target.closest("[data-ntg]");
  if (tgl || fr) {
    const key = tgl ? tgl.dataset.ntg : "frpsOn";
    if (key === "frpsOn") {
      if (!FRPS()) return; // 设置里没配置：拨不动（键盘、脚本点到的也不算）
      N.frpsOn = !N.frpsOn; const p = $(".netv .frpp"); p.classList.toggle("on", N.frpsOn); p.title = N.frpsOn ? "" : FRPS_OFF;
      const t = $("[data-ntg=frpsOn]", p); t.classList.toggle("on", N.frpsOn); t.setAttribute("aria-checked", N.frpsOn);
      const i = $("[data-f=remote]", p); if (N.frpsOn) i.removeAttribute("tabindex"); else { i.tabIndex = -1; markRemote(false); }
      paintSave(); // 公网端口是选填的，打开时不把光标塞进去
      return;
    }
    if (key === "follow") { const w = tgl.closest(".tunp").dataset.tun, t = w === "voice" ? N.voice : N.game; t.follow = !t.follow; tgl.classList.toggle("on", t.follow); tgl.setAttribute("aria-checked", t.follow); return; }
    if (key === "saveKey") { N.saveKey = !N.saveKey; tgl.classList.toggle("on", N.saveKey); tgl.setAttribute("aria-checked", N.saveKey); return; }
    return;
  }
  if (a === "tstop" || a === "tstart") {
    const w = e.target.closest(".tunp").dataset.tun, t = w === "voice" ? N.voice : N.game;
    if (a === "tstop") { t.on = false; t.wait = false; repaintTun(w); } else connectTun(t, w);
    return;
  }
  if (a === "tmenu") { toggleMenu(e.target.closest(".nib")); return; }
  if (a === "vadd") {
    N.busy = true; swapItems($(".netv .tunp.empty .vadd"), `<button class="nb pri" disabled><i data-ic="spinner" class="spin"></i>创建中</button>`, 0, "Y");
    setTimeout(() => {
      N.busy = false; N.voice = { kind: "voice", plat: N.game.plat, name: N.game.name + "_voice", node: N.game.node, port: N.game.port + 7, follow: false, on: false, wait: false };
      const old = $(".netv .tunp.empty"); if (!old) return;
      old.animate([{ opacity: 1 }, { opacity: 0, transform: "translateY(-5px)" }], { duration: 90, easing: "cubic-bezier(.4, 0, 1, 1)", fill: "forwards" }).onfinish = () => {
        old.outerHTML = tunBlock(N.voice); const nw = $(`.netv .tunp[data-tun="voice"]`); C.KIT.hydrate(nw);
        nw.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: "cubic-bezier(.2, .7, .2, 1)", fill: "backwards" });
        if (N.game.on || N.game.wait) connectTun(N.voice, "voice");
      };
    }, 1200);
    return;
  }
  if (a === "swapnode") { N.swap = true; N.q = ""; N.pick[k] = ""; swapDetail(1); return; }
  if (a === "swapback") { N.swap = false; N.pick[k] = ""; swapDetail(-1); return; }
  if (a === "nmain") { nmain(); return; }
  if (a === "nrefresh") {
    const b = e.target.closest(".nib"); if (b.classList.contains("spinning")) return;
    b.classList.add("spinning"); setTimeout(() => { b.classList.remove("spinning"); regridNodes(0); }, 600); return;
  }
  if (a === "connect") {
    if (!N.key.trim()) { $("#nskey")?.focus(); return; }
    N.connecting = true; const b = e.target.closest(".kbtn"); b.disabled = true; b.innerHTML = `${C.KIT.icon("spinner", "spin")}连接中`;
    setTimeout(() => { N.connecting = false; N.sakura = true; paintMeta(); swapDetail(0, "Y"); }, 1200);
    return;
  }
  const sp = e.target.closest(".seg span"); if (sp) {
    if (sp.classList.contains("on")) return;
    N.tab[k] = sp.dataset.v; $$(".netv .seg span").forEach(x => x.classList.toggle("on", x === sp)); placeSeg(true);
    const useTab = N.tab[k] === "use", nq = $("#nnq"), rb = $(".netv [data-act=nrefresh]");
    if (nq) nq.placeholder = useTab ? "搜索隧道" : "搜索节点";
    if (rb) { const rt = useTab ? "刷新隧道列表" : "刷新节点列表"; rb.title = rt; delete rb.dataset.tip; if (rb.hasAttribute("aria-label")) rb.setAttribute("aria-label", rt); }
    paintMain(); swapItems($(".netv .nbody"), nodeBody(k), useTab ? 1 : -1); return;
  }
  const nf = e.target.closest("[data-nfil]"); if (nf) {
    const v = nf.dataset.nfil; if (N.filter === v) return;
    const btns = $$("[data-nfil]"), at = x => btns.findIndex(b => b.dataset.nfil === x), dir = at(v) > at(N.filter) ? 1 : -1;
    N.filter = v; btns.forEach(b => b.classList.toggle("on", b.dataset.nfil === v)); placeNfil(true); regridNodes(dir); return;
  }
  const ck = e.target.closest("[data-ck]"); if (ck) { N.show[ck.dataset.ck] = !N.show[ck.dataset.ck]; ck.classList.toggle("on"); regridNodes(0); return; }
  const nd = e.target.closest(".nd"); if (nd) {
    if (nd.classList.contains("off") || nd.classList.contains("cur")) return;
    N.pick[k] = N.pick[k] === nd.dataset.node ? "" : nd.dataset.node;
    $$(".netv .nd").forEach(x => x.classList.toggle("on", x.dataset.node === N.pick[k])); paintMain(); return;
  }
  const tr = e.target.closest(".trow[data-use]"); if (tr) {
    if (tr.classList.contains("off")) return;
    N.use[k] = N.use[k] === tr.dataset.use ? "" : tr.dataset.use;
    $$(".netv .trow").forEach(x => x.classList.toggle("on", x.dataset.use === N.use[k])); paintMain(); return;
  }
});
let qT = 0;
document.addEventListener("input", e => {
  if (!e.target.closest?.(".netv") || !C) return;
  const N = net();
  if (e.target.id === "nnq") { N.q = e.target.value; clearTimeout(qT); qT = setTimeout(() => regridNodes(0), 180); return; }
  if (e.target.id === "nskey") { N.key = e.target.value; return; }
  if (e.target.dataset.f === "remote") { // 公网端口：边填边拼大字，红框和那句红字一改就收
    N.frps.remote = e.target.value; if (N.frps.fbad) markRemote(false);
    const el = $("#nfaddr"); if (el) { el.innerHTML = frpsAddr(); C.KIT.hydrate(el); }
    paintSave();
  }
});
document.addEventListener("keydown", e => {
  if (e.key === "Escape") { closeMenu(); closeMask(); }
  if (e.key === "Enter" && !e.isComposing && e.target.dataset?.f === "remote" && e.target.closest(".netv")) { e.preventDefault(); saveFrps(); } // 公网端口框里回车同点「保存」
});
addEventListener("resize", () => { if (C?.tab === "net") { placeVias(false); placeSeg(false); placeNfil(false); } });

window.DETAIL_TABS.net = {
  // 草图切换条（外壳左下角第二条）：设置里有没有存过自建 frps
  sk: [{ t: "自建 frps", k: "frps", opts: [[null, "未配置", "设置里没有存过自建 frps"], ["set", "已配置", "设置里存过 frp.example.com:7000（同设置页 &frps=set）"]] }],
  html(ctx) { C = ctx; return `<div class="netv">${cards()}<div class="vdet">${detail()}</div></div>`; },
  mount(ctx) { C = ctx; placeVias(false); after(); },
};
})();
