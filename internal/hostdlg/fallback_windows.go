package hostdlg

// 后备：PowerShell 调 WinForms 的 FolderBrowserDialog（系统对话框那条路走不通时才用）。
// 和以前的写法差四处：
//   - 只给 CREATE_NO_WINDOW，不再冒黑色的终端窗口。不能连 STARTF_USESHOWWINDOW 一起给（procutil.HideWindow 那样）：
//     那是让进程的第一个窗口也藏起来，对话框就看不见了，这边会一直等下去。
//   - 对话框挂在一个置顶的隐形窗体上，不会被 MineDeck 的窗口盖在下面。
//   - 路径按 UTF-8 再转 Base64 传回来：没有控制台时 PowerShell 的输出按系统代码页编码，中文、日文路径会变成乱码。
//   - 标题走环境变量，不拼进命令：PowerShell 把弯引号（‘ ’）也当单引号，拼字符串的转义防不住；现在标题是写死的，
//     这样以后换成别的来源也不会变成注入。

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func pickFolderFallback(title string) (string, error) {
	ps := "Add-Type -AssemblyName System.Windows.Forms | Out-Null; " +
		"$o = New-Object System.Windows.Forms.Form; $o.TopMost = $true; " +
		"$f = New-Object System.Windows.Forms.FolderBrowserDialog; " +
		"$f.Description = $env:MINEDECK_DLG_TITLE; $f.ShowNewFolderButton = $true; " +
		"if ($f.ShowDialog($o) -eq [System.Windows.Forms.DialogResult]::OK) { " +
		"[Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($f.SelectedPath))) }"
	cmd := exec.Command("powershell", "-STA", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Env = append(os.Environ(), "MINEDECK_DLG_TITLE="+title)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w：%s", err, strings.TrimSpace(errOut.String()))
	}
	enc := strings.TrimSpace(out.String())
	if enc == "" {
		return "", nil // 取消
	}
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("PowerShell 返回的内容读不懂：%q", enc)
	}
	return string(b), nil
}
