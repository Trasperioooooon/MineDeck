package main

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 测试里冒充「系统给的 lparam」的两块内存。放在包级变量里：它们在数据段，不在栈上也不归垃圾回收管，
// 地址从头到尾不变，转成 uintptr 传进去再解引用是安全的。
var (
	testNCCParams nccalcsizeParams
	testNCCPos    windowPos
	testMinMax    minMaxInfo
)

// WM_NCCALCSIZE：真建一个带标题栏的窗口，照系统的办法递一份 NCCALCSIZE_PARAMS 进去。
// 处理完以后客户区的顶边要留在窗口顶（标题栏抹掉了），左、右、下三边由默认过程收进一个缩放边框。
// 这条测试守的是「经 lparam 读写的就是系统给的那块内存」：指针取错的话，顶边留不在原处（默认过程照样会把三边收进去，
// 抓住错的是顶边那条断言）。最大化那一支这里没测，在真窗口里量过。
func TestFramelessNCCalcSize(t *testing.T) {
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
	cls, _ := windows.UTF16PtrFromString("MineDeckFramelessTest")
	wc := wndClassEx{WndProc: pDefWindowProcW.Addr(), Instance: uintptr(hinst), ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Fatalf("注册窗口类失败：%v", err)
	}
	h, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0xCF0000, // WS_OVERLAPPEDWINDOW，放在屏幕外，不显示
		uintptr(0xFFFFD8F0), uintptr(0xFFFFD8F0), 800, 600, 0, 0, uintptr(hinst), 0)
	if h == 0 {
		t.Fatalf("建窗失败：%v", err)
	}
	defer pDestroyWindow.Call(h)

	// wparam 为 0 时不接管，内存也不该被动过
	orig := nccRect{Left: 100, Top: 100, Right: 900, Bottom: 700}
	testNCCPos = windowPos{HWnd: h, X: orig.Left, Y: orig.Top, CX: orig.Right - orig.Left, CY: orig.Bottom - orig.Top}
	testNCCParams = nccalcsizeParams{Rgrc: [3]nccRect{orig, orig, orig}, Lppos: uintptr(unsafe.Pointer(&testNCCPos))}
	lparam := uintptr(unsafe.Pointer(&testNCCParams))
	if _, handled := framelessNCCalcSize(h, 0, lparam); handled {
		t.Fatal("wparam 为 0 时不该接管")
	}
	if testNCCParams.Rgrc[0] != orig {
		t.Fatalf("wparam 为 0 时不该改矩形：%+v", testNCCParams.Rgrc[0])
	}

	ret, handled := framelessNCCalcSize(h, 1, lparam)
	if !handled || ret != 0 {
		t.Fatalf("wparam 为 1 时应接管并返回 0：ret=%d handled=%v", ret, handled)
	}
	got := testNCCParams.Rgrc[0]
	if got.Top != orig.Top {
		t.Errorf("客户区顶边应留在窗口顶 %d（标题栏抹掉），实际 %d", orig.Top, got.Top)
	}
	if !(got.Left > orig.Left && got.Right < orig.Right && got.Bottom < orig.Bottom) {
		t.Errorf("左、右、下应由默认过程收进一个缩放边框：原来 %+v，处理后 %+v", orig, got)
	}
}

// WM_GETMINMAXINFO：别的字段留着原过程填的值，只有最小可拖动尺寸被改成算出来的那个。
func TestApplyMinTrackSize(t *testing.T) {
	filled := minMaxInfo{
		Reserved:     point{X: 1, Y: 2},
		MaxSize:      point{X: 3, Y: 4},
		MaxPosition:  point{X: 5, Y: 6},
		MinTrackSize: point{X: 7, Y: 8},
		MaxTrackSize: point{X: 9, Y: 10},
	}
	testMinMax = filled
	applyMinTrackSize(0, 0) // lparam 为 0：什么都不做，也不能崩
	if testMinMax != filled {
		t.Fatalf("lparam 为 0 时不该改任何东西：%+v", testMinMax)
	}

	// 窗口句柄给 0：DPI 取不到按 96 算，工作区取最近的显示器（取不到就不收）。期望值用同一套函数算，不写死屏幕大小。
	applyMinTrackSize(0, uintptr(unsafe.Pointer(&testMinMax)))
	workW, workH := workAreaSize(0)
	w, h := minTrackSize(windowDPI(0), workW, workH)
	if w <= 0 || h <= 0 {
		t.Fatalf("算出来的最小尺寸不对：%d×%d", w, h)
	}
	want := filled
	want.MinTrackSize = point{X: w, Y: h}
	if testMinMax != want {
		t.Errorf("只该改最小可拖动尺寸：得到 %+v，应为 %+v", testMinMax, want)
	}
}
