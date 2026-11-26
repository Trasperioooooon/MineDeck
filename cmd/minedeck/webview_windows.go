package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"time"

	"minedeck/internal/hostdlg"

	webview2 "github.com/jchv/go-webview2"
)

// openWebView 尝试以原生窗口运行界面；窗口关闭或收到中断信号时返回。
func openWebView(url string, sig chan os.Signal, minimized bool) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	// Win32 窗口线程亲和：建窗与消息循环须固定在同一 OS 线程
	runtime.LockOSThread()
	scale := systemDPIScale() // 窗口尺寸按 DPI 放大，保持高分屏上的视觉大小
	setWebViewBackground()    // 页面画出来之前的底色取界面标题栏色，冷启动不白闪（须在创建 WebView2 之前）
	// NewWithOptions 建完窗口就显示、再花几百毫秒初始化 WebView2，这段时间窗口是白的（见 hiddenstart_windows.go）：
	// 挂钩子让窗口照常显示但先隐身，页面画出头两帧后再现身（下面的 ready）。随开机自启静默进托盘时，那次显示也不激活窗口。
	// 静默启动以前是干脆拦掉那次显示，可 WebView 在从没显示过的窗口里不出画面，之后从托盘恢复一直是白的（2026-11 实测）
	endHidden := beginHiddenCreate("webview", true, minimized)
	defer endHidden() // NewWithOptions 里 panic 被上面的 recover 接住时也撤掉钩子（end 可重复调用）
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  webviewDataDir(), // 固定数据目录，界面偏好不随 exe 改名丢失
		WindowOptions: webview2.WindowOptions{
			Title:  "MineDeck · MC 一键开服",
			Width:  uint(float64(1280) * scale),
			Height: uint(float64(850) * scale),
			Center: true,
		},
	})
	endHidden() // 撤掉钩子：之后从托盘恢复照常显示
	if w == nil {
		runtime.UnlockOSThread() // WebView2 不可用，回退浏览器：释放线程绑定
		return false
	}
	defer w.Destroy()
	if err := disableWebViewStatusBar(w); err != nil {
		log.Println("关闭 WebView2 链接状态栏失败:", err)
	}
	hwnd := uintptr(w.Window())
	hostdlg.SetOwner(hwnd) // 「选择文件夹」挂在主窗口上：开着的时候主窗口点不动，也不会盖住它
	defer hostdlg.SetOwner(0)
	clearPickerDispatch := hostdlg.SetDispatcher(currentUIThreadID(), w.Dispatch)
	defer clearPickerDispatch()
	applyDarkTitleBar(hwnd) // DWM 边框深色化，匹配深色主题
	applyWindowIcon(hwnd)   // 任务栏 / Alt-Tab / 托盘图标
	setupTray(hwnd)         // 子类化窗口过程（含去标题栏）+ 挂托盘图标（关窗最小化 / 右键菜单）
	if minimized && !trayActive {
		// 静默启动却没登记上托盘图标（开机时外壳还没就绪最常见）：窗口一直藏着就再也找不回来，退到任务栏最小化；
		// 用 SHOWMINNOACTIVE，开机时不改动前台窗口（独立评审发现）
		pShowWindow.Call(hwnd, _SW_SHOWMINNOACTIVE)
	}
	applyFrameless(hwnd)      // 触发边框重算，让去标题栏立即生效（须在子类化之后）
	paintHostBackground(hwnd) // 整窗铺上启动画面的底色：现身时 WebView 还没上屏的那一两帧不露白
	// 无边框后原生标题栏没了，窗口拖动/最小化/最大化/关闭由前端标题栏经 Bind 桥接驱动
	w.Bind("hostWinDrag", func() { hostStartDrag(hwnd) })
	w.Bind("hostWinMin", func() { hostMinimize(hwnd) })
	w.Bind("hostWinMaxToggle", func() { hostMaximizeToggle(hwnd) })
	w.Bind("hostWinClose", func() { postWindowClose(hwnd) })
	w.Bind("hostWinIsMax", func() bool { return isZoomed(hwnd) })
	w.Bind("hostDblClickTime", func() int { return doubleClickTime() }) // 前端标题栏认双击用的系统双击间隔
	// 关窗确认（见 tray_windows.go 的 onClose）：界面回应一声、收进托盘、停止服务器并退出
	w.Bind("hostCloseAck", func() { hostCloseAck() })
	w.Bind("hostWinHide", func() { hideToTray(hwnd) })
	w.Bind("hostWinQuit", func() { postTrayQuit(hwnd) })
	// 外部链接（EULA、申请 CurseForge Key、openfrp.net 等）交给系统默认浏览器，不再弹 WebView2 自带的简易窗口：
	// app.js 的 initExternalLinks 拦下点击和 window.open 后调它。页面里谁都能调宿主函数，地址由 openExternalURL 把关，
	// 只认 http / https（见 openurl_windows.go）；后台打开失败时回头叫页面弹报错提示
	w.Bind("hostOpenURL", func(raw string) error {
		return openExternalURL(raw, func(err error) { w.Dispatch(func() { w.Eval(openURLFailedJS(err)) }) })
	})
	// 「设置 → 存储位置」把程序数据文件夹加进 / 撤出 Windows 安全中心的排除项（见 defender_windows.go）。只有这里的页面调得到，
	// 没登记进操作层：AI 助手、MCP 碰不到。页面只给 add / remove，路径由宿主自己定；UAC 等用户点完要一会儿，在后台做，
	// 做完切回界面线程把结果交给页面（window.__mdDefenderDone）
	w.Bind("hostDefenderExclusion", func(action string) error {
		return startDefenderExclusion(action, hwnd, func(r defenderResult) { w.Dispatch(func() { w.Eval(defenderDoneJS(r)) }) })
	})
	// 主窗口导航兜底（A89）：页面绕开上面的外链拦截、让主窗口自己跳走时（脚本改 location、file:// 之类），在 NavigationStarting 里取消，
	// 外部 http/https 交给默认浏览器，别的协议只取消。挡的是主窗口换成别的页面，挡不住请求：取消以后那一笔 GET 照样发到了对方（实测），
	// 所以走到这里的外部地址会被请求两次。iframe 不归它管，新开的窗口也不归它管（读代码得出）；拖进来的文件管不管没验证过。
	if removeNavGuard, err := installWebViewNavGuard(w, url, func(err error) {
		w.Dispatch(func() { w.Eval(openURLFailedJS(err)) })
	}); err != nil {
		log.Println("安装 WebView2 主窗口导航拦截失败:", err)
	} else {
		defer removeNavGuard()
	}
	// 页面画出头两帧（index.html 调 hostReady）：正常启动就现身；静默启动先收进托盘、再撤掉隐身，
	// WebView 已经画过，之后从托盘恢复不白屏。托盘图标没登记上时窗口上面已经最小化到任务栏，不收。
	// 页面没调（加载失败、脚本出错）也不能一直隐身，3 秒兜底。只做一次：以后刷新页面也会调 hostReady。
	readyDone := false
	ready := func() {
		if readyDone {
			return
		}
		readyDone = true
		if minimized && trayActive {
			hideTrayWindow(hwnd)
		}
		revealWindow(hwnd)
	}
	w.Bind("hostReady", ready)
	go func() {
		time.Sleep(3 * time.Second)
		w.Dispatch(ready)
	}()
	askCloseUI = func(tray bool, running int) {
		w.Eval(fmt.Sprintf("window.__mdCloseAsk && window.__mdCloseAsk({tray: %t, running: %d})", tray, running))
	}
	// 退出信号（Ctrl-C / 「退出程序」按钮）线程安全地投递到窗口线程，避免跨线程 Terminate
	go func() {
		<-sig
		postTrayQuit(hwnd)
	}()
	// 本次启动的 ID：前端据此只在冷启动后的第一次页面加载播放启动画面（刷新、托盘恢复都不再播）
	w.Init("window.__MD_BOOT = " + strconv.Quote(bootID) + ";")
	w.Navigate(url)
	w.Run()
	return true
}
