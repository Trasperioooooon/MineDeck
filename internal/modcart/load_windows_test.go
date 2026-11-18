package modcart

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// lockExclusive 像杀毒、同步盘那样把文件独占打开（共享模式 0）：别人读、写、改名都不行（同 tunnel 包的 load_windows_test.go）。
// os.ReadFile 默认共享读写打开，只读属性、FILE_SHARE_READ 都挡不住它，只有独占才读不出来。返回放开的函数。
func lockExclusive(t *testing.T, path string) (unlock func()) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once // 可能从 time.AfterFunc 和 t.Cleanup 两处调
	unlock = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(unlock)
	return unlock
}

// 开机时 cart.json 被别的程序占着、几次重读都没读成：不当空清单，启动时不按空清单清暂存目录（以前这一步就把本地文件删了），
// 加入、移除、清空、添加和移除本地文件都报 LoadError。放开以后不用重启，下一次操作读回来，原来的和新加的都在。
//
// 只断言「cart.json 没变」测不出东西：独占的句柄同样挡住保存时的改名，旧代码也会落得原文件没变。
// 所以断言的是报错类型、暂存的文件还在，以及放开以后再加一项，原来那项还在（旧代码会把它盖掉）。
func TestCartLockedFileBlocksUntilReleased(t *testing.T) {
	c := seedCart(t)
	unlock := lockExclusive(t, c.path())
	s := New(c.dir, c.mr.srv.URL)
	le := wantLoadError(t, s.LoadErr(), false)
	if le.Err == nil {
		t.Fatal("应带上系统给的原因")
	}
	assertBlocked(t, s, c, false)
	unlock()
	if readText(t, c.path()) != c.json {
		t.Fatal("占着时 cart.json 被改了")
	}
	assertRecovered(t, s, c)
}

// 开机那一下刚好被占着、很快就放开了：启动时隔一小会儿的重读就读回来，暂存的文件照常按读回来的清单留着。
func TestCartLoadRetriesBrieflyAtStartup(t *testing.T) {
	c := seedCart(t)
	loadRetryWait, loadRetries = 150*time.Millisecond, 3
	unlock := lockExclusive(t, c.path())
	time.AfterFunc(30*time.Millisecond, unlock)
	s := New(c.dir, c.mr.srv.URL)
	if err := s.LoadErr(); err != nil || len(s.List(Draft)) != 1 || len(s.Locals(Draft)) != 1 {
		t.Fatalf("启动时的重读应已读回来：%v %v %v", err, s.List(Draft), s.Locals(Draft))
	}
	assertStagedIntact(t, c)
}

// 「文件不在」只认文件夹在、里面没有的那一种；路径里某一级不在不算。
func TestCartFileNotFoundOnlyForMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, missing := os.Lstat(filepath.Join(dir, "cart.json"))
	_, noPath := os.Lstat(filepath.Join(dir, "gone", "cart.json"))
	if !fileNotFound(missing) || fileNotFound(noPath) {
		t.Fatalf("文件不在：%v；路径不在：%v", missing, noPath)
	}
}
