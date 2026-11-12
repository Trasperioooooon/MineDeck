package main

// 应用图标与深色标题栏（Windows）。
// - 图标：内嵌 assets/icon.ico，运行时经 LoadImageW 载入 HICON，WM_SETICON 挂到窗口
//   （标题栏 / 任务栏 / Alt-Tab），托盘复用同一图标；exe 文件图标另由 rsrc_windows_amd64.syso 提供。
// - 深色标题栏：DwmSetWindowAttribute(DWMWA_USE_IMMERSIVE_DARK_MODE) 让系统标题栏转深色，匹配深色主题。

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"minedeck/assets"

	"golang.org/x/sys/windows"
)

// 图标文件在仓库根的 assets/ 里，go:embed 够不到上级目录，所以由 assets 包内嵌、这里取用。
var icoData = assets.IconICO

const (
	_IMAGE_ICON      = 1
	_LR_LOADFROMFILE = 0x00000010
	_LR_DEFAULTSIZE  = 0x00000040
	_WM_SETICON      = 0x0080
	_ICON_SMALL      = 0
	_ICON_BIG        = 1

	// Win10 20H1+ / Win11：让非客户区（标题栏）使用深色配色
	_DWMWA_USE_IMMERSIVE_DARK_MODE = 20
)

var (
	dwmapi            = windows.NewLazySystemDLL("dwmapi.dll")
	pDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
	pLoadImageW       = user32t.NewProc("LoadImageW") // user32t 定义于 tray_windows.go（同 package）
	pSendMessageW     = user32t.NewProc("SendMessageW")

	iconTmpPath string
	iconTmpOnce sync.Once
)

// iconFile 将内嵌 .ico 落到临时目录一次，返回其路径（供 LoadImageW 从文件加载，稳妥支持 PNG 压缩的 .ico）。
func iconFile() string {
	iconTmpOnce.Do(func() {
		p := filepath.Join(os.TempDir(), "minedeck-app.ico")
		if err := os.WriteFile(p, icoData, 0o644); err == nil {
			iconTmpPath = p
		}
	})
	return iconTmpPath
}

// loadAppIcon 从内嵌图标载入指定像素尺寸的 HICON（px=0 取系统默认大图标尺寸）；失败返回 0。
func loadAppIcon(px int) uintptr {
	p := iconFile()
	if p == "" {
		return 0
	}
	ptr, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return 0
	}
	flags := uintptr(_LR_LOADFROMFILE)
	if px == 0 {
		flags |= _LR_DEFAULTSIZE
	}
	h, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(ptr)), _IMAGE_ICON, uintptr(px), uintptr(px), flags)
	return h
}

// applyWindowIcon 把应用图标设到窗口（影响标题栏小图标、任务栏、Alt-Tab）。
func applyWindowIcon(hwnd uintptr) {
	if big := loadAppIcon(0); big != 0 {
		pSendMessageW.Call(hwnd, _WM_SETICON, _ICON_BIG, big)
	}
	if small := loadAppIcon(16); small != 0 {
		pSendMessageW.Call(hwnd, _WM_SETICON, _ICON_SMALL, small)
	}
}

// applyDarkTitleBar 让原生标题栏转为深色（旧系统上不支持该属性时静默失败，无副作用）。
func applyDarkTitleBar(hwnd uintptr) {
	var enabled int32 = 1
	pDwmSetWindowAttr.Call(hwnd, _DWMWA_USE_IMMERSIVE_DARK_MODE,
		uintptr(unsafe.Pointer(&enabled)), unsafe.Sizeof(enabled))
}
