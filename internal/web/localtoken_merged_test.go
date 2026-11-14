package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 「仅本机要带令牌」（PR #20）和平台账户（PR #13）、AI 的模型列表（PR #19）、地图导入（PR #18）是分开写、分开审的，
// 2026-11-14 一起合进 main。后三个的仅本机接口写的时候，「本机」还只看连接地址。这里核对它们合到一起以后用的是新规则：
// 只带局域网 Cookie 的本机请求（经本机 frpc 转发进来的就是这样）在这几组接口上一律 403，带着令牌的本机请求能过这一关。
func TestCookieOnlyLoopbackIsNotLocalForAccountsWorldAndAI(t *testing.T) {
	e := newFullEnv(t) // 开着局域网访问、设了访问密码

	rec := e.call(t, "POST", "/api/login", `{"password":"`+lanPass+`"}`, 200, noToken)
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "amh_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("登录后没有发局域网会话 Cookie")
	}
	send := func(method, path, body string, withToken bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host, req.RemoteAddr = "127.0.0.1", "127.0.0.1:52000"
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if withToken {
			req.Header.Set("X-Token", Token)
		} else {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}

	nope := `{"path":"` + strings.ReplaceAll(filepath.Join(app.Base, "nope"), `\`, `\\`) + `"}`
	cases := []struct {
		method, path, body string
		// withTokenToo 这条带上令牌再发一遍，确认 403 是「不算本机」造成的，不是这个接口本来就拒绝。
		// 只挑不会联网的：取消一个不存在的授权、检测一个不存在的文件夹、存一份不完整的 AI 设置。
		withTokenToo bool
	}{
		// PR #13：用到或改动平台凭据的都是仅本机
		{"POST", "/api/tunnel-accounts/openfrp/login", ``, false},
		{"POST", "/api/tunnel-accounts/openfrp/login/nope/poll", ``, false},
		{"POST", "/api/tunnel-accounts/openfrp/login/nope/cancel", ``, true},
		{"POST", "/api/tunnel-accounts/natfrp/connect", `{"token":"x"}`, false},
		{"DELETE", "/api/tunnel-accounts/natfrp", ``, false},
		{"GET", "/api/tunnel-accounts/natfrp/overview", ``, false},
		{"POST", "/api/tunnel-accounts/natfrp/tunnels", `{}`, false},
		{"POST", "/api/tunnel-accounts/natfrp/import", `{}`, false},
		// PR #18：读本机文件夹的检测
		{"POST", "/api/world/scan", nope, true},
		// PR #19：拉模型列表会带着 Key 去请求；改服务商
		{"POST", "/api/ai/models", `{}`, false},
		{"PUT", "/api/ai/provider", `{}`, true},
	}
	for _, c := range cases {
		if got := send(c.method, c.path, c.body, false); got.Code != http.StatusForbidden {
			t.Errorf("%s %s：只带 Cookie 的本机请求应当 403，得到 %d：%s", c.method, c.path, got.Code, got.Body)
		}
		if !c.withTokenToo {
			continue
		}
		if got := send(c.method, c.path, c.body, true); got.Code == http.StatusForbidden || got.Code == http.StatusUnauthorized {
			t.Errorf("%s %s：带令牌的本机请求不该被当成外人，得到 %d：%s", c.method, c.path, got.Code, got.Body)
		}
	}

	// 账户状态局域网也能看，但只读：local 是 false，界面据此不出按钮
	if body := send("GET", "/api/tunnel-accounts", "", false).Body.String(); !strings.Contains(body, `"local":false`) {
		t.Errorf("只带 Cookie 的本机请求看账户状态时应是只读（local=false）：%s", body)
	}
	if body := send("GET", "/api/tunnel-accounts", "", true).Body.String(); !strings.Contains(body, `"local":true`) {
		t.Errorf("带令牌的本机请求看账户状态时应是 local=true：%s", body)
	}
}
