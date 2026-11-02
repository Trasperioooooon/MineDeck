package bridge

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// 客户端的时间参数（README「流程」一节）。
const (
	defaultGrace      = 10 * time.Second
	defaultBusyWait   = 8 * time.Second
	defaultRetry      = 1500 * time.Millisecond
	defaultPing       = time.Second     // 每秒至少发一帧
	defaultReadIdle   = 5 * time.Second // 5 秒没收到模组的任何帧算断线（STATUS 每 2 秒一次）
	dialTimeout       = 3 * time.Second
	handshakeTimeout  = 5 * time.Second
	writeTimeout      = 2 * time.Second
	endTimeout        = time.Second
	queueFrames       = 10 // 待发的 PCM 最多 10 帧（200 毫秒），满了丢最旧的
	reconnectLogEvery = 30 // 一直重连时，每 30 次才记一条日志
)

// Options 客户端参数。
type Options struct {
	Dir  string // 实例目录：每次连接前都重新读一遍配置文件里的端口和令牌（用户或 MineDeck 可能刚改过）
	Name string // HELLO 里的广播者名字，只写进模组日志

	// NotBefore 配置刚写入时等到这个时间再连：模组每 2 秒才重读一次配置，连早了会碰上旧端口、旧令牌，
	// 或者共享还没打开。
	NotBefore time.Time
	// Grace 一串失败最多容忍多久就放弃（0 为 10 秒）。只对这几种算：还没连上过时连不上；令牌不对、
	// 共享已关闭、读不到配置（模组可能还没读到新配置）。连上过以后断线一律接着重连（服务器重启语音服务时会这样），
	// 什么时候停由调用方决定。
	Grace time.Duration
	// BusyWait 还没连上过、一直「已经有一个广播连接」时等多久（0 为 8 秒）：旧连接最多 5 秒心跳超时。
	BusyWait time.Duration
	// RetryDelay 重连间隔（0 为 1.5 秒）。
	RetryDelay time.Duration
	// Logf 诊断日志（连上、断线、放弃）；nil 不输出。
	Logf func(format string, args ...any)

	dial      func(ctx context.Context, addr string) (net.Conn, error) // 测试用
	pingEvery time.Duration                                            // 测试用
	readIdle  time.Duration                                            // 测试用
	writeIdle time.Duration                                            // 测试用
}

// Phase 客户端所处的阶段。
type Phase string

const (
	PhaseConnecting Phase = "connecting" // 还没连上过（含等模组重读配置）
	PhaseLive       Phase = "live"       // 已连上，正在推音频
	PhaseRetrying   Phase = "retrying"   // 连上过、断线了，正在重连
	PhaseStopped    Phase = "stopped"    // 已停止：Stop 了，或者放弃了（见 Err）
)

// State 客户端的状态快照。
type State struct {
	Phase      Phase     `json:"phase"`
	Since      time.Time `json:"since"`                // 进入这个阶段的时间
	Addr       string    `json:"addr,omitempty"`       // 模组的地址
	ModVersion string    `json:"modVersion,omitempty"` // 握手时模组报的版本
	Status     *Status   `json:"status,omitempty"`     // 模组最近一次报的状态（这次连接）
	RTTMs      float64   `json:"rttMs,omitempty"`      // 最近一次心跳的往返时间（Windows 上 Go 的时钟一跳 0.5～15.6 毫秒，本机连接常量成 0）
	Sent       uint64    `json:"sent"`                 // 发出的 PCM 帧（累计）
	Dropped    uint64    `json:"dropped"`              // 没连上、或者发不及丢掉的帧（累计）
	Connects   int       `json:"connects"`             // 握手成功的次数
	LastError  string    `json:"lastError,omitempty"`  // 最近一次连接失败或断线的原因
	Busy       bool      `json:"busy,omitempty"`       // 最近一次被拒是因为已经有别的广播连接
}

// EndError 模组发来 END 结束了连接。
type EndError struct{ Reason string }

func (e *EndError) Error() string {
	if e.Reason == "" {
		return "桥接模组结束了连接"
	}
	return "桥接模组结束了连接：" + e.Reason
}

// ConfigError 读不到模组配置，或者配置不能用。
type ConfigError struct{ Err error }

func (e *ConfigError) Error() string { return e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// Client 连到实例上的桥接模组，把 PCM 帧发过去；握手、心跳、断线重连都在里面。
// Send 从不阻塞（采集线程不能被拖住）；所有 socket 写入都在客户端自己的 goroutine 里，带超时。
type Client struct {
	opts  Options
	queue chan []int16
	stopc chan struct{}
	done  chan struct{}
	ctx   context.Context
	stopF context.CancelFunc

	phase    atomic.Value // Phase，Send 的快路径用
	stopOnce sync.Once
	wg       sync.WaitGroup

	mu     sync.Mutex
	st     State
	err    error
	reason string   // Stop 的原因，写进 END
	hsConn net.Conn // 握手中的连接：Stop 时关掉它，免得等握手超时
}

// Start 开始连接（在后台）。
func Start(opts Options) *Client {
	if opts.Grace <= 0 {
		opts.Grace = defaultGrace
	}
	if opts.BusyWait <= 0 {
		opts.BusyWait = defaultBusyWait
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = defaultRetry
	}
	if opts.pingEvery <= 0 {
		opts.pingEvery = defaultPing
	}
	if opts.readIdle <= 0 {
		opts.readIdle = defaultReadIdle
	}
	if opts.writeIdle <= 0 {
		opts.writeIdle = writeTimeout
	}
	if opts.dial == nil {
		var d net.Dialer
		opts.dial = func(ctx context.Context, addr string) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }
	}
	c := &Client{opts: opts, queue: make(chan []int16, queueFrames), stopc: make(chan struct{}), done: make(chan struct{})}
	c.ctx, c.stopF = context.WithCancel(context.Background())
	c.setPhase(PhaseConnecting, "")
	go c.run()
	return c
}

// Send 交一帧 PCM（48 kHz 单声道，960 个采样）。只在已连上时排队，不阻塞；返回这一帧有没有排上队。
func (c *Client) Send(samples []int16) bool {
	if c.phase.Load() != PhaseLive {
		c.addDropped(1)
		return false
	}
	s := append([]int16(nil), samples...)
	for range 2 {
		select {
		case c.queue <- s:
			return true
		default:
		}
		select { // 写不及：丢掉最旧的一帧，保持低延迟
		case <-c.queue:
			c.addDropped(1)
		default:
		}
	}
	c.addDropped(1)
	return false
}

// State 当前状态。
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.st
	if st.Status != nil {
		s := *st.Status
		st.Status = &s
	}
	return st
}

// Done 客户端停下（Stop 了或者放弃了）时关闭。
func (c *Client) Done() <-chan struct{} { return c.done }

// Err 放弃的原因；Stop 停下的为 nil。
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Stop 停止：连着的话先给模组发 END（带上原因），再断开；等后台的 goroutine 都退出后返回。可以重复调用。
func (c *Client) Stop(reason string) {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.reason = reason
		hs := c.hsConn
		c.mu.Unlock()
		close(c.stopc)
		c.stopF()
		if hs != nil {
			hs.Close()
		}
	})
	<-c.done
	c.wg.Wait()
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
	if c.opts.Logf != nil {
		c.opts.Logf(format, args...)
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
	c.phase.Store(p)
}

func (c *Client) addDropped(n uint64) {
	c.mu.Lock()
	c.st.Dropped += n
	c.mu.Unlock()
}

// sleep 等 d，期间 Stop 了返回 false。
func (c *Client) sleep(d time.Duration) bool {
	if d <= 0 {
		return !c.stopping()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.stopc:
		return false
	case <-t.C:
		return true
	}
}

// errFatal 不用再重试的错误。
type errFatal struct{ error }

func (e errFatal) Unwrap() error { return e.error }

var errBadWelcome = errors.New("桥接模组的 WELCOME 不对")

func (c *Client) run() {
	defer close(c.done)
	defer c.drop()
	if !c.sleep(time.Until(c.opts.NotBefore)) {
		c.setPhase(PhaseStopped, "")
		return
	}
	everLive := false
	var trouble time.Time // 这一串失败从什么时候开始；握手成功就清零
	fails := 0
	for {
		live, err := c.session()
		if c.stopping() {
			c.setPhase(PhaseStopped, "")
			return
		}
		now := time.Now()
		if live {
			everLive, trouble, fails = true, time.Time{}, 0
		}
		if trouble.IsZero() {
			trouble = now
		}
		fails++
		var me *ModError
		var ce *ConfigError
		busy := errors.As(err, &me) && me.Code == ErrBusy
		var fatal error
		switch {
		case errors.As(err, new(errFatal)):
			fatal = err
		case busy:
			if !everLive && now.Sub(trouble) > c.opts.BusyWait {
				fatal = fmt.Errorf("已经有别的广播连接（可能是另一台电脑上的 MineDeck，或者测试工具），等了 %v 还没结束", c.opts.BusyWait.Round(time.Second))
			}
		case errors.As(err, &ce), errors.As(err, &me):
			// 令牌不对、共享已关闭、读不到配置：模组可能还没读到新配置（每 2 秒一次），等一会儿
			if now.Sub(trouble) > c.opts.Grace {
				fatal = err
			}
		default: // 连不上、断线
			if !everLive && now.Sub(trouble) > c.opts.Grace {
				fatal = fmt.Errorf("%w（桥接模组可能没有加载，或者音乐共享端口没打开）", err)
			}
		}
		c.mu.Lock()
		c.st.Busy = busy
		c.mu.Unlock()
		if fatal != nil {
			c.mu.Lock()
			c.err = fatal
			c.mu.Unlock()
			c.setPhase(PhaseStopped, fatal.Error())
			c.logf("放弃连接桥接模组：%v", fatal)
			return
		}
		next := PhaseConnecting
		if everLive {
			next = PhaseRetrying
		}
		c.setPhase(next, err.Error())
		if live || fails == 1 || fails%reconnectLogEvery == 0 {
			c.logf("%v；%v 后重连", err, c.opts.RetryDelay.Round(100*time.Millisecond))
		}
		if !c.sleep(c.opts.RetryDelay) {
			c.setPhase(PhaseStopped, "")
			return
		}
	}
}

// drop 停下以后队列里剩下的帧算丢掉。
func (c *Client) drop() {
	for {
		select {
		case <-c.queue:
			c.addDropped(1)
		default:
			return
		}
	}
}

// session 连一次：读配置 → 连接 → 握手 → 推音频，直到断开。live 表示握手成功过。
func (c *Client) session() (live bool, err error) {
	cfg, err := ReadConfig(c.opts.Dir)
	switch {
	case os.IsNotExist(err):
		return false, &ConfigError{fmt.Errorf("没有桥接模组的配置文件 %s", ConfigRel)}
	case err != nil:
		return false, &ConfigError{fmt.Errorf("读桥接模组的配置失败：%w", err)}
	}
	if p := cfg.Problem(); p != "" {
		return false, &ConfigError{fmt.Errorf("桥接模组的配置不能用：%s", p)}
	}
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	ctx, cancel := context.WithTimeout(c.ctx, dialTimeout)
	conn, err := c.opts.dial(ctx, addr)
	cancel()
	if err != nil {
		return false, fmt.Errorf("连不上桥接模组（%s）：%w", addr, err)
	}
	defer conn.Close()
	c.mu.Lock()
	c.hsConn = conn
	c.st.Addr = addr
	c.mu.Unlock()
	if c.stopping() { // Stop 在拨号期间发生：不再握手
		return false, errors.New("已停止")
	}
	w, err := c.handshake(conn, cfg.Token)
	c.mu.Lock()
	c.hsConn = nil
	c.mu.Unlock()
	if err != nil {
		return false, err
	}
	for { // 上一次连接剩下的帧已经过时了
		select {
		case <-c.queue:
			continue
		default:
		}
		break
	}
	c.mu.Lock()
	c.st.ModVersion, c.st.Status, c.st.Connects, c.st.Busy = w.ModVersion, &Status{Listeners: w.Listeners}, c.st.Connects+1, false
	c.mu.Unlock()
	c.setPhase(PhaseLive, "")
	c.logf("已连上桥接模组（%s，模组 %s），当前听众 %d 位", addr, w.ModVersion, w.Listeners)
	return true, c.stream(conn)
}

func (c *Client) handshake(conn net.Conn, token string) (Welcome, error) {
	hello, err := HelloPayload(token, c.opts.Name)
	if err != nil {
		return Welcome{}, errFatal{err}
	}
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := WriteFrame(conn, TypeHello, hello); err != nil {
		return Welcome{}, fmt.Errorf("握手失败：%w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	f, err := ReadFrame(conn)
	if err != nil {
		return Welcome{}, fmt.Errorf("握手失败：%w", readErr(err))
	}
	switch f.Type {
	case TypeWelcome:
	case TypeError:
		return Welcome{}, modError(f.Payload)
	default:
		return Welcome{}, errFatal{fmt.Errorf("%w：收到意外的帧类型 0x%02x", errBadWelcome, f.Type)}
	}
	w, err := ParseWelcome(f.Payload)
	if err != nil {
		return Welcome{}, errFatal{fmt.Errorf("%w：%v", errBadWelcome, err)}
	}
	if w.Version != ProtoVersion || w.SampleRate != SampleRate || w.FrameSamples != FrameSamples || w.Channels != 1 {
		return Welcome{}, errFatal{fmt.Errorf("%w：协议 %d、%d Hz、每帧 %d 个采样、%d 声道（MineDeck 要的是协议 %d、%d Hz、%d 个采样、单声道）",
			errBadWelcome, w.Version, w.SampleRate, w.FrameSamples, w.Channels, ProtoVersion, SampleRate, FrameSamples)}
	}
	return w, nil
}

// stream 推音频直到断开：读模组发来的帧（PONG、STATUS、END、ERROR）在单独的 goroutine 里；
// 写（PCM、PING、END）都在这里，每次写都带超时。Stop 时发 END 后返回 nil。
func (c *Client) stream(conn net.Conn) error {
	errc := make(chan error, 1)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		errc <- c.readLoop(conn)
	}()
	ping := time.NewTicker(c.opts.pingEvery)
	defer ping.Stop()
	var seq uint32
	write := func(typ byte, p []byte) error {
		_ = conn.SetWriteDeadline(time.Now().Add(c.opts.writeIdle))
		if err := WriteFrame(conn, typ, p); err != nil {
			return fmt.Errorf("发送失败：%w", readErr(err))
		}
		return nil
	}
	for {
		select {
		case <-c.stopc:
			c.mu.Lock()
			reason := c.reason
			c.mu.Unlock()
			_ = conn.SetWriteDeadline(time.Now().Add(endTimeout))
			_ = WriteFrame(conn, TypeEnd, []byte(reason))
			return nil
		case err := <-errc:
			return err
		case s := <-c.queue:
			if err := write(TypePCM, PCMFrame(seq, s)); err != nil {
				return err
			}
			seq++
			c.mu.Lock()
			c.st.Sent++
			c.mu.Unlock()
		case <-ping.C:
			var ts [8]byte
			binary.LittleEndian.PutUint64(ts[:], uint64(time.Now().UnixNano()))
			if err := write(TypePing, ts[:]); err != nil {
				return err
			}
		}
	}
}

func (c *Client) readLoop(conn net.Conn) error {
	br := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(c.opts.readIdle))
		f, err := ReadFrame(br)
		if err != nil {
			var ne net.Error
			switch {
			case errors.As(err, &ne) && ne.Timeout():
				return fmt.Errorf("心跳超时：%v 没收到桥接模组的数据", c.opts.readIdle.Round(time.Second))
			case errors.Is(err, io.EOF):
				return errors.New("桥接模组关闭了连接")
			}
			return fmt.Errorf("连接断开：%w", readErr(err))
		}
		switch f.Type {
		case TypePong:
			if len(f.Payload) == 8 {
				sent := int64(binary.LittleEndian.Uint64(f.Payload))
				if rtt := time.Since(time.Unix(0, sent)); rtt >= 0 && rtt < time.Minute {
					c.mu.Lock()
					c.st.RTTMs = float64(rtt.Microseconds()) / 1000
					c.mu.Unlock()
				}
			}
		case TypeStatus:
			if s, err := ParseStatus(f.Payload); err == nil {
				c.mu.Lock()
				c.st.Status = &s
				c.mu.Unlock()
			}
		case TypeEnd:
			return &EndError{Reason: string(f.Payload)}
		case TypeError:
			return modError(f.Payload)
		}
	}
}

// modError ERROR 帧：协议版本不对、协议错误说明两边的实现对不上，重试也没用。
func modError(p []byte) error {
	code, msg := ParseError(p)
	me := &ModError{Code: code, Msg: msg}
	if code == ErrVersion || code == ErrProtocol {
		return errFatal{me}
	}
	return me
}

// readErr 把「use of closed network connection」这类底层错误说得好懂一点。
func readErr(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return errors.New("连接已关闭")
	}
	return err
}
