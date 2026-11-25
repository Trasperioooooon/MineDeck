//go:build windows

package hostdlg

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func swapPickersForTest(t *testing.T, native func(uintptr, string) (string, error), fallback func(string) (string, error)) {
	t.Helper()
	oldNative, oldFallback := nativePicker, fallbackPicker
	if native != nil {
		nativePicker = native
	}
	if fallback != nil {
		fallbackPicker = fallback
	}
	t.Cleanup(func() {
		nativePicker = oldNative
		fallbackPicker = oldFallback
	})
}

func TestPickFolderDispatchesOntoUIThread(t *testing.T) {
	uiDone := make(chan struct{})
	uiReady := make(chan uint32, 1)
	queue := make(chan func(), 4)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		uiReady <- windows.GetCurrentThreadId()
		for {
			select {
			case fn := <-queue:
				fn()
			case <-uiDone:
				return
			}
		}
	}()
	t.Cleanup(func() { close(uiDone) })

	uiTID := <-uiReady
	callerTID := make(chan uint32, 1)
	clear := SetDispatcher(uiTID, func(fn func()) { queue <- fn })
	t.Cleanup(clear)
	SetOwner(0x1234)
	t.Cleanup(func() { SetOwner(0) })

	swapPickersForTest(t, func(hwnd uintptr, title string) (string, error) {
		callerTID <- windows.GetCurrentThreadId()
		if hwnd != 0x1234 || title != "选服目录" {
			return "", errors.New("参数不一致")
		}
		return `D:\Servers\Demo`, nil
	}, func(string) (string, error) {
		t.Fatal("原生弹窗成功时不该走到 PowerShell 后备")
		return "", nil
	})

	got, err := PickFolder("选服目录", nil)
	if err != nil || got != `D:\Servers\Demo` {
		t.Fatalf("PickFolder 返回不符：path=%q err=%v", got, err)
	}
	if gotTID := <-callerTID; gotTID != uiTID {
		t.Fatalf("原生弹窗应在 UI 线程 %d 运行，实际线程为 %d", uiTID, gotTID)
	}
}

func TestPickFolderSelfCallOnUIThreadDoesNotDeadlock(t *testing.T) {
	// 若未放行「当前已在 UI 线程」，调进 dispatcher 后队列无人消费会卡在 doneCh；
	// 放在受试 goroutine 里限时 500ms 检测，卡住时先调 clear() 解开等待并报错。
	swapPickersForTest(t, func(uintptr, string) (string, error) {
		return `D:\Direct`, nil
	}, nil)

	type outcome struct {
		path string
		err  error
	}
	done := make(chan outcome, 1)
	dispatched := make(chan struct{}, 1)
	uiStopped := make(chan struct{})

	ready := make(chan func(), 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		uiTID := windows.GetCurrentThreadId()
		clear := SetDispatcher(uiTID, func(func()) {
			select {
			case dispatched <- struct{}{}:
			default:
			}
		})
		ready <- clear
		p, err := PickFolder("UI 线程直调", nil)
		done <- outcome{path: p, err: err}
		close(uiStopped)
	}()

	clearFn := <-ready
	t.Cleanup(func() {
		clearFn()
		<-uiStopped
	})

	select {
	case r := <-done:
		if r.err != nil || r.path != `D:\Direct` {
			t.Fatalf("直调结果不对：path=%q err=%v", r.path, r.err)
		}
	case <-time.After(500 * time.Millisecond):
		clearFn()
		t.Fatal("UI 线程自调发生死锁")
	}
	select {
	case <-dispatched:
		t.Fatal("已在 UI 线程上不应再走派发队列")
	default:
	}
}

func TestPickFolderFallbackRunsOffUIThreadWhenNativeFails(t *testing.T) {
	uiDone := make(chan struct{})
	uiReady := make(chan uint32, 1)
	queue := make(chan func(), 4)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		uiReady <- windows.GetCurrentThreadId()
		for {
			select {
			case fn := <-queue:
				fn()
			case <-uiDone:
				return
			}
		}
	}()
	t.Cleanup(func() { close(uiDone) })

	uiTID := <-uiReady
	clear := SetDispatcher(uiTID, func(fn func()) { queue <- fn })
	t.Cleanup(clear)

	nativeTID := make(chan uint32, 1)
	fallbackTID := make(chan uint32, 1)
	swapPickersForTest(t, func(uintptr, string) (string, error) {
		nativeTID <- windows.GetCurrentThreadId()
		return "", errors.New("COM 失败")
	}, func(string) (string, error) {
		fallbackTID <- windows.GetCurrentThreadId()
		return `D:\Fallback`, nil
	})

	got, err := PickFolder("后备测试", nil)
	if err != nil || got != `D:\Fallback` {
		t.Fatalf("后备结果不符：path=%q err=%v", got, err)
	}
	if gotTID := <-nativeTID; gotTID != uiTID {
		t.Fatalf("原生失败前也应在 UI 线程运行：got=%d want=%d", gotTID, uiTID)
	}
	if gotFallbackTID := <-fallbackTID; gotFallbackTID == uiTID {
		t.Fatalf("PowerShell 后备不应阻塞 UI 线程：%d", gotFallbackTID)
	}
}

func TestPickFolderUnblocksWhenDispatcherCleared(t *testing.T) {
	queued := make(chan struct{})
	clear := SetDispatcher(999999, func(func()) {
		close(queued) // 模拟消息循环已停，派发任务不会再被执行
	})

	swapPickersForTest(t, func(uintptr, string) (string, error) {
		t.Fatal("窗口关闭取消等待后不应执行原生弹窗")
		return "", nil
	}, func(string) (string, error) {
		t.Fatal("主窗口退出时不应再弹 PowerShell 后备框")
		return "", nil
	})

	type outcome struct {
		path string
		err  error
	}
	out := make(chan outcome, 1)
	go func() {
		p, err := PickFolder("退出测试", nil)
		out <- outcome{p, err}
	}()

	<-queued
	clear()
	clear() // 二次清理幂等安全

	select {
	case r := <-out:
		if !errors.Is(r.err, ErrUnavailable) || r.path != "" {
			t.Fatalf("清掉派发器后应返回 ErrUnavailable：path=%q err=%v", r.path, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clear() 未能唤醒等待中的 PickFolder")
	}
}
