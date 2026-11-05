package main

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 静默启动：钩子挂着时，建出来的窗口经 ShowWindow(SW_SHOW) 也不显示（go-webview2 建窗后就是这么调的）；
// 撤掉之后能正常显示（托盘恢复走的就是 ShowWindow），窗口过程还原成原来的。别的类名的窗口不受影响。
func TestHiddenCreate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	pCreateWindowEx := user32t.NewProc("CreateWindowExW")
	pRegisterClassEx := user32t.NewProc("RegisterClassExW")
	type wndClassEx struct {
		Size, Style                        uint32
		WndProc                            uintptr
		ClsExtra, WndExtra                 int32
		Instance, Icon, Cursor, Background uintptr
		MenuName, ClassName                *uint16
		IconSm                             uintptr
	}
	var hinst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinst)
	register := func(name string) *uint16 {
		p, _ := windows.UTF16PtrFromString(name)
		wc := wndClassEx{WndProc: pDefWindowProcW.Addr(), Instance: uintptr(hinst), ClassName: p}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			t.Fatalf("注册窗口类 %s 失败：%v", name, err)
		}
		return p
	}
	target, other := register("MineDeckHiddenTest"), register("MineDeckOtherTest")
	create := func(cls *uint16) uintptr {
		// 放在屏幕外，测试万一失败露出来也不打扰
		h, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0xCF0000, // WS_OVERLAPPEDWINDOW
			uintptr(0xFFFFD8F0), uintptr(0xFFFFD8F0), 200, 200, 0, 0, uintptr(hinst), 0) // x = y = -10000
		if h == 0 {
			t.Fatalf("建窗失败：%v", err)
		}
		return h
	}
	visible := isWindowVisible
	wndProc := func(h uintptr) uintptr {
		idx := _GWLP_WNDPROC
		r, _, _ := pGetWindowLongPtr.Call(h, uintptr(idx))
		return r
	}

	end := beginHiddenCreate("MineDeckHiddenTest", false, false)
	if hiddenHook == 0 {
		t.Fatal("CBT 钩子没挂上")
	}
	// 别的类先建、先显示：钩子不该逮它；也顺带用掉进程里「第一次 ShowWindow 按启动参数来」那一次，
	// 免得下面的断言因为启动参数恰好是隐藏而误过
	o := create(other)
	defer pDestroyWindow.Call(o)
	pShowWindow.Call(o, 4) // SW_SHOWNOACTIVATE
	if !visible(o) {
		t.Fatal("别的类的窗口被拦住了")
	}
	h := create(target)
	defer pDestroyWindow.Call(h)
	if hiddenHWND != h {
		t.Fatalf("钩子逮到的窗口不对：%#x，应为 %#x", hiddenHWND, h)
	}
	pShowWindow.Call(h, _SW_SHOW)
	if visible(h) {
		t.Fatal("钩子挂着时 ShowWindow(SW_SHOW) 不该让窗口显示出来")
	}

	end()
	end() // 可重复调用
	if hiddenHook != 0 {
		t.Error("end 后钩子还在")
	}
	if wndProc(h) != pDefWindowProcW.Addr() {
		t.Error("end 后窗口过程没还原")
	}
	pShowWindow.Call(h, 4)
	if !visible(h) {
		t.Error("撤掉钩子后窗口应能正常显示（托盘恢复）")
	}
}

// 正常启动（cloak 模式）：不拦显示，窗口照常是「显示着」的，但第一次显示前已经被 DWM 隐身；
// end 不会让它现身，revealWindow 才现身，重复调用没事。
func TestHiddenCreateCloak(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	pCreateWindowEx := user32t.NewProc("CreateWindowExW")
	pRegisterClassEx := user32t.NewProc("RegisterClassExW")
	pDwmGetWindowAttribute := windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
	type wndClassEx struct {
		Size, Style                        uint32
		WndProc                            uintptr
		ClsExtra, WndExtra                 int32
		Instance, Icon, Cursor, Background uintptr
		MenuName, ClassName                *uint16
		IconSm                             uintptr
	}
	var hinst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinst)
	cls, _ := windows.UTF16PtrFromString("MineDeckCloakTest")
	wc := wndClassEx{WndProc: pDefWindowProcW.Addr(), Instance: uintptr(hinst), ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Fatalf("注册窗口类失败：%v", err)
	}
	cloaked := func(h uintptr) bool {
		var v uint32
		r, _, _ := pDwmGetWindowAttribute.Call(h, 14, uintptr(unsafe.Pointer(&v)), 4) // DWMWA_CLOAKED
		return r == 0 && v != 0
	}

	end := beginHiddenCreate("MineDeckCloakTest", true, true)
	if hiddenHook == 0 {
		t.Fatal("CBT 钩子没挂上")
	}
	h, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0xCF0000, // WS_OVERLAPPEDWINDOW，放在屏幕外
		uintptr(0xFFFFD8F0), uintptr(0xFFFFD8F0), 200, 200, 0, 0, uintptr(hinst), 0)
	if h == 0 {
		t.Fatalf("建窗失败：%v", err)
	}
	defer pDestroyWindow.Call(h)
	pShowWindow.Call(h, 4) // SW_SHOWNOACTIVATE
	if !isWindowVisible(h) {
		t.Fatal("cloak 模式不该拦显示")
	}
	if !cloaked(h) {
		t.Fatal("第一次显示前应已隐身")
	}
	end()
	if !cloaked(h) {
		t.Fatal("撤钩子不该让窗口现身（要等页面叫 revealWindow）")
	}
	revealWindow(h)
	revealWindow(h)
	if cloaked(h) {
		t.Fatal("revealWindow 后应现身")
	}
}
