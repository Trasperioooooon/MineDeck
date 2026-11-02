package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"minedeck/internal/assistant"
	"minedeck/internal/ops"
)

// StatusTool 查提案处理结果的工具。它不是操作层的操作，只在 MCP 接口里有：外部 AI 收不到确认卡片的结果，要自己来查。
const StatusTool = "proposal_status"

// maxPendingMCP 同时等确认的 MCP 提案最多几个：外部 AI 一直提，也不会把应用内助手的提案挤掉（操作层一共留 100 个）。
const maxPendingMCP = 10

// keepRecord 提案的处理结果留多久（给 proposal_status 查）。
const keepRecord = time.Hour

// mcpNotes 写操作、危险操作的工具说明末尾的提示：确认卡片在 MineDeck 的界面上，不在客户端里。
var mcpNotes = assistant.ToolNotes{
	Write:  "（写操作：调用后不会马上执行，而是在 MineDeck 的界面上生成一张确认卡片，用户在 MineDeck 里确认后才执行；结果里有提案编号，用 proposal_status 查是否已执行）",
	Danger: "（危险操作：调用后不会马上执行，而是在 MineDeck 的界面上生成一张确认卡片，用户确认时还要再勾选一次；结果里有提案编号，用 proposal_status 查是否已执行）",
}

// caller 外部 AI 在操作层里的身份：来源 mcp，actor 是客户端报的名字。MCP 接口只收本机的连接，所以 Local 为 true
// （「仅本机」的写操作照样只生成提案，确认时还要本机的人来确认）。
func caller(id identity) ops.Caller {
	return ops.Caller{Source: ops.SrcMCP, Actor: id.actor(), Local: true}
}

// toolOps 给外部 AI 的操作：和应用内助手一样（仅人工的不给，去掉排错用不上的几个），工具名到操作的对照。
func (s *Service) toolOps() ([]*ops.Op, map[string]*ops.Op) {
	list := assistant.ToolOps(s.reg, caller(identity{}))
	byName := map[string]*ops.Op{}
	out := list[:0:0]
	for _, op := range list {
		name := ops.ToolName(op.Name)
		if name == StatusTool {
			continue
		}
		out = append(out, op)
		byName[name] = op
	}
	return out, byName
}

type toolDef struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"` // 2025-06-18 起才有
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations *annotations    `json:"annotations,omitempty"` // 2025-03-26 起才有
}

// annotations 工具行为的提示（客户端拿来决定要不要先问用户；不能当真，权限都在操作层）。
type annotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
}

func boolp(b bool) *bool { return &b }

// toolDefs tools/list 的工具清单，按登记顺序（顺序固定，客户端的缓存和前缀缓存才能命中），最后是 proposal_status。
func (s *Service) toolDefs(ver string) []toolDef {
	list, _ := s.toolOps()
	out := make([]toolDef, 0, len(list)+1)
	for _, op := range list {
		d := toolDef{Name: ops.ToolName(op.Name), Description: assistant.ToolDesc(op, mcpNotes), InputSchema: op.AISchema()}
		if ver >= "2025-06-18" {
			d.Title = op.Title
		}
		if ver >= "2025-03-26" {
			switch op.Level {
			case ops.Read:
				d.Annotations = &annotations{ReadOnlyHint: boolp(true)}
			case ops.Write:
				d.Annotations = &annotations{ReadOnlyHint: boolp(false), DestructiveHint: boolp(false)}
			case ops.Danger:
				d.Annotations = &annotations{ReadOnlyHint: boolp(false), DestructiveHint: boolp(true)}
			}
		}
		out = append(out, d)
	}
	st := toolDef{Name: StatusTool, Description: statusDesc, InputSchema: statusSchema}
	if ver >= "2025-06-18" {
		st.Title = "查看提案的处理结果"
	}
	if ver >= "2025-03-26" {
		st.Annotations = &annotations{ReadOnlyHint: boolp(true)}
	}
	return append(out, st)
}

const statusDesc = "查看写操作、危险操作的提案现在怎么样了：还在等用户在 MineDeck 里确认、已确认并执行（带执行结果）、执行失败、被拒绝（带理由）、过期或作废。" +
	"id 是调用写操作的工具后得到的提案编号；不填就列出最近的提案。wait 可以等最多 30 秒，提案在这期间处理完就马上返回。"

var statusSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"id":{"type":"string","description":"提案编号，如 p_8f2c1a9e3b7d；不填就列出最近一小时的提案","maxLength":40},` +
	`"wait":{"type":"integer","description":"最多等几秒（0 到 30，默认 0：马上返回）","minimum":0,"maximum":30}}}`)

// toolResult tools/call 的结果（text 是交给外部 AI 的内容）。
func toolResult(ver, text string, isErr bool) map[string]any {
	res := map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isErr}
	if ver == Modern {
		res["resultType"] = "complete"
	}
	return res
}

// callTool 调用一个工具：读操作直接执行；写操作和危险操作由操作层生成提案，不执行；仅人工的不在清单里。
// 参数不对、执行失败都按工具的错误（isError）交回，外部 AI 可以改了再试；没有这个工具才是协议错误。
func (s *Service) callTool(ctx context.Context, id identity, ver string, params map[string]json.RawMessage) (map[string]any, *rpcError) {
	var name string
	if json.Unmarshal(params["name"], &name) != nil || name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call 缺少工具名 name"}
	}
	args := params["arguments"]
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}
	if name == StatusTool {
		text, isErr := s.statusTool(ctx, args)
		return toolResult(ver, text, isErr), nil
	}
	_, byName := s.toolOps()
	op := byName[name]
	if op == nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "没有这个工具：" + name}
	}
	if (op.Level == ops.Write || op.Level == ops.Danger) && len(s.Pending()) >= maxPendingMCP {
		return toolResult(ver, fmt.Sprintf("已经有 %d 个提案在等用户在 MineDeck 里确认，先等用户处理完（可以用 proposal_status 查），再提新的。", maxPendingMCP), true), nil
	}
	out, err := s.reg.Do(ctx, caller(id), op.Name, ops.Args(args))
	if p, ok := out.(*ops.Proposal); ok && p != nil && err == nil {
		s.track(p)
		return toolResult(ver, s.proposalText(p), false), nil
	}
	if err != nil {
		return toolResult(ver, "执行失败："+s.redact.Text(err.Error()), true), nil
	}
	return toolResult(ver, assistant.FormatResult(s.redact, out), false), nil
}

// proposalText 生成了提案：告诉外部 AI 还没有执行、要做什么、要用户在 MineDeck 里确认、怎么查结果。
func (s *Service) proposalText(p *ops.Proposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "已生成提案 %s，还没有执行。\n", p.ID)
	if p.Plan != nil {
		fmt.Fprintf(&b, "要做的事：%s\n", s.redact.Text(p.Plan.Summary))
		for _, c := range p.Plan.Changes {
			if c.Before == "" {
				fmt.Fprintf(&b, "改动：%s 设成 %s\n", s.redact.Text(c.What), s.redact.Text(c.After))
			} else {
				fmt.Fprintf(&b, "改动：%s 从 %s 改成 %s\n", s.redact.Text(c.What), s.redact.Text(c.Before), s.redact.Text(c.After))
			}
		}
		if p.Plan.Warning != "" {
			fmt.Fprintf(&b, "警告：%s\n", s.redact.Text(p.Plan.Warning))
		}
		if p.Plan.Diff != "" {
			b.WriteString("文本文件的逐行对比显示在确认卡片上。\n")
		}
	}
	if p.Level == ops.Danger {
		b.WriteString("这是危险操作，用户确认时还要再勾选一次。\n")
	}
	if p.Consent != nil {
		fmt.Fprintf(&b, "确认时要用户本人勾选「%s」，你不能替用户同意。\n", p.Consent.Text)
	}
	mins := int(p.Expires.Sub(p.Created).Round(time.Minute) / time.Minute)
	fmt.Fprintf(&b, "要用户在 MineDeck 的界面上确认：标题栏右上角「AI 助手」里会出现这张确认卡片。提案 %d 分钟内有效，过期或被拒绝就不会执行。\n", mins)
	fmt.Fprintf(&b, "请告诉用户去 MineDeck 里确认，之后用 %s（id 填 %s）查结果；没收到执行结果之前，不要说已经改好了。", StatusTool, p.ID)
	return b.String()
}

// ---------- 提案的记录和 proposal_status ----------

// record MCP 提出的一个提案：处理完之前 outcome 为空。
type record struct {
	id, op, title, actor string
	level                ops.Level
	created, expires     time.Time
	outcome              ops.Outcome
	text                 string // 处理完交给外部 AI 的说法（执行结果已脱敏、截断）
	done                 time.Time
	seq                  uint64 // 第几个提出的：列表按它排先后（Windows 上前后脚提出的两个 created 常常一样，见 Client.seq）
}

func (s *Service) track(p *ops.Proposal) {
	now := s.now()
	s.mu.Lock()
	for id, r := range s.props {
		if now.Sub(r.created) > keepRecord {
			delete(s.props, id)
		}
	}
	s.seq++
	s.props[p.ID] = &record{id: p.ID, op: p.Op, title: p.Title, actor: p.Caller.Actor, level: p.Level, created: p.Created, expires: p.Expires, seq: s.seq}
	s.mu.Unlock()
	s.publish()
	// 过期时再通知一次界面（操作层不会为过期发通知）：卡片按时消失
	if d := time.Until(p.Expires); d > 0 {
		time.AfterFunc(d+500*time.Millisecond, s.publish)
	}
}

// onResolve 操作层通知：MCP 提出的提案处理完了（确认后执行完、执行失败、拒绝、过期、作废）。
func (s *Service) onResolve(r ops.Resolution) {
	p := r.Proposal
	if p == nil || p.Caller.Source != ops.SrcMCP {
		return
	}
	errText := ""
	if r.Err != nil {
		errText = s.redact.Text(r.Err.Error())
	}
	var text string
	switch r.Outcome {
	case ops.OutDone:
		text = "用户确认了，已经执行。结果：\n" + assistant.FormatResult(s.redact, r.Result)
	case ops.OutFailed:
		text = "用户确认了，但执行失败：" + errText
	case ops.OutRejected:
		text = "用户拒绝了，没有执行。"
		if r.Reason != "" {
			text += "用户说：" + s.redact.Text(r.Reason)
		}
	case ops.OutExpired:
		text = "在有效期内没有被确认，已经过期，没有执行。需要的话可以重新提出。"
	case ops.OutInvalid:
		text = "作废了，没有执行（" + errText + "）。请先重新查看最新的情况，再决定要不要重新提出。"
	default:
		text = "没有执行。"
	}
	s.mu.Lock()
	rec := s.props[p.ID]
	if rec == nil {
		rec = &record{id: p.ID, op: p.Op, title: p.Title, actor: p.Caller.Actor, level: p.Level, created: p.Created, expires: p.Expires}
		s.props[p.ID] = rec
	}
	rec.outcome, rec.text, rec.done = r.Outcome, text, s.now()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	s.publish()
}

// statusTool proposal_status：查一个提案（id），或者列出最近的提案；wait 秒内处理完就马上返回。
func (s *Service) statusTool(ctx context.Context, args json.RawMessage) (string, bool) {
	var a struct {
		ID   *string  `json:"id"`
		Wait *float64 `json:"wait"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "参数不对：应为 {\"id\": \"p_…\", \"wait\": 秒数}", true
	}
	wait := 0.0
	if a.Wait != nil {
		if *a.Wait < 0 || *a.Wait > 30 {
			return "参数 wait 应在 0 到 30 之间", true
		}
		wait = *a.Wait
	}
	id := ""
	if a.ID != nil {
		id = strings.TrimSpace(*a.ID)
	}
	if id == "" {
		return s.listText(), false
	}
	deadline := time.Now().Add(time.Duration(wait * float64(time.Second)))
	for {
		s.mu.Lock()
		rec := s.props[id]
		var ch chan struct{}
		if rec != nil {
			r := *rec
			rec = &r
		}
		ch = s.changed
		s.mu.Unlock()
		if rec == nil {
			return "没有这个提案：编号不对、超过一小时了，或者 MineDeck 重启过（重启后没处理的提案都作废了）。", true
		}
		if rec.outcome != "" {
			return fmt.Sprintf("提案 %s（%s）：%s", rec.id, rec.title, rec.text), false
		}
		now := s.now()
		if !now.Before(rec.expires) {
			return fmt.Sprintf("提案 %s（%s）：在有效期内没有被确认，已经过期，没有执行。需要的话可以重新提出。", rec.id, rec.title), false
		}
		left := time.Until(deadline)
		if left <= 0 {
			return fmt.Sprintf("提案 %s（%s）还在等用户在 MineDeck 里确认，大约 %d 秒后过期。", rec.id, rec.title, int(rec.expires.Sub(now).Seconds())), false
		}
		t := time.NewTimer(left)
		select {
		case <-ch:
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return "请求被取消了。", true
		}
		t.Stop()
	}
}

// listText 最近一小时的提案，新的在前（最多 20 个）。
func (s *Service) listText() string {
	now := s.now()
	s.mu.Lock()
	var recs []record
	for _, r := range s.props {
		if now.Sub(r.created) <= keepRecord {
			recs = append(recs, *r)
		}
	}
	s.mu.Unlock()
	if len(recs) == 0 {
		return "最近一小时没有经 MCP 提出的提案。"
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].seq > recs[j].seq })
	if len(recs) > 20 {
		recs = recs[:20]
	}
	var b strings.Builder
	b.WriteString("最近的提案（新的在前）：")
	for _, r := range recs {
		st := map[ops.Outcome]string{ops.OutDone: "已确认并执行", ops.OutFailed: "确认了但执行失败", ops.OutRejected: "被拒绝",
			ops.OutExpired: "已过期", ops.OutInvalid: "已作废"}[r.outcome]
		if r.outcome == "" {
			st = "等用户确认"
			if !now.Before(r.expires) {
				st = "已过期"
			}
		}
		fmt.Fprintf(&b, "\n- %s %s：%s", r.id, r.title, st)
	}
	b.WriteString("\n要看某一个的详情和执行结果，用 id 再查一次。")
	return b.String()
}

// voidPending 还没处理的 MCP 提案经操作层拒绝作废（关闭 MCP 接口时）。
func (s *Service) voidPending(reason string) {
	who := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	for _, p := range s.Pending() {
		_ = s.reg.Reject(who, p.ID, reason)
	}
}

// ---------- 给界面：等确认的外部 AI 提案 ----------

// PendingItem 一个等确认的 MCP 提案（界面上用应用内助手的确认卡片显示）。
type PendingItem struct {
	ID      string       `json:"id"`
	Op      string       `json:"op"`
	Title   string       `json:"title"`
	Level   string       `json:"level"` // W / D
	Args    ops.Args     `json:"args"`
	Plan    *ops.Plan    `json:"plan"`
	Consent *ops.Consent `json:"consent,omitempty"`
	Actor   string       `json:"actor"` // 客户端报的名字
	Created time.Time    `json:"created"`
	Expires time.Time    `json:"expires"`
}

// Pending 还在等确认、没有过期的 MCP 提案，按提出的时间排序。
func (s *Service) Pending() []PendingItem {
	out := []PendingItem{}
	for _, p := range s.reg.Pending("") {
		if p.Caller.Source != ops.SrcMCP {
			continue
		}
		out = append(out, PendingItem{ID: p.ID, Op: p.Op, Title: p.Title, Level: p.Level.String(), Args: p.Args, Plan: p.Plan, Consent: p.Consent,
			Actor: p.Caller.Actor, Created: p.Created, Expires: p.Expires})
	}
	return out
}

// CanDecide 外部 AI（MCP）提出的提案只能由这台电脑上的人处理（网页的确认、拒绝接口用）；别的提案不管。
func CanDecide(who ops.Caller, p *ops.Proposal) error {
	if p != nil && p.Caller.Source == ops.SrcMCP && !who.Local {
		return ops.Errorf(403, "外部 AI（MCP）提出的操作只能在运行 MineDeck 的那台电脑上确认或拒绝")
	}
	return nil
}

// Subscribe 等确认的 MCP 提案有变化时收到一个信号（界面的事件流用，信号会合并）。cancel 取消订阅。
func (s *Service) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

func (s *Service) publish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default: // 还有没取走的信号：合并
		}
	}
}
