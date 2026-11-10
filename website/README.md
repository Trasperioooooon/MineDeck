# MineDeck 官网

minedeck.cn 的源码：首页和更新日志墙，中英文各一套。纯静态，构建后只有 HTML、CSS、JS、字体、图片和一份 JSON，不需要后端。

浏览器不请求 GitHub。版本数据在构建时从 GitHub Releases 取出，写进页面和 `data/releases.json`。

和仓库里现有的 `site/`（GitHub Pages 介绍页，由 `.github/workflows/pages.yml` 发布）互不相干，两边可以同时存在。

## 目录

```
website/
├── build.mjs               构建脚本（只用 Node 自带模块）
├── serve.mjs               本地预览用的静态服务器
├── site.config.json        仓库名、站点地址、镜像链接模板、备案号
├── lib/markdown.mjs        Release 说明的 Markdown 渲染与标签白名单
├── src/
│   ├── templates.mjs       页面模板（中英文共用）
│   ├── i18n/zh.json        中文文案（含功能介绍）
│   ├── i18n/en.json        英文文案
│   ├── style.css
│   └── app.js              筛选、展开、语言记忆等增强；关掉 JS 时页面照常可用
├── fonts/src/              Fusion Pixel 12px 简体中文原始字体（构建时裁成子集）
├── fonts/licenses/         Fusion Pixel 及其上游字体的许可证
├── data/releases.cache.json  Releases 缓存，供离线构建
└── ci/build-website.example.yml  GitHub Actions 示例（只构建、上传 artifact）
```

构建还会读取仓库里的这些文件，不另存副本：

- `docs/images/*.png`：截图
- `internal/web/ui/img/logo-144.png`：Logo
- `internal/web/ui/fonts/`：Geist、Geist Mono、Noto Sans SC（GB2312 子集）及许可证
- `assets/icon.ico`：favicon
- `assets/social-preview.png`：分享预览图

## 构建

需要 Node 18 或更高。字体子集化需要 fonttools（`pip install fonttools brotli`）。没装 fonttools 时也能构建，但会直接复制完整字体：Noto Sans SC 约 1.8 MB，Fusion Pixel 约 0.7 MB。

```bash
cd website
node build.mjs                       # 联网拉取 Releases，更新缓存，输出到 dist/
node build.mjs --offline             # 不联网，用 data/releases.cache.json
node build.mjs --offline --preview   # 生成可双击打开的本地预览，输出到 dist-preview/
node serve.mjs dist 8080             # 本地起一个静态服务器看 dist/
```

- 联网构建会调用 `https://api.github.com/repos/<repo>/releases`。设置环境变量 `GITHUB_TOKEN` 或 `GH_TOKEN` 可以避免匿名请求被限流（每小时 60 次）。
- 国内网络拉不到 GitHub 时，用 `--offline`。缓存文件随仓库提交，发版后在能访问 GitHub 的地方重新跑一次联网构建并提交缓存即可。
- `--preview` 只为本地查看：站内链接写成 `xxx/index.html`，并去掉字体 preload（`file://` 下会被浏览器拦截）。上线请用不带 `--preview` 的构建。
- `SOURCE_DATE_EPOCH` 可以固定构建时间。这个时间影响发布日历的截止日期和 `generated_at`。

## 产物

`dist/` 的内容：

```
dist/
├── index.html                中文首页
├── changelog/index.html      中文更新日志
├── en/index.html             英文首页
├── en/changelog/index.html   英文更新日志
├── 404.html                  中英双语 404，资源用根路径引用
├── data/releases.json        版本数据（含 latest_stable、latest_beta）
├── assets/                   CSS、JS、字体、图片，文件名带内容 hash
│   └── licenses/             字体许可证
├── favicon.ico
├── robots.txt
└── sitemap.xml
```

页面里的链接都是相对路径，所以站点可以放在域名根目录，也可以放在子目录。只有 `404.html` 用根路径（`/assets/...`），因为它会在任意路径下被返回。

## 数据与内容

- **Releases**：去掉 draft，按发布时间倒序。`prerelease` 为 true 的是测试版。`latest_stable` 是最新的非预发布版，`latest_beta` 是最新的预发布版。首页只有在测试版比稳定版新时，才显示测试版链接和「测试版里的新功能」。
- **Release 说明**：用 `lib/markdown.mjs` 渲染。只支持 Release 里实际用到的语法：标题、列表、引用、表格、代码、链接。原文里的 HTML 一律转义。输出再过一遍标签白名单，只留 p、ul、ol、li、a（http/https）、table、code、blockquote 等；链接加 `rel="noopener nofollow"`。
- **2.x 版本**：exe 文件名是 `AutoMCHUB.exe` 的版本，归入「AutoMCHUB 时期」分组。
- **时间**：按北京时间（`timezoneOffsetHours: 8`）显示。
- **镜像链接**：按 `site.config.json` 的 `mirror.template` 生成，`{tag}`、`{file}` 会被替换。默认值 `https://dl.minedeck.cn/releases/{tag}/{file}` 是占位，镜像上线前要改成实际地址；或把 `mirror.enabled` 设为 false，主下载按钮改指 GitHub。
- **功能介绍**：写在 `src/i18n/zh.json`、`en.json` 里，不从代码自动生成。v3.1.0 正式发布后，要把 `beta.items` 里的条目挪进 `features.items`；之后有新测试版，再改 `beta.introduced`。
- **截图**：`shots.items` 里列出要用的图和说明。目前的截图拍摄于 v2.9.0-beta.1。

## 部署到阿里云 OSS + CDN

构建和部署分开。CI 示例只构建、上传 artifact。下面是手动或另写流水线时要注意的事项。

### 静态网站托管

- 在 Bucket 的「静态页面」里设置：
  - 默认首页：`index.html`
  - 子目录首页：开启。否则 `/changelog/`、`/en/` 不会返回各自的 `index.html`。
  - 默认 404 页：`404.html`
- 子目录首页有一个选项：访问 `/changelog`（不带斜杠）时，是 302 跳到 `/changelog/`，还是返回 index。建议选跳转，避免相对路径错位。
- 必须绑定自定义域名访问。通过 OSS 默认域名（`*.aliyuncs.com`）打开 HTML 时，浏览器会下载文件，而不是显示页面。
- 导入备案实例时，按阿里云当时的要求把域名接入对应实例或 CDN，再开放访问。

### 缓存头

| 路径 | 建议 Cache-Control |
| --- | --- |
| `assets/*`（带 hash） | `public, max-age=31536000, immutable` |
| `*.html`、`data/releases.json` | `no-cache`，或 `max-age=300` |
| `robots.txt`、`sitemap.xml`、`favicon.ico` | `max-age=86400` |

上传和刷新：

- 用 ossutil 上传时，可以分两次 `cp`，分别带 `--meta "Cache-Control:..."`。参数写法以所用 ossutil 版本的文档为准。
- 先传 `assets/`，再传 HTML。旧的 hash 文件先别删，等 CDN 上的 HTML 都更新了再清理。
- CDN 上为 HTML 和 JSON 设短缓存，或者每次发布后刷新 `/`、`/changelog/`、`/en/`、`/en/changelog/`、`/data/releases.json`。
- 在 CDN 上为 html、css、js、json、xml、txt 开启 Gzip 或 Brotli。woff2 和 png 已经压缩过，不用再压。
- `.woff2` 的 Content-Type 应为 `font/woff2`。ossutil 一般能按扩展名识别；识别不对时手动指定。

### HTTPS

- 在 CDN 配置证书，可以用阿里云免费证书，注意到期续签。
- 开启 HTTP 跳转 HTTPS。确认稳定后再考虑 HSTS。
- `site.config.json` 的 `siteUrl` 要与最终域名一致（含 https）。canonical、hreflang、sitemap、分享图片都用它生成。

### 镜像文件同步

- 页面只生成镜像链接，不负责同步文件。每次发版都要把 Release 附件按模板路径放到镜像上：`MineDeck.exe`、伙伴模组 jar、`SHA256SUMS`。默认模板对应 `releases/{tag}/{file}`。
- 上传后核对 SHA-256。页面和 `releases.json` 里有 GitHub 给出的 sha256。
- exe 建议设置 `Content-Disposition: attachment`，以及较长的缓存时间（同一 tag 的文件不会变）。
- 下载域名单独走 CDN 时，注意流量费用，可以开启 Referer 防盗链（允许 minedeck.cn 和空 Referer）。
- 只同步较新的版本时，把旧版本的 `mirror_url` 关掉，或把 `mirror.enabled` 设为 false，避免页面出现失效链接。现在模板对所有版本都会生成镜像链接。

### 备案号

- 在 `site.config.json` 的 `beian` 里填 `icp`，例如「京ICP备XXXXXXXX号-1」，页脚会链接到 https://beian.miit.gov.cn/ 。
- 公安备案需要在网站开通后按要求办理。拿到编号后填 `psb`，以及用于查询链接的纯数字 `psbCode`，页脚会链接到全国互联网安全管理服务平台。
- 公安备案图标没有放进仓库。需要显示时，从官方渠道获取后放进 `src/`，并改模板。
- 填完重新构建、上传 HTML。

## 中英文

- 中文在 `/`，英文在 `/en/`，用同一套模板和两份文案生成。每页都带 `hreflang`（zh-CN、en、x-default 指向中文）。
- 点右上角的「中 / EN」时，选择会记在 `localStorage`（键 `md-lang`）。之后打开另一种语言的页面，会自动跳到记住的语言，每个标签页只跳一次。
- 首次访问不做自动跳转。
- Release 说明只有中文。英文页显示中文原文，并在页面上说明。

## 许可

- 代码随 MineDeck 仓库，MIT。
- 字体均为 SIL Open Font License 1.1：Fusion Pixel（上游 Ark Pixel、Cubic 11、Galmuri）、Geist、Geist Mono、Noto Sans SC。构建时裁成子集，许可证随产物发布在 `assets/licenses/`。
- 本站不使用 Mojang 或 Microsoft 的素材、Logo 和字体。页脚有无关联声明。
