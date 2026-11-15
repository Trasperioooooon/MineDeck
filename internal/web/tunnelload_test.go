package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/tunnel"
)

type errBody struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Hint  string `json:"hint"`
}

func decodeErr(t *testing.T, b []byte) errBody {
	t.Helper()
	var v errBody
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// tunnels.json 坏了（写到一半）：隧道列表的接口报 503，带一句给人看的 hint，不拿空列表冒充「没有隧道」；
// 新增、平台创建都报同一句，原文件不动。用户修好文件以后，不重启，列表接口就照常回来。
func TestTunnelListReportsUnreadableConfig(t *testing.T) {
	e := newFullEnv(t)
	path := filepath.Join(app.Base, "tunnels.json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := good[:len(good)/2]
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	// 换一份从坏文件起步的隧道管理器（相当于重启 MineDeck 时文件已经坏了）
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	tun := tunnel.NewManager()
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Accounts: tunnel.NewAccounts(tun, app.Secrets()), Port: 27333})
	e2 := &fullEnv{h: New(mgr, tun, reg, catalog.NewAssistant(reg, catalog.NewAI(), Token)), dials: e.dials}

	hint := (&tunnel.LoadError{Corrupt: true}).Hint()
	v := decodeErr(t, e2.call(t, "GET", "/api/tunnels", "", 503).Body.Bytes())
	if v.OK || v.Hint != hint || !strings.Contains(v.Error, "内容坏了") || !strings.Contains(v.Error, "行附近") {
		t.Fatalf("列表接口：%+v", v)
	}
	v = decodeErr(t, e2.call(t, "POST", "/api/tunnels", `{"provider":"openfrp","name":"新隧道","credential":"openfrp-key-xyz","proxyId":"42"}`, 400).Body.Bytes())
	if v.Hint != hint || strings.Contains(v.Error, "openfrp-key-xyz") {
		t.Fatalf("新增接口：%+v", v)
	}
	v = decodeErr(t, e2.call(t, "POST", "/api/tunnel-accounts/openfrp/tunnels", `{"name":"mc","nodeId":1,"type":"tcp","localPort":25565,"remotePort":30000}`, 400).Body.Bytes())
	if v.Hint != hint || strings.Contains(v.Error, "平台上的隧道已经建好") {
		t.Fatalf("平台创建应在找平台之前拦下：%+v", v)
	}
	if got, _ := os.ReadFile(path); string(got) != string(broken) {
		t.Fatal("没读成时原文件被改了")
	}

	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	if rows := e2.tunnels(t); len(rows) != 1 || rows[0].ID != e.tunID {
		t.Fatalf("修好以后列表应读回来：%+v", rows)
	}
}
