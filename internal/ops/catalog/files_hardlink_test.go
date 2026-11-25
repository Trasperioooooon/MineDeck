package catalog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"minedeck/internal/atomicfile"
	"minedeck/internal/inst"
)

// 配置备份要用的名字上已经有一个文件、而且是服务器文件夹外某个文件的硬链接时（A11）：写备份不能改到外面那份，
// 服务器里的备份换成独立的新文件。
func TestConfigBackupDoesNotWriteThroughHardlink(t *testing.T) {
	base := t.TempDir()
	i := &inst.Instance{Dir: filepath.Join(base, "server")}
	fixed := time.Date(2026, 11, 25, 10, 0, 0, 123_000_000, time.Local)
	old := configBackupNow
	configBackupNow = func() time.Time { return fixed }
	t.Cleanup(func() { configBackupNow = old })

	dir := backupDir(i)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.txt")
	const keep = "outside, do not touch\n"
	if err := os.WriteFile(outside, []byte(keep), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "server.properties~" + fixed.Format("20060102-150405.000")
	if err := os.Link(outside, filepath.Join(dir, name)); err != nil {
		t.Fatalf("建硬链接失败（要在允许建硬链接的盘上跑）：%v", err)
	}

	const content = "motd=hello\n"
	rel, err := backupConfig(i, "server.properties", []byte(content))
	if err != nil {
		t.Fatalf("备份：%v", err)
	}
	if want := ".minedeck/config-backups/" + name; rel != want {
		t.Fatalf("备份的位置：%q，应为 %q", rel, want)
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != keep {
		t.Fatalf("服务器文件夹外的文件被改了：%q %v", b, err)
	}
	bk := filepath.Join(dir, name)
	if b, err := os.ReadFile(bk); err != nil || string(b) != content {
		t.Fatalf("备份内容不对：%q %v", b, err)
	}
	f, err := os.Open(bk)
	if err != nil {
		t.Fatal(err)
	}
	n, err := atomicfile.HardlinkCount(f)
	f.Close()
	if err != nil || n != 1 {
		t.Fatalf("服务器里的备份应是独立的文件（硬链接数 1）：n=%d err=%v", n, err)
	}
}
