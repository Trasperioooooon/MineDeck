package inst

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
