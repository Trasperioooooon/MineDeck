package portprobe

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
		// 两次调用之间如果多出几个监听端口，size+256 留出富余
		buf := make([]byte, size+256)
		size = uint32(len(buf))
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

// ProcessName 进程的程序名（如 java.exe）；查不到返回空串。
func ProcessName(pid int) string {
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
	return BaseExe(windows.UTF16ToString(buf[:n]))
}
