package inst

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// fastSettle 把「等服务器稳定下来」的间隔调短（真的时钟）。
func fastSettle(t *testing.T) {
	t.Helper()
	oldWait, oldQuiet, oldPoll := backupSettleWait, backupSettleQuiet, backupSettlePoll
	backupSettleWait, backupSettleQuiet, backupSettlePoll = 10*time.Second, 20*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { backupSettleWait, backupSettleQuiet, backupSettlePoll = oldWait, oldQuiet, oldPoll })
}

// fakeSettleClock 让 waitSettled 用假的时钟：每「睡」一次时间往前走 backupSettlePoll，再调 step（第几次睡）。
// 等多久、中途状态怎么变都由测试一步一步定，不看机器快慢。返回现在的假时间。
func fakeSettleClock(t *testing.T, step func(n int)) func() time.Time {
	t.Helper()
	now := time.Date(2026, 11, 16, 4, 0, 0, 0, time.Local)
	n := 0
	oldNow, oldSleep := settleNow, settleSleep
	settleNow = func() time.Time { return now }
	settleSleep = func(d time.Duration) {
		now = now.Add(d)
		n++
		step(n)
	}
	t.Cleanup(func() { settleNow, settleSleep = oldNow, oldSleep })
	return func() time.Time { return now }
}

func setState(i *Instance, state string) {
	i.procMu.Lock()
	i.state = state
	i.procMu.Unlock()
}

func consoleHas(i *Instance, text string) bool {
	return strings.Contains(strings.Join(i.Console.Lines(), "\n"), text)
}

// 定时备份到点的时候服务器正在启动（比如排在定时重启后一两分钟）：不是报一句失败就算了，等它启动完再备份。
// 2026-11-14 起，服务器在启动或停止中时备份会被直接拒绝；定时备份原来不重试，这样排的服主每天都没有备份。
func TestScheduledBackupWaitsWhileServerIsStarting(t *testing.T) {
	m, _ := tempInstance(t)
	fastSettle(t)
	i, _ := m.Get("s")
	setState(i, "starting")
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.execSchedule(i, Schedule{Type: "backup", At: "04:02"})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !consoleHas(i, "等服务器启动或停止完再试") {
		if time.Now().After(deadline) {
			t.Fatalf("启动中的服务器，定时备份应当等一等再试。控制台：\n%s", strings.Join(i.Console.Lines(), "\n"))
		}
		select {
		case <-done:
			t.Fatalf("服务器还在启动，定时备份就结束了。控制台：\n%s", strings.Join(i.Console.Lines(), "\n"))
		case <-time.After(5 * time.Millisecond):
		}
	}
	if n := len(backupFiles(t, m)); n != 0 {
		t.Fatalf("服务器还在启动，不该已经有备份：%d 份", n)
	}
	setState(i, "") // 这里用「已停止」代表稳定下来（运行中的热备要真的服务器进程）
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("服务器稳定下来以后定时备份应当做完")
	}
	if n := len(backupFiles(t, m)); n != 1 || !consoleHas(i, "定时备份完成") {
		t.Fatalf("稳定下来以后应当有一份定时备份：%d 份。控制台：\n%s", n, strings.Join(i.Console.Lines(), "\n"))
	}
}

// 定时备份做到一半服务器被重启（备份和定时重启定在同一分钟、世界又大到一分钟压不完）：这一份作废，
// 等服务器稳定下来再备份一次。连着作废两次（第一次重试正好落在重启中间）也还有第三次。
func TestScheduledBackupRetriesWhenRestartVoidsIt(t *testing.T) {
	m, _ := tempInstance(t)
	fastSettle(t)
	i, _ := m.Get("s")
	calls := 0
	backupHook = func() {
		calls++
		if calls <= 2 { // 前两次：压缩期间服务器重启过
			i.procMu.Lock()
			i.runGen++
			i.procMu.Unlock()
		}
	}
	t.Cleanup(func() { backupHook = nil })
	m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
	if n := len(backupFiles(t, m)); calls != 3 || n != 1 || !consoleHas(i, "定时备份完成") {
		t.Fatalf("作废两次以后第三次应当成功：试了 %d 次，%d 份备份。控制台：\n%s", calls, n, strings.Join(i.Console.Lines(), "\n"))
	}

	// 一直不稳定：试到第三次为止，照实报失败，不留半截
	calls = 0
	backupHook = func() {
		calls++
		i.procMu.Lock()
		i.runGen++
		i.procMu.Unlock()
	}
	m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
	if n := len(backupFiles(t, m)); calls != 3 || n != 1 || !consoleHas(i, "定时备份失败") {
		t.Fatalf("一直被作废时应当试三次后报失败：试了 %d 次，%d 份备份。控制台：\n%s", calls, n, strings.Join(i.Console.Lines(), "\n"))
	}

	// 和启动、停止无关的失败不重试
	calls = 0
	backupHook = func() { calls++ }
	retries := func() int {
		return strings.Count(strings.Join(i.Console.Lines(), "\n"), "等服务器启动或停止完再试")
	}
	before := retries()
	i.Dir = i.Dir + "-没有这个文件夹" // 没有世界可备份
	m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
	if calls != 0 || retries() != before || !consoleHas(i, "还没有世界") {
		t.Fatalf("别的原因失败不该重试：进到压缩那一步 %d 次，重试 %d 次。控制台：\n%s", calls, retries()-before, strings.Join(i.Console.Lines(), "\n"))
	}
}

// 备份压到一半服务器的状态变了（有人点了停止，进程还没退出压缩就结束了）：这一份同样作废，不发布。
// 原来只比进程和运行代际，进程退出之前这两样都没变，后半截边停服存盘边压的备份照样发布。
func TestBackupVoidedWhenStateChangesMidway(t *testing.T) {
	m, _ := tempInstance(t)
	i, _ := m.Get("s")
	backupHook = func() { setState(i, "stopping") }
	t.Cleanup(func() { backupHook = nil })
	if _, err := m.CreateBackup("s", "手动"); err == nil || !strings.Contains(err.Error(), "作废") {
		t.Fatalf("压缩期间状态变了的备份应当作废：%v", err)
	}
	if n := len(backupFiles(t, m)); n != 0 {
		t.Fatalf("作废的备份不该发布：%d 份", n)
	}
	// 状态没变（空串和 stopped 是一回事）：照常
	setState(i, "")
	backupHook = func() { setState(i, "stopped") }
	if _, err := m.CreateBackup("s", "手动"); err != nil {
		t.Fatalf("状态没变的备份应当成功：%v", err)
	}
}

// 服务器只是一小会儿「已停止」（定时重启在停和启之间、「停止并还原」在停下和还原之间）：不在这时动手冷备，
// 要一直不变满 backupSettleQuiet 才备份；中途又启动、停止过就重新算。用假的时钟，一步一步走，不看机器快慢。
func TestScheduledBackupWaitsForQuietPeriod(t *testing.T) {
	m, _ := tempInstance(t)
	i, _ := m.Get("s")
	oldWait, oldQuiet, oldPoll := backupSettleWait, backupSettleQuiet, backupSettlePoll
	backupSettleWait, backupSettleQuiet, backupSettlePoll = 15*time.Minute, 30*time.Second, 2*time.Second
	t.Cleanup(func() { backupSettleWait, backupSettleQuiet, backupSettlePoll = oldWait, oldQuiet, oldPoll })
	var finalStop time.Time
	var now func() time.Time
	now = fakeSettleClock(t, func(n int) {
		switch n {
		case 3: // 停下来了
			setState(i, "")
		case 6: // 才 6 秒，又开始启动（定时重启的后半段）
			setState(i, "starting")
		case 20: // 启动失败停下了：从这一刻起再等满 30 秒
			setState(i, "")
			finalStop = now()
		}
	})
	setState(i, "starting")
	var backupAt time.Time
	backupHook = func() { backupAt = now() }
	t.Cleanup(func() { backupHook = nil })
	m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
	if backupAt.IsZero() || len(backupFiles(t, m)) != 1 {
		t.Fatalf("稳定下来以后应当备份一份。控制台：\n%s", strings.Join(i.Console.Lines(), "\n"))
	}
	if finalStop.IsZero() || backupAt.Sub(finalStop) < 30*time.Second {
		t.Fatalf("应当在最后一次停下以后满 30 秒才动手：停下在 %v，动手在 %v", finalStop.Format("15:04:05"), backupAt.Format("15:04:05"))
	}
}

// 程序正在退出（ShutdownAll 关服）、这台服务器被删掉或换成同名的另一台：等着重试的定时备份不再动手，照实报失败。
// 原来一看到「已停止」就冷备：退出时会在备份文件夹里留下半截的临时文件，换成同名的新服务器时会去备份那一台。
func TestScheduledBackupGivesUpOnShutdownOrReplacement(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(m *Manager, i *Instance)
	}{
		{"程序正在退出", func(m *Manager, _ *Instance) { m.closing.Store(true) }},
		{"换成了同名的另一台", func(m *Manager, i *Instance) {
			m.mu.Lock()
			m.insts["s"] = &Instance{Settings: Settings{Name: "s"}, Dir: i.Dir, Console: newConsole()}
			m.mu.Unlock()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := tempInstance(t)
			i, _ := m.Get("s")
			fakeSettleClock(t, func(n int) {
				if n == 1 {
					tc.do(m, i)
					setState(i, "") // 关服或删除都要先停下来
				}
			})
			setState(i, "starting")
			calls := 0
			backupHook = func() { calls++ }
			t.Cleanup(func() { backupHook = nil })
			m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
			if calls != 0 || len(backupFiles(t, m)) != 0 || !consoleHas(i, "定时备份失败") {
				t.Fatalf("不该再动手：进到压缩那一步 %d 次。控制台：\n%s", calls, strings.Join(i.Console.Lines(), "\n"))
			}
		})
	}
}

// 压到一半读文件出错，而这时服务器停止或重启过（停服时存盘会写临时文件再改名，列出来的文件转眼就不在了）：
// 按「服务器停止或重启过」算，定时备份等它稳定下来再试。原来这种直接报「压缩备份失败」，不重试，当天没有备份。
// 读文件出错、服务器却一直没变的，是真的读不了，不重试。
func TestScheduledBackupRetriesWhenZipFailsDuringRestart(t *testing.T) {
	for _, restarted := range []bool{true, false} {
		name := "期间重启过：重试"
		if !restarted {
			name = "服务器没变：照实报错"
		}
		t.Run(name, func(t *testing.T) {
			m, dir := tempInstance(t)
			fastSettle(t)
			i, _ := m.Get("s")
			var unlock func()
			calls := 0
			backupHook = func() {
				calls++
				if calls == 1 { // 第一次：世界里的文件被占着读不了
					unlock = lockExclusive(t, filepath.Join(dir, "world", "level.dat"))
					if restarted {
						i.procMu.Lock()
						i.runGen++
						i.procMu.Unlock()
					}
				} else if unlock != nil {
					unlock()
					unlock = nil
				}
			}
			t.Cleanup(func() {
				backupHook = nil
				if unlock != nil {
					unlock()
				}
			})
			m.execSchedule(i, Schedule{Type: "backup", At: "04:00"})
			if restarted {
				if calls != 2 || len(backupFiles(t, m)) != 1 || !consoleHas(i, "定时备份完成") {
					t.Fatalf("读文件出错时服务器重启过，应当等一等再备份成：试了 %d 次。控制台：\n%s", calls, strings.Join(i.Console.Lines(), "\n"))
				}
				return
			}
			if calls != 1 || len(backupFiles(t, m)) != 0 || !consoleHas(i, "压缩备份失败") {
				t.Fatalf("服务器没变时读不了就是读不了，不该重试：试了 %d 次。控制台：\n%s", calls, strings.Join(i.Console.Lines(), "\n"))
			}
		})
	}
}

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
