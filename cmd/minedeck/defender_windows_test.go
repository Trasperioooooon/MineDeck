package main

// 「加入 Windows 安全中心排除项」的测试。绝不真去提权、真去改 Defender：
//   - 下面的 init 先把 runElevated 换成一碰就失败的，哪个测试忘了换成假的，也只会失败，不会弹 UAC；
//   - 拼命令、分类、记状态都拿假的执行器测；
//   - 只有 TestDefenderScriptRealPowerShell 起真的 powershell.exe：不提权、不带任何 Mp 命令，只让 PowerShell 的解析器
//     读一遍拼出来的脚本（不执行），再让它把单引号字面量原样吐回来，确认转义在真的 Windows 命令行和 PowerShell 上都对。

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// 拼出来的参数交给真的 CommandLineToArgvW 拆开：前面几个开关原样，脚本是一个完整的参数；脚本里 -ExclusionPath 后面和 $o= 后面
// 按 PowerShell 的规矩读回来，正好是原来的文件夹和结果文件；路径里的东西没有漏到字面量外面去。
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
			i := strings.Index(script, cmdlet+" -ExclusionPath ")
			if i < 0 {
				t.Fatalf("%s：脚本里找不到 %s -ExclusionPath：%s", action, cmdlet, script)
			}
			got, rest := psLiteralAt(t, script[i+len(cmdlet+" -ExclusionPath "):])
			if got != p {
				t.Errorf("%s：-ExclusionPath 读回来是 %q，应为 %q", action, got, p)
			}
			if !strings.HasPrefix(rest, " -Force -ErrorAction Stop; exit 0 }") {
				t.Errorf("%s %q：路径后面应紧跟 -Force -ErrorAction Stop，实际 %.40q", action, p, rest)
			}
			j := strings.Index(script, "$o=")
			if gotOut, rest := psLiteralAt(t, script[j+3:]); gotOut != out || !strings.HasPrefix(rest, "; ") {
				t.Errorf("$o 读回来是 %q，应为 %q", gotOut, out)
			}
			if other := map[string]string{defenderAdd: "Remove-MpPreference", defenderRemove: "Add-MpPreference"}[action]; strings.Contains(script, other) {
				t.Errorf("%s 的脚本里不该出现 %s", action, other)
			}
		}
	}
}

// 真的 PowerShell（不提权、不带 Mp 命令）：
//  1. 解析器读一遍 defenderScript 拼出来的脚本（只解析、不执行），没有语法错，里面的命令只有那几个，-ExclusionPath 和 $o 的值是原样的；
//  2. 照 defenderParams 的拼法（同一个 psCommandParams）起 powershell.exe，让它把 psQuote 的字面量按 UTF-8 转 Base64 吐回来，
//     和原来的一样：Windows 命令行和 PowerShell 两层的转义都对。
func TestDefenderScriptRealPowerShell(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：不起 PowerShell")
	}
	exe := powershellPath()
	if _, err := os.Stat(exe); err != nil {
		t.Skip("这台电脑上没有 Windows PowerShell：", err)
	}
	out := `C:\Users\张三 O’Neil\AppData\Local\Temp\minedeck-defender-1.txt`

	// 1. 只解析：脚本写成 JSON、转 Base64 从标准输入交过去（原样一个字节不差，也没有环境变量 32K 字符的上限）；
	//    结果同样转成 JSON 再 Base64，不受控制台代码页影响
	type check struct {
		Errors   int      `json:"errors"`
		Commands []string `json:"commands"`
		Path     string   `json:"path"`
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
		`$p=$null; foreach ($c in $cmds) { $el=$c.CommandElements; for ($i=0; $i -lt $el.Count-1; $i++) { ` +
		`if ($el[$i] -is [System.Management.Automation.Language.CommandParameterAst] -and $el[$i].ParameterName -eq 'ExclusionPath') { $p=$el[$i+1].Value } } }; ` +
		`$asg=@($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.AssignmentStatementAst] -and $n.Left.VariablePath.UserPath -eq 'o' }, $true)); ` +
		`[pscustomobject]@{ errors=@($er).Count; commands=@($cmds | ForEach-Object { $_.GetCommandName() }); path=$p; out=$asg[0].Right.Expression.Value } }; ` +
		`[Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -InputObject @($res) -Depth 4 -Compress))))`
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(exe) + " " + psCommandParams(parser), CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(js))
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("PowerShell 解析脚本失败：%v：%s", err, se.String())
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(so.String()))
	if err != nil {
		t.Fatalf("PowerShell 的输出读不懂：%q（%s）", so.String(), se.String())
	}
	var checks []check
	if err := json.Unmarshal(raw, &checks); err != nil || len(checks) != len(scripts) {
		t.Fatalf("解析结果对不上：%v，%d 条：%s", err, len(checks), raw)
	}
	allowed := map[string]bool{"Get-Service": true, "Set-Content": true, "Add-MpPreference": true, "Remove-MpPreference": true}
	for i, c := range checks {
		if c.Errors != 0 {
			t.Errorf("%q：PowerShell 说脚本有 %d 处语法错：%s", paths[i], c.Errors, scripts[i])
		}
		for _, name := range c.Commands {
			if !allowed[name] {
				t.Errorf("%q：脚本里冒出了别的命令 %q（转义漏了）：%s", paths[i], name, scripts[i])
			}
		}
		if c.Path != paths[i] {
			t.Errorf("PowerShell 读到的 -ExclusionPath 是 %q，应为 %q", c.Path, paths[i])
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
	cmd = exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(exe) + " " + psCommandParams(echo), CreationFlags: windows.CREATE_NO_WINDOW}
	so.Reset()
	se.Reset()
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := cmd.Run(); err != nil {
		t.Fatalf("PowerShell 回显失败：%v：%s", err, se.String())
	}
	raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(so.String()))
	if err != nil {
		t.Fatalf("PowerShell 的输出读不懂：%q（%s）", so.String(), se.String())
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
		{"UAC 里点了否", 0, windows.ERROR_CANCELLED, dfCancelled, "「否」", defenderAdd},
		{"撤销时 UAC 里点了否", 0, windows.ERROR_CANCELLED, dfCancelled, "「否」", defenderRemove},
		{"包了一层的 ERROR_CANCELLED 也认", 0, errors.Join(errors.New("x"), windows.ERROR_CANCELLED), dfCancelled, "「否」", defenderAdd},
		{"组策略拦下 PowerShell", 0, windows.ERROR_ACCESS_DISABLED_BY_POLICY, dfBlocked, "组策略", defenderAdd},
		{"软件限制策略（不弹窗的那种）", 0, windows.ERROR_ACCESS_DISABLED_NO_SAFER_UI_BY_POLICY, dfBlocked, "手动添加", defenderAdd},
		{"应用程序控制拦下", 0, windows.ERROR_SYSTEM_INTEGRITY_POLICY_VIOLATION, dfBlocked, "手动删除", defenderRemove},
		{"找不到 PowerShell", 0, windows.ERROR_FILE_NOT_FOUND, dfFailed, "找不到 PowerShell", defenderAdd},
		{"找不到 PowerShell 的文件夹", 0, windows.ERROR_PATH_NOT_FOUND, dfFailed, "找不到 PowerShell", defenderAdd},
		{"等太久", 0, errDefenderTimeout, dfFailed, "两分钟", defenderAdd},
		{"别的系统错误", 0, windows.ERROR_ACCESS_DENIED, dfFailed, "手动添加", defenderAdd},
		{"别的错误", 0, errors.New("x"), dfFailed, "手动删除", defenderRemove},
		{"WinDefend 服务没在跑", psExitNoService, nil, dfUnavailable, "别的杀毒软件", defenderAdd},
		{"没有 Defender 的命令", psExitNoCmdlet, nil, dfUnavailable, "别的杀毒软件", defenderRemove},
		{"Add-MpPreference 报错", psExitCmdletErr, nil, dfFailed, "手动添加", defenderAdd},
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
	for _, bad := range []string{
		`C:\`, `D:\`, `d:`, `\\server\share`, `\\server\share\`,
		`C:\Users\张三`, `c:\users\张三\`, `C:\Users`, `C:\Users\张三\Desktop`, `C:\USERS\张三\DOWNLOADS`,
		`C:\Users\张三\OneDrive`, `C:\Windows`, `C:\Program Files`, ``, `relative\dir`,
	} {
		if broadReason(bad, broad) == "" {
			t.Errorf("%q 不该整个排除", bad)
		}
	}
	for _, ok := range []string{
		`E:\MineDeck`, `C:\Users\张三\Desktop\MineDeck`, `C:\Users\张三\Downloads\MineDeck_3.2.9`, `C:\Program Files\MineDeck`,
		`C:\Users\张三\Desktop2`, `C:\Windows2\x`, `\\server\share\MineDeck`, `D:\我的世界\开服 工具\`,
	} {
		if why := broadReason(ok, broad); why != "" {
			t.Errorf("%q 应该能加：%s", ok, why)
		}
	}
}

// fakeRun 假的执行器：记下每次的参数，往结果文件里写 out（模拟脚本出错时写的原文），返回 code / err。
type fakeRun struct {
	t       *testing.T
	dir     string
	code    uint32
	err     error
	out     string
	calls   []string
	outSeen []string
}

func (f *fakeRun) run(exe, params string) (uint32, error) {
	f.calls = append(f.calls, params)
	if !strings.EqualFold(filepath.Base(exe), "powershell.exe") {
		f.t.Errorf("跑的应是 powershell.exe，实际 %q", exe)
	}
	m, _ := filepath.Glob(filepath.Join(f.dir, "minedeck-defender-*.txt"))
	if len(m) != 1 {
		f.t.Errorf("提权之前应正好建好一个结果文件，实际 %v", m)
	} else {
		f.outSeen = append(f.outSeen, m[0])
		if !strings.Contains(params, psQuote(m[0])) {
			f.t.Errorf("参数里应带着结果文件 %s：%s", m[0], params)
		}
		if f.out != "" {
			// PowerShell 5.1 的 Set-Content -Encoding UTF8 带 BOM
			if err := os.WriteFile(m[0], append([]byte("\xef\xbb\xbf"), []byte(f.out+"\r\n")...), 0o644); err != nil {
				f.t.Fatal(err)
			}
		}
	}
	return f.code, f.err
}

func TestDoDefenderAction(t *testing.T) {
	t0 := time.Date(2026, 11, 27, 14, 3, 0, 0, time.Local)
	base := `D:\我的世界\Tom's MineDeck`
	old := &app.DefenderExclusion{Path: `E:\旧位置\MineDeck`, At: t0.Add(-48 * time.Hour)}
	same := &app.DefenderExclusion{Path: `d:\我的世界\tom's minedeck\`, At: t0.Add(-time.Hour)}
	broad := []string{`C:\Users\张三`, `C:\Users\张三\Downloads`}
	cases := []struct {
		name     string
		action   string
		base     string
		record   *app.DefenderExclusion
		code     uint32
		err      error
		out      string
		kind     string
		target   string                 // 这次处理的文件夹（没去跑时也看 res.Path）
		ran      bool                   // 有没有去提权
		changed  bool                   // 配置要不要改
		rec      *app.DefenderExclusion // 改成什么（changed 为 false 时应原样是 record）
		inDetail []string
	}{
		{name: "加入：做成了，记下文件夹和时间", action: defenderAdd, base: base, kind: dfOK, target: base, ran: true, changed: true,
			rec: &app.DefenderExclusion{Path: base, At: t0}},
		{name: "已经加过再加一次：照样跑（Add-MpPreference 本身不报错），时间更新", action: defenderAdd, base: base, record: same, kind: dfOK, target: base,
			ran: true, changed: true, rec: &app.DefenderExclusion{Path: base, At: t0}},
		{name: "加入：UAC 里点了否，什么都不记", action: defenderAdd, base: base, err: windows.ERROR_CANCELLED, kind: dfCancelled, target: base, ran: true,
			inDetail: []string{"1223", base}},
		{name: "加入：Defender 服务没在跑", action: defenderAdd, base: base, code: psExitNoService, out: "WinDefend service status: Stopped",
			kind: dfUnavailable, target: base, ran: true, inDetail: []string{"WinDefend service status: Stopped", "退出码：20"}},
		{name: "加入：没有 Defender 的命令", action: defenderAdd, base: base, code: psExitNoCmdlet, out: "无法将“Add-MpPreference”项识别为 cmdlet | CommandNotFoundException",
			kind: dfUnavailable, target: base, ran: true, inDetail: []string{"CommandNotFoundException"}},
		{name: "加入：Add-MpPreference 报错，原文进详情", action: defenderAdd, base: base, code: psExitCmdletErr,
			out: "操作失败，出现以下错误: 0x800106ba。 | HRESULT 0x800106ba,Add-MpPreference | HResult=0x80131500", kind: dfFailed, target: base, ran: true,
			inDetail: []string{"0x800106ba", "退出码：22", "以管理员身份运行："}},
		{name: "加入：PowerShell 被组策略拦下", action: defenderAdd, base: base, err: windows.ERROR_ACCESS_DISABLED_BY_POLICY, kind: dfBlocked, target: base, ran: true,
			inDetail: []string{"1260"}},
		{name: "加入：等太久", action: defenderAdd, base: base, err: errDefenderTimeout, kind: dfFailed, target: base, ran: true, inDetail: []string{"两分钟"}},
		{name: "加入：记着的是另一个位置，不替它加", action: defenderAdd, base: base, record: old, kind: dfRefused, target: base,
			inDetail: []string{old.Path, base}},
		{name: "加入：路径是空的记录（手改坏的配置）当没记过，照样加", action: defenderAdd, base: base, record: &app.DefenderExclusion{At: t0}, kind: dfOK,
			target: base, ran: true, changed: true, rec: &app.DefenderExclusion{Path: base, At: t0}},
		{name: "加入：磁盘根目录不加", action: defenderAdd, base: `D:\`, kind: dfRefused, target: `D:\`, inDetail: []string{"磁盘根目录"}},
		{name: "加入：程序就放在下载文件夹里，不加", action: defenderAdd, base: `C:\Users\张三\Downloads`, kind: dfRefused, target: `C:\Users\张三\Downloads`,
			inDetail: []string{"系统文件夹"}},
		{name: "加入：用户文件夹的上一层，不加", action: defenderAdd, base: `C:\Users`, kind: dfRefused, target: `C:\Users`},
		{name: "加入：末尾带反斜杠的照样认", action: defenderAdd, base: `E:\MineDeck\`, kind: dfOK, target: `E:\MineDeck`, ran: true, changed: true,
			rec: &app.DefenderExclusion{Path: `E:\MineDeck`, At: t0}},
		{name: "撤销：删记着的那一条（文件夹搬过，删旧位置），清掉记录", action: defenderRemove, base: base, record: old, kind: dfOK, target: old.Path,
			ran: true, changed: true, rec: nil},
		{name: "撤销一个没加过的：删现在的文件夹，不算错", action: defenderRemove, base: base, kind: dfOK, target: base, ran: true, changed: true, rec: nil},
		{name: "撤销：UAC 里点了否，记录留着", action: defenderRemove, base: base, record: same, err: windows.ERROR_CANCELLED, kind: dfCancelled,
			target: same.Path, ran: true},
		{name: "撤销：Defender 没在工作，记录留着", action: defenderRemove, base: base, record: same, code: psExitNoService, kind: dfUnavailable,
			target: same.Path, ran: true},
		{name: "撤销：Remove-MpPreference 报错，记录留着", action: defenderRemove, base: base, record: same, code: psExitCmdletErr, out: "拒绝访问。",
			kind: dfFailed, target: same.Path, ran: true, inDetail: []string{"拒绝访问。"}},
		{name: "撤销：程序数据文件夹没定下来，不拿相对路径去删", action: defenderRemove, base: "", kind: dfFailed, target: "."},
		{name: "不认识的动作", action: "set", base: base, kind: dfFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			f := &fakeRun{t: t, dir: dir, code: c.code, err: c.err, out: c.out}
			env := defenderEnv{base: c.base, record: c.record, broad: broad, exe: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
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
				if !strings.Contains(f.calls[0], " -ExclusionPath "+psQuote(c.target)+" -Force -ErrorAction Stop;") {
					t.Errorf("参数里应带着 %s：%s", psQuote(c.target), f.calls[0])
				}
				cmdlet := map[string]string{defenderAdd: "Add-MpPreference", defenderRemove: "Remove-MpPreference"}[c.action]
				if !strings.Contains(f.calls[0], cmdlet+" -ExclusionPath") {
					t.Errorf("应跑 %s：%s", cmdlet, f.calls[0])
				}
				for _, p := range f.outSeen {
					if _, err := os.Stat(p); !os.IsNotExist(err) {
						t.Errorf("结果文件用完应删掉：%s（%v）", p, err)
					}
				}
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

// 读回结果文件：去掉 BOM 和首尾空白；超过上限时截在完整的字上；没有文件是空串。
func TestReadDefenderOut(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "o.txt")
	if got := readDefenderOut(p); got != "" {
		t.Errorf("没有文件应为空串：%q", got)
	}
	os.WriteFile(p, []byte("\xef\xbb\xbf拒绝访问。\r\n"), 0o644)
	if got := readDefenderOut(p); got != "拒绝访问。" {
		t.Errorf("读回 %q", got)
	}
	os.WriteFile(p, []byte(strings.Repeat("长", defenderOutMax)), 0o644)
	if got := readDefenderOut(p); !utf8.ValidString(got) || len(got) > defenderOutMax || len(got) < defenderOutMax-3 {
		t.Errorf("超长时应截在完整的字上：%d 字节，合法 %v", len(got), utf8.ValidString(got))
	}
}

// 交给页面的脚本：结果写成 JSON 对象，引号、换行、U+2028 都转义了，拼不坏脚本。
func TestDefenderDoneJS(t *testing.T) {
	r := defenderResult{Action: defenderAdd, Kind: dfFailed, Path: `D:\Tom's "MC"`, Msg: "x", Detail: "a\nb\u2028c</script>"}
	js := defenderDoneJS(r)
	if !strings.HasPrefix(js, "window.__mdDefenderDone && window.__mdDefenderDone({") || !strings.HasSuffix(js, "})") {
		t.Fatalf("脚本的样子不对：%s", js)
	}
	if strings.ContainsAny(js, "\n\u2028") || strings.Contains(js, "</script>") {
		t.Errorf("换行、U+2028、</script> 应转义：%q", js)
	}
	var back defenderResult
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(js, "window.__mdDefenderDone && window.__mdDefenderDone("), ")")), &back); err != nil || back != r {
		t.Errorf("读回来 %+v（%v），应为 %+v", back, err, r)
	}
}

// 用真的配置走一遍（执行器是假的）：加入做成了记进配置（带时间），撤销做成了清掉；没做成不动配置。
// 同一时间只做一件：上一次还没完时再点，直接说「还没做完」，不再弹第二个 UAC。
func TestStartDefenderExclusionRecordsConfig(t *testing.T) {
	oldBase := app.Base
	dir := t.TempDir() // 先建：下面的 Cleanup 后登记、先执行，临时目录还在
	t.Cleanup(func() {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = nil; return nil })
		app.Base = oldBase
		runElevated = refuseElevate
	})
	app.Base = filepath.Join(dir, "Tom's MineDeck") // config.json 落到临时目录
	if err := os.MkdirAll(app.Base, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = nil; return nil }); err != nil {
		t.Fatal(err)
	}

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
	done := func(r defenderResult) { results <- r }
	start := func(action string) defenderResult {
		t.Helper()
		if err := startDefenderExclusion(action, 0x1234, done); err != nil {
			t.Fatalf("%s 没开始：%v", action, err)
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

	if err := startDefenderExclusion("disable", 0x1234, done); err == nil {
		t.Error("不认识的动作应直接拒绝")
	}

	// 正在做（UAC 窗口开着）时再点：直接说还没做完
	if err := startDefenderExclusion(defenderAdd, 0x1234, done); err != nil {
		t.Fatal(err)
	}
	if err := startDefenderExclusion(defenderRemove, 0x1234, done); err == nil || !strings.Contains(err.Error(), "还没做完") {
		t.Errorf("上一次还没完时应说还没做完：%v", err)
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

	// 撤销时在 UAC 里点了否：记录留着
	runErr = windows.ERROR_CANCELLED
	if r := start(defenderRemove); r.Kind != dfCancelled {
		t.Fatalf("撤销点了否：%+v", r)
	}
	if got := app.GetConfig().DefenderExclusion; got == nil || got.Path != app.Base {
		t.Fatalf("点了否，记录应留着：%+v", got)
	}

	// 撤销做成了：记录清掉
	runErr = nil
	if r := start(defenderRemove); r.Kind != dfOK {
		t.Fatalf("撤销：%+v", r)
	}
	if got := app.GetConfig().DefenderExclusion; got != nil {
		t.Fatalf("撤销做成了应清掉记录：%+v", got)
	}
	if len(seen) != 3 || !strings.Contains(seen[2], "Remove-MpPreference -ExclusionPath "+psQuote(app.Base)) {
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
}
