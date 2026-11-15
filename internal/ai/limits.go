package ai

import (
	"context"
	"errors"
	"io"
	"sync"
)

// 回复的上限分两层（2026-11-14 合并后复审发现 7）：
//
//   - 内容：解码以后留下来的东西，也就是正文、思考、工具调用的参数，连同工具的编号和名字、Claude 思考块的签名、
//     内容块的骨架。单次回复 8 MiB，一轮（工具往返、重试、服务商回退、人工确认后继续）合计 32 MiB。
//     以前这两个数按事件流的原始字节算：OpenAI 兼容接口一个词元一个事件，每个事件带两三百字节的外壳，
//     8 MiB 只够两三万个词元，带长思考的模型一次回复就可能碰到，碰到了这一轮就结束。
//   - 原始字节：事件流的全部字节（外壳、保活、看不懂的块、不认识的字段都算），单次 128 MiB 兜底，
//     挡住「内容不涨、外壳一直来」的无限增长；不流式的整段响应另外受 maxBody（32 MiB）限制。
//     128 MiB 的来历：逐词元推送时每个事件的外壳一般两三百字节，带内容过滤标注的（Azure）五六百字节，
//     按 600 字节算也能收二十多万个词元，比现在各家模型一次输出的上限（十几万）还多一截。
//     事件数同理放宽到 1 Mi 个，是十几万词元的好几倍。
//
// 两层都不依赖服务商遵守 max_tokens。超限停止，不自动重试，也不执行没收完的工具调用。
const (
	maxResponseBytes     = 8 << 20   // 单次回复的内容
	maxTurnResponseBytes = 32 << 20  // 一轮的内容合计
	maxResponseRawBytes  = 128 << 20 // 单次响应的原始字节（流式）；不流式时取它和 maxBody 中小的那个
	maxReplyTools        = 128
	maxReplyBlocks       = 1024
	maxSSEEvents         = 1 << 20
)

var ErrResponseTooLarge = errors.New("AI 回复内容过多，已停止接收，请缩小问题范围后重试")

type responseBudgetKey struct{}

// responseBudget 一轮对话共用的内容额度（字节）。
type responseBudget struct {
	mu   sync.Mutex
	left int64
}

// WithResponseBudget 为一轮对话的多次 Chat 共用内容额度。重复调用不重置，
// 因而工具往返、重试、服务商回退都算在同一轮里面。
func WithResponseBudget(ctx context.Context) context.Context {
	return ResumeResponseBudget(ctx, 0)
}

// ResumeResponseBudget 等待人工确认后继续同一轮时，沿用已经消耗的额度。
// received 可随待确认状态存盘；旧会话未记录时按零处理。旧版本存的是原始字节数，比内容字节多，沿用只会更保守。
func ResumeResponseBudget(ctx context.Context, received int64) context.Context {
	if _, ok := ctx.Value(responseBudgetKey{}).(*responseBudget); ok {
		return ctx
	}
	used := min(max(received, 0), int64(maxTurnResponseBytes))
	return context.WithValue(ctx, responseBudgetKey{}, &responseBudget{left: maxTurnResponseBytes - used})
}

// ResponseBytes 这一轮已经收到的内容字节数，用于跨确认卡片延续额度。
func ResponseBytes(ctx context.Context) int64 {
	b, _ := ctx.Value(responseBudgetKey{}).(*responseBudget)
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(maxTurnResponseBytes) - max(b.left, 0)
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

// responseReader 单次响应的原始字节上限。
type responseReader struct {
	r    io.Reader
	left int64
}

// rawReader 给响应体套上原始字节上限：流式是 rawLimit；不流式时整段要读进内存，再受 maxBody 限制，
// 取两者中小的那个，超了一样报 ErrResponseTooLarge（不然 readAll 报的「响应太大」会被当成网络错误重试）。
func (c *Client) rawReader(r io.Reader) io.Reader {
	limit := c.rawLimit
	if !c.cfg.Stream {
		limit = min(limit, maxBody)
	}
	return &responseReader{r: r, left: limit}
}

func (r *responseReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.left < 0 {
		return 0, ErrResponseTooLarge
	}
	// 多读一个字节分清「正好到上限」和超限；不把超限块交给解析器或回调。
	if int64(len(p)) > r.left+1 {
		p = p[:r.left+1]
	}
	n, err := r.r.Read(p)
	r.left -= int64(n)
	if r.left < 0 {
		return 0, ErrResponseTooLarge
	}
	return n, err
}

// contentMeter 一次响应的内容记账：left 是这次回复还能收多少，turn 是这一轮共用的额度（没有时只看单次）。
type contentMeter struct {
	left int64
	turn *responseBudget
}

func (c *Client) contentMeter(ctx context.Context) *contentMeter {
	b, _ := ctx.Value(responseBudgetKey{}).(*responseBudget)
	return &contentMeter{left: c.responseLimit, turn: b}
}

// take 记入解码后的 n 个字节。超出单次或整轮的额度时返回 false：调用方不再把这一块交给累加器和回调，停止接收。
// 正好用到额度为止不算超；整轮超了，额度记成用完，同一轮之后的请求直接拒绝。nil 不记账（不流式时整段先记过了）。
func (m *contentMeter) take(n int) bool {
	if m == nil {
		return true
	}
	if m.left < 0 {
		return false
	}
	if n <= 0 {
		return true
	}
	if int64(n) > m.left {
		m.left = -1
		return false
	}
	if m.turn != nil {
		m.turn.mu.Lock()
		over := int64(n) > m.turn.left
		m.turn.left -= int64(n)
		m.turn.mu.Unlock()
		if over {
			m.left = -1
			return false
		}
	}
	m.left -= int64(n)
	return true
}

// over 这次回复的内容超限了（区别于工具个数、内容块个数超限：那两种照旧按原来的方式返回）。
func (m *contentMeter) over() bool { return m != nil && m.left < 0 }
