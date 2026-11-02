package bridge

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMod 假的桥接模组：在 127.0.0.1 上监听，按协议第 1 版应答。行为可以在测试中途改。
type fakeMod struct {
	t     *testing.T
	ln    net.Listener
	token atomic.Value // string

	reject   atomic.Int32 // 非 0：用这个错误码拒绝新连接
	silent   atomic.Bool  // 握手后什么都不回（不回 PONG、不发 STATUS），用来测心跳超时
	noRead   atomic.Bool  // 握手后不读数据，用来测写超时
	status   atomic.Bool  // 每 50 毫秒发一次 STATUS
	pongWait atomic.Int64 // 收到 PING 后等这么多纳秒再回 PONG（Windows 的时钟一跳 0.5～15.6 毫秒，本机往返太快会量成 0）
	accepted atomic.Int32

	mu     sync.Mutex
	conns  []net.Conn
	hellos []string // 收到的广播者名字
	pcm    []pcmRec
	pings  int
	ends   []string
}

type pcmRec struct {
	conn int
	seq  uint32
	s0   int16
}

func newFakeMod(t *testing.T, token string) *fakeMod {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeMod{t: t, ln: ln}
	m.token.Store(token)
	go m.serve()
	t.Cleanup(func() { m.close() })
	return m
}

func (m *fakeMod) port() int { return m.ln.Addr().(*net.TCPAddr).Port }

func (m *fakeMod) close() {
	m.ln.Close()
	m.kick()
}

// kick 断开所有连接（模拟模组崩溃或服务器重启语音服务）。
func (m *fakeMod) kick() {
	m.mu.Lock()
	for _, c := range m.conns {
		c.Close()
	}
	m.conns = nil
	m.mu.Unlock()
}

func (m *fakeMod) serve() {
	for {
		c, err := m.ln.Accept()
		if err != nil {
			return
		}
		id := int(m.accepted.Add(1))
		m.mu.Lock()
		m.conns = append(m.conns, c)
		m.mu.Unlock()
		go m.handle(c, id)
	}
}

func (m *fakeMod) handle(c net.Conn, id int) {
	defer c.Close()
	br := bufio.NewReader(c)
	f, err := ReadFrame(br)
	if err != nil || f.Type != TypeHello || len(f.Payload) < 3 {
		return
	}
	tl := int(f.Payload[2])
	tok, name := string(f.Payload[3:3+tl]), string(f.Payload[3+tl:])
	m.mu.Lock()
	m.hellos = append(m.hellos, name)
	m.mu.Unlock()
	send := func(typ byte, p []byte) {
		m.mu.Lock() // 写和测试里的 kick 不打架就行
		defer m.mu.Unlock()
		_ = WriteFrame(c, typ, p)
	}
	if code := m.reject.Load(); code != 0 {
		send(TypeError, append([]byte{byte(code), 0}, ErrorName(int(code))...))
		return
	}
	if tok != m.token.Load().(string) {
		send(TypeError, append([]byte{ErrBadToken, 0}, "令牌不对"...))
		return
	}
	w := make([]byte, 11)
	binary.LittleEndian.PutUint16(w[0:], ProtoVersion)
	binary.LittleEndian.PutUint32(w[2:], SampleRate)
	binary.LittleEndian.PutUint16(w[6:], FrameSamples)
	w[8] = 1
	binary.LittleEndian.PutUint16(w[9:], 2)
	send(TypeWelcome, append(w, "0.1.0-fake"...))
	if m.noRead.Load() { // 只发 STATUS、从不读：MineDeck 那边的写迟早会写满、卡住
		for i := 0; i < 1200; i++ {
			p := make([]byte, 25)
			send(TypeStatus, p)
			time.Sleep(50 * time.Millisecond)
		}
		return
	}
	stop := make(chan struct{})
	defer close(stop)
	if m.status.Load() && !m.silent.Load() {
		go func() {
			tk := time.NewTicker(50 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tk.C:
					p := make([]byte, 25)
					binary.LittleEndian.PutUint16(p[0:], 2)
					p[4] = SourceBroadcast
					m.mu.Lock()
					binary.LittleEndian.PutUint32(p[5:], uint32(len(m.pcm)))
					m.mu.Unlock()
					send(TypeStatus, p)
				}
			}
		}()
	}
	for {
		f, err := ReadFrame(br)
		if err != nil {
			return
		}
		switch f.Type {
		case TypePCM:
			m.mu.Lock()
			m.pcm = append(m.pcm, pcmRec{conn: id, seq: binary.LittleEndian.Uint32(f.Payload), s0: int16(binary.LittleEndian.Uint16(f.Payload[4:]))})
			m.mu.Unlock()
		case TypePing:
			m.mu.Lock()
			m.pings++
			m.mu.Unlock()
			switch d := time.Duration(m.pongWait.Load()); {
			case m.silent.Load():
			case d > 0:
				time.AfterFunc(d, func() { send(TypePong, f.Payload) })
			default:
				send(TypePong, f.Payload)
			}
		case TypeEnd:
			m.mu.Lock()
			m.ends = append(m.ends, string(f.Payload))
			m.mu.Unlock()
			return
		}
	}
}

func (m *fakeMod) snapshot() (hellos []string, pcm []pcmRec, pings int, ends []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.hellos...), append([]pcmRec(nil), m.pcm...), m.pings, append([]string(nil), m.ends...)
}

// instDir 一个实例目录，配置指向 port、用 token。
func instDir(t *testing.T, port int, token string) string {
	t.Helper()
	dir := t.TempDir()
	writeCfg(t, dir, port, token)
	return dir
}

func writeCfg(t *testing.T, dir string, port int, token string) {
	t.Helper()
	body := fmt.Sprintf("enabled=true\nport=%d\ntoken=%s\nexclude=\n", port, token)
	if err := WriteFileAtomic(ConfigPath(dir), []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("等了 %v：%s", d, what)
	}
}

const tok = "0123456789abcdef0123456789abcdef"

func fastOpts(dir string) Options {
	return Options{Dir: dir, Name: "房主", RetryDelay: 50 * time.Millisecond, Grace: time.Second, BusyWait: 600 * time.Millisecond,
		pingEvery: 40 * time.Millisecond, readIdle: 400 * time.Millisecond, writeIdle: 300 * time.Millisecond}
}

func frame(v int16) []int16 {
	s := make([]int16, FrameSamples)
	s[0] = v
	return s
}

// 握手、PCM 帧（序号从 0 起、采样原样）、心跳（PING / PONG 往返时间）、STATUS；停止时发 END 带原因。
func TestClientHandshakeFramesHeartbeat(t *testing.T) {
	m := newFakeMod(t, tok)
	m.status.Store(true)
	m.pongWait.Store(int64(20 * time.Millisecond))
	c := Start(fastOpts(instDir(t, m.port(), tok)))
	waitFor(t, "连上", 3*time.Second, func() bool { return c.State().Phase == PhaseLive })
	st := c.State()
	if st.ModVersion != "0.1.0-fake" || st.Connects != 1 || st.Status == nil || st.Status.Listeners != 2 || st.Addr != fmt.Sprintf("127.0.0.1:%d", m.port()) {
		t.Fatalf("握手后的状态：%+v", st)
	}
	for i := range 5 {
		if !c.Send(frame(int16(100 + i))) {
			t.Fatalf("第 %d 帧没排上队", i)
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, "收到 5 帧", 2*time.Second, func() bool { _, pcm, _, _ := m.snapshot(); return len(pcm) == 5 })
	waitFor(t, "心跳和 STATUS", 2*time.Second, func() bool {
		_, _, pings, _ := m.snapshot()
		s := c.State()
		return pings >= 2 && s.RTTMs >= 10 && s.Status != nil && s.Status.Received == 5 && s.Status.Source == SourceBroadcast
	})
	hellos, pcm, _, _ := m.snapshot()
	if len(hellos) != 1 || hellos[0] != "房主" {
		t.Errorf("HELLO 里的名字：%v", hellos)
	}
	for i, r := range pcm {
		if r.seq != uint32(i) || r.s0 != int16(100+i) {
			t.Errorf("第 %d 帧：序号 %d、第一个采样 %d", i, r.seq, r.s0)
		}
	}
	c.Stop("用户停止")
	select {
	case <-c.Done():
	default:
		t.Fatal("Stop 返回后 Done 应已关闭")
	}
	if c.Err() != nil || c.State().Phase != PhaseStopped || c.State().Sent != 5 {
		t.Fatalf("停止后：%v %+v", c.Err(), c.State())
	}
	waitFor(t, "模组收到 END", time.Second, func() bool { _, _, _, ends := m.snapshot(); return len(ends) == 1 && ends[0] == "用户停止" })
	c.Stop("再停一次") // 可以重复调用
	if c.Send(frame(1)) {
		t.Fatal("停止后不该再排队")
	}
}

// 断线重连：模组断开（崩溃、语音服务重启）后自动重连，序号从 0 重新开始；心跳超时（模组不再回任何东西）也会重连。
func TestClientReconnects(t *testing.T) {
	m := newFakeMod(t, tok)
	c := Start(fastOpts(instDir(t, m.port(), tok)))
	defer c.Stop("测试结束")
	waitFor(t, "连上", 3*time.Second, func() bool { return c.State().Phase == PhaseLive })
	c.Send(frame(1))
	waitFor(t, "第一条连接收到一帧", time.Second, func() bool { _, pcm, _, _ := m.snapshot(); return len(pcm) == 1 })
	m.kick()
	waitFor(t, "断线后重连", 3*time.Second, func() bool { s := c.State(); return s.Connects == 2 && s.Phase == PhaseLive })
	if s := c.State(); s.LastError == "" {
		t.Errorf("应记下断线原因：%+v", s)
	}
	c.Send(frame(2))
	waitFor(t, "第二条连接收到一帧", time.Second, func() bool { _, pcm, _, _ := m.snapshot(); return len(pcm) == 2 })
	if _, pcm, _, _ := m.snapshot(); pcm[1].conn != 2 || pcm[1].seq != 0 {
		t.Errorf("重连后序号应从 0 开始：%+v", pcm[1])
	}

	m.silent.Store(true) // 下一条连接：握手后什么都不回
	m.kick()
	waitFor(t, "第三条连接", 3*time.Second, func() bool { return c.State().Connects == 3 })
	waitFor(t, "心跳超时后重连", 3*time.Second, func() bool { return c.State().Connects >= 4 })
	if s := c.State(); !strings.Contains(s.LastError, "心跳超时") && s.Phase != PhaseLive {
		t.Errorf("断线原因：%+v", s)
	}
	m.silent.Store(false)
	if c.Err() != nil {
		t.Fatalf("连上过以后断线不该放弃：%v", c.Err())
	}
}

// 令牌不对：先在宽限期里重试（模组可能还没读到新配置），期间配置里的令牌改对了就连上（每次连接前重读配置）；
// 一直不对就放弃，Err 是 ModError（错误码 1）。
func TestClientBadToken(t *testing.T) {
	m := newFakeMod(t, tok)
	dir := instDir(t, m.port(), "wrong-token-000000000000")
	c := Start(fastOpts(dir))
	waitFor(t, "被拒几次", 2*time.Second, func() bool { h, _, _, _ := m.snapshot(); return len(h) >= 2 })
	if s := c.State(); s.Phase != PhaseConnecting || !strings.Contains(s.LastError, "令牌不对") {
		t.Fatalf("宽限期内：%+v", s)
	}
	writeCfg(t, dir, m.port(), tok) // 配置改对了（比如模组刚生成了新令牌，MineDeck 重新读到）
	waitFor(t, "改对令牌后连上", 3*time.Second, func() bool { return c.State().Phase == PhaseLive })
	c.Stop("测试结束")

	c2 := Start(fastOpts(instDir(t, m.port(), "wrong-token-000000000000")))
	select {
	case <-c2.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("一直令牌不对应在宽限期后放弃")
	}
	var me *ModError
	if !errors.As(c2.Err(), &me) || me.Code != ErrBadToken || c2.State().Phase != PhaseStopped {
		t.Fatalf("放弃的原因：%v %+v", c2.Err(), c2.State())
	}
}

// 忙（已经有别的广播连接）：等一会儿；旧连接结束了就连上；一直忙就放弃，说明原因。协议版本不对立刻放弃。
func TestClientBusyAndVersion(t *testing.T) {
	m := newFakeMod(t, tok)
	m.reject.Store(ErrBusy)
	c := Start(fastOpts(instDir(t, m.port(), tok)))
	waitFor(t, "被拒为忙", 2*time.Second, func() bool { return c.State().Busy })
	m.reject.Store(0)
	waitFor(t, "不忙了就连上", 3*time.Second, func() bool { return c.State().Phase == PhaseLive })
	if c.State().Busy {
		t.Error("连上后 Busy 应清掉")
	}
	c.Stop("测试结束")

	m.reject.Store(ErrBusy)
	c2 := Start(fastOpts(instDir(t, m.port(), tok)))
	select {
	case <-c2.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("一直忙应放弃")
	}
	if err := c2.Err(); err == nil || !strings.Contains(err.Error(), "已经有别的广播连接") {
		t.Fatalf("一直忙：%v", err)
	}

	m.reject.Store(ErrVersion)
	start := time.Now()
	c3 := Start(fastOpts(instDir(t, m.port(), tok)))
	<-c3.Done()
	var me *ModError
	if !errors.As(c3.Err(), &me) || me.Code != ErrVersion || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("协议版本不对应立刻放弃：%v（%v）", c3.Err(), time.Since(start))
	}
}

// 还没连上过：连不上的话宽限期后放弃（模组没加载或端口没开）；NotBefore 之前不连接；
// 端口在配置里改了，下次连接就连新端口；没有配置文件也算在宽限期里。
func TestClientConnectRules(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	deadPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	c := Start(fastOpts(instDir(t, deadPort, tok)))
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("一直连不上应放弃")
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "连不上桥接模组") || !strings.Contains(err.Error(), "可能没有加载") {
		t.Fatalf("连不上：%v", err)
	}

	m := newFakeMod(t, tok)
	o := fastOpts(instDir(t, m.port(), tok))
	o.NotBefore = time.Now().Add(300 * time.Millisecond)
	c2 := Start(o)
	time.Sleep(150 * time.Millisecond)
	if m.accepted.Load() != 0 {
		t.Fatal("NotBefore 之前不该连接")
	}
	waitFor(t, "NotBefore 之后连上", 3*time.Second, func() bool { return c2.State().Phase == PhaseLive })
	// 配置里换了端口（模组那边也换了）：断线后按新端口连
	m2 := newFakeMod(t, tok)
	writeCfg(t, o.Dir, m2.port(), tok)
	m.close()
	waitFor(t, "按新端口重连", 3*time.Second, func() bool { return m2.accepted.Load() == 1 && c2.State().Phase == PhaseLive })
	c2.Stop("测试结束")

	dir := t.TempDir() // 没有配置文件
	c3 := Start(fastOpts(dir))
	time.Sleep(200 * time.Millisecond)
	if s := c3.State(); s.Phase != PhaseConnecting || !strings.Contains(s.LastError, "没有桥接模组的配置文件") {
		t.Fatalf("没有配置文件：%+v", s)
	}
	writeCfg(t, dir, m2.port(), tok)
	waitFor(t, "配置出现后连上", 3*time.Second, func() bool { return c3.State().Phase == PhaseLive })
	c3.Stop("测试结束")
	if err := os.Remove(ConfigPath(dir)); err != nil {
		t.Fatal(err)
	}
}

// Send 从不阻塞：没连上时直接丢；连上了但模组不读数据（socket 写满）时，排队的帧满了丢最旧的，
// 写超时后断开重连，调用方一直不受影响。
func TestClientSendNeverBlocks(t *testing.T) {
	m := newFakeMod(t, tok)
	m.noRead.Store(true)
	o := fastOpts(instDir(t, m.port(), tok))
	o.NotBefore = time.Now().Add(time.Hour)
	c := Start(o)
	if c.Send(frame(1)) || c.State().Dropped != 1 {
		t.Fatalf("没连上时应直接丢：%+v", c.State())
	}
	c.Stop("测试结束")

	c2 := Start(fastOpts(instDir(t, m.port(), tok)))
	defer c2.Stop("测试结束")
	waitFor(t, "连上", 3*time.Second, func() bool { return c2.State().Phase == PhaseLive })
	var worst time.Duration
	deadline := time.Now().Add(8 * time.Second)
	for c2.State().Connects < 2 && time.Now().Before(deadline) {
		t0 := time.Now()
		c2.Send(frame(1))
		worst = max(worst, time.Since(t0))
		time.Sleep(200 * time.Microsecond)
	}
	if worst > 50*time.Millisecond {
		t.Errorf("Send 最慢一次用了 %v，有阻塞", worst)
	}
	s := c2.State()
	if s.Connects < 2 || s.Dropped == 0 || !strings.Contains(s.LastError, "发送失败") {
		t.Fatalf("写不出去时应丢帧、写超时后断开重连：%+v", s)
	}
}

// 模组主动 END（关服时）：记下原因后接着重连；关服后模组那边连不上，由调用方决定停。
func TestClientEndFromMod(t *testing.T) {
	m := newFakeMod(t, tok)
	c := Start(fastOpts(instDir(t, m.port(), tok)))
	defer c.Stop("测试结束")
	waitFor(t, "连上", 3*time.Second, func() bool { return c.State().Phase == PhaseLive })
	m.mu.Lock()
	for _, conn := range m.conns {
		_ = WriteFrame(conn, TypeEnd, []byte("语音服务器已停止"))
	}
	m.mu.Unlock()
	waitFor(t, "收到 END 后进入重连", 2*time.Second, func() bool {
		s := c.State()
		return strings.Contains(s.LastError, "语音服务器已停止")
	})
	waitFor(t, "重新连上", 3*time.Second, func() bool { return c.State().Connects == 2 })
}
