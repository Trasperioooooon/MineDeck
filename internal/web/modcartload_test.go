package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/modcart"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/tunnel"
)

// cart.json 坏了（写到一半）：清单接口报 503，带一句给人看的 hint，不拿空清单冒充「清单是空的」；
// 加入、移除、清空、查前置、安装都报同一句（安装不说成「待装清单是空的」），原文件不动。
// 用户修好文件以后，不重启，清单接口就照常回来。
func TestModCartReportsUnreadableCart(t *testing.T) {
	e, mgr, _ := newCartEnv(t)
	e.call(t, "POST", "/api/modcart", `{"projectIds":["aaa"]}`, 200)
	path := filepath.Join(app.Base, "data", "modcart", "cart.json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := good[:len(good)/2]
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	// 换一份从坏文件起步的清单（相当于重启 MineDeck 时文件已经坏了）
	tun := tunnel.NewManager()
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Port: 27333,
		ModCart: modcart.New(filepath.Join(app.Base, "data", "modcart"), cartModrinth(t).URL)})
	e2 := &fullEnv{h: New(mgr, tun, reg, catalog.NewAssistant(reg, catalog.NewAI(), Token)), dials: e.dials}

	hint := (&modcart.LoadError{Corrupt: true}).Hint()
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/modcart", ""},
		{"POST", "/api/modcart", `{"projectIds":["aaa"]}`},
		{"POST", "/api/modcart/remove", `{"projectId":"aaa"}`},
		{"POST", "/api/modcart/clear", `{}`},
		{"GET", "/api/modcart/deps?core=fabric&mc=1.20.1", ""},
		{"POST", "/api/instances/" + url.PathEscape("模组服") + "/modcart/install", `{"includeDraft":true}`},
	} {
		v := decodeErr(t, e2.call(t, c.method, c.path, c.body, 503).Body.Bytes())
		if v.OK || v.Hint != hint || !strings.Contains(v.Error, "内容坏了") || !strings.Contains(v.Error, "行附近") {
			t.Fatalf("%s %s：%+v", c.method, c.path, v)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != string(broken) {
		t.Fatal("没读成时原文件被改了")
	}

	if err := os.WriteFile(path, good, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := data(t, e2.call(t, "GET", "/api/modcart", "", 200)); d["count"].(float64) != 1 {
		t.Fatalf("修好以后清单应读回来：%v", d)
	}
}
