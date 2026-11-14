package ai

import (
	"context"
	"errors"
	"io"
	"sync"
)

// 连续的合法小事件也能无限增长。按接收字节限制整次响应，正文、思考、
// 工具参数、签名、保活和未知字段都计入；不依赖服务商遵守 max_tokens。
const (
	maxResponseBytes     = 8 << 20
	maxTurnResponseBytes = 32 << 20
	maxReplyTools        = 128
	maxReplyBlocks       = 1024
	maxSSEEvents         = 65536
)

var ErrResponseTooLarge = errors.New("AI 回复内容过多，已停止接收，请缩小问题范围后重试")

type responseBudgetKey struct{}
type responseBudget struct {
	mu   sync.Mutex
	left int64
}

// WithResponseBudget 为一轮对话的多次 Chat 共用接收额度。重复调用不重置，
// 因而工具往返、重试、服务商回退都算在同一轮里面。
func WithResponseBudget(ctx context.Context) context.Context {
	return ResumeResponseBudget(ctx, 0)
}

// ResumeResponseBudget 等待人工确认后继续同一轮时，沿用已经消耗的额度。
// received 可随待确认状态存盘；旧会话未记录时按零处理。
func ResumeResponseBudget(ctx context.Context, received int64) context.Context {
	if _, ok := ctx.Value(responseBudgetKey{}).(*responseBudget); ok {
		return ctx
	}
	used := min(max(received, 0), int64(maxTurnResponseBytes))
	return context.WithValue(ctx, responseBudgetKey{}, &responseBudget{left: maxTurnResponseBytes - used})
}

// ResponseBytes 这一轮已经收到的字节数，用于跨确认卡片延续额度。
func ResponseBytes(ctx context.Context) int64 {
	b, _ := ctx.Value(responseBudgetKey{}).(*responseBudget)
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(maxTurnResponseBytes) - max(b.left, 0)
}

type responseReader struct {
	r    io.Reader
	left int64
	turn *responseBudget
}

func (c *Client) responseReader(ctx context.Context, r io.Reader) io.Reader {
	b, _ := ctx.Value(responseBudgetKey{}).(*responseBudget)
	return &responseReader{r: r, left: c.responseLimit, turn: b}
}

func responseBudgetSpent(ctx context.Context) bool {
	b, _ := ctx.Value(responseBudgetKey{}).(*responseBudget)
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.left <= 0
}

func (r *responseReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.left < 0 {
		return 0, ErrResponseTooLarge
	}
	if r.turn != nil {
		r.turn.mu.Lock()
		left := r.turn.left
		r.turn.mu.Unlock()
		if left < 0 {
			return 0, ErrResponseTooLarge
		}
		if int64(len(p)) > left+1 {
			p = p[:left+1]
		}
	}
	// 多读一个字节分清「正好到上限」和超限；不把超限块交给解析器或回调。
	if int64(len(p)) > r.left+1 {
		p = p[:r.left+1]
	}
	n, err := r.r.Read(p)
	r.left -= int64(n)
	if r.turn != nil {
		r.turn.mu.Lock()
		r.turn.left -= int64(n)
		over := r.turn.left < 0
		r.turn.mu.Unlock()
		if over {
			r.left = -1
		}
	}
	if r.left < 0 {
		return 0, ErrResponseTooLarge
	}
	return n, err
}
