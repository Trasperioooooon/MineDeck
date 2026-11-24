package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// platformRoundTrip 把发往 host 的请求改写到本机的假平台，别的地址一律不连。
type platformRoundTrip struct {
	host   string
	target *url.URL
	next   http.RoundTripper
}

func (p platformRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != p.host {
		return nil, fmt.Errorf("测试里不许联网：%s", r.URL.Host)
	}
	clone := r.Clone(r.Context())
	clone.URL.Scheme, clone.URL.Host = p.target.Scheme, p.target.Host
	return p.next.RoundTrip(clone)
}

// 在平台上创建隧道走真路由：平台回了成功、MineDeck 没能导入的，响应 JSON 带 created: true（界面据此锁住「创建并导入」、
// 引导去导入）。以前只在 Accounts.CreateTunnel 和 writeErr 两头各测过，中间的路由和操作层没走过（A57）。
func TestPlatformCreateRouteReportsCreated(t *testing.T) {
	e := newFullEnv(t)
	var posts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/user/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "fake-frpc-key-123", "name": "bob"})
	})
	mux.HandleFunc("/v4/nodes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"1": map[string]any{"name": "上海多线1", "flag": (1 << 2) | (1 << 5)}})
	})
	mux.HandleFunc("/v4/tunnels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		posts.Add(1)
		w.WriteHeader(http.StatusCreated) // 建好了，却没回新隧道的编号：本机导入不了
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 0})
	})
	fake := httptest.NewServer(mux)
	t.Cleanup(fake.Close)
	target, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	// 平台账户的 http.Client 没设 Transport，用的是 http.DefaultTransport：这条用例里把樱花的域名改写到假平台
	old := http.DefaultTransport
	http.DefaultTransport = platformRoundTrip{host: "api.natfrp.com", target: target, next: old}
	t.Cleanup(func() { http.DefaultTransport = old })

	e.call(t, "POST", "/api/tunnel-accounts/natfrp/connect", `{"token":"fake-sakura-access-key"}`, 200)
	rec := e.call(t, "POST", "/api/tunnel-accounts/natfrp/tunnels", `{"name":"mc","nodeId":1,"type":"tcp","localPort":25565}`, 400)
	var v struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Hint    string `json:"hint"`
		Created bool   `json:"created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.OK || !v.Created || !strings.Contains(v.Error, "平台上的隧道已经建好") || !strings.Contains(v.Hint, "不要再创建") || posts.Load() != 1 {
		t.Fatalf("平台上建好、没导入成：响应要带 created: true 和去导入的 hint（POST %d 次）：%s", posts.Load(), rec.Body)
	}
}
