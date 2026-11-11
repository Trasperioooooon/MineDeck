package hostdlg

// 后备：PowerShell 调 WinForms 的 FolderBrowserDialog（系统对话框那条路走不通时才用）。
// 和以前的写法差三处：
//   - 只给 CREATE_NO_WINDOW，不再冒黑色的终端窗口。不能连 STARTF_USESHOWWINDOW 一起给（procutil.HideWindow 那样）：
//     那是让进程的第一个窗口也藏起来，对话框就看不见了，这边会一直等下去。
//   - 对话框挂在一个置顶的隐形窗体上，不会被 MineDeck 的窗口盖在下面。
//   - 路径按 UTF-8 再转 Base64 传回来：没有控制台时 PowerShell 的输出按系统代码页编码，中文、日文路径会变成乱码。

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// psQuote 写成 PowerShell 的单引号字符串（里面的单引号写两遍）。
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func pickFolderFallback(title string) (string, error) {
	ps := "Add-Type -AssemblyName System.Windows.Forms | Out-Null; " +
		"$o = New-Object System.Windows.Forms.Form; $o.TopMost = $true; " +
		"$f = New-Object System.Windows.Forms.FolderBrowserDialog; " +
		"$f.Description = " + psQuote(title) + "; $f.ShowNewFolderButton = $true; " +
		"if ($f.ShowDialog($o) -eq [System.Windows.Forms.DialogResult]::OK) { " +
		"[Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($f.SelectedPath))) }"
	cmd := exec.Command("powershell", "-STA", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
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
