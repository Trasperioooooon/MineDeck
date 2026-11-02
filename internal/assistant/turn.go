package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// maxRounds 一轮最多请求 AI 几次（工具调用到上限后还要再问一次让它收尾）。
const maxRounds = maxTools + 3

// attachTTL 打包好的排错资料多久内发送有效（过期了要重新预览，免得发出去的和看到的不一样）。
const attachTTL = 30 * time.Minute

// ---------- 事件 ----------

// 发给界面的事件：
//
//	{"type":"session","session":View}        订阅时先发一份完整的会话
//	{"type":"item","item":Item}              新增或更新一条（按 id 替换）
//	{"type":"delta","id":"i3","text":"…"}    AI 回复的文字片段（追加到这一条）
//	{"type":"state","busy":…,"waiting":…,"needReply":…,"quota":…}
//	{"type":"retry","attempt":1,"err":"…"}  服务商出错，正在重试
type event map[string]any

func (s *Service) publishItem(sess *Session, it *Item) {
	sess.mu.Lock()
	c := slim(it)
	sess.mu.Unlock()
	sess.hub.publish(event{"type": "item", "item": c})
}

func (s *Service) publishState(sess *Session) {
	sess.mu.Lock()
	v := s.viewLocked(sess)
	sess.mu.Unlock()
	sess.hub.publish(event{"type": "state", "busy": v.Busy, "waiting": v.Waiting, "needReply": v.NeedReply, "stale": v.Stale,
		"long": v.Long, "attach": v.Attach, "title": v.Title, "quota": v.Quota})
}

// Subscribe 订阅会话的事件：先给一份完整的会话，之后是增量。cancel 取消订阅。
func (s *Service) Subscribe(who ops.Caller, id string) (*View, <-chan []byte, func(), error) {
	sess, err := s.get(who, id)
	if err != nil {
		return nil, nil, nil, err
	}
	ch, cancel := sess.hub.subscribe()
	sess.mu.Lock()
	v := s.viewLocked(sess)
	sess.mu.Unlock()
	return v, ch, cancel, nil
}

// ---------- 发消息、继续、停止 ----------

// Send 用户发一条消息，在后台开始这一轮（结果经事件推给界面）。attach 为 true 时附上之前预览过的排错资料。
// 有确认卡片还没处理时，没处理的提案作废（「用户没有处理就发了新消息」），结果一起交还给 AI。
func (s *Service) Send(who ops.Caller, id, text string, attach bool) error {
	sess, err := s.get(who, id)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > 20000 {
		return ops.Errorf(400, "消息太长了（最多 2 万字）")
	}
	sess.mu.Lock()
	var pack string
	if attach {
		if sess.attach == nil || s.now().Sub(sess.attach.created) > attachTTL {
			sess.mu.Unlock()
			return ops.Errorf(409, "排错资料已经过期，请重新点「让 AI 分析」生成预览")
		}
		pack = sess.attach.text
		if text == "" {
			text = "请帮我分析这台服务器的问题。"
		}
	}
	sess.mu.Unlock()
	if text == "" {
		return ops.Errorf(400, "消息是空的")
	}
	return s.start(who, sess, func(ctx context.Context) bool { return s.addUser(sess, text, pack) })
}

// Continue 上一次回复没完成（停止、出错、空回复），或者确认卡片都处理完了：接着让 AI 回复。
func (s *Service) Continue(who ops.Caller, id string) error {
	sess, err := s.get(who, id)
	if err != nil {
		return err
	}
	return s.start(who, sess, nil)
}

// Stop 停止正在进行的回复（已经执行的工具不撤回）。
func (s *Service) Stop(who ops.Caller, id string) error {
	sess, err := s.get(who, id)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	sess.mu.Unlock()
	return nil
}

// start 检查能不能开始，然后在后台跑这一轮。prepare 在回复之前执行（追加用户的话），返回 false 表示不用再请求 AI。
func (s *Service) start(who ops.Caller, sess *Session, prepare func(ctx context.Context) bool) error {
	if err := s.ai.Begin(ai.FeatAssistant); err != nil {
		var qe *ai.QuotaError
		if errors.As(err, &qe) {
			return ops.Wrap(429, err)
		}
		return ops.Wrap(400, err)
	}
	sess.mu.Lock()
	if sess.busy {
		sess.mu.Unlock()
		return ErrBusy
	}
	if st := s.stale(sess); st != "" {
		sess.mu.Unlock()
		return ops.Errorf(409, "%s", st)
	}
	if prepare == nil && sess.Wait == nil && !sess.conv.NeedsReply() {
		sess.mu.Unlock()
		return ops.Errorf(409, "没有要继续的回复")
	}
	if prepare == nil && sess.Wait != nil && len(sess.Wait.Results) < len(sess.Wait.Calls) && !s.expiredAllLocked(sess) {
		sess.mu.Unlock()
		return ops.Errorf(409, "还有确认卡片没处理")
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess.busy, sess.cancel = true, cancel
	sess.mu.Unlock()
	s.publishState(sess)
	go s.turn(ctx, who, sess, prepare)
	return nil
}

// expiredAllLocked 没处理的提案是不是都过期了（过期的可以直接继续）。
func (s *Service) expiredAllLocked(sess *Session) bool {
	now := s.now()
	for pid, callID := range sess.Wait.Props {
		if _, ok := sess.Wait.Results[callID]; ok {
			continue
		}
		if p := s.reg.Proposal(pid); p != nil && now.Before(p.Expires) {
			return false
		}
	}
	return true
}

// turn 后台跑一轮：先把等着的确认卡片收尾（没处理的作废），追加用户的话，再循环请求 AI、执行工具。
func (s *Service) turn(ctx context.Context, who ops.Caller, sess *Session, prepare func(ctx context.Context) bool) {
	defer func() {
		sess.mu.Lock()
		sess.busy, sess.cancel = false, nil
		sess.Updated = s.now()
		_ = s.saveLocked(sess)
		sess.mu.Unlock()
		s.publishState(sess)
	}()
	sess.mu.Lock()
	if w := sess.Wait; w != nil && prepare == nil { // 确认卡片处理完了，接着这一轮
		sess.rounds, sess.used = w.Rounds, w.Tools
	} else { // 用户的新消息：新的一轮
		sess.rounds, sess.used = 0, 0
	}
	sess.mu.Unlock()
	if !s.settleWait(who, sess) {
		return
	}
	if prepare != nil && !prepare(ctx) {
		return
	}
	s.loop(ctx, sess)
}

// settleWait 把等着的这一轮收尾：没处理的提案作废（经操作层拒绝，记一条操作记录），全部结果一起交还给 AI。
func (s *Service) settleWait(who ops.Caller, sess *Session) bool {
	sess.mu.Lock()
	w := sess.Wait
	var open []string
	if w != nil {
		for pid, callID := range w.Props {
			if _, ok := w.Results[callID]; !ok {
				open = append(open, pid)
			}
		}
	}
	sess.mu.Unlock()
	if w == nil {
		return true
	}
	for _, pid := range open {
		// 先在会话里记成作废（操作层的通知回来时看到已经有结果，就不再改）
		out := "canceled"
		if p := s.reg.Proposal(pid); p == nil || !s.now().Before(p.Expires) {
			out = string(ops.OutExpired)
		}
		sess.mu.Lock()
		callID := w.Props[pid]
		w.Results[callID] = ai.ToolResult{ID: callID, IsError: true, Content: outcomeText(out, "", "")}
		it := setOutcome(sess, pid, out, "", who.Actor, "")
		if it != nil {
			setSent(it, callID, w.Results[callID].Content)
		}
		sess.mu.Unlock()
		if it != nil {
			s.publishItem(sess, it)
		}
		_ = s.reg.Reject(who, pid, "用户没有处理这张确认卡片，接着发了新消息")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if err := sess.conv.AddToolResults(orderedResults(w)); err != nil {
		s.noticeLocked(sess, "error", "交还工具结果失败："+err.Error())
		return false
	}
	sess.Wait = nil
	return true
}

func setSent(it *Item, callID, content string) {
	for _, t := range it.Tools {
		if t.ID == callID {
			t.Sent = content
		}
	}
}

func orderedResults(w *Wait) []ai.ToolResult {
	out := make([]ai.ToolResult, 0, len(w.Calls))
	for _, c := range w.Calls {
		r, ok := w.Results[c.ID]
		if !ok {
			r = ai.ToolResult{ID: c.ID, IsError: true, Content: "没有执行。"}
		}
		out = append(out, r)
	}
	return out
}

// addUser 追加用户的话：脱敏后发给 AI（附上排错资料的话一起）；界面上显示原话。
func (s *Service) addUser(sess *Session, text, pack string) bool {
	masked := s.redact.Text(text)
	sent := masked
	if pack != "" {
		sent += "\n\n" + pack
	}
	sess.mu.Lock()
	if err := sess.conv.AddUser(sent); err != nil {
		s.noticeLocked(sess, "error", "发不出去："+err.Error())
		sess.mu.Unlock()
		return false
	}
	it := sess.newItem("user", s.now())
	it.Text = text
	if sent != text {
		it.Sent = sent
	}
	it.Masked = masked != text
	if pack != "" {
		it.Attach = utf8.RuneCountInString(pack)
		sess.attach = nil
	}
	if sess.Title == "新会话" || strings.HasPrefix(sess.Title, "新会话 · ") {
		sess.Title = title(text, sess.Inst, pack != "")
	}
	_ = s.saveLocked(sess)
	sess.mu.Unlock()
	s.publishItem(sess, it)
	return true
}

func title(text, inst string, analyze bool) string {
	if analyze && inst != "" {
		return "分析「" + inst + "」的问题"
	}
	t := strings.Join(strings.Fields(text), " ")
	if r := []rune(t); len(r) > 30 {
		t = string(r[:30]) + "…"
	}
	return t
}

func (s *Service) noticeLocked(sess *Session, level, text string) *Item {
	it := sess.newItem("notice", s.now())
	it.Level, it.Text = level, text
	sess.hub.publish(event{"type": "item", "item": slim(it)})
	return it
}

func (s *Service) notice(sess *Session, level, text string) {
	sess.mu.Lock()
	s.noticeLocked(sess, level, text)
	sess.mu.Unlock()
}

// loop 循环请求 AI、执行工具，直到 AI 不再调用工具、要等确认卡片，或者出错。
func (s *Service) loop(ctx context.Context, sess *Session) {
	for {
		sess.mu.Lock()
		if sess.rounds >= maxRounds {
			sess.mu.Unlock()
			s.notice(sess, "warn", "这一轮请求 AI 的次数到上限了，先停在这里。可以点「继续回复」，或者换个问法。")
			return
		}
		sess.rounds++
		sess.mu.Unlock()

		if err := s.ai.Begin(ai.FeatAssistant); err != nil {
			s.fail(sess, nil, err)
			return
		}
		cl, err := s.ai.Client(true)
		if err != nil {
			s.fail(sess, nil, err)
			return
		}
		sess.mu.Lock()
		it := sess.newItem("assistant", s.now())
		conv := sess.conv
		sess.mu.Unlock()
		s.publishItem(sess, it)

		res, err := cl.Chat(ctx, conv, func(e ai.Event) {
			switch e.Type {
			case ai.EvText:
				sess.mu.Lock()
				it.Text += e.Text
				sess.mu.Unlock()
				sess.hub.publish(event{"type": "delta", "id": it.ID, "text": e.Text})
			case ai.EvToolStart:
				if e.Tool != nil {
					sess.hub.publish(event{"type": "tool_start", "id": it.ID, "name": e.Tool.Name})
				}
			case ai.EvRetry:
				sess.hub.publish(event{"type": "retry", "attempt": e.Attempt, "err": s.redact.Text(e.Err)})
			case ai.EvReasoning:
				sess.hub.publish(event{"type": "thinking", "id": it.ID})
			}
		})
		cost := s.ai.Record(ai.FeatAssistant, res) // 不管成没成功，花了的都要记
		sess.mu.Lock()
		if res != nil {
			u := res.Usage
			it.Usage, it.Cost, it.Model = &u, cost, res.Model
			if res.Usage.Prompt() > 100000 {
				sess.Long = true
			}
		}
		if err == nil && res.Stop == ai.StopRefusal {
			err = errRefused
		}
		if err == nil && conv.AddResult(res) != nil { // 空回复：Claude 空的 end_turn、OpenAI 兼容只给了思考内容
			err = errEmpty
		}
		if err != nil {
			sess.mu.Unlock()
			if ctx.Err() != nil { // 用户点了停止（开始输出以后停的，接入层报的是「中途断开」）
				err = context.Canceled
			}
			s.fail(sess, it, err)
			return
		}
		it.Text = res.Text
		if res.Stop == ai.StopMaxTokens && len(res.ToolCalls) == 0 {
			s.noticeLocked(sess, "warn", "回复太长，到了输出上限，后面的被截断了")
		}
		_ = s.saveLocked(sess)
		sess.mu.Unlock()
		s.publishItem(sess, it)
		if len(res.ToolCalls) == 0 {
			return
		}
		if !s.runTools(ctx, sess, it, res) {
			return // 等确认卡片
		}
	}
}

var (
	errRefused = errors.New("模型拒绝回答这个问题")
	errEmpty   = errors.New("模型没有给出回复")
)

// fail 这次请求没得到能用的回复：记账已经做了；最后一条是用户的话就撤回（界面把原话放回输入框，换个说法再问），
// 否则会话停在「工具结果已交回、等 AI 回复」，可以继续回复或者接着说话。
func (s *Service) fail(sess *Session, it *Item, err error) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if it != nil {
		if it.Text == "" && len(it.Tools) == 0 {
			sess.Items = removeItem(sess.Items, it.ID)
			sess.hub.publish(event{"type": "remove", "id": it.ID})
		} else {
			it.Partial = true
			sess.hub.publish(event{"type": "item", "item": slim(it)})
		}
	}
	if sess.conv.UndoUser() {
		for k := len(sess.Items) - 1; k >= 0; k-- {
			if sess.Items[k].Kind == "user" && !sess.Items[k].Withdrawn {
				sess.Items[k].Withdrawn = true
				sess.hub.publish(event{"type": "item", "item": slim(sess.Items[k])})
				break
			}
		}
	}
	var msg string
	var qe *ai.QuotaError
	switch {
	case errors.Is(err, context.Canceled):
		msg = "已停止"
	case errors.As(err, &qe), errors.Is(err, ai.ErrDisabled):
		msg = err.Error()
	case errors.Is(err, errRefused):
		msg = "模型拒绝回答这个问题。可以换个说法再问，或者开一个新会话。"
	case errors.Is(err, errEmpty):
		msg = "模型没有给出回复。可以换个说法再问一次。"
	default:
		msg = "请求 AI 失败：" + s.redact.Text(err.Error())
	}
	level := "error"
	if msg == "已停止" {
		level = "info"
	}
	s.noticeLocked(sess, level, msg)
	_ = s.saveLocked(sess)
}

func removeItem(items []*Item, id string) []*Item {
	out := items[:0]
	for _, it := range items {
		if it.ID != id {
			out = append(out, it)
		}
	}
	return out
}

// runTools 执行这次回复里的工具调用：读操作直接执行，写操作和危险操作生成提案。返回 true 表示结果都有了
// （已经交回，可以接着请求 AI），false 表示要等确认卡片（或者出错停下了）。
func (s *Service) runTools(ctx context.Context, sess *Session, it *Item, res *ai.Result) bool {
	sess.mu.Lock()
	w := &Wait{Item: it.ID, Calls: res.ToolCalls, Results: map[string]ai.ToolResult{}, Props: map[string]string{}}
	sess.Wait = w // 先挂上：确认得很快时，操作层的通知回来也找得到
	caller := sess.caller()
	sess.mu.Unlock()
	for _, tc := range res.ToolCalls {
		opName := sess.Tools[tc.Name]
		op := s.reg.Op(opName)
		ti := &ToolItem{ID: tc.ID, Name: tc.Name, Args: tc.Args, Status: "running"}
		if op != nil {
			ti.Op, ti.Title, ti.Level = op.Name, op.Title, op.Level.String()
		}
		sess.mu.Lock()
		it.Tools = append(it.Tools, ti)
		used := sess.used
		sess.mu.Unlock()
		s.publishItem(sess, it)

		var status, msg, content string
		var prop *ops.Proposal
		switch {
		case res.Stop == ai.StopMaxTokens:
			status, msg = "skipped", "回复到了输出上限被截断，这次调用的参数可能不完整，没有执行。"
		case tc.Bad != "":
			status, msg = "error", "参数格式不对（"+tc.Bad+"），没有执行。请重新调用。"
		case op == nil:
			status, msg = "error", "没有这个工具："+tc.Name
		case used >= maxTools:
			status, msg = "skipped", fmt.Sprintf("这一轮已经调用了 %d 次工具，到上限了，没有执行。请根据已经拿到的信息回答；需要的话请用户让你继续。", maxTools)
		default:
			sess.mu.Lock()
			sess.used++
			sess.mu.Unlock()
			out, err := s.reg.Do(ctx, caller, opName, ops.Args(tc.Args))
			switch p, ok := out.(*ops.Proposal); {
			case ok:
				status, prop = "pending", p
			case err != nil:
				status, msg = "error", "执行失败："+s.redact.Text(err.Error())
			default:
				status, content = "done", s.content(out)
			}
		}
		sess.mu.Lock()
		ti.Status = status
		switch {
		case prop != nil:
			ti.Proposal = proposalView(prop)
			w.Props[prop.ID] = tc.ID
		case status == "done":
			ti.Sent = content
			w.Results[tc.ID] = ai.ToolResult{ID: tc.ID, Content: content}
		default:
			ti.Error, ti.Sent = msg, msg
			w.Results[tc.ID] = ai.ToolResult{ID: tc.ID, IsError: true, Content: msg}
		}
		var early *ops.Resolution
		if prop != nil {
			if r, ok := sess.early[prop.ID]; ok {
				delete(sess.early, prop.ID)
				early = &r
			}
		}
		sess.mu.Unlock()
		if early != nil {
			s.applyResolution(sess, *early)
		}
		s.publishItem(sess, it)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	w.Tools, w.Rounds = sess.used, sess.rounds
	if len(w.Results) < len(w.Calls) {
		_ = s.saveLocked(sess)
		return false // 等确认卡片：处理完了由操作层的通知接着跑（resume）
	}
	sess.Wait = nil
	if err := sess.conv.AddToolResults(orderedResults(w)); err != nil {
		s.noticeLocked(sess, "error", "交还工具结果失败："+err.Error())
		return false
	}
	_ = s.saveLocked(sess)
	return true
}

func proposalView(p *ops.Proposal) *ProposalView {
	return &ProposalView{ID: p.ID, Op: p.Op, Title: p.Title, Level: p.Level.String(), Args: p.Args, Plan: p.Plan, Consent: p.Consent, Expires: p.Expires}
}

// content 工具结果交还给 AI 的样子，见 FormatResult。
func (s *Service) content(v any) string { return FormatResult(s.redact, v) }

// FormatResult 操作的结果交给 AI 的样子：脱敏后的紧凑 JSON（字符串原样脱敏），外面加上边界标记，超过 16 KB 截断并注明。
// 带 lines 的结果（控制台、日志的末尾）从最旧的行删起，留下最新的；其余的截掉后面的部分。应用内助手和 MCP 接口共用。
func FormatResult(red *Redactor, v any) string {
	var body string
	if str, ok := v.(string); ok {
		body = red.Text(str)
	} else {
		body = red.JSON(v)
		if len(body) > maxResult {
			body = trimLines(red, v, body)
		}
	}
	if len(body) > maxResult {
		cut := maxResult
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}
		body = body[:cut] + fmt.Sprintf("\n……（已截断：结果共 %d KB，只给出前 16 KB。需要后面的内容请缩小范围再查）", (len(body)+1023)/1024)
	}
	return dataBegin + "\n" + body + "\n" + dataEnd
}

// trimLines 结果里有 lines 数组时，从前面删行直到装得下，并写明删了几行。
func trimLines(red *Redactor, v any, body string) string {
	var m map[string]any
	if decode(v, &m) != nil {
		return body
	}
	lines, ok := m["lines"].([]any)
	if !ok || len(lines) < 2 {
		return body
	}
	lo, hi := 1, len(lines) // 二分找最少删几行
	for lo < hi {
		mid := (lo + hi) / 2
		m["lines"], m["omitted"] = lines[mid:], fmt.Sprintf("太长了，前面 %d 行没有给出", mid)
		if len(red.JSON(m)) <= maxResult {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	m["lines"], m["omitted"] = lines[lo:], fmt.Sprintf("太长了，前面 %d 行没有给出", lo)
	return red.JSON(m)
}

// ---------- 提案处理完 ----------

// onResolve 操作层通知：助手提出的提案处理完了（确认后执行完、执行失败、拒绝、过期、作废）。
// 记进会话，这一轮的提案都处理完了就接着请求 AI。
func (s *Service) onResolve(r ops.Resolution) {
	p := r.Proposal
	if p == nil || p.Caller.Source != ops.SrcAssistant {
		return
	}
	s.mu.Lock()
	sess := s.sessions[p.Caller.Session]
	s.mu.Unlock()
	if sess == nil {
		return
	}
	if s.applyResolution(sess, r) {
		s.resume(sess)
	}
}

// applyResolution 把结果记进等着的这一轮；返回这一轮是不是都处理完了。
func (s *Service) applyResolution(sess *Session, r ops.Resolution) bool {
	sess.mu.Lock()
	w := sess.Wait
	callID := ""
	if w != nil {
		callID = w.Props[r.Proposal.ID]
	}
	if callID == "" { // 还没记下对照（确认得极快）：先存着
		if w != nil || sess.busy {
			if sess.early == nil {
				sess.early = map[string]ops.Resolution{}
			}
			sess.early[r.Proposal.ID] = r
		}
		sess.mu.Unlock()
		return false
	}
	if _, done := w.Results[callID]; done {
		sess.mu.Unlock()
		return false
	}
	errText := ""
	if r.Err != nil {
		errText = s.redact.Text(r.Err.Error())
	}
	content := outcomeText(string(r.Outcome), r.Reason, errText)
	res := ai.ToolResult{ID: callID, Content: content, IsError: r.Outcome != ops.OutDone}
	if r.Outcome == ops.OutDone {
		res.Content = "用户确认了，已经执行。结果：\n" + s.content(r.Result)
	}
	w.Results[callID] = res
	it := setOutcome(sess, r.Proposal.ID, string(r.Outcome), r.Reason, r.By.Actor, errText)
	if it != nil {
		setSent(it, callID, res.Content)
	}
	all := len(w.Results) == len(w.Calls)
	sess.Updated = s.now()
	_ = s.saveLocked(sess)
	busy := sess.busy
	sess.mu.Unlock()
	if it != nil {
		s.publishItem(sess, it)
	}
	return all && !busy
}

// resume 这一轮的确认卡片都处理完了：接着请求 AI（人刚处理完，接着花 token 是预期之中的）。
func (s *Service) resume(sess *Session) {
	sess.mu.Lock()
	if sess.busy || sess.Wait == nil {
		sess.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess.busy, sess.cancel = true, cancel
	sess.mu.Unlock()
	s.publishState(sess)
	go s.turn(ctx, ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}, sess, nil)
}

// setOutcome 在界面的那一条上记下提案的结果（调用方持有 sess.mu），返回改了的那一条。
func setOutcome(sess *Session, pid, outcome, reason, by, errText string) *Item {
	for _, it := range sess.Items {
		for _, t := range it.Tools {
			if t.Proposal != nil && t.Proposal.ID == pid {
				t.Proposal.Outcome, t.Proposal.Reason, t.Proposal.By, t.Proposal.Error = outcome, reason, by, errText
				t.Status = map[string]string{string(ops.OutDone): "confirmed"}[outcome]
				if t.Status == "" {
					t.Status = outcome
				}
				return it
			}
		}
	}
	return nil
}

// outcomeText 提案的结局交还给 AI 时怎么说。
func outcomeText(outcome, reason, errText string) string {
	switch outcome {
	case string(ops.OutFailed):
		return "用户确认了，但执行失败：" + errText
	case string(ops.OutRejected):
		if reason != "" {
			return "用户拒绝了这个操作，没有执行。用户说：" + reason
		}
		return "用户拒绝了这个操作，没有执行。"
	case string(ops.OutExpired):
		return "提案在有效期内没有被确认，已经过期，没有执行。需要的话可以重新提出。"
	case string(ops.OutInvalid):
		return "提案作废了，没有执行（" + errText + "）。请先重新查看最新的情况，再决定要不要重新提出。"
	case "canceled":
		return "用户没有处理这张确认卡片，直接发了新消息，提案已取消，没有执行。"
	}
	return "没有执行。"
}

// ---------- 排错资料 ----------

// Context 「让 AI 分析」：打包这个会话关联的实例的排错资料（脱敏后），存起来等用户看过预览再发；返回将要发送的全文。
func (s *Service) Context(ctx context.Context, who ops.Caller, id string) (*Preview, error) {
	if err := s.allow(who); err != nil {
		return nil, err
	}
	sess, err := s.get(who, id)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	inst, caller := sess.Inst, sess.caller()
	sess.mu.Unlock()
	if inst == "" {
		return nil, ops.Errorf(400, "这个会话没有关联实例")
	}
	text, err := s.pack(ctx, caller, inst)
	if err != nil {
		return nil, err
	}
	text = s.redact.Text(text)
	sess.mu.Lock()
	sess.attach = &attachment{text: text, created: s.now()}
	sess.mu.Unlock()
	s.publishState(sess)
	lead := "请帮我分析这台服务器的问题。"
	full := lead + "\n\n" + text
	return &Preview{Lead: lead, Text: full, Chars: utf8.RuneCountInString(full), Tokens: estimateTokens(full)}, nil
}

// Preview 发送预览：将要发给 AI 的全文、字数和大约的 token 数。
type Preview struct {
	Lead   string `json:"lead"` // 默认的提问（用户可以改）
	Text   string `json:"text"`
	Chars  int    `json:"chars"`
	Tokens int    `json:"tokens"`
}

// estimateTokens 粗估 token 数：中文大约一个字一个 token，西文大约四个字符一个。
func estimateTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if r > 0x2E80 {
			cjk++
		} else {
			other++
		}
	}
	return cjk + other/4
}

// decode 把操作的结果（Go 的值）按 JSON 转成具体的结构（打包排错资料、截断结果时用）。
func decode(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
