package web

import (
	"net/http"
	"strings"
	"testing"
)

// JSON 请求体有字节上限（2026-11-14 Codex 复审第 3 条）：以前不限，字段长度要等整个请求体解析完才查。
// 这里把上限调小来测，免得真造几兆的请求体。
func TestJSONBodyIsCapped(t *testing.T) {
	e := newFullEnv(t)
	oldBig, oldSmall := maxJSONBody, maxSmallBody
	t.Cleanup(func() { maxJSONBody, maxSmallBody = oldBig, oldSmall })
	maxJSONBody, maxSmallBody = 64<<10, 8<<10

	pad := func(n int) string { return `{"pad":"` + strings.Repeat("x", n) + `"}` }
	tooLarge := func(method, path, body string) {
		t.Helper()
		rec := e.call(t, method, path, body, http.StatusRequestEntityTooLarge)
		if !strings.Contains(rec.Body.String(), "请求的内容太大了") {
			t.Errorf("%s %s：超过上限应当说内容太大：%s", method, path, rec.Body)
		}
	}

	// 走 bodyArgs 的接口（这里拿新增隧道、改服务器设置、地图导入的计划各试一个）
	for _, c := range [][2]string{
		{"POST", "/api/tunnels"},
		{"PUT", "/api/instances/生存服/settings"},
		{"POST", "/api/world/plan"},
	} {
		tooLarge(c[0], c[1], pad(int(maxJSONBody)))
	}
	// 没超的照常往下走：不是 413（多出来的字段会被参数检查拒掉，那是 400）
	if rec := e.call(t, "POST", "/api/tunnels", pad(int(maxJSONBody)/2), http.StatusBadRequest); strings.Contains(rec.Body.String(), "太大") {
		t.Errorf("没超上限的请求不该按太大拒绝：%s", rec.Body)
	}

	// 助手的接口：小上限
	tooLarge("POST", "/api/assistant/sessions", pad(int(maxSmallBody)))
	tooLarge("POST", "/api/assistant/sessions/nope/messages", pad(int(maxSmallBody)))

	// 走 readJSON 的接口（玩家操作）：同样 413
	tooLarge("POST", "/api/instances/生存服/players", pad(int(maxSmallBody)))

	// 请求体可省略、或者解析失败另有说法的三个接口：超限同样是 413，而且不往下做。
	// 以前创建备份把读请求体的错全吞了，超限的请求照样创建备份；恢复备份和装资源把超限说成「缺少文件名」「缺少项目 ID」
	// （2026-11-14 Codex 第二轮复审）
	// 回了 413 就是没有交给操作层：参数这一步报了错，后面的操作不会跑
	tooLarge("POST", "/api/instances/生存服/backups", pad(int(maxSmallBody)))
	tooLarge("POST", "/api/instances/生存服/backups/restore", pad(int(maxSmallBody)))
	tooLarge("POST", "/api/instances/生存服/resources/install", pad(int(maxSmallBody)))
	// 没超限、只是没带文件名的，照旧是原来那句话
	if rec := e.call(t, "POST", "/api/instances/生存服/backups/restore", `{}`, http.StatusBadRequest); !strings.Contains(rec.Body.String(), "缺少备份文件名") {
		t.Errorf("没带文件名应当照旧说缺少备份文件名：%s", rec.Body)
	}

	// 登录接口自己那层更小的上限照旧认得出来
	if rec := e.call(t, "POST", "/api/login", `{"password":"`+strings.Repeat("x", 8<<10)+`"}`, http.StatusRequestEntityTooLarge, noToken); !strings.Contains(rec.Body.String(), "请求太大") {
		t.Errorf("登录请求体超限应当是 413：%s", rec.Body)
	}
}

// 默认的上限装得下最大的正常请求：改配置文件时全文最多 50 万字（中文三个字节一个字）。
func TestJSONBodyCapFitsLargestRealRequest(t *testing.T) {
	const configChars = 500000
	if need := int64(configChars*3 + 20*2*20000*3); maxJSONBody < need {
		t.Errorf("bodyArgs 的上限 %d 装不下改配置文件的最大请求（约 %d 字节）", maxJSONBody, need)
	}
	if need := int64(20000 * 3 * 2); maxSmallBody < need {
		t.Errorf("小上限 %d 装不下助手两万字的消息（约 %d 字节）", maxSmallBody, need)
	}
}
