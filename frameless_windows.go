package main

// 无边框窗口（去掉系统标题栏，改由前端标题栏代替）。
// 手法：拦 WM_NCCALCSIZE，先让系统算出标准边框，再把客户区顶边恢复到窗口顶——
// 即「去掉标题栏、保留原生左右下缩放边框与 DWM 阴影/圆角」。WebView2 子窗口铺满客户区、
// 会吞掉命中测试，故拖动窗口与最小化/最大化/关闭改由 Bind 暴露给前端顶栏调用（见 main.go）。

import "unsafe"

const (
	_WM_NCCALCSIZE    = 0x0083
	_WM_NCLBUTTONDOWN = 0x00A1
	_HTCAPTION        = 2

	_SM_CYSIZEFRAME    = 33
	_SM_CXPADDEDBORDER = 92

	_SW_MAXIMIZE = 3
	_SW_MINIMIZE = 6

	_SWP_NOSIZE       = 0x0001
	_SWP_NOMOVE       = 0x0002
	_SWP_NOZORDER     = 0x0004
	_SWP_FRAMECHANGED = 0x0020
)

var (
	pDefWindowProcW         = user32t.NewProc("DefWindowProcW")
	pIsZoomed               = user32t.NewProc("IsZoomed")
	pReleaseCapture         = user32t.NewProc("ReleaseCapture")
	pSetWindowPos           = user32t.NewProc("SetWindowPos")
	pGetSystemMetrics       = user32t.NewProc("GetSystemMetrics")
	pGetDpiForWindow        = user32t.NewProc("GetDpiForWindow")
	pGetSystemMetricsForDpi = user32t.NewProc("GetSystemMetricsForDpi")
	pGetDoubleClickTime     = user32t.NewProc("GetDoubleClickTime")
)

type nccRect struct{ Left, Top, Right, Bottom int32 }

type nccalcsizeParams struct {
	Rgrc  [3]nccRect
	Lppos uintptr
}

// framelessNCCalcSize 处理 WM_NCCALCSIZE。返回 (ret, handled)；未接管时调用方转交默认过程。
func framelessNCCalcSize(hwnd, wparam, lparam uintptr) (ret uintptr, handled bool) {
	if wparam == 0 { // 非计算请求，交给默认过程
		return 0, false
	}
	// lparam 由系统指向一个 NCCALCSIZE_PARAMS，在本次消息处理期间有效。
	// go vet 会对 uintptr→unsafe.Pointer 报「possible misuse」——这是 WndProc 回调解引用参数的
	// 固有写法（go-webview2 处理 WM_GETMINMAXINFO 亦然），此处安全，为已知误报。
	p := (*nccalcsizeParams)(unsafe.Pointer(lparam)) //nolint:govet
	origTop := p.Rgrc[0].Top
	pDefWindowProcW.Call(hwnd, _WM_NCCALCSIZE, wparam, lparam) // 先算标准边框（含侧/底缩放边框）
	if isZoomed(hwnd) {
		// 最大化：窗口四边各越出屏幕一个缩放边框宽。默认过程会在顶部让出「边框+标题栏」，
		// 导致原生标题栏露出（与前端标题栏形成双顶栏）；只保留越屏的边框高，客户区顶恰好落在工作区顶。
		// 左/右/下沿用系统计算，任务栏/工作区适配不受影响。
		p.Rgrc[0].Top = origTop + maximizedTopInset(hwnd)
	} else {
		p.Rgrc[0].Top = origTop // 非最大化：客户区顶到窗口顶，抹掉标题栏
	}
	return 0, true
}

// maximizedTopInset 返回最大化时窗口顶边越出显示器的高度（缩放边框 + 附加边框）。
// 优先按窗口所在显示器 DPI 计算（Win10 1607+），避免高 DPI 屏上残留细缝或裁掉前端标题栏顶部。
func maximizedTopInset(hwnd uintptr) int32 {
	if pGetDpiForWindow.Find() == nil && pGetSystemMetricsForDpi.Find() == nil {
		if dpi, _, _ := pGetDpiForWindow.Call(hwnd); dpi != 0 {
			f, _, _ := pGetSystemMetricsForDpi.Call(_SM_CYSIZEFRAME, dpi)
			pad, _, _ := pGetSystemMetricsForDpi.Call(_SM_CXPADDEDBORDER, dpi)
			return int32(f) + int32(pad)
		}
	}
	f, _, _ := pGetSystemMetrics.Call(_SM_CYSIZEFRAME)
	pad, _, _ := pGetSystemMetrics.Call(_SM_CXPADDEDBORDER)
	return int32(f) + int32(pad)
}

func isZoomed(hwnd uintptr) bool {
	r, _, _ := pIsZoomed.Call(hwnd)
	return r != 0
}

// applyFrameless 子类化生效后触发一次边框重算，让去标题栏立即生效。
func applyFrameless(hwnd uintptr) {
	pSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, _SWP_NOMOVE|_SWP_NOSIZE|_SWP_NOZORDER|_SWP_FRAMECHANGED)
}

// hostStartDrag 从当前光标发起原生窗口拖动（前端顶栏空白处 mousedown 调用）。
// 系统的移动循环会收走这一下的松开键，网页因此收不到 dblclick；双击最大化由前端在 mousedown 里自己认
// （第二下改调 hostWinMaxToggle、不再发起拖动），双击间隔取 doubleClickTime。
func hostStartDrag(hwnd uintptr) {
	pReleaseCapture.Call()
	pSendMessageW.Call(hwnd, _WM_NCLBUTTONDOWN, _HTCAPTION, 0)
}

// doubleClickTime 系统设置里的双击间隔（毫秒，默认 500）。
func doubleClickTime() int {
	r, _, _ := pGetDoubleClickTime.Call()
	return int(r)
}

func hostMinimize(hwnd uintptr) { pShowWindow.Call(hwnd, _SW_MINIMIZE) }

// postWindowClose 请求关闭窗口（走 WM_CLOSE，交由托盘逻辑决定最小化到托盘或真正退出）。
func postWindowClose(hwnd uintptr) { pPostMessage.Call(hwnd, _WM_CLOSE, 0, 0) }

func hostMaximizeToggle(hwnd uintptr) {
	if isZoomed(hwnd) {
		pShowWindow.Call(hwnd, _SW_RESTORE)
	} else {
		pShowWindow.Call(hwnd, _SW_MAXIMIZE)
	}
}
