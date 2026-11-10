package dl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("解析 %s: %v", s, err)
	}
	return addr
}

// TestAddrIsPublic 公网判定的地址段（modpack.HostIsPublic 和连接级检查共用这一份语义）。
func TestAddrIsPublic(t *testing.T) {
	// 198.18.0.0/15 是 Clash、sing-box 的 fake-ip 段：开着这类代理时所有域名都解析到这里，必须放行
	pub := []string{"1.1.1.1", "8.8.8.8", "2001:4860:4860::8888", "198.18.0.1", "198.19.255.254"}
	for _, s := range pub {
		if !AddrIsPublic(mustAddr(t, s)) {
			t.Errorf("%s 应判为公网", s)
		}
	}
	priv := []string{
		"127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "172.16.0.1", "172.31.255.255",
		"169.254.169.254", "100.64.1.1", "100.127.255.255", "0.0.0.0", "255.255.255.255",
		"fe80::1", "fd00::1", "fc00::1", "224.0.0.1", "ff02::1",
	}
	for _, s := range priv {
		if AddrIsPublic(mustAddr(t, s)) {
			t.Errorf("%s 应判为内网/不可用", s)
		}
	}
}

// TestControlPublic 拨号级检查：连接真正建立之前挡掉所有非公网地址（本机、内网、云元数据段）。
func TestControlPublic(t *testing.T) {
	for _, bad := range []string{"10.0.0.5:443", "127.0.0.1:443", "[::1]:443", "169.254.169.254:80", "100.64.0.1:443"} {
		if err := controlPublic("tcp", bad, nil); err == nil {
			t.Errorf("%s 应被拒绝连接", bad)
		}
	}
	for _, ok := range []string{"1.1.1.1:443", "93.184.216.34:443", "[2606:4700:4700::1111]:443"} {
		if err := controlPublic("tcp", ok, nil); err != nil {
			t.Errorf("%s 应放行: %v", ok, err)
		}
	}
}

// TestGuardedClientDialReject 连接级防线（集成）：本机的 httptest 服务器监听在 127.0.0.1，
// 受控客户端取它时在拨号那一步被拒绝——下载前查过公网也拦得住解析之后变了向的本机地址。
func TestGuardedClientDialReject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("x"))
	}))
	defer srv.Close()
	client := GuardedClient(nil)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "非公网") {
		t.Fatalf("拨号到本机应被受控客户端拒绝，实际: %v", err)
	}
}

// TestGuardedClientRedirect 跳转的规矩：只跟 https 的，最多 maxRedirects 次；跳到哪个主机不限
// （GitHub 的发布文件、CurseForge 的文件都会跳到别的 CDN 域名，限了主机这两类整合包就导不进来）。
func TestGuardedClientRedirect(t *testing.T) {
	cr := GuardedClient(func(h string) bool { return h == "cdn.modrinth.com" }).CheckRedirect
	cases := []struct {
		name string
		to   string
		want string // 错误文案须包含的字样（空串 = 放行）
	}{
		{"跳到 http", "http://cdn.modrinth.com/a", "不允许的地址"},
		{"GitHub 发布文件的 CDN", "https://release-assets.githubusercontent.com/github-production-release-asset/1/2", ""},
		{"CurseForge 的 CDN", "https://mediafilez.forgecdn.net/files/4712/866/x.jar", ""},
		{"自定义站点自己的 CDN", "https://cdn.files.example.net/c", ""},
	}
	for _, c := range cases {
		u, err := url.Parse(c.to)
		if err != nil {
			t.Fatal(err)
		}
		got := cr(&http.Request{URL: u}, []*http.Request{{}})
		if c.want == "" {
			if got != nil {
				t.Errorf("%s 应放行: %v", c.name, got)
			}
			continue
		}
		if got == nil || !strings.Contains(got.Error(), c.want) {
			t.Errorf("%s 应拒绝（%s）: %v", c.name, c.want, got)
		}
	}
	// 跳转次数有上限：自己写了 CheckRedirect，net/http 默认的 10 次上限就不管了，两个站点互相跳会没完
	u, _ := url.Parse("https://files.example.net/loop")
	if err := cr(&http.Request{URL: u}, make([]*http.Request, maxRedirects-1)); err != nil {
		t.Errorf("第 %d 次跳转应放行: %v", maxRedirects-1, err)
	}
	if err := cr(&http.Request{URL: u}, make([]*http.Request, maxRedirects)); err == nil || !strings.Contains(err.Error(), "跳转超过") {
		t.Errorf("第 %d 次跳转应中断: %v", maxRedirects, err)
	}
}

// hopTransport 假的传输层：按地址给响应，记下每一跳请求的主机。
type hopTransport struct {
	hops  map[string]string // 地址 → 跳转目标；不在表里的地址回 200
	hosts []string
}

func (h *hopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	h.hosts = append(h.hosts, req.URL.Hostname())
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("jar")), Request: req}
	if to, ok := h.hops[req.URL.String()]; ok {
		resp.StatusCode = http.StatusFound
		resp.Header.Set("Location", to)
	}
	return resp, nil
}

// TestGuardedClientFollowsCDN 整条链路走一遍：白名单站点跳到名单外的 CDN 要跟过去拿到文件；
// 跳到 http 的中断。这两条是真实整合包里天天发生的事（2026-11-10 实测 github.com 的发布文件
// 跳 release-assets.githubusercontent.com，edge.forgecdn.net 跳 mediafilez.forgecdn.net）。
func TestGuardedClientFollowsCDN(t *testing.T) {
	rt := &hopTransport{hops: map[string]string{
		"https://github.com/o/r/releases/download/v1/a.jar": "https://release-assets.githubusercontent.com/asset/1",
		"https://edge.forgecdn.net/files/1/2/b.jar":         "https://mediafilez.forgecdn.net/files/1/2/b.jar",
		"https://files.example.net/c.jar":                   "http://files.example.net/c.jar",
	}}
	client := GuardedClientWithTransport(rt)
	for _, from := range []string{"https://github.com/o/r/releases/download/v1/a.jar", "https://edge.forgecdn.net/files/1/2/b.jar"} {
		resp, err := client.Get(from)
		if err != nil {
			t.Fatalf("%s 应跟着跳转拿到文件: %v", from, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(b) != "jar" {
			t.Errorf("%s 跳转后的响应不对: %d %q", from, resp.StatusCode, b)
		}
	}
	if got := strings.Join(rt.hosts, " "); got != "github.com release-assets.githubusercontent.com edge.forgecdn.net mediafilez.forgecdn.net" {
		t.Errorf("经过的主机不对: %s", got)
	}
	if _, err := client.Get("https://files.example.net/c.jar"); err == nil || !strings.Contains(err.Error(), "不允许的地址") {
		t.Errorf("跳到 http 应中断: %v", err)
	}
}

// TestGuardedTransportProxy 环境变量里配了代理时：连的是代理（常常就在 127.0.0.1），不能拿拨号级的
// 公网检查去拦它，否则配了 HTTP_PROXY 的人一个整合包文件都下不了；目标改按主机名查。
func TestGuardedTransportProxy(t *testing.T) {
	var direct, proxied []string
	mark := func(into *[]string) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			*into = append(*into, req.URL.Hostname())
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
		})
	}
	proxyURL, _ := url.Parse("http://127.0.0.1:7890")
	useProxy := true
	g := &guardedTransport{
		direct:  mark(&direct),
		proxied: mark(&proxied),
		proxyFor: func(*http.Request) (*url.URL, error) {
			if useProxy {
				return proxyURL, nil
			}
			return nil, nil
		},
		lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			switch host {
			case "files.example.net":
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			case "rebind.example.net": // 两条记录里混着一条内网的
				return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("192.168.1.1")}, nil
			case "fakeip.example.net":
				return []netip.Addr{netip.MustParseAddr("198.18.0.7")}, nil
			case "raw.githubusercontent.com": // 国内常见的污染结果
				return []netip.Addr{netip.MustParseAddr("0.0.0.0")}, nil
			}
			return nil, errors.New("no such host")
		},
		known: func(h string) bool { return h == "raw.githubusercontent.com" },
	}
	get := func(u string) error {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		resp, err := g.RoundTrip(req)
		if resp != nil {
			resp.Body.Close()
		}
		return err
	}
	for _, ok := range []string{
		"https://files.example.net/a.jar",         // 本机解析出来是公网
		"https://fakeip.example.net/a.jar",        // fake-ip 段
		"https://unresolvable.example.net/a.jar",  // 本机解析不出来：交给代理
		"https://raw.githubusercontent.com/o/r/a", // 内置站点不看本机的解析结果
		"https://93.184.216.34/a.jar",
	} {
		if err := get(ok); err != nil {
			t.Errorf("走代理时 %s 应放行: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"https://127.0.0.1/a", "https://192.168.1.1:8443/a", "https://[::1]/a", "https://169.254.169.254/latest/meta-data",
		"https://localhost/a", "https://a.localhost/a", "https://rebind.example.net/a",
	} {
		if err := get(bad); err == nil {
			t.Errorf("走代理时 %s 应拒绝", bad)
		}
	}
	if len(direct) != 0 {
		t.Errorf("配了代理不该走直连那条路: %v", direct)
	}
	if len(proxied) != 5 {
		t.Errorf("放行的 5 个都该交给代理，被拒的一个都不该发出去: %v", proxied)
	}
	// 没配代理：全部走直连那条路（公网检查在拨号时做，这里不按主机名拦）
	useProxy = false
	if err := get("https://files.example.net/a.jar"); err != nil || len(direct) != 1 {
		t.Errorf("没配代理应走直连: %v %v", err, direct)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
