package companion_test

import (
	"encoding/json"
	"testing"

	"minedeck/internal/companion"
)

// 任务结束的那句话（游戏里的模板通知、「最近的事件」都用它）。模组 0.2.1 起 give 没交够时报 incomplete、一个都没交出去时报
// failed，带 want 和原因；不到半秒就结束的不写用时。
func TestResultText(t *testing.T) {
	n := func(v int) *int { return &v }
	task := func(op string, args map[string]any) *companion.TaskRef {
		b, _ := json.Marshal(args)
		return &companion.TaskRef{Task: companion.Task{ID: 7, Op: op, Args: b}}
	}
	give := task("give", map[string]any{"item": "minecraft:oak_log", "count": 5, "player": "Steve"})
	for _, c := range []struct {
		name string
		e    companion.Event
		want string
	}{
		{"give 做完了", companion.Event{Task: give, Result: "done", Given: n(5), Have: n(2), Want: n(5), Seconds: 4.2},
			"交出 oak_log ×5 给 Steve 做完了：交出了 5 个（要 5 个），背包里还有 2 个，用时 4 秒。"},
		{"give 没交够", companion.Event{Task: give, Result: "incomplete", Given: n(2), Have: n(0), Want: n(5), Seconds: 9.6,
			Detail: "原来只有 2 个，也没弄到更多"},
			"交出 oak_log ×5 给 Steve 没做够：交出了 2 个（要 5 个），背包里还有 0 个，原来只有 2 个，也没弄到更多，用时 10 秒。"},
		{"give 一个都没交出去，马上就结束了", companion.Event{Task: give, Result: "failed", Given: n(0), Have: n(3), Want: n(5), Seconds: 0.1,
			Detail: "Steve 不在伙伴所在的维度"},
			"交出 oak_log ×5 给 Steve 失败了：交出了 0 个（要 5 个），背包里还有 3 个，Steve 不在伙伴所在的维度。"},
		{"0.2.0 的 give 没有 want", companion.Event{Task: give, Result: "done", Given: n(5), Have: n(0), Seconds: 3},
			"交出 oak_log ×5 给 Steve 做完了：交出了 5 个，背包里还有 0 个，用时 3 秒。"},
		{"collect 照旧", companion.Event{Task: task("collect", map[string]any{"item": "minecraft:oak_log", "count": 3}), Result: "done",
			Gained: n(3), Want: n(3), Have: n(3), Seconds: 12.4},
			"采集 oak_log ×3 做完了：这次拿到 3 个（要 3 个），背包里共 3 个，用时 12 秒。"},
		{"不认识的 result 当成失败", companion.Event{Task: task("goto", map[string]any{"x": 1, "z": 2}), Result: "exploded", Seconds: 0.6},
			"去 (1, 2) 失败了：用时 1 秒。"},
	} {
		if got := companion.ResultText(c.e); got != c.want {
			t.Errorf("%s：\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}
