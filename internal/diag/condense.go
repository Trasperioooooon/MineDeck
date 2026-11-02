package diag

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	levelRe     = regexp.MustCompile(`/(WARN|ERROR|FATAL)\]|^\[\d{2}:\d{2}:\d{2} (WARN|ERROR|FATAL)\]`)
	exceptionRe = regexp.MustCompile(`^(?:Exception in thread|Caused by: |[\w.$]+(?:Exception|Error)(?::|$)|\S.*\b[\w.$]+(?:Exception|Error): )`)
	frameLineRe = regexp.MustCompile(`^\s+at |^\s*\.\.\. \d+ more`)
	tsPrefixRe  = regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}(?: \w+)?\] ?`)
)

// Condense 把一次启动的控制台精简成发给 AI 的样子（§5.3）：只留 WARN / ERROR 行、异常块（异常那一行和
// Caused by，每块最多 15 行 at）、最后 tail 行；连着重复的行（不看时间戳）合并成「×N」；省略的地方写明省略了几行。
func Condense(lines []string, tail int) []string {
	keep := make([]bool, len(lines))
	frames := 0
	inEx := false
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "[MineDeck]"):
			keep[i] = true // 启动参数、退出码都有用
			inEx = false
		case levelRe.MatchString(l):
			keep[i] = true
			inEx = exceptionRe.MatchString(bodyOf(l)) || strings.Contains(l, "Exception") || strings.Contains(l, "Error")
			frames = 0
		case exceptionRe.MatchString(l):
			keep[i], inEx, frames = true, true, 0
		case frameLineRe.MatchString(l):
			if inEx && frames < 15 {
				keep[i] = true
			}
			frames++
		default:
			inEx = false
		}
	}
	for i := max(0, len(lines)-tail); i < len(lines); i++ {
		keep[i] = true
	}
	var out []string
	skipped := 0
	last, rep := "", 0
	flushRep := func() {
		if rep > 1 {
			out[len(out)-1] += fmt.Sprintf("  ×%d", rep)
		}
		rep = 0
	}
	for i, l := range lines {
		if !keep[i] {
			skipped++
			continue
		}
		if skipped > 0 {
			flushRep()
			last = ""
			out = append(out, fmt.Sprintf("……（省略 %d 行）", skipped))
			skipped = 0
		}
		key := tsPrefixRe.ReplaceAllString(l, "")
		if key == last && rep > 0 {
			rep++
			continue
		}
		flushRep()
		out = append(out, l)
		last, rep = key, 1
	}
	flushRep()
	if skipped > 0 {
		out = append(out, fmt.Sprintf("……（省略 %d 行）", skipped))
	}
	return out
}

var (
	crashHeadEnd = "A detailed walkthrough of the error"
	crashKeepRe  = regexp.MustCompile(`^\s*(Suspected Mods?:|Minecraft Version:|Minecraft Version ID:|Operating System:|Java Version:|Java VM Version:|Memory:|JVM Flags:|ModLauncher:|FML Loader:|Forge:|NeoForge:|Fabric Loader:|Loaded Mods|Server Running:|Is Modded:|Type:)`)
)

// CrashSummary 崩溃报告的节选（§5.3）：开头到第一个异常（每个异常块最多 15 行 at）、加载器标出的可疑模组，
// 以及 System Details 里的版本、Java、内存和 JVM 参数。
func CrashSummary(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	var out []string
	frames := 0
	i := 0
	for ; i < len(lines); i++ {
		l := lines[i]
		if strings.Contains(l, crashHeadEnd) {
			break
		}
		if frameLineRe.MatchString(l) {
			frames++
			if frames > 15 {
				continue
			}
		} else {
			frames = 0
		}
		out = append(out, l)
	}
	if i < len(lines) {
		out = append(out, "……")
	}
	for j := i; j < len(lines); j++ {
		l := lines[j]
		if !crashKeepRe.MatchString(l) {
			continue
		}
		out = append(out, l)
		if strings.Contains(l, "Suspected Mod") { // 可疑模组列在下面几行
			for k := j + 1; k < len(lines) && k < j+8 && strings.HasPrefix(lines[k], "\t\t"); k++ {
				out = append(out, lines[k])
			}
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// HsErrSummary hs_err_pid*.log（Java 虚拟机自己崩溃时写的）开头的问题摘要：最前面那段 # 开头的注释。
func HsErrSummary(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []string
	for _, l := range lines {
		if !strings.HasPrefix(l, "#") {
			if len(out) > 0 {
				break
			}
			continue
		}
		out = append(out, l)
		if len(out) >= 40 {
			break
		}
	}
	return strings.Join(out, "\n")
}

// LineLevel 一行输出的严重程度：error（ERROR / FATAL、异常行和堆栈）、warn（WARN），其余为空串。查看控制台时按级别过滤用。
func LineLevel(l string) string {
	if m := levelRe.FindStringSubmatch(l); m != nil {
		if m[1]+m[2] == "WARN" {
			return "warn"
		}
		return "error"
	}
	if exceptionRe.MatchString(l) || frameLineRe.MatchString(l) {
		return "error"
	}
	return ""
}
