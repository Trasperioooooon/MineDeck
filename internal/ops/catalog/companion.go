package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/companion"
	"minedeck/internal/inst"
	"minedeck/internal/ops"
)

// AI 伙伴（设计文档 §7，第七阶段）：实例的「伙伴」标签。
//
// 风险等级照现有的约定：看状态是读操作；装模组（要从 Modrinth 下载 PlayerEngine 和 Architectury API）、改设置、
// 刷出、移除、全部停下是写操作，人在界面上点了就做，AI 助手只能提出、人确认后才做。
// 伙伴的动作（跟随、采集、去某处、打怪、给东西、停下、说话）不在这里：只有游戏内 AI 的工具能用，而且只给授权的玩家
// （见 chatai/companion.go）。每个动作另记一条 companion.action（来源 companion），不是登记的操作。

// NewCompanion 建 AI 伙伴的管理器：服务器开好、启用了伙伴时自动连上伙伴模组；有人发起的任务做完或失败时经 onFinish
// （游戏内 AI 的 NotifyTask）用模板通知他。只在用户点「安装」时访问 Modrinth（dl.Client），别的时候不联网。
func NewCompanion(mgr *inst.Manager, onFinish func(companion.Finished)) *companion.Service {
	return companion.New(companion.Deps{Instances: companionInstances{mgr}, ClientName: "MineDeck " + app.Version, OnFinish: onFinish})
}

// companionInstances 用 inst.Manager 实现 companion.Instances。
type companionInstances struct{ m *inst.Manager }

func (w companionInstances) Names() []string {
	var out []string
	for _, i := range w.m.List() {
		out = append(out, i.Name)
	}
	return out
}

func (w companionInstances) Info(name string) (companion.InstInfo, error) {
	i, err := w.m.Get(name)
	if err != nil {
		return companion.InstInfo{}, err
	}
	return companion.InstInfo{Name: i.Name, Dir: i.Dir, Core: string(i.Core), MC: i.MC, Build: i.Build, Status: i.Status(), Online: i.OnlinePlayers()}, nil
}

func (w companionInstances) Log(name, line string) {
	if i, err := w.m.Get(name); err == nil && i.Console != nil {
		i.Console.Append(line)
	}
}

// companionErr 带上 companion 给的状态码。
func companionErr(err error) error {
	var ce *companion.Error
	if errors.As(err, &ce) {
		return ops.Wrap(ce.Code, ce.Err)
	}
	return err
}

// companionView 「伙伴」标签的内容：companion.Status，加上 AI 的情况（伙伴要靠游戏内 AI 指挥）。
type companionView struct {
	*companion.View
	AI     companionAI     `json:"ai"`
	GameAI companionGameAI `json:"gameai"`
	Local  bool            `json:"local"`
}

type companionAI struct {
	Enabled    bool   `json:"enabled"`
	Missing    string `json:"missing,omitempty"`
	QuotaUsed  int64  `json:"quotaUsed"`  // 「设置 → AI」里 AI 伙伴今天用了多少 token
	QuotaLimit int64  `json:"quotaLimit"` // 那里设的每日额度，0 = 不限
}

type companionGameAI struct {
	Enabled bool     `json:"enabled"` // 实例的游戏内 AI 开着（玩家才能在聊天里指挥伙伴）
	Prefix  string   `json:"prefix"`
	Allowed []string `json:"allowed"` // 能指挥伙伴的玩家（就是能让 AI 执行命令的那些）
}

type companionZoneArg struct {
	Name string `json:"name"`
	Dim  string `json:"dim"`
	Min  []int  `json:"min"`
	Max  []int  `json:"max"`
}

type companionSettingsArgs struct {
	Inst          string              `json:"inst"`
	Enabled       *bool               `json:"enabled"`
	MaxCompanions *int                `json:"maxCompanions"`
	AttackDeny    *[]string           `json:"attackDeny"`
	DenyDefault   bool                `json:"denyDefault"`
	Zones         *[]companionZoneArg `json:"zones"`
}

// patch 转成 companion.SettingsPatch；两个角都要正好 3 个坐标（JSON 解进 [3]int 时少写的会悄悄变成 0）。
func (a companionSettingsArgs) patch() (companion.SettingsPatch, error) {
	p := companion.SettingsPatch{Enabled: a.Enabled, Patch: companion.Patch{MaxCompanions: a.MaxCompanions, AttackDeny: a.AttackDeny, DenyDefault: a.DenyDefault}}
	if a.Zones != nil {
		zs := []companion.Zone{}
		for _, z := range *a.Zones {
			if len(z.Min) != 3 || len(z.Max) != 3 {
				return p, ops.Errorf(400, "禁挖区域「%s」的两个角都要写 x、y、z 三个坐标", z.Name)
			}
			zs = append(zs, companion.Zone{Name: z.Name, Dim: z.Dim, Min: [3]int{z.Min[0], z.Min[1], z.Min[2]}, Max: [3]int{z.Max[0], z.Max[1], z.Max[2]}})
		}
		p.Zones = &zs
	}
	return p, nil
}

type companionIDArgs struct {
	Inst      string `json:"inst"`
	Companion int    `json:"companion"`
	All       bool   `json:"all"`
	Drop      bool   `json:"drop"`
}

type companionSpawnArgs struct {
	Inst string `json:"inst"`
	companion.SpawnReq
}

func (c *cat) companionOps() {
	if c.Companion == nil {
		return
	}
	view := func(caller ops.Caller, name string) (any, error) {
		v, err := c.Companion.Status(name)
		if err != nil {
			return nil, companionErr(err)
		}
		out := companionView{View: v, Local: caller.Local, GameAI: companionGameAI{Allowed: []string{}}}
		if c.AI != nil {
			av := c.AI.View()
			used, limit := c.AI.Usage(ai.FeatCompanion)
			out.AI = companionAI{Enabled: av.Enabled, Missing: av.Missing, QuotaUsed: used.Tokens, QuotaLimit: limit}
		}
		if c.GameChat != nil {
			if s, err := c.GameChat.Settings(name); err == nil {
				out.GameAI = companionGameAI{Enabled: s.Enabled, Prefix: s.Prefix, Allowed: append([]string{}, s.Allowed...)}
			}
		}
		return out, nil
	}
	who := func(name string, id int, all bool) string {
		if all {
			return "所有伙伴"
		}
		if comp, ok := c.Companion.Companion(name, id); ok {
			return fmt.Sprintf("伙伴「%s」（%d 号）", comp.Name, id)
		}
		return fmt.Sprintf("%d 号伙伴", id)
	}
	idParams := func(extra map[string]*ops.Schema) *ops.Schema {
		p := map[string]*ops.Schema{
			"companion": ops.Integer("伙伴的编号（见 companion.status 的 companions）").Range(1, 1000),
			"all":       ops.Boolean("所有伙伴"),
		}
		for k, v := range extra {
			p[k] = v
		}
		return withInst(p)
	}

	c.reg.Register(&ops.Op{
		Name: "companion.status", Title: "查看 AI 伙伴", Level: ops.Read,
		Desc: "实例「伙伴」标签的内容：伙伴模组、PlayerEngine、Architectury API 装没装、是什么版本，好友的客户端要装什么；" +
			"伙伴的设置（开关、伙伴上限、attack 不许打的生物、禁挖区域）和模组实际在用的；和伙伴模组的连接；现有的伙伴（位置、血量、状态、当前任务、" +
			"上次的结果）；最近的事件。不改任何东西，不联网。",
		Params: withInst(nil),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			return view(caller, p.Inst)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "companion.install", Title: "安装 AI 伙伴模组", Level: ops.Write,
		Desc: "把 MineDeck 内置的伙伴模组装进实例的 mods，再从 Modrinth 下载它依赖的 PlayerEngine 和 Architectury API" +
			"（先装实测过的版本、核对大小和 sha512；实测过的下载不到才换成 Modrinth 上符合规则的版本；已经装着、版本在范围内的不动）。" +
			"换掉旧版本；服务器运行中只能新加、重启后生效，要替换旧版本得先停服。只支持清单里的加载器和 MC 版本。",
		Params: withInst(nil),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			pv, err := c.Companion.PreviewInstall(p.Inst)
			if err != nil {
				return nil, companionErr(err)
			}
			plan := &ops.Plan{Pre: map[string]string{"实例状态": pv.Status, "mods 里的相关文件": pv.Files}}
			if pv.Noop {
				plan.Summary = fmt.Sprintf("「%s」的伙伴模组和两个依赖都装好了，不用改", p.Inst)
				return plan, nil
			}
			plan.Summary = fmt.Sprintf("给「%s」装 AI 伙伴：%s", p.Inst, strings.Join(append(append([]string{}, pv.Write...), pv.Download...), "、"))
			if len(pv.Download) > 0 {
				plan.Summary += fmt.Sprintf("（要从 Modrinth 下载约 %.1f MB）", float64(pv.Bytes)/(1<<20))
			}
			if pv.Running {
				plan.Summary += "；服务器正在运行，重启后生效"
			}
			for _, f := range pv.Write {
				plan.Changes = append(plan.Changes, ops.Change{What: "mods/" + f, Before: "无", After: "安装（MineDeck 内置）"})
			}
			for _, f := range pv.Download {
				plan.Changes = append(plan.Changes, ops.Change{What: "mods/" + f, Before: "无", After: "从 Modrinth 下载"})
			}
			for _, f := range pv.Remove {
				plan.Changes = append(plan.Changes, ops.Change{What: "mods/" + f, Before: "有", After: "删除（旧版本）"})
			}
			return plan, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			res, err := c.Companion.Install(ctx, p.Inst)
			if err != nil {
				return nil, companionErr(err)
			}
			return res, nil
		},
	})

	zone := ops.Object(map[string]*ops.Schema{
		"name": ops.String("区域名：1–32 个字母、数字、_、-").Len(1, 32),
		"dim":  ops.String("维度的完整 ID，如 minecraft:overworld、minecraft:the_nether").Len(3, 100),
		"min":  ops.Array("一个角的方块坐标 [x, y, z]", ops.Integer("坐标")).MaxLen(3),
		"max":  ops.Array("另一个角的方块坐标 [x, y, z]（谁先谁后都行，包含两端）", ops.Integer("坐标")).MaxLen(3),
	}, "name", "dim", "min", "max")
	settingsPre := func(name string) (map[string]string, error) {
		i, err := c.get(name)
		if err != nil {
			return nil, err
		}
		return map[string]string{"伙伴模组的配置文件": fileHash(companion.ConfigPath(i.Dir))}, nil
	}
	c.reg.Register(&ops.Op{
		Name: "companion.settings.set", Title: "修改 AI 伙伴的设置", Level: ops.Write,
		Desc: "只改传了的字段。enabled：打开伙伴（伙伴模组在 127.0.0.1 上打开控制端口，MineDeck 写好端口和令牌，服务器开着就去连）或者关掉" +
			"（伙伴还在游戏里，只是指挥不了）；maxCompanions：同时最多几个伙伴（1–16）；attackDeny：attack 不许打的生物种类（整体替换，空数组表示没有额外限制）；" +
			"denyDefault：恢复默认的不许打名单；zones：禁挖区域（整体替换，伙伴不拆也不放区域里的方块）。约 2 秒内生效，不用重启。",
		Params: withInst(map[string]*ops.Schema{
			"enabled":       ops.Boolean("打开伙伴"),
			"maxCompanions": ops.Integer("同时最多几个伙伴").Range(1, 16),
			"attackDeny":    ops.Array("attack 不许打的生物种类 ID", ops.String("生物 ID，如 minecraft:villager").Len(1, 100)).MaxLen(64),
			"denyDefault":   ops.Boolean("恢复默认的不许打名单（村民、流浪商人、铁傀儡、雪傀儡、悦灵）"),
			"zones":         ops.Array("禁挖区域", zone).MaxLen(64),
		}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[companionSettingsArgs](a)
			if err != nil {
				return nil, err
			}
			sp, err := p.patch()
			if err != nil {
				return nil, err
			}
			if _, err := sp.Patch.Check(); err != nil {
				return nil, ops.Wrap(400, err)
			}
			v, err := c.Companion.Status(p.Inst)
			if err != nil {
				return nil, companionErr(err)
			}
			pre, err := settingsPre(p.Inst)
			if err != nil {
				return nil, err
			}
			cur := v.Settings
			plan := &ops.Plan{Pre: pre}
			onOff := map[bool]string{true: "开", false: "关"}
			if sp.Enabled != nil && *sp.Enabled != cur.Enabled {
				plan.Changes = append(plan.Changes, ops.Change{What: "伙伴", Before: onOff[cur.Enabled], After: onOff[*sp.Enabled]})
			}
			if sp.MaxCompanions != nil && *sp.MaxCompanions != cur.MaxCompanions {
				plan.Changes = append(plan.Changes, ops.Change{What: "伙伴上限", Before: fmt.Sprint(cur.MaxCompanions), After: fmt.Sprint(*sp.MaxCompanions)})
			}
			if sp.DenyDefault || sp.AttackDeny != nil {
				after := strings.Join(companion.DefaultAttackDeny, "、") + "（默认）"
				if !sp.DenyDefault {
					after = orEmpty(strings.Join(*sp.AttackDeny, "、"))
				}
				plan.Changes = append(plan.Changes, ops.Change{What: "attack 不许打的生物", Before: orEmpty(strings.Join(cur.AttackDeny, "、")), After: after})
			}
			if sp.Zones != nil {
				names := func(zs []companion.Zone) string {
					var n []string
					for _, z := range zs {
						n = append(n, z.Name)
					}
					return orEmpty(strings.Join(n, "、"))
				}
				plan.Changes = append(plan.Changes, ops.Change{What: "禁挖区域", Before: names(cur.Zones), After: names(*sp.Zones)})
			}
			plan.Summary = fmt.Sprintf("改「%s」的 AI 伙伴设置", p.Inst)
			if len(plan.Changes) == 0 {
				plan.Summary += "（和现在一样，不用改）"
			}
			return plan, nil
		},
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[companionSettingsArgs](a)
			if err != nil {
				return nil, err
			}
			sp, err := p.patch()
			if err != nil {
				return nil, err
			}
			if _, err := c.Companion.SetSettings(p.Inst, sp); err != nil {
				return nil, companionErr(err)
			}
			return view(caller, p.Inst)
		},
	})

	livePre := func(name string) (map[string]string, error) {
		v, err := c.Companion.Status(name)
		if err != nil {
			return nil, companionErr(err)
		}
		return map[string]string{"伙伴模组的连接": v.Link.State + " " + v.Link.Session}, nil
	}
	c.reg.Register(&ops.Op{
		Name: "companion.spawn", Title: "刷出 AI 伙伴", Level: ops.Write,
		Desc: "刷出一个伙伴（服务器要开着、MineDeck 连着伙伴模组）。name：名字，1–16 个字，玩家在聊天里用「@名字」叫它；owner：主人的游戏名" +
			"（任务通知、跟随和给东西默认找他）；near：出生在这个在线玩家旁边，不填就在主人旁边（主人不在线就在世界出生点）。",
		Params: withInst(map[string]*ops.Schema{
			"name":  ops.String("伙伴的名字").Len(1, 16),
			"owner": ops.String("主人的游戏名").Len(0, 20),
			"near":  ops.String("出生在这个在线玩家旁边").Len(0, 20),
		}, "name"),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[companionSpawnArgs](a)
			if err != nil {
				return nil, err
			}
			if err := companion.CheckName(strings.TrimSpace(p.Name)); err != nil {
				return nil, ops.Wrap(400, err)
			}
			pre, err := livePre(p.Inst)
			if err != nil {
				return nil, err
			}
			s := fmt.Sprintf("在「%s」刷出伙伴「%s」", p.Inst, strings.TrimSpace(p.Name))
			if p.Owner != "" {
				s += "，主人 " + p.Owner
			}
			if p.Near != "" {
				s += "，出生在 " + p.Near + " 旁边"
			}
			return &ops.Plan{Summary: s, Pre: pre}, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[companionSpawnArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.checkCompanionName(p.Inst, strings.TrimSpace(p.Name)); err != nil {
				return nil, err
			}
			comp, err := c.Companion.Spawn(ctx, p.Inst, p.SpawnReq)
			if err != nil {
				return nil, companionErr(err)
			}
			return comp, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "companion.despawn", Title: "移除 AI 伙伴", Level: ops.Write,
		Desc:   "移除一个伙伴（companion 写编号）或者所有伙伴（all）。drop 为 true 时背包里的东西掉在地上，否则一起消失。",
		Params: idParams(map[string]*ops.Schema{"drop": ops.Boolean("背包里的东西掉在地上")}),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[companionIDArgs](a)
			if err != nil {
				return nil, err
			}
			if !p.All && p.Companion <= 0 {
				return nil, ops.Errorf(400, "要说清楚移除哪个伙伴（companion），或者 all")
			}
			pre, err := livePre(p.Inst)
			if err != nil {
				return nil, err
			}
			s := fmt.Sprintf("从「%s」移除%s", p.Inst, who(p.Inst, p.Companion, p.All))
			if p.Drop {
				s += "，背包里的东西掉在原地"
			} else {
				s += "，背包里的东西一起消失"
			}
			return &ops.Plan{Summary: s, Pre: pre}, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[companionIDArgs](a)
			if err != nil {
				return nil, err
			}
			removed, err := c.Companion.Despawn(ctx, p.Inst, companion.DespawnReq{Companion: p.Companion, All: p.All, Drop: p.Drop})
			if err != nil {
				return nil, companionErr(err)
			}
			return map[string]any{"removed": removed}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "companion.stop", Title: "让 AI 伙伴停下", Level: ops.Write,
		Desc:   "停下一个伙伴（companion 写编号）或者所有伙伴（all）当前的任务。",
		Params: idParams(nil),
		Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[companionIDArgs](a)
			if err != nil {
				return nil, err
			}
			if !p.All && p.Companion <= 0 {
				return nil, ops.Errorf(400, "要说清楚停下哪个伙伴（companion），或者 all")
			}
			pre, err := livePre(p.Inst)
			if err != nil {
				return nil, err
			}
			return &ops.Plan{Summary: fmt.Sprintf("让「%s」的%s停下手上的任务", p.Inst, who(p.Inst, p.Companion, p.All)), Pre: pre}, nil
		},
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[companionIDArgs](a)
			if err != nil {
				return nil, err
			}
			stopped, err := c.Companion.StopTasks(ctx, p.Inst, companion.StopReq{Companion: p.Companion, All: p.All})
			if err != nil {
				return nil, companionErr(err)
			}
			return map[string]any{"stopped": stopped}, nil
		},
	})
}

// checkCompanionName 伙伴的名字不能和游戏内 AI 的前缀、别名撞（撞了的话「@名字」叫不到它，前缀优先）。
func (c *cat) checkCompanionName(name, comp string) error {
	if err := companion.CheckName(comp); err != nil {
		return ops.Wrap(400, err)
	}
	if c.GameChat == nil {
		return nil
	}
	s, err := c.GameChat.Settings(name)
	if err != nil {
		return nil
	}
	for _, p := range s.Prefixes() {
		if (strings.HasPrefix(p, "@") || strings.HasPrefix(p, "＠")) && strings.EqualFold(strings.TrimLeft(p, "@＠"), comp) {
			return ops.Errorf(400, "伙伴的名字「%s」和游戏内 AI 的前缀「%s」撞了：玩家说「@%s」时会当成问游戏内 AI，换个名字吧", comp, p, comp)
		}
	}
	return nil
}
