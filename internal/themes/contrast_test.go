// Package themes 只含测试：校验前端配色方案（internal/web/ui/themes.css）的 WCAG 对比度。
// 不依赖 Windows 专属代码，任何平台都能跑：go test ./internal/themes/
package themes

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type rgba struct{ r, g, b, a float64 }

var (
	reComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reBlock   = regexp.MustCompile(`((?:\[data-[^{]+)|:root)\s*\{([^}]*)\}`)
	reVar     = regexp.MustCompile(`(--[\w-]+)\s*:\s*([^;]+);`)
	reScheme  = regexp.MustCompile(`^\[data-scheme="(\w+)"\]\[data-theme="(light|dark)"\]$`)
	reAccent  = regexp.MustCompile(`^\[data-accent="(\w+)"\]\[data-theme="(light|dark)"\]$`)
	reHex     = regexp.MustCompile(`^#([0-9a-f]{3}|[0-9a-f]{6})$`)
	reRGB     = regexp.MustCompile(`^rgba?\(([^)]+)\)$`)
)

func parseColor(s string) (rgba, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if m := reHex.FindStringSubmatch(s); m != nil {
		h := m[1]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		v, _ := strconv.ParseUint(h, 16, 32)
		return rgba{float64(v >> 16 & 255), float64(v >> 8 & 255), float64(v & 255), 1}, nil
	}
	if m := reRGB.FindStringSubmatch(s); m != nil {
		p := strings.Split(m[1], ",")
		if len(p) < 3 {
			return rgba{}, fmt.Errorf("颜色分量不足: %s", s)
		}
		f := make([]float64, 4)
		f[3] = 1
		for i := range p {
			v, err := strconv.ParseFloat(strings.TrimSpace(p[i]), 64)
			if err != nil {
				return rgba{}, fmt.Errorf("无法解析颜色: %s", s)
			}
			f[i] = v
		}
		return rgba{f[0], f[1], f[2], f[3]}, nil
	}
	return rgba{}, fmt.Errorf("无法解析颜色: %s", s)
}

// over 把半透明前景 alpha 合成到不透明背景上。
func over(fg, bg rgba) rgba {
	a := fg.a
	return rgba{fg.r*a + bg.r*(1-a), fg.g*a + bg.g*(1-a), fg.b*a + bg.b*(1-a), 1}
}

func luminance(c rgba) float64 {
	ch := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
}

func ratio(x, y rgba) float64 {
	a, b := luminance(x), luminance(y)
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

type palette map[string]string

// color 取令牌的颜色；以 # 开头的是字面色值（app.css 里 color-mix 用到的 #000 等）。
func (p palette) color(k string) (rgba, error) {
	v := p[k]
	if strings.HasPrefix(k, "#") {
		v = k
	}
	c, err := parseColor(v)
	if err != nil {
		return rgba{}, fmt.Errorf("%s: %w", k, err)
	}
	return c, nil
}

// pair 计算 c.fg 叠在 c.bg 上的对比度：bg 半透明时先合成到 base 上；
// c.mix 非空时再按 c.pct 的比例把 mix 色混进去（对应 app.css 里的 color-mix(in srgb, mix pct%, bg)）。
func (p palette) pair(c check) (float64, error) {
	b, err := p.color(c.bg)
	if err != nil {
		return 0, err
	}
	if b.a < 1 {
		under, err := p.color(c.base)
		if err != nil {
			return 0, err
		}
		b = over(b, under)
	}
	if c.mix != "" {
		m, err := p.color(c.mix)
		if err != nil {
			return 0, err
		}
		b = over(rgba{m.r, m.g, m.b, c.pct}, b)
	}
	f, err := p.color(c.fg)
	if err != nil {
		return 0, err
	}
	if f.a < 1 {
		f = over(f, b)
	}
	return ratio(f, b), nil
}

type check struct {
	fg, bg, base string
	min          float64
	mix          string  // 可选：混入背景的颜色（令牌或字面色值）
	pct          float64 // mix 的比例（0–1）
}

// 与 design-system/MASTER.md「配色方案」一节的约定一致：正文 / 小字 ≥ 4.5，图形 ≥ 3。
// mode 用于只按明暗区分的派生色（如标签页轨道 --track，见 app.css）。
func schemeChecks(mode string) []check {
	surfaces := []string{"--bg", "--surface", "--chrome", "--overlay"}
	var cs []check
	for _, s := range append(append([]string{}, surfaces...), "--surface-2") {
		cs = append(cs, check{fg: "--text", bg: s, min: 4.5})
	}
	for _, s := range surfaces {
		cs = append(cs, check{fg: "--muted", bg: s, min: 4.5})
	}
	cs = append(cs,
		check{fg: "--text", bg: "--accent-subtle", base: "--chrome", min: 4.5}, // 当前导航项
		check{fg: "--text", bg: "--hover", base: "--chrome", min: 4.5},
		check{fg: "--on-danger", bg: "--danger-solid", min: 4.5},
		check{fg: "--on-danger", bg: "--danger-solid-hov", min: 4.5},
	)
	for _, k := range []string{"success", "warning", "danger"} {
		for _, s := range []string{"--surface", "--bg"} {
			cs = append(cs, check{fg: "--" + k + "-text", bg: s, min: 4.5}, check{fg: "--" + k + "-text", bg: "--" + k + "-subtle", base: s, min: 4.5})
		}
	}
	for _, k := range []string{"fg", "cmd", "warn", "err"} {
		cs = append(cs, check{fg: "--term-" + k, bg: "--term-bg", min: 4.5})
	}
	// v5：凸起控件（次要按钮、下拉、分段的底）与输入框上的文字
	for _, s := range []string{"--control", "--control-hov", "--control-act", "--field"} {
		cs = append(cs, check{fg: "--text", bg: s, min: 4.5})
	}
	for _, s := range []string{"--control", "--field", "--surface-2"} {
		cs = append(cs, check{fg: "--muted", bg: s, min: 4.5}) // 按钮里的次要字、输入框占位符
	}
	// 标签页 / 分段控件的凹陷轨道：浅色 = 正文色 6% 混进页面底，深色 = 黑 30% 混进页面底（同 app.css 的 --track）
	track := check{bg: "--bg", mix: "--text", pct: .06}
	if mode == "dark" {
		track = check{bg: "--bg", mix: "#000000", pct: .30}
	}
	for _, fg := range []string{"--text", "--muted"} {
		t := track
		t.fg, t.min = fg, 4.5
		cs = append(cs, t)
	}
	cs = append(cs,
		check{fg: "--danger-text", bg: "--control", min: 4.5},                    // 列表里的危险按钮（红字）
		check{fg: "--text", bg: "--hover", base: "--surface", min: 4.5},          // 第三级按钮、菜单项悬停（卡片上 / 浮层上）
		check{fg: "--text", bg: "--hover", base: "--bg", min: 4.5},               // 第三级按钮悬停（页面底上）
		check{fg: "--muted", bg: "--surface", mix: "--text", pct: .07, min: 4.5}, // 徽标、已停止的状态胶囊
		check{fg: "--muted", bg: "--bg", mix: "--text", pct: .06, min: 4.5},      // 分组标题旁的计数
		check{fg: "--warning-text", bg: "--overlay", min: 4.5},                   // 吸底保存条里的「运行中：保存后需重启生效」
		check{fg: "--danger-text", bg: "--overlay", min: 4.5},                    // 菜单里的危险项
	)
	return append(cs, accentChecks()...)
}

func accentChecks() []check {
	var cs []check
	for _, s := range []string{"--bg", "--surface", "--chrome", "--overlay"} {
		cs = append(cs, check{fg: "--accent-text", bg: s, min: 4.5})
	}
	return append(cs,
		check{fg: "--accent-text", bg: "--accent-subtle", base: "--surface", min: 4.5},
		check{fg: "--muted", bg: "--accent-subtle", base: "--surface", min: 4.5}, // 选中版本行的类型 / 发布日期副行
		check{fg: "--on-accent", bg: "--accent-solid", min: 4.5},
		check{fg: "--on-accent", bg: "--accent-solid-hov", min: 4.5},
		check{fg: "--accent", bg: "--surface", min: 3},
		check{fg: "--accent", bg: "--bg", min: 3},
		// v5
		check{fg: "--accent-text", bg: "--control", min: 4.5},
		check{fg: "--text", bg: "--accent-subtle", base: "--surface", min: 4.5},        // 选中行、菜单当前项
		check{fg: "--text", bg: "--surface", mix: "--accent", pct: .06, min: 4.5},      // 选中的单选卡 / 核心卡
		check{fg: "--muted", bg: "--surface", mix: "--accent", pct: .06, min: 4.5},     // 同上，卡片里的说明
		check{fg: "--accent-text", bg: "--surface", mix: "--accent", pct: .12, min: 3}, // 设置行左侧的图标瓦片（图形）
	)
}

// defaultScheme 是默认配色方案（index.html、app.js 的默认值与它一致，见 TestSchemeListsInSync）。
const defaultScheme = "craft"

// v5 起每个方案 × 明暗都必须定义的层级令牌（app.css 直接引用，缺了会回退到别的方案的值）。
var v5Tokens = []string{"--control", "--control-hov", "--control-act", "--field", "--shadow-rgb"}

// parseThemes 解析 themes.css，返回 方案 → 明暗 → 变量，以及 强调色 → 明暗 → 变量。
func parseThemes(t *testing.T) (schemes, accents map[string]map[string]palette) {
	t.Helper()
	raw, err := os.ReadFile("../web/ui/themes.css")
	if err != nil {
		t.Fatal(err)
	}
	css := reComment.ReplaceAllString(string(raw), "")
	schemes = map[string]map[string]palette{}
	accents = map[string]map[string]palette{}
	for _, m := range reBlock.FindAllStringSubmatch(css, -1) {
		sel := strings.TrimSpace(m[1])
		vars := palette{}
		for _, v := range reVar.FindAllStringSubmatch(m[2], -1) {
			vars[v[1]] = strings.TrimSpace(v[2])
		}
		if s := reScheme.FindStringSubmatch(sel); s != nil {
			if schemes[s[1]] == nil {
				schemes[s[1]] = map[string]palette{}
			}
			schemes[s[1]][s[2]] = vars
		} else if a := reAccent.FindStringSubmatch(sel); a != nil {
			if accents[a[1]] == nil {
				accents[a[1]] = map[string]palette{}
			}
			accents[a[1]][a[2]] = vars
		}
	}
	return schemes, accents
}

func TestThemeContrast(t *testing.T) {
	schemes, accents := parseThemes(t)
	if len(schemes) < 7 || len(accents) < 8 {
		t.Fatalf("解析到的方案 / 强调色数量异常：%d / %d", len(schemes), len(accents))
	}
	if _, ok := schemes[defaultScheme]; !ok {
		t.Fatalf("缺少默认方案 %s", defaultScheme)
	}
	n := 0
	run := func(ctx string, p palette, cs []check) {
		for _, c := range cs {
			n++
			r, err := p.pair(c)
			if err != nil {
				t.Errorf("%s: %v", ctx, err)
				continue
			}
			if r < c.min {
				on := c.bg
				if c.base != "" {
					on += "@" + c.base
				}
				if c.mix != "" {
					on = fmt.Sprintf("%s 混入 %.0f%% %s", on, c.pct*100, c.mix)
				}
				t.Errorf("%s: %s 叠在 %s 上对比度 %.2f < %.1f", ctx, c.fg, on, r, c.min)
			}
		}
	}
	for name, modes := range schemes {
		for _, mode := range []string{"light", "dark"} {
			base, ok := modes[mode]
			if !ok {
				t.Errorf("方案 %s 缺少 %s 模式", name, mode)
				continue
			}
			for _, k := range v5Tokens {
				if _, ok := base[k]; !ok {
					t.Errorf("方案 %s/%s 缺少令牌 %s", name, mode, k)
				}
			}
			run(name+"/"+mode, base, schemeChecks(mode))
			for acc, am := range accents {
				merged := palette{}
				for k, v := range base {
					merged[k] = v
				}
				for k, v := range am[mode] {
					merged[k] = v
				}
				run(name+"/"+mode+"+"+acc, merged, accentChecks())
			}
		}
	}
	t.Logf("校验 %d 个方案 × 2 模式 + %d 种强调色，共 %d 组对比度", len(schemes), len(accents), n)
}

// index.html（首帧前落属性）与 app.js（「设置 → 外观」的方案卡）各有一份方案列表与默认值：
// 两份列表都必须与 themes.css 里定义的方案一一对应，默认值都必须是 defaultScheme。
func TestSchemeListsInSync(t *testing.T) {
	schemes, _ := parseThemes(t)
	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile("../web/ui/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	ids := func(list string, re *regexp.Regexp) []string {
		var out []string
		for _, m := range re.FindAllStringSubmatch(list, -1) {
			out = append(out, m[1])
		}
		return out
	}
	same := func(where string, got []string) {
		t.Helper()
		seen := map[string]bool{}
		for _, id := range got {
			if _, ok := schemes[id]; !ok {
				t.Errorf("%s 列出了 themes.css 里没有的方案 %s", where, id)
			}
			seen[id] = true
		}
		for id := range schemes {
			if !seen[id] {
				t.Errorf("%s 缺少方案 %s", where, id)
			}
		}
	}
	html := read("index.html")
	if m := regexp.MustCompile(`var SCHEMES = \[([^\]]*)\]`).FindStringSubmatch(html); m == nil {
		t.Error("index.html 里找不到 SCHEMES 列表")
	} else {
		same("index.html", ids(m[1], regexp.MustCompile(`"(\w+)"`)))
	}
	for _, re := range []string{`get\("amh_scheme", "(\w+)"\)`, `SCHEMES\.indexOf\(scheme\) < 0\) scheme = "(\w+)"`} {
		if m := regexp.MustCompile(re).FindStringSubmatch(html); m == nil || m[1] != defaultScheme {
			t.Errorf("index.html 的默认方案应为 %s：%v", defaultScheme, m)
		}
	}
	js := read("app.js")
	if m := regexp.MustCompile(`(?s)const SCHEMES = \[(.*?)\];`).FindStringSubmatch(js); m == nil {
		t.Error("app.js 里找不到 SCHEMES 列表")
	} else {
		got := ids(m[1], regexp.MustCompile(`id: "(\w+)"`))
		same("app.js", got)
		if len(got) == 0 || got[0] != defaultScheme {
			t.Errorf("app.js 的方案列表应以默认方案 %s 开头：%v", defaultScheme, got)
		}
	}
	if m := regexp.MustCompile(`scheme: d\.scheme \|\| "(\w+)"`).FindStringSubmatch(js); m == nil || m[1] != defaultScheme {
		t.Errorf("app.js 读取外观时的默认方案应为 %s：%v", defaultScheme, m)
	}
}

// 原生窗口在页面画出来之前用的默认底色（webviewbg_windows.go，用户还没上报过界面底色时用）
// 必须等于默认方案的标题栏色 --chrome，否则冷启动会先闪一下别的颜色。
func TestDefaultWindowBackground(t *testing.T) {
	schemes, _ := parseThemes(t)
	src, err := os.ReadFile("../../cmd/minedeck/webviewbg_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for mode, name := range map[string]string{"light": "defaultChromeLight", "dark": "defaultChromeDark"} {
		m := regexp.MustCompile(name + `\s*=\s*"(#[0-9a-f]{6})"`).FindSubmatch(src)
		if m == nil {
			t.Errorf("webviewbg_windows.go 里找不到 %s", name)
			continue
		}
		if want := schemes[defaultScheme][mode]["--chrome"]; string(m[1]) != want {
			t.Errorf("%s = %s，应与方案 %s/%s 的 --chrome（%s）一致", name, m[1], defaultScheme, mode, want)
		}
	}
}
