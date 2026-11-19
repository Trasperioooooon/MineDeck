# MineDeck 文档中心编写与维护指南

这份指南面向撰写、校对与更新文档的人。文档中心采用**纯 Markdown 驱动**：绝大多数新增与修订**只需增删或修改 `.md` 文件**，无需修改路由表、页面模板或客户端 JS。

## 一、日常新增或修改一篇文章

1. 复制 [文章模板 `_template.md`](./_template.md) 到对应语言和分类目录：
   - 中文：`website/docs-content/zh/<分类id>/<文章slug>.md`
   - 英文（可选）：`website/docs-content/en/<分类id>/<文章slug>.md`
2. 目录与 URL slug 统一使用**小写英文 kebab-case**（如 `tunnels-and-accounts.md` → `/docs/networking/tunnels-and-accounts/`）。
3. 把头部 `draft: true` 改为 `draft: false`（或删掉该行），运行构建即可自动出现在首页、左侧分类树、上一篇/下一篇翻页、搜索索引和 `sitemap.xml` 中。
4. 只有新增一整组一级大分类时，才需要编辑 [分类定义 `_meta.json`](./_meta.json)。

## 二、Front-Matter 字段规范

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `title` | string | **是** | 文章标题（会参与导航和像素字体子集裁切） |
| `description` | string | **是** | 一句话摘要（用于首页卡片、搜索摘要与 `<meta description>`） |
| `order` | number | **是** | 同分类内排序权重（推荐步长 `10, 20, 30...` 方便日后插空；同分类内不可冲突） |
| `since` | string | 否 | 适用起始版本，如 `v3.2.0`，正文页头自动展示「`v3.2.0 起`」徽标 |
| `beta` | boolean | 否 | 为 `true` 时显示「测试版功能」徽标，适合未进稳定版或尚待充分验证的能力 |
| `featured` | boolean | 否 | 为 `true` 时自动进入 `/docs/` 首页顶部「热门与新手必读」卡片区 |
| `tags` | string[] | 否 | 补充检索词、旧界面别名、常见英文报错关键字，提高搜索命中精度 |
| `updated` | string | 否 | `YYYY-MM-DD`；构建期优先取该文件在 Git 中的最后一次提交时间，取不到（如未提交本地预览）时回退此字段 |
| `aliases` | string[] | 否 | 文章改名或迁址后的旧路径跳转（如 `["/docs/management/cart/"]`），构建器自动生成轻量跳转页 |
| `draft` | boolean | 否 | 为 `true` 时构建期间跳过发布；**注意不要在正式文章里链接指向草稿**，构建会直接拦截报错 |

## 三、Markdown 扩展语法与界面术语约定

1. **界面按钮与路径写法**：涉及 MineDeck 界面元素一律以 **`internal/web/ui/` 实际显示文本为准**，按钮用 **`「按钮名」`**（构建器会自动加微弱胶囊边框突出可读性），层级入口写 `「设置 → 启动与内存」`。
2. **标准小标题与锚点**：正文小标题从 `##` 开始（勿在正文写 `#` 一级标题，一级标题由 `title` 生成）；如需长久稳定被外部或跨文引用，可在标题末尾附显式锚点 `## 常见问题 {#faq}`。
3. **提示框（Alerts）**：
   - `> [!NOTE]` / `> [!TIP]` / `> [!WARNING]` / `> [!CAUTION]` / `> [!IMPORTANT]`
4. **文档间互链**：统一写相对 `.md` 路径，例如 `[报错索引](../troubleshooting/error-banner-index.md#port-in-use)`。这样在 GitHub 直接浏览仓库和构建后网站中都能准确跳转，构建校验会自动检查目标文件存在性、非草稿状态及 `#anchor` 有效性。
5. **截图引用**：支持引用仓库 `docs/images/*.png`（如 `../../../../docs/images/01-servers.png`）或 `website/docs-content/assets/` 下的配图。若继续沿用 `v2.9.0-beta.1` 的历史截图，请在图注中诚实标注版本。

## 四、写作安全红线（构建期自动扫描拦截）

`website/lib/doc-checks.mjs` 会对所有中文与英文文章（含草稿）执行强检查，触发即阻断构建并报告 `文件:行号`：

- 严禁出现本机路径、系统用户名目录或项目绝对盘符路径；
- 严禁出现真实或形似实物的 API Key / GitHub Token / Bearer Token 字符串（示例代码请用 `<YOUR-TOKEN>` 等占位写法）；
- 严禁提及或链接仓库内部过程文档与工作目录（`docs/日志/`、`docs/调研/`、`docs/归档/`、`docs/测试与评审/`、`docs/设计与参考/`、`docs/待办.md`、`docs/现状.md`、`.claude/` 等）；
- 因全站产出检查禁止 HTML 文本出现 JS 故障词，正文中不要原样写出未加说明的敏感调试字面值。

## 五、中英文同步规则

- 英文译文放在 `website/docs-content/en/<分类>/<slug>.md`，slug 与分类必须与中文对应一致；
- 如果英文暂时缺某篇文章，**构建不会失败**（仅输出非阻断警告），并自动在 `/en/docs/<分类>/<slug>/` 生成友好的「暂未翻译」回退引导页与中文原文直达入口。

## 六、发布前本地自检命令

在 `website/` 目录下执行：

```bash
node build.mjs build               # 构建 dist/ 并运行全部校验与安全扫描
node build.mjs build --preview     # 生成可在 file:// 双击预览的 dist-preview/
node serve.mjs dist 8080           # 启动本地静态服务器体验搜索与跳转
node scripts/check-docs-release.mjs # 发版前核对 beta 标记、超前版本、缺译与旧截图清单
```
