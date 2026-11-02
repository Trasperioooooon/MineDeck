package chatai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"minedeck/internal/ai"
	"minedeck/internal/companion"
	"minedeck/internal/ops"
)

// AI 伙伴（设计文档 §7.2，第七阶段）。伙伴是服务器里一个像玩家一样的角色，PlayerEngine 做身体，大脑在这里：
//   - 实例启用了伙伴、MineDeck 连着伙伴模组时，有执行权限、又没带 /问 的玩家提问，AI 多一组伙伴工具（跟随、采集、去某处、
//     打怪、给东西、停下、说话，加上只读的查状态）。权限只在代码里判断：没有权限的人提问时这些工具根本不在请求里。
//   - 玩家可以直接叫伙伴的名字：「@小方 跟着我」等同于对小方说的「@AI 跟着我」，回复以小方的名义发出，记在 AI 伙伴的额度里。
//   - 任务做完或失败时，按模板私下告诉发起的玩家（NotifyTask），不再调用 AI，不算次数。
// 伙伴的动作不走操作层（游戏内调用者的写操作在操作层一律 403），每个动作记一条操作记录 companion.action（来源 companion）。

// Companions 实例的 AI 伙伴：ops/catalog 用 companion.Service 实现，测试里是假的。
type Companions interface {
	// Live 实例的伙伴：启用了伙伴、而且连着伙伴模组时 ok 为 true；list 是现有的伙伴，ops 是模组支持的指令。
	Live(inst string) (list []companion.Companion, ops []string, ok bool)
	// Act 让伙伴做一个动作（companion.ActionOps 之一）；origin 是发起的玩家，任务结束时据此通知他。
	Act(ctx context.Context, inst, op string, args map[string]any, origin companion.Origin) (*companion.ActResult, error)
}

// maxActions 一个问题最多让伙伴做几个动作（查状态不算）。
const maxActions = 4

// ActionRecord AI 让伙伴做的一个动作（最近的问答里显示）。
type ActionRecord struct {
	Op        string `json:"op"`
	Companion string `json:"companion,omitempty"` // 落在哪个伙伴身上
	Title     string `json:"title"`               // 「采集 oak_log ×3」
	OK        bool   `json:"ok"`
	Task      int64  `json:"task,omitempty"`   // 开始了的任务编号
	Reason    string `json:"reason,omitempty"` // 没做成的原因
}

// companionsLive 实例现在的伙伴（没启用、没连着时 ok 为 false）。
func (s *Service) companionsLive(inst string) ([]companion.Companion, []string, bool) {
	if s.comps == nil {
		return nil, nil, false
	}
	return s.comps.Live(inst)
}

// companionPrefixes 「@名字」：伙伴的名字也能触发（排在设置里的前缀后面：撞了的话前缀优先）。
func companionPrefixes(list []companion.Companion) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		if c.Name != "" {
			out = append(out, "@"+c.Name)
		}
	}
	return out
}

// addressed 认出的前缀是不是某个伙伴的名字：是的话返回它的名字。设置里的前缀和别名优先。
func addressed(prefix string, rules Rules, list []companion.Companion) string {
	same := func(p string) bool { rest, ok := cutPrefix(prefix, p); return ok && rest == "" }
	for _, p := range rules.Prefixes {
		if same(p) {
			return ""
		}
	}
	for _, c := range list {
		if c.Name != "" && same("@"+c.Name) {
			return c.Name
		}
	}
	return ""
}

// 伙伴工具的参数说明（固定不变：同样的设置得到一字不差的工具清单，前缀缓存才能命中）。
const companionParam = `"companion":{"type":"string","description":"伙伴的名字；只有一个伙伴、或者玩家是叫着某个伙伴说的，可以不写"}`

type companionTool struct {
	op, desc, props string
	required        []string
}

var companionTools = []companionTool{
	{"follow", "让伙伴跟着一个玩家走，直到被停下、换成别的任务、超时或那人下线。player 不写就是提问的人。",
		`"player":{"type":"string","description":"玩家名"},"timeout":{"type":"integer","description":"最多跟多少秒，0 或不写表示一直跟"}`, nil},
	{"collect", "让伙伴采集或合成若干个某物，比如砍 10 个橡木原木、挖 20 个圆石。是「再弄 N 个」：背包里已有的不算。物品写完整的 ID（如 minecraft:oak_log；模组物品写它自己的命名空间）。",
		`"item":{"type":"string","description":"物品 ID，如 minecraft:oak_log"},"count":{"type":"integer","description":"数量 1–2304，默认 1"},"timeout":{"type":"integer","description":"最多做多少秒，默认 600"}`, []string{"item"}},
	{"goto", "让伙伴走到某个坐标（只在它现在所在的维度里走）。y 不写就只管水平位置。",
		`"x":{"type":"integer"},"y":{"type":"integer"},"z":{"type":"integer"},"timeout":{"type":"integer","description":"最多走多少秒，默认 300"}`, []string{"x", "z"}},
	{"attack", "让伙伴打某种生物（生物 ID，如 minecraft:zombie）：只打以它现在的位置为中心、radius 格以内的，打够 count 只或者打满 duration 秒就结束；只给 duration 是「守在这里，来一只打一只」。永远不打玩家、伙伴、驯服的、起了名字的、拴着的生物，以及服主禁止的种类（村民、铁傀儡……）。",
		`"mob":{"type":"string","description":"生物 ID，如 minecraft:zombie"},"count":{"type":"integer","description":"1–64"},"duration":{"type":"integer","description":"1–1800 秒"},"radius":{"type":"integer","description":"1–48 格，默认 16"},"timeout":{"type":"integer"}`, []string{"mob"}},
	{"give", "让伙伴把背包里的东西交给一个玩家（走到旁边扔给他）。player 不写就是提问的人；fetch 为 true 时背包里不够就先去弄。",
		`"item":{"type":"string","description":"物品 ID"},"count":{"type":"integer","description":"默认 1"},"player":{"type":"string","description":"玩家名"},"fetch":{"type":"boolean"},"timeout":{"type":"integer"}`, []string{"item"}},
	{"stop", "让伙伴停下当前的任务；all 为 true 时所有伙伴都停下。",
		`"all":{"type":"boolean","description":"所有伙伴都停下"}`, nil},
	{"say", "让伙伴以它自己的名字在聊天栏说一句话（最多 256 个字）；to 写玩家名时只发给他一个人。平常回答玩家不用它，直接回答就行。",
		`"text":{"type":"string"},"to":{"type":"string","description":"只发给这个玩家"}`, []string{"text"}},
	{"status", "查看伙伴的状态：位置、血量、当前任务、上次任务的结果；inventory 为 true 时带上背包。只读。",
		`"inventory":{"type":"boolean","description":"带上背包"}`, nil},
}

const companionToolPrefix = "companion_"

// companionToolList 伙伴工具：只列模组支持的（兼容规则 2：用新指令之前先看 WELCOME 的 ops），顺序固定。
func companionToolList(ops []string) []ai.Tool {
	var out []ai.Tool
	for _, t := range companionTools {
		if !slices.Contains(ops, t.op) {
			continue
		}
		req := ""
		if len(t.required) > 0 {
			b, _ := json.Marshal(t.required)
			req = `,"required":` + string(b)
		}
		out = append(out, ai.Tool{Name: companionToolPrefix + t.op, Description: t.desc,
			Schema: json.RawMessage(`{"type":"object","properties":{` + companionParam + `,` + t.props + `}` + req + `}`)})
	}
	return out
}

// companionLines 用户消息里的伙伴：名字、编号、主人、状态、当前任务（每次都变，放在用户消息里，不放进系统提示）。
func companionLines(list []companion.Companion) string {
	var parts []string
	for _, c := range list {
		s := fmt.Sprintf("%s（%d 号", c.Name, c.ID)
		if c.Owner != "" {
			s += "，主人 " + c.Owner
		}
		s += "，" + companion.StateText(c.State)
		if c.Task != nil {
			s += "：" + companion.TaskTitle(c.Task)
		}
		f := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
		s += fmt.Sprintf("，血量 %s/%s，在 %s %s, %s, %s）", f(c.Health), f(c.MaxHealth), strings.TrimPrefix(c.Dim, "minecraft:"), f(c.X), f(c.Y), f(c.Z))
		parts = append(parts, s)
	}
	return strings.Join(parts, "；")
}

// companionAct 执行 AI 要伙伴做的一个动作：先按最新的设置再查一次授权，玩家名不写的补上提问的人，再交给伙伴模组。
func (r *room) companionAct(ctx context.Context, j *job, tc ai.ToolCall, acted *int) (string, bool, ActionRecord) {
	s := r.s
	op := strings.TrimPrefix(tc.Name, companionToolPrefix)
	var args map[string]any
	_ = json.Unmarshal(tc.Args, &args)
	if args == nil {
		args = map[string]any{}
	}
	ar := ActionRecord{Op: op, Title: companion.ActionTitle(op, args)}
	start := s.now()
	note := func(res *companion.ActResult, err error) {
		if s.reg == nil {
			return
		}
		rec := map[string]any{"inst": r.name, "op": op, "args": args}
		if res != nil {
			rec["args"] = res.Sent
			if res.Companion != nil {
				rec["companion"] = res.Companion.Name
			}
			if res.Task != nil {
				rec["task"] = res.Task.ID
			}
			if !res.OK && err == nil {
				err = errors.New(res.Error)
			}
		}
		s.reg.Record(ops.Caller{Source: ops.SrcCompanion, Actor: j.player}, "companion.action", ops.ArgsOf(rec), err, s.now().Sub(start))
	}
	cur := r.settings()
	if !cur.Enabled || !cur.Authorized(j.player) {
		ar.Reason = j.player + " 的执行权限已经被服主收回"
		note(nil, errors.New(ar.Reason))
		return "没有做：" + ar.Reason + "。", true, ar
	}
	if op != "status" {
		if *acted >= maxActions {
			ar.Reason = fmt.Sprintf("一个问题最多让伙伴做 %d 件事", maxActions)
			note(nil, errors.New(ar.Reason))
			return "没有做：" + ar.Reason + "。", true, ar
		}
		*acted++
	}
	switch op {
	case "follow", "give":
		if p, _ := args["player"].(string); strings.TrimSpace(p) == "" {
			args["player"] = j.player // 「跟着我」「给我」：不写就是提问的人（不是伙伴的主人）
		}
	case "status":
		if _, ok := args["inventory"]; !ok {
			args["inventory"] = false
		}
	case "say":
		if t, ok := args["text"].(string); ok { // 伙伴说的话会显示在聊天栏：去掉 § 格式码、换行和控制字符（同 tellraw）
			args["text"] = strings.TrimSpace(cleanText(t))
		}
	}
	ar.Title = companion.ActionTitle(op, args)
	res, err := s.comps.Act(ctx, r.name, op, args, companion.Origin{Player: j.player, Companion: j.companion})
	note(res, err)
	if res != nil && res.Companion != nil {
		ar.Companion = res.Companion.Name
	}
	if err != nil {
		ar.Reason = err.Error()
		return "没有做：" + r.redactText(err.Error()) + "。请用中文把原因告诉玩家。", true, ar
	}
	if !res.OK {
		ar.Reason = res.Error
		return fmt.Sprintf("伙伴模组拒绝了（%s）：%s。请用中文把原因告诉玩家。", res.Code, r.redactText(res.Error)), true, ar
	}
	ar.OK = true
	who := "伙伴"
	if res.Companion != nil {
		who = "「" + res.Companion.Name + "」"
	}
	switch {
	case res.Task != nil:
		ar.Task = res.Task.ID
		lim := "不限时"
		if res.Task.Timeout > 0 {
			lim = fmt.Sprintf("最多 %d 秒", res.Task.Timeout)
		}
		return fmt.Sprintf("%s开始%s了（任务 %d，%s）。做完或失败时 MineDeck 会自动私下告诉 %s，不用等它做完，直接用一句话回答玩家就行。",
			who, companion.TaskTitle(res.Task), res.Task.ID, lim, j.player), false, ar
	case op == "status":
		var body struct {
			Companions json.RawMessage `json:"companions"`
		}
		_ = res.Reply.Decode(&body)
		return wrapData(r.redactText(string(body.Companions))), false, ar
	case op == "stop":
		var body struct {
			Stopped []int64 `json:"stopped"`
		}
		_ = res.Reply.Decode(&body)
		if len(body.Stopped) == 0 {
			return who + "本来就没有在做的任务。", false, ar
		}
		return fmt.Sprintf("已停下 %d 个任务。", len(body.Stopped)), false, ar
	case op == "say":
		return who + "已经说了。", false, ar
	}
	return "做了。", false, ar
}

// NotifyTask 有人让伙伴干的活做完、没做够、失败或超时了（companion.Service 的 OnFinish）：
// 按模板私下告诉发起的玩家，开头是伙伴的名字；不调用 AI、不算次数、不扣额度。他不在线、游戏内 AI 关着时不发，只记一笔。
func (s *Service) NotifyTask(f companion.Finished) {
	r, err := s.room(f.Inst)
	if err != nil {
		return
	}
	rec := Record{T: s.now(), Player: f.Origin.Player, Kind: "task", Companion: f.Companion, Private: true,
		Actions: []ActionRecord{{Op: f.Op, Companion: f.Companion, Title: f.Text, OK: f.Result == companion.ResultDone, Task: f.TaskID}}}
	switch {
	case !r.settings().Enabled:
		return
	case validSender(f.Origin.Player, true) != "":
		return // 名字要写进 tellraw 的目标：不合规的不发（正常走不到这里，发起的人是认出来的聊天发送者）
	case !containsName(s.b.Online(f.Inst), f.Origin.Player):
		rec.Note = f.Origin.Player + " 不在线，没有发通知：" + f.Text
	default:
		color := colorBody
		if f.Result != companion.ResultDone {
			color = colorSystem
		}
		msgs, _ := Pack(f.Text, MaxMsgRunes, MaxMsgs)
		head := "[" + f.Companion + "] "
		for _, m := range msgs {
			_ = s.b.Send(f.Inst, Tellraw(f.Origin.Player, head, m, color), "[MineDeck] 伙伴「"+f.Companion+"」的任务通知（只发给 "+f.Origin.Player+"）："+m)
		}
		rec.Reply = strings.Join(msgs, " ")
	}
	r.record(rec)
}

// companionHelp /帮助 里伙伴的那一行。
func companionHelp(prefix string, list []companion.Companion, authorized bool) string {
	if len(list) == 0 {
		return ""
	}
	name := list[0].Name
	text := fmt.Sprintf("\n也可以直接叫伙伴的名字，比如 @%s 你好（现有：%s）。", name, namesOf(list))
	if authorized {
		text += fmt.Sprintf("你能让伙伴跟着你、采集、去某处、打怪、给你东西、停下、说话，比如 @%s 跟着我、%s 让%s砍 10 个木头；活干完了会私下告诉你。", name, prefix, name)
	} else {
		text += "只有服主授权的人能让伙伴干活。"
	}
	return text
}

func namesOf(list []companion.Companion) string {
	var n []string
	for _, c := range list {
		n = append(n, c.Name)
	}
	return strings.Join(n, "、")
}

// featureOf 这次记在哪项功能名下：叫着伙伴的名字说的是 AI 伙伴，其余是游戏内 AI。
func featureOf(companionName string) ai.Feature {
	if companionName != "" {
		return ai.FeatCompanion
	}
	return ai.FeatGameChat
}

func quotaMsg(f ai.Feature) string {
	if f == ai.FeatCompanion {
		return "AI 伙伴今天的额度用完了，明天再来吧。"
	}
	return "游戏内 AI 今天的额度用完了，明天再来吧。"
}
