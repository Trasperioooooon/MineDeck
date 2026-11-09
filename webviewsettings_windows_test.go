package main

import (
	"os"
	"runtime"
	"testing"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// 使用真实 WebView2 核对原生设置，窗口保持隐藏，不启动服务器或访问网络。
func TestWebViewStatusBarDisabled(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	endHidden := beginHiddenCreate("webview", true, true)
	defer endHidden()
	// 不用 t.TempDir()：窗口销毁以后，WebView2 的浏览器进程还要一小会儿才退出、放开数据目录里的 lockfile，
	// 这时候删目录会失败，t.TempDir 会把整条测试算成失败（GitHub 的测试机上偶尔撞上）。自己建、自己多试几次删，
	// 实在删不掉只记一句，不算测试失败。
	dataDir, err := os.MkdirTemp("", "minedeck-webview-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dataDir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("临时的 WebView2 数据目录没删掉：%s", dataDir)
	})
	w := webview2.NewWithOptions(webview2.WebViewOptions{DataPath: dataDir})
	if w == nil {
		t.Fatal("WebView2 创建失败")
	}
	defer func() {
		w.Destroy()
		w.Run() // Destroy 投递 WM_CLOSE，处理消息后才能释放临时用户数据目录。
	}()
	if err := disableWebViewStatusBar(w); err != nil {
		t.Fatal(err)
	}
	settings, err := webViewSettings(w)
	if err != nil {
		t.Fatal(err)
	}
	if enabled, err := settings.GetIsStatusBarEnabled(); err != nil || enabled {
		t.Fatalf("链接状态栏未关闭：enabled=%v, err=%v", enabled, err)
	}
}
