package app

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Mac 上的系统信息（第 0 步摸底：最小可用，查不到时按各自约定的兜底值返回，见 sys_windows.go）。

// DiskFree 路径所在的卷还剩多少字节（当前用户能用的，即 statfs 的 f_bavail）。路径要已经存在。
func DiskFree(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// TotalRAMMB 返回物理内存总量（MB），失败时返回保守值。
func TotalRAMMB() int {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil || n == 0 {
		return 8192
	}
	return int(n / (1024 * 1024))
}

// AvailRAMMB 返回当前可用物理内存（MB）；查不到时返回 0（表示未知）。
// macOS 没有和 Windows AvailPhys 一一对应的数：空闲页很少（文件缓存会把它吃满），这里用内核的
// kern.memorystatus_level（还能用的内存占总量的百分比，「活动监视器」里内存压力的来源）乘总量估算。
func AvailRAMMB() int {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0
	}
	level, err := unix.SysctlUint32("kern.memorystatus_level")
	if err != nil || level == 0 || level > 100 {
		return 0
	}
	return int(total / 100 * uint64(level) / (1024 * 1024))
}

// OSVersion macOS 的版本号，如「macOS 26.1（25B78，arm64）」。
func OSVersion() string {
	ver, _ := unix.Sysctl("kern.osproductversion")
	build, _ := unix.Sysctl("kern.osversion")
	arch := "x86_64"
	if v, err := unix.SysctlUint32("hw.optional.arm64"); err == nil && v == 1 {
		arch = "arm64"
	}
	if ver == "" {
		return "macOS（" + arch + "）"
	}
	if build == "" {
		return fmt.Sprintf("macOS %s（%s）", ver, arch)
	}
	return fmt.Sprintf("macOS %s（%s，%s）", ver, build, arch)
}
