// Package diag 本地规则排错（设计文档 §5.3）：从一次启动的控制台输出里认出 NeoForge / Forge 等服务端的常见报错，
// 给出中文结论和修复办法。不联网、不花钱，没配置 AI 也照常工作；控制台顶部的报错横幅就是它的结果。
//
// 另有给 AI 打包上下文用的精简函数（Condense、CrashSummary、HsErrSummary）。
//
// 不依赖 Windows，Linux 上可以直接测试。规则库的样本在 testdata/。
package diag

import (
	"regexp"
	"strings"

	"minedeck/internal/mclog"
	"minedeck/internal/modinfo"
)

// Input 要分析的东西。只有 Lines 是必需的，其余的有就用（比如推荐内存要知道本机内存）。
type Input struct {
	Lines     []string // 控制台输出（一般是最近一次启动以来的，见 SinceStart）
	Core      string   // neoforge / forge / fabric / paper …
	MC        string   // Minecraft 版本
	JavaMajor int
	XmxMB     int
	RAMMB     int // 本机内存总量
	AvailMB   int // 本机可用内存
	Port      int // server.properties 里的端口
	Mods      []modinfo.Mod
	// FindClass 这个类在哪个 jar 里（客户端模组规则用：报错里只有类名时据此找到 jar）；可以为 nil
	FindClass func(class string) string
}

// Finding 一条结论。
type Finding struct {
	Rule     string   `json:"rule"`               // 规则编号，如 missing-deps
	Title    string   `json:"title"`              // 「缺少前置模组」
	Summary  string   `json:"summary"`            // 一句话结论
	Details  []string `json:"details,omitempty"`  // 逐项说明
	Fixes    []string `json:"fixes,omitempty"`    // 怎么修
	Evidence []string `json:"evidence,omitempty"` // 认出来的那几行原文（最多 5 行）
	Mods     []string `json:"mods,omitempty"`     // 相关的模组 ID
	Files    []string `json:"files,omitempty"`    // 相关的 jar 文件名
	Severity string   `json:"severity"`           // error：起不来或崩了；warn：还能跑，但有问题
}

// Report 一次分析的结果。
type Report struct {
	Findings []Finding `json:"findings"`
	Crashed  bool      `json:"crashed"`        // 服务器自己退出了（不是用户停的）
	Exit     string    `json:"exit,omitempty"` // 退出时 MineDeck 写的那一行
	Lines    int       `json:"lines"`          // 分析了多少行
}

// StartMark MineDeck 每次启动服务器时写进控制台的第一行的开头。
const StartMark = "[MineDeck] 启动:"

// SinceStart 取最近一次启动以来的输出（含启动那一行）；没有启动记录时原样返回。
func SinceStart(lines []string) []string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], StartMark) {
			return lines[i:]
		}
	}
	return lines
}

// line 一行输出：原文（去掉颜色和格式码）、是不是聊天或 MineDeck 自己写的行。
type line struct {
	text  string
	skip  bool // 玩家聊天、MineDeck 的命令回显和提示：规则不看这些行（玩家打一句「FAILED TO BIND TO PORT」不算）
	level string
}

// Analyze 按规则逐条检查。
func Analyze(in Input) Report {
	ls := make([]line, len(in.Lines))
	for i, raw := range in.Lines {
		p := mclog.Parse(raw)
		say := p.Level == "INFO" && sayRe.MatchString(p.Body) // /say、/me 的广播：玩家或控制台说的话，同样不看
		ls[i] = line{text: p.Text, skip: p.Kind == mclog.Chat || p.Kind == mclog.MineDeck || say, level: p.Level}
	}
	rep := Report{Findings: []Finding{}, Lines: len(ls)}
	rep.Crashed, rep.Exit = exitState(in.Lines)
	ctx := &ruleCtx{in: in, ls: ls}
	for _, r := range rules {
		if f := r(ctx); f != nil {
			if f.Severity == "" {
				f.Severity = "error"
			}
			rep.Findings = append(rep.Findings, *f)
		}
	}
	if rep.Crashed {
		hasErr := false
		for _, f := range rep.Findings {
			hasErr = hasErr || f.Severity == "error"
		}
		if !hasErr {
			rep.Findings = append(rep.Findings, Finding{Rule: "crash", Title: "服务器异常退出", Severity: "error",
				Summary:  "服务器自己退出了，本地规则没认出具体原因。",
				Fixes:    []string{"看看控制台里退出前最后几行 ERROR 和异常", "配置了 AI 的话，可以让 AI 结合崩溃报告分析"},
				Evidence: []string{rep.Exit}})
		}
	}
	return rep
}

// sayRe /say 的广播「[Server] …」「[Steve] …」和 /me 的「* Steve …」。
var sayRe = regexp.MustCompile(`^(\[[A-Za-z0-9_ ]{1,16}\] |\* [A-Za-z0-9_]{1,16} )`)

var (
	userStopRe = regexp.MustCompile(`^\[MineDeck\] (正在保存世界并停止服务器|强制结束进程|停止超时，强制结束进程|服务器正在关闭（有人执行了 stop）)`)
	stopCmdRe  = regexp.MustCompile(`^> /?(?i:stop)(\s|$)`) // 控制台里打的 stop（inst 的 stopCommand 同样认大小写和前面的 /）
	exitRe     = regexp.MustCompile(`^\[MineDeck\] (服务器进程已退出|服务器已正常关闭)`)
)

// exitState 最近一次启动之后服务器有没有自己退出：有退出行，而且之前没有用户停服、强杀或在控制台打 stop。
func exitState(lines []string) (crashed bool, exit string) {
	lines = SinceStart(lines)
	user := false
	for _, l := range lines {
		switch {
		case userStopRe.MatchString(l), stopCmdRe.MatchString(l):
			user = true
		case exitRe.MatchString(l):
			return !user, l
		}
	}
	return false, ""
}

// ruleCtx 规则共用的东西。
type ruleCtx struct {
	in Input
	ls []line
}

// find 找匹配 re 的行（跳过聊天和 MineDeck 的行），返回下标。
func (c *ruleCtx) find(re *regexp.Regexp) []int {
	var out []int
	for i, l := range c.ls {
		if !l.skip && re.MatchString(l.text) {
			out = append(out, i)
		}
	}
	return out
}

// evidence 取这几行原文（最多 5 行，每行最多 300 字）。
func (c *ruleCtx) evidence(idx []int) []string {
	var out []string
	seen := map[string]bool{}
	for _, i := range idx {
		t := strings.TrimSpace(c.ls[i].text)
		if r := []rune(t); len(r) > 300 {
			t = string(r[:300]) + "…"
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
		if len(out) == 5 {
			break
		}
	}
	return out
}

// modByMixin 按 mixin 配置文件名找模组。
func (c *ruleCtx) modByMixin(cfg string) (id, file string) {
	for _, m := range c.in.Mods {
		for _, x := range m.Mixins {
			if x == cfg {
				return m.ID, m.File
			}
		}
	}
	return "", ""
}

// modFile 按模组 ID 找 jar。
func (c *ruleCtx) modFile(id string) string {
	for _, m := range c.in.Mods {
		if m.ID == id || contains(m.Also, id) {
			return m.File
		}
	}
	return ""
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func uniq(ss []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// isModded 是不是模组服（有 mods 目录的核心）。
func (c *ruleCtx) isModded() bool {
	switch c.in.Core {
	case "neoforge", "forge", "fabric", "mohist", "banner":
		return true
	}
	return false
}
