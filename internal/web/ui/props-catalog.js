// props-catalog.js — server.properties 全量中文目录（数据驱动）
// 来源：docs/UI与体验改进调研报告.md「附录 B · server.properties 全量中文目录」
//       （整理自 zh.minecraft.wiki / minecraft.wiki 的 Server.properties 页及官方快照日志）
// 适用范围：Java 版 1.12.2 ～ 26.2（含 26.3 快照已公布的变更）。
// 约定：
//   since  = 引入版本；removed = 移除版本；两者为空字符串表示全程可用 / 未移除。
//   type   = 'bool' | 'int' | 'str' | 'sel' | 'gamerule-bool'
//            gamerule-bool 表示该键在 1.21.9 已迁移为同名/对应游戏规则。
//   gamerule = 迁移目标游戏规则名（非迁移项为 null）。
//   restart  = 是否建议重启服务器才能可靠生效。
// 注意：本文件为 UI 表单的唯一数据源，数据驱动、请勿手改单项；
//       如需增删字段，应回到附录 B 同步后整体重生成。
window.PROPS_CATALOG = [
  // ===== 基础与玩法 =====
  { key: 'motd', group: '基础与玩法', type: 'str', def: 'A Minecraft Server', label: '服务器简介', desc: '显示在多人游戏服务器列表里的介绍文字。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'gamemode', group: '基础与玩法', type: 'sel', opts: ['survival', 'creative', 'adventure', 'spectator'], optNames: ['生存', '创造', '冒险', '旁观'], def: 'survival', label: '默认游戏模式', desc: '新玩家进服时的模式：生存/创造/冒险/旁观。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'force-gamemode', group: '基础与玩法', type: 'bool', def: 'false', label: '强制游戏模式', desc: '开启后玩家每次进服都被改回默认游戏模式。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'difficulty', group: '基础与玩法', type: 'sel', opts: ['peaceful', 'easy', 'normal', 'hard'], optNames: ['和平', '简单', '普通', '困难'], def: 'easy', label: '游戏难度', desc: '和平/简单/普通/困难；和平模式不刷怪、不掉饥饿。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'hardcore', group: '基础与玩法', type: 'bool', def: 'false', label: '极限模式', desc: '难度固定为困难。玩家死亡后进入旁观。旧版是死亡后直接封禁。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'pvp', group: '基础与玩法', type: 'gamerule-bool', def: 'true', label: '玩家互斗', desc: '关闭后玩家之间无法互相攻击造成伤害。1.21.9 起改用游戏规则 pvp。', since: '', removed: '1.21.9', gamerule: 'pvp', restart: false },
  { key: 'allow-flight', group: '基础与玩法', type: 'bool', def: 'false', label: '允许飞行', desc: '允许生存玩家用模组飞行；关闭时悬空玩家会被踢出。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'allow-nether', group: '基础与玩法', type: 'gamerule-bool', def: 'true', label: '允许下界', desc: '关闭后下界传送门失效，玩家无法前往下界。1.21.9 起改用游戏规则 allowEnteringNetherUsingPortals。', since: '', removed: '1.21.9', gamerule: 'allowEnteringNetherUsingPortals', restart: false },
  { key: 'enable-command-block', group: '基础与玩法', type: 'gamerule-bool', def: 'false', label: '命令方块', desc: '开启后命令方块才能工作。1.21.9 起改用游戏规则 enableCommandBlocks，默认值从 false 翻转为 true。', since: '', removed: '1.21.9', gamerule: 'enableCommandBlocks', restart: false },
  { key: 'spawn-monsters', group: '基础与玩法', type: 'gamerule-bool', def: 'true', label: '生成怪物', desc: '关闭后夜晚和黑暗处不再自然刷出怪物。1.21.9 起改用游戏规则 spawnMonsters。', since: '', removed: '1.21.9', gamerule: 'spawnMonsters', restart: false },
  { key: 'spawn-animals', group: '基础与玩法', type: 'bool', def: 'true', label: '生成动物', desc: '关闭后不再自然刷出牛、羊等友好动物。', since: '', removed: '1.21.2', gamerule: null, restart: false },
  { key: 'spawn-npcs', group: '基础与玩法', type: 'bool', def: 'true', label: '生成村民', desc: '关闭后村庄不再刷出村民。', since: '', removed: '1.21.2', gamerule: null, restart: false },
  { key: 'player-idle-timeout', group: '基础与玩法', type: 'int', def: '0', label: '挂机踢出', desc: '玩家发呆超过该分钟数被自动踢出，0 为永不踢。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'spawn-protection', group: '基础与玩法', type: 'int', def: '16', label: '出生点保护', desc: '出生点周围该半径内只有 OP 能挖放方块；无 OP 时不生效。', since: '', removed: '', gamerule: null, restart: false },

  // ===== 世界生成 =====
  { key: 'level-name', group: '世界生成', type: 'str', def: 'world', label: '世界名称', desc: '存档文件夹名；改名等于换一个新世界存档。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'level-seed', group: '世界生成', type: 'str', def: '', label: '世界种子', desc: '生成新世界用的种子，相同种子地形相同。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'level-type', group: '世界生成', type: 'sel', opts: ['minecraft:normal', 'minecraft:flat', 'minecraft:large_biomes', 'minecraft:amplified', 'minecraft:single_biome_surface'], optNames: ['普通', '超平坦', '巨型群系', '放大化', '单一群系'], optsByVer: { '1.12.2': 'DEFAULT / FLAT / LARGEBIOMES / AMPLIFIED（全大写）', '1.16-1.18': 'default / flat / largeBiomes / amplified（驼峰）', '1.19+': 'minecraft:normal / minecraft:flat / minecraft:large_biomes / minecraft:amplified / minecraft:single_biome_surface（命名空间 ID）' }, def: 'minecraft:normal', label: '世界类型', desc: '普通/超平坦/巨型群系/放大化等；只影响新生成区块。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'generator-settings', group: '世界生成', type: 'str', def: '{}', label: '生成器参数', desc: '配合世界类型自定义地形，如超平坦的层数配置。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'generate-structures', group: '世界生成', type: 'bool', def: 'true', label: '生成结构', desc: '关闭后新区块不再生成村庄、要塞等建筑。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'max-world-size', group: '世界生成', type: 'int', def: '29999984', label: '世界边界上限', desc: '限制世界边界可设置的最大半径（方块数）。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'initial-enabled-packs', group: '世界生成', type: 'str', def: 'vanilla', label: '启用数据包', desc: '建世界时自动启用的数据包，仅创建世界时生效。', since: '1.19.3', removed: '', gamerule: null, restart: true },
  { key: 'initial-disabled-packs', group: '世界生成', type: 'str', def: '', label: '禁用数据包', desc: '建世界时不自动启用的数据包列表。', since: '1.19.3', removed: '', gamerule: null, restart: true },
  { key: 'max-build-height', group: '世界生成', type: 'int', def: '256', label: '建筑高度上限', desc: '玩家可建造的最大高度，超过无法放置方块。', since: '', removed: '1.17', gamerule: null, restart: true },

  // ===== 玩家与权限 =====
  { key: 'max-players', group: '玩家与权限', type: 'int', def: '20', label: '最大人数', desc: '服务器最多同时在线的玩家数。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'online-mode', group: '玩家与权限', type: 'bool', def: 'true', label: '正版验证', desc: '开启之后，只有正版账号能进。关闭之后，任何人可以用任意名字进服。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'white-list', group: '玩家与权限', type: 'bool', def: 'false', label: '白名单开关', desc: '开启之后，只有名单里的玩家能进。OP 不受白名单限制。26.3 快照起，默认是开启。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'enforce-whitelist', group: '玩家与权限', type: 'bool', def: 'false', label: '强制白名单', desc: '重载白名单时，把已在线但不在名单里的玩家立即踢出。', since: '1.13', removed: '', gamerule: null, restart: false },
  { key: 'op-permission-level', group: '玩家与权限', type: 'int', def: '4', label: 'OP 权限等级', desc: '/op 授予的权限级别；4 最高，可用 /stop 等全部命令。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'function-permission-level', group: '玩家与权限', type: 'int', def: '2', label: '函数权限等级', desc: '数据包函数执行命令时使用的权限级别。', since: '1.14.4', removed: '', gamerule: null, restart: false },
  { key: 'enforce-secure-profile', group: '玩家与权限', type: 'bool', def: 'true', label: '聊天签名校验', desc: '要求玩家持 Mojang 签名密钥并签名聊天；离线模式建议关。', since: '1.19', removed: '', gamerule: null, restart: true },
  { key: 'enable-code-of-conduct', group: '玩家与权限', type: 'bool', def: 'false', label: '行为准则', desc: '开启后玩家须先同意服务器行为准则才能进入。', since: '1.21.9', removed: '', gamerule: null, restart: false },

  // ===== 性能 =====
  { key: 'view-distance', group: '性能', type: 'int', def: '10', label: '视距', desc: '服务器发给玩家的地图范围；越大越吃内存和带宽。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'simulation-distance', group: '性能', type: 'int', def: '10', label: '模拟距离', desc: '生物和作物会在这个范围内继续活动。范围越大，越占 CPU。', since: '1.18', removed: '', gamerule: null, restart: true },
  { key: 'entity-broadcast-range-percentage', group: '性能', type: 'int', def: '100', label: '实体可见距离', desc: '实体离玩家多远还发给客户端，按百分比算。调低能少用流量。调低之后，远处的实体会看不到。', since: '1.16', removed: '', gamerule: null, restart: true },
  { key: 'max-tick-time', group: '性能', type: 'int', def: '60000', label: '卡死超时', desc: '单刻卡顿超过该毫秒数即判定死机并强制关服。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'max-chained-neighbor-updates', group: '性能', type: 'int', def: '1000000', label: '连锁更新上限', desc: '限制红石等连锁方块更新数量，防恶意机器卡服。', since: '1.19', removed: '', gamerule: null, restart: true },
  { key: 'network-compression-threshold', group: '性能', type: 'int', def: '256', label: '网络压缩阈值', desc: '数据包超过该字节数才压缩；内网服可设 -1 省 CPU。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'rate-limit', group: '性能', type: 'int', def: '0', label: '发包限速', desc: '玩家发包超过该速率会被踢出，0 为不限制。', since: '1.16.2', removed: '', gamerule: null, restart: true },
  { key: 'sync-chunk-writes', group: '性能', type: 'bool', def: 'true', label: '同步写区块', desc: '同步写入磁盘，崩溃时更不容易丢档。关掉之后存档更快，但有丢档的风险。', since: '1.16', removed: '', gamerule: null, restart: true },
  { key: 'use-native-transport', group: '性能', type: 'bool', def: 'true', label: 'Linux 网络优化', desc: '仅 Linux 服务器生效的收发包优化，一般不用动。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'region-file-compression', group: '性能', type: 'sel', opts: ['deflate', 'lz4', 'none'], optNames: ['Deflate 压缩', 'LZ4 快速', '不压缩'], def: 'deflate', label: '区块压缩算法', desc: '存档压缩方式：lz4 读写更快但占盘更大，none 不压缩。', since: '1.20.5', removed: '', gamerule: null, restart: true },
  { key: 'pause-when-empty-seconds', group: '性能', type: 'int', def: '60', label: '空服暂停', desc: '无人在线超过该秒数后暂停世界运算，节省资源。', since: '1.21.2', removed: '', gamerule: null, restart: true },

  // ===== 网络与远程管理 =====
  { key: 'server-ip', group: '网络与远程管理', type: 'str', def: '', label: '监听 IP', desc: '绑定的本机 IP；一般留空即监听全部网卡。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'server-port', group: '网络与远程管理', type: 'int', def: '25565', label: '服务器端口', desc: '玩家连接用的 TCP 端口，需在防火墙/路由器放行。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'enable-status', group: '网络与远程管理', type: 'bool', def: 'true', label: '列表在线状态', desc: '关闭后服务器在列表中显示“离线”，也看不到简介和人数。', since: '1.16', removed: '', gamerule: null, restart: true },
  { key: 'hide-online-players', group: '网络与远程管理', type: 'bool', def: 'false', label: '隐藏在线名单', desc: '开启后状态查询不返回在线玩家名字。', since: '1.18', removed: '', gamerule: null, restart: false },
  { key: 'enable-query', group: '网络与远程管理', type: 'bool', def: 'false', label: 'Query 查询', desc: '开启 GameSpy4 协议，供面板/网站查询服务器信息。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'query.port', group: '网络与远程管理', type: 'int', def: '25565', label: 'Query 端口', desc: 'Query 查询使用的 UDP 端口。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'enable-rcon', group: '网络与远程管理', type: 'bool', def: 'false', label: 'RCON 开关', desc: '允许远程发送控制台命令，需配合密码使用。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'rcon.port', group: '网络与远程管理', type: 'int', def: '25575', label: 'RCON 端口', desc: 'RCON 远程控制使用的 TCP 端口。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'rcon.password', group: '网络与远程管理', type: 'str', def: '', label: 'RCON 密码', desc: '远程控制密码；留空则 RCON 不会启动。', since: '', removed: '', gamerule: null, restart: true },
  { key: 'broadcast-rcon-to-ops', group: '网络与远程管理', type: 'bool', def: 'true', label: 'RCON 回显 OP', desc: '通过 RCON 执行命令的结果会通知在线 OP。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'broadcast-console-to-ops', group: '网络与远程管理', type: 'bool', def: 'true', label: '后台回显 OP', desc: '后台控制台执行命令的结果会通知在线 OP。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'enable-jmx-monitoring', group: '网络与远程管理', type: 'bool', def: 'false', label: 'JMX 监控', desc: '暴露 JVM 性能指标（tick 耗时）供专业监控工具读取。', since: '1.16', removed: '', gamerule: null, restart: true },
  { key: 'log-ips', group: '网络与远程管理', type: 'bool', def: 'true', label: '记录玩家 IP', desc: '玩家进服时是否把其 IP 地址写入日志。', since: '1.20.2', removed: '', gamerule: null, restart: false },
  { key: 'accepts-transfers', group: '网络与远程管理', type: 'bool', def: 'false', label: '接受转移', desc: '允许其他服务器用转移包把玩家直接送进本服。', since: '1.20.5', removed: '', gamerule: null, restart: false },
  { key: 'status-heartbeat-interval', group: '网络与远程管理', type: 'int', def: '0', label: '状态心跳', desc: '管理协议定时推送服务器状态的间隔秒数。', since: '1.21.9', removed: '', gamerule: null, restart: false },
  { key: 'management-server-enabled', group: '网络与远程管理', type: 'bool', def: 'false', label: '管理协议开关', desc: '开启官方“服务器管理协议”，供面板实时管理服务器。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-host', group: '网络与远程管理', type: 'str', def: 'localhost', label: '管理协议地址', desc: '管理协议监听的主机地址。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-port', group: '网络与远程管理', type: 'int', def: '0', label: '管理协议端口', desc: '管理协议监听端口，0 为自动分配。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-secret', group: '网络与远程管理', type: 'str', def: '', label: '管理协议密钥', desc: '连接管理协议时的身份认证密钥；留空由服务器自动生成 40 位密钥。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-tls-enabled', group: '网络与远程管理', type: 'bool', def: 'true', label: '管理 TLS', desc: '管理协议连接是否使用 TLS 加密。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-tls-keystore', group: '网络与远程管理', type: 'str', def: '', label: 'TLS 证书库', desc: '自定义 TLS 证书库（keystore）文件路径。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-tls-keystore-password', group: '网络与远程管理', type: 'str', def: '', label: '证书库密码', desc: '上述证书库文件的密码。', since: '1.21.9', removed: '', gamerule: null, restart: true },
  { key: 'management-server-allowed-origins', group: '网络与远程管理', type: 'str', def: '', label: '管理跨域来源', desc: '允许连接管理协议的网页来源（Origin）列表。', since: '1.21.9', removed: '', gamerule: null, restart: true },

  // ===== 资源包 =====
  { key: 'resource-pack', group: '资源包', type: 'str', def: '', label: '资源包链接', desc: '玩家进服时提示下载的资源包直链地址。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'resource-pack-sha1', group: '资源包', type: 'str', def: '', label: '资源包校验', desc: '资源包 SHA-1 校验值，防下载损坏并利于客户端缓存。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'resource-pack-id', group: '资源包', type: 'str', def: '', label: '资源包 ID', desc: '资源包唯一标识，供客户端识别与管理。', since: '1.20.3', removed: '', gamerule: null, restart: false },
  { key: 'resource-pack-prompt', group: '资源包', type: 'str', def: '', label: '下载提示语', desc: '自定义资源包下载弹窗里显示的提示文字。', since: '1.17', removed: '', gamerule: null, restart: false },
  { key: 'require-resource-pack', group: '资源包', type: 'bool', def: 'false', label: '强制资源包', desc: '玩家拒绝下载资源包时直接被断开连接。', since: '1.17', removed: '', gamerule: null, restart: false },

  // ===== 安全与杂项 =====
  { key: 'prevent-proxy-connections', group: '安全与杂项', type: 'bool', def: 'false', label: '阻止代理连接', desc: '踢出疑似用 VPN/代理连接的玩家；仅正版验证下有效。', since: '', removed: '', gamerule: null, restart: false },
  { key: 'text-filtering-config', group: '安全与杂项', type: 'str', def: '', label: '聊天过滤配置', desc: 'Realms 内部使用的聊天过滤接口，普通服务器勿动。', since: '1.16.4', removed: '', gamerule: null, restart: false },
  { key: 'text-filtering-version', group: '安全与杂项', type: 'int', def: '0', label: '过滤配置版本', desc: '上述聊天过滤配置所用的格式版本号。', since: '1.21.2', removed: '', gamerule: null, restart: false },
  { key: 'chat-spam-threshold-seconds', group: '安全与杂项', type: 'int', def: '10', label: '聊天刷屏阈值', desc: '连续刷屏超过阈值会被踢出；调大更宽容。', since: '26.2', removed: '', gamerule: null, restart: false },
  { key: 'command-spam-threshold-seconds', group: '安全与杂项', type: 'int', def: '10', label: '命令刷屏阈值', desc: '同上，针对连续执行命令的刷屏判定。', since: '26.2', removed: '', gamerule: null, restart: false },
  { key: 'bug-report-link', group: '安全与杂项', type: 'str', def: '', label: '问题反馈链接', desc: '客户端“报告服务器问题”按钮指向的网址。', since: '1.21', removed: '', gamerule: null, restart: false },
  { key: 'snooper-enabled', group: '安全与杂项', type: 'bool', def: 'true', label: '数据上报', desc: '是否向 Mojang 发送匿名统计数据。', since: '', removed: '1.18', gamerule: null, restart: false },
  { key: 'previews-chat', group: '安全与杂项', type: 'bool', def: 'false', label: '聊天预览', desc: '发送前向服务器请求聊天样式预览。', since: '1.19', removed: '1.19.3', gamerule: null, restart: false }
];
