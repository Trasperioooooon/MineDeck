// MineDeck — Windows 本地 Minecraft 服务器一键部署工具
// GUI = 内嵌 Web 界面 + WebView2 原生窗口（缺失 WebView2 时自动回退默认浏览器；见 webview_windows.go）。
// Mac 版（摸底阶段）还没有原生窗口，界面用默认浏览器打开（见 host_darwin.go）。
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
	"time"

	"minedeck/internal/app"
	"minedeck/internal/autostart"
	"minedeck/internal/companion"
	"minedeck/internal/inst"
	"minedeck/internal/java"
	"minedeck/internal/musicshare"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/procutil"
	"minedeck/internal/tmpsweep"
	"minedeck/internal/tunnel"
	"minedeck/internal/update"
	"minedeck/internal/web"
	"minedeck/internal/webhook"
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
		fatal("初始化数据文件夹失败: " + err.Error())
	}
	procutil.InitJob() // 确保程序退出后不残留 java/frpc 进程
	if err := autostart.RemoveLegacy(); err != nil {
		log.Println("删除旧版（AutoMCHUB）开机自启项失败:", err)
	}

	mgr, err := inst.NewManager()
	if err != nil {
		fatal("加载服务器列表失败: " + err.Error())
	}
	go mgr.MigrateLog4j(context.Background()) // 已经建好的服务器，log4shell 防护整理成现在的做法：换、补、摘（见 inst/log4j_migrate.go）
	go sweepStaleTemps(mgr)                   // 以前被强退、断电时没写完留下的临时文件，后台清一遍
	tun := tunnel.NewManager()
	tun.FollowInstances(func(name string) string { // 开了跟随启动的隧道跟着实例启停
		i, err := mgr.Get(name)
		if err != nil {
			return "stopped"
		}
		return i.Status()
	})
	accounts := tunnel.NewAccounts(tun, app.Secrets())
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
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Accounts: accounts, AI: aiSvc, Port: addr.Port, Token: web.Token, Shutdown: func() {
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

// staleTempAge 临时文件多久没动过才清：正在写的一直在变，写完等着改名的那一下只有几毫秒，一小时绰绰有余。
const staleTempAge = time.Hour

// sweepStaleTemps 清掉 MineDeck 自己留下、很久没动过的临时文件（见 internal/tmpsweep）。只在程序启动后做一遍。
func sweepStaleTemps(mgr *inst.Manager) {
	var dirs []string
	for _, i := range mgr.List() {
		dirs = append(dirs, i.Dir)
	}
	if r := tmpsweep.Startup(app.Base, app.CacheDir, app.RuntimesDir, dirs, staleTempAge); r.Files > 0 {
		log.Printf("清掉了 %d 个以前没写完留下的临时文件，共 %.1f MB", r.Files, float64(r.Bytes)/(1<<20))
	}
}

func fatal(msg string) {
	log.Println(msg)
	// GUI 子系统下没有控制台，弹窗提示
	_ = exec.Command("mshta", "javascript:alert('MineDeck 启动失败：\\n"+msg+"');close()").Run()
	os.Exit(1)
}
