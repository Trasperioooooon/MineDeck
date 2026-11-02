# 内嵌字体生成工具（tools/fonts）

界面用的三款字体随 exe 内嵌（`internal/web/ui/fonts/`，经 `go:embed` 打包），运行时不发任何外部请求。
这里的脚本负责从固定版本的官方源文件生成这些子集。它是手动工具：产物直接入库，构建 MineDeck 时不需要运行它。

| 产物 | 字体 | 用途 | 体积 |
|---|---|---|---|
| `Geist-latin.woff2` | Geist 1.802，可变字重 400–600 | 西文正文、数字 | 约 23 KB |
| `GeistMono-latin.woff2` | Geist Mono 1.802，可变字重 400–600 | 控制台、路径、端口、版本号 | 约 27 KB |
| `NotoSansSC-gb2312.woff2` | Noto Sans SC，可变字重 400–600 | 中文、全角标点 | 约 1.8 MB |
| `fonts.css` | 上面三个字体的 `@font-face` | `index.html` 引用 | — |
| `OFL-Geist.txt`、`OFL-NotoSansSC.txt` | SIL Open Font License 1.1 原文 | 随字体分发必须附带 | — |

## 什么时候需要重新生成

- 界面文案里新出现了 GB2312 以外的字符（脚本会列出「界面用字中 GB2312 以外的字符」）；
- 要升级字体版本，或调整字符集、字重范围。

「界面用字」取自 `internal/web/ui` 下全部 js / html / css 文件的全文（`fonts/` 目录除外），代码注释也算在内：注释里写了特殊字符，它也会进子集，只是多几个字形，不影响显示。

改完界面文案后可以先跑一次 `--check`，它只比对、不写文件，产物需要更新时退出码为 1。

## 用法

需要 Python 3.9 或更新版本。在仓库根目录运行：

```bash
python -m pip install -r tools/fonts/requirements.txt   # fonttools 4.68.0 + brotli 1.2.1（固定版本，保证产物逐字节一致）
python tools/fonts/build_fonts.py                       # 下载 → 校验 → 子集化 → 写入 internal/web/ui/fonts/
python tools/fonts/build_fonts.py --check               # 只检查产物是否最新
```

第一次运行会下载约 23 MB 的源文件，缓存在 `tools/fonts/.cache/`（已被 git 忽略）。下载的文件都会核对校验和，对不上就报错退出。生成过程不改写字体里的时间戳，同样的输入得到逐字节相同的输出。

## 字体来源

| 字体 | 来源（固定版本） | 校验 |
|---|---|---|
| Geist、Geist Mono | Vercel 官方 npm 包 [`geist@1.8.2`](https://www.npmjs.com/package/geist)（仓库 [vercel/geist-font](https://github.com/vercel/geist-font)），取包内 `dist/fonts/geist-sans/Geist-Variable.ttf`、`dist/fonts/geist-mono/GeistMono-Variable.ttf` 与 `LICENSE.txt` | npm 的 sha512 integrity，外加两个 TTF 的 SHA-256 |
| Noto Sans SC | Google Fonts 官方仓库 [google/fonts](https://github.com/google/fonts) 提交 `ddf386ba1b9fbc6b2c818f1ddcd9148f54ced94b` 的 `ofl/notosanssc/NotoSansSC[wght].ttf` 与 `OFL.txt`（字形与上游 [notofonts/noto-cjk](https://github.com/notofonts/noto-cjk)、思源黑体同源） | SHA-256 |

具体的地址和校验和写在 `build_fonts.py` 开头。

## 字符集

**Noto Sans SC（中文）**，共 7456 个字符（随界面用字增减）：

- GB2312 全部 6763 个汉字（一级 3755 + 二级 3008），按 GB2312 区位表推导，不需要外部字表；
- GB2312 符号区（1–9 区）682 个：中文标点、全角字符、带圈与括号序号、日文假名、希腊与俄文字母、拼音与注音、制表符；
- 常用中文标点与符号，补上 Python 的 GB2312 映射与微软 / GBK 映射不一致的几个（`·` `—` `―` 等）；
- 界面文案用到的全部字符：扫描 `internal/web/ui/` 下的 `.js`、`.html`、`.css`。
- 不含 ASCII：西文和数字交给 Geist。

**Geist / Geist Mono（西文）**：Google Fonts「latin」范围（基本拉丁、拉丁补充、常用变音符号与通用标点）+ 箭头、`≤ ≥ ≈ ≠ ∞ −` 等常用符号 + 控制台用的制表符与方块元素（U+2500–259F，只有 Geist Mono 有）。中文语境的破折号、弯引号、省略号（U+2014、U+2018–201F、U+2026）刻意不放进西文字体，交给 Noto Sans SC 渲染成全角样式。`fonts.css` 里西文字体的 `unicode-range` 按子集实际的字形表生成。

**回退**：子集之外的字（服务器名、MOTD、玩家名里的生僻字等）由字体栈后面的系统字体逐字补上（Windows 上是 Microsoft YaHei UI / 微软雅黑、Segoe UI），不会显示成方框。

## 字重

界面只用 400（正文）、500（按钮、标签、导航）、600（标题、当前项）三档，所以三款可变字体的字重轴都限制在 400–600。请求 700（如 `<b>`、MOTD 的 `§l`）时按可变范围取到 600，不做伪粗体（`font-synthesis-weight: none`）。

## 许可

三款字体都是 [SIL Open Font License 1.1](https://openfontlicense.org/)：允许随任何许可的软件捆绑再分发，允许子集化（属于修改）；不得单独售卖字体。随软件分发时附带版权声明与许可原文——子集保留了字体 name 表里的版权（ID 0）、许可（ID 13）与许可链接（ID 14），许可原文放在 `internal/web/ui/fonts/OFL-*.txt`。Noto Sans SC 的保留字体名是「Source」，子集的字体名里不含这个词，无需改名；Geist 没有保留字体名。
