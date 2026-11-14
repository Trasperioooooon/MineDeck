package catalog

import (
	"context"
	"testing"

	"minedeck/internal/ops"
)

func TestTaskCancelOnlyBeforeCommit(t *testing.T) {
	r, m, _ := newModCatalog(t)
	task, ctx := m.Tasks.New("新建", nil)
	task.EnableCancel()
	a := ops.ArgsOf(map[string]string{"id": task.ID()})
	if _, err := r.Do(context.Background(), ops.Caller{Source: ops.SrcMCP}, "task.cancel", a); err == nil {
		t.Fatal("AI 不应直接取消人工创建任务")
	}
	if _, err := r.Do(context.Background(), human, "task.cancel", a); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("没有取消任务 context")
	}
	if task.Snapshot().Ended {
		t.Fatal("应由任务清理完再结束")
	}
	if _, err := r.Do(context.Background(), human, "task.cancel", a); ops.CodeOf(err) != 409 {
		t.Fatalf("重复取消应为409: %v", err)
	}
	other, _ := m.Tasks.New("发布", nil)
	other.EnableCancel()
	other.BeginCommit()
	if _, err := r.Do(context.Background(), human, "task.cancel", ops.ArgsOf(map[string]string{"id": other.ID()})); ops.CodeOf(err) != 409 {
		t.Fatalf("提交期间应为409: %v", err)
	}
	if _, err := r.Do(context.Background(), human, "task.cancel", ops.ArgsOf(map[string]string{"id": "missing"})); ops.CodeOf(err) != 404 {
		t.Fatalf("不存在应为404: %v", err)
	}
}

func TestTaskCancelCallerPermissions(t *testing.T) {
	r, m, _ := newModCatalog(t)
	for _, tc := range []struct {
		caller  ops.Caller
		allowed bool
	}{
		{ops.Caller{Source: ops.SrcUI, Local: true}, true},
		{ops.Caller{Source: ops.SrcAPI, Local: true}, true},
		{ops.Caller{Source: ops.SrcLAN, Actor: "192.0.2.1"}, true},
		{ops.Caller{Source: ops.SrcAssistant, Local: true}, false},
		{ops.Caller{Source: ops.SrcMCP, Local: true}, false},
		{ops.Caller{Source: ops.SrcGameChat, Actor: "Steve"}, false},
		{ops.Caller{Source: ops.SrcCompanion, Actor: "伙伴"}, false},
		{ops.Caller{Source: ops.SrcSchedule}, false},
	} {
		task, ctx := m.Tasks.New("新建", nil)
		task.EnableCancel()
		_, err := r.Do(context.Background(), tc.caller, "task.cancel", ops.ArgsOf(map[string]string{"id": task.ID()}))
		if tc.allowed {
			if err != nil || ctx.Err() != context.Canceled {
				t.Errorf("%s人工取消应成功: %v", tc.caller.Source, err)
			}
		} else if ops.CodeOf(err) != 403 || ctx.Err() != nil {
			t.Errorf("%s不应取消: %v context=%v", tc.caller.Source, err, ctx.Err())
		}
	}
}
