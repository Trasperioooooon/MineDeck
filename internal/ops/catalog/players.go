package catalog

import (
	"context"
	"fmt"

	"minedeck/internal/ops"
)

// PlayerOps 网页接口 POST /api/instances/{name}/players 的 action → 操作名。
// 拆成几个操作是因为风险不同：加减白名单是写操作，op、踢人、封禁这些是危险操作（设计文档 §5.2）。
var PlayerOps = map[string]string{
	"whitelist-add":    "instance.whitelist.add",
	"whitelist-remove": "instance.whitelist.remove",
	"op":               "instance.players.op",
	"deop":             "instance.players.deop",
	"kick":             "instance.players.kick",
	"ban":              "instance.players.ban",
	"pardon":           "instance.players.pardon",
}

type playerArgs struct {
	Inst   string `json:"inst"`
	Player string `json:"player"`
}

func (c *cat) playerOps() {
	c.reg.Register(&ops.Op{
		Name: "instance.players.get", Title: "查看玩家名单", Level: ops.Read,
		Desc:   "在线玩家，以及白名单、管理员、封禁三份名单。",
		Params: withInst(nil),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[instArgs](a)
			if err != nil {
				return nil, err
			}
			return c.Mgr.GetPlayers(p.Inst)
		},
	})

	actions := []struct {
		action, title string
		level         ops.Level
		summary       string // 第一个 %s 是玩家名，第二个是实例名
	}{
		{"whitelist-add", "加入白名单", ops.Write, "把 %s 加进「%s」的白名单"},
		{"whitelist-remove", "移出白名单", ops.Write, "把 %s 移出「%s」的白名单"},
		{"op", "设为管理员", ops.Danger, "把 %s 设为「%s」的管理员（op）"},
		{"deop", "取消管理员", ops.Danger, "取消 %s 在「%s」的管理员身份"},
		{"kick", "踢出玩家", ops.Danger, "把 %s 踢出「%s」"},
		{"ban", "封禁玩家", ops.Danger, "封禁 %s（「%s」）"},
		{"pardon", "解除封禁", ops.Danger, "解除 %s 在「%s」的封禁"},
	}
	for _, act := range actions {
		act := act
		c.reg.Register(&ops.Op{
			Name: PlayerOps[act.action], Title: act.title, Level: act.level,
			Desc:   "服务器运行中走控制台命令；停止时直接改名单文件（踢人要求运行中）。",
			Params: withInst(map[string]*ops.Schema{"player": ops.String("玩家名，1–16 位字母、数字、下划线")}, "player"),
			Preview: func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
				p, err := bind[playerArgs](a)
				if err != nil {
					return nil, err
				}
				_, pre, err := c.statusPre(p.Inst)
				if err != nil {
					return nil, err
				}
				return &ops.Plan{Summary: fmt.Sprintf(act.summary, p.Player, p.Inst), Pre: pre}, nil
			},
			Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
				p, err := bind[playerArgs](a)
				if err != nil {
					return nil, err
				}
				if err := c.Mgr.PlayerAction(p.Inst, act.action, p.Player); err != nil {
					return nil, err
				}
				return "ok", nil
			},
		})
	}
}
