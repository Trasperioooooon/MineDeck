# OpenFrp / SakuraFrp 账户与签到/购买协议调研笔记

> 调研日期：2026-03-24
> 用途：供 MineDeck `internal/tunnel` 账户接入实现（登录、签到含验证码、购买跳转、创建/导入隧道）。
> 约束：支付只跳转、不代收；禁止自动签到；平台凭据不以明文持久化。

相关已有实现参考：[`internal/tunnel/accounts.go`](../internal/tunnel/accounts.go)
OpenFrp 官方 OPENAPI 原文：[`docs/_openfrp_api_tmp.md`](./_openfrp_api_tmp.md)
Sakura 官方 OpenAPI：<https://api.natfrp.com/docs/>（源文件 `openapi.yaml`）

---

## 0. 红线（必须遵守）

1. **禁止自动签到**（OpenFrp OPENAPI 明文禁止；Sakura 签到也必须由用户主动触发）。
2. OpenFrp 使用 OPENAPI 时必须：
   - 在项目明显位置注明使用 OpenFrp OPENAPI；
   - 注明「此项目由社区开发，OpenFrp 官方不负责除节点问题以外的技术支持」；
   - 请求携带应用程序 UA（MineDeck 已用 `MineDeck/<version>`）。
3. **支付只跳转官方支付页 / 控制台，不代下单、不代收、不保存支付凭据。**
4. 验证码必须交给用户在 UI/内嵌 WebView 中完成；不得绕过 CaptchaSDK / SESSION。
5. Sakura `/user/sign` **不能**用访问密钥调用；不要把访问密钥误当成可签到凭证。

---

## 1. OpenFrp

### 1.1 基础

| 项 | 值 |
|---|---|
| API 基址 | `https://api.openfrp.net`（备用 `https://of-dev-api.bfsea.com`） |
| 控制台 | `https://console.openfrp.net` |
| 登录网关 | `https://access.openfrp.net` |
| 账户中心 (Natayark) | `https://account.naids.com` |
| 鉴权 | Header `Authorization: <明文授权>`（远程登录解密得到；可轮换） |
| Content-Type | `application/json` |
| 通用响应 | `{ "flag": bool, "msg": string, "data": ... }` |

控制台前端实际请求路径带 `/web` 前缀（如 `/web/frp/api/userSign`）。
**第三方 OPENAPI 文档路径不带 `/web`**（如 `/frp/api/userSign`）。MineDeck 现有实现走 OPENAPI 基址 + `/frp/api/*`，与文档一致。

### 1.2 登录（Remote Login）

官方 OPENAPI 写明「**现在不再提供基于账户密码的登录方式**」；公开稳定入口是 Remote Login / 粘贴 Authorization。
OpenFrp 不提供公开的账户密码登录 API。MineDeck 仅使用 Remote Login，不调用控制台或 Natayark 账户中心的私有账密接口。

#### A. Remote Login（默认连接方式，已实现）

1. 本地生成 curve25519 密钥对（nacl/box）。
2. `POST https://access.openfrp.net/argoAccess/requestLogin`
   Body: `{ "public_key": "<base64>" }`
   → `data.authorization_url`（必须是 `https://console.openfrp.net/...`）、`data.request_uuid`
3. 用户浏览器打开 `authorization_url` 授权。
4. 轮询 `GET/POST https://access.openfrp.net/argoAccess/pollLogin`（建议 1 次/5s，最长 5 分钟）
   Body: `{ "request_uuid": "..." }`
   Header 返回 `X-Request-Public-Key`，Body `data.authorization_data` 需用本地私钥解密得到 Authorization。
5. 注意 429 限流；uuid 5 分钟失效。

#### B. Authorization 粘贴登录

用户在控制台「第三方客户端安全登录」复制 Authorization，粘贴到客户端。该方式不使用账户密码。

### 1.3 用户信息 / 节点 / 隧道（已实现）

| 方法 | OPENAPI 路径 | Body | 说明 |
|---|---|---|---|
| POST | `/frp/api/getUserInfo` | `{}` | 用户名、组、流量、隧道配额、frpc token 等 |
| POST | `/frp/api/getUserProxies` | `{}` | 隧道列表 |
| POST | `/frp/api/getNodeList` | `{}` | 节点列表 |
| POST | `/frp/api/newProxy` | 创建参数 | 创建隧道 |
| POST | `/frp/api/removeProxy` 等 | … | 管理类 |

导入隧道：用 `getUserProxies` 结果 + 用户 token 写入本地 frpc。

### 1.4 签到 + 验证码（重点）

官方 OPENAPI §9：`POST /frp/api/userSign`，但写明「近期加入人机验证，内容暂不可供参考」，并禁止自动签到。

#### 验证码厂商 / SDK（从前端实锤）

| 项 | 值 |
|---|---|
| 厂商 | **Natayark / Naids CaptchaSDK**（**不是** Turnstile / hCaptcha / GeeTest） |
| SDK URL | `https://cv.naids.com/sdk/load.min.js` |
| AppId（site key 等价物） | `app_a287037f741b44eea03ced98452dc7cc` |
| 页面挂载 | console HTML：`<script defer data-appid="app_a287037f741b44eea03ced98452dc7cc" src="https://cv.naids.com/sdk/load.min.js">` |
| 全局对象 | `window.CaptchaSDK` |
| 拉起方式 | `const token = await CaptchaSDK.open()` |
| 取消 | `CaptchaSDK.isCancelled(err)` |
| SDK 默认 API | `https://cv.naids.com/api/open/captcha/generate?appId=...`、`.../verify` |

> 说明：这是 Naids 自研弹窗验证码（TAC），不是 Cloudflare Turnstile。实现时按「加载官方 SDK → 用户完成 → 拿 token」接入，不要自己拼 challenge。

#### 签到请求

```http
POST https://api.openfrp.net/frp/api/userSign
Authorization: <auth>
Content-Type: application/json
User-Agent: MineDeck/<version>

{"token":"<CaptchaSDK.open() 返回的字符串>"}
```

控制台等价路径：`POST /web/frp/api/userSign`，body 同样 `{ token }`。

#### 成功 / 失败（前端行为）

- 成功：`flag == true`，前端把 `data` 当成功文案展示，并刷新用户信息。
- 失败：`flag == false`，展示 `msg`；或 HTTP/网络错误。
- SDK 未加载：提示「验证码 SDK 尚未加载或出现错误」。
- 用户取消验证码：静默/不报错（`isCancelled`）。

脱敏示例：

```json
// 成功（示意）
{ "flag": true, "msg": "OK", "data": "签到成功，获得 xxx MB 流量" }

// 失败（示意）
{ "flag": false, "msg": "验证码无效或已过期", "data": null }
```

#### 签到状态查询

```http
POST https://api.openfrp.net/frp/api/getSignInfo
Authorization: <auth>
Content-Type: application/json

{}
```

前端使用字段（`data` 对象）：

| 字段 | 含义 |
|---|---|
| `signdate` | 上次签到日期（用于判断今日是否已签） |
| `totalsign` | 累计签到次数 |
| `totaltraffic` | 累计签到流量文案（如含 `GB`） |
| `sign_min` | 单次签到最小流量（MB） |

今日已签判断（控制台逻辑）：

```text
dayjs(signdate).diff(dayjs().startOf('day')) >= 0
```

快速签到深链（控制台）：`https://console.openfrp.net/dashboard?action=sign`
（打开后若未签会自动拉起 CaptchaSDK）。

#### MineDeck 实现建议（签到）

1. `CaptchaInfo` 返回：`vendor=naids`、`sdkUrl`、`appId`、可选 `openURL=dashboard?action=sign`。
2. 桌面端在 `sandbox="allow-scripts allow-forms"` 且没有 `allow-same-origin` 的 iframe 中运行 SDK；仅由用户完成验证并回传 token。父页面校验 iframe 的 `source`、opaque origin 和一次性 nonce，SDK 不接触主页面的 localStorage 或 API token。
3. 拿到 token 后由客户端调 `userSign`；**不要后台定时签到**。
4. 签到前后调 `getSignInfo` / `getUserInfo` 刷新 UI。
5. 失败引导：
   - SDK/验证码失败 → 打开 `https://console.openfrp.net/dashboard?action=sign`
   - 授权失效 → 重新 Remote Login
   - 未实名 → `https://console.openfrp.net/usercenter` 或 `https://account.naids.com/real_name`

### 1.5 购买 / 会员（无公开 OPENAPI）

公开 OPENAPI **没有**购买/支付接口。控制台存在非公开支付 API（**仅供理解，客户端不要代下单**）：

| 方法 | 控制台路径 | 作用 |
|---|---|---|
| GET | `/web/pay/getOrderList` | 返回商品列表（`data` 为 **JSON 字符串**，需再 `JSON.parse`） |
| POST | `/web/pay/preCreate` | Body `{ "pack_id": <套餐 id> }` → `data.oid` + `data.url`（支付页） |
| GET | `/web/pay/check/{oid}` | 查询是否已支付 |

商品页路由：`https://console.openfrp.net/premium`（「增值服务」：平台会员 / 加量包）。
支付成功后前端 `window.open(data.url)` 打开支付窗口；**MineDeck 只应跳转控制台，不调用 preCreate。**

#### 推荐 PurchaseOptions（只跳转）

| title | url | kind |
|---|---|---|
| 增值服务（会员 / 加量） | `https://console.openfrp.net/premium` | `premium` |
| 用户中心 | `https://console.openfrp.net/usercenter` | `account` |
| Natayark 账户中心 | `https://account.naids.com/dashboard` | `account` |
| 实名认证 | `https://account.naids.com/real_name` | `realname` |

登录态：用户浏览器已登录 console / account.naids.com 即可深链；MineDeck 的 Authorization **不能**直接注入到系统浏览器 cookie，因此购买引导以「打开官方页」为准。

---

## 2. SakuraFrp（natfrp）

### 2.1 基础

| 项 | 值 |
|---|---|
| API 基址 | `https://api.natfrp.com/v4` |
| 官方 OpenAPI 文档 | <https://api.natfrp.com/docs/> |
| 管理面板 | <https://www.natfrp.com/> |
| 访问密钥获取页 | <https://www.natfrp.com/user/profile>（文档写明：用户信息页复制访问密钥） |
| 帮助文档 | <https://doc.natfrp.com/> |
| 付款 FAQ | <https://doc.natfrp.com/faq/payment.html> |

#### 鉴权

公开 OpenAPI `securitySchemes`：

1. **UserToken**：`Authorization: Bearer <访问密钥>`
2. **UserTokenPost**：query `token=<访问密钥>`
3. **PanelSessionCookie**（`PHPSESSID`）在 YAML 里被**注释掉**，说明主站 SESSION 存在，但**不是公开 API 文档支持的方式**。

MineDeck 现有实现：Bearer 访问密钥。

### 2.1.1 网页账密登录（Nyatwork OpenID + GeeTest）

Sakura 注册/登录统一走 **Nyatwork OpenID**（`https://openid.13a.com`），不是 `api.natfrp.com` 公开账密接口。

| 项 | 值 |
|---|---|
| 登录页 | `https://openid.13a.com/login` |
| 面板入口 | `https://www.natfrp.com/user/`、`/user/login`（SPA；懒加载 `assets/login-DVoAj3JW.js`，重度混淆） |
| 验证码 | **GeeTest v3 bind**（不是 CaptchaSDK） |
| 会话 cookie | OpenID / 面板侧可见 `PHPSESSID`（OpenAPI 注释名；HttpOnly） |

**1) 拉起验证码配置**

```http
GET https://openid.13a.com/cgi/captcha?login
→ {"success":true,"message":{"gt":"<id>","challenge":"<challenge>","new_captcha":true,"offline":false}}
```

前端脚本：`/js/gt.js` + `/js/base.js`（`loadGeetest` / `callGeetest`）。
`initGeetest({ product:'bind', ...data.message })`；成功回调把字段展开进登录 body：

- `geetest_id`：取自当时的 `data.message.challenge`（注意：字段名是 `geetest_id`，值却是 challenge）
- 以及 `geetest.getValidate()` 展开字段（通常含 `geetest_challenge` / `geetest_validate` / `geetest_seccode`）

实测 captcha 响应里的 `gt` 示例（会轮换，勿写死进客户端逻辑以外的配置）：`f53512a605e97afa6985c0daee8c8179`。
登录页用 `loadGeetest(true)` → `/cgi/captcha?login`；注册等非登录场景走 `/cgi/captcha`（无 `?login`）。

**2) 账密登录**

```http
POST https://openid.13a.com/cgi/password/login
Content-Type: application/x-www-form-urlencoded

username=<用户名/邮箱/手机号>&password=<密码>&geetest_id=...&geetest_challenge=...&geetest_validate=...&geetest_seccode=...
```

登录页按钮逻辑（明文，未混淆）：校验非空 → `callGeetest(captcha => $.post('/cgi/password/login', { username, password, ...captcha }, ...))`。
另有 Passkey：`#webauthn` → `window.temp_webauthn()`（`/_temp_webauthn.js`）；本调研不依赖。

| 情况 | 响应 |
|---|---|
| 缺参 | `{"success":false,"message":"参数错误"}` |
| 假 GeeTest | `{"success":false,"message":"验证码已过期, 请刷新页面重新加载"}` |
| 成功（前端） | `{"success":true,...}` → `location.href = '/redirect'` |

注意：对 `openid.13a.com` 频繁探针会触发 Cloudflare **Access denied … try again in 1 hour**（`Retry-After`），属防护限流，不是接口形状变化。

**3) 登录后如何落到面板 / 访问密钥（当前静态结论）**

**已确认**

1. OpenID 登录页（`/login` HTML 明文，未混淆）成功后固定：
   `location.href = '/redirect'`（**不带**前端拼的 `service` / `return` query；回跳目标若存在，应来自登录前写入的 OpenID 会话 / cookie，而不是登录按钮再拼 URL）。
2. `GET https://openid.13a.com/redirect` **无有效会话**时：
   - `302 Location: /login`
   - `Set-Cookie: PHPSESSID=...; path=/; HttpOnly; SameSite=Lax`（长有效期）
   - 这只证明 OpenID 域会发 `PHPSESSID`，**不等于**已落到 `www.natfrp.com`。
3. `openid.natfrp.com` **DNS 解析失败**；登录页里曾有注释掉的 `https://openid.natfrp.com/js/backup-check.js`。实际可用主机是 `openid.13a.com`。
4. 面板 `www.natfrp.com` 是 Vite SPA：`/user/`、`/user/login`、`/sso`、`/openid`、`/auth`、`/login`、甚至 `/api/user/info` 在未登录探测下都可能直接回 **同一份 SPA HTML**（前端路由 / fallback），**不是**可脚本调用的 JSON 登录 API。前端路由入口本身**不会** 302 到 OpenID。
5. 路由表明文可见：`/user/login` → 懒加载 `assets/login-DVoAj3JW.js`；`/user/profile` → `assets/profile-BXCBPRYg.js`。`utils` 里 axios 有 `baseURL`（混淆）与 `401` / `401001` 拦截器（`401001` 会 `router` 跳转，疑似回登录页）。
6. **面板→OpenID 的真实入口是同源 CGI，不是前端拼 `openid.13a.com/login?...`：**
   - 生产：`GET https://www.natfrp.com/cgi/user/login`
     → `302 Location: https://openid.13a.com/oauth/authorize?response_type=code&client_id=PYoPownjQEAJYNbgwn7EZ2goa3kMaxQjBsuvTUmE`
     → 同时 `Set-Cookie: PHPSESSID=...; path=/; HttpOnly; SameSite=Lax`（面板域，长有效期）
     → Body 为「正在跳转…」+ 同 URL 的 `<a href>`（当前响应里 **未见** `redirect_uri` / `state` / `scope` 等额外 query；可能由 OpenID 侧按 `client_id` 预置，或藏在随后 OpenID 跳转里——**未跟进 openid 以免触发 Cloudflare**）。
   - 开发：`GET https://www.natfrp.com/cgi/user/login?dev`
     → 同形态，但 `client_id=NRdmEMNqDEOMVK7435I5ZLpsX3bWvUBtSri8GKlb`。
   - 登出：`GET https://www.natfrp.com/cgi/user/logout`
     → `302 Location: https://openid.13a.com/logout?continue=https%3A%2F%2Fwww.natfrp.com%2F` + 面板 `PHPSESSID`。
7. **OAuth 回调也落在同一 CGI：**
   - `GET https://www.natfrp.com/cgi/user/login?code=fake`（及带假 `state`）
     → **200** HTML：`内部错误, 请联系站点管理员 (invalid_grant)`（不是 SPA fallback）。
   - 这说明授权码换票入口就是 `/cgi/user/login`，协议是 **OAuth2 `response_type=code`**，不是 CAS 风格 `ticket=`。
   - 假 code 只能证明入口与错误形态；**成功换票后的 Location / Set-Cookie / 是否立刻给出访问密钥仍缺真实授权码**。
8. `login-DVoAj3JW.js` 经 Node 跑 obfuscator 字符串表旋转（`S` + `c.push(c.shift())`，校验和 `602220`）后还原出的前端逻辑（有证据）：
   - 未登录跳转目标：`/cgi/user/login`（文案「正在跳转到登录页」）；`?dev` 变体也在字符串表中。
   - `sessionStorage` 键名 **`login_redirect`**：若存在则 `router.push({ path: sessionStorage.login_redirect })` 后 `removeItem`；否则回 **`/user/`**。
   - 从当前 URL `params` 读到 `redirect` 时写入 `sessionStorage.login_redirect`，再 `location.href = ...`（目标字符串在混淆包装后解出为 `/cgi/user/login` 一侧的常量路径）。
   - 另有「正在完成登录」「success」「loggedin」等字符串；**未能**从本 chunk 解出明文 `openid.13a.com` / `ticket` / Bearer 换票 XHR 路径（OpenID URL 由服务端 302 给出）。
9. `api.natfrp.com` CORS：`Access-Control-Allow-Origin: *` 且 **`Access-Control-Allow-Credentials: false`**，只放行 `Authorization`。这意味着**浏览器里的面板前端不能把 `PHPSESSID` 当跨域 cookie 交给 `api.natfrp.com`**。面板登录后若要调 `/v4/*`，更可能是：
   - SSO 回跳后某次请求拿到 **访问密钥 / Bearer**，再以 `Authorization` 调用；或
   - 走同源代理（当前未见明文 `/api` JSON 端点）。
10. OpenAPI `UserInfo.token` = **访问密钥**。因此一旦能用 SESSION 或临时票换到可调 `/v4/user/info` 的凭据，响应里就能直接读出访问密钥，不必去刮 profile 页 DOM。
11. 启动器 WebUI（`launcher.natfrp.com` / `assets/index-DAaCXJy9.js`）**没有** OpenID/SSO 换票实现；只链到 `https://www.natfrp.com/user/`、`/user/profile`、`/tunnel/` 等。本地设置里的 `password` 是 **Launcher WebUI / 远程管理 token**，不是 Nyatwork 账户密码。
12. `doc.natfrp.com` / `natfrp/wiki` 账户 FAQ 只链到 OpenID 用户中心页（`/reset_password`、`/realname_query`、`/user#tab-4` 等），**不描述**面板 OAuth/`/cgi/user/login` 换票。GitHub `natfrp` 公开仓（wiki、launcher-*、auth-guest 等）未见可用的面板 SSO 源码说明。

**抓包对照用的固定模式（未登录）**

```http
GET https://www.natfrp.com/cgi/user/login
→ 302 Location: https://openid.13a.com/oauth/authorize?response_type=code&client_id=PYoPownjQEAJYNbgwn7EZ2goa3kMaxQjBsuvTUmE
→ Set-Cookie: PHPSESSID=...; path=/; HttpOnly; SameSite=Lax
```

用户态抓包时请从该 302 起保留后续 **OpenID authorize →（可能）/login → 账密+GeeTest → /redirect → 回 `www.natfrp.com/cgi/user/login?code=...`** 整链。

**补充探针（2026-11-11，未登录）**

1. 同源 CGI 除登录外未见公开 JSON 端点：
   `GET /cgi/user/info`、`/cgi/user/token`、`/cgi/user/profile`、`/cgi/token`、`/cgi/auth`、`/cgi/session`、`/cgi/oauth/callback` → **nginx 404**。
   因此「登录后用某个 `/cgi/*` 直接吐访问密钥」在静态探测下**不成立**；换票成功路径仍应落在 `/cgi/user/login?code=...` 本身或其之后的 **`api.natfrp.com` XHR**。
2. 面板 Vuex/持久化默认态含 `loggedin:!1`，并从 `localStorage[...]` 恢复整份 store（键名混淆）；**未见明文** `Bearer` / `access_token` 赋值。`Authorization` 明文出现处是 axios 的 Basic auth 辅助，不是面板 Bearer。
3. `401001` 响应拦截会清登录态并 `router` 跳转（目标路径混淆，先前还原为登录相关）。
4. MineDeck 半成品：`WebLogin("natfrp")` / `GET /api/tunnel-accounts/natfrp/weblogin` 打开 `/cgi/user/login`；UI「网页登录」按钮。**自动换票未做**。

**仍缺（阻断 ConnectSakuraPassword）**

| # | 缺口 | 需要的用户态抓包（DevTools Network，保留 Redirect） |
|---|---|---|
| A | ~~面板如何跳到 OpenID~~（静态已部分闭合） | **已确认**入口为 `/cgi/user/login` → OpenID `/oauth/authorize?response_type=code&client_id=...`。仍建议用户态核对：浏览器里 Location 是否另有 `redirect_uri`/`state`/`scope`，以及 authorize 之后是否再 302 到 `/login?...` |
| B | OpenID 成功后如何带着 `code` 回来 | 账密+GeeTest 成功后：`/redirect`（或等价）的 **302 Location** 是否回到 `https://www.natfrp.com/cgi/user/login?code=...`（及 `state` 等），以及 OpenID 域 Set-Cookie |
| C | 面板落票 | `GET/POST /cgi/user/login?code=...` **成功**时的最终状态：302 Location？面板 `PHPSESSID` 是否轮换？之后哪条 XHR 换到 Bearer / 访问密钥（URL、方法、头、JSON）；并看 `localStorage` 里 `loggedin` 相关对象是否出现 `token` |
| D | SESSION 与 API | 若拿到面板 `PHPSESSID`：用同一 cookie 调 `https://api.natfrp.com/v4/user/info` 与 `/user/sign`，看是否 200（公开 CORS 暗示浏览器跨域不行，但**原生客户端带 Cookie 不受 CORS 限制**，仍值得验证） |

没有 **成功的 B→C**（真实 `code` 换票）之前，**不能**把「OpenID 登录成功」稳定接到 MineDeck 可持久化的访问密钥 / 可用 SESSION。假 `code` 仅得到 `invalid_grant`，不能当成功路径。

**MineDeck 落地策略（当前采纳）**

- **不实现** `ConnectSakuraPassword`（保持显式错误；禁止假实现）。
- UI：**网页登录**打开 `/cgi/user/login`（OpenID），回来后**仍粘贴访问密钥**连接；签到继续 `needSession` + 官网引导。
- 后续若用户态抓包补齐 B→C，再做：回调 CGI 成功 → 取访问密钥落盘；密码不落盘。SESSION 仅作签到备选。

### 2.2 访问密钥可用接口（已实现 / OpenAPI 已列）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/user/info` | 用户信息（含 `sign`、`traffic`、`token`） |
| GET | `/user/data_plans` | 流量包 |
| GET | `/user/traffic_history` | 流量历史 |
| GET | `/nodes` | 节点 map |
| GET/POST | `/tunnels` | 列表 / 创建 |
| 其他 | `/tunnel/*`、`/domain/*`、`/computer/*`… | 见官方 openapi.yaml |

无效密钥探针响应示例：

```json
{"code":401,"msg":"访问密钥无效"}
```

#### `/user/info` 中的签到字段（可只读展示）

```yaml
sign:
  signed: bool      # 今日已签到
  last: date        # 上次签到日期
  days: int         # 连续签到天数
  traffic: number   # 签到累计流量
  config: [min,max] | false
traffic: [本日消耗, 总剩余]
```

### 2.3 签到 API（SESSION-only）

探针结果：

```http
GET|POST https://api.natfrp.com/v4/user/sign
→ {"code":401,"msg":"当前接口仅支持 SESSION 鉴权"}
```

结论：

1. **访问密钥不能签到。**
2. 需要浏览器主站 **SESSION**（OpenAPI 注释暗示 cookie 名 `PHPSESSID`）。
3. **公开 openapi.yaml 未收录 `/user/sign`**，请求体/成功响应字段**无官方文档**。
4. 前端 JS 高度混淆，路径字符串被拆碎；登录相关 chunk 出现 GeeTest 风格 `gt` 字段痕迹，**不能确认**签到是否也要二次验证码。

#### MineDeck 实现建议（Sakura 签到）

短期（推荐落地）：

1. `Sign`：若仅有访问密钥 → 返回
   `needSession=true`，`openURL=https://www.natfrp.com/user/`（或面板首页），文案提示「请在官网登录后手动签到」。
2. UI 仍可用 `/user/info` 的 `sign.signed` / `sign.days` 展示状态（签到后用户回来再刷新）。
3. **不要**尝试用访问密钥硬调 `/user/sign`。

中长期（可选，需产品评估）：

- 内嵌 WebView 登录官网拿 `PHPSESSID` 再调 `/user/sign`；但属于非公开鉴权，稳定性/合规风险高，且可能触发登录验证码（GeeTest 痕迹）。
- 在拿到真实抓包前，**不要假装已支持 API 签到**。

### 2.4 购买 / 商店

公开 OpenAPI **没有** shop/order/pay 路径。
管理面板前端路由（SPA，需登录 SESSION）：

| URL | 用途 |
|---|---|
| `https://www.natfrp.com/shop` | 商店 |
| `https://www.natfrp.com/tunnel/shop` | 隧道相关商店入口 |
| `https://www.natfrp.com/user/` | 用户中心 |
| `https://www.natfrp.com/user/profile` | 用户信息 / **访问密钥** |
| `https://www.natfrp.com/` | 面板首页（登录后可进订单/VIP） |

付款 FAQ 要点（实现文案可引用）：

- VIP 购买上限 6 个月；年费需看 FAQ；
- 支持的付款方式见官网 FAQ；
- 订单问题在面板订单列表处理；
- 退款策略：<https://www.natfrp.com/policy/refund>。

#### 推荐 PurchaseOptions

| title | url | kind |
|---|---|---|
| 商店 | `https://www.natfrp.com/shop` | `shop` |
| 隧道商店 | `https://www.natfrp.com/tunnel/shop` | `shop` |
| 用户中心 | `https://www.natfrp.com/user/` | `account` |
| 获取访问密钥 | `https://www.natfrp.com/user/profile` | `token` |
| 付款说明 | `https://doc.natfrp.com/faq/payment.html` | `docs` |

### 2.5 访问密钥获取说明（给用户看的）

官方启动器文档原话路径：

1. 打开 <https://www.natfrp.com/user/profile>（用户信息）
2. 复制 **访问密钥**
3. 粘贴到客户端登录

文档入口：<https://doc.natfrp.com/launcher/usage.html>

---

## 3. 端点速查表

### OpenFrp

| 场景 | 方法 | URL / 路径 | 鉴权 | Body |
|---|---|---|---|---|
| 申请登录 | POST | `https://access.openfrp.net/argoAccess/requestLogin` | 无 | `{public_key}` |
| 轮询登录 | GET/POST | `https://access.openfrp.net/argoAccess/pollLogin` | 无 | `{request_uuid}` |
| 用户信息 | POST | `/frp/api/getUserInfo` | Authorization | `{}` |
| 隧道列表 | POST | `/frp/api/getUserProxies` | Authorization | `{}` |
| 节点列表 | POST | `/frp/api/getNodeList` | Authorization | `{}` |
| 创建隧道 | POST | `/frp/api/newProxy` | Authorization | 隧道字段 |
| 签到状态 | POST | `/frp/api/getSignInfo` | Authorization | `{}` |
| 签到 | POST | `/frp/api/userSign` | Authorization | `{token}` |
| 增值服务页 | — | `https://console.openfrp.net/premium` | 浏览器登录态 | — |
| 控制台支付列表* | GET | `/web/pay/getOrderList` | Authorization | — |
| 控制台预下单* | POST | `/web/pay/preCreate` | Authorization | `{pack_id}` |
| 控制台查单* | GET | `/web/pay/check/{oid}` | Authorization | — |

\* 非公开 OPENAPI，**仅跳转 premium，不建议客户端调用。**

### SakuraFrp

| 场景 | 方法 | 路径 | 鉴权 | 备注 |
|---|---|---|---|---|
| 用户信息 | GET | `/user/info` | Bearer 访问密钥 | 含 `sign` |
| 节点 | GET | `/nodes` | Bearer | |
| 隧道列表 | GET | `/tunnels` | Bearer | |
| 创建隧道 | POST | `/tunnels` | Bearer | form-urlencoded（见 OpenAPI） |
| 流量包 | GET | `/user/data_plans` | Bearer | |
| 签到 | GET/POST | `/user/sign` | **仅 SESSION** | 访问密钥 → 401 |
| 商店 | — | `https://www.natfrp.com/shop` | 浏览器 SESSION | |
| 访问密钥页 | — | `https://www.natfrp.com/user/profile` | 浏览器 SESSION | |

---

## 4. 给实现的字段建议（Accounts API 形状）

### CaptchaInfo（OpenFrp 签到）

```json
{
  "provider": "openfrp",
  "vendor": "naids",
  "sdkUrl": "https://cv.naids.com/sdk/load.min.js",
  "appId": "app_a287037f741b44eea03ced98452dc7cc",
  "openURL": "https://console.openfrp.net/dashboard?action=sign"
}
```

CaptchaSDK 只用于用户主动签到。它在 `127.0.0.1` / `localhost` 主机名别名构成的跨 origin iframe 中运行，sandbox 只允许脚本、表单和该独立 origin 的存储；父页面校验消息的 source、精确 origin 和一次性 nonce 后才接受 token。iframe URL 不带主页面 API token，且无法访问主页面 localStorage。

### Sign 结果（示意）

```json
{
  "ok": true,
  "message": "签到成功…",
  "signedToday": true,
  "needCaptcha": false,
  "needSession": false,
  "openURL": ""
}
```

Sakura 无 SESSION 时：

```json
{
  "ok": false,
  "needSession": true,
  "message": "SakuraFrp 签到需要在官网登录会话中完成",
  "openURL": "https://www.natfrp.com/user/"
}
```

### PurchaseOptions（示意）

```json
[
  {"provider":"openfrp","title":"增值服务（会员/加量）","url":"https://console.openfrp.net/premium","kind":"premium"},
  {"provider":"natfrp","title":"商店","url":"https://www.natfrp.com/shop","kind":"shop"},
  {"provider":"natfrp","title":"获取访问密钥","url":"https://www.natfrp.com/user/profile","kind":"token"}
]
```

---

## 5. 已知缺口 / 后续可选抓包

| 缺口 | 影响 | 建议 |
|---|---|---|
| Sakura `/user/sign` 请求体与成功响应 | 无法 API 签到 | 用户浏览器登录后手动签；或后续 WebView+SESSION 抓包 |
| Sakura OpenID→面板 SESSION / 访问密钥自动换票 | 无法稳定账密直连 | 面板入口已确认：`/cgi/user/login` → OpenID `/oauth/authorize?response_type=code&client_id=PYoPownj...`；假 `code` 回同一 CGI 得 `invalid_grant`。仍缺真实授权成功后的 B/C（见 §2.1.1）。`api.natfrp.com` CORS 禁止 credentials |
| OpenFrp `/web/pay/*` 完整商品 schema | 不影响 | 只跳转 `/premium` |
| Naids Captcha 题型细节 | 不影响 | 一律走官方 SDK `open()`；仅用于用户主动签到 |

---

## 6. 材料来源

1. OpenFrp OPENAPI：`ZGIT-Network/OPENFRP-APIDOC` / [`docs/_openfrp_api_tmp.md`](./_openfrp_api_tmp.md)
2. OpenFrp 控制台：`https://console.openfrp.net/static/js/index-DuW8MELg.js` + `/web/oauth2/login`、`/web/oauth2/callback`
3. Naids CaptchaSDK：`https://cv.naids.com/sdk/load.min.js`（OpenFrp 签到）
4. Naids SDK：`https://cv.naids.com/sdk/load.min.js`
5. Sakura OpenAPI：`https://api.natfrp.com/docs/openapi.yaml` + `schemas/UserInfo.yaml`
6. Sakura / Nyatwork OpenID：`https://openid.13a.com/login`（HTML 明文成功跳 `/redirect`）、`/js/base.js`、`/cgi/captcha?login`、`/cgi/password/login`；无会话 `GET /redirect` → 302 `/login` + `PHPSESSID`
7. Sakura 面板静态资源：`assets/utils-BoVbbYnI.js`、`assets/login-DVoAj3JW.js`（字符串表旋转还原）、`assets/index.prod-B_SI5ipN.js`、`assets/profile-BXCBPRYg.js`；launcher `https://launcher.natfrp.com/assets/index-DAaCXJy9.js`（无 SSO）
8. Sakura 面板同源 CGI（无鉴权）：`GET /cgi/user/login` → 302 OpenID `/oauth/authorize?response_type=code&client_id=PYoPownjQEAJYNbgwn7EZ2goa3kMaxQjBsuvTUmE` + 面板 `PHPSESSID`；`?dev` 换另一 `client_id`；`/cgi/user/logout` → OpenID `/logout?continue=...`；`/cgi/user/login?code=fake` → 200 `invalid_grant`
9. Sakura 文档：`doc.natfrp.com/faq/account.html`（及 wiki `faq/account.md`）链到 `openid.13a.com` 重置/注销页，无 OAuth 换票说明；`faq/payment.html`、`launcher/usage.html`；OpenAPI `schemas/UserInfo.yaml`（`token`=访问密钥）
10. 无鉴权探针：`/v4/user/sign`、`/v4/user/info`（CORS：`Allow-Credentials: false`）、Natayark login/authorize、OpenFrp oauth callback 等

临时抓取缓存目录（可删）：`docs/_frp_research_tmp/`、`docs/_sakura_assets/`、`docs/_sakura_openid_base.js`、`docs/_sakura_utils.js`
