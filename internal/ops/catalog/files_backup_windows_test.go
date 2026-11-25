package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"minedeck/internal/inst"
)

// 配置备份要用的名字上已经有一个只读文件：Windows 上照旧报错、不覆盖（和以前的 os.WriteFile 一样）；
// 报错里带上备份在服务器文件夹里的相对路径，但不带本机的完整路径。
func TestConfigBackupReadOnlyRefused(t *testing.T) {
	base := t.TempDir()
	i := &inst.Instance{Dir: filepath.Join(base, "server")}
	fixed := time.Date(2026, 11, 25, 10, 0, 0, 456_000_000, time.Local)
	old := configBackupNow
	configBackupNow = func() time.Time { return fixed }
	t.Cleanup(func() { configBackupNow = old })

	dir := backupDir(i)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "server.properties~" + fixed.Format("20060102-150405.000")
	bk := filepath.Join(dir, name)
	const oldBak = "old backup\n"
	if err := os.WriteFile(bk, []byte(oldBak), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bk, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bk, 0o644) })

	_, err := backupConfig(i, "server.properties", []byte("motd=hello\n"))
	if err == nil || !strings.Contains(err.Error(), ".minedeck/config-backups/"+name) {
		t.Fatalf("只读的备份应报错并说出是哪个文件：%v", err)
	}
	if strings.Contains(err.Error(), base) {
		t.Fatalf("报错里不该有本机的完整路径：%v", err)
	}
	if b, _ := os.ReadFile(bk); string(b) != oldBak {
		t.Fatalf("只读的备份被换掉了：%q", b)
	}
	if st, err := os.Stat(bk); err != nil || st.Mode().Perm()&0o200 != 0 {
		t.Fatalf("备份应还是只读的：%v %v", st.Mode(), err)
	}
}
