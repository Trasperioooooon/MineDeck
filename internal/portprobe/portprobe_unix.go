//go:build !windows

package portprobe

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// portTable 用系统自带的 lsof 列出的 TCP 监听（udp 为真时是 UDP 绑定），逐行交给 fn(端口, 进程编号)。
// lsof 只看得到当前用户的进程；查不到（没有 lsof 或无权限）时不报错、由调用方退回试绑判断。
func portTable(udp bool, fn func(port, pid int)) {
	args := []string{"-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn"}
	if udp {
		args = []string{"-nP", "-iUDP", "-Fpn"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", args...).Output()
	if err != nil && len(out) == 0 { // 一个都没有时 lsof 的退出码也是 1
		return
	}
	pid := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid > 0:
			addr := line[1:]
			if k := strings.Index(addr, "->"); k >= 0 { // UDP 已连接的套接字带着对端地址
				addr = addr[:k]
			}
			if i := strings.LastIndexByte(addr, ':'); i >= 0 {
				if p, err := strconv.Atoi(addr[i+1:]); err == nil && p > 0 && p <= 65535 {
					fn(p, pid)
				}
			}
		}
	}
}
