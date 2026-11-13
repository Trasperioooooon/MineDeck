package ai

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// 重定向什么时候跟（Codex 第二轮）：只认同一个网站，或者标准的 http:80 → https:443 升级；
// 换端口、换主机、https 降回 http 都不跟（api-key、x-api-key 不会被 Go 自动去掉）。
func TestKeepKeyOnSite(t *testing.T) {
	req := func(s string) *http.Request {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	chain := func(urls ...string) (*http.Request, []*http.Request) {
		var via []*http.Request
		for _, s := range urls[:len(urls)-1] {
			via = append(via, req(s))
		}
		return req(urls[len(urls)-1]), via
	}
	for _, c := range []struct {
		name string
		urls []string
		ok   bool
	}{
		{"同网站换路径", []string{"https://api.a.com/v1/models", "https://api.a.com/v1/models-moved"}, true},
		{"主机名大小写", []string{"https://API.A.com/v1", "https://api.a.com/v2"}, true},
		{"写出默认端口", []string{"https://api.a.com:443/v1", "https://api.a.com/v2"}, true},
		{"前导零的默认端口", []string{"https://api.a.com:0443/v1", "https://api.a.com/v2"}, true},
		{"标准升级 80→443", []string{"http://api.a.com/v1", "https://api.a.com/v1"}, true},
		{"写出端口的标准升级", []string{"http://api.a.com:80/v1", "https://api.a.com:443/v1"}, true},
		{"IPv6 同网站", []string{"http://[::1]:8080/v1", "http://[::1]:8080/v2"}, true},
		{"换主机", []string{"https://api.a.com/v1", "https://evil.com/v1"}, false},
		{"同一台机器换端口", []string{"http://127.0.0.1:8080/v1", "http://127.0.0.1:9090/v1"}, false},
		{"换端口的升级", []string{"http://api.a.com:8080/v1", "https://api.a.com:9443/v1"}, false},
		{"https 降回 http", []string{"https://api.a.com/v1", "http://api.a.com/v1"}, false},
		{"升级后又降回去", []string{"http://api.a.com/v1", "https://api.a.com/v1", "http://api.a.com/v1"}, false},
		{"IPv6 换端口", []string{"http://[::1]:8080/v1", "http://[::1]:9090/v1"}, false},
	} {
		r, via := chain(c.urls...)
		err := keepKeyOnSite(r, via)
		if c.ok && err != nil {
			t.Errorf("%s：应该跟，结果 %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, http.ErrUseLastResponse) {
			t.Errorf("%s：不应该跟，结果 %v", c.name, err)
		}
	}
	// 10 次上限
	urls := make([]string, 11)
	for i := range urls {
		urls[i] = "https://api.a.com/v" + strings.Repeat("1", i+1)
	}
	if r, via := chain(urls...); keepKeyOnSite(r, via) == nil {
		t.Error("跳了 10 次还在跟")
	}
}

// 调用方自己的 CheckRedirect 照样生效；NewClient 不改调用方传进来的 http.Client。
func TestCheckRedirectComposedAndCallerUntouched(t *testing.T) {
	f := newFake(t, func(n int, _ recorded, w http.ResponseWriter) {
		if n == 0 {
			w.Header().Set("Location", "/v1/models-moved")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	})
	hc := f.srv.Client()
	calls := 0
	own := func(*http.Request, []*http.Request) error { calls++; return errors.New("调用方不让跟") }
	hc.CheckRedirect = own
	cl, _ := NewClient(Config{Protocol: OpenAI, BaseURL: f.srv.URL + "/v1", Key: "sk-test-0123456789", Model: "m", HTTP: hc})
	if _, err := cl.ListModels(context.Background()); err == nil || !strings.Contains(err.Error(), "调用方不让跟") || calls != 1 {
		t.Fatalf("同网站的重定向应交给调用方的 CheckRedirect：calls=%d err=%v", calls, err)
	}
	if hc.CheckRedirect == nil || hc.CheckRedirect(nil, nil) == nil || calls != 2 {
		t.Fatal("调用方的 http.Client 被改了")
	}
}

// 端口号的写法：前导零、默认端口都归一，和浏览器的 URL 一致（前后端认出的是不是同一个网站要对得上）。
func TestSiteNormalizesPort(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"https://api.a.com:0443/v1", "https://api.a.com/v1", true},
		{"http://api.a.com:080/v1", "http://api.a.com/v1", true},
		{"https://API.a.com:8443/x", "https://api.a.com:08443/y", true},
		{"https://api.a.com:8443/v1", "https://api.a.com/v1", false},
		{"http://api.a.com/v1", "https://api.a.com/v1", false},
		{"http://[::1]:8080/v1", "http://[::1]:8080/v2", true},
	} {
		if got := SameSite(c.a, c.b); got != c.same {
			t.Errorf("SameSite(%q, %q) = %v，应为 %v", c.a, c.b, got, c.same)
		}
	}
	if normPort("0443") != "443" || normPort("") != "" || normPort("abc") != "abc" {
		t.Errorf("normPort：%q %q %q", normPort("0443"), normPort(""), normPort("abc"))
	}
}

// 已经开始输出以后流里来了报错：当作断开，但原来那条报错的提示（这里是上下文超长）不能丢。
func TestDisconnectKeepsHint(t *testing.T) {
	f := newFake(t, func(_ int, _ recorded, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"先看一下"}}]}` + "\n\n" +
			`data: {"error":{"message":"This model's maximum context length is 8192 tokens","type":"invalid_request_error"}}` + "\n\n"))
	})
	cl, _ := NewClient(presetConfig(t, "openai", f.srv.URL, true))
	conv, _ := cl.NewConversation("", nil)
	_ = conv.AddUser("x")
	res, err := cl.Chat(context.Background(), conv, nil)
	if !errors.Is(err, ErrDisconnected) || res == nil || !res.Partial {
		t.Fatalf("应当作断开：%v %+v", err, res)
	}
	var ae *APIError
	if !errors.As(err, &ae) || !strings.Contains(HintOf(err), "对话太长") {
		t.Fatalf("断开后原来的报错和提示丢了：%v hint=%q", err, HintOf(err))
	}
}
