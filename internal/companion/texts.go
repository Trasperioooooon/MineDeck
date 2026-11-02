package companion

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 给人看的话：任务是什么、结果怎么样。界面上「最近的事件」和游戏里的任务通知（模板消息，不调用 AI）都用它。

// shortID 物品、生物的 ID 去掉 minecraft: 前缀（模组的保留命名空间）。
func shortID(id string) string { return strings.TrimPrefix(id, "minecraft:") }

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func fmtNum(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// TaskArgs 任务对象里整理过的参数。
func TaskArgs(t *Task) map[string]any {
	m := map[string]any{}
	if t != nil && len(t.Args) > 0 {
		_ = json.Unmarshal(t.Args, &m)
	}
	return m
}

// TaskTitle 一个任务的说法：「采集 oak_log ×3」「跟着 Steve」「去 (100, 64, -20)」……
func TaskTitle(t *Task) string {
	if t == nil {
		return "任务"
	}
	return ActionTitle(t.Op, TaskArgs(t))
}

// ActionTitle 一个动作的说法（args 是指令的参数）。
func ActionTitle(op string, a map[string]any) string {
	str := func(k string) string { s, _ := a[k].(string); return s }
	n := func(k string) (string, bool) {
		if f, ok := num(a[k]); ok {
			return fmtNum(f), true
		}
		return "", false
	}
	switch op {
	case "collect":
		s := "采集 " + shortID(str("item"))
		if c, ok := n("count"); ok {
			s += " ×" + c
		}
		return s
	case "follow":
		if p := str("player"); p != "" {
			return "跟着 " + p
		}
		return "跟着主人"
	case "goto":
		x, _ := n("x")
		z, _ := n("z")
		if y, ok := n("y"); ok {
			return fmt.Sprintf("去 (%s, %s, %s)", x, y, z)
		}
		return fmt.Sprintf("去 (%s, %s)", x, z)
	case "attack":
		s := "打 " + shortID(str("mob"))
		if c, ok := n("count"); ok {
			s += " ×" + c
		}
		if d, ok := n("duration"); ok {
			s += "（守 " + d + " 秒）"
		}
		return s
	case "give":
		s := "交出 " + shortID(str("item"))
		if c, ok := n("count"); ok {
			s += " ×" + c
		}
		if p := str("player"); p != "" {
			s += " 给 " + p
		}
		return s
	case "stop":
		return "停下"
	case "say":
		return "说话"
	case "status":
		return "看状态"
	}
	return op
}

func posText(p Pos) string {
	return fmt.Sprintf("%s %s, %s, %s", shortID(p.Dim), fmtNum(p.X), fmtNum(p.Y), fmtNum(p.Z))
}

// ResultText task_finished 的一句话：「采集 oak_log ×3 做完了：这次拿到 3 个，背包里共 5 个，用时 10 秒。」
func ResultText(e Event) string {
	var t *Task
	if e.Task != nil {
		t = &e.Task.Task
	}
	head := TaskTitle(t)
	var verb string
	switch KnownResult(e.Result) {
	case ResultDone:
		verb = "做完了"
	case ResultIncomplete:
		verb = "没做够"
	case ResultFailed:
		verb = "失败了"
	case ResultCancelled:
		verb = "停下了"
	case ResultSuperseded:
		verb = "被新任务顶掉了"
	case ResultTimeout:
		verb = "超时了"
	}
	var parts []string
	iv := func(p *int) (int, bool) {
		if p == nil {
			return 0, false
		}
		return *p, true
	}
	op := ""
	if t != nil {
		op = t.Op
	}
	switch op {
	case "collect":
		if g, ok := iv(e.Gained); ok {
			s := fmt.Sprintf("这次拿到 %d 个", g)
			if w, ok := iv(e.Want); ok {
				s += fmt.Sprintf("（要 %d 个）", w)
			}
			parts = append(parts, s)
		}
		if h, ok := iv(e.Have); ok {
			parts = append(parts, fmt.Sprintf("背包里共 %d 个", h))
		}
	case "attack":
		if k, ok := iv(e.Killed); ok {
			s := fmt.Sprintf("打到 %d 只", k)
			if w, ok := iv(e.Want); ok {
				s += fmt.Sprintf("（要 %d 只）", w)
			}
			parts = append(parts, s)
		}
	case "give":
		if g, ok := iv(e.Given); ok {
			s := fmt.Sprintf("交出了 %d 个", g)
			if w, ok := iv(e.Want); ok { // 模组 0.2.1 起 give 也带 want
				s += fmt.Sprintf("（要 %d 个）", w)
			}
			parts = append(parts, s)
		}
		if h, ok := iv(e.Have); ok {
			parts = append(parts, fmt.Sprintf("背包里还有 %d 个", h))
		}
	}
	if d := strings.TrimSpace(e.Detail); d != "" {
		parts = append(parts, d)
	}
	if sec := int64(e.Seconds + 0.5); sec > 0 { // 不到半秒就结束的（多半是一下子就失败了）不写用时
		parts = append(parts, "用时 "+fmtNum(float64(sec))+" 秒")
	}
	s := head + " " + verb
	if len(parts) > 0 {
		s += "：" + strings.Join(parts, "，")
	}
	return s + "。"
}

// StateText 伙伴状态的说法。
func StateText(state string) string {
	switch KnownState(state) {
	case StateStarting:
		return "准备中"
	case StateIdle:
		return "空闲"
	}
	return "忙"
}
