/* MineDeck v3.1.0 宣传片：节拍网格、分段、所有上屏文字。场景页（scene.js）和检查脚本（check-text.js）都读这一份。
   节拍跟着配乐走：Morning on the Balcony 实测 123.99 BPM（analyze-music.py 量的），一拍 0.48391 秒，一小节 1.93564 秒。
   画面从曲子的第 3 小节（曲子里 5.243 秒）开始，所以画面第 b 小节 = 曲子第 b + 2 小节：
     第 3 小节（3.87 秒）低音进来，图标落定；第 6 小节（9.68 秒）鼓进来，进入第一章；
     第 22 小节（40.65 秒）曲子安静下来，放「自己的服不想开了？」；第 26 小节（48.39 秒）主段落落下，交接包的动画；
     第 42 小节（79.36 秒）曲子收尾，片尾。曲子在 84 秒左右收尽。
   T(小节, 拍) 把「第几小节（从 1 数）第几拍（从 0 数，可以是小数）」换成秒。 */
(function (root) {
  "use strict";
  const BPM = 123.99, BEAT = 60 / BPM, BAR = 4 * BEAT, FPS = 60;
  const T = (bar, beat = 0) => (bar - 1) * BAR + beat * BEAT;
  const BARS = 43;
  const DURATION = 84; // 第 44 小节开头是 83.23 秒，曲子的尾音到 84 秒
  const MUSIC_START = 5.2431; // 画面 0 秒对应曲子里的这一秒（曲子第 3 小节的第一拍）

  // 分段（对着曲子的段落）
  const SECTIONS = [
    { id: "hook",     bars: [1, 5],   name: "开场：一句话 → 方块拼出图标（第 3 小节落定）→ 新建服务器的四种方式", music: "前奏（钢琴），第 3 小节低音进来" },
    { id: "adopt",    bars: [6, 11],  name: "接管已有服务器、换 Java、移出列表", music: "律动进来（鼓、贝斯）" },
    { id: "mods",     bars: [12, 17], name: "选模组、待装清单、前置、本地 jar、一次装好", music: "律动" },
    { id: "pack",     bars: [18, 21], name: "导入整合包：待决文件、现成服务端", music: "律动" },
    { id: "turn",     bars: [22, 25], name: "「自己的服不想开了？」→ 导出交接包的菜单和弹窗", music: "安静的过渡，后两小节蓄力" },
    { id: "handover", bars: [26, 31], name: "交接包：打包、发给朋友、对方导入、开起来", music: "主段落落下" },
    { id: "more",     bars: [32, 35], name: "报错、frpc 被拦、Java 提醒、后台任务", music: "主段落" },
    { id: "release",  bars: [36, 40], name: "正式版前的检查、升级、一个文件就够了、回顾", music: "主段落" },
    { id: "outro",    bars: [41, 43], name: "片尾卡（第 42 小节起文字到位）", music: "第 42 小节起收尾" },
  ];

  // 分段之间的像素擦除转场（落在小节线上）
  const CUTS = [T(4), T(6), T(12), T(18), T(22), T(26), T(32), T(36), T(40)];

  // 字幕：t0–t1 秒；eb 眉标，main 主句，sub 副句。每行 ≤ 18 字，只用 GB2312 里的字。
  const one = (bar, eb, main, sub) => ({ t0: T(bar, 0.25), t1: T(bar + 1, -0.25), eb, main, sub });
  const CAPTIONS = [
    { t0: T(4, 0.5), t1: T(6, -0.25), eb: "新建服务器", main: "现在有四种方式", sub: "从零创建之外，还能导入和接管" },
    one(6, "接管已有服务器", "以前开的服务器，选中它的文件夹"),
    one(7, "接管已有服务器", "核心、版本、启动文件自动认出", "原来的内存和端口也带上"),
    one(8, "接管已有服务器", "就地登记，文件不搬", "存档、模组和配置都不动"),
    one(9, "接管已有服务器", "和新建的服务器一样管"),
    one(10, "换 Java", "每台服务器可以单独换 Java", "下载的、电脑上装的，或现下一个"),
    one(11, "移出列表", "不想管了，只从列表移出", "文件都在，以后还能接管回来"),
    one(12, "选模组", "新建向导里多了一步：选模组", "按核心和版本列出热门模组"),
    one(13, "选模组", "按类别筛，也能直接搜", "插件服这一步叫「选插件」"),
    one(14, "待装清单", "挑中的放进待装清单"),
    one(15, "待装清单", "缺的前置，自动一起装上", "建好的服务器也有待装清单"),
    one(16, "待装清单", "自己下载的 jar 也能加", "加载器不对的会标出来"),
    one(17, "待装清单", "点安装，一次装好", "从 Modrinth 下载并校验"),
    one(18, "导入整合包", "个别文件下载不了，也照常导入", "可以填旧服务器的文件夹来补"),
    one(19, "导入整合包", "下载不了的，列进待决文件"),
    one(20, "导入整合包", "跳过、本地代替，或信任后下载", "每个文件自己选"),
    one(21, "现成服务端", "核心已经在包里的服务端压缩包", "直接导入成一台新服务器"),
    one(24, "交接包", "整台服务器，打包交给别人"),
    one(25, "交接包", "交给下一任服主，或发给朋友照着开"),
    one(26, "交接包", "存档、配置、模组、名单都在包里", "核心默认不带，导入时重新下载"),
    one(27, "交接包", "一个 .mdpack，发给朋友"),
    one(28, "导入交接包", "对方在「导入交接包」里选中它", "只导入信得过的人给的包"),
    one(29, "导入交接包", "Java 和核心在他电脑上重新准备"),
    one(30, "导入交接包", "接着开，存档都在"),
    one(31, "交接包", "换电脑、换服主，服务器接着开"),
    one(32, "报错", "报错先说没做成什么", "原文点「详情」能看、能复制"),
    one(33, "内网穿透", "frpc 被杀毒软件拦了，直接说", "还告诉你去哪里允许或还原"),
    one(34, "Java 提醒", "模组要更新的 Java，会提醒你换"),
    one(35, "后台任务", "部署切走了，回来还能找到进度"),
    one(37, "升级", "用着 3.0 的，设置里检查更新", "服务器、Java 和设置都接着用"),
  ];

  // 场景里的其他文字
  const TEXT = {
    hookQ: "开服，不一定从零开始",
    word: "MineDeck",
    badge: "v3.1.0 正式版",
    hookTag: "我的世界 Windows 一键开服工具",
    turn: ["自己的服不想开了？", "或者，想让朋友也开一台"],
    // 换 Java 的下拉列表：capture.js 从真界面里读出来的选项（out/shots/extra.json），这里写一份给文字检查用
    javaOpts: ["Java 21（MineDeck 下载的） · 现在用的", "Java 17（MineDeck 下载的）",
      "Java 21（本机装的 21.0.4：C:\\Program Files\\Eclipse Adoptium\\jdk-21.0.4.7-hotspot\\bin\\java.exe）",
      "Java 8（本机装的 1.8.0_421：C:\\Program Files\\Java\\jre1.8.0_421\\bin\\java.exe）",
      "Java 11（本机没有，下载一份，约 50 MB）", "Java 16（本机没有，下载一份，约 50 MB）", "Java 25（本机没有，下载一份，约 50 MB）"],
    packItems: ["存档", "server.properties", "mods", "config", "白名单", "管理员名单"],
    packFile: "机械动力整合包.mdpack",
    packSize: "1.9 GB",
    pcs: ["你的电脑", "朋友的电脑"],
    srvName: "机械动力整合包",
    srvMeta: "Forge 1.20.1",
    srvState: ["已停止", "运行中"],
    relTitle: "正式版前，从头查了一遍",
    relChips: ["自动测试", "真机测试", "安全审查"],
    exeTitle: "一个文件就够了",
    exeFile: "MineDeck.exe",
    exeSize: "大约 15 MB",
    exeRows: ["不用安装，双击运行", "Java 不用自己装", "数据都在 exe 所在的文件夹"],
    recap: ["接管", "挑模组", "导入", "交接"],
    endLine: ["免费开源", "Windows 10/11", "v3.1.0"],
    endUrl: "trasperioooooon.github.io/MineDeck",
    endSearch: "GitHub 搜索 MineDeck",
    endNote: "非 Minecraft 官方产品，与 Mojang、微软、网易无关",
  };

  const out = { BPM, BEAT, BAR, FPS, T, BARS, DURATION, MUSIC_START, SECTIONS, CUTS, CAPTIONS, TEXT };
  if (typeof module !== "undefined" && module.exports) module.exports = out;
  else root.PROMO = out;
})(this);
