package companion

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// LinkView 和伙伴模组的连接。
type LinkView struct {
	// State off：没启用；waiting：服务器没开（或者还没开好）；connecting：正在连；live：连着；
	// retrying：断了、正在重连；failed：重试也没用（协议版本对不上之类），等关服或改设置。
	State        string    `json:"state"`
	Since        time.Time `json:"since,omitempty"`
	Addr         string    `json:"addr,omitempty"`
	Error        string    `json:"error,omitempty"`
	Busy         bool      `json:"busy,omitempty"` // 别的程序占着控制连接（比如 testctl、另一台电脑上的 MineDeck）
	Mod          string    `json:"mod,omitempty"`  // 模组报的版本
	PlayerEngine string    `json:"playerEngine,omitempty"`
	Session      string    `json:"session,omitempty"`
	Protocol     int       `json:"protocol,omitempty"`
	Ops          []string  `json:"ops,omitempty"`
	Features     []string  `json:"features,omitempty"`
	RTTMs        float64   `json:"rttMs,omitempty"`
}

// SettingsView 配置文件里的设置，以及模组现在实际在用的。
type SettingsView struct {
	Config
	Applied     *ModConfig `json:"applied,omitempty"` // 模组报来的（连着时才有）
	Pending     bool       `json:"pending"`           // 文件改了、模组还没读到（每 2 秒读一次）
	DefaultDeny []string   `json:"defaultDeny"`       // attack_deny 的默认值
	MaxLimit    int        `json:"maxLimit"`          // 伙伴上限最多能设几个
	MaxZones    int        `json:"maxZones"`
}

// View 实例「伙伴」标签的全部内容。只读，不联网。
type View struct {
	Inst         string       `json:"inst"`
	ServerStatus string       `json:"serverStatus"`
	Online       []string     `json:"online"` // 在线的玩家（刷出时选主人、出生在谁旁边）
	Install      InstallView  `json:"install"`
	Client       []ClientMod  `json:"client"` // 好友的客户端要装什么（按清单）
	Settings     SettingsView `json:"settings"`
	Link         LinkView     `json:"link"`
	Companions   []Companion  `json:"companions"`
	Events       []EventView  `json:"events"`  // 新的在前
	Restart      bool         `json:"restart"` // 服务器运行中装了模组：重启后生效
}

// Status 汇总「伙伴」标签要显示的东西。
func (s *Service) Status(name string) (*View, error) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	v := &View{Inst: name, ServerStatus: info.Status, Online: append([]string{}, info.Online...), Install: s.installView(info), Client: []ClientMod{},
		Companions: []Companion{}, Events: []EventView{}}
	if man, err := s.manifest(); err == nil && v.Install.Supported {
		j, _ := supported(man, info.Core, info.MC)
		v.Client = clientMods(man, j)
	}
	cfg, err := ReadConfig(info.Dir)
	if err != nil {
		return nil, &Error{Code: 500, Err: fmt.Errorf("读伙伴模组的配置失败：%w", err)}
	}
	v.Settings = SettingsView{Config: cfg, DefaultDeny: DefaultAttackDeny, MaxLimit: MaxCompanionsLimit, MaxZones: MaxZones}

	s.mu.Lock()
	r := s.roomLocked(name)
	v.Restart = r.restart
	var c *Client
	if r.link != nil {
		c = r.link.c
	}
	for i := len(r.events) - 1; i >= 0; i-- {
		v.Events = append(v.Events, r.events[i])
	}
	if r.welcome != nil {
		v.Companions = sortedComps(r.comps)
		if r.modCfg != nil {
			applied := *r.modCfg
			v.Settings.Applied = &applied
		}
	}
	w := r.welcome
	s.mu.Unlock()

	switch {
	case !cfg.Enabled:
		v.Link.State = "off"
	case c == nil:
		v.Link.State = "waiting"
	default:
		st := c.State()
		v.Link.Since, v.Link.Addr, v.Link.Error, v.Link.Busy, v.Link.RTTMs = st.Since, st.Addr, st.LastError, st.Busy, st.RTTMs
		switch st.Phase {
		case PhaseLive:
			v.Link.State, v.Link.Error = "live", ""
		case PhaseRetrying:
			v.Link.State = "retrying"
		case PhaseStopped:
			v.Link.State = "failed"
		default:
			v.Link.State = "connecting"
		}
	}
	if w != nil && v.Link.State != "off" && v.Link.State != "waiting" {
		v.Link.Mod, v.Link.PlayerEngine, v.Link.Session, v.Link.Protocol = w.Mod, w.PlayerEngine, w.Session, w.Protocol
		v.Link.Ops, v.Link.Features = w.Ops, w.Features
	}
	if v.Link.State == "live" && v.Settings.Applied != nil {
		v.Settings.Pending = !sameConfig(cfg, *v.Settings.Applied)
	}
	if v.Link.State == "off" || v.Link.State == "waiting" {
		v.Companions = []Companion{}
	}
	for i := range v.Companions { // 不认识的 state 当成 busy、result 当成 failed（兼容规则 4）
		c := &v.Companions[i]
		c.State = KnownState(c.State)
		if c.LastResult != nil {
			lr := *c.LastResult
			lr.Result = KnownResult(lr.Result)
			c.LastResult = &lr
		}
	}
	return v, nil
}

// sameConfig 配置文件和模组在用的是不是一样（区域按 min / max 比）。
func sameConfig(c Config, a ModConfig) bool {
	if c.MaxCompanions != a.MaxCompanions {
		return false
	}
	x, y := slices.Clone(c.AttackDeny), slices.Clone(a.AttackDeny)
	slices.Sort(x)
	slices.Sort(y)
	if !slices.Equal(x, y) {
		return false
	}
	norm := func(zs []Zone) []string {
		var out []string
		for _, z := range zs {
			lo, hi := z.Min, z.Max
			for i := 0; i < 3; i++ {
				if lo[i] > hi[i] {
					lo[i], hi[i] = hi[i], lo[i]
				}
			}
			out = append(out, fmt.Sprintf("%s|%s|%v|%v", z.Name, z.Dim, lo, hi))
		}
		sort.Strings(out)
		return out
	}
	return slices.Equal(norm(c.Zones), norm(a.Zones))
}

// ---------- 改设置 ----------

// SettingsPatch 「伙伴」标签上的设置：启用开关，以及模组配置里的伙伴上限、attack_deny、禁挖区域。只改传了的字段。
type SettingsPatch struct {
	Enabled *bool `json:"enabled"`
	Patch
}

// SetSettings 改设置。打开时补好配置（端口、令牌）、服务器开着就去连；关掉时先给模组发 END 再改 enabled=false
// （伙伴还在游戏里，只是 MineDeck 指挥不了它们）。别的设置改了约 2 秒内生效，不用重启。
func (s *Service) SetSettings(name string, p SettingsPatch) (*View, error) {
	info, err := s.d.Instances.Info(name)
	if err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	iv := s.installView(info)
	if !iv.Supported {
		return nil, &Error{Code: 400, Err: errors.New(iv.Reason)}
	}
	if p.Enabled != nil && *p.Enabled && iv.Mods[0].State == ModMissing {
		return nil, errorf(400, "先装伙伴模组，再打开")
	}
	if _, err := p.Patch.Check(); err != nil {
		return nil, &Error{Code: 400, Err: err}
	}
	if p.MaxCompanions != nil || p.AttackDeny != nil || p.DenyDefault || p.Zones != nil {
		if _, err := Update(info.Dir, p.Patch); err != nil {
			return nil, &Error{Code: 500, Err: fmt.Errorf("写伙伴模组的配置失败：%w", err)}
		}
		s.mu.Lock()
		s.roomLocked(name).settled = time.Now()
		s.mu.Unlock()
	}
	if p.Enabled != nil {
		if *p.Enabled {
			if _, _, err := EnsureConfig(info.Dir, EnsureOptions{Avoid: s.portsInUse(name)}); err != nil {
				return nil, &Error{Code: 500, Err: fmt.Errorf("写伙伴模组的配置失败：%w", err)}
			}
			s.mu.Lock()
			s.roomLocked(name).settled = time.Now()
			s.mu.Unlock()
			s.ensure(name)
		} else {
			s.drop(name, "MineDeck 关掉了伙伴", true)
			if _, err := Disable(info.Dir); err != nil {
				return nil, &Error{Code: 500, Err: fmt.Errorf("写伙伴模组的配置失败：%w", err)}
			}
		}
	}
	return s.Status(name)
}

// ---------- 刷出、移除、停下（人在界面上做，或者走提案）----------

var playerRe = regexp.MustCompile(`^[A-Za-z0-9_.]{1,20}$`)

// ValidPlayer 玩家名（同模组的检查）：1–20 个字母、数字、_、.。
func ValidPlayer(n string) bool { return playerRe.MatchString(n) }

// MaxNameRunes 伙伴名字最多几个字（模组允许 32 个，MineDeck 收紧到 16：要能在聊天里叫它「@名字」）。
const MaxNameRunes = 16

// CheckName 伙伴的名字：1–16 个字；不能有空白、@、选项标记（/ ／ 、）、冒号、逗号、< > " \ §：
// 玩家在聊天里用「@名字 话」叫它，这些字符会让游戏内 AI 认不出来。
func CheckName(n string) error {
	if c := utf8.RuneCountInString(n); c == 0 || c > MaxNameRunes {
		return fmt.Errorf("伙伴的名字要 1–%d 个字", MaxNameRunes)
	}
	for _, r := range n {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`@＠/／、:：,，<>"\§`, r) {
			return fmt.Errorf("伙伴的名字「%s」里有不能用的字符（空格、@、/、、、冒号、逗号、< > \" \\ §）：玩家要在聊天里用「@名字」叫它", n)
		}
	}
	return nil
}

// SpawnReq 刷出伙伴。
type SpawnReq struct {
	Name  string `json:"name"`
	Owner string `json:"owner"` // 主人的游戏名（可以不在线），任务通知、跟随和给东西默认找他
	Near  string `json:"near"`  // 出生在这个在线玩家旁边；不填就在主人旁边（主人不在线就在世界出生点）
}

func (s *Service) mustLive(name string) (*Client, error) {
	if _, err := s.d.Instances.Info(name); err != nil {
		return nil, &Error{Code: 404, Err: err}
	}
	c, _ := s.liveClient(name)
	if c == nil {
		return nil, errorf(409, "现在没连着伙伴模组：要先开服、打开伙伴，等连上以后再试")
	}
	return c, nil
}

func opErr(err error) error {
	var oe *OpError
	if errors.As(err, &oe) {
		return &Error{Code: 400, Err: err}
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return &Error{Code: 502, Err: err}
}

// Spawn 刷出一个伙伴，返回它的伙伴对象。
func (s *Service) Spawn(ctx context.Context, name string, req SpawnReq) (*Companion, error) {
	req.Name, req.Owner, req.Near = strings.TrimSpace(req.Name), strings.TrimSpace(req.Owner), strings.TrimSpace(req.Near)
	if err := CheckName(req.Name); err != nil {
		return nil, &Error{Code: 400, Err: err}
	}
	for _, p := range []string{req.Owner, req.Near} {
		if p != "" && !ValidPlayer(p) {
			return nil, errorf(400, "玩家名「%s」不对：要 1–20 个字母、数字、_、.", p)
		}
	}
	c, err := s.mustLive(name)
	if err != nil {
		return nil, err
	}
	args := map[string]any{"name": req.Name}
	if req.Owner != "" {
		args["owner"] = req.Owner
	}
	if req.Near != "" {
		args["near"] = req.Near
	}
	var out struct {
		Info *Companion `json:"info"`
	}
	if _, err := c.Call(ctx, "spawn", args, &out); err != nil {
		return nil, opErr(err)
	}
	if out.Info == nil {
		return nil, errorf(502, "伙伴模组的回复里没有伙伴的信息")
	}
	return out.Info, nil
}

// DespawnReq 移除伙伴。
type DespawnReq struct {
	Companion int  `json:"companion"`
	All       bool `json:"all"`
	Drop      bool `json:"drop"` // true：背包里的东西掉在地上；false：一起消失
}

// Despawn 移除伙伴，返回移除了的编号。
func (s *Service) Despawn(ctx context.Context, name string, req DespawnReq) ([]int, error) {
	if !req.All && req.Companion <= 0 {
		return nil, errorf(400, "要说清楚移除哪个伙伴（companion），或者 all")
	}
	c, err := s.mustLive(name)
	if err != nil {
		return nil, err
	}
	args := map[string]any{"drop": req.Drop}
	if req.All {
		args["all"] = true
	} else {
		args["companion"] = req.Companion
	}
	var out struct {
		Removed []int `json:"removed"`
	}
	if _, err := c.Call(ctx, "despawn", args, &out); err != nil {
		return nil, opErr(err)
	}
	if out.Removed == nil {
		out.Removed = []int{}
	}
	return out.Removed, nil
}

// StopReq 停下伙伴的任务。
type StopReq struct {
	Companion int  `json:"companion"`
	All       bool `json:"all"`
}

// StopTasks 停下任务，返回被停下的任务编号。
func (s *Service) StopTasks(ctx context.Context, name string, req StopReq) ([]int64, error) {
	if !req.All && req.Companion <= 0 {
		return nil, errorf(400, "要说清楚停下哪个伙伴（companion），或者 all")
	}
	c, err := s.mustLive(name)
	if err != nil {
		return nil, err
	}
	args := map[string]any{}
	if req.All {
		args["all"] = true
	} else {
		args["companion"] = req.Companion
	}
	var out struct {
		Stopped []int64 `json:"stopped"`
	}
	if _, err := c.Call(ctx, "stop", args, &out); err != nil {
		return nil, opErr(err)
	}
	if out.Stopped == nil {
		out.Stopped = []int64{}
	}
	return out.Stopped, nil
}

// Companion 按编号取现有的伙伴（提案的预览、确认卡片上显示名字用）。
func (s *Service) Companion(name string, id int) (Companion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[name]
	if r == nil {
		return Companion{}, false
	}
	c, ok := r.comps[id]
	return c, ok
}
