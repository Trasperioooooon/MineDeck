package main

import (
	"fmt"
	"reflect"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

// go-webview2 当前固定版本没有公开原生 settings，取出其 Chromium 后调用 SDK。
// 仅在创建 WebView 的 UI 线程调用；升级依赖时由原生测试核对 browser 字段。
func webViewSettings(w webview2.WebView) (*edge.ICoreWebViewSettings, error) {
	browser := reflect.ValueOf(w).Elem().FieldByName("browser")
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
