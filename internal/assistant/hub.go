package assistant

import (
	"encoding/json"
	"sync"
)

// hub 一个会话的事件订阅（界面用 SSE 收）：发不出去的订阅者直接断开，浏览器重连时先拿一份完整的会话再接着收。
type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func (h *hub) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[chan []byte]struct{}{}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *hub) publish(ev any) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default: // 堵住了：断开，让它重连拿完整的会话
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// closeAll 会话被删掉时断开所有订阅。
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}
