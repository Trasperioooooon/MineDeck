### 介绍页（GitHub Pages，2026-10-31）

源文件在 `site/`：中文首页 `index.html`、英文页 `en/index.html`、共用的 `style.css`、只含西文的 Geist 字体（中文用系统字体，不带 1.8 MB 的中文子集），以及 `robots.txt`、`sitemap.xml`、`llms.txt`。两页都有 canonical、hreflang、Open Graph 和 SoftwareApplication 结构化数据，没有 JavaScript。截图不在 `site/` 里存第二份：`.github/workflows/pages.yml` 组装 `_site` 时把 `docs/images/*.png`、`assets/social-preview.png` 复制进 `images/`，`assets/icon.ico` 做成 favicon。地址是 https://trasperioooooon.github.io/MineDeck/。

- 发布：推到 main 且改到 `site/**`、`docs/images/**`、社交预览图、图标或这个工作流时自动发布，也可以在 Actions 里手动运行。Pages 的 `github-pages` 环境默认只许 main 部署。
- 用户要做的一次性设置：Settings → Pages → Source 选「GitHub Actions」。想在合入 main 之前上线的，再到 Settings → Environments → github-pages 把工作分支加进允许部署的分支，然后在 Actions → Pages 里选工作分支手动运行；这时页脚指向 main 上测试文档的两个链接要等合入 main 才能打开。
- 本地核对：照工作流里的组装步骤生成 `_site`，挂在 `/MineDeck/` 前缀下打开，看两页在 1440 和 390 宽、浅色和深色下的样子，360 宽不横向滚动。
- 截图过时了就用 `node tools/promo/shoot.js` 重拍，再复制到 `docs/images/`。

