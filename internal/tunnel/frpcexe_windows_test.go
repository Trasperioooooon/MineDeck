package tunnel

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

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
