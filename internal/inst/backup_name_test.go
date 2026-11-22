package inst

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/tmpsweep"
)

// 备份文件的名字靠时间区分。时钟没往前走（Windows 的时钟一跳可以有十几毫秒，极小的世界一次备份比这还快）时接连备份两次、
// 标签也一样：后一份不能把前一份盖掉，要等时钟走了取别的名字；时钟一直不走就报错，已有的那份原样留着。
func TestBackupNameDoesNotOverwriteExisting(t *testing.T) {
	m, dir := tempInstance(t)
	fixed := time.Date(2026, 11, 18, 12, 0, 0, 123456789, time.Local)
	var calls atomic.Int32
	stuckFor := int32(1 << 30) // 时钟卡多少次以后往前走
	oldNow, oldTries := backupNow, backupNameTries
	t.Cleanup(func() { backupNow, backupNameTries = oldNow, oldTries })
	backupNow = func() time.Time {
		if calls.Add(1) > stuckFor {
			return fixed.Add(time.Second)
		}
		return fixed
	}
	backupNameTries = 20

	first, err := m.CreateBackup("s", "same")
	if err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(backupDirOf("s"), first)
	before, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}

	// 世界变了，时钟还卡在同一格：试完就报错，不盖第一份
	writeLevel(t, dir, "changed")
	if _, err := m.CreateBackup("s", "same"); err == nil || !strings.Contains(err.Error(), "撞上了") {
		t.Fatalf("名字撞了又等不到时钟走，应报错：%v", err)
	}
	if after, _ := os.ReadFile(firstPath); string(after) != string(before) {
		t.Fatal("已有的那份备份被盖掉了")
	}
	if files := backupFiles(t, m); len(files) != 1 {
		t.Fatalf("应当还是只有第一份：%v", files)
	}
	if ents, _ := os.ReadDir(backupDirOf("s")); len(ents) != 1 {
		t.Fatalf("没备份成不该留下别的文件：%v", ents)
	}

	// 时钟再卡几下就往前走：第二份取到别的名字，两份都在
	stuckFor = calls.Load() + 3
	second, err := m.CreateBackup("s", "same")
	if err != nil || second == first {
		t.Fatalf("时钟走了以后应取到别的名字：%q %v", second, err)
	}
	if after, _ := os.ReadFile(firstPath); string(after) != string(before) {
		t.Fatal("已有的那份备份被盖掉了")
	}
	if files := backupFiles(t, m); len(files) != 2 {
		t.Fatalf("两份都应在：%v", files)
	}
	// 两份各是各的内容：还原第一份回到 current，第二份是 changed（还原前会自动再备份一次，这里换回真的时钟）
	backupNow = time.Now
	for name, want := range map[string]string{first: "current", second: "changed"} {
		if err := m.RestoreBackup("s", name); err != nil {
			t.Fatal(err)
		}
		if got := readLevel(t, dir); got != want {
			t.Fatalf("%s 还原出来是 %q，应为 %q", name, got, want)
		}
	}
}

// A23.2：备份通过 atomicfile.PublishNew 只在目标不存在时发布（Windows 上是不覆盖的 MoveFileEx 一步发布，非 Windows 是 O_EXCL 占位再改名）。
// 无论是备份前目录里已经预先有一份同名文件，还是跨进程在压缩期间先一步写入了同名备份，都不能把已有的那份盖掉，而应换一个时间重取或试满报错。
func TestBackupExclDoesNotOverwritePreexistingOrConcurrent(t *testing.T) {
	m, dir := tempInstance(t)
	bdir := backupDirOf("s")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		t.Fatal(err)
	}

	t0 := time.Date(2026, 11, 22, 15, 0, 0, 100000000, time.Local)
	t1 := t0.Add(time.Millisecond)
	t2 := t0.Add(2 * time.Millisecond)

	oldNow, oldTries, oldHook := backupNow, backupNameTries, backupHook
	t.Cleanup(func() {
		backupNow, backupNameTries, backupHook = oldNow, oldTries, oldHook
	})
	backupNameTries = 10

	// 场景 1：目录里预先放好一份同名文件（t0），时钟第一次给 t0、第二次给 t1 -> 预先那份原封不动，新备份落在 t1。
	preName := t0.Format("20060102-150405.000000000") + "-auto.zip"
	prePath := filepath.Join(bdir, preName)
	const preBytes = "preexisting-backup-from-other-tool"
	if err := os.WriteFile(prePath, []byte(preBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	var seq atomic.Int32
	backupNow = func() time.Time {
		if seq.Add(1) == 1 {
			return t0
		}
		return t1
	}
	gotName, err := m.CreateBackup("s", "auto")
	if err != nil {
		t.Fatalf("避开预先存在的同名文件后应成功：%v", err)
	}
	if gotName == preName {
		t.Fatalf("新备份不应继续使用已被占住的名字 %s", gotName)
	}
	if got, _ := os.ReadFile(prePath); string(got) != preBytes {
		t.Fatalf("预先存在的同名文件被盖掉了：%q", got)
	}

	// 场景 2：跨进程同时备份同一台服——本进程开始备份时 t2 还没人占，压缩途中（backupHook）另一进程先一步建好了 t2 的同名文件；
	// 本进程发布时（Windows 不覆盖的 MoveFileEx，非 Windows O_EXCL 占位再改名）必须发现名字已被占住，换下一个时间 t3 存，绝不覆盖另一进程刚写好的 t2。
	raceName := t2.Format("20060102-150405.000000000") + "-auto.zip"
	racePath := filepath.Join(bdir, raceName)
	const otherProcBytes = "written-by-another-process-during-zip"
	t3 := t2.Add(time.Millisecond)
	var raceCalls atomic.Int32
	hookFired := false
	backupNow = func() time.Time {
		// 另一进程写完之前时钟一直是 t2；本进程发布撞上同名重取时时钟走到 t3
		if !hookFired || raceCalls.Add(1) == 1 {
			return t2
		}
		return t3
	}
	backupHook = func() {
		hookFired = true
		if err := os.WriteFile(racePath, []byte(otherProcBytes), 0o644); err != nil {
			t.Errorf("模拟另一进程写入同名备份失败：%v", err)
		}
	}
	writeLevel(t, dir, "world-v2")
	gotRace, err := m.CreateBackup("s", "auto")
	backupHook = nil
	if err != nil {
		t.Fatalf("跨进程撞名后换时间重取应成功：%v", err)
	}
	if gotRace == raceName {
		t.Fatalf("跨进程已先建好 %s，本进程不应仍用同名覆盖", raceName)
	}
	if got, _ := os.ReadFile(racePath); string(got) != otherProcBytes {
		t.Fatalf("另一进程先建好的同名备份被覆盖了：got=%q want=%q", got, otherProcBytes)
	}
}

// R1：备份发布前备份目录里不应先出现最终名字的 0 字节占位 .zip（否则杀毒或资源管理器打开占位会让 Windows 改名拒绝访问），
// 且没撞名时备份文件名里的时间应取压缩开始前那一次，而不是压缩结束后的时间。
func TestBackupPublishNoPlaceholderAndUsesStartTime(t *testing.T) {
	m, _ := tempInstance(t)
	bdir := backupDirOf("s")

	oldNow, oldBackupHook, oldAtomicHook := backupNow, backupHook, atomicfile.Hook
	t.Cleanup(func() {
		backupNow, backupHook, atomicfile.Hook = oldNow, oldBackupHook, oldAtomicHook
	})

	start := time.Date(2026, 11, 22, 20, 0, 0, 0, time.Local)
	var tick atomic.Int32
	backupNow = func() time.Time {
		n := tick.Add(1)
		return start.Add(time.Duration(n-1) * 10 * time.Second)
	}

	var callsBeforeZip int32
	backupHook = func() {
		callsBeforeZip = tick.Load()
		// 模拟压缩耗时推进了假时钟：如果发布时才第一次取时间，文件名就会跳到后面的秒数
		_ = backupNow()
	}

	hookSawTarget := false
	atomicfile.Hook = func(p string) {
		if filepath.Dir(p) != bdir {
			return
		}
		hookSawTarget = true
		if st, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("发布改名前目标路径 %s 已存在（size=%d err=%v），不应预先建占位文件", filepath.Base(p), func() int64 {
				if st != nil {
					return st.Size()
				}
				return -1
			}(), err)
		}
	}

	gotName, err := m.CreateBackup("s", "check")
	backupHook = nil
	atomicfile.Hook = oldAtomicHook
	if err != nil {
		t.Fatalf("CreateBackup 失败：%v", err)
	}
	if !hookSawTarget {
		t.Fatal("发布备份时未经过 atomicfile.Hook")
	}
	if callsBeforeZip != 1 {
		t.Fatalf("压缩开始前应先取一次时间，实际在 backupHook 时已调用 %d 次", callsBeforeZip)
	}
	wantName := start.Format("20060102-150405.000000000") + "-check.zip"
	if gotName != wantName {
		t.Fatalf("备份文件名应使用压缩开始前的时间：got=%q want=%q", gotName, wantName)
	}
}

// A18.1：新建备份与 zipDirs 打包时的临时文件统一用 .minedeck-backup-<数字>.tmp，与 tmpsweep.IsOurs 对齐。
func TestBackupTempFileNameMatchesTmpsweep(t *testing.T) {
	m, dir := tempInstance(t)
	bdir := backupDirOf("s")

	oldHook := atomicfile.Hook
	t.Cleanup(func() { atomicfile.Hook = oldHook })

	var sawCreateTemp string
	atomicfile.Hook = func(p string) {
		if filepath.Dir(p) != bdir {
			return
		}
		ents, err := os.ReadDir(bdir)
		if err != nil {
			t.Errorf("读取备份目录失败：%v", err)
			return
		}
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".tmp") {
				sawCreateTemp = e.Name()
			}
		}
	}

	if _, err := m.CreateBackup("s", "tmpcheck"); err != nil {
		t.Fatalf("CreateBackup 失败：%v", err)
	}
	if sawCreateTemp == "" {
		t.Fatal("发布前未在备份目录里观察到 .tmp 临时文件")
	}
	if !strings.HasPrefix(sawCreateTemp, ".minedeck-backup-") || !tmpsweep.IsOurs(sawCreateTemp) {
		t.Fatalf("CreateBackup 临时文件名 = %q，应以 .minedeck-backup- 开头且被 tmpsweep.IsOurs 识别", sawCreateTemp)
	}

	otherOut := t.TempDir()
	var sawZipDirsTemp string
	atomicfile.Hook = func(p string) {
		if filepath.Dir(p) != otherOut {
			return
		}
		ents, _ := os.ReadDir(otherOut)
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), ".tmp") {
				sawZipDirsTemp = e.Name()
			}
		}
	}
	if err := zipDirs(dir, []string{"world"}, filepath.Join(otherOut, "manual.zip")); err != nil {
		t.Fatalf("zipDirs 失败：%v", err)
	}
	atomicfile.Hook = oldHook
	if !strings.HasPrefix(sawZipDirsTemp, ".minedeck-backup-") || !tmpsweep.IsOurs(sawZipDirsTemp) {
		t.Fatalf("zipDirs 临时文件名 = %q，应以 .minedeck-backup- 开头且被 tmpsweep.IsOurs 识别", sawZipDirsTemp)
	}
}
