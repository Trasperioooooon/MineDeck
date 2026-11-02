//go:build !windows

package mcp

import (
	"fmt"
	"os"
	"syscall"
)

// processAlive MineDeck 只在 Windows 上运行；别的平台只为能编译和测试，只看进程还在不在。
func processAlive(pid int, _ string) error {
	p, err := os.FindProcess(pid)
	if err == nil {
		err = p.Signal(syscall.Signal(0))
	}
	if err != nil {
		return fmt.Errorf("进程 %d 不在了", pid)
	}
	return nil
}
