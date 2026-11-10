package web

// 自选模组 + 购物车的接口：草稿车和实例的车、查前置、装进服务器（后台任务）、核对已装模组的依赖。

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/modcart"
	"minedeck/internal/ops/catalog"
	"minedeck/internal/tunnel"
)

// cartModrinth 假的 Modrinth：Alpha（aaa）必装 Fabric API（fapi），都只有 fabric 1.20.1 的版本。
func cartModrinth(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	body := func(fn string) []byte { return bytes.Repeat([]byte(fn+";"), 400) }
	ver := func(pid, fn string, deps []map[string]any) []map[string]any {
		sum := sha1.Sum(body(fn))
		return []map[string]any{{"id": "v-" + pid, "project_id": pid, "version_number": "1.0", "version_type": "release",
			"date_published": "2026-01-01T00:00:00Z", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"},
			"files":        []map[string]any{{"url": srv.URL + "/files/" + fn, "filename": fn, "primary": true, "size": len(body(fn)), "hashes": map[string]string{"sha1": hex.EncodeToString(sum[:])}}},
			"dependencies": deps}}
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch p := r.URL.Path; {
		case p == "/search":
			reply(map[string]any{"hits": []map[string]any{{"project_id": "aaa", "slug": "alpha", "title": "Alpha", "description": "d", "downloads": 9, "icon_url": "", "license": "MIT"}}})
		case p == "/projects":
			out := []map[string]any{}
			if ids := r.URL.Query().Get("ids"); strings.Contains(ids, "aaa") || strings.Contains(ids, "alpha") {
				out = append(out, map[string]any{"id": "aaa", "slug": "alpha", "title": "Alpha", "project_type": "mod", "server_side": "required", "license": map[string]any{"id": "MIT"}})
			}
			if strings.Contains(r.URL.Query().Get("ids"), "fapi") {
				out = append(out, map[string]any{"id": "fapi", "slug": "fabric-api", "title": "Fabric API", "project_type": "mod", "server_side": "optional", "license": map[string]any{"id": "Apache-2.0"}})
			}
			reply(out)
		case p == "/project/aaa/version":
			reply(ver("aaa", "alpha-1.0.jar", []map[string]any{{"project_id": "fapi", "dependency_type": "required"}}))
		case p == "/project/fapi/version":
			reply(ver("fapi", "fabric-api-1.0.jar", []map[string]any{}))
		case p == "/version_files":
			reply(map[string]any{})
		case strings.HasPrefix(p, "/files/"):
			_, _ = w.Write(body(strings.TrimPrefix(p, "/files/")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newCartEnv 在 newFullEnv 的基础上加一台 Fabric 1.20.1 的「模组服」，购物车接到假的 Modrinth 上。
func newCartEnv(t *testing.T) (*fullEnv, *inst.Manager, string) {
	t.Helper()
	e := newFullEnv(t)
	dir := filepath.Join(app.ServersDir, "s3")
	s, _ := json.Marshal(inst.Settings{Name: "模组服", Core: "fabric", MC: "1.20.1", XmxMB: 2048, XmsMB: 1024})
	for p, b := range map[string]string{filepath.Join(dir, "instance.json"): string(s), filepath.Join(dir, "server.properties"): "server-port=25566\n"} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	tun := tunnel.NewManager()
	reg := catalog.New(catalog.Env{Mgr: mgr, Tun: tun, Port: 27333,
		ModCart: modcart.New(filepath.Join(app.Base, "data", "modcart"), cartModrinth(t).URL)})
	return &fullEnv{h: New(mgr, tun, reg, catalog.NewAssistant(reg, catalog.NewAI(), Token)), dials: e.dials}, mgr, dir
}

// data 取响应里的 data。
func data(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.OK {
		t.Fatalf("响应不对：%s", rec.Body)
	}
	return resp.Data
}

func TestModCartRoutes(t *testing.T) {
	e, mgr, dir := newCartEnv(t)
	ip := "/api/instances/" + url.PathEscape("模组服")

	if d := data(t, e.call(t, "GET", "/api/modcart/search?q=alpha&core=fabric&mc=1.20.1", "", 200)); d["type"] != "mod" || len(d["hits"].([]any)) != 1 {
		t.Errorf("搜索：%v", d)
	}
	e.call(t, "GET", "/api/modcart/search?q=alpha", "", 400)
	e.call(t, "GET", "/api/modcart/search?q=alpha&inst=nope", "", 404)

	if d := data(t, e.call(t, "POST", "/api/modcart", `{"projectIds":["alpha"]}`, 200)); d["count"].(float64) != 1 {
		t.Errorf("加进草稿车：%v", d)
	}
	e.call(t, "POST", "/api/modcart", `{"projectIds":["aaa"],"inst":"nope"}`, 404)
	e.call(t, "POST", "/api/modcart", `{}`, 400)
	if d := data(t, e.call(t, "GET", "/api/modcart", "", 200)); d["count"].(float64) != 1 {
		t.Errorf("草稿车：%v", d)
	}
	if d := data(t, e.call(t, "GET", "/api/modcart?inst="+url.QueryEscape("模组服"), "", 200)); d["count"].(float64) != 0 {
		t.Errorf("实例的车：%v", d)
	}
	d := data(t, e.call(t, "GET", "/api/modcart/deps?core=fabric&mc=1.20.1", "", 200))
	if len(d["files"].([]any)) != 2 || len(d["missing"].([]any)) != 1 || d["localHits"] == nil {
		t.Errorf("查前置：%v", d)
	}
	if d := data(t, e.call(t, "POST", "/api/modcart/remove", `{"projectId":"aaa"}`, 200)); d["count"].(float64) != 0 {
		t.Errorf("拿掉：%v", d)
	}
	e.call(t, "POST", "/api/modcart", `{"projectIds":["aaa"]}`, 200)
	if d := data(t, e.call(t, "POST", "/api/modcart/clear", `{}`, 200)); d["count"].(float64) != 0 {
		t.Errorf("清空：%v", d)
	}

	// 向导的提交链：草稿车里放好，建好的实例带上 includeDraft 装
	e.call(t, "POST", "/api/modcart", `{"projectIds":["aaa"]}`, 200)
	e.call(t, "POST", "/api/instances/nope/modcart/install", `{"includeDraft":true}`, 404)
	id := data(t, e.call(t, "POST", ip+"/modcart/install", `{"includeDraft":true}`, 200))["taskId"].(string)
	deadline := time.Now().Add(20 * time.Second)
	for {
		tk := data(t, e.call(t, "GET", "/api/tasks/"+id, "", 200))
		if tk["ended"] == true {
			if tk["error"] != nil || tk["kind"] != "mods" || tk["result"] != "模组服" {
				t.Fatalf("任务：%v", tk)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务没结束")
		}
		time.Sleep(30 * time.Millisecond)
	}
	for _, fn := range []string{"alpha-1.0.jar", "fabric-api-1.0.jar"} {
		if _, err := os.Stat(filepath.Join(dir, "mods", fn)); err != nil {
			t.Errorf("没装上 %s", fn)
		}
	}
	if d := data(t, e.call(t, "GET", "/api/modcart", "", 200)); d["count"].(float64) != 0 {
		t.Errorf("装好后草稿车应清掉：%v", d)
	}
	e.call(t, "POST", ip+"/modcart/install", `{}`, 400) // 车空了

	if d := data(t, e.call(t, "GET", ip+"/mods/deps", "", 200)); d["count"].(float64) != 0 || d["checked"] == nil {
		t.Errorf("本地依赖：%v", d)
	}
	e.call(t, "GET", "/api/instances/nope/mods/deps", "", 404)
	_ = mgr
}
