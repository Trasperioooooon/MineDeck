package modcart

import (
	"slices"
	"strconv"
	"strings"

	"minedeck/internal/modinfo"
)

// ---- 装完以后的本地核对 ----
//
// 不联网：用 modinfo 读服务器目录里每个模组自己声明的依赖（fabric.mod.json 的 depends、mods.toml 的 required 依赖），
// 对照目录里实际有的模组 ID（含 provides）。所以手动放进来的、魔改的、不在 Modrinth 上的模组，只要模组 ID 对得上就算满足。

// hostIDs 不是模组、由服务器本身提供的依赖：不算缺失。
var hostIDs = []string{"minecraft", "java", "forge", "neoforge", "fabricloader", "fabric-loader", "quilt_loader", "mohist"}

// mustHave 这条依赖是不是「服务器上必须另有一个模组」：必需的、不是不兼容声明、不是只在客户端要的、不是服务器本身提供的。
func mustHave(d modinfo.Dep) bool {
	return d.Required && d.Kind == "" && !strings.EqualFold(d.Side, "CLIENT") && !slices.Contains(hostIDs, strings.ToLower(d.ID))
}

// MissingMod 缺的或版本不对的依赖。
type MissingMod struct {
	ID         string `json:"id"`              // 缺的模组 ID
	Range      string `json:"range,omitempty"` // 要求的版本
	Have       string `json:"have,omitempty"`  // 版本不对时：现在的版本
	RequiredBy string `json:"requiredBy"`      // 谁需要它（名称）
	File       string `json:"file"`            // 需要它的那个模组的文件
}

// CheckLocal 核对目录里模组的必需依赖：missing 是一个都没有的，stale 是有但版本不在要求范围里的，checked 是读了几个模组。
// 停用的模组不算已装，它自己的依赖也不查；只在客户端用的依赖（side=CLIENT）不查。目录不存在时三样都是空的。
func CheckLocal(dir string) (missing, stale []MissingMod, checked int, err error) {
	mods, err := modinfo.ReadDir(dir)
	if err != nil {
		return nil, nil, 0, err
	}
	have := map[string]string{} // 模组 ID → 版本
	for _, m := range mods {
		if m.Disabled || m.ID == "" {
			continue
		}
		have[strings.ToLower(m.ID)] = m.Version
		for _, a := range m.Also {
			if _, ok := have[strings.ToLower(a)]; !ok {
				have[strings.ToLower(a)] = ""
			}
		}
	}
	missing, stale = []MissingMod{}, []MissingMod{}
	for _, m := range mods {
		if m.Disabled || m.ID == "" {
			continue
		}
		checked++
		by := orID(m.Name, m.ID)
		for _, d := range m.Deps {
			id := strings.ToLower(d.ID)
			if !mustHave(d) {
				continue
			}
			v, ok := have[id]
			switch {
			case !ok:
				missing = append(missing, MissingMod{ID: d.ID, Range: d.Range, RequiredBy: by, File: m.File})
			case v != "" && !strings.Contains(v, "${") && !InRange(v, d.Range):
				stale = append(stale, MissingMod{ID: d.ID, Range: d.Range, Have: v, RequiredBy: by, File: m.File})
			}
		}
	}
	return missing, stale, checked, nil
}

// InRange 版本 v 满不满足范围 r。认两种写法：
//   - Maven（mods.toml 的 versionRange）：[a,b)、(a,b]、[a,)、[a]，逗号隔开的几段是并集；
//   - Fabric 的版本谓词：>=、<=、>、<、=、~、^、通配符 x/*，空格隔开是「且」，|| 是「或」。
//
// 不带运算符的单个版本按「不低于它」算（Maven 的推荐版本是这个意思；Fabric 是「等于」，宽松些少报错）。
// 认不出的写法返回 true：拿不准时不报「版本不对」。
func InRange(v, r string) bool {
	r = strings.TrimSpace(r)
	if r == "" || r == "*" {
		return true
	}
	if strings.ContainsAny(r, "[]()") {
		ok, valid := mavenIn(v, r)
		return ok || !valid
	}
	for _, alt := range strings.Split(r, "||") {
		ok, valid := semverIn(v, strings.Fields(alt))
		if !valid {
			return true
		}
		if ok {
			return true
		}
	}
	return false
}

// mavenIn Maven 写法；valid=false 表示认不出。
func mavenIn(v, r string) (ok, valid bool) {
	start := -1
	for i, c := range r {
		switch c {
		case '[', '(':
			start = i
		case ']', ')':
			if start < 0 {
				return false, false
			}
			iv := r[start : i+1]
			start = -1
			in, good := interval(v, iv)
			if !good {
				return false, false
			}
			if in {
				ok = true
			}
			valid = true
		}
	}
	return ok, valid && start < 0
}

func interval(v, iv string) (ok, valid bool) {
	lo, hi := iv[0], iv[len(iv)-1]
	a, b, comma := strings.Cut(iv[1:len(iv)-1], ",")
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if !comma { // [1.0]：正好这个版本
		return lo == '[' && hi == ']' && compareVersion(v, a) == 0, lo == '[' && hi == ']' && a != ""
	}
	if a != "" {
		if c := compareVersion(v, a); c < 0 || (c == 0 && lo == '(') {
			return false, true
		}
	}
	if b != "" {
		if c := compareVersion(v, b); c > 0 || (c == 0 && hi == ')') {
			return false, true
		}
	}
	return true, true
}

// semverIn 一组都要满足的 Fabric 谓词；valid=false 表示认不出。
func semverIn(v string, terms []string) (ok, valid bool) {
	if len(terms) == 0 {
		return false, false
	}
	for _, t := range terms {
		in, good := semverTerm(v, t)
		if !good {
			return false, false
		}
		if !in {
			return false, true
		}
	}
	return true, true
}

func semverTerm(v, t string) (ok, valid bool) {
	if t == "*" {
		return true, true
	}
	op := ""
	for _, p := range []string{">=", "<=", ">", "<", "=", "~", "^"} {
		if strings.HasPrefix(t, p) {
			op, t = p, strings.TrimSpace(t[len(p):])
			break
		}
	}
	if t == "" || t[0] < '0' || t[0] > '9' { // 版本号都以数字开头；别的写法认不出
		return false, false
	}
	if hasWildcard(t) { // 1.20.x：前面几段一样就行
		if op != "" && op != "=" {
			return false, false
		}
		return wildcardMatch(v, t), true
	}
	c := compareVersion(v, t)
	switch op {
	case ">=", "":
		return c >= 0, true
	case "<=":
		return c <= 0, true
	case ">":
		return c > 0, true
	case "<":
		return c < 0, true
	case "=":
		return c == 0, true
	case "~": // ~1.20.1：>=1.20.1 且 <1.21
		return c >= 0 && samePrefix(v, t, 2), true
	case "^": // ^1.2.3：>=1.2.3 且 <2（0.x 时 <0.(x+1)）
		n := 1
		if p := numParts(t); len(p) > 0 && p[0] == 0 {
			n = 2
		}
		return c >= 0 && samePrefix(v, t, n), true
	}
	return false, false
}

func hasWildcard(t string) bool {
	for _, p := range strings.Split(t, ".") {
		if p == "x" || p == "X" || p == "*" {
			return true
		}
	}
	return false
}

func wildcardMatch(v, t string) bool {
	vp := strings.Split(core(v), ".")
	for i, p := range strings.Split(t, ".") {
		if p == "x" || p == "X" || p == "*" {
			return true
		}
		if i >= len(vp) || vp[i] != p {
			return false
		}
	}
	return true
}

// samePrefix 前 n 段数字一样（~、^ 的上界）。
func samePrefix(v, t string, n int) bool {
	a, b := numParts(v), numParts(t)
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return false
		}
	}
	return true
}

func numParts(v string) []int {
	var out []int
	for _, p := range strings.Split(core(v), ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// core 去掉构建信息（+ 后面）和预发布标记（- 后面）：0.92.2+1.20.1 → 0.92.2。
func core(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	return v
}

// compareVersion 比较版本号：去掉 + 后面的构建信息；主干按 . 分段，都是数字按数字比，否则按字符串比，少一段的算小；
// 主干一样时带预发布标记（-beta.1）的算小。
func compareVersion(a, b string) int {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if i := strings.IndexByte(a, '+'); i >= 0 {
		a = a[:i]
	}
	if i := strings.IndexByte(b, '+'); i >= 0 {
		b = b[:i]
	}
	ac, apre, _ := strings.Cut(a, "-")
	bc, bpre, _ := strings.Cut(b, "-")
	pa, pb := strings.Split(ac, "."), strings.Split(bc, ".")
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
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	case apre < bpre:
		return -1
	}
	return 1
}
