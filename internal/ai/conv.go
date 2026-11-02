package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type msgKind uint8

const (
	kindUser        msgKind = iota + 1 // 用户的一句话
	kindAssistant                      // AI 的回复
	kindToolResults                    // 交回的工具结果（Claude 是一条 user 消息，OpenAI 兼容是若干条 tool 消息）
)

// Conversation 一个会话的历史，绑定一种协议。
//
// 只能追加，不能改写：系统提示和工具清单在创建时编好、之后不增不减；每条消息追加时按协议编成原文存下，
// 之后的每次请求都把这些原文原样拼进去，所以相邻两次请求的系统提示、工具清单和已有的消息逐字节相同
// （Claude 的思考块据此保持有效，各家的前缀缓存也才能命中，§4.3）。会话太长时提示用户开新会话，不在本地裁剪。
type Conversation struct {
	proto    Protocol
	eager    bool // Claude：工具带 eager_input_streaming（流式请求时让工具参数边生成边推过来）
	toolName bool // OpenAI 兼容：tool 消息带上工具名（Kimi 的示例这么写）
	system   string
	tools    []Tool
	sysRaw   json.RawMessage
	toolRaw  json.RawMessage
	msgs     []json.RawMessage
	kinds    []msgKind
	pending  []ToolCall // 上一条回复里等着交回结果的工具调用（只有编号和名字）
}

var toolNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func newConversation(p Protocol, system string, tools []Tool, eager, toolName bool) (*Conversation, error) {
	c := &Conversation{proto: p, eager: eager, toolName: toolName, system: system, tools: append([]Tool(nil), tools...)}
	seen := map[string]bool{}
	for _, t := range tools {
		if !toolNameRe.MatchString(t.Name) {
			return nil, fmt.Errorf("工具名 %q 不合法：只能有字母、数字、下划线和横线，最长 64 个", t.Name)
		}
		if seen[t.Name] {
			return nil, fmt.Errorf("工具名 %q 重复", t.Name)
		}
		seen[t.Name] = true
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(t.Schema, &obj); err != nil || obj == nil {
			return nil, fmt.Errorf("工具 %s 的参数说明不是 JSON 对象", t.Name)
		}
	}
	c.encodeFixed()
	return c, nil
}

// encodeFixed 编好系统提示和工具清单（只在创建时、以及第一次请求就被拒、还没有任何回复时调用）。
func (c *Conversation) encodeFixed() {
	switch c.proto {
	case Anthropic:
		c.sysRaw = nil
		if c.system != "" {
			// 系统提示末尾加 cache_control：工具清单和系统提示一起进缓存（§4.3 缓存）
			c.sysRaw = mustJSON([]map[string]any{{"type": "text", "text": c.system, "cache_control": map[string]string{"type": "ephemeral"}}})
		}
		c.toolRaw = nil
		if len(c.tools) > 0 {
			var buf bytes.Buffer
			buf.WriteByte('[')
			for i, t := range c.tools {
				if i > 0 {
					buf.WriteByte(',')
				}
				buf.WriteString(`{"name":`)
				buf.Write(mustJSON(t.Name))
				if t.Description != "" {
					buf.WriteString(`,"description":`)
					buf.Write(mustJSON(t.Description))
				}
				buf.WriteString(`,"input_schema":`)
				buf.Write(compactJSON(t.Schema))
				if c.eager {
					buf.WriteString(`,"eager_input_streaming":true`)
				}
				buf.WriteByte('}')
			}
			buf.WriteByte(']')
			c.toolRaw = buf.Bytes()
		}
	default:
		c.sysRaw = nil
		if c.system != "" {
			c.sysRaw = mustJSON(map[string]string{"role": "system", "content": c.system})
		}
		c.toolRaw = nil
		if len(c.tools) > 0 {
			var buf bytes.Buffer
			buf.WriteByte('[')
			for i, t := range c.tools {
				if i > 0 {
					buf.WriteByte(',')
				}
				buf.WriteString(`{"type":"function","function":{"name":`)
				buf.Write(mustJSON(t.Name))
				if t.Description != "" {
					buf.WriteString(`,"description":`)
					buf.Write(mustJSON(t.Description))
				}
				buf.WriteString(`,"parameters":`)
				buf.Write(compactJSON(t.Schema))
				buf.WriteString(`}}`)
			}
			buf.WriteByte(']')
			c.toolRaw = buf.Bytes()
		}
	}
}

// Protocol 会话绑定的协议。
func (c *Conversation) Protocol() Protocol { return c.proto }

// Len 已有几条消息（OpenAI 兼容的每条工具结果单算一条）。
func (c *Conversation) Len() int { return len(c.msgs) }

// Pending 上一条回复里还等着交回结果的工具调用。
func (c *Conversation) Pending() []ToolCall { return append([]ToolCall(nil), c.pending...) }

func (c *Conversation) last() msgKind {
	if len(c.kinds) == 0 {
		return 0
	}
	return c.kinds[len(c.kinds)-1]
}

func (c *Conversation) hasReply() bool {
	for _, k := range c.kinds {
		if k == kindAssistant {
			return true
		}
	}
	return false
}

func (c *Conversation) push(k msgKind, raw json.RawMessage) {
	c.msgs = append(c.msgs, raw)
	c.kinds = append(c.kinds, k)
}

// AddUser 追加用户的一句话。上一条必须是 AI 的回复（或者会话刚开始），而且没有等着交回的工具结果。
//
// 上一条也可以是交回的工具结果：AI 拿到工具结果后的那次回复被用户停止、出错或者是空的，用户接着说话
// （或者用户没处理确认卡片就发了新消息，提案取消后的结果先交回）。OpenAI 兼容是在 tool 消息后面接一条
// user 消息；Claude 是紧跟在工具结果那条 user 消息后面再来一条，服务端会把相邻的两条合成一轮
// （工具结果在前、文字在后）。两种写法都只是追加，前面的消息一个字节都不动。
func (c *Conversation) AddUser(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("消息是空的")
	}
	if len(c.pending) > 0 {
		return errors.New("上一条回复里的工具调用还没交回结果")
	}
	if c.last() == kindUser {
		return errors.New("还没收到上一条消息的回复")
	}
	if c.proto == Anthropic {
		c.push(kindUser, mustJSON(map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": text}}}))
	} else {
		c.push(kindUser, mustJSON(map[string]string{"role": "user", "content": text}))
	}
	return nil
}

// NeedsReply 最后一条是用户的话或交回的工具结果，还等着 AI 回复（这时可以直接 Chat）。
func (c *Conversation) NeedsReply() bool { k := c.last(); return k == kindUser || k == kindToolResults }

// UndoUser 撤回最后一条还没得到回复的用户消息（请求出错、被拒答、用户点了停止之后，用户想换个说法时用）。
// 它后面没有任何回复，撤回不影响之前的思考块和缓存。最后一条不是用户的话时什么也不做，返回 false。
func (c *Conversation) UndoUser() bool {
	if c.last() != kindUser {
		return false
	}
	c.msgs = c.msgs[:len(c.msgs)-1]
	c.kinds = c.kinds[:len(c.kinds)-1]
	return true
}

// AddResult 把一次完整的回复追加进历史（原生内容原样存下，包括思考块和服务端回退留下的块）。
// 不完整的、拒答的、空的回复不能追加。
func (c *Conversation) AddResult(r *Result) error {
	switch {
	case r == nil || r.proto != c.proto:
		return errors.New("回复和会话的协议对不上")
	case r.Partial:
		return errors.New("不完整的回复不能追加进历史")
	case r.Stop == StopRefusal:
		return errors.New("被拒答的回复不能追加进历史（可以用 UndoUser 撤回这条提问）")
	case len(r.msg) == 0:
		return errors.New("回复是空的")
	}
	if k := c.last(); k != kindUser && k != kindToolResults {
		return errors.New("没有等着回复的消息")
	}
	c.push(kindAssistant, r.msg)
	c.pending = nil
	for _, tc := range r.ToolCalls {
		c.pending = append(c.pending, ToolCall{ID: tc.ID, Name: tc.Name})
	}
	return nil
}

// AddToolResults 交回上一条回复里全部工具调用的结果（一个都不能少，按调用的顺序放）。
// Claude 要求同一轮的结果放在同一条消息里，分开交回会让模型以后不再并行调用（§3.3）。
func (c *Conversation) AddToolResults(rs []ToolResult) error {
	if len(c.pending) == 0 {
		return errors.New("没有等着结果的工具调用")
	}
	byID := map[string]ToolResult{}
	for _, r := range rs {
		if _, dup := byID[r.ID]; dup {
			return fmt.Errorf("工具调用 %s 的结果交了两次", r.ID)
		}
		byID[r.ID] = r
	}
	if len(byID) != len(c.pending) {
		return fmt.Errorf("要交回 %d 个工具调用的结果，实际 %d 个", len(c.pending), len(byID))
	}
	ordered := make([]ToolResult, 0, len(c.pending))
	for _, p := range c.pending {
		r, ok := byID[p.ID]
		if !ok {
			return fmt.Errorf("缺少工具调用 %s 的结果", p.ID)
		}
		if r.Content == "" {
			r.Content = "（没有输出）"
		}
		ordered = append(ordered, r)
	}
	if c.proto == Anthropic {
		blocks := make([]map[string]any, 0, len(ordered))
		for _, r := range ordered {
			b := map[string]any{"type": "tool_result", "tool_use_id": r.ID, "content": r.Content}
			if r.IsError {
				b["is_error"] = true
			}
			blocks = append(blocks, b)
		}
		c.push(kindToolResults, mustJSON(map[string]any{"role": "user", "content": blocks}))
	} else {
		for i, r := range ordered {
			content := r.Content
			if r.IsError {
				content = "工具执行出错：" + content // 这个协议没有 is_error 字段
			}
			m := map[string]string{"role": "tool", "tool_call_id": r.ID, "content": content}
			if c.toolName {
				m["name"] = c.pending[i].Name
			}
			c.push(kindToolResults, mustJSON(m))
		}
	}
	c.pending = nil
	return nil
}

// ---------- 保存与读回（第三阶段的会话保存用） ----------

type convFile struct {
	V        int               `json:"v"`
	Proto    Protocol          `json:"proto"`
	Eager    bool              `json:"eager,omitempty"`
	ToolName bool              `json:"toolName,omitempty"`
	System   string            `json:"system"`
	Tools    []Tool            `json:"tools"`
	SysRaw   json.RawMessage   `json:"sysRaw,omitempty"`
	ToolRaw  json.RawMessage   `json:"toolRaw,omitempty"`
	Msgs     []json.RawMessage `json:"msgs"`
	Kinds    []msgKind         `json:"kinds"`
	Pending  []ToolCall        `json:"pending,omitempty"`
}

// Save 把会话写成 JSON。读回来的会话发出的请求和保存前逐字节相同（用了不转义 HTML 的编码器，
// 原文不会被改写；不要用 json.Marshal 包一层再存，它会把原文里的 < > & 转义掉）。
func (c *Conversation) Save(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(convFile{V: 1, Proto: c.proto, Eager: c.eager, ToolName: c.toolName, System: c.system, Tools: c.tools,
		SysRaw: c.sysRaw, ToolRaw: c.toolRaw, Msgs: c.msgs, Kinds: c.kinds, Pending: c.pending})
}

// LoadConversation 读回 Save 写的会话。
func LoadConversation(r io.Reader) (*Conversation, error) {
	var f convFile
	if err := json.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("会话文件坏了：%w", err)
	}
	if f.V != 1 || !f.Proto.valid() || len(f.Msgs) != len(f.Kinds) {
		return nil, errors.New("会话文件坏了：格式不对")
	}
	return &Conversation{proto: f.Proto, eager: f.Eager, toolName: f.ToolName, system: f.System, tools: f.Tools,
		sysRaw: f.SysRaw, toolRaw: f.ToolRaw, msgs: f.Msgs, kinds: f.Kinds, pending: f.Pending}, nil
}

// ---------- 编码 ----------

// mustJSON 编码成 JSON，不转义 HTML（< > & 原样留着，好读）。只用于编得出来的值。
func mustJSON(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(fmt.Sprintf("ai: 编码 JSON 失败：%v", err))
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// compactJSON 去掉空白（调用方保证是合法的 JSON）。
func compactJSON(b []byte) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return b
	}
	return buf.Bytes()
}
