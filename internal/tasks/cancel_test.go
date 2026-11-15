package tasks

import (
	"context"
	"testing"
)

func TestCancelAndCommitAreExclusive(t *testing.T) {
	m := NewManager()
	task, ctx := m.New("创建", nil)
	if m.Cancel(task.ID()) {
		t.Fatal("未显式开放的任务不能取消")
	}
	task.EnableCancel()
	if !task.Snapshot().CanCancel || !m.Cancel(task.ID()) {
		t.Fatal("应该接受取消")
	}
	if ctx.Err() != context.Canceled || task.BeginCommit() {
		t.Fatal("取消后不能发布")
	}
	if !task.Cancelled() {
		t.Fatal("收下取消以后要认得出这个任务是被取消的（收尾时按「已取消」报，不报成失败原因）")
	}
	if m.Cancel(task.ID()) || task.Snapshot().CanCancel {
		t.Fatal("不应重复接受取消")
	}
	task.Fail(ctx.Err())
	next, _ := m.New("另一次", nil)
	next.EnableCancel()
	if next.Cancelled() {
		t.Fatal("没取消过的任务不算被取消")
	}
	if !next.BeginCommit() || m.Cancel(next.ID()) || next.Snapshot().CanCancel {
		t.Fatal("开始发布后不能接受取消")
	}
	next.Finish("完成")
	if m.Cancel(next.ID()) || m.Cancel("missing") {
		t.Fatal("结束或不存在的任务不能取消")
	}
}

// 任务自己的取消入口（换地图的 worldimport.Service.Cancel）收下取消：NoteCancel 只记下「人取消了」，不开放通用的取消；
// 之后 EndCancelled 按「已取消」结束。结束了的任务不再记。
func TestNoteCancel(t *testing.T) {
	m := NewManager()
	task, _ := m.New("导入地图", []string{"核对", "复制"})
	task.StartStep(1)
	task.NoteCancel()
	if s := task.Snapshot(); !s.Cancelling || s.CanCancel || s.Ended || !task.Cancelled() {
		t.Fatalf("记下取消以后是「正在取消」，不开放通用的取消：%+v", s)
	}
	if m.Cancel(task.ID()) {
		t.Fatal("NoteCancel 不该让任务页的通用取消生效")
	}
	task.EndCancelled(errorString("已取消，没有替换任何文件"))
	if s := task.Snapshot(); !s.Cancelled || s.Cancelling || s.Err != "已取消，没有替换任何文件" || s.Steps[1].Status != "cancelled" {
		t.Fatalf("应按「已取消」结束：%+v", s)
	}
	done, _ := m.New("做完的", nil)
	done.Finish("ok")
	done.NoteCancel()
	if s := done.Snapshot(); s.Cancelled || s.Cancelling {
		t.Fatalf("结束了的任务不再记取消：%+v", s)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }
