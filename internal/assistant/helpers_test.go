package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// ---------- 假的 AI 服务（OpenAI 兼容，流式） ----------

// fakeAI 按顺序交出准备好的回复，记下每次请求的请求体。测试里不访问真实网络。
type fakeAI struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	replies [][]string // 每次回复的 SSE 数据块
	bodies  [][]byte
	hold    chan struct{} // 不为 nil 时，回复写出第一块后等它关闭（测「停止」）
}

func newFakeAI(t *testing.T) *fakeAI {
	f := &fakeAI{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r) // 价格文件之类的一律 404
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, b)
		var chunks []string
		if len(f.replies) > 0 {
			chunks, f.replies = f.replies[0], f.replies[1:]
		} else {
			chunks = textReply("（测试里没有准备更多回复）")
		}
		hold := f.hold
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for k, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			fl.Flush()
			if k == 0 && hold != nil {
				select {
				case <-hold:
				case <-r.Context().Done():
					return
				}
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAI) queue(rs ...[]string) {
	f.mu.Lock()
	f.replies = append(f.replies, rs...)
	f.mu.Unlock()
}

func (f *fakeAI) requests() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.bodies...)
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func usageChunk() string {
	return `{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`
}

// textReply 普通的文字回复，分两块推过来。
func textReply(text string) []string {
	r := []rune(text)
	half := len(r) / 2
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + js(string(r[:half])) + `},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"content":` + js(string(r[half:])) + `},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		usageChunk(),
	}
}

type call struct{ id, name, args string }

// toolReply 调用工具的回复（可以带一句话）。
func toolReply(text string, calls ...call) []string {
	var tcs []string
	for k, c := range calls {
		tcs = append(tcs, fmt.Sprintf(`{"index":%d,"id":%s,"type":"function","function":{"name":%s,"arguments":%s}}`, k, js(c.id), js(c.name), js(c.args)))
	}
	content := "null"
	if text != "" {
		content = js(text)
	}
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + content + `,"tool_calls":[` + strings.Join(tcs, ",") + `]},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		usageChunk(),
	}
}

// emptyReply 只有思考内容、没有正文的回复（OpenAI 兼容的服务有时会这样）。
func emptyReply() []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"想一想……"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		usageChunk(),
	}
}

// reqMessages 一次请求里的 messages（每条的原文）。
func reqMessages(t *testing.T, body []byte) []json.RawMessage {
	t.Helper()
	var r struct {
		Messages []json.RawMessage `json:"messages"`
		Tools    []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r.Messages
}

// ---------- 假的操作层 ----------

type memKeys struct{}

func (memKeys) Get(string) (string, error) { return "", nil }
func (memKeys) Set(string, string) error   { return nil }
func (memKeys) State(string) (bool, bool)  { return false, false }

// fixture 一套助手：假的 AI 服务、登记了几个假操作的注册表（名字和返回的样子照 ops/catalog）。
type fixture struct {
	t    *testing.T
	ai   *fakeAI
	svc  *ai.Service
	reg  *ops.Registry
	as   *Service
	dir  string
	mu   sync.Mutex
	runs map[string][]string // 操作名 → 每次执行收到的参数
	file string              // instance.props.set 预览时看的「文件内容」（前置条件）
}

const (
	winUser  = "alice"
	lanIP    = "192.168.1.23"
	aiSecret = "sk-live-0123456789abcdefghij"
)

var (
	local = ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	lan   = ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.50"}
)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, ai: newFakeAI(t), dir: t.TempDir(), runs: map[string][]string{}, file: "max-players=20"}
	svc := ai.NewService(ai.Options{Dir: filepath.Join(f.dir, "ai"), Keys: memKeys{}, HTTP: f.ai.srv.Client(), PriceURL: f.ai.srv.URL + "/prices.json"})
	p := func(s string) *string { return &s }
	proto := ai.OpenAI
	if err := svc.SetProvider(ai.ProviderPatch{Preset: p("custom"), Protocol: &proto, BaseURL: p(f.ai.srv.URL + "/v1"), Model: p("test-model")}); err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	f.reg = ops.NewRegistry(ops.NewLog(filepath.Join(f.dir, "logs")))
	f.register()
	base := `C:\Users\alice\Desktop\MineDeck`
	f.as = New(Options{Ops: f.reg, AI: svc, Dir: filepath.Join(f.dir, "sessions"),
		Redact: &Redactor{Base: base, Home: `C:\Users\alice`, User: winUser, Computer: "DESKTOP-ALICE7", IPs: []string{lanIP},
			Secrets: func() []string { return []string{aiSecret} }}})
	t.Cleanup(f.idle) // 测试结束前等后台的回复跑完（先于临时目录的清理执行）
	return f
}

// idle 等所有会话都不忙了。
func (f *fixture) idle() {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		f.as.mu.Lock()
		for _, sess := range f.as.sessions {
			sess.mu.Lock()
			busy = busy || sess.busy
			sess.mu.Unlock()
		}
		f.as.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fixture) run(name string) func(context.Context, ops.Caller, ops.Args) (any, error) {
	return func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
		f.mu.Lock()
		f.runs[name] = append(f.runs[name], string(a))
		f.mu.Unlock()
		return map[string]string{"result": "ok"}, nil
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
		"version": "2.6.1", "os": "Windows 11（10.0.26300）", "base": `C:\Users\alice\Desktop\MineDeck`, "ramMb": 16384, "availRamMb": 9216,
		"ips": []string{lanIP, "10.0.0.5"}, "config": map[string]any{"webhookUrl": "https://hook.example/x?token=hook-secret-77", "cfApiKey": ""}})})
	f.reg.Register(&ops.Op{Name: "app.update.check", Title: "检查更新", Level: ops.Read, Run: ok("x")})
	f.reg.Register(&ops.Op{Name: "app.config.set", Title: "修改全局设置", Level: ops.HumanOnly, Run: f.run("app.config.set")})
	f.reg.Register(&ops.Op{Name: "instance.list", Title: "查看实例列表", Level: ops.Read, Run: ok([]map[string]any{{
		"name": "生存服", "core": "neoforge", "mc": "1.21.1", "build": "21.1.77", "status": "stopped", "javaMajor": 17, "javaSource": "portable",
		"xmxMb": 6144, "xmsMb": 1024, "port": 25565, "extraJvm": []string{"-XX:+UseG1GC"}, "dir": `C:\Users\alice\Desktop\MineDeck\servers\s1`}})})
	f.reg.Register(&ops.Op{Name: "instance.props.get", Title: "读取 server.properties", Level: ops.Read, Params: inst, Run: ok(map[string]any{
		"pairs": []map[string]string{{"key": "max-players", "value": "20"}, {"key": "rcon.password", "value": "hunter2"}, {"key": "online-mode", "value": "false"}}})})
	f.reg.Register(&ops.Op{Name: "instance.props.set", Title: "修改 server.properties", Level: ops.Write,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例名称"), "pairs": ops.Array("键值对", ops.Object(map[string]*ops.Schema{
			"key": ops.String("键"), "value": ops.String("值")}, "key", "value"))}, "inst", "pairs"),
		Preview: func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return &ops.Plan{Summary: "修改「生存服」的 server.properties", Changes: []ops.Change{{What: "max-players", Before: "20", After: "10"}},
				Pre: map[string]string{"server.properties": f.file}}, nil
		}, Run: f.run("instance.props.set")})
	f.reg.Register(&ops.Op{Name: "instance.stop", Title: "停止服务器", Level: ops.Danger, Params: inst,
		Preview: func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
			return &ops.Plan{Summary: "停止「生存服」", Warning: "停服：在线的 2 人会掉线"}, nil
		}, Run: f.run("instance.stop")})
	f.reg.Register(&ops.Op{Name: "instance.create", Title: "新建实例", Level: ops.Write, HumanParams: []string{"eula", "root"},
		Consent: &ops.Consent{Param: "eula", Text: "我已阅读并同意 Minecraft EULA", URL: "https://aka.ms/MinecraftEULA"},
		Params: ops.Object(map[string]*ops.Schema{"name": ops.String("名称"), "core": ops.String("核心"), "eula": ops.Boolean("同意 EULA"),
			"root": ops.String("存放目录")}, "name"),
		Run: f.run("instance.create")})
	f.reg.Register(&ops.Op{Name: "t.localw", Title: "仅本机的写操作", Level: ops.Write, LocalOnly: true, Run: f.run("t.localw")})
	f.reg.Register(&ops.Op{Name: "instance.diag", Title: "本地规则排错", Level: ops.Read, Params: inst, Run: ok(map[string]any{
		"findings": []map[string]any{{"rule": "missing-deps", "title": "缺少前置模组", "summary": "少装了前置模组：geckolib",
			"details": []string{"create 需要 geckolib（版本 [4.4,)），但没有装"}, "mods": []string{"geckolib", "create"}, "severity": "error"}},
		"crashed": true})})
	f.reg.Register(&ops.Op{Name: "instance.console.tail", Title: "查看控制台输出", Level: ops.Read,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例"), "since": ops.String("范围"), "lines": ops.Integer("行数").Range(1, 3000)}, "inst"),
		Run: ok(map[string]any{"lines": []string{
			`[MineDeck] 启动: java -Xmx6144M -jar C:\Users\alice\Desktop\MineDeck\servers\s1\server.jar nogui`,
			`[09:30:01] [main/INFO] [x/]: Loading from C:\Users\alice\AppData\Roaming\.minecraft and D:/Users/bob/mods`,
			"[09:30:03] [main/ERROR] [ne.ne.fm.lo.ModSorter/LOADING]: Missing or unsupported mandatory dependencies:",
			"\tMod ID: 'geckolib', Requested by: 'create', Expected range: '[4.4,)', Actual version: '[MISSING]'",
			"[09:30:04] [Server thread/INFO] [minecraft/DedicatedServer]: Starting Minecraft server on " + lanIP + ":25565",
			"[09:30:05] [main/WARN] [x/]: key leaked " + aiSecret + " and token=ghp_abcdefghijklmnopqrstuvwxyz0123",
			"[09:30:06] [main/INFO] [x/]: player Steve logged in from 10.0.0.77 on DESKTOP-ALICE7",
			"[MineDeck] 服务器进程已退出（exit status 1）"}})})
	f.reg.Register(&ops.Op{Name: "instance.crashes", Title: "查看崩溃报告", Level: ops.Read,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例"), "file": ops.String("文件")}, "inst"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			var p struct{ File string }
			_ = a.Bind(&p)
			if p.File == "" {
				return map[string]any{"reports": []map[string]any{{"name": "crash-2026-10-28_09.30.10-server.txt", "kind": "crash", "thisStart": true},
					{"name": "crash-old.txt", "kind": "crash", "thisStart": false}}}, nil
			}
			return map[string]any{"summary": "Description: Mod loading failed\n\tJava Version: 17.0.12\n\tJVM Flags: 2 total; -Xmx6144M USERNAME=alice"}, nil
		}})
	f.reg.Register(&ops.Op{Name: "instance.mods.list", Title: "查看模组清单", Level: ops.Read,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例"), "q": ops.String("关键字"), "detail": ops.Boolean("详情")}, "inst"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			var p struct{ Q string }
			_ = a.Bind(&p)
			if p.Q == "create" {
				return map[string]any{"count": 2, "mods": []map[string]any{{"file": "create-1.21.1-6.0.4.jar", "id": "create", "version": "6.0.4",
					"deps": []map[string]any{{"id": "geckolib", "range": "[4.4,)", "required": true}}}}}, nil
			}
			return map[string]any{"count": 2, "disabled": 0, "mods": []map[string]any{}}, nil
		}})
}

// open 开一个本机的会话，关联「生存服」。
func (f *fixture) open(who ops.Caller) string {
	f.t.Helper()
	v, err := f.as.Create(who, "生存服")
	if err != nil {
		f.t.Fatal(err)
	}
	return v.ID
}

// wait 等后台的这一轮跑完（不忙了，而且 cond 成立）。
func (f *fixture) wait(id string, cond func(*View) bool) *View {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, err := f.as.Get(local, id)
		if err != nil {
			f.t.Fatal(err)
		}
		if !v.Busy && (cond == nil || cond(v)) {
			return v
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("等不到会话跑完：%s", dump(v))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func dump(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	_ = enc.Encode(v)
	return b.String()
}

// tool 找会话里的一次工具调用。
func tool(v *View, name string) *ToolItem {
	for _, it := range v.Items {
		for _, t := range it.Tools {
			if t.Name == name {
				return t
			}
		}
	}
	return nil
}

func notices(v *View) string {
	var s []string
	for _, it := range v.Items {
		if it.Kind == "notice" {
			s = append(s, it.Text)
		}
	}
	return strings.Join(s, " | ")
}
