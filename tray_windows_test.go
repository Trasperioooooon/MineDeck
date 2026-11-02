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
