package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// 页面交来的地址：只认 http / https 的绝对地址，原样交出去；别的协议、相对地址、带空白引号控制字符的一律拒绝。
// 这里的测试不拿网址碰真正的 ShellExecuteEx，跑测试时不会打开浏览器（TestShellOpenReportsMissingFile 只打开一个不存在的文件）。
func TestCheckExternalURL(t *testing.T) {
	atLimit := "https://example.com/" + strings.Repeat("a", maxExternalURLLen-len("https://example.com/"))
	for _, ok := range []string{
		"https://aka.ms/MinecraftEULA",
		"https://console.curseforge.com/",
		"https://www.openfrp.net/",
		"http://example.com",
		"HTTPS://WWW.OPENFRP.NET/", // 协议名大小写不论
		"https://example.com:8443/a/b?c=d&e=f#g",
		"https://user:pass@example.com/",
		"http://[::1]:8080/x",
		"http://127.0.0.1:27333/",
		"https://zh.wikipedia.org/wiki/%E6%88%91%E7%9A%84%E4%B8%96%E7%95%8C", // 浏览器把中文转成了 %XX
		"https://xn--fiqs8s.example/",                                        // 中文域名转成了 punycode
		`https://example.com/search?q={a|b}\c`,                               // 查询串里浏览器原样保留的符号
		"https://example.com/50%off",                                         // 零散的「%」浏览器也原样保留，url.Parse 本来会拒收
		"https://example.com/a%",
		"https://example.com/#50%off",
		"https://us%er@example.com/",
		atLimit,
	} {
		got, err := checkExternalURL(ok)
		if err != nil || got != ok {
			t.Errorf("%.80q 应该原样放行，实际 %.80q, %v", ok, got, err)
		}
	}
	for _, bad := range []string{
		"",
		"file:///C:/Windows/System32/calc.exe",
		"file://server/share/x.exe",
		"javascript:alert(1)",
		"javascript://example.com/%0aalert(1)",
		"ms-settings:privacy",
		"mailto:someone@example.com",
		"ftp://example.com/",
		"shell:startup",
		"search-ms:query=x",
		`C:\Windows\System32\calc.exe`,
		`\\server\share\x.exe`,
		"/api/config",
		"relative.html",
		"//example.com/x",
		"#/settings",
		"http:example.com",
		`https:\\example.com`,
		"http:/example.com",
		"http://",
		"http://:80/",
		"http://user@/",
		" https://example.com",
		"https://example.com ",
		"https://example.com/a b",
		"https://example.com/\t",
		"\nhttps://example.com",
		"https://example.com/\x00",
		"https://example.com/\x7f",
		`https://example.com/" --renderer-cmd-prefix=calc`,
		"https://example.com/\u202e", // 从右往左排的控制符，能把地址伪装成别的样子
		"https://example.com/\u00a0",
		"https://例子.测试/",
		atLimit + "a",
	} {
		if got, err := checkExternalURL(bad); err == nil {
			t.Errorf("%.80q 应该拒绝，实际放行成 %.80q", bad, got)
		}
	}
}

// 只给检查用的副本：零散的「%」写成 %25，正常的 %XX 不动。
func TestEscapeStrayPercent(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/50%off":     "https://example.com/50%25off",
		"https://example.com/a%":         "https://example.com/a%25",
		"https://example.com/a%2":        "https://example.com/a%252",
		"https://example.com/%E6%88%91":  "https://example.com/%E6%88%91",
		"https://example.com/%2f%zz%4F%": "https://example.com/%2f%25zz%4F%25",
	} {
		if got := escapeStrayPercent(in); got != want {
			t.Errorf("%q 应写成 %q，实际 %q", in, want, got)
		}
	}
}

// 拒绝的地址不会交给浏览器；放行的地址原样交出去。
func TestOpenExternalURLLaunchesOnlyCheckedURLs(t *testing.T) {
	got := make(chan string, 8)
	old := launchURL
	launchURL = func(u string) error { got <- u; return nil }
	t.Cleanup(func() { launchURL = old })

	for _, bad := range []string{"file:///C:/Windows/System32/calc.exe", "javascript:alert(1)", "ms-settings:", "/api/config", `https://example.com/"x`} {
		if err := openExternalURL(bad, nil); err == nil {
			t.Errorf("%q 应该在检查这一步就被拒绝", bad)
		}
	}
	if err := openExternalURL("https://aka.ms/MinecraftEULA", nil); err != nil {
		t.Fatalf("合法地址被拒绝：%v", err)
	}
	select {
	case u := <-got:
		if u != "https://aka.ms/MinecraftEULA" {
			t.Errorf("交给浏览器的地址变了：%q", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("合法地址没有交给浏览器")
	}
	select { // 被拒绝的根本没开后台任务，这里不会再收到
	case u := <-got:
		t.Errorf("拒绝的地址也交给了浏览器：%q", u)
	case <-time.After(100 * time.Millisecond):
	}
}

// 后台打开失败时通知页面，报错里带着网址和系统给的原因。
func TestOpenExternalURLReportsLaunchFailure(t *testing.T) {
	old := launchURL
	launchURL = func(string) error { return windows.ERROR_NO_ASSOCIATION }
	t.Cleanup(func() { launchURL = old })

	failed := make(chan error, 1)
	if err := openExternalURL("https://www.openfrp.net/", func(err error) { failed <- err }); err != nil {
		t.Fatalf("合法地址不该在检查这一步失败：%v", err)
	}
	select {
	case err := <-failed:
		if !errors.Is(err, windows.ERROR_NO_ASSOCIATION) || !strings.Contains(err.Error(), "https://www.openfrp.net/") {
			t.Errorf("交给页面的报错不对：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("打开失败时没有通知页面")
	}
}

// 真调一次 ShellExecuteEx，验证自己声明的结构体和调用对得上：打开一个不存在的文件，应该报「找不到」，
// 不弹任何窗口（带了 SEE_MASK_FLAG_NO_UI）。结构体的大小或字段排错了，报的会是参数不对一类的错。
// 不拿网址试：那会真的打开浏览器。
func TestShellOpenReportsMissingFile(t *testing.T) {
	err := shellOpenURL(filepath.Join(t.TempDir(), "不存在的文件.txt"))
	if err == nil {
		t.Fatal("打开不存在的文件应该失败")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		t.Errorf("应报找不到文件：%v", err)
	}
}

// 通知页面的脚本：报错原文变成 JS 字符串字面量，引号、换行、U+2028、</script> 都拼不坏脚本，解回来一字不差。
func TestOpenURLFailedJS(t *testing.T) {
	const msg = "用默认浏览器打开 https://example.com/ 失败：\"引号\"\n第二行\u2028</script>"
	js := openURLFailedJS(errors.New(msg))
	const prefix = "window.__mdOpenURLFailed && window.__mdOpenURLFailed("
	if !strings.HasPrefix(js, prefix) || !strings.HasSuffix(js, ")") {
		t.Fatalf("脚本的样子不对：%s", js)
	}
	lit := strings.TrimSuffix(strings.TrimPrefix(js, prefix), ")")
	if strings.ContainsAny(lit, "\n\u2028<") {
		t.Errorf("字面量里有没转义的换行、U+2028 或 <：%s", lit)
	}
	var back string
	if err := json.Unmarshal([]byte(lit), &back); err != nil || back != msg {
		t.Errorf("解不回原文：%q, %v", back, err)
	}
}
