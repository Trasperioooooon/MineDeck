package main

// 把 MineDeck 的程序数据文件夹加进 Windows 安全中心（Microsoft Defender）的排除项，或者撤销（窗口版「设置 → 存储位置」）。
//
// 为什么要：实时防护每读写一个文件都要扫一遍，开服、存档会慢一些；frpc 这类穿透程序还常被误报、隔离。加了排除项，
// 这个文件夹里的东西就不扫了。放在别处的服务器和备份（新建时、设置里都能改存放位置）不在里面，也不自动替它们加。
//
// 怎么做：改排除项要管理员权限。用 ShellExecuteEx 的 runas 以管理员身份跑 PowerShell 的 Add-MpPreference /
// Remove-MpPreference，弹的是系统的 UAC 确认窗口；等它跑完，按结果分成：做成了、在 UAC 里点了「否」、Defender 没在工作
// （装了别的杀毒软件、被关掉）、PowerShell 被组策略拦下、别的失败。提权起来的进程拿不到它的输出，报错原文由脚本写进一个
// 临时文件，读回来放进界面的「详情」。命令写成明文的 -Command（不用 -EncodedCommand）：UAC 窗口的「显示更多详细信息」里
// 看得懂要做什么，而且「编码过的 PowerShell 加 Defender 排除项」正是杀毒软件盯着的写法。
//
// 只能人点：这是改系统安全设置的事。入口是宿主函数 hostDefenderExclusion（webview_windows.go 里 Bind），只有原生窗口里的
// 页面调得到；没有登记进操作层（internal/ops/catalog），AI 助手、MCP、局域网登录的手机都调不到。页面只说「加入」还是「撤销」，
// 不给路径：路径由这里定（app.Base，撤销时是记下来的那一条），页面里的脚本借它排除不了别的文件夹。
//
// 加没加过：不提权读不到排除项（非管理员下 Get-MpPreference 不给 ExclusionPath），所以只记 MineDeck 自己做成的结果
// （配置里的 defenderExclusion，带时间），界面照这个显示，不假装查得到系统里的真实状态。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// 结果的种类，页面按它挑说法和提示的样子（cancelled 是用户自己点的，不按报错提示）。
const (
	dfOK          = "ok"          // 做成了
	dfCancelled   = "cancelled"   // 在 UAC 的确认窗口里点了「否」
	dfUnavailable = "unavailable" // Windows 安全中心（Defender）没在工作
	dfBlocked     = "blocked"     // 这台电脑的策略不让运行 PowerShell
	dfRefused     = "refused"     // 没去做：文件夹太大（磁盘根目录、桌面、下载……），或者记着的是另一个位置
	dfFailed      = "failed"      // 别的失败
)

// 脚本自己定的退出码。不用 1：PowerShell 自己出错（命令解析不了等）时退出码是 1，分不清是谁。
const (
	psExitNoService = 20 // WinDefend 服务没装或者没在运行：Defender 没在工作
	psExitNoCmdlet  = 21 // 没有 Defender 的 PowerShell 命令（精简过的系统、没装 Defender 的服务器版）
	psExitCmdletErr = 22 // Add-MpPreference / Remove-MpPreference 报错，原文在结果文件里
)

// defenderWait 等提权起来的 PowerShell 跑完最多等多久。UAC 窗口开着的时间不算在里面（ShellExecuteEx 等用户点完才返回）；
// 平时一两秒就完，等这么久还没完的多半是 Defender 卡住了，不再等，按失败说。
const defenderWait = 2 * time.Minute

// defenderOutMax 结果文件最多读多少字节。正常的报错一两行；读多了界面的「详情」里也放不下。
const defenderOutMax = 8 << 10

var errDefenderTimeout = errors.New("等了两分钟，PowerShell 还没结束")

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
	exe     string                                   // powershell.exe 的完整路径
	tempDir string                                   // 结果文件放哪（空 = 系统临时文件夹）
	run     func(exe, params string) (uint32, error) // 以管理员身份跑 exe 并等它结束，返回退出码
	now     func() time.Time
}

// psQuote 把 s 写成 PowerShell 的单引号字符串字面量：里面的单引号写两遍。PowerShell 把弯引号 ‘ ’ ‚ ‛ 也当单引号
// （见 internal/hostdlg/fallback_windows.go 的说明），这几个也要写两遍，不然文件夹名里带一个 ’ 就能把字符串截断、
// 后面的字当命令跑。单引号字符串里 $、`、反斜杠、双引号都不特殊，原样写。
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

// isPSSingleQuote PowerShell 认作单引号的字符（和它的 CodeGeneration.EscapeSingleQuotedStringContent 一致）。
func isPSSingleQuote(r rune) bool {
	switch r {
	case '\'', '\u2018', '\u2019', '\u201A', '\u201B':
		return true
	}
	return false
}

// defenderScript 拼出要以管理员身份跑的那段 PowerShell。先看 WinDefend 服务在不在跑（不在跑的话 Add-MpPreference 反正会失败，
// 而且报的是一串十六进制的错误码，看不出是 Defender 没开）；再加或删；出错时把原文写进 outFile。
// Add / Remove-MpPreference 失败时给的是不终止的错误：除了开头的 $ErrorActionPreference，命令上再写一遍 -ErrorAction Stop，
// 不然万一没进 catch，脚本照样 exit 0，这边就把没加上的记成了「已加入」。
// 结果文件出错写不进去也不改退出码（-ErrorAction SilentlyContinue），界面照样分得清是哪种失败，只是「详情」里少了原文。
// 整段不含双引号：交给 windows.EscapeArg 包成一个参数时不用再转义里面的东西。
func defenderScript(action, path, outFile string) string {
	cmdlet := "Add-MpPreference"
	if action == defenderRemove {
		cmdlet = "Remove-MpPreference"
	}
	save := "Set-Content -LiteralPath $o -Encoding UTF8 -ErrorAction SilentlyContinue -Value "
	return "$ErrorActionPreference='Stop'; $o=" + psQuote(outFile) + "; " +
		"$s=Get-Service -Name WinDefend -ErrorAction SilentlyContinue; " +
		"if (-not $s) { " + save + "'WinDefend service not installed'; exit " + fmt.Sprint(psExitNoService) + " }; " +
		"if ($s.Status -ne 'Running') { " + save + "('WinDefend service status: ' + $s.Status); exit " + fmt.Sprint(psExitNoService) + " }; " +
		"try { " + cmdlet + " -ExclusionPath " + psQuote(path) + " -Force -ErrorAction Stop; exit 0 } " +
		"catch { $e=$_; " + save + "($e.Exception.Message + ' | ' + $e.FullyQualifiedErrorId + ' | HResult=0x' + ('{0:X8}' -f $e.Exception.HResult)); " +
		"if ($e.FullyQualifiedErrorId -eq 'CommandNotFoundException') { exit " + fmt.Sprint(psExitNoCmdlet) + " }; exit " + fmt.Sprint(psExitCmdletErr) + " }"
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

// samePath Windows 上不分大小写比较两个路径（先整理掉多余的分隔符和末尾的反斜杠）。
func samePath(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// isVolumeRoot 是不是磁盘根目录（D:\、\\server\share\）。
func isVolumeRoot(p string) bool {
	p = filepath.Clean(p)
	rest := p[len(filepath.VolumeName(p)):]
	return rest == "" || rest == `\` || rest == "/"
}

// broadReason 程序数据文件夹能不能整个排除：MineDeck 是绿色版，有人直接把它放在磁盘根目录、桌面、「下载」里，
// 那样加进排除项等于整个盘、整个下载文件夹都不扫了，比不加还糟。是磁盘根目录，或者就是某个系统文件夹、或者是它的上一层时，
// 返回原因（写进「详情」）；能加时返回空串。broad 是这台电脑上的那些系统文件夹（knownBroadFolders）。
func broadReason(dir string, broad []string) string {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Sprintf("程序数据文件夹不是一个完整的路径：%q", dir)
	}
	if isVolumeRoot(dir) {
		return "程序数据文件夹是磁盘根目录：" + filepath.Clean(dir)
	}
	d := strings.ToLower(filepath.Clean(dir))
	for _, k := range broad {
		if k == "" {
			continue
		}
		kc := strings.ToLower(filepath.Clean(k))
		if kc == d || strings.HasPrefix(kc, d+`\`) {
			return "程序数据文件夹 " + filepath.Clean(dir) + " 就是系统文件夹 " + filepath.Clean(k) + "，或者是它的上一层"
		}
	}
	return ""
}

// knownBroadFolders 这台电脑上不该整个排除的系统文件夹：用户文件夹、桌面、文档、下载、Windows、Program Files、ProgramData、临时文件夹。
func knownBroadFolders() []string {
	var out []string
	for _, id := range []*windows.KNOWNFOLDERID{
		windows.FOLDERID_Profile, windows.FOLDERID_Desktop, windows.FOLDERID_Documents, windows.FOLDERID_Downloads,
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

// readDefenderOut 读回脚本写的报错原文。PowerShell 5.1 的 -Encoding UTF8 写的是带 BOM 的 UTF-8，去掉 BOM；读不到就是空串。
func readDefenderOut(name string) string {
	f, err := os.Open(name)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, defenderOutMax)
	n, _ := f.Read(b)
	b = bytes.TrimPrefix(b[:n], []byte("\xef\xbb\xbf"))
	for !utf8.Valid(b) && len(b) > 0 { // 读到上限时可能把一个字切成了两半
		b = b[:len(b)-1]
	}
	return strings.TrimSpace(string(b))
}

// classifyDefender 按 ShellExecuteEx 的错误和 PowerShell 的退出码分出结果的种类，配上给人看的一句话。
// 只看自己能定的信号（系统错误码、脚本自己定的退出码），不去认报错原文里的英文和十六进制码：那些各个版本说法不一，
// 原文照样放进「详情」。
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
			// UAC 窗口里点了「否」
			return dfCancelled, "在系统的确认窗口里点了「否」"
		case errors.As(runErr, &errno) && (errno == windows.ERROR_ACCESS_DISABLED_BY_POLICY ||
			errno == windows.ERROR_ACCESS_DISABLED_NO_SAFER_UI_BY_POLICY || errno == windows.ERROR_SYSTEM_INTEGRITY_POLICY_VIOLATION):
			// 软件限制策略、AppLocker、应用程序控制拦下 powershell.exe 时系统给的错误码（照微软文档的说法对上的，
			// 没在真开了这些策略的电脑上试过；对不上的话落到下面的「别的失败」，原文照样在详情里）
			return dfBlocked, "这台电脑的组策略不让运行 PowerShell，请在 Windows 安全中心里手动" + verb
		case errors.As(runErr, &errno) && (errno == windows.ERROR_FILE_NOT_FOUND || errno == windows.ERROR_PATH_NOT_FOUND):
			return dfFailed, "这台电脑上找不到 PowerShell，请在 Windows 安全中心里手动" + verb
		case errors.Is(runErr, errDefenderTimeout):
			return dfFailed, "等了两分钟还没做完。" + manual
		}
		return dfFailed, manual
	}
	switch code {
	case 0:
		return dfOK, ""
	case psExitNoService, psExitNoCmdlet:
		return dfUnavailable, "Windows 安全中心没在工作，可能装了别的杀毒软件。可以在那个软件里把这个文件夹设为信任"
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
	var target string
	switch action {
	case defenderAdd:
		target = filepath.Clean(env.base)
		res.Path = target
		// 界面上记着别的位置时只给「撤销」，不会走到这里；页面里的脚本直接调过来时也不替它加：
		// 新的一记，旧的那条就没人记得了，留在系统里一直不扫
		if env.record != nil && env.record.Path != "" && !samePath(env.record.Path, target) { // 路径是空的记录（手改坏的配置）当没记过
			res.Kind, res.Msg = dfRefused, "以前加入的是另一个位置，先撤销那一条"
			res.Detail = "以前加入的：" + env.record.Path + "\n现在的程序数据文件夹：" + target
			return res, env.record, false
		}
		if why := broadReason(env.base, env.broad); why != "" {
			res.Kind, res.Msg = dfRefused, "程序数据文件夹是磁盘根目录或「下载」这类系统文件夹，整个排除不安全。先把 MineDeck 放进单独的文件夹"
			res.Detail = why
			return res, env.record, false
		}
	case defenderRemove:
		// 撤销记着的那一条（文件夹搬过的话就是旧位置）；没记着时撤销现在的程序数据文件夹，没加过也没关系：删一个不在的排除项不算错
		target = filepath.Clean(env.base)
		if env.record != nil && env.record.Path != "" {
			target = env.record.Path
		}
		res.Path = target
		if !filepath.IsAbs(target) { // 程序数据文件夹没定下来（不该发生）：不拿「.」这种路径去删
			res.Kind, res.Detail = dfFailed, fmt.Sprintf("要撤销的不是一个完整的路径：%q", target)
			return res, env.record, false
		}
	default:
		res.Kind, res.Msg, res.Detail = dfFailed, "", fmt.Sprintf("不认识的动作：%q", action)
		return res, env.record, false
	}

	f, err := os.CreateTemp(env.tempDir, "minedeck-defender-*.txt")
	if err != nil { // 还没去提权：不按系统错误码分类（找不到临时文件夹不是「找不到 PowerShell」）
		res.Kind = dfFailed
		res.Detail = defenderDetail(action, target, env.exe, 0, errors.New("建不了放结果的临时文件："+err.Error()), "")
		return res, env.record, false
	}
	outFile := f.Name()
	f.Close()
	defer os.Remove(outFile)

	code, runErr := env.run(env.exe, defenderParams(action, target, outFile))
	out := readDefenderOut(outFile)
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

// defenderBusy 同一时间只做一件：UAC 窗口开着时再点，不再弹第二个。
var defenderBusy sync.Mutex

// startDefenderExclusion 宿主函数 hostDefenderExclusion 的实现。宿主函数在界面线程上被调，UAC 窗口要等用户点完，
// 所以这里只查参数、占住「正在做」就返回，在后台做完再调 done 交结果（done 自己切回界面线程）。
// 返回错误表示这次没开始（动作不对、上一次还没做完），页面那边的 Promise 拿到这句话。
func startDefenderExclusion(action string, hwnd uintptr, done func(defenderResult)) error {
	if action != defenderAdd && action != defenderRemove {
		return fmt.Errorf("不认识的动作：%q", action)
	}
	if !defenderBusy.TryLock() {
		return errors.New("上一次的还没做完，先在系统弹出的确认窗口里选好")
	}
	go func() {
		r := runDefenderAction(action, hwnd)
		defenderBusy.Unlock()
		done(r)
	}()
	return nil
}

// runDefenderAction 用真的环境做一次，做成了就记进配置（只有这里写 defenderExclusion）。
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
	if changed {
		_, _ = app.UpdateConfig(func(c *app.Config) error { c.DefenderExclusion = rec; return nil })
	}
	return res
}

// defenderDoneJS 拼出把结果交给页面的脚本，app.js 的 window.__mdDefenderDone 接着弹提示、重画设置页。
// 结果用 JSON 写成 JS 的对象字面量（字符串里的引号、换行、U+2028 都转义了），拼不坏脚本。
func defenderDoneJS(r defenderResult) string {
	b, _ := json.Marshal(r)
	return "window.__mdDefenderDone && window.__mdDefenderDone(" + string(b) + ")"
}

const seeMaskNoCloseProcess = 0x00000040 // 交回新进程的句柄，好等它结束、取退出码

// shellRunElevated 用 ShellExecuteEx 的 runas 以管理员身份启动 exe（弹 UAC 确认窗口，挂在 hwnd 这个窗口上），等它结束，返回退出码。
// 用户在 UAC 里点了「否」时 ShellExecuteEx 失败、错误码是 ERROR_CANCELLED。和 shellOpenURL 一样先在这个线程上初始化 COM。
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
	ev, err := windows.WaitForSingleObject(info.hProcess, uint32(defenderWait/time.Millisecond))
	if err != nil {
		return 0, err
	}
	if ev == uint32(windows.WAIT_TIMEOUT) {
		return 0, errDefenderTimeout
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return 0, err
	}
	return code, nil
}
