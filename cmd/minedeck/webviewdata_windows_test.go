package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mkUDF 在 appData 下造一个 go-webview2 默认数据目录 <name>\EBWebView，里面放一个标记文件。
func mkUDF(t *testing.T, appData, name string) {
	t.Helper()
	d := filepath.Join(appData, name, "EBWebView", "Default")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "mark"), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
}

// markOf 读出 dir 里的标记文件，看它是从哪个旧目录搬来的；没有则返回空串。
func markOf(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "EBWebView", "Default", "mark"))
	return string(b)
}

func TestMigrateWebViewData(t *testing.T) {
	type env struct{ appData, local, dir string }
	setup := func(t *testing.T) env {
		root := t.TempDir()
		e := env{appData: filepath.Join(root, "Roaming"), local: filepath.Join(root, "Local")}
		e.dir = filepath.Join(e.local, "MineDeck", "WebView2")
		if err := os.MkdirAll(e.appData, 0o755); err != nil {
			t.Fatal(err)
		}
		return e
	}

	t.Run("优先搬当前 exe 名对应的旧目录", func(t *testing.T) {
		e := setup(t)
		mkUDF(t, e.appData, "开服.exe")
		mkUDF(t, e.appData, "AutoMCHUB.exe")
		if got := migrateWebViewData(e.dir, e.appData, "开服.exe"); got != e.dir {
			t.Fatalf("返回 %s，应为 %s", got, e.dir)
		}
		if m := markOf(e.dir); m != "开服.exe" {
			t.Fatalf("搬来的是 %q，应为 开服.exe 的旧目录", m)
		}
		if pathExists(filepath.Join(e.appData, "开服.exe")) || !pathExists(filepath.Join(e.appData, "AutoMCHUB.exe")) {
			t.Fatal("只应移走 开服.exe 的旧目录，AutoMCHUB.exe 的不动")
		}
	})

	t.Run("当前 exe 名没有旧目录时搬改名前的 AutoMCHUB.exe", func(t *testing.T) {
		e := setup(t)
		mkUDF(t, e.appData, "AutoMCHUB.exe")
		if got := migrateWebViewData(e.dir, e.appData, "MineDeck.exe"); got != e.dir || markOf(e.dir) != "AutoMCHUB.exe" {
			t.Fatalf("返回 %s、标记 %q，应搬入 %s", got, markOf(e.dir), e.dir)
		}
	})

	t.Run("新目录已存在时不再搬", func(t *testing.T) {
		e := setup(t)
		mkUDF(t, e.appData, "AutoMCHUB.exe")
		if err := os.MkdirAll(e.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if got := migrateWebViewData(e.dir, e.appData, "MineDeck.exe"); got != e.dir || markOf(e.dir) != "" {
			t.Fatalf("返回 %s、标记 %q，应直接用已有的 %s", got, markOf(e.dir), e.dir)
		}
		if !pathExists(filepath.Join(e.appData, "AutoMCHUB.exe")) {
			t.Fatal("旧目录不应被动")
		}
	})

	t.Run("没有旧目录时用新目录", func(t *testing.T) {
		e := setup(t)
		if err := os.MkdirAll(filepath.Join(e.appData, "MineDeck.exe"), 0o755); err != nil { // 没有 EBWebView，不是 WebView2 数据目录
			t.Fatal(err)
		}
		if got := migrateWebViewData(e.dir, e.appData, "MineDeck.exe"); got != e.dir {
			t.Fatalf("返回 %s，应为 %s", got, e.dir)
		}
		if !pathExists(filepath.Join(e.appData, "MineDeck.exe")) {
			t.Fatal("不含 EBWebView 的目录不应被搬走")
		}
	})

	t.Run("exe 名取不到时不会把 AppData 本身搬走", func(t *testing.T) {
		e := setup(t)
		if err := os.MkdirAll(filepath.Join(e.appData, "EBWebView"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"", "."} {
			if got := migrateWebViewData(e.dir, e.appData, name); got != e.dir {
				t.Fatalf("exe 名 %q：返回 %s，应为 %s", name, got, e.dir)
			}
		}
		if !pathExists(filepath.Join(e.appData, "EBWebView")) {
			t.Fatal("AppData 被动过")
		}
	})

	t.Run("搬不动时本次沿用旧目录", func(t *testing.T) {
		e := setup(t)
		mkUDF(t, e.appData, "AutoMCHUB.exe")
		// 让新目录的上级是个文件，建目录必然失败
		if err := os.MkdirAll(e.local, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.local, "MineDeck"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		old := filepath.Join(e.appData, "AutoMCHUB.exe")
		if got := migrateWebViewData(e.dir, e.appData, "MineDeck.exe"); got != old {
			t.Fatalf("返回 %s，应沿用旧目录 %s", got, old)
		}
		if markOf(old) != "AutoMCHUB.exe" {
			t.Fatal("旧目录内容不应受影响")
		}
	})
}
