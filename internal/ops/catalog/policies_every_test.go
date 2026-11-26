package catalog

import (
	"context"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// 定时任务加了每隔多久（every）和文件夹（group）以后（2026-11-27）：AI、MCP 经 instance.policies.set 能加、能挪文件夹、能删，
// 预览里写出间隔和文件夹；老的 {type, at, args} 照旧能用；不合格的直接 400，连提案都不生成。
func TestPoliciesEveryAndGroup(t *testing.T) {
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
	get := func() inst.Policies {
		t.Helper()
		p, err := reg.Op("instance.policies.get").Run(ctx, ui, ops.Args(`{"inst":"生存服"}`))
		if err != nil {
			t.Fatal(err)
		}
		return p.(inst.Policies)
	}
	// 给 AI 看的参数说明里有这两个字段
	item := reg.Op("instance.policies.set").Params.Properties["addSchedules"].Items
	if item.Properties["every"] == nil || item.Properties["every"].Type != "integer" || item.Properties["group"] == nil {
		t.Fatalf("addSchedules 的每一项应说明 every、group：%+v", item.Properties)
	}

	do(ui, `{"inst":"生存服","addSchedules":[{"type":"restart","at":"4:00"}]}`) // 老写法照旧
	do(ui, `{"inst":"生存服","addSchedules":[{"type":"backup","every":60,"group":"存档"},{"type":"command","every":60,"args":"kill @e[type=item]","group":" 扫地 "}]}`)
	hourly := inst.Schedule{Type: "backup", Every: 60, Group: "存档"}
	sweep := inst.Schedule{Type: "command", Every: 60, Args: "kill @e[type=item]", Group: "扫地"}
	if got := get(); !slices.Equal(got.Schedules, []inst.Schedule{{Type: "restart", At: "04:00"}, hourly, sweep}) {
		t.Fatalf("存进去的：%+v", got.Schedules)
	}

	// AI 把每小时存档挪进「备份」、删掉定时重启：预览列出改前改后，确认以后就是这个样子
	p := do(asst, `{"inst":"生存服","removeSchedules":[{"type":"restart","at":"04:00"},{"type":"backup","every":60,"group":"存档"}],"addSchedules":[{"type":"backup","every":60,"group":"备份"}]}`).(*ops.Proposal)
	want := []ops.Change{{What: "定时任务",
		Before: "04:00 restart；「存档」每隔 60 分钟 backup；「扫地」每隔 60 分钟 command：kill @e[type=item]",
		After:  "「扫地」每隔 60 分钟 command：kill @e[type=item]；「备份」每隔 60 分钟 backup"}}
	if !slices.Equal(p.Plan.Changes, want) {
		t.Fatalf("预览：%+v", p.Plan.Changes)
	}
	if _, err := reg.Confirm(ctx, ui, p.ID, false); err != nil {
		t.Fatal(err)
	}
	moved := hourly
	moved.Group = "备份"
	if got := get(); !slices.Equal(got.Schedules, []inst.Schedule{sweep, moved}) {
		t.Fatalf("确认以后：%+v", got.Schedules)
	}

	// 不带文件夹删不掉文件夹里的（找不到不算错，什么都不改）
	do(ui, `{"inst":"生存服","removeSchedules":[{"type":"command","every":60,"args":"kill @e[type=item]"}]}`)
	if got := get(); len(got.Schedules) != 2 {
		t.Fatalf("不带文件夹的删除项不该删掉文件夹里的：%+v", got.Schedules)
	}

	// 不合格的：直接 400，说清楚为什么
	for args, msg := range map[string]string{
		`{"inst":"生存服","addSchedules":[{"type":"backup","every":10}]}`:               "定时备份最短每隔 30 分钟一次",
		`{"inst":"生存服","addSchedules":[{"type":"backup","at":"04:00","every":60}]}`:  "只能填一样",
		`{"inst":"生存服","addSchedules":[{"type":"command","every":"60","args":"x"}]}`: "整数",
	} {
		for _, c := range []ops.Caller{ui, asst} {
			_, err := reg.Do(ctx, c, "instance.policies.set", ops.Args(args))
			if ops.CodeOf(err) != 400 || err == nil || !strings.Contains(err.Error(), msg) {
				t.Errorf("%s（%s）应 400「%s」：%v", args, c.Source, msg, err)
			}
		}
	}
	if got := get(); len(got.Schedules) != 2 {
		t.Fatalf("不合格的改动不该存进去：%+v", got.Schedules)
	}
}
