package tunnel

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"minedeck/internal/app"
)

// 程序文件夹本身是个目录联接（比如用户把 MineDeck 文件夹挪到别的盘、原处留了联接）：备份、临时文件、写回都在联接
// 指着的那个文件夹里，联接还是联接，没被换成普通文件夹。联接的建法同 catalog 包测试里的 junction（测试帮手跨不了包）。
func TestRenumberThroughJunction(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	target := filepath.Join(t.TempDir(), "真的文件夹")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "联接")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	app.Base = link

	orig, _ := dupFixture(t)
	if err := os.WriteFile(filepath.Join(target, "tunnels.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("编号：%v", ids)
	}
	if got := readFile(t, filepath.Join(target, bakName)); got != orig {
		t.Fatal("备份应在联接指着的文件夹里、是原文件")
	}
	app.Base = target
	d, _ := readDisk(t)
	if ids := diskIDs(d); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("联接指着的文件夹里的 tunnels.json 应已写回：%v", ids)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeIrregular == 0 && fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("联接应还是联接：%v %v", fi.Mode(), err)
	}
}

// 程序文件夹里有名字像备份的目录联接和文件夹（这台机器建不了文件符号链接，用它们代替）：不当成现成的备份，
// 照样另留一份真的备份；联接还是联接，它指着的文件夹、那个文件夹都不动。
func TestRenumberSkipsNonFileNamedLikeBackup(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "别处")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "tunnels.json"), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(app.Base, "tunnels.json.bak-20200101-000000")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	dir := filepath.Join(app.Base, "tunnels.json.bak-20200101-000001")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("编号：%v", ids)
	}
	want := []string{filepath.Base(link), filepath.Base(dir), bakName}
	if got := backupNames(t); !slices.Equal(got, want) {
		t.Fatalf("应另留一份真的备份：%v", got)
	}
	if got := readFile(t, filepath.Join(app.Base, bakName)); got != orig {
		t.Fatal("备份应是原文件")
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 1 || notes[0].Args["backup"] != bakName {
		t.Fatalf("操作记录里的备份应是新留的那份：%+v", notes)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeIrregular == 0 && fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("联接应还是联接：%v %v", fi.Mode(), err)
	}
	if got := readFile(t, filepath.Join(target, "tunnels.json")); got != orig {
		t.Fatal("联接指着的文件夹里的东西被动了")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("名字像备份的文件夹应还在：%v", err)
	}
}

// tunnels.json 被别的程序开着、只许读（杀毒、同步盘常这样）：读得出来，换号照做，写回换不上去（atomicfile 重试一小会儿后放弃）；
// 内存里编号照样不重复，原文件一个字不动，备份留着，不算没读成。放开以后，下一次保存把换好的编号写进去，不再多一份备份。
// 独占着连读都不让的，走「没读成」那一套（load_windows_test.go），不进换号。
func TestRenumberWhileFileHeld(t *testing.T) {
	tempBase(t)
	bakName := pinBackupClock(t)
	orig, _ := dupFixture(t)
	if err := os.WriteFile(tunnelsPath(), []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	hold := holdNoDelete(t, tunnelsPath())
	release := func() { once.Do(hold) }
	t.Cleanup(release)

	m := NewManager()
	if err := m.LoadErr(); err != nil {
		t.Fatalf("写不回去不算没读成：%v", err)
	}
	if ids := tunnelIDs(m); !slices.Equal(ids, dupWantIDs) {
		t.Fatalf("写不回去时内存里的编号也不该重复：%v", ids)
	}
	if got := readFile(t, tunnelsPath()); got != orig {
		t.Fatal("被占着时原文件被改了")
	}
	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("备份应留着：%v", got)
	}
	var notes []AutoAction
	m.SetAutoHook(func(a AutoAction) { notes = append(notes, a) })
	if len(notes) != 1 || notes[0].Err == nil || notes[0].Args["backup"] != bakName {
		t.Fatalf("操作记录应说没写回去、备份在哪：%+v", notes)
	}

	release()
	if _, err := m.Add(openfrpTunnel("新加的")); err != nil {
		t.Fatal(err)
	}
	d, _ := readDisk(t)
	if ids := diskIDs(d); !slices.Equal(ids, append(slices.Clone(dupWantIDs), "tun6")) {
		t.Fatalf("放开以后保存，换好的编号应一起写进去：%v", ids)
	}
	if got := backupNames(t); !slices.Equal(got, []string{bakName}) {
		t.Fatalf("备份应还是那一份：%v", got)
	}
}
