package chatai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	srv     *httptest.Server
	mu      sync.Mutex
	replies [][]string
	bodies  [][]byte
	hold    chan struct{} // 不为 nil 时，每次回复前等它关闭（测「正忙」）
}

func newFakeAI(t *testing.T) *fakeAI {
	f := &fakeAI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, b)
		hold := f.hold
		f.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		f.mu.Lock()
		var chunks []string
		if len(f.replies) > 0 {
			chunks, f.replies = f.replies[0], f.replies[1:]
		} else {
			chunks = textReply("（测试里没有准备更多回复）")
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
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

// waitRequests 等假的 AI 收到 n 个请求（最多等 5 秒）。
func (f *fixture) waitRequests(n int) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(f.ai.requests()) < n {
		if time.Now().After(deadline) {
			f.t.Fatalf("等了 5 秒，假的 AI 只收到 %d 个请求", len(f.ai.requests()))
		}
		time.Sleep(5 * time.Millisecond)
	}
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

func textReply(text string) []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + js(text) + `},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		usageChunk(),
	}
}

type call struct{ id, name, args string }

func toolReply(calls ...call) []string {
	var tcs []string
	for k, c := range calls {
		tcs = append(tcs, fmt.Sprintf(`{"index":%d,"id":%s,"type":"function","function":{"name":%s,"arguments":%s}}`, k, js(c.id), js(c.name), js(c.args)))
	}
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[` + strings.Join(tcs, ",") + `]},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		usageChunk(),
	}
}

func cmdCall(id, command string) call {
	return call{id, "run_command", js(map[string]string{"command": command})}
}

func emptyReply() []string {
	return []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"想一想……"},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		usageChunk(),
	}
}

// request 一次请求里的工具名和消息。
type request struct {
	Tools []struct {
		Function struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"function"`
	} `json:"tools"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func parseReq(t *testing.T, b []byte) request {
	t.Helper()
	var r request
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r request) toolNames() []string {
	var out []string
	for _, t := range r.Tools {
		out = append(out, t.Function.Name)
	}
	return out
}

type memKeys struct{}

func (memKeys) Get(string) (string, error) { return "", nil }
func (memKeys) Set(string, string) error   { return nil }
func (memKeys) State(string) (bool, bool)  { return false, false }

// ---------- 假的实例 ----------

const instName = "生存服"

// fakeBackend 一个运行中的实例：记下写进标准输入的每一行；执行 weather / time 这类命令后像服务器一样回一行输出。
type fakeBackend struct {
	mu      sync.Mutex
	dir     string
	online  []string
	wl, ops []string
	wlOn    bool
	started time.Time
	status  string
	proxy   bool
	sent    []string
	echoes  []string
	svc     *Service
	reply   func(cmd string) string // 执行命令后服务器回的一行（空串不回）
}

func (b *fakeBackend) Dir(name string) (string, error) {
	if name != instName {
		return "", fmt.Errorf("实例不存在: %s", name)
	}
	return b.dir, nil
}

func (b *fakeBackend) Info(name string) (Info, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Info{Core: "neoforge", MC: "1.21.1", Status: b.status, Started: b.started, Proxy: b.proxy}, nil
}

func (b *fakeBackend) Online(string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.online...)
}

func (b *fakeBackend) Players(string) (Players, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Players{Online: append([]string(nil), b.online...), Whitelist: b.wl, Ops: b.ops, WhitelistOn: b.wlOn}, nil
}

func (b *fakeBackend) Send(name, cmd, echo string) error {
	b.mu.Lock()
	b.sent = append(b.sent, cmd)
	b.echoes = append(b.echoes, echo)
	reply := b.reply
	b.mu.Unlock()
	if reply != nil && !strings.HasPrefix(cmd, "tellraw ") {
		if out := reply(cmd); out != "" {
			b.svc.OnLine(name, out)
		}
	}
	return nil
}

func (b *fakeBackend) lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.sent...)
}

func (b *fakeBackend) reset() {
	b.mu.Lock()
	b.sent, b.echoes = nil, nil
	b.mu.Unlock()
}

// fixture 一套游戏内 AI：假实例（Steve、Alex 在线）、假 AI、登记了 game.info 的注册表、可以拨的时钟。
type fixture struct {
	t   *testing.T
	ai  *fakeAI
	svc *ai.Service
	reg *ops.Registry
	b   *fakeBackend
	gc  *Service

	clock sync.Mutex
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWith(t, nil)
}

// newFixtureWith 同 newFixture，再接上 AI 伙伴（第七阶段）。
func newFixtureWith(t *testing.T, comps Companions) *fixture {
	t.Helper()
	f := &fixture{t: t, ai: newFakeAI(t), now: time.Date(2026, 10, 28, 20, 15, 0, 0, time.Local)}
	dir := t.TempDir()
	svc := ai.NewService(ai.Options{Dir: filepath.Join(dir, "ai"), Keys: memKeys{}, HTTP: f.ai.srv.Client(), PriceURL: f.ai.srv.URL + "/prices.json"})
	p := func(s string) *string { return &s }
	proto := ai.OpenAI
	if err := svc.SetProvider(ai.ProviderPatch{Preset: p("custom"), Protocol: &proto, BaseURL: p(f.ai.srv.URL + "/v1"), Model: p("test-model")}); err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	f.reg = ops.NewRegistry(ops.NewLog(filepath.Join(dir, "logs")))
	f.reg.Register(&ops.Op{Name: "game.info", Title: "游戏内 AI 查看服务器信息", Level: ops.Read, InGame: true,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例")}, "inst"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			return map[string]any{"core": "NeoForge", "mc": "1.21.1", "mods": 87, "online": f.b.Online(instName), "time": "2026-10-28 20:15（星期三）",
				"lan": "192.168.1.23"}, nil
		}})
	f.b = &fakeBackend{dir: filepath.Join(dir, "srv"), online: []string{"Steve", "Alex"}, status: "running", started: f.now.Add(-time.Hour),
		reply: func(cmd string) string {
			switch {
			case strings.HasPrefix(cmd, "weather clear"):
				return "[20:15:03] [Server thread/INFO] [minecraft/MinecraftServer]: Set the weather to clear"
			case strings.HasPrefix(cmd, "time set day"):
				return "[20:15:03] [Server thread/INFO] [minecraft/MinecraftServer]: Set the time to 1000"
			}
			return ""
		}}
	if err := os.MkdirAll(f.b.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.gc = New(Options{Backend: f.b, AI: svc, Ops: f.reg, Redact: redactLAN{}, Now: f.clockNow, CmdWait: 30 * time.Millisecond, Timeout: 5 * time.Second,
		Companions: comps})
	f.b.svc = f.gc
	t.Cleanup(func() { f.gc.Wait(); f.gc.Close() })
	f.set(Patch{Enabled: ptr(true)})
	return f
}

// redactLAN 测试用的脱敏：把局域网 IP 换掉。
type redactLAN struct{}

func (redactLAN) Text(s string) string { return strings.ReplaceAll(s, "192.168.1.23", "<局域网IP>") }

func ptr[T any](v T) *T { return &v }

func (f *fixture) clockNow() time.Time {
	f.clock.Lock()
	defer f.clock.Unlock()
	return f.now
}

func (f *fixture) advance(d time.Duration) {
	f.clock.Lock()
	f.now = f.now.Add(d)
	f.clock.Unlock()
}

func (f *fixture) set(p Patch) *View {
	f.t.Helper()
	v, err := f.gc.Update(instName, p)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

// say 某人在游戏里说了一句话（NeoForge 的聊天行），等处理完。
func (f *fixture) say(player, msg string) {
	f.t.Helper()
	f.gc.OnLine(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: [Not Secure] <"+player+"> "+msg)
	f.gc.Wait()
}

func (f *fixture) history() []Record {
	f.t.Helper()
	h, err := f.gc.History(instName, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

// tellraws 写进标准输入的 tellraw，解出目标、前缀和正文。
type sentMsg struct{ target, head, body, color string }

func (f *fixture) tellraws() []sentMsg {
	f.t.Helper()
	var out []sentMsg
	for _, l := range f.b.lines() {
		if !strings.HasPrefix(l, "tellraw ") {
			continue
		}
		asciiOnly(f.t, l)
		rest := strings.TrimPrefix(l, "tellraw ")
		target, js, _ := strings.Cut(rest, " ")
		var v tellrawJSON
		if err := json.Unmarshal([]byte(js), &v); err != nil || len(v.Extra) != 1 {
			f.t.Fatalf("tellraw 不对：%s", l)
		}
		out = append(out, sentMsg{target, v.Text, v.Extra[0].Text, v.Extra[0].Color})
	}
	return out
}

// commands 写进标准输入的命令（tellraw 以外的）。
func (f *fixture) commands() []string {
	var out []string
	for _, l := range f.b.lines() {
		if !strings.HasPrefix(l, "tellraw ") {
			out = append(out, l)
		}
	}
	return out
}

func (f *fixture) oplog() []ops.Record {
	f.t.Helper()
	recs, err := f.reg.Log.Query(ops.Query{})
	if err != nil {
		f.t.Fatal(err)
	}
	return recs
}
