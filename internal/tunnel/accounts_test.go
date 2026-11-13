package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"minedeck/internal/secret"
)

func testAccounts(t *testing.T, openAPI, openLogin, sakuraAPI string) (*Accounts, *secret.Store) {
	a, keys, _ := testAccountsDir(t, openAPI, openLogin, sakuraAPI)
	return a, keys
}

func testAccountsDir(t *testing.T, openAPI, openLogin, sakuraAPI string) (*Accounts, *secret.Store, string) {
	t.Helper()
	tempBase(t)
	dir := t.TempDir()
	keys := secret.NewStore(filepath.Join(dir, "secrets.json"))
	tun := NewManager()
	a := NewAccounts(tun, keys)
	a.openAPI = openAPI
	a.openLogin = openLogin
	a.sakuraAPI = sakuraAPI
	a.now = func() time.Time { return time.Date(2026, 3, 20, 12, 0, 0, 0, time.Local) }
	return a, keys, dir
}

func TestSignAndPurchaseOnlyPointToOfficialPages(t *testing.T) {
	a, _ := testAccounts(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")
	r, err := a.Sign("openfrp")
	if err != nil || r.OK || r.OpenURL != "https://console.openfrp.net/dashboard?action=sign" || r.Message == "" {
		t.Fatalf("openfrp sign: %+v %v", r, err)
	}
	r, err = a.Sign("natfrp")
	if err != nil || r.OK || r.OpenURL != "https://www.natfrp.com/user/" {
		t.Fatalf("natfrp sign: %+v %v", r, err)
	}
	if _, err := a.Sign("other"); err == nil {
		t.Fatal("未知平台应报错")
	}
	po, err := a.PurchaseOptions("openfrp")
	if err != nil || len(po.Options) == 0 || !strings.Contains(po.Options[0].URL, "console.openfrp.net") {
		t.Fatalf("openfrp purchase: %+v %v", po, err)
	}
	po, err = a.PurchaseOptions("natfrp")
	if err != nil || len(po.Options) < 2 {
		t.Fatalf("natfrp purchase: %+v %v", po, err)
	}
	for _, o := range po.Options {
		if !strings.HasPrefix(o.URL, "https://www.natfrp.com/") && !strings.HasPrefix(o.URL, "https://doc.natfrp.com/") {
			t.Errorf("樱花购买入口应只指向官网: %s", o.URL)
		}
	}
}

func TestOverviewRequiresLogin(t *testing.T) {
	a, _ := testAccounts(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")
	if _, err := a.Overview(context.Background(), "openfrp"); err == nil || !strings.Contains(err.Error(), "请先连接 OpenFrp") {
		t.Fatalf("openfrp overview 未登录: %v", err)
	}
	if _, err := a.Overview(context.Background(), "natfrp"); err == nil || !strings.Contains(err.Error(), "请先连接 SakuraFrp") {
		t.Fatalf("natfrp overview 未登录: %v", err)
	}
	if _, err := a.Overview(context.Background(), "other"); err == nil {
		t.Fatal("未知平台应报错")
	}
}

func TestStatusReportsLocalAndUnreadable(t *testing.T) {
	a, _, dir := testAccountsDir(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")
	st := a.Status(true)
	if st.OpenFrp || st.Sakura || !st.Local || st.OpenFrpUnreadable || st.SakuraUnreadable {
		t.Fatalf("空状态: %+v", st)
	}
	// 换了电脑或 Windows 用户：存着 dpapi 密文但解不开
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(`{"tunnel.openfrp.authorization":"dpapi:AAAA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st = a.Status(false)
	if st.OpenFrp || !st.OpenFrpUnreadable || st.Local {
		t.Fatalf("解不开的凭据应报 unreadable: %+v", st)
	}
}

// openFrpServer 模拟 OpenFrp 的登录网关和 OPENAPI。签到相关的接口（userSign、getSignInfo）一旦被调用就判失败。
type openFrpServer struct {
	t         *testing.T
	srv       *httptest.Server
	auth      string // 当前有效的 Authorization
	rotateTo  string // 非空时在下一次响应里带上这个新值（只带一次）
	keepAuth  bool   // 带了新值但服务端自己不换（模拟客户端理应拒收的新值）
	failNext  int    // 非零时下一次 API 请求按这个状态码失败（同样带上 rotateTo 头）
	onRequest func(path string)
	userInfo  atomic.Int32
	created   bool
	newName   string
	clientPub *[32]byte
}

func newOpenFrpServer(t *testing.T) *openFrpServer {
	f := &openFrpServer{t: t, auth: "openfrp-auth-token-xyz"}
	srvPub, srvPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/argoAccess/requestLogin", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PublicKey string `json:"public_key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		raw, err := base64.URLEncoding.DecodeString(body.PublicKey)
		if err != nil || len(raw) != 32 {
			t.Errorf("public_key: %v", err)
			http.Error(w, "bad key", 400)
			return
		}
		var pub [32]byte
		copy(pub[:], raw)
		f.clientPub = &pub
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "ok",
			"data": map[string]string{
				"authorization_url": "https://console.openfrp.net/usercenter?argoaccess=req-1",
				"request_uuid":      "req-1",
			},
		})
	})
	mux.HandleFunc("/argoAccess/pollLogin", func(w http.ResponseWriter, r *http.Request) {
		if f.onRequest != nil {
			f.onRequest(r.URL.Path)
		}
		if f.clientPub == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var nonce [24]byte
		_, _ = rand.Read(nonce[:])
		sealed := box.Seal(nonce[:], []byte(f.auth), &nonce, f.clientPub, srvPriv)
		w.Header().Set("X-Request-Public-Key", base64.URLEncoding.EncodeToString(srvPub[:]))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "ok",
			"data": map[string]string{"authorization_data": base64.StdEncoding.EncodeToString(sealed), "request_uuid": "req-1"},
		})
	})
	api := func(path string, data func() any) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != f.auth {
				t.Errorf("%s Authorization=%q, want %q", path, got, f.auth)
			}
			if !strings.HasPrefix(r.Header.Get("User-Agent"), "MineDeck/") {
				t.Errorf("%s 没带 MineDeck 的 UA: %q", path, r.Header.Get("User-Agent"))
			}
			if f.onRequest != nil {
				f.onRequest(path)
			}
			rotated := f.rotateTo
			f.rotateTo = ""
			if rotated != "" {
				w.Header().Set("Authorization", rotated)
			}
			if f.failNext != 0 {
				code := f.failNext
				f.failNext = 0
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"flag": false, "msg": "服务器开小差了"})
				return
			}
			if rotated != "" && !f.keepAuth {
				f.auth = rotated
			}
			writeOpenFrp(w, data())
		})
	}
	api("/frp/api/getUserInfo", func() any {
		f.userInfo.Add(1)
		return map[string]any{
			"token": "frpc-user-key", "username": "alice", "friendlyGroup": "普通用户",
			"realname": true, "traffic": 1024, "used": 1, "proxies": 5,
		}
	})
	api("/frp/api/getUserProxies", func() any {
		list := []map[string]any{{
			"id": 7, "proxyName": "mc", "proxyType": "tcp", "friendlyNode": "节点A",
			"localIp": "127.0.0.1", "localPort": 25565, "connectAddress": "a.example:10001",
			"nid": 3, "online": true, "status": true,
		}}
		if f.created {
			list = append(list, map[string]any{
				"id": 8, "proxyName": f.newName, "proxyType": "tcp", "friendlyNode": "节点A",
				"localIp": "127.0.0.1", "localPort": 25565, "connectAddress": "a.example:10002",
				"nid": 3, "online": false, "status": true,
			})
		}
		return map[string]any{"list": list}
	})
	api("/frp/api/getNodeList", func() any {
		return map[string]any{"list": []map[string]any{{
			"id": 3, "name": "节点A", "description": "测试", "status": 200,
			"protocolSupport": map[string]bool{"tcp": true, "udp": true}, "fullyLoaded": false,
		}}}
	})
	mux.HandleFunc("/frp/api/newProxy", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		name, _ := body["name"].(string)
		if !openFrpTunnelName.MatchString(name) {
			t.Errorf("不合规的隧道名不应发到 OpenFrp: %q", name)
		}
		f.created, f.newName = true, name
		writeOpenFrp(w, nil)
	})
	for _, p := range []string{"/frp/api/userSign", "/frp/api/getSignInfo"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("MineDeck 不应调用 %s", r.URL.Path)
			writeOpenFrp(w, nil)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func loginOpenFrp(t *testing.T, a *Accounts) {
	t.Helper()
	login, err := a.BeginOpenFrpLogin(context.Background())
	if err != nil || login.ID != "req-1" || !strings.Contains(login.URL, "console.openfrp.net") {
		t.Fatalf("login begin: %+v %v", login, err)
	}
	poll, err := a.PollOpenFrpLogin(context.Background(), login.ID)
	if err != nil || poll.Pending {
		t.Fatalf("login poll: %+v %v", poll, err)
	}
}

func TestOpenFrpLoginOverviewCreateImport(t *testing.T) {
	f := newOpenFrpServer(t)
	a, keys := testAccounts(t, f.srv.URL, f.srv.URL, f.srv.URL)
	loginOpenFrp(t, a)
	if got, err := keys.Get(openFrpAuthKey); err != nil || got != "openfrp-auth-token-xyz" {
		t.Fatalf("saved auth: %q %v", got, err)
	}
	if st := a.Status(true); !st.OpenFrp {
		t.Fatalf("登录后应为已连接: %+v", st)
	}

	ov, err := a.Overview(context.Background(), "openfrp")
	if err != nil || ov.Account.Name != "alice" || len(ov.Tunnels) != 1 || len(ov.Nodes) != 1 {
		t.Fatalf("overview: %+v %v", ov, err)
	}
	if ov.Account.SignedToday != nil || ov.Account.ConsecutiveDays != 0 {
		t.Fatalf("OpenFrp 概览不应再带签到状态: %+v", ov.Account)
	}
	// 30 秒内再看概览走缓存，不再打平台
	if _, err := a.Overview(context.Background(), "openfrp"); err != nil {
		t.Fatal(err)
	}
	if n := f.userInfo.Load(); n != 1 {
		t.Fatalf("概览应走缓存，getUserInfo 被调了 %d 次", n)
	}

	// 中文名在发请求之前就被拒
	if _, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
		Provider: "openfrp", Name: "我的隧道", NodeID: 3, Type: "tcp", LocalPort: 25565, RemotePort: 10002,
	}); err == nil || !strings.Contains(err.Error(), "英文字母") {
		t.Fatalf("中文隧道名应被拒: %v", err)
	}
	if f.created {
		t.Fatal("中文隧道名不应发到 OpenFrp")
	}

	st, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
		Provider: "openfrp", Name: "minedeck_ab12", NodeID: 3, Type: "tcp", LocalPort: 25565, RemotePort: 10002,
	})
	if err != nil || st.ProxyID != "8" || st.Provider != "openfrp" || st.Credential != "" {
		t.Fatalf("create: %+v %v", st, err)
	}
	// 创建后概览重新拉，能看到新隧道
	ov, err = a.Overview(context.Background(), "openfrp")
	if err != nil || len(ov.Tunnels) != 2 {
		t.Fatalf("创建后概览应刷新: %+v %v", ov.Tunnels, err)
	}

	// 「007」规范成 7，再导入「7」就是重复
	imp, err := a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "openfrp", ProxyID: "007"})
	if err != nil || imp.ProxyID != "7" {
		t.Fatalf("import: %+v %v", imp, err)
	}
	if _, err := a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "openfrp", ProxyID: "7"}); err == nil || !strings.Contains(err.Error(), "已经添加") {
		t.Fatalf("重复导入应被拒: %v", err)
	}
	for _, bad := range []string{"+7", "-7", "0", "abc", ""} {
		if _, err := a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "openfrp", ProxyID: bad}); err == nil || !strings.Contains(err.Error(), "编号无效") {
			t.Errorf("编号 %q 应被拒: %v", bad, err)
		}
	}
}

func TestOpenFrpAuthorizationRotation(t *testing.T) {
	f := newOpenFrpServer(t)
	a, keys := testAccounts(t, f.srv.URL, f.srv.URL, f.srv.URL)
	loginOpenFrp(t, a)

	// 失败响应里带的新值不收
	f.rotateTo, f.failNext = "rotated-on-error-1", http.StatusInternalServerError
	if _, err := a.Overview(context.Background(), "openfrp"); err == nil {
		t.Fatal("500 应报错")
	}
	if got, _ := keys.Get(openFrpAuthKey); got != "openfrp-auth-token-xyz" {
		t.Fatalf("失败响应不应改写凭据: %q", got)
	}
	// 成功响应里太短的新值也不收
	f.rotateTo, f.keepAuth = "short", true
	if _, err := a.Overview(context.Background(), "openfrp"); err != nil {
		t.Fatal(err)
	}
	if got, _ := keys.Get(openFrpAuthKey); got != "openfrp-auth-token-xyz" {
		t.Fatalf("不像样的新值不应保存: %q", got)
	}
	f.keepAuth = false
	a.forgetOverview("openfrp")
	// 成功响应里像样的新值才收
	f.rotateTo = "rotated-token-12345"
	if _, err := a.Overview(context.Background(), "openfrp"); err != nil {
		t.Fatal(err)
	}
	if got, _ := keys.Get(openFrpAuthKey); got != "rotated-token-12345" {
		t.Fatalf("成功响应里的新值应保存: %q", got)
	}
}

func TestOpenFrpDisconnectWinsOverInFlightRequests(t *testing.T) {
	f := newOpenFrpServer(t)
	a, keys := testAccounts(t, f.srv.URL, f.srv.URL, f.srv.URL)

	// 授权轮询在途时断开：回来的授权不能写进去
	login, err := a.BeginOpenFrpLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.onRequest = func(path string) {
		if path == "/argoAccess/pollLogin" {
			if err := a.Disconnect("openfrp"); err != nil {
				t.Error(err)
			}
		}
	}
	if _, err := a.PollOpenFrpLogin(context.Background(), login.ID); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("断开后的轮询应报已取消: %v", err)
	}
	if got, _ := keys.Get(openFrpAuthKey); got != "" {
		t.Fatalf("断开后不应写入授权: %q", got)
	}

	// 普通请求在途时断开：响应里换发的新值不能把账户连回去
	f.onRequest = nil
	loginOpenFrp(t, a)
	f.rotateTo = "rotated-token-12345"
	f.onRequest = func(path string) {
		if path == "/frp/api/getUserInfo" {
			if err := a.Disconnect("openfrp"); err != nil {
				t.Error(err)
			}
		}
	}
	_, _ = a.Overview(context.Background(), "openfrp")
	if got, _ := keys.Get(openFrpAuthKey); got != "" {
		t.Fatalf("断开后换发的授权不应写回: %q", got)
	}
}

// sakuraServer 模拟樱花 API v4。节点 1 是公开服专用（名称含「多线」），节点 2 只能 TCP、不能放公开服。
func sakuraServer(t *testing.T, onInfo func()) (*httptest.Server, *map[string]any) {
	var lastCreate map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/user/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sakura-access-key" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "msg": "访问密钥无效"})
			return
		}
		if onInfo != nil {
			onInfo()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "sakura-access-key", "name": "bob", "realname": 1, "tunnels": 3,
			"group": map[string]any{"name": "普通"}, "traffic": []int64{10, 90},
			"sign": map[string]any{"signed": true, "days": 5},
		})
	})
	mux.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"1": map[string]any{"name": "上海多线1", "description": "公开服", "flag": (1 << 2) | (1 << 5)},
			"2": map[string]any{"name": "北京三线", "description": "仅 TCP", "flag": 1 << 2},
		})
	})
	mux.HandleFunc("/tunnels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				t.Errorf("创建隧道的 Content-Type: %q", r.Header.Get("Content-Type"))
			}
			_ = json.NewDecoder(r.Body).Decode(&lastCreate)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "name": lastCreate["name"], "remote": "30000"})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 9, "name": "游戏", "node": 1, "type": "tcp", "status": 0, "online": true,
			"local_ip": "127.0.0.1", "local_port": 25565, "remote": "20000",
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &lastCreate
}

func TestSakuraConnectOverviewCreateImport(t *testing.T) {
	srv, lastCreate := sakuraServer(t, nil)
	a, keys := testAccounts(t, srv.URL, srv.URL, srv.URL)
	if _, err := a.ConnectSakura(context.Background(), "wrong-access-key"); err == nil {
		t.Fatal("错误的访问密钥应连不上")
	}
	if got, _ := keys.Get(sakuraAuthKey); got != "" {
		t.Fatalf("连不上时不应保存: %q", got)
	}
	info, err := a.ConnectSakura(context.Background(), "Bearer sakura-access-key")
	if err != nil || info.Name != "bob" || info.SignedToday == nil || !*info.SignedToday || info.ConsecutiveDays != 5 {
		t.Fatalf("connect: %+v %v", info, err)
	}
	if got, _ := keys.Get(sakuraAuthKey); got != "sakura-access-key" {
		t.Fatalf("saved token: %q", got)
	}
	ov, err := a.Overview(context.Background(), "natfrp")
	if err != nil || len(ov.Nodes) != 2 || len(ov.Tunnels) != 1 {
		t.Fatalf("overview: %+v %v", ov, err)
	}
	public := map[int]bool{}
	for _, n := range ov.Nodes {
		public[n.ID] = n.PublicGameAllowed
	}
	if !public[1] || public[2] {
		t.Fatalf("公开服节点判定（含「多线」可以，「三线」不行）: %+v", public)
	}

	// 公开服务器不能放在非公开服节点上
	if _, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
		Provider: "natfrp", Name: "mc", NodeID: 2, Type: "tcp", LocalPort: 25565, PublicServer: true,
	}); err == nil || !strings.Contains(err.Error(), "公开服专用节点") {
		t.Fatalf("公开服用错节点应被拒: %v", err)
	}
	if *lastCreate != nil {
		t.Fatal("被拒的创建不应发到樱花")
	}
	st, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
		Provider: "natfrp", Name: "语音", NodeID: 1, Type: "udp", LocalPort: 24454, Purpose: PurposeVoice, PublicServer: true,
	})
	if err != nil || st.ProxyID != "42" || st.Purpose != PurposeVoice {
		t.Fatalf("create: %+v %v", st, err)
	}
	got := *lastCreate
	if got["name"] != "语音" || got["type"] != "udp" || got["node"] != float64(1) || got["local_ip"] != "127.0.0.1" || got["local_port"] != float64(24454) {
		t.Fatalf("创建请求的字段: %+v", got)
	}
	if _, ok := got["remote"]; ok {
		t.Fatalf("没填公网端口时不应带 remote: %+v", got)
	}
	imp, err := a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "natfrp", ProxyID: "9"})
	if err != nil || imp.ProxyID != "9" {
		t.Fatalf("import: %+v %v", imp, err)
	}
}

func TestSakuraDisconnectDuringConnect(t *testing.T) {
	var a *Accounts
	srv, _ := sakuraServer(t, func() {
		if err := a.Disconnect("natfrp"); err != nil {
			t.Error(err)
		}
	})
	var keys *secret.Store
	a, keys = testAccounts(t, srv.URL, srv.URL, srv.URL)
	if _, err := a.ConnectSakura(context.Background(), "sakura-access-key"); err == nil || !strings.Contains(err.Error(), "重新连接") {
		t.Fatalf("连接途中被断开应报错: %v", err)
	}
	if got, _ := keys.Get(sakuraAuthKey); got != "" {
		t.Fatalf("断开后不应写入访问密钥: %q", got)
	}
}

func TestResponseErrorMessages(t *testing.T) {
	if err := responseError(http.StatusTooManyRequests, nil); !strings.Contains(err.Error(), "太频繁") {
		t.Fatalf("429: %v", err)
	}
	long := strings.Repeat("错", 200)
	msg := safeMessage(long)
	if len([]rune(msg)) != 120 || !strings.HasPrefix(long, msg) {
		t.Fatalf("按字符截断: %d", len([]rune(msg)))
	}
	if !validAuthorization("12345678") || validAuthorization("short") || validAuthorization("abcdefgh\r\nX: y") {
		t.Fatal("validAuthorization")
	}
}

func writeOpenFrp(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"flag": true, "msg": "OK", "data": data})
}
