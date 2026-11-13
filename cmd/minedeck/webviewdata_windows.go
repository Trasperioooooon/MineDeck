package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// webviewDataDir 返回 WebView2 的用户数据目录。界面偏好（主题、配色、强调色、控制台字号与命令历史）
// 存在这里的浏览器本地存储里。go-webview2 默认用 %AppData%\<exe 文件名>，exe 一改名偏好就丢了，
// 所以固定为 %LocalAppData%\MineDeck\WebView2。
func webviewDataDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return "" // 取不到时交给 go-webview2 用默认目录
	}
	exe, _ := os.Executable()
	return migrateWebViewData(filepath.Join(local, "MineDeck", "WebView2"), os.Getenv("APPDATA"), filepath.Base(exe))
}

// migrateWebViewData 在 dir 还不存在时，把旧的默认数据目录整体移过来：优先找当前 exe 名对应的
// %AppData%\<exe 文件名>，再找改名前的 %AppData%\AutoMCHUB.exe。返回本次应使用的目录；
// 移动失败（旧进程的 WebView2 还没退干净、跨盘等）时本次沿用旧目录，下次启动再试。
func migrateWebViewData(dir, appData, exeName string) string {
	if appData == "" || pathExists(dir) {
		return dir
	}
	for _, name := range []string{exeName, "AutoMCHUB.exe"} {
		// 只搬 go-webview2 默认建的 %AppData%\<xxx.exe>\EBWebView，exe 名取不到时也绝不会搬 %AppData% 本身
		if !strings.EqualFold(filepath.Ext(name), ".exe") || !pathExists(filepath.Join(appData, name, "EBWebView")) {
			continue
		}
		old := filepath.Join(appData, name)
		err := os.MkdirAll(filepath.Dir(dir), 0o755)
		if err == nil {
			err = os.Rename(old, dir)
		}
		if err == nil || pathExists(dir) { // 同时启动的另一个进程可能已经搬完
			return dir
		}
		log.Println("迁移 WebView2 数据目录失败，本次沿用旧目录:", err)
		return old
	}
	return dir
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
