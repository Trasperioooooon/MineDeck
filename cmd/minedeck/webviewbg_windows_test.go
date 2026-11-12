package main

import (
	"testing"

	"minedeck/internal/app"
)

// WebView2 默认底色：有合法的上报色就用它（大写 AARRGGBB，完全不透明）；没有或写法不对时退回默认方案的标题栏色。
func TestWebviewBackground(t *testing.T) {
	app.Base = t.TempDir() // config.json 落到临时目录
	set := func(c string) {
		t.Helper()
		if _, err := app.UpdateConfig(func(cfg *app.Config) error { cfg.ChromeColor = c; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	set("#2a3b4c")
	if got := webviewBackground(); got != "FF2A3B4C" {
		t.Errorf("上报色 #2a3b4c 应得到 FF2A3B4C，实际 %s", got)
	}
	for _, c := range []string{"", "red", "#fff", "#12345g"} {
		set(c)
		if got := webviewBackground(); got != "FFEAE5D7" && got != "FF0E1210" {
			t.Errorf("chromeColor=%q 应退回默认标题栏色，实际 %s", c, got)
		}
	}
	if len(bootID) != 16 {
		t.Errorf("启动 ID 应为 16 位十六进制：%q", bootID)
	}
}
