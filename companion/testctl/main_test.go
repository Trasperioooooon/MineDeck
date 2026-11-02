package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	for _, n := range []int{0, 1, 300, maxPayload} {
		buf.Reset()
		p := bytes.Repeat([]byte{'x'}, n)
		if err := writeFrame(&buf, typeEvent, p); err != nil {
			t.Fatal(err)
		}
		if got := buf.Bytes(); got[0] != typeEvent || got[1] != 0 || int(binary.LittleEndian.Uint16(got[2:])) != n {
			t.Fatalf("帧头不对：% x", got[:4])
		}
		f, err := readFrame(&buf)
		if err != nil || f.typ != typeEvent || !bytes.Equal(f.payload, p) {
			t.Fatalf("读回来不对：%v %d", err, len(f.payload))
		}
	}
	if err := writeFrame(&buf, typeEvent, make([]byte, maxPayload+1)); err == nil {
		t.Fatal("超过 65535 字节应该报错")
	}
	// 一帧只读到一半
	if _, err := readFrame(bytes.NewReader([]byte{typeEvent, 0, 5, 0, 'a'})); err == nil {
		t.Fatal("半截的帧应该报错")
	}
}

func TestHelloPayload(t *testing.T) {
	p, err := helloPayload(1, "0123456789abcdef", "MineDeck 测试")
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(p) != 1 || p[2] != 16 || string(p[3:19]) != "0123456789abcdef" || string(p[19:]) != "MineDeck 测试" {
		t.Fatalf("HELLO 不对：% x", p)
	}
	if _, err := helloPayload(1, strings.Repeat("x", 256), ""); err == nil {
		t.Fatal("令牌太长应该报错")
	}
}

func TestReadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companion.properties")
	body := "\xEF\xBB\xBF# 注释\n! 也是注释\nenabled = true\nport: 25611\n  token=abcdefabcdefabcdef  \nnodig.base=minecraft:overworld 0 0 0 1 1 1\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	port, token, enabled, err := readConfig(path)
	if err != nil || port != 25611 || token != "abcdefabcdefabcdef" || !enabled {
		t.Fatalf("读出：%d %q %v %v", port, token, enabled, err)
	}
	if err := os.WriteFile(path, []byte("port=abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readConfig(path); err == nil {
		t.Fatal("port 不是数字应该报错")
	}
	if _, _, _, err := readConfig(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("文件不存在应该报错")
	}
}

// fakeMod 照协议 v1 的模组那一侧：握手（检查版本和令牌）、回 WELCOME，然后把每条 REQUEST 交给 handle。
type fakeMod struct {
	ln       net.Listener
	token    string
	busyOnce atomic.Bool // 第一次连接回 ERROR 3
	reject   uint16      // 非 0 时每次都回这个 ERROR
	handle   func(c net.Conn, req map[string]any)
	conns    atomic.Int32
}

func startFake(t *testing.T, token string) *fakeMod {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeMod{ln: ln, token: token}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go m.serve(t, c)
		}
	}()
	return m
}

func (m *fakeMod) addr() string { return m.ln.Addr().String() }

func (m *fakeMod) serve(t *testing.T, c net.Conn) {
	defer c.Close()
	m.conns.Add(1)
	f, err := readFrame(c)
	if err != nil || f.typ != typeHello || len(f.payload) < 3 {
		return
	}
	version := binary.LittleEndian.Uint16(f.payload)
	n := int(f.payload[2])
	tok := string(f.payload[3 : 3+n])
	errFrame := func(code uint16, msg string) {
		p := make([]byte, 2, 2+len(msg))
		binary.LittleEndian.PutUint16(p, code)
		_ = writeFrame(c, typeError, append(p, msg...))
	}
	switch {
	case m.reject != 0:
		errFrame(m.reject, "测试拒绝")
		return
	case version == 0:
		errFrame(errVersion, "版本不支持")
		return
	case tok != m.token:
		errFrame(errBadToken, "令牌不对")
		return
	case m.busyOnce.CompareAndSwap(true, false):
		errFrame(errBusy, "已经有一个控制连接")
		return
	}
	welcome := []byte{1, 0}
	welcome = append(welcome, `{"protocol":1,"session":"abcd1234","ops":["spawn"],"companions":[]}`...)
	if writeFrame(c, typeWelcome, welcome) != nil {
		return
	}
	for {
		f, err := readFrame(c)
		if err != nil {
			return
		}
		switch f.typ {
		case typePing:
			_ = writeFrame(c, typePong, f.payload)
		case typeRequest:
			var req map[string]any
			if json.Unmarshal(f.payload, &req) != nil {
				_ = writeFrame(c, typeResponse, []byte(`{"id":null,"ok":false,"code":"bad_json","error":"不是 JSON"}`))
				continue
			}
			if m.handle != nil {
				m.handle(c, req)
			}
		case typeEnd:
			return
		}
	}
}

func reply(c net.Conn, v map[string]any) {
	b, _ := json.Marshal(v)
	_ = writeFrame(c, typeResponse, b)
}

func event(c net.Conn, v map[string]any) {
	b, _ := json.Marshal(v)
	_ = writeFrame(c, typeEvent, b)
}

// 握手、指令、回复、事件；中间夹一个不认识的帧，客户端照协议忽略。
func TestDialCallEvent(t *testing.T) {
	m := startFake(t, "0123456789abcdef")
	m.handle = func(c net.Conn, req map[string]any) {
		_ = writeFrame(c, 0x77, []byte("以后的新帧"))
		event(c, map[string]any{"event": "spawned", "companion": 1})
		reply(c, map[string]any{"id": req["id"], "ok": true, "companion": 1})
	}
	var out bytes.Buffer
	cl, err := dial(m.addr(), "0123456789abcdef", "test", &out, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.close("测试结束")
	if cl.version != 1 || cl.welcome["session"] != "abcd1234" {
		t.Fatalf("WELCOME：v%d %v", cl.version, cl.welcome)
	}
	res, err := cl.call(map[string]any{"op": "spawn"}, 5*time.Second)
	if err != nil || res["ok"] != true || res["companion"] != float64(1) {
		t.Fatalf("回复：%v %v", res, err)
	}
	ev, err := cl.waitEvent(func(e map[string]any) bool { return e["event"] == "spawned" }, 5*time.Second)
	if err != nil || ev["companion"] != float64(1) {
		t.Fatalf("事件：%v %v", ev, err)
	}
	if !strings.Contains(out.String(), "不认识的帧 0x77") {
		t.Fatalf("没有提示不认识的帧：%s", out.String())
	}
}

// ERROR 3（上一个连接还没走）重试；ERROR 2 不重试。
func TestConnectRetry(t *testing.T) {
	m := startFake(t, "0123456789abcdef")
	m.busyOnce.Store(true)
	cl, err := connect("", m.addr(), "0123456789abcdef", "test", io.Discard, false, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cl.close("测试结束")
	if m.conns.Load() != 2 {
		t.Fatalf("应该连了 2 次，实际 %d 次", m.conns.Load())
	}

	m2 := startFake(t, "0123456789abcdef")
	m2.reject = errVersion
	start := time.Now()
	_, err = connect("", m2.addr(), "0123456789abcdef", "test", io.Discard, false, 5*time.Second)
	var re *rejectError
	if err == nil || !asReject(err, &re) || re.code != errVersion || time.Since(start) > 2*time.Second {
		t.Fatalf("ERROR 2 应该马上失败：%v", err)
	}

	// 令牌不对：在耐心范围内重试（模组可能还没读到新配置），之后失败
	m3 := startFake(t, "the-right-token-123")
	start = time.Now()
	_, err = connect("", m3.addr(), "the-wrong-token-123", "test", io.Discard, false, 1200*time.Millisecond)
	if err == nil || !asReject(err, &re) || re.code != errBadToken || time.Since(start) < time.Second {
		t.Fatalf("令牌不对应该重试一会再失败：%v（%v）", err, time.Since(start))
	}
}

func asReject(err error, re **rejectError) bool {
	r, ok := err.(*rejectError)
	if ok {
		*re = r
	}
	return ok
}

// send -wait：等到 task_finished，按 result 决定退出码；-expect：错误码对上就是 0；-raw：原样发出去。
func TestRunSend(t *testing.T) {
	m := startFake(t, "0123456789abcdef")
	m.handle = func(c net.Conn, req map[string]any) {
		switch req["op"] {
		case "collect":
			task := map[string]any{"id": 7, "companion": 1, "op": "collect"}
			event(c, map[string]any{"event": "task_started", "companion": 1, "task": task})
			reply(c, map[string]any{"id": req["id"], "ok": true, "task": task})
			go func() {
				time.Sleep(100 * time.Millisecond)
				event(c, map[string]any{"event": "task_finished", "companion": 1, "task": map[string]any{"id": 6}, "result": "failed"})
				event(c, map[string]any{"event": "task_finished", "companion": 1, "task": task, "result": req["want"]})
			}()
		default:
			reply(c, map[string]any{"id": req["id"], "ok": false, "code": "bad_args", "error": "参数不对"})
		}
	}
	base := []string{"-addr", m.addr(), "-token", "0123456789abcdef", "send"}
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"-wait", `{"op":"collect","want":"done"}`}, 0},
		{[]string{"-wait", `{"op":"collect","want":"incomplete"}`}, 4},
		{[]string{"-wait", "-timeout", "50ms", `{"op":"collect","want":"done"}`}, 5},
		{[]string{`{"op":"oops"}`}, 4},
		{[]string{"-expect", "bad_args", `{"op":"oops"}`}, 0},
		{[]string{"-expect", "no_player", `{"op":"oops"}`}, 4},
		{[]string{"-raw", "-expect", "bad_json", `not json`}, 0},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		if got := run(append(append([]string{}, base...), c.args...), strings.NewReader(""), &out, &errOut); got != c.want {
			t.Errorf("%v：退出码 %d，想要 %d\n%s%s", c.args, got, c.want, out.String(), errOut.String())
		}
	}
	// 连不上的端口：本地错误
	var errOut bytes.Buffer
	if got := run([]string{"-addr", "127.0.0.1:1", "-token", "x", "-patience", "0", "welcome"}, nil, io.Discard, &errOut); got != 1 {
		t.Errorf("连不上时退出码 %d", got)
	}
}
