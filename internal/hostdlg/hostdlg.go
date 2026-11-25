// Package hostdlg 在这台电脑的桌面上弹系统对话框（目前只有「选择文件夹」）。
//
// 以前是起一个 PowerShell 去调 WinForms 的 FolderBrowserDialog：MineDeck 是窗口程序，PowerShell 是控制台程序，
// 于是每次都多冒出一个黑色的终端窗口；把那个黑窗关掉，PowerShell 被杀，界面上报的是「exit status 0xc000013a」。
// 现在先用系统自带的新式对话框（IFileOpenDialog，就是资源管理器那种带地址栏的，挂在 MineDeck 窗口上）；
// 这条路走不通时（极少见：COM 初始化不了）退回 PowerShell 那一套，但不再带控制台窗口。
// 取消不算错（返回空路径）；真打不开时返回 ErrUnavailable，原因写进日志，界面上只说人话。
package hostdlg

import (
	"errors"
	"sync"
	"sync/atomic"
)

// ErrBusy 已经有一个对话框开着（上一次点的还没选完）。
var ErrBusy = errors.New("选择文件夹的窗口已经开着了，先在那个窗口里选好，或者把它关掉")

// ErrUnavailable 对话框打不开。具体原因在日志里。
var ErrUnavailable = errors.New("没能打开选择文件夹的窗口，请再试一次")

var (
	owner          atomic.Uintptr // MineDeck 主窗口的句柄；浏览器模式（-nogui）下是 0
	busy           sync.Mutex
	dispMu         sync.Mutex
	activeDisp     *dispatcher
	nativePicker   = pickFolderNative
	fallbackPicker = pickFolderFallback
)

type dispatcher struct {
	uiThreadID uint32
	fn         func(func())
	stopped    chan struct{}
	stopOnce   sync.Once
}

// SetOwner 记下主窗口的句柄：对话框挂在它上面，开着的时候主窗口点不动，也不会被主窗口盖住。
func SetOwner(hwnd uintptr) { owner.Store(hwnd) }

// SetDispatcher 注册主窗口 UI 线程的派发入口。非 UI 线程调用 PickFolder 时，
// 原生系统对话框会切回 UI 线程执行，避免跨线程持有 hwndOwner 时模态弹窗卡死或失焦。
// 返回的 clear 应在主窗口消息循环退出时调用，以解除绑定并唤醒仍在等待的调用。
func SetDispatcher(uiThreadID uint32, dispatch func(func())) (clear func()) {
	var d *dispatcher
	if dispatch != nil {
		d = &dispatcher{
			uiThreadID: uiThreadID,
			fn:         dispatch,
			stopped:    make(chan struct{}),
		}
	}
	dispMu.Lock()
	prev := activeDisp
	activeDisp = d
	dispMu.Unlock()
	if prev != nil {
		prev.stop()
	}
	if d == nil {
		return func() {}
	}
	return func() {
		dispMu.Lock()
		if activeDisp == d {
			activeDisp = nil
		}
		dispMu.Unlock()
		d.stop()
	}
}

func (d *dispatcher) stop() {
	d.stopOnce.Do(func() { close(d.stopped) })
}

func currentDispatcher() *dispatcher {
	dispMu.Lock()
	d := activeDisp
	dispMu.Unlock()
	return d
}

// callNativePicker 在登记了 UI 派发器且当前不在 UI 线程上时，将原生弹窗切到 UI 线程执行；
// 若本身已在 UI 线程或未登记派发器（-nogui / 单元测试），则直接在当前线程打开以免自锁。
func callNativePicker(ownerHWND uintptr, title string) (path string, stopped bool, err error) {
	d := currentDispatcher()
	if d == nil || (d.uiThreadID != 0 && currentThreadID() == d.uiThreadID) {
		p, e := nativePicker(ownerHWND, title)
		return p, false, e
	}
	type nativeRes struct {
		path string
		err  error
	}
	doneCh := make(chan nativeRes, 1)
	d.fn(func() {
		p, e := nativePicker(ownerHWND, title)
		doneCh <- nativeRes{path: p, err: e}
	})
	select {
	case r := <-doneCh:
		return r.path, false, r.err
	case <-d.stopped:
		select {
		case r := <-doneCh:
			return r.path, false, r.err
		default:
			return "", true, nil
		}
	}
}

// PickFolder 弹出「选择文件夹」，返回选中的目录；用户取消时返回空串、不报错。
// 同一时间只开一个：上一个还没关就再调，返回 ErrBusy。logf 用来记打不开的原因（可以是 nil）。
func PickFolder(title string, logf func(format string, a ...any)) (string, error) {
	if !busy.TryLock() {
		return "", ErrBusy
	}
	defer busy.Unlock()
	if logf == nil {
		logf = func(string, ...any) {}
	}
	path, stopped, err := callNativePicker(owner.Load(), title)
	if stopped {
		logf("选择文件夹：主窗口已退出，取消等待弹窗")
		return "", ErrUnavailable
	}
	if err == nil {
		return path, nil
	}
	logf("选择文件夹：系统对话框打不开（%v），改用 PowerShell", err)
	path, err = fallbackPicker(title)
	if err != nil {
		logf("选择文件夹：PowerShell 这条路也没走通：%v", err)
		return "", ErrUnavailable
	}
	return path, nil
}
