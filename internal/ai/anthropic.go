package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Claude 原生协议（Messages API）。请求头 x-api-key、anthropic-version；不发 temperature 等采样参数（这个版本起会 400）；
// 不强制工具调用（用默认的 auto）；不传 thinking（当前模型默认就开着自适应思考，老模型传了反而报错）。
// 流式事件：message_start（带输入用量）→ content_block_start / content_block_delta / content_block_stop
// → message_delta（停止原因、累计的输出用量）→ message_stop；ping 和不认识的事件跳过；
// error 事件在 message_start 之前出现的按对应的错误处理（可以重试），之后出现的当作断开。

func (c *Client) claudeBody(conv *Conversation) []byte {
	c.mu.Lock()
	fallback := c.fallback
	c.mu.Unlock()
	var b bytes.Buffer
	b.WriteString(`{"model":`)
	b.Write(mustJSON(c.cfg.Model))
	fmt.Fprintf(&b, `,"max_tokens":%d`, c.cfg.MaxTokens)
	if len(conv.sysRaw) > 0 {
		b.WriteString(`,"system":`)
		b.Write(conv.sysRaw)
	}
	if len(conv.toolRaw) > 0 {
		b.WriteString(`,"tools":`)
		b.Write(conv.toolRaw)
	}
	b.WriteString(`,"messages":[`)
	for i, m := range conv.msgs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(m)
	}
	b.WriteByte(']')
	// 会变的参数放在历史后面：改它们不影响历史前缀
	if c.cfg.Stream {
		b.WriteString(`,"stream":true`)
	}
	if c.cfg.Effort != "" {
		b.WriteString(`,"output_config":{"effort":`)
		b.Write(mustJSON(c.cfg.Effort))
		b.WriteByte('}')
	}
	if fallback {
		b.WriteString(`,"fallbacks":"default"`) // 拒答时服务端按拒答类别自动换模型（beta）
	}
	b.WriteByte('}')
	return b.Bytes()
}

func (c *Client) claudeHeader(withBeta bool) http.Header {
	h := http.Header{}
	h.Set("x-api-key", c.cfg.Key)
	if !c.cfg.Official && c.cfg.Key != "" {
		h.Set("Authorization", "Bearer "+c.cfg.Key) // 中转有的只认这个（推断）
	}
	h.Set("anthropic-version", anthropicVersion)
	if withBeta {
		c.mu.Lock()
		if c.fallback {
			h.Set("anthropic-beta", fallbackBeta)
		}
		c.mu.Unlock()
	}
	if c.cfg.Stream {
		h.Set("Accept", "text/event-stream")
	}
	return h
}

type claudeIteration struct {
	Type          string `json:"type"` // message：拒答了的那次；fallback_message：回退后给出回复的那次
	Model         string `json:"model"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	CacheCreation int64  `json:"cache_creation_input_tokens"`
	CacheRead     int64  `json:"cache_read_input_tokens"`
}

// claudeUsage message_start 和 message_delta 里的 usage；message_delta 的是累计值，出现的字段覆盖前面的。
type claudeUsage struct {
	InputTokens   *int64 `json:"input_tokens"`
	OutputTokens  *int64 `json:"output_tokens"`
	CacheCreation *int64 `json:"cache_creation_input_tokens"`
	CacheRead     *int64 `json:"cache_read_input_tokens"`
	OutputDetails *struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
	Iterations []claudeIteration `json:"iterations"`
}

func (u *claudeUsage) merge(o *claudeUsage) {
	if o == nil {
		return
	}
	if o.InputTokens != nil {
		u.InputTokens = o.InputTokens
	}
	if o.OutputTokens != nil {
		u.OutputTokens = o.OutputTokens
	}
	if o.CacheCreation != nil {
		u.CacheCreation = o.CacheCreation
	}
	if o.CacheRead != nil {
		u.CacheRead = o.CacheRead
	}
	if o.OutputDetails != nil {
		u.OutputDetails = o.OutputDetails
	}
	if len(o.Iterations) > 0 {
		u.Iterations = o.Iterations
	}
}

func val(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// norm 统一口径。发生过服务端回退时，顶层只算给出回复的那次，usage.iterations 才是每次尝试的实数：
// 这时按 iterations 逐次累加（额度偏保守），并列出每次的模型（算钱时各按各的价格）。
func (u *claudeUsage) norm() (Usage, []Attempt) {
	out := Usage{Input: val(u.InputTokens), Output: val(u.OutputTokens), CacheRead: val(u.CacheRead), CacheWrite: val(u.CacheCreation)}
	if u.OutputDetails != nil {
		out.Reasoning = u.OutputDetails.ThinkingTokens
	}
	if len(u.Iterations) == 0 {
		return out, nil
	}
	var sum Usage
	var atts []Attempt
	for _, it := range u.Iterations {
		x := Usage{Input: it.InputTokens, Output: it.OutputTokens, CacheRead: it.CacheRead, CacheWrite: it.CacheCreation}
		sum.Add(x)
		atts = append(atts, Attempt{Model: it.Model, Fallback: it.Type == "fallback_message", Usage: x})
	}
	return sum, atts
}

// claudeBlock 拼到一半的内容块：content_block_start 给的对象，加上后面的 delta。
type claudeBlock struct {
	obj    map[string]json.RawMessage
	typ    string
	text   strings.Builder // text_delta / thinking_delta
	sig    strings.Builder // signature_delta
	input  strings.Builder // input_json_delta
	cites  []json.RawMessage
	tool   int // 第几个工具调用；不是 tool_use 为 -1
	call   *ToolCall
	closed bool
}

type claudeAcc struct {
	c       *Client
	emit    func(Event)
	started bool // 收到了 message_start
	model   string
	blocks  map[int]*claudeBlock
	order   []int
	stop    string
	details json.RawMessage
	usage   claudeUsage
	tools   int
	outLen  int // 收到的输出有多少字节（断开时估算用量）
}

func newClaudeAcc(c *Client, emit func(Event)) *claudeAcc {
	return &claudeAcc{c: c, emit: emit, blocks: map[int]*claudeBlock{}}
}

func (a *claudeAcc) start(index int, raw json.RawMessage) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		obj = map[string]json.RawMessage{}
	}
	b := &claudeBlock{obj: obj, typ: rawText(obj["type"]), tool: -1}
	if _, dup := a.blocks[index]; !dup {
		a.order = append(a.order, index)
	}
	a.blocks[index] = b
	switch b.typ {
	case "tool_use":
		b.tool = a.tools
		a.tools++
		b.call = &ToolCall{ID: rawText(obj["id"]), Name: rawText(obj["name"])}
		a.emit(Event{Type: EvToolStart, Index: b.tool, Tool: &ToolCall{ID: b.call.ID, Name: b.call.Name}})
	case "fallback":
		var fb struct {
			To struct {
				Model string `json:"model"`
			} `json:"to"`
		}
		_ = json.Unmarshal(raw, &fb)
		a.emit(Event{Type: EvFallback, Model: fb.To.Model})
	}
}

func (a *claudeAcc) delta(index int, raw json.RawMessage) {
	b := a.blocks[index]
	if b == nil {
		return
	}
	var d struct {
		Type        string          `json:"type"`
		Text        string          `json:"text"`
		Thinking    string          `json:"thinking"`
		Signature   string          `json:"signature"`
		PartialJSON string          `json:"partial_json"`
		Citation    json.RawMessage `json:"citation"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return
	}
	switch d.Type {
	case "text_delta":
		b.text.WriteString(d.Text)
		a.outLen += len(d.Text)
		a.emit(Event{Type: EvText, Text: d.Text})
	case "thinking_delta":
		b.text.WriteString(d.Thinking)
		a.outLen += len(d.Thinking)
		if d.Thinking != "" {
			a.emit(Event{Type: EvReasoning, Text: d.Thinking})
		}
	case "signature_delta":
		b.sig.WriteString(d.Signature)
	case "input_json_delta":
		b.input.WriteString(d.PartialJSON)
		a.outLen += len(d.PartialJSON)
		if b.tool >= 0 && d.PartialJSON != "" {
			a.emit(Event{Type: EvToolDelta, Index: b.tool, Text: d.PartialJSON})
		}
	case "citations_delta":
		if len(d.Citation) > 0 {
			b.cites = append(b.cites, d.Citation)
		}
	}
}

// finish 收齐一个块：把累加的内容写回对象。工具参数要严格解析成 JSON 对象（eager_input_streaming 打开后
// 服务端不再替我们校验），解析不了的标成 Bad，历史里的 input 写成 {}，交回结果时报参数错误让它重来。
func (a *claudeAcc) finish(index int) {
	b := a.blocks[index]
	if b == nil || b.closed {
		return
	}
	b.closed = true
	switch b.typ {
	case "text":
		if b.text.Len() > 0 {
			b.obj["text"] = mustJSON(b.text.String())
		}
		if len(b.cites) > 0 {
			b.obj["citations"] = mustJSON(b.cites)
		}
	case "thinking":
		if b.text.Len() > 0 {
			b.obj["thinking"] = mustJSON(b.text.String())
		}
		if b.sig.Len() > 0 {
			b.obj["signature"] = mustJSON(b.sig.String())
		}
	case "tool_use", "server_tool_use", "mcp_tool_use":
		if b.input.Len() > 0 {
			args, bad := parseArgs(b.input.String())
			b.obj["input"] = args
			if b.call != nil {
				b.call.Args, b.call.Bad = args, bad
				if bad != "" {
					b.call.Raw = b.input.String()
				}
			}
		} else if b.call != nil {
			b.call.Args, b.call.Bad = jsonObjectOr(b.obj["input"]), ""
		}
	}
	if b.call != nil {
		tc := *b.call
		a.emit(Event{Type: EvToolDone, Index: b.tool, Tool: &tc})
	}
}

func jsonObjectOr(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return json.RawMessage(`{}`)
	}
	return compactJSON(raw)
}

// assemble 按序号排好所有块，套用服务端回退的回传规则，拼出回复和要追加进历史的消息。
func (a *claudeAcc) assemble(partial bool, bodyLen int) *Result {
	sort.Ints(a.order)
	var raws []json.RawMessage
	var calls []*ToolCall
	for _, i := range a.order {
		b := a.blocks[i]
		if !b.closed {
			a.finish(i)
		}
		raws = append(raws, mustJSON(b.obj))
		calls = append(calls, b.call)
	}
	return a.c.claudeResult(raws, calls, a.model, a.stop, a.details, &a.usage, partial, bodyLen, a.outLen)
}

// echoBlocks 服务端回退后要带回的块（官方的回传规则）：最后一个 fallback 块之前的 thinking、redacted_thinking、
// connector_text 和客户端工具调用不带回，没有配对结果的服务端工具调用也不带回；文字、配对的服务端工具块、
// fallback 块本身和它之后的一切照原样带回。keep[i] 说第 i 块要不要带回。
func echoBlocks(types []string, objs []map[string]json.RawMessage) []bool {
	keep := make([]bool, len(types))
	last := -1
	for i, t := range types {
		keep[i] = true
		if t == "fallback" {
			last = i
		}
	}
	if last < 0 {
		return keep
	}
	results := map[string]bool{} // 服务端工具结果对应的调用编号
	for i, t := range types {
		if strings.HasSuffix(t, "_tool_result") {
			results[rawText(objs[i]["tool_use_id"])] = true
		}
	}
	for i := 0; i < last; i++ {
		switch types[i] {
		case "thinking", "redacted_thinking", "connector_text", "tool_use":
			keep[i] = false
		case "server_tool_use", "mcp_tool_use":
			keep[i] = results[rawText(objs[i]["id"])]
		}
	}
	return keep
}

// claudeResult 流式和非流式共用：raws 是各个内容块的原文，calls[i] 是第 i 块对应的工具调用（不是工具调用为 nil）。
func (c *Client) claudeResult(raws []json.RawMessage, calls []*ToolCall, model, stop string, details json.RawMessage,
	u *claudeUsage, partial bool, bodyLen, outLen int) *Result {
	r := &Result{proto: Anthropic, Model: model, StopRaw: stop, Partial: partial}
	types := make([]string, len(raws))
	objs := make([]map[string]json.RawMessage, len(raws))
	for i, raw := range raws {
		_ = json.Unmarshal(raw, &objs[i])
		types[i] = rawText(objs[i]["type"])
	}
	keep := echoBlocks(types, objs)
	var text, think strings.Builder
	var kept []json.RawMessage
	for i, raw := range raws {
		switch types[i] {
		case "text":
			text.WriteString(rawText(objs[i]["text"]))
		case "thinking":
			think.WriteString(rawText(objs[i]["thinking"]))
		}
		if !keep[i] {
			continue
		}
		kept = append(kept, raw)
		if calls != nil && calls[i] != nil {
			r.ToolCalls = append(r.ToolCalls, *calls[i])
			r.calls = append(r.calls, calls[i].ID)
		}
	}
	r.Text, r.Reasoning = text.String(), think.String()
	switch stop {
	case "end_turn", "stop_sequence":
		r.Stop = StopEnd
	case "tool_use":
		r.Stop = StopTool
	case "max_tokens", "model_context_window_exceeded":
		r.Stop = StopMaxTokens
	case "refusal":
		r.Stop = StopRefusal
	case "pause_turn":
		r.Stop = StopPause
	default:
		r.Stop = StopOther
	}
	if r.Stop == StopRefusal {
		var d struct {
			Category    string `json:"category"`
			Explanation string `json:"explanation"`
		}
		_ = json.Unmarshal(details, &d)
		r.Refusal = &Refusal{Category: d.Category, Explanation: d.Explanation}
		r.ToolCalls, r.calls = nil, nil // 拒答时不完整的输出一律不用，工具更不能执行
	}
	r.Usage, r.Attempts = u.norm()
	if partial {
		// 中途断开：输入用量 message_start 里已经有了；输出只有开头的一两个，按收到的字数估（§4.3，标上「估」）
		if u.InputTokens == nil {
			r.Usage.Input = estimateTokens(bodyLen)
		}
		r.Usage.Output = max(r.Usage.Output, estimateTokens(outLen))
		r.Usage.Estimated = true
	}
	if partial || r.Stop == StopRefusal || len(kept) == 0 {
		return r
	}
	var b bytes.Buffer
	b.WriteString(`{"role":"assistant","content":[`)
	for i, raw := range kept {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(raw)
	}
	b.WriteString(`]}`)
	r.msg = b.Bytes()
	return r
}

func (c *Client) claudeOnce(ctx context.Context, conv *Conversation, emit func(Event)) (*Result, error) {
	body := c.claudeBody(conv)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	limit := c.cfg.Timeout
	if c.cfg.Stream {
		limit = c.cfg.IdleTimeout
	}
	dog := newWatchdog(limit, cancel)
	defer dog.stop()
	resp, err := c.post(ctx, c.endpoint("/messages"), body, c.claudeHeader(true))
	if err != nil {
		if dog.Fired() {
			return nil, &NetError{Err: errors.New("等待响应超时")}
		}
		return nil, err
	}
	defer resp.Body.Close()

	if !c.cfg.Stream {
		b, err := readAll(kickReader{resp.Body, dog})
		if err != nil {
			return nil, c.cutErr(ctx, dog, err)
		}
		var m struct {
			Type       string            `json:"type"`
			Model      string            `json:"model"`
			Content    []json.RawMessage `json:"content"`
			StopReason string            `json:"stop_reason"`
			Details    json.RawMessage   `json:"stop_details"`
			Usage      *claudeUsage      `json:"usage"`
			Error      json.RawMessage   `json:"error"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("服务商返回的内容看不懂：%w", err)
		}
		if m.Type == "error" {
			return nil, c.apiError(500, http.Header{}, b)
		}
		calls := make([]*ToolCall, len(m.Content))
		ti := 0
		for i, raw := range m.Content {
			var blk struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
				Text  string          `json:"text"`
			}
			_ = json.Unmarshal(raw, &blk)
			switch blk.Type {
			case "tool_use":
				tc := &ToolCall{ID: blk.ID, Name: blk.Name, Args: jsonObjectOr(blk.Input)}
				calls[i] = tc
				emit(Event{Type: EvToolDone, Index: ti, Tool: tc})
				ti++
			case "text":
				emit(Event{Type: EvText, Text: blk.Text})
			case "fallback":
				var fb struct {
					To struct {
						Model string `json:"model"`
					} `json:"to"`
				}
				_ = json.Unmarshal(raw, &fb)
				emit(Event{Type: EvFallback, Model: fb.To.Model})
			}
		}
		u := m.Usage
		if u == nil {
			u = &claudeUsage{}
		}
		r := c.claudeResult(m.Content, calls, m.Model, m.StopReason, m.Details, u, false, len(body), 0)
		emit(Event{Type: EvUsage, Usage: &r.Usage})
		return r, nil
	}

	acc := newClaudeAcc(c, emit)
	sr := newSSEReader(kickReader{resp.Body, dog})
	for {
		ev, err := sr.next()
		if err == io.EOF {
			if acc.started {
				return acc.assemble(true, len(body)), fmt.Errorf("%w：没有收到 message_stop", ErrDisconnected)
			}
			return nil, &NetError{Err: errors.New("连接被关闭，没有收到回复")}
		}
		if err != nil {
			if acc.started {
				return acc.assemble(true, len(body)), fmt.Errorf("%w：%v", ErrDisconnected, c.cutErr(ctx, dog, err))
			}
			return nil, c.cutErr(ctx, dog, err)
		}
		var head struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			Message      json.RawMessage `json:"message"`
			ContentBlock json.RawMessage `json:"content_block"`
			Delta        json.RawMessage `json:"delta"`
			Usage        *claudeUsage    `json:"usage"`
			Error        json.RawMessage `json:"error"`
		}
		if json.Unmarshal(ev.Data, &head) != nil {
			continue
		}
		typ := head.Type
		if typ == "" {
			typ = ev.Name
		}
		switch typ {
		case "message_start":
			var m struct {
				Model string       `json:"model"`
				Usage *claudeUsage `json:"usage"`
			}
			_ = json.Unmarshal(head.Message, &m)
			acc.started = true
			acc.model = m.Model
			acc.usage.merge(m.Usage)
			n, _ := acc.usage.norm()
			emit(Event{Type: EvUsage, Usage: &n})
		case "content_block_start":
			acc.start(head.Index, head.ContentBlock)
		case "content_block_delta":
			acc.delta(head.Index, head.Delta)
		case "content_block_stop":
			acc.finish(head.Index)
		case "message_delta":
			var d struct {
				StopReason string          `json:"stop_reason"`
				Details    json.RawMessage `json:"stop_details"`
			}
			_ = json.Unmarshal(head.Delta, &d)
			if d.StopReason != "" {
				acc.stop = d.StopReason
			}
			if len(d.Details) > 0 && string(d.Details) != "null" {
				acc.details = d.Details
			}
			acc.usage.merge(head.Usage)
			n, _ := acc.usage.norm()
			emit(Event{Type: EvUsage, Usage: &n})
		case "message_stop":
			return acc.assemble(false, len(body)), nil
		case "error":
			e := c.apiError(500, http.Header{}, []byte(`{"error":`+string(head.Error)+`}`))
			e.Status = streamTypeStatus(e.Type)
			e.InStream = true
			if acc.started {
				// 已经开始输出：当作断开，不重试
				return acc.assemble(true, len(body)), fmt.Errorf("%w：%v", ErrDisconnected, e)
			}
			return nil, e // 还没开始：按它对应的 429、529 等错误处理，可以重试
		default:
			// ping 和以后新加的事件一律跳过
		}
	}
}

func (c *Client) claudeModels(ctx context.Context) ([]Model, error) {
	var out []Model
	after := ""
	for page := 0; page < 5; page++ {
		q := url.Values{"limit": {"1000"}}
		if after != "" {
			q.Set("after_id", after)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/models")+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header = c.claudeHeader(false)
		req.Header.Del("Accept")
		resp, err := c.do(req)
		if err != nil {
			return nil, err
		}
		b, err := readAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var v struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				MaxTokens   int    `json:"max_tokens"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, errors.New("服务商返回的模型列表看不懂，请手动填写模型名")
		}
		for _, m := range v.Data {
			if m.ID != "" {
				out = append(out, Model{ID: m.ID, Name: m.DisplayName, MaxOutput: m.MaxTokens})
			}
		}
		if !v.HasMore || v.LastID == "" || v.LastID == after {
			break
		}
		after = v.LastID
	}
	return out, nil // 官方按发布时间从新到旧排，保持原顺序
}
