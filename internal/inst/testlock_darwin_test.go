package inst

import (
	"os"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// lockExclusive 模拟别的程序独占着一个文件（见 testlock_windows_test.go）。Mac 上没有独占打开：
// 改成读写不了（权限清零），再加上不可更改标记（chflags uchg：换不掉、删不掉），放开时改回原样。
func lockExclusive(t *testing.T, path string) func() {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.Chflags(path, unix.UF_IMMUTABLE); err != nil {
		_ = os.Chmod(path, st.Mode().Perm())
		t.Fatal(err)
	}
	var once sync.Once
	undo := func() {
		once.Do(func() {
			_ = unix.Chflags(path, 0)
			_ = os.Chmod(path, st.Mode().Perm())
		})
	}
	t.Cleanup(undo)
	return undo
}

// shortPath Mac 上没有 8.3 短名，原样返回（用到它的测试会跳过短名那一半）。
func shortPath(p string) string { return p }
