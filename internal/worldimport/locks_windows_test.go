package worldimport

import (
	"sync"
	"testing"

	"golang.org/x/sys/windows"
)

// lockDirExclusive 像别的程序独占着一个文件夹那样把它打开（不共享），返回放开它的函数。占着的时候别人读不了它。
func lockDirExclusive(t *testing.T, dir string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("没能独占打开 %s：%v", dir, err)
	}
	var once sync.Once
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

// lockFileExclusive 像别的程序独占着一个文件那样把它打开（不共享），返回放开它的函数。
func lockFileExclusive(t *testing.T, path string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("没能独占打开 %s：%v", path, err)
	}
	var once sync.Once
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

// lockFileNoWrite 像别的程序开着一个文件那样：别人还能读它，但改不了、换不掉。返回放开的函数。
func lockFileNoWrite(t *testing.T, path string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatalf("没能打开 %s：%v", path, err)
	}
	var once sync.Once
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}
