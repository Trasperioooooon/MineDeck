package inst

import (
	"strings"
	"testing"
	"time"
)

// fastSettle 把「等服务器稳定下来」的间隔调短。
func fastSettle(t *testing.T) {
	t.Helper()
	oldWait, oldPoll := backupSettleWait, backupSettlePoll
	backupSettleWait, backupSettlePoll = 10*time.Second, 5*time.Millisecond
	t.Cleanup(func() { backupSettleWait, backupSettlePoll = oldWait, oldPoll })
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
