package main

import "golang.org/x/sys/windows"

// setDPIAware 声明进程为 Per-Monitor v2 DPI 感知，避免高 DPI 屏（2K/4K）上
// WebView2 窗口被系统按 96DPI 渲染后位图拉伸而整体发虚。必须在创建任何窗口前调用。
// 逐级降级：Win10 1703+（PerMonitorV2）→ Win8.1+（PerMonitor）→ Vista+（System）。
func setDPIAware() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4；^uintptr(3) 即 -4
		if r, _, _ := p.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	if p := windows.NewLazySystemDLL("shcore.dll").NewProc("SetProcessDpiAwareness"); p.Find() == nil {
		// PROCESS_PER_MONITOR_DPI_AWARE = 2；返回 S_OK(0) 视为成功
		if r, _, _ := p.Call(2); r == 0 {
			return
		}
	}
	if p := user32.NewProc("SetProcessDPIAware"); p.Find() == nil {
		_, _, _ = p.Call()
	}
}

// systemDPIScale 返回主显示器的 DPI 缩放系数（如 150% → 1.5）；取不到则回退 1.0。
// go-webview2 以物理像素创建窗口，故据此放大初始窗口尺寸以保持等效视觉大小。
func systemDPIScale() float64 {
	if p := windows.NewLazySystemDLL("user32.dll").NewProc("GetDpiForSystem"); p.Find() == nil {
		if r, _, _ := p.Call(); r >= 96 {
			return float64(r) / 96.0
		}
	}
	return 1.0
}
