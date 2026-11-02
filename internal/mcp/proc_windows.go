package mcp

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// processAlive 运行时文件里的进程还在运行，而且是 base 目录下的程序（防止 MineDeck 崩溃后进程号被别的程序用了，
// 端口也被别的程序占了时，把令牌发给它）。
func processAlive(pid int, base string) error {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("进程 %d 不在了", pid)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != 259 { // 259 = STILL_ACTIVE
		return fmt.Errorf("进程 %d 已经退出", pid)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return fmt.Errorf("查不到进程 %d 的程序路径：%v", pid, err)
	}
	exe := windows.UTF16ToString(buf[:n])
	if !strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(base)) {
		return fmt.Errorf("进程 %d 不是这个目录下的 MineDeck（%s）", pid, exe)
	}
	return nil
}
