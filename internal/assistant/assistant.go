// Package assistant 应用内 AI 助手（设计文档 §5）：会话、工具映射（从操作层按调用方取可用的操作）、提案流程、
// 上下文打包和脱敏、会话保存。
//
// 一轮对话：用户的话 → AI 回复（流式）→ AI 调用工具：读操作自动执行，写操作和危险操作在操作层生成提案、
// 等人在确认卡片上确认（确认、拒绝、过期、作废都由操作层通知回来）→ 全部处理完一起交回给 AI → 直到 AI 不再调用工具。
// 一轮最多 8 次工具调用；单个工具结果超过 16 KB 截断。会话在后台跑，界面经事件订阅收流式输出，关掉面板不影响。
//
// 用 ai 接入层的顺序（第二阶段交接说明）：Begin → Client → NewConversation → Chat → Record → AddResult / AddToolResults，
// 出错、拒答、空回复、用户停止时 UndoUser；会话用 Save / LoadConversation 存盘读回。
//
// 不引用 Windows API，Linux 上能直接测试。
package assistant

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// MaxSessions 本机最多保存几个会话（§5.1），超出时删掉最久没动的。
const MaxSessions = 20

// Options 助手的依赖。
type Options struct {
	Ops    *ops.Registry // 和网页接口共用同一个注册表（提案存在里面）
	AI     *ai.Service
	Dir    string // 会话存放目录，一般是 <程序目录>/data/ai/sessions
	Redact *Redactor
	Now    func() time.Time
}

// Service 应用内助手。
type Service struct {
	reg    *ops.Registry
	ai     *ai.Service
	dir    string
	redact *Redactor
	now    func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session
}

// New 读回保存的会话，登记提案处理完的回调。不联网。
func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Redact == nil {
		o.Redact = &Redactor{}
	}
	s := &Service{reg: o.Ops, ai: o.AI, dir: o.Dir, redact: o.Redact, now: o.Now, sessions: map[string]*Session{}}
	s.loadAll()
	o.Ops.OnResolve(s.onResolve)
	return s
}

// ---------- 会话 ----------

// Origin 会话是谁开的：本机，还是局域网的哪台设备。
type Origin struct {
	Local bool   `json:"local"`
	Actor string `json:"actor"`
}

// Session 一个会话。存盘的字段带 json 标签；会话历史（ai.Conversation）原样存在 Conv 里。
type Session struct {
	ID      string            `json:"id"`
	Title   string            `json:"title"`
	Inst    string            `json:"inst,omitempty"`
	Created time.Time         `json:"created"`
	Updated time.Time         `json:"updated"`
	Origin  Origin            `json:"origin"`
	AI      ai.Current        `json:"ai"`    // 开会话时的服务商和模型：换了就要开新会话
	Tools   map[string]string `json:"tools"` // 工具名 → 操作名（创建时定下来，之后不增不减）
	Items   []*Item           `json:"items"`
	Wait    *Wait             `json:"wait,omitempty"` // 在等确认卡片
	Long    bool              `json:"long,omitempty"` // 会话已经很长了，建议开新会话
	Conv    json.RawMessage   `json:"conv"`

	mu     sync.Mutex
	conv   *ai.Conversation
	busy   bool
	cancel context.CancelFunc
	hub    hub
	attach *attachment
	early  map[string]ops.Resolution // 提案在记下对照之前就处理完了（极快的确认）：先存着
	nextID int
	rounds int // 这一轮已经请求了 AI 几次
	used   int // 这一轮已经用了几次工具
}

// Item 会话里的一条（界面显示用）。
type Item struct {
	ID        string      `json:"id"`
	Kind      string      `json:"kind"` // user / assistant / notice
	T         time.Time   `json:"t"`
	Text      string      `json:"text,omitempty"`
	Sent      string      `json:"sent,omitempty"`      // 用户的话：实际发给 AI 的内容（脱敏后、带上附加的排错资料），和 Text 一样时为空
	Attach    int         `json:"attach,omitempty"`    // 用户的话：附加了多少字的排错资料
	Masked    bool        `json:"masked,omitempty"`    // 用户的话：里面的隐私信息（路径里的用户名、局域网 IP、Key 等）发送前替换掉了
	Withdrawn bool        `json:"withdrawn,omitempty"` // 用户的话：没得到回复，已经从历史里撤回
	Tools     []*ToolItem `json:"tools,omitempty"`     // AI 的回复：这次调用的工具
	Usage     *ai.Usage   `json:"usage,omitempty"`
	Cost      *ai.Cost    `json:"cost,omitempty"`
	Model     string      `json:"model,omitempty"`
	Partial   bool        `json:"partial,omitempty"` // AI 的回复：没说完（停止、断开），不在历史里
	Level     string      `json:"level,omitempty"`   // 提示：info / warn / error
}

// ToolItem 一次工具调用。
type ToolItem struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Op       string          `json:"op,omitempty"`
	Title    string          `json:"title,omitempty"`
	Level    string          `json:"level,omitempty"` // R / W / D
	Args     json.RawMessage `json:"args,omitempty"`
	Status   string          `json:"status"` // running / done / error / skipped / pending（等确认）/ confirmed / failed / rejected / expired / invalid / canceled
	Error    string          `json:"error,omitempty"`
	Sent     string          `json:"sent,omitempty"` // 交还给 AI 的内容（脱敏、截断后）
	Proposal *ProposalView   `json:"proposal,omitempty"`
}

// ProposalView 确认卡片上要显示的提案。
type ProposalView struct {
	ID      string       `json:"id"`
	Op      string       `json:"op"`
	Title   string       `json:"title"`
	Level   string       `json:"level"`
	Args    ops.Args     `json:"args"`
	Plan    *ops.Plan    `json:"plan"`
	Consent *ops.Consent `json:"consent,omitempty"`
	Expires time.Time    `json:"expires"`
	Outcome string       `json:"outcome,omitempty"` // 处理结果，同 ops.Outcome，另有 canceled（没处理就发了新消息）
	Reason  string       `json:"reason,omitempty"`  // 拒绝的理由
	By      string       `json:"by,omitempty"`      // 处理的人
	Error   string       `json:"error,omitempty"`
}

// Wait 等确认卡片时存下的这一轮：工具调用、已有的结果、提案对照。
type Wait struct {
	Item    string                   `json:"item"`
	Calls   []ai.ToolCall            `json:"calls"`
	Results map[string]ai.ToolResult `json:"results"`
	Props   map[string]string        `json:"props"` // 提案编号 → 工具调用编号
	Tools   int                      `json:"tools"` // 这一轮已经用了几次工具
	Rounds  int                      `json:"rounds"`
}

// attachment 「让 AI 分析」打包好、用户看过预览的排错资料：发送时原样附在用户的话后面。
type attachment struct {
	text    string
	created time.Time
}

func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (sess *Session) caller() ops.Caller {
	return ops.Caller{Source: ops.SrcAssistant, Actor: sess.Origin.Actor, Session: sess.ID, Local: sess.Origin.Local}
}

func (sess *Session) newItem(kind string, now time.Time) *Item {
	sess.nextID++
	it := &Item{ID: fmt.Sprintf("i%d", sess.nextID), Kind: kind, T: now}
	sess.Items = append(sess.Items, it)
	return it
}

func (sess *Session) item(id string) *Item {
	for _, it := range sess.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// ---------- 错误 ----------

// ErrBusy 上一条还在回复。
var ErrBusy = ops.Errorf(409, "上一条消息还在回复，等它说完或者先点停止")

func errNotFound() error { return ops.Errorf(404, "会话不存在，可能已经被删掉了") }

// ---------- 状态 ----------

// Status 界面决定显不显示助手入口用：配置好了、本机或者局域网开关开着才显示（§4.5、§10.3）。
type Status struct {
	Enabled bool   `json:"enabled"`           // AI 配置好了
	Allowed bool   `json:"allowed"`           // 这个请求能用助手
	Local   bool   `json:"local"`             // 请求来自本机
	LAN     bool   `json:"lan"`               // 「局域网可用 AI 助手」开关
	Missing string `json:"missing,omitempty"` // 还差什么
	Preset  string `json:"preset,omitempty"`
	Model   string `json:"model,omitempty"`
	Quota   Quota  `json:"quota"`
}

// Quota 助手今天的用量和每日额度（token，0 表示不限）。
type Quota struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}

func (s *Service) quota() Quota {
	t, limit := s.ai.Usage(ai.FeatAssistant)
	return Quota{Used: t.Tokens, Limit: limit}
}

// Status 见 Status 类型。
func (s *Service) Status(who ops.Caller) Status {
	v := s.ai.View()
	st := Status{Enabled: v.Enabled, Local: who.Local, LAN: v.LANAssistant, Missing: v.Missing, Quota: s.quota()}
	st.Allowed = st.Enabled && (who.Local || st.LAN)
	if st.Enabled {
		st.Preset, st.Model = v.Provider.Preset, v.Provider.Model
	}
	return st
}

// allowView 这个请求能不能看、删助手的会话。不要求 AI 配好：停用 AI 之后也能看历史、清空会话。
func (s *Service) allowView(who ops.Caller) error {
	if !who.Source.Human() {
		return ops.Errorf(403, "只有人能用助手")
	}
	if !who.Local && !s.ai.LANAssistant() {
		return ops.Errorf(403, "主机关闭了「局域网可用 AI 助手」，局域网登录的设备不能用助手")
	}
	return nil
}

// allow 这个请求能不能用助手（开会话、生成发送预览）：还要 AI 配好了。发消息由 start 经 ai.Begin 检查。
func (s *Service) allow(who ops.Caller) error {
	if err := s.allowView(who); err != nil {
		return err
	}
	if !s.ai.Enabled() {
		return ops.Wrap(400, ai.ErrDisabled)
	}
	return nil
}

// visible 这个会话对请求的人可见：本机都看得见；局域网的设备只看得见局域网开的会话。
func visible(who ops.Caller, sess *Session) bool { return who.Local || !sess.Origin.Local }

// get 取会话并检查权限。
func (s *Service) get(who ops.Caller, id string) (*Session, error) {
	if err := s.allowView(who); err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess := s.sessions[id]
	s.mu.Unlock()
	if sess == nil || !visible(who, sess) {
		return nil, errNotFound()
	}
	return sess, nil
}

// CanDecide 这个人能不能处理这个提案（网页的确认、拒绝接口用）：助手提出的提案，局域网的设备只能处理
// 局域网开的会话里的，而且主机开着「局域网可用 AI 助手」。其余的交给操作层判断。
func (s *Service) CanDecide(who ops.Caller, p *ops.Proposal) error {
	if p.Caller.Source != ops.SrcAssistant || who.Local {
		return nil
	}
	if !s.ai.LANAssistant() {
		return ops.Errorf(403, "主机关闭了「局域网可用 AI 助手」，局域网登录的设备不能处理助手的提案")
	}
	if p.Caller.Local {
		return ops.Errorf(403, "这个提案来自主机上的会话，只能在主机上处理")
	}
	return nil
}

// ---------- 会话的增删查 ----------

// Summary 会话列表的一项。
type Summary struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Inst    string    `json:"inst,omitempty"`
	Updated time.Time `json:"updated"`
	Origin  string    `json:"origin"` // local / lan
	Actor   string    `json:"actor"`
	Busy    bool      `json:"busy,omitempty"`
	Waiting bool      `json:"waiting,omitempty"`
}

// List 会话列表，最近动过的在前。
func (s *Service) List(who ops.Caller) ([]Summary, error) {
	if err := s.allowView(who); err != nil {
		return nil, err
	}
	s.mu.Lock()
	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if visible(who, sess) {
			all = append(all, sess)
		}
	}
	s.mu.Unlock()
	out := []Summary{}
	for _, sess := range all {
		sess.mu.Lock()
		out = append(out, Summary{ID: sess.ID, Title: sess.Title, Inst: sess.Inst, Updated: sess.Updated, Origin: origin(sess.Origin),
			Actor: sess.Origin.Actor, Busy: sess.busy, Waiting: sess.Wait != nil})
		sess.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

func origin(o Origin) string {
	if o.Local {
		return "local"
	}
	return "lan"
}

// Create 开一个新会话（可以关联一个实例）。系统提示和工具清单在这里定下来，按开会话的人取：
// 本机开的 Caller.Local 为 true，局域网开的为 false（「仅本机」的操作由操作层据此判断）。
func (s *Service) Create(who ops.Caller, inst string) (*View, error) {
	if err := s.allow(who); err != nil {
		return nil, err
	}
	inst = strings.TrimSpace(inst)
	if len([]rune(inst)) > 100 {
		return nil, ops.Errorf(400, "实例名太长")
	}
	cl, err := s.ai.Client(true)
	if err != nil {
		return nil, ops.Wrap(400, err)
	}
	now := s.now()
	sess := &Session{ID: newID("s_"), Inst: inst, Created: now, Updated: now, Origin: Origin{Local: who.Local, Actor: who.Actor},
		AI: s.ai.Current(), Items: []*Item{}}
	if sess.Origin.Actor == "" {
		sess.Origin.Actor = "本机"
	}
	sess.Title = "新会话"
	if inst != "" {
		sess.Title = "新会话 · " + inst
	}
	tl, names := tools(s.reg, sess.caller())
	sess.Tools = names
	conv, err := cl.NewConversation(sessionPrompt(inst), tl)
	if err != nil {
		return nil, ops.Wrap(500, err)
	}
	sess.conv = conv
	sess.mu.Lock()
	err = s.saveLocked(sess)
	v := s.viewLocked(sess)
	sess.mu.Unlock()
	if err != nil {
		return nil, ops.Wrap(500, err)
	}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	s.prune()
	return v, nil
}

// prune 只留最近的 MaxSessions 个会话。
func (s *Service) prune() {
	s.mu.Lock()
	all := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		all = append(all, sess)
	}
	s.mu.Unlock()
	if len(all) <= MaxSessions {
		return
	}
	sort.Slice(all, func(i, j int) bool { return updated(all[i]).After(updated(all[j])) })
	for _, sess := range all[MaxSessions:] {
		s.remove(sess)
	}
}

func updated(sess *Session) time.Time {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.Updated
}

// remove 删掉一个会话：停掉正在跑的回复、取消没处理的提案、删文件、断开订阅。
func (s *Service) remove(sess *Session) {
	s.mu.Lock()
	delete(s.sessions, sess.ID)
	s.mu.Unlock()
	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	var props []string
	if sess.Wait != nil {
		for pid := range sess.Wait.Props {
			props = append(props, pid)
		}
	}
	sess.mu.Unlock()
	for _, pid := range props {
		_ = s.reg.Reject(ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}, pid, "会话被删除了")
	}
	_ = os.Remove(s.path(sess.ID))
	sess.hub.closeAll()
}

// Delete 删掉一个会话。
func (s *Service) Delete(who ops.Caller, id string) error {
	sess, err := s.get(who, id)
	if err != nil {
		return err
	}
	s.remove(sess)
	return nil
}

// Clear 清空会话：本机清空全部，局域网的设备只清空局域网开的。返回删了几个。
func (s *Service) Clear(who ops.Caller) (int, error) {
	if err := s.allowView(who); err != nil {
		return 0, err
	}
	s.mu.Lock()
	var del []*Session
	for _, sess := range s.sessions {
		if visible(who, sess) {
			del = append(del, sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range del {
		s.remove(sess)
	}
	return len(del), nil
}

// ---------- 给界面看的会话 ----------

// View 一个会话的完整内容（工具交还给 AI 的原文不在里面，要看时用 Sent 取）。
type View struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Inst      string    `json:"inst,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Origin    string    `json:"origin"`
	Actor     string    `json:"actor"`
	Model     string    `json:"model"`
	Preset    string    `json:"preset"`
	Items     []*Item   `json:"items"`
	Busy      bool      `json:"busy"`
	Waiting   bool      `json:"waiting"`          // 有确认卡片还没处理
	NeedReply bool      `json:"needReply"`        // 上一次回复没完成（停止、出错），可以「继续回复」
	Stale     string    `json:"stale,omitempty"`  // AI 设置改过了，这个会话不能接着用（写明原因）
	Long      bool      `json:"long,omitempty"`   // 会话很长了，建议开新会话
	Attach    int       `json:"attach,omitempty"` // 打包好的排错资料有多少字（发送时会附上）
	Quota     Quota     `json:"quota"`
}

// stale AI 设置改过了，这个会话还能不能接着用：换了服务商、协议或地址不行；Claude 换了模型也不行（思考块绑定模型）。
func (s *Service) stale(sess *Session) string {
	cur := s.ai.Current()
	a := sess.AI
	switch {
	case cur.Preset == "":
		return "AI 已停用"
	case cur.Preset != a.Preset || cur.Protocol != a.Protocol || cur.BaseURL != a.BaseURL:
		return "AI 的服务商或接口地址改过了，这个会话不能接着用，请开新会话"
	case cur.Protocol == ai.Anthropic && cur.Model != a.Model:
		return "Claude 的模型换过了，这个会话不能接着用，请开新会话"
	}
	return ""
}

func (s *Service) viewLocked(sess *Session) *View {
	v := &View{ID: sess.ID, Title: sess.Title, Inst: sess.Inst, Created: sess.Created, Updated: sess.Updated, Origin: origin(sess.Origin),
		Actor: sess.Origin.Actor, Model: s.ai.Current().Model, Preset: sess.AI.Preset, Busy: sess.busy, Waiting: sess.Wait != nil,
		Stale: s.stale(sess), Long: sess.Long, Quota: s.quota()}
	if v.Model == "" {
		v.Model = sess.AI.Model
	}
	v.NeedReply = sess.Wait == nil && !sess.busy && sess.conv != nil && sess.conv.NeedsReply()
	if sess.attach != nil {
		v.Attach = len([]rune(sess.attach.text))
	}
	v.Items = make([]*Item, len(sess.Items))
	for i, it := range sess.Items {
		v.Items[i] = slim(it)
	}
	return v
}

// slim 给界面的一条：去掉工具交还给 AI 的原文（太大，要看时单独取）。
func slim(it *Item) *Item {
	c := *it
	c.Sent = ""
	if len(it.Tools) > 0 {
		c.Tools = make([]*ToolItem, len(it.Tools))
		for i, t := range it.Tools {
			tc := *t
			tc.Sent = ""
			c.Tools[i] = &tc
		}
	}
	return &c
}

// Get 取一个会话的完整内容。
func (s *Service) Get(who ops.Caller, id string) (*View, error) {
	sess, err := s.get(who, id)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return s.viewLocked(sess), nil
}

// Sent 发给 AI 的原文（「发送内容预览」）：一条用户的话（含附加的排错资料），或者一次工具调用交还的结果。
func (s *Service) Sent(who ops.Caller, id, item, tool string) (string, error) {
	sess, err := s.get(who, id)
	if err != nil {
		return "", err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	it := sess.item(item)
	if it == nil {
		return "", ops.Errorf(404, "没有这一条")
	}
	if tool == "" {
		if it.Sent != "" {
			return it.Sent, nil
		}
		return it.Text, nil
	}
	for _, t := range it.Tools {
		if t.ID == tool {
			return t.Sent, nil
		}
	}
	return "", ops.Errorf(404, "没有这次工具调用")
}

// ---------- 存盘 ----------

type sessionFile struct {
	V int `json:"v"`
	*Session
	NextID int `json:"nextId"`
}

func (s *Service) path(id string) string { return filepath.Join(s.dir, id+".json") }

// saveLocked 写会话文件（调用方持有 sess.mu）：会话历史用 Conversation.Save 编好原样放进去（不转义 HTML，
// 读回来发出的请求逐字节不变）。
func (s *Service) saveLocked(sess *Session) error {
	if sess.conv != nil {
		var buf bytes.Buffer
		if err := sess.conv.Save(&buf); err != nil {
			return err
		}
		sess.Conv = json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n"))
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(sessionFile{V: 1, Session: sess, NextID: sess.nextID}); err != nil {
		return err
	}
	tmp := s.path(sess.ID) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(sess.ID)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// loadAll 读回保存的会话。读不懂的文件跳过（不删，留给人看）。程序重启过，没处理的提案已经不在了：
// 读回时把它们记成过期。
func (s *Service) loadAll() {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		sess, err := loadSession(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		s.sessions[sess.ID] = sess
	}
}

func loadSession(path string) (*Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := sessionFile{Session: &Session{}}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	sess := f.Session
	if f.V != 1 || sess.ID == "" || len(sess.Conv) == 0 {
		return nil, errors.New("会话文件格式不对")
	}
	conv, err := ai.LoadConversation(bytes.NewReader(sess.Conv))
	if err != nil {
		return nil, err
	}
	sess.conv = conv
	sess.nextID = f.NextID
	if sess.Items == nil {
		sess.Items = []*Item{}
	}
	if w := sess.Wait; w != nil { // 程序重启过：提案都不在了
		for pid, callID := range w.Props {
			if _, ok := w.Results[callID]; !ok {
				w.Results[callID] = ai.ToolResult{ID: callID, IsError: true, Content: outcomeText(string(ops.OutExpired), "", "")}
				setOutcome(sess, pid, string(ops.OutExpired), "", "", "程序重启过，提案已失效")
			}
		}
	}
	return sess, nil
}
