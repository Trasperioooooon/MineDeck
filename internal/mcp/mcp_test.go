package mcp

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 默认关闭：端点和 initialize 都回「没有打开」的中文说明，没有令牌、没有运行时文件；打开时才生成令牌、写运行时文件。
func TestOffByDefaultAndToggle(t *testing.T) {
	f := newFixture(t)
	if f.svc.Enabled() || f.svc.Status(true).TokenSet {
		t.Fatal("默认应关闭，也没有令牌")
	}
	r := f.post(legacyReq(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	if r.status != http.StatusForbidden || r.code() != CodeDisabled || !strings.Contains(r.raw, "MCP 接口没有打开") || !strings.Contains(r.raw, "设置 → AI（可选）→ MCP 接口") {
		t.Fatalf("关闭时：%d %s", r.status, r.raw)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "mcp", RuntimeFile)); err == nil {
		t.Fatal("关闭时不该有运行时文件")
	}
	tok := f.on()
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(tok) {
		t.Fatalf("令牌：%q", tok)
	}
	var rt Runtime
	b, err := os.ReadFile(filepath.Join(f.dir, "mcp", RuntimeFile))
	if err != nil || json.Unmarshal(b, &rt) != nil || rt.V != 1 || rt.PID != os.Getpid() || rt.Port == 0 || !strings.Contains(f.srv.URL, ":"+itoa(rt.Port)) {
		t.Fatalf("运行时文件：%v %s", err, b)
	}
	if strings.Contains(string(b), tok) {
		t.Fatal("运行时文件里不能有令牌")
	}
	st := f.svc.Status(true)
	if !st.Enabled || !st.TokenSet || st.URL != "http://127.0.0.1:"+itoa(rt.Port)+"/mcp" || !st.PortChanged || st.Tools == 0 {
		t.Fatalf("打开后的状态：%+v", st)
	}
	if b, _ := json.Marshal(st); strings.Contains(string(b), tok) {
		t.Fatal("状态里不能有令牌")
	}
	if err := f.svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "mcp", RuntimeFile)); err == nil {
		t.Fatal("关闭后应删掉运行时文件")
	}
	if f.on() != tok {
		t.Fatal("再打开时令牌不变（客户端的配置不用改）")
	}
	// 设置存盘：重启后照样是打开的，令牌也还在
	again := New(Options{Ops: f.reg, Dir: filepath.Join(f.dir, "mcp"), Keys: f.keys})
	if !again.Enabled() || !again.Status(true).TokenSet {
		t.Fatal("重启后应保持打开")
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// 认证：没带令牌、令牌不对都是 401；令牌解不开时提示重置；重置后旧令牌失效。
func TestAuth(t *testing.T) {
	f := newFixture(t)
	tok := f.on()
	init := legacyReq(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if r := f.post(init, "Authorization", ""); r.status != 401 || r.code() != CodeAuth || r.header.Get("WWW-Authenticate") == "" || !strings.Contains(r.raw, "令牌不对") {
		t.Fatalf("没带令牌：%d %s", r.status, r.raw)
	}
	if r := f.post(init, "Authorization", "Bearer "+strings.Repeat("0", 64)); r.status != 401 {
		t.Fatalf("令牌不对：%d", r.status)
	}
	if r := f.post(init, "Authorization", "Basic "+tok); r.status != 401 {
		t.Fatalf("不是 Bearer：%d", r.status)
	}
	if r := f.post(init, "Authorization", "bearer  "+tok+" "); r.status != 200 {
		t.Fatalf("Bearer 不分大小写、前后空白不算：%d %s", r.status, r.raw)
	}
	if err := f.svc.ResetToken(); err != nil {
		t.Fatal(err)
	}
	if r := f.post(init, "Authorization", "Bearer "+tok); r.status != 401 {
		t.Fatal("重置后旧令牌应失效")
	}
	if r := f.post(init); r.status != 200 {
		t.Fatalf("新令牌：%d", r.status)
	}
	// 换了电脑：令牌解不开
	f.keys.mu.Lock()
	f.keys.broken = true
	f.keys.mu.Unlock()
	f.svc.mu.Lock()
	f.svc.loadTokenLocked()
	f.svc.mu.Unlock()
	if r := f.post(init, "Authorization", "Bearer x"); r.status != 401 || !strings.Contains(r.raw, "解不开") {
		t.Fatalf("令牌解不开：%d %s", r.status, r.raw)
	}
	if !f.svc.Status(true).TokenBroken {
		t.Fatal("状态里应写明令牌解不开")
	}
	if err := f.svc.ResetToken(); err != nil || f.post(init).status != 200 {
		t.Fatal("重置后应能用")
	}
}

// 只收本机的 POST；Host、Origin 不是本机的一律 403（防 DNS 重绑定）。
func TestTransportGuards(t *testing.T) {
	f := newFixture(t)
	f.on()
	init := legacyReq(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	for _, o := range []string{"http://evil.example", "https://127.0.0.1.evil.example", "null", "file://", "http://192.168.1.23:27333"} {
		if r := f.post(init, "Origin", o); r.status != 403 || r.code() != CodeForbidden {
			t.Errorf("Origin %s：%d %s", o, r.status, r.raw)
		}
	}
	for _, o := range []string{"http://localhost:6274", "http://127.0.0.1:27333", "https://[::1]"} {
		if r := f.post(init, "Origin", o); r.status != 200 {
			t.Errorf("本机的 Origin %s 应放行：%d %s", o, r.status, r.raw)
		}
	}
	if r := f.post(init, "Host", "evil.example:27333"); r.status != 403 || !strings.Contains(r.raw, "Host") {
		t.Errorf("Host 不对：%d %s", r.status, r.raw)
	}
	if r := f.post(init, "Host", "localhost:27333"); r.status != 200 {
		t.Errorf("localhost 应放行：%d", r.status)
	}
	// 局域网的设备（MineDeck 开着局域网访问时监听 0.0.0.0）
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(init))
	req.Host = "127.0.0.1:27333"
	req.RemoteAddr = "192.168.1.50:52000"
	req.Header.Set("Authorization", "Bearer "+f.keys.token())
	w := httptest.NewRecorder()
	f.svc.ServeHTTP(w, req)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "只接受这台电脑上的连接") {
		t.Errorf("局域网：%d %s", w.Code, w.Body.String())
	}
	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		req := httptest.NewRequest(m, "/mcp", nil)
		req.Host, req.RemoteAddr = "127.0.0.1:27333", "127.0.0.1:52000"
		w := httptest.NewRecorder()
		f.svc.ServeHTTP(w, req)
		if w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Errorf("%s 应回 405：%d", m, w.Code)
		}
	}
}

// JSON-RPC 本身的错误：解析失败、不是请求、id 为 null、params 不是对象、未知方法；通知和客户端的响应回 202。
func TestJSONRPC(t *testing.T) {
	f := newFixture(t)
	f.on()
	cases := []struct {
		body   string
		status int
		code   int
		id     bool
	}{
		{`{"jsonrpc":"2.0","id":1,`, 400, codeParse, false},
		{`[]`, 400, codeInvalidRequest, false},
		{`"hello"`, 400, codeInvalidRequest, false},
		{`{"jsonrpc":"1.0","id":5,"method":"ping"}`, 400, codeInvalidRequest, true},
		{`{"jsonrpc":"2.0","id":null,"method":"ping"}`, 400, codeInvalidRequest, false},
		{`{"jsonrpc":"2.0","id":{"x":1},"method":"ping"}`, 400, codeInvalidRequest, false},
		{`{"jsonrpc":"2.0","id":2,"method":"ping","params":[1]}`, 400, codeInvalidParams, true},
		{`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`, 200, codeMethodNotFound, true},
		{`{"jsonrpc":"2.0","id":"s-1","method":"tools/call","params":{"arguments":{}}}`, 200, codeInvalidParams, true},
		{`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"instance_delete"}}`, 200, codeInvalidParams, true},
	}
	for _, c := range cases {
		r := f.post(c.body, "MCP-Protocol-Version", "2025-06-18")
		if r.status != c.status || r.code() != c.code || (r.m["id"] != nil) != c.id {
			t.Errorf("%s：%d %s", c.body, r.status, r.raw)
		}
	}
	for _, body := range []string{`{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":9,"result":{}}`} {
		if r := f.post(body); r.status != 202 || r.raw != "" {
			t.Errorf("%s 应回 202、没有内容：%d %q", body, r.status, r.raw)
		}
	}
	if r := f.post(legacyReq("abc", "ping", nil)); r.status != 200 || string(mustJSON(r.m["id"])) != `"abc"` || r.result() == nil {
		t.Errorf("ping：%s", r.raw)
	}
	if r := f.post(legacyReq(12345678901234, "ping", nil)); !strings.Contains(r.raw, `"id":12345678901234`) {
		t.Errorf("大数字 id 要原样回：%s", r.raw)
	}
	big := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"`+strings.Repeat("a", maxBody)+`"}}`))
	big.Host, big.RemoteAddr = "127.0.0.1:27333", "127.0.0.1:52000"
	big.Header.Set("Authorization", "Bearer "+f.keys.token())
	w := httptest.NewRecorder()
	f.svc.ServeHTTP(w, big)
	if w.Code != 413 {
		t.Errorf("太大的请求体：%d", w.Code)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// 旧版的批量请求：逐条处理，回复按顺序；全是通知就回 202；2026-07-28 的请求不能放进批量。
func TestBatch(t *testing.T) {
	f := newFixture(t)
	f.on()
	r := f.post(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`)
	var arr []map[string]any
	if r.status != 200 || json.Unmarshal([]byte(r.raw), &arr) != nil || len(arr) != 2 || arr[0]["id"].(float64) != 1 || arr[1]["result"] == nil {
		t.Fatalf("批量：%d %s", r.status, r.raw)
	}
	if r := f.post(`[{"jsonrpc":"2.0","method":"notifications/initialized"}]`); r.status != 202 {
		t.Fatalf("只有通知：%d", r.status)
	}
	modern := legacyReq(3, "tools/list", map[string]any{"_meta": modernMeta("x")})
	r = f.post("[" + modern + "]")
	if json.Unmarshal([]byte(r.raw), &arr) != nil || len(arr) != 1 || !strings.Contains(r.raw, "不支持批量") {
		t.Fatalf("批量里的 2026-07-28 请求：%s", r.raw)
	}
}

// 版本协商：旧版 initialize 认识的版本原样回，不认识的回 2025-11-25；2026-07-28 用 server/discover，
// 不支持的版本回 UnsupportedProtocolVersionError（带支持的版本）。
func TestVersionNegotiation(t *testing.T) {
	f := newFixture(t)
	f.on()
	for want, got := range map[string]string{"2025-11-25": "2025-11-25", "2025-06-18": "2025-06-18", "2025-03-26": "2025-03-26",
		"2024-11-05": "2024-11-05", "2024-10-07": "2025-11-25", "2099-01-01": "2025-11-25", Modern: "2025-11-25"} {
		r, sid := f.initialize(want, "TestClient")
		res := r.result()
		if r.status != 200 || res["protocolVersion"] != got || sid == "" || !strings.Contains(js(res["capabilities"]), `"tools":{"listChanged":false}`) {
			t.Errorf("initialize %s：%s", want, r.raw)
		}
		info, _ := res["serverInfo"].(map[string]any)
		if info["name"] != "minedeck" || info["version"] != "9.9.9" || (info["title"] != nil) != (got >= "2025-06-18") {
			t.Errorf("initialize %s 的 serverInfo：%v", want, info)
		}
		if !strings.Contains(res["instructions"].(string), "proposal_status") {
			t.Error("instructions 应说明提案和 proposal_status")
		}
		if _, ok := res["resultType"]; ok {
			t.Error("旧版的结果不带 resultType")
		}
	}
	// 2026-07-28：server/discover
	r := f.modern(1, "server/discover", nil)
	res := r.result()
	if r.status != 200 || res["resultType"] != "complete" || js(res["supportedVersions"]) != js(Versions) || res["cacheScope"] != "private" || res["ttlMs"] == nil {
		t.Fatalf("server/discover：%s", r.raw)
	}
	if meta, _ := res["_meta"].(map[string]any); meta == nil || meta["io.modelcontextprotocol/serverInfo"] == nil {
		t.Fatalf("2026-07-28 的结果要在 _meta 里带 serverInfo：%s", r.raw)
	}
	// 不支持的版本
	meta := modernMeta("x")
	meta["io.modelcontextprotocol/protocolVersion"] = "2030-01-01"
	r = f.post(legacyReq(2, "tools/list", map[string]any{"_meta": meta}), "MCP-Protocol-Version", "2030-01-01", "Mcp-Method", "tools/list")
	data, _ := r.err()["data"].(map[string]any)
	if r.status != 400 || r.code() != codeUnsupported || js(data["supported"]) != js(Versions) || data["requested"] != "2030-01-01" {
		t.Fatalf("不支持的版本：%d %s", r.status, r.raw)
	}
	// 旧版的请求带了不支持的版本头
	if r := f.post(legacyReq(3, "tools/list", nil), "MCP-Protocol-Version", "1999-01-01"); r.status != 400 || r.code() != codeUnsupported {
		t.Fatalf("不支持的版本头：%d %s", r.status, r.raw)
	}
	// 版本头说是 2026-07-28，请求体的 _meta 里却没有版本：缺了必填的 _meta 字段（-32602）
	if r := f.post(legacyReq(4, "tools/list", nil), "MCP-Protocol-Version", Modern); r.status != 400 || r.code() != codeInvalidParams {
		t.Fatalf("缺 _meta 的版本：%d %s", r.status, r.raw)
	}
	if r := f.post(legacyReq(4, "tools/list", map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/clientCapabilities": map[string]any{}}}),
		"MCP-Protocol-Version", Modern, "Mcp-Method", "tools/list"); r.status != 400 || r.code() != codeInvalidParams {
		t.Fatalf("_meta 里没有版本：%d %s", r.status, r.raw)
	}
	// 头和请求体不一致，先报不一致，再看支不支持
	meta["io.modelcontextprotocol/protocolVersion"] = "2030-01-01"
	if r := f.post(legacyReq(4, "tools/list", map[string]any{"_meta": meta}), "MCP-Protocol-Version", Modern, "Mcp-Method", "tools/list"); r.status != 400 || r.code() != codeHeaderMismatch {
		t.Fatalf("头和请求体不一致：%d %s", r.status, r.raw)
	}
	// server/discover 不带 _meta：按旧版处理，没有这个方法
	if r := f.post(legacyReq(5, "server/discover", nil)); r.status != 200 || r.code() != codeMethodNotFound {
		t.Fatalf("server/discover 不带 _meta：%d %s", r.status, r.raw)
	}
	// 2026-07-28 去掉了的方法：initialize、ping 带着 _meta 来就是 404
	for _, m := range []string{"initialize", "ping"} {
		if r := f.modern(6, m, nil); r.status != 404 || r.code() != codeMethodNotFound {
			t.Fatalf("2026-07-28 的 %s：%d %s", m, r.status, r.raw)
		}
	}
	// 版本头是乱写的（不是日期）：当旧版请求，报不支持的版本
	if r := f.post(legacyReq(7, "tools/list", nil), "MCP-Protocol-Version", "latest"); r.status != 400 || r.code() != codeUnsupported {
		t.Fatalf("乱写的版本头：%d %s", r.status, r.raw)
	}
	// 没带版本头、也认不出会话的旧版请求，按 2025-03-26 处理（工具没有 title）
	r = f.post(legacyReq(6, "tools/list", nil))
	if r.status != 200 || strings.Contains(r.raw, `"title"`) || !strings.Contains(r.raw, `"annotations"`) {
		t.Fatalf("默认版本：%s", r.raw[:min(300, len(r.raw))])
	}
}

// 2026-07-28 的 Streamable HTTP：镜像头必须和请求体一致，tools/call 还要 Mcp-Name（不是 ASCII 的用 Base64 写法）；
// 每个请求都要带 clientCapabilities；未知方法回 404。
func TestModernHeaders(t *testing.T) {
	f := newFixture(t)
	f.on()
	p := func() map[string]any { return map[string]any{"_meta": modernMeta("Cursor")} }
	body := legacyReq(1, "tools/list", p())
	for _, c := range []struct {
		kv   []string
		want string
	}{
		{[]string{"Mcp-Method", "tools/list"}, "缺少 MCP-Protocol-Version"},
		{[]string{"MCP-Protocol-Version", "2025-06-18", "Mcp-Method", "tools/list"}, "MCP-Protocol-Version 请求头是"},
		{[]string{"MCP-Protocol-Version", Modern}, "缺少 Mcp-Method"},
		{[]string{"MCP-Protocol-Version", Modern, "Mcp-Method", "tools/call"}, "Mcp-Method 请求头是"},
	} {
		if r := f.post(body, c.kv...); r.status != 400 || r.code() != codeHeaderMismatch || !strings.Contains(r.raw, c.want) {
			t.Errorf("%v：%d %s", c.kv, r.status, r.raw)
		}
	}
	call := func(name string) map[string]any {
		x := p()
		x["name"], x["arguments"] = name, map[string]any{}
		return x
	}
	cb := legacyReq(2, "tools/call", call("proposal_status"))
	if r := f.post(cb, "MCP-Protocol-Version", Modern, "Mcp-Method", "tools/call"); r.code() != codeHeaderMismatch || !strings.Contains(r.raw, "缺少 Mcp-Name") {
		t.Errorf("没带 Mcp-Name：%s", r.raw)
	}
	if r := f.post(cb, "MCP-Protocol-Version", Modern, "Mcp-Method", "tools/call", "Mcp-Name", "app_info"); r.code() != codeHeaderMismatch {
		t.Errorf("Mcp-Name 对不上：%s", r.raw)
	}
	b64 := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("proposal_status")) + "?="
	if r := f.post(cb, "MCP-Protocol-Version", Modern, "Mcp-Method", "tools/call", "Mcp-Name", b64); r.status != 200 || r.result()["resultType"] != "complete" {
		t.Errorf("Base64 写法的 Mcp-Name：%d %s", r.status, r.raw)
	}
	if EncodeHeader("工具") == "工具" || EncodeHeader("=?base64?x?=") == "=?base64?x?=" || EncodeHeader("app_info") != "app_info" {
		t.Error("EncodeHeader 不对")
	}
	if v, ok := decodeHeader(EncodeHeader(" 带空格的 名字 ")); !ok || v != " 带空格的 名字 " {
		t.Error("Base64 写法往返不对")
	}
	noCaps := map[string]any{"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": Modern}}
	if r := f.post(legacyReq(3, "tools/list", noCaps), "MCP-Protocol-Version", Modern, "Mcp-Method", "tools/list"); r.status != 400 || r.code() != codeInvalidParams {
		t.Errorf("没带 clientCapabilities：%d %s", r.status, r.raw)
	}
	if r := f.modern(4, "resources/list", nil); r.status != 404 || r.code() != codeMethodNotFound {
		t.Errorf("2026-07-28 的未知方法应回 404：%d %s", r.status, r.raw)
	}
	if r := f.modern(5, "tools/list", nil, "Mcp-Session-Id", "whatever"); r.status != 200 || r.header.Get("Mcp-Session-Id") != "" {
		t.Errorf("2026-07-28 不发会话，带了也不管：%d", r.status)
	}
}
