// 界面原型的场景清单。一个场景 = 仿真后端摆成什么样（setup）+ 界面停在哪一页（hash）+ 进页面后自动点几下（steps）。
// 目录页（index.html）按 group 分组列出来，点一个就切过去；切之前仿真后端先回到 base() 的样子，场景之间互不影响。
//
// steps 是一串动作，在嵌着的界面里依次执行（每步都会先等元素出现）：
//   { click: 选择器 }  { dblclick: 选择器 }  { fill: 选择器, value: 文字 }  { select: 选择器, value: 值 }
//   { wait: 选择器 }   { sleep: 毫秒 }       { key: "Escape" }            { scroll: 选择器, to: "bottom" }
//   { upload: 文件选择框的选择器, files: [{ name, kb?, text? }] }  往选择框里放几个假文件（text 不写就是一个假的 jar）
// setup(state, proto)：state 是仿真后端的内存数据（见 ../server.js 的 newState），直接改；
//   proto.api("POST /api/pickdir", { code: 500, error: "…" }) 让某个接口在这个场景里固定这样回。
// view：这个场景默认用哪种窗口大小（目录页右上角能改）："min" 最小窗口、"phone" 手机；不写就沿用当前选的。
// tag："new" 的场景会标一个「本次改动」的记号。
"use strict";
const { INSTANCES } = require("./fiction");

const enc = encodeURIComponent;
const clone = x => JSON.parse(JSON.stringify(x));
const four = () => clone(INSTANCES.slice(0, 4));
const all = () => clone(INSTANCES);

// 每个场景开始前都摆成这样：12 台服务器、两条隧道（同仿真后端默认的那两条）
function base(state) {
  state.instances = all();
  state.empty = false;
}

// 「从电脑添加」的几个假文件。仿真后端没有真的 jar 可读，是按文件名里的字样装样子的（见 ../server.js 里 modcart 一节的说明）：
// 带 sodium 的算在 Modrinth 上认出来了，带 needs 的认不出来、还缺一个要自己找的前置，带 fabric / forge 的是那种加载器的文件
const LOCAL_FILES = [
  { name: "sodium-extra-neoforge-0.6.0.jar", kb: 412 },
  { name: "needs-geckolib-cataclysm-2.43.jar", kb: 2380 },
  { name: "lithium-fabric-0.13.1.jar", kb: 690 },
  { name: "安装说明.txt", text: "把 jar 放进 mods 文件夹" },
];

const GROUPS = [
  { id: "changed", title: "这次改了的地方", note: "对着 PR 说明里的六条看" },
  { id: "list", title: "服务器列表" },
  { id: "create", title: "新建服务器" },
  { id: "task", title: "部署与后台任务" },
  { id: "detail", title: "服务器详情（12 种核心）", note: "进去以后标签可以随便点" },
  { id: "tunnel", title: "内网穿透" },
  { id: "settings", title: "设置" },
  { id: "overlay", title: "弹窗、提示和助手" },
];

const S = [];
const add = (group, id, title, o = {}) => S.push({ group, id, title, hash: "#/instances", ...o });

// 向导走到第几步：核心 → 版本 → 构建 →（选模组 / 选插件）→ 基本设置
const wizTo = (core, upto) => {
  const st = [{ click: `.core-card[data-id="${core}"]` }];
  if (upto === "core") return st;
  st.push({ click: "#wnext" }, { wait: ".ver-item" });
  if (upto === "version") return st;
  st.push({ click: "#wnext" });
  if (core === "vanilla") return [...st, { wait: "#f-name" }];
  st.push({ wait: "#build-list .ver-item" });
  if (upto === "build") return st;
  st.push({ click: "#wnext" }, { wait: "#wiz-mod-body" });
  if (upto === "mods") return st;
  return [...st, { click: "#wnext" }, { wait: "#f-name" }];
};

// ==================== 这次改了的地方 ====================
add("changed", "c-home-empty", "④ 首页：还没有服务器", {
  tag: "new", note: "主按钮不再是「一键开服（Paper）」，现在叫「从零创建」，点了直接进向导；下面那行小字去新建页挑别的方式。",
  setup: s => { s.empty = true; },
});
add("changed", "c-create", "③ 新建页：四张卡片分两组", {
  tag: "new", hash: "#/create", note: "四张卡片一样大，分「新建」和「导入与接管」两组。每种方式有自己的地址，后退键能回到这里。",
});
add("changed", "c-tunnel-empty", "⑥ 内网穿透：还没有隧道", {
  tag: "new", hash: "#/tunnels", note: "表单在上面、铺满到右边，插画在下面。说明只写当前选中的那家服务商，换一家试试。",
  setup: s => { s.tunnels = []; },
});
add("changed", "c-tunnel-form", "⑥ 内网穿透：有隧道时展开表单", {
  tag: "new", hash: "#/tunnels", note: "表单出现在列表上方，和列表一样宽；页头的按钮变成「收起」。",
  steps: [{ click: "#tn-toggle" }],
});
add("changed", "c-wiz-mods", "⑤ 选模组：一进来就是热门清单", {
  tag: "new", hash: "#/create/new", note: "NeoForge 26.1 为例。左边按下载量列热门，可以按类别筛、往下翻页；右边是待装清单，可以「从电脑添加」自己下载的 jar。",
  steps: wizTo("neoforge", "mods"),
});
add("changed", "c-wiz-local", "⑤ 选模组：自己下载的 jar 也能放进清单", {
  tag: "new", hash: "#/create/new",
  note: "右边点「从电脑添加…」选了四个文件：一个加载器不对（行上有提醒），一个不是 jar（下面说明原因）。加完自动检查前置：认不出来的文件缺的前置会单独列出，要自己去找。",
  steps: [...wizTo("neoforge", "mods"), { click: '.mc-add[data-pid="create"]' }, { upload: "#mc-file", files: LOCAL_FILES }, { sleep: 1200 }],
});
add("changed", "c-wiz-cats", "⑤ 选模组：按类别筛、往下翻页", {
  tag: "new", hash: "#/create/new", note: "点了「优化」。列表底下有「加载更多」，每次 20 个。",
  steps: [...wizTo("fabric", "mods"), { click: '.mc-chips .cp-chip[data-cat="optimization"]' }],
});
add("changed", "c-wiz-offline", "⑤ 选模组：热门清单没加载出来", {
  tag: "new", hash: "#/create/new", note: "连不上 Modrinth 时只在列表区说明并给「重试」，不弹提示，也不挡「下一步」；从电脑添加照样能用。",
  setup: s => { s.modcart.searchFail = "连不上 Modrinth（api.modrinth.com），检查一下网络或代理"; },
  steps: wizTo("neoforge", "mods"),
});
add("changed", "c-wiz-plugins", "⑤ 选插件：Paper 这一步叫「选插件」", {
  tag: "new", hash: "#/create/new", note: "插件端（Paper 一系和代理端）这一步的名字和文案都写「插件」。",
  steps: wizTo("paper", "mods"),
});
add("changed", "c-cart", "⑤ 待装清单：原来的「购物车」标签", {
  tag: "new", hash: `#/inst/${enc("机械动力整合包")}/cart`, note: "标签、按钮、提示里的「购物车」都改叫「待装清单」，「依赖」改叫「前置」。",
});
add("changed", "c-pickdir-fail", "① 选文件夹的窗口打不开时", {
  tag: "new", hash: "#/settings/storage", note: "原来右上角报「exit status 0xc000013a」。黑窗本身在原型里看不到（那是真程序的事，已经在真程序上测过），这里看的是打不开时的提示。",
  setup: (s, p) => p.api("POST /api/pickdir", { code: 500, error: "没能打开选择文件夹的窗口，请再试一次" }),
  steps: [{ click: '[data-pick="servers"]' }],
});
add("changed", "c-pickdir-busy", "① 已经开着一个选文件夹的窗口，又点了一次", {
  tag: "new", hash: "#/settings/storage",
  setup: (s, p) => p.api("POST /api/pickdir", { code: 409, error: "选择文件夹的窗口已经开着了，先在那个窗口里选好，或者把它关掉" }),
  steps: [{ click: '[data-pick="backups"]' }],
});
add("changed", "c-min-window", "② 最小窗口 1024×576", {
  tag: "new", view: "min", hash: "#/settings/ai", note: "窗口最小只能拖到这么大（16:9），侧栏还是完整的。原来能一直拖小到挤成一团。",
});

// ==================== 服务器列表 ====================
add("list", "l-empty", "还没有服务器", { setup: s => { s.empty = true; } });
add("list", "l-onboard", "第一次打开：引导和用户协议", { setup: s => { s.empty = true; s.config.onboarded = false; s.config.tosAccepted = false; } });
add("list", "l-four", "四台：运行中、已停止、启动中、代理端", { setup: s => { s.instances = four(); } });
add("list", "l-all", "十二种核心各一台", { note: "每种核心的像素头像、徽标和状态都在这一页。" });
add("list", "l-error", "拿不到服务器列表", {
  note: "报错的呈现方式：页面上一句中文结论，下一行写下一步（正在自动重试），原始报错收进「复制报错信息」，点一下复制到剪贴板。",
  setup: (s, p) => p.api("GET /api/instances", { code: 500, error: "instance store unavailable: open E:\\MineDeck\\servers: Access is denied" }),
});
add("list", "l-search", "搜索：只留名字里带「服」的", { steps: [{ fill: "#inst-q", value: "服" }] });
add("list", "l-search-none", "搜索：没有匹配的", { steps: [{ fill: "#inst-q", value: "不存在的名字" }] });
add("list", "l-menu", "卡片上的「···」菜单", { setup: s => { s.instances = four(); }, steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="more"]` }] });
add("list", "l-menu-running", "运行中的服务器：删除是灰的并写明原因", { setup: s => { s.instances = four(); }, steps: [{ click: `.card[data-name="生存服 · 朋友"] [data-act="more"]` }] });
add("list", "l-phone", "手机上（局域网远程访问）", { view: "phone", setup: s => { s.instances = four(); } });

// ==================== 新建服务器 ====================
add("create", "n-home", "入口：四种方式", { hash: "#/create" });
add("create", "n-home-tasks", "入口：有后台任务在跑", {
  hash: "#/create",
  setup: s => { s.taskList = [{ id: "task-pack", title: "导入 机械动力整合包", ended: false }, { id: "task-create", title: "创建 Paper 测试服", ended: false }, { id: "task-failed", title: "创建失败的服务器", ended: true, error: "下载失败" }]; },
});
add("create", "n-core", "从零创建 ① 选核心", { hash: "#/create/new" });
add("create", "n-version", "从零创建 ② 选版本（Paper）", { hash: "#/create/new", steps: wizTo("paper", "version") });
add("create", "n-version-vanilla", "从零创建 ② 选版本（原版，带发布日期）", { hash: "#/create/new", steps: wizTo("vanilla", "version") });
add("create", "n-build", "从零创建 ③ 选构建", { hash: "#/create/new", steps: wizTo("paper", "build") });
add("create", "n-mods", "从零创建 ④ 选模组（NeoForge）", { hash: "#/create/new", steps: wizTo("neoforge", "mods") });
add("create", "n-mods-search", "从零创建 ④ 选模组：搜「JEI」并加入清单", {
  hash: "#/create/new",
  steps: [...wizTo("fabric", "mods"), { fill: "#mc-q", value: "JEI" }, { click: "#mc-go" }, { click: '.mc-add[data-pid="jei"]' }],
});
add("create", "n-mods-local", "从零创建 ④ 选模组：从电脑添加了几个文件", {
  hash: "#/create/new", tag: "new",
  steps: [...wizTo("neoforge", "mods"), { upload: "#mc-file", files: LOCAL_FILES }, { sleep: 1200 }],
});
add("create", "n-mods-more", "从零创建 ④ 选模组：翻到第二页", {
  hash: "#/create/new", tag: "new", steps: [...wizTo("neoforge", "mods"), { click: "#mc-more" }],
});
add("create", "n-plugins", "从零创建 ④ 选插件（Paper）", { hash: "#/create/new", tag: "new", steps: wizTo("paper", "mods") });
add("create", "n-settings", "从零创建 ⑤ 基本设置", { hash: "#/create/new", steps: wizTo("paper", "settings") });
add("create", "n-settings-vanilla", "从零创建：原版只有四步", { hash: "#/create/new", steps: wizTo("vanilla", "settings") });
add("create", "n-settings-proxy", "从零创建：代理端（Velocity）的基本设置", { hash: "#/create/new", steps: wizTo("velocity", "settings") });
add("create", "n-eula", "从零创建：没同意 EULA 就点创建", { hash: "#/create/new", steps: [...wizTo("paper", "settings"), { click: "#wcreate" }] });
add("create", "n-import", "导入整合包", { hash: "#/create/import" });
add("create", "n-transfer", "导入交接包", { hash: "#/create/transfer" });
add("create", "n-adopt", "接管已有服务器", { hash: "#/create/adopt" });
add("create", "n-adopt-scan", "接管已有服务器：识别出来了", {
  hash: "#/create/adopt", steps: [{ fill: "#ad-dir", value: "E:\\MCServers\\create-pack" }, { click: "#ad-scan" }],
});
add("create", "n-create-min", "入口：最小窗口下", { view: "min", hash: "#/create" });
// 用已有地图开服：仿真后端的地图是按来源的名字装样子的（见 ../server.js「地图导入」一节），「选择地图文件夹」选到的那个是一张 1.19.4 的冒险地图
const wizMap = [{ click: "#wi-wiz-pick" }, { click: "#wi-browse" }, { wait: "#wi-wiz-chosen" }];
add("create", "n-world", "从零创建：第一步的「用已有地图开服」", {
  hash: "#/create/new", tag: "new", note: "步骤条下面多了一条。点进去选地图文件夹（这台电脑上）或者上传 ZIP；别的设备上只有上传。",
  steps: [{ click: "#wi-wiz-pick" }, { wait: "#wi-upload" }],
});
add("create", "n-world-chosen", "从零创建：选好了地图，核心和版本按地图预选", {
  hash: "#/create/new", tag: "new", note: "核心按存档里的线索预选（这张没有线索，预选原版），版本预选地图保存的 1.19.4，不是最新版。两样都可以改；「换一张」「不用地图」在那一条的右边。",
  steps: [...wizMap, { click: "#wnext" }, { wait: ".ver-item.sel" }],
});
add("create", "n-world-final", "从零创建：最后一步核对这张地图", {
  hash: "#/create/new", tag: "new", note: "换成 Paper 1.21.4 以后地图比服务器旧，要勾「我已了解上面需要确认的事项」才能创建；被标红拦下时创建键点了也不发请求。难度和默认模式不替人改，点「按地图的填」才改。",
  steps: [...wizMap, { click: '.core-card[data-id="paper"]' }, { click: "#wnext" }, { wait: ".ver-item.sel" }, { click: '.ver-item[data-id="1.21.4"]' }, { click: "#wnext" },
    { wait: "#build-list .ver-item" }, { click: "#wnext" }, { wait: "#wiz-mod-body" }, { click: "#wnext" }, { wait: "#wi-wiz-plan #wi-rel" }, { scroll: "#main", to: "bottom" }],
});

// ==================== 部署与后台任务 ====================
add("task", "t-running", "部署中", { hash: "#/task/task-1", setup: s => { s.taskStage = 2; } });
add("task", "t-slow", "部署中：下载慢、在重试", { hash: "#/task/task-1", setup: s => { s.taskStage = 1; s.taskNet = { count: 3, host: "fill-data.papermc.io", what: "Paper 核心", ago: 42 }; } });
add("task", "t-error", "部署失败", { hash: "#/task/task-2", setup: s => { s.taskStage = "error"; } });
add("task", "t-done", "部署完成", { hash: "#/task/task-1", setup: s => { s.taskStage = 4; } });
add("task", "t-mods", "安装待装清单", { hash: "#/task/task-cart", tag: "new" });

// ==================== 服务器详情：每种核心一台 ====================
const CORE_CN = { paper: "Paper", forge: "Forge", vanilla: "Vanilla 原版", velocity: "Velocity 代理", purpur: "Purpur", leaves: "Leaves", folia: "Folia", fabric: "Fabric", neoforge: "NeoForge", mohist: "Mohist", banner: "Banner", waterfall: "Waterfall 代理" };
const ST_CN = { running: "运行中", stopped: "已停止", starting: "启动中", stopping: "停止中" };
for (const i of INSTANCES) {
  add("detail", "d-" + i.core, `${CORE_CN[i.core]} · ${i.name}（${ST_CN[i.status]}）`, { hash: `#/inst/${enc(i.name)}/console` });
}
add("detail", "d-players", "玩家：在线、白名单、管理员、封禁", { hash: `#/inst/${enc("周末活动服")}/players` });
add("detail", "d-backups", "备份", { hash: `#/inst/${enc("生存服 · 朋友")}/backups` });
add("detail", "d-backups-empty", "备份：一份都没有", { hash: `#/inst/${enc("Banner 试验场")}/backups` });
add("detail", "d-world", "地图：当前世界和恢复点", {
  hash: `#/inst/${enc("机械动力整合包")}/world`, tag: "new",
  note: "三条恢复点各是一种状态：还能换回的、已经换回（导入的那份留在旁边）的、中途取消没生效的。换回和删除都要过确认框。",
});
add("detail", "d-world-plan", "地图：导入前的核对页", {
  hash: `#/inst/${enc("生存服 · 朋友")}/world`, tag: "new",
  note: "最要紧的一屏：版本关系、要确认的事（只有一个勾选框）、地图里有什么、会换掉什么、可以一起改的设置（默认都不勾）、说明书。服务器开着，所以底下还有「停服并导入」。",
  steps: [{ click: "#wi-start" }, { click: "#wi-browse" }, { wait: "#wi-go" }],
});
add("detail", "d-world-cands", "地图：一个压缩包里有三张地图", {
  hash: `#/inst/${enc("机械动力整合包")}/world`, tag: "new", note: "列出来让人选，不替人挑第一张；level.dat 读不出来的那张选不了。",
  steps: [{ click: "#wi-start" }, { fill: "#wi-path", value: "D:\\Maps\\multi-pack.zip" }, { click: "#wi-scan" }, { wait: "[data-wi-pick]" }],
});
add("detail", "d-world-reject", "地图：选到的是基岩版世界", {
  hash: `#/inst/${enc("机械动力整合包")}/world`, tag: "new", note: "不是能导入的 Java 版世界时，原因摆在最前面，到此为止。蓝图、资源包、整合包、RAR 也是这个样子。",
  steps: [{ click: "#wi-start" }, { fill: "#wi-path", value: "D:\\Maps\\bedrock-world" }, { click: "#wi-scan" }, { wait: "#wi-reject" }],
});
add("detail", "d-world-stuck", "地图：上次换地图卡住了", {
  hash: `#/inst/${enc("机械动力整合包")}/world`, tag: "new", note: "自动恢复没成功时服务器暂停启动。页面顶上写原因和要看的文件夹，处理完点「重试恢复」。",
  setup: s => {
    s.world.insts["机械动力整合包"] = { levelName: "world", busy: "上次换地图没能自动恢复，服务器已暂停启动。请到「地图 / 世界」里查看恢复步骤",
      points: [{ id: "w9aa10001", state: "stuck", created: "2026-11-12T20:10:00+08:00", source: "SkyCity.zip", candidate: "天空之城", levelName: "world", hasOld: true, oldBytes: 5e8, keptBytes: 0, canReturn: false,
        error: "rename world: Access is denied.", stuck: ["D:\\MineDeck\\servers\\create-pack\\world", "D:\\MineDeck\\servers\\create-pack\\.minedeck\\world-import\\w9aa10001\\old\\world"] }] };
  },
});
add("detail", "d-world-phone", "地图：手机上（只能上传 ZIP）", {
  view: "phone", hash: `#/inst/${enc("机械动力整合包")}/world`, tag: "new",
  setup: s => { s.local = false; },
  steps: [{ click: "#wi-start" }, { wait: "#wi-upload" }],
});
add("detail", "d-sched", "定时任务", { hash: `#/inst/${enc("生存服 · 朋友")}/tasks` });
add("detail", "d-res", "资源（搜到就装）", { hash: `#/inst/${enc("生存服 · 朋友")}/res`, steps: [{ click: "#res-go" }] });
add("detail", "d-cart", "待装清单：加了一项并检查前置", {
  hash: `#/inst/${enc("机械动力整合包")}/cart`, tag: "new",
  steps: [{ fill: "#mc-q", value: "JEI" }, { click: "#mc-go" }, { click: '.mc-add[data-pid="jei"]' }, { sleep: 500 }, { click: "#mc-dep-go" }],
});
add("detail", "d-cart-local", "待装清单：有本地文件，检查出要自己找的前置", {
  hash: `#/inst/${enc("机械动力整合包")}/cart`, tag: "new",
  steps: [{ click: '.mc-add[data-pid="create"]' }, { upload: "#mc-file", files: [{ name: "sodium-extra-forge-0.6.0.jar", kb: 412 }, { name: "needs-geckolib-cataclysm-forge-2.43.jar", kb: 2380 }, { name: "lithium-fabric-0.13.1.jar", kb: 690 }] }, { sleep: 1200 }],
});
add("detail", "d-cart-plugin", "待装清单：插件端（Purpur）", { hash: `#/inst/${enc("空岛生存")}/cart`, tag: "new" });
add("detail", "d-common", "常用设置", { hash: `#/inst/${enc("生存服 · 朋友")}/common` });
add("detail", "d-common-dirty", "常用设置：改了没保存", { hash: `#/inst/${enc("生存服 · 朋友")}/common`, steps: [{ click: '.prop-item:has([data-k="hardcore"]) .switch input' }] });
add("detail", "d-raw", "全部配置", { hash: `#/inst/${enc("生存服 · 朋友")}/raw` });
add("detail", "d-jvm", "内存 / 启动", { hash: `#/inst/${enc("机械动力整合包")}/jvm` });
add("detail", "d-proxy-common", "代理端没有游戏设置", { hash: `#/inst/${enc("群组代理")}/jvm` });
add("detail", "d-dirmenu", "「打开文件夹」菜单", { hash: `#/inst/${enc("生存服 · 朋友")}/console`, steps: [{ click: "#d-dir" }] });
add("detail", "d-min", "详情页：最小窗口下", { view: "min", hash: `#/inst/${enc("周末活动服")}/console` });
add("detail", "d-phone", "详情页：手机上", { view: "phone", hash: `#/inst/${enc("生存服 · 朋友")}/console` });

// ==================== 内网穿透 ====================
add("tunnel", "u-empty", "还没有隧道", { hash: "#/tunnels", tag: "new", setup: s => { s.tunnels = []; } });
add("tunnel", "u-two", "两条：一条在跑，一条上次出错", { hash: "#/tunnels" });
add("tunnel", "u-form", "展开添加表单（OpenFrp）", { hash: "#/tunnels", tag: "new", steps: [{ click: "#tn-toggle" }] });
add("tunnel", "u-form-natfrp", "添加表单：樱花 frp", { hash: "#/tunnels", tag: "new", setup: s => { s.tunnels = []; }, steps: [{ select: "#tn-provider", value: "natfrp" }] });
add("tunnel", "u-form-custom", "添加表单：自定义 frps", { hash: "#/tunnels", tag: "new", setup: s => { s.tunnels = []; }, steps: [{ select: "#tn-provider", value: "custom" }] });
add("tunnel", "u-form-voice", "添加表单：语音隧道（给音乐共享用）", { hash: "#/tunnels", setup: s => { s.tunnels = []; }, steps: [{ select: "#tn-purpose", value: "voice" }] });
add("tunnel", "u-cred", "凭据解不开（换了电脑或 Windows 用户）", { hash: "#/tunnels", setup: s => { for (const t of s.tunnels) t.credBroken = t.id === "t2"; } });
add("tunnel", "u-log", "展开日志", { hash: "#/tunnels", steps: [{ click: '[data-tlog="t1"]' }] });
add("tunnel", "u-list-error", "拿不到隧道列表", {
  hash: "#/tunnels", tag: "new", note: "页面报错框：一句结论，下面「复制报错信息」。原文带着路径和英文，所以正文不接它。",
  setup: (s, p) => p.api("GET /api/tunnels", { code: 500, error: "读取隧道配置失败: open D:\\MineDeck\\tunnels.json: Access is denied." }),
});
add("tunnel", "u-min", "最小窗口下", { view: "min", hash: "#/tunnels", setup: s => { s.tunnels = []; } });
add("tunnel", "u-phone", "手机上", { view: "phone", hash: "#/tunnels" });

// ==================== 设置 ====================
for (const [id, label] of [["appearance", "外观"], ["download", "下载与网络"], ["storage", "存储位置"], ["java", "Java 运行时"], ["remote", "远程访问"], ["notify", "通知与集成"], ["ai", "AI（可选）"], ["startup", "启动与托盘"], ["about", "更新与关于"]]) {
  add("settings", "s-" + id, label, { hash: "#/settings/" + id });
}
add("settings", "s-remote-on", "远程访问：已开启（二维码）", { hash: "#/settings/remote", setup: s => { s.config.listenLan = s.lanActive = true; s.lanSet = true; } });
add("settings", "s-ai-on", "AI：已经配好一家", {
  hash: "#/settings/ai",
  setup: s => { Object.assign(s.ai, { preset: "deepseek", protocol: "openai", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash", key: "sk-demo-not-a-real-key" }); },
});
add("settings", "s-ai-test-fail", "AI：测试连接失败（一行字里的报错）", {
  hash: "#/settings/ai", tag: "new",
  note: "一行字里的报错：警告图标、结论，后面一个只有图标的「复制报错信息」。这次的原因带着 HTTP 状态码，按规矩只写结论，原文点复制键拿走。",
  setup: s => { Object.assign(s.ai, { preset: "deepseek", protocol: "openai", baseUrl: "https://api.deepseek.com", model: "deepseek-v4-flash", key: "sk-demo-not-a-real-key", testFail: true }); },
  steps: [{ click: "#ai-test" }, { wait: "#ai-test-sub .err-inline" }],
});

// ==================== 弹窗、提示和助手 ====================
add("overlay", "o-delete", "删除服务器的确认框", {
  setup: s => { s.instances = four(); },
  steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="more"]` }, { click: ".dropmenu button.danger" }],
});
add("overlay", "o-guard", "还有修改没保存就想走", {
  hash: `#/inst/${enc("生存服 · 朋友")}/common`,
  steps: [{ click: '.prop-item:has([data-k="hardcore"]) .switch input' }, { click: '.tabs .tab[data-t="console"]' }],
});
add("overlay", "o-toast-err", "右上角的报错提示：停 8 秒，带「详情」和关闭", {
  hash: "#/settings/storage",
  setup: (s, p) => p.api("POST /api/pickdir", { code: 500, error: "没能打开选择文件夹的窗口，请再试一次" }),
  steps: [{ click: '[data-pick="servers"]' }],
});
add("overlay", "o-err-detail", "报错详情：原文可以选中、复制", {
  hash: "#/settings/storage",
  setup: (s, p) => p.api("POST /api/pickdir", { code: 500, error: "没能打开选择文件夹的窗口，请再试一次" }),
  steps: [{ click: '[data-pick="servers"]' }, { click: '.toast.error button[aria-label="查看报错详情"]' }],
});
add("overlay", "o-toast-err-why", "报错提示：后端说的原因接在结论后面", {
  tag: "new", note: "结论（这一步没做成什么）在前；后端给的是一句看得懂的中文时整句接在后面。原文照样在「详情」里。",
  setup: (s, p) => { s.instances = four(); p.api(`POST /api/instances/${enc("机械动力整合包")}/start`, { code: 400, error: "端口 25566 被别的程序占着，换个端口或关掉占用它的程序" }); },
  steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="start"]` }],
});
add("overlay", "o-toast-err-raw", "报错提示：原文带路径和英文时只写结论，「详情」里是原文", {
  tag: "new", note: "原文里有文件路径、英文报错句、网址这类痕迹，或者超过 60 个字时，正文只写结论；原文一个字不少地放在「详情」里，可以选中、复制。",
  setup: (s, p) => { s.instances = four(); p.api(`POST /api/instances/${enc("机械动力整合包")}/start`, { code: 500, error: "启动失败: fork/exec D:\\MineDeck\\runtimes\\java17\\bin\\java.exe: The system cannot find the file specified." }); },
  steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="start"]` }, { click: '.toast.error button[aria-label="查看报错详情"]' }],
});
add("overlay", "o-toast", "右上角的提示", { setup: s => { s.instances = four(); }, steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="start"]` }] });
add("overlay", "o-asst", "AI 助手面板", { steps: [{ click: 'button[aria-label^="AI 助手"]' }] });

module.exports = { GROUPS, SCENES: S, base };
