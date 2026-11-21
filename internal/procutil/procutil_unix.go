//go:build !windows

package procutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// ---------- 子进程托管（Mac / 其他类 Unix） ----------
//
// Windows 上 InitJob 把 Java、frpc 放进一个 Job Object，MineDeck 被强行结束时系统连带结束它们。
// macOS 没有对应的机制（没有 Linux 的 PR_SET_PDEATHSIG）：MineDeck 正常退出时照常先停服务器和隧道
// （main 里的 ShutdownAll），被强退、崩溃时子进程会留下来。要补的话得另起一个看门进程或用 kqueue 盯父进程，
// 是 Mac 版后面阶段的事。这里两个函数什么都不做。

func InitJob() {}

func AssignToJob(*os.Process) {}

// HideWindow 类 Unix 上没有控制台窗口要藏。子进程放进自己的进程组：在终端里按 Ctrl-C 结束 MineDeck 时，
// 终端发的 SIGINT 不会同时打到 Java 上，关服仍由 MineDeck 按顺序发 stop（和 Windows 上的行为一致）。
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// OpenFolder 在访达（Finder）里打开一个文件夹。
func OpenFolder(dir string) error { return exec.Command("open", filepath.Clean(dir)).Start() }
