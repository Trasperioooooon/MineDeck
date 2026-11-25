package chatai

import (
	"os"
	"path/filepath"
	"testing"

	"minedeck/internal/atomicfile"
)

// 游戏内 AI 的设置和状态写在服务器文件夹里（A11）：以前先写固定名字的「原名.tmp」，那个名字上要是放着服务器文件夹外
// 某个文件的硬链接，外面那份会被盖掉。现在两份都不能改到外面，服务器里的文件是独立的新文件。
func TestSettingsAndStateWriteDoNotTouchHardlinkedTmp(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const keep = "outside, do not touch\n"
	outsideOf := map[string]string{}
	for _, name := range []string{settingsFile, stateFile} {
		outside := filepath.Join(base, "outside-"+name)
		if err := os.WriteFile(outside, []byte(keep), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(outside, filepath.Join(dir, name+".tmp")); err != nil {
			t.Fatalf("建硬链接失败（要在允许建硬链接的盘上跑）：%v", err)
		}
		outsideOf[name] = outside
	}

	s := Defaults()
	s.Enabled = true
	if err := saveSettings(dir, s); err != nil {
		t.Fatalf("存设置：%v", err)
	}
	if err := writeFile(filepath.Join(dir, stateFile), []byte(`{"v":1}`+"\n")); err != nil { // persist 走的就是它
		t.Fatalf("写状态：%v", err)
	}

	for name, outside := range outsideOf {
		if b, err := os.ReadFile(outside); err != nil || string(b) != keep {
			t.Fatalf("%s：服务器文件夹外的文件被改了：%q %v", name, b, err)
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		n, err := atomicfile.HardlinkCount(f)
		f.Close()
		if err != nil || n != 1 {
			t.Fatalf("%s：服务器里的应是独立的文件（硬链接数 1）：n=%d err=%v", name, n, err)
		}
	}
	if got := loadSettings(dir); !got.Enabled {
		t.Fatalf("存下的设置读不回来：%+v", got)
	}
}
