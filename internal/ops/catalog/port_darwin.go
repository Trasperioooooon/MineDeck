package catalog

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Mac 版的端口占用查询（对应 port_windows.go）。系统没有 GetExtendedTcpTable 那样的接口可直接调，
// 用系统自带的 lsof 读监听表。lsof 只看得到当前用户的进程：别的用户（包括系统服务）占着的端口查不到是谁，
// 和查不到一样返回空，由试绑端口那一步兜底说「被占着」。

// portOwners 查哪些进程在监听这个端口（TCP 只看监听的，UDP 看绑定的）。查不到返回空。
func portOwners(port int, udp bool) []int {
	seen := map[int]bool{}
	var out []int
	portTable(udp, func(p, pid int) {
		if p == port && !seen[pid] {
			seen[pid] = true
			out = append(out, pid)
		}
	})
	return out
}

// tcpListeners 这台电脑上正在监听的 TCP 端口，端口 → 第一个查到的进程编号。查不了返回空表。
func tcpListeners() map[int]int {
	out := map[int]int{}
	portTable(false, func(p, pid int) {
		if _, ok := out[p]; !ok {
			out[p] = pid
		}
	})
	return out
}

// portTable 把 lsof 列出的 TCP 监听（udp 为真时是 UDP 绑定）逐行交给 fn(端口, 进程编号)，IPv4 和 IPv6 都给。
// -F pn 的输出一行一个字段：p 开头是进程编号，n 开头是地址（*:25565、127.0.0.1:27333、[::1]:8080），
// 地址行属于它前面最近的那个进程。
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
			i := strings.LastIndexByte(addr, ':')
			if i < 0 {
				continue
			}
			if p, err := strconv.Atoi(addr[i+1:]); err == nil && p > 0 && p < 65536 {
				fn(p, pid)
			}
		}
	}
}

// processName 进程的程序名（如 java）；查不到返回空串。先从 kern.procargs2 取完整的程序路径，
// 取不到（别的用户的进程）再用内核记的短名（最多 16 个字节）。
func processName(pid int) string {
	if b, err := unix.SysctlRaw("kern.procargs2", pid); err == nil && len(b) > 4 {
		// 开头 4 个字节是参数个数，后面紧跟程序路径
		if p, _, _ := bytes.Cut(b[4:], []byte{0}); len(p) > 0 {
			return baseExe(string(p))
		}
	}
	if kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid); err == nil {
		if name, _, _ := bytes.Cut(kp.Proc.P_comm[:], []byte{0}); len(name) > 0 {
			return string(name)
		}
	}
	return ""
}
