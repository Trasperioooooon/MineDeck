package inst

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"minedeck/internal/atomicfile"
)

// server.properties 的保存：先写好临时文件，再一步改名盖掉原文件。任何时刻原位上都有一份完整的文件。
// 以前是先删原文件再改名，中间断电就没有配置文件了（2026-11-14 Codex 合并复审第 4 条）。
func TestPropsSaveNeverLeavesFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(path, []byte("level-name=mymap\ndifficulty=easy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadProps(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Set("difficulty", "hard")

	// 新文件写好、还没换上去的那一刻：原文件还在，内容还是旧的
	var seen string
	atomicfile.Hook = func(string) {
		b, err := os.ReadFile(path)
		if err != nil {
			seen = "<没有了：" + err.Error() + ">"
			return
		}
		seen = string(b)
	}
	t.Cleanup(func() { atomicfile.Hook = nil })
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	atomicfile.Hook = nil
	if seen != "level-name=mymap\ndifficulty=easy\n" {
		t.Errorf("换上新文件之前，原位上应当还是完整的旧文件：%q", seen)
	}
	if b, _ := os.ReadFile(path); string(b) != "level-name=mymap\ndifficulty=hard\n" {
		t.Errorf("保存以后的内容：%q", b)
	}
	if left := tmpFiles(t, dir); len(left) != 0 {
		t.Errorf("保存以后不该留下临时文件：%v", left)
	}

	// 原文件是只读的：照样换得掉（以前靠先删，删的时候系统会去掉只读）
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	p.Set("difficulty", "normal")
	if err := p.Save(path); err != nil {
		t.Fatalf("只读的原文件应当换得掉：%v", err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "difficulty=normal") {
		t.Errorf("只读的原文件换完以后：%q", b)
	}

	// 原文件正被别的程序独占着：保存失败，原文件原样留着，不留临时文件
	ptr, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(ptr, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	p.Set("difficulty", "peaceful")
	serr := p.Save(path)
	ferr := ForceProp(path, "difficulty", "peaceful")
	windows.CloseHandle(h)
	if serr == nil || ferr == nil {
		t.Errorf("原文件被独占时保存应当报错：Save=%v ForceProp=%v", serr, ferr)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "difficulty=normal") || !strings.Contains(string(b), "level-name=mymap") {
		t.Errorf("保存失败以后原文件应当原样留着：%q", b)
	}
	if left := tmpFiles(t, dir); len(left) != 0 {
		t.Errorf("保存失败以后不该留下临时文件：%v", left)
	}
}

// tmpFiles dir 里名字以 .tmp 结尾的文件。
func tmpFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			out = append(out, e.Name())
		}
	}
	return out
}
