package inst

import (
	"bytes"
	"fmt"
	"path"
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

// droppedJVMArg 没带过来的一个参数和原因。
type droppedJVMArg struct{ arg, why string }

// scriptJVM 从启动脚本里认出来的 JVM 参数。
type scriptJVM struct {
	script  string // 从哪个脚本认的
	carried []string
	dropped []droppedJVMArg
}

// packScriptJVM 读压缩包里的启动脚本，认出启动 launch（jar:… 或 args:…）那一行 Java 的 JVM 参数并过一遍 filterScriptJVM。
// ok 为 false 是没认出启动 Java 的那一行。
func packScriptJVM(pack *modpack.Pack, launch string) (scriptJVM, bool) {
	files := modpack.ServerRootFiles(pack, append(slices.Clone(importScripts), userJVMArgsFile), 256<<10)
	for _, name := range importScripts {
		b, ok := files[name]
		if !ok {
			continue
		}
		opts, ok := scriptJavaOptions(b, strings.HasSuffix(name, ".bat"), launch)
		if !ok {
			continue
		}
		var expanded []string
		for _, o := range opts {
			if isUserJVMArgsRef(o) {
				for _, line := range strings.Split(string(bytes.TrimPrefix(files[userJVMArgsFile], []byte("\xef\xbb\xbf"))), "\n") {
					expanded = append(expanded, argfileTokens(line)...)
				}
				continue
			}
			expanded = append(expanded, o)
		}
		carried, dropped := filterScriptJVM(expanded)
		return scriptJVM{script: name, carried: carried, dropped: dropped}, true
	}
	return scriptJVM{}, false
}

func isUserJVMArgsRef(tok string) bool {
	rest, ok := strings.CutPrefix(tok, "@")
	return ok && strings.EqualFold(path.Base(strings.ReplaceAll(rest, `\`, "/")), userJVMArgsFile)
}

// scriptJavaOptions 从一个脚本里找启动 Java 的那一行，返回 java 和启动目标（-jar、@参数文件、-cp、主类）之间的那些参数。
// bat 为 true 时按 cmd 的写法读（^ 续行、set、%变量%），否则按 sh（\ 续行、赋值、$变量）。
// 有几行都在启动 Java 时，优先点名 launch 的那一行，没有就用第一行。
func scriptJavaOptions(src []byte, bat bool, launch string) ([]string, bool) {
	vars := map[string]string{}
	var lines [][]string
	for _, line := range scriptLines(src, bat) {
		if isScriptComment([]byte(line)) {
			continue
		}
		if bat {
			line = strings.TrimSpace(strings.TrimPrefix(line, "@"))
			if name, val, ok := batSet(line); ok {
				vars[strings.ToUpper(name)] = expandBatVars(val, vars)
				continue
			}
			if toks := splitArgs(expandBatVars(line, vars), false); isJavaCommand(toks) {
				lines = append(lines, toks)
			}
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if name, val, ok := shAssign(line); ok {
			vars[name] = expandShVars(val, vars)
			continue
		}
		toks := splitArgs(expandShVars(line, vars), true)
		if len(toks) > 0 && toks[0] == "exec" {
			toks = toks[1:]
		}
		if isJavaCommand(toks) {
			lines = append(lines, toks)
		}
	}
	if len(lines) == 0 {
		return nil, false
	}
	pick := lines[0]
	for _, toks := range lines {
		if launchesTarget(toks, launch) {
			pick = toks
			break
		}
	}
	var opts []string
	for _, tok := range pick[1:] {
		low := strings.ToLower(tok)
		// 后面是启动目标和给服务端的参数。认不出的变量（%JAVA_OPTS%、$OPTS）多半是一串 JVM 参数，接着往下看，
		// 由 filterScriptJVM 说明为什么不带
		if low == "-jar" || low == "-cp" || low == "-classpath" || low == "--class-path" || low == "-m" || low == "--module" ||
			!strings.HasPrefix(tok, "-") && !isUserJVMArgsRef(tok) && !strings.ContainsAny(tok, "%$") {
			break
		}
		opts = append(opts, tok)
	}
	return opts, true
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
func expandBatVars(s string, vars map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
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
	return b.String()
}

// expandShVars 换掉 sh 的变量：赋值过的 $NAME、${NAME} 换成值；$@、$*、$1 换成空；别的原样留着（同 expandBatVars）。
func expandShVars(s string, vars map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
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
	return b.String()
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

// filterScriptJVM 按安全规矩挑出能带过来的 JVM 参数（A43）：只带 -XX:、-D、-X 开头的；内存（-Xmx、-Xms）用导入页上填的；
// 能让 Java 另外加载程序的（-javaagent、-agentlib、-agentpath、-Xbootclasspath、-Xrun）、出错时执行命令的
// （-XX:OnError、-XX:OnOutOfMemoryError）、再从文件里读参数的（-XX:Flags、-XX:VMOptionsFile，能夹带上面那些）一律不带；
// 日志配置（-Dlog4j…）由 MineDeck 按版本套 log4shell 防护，不带。带空格、引号、控制字符的设置页存不了，
// 带着认不出的脚本变量的不知道是什么，也不带。最多带 100 个，重复的只留一个。
func filterScriptJVM(args []string) (carried []string, dropped []droppedJVMArg) {
	drop := func(a, why string) { dropped = append(dropped, droppedJVMArg{a, why}) }
	for _, a := range args {
		switch {
		case strings.ContainsAny(a, "%$"):
			drop(a, "里面有脚本变量，看不出是什么")
		case strings.HasPrefix(a, "-Xmx") || strings.HasPrefix(a, "-Xms"):
			drop(a, "内存按导入页上填的")
		case hasAnyPrefix(a, "-javaagent", "-agentlib", "-agentpath", "-Xbootclasspath", "-Xrun"):
			drop(a, "会让 Java 另外加载程序，一律不带")
		case hasAnyPrefix(a, "-XX:OnError", "-XX:OnOutOfMemoryError"):
			drop(a, "出错时会执行命令，一律不带")
		case hasAnyPrefix(a, "-XX:Flags", "-XX:VMOptionsFile"):
			drop(a, "会从别的文件里再读参数，一律不带")
		case len(a) > 2 && strings.HasPrefix(strings.ToLower(a), "-dlog4j"):
			drop(a, "日志配置由 MineDeck 按版本处理")
		case strings.IndexFunc(a, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '"' }) >= 0:
			drop(a, "带空格或引号，设置里存不了")
		case strings.HasPrefix(a, "-XX:") && len(a) > 4, !strings.HasPrefix(a, "-XX:") && len(a) > 2 && hasAnyPrefix(a, "-D", "-X"):
			if !slices.Contains(carried, a) && len(carried) < 100 {
				carried = append(carried, a)
			}
		default:
			drop(a, "只带 -XX:、-D、-X 开头的")
		}
	}
	return carried, dropped
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(s, p) })
}

// applyScriptJVM 把启动脚本里能带的 JVM 参数放进 inst.ExtraJVM，带了哪些、没带哪些写进任务日志和任务的结果（告警）。
func applyScriptJVM(t *tasks.Task, inst *Instance, pack *modpack.Pack) {
	sj, ok := packScriptJVM(pack, inst.LaunchTarget)
	if !ok || len(sj.carried)+len(sj.dropped) == 0 {
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
	note := "只带 -XX:、-D、-X 开头的；内存按导入页上填的；能让 Java 另外加载程序、执行命令的（-javaagent、-XX:OnError 这类）和日志配置一律不带，需要的话确认可信以后到「设置 → 启动与内存」里自己加。"
	if len(sj.carried) > 0 {
		t.Logf("%s 里的这些 JVM 参数带到了「自定义 JVM 参数」：%s", sj.script, strings.Join(sj.carried, " "))
		note = "带过来的在「设置 → 启动与内存」的自定义 JVM 参数里：" + strings.Join(sj.carried, " ") +
			"。它们是给原来那个 Java 写的，换了大版本以后有的可能不认（比如老的 -XX:+UseConcMarkSweepGC），开不了服就到那里删掉。" + note
	}
	items := make([]tasks.WarnItem, 0, len(sj.dropped))
	for _, d := range sj.dropped {
		items = append(items, tasks.WarnItem{Name: fmt.Sprintf("%s（%s）", d.arg, d.why)})
	}
	if len(items) > 0 {
		names := make([]string, 0, len(sj.dropped))
		for _, it := range items {
			names = append(names, it.Name)
		}
		t.Logf("%s 里的这些 JVM 参数没带过来：%s", sj.script, strings.Join(names, "、"))
	}
	t.AddWarning(tasks.Warning{Kind: "script-jvm", Title: title, Note: note, Items: items})
}
