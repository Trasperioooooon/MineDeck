package main

import (
	"testing"
	"time"

	"minedeck/internal/app"
)

// 关窗：开了「最小化到托盘」（且托盘图标在）就收进托盘；否则有服务器在运行、界面又能问时先确认；其余直接关。
func TestDecideClose(t *testing.T) {
	cases := []struct {
		name            string
		minToTray, tray bool
		running         int
		askable         bool
		want            closeAction
	}{
		{"开了托盘：收进托盘，不管有没有服务器", true, true, 2, true, closeToTray},
		{"开了托盘但图标登记失败：不藏（藏了没法恢复），有服务器就确认", true, false, 1, true, closeConfirm},
		{"开了托盘但图标登记失败、没服务器：直接关", true, false, 0, true, closeDefault},
		{"没开托盘、有服务器在跑：先确认", false, true, 1, true, closeConfirm},
		{"没开托盘、没服务器：照旧直接关", false, true, 0, true, closeDefault},
		{"没开托盘、有服务器，但界面失联：直接关，不困住窗口", false, true, 3, false, closeDefault},
	}
	for _, c := range cases {
		if got := decideClose(c.minToTray, c.tray, c.running, c.askable); got != c.want {
			t.Errorf("%s：得到 %d，应为 %d", c.name, got, c.want)
		}
	}
}

// 界面失联的兜底：请过确认、超时还没回应，下一次关窗不再问；回应过就一直能问。
func TestCloseAskerUsable(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	var a closeAsker
	if !a.usable(t0) {
		t.Error("没请过确认时应能问")
	}
	a.askedAt = t0
	if !a.usable(t0.Add(closeAckTimeout - time.Millisecond)) {
		t.Error("还没等满超时，应能再问（确认框可能正在弹）")
	}
	if a.usable(t0.Add(closeAckTimeout)) {
		t.Error("超时没回应：页面失联，应直接关")
	}
	if !a.usable(t0.Add(closeAskStale)) {
		t.Error("太久以前那次没回应的请求已经作废，应能再问")
	}
	a.acked = true
	if !a.usable(t0.Add(time.Hour)) {
		t.Error("回应过就应能问")
	}
}

// onClose 串起来：没开托盘、有服务器在跑 → 请界面确认并记下时间；界面没回应，超时后的下一次直接关。
func TestOnCloseAsksThenFallsBack(t *testing.T) {
	app.Base = t.TempDir() // config.json 落到临时目录；MinimizeToTray 默认关
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.MinimizeToTray = false; return nil }); err != nil {
		t.Fatal(err)
	}
	oldRun, oldAsk, oldTray, oldState := runningServers, askCloseUI, trayActive, closeAsk
	defer func() { runningServers, askCloseUI, trayActive, closeAsk = oldRun, oldAsk, oldTray, oldState }()

	running, asked := 2, 0
	var gotTray bool
	var gotRunning int
	runningServers = func() int { return running }
	askCloseUI = func(tray bool, n int) { asked++; gotTray, gotRunning = tray, n }
	trayActive = true
	closeAsk = closeAsker{}

	t0 := time.Now()
	if act := onClose(t0, true); act != closeConfirm || asked != 1 || !gotTray || gotRunning != 2 {
		t.Fatalf("第一次关窗应请界面确认：act=%d asked=%d tray=%v running=%d", act, asked, gotTray, gotRunning)
	}
	if act := onClose(t0.Add(closeAckTimeout+time.Second), true); act != closeDefault || asked != 1 {
		t.Fatalf("界面超时没回应，下一次应直接关：act=%d asked=%d", act, asked)
	}

	closeAsk = closeAsker{}
	onClose(t0, true)
	hostCloseAck() // 界面回应了
	if act := onClose(t0.Add(time.Minute), true); act != closeConfirm || asked != 3 {
		t.Fatalf("界面回应过，之后关窗应照常确认：act=%d asked=%d", act, asked)
	}

	// 窗口藏着（静默启动、没开托盘开关）时收到关闭：用户看不见确认框，照旧直接退出
	if act := onClose(t0.Add(90*time.Second), false); act != closeDefault || asked != 3 {
		t.Fatalf("窗口藏着时应直接关：act=%d asked=%d", act, asked)
	}

	running = 0
	if act := onClose(t0.Add(2*time.Minute), true); act != closeDefault {
		t.Fatalf("没有服务器在跑应直接关：act=%d", act)
	}
}

// 评审发现的两种情况：页面没反应时用户每隔一两秒连点关闭，不能一直重新计时把窗口困住；
// 很久以前一次没回应的请求（页面还没加载完时的关窗）作废，之后正常关窗照常确认，不直接退出停服。
func TestCloseAskRapidAndStale(t *testing.T) {
	app.Base = t.TempDir()
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.MinimizeToTray = false; return nil }); err != nil {
		t.Fatal(err)
	}
	oldRun, oldAsk, oldTray, oldState := runningServers, askCloseUI, trayActive, closeAsk
	defer func() { runningServers, askCloseUI, trayActive, closeAsk = oldRun, oldAsk, oldTray, oldState }()
	asked := 0
	runningServers = func() int { return 1 }
	askCloseUI = func(bool, int) { asked++ }
	trayActive = true

	closeAsk = closeAsker{}
	t0 := time.Now()
	for _, d := range []time.Duration{0, 1500 * time.Millisecond, 2500 * time.Millisecond} { // 页面没回应，连点
		if act := onClose(t0.Add(d), true); act != closeConfirm {
			t.Fatalf("%v：还没等满超时应再请一次：act=%d", d, act)
		}
	}
	if act := onClose(t0.Add(3500*time.Millisecond), true); act != closeDefault {
		t.Fatalf("从第一次请求算起已过 3 秒没回应，应直接关（连点不重新计时）：act=%d", act)
	}

	closeAsk = closeAsker{}
	onClose(t0, true) // 页面还没加载完时的一次关窗，没人回应
	if act := onClose(t0.Add(2*time.Hour), true); act != closeConfirm {
		t.Fatalf("两小时前没回应的请求应作废，这次照常确认：act=%d", act)
	}
	if !closeAsk.askedAt.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("作废后应从这次重新计时：%v", closeAsk.askedAt)
	}
}
