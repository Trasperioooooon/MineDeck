package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// 脚本按系统代码页读，所以里面只能有 ASCII；路径不能写进脚本，系统命令要写全路径。
func TestSwapScriptIsPlainASCII(t *testing.T) {
	for i, r := range swapScript {
		if r > 0x7e || (r < 0x20 && r != '\n') {
			t.Fatalf("脚本第 %d 个字符 %q 不是可见的 ASCII", i, r)
		}
	}
	for _, bad := range []string{"cd /d", "\ntasklist", "| find", "\nping", " ping ", "%SystemRoot%"} {
		if strings.Contains(swapScript, bad) {
			t.Errorf("脚本里不该出现 %q", bad)
		}
	}
	for _, want := range []string{`"%MD_UPDATE_SYS%\tasklist.exe"`, `"%MD_UPDATE_SYS%\find.exe"`, `"%MD_UPDATE_SYS%\ping.exe"`,
		`move /y "%MD_UPDATE_NEW%" "%MD_UPDATE_EXE%"`, `if exist "%MD_UPDATE_EXE%" start "" /D "%MD_UPDATE_DIR%" "%MD_UPDATE_EXE%"`,
		`>nul && goto done`} {
		if !strings.Contains(swapScript, want) {
			t.Errorf("脚本里少了 %q", want)
		}
	}
}

// 真跑一遍换 exe：文件夹名字带中文、空格和 cmd 里有讲究的符号；PATH 最前面放着冒名的 find、tasklist、ping、cmd；
// 当前目录不是程序所在的文件夹；旧进程退出以后 exe 还被别人占着一会儿。
// 要做到：旧进程活着时不动；退出后等占用放开再换；换好以后从程序所在的文件夹打开新版，
// 新版拿不到 MD_UPDATE_* 这些变量；脚本删掉自己。
func TestSwapReplacesExeAndRestarts(t *testing.T) {
	s := newSwapSetup(t)
	old := s.startOld(t)

	if err := startSwap(s.dir, s.oldExe, s.newExe, old.Process.Pid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	s.wantExe(t, s.oldBin, "旧进程还开着，exe 就被换了")
	if _, err := os.Stat(s.newExe); err != nil {
		t.Fatalf("旧进程还开着，新版就不见了: %v", err)
	}

	// 旧进程退出，但 exe 被占着（不许删、不许改名）一会儿，脚本要隔一会儿再试，而不是放弃
	lock := holdFile(t, s.oldExe)
	if err := old.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = old.Process.Wait()
	time.Sleep(2500 * time.Millisecond)
	s.wantExe(t, s.oldBin, "exe 被占着的时候就换了？")
	_ = windows.CloseHandle(lock)

	s.waitScriptGone(t, true)
	s.wantExe(t, s.newBin, "exe 没换成新版")
	if _, err := os.Stat(s.newExe); !os.IsNotExist(err) {
		t.Errorf("MineDeck.new.exe 还在: %v", err)
	}
	s.wantStarted(t)
}

// 换不成、旧进程又一直不退：什么都不打开，免得同时开着两个；新版留着，exe 不动。
func TestSwapGivesUpWithoutSecondCopy(t *testing.T) {
	s := newSwapSetup(t)
	shortenSwap(t, 2, 3)
	old := s.startOld(t)

	if err := startSwap(s.dir, s.oldExe, s.newExe, old.Process.Pid); err != nil {
		t.Fatal(err)
	}
	s.waitScriptGone(t, false)
	time.Sleep(time.Second) // 万一脚本 start 了什么，给它一点时间写出 started.txt
	s.wantExe(t, s.oldBin, "旧进程还开着，exe 却被换了")
	if _, err := os.Stat(s.newExe); err != nil {
		t.Errorf("放弃以后新版应该留着: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "started.txt")); err == nil {
		t.Error("旧进程还开着，脚本又打开了一个")
	}
	if !processAlive(old.Process.Pid) {
		t.Error("旧进程不该被动到")
	}
}

// processAlive 看进程还在不在跑。
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

// 旧进程已经退了，但 exe 一直被占着换不了：放弃，把旧版重新打开（用户不至于点了更新以后什么都没了）。
func TestSwapReopensOldWhenExeStaysLocked(t *testing.T) {
	s := newSwapSetup(t)
	shortenSwap(t, 2, 3)
	old := s.startOld(t)
	lock := holdFile(t, s.oldExe)
	defer windows.CloseHandle(lock)
	pid := old.Process.Pid
	if err := old.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = old.Process.Wait()

	if err := startSwap(s.dir, s.oldExe, s.newExe, pid); err != nil {
		t.Fatal(err)
	}
	s.waitScriptGone(t, true)
	s.wantExe(t, s.oldBin, "exe 被占着，却换成了新版？")
	if _, err := os.Stat(s.newExe); err != nil {
		t.Errorf("放弃以后新版应该留着: %v", err)
	}
	s.wantStarted(t)
}

type swapSetup struct {
	dir, oldExe, newExe string
	oldBin, newBin      []byte
}

var (
	markerOnce sync.Once
	markerBin  []byte
	markerErr  string
)

// newSwapSetup 编好测试用的小程序（整个包只编一次），在一个名字很刁钻的文件夹里放好旧版和新版，
// 再把冒名的系统命令放到 PATH 最前面。
func newSwapSetup(t *testing.T) *swapSetup {
	t.Helper()
	if testing.Short() {
		t.Skip("要编一个小程序、真跑脚本，-short 时跳过")
	}
	gobin := goTool(t) // 找不到 go 就在这里跳过，不能放进 Once 里（那样后面的测试会当成编译失败）
	markerOnce.Do(func() {
		out := filepath.Join(os.TempDir(), "minedeck-swapmarker-test.exe")
		build := exec.Command(gobin, "build", "-ldflags=-H=windowsgui", "-o", out, "./testdata/swapmarker")
		if msg, err := build.CombinedOutput(); err != nil {
			markerErr = err.Error() + "\n" + string(msg)
			return
		}
		markerBin, _ = os.ReadFile(out)
		_ = os.Remove(out)
	})
	if markerBin == nil {
		t.Fatalf("编测试用的小程序失败: %s", markerErr)
	}

	root := t.TempDir()
	// 中文、空格、% & ( ) ! ^ = ; , ' 都放进去：路径要原样经环境变量走到 move 和 start
	s := &swapSetup{dir: filepath.Join(root, "我的世界 100% A&B (开服) !x! ^y a=b;c,d'e")}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if wd, _ := os.Getwd(); strings.EqualFold(longPath(wd), longPath(s.dir)) {
		t.Fatal("测试的当前目录不能就是程序所在的文件夹")
	}
	s.oldExe = filepath.Join(s.dir, "MineDeck.exe")
	s.newExe = filepath.Join(s.dir, "MineDeck.new.exe")
	s.oldBin = markerBin
	s.newBin = append(append([]byte{}, markerBin...), []byte("\nNEW-BUILD-TRAILER")...) // 末尾多几个字节，exe 照样能跑，内容就和旧的不一样了
	if err := os.WriteFile(s.oldExe, s.oldBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.newExe, s.newBin, 0o755); err != nil {
		t.Fatal(err)
	}

	// 冒名的系统命令：脚本要是没写全路径用到了它们，就会像 3.1.0 那样不等旧进程、换不成。
	// cmd 按 PATH 的顺序找，同一个文件夹里 .bat 和 .exe 一样算数。move、start 是 cmd 自己的命令，不查 PATH，不用放。
	shadow := filepath.Join(root, "shadow")
	if err := os.MkdirAll(shadow, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"find.bat", "tasklist.bat", "ping.bat", "cmd.bat"} {
		if err := os.WriteFile(filepath.Join(shadow, name), []byte("@exit /b 1\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+os.Getenv("PATH"))
	return s
}

// startOld 扮旧版：开着不退，占住 MineDeck.exe。测试结束时一定关掉。
func (s *swapSetup) startOld(t *testing.T) *exec.Cmd {
	t.Helper()
	old := exec.Command(s.oldExe, "-sleep", "120")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Process.Kill(); _, _ = old.Process.Wait() })
	return old
}

// waitScriptGone 等脚本删掉自己；opened 为真时还要等被打开的程序写出 started.txt。
func (s *swapSetup) waitScriptGone(t *testing.T, opened bool) {
	t.Helper()
	bat := filepath.Join(s.dir, "update.bat")
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		_, errB := os.Stat(bat)
		_, errS := os.Stat(filepath.Join(s.dir, "started.txt"))
		if os.IsNotExist(errB) && (!opened || errS == nil) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("等了 40 秒，脚本还没跑完（update.bat 还在，或者程序没被打开）")
}

func (s *swapSetup) wantExe(t *testing.T, want []byte, msg string) {
	t.Helper()
	if got, _ := os.ReadFile(s.oldExe); !bytes.Equal(got, want) {
		t.Fatal(msg)
	}
}

// wantStarted 看被打开的程序：从程序所在的文件夹打开、不带参数、拿不到脚本用的环境变量。
func (s *swapSetup) wantStarted(t *testing.T) {
	t.Helper()
	report, err := os.ReadFile(filepath.Join(s.dir, "started.txt"))
	if err != nil {
		t.Fatalf("程序没被打开: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(report)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	// CI 上临时目录可能是 RUNNER~1 这种短名，比较前都换成长名
	if !strings.EqualFold(longPath(got["wd"]), longPath(s.dir)) {
		t.Errorf("程序打开时的当前目录是 %q，应为 %q", got["wd"], s.dir)
	}
	if got["args"] != "0" {
		t.Errorf("程序打开时带了 %s 个参数，应该不带", got["args"])
	}
	if got["leaked"] != "" {
		t.Errorf("程序拿到了脚本用的环境变量: %s", got["leaked"])
	}
}

// shortenSwap 把脚本等旧进程的轮数、换 exe 的次数改小，测试结束时改回去。
func shortenSwap(t *testing.T, waits, tries int) {
	t.Helper()
	w, r := swapMaxWaits, swapMaxTries
	swapMaxWaits, swapMaxTries = waits, tries
	t.Cleanup(func() { swapMaxWaits, swapMaxTries = w, r })
}

// holdFile 只读打开文件、不让别人删除或改名（不给 FILE_SHARE_DELETE），占住它。
func holdFile(t *testing.T, path string) windows.Handle {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("占住 %s 失败: %v", path, err)
	}
	return h
}

// longPath 把 8.3 短名换成长名，换不了就原样返回。
func longPath(p string) string {
	u, err := syscall.UTF16FromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(&u[0], &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return syscall.UTF16ToString(buf[:n])
}

// goTool 找 go 命令：先看 PATH，再看编这个测试用的 GOROOT。都没有就跳过。
func goTool(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	if p := filepath.Join(runtime.GOROOT(), "bin", "go.exe"); fileExists(p) {
		return p
	}
	t.Skip("找不到 go 命令，编不了测试用的小程序")
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
