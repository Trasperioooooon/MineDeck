# 离线服和正版 UUID

2026-11-21 核对。这不是已经做进程序的功能，是给「离线服要不要同步正版 UUID」用的设计。含义和迁移还有没定的地方，所以没有改 `online-mode`，也没有搬玩家数据。

## 用户原话

「有没有离线服的正版uuid同步？我换个版本还得找皮肤加载器，而且上回忘关正版验证的版本没有tureuuid，我还得手动改文件，虽然这个功能基本不用，但要用的时候如果有就很方便。」

「tureuuid」对得上公开项目 [YuWan-030/TrueUUID](https://github.com/YuWan-030/TrueUUID)（Modrinth 项目页 [trueuuid](https://modrinth.com/project/trueuuid)）。2026-11-20 读到的 README 写的是：服务器 `online-mode=false`，用模组或插件在登录时向玩家自己的认证服务核对会话，核对过的玩家保留正版 UUID、名字和皮肤。Forge、Fabric、NeoForge 要客户端和服务端装同一份、对得上加载器和 Minecraft 版本的 jar。Spigot / Paper 1.20.1 是服务端插件，还要 ProtocolLib。README 还写了玩家数据迁移。这份仓库没有跑过这些 jar。

## 几件不同的事

| 说法 | 实际是什么 | MineDeck 现在做什么 |
| --- | --- | --- |
| 正版验证 | `server.properties` 的 `online-mode`。开着时只有正版会话能进，UUID 是正版的。 | 新建时按表单写，默认关。导入现成服务端时，包里已有这项就沿用，不改。 |
| 离线 UUID | `online-mode=false` 时，游戏按玩家名算出 UUID（不是正版账号那一个）。 | 不另算、不改玩家文件。换地图的说明里已经写了：离线 UUID 接不上正版那份存档。 |
| 正版 UUID 查询 | 用公开接口按名字查 Mojang 的 UUID。查到不等于这个人证明了自己是这个账号。 | 不做。不做的原因是查到就可以拿去改玩家文件，那是冒充。 |
| 皮肤 | 客户端从正版会话或皮肤站拿皮肤。离线模式默认没有。 | 工具箱能做离线皮肤资源包。那和 UUID 无关，也不代替皮肤模组。 |
| 代理转发 | Velocity / Waterfall 的 `online-mode` 和转发模式决定后端看到的 UUID。Velocity 官方 `velocity.toml` 写明 `player-info-forwarding-mode = "none"` 时后端拿到的是离线 UUID。 | 接管时读端口，不改 `velocity.toml`。 |
| TrueUUID | 第三方模组或插件。要关正版验证，还要客户端配合，并可能迁移玩家数据。 | 不安装，不改 `online-mode`，不迁移。 |

关掉正版验证不会让玩家文件变成正版 UUID，也不会带上皮肤。上次忘了关正版验证，玩家文件用的是正版 UUID；换到没有 TrueUUID 的版本再开离线，名字对得上、文件名对不上，所以要手改文件。这和「MineDeck 再写一次 online-mode」不是同一件事。

## 没定、所以没做的

1. 用户要的是 TrueUUID 这一份模组，还是只想在界面上对照离线 UUID 和正版 UUID、自己决定改不改文件。
2. 迁移会动 `playerdata`、`stats`、`advancements`，有的服还有权限插件按 UUID 记账。自动改这些会把背包和权限一起换人。
3. TrueUUID 的 jar 必须和加载器、Minecraft 版本一致。自动下载一份未在本仓库核对过的 jar，不在这次的范围内。
4. 代理后面的服，UUID 由代理决定。只改后端的 `online-mode` 不够，改错了会让人进错存档。

界面上只加了一句说明：正版验证默认关闭，关掉不会同步正版 UUID，也不会带上皮肤。全部配置里 `online-mode` 的说明同样写了 TrueUUID 要另装、MineDeck 不会自动安装或搬存档。
