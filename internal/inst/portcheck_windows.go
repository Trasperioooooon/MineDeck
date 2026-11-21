package inst

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// soExclusiveAddrUse Windows 的 SO_EXCLUSIVEADDRUSE（按定义是 SO_REUSEADDR 取反），x/sys/windows 里没有这个常量。
const soExclusiveAddrUse = ^windows.SO_REUSEADDR

// checkBindPort 认的两种失败：「被占着」和「不许访问」。
const (
	errAddrInUse  = windows.WSAEADDRINUSE
	errAddrAccess = windows.WSAEACCES
)

// probeControl 试绑时照 Java 的做法带上 SO_EXCLUSIVEADDRUSE（见 checkBindPort）。
func probeControl(_, _ string, c syscall.RawConn) error {
	// 设不上（比如 wine 不认这个选项）就按普通方式试绑，不因为这个报错
	return c.Control(func(fd uintptr) {
		_ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
	})
}
