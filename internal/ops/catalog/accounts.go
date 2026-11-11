package catalog

import (
	"context"
	"strings"

	"minedeck/internal/ops"
	"minedeck/internal/tunnel"
)

// 穿透平台账户（OpenFrp / SakuraFrp）：连接、概览、手动签到、购买跳转、在平台创建/导入隧道。
// 全部 HumanOnly：凭据与支付相关，不给 AI。

func (c *cat) accountOps() {
	if c.Accounts == nil {
		return
	}
	providerField := ops.String("穿透平台：openfrp、natfrp（樱花）")
	providerOnly := ops.Object(map[string]*ops.Schema{"provider": providerField}, "provider")

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.status", Title: "查看穿透账户连接状态", Level: ops.HumanOnly, NoLog: true,
		Desc: "是否已保存 OpenFrp Authorization / SakuraFrp 访问密钥（不含明文）。",
		Run:  func(context.Context, ops.Caller, ops.Args) (any, error) { return c.Accounts.Status(), nil },
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
		Name: "tunnel.account.weblogin", Title: "获取穿透平台网页登录入口", Level: ops.HumanOnly, NoLog: true, Params: providerOnly,
		Desc: "Sakura：返回面板 /cgi/user/login（会跳转 OpenID）；登录后仍需粘贴访问密钥。OpenFrp：账户中心链接。",
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.WebLogin(p.Provider)
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
		Name: "tunnel.account.overview", Title: "查看穿透账户概览", Level: ops.HumanOnly, NoLog: true, Params: providerOnly,
		Desc: "账户信息、可用节点、平台上的隧道列表（不含 frpc 密钥明文）。",
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
		Name: "tunnel.account.captcha", Title: "获取签到验证码配置", Level: ops.HumanOnly, NoLog: true, Params: providerOnly,
		Desc: "OpenFrp 返回 CaptchaSDK 的 appId/sdkUrl；Sakura 标明需 SESSION。禁止自动签到。",
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.CaptchaInfo(p.Provider)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.sign", Title: "手动签到", Level: ops.HumanOnly, LocalOnly: true, Params: ops.Object(map[string]*ops.Schema{
			"provider": providerField,
			"token":    ops.String("OpenFrp CaptchaSDK.open() 得到的 token；Sakura 暂不使用"),
		}, "provider"),
		Desc: "仅用户手动触发。OpenFrp 带人机验证 token；Sakura 访问密钥不可用时返回官网引导。禁止自动签到。",
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
				Token    string `json:"token"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.Sign(ctx, p.Provider, tunnel.SignPayload{Token: p.Token})
		},
	})

	c.reg.Register(&ops.Op{
		Name: "tunnel.account.purchase", Title: "查看购买入口", Level: ops.HumanOnly, NoLog: true, Params: providerOnly,
		Desc: "返回官方商店/充值页 URL，供前端打开；不代下单。",
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				Provider string `json:"provider"`
			}](a)
			if err != nil {
				return nil, err
			}
			return c.Accounts.PurchaseOptions(ctx, p.Provider)
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
			"publicServer":  ops.Boolean("Sakura 公开游戏服（仅多线/公开服节点）"),
		}, "provider", "name", "nodeId", "type", "localPort"),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			o, err := bind[tunnel.AccountTunnelOptions](a)
			if err != nil {
				return nil, err
			}
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
