package catalog

import (
	"context"
	"os"

	"minedeck/internal/app"
	"minedeck/internal/mcp"
	"minedeck/internal/ops"
)

// NewMCP 按程序目录建 MCP 接口（设计文档 §15）：设置和运行时文件在 <程序目录>/data/mcp/，令牌用 DPAPI 加密存进
// secrets.json（键名 mcp.token）；外部 AI 的工具和提案用同一个注册表，脱敏同应用内助手。token 是 MineDeck 这次启动的
// 访问令牌：原样出现在要交给外部 AI 的内容里就换成 ***。
//
// 同时登记 MCP 接口的设置操作（mcp.*，都是仅人工；修改类的仅本机）：要用到 MCP 服务本身，所以不在 New 里登记。
func NewMCP(reg *ops.Registry, token string) *mcp.Service {
	exe, _ := os.Executable()
	svc := mcp.New(mcp.Options{Ops: reg, Dir: mcp.DataDir(app.Base), Keys: app.Secrets(), Redact: NewRedactor(token),
		Version: app.Version, Exe: exe})
	mcpOps(reg, svc)
	return svc
}

// mcpOps MCP 接口的设置。全部仅人工：外部 AI 和应用内助手都看不到、改不了。修改类的仅本机，局域网的设备只能看
// （看不到令牌，也拿不到带令牌的配置）。
func mcpOps(reg *ops.Registry, svc *mcp.Service) {
	const localMsg = "MCP 接口的设置仅支持在本机修改"
	view := func(c ops.Caller) any { return svc.Status(c.Local) }

	reg.Register(&ops.Op{
		Name: "mcp.get", Title: "查看 MCP 接口", Level: ops.HumanOnly, NoLog: true,
		Desc: "开关、接口地址、令牌是否已生成、本次启动以来连过的客户端、等确认的外部 AI 提案数。不含令牌。",
		Run:  func(_ context.Context, c ops.Caller, _ ops.Args) (any, error) { return view(c), nil },
	})

	reg.Register(&ops.Op{
		Name: "mcp.set", Title: "开关 MCP 接口", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc:   "打开或关闭 MCP 接口。第一次打开时生成令牌；关闭时还没处理的外部 AI 提案作废。",
		Params: ops.Object(map[string]*ops.Schema{"enabled": ops.Boolean("打开 MCP 接口")}, "enabled"),
		Run: func(_ context.Context, c ops.Caller, a ops.Args) (any, error) {
			body, err := bind[struct {
				Enabled bool `json:"enabled"`
			}](a)
			if err != nil {
				return nil, err
			}
			if err := svc.SetEnabled(body.Enabled); err != nil {
				return nil, ops.Wrap(500, err)
			}
			return view(c), nil
		},
	})

	reg.Register(&ops.Op{
		Name: "mcp.token.reset", Title: "重置 MCP 令牌", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc: "换一个新令牌：HTTP 方式连的客户端要换成新令牌；stdio 方式（MineDeck.exe -mcp）不用改。",
		Run: func(_ context.Context, c ops.Caller, _ ops.Args) (any, error) {
			if err := svc.ResetToken(); err != nil {
				return nil, ops.Wrap(500, err)
			}
			return view(c), nil
		},
	})

	reg.Register(&ops.Op{
		Name: "mcp.config", Title: "复制 MCP 配置", Level: ops.HumanOnly, LocalOnly: true, LocalMsg: localMsg,
		Desc: "各家客户端的配置（含令牌明文和 MineDeck.exe 的路径），给本机的界面复制用。",
		Run:  func(context.Context, ops.Caller, ops.Args) (any, error) { return svc.Config() },
	})
}
