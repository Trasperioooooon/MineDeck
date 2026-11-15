package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// holder 模拟别的程序（或本程序里没排队的那一路）打开着某个文件，可以在任意时刻放开，放开只算一次。
type holder struct {
	h    windows.Handle
	once sync.Once
}

func hold(t *testing.T, path string, access, share, flags uint32) *holder {
	t.Helper()
	u, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(u, access, share, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		t.Fatal(err)
	}
	o := &holder{h: h}
	t.Cleanup(o.release)
	return o
}

func (o *holder) release() { o.once.Do(func() { windows.CloseHandle(o.h) }) }

// setRetry 换上测试用的重试间隔，并记下每次重试；测试结束还原。
func setRetry(t *testing.T, delays []time.Duration, onRetry func(attempt int)) *int {
	t.Helper()
	oldDelays, oldHook := retryDelays, beforeRetry
	t.Cleanup(func() { retryDelays, beforeRetry = oldDelays, oldHook })
	n := new(int)
	retryDelays = delays
	beforeRetry = func(attempt int) {
		*n = attempt
		if onRetry != nil {
			onRetry(attempt)
		}
	}
	return n
}

func ms(n int) []time.Duration {
	d := make([]time.Duration, n)
	for i := range d {
		d[i] = time.Millisecond
	}
	return d
}

const shareAll = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

// 目标文件被别人打开着一小会儿再放开：重试以后换上新内容。
// 第一种打开方式就是 PathKey 查真实名字时的做法，2026-11-14 CI 上那次 Access is denied 撞的就是它。
func TestReplaceRetriesUntilTargetReleased(t *testing.T) {
	holders := []struct {
		name                 string
		access, share, flags uint32
	}{
		{"算锁键那样打开（不要权限、允许删）", 0, shareAll, windows.FILE_FLAG_BACKUP_SEMANTICS | windows.FILE_FLAG_OPEN_REPARSE_POINT},
		{"读着、不许删（os.Open 的共享方式）", windows.GENERIC_READ, windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE, 0},
		{"独占", windows.GENERIC_READ, 0, 0},
	}
	writers := []struct {
		name  string
		write func(string, []byte, fs.FileMode) error
	}{{"Write", Write}, {"WriteProtected", WriteProtected}}
	for _, hd := range holders {
		for _, w := range writers {
			t.Run(hd.name+"/"+w.name, func(t *testing.T) {
				dir := t.TempDir()
				p := filepath.Join(dir, "server.properties")
				if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
				o := hold(t, p, hd.access, hd.share, hd.flags)
				n := setRetry(t, ms(10), func(attempt int) {
					if attempt == 2 {
						o.release()
					}
				})
				if err := w.write(p, []byte("new"), 0o644); err != nil {
					t.Fatalf("放开以后应当换得上：%v", err)
				}
				if *n != 2 {
					t.Errorf("应当在第 2 次重试时成功，实际重试了 %d 次", *n)
				}
				if got, _ := os.ReadFile(p); string(got) != "new" {
					t.Errorf("内容：%q", got)
				}
				assertNoTemps(t, dir)
			})
		}
	}
}

// 临时文件刚写好就被别人打开着、不许删（杀毒扫新文件常这样）：报的是共享冲突，同样重试。目标在不在都一样。
func TestReplaceRetriesWhileTempHeld(t *testing.T) {
	for _, exists := range []bool{true, false} {
		name := "目标在"
		if !exists {
			name = "目标不在"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "tunnels.json")
			if exists {
				if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var o *holder
			t.Cleanup(func() { Hook = nil })
			Hook = func(string) {
				tmps, _ := filepath.Glob(filepath.Join(dir, ".minedeck-write-*.tmp"))
				if len(tmps) != 1 {
					t.Errorf("临时文件：%v", tmps)
					return
				}
				o = hold(t, tmps[0], windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, 0)
			}
			n := setRetry(t, ms(10), func(attempt int) {
				if attempt == 1 && o != nil {
					o.release()
				}
			})
			if err := Write(p, []byte("new"), 0o644); err != nil {
				t.Fatal(err)
			}
			Hook = nil
			if o == nil || *n != 1 {
				t.Errorf("应当占过临时文件并重试 1 次：占了=%v 重试=%d", o != nil, *n)
			}
			if got, _ := os.ReadFile(p); string(got) != "new" {
				t.Errorf("内容：%q", got)
			}
			assertNoTemps(t, dir)
		})
	}
}

// 临时文件一直被占着：试满次数后报共享冲突，原文件完好。临时文件留给调用方清理（Replace 的约定）。
func TestReplaceGivesUpWhenTempStaysHeld(t *testing.T) {
	n := setRetry(t, ms(5), nil)
	dir := t.TempDir()
	p := filepath.Join(dir, "target")
	tmp := filepath.Join(dir, "incoming.tmp")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	o := hold(t, tmp, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, 0)
	err = replace(tmp, p, old)
	o.release()
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("应当报共享冲突：%v", err)
	}
	if *n != 5 {
		t.Errorf("应当重试 5 次，实际 %d 次", *n)
	}
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Errorf("原文件被改了：%q", got)
	}
	if err := os.Remove(tmp); err != nil {
		t.Errorf("临时文件应当还在、由调用方删：%v", err)
	}
}

// 不值得等的失败立刻报错：目标是文件夹或目录联接（Lstat 对联接不报 ModeDir）、错误不是拒绝访问或共享冲突。
func TestReplaceDoesNotRetryHopelessFailures(t *testing.T) {
	n := setRetry(t, ms(10), nil)
	dir := t.TempDir()
	folder := filepath.Join(dir, "folder")
	real := filepath.Join(dir, "real")
	junction := filepath.Join(dir, "junction")
	for _, d := range []string{folder, real} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, real).CombinedOutput(); err != nil {
		t.Fatalf("创建目录联接失败：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	for _, target := range []string{folder, junction} {
		*n = 0
		if err := Write(target, []byte("x"), 0o644); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Errorf("%s：应当报拒绝访问：%v", filepath.Base(target), err)
		}
		if *n != 0 {
			t.Errorf("%s：换不掉文件夹，不该重试，实际重试了 %d 次", filepath.Base(target), *n)
		}
		if st, err := os.Stat(target); err != nil || !st.IsDir() {
			t.Errorf("%s：文件夹被改了：%v", filepath.Base(target), err)
		}
	}
	*n = 0
	if err := replace(filepath.Join(dir, "missing.tmp"), filepath.Join(dir, "x"), nil); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Errorf("没有临时文件应当报找不到：%v", err)
	}
	if *n != 0 {
		t.Errorf("找不到文件不该重试，实际重试了 %d 次", *n)
	}
	assertNoTemps(t, dir)
}

// 原文件一开始是只读的，换之前被人改成可写、还占着：仍按只读的老路走，一次也不重试（守住 old 那一条）。
func TestOriginallyReadOnlyTargetIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(p, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	n := setRetry(t, ms(10), nil)
	t.Cleanup(func() { Hook = nil })
	Hook = func(string) {
		if err := os.Chmod(p, 0o644); err != nil {
			t.Error(err)
		}
		hold(t, p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, 0)
	}
	if err := Write(p, []byte("new"), 0o644); err == nil {
		t.Fatal("被占着、不许删的目标不该换得掉")
	}
	Hook = nil
	if *n != 0 {
		t.Errorf("原来只读的目标不该进重试，实际重试了 %d 次", *n)
	}
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Errorf("原文件被改了：%q", got)
	}
}

// 一直占着：用尽次数后报原来的拒绝访问，原文件完好，不留临时文件。用的是真实的间隔，顺带看总时长有上限。
func TestReplaceGivesUpWhenTargetStaysBusy(t *testing.T) {
	var total time.Duration
	for _, d := range retryDelays {
		total += d
	}
	if total > time.Second {
		t.Fatalf("重试总时长 %v 太长：保存时还拿着锁，别的保存都在等", total)
	}
	n := setRetry(t, retryDelays, nil)
	dir := t.TempDir()
	p := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := hold(t, p, windows.GENERIC_READ, 0, 0)
	start := time.Now()
	err := Write(p, []byte("new"), 0o644)
	took := time.Since(start)
	o.release()
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("应当报拒绝访问：%v", err)
	}
	if *n != len(retryDelays) {
		t.Errorf("应当重试 %d 次，实际 %d 次", len(retryDelays), *n)
	}
	if !strings.Contains(err.Error(), "重试") {
		t.Errorf("报错里应当说重试过：%v", err)
	}
	// 下限是 Sleep 保证的；上限不断言，机器一忙墙钟就不可靠，只记下来
	if took < total {
		t.Errorf("用时 %v，比间隔加起来的 %v 还短", took, total)
	}
	t.Logf("重试 %d 次，用时 %v（间隔合计 %v）", *n, took, total)
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Errorf("原文件被改了：%q", got)
	}
	assertNoTemps(t, dir)
}

// 只读的目标按原来的规矩：WriteProtected 一次就拒绝、不重试；Write 直接走扩展改名换上，也不重试。
func TestReadOnlyTargetIsNotRetried(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(p, []byte("old"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	n := setRetry(t, ms(10), nil)
	if err := WriteProtected(p, []byte("new"), 0o644); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("只读目标应当拒绝覆盖：%v", err)
	}
	if *n != 0 {
		t.Errorf("只读目标不该重试，实际重试了 %d 次", *n)
	}
	st, _ := os.Stat(p)
	if got, _ := os.ReadFile(p); string(got) != "old" || st.Mode().Perm()&0o200 != 0 {
		t.Errorf("只读目标被改了：%q %v", got, st.Mode())
	}
	if err := Write(p, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if *n != 0 {
		t.Errorf("Write 换只读目标走扩展改名，不该重试，实际重试了 %d 次", *n)
	}
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Errorf("内容：%q", got)
	}
	assertNoTemps(t, dir)
}

// 重试期间目标变成了只读（原地改属性，或者被换成另一个只读文件）：绝不盖掉它，也不再接着试。
// 钩子在第 2 次改名之前改目标，所以第 2 次改名照样会调，是 MoveFileEx 自己拒绝了只读目标；之后的判断看到只读就停。
// 两种写法都要守住：Write 只对一开始就只读的原文件用扩展改名，WriteProtected 从来不用。
func TestRetryStopsWhenTargetTurnsReadOnly(t *testing.T) {
	changes := []struct {
		name   string
		change func(t *testing.T, p string)
		want   string
	}{
		{"原地改成只读", func(t *testing.T, p string) {
			if err := os.Chmod(p, 0o444); err != nil {
				t.Error(err)
			}
		}, "old"},
		{"换成另一个只读文件", func(t *testing.T, p string) {
			if err := os.Rename(p, p+".orig"); err != nil {
				t.Error(err)
			}
			if err := os.WriteFile(p, []byte("other"), 0o444); err != nil {
				t.Error(err)
			}
		}, "other"},
	}
	writers := []struct {
		name  string
		write func(string, []byte, fs.FileMode) error
	}{{"Write", Write}, {"WriteProtected", WriteProtected}}
	for _, c := range changes {
		for _, w := range writers {
			t.Run(c.name+"/"+w.name, func(t *testing.T) {
				dir := t.TempDir()
				p := filepath.Join(dir, "server.properties")
				if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(p, 0o644) })
				o := hold(t, p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, 0)
				n := setRetry(t, ms(10), func(attempt int) {
					if attempt == 2 {
						o.release()
						c.change(t, p)
					}
				})
				if err := w.write(p, []byte("new"), 0o644); err == nil {
					t.Fatal("变成只读的目标不该被盖掉")
				}
				if *n != 2 {
					t.Errorf("目标变成只读以后应当停止重试：重试了 %d 次", *n)
				}
				st, _ := os.Stat(p)
				if got, _ := os.ReadFile(p); string(got) != c.want || st.Mode().Perm()&0o200 != 0 {
					t.Errorf("目标被改了：%q %v", got, st.Mode())
				}
				assertNoTemps(t, dir)
			})
		}
	}
}
