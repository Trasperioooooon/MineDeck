package inst

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"minedeck/internal/modpack"
	"minedeck/internal/tasks"
)

// 现成服务端压缩包里的启动脚本常写着 JVM 参数（A43）。脚本本身不解压、不运行（run.bat 是控制文件，见 IsControlFile），
// 导入时从压缩包里读出启动 Java 的那一行，安全的那几类带进这台服务器的自定义 JVM 参数，别的不带、说清楚。
// 认不出启动 Java 的那一行就什么都不带。

// importScripts 看哪几个脚本、按什么先后：Windows 的说了算（同接管时的 scanScripts），它们里面没有启动 Java 的那一行才看 .sh。
var importScripts = []string{"run.bat", "start.bat", "run.sh", "start.sh"}

// userJVMArgsFile Forge / NeoForge 的启动脚本用 @user_jvm_args.txt 读的参数文件，里面的也算脚本里的 JVM 参数。
const userJVMArgsFile = "user_jvm_args.txt"

// maxScriptLine 脚本里一个变量的值、展开以后的一行最长多少。`set A=%A%%A%` 一行翻一倍，二三十行就能把内存吃光；
// 超了整份按认不出办，什么都不带。正常脚本里启动 Java 的那一行也就几百个字。
const maxScriptLine = 32 << 10

// maxScriptExpand 一份脚本里各个变量的值、各行展开以后的长度加起来最多多少（A43）。只限单个变量和单行还不够：
// 一个将近 32 KB 的变量在一千行里各用一次（脚本才 29 KB）就是几十 MB，拆成参数还要再翻几倍；几千个这样的变量也一样。
// 比读脚本的上限（256 KB）大得多，没有变量的脚本碰不到它。超了同样整份按认不出办。
const maxScriptExpand = 1 << 20

var (
	errScriptTooLong = errors.New("变量展开以后太长")
	errNoJavaLine    = errors.New("认不出启动 Java 的那一行")
)

// droppedJVMArg 没带过来的一个参数和原因。
type droppedJVMArg struct{ arg, why string }

// scriptJVM 从启动脚本里认出来的 JVM 参数。
type scriptJVM struct {
	script  string // 从哪个脚本认的
	carried []string
	memory  []string // 内存参数：内存按导入页上填的，这几个不算「没带」
	dropped []droppedJVMArg
}

// packScriptJVM 读压缩包里的启动脚本，认出启动 launch（jar:… 或 args:…）那一行 Java 的 JVM 参数并过一遍 filterScriptJVM。
// found 为 false 是压缩包里没有这几个脚本。err 是有脚本却认不出（errNoJavaLine）、或者变量展开以后太长（errScriptTooLong，
// 这时 sj.script 是那个脚本）：都什么都不带。
func packScriptJVM(pack *modpack.Pack, launch string) (sj scriptJVM, found bool, err error) {
	files := modpack.ServerRootFiles(pack, append(slices.Clone(importScripts), userJVMArgsFile), 256<<10)
	for _, name := range importScripts {
		b, ok := files[name]
		if !ok {
			continue
		}
		found = true
		toks, err := scriptJavaOptions(b, strings.HasSuffix(name, ".bat"), launch)
		if errors.Is(err, errScriptTooLong) {
			return scriptJVM{script: name}, true, err
		}
		if err != nil {
			continue
		}
		var opts []string
		for _, o := range toks {
			if isUserJVMArgsRef(o) {
				var args []string
				for _, line := range strings.Split(string(bytes.TrimPrefix(files[userJVMArgsFile], []byte("\xef\xbb\xbf"))), "\n") {
					args = append(args, argfileTokens(line)...)
				}
				opts = append(opts, jvmOptions(args)...)
				continue
			}
			opts = append(opts, o)
		}
		sj = filterScriptJVM(opts)
		sj.script = name
		return sj, true, nil
	}
	if found {
		return scriptJVM{}, true, errNoJavaLine
	}
	return scriptJVM{}, false, nil
}

func isUserJVMArgsRef(tok string) bool {
	rest, ok := strings.CutPrefix(tok, "@")
	return ok && strings.EqualFold(path.Base(strings.ReplaceAll(rest, `\`, "/")), userJVMArgsFile)
}

// scriptJavaOptions 从一个脚本里找启动 Java 的那一行，返回 java 和启动目标之间的那些参数（见 jvmOptions）。
// bat 为 true 时按 cmd 的写法读（^ 续行、set、%变量%），否则按 sh（\ 续行、赋值、$变量）。
// 有几行都在启动 Java 时，用第一行点名 launch 的，没有就用第一行：只留着第一行，碰到点名的就不再往下看。
// 没有这一行回 errNoJavaLine；展开超了上限（见 scriptExpander）回 errScriptTooLong。
func scriptJavaOptions(src []byte, bat bool, launch string) ([]string, error) {
	e := scriptExpander{bat: bat, vars: map[string]string{}}
	var first []string
	for _, line := range scriptLines(src, bat) {
		if isScriptComment([]byte(line)) {
			continue
		}
		toks, err := e.line(line)
		if err != nil {
			return nil, err
		}
		if !isJavaCommand(toks) {
			continue
		}
		if launchesTarget(toks, launch) {
			return jvmOptions(toks[1:]), nil
		}
		if first == nil {
			first = toks
		}
	}
	if first == nil {
		return nil, errNoJavaLine
	}
	return jvmOptions(first[1:]), nil
}

// scriptExpander 一行一行展开脚本：记着赋值过的变量，和整份已经展开了多少。
type scriptExpander struct {
	bat   bool // 按 cmd 的写法（见 scriptJavaOptions）
	vars  map[string]string
	spent int // 已经展开出来的长度，见 maxScriptExpand
}

// line 展开一行：是赋值的记下变量，toks 为空；别的拆成参数（sh 去掉开头的 exec）。
// 一个变量的值、展开以后的这一行超过 maxScriptLine，或者整份加起来超过 maxScriptExpand，回 errScriptTooLong。
func (e *scriptExpander) line(line string) (toks []string, err error) {
	if e.bat {
		line = strings.TrimSpace(strings.TrimPrefix(line, "@"))
		if name, val, ok := batSet(line); ok {
			return nil, e.assign(strings.ToUpper(name), val)
		}
	} else {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if name, val, ok := shAssign(line); ok {
			return nil, e.assign(name, val)
		}
	}
	expanded, err := e.expand(line)
	if err != nil {
		return nil, err
	}
	toks = splitArgs(expanded, !e.bat)
	if !e.bat && len(toks) > 0 && toks[0] == "exec" {
		toks = toks[1:]
	}
	return toks, nil
}

func (e *scriptExpander) assign(name, val string) error {
	v, err := e.expand(val)
	if err == nil {
		e.vars[name] = v
	}
	return err
}

// expand 换掉 s 里的变量，展开出来的长度记进 spent。
func (e *scriptExpander) expand(s string) (string, error) {
	var out string
	var ok bool
	if e.bat {
		out, ok = expandBatVars(s, e.vars)
	} else {
		out, ok = expandShVars(s, e.vars)
	}
	e.spent += len(out)
	if !ok || e.spent > maxScriptExpand {
		return "", errScriptTooLong
	}
	return out, nil
}

// jvmPairOptions 后面跟着一个值的 Java 选项（`--add-opens 模块/包=目标`、`-cp 路径`）：选项和值算一项，列进「没带」；
// 不能把值当成主类，截掉后面的 JVM 参数。
var jvmPairOptions = map[string]bool{
	"-cp": true, "-classpath": true, "--class-path": true, "-p": true, "--module-path": true, "--upgrade-module-path": true,
	"--add-opens": true, "--add-exports": true, "--add-reads": true, "--add-modules": true, "--patch-module": true, "--limit-modules": true,
}

// jvmOptions java 后面的参数里取出 JVM 参数：到 -jar、-m（后面是启动目标和给服务端的参数）或者第一个不是 - 开头的
// （主类、@参数文件）为止。认不出的变量（%JAVA_OPTS%、$OPTS）多半是一串 JVM 参数，接着往下看，由 filterScriptJVM 说明为什么不带。
func jvmOptions(toks []string) []string {
	var opts []string
	for k := 0; k < len(toks); k++ {
		tok := toks[k]
		low := strings.ToLower(tok)
		switch {
		case low == "-jar" || low == "-m" || low == "--module":
			return opts
		case jvmPairOptions[low] && k+1 < len(toks):
			opts = append(opts, tok+" "+toks[k+1])
			k++
		case strings.HasPrefix(tok, "-") || isUserJVMArgsRef(tok) || strings.ContainsAny(tok, "%$"):
			opts = append(opts, tok)
		default:
			return opts
		}
	}
	return opts
}

// scriptLines 按行拆开，接上续行（cmd 行末的 ^、sh 行末的 \，拿掉它和换行，下一行直接接上）。
func scriptLines(src []byte, bat bool) []string {
	cont := `\`
	if bat {
		cont = "^"
	}
	var out []string
	var cur strings.Builder
	for _, raw := range strings.Split(string(bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))), "\n") {
		raw = strings.TrimRight(raw, "\r")
		if strings.HasSuffix(raw, cont) {
			cur.WriteString(strings.TrimSuffix(raw, cont))
			continue
		}
		cur.WriteString(raw)
		out = append(out, strings.TrimSpace(cur.String()))
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// batSet 认 cmd 的 set NAME=value 和 set "NAME=value"（/a、/p 这类不认）。
func batSet(line string) (name, val string, ok bool) {
	if len(line) < 4 || !strings.EqualFold(line[:4], "set ") {
		return "", "", false
	}
	rest := strings.TrimSpace(line[4:])
	if q, ok := strings.CutPrefix(rest, `"`); ok {
		rest = q
		if i := strings.LastIndex(rest, `"`); i >= 0 {
			rest = rest[:i]
		}
	}
	name, val, ok = strings.Cut(rest, "=")
	if !ok || name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, " \t") {
		return "", "", false
	}
	return name, val, true
}

// shAssign 认 sh 的 NAME=value（值可以带引号）。
func shAssign(line string) (name, val string, ok bool) {
	name, val, ok = strings.Cut(line, "=")
	if !ok || name == "" || strings.IndexFunc(name, func(r rune) bool { return !(r == '_' || r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r))) }) >= 0 ||
		unicode.IsDigit(rune(name[0])) {
		return "", "", false
	}
	if toks := splitArgs(val, true); len(toks) == 1 {
		return name, toks[0], true
	}
	return name, strings.TrimSpace(val), true
}

// expandBatVars 换掉 cmd 的变量：脚本里 set 过的换成值；%~dp0 这类（脚本所在的文件夹）、%*、%1 换成空；%% 是一个 %。
// 没 set 过的原样留着：它的值在别人的电脑上，看不到，带着它的参数就不带了（见 filterScriptJVM）。
// 换出来超过 maxScriptLine 时停下，ok 为 false。
func expandBatVars(s string, vars map[string]string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if b.Len() > maxScriptLine {
			return "", false
		}
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		rest := s[i+1:]
		switch {
		case rest == "":
			b.WriteByte('%')
		case rest[0] == '%':
			b.WriteByte('%')
			i++
		case rest[0] == '*' || rest[0] >= '0' && rest[0] <= '9':
			i++
		case rest[0] == '~':
			j := 1
			for j < len(rest) && (rest[j] >= 'a' && rest[j] <= 'z' || rest[j] >= 'A' && rest[j] <= 'Z') {
				j++
			}
			if j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			i += j
		default:
			end := strings.IndexByte(rest, '%')
			if end <= 0 {
				b.WriteByte('%')
				continue
			}
			if v, ok := vars[strings.ToUpper(rest[:end])]; ok {
				b.WriteString(v)
			} else {
				b.WriteString(s[i : i+end+2])
			}
			i += end + 1
		}
	}
	return b.String(), b.Len() <= maxScriptLine
}

// expandShVars 换掉 sh 的变量：赋值过的 $NAME、${NAME} 换成值；$@、$*、$1 换成空；别的原样留着（同 expandBatVars）。
func expandShVars(s string, vars map[string]string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if b.Len() > maxScriptLine {
			return "", false
		}
		if s[i] != '$' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		rest := s[i+1:]
		name, used := "", 0
		switch {
		case rest[0] == '@' || rest[0] == '*' || rest[0] >= '0' && rest[0] <= '9':
			i++
			continue
		case rest[0] == '{':
			if end := strings.IndexByte(rest, '}'); end > 1 {
				name, used = rest[1:end], end+1
			}
		default:
			for used < len(rest) && (rest[used] == '_' || rest[used] < 128 && (unicode.IsLetter(rune(rest[used])) || unicode.IsDigit(rune(rest[used])))) {
				used++
			}
			name = rest[:used]
		}
		if v, ok := vars[name]; ok && name != "" {
			b.WriteString(v)
			i += used
			continue
		}
		b.WriteByte('$')
	}
	return b.String(), b.Len() <= maxScriptLine
}

// splitArgs 按空白拆成参数，双引号里的空白不拆、引号去掉；sh 另认单引号和引号外面的反斜杠转义。
func splitArgs(s string, sh bool) []string {
	var out []string
	var cur strings.Builder
	in, quote := false, rune(0)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else if sh && quote == '"' && r == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\' || rs[i+1] == '$') {
				i++
				cur.WriteRune(rs[i])
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || sh && r == '\'':
			quote, in = r, true
		case sh && r == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			in = true
		case r == ' ' || r == '\t':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

// isJavaCommand 这一行是不是在启动 Java：第一个参数去掉路径和 .exe 是 java 或 javaw，后面还有东西。
func isJavaCommand(toks []string) bool {
	if len(toks) < 2 {
		return false
	}
	exe := strings.ToLower(path.Base(strings.ReplaceAll(toks[0], `\`, "/")))
	exe = strings.TrimSuffix(exe, ".exe")
	return exe == "java" || exe == "javaw"
}

// launchesTarget 这一行启动的是不是 launch：-jar 后面的文件名、或者 @…/win_args.txt（unix_args.txt）的文件夹对得上。
func launchesTarget(toks []string, launch string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(s, `\`, "/"), "./"))
	}
	if jar, ok := strings.CutPrefix(launch, "jar:"); ok {
		for i := 1; i+1 < len(toks); i++ {
			if strings.EqualFold(toks[i], "-jar") && norm(toks[i+1]) == norm(jar) {
				return true
			}
		}
		return false
	}
	args, ok := strings.CutPrefix(launch, "args:")
	if !ok {
		return false
	}
	want := path.Dir(norm(args))
	for _, tok := range toks[1:] {
		if f, ok := strings.CutPrefix(tok, "@"); ok && path.Dir(norm(f)) == want {
			return true
		}
	}
	return false
}

// 启动脚本里哪些 JVM 参数带过来（A43）。带过来的存进 instance.json，以后换核心、换 Java 都还在，每次开服都用；
// JVM 参数能让 Java 读写服务器文件夹外面的文件、另外加载程序、开网络端口，所以只带调性能的几类，别的列出来不带，
// 要用的人确认可信以后自己到「设置 → 启动与内存」里加。下面几张表就是全部规矩，filterScriptJVM 按这个先后看。
const (
	whyVar      = "里面有脚本变量，看不出是什么"
	whyLoads    = "会让 Java 另外加载程序，一律不带"
	whyPrefix   = "只带 -XX:、-D、-X 开头的"
	whyLong     = "太长"
	whySpace    = "带空格或引号，设置里存不了"
	whyXX       = "-XX: 只带开关和数值，带路径、文字的不带"
	whyX        = "-X 只带 -Xss、-Xmn、-Xshare:auto 这几样"
	whyNoName   = "-D 后面没写名字"
	whyEncoding = "编码由 MineDeck 管"
	whyOutside  = "指到服务器文件夹外面"
	whyTooMany  = "带过来的已经够多了，后面的没带"
)

var (
	// jvmDeniedPrefixes 不管后面写什么都不带的：另外加载程序的、出错时执行命令的、从别的文件里再读参数的（能夹带前面这些）、
	// 放开别的程序随时挂进 Java 的、关掉 Java 对程序的检查的、让后台的服务器停下等人点的。原因写得具体些，人才知道为什么。
	// -XX:+UnlockDiagnosticVMOptions、-XX:+UnlockExperimentalVMOptions 不在这里：常见的调优参数集要用它们。
	jvmDeniedPrefixes = []struct{ prefix, why string }{
		{"-javaagent", whyLoads}, {"-agentlib", whyLoads}, {"-agentpath", whyLoads}, {"-Xbootclasspath", whyLoads}, {"-Xrun", whyLoads},
		{"-XX:OnError", "出错时会执行命令，一律不带"}, {"-XX:OnOutOfMemoryError", "出错时会执行命令，一律不带"},
		{"-XX:Flags", "会从别的文件里再读参数，一律不带"}, {"-XX:VMOptionsFile", "会从别的文件里再读参数，一律不带"},
		{"-XX:+EnableDynamicAgentLoading", "会让别的程序随时挂进 Java，一律不带"},
		{"-XX:-BytecodeVerificationRemote", "会关掉 Java 对程序的检查（同 -Xverify:none），一律不带"},
		{"-XX:-BytecodeVerificationLocal", "会关掉 Java 对程序的检查（同 -Xverify:none），一律不带"},
		{"-XX:+ShowMessageBoxOnError", "崩溃时会弹窗等人点，后台的服务器会一直卡着"},
		{"-XX:+PauseAtStartup", "启动时会停住等人，服务器起不来"},
	}
	// jvmMemoryPrefixes 内存：按导入页上填的。后三个是 -Xmx、-Xms 的另一种写法，带过来会盖掉 MineDeck 的内存设置。
	jvmMemoryPrefixes = []string{"-Xmx", "-Xms", "-XX:MaxHeapSize", "-XX:InitialHeapSize", "-XX:MinHeapSize"}
	// jvmAllowedXX -XX: 只带开关（-XX:+名、-XX:-名）和数值（-XX:名=200、=8M、=75.0）；带路径、带文字的
	// （-XX:HeapDumpPath、-XX:ErrorFile、-XX:SharedArchiveFile、-XX:AOTLibrary 这类）自然出局。
	jvmAllowedXX = regexp.MustCompile(`^-XX:(?:[+-][A-Za-z][A-Za-z0-9_]*|[A-Za-z][A-Za-z0-9_]*=[0-9]+(?:\.[0-9]+)?[kKmMgGtT]?)$`)
	// jvmAllowedX -X（不是 -XX:）只带这几样；-Xlog、-Xloggc 能往任意位置写文件，-Xdebug、-Xverify 这类关掉检查，都不带。
	jvmAllowedX = regexp.MustCompile(`^-X(?:(?:ss|mn)[0-9]+[kKmMgG]?|share:(?:auto|off)|noclassgc|incgc)$`)
	// jvmDeniedProps -D 不带的名字（小写比，前缀对上就算）。编码排在前面：sun.stdout.encoding 也落在 sun. 里，原因要说成编码。
	// 模组和服务端自己的（-Dfml.*、-Dpaper.*、-Dterminal.* 这些）不受影响；user. 只拒这两个，user.timezone、user.language、
	// user.country 照旧带（国内的脚本常写）。名字过了的，值再过一遍 propOutside。
	jvmDeniedProps = []struct{ prefix, why string }{
		{"file.encoding", whyEncoding}, {"stdout.encoding", whyEncoding}, {"stderr.encoding", whyEncoding},
		{"sun.stdout.encoding", whyEncoding}, {"sun.stderr.encoding", whyEncoding},
		{"java.", "改的是 Java 自己的设置"}, {"javax.", "改的是 Java 自己的设置"}, {"jdk.", "改的是 Java 自己的设置"},
		{"sun.", "改的是 Java 自己的设置"}, {"com.sun.", "改的是 Java 自己的设置"},
		{"user.dir", "会改 Java 认的当前文件夹"}, {"user.home", "会改 Java 认的用户文件夹"},
		{"http.proxy", "会改 Java 连网走的代理"}, {"https.proxy", "会改 Java 连网走的代理"}, {"socksproxy", "会改 Java 连网走的代理"},
		{"ftp.proxy", "会改 Java 连网走的代理"}, {"http.nonproxyhosts", "会改 Java 连网走的代理"}, {"ftp.nonproxyhosts", "会改 Java 连网走的代理"},
		{"socksnonproxyhosts", "会改 Java 连网走的代理"},
		{"minecraft.api.", "会改正版验证连的服务器"},
		// jna.library.path、jna.boot.library.path 指定从哪里加载本地库；fabric.addMods 另外加载模组；
		// Paper 启动器的 bundlerRepoDir、bundlerMainClass 改从哪里解出、启动哪个程序
		{"jna.", whyLoads}, {"fabric.addmods", whyLoads}, {"bundler", whyLoads},
		// org.apache.logging.log4j.* 是 log4j 2.10 起 log4j2.* 的另一种写法，能盖掉 MineDeck 给老版本加的 formatMsgNoLookups
		{"log4j", "日志配置由 MineDeck 按版本处理"}, {"org.apache.logging.log4j.", "日志配置由 MineDeck 按版本处理"},
	}
)

const (
	maxScriptArg     = 1024    // 单个参数最长多少
	maxCarriedJVM    = 100     // 最多带多少个
	maxCarriedJVMLen = 8 << 10 // 带过来的加起来最长多少：run.bat 那一行在 Windows 上总共不能超过 32767 个字
)

// judgeScriptJVM 一个参数怎么办：why 为空是能带；memory 为 true 是内存参数（按导入页上填的，不算没带）。
func judgeScriptJVM(a string) (why string, memory bool) {
	if strings.ContainsAny(a, "%$") {
		return whyVar, false
	}
	for _, d := range jvmDeniedPrefixes {
		if strings.HasPrefix(a, d.prefix) {
			return d.why, false
		}
	}
	if !strings.HasPrefix(a, "-X") && !strings.HasPrefix(a, "-D") {
		return whyPrefix, false
	}
	if len(a) > maxScriptArg {
		return whyLong, false
	}
	if strings.IndexFunc(a, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '"' }) >= 0 {
		return whySpace, false
	}
	if hasAnyPrefix(a, jvmMemoryPrefixes...) {
		return "", true
	}
	switch {
	case strings.HasPrefix(a, "-XX:"):
		if !jvmAllowedXX.MatchString(a) {
			return whyXX, false
		}
	case strings.HasPrefix(a, "-D"):
		return judgeProp(a[2:]), false
	default:
		if !jvmAllowedX.MatchString(a) {
			return whyX, false
		}
	}
	return "", false
}

// judgeProp 一个 -D（prop 是 -D 后面那段）带不带：先按名字（jvmDeniedProps），再按值（propOutside）。空串是能带。
func judgeProp(prop string) string {
	name, val, _ := strings.Cut(prop, "=")
	name = strings.ToLower(name)
	if name == "" {
		return whyNoName
	}
	for _, d := range jvmDeniedProps {
		if strings.HasPrefix(name, d.prefix) {
			return d.why
		}
	}
	if propOutside(val) {
		return whyOutside
	}
	return ""
}

// propOutside -D 的值像是指到服务器文件夹外面的路径：绝对路径（盘符开头、\ 或 / 开头、file: 网址）或者带 .. 段的。
// 带过来的存进 instance.json，模组、服务端照着它读写的就不是这台服务器的文件了。用 ; 或 , 隔开的一串路径逐个看。
// 网址（Aikar 参数里的 https://mcflags.emc.gs）、相对路径（config/foo）、Asia/Shanghai 这类不受影响。
func propOutside(val string) bool {
	for _, part := range strings.FieldsFunc(val, func(r rune) bool { return r == ';' || r == ',' }) {
		low := strings.ToLower(part)
		if strings.HasPrefix(part, "/") || strings.HasPrefix(part, `\`) || strings.HasPrefix(low, "file:") ||
			len(low) >= 2 && low[1] == ':' && low[0] >= 'a' && low[0] <= 'z' {
			return true
		}
		if slices.Contains(strings.FieldsFunc(part, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
			return true
		}
	}
	return false
}

// filterScriptJVM 按上面的规矩把参数分成带过来的、内存、没带的。重复的只带一个；带的个数、总长有上限（maxCarriedJVM、maxCarriedJVMLen）。
func filterScriptJVM(args []string) scriptJVM {
	var sj scriptJVM
	total := 0
	for _, a := range args {
		why, memory := judgeScriptJVM(a)
		switch {
		case memory:
			sj.memory = append(sj.memory, a)
		case why != "":
			sj.dropped = append(sj.dropped, droppedJVMArg{a, why})
		case slices.Contains(sj.carried, a):
		case len(sj.carried) >= maxCarriedJVM || total+len(a) > maxCarriedJVMLen:
			sj.dropped = append(sj.dropped, droppedJVMArg{a, whyTooMany})
		default:
			sj.carried = append(sj.carried, a)
			total += len(a)
		}
	}
	return sj
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s, p) })
}

// applyScriptJVM 把启动脚本里能带的 JVM 参数放进 inst.ExtraJVM，带了哪些、没带哪些写进任务日志和任务的结果（告警）。
// 有启动脚本却认不出时什么都不带，任务日志说一句。
func applyScriptJVM(t *tasks.Task, inst *Instance, pack *modpack.Pack) {
	sj, found, err := packScriptJVM(pack, inst.LaunchTarget)
	switch {
	case !found:
		return
	case errors.Is(err, errScriptTooLong):
		t.Logf("%s 里的变量展开以后太长（一个变量或一行超过 %d KB，或者整份加起来超过 %d MB），按认不出办，里面的 JVM 参数一个都没带过来；需要的话到「设置 → 启动与内存」里自己加",
			sj.script, maxScriptLine>>10, maxScriptExpand>>20)
		return
	case err != nil:
		t.Logf("启动脚本里认不出启动 Java 的那一行（比如是用 start、if 或者 & 连着别的命令起的），里面的 JVM 参数没有带过来；需要的话到「设置 → 启动与内存」里自己加")
		return
	}
	if len(sj.memory) > 0 {
		t.Logf("%s 里的内存设置（%s）没有用，内存按导入页上填的 %d MB", sj.script, shortNames(sj.memory, 8), inst.XmxMB)
	}
	if len(sj.carried)+len(sj.dropped) == 0 {
		return
	}
	inst.ExtraJVM = mergeArgs(inst.ExtraJVM, sj.carried)
	var title string
	switch {
	case len(sj.carried) > 0 && len(sj.dropped) > 0:
		title = fmt.Sprintf("启动脚本里的 JVM 参数带过来 %d 个，没带 %d 个", len(sj.carried), len(sj.dropped))
	case len(sj.carried) > 0:
		title = fmt.Sprintf("启动脚本里的 %d 个 JVM 参数带过来了", len(sj.carried))
	default:
		title = fmt.Sprintf("启动脚本里有 %d 个 JVM 参数没带过来", len(sj.dropped))
	}
	note := "只带调性能的几类（-XX: 的开关和数值、-Xss 这几样、-D 里不是 Java 自己的设置的）；内存按导入页上填的；" +
		"能让 Java 另外加载程序、执行命令、往别处写文件、改代理的和日志配置一律不带，需要的话确认可信以后到「设置 → 启动与内存」里自己加。"
	if len(sj.carried) > 0 {
		t.Logf("%s 里的这些 JVM 参数带到了「自定义 JVM 参数」：%s", sj.script, shortNames(sj.carried, 20))
		note = "带过来的在「设置 → 启动与内存」的自定义 JVM 参数里：" + strings.Join(sj.carried, " ") +
			"。它们是给原来那个 Java 写的，换了大版本以后有的可能不认（比如老的 -XX:+UseConcMarkSweepGC），开不了服就到那里删掉。" + note
	}
	names := make([]string, 0, len(sj.dropped))
	for _, d := range sj.dropped {
		names = append(names, fmt.Sprintf("%s（%s）", clipArg(d.arg), d.why))
	}
	if len(names) > 0 {
		t.Logf("%s 里的这些 JVM 参数没带过来：%s", sj.script, shortNames(names, 20))
	}
	t.AddWarning(tasks.Warning{Kind: "script-jvm", Title: title, Note: note, Items: warnItems(names, len(names), "个")})
}

// clipArg 告警和日志里列一个参数：太长的只列开头。
func clipArg(a string) string {
	if r := []rune(a); len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return a
}
