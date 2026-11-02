package chatai

// AI 伙伴（第七阶段）：伙伴工具只给授权、又没带 /问 的人；「@伙伴名」触发；任务结束时用模板通知、不调用 AI；伙伴的额度。

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/companion"
	"minedeck/internal/ops"
)

// fakeComps 假的伙伴：连着、有一个叫小方的伙伴，记下每个动作。
type fakeComps struct {
	mu    sync.Mutex
	live  bool
	list  []companion.Companion
	ops   []string
	calls []actCall
	next  int64
}

type actCall struct {
	op     string
	args   map[string]any
	origin companion.Origin
}

func newComps() *fakeComps {
	return &fakeComps{live: true, ops: slices.Clone(companion.ActionOps), list: []companion.Companion{
		{ID: 1, Name: "小方", Owner: "Steve", Dim: "minecraft:overworld", X: 32.5, Y: 64, Z: -383.5, Health: 20, MaxHealth: 20, State: "idle"}}}
}

func (f *fakeComps) Live(string) ([]companion.Companion, []string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.live {
		return nil, nil, false
	}
	return slices.Clone(f.list), slices.Clone(f.ops), true
}

func (f *fakeComps) Act(_ context.Context, _ string, op string, args map[string]any, o companion.Origin) (*companion.ActResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, actCall{op, args, o})
	c := f.list[0]
	res := &companion.ActResult{OK: true, Companion: &c, Sent: args}
	switch {
	case companion.IsTaskOp(op):
		f.next++
		b, _ := json.Marshal(args)
		res.Task = &companion.Task{ID: f.next, Companion: c.ID, Op: op, Args: b, Timeout: 600}
	case op == "status":
		res.Reply = &companion.Response{OK: true, Raw: json.RawMessage(`{"ok":true,"companions":[{"id":1,"name":"小方","health":20}]}`)}
	}
	return res, nil
}

func (f *fakeComps) actCalls() []actCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func companionToolNames() []string {
	var out []string
	for _, op := range companion.ActionOps {
		out = append(out, "companion_"+op)
	}
	return out
}

// 伙伴工具只给有执行权限、又没带 /问 的人：没授权的人、带了 /问 的人，请求里根本没有这些工具，模型硬要调用也不执行；
// 实例没有伙伴（没启用或者没连着）时连提都不提，系统提示和原来一样。模组不支持的动作不给（兼容规则 2）。
func TestCompanionToolsOnlyForAuthorized(t *testing.T) {
	comps := newComps()
	f := newFixtureWith(t, comps)
	f.set(Patch{Allowed: &[]string{"Steve"}})

	// 没授权：只有 game_info；系统提示说这次不能指挥；用户消息里有伙伴；硬要调用不执行
	f.ai.queue(toolReply(call{"c1", "companion_follow", `{}`}), textReply("这次我指挥不了伙伴，要服主授权。"))
	f.say("Alex", "@AI 让小方跟着我")
	reqs := f.ai.requests()
	r0 := parseReq(t, reqs[0])
	if names := r0.toolNames(); !slices.Equal(names, []string{"game_info"}) {
		t.Errorf("没授权时只能有 game_info：%v", names)
	}
	if sys := r0.Messages[0].Content; !strings.Contains(sys, "这一次你不能指挥伙伴") || strings.Contains(sys, "companion_ 开头的工具") {
		t.Errorf("没授权时的系统提示：%s", sys)
	}
	if u := r0.Messages[len(r0.Messages)-1].Content; !strings.Contains(u, "伙伴：小方（1 号，主人 Steve，空闲，血量 20/20，在 overworld 32.5, 64, -383.5）") {
		t.Errorf("用户消息里应列出伙伴：%s", u)
	}
	if c := parseReq(t, reqs[1]).Messages; !strings.Contains(c[len(c)-1].Content, "没有这个工具") || len(comps.actCalls()) != 0 {
		t.Errorf("没授权的人硬要调用伙伴工具不该执行：%v %v", c[len(c)-1], comps.actCalls())
	}

	// 授权了、带了 /问：也没有
	f.advance(time.Minute)
	f.ai.queue(textReply("好的。"))
	f.say("Steve", "@AI /问 让小方跟着我")
	if names := parseReq(t, f.ai.requests()[2]).toolNames(); !slices.Equal(names, []string{"game_info"}) {
		t.Errorf("带了 /问 时只能有 game_info：%v", names)
	}

	// 授权、没带 /问：game_info、run_command，再加 8 个伙伴工具（顺序固定）
	f.advance(time.Minute)
	f.ai.queue(textReply("好。"))
	f.say("Steve", "@AI 让小方跟着我")
	r3 := parseReq(t, f.ai.requests()[3])
	if names := r3.toolNames(); !slices.Equal(names, append([]string{"game_info", "run_command"}, companionToolNames()...)) {
		t.Errorf("授权的人应多一组伙伴工具：%v", names)
	}
	if sys := r3.Messages[0].Content; !strings.Contains(sys, "companion_ 开头的工具") {
		t.Errorf("系统提示：%s", sys)
	}

	// 模组不支持 attack：不给这个工具
	comps.mu.Lock()
	comps.ops = slices.DeleteFunc(comps.ops, func(o string) bool { return o == "attack" })
	comps.mu.Unlock()
	f.advance(time.Minute)
	f.ai.queue(textReply("好。"))
	f.say("Steve", "@AI 你好")
	if names := parseReq(t, f.ai.requests()[4]).toolNames(); slices.Contains(names, "companion_attack") || !slices.Contains(names, "companion_follow") {
		t.Errorf("模组不支持的动作不给：%v", names)
	}

	// 没有伙伴（没启用、没连着）：不提伙伴，系统提示和原来一字不差
	comps.mu.Lock()
	comps.live = false
	comps.mu.Unlock()
	f.advance(time.Minute)
	f.ai.queue(textReply("好。"))
	f.say("Steve", "@AI 你好")
	r5 := parseReq(t, f.ai.requests()[5])
	if names := r5.toolNames(); !slices.Equal(names, []string{"game_info", "run_command"}) || r5.Messages[0].Content != systemPrompt(true, compNone) ||
		strings.Contains(r5.Messages[0].Content, "伙伴") || strings.Contains(r5.Messages[len(r5.Messages)-1].Content, "伙伴") {
		t.Errorf("没有伙伴时：%v\n%s", names, r5.Messages[0].Content)
	}
}

// 「@小方 跟着我」：等同于对小方说的 @AI——用户消息写明是对小方说的，回复以小方的名义发出（[小方→Steve]），
// 用量记在 AI 伙伴名下。「跟着我」的玩家由程序补成提问的人；动作记操作记录（来源 companion）。全角 ＠ 也认；
// 「@小方abc」不触发；伙伴的名字和前缀撞了时前缀优先；没连着伙伴模组时不认伙伴的名字。
func TestCompanionNameTrigger(t *testing.T) {
	comps := newComps()
	f := newFixtureWith(t, comps)
	f.set(Patch{Allowed: &[]string{"Steve"}})
	f.ai.queue(toolReply(call{"c1", "companion_follow", `{}`}), textReply("好嘞，我跟着你。"))
	f.say("Steve", "@小方 跟着我")
	reqs := f.ai.requests()
	if len(reqs) != 2 {
		t.Fatalf("应请求 2 次：%d", len(reqs))
	}
	u := parseReq(t, reqs[0]).Messages
	if c := u[len(u)-1].Content; !strings.Contains(c, "这句话是对伙伴「小方」说的。") || !strings.Contains(c, "问题：跟着我") {
		t.Errorf("用户消息：%s", c)
	}
	calls := comps.actCalls()
	if len(calls) != 1 || calls[0].op != "follow" || calls[0].args["player"] != "Steve" || calls[0].origin != (companion.Origin{Player: "Steve", Companion: "小方"}) {
		t.Fatalf("动作：%+v", calls)
	}
	m := parseReq(t, reqs[1]).Messages
	if c := m[len(m)-1].Content; !strings.Contains(c, "「小方」开始跟着 Steve了") || !strings.Contains(c, "不用等它做完") {
		t.Errorf("交还给 AI 的结果：%s", c)
	}
	got := f.tellraws()
	if last := got[len(got)-1]; last != (sentMsg{"@a", "[小方→Steve] ", "好嘞，我跟着你。", "white"}) {
		t.Errorf("回复应以小方的名义：%+v", last)
	}
	h := f.history()[0]
	if h.Companion != "小方" || len(h.Actions) != 1 || !h.Actions[0].OK || h.Actions[0].Task != 1 || h.Actions[0].Title != "跟着 Steve" {
		t.Errorf("最近的问答：%+v", h)
	}
	if u, _ := f.svc.Usage(ai.FeatCompanion); u.Requests != 2 || u.Tokens != 300 {
		t.Errorf("用量应记在 AI 伙伴名下：%+v", u)
	}
	if u, _ := f.svc.Usage(ai.FeatGameChat); u.Requests != 0 {
		t.Errorf("游戏内 AI 不该记：%+v", u)
	}
	var act, ask *ops.Record
	recs := f.oplog()
	for i := range recs {
		switch recs[i].Op {
		case "companion.action":
			act = &recs[i]
		case "gameai.ask":
			ask = &recs[i]
		}
	}
	if act == nil || act.Src != ops.SrcCompanion || act.Actor != "Steve" || !act.OK || !strings.Contains(string(act.Args), `"op":"follow"`) ||
		!strings.Contains(string(act.Args), `"companion":"小方"`) || !strings.Contains(string(act.Args), `"task":1`) {
		t.Errorf("动作的操作记录：%+v", act)
	}
	if ask == nil || !strings.Contains(string(ask.Args), `"companion":"小方"`) || !strings.Contains(string(ask.Args), `"actions":1`) {
		t.Errorf("问答的操作记录：%+v", ask)
	}

	// 全角 ＠、私聊
	f.advance(time.Minute)
	f.ai.queue(textReply("我在这儿。"))
	f.say("Alex", "＠小方 /私 你在哪")
	if last := f.tellraws()[len(f.tellraws())-1]; last != (sentMsg{"Alex", "[小方] ", "我在这儿。", "white"}) {
		t.Errorf("全角 ＠、/私：%+v", last)
	}
	// 名字后面直接跟别的字：不触发
	n := len(f.ai.requests())
	f.advance(time.Minute)
	f.say("Alex", "@小方abc 你好")
	if len(f.ai.requests()) != n {
		t.Error("「@小方abc」不该触发")
	}
	// 伙伴叫 AI：和前缀撞了，@AI 还是游戏内 AI
	comps.mu.Lock()
	comps.list = append(comps.list, companion.Companion{ID: 2, Name: "ai", State: "idle"})
	comps.mu.Unlock()
	f.advance(time.Minute)
	f.ai.queue(textReply("你好。"))
	f.say("Alex", "@AI 你好")
	if last := f.tellraws()[len(f.tellraws())-1]; last.head != "[AI→Alex] " || f.history()[0].Companion != "" {
		t.Errorf("前缀优先：%+v %+v", last, f.history()[0])
	}
	// 没连着伙伴模组：不认伙伴的名字
	comps.mu.Lock()
	comps.live = false
	comps.mu.Unlock()
	n = len(f.ai.requests())
	f.advance(time.Minute)
	f.say("Steve", "@小方 跟着我")
	if len(f.ai.requests()) != n {
		t.Error("没连着伙伴模组时不该认伙伴的名字")
	}

	// 识别测试也认伙伴的名字
	comps.mu.Lock()
	comps.live = true
	comps.mu.Unlock()
	tr, err := f.gc.Test(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: <Steve> @小方 跟着我", nil)
	if err != nil || tr.Companion != "小方" || !strings.Contains(tr.Result, "对伙伴「小方」说的") {
		t.Errorf("识别测试：%+v %v", tr, err)
	}
}

// 任务做完或失败时：按模板私下告诉发起的玩家（开头是伙伴的名字），不调用 AI、不算次数、不扣额度；
// 不在线不发（只记一笔）；游戏内 AI 关着时什么都不做。
func TestTaskNotificationTemplate(t *testing.T) {
	comps := newComps()
	f := newFixtureWith(t, comps)
	before := len(f.ai.requests())
	f.gc.NotifyTask(companion.Finished{Inst: instName, Origin: companion.Origin{Player: "Steve", Companion: "小方"}, Companion: "小方", TaskID: 3,
		Op: "collect", Result: "done", Text: "采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 5 个，用时 10 秒。"})
	got := f.tellraws()
	if len(got) != 1 || got[0] != (sentMsg{"Steve", "[小方] ", "采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 5 个，用时 10 秒。", "white"}) {
		t.Fatalf("通知：%+v", got)
	}
	f.b.mu.Lock()
	echo := f.b.echoes[len(f.b.echoes)-1]
	f.b.mu.Unlock()
	if !strings.Contains(echo, "伙伴「小方」的任务通知（只发给 Steve）") {
		t.Errorf("控制台的回显：%s", echo)
	}
	if len(f.ai.requests()) != before {
		t.Error("任务通知不该调用 AI")
	}
	h := f.history()[0]
	if h.Kind != "task" || h.Companion != "小方" || h.Player != "Steve" || !h.Private || len(h.Actions) != 1 || h.Actions[0].Task != 3 || !h.Actions[0].OK {
		t.Errorf("最近的问答里记一笔：%+v", h)
	}
	if v, _ := f.gc.View(instName); v.Today.Count != 0 || v.Today.Tokens != 0 {
		t.Errorf("不算次数、不扣额度：%+v", v.Today)
	}
	// 失败的：灰色
	f.gc.NotifyTask(companion.Finished{Inst: instName, Origin: companion.Origin{Player: "Steve"}, Companion: "小方", Op: "goto", Result: "failed", Text: "去 (1, 2) 失败了：找不到路。"})
	if got := f.tellraws(); got[len(got)-1].color != "gray" {
		t.Errorf("失败的用灰色：%+v", got[len(got)-1])
	}
	// 不在线：不发
	n := len(f.tellraws())
	f.gc.NotifyTask(companion.Finished{Inst: instName, Origin: companion.Origin{Player: "Notch"}, Companion: "小方", Op: "goto", Result: "done", Text: "去 (1, 2) 做完了。"})
	if len(f.tellraws()) != n || !strings.Contains(f.history()[0].Note, "Notch 不在线") {
		t.Errorf("不在线时只记一笔：%+v", f.history()[0])
	}
	// 游戏内 AI 关着：什么都不做
	f.set(Patch{Enabled: ptr(false)})
	f.gc.NotifyTask(companion.Finished{Inst: instName, Origin: companion.Origin{Player: "Steve"}, Companion: "小方", Op: "goto", Result: "done", Text: "去 (1, 2) 做完了。"})
	if len(f.tellraws()) != n {
		t.Error("游戏内 AI 关着时不该发")
	}
}

// AI 伙伴的额度：「设置 → AI」里单独一行，用完以后叫伙伴的名字只回一句提示、不调用 AI；@AI 照常（游戏内 AI 的额度另算）。
// /额度 说明 AI 伙伴的总额度。
func TestCompanionQuota(t *testing.T) {
	comps := newComps()
	f := newFixtureWith(t, comps)
	if err := f.svc.SetQuota(map[ai.Feature]int64{ai.FeatCompanion: 100}); err != nil {
		t.Fatal(err)
	}
	f.ai.queue(textReply("你好呀。"))
	f.say("Alex", "@小方 你好")
	if len(f.ai.requests()) != 1 {
		t.Fatal("额度没用完时照常回答")
	}
	f.advance(time.Minute)
	f.say("Alex", "@小方 你好")
	got := f.tellraws()
	if last := got[len(got)-1]; last.target != "Alex" || !strings.Contains(last.body, "AI 伙伴今天的额度用完了") || len(f.ai.requests()) != 1 {
		t.Errorf("AI 伙伴的额度用完了：%+v", last)
	}
	f.advance(time.Minute)
	f.ai.queue(textReply("你好。"))
	f.say("Alex", "@AI 你好")
	if len(f.ai.requests()) != 2 {
		t.Error("游戏内 AI 的额度另算，照常回答")
	}
	f.advance(time.Minute)
	f.say("Alex", "@小方 /额度")
	if last := f.tellraws()[len(f.tellraws())-1]; !strings.Contains(last.body, "AI 伙伴今天的总额度已经用完了") {
		t.Errorf("/额度：%+v", last)
	}
	// /帮助：列出伙伴；有执行权限的人多一句能让伙伴做什么
	f.set(Patch{Allowed: &[]string{"Steve"}})
	f.advance(time.Minute)
	f.say("Steve", "@AI /帮助")
	all := ""
	for _, m := range f.tellraws() {
		all += m.body
	}
	if !strings.Contains(all, "@小方 你好（现有：小方）") || !strings.Contains(all, "你能让伙伴跟着你、采集") {
		t.Errorf("/帮助：%s", all)
	}
}

// 指挥伙伴的权限在执行时再查一次（排队的时候服主收回了授权，就不执行）；一个问题最多让伙伴做 4 件事（查状态不算）。
func TestCompanionActChecks(t *testing.T) {
	comps := newComps()
	f := newFixtureWith(t, comps)
	f.set(Patch{Allowed: &[]string{"Steve"}})
	calls := []call{{"c0", "companion_status", `{"inventory":true}`}}
	f.ai.queue(toolReply(call{"s1", "companion_say", js(map[string]string{"text": "§c大家好\n我是小方"})}), textReply("说了。"))
	f.say("Steve", "@AI 让小方打个招呼")
	if a := comps.actCalls(); len(a) != 1 || a[0].op != "say" || a[0].args["text"] != "大家好 我是小方" {
		t.Fatalf("说话要去掉 § 格式码和换行：%+v", a)
	}
	comps.mu.Lock()
	comps.calls = nil
	comps.mu.Unlock()
	f.advance(time.Minute)
	for i := 1; i <= 5; i++ {
		calls = append(calls, call{"c" + string(rune('0'+i)), "companion_collect", `{"item":"minecraft:oak_log","count":1}`})
	}
	f.ai.queue(toolReply(calls[:4]...), toolReply(calls[4:]...), textReply("好了。"))
	f.say("Steve", "@AI 让小方砍点木头")
	acts := comps.actCalls()
	if len(acts) != 5 || acts[0].op != "status" || acts[0].args["inventory"] != true {
		t.Fatalf("查状态 + 4 次采集：%+v", acts)
	}
	h := f.history()[0]
	if len(h.Actions) != 6 || h.Actions[5].OK || !strings.Contains(h.Actions[5].Reason, "最多让伙伴做 4 件事") {
		t.Errorf("第 5 次采集不该执行：%+v", h.Actions)
	}
	reqs := f.ai.requests()
	if c := parseReq(t, reqs[3]).Messages; !strings.Contains(c[len(c)-4].Content, "【数据开始") || !strings.Contains(c[len(c)-4].Content, `"name":"小方"`) {
		t.Errorf("查状态的结果交给 AI：%+v", c)
	}

	// 排队的时候收回了授权：不执行
	f.advance(time.Minute)
	f.ai.hold = make(chan struct{})
	f.ai.queue(toolReply(call{"c1", "companion_follow", `{}`}), textReply("嗯。"))
	f.gc.OnLine(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: <Steve> @小方 跟着我")
	f.waitRequests(len(reqs) + 1)
	f.set(Patch{Allowed: &[]string{}})
	close(f.ai.hold)
	f.ai.mu.Lock()
	f.ai.hold = nil
	f.ai.mu.Unlock()
	f.gc.Wait()
	if n := len(comps.actCalls()); n != 5 {
		t.Errorf("收回授权后不该再执行：%d", n)
	}
	if h := f.history()[0]; len(h.Actions) != 1 || h.Actions[0].OK || !strings.Contains(h.Actions[0].Reason, "已经被服主收回") {
		t.Errorf("记录：%+v", h)
	}
}
