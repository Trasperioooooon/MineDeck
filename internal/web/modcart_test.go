package web

// 自选模组 + 购物车的接口：草稿车和实例的车、查前置、装进服务器（后台任务）、核对已装模组的依赖。

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
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

// localJarBytes 一个只有 fabric.mod.json 的 jar。
func localJarBytes(t *testing.T, id string, depends map[string]string) []byte {
	t.Helper()
	meta, _ := json.Marshal(map[string]any{"id": id, "name": "Mod " + id, "version": "1.0", "depends": depends})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("fabric.mod.json")
	_, _ = w.Write(meta)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// uploadLocal 像界面那样用 multipart 上传一个 jar（file，另带 inst / core / mc）。
func (e *fullEnv) uploadLocal(t *testing.T, fields map[string]string, filename string, content []byte, want int, opts ...reqOpt) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if filename != "" {
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(content)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/modcart/local", bytes.NewReader(body.Bytes()))
	req.Host = "127.0.0.1"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("X-Token", Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Errorf("上传 %s：HTTP %d，应为 %d：%s", filename, rec.Code, want, rec.Body)
	}
	return rec
}

// 热门清单和本地文件的接口：响应的形状就是界面（modcart.js）读的那些字段，这里按字段名逐个核对——
// 界面测试用的是仿真后端，字段名对不上只有这里查得出来。
func TestModCartHotListAndLocalRoutes(t *testing.T) {
	e, _, dir := newCartEnv(t)
	ip := "/api/instances/" + url.PathEscape("模组服")
	uploads := filepath.Join(app.CacheDir, "modcart-upload")
	noLeftover := func(when string) {
		t.Helper()
		if ents, _ := os.ReadDir(uploads); len(ents) != 0 {
			t.Errorf("%s：上传目录里不该留下临时文件：%v", when, ents)
		}
	}

	// 热门清单：关键词留空，带类别和 offset；界面读 hits / more / offset / total
	d := data(t, e.call(t, "GET", "/api/modcart/search?core=fabric&mc=1.20.1&category=optimization&offset=20", "", 200))
	for _, k := range []string{"type", "hits", "more", "offset", "total"} {
		if _, ok := d[k]; !ok {
			t.Errorf("搜索结果缺字段 %s：%v", k, d)
		}
	}
	if d["offset"].(float64) != 20 || d["more"] != false {
		t.Errorf("搜索结果：%v", d)
	}
	for _, q := range []string{"offset=abc", "offset=100", "offset=-20", "category=nsfw", "category=economy"} {
		e.call(t, "GET", "/api/modcart/search?core=fabric&mc=1.20.1&"+q, "", 400)
	}

	// 从电脑添加：返回整份清单，local 的每一项有 sha1 / fileName / size / title / loader，另有 added 和 duplicate
	jar := localJarBytes(t, "mine", map[string]string{"ghostlib": "*"})
	d = data(t, e.uploadLocal(t, map[string]string{"core": "fabric", "mc": "1.20.1"}, "我的模组 mine-1.0.jar", jar, 200))
	local, _ := d["local"].([]any)
	if d["count"].(float64) != 0 || d["total"].(float64) != 1 || len(local) != 1 || d["duplicate"] != false || len(d["items"].([]any)) != 0 {
		t.Fatalf("添加以后：%v", d)
	}
	row := local[0].(map[string]any)
	sum, _ := row["sha1"].(string)
	if len(sum) != 40 || row["fileName"] != "我的模组 mine-1.0.jar" || row["size"].(float64) != float64(len(jar)) || row["title"] != "Mod mine" ||
		row["loader"] != "fabric" || row["addedAt"] == nil || row["warn"] != nil {
		t.Fatalf("清单里的本地文件：%v", row)
	}
	if added := d["added"].(map[string]any); added["sha1"] != sum || added["fileName"] != "我的模组 mine-1.0.jar" || added["title"] != "Mod mine" {
		t.Errorf("added：%v", added)
	}
	if body := e.call(t, "GET", "/api/modcart?core=fabric&mc=1.20.1", "", 200).Body.String(); strings.Contains(body, "upload") || strings.Contains(body, `\\`) ||
		!strings.Contains(body, sum) {
		t.Errorf("清单里不该有路径：%s", body)
	}
	noLeftover("添加成功以后")
	// 给了 core：加载器不对的在行上有提醒（Fabric 的 jar，按 Forge 服看）
	d = data(t, e.call(t, "GET", "/api/modcart?core=forge&mc=1.20.1", "", 200))
	if w, _ := d["local"].([]any)[0].(map[string]any)["warn"].(string); !strings.Contains(w, "给 Fabric 用的") {
		t.Errorf("加载器不对应有提醒：%v", d["local"])
	}

	// 不收的：不是 jar、不是 zip、没带文件、名字相同内容不同、太大的。都说人话，都不留临时文件
	for name, c := range map[string]struct {
		body []byte
		code int
		say  string
	}{
		"readme.txt":        {[]byte("hi"), 400, "只能添加 .jar 文件"},
		"broken.jar":        {[]byte("不是 zip"), 400, "打不开"},
		"我的模组 MINE-1.0.jar": {localJarBytes(t, "mine", nil), 409, "内容和这个不一样"},
		"":                  {nil, 400, "没有收到文件"},
	} {
		rec := e.uploadLocal(t, map[string]string{"core": "fabric", "mc": "1.20.1"}, name, c.body, c.code)
		if !strings.Contains(rec.Body.String(), c.say) || strings.Contains(rec.Body.String(), "cache") {
			t.Errorf("上传 %q 的报错应有「%s」、不带路径：%s", name, c.say, rec.Body)
		}
	}
	e.uploadLocal(t, map[string]string{"inst": "没有这台"}, "a.jar", jar, 404)
	noLeftover("被拒以后")
	old := modcartLocalLimit
	modcartLocalLimit = 1024
	rec := e.uploadLocal(t, nil, "huge.jar", bytes.Repeat([]byte("x"), 2<<20), http.StatusRequestEntityTooLarge)
	modcartLocalLimit = old
	if !strings.Contains(rec.Body.String(), "256 MB") || strings.Contains(rec.Body.String(), "too large") {
		t.Errorf("太大的要说清楚：%s", rec.Body)
	}
	noLeftover("太大被拒以后")
	// 同一个文件再传一次：不报错、不重复
	if d = data(t, e.uploadLocal(t, nil, "another.jar", jar, 200)); d["duplicate"] != true || d["total"].(float64) != 1 {
		t.Errorf("同一个文件再传：%v", d)
	}
	noLeftover("重复添加以后")

	// 检查前置：界面读 local[]（recognized / warn / covers / skip）和 manual[]（id / requiredBy / file）
	d = data(t, e.call(t, "GET", "/api/modcart/deps?core=fabric&mc=1.20.1", "", 200))
	lc := d["local"].([]any)[0].(map[string]any)
	man := d["manual"].([]any)
	if lc["sha1"] != sum || lc["fileName"] == nil || lc["recognized"] != false || len(man) != 1 {
		t.Fatalf("检查前置里的本地文件：%v 手动 %v", lc, man)
	}
	if m := man[0].(map[string]any); m["id"] != "ghostlib" || m["requiredBy"] != "Mod mine" || m["file"] != "我的模组 mine-1.0.jar" {
		t.Errorf("要自己去找的前置：%v", m)
	}
	for _, k := range []string{"files", "missing", "optional", "conflicts", "localHits", "unresolved"} {
		if _, ok := d[k].([]any); !ok {
			t.Errorf("检查前置的 %s 应是数组：%v", k, d[k])
		}
	}

	// 向导的提交链：草稿清单里只有本地文件。没带 expectLocal（或对不上）不装，带对了才装
	e.call(t, "POST", ip+"/modcart/install", `{"includeDraft":true,"expect":[]}`, 409)
	e.call(t, "POST", ip+"/modcart/install", `{"includeDraft":true,"expect":[],"expectLocal":[]}`, 409)
	if ents, _ := os.ReadDir(filepath.Join(dir, "mods")); len(ents) != 0 {
		t.Fatalf("被拒的安装不该落下文件：%v", ents)
	}
	id := data(t, e.call(t, "POST", ip+"/modcart/install", `{"includeDraft":true,"expect":[],"expectLocal":["`+sum+`"]}`, 200))["taskId"].(string)
	deadline := time.Now().Add(20 * time.Second)
	for {
		tk := data(t, e.call(t, "GET", "/api/tasks/"+id, "", 200))
		if tk["ended"] == true {
			if tk["error"] != nil || tk["kind"] != "mods" {
				t.Fatalf("任务：%v", tk)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务没结束")
		}
		time.Sleep(30 * time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "mods", "我的模组 mine-1.0.jar")); err != nil || !bytes.Equal(b, jar) {
		t.Fatalf("本地文件没装进 mods：%v", err)
	}
	if d = data(t, e.call(t, "GET", "/api/modcart", "", 200)); d["total"].(float64) != 0 || len(d["local"].([]any)) != 0 {
		t.Errorf("装好以后草稿清单应清掉：%v", d)
	}

	// 移除、清空（实例的清单）
	d = data(t, e.uploadLocal(t, map[string]string{"inst": "模组服"}, "second.jar", localJarBytes(t, "second", nil), 200))
	second := d["local"].([]any)[0].(map[string]any)["sha1"].(string)
	e.call(t, "POST", "/api/modcart/local/remove", `{"inst":"模组服","sha1":"zzz"}`, 400)
	e.call(t, "POST", "/api/modcart/local/remove", `{"inst":"模组服"}`, 400)
	if d = data(t, e.call(t, "POST", "/api/modcart/local/remove", `{"inst":"模组服","sha1":"`+second+`"}`, 200)); len(d["local"].([]any)) != 0 || d["total"].(float64) != 0 {
		t.Errorf("移除以后：%v", d)
	}
	e.uploadLocal(t, map[string]string{"inst": "模组服"}, "second.jar", localJarBytes(t, "second", nil), 200)
	if d = data(t, e.call(t, "POST", "/api/modcart/clear", `{"inst":"模组服"}`, 200)); len(d["local"].([]any)) != 0 || d["total"].(float64) != 0 {
		t.Errorf("人清空以后：%v", d)
	}
	if n := 0; filepath.WalkDir(filepath.Join(app.Base, "data", "modcart", "local"), func(_ string, de os.DirEntry, err error) error {
		if err == nil && !de.IsDir() {
			n++
		}
		return nil
	}) != nil || n != 0 {
		t.Errorf("清空以后暂存目录里不该有文件：%d 个", n)
	}
}
