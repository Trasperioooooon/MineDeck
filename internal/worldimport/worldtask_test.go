package worldimport

import (
	"path/filepath"
	"strings"
	"testing"

	wt "minedeck/internal/worldimport/worldtest"
)

// 世界状态里正在跑的换地图任务（A53）：别的设备接得上进度；能不能取消、不能的话为什么，按问的人说。

const lanOwner = "lan|192.168.1.5"

// 导入：复制阶段发起的人能取消，别人不能；开始换以后谁都不能，说清在做什么；任务一显示结束，状态里就没有它了。
// 结果里带着服务器名。
func TestImportTaskInWorldStatus(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	idCh := make(chan string, 1)
	var copying, copyingLAN, replacing *WorldTask
	var lanRefusal, lateRefusal string
	var lateCancel bool
	stagedHook = func() {
		id := <-idCh
		idCh <- id
		copying, copyingLAN = e.s.TaskOf(me, "生存服"), e.s.TaskOf(lanOwner, "生存服")
		if e.s.Cancel(lanOwner, id) {
			t.Error("别的设备取消了别人发起的导入")
		}
		lanRefusal = e.s.CancelRefusal(lanOwner, id)
	}
	faultHook = func(point string) error {
		if point == "old-out-0" {
			id := <-idCh
			replacing = e.s.TaskOf(me, "生存服")
			lateCancel = e.s.Cancel(me, id)
			lateRefusal = e.s.CancelRefusal(me, id)
		}
		return nil
	}
	t.Cleanup(func() { stagedHook, faultHook = nil, nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	idCh <- id
	res, msg := e.wait(id)
	if msg != "" {
		t.Fatal(msg)
	}
	if copying == nil || copying.ID != id || copying.Kind != jobImport || !copying.CanCancel || copying.CancelWhy != "" {
		t.Errorf("复制阶段，发起的人看到的：%+v", copying)
	}
	if copyingLAN == nil || copyingLAN.ID != id || copyingLAN.CanCancel || copyingLAN.CancelWhy != whyNotOwner || lanRefusal != whyNotOwner {
		t.Errorf("复制阶段，别的设备看到的：%+v，取消被拒的原因 %q", copyingLAN, lanRefusal)
	}
	if replacing == nil || replacing.ID != id || replacing.CanCancel || replacing.CancelWhy != whyReplacing || lateCancel || lateRefusal != whyReplacing {
		t.Errorf("换地图那一步：%+v，取消 %v，原因 %q", replacing, lateCancel, lateRefusal)
	}
	if got := e.s.TaskOf(me, "生存服"); got != nil {
		t.Errorf("任务显示结束时，世界状态里不该还带着它：%+v", got)
	}
	if !strings.Contains(res, "「生存服」") {
		t.Errorf("结果里应有服务器名：%q", res)
	}
}

// 复制完、进了不能取消的阶段（closeCancel 之后、换地图之前）：不能取消，原因是已经开始停服。
// 拿掉取消的登记和记下原因在同一把锁里，中间问不出「这个任务已经不能取消了」这种没头没脑的话。
func TestImportTaskAfterCopyCannotCancel(t *testing.T) {
	e := newEnv(t, "")
	drop := e.s.addJob("生存服", "t-fake", jobImport, "")
	defer drop()
	e.s.mu.Lock()
	e.s.cancels["t-fake"] = cancelEntry{owner: me, fn: func() {}}
	e.s.mu.Unlock()
	if got := e.s.TaskOf(me, "生存服"); got == nil || !got.CanCancel {
		t.Fatalf("还在复制时：%+v", got)
	}
	if !e.s.closeCancel(t.Context(), "t-fake") {
		t.Fatal("没取消过的应能进不能取消的阶段")
	}
	if got := e.s.TaskOf(me, "生存服"); got == nil || got.CanCancel || got.CancelWhy != whyStopping {
		t.Fatalf("开始停服以后：%+v", got)
	}
	if why := e.s.CancelRefusal(me, "t-fake"); why != whyStopping {
		t.Fatalf("取消被拒的原因：%q", why)
	}
}

// 换回旧地图：状态里带着任务（kind restore），一直不能取消；结果里带着服务器名。
func TestRestoreTaskInWorldStatus(t *testing.T) {
	e := newEnv(t, "")
	if _, msg := e.wait(mustExecute(t, e, e.plan(e.bukkitZip()))); msg != "" {
		t.Fatal(msg)
	}
	st, err := e.s.Status("生存服")
	if err != nil || len(st.Points) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	var during *WorldTask
	faultHook = func(point string) error {
		if point == "cur-out-0" {
			during = e.s.TaskOf(me, "生存服")
		}
		return nil
	}
	t.Cleanup(func() { faultHook = nil })
	id, err := e.s.Restore("生存服", st.Points[0].ID, "restore")
	if err != nil {
		t.Fatal(err)
	}
	res, msg := e.wait(id)
	if msg != "" {
		t.Fatal(msg)
	}
	if during == nil || during.ID != id || during.Kind != jobRestore || during.CanCancel || during.CancelWhy != whyRestoring {
		t.Errorf("换回的时候：%+v", during)
	}
	if got := e.s.TaskOf(me, "生存服"); got != nil {
		t.Errorf("换回结束以后不该还带着任务：%+v", got)
	}
	if !strings.Contains(res, "「生存服」已经换回导入前的地图") {
		t.Errorf("结果里应有服务器名：%q", res)
	}
}

func mustExecute(t *testing.T, e *env, p *Plan) string {
	t.Helper()
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
