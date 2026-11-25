//go:build windows

package hostdlg

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
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
	var queuedOnce sync.Once
	// 派发可能被调好几次（等着的时候会补发唤醒），只在第一次通知
	clear := SetDispatcher(999999, func(func()) {
		queuedOnce.Do(func() { close(queued) }) // 模拟消息循环已停，派发任务不会再被执行
	})
	t.Cleanup(clear)

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

	select {
	case <-queued:
	case <-time.After(2 * time.Second):
		t.Fatal("PickFolder 没有把弹窗派发出去")
	}
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

// fakeUILoop 照 go-webview2 的 Dispatch 做的假 UI 线程：回调排进队列，另外发一个「唤醒」（真的是一条线程消息）；
// 循环收到唤醒才把队列整个取出来执行。唤醒可以丢：系统的模态循环（对话框、拖动窗口、托盘菜单）会把线程消息吞掉，
// 回调就一直排在队列里，等下一次有人派发才跟着执行。
type fakeUILoop struct {
	tid   uint32
	mu    sync.Mutex
	queue []func()
	drop  int // 接下来要丢掉几次唤醒
	calls atomic.Int32
	wake  chan struct{}
}

func startFakeUILoop(t *testing.T) *fakeUILoop {
	t.Helper()
	l := &fakeUILoop{wake: make(chan struct{}, 256)}
	ready := make(chan struct{})
	quit := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(exited)
		l.tid = windows.GetCurrentThreadId()
		close(ready)
		for {
			select {
			case <-l.wake:
				l.mu.Lock()
				q := l.queue
				l.queue = nil
				l.mu.Unlock()
				for _, fn := range q {
					fn()
				}
			case <-quit:
				return
			}
		}
	}()
	<-ready
	t.Cleanup(func() {
		close(quit)
		<-exited
	})
	return l
}

func (l *fakeUILoop) dispatch(fn func()) {
	l.calls.Add(1)
	l.mu.Lock()
	l.queue = append(l.queue, fn)
	lost := l.drop > 0
	if lost {
		l.drop--
	}
	l.mu.Unlock()
	if lost {
		return
	}
	select {
	case l.wake <- struct{}{}:
	default: // 线程消息队列满了也是丢
	}
}

// swallowWakes 模拟对话框的模态循环：把这时候到的唤醒都取走、什么也不做。只在假 UI 线程上调。
func (l *fakeUILoop) swallowWakes() {
	for {
		select {
		case <-l.wake:
		default:
			return
		}
	}
}

func setRewakeIntervalForTest(t *testing.T, d time.Duration) {
	t.Helper()
	old := rewakeInterval
	rewakeInterval = d
	t.Cleanup(func() { rewakeInterval = old })
}

// 对话框开着时别处派发的回调（绑定函数的回应、打开失败的提示……），它们的唤醒被对话框的模态循环吞了；
// 对话框关掉以后要补派发一次，把它们冲出来，不能等到下一次有人派发。
func TestPickFolderFlushesDispatchesHeldByDialog(t *testing.T) {
	setRewakeIntervalForTest(t, time.Hour) // 只测关掉以后的补发，别让等待时的重发顺手把队列冲掉
	l := startFakeUILoop(t)
	clear := SetDispatcher(l.tid, l.dispatch)
	t.Cleanup(clear)

	heldRan := make(chan struct{})
	swapPickersForTest(t, func(uintptr, string) (string, error) {
		l.dispatch(func() { close(heldRan) })
		l.swallowWakes()
		return `D:\Picked`, nil
	}, nil)

	got, err := PickFolder("补发测试", nil)
	if err != nil || got != `D:\Picked` {
		t.Fatalf("PickFolder 返回不符：path=%q err=%v", got, err)
	}
	select {
	case <-heldRan:
	case <-time.After(2 * time.Second):
		t.Fatal("对话框开着时派发的回调，对话框关掉以后一直没执行")
	}
}

// 派发弹窗那一下 UI 线程正在别的模态循环里（拖动窗口、托盘菜单），唤醒丢了：等着的一方要隔一会儿补发唤醒，
// 不能拿着 busy 一直等；回调开始执行（对话框弹出来）以后就不再补发。
func TestPickFolderRewakesWhenFirstWakeIsLost(t *testing.T) {
	const interval = 20 * time.Millisecond
	setRewakeIntervalForTest(t, interval)
	l := startFakeUILoop(t)
	l.drop = 1
	clear := SetDispatcher(l.tid, l.dispatch)
	t.Cleanup(clear)

	var callsWhileOpen atomic.Int32
	swapPickersForTest(t, func(uintptr, string) (string, error) {
		time.Sleep(2 * interval) // 先让等着的一方看到「已开始」：刚开始那一下和补发撞上，多发一次不算错
		before := l.calls.Load()
		time.Sleep(10 * interval) // 对话框开着
		callsWhileOpen.Store(l.calls.Load() - before)
		return `D:\Woken`, nil
	}, nil)

	type outcome struct {
		path string
		err  error
	}
	out := make(chan outcome, 1)
	go func() {
		p, err := PickFolder("重发测试", nil)
		out <- outcome{p, err}
	}()
	select {
	case r := <-out:
		if r.err != nil || r.path != `D:\Woken` {
			t.Fatalf("PickFolder 返回不符：path=%q err=%v", r.path, r.err)
		}
	case <-time.After(2 * time.Second):
		clear() // 放开卡住的 PickFolder，不然它一直占着 busy，后面的用例都会报 ErrBusy
		<-out
		t.Fatal("派发弹窗的唤醒丢了以后 PickFolder 一直在等，对话框没有弹出来")
	}
	if n := callsWhileOpen.Load(); n != 0 {
		t.Errorf("对话框弹出来以后不该再补发唤醒，开着期间补发了 %d 次", n)
	}
}

// 派发器被清掉（主窗口退出）以后，等着的一方返回，补发唤醒也跟着停，不留一直在跑的东西。
func TestPickFolderStopsRewakingWhenDispatcherCleared(t *testing.T) {
	const interval = 10 * time.Millisecond
	setRewakeIntervalForTest(t, interval)
	var calls atomic.Int32
	clear := SetDispatcher(999999, func(func()) { calls.Add(1) }) // 消息循环已停：派发什么都不会执行
	t.Cleanup(clear)
	swapPickersForTest(t, func(uintptr, string) (string, error) {
		t.Fatal("派发出去的弹窗不该被执行")
		return "", nil
	}, nil)

	out := make(chan error, 1)
	go func() {
		_, err := PickFolder("停止重发测试", nil)
		out <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(interval)
	}
	if calls.Load() < 3 {
		clear()
		<-out
		t.Fatalf("等着的时候应该隔一会儿补发唤醒，2 秒内只派发了 %d 次", calls.Load())
	}
	clear()
	select {
	case err := <-out:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("清掉派发器后应返回 ErrUnavailable，实际 %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clear() 未能唤醒等待中的 PickFolder")
	}
	after := calls.Load()
	time.Sleep(10 * interval)
	if n := calls.Load() - after; n != 0 {
		t.Errorf("派发器清掉以后还在补发唤醒：又派发了 %d 次", n)
	}
}
