package inst

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
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
	// owner 占着它的是本软件管着的另一台服务器（没停着、朋友连的端口和这个一样，可用性走查 R08）；空 = 别的程序。
	// Java 绑端口带 SO_EXCLUSIVEADDRUSE，另一台 MineDeck 的服务器占着时系统报的也可能是「不许访问」，所以两种都要看
	owner string
	// suggest 「改成 N 并启动」的 N（只在 owner 非空时找）：见 freePortFrom；0 = 没找到
	suggest int
}

func (e *portBusyError) Error() string {
	if e.owner != "" {
		return fmt.Sprintf("端口 %d 被你的服务器「%s」占着，先停掉它，或者给这台换个端口", e.port, e.owner)
	}
	if e.reserved {
		return fmt.Sprintf("端口 %d 用不了：被别的程序独占着，或者在 Windows 保留的端口范围里，换个端口试试", e.port)
	}
	return fmt.Sprintf("端口 %d 被别的程序占着，换个端口或关掉占用它的程序", e.port)
}

// PortBusy 上一次启动时端口被占着（可用性走查 R08），实例列表里的 startPortBusy：界面按它在详情页头给
// 「改成 N 并启动」（本软件的另一台服务器占着）或「改端口」（别的程序占着）。同 LastStartError，只在内存里，进程起来了就清掉。
type PortBusy struct {
	Port    int    `json:"port"`
	Owner   string `json:"owner,omitempty"`   // 占着它的是本软件的哪台服务器；空 = 别的程序
	Suggest int    `json:"suggest,omitempty"` // 可以改成的端口（只在 Owner 非空时给）；0 = 没找到
}

// portPeer 本软件管着的另一台服务器用的端口：朋友连的那个（ConnectPort，代理端读它自己的配置），live 是它没停着。
type portPeer struct {
	name string
	port int
	live bool
}

// portPeers 除 self 以外的服务器各用哪个端口（读不出端口的代理端不算）。要查别的服务器的状态、拿它们的锁，
// 所以调用时不能拿着任何一台服务器的 procMu：两台同端口的服务器同时启动时，各拿着自己的锁去查对方会互相等。
func (m *Manager) portPeers(self *Instance) []portPeer {
	var out []portPeer
	for _, o := range m.List() {
		if o == self {
			continue
		}
		if p := o.ConnectPort(); p > 0 {
			out = append(out, portPeer{name: o.Name, port: p, live: o.Status() != "stopped"})
		}
	}
	return out
}

// portSuggestTries 找「改成 N」的 N 时最多试绑几个端口。
const portSuggestTries = 200

// freePortFrom 「改成 N 并启动」的 N：从被占的端口往上，本软件别的服务器都没配、现在照服务端的绑法也绑得上的第一个端口。
// 从被占的端口往上找，不从 25565 找：改完离原来的近，也不会把人往 25565 这种最常被占的端口上带。都不行返回 0。
func freePortFrom(t bindTarget, peers []portPeer) int {
	taken := map[int]bool{}
	for _, p := range peers {
		taken[p.port] = true
	}
	tried := 0
	for p := t.port + 1; p <= 65535 && tried < portSuggestTries; p++ {
		if taken[p] {
			continue
		}
		tried++
		c := t
		c.port = p
		if checkBindPort(c) == nil {
			return p
		}
	}
	return 0
}

// PortUse 一台服务器用的端口（port.suggest 列给界面，手填的端口撞上了当场提示）。
type PortUse struct {
	Port     int    `json:"port"`
	Instance string `json:"instance"`
	Status   string `json:"status"`
}

// PortsInUse 本软件管着的服务器各用哪个端口（朋友连的那个；代理端读它自己的配置，还没生成配置的不算），except 那台不算。
// 按端口从小到大排，同一个端口几台时按名字排。
func (m *Manager) PortsInUse(except string) []PortUse {
	out := []PortUse{}
	for _, i := range m.List() {
		if i.Name == except {
			continue
		}
		if p := i.ConnectPort(); p > 0 {
			out = append(out, PortUse{Port: p, Instance: i.Name, Status: i.Status()})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Port != out[b].Port {
			return out[a].Port < out[b].Port
		}
		return out[a].Instance < out[b].Instance
	})
	return out
}

// DefaultPort 新建、导入整合包、接管、用已有地图开服时端口的默认值（可用性走查 R08）：从 from（不在 1–65535 内按 25565）起，
// used 里都没有的最小端口。usedBy 是 from 被哪台服务器用着（from 本来就空着时为空串），界面拿它说「25565 已被『X』使用，已改为 25566」。
// 只看本软件管着的服务器，不看别的程序：别的程序占着的，启动时会查出来（见 checkBindPort）。
func DefaultPort(used []PortUse, from int) (port int, usedBy string) {
	return DefaultPortSkipping(used, from, nil)
}

// DefaultPortSkipping 同 DefaultPort，另外跳过 busy(端口) 为真的端口：新建、导入整合包、用已有地图开服时，被别的程序占着的也不给
// （beta.3 回访走查 11-16：以前只避开 MineDeck 自己的服务器，默认 25565 被别的程序占着，开服才报）。busy 为 nil 时同 DefaultPort。
// usedBy 照旧只说 from 被本软件哪台服务器用着。
func DefaultPortSkipping(used []PortUse, from int, busy func(port int) bool) (port int, usedBy string) {
	if from < 1 || from > 65535 {
		from = 25565
	}
	taken := map[int]string{}
	for _, u := range used {
		if _, ok := taken[u.Port]; !ok {
			taken[u.Port] = u.Instance
		}
	}
	usedBy = taken[from]
	for p := from; p <= 65535; p++ {
		if _, ok := taken[p]; !ok && (busy == nil || !busy(p)) {
			return p, usedBy
		}
	}
	return from, usedBy // 从 from 往上全被占了（不会真碰上）：照原样给
}

// bindTarget 服务器启动时要绑的地址。network 是 "tcp"（Java 默认：server-ip 留空时绑 IPv6 双栈的通配地址），
// JVM 参数里有 -Djava.net.preferIPv4Stack=true 时是 "tcp4"：这时 Java 只用 IPv4，server-ip 留空绑的是 0.0.0.0，
// 别的程序只在 IPv6 上占着这个端口不妨碍它，按双栈去查就冤枉了。
type bindTarget struct {
	network, host string
	port          int
}

// startPort 服务器启动时要绑的地址和端口：server.properties 里的 server-ip（空 = 所有网卡）和 server-port。
// 还没有 server.properties 时（第一次启动前）按服务端的默认值：所有网卡、25565（同 Port）。jvm 是这台服务器的 JVM 参数（ExtraJVM）。
// ok 为假表示不查：Velocity、Waterfall 这类代理端的端口写在它们自己的配置文件里，MineDeck 没有读它们的代码；
// server-port 写的是 0 的，服务端会自己挑一个空端口（Port 遇到 0 按 25565 算，拿 25565 去查会冤枉它）。
func startPort(i *Instance, jvm []string) (t bindTarget, ok bool) {
	if mcsrc.KindOf(i.Core) == mcsrc.KindProxy {
		return bindTarget{}, false
	}
	props := javaPropsOf(i.PropsPath())
	if v, has := props["server-port"]; has && strings.TrimSpace(v) == "0" {
		return bindTarget{}, false
	}
	host := strings.TrimSpace(props["server-ip"])
	// 「[::1]」这种带方括号的 IPv6 写法 Java 认；JoinHostPort 会再套一层括号，绑不了，这里先去掉
	if len(host) > 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	t = bindTarget{network: "tcp", host: host, port: i.Port()}
	if preferIPv4Stack(jvm) {
		t.network = "tcp4"
	}
	return t, true
}

// preferIPv4Stack JVM 参数里是不是打开了 java.net.preferIPv4Stack。Java 读它时不分大小写、不去空格，同一个属性写了几次以最后一次为准。
func preferIPv4Stack(jvm []string) bool {
	on := false
	for _, a := range jvm {
		if v, ok := strings.CutPrefix(a, "-Djava.net.preferIPv4Stack="); ok {
			on = strings.EqualFold(v, "true")
		}
	}
	return on
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
func checkBindPort(t bindTarget) error {
	host, port := t.host, t.port
	if host == "" {
		host = PortProbeHost
	}
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		// 设不上（比如 wine 不认这个选项）就按普通方式试绑，不因为这个报错
		return c.Control(func(fd uintptr) {
			_ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
		})
	}}
	// server-ip 写的是域名时要先解析：给个时限，解析卡住也不让「启动」一直等着（Start 调这里时拿着这台服务器的锁）。
	// 超时算「别的失败」，照常启动
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ln, err := lc.Listen(ctx, t.network, net.JoinHostPort(host, strconv.Itoa(port)))
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
// 调用时不能拿着 procMu（要查状态、取 JVM 参数）。
func (m *Manager) waitPortFree(i *Instance) {
	t, ok := startPort(i, i.ExtraJVMSnapshot())
	if !ok {
		return
	}
	for k := 0; k < portWaitTimes; k++ {
		if i.Status() != "stopped" {
			return // 等的时候已经有人把它启动了（比如用户点了「启动」）：端口是它自己占的，不用再等
		}
		if checkBindPort(t) == nil {
			return
		}
		if k == 0 {
			i.Console.Append(fmt.Sprintf("[MineDeck] 端口 %d 还被占着，等一会儿再启动", t.port))
		}
		time.Sleep(portWaitGap)
	}
}

// restartGaveUp 重启（崩溃自动重启、定时重启）时 Start 报端口被占：等过几秒了还占着，多半是别的程序一直占着，不再重试。
// 控制台上把这一点说清楚，免得以为过一会儿还会自己起来。
func restartGaveUp(err error) string {
	var busy *portBusyError
	if errors.As(err, &busy) {
		return "。不再自动重试，端口放开以后请手动启动"
	}
	return ""
}
