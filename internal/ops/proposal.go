package ops

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sort"
	"strings"
	"time"
)

// Proposal AI 发起的写操作先生成的提案（设计文档 §3.3）：等人在界面上确认，确认后按这里存下的参数原样执行，
// 不让 AI 再生成一遍参数。唯一的例外是 Consent：确认的人勾了同意，才把那一项设成 true。
type Proposal struct {
	ID      string    `json:"id"` // "p_8f2c1a9e3b7d"
	Op      string    `json:"op"`
	Title   string    `json:"title"`
	Level   Level     `json:"level"`
	Args    Args      `json:"args"` // 确认后原样执行的参数（规范化后的原文）
	Plan    *Plan     `json:"plan"`
	Consent *Consent  `json:"consent,omitempty"` // 确认时要勾选的同意（如 EULA）；不需要时为空
	Caller  Caller    `json:"caller"`            // 谁提出的
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

const maxPending = 100 // 同时等确认的提案上限，超出时挤掉最早的

func (p *Proposal) clone() *Proposal {
	c := *p
	c.Args = bytes.Clone(p.Args)
	return &c
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "p_" + hex.EncodeToString(b)
}

// Outcome 提案的结局。
type Outcome string

const (
	OutDone     Outcome = "done"     // 确认后执行成功
	OutFailed   Outcome = "failed"   // 确认后执行失败
	OutRejected Outcome = "rejected" // 人拒绝了
	OutExpired  Outcome = "expired"  // 过期了
	OutInvalid  Outcome = "invalid"  // 前置条件变了（或者预览失败），作废
)

// Resolution 一个提案处理完了：结局、执行结果、拒绝的理由、处理的人。
type Resolution struct {
	Proposal *Proposal
	Outcome  Outcome
	Result   any    // 执行的结果（OutDone）
	Err      error  // 执行失败、作废、过期的原因
	Reason   string // 拒绝的理由（可以为空）
	By       Caller // 处理的人
}

// OnResolve 登记提案处理完时的回调（助手据此把结果交还给 AI）。在处理提案的那个调用里同步执行。
func (r *Registry) OnResolve(fn func(Resolution)) {
	r.mu.Lock()
	r.resolved = append(r.resolved, fn)
	r.mu.Unlock()
}

func (r *Registry) notify(res Resolution) {
	r.mu.Lock()
	fns := slices.Clone(r.resolved)
	r.mu.Unlock()
	for _, fn := range fns {
		fn(res)
	}
}

// propose 调用预览函数生成提案并存起来，不执行；记一条操作记录（event 为 propose）。
func (r *Registry) propose(ctx context.Context, c Caller, op *Op, args Args) (*Proposal, error) {
	plan := &Plan{Summary: op.Title}
	if op.Preview != nil {
		p, err := op.Preview(ctx, c, args)
		if err != nil {
			return nil, err
		}
		if p != nil {
			plan = p
		}
	}
	now := r.now()
	p := &Proposal{ID: newID(), Op: op.Name, Title: op.Title, Level: op.Level, Args: bytes.Clone(args),
		Plan: plan, Caller: c, Created: now, Expires: now.Add(r.TTL)}
	if op.Consent != nil && (op.Consent.Need == nil || op.Consent.Need(args)) {
		cs := *op.Consent
		cs.Need = nil
		p.Consent = &cs
	}
	r.mu.Lock()
	r.gcLocked()
	for len(r.proposals) >= maxPending {
		oldest := ""
		for id, q := range r.proposals {
			if oldest == "" || q.Created.Before(r.proposals[oldest].Created) {
				oldest = id
			}
		}
		delete(r.proposals, oldest)
	}
	r.proposals[p.ID] = p
	r.mu.Unlock()
	r.record(c, op.Name, args, EvPropose, p.ID, "", nil, 0)
	return p.clone(), nil
}

// gcLocked 丢掉过期已久的提案。刚过期的再留一个有效期，确认时还能明确告诉用户「已过期」。
func (r *Registry) gcLocked() {
	now := r.now()
	for id, p := range r.proposals {
		if now.After(p.Expires.Add(r.TTL)) {
			delete(r.proposals, id)
		}
	}
}

// Pending 还在等确认、没有过期的提案，按提出的时间排序；session 不为空时只列这个会话的。
func (r *Registry) Pending(session string) []*Proposal {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gcLocked()
	now := r.now()
	out := []*Proposal{}
	for _, p := range r.proposals {
		if now.After(p.Expires) || (session != "" && p.Caller.Session != session) {
			continue
		}
		out = append(out, p.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Proposal 按编号取一个还没处理的提案（过期了但还没清理的也给）；没有返回 nil。
func (r *Registry) Proposal(id string) *Proposal {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.proposals[id]; p != nil {
		return p.clone()
	}
	return nil
}

// ConfirmInput 确认时人给出的东西。
type ConfirmInput struct {
	Ack     bool // 危险操作的第二次确认（「我知道这会让在线玩家掉线」）
	Consent bool // 勾了提案要求的同意（如 Minecraft EULA）
}

// Confirm 同 ConfirmWith，只带危险操作的第二次确认。
func (r *Registry) Confirm(ctx context.Context, approver Caller, id string, ack bool) (any, error) {
	return r.ConfirmWith(ctx, approver, id, ConfirmInput{Ack: ack})
}

// ConfirmWith 人确认提案：先查确认的人有没有资格，再复查有效期和前置条件，最后按提案里存下的参数原样执行
// （要求勾选同意的，勾了才把那一项设成 true）。提案只能用一次：查过资格之后，不论成败都从等待列表里拿掉。
func (r *Registry) ConfirmWith(ctx context.Context, approver Caller, id string, in ConfirmInput) (any, error) {
	r.mu.Lock()
	p := r.proposals[id]
	if p == nil {
		r.mu.Unlock()
		return nil, Errorf(404, "提案不存在，或者已经处理过了")
	}
	op := r.ops[p.Op]
	switch {
	case !approver.Source.Human():
		r.mu.Unlock()
		return nil, Errorf(403, "提案只能由人确认")
	case op.LocalOnly && !approver.Local:
		r.mu.Unlock()
		return nil, Errorf(403, "「%s」仅支持在本机确认", op.Title)
	case op.Level == Danger && !in.Ack:
		r.mu.Unlock()
		return nil, Errorf(400, "这是危险操作，需要再确认一次")
	case p.Consent != nil && !in.Consent:
		r.mu.Unlock()
		return nil, Errorf(400, "需要先勾选「%s」", p.Consent.Text)
	}
	delete(r.proposals, id)
	r.mu.Unlock()

	void := func(out Outcome, err error) (any, error) {
		r.record(p.Caller, op.Name, p.Args, EvVoid, p.ID, approver.Actor, err, 0)
		r.notify(Resolution{Proposal: p.clone(), Outcome: out, Err: err, By: approver})
		return nil, err
	}
	if r.now().After(p.Expires) {
		return void(OutExpired, Errorf(410, "提案已过期，请让 AI 重新提出"))
	}
	if op.Preview != nil {
		plan, err := op.Preview(ctx, p.Caller, p.Args)
		if err != nil {
			return void(OutInvalid, Errorf(409, "提案作废：%v。请让 AI 重新预览", err))
		}
		var pre map[string]string
		if plan != nil {
			pre = plan.Pre
		}
		if changed := diffPre(p.Plan.Pre, pre); len(changed) > 0 {
			return void(OutInvalid, Errorf(409, "提案作废：预览之后%s变了，请让 AI 重新预览", strings.Join(changed, "、")))
		}
	}
	args := p.Args
	if p.Consent != nil {
		args = setParam(args, p.Consent.Param, true)
	}
	res, err := r.exec(ctx, p.Caller, op, args, p.ID, approver.Actor)
	out := OutDone
	if err != nil {
		out = OutFailed
	}
	r.notify(Resolution{Proposal: p.clone(), Outcome: out, Result: res, Err: err, By: approver})
	return res, err
}

// Reject 人拒绝提案；reason 是理由（可以为空，由上层交还给 AI）。过期的提案按作废处理，返回 410。
func (r *Registry) Reject(approver Caller, id, reason string) error {
	if !approver.Source.Human() {
		return Errorf(403, "提案只能由人处理")
	}
	r.mu.Lock()
	p := r.proposals[id]
	if p == nil {
		r.mu.Unlock()
		return Errorf(404, "提案不存在，或者已经处理过了")
	}
	delete(r.proposals, id)
	r.mu.Unlock()
	if r.now().After(p.Expires) {
		err := Errorf(410, "提案已过期")
		r.record(p.Caller, p.Op, p.Args, EvVoid, p.ID, approver.Actor, err, 0)
		r.notify(Resolution{Proposal: p.clone(), Outcome: OutExpired, Err: err, By: approver})
		return err
	}
	reason = truncateRunes(strings.TrimSpace(reason), 200)
	if r.Log != nil {
		rec := Record{T: r.now().Format(time.RFC3339), Op: p.Op, Src: p.Caller.Source, Actor: p.Caller.Actor, Session: p.Caller.Session,
			Event: EvReject, Proposal: p.ID, ApprovedBy: approver.Actor, Reason: reason, OK: true}
		rec.Inst, rec.Args = splitArgs(p.Args)
		r.Log.Write(rec)
	}
	r.notify(Resolution{Proposal: p.clone(), Outcome: OutRejected, Reason: reason, By: approver})
	return nil
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// diffPre 列出值变了（或多了、少了）的前置条件名称。
func diffPre(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
