//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

func TestClassifyMainFrameNavigation(t *testing.T) {
	const base = "http://127.0.0.1:27333/?token=secret-token"
	for _, allow := range []string{
		"http://127.0.0.1:27333/",
		"http://127.0.0.1:27333/?token=secret-token#/settings",
		"http://127.0.0.1:27333/api/toolbox/skin-pack/download?id=1",
		"HTTP://127.0.0.1:27333/index.html",
		"http://127.0.0.1:027333/",
		// 同源判断只看协议、主机、端口：路径、查询、# 后面写成什么样都还是 MineDeck 自己的页面。
		// 下面这些是页面里 location.href = '/next?p=C:\x' 之类补全成绝对地址后、WebView2 原样交来的样子，
		// url.Parse 整串解析会拒收（反斜杠、零散的 %），以前因此被当成外部链接交给了默认浏览器（A89）。
		`http://127.0.0.1:27333/next?p=C:\x`,
		"http://127.0.0.1:27333/next/50%off",
		"http://127.0.0.1:27333/?token=secret-token#50%off",
		`http://127.0.0.1:27333/?token=secret-token#C:\x`,
		"http://127.0.0.1:27333/?q=<x>",
		"http://127.0.0.1:27333/?q=%3Cx%3E",
		`http://127.0.0.1:27333/a b"c`,
		"http://127.0.0.1:27333?x=1",
		"http://127.0.0.1:27333#top",
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
		"https://example.com/50%off",
		"http://[::1]:27333/",
		// 长得像 MineDeck 的地址、其实是别的主机：照旧当外部地址
		"http://127.0.0.1:27333@evil.com/",
		"http://evil.com/?http://127.0.0.1:27333/",
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
		"javascript://127.0.0.1:27333/%0aalert(1)",
		"ms-settings:privacy",
		"data:text/html,<script>alert(1)</script>",
		"blob:http://127.0.0.1:27333/1234",
		"ftp://example.com/",
		"http://evil@127.0.0.1:27333/",
		`http://evil@127.0.0.1:27333/p\x`,
		"https://example.com/a b",
		`https://example.com/"x`,
		"https://example.com/\u202e",
		// 取不出「协议://主机:端口」的：不知道是不是自己，取消、不外交
		"http:127.0.0.1:27333/",
		"http:///127.0.0.1:27333/",
		"http://127.0.0.1:27333.evil.com/",
		"http://127.0.0.1:27333:1/",
		"http://[::1/",
		"http://%zz/",
		"http://exa mple.com/",
		"1http://127.0.0.1:27333/",
		`http://evil.com\@127.0.0.1:27333/`,
	} {
		if got := classifyMainFrameNavigation(cancel, base); got != navCancel {
			t.Errorf("%q 应当直接拦截且不外跳，实际 %v", cancel, got)
		}
	}
}

// 冒充 go-webview2 窗口的假实现：嵌入接口只为满足 webview2.WebView，方法都不会被调到。
type fakeWebViewNoBrowser struct{ webview2.WebView }

type fakeWebViewOddBrowser struct {
	webview2.WebView
	browser any
}

func TestExtractCoreWebView2RejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    webview2.WebView
	}{
		{"nil", nil},
		{"带类型的空指针", (*fakeWebViewNoBrowser)(nil)},
		{"不是指针", fakeWebViewNoBrowser{}},
		{"没有 browser 字段", &fakeWebViewNoBrowser{}},
		{"browser 是空的", &fakeWebViewOddBrowser{}},
		{"browser 类型不对", &fakeWebViewOddBrowser{browser: "not chromium"}},
		{"browser 是空的 Chromium 指针", &fakeWebViewOddBrowser{browser: (*edge.Chromium)(nil)}},
		{"Chromium 里还没有 ICoreWebView2", &fakeWebViewOddBrowser{browser: &edge.Chromium{}}},
	} {
		_, _, _, err := extractCoreWebView2(tc.w)
		if err == nil {
			t.Errorf("%s：应当返回错误", tc.name)
			continue
		}
		// 每种畸形输入都该由前面的检查挡下，而不是靠反射 panic 后的兜底
		if strings.Contains(err.Error(), "反射读取") {
			t.Errorf("%s：走到了 panic 兜底：%v", tc.name, err)
		}
	}
}

var (
	fakePutCancelCalls atomic.Int32
	fakePutCancelValue atomic.Int32
	// 记账用的 put_Cancel：只建一次，-count 多跑几遍也不会多占系统回调槽位
	fakePutCancel = edge.NewComProc(func(_, cancel uintptr) uintptr {
		fakePutCancelCalls.Add(1)
		fakePutCancelValue.Store(int32(cancel))
		return comSOK
	})
)

func TestNavHandlerInvokeCancelsWhenCallbackPanics(t *testing.T) {
	fakePutCancelCalls.Store(0)
	fakePutCancelValue.Store(0)
	id := registerNavCallback(func(*navStartingEventArgs) { panic("模拟回调出错") })
	t.Cleanup(func() {
		navHandlersMu.Lock()
		delete(navHandlers, id)
		navHandlersMu.Unlock()
	})
	var logBuf bytes.Buffer
	oldOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(oldOut) })

	args := &navStartingEventArgs{vtbl: &navStartingEventArgsVtbl{PutCancel: fakePutCancel}}
	if r := navHandlerInvoke(&navStartingHandler{id: id}, nil, args); r != comSOK {
		t.Errorf("回调出错也应照常回 S_OK，实际 0x%X", r)
	}
	if n, v := fakePutCancelCalls.Load(), fakePutCancelValue.Load(); n != 1 || v != 1 {
		t.Errorf("回调出错后应取消这次导航（put_Cancel(1) 一次），实际调了 %d 次、最后一次的值 %d", n, v)
	}
	if !strings.Contains(logBuf.String(), "导航拦截") {
		t.Errorf("回调出错应记日志，实际日志：%q", logBuf.String())
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

	// 只记下、不断言：取消导航挡不住请求本身，这一笔发不发是 WebView2 的事，这里只把看到的写进测试输出
	extSeen := make(chan string, 8)
	extSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case extSeen <- r.Method + " " + r.URL.Path + " Sec-Fetch-Mode=" + r.Header.Get("Sec-Fetch-Mode"):
		default:
		}
		fmt.Fprint(w, `<!doctype html><title>EXT_PAGE</title>`)
	}))
	defer extSrv.Close()
	defer func() {
		var seen []string
		for len(extSeen) > 0 {
			seen = append(seen, <-extSeen)
		}
		t.Logf("导航被取消时外部服务收到 %d 笔请求：%q", len(seen), seen)
	}()

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

		// 4. 同源脚本跳转照常加载，地址里的反斜杠、# 后面零散的 % 也不能让它被当成外部链接（A89）。
		noneLaunched := func(step string) {
			select {
			case u := <-launched:
				t.Errorf("%s：同源地址不该交给外部浏览器：%q", step, u)
			case <-time.After(300 * time.Millisecond):
			}
		}
		w.Dispatch(func() { w.Eval(`location.href = '/next?p=C:\\x'`) })
		waitTitle("SAME_NEXT")
		noneLaunched("查询里带反斜杠")
		w.Dispatch(func() { w.Eval(`location.hash = 'a%zz\\b'; location.reload()`) })
		waitTitle("SAME_NEXT")
		noneLaunched("# 后面带零散的 % 和反斜杠再刷新")

		w.Dispatch(func() {
			removeGuard()
			removeGuard()
			w.Terminate()
		})
	}()

	w.Run()
}
