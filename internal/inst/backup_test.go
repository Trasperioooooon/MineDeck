package inst

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"minedeck/internal/app"
	"minedeck/internal/diskguard"
)

// tempInstance 把程序目录与内置根换成临时的一份（测试结束后还原），备份目录恢复默认，
// 并准备一个带 world 存档的停止态实例 s，返回 Manager 与实例目录。
func tempInstance(t *testing.T) (*Manager, string) {
	t.Helper()
	oldBase, oldServers, oldBackups := app.Base, app.ServersDir, app.BackupsDir
	t.Cleanup(func() { app.Base, app.ServersDir, app.BackupsDir = oldBase, oldServers, oldBackups })
	app.Base = t.TempDir()
	app.ServersDir = filepath.Join(app.Base, "servers")
	app.BackupsDir = filepath.Join(app.Base, "backups")
	setConfig(t, func(c *app.Config) { c.BackupsDir, c.BackupKeep, c.ScheduledBackupKeep = "", 0, 0 })
	dir := filepath.Join(app.ServersDir, "s")
	writeLevel(t, dir, "current")
	m := &Manager{insts: map[string]*Instance{}, creating: map[string]bool{}}
	m.insts["s"] = &Instance{Settings: Settings{Name: "s"}, Dir: dir, Console: newConsole()}
	return m, dir
}

func setConfig(t *testing.T, mutate func(*app.Config)) {
	t.Helper()
	if _, err := app.UpdateConfig(func(c *app.Config) error { mutate(c); return nil }); err != nil {
		t.Fatal(err)
	}
}

func writeLevel(t *testing.T, dir, content string) {
	t.Helper()
	p := filepath.Join(dir, "world", "level.dat")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLevel(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "world", "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func backupFiles(t *testing.T, m *Manager) []string {
	t.Helper()
	list, err := m.ListBackups("s")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, b := range list {
		out = append(out, b.File)
	}
	return out
}

// zipWorld 把 world 存为 content 后直接打包成 dest：自定文件名（时间戳）便于控制新旧顺序。
func zipWorld(t *testing.T, dir, content, dest string) {
	t.Helper()
	writeLevel(t, dir, content)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := zipDirs(dir, []string{"world"}, dest); err != nil {
		t.Fatal(err)
	}
}

// 回归：份数已满时还原最旧的备份，还原前自动备份的滚动清理曾先把待还原的源备份删掉——
// 世界已清空、解压失败，那份备份也没了。
func TestRestoreOldestBackupAtKeep(t *testing.T) {
	m, dir := tempInstance(t)
	setConfig(t, func(c *app.Config) { c.BackupKeep = 2 })
	bdir := filepath.Join(app.BackupsDir, "s")
	zipWorld(t, dir, "old1", filepath.Join(bdir, "20200101-000000.zip"))
	zipWorld(t, dir, "old2", filepath.Join(bdir, "20200102-000000.zip"))
	writeLevel(t, dir, "current")
	if err := m.RestoreBackup("s", "20200101-000000.zip"); err != nil {
		t.Fatal(err)
	}
	if got := readLevel(t, dir); got != "old1" {
		t.Fatalf("level.dat = %q，应还原为 old1", got)
	}
	if _, err := os.Stat(filepath.Join(bdir, "20200101-000000.zip")); err != nil {
		t.Fatalf("待还原的源备份被滚动清理掉了: %v", err)
	}
}

// 回归：改备份目录或恢复默认后，旧根里的备份曾从列表消失、无法还原，也不再计入保留份数。
func TestBackupsAcrossBackupRoots(t *testing.T) {
	const old = "20200101-000000.zip"
	for _, tc := range []struct{ name, next string }{{"changeDir", "E"}, {"resetDefault", ""}} { // 改目录 / 恢复默认
		t.Run(tc.name, func(t *testing.T) {
			m, dir := tempInstance(t)
			d := filepath.Join(app.Base, "D")
			setConfig(t, func(c *app.Config) { c.BackupsDir, c.BackupKeep = d, 2 })
			zipWorld(t, dir, "old", filepath.Join(d, "s", old))
			next, root := "", app.BackupsDir
			if tc.next != "" {
				next = filepath.Join(app.Base, tc.next)
				root = next
			}
			setConfig(t, func(c *app.Config) { c.BackupsDir = next })

			if list := backupFiles(t, m); !slices.Contains(list, old) {
				t.Fatalf("旧根里的备份不在列表里：%v", list)
			}
			f, err := m.CreateBackup("s", "new")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "s", f)); err != nil {
				t.Fatalf("新备份应写在当前根 %s：%v", root, err)
			}
			// 份数已满（2 份）时还原旧根里最旧的那份：还原成功，源备份也没被还原前自动备份清理掉
			writeLevel(t, dir, "current")
			if err := m.RestoreBackup("s", old); err != nil {
				t.Fatal(err)
			}
			if got := readLevel(t, dir); got != "old" {
				t.Fatalf("level.dat = %q，应还原为 old", got)
			}
			if _, err := os.Stat(filepath.Join(d, "s", old)); err != nil {
				t.Fatalf("待还原的源备份被滚动清理掉了: %v", err)
			}
			// 保留份数跨根计：再备份一次后只剩最新 2 份，旧根里最旧的那份被清掉
			if _, err := m.CreateBackup("s", "more"); err != nil {
				t.Fatal(err)
			}
			if list := backupFiles(t, m); len(list) != 2 || slices.Contains(list, old) {
				t.Fatalf("应只剩最新 2 份且不含 %s：%v", old, list)
			}
			if _, err := os.Stat(filepath.Join(d, "s", old)); !os.IsNotExist(err) {
				t.Fatalf("旧根里最旧的备份应被滚动清理：%v", err)
			}
		})
	}
}

// 列表里每份备份带着它在哪个文件夹；改存放位置以前做的标 elsewhere（回访走查 11-16：备份页「备份放在」写新文件夹，
// 列出来的却是旧文件夹里的，点「打开」看到的是空的）。同名的两份只列当前根里那份，也就不算 elsewhere
func TestBackupListSaysWhichFolder(t *testing.T) {
	m, dir := tempInstance(t)
	d := filepath.Join(app.Base, "D")
	setConfig(t, func(c *app.Config) { c.BackupsDir = d })
	zipWorld(t, dir, "old", filepath.Join(d, "s", "20200101-000000.zip"))
	zipWorld(t, dir, "both", filepath.Join(d, "s", "20200102-000000.zip"))
	e := filepath.Join(app.Base, "E")
	setConfig(t, func(c *app.Config) { c.BackupsDir = e })
	zipWorld(t, dir, "both", filepath.Join(e, "s", "20200102-000000.zip"))
	f, err := m.CreateBackup("s", "")
	if err != nil {
		t.Fatal(err)
	}
	list, err := m.ListBackups("s")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]BackupInfo{}
	for _, b := range list {
		got[b.File] = b
	}
	cur, old := filepath.Join(e, "s"), filepath.Join(d, "s")
	for _, c := range []struct {
		file, dir string
		elsewhere bool
	}{{f, cur, false}, {"20200102-000000.zip", cur, false}, {"20200101-000000.zip", old, true}} {
		b, ok := got[c.file]
		if !ok || b.Dir != c.dir || b.Elsewhere != c.elsewhere {
			t.Errorf("%s：%+v（应在 %s，elsewhere=%v）", c.file, b, c.dir, c.elsewhere)
		}
	}
	if s, err := m.Get("s"); err != nil || s.BackupDir() != cur {
		t.Errorf("BackupDir 应和列表里当前根的文件夹一样（%s）：%v", cur, err)
	}
}

// 同名备份（比如手动拷到新根的）只列一份；删除时各根里的一并删掉，不会删完又从旧根冒出来。
func TestSameNameBackupInTwoRoots(t *testing.T) {
	m, dir := tempInstance(t)
	const f = "20200101-000000.zip"
	d := filepath.Join(app.Base, "D")
	zipWorld(t, dir, "x", filepath.Join(app.BackupsDir, "s", f))
	zipWorld(t, dir, "x", filepath.Join(d, "s", f))
	setConfig(t, func(c *app.Config) { c.BackupsDir = d })
	if list := backupFiles(t, m); !slices.Equal(list, []string{f}) {
		t.Fatalf("同名备份应只列一份：%v", list)
	}
	if err := m.DeleteBackup("s", f); err != nil {
		t.Fatal(err)
	}
	if list := backupFiles(t, m); len(list) != 0 {
		t.Fatalf("删除后不应再列出：%v", list)
	}
}

// 客户端给的备份文件名只能解析到已知备份根下本实例的目录里：带路径、越级、盘符、流名的都落不到外面。
func TestBackupFileStaysInsideRoots(t *testing.T) {
	m, dir := tempInstance(t)
	outside := filepath.Join(app.Base, "outside.zip")          // 不在任何备份根里
	sibling := filepath.Join(app.BackupsDir, "other", "x.zip") // 在备份根里，但属于别的实例
	zipWorld(t, dir, "evil", outside)
	zipWorld(t, dir, "evil", sibling)
	writeLevel(t, dir, "safe")
	for _, f := range []string{outside, "../outside.zip", `..\..\outside.zip`, `..\other\x.zip`, "..", "", "x", "C:x.zip", "ab:x.zip"} {
		if err := m.RestoreBackup("s", f); err == nil {
			t.Errorf("RestoreBackup(%q) 应失败", f)
		}
		if err := m.DeleteBackup("s", f); err == nil {
			t.Errorf("DeleteBackup(%q) 应失败", f)
		}
	}
	for _, p := range []string{outside, sibling} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s 不应被删除：%v", p, err)
		}
	}
	if got := readLevel(t, dir); got != "safe" {
		t.Fatalf("世界不应被改动：%q", got)
	}
}

// 还原时目标本身是服务器文件夹里一个指到外面文件的符号链接：上一级查链接那段不看目标本身，
// 只剩 RealPathStrict + InsideReal 这一道拦它。还原要被拒，外面那份不动。
func TestBackupRestoreRefusesTargetSymlinkOutside(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside.dat")
	const keep = "outside, do not touch"
	if err := os.WriteFile(outside, []byte(keep), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(base, "server")
	if err := os.MkdirAll(filepath.Join(dest, "world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, "world", "level.dat")); err != nil {
		if os.Getenv("CI") == "" {
			t.Skipf("建不了符号链接（多半是账户没有这个权限），跳过：%v", err)
		}
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create("world/level.dat")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("escaped"))
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkBackupPaths(zr.File, dest); err == nil || !strings.Contains(err.Error(), "「world/level.dat」指到了服务器文件夹外面") {
		t.Fatalf("目标本身是指到外面的符号链接，应被拦住并说清是哪一个：%v", err)
	}
	lease, err := diskguard.Acquire(context.Background(), dest, diskguard.Options{AllowUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := unzipReaderTo(zr, dest, lease); err == nil {
		t.Fatal("解压也应被拦住")
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != keep {
		t.Fatalf("服务器文件夹外的文件被改了：%q %v", b, err)
	}
}
