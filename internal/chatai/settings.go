package chatai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 设置和状态存在实例目录里（跟着实例走：删实例时一起删掉，同名的新实例不会继承旧的授权名单）。
const (
	settingsFile = "gameai.json"       // 用户改的设置
	stateFile    = "gameai-state.json" // 程序写的状态：当天的次数和用量、最近出现过的名字、最近的问答
)

// 限流和额度的默认值（§6.7）。
const (
	DefaultPrefix    = "@AI"
	DefaultCooldown  = 15      // 每人冷却（秒）
	DefaultPerPlayer = 30      // 每人每天几次
	DefaultPerServer = 300     // 全服每天几次
	DefaultBudget    = 500_000 // 全服每天多少 token
)

// Settings 一个实例的游戏内 AI 设置。
type Settings struct {
	Enabled   bool            `json:"enabled"`
	Prefix    string          `json:"prefix"`            // 触发前缀，默认 @AI
	Aliases   []string        `json:"aliases"`           // 别名，比如 @小助手
	Allowed   []string        `json:"allowed"`           // 能让 AI 执行命令的玩家（默认没有）
	Templates map[string]bool `json:"templates"`         // 命令模板的开关（每个模板都有）
	Cooldown  int             `json:"cooldown"`          // 每人冷却（秒），0 = 不冷却
	PerPlayer int             `json:"perPlayer"`         // 每人每天几次，0 = 不限
	PerServer int             `json:"perServer"`         // 全服每天几次，0 = 不限
	Budget    int64           `json:"budget"`            // 全服每天多少 token，0 = 不限（另有「设置 → AI」里游戏内 AI 的总额度）。键名不带 token：操作记录会把带 token 的键当密钥写成 ***
	Loose     bool            `json:"looseNames"`        // 放宽名字规则
	AnyThread bool            `json:"anyThread"`         // 不看线程名
	Pattern   string          `json:"pattern,omitempty"` // 自定义正则（命名分组 name、msg）
}

// Defaults 没设置过的实例：关着，前缀 @AI，没人能执行命令，模板和限流按 §6.6、§6.7 的默认值。
func Defaults() Settings {
	s := Settings{Prefix: DefaultPrefix, Aliases: []string{}, Allowed: []string{}, Templates: map[string]bool{},
		Cooldown: DefaultCooldown, PerPlayer: DefaultPerPlayer, PerServer: DefaultPerServer, Budget: DefaultBudget}
	for _, t := range Templates {
		s.Templates[t.ID] = t.Default
	}
	return s
}

// Prefixes 主前缀和别名。
func (s Settings) Prefixes() []string { return append([]string{s.Prefix}, s.Aliases...) }

// Rules 识别规则（自定义正则编译不过时当作没填：保存时已经检查过，这里只防手改的文件）。
func (s Settings) Rules() Rules {
	re, _ := compilePattern(s.Pattern)
	return Rules{Prefixes: s.Prefixes(), LooseNames: s.Loose, AnyThread: s.AnyThread, Pattern: re}
}

// Authorized 这个人能不能让 AI 执行命令（名字不分大小写）。
func (s Settings) Authorized(player string) bool {
	return slices.ContainsFunc(s.Allowed, func(n string) bool { return strings.EqualFold(n, player) })
}

// enabledTemplates 打开了的模板（只认登记过的编号）。
func (s Settings) enabledTemplates() map[string]bool {
	out := map[string]bool{}
	for _, t := range Templates {
		if s.Templates[t.ID] {
			out[t.ID] = true
		}
	}
	return out
}

// checkPrefix 前缀：1–16 个字，不含空白、选项标记（/ ／ 、）、分隔符（冒号、逗号）、< > " \ §，不以 / 开头（会被当成命令）。
func checkPrefix(p string) error {
	if n := utf8.RuneCountInString(p); n == 0 || n > 16 {
		return fmt.Errorf("前缀要 1–16 个字：%q", p)
	}
	for _, r := range p {
		if unicode.IsSpace(r) || isMark(r) || isSep(r) || strings.ContainsRune(`<>"\§`, r) || unicode.IsControl(r) {
			return fmt.Errorf("前缀「%s」里有不能用的字符（空格、/、、、冒号、逗号、< > \" \\ §）", p)
		}
	}
	return nil
}

// Patch 改设置：只改传了的字段（界面一次只改一块）。
type Patch struct {
	Enabled   *bool           `json:"enabled"`
	Prefix    *string         `json:"prefix"`
	Aliases   *[]string       `json:"aliases"`
	Allowed   *[]string       `json:"allowed"`
	Templates map[string]bool `json:"templates"` // 只改列出的模板
	Cooldown  *int            `json:"cooldown"`
	PerPlayer *int            `json:"perPlayer"`
	PerServer *int            `json:"perServer"`
	Budget    *int64          `json:"budget"`
	Loose     *bool           `json:"looseNames"`
	AnyThread *bool           `json:"anyThread"`
	Pattern   *string         `json:"pattern"`
}

// Apply 把改动用到 s 上并检查；不合规时整次不改，返回原因。
func (p Patch) Apply(s Settings) (Settings, error) {
	s.Aliases, s.Allowed, s.Templates = slices.Clone(s.Aliases), slices.Clone(s.Allowed), cloneMap(s.Templates)
	if p.Enabled != nil {
		s.Enabled = *p.Enabled
	}
	if p.Prefix != nil {
		v := strings.TrimSpace(*p.Prefix)
		if err := checkPrefix(v); err != nil {
			return s, err
		}
		s.Prefix = v
	}
	if p.Aliases != nil {
		var out []string
		for _, a := range *p.Aliases {
			if a = strings.TrimSpace(a); a == "" {
				continue
			}
			if err := checkPrefix(a); err != nil {
				return s, fmt.Errorf("别名%s", strings.TrimPrefix(err.Error(), "前缀"))
			}
			if !slices.ContainsFunc(out, func(x string) bool { return strings.EqualFold(x, a) }) {
				out = append(out, a)
			}
		}
		if len(out) > 8 {
			return s, fmt.Errorf("别名最多 8 个")
		}
		s.Aliases = out
	}
	if p.Allowed != nil {
		var out []string
		for _, n := range *p.Allowed {
			n = strings.TrimSpace(n)
			if why := validSender(n, true); why != "" {
				return s, fmt.Errorf("「%s」不能授权：%s", n, why)
			}
			if !slices.ContainsFunc(out, func(x string) bool { return strings.EqualFold(x, n) }) {
				out = append(out, n)
			}
		}
		if len(out) > 200 {
			return s, fmt.Errorf("能执行命令的玩家最多 200 个")
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
		s.Allowed = out
	}
	for id, on := range p.Templates {
		if TemplateByID(id) == nil {
			return s, fmt.Errorf("没有这个命令模板：%s", id)
		}
		s.Templates[id] = on
	}
	for _, x := range []struct {
		v   *int
		dst *int
		max int
		w   string
	}{{p.Cooldown, &s.Cooldown, 3600, "冷却"}, {p.PerPlayer, &s.PerPlayer, 100000, "每人每天的次数"}, {p.PerServer, &s.PerServer, 1000000, "全服每天的次数"}} {
		if x.v == nil {
			continue
		}
		if *x.v < 0 || *x.v > x.max {
			return s, fmt.Errorf("%s要在 0 到 %d 之间（0 表示不限）", x.w, x.max)
		}
		*x.dst = *x.v
	}
	if p.Budget != nil {
		if *p.Budget < 0 || *p.Budget > 1e11 {
			return s, fmt.Errorf("全服每天的 token 要在 0 到 1000 亿之间（0 表示不限）")
		}
		s.Budget = *p.Budget
	}
	if p.Loose != nil {
		s.Loose = *p.Loose
	}
	if p.AnyThread != nil {
		s.AnyThread = *p.AnyThread
	}
	if p.Pattern != nil {
		v := strings.TrimSpace(*p.Pattern)
		if utf8.RuneCountInString(v) > 500 {
			return s, fmt.Errorf("自定义正则最多 500 个字")
		}
		if _, err := compilePattern(v); err != nil {
			return s, err
		}
		s.Pattern = v
	}
	return s, nil
}

func cloneMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// loadSettings 读实例目录里的设置；没有文件或文件坏了按默认值（坏了的部分逐项回到默认）。
func loadSettings(dir string) Settings {
	s := Defaults()
	b, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		return s
	}
	var f Settings
	if json.Unmarshal(b, &f) != nil {
		return s
	}
	s.Enabled = f.Enabled
	if checkPrefix(f.Prefix) == nil {
		s.Prefix = f.Prefix
	}
	for _, a := range f.Aliases {
		if checkPrefix(a) == nil && len(s.Aliases) < 8 {
			s.Aliases = append(s.Aliases, a)
		}
	}
	for _, n := range f.Allowed {
		if validSender(n, true) == "" {
			s.Allowed = append(s.Allowed, n)
		}
	}
	for id, on := range f.Templates {
		if TemplateByID(id) != nil {
			s.Templates[id] = on
		}
	}
	if f.Cooldown >= 0 && f.Cooldown <= 3600 {
		s.Cooldown = f.Cooldown
	}
	if f.PerPlayer >= 0 {
		s.PerPlayer = f.PerPlayer
	}
	if f.PerServer >= 0 {
		s.PerServer = f.PerServer
	}
	if f.Budget >= 0 {
		s.Budget = f.Budget
	}
	s.Loose, s.AnyThread = f.Loose, f.AnyThread
	if _, err := compilePattern(f.Pattern); err == nil {
		s.Pattern = f.Pattern
	}
	return s
}

func saveSettings(dir string, s Settings) error {
	return writeJSON(filepath.Join(dir, settingsFile), s)
}

// writeJSON 原子地写一个 JSON 文件（先写临时文件再改名）。
func writeJSON(path string, v any) error {
	b, err := encodeJSON(v)
	if err != nil {
		return err
	}
	return writeFile(path, b)
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeFile(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
