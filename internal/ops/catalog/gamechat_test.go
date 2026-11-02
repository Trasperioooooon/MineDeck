package catalog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/chatai"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// TestMain 端到端测试把这个测试程序自己当成「java」启动，当一个假的服务器进程（见 fakeServer）。
func TestMain(m *testing.M) {
	if os.Getenv("MINEDECK_FAKE_SERVER") == "1" {
		fakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeServer 假的服务器进程：照 NeoForge 的格式打几行日志；标准输入收到的每一行原样（字节不变）记进 MINEDECK_FAKE_STDIN
// 指的文件；收到「fake-chat 名字 话」就打印一行这个人的聊天（测试借它在游戏里说话）；「weather clear」回一行执行结果；「stop」退出。
func fakeServer() {
	logf := func(logger, body string) {
		fmt.Printf("[20:15:00] [Server thread/INFO] [%s]: %s\n", logger, body)
	}
	logf("minecraft/DedicatedServer", `Done (1.000s)! For help, type "help"`)
	logf("minecraft/PlayerList", "Steve[/127.0.0.1:51234] logged in with entity id 112 at (8.5, 64.0, 8.5)")
	logf("minecraft/MinecraftServer", "Steve joined the game")
	f, err := os.OpenFile(os.Getenv("MINEDECK_FAKE_STDIN"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(3)
	}
	defer f.Close()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		_, _ = f.Write(append(append([]byte(nil), line...), '\n'))
		_ = f.Sync()
		s := string(line)
		switch {
		case strings.HasPrefix(s, "fake-chat "):
			name, msg, _ := strings.Cut(strings.TrimPrefix(s, "fake-chat "), " ")
			logf("minecraft/MinecraftServer", "[Not Secure] <"+name+"> "+msg)
		case s == "weather clear":
			logf("minecraft/MinecraftServer", "Set the weather to clear")
		case s == "stop":
			logf("minecraft/MinecraftServer", "Stopping server")
			return
		}
	}
}

// ---------- 假的 AI 服务（OpenAI 兼容，流式） ----------

type fakeAI struct {
	srv     *httptest.Server
	mu      sync.Mutex
	replies [][]string
	bodies  [][]byte
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
		chunks := []string{`{"choices":[{"index":0,"delta":{"role":"assistant","content":"（没有准备更多回复）"},"finish_reason":"stop"}]}`}
		if len(f.replies) > 0 {
			chunks, f.replies = f.replies[0], f.replies[1:]
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

func (f *fakeAI) requests() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.bodies...)
}

func jsq(v any) string { b, _ := json.Marshal(v); return string(b) }

const usageChunk = `{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}}`

func aiText(s string) []string {
	return []string{`{"choices":[{"index":0,"delta":{"role":"assistant","content":` + jsq(s) + `},"finish_reason":"stop"}]}`, usageChunk}
}

func aiCommand(cmd string) []string {
	args := jsq(map[string]string{"command": cmd})
	return []string{`{"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"run_command","arguments":` +
		jsq(args) + `}}]},"finish_reason":"tool_calls"}]}`, usageChunk}
}

type memKeys struct{}

func (memKeys) Get(string) (string, error) { return "", nil }
func (memKeys) Set(string, string) error   { return nil }
func (memKeys) State(string) (bool, bool)  { return false, false }

// gameEnv 一套带游戏内 AI 的后端：临时程序目录里一个 NeoForge 实例「生存服」（java 是这个测试程序自己），假的 AI 服务。
type gameEnv struct {
	reg   *ops.Registry
	mgr   *inst.Manager
	ai    *fakeAI
	dir   string // 实例目录
	stdin string // 假服务器记下的标准输入
}

var localUI = ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}

func newGameEnv(t *testing.T) *gameEnv {
	t.Helper()
	old := []string{app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir}
	t.Cleanup(func() {
		app.Base, app.ServersDir, app.BackupsDir, app.CacheDir, app.RuntimesDir = old[0], old[1], old[2], old[3], old[4]
	})
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	app.CacheDir = filepath.Join(app.Base, "cache")
	app.RuntimesDir = filepath.Join(app.Base, "runtimes")
	if _, err := app.UpdateConfig(func(c *app.Config) error { *c = app.Config{Source: "auto"}; return nil }); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	e := &gameEnv{ai: newFakeAI(t), dir: filepath.Join(app.ServersDir, "s1"), stdin: filepath.Join(app.Base, "stdin.txt")}
	if err := os.MkdirAll(filepath.Join(e.dir, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := json.Marshal(inst.Settings{Name: "生存服", Core: "neoforge", MC: "1.21.1", Build: "21.1.77", JavaMajor: 21, JavaPath: exe,
		XmxMB: 2048, XmsMB: 1024, LaunchTarget: "jar:server.jar"})
	files := map[string]string{
		filepath.Join(e.dir, "instance.json"):     string(s),
		filepath.Join(e.dir, "server.properties"): fmt.Sprintf("motd=hello\nmax-players=10\nonline-mode=false\nserver-port=%d\n", port),
		filepath.Join(e.dir, "mods", "a.jar"):     "jar", filepath.Join(e.dir, "mods", "b.jar"): "jar",
		filepath.Join(e.dir, "mods", "c.jar.disabled"): "jar",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.mgr, err = inst.NewManager()
	if err != nil {
		t.Fatal(err)
	}
	aiSvc := ai.NewService(ai.Options{Dir: filepath.Join(app.Base, "data", "ai"), Keys: memKeys{}, HTTP: e.ai.srv.Client(), PriceURL: e.ai.srv.URL + "/prices.json"})
	p := func(s string) *string { return &s }
	proto := ai.OpenAI
	if err := aiSvc.SetProvider(ai.ProviderPatch{Preset: p("custom"), Protocol: &proto, BaseURL: p(e.ai.srv.URL + "/v1"), Model: p("test-model")}); err != nil {
		t.Fatal(err)
	}
	e.reg = New(Env{Mgr: e.mgr, Tun: tunnel.NewManager(), AI: aiSvc, Port: 27333, Token: "tok-0123456789"})
	t.Setenv("MINEDECK_FAKE_SERVER", "1")
	t.Setenv("MINEDECK_FAKE_STDIN", e.stdin)
	return e
}

func (e *gameEnv) do(t *testing.T, c ops.Caller, name string, args map[string]any) (any, error) {
	t.Helper()
	return e.reg.Do(context.Background(), c, name, ops.ArgsOf(args))
}

func waitFor(t *testing.T, what string, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("等了 %v 还没有：%s", timeout, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// stdinLines 假服务器收到的标准输入（去掉测试自己用来「在游戏里说话」的 fake-chat 行）。
func (e *gameEnv) stdinLines() []string {
	b, _ := os.ReadFile(e.stdin)
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" && !strings.HasPrefix(l, "fake-chat ") {
			out = append(out, l)
		}
	}
	return out
}

// 端到端（§12 第 4 行）：真的 inst.Manager 起一个假的服务器进程，它的控制台出现一行聊天 → 游戏内 AI 认出提问 → 假的 AI 要执行
// weather clear → 写进服务器的标准输入 → 服务器的输出交还给 AI → 回复的 tellraw 写进标准输入。标准输入里的每个字节都是 ASCII。
func TestGameChatEndToEnd(t *testing.T) {
	e := newGameEnv(t)
	if _, err := e.do(t, localUI, "gameai.set", map[string]any{"inst": "生存服", "enabled": true, "allowed": []string{"Steve"}}); err != nil {
		t.Fatal(err)
	}
	e.ai.mu.Lock()
	e.ai.replies = [][]string{aiCommand("weather clear"), aiText("天气已经改成晴天了。")}
	e.ai.mu.Unlock()
	if err := e.mgr.Start("生存服"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.mgr.Stop("生存服")
		i, _ := e.mgr.Get("生存服")
		deadline := time.Now().Add(15 * time.Second)
		for i.Status() != "stopped" && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if i.Status() != "stopped" {
			_ = e.mgr.Kill("生存服")
		}
	})
	i, _ := e.mgr.Get("生存服")
	waitFor(t, "服务器启动完成、Steve 进服", 20*time.Second, func() bool {
		return i.Status() == "running" && len(i.OnlinePlayers()) == 1
	})
	if err := e.mgr.Command("生存服", "fake-chat Steve @AI 把天气改晴"); err != nil {
		t.Fatal(err)
	}
	reply := "tellraw @a {\"text\":\"[AI\\u2192Steve] \",\"color\":\"aqua\",\"extra\":[{\"text\":" +
		"\"\\u5929\\u6c14\\u5df2\\u7ecf\\u6539\\u6210\\u6674\\u5929\\u4e86\\u3002\",\"color\":\"white\"}]}"
	waitFor(t, "回复的 tellraw", 20*time.Second, func() bool {
		l := e.stdinLines()
		return len(l) > 0 && l[len(l)-1] == reply
	})
	lines := e.stdinLines()
	if len(lines) != 3 || lines[1] != "weather clear" || lines[2] != reply {
		t.Fatalf("标准输入：\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[0], "tellraw @a {\"text\":\"[AI] \",\"color\":\"aqua\",\"extra\":[{\"text\":\"") || !strings.HasSuffix(lines[0], "\"color\":\"gray\"}]}") {
		t.Errorf("隐私提示：%s", lines[0])
	}
	b, _ := os.ReadFile(e.stdin)
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "fake-chat ") {
			continue
		}
		for k := 0; k < len(l); k++ {
			if l[k] < 0x20 || l[k] > 0x7e {
				t.Fatalf("写进标准输入的命令里有非 ASCII 字节 0x%02x：%q", l[k], l)
			}
		}
	}
	// 服务器对 weather clear 的输出交还给了 AI
	reqs := e.ai.requests()
	if len(reqs) != 2 || !strings.Contains(string(reqs[1]), "Set the weather to clear") || !strings.Contains(string(reqs[0]), `"run_command"`) {
		t.Errorf("AI 的请求：%d 次", len(reqs))
	}
	// MineDeck 的控制台里显示看得懂的回显，不是 \u 转义
	con := strings.Join(i.Console.Lines(), "\n")
	if !strings.Contains(con, "[MineDeck] 游戏内 AI 替 Steve 执行：/weather clear") ||
		!strings.Contains(con, "[MineDeck] 游戏内 AI 回复（全服可见）：[AI→Steve] 天气已经改成晴天了。") || strings.Contains(con, "\\u5929") {
		t.Errorf("控制台：\n%s", con)
	}
	// 操作记录（来源 gamechat）
	recs, _ := e.reg.Log.Query(ops.Query{Src: ops.SrcGameChat})
	var got []string
	for _, r := range recs {
		got = append(got, fmt.Sprintf("%s|%s|%v", r.Op, r.Actor, r.OK))
	}
	if strings.Join(got, ",") != "gameai.ask|Steve|true,gameai.command|Steve|true" {
		t.Errorf("操作记录：%v", got)
	}
	// 最近的问答
	h, err := e.do(t, localUI, "gameai.log", map[string]any{"inst": "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	if rs := h.([]chatai.Record); len(rs) != 1 || rs[0].Reply != "天气已经改成晴天了。" || len(rs[0].Commands) != 1 || rs[0].Commands[0].Output != "Set the weather to clear" {
		t.Errorf("最近的问答：%+v", rs)
	}
	// 服务器运行中一键开启白名单：先 whitelist add 再 whitelist on；server.properties 在发 whitelist on 之前改好
	n := len(e.stdinLines())
	v, err := e.do(t, localUI, "gameai.whitelist", map[string]any{"inst": "生存服", "players": []string{"Steve", "Alex"}})
	if err != nil {
		t.Fatal(err)
	}
	if !v.(*chatai.View).WhitelistOn {
		t.Error("应已开启白名单")
	}
	waitFor(t, "whitelist on", 10*time.Second, func() bool { return len(e.stdinLines()) >= n+3 })
	if got := strings.Join(e.stdinLines()[n:], "|"); got != "whitelist add Steve|whitelist add Alex|whitelist on" {
		t.Errorf("运行中一键开白名单：%s", got)
	}
	if pr, _ := os.ReadFile(filepath.Join(e.dir, "server.properties")); !strings.Contains(string(pr), "white-list=true") {
		t.Errorf("server.properties：%s", pr)
	}
}

// game.info：游戏内 AI 唯一能调的操作，只读；游戏内调别的操作一律 403。
func TestGameInfo(t *testing.T) {
	e := newGameEnv(t)
	gc := ops.Caller{Source: ops.SrcGameChat, Actor: "Steve"}
	out, err := e.do(t, gc, "game.info", map[string]any{"inst": "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	gi := out.(gameInfo)
	if gi.Core != "NeoForge" || gi.MC != "1.21.1" || gi.Loader != "21.1.77" || gi.Mods != 2 || gi.MaxPlayers != 10 || gi.Status != "stopped" ||
		len(gi.Online) != 0 || !strings.Contains(gi.Time, "（星期") {
		t.Errorf("game.info：%+v", gi)
	}
	for _, name := range []string{"instance.list", "instance.props.get", "instance.command", "instance.stop", "gameai.get", "gameai.set", "app.info"} {
		if _, err := e.do(t, gc, name, map[string]any{"inst": "生存服"}); ops.CodeOf(err) != 403 {
			t.Errorf("游戏内调 %s 应 403：%v", name, err)
		}
	}
}

// 「游戏内 AI」标签的几个操作：改设置、一键开白名单仅本机；局域网能看、能做识别测试；没开白名单时一键开启（停着时改名单文件和 server.properties）。
func TestGameAIOps(t *testing.T) {
	e := newGameEnv(t)
	lan := ops.Caller{Source: ops.SrcLAN, Actor: "192.168.1.23"}
	v, err := e.do(t, lan, "gameai.get", map[string]any{"inst": "生存服"})
	if err != nil {
		t.Fatal(err)
	}
	if gv := v.(*chatai.View); gv.Local || gv.Settings.Enabled || gv.Settings.Prefix != "@AI" || gv.WhitelistOn || !gv.Supported {
		t.Errorf("默认的设置：%+v", gv)
	}
	if _, err := e.do(t, lan, "gameai.set", map[string]any{"inst": "生存服", "enabled": true}); ops.CodeOf(err) != 403 || !strings.Contains(err.Error(), "仅支持在本机修改") {
		t.Errorf("局域网改设置应 403：%v", err)
	}
	if _, err := e.do(t, lan, "gameai.whitelist", map[string]any{"inst": "生存服", "players": []string{"Steve"}}); ops.CodeOf(err) != 403 {
		t.Errorf("局域网一键开白名单应 403：%v", err)
	}
	if _, err := e.do(t, localUI, "gameai.set", map[string]any{"inst": "生存服", "prefix": "@A I"}); ops.CodeOf(err) != 400 {
		t.Errorf("前缀不合规应 400：%v", err)
	}
	if _, err := e.do(t, localUI, "gameai.set", map[string]any{"inst": "没有这个服", "enabled": true}); ops.CodeOf(err) != 404 {
		t.Errorf("实例不存在应 404：%v", err)
	}
	v, err = e.do(t, localUI, "gameai.set", map[string]any{"inst": "生存服", "enabled": true, "allowed": []string{"Steve"}, "templates": map[string]bool{"give": true}, "budget": 1000})
	if err != nil {
		t.Fatal(err)
	}
	if gv := v.(*chatai.View); !gv.Local || !gv.Settings.Enabled || !gv.Settings.Authorized("Steve") || !gv.Settings.Templates["give"] || gv.Settings.Budget != 1000 {
		t.Errorf("改后的设置：%+v", gv.Settings)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "gameai.json")); err != nil {
		t.Errorf("设置应存在实例目录里：%v", err)
	}
	r, err := e.do(t, lan, "gameai.test", map[string]any{"inst": "生存服", "line": "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: [Not Secure] <Steve> @AI /私 你好"})
	if err != nil {
		t.Fatal(err)
	}
	if tr := r.(*chatai.TestResult); !tr.Trigger() || tr.Player != "Steve" || !tr.Ask.Private || tr.Online || !strings.Contains(tr.Result, "不在线") {
		t.Errorf("识别测试：%+v", tr)
	}
	// 一键开启白名单：服务器停着时直接改名单文件和 server.properties
	if _, err := e.do(t, localUI, "gameai.whitelist", map[string]any{"inst": "生存服", "players": []string{"Mr-X"}}); ops.CodeOf(err) != 400 {
		t.Errorf("名字不合规应 400：%v", err)
	}
	v, err = e.do(t, localUI, "gameai.whitelist", map[string]any{"inst": "生存服", "players": []string{"Steve", "Alex"}})
	if err != nil {
		t.Fatal(err)
	}
	if !v.(*chatai.View).WhitelistOn {
		t.Error("应已开启白名单")
	}
	wl, _ := os.ReadFile(filepath.Join(e.dir, "whitelist.json"))
	pr, _ := os.ReadFile(filepath.Join(e.dir, "server.properties"))
	if !strings.Contains(string(wl), `"Steve"`) || !strings.Contains(string(wl), `"Alex"`) || !strings.Contains(string(pr), "white-list=true") {
		t.Errorf("白名单：%s\n%s", wl, pr)
	}
	recs, _ := e.reg.Log.Query(ops.Query{Op: "gameai."})
	var names []string
	for _, rc := range recs {
		names = append(names, rc.Op)
	}
	if strings.Join(names, ",") != "gameai.whitelist,gameai.whitelist,gameai.set,gameai.set,gameai.set" {
		t.Errorf("操作记录（看、测试不记，失败的也记）：%v", names)
	}
}
