package chatai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"minedeck/internal/ai"
	"minedeck/internal/companion"
	"minedeck/internal/mclog"
	"minedeck/internal/ops"
)

// answer 回答一个问题：一次新的对话，循环请求 AI、执行工具，直到 AI 给出回答（最多请求 4 次）。
// 用 AI 接入层的顺序照应用内助手：Begin → Client → NewConversation → Chat → Record → AddResult / AddToolResults；
// AddResult 报错（空回复）按「模型没有给出回复」处理。
func (r *room) answer(j *job) {
	s := r.s
	start := s.now()
	ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
	defer cancel()
	caller := ops.Caller{Source: ops.SrcGameChat, Actor: j.player}
	set := r.settings()
	if !set.Enabled { // 排队的时候服主把游戏内 AI 关掉了
		return
	}
	rec := Record{T: j.t, Player: j.player, Kind: string(ActAsk), Opts: j.ask.Opts, Question: j.ask.Question, Private: j.ask.Private, Exec: j.exec,
		Companion: j.companion}
	var usage ai.Usage
	cost := map[string]float64{}
	reply, failMsg, err := r.converse(ctx, j, set, caller, &usage, cost, &rec.Commands, &rec.Actions)
	speaker := "AI"
	if j.companion != "" {
		speaker = j.companion // 叫着伙伴的名字问的：以它的名义回答
	}
	if usage.Total() > 0 {
		rec.Usage = &usage
	}
	if len(cost) > 0 {
		rec.Cost = cost
	}
	if err != nil {
		rec.Kind, rec.Err = "failed", r.redactText(err.Error())
		rec.Reply, _ = r.sayAs(speaker, j.player, true, failMsg, colorSystem)
		rec.Private = true
	} else {
		rec.Reply, rec.Cut = r.sayAs(speaker, j.player, j.ask.Private, reply, colorBody)
		if rec.Cut {
			rec.Reply = reply // 游戏里截断了，这里存完整的
		}
	}
	r.record(rec)
	if s.reg != nil {
		args := map[string]any{"inst": r.name, "chars": utf8.RuneCountInString(j.ask.Question), "opts": optLabels(j.ask.Opts),
			"exec": j.exec, "result": rec.Kind, "usage": usage.Total(), "commands": len(rec.Commands)} // 键名不能带 token：操作记录会把它当密钥写成 ***
		if j.companion != "" || len(rec.Actions) > 0 { // AI 伙伴（第七阶段）
			args["companion"], args["actions"] = j.companion, len(rec.Actions)
		}
		s.reg.Record(caller, "gameai.ask", ops.ArgsOf(args), err, s.now().Sub(start))
	}
}

// optLabels 选项写成 /私、/问 这样（操作记录里用）。
func optLabels(opts []Opt) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, "/"+string(o))
	}
	return out
}

var (
	errRefused = errors.New("模型拒绝回答这个问题")
	errEmpty   = errors.New("模型没有给出回复")
)

// converse 和 AI 来回：返回回答；出错时返回给玩家看的提示和错误详情（给服主看）。
func (r *room) converse(ctx context.Context, j *job, set Settings, caller ops.Caller, usage *ai.Usage, cost map[string]float64, cmds *[]CmdRecord,
	acts *[]ActionRecord) (string, string, error) {
	s := r.s
	feat := j.feat
	if feat == "" {
		feat = ai.FeatGameChat
	}
	tools := []ai.Tool{gameInfoTool}
	if j.exec {
		tools = append(tools, runCommandTool(set.enabledTemplates()))
	}
	// AI 伙伴（第七阶段）：实例启用了伙伴、连着模组时才提；有执行权限、又没带 /问 的才给工具（权限只在代码里判断）
	comps, compOps, live := s.companionsLive(r.name)
	mode := compNone
	if live {
		mode = compTalk
		if j.exec {
			mode = compAct
			tools = append(tools, companionToolList(compOps)...)
		}
	}
	cl, err := s.ai.Client(true)
	if err != nil {
		return "", failText(err), err
	}
	conv, err := cl.NewConversation(systemPrompt(j.exec, mode), tools)
	if err != nil {
		return "", failText(err), err
	}
	if err := conv.AddUser(userMessage(j, r.redactText(j.ask.Question), comps, live)); err != nil {
		return "", failText(err), err
	}
	calls, ran, acted := 0, 0, 0
	for round := 0; ; round++ {
		if err := s.ai.Begin(feat); err != nil {
			return "", failText(err), err
		}
		res, err := cl.Chat(ctx, conv, nil)
		if c := s.ai.Record(feat, res); c != nil && c.Amount > 0 { // 不管成没成功，花了的都要记
			cost[c.Currency] += c.Amount
		}
		if res != nil {
			usage.Add(res.Usage)
			r.addTokens(res.Usage.Total())
		}
		if err == nil && res.Stop == ai.StopRefusal {
			err = errRefused
		}
		if err == nil && conv.AddResult(res) != nil { // 空回复：Claude 空的 end_turn、OpenAI 兼容只给了思考内容
			err = errEmpty
		}
		if err != nil {
			if ctx.Err() != nil && s.ctx.Err() == nil {
				err = fmt.Errorf("回复超时（%v）：%w", s.timeout, err)
			}
			if errors.Is(err, errEmpty) && (len(*cmds) > 0 || len(*acts) > 0) {
				return summary(*cmds, *acts), "", nil // 命令执行了、只是没有收尾的话：照实说执行了什么
			}
			return "", failText(err), err
		}
		if len(res.ToolCalls) == 0 {
			if t := strings.TrimSpace(res.Text); t != "" {
				return t, "", nil
			}
			if len(*cmds) > 0 || len(*acts) > 0 {
				return summary(*cmds, *acts), "", nil
			}
			return "", failText(errEmpty), errEmpty // 只有空白
		}
		if round >= maxRounds-1 {
			if t := strings.TrimSpace(res.Text); t != "" {
				return t, "", nil
			}
			if len(*cmds) > 0 || len(*acts) > 0 {
				return summary(*cmds, *acts), "", nil
			}
			return "", "这个问题有点复杂，AI 没能在几步之内答完，换个问法试试。", errors.New("请求 AI 的次数到上限了")
		}
		results := make([]ai.ToolResult, 0, len(res.ToolCalls))
		for _, tc := range res.ToolCalls {
			var content string
			isErr := true
			switch {
			case res.Stop == ai.StopMaxTokens:
				content = "回复到了输出上限被截断，这次调用的参数可能不完整，没有执行。"
			case tc.Bad != "":
				content = "参数格式不对（" + tc.Bad + "），没有执行。请重新调用。"
			case calls >= maxToolCalls:
				content = fmt.Sprintf("这个问题已经调用了 %d 次工具，到上限了，没有执行。请根据已经拿到的信息回答。", maxToolCalls)
			case tc.Name == gameInfoTool.Name:
				calls++
				content, isErr = r.gameInfo(ctx, caller)
			case tc.Name == "run_command" && j.exec:
				calls++
				var cr CmdRecord
				content, isErr, cr = r.runCommand(ctx, j, tc, &ran)
				*cmds = append(*cmds, cr)
			case mode == compAct && strings.HasPrefix(tc.Name, companionToolPrefix) && slices.Contains(compOps, strings.TrimPrefix(tc.Name, companionToolPrefix)) &&
				slices.Contains(companion.ActionOps, strings.TrimPrefix(tc.Name, companionToolPrefix)):
				calls++
				var a ActionRecord
				content, isErr, a = r.companionAct(ctx, j, tc, &acted)
				*acts = append(*acts, a)
			default: // 没给的工具（没有执行权限时的 run_command）：根本不在请求里，模型硬要调用也不执行
				content = "没有这个工具：" + tc.Name
			}
			results = append(results, ai.ToolResult{ID: tc.ID, Content: content, IsError: isErr})
		}
		if err := conv.AddToolResults(results); err != nil {
			return "", failText(err), err
		}
	}
}

// failText 出错时给玩家看的话（详情只记给服主看）。
func failText(err error) string {
	var qe *ai.QuotaError
	switch {
	case errors.As(err, &qe):
		return quotaMsg(qe.Feature)
	case errors.Is(err, ai.ErrDisabled):
		return "主机的 MineDeck 还没配置 AI，现在问不了问题。"
	case errors.Is(err, errRefused):
		return "AI 拒绝回答这个问题，换个说法试试。"
	case errors.Is(err, errEmpty):
		return "AI 没有给出回复，换个说法再问一次。"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "超时"):
		return "AI 回复超时了，请稍后再问。"
	}
	return "AI 暂时连不上，请稍后再问。"
}

// summary 命令执行了（伙伴开始干活了）、AI 却没说话时的回答。
func summary(cmds []CmdRecord, acts []ActionRecord) string {
	var ok, bad []string
	for _, c := range cmds {
		if c.OK {
			ok = append(ok, c.Run)
		} else {
			bad = append(bad, c.Command)
		}
	}
	for _, a := range acts {
		t := a.Title
		if a.Companion != "" {
			t = a.Companion + " " + t
		}
		if a.OK {
			ok = append(ok, t)
		} else {
			bad = append(bad, t)
		}
	}
	var parts []string
	if len(ok) > 0 {
		parts = append(parts, "已执行："+strings.Join(ok, "；"))
	}
	if len(bad) > 0 {
		parts = append(parts, "没执行："+strings.Join(bad, "；"))
	}
	return strings.Join(parts, "。")
}

// addTokens 记进全服当天的 token。
func (r *room) addTokens(n int64) {
	if n <= 0 {
		return
	}
	r.mu.Lock()
	r.st.Counters.roll(r.s.now())
	r.st.Counters.Tokens += n
	r.mu.Unlock()
	r.persist()
}

func (r *room) redactText(s string) string {
	if r.s.redact == nil {
		return s
	}
	return r.s.redact.Text(s)
}

// gameInfo 经操作层调用只读的 game.info（游戏内只放行 InGame 的读操作）。实例由程序填，AI 给的参数一概不用。
func (r *room) gameInfo(ctx context.Context, caller ops.Caller) (string, bool) {
	if r.s.reg == nil {
		return "查不到服务器信息。", true
	}
	out, err := r.s.reg.Do(ctx, caller, "game.info", ops.ArgsOf(map[string]string{"inst": r.name}))
	if err != nil {
		return "查不到服务器信息：" + r.redactText(err.Error()), true
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "查不到服务器信息。", true
	}
	return wrapData(r.redactText(string(b))), false
}

// runCommand 执行 AI 要的一条命令：先按白名单校验（§6.6），通过了才写进服务器，再收集接下来 1 秒的控制台输出交给 AI。
// 校验没过、执行失败的都记一条操作记录（来源 gamechat）。
func (r *room) runCommand(ctx context.Context, j *job, tc ai.ToolCall, ran *int) (string, bool, CmdRecord) {
	s := r.s
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(tc.Args, &a)
	cr := CmdRecord{Command: clip(a.Command, MaxCommandLen+20)}
	start := s.now()
	note := func(run string, err error) {
		if s.reg != nil {
			s.reg.Record(ops.Caller{Source: ops.SrcGameChat, Actor: j.player}, "gameai.command",
				ops.ArgsOf(map[string]string{"inst": r.name, "command": cr.Command, "run": run}), err, s.now().Sub(start))
		}
	}
	if *ran >= maxCommands {
		cr.Reason = fmt.Sprintf("一个问题最多执行 %d 条命令", maxCommands)
		note("", errors.New(cr.Reason))
		return "没有执行：" + cr.Reason + "。", true, cr
	}
	cur := r.settings() // 用最新的设置：排队、回答的时候服主可能收回了授权、关掉了某个模板
	if !cur.Enabled || !cur.Authorized(j.player) {
		cr.Reason = j.player + " 的执行权限已经被服主收回"
		note("", errors.New(cr.Reason))
		return "没有执行：" + cr.Reason + "。", true, cr
	}
	run, err := CheckCommand(a.Command, j.player, s.b.Online(r.name), cur.enabledTemplates())
	if err != nil {
		cr.Reason = err.Error()
		note("", err)
		return "没有执行：" + cr.Reason + "。请用中文把原因告诉玩家。", true, cr
	}
	*ran++
	cr.Run = run
	buf := []string{}
	r.mu.Lock()
	r.capture = &buf
	r.mu.Unlock()
	err = s.b.Send(r.name, run, "[MineDeck] 游戏内 AI 替 "+j.player+" 执行：/"+run)
	if err == nil {
		select {
		case <-time.After(s.cmdWait):
		case <-ctx.Done():
		}
	}
	r.mu.Lock()
	lines := *r.capture
	r.capture = nil
	r.mu.Unlock()
	note(run, err)
	if err != nil {
		cr.Reason = "写不进服务器：" + err.Error()
		return "没有执行：" + cr.Reason, true, cr
	}
	cr.OK = true
	cr.Output = r.redactText(outputOf(lines))
	out := cr.Output
	if out == "" {
		out = "（1 秒内服务器没有输出）"
	}
	return "已执行：" + run + "\n服务器的输出：\n" + wrapData(out), false, cr
}

// outputOf 执行命令后的控制台输出：去掉日志前缀；聊天行不要（不把其他玩家的聊天交给 AI，§6.4），MineDeck 自己的行也不要。
func outputOf(lines []string) string {
	var b strings.Builder
	for _, raw := range lines {
		l := mclog.Parse(raw)
		if l.Kind == mclog.Chat || l.Kind == mclog.MineDeck {
			continue
		}
		t := l.Body
		if l.Format == mclog.FormatNone {
			t = l.Text
		}
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if b.Len()+len(t)+1 > maxOutput {
			b.WriteString("……")
			break
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t)
	}
	return b.String()
}
