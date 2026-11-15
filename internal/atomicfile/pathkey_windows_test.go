package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// 排队等锁的人要先算锁键，这时持锁的人可能正在改名替换同一个文件。
// 以前算锁键会用句柄打开这个文件本身，碰上改名替换，改名就被拒（Access is denied）；
// 改之前同样一边循环算锁键一边保存，500 次里本机失败 101 次、CI 上失败 288 次；
// inst 的 TestUpdatePropsKeepsEveryonesChange 在 CI 上偶发失败也是它（2026-11-15 在 CI 上加计数核实过）。
// 注意：替换文件要是加了重试，这个用例不改也会过；那时要在用例里关掉重试，它才还能抓住回归。
func TestPathKeyDoesNotBlockReplace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.properties")
	if err := os.WriteFile(p, []byte("a=b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				PathKey(p)
			}
		}
	}()
	const n = 200
	var errs []error
	for i := 0; i < n; i++ {
		if err := Write(p, []byte(fmt.Sprintf("a=%d\n", i)), 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	close(stop)
	<-done
	if len(errs) > 0 {
		t.Fatalf("一边算锁键一边保存，%d 次里失败 %d 次，第一次：%v", n, len(errs), errs[0])
	}
	if got, _ := os.ReadFile(p); string(got) != fmt.Sprintf("a=%d\n", n-1) {
		t.Fatalf("最后存下的不是最后一次：%q", got)
	}
}

// 最后一项改成从目录列表里查以后，锁键必须和原来打开文件问出来的一模一样：
// 不然同一个文件会有两把锁，两边的读改写不再排队。
func TestPathKeySameAsHandleLookup(t *testing.T) {
	old := func(p string) string { return strings.ToUpper(canonicalEntryPath(p, handlePath)) }
	dir := t.TempDir()
	mkfile := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "Server-Long-Name.Properties")
	mkfile(file)
	sub := filepath.Join(dir, "Sub Dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mkfile(filepath.Join(sub, "inside.txt"))
	junction := filepath.Join(dir, "alias")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, sub).CombinedOutput(); err != nil {
		t.Fatalf("创建目录联接失败：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	long := filepath.Join(dir, strings.Repeat("x", 240)+".txt")
	if err := Write(long, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	type tc struct {
		name, path string
		exists     bool // 存在的路径必须真的问到了系统（设备路径），不能两边都退回原样的路径而碰巧相等
	}
	cases := []tc{
		{"文件", file, true},
		{"最后一项大小写不同", filepath.Join(dir, "server-long-name.PROPERTIES"), true},
		{"整条路径小写", strings.ToLower(file), true},
		{"不存在的文件", filepath.Join(dir, "missing.txt"), false},
		{"不存在的几级", filepath.Join(dir, "a", "b", "c.txt"), false},
		{"文件夹", sub, true},
		{"目录联接本身", junction, true},
		{"联接里的文件", filepath.Join(junction, "inside.txt"), true},
		{"联接里不存在的文件", filepath.Join(junction, "missing.txt"), false},
		{"很长的路径", long, true},
		{"卷根", filepath.VolumeName(dir) + `\`, true},
	}
	if short, ok := shortName(file); ok && !strings.EqualFold(short, file) {
		cases = append(cases, tc{"8.3 短名", short, true})
	} else {
		t.Log("这个卷没有给出不同的 8.3 短名，跳过短名一项")
	}
	link := filepath.Join(dir, "link.properties")
	dangling := filepath.Join(dir, "dangling.properties")
	switch err := os.Symlink(file, link); {
	case err == nil:
		if err := os.Symlink(filepath.Join(dir, "gone.properties"), dangling); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, tc{"文件符号链接本身", link, true}, tc{"指向不存在文件的符号链接", dangling, true})
	case errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD):
		t.Logf("账户没有创建符号链接的权限，跳过符号链接两项：%v", err)
	default:
		t.Fatal(err)
	}

	for _, c := range cases {
		got, want := PathKey(c.path), old(c.path)
		if got != want {
			t.Errorf("%s：%q 的锁键是 %q，原来是 %q", c.name, c.path, got, want)
		}
		if c.exists && !strings.HasPrefix(got, `\DEVICE\`) {
			t.Errorf("%s：%q 没有问到系统里的路径：%q", c.name, c.path, got)
		}
	}
	if _, err := os.Lstat(link); err == nil && PathKey(link) == PathKey(file) {
		t.Error("文件符号链接被跟随了：替换的是链接这个目录项，锁键不能是它指向的文件")
	}
}

func shortName(p string) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || int(n) >= len(buf) {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}
