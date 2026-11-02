package catalog

import (
	"context"
	"fmt"
	"strconv"

	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// tunnelFields 新增、修改隧道的参数说明（凭据只写不读）。
var tunnelFields = map[string]*ops.Schema{
	"provider":      ops.String("服务商：openfrp、natfrp（樱花）、custom（自建 frps）"),
	"name":          ops.String("隧道名称"),
	"credential":    ops.String("OpenFrp 用户密钥 / 樱花访问密钥 / 自建 frps 的 token（只写不读；修改时留空表示不变）"),
	"proxyId":       ops.String("OpenFrp、樱花的隧道 ID"),
	"serverAddr":    ops.String("自建 frps 的地址"),
	"serverPort":    ops.Integer("自建 frps 的端口"),
	"remotePort":    ops.Integer("自建 frps 上的公网端口"),
	"localPort":     ops.Integer("本地端口（MC 服务器端口）"),
	"boundInstance": ops.String("绑定的实例名"),
	"autoStart":     ops.Boolean("绑定的实例启动时跟着启动"),
	"purpose":       ops.String("用途：game 游戏（默认）、voice 语音（Simple Voice Chat 的 UDP 隧道，公网地址会自动写进绑定实例的 voice_host）"),
	"proto":         ops.String("自定义 frps 的协议：tcp（默认）、udp（语音隧道用）"),
}

type tunnelArgs struct {
	ID string `json:"id"`
}

func (c *cat) tunnelState(id string) (tunnel.Status, error) {
	for _, st := range c.Tun.List() {
		if st.ID == id {
			return st, nil
		}
	}
	return tunnel.Status{}, ops.Errorf(404, "隧道不存在")
}

func (c *cat) tunnelOps() {
	c.reg.Register(&ops.Op{
		Name: "tunnel.list", Title: "查看隧道", Level: ops.Read,
		Desc: "穿透隧道的配置和状态（不含凭据）：服务商、绑定的实例、公网地址、是否运行、最近一次失败原因。",
		Run:  func(context.Context, ops.Caller, ops.Args) (any, error) { return c.Tun.List(), nil },
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.add", Title: "新增隧道", Level: ops.HumanOnly,
		Params: ops.Object(tunnelFields),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			t, err := bind[tunnel.Tunnel](a)
			if err != nil {
				return nil, err
			}
			return c.Tun.Add(t)
		},
	})

	updateFields := map[string]*ops.Schema{"id": ops.String("隧道编号")}
	for k, v := range tunnelFields {
		updateFields[k] = v
	}
	c.reg.Register(&ops.Op{
		Name: "tunnel.update", Title: "修改隧道", Level: ops.HumanOnly,
		Desc:   "整体替换隧道配置（运行中不能改）；凭据留空表示不修改。",
		Params: ops.Object(updateFields, "id"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			t, err := bind[tunnel.Tunnel](a) // 编号也在 Tunnel 的 id 字段里
			if err != nil {
				return nil, err
			}
			if err := c.Tun.Update(t.ID, t); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	idOnly := ops.Object(map[string]*ops.Schema{"id": ops.String("隧道编号（见 tunnel.list）")}, "id")
	c.reg.Register(&ops.Op{
		Name: "tunnel.delete", Title: "删除隧道", Level: ops.HumanOnly, Params: idOnly,
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[tunnelArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Tun.Delete(p.ID); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	preview := func(summary func(tunnel.Status) string, warning string) func(context.Context, ops.Caller, ops.Args) (*ops.Plan, error) {
		return func(_ context.Context, _ ops.Caller, a ops.Args) (*ops.Plan, error) {
			p, err := bind[tunnelArgs](a)
			if err != nil {
				return nil, err
			}
			st, err := c.tunnelState(p.ID)
			if err != nil {
				return nil, err
			}
			return &ops.Plan{Summary: summary(st), Warning: warning, Pre: map[string]string{"隧道状态": strconv.FormatBool(st.Running)}}, nil
		}
	}
	c.reg.Register(&ops.Op{
		Name: "tunnel.start", Title: "启动隧道", Level: ops.Write, Params: idOnly,
		Desc:    "启动这条隧道的 frpc，公网地址稍后显示在隧道列表里。",
		Preview: preview(func(st tunnel.Status) string { return fmt.Sprintf("启动隧道「%s」", st.Name) }, ""),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[tunnelArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Tun.Start(p.ID); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})
	c.reg.Register(&ops.Op{
		Name: "tunnel.stop", Title: "停止隧道", Level: ops.Danger, Params: idOnly,
		Preview: preview(func(st tunnel.Status) string { return fmt.Sprintf("停止隧道「%s」", st.Name) }, "通过这条隧道连进来的玩家会掉线"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[tunnelArgs](a)
			if err != nil {
				return nil, err
			}
			if err := c.Tun.Stop(p.ID); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})
}
