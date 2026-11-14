# 界面走查用的真窗口驱动

2026-11-14 做可用性走查时写的一套脚本：用发布版 MineDeck.exe 开真窗口（WebView2 带调试端口），再用主仓库 `tools/ui-dev/node_modules` 里的 Playwright 连上去，像用户一样截图、点击、悬停、打字、选文件。报告在 `docs/可用性走查-2026-11-14.md`，那里的整改验收就是拿这套脚本让一个没看过源码的代理把同样的任务重走一遍。

所有路径都写死在 `D:\MineDeck-ux` 下（短路径，免得超过 260 个字符）：`bin\` 放编好的程序，`first\` 是全新副本，`return\` 是预置了数据的副本，`testdata\` 是测试文件，`rig\` 放这些脚本（从这里复制过去即可）。两份副本的程序端口分别是 27481、27482，调试端口 9341、9342。程序用的 LOCALAPPDATA、APPDATA、TEMP 都指进副本，不碰本机正在用的 MineDeck。

怎么重新搭一遍：

1. 按发布参数编程序，放进 `D:\MineDeck-ux\bin\MineDeck.exe`，复制到 `first\` 和 `return\`；有下载缓存的话把 `cache\` 一起放进去会快很多。
2. `return\` 副本：`start.ps1 -Copy return -NoGui` 起一个不带窗口的，再跑 `setup\seed.ps1 -Step config`、`setup\seed.ps1 -Step create`、`setup\seed2.ps1`（建三台服务器、做两份备份、加一条不启动的假隧道），之后用 `stop.ps1 -Copy return` 关掉。走查时用到的「Java 路径失效」和端口错开（2557x）是手改的 instance.json 和 server.properties，见报告第一节。
3. `setup\testdata.ps1` 生成测试文件（整合包、以前手动开的服、单人存档、插件 jar）。

用法：`start.ps1 -Copy first|return -Out <证据文件夹>` 开窗口；`node drive.js <副本> help` 看命令；`stop.ps1 -Copy <副本>` 关掉。每一步的截图和记录（actions.jsonl）都在证据文件夹里，页面报错在同一处的 console.log。走查员的规则和任务单是同目录的 `共同规则.md` 和两份 `任务单-*.md`。

已知的坑：

- 系统「选择文件夹」对话框由 `osdlg.ps1` 用窗口消息填路径、点确定；它的截图会拍到本机文件夹名，不要放进仓库。
- `upload` 必须先等 Playwright 开始拦截文件对话框再点，脚本里等了 0.6 秒。2026-11-14 第一轮没等，系统「打开」对话框真弹到了桌面上（归 msedgewebview2 进程，驱动看不到），代理以为按钮没反应。
- 点外部链接会在本机默认浏览器里真开网页；右键会弹出脚本关不掉的 WebView2 菜单；「开机自动启动」写的是本机注册表。三样都别碰。
- 两份副本同时开服会抢 25565：并行走查时把一份的服务器端口错开。
