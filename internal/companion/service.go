package companion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"minedeck/internal/bridge"
	companionjar "minedeck/internal/companion/jar"
	"minedeck/internal/events"
)

// Error 带状态码的错误（沿用 HTTP 的含义：400 参数或业务错误，404 找不到，409 冲突，502 Modrinth 或伙伴模组那边的问题）。
type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func errorf(code int, format string, a ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, a...)}
}

// InstInfo 伙伴要用到的实例信息。
type InstInfo struct {
	Name   string
	Dir    string
	Core   string // 核心：neoforge、forge……
	MC     string
	Build  string // 加载器的版本
	Status string // stopped | starting | running | stopping
	Online []string
}

// Instances 实例（inst.Manager 的一层薄包装，测试里换成假的）。
type Instances interface {
	Names() []string
	Info(name string) (InstInfo, error)
	// Log 往实例的控制台写一行（给人看的提示）。
	Log(name, line string)
}

// Origin 谁让伙伴干的活：任务结束时据此通知他。
type Origin struct {
	Player    string `json:"player"`
	Companion string `json:"companion,omitempty"` // 玩家是叫着这个伙伴的名字说的
}

// Finished 一个有人发起的任务做完了或失败了（给游戏内 AI 发模板通知，不再调用 AI）。
type Finished struct {
	Inst      string `json:"inst"`
	Origin    Origin `json:"origin"`
	Companion string `json:"companion"` // 伙伴的名字
	TaskID    int64  `json:"task"`
	Op        string `json:"op"`
	Result    string `json:"result"` // 已按兼容规则归一（不认识的当成 failed）
	Text      string `json:"text"`   // 按模板写好的一句话
}

// Deps Service 用到的东西。
type Deps struct {
	Instances  Instances
	ClientName string                 // HELLO 里的名字，如「MineDeck 2.7.0」
	HTTP       *http.Client           // 访问 Modrinth（只在用户点「安装」时）；nil 用 dl.Client
	API        string                 // Modrinth 的 API 地址；空串用 ModrinthAPI
	Manifest   *companionjar.Manifest // nil 用内置的清单
	// OnFinish 有人发起的任务做完了、没做够、失败了或超时了（被停下、被新任务顶掉的不算）。在单独的协程里按顺序调用。
	OnFinish func(Finished)
	// Subscribe 订阅事件（实例启停）；nil 用 events.Subscribe。
	Subscribe func(func(events.Event))
	// Logf 诊断日志；nil 不输出。
	Logf func(format string, args ...any)

	clientOpts func(*Options) // 测试用：改客户端的参数
	tick       time.Duration  // 测试用：等服务器开好时多久看一次状态（默认 1 秒）
}

// Service 所有实例的 AI 伙伴。
type Service struct {
	d Deps

	manOnce sync.Once
	man     *companionjar.Manifest
	manErr  error

	mu    sync.Mutex
	rooms map[string]*room

	notify chan func() // OnFinish 按顺序在这里调用
}

// room 一个实例：连接和这次开服期间的状态。
type room struct {
	name    string
	link    *link
	welcome *Welcome
	session string
	comps   map[int]Companion
	modCfg  *ModConfig
	events  []EventView
	origins map[int64]Origin // 任务编号 → 谁发起的（这次开服）
	early   map[int64]Event  // 回复还没到、先收到了 task_finished 的任务
	restart bool             // 服务器运行中装了模组：重启后生效
	settled time.Time        // MineDeck 最近一次改配置文件的时间
	down    string           // 最近一次断线的原因（连着时为空）
}

type link struct {
	c      *Client
	stop   chan struct{}
	done   chan struct{}
	reason string
}

const maxEvents = 100

var (
	allMu sync.Mutex
	all   []*Service
)

// New 建好 AI 伙伴的管理器，订阅实例启停。不联网，不起连接：实例开服、而且启用了伙伴时才去连。
func New(d Deps) *Service {
	if d.tick <= 0 {
		d.tick = time.Second
	}
	if d.ClientName == "" {
		d.ClientName = "MineDeck"
	}
	s := &Service{d: d, rooms: map[string]*room{}, notify: make(chan func(), 256)}
	go func() {
		for f := range s.notify {
			f()
		}
	}()
	sub := d.Subscribe
	if sub == nil {
		sub = events.Subscribe
	}
	sub(s.HandleEvent)
	allMu.Lock()
	all = append(all, s)
	allMu.Unlock()
	return s
}

// ShutdownAll 退出 MineDeck 前调用：给所有连着的伙伴模组发 END，断开连接（伙伴和它们的任务留在游戏里）。
func ShutdownAll(reason string) {
	allMu.Lock()
	list := append([]*Service(nil), all...)
	allMu.Unlock()
	for _, s := range list {
		s.Shutdown(reason)
	}
}

// Shutdown 断开这个 Service 的所有连接。
func (s *Service) Shutdown(reason string) {
	s.mu.Lock()
	var names []string
	for n, r := range s.rooms {
		if r.link != nil {
			names = append(names, n)
		}
	}
	s.mu.Unlock()
	for _, n := range names {
		s.drop(n, reason, false)
	}
}

func (s *Service) manifest() (*companionjar.Manifest, error) {
	s.manOnce.Do(func() {
		if s.d.Manifest != nil {
			s.man = s.d.Manifest
			return
		}
		s.man, s.manErr = companionjar.Load()
	})
	return s.man, s.manErr
}

func (s *Service) logf(format string, a ...any) {
	if s.d.Logf != nil {
		s.d.Logf(format, a...)
	}
}

// log 往实例的控制台写一行「[MineDeck] 伙伴：…」。
func (s *Service) log(name, format string, a ...any) {
	if s.d.Instances != nil {
		s.d.Instances.Log(name, "[MineDeck] 伙伴："+fmt.Sprintf(format, a...))
	}
}

func (s *Service) roomLocked(name string) *room {
	r := s.rooms[name]
	if r == nil {
		r = &room{name: name, comps: map[int]Companion{}, origins: map[int64]Origin{}, early: map[int64]Event{}}
		s.rooms[name] = r
	}
	return r
}

func (s *Service) markRestart(name string) {
	s.mu.Lock()
	s.roomLocked(name).restart = true
	s.mu.Unlock()
}

// HandleEvent 实例开服时，启用了伙伴就去连（等服务器开好、模组打开端口）；关服、崩溃时断开，清掉这次开服的状态。
func (s *Service) HandleEvent(e events.Event) {
	name, _ := e.Data["instance"].(string)
	if name == "" {
		return
	}
	switch e.Type {
	case "instance.start":
		s.mu.Lock()
		s.roomLocked(name).restart = false
		s.mu.Unlock()
		s.ensure(name)
	case "instance.stop", "instance.crash":
		s.drop(name, "服务器已停止", true)
	}
}

// BeforeStart 起服务器进程之前（inst 的回调）：启用了伙伴的，保证配置能用（端口、令牌），避开别的实例已经用了的端口。
func (s *Service) BeforeStart(name string) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return
	}
	cfg, err := ReadConfig(info.Dir)
	if err != nil || !cfg.Enabled {
		return
	}
	if _, changed, err := EnsureConfig(info.Dir, EnsureOptions{Avoid: s.portsInUse(name)}); err != nil {
		s.log(name, "写伙伴模组的配置失败：%v", err)
	} else if changed {
		s.mu.Lock()
		s.roomLocked(name).settled = time.Now()
		s.mu.Unlock()
	}
}

// portsInUse 选端口时要避开的：别的实例的伙伴模组已经用了的，以及所有实例（含自己）音乐桥接用的（它从 24470 起选，会碰上）。
func (s *Service) portsInUse(self string) func(int) bool {
	used := map[int]bool{}
	for _, n := range s.d.Instances.Names() {
		info, err := s.d.Instances.Info(n)
		if err != nil {
			continue
		}
		if c, err := bridge.ReadConfig(info.Dir); err == nil && c.Port > 0 {
			used[c.Port] = true
		}
		if n == self {
			continue
		}
		if c, err := ReadConfig(info.Dir); err == nil && c.Exists && c.Port > 0 {
			used[c.Port] = true
		}
	}
	return func(p int) bool { return used[p] }
}

// ensure 启用了伙伴的话，起这个实例的连接（已经有了就不动）：先等服务器开好（模组在 Done 之后才打开端口），再连。
func (s *Service) ensure(name string) {
	info, err := s.d.Instances.Info(name)
	if err != nil || info.Status == "stopped" {
		return
	}
	if cfg, err := ReadConfig(info.Dir); err != nil || !cfg.Enabled {
		return
	}
	s.mu.Lock()
	r := s.roomLocked(name)
	if r.link != nil {
		c := r.link.c
		s.mu.Unlock()
		if c != nil {
			c.Nudge()
		}
		return
	}
	l := &link{stop: make(chan struct{}), done: make(chan struct{})}
	r.link = l
	s.mu.Unlock()
	go s.runLink(name, info.Dir, l)
}

func (s *Service) runLink(name, dir string, l *link) {
	defer close(l.done)
	defer func() {
		s.mu.Lock()
		if r := s.rooms[name]; r != nil && r.link == l {
			r.link = nil
		}
		s.mu.Unlock()
	}()
	for { // 等服务器开好
		info, err := s.d.Instances.Info(name)
		if err != nil || info.Status == "stopped" {
			return
		}
		if info.Status == "running" {
			break
		}
		select {
		case <-l.stop:
			return
		case <-time.After(s.d.tick):
		}
	}
	s.mu.Lock()
	settle := s.roomLocked(name).settled
	s.mu.Unlock()
	if time.Since(settle) > defaultQuickWindow {
		settle = time.Time{} // 从现在算起：服务器刚开好
	}
	o := Options{Dir: dir, Name: s.d.ClientName, Settle: settle,
		OnWelcome: func(w Welcome) { s.onWelcome(name, w) },
		OnEvent:   func(e Event) { s.onEvent(name, e) },
		OnStatus:  func(st Status) { s.onStatus(name, st) },
		OnDown:    func(err error) { s.onDown(name, l, err) },
		Logf:      func(f string, a ...any) { s.logf("[%s] "+f, append([]any{name}, a...)...) },
	}
	if s.d.clientOpts != nil {
		s.d.clientOpts(&o)
	}
	c := Start(o)
	s.mu.Lock()
	l.c = c
	s.mu.Unlock()
	select {
	case <-l.stop:
		c.Stop(l.reason)
	case <-c.Done(): // 重试也没用的错误：留着这个连接的状态（界面上显示原因），等关服或者改设置
		if err := c.Err(); err != nil {
			s.log(name, "连不上伙伴模组：%v", err)
		}
		<-l.stop
	}
}

// dropLink 只断开 l 这一个连接（期间已经换成了新的连接就不动），不清这次开服的状态。
func (s *Service) dropLink(name string, l *link, reason string) {
	s.mu.Lock()
	r := s.rooms[name]
	if r == nil || l == nil || r.link != l {
		s.mu.Unlock()
		return
	}
	r.link = nil
	s.mu.Unlock()
	l.reason = reason
	close(l.stop)
	<-l.done
}

// drop 断开实例的连接（给模组发 END，带上原因）；cleared 为 true 时连这次开服的状态（伙伴、任务、谁发起的）一起清掉。
func (s *Service) drop(name, reason string, cleared bool) {
	s.mu.Lock()
	r := s.rooms[name]
	var l *link
	if r != nil {
		l, r.link = r.link, nil
		if cleared {
			r.welcome, r.session, r.modCfg, r.down = nil, "", nil, ""
			r.comps, r.origins, r.early = map[int]Companion{}, map[int64]Origin{}, map[int64]Event{}
		}
	}
	s.mu.Unlock()
	if l != nil {
		l.reason = reason
		close(l.stop)
		<-l.done
	}
}

// liveClient 实例连着的客户端；没连着返回 nil。
func (s *Service) liveClient(name string) (*Client, *room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[name]
	if r == nil || r.link == nil || r.link.c == nil || !r.link.c.Live() {
		return nil, r
	}
	return r.link.c, r
}

// ---------- 模组发来的东西（在读连接的协程里按顺序调用）----------

func (s *Service) onWelcome(name string, w Welcome) {
	s.mu.Lock()
	r := s.roomLocked(name)
	if r.session != "" && r.session != w.Session { // 服务器重启过：任务编号从 1 重新数
		r.origins, r.early = map[int64]Origin{}, map[int64]Event{}
	}
	wasDown := r.down != "" || r.welcome == nil
	r.welcome, r.session, r.down = &w, w.Session, ""
	cfg := w.Config
	r.modCfg = &cfg
	r.comps = map[int]Companion{}
	for _, c := range w.Companions {
		r.comps[c.ID] = c
	}
	if wasDown {
		r.addEvent(EventView{Event: "connected", Level: "info",
			Text: fmt.Sprintf("连上了伙伴模组 %s（PlayerEngine %s），现有 %d 个伙伴", w.Mod, w.PlayerEngine, len(w.Companions))})
	}
	// 断开期间的事件不补（README「连接」）：有人发起的任务要是在这期间结束了，从伙伴的 lastResult 里认出来，照样通知
	s.settleLocked(name, r, w.Companions, "（断开期间结束的）")
	s.mu.Unlock()
}

// settleLocked 伙伴对象的 lastResult 是有人发起、还没通知过的任务：照样通知（WELCOME 里是断开期间结束的；STATUS 里是
// 事件丢了的：模组的 WELCOME 读的是每 tick 末尾才刷新的快照，握手正好落在任务结束的那个 tick 里时，task_finished 发在
// 连接就绪之前被丢掉，WELCOME 里也还没有这次的 lastResult，要等下一个 STATUS 才看得到）。调用方持有 s.mu。
func (s *Service) settleLocked(name string, r *room, list []Companion, note string) {
	for _, c := range list {
		if c.LastResult == nil || c.LastResult.Task == nil {
			continue
		}
		o, ok := r.origins[c.LastResult.Task.ID]
		if !ok {
			continue
		}
		delete(r.origins, c.LastResult.Task.ID)
		e := resultEvent(c.ID, *c.LastResult)
		ev := EventView{Event: EvTaskFinished, Companion: c.ID, Name: c.Name, Level: "info", Text: "「" + c.Name + "」" + ResultText(e) + note}
		if k := KnownResult(e.Result); k == ResultFailed || k == ResultTimeout || k == ResultIncomplete {
			ev.Level = "warn"
		}
		r.addEvent(ev)
		s.finishLocked(name, c.Name, o, e)
	}
}

// resultEvent 把伙伴对象的 lastResult 还原成 task_finished 事件。
func resultEvent(companion int, r Result) Event {
	return Event{Event: EvTaskFinished, Companion: companion, Task: r.Task, Result: r.Result, Detail: r.Detail, Seconds: r.Seconds, Pos: r.Pos,
		Have: r.Have, Gained: r.Gained, Want: r.Want, Killed: r.Killed, Given: r.Given}
}

// onDown 连接断了。服务器正在关（或者已经关了）时这是正常的：模组关服时先发 END「服务器正在关闭」再关端口
// （README「连接」），这时不报「断开了、会自动重连」，也不再重连，等下次开服（instance.start）再连。
func (s *Service) onDown(name string, l *link, err error) {
	closing, why := false, ""
	if info, e := s.d.Instances.Info(name); e == nil && (info.Status == "stopping" || info.Status == "stopped") {
		closing, why = true, map[string]string{"stopping": "服务器正在关闭", "stopped": "服务器已经停了"}[info.Status]
	}
	// 模组自己说了「服务器正在关闭」：不管是谁让它停的（游戏里的 /stop、RCON……），实例状态这时可能还没跟上
	if end := (*EndError)(nil); !closing && errors.As(err, &end) && end.Reason == endServerStopping {
		closing, why = true, endServerStopping
	}
	s.mu.Lock()
	r := s.roomLocked(name)
	r.down = err.Error()
	var c *Client
	if l != nil {
		c = l.c
	}
	if closing {
		r.addEvent(EventView{Event: "disconnected", Level: "info", Text: why + "，和伙伴模组的连接断开了（伙伴留在存档里，下次开服自动接管）"})
	} else {
		r.addEvent(EventView{Event: "disconnected", Level: "warn", Text: "和伙伴模组的连接断开了：" + err.Error() + "（伙伴和任务还在游戏里，会自动重连）"})
	}
	s.mu.Unlock()
	if closing {
		if c != nil {
			c.halt(why) // 在客户端自己的协程里：只叫它别再重连，不等它退出
		}
		go s.dropLink(name, l, why)
	}
}

func (s *Service) onStatus(name string, st Status) {
	s.mu.Lock()
	r := s.roomLocked(name)
	next := map[int]Companion{}
	for _, c := range st.Companions {
		next[c.ID] = c
	}
	r.comps = next
	s.settleLocked(name, r, st.Companions, "")
	s.mu.Unlock()
}

func (s *Service) onEvent(name string, e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.roomLocked(name)
	cname := func(id int) string {
		if c, ok := r.comps[id]; ok && c.Name != "" {
			return c.Name
		}
		if e.Name != "" {
			return e.Name
		}
		if e.Info != nil && e.Info.Name != "" {
			return e.Info.Name
		}
		return fmt.Sprintf("%d 号伙伴", id)
	}
	ev := EventView{Event: e.Event, Companion: e.Companion, Level: "info"}
	switch e.Event {
	case EvSpawned, EvAdopted:
		if e.Info == nil {
			return
		}
		r.comps[e.Info.ID] = *e.Info
		ev.Companion, ev.Name = e.Info.ID, e.Info.Name
		ev.Text = "刷出了伙伴「" + e.Info.Name + "」"
		if e.Event == EvAdopted {
			ev.Text = "接管了存档里的伙伴「" + e.Info.Name + "」"
		}
		if e.Info.Owner != "" {
			ev.Text += "（主人 " + e.Info.Owner + "）"
		}
	case EvTaskStarted:
		if e.Task == nil {
			return
		}
		ev.Name = cname(e.Companion)
		if c, ok := r.comps[e.Companion]; ok {
			t := e.Task.Task
			c.Task, c.State = &t, StateBusy
			r.comps[e.Companion] = c
		}
		ev.Text = "「" + ev.Name + "」开始" + TaskTitle(&e.Task.Task)
	case EvTaskFinished:
		ev.Name = cname(e.Companion)
		res := e.AsResult()
		if c, ok := r.comps[e.Companion]; ok {
			if c.Task != nil && e.Task != nil && c.Task.ID == e.Task.ID {
				c.Task = nil
				c.State = StateIdle
			}
			c.LastResult = &res
			r.comps[e.Companion] = c
		}
		ev.Text = "「" + ev.Name + "」" + ResultText(e)
		if r := KnownResult(e.Result); r == ResultFailed || r == ResultTimeout || r == ResultIncomplete {
			ev.Level = "warn"
		}
		if e.Task != nil {
			if o, ok := r.origins[e.Task.ID]; ok {
				delete(r.origins, e.Task.ID)
				s.finishLocked(name, ev.Name, o, e)
			} else {
				r.early[e.Task.ID] = e
				if len(r.early) > 50 {
					for k := range r.early {
						delete(r.early, k)
						break
					}
				}
			}
		}
	case EvStuck:
		ev.Name, ev.Level = cname(e.Companion), "warn"
		why := map[string]string{"no_progress": "30 秒几乎没动", "nodig": "要拆或放禁挖区域里的方块，绕不过去"}[e.Reason]
		if why == "" {
			why = e.Reason // 不认识的 reason 照原样显示（兼容规则 4）
		}
		ev.Text = "「" + ev.Name + "」卡住了：" + why
		if e.Detail != "" {
			ev.Text += "（" + e.Detail + "）"
		}
	case EvDied:
		ev.Name, ev.Level = cname(e.Companion), "warn"
		ev.Text = "「" + ev.Name + "」死了"
		if e.Cause != "" {
			ev.Text += "：" + e.Cause
		}
		ev.Text += "，背包里的东西掉在了原地"
		if e.Pos != nil {
			ev.Text += "（" + posText(*e.Pos) + "）"
		}
	case EvRemoved:
		ev.Name = cname(e.Companion)
		delete(r.comps, e.Companion)
		why := map[string]string{"despawn": "MineDeck 移除的", "died": "死了", "discarded": "被命令或别的模组清掉了", "lost": "重启后存档里找不到了"}[e.Reason]
		if why == "" {
			why = e.Reason
		}
		ev.Text = "伙伴「" + ev.Name + "」没了（" + why + "）"
	case EvConfigApplied:
		if e.Config == nil {
			return
		}
		cfg := *e.Config
		r.modCfg = &cfg
		ev.Text = "伙伴模组读到了新的配置"
		if len(cfg.Problems) > 0 {
			ev.Level, ev.Text = "warn", ev.Text+"，有没生效的条目："+strings.Join(cfg.Problems, "；")
		}
	default: // 不认识的事件：忽略（兼容规则 1）
		return
	}
	r.addEvent(ev)
}

// finishLocked 有人发起的任务结束了：做完、没做够、失败、超时才通知（被停下、被新任务顶掉的不算）。调用方持有 s.mu。
func (s *Service) finishLocked(name, cname string, o Origin, e Event) {
	res := KnownResult(e.Result)
	if res == ResultCancelled || res == ResultSuperseded || s.d.OnFinish == nil || o.Player == "" {
		return
	}
	f := Finished{Inst: name, Origin: o, Companion: cname, TaskID: e.Task.ID, Op: e.Task.Op, Result: res, Text: ResultText(e)}
	cb := s.d.OnFinish
	select {
	case s.notify <- func() { cb(f) }:
	default: // 积压太多：丢掉这一条通知（不阻塞读连接的协程）
	}
}

func (r *room) addEvent(ev EventView) {
	if ev.T.IsZero() {
		ev.T = time.Now()
	}
	r.events = append(r.events, ev)
	if len(r.events) > maxEvents {
		r.events = r.events[len(r.events)-maxEvents:]
	}
}

// EventView 最近的一件事（界面上「最近的事件」），新的在前。
type EventView struct {
	T         time.Time `json:"t"`
	Event     string    `json:"event"` // 模组的事件名，另有 connected、disconnected
	Companion int       `json:"companion,omitempty"`
	Name      string    `json:"name,omitempty"`
	Level     string    `json:"level"` // info / warn
	Text      string    `json:"text"`
}

// ---------- 给游戏内 AI 用 ----------

// Live 实例的伙伴：连着伙伴模组时 ok 为 true；list 是现有的伙伴（按编号），ops 是模组支持的指令。
func (s *Service) Live(name string) (list []Companion, ops []string, ok bool) {
	c, r := s.liveClient(name)
	if c == nil || r == nil || r.welcome == nil {
		return nil, nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedComps(r.comps), slices.Clone(r.welcome.Ops), true
}

// Names 实例现有伙伴的名字（游戏内 AI 认「@名字」用）。没连着伙伴模组时为空。
func (s *Service) Names(name string) []string {
	list, _, ok := s.Live(name)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.Name)
	}
	return out
}

func sortedComps(m map[int]Companion) []Companion {
	out := make([]Companion, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// actionArgs 游戏内 AI 能用的动作和每个动作能带的参数（协议 v1 的指令表）。参数表以外的键一律去掉：
// 模型没法借参数塞进别的东西。spawn、despawn 不在这里：刷出、移除只能由人在界面上做（或者走提案）。
var actionArgs = map[string][]string{
	"status":  {"companion", "inventory"},
	"collect": {"companion", "item", "count", "timeout"},
	"follow":  {"companion", "player", "timeout"},
	"goto":    {"companion", "x", "y", "z", "timeout"},
	"attack":  {"companion", "mob", "count", "duration", "radius", "timeout"},
	"give":    {"companion", "item", "count", "player", "fetch", "timeout"},
	"stop":    {"companion", "all"},
	"say":     {"companion", "text", "to"},
}

// ActionOps 游戏内 AI 能让伙伴做的动作，按固定的顺序（工具清单的顺序不变，前缀缓存才能命中）。
var ActionOps = []string{"follow", "collect", "goto", "attack", "give", "stop", "say", "status"}

// IsTaskOp 这个动作是不是任务（开始后要等它结束）。
func IsTaskOp(op string) bool {
	switch op {
	case "collect", "follow", "goto", "attack", "give":
		return true
	}
	return false
}

// ActResult 一个动作的结果。
type ActResult struct {
	Reply     *Response      `json:"-"`
	OK        bool           `json:"ok"`
	Code      string         `json:"code,omitempty"`
	Error     string         `json:"error,omitempty"`
	Companion *Companion     `json:"companion,omitempty"` // 动作落在了哪个伙伴身上
	Task      *Task          `json:"task,omitempty"`
	Sent      map[string]any `json:"sent"` // 实际发给模组的参数
}

// Act 替游戏内 AI 让伙伴做一个动作（见 actionArgs）。companion 可以是编号或名字（不分大小写）；不写时：
// 玩家叫着某个伙伴说的就是它，只有一个伙伴就是那一个。任务开始后记下 origin，结束时通知他（Deps.OnFinish）。
func (s *Service) Act(ctx context.Context, name, op string, args map[string]any, o Origin) (*ActResult, error) {
	keys, ok := actionArgs[op]
	if !ok {
		return nil, fmt.Errorf("伙伴没有「%s」这个动作", op)
	}
	c, r := s.liveClient(name)
	if c == nil {
		return nil, errors.New("现在没连着伙伴模组（服务器没开、伙伴没启用，或者正在重连）")
	}
	s.mu.Lock()
	ops := []string{}
	if r.welcome != nil {
		ops = r.welcome.Ops
	}
	comps := sortedComps(r.comps)
	s.mu.Unlock()
	if !slices.Contains(ops, op) {
		return nil, fmt.Errorf("这个伙伴模组不支持「%s」（可能要更新模组）", op)
	}
	sent := map[string]any{}
	for _, k := range keys {
		if v, ok := args[k]; ok && v != nil {
			sent[k] = v
		}
	}
	if item, ok := sent["item"].(string); ok && op == "give" {
		sent["item"] = giveItem(item)
	}
	var target *Companion
	if all, _ := sent["all"].(bool); !all || op != "stop" {
		delete(sent, "all")
		t, err := resolve(comps, sent["companion"], o.Companion)
		if err != nil {
			return nil, err
		}
		target = t
		sent["companion"] = t.ID
	}
	resp, err := c.Request(ctx, op, sent)
	if err != nil {
		return nil, err
	}
	res := &ActResult{Reply: resp, OK: resp.OK, Code: resp.Code, Error: resp.Error, Companion: target, Sent: sent}
	if !resp.OK && res.Error == "" {
		res.Error = codeName(resp.Code)
	}
	if resp.OK && IsTaskOp(op) {
		var body struct {
			Task *Task `json:"task"`
		}
		if resp.Decode(&body) == nil && body.Task != nil {
			res.Task = body.Task
			s.mu.Lock()
			if e, done := r.early[body.Task.ID]; done { // 回复到之前任务就结束了
				delete(r.early, body.Task.ID)
				s.finishLocked(name, target.Name, o, e)
			} else {
				r.origins[body.Task.ID] = o
			}
			s.mu.Unlock()
		}
	}
	return res, nil
}

// giveItem give 的物品名：原版物品去掉 minecraft: 命名空间。模组 0.2.0 把 item 原样拼进 PlayerEngine 的 `give <玩家> <物品> <数量>`，
// PlayerEngine 1.4.0 的 GiveCommand 只认目录里的名字和 ItemHelper.stripItemName（原版物品是不带命名空间的 oak_log，模组物品是
// create:brass_ingot）：写成 minecraft:oak_log 时它找不到这个物品，什么都没给就结束了，模组还报 done（given 为 0，两半联调时实测）。
// collect（PlayerEngine 的 get）两种写法都认，不用改；模组物品照写命名空间。模组 0.2.1 起自己也去掉，这里留着给连着 0.2.0 的服。
func giveItem(item string) string {
	if rest, ok := strings.CutPrefix(strings.TrimSpace(item), "minecraft:"); ok && rest != "" {
		return rest
	}
	return item
}

// resolve 找出动作落在哪个伙伴身上。
func resolve(comps []Companion, v any, addressed string) (*Companion, error) {
	if len(comps) == 0 {
		return nil, errors.New("现在一个伙伴都没有：先在 MineDeck 的「伙伴」标签里刷出一个")
	}
	find := func(name string) *Companion {
		for i := range comps {
			if strings.EqualFold(comps[i].Name, strings.TrimSpace(name)) {
				return &comps[i]
			}
		}
		return nil
	}
	switch x := v.(type) {
	case nil:
	case float64, int, int64, json.Number:
		var id int64
		switch n := x.(type) {
		case float64:
			id = int64(n)
		case int:
			id = int64(n)
		case int64:
			id = n
		case json.Number:
			id, _ = n.Int64()
		}
		for i := range comps {
			if int64(comps[i].ID) == id {
				return &comps[i], nil
			}
		}
		return nil, fmt.Errorf("没有编号是 %d 的伙伴", id)
	case string:
		if strings.TrimSpace(x) != "" {
			if c := find(x); c != nil {
				return c, nil
			}
			return nil, fmt.Errorf("没有叫「%s」的伙伴，现有：%s", x, compNames(comps))
		}
	default:
		return nil, errors.New("companion 要写伙伴的名字或编号")
	}
	if addressed != "" {
		if c := find(addressed); c != nil {
			return c, nil
		}
	}
	if len(comps) == 1 {
		return &comps[0], nil
	}
	return nil, fmt.Errorf("现在有 %d 个伙伴（%s），要说清楚是哪一个", len(comps), compNames(comps))
}

func compNames(comps []Companion) string {
	var n []string
	for _, c := range comps {
		n = append(n, c.Name)
	}
	return strings.Join(n, "、")
}
