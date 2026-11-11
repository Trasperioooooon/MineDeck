package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"minedeck/internal/secret"
)

func testAccounts(t *testing.T, openAPI, openLogin, sakuraAPI string) (*Accounts, *secret.Store) {
	t.Helper()
	tempBase(t)
	keys := secret.NewStore(filepath.Join(t.TempDir(), "secrets.json"))
	tun := NewManager()
	a := NewAccounts(tun, keys)
	a.openAPI = openAPI
	a.openLogin = openLogin
	a.sakuraAPI = sakuraAPI
	a.now = func() time.Time { return time.Date(2026, 3, 20, 12, 0, 0, 0, time.Local) }
	return a, keys
}

func TestCaptchaAndPurchaseStatic(t *testing.T) {
	a, _ := testAccounts(t, "http://127.0.0.1", "http://127.0.0.1", "http://127.0.0.1")
	info, err := a.CaptchaInfo("openfrp")
	if err != nil || info.Mode != "token" || info.Vendor != "naids" || info.AppID == "" || !strings.Contains(info.SDKURL, "cv.naids.com") || !strings.Contains(info.OpenURL, "action=sign") {
		t.Fatalf("openfrp captcha: %+v %v", info, err)
	}
	info, err = a.CaptchaInfo("natfrp")
	if err != nil || info.Mode != "session" || !strings.Contains(info.OpenURL, "natfrp.com/user") {
		t.Fatalf("natfrp captcha: %+v %v", info, err)
	}
	web, err := a.WebLogin("natfrp")
	if err != nil || !web.NeedToken || !strings.Contains(web.URL, "/cgi/user/login") || !strings.Contains(web.Authorize, "openid.13a.com/oauth/authorize") {
		t.Fatalf("sakura weblogin: %+v %v", web, err)
	}
	po, err := a.PurchaseOptions(context.Background(), "openfrp")
	if err != nil || len(po.Options) == 0 || !strings.Contains(po.Options[0].URL, "console.openfrp.net") {
		t.Fatalf("openfrp purchase: %+v %v", po, err)
	}
	po, err = a.PurchaseOptions(context.Background(), "natfrp")
	if err != nil || len(po.Options) < 2 {
		t.Fatalf("natfrp purchase: %+v %v", po, err)
	}
}

func TestSignRequiresLoginOrToken(t *testing.T) {
	a, _ := testAccounts(t, "http://127.0.0.1", "http://127.0.0.1", "http://127.0.0.1")
	if _, err := a.Sign(context.Background(), "openfrp", SignPayload{}); err == nil || !strings.Contains(err.Error(), "人机验证") {
		t.Fatalf("缺 token 应失败: %v", err)
	}
	if _, err := a.Sign(context.Background(), "openfrp", SignPayload{Token: "tok"}); err == nil || !strings.Contains(err.Error(), "请先连接 OpenFrp") {
		t.Fatalf("未登录应失败: %v", err)
	}
	if _, err := a.Sign(context.Background(), "natfrp", SignPayload{}); err == nil || !strings.Contains(err.Error(), "请先连接 SakuraFrp") {
		t.Fatalf("Sakura 未登录应失败: %v", err)
	}
	if _, err := a.Overview(context.Background(), "openfrp"); err == nil || !strings.Contains(err.Error(), "请先连接 OpenFrp") {
		t.Fatalf("overview 未登录: %v", err)
	}
}

func TestOpenFrpLoginPollDecryptAndSign(t *testing.T) {
	var clientPub *[32]byte
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
		clientPub = &pub
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "ok",
			"data": map[string]string{
				"authorization_url": "https://console.openfrp.net/usercenter?argoaccess=req-1",
				"request_uuid":      "req-1",
			},
		})
	})
	mux.HandleFunc("/argoAccess/pollLogin", func(w http.ResponseWriter, r *http.Request) {
		if clientPub == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var nonce [24]byte
		_, _ = rand.Read(nonce[:])
		plain := []byte("openfrp-auth-token-xyz")
		sealed := box.Seal(nonce[:], plain, &nonce, clientPub, srvPriv)
		w.Header().Set("X-Request-Public-Key", base64.URLEncoding.EncodeToString(srvPub[:]))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "ok",
			"data": map[string]string{
				"authorization_data": base64.StdEncoding.EncodeToString(sealed),
				"request_uuid":       "req-1",
			},
		})
	})
	mux.HandleFunc("/frp/api/getUserInfo", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r, "openfrp-auth-token-xyz")
		writeOpenFrp(w, map[string]any{
			"token": "frpc-user-key", "username": "alice", "friendlyGroup": "普通用户",
			"realname": true, "traffic": 1024, "used": 1, "proxies": 5,
		})
	})
	mux.HandleFunc("/frp/api/getUserProxies", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r, "openfrp-auth-token-xyz")
		writeOpenFrp(w, map[string]any{"list": []map[string]any{{
			"id": 7, "proxyName": "mc", "proxyType": "tcp", "friendlyNode": "节点A",
			"localIp": "127.0.0.1", "localPort": 25565, "connectAddress": "a.example:10001",
			"nid": 3, "online": true, "status": true,
		}}})
	})
	mux.HandleFunc("/frp/api/getNodeList", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r, "openfrp-auth-token-xyz")
		writeOpenFrp(w, map[string]any{"list": []map[string]any{{
			"id": 3, "name": "节点A", "description": "测试", "status": 200,
			"protocolSupport": map[string]bool{"tcp": true, "udp": true}, "fullyLoaded": false,
		}}})
	})
	mux.HandleFunc("/frp/api/getSignInfo", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r, "openfrp-auth-token-xyz")
		writeOpenFrp(w, map[string]any{
			"signdate": "2026-03-20", "totalsign": 12, "totaltraffic": 100, "sign_min": 1,
		})
	})
	mux.HandleFunc("/frp/api/userSign", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r, "openfrp-auth-token-xyz")
		var body struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Token != "captcha-token-1" {
			writeOpenFrpFail(w, "bad captcha")
			return
		}
		writeOpenFrp(w, "签到成功，获得 10MiB 流量")
	})
	mux.HandleFunc("/frp/api/newProxy", func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r, "openfrp-auth-token-xyz")
		writeOpenFrp(w, nil)
	})

	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/frp/api/getUserProxies" && created {
			writeOpenFrp(w, map[string]any{"list": []map[string]any{
				{"id": 7, "proxyName": "mc", "proxyType": "tcp", "friendlyNode": "节点A", "localIp": "127.0.0.1", "localPort": 25565, "connectAddress": "a.example:10001", "nid": 3, "online": true, "status": true},
				{"id": 8, "proxyName": "新隧道", "proxyType": "tcp", "friendlyNode": "节点A", "localIp": "127.0.0.1", "localPort": 25565, "connectAddress": "a.example:10002", "nid": 3, "online": false, "status": true},
			}})
			return
		}
		if r.URL.Path == "/frp/api/newProxy" {
			created = true
		}
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()

	a, keys := testAccounts(t, srv.URL, srv.URL, srv.URL)
	login, err := a.BeginOpenFrpLogin(context.Background())
	if err != nil || login.ID != "req-1" || !strings.Contains(login.URL, "console.openfrp.net") {
		t.Fatalf("login begin: %+v %v", login, err)
	}
	poll, err := a.PollOpenFrpLogin(context.Background(), login.ID)
	if err != nil || poll.Pending {
		t.Fatalf("login poll: %+v %v", poll, err)
	}
	got, err := keys.Get(openFrpAuthKey)
	if err != nil || got != "openfrp-auth-token-xyz" {
		t.Fatalf("saved auth: %q %v", got, err)
	}

	ov, err := a.Overview(context.Background(), "openfrp")
	if err != nil || ov.Account.Name != "alice" || len(ov.Tunnels) != 1 || len(ov.Nodes) != 1 {
		t.Fatalf("overview: %+v %v", ov, err)
	}
	if ov.Account.SignedToday == nil || !*ov.Account.SignedToday || ov.Account.ConsecutiveDays != 12 {
		t.Fatalf("sign info on overview: %+v", ov.Account)
	}

	sign, err := a.Sign(context.Background(), "openfrp", SignPayload{Token: "captcha-token-1"})
	if err != nil || !sign.OK || !strings.Contains(sign.Message, "签到成功") {
		t.Fatalf("sign: %+v %v", sign, err)
	}

	st, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
		Provider: "openfrp", Name: "新隧道", NodeID: 3, Type: "tcp", LocalPort: 25565, RemotePort: 10002,
	})
	if err != nil || st.ProxyID != "8" || st.Provider != "openfrp" {
		t.Fatalf("create: %+v %v", st, err)
	}

	// 再导入已存在的 7 号（Create 已导入 8，7 尚未本地化）
	imp, err := a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "openfrp", ProxyID: "7"})
	if err != nil || imp.ProxyID != "7" {
		t.Fatalf("import: %+v %v", imp, err)
	}
}

func TestSakuraConnectOverviewSignNeedSession(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/info", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "no auth", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "frpc-sakura-token", "name": "bob", "realname": 1, "tunnels": 3,
			"group": map[string]any{"name": "普通"}, "traffic": []int64{10, 90},
			"sign": map[string]any{"signed": true, "days": 5},
		})
	})
	mux.HandleFunc("/nodes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"1": map[string]any{"name": "多线节点", "description": "公开服", "flag": (1 << 2) | (1 << 5) | (1 << 11)},
		})
	})
	mux.HandleFunc("/tunnels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 42, "name": "语音", "node": 1, "type": "udp", "status": 0, "online": false,
				"local_ip": "127.0.0.1", "local_port": 24454, "remote": "30000",
			})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 9, "name": "游戏", "node": 1, "type": "tcp", "status": 0, "online": true,
			"local_ip": "127.0.0.1", "local_port": 25565, "remote": "20000",
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a, keys := testAccounts(t, srv.URL, srv.URL, srv.URL)
	info, err := a.ConnectSakura(context.Background(), "sakura-access-key")
	if err != nil || info.Name != "bob" || info.SignedToday == nil || !*info.SignedToday {
		t.Fatalf("connect: %+v %v", info, err)
	}
	got, _ := keys.Get(sakuraAuthKey)
	if got != "sakura-access-key" {
		t.Fatalf("saved token: %q", got)
	}
	ov, err := a.Overview(context.Background(), "natfrp")
	if err != nil || len(ov.Nodes) != 1 || len(ov.Tunnels) != 1 {
		t.Fatalf("overview: %+v %v", ov, err)
	}
	sign, err := a.Sign(context.Background(), "natfrp", SignPayload{})
	if err != nil || sign.OK || !sign.NeedSession || !strings.Contains(sign.OpenURL, "natfrp.com/user") {
		t.Fatalf("sakura sign skeleton: %+v %v", sign, err)
	}
	st, err := a.CreateTunnel(context.Background(), AccountTunnelOptions{
		Provider: "natfrp", Name: "语音", NodeID: 1, Type: "udp", LocalPort: 24454, Purpose: PurposeVoice,
	})
	if err != nil || st.ProxyID != "42" {
		t.Fatalf("create: %+v %v", st, err)
	}
	imp, err := a.ImportTunnel(context.Background(), AccountTunnelImport{Provider: "natfrp", ProxyID: "9"})
	if err != nil || imp.ProxyID != "9" {
		t.Fatalf("import: %+v %v", imp, err)
	}
}

func assertAuth(t *testing.T, r *http.Request, want string) {
	t.Helper()
	if r.Header.Get("Authorization") != want {
		t.Errorf("%s Authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
	}
}

func writeOpenFrp(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"flag": true, "msg": "OK", "data": data})
}

func writeOpenFrpFail(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"flag": false, "msg": msg, "data": nil})
}
