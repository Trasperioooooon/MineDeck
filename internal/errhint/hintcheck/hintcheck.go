// Package hintcheck 仅用于测试校验界面提示文案是否满足前端 errSummary 的过滤规则。
package hintcheck

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	reWinDrive   = regexp.MustCompile(`(?:^|[^A-Za-z0-9])[A-Za-z]:[\\/]`)
	reBackslash  = regexp.MustCompile(`[^\s\\「『"'(（]\\[^\s\\」』"')）]`)
	reUnixPath   = regexp.MustCompile(`(?:^|[^\w./\\-])\/[\w.-]+\/`)
	reRelFileExt = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,7}\b`)
	reExecErr    = regexp.MustCompile(`(?i)fork/exec|exit status|\bi/o\b`)
	reURL        = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s」』"')）>]`)
	reColonSpace = regexp.MustCompile(`: `)
	reEnWords    = regexp.MustCompile(`\b[a-z]{2,}(?: [a-z]{2,})+\b`)
	reEnPhrase   = regexp.MustCompile(`(?i)\b(?:bad gateway|bad request|not found|forbidden|unauthori[sz]ed|internal server error|service unavailable|gateway time-?out|too many requests|access denied|permission denied|connection (?:refused|reset|closed)|timed out|no such host|broken pipe)\b`)
	reEOF        = regexp.MustCompile(`\bEOF\b`)
	reHTTPCode   = regexp.MustCompile(`(?i)\bHTTP\s*\d{3}\b`)
	reHex        = regexp.MustCompile(`(?i)\b0x[0-9a-f]{4,}\b`)
)

// CheckUIHint 核对一条修复提示是否符合前端 errSummary 的全部展示约束（含中文、<=60 字、无技术痕迹）。
func CheckUIHint(hint string) error {
	if strings.TrimSpace(hint) == "" {
		return fmt.Errorf("hint 不能为空")
	}
	hasHan := false
	for _, r := range hint {
		if r >= 0x3400 && r <= 0x9FFF {
			hasHan = true
			break
		}
	}
	if !hasHan {
		return fmt.Errorf("hint 缺少中文：%q", hint)
	}
	if n := utf8.RuneCountInString(hint); n > 60 {
		return fmt.Errorf("hint 超过 60 字（%d 字）：%q", n, hint)
	}
	if strings.ContainsAny(hint, "\r\n") {
		return fmt.Errorf("hint 不能换行：%q", hint)
	}
	for _, pat := range []*regexp.Regexp{
		reWinDrive, reBackslash, reUnixPath, reRelFileExt, reExecErr,
		reURL, reColonSpace, reEnWords, reEnPhrase, reEOF, reHTTPCode, reHex,
	} {
		if pat.MatchString(hint) {
			return fmt.Errorf("hint 命中前端技术痕迹过滤（%s）：%q", pat.String(), hint)
		}
	}
	return nil
}
