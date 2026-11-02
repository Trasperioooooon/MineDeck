# 前端开发工具（tools/ui-dev）

改 `internal/web/ui/` 下的前端时用：不需要 Windows，也不需要 Go 后端，在浏览器里就能看界面、跑交互回归测试、批量截图。

| 文件 | 作用 |
|---|---|
| `server.js` | 仿真后端：直接托管 `internal/web/ui/`，`/api/*` 返回内存里的假数据（4 台服务器、2 条隧道等） |
| `tests.js` | 交互回归测试：切页后的轮询、未保存守卫、删除确认、新建向导、外观设置（默认方案「方块」）、远程访问二维码、局域网登录框、log4shell 参数保留、启动画面（只在冷启动播一次、可跳过）、设置分组列表、服务器状态切换的动效、「减少动态」、CurseForge Key 只写不读、隧道凭据解不开时的提示等 |
| `shoot.js` | 截图：逐页截图（深色、浅色各一套），以及 7 套配色 × 浅色 / 深色 |

## 准备（只需一次）

需要 Node.js 18 或更新版本。在本目录下运行：

```powershell
npm install
npx playwright install chromium
```

## 用法

```powershell
npm run mock                        # 启动仿真后端，然后用浏览器打开 http://127.0.0.1:27390/
npm test                            # 跑全部交互测试，全部通过时退出码为 0
npm test -- 外观                     # 只跑名字里含「外观」的测试（参数是正则）
npm run shots                       # 全部截图，存到 shots/pages 与 shots/schemes
npm run shots -- pages --only=wiz   # 只截名字匹配 wiz 的页面（新建向导各步）
npm run shots -- schemes            # 只截 7 套配色
```

`npm test` 和 `npm run shots` 会各自启动一份全新的仿真后端（随机端口），不用先开 `npm run mock`，每次都从同一份初始数据开始。`npm run mock` 默认用 27390 端口，避开 MineDeck 自己的 27333；要换端口就运行 `node server.js <端口>`。

## 注意

- 仿真后端的数据是假的，只用来看界面、测交互。后端逻辑用 `go test` 测：`internal/themes` 在任何平台都能跑，其余包需要 Windows。
- 浏览器里没有 WebView2 宿主。测试和截图脚本会模拟宿主注入的窗口函数，所以标题栏显示最小化 / 最大化 / 关闭三键；用 `npm run mock` 手动打开时不显示。启动画面只在有宿主函数、且注入了本次启动的 ID（`window.__MD_BOOT`）时播放，测试里单独模拟。
- 改了界面文字、选择器或交互，要同步改 `tests.js`；新加交互时顺手补一条测试，放在末尾「全程无页面报错」那条之前。
- 要模拟某种场景（服务器列表为空、首启引导、部署失败、开启远程访问、老版本实例），看 `server.js` 里 `/__mock/state` 的说明。
