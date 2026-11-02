package companion_test

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/companion"
	"minedeck/internal/companion/companiontest"
)

const token = "0123456789abcdef0123456789abcdef"

// writeCfg 写一份 companion.properties：打开端口、这个端口和令牌。
func writeCfg(t *testing.T, dir string, port int, tok string) {
	t.Helper()
	path := companion.ConfigPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("# 测试\nenabled=true\nport=%d\ntoken=%s\n", port, tok)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func startMod(t *testing.T, o companiontest.Options) *companiontest.FakeMod {
	t.Helper()
	if o.Token == "" {
		o.Token = token
	}
	if o.TokenDelay == 0 {
		o.TokenDelay = 5 * time.Millisecond
	}
	m, err := companiontest.Start(0, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

// recorder 记下客户端的回调，按发生的顺序。
type recorder struct {
	mu       sync.Mutex
	order    []string
	events   []companion.Event
	welcomes []companion.Welcome
	downs    []string
	statuses int
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	r.order = append(r.order, s)
	r.mu.Unlock()
}

func (r *recorder) snapshot() ([]string, []companion.Event, []companion.Welcome, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...), append([]companion.Event(nil), r.events...), append([]companion.Welcome(nil), r.welcomes...), append([]string(nil), r.downs...)
}

func (r *recorder) options(dir string) companion.Options {
	return companion.Options{Dir: dir, Name: "MineDeck 测试",
		OnWelcome: func(w companion.Welcome) {
			r.mu.Lock()
			r.welcomes = append(r.welcomes, w)
			r.order = append(r.order, "welcome")
			r.mu.Unlock()
		},
		OnEvent: func(e companion.Event) {
			r.mu.Lock()
			r.events = append(r.events, e)
			r.order = append(r.order, "event:"+e.Event)
			r.mu.Unlock()
		},
		OnStatus: func(companion.Status) {
			r.mu.Lock()
			r.statuses++
			r.mu.Unlock()
		},
		OnDown: func(err error) {
			r.mu.Lock()
			r.downs = append(r.downs, err.Error())
			r.order = append(r.order, "down")
			r.mu.Unlock()
		},
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("等了 5 秒还没有：%s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// 握手：HELLO 里是 MineDeck 支持的最高版本、配置里的令牌和客户端名字；WELCOME 解出来；Stop 时给模组发 END 带上原因。
// WELCOME 里多出来的字段不影响（兼容规则 1）。
func TestHandshakeAndStop(t *testing.T) {
	m := startMod(t, companiontest.Options{Welcome: func(w map[string]any) {
		w["newField"] = map[string]any{"x": 1}
		w["limits"].(map[string]any)["maxSomething"] = 9
	}})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	rec := &recorder{}
	c := companion.Start(companion.WithOptions(rec.options(dir), companion.FastRetry()))
	waitFor(t, "连上", c.Live)
	h := m.Hellos()
	if len(h) != 1 || h[0].Version != companion.ProtoVersion || h[0].Token != token || h[0].Client != "MineDeck 测试" {
		t.Fatalf("HELLO：%+v", h)
	}
	_, _, ws, _ := rec.snapshot()
	if len(ws) != 1 || ws[0].Session != "s1" || ws[0].Mod != "0.2.0" || ws[0].Protocol != 1 || ws[0].Limits.MaxText != 256 ||
		ws[0].Config.MaxCompanions != 4 || len(ws[0].Ops) != 10 || ws[0].PlayerEngine != "1.21.1-1.4.0" {
		t.Fatalf("WELCOME：%+v", ws)
	}
	if st := c.State(); st.Welcome == nil || st.Addr != fmt.Sprintf("127.0.0.1:%d", m.Port()) || st.Connects != 1 {
		t.Errorf("状态：%+v", st)
	}
	c.Stop("MineDeck 已退出")
	waitFor(t, "模组收到 END", func() bool { return len(m.Ends()) == 1 })
	if e := m.Ends(); e[0] != "MineDeck 已退出" {
		t.Errorf("END 的原因：%q", e)
	}
	if st := c.State(); st.Phase != companion.PhaseStopped || c.Err() != nil {
		t.Errorf("Stop 以后：%+v %v", st, c.Err())
	}
	if _, _, _, downs := rec.snapshot(); len(downs) != 0 {
		t.Errorf("自己 Stop 的不算断线：%v", downs)
	}
}

// rawMod 一个只握手的假模组：收到 HELLO 后按 reply 回一帧，记下之后收到的帧。
func rawMod(t *testing.T, reply func(hello []byte) (byte, []byte)) (port int, frames func() []companion.Frame) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var got []companion.Frame
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				f, err := companion.ReadFrame(br)
				if err != nil {
					return
				}
				typ, p := reply(f.Payload)
				_ = companion.WriteFrame(c, typ, p)
				for {
					f, err := companion.ReadFrame(br)
					if err != nil {
						return
					}
					mu.Lock()
					got = append(got, f)
					mu.Unlock()
				}
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, func() []companion.Frame {
		mu.Lock()
		defer mu.Unlock()
		return append([]companion.Frame(nil), got...)
	}
}

// 版本协商：模组用 min(v, M)；选了 MineDeck 不支持的版本时，MineDeck 发 END 断开、不再重试，提示更新；
// 模组回 ERROR 2（版本不支持）、ERROR 4（协议错误）同样不再重试。
func TestVersionNegotiation(t *testing.T) {
	// 模组支持到 3：用 min(1, 3) = 1
	m := startMod(t, companiontest.Options{MaxVersion: 3})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	c := companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
	waitFor(t, "连上", c.Live)
	if w := c.State().Welcome; w == nil || w.Protocol != 1 {
		t.Errorf("应用协议 1：%+v", w)
	}
	c.Stop("测完了")

	// 模组不守规矩，选了 2
	port, frames := rawMod(t, func([]byte) (byte, []byte) {
		p := make([]byte, 2)
		binary.LittleEndian.PutUint16(p, 2)
		return companion.TypeWelcome, append(p, []byte(`{"protocol":2}`)...)
	})
	writeCfg(t, dir, port, token)
	c = companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("协议版本对不上时应该停下")
	}
	if err := c.Err(); !companion.IsFatal(err) || !strings.Contains(err.Error(), "协议 2") || !strings.Contains(err.Error(), "更新") {
		t.Errorf("错误：%v", err)
	}
	waitFor(t, "模组收到 END", func() bool { return len(frames()) > 0 })
	if f := frames()[0]; f.Type != companion.TypeEnd || !strings.Contains(string(f.Payload), "只支持协议") {
		t.Errorf("应发 END 说明原因：%+v %q", f, f.Payload)
	}

	for _, code := range []int{companion.ErrVersion, companion.ErrProtocol} {
		m2 := startMod(t, companiontest.Options{})
		m2.SetReject(code)
		writeCfg(t, dir, m2.Port(), token)
		c := companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
		select {
		case <-c.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("ERROR %d 时应该停下", code)
		}
		var me *companion.ModError
		if err := c.Err(); !errors.As(err, &me) || me.Code != code || !companion.IsFatal(err) {
			t.Errorf("ERROR %d：%v", code, err)
		}
		if n := len(m2.Hellos()); n != 1 {
			t.Errorf("ERROR %d 不该重试：HELLO %d 次", code, n)
		}
	}
}

// 令牌不对：配置刚改过的 10 秒内（这里缩短了）当作「模组还没读到」，很快地重试；过了这段时间慢下来。
// 改对了配置、Nudge 一下，马上连上。
func TestBadTokenRetry(t *testing.T) {
	m := startMod(t, companiontest.Options{})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), strings.Repeat("x", 32))
	c := companion.Start(companion.WithOptions(companion.Options{Dir: dir}, func(o *companion.Options) {
		companion.FastRetry()(o)
		companion.SetRetry(o, 20*time.Millisecond, 300*time.Millisecond, time.Hour, time.Hour)
	}))
	defer c.Stop("测完了")
	waitFor(t, "快速重试几次", func() bool { return len(m.Hellos()) >= 3 })
	time.Sleep(400 * time.Millisecond) // 过了快速重试的时间：之后一小时才再试
	n := len(m.Hellos())
	time.Sleep(200 * time.Millisecond)
	if len(m.Hellos()) != n {
		t.Errorf("过了快速重试的时间还在快速重试：%d → %d", n, len(m.Hellos()))
	}
	if st := c.State(); st.Phase != companion.PhaseConnecting || !strings.Contains(st.LastError, "令牌不对") {
		t.Errorf("状态：%+v", st)
	}
	writeCfg(t, dir, m.Port(), token) // MineDeck 每次连接前都重读配置
	c.Nudge()
	waitFor(t, "改对令牌后连上", c.Live)
}

// 已经有一个控制连接：第二个收到 ERROR 3，标明 busy，慢慢重试；第一个走了以后连上。
// 刚断开就重连遇到 ERROR 3（模组还没发现旧连接断了）时快速重试，不按 busy 的慢节奏。
func TestBusyAndReconnectRules(t *testing.T) {
	m := startMod(t, companiontest.Options{})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	a := companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
	waitFor(t, "A 连上", a.Live)
	b := companion.Start(companion.WithOptions(companion.Options{Dir: dir}, func(o *companion.Options) {
		companion.FastRetry()(o)
		companion.SetRetry(o, 20*time.Millisecond, 100*time.Millisecond, time.Hour, 50*time.Millisecond)
	}))
	defer b.Stop("测完了")
	waitFor(t, "B 被拒", func() bool { return b.State().Busy })
	if st := b.State(); st.Phase != companion.PhaseConnecting || !strings.Contains(st.LastError, "已经有一个控制连接") {
		t.Errorf("B 的状态：%+v", st)
	}
	a.Stop("让给 B")
	waitFor(t, "B 连上", b.Live)
	if b.State().Busy {
		t.Error("连上以后 busy 应清掉")
	}

	// 刚断开：模组还占着旧连接，回 ERROR 3；10 秒内（这里 300 毫秒）快速重试。busy、slow 都设成一小时，用了它们就连不回来
	rec := &recorder{}
	b.Stop("换一个")
	c := companion.Start(companion.WithOptions(rec.options(dir), func(o *companion.Options) {
		companion.FastRetry()(o)
		companion.SetRetry(o, 20*time.Millisecond, 300*time.Millisecond, time.Hour, time.Hour)
	}))
	defer c.Stop("测完了")
	waitFor(t, "C 连上", c.Live)
	time.Sleep(350 * time.Millisecond) // 让开服时的快速重试时间过去
	m.SetReject(companion.ErrBusy)
	m.Kick()
	waitFor(t, "C 发现断了", func() bool { _, _, _, d := rec.snapshot(); return len(d) == 1 })
	time.Sleep(100 * time.Millisecond)
	m.SetReject(0)
	waitFor(t, "C 很快重连", c.Live)
	if st := c.State(); st.Connects != 2 {
		t.Errorf("应握手 2 次：%+v", st)
	}

	// 过了快速重试的时间还是 ERROR 3：真的有别的程序占着，按 busy 的节奏（一小时）不再试
	time.Sleep(350 * time.Millisecond)
	m.SetReject(companion.ErrBusy)
	m.Kick()
	time.Sleep(400 * time.Millisecond)
	n := len(m.Hellos())
	m.SetReject(0)
	time.Sleep(200 * time.Millisecond)
	if c.Live() || len(m.Hellos()) != n {
		t.Errorf("快速重试的时间过了以后不该马上重试：live=%v HELLO %d → %d", c.Live(), n, len(m.Hellos()))
	}
}

// 心跳：MineDeck 每 2 秒（这里缩短了）发一次 PING；10 秒没收到模组的任何帧就断开重连；
// MineDeck 不发 PING 时模组发 END（心跳超时）断开，MineDeck 重连。
func TestHeartbeat(t *testing.T) {
	m := startMod(t, companiontest.Options{StatusEvery: 30 * time.Millisecond, IdleTimeout: 300 * time.Millisecond, PongDelay: 20 * time.Millisecond})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	rec := &recorder{}
	c := companion.Start(companion.WithOptions(rec.options(dir), func(o *companion.Options) {
		companion.FastRetry()(o)
		companion.SetHeartbeat(o, 40*time.Millisecond, 250*time.Millisecond)
	}))
	defer c.Stop("测完了")
	waitFor(t, "连上", c.Live)
	waitFor(t, "发 PING、收 STATUS", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return m.Pings() >= 3 && rec.statuses >= 3
	})
	if rtt := c.State().RTTMs; rtt < 10 {
		t.Errorf("应记下心跳的往返时间（模组 20 毫秒后才回 PONG）：%v 毫秒", rtt)
	}
	time.Sleep(400 * time.Millisecond) // 一直有 PING：模组不会断开
	if !c.Live() || m.Accepts() != 1 {
		t.Fatalf("有心跳时不该断开：live=%v accepts=%d", c.Live(), m.Accepts())
	}
	// 模组不说话了（不发 STATUS、不回 PONG）：MineDeck 250 毫秒后断开重连
	m.SetSilent(true)
	waitFor(t, "心跳超时", func() bool { _, _, _, d := rec.snapshot(); return len(d) == 1 })
	m.SetSilent(false)
	_, _, _, downs := rec.snapshot()
	if !strings.Contains(downs[0], "心跳超时") {
		t.Errorf("断开的原因：%v", downs)
	}
	waitFor(t, "重连", func() bool { return c.Live() && m.Accepts() == 2 })

	// MineDeck 不发 PING：模组 300 毫秒后发 END（心跳超时）
	rec2 := &recorder{}
	c.Stop("换一个不发 PING 的")
	c2 := companion.Start(companion.WithOptions(rec2.options(dir), func(o *companion.Options) {
		companion.FastRetry()(o)
		companion.SetHeartbeat(o, time.Hour, time.Hour)
	}))
	defer c2.Stop("测完了")
	waitFor(t, "模组发 END", func() bool { _, _, _, d := rec2.snapshot(); return len(d) == 1 })
	if _, _, _, d := rec2.snapshot(); !strings.Contains(d[0], "伙伴模组结束了连接：心跳超时") {
		t.Errorf("模组那边的心跳超时：%v", d)
	}
	waitFor(t, "又连上", c2.Live)
}

// 指令和回复：按 id 对应（几个协程同时发也不会串）；模组回 ok=false 时 Call 报 *OpError；
// 指令引起的事件先于它的回复交给 OnEvent；没连着时报 ErrNotConnected；回复太慢时超时。
func TestRequestsAndEventOrder(t *testing.T) {
	m := startMod(t, companiontest.Options{Handle: func(req map[string]any) ([]any, map[string]any) {
		switch req["op"] {
		case "echo":
			return nil, map[string]any{"ok": true, "echo": req["n"]}
		case "slow":
			time.Sleep(300 * time.Millisecond)
			return nil, map[string]any{"ok": true}
		}
		return nil, nil
	}})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	rec := &recorder{}
	c := companion.Start(companion.WithOptions(rec.options(dir), func(o *companion.Options) {
		companion.FastRetry()(o)
		companion.SetRequestTimeout(o, 150*time.Millisecond)
	}))
	defer c.Stop("测完了")
	if _, err := c.Request(context.Background(), "status", nil); !errors.Is(err, companion.ErrNotConnected) && !c.Live() {
		t.Errorf("没连着时：%v", err)
	}
	waitFor(t, "连上", c.Live)
	ctx := context.Background()

	var out struct {
		Companion int                  `json:"companion"`
		Info      *companion.Companion `json:"info"`
	}
	if _, err := c.Call(ctx, "spawn", map[string]any{"name": "小方", "owner": "Steve"}, &out); err != nil {
		t.Fatal(err)
	}
	rec.add("response:spawn")
	if out.Companion != 1 || out.Info == nil || out.Info.Name != "小方" || out.Info.Owner != "Steve" || out.Info.State != companion.StateIdle {
		t.Fatalf("spawn 的回复：%+v", out)
	}
	if _, err := c.Call(ctx, "spawn", map[string]any{"name": "小木"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2} {
		if _, err := c.Call(ctx, "collect", map[string]any{"companion": id, "item": "oak_log", "count": 3}, nil); err != nil {
			t.Fatal(err)
		}
	}
	rec.mu.Lock()
	rec.order = nil // 只看 stop 这一条的先后
	rec.mu.Unlock()
	var stopped struct {
		Stopped []int64 `json:"stopped"`
	}
	if _, err := c.Call(ctx, "stop", map[string]any{"all": true}, &stopped); err != nil {
		t.Fatal(err)
	}
	rec.add("response:stop")
	order, evs, _, _ := rec.snapshot()
	if strings.Join(order, ",") != "event:task_finished,event:task_finished,response:stop" || len(stopped.Stopped) != 2 {
		t.Errorf("事件应先于回复、按顺序：%v %v", order, stopped)
	}
	if e := evs[len(evs)-1]; e.Result != companion.ResultCancelled || e.Task == nil || e.Task.Op != "collect" ||
		string(e.Task.Args) == "" || e.Seconds != 3 {
		t.Errorf("task_finished：%+v", e)
	}
	spawned := false
	for _, e := range evs {
		if e.Event == companion.EvSpawned && e.Info != nil && e.Info.Name == "小方" {
			spawned = true
		}
	}
	if !spawned {
		t.Errorf("没收到 spawned：%+v", evs)
	}

	// 几个协程同时发：各自拿到自己的回复
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var r struct {
				Echo int `json:"echo"`
			}
			if _, err := c.Call(ctx, "echo", map[string]any{"n": i}, &r); err != nil || r.Echo != i {
				errs <- fmt.Errorf("第 %d 条：%v %d", i, err, r.Echo)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	ids := map[float64]bool{}
	for _, r := range m.Requests() {
		id, ok := r["id"].(float64)
		if !ok || ids[id] {
			t.Errorf("id 应是整数、不重复：%v", r)
		}
		ids[id] = true
	}

	// ok=false
	_, err := c.Call(ctx, "fly", nil, nil)
	var oe *companion.OpError
	if !errors.As(err, &oe) || oe.Code != companion.CodeUnknownOp || !strings.Contains(err.Error(), "不认识的指令") {
		t.Errorf("不认识的指令：%v", err)
	}
	if _, err := c.Call(ctx, "give", map[string]any{"companion": 1, "item": "oak_log", "player": "Nobody"}, nil); !errors.As(err, &oe) || oe.Code != companion.CodeNoPlayer {
		t.Errorf("玩家不在线：%v", err)
	}
	// 回复太慢：超时
	if _, err := c.Request(ctx, "slow", nil); err == nil || !strings.Contains(err.Error(), "没有回复") {
		t.Errorf("回复太慢应超时：%v", err)
	}
	waitFor(t, "慢的那条回完", func() bool {
		_, err := c.Call(ctx, "echo", map[string]any{"n": 7}, nil)
		return err == nil
	})
}

// 不认识的帧类型、事件名、字段一律忽略，不断开（兼容规则 1）。
func TestIgnoreUnknown(t *testing.T) {
	m := startMod(t, companiontest.Options{})
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	rec := &recorder{}
	c := companion.Start(companion.WithOptions(rec.options(dir), companion.FastRetry()))
	defer c.Stop("测完了")
	waitFor(t, "连上", c.Live)
	if err := m.SendFrame(0x7f, []byte("future")); err != nil {
		t.Fatal(err)
	}
	if err := m.SendEvent(map[string]any{"event": "teleported", "companion": 1, "where": "moon"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SendEvent(map[string]any{"event": "stuck", "companion": 1, "task": nil, "reason": "flying", "detail": "飞起来了",
		"pos": map[string]any{"dim": "minecraft:overworld", "x": 1, "y": 2, "z": 3}, "mood": "happy"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SendFrame(companion.TypeEvent, []byte("not json")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "收到两个事件", func() bool { _, e, _, _ := rec.snapshot(); return len(e) == 2 })
	_, evs, _, _ := rec.snapshot()
	if evs[0].Event != "teleported" || evs[1].Event != companion.EvStuck || evs[1].Reason != "flying" || evs[1].Task != nil || evs[1].Pos == nil {
		t.Errorf("事件：%+v", evs)
	}
	if _, err := c.Call(context.Background(), "status", nil, nil); err != nil || !c.Live() || m.Accepts() != 1 {
		t.Errorf("不认识的东西不该断开连接：%v live=%v accepts=%d", err, c.Live(), m.Accepts())
	}
}

// 模组发 END（关服、改成 enabled=false）：MineDeck 记下原因，重连；断线时还没回复的指令马上报错。
func TestEndFromModAndPendingRequests(t *testing.T) {
	block := make(chan struct{})
	m := startMod(t, companiontest.Options{Handle: func(req map[string]any) ([]any, map[string]any) {
		if req["op"] == "hang" {
			<-block
			return nil, map[string]any{"ok": true}
		}
		return nil, nil
	}})
	defer close(block)
	dir := t.TempDir()
	writeCfg(t, dir, m.Port(), token)
	rec := &recorder{}
	c := companion.Start(companion.WithOptions(rec.options(dir), companion.FastRetry()))
	defer c.Stop("测完了")
	waitFor(t, "连上", c.Live)
	errc := make(chan error, 1)
	go func() { _, err := c.Request(context.Background(), "hang", nil); errc <- err }()
	waitFor(t, "模组收到指令", func() bool { return len(m.Requests()) == 1 })
	m.Kick()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "可能执行了") {
			t.Errorf("断线时没回复的指令：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("断线后等着的指令应马上报错")
	}
	waitFor(t, "重连", func() bool { return c.Live() && m.Accepts() == 2 })
	m.SendEnd("控制端口正在关闭")
	waitFor(t, "收到 END", func() bool { _, _, _, d := rec.snapshot(); return len(d) == 2 })
	if _, _, _, d := rec.snapshot(); !strings.Contains(d[1], "伙伴模组结束了连接：控制端口正在关闭") {
		t.Errorf("END 的原因：%v", d)
	}
	waitFor(t, "再连上", func() bool { return c.Live() && m.Accepts() == 3 })
	order, _, ws, _ := rec.snapshot()
	if len(ws) != 3 || order[0] != "welcome" {
		t.Errorf("每次连上都有 WELCOME：%v", order)
	}
}

// 配置不能用（没打开、令牌太短、端口不对）时不去连，说清楚原因。
func TestConfigProblems(t *testing.T) {
	dir := t.TempDir()
	c := companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
	waitFor(t, "报原因", func() bool { return strings.Contains(c.State().LastError, "还没有伙伴模组的配置文件") })
	c.Stop("测完了")
	writeCfg(t, dir, 80, token)
	c = companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
	waitFor(t, "报原因", func() bool { return strings.Contains(c.State().LastError, "port 要是 1024 到 65535") })
	c.Stop("测完了")
	writeCfg(t, dir, 24480, "short")
	c = companion.Start(companion.WithOptions(companion.Options{Dir: dir}, companion.FastRetry()))
	waitFor(t, "报原因", func() bool {
		return strings.Contains(c.State().LastError, "token 要是 16–255 个可见的 ASCII 字符")
	})
	c.Stop("测完了")
	b, _ := json.Marshal(c.State())
	if strings.Contains(string(b), token) {
		t.Error("状态里不该有令牌")
	}
}
