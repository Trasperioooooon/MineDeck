# MineDeck 官网

minedeck.cn 的源码。纯静态站，中英文各一套页面，构建后只有 HTML、CSS、JS、字体、图片和几份 JSON，不需要后端。

浏览器不请求 GitHub。版本、贡献者、Star 数、提交活动都在构建时取好，写进页面和 `data/` 下的 JSON。页面上指向 GitHub 的链接只是备用入口，统一标「↗」。

仓库里现有的 `site/`（GitHub Pages 介绍页，由 `.github/workflows/pages.yml` 发布）与本目录互不相干。

## 页面

| 路径 | 内容 |
| --- | --- |
| `/` | 首页：下载、版本状态、核心功能、近 26 周活动、贡献者、问题入口 |
| `/download/` | 稳定版与测试版下载、伙伴模组、安装步骤（SmartScreen 演示）、校验、系统要求、最近版本 |
| `/features/` | 完整功能清单，测试版功能单独标注 |
| `/releases/` | 更新日志：按大版本分组，可按稳定版 / 测试版筛选（`?channel=beta`） |
| `/releases/<tag>/` | 单个版本：文件表、SHA-256、发布说明全文和目录、上一版 / 下一版、版本跳转 |
| `/help/` | 快速开始、常用入口、FAQ、数据位置与卸载、已知边界 |
| `/feedback/` | 问题与建议表单：生成脱敏报告，复制、下载或带到 GitHub Issue |
| `/about/` | 项目简介、贡献者、52 周活动、每月发布数、许可证、致谢、声明 |
| `/404.html` | 中英双语 404 |
| `/feed.xml` | Atom 订阅，最多 20 个最近版本（`feedCount`） |
| `/data/*.json` | `releases.json`（全部版本）、`latest.json`（最新稳定版和测试版）、`stats.json` |

英文页在 `/en/` 下，结构相同。`/changelog/` 是旧地址，跳转到 `/releases/`。

路由集中在 `src/routes.mjs`：新增页面时在 `ROUTES` 里加一项，并在 `src/pages/` 写对应模块。导航和页脚也从这里读取。

## 目录

```
website/
├── build.mjs              构建入口（只用 Node 自带模块）
├── serve.mjs              本地静态服务器，行为接近 OSS 静态托管
├── site.config.json       仓库、站点地址、镜像模板、备案号、贡献者排除名单、反馈通道
├── lib/
│   ├── fetch.mjs          从 GitHub 拉数据写进 data/
│   ├── data.mjs           把 data/ 整理成页面用的模型（分组、上下版本、活动等）
│   ├── markdown.mjs       Release 说明渲染与标签白名单
│   └── checks.mjs         产物检查
├── data/                  构建输入（随仓库提交，可离线构建）
│   ├── releases.cache.json
│   ├── repo.json          Star 等仓库信息快照
│   ├── contributors.json
│   ├── activity.json      每日提交数
│   └── avatars/           贡献者头像（96px）
├── content/               文案：ui / features / help，各有 .zh.json 和 .en.json
├── src/
│   ├── tokens.json        设计 token（颜色、字号、间距、圆角、阴影、尺寸、动效、断点）
│   ├── routes.mjs         路由表、导航、页脚分组
│   ├── partials/          layout、nav、footer、ui 组件、release、heatmap、people、feedback-form、svg
│   ├── pages/             每个页面一个模块
│   ├── css/               base / components / pages / motion，构建时按 @layer 合并
│   ├── js/                core.js（全站）、releases.js、feedback/*.js
│   └── svg/               图标集和三幅插画（hero、smartscreen、broken-block）
├── fonts/                 Fusion Pixel 原始字体与许可证
└── ci/build-website.example.yml
```

构建还会读取仓库里的这些文件，不另存副本：`docs/images/*.png`（截图）、`internal/web/ui/img/logo-144.png`、`internal/web/ui/fonts/`（Geist、Geist Mono、Noto Sans SC 及许可证）、`assets/icon.ico`、`assets/social-preview.png`、仓库根目录的 `LICENSE`。

## 构建

需要 Node 18 或更高。可选：fonttools（`pip install fonttools brotli`）用于字体子集，Pillow 用于压缩头像。没有时也能构建，只是字体更大。

```bash
cd website
node build.mjs fetch            # 只拉数据，更新 data/
node build.mjs build            # 只用 data/ 构建，输出 dist/（不联网）
node build.mjs                  # 先 fetch 再 build
node build.mjs build --preview  # 可双击打开的本地预览，输出 dist-preview/
node serve.mjs dist 8080        # 本地看 dist/，含 404 行为
```

- `fetch` 调用 GitHub API：releases、仓库信息、贡献者及头像。设置 `GITHUB_TOKEN` 或 `GH_TOKEN` 可避免匿名限流。某一项失败时打印警告，继续用 `data/` 里已有的文件。
- 每日提交数优先用本地 `git log --no-merges origin/<默认分支>`，需要完整历史（CI 里 `fetch-depth: 0`）。拿不到时改用 API 的 `stats/commit_activity`。
- 国内网络拉不到 GitHub 时，直接 `build`。`data/` 随仓库提交；发版后在能访问 GitHub 的地方跑一次 `fetch` 并提交即可。
- `--preview` 只用于本地查看：站内链接写成 `xxx/index.html`，去掉字体 preload。上线用不带 `--preview` 的构建。
- `SOURCE_DATE_EPOCH` 固定构建时间，影响热力图截止日、「数据更新于」和 `generated_at`。

### 构建检查

构建失败的情况：

- `src/css/*.css` 里出现颜色字面量（`#fff`、`rgb()` 等）或硬阴影字面量。颜色只能用 `var(--color-*)` 等 token。
- CSS 花括号不配平。
- 产物 HTML/CSS/JS 中：`src`、`srcset`、CSS `url()`、`fetch()` 指向 GitHub；引用国外 CDN（Google Fonts、jsDelivr、unpkg、cdnjs）；指向 GitHub 的 `<a>` 没有经过 `extLink()`（缺 `data-ext`）。
- 页面文本里出现 `[object Object]`、`undefined`、`NaN`。

## 设计 token

`src/tokens.json` 是唯一的取值来源，构建时生成 `@layer tokens` 里的 CSS 变量：

- `palette`：原始色值，如 `--palette-grass-500`。
- `color`：语义色，写成 `{palette.grass-500}` 引用，如 `--color-accent`、`--color-beta`、`--color-heat-0..4`。改主题只需要改这里。
- 其余：`font`、`fs`、`lh`、`fw`、`space`、`radius`、`border`、`shadow`、`size`、`motion`、`z`、`breakpoint`，对应 `--fs-*`、`--space-*` 等。

CSS 按 `@layer tokens, base, components, pages, motion` 排列。断点用 900px 和 560px。

## 数据与内容

- **Releases**：去掉 draft，按时间倒序。`prerelease` 为测试版。测试版比最新稳定版新时，首页和下载页才显示测试版。2.x 中 exe 名为 `AutoMCHUB.exe` 的版本归入「AutoMCHUB 时期」。
- **Release 说明**：`lib/markdown.mjs` 渲染，原文 HTML 一律转义，输出过标签白名单。指向本仓库 Release 的链接改成站内 `/releases/<tag>/`，其他 GitHub 链接标为外部链接。小标题生成目录（多于 3 个时显示）。
- **贡献者**：`contributors.exclude` 中的账号不显示（默认排除 `claude`），`type` 为 Bot 的账号总是排除。`showHiddenNote` 控制是否注明「已隐藏 AI 编码工具和机器人账号」。
- **统计**：`stats.showStars` 显示构建时的 Star 快照并注明日期；`stats.showDownloads` 默认关闭。
- **时间**：按 `timezoneOffsetHours`（默认 8，北京时间）显示。
- **镜像链接**：按 `mirror.template` 生成，`{tag}`、`{file}` 会被替换。默认 `https://dl.minedeck.cn/releases/{tag}/{file}` 是占位；镜像上线前改成实际地址，或把 `mirror.enabled` 设为 false，主按钮改指 GitHub。
- **文案**：`content/*.json`。功能清单里带 `"channel": "beta"` 的条目显示测试版标记；v3.1.0 正式发布后去掉这个字段即可。
- **`/data/latest.json`**：给以后客户端检查更新用，字段含 tag、时间、SHA-256、大小、镜像地址和 GitHub 地址。现有客户端代码没有改动。

## 反馈页

表单在浏览器里生成报告，本身不上传任何东西。

- 报告格式和 GitHub Issue 模板一致（【类型】【标题】【版本】……）。生成前脱敏：Windows / macOS / Linux 用户目录里的用户名、`sk-` 和 GitHub / Slack / AWS 令牌、CurseForge API Key、Bearer、`key=`/`token=` 形式、长十六进制和 Base64 串、IPv4（保留 127.0.0.1，局域网保留前两段）。输入框里的原文不改。
- 日志可拖入 `.log` / `.txt`，过长时保留开头和结尾。草稿存在 `localStorage`（`md.feedback.draft`）。
- 支持 `?version=v3.1.0-beta.2`、`?kind=beta|bug|suggestion` 预填，版本页的「反馈这个版本的问题」会带上这些参数。
- 通道在 `site.config.json` 的 `feedback.channels` 里按顺序配置：
  - `copy`：复制报告。
  - `download`：下载为 `.md`。
  - `github`：打开预填的 `issues/new`，`labels` 为标签。URL 超过 `maxUrl` 时去掉日志，并把日志复制到剪贴板。
  - `endpoint`：默认关闭。开启后按 `minedeck.feedback/v1` 以 JSON POST 到 `url`，超时 `timeoutMs`。服务端返回 `{ ok, id?, issueUrl?, message? }`。协议见 ARCH.md 第 6.5 节。
  - `email`、`community`（如 QQ 群）：默认关闭，地址留空时不显示。
- 关掉 JavaScript 时表单不显示，改为提示和 GitHub Issues 链接。

## 动效

插画和图标是内联 SVG。动效只用 CSS，少量由 `core.js` 触发（进入视口、数字计数、SmartScreen 演示）。默认画面就是最终画面，没有 JS 时内容完整。系统设置了「减少动态效果」时全部关闭。

## 中英文

- 中文在 `/`，英文在 `/en/`，同一套模板、两份文案。每页带 `hreflang`（zh-CN、en，x-default 指向中文）。
- 点「中 / EN」时选择记在 `localStorage`（`md-lang`），之后打开另一语言的页面会跳到记住的语言，每个标签页只跳一次。首次访问不自动跳转。
- Release 说明只有中文，英文页显示原文并注明。

## 产物与部署到阿里云 OSS + CDN

构建和部署分开。CI 示例只构建、上传 artifact，不部署。

```
dist/
├── index.html、en/index.html
├── download/ features/ releases/ releases/<tag>/ help/ feedback/ about/   （每个目录一个 index.html，en/ 下相同）
├── changelog/index.html   跳转到 releases/
├── 404.html               中英双语，资源用根路径引用
├── feed.xml
├── data/releases.json、latest.json、stats.json
├── assets/                CSS、JS、字体、图片、头像，文件名带内容 hash；licenses/ 为字体许可证
├── favicon.ico、robots.txt、sitemap.xml
```

站内链接都是相对路径，可以放在根目录或子目录。只有 `404.html` 用根路径。

### 静态网站托管

- 默认首页 `index.html`；**开启子目录首页**，否则 `/releases/`、`/en/` 等不会返回各自的 `index.html`；默认 404 页 `404.html`。
- 子目录首页建议选「访问 `/releases` 时 302 跳到 `/releases/`」，避免相对路径错位。
- 必须用自定义域名访问。用 OSS 默认域名打开 HTML 会变成下载。
- 备案实例接入按阿里云当时的要求处理。

### 缓存头

| 路径 | 建议 Cache-Control |
| --- | --- |
| `assets/*`（带 hash） | `public, max-age=31536000, immutable` |
| `*.html`、`data/*.json`、`feed.xml` | `no-cache`，或 `max-age=300` |
| `robots.txt`、`sitemap.xml`、`favicon.ico` | `max-age=86400` |

- 先传 `assets/`，再传 HTML、JSON、`feed.xml`。旧 hash 文件等 CDN 上的 HTML 都更新后再清理。
- 每次发布后刷新 CDN 上的目录 `/`、`/en/`，以及 `/data/`、`/feed.xml`。新版本会新增 `/releases/<tag>/` 目录，旧版本页面的「下一版」链接也会变，所以版本页需要一起刷新（按目录刷新 `/releases/`、`/en/releases/` 最省事）。
- CDN 上为 html、css、js、json、xml、txt 开启 Gzip 或 Brotli。woff2 和 png 不用再压。
- `.woff2` 的 Content-Type 应为 `font/woff2`，`feed.xml` 为 `application/atom+xml` 或 `application/xml`。

### HTTPS

- CDN 配证书（可用阿里云免费证书，注意续签），开启 HTTP 跳 HTTPS，稳定后再考虑 HSTS。
- `siteUrl` 要与最终域名一致。canonical、hreflang、sitemap、分享图、feed 都用它生成。

### 镜像文件同步

- 页面只生成镜像链接，不同步文件。每次发版把 `MineDeck.exe`、伙伴模组 jar、`SHA256SUMS` 按模板路径放上去，并核对 SHA-256（页面和 `releases.json` 里有）。
- exe 建议 `Content-Disposition: attachment`，同一 tag 的文件可长缓存。
- 下载域名单独走 CDN 时注意流量费用，可开 Referer 防盗链（允许 minedeck.cn 和空 Referer）。
- 现在模板对所有版本都生成镜像链接。只同步较新版本时，需要调整模板或关闭镜像，避免失效链接。

### 备案号

- `beian.icp` 填 ICP 备案号，页脚链接到 https://beian.miit.gov.cn/ 。
- 公安备案拿到后填 `beian.psb` 和纯数字 `beian.psbCode`。公安备案图标没有放进仓库，需要时从官方渠道获取后放进 `src/` 并改 `src/partials/footer.mjs`。
- 留空时页脚显示「待填写」。填完重新构建并上传 HTML。

## 许可

- 代码随 MineDeck 仓库，MIT。
- 字体均为 SIL Open Font License 1.1：Fusion Pixel（上游 Ark Pixel、Cubic 11、Galmuri）、Geist、Geist Mono、Noto Sans SC。构建时裁成子集、自行托管，许可证在 `assets/licenses/`。
- 本站不使用 Mojang 或 Microsoft 的素材、Logo 和字体，页脚有无关联声明。
