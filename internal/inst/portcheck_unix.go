//go:build !windows

package inst

import (
	"syscall"

	"minedeck/internal/portprobe"
)

// checkBindPort 认的两种失败：「被占着」和「不许访问」（类 Unix 上是没有权限绑这个端口）。
const (
	errAddrInUse  = syscall.EADDRINUSE
	errAddrAccess = syscall.EACCES
)

// probeControl 类 Unix 上 Java 绑端口用的是系统默认做法（带 SO_REUSEADDR，Go 的 Listen 也带），试绑不用另设选项。
func probeControl(string, string, syscall.RawConn) error { return nil }

func defaultRunNetshExcluded(bool) (string, error) { return "", nil }

func defaultListenPorts() map[int]int { return portprobe.TCPListeners() }

func defaultProcessName(pid int) string { return portprobe.ProcessName(pid) }

// extraBindHosts 类 Unix（Mac）上监听通配地址和 127.0.0.1 能同时成功：另一程序只占着 127.0.0.1（或 ::1）时，
// 仅试绑通配地址会误判空闲。通配或 0.0.0.0/:: 时补试回环地址。
func extraBindHosts(network, host string) []string {
	switch host {
	case "":
		if network == "tcp4" {
			return []string{"127.0.0.1"}
		}
		return []string{"127.0.0.1", "::1"}
	case "0.0.0.0":
		return []string{"127.0.0.1"}
	case "::":
		return []string{"::1"}
	}
	return nil
}
