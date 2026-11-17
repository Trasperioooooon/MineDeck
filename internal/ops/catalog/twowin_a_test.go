package catalog

import (
	"context"
	"slices"
	"testing"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// 定时任务改成按条增删以后（2026-11-17），AI 提案的预览要用和真正保存同一个合并算「改成什么样」：
// 窗口 A 先加了一条，AI 拿着加之前的样子提「删 Y、加 Z、打开崩溃重启」，预览里列的改后就是确认以后存进去的，
// A 加的那条两边都在。只拨崩溃重启时不列定时任务；不合格的改动连提案都不生成。
func TestPoliciesPreviewUsesMerge(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	asst := ops.Caller{Source: ops.SrcAssistant, Actor: "本机", Session: "s1", Local: true}
	do := func(c ops.Caller, args string) any {
		t.Helper()
		res, err := reg.Do(ctx, c, "instance.policies.set", ops.Args(args))
		if err != nil {
			t.Fatalf("%s：%v", args, err)
		}
		return res
	}
	do(ui, `{"inst":"生存服","schedules":[{"type":"restart","at":"04:00"}]}`)
	do(ui, `{"inst":"生存服","addSchedules":[{"type":"backup","at":"3:00"}]}`) // 窗口 A
	p := do(asst, `{"inst":"生存服","crashRestart":true,"removeSchedules":[{"type":"restart","at":"04:00"}],"addSchedules":[{"type":"command","at":"05:00","args":"say hi"}]}`).(*ops.Proposal)
	want := []ops.Change{
		{What: "崩溃自动重启", Before: "关", After: "开"},
		{What: "定时任务", Before: "04:00 restart；03:00 backup", After: "03:00 backup；05:00 command：say hi"},
	}
	if !slices.Equal(p.Plan.Changes, want) {
		t.Fatalf("预览应按合并以后的样子列：%+v", p.Plan.Changes)
	}
	if _, err := reg.Confirm(ctx, ui, p.ID, false); err != nil {
		t.Fatal(err)
	}
	i, _ := reg.Op("instance.policies.get").Run(ctx, ui, ops.Args(`{"inst":"生存服"}`))
	got := i.(inst.Policies)
	if !got.CrashRestart || !slices.Equal(got.Schedules, []inst.Schedule{{Type: "backup", At: "03:00"}, {Type: "command", At: "05:00", Args: "say hi"}}) {
		t.Errorf("确认以后存进去的：%+v", got)
	}
	// 只拨崩溃重启：预览里只有这一项，确认以后任务表一条不少（以前不给 schedules 等于清空）
	p = do(asst, `{"inst":"生存服","crashRestart":false}`).(*ops.Proposal)
	if len(p.Plan.Changes) != 1 || p.Plan.Changes[0].What != "崩溃自动重启" {
		t.Errorf("只拨崩溃重启：%+v", p.Plan.Changes)
	}
	if _, err := reg.Confirm(ctx, ui, p.ID, false); err != nil {
		t.Fatal(err)
	}
	i, _ = reg.Op("instance.policies.get").Run(ctx, ui, ops.Args(`{"inst":"生存服"}`))
	if got := i.(inst.Policies); got.CrashRestart || len(got.Schedules) != 2 {
		t.Errorf("只拨崩溃重启以后：%+v", got)
	}
	// 加的任务不合格：提案就不生成
	if _, err := reg.Do(ctx, asst, "instance.policies.set", ops.Args(`{"inst":"生存服","addSchedules":[{"type":"dance","at":"04:00"}]}`)); ops.CodeOf(err) != 400 {
		t.Errorf("不合格的改动应直接 400：%v", err)
	}
}
