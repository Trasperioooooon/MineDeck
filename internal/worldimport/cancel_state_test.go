package worldimport

// beta.3：换地图的取消接上任务的「已取消」状态（同新建、导入整合包的取消，tasks.EndCancelled，2026-11-14 复审发现 9）。
// 以前取消的导入按失败结束：任务页画红框「没有完成：已取消，没有替换任何文件」。

import (
	"path/filepath"
	"testing"

	wt "minedeck/internal/worldimport/worldtest"
)

// 复制完、还没开始换的时候点取消：任务按「已取消」结束（cancelled 为真，正在做的那一步是 cancelled，不是 error），
// error 里仍是那句中文；列表摘要也带 cancelled。原来的世界没动。
func TestCancelledImportEndsAsCancelled(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	idCh := make(chan string, 1)
	var accepted, cancelling bool
	stagedHook = func() {
		id := <-idCh
		accepted = e.s.Cancel(me, id)
		// 取消收下了、任务还没结束：快照里是「正在取消」，不是已经结束
		s := e.m.Tasks.Get(id).Snapshot()
		cancelling = s.Cancelling && !s.Ended && !s.Cancelled
	}
	t.Cleanup(func() { stagedHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	idCh <- id
	_, msg := e.wait(id)
	if !accepted {
		t.Fatal("复制刚完成时取消应当返回成功")
	}
	if !cancelling {
		t.Error("取消收下以后、清理完之前，快照应是「正在取消」")
	}
	s := e.m.Tasks.Get(id).Snapshot()
	if !s.Cancelled || s.Cancelling || msg != "已取消，没有替换任何文件" || s.CanCancel {
		t.Errorf("应按「已取消」结束：cancelled=%v cancelling=%v err=%q canCancel=%v", s.Cancelled, s.Cancelling, msg, s.CanCancel)
	}
	var cancelledSteps int
	for _, st := range s.Steps {
		if st.Status == "error" {
			t.Errorf("取消的任务不该有画红叉的步骤：%+v", s.Steps)
		}
		if st.Status == "cancelled" {
			cancelledSteps++
		}
	}
	if cancelledSteps != 1 {
		t.Errorf("正在做的那一步应标成 cancelled：%+v", s.Steps)
	}
	found := false
	for _, sum := range e.m.Tasks.List() {
		if sum.ID == id {
			found = true
			if !sum.Cancelled || sum.Err != msg {
				t.Errorf("列表摘要也要带 cancelled：%+v", sum)
			}
		}
	}
	if !found {
		t.Error("任务列表里没有这个任务")
	}
	if got := e.marker("world/region/r.0.0.mca"); got != "MARK:minecraft:overworld:OLD" {
		t.Errorf("取消以后原来的世界不该动：%s", got)
	}
}

// 没人取消、导入照常做完的任务不带 cancelled；过了不能取消的那一步再点取消，取消不收，任务照常完成，也不带 cancelled。
func TestRefusedCancelIsNotCancelled(t *testing.T) {
	e := newEnv(t, "")
	src := filepath.Join(e.tmp, "B")
	if err := wt.Legacy(src, wt.WorldOpts{Level: lv("1.20.4", 3700), Tag: "B", Overworld: true}); err != nil {
		t.Fatal(err)
	}
	p := e.plan(src)
	idCh := make(chan string, 1)
	asked, accepted := false, false
	faultHook = func(point string) error {
		if point == "old-out-0" && !asked {
			asked = true
			accepted = e.s.Cancel(me, <-idCh)
		}
		return nil
	}
	t.Cleanup(func() { faultHook = nil })
	id, err := e.s.Execute(me, ExecReq{PlanID: p.ID, Confirm: "replace", Acknowledge: true})
	if err != nil {
		t.Fatal(err)
	}
	idCh <- id
	_, msg := e.wait(id)
	if !asked || accepted || msg != "" {
		t.Fatalf("测试前提：走到了换地图那一步 %v，取消不该收 %v，任务应完成 %q", asked, accepted, msg)
	}
	if s := e.m.Tasks.Get(id).Snapshot(); s.Cancelled || s.Cancelling {
		t.Errorf("取消没收下，任务不该带取消的状态：%+v", s)
	}
}
