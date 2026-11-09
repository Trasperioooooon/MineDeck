package main

import (
	"fmt"
	"reflect"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

// go-webview2 当前固定版本没有公开原生 settings，取出其 Chromium 后调用 SDK。
// 仅在创建 WebView 的 UI 线程调用；升级依赖时由原生测试核对 browser 字段。
//
// 这里靠反射读依赖的内部结构，结构对不上时只返回错误，不能 panic：openWebView 顶层的 recover
// 会把 panic 当成「原生窗口不可用」退回浏览器模式，不该因为关不掉状态栏就丢掉整个窗口。
// 所以取字段之前逐级核对类型，末尾再用 recover 兜住没想到的情况。
//
// 返回的 settings 是 COM 对象，按约定用完该 Release，但 go-webview2 没有公开这个方法（它自己初始化时
// 取的那一份也不释放）。这个对象和 WebView 同寿命，每次启动只取一次，多出的这一次引用不处理。
func webViewSettings(w webview2.WebView) (settings *edge.ICoreWebViewSettings, err error) {
	defer func() {
		if r := recover(); r != nil {
			settings, err = nil, fmt.Errorf("读取 go-webview2 内部结构失败：%v", r)
		}
	}()
	view := reflect.ValueOf(w)
	if view.Kind() != reflect.Pointer || view.IsNil() || view.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("go-webview2 WebView 类型不兼容")
	}
	browser := view.Elem().FieldByName("browser")
	if !browser.IsValid() || browser.Kind() != reflect.Interface || browser.IsNil() {
		return nil, fmt.Errorf("go-webview2 browser 不可用")
	}
	chromium := browser.Elem()
	if chromium.Type() != reflect.TypeFor[*edge.Chromium]() || chromium.IsNil() {
		return nil, fmt.Errorf("go-webview2 browser 类型不兼容")
	}
	return (*edge.Chromium)(chromium.UnsafePointer()).GetSettings()
}

func disableWebViewStatusBar(w webview2.WebView) error {
	settings, err := webViewSettings(w)
	if err != nil {
		return err
	}
	return settings.PutIsStatusBarEnabled(false)
}
