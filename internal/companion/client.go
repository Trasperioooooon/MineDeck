package companion

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// 客户端的时间参数（README「连接」「握手和版本协商」两节）。
const (
	defaultPing        = 2 * time.Second        // MineDeck 每 2 秒发一次 PING（模组 10 秒没收到任何帧就断开）
	defaultReadIdle    = 10 * time.Second       // 10 秒没收到模组的任何帧就断开重连（模组每 2 秒发一次 STATUS）
	defaultQuick       = 500 * time.Millisecond // 模组还没读到新配置、刚断开就重连遇到 ERROR 3：隔 0.5 秒重试……
	defaultQuickWindow = 10 * time.Second       // ……最多 10 秒
	defaultSlow        = 3 * time.Second        // 过了这 10 秒：连不上、令牌不对、端口正在关闭，隔 3 秒再试（服务器开着就一直试）
	defaultBusy        = 10 * time.Second       // 别的程序占着控制连接：隔 10 秒再试
	defaultRequest     = 15 * time.Second       // 服务器卡的时候回复会晚到
	dialTimeout        = 3 * time.Second
	helloTimeout       = 6 * time.Second // 令牌不对时模组等 0.3 秒才回 ERROR 1
	writeTimeout       = 3 * time.Second
	endTimeout         = time.Second
	logEvery           = 30 // 一直重连时，每 30 次才记一条日志
)

// ErrNotConnected 现在没连着伙伴模组。
var ErrNotConnected = errors.New("伙伴模组还没连上")

// Options 客户端参数。
type Options struct {
	Dir  string // 实例目录：每次连接前都重读一遍配置文件里的端口和令牌（README：MineDeck 每次连接前都重读配置文件）
	Name string // HELLO 里的客户端名字，只写进模组日志

	// Settle 配置文件最近一次改动的时间：之后 10 秒内连不上、ERROR 1、ERROR 5 都算「模组还没读到」，隔 0.5 秒重试。
	// 零值从 Start 算起（服务器刚开好，模组刚打开端口）。
	Settle time.Time

	// 下面几个回调都在读连接的协程里按顺序同步调用：要很快，慢的事自己交给别的协程。
	// 一次连接里的顺序是 OnWelcome → 收到的 EVENT / STATUS（按到达的顺序；指令引起的事件先于它的 RESPONSE）→ OnDown。
	OnWelcome func(Welcome)
	OnEvent   func(Event)
	OnStatus  func(Status)
	OnDown    func(err error) // 连上过的这次连接断了（Stop 的不算）
	Logf      func(format string, args ...any)

	dial        func(ctx context.Context, addr string) (net.Conn, error) // 测试用
	pingEvery   time.Duration                                            // 测试用
	readIdle    time.Duration
	quick       time.Duration
	quickWindow time.Duration
	slow        time.Duration
	busy        time.Duration
	request     time.Duration
}

// Phase 客户端所处的阶段。
type Phase string

const (
	PhaseConnecting Phase = "connecting" // 还没连上过
	PhaseLive       Phase = "live"       // 连着
	PhaseRetrying   Phase = "retrying"   // 连上过、断了，正在重连
	PhaseStopped    Phase = "stopped"    // Stop 了，或者遇到重试也没用的错误（见 Err）
)

// State 客户端的状态快照。
type State struct {
	Phase     Phase     `json:"phase"`
	Since     time.Time `json:"since"`
	Addr      string    `json:"addr,omitempty"`
	LastError string    `json:"lastError,omitempty"` // 最近一次连接失败或断线的原因
	Busy      bool      `json:"busy,omitempty"`      // 最近一次被拒是因为已经有别的控制连接
	Connects  int       `json:"connects"`            // 握手成功的次数
	RTTMs     float64   `json:"rttMs,omitempty"`     // 最近一次心跳的往返时间（Windows 上 Go 的时钟一跳 0.5～15.6 毫秒，本机连接常量成 0）
	Welcome   *Welcome  `json:"-"`                   // 最近一次握手时模组发来的
}

// EndError 模组发来 END 结束了连接。
type EndError struct{ Reason string }

// endServerStopping 服务器关服时模组发的 END 原因（CompanionManager 关服时 control.close 的原话）。
const endServerStopping = "服务器正在关闭"

func (e *EndError) Error() string {
	if e.Reason == "" {
		return "伙伴模组结束了连接"
	}
	return "伙伴模组结束了连接：" + e.Reason
}

// ConfigError 读不到模组的配置，或者配置不能用（模组也不会打开端口）。
type ConfigError struct{ Err error }

func (e *ConfigError) Error() string { return e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// errFatal 重试也没用的错误：协议版本对不上、协议错误。
type errFatal struct{ error }

func (e errFatal) Unwrap() error { return e.error }

// IsFatal 是不是重试也没用的错误（客户端遇到它就停下了）。
func IsFatal(err error) bool { return errors.As(err, new(errFatal)) }

type reply struct {
	r   *Response
	err error
}

// Client 连到实例上的伙伴模组：握手、心跳、指令和回复的对应、事件、断线重连都在里面。
// 所有 socket 写入都带超时；Request 可以在任何协程里调用，可以不等回复连着发好几条。
type Client struct {
	o      Options
	stopc  chan struct{}
	done   chan struct{}
	nudgec chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	wmu sync.Mutex // 写连接的先后

	mu      sync.Mutex
	st      State
	err     error    // 停下的原因（重试也没用的错误）；Stop 的为 nil
	reason  string   // Stop 的原因，写进 END
	conn    net.Conn // 正在握手或连着的连接：Stop 时关掉
	live    net.Conn // 握手成功、能发指令的连接
	nextID  int64
	waiters map[int64]chan reply
	unknown map[byte]bool // 记过日志的不认识的帧类型
}

// Start 开始连接（在后台）。
func Start(o Options) *Client {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&o.pingEvery, defaultPing)
	def(&o.readIdle, defaultReadIdle)
	def(&o.quick, defaultQuick)
	def(&o.quickWindow, defaultQuickWindow)
	def(&o.slow, defaultSlow)
	def(&o.busy, defaultBusy)
	def(&o.request, defaultRequest)
	if o.dial == nil {
		var d net.Dialer
		o.dial = func(ctx context.Context, addr string) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }
	}
	c := &Client{o: o, stopc: make(chan struct{}), done: make(chan struct{}), nudgec: make(chan struct{}, 1),
		waiters: map[int64]chan reply{}, unknown: map[byte]bool{}}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.setPhase(PhaseConnecting, "")
	go c.run()
	return c
}

// State 当前状态。
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

// Live 现在连着没有。
func (c *Client) Live() bool { return c.State().Phase == PhaseLive }

// Done 客户端停下（Stop 了或者遇到重试也没用的错误）时关闭。
func (c *Client) Done() <-chan struct{} { return c.done }

// Err 停下的原因；Stop 停下的、还在跑的为 nil。
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Nudge 配置文件刚改过（端口、令牌、enabled）：没连着的话马上重试，并重新开始 10 秒的快速重试。
func (c *Client) Nudge() {
	select {
	case c.nudgec <- struct{}{}:
	default:
	}
}

// Stop 停止：连着的话先给模组发 END（带上原因）再断开；等后台的协程都退出后返回。可以重复调用。
func (c *Client) Stop(reason string) {
	c.halt(reason)
	<-c.done
}

// halt 同 Stop，但不等后台的协程退出：可以在回调（OnDown）里调用，这次断开以后就不再重连。
func (c *Client) halt(reason string) {
	c.once.Do(func() {
		c.mu.Lock()
		c.reason = reason
		conn, live := c.conn, c.live
		c.mu.Unlock()
		close(c.stopc)
		c.cancel()
		if conn != nil && conn != live { // 握手中：直接关掉；连着的由 stream 发完 END 再关
			conn.Close()
		}
	})
}

func (c *Client) stopping() bool {
	select {
	case <-c.stopc:
		return true
	default:
		return false
	}
}

func (c *Client) logf(format string, args ...any) {
	if c.o.Logf != nil {
		c.o.Logf(format, args...)
	}
}

func (c *Client) setPhase(p Phase, lastErr string) {
	c.mu.Lock()
	if c.st.Phase != p {
		c.st.Phase, c.st.Since = p, time.Now()
	}
	if lastErr != "" {
		c.st.LastError = lastErr
	}
	c.mu.Unlock()
}

// sleep 等 d：期间 Stop 了返回 false；Nudge 了提前醒来，nudged 为 true。
func (c *Client) sleep(d time.Duration) (ok, nudged bool) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.stopc:
		return false, false
	case <-c.nudgec:
		return true, true
	case <-t.C:
		return true, false
	}
}

// run 重连的规则（README「连接」）：
//   - 每次连接前都重读配置；
//   - 配置刚改过（Settle）、或者服务器刚开好时，10 秒内连不上、ERROR 1、ERROR 5 都算「模组还没读到」，隔 0.5 秒重试；
//   - 刚断开就重连遇到 ERROR 3，同样隔 0.5 秒重试，最多 10 秒；
//   - 过了这 10 秒照样重试，只是慢一点（别的程序占着控制连接时更慢），服务器开着就一直试，什么时候停由调用方决定；
//   - 协议版本对不上、协议错误：重试也没用，停下，提示用户更新模组或 MineDeck。
func (c *Client) run() {
	defer close(c.done)
	quickUntil := c.o.Settle
	if quickUntil.IsZero() {
		quickUntil = time.Now()
	}
	quickUntil = quickUntil.Add(c.o.quickWindow)
	everLive := false
	fails := 0
	for {
		live, err := c.session()
		if c.stopping() {
			c.setPhase(PhaseStopped, "")
			return
		}
		now := time.Now()
		if live {
			everLive, fails = true, 0
			quickUntil = now.Add(c.o.quickWindow)
		}
		fails++
		var me *ModError
		busy := errors.As(err, &me) && me.Code == ErrBusy
		c.mu.Lock()
		c.st.Busy = busy
		c.mu.Unlock()
		if IsFatal(err) {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			c.setPhase(PhaseStopped, err.Error())
			c.logf("不再连接伙伴模组：%v", err)
			return
		}
		delay := c.o.slow
		switch quick := now.Before(quickUntil); {
		case quick:
			delay = c.o.quick
		case busy:
			delay = c.o.busy
		}
		next := PhaseConnecting
		if everLive {
			next = PhaseRetrying
		}
		c.setPhase(next, err.Error())
		if live || fails == 1 || fails%logEvery == 0 {
			c.logf("%v；%v 后重连", err, delay.Round(100*time.Millisecond))
		}
		ok, nudged := c.sleep(delay)
		if !ok {
			c.setPhase(PhaseStopped, "")
			return
		}
		if nudged {
			quickUntil = time.Now().Add(c.o.quickWindow)
		}
	}
}

// session 连一次：读配置 → 连接 → 握手 → 收发，直到断开。live 表示握手成功过。
func (c *Client) session() (live bool, err error) {
	cfg, err := ReadConfig(c.o.Dir)
	if err != nil {
		return false, &ConfigError{fmt.Errorf("读伙伴模组的配置失败：%w", err)}
	}
	if p := cfg.Problem(); p != "" {
		return false, &ConfigError{errors.New("伙伴模组的配置不能用：" + p)}
	}
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	ctx, cancel := context.WithTimeout(c.ctx, dialTimeout)
	conn, err := c.o.dial(ctx, addr)
	cancel()
	if err != nil {
		return false, fmt.Errorf("连不上伙伴模组（%s）：%w", addr, err)
	}
	defer conn.Close()
	c.mu.Lock()
	c.conn = conn
	c.st.Addr = addr
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()
	if c.stopping() { // Stop 在拨号期间发生：不再握手
		return false, errors.New("已停止")
	}
	br := bufio.NewReader(conn)
	w, err := c.handshake(conn, br, cfg.Token)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	c.live = conn
	c.st.Welcome = &w
	c.st.Connects++
	c.st.Busy = false
	c.st.LastError = ""
	c.mu.Unlock()
	c.setPhase(PhaseLive, "")
	c.logf("已连上伙伴模组（%s，模组 %s，PlayerEngine %s，现有 %d 个伙伴）", addr, w.Mod, w.PlayerEngine, len(w.Companions))
	if c.o.OnWelcome != nil {
		c.o.OnWelcome(w)
	}
	err = c.stream(conn, br)
	c.mu.Lock()
	c.live = nil
	waiters := c.waiters
	c.waiters = map[int64]chan reply{}
	c.mu.Unlock()
	lost := err
	if lost == nil {
		lost = errors.New("连接已关闭")
	}
	for _, ch := range waiters {
		ch <- reply{err: fmt.Errorf("还没收到回复，和伙伴模组的连接就断了（%v），指令可能执行了，也可能没执行", lost)}
	}
	if err != nil && c.o.OnDown != nil {
		c.o.OnDown(err)
	}
	if err == nil {
		err = errors.New("已停止")
	}
	return true, err
}

// handshake 发 HELLO，等 WELCOME 或者 ERROR。版本协商：HELLO 里写 MineDeck 支持的最高版本，模组回 min(v, M)；
// MineDeck 不支持模组选的版本时发 END 断开。
func (c *Client) handshake(conn net.Conn, br *bufio.Reader, token string) (Welcome, error) {
	hello, err := HelloPayload(ProtoVersion, token, c.o.Name)
	if err != nil {
		return Welcome{}, &ConfigError{err}
	}
	if err := c.write(conn, TypeHello, hello, writeTimeout); err != nil {
		return Welcome{}, fmt.Errorf("握手失败：%w", readErr(err))
	}
	_ = conn.SetReadDeadline(time.Now().Add(helloTimeout))
	for {
		f, err := ReadFrame(br)
		if err != nil {
			return Welcome{}, fmt.Errorf("握手失败：%w", readErr(err))
		}
		switch f.Type {
		case TypeWelcome:
			v, js, err := ParseWelcome(f.Payload)
			if err != nil {
				return Welcome{}, errFatal{fmt.Errorf("伙伴模组的 WELCOME 不对：%v", err)}
			}
			if v < 1 || v > ProtoVersion {
				reason := fmt.Sprintf("MineDeck 只支持协议 1–%d，模组选的是 %d", ProtoVersion, v)
				_ = c.write(conn, TypeEnd, []byte(reason), endTimeout)
				return Welcome{}, errFatal{fmt.Errorf("伙伴模组用的是协议 %d，这个 MineDeck 只支持协议 1–%d：请把 MineDeck 和伙伴模组都更新到最新版（在「伙伴」标签里重新安装伙伴模组）", v, ProtoVersion)}
			}
			var w Welcome
			if err := json.Unmarshal(js, &w); err != nil {
				_ = c.write(conn, TypeEnd, []byte("WELCOME 不是合法的 JSON"), endTimeout)
				return Welcome{}, errFatal{fmt.Errorf("伙伴模组的 WELCOME 不是合法的 JSON：%v", err)}
			}
			if w.Protocol == 0 {
				w.Protocol = v
			}
			return w, nil
		case TypeError:
			code, msg := ParseError(f.Payload)
			me := &ModError{Code: code, Msg: msg}
			if code == ErrVersion || code == ErrProtocol {
				return Welcome{}, errFatal{me}
			}
			return Welcome{}, me
		case TypeEnd:
			return Welcome{}, &EndError{Reason: string(f.Payload)}
		default:
			c.noteUnknown(f.Type) // 不认识的帧类型两边都忽略，不断开
		}
	}
}

// write 写一帧（带超时）；几个协程同时写时排队。
func (c *Client) write(conn net.Conn, typ byte, payload []byte, timeout time.Duration) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	return WriteFrame(conn, typ, payload)
}

// stream 连着的时候：读模组发来的帧在单独的协程里（回调按顺序在那里调用）；这里每 2 秒发一次 PING。
// Stop 时先发 END 再断开，返回 nil。
func (c *Client) stream(conn net.Conn, br *bufio.Reader) error {
	errc := make(chan error, 1)
	go func() { errc <- c.readLoop(conn, br) }()
	ping := time.NewTicker(c.o.pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-c.stopc:
			c.mu.Lock()
			reason := c.reason
			c.mu.Unlock()
			_ = c.write(conn, TypeEnd, []byte(reason), endTimeout)
			conn.Close()
			<-errc
			return nil
		case err := <-errc:
			return err
		case <-ping.C:
			var ts [8]byte
			binary.LittleEndian.PutUint64(ts[:], uint64(time.Now().UnixNano()))
			if err := c.write(conn, TypePing, ts[:], writeTimeout); err != nil {
				conn.Close()
				<-errc
				return fmt.Errorf("发送失败：%w", readErr(err))
			}
		}
	}
}

func (c *Client) readLoop(conn net.Conn, br *bufio.Reader) error {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(c.o.readIdle))
		f, err := ReadFrame(br)
		if err != nil {
			var ne net.Error
			switch {
			case errors.As(err, &ne) && ne.Timeout():
				return fmt.Errorf("心跳超时：%v 没收到伙伴模组的数据", c.o.readIdle.Round(time.Second))
			case errors.Is(err, io.EOF):
				return errors.New("伙伴模组关闭了连接")
			}
			return fmt.Errorf("连接断开：%w", readErr(err))
		}
		switch f.Type {
		case TypeResponse:
			c.onResponse(f.Payload)
		case TypeEvent:
			var e Event
			if json.Unmarshal(f.Payload, &e) == nil && e.Event != "" && c.o.OnEvent != nil {
				c.o.OnEvent(e)
			}
		case TypeStatus:
			var s Status
			if json.Unmarshal(f.Payload, &s) == nil && c.o.OnStatus != nil {
				c.o.OnStatus(s)
			}
		case TypePong:
			if len(f.Payload) == 8 {
				sent := int64(binary.LittleEndian.Uint64(f.Payload))
				if rtt := time.Since(time.Unix(0, sent)); rtt >= 0 && rtt < time.Minute {
					c.mu.Lock()
					c.st.RTTMs = float64(rtt.Microseconds()) / 1000
					c.mu.Unlock()
				}
			}
		case TypeEnd:
			return &EndError{Reason: string(f.Payload)}
		case TypeError:
			code, msg := ParseError(f.Payload)
			return &ModError{Code: code, Msg: msg}
		default:
			c.noteUnknown(f.Type)
		}
	}
}

func (c *Client) noteUnknown(t byte) {
	c.mu.Lock()
	first := !c.unknown[t]
	c.unknown[t] = true
	c.mu.Unlock()
	if first {
		c.logf("伙伴模组发来了不认识的帧类型 0x%02x，忽略", t)
	}
}

// onResponse 按 id 交给等着的 Request。取不到 id（bad_json）的回复没法对应，丢掉。
func (c *Client) onResponse(p []byte) {
	var r Response
	if json.Unmarshal(p, &r) != nil {
		return
	}
	r.Raw = append(json.RawMessage(nil), p...)
	var id int64
	if json.Unmarshal(r.ID, &id) != nil {
		return
	}
	c.mu.Lock()
	ch := c.waiters[id]
	delete(c.waiters, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- reply{r: &r}
	}
}

// Request 发一条指令，等它的回复（最多 15 秒）。args 是指令的参数（id、op 由这里填）。
// 返回的错误只说明没拿到回复（没连着、断线、超时）；模组回了 ok=false 时 err 为 nil，看 Response.OK。
func (c *Client) Request(ctx context.Context, op string, args map[string]any) (*Response, error) {
	body := make(map[string]any, len(args)+2)
	for k, v := range args {
		body[k] = v
	}
	c.mu.Lock()
	conn := c.live
	if conn == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	c.nextID++
	id := c.nextID
	ch := make(chan reply, 1)
	c.waiters[id] = ch
	c.mu.Unlock()
	body["id"], body["op"] = id, op
	drop := func() {
		c.mu.Lock()
		delete(c.waiters, id)
		c.mu.Unlock()
	}
	b, err := json.Marshal(body)
	if err == nil && len(b) > MaxPayload {
		err = fmt.Errorf("指令太长：%d 字节", len(b))
	}
	if err != nil {
		drop()
		return nil, err
	}
	if err := c.write(conn, TypeRequest, b, writeTimeout); err != nil {
		drop()
		conn.Close() // 写不进去：这个连接坏了，读协程会报错、重连
		return nil, fmt.Errorf("指令没发出去：%w", readErr(err))
	}
	t := time.NewTimer(c.o.request)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.r, r.err
	case <-t.C:
		drop()
		return nil, fmt.Errorf("伙伴模组 %v 没有回复（服务器可能很卡），指令可能执行了，也可能没执行", c.o.request.Round(time.Second))
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-c.stopc:
		drop()
		return nil, errors.New("已停止连接伙伴模组")
	}
}

// Call 发一条指令：模组回了 ok=false 时返回 *OpError。out 不为 nil 时把回复解进去。
func (c *Client) Call(ctx context.Context, op string, args map[string]any, out any) (*Response, error) {
	r, err := c.Request(ctx, op, args)
	if err != nil {
		return nil, err
	}
	if !r.OK {
		return r, &OpError{Op: op, Code: r.Code, Msg: r.Error, Reply: r}
	}
	if out != nil {
		if err := r.Decode(out); err != nil {
			return r, fmt.Errorf("伙伴模组的回复读不懂：%w", err)
		}
	}
	return r, nil
}

// readErr 把「use of closed network connection」这类底层错误说得好懂一点。
func readErr(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return errors.New("连接已关闭")
	}
	return err
}
