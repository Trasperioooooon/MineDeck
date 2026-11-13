package main

// 窗口的最小尺寸：以前能一直拖小到界面挤成一团。现在最小是 1024×576（16:9，逻辑像素），
// 按窗口所在显示器的 DPI 放大；显示器的工作区比这还小时（小屏配高缩放）收到工作区那么大，免得窗口比屏幕还大。
// 只管「最小能拖到多小」，不锁宽高比：窗口照样可以拖成任意比例。
//
// 做法：在子类化的窗口过程里接 WM_GETMINMAXINFO（见 tray_windows.go），先让 go-webview2 的原过程填默认值，
// 再改 ptMinTrackSize。每次拖动系统都会来问，所以窗口挪到另一块 DPI 不同的屏幕上也跟着变。
// 没用 go-webview2 的 SetSize(…, HintMin)：它只在调用那一刻存一个固定的物理像素值，换屏幕后就不对了。

import "unsafe"

const (
	_WM_GETMINMAXINFO = 0x0024

	_MONITOR_DEFAULTTONEAREST = 2

	// 最小窗口（逻辑像素，96 DPI 下的大小）。宽 1024 还在「侧栏完整显示」的范围里（960 及以下侧栏收成图标栏）。
	minWinW = 1024
	minWinH = 576
)

var (
	pMonitorFromWindow = user32t.NewProc("MonitorFromWindow")
	pGetMonitorInfo    = user32t.NewProc("GetMonitorInfoW")
)

// minMaxInfo 即 Win32 的 MINMAXINFO。
type minMaxInfo struct {
	Reserved     point
	MaxSize      point
	MaxPosition  point
	MinTrackSize point
	MaxTrackSize point
}

// monitorInfo 即 Win32 的 MONITORINFO。
type monitorInfo struct {
	CbSize  uint32
	Monitor nccRect
	Work    nccRect
	Flags   uint32
}

// minTrackSize 算出最小窗口的物理像素大小：逻辑尺寸按 dpi 放大，再收到工作区以内。
// dpi 取不到（0）按 96 算；workW、workH 取不到（≤0）就不收。
func minTrackSize(dpi uint32, workW, workH int32) (w, h int32) {
	if dpi == 0 {
		dpi = 96
	}
	w = int32((int64(minWinW)*int64(dpi) + 48) / 96)
	h = int32((int64(minWinH)*int64(dpi) + 48) / 96)
	if workW > 0 && w > workW {
		w = workW
	}
	if workH > 0 && h > workH {
		h = workH
	}
	return w, h
}

// windowDPI 窗口所在显示器的 DPI（Win10 1607+）；取不到返回 0。
func windowDPI(hwnd uintptr) uint32 {
	if pGetDpiForWindow.Find() != nil {
		return 0
	}
	dpi, _, _ := pGetDpiForWindow.Call(hwnd)
	return uint32(dpi)
}

// workAreaSize 窗口所在显示器的工作区（去掉任务栏）大小；取不到返回 0, 0。
func workAreaSize(hwnd uintptr) (w, h int32) {
	mon, _, _ := pMonitorFromWindow.Call(hwnd, _MONITOR_DEFAULTTONEAREST)
	if mon == 0 {
		return 0, 0
	}
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return 0, 0
	}
	return mi.Work.Right - mi.Work.Left, mi.Work.Bottom - mi.Work.Top
}

// applyMinTrackSize 处理 WM_GETMINMAXINFO：原过程填完之后调用，把最小可拖动尺寸改成上面算出来的。
func applyMinTrackSize(hwnd, lparam uintptr) {
	if lparam == 0 {
		return
	}
	// lparam 由系统指向一个 MINMAXINFO，本次消息处理期间有效。经 &lparam 取指针，免得 go vet 报
	// 「possible misuse of unsafe.Pointer」（同 frameless_windows.go 里对 WM_NCCALCSIZE 的写法，缘由写在那里）。
	mmi := *(**minMaxInfo)(unsafe.Pointer(&lparam))
	workW, workH := workAreaSize(hwnd)
	w, h := minTrackSize(windowDPI(hwnd), workW, workH)
	mmi.MinTrackSize = point{X: w, Y: h}
}
