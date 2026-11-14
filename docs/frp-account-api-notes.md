# OpenFrp / SakuraFrp 账户接入笔记

这份笔记记的是 MineDeck「平台账户」功能用到的两家接口、对方的使用要求，以及我们有意不做的事。实现见 [`internal/tunnel/accounts.go`](../internal/tunnel/accounts.go)，本地接口见 [API.md](API.md) 的「穿透平台账户」一节。

只依据两家公开的官方文档：

- OpenFrp OPENAPI：<https://github.com/ZGIT-Network/OPENFRP-APIDOC>
- SakuraFrp 开放 API 定义（v4）：<https://github.com/natfrp/api>（`openapi.yaml` 和 `schemas/`），在线版 <https://api.natfrp.com/docs/>

不在这两份文档里的接口一律不用，也不在仓库里记录对方网页前端的内部接口。

## 底线

1. 不自动签到，也不在 MineDeck 里代签。两家的签到都只打开官网页面，由用户自己完成。
2. 不收、不存平台账号密码。OpenFrp 走浏览器远程授权，樱花用户自己粘贴访问密钥。
3. 支付只打开官方页面，不代下单、不代收、不保存支付信息。
4. 平台凭据用 DPAPI 加密存在 `secrets.json`，任何响应都不回给前端；用到或改动凭据的操作只允许在本机做。

## OpenFrp

### 使用要求（官方 OPENAPI 原文的要点）

- 请求 OPENAPI 和相关资源站点都要带应用程序 UA，否则可能被防火墙拦截。MineDeck 带 `MineDeck/<版本>`。官方欢迎把 UA 提交到第三方开发者交流群，要不要提交由维护者决定。
- 要在项目的明显可见位置注明使用了 OpenFrp OPENAPI。MineDeck 写在 README 的「联机」一节和账户区的说明里。
- 要在项目有关位置加上「此项目由社区开发，OpenFrp 官方不负责除节点问题以外的技术支持」，并尽量附上技术支持方式（MineDeck 给的是本仓库的 Issues）。
- 不应设计任何自动签到功能，自动签到属于滥用、违反服务条款。
- 用 OPENAPI 做商业活动，必须有 OpenFrp Project 项目组的书面授权。

### 用到的接口

登录用文档第 1 节的远程授权（Remote Login），基址 `https://access.openfrp.net`：

- `POST /argoAccess/requestLogin`，请求体带本地生成的 X25519 公钥，返回授权页地址和 `request_uuid`。MineDeck 只接受 `https://console.openfrp.net` 下的授权页地址。
- `GET /argoAccess/pollLogin`，请求体带 `request_uuid`（文档就是 GET 加请求体的写法）。授权完成后返回加密的 `authorization_data`，服务端公钥在响应头 `X-Request-Public-Key`。文档要求轮询不快于 5 秒一次、最长 5 分钟，超出阈值会一律回 429；MineDeck 前端 5 秒一次，服务端另有 5 秒节流和 5 分钟过期。

账户和隧道，基址 `https://api.openfrp.net`，请求头 `Authorization` 带授权：

- `POST /frp/api/getUserInfo`：用户名、用户组、流量、隧道数和上限、实名状态，以及 frpc 用的用户密钥。
- `POST /frp/api/getUserProxies`：平台上的隧道列表。
- `POST /frp/api/getNodeList`：节点列表。
- `POST /frp/api/newProxy`：创建隧道。文档写明隧道名不支持中文，MineDeck 只放行英文字母、数字、下划线和连字符。

Authorization 最长 30 天有效。文档说请求任意接口时「有概率返回新值」，但没写放在哪个响应头；MineDeck 只在成功响应里、按响应头 `Authorization` 收新值，且新值要像样、期间账户没被断开或重新登录过。真实位置要用真实账号核实。

### 有意不做的

- 签到（文档第 9 节 `userSign`）：文档写明签到加了人机验证，内容暂不可供参考。MineDeck 的「签到」只打开 `https://console.openfrp.net/dashboard?action=sign`，控制台会自己拉起验证码。也不查签到状态。
- 账号密码登录：文档写明不再提供。
- 购买：公开 OPENAPI 没有支付接口，只打开 `https://console.openfrp.net/premium` 和账户中心。

## SakuraFrp（樱花穿透）

### 使用要求

- 鉴权用访问密钥，`Authorization: Bearer <访问密钥>`。访问密钥在官网「用户信息」页复制：<https://www.natfrp.com/user/profile>。`/user/info` 返回的 `token` 就是访问密钥，樱花的 frpc 也用它启动。
- 服务条款见 API 定义里的 `termsOfService`：<https://www.natfrp.com/policy/tos>。
- 公开游戏服务器的限制（API 定义里「创建隧道」的说明）：把连接方式发到论坛、服务器列表、QQ 大群，或在 B 站、抖音、小红书等公开平台宣传招人的，算公开游戏服务器，只能用公开服专用节点，判断条件是节点 `flag & 2048 != 0`，或节点名称含「多线」（「三线」不算）。违反会被封号且不退款。MineDeck 在创建对话框里让用户勾选「这是公开服务器」，勾选后只列符合条件的节点，后端也再查一次。

### 用到的接口

基址 `https://api.natfrp.com/v4`：

- `GET /user/info`：用户名、用户组、实名状态、隧道上限、流量 `[本日消耗, 总剩余]`、签到状态（今日是否已签、连续签到天数）、访问密钥。
- `GET /nodes`：节点表（以节点 ID 为键）。用到的 `flag` 位：`1<<2` 可以创建隧道（满载时为 0），`1<<5` 允许 UDP，`1<<11` 允许穿透公开游戏。
- `GET /tunnels`：隧道列表。`status` 为 0 是正常，2 是封禁。
- `POST /tunnels`：创建隧道。请求体可以是 JSON 或 form-urlencoded，MineDeck 发 JSON，字段 `name`、`type`、`node`、`local_ip`、`local_port`，填了公网端口时带 `remote`。成功回 201。

流量的单位 API 定义里没写明，界面暂时原样显示数字，要用真实账号核实后再加单位。

### 有意不做的

- 签到：公开 API 里没有签到接口，访问密钥签不了。「签到」只打开 <https://www.natfrp.com/user/>。
- 网页登录换取访问密钥：公开 API 不提供，不做。用户自己到「用户信息」页复制访问密钥。
- 购买：只打开商店、隧道套餐、用户中心和付款说明这几个官网页面。

## 还要用真实账号核实的

1. OpenFrp 远程授权：`requestLogin` 接受 URL-safe base64 的公钥；`pollLogin` 的 GET 请求体能被网关收到；`access.openfrp.net` 和 `api.openfrp.net` 对 MineDeck 的 Go 客户端不回 567（frpc 下载源曾经这样拦过）。
2. OpenFrp 换发 Authorization 时新值放在哪个响应头，旧值是否立即失效。
3. OpenFrp 概览字段的含义：流量单位、`used`、`proxies`、节点 `status`、`protocolSupport`、`fullyLoaded`。
4. 两家创建、导入隧道后 frpc 能启动，朋友能连进来；UDP 语音隧道的地址写进实例的 `voice_host`。
5. 樱花流量的单位，以及公开服节点的判断和官网显示是否一致。
