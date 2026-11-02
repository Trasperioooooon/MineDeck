package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"minedeck/internal/assistant"
	"minedeck/internal/ops"
	"minedeck/internal/secret"
)

// memKeys 内存里的令牌存放处（Linux 上没有 DPAPI）。broken 为 true 时模拟「换了电脑，解不开」。
type memKeys struct {
	mu     sync.Mutex
	m      map[string]string
	broken bool
}

func (k *memKeys) Get(name string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.broken {
		return "", secret.ErrUnreadable
	}
	return k.m[name], nil
}

func (k *memKeys) Set(name, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[name] = v
	k.broken = false
	return nil
}

func (k *memKeys) State(name string) (bool, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[name] != "", k.broken
}

func (k *memKeys) token() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[KeyName]
}

const (
	winUser  = "alice"
	lanIP    = "192.168.1.23"
	aiSecret = "sk-live-0123456789abcdefghij"
	baseDir  = `C:\Users\alice\Desktop\MineDeck`
)

var (
	local = ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	lan   = ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.50"}
)

// fixture 一套 MCP 接口：登记了几个假操作的注册表（名字和样子照 ops/catalog）、本机的 httptest 服务。
type fixture struct {
	t    *testing.T
	reg  *ops.Registry
	svc  *Service
	keys *memKeys
	dir  string
	srv  *httptest.Server
	mu   sync.Mutex
	runs map[string][]string
	big  []string // instance.console.tail 返回的行
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, keys: &memKeys{}, dir: t.TempDir(), runs: map[string][]string{}}
	f.reg = ops.NewRegistry(ops.NewLog(filepath.Join(f.dir, "logs")))
	f.register()
	red := &assistant.Redactor{Base: baseDir, Home: `C:\Users\alice`, User: winUser, Computer: "DESKTOP-ALICE7", IPs: []string{lanIP},
		Secrets: func() []string { return []string{aiSecret, f.keys.token()} }}
	f.svc = New(Options{Ops: f.reg, Dir: filepath.Join(f.dir, "mcp"), Keys: f.keys, Redact: red, Version: "9.9.9",
		Exe: baseDir + `\MineDeck.exe`})
	f.srv = httptest.NewServer(f.svc)
	t.Cleanup(f.srv.Close)
	port, _ := strconv.Atoi(f.srv.URL[strings.LastIndex(f.srv.URL, ":")+1:])
	f.svc.Start(port, 27333)
	return f
}

// on 打开 MCP 接口，返回令牌。
func (f *fixture) on() string {
	f.t.Helper()
	if err := f.svc.SetEnabled(true); err != nil {
		f.t.Fatal(err)
	}
	return f.keys.token()
}

func (f *fixture) run(name string) func(context.Context, ops.Caller, ops.Args) (any, error) {
	return func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
		f.mu.Lock()
		f.runs[name] = append(f.runs[name], string(a))
		f.mu.Unlock()
		return map[string]string{"result": "ok", "path": baseDir + `\servers\s1\server.properties`}, nil
	}
}

func (f *fixture) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs[name])
}

func (f *fixture) lastArgs(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.runs[name]
	if len(r) == 0 {
		return ""
	}
	return r[len(r)-1]
}

func (f *fixture) register() {
	inst := ops.Object(map[string]*ops.Schema{"inst": ops.String("实例名称")}, "inst")
	ok := func(v any) func(context.Context, ops.Caller, ops.Args) (any, error) {
		return func(context.Context, ops.Caller, ops.Args) (any, error) { return v, nil }
	}
	f.reg.Register(&ops.Op{Name: "app.info", Title: "查看程序信息", Level: ops.Read, Run: ok(map[string]any{
		"version": "2.9.0", "base": baseDir, "ips": []string{lanIP, "10.0.0.5"}, "note": "key " + aiSecret,
		"config": map[string]any{"webhookUrl": "https://hook.example/x?token=hook-secret-77"}})})
	f.reg.Register(&ops.Op{Name: "app.update.check", Title: "检查更新", Level: ops.Read, Run: ok("x")})
	f.reg.Register(&ops.Op{Name: "game.info", Title: "游戏内查看", Level: ops.Read, InGame: true, Run: ok("x")})
	f.reg.Register(&ops.Op{Name: "instance.delete", Title: "删除实例", Level: ops.HumanOnly, Params: inst, Run: f.run("instance.delete")})
	f.reg.Register(&ops.Op{Name: "instance.props.get", Title: "读取 server.properties", Level: ops.Read, Params: inst, Run: ok(map[string]any{
		"pairs": []map[string]string{{"key": "max-players", "value": "20"}, {"key": "rcon.password", "value": "hunter2"}}})})
	f.reg.Register(&ops.Op{Name: "instance.props.set", Title: "修改 server.properties", Level: ops.Write,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例名称"), "pairs": ops.Array("键值对", ops.Object(map[string]*ops.Schema{
			"key": ops.String("键"), "value": ops.String("值")}, "key", "value"))}, "inst", "pairs"),
		Preview: func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
			return &ops.Plan{Summary: "修改「生存服」的 server.properties（" + baseDir + `\servers\s1）`,
				Changes: []ops.Change{{What: "max-players", Before: "20", After: "10"}}, Pre: map[string]string{"文件": "v1"}}, nil
		}, Run: f.run("instance.props.set")})
	f.reg.Register(&ops.Op{Name: "instance.restart", Title: "重启服务器", Level: ops.Danger, Params: inst,
		Preview: func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
			return &ops.Plan{Summary: "重启「生存服」", Warning: "重启：在线的 2 人会掉线"}, nil
		}, Run: f.run("instance.restart")})
	f.reg.Register(&ops.Op{Name: "instance.create", Title: "新建实例", Level: ops.Write, HumanParams: []string{"eula", "root"},
		Consent: &ops.Consent{Param: "eula", Text: "我已阅读并同意 Minecraft EULA", URL: "https://aka.ms/MinecraftEULA"},
		Params: ops.Object(map[string]*ops.Schema{"name": ops.String("名称").Len(1, 40), "core": ops.String("核心"), "eula": ops.Boolean("同意 EULA"),
			"root": ops.String("存放目录")}, "name"),
		Run: f.run("instance.create")})
	f.reg.Register(&ops.Op{Name: "instance.stop", Title: "停止服务器", Level: ops.Danger, Params: inst,
		Preview: func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
			return nil, ops.Errorf(400, "「生存服」没有在运行")
		},
		Run: f.run("instance.stop")})
	f.reg.Register(&ops.Op{Name: "instance.console.tail", Title: "查看控制台输出", Level: ops.Read, Params: inst,
		Run: func(context.Context, ops.Caller, ops.Args) (any, error) { return map[string]any{"lines": f.big}, nil }})
}

// reply 一次 POST /mcp 的结果。
type reply2 struct {
	status int
	header http.Header
	raw    string
	m      map[string]any
}

func (r reply2) err() map[string]any { e, _ := r.m["error"].(map[string]any); return e }
func (r reply2) code() int {
	e := r.err()
	if e == nil {
		return 0
	}
	n, _ := e["code"].(float64)
	return int(n)
}
func (r reply2) result() map[string]any { x, _ := r.m["result"].(map[string]any); return x }

// text tools/call 结果的文字和 isError。
func (r reply2) text() (string, bool) {
	res := r.result()
	if res == nil {
		return "", false
	}
	c, _ := res["content"].([]any)
	if len(c) != 1 {
		return "", false
	}
	t, _ := c[0].(map[string]any)["text"].(string)
	e, _ := res["isError"].(bool)
	return t, e
}

// post 发一个 POST /mcp（kv 是请求头：名, 值, 名, 值……；值为空串表示删掉这个头）。默认带上令牌。
func (f *fixture) post(body string, kv ...string) reply2 {
	f.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if tok := f.keys.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i] == "Host" {
			req.Host = kv[i+1]
			continue
		}
		if kv[i+1] == "" {
			req.Header.Del(kv[i])
		} else {
			req.Header.Set(kv[i], kv[i+1])
		}
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := reply2{status: resp.StatusCode, header: resp.Header, raw: string(b)}
	if len(bytes.TrimSpace(b)) > 0 && b[0] == '{' {
		if err := json.Unmarshal(b, &r.m); err != nil {
			f.t.Fatalf("回复不是 JSON：%s", b)
		}
	}
	return r
}

// js 编成 JSON。
func js(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSpace(buf.String())
}

// legacyReq 旧版的请求体。
func legacyReq(id any, method string, params map[string]any) string {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return js(m)
}

// modernMeta 2026-07-28 每个请求都带的 _meta。
func modernMeta(name string) map[string]any {
	return map[string]any{"io.modelcontextprotocol/protocolVersion": Modern, "io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo": map[string]any{"name": name, "version": "1.0.0"}}
}

// modern 发一个 2026-07-28 的请求：请求头照规范镜像（tools/call 带 Mcp-Name）。
func (f *fixture) modern(id any, method string, params map[string]any, kv ...string) reply2 {
	f.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["_meta"]; !ok {
		params["_meta"] = modernMeta("Cursor")
	}
	h := []string{"MCP-Protocol-Version", Modern, "Mcp-Method", method}
	if n, ok := params["name"].(string); ok && method == "tools/call" {
		h = append(h, "Mcp-Name", EncodeHeader(n))
	}
	return f.post(legacyReq(id, method, params), append(h, kv...)...)
}

// initialize 旧版握手，返回会话编号。
func (f *fixture) initialize(ver, name string, kv ...string) (reply2, string) {
	f.t.Helper()
	r := f.post(legacyReq(1, "initialize", map[string]any{"protocolVersion": ver, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": name, "version": "0.9"}}), kv...)
	return r, r.header.Get("Mcp-Session-Id")
}

// call 旧版的 tools/call（带会话和版本头）。
func (f *fixture) call(sid, tool string, args map[string]any, kv ...string) reply2 {
	f.t.Helper()
	h := append([]string{"Mcp-Session-Id", sid, "MCP-Protocol-Version", "2025-06-18"}, kv...)
	return f.post(legacyReq(7, "tools/call", map[string]any{"name": tool, "arguments": args}), h...)
}

func (f *fixture) records() []ops.Record {
	f.t.Helper()
	recs, err := f.reg.Log.Query(ops.Query{Limit: 1000})
	if err != nil {
		f.t.Fatal(err)
	}
	return recs
}

// proposalID 从工具结果的文字里取出提案编号。
func proposalID(t *testing.T, text string) string {
	t.Helper()
	i := strings.Index(text, "p_")
	if i < 0 || len(text) < i+14 {
		t.Fatalf("结果里没有提案编号：%s", text)
	}
	return text[i : i+14]
}
