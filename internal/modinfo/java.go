package modinfo

import (
	"strconv"
	"strings"
)

// JavaMajorOf 从模组声明的依赖里解出「要求的最低 Java 大版本」：fabric.mod.json / quilt 的 depends.java
// （如 ">=25"、">=17 <21"）、mods.toml 里 modId 为 java 的 versionRange（如 "[25,)"、"[17,25)"）。
// 多条声明取最高的下界；写法读不出、或解出来不像 Java 版本号（8–40 以外）时返回 0（=没有可识别的要求）。
// 只用来提示（导入结果、实例详情的「建议换 Java」）：1.20.1 的实例配声明 Java 25 的魔改模组是允许存在的，
// 由用户决定要不要换，不阻断启动。见 internal/inst 的导入扫描和 instance.mods.list 的 needJava。
func JavaMajorOf(m Mod) int {
	best := 0
	for _, d := range m.Deps {
		if !strings.EqualFold(strings.TrimSpace(d.ID), "java") {
			continue
		}
		for _, part := range strings.Split(d.Range, "||") {
			if v := javaMajorOfExpr(part); v > best {
				best = v
			}
		}
	}
	return best
}

// javaMajorOfExpr 取一个版本表达式里的「下界 Java 大版本」：
// 支持 ">=25"（Fabric 常写法）、"[25,)"（Maven 区间下界）、"25"、"25.x"、"~21" 这几种；
// "<21" 这类上界、"*" 和读不懂的写法返回 0（不猜）。
func javaMajorOfExpr(expr string) int {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "*" {
		return 0
	}
	// Maven 区间：[a,b) / [a,b] / (a,b)：下界是逗号前的那一段；没有逗号时括号里整体是下界
	if strings.HasPrefix(expr, "[") || strings.HasPrefix(expr, "(") {
		end := strings.Index(expr, ",")
		if end < 0 {
			end = len(expr)
		}
		return javaMajorOfVersion(strings.Trim(strings.TrimSpace(expr[:end]), "[]()"))
	}
	// 合取（空格分隔）：只有带 ">= / >" 下界操作符的那一段算要求，"<21" 这类上界不算。
	// 操作符和数字之间可能隔着空格（">= 17"），拼回来看。
	toks := strings.Fields(expr)
	for i, tok := range toks {
		for _, op := range []string{">=", ">"} {
			if !strings.HasPrefix(tok, op) {
				continue
			}
			rest := strings.TrimPrefix(tok, op)
			if rest == "" && i+1 < len(toks) {
				rest = toks[i+1]
			}
			return javaMajorOfVersion(rest)
		}
	}
	// 单独一个前缀操作符：">=25"、">25"、"~21"、"^21"——都按「至少这个大版本」处理
	rest := expr
	for _, op := range []string{">=", ">", "^", "~"} {
		if strings.HasPrefix(rest, op) {
			return javaMajorOfVersion(strings.TrimSpace(strings.TrimPrefix(rest, op)))
		}
	}
	// 上界写法（"<21"、"<=17.0.11"）不当要求
	if strings.HasPrefix(expr, "<") {
		return 0
	}
	return javaMajorOfVersion(expr)
}

// javaMajorOfVersion 从版本号字符串里取大版本：取第一个点号前的数字，必须在 8–40 之间才算
// （设下界防把 MC 版本号 1.20.1 之类误认成 Java 版本）。
func javaMajorOfVersion(v string) int {
	v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "\"'"))
	if v == "" {
		return 0
	}
	// "25.x" 里的 x、范围里的空格都不算数字：只取开头的数字段
	digits := ""
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			digits += string(v[i])
			continue
		}
		break
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 8 || n > 40 {
		return 0
	}
	return n
}
