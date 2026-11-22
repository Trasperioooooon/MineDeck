//go:build !windows

package inst

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// checkBindPort 认的两种失败：「被占着」和「不许访问」（类 Unix 上是没有权限绑这个端口）。
const (
	errAddrInUse  = syscall.EADDRINUSE
	errAddrAccess = syscall.EACCES
)

// probeControl 类 Unix 上 Java 绑端口用的是系统默认做法（带 SO_REUSEADDR，Go 的 Listen 也带），试绑不用另设选项。
func probeControl(string, string, syscall.RawConn) error { return nil }

func defaultRunNetshExcluded(bool) (string, error) { return "", nil }

func defaultListenPorts() map[int]int {
	out := map[int]int{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn").Output()
	if err != nil && len(raw) == 0 {
		return out
	}
	pid := 0
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid > 0:
			addr := line[1:]
			if i := strings.LastIndexByte(addr, ':'); i >= 0 {
				if p, err := strconv.Atoi(addr[i+1:]); err == nil && p > 0 && p <= 65535 {
					if _, ok := out[p]; !ok {
						out[p] = pid
					}
				}
			}
		}
	}
	return out
}

func defaultProcessName(int) string { return "" }
