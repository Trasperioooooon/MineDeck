package app

import (
	"os"
	"strings"
	"testing"
)

// 两种保留份数：手动的（BackupKeep）缺省 10，定时的（ScheduledBackupKeep）缺省 24；都限在 1~1000。
// 定时的没设过时取 24 和 BackupKeep 里大的：以前定时备份和别的备份合起来按 BackupKeep 留，调到 24 以上的人升级以后不该少留。
func TestScheduledBackupKeep(t *testing.T) {
	for _, tc := range []struct {
		name         string
		keep, sched  int
		wantK, wantS int
	}{
		{"都没设过", 0, 0, DefaultBackupKeep, DefaultScheduledBackupKeep},
		{"手动的调小了，定时的照旧 24", 5, 0, 5, 24},
		{"手动的调到 24 以上、定时的没设过：跟着手动的", 30, 0, 30, 30},
		{"手动的超出上限", 5000, 0, 1000, 1000},
		{"定时的设过就按设的", 30, 7, 30, 7},
		{"定时的超出上限", 0, 5000, DefaultBackupKeep, 1000},
	} {
		c := Config{BackupKeep: tc.keep, ScheduledBackupKeep: tc.sched}
		if got := backupKeepOf(c); got != tc.wantK {
			t.Errorf("%s：BackupKeep = %d，应为 %d", tc.name, got, tc.wantK)
		}
		if got := ScheduledBackupKeepOf(c); got != tc.wantS {
			t.Errorf("%s：ScheduledBackupKeep = %d，应为 %d", tc.name, got, tc.wantS)
		}
	}
	for in, want := range map[int]int{-3: 1, 0: 1, 1: 1, 24: 24, 1000: 1000, 1001: 1000} {
		if got := ClampBackupKeep(in); got != want {
			t.Errorf("ClampBackupKeep(%d) = %d，应为 %d", in, got, want)
		}
	}
}

// 定时备份的份数存进 config.json、重启以后还在；没设过时不回填（生效的值每次按 ScheduledBackupKeepOf 算）。
func TestScheduledBackupKeepPersisted(t *testing.T) {
	tempBase(t)
	if _, err := UpdateConfig(func(c *Config) error { c.BackupKeep = 5; return nil }); err != nil {
		t.Fatal(err)
	}
	restart()
	if b, err := os.ReadFile(configPath()); err != nil || strings.Contains(string(b), "scheduledBackupKeep") {
		t.Fatalf("没设过定时备份的份数，config.json 里不该有这一项：%s %v", b, err)
	}
	if got := ScheduledBackupKeep(); got != DefaultScheduledBackupKeep {
		t.Fatalf("没设过时应为 %d：%d", DefaultScheduledBackupKeep, got)
	}
	if _, err := UpdateConfig(func(c *Config) error { c.ScheduledBackupKeep = 48; return nil }); err != nil {
		t.Fatal(err)
	}
	restart()
	if c := GetConfig(); c.ScheduledBackupKeep != 48 || ScheduledBackupKeep() != 48 || BackupKeep() != 5 {
		t.Fatalf("重启以后两项都该还在：%+v", c)
	}
	if b, _ := os.ReadFile(configPath()); !strings.Contains(string(b), `"scheduledBackupKeep": 48`) {
		t.Fatalf("config.json 里应有 scheduledBackupKeep：%s", b)
	}
}
