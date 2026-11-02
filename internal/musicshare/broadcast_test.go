package musicshare

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"minedeck/internal/bridge"
	"minedeck/internal/mcsrc"
	"minedeck/internal/music"
)

// 开始 → 连上桥接模组 → 采集的帧调过音量后推过去（静音帧不发）→ 停止：模组收到 END「用户停止」、采集停了。
// 同一时间只有一个广播：再开（同一个实例或别的实例）都是 409。
func TestBroadcastLifecycle(t *testing.T) {
	e := newEnv(t)
	fm := e.ready()
	e.inst.add(InstInfo{Name: "创造服", Dir: filepath.Join(t.TempDir(), "x"), Core: mcsrc.CoreForge, MC: "1.20.1", Status: "running"})

	v, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242, Exe: "cloudmusic.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "cloudmusic.exe（PID 4242）" || v.Mode != "process" || v.Volume != 100 || v.Warning != "" {
		t.Fatalf("开始后的状态：%+v", v)
	}
	c := e.caps.last()
	if c.opts.Mode != music.ModeProcess || c.opts.PID != 4242 {
		t.Fatalf("采集参数：%+v", c.opts)
	}
	waitFor(t, "连上桥接模组", 3*time.Second, func() bool { b := e.status().Broadcast; return b != nil && b.Phase == "live" })
	if hs := fm.Hellos(); len(hs) != 1 || hs[0] != "MineDeck：cloudmusic.exe" {
		t.Errorf("HELLO 里的名字：%v", hs)
	}
	for _, v := range []int16{1000, 0, 0, -2000} { // 两帧静音不发
		c.push(v)
	}
	waitFor(t, "模组收到两帧", 2*time.Second, func() bool { return len(fm.Samples()) == 2 })
	if s := fm.Samples(); s[0][0] != 1000 || s[1][5] != -2000 {
		t.Fatalf("收到的采样：%d %d", s[0][0], s[1][5])
	}
	waitFor(t, "听众数", 2*time.Second, func() bool { b := e.status().Broadcast; return b.Listeners == 2 && b.Silent == 2 })

	// 音量改成 50%：正在广播时立刻生效，也记下来
	vol := 50
	if err := e.s.SetSettings(instName, nil, &vol); err != nil {
		t.Fatal(err)
	}
	c.push(1000)
	c.push(30000)
	waitFor(t, "调过音量的帧", 2*time.Second, func() bool { return len(fm.Samples()) == 4 })
	if s := fm.Samples(); s[2][0] != 500 || s[3][0] != 15000 {
		t.Fatalf("音量 50%%：%d %d", s[2][0], s[3][0])
	}

	for _, name := range []string{instName, "创造服"} {
		if _, err := e.s.Start(StartReq{Inst: name, Mode: "process", PID: 4242}); codeOf(err) != 409 {
			t.Errorf("已经在广播时再开「%s」应为 409：%v", name, err)
		}
	}
	if st, _ := e.s.Status("创造服"); st.Elsewhere != instName || st.Broadcast != nil {
		t.Errorf("别的实例的页面应显示「正在给生存服广播」：%+v", st)
	}

	if err := e.s.Stop("创造服"); codeOf(err) != 400 {
		t.Errorf("没在给它广播：%v", err)
	}
	if err := e.s.Stop(instName); err != nil {
		t.Fatal(err)
	}
	if !c.stopped() || e.s.Current() != "" {
		t.Fatal("停止后采集应已停、没有广播")
	}
	waitFor(t, "模组收到 END", 2*time.Second, func() bool { return slices.Equal(fm.Ends(), []string{"用户停止"}) })
	st := e.status()
	if st.Broadcast != nil || st.Last == nil || st.Last.Reason != "用户停止" || st.Last.Error {
		t.Fatalf("停止后：%+v %+v", st.Broadcast, st.Last)
	}
	if st.Settings.Volume != 50 || st.Settings.Exe != "cloudmusic.exe" || st.Settings.Mode != "process" {
		t.Errorf("设置没记下来：%+v", st.Settings)
	}
	select {
	case a := <-e.auto:
		t.Errorf("用户停止不该当成自动停止：%s", a)
	default:
	}
	// 停了以后可以再开
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	e.s.Stop(instName)
}

// 关服时收尾：服务器进入「停止中」，广播自己停下，给模组发 END 带上原因、停采集，记一条自动停止；
// 停服事件（instance.stop）同样会停。
func TestBroadcastStopsWithServer(t *testing.T) {
	e := newEnv(t)
	fm := e.ready()
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "连上", 3*time.Second, func() bool { b := e.status().Broadcast; return b != nil && b.Phase == "live" })
	c := e.caps.last()
	e.inst.setStatus(instName, "stopping")
	waitFor(t, "广播自己停下", 2*time.Second, func() bool { return e.s.Current() == "" })
	if !c.stopped() || !slices.Equal(fm.Ends(), []string{"服务器正在停止"}) {
		t.Fatalf("收尾：采集停了 %v，END %v", c.stopped(), fm.Ends())
	}
	if a := <-e.auto; a != instName+"：服务器正在停止" {
		t.Errorf("自动停止的记录：%s", a)
	}
	if last := e.status().Last; last == nil || last.Reason != "服务器正在停止" || last.Error {
		t.Errorf("上一次广播：%+v", last)
	}
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); codeOf(err) != 400 || !strings.Contains(err.Error(), "没在运行") {
		t.Errorf("服务器不在运行时不能开始：%v", err)
	}

	// 停服事件
	e.inst.setStatus(instName, "running")
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	e.s.HandleEvent(eventOf("instance.stop", instName))
	if e.s.Current() != "" || !e.caps.last().stopped() {
		t.Fatal("停服事件后应已收尾")
	}
}

// 退出 MineDeck 时收尾：ShutdownAll 给模组发 END「MineDeck 已退出」、停采集，返回时都做完了。
func TestBroadcastShutdownAll(t *testing.T) {
	e := newEnv(t)
	fm := e.ready()
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "连上", 3*time.Second, func() bool { b := e.status().Broadcast; return b != nil && b.Phase == "live" })
	ShutdownAll("MineDeck 已退出")
	if e.s.Current() != "" || !e.caps.last().stopped() {
		t.Fatal("ShutdownAll 返回时应已收尾")
	}
	waitFor(t, "模组收到 END", time.Second, func() bool { return slices.Equal(fm.Ends(), []string{"MineDeck 已退出"}) })
	ShutdownAll("再来一次") // 没有广播时什么都不做
}

// 采集中途结束（音乐软件退出了）、客户端放弃（模组说协议版本不对）：广播自己停下并说明原因。
func TestBroadcastEndsOnCaptureOrBridgeFailure(t *testing.T) {
	e := newEnv(t)
	fm := e.ready()
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	e.caps.last().die(music.ErrProcessExited)
	waitFor(t, "采集结束后收尾", 2*time.Second, func() bool { return e.s.Current() == "" })
	if last := e.status().Last; last.Reason != "音乐软件退出了" || last.Error {
		t.Errorf("音乐软件退出：%+v", last)
	}
	<-e.auto

	fm.Reject.Store(bridge.ErrVersion)
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "客户端放弃后收尾", 3*time.Second, func() bool { return e.s.Current() == "" })
	if last := e.status().Last; !strings.Contains(last.Reason, "连不上桥接模组") || !strings.Contains(last.Reason, "协议版本") || !last.Error {
		t.Errorf("模组拒绝：%+v", last)
	}
	if !e.caps.last().stopped() {
		t.Error("客户端放弃后采集应已停")
	}
}

// 模组那边已经有别的广播连接（同一时间只有一个广播者）：显示「忙」，等一会儿还是忙就放弃并说明。
func TestBroadcastBusyOnModSide(t *testing.T) {
	e := newEnv(t)
	fm := e.ready()
	fm.Reject.Store(bridge.ErrBusy)
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "process", PID: 4242}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "显示忙", 2*time.Second, func() bool { b := e.status().Broadcast; return b != nil && b.Busy })
	waitFor(t, "一直忙就放弃", 4*time.Second, func() bool { return e.s.Current() == "" })
	if last := e.status().Last; !strings.Contains(last.Reason, "已经有别的广播连接") {
		t.Errorf("一直忙：%+v", last)
	}
}

// Win10 只能整机录音：没确认过不能开始（409，说明会把游戏声音、语音一起播出去）；确认一次就记住（每台电脑一次），
// 之后可以开始，状态里一直带着整机录音的提醒；高级选项里选的播放设备传给采集、也记下来。
func TestSystemCaptureNeedsConfirm(t *testing.T) {
	e := newEnv(t)
	e.setCap(win10)
	fm := e.ready()
	sv := e.s.Sources()
	if !sv.Capability.Warn || sv.Capability.Mode != "system" || !strings.Contains(sv.Capability.OS, "Windows 10") || sv.SystemConfirmed {
		t.Fatalf("Win10 的能力：%+v", sv.Capability)
	}
	_, err := e.s.Start(StartReq{Inst: instName, Mode: "system"})
	if codeOf(err) != 409 || !errors.Is(err, ErrNeedConfirm) || !strings.Contains(err.Error(), "游戏声音、语音聊天") {
		t.Fatalf("没确认就开始：%v", err)
	}
	if e.caps.count() != 0 {
		t.Fatal("没确认时不该开始采集")
	}
	if err := e.s.ConfirmSystem(); err != nil {
		t.Fatal(err)
	}
	if !e.newService().Sources().SystemConfirmed {
		t.Fatal("确认应记进设置（重启 MineDeck 后还在）")
	}
	v, err := e.s.Start(StartReq{Inst: instName, Mode: "system", Device: "{dev-2}"})
	if err != nil {
		t.Fatal(err)
	}
	if c := e.caps.last(); c.opts.Mode != music.ModeSystem || c.opts.Device != "{dev-2}" {
		t.Fatalf("整机录音的参数：%+v", c.opts)
	}
	if v.Warning == "" || v.Source != "整机声音：扬声器（假的）" || v.Mode != "system" {
		t.Fatalf("整机录音的状态：%+v", v)
	}
	waitFor(t, "连上", 3*time.Second, func() bool { b := e.status().Broadcast; return b != nil && b.Phase == "live" })
	e.s.Stop(instName)
	if e.s.Sources().Device != "{dev-2}" {
		t.Error("选的播放设备应记下来")
	}
	// 下次不带设备：用记下的那个
	if _, err := e.s.Start(StartReq{Inst: instName, Mode: "system"}); err != nil {
		t.Fatal(err)
	}
	if c := e.caps.last(); c.opts.Device != "{dev-2}" {
		t.Errorf("应沿用上次选的设备：%+v", c.opts)
	}
	e.s.Stop(instName)
	_ = fm
}

// 开始前的检查：不支持的核心、服务器没在运行、没装桥接模组、没有 SVC、没选程序、采集失败都不开始，说清原因；
// 刚装好桥接模组还没重启服务器时也不开始（运行中的服务器里没有它）。
func TestStartChecks(t *testing.T) {
	e := newEnv(t)
	start := func(r StartReq) error { _, err := e.s.Start(r); return err }
	r := StartReq{Inst: instName, Mode: "process", PID: 4242}
	if err := start(StartReq{Inst: "没有的实例", Mode: "process", PID: 1}); codeOf(err) != 404 {
		t.Errorf("没有的实例：%v", err)
	}
	if err := start(r); codeOf(err) != 400 || !strings.Contains(err.Error(), "没在运行") {
		t.Errorf("没在运行：%v", err)
	}
	e.inst.setStatus(instName, "running")
	if err := start(r); !strings.Contains(err.Error(), "还没装桥接模组") {
		t.Errorf("没装桥接模组：%v", err)
	}
	// 运行中先装 SVC 再装桥接模组：要重启才能用
	e.addSVC()
	res, err := e.s.InstallBridge(instName, bridge.VariantMain)
	if err != nil || !res.Restart || !res.Current {
		t.Fatalf("运行中安装：%+v %v", res, err)
	}
	if err := start(r); !strings.Contains(err.Error(), "还没重启服务器") {
		t.Errorf("装好还没重启：%v", err)
	}
	if st := e.status(); !slices.Equal(st.Restart, []string{"桥接模组"}) {
		t.Errorf("要重启的改动：%v", st.Restart)
	}
	e.inst.setStatus(instName, "stopped")
	e.s.BeforeStart(instName) // 重启
	e.inst.setStatus(instName, "running")
	if st := e.status(); len(st.Restart) != 0 {
		t.Errorf("重启后不该再提示：%v", st.Restart)
	}
	if err := start(StartReq{Inst: instName, Mode: "process"}); !strings.Contains(err.Error(), "先在列表里选一个") {
		t.Errorf("没选程序：%v", err)
	}
	if err := start(StartReq{Inst: instName, Mode: "radio"}); codeOf(err) != 400 {
		t.Errorf("未知的采集方式：%v", err)
	}
	e.caps.setFail(errors.New("ActivateAudioInterfaceAsync → 0x80070005 E_ACCESSDENIED"))
	if err := start(r); !strings.Contains(err.Error(), "只录这个程序没成功") || !strings.Contains(err.Error(), "改用整机录音") {
		t.Errorf("采集失败：%v", err)
	}
	e.caps.setFail(nil)
	if e.s.Current() != "" {
		t.Fatal("失败的开始不该留下广播")
	}
	// SVC 被删了
	if err := os.Remove(filepath.Join(e.dir, "mods", "voicechat-neoforge-1.21.1-2.6.27.jar")); err != nil {
		t.Fatal(err)
	}
	if err := start(r); !strings.Contains(err.Error(), "没有 Simple Voice Chat") {
		t.Errorf("没有 SVC：%v", err)
	}
	// Paper 不支持
	e.inst.add(InstInfo{Name: "插件服", Dir: t.TempDir(), Core: mcsrc.CorePaper, MC: "1.21.4", Status: "running"})
	if err := start(StartReq{Inst: "插件服", Mode: "process", PID: 1}); codeOf(err) != 400 || !strings.Contains(err.Error(), "只支持 NeoForge、Forge") {
		t.Errorf("Paper：%v", err)
	}
}

func TestApplyGainAndSilence(t *testing.T) {
	in := []int16{100, -100, 32767, -32768}
	if got := applyGain(in, 200); !slices.Equal(got, []int16{200, -200, 32767, -32768}) {
		t.Errorf("200%%：%v", got)
	}
	if got := applyGain(in, 0); !slices.Equal(got, []int16{0, 0, 0, 0}) {
		t.Errorf("0%%：%v", got)
	}
	if !silent(make([]int16, 960)) || silent([]int16{0, 1}) {
		t.Error("静音判断")
	}
}
