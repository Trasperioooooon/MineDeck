package web

// 音乐共享的接口（第五阶段）。

import (
	"encoding/json"
	"strings"
	"testing"
)

// 八个接口都通、都走操作层：vanilla 实例不支持，开始广播、装桥接模组报原因；开始广播、列音源、
// 确认整机录音只能在本机；局域网可以看状态、停止广播；开始广播的尝试也写进操作记录。
func TestMusicRoutes(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服（vanilla）
	cases := []struct {
		method, path, body string
		code               int
		errHas             string
		opts               []reqOpt
	}{
		{"GET", ip + "/music", "", 200, "", nil},
		{"GET", ip + "/music", "", 200, "", []reqOpt{fromLAN}},
		{"GET", "/api/instances/nope/music", "", 404, "实例不存在", nil},
		{"POST", ip + "/music/start", `{"mode":"process","pid":4242,"exe":"cloudmusic.exe"}`, 400, "只支持 NeoForge、Forge 服", nil},
		{"POST", ip + "/music/start", `{"mode":"process","pid":4242}`, 403, "只能在这台电脑上开始", []reqOpt{fromLAN}},
		{"POST", ip + "/music/start", `{"mode":"radio"}`, 400, "mode", nil},
		{"POST", ip + "/music/stop", "", 400, "没在给「生存服」广播", []reqOpt{fromLAN}},
		{"PUT", ip + "/music/settings", `{"volume":80,"exclude":"房主"}`, 200, "", nil},
		{"PUT", ip + "/music/settings", `{"exclude":"a=b"}`, 400, "", nil},
		{"POST", ip + "/music/install", `{}`, 400, "只支持 NeoForge、Forge 服", nil},
		{"PUT", ip + "/music/voicehost", `{"value":"frp-abc.example.com:34567"}`, 400, "先开一次服", nil},
		{"GET", "/api/music/sources", "", 403, "只能在这台电脑上开始", []reqOpt{fromLAN}},
		{"GET", "/api/music/sources", "", 200, "", nil},
		{"POST", "/api/music/confirm-system", "", 403, "要在这台电脑上确认", []reqOpt{fromLAN}},
		{"POST", "/api/music/confirm-system", "", 200, "", nil},
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
	}

	type status struct {
		Data struct {
			Local           bool   `json:"local"`
			Supported       bool   `json:"supported"`
			Reason          string `json:"reason"`
			SystemConfirmed bool   `json:"systemConfirmed"`
			Settings        struct {
				Exclude string `json:"exclude"`
				Volume  int    `json:"volume"`
			} `json:"settings"`
			Guide string `json:"guide"`
		} `json:"data"`
	}
	var st, lan status
	if err := json.Unmarshal(e.call(t, "GET", ip+"/music", "", 200).Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if d := st.Data; !d.Local || d.Supported || d.Reason == "" || !d.SystemConfirmed || d.Settings.Volume != 80 || d.Settings.Exclude != "房主" || d.Guide == "" {
		t.Errorf("状态：%+v", d)
	}
	if err := json.Unmarshal(e.call(t, "GET", ip+"/music", "", 200, fromLAN).Body.Bytes(), &lan); err != nil || lan.Data.Local {
		t.Errorf("局域网看到的状态应标明不在本机：%+v %v", lan.Data, err)
	}

	var log struct {
		Data []struct {
			Op  string `json:"op"`
			Src string `json:"src"`
			OK  bool   `json:"ok"`
		} `json:"data"`
	}
	if err := json.Unmarshal(e.call(t, "GET", "/api/ops/log", "", 200).Body.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range log.Data {
		seen[r.Op] = true
	}
	for _, op := range []string{"music.start", "music.stop", "music.settings.set", "music.system.confirm", "music.bridge.install"} {
		if !seen[op] {
			t.Errorf("操作记录里没有 %s", op)
		}
	}
	if seen["music.sources"] || seen["music.status"] {
		t.Error("看状态、列音源不该写操作记录")
	}
}
