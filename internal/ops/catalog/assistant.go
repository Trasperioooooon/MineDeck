package catalog

import (
	"net"
	"os"
	"path/filepath"

	"minedeck/internal/ai"
	"minedeck/internal/app"
	"minedeck/internal/assistant"
	"minedeck/internal/mcp"
	"minedeck/internal/ops"
)

// NewAssistant 按程序目录建应用内助手：会话存在 <程序目录>/data/ai/sessions/，和网页接口共用同一个注册表（提案存在里面）。
// token 是 MineDeck 这次启动的访问令牌：和各种 Key 一样，原样出现在要发给 AI 的内容里就换成 ***。
func NewAssistant(reg *ops.Registry, aiSvc *ai.Service, token string) *assistant.Service {
	return assistant.New(assistant.Options{Ops: reg, AI: aiSvc, Dir: filepath.Join(app.Base, "data", "ai", "sessions"), Redact: NewRedactor(token)})
}

// NewRedactor 发给 AI 之前的脱敏（设计文档 §5.3、§10.1），应用内助手、游戏内 AI 和 MCP 接口共用：路径里的 Windows 用户名、
// 程序目录、用户目录、计算机名和本机各网卡的地址换成占位符；MineDeck 的令牌（token）、CurseForge 和 AI 的 Key、
// MCP 令牌、访问密码的哈希原样出现就换成 ***。
func NewRedactor(token string) *assistant.Redactor {
	home, _ := os.UserHomeDir()
	return &assistant.Redactor{Base: app.Base, Home: home, User: os.Getenv("USERNAME"), Computer: os.Getenv("COMPUTERNAME"), IPs: allIPs(),
		Secrets: func() []string {
			out := []string{token, app.CFKey(), app.GetConfig().AccessPasswordHash}
			for _, name := range []string{ai.KeyName, mcp.KeyName} {
				if k, err := app.Secrets().Get(name); err == nil {
					out = append(out, k)
				}
			}
			return out
		}}
}

// allIPs 本机各网卡的地址（IPv4 和 IPv6，回环除外）。
func allIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}
