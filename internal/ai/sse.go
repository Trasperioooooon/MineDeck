package ai

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"sync"
	"time"
)

// sseEvent 一个 SSE 事件：event 字段（没写就是空）和 data（多行 data 用换行连起来）。
type sseEvent struct {
	Name string
	Data []byte
}

// sseReader 按 SSE 规范逐个读出事件：空行分隔事件；冒号开头的是注释（有的服务商用它保活），跳过；
// id、retry 字段我们用不着，也跳过。兼容 \r\n。
type sseReader struct {
	r   *bufio.Reader
	max int // 单行最长多少字节，超出当作出错（防止对方发一个没完没了的行把内存撑爆）
}

const maxSSELine = 16 << 20

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(r, 64<<10), max: maxSSELine}
}

var errLineTooLong = errors.New("SSE 单行过长")

// line 读一行（不含行尾）。读到末尾且最后一行没有换行时，照样返回这一行。
func (s *sseReader) line() ([]byte, error) {
	var buf []byte
	for {
		frag, err := s.r.ReadSlice('\n')
		buf = append(buf, frag...)
		if len(buf) > s.max {
			return nil, errLineTooLong
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if err == io.EOF && len(buf) > 0 {
				return bytes.TrimRight(buf, "\r\n"), nil
			}
			return nil, err
		}
		return bytes.TrimRight(buf, "\r\n"), nil
	}
}

// next 下一个事件；流结束返回 io.EOF（结束前没有空行收尾的最后一个事件也会交出来）。
func (s *sseReader) next() (sseEvent, error) {
	var ev sseEvent
	var data [][]byte
	have := false
	for {
		l, err := s.line()
		if err != nil {
			if err == io.EOF && have {
				ev.Data = bytes.Join(data, []byte("\n"))
				return ev, nil
			}
			return sseEvent{}, err
		}
		if len(l) == 0 {
			if have {
				ev.Data = bytes.Join(data, []byte("\n"))
				return ev, nil
			}
			continue
		}
		if l[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(l, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			ev.Name = string(value)
			have = true
		case "data":
			data = append(data, append([]byte(nil), value...))
			have = true
		}
	}
}

// watchdog 一段时间没有新数据就调用 fire（取消请求）。每读到数据调 kick 重新计时。
type watchdog struct {
	mu    sync.Mutex
	t     *time.Timer
	d     time.Duration
	fired bool
}

func newWatchdog(d time.Duration, fire func()) *watchdog {
	w := &watchdog{d: d}
	w.t = time.AfterFunc(d, func() {
		w.mu.Lock()
		w.fired = true
		w.mu.Unlock()
		fire()
	})
	return w
}

func (w *watchdog) kick() { w.t.Reset(w.d) }
func (w *watchdog) stop() { w.t.Stop() }

func (w *watchdog) Fired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fired
}

// kickReader 每读到数据就让看门狗重新计时。
type kickReader struct {
	r io.Reader
	w *watchdog
}

func (k kickReader) Read(p []byte) (int, error) {
	n, err := k.r.Read(p)
	if n > 0 {
		k.w.kick()
	}
	return n, err
}
