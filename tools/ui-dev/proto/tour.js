// 给访客看的导览：从场景清单（scenes.js）里挑十来个最能说明产品的，排成一条能顺着看下去的线，
// 每一步配一个标题和一两句话，写给没用过 MineDeck、也不懂技术的玩家看。官网的「在线体验」用的就是这一份。
//
// 一步就是一个场景，和目录页里的是同一种东西：use 写的是 scenes.js 里某个场景的编号，落在哪一页、自动点哪几下都照它的；
// 这里再写的字段（hash、steps、view、setup）会盖掉它的。所以界面的选择器变了只用改 scenes.js 一处，自检也是同一套。
// 和目录页不一样的地方只有数据：导览里从头到尾都是那四台服务器，免得上一步四台、下一步十二台。
//
// text 按语言分开写（界面本身只有中文，英文只是外面这层说明）；beta: true 的步骤是测试版才有的功能，导览页会标出来。
"use strict";
const { INSTANCES } = require("./fiction");
const { SCENES } = require("./scenes");

const enc = encodeURIComponent;
const HOME = "生存服 · 朋友", PACK = "机械动力整合包";
const four = () => JSON.parse(JSON.stringify(INSTANCES.slice(0, 4)));

const T = [];
function step(id, use, o, text) {
  const from = use ? SCENES.find(s => s.id === use) : {};
  if (!from) throw new Error(`导览的「${id}」要用的场景 ${use} 不在 scenes.js 里`);
  const { group, tag, title, note, setup: fromSetup, ...rest } = from;
  T.push({
    hash: "#/instances", ...rest, ...o, id, text,
    setup: (s, p) => { s.instances = four(); if (fromSetup) fromSetup(s, p); if (o.setup) o.setup(s, p); },
  });
}

step("v-home", "l-four", {}, {
  zh: { label: "首页", title: "你的服务器都在这", note: "每台服务器一张卡片：开没开着、几个人在线、用的什么服务端，一眼就看到。开服、停服直接点卡片上的按钮，可以把停着的那台启动起来试试。" },
  en: { label: "Home", title: "All your servers in one place", note: "Each server is a card: whether it is running, how many players are online, which server software it uses. Start and stop right from the card. Try starting one that is stopped." },
});
step("v-create", "n-home", { beta: true }, {
  zh: { label: "新建", title: "四种开服的办法", note: "从零建一台、导入整合包、收下朋友打包发来的服务器，或者把电脑里已经有的服务器接过来管。下一步看「从零创建」。" },
  en: { label: "New server", title: "Four ways to get a server", note: "Create one from scratch, import a modpack, take over a server a friend packed up for you, or adopt one that already lives on your PC. The next steps walk through creating from scratch." },
});
step("v-core", "n-core", {}, {
  zh: { label: "选服务端", title: "第一步：想开哪种服", note: "原版、能装插件的 Paper、装模组的 Forge、NeoForge、Fabric……十二种服务端都列在这，每种写了一句它适合干什么。点一张卡片就选中了。" },
  en: { label: "Pick software", title: "Step one: what kind of server", note: "Vanilla, Paper for plugins, Forge, NeoForge and Fabric for mods: twelve kinds of server software, each with a line on what it is good for. Click a card to choose." },
});
step("v-version", "n-version", {}, {
  zh: { label: "选版本", title: "第二步：选游戏版本", note: "版本直接从列表里点，不用自己去找下载地址。这个版本要用哪个 Java，MineDeck 会自己配好，电脑上没有就帮你下载。" },
  en: { label: "Pick version", title: "Step two: pick the game version", note: "Choose a version from the list. No hunting for download links. MineDeck works out which Java that version needs and downloads it if your PC does not have it." },
});
step("v-mods", "n-mods", { beta: true }, {
  zh: { label: "挑模组", title: "顺手把模组挑好", note: "开模组服的话，这一步可以先挑模组：左边是 Modrinth 上最热门的，点「加入清单」就进右边的待装清单，服务器建好以后自动装上，缺的前置模组也一起补齐。自己下载的 jar 也能放进来。" },
  en: { label: "Pick mods", title: "Choose your mods while you are here", note: "For a modded server you can pick mods right in the wizard. The left side lists the most popular ones on Modrinth; add them to the list on the right and they are installed once the server is created, together with any required dependencies. Jar files you downloaded yourself can go in too." },
});
step("v-settings", "n-settings", {}, {
  zh: { label: "基本设置", title: "最后：起名字、定内存", note: "起个名字，定一下内存和端口，同意 Minecraft 的用户协议（EULA），点「开始部署」。下一步看部署的过程。" },
  en: { label: "Settings", title: "Last step: name and memory", note: "Give it a name, set memory and port, accept the Minecraft EULA and start the deployment. The next step shows what happens then." },
});
step("v-task", "", {
  hash: "#/task/task-1",
  // 一进来就有一台刚点了「创建」的服务器，进度从第一步走到完成（见 demo.js）
  setup: (s, p) => p.create({ name: "我的第一个服", core: "paper", mc: "1.21.4", build: "232", javaMajor: 21, xmxMb: 4096, port: 25568, dir: "D:\\MineDeck\\servers\\my-first-server" }),
}, {
  zh: { label: "自动部署", title: "剩下的交给它", note: "Java、服务端核心、配置文件、启动脚本，MineDeck 一步一步自己弄好，进度都写在这里。走完以后可以直接进这台服务器。" },
  en: { label: "Setup", title: "MineDeck does the rest", note: "Java, the server software, config files and a launch script: MineDeck sets them up one by one and shows its progress here. When it finishes you can open the new server straight away." },
});
step("v-console", "", { hash: `#/inst/${enc(HOME)}/console` }, {
  zh: { label: "控制台", title: "服务器在说什么", note: "服务器的输出实时显示在这里，最下面那一行可以直接发命令。可以输入 list 看看谁在线，或者 say 大家好。" },
  en: { label: "Console", title: "See what the server is saying", note: "Server output appears here as it happens, and the box at the bottom sends commands. Try typing list to see who is online, or say hello." },
});
step("v-players", "", { hash: `#/inst/${enc(HOME)}/players` }, {
  zh: { label: "玩家", title: "谁能进、谁是管理员", note: "在线的人、白名单、管理员、封禁名单都在这一页。加人、踢人、封禁点按钮就行，不用背命令。" },
  en: { label: "Players", title: "Who can join, who is an admin", note: "Online players, the whitelist, operators and bans are all on this page. Add, kick or ban with a click instead of memorising commands." },
});
step("v-backups", "d-backups", {}, {
  zh: { label: "备份", title: "存档丢不了", note: "随时点一下就备份世界，也可以定时自动备份。想回到之前的存档就点还原，还原之前会先把现在的世界再存一份。" },
  en: { label: "Backups", title: "Keep your world safe", note: "Back up the world with one click, or on a daily schedule. Restoring is one click too, and the current world is saved first so nothing is lost." },
});
step("v-cart", "d-cart", { beta: true }, {
  zh: { label: "加模组", title: "开服以后再加模组", note: "服务器已经建好了还想加模组：搜到以后放进待装清单，检查一下缺不缺前置，一次装好。" },
  en: { label: "Add mods", title: "Add mods later on", note: "Want more mods after the server is up? Search, add them to the list, check for missing dependencies and install everything in one go." },
});
step("v-tunnel", "u-two", {}, {
  zh: { label: "内网穿透", title: "没有公网 IP 也能联机", note: "接上 OpenFrp、樱花穿透或者自己的 frp 服务器，启动隧道，把公网地址复制给朋友，他们就能连进来。" },
  en: { label: "Tunnels", title: "Play together without a public IP", note: "Connect OpenFrp, SakuraFrp or your own frp server, start the tunnel and send friends the public address. That is all they need to join." },
});
step("v-look", "s-appearance", {}, {
  zh: { label: "外观", title: "换个样子", note: "七套配色，浅色、深色或者跟着系统走。在这里点一下，整个界面当场就换。" },
  en: { label: "Appearance", title: "Make it yours", note: "Seven colour schemes, each in light or dark, or following your system. Click one here and the whole interface changes on the spot." },
});
step("v-phone", "l-phone", {}, {
  zh: { label: "手机上", title: "不在电脑前也能管", note: "打开「远程访问」并设好密码以后，连着同一个 Wi-Fi 的手机用浏览器就能打开这个界面。这就是手机上的样子。" },
  en: { label: "On your phone", title: "Manage it away from the PC", note: "Turn on remote access, set a password, and any phone on the same Wi-Fi can open this interface in its browser. This is what it looks like there." },
});

module.exports = { TOUR: T };
