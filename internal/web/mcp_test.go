package web

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/ops"
)

// ---------- MCP 接口（设计文档 §15） ----------

// mcpPost 发一个 POST /mcp（本机，带 MCP 令牌；不带网页令牌）。kv：请求头 名, 值……
func (e *fullEnv) mcpPost(t *testing.T, tok, body string, want int, kv ...string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Host = "127.0.0.1:27333"
	req.RemoteAddr = "127.0.0.1:52000"
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i] == "RemoteAddr" {
			req.RemoteAddr = kv[i+1]
			continue
		}
		req.Header.Set(kv[i], kv[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("POST /mcp %s：HTTP %d，应为 %d：%s", body, rec.Code, want, rec.Body)
	}
	var m map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("不是 JSON：%s", rec.Body)
		}
	}
	if sid := rec.Header().Get("Mcp-Session-Id"); sid != "" {
		m["_session"] = sid
	}
	return m
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func rpc(id any, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

func toolText(t *testing.T, m map[string]any) (string, bool) {
	t.Helper()
	res, _ := m["result"].(map[string]any)
	c, _ := res["content"].([]any)
	if len(c) != 1 {
		t.Fatalf("工具结果：%v", m)
	}
	s, _ := c[0].(map[string]any)["text"].(string)
	isErr, _ := res["isError"].(bool)
	return s, isErr
}

// 设置接口：局域网能看不能改，拿不到带令牌的配置；令牌 DPAPI 加密存进 secrets.json；/mcp 只认 MCP 令牌（不认网页令牌）；
// 外部 AI 的写操作只生成提案，局域网看不到、确认不了，本机确认后按提案执行，操作记录的来源是 mcp。
func TestMCPRoutes(t *testing.T) {
	e := newFullEnv(t)
	var st struct {
		Data struct {
			Enabled  bool   `json:"enabled"`
			Local    bool   `json:"local"`
			URL      string `json:"url"`
			TokenSet bool   `json:"tokenSet"`
			Pending  int    `json:"pending"`
		} `json:"data"`
	}
	_ = json.Unmarshal(e.call(t, "GET", "/api/mcp", "", 200).Body.Bytes(), &st)
	if st.Data.Enabled || !st.Data.Local || st.Data.TokenSet || st.Data.URL != "http://127.0.0.1:27333/mcp" {
		t.Fatalf("默认：%+v", st.Data)
	}
	init := rpc(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "MCP 测试", "version": "1"}})
	if m := e.mcpPost(t, "x", init, 403); !strings.Contains(js(m), "MCP 接口没有打开") {
		t.Fatalf("关着：%v", m)
	}
	_ = json.Unmarshal(e.call(t, "GET", "/api/mcp", "", 200, fromLAN).Body.Bytes(), &st)
	if st.Data.Local {
		t.Error("局域网打开时 local 为 false")
	}
	e.call(t, "PUT", "/api/mcp", `{"enabled":true}`, 403, fromLAN)
	e.call(t, "POST", "/api/mcp/token", "", 403, fromLAN)
	e.call(t, "GET", "/api/mcp/config", "", 403, fromLAN)
	e.call(t, "GET", "/api/mcp/events", "", 403, fromLAN)
	e.call(t, "PUT", "/api/mcp", `{"enabled":true}`, 200, fromUI)
	var cfg struct {
		Data struct {
			Token    string `json:"token"`
			Snippets []struct {
				ID, Text string
			} `json:"snippets"`
		} `json:"data"`
	}
	_ = json.Unmarshal(e.call(t, "GET", "/api/mcp/config", "", 200, fromUI).Body.Bytes(), &cfg)
	tok := cfg.Data.Token
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(tok) || len(cfg.Data.Snippets) < 5 {
		t.Fatalf("配置：%+v", cfg.Data)
	}
	if b, _ := os.ReadFile(filepath.Join(app.Base, "secrets.json")); !strings.Contains(string(b), `"mcp.token": "dpapi:`) || strings.Contains(string(b), tok) {
		t.Fatalf("令牌要 DPAPI 加密：%s", b)
	}
	if rt, err := os.ReadFile(filepath.Join(app.Base, "data", "mcp", "runtime.json")); err != nil || !strings.Contains(string(rt), `"port": 27333`) || strings.Contains(string(rt), tok) {
		t.Fatalf("运行时文件：%v %s", err, rt)
	}
	for _, path := range []string{"/api/mcp", "/api/app", "/api/ops/log"} {
		if body := e.call(t, "GET", path, "", 200).Body.String(); strings.Contains(body, tok) {
			t.Errorf("%s 里不能有 MCP 令牌", path)
		}
	}
	// 网页令牌、局域网都不行
	e.mcpPost(t, Token, init, 401)
	e.mcpPost(t, "", init, 401, "X-Token", Token)
	e.mcpPost(t, tok, init, 403, "RemoteAddr", "192.168.1.23:52000")
	e.mcpPost(t, tok, init, 403, "Origin", "http://evil.example")
	m := e.mcpPost(t, tok, init, 200)
	sid, _ := m["_session"].(string)
	if sid == "" || !strings.Contains(js(m), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize：%v", m)
	}
	hdr := []string{"Mcp-Session-Id", sid, "MCP-Protocol-Version", "2025-06-18"}
	list := js(e.mcpPost(t, tok, rpc(2, "tools/list", nil), 200, hdr...))
	for _, want := range []string{`"name":"instance_props_set"`, `"name":"instance_restart"`, `"name":"proposal_status"`, `"name":"instance_list"`} {
		if !strings.Contains(list, want) {
			t.Errorf("工具清单里应有 %s", want)
		}
	}
	for _, bad := range []string{"instance_delete", "mcp_get", "mcp_set", "mcp_config", "ai_provider_set", "ai_settings_get", "app_config_set", "host_openpath", "game_info"} {
		if strings.Contains(list, `"name":"`+bad+`"`) {
			t.Errorf("仅人工的操作不能给外部 AI：%s", bad)
		}
	}
	// 读操作直接执行，脱敏
	text, isErr := toolText(t, e.mcpPost(t, tok, rpc(3, "tools/call", map[string]any{"name": "instance_list", "arguments": map[string]any{}}), 200, hdr...))
	if isErr || !strings.Contains(text, "生存服") || strings.Contains(text, app.Base) || !strings.Contains(text, "<程序目录>") {
		t.Fatalf("读操作：%s", text)
	}
	// 写操作：只生成提案
	text, _ = toolText(t, e.mcpPost(t, tok, rpc(4, "tools/call", map[string]any{"name": "instance_props_set",
		"arguments": map[string]any{"inst": "生存服", "pairs": []map[string]string{{"key": "max-players", "value": "10"}}}}), 200, hdr...))
	i := strings.Index(text, "p_")
	if i < 0 || !strings.Contains(text, "还没有执行") {
		t.Fatalf("写操作：%s", text)
	}
	pid := text[i : i+14]
	props := func() string {
		b, _ := os.ReadFile(filepath.Join(app.ServersDir, "s1", "server.properties"))
		return string(b)
	}
	if strings.Contains(props(), "max-players=10") {
		t.Fatal("没确认就改了")
	}
	if p := e.proposals(t, "", fromLAN); len(p.Data) != 0 {
		t.Fatalf("局域网看不到外部 AI 的提案：%+v", p.Data)
	}
	if p := e.proposals(t, ""); len(p.Data) != 1 || p.Data[0].ID != pid || p.Data[0].Caller.Source != ops.SrcMCP || p.Data[0].Caller.Actor != "MCP 测试" {
		t.Fatalf("本机看得到：%+v", p.Data)
	}
	e.call(t, "POST", "/api/ops/proposals/"+pid+"/confirm", `{}`, 403, fromLAN)
	e.call(t, "POST", "/api/ops/proposals/"+pid+"/reject", `{}`, 403, fromLAN)
	// 界面的事件流（本机）：先推一份等确认的提案
	if body := e.call(t, "GET", "/api/mcp/events?token="+Token, "", 200, canceled).Body.String(); !strings.Contains(body, `"type":"pending"`) || !strings.Contains(body, pid) {
		t.Fatalf("事件流：%s", body)
	}
	e.call(t, "POST", "/api/ops/proposals/"+pid+"/confirm", `{}`, 200, fromUI)
	if !strings.Contains(props(), "max-players=10") {
		t.Fatalf("确认后应改好：%s", props())
	}
	var lg struct {
		Data []ops.Record `json:"data"`
	}
	_ = json.Unmarshal(e.call(t, "GET", "/api/ops/log?src=mcp", "", 200).Body.Bytes(), &lg)
	if len(lg.Data) != 2 || lg.Data[0].Op != "instance.props.set" || lg.Data[0].Actor != "MCP 测试" || lg.Data[0].ApprovedBy != "本机" || lg.Data[0].Proposal != pid ||
		lg.Data[1].Event != ops.EvPropose {
		t.Fatalf("操作记录：%+v", lg.Data)
	}
	text, _ = toolText(t, e.mcpPost(t, tok, rpc(5, "tools/call", map[string]any{"name": "proposal_status", "arguments": map[string]any{"id": pid}}), 200, hdr...))
	if !strings.Contains(text, "已经执行") {
		t.Fatalf("proposal_status：%s", text)
	}
	// 重置令牌：旧的失效
	e.call(t, "POST", "/api/mcp/token", "", 200, fromUI)
	e.mcpPost(t, tok, rpc(6, "ping", nil), 401)
	// 关掉：端点拒绝，运行时文件删掉；操作记录里有开关和重置
	e.call(t, "PUT", "/api/mcp", `{"enabled":false}`, 200, fromUI)
	e.mcpPost(t, tok, rpc(7, "ping", nil), 403)
	if _, err := os.Stat(filepath.Join(app.Base, "data", "mcp", "runtime.json")); err == nil {
		t.Error("关掉后应删掉运行时文件")
	}
	_ = json.Unmarshal(e.call(t, "GET", "/api/ops/log?op=mcp.", "", 200).Body.Bytes(), &lg)
	var opsSeen []string
	for _, r := range lg.Data {
		opsSeen = append(opsSeen, r.Op+"/"+string(r.Src))
	}
	if strings.Join(opsSeen, ",") != "mcp.set/ui,mcp.token.reset/ui,mcp.config/ui,mcp.set/ui" {
		t.Errorf("MCP 设置的操作记录：%v", opsSeen)
	}
}
