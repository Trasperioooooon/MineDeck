package catalog

import (
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCPTable = iphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUDPTable = iphlpapi.NewProc("GetExtendedUdpTable")
)

const (
	tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER：只列在监听的
	udpTableOwnerPID         = 1 // UDP_TABLE_OWNER_PID
	afInet                   = 2
	afInet6                  = 23
)

// portOwners 查哪些进程在监听这个端口（TCP 只看监听的，UDP 看绑定的），IPv4 和 IPv6 都查。查不到返回空。
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

// tcpListeners 这台电脑上正在监听的 TCP 端口，端口 → 第一个查到的进程编号（IPv4 和 IPv6 都算）。
// 只读一次系统的监听表，不试绑端口：推荐默认端口时要连着看好多个，而且试绑所有网卡会让防火墙弹窗问要不要放行（见 inst.PortProbeHost）。
// 查不了（没有这个系统调用）返回空表。
func tcpListeners() map[int]int {
	out := map[int]int{}
	portTable(false, func(p, pid int) {
		if _, ok := out[p]; !ok {
			out[p] = pid
		}
	})
	return out
}

// portTable 把系统的 TCP 监听表（udp 为真时是 UDP 绑定表）逐行交给 fn(端口, 进程编号)，IPv4 和 IPv6 都给；进程编号为 0 的行不给。
func portTable(udp bool, fn func(port, pid int)) {
	for _, af := range []uint32{afInet, afInet6} {
		proc, class := procGetExtendedTCPTable, uint32(tcpTableOwnerPIDListener)
		// 每一行的长度、端口和进程编号在行里的位置（字节）
		rowLen, portOff, pidOff := 24, 8, 20 // MIB_TCPROW_OWNER_PID
		if af == afInet6 {
			rowLen, portOff, pidOff = 56, 20, 52 // MIB_TCP6ROW_OWNER_PID
		}
		if udp {
			proc, class = procGetExtendedUDPTable, udpTableOwnerPID
			rowLen, portOff, pidOff = 12, 4, 8 // MIB_UDPROW_OWNER_PID
			if af == afInet6 {
				rowLen, portOff, pidOff = 28, 20, 24 // MIB_UDP6ROW_OWNER_PID
			}
		}
		if proc.Find() != nil {
			return
		}
		var size uint32
		_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(af), uintptr(class), 0)
		if size == 0 || size > 16<<20 {
			continue
		}
		buf := make([]byte, size)
		r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), uintptr(class), 0)
		if r != 0 || len(buf) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(buf[0:4]))
		for k := 0; k < n; k++ {
			row := 4 + k*rowLen
			if row+rowLen > len(buf) {
				break
			}
			// 端口是网络字节序，放在这个 DWORD 的低 16 位
			p := int(buf[row+portOff])<<8 | int(buf[row+portOff+1])
			pid := int(binary.LittleEndian.Uint32(buf[row+pidOff:]))
			if pid > 0 {
				fn(p, pid)
			}
		}
	}
}

// processName 进程的程序名（如 java.exe）；查不到返回空串。
func processName(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return baseExe(windows.UTF16ToString(buf[:n]))
}
