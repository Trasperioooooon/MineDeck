package inst

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"minedeck/internal/mcsrc"

	"golang.org/x/sys/windows"
)

// PortProbeHost 服务器没填 server-ip（所有网卡）时，开服前试绑端口用的地址；操作层的 port.check 也用它。
// 空 = 所有网卡：服务器监听的就是所有网卡，这样查得准。
// 测试里设成 127.0.0.1：每次 go test 编出来的测试程序路径都不一样，绑所有网卡会让 Windows 防火墙每次都弹窗问要不要放行。
var PortProbeHost = ""

// soExclusiveAddrUse Windows 的 SO_EXCLUSIVEADDRUSE（按定义是 SO_REUSEADDR 取反），x/sys/windows 里没有这个常量。
const soExclusiveAddrUse = ^windows.SO_REUSEADDR

// portBusyError 服务器要绑的端口被占着，开服前就查出来了。界面把这句话直接接在「无法启动服务器」后面。
type portBusyError struct {
	port int
	// reserved 系统说「不许访问」（WSAEACCES）：别的程序独占着这个端口，或者它在 Windows 保留的端口范围里
	// （Hyper-V、WSL、Docker 会成段保留端口）。两种都只能换端口。
	reserved bool
}

func (e *portBusyError) Error() string {
	if e.reserved {
		return fmt.Sprintf("端口 %d 用不了：被别的程序独占着，或者在 Windows 保留的端口范围里，换个端口试试", e.port)
	}
	return fmt.Sprintf("端口 %d 被别的程序占着，换个端口或关掉占用它的程序", e.port)
}

// startPort 服务器启动时要绑的地址和端口：server.properties 里的 server-ip（空 = 所有网卡）和 server-port。
// 还没有 server.properties 时（第一次启动前）按服务端的默认值：所有网卡、25565（同 Port）。
// ok 为假表示不查：Velocity、Waterfall 这类代理端的端口写在它们自己的配置文件里，MineDeck 没有读它们的代码。
func startPort(i *Instance) (host string, port int, ok bool) {
	if mcsrc.KindOf(i.Core) == mcsrc.KindProxy {
		return "", 0, false
	}
	return strings.TrimSpace(javaPropsOf(i.PropsPath())["server-ip"]), i.Port(), true
}

// checkBindPort 照服务端自己绑端口的办法试绑一次，绑不上就说端口被占着。
//
// 服务端（Java）在 Windows 上绑端口时带 SO_EXCLUSIVEADDRUSE，server-ip 留空时绑的是 IPv6 双栈的通配地址。
// 以前的试绑不带这个选项，别的程序只占着 0.0.0.0 或 127.0.0.1 上的这个端口时照样绑得上，于是先起了 Java，
// 等它自己报 BindException 才知道（检查报告 D-5，测试报告 E-09）。带上以后，几种占法（0.0.0.0、127.0.0.1、[::]、[::1]、
// 别的程序独占、别的程序开了 SO_REUSEADDR）试出来的结果都和 Java 17 实际绑的结果一致（3.1.1 修这条时实测对照过）。
//
// 只认「被占着」（WSAEADDRINUSE）和「不许访问」（WSAEACCES）两种失败；别的失败（server-ip 不是这台电脑的地址、
// 域名解析不了……）这里不下结论，照常启动，由服务端自己报。
func checkBindPort(host string, port int) error {
	if host == "" {
		host = PortProbeHost
	}
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		// 设不上（比如 wine 不认这个选项）就按普通方式试绑，不因为这个报错
		return c.Control(func(fd uintptr) { _ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1) })
	}}
	ln, err := lc.Listen(context.Background(), "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		ln.Close()
		return nil
	}
	var en syscall.Errno
	if errors.As(err, &en) {
		switch en {
		case windows.WSAEADDRINUSE:
			return &portBusyError{port: port}
		case windows.WSAEACCES:
			return &portBusyError{port: port, reserved: true}
		}
	}
	return nil
}

// 重启（崩溃自动重启、定时重启、「重启」）时上一次的进程刚退出，端口一般已经放开；万一还占着，隔一会儿再查，
// 最多查这么多次，不让一下子的状态把重启取消掉。还占着的话照常去启动，由 Start 报端口被占。测试里改小。
var (
	portWaitTimes = 5
	portWaitGap   = time.Second
)

// waitPortFree 重启前等端口放开（见 portWaitTimes），等不到也返回，由接下来的 Start 报错。
func (m *Manager) waitPortFree(i *Instance) {
	host, port, ok := startPort(i)
	if !ok {
		return
	}
	for k := 0; k < portWaitTimes; k++ {
		if checkBindPort(host, port) == nil {
			return
		}
		if k == 0 {
			i.Console.Append(fmt.Sprintf("[MineDeck] 端口 %d 还被占着，等一会儿再启动", port))
		}
		time.Sleep(portWaitGap)
	}
}
