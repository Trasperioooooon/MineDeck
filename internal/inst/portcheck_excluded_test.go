package inst

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"minedeck/internal/mcsrc"
)

func TestParseExcludedPortRanges(t *testing.T) {
	// 带 * 的行（管理的端口排除）在 Windows 上实测可以正常绑定，不算系统保留段；只收不带 * 的两列数字行。
	en := `
Protocol tcp Port Exclusion Ranges

Start Port    End Port
----------    --------
     25500       25600
     50000       50059     *
     60100       60120

* - Administered port exclusions.
`
	gotEn := parseExcludedPortRanges(en)
	wantEn := []portRange{{25500, 25600}, {60100, 60120}}
	if !reflect.DeepEqual(gotEn, wantEn) {
		t.Fatalf("英文 netsh 解析 = %+v，应为 %+v（带 * 的行不收）", gotEn, wantEn)
	}

	zh := "\r\n协议 tcp 端口排除范围\r\n\r\n开始端口      结束端口\r\n----------    --------\r\n      1024        1123\r\n     25565       25565     *\r\n     49670       49769\r\n\r\n* - 管理的端口排除。\r\n"
	gotZh := parseExcludedPortRanges(zh)
	wantZh := []portRange{{1024, 1123}, {49670, 49769}}
	if !reflect.DeepEqual(gotZh, wantZh) {
		t.Fatalf("中文 netsh 解析 = %+v，应为 %+v（带 * 的行不收）", gotZh, wantZh)
	}

	bad := `
0 100
25600 25500
65536 70000
100 200 extra
100 200 *
not numbers
`
	if got := parseExcludedPortRanges(bad); len(got) != 0 {
		t.Fatalf("非法行或带 * 行不应被收入保留段：%+v", got)
	}
}

func TestExcludedPortsCacheAndFallback(t *testing.T) {
	oldRun, oldNow, oldQuery := runNetshExcluded, excludedNow, queryExcludedPorts
	resetExcludedCache()
	t.Cleanup(func() {
		runNetshExcluded, excludedNow, queryExcludedPorts = oldRun, oldNow, oldQuery
		resetExcludedCache()
	})
	queryExcludedPorts = loadExcludedPorts

	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	excludedNow = func() time.Time { return now }

	calls := 0
	outV4 := "Start Port End Port\n25560 25570\n26000 26010 *\n"
	outV6 := "Start Port End Port\n30000 30010 *\n31000 31010\n"
	var runErr error
	runNetshExcluded = func(ipv6 bool) (string, error) {
		calls++
		if runErr != nil {
			return "", runErr
		}
		if ipv6 {
			return outV6, nil
		}
		return outV4, nil
	}

	if !IsPortReserved(25565) || IsPortReserved(26005) || IsPortReserved(30005) || !IsPortReserved(31005) || IsPortReserved(25571) {
		t.Fatalf("第一次查询保留段结果不对（带 * 的 26005、30005 不应算保留）")
	}
	if calls != 2 {
		t.Fatalf("首次查询应跑 ipv4+ipv6 两次 netsh，实际 %d 次", calls)
	}

	// 1 分钟内再次查询应走缓存，不再起 netsh
	now = now.Add(30 * time.Second)
	outV4 = "Start Port End Port\n40000 40010\n"
	if !IsPortReserved(25565) || IsPortReserved(40005) || calls != 2 {
		t.Fatalf("1 分钟内应命中缓存：calls=%d", calls)
	}

	// 超过 1 分钟后重新查询
	now = now.Add(31 * time.Second)
	if IsPortReserved(25565) || !IsPortReserved(40005) || calls != 4 {
		t.Fatalf("缓存过期后应重新查询：calls=%d", calls)
	}

	// netsh 失败或超时：当没有保留段，不报错阻塞
	now = now.Add(2 * time.Minute)
	runErr = errors.New("exit status 1")
	if IsPortReserved(40005) || calls != 6 {
		t.Fatalf("查询失败应回退成无保留段：calls=%d", calls)
	}
}

func TestReservedPortSkippedAndStartError(t *testing.T) {
	t.Setenv(fakeServerEnv, "1")
	oldQuery, oldRun, oldListen := queryExcludedPorts, runNetshExcluded, listenBindPort
	resetExcludedCache()
	t.Cleanup(func() {
		queryExcludedPorts, runNetshExcluded, listenBindPort = oldQuery, oldRun, oldListen
		resetExcludedCache()
	})

	queryExcludedPorts = func() ([]portRange, []portRange) {
		return []portRange{{25565, 25567}}, []portRange{{25570, 25572}}
	}

	// DefaultPortSkipping 预查默认端口时把保留段也跳过（表单和建议端口不试绑，用 netsh 结果）
	if p, by := DefaultPortSkipping(nil, 25565, func(int) bool { return false }); p != 25568 || by != "" {
		t.Fatalf("DefaultPortSkipping 应跳过保留段 25565..25567，得到 %d %q", p, by)
	}
	if p, _ := DefaultPortSkipping(nil, 25570, func(int) bool { return false }); p != 25573 {
		t.Fatalf("DefaultPortSkipping 应跳过 IPv6 保留段 25570..25572，得到 %d", p)
	}

	// 1) R1 关键用例：开服前检查以试绑为准。即使 netsh 说某个端口「被保留」（不带 *），只要实际空着绑得上，Start 就放行。
	lnFree, freePort := occupy(t, "127.0.0.1")
	_ = lnFree.Close()
	resetExcludedCache()
	queryExcludedPorts = loadExcludedPorts
	runNetshExcluded = func(ipv6 bool) (string, error) {
		if ipv6 {
			return "", nil
		}
		return fmt.Sprintf("Start Port End Port\n%d %d\n", freePort, freePort), nil
	}
	mOK, iOK := portInst(t, "绑得上就放行", fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", freePort), mcsrc.CoreVanilla)
	if err := mOK.Start(iOK.Name); err != nil {
		t.Fatalf("端口实际空着绑得上时，即使 netsh 列为保留也应放行启动，实际报错：%v", err)
	}
	t.Cleanup(func() {
		if iOK.Status() != "stopped" {
			_ = mOK.Kill(iOK.Name)
			waitStatus(t, iOK, "stopped")
		}
	})
	waitStatus(t, iOK, "running")
	_ = mOK.Kill(iOK.Name)
	waitStatus(t, iOK, "stopped")

	// 2) 试绑报 WSAEACCES（errAddrAccess）时，再去查保留段：落在段里说「系统保留」，不在段里照原样说。
	lnProbe, errPort := occupy(t, "127.0.0.1")
	_ = lnProbe.Close()
	listenBindPort = func(ctx context.Context, network, address string) (net.Listener, error) {
		return nil, &net.OpError{Op: "listen", Net: network, Err: errAddrAccess}
	}
	queryExcludedPorts = func() ([]portRange, []portRange) {
		return []portRange{{errPort, errPort}}, nil
	}
	mFail, iFail := portInst(t, "保留段测试", fmt.Sprintf("server-port=%d\n", errPort), mcsrc.CoreVanilla)
	err := mFail.Start(iFail.Name)
	var busy *portBusyError
	if err == nil || !errors.As(err, &busy) {
		t.Fatalf("试绑报 WSAEACCES 应拒绝启动：%v", err)
	}
	if !strings.Contains(err.Error(), "系统保留") {
		t.Errorf("命中保留段时报错应说明被系统保留：%s", err)
	}
	if n := len([]rune(err.Error())); n > 60 || strings.Contains(err.Error(), ": ") {
		t.Errorf("报错需满足 60 字以内且无半角冒号：%s（%d 字）", err, n)
	}
	if pb := iFail.LastStartPortBusy(); pb == nil || pb.Port != errPort {
		t.Errorf("应记下 startPortBusy：%+v", pb)
	}

	// 保留段查不到时，WSAEACCES 照原来的说法，不谎称一定在系统保留段里
	queryExcludedPorts = func() ([]portRange, []portRange) { return nil, nil }
	if err2 := checkBindPort(bindTarget{network: "tcp", host: "127.0.0.1", port: errPort}); err2 == nil || strings.Contains(err2.Error(), "在系统保留的端口范围里（Hyper-V") {
		t.Errorf("不在保留段里的 WSAEACCES 不应说成查到了系统保留段：%v", err2)
	}

	// 仅 IPv6 保留段时（用 :0 拿到的端口，不写死固定端口）：preferIPv4Stack=true（tcp4）不认作系统保留，双栈通配认作系统保留
	queryExcludedPorts = func() ([]portRange, []portRange) {
		return nil, []portRange{{errPort, errPort}}
	}
	if err4 := checkBindPort(bindTarget{network: "tcp4", host: "127.0.0.1", port: errPort}); err4 == nil || strings.Contains(err4.Error(), "在系统保留的端口范围里（Hyper-V") {
		t.Errorf("仅 IPv4 栈遇 WSAEACCES 不应命中纯 IPv6 保留段：%v", err4)
	}
	if err6 := checkBindPort(bindTarget{network: "tcp", host: "", port: errPort}); err6 == nil || !strings.Contains(err6.Error(), "系统保留") {
		t.Errorf("双栈通配遇 WSAEACCES 应命中 IPv6 保留段：%v", err6)
	}
}
