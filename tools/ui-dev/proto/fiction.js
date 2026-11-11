// 界面原型用的虚构数据：12 种核心各一台服务器，每台有自己的玩家、备份和控制台输出。
// 全是编的，只为了让每个页面看起来像真的在用；名字、端口、时间都不对应任何真实的服务器。
"use strict";

const java = major => ({ javaSource: "portable", javaPath: `D:\\MineDeck\\runtimes\\java-${major}\\bin\\java.exe`, adopted: false });
const inst = o => ({
  kind: "game", xmsMb: 0, consoleEncoding: "auto", onlineCount: 0, uptimeSec: 0, maxPlayers: 20, extraJvm: [], motd: "", build: "",
  ...o, ...java(o.javaMajor), ...(o.adopted ? { adopted: true } : {}),
});

// 前四台和仿真后端自带的那四台同名（音乐共享、游戏内 AI、伙伴这些标签的假数据是按名字挂的）
const INSTANCES = [
  inst({ name: "生存服 · 朋友", core: "paper", mc: "1.21.4", build: "232", javaMajor: 21, xmxMb: 4096, port: 25565, status: "running", motd: "§a生存服 §7· §e一起来玩！", dir: "D:\\MineDeck\\servers\\server-20261020-2130", createdAt: "2026-10-20T21:30:00+08:00", onlineCount: 3, uptimeSec: 7980 }),
  inst({ name: "机械动力整合包", core: "forge", mc: "1.20.1", build: "47.3.0", javaMajor: 17, xmxMb: 8192, port: 25566, status: "stopped", motd: "§6Create 机械动力 §f整合包", dir: "D:\\MineDeck\\servers\\create-pack", createdAt: "2026-10-18T10:00:00+08:00", consoleEncoding: "gbk", extraJvm: ["-XX:+UseG1GC"] }),
  inst({ name: "原版快照测试", core: "vanilla", mc: "26.2", javaMajor: 25, xmxMb: 2048, port: 25567, status: "starting", motd: "A Minecraft Server", dir: "D:\\MineDeck\\servers\\vanilla-test", createdAt: "2026-10-25T09:00:00+08:00", uptimeSec: 12 }),
  inst({ name: "群组代理", core: "velocity", kind: "proxy", mc: "3.4.0", build: "450", javaMajor: 21, xmxMb: 1024, port: 25577, status: "stopped", dir: "D:\\MineDeck\\servers\\velocity", createdAt: "2026-10-10T09:00:00+08:00", maxPlayers: 0 }),
  inst({ name: "空岛生存", core: "purpur", mc: "1.21.4", build: "2416", javaMajor: 21, xmxMb: 4096, port: 25570, status: "running", motd: "§b空岛生存 §7| §f从一棵树开始", dir: "D:\\MineDeck\\servers\\skyblock", createdAt: "2026-11-02T19:20:00+08:00", onlineCount: 2, uptimeSec: 41520, maxPlayers: 30 }),
  inst({ name: "红石科技服", core: "leaves", mc: "1.21.4", build: "521", javaMajor: 21, xmxMb: 6144, port: 25571, status: "stopped", motd: "§c红石科技 §7· §f原版机器都能用", dir: "D:\\MineDeck\\servers\\redstone-lab", createdAt: "2026-10-28T14:05:00+08:00" }),
  inst({ name: "周末活动服", core: "folia", mc: "1.21.4", build: "13", javaMajor: 21, xmxMb: 12288, port: 25572, status: "running", motd: "§d周末活动 §7· §e周六晚 8 点开跑酷", dir: "E:\\MCServers\\weekend-event", createdAt: "2026-09-14T20:00:00+08:00", onlineCount: 37, uptimeSec: 18640, maxPlayers: 100 }),
  inst({ name: "轻量模组服", core: "fabric", mc: "1.21.1", build: "0.16.9", javaMajor: 21, xmxMb: 4096, port: 25573, status: "running", motd: "§f轻量模组 §7· §a钠锂磷一套", dir: "D:\\MineDeck\\servers\\fabric-lite", createdAt: "2026-11-05T22:40:00+08:00", onlineCount: 1, uptimeSec: 3120, maxPlayers: 10 }),
  inst({ name: "匠魂与暮色", core: "neoforge", mc: "1.21.1", build: "21.1.265", javaMajor: 21, xmxMb: 8192, port: 25574, status: "stopping", motd: "§6匠魂 §7+ §5暮色森林", dir: "E:\\MCServers\\tinkers-twilight", createdAt: "2026-10-30T13:15:00+08:00", uptimeSec: 9000, adopted: true }),
  inst({ name: "模组加插件", core: "mohist", kind: "hybrid", mc: "1.20.1", build: "923", javaMajor: 17, xmxMb: 8192, port: 25575, status: "stopped", motd: "§e模组和插件一起跑", dir: "D:\\MineDeck\\servers\\mohist-mix", createdAt: "2026-10-12T16:30:00+08:00" }),
  inst({ name: "Banner 试验场", core: "banner", kind: "hybrid", mc: "1.20.1", build: "792", javaMajor: 17, xmxMb: 4096, port: 25576, status: "stopped", motd: "Fabric 模组 + Bukkit 插件", dir: "D:\\MineDeck\\servers\\banner-lab", createdAt: "2026-10-06T11:00:00+08:00" }),
  inst({ name: "老群组入口", core: "waterfall", kind: "proxy", mc: "1.21", build: "590", javaMajor: 21, xmxMb: 512, port: 25578, status: "stopped", dir: "D:\\MineDeck\\servers\\waterfall", createdAt: "2026-08-21T09:30:00+08:00", maxPlayers: 0 }),
];

// ---- 每台服务器自己的玩家、备份 ----
const people = (online, whitelist, ops, banned) => ({ online, whitelist: whitelist.map(name => ({ name })), ops: ops.map(name => ({ name })), banned: banned.map(name => ({ name })) });
const PLAYERS = {
  "生存服 · 朋友": people(["Steve", "Alex", "Notch_CN"], ["Steve", "Alex", "Notch_CN", "xiaoming_2012", "MoMo酱"], ["Steve"], ["Griefer233"]),
  "机械动力整合包": people([], ["Steve", "齿轮爱好者", "brass_casing"], ["Steve"], []),
  "原版快照测试": people([], [], [], []),
  "空岛生存": people(["岛主阿青", "one_block_only"], ["岛主阿青", "one_block_only", "CobbleGen", "小鱼干"], ["岛主阿青"], []),
  "红石科技服": people([], ["活塞虫", "0tick", "观察者不观察", "TNT_Dupe"], ["活塞虫"], ["xray_user01"]),
  "周末活动服": people(["主持人老K", "跑酷第一名", "Mango", "不想上班", "luobo_", "Creeper_aw", "阿伟", "night_owl", "三级头"], ["主持人老K"], ["主持人老K", "场控小周"], ["外挂哥", "spam_bot_77", "刷屏的"]),
  "轻量模组服": people(["sodium_enjoyer"], ["sodium_enjoyer", "Lithium", "小白"], ["sodium_enjoyer"], []),
  "匠魂与暮色": people([], ["铸造大师", "暮色旅人", "Naga_Hunter"], ["铸造大师"], []),
  "模组加插件": people([], ["Steve", "插件党", "模组党"], ["Steve"], []),
  "Banner 试验场": people([], [], [], []),
};

const backups = (...rows) => rows.map(([file, sizeMb, time]) => ({ file, sizeMb, time }));
const BACKUPS = {
  "生存服 · 朋友": backups(["2026-11-11_04-00_auto.zip", 412.3, "2026-11-11 04:00"], ["2026-11-10_22-10_打龙前.zip", 405.8, "2026-11-10 22:10"], ["2026-11-10_04-00_auto.zip", 398.1, "2026-11-10 04:00"], ["2026-11-09_04-00_auto.zip", 391.6, "2026-11-09 04:00"]),
  "机械动力整合包": backups(["2026-11-08_23-41_拆主基地前.zip", 1286.4, "2026-11-08 23:41"], ["2026-11-02_04-00_auto.zip", 1190.2, "2026-11-02 04:00"]),
  "原版快照测试": [],
  "空岛生存": backups(["2026-11-11_04-00_auto.zip", 38.7, "2026-11-11 04:00"], ["2026-11-10_04-00_auto.zip", 37.9, "2026-11-10 04:00"]),
  "红石科技服": backups(["2026-11-06_21-05_存图机改造前.zip", 842.0, "2026-11-06 21:05"]),
  "周末活动服": backups(["2026-11-08_19-30_活动开始前.zip", 2210.5, "2026-11-08 19:30"], ["2026-11-01_19-30_活动开始前.zip", 2104.8, "2026-11-01 19:30"], ["2026-10-25_19-30_活动开始前.zip", 1987.3, "2026-10-25 19:30"]),
  "轻量模组服": backups(["2026-11-10_04-00_auto.zip", 156.2, "2026-11-10 04:00"]),
  "匠魂与暮色": backups(["2026-11-09_02-15_进暮色前.zip", 967.4, "2026-11-09 02:15"]),
  "模组加插件": [],
  "Banner 试验场": [],
};

// ---- 控制台：按核心的日志格式各写一段 ----
const paperLog = (ver, brand, who, extra = []) => [
  `[21:30:02 INFO]: Starting minecraft server version ${ver}`,
  "[21:30:02 INFO]: Loading properties",
  `[21:30:03 INFO]: This server is running ${brand} (Implementing API version ${ver}-R0.1-SNAPSHOT)`,
  "[21:30:04 WARN]: Using offline mode! Players will not be authenticated.",
  "[21:30:05 INFO]: Preparing level \"world\"",
  "[21:30:09 INFO]: Done (6.812s)! For help, type \"help\"",
  ...who.map((n, i) => `[21:${String(42 + i).padStart(2, "0")}:17 INFO]: ${n} joined the game`),
  ...extra,
];
const CONSOLES = {
  "生存服 · 朋友": paperLog("1.21.4", "Paper version 1.21.4-232-main@a1b2c3d", ["Steve", "Alex"], [
    "[21:44:30 INFO]: <Steve> 大家好！", "> say 欢迎来到生存服", "[21:44:35 INFO]: [Server] 欢迎来到生存服",
    "[21:50:12 ERROR]: Could not pass event PlayerInteractEvent to ExamplePlugin v1.0",
    "java.lang.NullPointerException: Cannot invoke \"String.length()\" because \"s\" is null", "\tat com.example.Plugin.onInteract(Plugin.java:42)",
    "[22:01:40 INFO]: Notch_CN joined the game",
  ]),
  "空岛生存": paperLog("1.21.4", "Purpur version 1.21.4-2416-ver/1.21.4@7f3c9aa", ["岛主阿青", "one_block_only"], [
    "[08:12:44 INFO]: [BentoBox] Loaded 2 addons: BSkyBlock, Level", "[08:20:03 INFO]: <岛主阿青> 刷石机修好了", "[08:21:19 INFO]: one_block_only has made the advancement [石器时代]",
  ]),
  "周末活动服": [
    "[19:58:01 INFO]: Starting minecraft server version 1.21.4",
    "[19:58:03 INFO]: This server is running Folia version 1.21.4-13-ver/1.21.4@51e9b2d (Implementing API version 1.21.4-R0.1-SNAPSHOT)",
    "[19:58:03 WARN]: Folia 是多线程核心，只有声明支持 Folia 的插件能加载",
    "[19:58:09 INFO]: Done (7.934s)! For help, type \"help\"",
    "[20:00:00 INFO]: [Server] 跑酷活动开始，第一名有奖！",
    "[20:00:12 INFO]: 跑酷第一名 joined the game", "[20:00:13 INFO]: Mango joined the game", "[20:00:15 INFO]: 不想上班 joined the game",
    "[20:03:41 INFO]: <主持人老K> 第二关的存档点在蓝色羊毛上", "[20:07:58 WARN]: Can't keep up! Is the server overloaded? Running 2104ms or 42 ticks behind",
    "[20:11:26 INFO]: 外挂哥 lost connection: Banned by an operator", "> tps", "[20:11:40 INFO]: TPS from last 5s, 1m, 5m, 15m: 19.98, 19.95, 19.97, 20.0",
  ],
  "轻量模组服": [
    "[22:40:11] [main/INFO]: Loading Minecraft 1.21.1 with Fabric Loader 0.16.9",
    "[22:40:12] [main/INFO]: Loading 14 mods:", "\t- fabric-api 0.110.0+1.21.1", "\t- lithium 0.13.1", "\t- ferritecore 7.0.2", "\t- c2me 0.3.0+alpha.0.320",
    "[22:40:19] [Server thread/INFO]: Starting minecraft server version 1.21.1",
    "[22:40:27] [Server thread/INFO]: Done (7.412s)! For help, type \"help\"",
    "[22:52:03] [Server thread/INFO]: sodium_enjoyer joined the game", "[22:52:40] [Server thread/INFO]: <sodium_enjoyer> 区块加载快多了",
  ],
  "匠魂与暮色": [
    "[13:15:02] [main/INFO] [cp.mo.mo.Launcher/MODLAUNCHER]: ModLauncher running: args [--launchTarget, forgeserver]",
    "[13:15:20] [main/INFO] [ne.ne.fm.lo.FMLLoader/CORE]: Found 86 mods, loading…",
    "[13:16:04] [Server thread/INFO] [minecraft/DedicatedServer]: Starting minecraft server version 1.21.1",
    "[13:16:31] [Server thread/INFO] [minecraft/DedicatedServer]: Done (27.118s)! For help, type \"help\"",
    "[15:40:55] [Server thread/INFO] [minecraft/MinecraftServer]: <铸造大师> 冶炼炉搭在出生点右边了",
    "> stop", "[15:45:00] [Server thread/INFO] [minecraft/MinecraftServer]: Stopping the server", "[15:45:01] [Server thread/INFO] [minecraft/MinecraftServer]: Saving worlds",
  ],
  "原版快照测试": [
    "[09:00:01] [ServerMain/INFO]: Environment: Environment[sessionHost=https://sessionserver.mojang.com, name=PROD]",
    "[09:00:03] [Server thread/INFO]: Starting minecraft server version 26.2", "[09:00:03] [Server thread/INFO]: Loading properties",
    "[09:00:04] [Server thread/INFO]: Preparing level \"world\"", "[09:00:06] [Worker-Main-3/INFO]: Preparing spawn area: 41%",
  ],
};

module.exports = { INSTANCES, PLAYERS, BACKUPS, CONSOLES };
