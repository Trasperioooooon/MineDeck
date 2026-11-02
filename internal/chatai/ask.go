package chatai

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Opt 提问选项（设计文档 §6.2）。中文名为主，英文别名为辅。
type Opt string

const (
	OptPrivate Opt = "私"  // 只把回答发给提问的人
	OptHelp    Opt = "帮助" // 列出能用的选项，不调用 AI
	OptAsk     Opt = "问"  // 只回答、不执行命令：这一次不给 AI run_command 工具
	OptQuota   Opt = "额度" // 看自己今天还能问几次，不调用 AI
)

// optNames 中文名要完全一样；英文别名不分大小写。
var optNames = map[string]Opt{
	"私": OptPrivate, "帮助": OptHelp, "问": OptAsk, "额度": OptQuota,
}

var optAliases = map[string]Opt{
	"p": OptPrivate, "h": OptHelp, "help": OptHelp, "ask": OptAsk, "quota": OptQuota,
}

// Action 这句话怎么处理。
type Action string

const (
	ActAsk       Action = "ask"       // 提问：调用 AI
	ActHelp      Action = "help"      // 回帮助（带了 /帮助，或者没写问题）
	ActQuota     Action = "quota"     // 回额度
	ActUnknown   Action = "unknown"   // 不认识的选项：私下提示能用哪些
	ActNoSpace   Action = "nospace"   // 选项名后面直接接了问题（/私今晚怎么打末影龙）：提示空一格
	ActMisplaced Action = "misplaced" // 问题的最后一个词是 /私：多半是想私聊但放错了地方，不发给 AI
)

// CallsAI 这种处理要不要调用 AI（只有提问要）。不调用 AI 的回复不算次数、不扣额度，但算进防刷屏的计数（§6.7）。
func (a Action) CallsAI() bool { return a == ActAsk }

// Ask 一句触发了游戏内 AI 的话解析出来的样子。
type Ask struct {
	Prefix   string `json:"prefix"`            // 认出的前缀（原文）
	Opts     []Opt  `json:"opts"`              // 认出的选项：去重，按出现的顺序
	Private  bool   `json:"private"`           // /私
	NoExec   bool   `json:"noExec"`            // /问
	Question string `json:"question"`          // 问题，原样保留
	Action   Action `json:"action"`            // 怎么处理
	Unknown  string `json:"unknown,omitempty"` // 第一个不认识的选项名（最多 16 个字）
	Example  string `json:"example,omitempty"` // 提示里举例用的问题（选项后面没空格、选项放错位置时，最多 20 个字）
}

// isSep 分隔符：空格、全角空格、冒号、逗号（§6.1 第 5 步、§6.2 解析规则 3）。
func isSep(r rune) bool {
	switch r {
	case ' ', '\U00003000', ':', '：', ',', '，', '\t':
		return true
	}
	return false
}

// isMark 选项标记：/，也认全角的 ／ 和顿号 、（中文输入法在中文标点状态下按 / 键常常打出 、）。
func isMark(r rune) bool { return r == '/' || r == '／' || r == '、' }

// foldRune 比较前缀用：全角 ＠ 当作 @（中文输入法很容易打出全角），西文字母不分大小写。
func foldRune(r rune) rune {
	if r == '＠' {
		r = '@'
	}
	return unicode.ToLower(r)
}

// cutPrefix 去掉开头的前缀 p（全角 ＠ 当作 @，不分大小写）；对不上返回 false。
func cutPrefix(s, p string) (rest string, ok bool) {
	for _, pr := range p {
		r, n := utf8.DecodeRuneInString(s)
		if n == 0 || foldRune(r) != foldRune(pr) {
			return "", false
		}
		s = s[n:]
	}
	return s, true
}

// ParseAsk 认出触发前缀并解析选项：消息去掉首尾空白后以某个前缀开头，前缀后面是分隔符、选项标记或者直接结束，
// 才算触发（「@AIbot」不触发）。不触发返回 nil。prefixes 里先试长的，别名和主前缀一样对待。
func ParseAsk(msg string, prefixes []string) *Ask {
	msg = strings.TrimFunc(msg, unicode.IsSpace)
	var best string
	rest := ""
	for _, p := range prefixes {
		if p == "" || utf8.RuneCountInString(p) <= utf8.RuneCountInString(best) {
			continue
		}
		if r, ok := cutPrefix(msg, p); ok {
			if r != "" {
				if c, _ := utf8.DecodeRuneInString(r); !isSep(c) && !isMark(c) && !unicode.IsSpace(c) {
					continue
				}
			}
			best, rest = p, r
		}
	}
	if best == "" {
		return nil
	}
	a := &Ask{Prefix: msg[:len(msg)-len(rest)], Opts: []Opt{}}
	parseOpts(a, rest)
	return a
}

// parseOpts §6.2 的解析规则：选项只能写在前缀之后、问题之前；选项名到下一个分隔符为止；可以连着写几个，
// 重复的算一个；从第一个不以标记开头的词起，到结尾都是问题（里面再有 / 只是普通文字）。
func parseOpts(a *Ask, s string) {
	seen := map[Opt]bool{}
	var unknown []string
	noSpace, after := false, "" // after：第一个不认识的选项里，中文选项名后面的部分连同后文（举例时在选项名后面补个空格）
	i := skipSeps(s, 0)
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !isMark(r) {
			a.Question = strings.TrimRightFunc(s[i:], unicode.IsSpace)
			break
		}
		j := i + n
		for j < len(s) {
			c, m := utf8.DecodeRuneInString(s[j:])
			if isSep(c) || unicode.IsSpace(c) {
				break
			}
			j += m
		}
		name := s[i+n : j]
		if o, ok := lookupOpt(name); ok {
			if !seen[o] {
				seen[o] = true
				a.Opts = append(a.Opts, o)
			}
		} else {
			if len(unknown) == 0 {
				if noSpace = startsWithOptName(name); noSpace {
					after = s[i+n+len(optPrefixOf(name)):]
				}
			}
			unknown = append(unknown, name)
		}
		i = skipSeps(s, j)
	}
	a.Private, a.NoExec = seen[OptPrivate], seen[OptAsk]
	switch {
	case seen[OptHelp]: // 带了 /帮助 就只回帮助，不管后面的问题
		a.Action = ActHelp
	case len(unknown) > 0:
		a.Unknown = clip(unknown[0], 16)
		a.Action = ActUnknown
		if noSpace {
			a.Action = ActNoSpace
			a.Example = strings.TrimFunc(after, unicode.IsSpace)
		}
	case seen[OptQuota]: // 带了 /额度（没带 /帮助）就只回额度
		a.Action = ActQuota
	case a.Question == "": // 没写问题、也没带 /额度：当成 /帮助
		a.Action = ActHelp
	case misplaced(a.Question):
		a.Action = ActMisplaced
		words := strings.FieldsFunc(a.Question, func(r rune) bool { return unicode.IsSpace(r) })
		a.Example = strings.Join(words[:len(words)-1], " ")
	default:
		a.Action = ActAsk
	}
	if a.Example != "" {
		a.Example = clip(a.Example, 20)
	}
}

func skipSeps(s string, i int) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !isSep(r) && !unicode.IsSpace(r) {
			break
		}
		i += n
	}
	return i
}

func lookupOpt(name string) (Opt, bool) {
	if o, ok := optNames[name]; ok {
		return o, true
	}
	o, ok := optAliases[strings.ToLower(name)]
	return o, ok
}

// optPrefixOf name 开头的中文选项名（先试长的）；没有返回空串。
func optPrefixOf(name string) string {
	for _, n := range []string{"帮助", "额度", "私", "问"} {
		if strings.HasPrefix(name, n) {
			return n
		}
	}
	return ""
}

// startsWithOptName 选项名以某个中文选项名开头、后面直接接了别的字（/私今晚怎么打末影龙）。
func startsWithOptName(name string) bool {
	p := optPrefixOf(name)
	return p != "" && len(name) > len(p)
}

// misplaced 问题的最后一个词正好是 /私 或 /p（标记也认全角和顿号）：选项放错了地方（§6.2 解析规则 7）。
func misplaced(q string) bool {
	words := strings.FieldsFunc(q, func(r rune) bool { return unicode.IsSpace(r) })
	if len(words) < 2 {
		return false
	}
	w := words[len(words)-1]
	r, n := utf8.DecodeRuneInString(w)
	if !isMark(r) {
		return false
	}
	o, ok := lookupOpt(w[n:])
	return ok && o == OptPrivate
}

// clip 截到最多 n 个字。
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
