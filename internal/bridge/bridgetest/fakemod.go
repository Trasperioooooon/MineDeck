// Package bridgetest 测试用的假桥接模组：在 127.0.0.1 上监听，按协议第 1 版应答，记下收到的东西。
// 给用 bridge.Client 的包写测试用（bridge 自己的测试在包内另有一份）。
package bridgetest

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"minedeck/internal/bridge"
)

// FakeMod 假的桥接模组。
type FakeMod struct {
	ln    net.Listener
	token string

	// Reject 非 0 时，用这个错误码拒绝新连接。
	Reject atomic.Int32
	// Listeners STATUS 和 WELCOME 里报的听众数。
	Listeners atomic.Int32

	mu      sync.Mutex
	conns   []net.Conn
	samples [][]int16 // 收到的每一帧 PCM
	ends    []string  // 收到的 END 原因
	hellos  []string  // HELLO 里的广播者名字
	closed  bool
}

// Start 在 127.0.0.1:port 上监听（port 为 0 时随便挑一个空端口）。
func Start(port int, token string) (*FakeMod, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	m := &FakeMod{ln: ln, token: token}
	go m.serve()
	return m, nil
}

// Port 监听的端口。
func (m *FakeMod) Port() int { return m.ln.Addr().(*net.TCPAddr).Port }

// Close 停止监听并断开所有连接。
func (m *FakeMod) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	m.ln.Close()
	m.Kick()
}

// Kick 断开所有连接（模拟模组崩溃、语音服务器重启）。
func (m *FakeMod) Kick() {
	m.mu.Lock()
	for _, c := range m.conns {
		c.Close()
	}
	m.conns = nil
	m.mu.Unlock()
}

// SendEnd 给所有连接发 END（模拟关服）。
func (m *FakeMod) SendEnd(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.conns {
		_ = bridge.WriteFrame(c, bridge.TypeEnd, []byte(reason))
	}
}

// Samples 收到的 PCM 帧（每帧 960 个采样）。
func (m *FakeMod) Samples() [][]int16 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]int16(nil), m.samples...)
}

// Ends 收到的 END 原因。
func (m *FakeMod) Ends() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.ends...)
}

// Hellos HELLO 里的广播者名字。
func (m *FakeMod) Hellos() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.hellos...)
}

func (m *FakeMod) serve() {
	for {
		c, err := m.ln.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns = append(m.conns, c)
		m.mu.Unlock()
		go m.handle(c)
	}
}

func (m *FakeMod) send(c net.Conn, typ byte, p []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	_ = bridge.WriteFrame(c, typ, p)
}

func (m *FakeMod) handle(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	f, err := bridge.ReadFrame(br)
	if err != nil || f.Type != bridge.TypeHello || len(f.Payload) < 3 {
		return
	}
	tl := int(f.Payload[2])
	if len(f.Payload) < 3+tl {
		return
	}
	tok, name := string(f.Payload[3:3+tl]), string(f.Payload[3+tl:])
	m.mu.Lock()
	m.hellos = append(m.hellos, name)
	m.mu.Unlock()
	if code := m.Reject.Load(); code != 0 {
		m.send(c, bridge.TypeError, append([]byte{byte(code), 0}, bridge.ErrorName(int(code))...))
		return
	}
	if tok != m.token {
		m.send(c, bridge.TypeError, append([]byte{bridge.ErrBadToken, 0}, "令牌不对"...))
		return
	}
	w := make([]byte, 11)
	binary.LittleEndian.PutUint16(w[0:], bridge.ProtoVersion)
	binary.LittleEndian.PutUint32(w[2:], bridge.SampleRate)
	binary.LittleEndian.PutUint16(w[6:], bridge.FrameSamples)
	w[8] = 1
	binary.LittleEndian.PutUint16(w[9:], uint16(m.Listeners.Load()))
	m.send(c, bridge.TypeWelcome, append(w, "0.2.0-fake"...))
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		tk := time.NewTicker(200 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				p := make([]byte, 25)
				binary.LittleEndian.PutUint16(p[0:], uint16(m.Listeners.Load()))
				p[4] = bridge.SourceBroadcast
				m.send(c, bridge.TypeStatus, p)
			}
		}
	}()
	for {
		f, err := bridge.ReadFrame(br)
		if err != nil {
			return
		}
		switch f.Type {
		case bridge.TypePCM:
			if len(f.Payload) != bridge.PCMPayload {
				return
			}
			s := make([]int16, bridge.FrameSamples)
			for i := range s {
				s[i] = int16(binary.LittleEndian.Uint16(f.Payload[4+2*i:]))
			}
			m.mu.Lock()
			m.samples = append(m.samples, s)
			m.mu.Unlock()
		case bridge.TypePing:
			m.send(c, bridge.TypePong, f.Payload)
		case bridge.TypeEnd:
			m.mu.Lock()
			m.ends = append(m.ends, string(f.Payload))
			m.mu.Unlock()
			return
		}
	}
}
