package hostdlg

// 真弹对话框的测试：会在桌面上闪一下窗口，所以默认跳过，要跑时设环境变量：
//
//	$env:MINEDECK_LIVE_DIALOG = "1"; go test ./internal/hostdlg/ -run Live -v
//
// 两条路（系统对话框、PowerShell 后备）各测两件事：对话框真的弹出来了（找得到那个窗口）；
// 关掉它 = 取消，返回空路径、不报错；按下确定，返回一个存在的文件夹。测试自己找到窗口发消息，不用人动手。

import (
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	pEnumWindows            = user32.NewProc("EnumWindows")
	pGetClassNameW          = user32.NewProc("GetClassNameW")
	pGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	pIsWindowVisible        = user32.NewProc("IsWindowVisible")
	pPostMessageW           = user32.NewProc("PostMessageW")
)

const (
	wmClose   = 0x0010
	wmCommand = 0x0111
	idOK      = 1
)

// childPIDs 本进程的子进程（PowerShell 后备起的那个）。
func childPIDs(t *testing.T) map[uint32]bool {
	out := map[uint32]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatalf("取不到进程列表：%v", err)
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	me := uint32(os.Getpid())
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if pe.ParentProcessID == me {
			out[pe.ProcessID] = true
		}
	}
	return out
}

// findDialog 找属于这些进程的、看得见的对话框窗口（窗口类 #32770）。
func findDialog(pids map[uint32]bool) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		pGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if !pids[pid] {
			return 1
		}
		if v, _, _ := pIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var cls [64]uint16
		pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), uintptr(len(cls)))
		if windows.UTF16ToString(cls[:]) == "#32770" {
			found = hwnd
			return 0
		}
		return 1
	})
	pEnumWindows.Call(cb, 0)
	return found
}

// drive 调 pick（会一直等到对话框关掉），同时找到弹出来的对话框，发 msg 过去；返回 pick 的结果。
func drive(t *testing.T, own bool, msg, wparam uintptr, pick func() (string, error)) (string, error) {
	t.Helper()
	type res struct {
		path string
		err  error
	}
	done := make(chan res, 1)
	go func() { p, err := pick(); done <- res{p, err} }()
	deadline := time.Now().Add(20 * time.Second)
	sent := false
	for time.Now().Before(deadline) {
		select {
		case r := <-done:
			if !sent {
				t.Fatalf("对话框没弹出来就返回了：path=%q err=%v", r.path, r.err)
			}
			return r.path, r.err
		case <-time.After(150 * time.Millisecond):
		}
		if sent {
			continue
		}
		pids := map[uint32]bool{uint32(os.Getpid()): true}
		if !own {
			pids = childPIDs(t)
		}
		if h := findDialog(pids); h != 0 {
			time.Sleep(600 * time.Millisecond) // 等它初始化完（文件夹列表读出来）再发消息
			pPostMessageW.Call(h, msg, wparam, 0)
			sent = true
		}
	}
	t.Fatalf("等了 20 秒：sent=%v（false = 一直没找到对话框窗口，true = 发了消息对话框没关）", sent)
	return "", nil
}

func liveOnly(t *testing.T) {
	if os.Getenv("MINEDECK_LIVE_DIALOG") != "1" {
		t.Skip("会真的弹窗口，默认跳过；设 MINEDECK_LIVE_DIALOG=1 再跑")
	}
}

func checkDir(t *testing.T, what, path string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：%v", what, err)
	}
	st, serr := os.Stat(path)
	if path == "" || serr != nil || !st.IsDir() {
		t.Fatalf("%s：返回的不是一个存在的文件夹：%q（%v）", what, path, serr)
	}
	t.Logf("%s → %s", what, path)
}

func TestLiveNativeCancel(t *testing.T) {
	liveOnly(t)
	p, err := drive(t, true, wmClose, 0, func() (string, error) { return pickFolderNative(0, "选择文件夹（测试：马上自动关掉）") })
	if p != "" || err != nil {
		t.Fatalf("关掉对话框应当算取消（空路径、不报错），实际 path=%q err=%v", p, err)
	}
}

func TestLiveNativeOK(t *testing.T) {
	liveOnly(t)
	p, err := drive(t, true, wmCommand, idOK, func() (string, error) { return pickFolderNative(0, "选择文件夹（测试：马上自动确定）") })
	checkDir(t, "系统对话框按确定", p, err)
}

func TestLiveFallbackCancel(t *testing.T) {
	liveOnly(t)
	p, err := drive(t, false, wmClose, 0, func() (string, error) { return pickFolderFallback("选择文件夹（测试：马上自动关掉）") })
	if p != "" || err != nil {
		t.Fatalf("关掉对话框应当算取消（空路径、不报错），实际 path=%q err=%v", p, err)
	}
}

func TestLiveFallbackOK(t *testing.T) {
	liveOnly(t)
	p, err := drive(t, false, wmCommand, idOK, func() (string, error) { return pickFolderFallback("选择文件夹（测试：马上自动确定）") })
	checkDir(t, "PowerShell 后备按确定", p, err)
}

// PickFolder 整条路：取消不报错；已经开着一个时再调，马上返回 ErrBusy。
func TestLivePickFolder(t *testing.T) {
	liveOnly(t)
	p, err := drive(t, true, wmClose, 0, func() (string, error) { return PickFolder("选择文件夹（测试：马上自动关掉）", t.Logf) })
	if p != "" || err != nil {
		t.Fatalf("PickFolder 取消应当返回空路径、不报错，实际 path=%q err=%v", p, err)
	}
	busy.Lock() // 假装有一个对话框开着
	_, err = PickFolder("不会弹出来", nil)
	busy.Unlock()
	if err != ErrBusy {
		t.Fatalf("已经开着一个时应当返回 ErrBusy，实际 %v", err)
	}
}
