package app

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DiskFree 路径所在的盘还剩多少字节（当前用户能用的）。路径要已经存在。
func DiskFree(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// TotalRAMMB 返回物理内存总量（MB），失败时返回保守值。
func TotalRAMMB() int {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 8192
	}
	return int(m.TotalPhys / (1024 * 1024))
}

// AvailRAMMB 返回当前可用物理内存（MB）；调用失败时返回 0（表示未知，
// 与 TotalRAMMB 的保守兜底 8192 区分，便于前端据 0 走降级逻辑）。
func AvailRAMMB() int {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0
	}
	return int(m.AvailPhys / (1024 * 1024))
}

// OSVersion Windows 的版本号，如「Windows 10.0.26300」（Win11 的主版本号也是 10，按内部版本号区分：22000 起是 Win11）。
func OSVersion() string {
	v := windows.RtlGetVersion()
	name := "Windows"
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		name = "Windows 11"
	} else if v.MajorVersion == 10 {
		name = "Windows 10"
	}
	return fmt.Sprintf("%s（%d.%d.%d）", name, v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
