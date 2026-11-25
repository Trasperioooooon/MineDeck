//go:build darwin

package portprobe

import (
	"bytes"

	"golang.org/x/sys/unix"
)

// ProcessName 进程的程序名（如 java）；查不到返回空串。先从 kern.procargs2 取完整的程序路径，
// 取不到（别的用户的进程）再用内核记的短名（最多 16 个字节）。
func ProcessName(pid int) string {
	if b, err := unix.SysctlRaw("kern.procargs2", pid); err == nil && len(b) > 4 {
		// 开头 4 个字节是参数个数，后面紧跟程序路径
		if p, _, _ := bytes.Cut(b[4:], []byte{0}); len(p) > 0 {
			return BaseExe(string(p))
		}
	}
	if kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid); err == nil {
		if name, _, _ := bytes.Cut(kp.Proc.P_comm[:], []byte{0}); len(name) > 0 {
			return string(name)
		}
	}
	return ""
}
