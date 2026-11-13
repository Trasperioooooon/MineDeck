package catalog

import (
	"context"
	"errors"

	"minedeck/internal/ai"
	"minedeck/internal/ops"
)

// aiOps AI 设置（设计文档 §4.5、§10.3）。全部是「仅人工」：AI 永远看不到、也改不了自己的设置和 Key。
// 修改类的都「仅本机」（含 Key 和接口地址）；局域网登录的设备只能看。Key 只写不读：任何响应都只给 keySet。
func (c *cat) aiOps() {
	const localMsg = "AI 设置仅支持在本机修改（含 Key 和接口地址）"
	presetIDs := []string{""}
	for _, p := range ai.Presets() {
		presetIDs = append(presetIDs, p.ID)
	}
	view := func(caller ops.Caller) ai.View {
		v := c.AI.View()
		v.Local = caller.Local
		return v
	}
	price := ops.Number("每百万 token 的单价").Range(0, 100000)

	c.reg.Register(&ops.Op{
		Name: "ai.settings.get", Title: "查看 AI 设置", Level: ops.HumanOnly, NoLog: true,
		Desc: "服务商预设、当前的服务商设置（不含 Key，只说是否已设置）、是否已启用、每日额度、今天和本月的用量、价格数据的状态和自填单价。不联网。",
		Run:  func(_ context.Context, caller ops.Caller, _ ops.Args) (any, error) { return view(caller), nil },
	})

	c.reg.Register(&ops.Op{
		Name: "ai.provider.set", Title: "修改 AI 服务商设置", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc: "只改传了的字段。换了服务商，或者接口地址换到了别的网站（http/https、域名、端口有一样不同），旧 Key 自动清掉、必须重填；同一个网站只改路径或协议，Key 照旧。preset 传空串表示停用 AI。",
		Params: ops.Object(map[string]*ops.Schema{
			"preset":    ops.String("服务商预设的编号；空串停用").OneOf(presetIDs...),
			"protocol":  ops.String("协议（只有自定义时可选）").OneOf("openai", "anthropic"),
			"baseUrl":   ops.String("接口地址").Len(0, 500),
			"model":     ops.String("模型名").Len(0, 200),
			"maxOutput": ops.Integer("模型的输出上限（模型列表给的）").Range(0, 10000000),
			"effort":    ops.String("Claude 的思考深度；空串用模型默认").OneOf("", "low", "medium", "high", "xhigh", "max"),
			"key":       ops.String("API Key（只写不读；空串清除）").Len(0, 4096),
			"auth":      ops.String("自定义服务商的鉴权头").OneOf("bearer", "api-key", "both"),
			"echo":      ops.String("自定义服务商是否回传思考内容").OneOf("auto", "on", "off"),
		}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			pt, err := bind[ai.ProviderPatch](a)
			if err != nil {
				return nil, err
			}
			if err := c.AI.SetProvider(pt); err != nil {
				return nil, err
			}
			return view(caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "ai.models.list", Title: "获取 AI 模型列表", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg, NoLog: true,
		Desc: "向服务商要模型列表（要联网）。可以用还没保存的服务商和地址；不传 Key 时，只有服务商没换、地址还是同一个网站才用已存的 Key。返回 {models, source, warning}：source 为 live 或 preset；对方没有模型列表时用预设里的建议，并在 warning 里说明，仍可手填。",
		Params: ops.Object(map[string]*ops.Schema{
			"preset":   ops.String("服务商预设的编号").OneOf(presetIDs...),
			"protocol": ops.String("协议（只有自定义时可选）").OneOf("openai", "anthropic"),
			"baseUrl":  ops.String("接口地址").Len(0, 500),
			"key":      ops.String("还没保存的 API Key").Len(0, 4096),
			"auth":     ops.String("自定义服务商的鉴权头").OneOf("bearer", "api-key", "both"),
			"echo":     ops.String("自定义服务商是否回传思考内容").OneOf("auto", "on", "off"),
		}),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			req, err := bind[ai.ModelsRequest](a)
			if err != nil {
				return nil, err
			}
			ms, err := c.AI.Models(ctx, req)
			if err != nil {
				return nil, upstream(err)
			}
			if ms.Models == nil {
				ms.Models = []ai.Model{}
			}
			return ms, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "ai.test", Title: "测试 AI 连接", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg, NoLog: true,
		Desc: "按保存的设置发一个最小的请求（要联网，花一点点 token，不计入额度），报告延迟和能不能调用工具。",
		Run: func(ctx context.Context, _ ops.Caller, _ ops.Args) (any, error) {
			return c.AI.Test(ctx)
		},
	})

	c.reg.Register(&ops.Op{
		Name: "ai.quota.set", Title: "修改 AI 每日额度", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc: "按功能设每天最多用多少 token（输入、输出、缓存都算），0 表示不限。用完后这项功能暂停到第二天。",
		Params: ops.Object(map[string]*ops.Schema{
			"assistant": ops.Integer("应用内助手（局域网登录的设备和本机共用）").Range(0, 1e12),
			"gamechat":  ops.Integer("游戏内 AI").Range(0, 1e12),
			"companion": ops.Integer("AI 伙伴（玩家在聊天里叫着伙伴的名字说话时）").Range(0, 1e12),
		}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			q, err := bind[map[ai.Feature]int64](a)
			if err != nil {
				return nil, err
			}
			if err := c.AI.SetQuota(q); err != nil {
				return nil, err
			}
			return view(caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "ai.assistant.set", Title: "修改局域网能否使用 AI 助手", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc:   "局域网登录的设备能不能用应用内助手（§10.3，默认开）。关掉后局域网打开的界面里没有助手入口，也不能确认助手的提案。",
		Params: ops.Object(map[string]*ops.Schema{"lan": ops.Boolean("局域网登录的设备可以用 AI 助手")}, "lan"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			body, err := bind[struct {
				LAN bool `json:"lan"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := c.AI.SetLANAssistant(body.LAN); err != nil {
				return nil, ops.Wrap(500, err)
			}
			return view(caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "ai.prices.set", Title: "修改 AI 自填单价", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc: "整体替换自填的单价（每百万 token，元或美元）。自填的优先于公开价格，也能补上查不到的模型。",
		Params: ops.Object(map[string]*ops.Schema{
			"prices": ops.Array("自填单价", ops.Object(map[string]*ops.Schema{
				"model":      ops.String("模型名").Len(1, 200),
				"currency":   ops.String("币种").OneOf("CNY", "USD"),
				"input":      price,
				"output":     price,
				"cacheRead":  price,
				"cacheWrite": price,
			}, "model", "currency", "input", "output")).MaxLen(100),
		}, "prices"),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			body, err := bind[struct {
				Prices []ai.CustomPrice `json:"prices"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := c.AI.SetPrices(body.Prices); err != nil {
				return nil, err
			}
			return view(caller), nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "ai.prices.refresh", Title: "刷新 AI 公开价格", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg, NoLog: true,
		Desc: "马上重新下载 LiteLLM 的公开价格（经 GitHub 加速镜像）。没配置 AI 或只配了本机模型时不取。",
		Run: func(ctx context.Context, caller ops.Caller, _ ops.Args) (any, error) {
			if err := c.AI.RefreshPrices(ctx); err != nil {
				if errors.Is(err, ai.ErrDisabled) {
					return nil, err
				}
				return nil, ops.Wrap(502, err)
			}
			return view(caller), nil
		},
	})
}

// upstream 服务商那边的错误按 502 报，自己这边的参数问题按 400。
func upstream(err error) error {
	var ae *ai.APIError
	var ne *ai.NetError
	if errors.As(err, &ae) || errors.As(err, &ne) {
		return ops.Wrap(502, err)
	}
	return err
}
