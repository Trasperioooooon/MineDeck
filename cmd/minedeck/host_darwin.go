package main

import (
	"log"
	"os"
	"os/exec"
)

// Mac 版（摸底阶段）的宿主：还没有原生窗口（WKWebView）和菜单栏图标，后端照常起来，界面用默认浏览器打开。
// 界面没有宿主注入的 hostWinClose、hostOpenURL 这些函数时照常能用（手机远程管理、tools/ui-dev 的测试都是这样跑的）。
// 退出：在终端里按 Ctrl-C，或者界面上的「退出程序」，都会先停服务器和隧道再退。

// setDPIAware Windows 上声明高 DPI 感知；Mac 由系统处理，不用做什么。
func setDPIAware() {}

// bridgeIdle Windows 上关掉 GUI 程序启动时的忙碌光标（见 mcpbridge_windows.go）；Mac 上没有这回事。
func bridgeIdle() {}

// runningServers 正在运行的服务器台数，由 main 注入。Windows 上关窗确认要用（见 tray_windows.go），Mac 版还没有窗口，暂时不用。
var runningServers func() int

// openWebView 用默认浏览器打开界面，收到退出信号时返回。minimized（开机自启进托盘）时不打开浏览器。
func openWebView(url string, sig chan os.Signal, minimized bool) bool {
	if minimized {
		log.Println("静默启动：不打开浏览器，界面地址:", url)
	} else {
		log.Println("Mac 版还没有原生窗口，用默认浏览器打开界面")
		if err := exec.Command("open", url).Start(); err != nil {
			log.Println("打开浏览器失败，请手动打开上面的地址:", err)
		}
	}
	<-sig
	return true
}
