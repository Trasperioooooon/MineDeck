package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// 读类问题不弹确认：AI 调用读操作，直接执行、把结果交回去，没有提案；交回的结果脱了敏（rcon.password 写成 ***）。
func TestReadToolsRunWithoutConfirm(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(
		toolReply("我先看看配置。", call{"c1", "instance_props_get", `{"inst":"生存服"}`}),
		textReply("max-players 是 20。"),
	)
	if err := f.as.Send(local, id, "生存服最多几个人？", false); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return len(v.Items) >= 3 })
	if len(f.reg.Pending("")) != 0 || v.Waiting {
		t.Fatal("读操作不该生成提案")
	}
	tl := tool(v, "instance_props_get")
	if tl == nil || tl.Status != "done" || tl.Level != "R" || tl.Proposal != nil {
		t.Fatalf("读操作应直接执行：%s", dump(v))
	}
	if last := v.Items[len(v.Items)-1]; last.Kind != "assistant" || last.Text != "max-players 是 20。" || last.Usage == nil || last.Usage.Input != 120 {
		t.Fatalf("最后的回复：%s", dump(last))
	}
	reqs := f.ai.requests()
	if len(reqs) != 2 {
		t.Fatalf("应请求 2 次：%d", len(reqs))
	}
	second := string(reqs[1])
	if !strings.Contains(second, "max-players") || strings.Contains(second, "hunter2") || !strings.Contains(second, dataBegin) {
		t.Errorf("交回的工具结果：%s", second)
	}
	sent, err := f.as.Sent(local, id, v.Items[1].ID, "c1")
	if err != nil || !strings.Contains(sent, `"value":"***"`) || strings.Contains(sent, "hunter2") {
		t.Errorf("发给 AI 的内容（发送预览）：%v %s", err, sent)
	}
	if recs, _ := f.reg.Log.Query(ops.Query{}); len(recs) != 0 {
		t.Errorf("读操作不写操作记录：%+v", recs)
	}
}

// 没确认的写操作绝不执行；确认后按提案里存下的参数原样执行（AI 之后再改参数也没用），结果交还给 AI，自动接着回复。
func TestWriteNeedsConfirmAndRunsExactArgs(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	args := `{"inst":"生存服","pairs":[{"key":"max-players","value":"10"}]}`
	f.ai.queue(toolReply("", call{"c1", "instance_props_set", args}), textReply("改好了，重启后生效。"))
	if err := f.as.Send(local, id, "把人数上限改成 10", false); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return v.Waiting })
	tl := tool(v, "instance_props_set")
	if tl == nil || tl.Status != "pending" || tl.Proposal == nil || tl.Proposal.Plan.Changes[0].After != "10" || tl.Proposal.Level != "W" {
		t.Fatalf("应生成确认卡片：%s", dump(v))
	}
	time.Sleep(100 * time.Millisecond)
	if f.count("instance.props.set") != 0 || len(f.ai.requests()) != 1 {
		t.Fatalf("没确认就执行了，或者没等确认就接着请求了 AI：%d %d", f.count("instance.props.set"), len(f.ai.requests()))
	}
	pending := f.reg.Pending(id)
	if len(pending) != 1 || pending[0].ID != tl.Proposal.ID {
		t.Fatalf("注册表里的提案：%+v", pending)
	}
	if _, err := f.reg.Confirm(context.Background(), local, tl.Proposal.ID, false); err != nil {
		t.Fatal(err)
	}
	v = f.wait(id, func(v *View) bool { return !v.Waiting && len(f.ai.requests()) == 2 })
	if got := f.lastArgs("instance.props.set"); got != `{"inst":"生存服","pairs":[{"key":"max-players","value":"10"}]}` || got != string(tl.Proposal.Args) {
		t.Fatalf("执行的参数和提案不一致：%s / %s", got, tl.Proposal.Args)
	}
	if tl = tool(v, "instance_props_set"); tl.Status != "confirmed" || tl.Proposal.Outcome != "done" || tl.Proposal.By != "本机" {
		t.Errorf("卡片的状态：%s", dump(tl))
	}
	if !strings.Contains(string(f.ai.requests()[1]), "用户确认了，已经执行") {
		t.Errorf("结果应交还给 AI：%s", f.ai.requests()[1])
	}
	f.wait(id, func(v *View) bool { return v.Items[len(v.Items)-1].Text == "改好了，重启后生效。" })
	// 操作记录：AI 提出、确认后执行，来源 assistant，确认人是本机
	recs, _ := f.reg.Log.Query(ops.Query{})
	if len(recs) != 2 || recs[0].Proposal != tl.Proposal.ID || recs[0].ApprovedBy != "本机" || recs[0].Src != ops.SrcAssistant ||
		recs[0].Session != id || recs[1].Event != ops.EvPropose {
		t.Errorf("操作记录：%+v", recs)
	}
}

// 拒绝（带理由）和危险操作的二次确认：理由交还给 AI；危险操作不勾 ack 不执行。
func TestRejectAndDangerAck(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(toolReply("", call{"c1", "instance_props_set", `{"inst":"生存服","pairs":[{"key":"motd","value":"x"}]}`},
		call{"c2", "instance_stop", `{"inst":"生存服"}`}), textReply("好的，不改了。"))
	_ = f.as.Send(local, id, "改一下再停服", false)
	v := f.wait(id, func(v *View) bool { return v.Waiting })
	p1, p2 := tool(v, "instance_props_set").Proposal, tool(v, "instance_stop").Proposal
	if p2.Level != "D" || p2.Plan.Warning == "" {
		t.Fatalf("危险操作的卡片：%s", dump(p2))
	}
	if err := f.reg.Reject(local, p1.ID, "先别改"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reg.Confirm(context.Background(), local, p2.ID, false); ops.CodeOf(err) != 400 || f.count("instance.stop") != 0 {
		t.Fatalf("危险操作没勾二次确认不能执行：%v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if len(f.ai.requests()) != 1 {
		t.Fatal("还有卡片没处理，不该接着请求 AI")
	}
	if _, err := f.reg.Confirm(context.Background(), local, p2.ID, true); err != nil {
		t.Fatal(err)
	}
	f.wait(id, func(v *View) bool { return len(f.ai.requests()) == 2 && !v.Waiting })
	body := string(f.ai.requests()[1])
	if !strings.Contains(body, "用户拒绝了这个操作，没有执行。用户说：先别改") || !strings.Contains(body, "用户确认了，已经执行") {
		t.Errorf("两张卡片的结果应一起交回：%s", body)
	}
	if f.count("instance.props.set") != 0 || f.count("instance.stop") != 1 {
		t.Errorf("执行次数：%v", f.runs)
	}
	// 两个结果在同一次请求里、按调用的顺序
	msgs := reqMessages(t, f.ai.requests()[1])
	if n := len(msgs); !strings.Contains(string(msgs[n-2]), `"tool_call_id":"c1"`) || !strings.Contains(string(msgs[n-1]), `"tool_call_id":"c2"`) {
		t.Errorf("工具结果的顺序：%s", msgs[n-2:])
	}
}

// 前置条件变了，提案作废：不执行，「作废」交还给 AI。
func TestPreconditionChangedVoids(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(toolReply("", call{"c1", "instance_props_set", `{"inst":"生存服","pairs":[{"key":"max-players","value":"10"}]}`}), textReply("文件变了，我重新看看。"))
	_ = f.as.Send(local, id, "改人数", false)
	v := f.wait(id, func(v *View) bool { return v.Waiting })
	f.mu.Lock()
	f.file = "max-players=30" // 预览之后有人在别处改了文件
	f.mu.Unlock()
	if _, err := f.reg.Confirm(context.Background(), local, tool(v, "instance_props_set").Proposal.ID, false); ops.CodeOf(err) != 409 {
		t.Fatalf("前置条件变了应作废（409）：%v", err)
	}
	v = f.wait(id, func(v *View) bool { return len(f.ai.requests()) == 2 })
	if f.count("instance.props.set") != 0 {
		t.Fatal("作废的提案不该执行")
	}
	if tl := tool(v, "instance_props_set"); tl.Status != "invalid" || !strings.Contains(tl.Proposal.Error, "server.properties") {
		t.Errorf("卡片应显示作废：%s", dump(tl))
	}
	if !strings.Contains(string(f.ai.requests()[1]), "提案作废了，没有执行") {
		t.Errorf("作废应交还给 AI：%s", f.ai.requests()[1])
	}
}

// 没处理确认卡片就发了新消息：提案作废（不执行、记一条拒绝），结果和新消息一起交给 AI；过期的提案继续时按过期交回。
func TestNewMessageCancelsPendingAndExpiry(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(toolReply("", call{"c1", "instance_props_set", `{"inst":"生存服","pairs":[{"key":"max-players","value":"10"}]}`}), textReply("好，先回答你的问题。"))
	_ = f.as.Send(local, id, "改人数", false)
	f.wait(id, func(v *View) bool { return v.Waiting })
	if err := f.as.Continue(local, id); ops.CodeOf(err) != 409 {
		t.Errorf("卡片没处理时不能「继续」：%v", err)
	}
	if err := f.as.Send(local, id, "等等，为什么要改这个？", false); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return len(f.ai.requests()) == 2 && !v.Waiting })
	if f.count("instance.props.set") != 0 || len(f.reg.Pending("")) != 0 {
		t.Fatal("没确认的提案不能执行，也不该还挂着")
	}
	if tl := tool(v, "instance_props_set"); tl.Status != "canceled" {
		t.Errorf("卡片应显示已取消：%s", dump(tl))
	}
	msgs := reqMessages(t, f.ai.requests()[1])
	n := len(msgs)
	if !strings.Contains(string(msgs[n-2]), "用户没有处理这张确认卡片") || !strings.Contains(string(msgs[n-1]), "为什么要改这个") || !strings.Contains(string(msgs[n-1]), `"role":"user"`) {
		t.Errorf("作废的结果和新消息：%s", msgs[n-2:])
	}
	if recs, _ := f.reg.Log.Query(ops.Query{}); len(recs) != 2 || recs[0].Event != ops.EvReject || recs[1].Event != ops.EvPropose {
		t.Errorf("操作记录：%+v", recs)
	}

	// 过期：卡片放着不管，过了有效期再「继续」
	f.reg.TTL = 30 * time.Millisecond
	f.ai.queue(toolReply("", call{"c9", "instance_stop", `{"inst":"生存服"}`}), textReply("提案过期了。"))
	_ = f.as.Send(local, id, "停服", false)
	f.wait(id, func(v *View) bool { return v.Waiting })
	time.Sleep(60 * time.Millisecond)
	if err := f.as.Continue(local, id); err != nil {
		t.Fatalf("都过期了应能继续：%v", err)
	}
	v = f.wait(id, func(v *View) bool { return len(f.ai.requests()) == 4 })
	if tl := tool(v, "instance_stop"); tl.Status != "expired" || f.count("instance.stop") != 0 {
		t.Errorf("过期：%s", dump(tl))
	}
	if !strings.Contains(string(f.ai.requests()[3]), "已经过期，没有执行") {
		t.Errorf("过期应交还给 AI")
	}
}

// EULA 只能来自人：给 AI 的工具说明里没有 eula；AI 传了也去掉；卡片上要勾同意，勾了才带上 eula 执行。
func TestEULAOnlyFromHuman(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(toolReply("", call{"c1", "instance_create", `{"name":"新服","core":"paper","eula":true,"root":"C:\\Windows"}`}), textReply("开始创建了。"))
	_ = f.as.Send(local, id, "帮我开个 Paper 服", false)
	v := f.wait(id, func(v *View) bool { return v.Waiting })
	var first struct {
		Tools []struct {
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(f.ai.requests()[0], &first)
	for _, tl := range first.Tools {
		if tl.Function.Name == "instance_create" && (bytes.Contains(tl.Function.Parameters, []byte("eula")) || bytes.Contains(tl.Function.Parameters, []byte("root"))) {
			t.Errorf("工具说明里不该有 eula、root：%s", tl.Function.Parameters)
		}
	}
	p := tool(v, "instance_create").Proposal
	if strings.Contains(string(p.Args), "eula") || p.Consent == nil {
		t.Fatalf("提案：%s %+v", p.Args, p.Consent)
	}
	if _, err := f.reg.ConfirmWith(context.Background(), local, p.ID, ops.ConfirmInput{}); err == nil || f.count("instance.create") != 0 {
		t.Fatal("没勾同意不能执行")
	}
	if _, err := f.reg.ConfirmWith(context.Background(), local, p.ID, ops.ConfirmInput{Consent: true}); err != nil {
		t.Fatal(err)
	}
	if got := f.lastArgs("instance.create"); got != `{"core":"paper","eula":true,"name":"新服"}` {
		t.Errorf("执行的参数：%s", got)
	}
}

// 工具清单：仅人工的不给，检查更新这类用不上的不给；局域网开的会话没有「仅本机」的操作。会话的工具清单定下就不变。
func TestToolList(t *testing.T) {
	f := newFixture(t)
	names := func(who ops.Caller) string {
		t.Helper()
		id := f.open(who)
		f.ai.queue(textReply("你好"))
		if err := f.as.Send(who, id, "你好", false); err != nil {
			t.Fatal(err)
		}
		f.wait(id, func(v *View) bool { return len(v.Items) == 2 })
		reqs := f.ai.requests()
		var r struct {
			Tools []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(reqs[len(reqs)-1], &r)
		var out []string
		for _, tl := range r.Tools {
			out = append(out, tl.Function.Name)
		}
		return strings.Join(out, ",")
	}
	got := names(local)
	if strings.Contains(got, "app_config_set") || strings.Contains(got, "app_update_check") || !strings.Contains(got, "instance_props_set") ||
		!strings.Contains(got, "t_localw") || !strings.Contains(got, "instance_diag") {
		t.Errorf("本机会话的工具：%s", got)
	}
	if got := names(lan); strings.Contains(got, "t_localw") || !strings.Contains(got, "instance_props_set") {
		t.Errorf("局域网会话的工具：%s", got)
	}
}

// 空回复不追加进历史：第一次请求就是空的，撤回这条提问；工具结果交回之后的那次是空的，停在等回复，
// 用户接着说话时在工具结果后面追加。
func TestEmptyReply(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(emptyReply())
	_ = f.as.Send(local, id, "在吗", false)
	v := f.wait(id, func(v *View) bool { return strings.Contains(notices(v), "没有给出回复") })
	if !v.Items[0].Withdrawn || v.NeedReply {
		t.Fatalf("空回复后应撤回提问：%s", dump(v))
	}
	f.ai.queue(toolReply("", call{"c1", "instance_list", `{}`}), emptyReply(), textReply("有一台：生存服。"))
	_ = f.as.Send(local, id, "有几台服务器", false)
	v = f.wait(id, func(v *View) bool { return v.NeedReply })
	if !strings.Contains(notices(v), "没有给出回复") {
		t.Fatalf("工具之后空回复：%s", dump(v))
	}
	_ = f.as.Send(local, id, "说话呀", false)
	f.wait(id, func(v *View) bool { return v.Items[len(v.Items)-1].Text == "有一台：生存服。" })
	msgs := reqMessages(t, f.ai.requests()[3])
	n := len(msgs)
	if !strings.Contains(string(msgs[n-2]), `"role":"tool"`) || !strings.Contains(string(msgs[n-1]), "说话呀") {
		t.Errorf("新消息应接在工具结果后面：%s", msgs[n-2:])
	}
	for _, m := range msgs {
		if strings.Contains(string(m), "在吗") {
			t.Error("撤回的提问不该再发给 AI")
		}
	}
}

// 停止：第一次请求时停止，撤回这条提问（界面把原话放回输入框）；记账照记。
func TestStop(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.hold = make(chan struct{})
	defer close(f.ai.hold)
	f.ai.queue(textReply("这是一段很长的回答"))
	_ = f.as.Send(local, id, "讲个长故事", false)
	deadline := time.Now().Add(3 * time.Second)
	for len(f.ai.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if err := f.as.Send(local, id, "再来", false); err != ErrBusy {
		t.Errorf("回复中再发应报忙：%v", err)
	}
	if err := f.as.Stop(local, id); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return strings.Contains(notices(v), "已停止") })
	if !v.Items[0].Withdrawn {
		t.Errorf("停止后应撤回提问：%s", dump(v))
	}
	if u, _ := f.svc.Usage(ai.FeatAssistant); u.Requests != 1 {
		t.Errorf("停止的那次也要记账：%+v", u)
	}
}

// 一轮最多 8 次工具调用：第 9 个不执行，告诉 AI 到上限了。
func TestToolLimit(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	var calls []call
	for k := 0; k < 9; k++ {
		calls = append(calls, call{string(rune('a'+k)) + "x", "instance_list", `{}`})
	}
	f.ai.queue(toolReply("", calls...), textReply("好了"))
	_ = f.as.Send(local, id, "查九次", false)
	v := f.wait(id, func(v *View) bool { return v.Items[len(v.Items)-1].Text == "好了" })
	tools := v.Items[1].Tools
	if len(tools) != 9 || tools[7].Status != "done" || tools[8].Status != "skipped" || !strings.Contains(tools[8].Error, "到上限了") {
		t.Errorf("第 9 次应跳过：%s", dump(tools))
	}
}

// 发送预览里看不到用户名、IP、Key：打包的排错资料脱敏后交给用户看，发出去的就是这一份；工具结果同样脱敏。
func TestPreviewRedacted(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	pv, err := f.as.Context(context.Background(), local, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{winUser, "bob", lanIP, "10.0.0.77", "10.0.0.5", aiSecret, "ghp_abcdefghij", "hunter2", "DESKTOP-ALICE7", "hook-secret-77"} {
		if strings.Contains(strings.ToLower(pv.Text), strings.ToLower(leak)) {
			t.Errorf("发送预览里有 %q：\n%s", leak, pv.Text)
		}
	}
	for _, want := range []string{"缺少前置模组", "create 需要 geckolib", "Missing or unsupported mandatory dependencies", "Java：17（MineDeck 便携版）",
		"崩溃报告 crash-2026-10-28_09.30.10-server.txt", "create 6.0.4（create-1.21.1-6.0.4.jar）：依赖 geckolib [4.4,)（必需）", phUser, phIP, "<计算机名>"} {
		if !strings.Contains(pv.Text, want) {
			t.Errorf("发送预览里应有 %q：\n%s", want, pv.Text)
		}
	}
	if pv.Chars == 0 || pv.Tokens == 0 || pv.Chars < pv.Tokens/4 {
		t.Errorf("字数和 token：%+v", pv)
	}
	if v, _ := f.as.Get(local, id); v.Attach == 0 {
		t.Error("打包好的资料应等着发送")
	}
	f.ai.queue(toolReply("", call{"c1", "app_info", `{}`}), textReply("少了 geckolib。"))
	if err := f.as.Send(local, id, "", true); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return len(f.ai.requests()) == 2 && len(v.Items) >= 3 })
	if v.Items[0].Attach == 0 || v.Title != "分析「生存服」的问题" {
		t.Errorf("附上排错资料的提问：%s", dump(v.Items[0]))
	}
	for k, body := range f.ai.requests() {
		s := strings.ToLower(string(body))
		for _, leak := range []string{winUser, lanIP, "10.0.0.5", aiSecret, "hunter2", "hook-secret-77", "desktop-alice7"} {
			if strings.Contains(s, strings.ToLower(leak)) {
				t.Errorf("第 %d 次请求里有 %q", k+1, leak)
			}
		}
	}
	if !strings.Contains(string(f.ai.requests()[0]), "Missing or unsupported mandatory dependencies") {
		t.Error("发出去的应是预览过的那一份")
	}
	if err := f.as.Send(local, id, "", true); ops.CodeOf(err) != 409 {
		t.Errorf("用过的资料不能再发：%v", err)
	}
}

// 会话保存后能读回：重启后会话还在，接着聊时发给 AI 的历史和之前逐字节相同。
func TestSaveAndLoad(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(toolReply("看看 <配置> & 状态", call{"c1", "instance_props_get", `{"inst":"生存服"}`}), textReply("人数上限 20。"))
	_ = f.as.Send(local, id, "生存服 <人数> & 上限？", false)
	before := f.wait(id, func(v *View) bool { return len(v.Items) == 3 })
	reg2 := ops.NewRegistry(nil)
	f.reg = reg2
	f.register()
	as2 := New(Options{Ops: reg2, AI: f.svc, Dir: f.as.dir, Redact: f.as.redact})
	after, err := as2.Get(local, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Title != before.Title || len(after.Items) != len(before.Items) || after.Items[2].Text != "人数上限 20。" || tool(after, "instance_props_get").Status != "done" {
		t.Fatalf("读回来的会话：%s", dump(after))
	}
	if l, _ := as2.List(local); len(l) != 1 || l[0].ID != id {
		t.Errorf("会话列表：%+v", l)
	}
	f.ai.queue(textReply("好的"))
	f.as = as2
	if err := as2.Send(local, id, "谢谢", false); err != nil {
		t.Fatal(err)
	}
	f.wait(id, func(v *View) bool { return len(v.Items) == 5 })
	reqs := f.ai.requests()
	prev, next := reqMessages(t, reqs[1]), reqMessages(t, reqs[2])
	if len(next) != len(prev)+2 {
		t.Fatalf("消息条数：%d → %d", len(prev), len(next))
	}
	for k := range prev {
		if !bytes.Equal(prev[k], next[k]) {
			t.Errorf("第 %d 条和保存前不一样：\n%s\n%s", k, prev[k], next[k])
		}
	}
	// 工具清单也逐字节不变
	var t1, t2 struct {
		Tools json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(reqs[1], &t1)
	_ = json.Unmarshal(reqs[2], &t2)
	if !bytes.Equal(t1.Tools, t2.Tools) {
		t.Error("读回后工具清单变了")
	}
}

// 局域网：开关开着能用，只看得见局域网开的会话；关掉后什么都不能做，也不能处理助手的提案。
// 没配置 AI 时助手不能用。
func TestLANAndDisabled(t *testing.T) {
	f := newFixture(t)
	mine := f.open(local)
	theirs := f.open(lan)
	if l, _ := f.as.List(lan); len(l) != 1 || l[0].ID != theirs || l[0].Origin != "lan" {
		t.Errorf("局域网看到的会话：%+v", l)
	}
	if _, err := f.as.Get(lan, mine); ops.CodeOf(err) != 404 {
		t.Errorf("局域网看不到本机的会话：%v", err)
	}
	if l, _ := f.as.List(local); len(l) != 2 {
		t.Errorf("本机都看得见：%+v", l)
	}
	if st := f.as.Status(lan); !st.Allowed || st.Local {
		t.Errorf("局域网的状态：%+v", st)
	}
	p := &ops.Proposal{Caller: ops.Caller{Source: ops.SrcAssistant, Session: mine, Local: true}}
	if err := f.as.CanDecide(lan, p); ops.CodeOf(err) != 403 || f.as.CanDecide(local, p) != nil {
		t.Errorf("本机会话的提案只能在本机处理：%v", err)
	}
	if err := f.svc.SetLANAssistant(false); err != nil {
		t.Fatal(err)
	}
	if st := f.as.Status(lan); st.Allowed || st.LAN {
		t.Errorf("关掉后：%+v", st)
	}
	if _, err := f.as.List(lan); ops.CodeOf(err) != 403 {
		t.Errorf("关掉后局域网不能用：%v", err)
	}
	if err := f.as.CanDecide(lan, &ops.Proposal{Caller: ops.Caller{Source: ops.SrcAssistant, Session: theirs}}); ops.CodeOf(err) != 403 {
		t.Errorf("关掉后局域网不能处理提案：%v", err)
	}
	if _, err := f.as.List(local); err != nil {
		t.Error("本机不受开关影响")
	}
	// 停用 AI：都不能用；会话标成不能接着用
	p0 := ""
	if err := f.svc.SetProvider(ai.ProviderPatch{Preset: &p0}); err != nil {
		t.Fatal(err)
	}
	if st := f.as.Status(local); st.Enabled || st.Allowed {
		t.Errorf("停用后：%+v", st)
	}
	if _, err := f.as.Create(local, ""); err == nil {
		t.Error("停用后不能开会话")
	}
	if err := f.as.Send(local, mine, "在吗", false); ops.CodeOf(err) != 400 {
		t.Errorf("停用后不能发消息：%v", err)
	}
	if _, err := f.as.Context(context.Background(), local, mine); ops.CodeOf(err) != 400 {
		t.Errorf("停用后不能生成发送预览：%v", err)
	}
	// 历史照样能看、能清空（停用 AI 之后也要能删掉自己的数据）
	if l, err := f.as.List(local); err != nil || len(l) != 2 {
		t.Errorf("停用后还能看历史：%v %+v", err, l)
	}
	if n, err := f.as.Clear(local); err != nil || n != 2 {
		t.Errorf("停用后也能清空：%d %v", n, err)
	}
}

// 用户的话里有隐私信息：界面上显示原话，发给 AI 的是替换后的，并标出来（可以点开看实际发送的内容）。
func TestUserTextMasked(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	f.ai.queue(textReply("好的"), textReply("嗯"))
	raw := `C:\Users\alice\Desktop 里的服务器，朋友用 ` + lanIP + ` 连不上`
	if err := f.as.Send(local, id, raw, false); err != nil {
		t.Fatal(err)
	}
	v := f.wait(id, func(v *View) bool { return len(v.Items) == 2 })
	u := v.Items[0]
	sent, _ := f.as.Sent(local, id, u.ID, "")
	if u.Text != raw || !u.Masked || u.Sent != "" || strings.Contains(sent, lanIP) || !strings.Contains(sent, phIP) || strings.Contains(sent, winUser) {
		t.Errorf("原话 %q，masked=%v，实际发送 %q", u.Text, u.Masked, sent)
	}
	if strings.Contains(string(f.ai.requests()[0]), lanIP) {
		t.Error("请求里有局域网 IP")
	}
	_ = f.as.Send(local, id, "没有隐私信息的一句话", false)
	v = f.wait(id, func(v *View) bool { return len(v.Items) == 4 })
	if v.Items[2].Masked {
		t.Errorf("没有替换就不标：%s", dump(v.Items[2]))
	}
}

// AI 设置改了：OpenAI 兼容换模型照常；换了地址，旧会话不能接着用。额度用完不能发。
func TestStaleAndQuota(t *testing.T) {
	f := newFixture(t)
	id := f.open(local)
	m := "other-model"
	if err := f.svc.SetProvider(ai.ProviderPatch{Model: &m}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.as.Get(local, id); v.Stale != "" {
		t.Errorf("OpenAI 兼容换模型不影响：%s", v.Stale)
	}
	f.ai.queue(textReply("你好"))
	_ = f.as.Send(local, id, "你好", false)
	f.wait(id, func(v *View) bool { return len(v.Items) == 2 })
	if err := f.svc.SetQuota(map[ai.Feature]int64{ai.FeatAssistant: 10}); err != nil {
		t.Fatal(err)
	}
	if err := f.as.Send(local, id, "再问一句", false); ops.CodeOf(err) != 429 || !strings.Contains(err.Error(), "额度") {
		t.Errorf("额度用完：%v", err)
	}
	if st := f.as.Status(local); st.Quota.Limit != 10 || st.Quota.Used != 150 {
		t.Errorf("额度提示：%+v", st.Quota)
	}
	_ = f.svc.SetQuota(map[ai.Feature]int64{ai.FeatAssistant: 0})
	u := f.ai.srv.URL + "/v2"
	if err := f.svc.SetProvider(ai.ProviderPatch{BaseURL: &u}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.as.Get(local, id); v.Stale == "" {
		t.Error("换了地址应标成不能接着用")
	}
	if err := f.as.Send(local, id, "还在吗", false); ops.CodeOf(err) != 409 {
		t.Errorf("不能接着用的会话不能发：%v", err)
	}
}

// 最多保存 20 个会话；删除、清空；删掉的会话里没处理的提案作废。
func TestSessionLimitAndDelete(t *testing.T) {
	f := newFixture(t)
	first := f.open(local)
	f.ai.queue(toolReply("", call{"c1", "instance_stop", `{"inst":"生存服"}`}))
	_ = f.as.Send(local, first, "停服", false)
	f.wait(first, func(v *View) bool { return v.Waiting })
	if err := f.as.Delete(local, first); err != nil {
		t.Fatal(err)
	}
	if len(f.reg.Pending("")) != 0 || f.count("instance.stop") != 0 {
		t.Error("删掉的会话里的提案应作废")
	}
	for k := 0; k < MaxSessions+3; k++ {
		f.open(local)
		time.Sleep(2 * time.Millisecond)
	}
	if l, _ := f.as.List(local); len(l) != MaxSessions {
		t.Errorf("最多 %d 个：%d", MaxSessions, len(l))
	}
	if n, err := f.as.Clear(local); err != nil || n != MaxSessions {
		t.Errorf("清空：%d %v", n, err)
	}
}

// 写操作的预览失败（比如重启一台没在运行的服务器）：不出确认卡片，这次调用按出错交回原因，AI 接着回复。
// 以前操作层返回装着 nil 提案的 any，这里会显示成「等你确认」、交回空的错误。
func TestPreviewFailureIsToolError(t *testing.T) {
	f := newFixture(t)
	f.reg.Register(&ops.Op{Name: "instance.restart", Title: "重启服务器", Level: ops.Danger,
		Params: ops.Object(map[string]*ops.Schema{"inst": ops.String("实例名称")}, "inst"),
		Preview: func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
			return nil, ops.Errorf(400, "「生存服」没有在运行")
		},
		Run: f.run("instance.restart")})
	id := f.open(local)
	f.ai.queue(toolReply("", call{"c1", "instance_restart", `{"inst":"生存服"}`}), textReply("服务器没在运行，不用重启。"))
	_ = f.as.Send(local, id, "重启一下", false)
	v := f.wait(id, func(v *View) bool { return len(f.ai.requests()) == 2 })
	tl := tool(v, "instance_restart")
	if tl == nil || tl.Status != "error" || tl.Proposal != nil || !strings.Contains(tl.Error, "没有在运行") || v.Waiting {
		t.Fatalf("预览失败：%s", dump(tl))
	}
	if body := string(f.ai.requests()[1]); !strings.Contains(body, "没有在运行") {
		t.Errorf("原因要交回给 AI：%s", body)
	}
}
