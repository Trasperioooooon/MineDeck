package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		`move /y "%MD_UPDATE_NEW%" "%MD_UPDATE_EXE%"`, `if exist "%MD_UPDATE_EXE%" start "" /D "%MD_UPDATE_DIR%" "%MD_UPDATE_EXE%"`} {
		if !strings.Contains(swapScript, want) {
			t.Errorf("脚本里少了 %q", want)
		}
	}
}

// 真跑一遍换 exe：文件夹名字带中文、%、&、括号和空格；PATH 最前面放着冒名的 find、tasklist、ping、cmd；
// 当前目录不是程序所在的文件夹；旧进程退出以后 exe 还被别人占着一会儿。
// 要做到：旧进程活着时不动；退出后等占用放开再换；换好以后从程序所在的文件夹打开新版，
// 新版拿不到 MD_UPDATE_* 这些变量；脚本删掉自己。
func TestSwapReplacesExeAndRestarts(t *testing.T) {
	if testing.Short() {
		t.Skip("要编一个小程序、真跑脚本，-short 时跳过")
	}
	root := t.TempDir()
	marker := filepath.Join(root, "swapmarker.exe")
	build := exec.Command(goTool(t), "build", "-ldflags=-H=windowsgui", "-o", marker, "./testdata/swapmarker")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编测试用的小程序失败: %v\n%s", err, out)
	}
	bin, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "我的世界 100% A&B (开服)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := filepath.Join(dir, "MineDeck.exe")
	newExe := filepath.Join(dir, "MineDeck.new.exe")
	newBin := append(append([]byte{}, bin...), []byte("\nNEW-BUILD-TRAILER")...) // 末尾多几个字节，exe 照样能跑，内容就和旧的不一样了
	if err := os.WriteFile(oldExe, bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newExe, newBin, 0o755); err != nil {
		t.Fatal(err)
	}

	// 冒名的系统命令：脚本要是没写全路径用到了它们，就会像 3.1.0 那样不等旧进程、换不成
	shadow := filepath.Join(root, "shadow")
	if err := os.MkdirAll(shadow, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"find.bat", "tasklist.bat", "ping.bat", "cmd.bat", "move.bat", "start.bat"} {
		if err := os.WriteFile(filepath.Join(shadow, name), []byte("@exit /b 1\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+os.Getenv("PATH"))

	old := exec.Command(oldExe, "-sleep", "120")
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Process.Kill(); _, _ = old.Process.Wait() })

	if err := startSwap(dir, oldExe, newExe, old.Process.Pid); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if got, _ := os.ReadFile(oldExe); !bytes.Equal(got, bin) {
		t.Fatal("旧进程还开着，exe 就被换了")
	}
	if _, err := os.Stat(newExe); err != nil {
		t.Fatalf("旧进程还开着，新版就不见了: %v", err)
	}

	// 旧进程退出，但 exe 被占着（不许删、不许改名）一会儿，脚本要隔一会儿再试，而不是放弃
	lock := holdFile(t, oldExe)
	if err := old.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = old.Process.Wait()
	time.Sleep(2500 * time.Millisecond)
	if got, _ := os.ReadFile(oldExe); !bytes.Equal(got, bin) {
		t.Fatal("exe 被占着的时候就换了？")
	}
	_ = windows.CloseHandle(lock)

	started := filepath.Join(dir, "started.txt")
	bat := filepath.Join(dir, "update.bat")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		_, errS := os.Stat(started)
		_, errB := os.Stat(bat)
		if errS == nil && os.IsNotExist(errB) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got, _ := os.ReadFile(oldExe); !bytes.Equal(got, newBin) {
		t.Fatal("exe 没换成新版")
	}
	if _, err := os.Stat(newExe); !os.IsNotExist(err) {
		t.Errorf("MineDeck.new.exe 还在: %v", err)
	}
	if _, err := os.Stat(bat); !os.IsNotExist(err) {
		t.Errorf("update.bat 没删掉自己: %v", err)
	}
	report, err := os.ReadFile(started)
	if err != nil {
		t.Fatalf("新版没被打开: %v", err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(report)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	// CI 上临时目录可能是 RUNNER~1 这种短名，比较前都换成长名
	if !strings.EqualFold(longPath(got["wd"]), longPath(dir)) {
		t.Errorf("新版打开时的当前目录是 %q，应为 %q", got["wd"], dir)
	}
	if got["args"] != "0" {
		t.Errorf("新版打开时带了 %s 个参数，应该不带", got["args"])
	}
	if got["leaked"] != "" {
		t.Errorf("新版拿到了脚本用的环境变量: %s", got["leaked"])
	}
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
