package catalog

// 自选模组 + 购物车的操作。

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/inst"
	"minedeck/internal/modcart"
	"minedeck/internal/ops"
	"minedeck/internal/tasks"
	"minedeck/internal/tunnel"
)

// brokenDownloads 假 Modrinth 上下载不了的文件名（测试下载失败用）。
var brokenDownloads sync.Map

// fakeModrinth 只有两个项目的假 Modrinth：Alpha（aaa）必装 Fabric API（fapi），都只有 fabric 1.20.1 的版本。
func fakeModrinth(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	body := func(name string) []byte { return bytes.Repeat([]byte(name+";"), 400) }
	version := func(pid, fn string, deps ...map[string]any) map[string]any {
		sum := sha1.Sum(body(fn))
		if deps == nil {
			deps = []map[string]any{}
		}
		return map[string]any{"id": "v-" + pid, "project_id": pid, "version_number": "1.0", "version_type": "release",
			"date_published": "2026-01-01T00:00:00Z", "loaders": []string{"fabric"}, "game_versions": []string{"1.20.1"},
			"files":        []map[string]any{{"url": srv.URL + "/files/" + fn, "filename": fn, "primary": true, "size": len(body(fn)), "hashes": map[string]string{"sha1": hex.EncodeToString(sum[:])}}},
			"dependencies": deps}
	}
	projects := map[string]map[string]any{
		"aaa":  {"id": "aaa", "slug": "alpha", "title": "Alpha", "project_type": "mod", "server_side": "required", "license": map[string]any{"id": "MIT"}},
		"fapi": {"id": "fapi", "slug": "fabric-api", "title": "Fabric API", "project_type": "mod", "server_side": "optional", "license": map[string]any{"id": "Apache-2.0"}},
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch p := r.URL.Path; {
		case p == "/search":
			reply(map[string]any{"hits": []map[string]any{{"project_id": "aaa", "slug": "alpha", "title": "Alpha", "description": "d",
				"downloads": 9, "icon_url": "", "license": "MIT", "server_side": "required", "facets": r.URL.Query().Get("facets")}}})
		case p == "/projects":
			var ids []string
			_ = json.Unmarshal([]byte(r.URL.Query().Get("ids")), &ids)
			out := []map[string]any{}
			for _, id := range ids {
				for _, pr := range projects {
					if pr["id"] == id || pr["slug"] == id {
						out = append(out, pr)
					}
				}
			}
			reply(out)
		case p == "/project/aaa/version":
			reply([]map[string]any{version("aaa", "alpha-1.0.jar", map[string]any{"project_id": "fapi", "dependency_type": "required"})})
		case p == "/project/fapi/version":
			reply([]map[string]any{version("fapi", "fabric-api-1.0.jar")})
		case p == "/version_files":
			reply(map[string]any{})
		case strings.HasPrefix(p, "/files/"):
			fn := strings.TrimPrefix(p, "/files/")
			if _, broken := brokenDownloads.Load(fn); broken {
				http.Error(w, "坏了", http.StatusBadGateway)
				return
			}
			_, _ = w.Write(body(fn))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newCartCatalog 在 newCatalog 的基础上再放一个停着的 Fabric 1.20.1 实例「模组服」，mods 里有一个缺前置 ghost 的模组；
// 购物车接到假的 Modrinth 上。
func newCartCatalog(t *testing.T) (*ops.Registry, *inst.Manager, string, *modcart.Service) {
	t.Helper()
	newCatalog(t)
	srv := fakeModrinth(t)
	dir := filepath.Join(app.ServersDir, "s3")
	s, _ := json.Marshal(inst.Settings{Name: "模组服", Core: "fabric", MC: "1.20.1", XmxMB: 2048, XmsMB: 1024})
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, b := range map[string]string{filepath.Join(dir, "instance.json"): string(s), filepath.Join(dir, "server.properties"): "server-port=25566\n"} {
		if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("fabric.mod.json")
	_, _ = w.Write([]byte(`{"id":"needy","name":"Needy","version":"1.0","depends":{"ghost":"*","minecraft":"1.20.1"}}`))
	_ = zw.Close()
	if err := os.WriteFile(filepath.Join(dir, "mods", "needy.jar"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr, err := inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	cart := modcart.New(filepath.Join(app.Base, "data", "modcart"), srv.URL)
	return New(Env{Mgr: mgr, Tun: tunnel.NewManager(), Port: 27333, ModCart: cart}), mgr, dir, cart
}

var cartUI = ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}

func cartDo(t *testing.T, reg *ops.Registry, c ops.Caller, name string, args map[string]any) (map[string]any, error) {
	t.Helper()
	res, err := reg.Do(context.Background(), c, name, ops.ArgsOf(args))
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(res)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

// 购物车的读操作：搜索可以只给核心和版本（实例还没建）；加进草稿车；查前置；参数不对时说清楚。
func TestModCartReadOps(t *testing.T) {
	reg, _, _, _ := newCartCatalog(t)
	m, err := cartDo(t, reg, cartUI, "modcart.search", map[string]any{"q": "alpha", "core": "fabric", "mc": "1.20.1"})
	if err != nil || m["type"] != "mod" || len(m["hits"].([]any)) != 1 {
		t.Fatalf("按核心和版本搜：%v %v", m, err)
	}
	for args, want := range map[string]map[string]any{
		"要给实例名": {"q": "x"},
		"装不了":   {"q": "x", "core": "vanilla", "mc": "1.20.1"},
		"MC 版本": {"q": "x", "core": "fabric"},
	} {
		if _, err := cartDo(t, reg, cartUI, "modcart.search", want); err == nil || ops.CodeOf(err) != 400 || !strings.Contains(err.Error(), args) {
			t.Errorf("%v：应报 400「%s」，得到 %v", want, args, err)
		}
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.search", map[string]any{"q": "x", "inst": "没有这台"}); ops.CodeOf(err) != 404 {
		t.Errorf("实例不存在应报 404：%v", err)
	}

	m, err = cartDo(t, reg, cartUI, "modcart.add", map[string]any{"projectIds": []string{"alpha", "nope"}})
	if err != nil || m["count"].(float64) != 1 || len(m["skipped"].([]any)) != 1 {
		t.Fatalf("加进草稿车：%v %v", m, err)
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"projectIds": []string{}}); ops.CodeOf(err) != 400 {
		t.Errorf("空的 projectIds 应报 400：%v", err)
	}
	if m, _ = cartDo(t, reg, cartUI, "modcart.list", map[string]any{}); m["count"].(float64) != 1 {
		t.Errorf("草稿车：%v", m)
	}
	if m, _ = cartDo(t, reg, cartUI, "modcart.list", map[string]any{"inst": "模组服"}); m["count"].(float64) != 0 {
		t.Errorf("实例的车应是空的：%v", m)
	}

	m, err = cartDo(t, reg, cartUI, "modcart.deps", map[string]any{"core": "fabric", "mc": "1.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	files := m["files"].([]any)
	if len(files) != 2 || len(m["missing"].([]any)) != 1 {
		t.Fatalf("前置：%v", m)
	}
	for _, f := range files {
		if f := f.(map[string]any); f["url"] != nil || f["sha1"] != nil || f["fileName"] == "" {
			t.Errorf("给 AI 的结果不该带下载地址和哈希：%v", f)
		}
	}

	if m, _ = cartDo(t, reg, cartUI, "modcart.remove", map[string]any{"projectId": "aaa"}); m["count"].(float64) != 0 {
		t.Errorf("拿掉以后：%v", m)
	}
	_, _ = cartDo(t, reg, cartUI, "modcart.add", map[string]any{"projectIds": []string{"alpha"}})
	if m, _ = cartDo(t, reg, cartUI, "modcart.clear", map[string]any{}); m["count"].(float64) != 0 {
		t.Errorf("清空以后：%v", m)
	}

	m, err = cartDo(t, reg, cartUI, "instance.mods.deps", map[string]any{"inst": "模组服"})
	if err != nil || m["count"].(float64) != 1 || !strings.Contains(string(mustJSON(m["missing"])), "ghost") || m["checked"].(float64) != 1 {
		t.Fatalf("本地依赖：%v %v", m, err)
	}
	if _, err := cartDo(t, reg, cartUI, "instance.mods.deps", map[string]any{"inst": "生存服"}); err != nil {
		t.Errorf("插件服读 plugins：%v", err)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// waitTask 等后台任务结束。
func waitTask(t *testing.T, mgr *inst.Manager, id string) tasks.Snapshot {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if tk := mgr.Tasks.Get(id); tk != nil {
			if s := tk.Snapshot(); s.Ended {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("任务 %s 没结束", id)
	return tasks.Snapshot{}
}

// 人在界面上点「安装」：马上返回任务编号，后台装上购物车里的和必装前置，草稿车（includeDraft）一起装；
// 装上的从车里拿掉；还缺的前置写进任务的提示。同一台同时只装一批。
func TestModCartInstall(t *testing.T) {
	reg, mgr, dir, cart := newCartCatalog(t)
	if _, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服"}); ops.CodeOf(err) != 400 {
		t.Fatalf("空车应报 400：%v", err)
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"projectIds": []string{"aaa"}}); err != nil {
		t.Fatal(err)
	}
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服", "includeDraft": true})
	if err != nil {
		t.Fatal(err)
	}
	s := waitTask(t, mgr, m["taskId"].(string))
	if s.Err != "" || s.Kind != "mods" || s.Result != "模组服" {
		t.Fatalf("任务：%+v", s)
	}
	for _, fn := range []string{"alpha-1.0.jar", "fabric-api-1.0.jar"} {
		if _, err := os.Stat(filepath.Join(dir, "mods", fn)); err != nil {
			t.Errorf("没装上 %s", fn)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "mods"))
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".download") {
			t.Errorf("留下了临时文件 %s", e.Name())
		}
	}
	var still bool
	for _, w := range s.Warnings {
		if w.Kind == "still-missing-deps" && strings.Contains(w.Items[0].Name, "ghost") && strings.Contains(w.Items[0].Name, "Needy") {
			still = true
		}
	}
	if !still {
		t.Errorf("还缺的前置应写进提示：%+v", s.Warnings)
	}
	if m, _ = cartDo(t, reg, cartUI, "modcart.list", map[string]any{}); m["count"].(float64) != 0 {
		t.Errorf("装上的应从草稿车拿掉：%v", m)
	}

	// 再装一次：服务器里已经有了，跳过，不重复下载
	_, _ = cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}})
	m, _ = cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服"})
	s = waitTask(t, mgr, m["taskId"].(string))
	if s.Err != "" || !strings.Contains(strings.Join(s.Log, "\n"), "跳过") {
		t.Fatalf("已有的应跳过：%+v", s)
	}

	// 同一台正在装：409
	_, _ = cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}})
	release, err := cart.BeginInstall("模组服")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服"}); ops.CodeOf(err) != 409 {
		t.Errorf("同一台正在装应报 409：%v", err)
	}
	release()
}

// AI 发起安装：生成提案，卡片上逐个列出要下载的文件（含前置和许可证），前置条件是购物车的指纹：确认前车变了，提案作废。
func TestModCartInstallProposal(t *testing.T) {
	reg, _, _, _ := newCartCatalog(t)
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s1", Local: true}
	if _, err := cartDo(t, reg, ai, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"alpha"}}); err != nil {
		t.Fatalf("AI 能直接加进车（读操作）：%v", err)
	}
	res, err := reg.Do(context.Background(), ai, "modcart.install", ops.ArgsOf(map[string]any{"inst": "模组服"}))
	if err != nil {
		t.Fatal(err)
	}
	p, ok := res.(*ops.Proposal)
	if !ok {
		t.Fatalf("AI 发起的安装应生成提案：%T", res)
	}
	if len(p.Plan.Changes) != 2 || !strings.Contains(p.Plan.Changes[0].After, "mods/alpha-1.0.jar") ||
		!strings.Contains(p.Plan.Changes[0].After, "MIT") || !strings.Contains(p.Plan.Changes[1].What, "前置") ||
		!strings.Contains(p.Plan.Summary, "另补 1 个必装前置") {
		t.Fatalf("提案：%+v %+v", p.Plan, p.Plan.Changes)
	}
	// 确认前车里多了东西：提案作废
	if _, err := cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"fapi"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Confirm(context.Background(), cartUI, p.ID, false); ops.CodeOf(err) != 409 {
		t.Fatalf("车变了，确认应报 409：%v", err)
	}
}

// 复审补的：有文件没下载成功时购物车整辆不动（没下成的是 Alpha 的前置，拿掉 Alpha 下次就不会再补它）；
// 网好了再装一次，已经装上的跳过、只补没装上的，装完才拿掉。
func TestModCartInstallPartialFailure(t *testing.T) {
	reg, mgr, dir, _ := newCartCatalog(t)
	brokenDownloads.Store("fabric-api-1.0.jar", true)
	defer brokenDownloads.Delete("fabric-api-1.0.jar")
	_, _ = cartDo(t, reg, cartUI, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}})
	m, err := cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服"})
	if err != nil {
		t.Fatal(err)
	}
	s := waitTask(t, mgr, m["taskId"].(string))
	var warned bool
	for _, w := range s.Warnings {
		warned = warned || (w.Kind == "download-failed" && strings.Contains(w.Items[0].Name, "fabric-api-1.0.jar"))
	}
	if s.Err != "" || !warned {
		t.Fatalf("前置没下载成功应是带提示的完成：%+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "alpha-1.0.jar")); err != nil {
		t.Fatal("Alpha 本身应已装上")
	}
	if m, _ = cartDo(t, reg, cartUI, "modcart.list", map[string]any{"inst": "模组服"}); m["count"].(float64) != 1 {
		t.Fatalf("有没下载成功的，车不该动：%v", m)
	}

	brokenDownloads.Delete("fabric-api-1.0.jar")
	m, _ = cartDo(t, reg, cartUI, "modcart.install", map[string]any{"inst": "模组服"})
	s = waitTask(t, mgr, m["taskId"].(string))
	if s.Err != "" {
		t.Fatalf("再装一次：%+v", s)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "fabric-api-1.0.jar")); err != nil {
		t.Fatal("再装一次应补上 Fabric API")
	}
	if m, _ = cartDo(t, reg, cartUI, "modcart.list", map[string]any{"inst": "模组服"}); m["count"].(float64) != 0 {
		t.Fatalf("都装上了应从车里拿掉：%v", m)
	}

	// 这时车里的都在服务器上了：AI 发起安装不出空卡片，直接说明；deps 带上草稿车一起算
	ai := ops.Caller{Source: ops.SrcAssistant, Session: "s2", Local: true}
	_, _ = cartDo(t, reg, ai, "modcart.add", map[string]any{"inst": "模组服", "projectIds": []string{"aaa"}})
	if _, err := reg.Do(context.Background(), ai, "modcart.install", ops.ArgsOf(map[string]any{"inst": "模组服"})); ops.CodeOf(err) != 400 ||
		!strings.Contains(err.Error(), "已经有了") {
		t.Fatalf("没有要下载的应报 400 并说明：%v", err)
	}
	_, _ = cartDo(t, reg, ai, "modcart.add", map[string]any{"projectIds": []string{"fapi"}})
	m, err = cartDo(t, reg, ai, "modcart.deps", map[string]any{"inst": "模组服", "includeDraft": true})
	if err != nil || m["count"].(float64) != 2 || len(m["localHits"].([]any)) != 2 {
		t.Fatalf("deps 带草稿车：%v %v", m, err)
	}
}
