package companionjar

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
)

// 内置的清单能读出来，两个加载器各有一个 jar，内容和登记的大小、sha256 一致。
func TestLoad(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Mod.ID != "minedeck_companion" || m.Mod.Protocol != 1 || m.Mod.License != "MIT" {
		t.Fatalf("mod：%+v", m.Mod)
	}
	for _, c := range []struct{ loader, mc string }{{LoaderNeoForge, "1.21.1"}, {LoaderForge, "1.20.1"}} {
		j, ok := m.Find(c.loader, c.mc)
		if !ok {
			t.Fatalf("找不到 %s %s 的 jar", c.loader, c.mc)
		}
		b, err := j.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(b)) != j.Size || sha256Hex(b) != j.SHA256 {
			t.Fatalf("%s：内容和登记的不一致", j.File)
		}
		if j.InstallName != FilePrefix+c.loader+"-"+c.mc+"-"+m.Mod.Version+".jar" {
			t.Errorf("installName：%s", j.InstallName)
		}
		mods := map[string]bool{}
		for _, d := range j.Dependencies {
			mods[d.ModID] = true
			if d.Modrinth.Tested.ModVersion == "" || !strings.Contains(strings.Join(d.Modrinth.Loaders, ","), c.loader) ||
				!strings.Contains(strings.Join(d.Modrinth.GameVersions, ","), c.mc) {
				t.Errorf("%s 的依赖 %s 写得不对：%+v", j.File, d.ModID, d.Modrinth)
			}
		}
		if !mods["playerengine"] || !mods["architectury"] {
			t.Errorf("%s 的依赖不全：%v", j.File, mods)
		}
	}
	if _, ok := m.Find(LoaderForge, "1.21.1"); ok {
		t.Error("Forge 1.21.1 不应该有 jar")
	}
	if _, ok := m.Find(LoaderNeoForge, "1.20.1"); ok {
		t.Error("NeoForge 1.20.1 不应该有 jar")
	}
	var raw map[string]any
	if err := json.Unmarshal(RawManifest(), &raw); err != nil || raw["schema"] != float64(1) {
		t.Fatalf("RawManifest：%v %v", raw["schema"], err)
	}
}

// jar 里的元数据和清单对得上：模组 ID、版本、依赖的版本范围和 side；没有 PlayerEngine、Architectury 的文件；
// 带许可说明；class 的版本和清单里的 Java 版本一致。
func TestJarContents(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range m.Jars {
		b, err := j.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		tomlName := "META-INF/mods.toml"
		if j.Loader == LoaderNeoForge {
			tomlName = "META-INF/neoforge.mods.toml"
		}
		names := map[string]bool{}
		classes := 0
		wantMajor := byte(j.Java + 44) // Java 17 → 61，Java 21 → 65
		for _, f := range zr.File {
			names[f.Name] = true
			for _, foreign := range []string{"com/player2/", "dev/architectury/", "baritone/", "META-INF/jars/"} {
				if strings.HasPrefix(f.Name, foreign) {
					t.Errorf("%s 里有别人的文件：%s", j.File, f.Name)
				}
			}
			if strings.HasSuffix(f.Name, ".class") {
				classes++
				head := readHead(t, f, 8)
				if head[6] != 0 || head[7] != wantMajor {
					t.Errorf("%s 的 %s 不是 Java %d 的字节码：% x", j.File, f.Name, j.Java, head)
				}
			}
		}
		for _, need := range []string{tomlName, "META-INF/LICENSE-NOTICE.md", "pack.mcmeta"} {
			if !names[need] {
				t.Errorf("%s 里缺 %s", j.File, need)
			}
		}
		if classes < 5 {
			t.Errorf("%s 只有 %d 个 class", j.File, classes)
		}
		f, err := zr.Open(tomlName)
		if err != nil {
			t.Fatal(err)
		}
		toml, _ := io.ReadAll(f)
		f.Close()
		mod, deps := parseModsToml(string(toml))
		if mod["modId"] != m.Mod.ID || mod["version"] != m.Mod.Version || mod["license"] != m.Mod.License {
			t.Errorf("%s 的 %s：%v，清单：%+v", j.File, tomlName, mod, m.Mod)
		}
		for _, d := range j.Dependencies {
			got, ok := deps[d.ModID]
			if !ok {
				t.Errorf("%s 的 %s 没有声明依赖 %s", j.File, tomlName, d.ModID)
				continue
			}
			if got["versionRange"] != d.VersionRange || strings.ToLower(got["side"]) != d.Side {
				t.Errorf("%s 对 %s 的依赖：mods.toml 写的是 %v，清单是 %s / %s", j.File, d.ModID, got, d.VersionRange, d.Side)
			}
			if !inRange(d.Modrinth.Tested.ModVersion, d.VersionRange) {
				t.Errorf("%s：实测的 %s %s 不在 %s 之内", j.File, d.ModID, d.Modrinth.Tested.ModVersion, d.VersionRange)
			}
		}
		if got := deps[j.Loader]; got["versionRange"] != j.LoaderVersions {
			t.Errorf("%s：加载器版本范围 mods.toml 写的是 %v，清单是 %s", j.File, got, j.LoaderVersions)
		}
		if got := deps["minecraft"]; !inRange(j.Minecraft, got["versionRange"]) {
			t.Errorf("%s：MC 版本 %s 不在 mods.toml 的 %v 之内", j.File, j.Minecraft, got)
		}
	}
}

// 客户端会加载的类（入口、只有外形的实体、渲染器、进服检查、PlayerEngine 实体类型的占位、mixin）一个都不引用
// PlayerEngine 和 Architectury 的类：玩家的客户端只装伙伴模组本身时，这些类照样能加载。mixin 配置也接好了。
func TestClientSafeClasses(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	clientSide := regexp.MustCompile(`^minedeck/companion/(CompanionMod|CompanionEntity|Compat|JoinCheck(\$.*)?|PlaceholderEntity|client/.*|mixin/.*)\.class$`)
	for _, j := range m.Jars {
		b, err := j.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		files := map[string][]byte{}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			files[f.Name] = data
			if !clientSide.MatchString(f.Name) {
				continue
			}
			checked++
			for _, foreign := range []string{"com/player2/", "dev/architectury/", "minedeck/companion/ServerCompanion", "minedeck/companion/CompanionManager"} {
				if bytes.Contains(data, []byte(foreign)) {
					t.Errorf("%s 的 %s 引用了 %s（客户端没装 PlayerEngine 时会加载失败）", j.File, f.Name, foreign)
				}
			}
		}
		if checked < 8 {
			t.Errorf("%s 只检查到 %d 个客户端会加载的类", j.File, checked)
		}
		if _, ok := files["minedeck_companion.mixins.json"]; !ok {
			t.Errorf("%s 里缺 mixin 配置", j.File)
		}
		switch j.Loader {
		case LoaderNeoForge:
			if !strings.Contains(string(files["META-INF/neoforge.mods.toml"]), `config = "minedeck_companion.mixins.json"`) {
				t.Errorf("%s 的 neoforge.mods.toml 没有登记 mixin 配置", j.File)
			}
		case LoaderForge:
			if !strings.Contains(string(files["META-INF/MANIFEST.MF"]), "MixinConfigs: minedeck_companion.mixins.json") {
				t.Errorf("%s 的 MANIFEST.MF 没有 MixinConfigs", j.File)
			}
		}
		for _, d := range j.Dependencies {
			if d.Side != SideServer {
				t.Errorf("%s 的依赖 %s 应该只要求服务端装（side=server），现在是 %s", j.File, d.ModID, d.Side)
			}
		}
	}
}

// 清单写错、文件对不上时 parse 报错，不会把坏的 jar 交出去。
func TestParseRejects(t *testing.T) {
	good := RawManifest()
	read := files.ReadFile
	if _, err := parse(good, read); err != nil {
		t.Fatal(err)
	}
	mutate := func(f func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(good, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		b, _ := json.Marshal(m)
		return b
	}
	jar0 := func(m map[string]any) map[string]any { return m["jars"].([]any)[0].(map[string]any) }
	dep0 := func(m map[string]any) map[string]any {
		return jar0(m)["dependencies"].([]any)[0].(map[string]any)
	}
	cases := map[string][]byte{
		"不是 JSON":   []byte("{"),
		"格式版本不对":    mutate(func(m map[string]any) { m["schema"] = 2 }),
		"没有 jar":    mutate(func(m map[string]any) { m["jars"] = []any{} }),
		"sha256 不对": mutate(func(m map[string]any) { jar0(m)["sha256"] = strings.Repeat("0", 64) }),
		"大小不对":      mutate(func(m map[string]any) { jar0(m)["size"] = 1 }),
		"文件不存在":     mutate(func(m map[string]any) { jar0(m)["file"] = "nope.jar" }),
		"安装名不带版本":   mutate(func(m map[string]any) { jar0(m)["installName"] = FilePrefix + "x.jar" }),
		"加载器不认识":    mutate(func(m map[string]any) { jar0(m)["loader"] = "fabric" }),
		"重复的组合":     mutate(func(m map[string]any) { m["jars"] = []any{jar0(m), jar0(m)} }),
		"side 不对":   mutate(func(m map[string]any) { dep0(m)["side"] = "client" }),
		"规则不认识":     mutate(func(m map[string]any) { dep0(m)["modrinth"].(map[string]any)["policy"] = "latest" }),
		"正则不合法":     mutate(func(m map[string]any) { dep0(m)["modrinth"].(map[string]any)["versionPattern"] = "(" }),
		"实测版本不符合正则": mutate(func(m map[string]any) { dep0(m)["modrinth"].(map[string]any)["versionPattern"] = "^9$" }),
		"下载地址不是这个版本的": mutate(func(m map[string]any) {
			dep0(m)["modrinth"].(map[string]any)["tested"].(map[string]any)["url"] = "https://example.com/x.jar"
		}),
	}
	for name, raw := range cases {
		if _, err := parse(raw, read); err == nil {
			t.Errorf("%s：应该报错", name)
		}
	}
	// 读不出文件
	if _, err := parse(good, func(string) ([]byte, error) { return nil, errors.New("坏了") }); err == nil {
		t.Error("读不出 jar 时应该报错")
	}
}

func readHead(t *testing.T, f *zip.File, n int) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	head := make([]byte, n)
	if _, err := io.ReadFull(rc, head); err != nil {
		t.Fatal(err)
	}
	return head
}

var tomlKV = regexp.MustCompile(`^\s*([A-Za-z]+)\s*=\s*"([^"]*)"`)

// parseModsToml 只认这里用到的那点 TOML：顶层和 [[mods]] 下的键，以及每个 [[dependencies.<id>]] 块里的键（按 modId 归类）。
func parseModsToml(s string) (mod map[string]string, deps map[string]map[string]string) {
	mod, deps = map[string]string{}, map[string]map[string]string{}
	top := map[string]string{}
	cur := top
	var blocks []map[string]string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "[[mods]]":
			cur = mod
		case strings.HasPrefix(line, "[[dependencies."):
			cur = map[string]string{}
			blocks = append(blocks, cur)
		default:
			if m := tomlKV.FindStringSubmatch(line); m != nil {
				cur[m[1]] = m[2]
			}
		}
	}
	if l, ok := top["license"]; ok {
		mod["license"] = l
	}
	for _, d := range blocks {
		deps[d["modId"]] = d
	}
	return mod, deps
}

// inRange 判断 v 在不在 Maven 写法的版本范围里（只支持 "[a,b)"、"[a,)" 这两种，按点、横线分段比较）。
func inRange(v, r string) bool {
	if !strings.HasPrefix(r, "[") || !(strings.HasSuffix(r, ")") || strings.HasSuffix(r, "]")) {
		return false
	}
	lo, hi, ok := strings.Cut(r[1:len(r)-1], ",")
	if !ok {
		return false
	}
	if compareVersion(v, lo) < 0 {
		return false
	}
	if hi == "" {
		return true
	}
	c := compareVersion(v, hi)
	return c < 0 || (c == 0 && strings.HasSuffix(r, "]"))
}

func compareVersion(a, b string) int {
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
		xn, xerr := atoi(x)
		yn, yerr := atoi(y)
		switch {
		case xerr == nil && yerr == nil:
			if xn < yn {
				return -1
			}
			return 1
		case x == "":
			return -1
		case y == "":
			return 1
		case x < y:
			return -1
		default:
			return 1
		}
	}
	return 0
}

func atoi(s string) (int, error) {
	if s == "" {
		return 0, errors.New("空")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("不是数字")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func TestInRange(t *testing.T) {
	for _, c := range []struct {
		v, r string
		want bool
	}{
		{"1.21.1-1.4.0", "[1.21.1-1.4.0,1.21.1-1.5)", true},
		{"1.21.1-1.4.9", "[1.21.1-1.4.0,1.21.1-1.5)", true},
		{"1.21.1-1.5.0", "[1.21.1-1.4.0,1.21.1-1.5)", false},
		{"1.21.1-1.3.0", "[1.21.1-1.4.0,1.21.1-1.5)", false},
		{"13.0.11", "[13.0.8,)", true},
		{"13.0.6", "[13.0.8,)", false},
		{"1.21.1", "[1.21.1,1.21.2)", true},
		{"1.21.2", "[1.21.1,1.21.2)", false},
	} {
		if got := inRange(c.v, c.r); got != c.want {
			t.Errorf("inRange(%s, %s) = %v", c.v, c.r, got)
		}
	}
}
