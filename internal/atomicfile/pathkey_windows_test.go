package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/windows"
)

// 排队等锁的人要先算锁键，这时持锁的人可能正在改名替换同一个文件。
// 以前算锁键会用句柄打开这个文件本身，碰上改名替换，改名就被拒（Access is denied）；
// 改之前同样一边循环算锁键一边保存，500 次里本机失败 101 次、CI 上失败 288 次；
// inst 的 TestUpdatePropsKeepsEveryonesChange 在 CI 上偶发失败也是它（2026-11-15 在 CI 上加计数核实过）。
// 替换文件现在带重试，不关掉的话旧查法下这个用例也会过，所以先把重试关掉，它才抓得住回归。
func TestPathKeyDoesNotBlockReplace(t *testing.T) {
	setRetry(t, nil, nil)
	p := filepath.Join(t.TempDir(), "server.properties")
	if err := os.WriteFile(p, []byte("a=b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop, done, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				PathKey(p)
				if calls.Add(1) == 1 {
					close(started)
				}
			}
		}
	}()
	<-started // 算锁键的那一路真的跑起来了再开始存
	const n = 200
	var errs []error
	for i := 0; i < n; i++ {
		if err := Write(p, []byte(fmt.Sprintf("a=%d\n", i)), 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	close(stop)
	<-done
	// 算锁键的次数比保存还少，说明那一路没怎么抢到时间片，这一遍没测到东西，不能算过
	if c := calls.Load(); c < n {
		t.Fatalf("保存 %d 次期间只算了 %d 次锁键，两边没有真的并发起来", n, c)
	}
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
	// 名字末尾带空格、带两个点的文件夹，旁边有一个名字正常的。这种文件夹普通办法建不出来（解压工具、WSL 建得出），
	// 单独拿它的路径去问系统，末尾的空格和点会被去掉，问到的是旁边那个。
	twin := filepath.Join(dir, "Odd")
	if err := os.Mkdir(twin, 0o755); err != nil {
		t.Fatal(err)
	}
	mkfile(filepath.Join(twin, "inside.txt"))
	var odd []string
	for _, name := range []string{"Odd ", "Odd.."} {
		verbatim := `\\?\` + dir + `\` + name
		if err := os.Mkdir(verbatim, 0o755); err != nil {
			t.Logf("这个卷建不出名字是 %q 的文件夹，跳过这一项：%v", name, err)
			continue
		}
		mkfile(verbatim + `\inside.txt`)
		odd = append(odd, dir+`\`+name+`\inside.txt`)
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
		{"末尾多一个点", file + ".", true},
		{"末尾多一个空格", file + " ", true},
		{"名字带星号", filepath.Join(dir, "Server*.Properties"), false},
		{"名字带问号", filepath.Join(dir, "Server-Long-Name.Propertie?"), false},
		{"默认数据流的写法", file + "::$DATA", true},
		{`带 \\?\ 前缀的写法`, `\\?\` + file, true},
		{"设备名", filepath.Join(dir, "nul"), false},
	}
	for _, p := range odd {
		cases = append(cases, tc{"上一级文件夹名末尾带空格或点", p, true}, tc{`上一级文件夹名末尾带空格或点，带 \\?\ 前缀`, `\\?\` + p, true})
	}
	// 指向同一个文件的各种写法，锁键都得是同一把
	same := []string{filepath.Join(dir, "server-long-name.PROPERTIES"), strings.ToLower(file), file + ".", file + " ", file + "::$DATA", `\\?\` + file}
	// 这几样平常的路径应该由目录列表查出来。要是悄悄都走了退路（退回打开句柄），
	// 锁键还是对的，TestPathKeyDoesNotBlockReplace 测的那个毛病却回来了。
	listed := []string{file, strings.ToLower(file), sub, junction, filepath.Join(junction, "inside.txt"), long}
	if short, ok := shortName(file); ok && !strings.EqualFold(short, file) {
		cases = append(cases, tc{"8.3 短名", short, true})
		listed = append(listed, short)
		same = append(same, short)
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
		listed = append(listed, link, dangling)
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
	for _, p := range same {
		if got, want := PathKey(p), PathKey(file); got != want {
			t.Errorf("%q 和 %q 是同一个文件，锁键却不一样：%q、%q", p, file, got, want)
		}
	}
	for _, p := range odd {
		if PathKey(p) == PathKey(filepath.Join(twin, "inside.txt")) {
			t.Errorf("%q 的锁键和旁边名字正常的文件夹里那个文件一样：上一级被认成了另一个文件夹", p)
		}
		// 带 \\?\ 的写法不经过系统的规范化，指的是同一个文件
		if v := `\\?\` + p; PathKey(v) != PathKey(p) {
			t.Errorf("%q 和 %q 是同一个文件，锁键却不一样：%q、%q", p, v, PathKey(p), PathKey(v))
		}
	}
	for _, p := range listed {
		got, ok := listedPath(p)
		want, _ := handlePath(p, false)
		if !ok || got != want {
			t.Errorf("%q 应该由目录列表查出来：得到 %q（%v），打开句柄问到的是 %q", p, got, ok, want)
		}
	}
	for _, p := range []string{filepath.Join(dir, "missing.txt"), filepath.Join(dir, "Server*.Properties"), filepath.VolumeName(dir) + `\`} {
		if got, ok := listedPath(p); ok {
			t.Errorf("%q 在目录列表里查不准，应该交给老查法，却得到了 %q", p, got)
		}
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
