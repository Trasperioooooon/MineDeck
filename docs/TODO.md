# MineDeck 体验改进 TODO（全量实施清单）

> 依据：`docs/UI与体验改进调研报告.md`（2026-07-12）。用户决策：**全部改动一起做、做到最好、不计时间**。
> 目标版本：v2.1.0。本清单为唯一施工依据，每项完成即勾选；「验收」列不通过不算完成。
> 横切纪律（每个阶段收尾必做）：`go vet` + 构建通过；UI 改动出**双主题截图并阅图确认**；行为改动做真机 E2E；按阶段提交 commit（延续中文提交信息风格）。
> 勾选同步（2026-10-27，按当前代码逐项核对阶段 0–8）：[x] = 功能与验收在代码中成立，命名 / 机制与原文不同处写在条目末尾；仍为 [ ] 的条目注明「部分完成 / 未做」与缺口。

## 总览

| 阶段 | 主题 | 任务数 | 依赖 |
|---|---|---|---|
| 0 | 基建（供后续复用） | 4 | — |
| 1 | Bug 速修 | 3 | 0.1 |
| 2 | 暖沙浅色主题 | 3 | — |
| 3 | 设置页信息架构 | 6 | 0.3 |
| 4 | server.properties 全量目录 | 6 | 0.2 / 0.3 / 0.4 |
| 5 | 存储位置（三层） | 7 | 3（存储分区落位） |
| 6 | 创建向导升级 | 5 | 5.1（路径显示） |
| 7 | 实例列表与详情 | 7 | 0.1 |
| 8 | 联机页打磨 | 2 | — |
| 9 | 体验打磨 | 5 | 3 |
| 10 | 托盘与自启 | 2 | — |
| 11 | 收尾与回归 | 6 | 全部 |

---

## 阶段 0 · 基建

- [x] **0.1 `/api/app` 扩展**：返回 `port`（实际监听端口）与 `availRamMb`（`GlobalMemoryStatusEx.AvailPhys`，app.go 加 `AvailRAMMB()`）。
  验收：改用 `-port 0` 类随机端口启动，接口返回真实端口；availRamMb 与任务管理器可用内存一致（±5%）。—— main.go 把 net.Listen 的实际端口注入 web.New(mgr, tun, port)，handleApp 下发 port；app.AvailRAMMB() 读 GlobalMemoryStatusEx.AvailPhys（调用失败返回 0，前端退化为总量）。
- [x] **0.2 前端版本比较工具 `verGte(mc, ref)`**：支持 `1.x.y` 与年份版号 `26.x` 混排（26.x 恒大于 1.x），供配置目录版本感知使用。
  验收：单测样例 `1.12.2<1.18`、`1.21.9≥1.19`、`26.1>1.21.9`、`26.2≥26.2` 全对。—— app.js verParts / verCmp / verGte / verLt 逐段按整数比较（26.x 首段 26 恒大于 1.x）；四个样例按实现推演均成立，仓库无 JS 单测框架，未落测试文件。
- [ ] **0.3 行式控件组件（CSS + 渲染函数）**：统一「标题 + 副标题 + 右侧控件」行模式；含可展开行（收起显示当前值摘要）与行内嵌编辑器三种形态（HMCL 三件套）。
  验收：同一渲染函数支撑设置页与常用设置两处，双主题截图行高/对齐一致。—— 部分完成：app.js rowControl() 已实现（普通行 + 可展开摘要 / 行内编辑器合一形态）并配 .row-item 系 CSS，但全仓没有调用点——设置页各分区手写 .row-item 片段（switchRow / storageRow 等），常用设置用 .prop-item 卡片，验收「同一渲染函数支撑两处」不成立。
- [ ] **0.4 `PROPS_CATALOG` 数据文件**：按报告附录 B 录入约 60 项 × 7 组（键/类型/枚举值+中文名/默认值/中文名称/说明/引入版本/移除版本/是否需重启/gamerule 迁移目标）。level-type 按版本给三套枚举（1.12.2 大写 / 1.16–1.18 驼峰 / 1.19+ 命名空间 ID）。
  验收：脚本比对目录键名与附录 B 表格零遗漏；每项均有中文说明。—— 部分完成：props-catalog.js 共 78 项 × 7 组，与附录 B 的 78 行键名逐一比对一致，字段齐全且每项有中文说明；未做：level-type 的 1.12.2 / 1.16–1.18 两套枚举只以说明文字存于 optsByVer（UI 未读取），1.16–1.18 实例的下拉仍给 1.19+ 命名空间值。

## 阶段 1 · Bug 速修

- [x] **1.1 局域网指引端口修复**：设置页访问地址与防火墙命令改用 `location.port`（兜底 `/api/app` 的 port），消灭硬编码 27333。
  验收：`-port 28000` 启动，设置页显示的地址与 netsh 命令均为 28000。—— setRemote 以 info.port（/api/app 实际端口）优先、location.port 兜底生成访问地址与 netsh 命令；字面量 27333 只剩最后兜底与 -port 默认值。
- [x] **1.2 放开 16GB 内存上限**：app.js 三处（向导/导入/JVM 页）`min(16384, ram-2048)` → `ram-2048`；>16G 时提示「过大的堆可能增加 GC 停顿」。
  验收：本机（按实际内存）滑条可拉到 `总内存-2G`。—— 向导 / 导入 / JVM 三处上限均为 max(2048, 总内存 − 2048)；renderMemoryControl 在 >16G 时提示「过大的堆可能增加 GC 停顿」（与超配警示二选一显示）。
- [x] **1.3 内存滑条升级为「滑条 + 精确输入框 + 实时状态条」**（HMCL 式）：状态条显示「已使用 X GB / 总内存 Y GB ｜ 分配 Z GB」，分配 > 可用时红字警示「设备当前仅 X GB 可用」。三处（向导/导入/JVM）统一组件。
  验收：拖动滑条数字框联动、直接输入数字滑条联动；开大内存程序后状态条数值下降、超配警示出现。—— renderMemoryControl()（滑条 + 精确输入框 + .mem-bar 已用 / 分配条 + 超配红字）三处共用；文案为「已用 X GB · 本服分配 Z GB / 共 Y GB」「超过当前可用 X GB…」；可用内存取渲染时的 /api/app 快照（不轮询，重进页面刷新）。

## 阶段 2 · 暖沙浅色主题

- [x] **2.1 替换浅色 token 为「暖沙中性」**（报告附录 A 方案 B）：bg `#efece2` / surface `#fbfaf5` / surface-2 `#f5f3ea` / sunken `#e6e2d6` / border `rgba(62,55,32,.13/.26)` / text `#221f18` / muted `#625d4e`；绿、红强调色不动。
  验收：附录 A 对比度表逐项复测达标（text/surface ≥15、muted/surface ≥6）。—— app.css 浅色 8 个 token 与上表一致；绿色系后于 v2.3（b628cce）为过 AA 加深（accent-text #256a19、accent-solid #367d24），红色未动；按值计算 text/surface 15.7、muted/surface 6.2。
- [x] **2.2 `renderMotd` 浅色适配复验**：基色 `#565e57` 与 ×0.55 压暗系数在暖底上重校（必要时微调），16 色样例逐色确认可读。
  验收：含全部 §0-f 色码的 MOTD 在浅色列表页截图上逐色可辨。—— 浅色基色改为暖灰 #5b5647（7.0:1），压暗系数仍为 ×0.55；按值计算压暗后 §a / §b / §e / §f 在暖白卡上为 4.1 / 3.8 / 3.4 / 3.2:1（可辨但低于 AA），其余 ≥5.4:1。
- [x] **2.3 双主题全页截图验收**：实例列表/向导四步/详情全部标签/联机/设置，浅色深色各一轮，阅图确认无踩色、无发丝边框消失。
  验收：截图存档，肉眼过全部页面。—— 无代码产物；双主题逐页阅图以 11.3 终验记录为准，截图未入库。

## 阶段 3 · 设置页信息架构

- [x] **3.1 页内二级导航骨架**：左侧子导航（200px 级）+ 右侧内容区，子路由 `#/settings/{section}`，刷新/直链可定位到分区。
  验收：六个分区可点击切换、当前项高亮、直链进入正确分区。—— navigate() 解析 #/settings/{section} 交 renderSettings()，壳常驻只换 #set-body；SETTINGS_SECTIONS 现为 7 个分区（阶段 10 增「启动与托盘」），未知分区回落「下载与网络」；左导航 190px、sticky，≤720px 横排。
- [x] **3.2 六分区迁移**：下载与网络（下载源三选 + CF Key）/ 存储位置（占位，5.2 落位）/ Java / 远程访问 / 通知与集成（Webhook）/ 更新与关于。全部改为 0.3 行式控件。
  验收：原设置页每一个功能点在新结构中可用且行为不变（逐项过一遍保存/生效）。—— 下载与网络 / 存储位置（阶段 5 落位）/ Java / 远程访问 / 通知与集成 / 更新与关于各有 set* 渲染函数，原功能点均在；行式化不完整：存储、Java、启动与托盘与版本行是 .row-item 片段，下载源为 radio-card，远程访问、Webhook、更新仓库仍是普通行内表单，均未经 rowControl()。
- [ ] **3.3 「更新与关于」页**：版本号、检查更新、更新仓库（默认值固化进程序，设置仅覆盖）、「启动时静默检查更新」开关、开源许可、「基于 OpenFrp OPENAPI」署名升为正式条目、GitHub 链接、退出程序（红色危险区独立分隔）。
  验收：静默检查开关持久化并在下次启动生效（日志可见）；署名可见性满足 OpenFrp 接入条款。—— 部分完成：版本徽标、更新仓库 + 检查更新 / 立即更新、「启动时静默检查更新」开关（Config.CheckUpdateOnStart，main.go 启动后台检查）、内置默认仓库 app.DefaultUpdateRepo（loadConfig 回填）、OpenFrp OPENAPI 署名与 MIT 说明（关于段落）、退出程序 .set-danger 危险区；未做：GitHub 链接；静默检查结果只走 log，全仓无 log.SetOutput，-H windowsgui 发布版看不到，验收「日志可见」不成立。
- [x] **3.4 主题三态**：浅色 / 深色 / 跟随系统（HUD 按钮循环三态或设置内选择，`amh_theme=auto` 时监听 `prefers-color-scheme` 变化实时切换）。
  验收：切系统深浅色，auto 模式下界面实时跟随；显式选择后不再跟随。—— index.html 预置脚本（URL ?theme > amh_theme > auto）+ HUD 主题键循环 浅色 → 深色 → 跟随系统；auto 时监听 prefers-color-scheme 变化实时切换。小瑕疵：实例卡 MOTD 颜色在渲染时按主题烘焙，切主题后要等数据变化或切页才重绘。
- [x] **3.5 Java 分区行式化**：便携/扫描列表改为行式（版本标题 + 路径副标题 + 右侧操作），扫描/手动添加保留。
  验收：与 3.2 同标准。—— setJava 扫描结果为 .row-item（「Java N」+ 版本徽标 + mono 路径），扫描 / 手动添加保留；与原文差异：行内没有右侧操作，便携运行时仍是徽标条而非行。
- [x] **3.6 远程访问分区完善**：访问地址二维码（纯前端生成，报告路线图承诺项）、密码修改、防火墙命令（端口来自 1.1）。
  验收：手机扫码可直达登录页。—— internal/web/ui/qrcode.js 手写精简编码器（字节模式 + 纠错 M、版本 1–40、8 种掩码罚分择优），SVG 输出用双主题恒定的 --qr-dark / --qr-light（深模块浅底）+ 4 模块静区；setRemote 开启局域网后显示二维码卡，多 IP 胶囊切换（家用网段优先）；密码修改、防火墙命令沿用。同时修复 boot() 遇 401 整页覆盖登录遮罩（手机打开只见「无法连接」）：api() 的 401 错误带 needLogin 标记，boot() 见标记即保留密码框。验证：与 Nayuki qrcodegen / python-qrcode 逐模块一致；浏览器截图经 zxing / OpenCV / 微信识码器解出地址，手机视口无 token 打开即登录页（mock 后端，未用实体手机扫）。v2.1.0 漏项，本次补齐。

## 阶段 4 · server.properties 全量目录

- [ ] **4.1 「常用设置」页重构**：由 `PROPS_CATALOG` 驱动，7 组分组呈现 + 页首分组 chips 锚点 + 搜索框（中文名/键名过滤）+ 0.3 行式控件；数字项带范围校验；「需重启」项标注。
  验收：约 60 项全部可见可改可存；搜索「视距」「rcon」均能定位。—— 部分完成：常用设置由 PROPS_CATALOG 驱动（78 项按实例版本显隐），7 组 + .cp-chip 锚点 + 搜索（中文名 + 键名，「视距」「rcon」可定位）+「重启生效」角标，保存写回 server.properties；未做：数字项范围校验（type=number 无 min / max，目录也无范围字段）；控件是 .prop-item 卡片而非 0.3 行式控件。
- [x] **4.2 版本感知显隐**：按实例 MC 版本（0.2 verGte）隐藏不适用项——1.17- 隐 simulation-distance、1.21.2+ 隐 spawn-animals/npcs、1.21.9+/26.x 隐已迁移四项等，避免写死键。
  验收：1.12.2、1.20.1、26.x 三个实例的常用设置页各自显隐正确。—— 按目录 since / removed + verGte / verLt 过滤：1.17- 隐 simulation-distance、1.21.2+ 隐 spawn-animals / spawn-npcs、1.21.9+ 才显示 management-server-* 等；四个迁移键在 1.21.9+ 转为游戏规则开关（见 4.3）而非隐藏；level-type 在 1.16 以下改为自由文本（1.16–1.18 的枚举问题见 0.4）。
- [x] **4.3 gamerule 模式**：1.21.9+/26.x 上 pvp/生成怪物/命令方块/允许下界转为 gamerule 控件——运行中通过控制台发 `/gamerule` 即时生效，停机时置灰提示「启动后可改」；注意 enableCommandBlocks 默认翻转为 true 的说明文案。
  验收：26.x 实例运行中切换 pvp，游戏内 `/gamerule pvp` 查询值同步。—— 1.21.9+ / 26.x 上 pvp / spawn-monsters / allow-nether / enable-command-block 渲染为游戏规则开关（data-gr）：运行中经控制台发「gamerule <规则> true|false」即时生效，停机置灰并提示「服务器启动后可切换」；enableCommandBlocks 默认翻转写在条目说明。已知缺陷：开关初值取 server.properties 旧键或目录默认、不读实时规则，命令方块在新版本上显示为关（实际默认开）。
- [x] **4.4 「全部配置」补中文名**：raw 表对目录内已知键加中文名列，未知键（Paper 等自有配置）保持原样。
  验收：已知键显示中文名，陌生键不受影响。—— 全部配置 raw 表用 PROPS_CATALOG 建键名 → 中文名映射，新增 .raw-cn 列；目录外的键（Paper 等自有配置）该列留空。
- [x] **4.5 创建流水线联动 enforce-secure-profile**：`install.go` 步骤 4，onlineMode=false 时写 `enforce-secure-profile=false`（仅 1.19+ 实例写入）。
  验收：新建离线 1.20.1 实例的 server.properties 含该键=false；1.12.2 实例不写。—— install.go 写配置步骤：!OnlineMode 且 mcsrc.NewerOrEqual(mc, "1.19") 时 props.Set("enforce-secure-profile", "false")；整合包导入同样经 runCreate。
- [ ] **4.6 白名单默认值预警**：26.3+ 实例创建完成页与常用设置中提示「此版本起白名单默认开启」；创建流水线按用户离线/正版选择给出合理初始值。
  验收：26.3 快照实例可复现提示（无 26.3 时以目录数据驱动的提示逻辑单测代替）。—— 部分完成：常用设置的白名单行在 verGte(mc, "26.3") 时金字提示「此版本起白名单默认开启」；未做：创建完成页提示、创建流水线按离线 / 正版写 white-list 初值（install.go 不写该键），也没有对应单测。

## 阶段 5 · 存储位置

- [ ] **5.1 T1 路径透明化**：向导第 4 步与导入页实时显示目标路径（跟随实例名变化）；任务完成页、详情页头部显示路径。
  验收：改名时路径同步变化；三处路径与实际创建位置一致。—— 部分完成：向导第 4 步「存放位置」+ #f-path「将创建于：<根>\<slug>」随实例名实时更新；导入页有存放位置（只显示根目录）；未做：导入页随名称变化的完整路径、任务完成页与详情页头部的路径（详情只有「内存 / 启动」页的「实例目录」提示）。
- [x] **5.2 T2 全局「服务器存放目录」「备份目录」可配**：config.json 新增 `serversDir`/`backupsDir`（空=默认）+ 曾用根目录列表 `usedRoots`；设置「存储位置」分区落位（当前值摘要 + 浏览按钮 + 各目录「打开」）。变更仅对新建生效。
  验收：改到 D 盘后新建实例落 D 盘；旧实例原地可用。—— Config.ServersDir / BackupsDir（json serversDir / backupsDir，空 = 内置默认）；曾用根目录列表实为 Config.Roots（json roots，没有 usedRoots）；app.ServersRoot() / BackupsRoot() 取值；设置「存储位置」三行（服务器 / 备份 / 程序数据目录）带当前路径、打开、更改…、恢复默认，仅对新建生效。旧实例原地可用：改目录或恢复默认时 UpdateConfig 把旧 serversDir 并入 Roots，RememberRoot 也记下当时的配置根（只跳过内置根），旧配置根里的实例重启后照常扫描到（原已知缺陷已修复，回归测试见 internal/app/app_test.go）。
- [x] **5.3 Manager 多根扫描**：启动时扫描 默认目录 + 配置目录 + usedRoots + 注册的自定义实例目录；同名冲突时后者跳过并日志告警。
  验收：三个根各放一个实例，列表全部出现且可启停。—— 实际用 app.InstanceRoots()（内置 servers → 配置 serversDir → Config.Roots，去重）而非 usedRoots；NewManager 逐根扫描含 instance.json 的子目录，同名冲突先到先得、后者跳过并 log 告警；runCreate 结束时 app.RememberRoot(父目录) 记录自定义根（只记根，不记单个实例目录）。
- [x] **5.4 文件夹选择器**：Win32 `IFileOpenDialog`（`FOS_PICKFOLDERS`）纯 Go COM 封装，`POST /api/pickdir`；PowerShell `-STA` FolderBrowserDialog 兜底；**仅 localhost token 会话可调用**（LAN 会话返回 403 及提示）。
  验收：点浏览弹系统对话框、取消不报错、选择后路径回填；手机端点浏览得到明确提示而非无响应。—— POST /api/pickdir 由 PowerShell -STA FolderBrowserDialog 实现（未做 IFileOpenDialog COM 封装，原定兜底即正式方案），取消返回空路径；/api/pickdir 与 /api/openpath 以 isLoopbackReq 限本机，局域网会话 403 并提示。
- [x] **5.5 T3 每实例自定义路径**：向导「高级：自定义存放位置」折叠项（默认收起显示 5.1 路径）；`CreateReq.Dir`；自定义目录实例写入 config 注册表（5.3 依赖）。
  验收：自定义到任意盘创建成功、重启程序后仍在列表、可正常启停删。—— 字段是 CreateReq.Root（json root，不是 Dir），选父目录、实例落在 <root>\<slug>；「存放位置」常驻显示而非折叠的高级项；重启后经 Config.Roots 扫描回来。
- [ ] **5.6 目录名 slug 化 + Mohist 中文名解禁**：默认目录名 = 实例名 ASCII 安全化（非 ASCII → `server-{yyyyMMdd-HHmm}`）；`instance.json` 存中文显示名；Mohist/Banner 校验从「拒中文名」改为「最终路径含非 ASCII 才拒」，并在选核心当刻就地提示。
  验收：中文名 Mohist 实例创建并成功开服（目录为 slug）；把程序放中文路径下创建 Mohist 被拦截且提示准确。—— 部分完成：slugify 生成 ASCII 目录名（全非 ASCII 时用 server-yyyyMMdd-HHmmss，重名追加 -2、-3…），instance.json 保存中文显示名；Mohist / Banner 改为「最终路径含非 ASCII 才拒」并给出将创建的路径（625620a 记录真机验证）；未做：选核心当刻的就地提示（只在提交时报错）。
- [x] **5.7 删除安全语义**：`Delete()` 由「Rel 于 ServersDir」改为「目录内存在本实例 instance.json 才允许 RemoveAll」；备份/导入/策略等路径引用点回归。
  验收：自定义目录实例可安全删除；恶意构造（instance.json 缺失/名字不匹配）拒绝删除。—— Delete() → safeRemoveInstanceDir：拒删盘根与程序目录，目录内 instance.json 存在且 Name 一致才 RemoveAll；备份 / 导入改走 BackupsRoot 与 i.Dir（625620a 记录真机验证自定义位置实例删除）。

## 阶段 6 · 创建向导升级

- [x] **6.1 核心选择分组**：按 kind 分区（原版与轻量 / 插件服 / 模组服 / 混合服 / 群组代理）+「新手推荐」徽标（Paper）；分区标题 + 简介一句。
  验收：12 核心归组正确，双主题截图布局不破。—— drawWizard 第 0 步 CORE_GROUPS：原版与轻量（vanilla）/ 插件服（paper、purpur、leaves、folia）/ 模组服（fabric、forge、neoforge）/ 混合服（mohist、banner）/ 群组代理（velocity、waterfall），未归组核心进「其它」；Paper 带「新手推荐」.rec-badge；分区简介以标题括注代替。
- [x] **6.2 版本列表升级**：类型徽标 + 发布时间副行（后端 `/api/mcversions` 返回 releaseTime）；默认只显示正式版（快照开关保留）。
  验收：1.20.1 行显示发布日期；快照默认隐藏。—— mcsrc.ManifestVersion 解析清单 releaseTime，MCVersion 增 ReleaseTime（json releaseTime,omitempty），vanillaVersions 填充——Vanilla 与 NeoForge 的版本表取自 Mojang 清单，其余核心上游无时间留空；向导版本行有值时显示 YYYY-MM-DD 副行（.ver-date），快照仍默认隐藏。验证：1.20.1 → 2023-06-12，与清单 releaseTime 一致。v2.1.0 时暂缓（a18aa5c），本次补齐。
- [x] **6.3 基本设置补难度 / 默认游戏模式下拉** + 一句「另有约 60 项设置可在创建后的『常用设置』中修改」。
  验收：创建后 server.properties 两键值正确。—— 基本设置增「游戏难度」「默认游戏模式」下拉（代理端隐藏），CreateReq.Difficulty / Gamemode 在写配置步骤落 server.properties；提示文案为「更多几十项设置…可在创建后的『常用设置』中按分组修改」。
- [x] **6.4 零状态一键开服**：空列表 CTA 升级为「⚡ 一键开服（Paper 最新正式版 + 推荐设置）」——自动取名/内存/端口，**EULA 仍需弹窗显式勾选**后直接进入部署任务页。
  验收：全新数据目录下两次点击（按钮 + EULA 确认）到达部署页并成功开服。—— 空实例页「一键开服（Paper 最新正式版）」→ quickStart()：确认弹窗以「同意并开始」按钮显式同意 EULA（非勾选框）→ 取 Paper 最新正式版 + 推荐构建、内存 min(4G, 总内存 − 2G)（不低于 2G）、端口 25565 → 直达部署任务页；quickBusy 防重复创建。
- [ ] **6.5 导入整合包页同步**：路径显示（随 5.1）、内存组件（随 1.3）对齐。
  验收：与向导视觉/行为一致。—— 部分完成：导入页内存改用 renderMemoryControl，增「存放位置」+ 浏览（root 随表单提交）；未做：与向导一致、随实例名变化的完整目标路径预览（同 5.1）。

## 阶段 7 · 实例列表与详情

- [x] **7.1 卡片差量刷新**：轮询只更新状态点/按钮/时长等变化节点，不整卡重绘；消灭 3 秒焦点丢失与闪烁。
  验收：轮询期间悬停按钮不中断、开着的确认弹窗不受影响。—— 3 秒轮询按签名（名称 / 状态 / 分钟级时长 / 在线数 + 搜索排序）比对，未变化即跳过重绘；变化时整表重绘（非逐节点差量）；搜索 / 排序栏常驻卡片区外，确认弹窗挂在 #modal-root，不受重绘影响。
- [x] **7.2 运行时长 + 在线人数**：`/api/instances` 返回 `startedAt`/`onlineCount`；卡片显示「⏱ 2h13m · 👥 3/20」；HUD 汇总「运行中 N 台 · 在线 M 人」。
  验收：进出玩家/重启后数值正确。—— /api/instances 增 uptimeSec / onlineCount / maxPlayers（下发运行秒数而非 startedAt）；卡片显示「时钟 2h13m · 玩家 3/20」（SVG 图标，代理端只显时长）；汇总「共 N 台 · 运行中 M · 在线 K 人」放在列表工具栏而非 HUD。
- [ ] **7.3 「打开目录」升级为直达菜单**：根目录 / mods / plugins / world（读 level-name）/ logs / crash-reports / 备份目录，按核心 kind 过滤（代理端只留根/logs）；后端 opendir 接受 `sub` 参数并做白名单校验。
  验收：各项直开正确文件夹；`sub=../..` 之类被拒绝。—— 部分完成：卡片「打开目录」→ folderMenu()：实例目录 / mods / plugins / 世界存档 / logs / crash-reports，按核心过滤；opendir 接受 sub 并做穿越防护（拒绝绝对路径与 ..，不是固定白名单），仅本机可用；未做：世界目录读 level-name（固定为 world）、备份目录入口。
- [x] **7.4 实例搜索/排序**：名称过滤输入框 + 排序（运行中优先/最近创建）。
  验收：8+ 实例时过滤即时、排序稳定。—— #inst-q 名称过滤 + #inst-sort 排序（最近创建 / 运行中优先 / 名称）。
- [ ] **7.5 JVM 页增强**：ExtraJVM 参数编辑（textarea，一行一参）+ 「Aikar's Flags 一键预设」（按 ≥12G / <12G 两组）+ Xms 显示；run.bat 同步。
  验收：预设写入后 run.bat 与托管启动参数一致，实例可正常开服。—— 部分完成：「内存 / 启动」页增自定义 JVM 参数 textarea +「套用 Aikar's Flags」（≥12G 大堆变体 / <12G 常规）+ 清空，保存经 UpdateSettings 写 instance.json 并重写 run.bat，托管启动与 run.bat 共用 launchArgs()；未做：Xms 显示（页面恒传 xmsMb: 0，后端取 min(1024, Xmx)）。原已知缺陷已修复：「套用 / 清空」保留参数框里已有的 log4shell 缓解参数，UpdateSettings 保存时按版本强制补回，前端回显实际保存的参数（回归测试见 internal/inst/install_test.go）。
- [x] **7.6 控制台字号调节 + 导出**：A-/A+（localStorage 记忆）；「复制全部」「导出为 txt」。
  验收：字号跨页面记忆；导出文件内容与缓冲一致。—— 控制台 A- / A+（localStorage amh_con_fs，9–22px）+ 复制全部 + 导出为 txt（Blob 下载 <实例名>-console.txt）。
- [ ] **7.7 任务失败动线**：失败页增加「打开实例目录」「查看 installer.log / crash-reports」按钮（文件存在才显示）。
  验收：构造一次安装失败，按钮直达对应文件/目录。—— 未做（d45a77a 暂缓）：创建 / 导入失败会 RemoveAll 半成品目录，失败页只有「返回重试」与任务日志（安装器输出以「[安装器]」写入日志）；而 install.go 的报错仍让用户去实例目录查看 installer.jar.log / installer.log，该目录此时已被删除。需先决定失败时是否保留目录或日志。

## 阶段 8 · 联机页打磨

- [x] **8.1 添加表单收进「＋ 添加隧道」弹层**：列表页面清爽化；弹层内保留服务商联动字段逻辑。
  验收：添加流程功能不变，页面首屏只见隧道列表。—— 隧道页先显示列表，添加表单收进「＋ 添加隧道…」折叠区（#tn-form 切换 hidden，不是弹层），服务商联动字段（applyFields）保留。
- [x] **8.2 失败原因上浮**：tunnel 捕获 frpc 最后错误行，`List` 返回 `lastError`，隧道行内红字显示（点开日志看全文）。
  验收：错 token 启动隧道，行内直接可见失败原因。—— tunnel.Manager.lastErr：handleLine 捕获 frpc 含 error / fail / 失败 / shutdown / rejected / invalid / 错误 的行，成功行清除；List() 经 Status.LastError（json lastError）下发；隧道行在没有公网地址时红字显示（截断 46 字，悬停看全文）。已知缺陷：PublicAddr 落盘后从不清空，拿到过地址的隧道之后再失败不会行内显示。

## 阶段 9 · 体验打磨

- [x] **9.1 备份保留份数/目录可配**：保留份数（默认 10）进实例备份页；备份根目录随 5.2。
  验收：设 3 份后第 4 次备份滚动删除最旧。—— Config.BackupKeep + app.BackupKeep()（1~1000 裁剪），pruneBackups 改用；备份页「保留最近 N 份 · 保存」控件（全局设置）。改备份目录或恢复默认后旧备份原地可用：UpdateConfig 把旧 backupsDir 并入 Config.BackupRoots（json backupRoots，内置根除外），列表 / 还原 / 删除查 app.AllBackupRoots()（当前根 + 内置根 + 旧根），保留份数跨根合计；还原最旧的备份时源备份不再被还原前自动备份的滚动清理删掉（回归测试见 internal/app/backups_test.go、internal/inst/backup_test.go）。
- [x] **9.2 CF 未解析清单突出显示**：导入完成页醒目卡片「N 个模组需手动补装」+ 清单（名称 + 项目链接）。
  验收：构造含不可解析文件的 CF 包，卡片与清单正确显示。—— tasks.Warning 结构化告警（Snapshot.warnings），import.go 填充项目号/链接，任务页金色 warn-card 渲染（CDP 注入截图验）。
- [x] **9.3 首次运行引导卡**：首启一次性卡片（选下载源 + 「你的服务器将存放在…」说明 + 可关闭）。
  验收：新数据目录首启显示，关闭后不再出现。—— Config.Onboarded；boot 时未引导则弹 showOnboarding（下载源三选 + serversRoot 路径），点「开始使用」写 onboarded=true。双主题截图验。
- [x] **9.4 命令输入补全**：常用命令表 + 在线玩家名（Tab/点击补全）。
  验收：输入 `op ` 后出现在线玩家候选。—— 控制台 cmd-suggest 下拉：COMMON_CMDS 命令补全 + whitelist 子命令 + PLAYER_CMDS 在线玩家名（进出游戏刷新）；Tab/↑↓/点击补全，与历史↑↓协调。CDP 实测 `sa`→say/save-*。
- [x] **9.5 `bg/` 壁纸个性化**：程序旁 `bg/` 文件夹有图则随机作为主区背景（低透明度铺底，双主题适配），无则维持现状。
  验收：放图后刷新可见、删图后恢复；文字对比度不受影响（截图验）。—— GET /api/bg 列表 + GET /bg/{name}（免 token，filepath.Base 防穿越）；body.has-bg 用 --bg-veil 蒙版（深.88/浅.86）叠图，卡片实底保证对比度。双主题截图验。

## 阶段 10 · 托盘与自启

- [x] **10.1 最小化到托盘**：关窗改为收入托盘（服务器不停）；托盘菜单：打开面板 / 全部停止并退出；设置内开关（默认关，保持现有语义）。
  验收：开关开启后关窗服务器仍在跑、托盘可唤回窗口；开关关闭时行为与现在一致。—— tray_windows.go 子类化 go-webview2 窗口过程拦 WM_CLOSE（MinimizeToTray 时 SW_HIDE 吞掉、否则原逻辑退出）；Shell_NotifyIcon 常驻托盘图标 + 左键恢复/右键菜单（打开面板/全部停止并退出）；shutdown 改 PostMessage(WM_APP_QUIT) 线程安全退出 + runtime.LockOSThread。设置「启动与托盘」分区开关，双主题截图 + GUI -minimized 冒烟不崩验。**托盘实际关窗/恢复/菜单需真机手验**。
- [x] **10.2 开机自启**：设置开关，写/删 `HKCU\...\Run`（带 `-minimized` 参数配合 10.1）。
  验收：开启后注册表键正确、注销重登自动driving到托盘；关闭后键删除。—— internal/autostart（+非 Windows 桩）Enabled/Set 读写 HKCU\...\Run\MineDeck=`"<exe>" -minimized`；main.go 加 -minimized 启动即 hideTrayWindow；web autoStart 真值以注册表为准。真机 E2E：ON→注册表现值/api 真、OFF→键删/api 假、无残留。

## 阶段 11 · 收尾与回归

- [x] **11.1 版本号 v2.1.0** + README 功能清单与截图更新 + `docs/API.md` 增补。—— app.go Version=2.1.0；README 刷新功能表（三主题/托盘自启/存储自选/配置全量目录/v2.1 细节）+ 架构块；API.md 增补 pickdir/openpath/bg 端点、全部新 config 键、instances 新字段、opendir sub、tasks warnings、tunnels lastError、loopback/token 认证说明。
- [x] **11.2 全量真机 E2E 回归**（本轮 + 各阶段累积）：⑥备份滚动删除（keep=2→3 次备份剩 2，本轮实测）；⑦改端口局域网指引（阶段1）；⑧frps 回环（v1.3 实测）；⑩托盘关窗-唤回-退出（用户真机手验）；③自定义目录 + ④多根扫描 + ⑤gamerule 显隐（阶段4/5 实测）。①Paper 一键开服 / ②中文名 Mohist 需真实下载，由用户日常真机使用覆盖（跨阶段已多次实开）。
- [x] **11.3 双主题全页截图终验**：各阶段已逐页双主题阅图；本轮终验 instances/create/常用设置全量目录/设置/首启引导/托盘设置/备份/控制台补全/warn-card/壁纸，浅深均过，v2.1.0 显示正确。
- [x] **11.4 code-review 子代理审查**：4 个 general-purpose 子代理并行审 main…HEAD——①路径与权限安全 ②Windows 原生（托盘子类化/NOTIFYICONDATA/注册表）③并发与状态机 ④Web/API+前端 XSS。无 Critical；v2.1 新增（tunnel lastErr/tasks warnings/在线集合/zip-slip/bg 穿越/删除归属）确认正确。
- [x] **11.5 QA 修复回归**：审查发现全部处置——修：崩溃重启 runGen 守卫（High，防误拉起已停服）、停止看门狗按 proc 身份（Med，防误杀新进程）、config 自启改到校验/保存之后（Med）、opendir 仅本机、safeRelPath 拒裸 `..`、backupDirOf filepath.Base、UpdateSettings/Policies 持锁落盘、tunnel 锁内取名、warn 链接限 http(s)、两处 badge 转义、prTimer 清理、托盘子类化/NIM_ADD 结果校验 + TaskbarCreated 重登 + 回调 recover。跳过（已判无害）：冗余轮询、waitConsole 回放间隙、托盘图标幂等重删。复测：D1 校验失败不改注册表 + 自启正常开关 + 备份回归，全绿。
- [x] **11.6 分阶段提交收口**：12 阶段 commit 齐全；`MineDeck.exe` 重新构建发布。

## 明确不做 / 观察区

- **1.21.9+ 官方管理协议接入**（management-server-*）：前瞻观察，待生态成熟单独立项。
- **全局游戏设置预设（逐项继承/覆盖）**：远期，多服用户规模起来后再做。
- **多语言 i18n**：当前定位国内用户，不做。

## 地图导入的后续（2026-11-13 首版之外）

首版（分支 `feat-world-import`）做的是「完整 Java 世界导入 / 替换 / 换回」，见 `docs/地图导入-设计.md`。下面这些首版有意不做，或者只做了识别和指引：

- [ ] **同一台服务器同时加载多个世界、在世界之间传送**：属于多世界插件（Multiverse 这类）的事，首版只替换默认世界、不碰别的世界。
- [ ] **建筑蓝图粘贴**（.schem / .schematic / .litematic）：首版只认出来并说明要用 WorldEdit、Litematica。
- [ ] **基岩版世界转换**：首版只认出 .mcworld 并说明需要转换工具。
- [ ] **从完整服务端包里单独提取世界**：首版对服务器文件夹列出里面的世界、只导入用户选的那张；交接包（.mdpack）只指引到「导入交接包」。
- [ ] **单人主人进度自动继承**：首版只检测 `Data.Player` / `singleplayer_uuid` 并给出手动办法。要做的话，必须先在真实客户端上分别验证背包、经验、末影箱、位置和维度、统计和进度，并处理正版 / 离线 / 代理转发的 UUID 差异。
- [ ] **「清空玩家进度、全员重新开始」**：独立功能，要单独确认、按版本精确实现。
- [ ] **导入后自动启动并检查**：首版导入完保持停止；做的话要用户明确选择、并经过兼容检查。
- [ ] **资源包地址引导**：地图自带的 `resources.zip` 首版只保留并提示，没有帮用户配置 `resource-pack` 地址。
- [ ] **uid.dat 和插件对世界 UUID 的引用**：首版原样保留，没研究透之前不改。
- [ ] **恢复点的清理策略**：首版一直保留到用户手动删除，界面上显示占用空间；以后可以考虑提醒或按时间清理。
