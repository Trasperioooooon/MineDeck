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
	if m.Cancel(task.ID()) || task.Snapshot().CanCancel {
		t.Fatal("不应重复接受取消")
	}
	task.Fail(ctx.Err())
	next, _ := m.New("另一次", nil)
	next.EnableCancel()
	if !next.BeginCommit() || m.Cancel(next.ID()) || next.Snapshot().CanCancel {
		t.Fatal("开始发布后不能接受取消")
	}
	next.Finish("完成")
	if m.Cancel(next.ID()) || m.Cancel("missing") {
		t.Fatal("结束或不存在的任务不能取消")
	}
}
