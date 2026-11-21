package inst

import (
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

// lockExclusive 用不许别人共享的方式打开一个文件，返回放开它的函数（模拟服务端或杀毒软件正占着它）。
func lockExclusive(t *testing.T, path string) func() {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	return func() { once.Do(func() { windows.CloseHandle(h) }) }
}

// shortPath 一个已有路径的 8.3 短名写法；这个盘没开短名时原样返回。
func shortPath(p string) string {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) >= len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}
