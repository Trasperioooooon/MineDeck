package chatai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"minedeck/internal/ai"
)

// 存在实例目录 gameai-state.json 里的状态：当天的次数和用量（重启 MineDeck 也不清零）、最近出现过的名字、最近的问答。
// 冷却和防刷屏的计数只在内存里。

const (
	maxHistory = 100 // 最近的问答留几条
	maxSeen    = 50  // 最近出现过的名字留几个
)

// Record 一次触发的记录（界面上「最近的问答和执行记录」）。游戏里的回答可能截断了，这里存完整的。
type Record struct {
	T        time.Time   `json:"t"`
	Player   string      `json:"player"`
	Kind     string      `json:"kind"` // 同 Action，另有 cooldown / limit / busy / silenced / disabled / failed，以及 task（伙伴的任务通知）
	Opts     []Opt       `json:"opts,omitempty"`
	Question string      `json:"question,omitempty"`
	Reply    string      `json:"reply,omitempty"`   // 发到游戏里的回答（完整的）或系统提示
	Private  bool        `json:"private,omitempty"` // 只发给了提问的人
	Exec     bool        `json:"exec,omitempty"`    // 这次给了 AI 执行命令的工具
	Commands []CmdRecord `json:"commands,omitempty"`
	// Companion 叫着这个伙伴的名字说的（回复以它的名义发出）；Kind 为 task 时是任务通知（模板消息，不调用 AI）的伙伴。
	Companion string             `json:"companion,omitempty"`
	Actions   []ActionRecord     `json:"actions,omitempty"` // AI 让伙伴做的事
	Usage     *ai.Usage          `json:"usage,omitempty"`
	Cost      map[string]float64 `json:"cost,omitempty"` // 币种 → 金额（估）；查不到价格时没有
	Cut       bool               `json:"cut,omitempty"`  // 回答太长，游戏里截断了
	Err       string             `json:"err,omitempty"`  // 出错的详情（给服主看，游戏里只说「暂时连不上」之类）
	Note      string             `json:"note,omitempty"` // 补充说明
}

// CmdRecord AI 要执行的一条命令。
type CmdRecord struct {
	Command string `json:"command"`          // 模型给的原文
	Run     string `json:"run,omitempty"`    // 实际执行的（规范化以后）
	OK      bool   `json:"ok"`               // 执行了
	Reason  string `json:"reason,omitempty"` // 没执行的原因
	Output  string `json:"output,omitempty"` // 执行后 1 秒内的控制台输出
}

// counters 当天的次数和用量（按本地日期，过了 0 点清零）。
type counters struct {
	Day     string         `json:"day"`
	Players map[string]int `json:"players"` // 名字小写 → 今天问了几次
	Count   int            `json:"count"`   // 全服今天问了几次
	Tokens  int64          `json:"tokens"`  // 全服今天用了多少 token
}

type stateFileV struct {
	Counters counters             `json:"counters"`
	Seen     map[string]time.Time `json:"seen"`
	History  []Record             `json:"history"`
}

// roll 换了一天就清零。
func (c *counters) roll(now time.Time) {
	if d := now.Local().Format("2006-01-02"); c.Day != d {
		*c = counters{Day: d, Players: map[string]int{}}
	}
	if c.Players == nil {
		c.Players = map[string]int{}
	}
}

func loadState(dir string) stateFileV {
	st := stateFileV{Seen: map[string]time.Time{}}
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Seen == nil {
		st.Seen = map[string]time.Time{}
	}
	if st.Counters.Players == nil {
		st.Counters.Players = map[string]int{}
	}
	if len(st.History) > maxHistory {
		st.History = st.History[len(st.History)-maxHistory:]
	}
	return st
}

// see 记下一个出现过的名字（进服、聊天），只留最近的 maxSeen 个。
func see(seen map[string]time.Time, name string, t time.Time) {
	for k := range seen {
		if strings.EqualFold(k, name) && k != name {
			delete(seen, k)
		}
	}
	seen[name] = t
	if len(seen) <= maxSeen {
		return
	}
	type kv struct {
		k string
		t time.Time
	}
	var all []kv
	for k, v := range seen {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
	for _, x := range all[maxSeen:] {
		delete(seen, x.k)
	}
}

// recentNames 最近出现过的名字，新的在前。
func recentNames(seen map[string]time.Time) []string {
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return seen[out[i]].After(seen[out[j]]) })
	return out
}
