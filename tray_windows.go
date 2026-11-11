package main

// 系统托盘 + 关窗处理：子类化 go-webview2 的窗口过程拦截 WM_CLOSE（最小化到托盘，或有服务器在运行时先确认），
// 挂 Shell_NotifyIcon 托盘图标与右键菜单。所有 Win32 调用均在创建窗口的 UI 线程（消息循环线程）上执行。

import (
	"syscall"
	"time"
	"unsafe"

	"minedeck/internal/app"

	"golang.org/x/sys/windows"
)

const (
	_GWLP_WNDPROC = -4

	_WM_CLOSE   = 0x0010
	_WM_DESTROY = 0x0002
	_WM_APP     = 0x8000

	_WM_LBUTTONUP     = 0x0202
	_WM_LBUTTONDBLCLK = 0x0203
	_WM_RBUTTONUP     = 0x0205

	// 自定义消息
	_WM_TRAYCALLBACK = _WM_APP + 1 // 托盘图标鼠标事件回调
	_WM_APP_QUIT     = _WM_APP + 2 // 线程安全地请求真正退出（供 shutdown / 托盘菜单用）

	_SW_HIDE    = 0
	_SW_SHOW    = 5
	_SW_RESTORE = 9

	_NIM_ADD     = 0x0
	_NIM_MODIFY  = 0x1
	_NIM_DELETE  = 0x2
	_NIF_MESSAGE = 0x1
	_NIF_ICON    = 0x2
	_NIF_TIP     = 0x4

	_MF_STRING       = 0x0
	_TPM_RIGHTBUTTON = 0x2
	_TPM_RETURNCMD   = 0x100

	_IDI_APPLICATION = 32512

	_idTrayOpen = 1
	_idTrayQuit = 2
	_trayUID    = 1
)

var (
	user32t = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	pSetWindowLongPtr      = user32t.NewProc("SetWindowLongPtrW")
	pCallWindowProc        = user32t.NewProc("CallWindowProcW")
	pShowWindow            = user32t.NewProc("ShowWindow")
	pIsWindowVisible       = user32t.NewProc("IsWindowVisible")
	pIsIconic              = user32t.NewProc("IsIconic")
	pSetForegroundWin      = user32t.NewProc("SetForegroundWindow")
	pDestroyWindow         = user32t.NewProc("DestroyWindow")
	pPostMessage           = user32t.NewProc("PostMessageW")
	pLoadIcon              = user32t.NewProc("LoadIconW")
	pCreatePopupMenu       = user32t.NewProc("CreatePopupMenu")
	pAppendMenu            = user32t.NewProc("AppendMenuW")
	pTrackPopupMenu        = user32t.NewProc("TrackPopupMenu")
	pDestroyMenu           = user32t.NewProc("DestroyMenu")
	pGetCursorPos          = user32t.NewProc("GetCursorPos")
	pRegisterWindowMessage = user32t.NewProc("RegisterWindowMessageW")
	pShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")

	origWndProc       uintptr
	trayHWND          uintptr
	trayHIcon         uintptr
	trayActive        bool    // 托盘图标是否登记成功（NIM_ADD 返回真）
	taskbarCreatedMsg uintptr // Explorer 重启后广播的消息，用于重新登记图标
	trayCallback      = syscall.NewCallback(trayWndProc)
)

// NOTIFYICONDATAW（Vista+ 完整布局）；cbSize 取本结构体大小，Windows 据此识别版本。
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type point struct{ X, Y int32 }

// setupTray 子类化 webview 窗口过程并挂上托盘图标。须在创建窗口的 UI 线程调用。
func setupTray(hwnd uintptr) {
	trayHWND = hwnd
	trayHIcon = loadAppIcon(16) // 品牌图标（16px 适配托盘）
	if trayHIcon == 0 {
		trayHIcon, _, _ = pLoadIcon.Call(0, _IDI_APPLICATION) // 兜底：通用应用图标
	}
	idx := _GWLP_WNDPROC // int(-4)；uintptr(idx) 在 64 位上正确符号扩展
	origWndProc, _, _ = pSetWindowLongPtr.Call(hwnd, uintptr(idx), trayCallback)
	if origWndProc == 0 {
		// 子类化失败（返回 0 即未替换成功）：放弃托盘功能，避免后续 CallWindowProc(0) 破坏窗口
		trayHWND = 0
		return
	}
	if p, err := syscall.UTF16PtrFromString("TaskbarCreated"); err == nil {
		taskbarCreatedMsg, _, _ = pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(p)))
	}
	addTrayIcon(hwnd)
}

// trayWndProc 子类化后的窗口过程：拦截关窗/托盘事件/退出请求，其余转交原过程。
func trayWndProc(hwnd, msg, wparam, lparam uintptr) (ret uintptr) {
	// 防御：回调经由 C 边界调入，Go panic 跨边界会直接杀进程且无法被 openWebView 的 recover 捕获
	defer func() { _ = recover() }()
	// Explorer 重启后需重新登记托盘图标（TaskbarCreated 为运行期注册的消息号，故用 if 而非 case）
	if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg {
		addTrayIcon(hwnd)
	}
	switch msg {
	case _WM_NCCALCSIZE:
		if ret, handled := framelessNCCalcSize(hwnd, wparam, lparam); handled {
			return ret // 去掉标题栏；未接管（wparam==0）则落到默认过程
		}
	case _WM_GETMINMAXINFO: // 最小窗口 1024×576（见 minsize_windows.go）：先让原过程填默认值，再改最小可拖动尺寸
		r, _, _ := pCallWindowProc.Call(origWndProc, hwnd, msg, wparam, lparam)
		applyMinTrackSize(hwnd, lparam)
		return r
	case _WM_CLOSE: // 窗口的 ×（前端经 hostWinClose 投递）、Alt+F4、任务栏「关闭窗口」都到这里
		switch onClose(time.Now(), isWindowVisible(hwnd)) {
		case closeToTray:
			pShowWindow.Call(hwnd, _SW_HIDE)
			return 0 // 吞掉关闭：仅隐藏到托盘，服务器继续运行
		case closeConfirm:
			if isIconic(hwnd) { // 在任务栏上关一个最小化着的窗口：先还原并置前，确认框才看得见
				restoreWindow(hwnd)
			}
			return 0 // 吞掉关闭：界面弹确认框，用户选了再经 hostWinHide / hostWinQuit 回来
		}
		// closeDefault → 落到原过程（销毁窗口 → 退出）
	case _WM_TRAYCALLBACK:
		switch lparam {
		case _WM_LBUTTONUP, _WM_LBUTTONDBLCLK:
			restoreWindow(hwnd)
		case _WM_RBUTTONUP:
			showTrayMenu(hwnd)
		}
		return 0
	case _WM_APP_QUIT:
		removeTrayIcon()
		pDestroyWindow.Call(hwnd) // → WM_DESTROY → webview.Terminate() → Run 退出
		return 0
	case _WM_DESTROY:
		removeTrayIcon() // 兜底：无论何路径退出都清掉托盘图标
	}
	r, _, _ := pCallWindowProc.Call(origWndProc, hwnd, msg, wparam, lparam)
	return r
}

// ---- 关窗确认 ----
// 没开「最小化到托盘」时点 × 会退出程序、停掉所有服务器。有服务器在运行时先请界面问一声
// （最小化到托盘 / 停止服务器并退出 / 取消），没有服务器在跑就照旧直接关。

type closeAction int

const (
	closeDefault closeAction = iota // 交给原窗口过程：销毁窗口 → 程序退出（停掉服务器）
	closeToTray                     // 隐藏到托盘，服务器继续运行
	closeConfirm                    // 先请界面弹确认框
)

// closeAckTimeout 请界面确认后等它回应的时长：页面失联（没加载完、脚本出错）时，超时后的下一次关窗直接退出，不把窗口困住
const closeAckTimeout = 3 * time.Second

var (
	runningServers func() int                   // 正在运行（含启动中、停止中）的服务器台数，由 main 注入
	askCloseUI     func(tray bool, running int) // 请界面弹关窗确认框，由 openWebView 注入（WebView2 建好之后才有）
	closeAsk       closeAsker
)

// closeAsker 记着上一次请界面确认的情况。只在窗口线程上读写（WM_CLOSE 与 Bind 回调都在这个线程）。
type closeAsker struct {
	askedAt time.Time // 上一次请界面确认的时间（零值 = 还没请过）
	acked   bool      // 界面回应了（确认框弹出来了）
}

// closeAskStale 一次没得到回应的请求过了这么久就作废：那多半是页面还没加载完时的一次关窗，
// 不能让几小时后正常点 × 也被当成「页面失联」直接退出、停掉服务器
const closeAskStale = 30 * time.Second

// usable 界面现在能不能接确认请求：没请过、上次回应了、上次请求还没等满超时，或者那次请求已经作废，都算能。
// 只有「请求发出 3～30 秒内还没回应」时不能：用户正在连着点关闭，而页面没反应，这次直接退出。
func (a *closeAsker) usable(now time.Time) bool {
	if a.askedAt.IsZero() || a.acked {
		return true
	}
	el := now.Sub(a.askedAt)
	return el < closeAckTimeout || el >= closeAskStale
}

// decideClose 决定一次关窗请求怎么处理。minToTray：设置里开了「最小化到托盘」；trayOK：托盘图标登记成功；
// running：在运行的服务器台数；askable：界面能弹确认框。托盘图标登记失败时不隐藏，免得窗口藏起来却没有图标可恢复。
func decideClose(minToTray, trayOK bool, running int, askable bool) closeAction {
	if minToTray && trayOK {
		return closeToTray
	}
	if running > 0 && askable {
		return closeConfirm
	}
	return closeDefault
}

// onClose 处理一次 WM_CLOSE：要确认时顺带把请求发给界面。visible：窗口当前显示着（含最小化）。
// 窗口藏着时（-minimized 静默启动后没开托盘开关）收到的关闭多半来自别的程序（taskkill、安装程序），
// 用户看不见确认框，照旧直接退出（会先停掉服务器）。
func onClose(now time.Time, visible bool) closeAction {
	minToTray := app.GetConfig().MinimizeToTray
	running := 0
	if !(minToTray && trayActive) && runningServers != nil {
		running = runningServers()
	}
	act := decideClose(minToTray, trayActive, running, askCloseUI != nil && visible && closeAsk.usable(now))
	if act == closeConfirm {
		// 还在等上一次回应时连点关闭不重新计时：页面失联时要让超时走得到，不把窗口困住
		if closeAsk.askedAt.IsZero() || closeAsk.acked || now.Sub(closeAsk.askedAt) >= closeAskStale {
			closeAsk.askedAt = now
		}
		closeAsk.acked = false
		askCloseUI(trayActive, running)
	}
	return act
}

func isWindowVisible(hwnd uintptr) bool { r, _, _ := pIsWindowVisible.Call(hwnd); return r != 0 }
func isIconic(hwnd uintptr) bool        { r, _, _ := pIsIconic.Call(hwnd); return r != 0 }

// hostCloseAck 界面收到关窗确认请求后回一声（前端 __mdCloseAsk 里调）。
func hostCloseAck() { closeAsk.acked = true }

// hideToTray 确认框里选了「最小化到托盘」：托盘图标在就收进托盘，不在就退一步最小化到任务栏。
func hideToTray(hwnd uintptr) {
	if trayActive {
		pShowWindow.Call(hwnd, _SW_HIDE)
	} else {
		pShowWindow.Call(hwnd, _SW_MINIMIZE)
	}
}

// restoreWindow 从托盘/最小化恢复窗口并置前。
func restoreWindow(hwnd uintptr) {
	pShowWindow.Call(hwnd, _SW_SHOW)
	pShowWindow.Call(hwnd, _SW_RESTORE)
	pSetForegroundWin.Call(hwnd)
}

// showTrayMenu 在光标处弹出托盘右键菜单。
func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	appendItem := func(id uintptr, text string) {
		p, err := syscall.UTF16PtrFromString(text)
		if err != nil {
			return
		}
		pAppendMenu.Call(menu, _MF_STRING, id, uintptr(unsafe.Pointer(p)))
	}
	appendItem(_idTrayOpen, "打开面板")
	appendItem(_idTrayQuit, "全部停止并退出")

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWin.Call(hwnd) // MSDN 要求：否则菜单在点击别处时不会消失
	cmd, _, _ := pTrackPopupMenu.Call(menu, _TPM_RIGHTBUTTON|_TPM_RETURNCMD,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	switch cmd {
	case _idTrayOpen:
		restoreWindow(hwnd)
	case _idTrayQuit:
		removeTrayIcon()
		pDestroyWindow.Call(hwnd)
	}
}

func newNID(hwnd uintptr) *notifyIconData {
	nid := &notifyIconData{
		HWnd:             hwnd,
		UID:              _trayUID,
		UFlags:           _NIF_MESSAGE | _NIF_ICON | _NIF_TIP,
		UCallbackMessage: _WM_TRAYCALLBACK,
		HIcon:            trayHIcon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(*nid))
	tip, err := syscall.UTF16FromString("MineDeck · MC 一键开服")
	if err == nil {
		copy(nid.SzTip[:len(nid.SzTip)-1], tip)
	}
	return nid
}

func addTrayIcon(hwnd uintptr) {
	nid := newNID(hwnd)
	r, _, _ := pShellNotifyIcon.Call(_NIM_ADD, uintptr(unsafe.Pointer(nid)))
	if r == 0 { // 别的程序广播了 TaskbarCreated、图标其实还在：重复 ADD 会失败，改成 MODIFY 再确认一次
		r, _, _ = pShellNotifyIcon.Call(_NIM_MODIFY, uintptr(unsafe.Pointer(nid)))
	}
	trayActive = r != 0 // 登记失败时（外壳未就绪等）关窗最小化会退化为正常退出，不困住窗口
}

func removeTrayIcon() {
	if trayHWND == 0 {
		return
	}
	nid := &notifyIconData{HWnd: trayHWND, UID: _trayUID}
	nid.CbSize = uint32(unsafe.Sizeof(*nid))
	pShellNotifyIcon.Call(_NIM_DELETE, uintptr(unsafe.Pointer(nid)))
}

// hideTrayWindow 隐藏窗口（-minimized 随开机自启静默启动时用）。
func hideTrayWindow(hwnd uintptr) { pShowWindow.Call(hwnd, _SW_HIDE) }

// postTrayQuit 线程安全地请求 GUI 退出（PostMessageW 投递到窗口所属线程）。
func postTrayQuit(hwnd uintptr) { pPostMessage.Call(hwnd, _WM_APP_QUIT, 0, 0) }
