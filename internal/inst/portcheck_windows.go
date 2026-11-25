package inst

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"minedeck/internal/procutil"

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

// defaultRunNetshExcluded 不弹窗运行 netsh 查 Windows 保留的 TCP 端口段（限时 3 秒）。
func defaultRunNetshExcluded(ipv6 bool) (string, error) {
	family := "ipv4"
	if ipv6 {
		family = "ipv6"
	}
	exe := "netsh.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		if cand := filepath.Join(root, "System32", "netsh.exe"); filepath.IsAbs(cand) {
			exe = cand
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "interface", family, "show", "excludedportrange", "protocol=tcp")
	procutil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTCPTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	tcpTableOwnerPIDListener = 3
	afInet                   = 2
	afInet6                  = 23
)

// defaultListenPorts 读取系统的 TCP 监听表（IPv4 和 IPv6），不试绑端口，不触发防火墙弹窗。
func defaultListenPorts() map[int]int {
	out := map[int]int{}
	if procGetExtendedTCPTable.Find() != nil {
		return out
	}
	for _, af := range []uint32{afInet, afInet6} {
		rowLen, portOff, pidOff := 24, 8, 20
		if af == afInet6 {
			rowLen, portOff, pidOff = 56, 20, 52
		}
		var size uint32
		_, _, _ = procGetExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPIDListener, 0)
		if size == 0 || size > 16<<20 {
			continue
		}
		buf := make([]byte, size+256)
		size = uint32(len(buf))
		r, _, _ := procGetExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPIDListener, 0)
		if r != 0 || len(buf) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(buf[0:4]))
		for k := 0; k < n; k++ {
			row := 4 + k*rowLen
			if row+rowLen > len(buf) {
				break
			}
			p := int(buf[row+portOff])<<8 | int(buf[row+portOff+1])
			pid := int(binary.LittleEndian.Uint32(buf[row+pidOff:]))
			if pid > 0 {
				if _, ok := out[p]; !ok {
					out[p] = pid
				}
			}
		}
	}
	return out
}

func defaultProcessName(pid int) string {
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
	return filepath.Base(strings.ReplaceAll(windows.UTF16ToString(buf[:n]), `\`, "/"))
}

// extraBindHosts Windows 下通配地址按 SO_EXCLUSIVEADDRUSE 试绑一次即可覆盖回环与所有网卡，无需额外地址。
func extraBindHosts(_, _ string) []string { return nil }
