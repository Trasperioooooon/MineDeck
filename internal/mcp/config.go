package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"minedeck/internal/ops"
)

// Snippet 一种客户端的配置：粘贴到哪儿、粘贴什么。
type Snippet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"` // stdio / http
	Where     string `json:"where"`
	Text      string `json:"text"`
}

// Config 复制配置用（只给本机的界面）：地址、令牌明文、MineDeck.exe 的路径和各家客户端的配置。
type Config struct {
	URL      string    `json:"url"`
	Token    string    `json:"token"`
	Exe      string    `json:"exe"`
	Snippets []Snippet `json:"snippets"`
}

// Config 见 Config 类型。MCP 接口要先打开（打开时才有令牌）。
func (s *Service) Config() (*Config, error) {
	if !s.Enabled() {
		return nil, ops.Errorf(400, "先打开 MCP 接口")
	}
	tok, err := s.Token()
	switch {
	case err != nil:
		return nil, ops.Errorf(400, "MCP 令牌解不开（可能换了电脑或 Windows 用户），请先重置令牌")
	case tok == "":
		return nil, ops.Errorf(400, "还没有 MCP 令牌：关掉 MCP 接口再打开一次，或者重置令牌")
	}
	url := s.URL()
	c := &Config{URL: url, Token: tok, Exe: s.exe}
	auth := map[string]string{"Authorization": "Bearer " + tok}
	type stdioEntry struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	type httpEntry struct {
		Type    string            `json:"type,omitempty"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	wrap := func(key string, v any) string { return pretty(map[string]any{key: map[string]any{"minedeck": v}}) }
	if s.exe != "" {
		c.Snippets = append(c.Snippets, Snippet{ID: "claude", Name: "Claude Desktop", Transport: "stdio",
			Where: "Claude Desktop：「设置 → 开发者 → 编辑配置」打开 claude_desktop_config.json，把 minedeck 这一项合并进 mcpServers，保存后完全退出 Claude Desktop 再打开。stdio 方式不用填令牌，MineDeck 换了端口也不受影响。",
			Text:  wrap("mcpServers", stdioEntry{Command: s.exe, Args: []string{"-mcp"}})})
	}
	c.Snippets = append(c.Snippets,
		Snippet{ID: "cherry", Name: "Cherry Studio", Transport: "http",
			Where: "Cherry Studio：「设置 → MCP 服务器 → 添加 → 从 JSON 导入」，粘贴后确定。",
			Text:  wrap("mcpServers", httpEntry{Type: "streamableHttp", URL: url, Headers: auth})},
		Snippet{ID: "cursor", Name: "Cursor", Transport: "http",
			Where: "Cursor：在 Cursor 的设置里打开 MCP，添加服务器会打开 mcp.json（全局的在用户目录的 .cursor\\mcp.json），把 minedeck 合并进 mcpServers。",
			Text:  wrap("mcpServers", httpEntry{URL: url, Headers: auth})},
		Snippet{ID: "vscode", Name: "VS Code", Transport: "http",
			Where: "VS Code：命令面板里运行「MCP: Open User Configuration」打开 mcp.json，把 minedeck 合并进 servers。",
			Text:  wrap("servers", httpEntry{Type: "http", URL: url, Headers: auth})},
		Snippet{ID: "http", Name: "其他（HTTP）", Transport: "http",
			Where: "其他支持 Streamable HTTP 的客户端：按下面填地址和请求头。",
			Text:  fmt.Sprintf("地址：%s\n请求头：Authorization: Bearer %s", url, tok)},
	)
	if s.exe != "" {
		c.Snippets = append(c.Snippets, Snippet{ID: "stdio", Name: "其他（stdio）", Transport: "stdio",
			Where: "其他支持 stdio 的客户端：命令填 MineDeck.exe 的完整路径，参数填 -mcp。",
			Text:  fmt.Sprintf("命令：%s\n参数：-mcp", s.exe)})
	}
	return c, nil
}

// pretty 两格缩进的 JSON，不转义 < > &（路径和地址原样好读）。
func pretty(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return strings.TrimRight(buf.String(), "\n")
}
