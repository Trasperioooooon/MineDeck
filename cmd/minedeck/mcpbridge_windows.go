package main

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// bridgeIdle 关掉 Windows 给刚启动的 GUI 程序显示的「正在后台运行」光标：系统在程序第一次调 GetMessage 时关掉它。
// 桥不建窗口，不这样做的话 AI 工具每次启动它，光标都会转一两秒。做法：PeekMessage 建好消息队列，给自己投一条 WM_NULL，
// 再用 GetMessage 取出来（队列里已经有消息，不会卡住；投递失败就不取）。只影响光标，和标准输入输出无关。
func bridgeIdle() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32 := windows.NewLazySystemDLL("user32.dll")
	peek, post, get := user32.NewProc("PeekMessageW"), user32.NewProc("PostThreadMessageW"), user32.NewProc("GetMessageW")
	if peek.Find() != nil || post.Find() != nil || get.Find() != nil {
		return
	}
	var msg [64]byte // MSG 结构（64 位下 48 字节，多留一点）
	// PM_NOREMOVE：只是建好这个线程的消息队列；再给自己投一条 WM_NULL（消息号 0）
	peek.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
	if r, _, _ := post.Call(uintptr(windows.GetCurrentThreadId()), 0, 0, 0); r == 0 {
		return
	}
	get.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
}
