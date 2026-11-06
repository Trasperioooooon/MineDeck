/* MineDeck 宣传视频：节拍网格、分段、所有上屏文字。场景页（scene.js）和检查脚本（check-text.js）都读这一份。
   节拍：120 BPM，一拍 0.5 秒 = 30 帧，一小节（4 拍）2 秒 = 120 帧。T(小节, 拍) 把「第几小节（从 1 数）第几拍（从 0 数，可以是小数）」换成秒。
   全片 38 小节 = 76 秒 = 4560 帧。分段都落在小节线上，切镜头和主要动作落在拍上。 */
(function (root) {
  "use strict";
  const BPM = 120, BEAT = 60 / BPM, BAR = 4 * BEAT, FPS = 60;
  const T = (bar, beat = 0) => (bar - 1) * BAR + beat * BEAT;
  const BARS = 38;
  const DURATION = T(BARS + 1); // 76 秒

  // 分段（音乐提示词按这个写）：energy 是给配乐的能量提示
  const SECTIONS = [
    { id: "hook",    bars: [1, 3],   t: [T(1), T(4)],   name: "开场：提问 → 方块拼出图标（第 3 小节第 1 拍落定，主揭示）", energy: "安静起 → 第 2 小节渐强 → 第 3 小节重拍落下" },
    { id: "create",  bars: [4, 10],  t: [T(4), T(11)],  name: "新建服务器 → 自动部署 → 一个 exe", energy: "稳定律动，中等能量" },
    { id: "run",     bars: [11, 16], t: [T(11), T(17)], name: "启动、控制台 Done、服务器列表、报错横幅", energy: "律动保持，第 11 小节「开起来了」小提升" },
    { id: "friends", bars: [17, 22], t: [T(17), T(23)], name: "整合包导入、内网穿透、手机管理", energy: "能量上扬，第 21–22 小节铺垫" },
    { id: "ai",      bars: [23, 31], t: [T(23), T(32)], name: "可选的 AI：助手、游戏内 AI、伙伴、音乐共享、Key 与额度", energy: "第 23 小节第二次落点（全片能量最高段）" },
    { id: "recap",   bars: [32, 32], t: [T(32), T(33)], name: "回顾：开服 / 排错 / 联机", energy: "收束前最后一次重拍" },
    { id: "outro",   bars: [33, 38], t: [T(33), T(39)], name: "片尾卡（第 34 小节起全部文字到位，停留约 10 秒）", energy: "尾声，渐弱到结束" },
  ];

  // 分段之间的像素擦除转场（落在小节线上）
  const CUTS = [T(4), T(11), T(17), T(23), T(32), T(33)];

  // 字幕：t0–t1 秒；eb 眉标，main 主句，sub 副句，chips 小标签。每行 ≤ 18 字，只用 GB2312 里的字。
  const CAPTIONS = [
    { t0: T(4, 0.5),  t1: T(5, -0.25), eb: "新建服务器", main: "先选核心", sub: "原版、插件、模组，一共 12 种" },
    { t0: T(5, 0.25), t1: T(6, -0.25), eb: "新建服务器", main: "再选游戏版本", sub: "从 1.12.2 到最新版" },
    { t0: T(6, 0.25), t1: T(7, -0.25), eb: "新建服务器", main: "起个名字，点开始部署" },
    { t0: T(7, 0.5),  t1: T(8, 2.75), eb: "自动部署", main: "Java 和服务端自动下载", sub: "优先走国内镜像，下完会校验" },
    { t0: T(11, 0.5), t1: T(12, -0.25), eb: "控制台", main: "点一下启动" },
    { t0: T(12),      t1: T(13, -0.25), eb: "控制台", main: "看到 Done，就开好了", sub: "谁进了服，这里都看得到" },
    { t0: T(13, 0.25), t1: T(14, -0.25), eb: "服务器列表", main: "几台服务器，一个窗口管", sub: "在线人数、运行时长一眼看到" },
    { t0: T(14, 0.5), t1: T(15, -0.25), eb: "报错横幅", main: "开服失败？横幅直接说原因" },
    { t0: T(15),      t1: T(16, -0.25), eb: "报错横幅", main: "常见原因都认得出" },
    { t0: T(16),      t1: T(17, -0.25), eb: "报错横幅", main: "这一步不用 AI", sub: "本地规则分析，还告诉你怎么改" },
    { t0: T(17, 0.5), t1: T(18, -0.25), eb: "整合包", main: "Modrinth 整合包直接导入", sub: "拖进 .mrpack，就能建好服务器" },
    { t0: T(18),      t1: T(19, -0.25), eb: "整合包", main: "核心、版本、加载器自动识别" },
    { t0: T(19, 0.5), t1: T(20, -0.25), eb: "内网穿透", main: "朋友不在同一个网络？", sub: "加一条隧道就能连进来" },
    { t0: T(20),      t1: T(21, -0.25), eb: "内网穿透", main: "复制公网地址，发给朋友", chips: ["OpenFrp", "樱花穿透", "自建 frps"] },
    { t0: T(21, 0.5), t1: T(23, -0.25), eb: "远程管理", main: "同一个局域网，手机也能管", sub: "设好密码，扫码就能打开" },
    { t0: T(24, 2.5), t1: T(25, 1.75), eb: "AI 开服助手", main: "先查日志和配置，再回答" },
    { t0: T(25, 2),   t1: T(26, 1.75), eb: "AI 开服助手", main: "要改东西，先弹卡片问你", sub: "你点了确认才执行" },
    { t0: T(26, 2.5), t1: T(27, 1.75), eb: "游戏内 AI", main: "在聊天里 @AI 提问" },
    { t0: T(27, 2),   t1: T(28, 1.75), eb: "游戏内 AI", main: "勾选的玩家，才能让它执行命令", sub: "而且只能执行白名单里的命令" },
    { t0: T(28, 2.5), t1: T(29, 2.75), eb: "AI 伙伴", main: "会跟随、砍树、打怪的伙伴", sub: "好友只需装一个伙伴模组" },
    { t0: T(29, 3.5), t1: T(31, -0.25), eb: "音乐共享", main: "房主放的歌，全服都能听", sub: "每个人可以单独调小或静音" },
    { t0: T(31, 0.25), t1: T(32, -0.25), eb: "AI 设置", main: "Key 加密存在这台电脑上", sub: "每天用量上限，按 token 算" },
  ];

  // 场景里的其他文字
  const TEXT = {
    hookQ: "想和朋友开个 MC 服务器？",
    word: "MineDeck",
    hookTag: "我的世界 Windows 一键开服工具",
    exeTitle: "一个文件就够了",
    exeFile: "MineDeck.exe",
    exeSize: "大约 13 MB",
    exeRows: ["不用安装，双击运行", "Java 不用自己装", "数据都在 exe 所在的文件夹"],
    causesTitle: "能直接认出的常见原因",
    causes: ["缺前置模组", "客户端模组放进了服务器", "端口被占", "Java 版本不对"],
    mrpack: "机械动力整合包-1.4.mrpack",
    aiTitle: ["AI 是可选的", "填你自己的 API Key 才会启用", "不配置，就不连任何 AI 服务"],
    chatAI: [
      { who: "Alex", text: "@AI 今晚怎么打末影龙" },
      { head: "[AI→Alex] ", text: "先准备床、弓和很多箭、水桶、南瓜头。" },
      { who: "Steve", text: "@AI 把天气改晴" },
      { head: "[AI→Steve] ", text: "天气已经改成晴天了。" },
    ],
    chatCompanion: [
      { who: "Steve", text: "@小方 砍 10 个橡木原木" },
      { head: "[小方→Steve] ", text: "好，这就去砍。" },
    ],
    recap: ["开服", "排错", "联机"],
    endLine: ["免费开源", "Windows 10/11", "v3.0.0"],
    endUrl: "trasperioooooon.github.io/MineDeck",
    endSearch: "GitHub 搜索 MineDeck",
  };

  const out = { BPM, BEAT, BAR, FPS, T, BARS, DURATION, SECTIONS, CUTS, CAPTIONS, TEXT };
  if (typeof module !== "undefined" && module.exports) module.exports = out;
  else root.PROMO = out;
})(this);
