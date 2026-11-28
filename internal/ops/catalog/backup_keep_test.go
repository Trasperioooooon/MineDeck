package catalog

import (
	"context"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/ops"
)

// 定时备份单独的保留份数（2026-11-29）：app.config.set 收 scheduledBackupKeep，超出 1~1000 按边界算；
// app.info 顶层给生效的份数（没设过时 config 里没有这一项）。
func TestScheduledBackupKeepConfig(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	local := ops.Caller{Source: ops.SrcUI, Local: true}
	set := func(body string) {
		t.Helper()
		if _, err := reg.Do(ctx, local, "app.config.set", ops.Args(body)); err != nil {
			t.Fatalf("%s：%v", body, err)
		}
	}
	effective := func() int {
		t.Helper()
		v, err := reg.Do(ctx, local, "app.info", nil)
		if err != nil {
			t.Fatal(err)
		}
		return v.(map[string]any)["scheduledBackupKeep"].(int)
	}
	if got := effective(); got != app.DefaultScheduledBackupKeep || app.GetConfig().ScheduledBackupKeep != 0 {
		t.Fatalf("没设过时生效的应为 %d、配置里不该有：%d / %d", app.DefaultScheduledBackupKeep, got, app.GetConfig().ScheduledBackupKeep)
	}
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"scheduledBackupKeep":48}`, 48},
		{`{"scheduledBackupKeep":0}`, 1},
		{`{"scheduledBackupKeep":-5}`, 1},
		{`{"scheduledBackupKeep":5000}`, 1000},
	} {
		set(tc.body)
		if c := app.GetConfig(); c.ScheduledBackupKeep != tc.want || effective() != tc.want {
			t.Errorf("%s 以后应为 %d：配置 %d，生效 %d", tc.body, tc.want, c.ScheduledBackupKeep, effective())
		}
	}
	// 同一次请求里两项都带：各按各的
	set(`{"backupKeep":3,"scheduledBackupKeep":30}`)
	if c := app.GetConfig(); c.BackupKeep != 3 || c.ScheduledBackupKeep != 30 {
		t.Fatalf("两项一起改：%d / %d", c.BackupKeep, c.ScheduledBackupKeep)
	}
	if _, err := reg.Do(ctx, local, "app.config.set", ops.Args(`{"scheduledBackupKeep":"24"}`)); err == nil {
		t.Error("scheduledBackupKeep 不是整数应整次拒绝")
	}
}

// 定时备份的份数没设过时跟着 backupKeep（取 24 和它里大的，见 app.ScheduledBackupKeepOf）。改 backupKeep 时先把定时的
// 记下来：把手动的份数调小，定时备份不跟着少留；以后两项互不牵连。
func TestBackupKeepChangeFreezesScheduledKeep(t *testing.T) {
	reg, _ := newCatalog(t)
	ctx := context.Background()
	local := ops.Caller{Source: ops.SrcUI, Local: true}
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.BackupKeep = 30; return nil }); err != nil { // 升级以前调到 30 的人
		t.Fatal(err)
	}
	if got := app.ScheduledBackupKeep(); got != 30 {
		t.Fatalf("backupKeep 是 30、定时的没设过：定时备份应也留 30 份：%d", got)
	}
	for _, body := range []string{`{"backupKeep":5}`, `{"backupKeep":50}`} {
		if _, err := reg.Do(ctx, local, "app.config.set", ops.Args(body)); err != nil {
			t.Fatal(err)
		}
		if c := app.GetConfig(); c.ScheduledBackupKeep != 30 || app.ScheduledBackupKeep() != 30 {
			t.Fatalf("%s 以后定时备份的份数应记成原来的 30、不跟着变：配置 %d，生效 %d", body, c.ScheduledBackupKeep, app.ScheduledBackupKeep())
		}
	}
	// 默认的 10 改成别的：定时的记成 24（同没设过时）
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.BackupKeep, c.ScheduledBackupKeep = 0, 0; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Do(ctx, local, "app.config.set", ops.Args(`{"backupKeep":7}`)); err != nil {
		t.Fatal(err)
	}
	if c := app.GetConfig(); c.BackupKeep != 7 || c.ScheduledBackupKeep != app.DefaultScheduledBackupKeep {
		t.Fatalf("从默认改 backupKeep：%d / %d", c.BackupKeep, c.ScheduledBackupKeep)
	}
}

// 「定时」留给定时任务做的备份：界面、AI 助手、MCP 走的 instance.backup.create 用它当标签时整次拒绝，说清原因（400）。
func TestBackupCreateRejectsScheduledLabel(t *testing.T) {
	reg, _ := newCatalog(t)
	_, err := reg.Do(context.Background(), ops.Caller{Source: ops.SrcUI, Local: true}, "instance.backup.create", ops.Args(`{"inst":"生存服","label":"定时"}`))
	if err == nil || !strings.Contains(err.Error(), "留给定时任务") || ops.CodeOf(err) != 400 {
		t.Fatalf("手动备份用「定时」当标签应被拒：%v（%d）", err, ops.CodeOf(err))
	}
}
