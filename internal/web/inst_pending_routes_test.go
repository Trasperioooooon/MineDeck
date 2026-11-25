package web

// beta.3 第二层：按服务器查、处置整合包导入时没装上的文件（R09）的接口。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// GET /api/instances/{name}/import-pending：别的设备也能看；没有清单是两个空列表；服务器不在 404。
// POST 同一路径：只许本机；空动作 400；处置以后返回结果和新的清单（跳过的留在 skipped 里）。
func TestInstPendingRoutes(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服
	type view struct {
		Data struct {
			Inst     string            `json:"inst"`
			Pending  []json.RawMessage `json:"pending"`
			Skipped  []json.RawMessage `json:"skipped"`
			Resolved []struct {
				ID   string `json:"id"`
				OK   bool   `json:"ok"`
				Note string `json:"note"`
			} `json:"resolved"`
		} `json:"data"`
		Error string `json:"error"`
	}
	get := func(opts ...reqOpt) view {
		var v view
		if err := json.Unmarshal(e.call(t, "GET", ip+"/import-pending", "", 200, opts...).Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := get(); v.Data.Inst != "生存服" || v.Data.Pending == nil || len(v.Data.Pending) != 0 || v.Data.Skipped == nil {
		t.Fatalf("没有清单时应是两个空列表（不是 null）：%+v", v)
	}
	e.call(t, "GET", "/api/instances/nope/import-pending", "", 404)
	e.call(t, "POST", ip+"/import-pending", `{"items":[]}`, 400)
	if rec := e.call(t, "POST", ip+"/import-pending", `{"items":[{"id":"p1","act":"skip"}]}`, 403, fromLAN); !strings.Contains(rec.Body.String(), "仅支持在本机操作") {
		t.Fatalf("别的设备不能处置：%s", rec.Body)
	}
	e.call(t, "POST", ip+"/import-pending", `{"items":[{"id":"p1","act":"毁掉"}]}`, 400)

	dir := filepath.Join(app.ServersDir, "s1")
	list := `{"version":1,"pack":"测试包","items":[{"id":"p1","path":"mods/a.jar","reason":"nohash"},{"id":"p2","path":"mods/b.jar","reason":"host","urls":["https://93.184.216.34/b.jar","javascript:alert(1)"],"host":"cdn.modrinth.com"}]}`
	if err := os.MkdirAll(filepath.Join(dir, ".minedeck"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".minedeck", "import-pending.json"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	v := get(fromLAN)
	if len(v.Data.Pending) != 2 {
		t.Fatalf("清单读回来：%+v", v)
	}
	if s := string(v.Data.Pending[1]); strings.Contains(s, "javascript") || strings.Contains(s, "modrinth") {
		t.Fatalf("坏地址和冒充的站点名不该出现在接口里：%s", s)
	}
	var r view
	if err := json.Unmarshal(e.call(t, "POST", ip+"/import-pending", `{"items":[{"id":"p1","act":"skip"}]}`, 200).Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Data.Resolved) != 1 || !r.Data.Resolved[0].OK || len(r.Data.Pending) != 1 || len(r.Data.Skipped) != 1 {
		t.Fatalf("跳过以后：%+v", r)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "导入待决清单.txt")); err != nil || !strings.Contains(string(b), "【已跳过】mods/a.jar") {
		t.Fatalf("给人看的那份跟着更新：%v %s", err, b)
	}

	// 真 HTTP 路由上校验单批上限（readJSON + 操作层 pendingResolveMax）：501 项在两个待决处置接口上都以 400 拒绝，500 项不被上限拦住
	batchBody := func(n int) string {
		type act struct {
			ID  string `json:"id"`
			Act string `json:"act"`
		}
		items := make([]act, n)
		for i := range items {
			items[i] = act{ID: "missing", Act: "skip"}
		}
		b, err := json.Marshal(struct {
			Items []act `json:"items"`
		}{Items: items})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, route := range []string{ip + "/import-pending", "/api/import/pending/task-no-such"} {
		rec501 := e.call(t, "POST", route, batchBody(501), 400)
		if !strings.Contains(rec501.Body.String(), "最多 500 项") {
			t.Fatalf("%s 提交 501 项应报「最多 500 项」：%s", route, rec501.Body)
		}
	}
	rec500 := e.call(t, "POST", ip+"/import-pending", batchBody(500), 200)
	if strings.Contains(rec500.Body.String(), "最多 500 项") {
		t.Fatalf("提交 500 项不应被单批上限拒绝：%s", rec500.Body)
	}
}
