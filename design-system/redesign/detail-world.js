// 服务器详情页 →「世界与备份」的两个子页：备份（world/backups）、换地图（world/map）。11-27 夜里派子代理照临时规范画的第一稿，
// 计划书把这一页归在「主会话照规范先做出大概、作者整体扫一眼」。样式在 detail-world.css，选择器都带 .wd- 前缀。
//
// 程序现在的样子（internal/web/ui/app.js 约 4113 行「view === "backups"」；换地图整块在 worldimport.js 的 render，约 476 行起）。
//
// ---- 备份 ----
// 程序：页顶一句「当前世界：从 … 的备份还原（标签），还原于 …」（或导入的地图、换回过、随交接包带来、没有记录），
//       标签输入框 +「立即备份世界」，「备份放在 <路径> 打开 · 保留最近 10 份（所有服务器共用）到设置里改」，改过存放位置时另一行说旧文件夹，
//       一大段说明（运行中也能备份、超过份数删最旧的、还原时会先停服、还原前自动备份、定时备份在「定时任务」里加），
//       下面一行一份：标签或文件名、「现在的世界」「在原来的文件夹」标记、时间 · 大小 · 文件名，运行中写「会先停服」，「还原」「删除」两个按钮。
// 这里：内容面板里一栏（圆角 18 = 26 − 8），从上到下：
//   · 顶上一行 28 高（同「玩家」各栏）：「备份」和份数「9 / 10」（后面那个数是保留份数，所有服务器共用；说明和到哪改进悬停，一个值只说一次）；
//     接着是「自动备份」开关（36 × 20，在字的左边）和一颗 28 高的胶囊「每天 04:00」（定时备份，点了去「定时任务」）。作者 11-28 定要加这颗开关：
//     自动备份就是「定时任务」里的定时备份，有一条就算开着。开 = 加一条每天 04:00 的（见下面 AUTO），关 = 把定时备份都拿掉、胶囊跟着收起；
//     点了就生效，不进草稿；要拿掉的不只是开关自己加的那一条时先确认。不止一条时胶囊写第一条、后面跟「+1」（同控制台报错横幅两个以上的问题），全部列在悬停里。
//     开关和胶囊靠左放：胶囊出来、收起时开关不挪地方（原来胶囊在右边，开关放它旁边的话一关就会从鼠标底下跑开）。
//     右边是「当前世界」那句（只在现在的世界不是列表里某一份时才出，比如导入的地图、换回过的、随交接包带来的、换地图还没走完的；
//     从列表里某一份还原的，就在那一行标「现在的世界」，不再另说一遍），最右边贴栏角是「打开备份文件夹」（外壳给的 ctx.folders）。
//   · 中间是列表，一行 40 高、圆角 10：时间（等宽）、标签（「定时」「还原前自动」是程序自己打的，淡一点）、标记、大小；「还原」「下载」「删除」悬停那一行才出来
//     （同「玩家」名单里的 ×、向导待装清单）。文件名进时间的悬停。多了在栏里滚（细滚动条）。
//     下载（作者 11-28 定要加，程序现在只能打开文件夹）：窗口版弹系统的「另存为」、选好位置把那份 zip 拷过去，从别的设备打开时就是浏览器的下载。
//     草图里不真存：那颗按钮转一下、换成勾 1.2 秒（同复制），不弹提示条。
//   · 最下面一行：标签输入框（40 高、圆角 10，程序的占位字）+「立即备份世界」（40 高、圆角 10，同联机页樱花那行「连接」的做法）。
//     启动中灰掉（后端这时拒绝备份：「服务器正在启动或停止，请稍后再备份」）。程序那段说明拆进各按钮的悬停。
//   · 还原：危险确认框（共用件页的样子：标题点名是哪一份、按钮写出后果、默认聚焦「取消」）。运行中、启动中按钮写「停止并还原」，带在线人数。
//     照程序修好以后的做法画：只换世界，核心、插件和服务器设置都不动（框里不写「会覆盖设置」）。
//     还原完列表顶上多一份「还原前自动」，「现在的世界」从原来那行挪到这一行（只换这两处标记），超过保留份数时最旧的一份往右划掉。
//   · 删除：危险确认框，同共用件页「删除备份「更新模组前」？」那张。删掉的正好是「现在的世界」那份时，顶上那句跟着出来（程序也是这样：worldFrom 还在）。
//   · 程序里没有的两样（都是作者 11-28 定要加的，要后端补）：下载备份的接口；备份页这颗「自动备份」开关（增删定时备份用现成的 PolicyPatch 就行，
//     「每隔多久」在另一个分支上，这个分支的 inst/policy.go 还只有每天几点）。程序里也没有「停用一条定时任务」，所以关就是删掉，再开回到预设那一条。
//
// ---- 换地图 ----
// 程序：「当前地图 world（level-name）· 服务器运行中」+「导入并替换当前地图」，一段说明，「恢复点」列表（每条：名字、状态标签、一句话、时间 · 来源 · 大小、
//       「恢复导入前的地图」「删除恢复点」，要先停服）；点了导入进流程：选来源（选文件夹 / 填路径 / 上传 ZIP）→ 一个来源里有几张时挑一张 →
//       核对（版本关系、要确认的、要留意的、地图里有什么、会换掉什么、可以一起改的设置、要多少空间、运行中「停服并导入」）→ 确认框 → 任务进度（五步、复制时的经验条、能取消）。
// 这里和新建的「导入地图存档」（import.html?flow=map）用同一套：虚线框、填路径那一行、封面条、左「地图 · 3」挑一张（选中框滑过去）、右「地图里有什么」那张小表、
// 「我已了解」开关（新版本、快照要确认，不开不让往下走，描红、晃一下）。详情页里没有向导浮在窗口底部的上一步、下一步，
// 所以换成面板底下一排 36 高的胶囊（36 的一半 18 = 26 − 8，又是胶囊、又和面板下角同心，同共用件页弹窗底下那排）。
//   · 打开时：左右两栏。左「替换当前地图」：虚线框（点了选压缩包、拖进来）+ 填路径那一行（选文件夹、检测）；标题后面淡淡一个 world（level-name，说明进悬停）。
//     右「恢复点」：一条一块（圆角 10），名字和状态标签、时间 · 来源 · 大小，「恢复导入前的地图」和删除；那句解释进状态标签的悬停。停着才能点，开着灰掉、悬停说原因。
//     贴栏角是「打开世界文件夹」。程序那段说明（原来的世界不会删、留在恢复点、核心插件设置都不动）进虚线框的悬停。
//   · 选了来源：虚线框里上传进度（30 格）或「正在识别…」，认出来以后整页换步到「选择地图」：封面条（来源）+ 两栏 + 底下「上一步」「下一步」。
//   · 「核对」：封面条换成选的那张地图；左栏版本关系（保存于 → 这台服务器，一个标签说一致 / 会升级 / 读不了）和会换掉什么、不动什么、要多少空间、要留意的；
//     右栏「可以一起改的设置」（地图和服务器不一样的几项，开关默认关；都一样时没有这一栏）；
//     底下一条（52 高、圆角 18，同 EULA）：「我已了解：…」和运行中的「停服并导入」；地图比服务器新时这一条换成红字，主键灰掉。
//   · 确认框 → 任务进度：封面条下一栏，左边四步（程序 worldimport importSteps 前四步的原话；它的第五步「完成」作者 11-28 定不要：
//     四步都打勾就是做完了，下面那句「地图已替换完成。」和按钮「完成」留着），右边复制的进度（30 格）和过程记录；复制完以前能取消。
//     做完点「完成」回到打开时那页，恢复点顶上多一条（从下面淡进来）。
//   · 11-28 补画的几种（程序里都有，说法照程序改成书面口吻；各有一个地址参数直接打开，见下面「草图专用的地址参数」）：
//     读取失败：选的来源读不出来（压缩包打不开、文件夹读不了），和「是整合包」一样写在虚线框里，再点、再拖就是换一个来源。
//     任务出错：换的那一步出错、已经自动退回原存档。那一步的图标换成 ×，下面一句红字说结果、一句淡字说怎么办（后面一颗复制报错信息），原话在过程记录最后一行；
//       底下同取消以后那排：「重新核对地图」「返回换地图」。回去以后恢复点多一条「没有生效」，当时的原因在标签的悬停里。存档没变，备份页那句「当前存档」不动。
//     警告卡（程序 busyHTML）：这台服务器的存档正被占着、或者换地图没走完时，左栏的虚线框换成这块（实线、警告色），导入用不了，填路径那行和恢复点的按钮都灰掉。
//       两种：等它做完的（转圈，没有按钮）；自动恢复没成功、要人处理的（写明文件都在、服务器已暂停启动，列出要看的位置，一颗「重试恢复」，要先停服）。
//       重试成功以后那块收起、虚线框回来，那条恢复点变成「没有生效」。备份页顶上那句跟着写「换地图尚未完成」。
//     从别的设备打开（局域网里用浏览器）：只能传 ZIP。填路径那行灰掉、悬停说原因（程序是藏掉，这里照规范「不藏起来」）；「打开 X 文件夹」也灰掉。
//     随交接包带来的：不是换地图页的状态（恢复点在 .minedeck 里，不进交接包），是备份页顶上那句的一种：
//       这台服务器是导入交接包得来的、存档在原来那台电脑上从备份还原过，那份备份不在这台电脑上，所以只在顶上说，列表里没有哪一行标「当前存档」。
//
// ---- 草图专用的地址参数（都只在头一次建这台服务器的数据时套用，换子页回来不会又重置；remote 例外，随时读） ----
//   map=unreadable 读取失败 · map=fail 任务出错（这台服务器之后每次导入都在「换上新地图」那一步出错）· map=busy 正在换（等它做完）· map=stuck 要人处理（重试恢复）
//   remote=1 从别的设备打开 · from=carried 随交接包带来的存档（看 sub=backups）· sched=multi 两条定时备份（看 sub=backups）
//   填路径时带「损坏」「broken」的也会读取失败（同原来带「整合包」「mrpack」的会认成整合包）。
// 示例数据：Paper、原版用 import.html 那个 SkyCity_v2.1.zip（三张，第三张只有 level.dat_old 不能导入）；地图存于 1.20.4，比 Paper 1.21.4、原版 26.2 旧，
// 所以要「我已了解」。Forge 那台 1.20.1 换成一个朋友的机械动力存档：第一张 1.20.1 能直接换，第二张 1.20.4 比服务器新、核对时拦下。都是编的。
// 原版那台以前导入过 SkyCity（有一条能换回的恢复点），Forge 那台换过一次又换回了，Paper 那台没换过：备份页顶上那句「当前世界」跟着这里的记录走。
// 草图里换不了页头的状态（外壳没给这个接口）：「停止并还原」「停服并导入」做完以后页头还是运行中，改程序时照真的走。
(() => {
  window.DETAIL_TABS = window.DETAIL_TABS || {};
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  const $ = (s, r = document) => r.querySelector(s), $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const EASE_IN = "cubic-bezier(.4, 0, 1, 1)", EASE_OUT = "cubic-bezier(.2, .7, .2, 1)", EASE_SLIDE = "cubic-bezier(.3, .7, .2, 1)";
  const m = s => `<span class="mono">${esc(s)}</span>`;
  const hy = el => { window.KIT.hydrate(el); return el; };
  const icon = (n, cls) => window.KIT.icon(n, cls);
  const pad = n => String(n).padStart(2, "0");
  const size = mb => mb >= 1024 ? (mb / 1024).toFixed(mb >= 10240 ? 0 : 1) + " GB" : mb.toFixed(1) + " MB";
  let C = null; // 外壳最近一次给的 ctx

  // ================= 数据（每台服务器一份；换标签回来还在） =================
  const KEEP = 10; // 程序默认保留份数（app.DefaultBackupKeep），所有服务器共用
  // 备份文件名：时间（20060102-150405）加可选的「-标签」，同 inst/backup.go 的 parseBackupName
  const parse = f => { const x = f.match(/^(\d{4})(\d\d)(\d\d)-(\d\d)(\d\d)(\d\d)(?:\.\d+)?(?:-(.+))?\.zip$/); return x ? { time: `${x[1]}-${x[2]}-${x[3]} ${x[4]}:${x[5]}`, label: x[7] || "" } : { time: "", label: "" }; };
  const bk = (file, mb, old) => ({ file, mb, ...parse(file), old: old || "" }); // old：改存放位置以前的旧文件夹（程序的 elsewhere + dir）
  // 「自动备份」开关打开时加的那一条定时备份：每天 04:00（主会话 11-28 改的：子代理先照「定时任务」的预设「每小时存档」画，
  // 但保留份数和手动备份共用，每小时一份 10 个小时就把手动的挤掉；每天一份是程序原来就有的定时备份，也同示例里 Paper 那条）
  const AUTO = { at: "04:00" };
  const SEED = {
    // scheds：这台服务器的定时备份（「定时任务」里类型是备份的那几条：at 每天几点，every 每隔几分钟）。Paper：天天 04:00 定时备份；11-22 从「开荒第一周」还原过一次（那时自动做了一份「还原前自动」）；最旧那份还在改存放位置以前的文件夹里
    paper: { dir: "D:\\MineDeck\\backups\\生存服 · 朋友", scheds: [{ at: "04:00" }], worldMb: 823, worlds: ["world", "world_nether", "world_the_end"],
      props: { difficulty: "easy", gamemode: "survival" },
      backups: [bk("20261127-040000-定时.zip", 812.4), bk("20261126-213012-更新插件前.zip", 806.0), bk("20261126-040000-定时.zip", 798.7),
        bk("20261125-040000-定时.zip", 791.2), bk("20261124-193544.zip", 785.9), bk("20261124-040000-定时.zip", 780.3),
        bk("20261122-150210-还原前自动.zip", 771.8), bk("20261120-221903-开荒第一周.zip", 640.1), bk("20261115-120400-刚建服.zip", 212.5, "E:\\旧备份\\生存服 · 朋友")],
      wf: { backup: "20261120-221903-开荒第一周.zip", at: "2026-11-22 15:02" }, points: [] },
    // Forge：没有定时备份；11-23 换过一张测试图，又换回来了（恢复点里留着那张导入的地图）
    forge: { dir: "D:\\MineDeck\\backups\\机械动力整合包", scheds: [], worldMb: 2410, worlds: ["world"], props: { difficulty: "easy", gamemode: "survival" },
      backups: [bk("20261126-224012-更新模组前.zip", 2366.4), bk("20261125-193000.zip", 2291.7), bk("20261123-201533-火车站建好.zip", 2150.2), bk("20261121-180210.zip", 1662.9)],
      wf: null, points: [{ id: "f1", name: "机械动力 · 测试图", state: "restored", created: "2026-11-23 18:40", source: "create_test_world.zip", keptMb: 402.6, oldMb: 0, hasOld: true }] },
    // 原版：11-20 用 SkyCity 换了地图（导入前的世界留在恢复点里，能换回）
    vanilla: { dir: "D:\\MineDeck\\backups\\原版快照测试", scheds: [], worldMb: 1204, worlds: ["world"], props: { difficulty: "easy", gamemode: "survival" },
      backups: [bk("20261126-220130.zip", 1190.6), bk("20261120-201005-换地图前.zip", 88.7)],
      wf: null, points: [{ id: "v1", name: "天空之城 SkyCity", state: "committed", created: "2026-11-20 20:14", source: "SkyCity_v2.1.zip", oldMb: 88.4, hasOld: true, canReturn: true }] },
  };
  // 换地图的来源（字段照 /api/world/scan 的候选，同 import.html 的 MAPSRC）
  const README = "天空之城 SkyCity v2.1\n作者：steve　测试：alex\n\n建议难度：普通　游戏模式：冒险\n建议人数：1～4 人\n\n【玩法】\n从出生点的告示牌开始，一路往上爬到城顶的灯塔。\n一共五层，每层有一个钥匙，拿齐五把才能开灯塔的门。\n掉下去会回到这一层的存档点，不扣东西。\n\n【注意】\n地图自带数据包 skycity_core，别删，删了机关不动。\n不要改游戏规则 commandBlockOutput，会刷屏。\n多人玩时建议关掉 PVP。\n\n【资源包】\n资源包 resources.zip 放在世界文件夹里，进服前自己装上。\n不装也能玩，只是钥匙和门的样子是原版的。\n\n【更新记录】\nv2.1　修了第三层的门卡住\nv2.0　加了第五层\nv1.0　第一版\n\n更新记录、反馈：https://example.com/skycity\n请勿二次发布。";
  const SKY = { file: "SkyCity_v2.1.zip", mb: 1331, path: "D:\\Maps\\SkyCity", cands: [
    { name: "天空之城 SkyCity", root: "SkyCity_v2.1/SkyCity", ver: "1.20.4", mb: 1126, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 186], ["下界", 12], ["末地", 4]], missing: [], dps: ["skycity_core"], rp: true, players: 0, readme: "README.txt", hints: ["安装说明.png"],
      diff: "normal", mode: "adventure", clues: [], notes: [] },
    { name: "天空之城（建造存档）", root: "SkyCity_v2.1/SkyCity_build", ver: "1.20.4", mb: 214, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 40]], missing: ["下界", "末地"], dps: [], rp: false, players: 1, readme: "", hints: [], diff: "peaceful", mode: "creative", clues: [],
      notes: [["warn", "单人存档：原玩家背包与经验能否衔接取决于登录账号", "此为单人存档，地形、建筑与容器内容均完整保留；原玩家的背包与经验能否衔接取决于进入服务器使用的账号（开启正版验证并使用同一正版账号通常可衔接）。MineDeck 不会修改玩家数据文件"]] },
    { name: "SkyCity", root: "SkyCity_v2.1/old/SkyCity", ver: "", mb: 1024, block: "只有备份用的 level.dat_old",
      blockFull: "该存档仅含备份文件 level.dat_old，缺少 level.dat。请先将 level.dat_old 复制并重命名为 level.dat，确认可在单人游戏中打开后再导入" },
  ] };
  const CREATE = { file: "朋友的机械动力存档.zip", mb: 2087, path: "D:\\Maps\\create_s2", cands: [
    { name: "机械动力生存 · 第二季", root: "create_s2/world", ver: "1.20.1", mb: 1843, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 412], ["下界", 38], ["末地", 9]], missing: [], dps: [], rp: false, players: 4, readme: "", hints: ["mods.txt"],
      diff: "normal", mode: "survival", clues: ["forge", "create", "createaddition", "railways"],
      notes: [["info", "该地图曾使用模组：缺少对应模组时相关方块与物品将消失", "检测到 Create、Create Crafts & Additions、Steam 'n' Rails 线索。若服务器未安装对应模组，相关方块与物品将在首次加载时被清除；当前服务器已全部安装"]] },
    { name: "测试世界", root: "create_s2/test", ver: "1.20.4", mb: 96, layout: "旧版布局", layoutFull: "旧版布局（26.1 以前的原版）",
      dims: [["主世界", 6]], missing: ["下界", "末地"], dps: [], rp: false, players: 1, readme: "", hints: [], diff: "peaceful", mode: "creative", clues: [], notes: [] },
  ] };
  const W = {};
  const dat = () => {
    const k = C.kind;
    if (!W[k]) {
      const s = SEED[k] || { dir: "", scheds: [], worldMb: 0, worlds: ["world"], props: { difficulty: "easy", gamemode: "survival" }, backups: [], wf: null, points: [] };
      W[k] = JSON.parse(JSON.stringify(s));
      W[k].level = "world"; W[k].src = k === "forge" ? CREATE : SKY;
      W[k].flow = freshFlow();
      seedFromQS(W[k]);
    }
    return W[k];
  };
  // err：虚线框里写的是哪一种读不了（SRC_ERR 的键）
  const freshFlow = () => ({ screen: "home", phase: "empty", err: "", pct: 0, path: "", fromPath: false, fileName: "", cand: 0, ack: false, stop: false, sets: {}, readme: false, task: null, tok: 0 });
  // 草图专用的地址参数（见文件开头）：只在头一次建这台服务器的数据时套用。外壳换子页时地址里的参数还在，放在 html() 里的话换回来又被重置
  function seedFromQS(D) {
    const F = D.flow, map = C.QS.get("map"), cd = D.src.cands[0];
    if (C.QS.get("sched") === "multi") D.scheds = [{ at: "04:00" }, { every: 60 }]; // 每天 04:00 加一条在「定时任务」里另配的每小时一份
    // 随交接包带来的（inst.WorldFrom 的 Carried）：还原是在原来那台电脑上做的，那份备份不在这台的列表里
    if (C.QS.get("from") === "carried") D.wf = { backup: "20261114-221500-交接前.zip", at: "2026-11-14 22:16", carried: true };
    if (map === "unreadable") Object.assign(F, { phase: "error", err: "zip", fileName: D.src.file });
    if (map === "fail") { D.fail = true; Object.assign(F, { screen: "task", fileName: D.src.file }); F.task = failedTask(); }
    // hold：这台服务器的存档现在动不了（程序 /world 的 busy、blocked）。busy 等它做完；stuck 换地图没走完、自动恢复也没成功，要人处理
    if (map === "busy" || map === "stuck") {
      D.hold = map;
      if (map === "stuck") D.wf = null; // 换到一半、没退回去：现在的存档说不清还是不是那份备份了，程序放掉占位时会清掉 worldFrom（inst/worldintent.go 的 Release）
      D.points.unshift({ id: "h1", name: cd.name, state: map === "busy" ? "committing" : "stuck", created: "2026-11-27 20:12", source: D.src.file, oldMb: 0, hasOld: false,
        error: map === "stuck" ? failCause() : "" });
    }
  }
  const remote = () => C.QS.get("remote") === "1"; // 从别的设备打开（程序 /api/app 的 local 为假）：填不了本机路径、打不开本机文件夹

  // ================= 共用的小件 =================
  // 只换变了的那几样：新的 HTML 里带 data-wd-k 的，和旧的一样就不动；不一样的旧的 0.09 秒往反方向挪 5 像素淡掉，新的 0.22 秒挪进来，错开 22 毫秒（规范第七节）
  function swapKeyed(box, html, dir = 1, axis = "Y") {
    if (!box) return;
    const tmp = document.createElement("div"); tmp.innerHTML = html; hy(tmp);
    const tf = v => axis === "X" ? `translateX(${v}px)` : `translateY(${v}px)`;
    let i = 0;
    $$("[data-wd-k]", tmp).forEach(n => {
      const o = box.querySelector(`[data-wd-k="${n.dataset.wdK}"]`); if (!o) return;
      const same = o.innerHTML === n.innerHTML && o.className === n.className && (o.getAttribute("title") || o.dataset.tip || "") === (n.getAttribute("title") || "");
      if (same) return;
      const tok = o._tok = (o._tok || 0) + 1, d = i++ * 22;
      o.getAnimations().forEach(a => a.cancel());
      o.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, delay: d, easing: EASE_IN, fill: "forwards" }).onfinish = () => {
        if (o._tok !== tok) return;
        o.innerHTML = n.innerHTML; o.className = n.className; delete o.dataset.tip;
        if (n.hasAttribute("title")) o.setAttribute("title", n.getAttribute("title")); else o.removeAttribute("title");
        if (o.classList.contains("wd-ps")) fitWdEl(o);
        o.getAnimations().forEach(a => a.cancel());
        o.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT, fill: "backwards" });
      };
    });
  }
  // 一块里的几样整批换（虚线框里的图标、一句、小字）：旧的一起淡掉，新的一样一样淡进来
  function swapItems(box, html, dir = 1, axis = "Y") {
    if (!box) return;
    const tf = v => axis === "X" ? `translateX(${v}px)` : `translateY(${v}px)`;
    const tok = box._tok = (box._tok || 0) + 1, olds = [...box.children];
    olds.forEach(x => { x.getAnimations().forEach(a => a.cancel()); x.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: tf(-5 * dir) }], { duration: 90, easing: EASE_IN, fill: "forwards" }); });
    setTimeout(() => {
      if (box._tok !== tok) return;
      box.innerHTML = html; hy(box);
      [...box.children].forEach((x, i) => x.animate([{ opacity: 0, transform: tf(5 * dir) }, { opacity: 1, transform: "none" }], { duration: 220, delay: i * 22, easing: EASE_OUT, fill: "backwards" }));
    }, olds.length ? 90 : 0);
  }
  // 新加的一行：高度从 0 长出来、从下面 5 像素淡进来（清单加减，规范第七节）
  function growIn(el, delay = 0) {
    const h = el.offsetHeight + "px";
    el.animate([{ opacity: 0, transform: "translateY(5px)", height: "0px", marginBottom: "-2px" }, { opacity: 0, transform: "translateY(5px)", height: h, marginBottom: "0px", offset: .45 },
      { opacity: 1, transform: "none", height: h, marginBottom: "0px" }], { duration: 360, delay, easing: EASE_OUT, fill: "backwards" });
  }
  // 拿掉一行：往右划 32 像素淡掉，再收起（同「玩家」名单、向导待装清单）
  function dropOut(el, done) {
    el.classList.add("wd-out");
    const h = el.offsetHeight + "px";
    el.animate([
      { opacity: 1, transform: "none", height: h, marginBottom: "0px", easing: EASE_IN },
      { opacity: 0, transform: "translateX(32px)", height: h, marginBottom: "0px", offset: .55, easing: EASE_OUT },
      { opacity: 0, transform: "translateX(32px)", height: "0px", marginBottom: "-2px" },
    ], { duration: 320, fill: "forwards" }).onfinish = () => { el.remove(); done?.(); };
  }
  const shake = el => el?.animate([{ transform: "none" }, { transform: "translateX(-6px)" }, { transform: "translateX(5px)" }, { transform: "translateX(-3px)" }, { transform: "none" }], { duration: 360, easing: EASE_SLIDE });
  const folders = () => {
    const D = dat();
    const off = remote() ? ` aria-disabled="true" title="仅可在运行 MineDeck 的设备上打开文件夹"` : ""; // 程序在别的设备上不给这颗按钮；这里灰掉、悬停说原因
    return (C.folders || []).length ? `<div class="wd-fd">${C.folders.map(([i, t]) => `<button type="button"${off || ` title="${esc(t + (C.sub === "backups" && D.dir ? "\n" + D.dir : ""))}"`}><i data-ic="${i}"></i></button>`).join("")}</div>` : "";
  };
  // 定时任务一条什么时候执行（同 detail-tasks.js 的 whenText）
  const whenText = s => s.every ? `每隔 ${s.every % 60 ? s.every + " 分钟" : s.every / 60 + " 小时"}` : `每天 ${s.at}`;
  // 复制、下载这类做完不弹提示条的：那颗图标按钮自己换成勾 1.2 秒（规范第七节），再换回来
  const setIb = (btn, ic, tip, cls) => { btn.innerHTML = icon(ic, cls); btn.title = tip; delete btn.dataset.tip; };
  function flashDone(btn, ic, done, tip) {
    setIb(btn, "check", done); btn.classList.add("ok");
    setTimeout(() => { if (btn.isConnected) { setIb(btn, ic, tip); btn.classList.remove("ok"); } }, 1200);
  }
  const live = () => C.st !== "stopped";
  const addonNoun = () => ["paper", "purpur", "leaves", "folia", "velocity", "waterfall"].includes(C.S.core) ? "插件" : ["mohist", "banner"].includes(C.S.core) ? "插件和模组" : C.S.core === "vanilla" ? "" : "模组";
  const addonList = () => addonNoun().replace("和", "、");
  const keptWhat = () => `核心${addonList() ? "、" + addonList() : ""}和设置保持不变`;
  const bkLabelPh = () => { const a = addonNoun(); return `备份标签（选填，如：${a === "插件" ? "更新插件前" : a === "模组" ? "更新模组前" : a ? "更新插件或模组前" : "更新版本前"}）`; };
  const fitWdEl = el => { if (el.scrollWidth > el.clientWidth + 1) { el.title = el.textContent; delete el.dataset.tip; } else { el.removeAttribute("title"); delete el.dataset.tip; } };
  const fitWdTitles = r => $$(".wd-ps", r).forEach(fitWdEl);

  // ---- 确认框：同共用件页（commons.html）的危险确认框。挂在 body 上，换标签时收掉 ----
  let mask = null;
  function ask({ icon: ic, title, body, ok, onOk, w = 380 }) {
    closeAsk(true);
    const mk = mask = Object.assign(document.createElement("div"), { className: "wd-mask enter" });
    mk.innerHTML = `<div class="wd-md" role="dialog" aria-modal="true" style="--w:${w}px"><div class="wd-mc"><div class="wd-mh"><span class="wd-mi"><i data-ic="${ic}"></i></span><h3>${title}</h3></div>`
      + `<div class="wd-mb">${body}</div></div><div class="wd-ma"><button type="button" class="wd-b fk" data-wd-x="no">取消</button><button type="button" class="wd-b dgr" data-wd-x="ok">${ok}</button></div></div>`;
    document.body.appendChild(hy(mk));
    const no = $("[data-wd-x=no]", mk); no.focus({ focusVisible: false });
    // 默认落在「取消」：鼠标点开时浏览器不画焦点框，回车却照样按到它，所以画一圈细框，一动别的就收（同共用件页）
    mk.addEventListener("pointerdown", e => { if (!e.target.closest(".fk")) no.classList.remove("fk"); });
    mk.addEventListener("keydown", e => { if (e.key === "Tab") no.classList.remove("fk"); });
    mk.addEventListener("click", e => {
      if (mask !== mk) return; // 已经关了（正在淡出时又点了一下）
      if (e.target.closest("[data-wd-x=ok]")) { closeAsk(); onOk(); return; }
      if (e.target.closest("[data-wd-x=no]") || e.target === mk) closeAsk();
    });
  }
  function closeAsk(now) {
    if (!mask) return; const mk = mask; mask = null;
    if (now) { mk.remove(); return; }
    mk.classList.remove("enter"); mk.classList.add("leave"); setTimeout(() => mk.remove(), 140);
  }
  document.addEventListener("keydown", e => { if (e.key === "Escape" && mask) closeAsk(); });

  // ================= 备份 =================
  const bkRoot = () => document.querySelector(".cp > .wd-bk:not(.fade-out)");
  const isSys = l => l === "定时" || l === "还原前自动";
  const bkName = b => b.label || b.time;
  // 现在的世界从哪来（app.js 约 4134 行 nowText、worldimport.js 的 origin）：从列表里某一份还原的，就在那一行标「现在的世界」，这里不出
  function nowLine() {
    const D = dat(), wf = D.wf;
    if (wf) {
      const p = parse(wf.backup);
      // 随交接包带来的（app.js 约 4138 行，A52）：还原是在原来那台电脑上做的，那份备份不在这台电脑上，照实说，不让人去列表里找。这一句长，标签进悬停
      if (wf.carried) return `<button type="button" class="wd-now" title="${esc(`在导出交接包的设备上从该备份还原${p.label ? `（标签「${p.label}」）` : ""}；该备份不在此设备的备份列表中`)}"><i data-ic="map"></i><span>当前存档：随交接包带入，来自原设备 ${esc(p.time)} 的备份</span></button>`;
      if (D.backups.some(b => b.file === wf.backup)) return "";
      return `<button type="button" class="wd-now" title="${esc(`还原于 ${wf.at}；该备份已不在列表中`)}"><i data-ic="map"></i><span>当前存档：从 ${esc(p.time)} 的备份还原${p.label ? `（${esc(p.label)}）` : ""}</span></button>`;
    }
    const og = origin(D);
    if (!og) return "";
    const [t, at] = og.kind === "busy" ? ["当前存档：换地图尚未完成", ""] : og.kind === "import" ? [`当前存档：从「${esc(og.source)}」导入的地图`, `导入于 ${og.at}；`] : ["当前存档：已换回导入地图前的存档", `换回于 ${og.at}；`];
    return `<button type="button" class="wd-now" data-wd-act="tomap" title="${esc(`${at}点击前往「换地图」查看`)}"><i data-ic="map"></i><span>${t}</span></button>`;
  }
  function origin(D) {
    if (D.hold) return { kind: "busy" }; // 正在换、要人处理的不管排在哪都先说（worldimport.js origin）
    for (const p of D.points) {
      if (p.state === "committed") return { kind: "import", source: p.source, at: p.created };
      if (p.state === "restored") return { kind: "returned", at: p.returned || p.created };
    }
    return null;
  }
  const keepTip = () => `保留最近 ${KEEP} 份，超出后自动删除最旧的备份。份数上限所有服务器共用，可在「设置 → 存储位置」中修改`;
  const countHTML = () => `${dat().backups.length}<small> / ${KEEP}</small>`;
  // 「自动备份」开关：有定时备份就是开着。开 = 照预设加一条，关 = 定时备份都拿掉（程序没有「停用」，只能增删）；说明进悬停
  const autoTip = n => n ? `关闭后移除${n > 1 ? `全部 ${n} 条` : ""}定时备份，已有的备份不受影响`
    : `开启后${whenText(AUTO)}自动备份一次存档，服务器未运行时不执行；时刻可在「定时任务」中修改。每台服务器保留最近 ${KEEP} 份（含手动备份），超出时自动清理最旧的备份`;
  function autoSw() {
    const n = dat().scheds.length;
    return `<button type="button" class="wd-tg${n ? " on" : ""}" data-wd-act="auto" role="switch" aria-checked="${n > 0}" title="${esc(autoTip(n))}"><span class="wd-sw"></span><span class="t">自动备份</span></button>`;
  }
  // 定时备份的胶囊：写第一条什么时候执行，不止一条时后面跟「+1」，全部列在悬停里；点了去「定时任务」。没有定时备份时不出
  function schedBtn() {
    const L = dat().scheds; if (!L.length) return "";
    const tip = (L.length > 1 ? `${L.length} 条定时备份：${L.map(whenText).join("、")}` : `${whenText(L[0])} 自动备份`) + "（标签「定时」）；可在「定时任务」中修改";
    return `<button type="button" class="wd-chip" data-wd-act="sched" title="${esc(tip)}"><i data-ic="clock"></i>${whenText(L[0])}${L.length > 1 ? `<small>+${L.length - 1}</small>` : ""}</button>`;
  }
  const restoreTip = () => live() ? `将先保存存档并停止服务器，再执行还原；仅替换存档，${keptWhat()}` : `仅替换存档，${keptWhat()}`;
  const dlTip = () => remote() ? "下载此备份（.zip）" : "下载此备份：另存为，选择位置后保存一份 .zip 副本";
  function tagsHTML(b) {
    const D = dat(), cur = D.wf && !D.wf.carried && D.wf.backup === b.file;
    return (cur ? `<span class="wd-tag acc" title="${esc(`于 ${D.wf.at} 从此备份还原`)}">当前存档</span>` : "")
      + (b.old ? `<button type="button" class="wd-tag" data-wd-act="old" title="${esc(`更改备份位置前创建，位于原文件夹，可正常还原或删除；点击打开文件夹\n${b.old}`)}">位于原文件夹</button>` : "");
  }
  function bkRow(b) {
    return `<div class="wd-row" data-wd-file="${esc(b.file)}"><span class="wd-time" title="${esc(b.file)}">${esc(b.time)}</span>`
      + `<span class="wd-lab${isSys(b.label) ? " sys" : ""}">${esc(b.label)}</span><span class="wd-tags">${tagsHTML(b)}</span><span class="wd-sp"></span>`
      + `<button type="button" class="wd-btn wd-act" data-wd-act="restore" title="${esc(restoreTip())}"><i data-ic="restore"></i>还原</button>`
      + `<button type="button" class="wd-ib wd-act" data-wd-act="dl" title="${esc(dlTip())}"><i data-ic="download"></i></button>`
      + `<button type="button" class="wd-ib wd-act" data-wd-act="del" title="删除"><i data-ic="trash"></i></button>`
      + `<span class="wd-size">${size(b.mb)}</span></div>`;
  }
  function makeBtn(busy) {
    const starting = C.st === "starting";
    const tip = starting ? "服务器正在启动或停止，请稍后再备份" : C.st === "running" ? "服务器运行中也可备份：先暂停自动保存，打包存档后再恢复自动保存" : "";
    return busy ? `<button type="button" class="wd-go" data-wd-act="make" aria-disabled="true"><i data-ic="spinner" class="wd-spin"></i>正在备份</button>`
      : `<button type="button" class="wd-go" data-wd-act="make"${starting ? ' aria-disabled="true"' : ""}${tip ? ` title="${esc(tip)}"` : ""}><i data-ic="camera"></i>立即备份存档</button>`;
  }
  function bkHTML() {
    const D = dat();
    return `<div class="wd-bk"><section class="wd-col">${folders()}`
      + `<div class="wd-hd fdpad"><span class="wd-t">备份</span><span class="wd-n" data-wd-k="n" title="${esc(keepTip())}">${countHTML()}</span>`
      + `<span class="wd-auto">${autoSw()}<span class="wd-chipslot">${schedBtn()}</span></span><span class="wd-sp"></span><span class="wd-nowslot" data-wd-k="now">${nowLine()}</span></div>`
      + `<div class="wd-lw${D.backups.length ? "" : " is-none"}"><div class="list">${D.backups.map(bkRow).join("")}</div><span class="thumb"></span><div class="wd-none">暂无备份</div></div>`
      + `<div class="wd-mk"><input class="wd-inp" maxlength="20" placeholder="${esc(bkLabelPh())}" aria-label="备份标签（选填）" autocomplete="off" spellcheck="false">`
      + `<span class="wd-goslot">${makeBtn(false)}</span></div></section></div>`;
  }
  // 只换变了的那几处
  const bkList = () => bkRoot()?.querySelector(".wd-lw .list");
  const rowOf = file => bkList() && [...bkList().querySelectorAll(".wd-row:not(.wd-out)")].find(r => r.dataset.wdFile === file);
  function paintHead(dir = 1) {
    const r = bkRoot(); if (!r) return;
    swapKeyed($(".wd-hd", r), `<span class="wd-n" data-wd-k="n" title="${esc(keepTip())}">${countHTML()}</span><span class="wd-nowslot" data-wd-k="now">${nowLine()}</span>`, dir);
    r.querySelector(".wd-lw").classList.toggle("is-none", !dat().backups.length);
  }
  function paintTags(file) {
    const row = rowOf(file), b = dat().backups.find(x => x.file === file); if (!row || !b) return;
    const box = $(".wd-tags", row), html = tagsHTML(b);
    if (box.innerHTML === hy(Object.assign(document.createElement("span"), { innerHTML: html })).innerHTML) return;
    swapItems(box, html, 1);
  }
  function addBackup(b, spare) {
    const D = dat(); D.backups.unshift(b);
    const list = bkList();
    if (list) {
      list.insertAdjacentHTML("afterbegin", bkRow(b)); const row = hy(list.firstElementChild);
      growIn(row); list.scrollTo({ top: 0, behavior: "smooth" });
    }
    trim(spare); paintHead(1); bkRoot()?.querySelector(".wd-lw")._paint?.();
  }
  // 超过保留份数：最旧的往右划掉（程序在做完一份以后按时间删最旧的，「还原前自动」也算数；
  // 还原时正要用的那一份不删，同 inst/backup.go createBackup 的 spare）
  function trim(spare) {
    const D = dat();
    while (D.backups.length > KEEP) {
      let at = D.backups.length - 1; if (D.backups[at].file === spare) at--;
      const [b] = D.backups.splice(at, 1), row = rowOf(b.file);
      if (row) setTimeout(() => dropOut(row, () => bkRoot()?.querySelector(".wd-lw")._paint?.()), 380);
    }
  }
  // 草图里的「现在」：示例数据最新的一份是 11-27 04:00，所以从 11-27 20:30 起走（照真的流逝），不拿这台电脑的钟（早上看草图时新备份会排到 04:00 那份后面）
  const T0 = Date.now(), NOW0 = new Date(2026, 10, 27, 20, 30, 0).getTime();
  const stamp = () => { const d = new Date(NOW0 + Date.now() - T0); return { f: `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`,
    t: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}` }; };
  let making = false;
  function makeBackup() {
    const r = bkRoot(); if (!r || making || C.st === "starting") return;
    const inp = $(".wd-inp", r), label = inp.value.trim().replace(/[\\/:*?"<>|]/g, "");
    making = true; inp.disabled = true;
    const slot = $(".wd-goslot", r); slot.innerHTML = makeBtn(true); hy(slot);
    setTimeout(() => {
      making = false;
      const s = stamp(), D = dat();
      addBackup({ file: `${s.f}${label ? "-" + label : ""}.zip`, mb: D.worldMb + Math.round(Math.random() * 40) / 10, time: s.t, label, old: "" });
      const rr = bkRoot(); if (!rr) return;
      const i2 = $(".wd-inp", rr); i2.disabled = false; i2.value = "";
      const sl = $(".wd-goslot", rr); sl.innerHTML = makeBtn(false); hy(sl);
    }, 1200);
  }
  function askRestore(b) {
    const D = dat(), lv = live(), n = lv ? (C.online || []).length : 0, nm = bkName(b);
    ask({ icon: "restore", title: `还原备份<span class="nw">「${esc(nm)}」？</span>`, ok: lv ? "停止并还原" : "还原存档", w: 400,
      body: `<span class="mono" title="${esc(b.file)}">${b.label ? esc(b.time) + " · " : ""}${size(b.mb)}</span><br>`
        + (C.st === "starting" ? `服务器正在启动，将先停止再还原；还原后不会自动启动。<br>`
          : lv ? `服务器正在运行${n ? `，当前有 <b>${n}</b> 位玩家在线，停止服务器后将断开连接` : ""}：将先保存存档并停止服务器，还原后不会自动启动。<br>` : "")
        + `仅替换存档；还原前会自动备份当前存档，需要时可再次还原。`,
      onOk: () => doRestore(b) });
  }
  function doRestore(b) {
    const row = rowOf(b.file);
    if (row) { row.classList.add("busy"); const btn = $("[data-wd-act=restore]", row); btn.innerHTML = `${icon("spinner", "wd-spin")}正在还原`; btn.setAttribute("aria-disabled", "true"); }
    setTimeout(() => {
      const D = dat(), s = stamp(), prev = D.wf && D.wf.backup;
      // 还原前自动备份现在的世界（inst/backup.go RestoreBackup），再把世界换成这一份、记下 worldFrom
      addBackup({ file: `${s.f}-还原前自动.zip`, mb: D.worldMb, time: s.t, label: "还原前自动", old: "" }, b.file);
      D.wf = { backup: b.file, at: `${s.t}` }; D.worldMb = b.mb;
      if (prev) paintTags(prev);
      paintTags(b.file); paintHead(1);
      const rw = rowOf(b.file);
      if (rw) { rw.classList.remove("busy"); const btn = $("[data-wd-act=restore]", rw); btn.innerHTML = `${icon("restore")}还原`; btn.removeAttribute("aria-disabled"); }
    }, 1400);
  }
  function askDelete(b) {
    ask({ icon: "trash", title: `删除备份<span class="nw">「${esc(bkName(b))}」？</span>`, ok: "删除备份",
      body: `<span class="mono" title="${esc(b.file)}">${b.label ? esc(b.time) + " · " : ""}${size(b.mb)}</span><br>删除后<b class="dg">无法恢复</b>。`,
      onOk: () => {
        const D = dat(); D.backups = D.backups.filter(x => x.file !== b.file);
        const row = rowOf(b.file); if (row) dropOut(row, () => bkRoot()?.querySelector(".wd-lw")._paint?.());
        paintHead(1);
      } });
  }
  // 开关自己滑过去（没变的不动），胶囊从开关那一侧淡进来、关的时候朝开关那一侧淡掉
  // 关掉时要拿掉的不只是开关自己加的那一条（不止一条，或是在「定时任务」里另配的）就先确认，免得一下删掉别处配好的任务（主会话 11-28 加的）
  async function toggleAuto() {
    const D = dat(), on = !D.scheds.length;
    if (!on && (D.scheds.length > 1 || whenText(D.scheds[0]) !== whenText(AUTO))) {
      const L = D.scheds.map(whenText).join("、");
      const k = await KIT.confirm({ ic: "clock", tone: "dgr", title: "关闭自动备份", body: `将移除「定时任务」中的 ${D.scheds.length} 条定时备份（${L}），已有的备份不受影响。`,
        btns: [{ k: "cancel", label: "取消" }, { k: "ok", label: "关闭", tone: "dgr" }] });
      if (k !== "ok" || !D.scheds.length) return;
    }
    const r = bkRoot(); if (!r) return;
    D.scheds = on ? [{ ...AUTO }] : [];
    const sw = $("[data-wd-act=auto]", r);
    sw.classList.toggle("on", on); sw.setAttribute("aria-checked", on); sw.title = autoTip(D.scheds.length); delete sw.dataset.tip;
    swapItems($(".wd-chipslot", r), schedBtn(), on ? -1 : 1, "X");
  }
  // 草图里不真存：转 0.9 秒当作在拷那份 zip，再换成勾。这期间那颗按钮留在行上（不用悬停着）
  function download(row, btn) {
    if (row.classList.contains("dl")) return;
    row.classList.add("dl"); setIb(btn, "spinner", "正在保存", "wd-spin");
    setTimeout(() => { if (!btn.isConnected) return; flashDone(btn, "download", "已保存", dlTip()); setTimeout(() => row.classList.remove("dl"), 1200); }, 900);
  }
  function bkClick(e) {
    const a = e.target.closest("[data-wd-act]"); if (!a) return;
    if (a.getAttribute("aria-disabled") === "true") return;
    const act = a.dataset.wdAct, row = a.closest(".wd-row"), b = row && dat().backups.find(x => x.file === row.dataset.wdFile);
    if (act === "make") makeBackup();
    else if (act === "auto") toggleAuto();
    else if (act === "dl" && b && !row.classList.contains("busy")) download(row, a);
    else if (act === "restore" && b && !row.classList.contains("busy")) askRestore(b);
    else if (act === "del" && b && !row.classList.contains("busy")) askDelete(b);
    else if (act === "sched") C.goTab("tasks");
    else if (act === "tomap") document.querySelector('#snav [data-s="map"]')?.click(); // 外壳没给切子页的接口：照外壳自己的做法点左边那一行
    else if (act === "old") a.animate([{ opacity: 1 }, { opacity: .5 }, { opacity: 1 }], { duration: 300 }); // 草图里不真打开文件夹
  }
  window.DETAIL_TABS["world/backups"] = {
    html(ctx) { C = ctx; closeAsk(true); dat(); return bkHTML(); },
    mount(ctx) {
      C = ctx; const r = bkRoot(); if (!r) return;
      ctx.thinBar($(".wd-lw", r));
      r.addEventListener("click", bkClick);
      $(".wd-inp", r).addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); makeBackup(); } });
      if (making) { const sl = $(".wd-goslot", r); sl.innerHTML = makeBtn(true); hy(sl); $(".wd-inp", r).disabled = true; }
    },
  };

  // ================= 换地图 =================
  const mapRoot = () => document.querySelector(".cp > .wd-map:not(.fade-out)");
  const SET_NAME = { difficulty: "难度", gamemode: "默认游戏模式" };
  const SET_VAL = { peaceful: "和平", easy: "简单", normal: "普通", hard: "困难", survival: "生存", creative: "创造", adventure: "冒险", spectator: "旁观" };
  const DELETABLE = new Set(["committed", "rolled-back", "restored"]);
  const verKey = v => { const x = String(v).match(/^(\d+)\.(\d+)(?:\.(\d+))?(?:-snapshot-(\d+))?/); return x ? [+x[1], +x[2], +(x[3] || 0), x[4] != null ? -1000 + +x[4] : 0] : [0, 0, 0, 0]; };
  const cmpVer = (a, b) => { const x = verKey(a), y = verKey(b); for (let i = 0; i < 4; i++) if (x[i] !== y[i]) return x[i] - y[i]; return 0; };
  const cand = () => { const D = dat(); return D.src.cands[D.flow.cand]; };
  const relation = c => !c.ver ? "unknown" : cmpVer(c.ver, C.S.mc) === 0 ? "same" : cmpVer(c.ver, C.S.mc) < 0 ? "older" : "newer";
  const settingsOf = c => Object.keys(SET_NAME).filter(k => c[k === "difficulty" ? "diff" : "mode"] && c[k === "difficulty" ? "diff" : "mode"] !== dat().props[k])
    .map(k => ({ key: k, before: dat().props[k], after: c[k === "difficulty" ? "diff" : "mode"] }));

  // 恢复点一条现在是什么情况：[标签样式, 标签, 一句话]（worldimport.js pointSay，那句话进标签的悬停）
  function pointSay(pt) {
    const D = dat();
    if (pt.state === "committed") {
      const newest = D.points.find(x => x.state === "committed");
      const noOld = "导入前服务器尚无存档，无可换回的原存档";
      if (newest && newest !== pt) return ["", "更早的导入", "此后已导入其他地图。" + (!pt.hasOld ? noOld : "需先换回最近一次导入，才能换回此次之前的存档")];
      if (pt.canReturn) return ["acc", "已导入", "当前正在使用此地图。导入前的存档保留在恢复点中，可随时换回"];
      return ["acc", "已导入", "当前正在使用此地图。" + (pt.hasOld ? "当前无法换回导入前的存档" : noOld)];
    }
    return ({ restored: ["", "已换回", "已换回导入前的存档。曾导入的地图（含后续改动）仍另行保留，确认不再需要后可删除"],
      restoring: ["warn", "正在换回", "正在换回导入前的存档"], "rolled-back": ["", "没有生效", "此次导入未生效，服务器仍使用原存档"],
      // 没走完的几种（worldimport.js 的 POINT）。草图里走得到的只有「正在更换」「需手动处理」（地址参数 map=busy、map=stuck）
      staging: ["", "未完成", "此次导入停在复制阶段，原存档未变动"], staged: ["", "未完成", "地图已复制完成，尚未开始替换，原存档未变动"],
      committing: ["warn", "正在更换", "正在更换地图"], "rolling-back": ["warn", "正在退回", "正在退回原存档"],
      stuck: ["warn", "需手动处理", "自动恢复未成功。所有文件均未删除，服务器已暂停启动"] })[pt.state]
      || ["", "状态不明", "无法读取此次导入的状态"];
  }
  // 存档动不了时（D.hold），导入、填路径和恢复点的按钮为什么灰着
  const holdWhy = () => ({ busy: "正在更换地图，请等待操作完成", stuck: "换地图尚未完成，请先重试恢复" })[dat().hold] || "";
  function ptRow(pt) {
    const [cls, tag, say0] = pointSay(pt), why = holdWhy() || (live() ? (pt.canReturn ? "请先停止服务器后再换回或删除" : "请先停止服务器后再删除恢复点") : "");
    const dis = why ? ' aria-disabled="true"' : "", busy = ["restoring", "committing", "rolling-back"].includes(pt.state);
    const say = say0 + (pt.error ? `\n当时的原因：${pt.error}` : ""); // 出过错的那一次（没有生效、需手动处理）：原因跟在那句话后面
    const sizes = [pt.oldMb > 0 ? `原存档 ${size(pt.oldMb)}` : "", pt.keptMb > 0 ? `已保留地图 ${size(pt.keptMb)}` : ""].filter(Boolean).join(" · ");
    return `<div class="wd-pt" data-wd-pt="${pt.id}"><div class="wd-pn"><span class="nm">${esc(pt.name)}</span><span class="wd-tag ${cls}" data-wd-k="tag" title="${esc(say)}">${tag}</span><span class="wd-sp"></span>`
      + `<span class="wd-pa" data-wd-k="acts">${busy ? `<span class="wd-busy">${icon("spinner", "wd-spin")}</span>`
        : (pt.canReturn ? `<button type="button" class="wd-btn" data-wd-act="ret"${dis} title="${esc(why || "换回此次导入前的存档；当前地图（含导入后的所有改动）会另行保留，不会删除")}"><i data-ic="restore"></i>换回导入前的存档</button>` : "")
        + (DELETABLE.has(pt.state) ? `<button type="button" class="wd-ib" data-wd-act="pdel"${dis} title="${esc(why || "删除恢复点")}"><i data-ic="trash"></i></button>` : "")}</span></div>`
      + `<div class="wd-ps" data-wd-k="sub">${esc(pt.created)} 导入 · ${m(pt.source)}${sizes ? " · " + sizes : ""}</div></div>`;
  }
  // ---- 打开时那页：左「替换当前地图」、右「恢复点」 ----
  const dzTip = () => `支持冒险、空岛、跑酷地图，以及单人存档或旧服存档。压缩包内嵌套多层文件夹、附带说明书或资源包时均可识别；含多张地图时可从中选择。\n原存档不会删除，将完整保留在恢复点中随时换回；核心、版本${addonList() ? "、" + addonList() : ""}和各项设置均保持不变`;
  // 来源读不出来、不是地图时虚线框里写的两句（程序 worldimport.js 的 drawRejected、scanPath 和 startUpload 的报错框；原话在 worldimport/scan.go、source.go）
  const SRC_ERR = {
    modpack: ["此文件为整合包，无法作为地图导入", "未检测到可用存档，请更换来源"],
    zip: ["无法读取此压缩包", "文件可能未下载完整或已损坏，请重新下载后再试"],
    dir: ["无法读取此文件夹", "可能被其他程序占用，或没有访问权限"],
  };
  // 警告卡（程序 busyHTML），占着虚线框的位置。busy：等它做完，这一页会自己刷新。stuck：换到一半、自动退回也没成功，文件都在、服务器不让启动；
  // 列出要人去看的位置（txn.go markStuck 记下的路径；别的设备上后端只给个数），关掉占着存档的程序、停了服务器再点「重试恢复」（service.go Retry）
  function holdInner() {
    if (dat().hold === "busy") return `<span class="wd-dzi warn"><i data-ic="spinner" class="wd-spin"></i></span><b>正在更换地图</b><span class="wd-dzs">请等待操作完成，本页将自动更新</span>`;
    const tx = `D:\\MineDeck\\servers\\${C.S.name}\\.minedeck\\world-import\\w4c1d9e07a2b35f6e18`;
    return `<span class="wd-dzi warn"><i data-ic="warn"></i></span><b>换地图未完成，自动恢复失败</b><span class="wd-dzs">所有文件均未删除，服务器已暂停启动</span>`
      + (remote() ? `<span class="wd-dzs">需检查 2 个文件夹，请在运行 MineDeck 的设备上查看</span>`
        : `<span class="wd-stk mono" title="需检查的位置">${[tx + "\\old\\" + dat().level, tx + "\\new\\" + dat().level].map(p => esc(p).replace(/\\/g, "\\<wbr>")).join("<br>")}</span>`)
      + `<span class="wd-dzs" title="例如在 MineDeck 之外手动启动的服务器、打开了此存档的地图工具">请先关闭占用存档文件夹的程序，再重试恢复</span><span class="wd-rty">${retryBtn(false)}</span>`;
  }
  const retryBtn = busy => busy ? `<button type="button" class="wd-btn" aria-disabled="true">${icon("spinner", "wd-spin")}正在恢复</button>`
    : `<button type="button" class="wd-btn" data-wd-act="retry"${live() ? ` aria-disabled="true" title="请先停止服务器"` : ""}><i data-ic="refresh"></i>重试恢复</button>`;
  // 虚线框现在是哪种样子：hold 换成警告卡、busy 正在传或识别、bad 来源用不了、空串等人选
  const dzState = () => { const D = dat(), p = D.flow.phase; return D.hold ? "hold" : p === "up" || p === "scan" ? "busy" : p === "error" ? "bad" : ""; };
  function dzInner() {
    const F = dat().flow;
    if (dat().hold) return holdInner();
    if (F.phase === "up") return `<span class="wd-dzi"><i data-ic="arrowUp"></i></span><b class="mono">${esc(F.fileName)}</b>`
      + `<div class="wd-upr"><div class="wd-upbar">${"<i></i>".repeat(30)}</div><button type="button" class="wd-ib" data-wd-act="upx" title="取消上传"><i data-ic="x"></i></button></div>`
      + `<span class="wd-dzs"><span class="num wd-uptxt">${upText()}</span></span>`;
    if (F.phase === "scan") return `<span class="wd-dzi"><i data-ic="spinner" class="wd-spin"></i></span><b>正在识别…</b><span class="wd-dzs">${m(F.fromPath ? F.path : F.fileName)}</span>`;
    if (F.phase === "error") { const [t, s] = SRC_ERR[F.err] || SRC_ERR.modpack; return `<span class="wd-dzi bad"><i data-ic="warn"></i></span><b>${t}</b><span class="wd-dzs bad">${s}</span>`; }
    return `<span class="wd-dzi"><i data-ic="map"></i></span><b>拖入地图压缩包，或点击选择</b><span class="wd-dzs">.zip · 最大 8 GB</span>`;
  }
  const upText = () => { const F = dat().flow, mb = dat().src.mb; return `已上传 ${size(mb * F.pct / 100)} / ${size(mb)} · ${Math.floor(F.pct)}%`; };
  // 填路径那一行。用不了的时候整行灰掉、悬停说原因：存档动不了时（警告卡在），或者从别的设备打开（程序 worldimport.js sourceUI：本机路径只给坐在这台电脑前的人用，
  // 别的设备上只能传 ZIP；程序是把这一行藏掉、另写一句说明）
  const pathOff = () => holdWhy() || (remote() ? "仅可在运行 MineDeck 的设备上选择文件夹或填写本机路径；在当前设备上，请将地图压缩为 ZIP 后上传" : "");
  function prowHTML() {
    const F = dat().flow, why = pathOff(), off = why ? ` aria-disabled="true" title="${esc(why)}"` : "";
    return `<label class="wd-prow${why ? " off" : ""}"${why ? ` title="${esc(why)}"` : ""}><input class="wd-path" spellcheck="false" autocomplete="off" value="${esc(F.fromPath ? F.path : "")}" aria-label="地图文件夹或 ZIP 文件的路径" placeholder="或输入文件夹 / ZIP 路径，如 D:\\Maps\\SkyCity"`
      + `${why ? ` readonly tabindex="-1"` : ` title="本机上的地图存档文件夹（含 level.dat 或其上级文件夹），或未解压的 ZIP 压缩包"`}>`
      + `<button type="button" class="wd-ib" data-wd-act="browse"${off || ` title="选择地图文件夹（含 level.dat 的存档文件夹或其上级文件夹）"`}><i data-ic="folder"></i></button>`
      + `<button type="button" class="wd-ib" data-wd-act="scan"${off || ` title="检测"`}><i data-ic="search"></i></button></label>`;
  }
  function homeHTML() {
    const D = dat(), st = dzState();
    return `<div class="wd-home"><section class="wd-col wd-imp"><div class="wd-hd"><span class="wd-t">替换当前存档</span>`
      + `<span class="wd-lv" title="${esc(`当前使用的存档文件夹 ${D.level}（对应 server.properties 中的 level-name）`)}">${esc(D.level)}</span></div>`
      + `<div class="wd-dz${st && " " + st}" data-wd-act="pick" tabindex="0" role="button" aria-label="拖入地图压缩包，或点击选择"${st === "busy" || st === "hold" ? "" : ` title="${esc(dzTip())}"`}><div class="wd-dzc">${dzInner()}</div></div>`
      + `${prowHTML()}</section>`
      + `<section class="wd-col wd-pts">${folders()}<div class="wd-hd fdpad"><span class="wd-t" title="导入地图时，原存档将完整保留于此，可随时换回。恢复点为原样留存的存档文件夹，独立于普通备份之外，不受备份保留份数限制，直至手动删除">恢复点</span>`
      + `<span class="wd-n" data-wd-k="pn">${D.points.length || ""}</span></div>`
      + `<div class="wd-lw${D.points.length ? "" : " is-none"}"><div class="list">${D.points.map(ptRow).join("")}</div><span class="thumb"></span><div class="wd-none">暂无恢复点</div></div></section></div>`;
  }
  // ---- 选了来源以后的三页：选择地图、核对、进度 ----
  function cover() {
    const D = dat(), F = D.flow, S = D.src, c = cand();
    const name = F.screen === "pick" ? (F.fromPath ? esc(F.path.split("\\").pop()) : m(F.fileName)) : esc(c.name);
    const sub = F.screen === "pick" ? (F.fromPath ? `文件夹 · ${m(F.path)}` : `压缩包 · ${size(S.mb)}`) : `${m(c.root)} · ${size(c.mb)} · 来自 ${F.fromPath ? m(F.path) : m(F.fileName)}`;
    return `<div class="wd-cov" style="--c:${MDPixel16.CORE_COLORS.vanilla}"><span class="wd-cava">${MDPixel16.svg("vanilla", 32)}</span>`
      + `<div class="wd-ct"><div class="wd-cn" data-wd-k="cn">${name}</div><div class="wd-cs" data-wd-k="cs">${sub}</div></div></div>`;
  }
  const footHTML = (left, right) => `<div class="wd-foot"><span class="wd-fl">${left}</span><span class="wd-sp"></span><span class="wd-fr">${right}</span></div>`;
  const backBtn = (act = "back") => `<button type="button" class="wd-nb back" data-wd-act="${act}"><i data-ic="chevronRight"></i>上一步</button>`;
  function candRow(c, i) {
    const F = dat().flow;
    return `<div class="wd-cand${i === F.cand ? " on" : ""}${c.block ? " off" : ""}" data-wd-cand="${i}"${c.block ? ` title="${esc(c.blockFull)}"` : ""}>`
      + `<span class="wd-ci"><i data-ic="map"></i></span><div class="wd-cm"><div class="n">${esc(c.name)}</div><div class="s">${m(c.root)} · ${size(c.mb)}</div></div>`
      + (c.block ? `<span class="wd-tag bad">无法导入</span>` : "") + `</div>`;
  }
  // 右栏：一张固定的小表（每张地图都是这几行，没有的写「没有」），换地图时只换变了的那几格（同 import.html 的 mapDetInner）
  function factsHTML(c) {
    const dims = c.dims.map(([n, r]) => `<span title="${r} 个区域文件">${n}</span>`).join(" · ")
      + (c.missing.length ? `<span class="f" title="地图未包含${c.missing.join("、")}（并非导入遗漏），服务器首次加载时将自动生成">${c.missing.join("、")}将自动生成</span>` : "");
      const dd = (k, v) => `<dd data-wd-k="${k}">${v}</dd>`, no = `<span class="f">无</span>`;
    const clue = c.clues.length ? `${c.clues.slice(0, 3).map(m).join("、")}${c.clues.length > 3 ? `<span class="f">等 ${c.clues.length} 个</span>` : ""}`
      : `<span title="未检测到服务端或模组线索，无法确认是否为纯原版">无（按原版处理）</span>`;
    const notes = (c.notes || []).map(([t, s, full]) => `<div class="wd-nt ${t}" title="${esc(full)}"><i data-ic="${t === "warn" ? "warn" : "info"}"></i><span>${esc(s)}</span></div>`).join("");
    return `<dl class="wd-facts"><dt>保存于</dt>${dd("ver", m(c.ver || "未记录"))}<dt>布局</dt>${dd("lay", `<span title="${esc(c.layoutFull)}">${c.layout}</span>`)}`
      + `<dt>维度</dt>${dd("dim", dims)}<dt>数据包</dt>${dd("dp", c.dps.length ? c.dps.map(m).join("、") : no)}`
      + `<dt>资源包</dt>${dd("rp", c.rp ? `${m("resources.zip")}<span class="f">不会自动下发给玩家</span>` : no)}`
      + `<dt>模组线索</dt>${dd("clue", clue)}<dt>玩家进度</dt>${dd("pl", c.players ? `${c.players} 份玩家数据` : no)}`
      + `<dt>说明书</dt>${dd("rd", c.readme ? `${m(c.readme)}<button type="button" class="wd-ib sm" data-wd-act="readme" title="查看说明书（仅以纯文本显示，请勿随意访问其中的链接）"><i data-ic="doc"></i></button>` : no)}`
      + `<dt>不会导入</dt>${dd("hint", c.hints.length ? `<span title="与地图位于同一文件夹的附带文件，不会导入">${c.hints.map(m).join("、")}</span>` : no)}</dl>`
      + `<div class="wd-notes" data-wd-k="notes">${notes}</div>`;
  }
  function pickHTML() {
    const D = dat(), S = D.src;
    return `<div class="wd-flow">${cover()}<div class="wd-scols">`
      + `<section class="wd-col"><div class="wd-hd"><span class="wd-t">地图</span><span class="wd-n">${S.cands.length}</span></div>`
      + `<div class="wd-lw"><div class="list"><i class="wd-hl"></i>${S.cands.map(candRow).join("")}</div><span class="thumb"></span></div></section>`
      + `<section class="wd-col wd-detc"><div class="wd-hd"><span class="wd-t">地图内容</span></div><div class="wd-det">${factsHTML(cand())}</div></section></div>`
      + footHTML(backBtn(), `<button type="button" class="wd-nb pri" data-wd-act="next">下一步<i data-ic="chevronRight"></i></button>`) + `</div>`;
  }
  // 核对（程序 planHTML 的 replace 那一种）
  const REL = {
    same: ["ok", "版本一致", "版本一致，可直接导入"],
    older: ["warn", "将自动升级", null],
    newer: ["bad", "无法读取", null],
    unknown: ["warn", "无法判断", "无法判断地图与服务器的版本高低（地图未记录版本或版本不在列表中）"],
  };
  const needAck = rel => rel === "older" || rel === "unknown";
  function ackText(c, rel) {
    const mc = C.S.mc;
    if (rel === "unknown") return ["地图未记录版本，无法判断是否兼容", REL.unknown[2]];
    return [`地图将升级至 ${mc}，升级后无法再以旧版本打开`, `该地图最后保存于 ${c.ver}，低于服务器的 ${mc}。首次启动时游戏会将其升级至 ${mc}，之后无法再用于旧版本；导入前的存档将保留在恢复点中，不受影响。旧版地图的机关、命令与数据包在新版本中可能失效`];
  }
  function checkHTML() {
    const D = dat(), F = D.flow, c = cand(), rel = relation(c), [rc, rt] = REL[rel], sets = rel === "newer" ? [] : settingsOf(c), S = C.S; // 读不了的地图不列可改的设置
    const relTip = rel === "newer" ? `地图版本高于服务器，${S.mc} 无法读取，无法导入。可新建一台 ${c.ver} 的服务器使用此地图` : rel === "older" ? ackText(c, rel)[1] : REL[rel][2];
    const left = `<section class="wd-col wd-chk"><div class="wd-hd"><span class="wd-t">核对</span></div>`
      + `<div class="wd-rel"><div class="wd-rv"><span class="k">地图保存于</span><span class="v mono">${esc(c.ver || "未记录")}</span></div><i data-ic="arrowRight"></i>`
      + `<div class="wd-rv"><span class="k">当前服务器</span><span class="v"><span class="mono">${esc(S.label)} ${esc(S.mc)}</span></span></div><span class="wd-sp"></span>`
      + `<span class="wd-tag ${rc}" title="${esc(relTip)}">${rt}</span></div>`
      + `<dl class="wd-facts"><dt>替换</dt><dd><span>${D.worlds.map(m).join("、")}</span><span class="f" title="将完整移入恢复点而不删除，后续可在「恢复点」中换回">移入恢复点</span></dd>`
      + `<dt>保留</dt><dd>核心和版本、Java、内存、端口、${addonList() ? addonList() + "、" : ""}白名单和管理员、其他设置</dd>`
      + `<dt>空间</dt><dd>需要约 ${size(c.mb * 1.08)}<span class="f">D 盘剩余 214 GB</span></dd></dl>`
      + `<div class="wd-notes">${(c.notes || []).map(([t, s, full]) => `<div class="wd-nt ${t}" title="${esc(full)}"><i data-ic="${t === "warn" ? "warn" : "info"}"></i><span>${esc(s)}</span></div>`).join("")}</div></section>`;
    const right = sets.length ? `<section class="wd-col wd-setc"><div class="wd-hd"><span class="wd-t" title="以下设置项与地图记录不同。默认保持不变；开启后将在导入时同步修改，换回原存档时一并恢复">可同步修改的设置</span></div>`
      + `<div class="wd-sets">${sets.map(s => `<button type="button" class="wd-set${F.sets[s.key] ? " on" : ""}" data-wd-set="${s.key}" role="switch" aria-checked="${!!F.sets[s.key]}">`
        + `<span class="wd-sw"></span><span class="nm">${SET_NAME[s.key]}</span><span class="wd-sp"></span><span class="v">${SET_VAL[s.before] || s.before}${icon("arrowRight")}<b>${SET_VAL[s.after] || s.after}</b></span></button>`).join("")}</div></section>` : "";
    // 底下一条：要确认的、要停服的；地图比服务器新时换成一句红字（不能往下走）
    let strip = "";
    if (rel === "newer") strip = `<div class="wd-strip"><span class="wd-sbad" title="${esc(relTip)}"><i data-ic="warn"></i>地图版本高于服务器，${esc(S.mc)} 无法读取，无法导入</span></div>`;
    else if (needAck(rel) || live()) {
      const [s, full] = needAck(rel) ? ackText(c, rel) : ["", ""];
      strip = `<div class="wd-strip">` + (needAck(rel) ? `<button type="button" class="wd-tg${F.ack ? " on" : ""}" data-wd-tg="ack" role="switch" aria-checked="${F.ack}" title="${esc(full)}"><span class="wd-sw"></span><span class="t">我已了解：<b>${esc(s)}</b></span></button>` : "")
        + (live() ? `<button type="button" class="wd-tg${F.stop ? " on" : ""}" data-wd-tg="stop" role="switch" aria-checked="${F.stop}" title="服务器正在运行。开启后，导入前会先通知服务器保存并正常退出（不强制终止）；若超时未能停止则中止导入，不会修改任何文件"><span class="wd-sw"></span><span class="t">停止并导入</span></button>` : "") + `</div>`;
    }
    const go = rel === "newer" ? `<button type="button" class="wd-nb pri" aria-disabled="true" title="${esc(relTip)}"><i data-ic="download"></i>导入并替换当前存档</button>`
      : `<button type="button" class="wd-nb pri" data-wd-act="go"><i data-ic="download"></i>导入并替换当前存档</button>`;
    return `<div class="wd-flow">${cover()}<div class="wd-scols chk${sets.length ? "" : " one"}">${left}${right}</div>${strip}${footHTML(backBtn(), go)}</div>`;
  }
  // 进度：四步（internal/worldimport/service.go importSteps 的前四步；它的第五步「完成」不画，四步都打勾就是做完了），复制那一步有 30 格的经验条
  const STEPS = ["核对地图和服务器", "复制地图到暂存区", "停止并锁定服务器", "换上新地图"];
  // 一步现在的样子：done 做完了、run 正在做、cx 取消时停在这一步、err 在这一步出的错、空串还没到。T.step 是正在做的那一步，都做完时等于步数
  const stepCls = i => { const T = dat().flow.task; return i < T.step ? "done" : i > T.step ? "" : !T.ended ? "run" : T.error ? "err" : "cx"; };
  const stepIco = i => ({ done: icon("check"), run: icon("spinner", "wd-spin"), err: icon("x"), cx: `<i class="wd-dash"></i>` })[stepCls(i)] || `<i class="wd-px"></i>`;
  // 示例里出的错：换的那一步挪不动存档文件夹（被别的程序占着），程序自动退回了原存档（service.go runImport「导入中断，已恢复到导入前的世界：…」；字同 task.js 那份）
  const failCause = () => `rename MineDeck\\servers\\${C.S.name}\\${dat().level}: Access is denied.`;
  const failText = () => `导入中断，已退回导入前的存档：${failCause()}`;
  const FAIL_HINT = "没有读写权限，请检查文件夹权限或关闭占用它的程序";
  const logLine = l => `<div${l === dat().flow.task.error ? ' class="err"' : ""}>${esc(l)}</div>`; // 报错那一行标红、折行，别的一行一条
  function taskHTML() {
    const D = dat(), T = D.flow.task, c = cand();
    return `<div class="wd-flow">${cover()}<section class="wd-col wd-task"><div class="wd-steps">${STEPS.map((s, i) => `<div class="wd-step ${stepCls(i)}" data-wd-step="${i}"><span class="ic">${stepIco(i)}</span><span>${s}</span></div>`).join("")}</div>`
      + `<div class="wd-prog"><div class="wd-ph"><span class="mono wd-pt2">${size(c.mb * T.pct / 100)} / ${size(c.mb)}</span><span class="wd-sp"></span><span class="mono wd-pct">${Math.floor(T.pct)}%</span></div>`
      + `<div class="wd-xp${T.step > 1 ? " done" : ""}">${Array.from({ length: 30 }, (_, i) => `<i${i < Math.floor(T.pct / 100 * 30 + 1e-6) ? ' class="on"' : ""}></i>`).join("")}</div>`
      + `<div class="wd-lw wd-log"><div class="list">${T.log.map(logLine).join("")}</div><span class="thumb"></span></div>`
      + `<div class="wd-msg" data-wd-k="msg">${taskMsg()}</div></div></section>${footHTML(taskLeft(), taskRight())}</div>`;
  }
  // 结束时的那句。出错：一句说结果，一句说怎么办、后面一颗复制报错信息（原话在过程记录最后一行）
  const taskMsg = () => {
    const T = dat().flow.task;
    if (T.error) return `<span class="wd-er">${icon("warn")}导入未完成，已退回导入前的存档。</span>`
      + `<span class="wd-mi2">${FAIL_HINT}<button type="button" class="wd-ib sm" data-wd-act="cperr" title="复制报错信息">${icon("copy")}</button></span>`;
    return T.cancelled ? `<span class="wd-mi2">${icon("info")}已取消换地图：未替换任何文件，原存档未变动。</span>` : T.ended ? `<span class="wd-ok">${icon("check")}地图已替换完成。</span>` : "";
  };
  // 出错和取消以后底下是同一排（worldimport.js showTask）：左「重新核对地图」（这次检测还留着，不用再传一遍），右「返回换地图」
  function taskLeft() {
    const T = dat().flow.task;
    if (T.cancelled || T.error) return `<button type="button" class="wd-nb" data-wd-act="again"><i data-ic="refresh"></i>重新核对地图</button>`;
    if (T.ended) return "";
    const can = T.step <= 1;
    return `<button type="button" class="wd-nb" data-wd-act="cancel"${can ? ` title="地图正在复制，原存档尚未变动"` : ` aria-disabled="true" title="已开始替换存档，无法取消；若出错将自动退回原存档"`}>取消导入</button>`;
  }
  const taskRight = () => { const T = dat().flow.task; return T.ended ? `<button type="button" class="wd-nb pri" data-wd-act="done">${T.cancelled || T.error ? "返回换地图" : `${icon("check")}完成`}</button>` : ""; };
  const mapInner = () => { const F = dat().flow; return F.screen === "pick" ? pickHTML() : F.screen === "check" ? checkHTML() : F.screen === "task" ? taskHTML() : homeHTML(); };
  function mapAfter(r) {
    $$(".wd-lw", r).forEach(w => C.thinBar(w));
    fitWdTitles(r);
    document.fonts?.ready.then(() => r.isConnected && fitWdTitles(r));
    placeHL(false);
    if (dat().flow.screen === "pick" && dat().flow.readme) openReadme(false);
  }
  // 换步：整页往走的方向挪 16 像素淡掉（0.14 秒），新的一步从另一边挪进来（0.26 秒）（规范第七节）
  let stepping = false; // 换步那一下（0.14 + 0.26 秒）里不收点击，免得连点两下照着新状态又走一步（同向导 goStep）
  function stepMap(dir) {
    const r = mapRoot(); if (!r) { stepping = false; return; }
    stepping = true;
    const tok = r._tok = (r._tok || 0) + 1;
    r.getAnimations().forEach(a => a.cancel());
    const out = r.animate([{ opacity: 1, transform: "none" }, { opacity: 0, transform: `translateX(${-16 * dir}px)` }], { duration: 140, easing: EASE_IN, fill: "forwards" });
    out.onfinish = () => {
      if (r._tok !== tok) return;
      r.innerHTML = mapInner(); hy(r); mapAfter(r); out.cancel();
      r.animate([{ opacity: 0, transform: `translateX(${16 * dir}px)` }, { opacity: 1, transform: "none" }], { duration: 260, easing: EASE_OUT })
        .onfinish = () => { if (r._tok === tok) stepping = false; };
      const nw = dat().flow.justAdded; if (nw) { dat().flow.justAdded = ""; const row = $(`[data-wd-pt="${nw}"]`, r); if (row) growIn(row, 180); }
    };
  }
  // 候选地图的选中框：单独一块，滑过去（同向导核心、版本那两列）
  function placeHL(animate) {
    const r = mapRoot(); if (!r) return;
    const hl = $(".wd-hl", r), on = $(".wd-cand.on", r); if (!hl || !on) return;
    if (!animate) hl.style.transition = "none";
    hl.style.transform = `translateY(${on.offsetTop}px)`; hl.style.height = on.offsetHeight + "px";
    if (!animate) { void hl.offsetWidth; hl.style.transition = ""; }
  }
  // ---- 选来源 ----
  function paintDz() {
    const r = mapRoot(); if (!r || dat().flow.screen !== "home") return;
    const dz = $(".wd-dz", r), st = dzState();
    ["hold", "busy", "bad"].forEach(k => dz.classList.toggle(k, k === st));
    delete dz.dataset.tip; if (st === "busy" || st === "hold") dz.removeAttribute("title"); else dz.setAttribute("title", dzTip()); // 正在传、正在识别、换成警告卡时不出那段说明
    swapItems($(".wd-dzc", r), dzInner(), 1);
  }
  // 「重试恢复」：转一会儿当作在退回，成了以后警告卡收起、虚线框和填路径那行回来，那条恢复点变成「没有生效」（service.go recoverInstance：退回成功就解除封锁）
  function retryHold(btn) {
    const D = dat(), slot = btn.parentElement;
    slot.innerHTML = retryBtn(true);
    setTimeout(() => {
      D.hold = ""; D.points.forEach(p => { if (p.state === "stuck") p.state = "rolled-back"; });
      const r = mapRoot(); if (!r || D.flow.screen !== "home") return;
      paintDz(); $(".wd-prow", r).outerHTML = prowHTML(); hy($(".wd-prow", r)); D.points.forEach(paintPoint);
    }, 1400);
  }
  function paintUp() {
    const r = mapRoot(); if (!r) return;
    const F = dat().flow, n = Math.floor(F.pct / 100 * 30 + 1e-6);
    $$(".wd-upbar i", r).forEach((c, i) => c.classList.toggle("on", i < n));
    const t = $(".wd-uptxt", r); if (t) t.textContent = upText();
  }
  function startUpload(name) {
    const D = dat(), F = D.flow; if (D.hold || F.phase === "up" || F.phase === "scan") return;
    const tok = ++F.tok;
    Object.assign(F, { phase: "up", pct: 0, fileName: name || D.src.file, fromPath: false, path: "" });
    const inp = mapRoot() && $(".wd-path", mapRoot()); if (inp) inp.value = "";
    paintDz();
    const tick = () => {
      if (F.tok !== tok || F.phase !== "up") return;
      F.pct = Math.min(100, F.pct + 2.6 + Math.random() * 2.2); paintUp();
      if (F.pct < 100) setTimeout(tick, 70); else setTimeout(() => { if (F.tok === tok) scanThen(tok); }, 250);
    };
    setTimeout(tick, 320);
  }
  function startScan(path) {
    const F = dat().flow; if (pathOff() || F.phase === "up" || F.phase === "scan") return;
    const r = mapRoot(), row = r && $(".wd-prow", r);
    if (!path) { if (row) { row.classList.add("bad"); shake(row); $(".wd-path", r).focus(); } return; }
    // 草图里没有真文件：路径里带这几个字的当作整合包、读不了的文件夹，别的都当作认得出的地图
    const err = /mrpack|整合包|modpack/i.test(path) ? "modpack" : /损坏|broken/i.test(path) ? "dir" : "";
    if (err) { Object.assign(F, { phase: "error", err, path }); paintDz(); return; }
    F.tok++; Object.assign(F, { fromPath: true, path }); scanThen(F.tok);
  }
  function scanThen(tok) {
    const F = dat().flow; F.phase = "scan"; paintDz();
    setTimeout(() => {
      if (F.tok !== tok || F.phase !== "scan") return;
      F.phase = "empty"; F.screen = "pick"; F.cand = dat().src.cands.findIndex(c => !c.block); F.readme = false;
      stepMap(1);
    }, 900);
  }
  // ---- 说明书：点了在右栏里铺开（只当纯文字，同 import.html） ----
  function openReadme(animate = true) {
    const r = mapRoot(), col = r && $(".wd-detc", r); if (!col || $(".wd-rdm", col)) return;
    dat().flow.readme = true;
    col.insertAdjacentHTML("beforeend", `<div class="wd-rdm"><div class="wd-rh"><span class="mono">${esc(cand().readme)}</span><span class="wd-sp"></span><span class="f">地图作者附带说明，仅以纯文本显示</span>`
      + `<button type="button" class="wd-ib" data-wd-act="rdclose" title="收起"><i data-ic="x"></i></button></div><div class="wd-lw"><div class="list"><pre>${esc(README)}</pre></div><span class="thumb"></span></div></div>`);
    const p = hy($(".wd-rdm", col)); C.thinBar($(".wd-lw", p));
    if (animate) p.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT });
  }
  function closeReadme() {
    const p = mapRoot() && $(".wd-rdm", mapRoot()); dat().flow.readme = false; if (!p) return;
    p.animate([{ opacity: 1 }, { opacity: 0, transform: "translateY(5px)" }], { duration: 90, easing: EASE_IN, fill: "forwards" }).onfinish = () => p.remove();
  }
  function pickCand(i) {
    const D = dat(), F = D.flow; if (i === F.cand || D.src.cands[i].block) return;
    const dir = i > F.cand ? 1 : -1; F.cand = i; F.ack = false; F.sets = {};
    const r = mapRoot(); $$(".wd-cand", r).forEach(x => x.classList.toggle("on", +x.dataset.wdCand === i)); placeHL(true);
    if (F.readme) closeReadme();
    swapKeyed($(".wd-det", r), factsHTML(cand()), dir);
  }
  // ---- 导入 ----
  function tryGo() {
    const D = dat(), F = D.flow, c = cand(), rel = relation(c), r = mapRoot();
    const miss = [needAck(rel) && !F.ack ? "ack" : "", live() && !F.stop ? "stop" : ""].filter(Boolean);
    if (miss.length) { // 不开不让往下走：那一条描红，开关晃一下（同向导的 EULA、import.html 的「核对地图」）
      const st = $(".wd-strip", r); st?.classList.add("bad");
      miss.forEach(k => { const t = $(`[data-wd-tg="${k}"]`, r); t?.classList.add("bad"); shake(t); });
      return;
    }
    const sets = settingsOf(c).filter(s => F.sets[s.key]);
    ask({ icon: "download", w: 420, title: `用<span class="nw">「${esc(c.name)}」</span>替换当前存档？`, ok: live() ? "停止并替换" : "替换存档",
      body: `当前存档（${D.worlds.map(m).join("、")}）将完整移入恢复点，后续可在本页换回。`
        + (sets.length ? `<br>同步修改设置：${sets.map(s => `${SET_NAME[s.key]}改为${SET_VAL[s.after]}`).join("、")}。` : "")
        + (live() ? `<br>将先通知服务器保存并正常停止（不强制终止）。` : "")
        + `<br>${keptWhat().replace("和设置保持不变", "和其他设置不变")}。`,
      onOk: startTask });
  }
  // 一次导入的任务。每一步做完时过程记录里添的话在 stepLogs 里，一步一组（直接画成出错结束的那一种也照它凑，见 failedTask）
  const newTask = () => { const F = dat().flow; return { step: 0, pct: 0, ended: false, cancelled: false, error: "", log: [`开始导入：${cand().name}（${F.fromPath ? F.path : F.fileName}）`] }; };
  const stepLogs = () => [["核对通过：地图与当前服务器兼容"], [`已复制 ${size(cand().mb)} 到暂存区`], [live() ? "服务器已保存并退出，已锁定当前服务器" : "已锁定当前服务器，暂停启动"],
    [`原存档已移入恢复点：${dat().worlds.join("、")}`, "新地图已替换完成"]];
  const doneStep = T => { T.log.push(...stepLogs()[T.step]); T.step++; };
  const failStep = T => { T.error = failText(); T.log.push(T.error); T.ended = true; }; // 出错：停在正在做的那一步，报错记进过程记录
  // 地址参数 map=fail 直接画出来的那种：前三步做完，「换上新地图」那一步出错
  function failedTask() { const T = newTask(); T.pct = 100; while (T.step < STEPS.length - 1) doneStep(T); failStep(T); return T; }
  function startTask() {
    const D = dat(), F = D.flow;
    F.task = newTask(); F.screen = "task"; stepMap(1);
    const tok = ++F.tok, T = F.task;
    const at = (ms, fn) => setTimeout(() => { if (F.tok === tok && !T.cancelled) fn(); }, ms);
    at(900, () => { doneStep(T); paintTask(); copy(); });
    const copy = () => {
      if (F.tok !== tok || T.cancelled) return;
      T.pct = Math.min(100, T.pct + 3 + Math.random() * 3); paintTask(true);
      if (T.pct < 100) { setTimeout(copy, 90); return; }
      doneStep(T); paintTask();
      at(900, () => { doneStep(T); paintTask(); });
      // 最后一步打勾的同一刻就算做完（没有第五步了：下面那句和按钮「完成」跟着一起出来）；D.fail 时在这一步出错
      at(2100, () => { if (D.fail) failStep(T); else { doneStep(T); T.ended = true; } paintTask(); });
    };
  }
  function paintTask(onlyBar) {
    const r = mapRoot(), T = dat().flow.task; if (!r || !T || dat().flow.screen !== "task") return;
    const n = Math.floor(T.pct / 100 * 30 + 1e-6);
    $$(".wd-xp i", r).forEach((c, i) => c.classList.toggle("on", i < n));
    $(".wd-pct", r).textContent = Math.floor(T.pct) + "%";
    $(".wd-pt2", r).textContent = `${size(cand().mb * T.pct / 100)} / ${size(cand().mb)}`;
    if (onlyBar) return;
    $(".wd-xp", r).classList.toggle("done", T.step > 1); // 复制完了：经验条淡下去，步骤那边已经打了勾
    $$(".wd-step", r).forEach((s, i) => { const cls = `wd-step ${stepCls(i)}`; if (s.className.trim() !== cls.trim()) { s.className = cls; $(".ic", s).innerHTML = stepIco(i); } });
    const list = $(".wd-log .list", r), have = list.children.length;
    T.log.slice(have).forEach(l => { list.insertAdjacentHTML("beforeend", logLine(l)); list.lastElementChild.animate([{ opacity: 0, transform: "translateY(5px)" }, { opacity: 1, transform: "none" }], { duration: 220, easing: EASE_OUT }); });
    list.scrollTop = list.scrollHeight; $(".wd-log", r)._paint?.();
    swapKeyed($(".wd-msg", r).parentElement, `<div class="wd-msg" data-wd-k="msg">${taskMsg()}</div>`, 1);
    const fl = $(".wd-fl", r), fr = $(".wd-fr", r);
    if (fl.innerHTML !== hy(Object.assign(document.createElement("span"), { innerHTML: taskLeft() })).innerHTML) swapItems(fl, taskLeft(), 1);
    if (fr.innerHTML !== hy(Object.assign(document.createElement("span"), { innerHTML: taskRight() })).innerHTML) swapItems(fr, taskRight(), 1);
  }
  function finishTask() {
    const D = dat(), F = D.flow, T = F.task, c = cand(); if (!T) return;
    const source = F.fromPath ? F.path.split("\\").pop() : F.fileName;
    if (T.error) {
      // 出错、已经退回：存档没变（不清「从备份还原的」那条记录，同程序 intent.WorldUnchanged），恢复点里留一条「没有生效」，带着当时的原因
      const id = "p" + Date.now();
      D.points.unshift({ id, name: c.name, state: "rolled-back", created: stamp().t, source, oldMb: 0, hasOld: false, error: failCause() });
      F.justAdded = id;
    } else if (!T.cancelled) {
      // 新的一条恢复点排最前；以前那条「已导入」就成了「更早的导入」，不能再直接换回（worldimport canReturn 只给最近那一次）
      D.points.forEach(p => { p.canReturn = false; });
      const s = stamp(), id = "p" + Date.now();
      D.points.unshift({ id, name: c.name, state: "committed", created: s.t, source, oldMb: D.worldMb, hasOld: true, canReturn: true });
      D.wf = null; D.worldMb = c.mb; // 换地图会清掉「从备份还原的」那条记录（app.js 注释：worldFrom 换地图时清掉）
      F.justAdded = id;
    }
    Object.assign(F, freshFlow(), { justAdded: F.justAdded || "" });
    stepMap(-1);
  }
  function askReturn(pt) {
    ask({ icon: "restore", w: 400, title: `换回<span class="nw">「${esc(pt.name)}」</span>导入前的存档？`, ok: "换回导入前的存档",
      body: `当前地图（含导入后的所有改动）会另行保留，不会删除。`,
      onOk: () => {
        const D = dat(); pt.state = "restoring"; paintPoint(pt);
        setTimeout(() => {
          pt.state = "restored"; pt.canReturn = false; pt.keptMb = D.worldMb; pt.returned = stamp().t; D.worldMb = pt.oldMb || D.worldMb; pt.oldMb = 0; D.wf = null;
          paintPoint(pt);
        }, 1600);
      } });
  }
  function askDelPoint(pt) {
    const what = pt.state === "committed" ? (pt.hasOld ? `将删除 ${esc(pt.created)} 导入「${esc(pt.name)}」前的原存档${pt.oldMb ? `（${size(pt.oldMb)}）` : ""}，删除后将无法换回。` : "导入前服务器尚无存档，仅删除此次导入记录。")
      : pt.state === "restored" ? `将删除换回原存档时保留的导入地图${pt.keptMb ? `（${size(pt.keptMb)}）` : ""}及此次导入记录。` : "此次导入未生效，将删除相关记录与临时文件。";
    ask({ icon: "trash", w: 400, title: `删除恢复点<span class="nw">「${esc(pt.name)}」？</span>`, ok: "删除恢复点",
      body: `${what}<br>删除后<b class="dg">无法恢复</b>，当前使用的存档不受影响。`,
      onOk: () => {
        const D = dat(); D.points = D.points.filter(x => x !== pt);
        const r = mapRoot(), row = r && $(`[data-wd-pt="${pt.id}"]`, r);
        if (row) dropOut(row, () => { $(".wd-pts .wd-lw", r)?._paint?.(); $(".wd-pts .wd-lw", r)?.classList.toggle("is-none", !D.points.length); });
        if (r) swapKeyed($(".wd-pts .wd-hd", r), `<span class="wd-n" data-wd-k="pn">${D.points.length || ""}</span>`, 1);
      } });
  }
  function paintPoint(pt) { const r = mapRoot(), row = r && $(`[data-wd-pt="${pt.id}"]`, r); if (row) swapKeyed(row, ptRow(pt), 1); }

  function mapClick(e) {
    if (stepping) return;
    const D = dat(), F = D.flow;
    const cd = e.target.closest("[data-wd-cand]"); if (cd) { pickCand(+cd.dataset.wdCand); return; }
    const st = e.target.closest("[data-wd-set]");
    if (st) { const k = st.dataset.wdSet; F.sets[k] = !F.sets[k]; st.classList.toggle("on", F.sets[k]); st.setAttribute("aria-checked", F.sets[k]); return; }
    const tg = e.target.closest("[data-wd-tg]");
    if (tg) {
      const k = tg.dataset.wdTg; F[k] = !F[k]; tg.classList.toggle("on", F[k]); tg.setAttribute("aria-checked", F[k]);
      if (F[k]) { tg.classList.remove("bad"); const s = tg.closest(".wd-strip"); if (s && !$(".wd-tg.bad", s)) s.classList.remove("bad"); }
      return;
    }
    const a = e.target.closest("[data-wd-act]"); if (!a || a.getAttribute("aria-disabled") === "true") return;
    const act = a.dataset.wdAct, pt = a.closest("[data-wd-pt]") && D.points.find(x => x.id === a.closest("[data-wd-pt]").dataset.wdPt);
    if (act === "upx") { e.stopPropagation(); F.tok++; F.phase = "empty"; F.pct = 0; paintDz(); return; }
    if (act === "pick") { if (F.phase === "empty" || F.phase === "error") startUpload(); return; } // 草图里不真弹选文件的窗口：当作选了示例压缩包
    if (act === "browse") { const inp = $(".wd-path", mapRoot()); inp.value = D.src.path; $(".wd-prow", mapRoot()).classList.remove("bad"); startScan(D.src.path); return; }
    if (act === "scan") { startScan($(".wd-path", mapRoot()).value.trim()); return; }
    if (act === "retry") { retryHold(a); return; }
    if (act === "cperr") { flashDone(a, "copy", "已复制", "复制报错信息"); return; } // 草图里不真往剪贴板里写
    if (act === "ret" && pt) { askReturn(pt); return; }
    if (act === "pdel" && pt) { askDelPoint(pt); return; }
    if (act === "readme") { openReadme(); return; }
    if (act === "rdclose") { closeReadme(); return; }
    if (act === "back") { if (F.screen === "check") { F.screen = "pick"; F.ack = false; F.stop = false; } else { Object.assign(F, freshFlow()); } stepMap(-1); return; }
    if (act === "next") { F.screen = "check"; F.ack = false; F.stop = false; F.sets = {}; F.readme = false; stepMap(1); return; }
    if (act === "go") { tryGo(); return; }
    if (act === "cancel") { const T = F.task; if (T && !T.ended && T.step <= 1) { T.cancelled = true; T.ended = true; F.tok++; paintTask(); } return; } // 取消的结果写在下面那句里，过程记录不再重复
    if (act === "again") { F.task = null; F.screen = "check"; F.ack = false; F.stop = false; stepMap(-1); return; }
    if (act === "done") { finishTask(); return; }
  }
  window.DETAIL_TABS["world/map"] = {
    html(ctx) { C = ctx; closeAsk(true); stepping = false; dat(); return `<div class="wd-map">${mapInner()}</div>`; },
    mount(ctx) {
      C = ctx; const r = mapRoot(); if (!r) return;
      mapAfter(r);
      r.addEventListener("click", mapClick);
      r.addEventListener("keydown", e => {
        if (e.target.classList?.contains("wd-path") && e.key === "Enter") { e.preventDefault(); if (!stepping) startScan(e.target.value.trim()); }
        if (e.target.classList?.contains("wd-dz") && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); e.target.click(); }
      });
      r.addEventListener("input", e => { if (e.target.classList?.contains("wd-path")) $(".wd-prow", r).classList.remove("bad"); });
      // 拖文件进来：经过时虚线框换强调色，放下就当选了那个文件
      r.addEventListener("dragover", e => { const dz = e.target.closest?.(".wd-dz"); if (!dz || dzState() === "busy" || dzState() === "hold") return; e.preventDefault(); dz.classList.add("over"); });
      r.addEventListener("dragleave", e => { const dz = e.target.closest?.(".wd-dz"); if (dz && !dz.contains(e.relatedTarget)) dz.classList.remove("over"); });
      r.addEventListener("drop", e => { const dz = e.target.closest?.(".wd-dz"); if (!dz) return; e.preventDefault(); dz.classList.remove("over"); startUpload(e.dataTransfer?.files?.[0]?.name); });
    },
  };
  addEventListener("resize", () => { placeHL(false); const r = mapRoot(); if (r) fitWdTitles(r); });
})();
