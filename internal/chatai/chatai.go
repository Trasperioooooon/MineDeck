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
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
	CmdWait time.Duration // 执行命令后收集多久的控制台输出，默认 1 秒（§6.6 第 6 步）
	Timeout time.Duration // 一个问题最多处理多久，默认 2 分钟
	// Companions AI 伙伴（第七阶段）；nil 时没有伙伴：不认「@伙伴名」，也没有伙伴工具。
	Companions Companions
}

// Service 游戏内聊天 AI。
type Service struct {
	b       Backend
	ai      *ai.Service
	reg     *ops.Registry
	redact  Redactor
	comps   Companions
	now     func() time.Time
	cmdWait time.Duration
	timeout time.Duration
	ctx     context.Context
	stop    context.CancelFunc

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
		o.CmdWait = time.Second
	}
	if o.Timeout <= 0 {
		o.Timeout = 2 * time.Minute
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Service{b: o.Backend, ai: o.AI, reg: o.Ops, redact: o.Redact, comps: o.Companions, now: o.Now, cmdWait: o.CmdWait, timeout: o.Timeout,
		ctx: ctx, stop: stop, rooms: map[string]*room{}}
}

// Close 停掉正在进行的 AI 请求（测试和退出时用）。
func (s *Service) Close() { s.stop() }

// Wait 等所有触发都处理完（测试用）。
func (s *Service) Wait() { s.busy.Wait() }

// 限制（§6.7）。
const (
	queueSize    = 3                // 同一实例同一时间只处理 1 个问题，最多排队 3 个
	spamWindow   = time.Minute      // 同一个人 1 分钟内……
	spamMax      = 5                // ……触发超过 5 次
	spamSilence  = 10 * time.Minute // 静默 10 分钟
	maxRounds    = 4                // 一个问题最多请求 AI 几次（工具调用之后还要再问一次）
	maxToolCalls = 6                // 一个问题最多调用几次工具
	maxCommands  = 3                // 一个问题最多执行几条命令
	maxOutput    = 2000             // 命令执行后交给 AI 的输出最多多少字节
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
	seen     map[string]time.Time // 最近出现过的名字（没开游戏内 AI 时也在内存里记，不写盘）
	capture  *[]string            // 执行命令后在收集控制台输出
	lastAsk  map[string]time.Time // 冷却：上次被受理的提问
	hits     map[string][]time.Time
	silenced map[string]time.Time
	noticed  bool      // 发过隐私提示了……
	notice   time.Time // ……是哪次开服发的（记开服时间：重新开服后再发一次）
	inbox    chan trigger
	jobs     chan *job
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
		r = &room{s: s, name: name, seen: map[string]time.Time{}, lastAsk: map[string]time.Time{}, hits: map[string][]time.Time{}, silenced: map[string]time.Time{}}
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
	if r.capture != nil && len(*r.capture) < 50 {
		*r.capture = append(*r.capture, line)
	}
	set, rules := r.set, r.rules
	r.mu.Unlock()
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

// spam 防刷屏（§6.7）：同一个人 1 分钟内触发超过 5 次，静默 10 分钟，静默期间什么都不回。
// 返回 drop 表示这一次不处理；started 表示这一次刚开始静默（记一条给服主看）。
func (r *room) spam(player string, now time.Time) (drop, started bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := strings.ToLower(player)
	if until, ok := r.silenced[k]; ok {
		if now.Before(until) {
			return true, false
		}
		delete(r.silenced, k)
		delete(r.hits, k)
	}
	var keep []time.Time
	for _, t := range r.hits[k] {
		if now.Sub(t) < spamWindow {
			keep = append(keep, t)
		}
	}
	keep = append(keep, now)
	r.hits[k] = keep
	if len(keep) > spamMax {
		r.silenced[k] = now.Add(spamSilence)
		delete(r.hits, k)
		return true, true
	}
	return false, false
}

// handle 处理一次触发：不在线的丢掉；防刷屏；不调用 AI 的（帮助、额度、未知选项、选项放错位置）直接私下回复；
// 提问的检查冷却、次数和额度，受理了就排进队列。系统提示一律只发给提问的人（§6.7）。
func (r *room) handle(t trigger) {
	s := r.s
	if !containsName(s.b.Online(r.name), t.player) {
		return // 名字必须是当前在线的玩家（§6.1 第 4 步）
	}
	r.mu.Lock()
	see(r.seen, t.player, t.t)
	r.mu.Unlock()
	if drop, started := r.spam(t.player, t.t); drop {
		if started {
			r.record(Record{T: t.t, Player: t.player, Kind: "silenced", Opts: t.ask.Opts, Question: t.ask.Question,
				Note: "1 分钟内触发超过 5 次，接下来 10 分钟不回复这个人"})
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
	case ActHelp:
		sys(string(ActHelp), r.helpText(set, authorized, t.companion))
		return
	case ActQuota:
		sys(string(ActQuota), r.quotaText(set, t.player, t.t, feat))
		return
	case ActUnknown, ActNoSpace, ActMisplaced:
		sys(string(t.ask.Action), hintText(set.Prefix, t.ask, authorized))
		return
	}
	// 提问
	if !s.ai.Enabled() {
		sys("disabled", "主机的 MineDeck 还没配置 AI，现在问不了问题。")
		return
	}
	if msg := r.limit(set, t.player, t.t); msg != "" {
		kind := "limit"
		if strings.HasPrefix(msg, "冷却") {
			kind = "cooldown"
		}
		sys(kind, msg)
		return
	}
	if err := s.ai.Begin(feat); err != nil { // 「设置 → AI」里游戏内 AI（叫着伙伴说的是 AI 伙伴）的每日额度（所有实例共用）
		sys("limit", quotaMsg(feat))
		return
	}
	if len(r.jobs) >= queueSize {
		sys("busy", "AI 正忙，请稍后再问。")
		return
	}
	r.notifyPrivacy() // 先说清楚提问会发给谁，再去问 AI
	r.accept(t.player, t.t)
	s.busy.Add(1)
	r.jobs <- &job{player: t.player, ask: t.ask, authorized: authorized, exec: authorized && !t.ask.NoExec, companion: t.companion, feat: feat, t: t.t}
}

// limit 冷却、每人每天、全服每天的次数、全服每天的 token；有一项不满足就返回给玩家的提示。
func (r *room) limit(set Settings, player string, now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &r.st.Counters
	c.roll(now)
	k := strings.ToLower(player)
	if last, ok := r.lastAsk[k]; ok && set.Cooldown > 0 {
		if left := time.Duration(set.Cooldown)*time.Second - now.Sub(last); left > 0 {
			return "冷却中，" + secs(left) + " 秒后再问。"
		}
	}
	switch {
	case set.PerPlayer > 0 && c.Players[k] >= set.PerPlayer:
		return "你今天的 " + itoa(set.PerPlayer) + " 次已经用完了，明天再来吧。"
	case set.PerServer > 0 && c.Count >= set.PerServer:
		return "全服今天的 " + itoa(set.PerServer) + " 次已经用完了，明天再来吧。"
	case set.Budget > 0 && c.Tokens >= set.Budget:
		return "全服今天的 AI 额度用完了，明天再来吧。"
	}
	return ""
}

// accept 受理了一个提问：算次数、开始冷却（不调用 AI 的回复不算次数、不触发冷却，§6.7）。
func (r *room) accept(player string, now time.Time) {
	r.mu.Lock()
	c := &r.st.Counters
	c.roll(now)
	k := strings.ToLower(player)
	c.Players[k]++
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
	r.broadcast("提示：向 AI 提的问题会发给" + providerName(r.s.ai) + "处理。只想自己看到回答，在问题前加 /私。")
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
func (r *room) sayAs(speaker, player string, private bool, text, color string) (string, bool) {
	msgs, cut := Pack(text, MaxMsgRunes, MaxMsgs)
	target, head, who := "@a", "["+speaker+"→"+player+"] ", "全服可见"
	if private {
		target, head, who = player, "["+speaker+"] ", "只发给 "+player
	}
	for _, m := range msgs {
		_ = r.s.b.Send(r.name, Tellraw(target, head, m, color), "[MineDeck] 游戏内 AI 回复（"+who+"）："+head+m)
	}
	return strings.Join(msgs, " "), cut
}

// broadcast 发给所有人的系统提示（只有隐私提示用）。
func (r *room) broadcast(text string) {
	msgs, _ := Pack(text, MaxMsgRunes, MaxMsgs)
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
