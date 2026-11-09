package dl

import (
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
	pub := []string{"1.1.1.1", "8.8.8.8", "2001:4860:4860::8888"}
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

// TestGuardedClientRedirect 重定向级防线：跳转目标必须是 https、且主机在允许列表里。
func TestGuardedClientRedirect(t *testing.T) {
	allowed := func(h string) bool { return h == "cdn.modrinth.com" || h == "files.example.net" }
	client := GuardedClient(allowed)
	cr := client.CheckRedirect
	cases := []struct {
		name string
		from string
		want string // 错误文案须包含的字样（空串 = 放行）
	}{
		{"跳到 http", "http://cdn.modrinth.com/a", "不允许的地址"},
		{"跳到不在列表的站点", "https://evil.example.org/b", "允许列表之外"},
		{"跳到允许的站点", "https://files.example.net/c", ""},
	}
	for _, c := range cases {
		u, err := url.Parse(c.from)
		if err != nil {
			t.Fatal(err)
		}
		got := cr(&http.Request{URL: u}, nil)
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
	// allowed 为 nil：https 的重定向放行（拨号级公网检查还在）
	nilClient := GuardedClient(nil)
	u, _ := url.Parse("https://anywhere.example.org/x")
	if err := nilClient.CheckRedirect(&http.Request{URL: u}, nil); err != nil {
		t.Errorf("没有允许列表时 https 重定向应放行: %v", err)
	}
}
