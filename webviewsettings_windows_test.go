package main

import (
	"runtime"
	"testing"

	webview2 "github.com/jchv/go-webview2"
)

// 使用真实 WebView2 核对原生设置，窗口保持隐藏，不启动服务器或访问网络。
func TestWebViewStatusBarDisabled(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	endHidden := beginHiddenCreate("webview", true, true)
	defer endHidden()
	w := webview2.NewWithOptions(webview2.WebViewOptions{DataPath: t.TempDir()})
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
