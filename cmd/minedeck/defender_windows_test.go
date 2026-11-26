package main

// 「加入 Windows 安全中心排除项」的测试。绝不真去提权、真去改 Defender：
//   - 下面的 init 先把 runElevated 换成一碰就失败的，哪个测试忘了换成假的，也只会失败，不会弹 UAC；
//   - 拼命令、分类、记状态都拿假的执行器测；
//   - 起真的 powershell.exe 的只有两处，都不提权：
//     TestDefenderScriptRealPowerShell 只让 PowerShell 的解析器读一遍拼出来的脚本（不执行），再让它把单引号字面量原样吐回来；
//     TestDefenderScriptBehavior 真的执行拼出来的脚本，但先关掉模块自动加载、确认 Defender 模块没加载，再把脚本用到的
//     Get-Service、Get-Command 和全部 Mp 命令定义成同名的假函数（PowerShell 里同名的函数比命令优先），真的 Mp 命令一个都调不到。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/windows"

	"minedeck/internal/app"
)

func init() {
	runElevated = refuseElevate
}

// refuseElevate 测试里默认的执行器：一碰就失败，绝不提权。
func refuseElevate(uintptr, string, string) (uint32, error) {
	return 0, errors.New("测试里不许真的以管理员身份运行")
}

// cleanOnly 只整理写法、不碰文件系统的 resolve（表格用例里的路径都是编的）。
func cleanOnly(p string) string { return filepath.Clean(stripVerbatim(p)) }

// 各种难缠的文件夹名：空格、中文、单引号、弯引号、末尾反斜杠、PowerShell 里有特殊意思的 $ ` ; 和逗号、双引号（Windows 文件名里不会有，也照样拼不坏）。
var trickyPaths = []string{
	`E:\MineDeck`,
	`C:\Program Files\MineDeck 3`,
	`D:\我的世界\开服 工具`,
	`D:\Tom's MC`,
	`D:\Tom''s MC`,
	`D:\it’s ‘mc’ ‚x‛`,
	`D:\MineDeck\`,
	`D:\MineDeck\\`,
	`E:\a\ b\`,
	`E:\$env:USERNAME\$(calc)`,
	"E:\\back`tick\\x",
	`E:\a; Remove-Item C:\x; '`,
	`E:\a,b`,
	`E:\a"b\"`,
	`\\server\share\MineDeck`,
	`E:\` + strings.Repeat("长", 120),
}

// psLiteralAt 按 PowerShell 的规矩读 s 开头的单引号字符串字面量（测试自己的一份：引号是那五个字符里的任何一个，
// 两个引号字符连着算一个字面的引号），返回字面的值和剩下的部分。
func psLiteralAt(t *testing.T, s string) (val, rest string) {
	t.Helper()
	r, n := utf8.DecodeRuneInString(s)
	if !isPSSingleQuote(r) {
		t.Fatalf("应以单引号开头：%.40q", s)
	}
	s = s[n:]
	var b strings.Builder
	for {
		if s == "" {
			t.Fatalf("字符串没有收尾的单引号")
		}
		r, n := utf8.DecodeRuneInString(s)
		s = s[n:]
		if isPSSingleQuote(r) {
			r2, n2 := utf8.DecodeRuneInString(s)
			if !isPSSingleQuote(r2) {
				return b.String(), s
			}
			s = s[n2:]
			r = r2
		}
		b.WriteRune(r)
	}
}

// psAssigned 读脚本里「$name=」后面的那个单引号字面量。
func psAssigned(t *testing.T, script, name string) string {
	t.Helper()
	i := strings.Index(script, "$"+name+"=")
	if i < 0 {
		t.Fatalf("脚本里找不到 $%s=：%s", name, script)
	}
	v, rest := psLiteralAt(t, script[i+len(name)+2:])
	if !strings.HasPrefix(rest, "; ") {
		t.Errorf("$%s 的字面量后面应紧跟「; 」，实际 %.40q", name, rest)
	}
	return v
}

func TestPSQuote(t *testing.T) {
	for in, want := range map[string]string{
		`E:\MineDeck`:      `'E:\MineDeck'`,
		`D:\Tom's MC`:      `'D:\Tom''s MC'`,
		`D:\it’s`:          `'D:\it’’s'`,
		"‘‚‛":              "'‘‘‚‚‛‛'",
		`D:\MineDeck\`:     `'D:\MineDeck\'`,
		`E:\$x\$(y)`:       `'E:\$x\$(y)'`,
		`D:\我的世界`:          `'D:\我的世界'`,
		``:                 `''`,
		`a"b`:              `'a"b'`,
		"back`tick":        "'back`tick'",
		`'`:                `''''`,
		`E:\a; rm x; '`:    `'E:\a; rm x; '''`,
		`C:\Program Files`: `'C:\Program Files'`,
	} {
		if got := psQuote(in); got != want {
			t.Errorf("psQuote(%q) = %q，应为 %q", in, got, want)
		}
	}
	for _, p := range trickyPaths {
		got, rest := psLiteralAt(t, psQuote(p))
		if got != p || rest != "" {
			t.Errorf("psQuote(%q) 读回来是 %q，后面还剩 %q", p, got, rest)
		}
	}
}

// 拼出来的参数交给真的 CommandLineToArgvW 拆开：前面几个开关原样，脚本是一个完整的参数；脚本里 $p= 和 $o= 后面
// 按 PowerShell 的规矩读回来，正好是原来的文件夹和结果文件；路径里的东西没有漏到字面量外面去。脚本的几道关都在。
func TestDefenderParamsRoundTrip(t *testing.T) {
	out := `C:\Users\张三 O'Neil\AppData\Local\Temp\minedeck-defender-1.txt`
	for _, action := range []string{defenderAdd, defenderRemove} {
		cmdlet := map[string]string{defenderAdd: "Add-MpPreference", defenderRemove: "Remove-MpPreference"}[action]
		for _, p := range trickyPaths {
			params := defenderParams(action, p, out)
			argv, err := windows.DecomposeCommandLine(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe ` + params)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command"}
			if len(argv) != 7 || strings.Join(argv[1:6], " ") != strings.Join(want, " ") {
				t.Fatalf("%s %q：拆出来的参数不对（%d 个）：%q", action, p, len(argv), argv)
			}
			script := argv[6]
			if script != defenderScript(action, p, out) {
				t.Fatalf("%s %q：命令行上的脚本和拼的不一样：\n%s\n%s", action, p, script, defenderScript(action, p, out))
			}
			if strings.Contains(script, `"`) && !strings.Contains(p, `"`) {
				t.Errorf("脚本里不该有双引号：%s", script)
			}
			if got := psAssigned(t, script, "p"); got != p {
				t.Errorf("%s：$p 读回来是 %q，应为 %q", action, got, p)
			}
			if got := psAssigned(t, script, "o"); got != out {
				t.Errorf("$o 读回来是 %q，应为 %q", got, out)
			}
			for _, must := range []string{
				cmdlet + " -ExclusionPath $p -Force -ErrorAction Stop }",     // 命令上再写一遍 -ErrorAction Stop
				"catch { $ErrorActionPreference='SilentlyContinue'; $e=$_; ", // catch 里先改回 SilentlyContinue 再拼报错
				"Get-Command " + cmdlet + " -ErrorAction SilentlyContinue",   // 先查有没有 Defender 的命令
				"0x8004100e",                             // WMI 命名空间不在也算没有 Defender
				"$mp=Get-MpPreference -ErrorAction Stop", // 改完回读核对
				"exit " + fmt.Sprint(psExitVerify),
				"Out-File -LiteralPath $o",
			} {
				if !strings.Contains(script, must) {
					t.Errorf("%s 的脚本里应有 %q：%s", action, must, script)
				}
			}
			if action == defenderAdd != strings.Contains(script, "Get-MpComputerStatus") {
				t.Errorf("只有加入时看 Get-MpComputerStatus（被动模式、实时防护关着就不加），撤销不拦：%s", script)
			}
			if strings.Contains(script, "Set-Content") || strings.Contains(script, "Set-MpPreference") {
				t.Errorf("脚本里不该有 Set-Content（和开着的结果文件冲突）、Set-MpPreference：%s", script)
			}
			if other := map[string]string{defenderAdd: "Remove-MpPreference", defenderRemove: "Add-MpPreference"}[action]; strings.Contains(script, other) {
				t.Errorf("%s 的脚本里不该出现 %s", action, other)
			}
		}
	}
}

// 真的 PowerShell（不提权、不执行）：
//  1. 解析器读一遍 defenderScript 拼出来的脚本（只解析、不执行），没有语法错，里面的命令只有那几个，$p 和 $o 的值是原样的，
//     Mp 命令的 -ExclusionPath 给的就是 $p；
//  2. 照 defenderParams 的拼法（同一个 psCommandParams）起 powershell.exe，让它把 psQuote 的字面量按 UTF-8 转 Base64 吐回来，
//     和原来的一样：Windows 命令行和 PowerShell 两层的转义都对。
func TestDefenderScriptRealPowerShell(t *testing.T) {
	exe := needPowerShell(t)
	out := `C:\Users\张三 O’Neil\AppData\Local\Temp\minedeck-defender-1.txt`

	// 1. 只解析：脚本写成 JSON、转 Base64 从标准输入交过去（原样一个字节不差，也没有环境变量 32K 字符的上限）；
	//    结果同样转成 JSON 再 Base64，不受控制台代码页影响
	type check struct {
		Errors   int      `json:"errors"`
		Commands []string `json:"commands"`
		PathVar  string   `json:"pathVar"`
		P        string   `json:"p"`
		Out      string   `json:"out"`
	}
	var scripts []string
	var paths []string
	for _, action := range []string{defenderAdd, defenderRemove} {
		for _, p := range trickyPaths {
			scripts = append(scripts, defenderScript(action, p, out))
			paths = append(paths, p)
		}
	}
	js, _ := json.Marshal(scripts)
	parser := `$ErrorActionPreference='Stop'; $all = ConvertFrom-Json ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd()))); ` +
		`$res = foreach ($src in $all) { $tk=$null; $er=$null; ` +
		`$ast=[System.Management.Automation.Language.Parser]::ParseInput($src,[ref]$tk,[ref]$er); ` +
		`$cmds=@($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.CommandAst] }, $true)); ` +
		`$pv=$null; foreach ($c in $cmds) { $el=$c.CommandElements; for ($i=0; $i -lt $el.Count-1; $i++) { ` +
		`if ($el[$i] -is [System.Management.Automation.Language.CommandParameterAst] -and $el[$i].ParameterName -eq 'ExclusionPath') { $pv=$el[$i+1].VariablePath.UserPath } } }; ` +
		`$asg = { param($name) @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.AssignmentStatementAst] -and $n.Left.VariablePath.UserPath -eq $name }.GetNewClosure(), $true))[0].Right.Expression.Value }; ` +
		`[pscustomobject]@{ errors=@($er).Count; commands=@($cmds | ForEach-Object { $_.GetCommandName() }); pathVar=$pv; p=(& $asg 'p'); out=(& $asg 'o') } }; ` +
		`[Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -InputObject @($res) -Depth 4 -Compress))))`
	so, se, err := runPS(exe, parser, base64.StdEncoding.EncodeToString(js), nil)
	if err != nil {
		t.Fatalf("PowerShell 解析脚本失败：%v：%s", err, se)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(so))
	if err != nil {
		t.Fatalf("PowerShell 的输出读不懂：%q（%s）", so, se)
	}
	var checks []check
	if err := json.Unmarshal(raw, &checks); err != nil || len(checks) != len(scripts) {
		t.Fatalf("解析结果对不上：%v，%d 条：%s", err, len(checks), raw)
	}
	allowed := map[string]bool{"Get-Service": true, "Get-Command": true, "Save-Result": true, "Out-File": true, "Where-Object": true,
		"Get-MpComputerStatus": true, "Add-MpPreference": true, "Remove-MpPreference": true, "Get-MpPreference": true}
	for i, c := range checks {
		if c.Errors != 0 {
			t.Errorf("%q：PowerShell 说脚本有 %d 处语法错：%.300s", paths[i], c.Errors, scripts[i])
		}
		for _, name := range c.Commands {
			if !allowed[name] {
				t.Errorf("%q：脚本里冒出了别的命令 %q（转义漏了）：%.300s", paths[i], name, scripts[i])
			}
		}
		if c.P != paths[i] || c.PathVar != "p" {
			t.Errorf("PowerShell 读到的 $p 是 %q、-ExclusionPath 给的是 $%s，应为 %q 和 $p", c.P, c.PathVar, paths[i])
		}
		if c.Out != out {
			t.Errorf("PowerShell 读到的 $o 是 %q，应为 %q", c.Out, out)
		}
	}

	// 2. 真跑一遍：只输出字面量本身
	var lits []string
	for _, p := range trickyPaths {
		lits = append(lits, psQuote(p))
	}
	echo := `$ErrorActionPreference='Stop'; [Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -InputObject @(` +
		strings.Join(lits, ", ") + `) -Compress))))`
	so, se, err = runPS(exe, echo, "", nil)
	if err != nil {
		t.Fatalf("PowerShell 回显失败：%v：%s", err, se)
	}
	raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(so))
	if err != nil {
		t.Fatalf("PowerShell 的输出读不懂：%q（%s）", so, se)
	}
	var back []string
	if err := json.Unmarshal(raw, &back); err != nil || len(back) != len(trickyPaths) {
		t.Fatalf("回显对不上：%v：%s", err, raw)
	}
	for i, p := range trickyPaths {
		if back[i] != p {
			t.Errorf("PowerShell 吐回来的是 %q，应为 %q", back[i], p)
		}
	}
}

// needPowerShell 起真 PowerShell 的测试：-short 时跳过，没有 Windows PowerShell 时跳过。
func needPowerShell(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("-short：不起 PowerShell")
	}
	exe := powershellPath()
	if _, err := os.Stat(exe); err != nil {
		t.Skip("这台电脑上没有 Windows PowerShell：", err)
	}
	return exe
}

// runPS 不提权地起 powershell.exe 跑 script（命令行照 psCommandParams 拼），stdin 是给它的标准输入，env 是另加的环境变量。
func runPS(exe, script, stdin string, env []string) (stdout, stderr string, err error) {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(exe) + " " + psCommandParams(script), CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Stdin = strings.NewReader(stdin)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

// psList 把几个字符串写成 PowerShell 的数组表达式（每个都过 psQuote）。
func psList(items ...string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = psQuote(s)
	}
	return "@(" + strings.Join(q, ", ") + ")"
}

// mpCmdlets 脚本可能用到的全部 Defender 命令：执行脚本的测试里每一个都要有同名的假函数顶着。
var mpCmdlets = []string{"Add-MpPreference", "Remove-MpPreference", "Get-MpPreference", "Get-MpComputerStatus", "Set-MpPreference"}

// 真的执行 defenderScript 拼出来的脚本（不提权），看每种情形的退出码、写进结果文件的原文、交给 Mp 命令的路径。
// 真的 Defender 命令一个都调不到：先关掉模块自动加载、确认 Defender 模块没加载（加载了就退出 99），
// 再把 Get-Service、Get-Command 和 mpCmdlets 全定义成同名的假函数（同名的函数比命令优先），假函数只往一个记账文件里写字。
// 结果文件是 openResultFile 建的、一直开着的那个：顺带核对脚本里的 Out-File 写得进这边开着的文件。
func TestDefenderScriptBehavior(t *testing.T) {
	exe := needPowerShell(t)
	const path = `D:\Tom’s ‘MC’ 世界\`
	type scen struct {
		name    string
		action  string
		service string // Get-Service 的假结果：none（没装）、Stopped、Running
		cmdlet  bool   // Get-Command 找不找得到 Add / Remove-MpPreference
		status  string // Get-MpComputerStatus 的假结果（PowerShell 表达式），throw 表示它报错
		mpThrow string // Add / Remove-MpPreference 抛的错，空表示成功
		list    string // Get-MpPreference 的 ExclusionPath（PowerShell 表达式）
		code    uint32
		out     string // 结果文件里应有的字
		called  bool   // Add / Remove-MpPreference 有没有被调到
	}
	normal := `[pscustomobject]@{ AMRunningMode='Normal'; RealTimeProtectionEnabled=$true }`
	scens := []scen{
		{"加入：做成了，回读列表里有（末尾反斜杠、大小写不同也算）", defenderAdd, "Running", true, normal, "", psList(`C:\Other`, `d:\tom’s ‘mc’ 世界`), 0, "", true},
		{"加入：列表读不到（一句 N/A）不当失败", defenderAdd, "Running", true, normal, "", `@('N/A: Must be an administrator to view exclusions')`, 0, "", true},
		{"加入：列表读得到、加完还是空的（没加进去）", defenderAdd, "Running", true, normal, "", `$null`, psExitVerify, "is not in ExclusionPath", true},
		{"加入：Get-MpPreference 报错、读不到列表，不当失败", defenderAdd, "Running", true, normal, "", "throw", 0, "", true},
		{"加入：回读读得到、却没有这一条", defenderAdd, "Running", true, normal, "", `@('C:\Other')`, psExitVerify, "is not in ExclusionPath", true},
		{"加入：WinDefend 没装", defenderAdd, "none", true, normal, "", `$null`, psExitNoService, "WinDefend service not installed", false},
		{"加入：WinDefend 停着", defenderAdd, "Stopped", true, normal, "", `$null`, psExitNoService, "WinDefend service status: Stopped", false},
		{"加入：被动模式（装了别的杀毒软件）", defenderAdd, "Running", true, `[pscustomobject]@{ AMRunningMode='Passive Mode'; RealTimeProtectionEnabled=$true }`, "", `$null`, psExitNoService, "AMRunningMode: Passive Mode", false},
		{"加入：实时防护关着", defenderAdd, "Running", true, `[pscustomobject]@{ AMRunningMode='Normal'; RealTimeProtectionEnabled=$false }`, "", `$null`, psExitNoService, "RealTimeProtectionEnabled: False", false},
		{"加入：老版本没有 AMRunningMode，实时防护开着就照加", defenderAdd, "Running", true, `[pscustomobject]@{ RealTimeProtectionEnabled=$true }`, "", psList(`D:\Tom’s ‘MC’ 世界`), 0, "", true},
		{"加入：Get-MpComputerStatus 报错", defenderAdd, "Running", true, "throw", "", `$null`, psExitNoService, "Get-MpComputerStatus: 状态读不出来", false},
		{"加入：没有 Defender 的命令", defenderAdd, "Running", false, normal, "", `$null`, psExitNoCmdlet, "Add-MpPreference not found", false},
		{"加入：Add-MpPreference 报错，原文写进结果文件", defenderAdd, "Running", true, normal, "操作失败，出现以下错误: 0x800106ba", `$null`, psExitCmdletErr, "0x800106ba", true},
		{"加入：WMI 命名空间不在（0x8004100E）算没有 Defender", defenderAdd, "Running", true, normal, "HRESULT 0x8004100e", `$null`, psExitNoCmdlet, "0x8004100e", true},
		{"撤销：不看实时防护（被动模式也照撤），回读里没有了", defenderRemove, "Running", true, "throw", "", `@('C:\Other')`, 0, "", true},
		{"撤销：回读还在里面", defenderRemove, "Running", true, normal, "", psList(`D:\TOM’S ‘MC’ 世界\`), psExitVerify, "is still in ExclusionPath", true},
		{"撤销：WinDefend 停着", defenderRemove, "Stopped", true, normal, "", `$null`, psExitNoService, "WinDefend service status: Stopped", false},
	}
	var wg sync.WaitGroup
	for _, s := range scens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir := t.TempDir()
			f, err := openResultFile(dir)
			if err != nil {
				t.Errorf("%s：%v", s.name, err)
				return
			}
			defer func() { f.Close(); os.Remove(f.Name()) }()
			logFile := filepath.Join(dir, "fake-calls.txt")
			svc := `$null`
			if s.service != "none" {
				svc = `[pscustomobject]@{ Status='` + s.service + `' }`
			}
			status := `return ` + s.status
			if s.status == "throw" {
				status = `throw '状态读不出来'`
			}
			mpBody := func(verb string) string {
				b := `Out-File -LiteralPath $env:MD_FAKE_LOG -Append -Encoding UTF8 -InputObject ('` + verb + ` ' + $ExclusionPath); `
				if s.mpThrow != "" {
					b += `throw '` + s.mpThrow + `'`
				}
				return b
			}
			// 先把真的 Defender 模块挡在外面，再定义同名假函数
			prefix := `$PSModuleAutoLoadingPreference='None'; Import-Module Microsoft.PowerShell.Utility, Microsoft.PowerShell.Management -ErrorAction Stop; ` +
				`if (Get-Module Defender) { exit 99 }; ` +
				`function Get-Service { [CmdletBinding()] param($Name) if ($Name -ne 'WinDefend') { exit 98 }; return ` + svc + ` }; ` +
				`function Get-Command { [CmdletBinding()] param($Name) if ($Name -notmatch '^(Add|Remove)-MpPreference$') { exit 97 }; return ` + map[bool]string{true: `'fake'`, false: `$null`}[s.cmdlet] + ` }; ` +
				`function Get-MpComputerStatus { [CmdletBinding()] param() ` + status + ` }; ` +
				`function Add-MpPreference { [CmdletBinding()] param($ExclusionPath, [switch]$Force) ` + mpBody("ADD") + ` }; ` +
				`function Remove-MpPreference { [CmdletBinding()] param($ExclusionPath, [switch]$Force) ` + mpBody("REMOVE") + ` }; ` +
				`function Get-MpPreference { [CmdletBinding()] param() ` + map[bool]string{true: `throw '读不到'`, false: `return [pscustomobject]@{ ExclusionPath = ` + s.list + ` }`}[s.list == "throw"] + ` }; ` +
				`function Set-MpPreference { exit 96 }; `
			for _, name := range mpCmdlets {
				if !strings.Contains(prefix, "function "+name+" ") {
					t.Errorf("没有给 %s 定义假函数，不许跑", name)
					return
				}
			}
			_, se, err := runPS(exe, prefix+defenderScript(s.action, path, f.Name()), "", []string{"MD_FAKE_LOG=" + logFile})
			var code uint32
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = uint32(ee.ExitCode())
			} else if err != nil {
				t.Errorf("%s：PowerShell 没跑起来：%v：%s", s.name, err, se)
				return
			}
			if code != s.code {
				t.Errorf("%s：退出码 %d，应为 %d（%s）", s.name, code, s.code, se)
			}
			if got := readDefenderOut(f); s.out == "" && got != "" || !strings.Contains(got, s.out) {
				t.Errorf("%s：结果文件里是 %q，应有 %q", s.name, got, s.out)
			}
			calls, _ := os.ReadFile(logFile)
			verb := map[string]string{defenderAdd: "ADD ", defenderRemove: "REMOVE "}[s.action]
			wantCall := verb + path
			if got := strings.TrimSpace(strings.TrimPrefix(string(calls), "\xef\xbb\xbf")); s.called && got != wantCall || !s.called && got != "" {
				t.Errorf("%s：假的 Mp 命令收到 %q，应为 %q（called=%v）", s.name, got, wantCall, s.called)
			}
		}()
	}
	wg.Wait()
}

// 分类：只看系统错误码和脚本自己的退出码。给人看的那句话：不超过 60 个字、有中文、不带路径和英文报错的痕迹（界面才会接在结论后面）。
func TestClassifyDefender(t *testing.T) {
	cases := []struct {
		name   string
		code   uint32
		err    error
		want   string
		inMsg  string
		action string
	}{
		{"做成了", 0, nil, dfOK, "", defenderAdd},
		{"UAC 没通过", 0, windows.ERROR_CANCELLED, dfCancelled, "没通过系统的管理员确认", defenderAdd},
		{"撤销时 UAC 没通过", 0, windows.ERROR_CANCELLED, dfCancelled, "确认被取消了", defenderRemove},
		{"包了一层的 ERROR_CANCELLED 也认", 0, errors.Join(errors.New("x"), windows.ERROR_CANCELLED), dfCancelled, "「否」", defenderAdd},
		{"策略禁止启动 PowerShell", 0, windows.ERROR_ACCESS_DISABLED_BY_POLICY, dfBlocked, "不让启动 PowerShell", defenderAdd},
		{"软件限制策略（不弹窗的那种）", 0, windows.ERROR_ACCESS_DISABLED_NO_SAFER_UI_BY_POLICY, dfBlocked, "手动添加", defenderAdd},
		{"应用程序控制拦下", 0, windows.ERROR_SYSTEM_INTEGRITY_POLICY_VIOLATION, dfBlocked, "手动删除", defenderRemove},
		{"找不到 PowerShell", 0, windows.ERROR_FILE_NOT_FOUND, dfFailed, "找不到 PowerShell", defenderAdd},
		{"找不到 PowerShell 的文件夹", 0, windows.ERROR_PATH_NOT_FOUND, dfFailed, "找不到 PowerShell", defenderAdd},
		{"等太久：说明可能还在跑", 0, errDefenderTimeout, dfFailed, "可能还在跑", defenderAdd},
		{"别的系统错误", 0, windows.ERROR_ACCESS_DENIED, dfFailed, "手动添加", defenderAdd},
		{"别的错误", 0, errors.New("x"), dfFailed, "手动删除", defenderRemove},
		{"加入：Defender 没在做实时防护", psExitNoService, nil, dfUnavailable, "加了也不起作用", defenderAdd},
		{"撤销：Defender 没在工作", psExitNoService, nil, dfUnavailable, "撤销不了", defenderRemove},
		{"没有 Defender 的命令", psExitNoCmdlet, nil, dfUnavailable, "别的杀毒软件", defenderRemove},
		{"Add-MpPreference 报错", psExitCmdletErr, nil, dfFailed, "手动添加", defenderAdd},
		{"加完回读没有", psExitVerify, nil, dfFailed, "加完再查", defenderAdd},
		{"撤完回读还在", psExitVerify, nil, dfFailed, "撤完再查", defenderRemove},
		{"PowerShell 自己出错", 1, nil, dfFailed, "手动添加", defenderAdd},
		{"PowerShell 没起来的怪退出码", 0xFFFD0000, nil, dfFailed, "手动删除", defenderRemove},
	}
	for _, c := range cases {
		kind, msg := classifyDefender(c.action, c.code, c.err)
		if kind != c.want {
			t.Errorf("%s：种类 %q，应为 %q", c.name, kind, c.want)
		}
		if kind == dfOK {
			if msg != "" {
				t.Errorf("%s：做成了不该有话：%q", c.name, msg)
			}
			continue
		}
		if !strings.Contains(msg, c.inMsg) {
			t.Errorf("%s：说法 %q 里应有 %q", c.name, msg, c.inMsg)
		}
		checkHumanMsg(t, c.name, msg)
	}
}

// checkHumanMsg 界面拼报错用的那句话的规矩（app.js 的 errSummary：含中文、不超过 60 个字、没有路径和「: 」这类痕迹）。
func checkHumanMsg(t *testing.T, name, msg string) {
	t.Helper()
	if n := utf8.RuneCountInString(msg); n == 0 || n > 60 {
		t.Errorf("%s：说法 %d 个字（应 1–60）：%q", name, n, msg)
	}
	if strings.ContainsAny(msg, "\\/\r\n") || strings.Contains(msg, ": ") || strings.Contains(msg, "0x") {
		t.Errorf("%s：说法里不该有路径、换行、英文冒号或错误码：%q", name, msg)
	}
}

func TestBroadReason(t *testing.T) {
	broad := []string{`C:\Users\张三`, `C:\Users\张三\Desktop`, `C:\Users\张三\Downloads`, `C:\Users\张三\OneDrive\文档`, `C:\Windows`, `C:\Program Files`, ``}
	// subst S: 指到 C:\Users\张三、映射盘 Z: 指到下载文件夹（用替身的 resolve 模拟系统解开以后的样子）
	fake := func(p string) string {
		c := cleanOnly(p)
		for from, to := range map[string]string{`s:`: `C:\Users\张三`, `z:`: `C:\Users\张三\Downloads`} {
			if lc := strings.ToLower(c); lc == from || strings.HasPrefix(lc, from+`\`) {
				return filepath.Clean(to + c[len(from):])
			}
		}
		return c
	}
	for _, bad := range []string{
		`C:\`, `D:\`, `d:`, `\\server\share`, `\\server\share\`, `\\?\C:\`, `\\?\UNC\server\share\`,
		`C:\Users\张三`, `c:\users\张三\`, `C:\Users`, `C:\Users\张三\Desktop`, `C:\USERS\张三\DOWNLOADS`, `\\?\C:\Users\张三\Downloads`,
		`C:\Users\张三\OneDrive`, `C:\Windows`, `C:\Program Files`, ``, `relative\dir`,
		`S:\`, `S:\Downloads`, `Z:\`, // subst / 映射盘解开以后是系统文件夹或磁盘根
	} {
		if broadReason(bad, broad, fake) == "" {
			t.Errorf("%q 不该整个排除", bad)
		}
	}
	for _, ok := range []string{
		`E:\MineDeck`, `C:\Users\张三\Desktop\MineDeck`, `C:\Users\张三\Downloads\MineDeck_3.2.9`, `C:\Program Files\MineDeck`,
		`C:\Users\张三\Desktop2`, `C:\Windows2\x`, `\\server\share\MineDeck`, `D:\我的世界\开服 工具\`, `Z:\MineDeck`, `\\?\E:\MineDeck`,
	} {
		if why := broadReason(ok, broad, fake); why != "" {
			t.Errorf("%q 应该能加：%s", ok, why)
		}
	}
}

// 真的目录联接和 8.3 短名：canonicalDir 解开以后，「系统文件夹」的别名绕不过 broadReason，samePath 认得出是同一个文件夹。
func TestBroadReasonFollowsJunctionsAndShortNames(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "Downloads 很长很长的文件夹名字")
	if err := os.MkdirAll(filepath.Join(real, "MineDeck"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "链接")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		t.Skipf("建不了目录联接：%v：%s", err, out)
	}
	if got := canonicalDir(link); !strings.EqualFold(got, canonicalDir(real)) {
		t.Fatalf("联接应解开成真实路径：%q，应为 %q", got, canonicalDir(real))
	}
	if broadReason(link, []string{real}, canonicalDir) == "" {
		t.Error("程序数据文件夹是指向系统文件夹的联接：不该整个排除")
	}
	if broadReason(real, []string{link}, canonicalDir) == "" {
		t.Error("系统文件夹本身是联接、程序就放在它指向的地方：不该整个排除")
	}
	if broadReason(root, []string{link}, canonicalDir) == "" {
		t.Error("联接所在的上一层也不该整个排除")
	}
	if why := broadReason(filepath.Join(link, "MineDeck"), []string{real}, canonicalDir); why != "" {
		t.Errorf("系统文件夹里单独建的文件夹（经联接访问）应该能加：%s", why)
	}
	// samePath 只比原样写法（Defender 存的就是这串字）：联接和它指向的文件夹在 Defender 那边是两条，不算同一条
	if samePath(link, real) || !samePath(real+`\`, strings.ToUpper(real)) {
		t.Error("samePath 只比整理过的原样写法：联接和真实路径不算同一条，大小写、末尾反斜杠不同的算")
	}
	if short, ok := shortPathOf(real); ok && !strings.EqualFold(short, real) {
		if broadReason(short, []string{real}, canonicalDir) == "" {
			t.Errorf("8.3 短名 %q 不该绕过", short)
		}
		if samePath(short, real) {
			t.Errorf("samePath 不解开短名：%q 和长名在 Defender 那边是两条", short)
		}
	} else {
		t.Log("这个卷上没有 8.3 短名，短名那一段没测")
	}
	if got := stripVerbatim(`\\?\UNC\server\share\x`); got != `\\server\share\x` {
		t.Errorf("\\\\?\\UNC\\ 前缀应写回 \\\\server：%q", got)
	}
	if !samePath(`\\?\C:\Users\x\`, `c:\users\X`) {
		t.Error("带 \\\\?\\ 前缀的和普通写法应算同一个")
	}
}

func shortPathOf(p string) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || int(n) >= len(buf) {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

// 这台电脑上的系统文件夹都在名单里（加上了 AppData、公用文件夹这些），临时文件夹挪到别处也不靠它的上一层兜着。
func TestKnownBroadFolders(t *testing.T) {
	got := knownBroadFolders()
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_RoamingAppData, windows.FOLDERID_LocalAppData, windows.FOLDERID_UserProfiles,
		windows.FOLDERID_Public, windows.FOLDERID_PublicDesktop, windows.FOLDERID_PublicDownloads, windows.FOLDERID_PublicDocuments,
		windows.FOLDERID_Profile, windows.FOLDERID_Downloads, windows.FOLDERID_Desktop} {
		p, err := windows.KnownFolderPath(id, 0)
		if err != nil || p == "" {
			continue
		}
		found := false
		for _, g := range got {
			found = found || strings.EqualFold(g, p)
		}
		if !found {
			t.Errorf("%s 应在不许整个排除的名单里：%q", p, got)
		}
		if broadReason(p, got, canonicalDir) == "" {
			t.Errorf("%s 不该整个排除", p)
		}
	}
	if la, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0); err == nil {
		if why := broadReason(filepath.Join(la, "MineDeck"), got, canonicalDir); why != "" {
			t.Errorf("AppData 里单独的文件夹应该能加：%s", why)
		}
	}
}

// 结果文件：建好以后一直开着，同一个用户的别的程序删不掉、改不了名（换不成链接），写得进去，从开着的句柄读得回来；关了再删。
func TestOpenResultFileHeld(t *testing.T) {
	dir := t.TempDir()
	f, err := openResultFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	if !strings.HasPrefix(filepath.Base(name), "minedeck-defender-") || filepath.Dir(name) != dir {
		t.Errorf("名字或位置不对：%s", name)
	}
	if err := os.Remove(name); err == nil {
		t.Fatal("开着的时候不该删得掉")
	}
	if err := os.Rename(name, name+".x"); err == nil {
		t.Fatal("开着的时候不该改得了名")
	}
	if err := os.WriteFile(name, []byte("\xef\xbb\xbf拒绝访问。\r\n"), 0o644); err != nil {
		t.Fatalf("别人（提权的 PowerShell）应写得进去：%v", err)
	}
	if got := readDefenderOut(f); got != "拒绝访问。" {
		t.Errorf("从开着的句柄读回 %q", got)
	}
	if err := os.WriteFile(name, []byte(strings.Repeat("长", defenderOutMax)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readDefenderOut(f); !utf8.ValidString(got) || len(got) > defenderOutMax || len(got) < defenderOutMax-3 {
		t.Errorf("超长时应截在完整的字上：%d 字节，合法 %v", len(got), utf8.ValidString(got))
	}
	f.Close()
	if err := os.Remove(name); err != nil {
		t.Errorf("关了以后应删得掉：%v", err)
	}
	if got := readDefenderOut(f); got != "" {
		t.Errorf("关掉的文件读不到东西，应为空串：%q", got)
	}
}

// fakeRun 假的执行器：记下每次的参数，核对结果文件开着、换不掉，往里写 out（模拟脚本出错时写的原文），返回 code / err。
type fakeRun struct {
	t       *testing.T
	dir     string
	code    uint32
	err     error
	out     string
	panic   bool
	calls   []string
	outSeen []string
}

func (f *fakeRun) run(exe, params string) (uint32, error) {
	f.calls = append(f.calls, params)
	if f.panic {
		panic("假的执行器出了意外")
	}
	if !strings.EqualFold(filepath.Base(exe), "powershell.exe") {
		f.t.Errorf("跑的应是 powershell.exe，实际 %q", exe)
	}
	m, _ := filepath.Glob(filepath.Join(f.dir, "minedeck-defender-*.txt"))
	if len(m) != 1 {
		f.t.Errorf("提权之前应正好建好一个结果文件，实际 %v", m)
		return f.code, f.err
	}
	f.outSeen = append(f.outSeen, m[0])
	if !strings.Contains(params, "$o="+psQuote(m[0])+";") {
		f.t.Errorf("参数里应带着结果文件 %s：%s", m[0], params)
	}
	if err := os.Remove(m[0]); err == nil {
		f.t.Errorf("等 UAC 的时候结果文件不该删得掉（会被换成链接）")
	}
	if f.out != "" {
		// PowerShell 5.1 的 Out-File -Encoding UTF8 带 BOM
		if err := os.WriteFile(m[0], append([]byte("\xef\xbb\xbf"), []byte(f.out+"\r\n")...), 0o644); err != nil {
			f.t.Errorf("写结果文件：%v", err)
		}
	}
	return f.code, f.err
}

func TestDoDefenderAction(t *testing.T) {
	t0 := time.Date(2026, 11, 27, 14, 3, 0, 0, time.Local)
	base := `D:\我的世界\Tom's MineDeck`
	old := &app.DefenderExclusion{Path: `E:\旧位置\MineDeck\`, At: t0.Add(-48 * time.Hour)}
	same := &app.DefenderExclusion{Path: `d:\我的世界\tom's minedeck\`, At: t0.Add(-time.Hour)}
	broad := []string{`C:\Users\张三`, `C:\Users\张三\Downloads`}
	subst := func(p string) string { // S: 是 subst 出来的盘，指到下载文件夹
		if c := cleanOnly(p); strings.HasPrefix(strings.ToLower(c), `s:`) {
			return filepath.Clean(`C:\Users\张三\Downloads` + c[2:])
		}
		return cleanOnly(p)
	}
	cases := []struct {
		name     string
		action   string
		base     string
		record   *app.DefenderExclusion
		resolve  func(string) string
		code     uint32
		err      error
		out      string
		kind     string
		target   string                 // 这次处理的文件夹（没去跑时也看 res.Path）
		ran      bool                   // 有没有去提权
		changed  bool                   // 配置要不要改
		rec      *app.DefenderExclusion // 改成什么（changed 为 false 时应原样是 record）
		inMsg    string
		inDetail []string
	}{
		{name: "加入：做成了，记下文件夹和时间", action: defenderAdd, base: base, kind: dfOK, target: base, ran: true, changed: true,
			rec: &app.DefenderExclusion{Path: base, At: t0}},
		{name: "已经加过再加一次：照样跑（Add-MpPreference 本身不报错），时间更新", action: defenderAdd, base: base, record: same, kind: dfOK, target: base,
			ran: true, changed: true, rec: &app.DefenderExclusion{Path: base, At: t0}},
		{name: "加入：UAC 没通过，什么都不记", action: defenderAdd, base: base, err: windows.ERROR_CANCELLED, kind: dfCancelled, target: base, ran: true,
			inMsg: "没通过系统的管理员确认", inDetail: []string{"1223", base}},
		{name: "加入：Defender 没在做实时防护", action: defenderAdd, base: base, code: psExitNoService, out: "AMRunningMode: Passive Mode | RealTimeProtectionEnabled: True",
			kind: dfUnavailable, target: base, ran: true, inMsg: "加了也不起作用", inDetail: []string{"Passive Mode", "退出码：20"}},
		{name: "加入：没有 Defender 的命令", action: defenderAdd, base: base, code: psExitNoCmdlet, out: "Add-MpPreference not found",
			kind: dfUnavailable, target: base, ran: true, inDetail: []string{"Add-MpPreference not found"}},
		{name: "加入：Add-MpPreference 报错，原文进详情", action: defenderAdd, base: base, code: psExitCmdletErr,
			out: "操作失败，出现以下错误: 0x800106ba。 | HRESULT 0x800106ba,Add-MpPreference | HResult=0x80131500", kind: dfFailed, target: base, ran: true,
			inDetail: []string{"0x800106ba", "退出码：22", "以管理员身份运行："}},
		{name: "加入：加完回读没有，不记", action: defenderAdd, base: base, code: psExitVerify, out: "verify: x is not in ExclusionPath after Add-MpPreference",
			kind: dfFailed, target: base, ran: true, inMsg: "加完再查", inDetail: []string{"is not in ExclusionPath", "退出码：24"}},
		{name: "加入：PowerShell 被策略禁止启动", action: defenderAdd, base: base, err: windows.ERROR_ACCESS_DISABLED_BY_POLICY, kind: dfBlocked, target: base, ran: true,
			inDetail: []string{"1260"}},
		{name: "加入：等太久", action: defenderAdd, base: base, err: errDefenderTimeout, kind: dfFailed, target: base, ran: true, inMsg: "可能还在跑",
			inDetail: []string{"可能还在跑"}},
		{name: "加入：记着的是另一个位置，不替它加", action: defenderAdd, base: base, record: old, kind: dfRefused, target: base,
			inDetail: []string{old.Path, base}},
		{name: "加入：路径是空的记录（手改坏的配置）当没记过，照样加", action: defenderAdd, base: base, record: &app.DefenderExclusion{At: t0}, kind: dfOK,
			target: base, ran: true, changed: true, rec: &app.DefenderExclusion{Path: base, At: t0}},
		{name: "加入：磁盘根目录不加", action: defenderAdd, base: `D:\`, kind: dfRefused, target: `D:\`, inDetail: []string{"磁盘根目录"}},
		{name: "加入：程序就放在下载文件夹里，不加", action: defenderAdd, base: `C:\Users\张三\Downloads`, kind: dfRefused, target: `C:\Users\张三\Downloads`,
			inDetail: []string{"系统文件夹"}},
		{name: "加入：用户文件夹的上一层，不加", action: defenderAdd, base: `C:\Users`, kind: dfRefused, target: `C:\Users`},
		{name: "加入：subst 盘的根其实是下载文件夹，不加", action: defenderAdd, base: `S:\`, resolve: subst, kind: dfRefused, target: `S:\`,
			inDetail: []string{`C:\Users\张三\Downloads`}},
		{name: "加入：subst 盘里单独的文件夹照样能加（加的是原来的写法）", action: defenderAdd, base: `S:\MineDeck`, resolve: subst, kind: dfOK, target: `S:\MineDeck`,
			ran: true, changed: true, rec: &app.DefenderExclusion{Path: `S:\MineDeck`, At: t0}},
		{name: "加入：末尾带反斜杠的照样认", action: defenderAdd, base: `E:\MineDeck\`, kind: dfOK, target: `E:\MineDeck`, ran: true, changed: true,
			rec: &app.DefenderExclusion{Path: `E:\MineDeck`, At: t0}},
		{name: "撤销：删记着的那一条（文件夹搬过，删旧位置，整理掉末尾的反斜杠），清掉记录", action: defenderRemove, base: base, record: old, kind: dfOK,
			target: `E:\旧位置\MineDeck`, ran: true, changed: true, rec: nil},
		{name: "撤销一个没加过的：删现在的文件夹，不算错", action: defenderRemove, base: base, kind: dfOK, target: base, ran: true, changed: true, rec: nil},
		{name: "撤销：UAC 没通过，记录留着", action: defenderRemove, base: base, record: same, err: windows.ERROR_CANCELLED, kind: dfCancelled,
			target: `d:\我的世界\tom's minedeck`, ran: true},
		{name: "撤销：Defender 没在工作，记录留着", action: defenderRemove, base: base, record: same, code: psExitNoService, kind: dfUnavailable,
			target: `d:\我的世界\tom's minedeck`, ran: true, inMsg: "撤销不了"},
		{name: "撤销：撤完回读还在，记录留着", action: defenderRemove, base: base, record: same, code: psExitVerify, kind: dfFailed,
			target: `d:\我的世界\tom's minedeck`, ran: true, inMsg: "撤完再查"},
		{name: "撤销：Remove-MpPreference 报错，记录留着", action: defenderRemove, base: base, record: same, code: psExitCmdletErr, out: "拒绝访问。",
			kind: dfFailed, target: `d:\我的世界\tom's minedeck`, ran: true, inDetail: []string{"拒绝访问。"}},
		{name: "撤销：程序数据文件夹没定下来，不拿相对路径去删", action: defenderRemove, base: "", kind: dfFailed, target: "."},
		{name: "不认识的动作", action: "set", base: base, kind: dfFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			f := &fakeRun{t: t, dir: dir, code: c.code, err: c.err, out: c.out}
			resolve := c.resolve
			if resolve == nil {
				resolve = cleanOnly
			}
			env := defenderEnv{base: c.base, record: c.record, broad: broad, resolve: resolve, exe: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				tempDir: dir, run: f.run, now: func() time.Time { return t0 }}
			res, rec, changed := doDefenderAction(c.action, env)
			if res.Kind != c.kind || res.Action != c.action {
				t.Fatalf("结果 %+v，种类应为 %q", res, c.kind)
			}
			if c.target != "" && res.Path != c.target {
				t.Errorf("处理的文件夹 %q，应为 %q", res.Path, c.target)
			}
			if ran := len(f.calls) > 0; ran != c.ran {
				t.Fatalf("有没有去提权：%v，应为 %v", ran, c.ran)
			}
			if c.ran {
				if len(f.calls) != 1 {
					t.Fatalf("应只提权一次，实际 %d 次", len(f.calls))
				}
				if !strings.Contains(f.calls[0], "$p="+psQuote(c.target)+";") {
					t.Errorf("参数里应带着 %s：%s", psQuote(c.target), f.calls[0])
				}
				cmdlet := map[string]string{defenderAdd: "Add-MpPreference", defenderRemove: "Remove-MpPreference"}[c.action]
				if !strings.Contains(f.calls[0], cmdlet+" -ExclusionPath $p -Force -ErrorAction Stop") {
					t.Errorf("应跑 %s：%s", cmdlet, f.calls[0])
				}
				for _, p := range f.outSeen {
					if _, err := os.Stat(p); !os.IsNotExist(err) {
						t.Errorf("结果文件用完应删掉：%s（%v）", p, err)
					}
				}
			}
			if left, _ := filepath.Glob(filepath.Join(dir, "minedeck-defender-*")); len(left) != 0 {
				t.Errorf("临时文件夹里不该留下结果文件：%v", left)
			}
			if changed != c.changed {
				t.Fatalf("配置要不要改：%v，应为 %v", changed, c.changed)
			}
			want := c.rec
			if !c.changed {
				want = c.record
			}
			if (rec == nil) != (want == nil) || rec != nil && (rec.Path != want.Path || !rec.At.Equal(want.At)) {
				t.Errorf("记录 %+v，应为 %+v", rec, want)
			}
			if c.kind == dfOK {
				if res.Msg != "" || res.Detail != "" {
					t.Errorf("做成了不该带说法和详情：%+v", res)
				}
				return
			}
			if c.kind != dfFailed || res.Msg != "" { // failed 有几种没有说法（界面只写结论，原文在详情里）
				checkHumanMsg(t, c.name, res.Msg)
			}
			if !strings.Contains(res.Msg, c.inMsg) {
				t.Errorf("说法 %q 里应有 %q", res.Msg, c.inMsg)
			}
			if res.Detail == "" {
				t.Errorf("没做成要有详情：%+v", res)
			}
			for _, s := range c.inDetail {
				if !strings.Contains(res.Detail, s) {
					t.Errorf("详情里应有 %q：%s", s, res.Detail)
				}
			}
		})
	}
}

// 交给页面的脚本：结果写成 JSON 对象，引号、换行、U+2028 都转义了，拼不坏脚本。
func TestDefenderDoneJS(t *testing.T) {
	r := defenderResult{Action: defenderAdd, Kind: dfFailed, Path: `D:\Tom's "MC"`, Msg: "x", Detail: "a\nb" + string(rune(0x2028)) + "c</script>"}
	js := defenderDoneJS(r)
	if !strings.HasPrefix(js, "window.__mdDefenderDone && window.__mdDefenderDone({") || !strings.HasSuffix(js, "})") {
		t.Fatalf("脚本的样子不对：%s", js)
	}
	if strings.ContainsAny(js, "\n"+string(rune(0x2028))) || strings.Contains(js, "</script>") {
		t.Errorf("换行、U+2028、</script> 应转义：%q", js)
	}
	var back defenderResult
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(js, "window.__mdDefenderDone && window.__mdDefenderDone("), ")")), &back); err != nil || back != r {
		t.Errorf("读回来 %+v（%v），应为 %+v", back, err, r)
	}
}

// useTempBase 把 app.Base 换成临时目录里一个带单引号的文件夹（config.json 落到这里），测试结束换回去、清掉记录、执行器换回一碰就失败的。
func useTempBase(t *testing.T) {
	t.Helper()
	oldBase, oldSave := app.Base, saveDefenderRecord
	dir := t.TempDir() // 先建：下面的 Cleanup 后登记、先执行，临时目录还在
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = nil; return nil })
		app.Base, saveDefenderRecord = oldBase, oldSave
		runElevated = refuseElevate
	})
	app.Base = filepath.Join(dir, "Tom's MineDeck")
	if err := os.MkdirAll(app.Base, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = nil; return nil }); err != nil {
		t.Fatal(err)
	}
}

// 用真的配置走一遍（执行器是假的）：加入做成了记进配置（带时间），撤销做成了清掉；没做成不动配置。
// 同一时间只做一件：上一次还没完时再点，回 busy，不再弹第二个 UAC；做完先解锁再交结果。
func TestStartDefenderExclusionRecordsConfig(t *testing.T) {
	useTempBase(t)
	release := make(chan struct{})
	var code uint32
	var runErr error
	var seen []string
	runElevated = func(hwnd uintptr, exe, params string) (uint32, error) {
		if hwnd != 0x1234 {
			t.Errorf("UAC 窗口应挂在主窗口上：%#x", hwnd)
		}
		seen = append(seen, params)
		<-release
		return code, runErr
	}
	results := make(chan defenderResult, 4)
	var unlockedAtDone []bool
	done := func(r defenderResult) {
		ok := defenderLock.TryLock() // 交结果的时候宿主应已经空出来
		if ok {
			defenderLock.Unlock()
		}
		unlockedAtDone = append(unlockedAtDone, ok)
		results <- r
	}
	start := func(action string) defenderResult {
		t.Helper()
		if s, err := startDefenderExclusion(action, 0x1234, done); err != nil || s != defenderStarted {
			t.Fatalf("%s 没开始：%q %v", action, s, err)
		}
		release <- struct{}{}
		select {
		case r := <-results:
			return r
		case <-time.After(10 * time.Second):
			t.Fatalf("%s 等不到结果", action)
		}
		return defenderResult{}
	}

	if _, err := startDefenderExclusion("disable", 0x1234, done); err == nil {
		t.Error("不认识的动作应直接拒绝")
	}

	// 正在做（UAC 窗口开着）时再点：回 busy（不是错误），页面接着等
	if s, err := startDefenderExclusion(defenderAdd, 0x1234, done); err != nil || s != defenderStarted {
		t.Fatal(s, err)
	}
	if s, err := startDefenderExclusion(defenderRemove, 0x1234, done); err != nil || s != defenderBusy {
		t.Errorf("上一次还没完时应回 busy：%q %v", s, err)
	}
	before := time.Now()
	release <- struct{}{}
	if r := <-results; r.Kind != dfOK || r.Path != app.Base {
		t.Fatalf("加入：%+v", r)
	}
	rec := app.GetConfig().DefenderExclusion
	if rec == nil || rec.Path != app.Base || rec.At.Before(before.Add(-time.Second)) || rec.At.After(time.Now()) {
		t.Fatalf("加入做成了应记下文件夹和时间：%+v", rec)
	}
	if b, err := os.ReadFile(filepath.Join(app.Base, "config.json")); err != nil || !strings.Contains(string(b), `"defenderExclusion"`) {
		t.Errorf("应写进 config.json：%v：%s", err, b)
	}

	// 撤销时 UAC 没通过：记录留着
	runErr = windows.ERROR_CANCELLED
	if r := start(defenderRemove); r.Kind != dfCancelled {
		t.Fatalf("撤销没通过 UAC：%+v", r)
	}
	if got := app.GetConfig().DefenderExclusion; got == nil || got.Path != app.Base {
		t.Fatalf("没通过 UAC，记录应留着：%+v", got)
	}

	// 撤销做成了：记录清掉
	runErr = nil
	if r := start(defenderRemove); r.Kind != dfOK {
		t.Fatalf("撤销：%+v", r)
	}
	if got := app.GetConfig().DefenderExclusion; got != nil {
		t.Fatalf("撤销做成了应清掉记录：%+v", got)
	}
	if len(seen) != 3 || !strings.Contains(seen[2], "Remove-MpPreference -ExclusionPath $p") || !strings.Contains(seen[2], "$p="+psQuote(app.Base)+";") {
		t.Errorf("撤销应删记着的那个文件夹：%q", seen)
	}

	// 加入时 Defender 没在工作：不记
	code = psExitNoService
	if r := start(defenderAdd); r.Kind != dfUnavailable {
		t.Fatalf("Defender 没在工作：%+v", r)
	}
	if got := app.GetConfig().DefenderExclusion; got != nil {
		t.Fatalf("没做成不该记：%+v", got)
	}
	for i, ok := range unlockedAtDone {
		if !ok {
			t.Errorf("第 %d 次交结果时宿主还占着：应先解锁再交结果", i+1)
		}
	}
}

// 做到一半出了意外（panic）：锁解得开，页面拿到一个失败的结果，程序不退出。
func TestStartDefenderExclusionRecoversPanic(t *testing.T) {
	useTempBase(t)
	runElevated = func(uintptr, string, string) (uint32, error) { panic("假的执行器出了意外") }
	results := make(chan defenderResult, 2)
	if s, err := startDefenderExclusion(defenderAdd, 0, func(r defenderResult) { results <- r }); err != nil || s != defenderStarted {
		t.Fatal(s, err)
	}
	select {
	case r := <-results:
		if r.Kind != dfFailed || !strings.Contains(r.Detail, "出了意外") {
			t.Errorf("出了意外应交回失败：%+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("等不到结果")
	}
	if !defenderLock.TryLock() {
		t.Fatal("出了意外以后锁应已解开")
	}
	defenderLock.Unlock()
	if got := app.GetConfig().DefenderExclusion; got != nil {
		t.Errorf("出了意外不该记：%+v", got)
	}
}

// 系统里已经改了、配置却记不下来：结果改成 unsaved，说清楚「已经加进去了 / 撤掉了，但没能记下来」，详情里有原因。
func TestDefenderUnsavedWhenConfigWriteFails(t *testing.T) {
	useTempBase(t)
	runElevated = func(uintptr, string, string) (uint32, error) { return 0, nil }
	saveDefenderRecord = func(*app.DefenderExclusion) error { return errors.New("磁盘满了") }
	for _, action := range []string{defenderAdd, defenderRemove} {
		r := runDefenderAction(action, 0)
		verb := map[string]string{defenderAdd: "已经加进去了", defenderRemove: "已经撤掉了"}[action]
		if r.Kind != dfUnsaved || !strings.Contains(r.Msg, verb) || !strings.Contains(r.Msg, "没能记下来") || !strings.Contains(r.Detail, "磁盘满了") {
			t.Errorf("%s 记不下来：%+v", action, r)
		}
		if n := utf8.RuneCountInString(r.Msg); n > 60 || strings.ContainsAny(r.Msg, `\/`) {
			t.Errorf("说法太长或带路径：%q", r.Msg)
		}
	}
}

// writeDefenderRecord 真的写不进 config.json（这里用一个叫 config.json 的文件夹挡着）时要报出来，写得进去时读回来对得上。
func TestWriteDefenderRecordDetectsFailure(t *testing.T) {
	useTempBase(t)
	cfg := filepath.Join(app.Base, "config.json")
	os.Remove(cfg)
	if err := os.Mkdir(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &app.DefenderExclusion{Path: app.Base, At: time.Date(2026, 11, 27, 14, 3, 0, 0, time.UTC)}
	if err := writeDefenderRecord(rec); err == nil {
		t.Fatal("config.json 写不进去时应报错")
	}
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeDefenderRecord(rec); err != nil {
		t.Fatalf("写得进去时不该报错：%v", err)
	}
	if err := writeDefenderRecord(nil); err != nil {
		t.Fatalf("清掉记录也要写得进去：%v", err)
	}
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "defenderExclusion") {
		t.Errorf("清掉以后 config.json 里不该还有：%s", b)
	}
}

// waitExitCode：进程结束了取退出码；超时按 errDefenderTimeout；不是「结束了」的一律按出错。
func TestWaitExitCode(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 7")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		t.Skip("起不了 cmd：", err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	if code, err := waitExitCode(h, 10*time.Second); err != nil || code != 7 {
		t.Errorf("退出码 %d，%v，应为 7", code, err)
	}
	windows.CloseHandle(h)
	_ = cmd.Wait()

	// 自己这个进程不会在 10 毫秒里结束：超时
	if _, err := waitExitCode(windows.CurrentProcess(), 10*time.Millisecond); !errors.Is(err, errDefenderTimeout) {
		t.Errorf("应超时：%v", err)
	}
	// 既不是「结束了」也不是超时的返回（这里是被遗弃的互斥量：WAIT_ABANDONED）：按出错，不去取退出码
	mu := make(chan windows.Handle)
	go func() {
		runtime.LockOSThread() // 不解锁就返回：这个线程跟着退出，它占着的互斥量就被遗弃了
		h, err := windows.CreateMutex(nil, true, nil)
		if err != nil {
			mu <- 0
			return
		}
		mu <- h
	}()
	m := <-mu
	if m == 0 {
		t.Fatal("建不了互斥量")
	}
	defer windows.CloseHandle(m)
	if _, err := waitExitCode(m, 5*time.Second); err == nil || errors.Is(err, errDefenderTimeout) || !strings.Contains(err.Error(), "0x80") {
		t.Errorf("WAIT_ABANDONED 应按出错（不是超时）：%v", err)
	}
	// 坏句柄：WAIT_FAILED，按出错
	if _, err := waitExitCode(windows.Handle(0xdead0), time.Second); err == nil {
		t.Error("坏句柄应报错")
	}
}

// 脚本里每一个退出码都在 classifyDefender 里有着落（20、21、22、24），exit 1 留给 PowerShell 自己。
func TestDefenderScriptExitCodesClassified(t *testing.T) {
	re := regexp.MustCompile(`exit (\d+)`)
	for _, action := range []string{defenderAdd, defenderRemove} {
		for _, m := range re.FindAllStringSubmatch(defenderScript(action, `E:\MineDeck`, `C:\o.txt`), -1) {
			var n uint32
			fmt.Sscan(m[1], &n)
			kind, _ := classifyDefender(action, n, nil)
			switch n {
			case 0:
				if kind != dfOK {
					t.Errorf("exit 0 应是做成了")
				}
			case 1:
				t.Errorf("脚本不该自己 exit 1（那是 PowerShell 自己出错的退出码）")
			case psExitNoService, psExitNoCmdlet:
				if kind != dfUnavailable {
					t.Errorf("exit %d 应是 Defender 没在工作：%s", n, kind)
				}
			case psExitCmdletErr, psExitVerify:
				if kind != dfFailed {
					t.Errorf("exit %d 应是失败：%s", n, kind)
				}
			default:
				t.Errorf("exit %d 没有专门的归类", n)
			}
		}
	}
}
