// MineDeck — Windows 本地 Minecraft 服务器一键部署工具
// GUI = 内嵌 Web 界面 + WebView2 原生窗口（缺失 WebView2 时自动回退默认浏览器）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/autostart"
	"minedeck/internal/companion"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/musicshare"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/procutil"
	"minedeck/internal/tunnel"
	"minedeck/internal/update"
	"minedeck/internal/web"
	"minedeck/internal/webhook"

	webview2 "github.com/jchv/go-webview2"
)

func main() {
	nogui := flag.Bool("nogui", false, "仅启动本地服务，不打开窗口")
	minimized := flag.Bool("minimized", false, "启动后隐藏窗口到托盘（配合开机自启）")
	port := flag.Int("port", 27333, "本地 Web 端口（被占用时自动改用随机端口）")
	mcpBridge := flag.Bool("mcp", false, "MCP 的 stdio 桥：由 AI 工具当子进程启动，把消息转给同一目录下运行着的 MineDeck（不打开窗口）")
	flag.Parse()
	if *mcpBridge { // 不建窗口、不初始化数据目录，只做 stdio ↔ 本机 HTTP 的转发（见 mcpbridge.go）
		os.Exit(runMCPBridge())
	}
	setDPIAware() // 高 DPI 屏清晰渲染：必须在任何窗口创建前声明 DPI 感知（解析命令行参数不建窗口，放在后面也一样）

	if err := app.Init(); err != nil {
		fatal("初始化数据目录失败: " + err.Error())
	}
	procutil.InitJob() // 确保程序退出后不残留 java/frpc 进程
	if err := autostart.RemoveLegacy(); err != nil {
		log.Println("删除旧版（AutoMCHUB）开机自启项失败:", err)
	}

	mgr, err := inst.NewManager()
	if err != nil {
		fatal("加载实例列表失败: " + err.Error())
	}
	tun := tunnel.NewManager()
	tun.FollowInstances(func(name string) string { // 开了跟随启动的隧道跟着实例启停
		i, err := mgr.Get(name)
		if err != nil {
			return "stopped"
		}
		return i.Status()
	})
	webhook.Init()
	go java.Scan() // 后台扫描本机已装 Java，创建实例时可直接复用
	// AI 接入层：没配置 AI 时什么也不做、不联网；配置了要花钱的服务商，才在后台检查价格数据（缓存超过 7 天才下载）
	aiSvc := catalog.NewAI()
	aiSvc.Start()

	bindHost := "127.0.0.1"
	if cfg := app.GetConfig(); cfg.ListenLAN && cfg.AccessPasswordHash != "" {
		bindHost = "0.0.0.0"
		log.Println("局域网访问已启用（密码保护），手机/其他设备可通过本机 IP 访问")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost, *port))
	if err != nil {
		ln, err = net.Listen("tcp", bindHost+":0")
		if err != nil {
			fatal("无法监听本地端口: " + err.Error())
		}
	}
	addr := ln.Addr().(*net.TCPAddr)
	app.LANListening = bindHost == "0.0.0.0" // 「允许局域网访问」改了要重启才生效，界面拿它和配置比（须在开始服务之前设好）
	url := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", addr.Port, web.Token)
	// 操作层的注册表在这里建好：网页接口和应用内助手共用同一个（AI 的提案存在里面）
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, AI: aiSvc, Port: addr.Port, Token: web.Token, Shutdown: func() {
		if web.OnShutdown != nil { // OnShutdown 在下面才注入，用到时再取
			web.OnShutdown()
		}
	}})
	asst := catalog.NewAssistant(reg, aiSvc, web.Token)
	// MCP 接口（默认关闭）：同一台电脑上的其他 AI 工具经 /mcp 或 MineDeck.exe -mcp 调用操作层，写操作要在界面上确认
	mcpSvc := catalog.NewMCP(reg, web.Token)
	go func() {
		// 显式 http.Server 以设 ReadHeaderTimeout，防局域网模式下的 slowloris 半开连接；
		// 不设 WriteTimeout，以兼容控制台 SSE 长连接。
		srv := &http.Server{
			Handler:           web.New(mgr, tun, reg, asst, web.WithMCP(mcpSvc)),
			ReadHeaderTimeout: 10 * time.Second,
		}
		if err := srv.Serve(ln); err != nil {
			log.Println("HTTP 服务退出:", err)
		}
	}()
	mcpSvc.Start(addr.Port, *port) // 打开着 MCP 接口时写运行时文件（端口），stdio 桥据此找到这个 MineDeck
	log.Println("MineDeck 已启动:", url)

	// 启动时静默检查更新（可在设置中开关；仅日志提示，不打断）
	if cfg := app.GetConfig(); cfg.CheckUpdateOnStart && cfg.UpdateRepo != "" {
		go func(repo string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if rel, has, err := update.Check(ctx, repo); err == nil && has {
				log.Printf("发现新版本 %s（当前 v%s），可在设置中一键更新", rel.Tag, app.Version)
			}
		}(cfg.UpdateRepo)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	web.OnShutdown = func() {
		select {
		case sig <- os.Interrupt:
		default:
		}
	}

	// 正在运行（含启动中、停止中）的服务器台数：没开「最小化到托盘」时关窗据此决定要不要先确认（见 tray_windows.go）
	runningServers = func() int {
		n := 0
		for _, i := range mgr.List() {
			if i.Status() != "stopped" {
				n++
			}
		}
		return n
	}

	if *nogui {
		<-sig
	} else if !openWebView(url, sig, *minimized) {
		log.Println("WebView2 不可用，使用默认浏览器打开界面")
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		<-sig
	}

	log.Println("正在停止所有运行中的服务器与隧道...")
	// 先删掉 MCP 的运行时文件：stdio 桥马上知道 MineDeck 关了
	mcpSvc.Stop()
	musicshare.ShutdownAll("MineDeck 已退出") // 音乐共享（第五阶段）：先停广播（给桥接模组发 END、停采集），再关服
	companion.ShutdownAll("MineDeck 已退出")  // AI 伙伴（第七阶段）：给伙伴模组发 END 断开（伙伴和任务留在游戏里）
	tun.StopAll()
	mgr.ShutdownAll(30 * time.Second)
}

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

func fatal(msg string) {
	log.Println(msg)
	// GUI 子系统下没有控制台，弹窗提示
	_ = exec.Command("mshta", "javascript:alert('MineDeck 启动失败：\\n"+msg+"');close()").Run()
	os.Exit(1)
}
