package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/ai"
	"minedeck/internal/chatai"
	"minedeck/internal/inst"
	"minedeck/internal/mclog"
	"minedeck/internal/mcsrc"
	"minedeck/internal/ops"
)

// 游戏内聊天 AI（设计文档 §6，第四阶段）。
//
// 游戏内 AI 自己只能调用一个操作：只读的 game.info（InGame，§6.4）；执行命令不走操作层，按命令模板的白名单校验后
// 直接写进服务器的标准输入，每条都记操作记录（来源 gamechat）。「游戏内 AI」标签的几个操作都是「仅人工」：
// AI 永远看不到、也改不了自己的设置。改设置和一键开白名单还「仅本机」（§3.3：游戏内 AI 的授权名单和命令模板只能在本机改）；
// 局域网登录的设备能看设置、最近的问答，能做识别测试。

// NewGameChat 建游戏内聊天 AI：服务器每输出一行都交给它认聊天（inst 的回调），game_info 和操作记录用 reg，
// 脱敏和应用内助手共用一套（NewRedactor）。comps 是 AI 伙伴（第七阶段，可以为 nil）。
// 不起任何后台任务：有人触发了才为那个实例起处理的协程。
func NewGameChat(mgr *inst.Manager, reg *ops.Registry, aiSvc *ai.Service, token string, comps chatai.Companions) *chatai.Service {
	gc := chatai.New(chatai.Options{Backend: gameBackend{mgr}, AI: aiSvc, Ops: reg, Redact: NewRedactor(token), Companions: comps})
	mgr.SetLineHook(gc.OnLine)
	return gc
}

// gameBackend 用 inst.Manager 实现 chatai.Backend。
type gameBackend struct{ m *inst.Manager }

func (b gameBackend) Dir(name string) (string, error) {
	i, err := b.m.Get(name)
	if err != nil {
		return "", err
	}
	return i.Dir, nil
}

func (b gameBackend) Info(name string) (chatai.Info, error) {
	i, err := b.m.Get(name)
	if err != nil {
		return chatai.Info{}, err
	}
	return chatai.Info{Core: string(i.Core), MC: i.MC, Status: i.Status(), Started: i.LastStart(), Proxy: mcsrc.KindOf(i.Core) == mcsrc.KindProxy}, nil
}

func (b gameBackend) Online(name string) []string {
	i, err := b.m.Get(name)
	if err != nil {
		return nil
	}
	return i.OnlinePlayers()
}

func (b gameBackend) Players(name string) (chatai.Players, error) {
	i, err := b.m.Get(name)
	if err != nil {
		return chatai.Players{}, err
	}
	pl, err := b.m.GetPlayers(name)
	if err != nil {
		return chatai.Players{}, err
	}
	out := chatai.Players{Online: pl.Online}
	for _, e := range pl.Whitelist {
		out.Whitelist = append(out.Whitelist, e.Name)
	}
	for _, e := range pl.Ops {
		out.Ops = append(out.Ops, e.Name)
	}
	out.WhitelistOn = whitelistOn(i)
	return out, nil
}

func (b gameBackend) Send(name, cmd, echo string) error { return b.m.CommandEcho(name, cmd, echo) }

// whitelistOn server.properties 的 white-list 是不是 true。
func whitelistOn(i *inst.Instance) bool {
	p, err := inst.LoadProps(i.PropsPath())
	if err != nil {
		return false
	}
	v, _ := p.Get("white-list")
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

// gameInfo game.info 的结果：§6.4 列出的只读信息，不含日志、配置文件和别的聊天。
type gameInfo struct {
	Core       string   `json:"core"`             // 核心的名字，如 NeoForge
	MC         string   `json:"mc"`               // Minecraft 版本
	Loader     string   `json:"loader,omitempty"` // 加载器或核心的构建号
	Mods       int      `json:"mods"`             // mods 目录里启用的模组数量
	Plugins    int      `json:"plugins,omitempty"`
	Status     string   `json:"status"`
	Online     []string `json:"online"`
	MaxPlayers int      `json:"maxPlayers"`
	Time       string   `json:"time"` // 服务器（主机）的本地时间
}

var weekdays = []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

// countJars 目录里 .jar 文件的数量（禁用的 .jar.disabled 不算）。
func countJars(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			n++
		}
	}
	return n
}

func coreDisplayName(c mcsrc.Core) string {
	for _, ci := range mcsrc.Cores() {
		if ci.ID == c {
			return ci.Name
		}
	}
	return string(c)
}

// patchSchema 改游戏内 AI 设置的参数（gameai.set、gameai.test 共用）。
func patchSchema() map[string]*ops.Schema {
	name := ops.String("玩家名").Len(1, 16)
	templates := map[string]*ops.Schema{}
	for _, t := range chatai.Templates {
		templates[t.ID] = ops.Boolean(t.Title + "：" + t.Usage)
	}
	return map[string]*ops.Schema{
		"enabled":    ops.Boolean("打开游戏内 AI"),
		"prefix":     ops.String("触发前缀，默认 @AI").Len(1, 16),
		"aliases":    ops.Array("前缀的别名，如 @小助手", ops.String("别名").Len(0, 16)).MaxLen(8),
		"allowed":    ops.Array("能让 AI 执行命令的玩家", name).MaxLen(200),
		"templates":  ops.Object(templates),
		"cooldown":   ops.Integer("每人冷却（秒），0 = 不冷却").Range(0, 3600),
		"perPlayer":  ops.Integer("每人每天几次，0 = 不限").Range(0, 100000),
		"perServer":  ops.Integer("全服每天几次，0 = 不限").Range(0, 1000000),
		"budget":     ops.Integer("全服每天多少 token，0 = 不限").Range(0, 1e11),
		"looseNames": ops.Boolean("放宽名字规则（离线服的名字带 - . 等字符时打开）"),
		"anyThread":  ops.Boolean("不看线程名"),
		"pattern":    ops.String("自定义正则：命名分组 name 是名字、msg 是聊天内容；空串用内置规则").Len(0, 500),
	}
}

type gameaiTestArgs struct {
	Inst string `json:"inst"`
	Line string `json:"line"`
	chatai.Patch
}

type gameaiWhitelistArgs struct {
	Inst    string   `json:"inst"`
	Players []string `json:"players"`
}

// gameErr 游戏内 AI 那边的错误：实例不存在按 404，其余是参数或设置的问题（400）。
func (c *cat) gameErr(name string, err error) error {
	if _, e := c.Mgr.Get(name); e != nil {
		return ops.Wrap(404, e)
	}
	return err
}

func (c *cat) gameChatOps() {
	if c.GameChat == nil {
		return
	}
	const localMsg = "游戏内 AI 的设置仅支持在本机修改（含能让 AI 执行命令的玩家和命令模板）"
	view := func(caller ops.Caller, name string) (any, error) {
		v, err := c.GameChat.View(name)
		if err != nil {
			return nil, c.gameErr(name, err)
		}
		v.Local = caller.Local
		return v, nil
	}

	c.reg.Register(&ops.Op{
		Name: "game.info", Title: "查看服务器的基本情况（游戏内 AI 用）", Level: ops.Read, InGame: true,
		Desc: "游戏内 AI 唯一能调用的操作（§6.4）：核心、Minecraft 版本、加载器版本、模组和插件的数量、在线玩家、人数上限、" +
			"服务器的本地时间。不含日志、配置文件和别的聊天。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			maxP := 20
			if pr, err := inst.LoadProps(i.PropsPath()); err == nil {
				if v, ok := pr.Get("max-players"); ok {
					if n, e := strconv.Atoi(strings.TrimSpace(v)); e == nil && n > 0 {
						maxP = n
					}
				}
			}
			now := time.Now()
			online := i.OnlinePlayers()
			if online == nil {
				online = []string{}
			}
			return gameInfo{Core: coreDisplayName(i.Core), MC: i.MC, Loader: i.Build, Mods: countJars(filepath.Join(i.Dir, "mods")),
				Plugins: countJars(filepath.Join(i.Dir, "plugins")), Status: i.Status(), Online: online, MaxPlayers: maxP,
				Time: now.Format("2006-01-02 15:04") + "（" + weekdays[now.Weekday()] + "）"}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "gameai.get", Title: "查看游戏内 AI 的设置", Level: ops.HumanOnly, NoLog: true,
		Desc:   "实例「游戏内 AI」标签的内容：开关、触发前缀、能执行命令的玩家（和可以授权的人）、命令模板、限流和额度、今天的用量、白名单开没开。",
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
		Name: "gameai.set", Title: "修改游戏内 AI 的设置", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc:   "只改传了的字段；授权名单、别名整体替换，命令模板只改列出的。",
		Params: withInst(patchSchema()),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst string `json:"inst"`
				chatai.Patch
			}](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(p.Inst); err != nil {
				return nil, err
			}
			if _, err := c.GameChat.Update(p.Inst, p.Patch); err != nil {
				return nil, c.gameErr(p.Inst, err)
			}
			return view(caller, p.Inst)
		},
	})

	props := patchSchema()
	props["line"] = ops.String("一行服务器日志").Len(0, 4000)
	c.reg.Register(&ops.Op{
		Name: "gameai.test", Title: "游戏内 AI 的识别测试", Level: ops.HumanOnly, NoLog: true,
		Desc:   "贴一行日志进去，按设置（可以带上还没保存的改动）显示认出的玩家、正文、选项和问题，以及会怎么处理。不发任何东西。",
		Params: withInst(props, "line"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[gameaiTestArgs](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(p.Inst); err != nil {
				return nil, err
			}
			r, err := c.GameChat.Test(p.Inst, p.Line, &p.Patch)
			if err != nil {
				return nil, c.gameErr(p.Inst, err)
			}
			return r, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "gameai.log", Title: "查看游戏内 AI 最近的问答", Level: ops.HumanOnly, NoLog: true,
		Desc:   "最近的问答和执行记录（新的在前，最多 100 条）：谁问的、用了哪些选项、完整的回答、执行了哪些命令和结果、用量。",
		Params: withInst(map[string]*ops.Schema{"limit": ops.Integer("最多几条，默认 50").Range(1, 100)}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Inst  string `json:"inst"`
				Limit int    `json:"limit"`
			}](a)
			if err != nil {
				return nil, err
			}
			if _, err := c.get(p.Inst); err != nil {
				return nil, err
			}
			if p.Limit == 0 {
				p.Limit = 50
			}
			return c.GameChat.History(p.Inst, p.Limit)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "gameai.whitelist", Title: "一键开启白名单", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc: "授权玩家让 AI 执行命令时，服务器没开白名单的话用它（§6.5）：先把勾选的人加进白名单，再开白名单，免得把好友关在外面。" +
			"运行中走控制台命令（whitelist add、whitelist on），发 whitelist on 之前先把 server.properties 的 white-list 改成 true；停着时直接改名单文件和 server.properties。",
		Params: withInst(map[string]*ops.Schema{
			"players": ops.Array("先加进白名单的人", ops.String("玩家名，1–16 位字母、数字、下划线").Len(1, 16)).MaxLen(100),
		}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[gameaiWhitelistArgs](a)
			if err != nil {
				return nil, err
			}
			i, err := c.get(p.Inst)
			if err != nil {
				return nil, err
			}
			for _, n := range p.Players {
				if !mclog.ValidName(n) {
					return nil, ops.Errorf(400, "「%s」加不进白名单：玩家名只能是 1–16 位字母、数字、下划线", n)
				}
			}
			pl, err := c.Mgr.GetPlayers(p.Inst)
			if err != nil {
				return nil, err
			}
			listed := map[string]bool{}
			for _, e := range pl.Whitelist {
				listed[strings.ToLower(e.Name)] = true
			}
			for _, n := range p.Players {
				if listed[strings.ToLower(n)] {
					continue
				}
				if err := c.Mgr.PlayerAction(p.Inst, "whitelist-add", n); err != nil {
					return nil, fmt.Errorf("把 %s 加进白名单失败：%w", n, err)
				}
				listed[strings.ToLower(n)] = true
			}
			// 先改 server.properties 再发 whitelist on：服务器执行 whitelist on 时自己也会写这个文件，先后错开，不会两边同时写
			props, err := inst.LoadProps(i.PropsPath())
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			props.Set("white-list", "true")
			if err := props.Save(i.PropsPath()); err != nil {
				return nil, ops.Wrap(500, errors.New("改 server.properties 失败："+err.Error()))
			}
			if i.Status() == "running" {
				if err := c.Mgr.Command(p.Inst, "whitelist on"); err != nil {
					return nil, err
				}
			}
			return view(caller, p.Inst)
		},
	})
}
