// Package chatai 游戏内聊天 AI（设计文档 §6）：玩家在聊天框里用「@AI 问题」提问，MineDeck 用 tellraw 回复，不用装插件。
//
// 一句聊天的处理：服务器的每行输出经 OnLine 交进来 → 用 mclog 认出聊天的发送者和正文（§6.1）→ 认触发前缀、解析提问选项
// （§6.2：/私、/问、/额度、/帮助）→ 防刷屏、冷却、每人每天和全服每天的次数、全服每天的 token（§6.7）→ 调 AI（每个问题
// 一次新的对话；有执行权限、又没带 /问 时才多给一个 run_command 工具，§6.4）→ 用 tellraw 回复（默认全服可见，§6.3）。
// AI 要执行的命令按命令模板的白名单和写死的禁止清单校验（§6.6），通过了才写进服务器的标准输入。
// 问答和执行过的命令都写进操作记录（来源 gamechat），最近的问答另外存在实例目录里，界面上能看完整的回答。
//
// 权限只在代码里判断，不靠提示词：没有执行权限的人提问时，run_command 根本不在请求里；读服务器信息的 game_info 经操作层调用
// （只放行 InGame 的读操作）。这个包不引用 Windows API，Linux 上能直接测试；实例那一侧（控制台、标准输入、玩家名单）由 Backend 注入。
package chatai

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"minedeck/internal/ai"
	"minedeck/internal/mclog"
	"minedeck/internal/ops"
)

// Backend 实例那一侧：ops/catalog 用 inst.Manager 实现，测试里是假的。
type Backend interface {
	// Dir 实例目录（设置和状态存在这里）。实例不存在时报错。
	Dir(inst string) (string, error)
	// Info 实例的基本情况。
	Info(inst string) (Info, error)
	// Online 当前在线的玩家（由锚定过的进服、退服规则维护，§6.1 第 4 步）。
	Online(inst string) []string
	// Players 在线玩家和三份名单（界面上列可以授权的玩家用）。
	Players(inst string) (Players, error)
	// Send 往服务器的标准输入写一行命令；echo 是写进 MineDeck 控制台的那一行（给人看的）。
	Send(inst, cmd, echo string) error
}

// Info 实例的基本情况。
type Info struct {
	Core    string
	MC      string
	Status  string    // running / starting / stopping / stopped
	Started time.Time // 这次开服的时间：隐私提示每次开服只发一次
	Proxy   bool      // 群组代理端（没有游戏聊天）
}

// Players 在线玩家和名单。
type Players struct {
	Online      []string
	Whitelist   []string
	Ops         []string
	WhitelistOn bool // server.properties 的 white-list 是 true
}

// Redactor 发给 AI 之前的脱敏（ops/catalog 里和应用内助手共用同一个 assistant.Redactor）。
type Redactor interface{ Text(string) string }

// Options Service 的依赖。
type Options struct {
	Backend Backend
	AI      *ai.Service
	Ops     *ops.Registry // game_info 经它调用；问答和命令也记进它的操作记录
	Redact  Redactor      // nil 时不脱敏
	Now     func() time.Time
	// 执行命令后等控制台的输出（§6.6 第 6 步）：收到输出就返回，一直没有，普通命令最多等 CmdWait（默认 2 秒），
	// locate 一类慢命令最多等 SlowCmdWait（默认 5 秒：1.20.1 加一堆模组的世界里 locate 要好几秒，测试报告观察第 18 条）。
	CmdWait     time.Duration
	SlowCmdWait time.Duration
	Timeout     time.Duration // 一个问题最多处理多久，默认 2 分钟
	// Companions AI 伙伴（第七阶段）；nil 时没有伙伴：不认「@伙伴名」，也没有伙伴工具。
	Companions Companions
}

// Service 游戏内聊天 AI。
type Service struct {
	b        Backend
	ai       *ai.Service
	reg      *ops.Registry
	redact   Redactor
	comps    Companions
	now      func() time.Time
	cmdWait  time.Duration
	slowWait time.Duration
	settle   time.Duration // 收到命令的第一行输出后，再等这么久没有新的就返回（测试里改短）
	timeout  time.Duration
	ctx      context.Context
	stop     context.CancelFunc

	mu    sync.Mutex
	rooms map[string]*room
	busy  sync.WaitGroup // 还在处理的触发（测试里等它们处理完）
}

// New 不起任何后台任务：有人触发了才为那个实例起两个处理的协程。
func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.CmdWait <= 0 {
		o.CmdWait = 2 * time.Second
	}
	if o.SlowCmdWait <= 0 {
		o.SlowCmdWait = 5 * time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 2 * time.Minute
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Service{b: o.Backend, ai: o.AI, reg: o.Ops, redact: o.Redact, comps: o.Companions, now: o.Now, cmdWait: o.CmdWait, slowWait: o.SlowCmdWait,
		settle: cmdSettle, timeout: o.Timeout, ctx: ctx, stop: stop, rooms: map[string]*room{}}
}

// Close 停掉正在进行的 AI 请求（测试和退出时用）。
func (s *Service) Close() { s.stop() }

// Wait 等所有触发都处理完（测试用）。
func (s *Service) Wait() { s.busy.Wait() }

// 限制（§6.7；防刷屏按 v2.9.0-beta.2 测试报告 F-12 的提案改过）。
const (
	queueSize    = 3                      // 同一实例同一时间只处理 1 个问题，最多排队 3 个
	spamWindow   = time.Minute            // 同一个人 1 分钟内……
	spamMax      = 5                      // ……超过 5 次（受理了的提问和帮助这类回复；被冷却、次数挡回的不算）
	spamSilence  = 3 * time.Minute        // 静默 3 分钟
	silenceNag   = time.Minute            // 静默期间再问，隔这么久才再私下提醒一次还要等多久
	refuseGap    = 30 * time.Second       // 次数用完、正忙这类拒绝，同一个人隔这么久才再说一次（冷却是每个冷却期说一次）
	cmdSettle    = 300 * time.Millisecond // 收到命令的第一行输出后，再等这么久没有新的就返回
	maxRounds    = 4                      // 一个问题最多请求 AI 几次（工具调用之后还要再问一次）
	maxToolCalls = 6                      // 一个问题最多调用几次工具
	maxCommands  = 3                      // 一个问题最多执行几条命令
	maxOutput    = 2000                   // 命令执行后交给 AI 的输出最多多少字节
)

// room 一个实例的游戏内 AI。
type room struct {
	s    *Service
	name string

	mu       sync.Mutex
	fileMu   sync.Mutex // 写状态文件的先后
	dir      string     // 设置是从哪个目录读的：实例删了又建了同名的，目录变了就重读
	set      Settings
	rules    Rules
	st       stateFileV
	seen     map[string]time.Time   // 最近出现过的名字（没开游戏内 AI 时也在内存里记，不写盘）
	capture  *capture               // 执行命令后在收集控制台输出
	lastAsk  map[string]time.Time   // 冷却：上次被受理的提问（问 AI 和叫伙伴共用）
	hits     map[string][]time.Time // 防刷屏：最近 1 分钟里算数的触发
	silenced map[string]*muted      // 正在静默的人
	refused  map[string]time.Time   // 名字小写 + 拒绝的种类 → 上次说的时候（冷却记的是说的那个冷却期，见 refuseOnce）
	noticed  bool                   // 发过隐私提示了……
	notice   time.Time              // ……是哪次开服发的（记开服时间：重新开服后再发一次）
	inbox    chan trigger
	jobs     chan *job
}

// muted 一个人的静默：到什么时候，上次私下提醒是什么时候。
type muted struct{ until, reminded time.Time }

// capture 执行命令后收集的控制台输出；got 在收到像是命令输出的一行时通知一下（容量 1）。
type capture struct {
	lines []string
	got   chan struct{}
	reply func(string) bool // 哪一行算「命令回了输出」：replyLine，locate 一类用 slowReplyLine
}

type trigger struct {
	player    string
	ask       *Ask
	companion string // 叫着这个伙伴的名字说的（「@小方 跟着我」）
	t         time.Time
}

type job struct {
	player     string
	ask        *Ask
	authorized bool
	exec       bool       // 这次给执行命令（和指挥伙伴）的工具
	companion  string     // 叫着这个伙伴的名字说的：以它的名义回答，记在 AI 伙伴的额度里
	feat       ai.Feature // 记在哪项功能名下
	t          time.Time
}

// room 取（没有就建）实例的游戏内 AI；实例目录变了（删了又建了同名的）就重新读设置。
func (s *Service) room(name string) (*room, error) {
	dir, err := s.b.Dir(name)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	r := s.rooms[name]
	if r == nil {
		r = &room{s: s, name: name, seen: map[string]time.Time{}, lastAsk: map[string]time.Time{}, hits: map[string][]time.Time{}, silenced: map[string]*muted{},
			refused: map[string]time.Time{}}
		s.rooms[name] = r
	}
	s.mu.Unlock()
	r.mu.Lock()
	if r.dir != dir {
		r.dir = dir
		r.set = loadSettings(dir)
		r.rules = r.set.Rules()
		r.st = loadState(dir)
	}
	r.mu.Unlock()
	return r, nil
}

// OnLine 服务器的一行输出（在读输出的协程里同步调用，所以只做很快的事：识别、记名字、交给处理的协程）。
func (s *Service) OnLine(inst, line string) {
	r, err := s.room(inst)
	if err != nil {
		return
	}
	r.mu.Lock()
	cp := r.capture
	if cp != nil && len(cp.lines) < 50 {
		cp.lines = append(cp.lines, line)
	}
	set, rules := r.set, r.rules
	r.mu.Unlock()
	if cp != nil && cp.reply(line) {
		select {
		case cp.got <- struct{}{}:
		default:
		}
	}
	l := mclog.ParseWith(line, mclog.ChatOptions{LooseNames: rules.LooseNames, AnyThread: rules.AnyThread})
	if l.Kind == mclog.Join {
		r.mu.Lock()
		see(r.seen, l.Player, s.now())
		r.mu.Unlock()
	}
	if !set.Enabled || (l.Kind != mclog.Chat && rules.Pattern == nil) {
		return
	}
	base := rules
	list, _, _ := s.companionsLive(inst) // 伙伴的名字也能触发：「@小方 跟着我」
	if len(list) > 0 {
		rules.Prefixes = append(slices.Clone(rules.Prefixes), companionPrefixes(list)...)
	}
	h := recognizeParsed(l, rules)
	if !h.Trigger() {
		return
	}
	r.start()
	s.busy.Add(1)
	select {
	case r.inbox <- trigger{player: h.Player, ask: h.Ask, companion: addressed(h.Ask.Prefix, base, list), t: s.now()}:
	default: // 收件箱满了（有人在刷屏）：丢掉
		s.busy.Done()
	}
}

// start 第一次有人触发时起两个协程：一个按顺序处理触发（限流、系统提示），一个按顺序回答问题。
func (r *room) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inbox != nil {
		return
	}
	r.inbox = make(chan trigger, 64)
	r.jobs = make(chan *job, queueSize)
	go func() {
		for t := range r.inbox {
			r.handle(t)
			r.s.busy.Done()
		}
	}()
	go func() {
		for j := range r.jobs {
			r.answer(j)
			r.s.busy.Done()
		}
	}()
}

func (r *room) settings() Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.set
}

// silent 这个人是不是在静默中：返回还剩多久，以及这次要不要再私下提醒（离上次提醒满 silenceNag）。静默过了就清掉。
func (r *room) silent(player string, now time.Time) (left time.Duration, remind bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := strings.ToLower(player)
	m := r.silenced[k]
	if m == nil {
		return 0, false
	}
	if !now.Before(m.until) {
		delete(r.silenced, k)
		delete(r.hits, k)
		return 0, false
	}
	if now.Sub(m.reminded) >= silenceNag {
		m.reminded = now
		return m.until.Sub(now), true
	}
	return m.until.Sub(now), false
}

// spam 防刷屏（§6.7）：记一次算数的触发（受理了的提问、帮助这类不调用 AI 的回复），1 分钟内超过 5 次返回 true（该静默了）。
// 被冷却、次数、正忙挡回的提问不算：原来连这些也算，越急着问越快被静默，玩家推不出这个因果（测试报告 F-12）。
func (r *room) spam(player string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := strings.ToLower(player)
	var keep []time.Time
	for _, t := range r.hits[k] {
		if now.Sub(t) < spamWindow {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now)
	if len(keep) > spamMax {
		delete(r.hits, k)
		return true
	}
	r.hits[k] = keep
	return false
}

// silence 开始静默：私下说一句还要等多久（静默期间再问，每分钟最多再提醒一次），记一条最近的问答和一条操作记录。
func (r *room) silence(t trigger) {
	r.mu.Lock()
	k := strings.ToLower(t.player)
	r.silenced[k] = &muted{until: t.t.Add(spamSilence), reminded: t.t}
	delete(r.hits, k)
	r.mu.Unlock()
	sent, _ := r.say(t.player, true, silenceText(spamSilence), colorSystem)
	note := fmt.Sprintf("1 分钟内超过 %d 次（被冷却、次数挡回的不算），接下来 %d 分钟不回复这个人；期间再问，每分钟最多私下提醒一次还要等多久",
		spamMax, int(spamSilence/time.Minute))
	r.record(Record{T: t.t, Player: t.player, Kind: "silenced", Opts: t.ask.Opts, Question: t.ask.Question, Reply: sent, Private: true, Note: note})
	r.logLimit(t, "silenced", note)
}

// refuseOnce 这次拒绝要不要说出来：冷却每个冷却期只说一次（玩家刚被告知还要等几秒）；次数用完、正忙这类，同一种隔 refuseGap 才再说。
// 不说的那些也不记进最近的问答：被挡回的不算进防刷屏，有人一直刷时，靠这个不刷屏、不把正常的记录挤掉、不反复写盘。
func (r *room) refuseOnce(player, kind string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := strings.ToLower(player)
	key := k + "\x00" + kind
	last, told := r.refused[key]
	if kind == "cooldown" {
		ask := r.lastAsk[k]
		if told && last.Equal(ask) {
			return false
		}
		r.refused[key] = ask
		return true
	}
	if told && now.Sub(last) < refuseGap {
		return false
	}
	r.refused[key] = now
	return true
}

// logLimit 限流挡下的提问记一条操作记录 gameai.limit（来源 gamechat，记为失败，err 是原因）。只记静默（每次开始一条）、
// 冷却和次数用完（跟着私下的提示走：冷却每个冷却期最多一条，次数用完隔 30 秒最多一条），刷屏的人刷不爆操作记录。
func (r *room) logLimit(t trigger, kind, why string) {
	if r.s.reg == nil {
		return
	}
	args := map[string]any{"inst": r.name, "result": kind, "chars": utf8.RuneCountInString(t.ask.Question), "opts": optLabels(t.ask.Opts)}
	if kind == "silenced" {
		args["minutes"] = int(spamSilence / time.Minute)
	}
	if t.companion != "" {
		args["companion"] = t.companion
	}
	r.s.reg.Record(ops.Caller{Source: ops.SrcGameChat, Actor: t.player}, "gameai.limit", ops.ArgsOf(args), errors.New(why), 0)
}

// handle 处理一次触发：不在线的丢掉；静默中的不处理（隔一阵私下提醒一次）；不调用 AI 的（帮助、额度、未知选项、选项放错位置）
// 直接私下回复；提问的检查冷却、次数和额度，受理了就排进队列。系统提示一律只发给提问的人（§6.7）。
// 防刷屏只数受理了的提问和不调用 AI 的回复；第 6 次不回答，改成私下说「你问得太快了」并开始静默。
func (r *room) handle(t trigger) {
	s := r.s
	if !containsName(s.b.Online(r.name), t.player) {
		return // 名字必须是当前在线的玩家（§6.1 第 4 步）
	}
	r.mu.Lock()
	see(r.seen, t.player, t.t)
	r.mu.Unlock()
	if left, remind := r.silent(t.player, t.t); left > 0 {
		if remind {
			r.say(t.player, true, silenceText(left), colorSystem)
		}
		return
	}
	set := r.settings()
	authorized := set.Authorized(t.player)
	sys := func(kind, text string) {
		sent, _ := r.say(t.player, true, text, colorSystem)
		r.record(Record{T: t.t, Player: t.player, Kind: kind, Opts: t.ask.Opts, Question: t.ask.Question, Reply: sent, Private: true})
	}
	feat := featureOf(t.companion)
	switch t.ask.Action {
	case ActHelp, ActQuota, ActUnknown, ActNoSpace, ActMisplaced:
		if r.spam(t.player, t.t) {
			r.silence(t)
			return
		}
		switch t.ask.Action {
		case ActHelp:
			sys(string(ActHelp), r.helpText(set, authorized, t.companion))
		case ActQuota:
			sys(string(ActQuota), r.quotaText(set, t.player, t.t, feat))
		default:
			sys(string(t.ask.Action), hintText(set.Prefix, t.ask, authorized))
		}
		return
	}
	// 提问：被挡回的不算进防刷屏，同样的拒绝也不反复说（refuseOnce）
	refuse := func(kind, text string) {
		if !r.refuseOnce(t.player, kind, t.t) {
			return
		}
		sys(kind, text)
		if kind == "cooldown" || kind == "limit" {
			r.logLimit(t, kind, text)
		}
	}
	if !s.ai.Enabled() {
		refuse("disabled", "主机的 MineDeck 还没配置 AI，现在问不了问题。")
		return
	}
	comp := t.companion != ""
	if kind, msg := r.limit(set, t.player, t.t, comp); msg != "" {
		refuse(kind, msg)
		return
	}
	if err := s.ai.Begin(feat); err != nil { // 「设置 → AI」里游戏内 AI（叫着伙伴说的是 AI 伙伴）的每日额度（所有实例共用）
		refuse("limit", quotaMsg(feat))
		return
	}
	if len(r.jobs) >= queueSize {
		refuse("busy", "AI 正忙，请稍后再问。")
		return
	}
	if r.spam(t.player, t.t) {
		r.silence(t)
		return
	}
	r.notifyPrivacy() // 先说清楚提问会发给谁，再去问 AI
	r.accept(t.player, t.t, comp)
	s.busy.Add(1)
	r.jobs <- &job{player: t.player, ask: t.ask, authorized: authorized, exec: authorized && !t.ask.NoExec, companion: t.companion, feat: feat, t: t.t}
}

// limit 冷却、每人每天、全服每天的次数、全服每天的 token；有一项不满足就返回种类（cooldown / limit）和给玩家的提示。
// comp 为 true 是叫伙伴（「@伙伴名」）：每人每天的次数单独算，上限和问 AI 一样；冷却、全服的次数和 token 两种共用。
func (r *room) limit(set Settings, player string, now time.Time, comp bool) (kind, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &r.st.Counters
	c.roll(now)
	k := strings.ToLower(player)
	if last, ok := r.lastAsk[k]; ok && set.Cooldown > 0 {
		if left := time.Duration(set.Cooldown)*time.Second - now.Sub(last); left > 0 {
			return "cooldown", "冷却中，" + secs(left) + " 秒后再问。"
		}
	}
	switch {
	case set.PerPlayer > 0 && c.used(k, comp) >= set.PerPlayer && comp:
		return "limit", "你今天叫伙伴的 " + itoa(set.PerPlayer) + " 次已经用完了，明天再来吧。"
	case set.PerPlayer > 0 && c.used(k, comp) >= set.PerPlayer:
		return "limit", "你今天的 " + itoa(set.PerPlayer) + " 次已经用完了，明天再来吧。"
	case set.PerServer > 0 && c.Count >= set.PerServer:
		return "limit", "全服今天的 " + itoa(set.PerServer) + " 次已经用完了，明天再来吧。"
	case set.Budget > 0 && c.Tokens >= set.Budget:
		return "limit", "全服今天的 AI 额度用完了，明天再来吧。"
	}
	return "", ""
}

// accept 受理了一个提问：算次数（叫伙伴的记在伙伴那一栏）、开始冷却（不调用 AI 的回复不算次数、不触发冷却，§6.7）。
func (r *room) accept(player string, now time.Time, comp bool) {
	r.mu.Lock()
	c := &r.st.Counters
	c.roll(now)
	k := strings.ToLower(player)
	if comp {
		c.Companion[k]++
	} else {
		c.Players[k]++
	}
	c.Count++
	r.lastAsk[k] = now
	r.mu.Unlock()
	r.persist()
}

// notifyPrivacy 这次开服第一次有人提问时，告诉大家一次：提问内容会发给哪家服务商（§10.1）。
func (r *room) notifyPrivacy() {
	info, err := r.s.b.Info(r.name)
	if err != nil {
		return
	}
	r.mu.Lock()
	if r.noticed && r.notice.Equal(info.Started) {
		r.mu.Unlock()
		return
	}
	r.noticed, r.notice = true, info.Started
	r.mu.Unlock()
	r.broadcast("提示：向 AI 提的问题会发给" + providerName(r.s.ai) + "处理。\n只想自己看到回答，在问题前加 /私。")
}

// replyLine 像是命令执行后服务器回的一行（收到它就可以准备返回了）：不是聊天、不是 MineDeck 自己的行；有线程名的，
// 要是服务器主线程打的 INFO（控制台执行命令的反馈都是这样）。别的线程、模组打的日志照样收进输出，只是不算「收到了」。
func replyLine(raw string) bool {
	l := mclog.Parse(raw)
	// 进服退服也是主线程的 INFO，管道里排在命令前面的那一行会让 locate 这类慢命令提前收工（Codex 复审发现）
	if l.Kind == mclog.Chat || l.Kind == mclog.MineDeck || l.Kind == mclog.Join || l.Kind == mclog.Leave || strings.TrimSpace(l.Text) == "" {
		return false
	}
	if l.Format == mclog.FormatNone { // 没有前缀的行；异常栈的续行（缩进开头）不算
		return !strings.HasPrefix(raw, "\t") && !strings.HasPrefix(raw, " ")
	}
	return l.Level == "INFO" && (l.Thread == "" || l.Thread == "Server thread")
}

// slowReplyRe locate 一类命令自己的反馈（服务器控制台固定是英文：原版语言文件 commands.locate.*、commands.locatebiome.*），
// 以及命令写错时的报错（「Unknown or incomplete command…」下一行是带 <--[HERE] 的原命令）。
var slowReplyRe = regexp.MustCompile(`^(The nearest |Could not find |There is no |Unknown or incomplete command|Incorrect argument for command|Unknown command)|<--\[HERE\]$`)

// slowReplyLine locate 一类要等好几秒的命令只认它自己的反馈：等的这几秒里成就、死亡消息、模组的日志都可能打在主线程上，
// 按 replyLine 算会提前收工，坐标还没出来就交给了 AI（独立评审发现）。没等到就等满 SlowCmdWait，期间的行照样收进输出。
func slowReplyLine(raw string) bool {
	if !replyLine(raw) {
		return false
	}
	l := mclog.Parse(raw)
	t := l.Body
	if l.Format == mclog.FormatNone {
		t = l.Text
	}
	return slowReplyRe.MatchString(strings.TrimSpace(t))
}

// record 记一条最近的问答（存进实例目录，只留最近 100 条）。
func (r *room) record(rec Record) {
	r.mu.Lock()
	r.st.History = append(r.st.History, rec)
	if len(r.st.History) > maxHistory {
		r.st.History = r.st.History[len(r.st.History)-maxHistory:]
	}
	r.mu.Unlock()
	r.persist()
}

// persist 写状态文件：在锁里编好，在锁外写盘（OnLine 在读服务器输出的协程里也要拿 r.mu，不能让它等磁盘）。
// 先拿 fileMu 再编：两处同时写时，后编的一定后写，盘上不会是旧的。没开游戏内 AI 时不写。
func (r *room) persist() {
	r.fileMu.Lock()
	defer r.fileMu.Unlock()
	r.mu.Lock()
	if !r.set.Enabled || r.dir == "" {
		r.mu.Unlock()
		return
	}
	for k, v := range r.seen {
		see(r.st.Seen, k, v)
	}
	dir := r.dir
	b, err := encodeJSON(r.st)
	r.mu.Unlock()
	if err == nil {
		_ = writeFile(filepath.Join(dir, stateFile), b)
	}
}

// say 回复一个人：回答默认全服可见（目标 @a，开头写 [AI→名字]），/私 和系统提示只发给提问的人（目标是名字，开头 [AI]）。
// 返回实际发出去的文字（拆成几条的用空格接起来）和是不是截断了。
func (r *room) say(player string, private bool, text, color string) (string, bool) {
	return r.sayAs("AI", player, private, text, color)
}

// sayAs 同 say，开头写成 speaker（叫着伙伴的名字问的，以伙伴的名义回答：[小方→Steve]）。
// 回答按 Pack 拆（每条最多 180 字、最多 3 条）；系统提示（灰色）按聊天框的宽度拆成一行一条（Fit），存进记录的是整段话。
func (r *room) sayAs(speaker, player string, private bool, text, color string) (string, bool) {
	target, head, who := "@a", "["+speaker+"→"+player+"] ", "全服可见"
	if private {
		target, head, who = player, "["+speaker+"] ", "只发给 "+player
	}
	var msgs []string
	cut, shown := false, ""
	if color == colorSystem {
		msgs, shown = Fit(text, head), flat(text)
	} else {
		msgs, cut = Pack(text, MaxMsgRunes, MaxMsgs)
		shown = strings.Join(msgs, " ")
	}
	for _, m := range msgs {
		_ = r.s.b.Send(r.name, Tellraw(target, head, m, color), "[MineDeck] 游戏内 AI 回复（"+who+"）："+head+m)
	}
	return shown, cut
}

// broadcast 发给所有人的系统提示（只有隐私提示用），同样一行一条。
func (r *room) broadcast(text string) {
	msgs := Fit(text, "[AI] ")
	for _, m := range msgs {
		_ = r.s.b.Send(r.name, Tellraw("@a", "[AI] ", m, colorSystem), "[MineDeck] 游戏内 AI 提示（全服可见）：[AI] "+m)
	}
}

func containsName(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }

// secs 剩下的秒数，向上取整。
func secs(d time.Duration) string { return itoa(int((d + time.Second - 1) / time.Second)) }
