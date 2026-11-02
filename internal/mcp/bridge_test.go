package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf 并发安全的缓冲（桥的标准错误）。
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// pipeBridge 在进程里跑一个桥：in 写给它的标准输入，lines 是它写到标准输出的每一行。
type pipeBridge struct {
	t      *testing.T
	in     *io.PipeWriter
	lines  chan string
	stderr *syncBuf
	done   chan error
}

func startBridge(t *testing.T, f *fixture, edit func(*BridgeOptions)) *pipeBridge {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	pb := &pipeBridge{t: t, in: inW, lines: make(chan string, 64), stderr: &syncBuf{}, done: make(chan error, 1)}
	o := BridgeOptions{Dir: filepath.Join(f.dir, "mcp"), Base: f.dir, In: inR, Out: outW, Err: pb.stderr, HTTP: f.srv.Client(), Version: "9.9.9",
		Token: func() (string, error) { return f.keys.token(), nil },
		Alive: func(pid int, _ string) error {
			if pid != os.Getpid() {
				return errors.New("进程不在了")
			}
			return nil
		}}
	if edit != nil {
		edit(&o)
	}
	go func() { pb.done <- Bridge(context.Background(), o); outW.Close() }()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for sc.Scan() {
			pb.lines <- sc.Text()
		}
		close(pb.lines)
	}()
	t.Cleanup(func() { inW.Close() })
	return pb
}

func (pb *pipeBridge) send(s string) {
	pb.t.Helper()
	if _, err := io.WriteString(pb.in, s+"\n"); err != nil {
		pb.t.Fatal(err)
	}
}

// next 读下一行回复（一行一条，必须是合法的 JSON）。
func (pb *pipeBridge) next() map[string]any {
	pb.t.Helper()
	select {
	case l, ok := <-pb.lines:
		if !ok {
			pb.t.Fatal("桥的标准输出关了")
		}
		if strings.ContainsAny(l, "\r") {
			pb.t.Fatalf("一条消息里不能有换行：%q", l)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			pb.t.Fatalf("标准输出里只能有 MCP 消息：%q", l)
		}
		return m
	case <-time.After(5 * time.Second):
		pb.t.Fatal("等不到回复")
	}
	return nil
}

// quiet 一段时间内没有回复。
func (pb *pipeBridge) quiet(d time.Duration) {
	pb.t.Helper()
	select {
	case l := <-pb.lines:
		pb.t.Fatalf("不该有回复：%s", l)
	case <-time.After(d):
	}
}

func resText(m map[string]any) string {
	res, _ := m["result"].(map[string]any)
	c, _ := res["content"].([]any)
	if len(c) == 0 {
		return ""
	}
	s, _ := c[0].(map[string]any)["text"].(string)
	return s
}

func errOf(m map[string]any) (int, string) {
	e, _ := m["error"].(map[string]any)
	if e == nil {
		return 0, ""
	}
	n, _ := e["code"].(float64)
	s, _ := e["message"].(string)
	return int(n), s
}

// 旧版客户端经桥：握手、列工具、调写工具得到提案；桥记住会话和版本；MineDeck 重启过（认不出旧会话）也照样记对名字；
// 标准输入关了桥就退出。
func TestBridgeLegacy(t *testing.T) {
	f := newFixture(t)
	f.on()
	pb := startBridge(t, f, nil)
	pb.send(legacyReq(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "Claude Desktop", "version": "1.2.3"}}))
	m := pb.next()
	if res, _ := m["result"].(map[string]any); m["id"].(float64) != 1 || res["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize：%v", m)
	}
	pb.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	pb.send(legacyReq(2, "tools/list", nil))
	m = pb.next()
	if m["id"].(float64) != 2 || !strings.Contains(js(m["result"]), `"name":"instance_props_set"`) || !strings.Contains(js(m["result"]), `"title"`) {
		t.Fatalf("tools/list（2025-06-18 的工具带 title）：%.300s", js(m))
	}
	pb.send(legacyReq("c-3", "tools/call", map[string]any{"name": "instance_props_set", "arguments": map[string]any{"inst": "生存服",
		"pairs": []map[string]string{{"key": "max-players", "value": "10"}}}}))
	m = pb.next()
	if m["id"] != "c-3" || !strings.Contains(resText(m), "已生成提案") {
		t.Fatalf("tools/call：%v", m)
	}
	pend := f.svc.Pending()
	if len(pend) != 1 || pend[0].Actor != "Claude Desktop" {
		t.Fatalf("提案的提出者：%+v", pend)
	}
	cl := f.svc.Status(true).Clients
	if len(cl) != 1 || cl[0].Transport != "stdio" || cl[0].Protocol != "2025-06-18" || cl[0].Calls != 1 || cl[0].Version != "1.2.3" {
		t.Fatalf("连过的客户端：%+v", cl)
	}
	// 模拟 MineDeck 重启：会话都没了，桥带着旧会话来
	f.svc.mu.Lock()
	f.svc.sessions = map[string]*session{}
	f.svc.mu.Unlock()
	pb.send(legacyReq(4, "tools/call", map[string]any{"name": "instance_restart", "arguments": map[string]any{"inst": "生存服"}}))
	m = pb.next()
	pid := proposalID(t, resText(m))
	if a := f.reg.Proposal(pid).Caller.Actor; a != "Claude Desktop" {
		t.Errorf("重启后认不出会话，名字靠桥带来：%q", a)
	}
	pb.in.Close()
	select {
	case err := <-pb.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("标准输入关了，桥应该退出")
	}
}

// 2026-07-28 的客户端经桥：桥按请求体补上 MCP-Protocol-Version、Mcp-Method、Mcp-Name，服务端的核对能通过。
func TestBridgeModern(t *testing.T) {
	f := newFixture(t)
	f.on()
	pb := startBridge(t, f, nil)
	pb.send(legacyReq(1, "server/discover", map[string]any{"_meta": modernMeta("TS SDK")}))
	m := pb.next()
	if !strings.Contains(js(m["result"]), `"supportedVersions":["2026-07-28"`) {
		t.Fatalf("server/discover：%v", m)
	}
	pb.send(legacyReq(2, "tools/call", map[string]any{"name": "app_info", "arguments": map[string]any{}, "_meta": modernMeta("TS SDK")}))
	m = pb.next()
	if res, _ := m["result"].(map[string]any); res["resultType"] != "complete" || !strings.Contains(resText(m), "<程序目录>") {
		t.Fatalf("tools/call：%v", m)
	}
	if cl := f.svc.Status(true).Clients; len(cl) != 1 || cl[0].Name != "TS SDK" || cl[0].Transport != "stdio" || cl[0].Protocol != Modern {
		t.Fatalf("连过的客户端：%+v", cl)
	}
}

// stdio 上的取消通知：桥断开那个请求，不再回它；别的请求照常。
func TestBridgeCancel(t *testing.T) {
	f := newFixture(t)
	f.on()
	pb := startBridge(t, f, nil)
	pb.send(legacyReq(1, "tools/call", map[string]any{"name": "instance_restart", "arguments": map[string]any{"inst": "生存服"}}))
	pid := proposalID(t, resText(pb.next()))
	pb.send(legacyReq(41, "tools/call", map[string]any{"name": StatusTool, "arguments": map[string]any{"id": pid, "wait": 30}}))
	time.Sleep(100 * time.Millisecond)
	pb.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":41,"reason":"用户停止"}}`)
	pb.send(legacyReq(42, "ping", nil))
	if m := pb.next(); m["id"].(float64) != 42 {
		t.Fatalf("应该先回 ping：%v", m)
	}
	pb.quiet(500 * time.Millisecond)
	pb.send("这不是 JSON")
	if code, _ := errOf(pb.next()); code != codeParse {
		t.Fatal("不是 JSON 的一行应回解析错误")
	}
}

// MineDeck 没有运行（没有运行时文件）：每个请求回 JSON-RPC 错误（带原来的 id），说明怎么办，也写到标准错误；
// MineDeck 打开了以后，下一个请求就能通。运行时文件是上次留下的（进程不在了）同样算没运行。
func TestBridgeNotRunning(t *testing.T) {
	f := newFixture(t)
	pb := startBridge(t, f, nil)
	pb.send(legacyReq(7, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	m := pb.next()
	if code, msg := errOf(m); code != CodeNotRunning || m["id"].(float64) != 7 || !strings.Contains(msg, "没有找到运行着的 MineDeck") {
		t.Fatalf("没运行：%v", m)
	}
	if !strings.Contains(pb.stderr.String(), "没有找到运行着的 MineDeck") {
		t.Errorf("标准错误：%s", pb.stderr.String())
	}
	pb.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`) // 通知没法回，只记标准错误
	pb.quiet(200 * time.Millisecond)
	f.on()
	pb.send(legacyReq(8, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	if m := pb.next(); m["result"] == nil {
		t.Fatalf("打开以后应能通：%v", m)
	}
	// 上次留下的运行时文件：进程号对不上
	b, _ := os.ReadFile(filepath.Join(f.dir, "mcp", RuntimeFile))
	var rt Runtime
	_ = json.Unmarshal(b, &rt)
	rt.PID++
	b, _ = json.Marshal(rt)
	_ = os.WriteFile(filepath.Join(f.dir, "mcp", RuntimeFile), b, 0o600)
	pb.send(legacyReq(9, "ping", nil))
	if code, _ := errOf(pb.next()); code != CodeNotRunning || !strings.Contains(pb.stderr.String(), "上次留下的") {
		t.Fatalf("上次留下的运行时文件：%s", pb.stderr.String())
	}
}

// MCP 接口关着（运行时文件还在，比如刚关）、令牌读不出来、令牌不对：都回带 id 的 JSON-RPC 错误和中文说明。
func TestBridgeRefusals(t *testing.T) {
	f := newFixture(t)
	f.on()
	rt, _ := os.ReadFile(filepath.Join(f.dir, "mcp", RuntimeFile))
	if err := f.svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(f.dir, "mcp", RuntimeFile), rt, 0o600)
	var tokErr error
	tok := f.keys.token()
	pb := startBridge(t, f, func(o *BridgeOptions) {
		o.Token = func() (string, error) { return tok, tokErr }
	})
	pb.send(legacyReq("a", "tools/list", nil))
	m := pb.next()
	if code, msg := errOf(m); code != CodeDisabled || m["id"] != "a" || !strings.Contains(msg, "MCP 接口没有打开") {
		t.Fatalf("关着：%v", m)
	}
	if !strings.Contains(pb.stderr.String(), "MCP 接口没有打开") {
		t.Errorf("标准错误：%s", pb.stderr.String())
	}
	f.on()
	_ = os.WriteFile(filepath.Join(f.dir, "mcp", RuntimeFile), rt, 0o600)
	tokErr = errors.New("密钥无法解开")
	pb.send(legacyReq("b", "tools/list", nil))
	if code, msg := errOf(pb.next()); code != CodeAuth || !strings.Contains(msg, "读不出 MineDeck 的 MCP 令牌") {
		t.Fatalf("令牌读不出来：%d %s", code, msg)
	}
	tokErr, tok = nil, "wrong-token"
	pb.send(legacyReq("c", "tools/list", nil))
	m = pb.next()
	if code, msg := errOf(m); code != CodeAuth || m["id"] != "c" || !strings.Contains(msg, "令牌不对") {
		t.Fatalf("令牌不对：%v", m)
	}
}
