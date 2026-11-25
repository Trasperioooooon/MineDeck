// Package portprobe 读取本机正在监听或绑定的 TCP / UDP 端口以及对应进程名，供实例启动检查与端口排查操作共用。
package portprobe

import (
	"path/filepath"
	"strings"
)

// BaseExe 取路径里的程序名（兼容 Windows 反斜杠和 Unix 正斜杠）。
func BaseExe(p string) string {
	return filepath.Base(strings.ReplaceAll(p, `\`, "/"))
}

// PortOwners 查哪些进程在监听或绑定这个端口（TCP 只看监听的，UDP 看绑定的），IPv4 和 IPv6 都查。查不到返回空。
func PortOwners(port int, udp bool) []int {
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

// TCPListeners 这台电脑上正在监听的 TCP 端口，端口 → 第一个查到的进程编号（IPv4 和 IPv6 都算）。
// 只读一次系统的监听表，不试绑端口，避免触发系统防火墙放行询问。查不了时返回空表。
func TCPListeners() map[int]int {
	out := map[int]int{}
	portTable(false, func(p, pid int) {
		if _, ok := out[p]; !ok {
			out[p] = pid
		}
	})
	return out
}
