package web

// AI 伙伴的接口（第七阶段）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/companion"
)

// 六个接口都通、都走操作层：vanilla 实例不支持伙伴，装模组、改设置报原因；没连着伙伴模组时刷出、移除、停下报 409；
// 局域网也能看、能管（不是仅本机）；响应里没有伙伴模组的令牌；整个过程不联网。
func TestCompanionRoutes(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服（vanilla）
	const tok = "companion-secret-token-77aa1234"
	cfg := companion.ConfigPath(filepath.Join(app.ServersDir, "s1"))
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("enabled=true\nport=24480\ntoken="+tok+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path, body string
		code               int
		errHas             string
		opts               []reqOpt
	}{
		{"GET", ip + "/companion", "", 200, "", nil},
		{"GET", ip + "/companion", "", 200, "", []reqOpt{fromLAN}},
		{"GET", "/api/instances/nope/companion", "", 404, "实例不存在", nil},
		{"POST", ip + "/companion/install", "", 400, "只支持", nil},
		{"PUT", ip + "/companion/settings", `{"maxCompanions":2}`, 400, "只支持", []reqOpt{fromLAN}},
		{"PUT", ip + "/companion/settings", `{"maxCompanions":99}`, 400, "maxCompanions", nil},
		{"POST", ip + "/companion/spawn", `{"name":"小方","owner":"Steve"}`, 409, "没连着伙伴模组", nil},
		{"POST", ip + "/companion/spawn", `{"name":"小 方"}`, 400, "不能用的字符", nil},
		{"POST", ip + "/companion/spawn", `{}`, 400, "name", nil},
		{"POST", ip + "/companion/despawn", `{"all":true}`, 409, "没连着伙伴模组", nil},
		{"POST", ip + "/companion/stop", `{"companion":1}`, 409, "没连着伙伴模组", []reqOpt{fromLAN}},
	}
	for _, c := range cases {
		rec := e.call(t, c.method, c.path, c.body, c.code, c.opts...)
		var resp struct {
			OK    bool            `json:"ok"`
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Errorf("%s %s：响应不是 JSON：%s", c.method, c.path, rec.Body)
			continue
		}
		if resp.OK != (c.code == 200) || (c.code == 200 && len(resp.Data) == 0) {
			t.Errorf("%s %s：响应结构不对：%s", c.method, c.path, rec.Body)
		}
		if c.errHas != "" && !strings.Contains(resp.Error, c.errHas) {
			t.Errorf("%s %s：错误「%s」里应有「%s」", c.method, c.path, resp.Error, c.errHas)
		}
		if strings.Contains(rec.Body.String(), tok) {
			t.Errorf("%s %s：响应里有伙伴模组的令牌", c.method, c.path)
		}
	}
	var st struct {
		Data struct {
			Local   bool `json:"local"`
			Install struct {
				Supported bool   `json:"supported"`
				Reason    string `json:"reason"`
			} `json:"install"`
			Settings struct {
				Enabled bool `json:"enabled"`
				Port    int  `json:"port"`
			} `json:"settings"`
			Link struct {
				State string `json:"state"`
			} `json:"link"`
			AI struct {
				QuotaLimit int64 `json:"quotaLimit"`
			} `json:"ai"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", ip+"/companion", "", 200).Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if d := st.Data; !d.Local || d.Install.Supported || !strings.Contains(d.Install.Reason, "NeoForge 1.21.1") || !d.Settings.Enabled || d.Settings.Port != 24480 ||
		d.Link.State != "waiting" {
		t.Errorf("状态：%+v", d)
	}
	// 「设置 → AI」里 AI 伙伴的每日额度
	e.call(t, "PUT", "/api/ai/quota", `{"companion":120000}`, 200)
	if err := json.Unmarshal(e.call(t, "GET", ip+"/companion", "", 200).Body.Bytes(), &st); err != nil || st.Data.AI.QuotaLimit != 120000 {
		t.Errorf("伙伴页的额度：%+v %v", st.Data.AI, err)
	}
	if d := e.dials.list(); len(d) != 0 {
		t.Errorf("不该有任何对外连接：%v", d)
	}
}
