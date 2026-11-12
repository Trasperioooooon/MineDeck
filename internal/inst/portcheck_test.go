package inst

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/mcsrc"
)

// occupy 占住 127.0.0.1（或给的地址）上的一个空端口，测试结束时放开。测试里只占回环地址，免得 Windows 防火墙弹窗。
func occupy(t *testing.T, host string) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// portInst 一台用 server.properties（props 为空串时不写这个文件）的实例；「Java」是测试程序自己（扮演服务器，见 fakeServer）。
func portInst(t *testing.T, name, props string, core mcsrc.Core) (*Manager, *Instance) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if props != "" {
		if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	i := &Instance{Settings: Settings{Name: name, Core: core, JavaPath: exe, XmxMB: 64, XmsMB: 64, LaunchTarget: "jar:server.jar"},
		Dir: dir, Console: newConsole()}
	return &Manager{insts: map[string]*Instance{name: i}, creating: map[string]bool{}}, i
}

// 端口被占着：「启动」直接报错（一句能直接显示的中文），不起服务器进程（检查报告 D-5）。
// 查的是服务器真正要绑的地址：server-ip 填了就按它查，别的地址上占着同一个端口不算。
func TestStartRejectsBusyPort(t *testing.T) {
	_, port := occupy(t, "127.0.0.1")
	want := fmt.Sprintf("端口 %d 被别的程序占着，换个端口或关掉占用它的程序", port)
	for _, props := range []string{
		fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port),
		fmt.Sprintf("server-port=%d\n", port), // 没填 server-ip：所有网卡（测试里用 PortProbeHost 代替）
	} {
		m, i := portInst(t, "端口测试", props, mcsrc.CoreVanilla)
		err := m.Start(i.Name)
		var busy *portBusyError
		if err == nil || !errors.As(err, &busy) || err.Error() != want {
			t.Fatalf("%q：端口被占着应拒绝启动，报「%s」：%v", props, want, err)
		}
		if n := []rune(err.Error()); len(n) > 60 || strings.Contains(err.Error(), ": ") {
			t.Errorf("报错要能在界面上直接显示（60 字以内、没有半角冒号）：%s", err)
		}
		if st := i.Status(); st != "stopped" {
			t.Errorf("没启动，状态应还是 stopped：%s", st)
		}
		if slices.ContainsFunc(i.Console.Lines(), func(l string) bool { return strings.Contains(l, "[MineDeck] 启动: java") }) {
			t.Errorf("端口被占着不该起服务器进程：%q", i.Console.Lines())
		}
	}
	// server-ip 填的是另一个回环地址：127.0.0.1 上占着同一个端口不妨碍它，服务端能绑上，这里也不拦
	if err := checkBindPort("127.0.0.2", port); err != nil {
		t.Errorf("别的地址上占着同一个端口不算被占：%v", err)
	}
	if _, port2 := occupy(t, "127.0.0.2"); checkBindPort("127.0.0.2", port2) == nil {
		t.Error("server-ip 上占着的端口应查得出来")
	}
	// 查不了的情况（地址不是这台电脑的）不下结论，交给服务端自己报
	if err := checkBindPort("192.0.2.1", port); err != nil {
		t.Errorf("绑不上的原因不是被占着时，不该说端口被占：%v", err)
	}
}

// 端口空着就照常启动；没有 server.properties（第一次启动前）按默认的 25565 查，不会因为读不到配置拦下。
func TestStartPortFreeProceeds(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	ln, port := occupy(t, "127.0.0.1")
	ln.Close() // 先占一个空端口再放开：拿到一个刚好空着的端口
	m, i := portInst(t, "端口空着", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port), mcsrc.CoreVanilla)
	if err := m.Start(i.Name); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, i, "running")
	_ = m.Kill(i.Name)
	waitStatus(t, i, "stopped")

	_, i2 := portInst(t, "没有配置", "", mcsrc.CoreVanilla)
	if host, p, ok := startPort(i2); !ok || host != "" || p != 25565 {
		t.Errorf("没有 server.properties 时按所有网卡、25565 查：%q %d %v", host, p, ok)
	}
	// 代理端的端口在它自己的配置文件里：不查（server.properties 里的数不是它监听的）
	_, px := portInst(t, "代理", "server-port=25565\n", mcsrc.CoreVelocity)
	if _, _, ok := startPort(px); ok {
		t.Error("代理端不该按 server.properties 查端口")
	}
}

// 重启前（崩溃自动重启、定时重启、「重启」）端口还没放开：隔一会儿再查，放开了就接着启动；一直占着就等够次数后交给 Start 报错。
func TestWaitPortFree(t *testing.T) {
	oldTimes, oldGap := portWaitTimes, portWaitGap
	t.Cleanup(func() { portWaitTimes, portWaitGap = oldTimes, oldGap })
	portWaitTimes, portWaitGap = 40, 50*time.Millisecond // 一直等满是 2 秒

	ln, port := occupy(t, "127.0.0.1")
	m, i := portInst(t, "重启测试", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port), mcsrc.CoreVanilla)
	go func() { time.Sleep(200 * time.Millisecond); ln.Close() }()
	start := time.Now()
	m.waitPortFree(i)
	if d := time.Since(start); d < 150*time.Millisecond || d > 1500*time.Millisecond {
		t.Errorf("应等到端口放开再返回，等了 %v", d)
	}
	if checkBindPort("127.0.0.1", port) != nil {
		t.Error("返回时端口应已放开")
	}
	if lines := i.Console.Lines(); len(lines) != 1 || !strings.Contains(lines[0], fmt.Sprintf("端口 %d 还被占着", port)) {
		t.Errorf("等的时候控制台说一句（只说一次）：%q", lines)
	}

	// 一直占着：等够次数就返回，Start 照常报端口被占
	_, port2 := occupy(t, "127.0.0.1")
	m2, i2 := portInst(t, "一直占着", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port2), mcsrc.CoreVanilla)
	portWaitTimes = 3
	start = time.Now()
	m2.waitPortFree(i2)
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Errorf("一直占着时应等够次数：%v", d)
	}
	var busy *portBusyError
	if err := m2.Start(i2.Name); !errors.As(err, &busy) {
		t.Errorf("等不到端口放开，Start 应报端口被占：%v", err)
	}
}

// 真正的问题场景：别的程序只占着 0.0.0.0 上的端口（IPv4 通配），服务器没填 server-ip。以前的试绑（IPv6 双栈、不独占）
// 照样绑得上，查不出来；Java 却绑不上。要绑所有网卡，Windows 防火墙会为测试程序弹窗，所以平时跳过：
// 设 MINEDECK_TEST_WILDCARD=1 再跑（go test ./internal/inst -run Wildcard）。
func TestBusyPortOnWildcard(t *testing.T) {
	if os.Getenv("MINEDECK_TEST_WILDCARD") != "1" {
		t.Skip("要绑所有网卡（防火墙会弹窗），设 MINEDECK_TEST_WILDCARD=1 再跑")
	}
	old := PortProbeHost
	t.Cleanup(func() { PortProbeHost = old })
	PortProbeHost = "" // 和正式运行时一样
	for _, occ := range []struct{ network, host string }{{"tcp4", "0.0.0.0"}, {"tcp4", "127.0.0.1"}, {"tcp", "::"}, {"tcp6", "::1"}} {
		ln, err := net.Listen(occ.network, net.JoinHostPort(occ.host, "0"))
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		if legacy, err := net.Listen("tcp", fmt.Sprintf(":%d", port)); err == nil { // 以前的试绑：记一下它查不查得出来
			legacy.Close()
			t.Logf("占着 %s:%d 时，以前的试绑查不出来", occ.host, port)
		}
		var busy *portBusyError
		if err := checkBindPort("", port); !errors.As(err, &busy) {
			t.Errorf("占着 %s:%d，没填 server-ip 时应查出被占：%v", occ.host, port, err)
		}
		ln.Close()
	}
}
