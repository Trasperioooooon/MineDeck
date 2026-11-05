package main

// 静默启动（-minimized，随开机自启）时从头到尾不露出窗口。
// go-webview2 的 NewWithOptions 建完窗口立刻 ShowWindow(SW_SHOW)，之后才初始化 WebView2（要几百毫秒）；
// 等它返回再隐藏，用户已经看到一块没上色的白窗口一闪（测试报告 I-02）。
// 办法：建窗前给本线程挂一个 CBT 钩子，窗口一建出来（HCBT_CREATEWND）就子类化它，
// 在 WM_WINDOWPOSCHANGING 里去掉 SWP_SHOWWINDOW，那次显示就落空；NewWithOptions 返回后撤掉钩子、摘掉子类化。
// 之后从托盘恢复照常走 ShowWindow，不受影响。
//
// 正常启动也一样会露白：2026-11 在 2560×1440 的屏上逐帧截过，冷启动时先露出带系统标题栏的白窗口，
// 去掉标题栏以后还白着，前后约 0.8 秒，页面的启动画面才出来。所以正常启动用同一个钩子，只是不拦显示，
// 而是在第一次显示之前用 DWM 把窗口隐身（DWMWA_CLOAK）：窗口照常显示、能拿到焦点，只是看不见；
// 页面画出头两帧以后经 hostReady 叫 revealWindow 现身（main.go 另有 3 秒兜底）。实测改完以后，
// 冷启动约 0.85 秒时直接出现深色的无边框窗口，不再有白窗口和系统标题栏。

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	_WH_CBT               = 5
	_HCBT_CREATEWND       = 3
	_WM_WINDOWPOSCHANGING = 0x0046
	_SWP_SHOWWINDOW       = 0x0040
	_SWP_NOACTIVATE       = 0x0010 // _SWP_NOZORDER 在 frameless_windows.go
	_DWMWA_CLOAK          = 13
)

var (
	pSetWindowsHookEx    = user32t.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx = user32t.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx      = user32t.NewProc("CallNextHookEx")
	pGetClassName        = user32t.NewProc("GetClassNameW")
	pGetWindowLongPtr    = user32t.NewProc("GetWindowLongPtrW")

	hiddenClass    string  // 要藏起来的窗口类名（go-webview2 的是 "webview"）
	hiddenHook     uintptr // CBT 钩子句柄
	hiddenHWND     uintptr // 钩子逮到的窗口
	hiddenPrev     uintptr // 子类化前的窗口过程
	hiddenSuppress bool    // 正在拦显示（cloak 模式下是：正在等第一次显示）
	hiddenCloak    bool    // 正常启动：不拦显示，第一次显示前隐身
	hiddenCloaked  bool    // 窗口现在是隐身的（等 revealWindow）
	hiddenHookCB   = syscall.NewCallback(hiddenHookProc)
	hiddenWndCB    = syscall.NewCallback(hiddenWndProc)
)

// WINDOWPOS（64 位布局）
type windowPos struct {
	HWnd, HWndInsertAfter uintptr
	X, Y, CX, CY          int32
	Flags                 uint32
}

// beginHiddenCreate 挂钩子：此后本线程建出的第一个 className 类窗口不会被显示出来，直到调用返回的 end。
// cloak 为 true 时（正常启动）不拦显示，而是在第一次显示之前把窗口隐身，要等 revealWindow 才看得见。
// 须在建窗的线程上调用（openWebView 已 LockOSThread）。钩子挂不上时 end 什么也不做，调用方照旧事后隐藏
// （cloak 模式下就是照旧露白，不会隐身不现）。end 可重复调用，不会让隐身的窗口现身。
func beginHiddenCreate(className string, cloak bool) (end func()) {
	hiddenClass, hiddenHWND, hiddenPrev, hiddenSuppress = className, 0, 0, false
	hiddenCloak, hiddenCloaked = cloak, false
	hiddenHook, _, _ = pSetWindowsHookEx.Call(_WH_CBT, hiddenHookCB, 0, uintptr(windows.GetCurrentThreadId()))
	return func() {
		if hiddenHook != 0 {
			pUnhookWindowsHookEx.Call(hiddenHook)
			hiddenHook = 0
		}
		hiddenSuppress = false
		if hiddenHWND == 0 || hiddenPrev == 0 {
			return
		}
		// 窗口过程还是我们的才摘掉（中间没人再子类化过），否则留着当直通，不打断别人的链
		idx := _GWLP_WNDPROC
		if cur, _, _ := pGetWindowLongPtr.Call(hiddenHWND, uintptr(idx)); cur == hiddenWndCB {
			pSetWindowLongPtr.Call(hiddenHWND, uintptr(idx), hiddenPrev)
		}
		hiddenHWND, hiddenPrev = 0, 0 // 再调一次（defer）时不再碰这个窗口，那时它可能已经销毁了
	}
}

// hiddenHookProc CBT 钩子：逮住目标窗口的创建，立刻子类化。其余一律放行。
func hiddenHookProc(code, wparam, lparam uintptr) (ret uintptr) {
	defer func() { _ = recover() }() // 回调经 C 边界调入，panic 不能漏出去
	if int32(code) == _HCBT_CREATEWND && hiddenHWND == 0 && windowClassName(wparam) == hiddenClass {
		idx := _GWLP_WNDPROC
		if prev, _, _ := pSetWindowLongPtr.Call(wparam, uintptr(idx), hiddenWndCB); prev != 0 {
			hiddenHWND, hiddenPrev, hiddenSuppress = wparam, prev, true
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, code, wparam, lparam)
	return r
}

// hiddenWndProc 临时窗口过程：拦显示期间去掉 SWP_SHOWWINDOW（cloak 模式下改成显示前先隐身），其余原样转交。
func hiddenWndProc(hwnd, msg, wparam, lparam uintptr) (ret uintptr) {
	defer func() { _ = recover() }()
	if uint32(msg) == _WM_WINDOWPOSCHANGING && hiddenSuppress && lparam != 0 {
		// lparam 由系统指向一个 WINDOWPOS，在本次消息处理期间有效；经 &lparam 取指针，免得 go vet 误报 unsafe.Pointer
		wp := *(**windowPos)(unsafe.Pointer(&lparam))
		switch {
		case !hiddenCloak:
			wp.Flags = wp.Flags&^_SWP_SHOWWINDOW | _SWP_NOACTIVATE | _SWP_NOZORDER // 也不激活、不调层次：开机时不抢用户正在用的窗口的焦点
		case wp.Flags&_SWP_SHOWWINDOW != 0:
			hiddenCloaked = setCloak(hwnd, true)
			hiddenSuppress = false // 只管第一次显示
		}
	}
	r, _, _ := pCallWindowProc.Call(hiddenPrev, hwnd, msg, wparam, lparam)
	return r
}

// setCloak 用 DWM 让窗口隐身或现身（Windows 8 起）。返回调用是否成功。
func setCloak(hwnd uintptr, on bool) bool {
	var v int32
	if on {
		v = 1
	}
	r, _, _ := pDwmSetWindowAttr.Call(hwnd, _DWMWA_CLOAK, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	return r == 0 // S_OK
}

// revealWindow 冷启动时隐身的窗口现身（页面要画第一帧了，或者兜底超时）。只在窗口线程调用；没隐身时什么也不做，可重复调用。
func revealWindow(hwnd uintptr) {
	if hiddenCloaked {
		setCloak(hwnd, false)
		hiddenCloaked = false
	}
}

func windowClassName(hwnd uintptr) string {
	var buf [64]uint16
	n, _, _ := pGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}
