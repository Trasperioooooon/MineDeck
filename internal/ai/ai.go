// Package ai AI 接入层（设计文档 §4）：和服务商无关，只认两种协议——OpenAI 兼容（Chat Completions）和
// Claude 原生（Messages API）。两种都用标准库自己发 HTTP，不用任何服务商的 SDK。
//
// 能力：普通对话、流式、工具调用（两种协议的工具格式统一成这里的 Tool / ToolCall / ToolResult）、用量统计、
// 模型列表；另有 7 个服务商预设、费用估算（运行时取 LiteLLM 的公开价格）、按功能记账和每日额度、设置与测试连接。
//
// 写 Claude 客户端守的规矩（§4.3）：会话历史只追加（Conversation 没有改写的方法，每条消息追加时编好原文，
// 之后原样拼进请求）；思考块和服务端回退留下的块原样带回；一个会话里系统提示和工具清单固定不变；
// 流里的 error 事件在 message_start 之前按对应的错误重试，开始输出以后当作断开。
//
// 这个包不引用 Windows API，Linux 上可以直接测试。Key 的加密存放由调用方注入（Windows 上是 secret 包的 DPAPI）。
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Protocol 接口协议。
type Protocol string

const (
	OpenAI    Protocol = "openai"    // OpenAI 兼容（Chat Completions）：DeepSeek、通义、Kimi、智谱、OpenAI、Ollama 和各种中转
	Anthropic Protocol = "anthropic" // Claude 原生（Messages API）
)

func (p Protocol) valid() bool { return p == OpenAI || p == Anthropic }

// Tool 给 AI 的一个工具。名字只能是字母、数字、下划线和横线（操作层用 ops.ToolName 把点换成下划线），
// Schema 是参数的 JSON Schema（object），两种协议都原样用它。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
}

// ToolCall AI 发起的一次工具调用。Args 是解析好的参数对象（原文）；参数不是合法的 JSON 对象时
// Bad 写明原因、Raw 是收到的原文，这时不要执行，回一个 IsError 的结果让它重来。
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
	Raw  string          `json:"raw,omitempty"`
	Bad  string          `json:"bad,omitempty"`
}

// ToolResult 交还给 AI 的工具结果，ID 对上 ToolCall.ID。
type ToolResult struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	IsError bool   `json:"isError,omitempty"`
}

// Usage 一次请求的用量，统一成五项（有就记）。Input 不含缓存命中和缓存写入；Output 含思考，
// Reasoning 是其中的思考部分（单独列出，算钱时不重复算）。
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead,omitempty"`
	CacheWrite int64 `json:"cacheWrite,omitempty"`
	Reasoning  int64 `json:"reasoning,omitempty"`
	Estimated  bool  `json:"estimated,omitempty"` // 流中途断开、拿不到用量，按字数估算的
}

// Total 这次一共处理了多少 token（每日额度按它算）。
func (u Usage) Total() int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// Prompt 输入一共多少 token（按输入长度分档计价时用）。
func (u Usage) Prompt() int64 { return u.Input + u.CacheRead + u.CacheWrite }

// Add 累加。
func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheWrite += o.CacheWrite
	u.Reasoning += o.Reasoning
	u.Estimated = u.Estimated || o.Estimated
}

// Attempt Claude 服务端回退时的一次尝试（usage.iterations 的一项）：哪个模型跑的、用了多少。
// 算钱时每次尝试按各自模型的价格算（§4.4）。
type Attempt struct {
	Model    string `json:"model"`
	Fallback bool   `json:"fallback,omitempty"` // 回退后由它给出回复
	Usage    Usage  `json:"usage"`
}

// StopReason 统一后的停止原因。
type StopReason string

const (
	StopEnd       StopReason = "end"        // 正常说完
	StopTool      StopReason = "tool_use"   // 要调用工具
	StopMaxTokens StopReason = "max_tokens" // 输出到上限被截断（这时的工具调用参数可能不完整，不要执行）
	StopRefusal   StopReason = "refusal"    // 拒答：Claude 的安全分类器，或国内服务商的内容审核
	StopPause     StopReason = "pause"      // Claude 服务端工具循环暂停（我们不用服务端工具，一般见不到）
	StopOther     StopReason = "other"
)

// Refusal Claude 拒答时的说明（stop_details）。Category 可能为空。
type Refusal struct {
	Category    string `json:"category,omitempty"`
	Explanation string `json:"explanation,omitempty"`
}

// EventType 流式事件的类型。
type EventType string

const (
	EvText      EventType = "text"       // 文字片段
	EvReasoning EventType = "reasoning"  // 思考片段（有的服务商会给；Claude 默认不显示思考原文）
	EvToolStart EventType = "tool_start" // 开始一个工具调用（Tool 里有编号和名字）
	EvToolDelta EventType = "tool_delta" // 工具参数的片段
	EvToolDone  EventType = "tool_done"  // 一个工具调用的参数收齐了
	EvUsage     EventType = "usage"      // 用量（可能来好几次，后面的覆盖前面的）
	EvFallback  EventType = "fallback"   // Claude 服务端回退：换成 Model 继续
	EvRetry     EventType = "retry"      // 出错了，等一下重试（还没有任何输出时才会重试）
)

// Event 一个流式事件。Index 是这次回复里第几个工具调用（从 0 起），两种协议口径一样。
type Event struct {
	Type    EventType `json:"type"`
	Text    string    `json:"text,omitempty"`
	Index   int       `json:"index,omitempty"`
	Tool    *ToolCall `json:"tool,omitempty"`
	Usage   *Usage    `json:"usage,omitempty"`
	Model   string    `json:"model,omitempty"`
	Attempt int       `json:"attempt,omitempty"` // 重试：第几次重试（从 1 起）
	Err     string    `json:"err,omitempty"`     // 重试：原因
}

// Result 一次回复。成功时用 Conversation.AddResult 追加进历史，再执行 ToolCalls、用 AddToolResults 交回结果。
// Chat 出错但已经有了输出（中途断开、用户停止）时也会返回一个不完整的 Result：只用来记账，不能追加进历史。
type Result struct {
	Text      string     `json:"text"`
	Reasoning string     `json:"reasoning,omitempty"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	Stop      StopReason `json:"stop"`
	StopRaw   string     `json:"stopRaw,omitempty"` // 服务商原始的 stop_reason / finish_reason
	Refusal   *Refusal   `json:"refusal,omitempty"`
	Model     string     `json:"model,omitempty"` // 实际给出回复的模型（Claude 服务端回退后是回退的那个）
	Usage     Usage      `json:"usage"`
	Attempts  []Attempt  `json:"attempts,omitempty"` // Claude 服务端回退时每次尝试的模型和用量；没有回退时为空
	Partial   bool       `json:"partial,omitempty"`  // 不完整（出错时返回的）

	proto Protocol
	msg   json.RawMessage // 要追加进历史的原生消息
	calls []string        // 要交回结果的工具调用编号
}

// Model 模型列表里的一项。
type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	MaxOutput int    `json:"maxOutput,omitempty"` // 输出上限（Claude 的模型列表会给）
}

// ---------- 错误 ----------

// ErrDisconnected 回复中途断开（开始输出以后网络断了、流里来了 error 事件、长时间没有新数据）。不重试。
var ErrDisconnected = errors.New("回复中途断开")

// APIError 服务商返回的错误：HTTP 不是 2xx，或者流里的 error 事件（按错误类型折算成对应的状态码，
// 比如 overloaded_error 是 529）。
type APIError struct {
	Status     int           `json:"status"`
	Type       string        `json:"type,omitempty"`
	Message    string        `json:"message,omitempty"`
	RetryAfter time.Duration `json:"-"`
	InStream   bool          `json:"-"` // 来自流里的 error 事件
}

func (e *APIError) Error() string {
	var b strings.Builder
	if h := statusHint(e.Status); h != "" {
		b.WriteString(h)
		b.WriteString("（")
	} else {
		b.WriteString("服务商返回错误（")
	}
	fmt.Fprintf(&b, "HTTP %d", e.Status)
	if e.Type != "" {
		b.WriteString("，" + e.Type)
	}
	b.WriteString("）")
	if e.Message != "" {
		b.WriteString("：" + e.Message)
	}
	return b.String()
}

// Retryable 429 和 5xx（含 Claude 的 529 过载）可以重试；额度、余额用完的 429 重试也没用，不重试。
func (e *APIError) Retryable() bool { return (e.Status == 429 && !e.quotaGone()) || e.Status >= 500 }

// lowerText 原文和错误类型合在一起、转小写，给下面按关键词分类用（错误类型里常有 insufficient_quota、context_length_exceeded）。
func (e *APIError) lowerText() string { return strings.ToLower(e.Message + " " + e.Type) }

// 关键词表按各家的真实报错原文定（出处见 docs/评审-PR19-助手渲染与兼容服务商.md 第四节第 7 条）。改这几张表时同步改 compat_test.go。
var (
	// 额度或余额用完：OpenAI 的 insufficient_quota、「Credit balance exhausted」，Kimi 的 exceeded_current_quota_error，
	// 智谱 1113「您的账户已欠费」，DeepSeek 402「Insufficient Balance」（402 另有 statusHint）
	quotaWords = []string{"quota", "insufficient", "balance", "credit", "exhaust", "billing", "额度", "余额", "欠费"}
	// 上下文超长：OpenAI「maximum context length」/ context_length_exceeded，Claude「prompt is too long」，
	// Kimi「Input token length too long」，智谱 1261「Prompt 超长」
	contextWords = []string{"context length", "context_length", "maximum context", "context window", "too long", "too many tokens", "超长", "上下文长度"}
	// reasoningField 原文说的是 reasoning_content 或 reasoning 这个字段，不是 reasoning_effort、reasoning_tokens 之类的参数
	reasoningField = regexp.MustCompile(`\breasoning(?:_content)?\b`)
)

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// quotaGone 429 说的是额度或余额用完了（不是请求太频繁）。
func (e *APIError) quotaGone() bool { return e.Status == 429 && containsAny(e.lowerText(), quotaWords) }

// Hint 给人看的那一句。按状态码区分，再按服务商原文里的关键词说得更具体一点；
// 仍是中文短句，不把英文原文、状态码或 Key 拼进来（界面正文只接这一句，完整原文在 Error()，复制时看得到）。
// Key 和地址对不上（MiMo 的按量 Key 配了 Token Plan 地址）不靠猜原文：测试连接时由服务层用 KeyWarning 换掉 401 的这一句。
func (e *APIError) Hint() string {
	msg := e.lowerText()
	switch e.Status {
	case 400:
		if containsAny(msg, contextWords) {
			return "对话太长，超过了模型能读的长度。开一个新会话再问"
		}
		if reasoningField.MatchString(msg) {
			return "请求被拒绝：多轮对话要把思考内容原样传回，或者这个服务商不接受思考字段"
		}
		if strings.Contains(msg, "model") {
			return "请求被拒绝，多半是模型名不对，或者这个模型不支持这次请求里的参数"
		}
		return "请求被服务商拒绝"
	case 403:
		if containsAny(msg, []string{"country", "region", "location", "地区"}) {
			return "没有权限：多半是地区限制"
		}
		return "没有权限（可能是地区限制，或者账号没开通这个模型）"
	case 404:
		if strings.Contains(msg, "model") {
			return "模型名不对，或者这个地址上没有这个模型"
		}
		return "接口地址或模型名不对"
	case 429:
		if e.quotaGone() {
			return "服务商那边的额度用完了"
		}
		return "请求太频繁，等一会儿再试"
	default:
		return statusHint(e.Status)
	}
}

func statusHint(code int) string {
	switch {
	case code == 400:
		return "请求被服务商拒绝"
	case code == 401:
		return "Key 不对或已失效"
	case code == 402:
		return "账户余额不足"
	case code == 403:
		return "没有权限（可能是地区限制，或者账号没开通这个模型）"
	case code == 404:
		return "接口地址或模型名不对"
	case code == 413:
		return "请求太大"
	case code == 429:
		return "请求太频繁，或者服务商那边的额度用完了" // Hint() 会再分成「太频繁」和「额度用完」
	case code == 529:
		return "服务商繁忙"
	case code >= 500:
		return "服务商暂时出了问题"
	}
	return ""
}

// NetError 连不上服务商（还没收到响应）。
type NetError struct{ Err error }

func (e *NetError) Error() string { return "连不上服务商：" + e.Err.Error() }
func (e *NetError) Unwrap() error { return e.Err }

// Hint 见 APIError.Hint。
func (e *NetError) Hint() string { return "连不上服务商，检查网络、代理和接口地址" }

// hintOf 报错链上第一个带 Hint() 的错误给的那一句；没有就是空串。
func hintOf(err error) string {
	var h interface{ Hint() string }
	if errors.As(err, &h) {
		return h.Hint()
	}
	return ""
}

// keyLike 报错里出现的另一把 Key（不一定是我们发出去的那把）。前面要是单词边界：
// 不然 http-proxy、task-manager 这类正常单词会被抹掉一截。
var keyLike = regexp.MustCompile(`\b(?:sk|tp|ttp|ak)-[A-Za-z0-9_\-]{8,}`)

// scrub 把错误信息里的 Key 换成 ***（有的服务商会在报错里回显 Key）。
func scrub(s, key string) string {
	if len(key) >= 6 {
		s = strings.ReplaceAll(s, key, "***")
	}
	return keyLike.ReplaceAllString(s, "***")
}
