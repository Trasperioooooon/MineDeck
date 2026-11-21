package catalog

import (
	"context"
	"strings"
	"testing"

	"minedeck/internal/ops"
)

func TestPendingResolveBatchLimit(t *testing.T) {
	reg, _ := newCatalog(t)
	ui := ops.Caller{Source: ops.SrcUI, Actor: "本机", Local: true}
	items := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = map[string]any{"id": "p1", "act": "skip"}
		}
		return out
	}
	for _, name := range []string{"instance.import.pending.resolve", "instance.import.pending"} {
		_, err := reg.Do(context.Background(), ui, name, ops.ArgsOf(map[string]any{
			"inst": "生存服", "taskId": "t1", "items": items(500),
		}))
		if err == nil || strings.Contains(err.Error(), "最多 500 项") {
			t.Fatalf("%s 500 项不应被上限拒绝：%v", name, err)
		}
		_, err = reg.Do(context.Background(), ui, name, ops.ArgsOf(map[string]any{
			"inst": "生存服", "taskId": "t1", "items": items(501),
		}))
		if err == nil || !strings.Contains(err.Error(), "最多 500 项") {
			t.Fatalf("%s 501 项应拒绝：%v", name, err)
		}
	}
}
