package companion

import (
	"strconv"
	"strings"
)

// InRange 版本 v 在不在 Maven 写法的版本范围 r 里（mods.toml 的 versionRange）：
// 支持 [a,b)、[a,b]、(a,b)、(a,b]、[a,)、(,b)、[a]，以及几段用逗号隔开的并集（[1,2),[3,4)）。
// 不带括号的单个版本（Maven 的「推荐版本」）按「不低于它」算。写法认不出时返回 false。
func InRange(v, r string) bool {
	r = strings.TrimSpace(r)
	if r == "" || r == "*" {
		return true
	}
	if !strings.ContainsAny(r, "[]()") {
		return CompareVersion(v, r) >= 0
	}
	for _, part := range splitRanges(r) {
		if ok, valid := inInterval(v, part); valid && ok {
			return true
		}
	}
	return false
}

// splitRanges 把「[1,2),[3,4)」拆成几段。
func splitRanges(r string) []string {
	var out []string
	start := -1
	for i, c := range r {
		switch c {
		case '[', '(':
			start = i
		case ']', ')':
			if start >= 0 {
				out = append(out, r[start:i+1])
				start = -1
			}
		}
	}
	return out
}

func inInterval(v, iv string) (ok, valid bool) {
	if len(iv) < 2 {
		return false, false
	}
	lo, hi := iv[0], iv[len(iv)-1]
	body := iv[1 : len(iv)-1]
	a, b, comma := strings.Cut(body, ",")
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if !comma { // [1.0]：正好这个版本
		return lo == '[' && hi == ']' && CompareVersion(v, a) == 0, lo == '[' && hi == ']'
	}
	if a != "" {
		c := CompareVersion(v, a)
		if c < 0 || (c == 0 && lo == '(') {
			return false, true
		}
	}
	if b != "" {
		c := CompareVersion(v, b)
		if c > 0 || (c == 0 && hi == ')') {
			return false, true
		}
	}
	return true, true
}

// CompareVersion 比较两个版本号：按 . - + 分段，两段都是数字时按数字比，否则按字符串比；少一段的算小。
// 够 PlayerEngine（1.21.1-1.4.0）、Architectury（13.0.11）和加载器版本（21.1.265、47.4.28）用。
func CompareVersion(a, b string) int {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	}
	pa, pb := split(a), split(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x == y {
			continue
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case x == "":
			return -1
		case y == "":
			return 1
		case xerr == nil && yerr == nil:
			if xn < yn {
				return -1
			}
			return 1
		case x < y:
			return -1
		default:
			return 1
		}
	}
	return 0
}
