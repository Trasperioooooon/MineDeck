package tunnel

import (
	"archive/zip"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

	"minedeck/internal/app"
	"minedeck/internal/procutil"
)

// 拿真的程序试：测试程序自己、系统自带的程序都是完整的 exe，不能被误伤；把测试程序截掉一截就该认出来。
// 再真的启动一下截断的那份，确认系统也不认它：这条规则和系统加载程序时的要求是一回事，不是自己想出来的。
func TestInspectExeRealBinary(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, why, _ := inspectExe(self); got != exeOK {
		t.Fatalf("测试程序自己是完整的，不该被认成坏的：%d %s", got, why)
	}
	for _, name := range []string{"where.exe", "cmd.exe", "notepad.exe", "curl.exe"} {
		sys := filepath.Join(os.Getenv("SystemRoot"), "System32", name)
		if _, err := os.Stat(sys); err != nil {
			continue
		}
		if got, why, _ := inspectExe(sys); got != exeOK {
			t.Errorf("系统自带的 %s 不该被认成坏的：%d %s", name, got, why)
		}
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, cut := range []struct {
		name string
		keep int
	}{{"留一半", len(b) / 2}, {"留九成", len(b) / 10 * 9}, {"只少最后 1 KB", len(b) - 1024}, {"留 4 KB", 4096}, {"留 100 字节", 100}} {
		p := filepath.Join(t.TempDir(), "frpc.exe")
		writeFile(t, p, b[:cut.keep])
		if got, why, _ := inspectExe(p); got != exeTruncated {
			t.Errorf("%s：inspectExe = %d %q，应认成半截的", cut.name, got, why)
		}
		err := exec.Command(p, "-test.run=^$").Run()
		var errno syscall.Errno
		if err == nil || !errors.As(err, &errno) {
			t.Errorf("%s：截断的程序系统应拒绝加载，实际：%v", cut.name, err)
		} else {
			t.Logf("%s：系统拒绝加载（错误码 %d）", cut.name, uintptr(errno))
		}
	}
}

// holdNoDelete 打开着文件、允许别人读，但不许删除和改名（杀毒软件、同步盘占着文件时常是这样）。返回放开的函数。
func holdNoDelete(t *testing.T, path string) func() {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() { _ = windows.CloseHandle(h) }
}

// 坏的 frpc 删不掉（被占着）：报错说清楚是哪个文件、在哪，不往下走去下载。
func TestBrokenExeCannotBeRemoved(t *testing.T) {
	f := newFrpcFake(t)
	dir := frpcDir("openfrp")
	exe := filepath.Join(dir, "frpc.exe")
	writeFile(t, exe, []byte("不是程序"))
	release := holdNoDelete(t, exe)
	defer release()
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if got != "" || err == nil || !strings.Contains(err.Error(), "没能删掉") || !strings.Contains(err.Error(), "frpc.exe") || !strings.HasSuffix(err.Error(), dir) {
		t.Fatalf("删不掉应报错：%s %v", got, err)
	}
	if n := f.hitCount(""); n != 0 {
		t.Fatalf("删不掉就不该去下载：%d 次请求", n)
	}
	if readFile(t, exe) != "不是程序" {
		t.Fatal("删不掉的文件不该被改")
	}
}

// OpenFrp 缓存的 zip 里 frpc 是坏的，zip 又被别的程序开着、删不掉（只许读，不许删）：不进第二轮（下一轮 dl.Fetch 会直接复用它，
// 说「重新下载了一次」不是真的），也不去下载；报下载到的 frpc 是坏的、缓存的压缩包删不掉，请人关掉占用它的程序或手动删掉（A56）。
func TestOpenfrpBrokenZipCannotBeDeleted(t *testing.T) {
	f := newFrpcFake(t)
	zipURL := "/client/OF_t1/" + openfrpFrpcZip
	f.set(func(f *frpcFake) { f.files[zipURL] = openfrpZip(t, string(fakeFrpcExe())) })
	cache := filepath.Join(app.CacheDir, "frpc", "openfrp-frpc-OF_t1.zip")
	writeFile(t, cache, openfrpZip(t, string(fakeFrpcExe()[:700])))
	release := holdNoDelete(t, cache)
	defer release()
	con := procutil.NewConsole()
	got, err := (openfrpProvider{}).EnsureFrpc(con)
	if got != "" || err == nil || !strings.Contains(err.Error(), "下载到的 frpc 是坏的：frpc_windows_amd64.exe") ||
		!strings.Contains(err.Error(), "缓存的压缩包删不掉（") || !strings.HasSuffix(err.Error(), "请关掉占用它的程序或手动删掉 "+cache+" 后重试") ||
		strings.Contains(err.Error(), "重新下载了一次") {
		t.Fatalf("压缩包删不掉应照实报、不进第二轮：%q %v", got, err)
	}
	if n := f.hitCount(zipURL); n != 0 {
		t.Fatalf("不该去下载：%d 次", n)
	}
	if lines := strings.Join(con.Lines(), "\n"); strings.Contains(lines, "重新下载") { // 「重新下载了一次」「重新下载一次」都不该有
		t.Fatalf("控制台不该说重新下载：%s", lines)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("删不掉的压缩包还在：%v", err)
	}
	if exe := findExe(frpcDir("openfrp")); exe != "" {
		t.Fatalf("解压出来的坏 frpc 照样删掉：%s", exe)
	}
}

// OpenFrp 缓存的 zip 不能用、又被别的程序开着删不掉：zip 本身坏了解压不了的、里面没有 frpc 的，报错前后两段
// （哪里不对、压缩包删不掉）都在，errors.Is 两个原因都认得出；不去下载（A56）。
func TestOpenfrpUnusableZipCannotBeDeleted(t *testing.T) {
	for _, c := range []struct {
		name  string
		zip   []byte // 不小于 1 MiB：小了 dl.Fetch 不复用缓存，会去重新下载
		want  string // 前一段
		cause error  // 前一段的原因，errors.Is 要认得出（没有可认的为 nil）
	}{
		{"zip 本身坏了", []byte(frpPad), "zip", zip.ErrFormat},
		{"zip 里没有 frpc", frpZip(t, frpEntry{"README.txt", frpPad}), "OpenFrp frpc 解压后未找到可执行文件", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFrpcFake(t)
			zipURL := "/client/OF_t1/" + openfrpFrpcZip
			f.set(func(f *frpcFake) { f.files[zipURL] = openfrpZip(t, string(fakeFrpcExe())) })
			cache := filepath.Join(app.CacheDir, "frpc", "openfrp-frpc-OF_t1.zip")
			writeFile(t, cache, c.zip)
			release := holdNoDelete(t, cache)
			defer release()
			got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
			if got != "" || err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "缓存的压缩包删不掉（") ||
				!strings.HasSuffix(err.Error(), "请关掉占用它的程序或手动删掉 "+cache+" 后重试") {
				t.Fatalf("两段都要说：%q %v", got, err)
			}
			if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || c.cause != nil && !errors.Is(err, c.cause) {
				t.Fatalf("errors.Is 要认得出两个原因（%v、删不掉）：%v", c.cause, err)
			}
			if n := f.hitCount(zipURL); n != 0 {
				t.Fatalf("不该去下载：%d 次", n)
			}
			if _, err := os.Stat(cache); err != nil {
				t.Fatalf("删不掉的压缩包还在：%v", err)
			}
		})
	}
}

// OpenFrp 的 frpc 文件夹是目录联接（里面的东西其实在用户别处的文件夹里）：里面坏了的标准名 frpc 和用户别的文件都不删、不改，
// 报出来请人处理，也不去下载（同 usableFrpc 的规矩，2026-11-19 Codex 复审）。联接的建法同 renumber_windows_test.go。
func TestOpenfrpJunctionFolderNotTouched(t *testing.T) {
	f := newFrpcFake(t)
	user := filepath.Join(t.TempDir(), "用户自己的文件夹")
	broken := fakeFrpcExe()[:700]
	writeFile(t, filepath.Join(user, "frpc_windows_amd64.exe"), broken)
	writeFile(t, filepath.Join(user, "笔记.txt"), []byte("用户的"))
	link := frpcDir("openfrp")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, user).CombinedOutput(); err != nil {
		t.Fatalf("建不了目录联接：%v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) }) // 只删联接本身
	got, err := (openfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	if got != "" || err == nil || !strings.Contains(err.Error(), "MineDeck 没有动它") {
		t.Fatalf("联接文件夹里坏的 frpc 应报出来、不动：%q %v", got, err)
	}
	if readFile(t, filepath.Join(user, "frpc_windows_amd64.exe")) != string(broken) || readFile(t, filepath.Join(user, "笔记.txt")) != "用户的" {
		t.Fatal("联接文件夹里用户原有的文件不该删、不该改")
	}
	if n := f.hitCount(""); n != 0 {
		t.Fatalf("不该去下载：%d 次请求", n)
	}
}

// 樱花的 frpc 读不了（被别的程序独占着，杀毒软件扫描时就是这样）：判断不了就不删，照旧交给启动那一步。
func TestNatfrpUnreadableExeIsKept(t *testing.T) {
	frpcTempDirs(t)
	exe := filepath.Join(frpcDir("natfrp"), "frpc.exe")
	half := fakeFrpcExe()[:900] // 内容其实是半截的，但读不到就不能下结论
	writeFile(t, exe, half)
	release := lockExclusive(t, exe)
	if st, why, _ := inspectExe(exe); st != exeUnknown {
		t.Fatalf("读不了的应判断不了：%d %s", st, why)
	}
	got, err := (natfrpProvider{}).EnsureFrpc(procutil.NewConsole())
	release()
	if err != nil || got != exe {
		t.Fatalf("读不了的应照旧交回去：%s %v", got, err)
	}
	if readFile(t, exe) != string(half) {
		t.Fatal("读不了的不该删、不该改")
	}
}
