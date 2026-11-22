package inst

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"minedeck/internal/mcsrc"
)

func TestParseExcludedPortRanges(t *testing.T) {
	en := `
Protocol tcp Port Exclusion Ranges

Start Port    End Port
----------    --------
     25500       25600
     50000       50059     *

* - Administered port exclusions.
`
	gotEn := parseExcludedPortRanges(en)
	wantEn := []portRange{{25500, 25600}, {50000, 50059}}
	if !reflect.DeepEqual(gotEn, wantEn) {
		t.Fatalf("英文 netsh 解析 = %+v，应为 %+v", gotEn, wantEn)
	}

	zh := "\r\n协议 tcp 端口排除范围\r\n\r\n开始端口      结束端口\r\n----------    --------\r\n      1024        1123\r\n     25565       25565     *\r\n     49670       49769\r\n\r\n* - 管理的端口排除。\r\n"
	gotZh := parseExcludedPortRanges(zh)
	wantZh := []portRange{{1024, 1123}, {25565, 25565}, {49670, 49769}}
	if !reflect.DeepEqual(gotZh, wantZh) {
		t.Fatalf("中文 netsh 解析 = %+v，应为 %+v", gotZh, wantZh)
	}

	bad := `
0 100
25600 25500
65536 70000
100 200 extra
not numbers
`
	if got := parseExcludedPortRanges(bad); len(got) != 0 {
		t.Fatalf("非法行不应被收入保留段：%+v", got)
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
	outV4 := "Start Port End Port\n25560 25570\n"
	outV6 := "Start Port End Port\n30000 30010 *\n"
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

	if !IsPortReserved(25565) || !IsPortReserved(30005) || IsPortReserved(25571) {
		t.Fatalf("第一次查询保留段结果不对")
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
	oldQuery := queryExcludedPorts
	t.Cleanup(func() { queryExcludedPorts = oldQuery })
	queryExcludedPorts = func() ([]portRange, []portRange) {
		return []portRange{{25565, 25567}}, []portRange{{25570, 25572}}
	}

	// DefaultPortSkipping 预查默认端口时把保留段也跳过
	if p, by := DefaultPortSkipping(nil, 25565, func(int) bool { return false }); p != 25568 || by != "" {
		t.Fatalf("DefaultPortSkipping 应跳过保留段 25565..25567，得到 %d %q", p, by)
	}
	if p, _ := DefaultPortSkipping(nil, 25570, func(int) bool { return false }); p != 25573 {
		t.Fatalf("DefaultPortSkipping 应跳过 IPv6 保留段 25570..25572，得到 %d", p)
	}

	// 开服前检查端口：落在保留段里直接报错，写明是被系统保留了，且满足界面直显要求（60 字内、无半角冒号）
	m, i := portInst(t, "保留段测试", "server-port=25566\n", mcsrc.CoreVanilla)
	err := m.Start(i.Name)
	var busy *portBusyError
	if err == nil || !errors.As(err, &busy) {
		t.Fatalf("保留段里的端口应拒绝启动：%v", err)
	}
	if !strings.Contains(err.Error(), "系统保留") {
		t.Errorf("报错应说明被系统保留：%s", err)
	}
	if n := len([]rune(err.Error())); n > 60 || strings.Contains(err.Error(), ": ") {
		t.Errorf("报错需满足 60 字以内且无半角冒号：%s（%d 字）", err, n)
	}
	if pb := i.LastStartPortBusy(); pb == nil || pb.Port != 25566 {
		t.Errorf("应记下 startPortBusy：%+v", pb)
	}

	// 打开了 preferIPv4Stack=true 时只用 IPv4，纯 IPv6 保留段不冤枉它；双栈通配时则拦下
	if err := checkBindPort(bindTarget{network: "tcp4", host: "127.0.0.1", port: 25571}); err != nil {
		t.Errorf("仅 IPv4 栈不应被 IPv6 保留段拦下：%v", err)
	}
	if err := checkBindPort(bindTarget{network: "tcp", host: "", port: 25571}); err == nil || !strings.Contains(err.Error(), "系统保留") {
		t.Errorf("双栈通配应被 IPv6 保留段拦下：%v", err)
	}
	_ = fmt.Sprint
}
