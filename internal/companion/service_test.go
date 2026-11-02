package companion_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"minedeck/internal/companion"
	"minedeck/internal/companion/companiontest"
	companionjar "minedeck/internal/companion/jar"
	"minedeck/internal/events"
)

// svcEnv 一个装好了伙伴模组的 NeoForge 1.21.1 实例「模组服」、一个假的伙伴模组、一个 Service（事件由测试直接交给它）。
type svcEnv struct {
	s        *companion.Service
	in       *fakeInsts
	mod      *companiontest.FakeMod
	dir      string
	handle   func(events.Event)
	mu       sync.Mutex
	finished []companion.Finished
}

func newSvcEnv(t *testing.T) *svcEnv {
	t.Helper()
	e := &svcEnv{in: newInsts(), mod: startMod(t, companiontest.Options{StatusEvery: 50 * time.Millisecond})}
	e.dir = e.in.add(t, "模组服", "neoforge", "1.21.1", "21.1.265")
	man, err := companionjar.Load()
	if err != nil {
		t.Fatal(err)
	}
	j, _ := man.Find("neoforge", "1.21.1")
	b, _ := j.Bytes()
	if err := os.WriteFile(filepath.Join(e.dir, "mods", j.InstallName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	d := companion.Deps{Instances: e.in, ClientName: "MineDeck 测试", Subscribe: func(f func(events.Event)) { e.handle = f },
		OnFinish: func(f companion.Finished) {
			e.mu.Lock()
			e.finished = append(e.finished, f)
			e.mu.Unlock()
		}}
	companion.SetClientOptions(&d, companion.FastRetry())
	companion.SetTick(&d, 10*time.Millisecond)
	e.s = companion.New(d)
	t.Cleanup(func() { e.s.Shutdown("测完了") })
	return e
}

// enable 在界面上打开伙伴：MineDeck 写好配置（端口、令牌）；测试再把端口和令牌改成假模组的。
func (e *svcEnv) enable(t *testing.T) {
	t.Helper()
	v, err := e.s.SetSettings("模组服", companion.SettingsPatch{Enabled: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Settings.Enabled || v.Settings.Port < 1024 {
		t.Fatalf("打开以后：%+v", v.Settings)
	}
	b, _ := os.ReadFile(companion.ConfigPath(e.dir))
	s := strings.ReplaceAll(string(b), "port="+itoa(v.Settings.Port), "port="+itoa(e.mod.Port()))
	cfg, _ := companion.ReadConfig(e.dir)
	s = strings.ReplaceAll(s, "token="+cfg.Token, "token="+token)
	if err := os.WriteFile(companion.ConfigPath(e.dir), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (e *svcEnv) start() {
	e.in.setStatus("模组服", "running")
	e.handle(events.Event{Type: "instance.start", Data: map[string]any{"instance": "模组服"}})
}

func (e *svcEnv) live(t *testing.T) *companion.View {
	t.Helper()
	var v *companion.View
	waitFor(t, "连上伙伴模组", func() bool {
		v, _ = e.s.Status("模组服")
		return v.Link.State == "live"
	})
	return v
}

func (e *svcEnv) finishedList() []companion.Finished {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]companion.Finished(nil), e.finished...)
}

// 服务器开着、启用了伙伴才去连；没启用时开服也不连。连上后从 WELCOME、事件和 STATUS 跟踪伙伴；关服时断开（发 END）、
// 清掉这次开服的状态；退出 MineDeck 时同样发 END。
func TestServiceAutoConnect(t *testing.T) {
	e := newSvcEnv(t)
	// 没启用：开服也不连
	e.start()
	time.Sleep(100 * time.Millisecond)
	if v, _ := e.s.Status("模组服"); v.Link.State != "off" || e.mod.Accepts() != 0 {
		t.Fatalf("没启用时不该连：%+v accepts=%d", v.Link, e.mod.Accepts())
	}
	e.in.setStatus("模组服", "stopped")
	e.enable(t)
	if v, _ := e.s.Status("模组服"); v.Link.State != "waiting" || e.mod.Accepts() != 0 {
		t.Fatalf("服务器没开时等着：%+v", v.Link)
	}
	e.in.setStatus("模组服", "starting")
	e.handle(events.Event{Type: "instance.start", Data: map[string]any{"instance": "模组服"}})
	time.Sleep(80 * time.Millisecond)
	if e.mod.Accepts() != 0 {
		t.Fatal("服务器还没开好（模组还没打开端口），不该去连")
	}
	e.in.setStatus("模组服", "running")
	v := e.live(t)
	if v.Link.Mod != "0.2.0" || v.Link.PlayerEngine != "1.21.1-1.4.0" || v.Link.Session != "s1" || len(v.Link.Ops) != 10 || len(v.Companions) != 0 {
		t.Errorf("连上以后：%+v", v.Link)
	}
	if h := e.mod.Hellos(); h[len(h)-1].Client != "MineDeck 测试" {
		t.Errorf("HELLO 的名字：%+v", h)
	}
	if v.Events[0].Event != "connected" {
		t.Errorf("最近的事件：%+v", v.Events)
	}

	c, err := e.s.Spawn(context.Background(), "模组服", companion.SpawnReq{Name: "小方", Owner: "Steve"})
	if err != nil || c.ID != 1 || c.Name != "小方" {
		t.Fatalf("刷出：%+v %v", c, err)
	}
	if _, err := e.s.Spawn(context.Background(), "模组服", companion.SpawnReq{Name: "小方"}); err == nil || !strings.Contains(err.Error(), "已经有一个叫") {
		t.Errorf("重名：%v", err)
	}
	if _, err := e.s.Spawn(context.Background(), "模组服", companion.SpawnReq{Name: "坏 名字"}); err == nil {
		t.Error("名字不合规应报错")
	}
	v, _ = e.s.Status("模组服")
	if len(v.Companions) != 1 || v.Companions[0].Owner != "Steve" || !strings.Contains(v.Events[0].Text, "刷出了伙伴「小方」（主人 Steve）") {
		t.Errorf("刷出以后：%+v %+v", v.Companions, v.Events)
	}
	if names := e.s.Names("模组服"); len(names) != 1 || names[0] != "小方" {
		t.Errorf("名字：%v", names)
	}
	// STATUS 每 50 毫秒刷新一次伙伴（模组那边伙伴走动、掉血）
	waitFor(t, "STATUS 更新", func() bool {
		v, _ := e.s.Status("模组服")
		return len(v.Companions) == 1 && v.Companions[0].Health == 20
	})

	// 关服：断开，发 END，清掉伙伴
	e.in.setStatus("模组服", "stopped")
	e.handle(events.Event{Type: "instance.stop", Data: map[string]any{"instance": "模组服"}})
	waitFor(t, "模组收到 END", func() bool { return len(e.mod.Ends()) == 1 })
	if e.mod.Ends()[0] != "服务器已停止" {
		t.Errorf("END：%v", e.mod.Ends())
	}
	v, _ = e.s.Status("模组服")
	if v.Link.State != "waiting" || len(v.Companions) != 0 || len(e.s.Names("模组服")) != 0 {
		t.Errorf("关服以后：%+v %+v", v.Link, v.Companions)
	}
	if _, err := e.s.Spawn(context.Background(), "模组服", companion.SpawnReq{Name: "小木"}); err == nil || !strings.Contains(err.Error(), "没连着") {
		t.Errorf("没连着时刷出：%v", err)
	}

	// 再开服；退出 MineDeck 时发 END
	e.start()
	e.live(t)
	companion.ShutdownAll("MineDeck 已退出")
	waitFor(t, "模组收到 END", func() bool { return len(e.mod.Ends()) == 2 })
	if e.mod.Ends()[1] != "MineDeck 已退出" {
		t.Errorf("END：%v", e.mod.Ends())
	}
}

// 游戏内 AI 让伙伴干活：记下是谁发起的；任务做完、没做够、失败、超时时按模板交回一句话（不调用 AI），
// 被停下、被新任务顶掉的不通知，不是经 Act 发起的任务也不通知。服务器重启（session 变了）后旧的任务编号作废。
func TestServiceActAndFinish(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	e.start()
	e.live(t)
	ctx := context.Background()
	if _, err := e.s.Act(ctx, "模组服", "collect", map[string]any{"item": "oak_log"}, companion.Origin{Player: "Steve"}); err == nil ||
		!strings.Contains(err.Error(), "一个伙伴都没有") {
		t.Errorf("没有伙伴时：%v", err)
	}
	for _, n := range []string{"小方", "小木"} {
		if _, err := e.s.Spawn(ctx, "模组服", companion.SpawnReq{Name: n, Owner: "Steve"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.s.Act(ctx, "模组服", "collect", map[string]any{"item": "oak_log"}, companion.Origin{Player: "Steve"}); err == nil || !strings.Contains(err.Error(), "要说清楚") {
		t.Errorf("两个伙伴、没说是哪个：%v", err)
	}
	// 叫着名字说的：默认就是它；参数表以外的键去掉
	r, err := e.s.Act(ctx, "模组服", "collect", map[string]any{"item": "oak_log", "count": 3, "op": "attack", "command": "kill @a"},
		companion.Origin{Player: "Steve", Companion: "小木"})
	if err != nil || !r.OK || r.Task == nil || r.Companion == nil || r.Companion.Name != "小木" {
		t.Fatalf("collect：%+v %v", r, err)
	}
	last := e.mod.Requests()[len(e.mod.Requests())-1]
	if last["op"] != "collect" || last["command"] != nil || last["companion"] != float64(2) || last["count"] != float64(3) {
		t.Errorf("发给模组的：%v", last)
	}
	// 按名字（不分大小写）、按编号指定
	if r, err := e.s.Act(ctx, "模组服", "goto", map[string]any{"companion": "小方", "x": 100, "z": -20}, companion.Origin{Player: "Alex"}); err != nil || !r.OK || r.Companion.ID != 1 {
		t.Errorf("按名字：%+v %v", r, err)
	}
	if r, err := e.s.Act(ctx, "模组服", "say", map[string]any{"companion": 1, "text": "你好"}, companion.Origin{Player: "Alex"}); err != nil || !r.OK {
		t.Errorf("按编号：%+v %v", r, err)
	}
	if _, err := e.s.Act(ctx, "模组服", "despawn", map[string]any{"all": true}, companion.Origin{Player: "Alex"}); err == nil {
		t.Error("游戏内 AI 不能移除伙伴")
	}
	if r, err := e.s.Act(ctx, "模组服", "attack", map[string]any{"companion": "小方", "mob": "villager"}, companion.Origin{Player: "Alex"}); err != nil || r.OK || r.Code != companion.CodeForbidden {
		t.Errorf("forbidden：%+v %v", r, err)
	}

	// 小木的采集做完了：通知 Steve
	if err := e.mod.Finish(r0(t, e, 2), companion.ResultDone, map[string]any{"have": 5, "gained": 3, "want": 3}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "通知", func() bool { return len(e.finishedList()) == 1 })
	f := e.finishedList()[0]
	if f.Inst != "模组服" || f.Origin.Player != "Steve" || f.Companion != "小木" || f.Op != "collect" || f.Result != "done" ||
		f.Text != "采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 5 个，用时 10 秒。" {
		t.Errorf("通知：%+v", f)
	}
	// 小方的 goto 被新任务顶掉（superseded）、新任务被停下（cancelled）：都不通知
	if _, err := e.s.Act(ctx, "模组服", "follow", map[string]any{"companion": "小方", "player": "Alex"}, companion.Origin{Player: "Alex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.StopTasks(ctx, "模组服", companion.StopReq{All: true}); err != nil {
		t.Fatal(err)
	}
	// 不是经 Act 发起的任务（比如 testctl 发的）结束了：不通知
	_ = e.mod.SendEvent(map[string]any{"event": "task_finished", "companion": 1, "result": "failed", "task": map[string]any{"id": 99, "op": "goto", "companion": 1}})
	// 失败的：通知；不认识的 result 当成 failed
	r2, _ := e.s.Act(ctx, "模组服", "give", map[string]any{"companion": "小方", "item": "minecraft:bread", "count": 2}, companion.Origin{Player: "Alex"})
	_ = e.mod.Finish(r2.Task.ID, "exploded", map[string]any{"detail": "找不到路", "given": 0, "have": 0})
	waitFor(t, "第二条通知", func() bool { return len(e.finishedList()) == 2 })
	if f := e.finishedList()[1]; f.Origin.Player != "Alex" || f.Result != "failed" || f.Text != "交出 bread ×2 失败了：交出了 0 个，背包里还有 0 个，找不到路，用时 10 秒。" {
		t.Errorf("失败的通知：%+v", f)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(e.finishedList()); n != 2 {
		t.Errorf("被停下、被顶掉、不是谁发起的都不通知：%d 条 %+v", n, e.finishedList())
	}
	v, _ := e.s.Status("模组服")
	var texts []string
	for _, ev := range v.Events {
		texts = append(texts, ev.Text)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"「小木」开始采集 oak_log ×3", "「小方」去 (100, -20) 被新任务顶掉了", "「小方」跟着 Alex 停下了", "「小方」交出 bread ×2 失败了"} {
		if !strings.Contains(all, want) {
			t.Errorf("最近的事件里应有「%s」：\n%s", want, all)
		}
	}
	if c := v.Companions; len(c) != 2 || c[0].Task != nil || c[0].LastResult == nil || c[0].LastResult.Result != "failed" || c[0].State != "idle" {
		t.Errorf("伙伴的任务和上次结果（不认识的 result 当成 failed）：%+v", c)
	}

	// 断开期间结束的任务：事件丢了不补，重连后从 WELCOME 里伙伴的 lastResult 认出来，照样通知
	r4, _ := e.s.Act(ctx, "模组服", "attack", map[string]any{"companion": "小方", "mob": "zombie", "count": 2}, companion.Origin{Player: "Steve"})
	e.mod.SetReject(companion.ErrBusy)
	e.mod.Kick()
	waitFor(t, "断开", func() bool { v, _ := e.s.Status("模组服"); return v.Link.State == "retrying" })
	if err := e.mod.Finish(r4.Task.ID, "done", map[string]any{"killed": 2, "want": 2}); err == nil {
		t.Fatal("断开期间事件应发不出去")
	}
	e.mod.SetReject(0)
	waitFor(t, "第三条通知", func() bool { return len(e.finishedList()) == 3 })
	if f := e.finishedList()[2]; f.Origin.Player != "Steve" || f.Text != "打 zombie ×2 做完了：打到 2 只（要 2 只），用时 10 秒。" {
		t.Errorf("断开期间结束的任务：%+v", f)
	}

	// 服务器重启：session 变了，旧的任务编号作废；新开服的 1 号任务不是 Steve 发起的
	r3, _ := e.s.Act(ctx, "模组服", "collect", map[string]any{"companion": "小方", "item": "stone"}, companion.Origin{Player: "Steve"})
	e.mod.Restart("s2")
	waitFor(t, "重连", func() bool { v, _ := e.s.Status("模组服"); return v.Link.State == "live" && v.Link.Session == "s2" })
	_ = e.mod.SendEvent(map[string]any{"event": "task_finished", "companion": 1, "result": "done", "task": map[string]any{"id": r3.Task.ID, "op": "collect", "companion": 1}})
	time.Sleep(80 * time.Millisecond)
	if n := len(e.finishedList()); n != 3 {
		t.Errorf("重启以后旧编号的任务不该通知：%+v", e.finishedList())
	}
}

// 关服时是模组先发 END「服务器正在关闭」再关端口（两半联调时实测）：服务器正在关的时候这是正常的，不报「断开了、会自动重连」，
// 也不再重连（端口已经关了），等下次开服再连。服务器开着时模组发 END（比如改了端口）照旧报出来、自动重连。
func TestServiceServerClosing(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	e.start()
	e.live(t)
	accepts := e.mod.Accepts()
	e.in.setStatus("模组服", "stopping") // inst.Manager.Stop：先改状态，再往控制台写 stop
	e.mod.SendEnd("服务器正在关闭")
	waitFor(t, "断开", func() bool { v, _ := e.s.Status("模组服"); return v.Link.State == "waiting" })
	time.Sleep(150 * time.Millisecond) // FastRetry 下 20 毫秒就会重连：这么久都没连说明真的不连了
	v, _ := e.s.Status("模组服")
	if n := e.mod.Accepts(); n != accepts || len(e.mod.Hellos()) != accepts {
		t.Errorf("服务器正在关时不该再重连：握手 %d → %d，HELLO %d", accepts, n, len(e.mod.Hellos()))
	}
	if ev := v.Events[0]; ev.Event != "disconnected" || ev.Level != "info" || !strings.Contains(ev.Text, "服务器正在关闭") || strings.Contains(ev.Text, "会自动重连") {
		t.Errorf("事件：%+v", ev)
	}
	// 进程退出：连接早就没了，不用再发 END
	e.in.setStatus("模组服", "stopped")
	e.handle(events.Event{Type: "instance.stop", Data: map[string]any{"instance": "模组服"}})
	time.Sleep(50 * time.Millisecond)
	if ends := e.mod.Ends(); len(ends) != 0 {
		t.Errorf("模组已经断开了，不该再收到 END：%v", ends)
	}
	// 下次开服照常连上
	e.start()
	e.live(t)
	// 服务器开着时模组发了 END：照旧报「会自动重连」，重连
	accepts = e.mod.Accepts()
	e.mod.SendEnd("端口改了，请重新连接")
	waitFor(t, "重连", func() bool { return e.mod.Accepts() > accepts })
	e.live(t)
	v, _ = e.s.Status("模组服")
	var warned bool
	for _, ev := range v.Events {
		warned = warned || (ev.Event == "disconnected" && ev.Level == "warn" && strings.Contains(ev.Text, "端口改了") && strings.Contains(ev.Text, "会自动重连"))
	}
	if !warned {
		t.Errorf("开着时断开应报出来：%+v", v.Events)
	}
}

// 游戏里的管理员 /stop、RCON 发的 stop：模组说「服务器正在关闭」时 MineDeck 这边的实例状态可能还是 running
// （停服那一行还在控制台的管道里没读到）。按模组的原话算正常关服：普通提示、不再重连（测试报告 H-02 发现 8）。
func TestServiceServerClosingBeforeStatus(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	e.start()
	e.live(t)
	accepts := e.mod.Accepts()
	e.mod.SendEnd("服务器正在关闭") // 状态没改，还是 running
	waitFor(t, "断开", func() bool { v, _ := e.s.Status("模组服"); return v.Link.State == "waiting" })
	time.Sleep(150 * time.Millisecond)
	if n := e.mod.Accepts(); n != accepts {
		t.Errorf("模组说服务器正在关闭，不该再重连：握手 %d → %d", accepts, n)
	}
	v, _ := e.s.Status("模组服")
	if ev := v.Events[0]; ev.Event != "disconnected" || ev.Level != "info" || strings.Contains(ev.Text, "会自动重连") {
		t.Errorf("事件：%+v", ev)
	}
}

// task_finished 丢了（模组的 WELCOME 读的是每 tick 末尾才刷新的快照：握手正好落在任务结束的那个 tick 里时，事件发在连接就绪之前，
// WELCOME 里也还没有这次的 lastResult）：之后的 STATUS 里伙伴的 lastResult 是有人发起、还没通知过的任务，照样通知，只通知一次。
func TestServiceStatusCatchesLostFinish(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	e.start()
	e.live(t)
	ctx := context.Background()
	if _, err := e.s.Spawn(ctx, "模组服", companion.SpawnReq{Name: "小木", Owner: "Steve"}); err != nil {
		t.Fatal(err)
	}
	r, err := e.s.Act(ctx, "模组服", "collect", map[string]any{"item": "minecraft:oak_log", "count": 3}, companion.Origin{Player: "Steve", Companion: "小木"})
	if err != nil || !r.OK || r.Task == nil {
		t.Fatalf("collect：%+v %v", r, err)
	}
	if err := e.mod.FinishLost(r.Task.ID, companion.ResultDone, map[string]any{"have": 3, "gained": 3, "want": 3}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "从 STATUS 认出来的通知", func() bool { return len(e.finishedList()) == 1 })
	if f := e.finishedList()[0]; f.Origin.Player != "Steve" || f.Companion != "小木" || f.TaskID != r.Task.ID || f.Text != "采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 3 个，用时 10 秒。" {
		t.Errorf("通知：%+v", f)
	}
	time.Sleep(200 * time.Millisecond) // 之后的 STATUS 里还是这个 lastResult：不再通知
	if n := len(e.finishedList()); n != 1 {
		t.Errorf("只通知一次：%d 条", n)
	}
	v, _ := e.s.Status("模组服")
	var seen bool
	for _, ev := range v.Events {
		seen = seen || (ev.Event == companion.EvTaskFinished && strings.Contains(ev.Text, "「小木」采集 oak_log ×3 做完了"))
	}
	if !seen {
		t.Errorf("最近的事件里应有这个任务的结果：%+v", v.Events)
	}
	// 没人发起的任务（不是经 Act 开始的）在 STATUS 里有 lastResult：不通知
	if _, err := e.s.StopTasks(ctx, "模组服", companion.StopReq{All: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(e.finishedList()); n != 1 {
		t.Errorf("没人发起的不通知：%d 条", n)
	}
}

// give 的物品名：原版物品去掉 minecraft:（PlayerEngine 1.4.0 的 give 只认 oak_log 这种写法，写成 minecraft:oak_log 时什么都不给就结束，
// 模组还报 done，两半联调时实测）；模组物品照写命名空间；collect 两种写法 PlayerEngine 都认，原样发。
func TestActGiveItemName(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	e.start()
	e.live(t)
	ctx := context.Background()
	if _, err := e.s.Spawn(ctx, "模组服", companion.SpawnReq{Name: "小木", Owner: "Steve"}); err != nil {
		t.Fatal(err)
	}
	last := func() map[string]any { r := e.mod.Requests(); return r[len(r)-1] }
	for _, c := range []struct{ op, item, want string }{
		{"give", "minecraft:oak_log", "oak_log"},
		{"give", " minecraft:bread", "bread"},
		{"give", "oak_log", "oak_log"},
		{"give", "create:brass_ingot", "create:brass_ingot"},
		{"collect", "minecraft:oak_log", "minecraft:oak_log"},
	} {
		r, err := e.s.Act(ctx, "模组服", c.op, map[string]any{"item": c.item, "player": "Steve"}, companion.Origin{Player: "Steve"})
		if err != nil || !r.OK {
			t.Fatalf("%s %s：%+v %v", c.op, c.item, r, err)
		}
		if got := last(); got["op"] != c.op || got["item"] != c.want || r.Sent["item"] != c.want {
			t.Errorf("%s %q：发给模组的 item 是 %v（记下的 %v），应为 %q", c.op, c.item, got["item"], r.Sent["item"], c.want)
		}
	}
}

// r0 某个伙伴现在的任务编号（从模组那边看）。
func r0(t *testing.T, e *svcEnv, id int) int64 {
	t.Helper()
	for _, c := range e.mod.Companions() {
		if int(c["id"].(float64)) == id {
			if task, ok := c["task"].(map[string]any); ok {
				return int64(task["id"].(float64))
			}
		}
	}
	t.Fatalf("%d 号伙伴没有任务", id)
	return 0
}

// 改设置：伙伴上限、attack_deny、禁挖区域写进配置文件；模组读到之前标明「还没生效」，收到 config_applied 后对上。
// 关掉伙伴：先给模组发 END，再改 enabled=false。
func TestServiceSettings(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	e.start()
	e.live(t)
	zones := []companion.Zone{{Name: "base", Dim: "minecraft:overworld", Min: [3]int{-120, -64, -80}, Max: [3]int{-40, 320, 10}}}
	v, err := e.s.SetSettings("模组服", companion.SettingsPatch{Patch: companion.Patch{MaxCompanions: ptr(2), Zones: &zones}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Settings.MaxCompanions != 2 || len(v.Settings.Zones) != 1 || !v.Settings.Pending || v.Settings.Applied == nil || v.Settings.Applied.MaxCompanions != 4 {
		t.Errorf("改了还没生效：%+v", v.Settings)
	}
	e.mod.SetConfig(map[string]any{"maxCompanions": 2, "attackDeny": companion.DefaultAttackDeny, "problems": []any{},
		"zones": []any{map[string]any{"name": "base", "dim": "minecraft:overworld", "min": []int{-120, -64, -80}, "max": []int{-40, 320, 10}}}})
	waitFor(t, "生效", func() bool { v, _ := e.s.Status("模组服"); return !v.Settings.Pending })
	if v, _ := e.s.Status("模组服"); v.Events[0].Event != "config_applied" {
		t.Errorf("事件：%+v", v.Events[0])
	}
	if _, err := e.s.SetSettings("模组服", companion.SettingsPatch{Patch: companion.Patch{MaxCompanions: ptr(40)}}); err == nil {
		t.Error("上限超范围应报错")
	}
	// 关掉
	v, err = e.s.SetSettings("模组服", companion.SettingsPatch{Enabled: ptr(false)})
	if err != nil || v.Settings.Enabled || v.Link.State != "off" {
		t.Fatalf("关掉：%+v %v", v, err)
	}
	if ends := e.mod.Ends(); len(ends) != 1 || !strings.Contains(ends[0], "关掉了伙伴") {
		t.Errorf("关掉时应先发 END：%v", ends)
	}
	cfg, _ := companion.ReadConfig(e.dir)
	if cfg.Enabled || cfg.MaxCompanions != 2 || len(cfg.Zones) != 1 {
		t.Errorf("配置文件：%+v", cfg)
	}
	// 开服前（inst 的回调）：没启用的不碰配置
	e.s.BeforeStart("模组服")
	if c2, _ := companion.ReadConfig(e.dir); c2.Enabled {
		t.Error("没启用时开服前不该打开")
	}
	// 伙伴模组还没装时不能打开
	e2 := newInstallEnv(t)
	if _, err := e2.s.SetSettings("模组服", companion.SettingsPatch{Enabled: ptr(true)}); err == nil || !strings.Contains(err.Error(), "先装伙伴模组") {
		t.Errorf("没装时打开：%v", err)
	}
	var ce *companion.Error
	if _, err := e.s.SetSettings("没有这个", companion.SettingsPatch{}); !errors.As(err, &ce) || ce.Code != 404 {
		t.Errorf("实例不存在：%v", err)
	}
}

// 开服前（inst 的回调）：启用了的，端口和别的实例撞了就换一个，令牌坏了重新生成。
func TestBeforeStartFixesConfig(t *testing.T) {
	e := newSvcEnv(t)
	e.enable(t)
	other := e.in.add(t, "另一台", "neoforge", "1.21.1", "")
	writeCfg(t, other, e.mod.Port(), token) // 另一台已经用了这个端口
	e.s.BeforeStart("模组服")
	c, _ := companion.ReadConfig(e.dir)
	if !c.Enabled || c.Port == e.mod.Port() || c.Token != token {
		t.Errorf("端口撞了应换一个、令牌留着：%+v", c)
	}
	// 和自己的音乐桥接撞了也换
	writeFile(t, filepath.Join(e.dir, "config", "minedeck", "voicebridge.properties"), "enabled=true\nport="+itoa(c.Port)+"\ntoken="+token+"\n")
	e.s.BeforeStart("模组服")
	if c2, _ := companion.ReadConfig(e.dir); c2.Port == c.Port || c2.Port == e.mod.Port() {
		t.Errorf("和音乐桥接的端口撞了应换一个：%d", c2.Port)
	}
}
