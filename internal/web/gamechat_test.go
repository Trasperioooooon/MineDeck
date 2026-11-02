package web

// 游戏内 AI 的接口（第四阶段）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"minedeck/internal/app"
)

// 五个接口都通、都走操作层：改设置、一键开白名单只能在本机；局域网能看设置和最近的问答、能做识别测试；
// 响应里搜不到 Key。
func TestGameChatRoutes(t *testing.T) {
	e := newFullEnv(t)
	ip := "/api/instances/%E7%94%9F%E5%AD%98%E6%9C%8D" // 生存服
	line := `[20:15:31] [Server thread/INFO]: [Not Secure] <Steve> @AI /私 今晚怎么打末影龙`
	test, _ := json.Marshal(map[string]any{"line": line, "aliases": []string{"@小助手"}})
	cases := []struct {
		method, path, body string
		code               int
		errHas             string
		opts               []reqOpt
	}{
		{"GET", ip + "/gameai", "", 200, "", nil},
		{"GET", ip + "/gameai", "", 200, "", []reqOpt{fromLAN}},
		{"GET", "/api/instances/nope/gameai", "", 404, "实例不存在", nil},
		{"PUT", ip + "/gameai", `{"enabled":true}`, 403, "游戏内 AI 的设置仅支持在本机修改", []reqOpt{fromLAN}},
		{"PUT", ip + "/gameai", `{"prefix":"/ai"}`, 400, "前缀", nil},
		{"PUT", ip + "/gameai", `{"templates":{"op":true}}`, 400, "没有这个命令模板", nil},
		{"PUT", ip + "/gameai", `{"cooldown":99999}`, 400, "cooldown", nil},
		{"PUT", ip + "/gameai", `{"enabled":true,"allowed":["Steve"],"budget":100000}`, 200, "", nil},
		{"POST", ip + "/gameai/test", string(test), 200, "", []reqOpt{fromLAN}},
		{"POST", ip + "/gameai/test", `{}`, 400, "line", nil},
		{"GET", ip + "/gameai/log?limit=5", "", 200, "", []reqOpt{fromLAN}},
		{"GET", ip + "/gameai/log?limit=x", "", 400, "limit", nil},
		{"POST", ip + "/gameai/whitelist", `{"players":["Steve"]}`, 403, "仅支持在本机修改", []reqOpt{fromLAN}},
		{"POST", ip + "/gameai/whitelist", `{"players":["Steve","Alex"]}`, 200, "", nil},
	}
	var bodies []string
	for _, c := range cases {
		rec := e.call(t, c.method, c.path, c.body, c.code, c.opts...)
		bodies = append(bodies, rec.Body.String())
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
	for _, b := range bodies {
		for _, s := range e.secrets {
			if strings.Contains(b, s) {
				t.Errorf("响应里有密钥 %q：%s", s[:6], b)
			}
		}
	}

	var v struct {
		Data struct {
			Local       bool `json:"local"`
			WhitelistOn bool `json:"whitelistOn"`
			Settings    struct {
				Enabled bool     `json:"enabled"`
				Prefix  string   `json:"prefix"`
				Allowed []string `json:"allowed"`
				Budget  int64    `json:"budget"`
			} `json:"settings"`
			Templates []struct {
				ID      string `json:"id"`
				Default bool   `json:"default"`
			} `json:"templates"`
			Players []struct {
				Name      string `json:"name"`
				Whitelist bool   `json:"whitelist"`
				Allowed   bool   `json:"allowed"`
			} `json:"players"`
		} `json:"data"`
	}
	_ = json.Unmarshal(e.call(t, "GET", ip+"/gameai", "", 200, fromLAN).Body.Bytes(), &v)
	if v.Data.Local || !v.Data.Settings.Enabled || v.Data.Settings.Prefix != "@AI" || len(v.Data.Settings.Allowed) != 1 || v.Data.Settings.Budget != 100000 ||
		!v.Data.WhitelistOn || len(v.Data.Templates) != 10 {
		t.Errorf("设置：%+v", v.Data)
	}
	if len(v.Data.Players) != 2 || !v.Data.Players[1].Whitelist || !v.Data.Players[1].Allowed || v.Data.Players[1].Name != "Steve" {
		t.Errorf("可以授权的玩家：%+v", v.Data.Players)
	}
	var tr struct {
		Data struct {
			Player string `json:"player"`
			Ask    struct {
				Private  bool   `json:"private"`
				Question string `json:"question"`
			} `json:"ask"`
			Result string `json:"result"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(bodies[8]), &tr)
	if tr.Data.Player != "Steve" || !tr.Data.Ask.Private || tr.Data.Ask.Question != "今晚怎么打末影龙" || !strings.Contains(tr.Data.Result, "只发给 Steve") {
		t.Errorf("识别测试：%s", bodies[8])
	}
	// 设置存在实例目录，白名单写进了名单文件和 server.properties（服务器停着）
	dir := filepath.Join(app.ServersDir, "s1")
	if b, _ := os.ReadFile(filepath.Join(dir, "gameai.json")); !strings.Contains(string(b), `"Steve"`) {
		t.Errorf("gameai.json：%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "server.properties")); !strings.Contains(string(b), "white-list=true") {
		t.Errorf("server.properties：%s", b)
	}
}
