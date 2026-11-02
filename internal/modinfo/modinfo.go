// Package modinfo 解析 mods / plugins 目录里的 jar：模组 ID、名称、版本、依赖、mixin 配置。
// 认 NeoForge（META-INF/neoforge.mods.toml）、Forge（META-INF/mods.toml）、Fabric（fabric.mod.json）、
// Quilt（quilt.mod.json）和 Bukkit 系插件（plugin.yml / paper-plugin.yml）。
//
// 只读 jar 的目录和几个小文件，不执行任何东西；不依赖 Windows，Linux 上可以直接测试。
package modinfo

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dep 一个依赖。
type Dep struct {
	ID       string `json:"id"`
	Range    string `json:"range,omitempty"` // 版本范围，如 [4.2,)
	Required bool   `json:"required"`        // 必需（不装就起不来）
	Kind     string `json:"kind,omitempty"`  // NeoForge 的 type：incompatible（不兼容）、discouraged（不建议同装）；Fabric 的 breaks 记成 incompatible
	Side     string `json:"side,omitempty"`  // BOTH / CLIENT / SERVER
}

// Mod 一个 jar 的解析结果。一个 jar 里登记了几个模组时，第一个作为主模组，其余的 ID 放进 Also。
type Mod struct {
	File     string   `json:"file"`
	Size     int64    `json:"size"`
	Disabled bool     `json:"disabled,omitempty"` // 文件名以 .disabled 结尾（MineDeck 禁用的）
	Loader   string   `json:"loader"`             // neoforge / forge / neoforge+forge / fabric / quilt / plugin / unknown
	ID       string   `json:"id,omitempty"`
	Name     string   `json:"name,omitempty"`
	Version  string   `json:"version,omitempty"`
	Env      string   `json:"env,omitempty"` // Fabric 的 environment：client / server / *
	Also     []string `json:"also,omitempty"`
	Deps     []Dep    `json:"deps,omitempty"`
	Mixins   []string `json:"mixins,omitempty"` // mixin 配置文件名
	Err      string   `json:"err,omitempty"`    // 读不了的原因（坏的 jar、不是模组等）
}

// IsJar 文件名是不是 jar（含禁用的 .jar.disabled）。
func IsJar(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".jar") || strings.HasSuffix(n, ".jar.disabled")
}

var cache = struct {
	sync.Mutex
	m map[string]cached
}{m: map[string]cached{}}

type cached struct {
	size int64
	mod  time.Time
	info Mod
}

// ReadDir 解析目录里的全部 jar，按文件名排序。目录不存在返回空列表。结果按「路径 + 大小 + 修改时间」缓存。
func ReadDir(dir string) ([]Mod, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Mod{}, nil
		}
		return nil, err
	}
	out := []Mod{}
	for _, e := range ents {
		if e.IsDir() || !IsJar(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		cache.Lock()
		c, ok := cache.m[path]
		cache.Unlock()
		if !ok || c.size != st.Size() || !c.mod.Equal(st.ModTime()) {
			c = cached{size: st.Size(), mod: st.ModTime(), info: Read(path)}
			cache.Lock()
			cache.m[path] = c
			cache.Unlock()
		}
		out = append(out, c.info)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].File) < strings.ToLower(out[j].File) })
	return out, nil
}

// Read 解析一个 jar。读不了的写在 Err 里（不返回错误：坏的 jar 也要出现在清单里）。
func Read(path string) Mod {
	m := Mod{File: filepath.Base(path), Loader: "unknown"}
	m.Disabled = strings.HasSuffix(strings.ToLower(m.File), ".disabled")
	if st, err := os.Stat(path); err == nil {
		m.Size = st.Size()
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		m.Err = "不是有效的 jar（zip）文件：" + err.Error()
		return m
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(name string) (string, bool) {
		f := files[name]
		if f == nil || f.UncompressedSize64 > 4<<20 {
			return "", false
		}
		rc, err := f.Open()
		if err != nil {
			return "", false
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, 4<<20))
		if err != nil {
			return "", false
		}
		return strings.TrimPrefix(string(b), "\ufeff"), true
	}
	manifest, _ := read("META-INF/MANIFEST.MF")
	neo, hasNeo := read("META-INF/neoforge.mods.toml")
	forge, hasForge := read("META-INF/mods.toml")
	switch {
	case hasNeo || hasForge:
		src := neo
		m.Loader = "neoforge"
		switch {
		case hasNeo && hasForge:
			m.Loader = "neoforge+forge"
		case hasForge:
			src, m.Loader = forge, "forge"
		}
		parseModsTOML(&m, src, manifestValue(manifest, "Implementation-Version"))
		if mc := manifestValue(manifest, "MixinConfigs"); mc != "" {
			for _, c := range strings.Split(mc, ",") {
				if c = strings.TrimSpace(c); c != "" && !contains(m.Mixins, c) {
					m.Mixins = append(m.Mixins, c)
				}
			}
		}
	default:
		if s, ok := read("fabric.mod.json"); ok {
			m.Loader = "fabric"
			parseFabric(&m, s)
		} else if s, ok := read("quilt.mod.json"); ok {
			m.Loader = "quilt"
			parseQuilt(&m, s)
		} else if s, ok := read("paper-plugin.yml"); ok {
			m.Loader = "plugin"
			parsePluginYML(&m, s)
		} else if s, ok := read("plugin.yml"); ok {
			m.Loader = "plugin"
			parsePluginYML(&m, s)
		} else {
			m.Err = "jar 里没有模组或插件的描述文件（mods.toml、fabric.mod.json、plugin.yml）"
		}
	}
	return m
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// manifestValue 取 MANIFEST.MF 里一项的值（续行以一个空格开头）。
func manifestValue(manifest, key string) string {
	lines := strings.Split(strings.ReplaceAll(manifest, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			v = strings.TrimSpace(v)
			for j := i + 1; j < len(lines) && strings.HasPrefix(lines[j], " "); j++ {
				v += lines[j][1:]
			}
			return v
		}
	}
	return ""
}

// ---------- mods.toml（NeoForge / Forge）----------

// parseModsTOML 从 mods.toml 里取 [[mods]]、[[dependencies.<modid>]] 和 [[mixins]]。
func parseModsTOML(m *Mod, src, jarVersion string) {
	tables, err := parseTOML(src)
	if err != nil {
		m.Err = "mods.toml 读不懂：" + err.Error()
	}
	for _, t := range tables {
		switch {
		case t.name == "mods":
			id := t.str("modId")
			if id == "" {
				continue
			}
			if m.ID == "" {
				m.ID = id
				m.Name = t.str("displayName")
				m.Version = t.str("version")
				if m.Version == "${file.jarVersion}" {
					m.Version = jarVersion
				}
			} else if !contains(m.Also, id) {
				m.Also = append(m.Also, id)
			}
		case strings.HasPrefix(t.name, "dependencies."):
			owner := strings.Trim(strings.TrimPrefix(t.name, "dependencies."), `"'`)
			if owner != m.ID && m.ID != "" && !strings.Contains(owner, "${") {
				continue // 只记主模组的依赖
			}
			d := Dep{ID: t.str("modId"), Range: t.str("versionRange"), Side: strings.ToUpper(t.str("side"))}
			if d.ID == "" {
				continue
			}
			switch typ := strings.ToLower(t.str("type")); {
			case typ == "required":
				d.Required = true
			case typ == "incompatible" || typ == "discouraged":
				d.Kind = typ
			case typ == "":
				d.Required = t.bool("mandatory") // Forge 的写法
			}
			m.Deps = append(m.Deps, d)
		case t.name == "mixins":
			if c := t.str("config"); c != "" && !contains(m.Mixins, c) {
				m.Mixins = append(m.Mixins, c)
			}
		}
	}
}

// table 一张 TOML 表（[name] 或 [[name]] 的一项）。
type table struct {
	name string
	kv   map[string]any // 只认字符串和布尔，其余的值（数字、数组、内联表）存原文
}

func (t table) str(k string) string {
	if s, ok := t.kv[k].(string); ok {
		return s
	}
	return ""
}

func (t table) bool(k string) bool { b, _ := t.kv[k].(bool); return b }

// parseTOML 宽松地解析 TOML：够 mods.toml 用（表头、键值、基本 / 字面字符串含多行、布尔、注释），
// 不认识的值保留原文，读不懂的行跳过。
func parseTOML(src string) ([]table, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out []table
	cur := table{kv: map[string]any{}}
	flush := func() {
		if cur.name != "" || len(cur.kv) > 0 {
			out = append(out, cur)
		}
	}
	var firstErr error
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") {
			name := strings.TrimSpace(stripComment(l))
			name = strings.TrimSpace(strings.Trim(name, "[]"))
			flush()
			cur = table{name: unquoteKey(name), kv: map[string]any{}}
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		val, err := tomlValue(strings.TrimSpace(v), lines, &i)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("第 %d 行：%v", i+1, err)
		}
		cur.kv[unquoteKey(strings.TrimSpace(k))] = val
	}
	flush()
	return out, firstErr
}

// tomlValue 解析一个值；多行字符串会接着往下读，*i 移到它结束的那一行。
func tomlValue(v string, lines []string, i *int) (any, error) {
	for _, q := range []string{`"""`, `'''`} {
		if !strings.HasPrefix(v, q) {
			continue
		}
		body := v[3:]
		for !strings.Contains(body, q) && *i+1 < len(lines) {
			*i++
			body += "\n" + lines[*i]
		}
		if j := strings.Index(body, q); j >= 0 {
			body = body[:j]
		}
		body = strings.TrimPrefix(body, "\n")
		if q == `"""` {
			body = unescapeBasic(body)
		}
		return body, nil
	}
	switch {
	case strings.HasPrefix(v, `"`):
		return basicString(v)
	case strings.HasPrefix(v, "'"):
		if j := strings.Index(v[1:], "'"); j >= 0 {
			return v[1 : j+1], nil
		}
		return v[1:], errors.New("字符串没有结束的引号")
	case strings.HasPrefix(v, "true"):
		return true, nil
	case strings.HasPrefix(v, "false"):
		return false, nil
	}
	return strings.TrimSpace(stripComment(v)), nil
}

func unquoteKey(k string) string {
	parts := strings.Split(k, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ".")
}

// stripComment 去掉行尾注释（不在字符串里的 #）。
func stripComment(s string) string {
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' && inStr == '"' {
				i++
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
		case c == '#':
			return s[:i]
		}
	}
	return s
}

// basicString 解析以 " 开头的基本字符串，返回内容。
func basicString(v string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '"':
			return b.String(), nil
		case c == '\\' && i+1 < len(v):
			i++
			b.WriteString(unescapeBasic(`\` + string(v[i])))
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), errors.New("字符串没有结束的引号")
}

func unescapeBasic(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	return strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`, `\r`, "").Replace(s)
}

// ---------- fabric.mod.json / quilt.mod.json ----------

func parseFabric(m *Mod, src string) {
	var f struct {
		ID          string                     `json:"id"`
		Name        string                     `json:"name"`
		Version     string                     `json:"version"`
		Environment string                     `json:"environment"`
		Depends     map[string]json.RawMessage `json:"depends"`
		Breaks      map[string]json.RawMessage `json:"breaks"`
		Mixins      []json.RawMessage          `json:"mixins"`
	}
	if err := json.Unmarshal([]byte(src), &f); err != nil {
		m.Err = "fabric.mod.json 读不懂：" + err.Error()
		return
	}
	m.ID, m.Name, m.Version, m.Env = f.ID, f.Name, f.Version, f.Environment
	rng := func(raw json.RawMessage) string {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		var ss []string
		if json.Unmarshal(raw, &ss) == nil {
			return strings.Join(ss, " || ")
		}
		return ""
	}
	for _, id := range sortedKeys(f.Depends) {
		m.Deps = append(m.Deps, Dep{ID: id, Range: rng(f.Depends[id]), Required: true})
	}
	for _, id := range sortedKeys(f.Breaks) {
		m.Deps = append(m.Deps, Dep{ID: id, Range: rng(f.Breaks[id]), Kind: "incompatible"})
	}
	for _, raw := range f.Mixins {
		var s string
		var o struct {
			Config string `json:"config"`
		}
		if json.Unmarshal(raw, &s) == nil && s != "" {
			m.Mixins = append(m.Mixins, s)
		} else if json.Unmarshal(raw, &o) == nil && o.Config != "" {
			m.Mixins = append(m.Mixins, o.Config)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func parseQuilt(m *Mod, src string) {
	var f struct {
		Loader struct {
			ID       string `json:"id"`
			Version  string `json:"version"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"quilt_loader"`
	}
	if err := json.Unmarshal([]byte(src), &f); err != nil {
		m.Err = "quilt.mod.json 读不懂：" + err.Error()
		return
	}
	m.ID, m.Version, m.Name = f.Loader.ID, f.Loader.Version, f.Loader.Metadata.Name
}

// ---------- plugin.yml ----------

// parsePluginYML 只取 name、version、depend、softdepend（顶层的简单写法）。
func parsePluginYML(m *Mod, src string) {
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	list := ""
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "-") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "- ") && list != "" {
				m.Deps = append(m.Deps, Dep{ID: yamlScalar(t[2:]), Required: list == "depend"})
			}
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		list = ""
		switch k {
		case "name":
			m.ID, m.Name = yamlScalar(v), yamlScalar(v)
		case "version":
			m.Version = yamlScalar(v)
		case "depend", "softdepend":
			if v == "" {
				list = k
				continue
			}
			for _, d := range strings.Split(strings.Trim(v, "[]"), ",") {
				if d = yamlScalar(d); d != "" {
					m.Deps = append(m.Deps, Dep{ID: d, Required: k == "depend"})
				}
			}
		}
	}
}

func yamlScalar(s string) string {
	s = strings.TrimSpace(stripComment(s))
	return strings.Trim(s, `"'`)
}

// ---------- 按类名找 jar ----------

// FindClass 在目录的 jar 里找这个类（a/b/C 或 a.b.C 都行），返回 jar 的文件名；找不到返回空串。
// 只读 jar 的目录（zip 的中央目录），不解压。
func FindClass(dir, class string) string {
	class = strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(class), ".", "/"), "/class")
	if class == "" {
		return ""
	}
	want := class + ".class"
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() || !IsJar(e.Name()) || strings.HasSuffix(strings.ToLower(e.Name()), ".disabled") {
			continue
		}
		zr, err := zip.OpenReader(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		found := false
		for _, f := range zr.File {
			if f.Name == want {
				found = true
				break
			}
		}
		zr.Close()
		if found {
			return e.Name()
		}
	}
	return ""
}
