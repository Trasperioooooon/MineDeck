package inst

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// 两个保存同时写同一份 server.properties：各用各的临时文件，最后留下的是其中一份完整的，不会是两份混在一起。
// 以前临时文件名是固定的，两个保存打开的是同一个文件（2026-11-14 Codex 第二轮复审阻挡项 3）。
func TestTwoSavesNeverMix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(path, []byte("level-name=mymap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk := func(val string) (*Props, string) {
		p, err := LoadProps(path)
		if err != nil {
			t.Fatal(err)
		}
		p.Set("motd", val)
		return p, "level-name=mymap\nmotd=" + val + "\n"
	}
	a, wantA := mk(strings.Repeat("A", 8000)) // 一长一短：混在一起的话，短的那份后面会拖着长的那份的尾巴
	b, wantB := mk(strings.Repeat("B", 50))

	// A 的临时文件写好、还没换上去的时候停住，让 B 从头到尾存完，再放 A 走
	started, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	atomicfile.Hook = func(string) {
		first := false
		once.Do(func() { first = true })
		if first {
			close(started)
			<-proceed
		}
	}
	t.Cleanup(func() { atomicfile.Hook = nil })
	errA := make(chan error, 1)
	go func() { errA <- a.Save(path) }()
	<-started
	if err := b.Save(path); err != nil {
		t.Fatalf("B 保存：%v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != wantB {
		t.Fatalf("B 存完以后应当是 B 的那一份（%d 字节），实际 %d 字节", len(wantB), len(got))
	}
	close(proceed)
	if err := <-errA; err != nil {
		t.Fatalf("A 保存：%v", err)
	}
	atomicfile.Hook = nil
	got, _ := os.ReadFile(path)
	if string(got) != wantA && string(got) != wantB {
		t.Errorf("最后留下的应当是其中一份完整的，实际 %d 字节，开头 %q", len(got), string(got[:min(len(got), 60)]))
	}
	if left := tmpFiles(t, dir); len(left) != 0 {
		t.Errorf("都存完以后不该留下临时文件：%v", left)
	}
}

// 很多个请求同时各改一项：读、改、写是排着队做的，谁的改动都不会被别人盖掉。
func TestUpdatePropsKeepsEveryonesChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(path, []byte("level-name=mymap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%02d", i)
			if i%2 == 0 {
				errs <- UpdateProps(path, func(p *Props) error { p.Set(key, "v"); return nil })
			} else {
				errs <- ForceProp(path, key, "v") // 换地图改设置走的是它，和上面排同一个队
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("保存：%v", err)
		}
	}
	got := javaPropsOf(path)
	var missing []string
	for i := 0; i < n; i++ {
		if key := fmt.Sprintf("key-%02d", i); got[key] != "v" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 || got["level-name"] != "mymap" {
		t.Errorf("有 %d 项改动被别人盖掉了：%v（level-name=%q）", len(missing), missing, got["level-name"])
	}
	// 回调报错就不存
	before, _ := os.ReadFile(path)
	if err := UpdateProps(path, func(p *Props) error { p.Set("x", "y"); return os.ErrInvalid }); err == nil {
		t.Error("回调报错应当原样返回")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("回调报错时不该存")
	}
	if left := tmpFiles(t, dir); len(left) != 0 {
		t.Errorf("不该留下临时文件：%v", left)
	}
}
