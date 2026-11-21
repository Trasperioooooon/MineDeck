//go:build !windows

package inst

import "syscall"

// checkBindPort 认的两种失败：「被占着」和「不许访问」（类 Unix 上是没有权限绑这个端口）。
const (
	errAddrInUse  = syscall.EADDRINUSE
	errAddrAccess = syscall.EACCES
)

// probeControl 类 Unix 上 Java 绑端口用的是系统默认做法（带 SO_REUSEADDR，Go 的 Listen 也带），试绑不用另设选项。
func probeControl(string, string, syscall.RawConn) error { return nil }
