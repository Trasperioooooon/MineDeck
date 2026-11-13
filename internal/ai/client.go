package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config 一个客户端的配置（由 Service 按设置和预设生成）。
type Config struct {
	Protocol Protocol
	BaseURL  string // 接口地址，用户填的原样（末尾的斜杠也保留，拼路径时不会多出一个斜杠）
	Key      string
	Model    string
	Stream   bool // 流式（回复一个字一个字地推过来）

	MaxTokens int    // Claude 必填的输出上限（含思考），0 = 16000
	Effort    string // Claude 的思考深度 output_config.effort；空 = 不传，用模型的默认值
	Fallback  bool   // Claude 服务端回退（拒答时自动换模型重试，beta）
	Eager     bool   // Claude 流式时给工具打开 eager_input_streaming
	Official  bool   // Claude 官方地址：只发 x-api-key；中转另外再带一个 Authorization: Bearer

	Auth           AuthMode // 鉴权头；空 = 这个协议的默认（OpenAI 兼容是 Bearer，Claude 见 claudeHeader）
	Echo           EchoMode // 见 EchoMode。空 = 不回传
	EchoPadAll     bool     // Echo 为 on 时，没有思考内容的助手消息全都补空串（DeepSeek）；false 只补带工具调用的（MiMo）
	ToolResultName bool     // OpenAI 兼容：tool 消息带上工具名（Kimi）

	// DropReasoning 已经知道对方不认 reasoning_content（之前的请求被拒过）：一开始就不带。
	// OnDropReasoning 这个客户端被拒后改成不带时调一次，让上层记住，下一轮新建的客户端直接不带。
	DropReasoning   bool
	OnDropReasoning func()

	HTTP        *http.Client
	RetryDelay  time.Duration // 重试的基础等待，默认 1 秒（第二次翻倍）
	IdleTimeout time.Duration // 流式时多久没有新数据算断开，默认 60 秒
	Timeout     time.Duration // 不流式时整个请求最长多久，默认 10 分钟
}

const (
	anthropicVersion = "2026-10-21"                      // 写死一个版本；升级时对照官方的版本说明改，并用录好的样本跑一遍测试
	fallbackBeta     = "server-side-fallback-2026-07-01" // fallbacks: "default" 要的 beta 请求头
	maxRetries       = 2                                 // 429 和 5xx 最多重试 2 次（§4.3）
	maxRetryAfter    = 30 * time.Second                  // 服务商要求等更久就不等了，直接报错
	maxBody          = 32 << 20                          // 非流式响应最大 32 MB
)

// Client 一个服务商的客户端。可以并发使用。
type Client struct {
	cfg Config

	mu            sync.Mutex
	eager         bool // 中转不认 eager_input_streaming 时关掉
	fallback      bool // 中转不认服务端回退时关掉
	noUsage       bool // OpenAI 兼容：服务不认 stream_options 时不再要求流末尾带用量
	dropReasoning bool // 对方不认 reasoning_content：这次客户端不再带回
}

// NewClient 检查配置，返回客户端。
func NewClient(cfg Config) (*Client, error) {
	if !cfg.Protocol.valid() {
		return nil, fmt.Errorf("未知的协议：%q", cfg.Protocol)
	}
	if _, err := ParseBaseURL(cfg.BaseURL); err != nil {
		return nil, err
	}
	if cfg.HTTP == nil {
		cfg.HTTP = DefaultHTTPClient()
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 16000
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 60 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Minute
	}
	return &Client{cfg: cfg, eager: cfg.Eager && cfg.Stream, fallback: cfg.Fallback, dropReasoning: cfg.DropReasoning && cfg.Echo == EchoAuto}, nil
}

// DefaultHTTPClient 默认的 HTTP 客户端：连接（含 TLS 握手）15 秒超时，走环境变量里的代理。
// 请求整体不设超时：流式由看门狗判断「多久没有新数据」，非流式另有整体时限。
func DefaultHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 0,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	}}
}

// ParseBaseURL 检查接口地址：http 或 https，有主机名，不带账号密码、查询串和锚点。
func ParseBaseURL(s string) (*url.URL, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("接口地址是空的")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("接口地址要以 http:// 或 https:// 开头")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("接口地址里不能带账号密码、问号或井号后面的部分")
	}
	return u, nil
}

// SameSite 两个接口地址是不是同一个网站：http/https、域名（不分大小写）、端口都一样，默认端口写不写都算一样。
// 已存的 Key 只发给同一个网站；有一个解析不了就算不是。
func SameSite(a, b string) bool {
	sa, sb := siteOf(a), siteOf(b)
	return sa != "" && sa == sb
}

func siteOf(s string) string {
	u, err := ParseBaseURL(s)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if p := u.Port(); p != "" && !(scheme == "https" && p == "443") && !(scheme == "http" && p == "80") {
		host += ":" + p
	}
	return scheme + "://" + host
}

// endpoint 拼出接口路径：去掉地址末尾的斜杠再接上（智谱的地址末尾带斜杠，原样存、拼的时候不重复）。
// Claude 的地址可以写到域名为止，也可以写到 /v1。
func (c *Client) endpoint(path string) string {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.BaseURL), "/")
	if c.cfg.Protocol == Anthropic && !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

// Protocol 协议。
func (c *Client) Protocol() Protocol { return c.cfg.Protocol }

// Model 模型名。
func (c *Client) Model() string { return c.cfg.Model }

// Features 当前 Claude 的两个可选功能开没开（中转不认时会被关掉）。
func (c *Client) Features() (eager, fallback bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.eager, c.fallback
}

// NewConversation 开一个新会话：系统提示和工具清单在这里定下来，之后不增不减。
func (c *Client) NewConversation(system string, tools []Tool) (*Conversation, error) {
	eager, _ := c.Features()
	return newConversation(c.cfg.Protocol, system, tools, eager, c.cfg.ToolResultName)
}

// Chat 发一次请求，返回这次的回复；onEvent 收流式事件（不流式时只收到用量；可以为 nil）。
// 429、5xx、网络错误在还没有任何输出时最多重试 2 次；已经开始输出以后出错不重试，
// 返回不完整的 Result（Partial，用量可能是估算的）和错误。
func (c *Client) Chat(ctx context.Context, conv *Conversation, onEvent func(Event)) (*Result, error) {
	if conv.proto != c.cfg.Protocol {
		return nil, errors.New("会话和客户端的协议对不上（换了服务商要开新会话）")
	}
	if strings.TrimSpace(c.cfg.Model) == "" {
		return nil, errors.New("还没选模型")
	}
	if k := conv.last(); k != kindUser && k != kindToolResults {
		return nil, errors.New("会话里没有等着回复的消息")
	}
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	for attempt := 0; ; attempt++ {
		var res *Result
		var err error
		out := false // 有没有交出过内容事件（交出过就不能重试了）
		emit := func(e Event) {
			if e.Type != EvUsage {
				out = true
			}
			onEvent(e)
		}
		if c.cfg.Protocol == Anthropic {
			res, err = c.claudeOnce(ctx, conv, emit)
		} else {
			res, err = c.openaiOnce(ctx, conv, emit)
		}
		if err == nil {
			return res, nil
		}
		if res != nil && res.Partial {
			return res, err
		}
		if c.downgrade(conv, err) {
			attempt-- // 换个写法重发，不算重试次数
			continue
		}
		wait, ok := c.retryWait(attempt, err)
		if !ok || out || ctx.Err() != nil {
			return res, err
		}
		onEvent(Event{Type: EvRetry, Attempt: attempt + 1, Err: err.Error()})
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// retryWait 这个错误能不能重试、等多久。服务商给了 retry-after 就照它的来（太久就不等了）。
func (c *Client) retryWait(attempt int, err error) (time.Duration, bool) {
	if attempt >= maxRetries {
		return 0, false
	}
	wait := c.cfg.RetryDelay << attempt
	var ae *APIError
	var ne *NetError
	switch {
	case errors.As(err, &ae):
		if !ae.Retryable() {
			return 0, false
		}
		if ae.RetryAfter > maxRetryAfter {
			return 0, false
		}
		if ae.RetryAfter > wait {
			wait = ae.RetryAfter
		}
	case errors.As(err, &ne):
	default:
		return 0, false
	}
	return wait, true
}

// downgrade 服务不认某个可选参数时（第一次请求就报 400），关掉再发一次：Claude 中转不认 eager_input_streaming
// 或服务端回退；OpenAI 兼容的服务不认 stream_options。Claude 的工具清单只在会话还没有任何回复时才重编
// （之后改动会让思考块失效）。
func (c *Client) downgrade(conv *Conversation, err error) bool {
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		return false
	}
	msg := strings.ToLower(ae.Message + " " + ae.Type)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.Protocol == OpenAI {
		if c.cfg.Stream && !c.noUsage && strings.Contains(msg, "stream_options") {
			c.noUsage = true
			return true
		}
		// 带了思考字段对方不认：去掉再发一次。已经去掉过就不再试，避免来回。
		// 只对 auto：on 是文档写明必须传回的服务商（DeepSeek、MiMo），去掉只会换来另一个 400，还把原来那句更有用的报错盖掉。
		if !c.dropReasoning && c.cfg.Echo == EchoAuto && reasoningRejected(msg) {
			c.dropReasoning = true
			if cb := c.cfg.OnDropReasoning; cb != nil {
				cb() // 只记一个结论，不回调客户端，持锁调用不会死锁
			}
			return true
		}
		return false
	}
	switch {
	case c.fallback && (strings.Contains(msg, "fallback") || strings.Contains(msg, "anthropic-beta")):
		c.fallback = false
		return true
	case strings.Contains(msg, "eager_input_streaming") && conv.eager && !conv.hasReply():
		c.eager = false
		conv.eager = false
		conv.encodeFixed()
		return true
	}
	return false
}

// reasoningRejected 400 的原文是在拒绝 reasoning_content 这个字段，而不是在要求把它传回。
// 字段名要完整出现（reasoning_content 或 reasoning），reasoning_effort 之类的参数被拒不算。
// Groq 的原文是「property 'reasoning_content' is unsupported」；DeepSeek 要求传回时是「… must be passed back to the API」，不会命中。
func reasoningRejected(msg string) bool {
	if !reasoningField.MatchString(msg) {
		return false
	}
	for _, s := range []string{"unknown", "unexpected", "unrecognized", "extra", "not support", "unsupported", "not permitted", "does not support", "invalid field"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// post 发一个 JSON 请求；返回 2xx 的响应（调用方关闭 Body），其余的读出错误信息转成 *APIError。
func (c *Client) post(ctx context.Context, url string, body []byte, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, &NetError{Err: errors.New(scrub(err.Error(), c.cfg.Key))}
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, c.apiError(resp.StatusCode, resp.Header, b)
	}
	return resp, nil
}

// apiError 从错误响应里取出类型和说明。两种协议的写法：
// Claude {"type":"error","error":{"type":"…","message":"…"}}；OpenAI 兼容 {"error":{"message":"…","type":"…","code":…}}，
// 有的服务商直接给 {"message":"…"} 或纯文本。
func (c *Client) apiError(status int, h http.Header, body []byte) *APIError {
	e := &APIError{Status: status}
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &v) == nil {
		var inner struct {
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Code    json.RawMessage `json:"code"`
		}
		if json.Unmarshal(v.Error, &inner) == nil {
			e.Type, e.Message = inner.Type, inner.Message
			if e.Type == "" && len(inner.Code) > 0 {
				e.Type = strings.Trim(string(inner.Code), `"`)
			}
		} else if s := ""; json.Unmarshal(v.Error, &s) == nil {
			e.Message = s
		}
		if e.Message == "" {
			e.Message = v.Message
		}
	}
	if e.Message == "" && e.Type == "" {
		e.Message = strings.TrimSpace(string(body))
	}
	e.Message = scrub(truncate(e.Message, 500), c.cfg.Key)
	if ra := h.Get("Retry-After"); ra != "" {
		if s, err := strconv.ParseFloat(ra, 64); err == nil && s >= 0 {
			e.RetryAfter = time.Duration(s * float64(time.Second))
		}
	}
	return e
}

// streamTypeStatus 流里 error 事件的类型折算成 HTTP 状态码（官方说流里的 overloaded_error 对应非流式时的 529）。
func streamTypeStatus(t string) int {
	switch t {
	case "invalid_request_error":
		return 400
	case "authentication_error":
		return 401
	case "billing_error":
		return 402
	case "permission_error":
		return 403
	case "not_found_error":
		return 404
	case "request_too_large":
		return 413
	case "rate_limit_error":
		return 429
	case "overloaded_error":
		return 529
	case "timeout_error":
		return 504
	}
	return 500 // api_error 和不认识的类型
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// estimateTokens 拿不到用量时按字数粗估：每 3 个字节算 1 个 token（中文一个字大约 1 个 token，
// 英文会多算一些），宁可估多，额度不至于被突破。
func estimateTokens(n int) int64 {
	if n <= 0 {
		return 0
	}
	return int64((n + 2) / 3)
}

// readAll 读非流式的响应体（有上限）。
func readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, errors.New("响应太大")
	}
	return b, nil
}

// ListModels 取模型列表。有的服务商不提供，这时让用户手填。
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if c.cfg.Protocol == Anthropic {
		return c.claudeModels(ctx)
	}
	return c.openaiModels(ctx)
}
