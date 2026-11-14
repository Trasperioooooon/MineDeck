package catalog

import (
	"context"
	"strings"

	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// 穿透平台账户（OpenFrp / SakuraFrp）：连接、概览、签到和购买的官网入口、在平台创建/导入隧道。
// 全部 HumanOnly：凭据与支付相关，不给 AI。会用到保存的平台凭据、或者会改它的操作都是仅本机：
// 局域网里知道访问密码的设备也不能看账户概览、不能替这台电脑连接或断开账户。

func (c *cat) accountOps() {
	if c.Accounts == nil {
		return
	}
	providerField := ops.String("穿透平台：openfrp、natfrp（樱花）")
	providerOnly := ops.Object(map[string]*ops.Schema{"provider": providerField}, "provider")
	const localMsg = "穿透平台账户只能在运行 MineDeck 的这台电脑上操作"
	c.Accounts.SetGuardPort(c.Port) // 创建、导入时拒绝指向 MineDeck 自己网页端口的隧道

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.status", Title: "查看穿透账户连接状态", Level: ops.HumanOnly, NoLog: true,
		Desc: "是否已保存 OpenFrp Authorization / SakuraFrp 访问密钥（不含明文），以及调用方是否在本机（局域网打开时界面只读）。",
		Run: func(_ context.Context, caller ops.Caller, _ ops.Args) (any, error) {
			return c.Accounts.Status(caller.Local), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.openfrp.login", Title: "开始 OpenFrp 远程授权", Level: ops.HumanOnly, LocalOnly: true,
		Desc: "生成授权链接，用户在浏览器完成授权后轮询拿 Authorization。",
		Run: func(ctx context.Context, _ ops.Caller, _ ops.Args) (any, error) {
			return c.Accounts.BeginOpenFrpLogin(ctx)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.openfrp.poll", Title: "轮询 OpenFrp 授权结果", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		Params: ops.Object(map[string]*ops.Schema{"id": ops.String("BeginOpenFrpLogin 返回的 request UUID")}, "id"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ID string `json:"id"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.PollOpenFrpLogin(ctx, strings.TrimSpace(p.ID))
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.openfrp.cancel", Title: "取消 OpenFrp 远程授权", Level: ops.HumanOnly, LocalOnly: true, NoLog: true,
		Params: ops.Object(map[string]*ops.Schema{"id": ops.String("BeginOpenFrpLogin 返回的 request UUID")}, "id"),
		Desc:   "用户点了「取消等待」：这次授权作废，之后即使在途的轮询拿到授权也不保存。",
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ID string `json:"id"`
			}](a)
			if err != nil {
				return nil, err
			}
			// cancelled=false：这次授权已经不在等了（多半刚好授权成功），界面要重新查连接状态
			return map[string]bool{"cancelled": c.Accounts.CancelOpenFrpLogin(strings.TrimSpace(p.ID))}, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.natfrp.connect", Title: "连接 SakuraFrp 账户", Level: ops.HumanOnly, LocalOnly: true,
		Desc: "校验并保存 SakuraFrp 访问密钥。",
		Params: ops.Object(map[string]*ops.Schema{
			"token": ops.String("SakuraFrp 访问密钥（只写不读）"),
		}, "token"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Token string `json:"token"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.ConnectSakura(ctx, p.Token)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.disconnect", Title: "断开穿透账户", Level: ops.HumanOnly, LocalOnly: true, Params: providerOnly,
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := c.Accounts.Disconnect(p.Provider); err != nil {
				return nil, err
			}
			return "ok", nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.overview", Title: "查看穿透账户概览", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg, NoLog: true, Params: providerOnly,
		Desc: "账户信息、可用节点、平台上的隧道列表（不含 frpc 密钥明文）。会用本机保存的凭据向平台发请求，结果缓存 30 秒。",
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.Overview(ctx, p.Provider)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.sign", Title: "签到入口", Level: ops.HumanOnly, NoLog: true, Params: providerOnly,
		Desc: "返回官网签到页地址，由用户在浏览器里自己签。不发任何签到请求：OpenFrp 要过控制台的人机验证且禁止自动签到，樱花只认官网登录会话。",
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.Sign(p.Provider)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.purchase", Title: "查看购买入口", Level: ops.HumanOnly, NoLog: true, Params: providerOnly,
		Desc: "返回官方商店/充值页 URL，供前端打开；不代下单。",
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.PurchaseOptions(p.Provider)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.create", Title: "在穿透平台创建隧道并导入", Level: ops.HumanOnly, LocalOnly: true,
		Params: ops.Object(map[string]*ops.Schema{
			"provider":      providerField,
			"name":          ops.String("隧道名称"),
			"nodeId":        ops.Integer("节点编号"),
			"type":          ops.String("tcp 或 udp"),
			"localPort":     ops.Integer("本地端口"),
			"remotePort":    ops.Integer("公网端口（OpenFrp 必填；Sakura 可选）"),
			"boundInstance": ops.String("绑定的实例名"),
			"autoStart":     ops.Boolean("实例启动时跟随启动"),
			"purpose":       ops.String("用途：game 或 voice"),
			"publicServer":  ops.Boolean("樱花：是否公开游戏服务器（只能用名称含「多线」或带公开游戏标记的节点）"),
		}, "provider", "name", "nodeId", "type", "localPort"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			o, err := bind[tunnel.AccountTunnelOptions](a)
			if err != nil {
				return nil, err
			}
			// 指向 MineDeck 自己网页端口的隧道由账户层拒绝（SetGuardPort）：映射到公网后经 frpc 进来的请求都来自本机地址
			return c.Accounts.CreateTunnel(ctx, o)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.import", Title: "从穿透平台导入已有隧道", Level: ops.HumanOnly, LocalOnly: true,
		Params: ops.Object(map[string]*ops.Schema{
			"provider":      providerField,
			"proxyId":       ops.String("平台上的隧道编号"),
			"boundInstance": ops.String("绑定的实例名"),
			"autoStart":     ops.Boolean("实例启动时跟随启动"),
			"purpose":       ops.String("用途：game 或 voice"),
		}, "provider", "proxyId"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			o, err := bind[tunnel.AccountTunnelImport](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.ImportTunnel(ctx, o)
		},
	})
}
