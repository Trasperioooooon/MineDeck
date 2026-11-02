package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 协议 v1 的常量，见 companion/README.md 的「协议 v1」。
const (
	protoVersion = 1
	maxPayload   = 0xFFFF

	typeHello    = 0x01
	typeWelcome  = 0x02
	typeError    = 0x03
	typeRequest  = 0x10
	typeResponse = 0x11
	typeEvent    = 0x12
	typePing     = 0x20
	typePong     = 0x21
	typeStatus   = 0x30
	typeEnd      = 0x40

	errBadToken = 1
	errVersion  = 2
	errBusy     = 3
	errProtocol = 4
	errDisabled = 5
)

type frame struct {
	typ     byte
	payload []byte
}

func writeFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > maxPayload {
		return fmt.Errorf("帧太长：%d 字节", len(payload))
	}
	buf := make([]byte, 4+len(payload))
	buf[0] = typ
	binary.LittleEndian.PutUint16(buf[2:], uint16(len(payload)))
	copy(buf[4:], payload)
	_, err := w.Write(buf)
	return err
}

func readFrame(r io.Reader) (frame, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return frame{}, err
	}
	n := int(binary.LittleEndian.Uint16(h[2:]))
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return frame{}, err
	}
	return frame{h[0], p}, nil
}

func helloPayload(version int, token, name string) ([]byte, error) {
	if len(token) > 255 {
		return nil, errors.New("令牌太长（最多 255 字节）")
	}
	p := []byte{byte(version), byte(version >> 8), byte(len(token))}
	p = append(p, token...)
	p = append(p, name...)
	return p, nil
}

// rejectError 模组用 ERROR 帧拒绝了连接。
type rejectError struct {
	code int
	msg  string
}

func (e *rejectError) Error() string {
	return fmt.Sprintf("模组拒绝：错误码 %d：%s", e.code, e.msg)
}

// client 一个控制连接。读线程把 RESPONSE 交给等它的调用，EVENT 放进 events，所有帧按 printer 打印。
type client struct {
	c       net.Conn
	wmu     sync.Mutex
	mu      sync.Mutex
	nextID  int64
	waiting map[int64]chan map[string]any
	events  chan map[string]any
	done    chan struct{}
	err     error
	out     io.Writer
	verbose bool
	version int
	welcome map[string]any
}

// dial 连上模组并握手。
func dial(addr, token, name string, out io.Writer, verbose bool) (*client, error) {
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, err
	}
	hello, err := helloPayload(protoVersion, token, name)
	if err != nil {
		c.Close()
		return nil, err
	}
	if err := writeFrame(c, typeHello, hello); err != nil {
		c.Close()
		return nil, err
	}
	_ = c.SetReadDeadline(time.Now().Add(6 * time.Second))
	f, err := readFrame(c)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("等 WELCOME 时出错：%w", err)
	}
	_ = c.SetReadDeadline(time.Time{})
	switch f.typ {
	case typeWelcome:
	case typeError:
		c.Close()
		if len(f.payload) < 2 {
			return nil, &rejectError{0, ""}
		}
		return nil, &rejectError{int(binary.LittleEndian.Uint16(f.payload)), string(f.payload[2:])}
	default:
		c.Close()
		return nil, fmt.Errorf("第一帧不是 WELCOME：0x%02x", f.typ)
	}
	if len(f.payload) < 2 {
		c.Close()
		return nil, errors.New("WELCOME 太短")
	}
	cl := &client{c: c, waiting: map[int64]chan map[string]any{}, events: make(chan map[string]any, 256),
		done: make(chan struct{}), out: out, verbose: verbose}
	cl.version = int(binary.LittleEndian.Uint16(f.payload))
	if err := json.Unmarshal(f.payload[2:], &cl.welcome); err != nil {
		c.Close()
		return nil, fmt.Errorf("WELCOME 不是 JSON：%w", err)
	}
	if cl.version < 1 || cl.version > protoVersion {
		_ = writeFrame(c, typeEnd, []byte("testctl 不支持协议版本 "+strconv.Itoa(cl.version)))
		c.Close()
		return nil, fmt.Errorf("模组选的协议版本 %d 不支持", cl.version)
	}
	cl.printf("← WELCOME v%d %s\n", cl.version, f.payload[2:])
	go cl.readLoop()
	go cl.pingLoop()
	return cl, nil
}

func (cl *client) printf(format string, a ...any) {
	if cl.out != nil {
		fmt.Fprintf(cl.out, format, a...)
	}
}

func (cl *client) send(typ byte, p []byte) error {
	cl.wmu.Lock()
	defer cl.wmu.Unlock()
	return writeFrame(cl.c, typ, p)
}

func (cl *client) readLoop() {
	defer close(cl.done)
	for {
		f, err := readFrame(cl.c)
		if err != nil {
			cl.err = err
			return
		}
		switch f.typ {
		case typeResponse:
			cl.printf("← RESPONSE %s\n", f.payload)
			var m map[string]any
			if json.Unmarshal(f.payload, &m) != nil {
				continue
			}
			id, _ := m["id"].(float64)
			cl.mu.Lock()
			ch := cl.waiting[int64(id)]
			delete(cl.waiting, int64(id))
			cl.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case typeEvent:
			cl.printf("← EVENT %s\n", f.payload)
			var m map[string]any
			if json.Unmarshal(f.payload, &m) == nil {
				select {
				case cl.events <- m:
				default:
				}
			}
		case typeStatus:
			if cl.verbose {
				cl.printf("← STATUS %s\n", f.payload)
			}
		case typePong:
		case typeEnd:
			cl.printf("← END %s\n", f.payload)
			cl.err = errors.New("模组结束了连接：" + string(f.payload))
			return
		case typeError:
			msg := ""
			if len(f.payload) >= 2 {
				msg = string(f.payload[2:])
			}
			cl.printf("← ERROR %s\n", msg)
		default:
			// 不认识的帧类型：照协议忽略
			cl.printf("← 不认识的帧 0x%02x（%d 字节），忽略\n", f.typ, len(f.payload))
		}
	}
}

func (cl *client) pingLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-cl.done:
			return
		case <-t.C:
			ts := make([]byte, 8)
			binary.LittleEndian.PutUint64(ts, uint64(time.Now().UnixNano()))
			if cl.send(typePing, ts) != nil {
				return
			}
		}
	}
}

// call 发一条指令，等它的回复。req 里没有 id 时自动编号。
func (cl *client) call(req map[string]any, timeout time.Duration) (map[string]any, error) {
	cl.mu.Lock()
	cl.nextID++
	id := cl.nextID
	ch := make(chan map[string]any, 1)
	cl.waiting[id] = ch
	cl.mu.Unlock()
	req["id"] = id
	b, _ := json.Marshal(req)
	cl.printf("→ REQUEST %s\n", b)
	if err := cl.send(typeRequest, b); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-cl.done:
		return nil, fmt.Errorf("连接断开：%v", cl.err)
	case <-time.After(timeout):
		return nil, errors.New("等回复超时")
	}
}

// sendRaw 原样发一帧 REQUEST（测试错误的指令用），等 id 为 expectID 的回复；expectID < 0 时等任意下一条回复。
func (cl *client) sendRaw(payload []byte, timeout time.Duration) (map[string]any, error) {
	cl.mu.Lock()
	ch := make(chan map[string]any, 1)
	cl.waiting[0] = ch // 取不到 id 的回复（"id":null）落在 0 上
	cl.mu.Unlock()
	var probe map[string]any
	if json.Unmarshal(payload, &probe) == nil {
		if id, ok := probe["id"].(float64); ok {
			cl.mu.Lock()
			delete(cl.waiting, 0)
			cl.waiting[int64(id)] = ch
			cl.mu.Unlock()
		}
	}
	cl.printf("→ REQUEST %s\n", payload)
	if err := cl.send(typeRequest, payload); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-cl.done:
		return nil, fmt.Errorf("连接断开：%v", cl.err)
	case <-time.After(timeout):
		return nil, errors.New("等回复超时")
	}
}

// waitEvent 等一个满足条件的事件。
func (cl *client) waitEvent(match func(map[string]any) bool, timeout time.Duration) (map[string]any, error) {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-cl.events:
			if match(ev) {
				return ev, nil
			}
		case <-cl.done:
			return nil, fmt.Errorf("连接断开：%v", cl.err)
		case <-deadline:
			return nil, errors.New("等事件超时")
		}
	}
}

func (cl *client) close(reason string) {
	_ = cl.send(typeEnd, []byte(reason))
	time.Sleep(100 * time.Millisecond)
	cl.c.Close()
}

// readConfig 读 companion.properties 里的 enabled、port、token（只认最简单的 key=value / key: value，# 和 ! 开头是注释）。
func readConfig(path string) (port int, token string, enabled bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false, err
	}
	b = bytes.TrimPrefix(b, []byte("\xEF\xBB\xBF"))
	port = 24480
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		i := strings.IndexAny(line, "=:")
		if i < 0 {
			continue
		}
		k, v := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		switch k {
		case "port":
			if port, err = strconv.Atoi(v); err != nil {
				return 0, "", false, fmt.Errorf("port 不是数字：%v", err)
			}
		case "token":
			token = v
		case "enabled":
			enabled = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes") || strings.EqualFold(v, "on")
		}
	}
	return port, token, enabled, sc.Err()
}

// connect 照协议的「连接」一节：每次连接前都重读配置；刚改完配置时 ERROR 1、ERROR 5、连不上都算模组还没读到，
// 隔 0.5 秒重试，最多 10 秒；刚断开就重连遇到 ERROR 3 同样重试。
func connect(cfgPath, addr, token, name string, out io.Writer, verbose bool, patience time.Duration) (*client, error) {
	deadline := time.Now().Add(patience)
	for {
		a, t := addr, token
		if cfgPath != "" {
			port, tok, _, err := readConfig(cfgPath)
			if err != nil {
				return nil, fmt.Errorf("读配置失败：%w", err)
			}
			a = fmt.Sprintf("127.0.0.1:%d", port)
			if t == "" {
				t = tok
			}
		}
		cl, err := dial(a, t, name, out, verbose)
		if err == nil {
			return cl, nil
		}
		retry := false
		var re *rejectError
		if errors.As(err, &re) {
			retry = re.code == errBusy || re.code == errBadToken || re.code == errDisabled
		} else {
			var ne net.Error
			retry = errors.As(err, &ne) || strings.Contains(err.Error(), "refused")
		}
		if !retry || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
