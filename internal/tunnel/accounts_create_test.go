package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// 在平台上创建隧道：平台的创建请求已经成功返回、后面没能导入本机的，报错都带 PlatformCreated（网页层据此在响应里加 created，
// 界面锁住「创建并导入」、引导去导入），说清平台上已经建好；平台上没建成的失败不带，界面照旧让人改了再试（A57）。

func platformCreated(err error) bool {
	var c interface{ PlatformCreated() bool }
	return errors.As(err, &c) && c.PlatformCreated()
}

func hintOf(err error) string {
	var h interface{ Hint() string }
	if errors.As(err, &h) {
		return h.Hint()
	}
	return ""
}

// 不是写不进磁盘的那几种，hint 都是同一句通用的
var createdHint = (&createdNotImportedError{err: errAccountChanged}).Hint()

// OpenFrp：newProxy 成功以后的每个出口（刷新列表失败、认不出新建的是哪条、取密钥失败、途中账户被断开）。
func TestOpenFrpCreateFailsAfterPlatformCreated(t *testing.T) {
	failOn := func(path string) func(*openFrpServer, *Accounts) {
		return func(f *openFrpServer, _ *Accounts) {
			f.onRequest = func(p string) {
				if p == path && f.created { // onRequest 在 f.mu 里调，这里直接改字段
					f.failNext = http.StatusInternalServerError
				}
			}
		}
	}
	for _, c := range []struct {
		name      string
		localPort int // 假平台列出来的新隧道本地端口是 25565：填别的就认不出是哪条
		arm       func(*openFrpServer, *Accounts)
		want      string
	}{
		{"建好以后刷新隧道列表失败", 25565, failOn("/frp/api/getUserProxies"), "刷新隧道列表失败"},
		{"认不出新建的是哪条", 25566, nil, "无法唯一识别新隧道"},
		{"取 frpc 密钥失败", 25565, failOn("/frp/api/getUserInfo"), "读取客户端密钥失败"},
		{"取完密钥账户被断开", 25565, func(f *openFrpServer, a *Accounts) {
			f.onRequest = func(p string) {
				if p == "/frp/api/getUserInfo" && f.created {
					if err := a.Disconnect("openfrp"); err != nil {
						t.Error(err)
					}
				}
			}
		}, "操作途中账户被断开或换了"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newOpenFrpServer(t)
			a, _ := testAccounts(t, f.srv.URL, f.srv.URL, f.srv.URL)
			loginOpenFrp(t, a)
			if c.arm != nil {
				c.arm(f, a)
			}
			_, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
				Provider: "openfrp", Name: "minedeck_ab12", NodeID: 3, Type: "tcp", LocalPort: c.localPort, RemotePort: 10002,
			})
			if !f.created {
				t.Fatal("平台上应已建好（测的就是建好以后的出口）")
			}
			if err == nil || !platformCreated(err) || !strings.Contains(err.Error(), c.want) ||
				!strings.HasPrefix(err.Error(), "平台上的隧道已经建好") || hintOf(err) != createdHint {
				t.Fatalf("应报平台上已建好、去导入，带 PlatformCreated：%v（hint %q）", err, hintOf(err))
			}
			if n := len(a.tunnels.List()); n != 0 {
				t.Fatalf("本机不该多出隧道：%d", n)
			}
		})
	}
}

// OpenFrp：平台上没建成的失败（节点不可用、newProxy 之前账户被断开、tunnels.json 没读成）不带 PlatformCreated。
func TestOpenFrpCreateFailsBeforePlatformCreated(t *testing.T) {
	f := newOpenFrpServer(t)
	a, _ := testAccounts(t, f.srv.URL, f.srv.URL, f.srv.URL)
	loginOpenFrp(t, a)
	ctx := context.Background()
	_, err := a.CreateTunnel(ctx, AccountTunnelOptions{Provider: "openfrp", Name: "minedeck_ab12", NodeID: 99, Type: "tcp", LocalPort: 25565, RemotePort: 10002})
	if err == nil || platformCreated(err) || f.created {
		t.Fatalf("节点不可用：平台上没建，不该带 PlatformCreated：%v", err)
	}
	f.onRequest = func(p string) {
		if p == "/frp/api/getUserProxies" {
			if err := a.Disconnect("openfrp"); err != nil {
				t.Error(err)
			}
		}
	}
	_, err = a.CreateTunnel(ctx, AccountTunnelOptions{Provider: "openfrp", Name: "minedeck_ab12", NodeID: 3, Type: "tcp", LocalPort: 25565, RemotePort: 10002})
	if err == nil || platformCreated(err) || f.created {
		t.Fatalf("newProxy 之前账户被断开：平台上没建，不该带 PlatformCreated：%v", err)
	}
}

// sakuraCreateServer 只有创建用到的三个接口的樱花假服务器：/user/info 回 frpc 用的 token，节点 1 是公开服专用、
// 节点 2 不是；POST /tunnels 怎么回由 create 决定。返回 POST /tunnels 被请求的次数。
func sakuraCreateServer(t *testing.T, token string, create func(w http.ResponseWriter)) (*httptest.Server, *atomic.Int32) {
	var posts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/user/info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": token, "name": "bob"})
	})
	mux.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"1": map[string]any{"name": "上海多线1", "flag": (1 << 2) | (1 << 5)},
			"2": map[string]any{"name": "北京三线", "flag": 1 << 2},
		})
	})
	mux.HandleFunc("/tunnels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		posts.Add(1)
		create(w)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &posts
}

// 樱花：POST /tunnels 成功以后的出口（没回编号、账户没有 frpc 密钥、途中账户被断开）带 PlatformCreated；
// POST 被拒、公开服用错节点（没发 POST）不带。写不进磁盘的那一种见 save_test.go。
func TestSakuraCreateFailuresAndPlatformCreated(t *testing.T) {
	createdOK := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": "mc"})
	}
	var a *Accounts
	for _, c := range []struct {
		name    string
		token   string // /user/info 给的 frpc 密钥
		create  func(w http.ResponseWriter)
		public  bool
		node    int
		created bool
		want    string
	}{
		{"建好了但没回编号", "sakura-access-key", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 0})
		}, false, 1, true, "没有返回新隧道编号"},
		{"建好了但账户没有 frpc 密钥", "", createdOK, false, 1, true, "没有可用于 frpc 的访问密钥"},
		{"建好以后账户被断开", "sakura-access-key", func(w http.ResponseWriter) {
			if err := a.Disconnect("natfrp"); err != nil {
				t.Error(err)
			}
			createdOK(w)
		}, false, 1, true, "操作途中账户被断开或换了"},
		{"平台拒绝创建", "sakura-access-key", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "msg": "隧道数量已达上限"})
		}, false, 1, false, "隧道数量已达上限"},
		{"公开服用错节点", "sakura-access-key", createdOK, true, 2, false, "公开服专用节点"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, posts := sakuraCreateServer(t, c.token, c.create)
			acc, keys := testAccounts(t, srv.URL, srv.URL, srv.URL)
			a = acc
			if err := keys.Set(sakuraAuthKey, "sakura-access-key"); err != nil { // 直接存：连接那一步要求 token 不为空
				t.Fatal(err)
			}
			_, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
				Provider: "natfrp", Name: "mc", NodeID: c.node, Type: "tcp", LocalPort: 25565, PublicServer: c.public,
			})
			if err == nil || !strings.Contains(err.Error(), c.want) || platformCreated(err) != c.created {
				t.Fatalf("报错：%v，PlatformCreated=%v，应为 %v", err, platformCreated(err), c.created)
			}
			if c.created && (!strings.HasPrefix(err.Error(), "平台上的隧道已经建好") || hintOf(err) != createdHint || posts.Load() != 1) {
				t.Fatalf("平台上已建好：要说清、给通用的 hint：%v（hint %q，POST %d 次）", err, hintOf(err), posts.Load())
			}
			if !c.created && strings.Contains(err.Error(), "平台上的隧道已经建好") {
				t.Fatalf("平台上没建成，不该说已经建好：%v", err)
			}
			if n := len(a.tunnels.List()); n != 0 {
				t.Fatalf("本机不该多出隧道：%d", n)
			}
		})
	}
}
