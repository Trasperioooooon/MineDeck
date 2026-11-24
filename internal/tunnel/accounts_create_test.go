package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// 不是写不进磁盘的那几种，hint 都是同一句通用的；平台回了成功、MineDeck 确认不了的是另一句
var (
	createdHint     = (&createdNotImportedError{err: errAccountChanged}).Hint()
	unconfirmedHint = (&createdNotImportedError{err: &unconfirmedError{err: errAccountChanged}}).Hint()
)

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
	// 平台收到 path 这个请求时断开账户：这个请求照常回来，下一个请求才发现账户换了
	disconnectOn := func(path string) func(*openFrpServer, *Accounts) {
		return func(f *openFrpServer, a *Accounts) {
			f.onRequest = func(p string) {
				if p == path && f.created {
					if err := a.Disconnect("openfrp"); err != nil {
						t.Error(err)
					}
				}
			}
		}
	}
	for _, c := range []struct {
		name      string
		localPort int // 假平台列出来的新隧道本地端口是 25565：填别的就认不出是哪条
		arm       func(*openFrpServer, *Accounts)
		want      string
		is        error // 非空时 errors.Is 要认得出（经 createdNotImportedError 的 Unwrap）
	}{
		{"建好以后刷新隧道列表失败", 25565, failOn("/frp/api/getUserProxies"), "刷新隧道列表失败", nil},
		{"认不出新建的是哪条", 25566, nil, "无法唯一识别新隧道", nil},
		{"取 frpc 密钥失败", 25565, failOn("/frp/api/getUserInfo"), "读取客户端密钥失败", nil},
		{"刷新列表时账户被断开、取密钥那一步停下", 25565, disconnectOn("/frp/api/getUserProxies"), "读取客户端密钥失败", errAccountChanged},
		// 这一种是写进本机那一步（addLocalTunnel）自己报的，不带 errAccountChanged
		{"取完密钥账户被断开", 25565, disconnectOn("/frp/api/getUserInfo"), "操作途中账户被断开或换了", nil},
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
			if c.is != nil && !errors.Is(err, c.is) {
				t.Fatalf("errors.Is 应认得出 %v：%v", c.is, err)
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

// 樱花：POST /tunnels 回了成功以后的出口（没回编号、响应体看不懂、途中账户被断开）带 PlatformCreated；
// 平台拒绝（HTTP 400、2xx 里写着错误码）、公开服用错节点、账户没有 frpc 密钥（这两种都不发 POST）不带。
// 写不进磁盘的那一种见 save_test.go。
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
		created bool  // 平台上已经建好或多半已经建好
		unsure  bool  // 平台回了成功、MineDeck 却确认不了（报错叫人去平台列表里看）
		posts   int32 // POST /tunnels 该发几次
		want    string
	}{
		{"建好了但没回编号", "sakura-access-key", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 0})
		}, false, 1, true, false, 1, "没有返回新隧道编号"},
		{"回了成功、响应体不是 JSON", "sakura-access-key", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("<html>稍后再试</html>"))
		}, false, 1, true, true, 1, "SakuraFrp API 响应格式无效"},
		{"建好以后账户被断开", "sakura-access-key", func(w http.ResponseWriter) {
			if err := a.Disconnect("natfrp"); err != nil {
				t.Error(err)
			}
			createdOK(w)
		}, false, 1, true, false, 1, "操作途中账户被断开或换了"},
		{"平台拒绝创建", "sakura-access-key", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "msg": "隧道数量已达上限"})
		}, false, 1, false, false, 1, "隧道数量已达上限"},
		{"回了 2xx 但响应体里写着错误码", "sakura-access-key", func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 403, "msg": "隧道名称重复"})
		}, false, 1, false, false, 1, "隧道名称重复"},
		{"公开服用错节点", "sakura-access-key", createdOK, true, 2, false, false, 0, "公开服专用节点"},
		{"账户没有 frpc 密钥，先不建", "", createdOK, false, 1, false, false, 0, "账户没有可用于 frpc 的访问密钥，没有创建隧道"},
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
			if n := posts.Load(); n != c.posts {
				t.Fatalf("POST /tunnels 发了 %d 次，应为 %d", n, c.posts)
			}
			prefix, hint := "平台上的隧道已经建好", createdHint
			if c.unsure {
				prefix, hint = "平台回了成功，但", unconfirmedHint
			}
			if c.created && (!strings.HasPrefix(err.Error(), prefix) || hintOf(err) != hint) {
				t.Fatalf("平台上已建好或多半建好：开头应是 %q、hint 应是 %q：%v（hint %q）", prefix, hint, err, hintOf(err))
			}
			if !c.created && (strings.Contains(err.Error(), "已经建好") || strings.Contains(err.Error(), "平台回了成功")) {
				t.Fatalf("平台上没建成，不该说已经建好：%v", err)
			}
			if n := len(a.tunnels.List()); n != 0 {
				t.Fatalf("本机不该多出隧道：%d", n)
			}
		})
	}
}

// OpenFrp：newProxy 回了 2xx、没明说失败的，之后出什么错都按平台上多半已经建好算（响应体看不懂、读到一半断了、
// 换发的授权存不进去）：报错叫人去平台列表里看，带 PlatformCreated。flag 明说 false、HTTP 不是 2xx 的是平台没建成，不带（A57）。
func TestOpenFrpCreatedJudgedByNewProxyReply(t *testing.T) {
	reply := func(code int, flag bool, msg string) func(http.ResponseWriter) bool {
		return func(w http.ResponseWriter) bool {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"flag": flag, "msg": msg})
			return code < 300 && flag
		}
	}
	rotateThen := func(next func(http.ResponseWriter) bool) func(http.ResponseWriter) bool {
		return func(w http.ResponseWriter) bool {
			w.Header().Set("Authorization", "rotated-token-12345")
			return next(w)
		}
	}
	for _, c := range []struct {
		name     string
		reply    func(w http.ResponseWriter) bool
		lockKeys bool // 换发的授权存不进 secrets.json
		created  bool
		want     string
	}{
		{"回了成功、响应体不是 JSON", func(w http.ResponseWriter) bool {
			_, _ = w.Write([]byte("<html>稍后再试</html>"))
			return true
		}, false, true, "OpenFrp API 响应格式无效"},
		{"回了成功、读响应体时断了", func(w http.ResponseWriter) bool {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"flag":`))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // 响应头和半截响应体已经发出去，接着断开连接
		}, false, true, "读取穿透平台响应失败"},
		{"回了成功、换发的授权存不进去", rotateThen(reply(http.StatusOK, true, "OK")), true, true, "更新 OpenFrp 授权失败"},
		{"回了 2xx、flag 为 false", reply(http.StatusOK, false, "隧道名已被占用"), false, false, "隧道名已被占用"},
		{"flag 为 false、换发的授权也存不进去", rotateThen(reply(http.StatusOK, false, "隧道名已被占用")), true, false, "更新 OpenFrp 授权失败"},
		{"newProxy 本身失败（HTTP 500）", reply(http.StatusInternalServerError, false, "服务器开小差了"), false, false, "服务器开小差了"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newOpenFrpServer(t)
			a, keys, dir := testAccountsDir(t, f.srv.URL, f.srv.URL, f.srv.URL)
			loginOpenFrp(t, a)
			if c.lockKeys {
				// secret.Store.Set 先写 secrets.json.tmp 再改名：那个位置被一个文件夹占着就写不进去，读照常
				if err := os.Mkdir(filepath.Join(dir, "secrets.json.tmp"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := keys.Set("tunnel.probe", "x"); err == nil {
					t.Fatal("造不出「授权存不进去」：Set 应失败")
				}
			}
			f.newProxyReply = c.reply
			_, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
				Provider: "openfrp", Name: "minedeck_ab12", NodeID: 3, Type: "tcp", LocalPort: 25565, RemotePort: 10002,
			})
			if err == nil || !strings.Contains(err.Error(), c.want) || platformCreated(err) != c.created {
				t.Fatalf("报错：%v，PlatformCreated=%v，应为 %v", err, platformCreated(err), c.created)
			}
			if c.created && (!strings.HasPrefix(err.Error(), "平台回了成功，但") || hintOf(err) != unconfirmedHint) {
				t.Fatalf("平台回了成功、确认不了：要叫人去平台列表里看：%v（hint %q）", err, hintOf(err))
			}
			if !c.created && (strings.Contains(err.Error(), "已经建好") || strings.Contains(err.Error(), "平台回了成功")) {
				t.Fatalf("平台上没建成，不该说已经建好：%v", err)
			}
			if n := len(a.tunnels.List()); n != 0 {
				t.Fatalf("本机不该多出隧道：%d", n)
			}
		})
	}
}
