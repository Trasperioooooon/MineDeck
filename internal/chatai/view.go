package chatai

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"minedeck/internal/ai"
)

// View 实例「游戏内 AI」标签的内容（§9 第 4 条）。
type View struct {
	Supported   bool         `json:"supported"` // 群组代理端没有游戏聊天
	Settings    Settings     `json:"settings"`
	Defaults    Limits       `json:"defaults"` // 限流和额度的默认值（§6.7）
	Templates   []*Template  `json:"templates"`
	AI          AIStatus     `json:"ai"`
	Today       Today        `json:"today"`
	Players     []PlayerView `json:"players"`     // 可以授权的玩家：在线的、白名单里的、管理员、最近出现过的、已经授权的
	WhitelistOn bool         `json:"whitelistOn"` // server.properties 的 white-list 是 true
	Running     bool         `json:"running"`
	Local       bool         `json:"local"` // 请求来自本机（只有本机能改），由调用方填
}

// Limits 限流和额度。
type Limits struct {
	Cooldown  int   `json:"cooldown"`
	PerPlayer int   `json:"perPlayer"`
	PerServer int   `json:"perServer"`
	Budget    int64 `json:"budget"`
}

// AIStatus AI 接入层的情况。
type AIStatus struct {
	Enabled    bool        `json:"enabled"`
	Missing    string      `json:"missing,omitempty"`  // 还差什么没配好
	Provider   string      `json:"provider,omitempty"` // 提问会发给谁（隐私提示里的说法）
	Model      string      `json:"model,omitempty"`
	QuotaUsed  int64       `json:"quotaUsed"`       // 「设置 → AI」里游戏内 AI 今天用了多少 token（所有实例合计）
	QuotaLimit int64       `json:"quotaLimit"`      // 那里设的每日额度，0 = 不限
	Price      *TokenPrice `json:"price,omitempty"` // 当前模型大约多少钱一百万 token（界面上据此写「全服每天的 token」约合多少钱）；查不到单价时没有
}

// TokenPrice 当前模型每百万 token 大约多少钱。输入、输出的单价不一样，按这个月游戏内 AI 实际的比例摊
// （缓存命中按缓存价）；这个月还没用过时按九成输入、一成输出估（游戏内 AI 的请求大多是系统提示和工具说明，回答很短）。
type TokenPrice struct {
	Currency   string  `json:"currency,omitempty"` // USD / CNY
	PerMillion float64 `json:"perMillion"`
	Local      bool    `json:"local,omitempty"` // 本机模型，不花钱
	Note       string  `json:"note,omitempty"`  // 国内几家按国际站美元价估算的说明
}

// Today 这个实例今天的次数和用量。
type Today struct {
	Count     int            `json:"count"`     // 全服今天一共受理了几次（问 AI 和叫伙伴合计）
	Tokens    int64          `json:"tokens"`    // 全服今天用了多少 token
	Players   map[string]int `json:"players"`   // 名字小写 → 问了 AI 几次
	Companion map[string]int `json:"companion"` // 名字小写 → 叫了伙伴几次
	Top       []PlayerUsage  `json:"top"`       // 今天谁用了几次：按两种合计从多到少，最多 20 个
}

// PlayerUsage 一个人今天的次数。
type PlayerUsage struct {
	Name      string `json:"name"` // 尽量用原来的大小写（最近出现过的名字里找）
	Count     int    `json:"count"`
	Companion int    `json:"companion,omitempty"`
}

// PlayerView 授权列表里的一个人。
type PlayerView struct {
	Name      string `json:"name"`
	Online    bool   `json:"online"`
	Whitelist bool   `json:"whitelist"`
	Op        bool   `json:"op"`
	Seen      bool   `json:"seen"` // 最近出现过
	Allowed   bool   `json:"allowed"`
}

// Settings 实例当前的设置。
func (s *Service) Settings(inst string) (Settings, error) {
	r, err := s.room(inst)
	if err != nil {
		return Settings{}, err
	}
	return r.settings(), nil
}

// View 「游戏内 AI」标签的内容。
func (s *Service) View(inst string) (*View, error) {
	r, err := s.room(inst)
	if err != nil {
		return nil, err
	}
	info, err := s.b.Info(inst)
	if err != nil {
		return nil, err
	}
	pl, err := s.b.Players(inst)
	if err != nil {
		return nil, err
	}
	now := s.now()
	r.mu.Lock()
	set := r.set
	r.st.Counters.roll(now)
	c := r.st.Counters
	today := Today{Count: c.Count, Tokens: c.Tokens, Players: map[string]int{}, Companion: map[string]int{}}
	for k, v := range c.Players {
		today.Players[k] = v
	}
	for k, v := range c.Companion {
		today.Companion[k] = v
	}
	seen := map[string]bool{}
	for _, n := range recentNames(r.seen) {
		seen[n] = true
	}
	for _, n := range recentNames(r.st.Seen) {
		seen[n] = true
	}
	r.mu.Unlock()
	today.Top = topUsers(today, seen, pl.Online, set.Allowed)

	v := &View{Supported: !info.Proxy, Settings: set, Templates: Templates, Today: today, WhitelistOn: pl.WhitelistOn,
		Running:  info.Status == "running",
		Defaults: Limits{Cooldown: DefaultCooldown, PerPlayer: DefaultPerPlayer, PerServer: DefaultPerServer, Budget: DefaultBudget}}
	av := s.ai.View()
	v.AI = AIStatus{Enabled: av.Enabled, Missing: av.Missing}
	if av.Enabled {
		v.AI.Provider, v.AI.Model = strings.TrimSpace(providerName(s.ai)), av.Provider.Model
		v.AI.Price = tokenPrice(av.ModelPrice, av.Month[ai.FeatGameChat])
	}
	used, limit := s.ai.Usage(ai.FeatGameChat)
	v.AI.QuotaUsed, v.AI.QuotaLimit = used.Tokens, limit

	// 可以授权的玩家：同一个名字（不分大小写）只列一次
	byKey := map[string]*PlayerView{}
	var order []string
	add := func(name string, f func(p *PlayerView)) {
		if name == "" {
			return
		}
		k := strings.ToLower(name)
		p := byKey[k]
		if p == nil {
			p = &PlayerView{Name: name}
			byKey[k] = p
			order = append(order, k)
		}
		f(p)
	}
	for _, n := range pl.Online {
		add(n, func(p *PlayerView) { p.Online = true })
	}
	for _, n := range set.Allowed {
		add(n, func(p *PlayerView) { p.Allowed = true })
	}
	for _, n := range pl.Whitelist {
		add(n, func(p *PlayerView) { p.Whitelist = true })
	}
	for _, n := range pl.Ops {
		add(n, func(p *PlayerView) { p.Op = true })
	}
	for n := range seen {
		add(n, func(p *PlayerView) { p.Seen = true })
	}
	for _, k := range order {
		p := byKey[k]
		p.Allowed = set.Authorized(p.Name)
		p.Whitelist = p.Whitelist || slices.ContainsFunc(pl.Whitelist, func(x string) bool { return strings.EqualFold(x, p.Name) })
		v.Players = append(v.Players, *p)
	}
	sort.SliceStable(v.Players, func(i, j int) bool {
		a, b := v.Players[i], v.Players[j]
		if a.Online != b.Online {
			return a.Online
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if v.Players == nil {
		v.Players = []PlayerView{}
	}
	return v, nil
}

// maxTop 「今天谁用了几次」最多列几个人。
const maxTop = 20

// topUsers 今天谁用了几次：按问 AI 和叫伙伴的合计从多到少（一样多的按名字），名字尽量换回原来的大小写。
func topUsers(t Today, seen map[string]bool, more ...[]string) []PlayerUsage {
	names := map[string]string{} // 小写 → 原样
	for n := range seen {
		names[strings.ToLower(n)] = n
	}
	for _, list := range more {
		for _, n := range list {
			names[strings.ToLower(n)] = n
		}
	}
	byKey := map[string]*PlayerUsage{}
	get := func(k string) *PlayerUsage {
		if byKey[k] == nil {
			n := names[k]
			if n == "" {
				n = k
			}
			byKey[k] = &PlayerUsage{Name: n}
		}
		return byKey[k]
	}
	for k, n := range t.Players {
		if n > 0 {
			get(k).Count = n
		}
	}
	for k, n := range t.Companion {
		if n > 0 {
			get(k).Companion = n
		}
	}
	out := make([]PlayerUsage, 0, len(byKey))
	for _, p := range byKey {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Count+out[i].Companion, out[j].Count+out[j].Companion
		if a != b {
			return a > b
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > maxTop {
		out = out[:maxTop]
	}
	return out
}

// tokenPrice 当前模型大约多少钱一百万 token（见 TokenPrice）。mp 是「设置 → AI」里显示的当前模型单价（每百万 token），
// month 是这个月游戏内 AI 的用量（只用它的输入、输出、缓存的比例）。查不到单价时返回 nil。
func tokenPrice(mp *ai.ModelPrice, month ai.Totals) *TokenPrice {
	switch {
	case mp == nil || !mp.Found:
		return nil
	case mp.Local:
		return &TokenPrice{Local: true}
	}
	or := func(p *float64, d float64) float64 {
		if p == nil {
			return d
		}
		return *p
	}
	in, out, cr, cw := float64(month.Input), float64(month.Output), float64(month.CacheRead), float64(month.CacheWrite)
	if in+out+cr+cw <= 0 {
		in, out, cr, cw = 0.9, 0.1, 0, 0
	}
	per := (in*mp.Input + out*mp.Output + cr*or(mp.CacheRead, mp.Input) + cw*or(mp.CacheWrite, mp.Input)) / (in + out + cr + cw)
	return &TokenPrice{Currency: mp.Currency, PerMillion: per, Note: mp.Note}
}

// Update 改设置（只改传了的字段），存进实例目录，马上生效。
func (s *Service) Update(inst string, p Patch) (*View, error) {
	r, err := s.room(inst)
	if err != nil {
		return nil, err
	}
	// 先拿 fileMu（两次改设置排好队），磁盘在 r.mu 外面写：OnLine 在读服务器输出的协程里也要拿 r.mu
	r.fileMu.Lock()
	r.mu.Lock()
	next, err := p.Apply(r.set)
	dir := r.dir
	r.mu.Unlock()
	if err == nil {
		if e := saveSettings(dir, next); e != nil {
			err = fmt.Errorf("保存游戏内 AI 的设置失败：%w", e)
		}
	}
	if err == nil {
		r.mu.Lock()
		r.set, r.rules = next, next.Rules()
		r.mu.Unlock()
	}
	r.fileMu.Unlock()
	if err != nil {
		return nil, err
	}
	r.persist()
	return s.View(inst)
}

// History 最近的问答和执行记录，新的在前。
func (s *Service) History(inst string, limit int) ([]Record, error) {
	r, err := s.room(inst)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxHistory {
		limit = maxHistory
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, 0, min(limit, len(r.st.History)))
	for i := len(r.st.History) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.st.History[i])
	}
	return out, nil
}

// TestResult 识别测试（§6.1 第 6 步）：贴一行日志进去，显示认出的玩家、正文和选项，以及会怎么处理。
type TestResult struct {
	Hit
	Companion  string `json:"companion,omitempty"` // 叫的是这个伙伴的名字（AI 伙伴，第七阶段）
	Online     bool   `json:"online"`              // 这个人现在在线（不在线的人说的话不处理）
	Authorized bool   `json:"authorized"`          // 能让 AI 执行命令
	Result     string `json:"result"`              // 一句话说明
}

// Test 识别测试：按实例当前的设置识别一行日志；p 不为 nil 时先套用这些还没保存的改动（界面上边改边试）。
func (s *Service) Test(inst, line string, p *Patch) (*TestResult, error) {
	set, err := s.Settings(inst)
	if err != nil {
		return nil, err
	}
	if p != nil {
		if set, err = p.Apply(set); err != nil {
			return nil, err
		}
	}
	rules := set.Rules()
	base := rules
	list, _, _ := s.companionsLive(inst) // 伙伴的名字也能触发
	if len(list) > 0 {
		rules.Prefixes = append(slices.Clone(rules.Prefixes), companionPrefixes(list)...)
	}
	t := &TestResult{Hit: Recognize(line, rules)}
	if t.Ask != nil {
		t.Companion = addressed(t.Ask.Prefix, base, list)
	}
	if t.Chat {
		t.Online = containsName(s.b.Online(inst), t.Player)
		t.Authorized = set.Authorized(t.Player)
	}
	t.Result = describe(t, set)
	return t, nil
}

// describe 识别结果的一句话说明。
func describe(t *TestResult, set Settings) string {
	if t.Ask == nil {
		if t.Chat {
			return t.Player + " 的聊天，不触发：" + t.Reason
		}
		return "不触发：" + t.Reason
	}
	a, who := t.Ask, t.Player
	var out string
	switch a.Action {
	case ActHelp:
		out = "私发帮助给 " + who + "，不调用 AI"
	case ActQuota:
		out = "私发 " + who + " 今天还能问几次，不调用 AI"
	case ActUnknown:
		out = "未知选项 /" + a.Unknown + "：私发提示给 " + who + "，不调用 AI"
	case ActNoSpace:
		out = "选项和问题之间没有空格：私发提示给 " + who + "，不调用 AI"
	case ActMisplaced:
		out = "选项写在了问题后面：私发提示给 " + who + "，不调用 AI（免得想私聊的回答被全服看到）"
	default:
		out = who + " 提问「" + a.Question + "」：回答"
		if a.Private {
			out += "只发给 " + who
		} else {
			out += "全服可见"
		}
		switch {
		case t.Authorized && a.NoExec:
			out += "；用了 /问，这次不给执行命令的工具"
		case t.Authorized:
			out += "；" + who + " 有执行权限，可以让 AI 执行打开了的命令"
		default:
			out += "；" + who + " 没有执行权限，只回答"
		}
	}
	if t.Companion != "" {
		out += "。这句是对伙伴「" + t.Companion + "」说的：回复以它的名义发出，用量记在 AI 伙伴名下"
	}
	if !t.Online {
		out += "。注意：" + who + " 现在不在线，实际运行时不在线的人说的话不处理"
	}
	if !set.Enabled {
		out += "。游戏内 AI 现在是关着的"
	}
	return out
}
