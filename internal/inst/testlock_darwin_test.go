package inst

import (
	"os"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// lockExclusive 模拟别的程序独占着一个文件（见 testlock_windows_test.go）。Mac 上没有独占打开：
// 按打开着的文件描述符（O_EVTONLY）清零权限并加不可更改标记（chflags uchg），撤销和清理也走这个 fd，
// 这样即使测试途中上层文件夹被改名，仍能稳定撤销标记让临时目录删得掉。
func lockExclusive(t *testing.T, path string) func() {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Fchmod(fd, 0); err != nil {
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	if err := unix.Fchflags(fd, unix.UF_IMMUTABLE); err != nil {
		_ = unix.Fchmod(fd, uint32(st.Mode().Perm()))
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	var once sync.Once
	undo := func() {
		once.Do(func() {
			_ = unix.Fchflags(fd, 0)
			_ = unix.Fchmod(fd, uint32(st.Mode().Perm()))
			_ = unix.Close(fd)
		})
	}
	t.Cleanup(undo)
	return undo
}

// shortPath Mac 上没有 8.3 短名，原样返回（用到它的测试会跳过短名那一半）。
func shortPath(p string) string { return p }
