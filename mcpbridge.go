package main

import (
	"context"
	"os"
	"path/filepath"

	"minedeck/internal/app"
	"minedeck/internal/mcp"
	"minedeck/internal/secret"
)

// runMCPBridge MineDeck.exe -mcp：MCP 的 stdio 桥（设计文档 §15.3）。MCP 客户端（Claude Desktop 等）把它当子进程启动，
// 标准输入输出一行一条 JSON-RPC 消息，转给同一目录下运行着的 MineDeck 的 /mcp。端口从 data\mcp\runtime.json 读；
// 令牌从 secrets.json 用 DPAPI 解出来（和 MineDeck 是同一个 Windows 用户才解得开），每次请求都重新读，重置令牌后不用改配置。
//
// release 版用 -H windowsgui 编译（GUI 子系统，没有控制台窗口）：客户端用管道启动它时，标准输入输出照常可用。
// 不弹任何对话框，出错只写标准错误（客户端一般记进它的日志）。
func runMCPBridge() int {
	exe, err := os.Executable()
	if err != nil {
		os.Stderr.WriteString("MineDeck MCP：找不到程序自己的路径：" + err.Error() + "\n")
		return 1
	}
	base := filepath.Dir(exe)
	bridgeIdle()
	keys := secret.NewStore(filepath.Join(base, "secrets.json"))
	err = mcp.Bridge(context.Background(), mcp.BridgeOptions{
		Dir: mcp.DataDir(base), Base: base, In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Version: app.Version,
		Token: func() (string, error) { return keys.Get(mcp.KeyName) },
	})
	if err != nil {
		os.Stderr.WriteString("MineDeck MCP：" + err.Error() + "\n")
		return 1
	}
	return 0
}
