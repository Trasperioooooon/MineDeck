// Package fakeserver 仅用于测试：让测试程序自身扮演 Minecraft 服务端子进程（与 internal/inst 和 internal/web 测试共用）。
package fakeserver

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Env 设了它为 "1" 时，测试程序在 TestMain 入口扮演服务器进程。
const Env = "MINEDECK_FAKE_SERVER"

// SaveEnv 控制收到 save-all flush 时的回显：world 回 "Saved the world"，silent 不回，默认回 "Saved the game"。
const SaveEnv = "MINEDECK_FAKE_SAVE"

// Run 打印启动完成行并读取标准输入响应 stop / opstop / crash / save-all flush / save-on，返回进程退出码。
func Run() int {
	fmt.Println(`[12:00:00] [Server thread/INFO]: Done (1.0s)! For help, type "help"`)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "stop":
			fmt.Println("[12:00:01] [Server thread/INFO]: Stopping the server")
			return 0
		case "opstop":
			fmt.Println("[12:00:01] [Server thread/INFO]: [Steve: Stopping the server]")
			return 0
		case "crash":
			return 0
		case "save-all flush":
			switch os.Getenv(SaveEnv) {
			case "world": // 1.12.2 及更早的说法
				fmt.Println("[12:00:01] [Server thread/INFO]: Saved the world")
			case "silent": // 保存得慢，迟迟不回确认
			default:
				fmt.Println("[12:00:01] [Server thread/INFO]: Saved the game")
			}
		case "save-on":
			fmt.Println("[12:00:01] [Server thread/INFO]: Automatic saving enabled")
		}
	}
	return 0
}
