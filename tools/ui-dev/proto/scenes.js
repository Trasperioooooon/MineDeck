// 界面原型的场景清单。一个场景 = 仿真后端摆成什么样（setup）+ 界面停在哪一页（hash）+ 进页面后自动点几下（steps）。
// 目录页（index.html）按 group 分组列出来，点一个就切过去；切之前仿真后端先回到 base() 的样子，场景之间互不影响。
//
// steps 是一串动作，在嵌着的界面里依次执行（每步都会先等元素出现）：
//   { click: 选择器 }  { dblclick: 选择器 }  { fill: 选择器, value: 文字 }  { select: 选择器, value: 值 }
//   { wait: 选择器 }   { sleep: 毫秒 }       { key: "Escape" }            { scroll: 选择器, to: "bottom" }
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
  tag: "new", note: "主按钮不再是「一键开服（Paper）」，点了直接进从零创建的向导；下面那行小字去新建页挑别的方式。",
  setup: s => { s.empty = true; },
});
add("changed", "c-create", "③ 新建页：四张卡片分两组", {
  tag: "new", hash: "#/create", note: "四张卡片一样大，分「新开一台」和「把现成的搬进来」。每种方式有自己的地址，后退键能回到这里。",
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
add("create", "n-core", "从零创建 ① 选择核心", { hash: "#/create/new" });
add("create", "n-version", "从零创建 ② 选择版本（Paper）", { hash: "#/create/new", steps: wizTo("paper", "version") });
add("create", "n-version-vanilla", "从零创建 ② 选择版本（原版，带发布日期）", { hash: "#/create/new", steps: wizTo("vanilla", "version") });
add("create", "n-build", "从零创建 ③ 选择构建", { hash: "#/create/new", steps: wizTo("paper", "build") });
add("create", "n-mods", "从零创建 ④ 选模组（NeoForge）", { hash: "#/create/new", steps: wizTo("neoforge", "mods") });
add("create", "n-mods-search", "从零创建 ④ 选模组：搜「JEI」并加入清单", {
  hash: "#/create/new",
  steps: [...wizTo("fabric", "mods"), { fill: "#mc-q", value: "JEI" }, { click: "#mc-go" }, { click: '.mc-add[data-pid="jei"]' }],
});
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
add("detail", "d-sched", "定时任务", { hash: `#/inst/${enc("生存服 · 朋友")}/tasks` });
add("detail", "d-res", "资源（搜到就装）", { hash: `#/inst/${enc("生存服 · 朋友")}/res`, steps: [{ click: "#res-go" }] });
add("detail", "d-cart", "待装清单：加了一项并检查前置", {
  hash: `#/inst/${enc("机械动力整合包")}/cart`, tag: "new",
  steps: [{ fill: "#mc-q", value: "JEI" }, { click: "#mc-go" }, { click: '.mc-add[data-pid="jei"]' }, { sleep: 500 }, { click: "#mc-dep-go" }],
});
add("detail", "d-common", "常用设置", { hash: `#/inst/${enc("生存服 · 朋友")}/common` });
add("detail", "d-common-dirty", "常用设置：改了没保存", { hash: `#/inst/${enc("生存服 · 朋友")}/common`, steps: [{ click: '.prop-item:has([data-k="hardcore"]) .switch input' }] });
add("detail", "d-raw", "全部配置", { hash: `#/inst/${enc("生存服 · 朋友")}/raw` });
add("detail", "d-jvm", "内存 / 启动", { hash: `#/inst/${enc("机械动力整合包")}/jvm` });
add("detail", "d-proxy-common", "代理端没有游戏设置", { hash: `#/inst/${enc("群组代理")}/jvm` });
add("detail", "d-dirmenu", "「打开目录」菜单", { hash: `#/inst/${enc("生存服 · 朋友")}/console`, steps: [{ click: "#d-dir" }] });
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

// ==================== 弹窗、提示和助手 ====================
add("overlay", "o-delete", "删除服务器的确认框", {
  setup: s => { s.instances = four(); },
  steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="more"]` }, { click: ".dropmenu button.danger" }],
});
add("overlay", "o-guard", "还有修改没保存就想走", {
  hash: `#/inst/${enc("生存服 · 朋友")}/common`,
  steps: [{ click: '.prop-item:has([data-k="hardcore"]) .switch input' }, { click: '.tabs .tab[data-t="console"]' }],
});
add("overlay", "o-toast", "右上角的提示", { setup: s => { s.instances = four(); }, steps: [{ click: `.card[data-name="机械动力整合包"] [data-act="start"]` }] });
add("overlay", "o-asst", "AI 助手面板", { steps: [{ click: 'button[aria-label^="AI 助手"]' }] });

module.exports = { GROUPS, SCENES: S, base };
