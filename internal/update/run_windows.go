package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// swapScript 是换 exe 的批处理脚本：等旧进程退出，把 MineDeck.new.exe 换上去，再把程序打开。
//
// 3.1.0 和以前的版本把路径直接写进脚本，实测有三种情况换不成：
//   - 脚本按 UTF-8 写盘，cmd 却按系统代码页读（简体中文系统是 936），中文路径会走样，cd 进不去；
//     开机自启起来的程序当前目录在 System32，接下来 move、start 都找不到文件，程序关掉以后就不再打开。
//   - 文件夹名字里的 % 会被 cmd 当成变量吃掉，后果同上。
//   - tasklist、find、ping 没写全路径，PATH 里排在前面的同名程序（比如 Git 自带的 GNU find）会顶替它们，
//     等旧进程退出的那一步直接跳过，exe 还被占着就去换，换不成。
//
// 所以现在脚本里只有 ASCII：路径、进程号、System32 的位置都放在环境变量里传进来，cmd 展开变量时
// 不经过代码页，也不会把展开出来的 % 再展开一遍；不靠当前目录，一律用全路径；系统命令写全路径。
// move、start、set、if、del 是 cmd 自己的命令，不查 PATH。
//
// 等旧进程最多 MD_UPDATE_MAXWAITS 轮（每轮约一秒；进程号要是被别的程序接着用了，不能一直等下去）；
// 换 exe 换不动就隔一秒再试，最多 MD_UPDATE_MAXTRIES 次（旧进程刚退出、杀毒软件正在扫新 exe 时会这样）。
// 实在换不了：旧进程要是还在，就什么都不打开，免得同时开着两个；旧进程已经不在了，就把旧版重新打开。
// 新版留在 MineDeck.new.exe，下次点更新会重新下。
//
// 打开程序那一行先清掉这些环境变量再 start：cmd 先把整行的 %变量% 展开完再执行，start 拿到的还是原来的值，
// 打开的 MineDeck 就不会带着它们。start 之前先看 exe 在不在：start 找不到文件会弹出错提示，
// 旧版本起脚本时隐藏了窗口，这个提示也跟着看不见，cmd 会一直卡在那里（用 v3.0.0 实测过）。
const swapScript = `@echo off
setlocal DisableDelayedExpansion
set /a MD_UPDATE_WAITS=0
:wait
"%MD_UPDATE_SYS%\tasklist.exe" /FI "PID eq %MD_UPDATE_PID%" /NH 2>nul | "%MD_UPDATE_SYS%\find.exe" "%MD_UPDATE_PID%" >nul || goto swap
set /a MD_UPDATE_WAITS+=1
if %MD_UPDATE_WAITS% lss %MD_UPDATE_MAXWAITS% ("%MD_UPDATE_SYS%\ping.exe" -n 2 127.0.0.1 >nul & goto wait)
:swap
set /a MD_UPDATE_TRIES=0
:move
move /y "%MD_UPDATE_NEW%" "%MD_UPDATE_EXE%" >nul 2>&1 && goto run
set /a MD_UPDATE_TRIES+=1
if %MD_UPDATE_TRIES% lss %MD_UPDATE_MAXTRIES% ("%MD_UPDATE_SYS%\ping.exe" -n 2 127.0.0.1 >nul & goto move)
"%MD_UPDATE_SYS%\tasklist.exe" /FI "PID eq %MD_UPDATE_PID%" /NH 2>nul | "%MD_UPDATE_SYS%\find.exe" "%MD_UPDATE_PID%" >nul && goto done
:run
set "MD_UPDATE_PID=" & set "MD_UPDATE_NEW=" & set "MD_UPDATE_EXE=" & set "MD_UPDATE_DIR=" & set "MD_UPDATE_BAT=" & set "MD_UPDATE_SYS=" & set "MD_UPDATE_WAITS=" & set "MD_UPDATE_TRIES=" & set "MD_UPDATE_MAXWAITS=" & set "MD_UPDATE_MAXTRIES=" & if exist "%MD_UPDATE_EXE%" start "" /D "%MD_UPDATE_DIR%" "%MD_UPDATE_EXE%"
:done
del /f /q "%~f0" >nul 2>&1
`

// 脚本等旧进程退出的轮数、换 exe 的次数，每轮、每次约一秒。测试里改小，免得要等好几分钟。
var (
	swapMaxWaits = 120
	swapMaxTries = 60
)

// startSwap 在 dir 里写出换 exe 的脚本，在后台跑起来就返回，不等它。
// exe 是要换掉的程序，newExe 是下载好、校验过的新版本，pid 是要等它退出的进程（正常就是本进程）。
// 换好以后程序在 dir 里打开（dir 是数据所在的文件夹，一般就是 exe 所在的文件夹）。
func startSwap(dir, exe, newExe string, pid int) error {
	bat := filepath.Join(dir, "update.bat")
	_ = os.Chmod(bat, 0o644) // 以前留下的 update.bat 要是被设成了只读，先去掉只读，不然写不进去
	// cmd 读 LF 换行的脚本时，goto 找标签偶尔会找不到，所以写成 CRLF
	if err := os.WriteFile(bat, []byte(strings.ReplaceAll(swapScript, "\n", "\r\n")), 0o644); err != nil {
		return err
	}
	sys, err := windows.GetSystemDirectory()
	if err != nil || sys == "" {
		sys = filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	cmdExe := filepath.Join(sys, "cmd.exe")
	c := exec.Command(cmdExe)
	// 命令行自己拼：/d 不跑注册表里给 cmd 设的 AutoRun，/e:on /v:off 不受注册表里命令扩展、延迟展开设置的影响；
	// /s 让 cmd 只剥掉最外面那对引号，脚本路径从环境变量展开后原样留在里面那对引号里（路径里有 &、空格也没事）。
	c.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + cmdExe + `" /d /e:on /v:off /s /c ""%MD_UPDATE_BAT%""`,
		// 只要不开控制台窗口。不设 HideWindow：万一 start 弹了出错提示，要让人看得见，而不是藏起来卡住
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	c.Env = append(os.Environ(),
		"MD_UPDATE_PID="+strconv.Itoa(pid),
		"MD_UPDATE_EXE="+exe,
		"MD_UPDATE_NEW="+newExe,
		"MD_UPDATE_DIR="+dir,
		"MD_UPDATE_BAT="+bat,
		"MD_UPDATE_SYS="+sys,
		"MD_UPDATE_MAXWAITS="+strconv.Itoa(swapMaxWaits),
		"MD_UPDATE_MAXTRIES="+strconv.Itoa(swapMaxTries),
	)
	// 不加入 Job Object：脚本要在本进程退出以后接着跑
	return c.Start()
}
