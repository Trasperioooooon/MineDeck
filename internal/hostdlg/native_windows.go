package hostdlg

// 系统自带的新式「选择文件夹」：COM 的 IFileOpenDialog 加 FOS_PICKFOLDERS。
// 直接按虚表下标调，不引第三方 COM 库。下标对着 shobjidl_core.h 里接口的声明顺序：
// IUnknown（0–2）→ IModalWindow（3 Show）→ IFileDialog（4–26）→ IFileOpenDialog（27–28）。

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32             = windows.NewLazySystemDLL("ole32.dll")
	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoUninitialize   = ole32.NewProc("CoUninitialize")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	clsctxInprocServer      = 0x1

	fosNoChangeDir     = 0x8
	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosPathMustExist   = 0x800

	sigdnFileSysPath = 0x80058000

	hrCancelled = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)：用户点了取消或关掉了对话框

	// 虚表下标
	vtRelease        = 2
	vtShow           = 3
	vtSetOptions     = 9
	vtGetOptions     = 10
	vtSetTitle       = 17
	vtGetResult      = 20
	vtGetDisplayName = 5 // IShellItem
)

// comCall 调 COM 对象虚表里第 idx 个方法（this 指针自动带上）。obj 是接口指针。
//
// go:uintptrescapes 不能少：调用处把 &opts、&item、标题字符串这些指针转成 uintptr 传进来，没有这个指示的话，
// 它们在 Go 眼里只是整数——栈上的变量可能在这次调用里因为栈增长被挪走，堆上的可能被回收，COM 那边就写到了别处。
// 加上以后，写在调用参数里的 uintptr(unsafe.Pointer(…)) 所指的东西会放到堆上，并且一直活到调用返回
// （和 windows.LazyProc.Call 是同一个办法）。所以转换必须直接写在 comCall 的参数里，不能先存进变量。
//
//go:uintptrescapes
func comCall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(**[32]uintptr)(obj)
	a := make([]uintptr, 0, len(args)+1)
	a = append(a, uintptr(obj))
	a = append(a, args...)
	r, _, _ := syscall.SyscallN(vtbl[idx], a...)
	return r
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// pickFolderNative 弹系统的选择文件夹对话框。owner 是它挂靠的窗口（0 = 不挂靠）。
// 返回 ("", nil) 表示用户取消；返回错误表示对话框根本没弹出来（调用方换另一条路）。
func pickFolderNative(owner uintptr, title string) (string, error) {
	// COM 的单线程套间要求从初始化到释放都在同一个系统线程上，对话框自己的消息循环也跑在这个线程上
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := pCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	if failed(hr) { // 这个线程已经按别的模式初始化过了（RPC_E_CHANGED_MODE）之类
		return "", fmt.Errorf("CoInitializeEx 失败：0x%08x", uint32(hr))
	}
	defer pCoUninitialize.Call()

	var dlg unsafe.Pointer
	hr, _, _ = pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if failed(hr) || dlg == nil {
		return "", fmt.Errorf("创建 FileOpenDialog 失败：0x%08x", uint32(hr))
	}
	defer comCall(dlg, vtRelease)

	var opts uint32
	if hr = comCall(dlg, vtGetOptions, uintptr(unsafe.Pointer(&opts))); failed(hr) {
		return "", fmt.Errorf("GetOptions 失败：0x%08x", uint32(hr))
	}
	opts |= fosPickFolders | fosForceFileSystem | fosPathMustExist | fosNoChangeDir
	if hr = comCall(dlg, vtSetOptions, uintptr(opts)); failed(hr) {
		return "", fmt.Errorf("SetOptions 失败：0x%08x", uint32(hr))
	}
	if t, err := windows.UTF16PtrFromString(title); err == nil && title != "" {
		comCall(dlg, vtSetTitle, uintptr(unsafe.Pointer(t))) // 标题设不上不碍事
	}

	hr = comCall(dlg, vtShow, owner)
	if uint32(hr) == hrCancelled {
		return "", nil
	}
	if failed(hr) {
		return "", fmt.Errorf("Show 失败：0x%08x", uint32(hr))
	}

	var item unsafe.Pointer
	if hr = comCall(dlg, vtGetResult, uintptr(unsafe.Pointer(&item))); failed(hr) || item == nil {
		return "", fmt.Errorf("GetResult 失败：0x%08x", uint32(hr))
	}
	defer comCall(item, vtRelease)

	var name *uint16
	if hr = comCall(item, vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); failed(hr) || name == nil {
		return "", fmt.Errorf("取不到所选文件夹的路径：0x%08x", uint32(hr))
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	return windows.UTF16PtrToString(name), nil
}
