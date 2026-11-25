package worldimport

import (
	"os"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// 别的程序占着文件、文件夹（见 locks_windows_test.go）。Mac 上没有独占打开，开着的文件也挡不住改名、替换，
// 用权限（读不了）加不可更改标记（chflags uchg：改不了、换不掉、删不掉，同一文件夹里别的东西照常）来模拟；
// 放开时改回原样，测试结束时也会改回去（不然临时文件夹删不掉）。

// hold unreadable 时把 path 的权限清零（读不了），再加上不可更改标记；先用 O_EVTONLY 拿到文件描述符，
// 后续加锁、解锁与清理都经 fd（Fchmod/Fchflags），避免上层目录被改名后按路径撤不掉标记导致临时目录清不干净。
func hold(t *testing.T, path string, unreadable bool) func() {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("没能占住 %s：%v", path, err)
	}
	fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("没能占住 %s：%v", path, err)
	}
	if unreadable {
		if err := unix.Fchmod(fd, 0); err != nil {
			_ = unix.Close(fd)
			t.Fatalf("没能占住 %s：%v", path, err)
		}
	}
	if err := unix.Fchflags(fd, unix.UF_IMMUTABLE); err != nil {
		if unreadable {
			_ = unix.Fchmod(fd, uint32(st.Mode().Perm()))
		}
		_ = unix.Close(fd)
		t.Fatalf("没能占住 %s：%v", path, err)
	}
	var once sync.Once
	undo := func() {
		once.Do(func() {
			_ = unix.Fchflags(fd, 0)
			if unreadable {
				_ = unix.Fchmod(fd, uint32(st.Mode().Perm()))
			}
			_ = unix.Close(fd)
		})
	}
	t.Cleanup(undo)
	return undo
}

// lockDirExclusive 占着的时候别人读不了这个文件夹，也挪不走、删不掉它。
func lockDirExclusive(t *testing.T, dir string) (unlock func()) {
	t.Helper()
	return hold(t, dir, true)
}

// lockFileExclusive 占着的时候别人读写不了这个文件，也换不掉、删不掉它。
func lockFileExclusive(t *testing.T, path string) (unlock func()) {
	t.Helper()
	return hold(t, path, true)
}

// lockFileNoWrite 别人还能读它，但改不了、换不掉。
func lockFileNoWrite(t *testing.T, path string) (unlock func()) {
	t.Helper()
	return hold(t, path, false)
}
