// Package ops 操作层：把 MineDeck 能做的事（查状态、改配置、启停、发命令、备份……）定义成带参数说明的「操作」，
// 网页接口和以后的 AI 都通过 Registry.Do 调用。权限分级、AI 写操作的提案与确认、操作记录都在这一层。
//
// 这个包只有框架，不引用 Windows API，Linux 上可以直接测试；MineDeck 的具体操作在 ops/catalog 里登记。
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Level 风险等级（设计文档 §3.3）。
type Level int

const (
	Read      Level = iota + 1 // R 读：不改任何东西
	Write                      // W 写：可撤销或影响小
	Danger                     // D 危险：影响在线玩家，或很难撤销
	HumanOnly                  // H 仅人工：AI 永远不能调用
)

func (l Level) String() string {
	switch l {
	case Read:
		return "R"
	case Write:
		return "W"
	case Danger:
		return "D"
	case HumanOnly:
		return "H"
	}
	return "?"
}

func (l Level) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// Source 调用来源。
type Source string

const (
	SrcUI        Source = "ui"        // 本机的界面
	SrcAPI       Source = "api"       // 本机的其他程序调用开放 API
	SrcLAN       Source = "lan"       // 局域网登录的设备
	SrcAssistant Source = "assistant" // 应用内 AI 助手
	SrcGameChat  Source = "gamechat"  // 游戏内聊天 AI
	SrcCompanion Source = "companion" // AI 伙伴
	SrcSchedule  Source = "schedule"  // 定时任务、崩溃自动重启
	SrcMCP       Source = "mcp"       // 这台电脑上的其他 AI 工具经 MCP 接口调用（actor 是客户端报的名字）
)

// Human 是不是人直接发起的（在界面上点了按钮，或者自己写程序调接口）。
func (s Source) Human() bool { return s == SrcUI || s == SrcAPI || s == SrcLAN }

// AI 是不是 AI 发起的。
func (s Source) AI() bool {
	return s == SrcAssistant || s == SrcGameChat || s == SrcCompanion || s == SrcMCP
}

// Caller 谁在调用。
type Caller struct {
	Source  Source `json:"src"`
	Actor   string `json:"actor,omitempty"`   // "本机"、局域网设备的地址、玩家名、"定时任务"、MCP 客户端报的名字
	Session string `json:"session,omitempty"` // 助手会话编号等
	Local   bool   `json:"-"`                 // 发起的人就在这台电脑上：「仅本机」的操作只认这个
}

// Op 一个操作。
type Op struct {
	Name      string  // "instance.props.set"
	Title     string  // "修改 server.properties"
	Desc      string  // 给 AI 看的说明
	Params    *Schema // 参数说明；nil 表示没有参数
	Level     Level
	LocalOnly bool   // 仅本机（沿用网页接口 isLoopbackReq 的规则）
	LocalMsg  string // 从别的设备调用「仅本机」操作时的提示；空则用通用说法
	InGame    bool   // 游戏内 AI、AI 伙伴也能调用（只对读操作有效）；默认不开放
	NoLog     bool   // 不写操作记录：不改任何状态的仅人工操作（打开目录、选择文件夹）
	// HumanParams 只能由人决定的参数：AI 发起时一律去掉（给 AI 的工具说明里也没有），比如新建实例的 eula、root。
	HumanParams []string
	// Consent 确认提案时要由确认的人勾选的同意（如 Minecraft EULA），勾了才把这个参数设成 true 再执行。
	// 它对应的参数应当也列在 HumanParams 里：AI 不能替用户同意。
	Consent *Consent
	// Preview 写操作：只算出会改什么，不真的去改。Plan.Pre 是前置条件，确认时重算一遍比对，变了提案作废。
	// 没有 Preview 的写操作，提案只带标题，没有前置条件可查。
	Preview func(ctx context.Context, c Caller, a Args) (*Plan, error)
	Run     func(ctx context.Context, c Caller, a Args) (any, error)
}

// ToolName 给 AI 用的工具名：点换成下划线（两家的工具名都只允许字母、数字、下划线和横线）。
func ToolName(name string) string { return strings.ReplaceAll(name, ".", "_") }

// Consent 只能由人在确认时给出的同意。
type Consent struct {
	Param string `json:"param"`         // 参数名，如 eula
	Text  string `json:"text"`          // 确认卡片上勾选框旁边的文字
	URL   string `json:"url,omitempty"` // 条款原文的地址
	// Need 这次要不要（比如代理端不用同意 EULA）；nil 表示总要。
	Need func(a Args) bool `json:"-"`
}

// AISchema 给 AI 的参数说明（JSON Schema）：去掉只能由人决定的参数。没有参数时是一个空对象。
// 编码是确定的（键按固定顺序），同一个操作每次得到的字节都一样，工具清单的前缀缓存才能命中。
func (op *Op) AISchema() json.RawMessage {
	if op.Params == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	if len(op.HumanParams) == 0 {
		return json.RawMessage(encode(op.Params))
	}
	s := *op.Params
	s.Properties = map[string]*Schema{}
	for k, v := range op.Params.Properties {
		if !slices.Contains(op.HumanParams, k) {
			s.Properties[k] = v
		}
	}
	s.Required = slices.DeleteFunc(slices.Clone(op.Params.Required), func(k string) bool { return slices.Contains(op.HumanParams, k) })
	return json.RawMessage(encode(&s))
}

// Plan 写操作的预览。
type Plan struct {
	Summary string            `json:"summary"`           // 一句话说明，如「把 生存服 的 max-players 从 20 改成 10」
	Changes []Change          `json:"changes,omitempty"` // 改动清单（改前 / 改后）
	Warning string            `json:"warning,omitempty"` // 危险操作的醒目警告，如「在线的 3 人会掉线」
	Diff    string            `json:"diff,omitempty"`    // 文本文件的改动（逐行对比，见 LineDiff）
	Pre     map[string]string `json:"-"`                 // 前置条件：名称 → 指纹（文件哈希、实例状态……），只在服务端比对
}

// Change 一项改动。
type Change struct {
	What   string `json:"what"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Args 一次调用的参数：一个 JSON 对象的原文。Do 校验后把它规范化（键排序、去掉空白），
// 提案存下的、确认后执行的、写进操作记录的，都是规范化后的这一份。
type Args json.RawMessage

// ArgsOf 把结构体或 map 编成参数（给网页适配层用）。
func ArgsOf(v any) Args {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("ops.ArgsOf: %v", err)) // 只会是调用方传了编不了 JSON 的类型
	}
	return Args(b)
}

// Bind 把参数解码进具体的结构体。
func (a Args) Bind(v any) error {
	if len(a) == 0 {
		return nil
	}
	return json.Unmarshal(a, v)
}

func (a Args) MarshalJSON() ([]byte, error) {
	if len(a) == 0 {
		return []byte("{}"), nil
	}
	return a, nil
}

func (a *Args) UnmarshalJSON(b []byte) error {
	*a = append((*a)[:0], b...)
	return nil
}

// Error 带状态码的错误。Code 沿用 HTTP 状态码的含义：400 参数或业务错误，403 没有权限，404 找不到，
// 409 提案作废（前置条件变了），410 提案过期，500 内部错误，502 上游服务失败。网页接口原样用作响应码。
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Errorf 生成带状态码的错误。
func Errorf(code int, format string, a ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, a...)}
}

// Wrap 给错误带上状态码；err 为 nil 时返回 nil。
func Wrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}

// CodeOf 取错误的状态码；没带状态码的按 400（业务错误）。
func CodeOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 400
}

// Registry 操作的注册表，也是统一入口。
type Registry struct {
	Log *Log // 操作记录；nil 时不记
	// TTL 提案的有效期，默认 5 分钟。
	TTL time.Duration

	mu        sync.Mutex
	ops       map[string]*Op
	order     []string
	proposals map[string]*Proposal
	resolved  []func(Resolution)
	now       func() time.Time
}

func NewRegistry(log *Log) *Registry {
	return &Registry{Log: log, TTL: 5 * time.Minute, ops: map[string]*Op{}, proposals: map[string]*Proposal{}, now: time.Now}
}

// Register 登记一个操作；名字重复或定义不全直接 panic（启动时就能发现）。
func (r *Registry) Register(op *Op) {
	if op.Name == "" || op.Run == nil || op.Level < Read || op.Level > HumanOnly {
		panic("ops: 操作定义不全：" + op.Name)
	}
	if op.Params != nil {
		op.Params.compile()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.ops[op.Name]; dup {
		panic("ops: 操作重复登记：" + op.Name)
	}
	r.ops[op.Name] = op
	r.order = append(r.order, op.Name)
}

// Op 按名字取操作；没有返回 nil。
func (r *Registry) Op(name string) *Op {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ops[name]
}

// List 全部操作，按登记顺序。
func (r *Registry) List() []*Op {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Op, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.ops[n])
	}
	return out
}

// Available 列出 c 能调用（或能提出提案）的操作，按登记顺序。给 AI 层生成工具清单用：
// 仅人工的操作永远不在 AI 的清单里，游戏内只列明确开放的读操作。
func (r *Registry) Available(c Caller) []*Op {
	var out []*Op
	for _, op := range r.List() {
		if _, err := allow(c, op); err == nil {
			out = append(out, op)
		}
	}
	return out
}

// allow 判断 c 能不能调用 op（来源 × 风险等级 × 仅本机）：propose 为 true 表示要先生成提案、等人确认。
func allow(c Caller, op *Op) (propose bool, err error) {
	if op.LocalOnly && !c.Local {
		if op.LocalMsg != "" {
			return false, &Error{Code: 403, Err: errors.New(op.LocalMsg)}
		}
		return false, Errorf(403, "「%s」仅支持在本机操作", op.Title)
	}
	switch {
	case c.Source.Human():
		return false, nil // 人在界面上点了按钮，本身就是确认；危险操作界面上另有二次确认
	case c.Source == SrcSchedule:
		if op.Level == HumanOnly {
			return false, Errorf(403, "「%s」只能由人操作", op.Title)
		}
		return false, nil
	case c.Source == SrcAssistant || c.Source == SrcMCP: // MCP 接口的外部 AI 和应用内助手一样：读直接执行，写和危险只生成提案
		switch op.Level {
		case Read:
			return false, nil
		case Write, Danger:
			return true, nil
		}
		return false, Errorf(403, "「%s」只能由人操作，AI 不能调用", op.Title)
	case c.Source == SrcGameChat || c.Source == SrcCompanion:
		if op.Level == Read && op.InGame {
			return false, nil
		}
		return false, Errorf(403, "游戏内不能调用「%s」", op.Title)
	}
	return false, Errorf(403, "未知的调用来源：%q", c.Source)
}

// Check 只判断 c 有没有权限调用 name（不看参数）。网页接口在解析请求体之前先调它，
// 「仅本机」的接口因此和原来一样先判权限、再报请求体的错。
func (r *Registry) Check(c Caller, name string) error {
	op := r.Op(name)
	if op == nil {
		return Errorf(404, "未知操作：%s", name)
	}
	_, err := allow(c, op)
	return err
}

// Do 统一入口：判断权限 → 按参数说明校验并规范化参数 → AI 发起的写操作生成提案（返回 *Proposal，不执行）
// → 执行，写操作和危险操作写一条操作记录。
func (r *Registry) Do(ctx context.Context, c Caller, name string, a Args) (any, error) {
	op := r.Op(name)
	if op == nil {
		return nil, Errorf(404, "未知操作：%s", name)
	}
	propose, err := allow(c, op)
	if err != nil {
		return nil, err
	}
	var skip []string // AI 发起时，只能由人决定的参数去掉，也不算必填
	if c.Source.AI() && len(op.HumanParams) > 0 {
		a, skip = dropParams(a, op.HumanParams), op.HumanParams
	}
	args, err := op.normalize(a, skip)
	if err != nil {
		return nil, err
	}
	if propose {
		p, err := r.propose(ctx, c, op, args)
		if err != nil {
			return nil, err // 不能返回装着 nil *Proposal 的 any：调用方用类型断言认提案
		}
		return p, nil
	}
	return r.exec(ctx, c, op, args, "", "")
}

// dropParams 去掉参数里的这几项（AI 发起时只能由人决定的参数）。不是 JSON 对象就原样返回，交给校验去报错。
func dropParams(a Args, names []string) Args {
	var obj map[string]json.RawMessage
	if json.Unmarshal(a, &obj) != nil || obj == nil {
		return a
	}
	for _, n := range names {
		delete(obj, n)
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return a
	}
	return Args(b)
}

// setParam 把参数里的一项设成 v（确认时勾了同意，就把 eula 设成 true）。
func setParam(a Args, name string, v any) Args {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(a))
	dec.UseNumber()
	if len(a) == 0 || dec.Decode(&obj) != nil || obj == nil {
		obj = map[string]any{}
	}
	obj[name] = v
	return encode(obj)
}

// normalize 校验参数并规范化：解析成 JSON 对象（null 和空白都当作 {}），按 Params 校验，再按键排序重新编码。
// skip 里的参数不算必填（AI 发起时去掉了的只能由人决定的参数）。
func (op *Op) normalize(a Args, skip []string) (Args, error) {
	var v any
	if len(bytes.TrimSpace(a)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(a))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, Errorf(400, "参数不是合法的 JSON：%v", err)
		}
		if dec.More() {
			return nil, Errorf(400, "参数不是合法的 JSON：对象后面还有多余内容")
		}
	}
	if v == nil {
		v = map[string]any{}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, Errorf(400, "参数应为 JSON 对象")
	}
	if op.Params != nil {
		s := op.Params
		if len(skip) > 0 {
			c := *s
			c.Required = slices.DeleteFunc(slices.Clone(s.Required), func(k string) bool { return slices.Contains(skip, k) })
			s = &c
		}
		if err := s.validate("", obj); err != nil {
			return nil, &Error{Code: 400, Err: err}
		}
	}
	return encode(obj), nil
}

// encode 按键排序编码，不转义 < > &（操作记录里好读）。
func encode(v any) Args {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return Args(bytes.TrimRight(buf.Bytes(), "\n"))
}

// exec 执行操作；写操作、危险操作和仅人工的操作写一条记录（NoLog 的除外）。
func (r *Registry) exec(ctx context.Context, c Caller, op *Op, args Args, proposal, approvedBy string) (any, error) {
	start := time.Now()
	res, err := op.Run(ctx, c, args)
	if op.Level != Read && !op.NoLog {
		r.record(c, op.Name, args, "", proposal, approvedBy, err, time.Since(start))
	}
	return res, err
}

// Record 记下一条没有经过 Do 的操作（定时任务、崩溃自动重启这类程序自己发起的事），格式和 Do 写的一样。
func (r *Registry) Record(c Caller, name string, args Args, err error, d time.Duration) {
	r.record(c, name, args, "", "", "", err, d)
}

// 提案的几个节点也各记一条（Record.Event）：AI 提出、人拒绝、提案作废（过期或前置条件变了）。确认后执行的那一条照常记，
// 不带 event。
const (
	EvPropose = "propose"
	EvReject  = "reject"
	EvVoid    = "void"
)

func (r *Registry) record(c Caller, name string, args Args, event, proposal, approvedBy string, err error, d time.Duration) {
	if r.Log == nil {
		return
	}
	rec := Record{
		T: r.now().Format(time.RFC3339), Op: name, Src: c.Source, Actor: c.Actor, Session: c.Session, Event: event,
		Proposal: proposal, ApprovedBy: approvedBy, OK: err == nil, MS: d.Milliseconds(),
	}
	rec.Inst, rec.Args = splitArgs(args)
	if err != nil {
		rec.Err = err.Error()
	}
	r.Log.Write(rec)
}
