package inst

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"minedeck/internal/app"
)

// 定时备份和别的备份分开计数（2026-11-29 作者定）：定时任务做的只和定时备份比、按 ScheduledBackupKeep 留，
// 手动备份、「还原前自动」只和它们自己比、按 BackupKeep 留。以前合起来按 BackupKeep 留，每小时一份定时备份只够留十个小时，
// 手动备份也被挤掉。

// newest 把按做的先后排的文件名倒过来（列表是新的在前）。
func newest(files ...string) []string {
	out := slices.Clone(files)
	slices.Reverse(out)
	return out
}

// 定时备份做得再多，也挤不掉手动备份：定时的只留最新的 ScheduledBackupKeep 份，手动的一份不少。
func TestScheduledBackupsDoNotPushOutOthers(t *testing.T) {
	m, dir := tempInstance(t)
	setConfig(t, func(c *app.Config) { c.BackupKeep, c.ScheduledBackupKeep = 2, 3 })
	bdir := filepath.Join(app.BackupsDir, "s")
	others := []string{"20200101-000000.000000000-更新模组前.zip", "20200102-000000.000000000-还原前自动.zip"}
	for _, f := range others {
		zipWorld(t, dir, f, filepath.Join(bdir, f))
	}
	var sched []string
	for k := 0; k < 5; k++ {
		f, err := m.createScheduledBackup("s")
		if err != nil {
			t.Fatal(err)
		}
		if !isScheduledBackup(f) {
			t.Fatalf("定时任务做的备份应认作定时备份：%s", f)
		}
		sched = append(sched, f)
	}
	want := append(newest(sched[2:]...), newest(others...)...)
	if got := backupFiles(t, m); !slices.Equal(got, want) {
		t.Fatalf("定时备份应只留最新 3 份、手动的 2 份都在：\n得到 %v\n应为 %v", got, want)
	}
}

// 手动备份做得再多，也挤不掉定时备份；老版本做的「定时」备份（文件名带不带纳秒都有）照旧认作定时备份。
func TestOtherBackupsDoNotPushOutScheduled(t *testing.T) {
	m, dir := tempInstance(t)
	setConfig(t, func(c *app.Config) { c.BackupKeep, c.ScheduledBackupKeep = 2, 3 })
	bdir := filepath.Join(app.BackupsDir, "s")
	legacy := []string{"20200101-040000-定时.zip", "20200102-040000.000000000-定时.zip", "20200103-040000.123456789-定时.zip"}
	for _, f := range legacy {
		zipWorld(t, dir, f, filepath.Join(bdir, f))
	}
	var manual []string
	for _, label := range []string{"a", "b", "", "d"} {
		f, err := m.CreateBackup("s", label)
		if err != nil {
			t.Fatal(err)
		}
		if isScheduledBackup(f) {
			t.Fatalf("手动备份不该认作定时备份：%s", f)
		}
		manual = append(manual, f)
	}
	want := append(newest(manual[2:]...), newest(legacy...)...)
	if got := backupFiles(t, m); !slices.Equal(got, want) {
		t.Fatalf("手动备份应只留最新 2 份、老的定时备份 3 份都在：\n得到 %v\n应为 %v", got, want)
	}
}

// 改小了哪一类的份数，下一次备份时就清那一类，不管这次做的是哪种备份（同以前「改了下次备份时生效」）。
func TestLoweredKeepAppliesOnAnyBackup(t *testing.T) {
	m, dir := tempInstance(t)
	bdir := filepath.Join(app.BackupsDir, "s")
	sched := []string{"20200101-040000-定时.zip", "20200102-040000-定时.zip", "20200103-040000-定时.zip"}
	for _, f := range sched {
		zipWorld(t, dir, f, filepath.Join(bdir, f))
	}
	setConfig(t, func(c *app.Config) { c.ScheduledBackupKeep = 1 })
	f, err := m.CreateBackup("s", "手动")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := backupFiles(t, m), []string{f, sched[2]}; !slices.Equal(got, want) {
		t.Fatalf("定时备份的份数调成 1 以后，下一次手动备份时应清到只剩最新一份定时备份：\n得到 %v\n应为 %v", got, want)
	}
}

// 手动备份不许用「定时」当标签（会被当成定时备份、按另一个份数清理），给出清楚的错误、不做备份；
// 只是像的标签照常能用，也不算定时备份。
func TestManualBackupCannotUseScheduledLabel(t *testing.T) {
	m, _ := tempInstance(t)
	if _, err := m.CreateBackup("s", scheduledLabel); !errors.Is(err, errScheduledLabel) {
		t.Fatalf("手动备份用「定时」当标签应被拒：%v", err)
	}
	if got := backupFiles(t, m); len(got) != 0 {
		t.Fatalf("被拒的手动备份不该留下文件：%v", got)
	}
	for _, label := range []string{"定时备份", " 定时", "定时 ", "每天定时"} {
		f, err := m.CreateBackup("s", label)
		if err != nil {
			t.Fatalf("标签 %q 应照常能用：%v", label, err)
		}
		if isScheduledBackup(f) {
			t.Errorf("标签 %q 做的手动备份不该认作定时备份：%s", label, f)
		}
	}
}

// 认定时备份只看文件名里拆出来的标签是不是恰好为「定时」；不是 MineDeck 起的名字不算。
func TestIsScheduledBackup(t *testing.T) {
	for file, want := range map[string]bool{
		"20261129-040000.000000000-定时.zip": true,
		"20261129-040000.5-定时.zip":         true,
		"20261129-040000-定时.zip":           true, // 早先不带纳秒的文件名
		"20261129-040000.000000000.zip":    false,
		"20261129-040000-定时备份.zip":         false,
		"20261129-040000-_定时.zip":          false,
		"20261129-040000-x-定时.zip":         false,
		"20261129-040000-定时.zip.zip":       false,
		"定时.zip":                           false,
		"我的存档-定时.zip":                      false,
	} {
		if got := isScheduledBackup(file); got != want {
			t.Errorf("isScheduledBackup(%q) = %v，应为 %v", file, got, want)
		}
	}
}

// 回归（同 TestRestoreOldestBackupAtKeep）：要还原的是定时备份里最旧的那份、定时备份又超了份数时，
// 还原前自动备份引起的清理也要放过它；手动、定时两类照旧各清各的。
func TestRestoreOldestScheduledBackupAtKeep(t *testing.T) {
	m, dir := tempInstance(t)
	setConfig(t, func(c *app.Config) { c.BackupKeep, c.ScheduledBackupKeep = 1, 2 })
	bdir := filepath.Join(app.BackupsDir, "s")
	const old1, old2, manual = "20200101-040000-定时.zip", "20200102-040000-定时.zip", "20200103-000000-手动.zip"
	zipWorld(t, dir, "old1", filepath.Join(bdir, old1))
	zipWorld(t, dir, "old2", filepath.Join(bdir, old2))
	zipWorld(t, dir, "manual", filepath.Join(bdir, manual))
	writeLevel(t, dir, "current")
	setConfig(t, func(c *app.Config) { c.ScheduledBackupKeep = 1 }) // 定时备份现在超了一份，最旧的 old1 本该清掉
	if err := m.RestoreBackup("s", old1); err != nil {
		t.Fatal(err)
	}
	if got := readLevel(t, dir); got != "old1" {
		t.Fatalf("level.dat = %q，应还原为 old1", got)
	}
	got := backupFiles(t, m)
	if len(got) != 3 || !slices.Contains(got, old1) || !slices.Contains(got, old2) || slices.Contains(got, manual) || isScheduledBackup(got[0]) {
		t.Fatalf("应剩还原前自动的那份、定时的 old2 和待还原的 old1（手动的只留最新一份，旧的那份清掉）：%v", got)
	}
	if _, err := os.Stat(filepath.Join(bdir, old1)); err != nil {
		t.Fatalf("待还原的源备份被滚动清理掉了: %v", err)
	}
}

// 定时任务到点做的备份带「定时」标签、认作定时备份（老的、新的同一个认法）。
func TestScheduleMakesScheduledBackup(t *testing.T) {
	m, _ := tempInstance(t)
	i, _ := m.Get("s")
	m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
	got := backupFiles(t, m)
	if len(got) != 1 || !isScheduledBackup(got[0]) {
		t.Fatalf("定时任务应做出一份定时备份：%v", got)
	}
	if _, label, _ := parseBackupName(got[0]); label != "定时" {
		t.Fatalf("列表、详情页显示的标签照旧是「定时」：%q", label)
	}
}
