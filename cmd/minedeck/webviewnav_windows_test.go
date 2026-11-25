//go:build windows

package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/jchv/go-webview2"
)

func TestClassifyMainFrameNavigation(t *testing.T) {
	const base = "http://127.0.0.1:27333/?token=secret-token"
	for _, allow := range []string{
		"http://127.0.0.1:27333/",
		"http://127.0.0.1:27333/?token=secret-token#/settings",
		"http://127.0.0.1:27333/api/toolbox/skin-pack/download?id=1",
		"HTTP://127.0.0.1:27333/index.html",
	} {
		if got := classifyMainFrameNavigation(allow, base); got != navAllow {
			t.Errorf("%q 应当同源放行，实际 %v", allow, got)
		}
	}

	for _, ext := range []string{
		"https://aka.ms/MinecraftEULA",
		"https://www.openfrp.net/",
		"https://user:pass@example.com/",
		"http://example.com/",
		"http://127.0.0.1:28080/",
		"http://localhost:27333/",
		"https://127.0.0.1:27333/",
	} {
		if got := classifyMainFrameNavigation(ext, base); got != navOpenExternal {
			t.Errorf("%q 应当拦截并交外部浏览器，实际 %v", ext, got)
		}
	}

	for _, cancel := range []string{
		"",
		"about:blank",
		"file:///C:/Windows/System32/calc.exe",
		"javascript:alert(1)",
		"ms-settings:privacy",
		"data:text/html,<script>alert(1)</script>",
		"blob:http://127.0.0.1:27333/1234",
		"ftp://example.com/",
		"http://evil@127.0.0.1:27333/",
		"https://example.com/a b",
		`https://example.com/"x`,
		"https://example.com/\u202e",
	} {
		if got := classifyMainFrameNavigation(cancel, base); got != navCancel {
			t.Errorf("%q 应当直接拦截且不外跳，实际 %v", cancel, got)
		}
	}
}

func TestExtractCoreWebView2RejectsMalformed(t *testing.T) {
	if _, _, _, err := extractCoreWebView2(nil); err == nil {
		t.Fatal("nil WebView 应返回错误")
	}
}

// 用真实 WebView2 隐藏窗口验证 NavigationStarting 拦截（全部走本地 httptest.Server，不连外网）：
//  1. 同源页面正常加载并能读回标题；
//  2. 页面内脚本 location.href 跳到外部本地服务 extSrv 时，主窗口取消提交并留在 APP_HOME，
//     外跳 URL 转交 launchURL 且打开失败时回调 onFail；
//  3. 非 http(s) 地址（file:///）被拦下留在原页且不交给 launchURL；
//  4. 随后同源 location.href = '/next' 仍能正常加载；退出消息循环前 removeGuard 幂等清理不崩溃。
func TestWebViewNavInterceptorLive(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	endHidden := beginHiddenCreate("webview", true, true)
	defer endHidden()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/next":
			fmt.Fprint(w, `<!doctype html><title>SAME_NEXT</title>`)
		default:
			fmt.Fprint(w, `<!doctype html><title>APP_HOME</title>`)
		}
	}))
	defer srv.Close()

	extSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>EXT_PAGE</title>`)
	}))
	defer extSrv.Close()

	dataDir, err := os.MkdirTemp("", "minedeck-nav-test")
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

	extTarget := extSrv.URL + "/fail-open"
	launched := make(chan string, 4)
	launchErr := make(chan error, 1)
	oldLaunch := launchURL
	launchURL = func(u string) error {
		launched <- u
		if u == extTarget {
			return errors.New("模拟浏览器拉起失败")
		}
		return nil
	}
	t.Cleanup(func() { launchURL = oldLaunch })

	w := webview2.NewWithOptions(webview2.WebViewOptions{DataPath: dataDir})
	if w == nil {
		t.Fatal("WebView2 创建失败")
	}
	defer func() {
		w.Destroy()
		w.Run()
	}()

	removeGuard, err := installWebViewNavGuard(w, srv.URL, func(err error) {
		launchErr <- err
	})
	if err != nil {
		t.Fatalf("installWebViewNavGuard 失败：%v", err)
	}

	stepCh := make(chan string, 8)
	locCh := make(chan string, 4)
	_ = w.Bind("reportPage", func(title string) {
		stepCh <- title
	})
	_ = w.Bind("reportLoc", func(s string) {
		locCh <- s
	})
	w.Init("window.addEventListener('DOMContentLoaded', () => { reportPage(document.title); });")

	w.Navigate(srv.URL + "/")
	go func() {
		waitTitle := func(want string) {
			deadline := time.After(10 * time.Second)
			for {
				select {
				case got := <-stepCh:
					if got == want {
						return
					}
					t.Errorf("收到意外页面标题：got=%q want=%q", got, want)
				case <-deadline:
					t.Errorf("等待页面 %q 超时", want)
					return
				}
			}
		}

		waitTitle("APP_HOME")

		// 2. 页面内脚本企图通过 location.href 跳到外部服务：必须取消且留在原页 APP_HOME。
		w.Dispatch(func() { w.Eval("location.href = " + strconv.Quote(extTarget)) })
		select {
		case ext := <-launched:
			if ext != extTarget {
				t.Errorf("交给外部浏览器的地址不符：%q", ext)
			}
		case <-time.After(5 * time.Second):
			t.Error("外跳地址未被转交系统默认浏览器")
		}
		select {
		case err := <-launchErr:
			if err == nil {
				t.Error("期望收到非空 onFail 错误")
			}
		case <-time.After(5 * time.Second):
			t.Error("外部打开失败时未回调 onFail")
		}
		time.Sleep(250 * time.Millisecond)
		w.Dispatch(func() { w.Eval("reportLoc(location.href + '|' + document.title)") })
		select {
		case gotLoc := <-locCh:
			if gotLoc != srv.URL+"/|APP_HOME" {
				t.Errorf("外跳未拦住：当前页面变成 %q，期望停留在 %q", gotLoc, srv.URL+"/|APP_HOME")
			}
		case <-time.After(5 * time.Second):
			t.Error("外跳后回读 location.href 超时")
		}
		select {
		case title := <-stepCh:
			t.Errorf("拦截外跳期间不应提交新页面：got=%q", title)
		default:
		}

		// 3. 非 http(s) 协议（file:///）：应取消导航，既不交给 launchURL 也留在 APP_HOME。
		w.Dispatch(func() { w.Navigate("file:///C:/Windows/win.ini") })
		select {
		case u := <-launched:
			t.Errorf("file:/// 不应交给外部浏览器：%q", u)
		case <-time.After(300 * time.Millisecond):
		}
		w.Dispatch(func() { w.Eval("reportLoc(location.href + '|' + document.title)") })
		select {
		case gotLoc := <-locCh:
			if gotLoc != srv.URL+"/|APP_HOME" {
				t.Errorf("file:/// 导航后页面偏离：got=%q", gotLoc)
			}
		case <-time.After(5 * time.Second):
			t.Error("file:/// 拦截后回读 location.href 超时")
		}

		// 4. 再做同源脚本跳转，确认主窗口仍停留在原应用下且功能正常。
		w.Dispatch(func() { w.Eval("location.href = '/next'") })
		waitTitle("SAME_NEXT")

		w.Dispatch(func() {
			removeGuard()
			removeGuard()
			w.Terminate()
		})
	}()

	w.Run()
}
