package chatai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// 回复默认全服可见：目标 @a，开头 [AI→Steve]；/私 只回提问的人（目标是名字，开头 [AI]）。这次开服第一次有人提问时，
// 先告诉大家一次提问会发给谁（§10.1），只发一次。
func TestReplyTargets(t *testing.T) {
	f := newFixture(t)
	f.ai.queue(textReply("先准备床和弓。"), textReply("附魔台要书、钻石和黑曜石。"))
	f.say("Steve", "@AI 今晚怎么打末影龙")
	f.advance(16 * time.Second) // 过了冷却
	f.say("Steve", "@AI /私 附魔台怎么做")
	got := f.tellraws()
	if len(got) != 4 {
		t.Fatalf("应有 4 条（提示两行、两次回答）：%+v", got)
	}
	if got[0].target != "@a" || got[0].head != "[AI] " || got[0].body != "提示：向 AI 提的问题会发给 127.0.0.1 处理。" || got[0].color != "gray" ||
		got[1] != (sentMsg{"@a", "[AI] ", "只想自己看到回答，在问题前加 /私。", "gray"}) {
		t.Errorf("隐私提示（一句一行）：%+v", got[:2])
	}
	if got[2] != (sentMsg{"@a", "[AI→Steve] ", "先准备床和弓。", "white"}) {
		t.Errorf("默认全服可见：%+v", got[2])
	}
	if got[3] != (sentMsg{"Steve", "[AI] ", "附魔台要书、钻石和黑曜石。", "white"}) {
		t.Errorf("/私 只回 Steve：%+v", got[3])
	}
	// 重新开服后再提示一次
	f.b.mu.Lock()
	f.b.started = f.b.started.Add(2 * time.Hour)
	f.b.mu.Unlock()
	f.b.reset()
	f.advance(time.Minute)
	f.ai.queue(textReply("好的。"))
	f.say("Alex", "@AI 你好")
	if got := f.tellraws(); len(got) != 3 || !strings.Contains(got[0].body, "会发给") {
		t.Errorf("重新开服后应再提示一次：%+v", got)
	}
	// 发给 AI 的用户消息：提问的人和权限；问题脱敏（局域网 IP 换掉）
	f.ai.queue(textReply("好。"))
	f.advance(time.Minute)
	f.say("Alex", "@AI 我的 IP 是 192.168.1.23")
	reqs := f.ai.requests()
	last := parseReq(t, reqs[len(reqs)-1])
	if m := last.Messages[len(last.Messages)-1].Content; !strings.Contains(m, "提问的人：Alex（没有执行命令的权限）") || strings.Contains(m, "192.168.1.23") ||
		!strings.Contains(m, "<局域网IP>") {
		t.Errorf("用户消息：%s", m)
	}
}

// 未授权的玩家要执行命令被拒：请求里根本没有 run_command；模型硬要调用也不执行。
func TestUnauthorizedCannotRunCommands(t *testing.T) {
	f := newFixture(t)
	f.ai.queue(toolReply(cmdCall("c1", "weather clear")), textReply("我这次只能回答问题。"))
	f.say("Alex", "@AI 把天气改晴")
	reqs := f.ai.requests()
	if len(reqs) != 2 {
		t.Fatalf("应请求 2 次：%d", len(reqs))
	}
	if names := parseReq(t, reqs[0]).toolNames(); !slices.Equal(names, []string{"game_info"}) {
		t.Errorf("没有执行权限时只能有 game_info：%v", names)
	}
	if cmds := f.commands(); len(cmds) != 0 {
		t.Errorf("没有执行权限，不该执行任何命令：%v", cmds)
	}
	second := parseReq(t, reqs[1])
	if c := second.Messages[len(second.Messages)-1].Content; !strings.Contains(c, "没有这个工具") {
		t.Errorf("硬要调用的结果应是「没有这个工具」：%s", c)
	}
	if h := f.history(); h[0].Kind != "ask" || h[0].Exec || len(h[0].Commands) != 0 {
		t.Errorf("记录：%+v", h[0])
	}
}

// 授权的玩家说「把天气改晴」：执行 weather clear，服务器的输出交还给 AI，再回复（§12 第 4 行的验收标准）。
func TestAuthorizedWeatherClear(t *testing.T) {
	f := newFixture(t)
	v := f.set(Patch{Allowed: &[]string{"Steve"}})
	if !v.Settings.Authorized("steve") {
		t.Fatal("授权没存上")
	}
	f.ai.queue(toolReply(cmdCall("c1", "weather clear")), textReply("天气已经改成晴天了。"))
	f.say("Steve", "@AI 把天气改晴")
	if cmds := f.commands(); !slices.Equal(cmds, []string{"weather clear"}) {
		t.Fatalf("写进标准输入的命令：%v", cmds)
	}
	reqs := f.ai.requests()
	first := parseReq(t, reqs[0])
	if names := first.toolNames(); !slices.Equal(names, []string{"game_info", "run_command"}) {
		t.Errorf("有执行权限时应多一个 run_command：%v", names)
	}
	if d := first.Tools[1].Function.Description; !strings.Contains(d, "weather clear|rain|thunder") || strings.Contains(d, "gamemode") || strings.Contains(d, "give <自己> <物品>") {
		t.Errorf("run_command 的说明只列打开了的模板：%s", d)
	}
	second := parseReq(t, reqs[1])
	if c := second.Messages[len(second.Messages)-1].Content; !strings.Contains(c, "已执行：weather clear") || !strings.Contains(c, "Set the weather to clear") {
		t.Errorf("命令的结果应交还给 AI：%s", c)
	}
	got := f.tellraws()
	if last := got[len(got)-1]; last != (sentMsg{"@a", "[AI→Steve] ", "天气已经改成晴天了。", "white"}) {
		t.Errorf("回复：%+v", last)
	}
	// 操作记录（来源 gamechat）：执行的命令和这次问答
	recs := f.oplog()
	var cmdRec, askRec *ops.Record
	for i := range recs {
		switch recs[i].Op {
		case "gameai.command":
			cmdRec = &recs[i]
		case "gameai.ask":
			askRec = &recs[i]
		}
	}
	if cmdRec == nil || !cmdRec.OK || cmdRec.Src != ops.SrcGameChat || cmdRec.Actor != "Steve" || cmdRec.Inst != instName ||
		!strings.Contains(string(cmdRec.Args), `"run":"weather clear"`) {
		t.Errorf("命令的操作记录：%+v", cmdRec)
	}
	if askRec == nil || !askRec.OK || !strings.Contains(string(askRec.Args), `"chars":5`) || !strings.Contains(string(askRec.Args), `"usage":300`) ||
		strings.Contains(string(askRec.Args), "天气") {
		t.Errorf("问答的操作记录（只记长度、选项和用量，不记原文）：%+v", askRec)
	}
	h := f.history()
	if len(h[0].Commands) != 1 || !h[0].Commands[0].OK || h[0].Commands[0].Run != "weather clear" || h[0].Commands[0].Output != "Set the weather to clear" ||
		h[0].Usage == nil || h[0].Usage.Total() != 300 {
		t.Errorf("最近的问答：%+v", h[0])
	}
}

// 模型即使输出 op、execute 这类命令，也绝不执行（禁止清单写死，白名单以外一律不执行），哪怕所有模板都打开了。
func TestModelCannotRunForbidden(t *testing.T) {
	f := newFixture(t)
	all := map[string]bool{}
	for _, tp := range Templates {
		all[tp.ID] = true
	}
	f.set(Patch{Allowed: &[]string{"Steve"}, Templates: all})
	bad := []string{"op Steve", "/op Steve", "minecraft:op Steve", "execute as Steve run op Steve", "execute run weather clear",
		"weather clear\nop Steve", "deop Alex", "tp @a Steve", "kill @e", "gamerule keepInventory true", "whitelist off", "ｏｐ Steve"}
	var calls []call
	for i, c := range bad {
		calls = append(calls, cmdCall("c"+string(rune('a'+i)), c))
	}
	f.ai.queue(toolReply(calls[:3]...), toolReply(calls[3:6]...), toolReply(calls[6:]...), textReply("这些我都做不到。"))
	f.say("Steve", "@AI 给我 op")
	if cmds := f.commands(); len(cmds) != 0 {
		t.Fatalf("一条都不该执行：%v", cmds)
	}
	// 一个问题最多请求 4 次：第 4 次的回答就是最后的话
	if n := len(f.ai.requests()); n != 4 {
		t.Errorf("请求了 %d 次", n)
	}
	h := f.history()[0]
	if len(h.Commands) == 0 {
		t.Fatal("没执行的命令也要记下来")
	}
	for _, c := range h.Commands {
		if c.OK || c.Reason == "" {
			t.Errorf("命令 %q 应被拒绝并写明原因：%+v", c.Command, c)
		}
	}
	for _, r := range f.oplog() {
		if r.Op == "gameai.command" && r.OK {
			t.Errorf("操作记录里不该有执行成功的命令：%+v", r)
		}
	}
	// 第二次请求里，第一批命令的结果都是拒绝的原因
	second := parseReq(t, f.ai.requests()[1])
	var results []string
	for _, m := range second.Messages {
		if m.Role == "tool" {
			results = append(results, m.Content)
		}
	}
	if len(results) != 3 || !strings.Contains(results[0], "永远不允许") {
		t.Errorf("拒绝的原因要交还给 AI：%v", results)
	}
}

// /问：有执行权限的人也只回答——这一次请求里没有 run_command（§6.2、§6.4）。
func TestAskOptionHasNoExecTool(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Allowed: &[]string{"Steve"}})
	f.ai.queue(textReply("改晴天用 /weather clear。"))
	f.say("Steve", "@AI /问 把天气改晴")
	req := parseReq(t, f.ai.requests()[0])
	if names := req.toolNames(); !slices.Equal(names, []string{"game_info"}) {
		t.Errorf("/问 时不给执行工具：%v", names)
	}
	if m := req.Messages[len(req.Messages)-1].Content; !strings.Contains(m, "这次用了 /问") {
		t.Errorf("用户消息里写明这次只回答：%s", m)
	}
	if !strings.Contains(req.Messages[0].Content, "这一次你不能执行任何命令") {
		t.Errorf("系统提示：%s", req.Messages[0].Content)
	}
	if got := f.tellraws(); got[len(got)-1].target != "@a" {
		t.Errorf("/问 不影响全服可见：%+v", got)
	}
	if len(f.commands()) != 0 {
		t.Error("/问 时不执行命令")
	}
}

// /帮助、/额度、未知选项、没写问题、选项放错位置：不调用 AI、不扣次数、不触发冷却，系统提示一律私发（§6.2、§6.7）。
func TestNoAIRepliesAreFree(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Allowed: &[]string{"Steve"}})
	msgs := []string{"@AI /帮助", "@AI /h", "@AI", "@AI /私"}
	for _, m := range msgs {
		f.say("Steve", m)
		f.advance(13 * time.Second) // 1 分钟内不超过 5 次（防刷屏另测）
	}
	got := f.tellraws()
	if len(got) < 4 {
		t.Fatalf("帮助：%+v", got)
	}
	for _, m := range got {
		if m.target != "Steve" || m.head != "[AI] " || m.color != "gray" {
			t.Errorf("系统提示一律私发：%+v", m)
		}
	}
	all := strings.Join(func() (out []string) {
		for _, m := range got {
			out = append(out, m.body)
		}
		return
	}(), "\n")
	if !strings.Contains(all, "用法：@AI 你的问题") || !strings.Contains(all, "/问（只回答、不执行命令）") || !strings.Contains(all, "改天气") {
		t.Errorf("有执行权限的人的帮助：%s", all)
	}
	for _, c := range []struct{ msg, want, not string }{
		{"@AI /额度", "你今天还能问 60 次（每天 60 次）。", ""},
		{"@AI /quota", "你今天还能问 60 次", ""},
		{"@AI /tp Steve", "不认识 /tp。", "/问"},
		{"@AI /私今晚怎么打末影龙", "空一格", ""},
		{"@AI 今晚怎么打末影龙 /私", "选项要写在问题前面", ""},
	} {
		f.b.reset()
		f.say("Alex", c.msg)
		f.advance(13 * time.Second)
		got := f.tellraws()
		var all []string
		for _, m := range got {
			if m.target != "Alex" || m.color != "gray" {
				t.Errorf("%s：系统提示一律私发：%+v", c.msg, m)
			}
			all = append(all, m.body)
		}
		if body := strings.Join(all, "\n"); len(got) == 0 || !strings.Contains(body, c.want) || (c.not != "" && strings.Contains(body, c.not)) {
			t.Errorf("%s 的回复：%s", c.msg, body)
		}
	}
	if n := len(f.ai.requests()); n != 0 {
		t.Errorf("不该调用 AI：%d 次", n)
	}
	if v, _ := f.gc.View(instName); v.Today.Count != 0 || len(v.Today.Players) != 0 {
		t.Errorf("不该扣次数：%+v", v.Today)
	}
	// 没触发冷却：马上提问照样受理
	f.ai.queue(textReply("好。"))
	f.say("Alex", "@AI 你好")
	if n := len(f.ai.requests()); n != 1 {
		t.Errorf("提问应被受理：%d", n)
	}
	if v, _ := f.gc.View(instName); v.Today.Count != 1 || v.Today.Players["alex"] != 1 {
		t.Errorf("提问才算次数：%+v", v.Today)
	}
	// 没有执行权限的人的帮助里不显示 /问
	f.b.reset()
	f.advance(time.Minute)
	f.say("Alex", "@AI /帮助")
	if got := f.tellraws(); len(got) == 0 || strings.Contains(got[0].body, "/问") {
		t.Errorf("没有执行权限的人不显示 /问：%+v", got)
	}
}

// 刷屏被限住：同一个人 1 分钟内超过 5 次（不调用 AI 的回复和受理了的提问才算），第 6 次不回答，私下说「你问得太快了，3 分钟后再问」，
// 静默 3 分钟；静默期间再问，离上次提醒满 1 分钟才再私下提醒一次还要等多久，其余的不出声。开始静默记一条最近的问答和一条操作记录。
func TestSpamIsSilenced(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		f.say("Steve", "@AI /帮助")
		f.advance(5 * time.Second)
	}
	f.b.reset()
	f.say("Steve", "@AI /帮助") // 第 6 次：不回帮助，开始静默
	lastBody := func() []string {
		var out []string
		for _, m := range f.tellraws() {
			if m.target != "Steve" || m.color != "gray" {
				t.Errorf("静默的提醒只发给本人：%+v", m)
			}
			out = append(out, m.body)
		}
		f.b.reset()
		return out
	}
	if got := lastBody(); !slices.Equal(got, []string{"你问得太快了，3 分钟后再问。"}) {
		t.Errorf("第一次静默私下说一句：%v", got)
	}
	f.advance(10 * time.Second)
	f.say("Steve", "@AI 今晚怎么打末影龙")
	if got := lastBody(); len(got) != 0 {
		t.Errorf("刚提醒过，不再出声：%v", got)
	}
	f.advance(51 * time.Second) // 离第一次提醒满 1 分钟：再说一次剩下的时间（还剩 119 秒，按分钟向上取整）
	f.say("Steve", "@AI /额度")
	if got := lastBody(); !slices.Equal(got, []string{"你问得太快了，2 分钟后再问。"}) {
		t.Errorf("隔 1 分钟再提醒一次：%v", got)
	}
	f.advance(50 * time.Second)
	f.say("Steve", "@AI /帮助")
	if got := lastBody(); len(got) != 0 {
		t.Errorf("不满 1 分钟不再提醒：%v", got)
	}
	f.advance(59 * time.Second) // 还剩 10 秒
	f.say("Steve", "@AI 你好")
	if got := lastBody(); !slices.Equal(got, []string{"你问得太快了，10 秒后再问。"}) {
		t.Errorf("不满 1 分钟按秒说：%v", got)
	}
	if len(f.ai.requests()) != 0 {
		t.Error("静默期间不调用 AI")
	}
	var sil []Record
	for _, r := range f.history() {
		if r.Kind == "silenced" {
			sil = append(sil, r)
		}
	}
	if len(sil) != 1 || sil[0].Reply != "你问得太快了，3 分钟后再问。" || !sil[0].Private || !strings.Contains(sil[0].Note, "3 分钟") {
		t.Errorf("开始静默时记一条最近的问答（提醒不再记）：%+v", sil)
	}
	var lim []ops.Record
	for _, r := range f.oplog() {
		if r.Op == "gameai.limit" {
			lim = append(lim, r)
		}
	}
	if len(lim) != 1 || lim[0].OK || lim[0].Src != ops.SrcGameChat || lim[0].Actor != "Steve" || lim[0].Inst != instName ||
		!strings.Contains(string(lim[0].Args), `"result":"silenced"`) || !strings.Contains(string(lim[0].Args), `"minutes":3`) || !strings.Contains(lim[0].Err, "3 分钟") {
		t.Errorf("静默写进操作记录：%+v", lim)
	}
	// 别人不受影响
	f.say("Alex", "@AI /帮助")
	if got := f.tellraws(); len(got) == 0 || got[0].target != "Alex" || !strings.Contains(got[0].body, "用法") {
		t.Errorf("别人不受影响：%+v", got)
	}
	f.b.reset()
	// 3 分钟后恢复
	f.advance(11 * time.Second)
	f.say("Steve", "@AI /帮助")
	if got := f.tellraws(); len(got) == 0 || !strings.Contains(got[0].body, "用法") {
		t.Errorf("静默结束后应恢复：%+v", got)
	}
}

// 被冷却挡回的提问不算进防刷屏（测试报告 F-12：原来越急着问越快被静默）：问一次，冷却期间连问 6 次，
// 只在第一次被挡时私下说「冷却中」（同一个冷却期不重复说、不重复记），冷却过了照样受理；下一个冷却期被挡再说一次。
func TestCooldownNotCountedAsSpam(t *testing.T) {
	f := newFixture(t)
	f.ai.queue(textReply("答一。"), textReply("答二。"))
	f.say("Steve", "@AI 一")
	f.b.reset()
	for i := 0; i < 6; i++ {
		f.advance(time.Second)
		f.say("Steve", "@AI 再问一次")
	}
	if got := f.tellraws(); len(got) != 1 || got[0].target != "Steve" || got[0].body != "冷却中，7 秒后再问。" {
		t.Errorf("同一个冷却期只说一次：%+v", got)
	}
	f.advance(2 * time.Second) // 冷却 8 秒过了
	f.say("Steve", "@AI 二")
	if n := len(f.ai.requests()); n != 2 {
		t.Fatalf("冷却过了应受理（被挡的不算刷屏）：请求了 %d 次", n)
	}
	f.b.reset()
	f.advance(time.Second)
	f.say("Steve", "@AI 三")
	if got := f.tellraws(); len(got) != 1 || got[0].body != "冷却中，7 秒后再问。" {
		t.Errorf("下一个冷却期被挡，再说一次：%+v", got)
	}
	cd := 0
	for _, r := range f.history() {
		if r.Kind == "cooldown" {
			cd++
		}
	}
	lim := 0
	for _, r := range f.oplog() {
		if r.Op == "gameai.limit" && strings.Contains(string(r.Args), `"result":"cooldown"`) && !r.OK && strings.Contains(r.Err, "冷却中") {
			lim++
		}
	}
	if cd != 2 || lim != 2 {
		t.Errorf("冷却每个冷却期只记一条：最近的问答 %d 条、操作记录 %d 条", cd, lim)
	}
	if v, _ := f.gc.View(instName); v.Today.Players["steve"] != 2 {
		t.Errorf("被挡回的不算次数：%+v", v.Today)
	}
}

// 受理了的提问也算进防刷屏：不冷却时 1 分钟内问到第 6 个，不再回答，改成私下说「你问得太快了」。
func TestAcceptedQuestionsCountAsSpam(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Cooldown: ptr(0)})
	for i := 0; i < 5; i++ {
		f.ai.queue(textReply("答。"))
		f.say("Steve", "@AI 问题")
		f.advance(5 * time.Second)
	}
	f.b.reset()
	f.say("Steve", "@AI 第六个问题")
	if n := len(f.ai.requests()); n != 5 {
		t.Errorf("第 6 个不该问 AI：请求了 %d 次", n)
	}
	if got := f.tellraws(); len(got) != 1 || got[0].body != "你问得太快了，3 分钟后再问。" {
		t.Errorf("第 6 个：%+v", got)
	}
	if v, _ := f.gc.View(instName); v.Today.Players["steve"] != 5 {
		t.Errorf("没受理的不算次数：%+v", v.Today)
	}
}

// 次数用完、正忙这类拒绝：同一个人同一种，隔 30 秒才再说一次（不说的也不记），说出来的次数用完记一条操作记录。
func TestRefusalThrottle(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{PerPlayer: ptr(1), Cooldown: ptr(0)})
	f.ai.queue(textReply("答。"))
	f.say("Steve", "@AI 一")
	f.b.reset()
	f.say("Steve", "@AI 二")
	f.advance(10 * time.Second)
	f.say("Steve", "@AI 三")
	if got := f.tellraws(); len(got) != 1 || got[0].body != "你今天的 1 次已经用完了，明天再来吧。" {
		t.Errorf("30 秒内只说一次：%+v", got)
	}
	f.advance(21 * time.Second)
	f.say("Steve", "@AI 四")
	if got := f.tellraws(); len(got) != 2 {
		t.Errorf("隔 30 秒再说一次：%+v", got)
	}
	n := 0
	for _, r := range f.oplog() {
		if r.Op == "gameai.limit" && strings.Contains(string(r.Args), `"result":"limit"`) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("说出来的次数用完各记一条操作记录：%d 条", n)
	}
	// 被挡回的不算进防刷屏：一直问也不会被静默
	for i := 0; i < 10; i++ {
		f.advance(time.Second)
		f.say("Steve", "@AI 再问")
	}
	if slices.ContainsFunc(f.history(), func(r Record) bool { return r.Kind == "silenced" }) {
		t.Error("被次数挡回的不该算进防刷屏")
	}
}

// 冷却、每人每天、全服每天的次数、全服每天的 token（§6.7），过了 0 点清零。
func TestLimits(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{PerPlayer: ptr(2), PerServer: ptr(3)})
	answer := func(who, q string) string {
		f.t.Helper()
		f.b.reset()
		f.ai.queue(textReply("答。"))
		f.say(who, "@AI "+q)
		got := f.tellraws()
		if len(got) == 0 {
			t.Fatalf("%s 的「%s」没有回复", who, q)
		}
		return got[len(got)-1].body
	}
	if r := answer("Steve", "一"); r != "答。" {
		t.Fatalf("第一次：%s", r)
	}
	if r := answer("Steve", "二"); !strings.Contains(r, "冷却中") || !strings.Contains(r, "8 秒后再问") {
		t.Errorf("冷却（默认 8 秒）：%s", r)
	}
	f.advance(16 * time.Second)
	if r := answer("Steve", "三"); r != "答。" {
		t.Errorf("冷却过了：%s", r)
	}
	f.advance(16 * time.Second)
	if r := answer("Steve", "四"); !strings.Contains(r, "你今天的 2 次已经用完了") {
		t.Errorf("每人每天：%s", r)
	}
	if r := answer("Alex", "五"); r != "答。" {
		t.Errorf("Alex 不受 Steve 的次数影响：%s", r)
	}
	f.advance(16 * time.Second)
	if r := answer("Alex", "六"); !strings.Contains(r, "全服今天的 3 次已经用完了") {
		t.Errorf("全服每天：%s", r)
	}
	// /额度 看得到
	f.b.reset()
	f.say("Alex", "@AI /额度")
	if got := f.tellraws(); len(got) != 2 || got[0].body != "你今天还能问 1 次（每天 2 次）。" || got[1].body != "全服今天的次数已经用完了。" {
		t.Errorf("/额度（一句一行）：%+v", got)
	}
	// 过了 0 点清零
	f.advance(24 * time.Hour)
	if r := answer("Steve", "第二天"); r != "答。" {
		t.Errorf("第二天应清零：%s", r)
	}
	// 全服每天的 token：一次请求 150 个，上限 200，第二个问题就不受理了
	f.set(Patch{PerServer: ptr(0), PerPlayer: ptr(0), Budget: ptr(int64(200))})
	f.advance(24 * time.Hour)
	f.advance(time.Minute)
	if r := answer("Alex", "甲"); r != "答。" {
		t.Errorf("token 还够：%s", r)
	}
	f.advance(time.Minute)
	f.ai.queue(textReply("答。"))
	f.advance(time.Minute)
	if r := answer("Alex", "乙"); r != "答。" {
		t.Errorf("150 < 200，还能问：%s", r)
	}
	f.advance(time.Minute)
	if r := answer("Steve", "丙"); !strings.Contains(r, "全服今天的 AI 额度用完了") {
		t.Errorf("token 用完：%s", r)
	}
	// 次数和用量存在实例目录里，重启 MineDeck 也不清零
	g := New(Options{Backend: f.b, AI: f.svc, Ops: f.reg, Now: f.clockNow})
	if v, err := g.View(instName); err != nil || v.Today.Tokens != 300 || v.Today.Count != 2 {
		t.Errorf("重启后读回：%+v %v", v, err)
	}
}

// 同一实例同一时间只处理 1 个问题，最多排队 3 个；满了就私下回复「AI 正忙」。
func TestBusyQueue(t *testing.T) {
	f := newFixture(t)
	hold := make(chan struct{})
	f.ai.mu.Lock()
	f.ai.hold = hold
	f.ai.mu.Unlock()
	f.b.mu.Lock()
	f.b.online = []string{"P1", "P2", "P3", "P4", "P5", "P6"}
	f.b.mu.Unlock()
	for i, p := range []string{"P1", "P2", "P3", "P4", "P5", "P6"} {
		f.ai.queue(textReply("答 " + p))
		f.gc.OnLine(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: <"+p+"> @AI 问题")
		if i == 0 {
			f.waitRequests(1) // 第一个问题已经在问 AI 了（卡在 hold 上），后面的排队
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	var busy []sentMsg
	for time.Now().Before(deadline) {
		busy = nil
		for _, m := range f.tellraws() {
			if strings.Contains(m.body, "AI 正忙") {
				busy = append(busy, m)
			}
		}
		if len(busy) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(busy) != 2 || busy[0].target != "P5" || busy[1].target != "P6" {
		t.Errorf("1 个在处理、3 个排队，第 5、6 个应收到「AI 正忙」：%+v", busy)
	}
	close(hold)
	f.gc.Wait()
	answered := 0
	for _, m := range f.tellraws() {
		if strings.HasPrefix(m.body, "答 ") {
			answered++
		}
	}
	if answered != 4 {
		t.Errorf("应答了 4 个：%d", answered)
	}
}

// 不在线的人、关着的游戏内 AI、没配置的 AI；名字不合规的不处理。
func TestIgnoredAndDisabled(t *testing.T) {
	f := newFixture(t)
	f.say("Notch", "@AI 你好") // 不在线
	f.gc.OnLine(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: <Mr-X> @AI 你好")
	f.gc.Wait()
	if len(f.b.lines()) != 0 || len(f.ai.requests()) != 0 {
		t.Errorf("不在线、名字不合规的不处理：%v", f.b.lines())
	}
	f.set(Patch{Enabled: ptr(false)})
	f.say("Steve", "@AI 你好")
	if len(f.b.lines()) != 0 {
		t.Errorf("关着时不回复：%v", f.b.lines())
	}
	f.set(Patch{Enabled: ptr(true)})
	if err := f.svc.SetProvider(ai.ProviderPatch{Preset: ptr("")}); err != nil {
		t.Fatal(err)
	}
	f.say("Steve", "@AI 你好")
	got := f.tellraws()
	if len(got) != 1 || got[0].target != "Steve" || !strings.Contains(got[0].body, "还没配置 AI") {
		t.Errorf("没配置 AI 时私下说明：%+v", got)
	}
	if len(f.ai.requests()) != 0 {
		t.Error("没配置 AI 时不联网")
	}
}

// 空回复：提示「AI 没有给出回复」（私发），照样算一次。
func TestEmptyReply(t *testing.T) {
	f := newFixture(t)
	f.ai.queue(emptyReply())
	f.say("Steve", "@AI 你好")
	got := f.tellraws()
	if last := got[len(got)-1]; last.target != "Steve" || !strings.Contains(last.body, "AI 没有给出回复") {
		t.Errorf("空回复：%+v", got)
	}
	if h := f.history()[0]; h.Kind != "failed" || !strings.Contains(h.Err, "模型没有给出回复") {
		t.Errorf("记录：%+v", h)
	}
	// 只有空白的回复也一样（不发一条空的 tellraw）
	f.advance(16 * time.Second)
	f.ai.queue(textReply(" \n\t "))
	f.say("Steve", "@AI 你好")
	n := len(got)
	got = f.tellraws()
	if len(got) != n+1 || got[n].target != "Steve" || !strings.Contains(got[n].body, "AI 没有给出回复") {
		t.Errorf("空白回复：%+v", got)
	}
	if h := f.history()[0]; h.Kind != "failed" || !strings.Contains(h.Err, "模型没有给出回复") {
		t.Errorf("记录：%+v", h)
	}
}

// 执行权限在排队、回答的时候被收回了：命令照样不执行（每条命令执行前按最新的设置再查一次）。
func TestRevokedBeforeRun(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Allowed: &[]string{"Steve"}})
	hold := make(chan struct{})
	f.ai.mu.Lock()
	f.ai.hold = hold
	f.ai.mu.Unlock()
	f.ai.queue(toolReply(cmdCall("c1", "time set day")), textReply("好了。"))
	f.gc.OnLine(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: <Steve> @AI 改成白天")
	f.waitRequests(1) // 已经受理、正在问 AI
	f.set(Patch{Allowed: &[]string{}})
	f.ai.mu.Lock()
	f.ai.hold = nil
	f.ai.mu.Unlock()
	close(hold)
	f.gc.Wait()
	if cmds := f.commands(); len(cmds) != 0 {
		t.Errorf("收回授权后不执行：%v", cmds)
	}
	if h := f.history()[0]; len(h.Commands) != 1 || h.Commands[0].OK || !strings.Contains(h.Commands[0].Reason, "收回") {
		t.Errorf("记录：%+v", h)
	}
}

// game_info 经操作层调用（来源 gamechat），实例由程序填，结果脱敏后加边界标记交给 AI。
func TestGameInfoViaOps(t *testing.T) {
	f := newFixture(t)
	f.ai.queue(toolReply(call{"g1", "game_info", `{"inst":"别的服"}`}), textReply("现在 Steve 和 Alex 在线。"))
	f.say("Steve", "@AI 谁在线")
	second := parseReq(t, f.ai.requests()[1])
	c := second.Messages[len(second.Messages)-1].Content
	if !strings.Contains(c, dataBegin) || !strings.Contains(c, `"online":["Steve","Alex"]`) || strings.Contains(c, "192.168.1.23") {
		t.Errorf("game_info 的结果：%s", c)
	}
	// 游戏内调用不了别的操作：操作层只放行 InGame 的读操作
	f.reg.Register(&ops.Op{Name: "instance.stop", Title: "停服", Level: ops.Danger, Run: func(context.Context, ops.Caller, ops.Args) (any, error) { return nil, nil }})
	if _, err := f.reg.Do(t.Context(), ops.Caller{Source: ops.SrcGameChat, Actor: "Steve"}, "instance.stop", nil); ops.CodeOf(err) != 403 {
		t.Errorf("游戏内调用写操作应 403：%v", err)
	}
}

// 设置：只改传了的字段；前缀、别名、授权名单、正则不合规时整次不改。
func TestSettingsPatch(t *testing.T) {
	f := newFixture(t)
	v := f.set(Patch{Prefix: ptr("@小助手"), Aliases: &[]string{"@AI", " ", "@ai"}, Templates: map[string]bool{"give": true}})
	if v.Settings.Prefix != "@小助手" || !slices.Equal(v.Settings.Aliases, []string{"@AI"}) || !v.Settings.Templates["give"] || !v.Settings.Templates["time"] {
		t.Errorf("设置：%+v", v.Settings)
	}
	for _, p := range []Patch{
		{Prefix: ptr("")}, {Prefix: ptr("@A I")}, {Prefix: ptr("/ai")}, {Prefix: ptr("@AI:")}, {Aliases: &[]string{"@a/b"}},
		{Allowed: &[]string{"@a"}}, {Allowed: &[]string{"史蒂夫"}}, {Templates: map[string]bool{"op": true}},
		{Cooldown: ptr(-1)}, {Budget: ptr(int64(-5))}, {Pattern: ptr(`(?P<name>\w+)`)}, {Pattern: ptr(`(`)},
	} {
		if _, err := f.gc.Update(instName, p); err == nil {
			t.Errorf("应拒绝：%+v", p)
		}
	}
	got, _ := f.gc.Settings(instName)
	if got.Prefix != "@小助手" {
		t.Errorf("拒绝的改动不该生效：%+v", got)
	}
	// 读回来一样（存在实例目录里）
	g := New(Options{Backend: f.b, AI: f.svc, Ops: f.reg})
	if s, _ := g.Settings(instName); s.Prefix != "@小助手" || !s.Templates["give"] || !s.Enabled {
		t.Errorf("读回：%+v", s)
	}
	b, _ := json.Marshal(Defaults())
	if !strings.Contains(string(b), `"cooldown":8`) || !strings.Contains(string(b), `"perPlayer":60`) || !strings.Contains(string(b), `"perServer":300`) ||
		!strings.Contains(string(b), `"budget":500000`) || !strings.Contains(string(b), `"gamemode":false`) || !strings.Contains(string(b), `"give":false`) {
		t.Errorf("默认值（§6.6、§6.7，2026-11 放宽了冷却和每人每天）：%s", b)
	}
	// 没改过限流：设置文件里不写这四项（以后改默认值，跟着变）
	raw, _ := os.ReadFile(filepath.Join(f.b.dir, settingsFile))
	if !strings.Contains(string(raw), `"v": 2`) || strings.Contains(string(raw), "cooldown") || strings.Contains(string(raw), "perPlayer") {
		t.Errorf("没改过的限流不写进文件：%s", raw)
	}
}

// 出厂默认值只影响用户没改过的项：第 2 版的设置文件只写改过的（填的正好是默认值也算改过）；第一版的文件（没有 v，四项总是写全）
// 里和当时的默认值（15 秒、30 次、300 次、50 万）一样的当没改过，用现在的默认值，不一样的照原样保留。
func TestSettingsDefaultsFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, settingsFile)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"enabled":true,"prefix":"@AI","cooldown":15,"perPlayer":30,"perServer":500,"budget":500000}`)
	s := loadSettings(dir)
	if !s.Enabled || s.Cooldown != DefaultCooldown || s.PerPlayer != DefaultPerPlayer || s.PerServer != 500 || s.Budget != DefaultBudget {
		t.Errorf("第一版里没改过的用新默认值，改过的保留：%+v", s)
	}
	write(`{"enabled":true,"cooldown":20,"perPlayer":0,"perServer":300,"budget":500000}`)
	s = loadSettings(dir)
	if s.Cooldown != 20 || s.PerPlayer != 0 || s.PerServer != DefaultPerServer {
		t.Errorf("第一版里改过的冷却、不限次数保留：%+v", s)
	}
	// 存回去是第 2 版，只写改过的
	if err := saveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"v": 2`) || !strings.Contains(string(raw), `"cooldown": 20`) || !strings.Contains(string(raw), `"perPlayer": 0`) ||
		strings.Contains(string(raw), "perServer") || strings.Contains(string(raw), "budget") {
		t.Errorf("第 2 版只写改过的：%s", raw)
	}
	if s2 := loadSettings(dir); s2.Cooldown != 20 || s2.PerPlayer != 0 || s2.PerServer != DefaultPerServer || s2.Budget != DefaultBudget {
		t.Errorf("读回第 2 版：%+v", s2)
	}
	// 第 2 版里填的正好是默认值：也算改过，照原样保留（第一版才按值猜）
	p, err := Patch{Cooldown: ptr(DefaultCooldown)}.Apply(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := saveSettings(dir, p); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if !strings.Contains(string(raw), `"cooldown": 8`) {
		t.Errorf("填过的默认值也要写：%s", raw)
	}
	write(`{"v":2,"enabled":true,"cooldown":15}`)
	if s := loadSettings(dir); s.Cooldown != 15 || s.PerPlayer != DefaultPerPlayer {
		t.Errorf("第 2 版里的 15 是用户填的，不能当默认值：%+v", s)
	}
	// 坏值照旧回到默认
	write(`{"v":2,"cooldown":99999,"perPlayer":-1}`)
	if s := loadSettings(dir); s.Cooldown != DefaultCooldown || s.PerPlayer != DefaultPerPlayer {
		t.Errorf("坏值回到默认：%+v", s)
	}
}

// 识别测试：按（还没保存的）设置识别一行，说明会怎么处理。
func TestRecognitionTest(t *testing.T) {
	f := newFixture(t)
	f.set(Patch{Allowed: &[]string{"Steve"}})
	r, err := f.gc.Test(instName, "[20:15:31] [Server thread/INFO] [minecraft/MinecraftServer]: [Not Secure] <Steve> @AI /问 /私 怎么做附魔台", nil)
	if err != nil || !r.Trigger() || r.Player != "Steve" || r.Message != "@AI /问 /私 怎么做附魔台" || !r.Online || !r.Authorized ||
		!strings.Contains(r.Result, "只发给 Steve") || !strings.Contains(r.Result, "用了 /问") {
		t.Errorf("识别测试：%+v %v", r, err)
	}
	r, _ = f.gc.Test(instName, "[20:15:31] [Server thread/INFO]: <Notch> @小助手 你好", &Patch{Aliases: &[]string{"@小助手"}})
	if !r.Trigger() || r.Online || !strings.Contains(r.Result, "不在线") {
		t.Errorf("用还没保存的别名：%+v", r)
	}
	r, _ = f.gc.Test(instName, "[20:15:31] [Server thread/INFO]: <Steve> @AIbot 你好", nil)
	if r.Trigger() || !strings.Contains(r.Result, "不触发") {
		t.Errorf("不触发：%+v", r)
	}
	if _, err := f.gc.Test(instName, "x", &Patch{Pattern: ptr("(")}); err == nil {
		t.Error("正则写错了应报错")
	}
}

// 「游戏内 AI」标签里可以授权的玩家：在线的、白名单里的、管理员、最近出现过的、已经授权的，同名只列一次。
func TestViewPlayers(t *testing.T) {
	f := newFixture(t)
	f.b.mu.Lock()
	f.b.wl, f.b.ops, f.b.wlOn = []string{"steve", "Bob"}, []string{"Admin"}, false
	f.b.mu.Unlock()
	f.gc.OnLine(instName, "[20:10:00] [Server thread/INFO] [minecraft/MinecraftServer]: Carol joined the game")
	v := f.set(Patch{Allowed: &[]string{"Dave"}})
	var names []string
	for _, p := range v.Players {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"Alex", "Steve", "Admin", "Bob", "Carol", "Dave"}) {
		t.Errorf("玩家：%v", names)
	}
	if v.WhitelistOn || !v.Players[1].Whitelist || !v.Players[1].Online || !v.Players[5].Allowed || !v.Players[4].Seen || !v.Players[2].Op {
		t.Errorf("标记：%+v", v.Players)
	}
	if !v.AI.Enabled || v.Defaults.Cooldown != 8 || v.Defaults.PerPlayer != 60 || len(v.Templates) != 10 {
		t.Errorf("视图：%+v", v)
	}
}
