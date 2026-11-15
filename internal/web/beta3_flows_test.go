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
}
