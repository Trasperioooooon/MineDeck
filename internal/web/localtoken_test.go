package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 「仅本机」不能只看连接来自 127.0.0.1：经本机 frpc 转发进来的请求也是。局域网登录的人带着 Cookie 从隧道进来，
// 连接地址是本机、请求头随便填，也不能算本机；只有带着这次启动的令牌（X-Token 或 ?token=）的本机请求才算。
func TestLocalNeedsTokenNotJustLoopback(t *testing.T) {
	e := newFullEnv(t) // 开着局域网访问、设了访问密码

	// 从本机地址登录（就像经隧道进来的那样）：拿到的是局域网会话 Cookie
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
	withCookie := func(r *http.Request) { r.AddCookie(cookie) }
	wrongToken := func(r *http.Request) { r.Header.Set("X-Token", strings.Repeat("0", len(Token))) }
	tunnelHost := func(r *http.Request) { r.Host = "frp.example.com:30001" }

	local := func(opts ...reqOpt) bool {
		t.Helper()
		var v struct {
			Data struct {
				Local bool `json:"local"`
			} `json:"data"`
		}
		if err := json.Unmarshal(e.call(t, "GET", "/api/app", "", 200, opts...).Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.Data.Local
	}
	// 打开一个不存在的文件夹：算本机时过了「仅本机」这一关，报 400「文件夹不存在」；不算本机时 403
	nope := `{"path":"` + strings.ReplaceAll(filepath.Join(app.Base, "nope"), `\`, `\\`) + `"}`

	cases := []struct {
		name  string
		opts  []reqOpt
		local bool
	}{
		{"本机 + 只带 Cookie", []reqOpt{noToken, withCookie}, false},
		{"本机 + Cookie + 界面的请求头", []reqOpt{noToken, withCookie, fromUI}, false},
		{"本机 + Cookie + 隧道的 Host", []reqOpt{noToken, withCookie, tunnelHost}, false},
		{"本机 + Cookie + 错的令牌", []reqOpt{withCookie, wrongToken}, false},
		{"本机 + 令牌（请求头）", nil, true},
		{"本机 + 令牌（请求头）+ Cookie", []reqOpt{withCookie}, true},
		{"局域网地址 + 令牌", []reqOpt{fromLAN}, false},
	}
	for _, c := range cases {
		if got := local(c.opts...); got != c.local {
			t.Errorf("%s：/api/app 的 local 是 %v，应为 %v", c.name, got, c.local)
		}
		want, msg := 403, "仅支持在本机操作"
		if c.local {
			want, msg = 400, "文件夹不存在"
		}
		if body := e.call(t, "POST", "/api/openpath", nope, want, c.opts...).Body.String(); !strings.Contains(body, msg) {
			t.Errorf("%s：打开文件夹应报「%s」：%s", c.name, msg, body)
		}
	}

	// 只带 ?token=（EventSource 设不了请求头，界面的控制台流就这么带）：仍算本机
	if body := e.call(t, "POST", "/api/openpath?token="+Token, nope, 400, noToken).Body.String(); !strings.Contains(body, "文件夹不存在") {
		t.Errorf("只带 ?token= 的本机请求应算本机：%s", body)
	}
	if body := e.call(t, "POST", "/api/openpath?token=bogus", nope, 403, noToken, withCookie).Body.String(); !strings.Contains(body, "仅支持在本机操作") {
		t.Errorf("?token= 不对、只靠 Cookie 进来的不算本机：%s", body)
	}

	// 只带 Cookie 的本机请求照样能用局域网能用的接口，操作记录里记作局域网，不记成「本机」
	e.call(t, "PUT", "/api/config", `{"backupKeep":7}`, 200, noToken, withCookie, fromUI)
	var logs struct {
		Data []struct {
			Op    string `json:"op"`
			Src   string `json:"src"`
			Actor string `json:"actor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", "/api/ops/log?op=app.config.set&limit=1", "", 200).Body.Bytes(), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs.Data) != 1 || logs.Data[0].Src != "lan" || logs.Data[0].Actor != "127.0.0.1" {
		t.Errorf("只带 Cookie 的本机请求应记作局域网（127.0.0.1）：%+v", logs.Data)
	}
}

// 普通隧道（自定义 frps）的本地端口不能是 MineDeck 自己的网页端口：新增、修改都拒绝。启动时的检查见 tunnel 包的测试。
func TestTunnelRejectsWebPort(t *testing.T) {
	e := newFullEnv(t) // 网页端口 27333
	custom := func(port string) string {
		return `{"provider":"custom","name":"自建","serverAddr":"frp.example.com","serverPort":7000,"remotePort":30001,"localPort":` + port + `}`
	}
	if body := e.call(t, "POST", "/api/tunnels", custom("27333"), 400).Body.String(); !strings.Contains(body, "MineDeck 自己的网页端口") {
		t.Errorf("新增指向网页端口的隧道应被拒绝：%s", body)
	}
	var added struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "POST", "/api/tunnels", custom("25565"), 200).Body.Bytes(), &added); err != nil || added.Data.ID == "" {
		t.Fatalf("新增指向 MC 端口的隧道应成功：%v %+v", err, added)
	}
	if body := e.call(t, "PUT", "/api/tunnels/"+added.Data.ID, custom("27333"), 400).Body.String(); !strings.Contains(body, "MineDeck 自己的网页端口") {
		t.Errorf("把隧道改成指向网页端口应被拒绝：%s", body)
	}
	for _, st := range e.tunnels(t) {
		if st.ID == added.Data.ID && st.LocalPort != 25565 {
			t.Errorf("被拒绝的修改不该存下来：本地端口成了 %d", st.LocalPort)
		}
	}
}

type tunnelRow struct {
	ID        string `json:"id"`
	LocalPort int    `json:"localPort"`
}

func (e *fullEnv) tunnels(t *testing.T) []tunnelRow {
	t.Helper()
	var v struct {
		Data []tunnelRow `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", "/api/tunnels", "", 200).Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.Data
}
