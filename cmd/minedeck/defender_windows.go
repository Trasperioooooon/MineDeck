package main

// 把 MineDeck 的程序数据文件夹加进 Windows 安全中心（Microsoft Defender）的排除项，或者撤销（窗口版「设置 → 存储位置」）。
//
// 为什么要：实时防护每读写一个文件都要扫一遍，开服、存档会慢一些；frpc 这类穿透程序还常被误报、隔离。加了排除项，
// 这个文件夹里的东西就不扫了。放在别处的服务器和备份（新建时、设置里都能改存放位置）不在里面，也不自动替它们加。
//
// 怎么做：改排除项要管理员权限。用 ShellExecuteEx 的 runas 以管理员身份跑 PowerShell 的 Add-MpPreference /
// Remove-MpPreference，弹的是系统的 UAC 确认窗口；等它跑完，按结果分成：做成了、没通过 UAC、Defender 没在做实时防护
// （装了别的杀毒软件、被关掉）、powershell.exe 被策略禁止启动、别的失败。改完在同一个提权的 PowerShell 里回读排除项核对一遍。
// 提权起来的进程拿不到它的输出，报错原文由脚本写进一个临时文件，读回来放进界面的「详情」。命令写成明文的 -Command
// （不用 -EncodedCommand）：UAC 窗口的「显示更多详细信息」里看得懂要做什么，而且「编码过的 PowerShell 加 Defender 排除项」
// 正是杀毒软件盯着的写法。
//
// 只能人点：这是改系统安全设置的事。入口是宿主函数 hostDefenderExclusion（webview_windows.go 里 Bind），只有原生窗口里的
// 页面调得到；没有登记进操作层（internal/ops/catalog），AI 助手、MCP、局域网登录的手机都调不到。页面只说「加入」还是「撤销」，
// 不给路径：路径由这里定（app.Base，撤销时是记下来的那一条），页面里的脚本借它排除不了别的文件夹。
//
// 加没加过：不提权读不到排除项（非管理员下 Get-MpPreference 的 ExclusionPath 是一句「N/A: Must be an administrator…」），
// 所以只记 MineDeck 自己做成的结果（配置里的 defenderExclusion，带时间），界面照这个显示，不假装查得到系统里的真实状态。

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"

	"minedeck/internal/app"
)

// 页面交来的动作。
const (
	defenderAdd    = "add"    // 加入排除项
	defenderRemove = "remove" // 撤销
)

// 结果的种类，页面按它挑说法和提示的样子（cancelled 是用户自己选的，不按报错提示）。
const (
	dfOK          = "ok"          // 做成了
	dfCancelled   = "cancelled"   // 没通过 UAC 的确认（点了「否」，或者确认被取消了）
	dfUnavailable = "unavailable" // Windows 安全中心（Defender）没在做实时防护
	dfBlocked     = "blocked"     // powershell.exe 本身被 AppLocker / 软件限制策略禁止启动
	dfRefused     = "refused"     // 没去做：文件夹太大（磁盘根目录、桌面、下载……），或者记着的是另一个位置
	dfFailed      = "failed"      // 别的失败
	dfUnsaved     = "unsaved"     // 系统里已经改了，但没能记进配置（config.json 写不进去）
)

// 宿主函数马上交给页面的回话（结果另由 window.__mdDefenderDone 交）。
const (
	defenderStarted = "started" // 开始了，等 UAC
	defenderBusy    = "busy"    // 上一次的还没做完（UAC 窗口开着时刷新了页面又点一次）：页面接着等上一次的结果
)

// 脚本自己定的退出码。不用 1：PowerShell 自己出错（命令解析不了等）时退出码是 1，分不清是谁。
const (
	psExitNoService = 20 // WinDefend 服务没装、没在运行，或者（加入时）Defender 处在被动模式、实时防护关着：加了也不起作用
	psExitNoCmdlet  = 21 // 没有 Defender 的 PowerShell 命令，或者它的 WMI 命名空间不在（精简过的系统、去掉了 Defender 的服务器版）
	psExitCmdletErr = 22 // Add-MpPreference / Remove-MpPreference 报错，原文在结果文件里
	psExitVerify    = 24 // 改完回读排除项，读得到、却和要的对不上（加了没在里面 / 撤了还在里面）
)

// defenderWait 等提权起来的 PowerShell 跑完最多等多久。UAC 窗口开着的时间不算在里面（ShellExecuteEx 等用户点完才返回）；
// 平时一两秒就完，等这么久还没完的多半是 Defender 卡住了，不再等，按失败说。进程不结束它：它可能过一会儿才改成。
const defenderWait = 2 * time.Minute

// defenderOutMax 结果文件最多读多少字节。正常的报错一两行；读多了界面的「详情」里也放不下。
const defenderOutMax = 8 << 10

var errDefenderTimeout = errors.New("等了两分钟，提权的 PowerShell 还没结束（没有结束它，可能还在跑）")

// defenderResult 交给页面的结果（window.__mdDefenderDone 收）。
type defenderResult struct {
	Action string `json:"action"`           // add | remove
	Kind   string `json:"kind"`             // 见上面的 df*
	Path   string `json:"path"`             // 这次处理的文件夹
	Msg    string `json:"msg,omitempty"`    // 给人看的一句话：不超过 60 个字、不带路径和英文原文，界面接在结论后面
	Detail string `json:"detail,omitempty"` // 原文，放进「详情」
}

// defenderEnv 做一次加入或撤销要用到的外部东西，测试里全换成假的。
type defenderEnv struct {
	base    string                                   // 程序数据文件夹（app.Base）
	record  *app.DefenderExclusion                   // 配置里记着的那一条（没加过是 nil）
	broad   []string                                 // 不许整个排除的系统文件夹（knownBroadFolders）
	resolve func(string) string                      // 文件夹的真实路径（nil = canonicalDir）
	exe     string                                   // powershell.exe 的完整路径
	tempDir string                                   // 结果文件放哪（空 = 系统临时文件夹）
	run     func(exe, params string) (uint32, error) // 以管理员身份跑 exe 并等它结束，返回退出码
	now     func() time.Time
}

// psQuote 把 s 写成 PowerShell 的单引号字符串字面量：里面的单引号写两遍。PowerShell 把弯引号 ‘ ’ ‚ ‛ 也当单引号，
// 这几个也要写两遍，不然文件夹名里带一个 ’ 就能把字符串截断、后面的字当命令跑。单引号字符串里 $、`、反斜杠、双引号都不特殊，原样写。
// （仓库里别处没有第二份：internal/hostdlg/fallback_windows.go 那边的标题走环境变量，不拼进命令，用不着转义。）
func psQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for _, r := range s {
		b.WriteRune(r)
		if isPSSingleQuote(r) {
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// isPSSingleQuote PowerShell 认作单引号的字符（和它的 CodeGeneration.EscapeSingleQuotedStringContent 一致）：
// ' 和 U+2018、U+2019、U+201A、U+201B。
func isPSSingleQuote(r rune) bool {
	switch r {
	case '\'', 0x2018, 0x2019, 0x201A, 0x201B:
		return true
	}
	return false
}

// psSaveResult 脚本里写报错原文的函数。用 Out-File：它打开文件时允许别人读，这边一直拿着结果文件的读句柄（见 openResultFile）
// 也写得进去；Set-Content、Add-Content 不允许，会撞上共享冲突（2026-11-27 在本机试过，不提权、不碰 Defender）。
// 写不进去也不改退出码（-ErrorAction SilentlyContinue），界面照样分得清是哪种失败，只是「详情」里少了原文。
const psSaveResult = "function Save-Result($t) { Out-File -LiteralPath $o -Encoding UTF8 -ErrorAction SilentlyContinue -InputObject $t }"

// defenderScript 拼出要以管理员身份跑的那段 PowerShell：
//  1. WinDefend 服务没装、没在运行，就是 Defender 没在工作（不先查的话 Add-MpPreference 报的是一串十六进制的错误码，看不出是这个原因）；
//  2. 没有 Defender 的命令（Get-Command 查不到），也算 Defender 没在工作；
//  3. 加入时再看 Get-MpComputerStatus：装了别的杀毒软件时 Defender 处在被动模式（AMRunningMode 不是 Normal），
//     或者实时防护被关掉了，服务照样在跑、Add-MpPreference 照样成功，可加了不起作用，真正在扫的杀毒软件也没加。撤销不拦；
//  4. 加或删。失败时给的是不终止的错误，除了开头的 $ErrorActionPreference，命令上再写一遍 -ErrorAction Stop，
//     不然万一没进 catch，脚本照样 exit 0，这边就把没加上的记成「已加入」。catch 里先把出错处理改回 SilentlyContinue 再拼报错：
//     拼的时候再出错不会把退出码变成 1。WMI 命名空间不在（0x8004100E）也算没有 Defender；
//  5. 回读 (Get-MpPreference).ExclusionPath 核对（末尾带不带反斜杠、大小写都算一样）。读不到列表（空的，或者只有一句「N/A…」：
//     有的策略下排除项对本机管理员也是藏着的）不当失败；读得到、又对不上时退出 24，原因写进结果文件。
//
// 整段不含双引号：交给 windows.EscapeArg 包成一个参数时不用再转义里面的东西。
func defenderScript(action, path, outFile string) string {
	cmdlet := "Add-MpPreference"
	if action == defenderRemove {
		cmdlet = "Remove-MpPreference"
	}
	code := func(n int) string { return fmt.Sprint(n) }
	var b strings.Builder
	b.WriteString("$ErrorActionPreference='Stop'; $o=" + psQuote(outFile) + "; $p=" + psQuote(path) + "; " + psSaveResult + "; ")
	b.WriteString("$s=Get-Service -Name WinDefend -ErrorAction SilentlyContinue; ")
	b.WriteString("if (-not $s) { Save-Result 'WinDefend service not installed'; exit " + code(psExitNoService) + " }; ")
	b.WriteString("if ($s.Status -ne 'Running') { Save-Result ('WinDefend service status: ' + $s.Status); exit " + code(psExitNoService) + " }; ")
	b.WriteString("if (-not (Get-Command " + cmdlet + " -ErrorAction SilentlyContinue)) { Save-Result '" + cmdlet + " not found'; exit " + code(psExitNoCmdlet) + " }; ")
	if action == defenderAdd {
		b.WriteString("try { $st=Get-MpComputerStatus -ErrorAction Stop } catch { Save-Result ('Get-MpComputerStatus: ' + $_.Exception.Message); exit " + code(psExitNoService) + " }; ")
		b.WriteString("if (($st.AMRunningMode -and $st.AMRunningMode -ne 'Normal') -or -not $st.RealTimeProtectionEnabled) { " +
			"Save-Result ('AMRunningMode: ' + $st.AMRunningMode + ' | RealTimeProtectionEnabled: ' + $st.RealTimeProtectionEnabled); exit " + code(psExitNoService) + " }; ")
	}
	b.WriteString("try { " + cmdlet + " -ExclusionPath $p -Force -ErrorAction Stop } catch { $ErrorActionPreference='SilentlyContinue'; $e=$_; " +
		"$m=[string]$e.Exception.Message; $m+=' | '+[string]$e.FullyQualifiedErrorId; $m+=' | HResult=0x'+('{0:X8}' -f $e.Exception.HResult); Save-Result $m; " +
		"if ($e.FullyQualifiedErrorId -eq 'CommandNotFoundException' -or $m -match '0x8004100e' -or [string]$e.Exception.NativeErrorCode -eq 'InvalidNamespace') { exit " + code(psExitNoCmdlet) + " }; " +
		"exit " + code(psExitCmdletErr) + " }; ")
	// 改完回读核对。读不到列表（Get-MpPreference 报错，或者策略对本地管理员藏起了排除项、读出来是一句「N/A…」）时不核对；
	// 读得到的空列表照样核对：大多数电脑原本一条排除项都没有，加完还是空的，正说明没加进去（复审第二轮指出的）
	b.WriteString("$ErrorActionPreference='SilentlyContinue'; $mp=$null; try { $mp=Get-MpPreference -ErrorAction Stop } catch {}; " +
		"$l=@($mp.ExclusionPath | Where-Object { $_ }); $w=$p.TrimEnd('\\'); " +
		"$hidden=(-not $mp) -or (@($l | Where-Object { ([string]$_).StartsWith('N/A') }).Count -gt 0); " +
		"$has=@($l | Where-Object { ([string]$_).TrimEnd('\\') -ieq $w }).Count -gt 0; ")
	if action == defenderAdd {
		b.WriteString("if (-not $hidden -and -not $has) { Save-Result ('verify: ' + $p + ' is not in ExclusionPath after Add-MpPreference'); exit " + code(psExitVerify) + " }; ")
	} else {
		b.WriteString("if ($has) { Save-Result ('verify: ' + $p + ' is still in ExclusionPath after Remove-MpPreference'); exit " + code(psExitVerify) + " }; ")
	}
	b.WriteString("exit 0")
	return b.String()
}

// psCommandParams 让 powershell.exe 跑 script 的整条参数（ShellExecuteEx 的 lpParameters）。脚本作为 -Command 后面的一个参数，
// 由 windows.EscapeArg 按 Windows 拆命令行的规矩包好（带空格就整个加双引号，双引号前的反斜杠加倍）。
// -WindowStyle Hidden 配合 ShellExecuteEx 的 SW_HIDE：提权起来的 PowerShell 不冒黑窗口。
func psCommandParams(script string) string {
	return "-NoProfile -NonInteractive -WindowStyle Hidden -Command " + windows.EscapeArg(script)
}

// defenderParams 加入或撤销 path 的整条参数，报错原文写进 outFile。
func defenderParams(action, path, outFile string) string {
	return psCommandParams(defenderScript(action, path, outFile))
}

// stripVerbatim 去掉 \\?\ 前缀（\\?\UNC\server\share 写回 \\server\share）。
func stripVerbatim(p string) string {
	switch {
	case len(p) >= 8 && strings.EqualFold(p[:8], `\\?\UNC\`):
		return `\\` + p[8:]
	case strings.HasPrefix(p, `\\?\`):
		return p[4:]
	}
	return p
}

// canonicalDir 文件夹的真实路径：打开它问系统最终的路径（GetFinalPathNameByHandle），目录联接、符号链接、subst 盘符、
// 映射的网络盘、8.3 短名都解开。打不开（不存在、没权限）时退回 filepath.EvalSymlinks，再不行就只整理写法（filepath.Clean）。
// 只拿来比较「是不是同一个文件夹」「是不是系统文件夹」；加进排除项的还是原来的写法（app.Base），界面拿它和记录对照。
func canonicalDir(p string) string {
	c := filepath.Clean(stripVerbatim(p))
	if p == "" || !filepath.IsAbs(c) {
		return c
	}
	if f, ok := finalPathOf(c); ok {
		return f
	}
	if e, err := filepath.EvalSymlinks(c); err == nil {
		return filepath.Clean(stripVerbatim(e))
	}
	return c
}

// finalPathOf 打开 p（文件夹也行，跟着联接和链接走）问它最终的路径。
func finalPathOf(p string) (string, bool) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	h, err := windows.CreateFile(u, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0) // 0 = FILE_NAME_NORMALIZED | VOLUME_NAME_DOS
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			return filepath.Clean(stripVerbatim(windows.UTF16ToString(buf[:n]))), true
		}
		buf = make([]uint16, n+1)
	}
}

// pathForms 一个路径拿来比较的两种写法：整理过的原样写法、解开以后的真实路径（都转小写）。
func pathForms(p string, resolve func(string) string) []string {
	c := strings.ToLower(filepath.Clean(stripVerbatim(p)))
	r := strings.ToLower(resolve(p))
	if r == c || r == "" {
		return []string{c}
	}
	return []string{c, r}
}

// samePath 记着的排除项和现在的程序数据文件夹是不是同一条：只比整理过的原样写法（不分大小写、末尾的反斜杠和 \\?\ 前缀不算），
// 不解开联接、短名、subst。Defender 的排除项存的就是这串字，换了写法（比如以前从 subst 盘符 M:\MineDeck 加的，现在从真实路径启动）
// 在 Defender 那边是两条：要先撤掉旧写法的那条再加新的，不然旧的那条留在系统里、MineDeck 也不再记得它。页面上 sameDir 也是这样比的。
// 「太大的文件夹不加」那边（broadReason）照旧解开真实路径再比，防的是用别名绕过去。
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(stripVerbatim(a)), filepath.Clean(stripVerbatim(b)))
}

// isVolumeRoot 是不是磁盘根目录（D:\、\\server\share\）。
func isVolumeRoot(p string) bool {
	p = filepath.Clean(stripVerbatim(p))
	rest := p[len(filepath.VolumeName(p)):]
	return rest == "" || rest == `\` || rest == "/"
}

// broadReason 程序数据文件夹能不能整个排除：MineDeck 是绿色版，有人直接把它放在磁盘根目录、桌面、「下载」里，
// 那样加进排除项等于整个盘、整个下载文件夹都不扫了，比不加还糟。是磁盘根目录，或者就是某个系统文件夹、或者是它的上一层时，
// 返回原因（写进「详情」）；能加时返回空串。broad 是这台电脑上的那些系统文件夹（knownBroadFolders）。
// 两边都按原样写法和真实路径各比一遍：8.3 短名（C:\Users\ADMINI~1）、目录联接、符号链接、subst 盘符绕不过去。
func broadReason(dir string, broad []string, resolve func(string) string) string {
	if dir == "" || !filepath.IsAbs(filepath.Clean(dir)) {
		return fmt.Sprintf("程序数据文件夹不是一个完整的路径：%q", dir)
	}
	real := resolve(dir)
	if isVolumeRoot(dir) || isVolumeRoot(real) {
		return "程序数据文件夹是磁盘根目录：" + filepath.Clean(dir) + "（实际是 " + real + "）"
	}
	ds := pathForms(dir, resolve)
	for _, k := range broad {
		if k == "" {
			continue
		}
		for _, kf := range pathForms(k, resolve) {
			for _, d := range ds {
				if kf == d || strings.HasPrefix(kf, d+`\`) {
					return "程序数据文件夹 " + filepath.Clean(dir) + "（实际是 " + real + "）就是系统文件夹 " + filepath.Clean(k) + "，或者是它的上一层"
				}
			}
		}
	}
	return ""
}

// knownBroadFolders 这台电脑上不该整个排除的系统文件夹：用户文件夹和它的上一层、桌面、文档、下载、AppData（漫游、本地）、
// 公用文件夹（及其桌面、下载、文档）、Windows、Program Files、ProgramData、临时文件夹。在它们里面单独建的文件夹照样能加。
func knownBroadFolders() []string {
	var out []string
	for _, id := range []*windows.KNOWNFOLDERID{
		windows.FOLDERID_Profile, windows.FOLDERID_UserProfiles, windows.FOLDERID_Desktop, windows.FOLDERID_Documents, windows.FOLDERID_Downloads,
		windows.FOLDERID_RoamingAppData, windows.FOLDERID_LocalAppData,
		windows.FOLDERID_Public, windows.FOLDERID_PublicDesktop, windows.FOLDERID_PublicDownloads, windows.FOLDERID_PublicDocuments,
		windows.FOLDERID_Windows, windows.FOLDERID_ProgramFiles, windows.FOLDERID_ProgramFilesX86, windows.FOLDERID_ProgramData,
	} {
		if p, err := windows.KnownFolderPath(id, 0); err == nil && p != "" {
			out = append(out, p)
		}
	}
	return append(out, os.TempDir())
}

// powershellPath 系统自带的 Windows PowerShell 的完整路径。不按 PATH 找：以管理员身份跑的程序不能让别人放个同名的顶替。
func powershellPath() string {
	dir, err := windows.GetSystemDirectory()
	if err != nil || dir == "" {
		dir = filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	return filepath.Join(dir, "WindowsPowerShell", "v1.0", "powershell.exe")
}

// openResultFile 在 dir（空 = 系统临时文件夹）里新建一个随机名字的空文件，给提权的 PowerShell 写报错原文，返回一直开着的句柄。
// 句柄带读权限、只共享读和写、不共享删除：从建好到读完，同一个用户的别的程序删不掉、改不了名，也就没法把它换成指向别处的链接，
// 让以管理员身份跑的 PowerShell 写到别的文件上去。CREATE_NEW：名字已经有了（包括别人先放的链接）就换一个名字再建。
// 用完先 Close 再删。
func openResultFile(dir string) (*os.File, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	for i := 0; i < 10; i++ {
		var rnd [8]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, "minedeck-defender-"+hex.EncodeToString(rnd[:])+".txt")
		u, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		h, err := windows.CreateFile(u, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
			windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(h), name), nil
	}
	return nil, errors.New("临时文件夹里建不出一个新名字的文件")
}

// readDefenderOut 从开着的结果文件里读回脚本写的报错原文（从头读）。PowerShell 5.1 的 -Encoding UTF8 写的是带 BOM 的 UTF-8，去掉 BOM；
// 读不到就是空串。
func readDefenderOut(f *os.File) string {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	b := make([]byte, defenderOutMax)
	n, _ := io.ReadFull(f, b)
	b = bytes.TrimPrefix(b[:n], []byte("\xef\xbb\xbf"))
	for !utf8.Valid(b) && len(b) > 0 { // 读到上限时可能把一个字切成了两半
		b = b[:len(b)-1]
	}
	return strings.TrimSpace(string(b))
}

// classifyDefender 按 ShellExecuteEx 的错误和 PowerShell 的退出码分出结果的种类，配上给人看的一句话。
// 只看自己能定的信号（系统错误码、脚本自己定的退出码），不去认报错原文里的英文：那些各个版本说法不一，原文照样放进「详情」。
func classifyDefender(action string, code uint32, runErr error) (kind, msg string) {
	verb := "添加"
	if action == defenderRemove {
		verb = "删除"
	}
	manual := "可以在 Windows 安全中心里手动" + verb + "：病毒和威胁防护 → 管理设置 → 排除项"
	if runErr != nil {
		var errno syscall.Errno
		switch {
		case errors.As(runErr, &errno) && errno == windows.ERROR_CANCELLED:
			// UAC 里点了「否」。策略设成自动拒绝提升、确认超时、切不到安全桌面也是这个错误码，所以不说死是点了「否」
			return dfCancelled, "没通过系统的管理员确认（点了「否」，或者确认被取消了）"
		case errors.As(runErr, &errno) && (errno == windows.ERROR_ACCESS_DISABLED_BY_POLICY ||
			errno == windows.ERROR_ACCESS_DISABLED_NO_SAFER_UI_BY_POLICY || errno == windows.ERROR_SYSTEM_INTEGRITY_POLICY_VIOLATION):
			// 只有 powershell.exe 本身被 AppLocker、软件限制策略、应用程序控制禁止启动时才是这几个错误码（照微软文档对上的，
			// 没在真开了这些策略的电脑上试过）。执行策略、受限语言模式、Defender 设置被单位锁死，都是 PowerShell 跑起来以后
			// 才出的错，落到下面的「别的失败」，界面上的说法也只说「不让启动 PowerShell」
			return dfBlocked, "这台电脑的策略不让启动 PowerShell，请在 Windows 安全中心里手动" + verb
		case errors.As(runErr, &errno) && (errno == windows.ERROR_FILE_NOT_FOUND || errno == windows.ERROR_PATH_NOT_FOUND):
			return dfFailed, "这台电脑上找不到 PowerShell，请在 Windows 安全中心里手动" + verb
		case errors.Is(runErr, errDefenderTimeout):
			return dfFailed, "等了两分钟还没做完，PowerShell 可能还在跑。过一会儿再点一次"
		}
		return dfFailed, manual
	}
	switch code {
	case 0:
		return dfOK, ""
	case psExitNoService, psExitNoCmdlet:
		if action == defenderRemove {
			return dfUnavailable, "Windows 安全中心现在没在工作（可能装了别的杀毒软件，或者被关掉了），撤销不了"
		}
		return dfUnavailable, "Windows 安全中心现在没在做实时防护（可能装了别的杀毒软件，或者被关掉了），加了也不起作用"
	case psExitVerify:
		if action == defenderRemove {
			return dfFailed, "撤完再查，Windows 安全中心的排除项里还有这一条，可能被单位的策略管着"
		}
		return dfFailed, "加完再查，Windows 安全中心的排除项里没有这一条，可能被单位的策略管着"
	}
	return dfFailed, manual
}

// defenderDetail 「详情」里的原文：做的是什么、哪个文件夹、怎么跑的、系统和 PowerShell 各自怎么说。
func defenderDetail(action, path, exe string, code uint32, runErr error, out string) string {
	what := "加入 Windows 安全中心的排除项"
	if action == defenderRemove {
		what = "从 Windows 安全中心的排除项里删掉"
	}
	lines := []string{what + "：" + path, "以管理员身份运行：" + exe}
	if runErr != nil {
		var errno syscall.Errno
		if errors.As(runErr, &errno) {
			lines = append(lines, fmt.Sprintf("PowerShell 没有跑起来（ShellExecuteEx 错误码 %d）：%v", uint32(errno), runErr))
		} else {
			lines = append(lines, "没拿到结果："+runErr.Error())
		}
	} else {
		lines = append(lines, fmt.Sprintf("PowerShell 退出码：%d", code))
	}
	if out != "" {
		lines = append(lines, "PowerShell 的原话："+out)
	}
	return strings.Join(lines, "\n")
}

// doDefenderAction 做一次加入或撤销：定下文件夹、先查能不能做、以管理员身份跑、读结果、分类。
// 返回结果，以及配置里该记成什么（changed 为 false 时不用动配置）。不碰全局状态，外部的东西都从 env 来。
func doDefenderAction(action string, env defenderEnv) (res defenderResult, rec *app.DefenderExclusion, changed bool) {
	res = defenderResult{Action: action}
	resolve := env.resolve
	if resolve == nil {
		resolve = canonicalDir
	}
	var target string
	switch action {
	case defenderAdd:
		target = filepath.Clean(env.base)
		res.Path = target
		// 界面上记着别的位置时只给「撤销」，不会走到这里；页面里的脚本直接调过来时也不替它加：
		// 新的一记，旧的那条就没人记得了，留在系统里一直不扫。路径是空的记录（手改坏的配置）当没记过
		if env.record != nil && env.record.Path != "" && !samePath(env.record.Path, target) {
			res.Kind, res.Msg = dfRefused, "以前加入的是另一个位置，先撤销那一条"
			res.Detail = "以前加入的：" + env.record.Path + "\n现在的程序数据文件夹：" + target
			return res, env.record, false
		}
		if why := broadReason(target, env.broad, resolve); why != "" {
			res.Kind, res.Msg = dfRefused, "程序数据文件夹是磁盘根目录或「下载」这类系统文件夹，整个排除不安全。先把 MineDeck 放进单独的文件夹"
			res.Detail = why
			return res, env.record, false
		}
	case defenderRemove:
		// 撤销记着的那一条（文件夹搬过的话就是旧位置）；没记着时撤销现在的程序数据文件夹，没加过也没关系：删一个不在的排除项不算错
		target = env.base
		if env.record != nil && env.record.Path != "" {
			target = env.record.Path
		}
		target = filepath.Clean(target)
		res.Path = target
		if !filepath.IsAbs(target) { // 程序数据文件夹没定下来（不该发生）：不拿「.」这种路径去删
			res.Kind, res.Detail = dfFailed, fmt.Sprintf("要撤销的不是一个完整的路径：%q", target)
			return res, env.record, false
		}
	default:
		res.Kind, res.Detail = dfFailed, fmt.Sprintf("不认识的动作：%q", action)
		return res, env.record, false
	}

	f, err := openResultFile(env.tempDir)
	if err != nil { // 还没去提权：不按系统错误码分类（建不了临时文件不是「找不到 PowerShell」）
		res.Kind = dfFailed
		res.Detail = defenderDetail(action, target, env.exe, 0, errors.New("建不了放结果的临时文件："+err.Error()), "")
		return res, env.record, false
	}
	// 一直开着到读完，关了再删（见 openResultFile）
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()

	code, runErr := env.run(env.exe, defenderParams(action, target, f.Name()))
	out := readDefenderOut(f)
	res.Kind, res.Msg = classifyDefender(action, code, runErr)
	res.Detail = defenderDetail(action, target, env.exe, code, runErr, out)
	if res.Kind != dfOK {
		return res, env.record, false
	}
	res.Msg, res.Detail = "", ""
	if action == defenderAdd {
		return res, &app.DefenderExclusion{Path: target, At: env.now()}, true
	}
	return res, nil, true
}

// runElevated 以管理员身份跑 exe 并等它结束，返回退出码。测试里换成假的（defender_windows_test.go 的 init 先换成一碰就报错的），
// 跑测试时绝不会真去提权、真去改 Defender。
var runElevated = shellRunElevated

// saveDefenderRecord 把记录写进配置并确认真的落了盘。测试里可以换成假的。
var saveDefenderRecord = writeDefenderRecord

// writeDefenderRecord app.UpdateConfig 写盘出错时不报（saveLocked 把错误吞了，app 包这次只许加字段，没改它），
// 所以写完从 config.json 读回来核对：系统里已经改了、配置却没存下的话，界面要照实说，不然以后既显示不对、也撤销不了。
func writeDefenderRecord(rec *app.DefenderExclusion) error {
	if _, err := app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = rec; return nil }); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(app.Base, "config.json"))
	if err != nil {
		return err
	}
	var saved struct {
		D *app.DefenderExclusion `json:"defenderExclusion"`
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		return fmt.Errorf("config.json 读不懂：%w", err)
	}
	if (saved.D == nil) != (rec == nil) || rec != nil && (saved.D.Path != rec.Path || !saved.D.At.Equal(rec.At)) {
		return errors.New("config.json 里没写进去（写盘失败，原因没有报出来）")
	}
	return nil
}

// defenderLock 同一时间只做一件：UAC 窗口开着时再点，不再弹第二个。
var defenderLock sync.Mutex

// startDefenderExclusion 宿主函数 hostDefenderExclusion 的实现。宿主函数在界面线程上被调，UAC 窗口要等用户点完，
// 所以这里只查参数、占住「正在做」就返回 defenderStarted，在后台做完再调 done 交结果（done 自己切回界面线程）。
// 上一次还没做完时返回 defenderBusy（不是错误：页面接着等上一次的结果）。动作不对时返回错误，页面那边的 Promise 拿到这句话。
//
// 先解锁、再调 done：页面收到结果时宿主已经空出来，马上再点也不会撞上「还没做完」。解锁放在 defer 里，
// 做的过程中出了意外（panic）也解得开；意外收成一个失败的结果交给页面，不让整个程序退出。
func startDefenderExclusion(action string, hwnd uintptr, done func(defenderResult)) (string, error) {
	if action != defenderAdd && action != defenderRemove {
		return "", fmt.Errorf("不认识的动作：%q", action)
	}
	if !defenderLock.TryLock() {
		return defenderBusy, nil
	}
	go func() {
		r := func() (r defenderResult) {
			defer defenderLock.Unlock()
			defer func() {
				if p := recover(); p != nil {
					log.Printf("Windows 安全中心排除项：做到一半出了意外：%v", p)
					r = defenderResult{Action: action, Kind: dfFailed, Detail: fmt.Sprint("做到一半出了意外：", p)}
				}
			}()
			return runDefenderAction(action, hwnd)
		}()
		done(r)
	}()
	return defenderStarted, nil
}

// runDefenderAction 用真的环境做一次，做成了就记进配置（只有这里写 defenderExclusion）。记不下来时结果改成 dfUnsaved。
func runDefenderAction(action string, hwnd uintptr) defenderResult {
	env := defenderEnv{
		base:   app.Base,
		record: app.GetConfig().DefenderExclusion,
		broad:  knownBroadFolders(),
		exe:    powershellPath(),
		run:    func(exe, params string) (uint32, error) { return runElevated(hwnd, exe, params) },
		now:    time.Now,
	}
	res, rec, changed := doDefenderAction(action, env)
	if !changed {
		return res
	}
	if err := saveDefenderRecord(rec); err != nil {
		log.Printf("Windows 安全中心排除项：系统里已经改了，没能记进配置：%v", err)
		res.Kind = dfUnsaved
		if action == defenderAdd {
			res.Msg = "Windows 安全中心里已经加进去了，但 MineDeck 没能记下来：配置文件写不进去"
		} else {
			res.Msg = "Windows 安全中心里已经撤掉了，但 MineDeck 没能记下来：配置文件写不进去"
		}
		res.Detail = res.Msg + "\n文件夹：" + res.Path + "\n原因：" + err.Error() +
			"\n这次运行里照改过以后的样子显示；下次打开 MineDeck 时可能显示不对，到时候点一次「加入」或「撤销」就对上了。"
	}
	return res
}

// defenderDoneJS 拼出把结果交给页面的脚本，app.js 的 window.__mdDefenderDone 接着弹提示、重画设置页。
// 结果用 JSON 写成 JS 的对象字面量（字符串里的引号、换行、U+2028 都转义了），拼不坏脚本。
func defenderDoneJS(r defenderResult) string {
	b, _ := json.Marshal(r)
	return "window.__mdDefenderDone && window.__mdDefenderDone(" + string(b) + ")"
}

// shellRunElevated 用 ShellExecuteEx 的 runas 以管理员身份启动 exe（弹 UAC 确认窗口，挂在 hwnd 这个窗口上），等它结束，返回退出码。
// 没通过 UAC 时 ShellExecuteEx 失败、错误码是 ERROR_CANCELLED。和 shellOpenURL 一样先在这个线程上初始化 COM。
// 在后台线程上调：ShellExecuteEx 要等用户在 UAC 里点完才返回，放在界面线程上会卡住窗口。
func shellRunElevated(hwnd uintptr, exe, params string) (uint32, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); hr == nil || hr == syscall.Errno(windows.S_FALSE) {
		defer windows.CoUninitialize()
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	par, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		hwnd:         windows.Handle(hwnd),
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: par,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(verb)
	runtime.KeepAlive(file)
	runtime.KeepAlive(par)
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return 0, errno
		}
		return 0, errors.New("系统没能以管理员身份启动它（ShellExecuteEx 失败）")
	}
	if info.hProcess == 0 {
		return 0, errors.New("系统没交回 PowerShell 的进程，不知道它跑得怎么样")
	}
	defer windows.CloseHandle(info.hProcess)
	return waitExitCode(info.hProcess, defenderWait)
}

// waitExitCode 等进程结束、取退出码。先认超时（进程不结束它），别的不是「结束了」的返回一律按出错。
func waitExitCode(h windows.Handle, wait time.Duration) (uint32, error) {
	ev, err := windows.WaitForSingleObject(h, uint32(wait/time.Millisecond))
	switch {
	case ev == uint32(windows.WAIT_TIMEOUT):
		return 0, errDefenderTimeout
	case err != nil:
		return 0, err
	case ev != windows.WAIT_OBJECT_0:
		return 0, fmt.Errorf("等 PowerShell 结束时出错（WaitForSingleObject 返回 %#x）", ev)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return 0, err
	}
	return code, nil
}
