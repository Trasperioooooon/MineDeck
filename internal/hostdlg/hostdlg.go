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
	owner atomic.Uintptr // MineDeck 主窗口的句柄；浏览器模式（-nogui）下是 0
	busy  sync.Mutex
)

// SetOwner 记下主窗口的句柄：对话框挂在它上面，开着的时候主窗口点不动，也不会被主窗口盖住。
func SetOwner(hwnd uintptr) { owner.Store(hwnd) }

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
	path, err := pickFolderNative(owner.Load(), title)
	if err == nil {
		return path, nil
	}
	logf("选择文件夹：系统对话框打不开（%v），改用 PowerShell", err)
	path, err = pickFolderFallback(title)
	if err != nil {
		logf("选择文件夹：PowerShell 这条路也没走通：%v", err)
		return "", ErrUnavailable
	}
	return path, nil
}
