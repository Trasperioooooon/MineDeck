package tunnel

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"minedeck/internal/app"
)

// lockExclusive 像杀毒、同步盘那样把文件独占打开（共享模式 0）：别人读、写、改名都不行。
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

// 开机时 tunnels.json 被别的程序占着、几次重读都没读成：不当空列表，新增、修改、删除、启动报 LoadError，
// 原文件不动。放开以后不用重启，下一次操作读回来，再加一条，旧的和新的都在。
//
// 只断言「原文件没变」测不出东西：独占的句柄同样挡住保存时的改名，旧代码也会落得原文件没变。
// 所以要断言报的是 LoadError（旧代码这里报 SaveError），以及放开以后再加，原来那条还在（旧代码会把它盖掉）。
func TestLoadLockedFileBlocksSavesUntilReleased(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	before := seedTunnels(t)
	unlock := lockExclusive(t, tunnelsPath())
	m := NewManager()
	le := wantLoadError(t, m.LoadErr(), false)
	if le.Err == nil {
		t.Fatal("应带上系统给的原因")
	}
	// 断言时读文件要先放开：assertBlocked 最后会读原文件，独占时读不出来。先把需要锁着做的几件做完
	got, err := m.Add(openfrpTunnel("二号"))
	wantLoadError(t, err, false)
	if got != nil || m.seq != 0 || len(m.tunnels) != 0 {
		t.Fatalf("占着时内存被改了：%+v seq=%d n=%d", got, m.seq, len(m.tunnels))
	}
	wantLoadError(t, m.Update("tun1", openfrpTunnel("改名")), false)
	wantLoadError(t, m.Delete("tun1"), false)
	wantLoadError(t, m.Start("tun1"), false)
	unlock()

	if after := readFile(t, tunnelsPath()); after != before {
		t.Fatalf("占着时原文件被改了：\n前：%s\n后：%s", before, after)
	}
	assertRecovered(t, m)
}

// 路径在两次检查之间又失联（Codex 第二轮复审 P1）：读的时候是「文件夹在、文件不在」，查程序文件夹也看得到，
// 再看文件时整个文件夹又不见了，报的是「路径找不到」。这不能当新装放行，不然路径恢复以后新增一条就把原文件盖掉。
// 用 beforeFileRecheck 在那个空当里把原文件放回去、再把整个程序文件夹挪走。
func TestLoadPathLostBetweenChecks(t *testing.T) {
	tempBase(t)
	fastLoadRetry(t)
	before := seedTunnels(t)
	base := app.Base
	away := base + "-away"
	aside := filepath.Join(base, "tunnels.json.aside")
	if err := os.Rename(tunnelsPath(), aside); err != nil { // 第一次读：文件夹在、文件不在
		t.Fatal(err)
	}
	t.Cleanup(func() { beforeFileRecheck = nil })
	beforeFileRecheck = func() {
		beforeFileRecheck = nil // 只在第一次检查时动手
		if err := os.Rename(aside, filepath.Join(base, "tunnels.json")); err != nil {
			t.Error(err)
		}
		if err := os.Rename(base, away); err != nil { // 再看文件时路径失联
			t.Error(err)
		}
	}
	m := NewManager()
	wantLoadError(t, m.LoadErr(), false)
	if err := os.Rename(away, base); err != nil { // 路径恢复
		t.Fatal(err)
	}
	if got := readFile(t, tunnelsPath()); got != before {
		t.Fatal("原文件被动了")
	}
	assertRecovered(t, m)
}

// 「文件不在」只认文件夹在、里面没有这个文件的那一种；路径里某一级不在不算。
func TestFileNotFoundOnlyForMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, missing := os.Lstat(filepath.Join(dir, "tunnels.json"))
	_, noPath := os.Lstat(filepath.Join(dir, "gone", "tunnels.json"))
	if !fileNotFound(missing) || fileNotFound(noPath) {
		t.Fatalf("文件不在：%v；路径不在：%v", missing, noPath)
	}
}

// 开机那一下刚好被占着、很快就放开了：启动时隔一小会儿的重读就读回来，不用等到有人操作。
func TestLoadRetriesBrieflyAtStartup(t *testing.T) {
	tempBase(t)
	oldWait, oldN := loadRetryWait, loadRetries
	t.Cleanup(func() { loadRetryWait, loadRetries = oldWait, oldN })
	loadRetryWait, loadRetries = 150*time.Millisecond, 3
	seedTunnels(t)
	unlock := lockExclusive(t, tunnelsPath())
	time.AfterFunc(30*time.Millisecond, unlock)
	m := NewManager()
	m.mu.Lock()
	err, n := m.loadErr, len(m.tunnels)
	m.mu.Unlock()
	if err != nil || n != 1 {
		t.Fatalf("启动时的重读应已读回来：err=%v n=%d", err, n)
	}
}
