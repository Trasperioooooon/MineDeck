package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// OpenAI 兼容协议（Chat Completions）。各家的细节不一样，这里逐家兼容（§4.3）：
//   - 流式请求带 stream_options.include_usage，用量在最后一块（choices 为空）；Kimi 放在 choices[0].usage，两处都读；
//   - 缓存命中：prompt_tokens_details.cached_tokens（OpenAI、通义、智谱），prompt_cache_hit_tokens / miss（DeepSeek），
//     usage.cached_tokens（Kimi）；思考：completion_tokens_details.reasoning_tokens；
//   - 思考内容的字段叫 reasoning_content（DeepSeek、通义、Kimi、智谱）或 reasoning（Ollama）；
//   - 工具参数分成很多小块到达，按 index 拼起来，拼完整体解析；智谱、Ollama 一次给完整个调用。

func (c *Client) openaiBody(conv *Conversation) []byte {
	c.mu.Lock()
	noUsage := c.noUsage
	c.mu.Unlock()
	var b bytes.Buffer
	b.WriteString(`{"model":`)
	b.Write(mustJSON(c.cfg.Model))
	if len(conv.toolRaw) > 0 {
		b.WriteString(`,"tools":`)
		b.Write(conv.toolRaw)
	}
	b.WriteString(`,"messages":[`)
	n := 0
	if len(conv.sysRaw) > 0 {
		b.Write(conv.sysRaw)
		n++
	}
	for _, m := range conv.msgs {
		if n > 0 {
			b.WriteByte(',')
		}
		b.Write(m)
		n++
	}
	b.WriteByte(']')
	// 会变的参数放在历史后面
	if c.cfg.Stream {
		b.WriteString(`,"stream":true`)
		if !noUsage {
			b.WriteString(`,"stream_options":{"include_usage":true}`)
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}

func (c *Client) openaiHeader() http.Header {
	h := http.Header{}
	if c.cfg.Key != "" {
		h.Set("Authorization", "Bearer "+c.cfg.Key)
	}
	if c.cfg.Stream {
		h.Set("Accept", "text/event-stream")
	}
	return h
}

// oaiUsage 各家用量的写法合在一起。
type oaiUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens        int64 `json:"cached_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_input_tokens"` // 通义的显式缓存
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`  // DeepSeek
	PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"` // DeepSeek
	CachedTokens          int64  `json:"cached_tokens"`            // Kimi
}

func (u *oaiUsage) norm() Usage {
	var out Usage
	var cached, created int64
	if d := u.PromptTokensDetails; d != nil {
		cached, created = d.CachedTokens, d.CacheCreationTokens
	}
	cached = max(cached, u.CachedTokens)
	if u.PromptCacheHitTokens != nil {
		cached = max(cached, *u.PromptCacheHitTokens)
	}
	out.CacheRead, out.CacheWrite = cached, created
	if u.PromptCacheMissTokens != nil {
		out.Input = *u.PromptCacheMissTokens
	} else {
		out.Input = max(0, u.PromptTokens-cached-created)
	}
	out.Output = u.CompletionTokens
	if d := u.CompletionTokensDetails; d != nil {
		out.Reasoning = d.ReasoningTokens
	}
	return out
}

// rawText 取 JSON 字符串的值；null、缺失或不是字符串都算没有。
func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || raw[0] != '"' {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

type oaiFunc struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type oaiToolCall struct {
	Index    *int    `json:"index"`
	ID       string  `json:"id"`
	Function oaiFunc `json:"function"`
}

// oaiMsg 非流式的 message 和流式的 delta 写法一样。
type oaiMsg struct {
	Content          json.RawMessage `json:"content"`
	ReasoningContent json.RawMessage `json:"reasoning_content"`
	Reasoning        json.RawMessage `json:"reasoning"`
	ToolCalls        []oaiToolCall   `json:"tool_calls"`
}

type oaiChoice struct {
	Message      *oaiMsg   `json:"message"`
	Delta        *oaiMsg   `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
	Usage        *oaiUsage `json:"usage"` // Kimi
}

type oaiResponse struct {
	Model   string          `json:"model"`
	Choices []oaiChoice     `json:"choices"`
	Usage   *oaiUsage       `json:"usage"`
	Error   json.RawMessage `json:"error"`
}

// oaiCall 拼到一半的工具调用。
type oaiCall struct {
	id, name string
	args     strings.Builder
}

// oaiAcc 把一次回复拼起来（流式一块一块地加，非流式一次加完）。
type oaiAcc struct {
	c         *Client
	emit      func(Event)
	model     string
	text      strings.Builder
	reasoning strings.Builder
	calls     []*oaiCall
	byIndex   map[int]*oaiCall
	finish    string
	usage     *oaiUsage
}

func (a *oaiAcc) addCalls(tcs []oaiToolCall) {
	for i, tc := range tcs {
		idx := i
		if tc.Index != nil {
			idx = *tc.Index
		}
		cur := a.byIndex[idx]
		if cur == nil || (tc.ID != "" && cur.id != "" && tc.ID != cur.id) {
			cur = &oaiCall{id: tc.ID, name: tc.Function.Name}
			a.byIndex[idx] = cur
			a.calls = append(a.calls, cur)
			a.emit(Event{Type: EvToolStart, Index: len(a.calls) - 1, Tool: &ToolCall{ID: cur.id, Name: cur.name}})
		} else {
			if cur.id == "" {
				cur.id = tc.ID
			}
			if cur.name == "" {
				cur.name = tc.Function.Name
			}
		}
		frag := rawText(tc.Function.Arguments)
		if frag == "" && len(tc.Function.Arguments) > 0 && tc.Function.Arguments[0] == '{' {
			frag = string(tc.Function.Arguments) // 有的服务商直接给对象
		}
		if frag != "" {
			cur.args.WriteString(frag)
			a.emit(Event{Type: EvToolDelta, Index: a.indexOf(cur), Text: frag})
		}
	}
}

func (a *oaiAcc) indexOf(call *oaiCall) int {
	for i, x := range a.calls {
		if x == call {
			return i
		}
	}
	return 0
}

func (a *oaiAcc) addChoice(ch oaiChoice) {
	d := ch.Delta
	if ch.Message != nil {
		d = ch.Message // 非流式：整条消息当作一块
	}
	if d != nil {
		if s := rawText(d.ReasoningContent) + rawText(d.Reasoning); s != "" {
			a.reasoning.WriteString(s)
			a.emit(Event{Type: EvReasoning, Text: s})
		}
		if s := rawText(d.Content); s != "" {
			a.text.WriteString(s)
			a.emit(Event{Type: EvText, Text: s})
		}
		a.addCalls(d.ToolCalls)
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		a.finish = *ch.FinishReason
	}
	if ch.Usage != nil {
		a.setUsage(ch.Usage)
	}
}

func (a *oaiAcc) setUsage(u *oaiUsage) {
	a.usage = u
	n := u.norm()
	a.emit(Event{Type: EvUsage, Usage: &n})
}

// parseArgs 工具参数整体解析：要是一个 JSON 对象（空串当作 {}）。
func parseArgs(s string) (json.RawMessage, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return json.RawMessage(`{}`), ""
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return json.RawMessage(`{}`), "参数不是合法的 JSON：" + err.Error()
	}
	if dec.More() {
		return json.RawMessage(`{}`), "参数不是合法的 JSON：对象后面还有多余内容"
	}
	if _, ok := v.(map[string]any); !ok {
		return json.RawMessage(`{}`), "参数应为 JSON 对象"
	}
	return compactJSON([]byte(s)), ""
}

// result 拼出最后的回复，以及要追加进历史的 assistant 消息。
func (a *oaiAcc) result(partial bool, bodyLen int) *Result {
	r := &Result{proto: OpenAI, Text: a.text.String(), Reasoning: a.reasoning.String(), Model: a.model, StopRaw: a.finish, Partial: partial}
	var calls []map[string]any
	argBytes := 0
	for i, call := range a.calls {
		id := call.id
		if id == "" {
			id = fmt.Sprintf("call_%d", i) // 有的服务商不给编号
		}
		raw := call.args.String()
		argBytes += len(raw)
		args, bad := parseArgs(raw)
		tc := ToolCall{ID: id, Name: call.name, Args: args, Bad: bad}
		if bad != "" {
			tc.Raw = raw
		}
		r.ToolCalls = append(r.ToolCalls, tc)
		r.calls = append(r.calls, id)
		if !partial {
			a.emit(Event{Type: EvToolDone, Index: i, Tool: &tc})
		}
		calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]string{"name": call.name, "arguments": raw}})
	}
	switch a.finish {
	case "stop", "":
		r.Stop = StopEnd
	case "tool_calls", "function_call":
		r.Stop = StopTool
	case "length":
		r.Stop = StopMaxTokens
	case "content_filter", "sensitive":
		r.Stop = StopRefusal
	default:
		r.Stop = StopOther
	}
	if len(r.ToolCalls) > 0 && (r.Stop == StopEnd || r.Stop == StopOther) {
		r.Stop = StopTool // 有的服务商带了工具调用，停止原因却写 stop
	}
	if a.usage != nil {
		r.Usage = a.usage.norm()
	} else {
		// 流中途断开拿不到用量：按字数估算，标上「估」（§4.3）
		r.Usage = Usage{Input: estimateTokens(bodyLen), Output: estimateTokens(len(r.Text) + len(r.Reasoning) + argBytes), Estimated: true}
	}
	if partial || r.Stop == StopRefusal {
		return r
	}
	m := map[string]any{"role": "assistant"}
	if len(calls) > 0 {
		m["tool_calls"] = calls
		if r.Text == "" {
			m["content"] = nil
		} else {
			m["content"] = r.Text
		}
	} else {
		m["content"] = r.Text
	}
	if a.c.cfg.EchoReasoning && r.Reasoning != "" {
		m["reasoning_content"] = r.Reasoning
	}
	if r.Text == "" && len(calls) == 0 {
		return r // 空回复：不能追加
	}
	r.msg = mustJSON(m)
	return r
}

// oaiStreamError 流里的 {"error": …}，按类型折算成状态码。
func (c *Client) oaiStreamError(raw json.RawMessage) *APIError {
	e := c.apiError(500, http.Header{}, []byte(`{"error":`+string(raw)+`}`))
	t := strings.ToLower(e.Type)
	switch {
	case strings.Contains(t, "rate_limit") || strings.Contains(t, "quota"):
		e.Status = 429
	case strings.Contains(t, "invalid_request"):
		e.Status = 400
	case strings.Contains(t, "auth"):
		e.Status = 401
	}
	e.InStream = true
	return e
}

func (c *Client) openaiOnce(ctx context.Context, conv *Conversation, emit func(Event)) (*Result, error) {
	body := c.openaiBody(conv)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var dog *watchdog
	if c.cfg.Stream {
		dog = newWatchdog(c.cfg.IdleTimeout, cancel)
	} else {
		dog = newWatchdog(c.cfg.Timeout, cancel)
	}
	defer dog.stop()
	resp, err := c.post(ctx, c.endpoint("/chat/completions"), body, c.openaiHeader())
	if err != nil {
		if dog.Fired() {
			return nil, &NetError{Err: errors.New("等待响应超时")}
		}
		return nil, err
	}
	defer resp.Body.Close()
	acc := &oaiAcc{c: c, emit: emit, byIndex: map[int]*oaiCall{}}

	if !c.cfg.Stream {
		b, err := readAll(kickReader{resp.Body, dog})
		if err != nil {
			return nil, c.cutErr(ctx, dog, err)
		}
		var r oaiResponse
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("服务商返回的内容看不懂：%w", err)
		}
		if len(r.Error) > 0 && string(r.Error) != "null" {
			return nil, c.oaiStreamError(r.Error)
		}
		if len(r.Choices) == 0 {
			return nil, errors.New("服务商返回的内容看不懂：没有 choices")
		}
		acc.model = r.Model
		acc.addChoice(r.Choices[0])
		if r.Usage != nil {
			acc.setUsage(r.Usage)
		}
		return acc.result(false, len(body)), nil
	}

	sr := newSSEReader(kickReader{resp.Body, dog})
	got := false // 收到过内容（文字、思考、工具调用）
	done := false
	for !done {
		ev, err := sr.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if got {
				return acc.result(true, len(body)), fmt.Errorf("%w：%v", ErrDisconnected, c.cutErr(ctx, dog, err))
			}
			return nil, c.cutErr(ctx, dog, err)
		}
		data := bytes.TrimSpace(ev.Data)
		if len(data) == 0 {
			continue
		}
		if string(data) == "[DONE]" {
			done = true
			break
		}
		var chunk oaiResponse
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue // 看不懂的块跳过
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			e := c.oaiStreamError(chunk.Error)
			if got {
				return acc.result(true, len(body)), fmt.Errorf("%w：%v", ErrDisconnected, e)
			}
			return nil, e
		}
		if chunk.Model != "" {
			acc.model = chunk.Model
		}
		for _, ch := range chunk.Choices {
			acc.addChoice(ch)
			got = got || acc.text.Len() > 0 || acc.reasoning.Len() > 0 || len(acc.calls) > 0
		}
		if chunk.Usage != nil {
			acc.setUsage(chunk.Usage)
		}
	}
	if !done && acc.finish == "" {
		if got {
			return acc.result(true, len(body)), fmt.Errorf("%w：连接被关闭", ErrDisconnected)
		}
		return nil, &NetError{Err: errors.New("连接被关闭，没有收到回复")}
	}
	if acc.finish == "network_error" { // 智谱：推理出错
		if got {
			return acc.result(true, len(body)), fmt.Errorf("%w：服务商推理出错", ErrDisconnected)
		}
		return nil, &APIError{Status: 500, Type: "network_error", Message: "服务商推理出错"}
	}
	return acc.result(false, len(body)), nil
}

// cutErr 读的过程中出错：用户取消、看门狗超时、网络断开分开说。
func (c *Client) cutErr(ctx context.Context, dog *watchdog, err error) error {
	switch {
	case dog.Fired():
		return &NetError{Err: errors.New("长时间没有收到新数据")}
	case ctx.Err() != nil:
		return context.Canceled
	}
	return &NetError{Err: errors.New(scrub(err.Error(), c.cfg.Key))}
}

func (c *Client) openaiModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/models"), nil)
	if err != nil {
		return nil, err
	}
	if c.cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Key)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := readAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, errors.New("服务商返回的模型列表看不懂，请手动填写模型名")
	}
	seen := map[string]bool{}
	var out []Model
	for _, m := range v.Data {
		if m.ID != "" && !seen[m.ID] && len(out) < 2000 {
			seen[m.ID] = true
			out = append(out, Model{ID: m.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
